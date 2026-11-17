// A Transport that talks to the gateway over WebSocket.
//
// CollabSession speaks in SyncMessages (see messages.ts); the gateway speaks
// its own protocol (server-protocol.ts). This transport translates between
// the two, and adds what a network connection needs:
//
// - Every op sent stays in an unacknowledged queue until the server acks it.
//   After a reconnect the whole queue is sent again. The server answers
//   anything it already stored as a duplicate, so resending is always safe,
//   and an edit typed while the connection was down is never lost.
// - The last sequence number with no gaps (SeqTracker) goes in the join
//   message, so a reconnect receives exactly what was missed.
// - Dropped connections are retried with exponential backoff and jitter, so
//   a restarted gateway is not hit by every client at the same instant.
//
// Session messages that only make sense between tabs (hello, state) are not
// sent: the server catches clients up from lastSeq instead.
//
// It also carries presence: it keeps the list of other editors (forgetting
// one not heard from in three heartbeats, as the server does) and sends this
// editor's cursor, at most every 50 ms and again after every reconnect.

import type { WireOp } from "@/lib/crdt";
import type { SyncMessage } from "./messages";
import { SeqTracker } from "./seq-tracker";
import { opKey, parseServerMessage, type Cursor, type Identity, type Peer } from "./server-protocol";
import type { Transport } from "./transport";

export type ConnectionStatus = "connecting" | "connected" | "reconnecting" | "closed";

/** The part of the browser's WebSocket this transport uses; tests fake it. */
export interface SocketLike {
  readonly readyState: number;
  send(data: string): void;
  close(code?: number, reason?: string): void;
  onopen: ((event: unknown) => void) | null;
  onmessage: ((event: { data: unknown }) => void) | null;
  onclose: ((event: { code: number; reason: string }) => void) | null;
  onerror: ((event: unknown) => void) | null;
}

export interface WebSocketTransportOptions {
  /** The gateway's WebSocket URL, such as ws://localhost:8080/ws. */
  url: string;
  docId: string;
  /** Must be the CollabSession's replica: the server checks every op's author. */
  replica: string;
  name?: string;
  /** Told about every connection change, and who the server says we are. */
  onStatus?: (status: ConnectionStatus, you?: Identity) => void;
  /** Creates the socket; defaults to the browser's WebSocket. */
  createSocket?: (url: string) => SocketLike;
  /** Reconnect delays: the first retry, and the most it grows to. */
  backoff?: { initialMs: number; maxMs: number };
  /** Told the other editors (sorted by name) whenever the list changes. */
  onPeers?: (peers: Peer[]) => void;
  /**
   * How long a peer stays listed without being heard from. The server
   * announces everyone every heartbeat (10 s), so 30 s is three missed.
   */
  peerTimeoutMs?: number;
}

/** A transport that also carries presence. */
export interface PresenceTransport extends Transport {
  /** Sets where this editor's selection is; sent soon, and after reconnects. */
  setCursor(cursor: Cursor): void;
}

/** Cursor moves are sent at most this often. */
export const CURSOR_THROTTLE_MS = 50;
const DEFAULT_PEER_TIMEOUT_MS = 30_000;

/** The server accepts up to 1000 ops per message; stay well below. */
const MAX_OPS_PER_MESSAGE = 500;
const OPEN = 1;

/** Close codes after which retrying cannot help: the server refused us. */
const FATAL_CLOSE_CODES = new Set([1003 /* unsupported data */, 1008 /* policy violation */]);

export function websocketTransport(options: WebSocketTransportOptions): PresenceTransport {
  return new WebSocketTransport(options);
}

