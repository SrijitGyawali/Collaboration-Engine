// The messages tabs exchange to keep one document in sync.
//
//   ops    "Here are edits I just made."             sent to everyone
//   hello  "I just opened this document."           sent by a new tab
//   state  "Here is every op I know of." (to hello) sent to the new tab
//
// A new tab cannot know what it missed, so instead of asking for "the ops
// after X" it gets everything and lets the CRDT discard what it already has
// (re-applied ops are harmless duplicates). Every open tab answers; that only
// produces more duplicates, never a wrong document.
//
// The server protocol (week 2) will use the same op format, with sequence
// numbers instead of full state.

import type { WireOp } from "@/lib/crdt";

export type SyncMessage =
  | { type: "ops"; from: string; ops: WireOp[] }
  | { type: "hello"; from: string }
  | { type: "state"; from: string; to: string; ops: WireOp[] };

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;

/**
 * Checks that data received from a channel has the shape of a SyncMessage.
 * Anything on the page can post to a BroadcastChannel, so nothing is
 * assumed. Only the envelope is checked here; each op is validated by the
 * CRDT when it is applied.
 */
export function parseMessage(data: unknown): SyncMessage | null {
  if (!isObject(data) || typeof data.from !== "string" || data.from === "") {
    return null;
  }
  switch (data.type) {
    case "hello":
      return { type: "hello", from: data.from };
    case "ops":
      return Array.isArray(data.ops) ? { type: "ops", from: data.from, ops: data.ops as WireOp[] } : null;
    case "state":
      return typeof data.to === "string" && Array.isArray(data.ops)
        ? { type: "state", from: data.from, to: data.to, ops: data.ops as WireOp[] }
        : null;
    default:
      return null;
  }
}
