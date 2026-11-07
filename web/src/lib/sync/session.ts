// CollabSession is one tab's side of a shared document: it owns the tab's
// CRDT replica, turns local edits into ops for the other participants, and
// applies theirs, reporting what changed so the editor can update in place.
// It knows nothing about the DOM; the editor component drives it.

import { CRDTError, type CRDT, type CRDTDocument, type WireID, type WireOp } from "@/lib/crdt";
import type { Change } from "@/lib/editor/change";

import { parseMessage } from "./messages";
import type { Transport } from "./transport";

/** What a batch of remote ops did to the document. */
export interface RemoteUpdate {
  /** The text after the batch. */
  text: string;
  /**
   * The edits, in order. Each one's position refers to the text as it was
   * just before that edit, so applying them one by one to the previous text
   * gives the new text, and moving the caret through them keeps it in place.
   *
   * null when the edits cannot be pinned down exactly: an op had to wait
   * for a character that had not arrived, or released ops that had been
   * waiting. The editor then compares the old and new text instead.
   */
  changes: Change[] | null;
}

/** A short random replica name; unique enough for the tabs of a document. */
export function randomReplicaId(): string {
  return crypto.randomUUID().replaceAll("-", "").slice(0, 12);
}

const opKey = (op: WireOp) => `${op.id.counter}@${op.id.replica}`;

export class CollabSession {
  readonly replica: string;
  #doc: CRDTDocument;
  #transport: Transport;
  #listeners = new Set<(update: RemoteUpdate) => void>();
  /** Every op this replica has, in the order it got them, for catch-up. */
  #log: WireOp[] = [];
  #known = new Set<string>();
  #closed = false;

  constructor(crdt: CRDT, transport: Transport, replica: string = randomReplicaId()) {
    this.replica = replica;
    this.#doc = crdt.newDocument(replica);
    this.#transport = transport;
    transport.onMessage((data) => this.#receive(data));
  }

  /** The current text of this replica. */
  get text(): string {
    return this.#doc.text();
  }

  /** Ops received that wait for a character that has not arrived yet. */
  get pendingCount(): number {
    return this.#doc.pendingCount;
  }

  /** Every op this replica has, in the order it got them. */
  get log(): readonly WireOp[] {
    return this.#log;
  }

  /**
   * Applies a local edit (for example from diffText) and sends its ops to
   * the other participants.
   */
  applyLocal(change: Change): void {
    this.#assertOpen();
    const ops: WireOp[] = [];
    if (change.deleteCount > 0) {
      ops.push(...this.#doc.deleteRange(change.pos, change.deleteCount));
    }
    if (change.insert !== "") {
      ops.push(...this.#doc.insertText(change.pos, change.insert));
    }
    if (ops.length === 0) {
      return;
    }
    this.#remember(ops);
    this.#transport.send({ type: "ops", from: this.replica, ops });
  }

  /** Calls listener after every batch of remote ops that changed anything. */
  onRemoteUpdate(listener: (update: RemoteUpdate) => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  /** Stops syncing and frees the replica. */
  close(): void {
    if (this.#closed) return;
    this.#closed = true;
    this.#listeners.clear();
    this.#transport.close();
    this.#doc.free();
  }

  #receive(data: unknown): void {
    const message = parseMessage(data);
    if (this.#closed || !message || message.from === this.replica) {
      return;
    }
    if (message.type === "ops") {
      this.#applyRemote(message.ops);
    }
  }

  #applyRemote(ops: WireOp[]): void {
    const changes: Change[] = [];
    let exact = true;
    let changed = false;

    for (const op of ops) {
      const pendingBefore = this.#doc.pendingCount;
      // A delete's position has to be read before it is applied, while the
      // character is still visible. null means it was not visible, so
      // deleting it again changes nothing on screen.
      const deletedAt = op.kind === "delete" ? this.#visiblePosition(op.target) : null;

      let status;
      try {
        status = this.#doc.apply(op);
      } catch (err) {
        // A malformed op from another participant must not break this
        // tab's document: skip it.
        if (err instanceof CRDTError) {
          console.warn(`sync: ignoring op from another replica: ${err.message}`);
          continue;
        }
        throw err;
      }
      if (status === "duplicate") {
        continue;
      }
      this.#remember([op]);
      changed = true;

      if (status === "pending") {
        exact = false; // it will be applied later, as part of another op
        continue;
      }
      if (this.#doc.pendingCount < pendingBefore) {
        exact = false; // this op released waiting ops in the same call
      }
      if (op.kind === "insert") {
        changes.push({ pos: this.#doc.positionOf(op.id).pos, deleteCount: 0, insert: op.value });
      } else if (deletedAt !== null) {
        changes.push({ pos: deletedAt, deleteCount: 1, insert: "" });
      }
    }

    if (!changed) {
      return;
    }
    const update: RemoteUpdate = { text: this.text, changes: exact ? changes : null };
    for (const listener of this.#listeners) {
      listener(update);
    }
  }

  /** The position of a visible character, or null if it is deleted or unknown. */
  #visiblePosition(id: WireID): number | null {
    try {
      const { pos, visible } = this.#doc.positionOf(id);
      return visible ? pos : null;
    } catch {
      return null;
    }
  }

  #remember(ops: WireOp[]): void {
    for (const op of ops) {
      const key = opKey(op);
      if (!this.#known.has(key)) {
        this.#known.add(key);
        this.#log.push(op);
      }
    }
  }

  #assertOpen(): void {
    if (this.#closed) {
      throw new Error("sync: session is closed");
    }
  }
}
