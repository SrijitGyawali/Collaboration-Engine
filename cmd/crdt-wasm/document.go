//go:build js && wasm

package main

import (
	"errors"
	"syscall/js"

	"github.com/SrijitGyawali/Collaboration-Engine/internal/crdt"
	"github.com/SrijitGyawali/Collaboration-Engine/internal/wire"
)

// newDocument implements CollabCRDT.newDocument(replica). It returns a JS
// object wrapping one crdt.Document:
//
//	text()                  -> string
//	length()                -> number
//	insertText(pos, text)   -> {ops: Op[]} | {error}
//	deleteRange(pos, count) -> {ops: Op[]} | {error}
//	free()                  -> releases the Go callbacks
//
// Ops are plain objects in the wire format (see internal/wire), ready to
// JSON.stringify and send.
func newDocument(_ js.Value, args []js.Value) any {
	replica, err := argString(args, 0, "replica")
	if err != nil {
		return errorResult(err)
	}
	if replica == "" {
		return errorResult(errors.New("crdt-wasm: replica must not be empty"))
	}

	doc := crdt.NewDocument(replica)
	obj := js.Global().Get("Object").New()

	// Every js.FuncOf allocates a callback the Go runtime keeps alive until
	// it is released; free() releases them all.
	var funcs []js.Func
	method := func(name string, fn func(js.Value, []js.Value) any) {
		f := js.FuncOf(guard(fn))
		funcs = append(funcs, f)
		obj.Set(name, f)
	}

	method("text", func(js.Value, []js.Value) any {
		return doc.Text()
	})
	method("length", func(js.Value, []js.Value) any {
		return doc.Len()
	})
	method("insertText", func(_ js.Value, args []js.Value) any {
		pos, err := argInt(args, 0, "pos")
		if err != nil {
			return errorResult(err)
		}
		text, err := argString(args, 1, "text")
		if err != nil {
			return errorResult(err)
		}
		return opsResult(doc.InsertText(pos, text))
	})
	method("deleteRange", func(_ js.Value, args []js.Value) any {
		pos, err := argInt(args, 0, "pos")
		if err != nil {
			return errorResult(err)
		}
		count, err := argInt(args, 1, "count")
		if err != nil {
			return errorResult(err)
		}
		return opsResult(doc.DeleteRange(pos, count))
	})
	method("free", func(js.Value, []js.Value) any {
		for _, f := range funcs {
			f.Release()
		}
		funcs = nil
		return nil
	})

	return obj
}

// opsResult turns the result of a local edit into {ops: [...]} or {error}.
func opsResult(ops []crdt.Op, err error) any {
	if err != nil {
		return errorResult(err)
	}
	list := make([]any, len(ops))
	for i, op := range ops {
		list[i] = wire.FromCRDT(op).ToMap()
	}
	return map[string]any{"ops": list}
}
