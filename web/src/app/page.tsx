import Link from "next/link";

import { CRDTCheck } from "./crdt-check";
import styles from "./page.module.css";

export default function Home() {
  return (
    <main className={styles.main}>
      <h1>Collab Engine</h1>
      <p className={styles.lead}>
        A real-time collaborative text editor. Edits merge through a CRDT
        written in Go and compiled to WebAssembly, so the browser and the
        server use the same merge code.
      </p>
      <p>
        <Link className={styles.cta} href="/editor">
          Open the shared editor
        </Link>{" "}
        <span className={styles.lead}>and open it again in a second tab to edit together.</span>
      </p>
      <CRDTCheck />
    </main>
  );
}
