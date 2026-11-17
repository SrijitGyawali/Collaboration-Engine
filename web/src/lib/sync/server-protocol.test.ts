import { describe, expect, it } from "vitest";

import { opKey, parseServerMessage } from "./server-protocol";

const op = { kind: "insert", id: { counter: 1, replica: "a" }, value: "x" };
const json = (v: unknown) => JSON.stringify(v);

describe("parseServerMessage", () => {
  it("parses the three message types", () => {
    expect(parseServerMessage(json({ type: "remote", ops: [{ seq: 3, op }] }))).toEqual({
      type: "remote",
      ops: [{ seq: 3, op }],
    });
    const you = { replica: "a", name: "Ann", color: "#000" };
    expect(parseServerMessage(json({ type: "joined", doc: "d", seq: 7, you }))).toEqual({
      type: "joined",
      doc: "d",
      seq: 7,
      you,
    });
    expect(parseServerMessage(json({ type: "ack", acks: [{ opId: "1@a", seq: 4 }] }))).toEqual({
      type: "ack",
      acks: [{ opId: "1@a", seq: 4 }],
    });
  });

  it("parses presence messages", () => {
    const ann = { replica: "a", name: "Ann", color: "#2f6fde" };
    const cursor = { anchor: { counter: 2, replica: "b" }, head: null };
    expect(parseServerMessage(json({ type: "peers", peers: [ann, { ...ann, replica: "c", cursor }] }))).toEqual({
      type: "peers",
      peers: [ann, { ...ann, replica: "c", cursor }],
    });
    // Nobody else there: the server leaves the list out.
    expect(parseServerMessage(json({ type: "peers" }))).toEqual({ type: "peers", peers: [] });
    expect(parseServerMessage(json({ type: "peer", peer: { ...ann, cursor } }))).toEqual({
      type: "peer",
      peer: { ...ann, cursor },
    });
    expect(parseServerMessage(json({ type: "left", replica: "a" }))).toEqual({ type: "left", replica: "a" });
  });

  it("rejects malformed presence", () => {
    const ann = { replica: "a", name: "Ann", color: "#2f6fde" };
    const bad = [
      { type: "peer", peer: { ...ann, color: "red;background:url(x)" } }, // only hex colours
      { type: "peer", peer: { ...ann, replica: "" } },
      { type: "peer", peer: { ...ann, cursor: { anchor: { counter: 0, replica: "b" }, head: null } } },
      { type: "peer", peer: { ...ann, cursor: "start" } },
      { type: "peers", peers: [ann, { name: "no replica" }] },
      { type: "left" },
    ];
    for (const msg of bad) expect(parseServerMessage(json(msg)), JSON.stringify(msg)).toBeNull();
  });

  it("treats an omitted joined seq as zero", () => {
    expect(parseServerMessage(json({ type: "joined", doc: "d" }))).toMatchObject({ seq: 0, you: null });
  });

  it.each([
    ["not a string", { type: "ack", acks: [] }],
    ["not JSON", "{oops"],
    ["unknown type", json({ type: "nope" })],
    ["remote without ops", json({ type: "remote" })],
    ["remote with a bad seq", json({ type: "remote", ops: [{ seq: 0, op }] })],
    ["remote with a fractional seq", json({ type: "remote", ops: [{ seq: 1.5, op }] })],
    ["joined without doc", json({ type: "joined", seq: 1 })],
    ["joined with a negative seq", json({ type: "joined", doc: "d", seq: -1 })],
    ["ack without opId", json({ type: "ack", acks: [{ seq: 1 }] })],
  ])("rejects %s", (_, raw) => {
    expect(parseServerMessage(raw)).toBeNull();
  });
});

it("formats op keys like the server", () => {
  expect(opKey({ counter: 42, replica: "alice" })).toBe("42@alice");
});
