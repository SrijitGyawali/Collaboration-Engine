"use client";

// Other editors' carets, drawn over the textarea: a thin bar in each one's
// colour with their name above it.
//
// Positions are worked out again whenever anything could move them: the
// peers' cursors change, the text changes (local or remote), or the textarea
// scrolls or is resized. A cursor whose character has not arrived yet is not
// drawn until it has.

import { useEffect, useState, type RefObject } from "react";

import { caretBox } from "@/lib/editor/caret";
import { cpToUtf16 } from "@/lib/editor/text";
import type { Peer } from "@/lib/sync/server-protocol";
import type { CollabSession } from "@/lib/sync/session";
import styles from "./collab-editor.module.css";

interface Drawn {
  replica: string;
  name: string;
  color: string;
  top: number;
  left: number;
  height: number;
}

export function RemoteCursors({
  textarea,
  session,
  peers,
}: {
  textarea: RefObject<HTMLTextAreaElement | null>;
  session: CollabSession | null;
  peers: Peer[];
}) {
  const [drawn, setDrawn] = useState<Drawn[]>([]);

  useEffect(() => {
    const el = textarea.current;
    if (!el || !session) return;

    let frame = 0;
    const measure = () => {
      frame = 0;
      const next: Drawn[] = [];
      for (const peer of peers) {
        if (!peer.cursor) continue;
        const pos = session.cursorPosition(peer.cursor.head);
        if (pos === null) continue;
        const box = caretBox(el, cpToUtf16(el.value, pos));
        const top = box.top - el.scrollTop;
        const left = box.left - el.scrollLeft;
        // Scrolled out of view: not drawn rather than drawn over the edge.
        if (top + box.height < 0 || top > el.clientHeight) continue;
        next.push({ replica: peer.replica, name: peer.name, color: peer.color, top, left, height: box.height });
      }
      setDrawn(next);
    };
    // Several triggers in one frame cost one measurement.
    const schedule = () => {
      frame ||= requestAnimationFrame(measure);
    };

    schedule();
    const stopRemote = session.onRemoteUpdate(schedule);
    el.addEventListener("input", schedule);
    el.addEventListener("scroll", schedule);
    const resize = new ResizeObserver(schedule);
    resize.observe(el);
    return () => {
      if (frame) cancelAnimationFrame(frame);
      stopRemote();
      el.removeEventListener("input", schedule);
      el.removeEventListener("scroll", schedule);
      resize.disconnect();
    };
  }, [textarea, session, peers]);

  return (
    <div className={styles.cursors} aria-hidden="true">
      {drawn.map((c) => (
        <div
          key={c.replica}
          className={styles.caret}
          style={{ top: c.top, left: c.left, height: c.height, background: c.color }}
        >
          <span className={c.top < 18 ? `${styles.label} ${styles.labelBelow}` : styles.label} style={{ background: c.color }}>
            {c.name}
          </span>
        </div>
      ))}
    </div>
  );
}
