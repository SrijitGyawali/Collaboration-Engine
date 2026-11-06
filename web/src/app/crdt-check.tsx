"use client";

// Loads the Go CRDT in the browser and runs the classic conflict on two
// replicas, so the page shows the module working end to end.

import { useEffect, useState } from "react";

import { loadCRDT, type WireOp } from "@/lib/crdt";
import styles from "./crdt-check.module.css";

type Check =
  | { state: "loading" }
  | { state: "error"; message: string }
  | { state: "ready"; loadMs: number; steps: Step[]; alice: string; bob: string };

interface Step {
  label: string;
  alice: string;
  bob: string;
}

/** Sends ops through JSON, as the network will. */
const send = (ops: WireOp[]): WireOp[] => JSON.parse(JSON.stringify(ops)) as WireOp[];

async function runCheck(): Promise<Check> {
  const started = performance.now();
  const crdt = await loadCRDT();
  const loadMs = Math.round(performance.now() - started);

  // Fixed names keep the result the same on every load ("HYXi": the tie
  // between the two concurrent inserts is broken by replica name).
  const alice = crdt.newDocument("alice");
  const bob = crdt.newDocument("bob");
  const steps: Step[] = [];
  const record = (label: string) => steps.push({ label, alice: alice.text(), bob: bob.text() });

  try {
    for (const op of send(alice.insertText(0, "Hi"))) bob.apply(op);
    record('Alice types "Hi"; Bob receives it');

    const x = alice.insertText(1, "X");
    const y = bob.insertText(1, "Y");
    record("At the same moment, both insert after H");

    for (const op of send(y)) alice.apply(op);
    for (const op of send(x)) bob.apply(op);
    record("They exchange edits");

    return { state: "ready", loadMs, steps, alice: alice.text(), bob: bob.text() };
  } finally {
    alice.free();
    bob.free();
  }
}

export function CRDTCheck() {
  const [check, setCheck] = useState<Check>({ state: "loading" });

  useEffect(() => {
    let cancelled = false;
    runCheck()
      .catch((err: unknown): Check => ({ state: "error", message: err instanceof Error ? err.message : String(err) }))
      .then((result) => {
        if (!cancelled) setCheck(result);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (check.state === "loading") {
    return <section className={styles.card}>Loading the CRDT module…</section>;
  }
  if (check.state === "error") {
    return (
      <section className={`${styles.card} ${styles.error}`} role="alert">
        <strong>The CRDT module failed to load.</strong> {check.message}
      </section>
    );
  }

  const converged = check.alice === check.bob;
  return (
    <section className={styles.card} aria-labelledby="crdt-check-title">
      <h2 id="crdt-check-title">Live check: two replicas in this tab</h2>
      <p className={styles.muted}>
        Go CRDT loaded as WebAssembly in {check.loadMs} ms. Every op below travels through JSON, as it will over
        the network.
      </p>
      <table className={styles.table}>
        <thead>
          <tr>
            <th scope="col">Step</th>
            <th scope="col">Alice sees</th>
            <th scope="col">Bob sees</th>
          </tr>
        </thead>
        <tbody>
          {check.steps.map((step) => (
            <tr key={step.label}>
              <td>{step.label}</td>
              <td>
                <code>{step.alice}</code>
              </td>
              <td>
                <code>{step.bob}</code>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className={converged ? styles.ok : styles.error}>
        {converged ? `Converged: both replicas show "${check.alice}".` : "Replicas diverged. This is a bug."}
      </p>
    </section>
  );
}
