// Package http serves the in-app notification centre: the caller's own notification feed
// and the mark-as-read write.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/app"
	"github.com/vgoats/goatos/backend/internal/notificationcentre/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Service is the behaviour this transport depends on.
type Service interface {
	List(ctx context.Context, req app.ListRequest) (domain.Page, error)
	MarkRead(ctx context.Context, req app.MarkReadRequest) (int, error)
}

// Handler serves the routes.
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

// Route patterns, named once so the handler and the permission-table test can assert on the
// SAME strings.
const (
	ListRoute     = "/app/notifications"
	MarkReadRoute = "/app/notifications/read"
)

// Register mounts the routes.
//
// Patterns here must stay byte-identical to the entries in permissions/routes.go -- the
// permission table is matched by method + pattern, and a mismatch serves the route ungated.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET "+ListRoute, h.List)
	mux.HandleFunc("POST "+MarkReadRoute, h.MarkRead)
}

// maxRequestBytes caps the write body: 200 uuids is about 8KB.
const maxRequestBytes = 64 * 1024

// List serves GET /app/notifications.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	// Unknown query params stay silently ignored on this READ path: the web bell and the
	// Android client evolve separately, and a stale extra param must not blank the feed.
	q := r.URL.Query()
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest("invalid_limit", "That page size is not valid."))
			return
		}
		limit = parsed
	}
	page, err := h.service.List(r.Context(), app.ListRequest{
		TenantID: tenantID(r),
		ActorID:  actorID(r),
		Cursor:   q.Get("cursor"),
		Limit:    limit,
	})
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	items := make([]notificationPayload, 0, len(page.Items))
	for _, n := range page.Items {
		items = append(items, toNotificationPayload(n))
	}
	httpresponse.WriteJSON(w, http.StatusOK, notificationPagePayload{
		Items:       items,
		UnreadCount: page.UnreadCount,
		NextCursor:  optional(page.NextCursor),
		TraceID:     traceID(r),
	})
}

// MarkRead serves POST /app/notifications/read.
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	key, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var body markReadRequestPayload
	if !h.decode(w, r, &body) {
		return
	}
	readCount, err := h.service.MarkRead(r.Context(), app.MarkReadRequest{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IDs:            body.NotificationRequestIDs,
		IdempotencyKey: key,
	})
	if err != nil {
		h.writeCause(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, markReadResponsePayload{ReadCount: readCount})
}

func (h *Handler) idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, app.BadRequest("missing_idempotency_key", "This could not be saved safely. Try again."))
		return "", false
	}
	if len(key) < 8 || len(key) > 200 {
		h.writeErr(w, r, app.BadRequest("invalid_idempotency_key", "This could not be saved safely. Try again."))
		return "", false
	}
	return key, true
}

// decode reads the JSON write body, failing loud on unknown fields.
func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That request could not be read. Try again."))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That request could not be read. Try again."))
		return false
	}
	return true
}

func (h *Handler) writeCause(w http.ResponseWriter, r *http.Request, err error) {
	appErr := app.HTTPError(err)
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"message": appErr.Message,
	}, fmt.Errorf("%s: %w", appErr.Code, err))
}

func (h *Handler) writeErr(w http.ResponseWriter, r *http.Request, appErr *app.Error) {
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"message": appErr.Message,
	}, errors.New(appErr.Code))
}

func tenantID(r *http.Request) string {
	return strings.TrimSpace(httpmiddleware.TenantIDFromContext(r.Context()))
}

// actorID is WHOSE feed this is. It comes from the authenticated session and from nowhere
// else: no query parameter, path segment, header or body field in this module names a
// person, so a caller has no way to ask for anybody's notifications but their own.
func actorID(r *http.Request) string {
	return strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context()))
}

func traceID(r *http.Request) string {
	return httpmiddleware.TraceIDFromContext(r.Context())
}
