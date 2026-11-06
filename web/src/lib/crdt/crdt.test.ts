// Tests the browser CRDT end to end: TypeScript wrapper -> WebAssembly bridge
// -> Go CRDT and back, running the real module in Node.
//
// Needs `make web-wasm` first, which puts crdt.wasm and wasm_exec.js in
// web/public.

import { existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { beforeAll, describe, expect, it } from "vitest";

import { CRDTError, loadCRDT, type CRDT, type WireOp } from "./index";

const publicFile = (name: string) => fileURLToPath(new URL(`../../../public/${name}`, import.meta.url));

let crdt: CRDT;

beforeAll(async () => {
  const wasm = publicFile("crdt.wasm");
  const exec = publicFile("wasm_exec.js");
  if (!existsSync(wasm) || !existsSync(exec)) {
    throw new Error("crdt.wasm or wasm_exec.js missing in web/public: run `make web-wasm` from the repo root");
  }
  createRequire(import.meta.url)(exec); // defines globalThis.Go
  crdt = await loadCRDT({ wasmBytes: readFileSync(wasm) });
});

/** Sends ops through JSON, as the network will. */
const overTheWire = (ops: WireOp[]): WireOp[] => JSON.parse(JSON.stringify(ops)) as WireOp[];

describe("local edits", () => {
  it("inserts and deletes by position", () => {
    const doc = crdt.newDocument("alice");
    doc.insertText(0, "hello world");
    doc.deleteRange(5, 6);
    doc.insertAt(5, "!");

    expect(doc.text()).toBe("hello!");
    expect(doc.length).toBe(6);
  });

  it("counts characters, not UTF-16 units", () => {
    const doc = crdt.newDocument("alice");
    doc.insertText(0, "hi 👋");

    expect(doc.length).toBe(4); // "hi 👋".length would be 5
    doc.deleteAt(3);
    expect(doc.text()).toBe("hi ");
  });

  it("returns ops in the wire format", () => {
    const doc = crdt.newDocument("alice");
    const [a, b] = doc.insertText(0, "ab");

    expect(a).toEqual({ kind: "insert", id: { counter: 1, replica: "alice" }, value: "a" });
    expect(b).toEqual({
      kind: "insert",
      id: { counter: 2, replica: "alice" },
      after: { counter: 1, replica: "alice" },
      value: "b",
    });
    expect(doc.deleteAt(0)).toEqual({
      kind: "delete",
      id: { counter: 3, replica: "alice" },
      target: { counter: 1, replica: "alice" },
    });
  });
});

describe("remote edits", () => {
  it("converges on concurrent inserts at the same spot", () => {
    const alice = crdt.newDocument("alice");
    const bob = crdt.newDocument("bob");
    for (const op of overTheWire(alice.insertText(0, "Hi"))) bob.apply(op);

    const x = alice.insertText(1, "X");
    const y = bob.insertText(1, "Y");
    for (const op of overTheWire(y)) alice.apply(op);
    for (const op of overTheWire(x)) bob.apply(op);

    expect(alice.text()).toBe("HYXi");
    expect(bob.text()).toBe("HYXi");
  });

  it("holds back ops that arrive early and reports duplicates", () => {
    const alice = crdt.newDocument("alice");
    const [h, i] = overTheWire(alice.insertText(0, "hi"));
    const bob = crdt.newDocument("bob");

    expect(bob.apply(i)).toBe("pending");
    expect(bob.pendingCount).toBe(1);
    expect(bob.apply(h)).toBe("applied");
    expect(bob.apply(h)).toBe("duplicate");
    expect(bob.text()).toBe("hi");
    expect(bob.positionOf(i.id)).toEqual({ pos: 1, visible: true });
  });

  it("reports where a deleted character was", () => {
    const alice = crdt.newDocument("alice");
    const ops = alice.insertText(0, "cart");
    const del = alice.deleteAt(2);

    const bob = crdt.newDocument("bob");
    for (const op of overTheWire([...ops, del])) bob.apply(op);

    expect(bob.text()).toBe("cat");
    expect(bob.positionOf(ops[2].id)).toEqual({ pos: 2, visible: false });
  });
});

describe("errors", () => {
  it("throws CRDTError for invalid edits", () => {
    const doc = crdt.newDocument("alice");
    doc.insertText(0, "abc");

    expect(() => doc.insertText(9, "x")).toThrow(CRDTError);
    expect(() => doc.deleteRange(2, 5)).toThrow(/out of range/);
    expect(() => doc.insertAt(0, "xy")).toThrow(/exactly one character/);
    expect(() => crdt.newDocument("")).toThrow(/must not be empty/);
    expect(doc.text()).toBe("abc");
  });

  it("rejects malformed ops from the network", () => {
    const doc = crdt.newDocument("alice");
    const bad = [
      { kind: "move", id: { counter: 1, replica: "z" } },
      { kind: "insert", id: { counter: 1.5, replica: "z" }, value: "a" },
      { kind: "insert", id: { counter: 1, replica: "z" }, value: "ab" },
      { kind: "delete", id: { counter: 1, replica: "z" } },
      "not an op",
    ];
    for (const op of bad) {
      expect(() => doc.apply(op as unknown as WireOp)).toThrow(CRDTError);
    }
    expect(doc.text()).toBe("");
  });

  it("refuses to use a freed document", () => {
    const doc = crdt.newDocument("alice");
    doc.free();
    expect(() => doc.text()).toThrow(/has been freed/);
  });
});

it("loads the module only once", async () => {
  const again = await loadCRDT();
  expect(again.newDocument("carol").text()).toBe("");
});
