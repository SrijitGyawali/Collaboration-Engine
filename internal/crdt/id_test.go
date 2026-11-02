package crdt

import "testing"

func TestIDCompare(t *testing.T) {
	tests := []struct {
		name string
		a, b ID
		want int
	}{
		{"equal", ID{3, "alice"}, ID{3, "alice"}, 0},
		{"lower counter first", ID{2, "zed"}, ID{3, "alice"}, -1},
		{"higher counter last", ID{4, "alice"}, ID{3, "zed"}, 1},
		{"same counter, replica breaks tie", ID{3, "alice"}, ID{3, "bob"}, -1},
		{"root before everything", Root, ID{1, "alice"}, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Compare(tt.b); got != tt.want {
				t.Errorf("%v.Compare(%v) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			// Compare must be antisymmetric, or replicas could disagree.
			if got := tt.b.Compare(tt.a); got != -tt.want {
				t.Errorf("%v.Compare(%v) = %d, want %d", tt.b, tt.a, got, -tt.want)
			}
		})
	}
}

func TestIDString(t *testing.T) {
	if got := (ID{42, "alice"}).String(); got != "42@alice" {
		t.Errorf("String() = %q, want %q", got, "42@alice")
	}
	if got := Root.String(); got != "root" {
		t.Errorf("Root.String() = %q, want %q", got, "root")
	}
}

func TestIsRoot(t *testing.T) {
	if !Root.IsRoot() {
		t.Error("Root.IsRoot() = false")
	}
	if (ID{1, "alice"}).IsRoot() {
		t.Error("ID{1, alice}.IsRoot() = true")
	}
}
