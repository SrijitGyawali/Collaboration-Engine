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

To edit together, open http://localhost:3000/editor in two tabs and type in both. `?doc=name` opens a different document. Until the server arrives (week 2), edits sync between tabs of the same browser through a `BroadcastChannel`.

## How the editor syncs

```text
textarea ──input──▶ TextareaBinding ──Change──▶ CollabSession ──ops──▶ Transport ──▶ other tabs
textarea ◀─value + caret── TextareaBinding ◀─RemoteUpdate── CollabSession ◀─ops── Transport
```

- **TextareaBinding** (`src/lib/editor/binding.ts`) diffs each input against the previous text, using the caret to place repeated characters. It moves the caret through remote changes so it stays next to the same characters, and waits while an input method (IME) is composing.
- **CollabSession** (`src/lib/sync/session.ts`) owns the tab's CRDT replica. It turns changes into ops, applies incoming ops, and reports exactly where each one landed. A new tab says hello and is sent the full op log.
- **Transport** (`src/lib/sync/transport.ts`) is a BroadcastChannel today and will be a WebSocket next.

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
| `src/app/` | Pages (App Router): landing page with the live CRDT check, `/editor` |
| `src/components/collab-editor.tsx` | The editor component: textarea, session, status line |
| `src/lib/editor/` | Code point helpers, diffing, caret transforms, the textarea binding |
| `src/lib/sync/` | Sync messages, transports, `CollabSession` |
| `src/test/` | Test helpers: real module loader, in-memory hub, fake textarea |
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
