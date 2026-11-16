// End to end against a running stack: real WebSockets, the real servers,
// real storage. Skipped unless GATEWAY_URL is set, for example:
//
//   make stack
//   GATEWAY_URL=ws://localhost:8080/ws npx vitest run gateway.e2e
//
// GATEWAY_URL is where browsers connect (the proxy). To also test editing
// across gateways, list two gateways' own addresses in GATEWAY_URLS:
//
//   GATEWAY_URLS=ws://localhost:8091/ws,ws://localhost:8092/ws

import { afterEach, beforeAll, describe, expect, it } from "vitest";

import type { CRDT } from "@/lib/crdt";
import { loadTestCRDT } from "@/test/load-crdt";
import { CollabSession, randomReplicaId } from "./session";
import { websocketTransport, type ConnectionStatus, type SocketLike } from "./websocket-transport";

const url = process.env.GATEWAY_URL;
const gateways = (process.env.GATEWAY_URLS ?? "")
  .split(",")
  .map((u) => u.trim())
  .filter(Boolean);
let crdt: CRDT;
const open: CollabSession[] = [];

beforeAll(async () => {
  if (url || gateways.length > 0) crdt = await loadTestCRDT();
});

afterEach(() => {
  for (const s of open.splice(0)) s.close();
});

function connect(doc: string, gateway = url!) {
  const replica = randomReplicaId();
  const statuses: ConnectionStatus[] = [];
  const sockets: SocketLike[] = [];
  const transport = websocketTransport({
    url: gateway,
    docId: doc,
    replica,
    onStatus: (s) => statuses.push(s),
    createSocket: (u) => {
      const s = new WebSocket(u) as unknown as SocketLike;
      sockets.push(s);
      return s;
    },
    backoff: { initialMs: 50, maxMs: 200 },
  });
  const session = new CollabSession(crdt, transport, replica);
  open.push(session);
  return { session, statuses, sockets };
}

async function eventually(check: () => void, ms = 5000): Promise<void> {
  const deadline = Date.now() + ms;
  for (;;) {
    try {
      check();
      return;
    } catch (err) {
      if (Date.now() > deadline) throw err;
      await new Promise((r) => setTimeout(r, 20));
    }
  }
}

const insert = (pos: number, text: string) => ({ pos, deleteCount: 0, insert: text });

describe.skipIf(!url)("through a running gateway", () => {
  it("syncs two clients and catches up a third", async () => {
    const doc = `e2e-${Date.now()}`;
    const a = connect(doc);
    const b = connect(doc);
    await eventually(() => expect(a.statuses.at(-1)).toBe("connected"));
    await eventually(() => expect(b.statuses.at(-1)).toBe("connected"));

    a.session.applyLocal(insert(0, "hello"));
    await eventually(() => expect(b.session.text).toBe("hello"));
    b.session.applyLocal(insert(5, " world"));
    await eventually(() => expect(a.session.text).toBe("hello world"));

    const late = connect(doc);
    await eventually(() => expect(late.session.text).toBe("hello world"));
  });

  it("delivers edits made while the connection was down", async () => {
    const doc = `e2e-offline-${Date.now()}`;
    const a = connect(doc);
    const b = connect(doc);
    await eventually(() => expect(a.statuses.at(-1)).toBe("connected"));
    await eventually(() => expect(b.statuses.at(-1)).toBe("connected"));

    a.sockets.at(-1)!.close(4000, "test: connection lost");
    await eventually(() => expect(a.statuses.at(-1)).toBe("reconnecting"));
    a.session.applyLocal(insert(0, "typed offline"));

    await eventually(() => expect(b.session.text).toBe("typed offline"));
    expect(a.sockets.length).toBeGreaterThan(1);
  });
});

describe.skipIf(gateways.length < 2)("across two gateways", () => {
  // Each editor stays on its own gateway with a single connection, so the
  // edits can only have crossed over live, through Redis, and not through
  // catch-up after a reconnect.
  it("shows each gateway's edits on the other, live", async () => {
    const doc = `e2e-cross-${Date.now()}`;
    const a = connect(doc, gateways[0]);
    const b = connect(doc, gateways[1]);
    await eventually(() => expect(a.statuses.at(-1)).toBe("connected"));
    await eventually(() => expect(b.statuses.at(-1)).toBe("connected"));

    a.session.applyLocal(insert(0, "from a"));
    await eventually(() => expect(b.session.text).toBe("from a"));
    b.session.applyLocal(insert(6, ", from b"));
    await eventually(() => expect(a.session.text).toBe("from a, from b"));

    expect(a.sockets).toHaveLength(1);
    expect(b.sockets).toHaveLength(1);
  });

  it("converges when both gateways' editors type at once", async () => {
    const doc = `e2e-race-${Date.now()}`;
    const editors = [0, 1, 0, 1].map((i) => connect(doc, gateways[i]));
    for (const e of editors) await eventually(() => expect(e.statuses.at(-1)).toBe("connected"));

    for (let round = 0; round < 25; round++) {
      // Editor i types its own letter at a spot that moves around.
      editors.forEach((e, i) => {
        const pos = (round * 7 + i) % (e.session.text.length + 1);
        e.session.applyLocal(insert(pos, String.fromCharCode(97 + i)));
      });
    }

    const want = 25 * editors.length;
    await eventually(() => {
      const texts = editors.map((e) => e.session.text);
      expect(texts[0]).toHaveLength(want);
      for (const t of texts) expect(t).toBe(texts[0]);
    }, 10_000);
  });
});
