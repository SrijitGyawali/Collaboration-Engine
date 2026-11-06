package wire

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestToMap(t *testing.T) {
	w := Op{
		Kind:  "insert",
		ID:    ID{Counter: 42, Replica: "alice"},
		After: &ID{Counter: 17, Replica: "bob"},
		Value: "X",
	}
	want := map[string]any{
		"kind":  "insert",
		"id":    map[string]any{"counter": float64(42), "replica": "alice"},
		"after": map[string]any{"counter": float64(17), "replica": "bob"},
		"value": "X",
	}
	if got := w.ToMap(); !reflect.DeepEqual(got, want) {
		t.Errorf("ToMap() = %v\n want %v", got, want)
	}

	del := Op{Kind: "delete", ID: ID{Counter: 43, Replica: "alice"}, Target: &ID{Counter: 17, Replica: "bob"}}
	got := del.ToMap()
	if _, ok := got["after"]; ok {
		t.Error("delete map has an after key")
	}
	if _, ok := got["value"]; ok {
		t.Error("delete map has a value key")
	}
	if got["target"] == nil {
		t.Error("delete map has no target")
	}
}

func TestCounterFromFloat(t *testing.T) {
	for _, f := range []float64{0, 1, 42, MaxSafeCounter} {
		got, err := CounterFromFloat(f)
		if err != nil || got != uint64(f) {
			t.Errorf("CounterFromFloat(%v) = %d, %v", f, got, err)
		}
	}
	for _, f := range []float64{-1, 1.5, MaxSafeCounter + 1, math.NaN(), math.Inf(1)} {
		if _, err := CounterFromFloat(f); !errors.Is(err, ErrMalformed) {
			t.Errorf("CounterFromFloat(%v) = %v, want ErrMalformed", f, err)
		}
	}
}
