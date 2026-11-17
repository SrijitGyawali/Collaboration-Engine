// The gateway's WebSocket protocol, client side. internal/gateway/protocol.go
// is the reference; these types and checks mirror it.
//
//   client → server   {"type":"join","doc","replica","name","lastSeq"}, then {"type":"ops","ops"}
//                     {"type":"cursor","cursor":{"anchor","head"}}   where my selection is
//   server → client   {"type":"remote","ops":[{"seq","op"}]}   history and others' edits
//                     {"type":"joined","doc","seq","you"}      catch-up done
//                     {"type":"ack","acks":[{"opId","seq","duplicate","error"}]}
//                     {"type":"peers","peers"}   who else is here, right after joined
//                     {"type":"peer","peer"}     someone joined or moved (and every heartbeat)
//                     {"type":"left","replica"}  someone closed the document

import type { WireID, WireOp } from "@/lib/crdt";

export interface SeqOp {
  seq: number;
  op: WireOp;
}

export interface ServerAck {
  opId: string;
  /** Absent when the op was rejected. */
  seq?: number;
  duplicate?: boolean;
  /** Why the op was rejected; it should be dropped. */
  error?: string;
}

/** Who the server says this connection is. */
export interface Identity {
  replica: string;
  name: string;
  color: string;
}

/**
 * A selection, held by characters rather than indexes so it stays put while
 * others edit: each end is the ID of the character just before it, or null
 * at the start of the document. anchor is the end that stays still while
 * selecting, head the one that moves (the caret); equal for a plain caret.
 */
export interface Cursor {
  anchor: WireID | null;
  head: WireID | null;
}

/** Another editor with the document open. */
export interface Peer extends Identity {
  /** Absent until they have placed their caret. */
  cursor?: Cursor;
}

export type ServerMessage =
  | { type: "remote"; ops: SeqOp[] }
  | { type: "joined"; doc: string; seq: number; you: Identity | null }
  | { type: "ack"; acks: ServerAck[] }
  | { type: "peers"; peers: Peer[] }
  | { type: "peer"; peer: Peer }
  | { type: "left"; replica: string };

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;
const isSeq = (v: unknown): v is number => typeof v === "number" && Number.isInteger(v) && v > 0;

const isID = (v: unknown): v is WireID =>
  isObject(v) && isSeq(v.counter) && typeof v.replica === "string" && v.replica !== "";

/** Colours end up in style attributes: only plain hex colours are accepted. */
const isColor = (v: unknown): v is string => typeof v === "string" && /^#[0-9a-f]{6}$/i.test(v);

function parsePeer(v: unknown): Peer | null {
  if (!isObject(v) || typeof v.replica !== "string" || v.replica === "" || typeof v.name !== "string" || !isColor(v.color)) {
    return null;
  }
  const peer: Peer = { replica: v.replica, name: v.name, color: v.color };
  if (v.cursor !== undefined) {
    const c = v.cursor;
    const end = (e: unknown) => e === null || e === undefined || isID(e);
    if (!isObject(c) || !end(c.anchor) || !end(c.head)) return null;
    peer.cursor = { anchor: (c.anchor as WireID | undefined) ?? null, head: (c.head as WireID | undefined) ?? null };
  }
  return peer;
}

/** The op ID ("counter@replica") the server uses in acks. */
export const opKey = (id: WireID): string => `${id.counter}@${id.replica}`;

/**
 * Parses a message from the gateway. Anything malformed returns null: the
 * transport ignores it rather than letting it break the editor. Each op is
 * validated again by the CRDT when applied.
 */
export function parseServerMessage(raw: unknown): ServerMessage | null {
  if (typeof raw !== "string") return null;
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!isObject(data)) return null;

  switch (data.type) {
    case "remote": {
      if (!Array.isArray(data.ops)) return null;
      const ops = data.ops.filter((o): o is SeqOp => isObject(o) && isSeq(o.seq) && isObject(o.op));
      return ops.length === data.ops.length ? { type: "remote", ops } : null;
    }
    case "joined": {
      if (typeof data.doc !== "string") return null;
      const seq = data.seq ?? 0; // the server omits a zero seq
      if (seq !== 0 && !isSeq(seq)) return null;
      const you = isObject(data.you) ? (data.you as unknown as Identity) : null;
      return { type: "joined", doc: data.doc, seq: seq as number, you };
    }
    case "ack": {
      if (!Array.isArray(data.acks)) return null;
      const acks = data.acks.filter((a): a is ServerAck => isObject(a) && typeof a.opId === "string");
      return acks.length === data.acks.length ? { type: "ack", acks } : null;
    }
    case "peers": {
      // The server omits the list when nobody else is there.
      const raw = data.peers ?? [];
      if (!Array.isArray(raw)) return null;
      const peers = raw.map(parsePeer);
      return peers.every((p) => p !== null) ? { type: "peers", peers: peers as Peer[] } : null;
    }
    case "peer": {
      const peer = parsePeer(data.peer);
      return peer ? { type: "peer", peer } : null;
    }
    case "left":
      return typeof data.replica === "string" && data.replica !== "" ? { type: "left", replica: data.replica } : null;
    default:
      return null;
  }
}
