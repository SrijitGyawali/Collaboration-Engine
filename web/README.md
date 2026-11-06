# web

The Next.js + TypeScript editor for Collab Engine.

The CRDT is not reimplemented in TypeScript. The Go library in `internal/crdt` is compiled to WebAssembly and loaded in the browser, so the client and the server merge edits with exactly the same code.

## Run it

From the repository root, build the WebAssembly module into `web/public` first:

```bash
make web-wasm
```

Then, in this folder:

```bash
npm install
npm run dev
```

Open http://localhost:3000.

## Layout

| Path | What it holds |
| --- | --- |
| `src/app/` | Pages (App Router) |
| `public/crdt.wasm`, `public/wasm_exec.js` | Built by `make web-wasm`; git-ignored |

## Scripts

| Script | What it does |
| --- | --- |
| `npm run dev` | Development server |
| `npm run build` | Production build |
| `npm run lint` | ESLint |
