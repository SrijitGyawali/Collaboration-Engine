"use client";

// The shared-document editor: a textarea, a CollabSession syncing it, and a
// status line. The two-way binding itself lives in TextareaBinding
// (src/lib/editor/binding.ts).
//
// With NEXT_PUBLIC_GATEWAY_URL set (see web/.env.example), edits sync through
// the gateway, between any browsers; without it, only between tabs of this
// browser.

import { useEffect, useRef, useState } from "react";

import { loadCRDT } from "@/lib/crdt";
import { TextareaBinding } from "@/lib/editor/binding";
import type { Identity } from "@/lib/sync/server-protocol";
import { CollabSession, randomReplicaId } from "@/lib/sync/session";
import { broadcastChannelTransport, type Transport } from "@/lib/sync/transport";
import { websocketTransport, type ConnectionStatus } from "@/lib/sync/websocket-transport";
import styles from "./collab-editor.module.css";

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

  useEffect(() => {
    let closed = false;
    let session: CollabSession | undefined;
    let binding: TextareaBinding | undefined;

    loadCRDT()
      .then((crdt) => {
        if (closed || !textarea.current) return;
        // The transport and the session must share the replica: the
        // server only accepts ops made by the replica that joined.
        const replica = randomReplicaId();
        const transport: Transport = GATEWAY_URL
          ? websocketTransport({
              url: GATEWAY_URL,
              docId,
              replica,
              onStatus: (s, you) => setConnection((prev) => ({ kind: "server", status: s, you: you ?? (prev.kind === "server" ? prev.you : undefined) })),
            })
          : broadcastChannelTransport(docId);
        session = new CollabSession(crdt, transport, replica);
        binding = new TextareaBinding(textarea.current, session, { onLengthChange: setLength });
        setStatus({ state: "ready", replica });
      })
      .catch((err: unknown) => {
        if (!closed) setStatus({ state: "error", message: err instanceof Error ? err.message : String(err) });
      });

    return () => {
      closed = true;
      binding?.dispose();
      session?.close();
    };
  }, [docId]);

  return (
    <div className={styles.editor}>
      <textarea
        ref={textarea}
        className={styles.textarea}
        disabled={status.state !== "ready"}
        placeholder={status.state === "ready" ? "Start typing…" : "Loading…"}
        aria-label={`Shared document ${docId}`}
        spellCheck={false}
      />
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
