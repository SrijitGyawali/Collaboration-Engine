// Types for the CRDT WebAssembly bridge (cmd/crdt-wasm).
//
// The wire types match internal/wire in Go field for field, so an op created
// in the browser can be JSON.stringify-ed and sent, and an op received from
// the server can be passed straight to apply().

/** Identifies one character, and the operation that created it, forever. */
export interface WireID {
  /** Lamport counter. Always a whole number below 2^53. */
  counter: number;
  /** Unique name of the replica (browser tab or server) that created it. */
  replica: string;
}

/** Insert one character after another one (or at the start). */
export interface InsertOp {
  kind: "insert";
  id: WireID;
  /** The character this one goes after. Absent means the document start. */
  after?: WireID;
  /** Exactly one character. */
  value: string;
}

/** Hide one character. */
export interface DeleteOp {
  kind: "delete";
  id: WireID;
  /** The character to remove. */
  target: WireID;
}

/** One edit, in the form replicas send each other. */
export type WireOp = InsertOp | DeleteOp;

/** What apply() did with an op; see crdt.Status in Go. */
export type ApplyStatus = "applied" | "duplicate" | "pending";

/** Where a character is in the text; see Document.PositionOf in Go. */
export interface Position {
  /** Number of visible characters before it. */
  pos: number;
  /** False if the character has been deleted (pos is then where it was). */
  visible: boolean;
}

// The raw bridge, exactly as Go exposes it. Every call that can fail returns
// {error} instead of throwing; CRDTDocument (document.ts) turns that into an
// exception. Application code should use CRDTDocument, not these.

export type BridgeResult<T> = T | { error: string };

export interface RawDocument {
  text(): string;
  length(): number;
  insertText(pos: number, text: string): BridgeResult<{ ops: WireOp[] }>;
  deleteRange(pos: number, count: number): BridgeResult<{ ops: WireOp[] }>;
  apply(op: WireOp): BridgeResult<{ status: ApplyStatus }>;
  positionOf(id: WireID): BridgeResult<Position>;
  pendingCount(): number;
  free(): void;
}

export interface RawBridge {
  newDocument(replica: string): BridgeResult<RawDocument>;
}

/** Go's WebAssembly runtime, defined by wasm_exec.js. */
export interface GoRuntime {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
}

declare global {
  // Set by wasm_exec.js.
  var Go: (new () => GoRuntime) | undefined;
  // Set by the Go module once it is running.
  var CollabCRDT: RawBridge | undefined;
  // Called by the Go module when CollabCRDT is ready, if defined.
  var __collabCRDTReady: (() => void) | undefined;
}
