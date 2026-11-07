import { describe, expect, it } from "vitest";

import { transformPosition, transformSelection } from "./selection";

describe("transformPosition", () => {
  // Text "hello world", caret at 6 (before "world").
  const caret = 6;

  it.each([
    ["insert before the caret pushes it right", { pos: 0, deleteCount: 0, insert: ">> " }, 9],
    ["insert after the caret leaves it", { pos: 8, deleteCount: 0, insert: "x" }, 6],
    ["insert at the caret leaves it (remote text goes after it)", { pos: 6, deleteCount: 0, insert: "x" }, 6],
    ["delete before the caret pulls it left", { pos: 0, deleteCount: 2, insert: "" }, 4],
    ["delete after the caret leaves it", { pos: 6, deleteCount: 5, insert: "" }, 6],
    ["delete around the caret moves it to the deletion", { pos: 4, deleteCount: 4, insert: "" }, 4],
    // The deletion moves the caret to 4; the insertion is then exactly at the
    // caret, so the same rule as above keeps the caret in front of it.
    ["replace around the caret puts it before the new text", { pos: 4, deleteCount: 4, insert: "XY" }, 4],
    ["emoji counts as one character", { pos: 0, deleteCount: 0, insert: "👋" }, 7],
  ])("%s", (_, change, want) => {
    expect(transformPosition(caret, change)).toBe(want);
  });
});

describe("transformSelection", () => {
  it("moves both ends independently", () => {
    // Selection "llo w" in "hello world" (2..7); remote inserts "X" at 4.
    expect(transformSelection({ start: 2, end: 7 }, { pos: 4, deleteCount: 0, insert: "X" })).toEqual({
      start: 2,
      end: 8,
    });
  });

  it("collapses a selection whose text was deleted remotely", () => {
    expect(transformSelection({ start: 2, end: 5 }, { pos: 1, deleteCount: 6, insert: "" })).toEqual({
      start: 1,
      end: 1,
    });
  });
});
