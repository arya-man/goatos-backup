// Package http exposes the herd register read model API.
package http

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type HerdRegisterService interface {
	GetSummary(ctx context.Context, req domain.HerdRegisterSummaryQuery) (domain.HerdRegisterSummary, error)
}

type Handler struct {
	service HerdRegisterService
	log     *slog.Logger
}

func NewHandler(service HerdRegisterService, log *slog.Logger) *Handler {
	return &Handler{service: service, log: log}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /herd-register/summary", h.GetSummary)
}

// GetSummary serves exact summary counts from the herd register summary projection.
func (h *Handler) GetSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return
	}

	// Parse query parameters.
	lifecycle := r.URL.Query().Get("lifecycle_status")
	park := r.URL.Query().Get("park_id")
	breed := r.URL.Query().Get("breed")
	sex := r.URL.Query().Get("sex")

	req := domain.HerdRegisterSummaryQuery{
		TenantID:        tenantID,
		LifecycleStatus: nullableString(lifecycle),
		ParkID:          nullableString(park),
		Breed:           nullableString(breed),
		Sex:             nullableString(sex),
	}

	summary, err := h.service.GetSummary(r.Context(), req)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, "herd register summary", err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, map[string]any{
		"items": summary.Items,
	})
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
