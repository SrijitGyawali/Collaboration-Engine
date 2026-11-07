// CollabSession over a real BroadcastChannel. Node has the same
// BroadcastChannel as browsers, delivering asynchronously, so this checks
// the actual transport rather than the in-memory Hub.

import { afterEach, beforeAll, expect, it } from "vitest";

import type { CRDT } from "@/lib/crdt";
import { loadTestCRDT } from "@/test/load-crdt";
import { CollabSession } from "./session";
import { broadcastChannelTransport } from "./transport";

let crdt: CRDT;
const open: CollabSession[] = [];

beforeAll(async () => {
  crdt = await loadTestCRDT();
});

afterEach(() => {
  // Open channels would keep the test process alive.
  for (const s of open.splice(0)) s.close();
});

function session(docId: string, replica: string): CollabSession {
  const s = new CollabSession(crdt, broadcastChannelTransport(docId), replica);
  open.push(s);
  return s;
}

/** Waits until check passes (messages arrive asynchronously), up to 2 s. */
async function eventually(check: () => void): Promise<void> {
  const deadline = Date.now() + 2000;
  for (;;) {
    try {
      check();
      return;
    } catch (err) {
      if (Date.now() > deadline) throw err;
      await new Promise((r) => setTimeout(r, 10));
    }
  }
}

it("syncs edits between sessions on the same document", async () => {
  const a = session("doc-1", "a");
  const b = session("doc-1", "b");

  a.applyLocal({ pos: 0, deleteCount: 0, insert: "hello" });
  await eventually(() => expect(b.text).toBe("hello"));

  b.applyLocal({ pos: 5, deleteCount: 0, insert: " world" });
  await eventually(() => expect(a.text).toBe("hello world"));
});

it("catches up a session opened later", async () => {
  const a = session("doc-2", "a");
  a.applyLocal({ pos: 0, deleteCount: 0, insert: "already here" });

  const late = session("doc-2", "late");
  await eventually(() => expect(late.text).toBe("already here"));
});

it("keeps different documents apart", async () => {
  const a = session("doc-3", "a");
  const other = session("doc-4", "b");
  const same = session("doc-3", "c");

  a.applyLocal({ pos: 0, deleteCount: 0, insert: "only doc-3" });
  await eventually(() => expect(same.text).toBe("only doc-3"));
  expect(other.text).toBe("");
});
