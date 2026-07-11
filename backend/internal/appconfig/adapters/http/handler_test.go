package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/appconfig/app"
	"github.com/vgoats/goatos/backend/internal/appconfig/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type fakeCompiler struct {
	resp domain.Response
	err  error
	last app.Input
}

func (f *fakeCompiler) Compile(_ context.Context, in app.Input) (domain.Response, error) {
	f.last = in
	if f.err != nil {
		return domain.Response{}, f.err
	}
	return f.resp, nil
}

func withActor(req *http.Request, tenantID, actorID string) *http.Request {
	ctx := httpmiddleware.WithTenantID(req.Context(), tenantID)
	ctx = httpmiddleware.WithActorID(ctx, actorID)
	return req.WithContext(ctx)
}

func TestGetConfigReturnsBundleAndSetsETag(t *testing.T) {
	compiler := &fakeCompiler{resp: domain.Response{
		Source:   domain.SourceAPI,
		Revision: "abc123",
		CachePolicy: domain.CachePolicy{
			ETag:            `W/"abc123"`,
			InProcessTTLSec: 60,
			RedisTTLHintSec: 600,
			RevisionSource:  "feature-flags+owned-modules+client-runtime-config",
		},
		FeatureFlags: map[string]bool{"vaccination_gaps_overlay": true},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(compiler))

	req := httptest.NewRequest(http.MethodGet, "/app/config", nil)
	req = withActor(req, "00000000-0000-4000-8000-000000000001", "actor-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("ETag") != `W/"abc123"` {
		t.Fatalf("ETag header = %q", rec.Header().Get("ETag"))
	}
	if compiler.last.TenantID != "00000000-0000-4000-8000-000000000001" || compiler.last.ActorID != "actor-1" {
		t.Fatalf("compile input = %#v", compiler.last)
	}
	var resp domain.Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Revision != "abc123" || !resp.FeatureFlags["vaccination_gaps_overlay"] {
		t.Fatalf("response = %#v", resp)
	}
}

func TestGetConfigPassesLocaleToCompiler(t *testing.T) {
	compiler := &fakeCompiler{resp: domain.Response{
		Source:      domain.SourceAPI,
		Revision:    "abc123",
		CachePolicy: domain.CachePolicy{ETag: `W/"abc123"`},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(compiler))

	req := httptest.NewRequest(http.MethodGet, "/app/config", nil)
	req.Header.Set("Accept-Language", "kn-IN, en;q=0.8")
	req = withActor(req, "00000000-0000-4000-8000-000000000001", "actor-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if compiler.last.LocaleTag != "kn" {
		t.Fatalf("compile locale = %q want kn", compiler.last.LocaleTag)
	}
}

func TestGetConfigReturns304OnMatchingIfNoneMatch(t *testing.T) {
	compiler := &fakeCompiler{resp: domain.Response{
		Source:      domain.SourceAPI,
		Revision:    "abc123",
		CachePolicy: domain.CachePolicy{ETag: `W/"abc123"`},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(compiler))

	req := httptest.NewRequest(http.MethodGet, "/app/config", nil)
	req.Header.Set("If-None-Match", `W/"abc123"`)
	req = withActor(req, "00000000-0000-4000-8000-000000000001", "actor-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d want 304 body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("304 body = %q want empty", rec.Body.String())
	}
}

func TestGetConfigReturns200OnStaleIfNoneMatch(t *testing.T) {
	compiler := &fakeCompiler{resp: domain.Response{
		Source:      domain.SourceAPI,
		Revision:    "new-rev",
		CachePolicy: domain.CachePolicy{ETag: `W/"new-rev"`},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(compiler))

	req := httptest.NewRequest(http.MethodGet, "/app/config", nil)
	req.Header.Set("If-None-Match", `W/"stale-rev"`)
	req = withActor(req, "00000000-0000-4000-8000-000000000001", "actor-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 (stale etag must refetch) body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetConfigMapsErrorTo500(t *testing.T) {
	compiler := &fakeCompiler{err: errBoom{}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(compiler))

	req := httptest.NewRequest(http.MethodGet, "/app/config", nil)
	req = withActor(req, "00000000-0000-4000-8000-000000000001", "actor-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d want 500", rec.Code)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
