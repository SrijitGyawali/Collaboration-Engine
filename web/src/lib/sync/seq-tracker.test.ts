import { expect, it } from "vitest";

import { SeqTracker } from "./seq-tracker";

it("advances through numbers that arrive in order", () => {
  const t = new SeqTracker();
  expect(t.last).toBe(0);
  for (const seq of [1, 2, 3]) t.add(seq);
  expect(t.last).toBe(3);
});

it("waits at a gap until it is filled", () => {
  const t = new SeqTracker();
  t.add(1);
  t.add(3);
  t.add(4);
  expect(t.last).toBe(1); // 2 is missing: a reconnect must ask for "after 1"

  t.add(2);
  expect(t.last).toBe(4);
});

it("ignores repeats and old numbers", () => {
  const t = new SeqTracker();
  for (const seq of [1, 2, 2, 1, 3, 3]) t.add(seq);
  expect(t.last).toBe(3);
});
