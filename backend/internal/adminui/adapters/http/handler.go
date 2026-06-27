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

func (h *Handler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	resp := h.service.Bootstrap(r.Context(), app.BootstrapInput{
		TenantID: httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:  httpmiddleware.ActorIDFromContext(r.Context()),
		Grants:   httpmiddleware.AuthGrantsFromContext(r.Context()),
		TraceID:  httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if resp.CachePolicy.ETag != "" {
		w.Header().Set("ETag", resp.CachePolicy.ETag)
	}
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Add("Vary", "Authorization")
	w.Header().Add("Vary", httpmiddleware.TenantContextHeader)
	if resp.CachePolicy.ETag != "" && ifNoneMatch(r.Header.Values("If-None-Match"), resp.CachePolicy.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
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
