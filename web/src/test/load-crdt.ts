// Loads the real CRDT WebAssembly module in Node for tests.
//
// Needs `make web-wasm` first (or run `make web-test`), which puts crdt.wasm
// and Go's wasm_exec.js in web/public.

import { existsSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

import { loadCRDT, type CRDT } from "@/lib/crdt";

const publicFile = (name: string) => fileURLToPath(new URL(`../../public/${name}`, import.meta.url));

export async function loadTestCRDT(): Promise<CRDT> {
  const wasm = publicFile("crdt.wasm");
  const exec = publicFile("wasm_exec.js");
  if (!existsSync(wasm) || !existsSync(exec)) {
    throw new Error("crdt.wasm or wasm_exec.js missing in web/public: run `make web-wasm` from the repo root");
  }
  if (!globalThis.Go) {
    createRequire(import.meta.url)(exec); // defines globalThis.Go
  }
  return loadCRDT({ wasmBytes: readFileSync(wasm) });
}
