// An in-memory Transport for tests: every session connected to a Hub
// receives the others' messages. Delivery is immediate by default; with
// hold() messages queue up, and the test releases them in any order it
// wants, to reproduce concurrent edits and out-of-order arrival exactly.

import type { SyncMessage } from "@/lib/sync/messages";
import type { Transport } from "@/lib/sync/transport";

interface Member {
  handler: (data: unknown) => void;
  open: boolean;
}

interface Queued {
  to: Member;
  data: unknown;
}

export class Hub {
  #members: Member[] = [];
  #queue: Queued[] = [];
  #holding = false;

  connect(): Transport {
    const me: Member = { handler: () => {}, open: true };
    this.#members.push(me);
    return {
      send: (message: SyncMessage) => {
        for (const other of this.#members) {
          if (other !== me && other.open) {
            // structuredClone copies the message as BroadcastChannel would,
            // so no object is shared between sessions.
            this.#deliver({ to: other, data: structuredClone(message) });
          }
        }
      },
      onMessage: (handler) => {
        me.handler = handler;
      },
      close: () => {
        me.open = false;
      },
    };
  }

  /** Queues messages instead of delivering them, until release(). */
  hold(): void {
    this.#holding = true;
  }

  /** Delivers queued messages in the given order (default: as sent), then resumes. */
  release(order: (queue: Queued[]) => Queued[] = (q) => q): void {
    const queue = order(this.#queue);
    this.#queue = [];
    this.#holding = false;
    for (const item of queue) this.#deliver(item);
  }

  #deliver(item: Queued): void {
    if (this.#holding) {
      this.#queue.push(item);
    } else if (item.to.open) {
      item.to.handler(item.data);
    }
  }
}
