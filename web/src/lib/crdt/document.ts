// The typed, exception-throwing API over the raw bridge. This is what the rest
// of the app uses.

import type { ApplyStatus, BridgeResult, Position, RawDocument, WireID, WireOp } from "./types";

/** Thrown when the Go CRDT rejects a call; message is Go's error text. */
export class CRDTError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "CRDTError";
  }
}

/** Unwraps a bridge result, throwing CRDTError for {error}. */
export function unwrap<T extends object>(result: BridgeResult<T>): T {
  if ("error" in result) {
    throw new CRDTError(result.error);
  }
  return result;
}

/**
 * One replica's copy of a document, held by the Go CRDT in WebAssembly.
 *
 * Local edits (insertText, deleteRange, ...) return the ops to send to other
 * replicas. Ops received from them go through apply(). Positions count
 * characters (Unicode code points), not UTF-16 units: "👋" is 1, although
 * "👋".length in JavaScript is 2.
 */
export class CRDTDocument {
  readonly replica: string;
  #raw: RawDocument | undefined;

  constructor(replica: string, raw: RawDocument) {
    this.replica = replica;
    this.#raw = raw;
  }

  /** The current text. */
  text(): string {
    return this.#doc().text();
  }

  /** Number of characters in the text. */
  get length(): number {
    return this.#doc().length();
  }

  /** Inserts text so that it starts at pos (0..length). */
  insertText(pos: number, text: string): WireOp[] {
    return unwrap(this.#doc().insertText(pos, text)).ops;
  }

  /** Inserts one character at pos (0..length). */
  insertAt(pos: number, char: string): WireOp {
    if ([...char].length !== 1) {
      throw new CRDTError(`insertAt needs exactly one character, got ${JSON.stringify(char)}`);
    }
    return this.insertText(pos, char)[0];
  }

  /** Deletes count characters starting at pos. */
  deleteRange(pos: number, count: number): WireOp[] {
    return unwrap(this.#doc().deleteRange(pos, count)).ops;
  }

  /** Deletes the character at pos (0..length-1). */
  deleteAt(pos: number): WireOp {
    return this.deleteRange(pos, 1)[0];
  }

  /** Applies an op received from another replica. */
  apply(op: WireOp): ApplyStatus {
    return unwrap(this.#doc().apply(op)).status;
  }

  /** Where a character is now, for updating the view after apply(). */
  positionOf(id: WireID): Position {
    return unwrap(this.#doc().positionOf(id));
  }

  /** Ops received that are waiting for a character that has not arrived. */
  get pendingCount(): number {
    return this.#doc().pendingCount();
  }

  /**
   * Releases the Go callbacks behind this document. It cannot be used
   * afterwards. Call it when the document is closed for good.
   */
  free(): void {
    this.#raw?.free();
    this.#raw = undefined;
  }

  #doc(): RawDocument {
    if (!this.#raw) {
      throw new CRDTError(`document ${this.replica} has been freed`);
    }
    return this.#raw;
  }
}
