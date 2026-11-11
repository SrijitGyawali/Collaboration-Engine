// End to end against a running gateway: real WebSockets, the real server,
// real storage. Skipped unless GATEWAY_URL is set, for example:
//
//   make up && DATABASE_URL=... go run ./cmd/gateway
//   GATEWAY_URL=ws://localhost:8080/ws npx vitest run gateway.e2e

import { afterEach, beforeAll, describe, expect, it } from "vitest";

import type { CRDT } from "@/lib/crdt";
import { loadTestCRDT } from "@/test/load-crdt";
import { CollabSession, randomReplicaId } from "./session";
import { websocketTransport, type ConnectionStatus, type SocketLike } from "./websocket-transport";

const url = process.env.GATEWAY_URL;
let crdt: CRDT;
const open: CollabSession[] = [];

beforeAll(async () => {
  if (url) crdt = await loadTestCRDT();
});

afterEach(() => {
  for (const s of open.splice(0)) s.close();
});

function connect(doc: string) {
  const replica = randomReplicaId();
  const statuses: ConnectionStatus[] = [];
  const sockets: SocketLike[] = [];
  const transport = websocketTransport({
    url: url!,
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
