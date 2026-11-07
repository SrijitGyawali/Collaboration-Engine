import { describe, expect, it } from "vitest";

import { applyChange, diffText, lengthDelta } from "./change";

describe("diffText", () => {
  it.each([
    ["typing at the end", "hell", "hello", 5, { pos: 4, deleteCount: 0, insert: "o" }],
    ["typing at the start", "ello", "hello", 1, { pos: 0, deleteCount: 0, insert: "h" }],
    ["typing in the middle", "helo", "hello", 3, { pos: 2, deleteCount: 0, insert: "l" }],
    ["backspace", "hello", "helo", 2, { pos: 2, deleteCount: 1, insert: "" }],
    ["paste", "hd", "hello world", 10, { pos: 1, deleteCount: 0, insert: "ello worl" }],
    ["typing over a selection", "hello world", "hello there", 11, { pos: 6, deleteCount: 5, insert: "there" }],
    ["deleting everything", "abc", "", 0, { pos: 0, deleteCount: 3, insert: "" }],
    ["emoji", "ab", "a👋b", 2, { pos: 1, deleteCount: 0, insert: "👋" }],
  ])("%s", (_, before, after, cursor, want) => {
    expect(diffText(before, after, cursor)).toEqual(want);
    expect(applyChange(before, want)).toBe(after);
  });

  it("returns null when nothing changed", () => {
    expect(diffText("same", "same", 2)).toBeNull();
  });

  // Typing "a" into "a|a" gives "aaa" whichever "a" is new. The caret says
  // which: here it is after the first "a" typed, at position 2.
  it("uses the cursor to resolve repeated characters", () => {
    expect(diffText("aa", "aaa", 2)).toEqual({ pos: 1, deleteCount: 0, insert: "a" });
    expect(diffText("aa", "aaa", 1)).toEqual({ pos: 0, deleteCount: 0, insert: "a" });
    // Without a cursor the edit is placed as early as possible.
    expect(diffText("aa", "aaa")).toEqual({ pos: 0, deleteCount: 0, insert: "a" });
  });

  it("resolves a repeated-character backspace with the cursor", () => {
    // "aa|a" + Backspace -> "a|a": the second "a" went.
    expect(diffText("aaa", "aa", 1)).toEqual({ pos: 1, deleteCount: 1, insert: "" });
  });
});

describe("lengthDelta", () => {
  it("counts code points", () => {
    expect(lengthDelta({ pos: 0, deleteCount: 1, insert: "👋👋" })).toBe(1);
  });
});
