import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { WireOp } from "@/lib/crdt";
import { websocketTransport, type ConnectionStatus, type SocketLike } from "./websocket-transport";

/** A WebSocket the test drives by hand. */
class FakeSocket implements SocketLike {
  readyState = 0;
  sent: Record<string, unknown>[] = [];
  onopen: SocketLike["onopen"] = null;
  onmessage: SocketLike["onmessage"] = null;
  onclose: SocketLike["onclose"] = null;
  onerror: SocketLike["onerror"] = null;

  send(data: string): void {
    this.sent.push(JSON.parse(data) as Record<string, unknown>);
  }
  close(code = 1000, reason = ""): void {
    this.drop(code, reason);
  }
  // Test controls:
  open(): void {
    this.readyState = 1;
    this.onopen?.({});
  }
  receive(msg: unknown): void {
    this.onmessage?.({ data: JSON.stringify(msg) });
  }
  drop(code = 1006, reason = ""): void {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.onclose?.({ code, reason });
  }
  sentOfType(type: string) {
    return this.sent.filter((m) => m.type === type);
  }
}

const op = (counter: number, replica = "me"): WireOp => ({ kind: "insert", id: { counter, replica }, value: "x" });

function setup() {
  const sockets: FakeSocket[] = [];
  const statuses: ConnectionStatus[] = [];
  const received: unknown[] = [];
  const transport = websocketTransport({
    url: "ws://test/ws",
    docId: "doc",
    replica: "me",
    name: "Me",
    onStatus: (s) => statuses.push(s),
    createSocket: () => {
      const s = new FakeSocket();
      sockets.push(s);
      return s;
    },
    backoff: { initialMs: 100, maxMs: 1000 },
  });
  transport.onMessage((m) => received.push(m));
  const current = () => sockets.at(-1)!;
  const joinCurrent = (seq = 0) => {
    current().open();
    current().receive({ type: "joined", doc: "doc", seq, you: { replica: "me", name: "Me", color: "#000" } });
  };
  return { transport, sockets, statuses, received, current, joinCurrent };
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.spyOn(Math, "random").mockReturnValue(1); // no jitter: delays are exact
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("websocketTransport", () => {
  it("joins with the document, replica, name and last seq", () => {
    const { current } = setup();
    current().open();
    expect(current().sent[0]).toEqual({ type: "join", doc: "doc", replica: "me", name: "Me", lastSeq: 0 });
  });

  it("queues ops until joined, then sends them", () => {
    const { transport, current, joinCurrent } = setup();
    transport.send({ type: "ops", from: "me", ops: [op(1)] });
    current().open();
    expect(current().sentOfType("ops")).toHaveLength(0);

    joinCurrent();
    expect(current().sentOfType("ops")).toEqual([{ type: "ops", ops: [op(1)] }]);
  });

  it("hands remote ops to the session as an ops message", () => {
    const { received, current, joinCurrent } = setup();
    joinCurrent();
    current().receive({ type: "remote", ops: [{ seq: 1, op: op(1, "bob") }] });
    expect(received).toEqual([{ type: "ops", from: "server", ops: [op(1, "bob")] }]);
  });

  it("ignores session messages meant for other tabs", () => {
    const { transport, current, joinCurrent } = setup();
    joinCurrent();
    transport.send({ type: "hello", from: "me" });
    expect(current().sent.map((m) => m.type)).toEqual(["join"]);
  });

  // The heart of it: nothing typed is lost when the connection drops.
  it("resends unacknowledged ops after reconnecting, from the last seq", () => {
    const { transport, sockets, current, joinCurrent } = setup();
    joinCurrent();
    current().receive({ type: "remote", ops: [{ seq: 1, op: op(1, "bob") }, { seq: 2, op: op(2, "bob") }] });
    transport.send({ type: "ops", from: "me", ops: [op(1), op(2)] });
    current().receive({ type: "ack", acks: [{ opId: "1@me", seq: 3 }] }); // op 2 never acked

    current().drop();
    transport.send({ type: "ops", from: "me", ops: [op(3)] }); // typed while offline
    vi.advanceTimersByTime(100);
    expect(sockets).toHaveLength(2);

    current().open();
    expect(current().sent[0]).toMatchObject({ type: "join", lastSeq: 3 });
    current().receive({ type: "joined", doc: "doc", seq: 3 });
    expect(current().sentOfType("ops")).toEqual([{ type: "ops", ops: [op(2), op(3)] }]);
  });

  it("asks for everything after a gap in sequence numbers", () => {
    const { current, joinCurrent } = setup();
    joinCurrent();
    current().receive({ type: "remote", ops: [{ seq: 1, op: op(1, "a") }, { seq: 3, op: op(1, "b") }] });

    current().drop();
    vi.advanceTimersByTime(100);
    current().open();
    expect(current().sent[0]).toMatchObject({ lastSeq: 1 }); // seq 2 is missing
  });

  it("drops rejected ops instead of resending them forever", () => {
    const { transport, current, joinCurrent } = setup();
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    joinCurrent();
    transport.send({ type: "ops", from: "me", ops: [op(1)] });
    current().receive({ type: "ack", acks: [{ opId: "1@me", error: "malformed" }] });

    current().drop();
    vi.advanceTimersByTime(100);
    joinCurrent();
    expect(current().sentOfType("ops")).toHaveLength(0);
    expect(warn).toHaveBeenCalledOnce();
  });

  it("backs off between attempts and resets after a successful join", () => {
    const { sockets, current, joinCurrent, statuses } = setup();
    current().drop();
    vi.advanceTimersByTime(99);
    expect(sockets).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(sockets).toHaveLength(2); // after 100 ms

    current().drop();
    vi.advanceTimersByTime(199);
    expect(sockets).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(sockets).toHaveLength(3); // then 200 ms

    joinCurrent();
    current().drop();
    vi.advanceTimersByTime(100);
    expect(sockets).toHaveLength(4); // back to 100 ms after a good connection
    expect(statuses).toContain("connected");
    expect(statuses).toContain("reconnecting");
  });

  it("stops retrying when the server refuses the connection", () => {
    const { sockets, current, statuses } = setup();
    vi.spyOn(console, "error").mockImplementation(() => {});
    current().open();
    current().drop(1008, "replica must be ...");
    vi.advanceTimersByTime(60_000);
    expect(sockets).toHaveLength(1);
    expect(statuses.at(-1)).toBe("closed");
  });

  it("splits large batches to stay under the server's limit", () => {
    const { transport, current, joinCurrent } = setup();
    joinCurrent();
    transport.send({ type: "ops", from: "me", ops: Array.from({ length: 1200 }, (_, i) => op(i + 1)) });
    expect(current().sentOfType("ops").map((m) => (m.ops as unknown[]).length)).toEqual([500, 500, 200]);
  });

  it("does nothing after close", () => {
    const { transport, sockets, current, joinCurrent } = setup();
    joinCurrent();
    transport.close();
    vi.advanceTimersByTime(60_000);
    transport.send({ type: "ops", from: "me", ops: [op(1)] });
    expect(sockets).toHaveLength(1);
    expect(current().sentOfType("ops")).toHaveLength(0);
  });
});
