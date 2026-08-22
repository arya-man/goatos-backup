package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// The mapping write routes must be REGISTERED, actor-gated, and strict about their bodies. A
// handler that exists but is not wired to the mux leaves the Tag Mapping buttons just as dead as
// having no handler at all.
func TestMappingWriteRoutesAreRegisteredAndActorGated(t *testing.T) {
	svc := &fakeService{mappingResp: domain.TagMappingResponse{GoatID: "g", TagID: "T", MappingState: "mapped"}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc))

	for _, tc := range []struct{ path, body string }{
		{"/herd-signals/tag-mappings", `{"goat_id":"g","tag_id":"t"}`},
		{"/herd-signals/tag-mappings/replace", `{"goat_id":"g","new_tag_id":"t"}`},
		{"/herd-signals/tag-mappings/unmap", `{"tag_id":"t"}`},
		{"/herd-signals/heartbeats", `{"gateway_id":"gw"}`},
	} {
		// No actor in context: authentication required, never a silent write.
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body)))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("POST %s without an actor = %d, want 401", tc.path, rec.Code)
		}

		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
		req = req.WithContext(httpmiddleware.WithTenantID(httpmiddleware.WithActorID(req.Context(), "actor-1"), "tenant-1"))
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("POST %s with an actor = %d, want 200 (is the route registered?): %s", tc.path, rec.Code, rec.Body.String())
		}

		// Unknown fields are rejected, not silently dropped: a client that misspells goat_id must
		// be told, not have its mapping quietly ignored.
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(`{"nonsense_field":1}`))
		req = req.WithContext(httpmiddleware.WithTenantID(httpmiddleware.WithActorID(req.Context(), "actor-1"), "tenant-1"))
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s with an unknown field = %d, want 400", tc.path, rec.Code)
		}
	}
}

// A mapping refusal is a first-class answer with its own status, not a 500.
func TestMappingConflictAndNotFoundMapToTheirOwnStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"conflict", domain.ErrMappingConflict, http.StatusConflict},
		{"not found", domain.ErrMappingNotFound, http.StatusNotFound},
		{"validation", domain.ErrValidation, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{mappingErr: tc.err}
			mux := http.NewServeMux()
			Register(mux, NewHandler(svc))
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/herd-signals/tag-mappings", bytes.NewBufferString(`{"goat_id":"g","tag_id":"t"}`))
			req = req.WithContext(httpmiddleware.WithTenantID(httpmiddleware.WithActorID(req.Context(), "actor-1"), "tenant-1"))
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
