// Loads the CRDT WebAssembly module and starts it.
//
// Three steps, in order:
//  1. Make sure Go's runtime glue (wasm_exec.js) has run, which defines
//     globalThis.Go. In the browser it is loaded as a <script>.
//  2. Fetch and instantiate crdt.wasm with Go's imports.
//  3. Start the Go program and wait for it to call __collabCRDTReady(),
//     which it does once globalThis.CollabCRDT is in place.

import type { RawBridge } from "./types";

export interface LoadOptions {
  /** URL of the module. Default: "/crdt.wasm" (served from web/public). */
  wasmUrl?: string;
  /** URL of Go's wasm_exec.js. Default: "/wasm_exec.js". */
  execUrl?: string;
  /**
   * The module's bytes, instead of fetching wasmUrl. Tests in Node use this,
   * after importing wasm_exec.js themselves.
   */
  wasmBytes?: BufferSource;
}

let loading: Promise<RawBridge> | undefined;

/**
 * Loads and starts the module once per page; later calls return the same
 * promise. If loading fails, the next call tries again.
 */
export function loadBridge(options: LoadOptions = {}): Promise<RawBridge> {
  loading ??= start(options).catch((err: unknown) => {
    loading = undefined;
    throw err;
  });
  return loading;
}

async function start(options: LoadOptions): Promise<RawBridge> {
  if (globalThis.CollabCRDT) {
    return globalThis.CollabCRDT;
  }

  await ensureGoRuntime(options.execUrl ?? "/wasm_exec.js");
  const go = new globalThis.Go!();

  const ready = new Promise<void>((resolve) => {
    globalThis.__collabCRDTReady = resolve;
  });
  const instance = await instantiate(options, go.importObject);

  // run() only settles when the Go program exits, which a healthy module
  // never does (main blocks forever). If it settles before the ready signal,
  // the module crashed during start-up; fail instead of waiting forever.
  const exited = go.run(instance).then(() => {
    throw new Error("crdt: WebAssembly module exited before it was ready");
  });
  await Promise.race([ready, exited]);

  const bridge = globalThis.CollabCRDT;
  if (!bridge) {
    throw new Error("crdt: module signalled ready without defining CollabCRDT");
  }
  return bridge;
}

/** Runs wasm_exec.js, unless globalThis.Go already exists. */
async function ensureGoRuntime(execUrl: string): Promise<void> {
  if (globalThis.Go) {
    return;
  }
  if (typeof document === "undefined") {
    throw new Error("crdt: wasm_exec.js has not run; outside a browser, import it before loading the module");
  }
  await new Promise<void>((resolve, reject) => {
    const script = document.createElement("script");
    script.src = execUrl;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error(`crdt: failed to load ${execUrl}`));
    document.head.appendChild(script);
  });
  if (!globalThis.Go) {
    throw new Error(`crdt: ${execUrl} did not define Go`);
  }
}

async function instantiate(options: LoadOptions, imports: WebAssembly.Imports): Promise<WebAssembly.Instance> {
  if (options.wasmBytes) {
    const { instance } = await WebAssembly.instantiate(options.wasmBytes, imports);
    return instance;
  }

  const url = options.wasmUrl ?? "/crdt.wasm";
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`crdt: failed to fetch ${url}: HTTP ${response.status}`);
  }
  // Streaming compiles while the module downloads, but needs the server to
  // send Content-Type: application/wasm. Fall back to a plain download
  // otherwise.
  if (response.headers.get("Content-Type") === "application/wasm") {
    const { instance } = await WebAssembly.instantiateStreaming(response, imports);
    return instance;
  }
  const { instance } = await WebAssembly.instantiate(await response.arrayBuffer(), imports);
  return instance;
}
