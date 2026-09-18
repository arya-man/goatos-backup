// Package http is the admin transport for Configuration -> Items and settings. Route patterns
// must stay byte-identical to permissions/routes.go.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/configuration/app"
	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

const maxRequestBytes = 256 << 10

// Service is the slice of the app service the transport needs.
type Service interface {
	Registers() []domain.Register
	Counts(ctx context.Context, tenantID string) (map[string]int, error)
	List(ctx context.Context, tenantID, register string, p ports.ListParams) (ports.Page, error)
	Get(ctx context.Context, tenantID, register, id string) (domain.Row, error)
	Options(ctx context.Context, tenantID, register string) ([]ports.RefOption, error)
	Usage(ctx context.Context, tenantID, register, id string) (domain.Usage, error)
	Create(ctx context.Context, w ports.WriteParams, register string, raw map[string]any) (domain.Row, error)
	Update(ctx context.Context, w ports.WriteParams, register, id string, raw map[string]any, rowVersion int) (domain.Row, error)
	SetStatus(ctx context.Context, w ports.WriteParams, register, id, status string, rowVersion int) (domain.Row, error)
	Delete(ctx context.Context, w ports.WriteParams, register, id string, rowVersion int) error
}

// Handler serves /admin/configuration/*.
type Handler struct {
	service Service
	log     *slog.Logger
}

// NewHandler constructs the transport.
func NewHandler(service Service, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{service: service, log: log}
}

// Register mounts the routes. The literal `registers` path is registered beside the
// {register} pattern; Go's mux prefers the literal.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /admin/configuration/registers", h.Registers)
	mux.HandleFunc("GET /admin/configuration/{register}", h.List)
	mux.HandleFunc("POST /admin/configuration/{register}", h.Create)
	mux.HandleFunc("GET /admin/configuration/{register}/options", h.Options)
	mux.HandleFunc("GET /admin/configuration/{register}/{row_id}", h.Get)
	mux.HandleFunc("PUT /admin/configuration/{register}/{row_id}", h.Update)
	mux.HandleFunc("DELETE /admin/configuration/{register}/{row_id}", h.Delete)
	mux.HandleFunc("GET /admin/configuration/{register}/{row_id}/usage", h.Usage)
	mux.HandleFunc("POST /admin/configuration/{register}/{row_id}/status", h.SetStatus)
}

type registersPayload struct {
	Registers []domain.Register `json:"registers"`
	Counts    map[string]int    `json:"counts"`
	Groups    []groupPayload    `json:"groups"`
	TraceID   string            `json:"trace_id"`
}

type groupPayload struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type listPayload struct {
	Register   domain.Register `json:"register"`
	Rows       []domain.Row    `json:"rows"`
	NextCursor string          `json:"next_cursor"`
	Total      int             `json:"total"`
	TraceID    string          `json:"trace_id"`
}

type rowPayload struct {
	Row     domain.Row `json:"row"`
	TraceID string     `json:"trace_id"`
}

type optionsPayload struct {
	Options []ports.RefOption `json:"options"`
	TraceID string            `json:"trace_id"`
}

type usagePayload struct {
	Usage    domain.Usage `json:"usage"`
	Sentence string       `json:"sentence"`
	TraceID  string       `json:"trace_id"`
}

type rowWrite struct {
	Fields     map[string]any `json:"fields"`
	RowVersion int            `json:"row_version"`
}

type statusWrite struct {
	Status     string `json:"status"`
	RowVersion int    `json:"row_version"`
}

type deleteWrite struct {
	RowVersion int `json:"row_version"`
}

// Registers serves GET /admin/configuration/registers: the catalog plus the rail counts.
func (h *Handler) Registers(w http.ResponseWriter, r *http.Request) {
	counts, err := h.service.Counts(r.Context(), tenantID(r))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, registersPayload{
		Registers: h.service.Registers(),
		Counts:    counts,
		Groups: []groupPayload{
			{Key: domain.GroupFarmPlaces, Label: "Farm places"},
			{Key: domain.GroupAnimalTypes, Label: "Animal types"},
			{Key: domain.GroupCatalogue, Label: "Catalogue"},
		},
		TraceID: traceID(r),
	})
}

// List serves GET /admin/configuration/{register}?status&q&cursor&limit&f.<column>=.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	register := r.PathValue("register")
	reg, ok := domain.RegisterByKey(register)
	if !ok {
		writeErr(w, r, h.log, app.HTTPError(domain.ErrUnknownRegister))
		return
	}
	q := r.URL.Query()
	p := ports.ListParams{Status: strings.TrimSpace(q.Get("status")), Query: q.Get("q"), Cursor: q.Get("cursor"), Filters: map[string]string{}}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeErr(w, r, h.log, app.BadRequest("invalid_limit", "Limit must be a positive number."))
			return
		}
		p.Limit = n
	}
	for key, vals := range q {
		if col, ok := strings.CutPrefix(key, "f."); ok && len(vals) > 0 && strings.TrimSpace(vals[0]) != "" {
			p.Filters[col] = strings.TrimSpace(vals[0])
		}
	}
	page, err := h.service.List(r.Context(), tenantID(r), register, p)
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	if page.Rows == nil {
		page.Rows = []domain.Row{}
	}
	httpresponse.WriteJSON(w, http.StatusOK, listPayload{Register: reg, Rows: page.Rows, NextCursor: page.NextCursor, Total: page.Total, TraceID: traceID(r)})
}

