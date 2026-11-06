# web

The Next.js + TypeScript editor for Collab Engine.

The CRDT is not reimplemented in TypeScript. The Go library in `internal/crdt` is compiled to WebAssembly and loaded in the browser, so the client and the server merge edits with exactly the same code.

## Run it

From the repository root:

```bash
make web-install
```

```bash
make web-dev
```

`make web-dev` rebuilds the WebAssembly module into `web/public` and starts the dev server. Open http://localhost:3000. The landing page loads the module and runs a live two-replica conflict check.

## Using the CRDT

```ts
import { loadCRDT } from "@/lib/crdt";

const crdt = await loadCRDT();           // loads /crdt.wasm once per page
const doc = crdt.newDocument(replicaId); // replicaId must be unique

const ops = doc.insertText(0, "hello");  // local edit: send these ops
doc.apply(opFromServer);                 // remote edit: "applied" | "duplicate" | "pending"
const { pos } = doc.positionOf(opFromServer.id); // where it landed, to update the view
```

Errors from Go come back as a thrown `CRDTError`. Positions count characters (Unicode code points), not UTF-16 units: `"👋"` has length 1 here.

## Layout

| Path | What it holds |
| --- | --- |
| `src/app/` | Pages (App Router) and the live CRDT check |
| `src/lib/crdt/types.ts` | Types for ops and the raw bridge, matching `internal/wire` |
| `src/lib/crdt/loader.ts` | Loads and starts the WebAssembly module |
| `src/lib/crdt/document.ts` | `CRDTDocument`, the API the app uses |
| `src/lib/crdt/crdt.test.ts` | End-to-end tests of the real module in Node |
| `public/crdt.wasm`, `public/wasm_exec.js` | Built by `make web-wasm`; git-ignored |
| `go.mod` | Not Go code: stops Go's `./...` from entering `node_modules` |

## Scripts

| Script | What it does |
| --- | --- |
| `npm run dev` | Development server |
| `npm run build` | Production build |
| `npm run lint` | ESLint |
| `npm test` | Vitest; needs `make web-wasm` first (`make web-test` does both) |
