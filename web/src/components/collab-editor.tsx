"use client";

// The shared-document editor: a textarea, a CollabSession syncing it, and a
// status line. The two-way binding itself lives in TextareaBinding
// (src/lib/editor/binding.ts).
//
// With NEXT_PUBLIC_GATEWAY_URL set (see web/.env.example), edits sync through
// the gateway, between any browsers, and everyone sees who else is here and
// where their caret is; without it, edits sync only between tabs of this
// browser, with no presence.

import { useEffect, useRef, useState } from "react";

import { loadCRDT } from "@/lib/crdt";
import { TextareaBinding } from "@/lib/editor/binding";
import { utf16ToCp } from "@/lib/editor/text";
import type { Identity, Peer } from "@/lib/sync/server-protocol";
import { CollabSession, randomReplicaId } from "@/lib/sync/session";
import { broadcastChannelTransport, type Transport } from "@/lib/sync/transport";
import { websocketTransport, type ConnectionStatus } from "@/lib/sync/websocket-transport";
import styles from "./collab-editor.module.css";
import { RemoteCursors } from "./remote-cursors";

/** Events after which the local selection may have moved. */
const SELECTION_EVENTS = ["select", "keyup", "mouseup", "input", "focus"] as const;

// Must be written out in full: Next.js inlines it at build time.
const GATEWAY_URL = process.env.NEXT_PUBLIC_GATEWAY_URL;

type Status = { state: "loading" } | { state: "ready"; replica: string } | { state: "error"; message: string };

/** Where edits go: the gateway (with its connection state) or tabs only. */
type Connection = { kind: "tabs" } | { kind: "server"; status: ConnectionStatus; you?: Identity };

const STATUS_TEXT: Record<ConnectionStatus, string> = {
  connecting: "connecting to the server…",
  connected: "connected to the server",
  reconnecting: "offline, reconnecting… (your edits are kept)",
  closed: "disconnected",
};

export function CollabEditor({ docId }: { docId: string }) {
  const textarea = useRef<HTMLTextAreaElement>(null);
  const [status, setStatus] = useState<Status>({ state: "loading" });
  const [connection, setConnection] = useState<Connection>(
    GATEWAY_URL ? { kind: "server", status: "connecting" } : { kind: "tabs" },
  );
  const [length, setLength] = useState(0);
  const [peers, setPeers] = useState<Peer[]>([]);
  const [live, setLive] = useState<CollabSession | null>(null);

  useEffect(() => {
    let closed = false;
    let session: CollabSession | undefined;
    let binding: TextareaBinding | undefined;
    let stopCursor = () => {};

    loadCRDT()
      .then((crdt) => {
        if (closed || !textarea.current) return;
        // The transport and the session must share the replica: the
        // server only accepts ops made by the replica that joined.
        const replica = randomReplicaId();
        const server = GATEWAY_URL
          ? websocketTransport({
              url: GATEWAY_URL,
              docId,
              replica,
              onStatus: (s, you) => setConnection((prev) => ({ kind: "server", status: s, you: you ?? (prev.kind === "server" ? prev.you : undefined) })),
              onPeers: setPeers,
            })
          : null;
        const transport: Transport = server ?? broadcastChannelTransport(docId);
        const s = new CollabSession(crdt, transport, replica);
        session = s;
        const el = textarea.current;
        binding = new TextareaBinding(el, s, { onLengthChange: setLength });

        // Tell the others where our selection is, held by characters (see
        // CollabSession.cursorAnchor) so it survives their edits.
        if (server) {
          const sendCursor = () => {
            const value = el.value;
            const start = s.cursorAnchor(utf16ToCp(value, el.selectionStart));
            const end = s.cursorAnchor(utf16ToCp(value, el.selectionEnd));
            const backward = el.selectionDirection === "backward";
            server.setCursor(backward ? { anchor: end, head: start } : { anchor: start, head: end });
          };
          for (const e of SELECTION_EVENTS) el.addEventListener(e, sendCursor);
          stopCursor = () => {
            for (const e of SELECTION_EVENTS) el.removeEventListener(e, sendCursor);
          };
        }
        setLive(s);
        setStatus({ state: "ready", replica });
      })
      .catch((err: unknown) => {
        if (!closed) setStatus({ state: "error", message: err instanceof Error ? err.message : String(err) });
      });

    return () => {
      closed = true;
      stopCursor();
      binding?.dispose();
      session?.close();
      setLive(null);
      setPeers([]);
    };
  }, [docId]);

  return (
    <div className={styles.editor}>
      <div className={styles.surface}>
        <textarea
          ref={textarea}
          className={styles.textarea}
          disabled={status.state !== "ready"}
          placeholder={status.state === "ready" ? "Start typing…" : "Loading…"}
          aria-label={`Shared document ${docId}`}
          spellCheck={false}
        />
        <RemoteCursors textarea={textarea} session={live} peers={peers} />
      </div>
      {connection.kind === "server" && status.state === "ready" && (
        <p className={styles.peers} aria-live="polite">
          {peers.length === 0 ? (
            "Nobody else is here yet."
          ) : (
            <>
              Also here:{" "}
              {peers.map((p) => (
                <span key={p.replica} className={styles.peer}>
                  <span className={styles.dot} style={{ background: p.color }} />
                  {p.name}
                </span>
              ))}
            </>
          )}
        </p>
      )}
      <p className={styles.status} role="status">
        {status.state === "loading" && "Loading the CRDT…"}
        {status.state === "error" && <span className={styles.error}>Could not start: {status.message}</span>}
        {status.state === "ready" && (
          <>
            Document <code>{docId}</code> · you are{" "}
            {connection.kind === "server" && connection.you ? (
              <strong style={{ color: connection.you.color }}>{connection.you.name}</strong>
            ) : (
              <code>{status.replica}</code>
            )}{" "}
            · {length} characters ·{" "}
            {connection.kind === "server" ? STATUS_TEXT[connection.status] : "syncing between tabs of this browser"}
          </>
        )}
      </p>
    </div>
  );
}
