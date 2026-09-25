// Package http exposes the admin-web UI contract.
package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/adminui/app"
	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type Service interface {
	Bootstrap(ctx context.Context, input app.BootstrapInput) domain.BootstrapResponse
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /admin-web/bootstrap", h.Bootstrap)
}

// bodyService is implemented by the real service: it hands back the contract already encoded,
// so the ~1.3 MB of pages is not encoded a second time here.
type bodyService interface {
	BootstrapBody(ctx context.Context, input app.BootstrapInput) (string, []byte, error)
}

func (h *Handler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	input := app.BootstrapInput{
		TenantID: httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:  httpmiddleware.ActorIDFromContext(r.Context()),
		Grants:   httpmiddleware.AuthGrantsFromContext(r.Context()),
		TraceID:  httpmiddleware.TraceIDFromContext(r.Context()),
	}
	if encoded, ok := h.service.(bodyService); ok {
		etag, body, err := encoded.BootstrapBody(r.Context(), input)
		if err == nil {
			if writeCacheHeaders(w, r, etag) {
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
		// An encode failure falls through to the generic encoder below.
	}
	resp := h.service.Bootstrap(r.Context(), input)
	if writeCacheHeaders(w, r, resp.CachePolicy.ETag) {
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// writeCacheHeaders sets the revalidation headers and reports whether a 304 was written.
func writeCacheHeaders(w http.ResponseWriter, r *http.Request, etag string) bool {
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Add("Vary", "Authorization")
	w.Header().Add("Vary", httpmiddleware.TenantContextHeader)
	if etag != "" && ifNoneMatch(r.Header.Values("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

func ifNoneMatch(values []string, etag string) bool {
	want := weakETagOpaque(etag)
	if want == "" {
		return false
	}
	for _, value := range values {
		for _, candidate := range strings.Split(value, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == "*" || weakETagOpaque(candidate) == want {
				return true
			}
		}
	}
	return false
}

func weakETagOpaque(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "W/") {
		value = strings.TrimSpace(strings.TrimPrefix(value, "W/"))
	}
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return ""
	}
	return value
}
