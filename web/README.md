# web

The Next.js + TypeScript editor. It is created on Nov 6 (see the build plan),
together with the WebAssembly build of `internal/crdt`.

It will hold:

- the editor UI and its binding from text changes to CRDT operations
- the CRDT, loaded from `crdt.wasm` (built with `make wasm`)
- the WebSocket client, the IndexedDB offline queue and presence
