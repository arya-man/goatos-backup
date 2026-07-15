package worker

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestHealthServer builds a HealthServer with a nil pool. Safe because the
// tests here only exercise paths that do NOT reach the DB ping (livez, and
// readyz when not-ready or draining).
func newTestHealthServer() *HealthServer {
	return NewHealthServer(":0", nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHealthLivezAlwaysOK(t *testing.T) {
	h := newTestHealthServer()
	rec := httptest.NewRecorder()
	h.handleLivez(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("livez = %d, want 200", rec.Code)
	}
}

func TestHealthReadyzNotReadyIs503(t *testing.T) {
	h := newTestHealthServer() // ready flag defaults false
	rec := httptest.NewRecorder()
	h.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz (not ready) = %d, want 503", rec.Code)
	}
}

func TestHealthReadyzDrainingIs503(t *testing.T) {
	h := newTestHealthServer()
	h.SetReady(true)
	h.BeginDraining() // draining must win even when ready
	rec := httptest.NewRecorder()
	h.handleReadyz(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz (draining) = %d, want 503", rec.Code)
	}
}
