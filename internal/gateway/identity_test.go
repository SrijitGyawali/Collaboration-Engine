package gateway

import (
	"strings"
	"testing"
)

func TestNewIdentity(t *testing.T) {
	id, err := newIdentity("a1b2c3d4e5f6", "  Srijit  ")
	if err != nil {
		t.Fatal(err)
	}
	if id.Replica != "a1b2c3d4e5f6" || id.Name != "Srijit" || !strings.HasPrefix(id.Color, "#") {
		t.Errorf("identity = %+v", id)
	}

	again, _ := newIdentity("a1b2c3d4e5f6", "")
	if again.Color != id.Color {
		t.Error("the same replica got a different colour")
	}
}

func TestDisplayName(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"", "Guest a1b2"},
		{"   ", "Guest a1b2"},
		{"Ann\u0000\u001b[31m", "Ann[31m"},
		{"नमस्ते", "नमस्ते"},
		{strings.Repeat("é", 50), strings.Repeat("é", 40)},
	} {
		if got := displayName(tt.in, "a1b2c3"); got != tt.want {
			t.Errorf("displayName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := displayName("", "ab"); got != "Guest ab" {
		t.Errorf("short replica: %q", got)
	}
}

func TestReplicaNamesAreChecked(t *testing.T) {
	for _, bad := range []string{"", "has space", "a@b", strings.Repeat("x", 33)} {
		if _, err := newIdentity(bad, ""); err == nil {
			t.Errorf("newIdentity(%q) accepted", bad)
		}
	}
}