class WebSocketTransport implements PresenceTransport {
  #opts: WebSocketTransportOptions;
  #socket: SocketLike | null = null;
  #handler: ((data: unknown) => void) | null = null;
  /** Ops sent but not yet acknowledged, in the order they were made. */
  #unacked = new Map<string, WireOp>();
  #seqs = new SeqTracker();
  #joined = false;
  #closed = false;
  #attempt = 0;
  #retry: ReturnType<typeof setTimeout> | null = null;
  /** Other editors, with when each was last heard from. */
  #peers = new Map<string, { peer: Peer; seen: number }>();
  #sweep: ReturnType<typeof setInterval> | null = null;
  /** This editor's latest cursor, and the last one actually sent. */
  #cursor: Cursor | null = null;
  #sentCursor = "";
  #cursorTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(opts: WebSocketTransportOptions) {
    this.#opts = opts;
    if (opts.onPeers) {
      const timeout = opts.peerTimeoutMs ?? DEFAULT_PEER_TIMEOUT_MS;
      this.#sweep = setInterval(() => this.#forgetSilentPeers(timeout), timeout / 3);
    }
    this.#connect();
  }

  setCursor(cursor: Cursor): void {
    if (this.#closed) return;
    this.#cursor = cursor;
    // Trailing throttle: the latest position goes out within 50 ms, and a
    // fast run of moves costs one message.
    this.#cursorTimer ??= setTimeout(() => {
      this.#cursorTimer = null;
      this.#sendCursor(false);
    }, CURSOR_THROTTLE_MS);
  }

  send(message: SyncMessage): void {
    if (message.type !== "ops" || this.#closed) return;
    for (const op of message.ops) {
      this.#unacked.set(opKey(op.id), op);
    }
    // Before joining, the ops wait in the queue and go out after "joined".
    if (this.#joined) {
      this.#sendOps(message.ops);
    }
  }

  onMessage(handler: (data: unknown) => void): void {
    this.#handler = handler;
  }

  close(): void {
    this.#closed = true;
    if (this.#retry) clearTimeout(this.#retry);
    if (this.#sweep) clearInterval(this.#sweep);
    if (this.#cursorTimer) clearTimeout(this.#cursorTimer);
    this.#setPeers(new Map());
    this.#socket?.close(1000, "closed by the editor");
    this.#opts.onStatus?.("closed");
  }

  #connect(): void {
    this.#opts.onStatus?.(this.#attempt === 0 ? "connecting" : "reconnecting");
    const socket = (this.#opts.createSocket ?? ((url) => new WebSocket(url) as unknown as SocketLike))(this.#opts.url);
    this.#socket = socket;

    socket.onopen = () => {
      socket.send(
        JSON.stringify({
          type: "join",
          doc: this.#opts.docId,
          replica: this.#opts.replica,
          name: this.#opts.name,
          lastSeq: this.#seqs.last,
        }),
      );
    };
    socket.onmessage = (event) => this.#receive(event.data);
    socket.onclose = (event) => this.#onClose(event.code, event.reason);
    socket.onerror = () => {}; // a close event always follows
  }

  #receive(raw: unknown): void {
    const msg = parseServerMessage(raw);
    if (!msg) {
      console.warn("sync: ignoring a malformed message from the gateway");
      return;
    }
    switch (msg.type) {
      case "remote":
        for (const { seq } of msg.ops) this.#seqs.add(seq);
        this.#handler?.({ type: "ops", from: "server", ops: msg.ops.map((o) => o.op) });
        break;
      case "joined":
        this.#joined = true;
        this.#attempt = 0;
        this.#opts.onStatus?.("connected", msg.you ?? undefined);
        // Whatever was not acknowledged before the connection dropped,
        // plus anything typed while it was down.
        if (this.#unacked.size > 0) {
          this.#sendOps([...this.#unacked.values()]);
        }
        // The server forgot our cursor with the old connection.
        this.#sendCursor(true);
        break;
      case "peers": {
        const now = Date.now();
        const peers = new Map<string, { peer: Peer; seen: number }>();
        for (const peer of msg.peers) {
          if (peer.replica !== this.#opts.replica) peers.set(peer.replica, { peer, seen: now });
        }
        this.#setPeers(peers);
        break;
      }
      case "peer":
        if (msg.peer.replica !== this.#opts.replica) {
          const peers = new Map(this.#peers);
          peers.set(msg.peer.replica, { peer: msg.peer, seen: Date.now() });
          this.#setPeers(peers);
        }
        break;
      case "left":
        if (this.#peers.has(msg.replica)) {
          const peers = new Map(this.#peers);
          peers.delete(msg.replica);
          this.#setPeers(peers);
        }
        break;
      case "ack":
        for (const ack of msg.acks) {
          this.#unacked.delete(ack.opId);
          if (ack.seq) this.#seqs.add(ack.seq);
          if (ack.error) console.warn(`sync: the server rejected ${ack.opId}: ${ack.error}`);
        }
        break;
    }
  }

  /** Sends the latest cursor if joined and it changed (or always, if forced). */
  #sendCursor(force: boolean): void {
    const socket = this.#socket;
    if (!this.#cursor || !this.#joined || !socket || socket.readyState !== OPEN) return;
    const data = JSON.stringify({ type: "cursor", cursor: this.#cursor });
    if (!force && data === this.#sentCursor) return;
    this.#sentCursor = data;
    socket.send(data);
  }

  #forgetSilentPeers(timeout: number): void {
    const now = Date.now();
    const alive = new Map([...this.#peers].filter(([, p]) => now - p.seen < timeout));
    if (alive.size !== this.#peers.size) this.#setPeers(alive);
  }

  #setPeers(peers: Map<string, { peer: Peer; seen: number }>): void {
    const changed =
      peers.size !== this.#peers.size || [...peers].some(([r, p]) => this.#peers.get(r)?.peer !== p.peer);
    this.#peers = peers;
    if (changed) {
      const list = [...peers.values()].map((p) => p.peer);
      list.sort((a, b) => a.name.localeCompare(b.name) || a.replica.localeCompare(b.replica));
      this.#opts.onPeers?.(list);
    }
  }

  #sendOps(ops: WireOp[]): void {
    const socket = this.#socket;
    if (!socket || socket.readyState !== OPEN) return; // resent after the next join
    for (let i = 0; i < ops.length; i += MAX_OPS_PER_MESSAGE) {
      socket.send(JSON.stringify({ type: "ops", ops: ops.slice(i, i + MAX_OPS_PER_MESSAGE) }));
    }
  }

  #onClose(code: number, reason: string): void {
    this.#joined = false;
    this.#socket = null;
    // Offline, we cannot see who is still here; the next join lists them.
    this.#setPeers(new Map());
    if (this.#closed) return;
    if (FATAL_CLOSE_CODES.has(code)) {
      console.error(`sync: the gateway refused this connection (${code}): ${reason}`);
      this.#closed = true;
      this.#opts.onStatus?.("closed");
      return;
    }
    const { initialMs, maxMs } = this.#opts.backoff ?? { initialMs: 500, maxMs: 10_000 };
    const delay = Math.min(maxMs, initialMs * 2 ** this.#attempt) * (0.5 + Math.random() / 2);
    this.#attempt++;
    this.#opts.onStatus?.("reconnecting");
    this.#retry = setTimeout(() => {
      this.#retry = null;
      if (!this.#closed) this.#connect();
    }, delay);
  }
}
