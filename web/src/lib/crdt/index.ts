// Public entry point of the browser CRDT.
//
//   const crdt = await loadCRDT();
//   const doc = crdt.newDocument(replicaId);
//   const ops = doc.insertText(0, "hello"); // send these
//   doc.apply(opFromServer);                // apply what arrives

import { CRDTDocument, unwrap } from "./document";
import { loadBridge, type LoadOptions } from "./loader";

export { CRDTDocument, CRDTError } from "./document";
export type { LoadOptions } from "./loader";
export type * from "./types";

/** The loaded CRDT module. */
export interface CRDT {
  /** Creates an empty document owned by replica, which must be unique. */
  newDocument(replica: string): CRDTDocument;
}

/** Loads the WebAssembly module (once per page) and returns the API. */
export async function loadCRDT(options?: LoadOptions): Promise<CRDT> {
  const bridge = await loadBridge(options);
  return {
    newDocument: (replica) => new CRDTDocument(replica, unwrap(bridge.newDocument(replica))),
  };
}
