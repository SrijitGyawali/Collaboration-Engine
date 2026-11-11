import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";

import { CollabEditor } from "@/components/collab-editor";
import styles from "./page.module.css";

export const metadata: Metadata = {
  title: "Editor · Collab Engine",
};

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

/** Document names are used in channel names and URLs: keep them simple. */
const DOC_ID = /^[a-zA-Z0-9_-]{1,40}$/;

// The page's frame is static and prerendered. Only the part that depends on
// ?doc= reads the request, so it sits inside <Suspense> (required with Cache
// Components: request data outside a boundary would block prerendering).
export default function EditorPage({ searchParams }: { searchParams: SearchParams }) {
  return (
    <main className={styles.main}>
      <header className={styles.header}>
        <h1>Shared document</h1>
        <Link href="/">Back</Link>
      </header>
      <Suspense fallback={<p className={styles.hint}>Opening the document…</p>}>
        <DocumentEditor searchParams={searchParams} />
      </Suspense>
    </main>
  );
}

async function DocumentEditor({ searchParams }: { searchParams: SearchParams }) {
  const { doc } = await searchParams;
  const docId = typeof doc === "string" && DOC_ID.test(doc) ? doc : "demo";

  return (
    <>
      <p className={styles.hint}>
        Open{" "}
        <a href={`/editor?doc=${docId}`} target="_blank" rel="noopener">
          this page in another tab
        </a>{" "}
        and type in both. Edits merge through the CRDT, and sync through the server when one is configured.
      </p>
      <CollabEditor docId={docId} />
    </>
  );
}
