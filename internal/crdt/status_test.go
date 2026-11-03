package crdt

import "testing"

func TestStatusString(t *testing.T) {
	for status, want := range map[Status]string{
		Applied:   "applied",
		Duplicate: "duplicate",
		Pending:   "pending",
		Status(9): "Status(9)",
	} {
		if got := status.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}
