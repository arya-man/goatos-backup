package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/appconfig/domain"
)

type fakeCompiler struct {
	resp  domain.Response
	err   error
	calls int
}

func (f *fakeCompiler) Compile(_ context.Context) (domain.Response, error) {
	f.calls++
	if f.err != nil {
		return domain.Response{}, f.err
	}
	return f.resp, nil
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
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want 200 body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("ETag") != `W/"abc123"` {
		t.Fatalf("ETag header = %q", rec.Header().Get("ETag"))
	}
	if compiler.calls != 1 {
		t.Fatalf("compile calls = %d want 1", compiler.calls)
	}
	var resp domain.Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Revision != "abc123" || !resp.FeatureFlags["vaccination_gaps_overlay"] {
		t.Fatalf("response = %#v", resp)
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
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d want 500", rec.Code)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
