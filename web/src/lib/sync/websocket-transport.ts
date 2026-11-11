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

import type { WireOp } from "@/lib/crdt";
import type { SyncMessage } from "./messages";
import { SeqTracker } from "./seq-tracker";
import { opKey, parseServerMessage, type Identity } from "./server-protocol";
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
}

/** The server accepts up to 1000 ops per message; stay well below. */
const MAX_OPS_PER_MESSAGE = 500;
const OPEN = 1;

/** Close codes after which retrying cannot help: the server refused us. */
const FATAL_CLOSE_CODES = new Set([1003 /* unsupported data */, 1008 /* policy violation */]);

export function websocketTransport(options: WebSocketTransportOptions): Transport {
  return new WebSocketTransport(options);
}

class WebSocketTransport implements Transport {
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

  constructor(opts: WebSocketTransportOptions) {
    this.#opts = opts;
    this.#connect();
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
