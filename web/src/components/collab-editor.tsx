"use client";

// The shared-document editor: a textarea, a CollabSession syncing it with the
// other tabs on the same document, and a status line. The two-way binding
// itself lives in TextareaBinding (src/lib/editor/binding.ts).

import { useEffect, useRef, useState } from "react";

import { loadCRDT } from "@/lib/crdt";
import { TextareaBinding } from "@/lib/editor/binding";
import { CollabSession } from "@/lib/sync/session";
import { broadcastChannelTransport } from "@/lib/sync/transport";
import styles from "./collab-editor.module.css";

type Status = { state: "loading" } | { state: "ready"; replica: string } | { state: "error"; message: string };

export function CollabEditor({ docId }: { docId: string }) {
  const textarea = useRef<HTMLTextAreaElement>(null);
  const [status, setStatus] = useState<Status>({ state: "loading" });
  const [length, setLength] = useState(0);

  useEffect(() => {
    let closed = false;
    let session: CollabSession | undefined;
    let binding: TextareaBinding | undefined;

    loadCRDT()
      .then((crdt) => {
        if (closed || !textarea.current) return;
        session = new CollabSession(crdt, broadcastChannelTransport(docId));
        binding = new TextareaBinding(textarea.current, session, { onLengthChange: setLength });
        setStatus({ state: "ready", replica: session.replica });
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
            Document <code>{docId}</code> · you are <code>{status.replica}</code> · {length} characters
          </>
        )}
      </p>
    </div>
  );
}
