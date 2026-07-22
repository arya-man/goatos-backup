package ceoai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// leadershipRequest builds an SSE ask request whose SERVER-SESSION context
// carries a CEO-internal grant (auth is never read from the body).
func leadershipRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/ceo-ai/ask", strings.NewReader(body))
	ctx := req.Context()
	ctx = httpmiddleware.WithTenantID(ctx, "tenant-1")
	ctx = httpmiddleware.WithActorID(ctx, "actor-1")
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal}})
	return req.WithContext(ctx)
}

// TestServiceMountedRouteStreamsSSE is the end-to-end wiring proof for the
// review's integration-gap finding: the FULLY-BUILT service (real orchestrator
// via ceoai.Build, deterministic fallback planner — no fake StreamingAsker) is
// mounted on a mux exactly as bootstrap does, and a POST /ceo-ai/ask with
// stream:true (the admin-web default) returns a live SSE token stream whose
// terminal `final` frame carries the answer envelope. This is what a live curl
// against :8080 exercises, minus the auth middleware.
func TestServiceMountedRouteStreamsSSE(t *testing.T) {
	svc := ceoai.Build(ceoai.Options{})
	mux := http.NewServeMux()
	svc.Register(mux) // same call bootstrap makes on protectedMux

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, leadershipRequest(`{"question":"how many goats","stream":true}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream (route did not reach StreamHandler)", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: final") {
		t.Fatalf("no terminal final event on the wire:\n%s", body)
	}
	// Only the allowed frame types may appear — no trace/CoT leak on the mounted path.
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "event:") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			if name != "token" && name != "final" && name != "error" && name != "progress" {
				t.Fatalf("unexpected event type %q on mounted route", name)
			}
		}
	}
}

// TestServiceMountedRouteJSONWhenStreamFalse proves the SAME route degrades to a
// single JSON envelope (no SSE) when the body sets stream:false — the router
// dispatches on the body field, one path, one permission.
func TestServiceMountedRouteJSONWhenStreamFalse(t *testing.T) {
	svc := ceoai.Build(ceoai.Options{})
	mux := http.NewServeMux()
	svc.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, leadershipRequest(`{"question":"how many goats","stream":false}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
		t.Fatalf("stream:false still returned SSE (content-type=%q)", ct)
	}
}

// TestServiceMountedRouteRejectsNonLeadership proves the mounted route enforces
// the leadership gate end-to-end: a request with no CEO-internal grant is
// unauthorized (tenant/role come only from the session).
func TestServiceMountedRouteRejectsNonLeadership(t *testing.T) {
	svc := ceoai.Build(ceoai.Options{})
	mux := http.NewServeMux()
	svc.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/ceo-ai/ask", strings.NewReader(`{"question":"q","stream":false}`))
	// No auth grants on the context.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(context.Background()))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for non-leadership", rec.Code)
	}
}
