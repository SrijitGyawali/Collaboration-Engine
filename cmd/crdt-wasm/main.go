//go:build js && wasm

// Command crdt-wasm exposes the CRDT library to the browser as a WebAssembly
// module, so the client and the server merge edits with the same code.
//
// Build it with `make wasm`.
package main

func main() {
	// Keep the module alive so exported functions stay callable from JS.
	select {}
}
