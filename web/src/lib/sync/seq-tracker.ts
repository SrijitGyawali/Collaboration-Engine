/**
 * Tracks which of a document's sequence numbers this client has seen, to know
 * what to ask for after reconnecting.
 *
 * Sequence numbers do not always arrive in order: two people's edits are
 * numbered one after the other by the server, but their broadcasts can
 * overtake each other. If the client simply kept the highest number seen, a
 * reconnect right after receiving seq 6 but before seq 5 would ask for "after
 * 6" and never get 5. So `last` is the highest number with nothing missing
 * below it; numbers seen beyond a gap wait until the gap fills.
 */
export class SeqTracker {
  #contiguous = 0;
  #ahead = new Set<number>();

  /** The highest sequence number such that every one up to it has been seen. */
  get last(): number {
    return this.#contiguous;
  }

  /** Records that seq has been seen. Repeats are harmless. */
  add(seq: number): void {
    if (seq <= this.#contiguous) return;
    this.#ahead.add(seq);
    while (this.#ahead.delete(this.#contiguous + 1)) {
      this.#contiguous++;
    }
  }
}
