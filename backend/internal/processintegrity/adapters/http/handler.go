// Package http exposes top-level process-integrity read APIs.
package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

type Reader interface {
	ActionCenter(ctx context.Context, q domain.Query) (domain.ActionCenterResponse, error)
	ActionCenterCounts(ctx context.Context, q domain.Query) (domain.ActionCenterCountsResponse, error)
	ProtocolAdherence(ctx context.Context, q domain.Query) (domain.ProtocolAdherenceResponse, error)
	ControlTower(ctx context.Context, q domain.Query) (domain.ControlTowerResponse, error)
	WorkflowDrilldown(ctx context.Context, q domain.Query, rowID string) (domain.WorkflowDrilldownResponse, bool, error)
}

type Handler struct {
	reader Reader
	log    *slog.Logger
	clock  func() time.Time
}

func NewHandler(reader Reader, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{reader: reader, log: l, clock: time.Now}
}

// WithClock overrides the wall clock for deterministic live-as-of tests.
// Production uses time.Now.
func (h *Handler) WithClock(clock func() time.Time) *Handler {
	if clock != nil {
		h.clock = clock
	}
	return h
}

func (h *Handler) now() time.Time {
	if h.clock == nil {
		return time.Now().In(biztime.DefaultLocation())
	}
	return h.clock().In(biztime.DefaultLocation())
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /action-center/obligations", h.ActionCenter)
	mux.HandleFunc("GET /vaccination/action-center", h.ActionCenter)
	mux.HandleFunc("GET /vaccination/action-center/counts", h.ActionCenterCounts)
	mux.HandleFunc("GET /vaccination/adherence", h.ProtocolAdherence)
	mux.HandleFunc("GET /control-tower/vaccination", h.ControlTower)
	mux.HandleFunc("GET /workflows/{row_id}", h.WorkflowDrilldown)
	mux.HandleFunc("GET /vaccination/workflows/{row_id}", h.WorkflowDrilldown)
}

const (
	defaultLimit       = 100
	defaultHorizonDays = 30
	maxLimit           = 500
	maxOffset          = 50000
)

var allowedWorkStates = map[domain.WorkState]bool{
	domain.WorkStateScheduled:           true,
	domain.WorkStateDue:                 true,
	domain.WorkStateOverdue:             true,
	domain.WorkStateInProgress:          true,
	domain.WorkStateProofPending:        true,
	domain.WorkStateVerificationPending: true,
	domain.WorkStateRejected:            true,
	domain.WorkStateDeferred:            true,
	domain.WorkStateMissed:              true,
	domain.WorkStateBlocked:             true,
	domain.WorkStateCompleted:           true,
}

