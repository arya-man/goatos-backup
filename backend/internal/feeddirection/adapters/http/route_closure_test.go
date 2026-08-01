package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInstantCompleteRouteIsGone proves the OLD instant feed-completion edge is closed.
//
// The ratified contract (docs/decisions/feed-distribution-verification.md, AGENTS.md) is
// operator submit -> pending_verification -> verifier approve -> completed. The pre-gate route
// POST /feed-direction/complete wrote 'completed' INSTANTLY, so as long as it stayed registered a
// client could walk straight around the verification gate. "Inert" has to mean unreachable, not
// merely un-navigated: the route must not exist on the mux the API serves.
func TestInstantCompleteRouteIsGone(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(nil, nil))

	req := httptest.NewRequest(http.MethodPost, "/feed-direction/complete", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /feed-direction/complete = %d, want 404: the instant completion path bypasses the verifier gate and must be unreachable", rec.Code)
	}
}

// TestGatedCompletionRoutesStillRegistered is the other half: closing the instant path must not
// take the verifier-gated replacements down with it.
func TestGatedCompletionRoutesStillRegistered(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(nil, nil))

	for _, path := range []string{
		"/feed-direction/distribution/complete",
		"/feed-direction/packing/complete",
	} {
		if _, pattern := mux.Handler(httptest.NewRequest(http.MethodPost, path, nil)); pattern == "" {
			t.Fatalf("POST %s is not registered; the verifier-gated completion path must stay live", path)
		}
	}
}
