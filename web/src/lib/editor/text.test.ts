import { describe, expect, it } from "vitest";

import { cpLength, cpToUtf16, utf16ToCp } from "./text";

const text = "a👋b"; // UTF-16: a=0, 👋=1..2, b=3; code points: a=0, 👋=1, b=2

describe("cpLength", () => {
  it("counts code points, not UTF-16 units", () => {
    expect(cpLength("")).toBe(0);
    expect(cpLength("abc")).toBe(3);
    expect(text.length).toBe(4);
    expect(cpLength(text)).toBe(3);
    expect(cpLength("नमस्ते")).toBe(6);
  });
});

describe("utf16ToCp", () => {
  it("maps every UTF-16 index", () => {
    expect([0, 1, 3, 4].map((u) => utf16ToCp(text, u))).toEqual([0, 1, 2, 3]);
  });

  it("treats an index inside a surrogate pair as after the character", () => {
    expect(utf16ToCp(text, 2)).toBe(2);
  });

  it("clamps past the end", () => {
    expect(utf16ToCp(text, 99)).toBe(3);
  });
});

describe("cpToUtf16", () => {
  it("maps every code point index", () => {
    expect([0, 1, 2, 3].map((cp) => cpToUtf16(text, cp))).toEqual([0, 1, 3, 4]);
  });

  it("round-trips with utf16ToCp", () => {
    for (let cp = 0; cp <= cpLength(text); cp++) {
      expect(utf16ToCp(text, cpToUtf16(text, cp))).toBe(cp);
    }
  });

  it("clamps past the end", () => {
    expect(cpToUtf16(text, 99)).toBe(4);
  });
});