var allowedSeverities = map[domain.Severity]bool{
	domain.SeverityOK:     true,
	domain.SeverityWatch:  true,
	domain.SeverityAtRisk: true,
	domain.SeverityBroken: true,
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

func (h *Handler) ActionCenter(w http.ResponseWriter, r *http.Request) {
	q, ok := h.query(w, r, defaultLimit)
	if !ok {
		return
	}
	resp, err := h.reader.ActionCenter(r.Context(), q)
	if err != nil {
		h.readError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) ActionCenterCounts(w http.ResponseWriter, r *http.Request) {
	q, ok := h.query(w, r, 1)
	if !ok {
		return
	}
	resp, err := h.reader.ActionCenterCounts(r.Context(), q)
	if err != nil {
		h.readError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) ProtocolAdherence(w http.ResponseWriter, r *http.Request) {
	q, ok := h.query(w, r, defaultLimit)
	if !ok {
		return
	}
	resp, err := h.reader.ProtocolAdherence(r.Context(), q)
	if err != nil {
		h.readError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) ControlTower(w http.ResponseWriter, r *http.Request) {
	q, ok := h.query(w, r, 50)
	if !ok {
		return
	}
	resp, err := h.reader.ControlTower(r.Context(), q)
	if err != nil {
		h.readError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) WorkflowDrilldown(w http.ResponseWriter, r *http.Request) {
	rowID := r.PathValue("row_id")
	if err := domain.ValidateRowID(rowID); err != nil {
		h.badRequest(w, r, "invalid_row_id", "row_id must be a supported workflow row id")
		return
	}
	q, ok := h.query(w, r, 1)
	if !ok {
		return
	}
	resp, found, err := h.reader.WorkflowDrilldown(r.Context(), q, rowID)
	if err != nil {
		h.readError(w, r, err)
		return
	}
	if !found {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found", Message: "workflow row was not found", TraceID: traceID(r)}, nil)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) query(w http.ResponseWriter, r *http.Request, defaultRowLimit int) (domain.Query, bool) {
	now := h.now()
	q := domain.Query{
		TenantID:  tenantID(r),
		AsOf:      now,
		DueBefore: now.Add(defaultHorizonDays * 24 * time.Hour),
		Limit:     defaultRowLimit,
	}
	values := r.URL.Query()
	if asOfRaw := values.Get("as_of"); asOfRaw != "" {
		parsed, err := biztime.ParseLiveAsOfRFC3339(asOfRaw, now)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return domain.Query{}, false
		}
		q.AsOf = parsed
		// Re-anchor the default horizon to as_of; an explicit due_before below still wins.
		q.DueBefore = q.AsOf.Add(defaultHorizonDays * 24 * time.Hour)
	}
	if parkID := values.Get("park_id"); parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
			return domain.Query{}, false
		}
		q.ParkID = &parkID
	}
	if shedID := values.Get("shed_id"); shedID != "" {
		if !uuidutil.IsUUIDString(shedID) {
			h.badRequest(w, r, "invalid_shed_id", "shed_id must be a UUID")
			return domain.Query{}, false
		}
		q.ShedID = &shedID
	}
	if categoryValue := values.Get("category"); categoryValue != "" {
		if categoryValue != domain.CategoryVaccination && categoryValue != domain.CategoryFeedDirection {
			h.badRequest(w, r, "invalid_category", "category must be vaccination or feed_direction")
			return domain.Query{}, false
		}
		q.Category = &categoryValue
	}
	if isVaccinationScopedRoute(r.URL.Path) {
		category := domain.CategoryVaccination
		q.Category = &category
	}
	stateValue := values.Get("work_state")
	if stateValue == "" {
		stateValue = values.Get("status")
	}
	if stateValue != "" {
		state := domain.WorkState(stateValue)
		if !allowedWorkStates[state] {
			h.badRequest(w, r, "invalid_work_state", "work_state must be a supported process work state")
			return domain.Query{}, false
		}
		q.WorkState = &state
	}
	if severityValue := values.Get("severity"); severityValue != "" {
		severity := domain.Severity(severityValue)
		if !allowedSeverities[severity] {
			h.badRequest(w, r, "invalid_severity", "severity must be ok, watch, at_risk, or broken")
			return domain.Query{}, false
		}
		q.Severity = &severity
	}
	if ownerID := values.Get("owner_id"); ownerID != "" {
		if !uuidutil.IsUUIDString(ownerID) {
			h.badRequest(w, r, "invalid_owner_id", "owner_id must be a UUID")
			return domain.Query{}, false
		}
		q.OwnerID = &ownerID
	}
	if versionID := values.Get("protocol_version_id"); versionID != "" {
		if !uuidutil.IsUUIDString(versionID) {
			h.badRequest(w, r, "invalid_protocol_version_id", "protocol_version_id must be a UUID")
			return domain.Query{}, false
		}
		q.ProtocolVersionID = &versionID
	}
	if dueAfter := values.Get("due_after"); dueAfter != "" {
		parsed, err := time.Parse(time.RFC3339, dueAfter)
		if err != nil {
			h.badRequest(w, r, "invalid_due_after", "due_after must be RFC3339")
			return domain.Query{}, false
		}
		parsed = parsed.In(biztime.DefaultLocation())
		q.DueAfter = &parsed
	}
	if dueBefore := values.Get("due_before"); dueBefore != "" {
		parsed, err := time.Parse(time.RFC3339, dueBefore)
		if err != nil {
			h.badRequest(w, r, "invalid_due_before", "due_before must be RFC3339")
			return domain.Query{}, false
		}
		q.DueBefore = parsed.In(biztime.DefaultLocation())
	}
	if limit := values.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return domain.Query{}, false
		}
		if n > maxLimit {
			n = maxLimit
		}
		q.Limit = n
	}
	if offset := values.Get("offset"); offset != "" {
		n, err := strconv.Atoi(offset)
		if err != nil || n < 0 {
			h.badRequest(w, r, "invalid_offset", "offset must be a non-negative integer")
			return domain.Query{}, false
		}
		if n > maxOffset {
			h.badRequest(w, r, "offset_too_large", "offset is too large; use filters or cursor pagination")
			return domain.Query{}, false
		}
		q.Offset = n
	}
	if cursorValue := values.Get("cursor"); cursorValue != "" {
		cursor, err := domain.DecodeCursor(cursorValue)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor must be a valid Action Center cursor")
			return domain.Query{}, false
		}
		q.Cursor = &cursor
	}
	return q, true
}

func isVaccinationScopedRoute(path string) bool {
	return path == "/control-tower/vaccination" ||
		path == "/vaccination/action-center" ||
		path == "/vaccination/adherence" ||
		strings.HasPrefix(path, "/vaccination/workflows/")
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
		errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
}

// readError maps a read-path error to an HTTP response. A projection-unavailable signal is an honest,
// retryable 503 (the read model has no serving version) — never a 500 and never a silent unbounded
// canonical fallback. Everything else is a 500.
func (h *Handler) readError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, domain.ErrProjectionUnavailable) {
		httpresponse.WriteError(w, r, h.log, http.StatusServiceUnavailable,
			errorEnvelope{Code: "projection_unavailable", Message: "process integrity read model is temporarily unavailable", TraceID: traceID(r)}, err)
		return
	}
	h.internal(w, r, err)
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, code, msg string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
		errorEnvelope{Code: code, Message: msg, TraceID: traceID(r)}, nil)
}