// Get serves GET /admin/configuration/{register}/{row_id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	row, err := h.service.Get(r.Context(), tenantID(r), r.PathValue("register"), r.PathValue("row_id"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, rowPayload{Row: row, TraceID: traceID(r)})
}

// Options serves GET /admin/configuration/{register}/options.
func (h *Handler) Options(w http.ResponseWriter, r *http.Request) {
	opts, err := h.service.Options(r.Context(), tenantID(r), r.PathValue("register"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	if opts == nil {
		opts = []ports.RefOption{}
	}
	httpresponse.WriteJSON(w, http.StatusOK, optionsPayload{Options: opts, TraceID: traceID(r)})
}

// Usage serves GET /admin/configuration/{register}/{row_id}/usage.
func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	u, err := h.service.Usage(r.Context(), tenantID(r), r.PathValue("register"), r.PathValue("row_id"))
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	if u.Uses == nil {
		u.Uses = []domain.UsageCount{}
	}
	httpresponse.WriteJSON(w, http.StatusOK, usagePayload{Usage: u, Sentence: u.Sentence(), TraceID: traceID(r)})
}

// Create serves POST /admin/configuration/{register}.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body rowWrite
	if !decode(w, r, h.log, &body) {
		return
	}
	if body.Fields == nil {
		body.Fields = map[string]any{}
	}
	row, err := h.service.Create(r.Context(), writeParams(r, key), r.PathValue("register"), body.Fields)
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, rowPayload{Row: row, TraceID: traceID(r)})
}

// Update serves PUT /admin/configuration/{register}/{row_id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body rowWrite
	if !decode(w, r, h.log, &body) {
		return
	}
	if body.Fields == nil {
		body.Fields = map[string]any{}
	}
	row, err := h.service.Update(r.Context(), writeParams(r, key), r.PathValue("register"), r.PathValue("row_id"), body.Fields, body.RowVersion)
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, rowPayload{Row: row, TraceID: traceID(r)})
}

// SetStatus serves POST /admin/configuration/{register}/{row_id}/status.
func (h *Handler) SetStatus(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body statusWrite
	if !decode(w, r, h.log, &body) {
		return
	}
	row, err := h.service.SetStatus(r.Context(), writeParams(r, key), r.PathValue("register"), r.PathValue("row_id"), strings.TrimSpace(body.Status), body.RowVersion)
	if err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, rowPayload{Row: row, TraceID: traceID(r)})
}

// Delete serves DELETE /admin/configuration/{register}/{row_id}. The body is optional (a
// row_version fence); the Idempotency-Key header is not.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body deleteWrite
	if r.ContentLength != 0 {
		if !decode(w, r, h.log, &body) {
			return
		}
	}
	if err := h.service.Delete(r.Context(), writeParams(r, key), r.PathValue("register"), r.PathValue("row_id"), body.RowVersion); err != nil {
		writeErr(w, r, h.log, app.HTTPError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func idempotencyKey(w http.ResponseWriter, r *http.Request, log *slog.Logger) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeErr(w, r, log, app.BadRequest("missing_idempotency_key", "This could not be saved safely. Try again."))
		return "", false
	}
	return key, true
}

func decode(w http.ResponseWriter, r *http.Request, log *slog.Logger, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(w, r, log, app.BadRequest("invalid_body", "That request could not be read. Try again."))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeErr(w, r, log, app.BadRequest("invalid_body", "That request could not be read. Try again."))
		return false
	}
	return true
}

func writeErr(w http.ResponseWriter, r *http.Request, log *slog.Logger, appErr *app.Error) {
	if appErr == nil {
		return
	}
	// Both `code` and `error` are carried: admin-web's envelope parser keys on `code` (and reads
	// `field_errors` to mark inputs), older readers on `error`.
	body := map[string]any{"code": appErr.Code, "error": appErr.Code, "message": appErr.Message, "trace_id": traceID(r), "retryable": false, "field_errors": []domain.FieldError{}}
	if len(appErr.Fields) > 0 {
		body["field_errors"] = appErr.Fields
	}
	httpresponse.WriteError(w, r, log, appErr.HTTPStatus, body, errors.New(appErr.Code))
}

func tenantID(r *http.Request) string {
	return strings.TrimSpace(httpmiddleware.TenantIDFromContext(r.Context()))
}

func traceID(r *http.Request) string {
	return httpmiddleware.TraceIDFromContext(r.Context())
}

func writeParams(r *http.Request, key string) ports.WriteParams {
	return ports.WriteParams{
		TenantID:       tenantID(r),
		ActorID:        strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context())),
		IdempotencyKey: key,
		TraceID:        traceID(r),
	}
}
