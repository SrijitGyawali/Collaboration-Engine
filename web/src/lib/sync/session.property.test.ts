// Randomized test of CollabSession: three sessions make random edits while
// the network randomly holds, reorders and releases their messages.

import { beforeAll, expect, it } from "vitest";

import type { CRDT } from "@/lib/crdt";
import { applyChange, type Change } from "@/lib/editor/change";
import { cpLength } from "@/lib/editor/text";
import { Hub } from "@/test/hub";
import { loadTestCRDT } from "@/test/load-crdt";
import { CollabSession } from "./session";

let crdt: CRDT;

beforeAll(async () => {
  crdt = await loadTestCRDT();
});

/** A small seeded generator (mulberry32), so a failing seed can be replayed. */
function rng(seed: number) {
  let s = seed >>> 0;
  return (n: number) => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return (((t ^ (t >>> 14)) >>> 0) % n);
  };
}

function randomChange(text: string, pick: (n: number) => number): Change {
  const len = cpLength(text);
  const pos = pick(len + 1);
  const deleteCount = pos < len && pick(3) === 0 ? 1 + pick(Math.min(3, len - pos)) : 0;
  const insert = deleteCount > 0 && pick(2) === 0 ? "" : ["a", "b", "👋", "xyz"][pick(4)];
  return { pos, deleteCount, insert };
}

it.each(Array.from({ length: 30 }, (_, seed) => seed))("random sessions converge (seed %i)", (seed) => {
  const pick = rng(seed);
  const hub = new Hub();
  const sessions = ["s0", "s1", "s2"].map((id) => new CollabSession(crdt, hub.connect(), id));

  // What each session's editor would show, maintained only from the
  // updates it is sent. Exact change lists must rebuild the new text.
  const shown = sessions.map(() => "");
  sessions.forEach((s, i) =>
    s.onRemoteUpdate((u) => {
      if (u.changes) {
        expect(u.changes.reduce(applyChange, shown[i])).toBe(u.text);
      }
      shown[i] = u.text;
    }),
  );

  for (let step = 0; step < 60; step++) {
    if (pick(4) === 0) hub.hold();
    const i = pick(sessions.length);
    const change = randomChange(sessions[i].text, pick);
    const before = sessions[i].text;
    sessions[i].applyLocal(change);
    // A local edit shows exactly the change the user made.
    expect(sessions[i].text).toBe(applyChange(before, change));
    shown[i] = sessions[i].text;
    if (pick(3) === 0) {
      hub.release((queue) => [...queue].sort(() => pick(3) - 1));
    }
  }
  hub.release((queue) => [...queue].reverse());

  const text = sessions[0].text;
  for (const [i, s] of sessions.entries()) {
    expect(s.text).toBe(text);
    expect(s.pendingCount).toBe(0);
    expect(shown[i]).toBe(text);
  }
});
