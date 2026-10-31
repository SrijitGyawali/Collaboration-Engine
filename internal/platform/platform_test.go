package platform

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnv(t *testing.T) {
	t.Setenv("COLLAB_TEST_SET", "value")
	t.Setenv("COLLAB_TEST_EMPTY", "")

	if got := Env("COLLAB_TEST_SET", "def"); got != "value" {
		t.Errorf("set variable: got %q, want %q", got, "value")
	}
	if got := Env("COLLAB_TEST_EMPTY", "def"); got != "def" {
		t.Errorf("empty variable: got %q, want %q", got, "def")
	}
	if got := Env("COLLAB_TEST_UNSET", "def"); got != "def" {
		t.Errorf("unset variable: got %q, want %q", got, "def")
	}
}

func TestRegisterHealth(t *testing.T) {
	mux := http.NewServeMux()
	RegisterHealth(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "ok" {
		t.Errorf("body: got %q, want %q", body, "ok")
	}
}
