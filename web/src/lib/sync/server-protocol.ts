// The gateway's WebSocket protocol, client side. internal/gateway/protocol.go
// is the reference; these types and checks mirror it.
//
//   client → server   {"type":"join","doc","replica","name","lastSeq"}, then {"type":"ops","ops"}
//   server → client   {"type":"remote","ops":[{"seq","op"}]}   history and others' edits
//                     {"type":"joined","doc","seq","you"}      catch-up done
//                     {"type":"ack","acks":[{"opId","seq","duplicate","error"}]}

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

export type ServerMessage =
  | { type: "remote"; ops: SeqOp[] }
  | { type: "joined"; doc: string; seq: number; you: Identity | null }
  | { type: "ack"; acks: ServerAck[] };

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;
const isSeq = (v: unknown): v is number => typeof v === "number" && Number.isInteger(v) && v > 0;

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
    default:
      return null;
  }
}
