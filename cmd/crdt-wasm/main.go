//go:build js && wasm

// Command crdt-wasm exposes the CRDT library to the browser as a WebAssembly
// module, so the client and the server merge edits with the same code.
//
// Build it with `make web-wasm`, which also copies Go's wasm_exec.js loader
// next to it. Once the module runs it sets one global:
//
//	globalThis.CollabCRDT.newDocument(replica) -> document
//
// and then calls globalThis.__collabCRDTReady(), if defined, so the loader
// knows the API is in place. The document object's methods are described in
// document.go. The TypeScript wrapper in web/src/lib/crdt gives them types
// and turns error results into exceptions.
package main

import (
	"fmt"
	"syscall/js"
)

func main() {
	api := js.Global().Get("Object").New()
	api.Set("newDocument", js.FuncOf(guard(newDocument)))
	js.Global().Set("CollabCRDT", api)

	if ready := js.Global().Get("__collabCRDTReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}

	// Keep the module alive: returning from main would make every function
	// above unusable.
	select {}
}

// guard wraps a bridge function so that a panic becomes an error result
// instead of killing the module. A panic inside a js.FuncOf callback would
// otherwise end the Go program, breaking every document in the page.
func guard(fn func(this js.Value, args []js.Value) any) func(js.Value, []js.Value) any {
	return func(this js.Value, args []js.Value) (result any) {
		defer func() {
			if r := recover(); r != nil {
				result = errorResult(fmt.Errorf("crdt-wasm: internal error: %v", r))
			}
		}()
		return fn(this, args)
	}
}

// errorResult is how every bridge call reports failure: {error: "..."}.
// Returning a value instead of throwing keeps Go and JavaScript error
// handling separate; the TypeScript wrapper throws.
func errorResult(err error) map[string]any {
	return map[string]any{"error": err.Error()}
}

// argString and argInt read argument i, failing clearly on the wrong type
// rather than panicking inside syscall/js.
func argString(args []js.Value, i int, name string) (string, error) {
	if i >= len(args) || args[i].Type() != js.TypeString {
		return "", fmt.Errorf("crdt-wasm: %s must be a string", name)
	}
	return args[i].String(), nil
}

func argInt(args []js.Value, i int, name string) (int, error) {
	if i >= len(args) || args[i].Type() != js.TypeNumber {
		return 0, fmt.Errorf("crdt-wasm: %s must be a number", name)
	}
	f := args[i].Float()
	if f != float64(int(f)) {
		return 0, fmt.Errorf("crdt-wasm: %s must be a whole number, got %v", name, f)
	}
	return int(f), nil
}
