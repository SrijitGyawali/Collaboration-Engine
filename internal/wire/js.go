package wire

import (
	"fmt"
	"math"
)

// MaxSafeCounter is the largest counter a JavaScript number holds exactly
// (Number.MAX_SAFE_INTEGER, 2^53 - 1). Every JavaScript number is a float64,
// so a larger counter would be silently rounded and the op would get a
// different ID. A replica would need to make nine quadrillion edits to get
// there, but the bridge still checks rather than corrupt data.
const MaxSafeCounter = 1<<53 - 1

// ToMap returns the op as nested maps of float64 and string, the types the
// WebAssembly bridge can turn into a JavaScript object with js.ValueOf. The
// keys match the JSON field names.
func (w Op) ToMap() map[string]any {
	m := map[string]any{
		"kind": w.Kind,
		"id":   w.ID.ToMap(),
	}
	if w.After != nil {
		m["after"] = w.After.ToMap()
	}
	if w.Target != nil {
		m["target"] = w.Target.ToMap()
	}
	if w.Value != "" {
		m["value"] = w.Value
	}
	return m
}

// ToMap returns the ID as a map with a float64 counter, for JavaScript.
func (id ID) ToMap() map[string]any {
	return map[string]any{"counter": float64(id.Counter), "replica": id.Replica}
}

// CounterFromFloat converts a counter received as a JavaScript number. It
// rejects anything that is not a whole number in [0, MaxSafeCounter], such as
// NaN, a fraction or a negative number from a buggy caller.
func CounterFromFloat(f float64) (uint64, error) {
	if f < 0 || f > MaxSafeCounter || f != math.Trunc(f) {
		return 0, fmt.Errorf("%w: counter %v is not a whole number in [0, 2^53)", ErrMalformed, f)
	}
	return uint64(f), nil
}
