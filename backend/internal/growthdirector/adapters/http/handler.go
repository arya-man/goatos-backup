// Package http exposes the Growth Director read over HTTP, mirroring the
// weighing module's handler/respond conventions.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

type Service interface {
	GetGrowthDirectorWeights(ctx context.Context, actor domain.Actor, parkID, fromBusinessDate, toBusinessDate, sex, origin, weighingCategory, sections string) (domain.GrowthDirectorWeights, error)
	GetFeedWeightBand(ctx context.Context, actor domain.Actor, parkID, fromBusinessDate, toBusinessDate, sex, origin, weighingCategory string) (domain.FeedWeightBand, error)
	GetFCR(ctx context.Context, actor domain.Actor, parkID, fromBusinessDate, toBusinessDate, sex, origin, weighingCategory string) (domain.FCRReport, error)
	GetSalePrices(ctx context.Context, actor domain.Actor) (domain.SalePrices, error)
	GetAssumptions(ctx context.Context, actor domain.Actor, includeStages bool) (domain.Assumptions, error)
	PutAssumptions(ctx context.Context, actor domain.Actor, update domain.AssumptionsUpdate) (domain.Assumptions, error)
}

type Handler struct {
	service Service
	log     *slog.Logger
}

func NewHandler(service Service, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{service: service, log: l}
}

func Register(mux *http.ServeMux, h *Handler) {
	// Admin-web read only; no /app twin — the phone has no Growth Director
	// section. Add the twin the day it does, with its own route entry.
	mux.HandleFunc("GET /growth-director/weights", h.GetGrowthDirectorWeights)
	// The ADG Analytics Weight-wise tab's feed table: latest feed direction per pen
	// beside the pen's latest weight evidence. Admin-web read only, like the above.
	mux.HandleFunc("GET /growth-director/feed-by-weight-band", h.GetFeedWeightBand)
	// The FCR tab on Kids -- ADG Analytics (maintainer request 2026-09-07) and the sale-price
	// vocabulary both it and the Comparison tab value gain at. Same module, same gate.
	mux.HandleFunc("GET /growth-director/fcr", h.GetFCR)
	mux.HandleFunc("GET /growth-director/sale-prices", h.GetSalePrices)
	// The Assumptions drawer (maintainer decision 2026-09-19): read on WeighingMonitor, write on
	// weighing.assumptions.write -- both decided in permissions/routes.go.
	mux.HandleFunc("GET /growth-director/assumptions", h.GetAssumptions)
	mux.HandleFunc("PUT /growth-director/assumptions", h.PutAssumptions)
}

// GetFCR serves the Weighing FCR tab. Parameters mirror GetGrowthDirectorWeights; `sex` and
// `origin` narrow at PEN grain because feed is directed to a whole pen.
func (h *Handler) GetFCR(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetFCR(
		r.Context(),
		actor(r),
		r.URL.Query().Get("park_id"),
		r.URL.Query().Get("from"),
		r.URL.Query().Get("to"),
		r.URL.Query().Get("sex"),
		r.URL.Query().Get("origin"),
		r.URL.Query().Get("weighing_category"),
	)
	h.respond(w, r, result, err)
}

// GetSalePrices serves the assumed live-weight sale price per species effective today.
func (h *Handler) GetSalePrices(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetSalePrices(r.Context(), actor(r))
	h.respond(w, r, result, err)
}

// GetAssumptions serves the figures the Weighing area is valued at.
func (h *Handler) GetAssumptions(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetAssumptions(r.Context(), actor(r), r.URL.Query().Get("include_stages") == "1")
	h.respond(w, r, result, err)
}

// PutAssumptions lands an edit from the drawer. A figure outside its business band is a 400
// invalid_assumption carrying the band in farm words; a stale row_version is a 409 so the drawer
// reloads rather than silently overwriting someone else's decision.
func (h *Handler) PutAssumptions(w http.ResponseWriter, r *http.Request) {
	var update domain.AssumptionsUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, errorEnvelope{Code: "invalid_body", Message: "request body must be valid JSON", TraceID: traceID(r)}, nil)
		return
	}
	result, err := h.service.PutAssumptions(r.Context(), actor(r), update)
	switch {
	case err == nil:
		httpresponse.WriteJSON(w, http.StatusOK, result)
	case errors.Is(err, ports.ErrInvalidArgument):
		msg := strings.TrimPrefix(err.Error(), ports.ErrInvalidArgument.Error()+": ")
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, errorEnvelope{Code: "invalid_assumption", Message: msg, TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrAssumptionConflict):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, errorEnvelope{Code: "row_version_conflict", Message: "these figures were changed by someone else; reload and try again", TraceID: traceID(r)}, nil)
	default:
		h.respond(w, r, result, err)
	}
}

// GetFeedWeightBand serves the Feed by weight band table. `park_id` is optional;
// `from`/`to` are inclusive business dates bounding the weight evidence; `sex`,
// `origin` and `weighing_category` are the Weights page's filters. Both head-count
// variants (on farm / including exited animals) ride on every row.
func (h *Handler) GetFeedWeightBand(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.service.GetFeedWeightBand(
		r.Context(),
		actor(r),
		q.Get("park_id"),
		q.Get("from"),
		q.Get("to"),
		q.Get("sex"),
		q.Get("origin"),
		q.Get("weighing_category"),
	)
	h.respond(w, r, result, err)
}

// GetGrowthDirectorWeights serves the Growth Director widgets on the admin-web
// Weights screen. `park_id` is optional (omit for every park the caller may
// monitor); `from`/`to` are INCLUSIVE Asia/Kolkata business dates (YYYY-MM-DD)
// defaulting to the last 28 days ending today.
func (h *Handler) GetGrowthDirectorWeights(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetGrowthDirectorWeights(
		r.Context(),
		actor(r),
		r.URL.Query().Get("park_id"),
		r.URL.Query().Get("from"),
		r.URL.Query().Get("to"),
		r.URL.Query().Get("sex"),
		r.URL.Query().Get("origin"),
		r.URL.Query().Get("weighing_category"),
		r.URL.Query().Get("sections"),
	)
	h.respond(w, r, result, err)
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, body any, err error) {
	if err == nil {
		httpresponse.WriteJSON(w, http.StatusOK, body)
		return
	}
	switch {
	case errors.Is(err, ports.ErrForbidden):
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden, errorEnvelope{Code: "permission_denied", Message: "permission denied", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrInvalidArgument):
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, errorEnvelope{Code: "invalid_request", Message: "request is invalid", TraceID: traceID(r)}, nil)
	case errors.Is(err, ports.ErrNotFound):
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound, errorEnvelope{Code: "not_found", Message: "growth director resource was not found", TraceID: traceID(r)}, nil)
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, errorEnvelope{Code: "internal", Message: "internal error", TraceID: traceID(r)}, err)
	}
}

func actor(r *http.Request) domain.Actor {
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	roles := make([]string, 0, len(grants))
	for _, grant := range grants {
		roles = append(roles, grant.Role)
	}
	return domain.Actor{
		TenantID: httpmiddleware.TenantIDFromContext(r.Context()),
		UserID:   httpmiddleware.ActorIDFromContext(r.Context()),
		Roles:    roles,
	}
}

func traceID(r *http.Request) string { return httpmiddleware.TraceIDFromContext(r.Context()) }
