// Package http exposes the admin-web UI contract.
package http

import (
	"net/http"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type Service interface {
	Bootstrap() domain.BootstrapResponse
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

func (h *Handler) Bootstrap(w http.ResponseWriter, _ *http.Request) {
	httpresponse.WriteJSON(w, http.StatusOK, h.service.Bootstrap())
}
