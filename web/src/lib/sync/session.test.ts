import { beforeAll, describe, expect, it, vi } from "vitest";

import type { CRDT } from "@/lib/crdt";
import { applyChange, type Change } from "@/lib/editor/change";
import { Hub } from "@/test/hub";
import { loadTestCRDT } from "@/test/load-crdt";
import { CollabSession, type RemoteUpdate } from "./session";

let crdt: CRDT;

beforeAll(async () => {
  crdt = await loadTestCRDT();
});

/** Two sessions on one hub, with every remote update recorded. */
function pair() {
  const hub = new Hub();
  const a = new CollabSession(crdt, hub.connect(), "a");
  const b = new CollabSession(crdt, hub.connect(), "b");
  const updates: RemoteUpdate[] = [];
  b.onRemoteUpdate((u) => updates.push(u));
  return { hub, a, b, updates };
}

const insert = (pos: number, text: string): Change => ({ pos, deleteCount: 0, insert: text });
const remove = (pos: number, count: number): Change => ({ pos, deleteCount: count, insert: "" });

describe("CollabSession", () => {
  it("sends local edits to the other session", () => {
    const { a, b, updates } = pair();
    a.applyLocal(insert(0, "hello"));
    a.applyLocal(remove(1, 3));

    expect(a.text).toBe("ho");
    expect(b.text).toBe("ho");
    expect(updates.map((u) => u.changes)).toEqual([
      [insert(0, "h"), insert(1, "e"), insert(2, "l"), insert(3, "l"), insert(4, "o")],
      [remove(1, 1), remove(1, 1), remove(1, 1)],
    ]);
  });

  it("reports changes that rebuild the new text from the old one", () => {
    const { a, b, updates } = pair();
    let shown = "";
    b.onRemoteUpdate((u) => {
      shown = u.changes!.reduce(applyChange, shown);
      expect(shown).toBe(u.text);
    });
    a.applyLocal(insert(0, "hello world"));
    a.applyLocal({ pos: 6, deleteCount: 5, insert: "there" });
    a.applyLocal(insert(0, "👋 "));

    expect(shown).toBe("👋 hello there");
    expect(updates).toHaveLength(3);
  });

  it("converges on concurrent edits", () => {
    const { hub, a, b } = pair();
    a.applyLocal(insert(0, "Hi"));

    hub.hold();
    a.applyLocal(insert(1, "X"));
    b.applyLocal(insert(1, "Y"));
    hub.release();

    expect(a.text).toBe("HYXi");
    expect(b.text).toBe("HYXi");
  });

  it("reports no change for deleting a character that is already gone", () => {
    const { hub, a, b, updates } = pair();
    a.applyLocal(insert(0, "abc"));
    updates.length = 0;

    hub.hold();
    a.applyLocal(remove(1, 1));
    b.applyLocal(remove(1, 1)); // both delete "b"
    hub.release();

    expect(b.text).toBe("ac");
    // b's own delete already hid "b"; a's delete of it changes nothing visible.
    expect(updates).toEqual([{ text: "ac", changes: [] }]);
  });

  it("falls back to null changes when ops arrive out of order", () => {
    const { hub, a, b, updates } = pair();
    hub.hold();
    a.applyLocal(insert(0, "a"));
    a.applyLocal(insert(1, "b"));
    hub.release((queue) => [...queue].reverse());

    expect(b.text).toBe("ab");
    expect(b.pendingCount).toBe(0);
    // "b" had to wait for "a", and "a" released it: positions are not exact.
    expect(updates.map((u) => u.changes)).toEqual([null, null]);
  });

  it("skips a malformed op without losing the rest of the batch", () => {
    const hub = new Hub();
    const b = new CollabSession(crdt, hub.connect(), "b");
    const attacker = hub.connect();
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});

    attacker.send({
      type: "ops",
      from: "x",
      ops: [
        { kind: "insert", id: { counter: 1, replica: "x" }, value: "too long" },
        { kind: "insert", id: { counter: 2, replica: "x" }, value: "!" },
      ],
    });

    expect(b.text).toBe("!");
    expect(warn).toHaveBeenCalledOnce();
    warn.mockRestore();
  });

  it("ignores invalid messages and its own replica name", () => {
    const hub = new Hub();
    const b = new CollabSession(crdt, hub.connect(), "b");
    const other = hub.connect();

    other.send({ type: "ops", from: "b", ops: [{ kind: "insert", id: { counter: 1, replica: "b" }, value: "x" }] });
    other.send({ nonsense: true } as never);

    expect(b.text).toBe("");
  });

  it("keeps a log of every op, once", () => {
    const { hub, a, b } = pair();
    a.applyLocal(insert(0, "ab"));
    hub.hold();
    b.applyLocal(remove(0, 1));
    hub.release();

    expect(a.log).toHaveLength(3);
    expect(b.log).toHaveLength(3);
  });

  it("catches a newly opened session up with the existing text", () => {
    const hub = new Hub();
    const a = new CollabSession(crdt, hub.connect(), "a");
    a.applyLocal(insert(0, "hello world"));
    a.applyLocal(remove(0, 6));

    const late = new CollabSession(crdt, hub.connect(), "late");

    expect(late.text).toBe("world");
    expect(late.log).toHaveLength(a.log.length);
  });

  it("catches up from several open sessions without duplicating text", () => {
    const hub = new Hub();
    const a = new CollabSession(crdt, hub.connect(), "a");
    const b = new CollabSession(crdt, hub.connect(), "b");
    a.applyLocal(insert(0, "ab"));
    b.applyLocal(insert(2, "cd"));

    // Both a and b answer the hello; the second answer is all duplicates.
    const late = new CollabSession(crdt, hub.connect(), "late");
    const third = new CollabSession(crdt, hub.connect(), "third");

    expect(late.text).toBe("abcd");
    expect(third.text).toBe("abcd");
  });

  it("only applies catch-up answers addressed to it", () => {
    const hub = new Hub();
    const b = new CollabSession(crdt, hub.connect(), "b");
    const other = hub.connect();
    const op = { kind: "insert", id: { counter: 1, replica: "x" }, value: "x" } as const;

    other.send({ type: "state", from: "x", to: "someone-else", ops: [op] });
    expect(b.text).toBe("");

    other.send({ type: "state", from: "x", to: "b", ops: [op] });
    expect(b.text).toBe("x");
  });

  it("answers a hello only when it has something to send", () => {
    const hub = new Hub();
    const listener = hub.connect();
    const received: unknown[] = [];
    listener.onMessage((m) => received.push(m));

    const a = new CollabSession(crdt, hub.connect(), "a");
    listener.send({ type: "hello", from: "newcomer" });
    expect(received.filter((m) => (m as { type: string }).type === "state")).toHaveLength(0);

    a.applyLocal(insert(0, "x"));
    listener.send({ type: "hello", from: "newcomer" });
    expect(received).toContainEqual(expect.objectContaining({ type: "state", from: "a", to: "newcomer" }));
  });

  it("cannot be used after close", () => {
    const { a } = pair();
    a.close();
    expect(() => a.applyLocal(insert(0, "x"))).toThrow(/closed/);
  });
});
