import { describe, expect, it } from "vitest";

import { parseMessage } from "./messages";

const op = { kind: "insert", id: { counter: 1, replica: "a" }, value: "x" };

describe("parseMessage", () => {
  it.each([
    [{ type: "hello", from: "a" }],
    [{ type: "ops", from: "a", ops: [op] }],
    [{ type: "state", from: "a", to: "b", ops: [] }],
  ])("accepts %j", (msg) => {
    expect(parseMessage(msg)).toEqual(msg);
  });

  it("drops fields that are not part of the message", () => {
    expect(parseMessage({ type: "hello", from: "a", extra: 1 })).toEqual({ type: "hello", from: "a" });
  });

  it.each([
    [null],
    ["hello"],
    [{ type: "hello" }],
    [{ type: "hello", from: "" }],
    [{ type: "ops", from: "a" }],
    [{ type: "ops", from: "a", ops: "x" }],
    [{ type: "state", from: "a", ops: [] }],
    [{ type: "move", from: "a" }],
  ])("rejects %j", (msg) => {
    expect(parseMessage(msg)).toBeNull();
  });
});
