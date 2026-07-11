// Package http exposes vaccination execution read APIs. Park/shed is the physical execution context of
// PC Vaccination work; Parks does not own these routes.
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
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	obligationports "github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// Reader is the vaccination execution read slice required by this handler.
type Reader interface {
	VaccinationExecution(ctx context.Context, q vaccexecd.ExecutionQuery) ([]vaccexecd.ExecutionRow, error)
	ShedDrilldown(ctx context.Context, q vaccexecd.ExecutionQuery) (vaccexecd.ShedDrilldown, bool, error)
	VaccinationOperations(ctx context.Context, q vaccexecd.OperationsQuery) (vaccexecd.OperationsResponse, error)
	ScanRoster(ctx context.Context, q vaccexecd.ScanRosterQuery) ([]vaccexecd.ScanRosterRow, error)
	// VaccinationGaps backs the mobile data-gaps overlay (animals excluded from coverage + reason).
	VaccinationGaps(ctx context.Context, q vaccexecd.GapsQuery) (vaccexecd.GapsResponse, error)
	// CoverageRollup backs the mobile doses-given overlay (per-vaccine given count + coverage %).
	CoverageRollup(ctx context.Context, q vaccexecd.OperationsQuery) (vaccexecd.CoverageResponse, error)
	// ShedSummary backs the shed-wise vaccination table (animal-level rollup + Manager/Backup).
	ShedSummary(ctx context.Context, q vaccexecd.ShedSummaryQuery) (vaccexecd.ShedSummaryResponse, error)
	// ShedDetail backs the shed drill-down header + per-vaccine breakdown.
	ShedDetail(ctx context.Context, shedID string, q vaccexecd.OperationsQuery) (vaccexecd.ShedDetailResponse, bool, error)
	// ShedAnimals backs the shed drill-down keyset-paginated animal roster.
	ShedAnimals(ctx context.Context, q vaccexecd.ShedAnimalQuery) (vaccexecd.ShedAnimalPage, error)
	// CapacityConfig backs the admin Config screen's read of the tenant daily vaccination cap.
	CapacityConfig(ctx context.Context, tenantID string) (vaccexecd.CapacityConfig, error)
}

// Writer is the obligation write interface needed for reschedule operations.
type Writer interface {
	// ReopenDeferredObligationByIdempotencyKey is the SM-2 health-recovery reopen path (a goat recovers
	// from a sick/ICU/quarantine defer). It is no longer called by this handler — RescheduleObligation
	// below uses RescheduleObligationByID instead — but stays part of the interface since it remains a
	// real, separately-used mechanism (backend/internal/vaccination/app/generation.go's
	// recoveryRescheduleForRule) and other Writer implementations may still need to satisfy it.
	ReopenDeferredObligationByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string, occurredAt time.Time, reschedule *domain.RecoveryReschedule) (string, bool, error)

	// RescheduleObligationByID reschedules an open (scheduled/due) obligation to a new due date in
	// place, targeted directly by obligation_id. This is the mobile "reschedule an overdue obligation"
	// write path and is deliberately separate from ReopenDeferredObligationByIdempotencyKey above,
	// which remains the only path that can reopen a health-held 'deferred' obligation (SM-2 recovery).
	// A 'missed' target is NOT mutated in place — missed is immutable closed history — instead a
	// brand-new obligation is created for the new due date and its id is returned; the missed row is
	// left untouched. Callers should treat the returned obligation_id as authoritative rather than
	// assuming it always equals the path's obligation_id.
	RescheduleObligationByID(ctx context.Context, tenantID, obligationID, idempotencyKey string, dueAt, windowStart time.Time, windowEnd *time.Time, occurredAt time.Time) (string, bool, error)
}

// Handler serves vaccination execution endpoints (park/shed execution context for PC Vaccination).
type Handler struct {
	reader Reader
	writer Writer
	log    *slog.Logger
}

// NewHandler constructs the vaccination execution handler.
func NewHandler(reader Reader, writer Writer, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{reader: reader, writer: writer, log: l}
}

// Register mounts the vaccination execution routes (owned by PC Vaccination, park/shed scope).
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /vaccination/execution", h.ListVaccinationExecution)
	mux.HandleFunc("GET /vaccination/execution/sheds/{shed_id}", h.GetShedDrilldown)
	mux.HandleFunc("GET /vaccination/operations", h.VaccinationOperations)
	mux.HandleFunc("GET /vaccination/sheds", h.ListShedSummary)
	mux.HandleFunc("GET /vaccination/sheds/{shed_id}", h.GetShedDetail)
	mux.HandleFunc("GET /vaccination/sheds/{shed_id}/animals", h.GetShedAnimals)
	mux.HandleFunc("GET /vaccination/capacity-config", h.GetCapacityConfig)
	mux.HandleFunc("GET /app/vaccination/execution/sheds/{shed_id}/roster", h.ScanRoster)
	mux.HandleFunc("POST /app/vaccination/obligations/{obligation_id}/reschedule", h.RescheduleObligation)
	mux.HandleFunc("GET /app/vaccination/gaps", h.VaccinationGaps)
	mux.HandleFunc("GET /app/vaccination/coverage", h.VaccinationCoverage)
}

// VaccinationOperations serves the source-backed cohort × protocol matrix + per-cohort detail for the
// /vaccination screen. Park scope (park_id) + as_of + due_before come from the top bar.
func (h *Handler) VaccinationOperations(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	asOf := time.Now().In(biztime.DefaultLocation())
	q := vaccexecd.OperationsQuery{
		TenantID:  tenantID(r),
		AsOf:      asOf,
		DueBefore: asOf.Add(defaultExecutionHorizonDays * 24 * time.Hour),
		Limit:     defaultDrilldownLimit,
	}
	if parkID := query.Get("park_id"); parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
			return
		}
		q.ParkID = &parkID
	}
	if asOfRaw := query.Get("as_of"); asOfRaw != "" {
		parsed, err := time.Parse(time.RFC3339, asOfRaw)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return
		}
		q.AsOf = parsed.In(biztime.DefaultLocation())
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
	}
	if dueBefore := query.Get("due_before"); dueBefore != "" {
		parsed, err := time.Parse(time.RFC3339, dueBefore)
		if err != nil {
			h.badRequest(w, r, "invalid_due_before", "due_before must be RFC3339")
			return
		}
		q.DueBefore = parsed.In(biztime.DefaultLocation())
	}
	if limit := query.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > maxExecutionLimit {
			n = maxExecutionLimit
		}
		q.Limit = n
	}
	resp, err := h.reader.VaccinationOperations(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

const (
	defaultExecutionLimit       = 200
	defaultDrilldownLimit       = 500
	maxExecutionLimit           = 500
	defaultExecutionHorizonDays = 30
)

var allowedWorkStates = map[vaccexecd.WorkState]bool{
	vaccexecd.WorkStateDue:                 true,
	vaccexecd.WorkStateOverdue:             true,
	vaccexecd.WorkStateScheduled:           true,
	vaccexecd.WorkStateInProgress:          true,
	vaccexecd.WorkStateProofPending:        true,
	vaccexecd.WorkStateVerificationPending: true,
	vaccexecd.WorkStateRejected:            true,
	vaccexecd.WorkStateDeferred:            true,
	vaccexecd.WorkStateMissed:              true,
	vaccexecd.WorkStateBlocked:             true,
	vaccexecd.WorkStateCompleted:           true,
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

// ListVaccinationExecution lists bounded vaccination execution rows grouped by park/shed context.
func (h *Handler) ListVaccinationExecution(w http.ResponseWriter, r *http.Request) {
	q, ok := h.executionQuery(w, r, defaultExecutionLimit)
	if !ok {
		return
	}
	rows, err := h.reader.VaccinationExecution(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, vaccexecd.ExecutionResponse{Source: vaccexecd.SourceAPI, Rows: rows})
}

// GetShedDrilldown returns the vaccination execution context for a single shed.
func (h *Handler) GetShedDrilldown(w http.ResponseWriter, r *http.Request) {
	shedID := r.PathValue("shed_id")
	if !uuidutil.IsUUIDString(shedID) {
		h.badRequest(w, r, "invalid_shed_id", "shed_id must be a UUID")
		return
	}
	q, ok := h.executionQuery(w, r, defaultDrilldownLimit)
	if !ok {
		return
	}
	q.ShedID = &shedID
	detail, found, err := h.reader.ShedDrilldown(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	if !found {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found", Message: "shed vaccination execution was not found", TraceID: traceID(r)}, nil)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, detail)
}

func (h *Handler) executionQuery(w http.ResponseWriter, r *http.Request, defaultLimit int) (vaccexecd.ExecutionQuery, bool) {
	query := r.URL.Query()
	asOf := time.Now().In(biztime.DefaultLocation())
	q := vaccexecd.ExecutionQuery{
		TenantID:  tenantID(r),
		AsOf:      asOf,
		DueBefore: asOf.Add(defaultExecutionHorizonDays * 24 * time.Hour),
		Limit:     defaultLimit,
	}

	if asOfRaw := query.Get("as_of"); asOfRaw != "" {
		parsed, err := time.Parse(time.RFC3339, asOfRaw)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return vaccexecd.ExecutionQuery{}, false
		}
		q.AsOf = parsed.In(biztime.DefaultLocation())
		// Re-anchor the default horizon to as_of; an explicit due_before below still wins.
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
	}
	if parkID := query.Get("park_id"); parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
			return vaccexecd.ExecutionQuery{}, false
		}
		q.ParkID = &parkID
	}
	if state := query.Get("work_state"); state != "" {
		workState := vaccexecd.WorkState(state)
		if !allowedWorkStates[workState] {
			h.badRequest(w, r, "invalid_work_state", "work_state must be a vaccination execution work state")
			return vaccexecd.ExecutionQuery{}, false
		}
		q.WorkState = &workState
	}
	if dueBefore := query.Get("due_before"); dueBefore != "" {
		parsed, err := time.Parse(time.RFC3339, dueBefore)
		if err != nil {
			h.badRequest(w, r, "invalid_due_before", "due_before must be RFC3339")
			return vaccexecd.ExecutionQuery{}, false
		}
		q.DueBefore = parsed.In(biztime.DefaultLocation())
	}
	if limit := query.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return vaccexecd.ExecutionQuery{}, false
		}
		if n > maxExecutionLimit {
			n = maxExecutionLimit
		}
		q.Limit = n
	}
	return q, true
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}

// ScanRoster returns per-animal vaccination obligations for a shed, with RFID tags and vaccine labels.
// Supports mobile scan screen keyboard-wedge tag matching.
func (h *Handler) ScanRoster(w http.ResponseWriter, r *http.Request) {
	shedID := r.PathValue("shed_id")
	if !uuidutil.IsUUIDString(shedID) {
		h.badRequest(w, r, "invalid_shed_id", "shed_id must be a UUID")
		return
	}
	query := r.URL.Query()
	limit := 500
	if limitRaw := query.Get("limit"); limitRaw != "" {
		n, err := strconv.Atoi(limitRaw)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > 5000 {
			n = 5000
		}
		limit = n
	}
	q := vaccexecd.ScanRosterQuery{
		TenantID: tenantID(r),
		ShedID:   shedID,
		Limit:    limit,
	}
	rows, err := h.reader.ScanRoster(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"source": "api",
		"rows":   rows,
	})
}

type rescheduleRequest struct {
	DueAt       time.Time  `json:"due_at"`
	WindowStart *time.Time `json:"window_start,omitempty"`
	WindowEnd   *time.Time `json:"window_end,omitempty"`
}

type rescheduleResponse struct {
	ObligationID     string `json:"obligation_id"`
	IdempotentReplay bool   `json:"idempotent_replay"`
}

// RescheduleObligation reschedules an open (scheduled/due) vaccination obligation to a new due date,
// targeted by obligation_id (not idempotency_key — the obligation_id path value is the actual
// write target). Idempotent via the Idempotency-Key header: an exact replay (same key + same
// due_at/window body) returns the original result without re-running the write; a same-key/
// different-payload replay is rejected with 409. A health-held ('deferred') obligation can never be
// reached through this endpoint — that recovery-reopen path stays exclusively owned by the SM-2 flow.
// A 'missed' target is immutable closed history: this endpoint reworks it onto a brand-new obligation
// for the new due date (a new obligation_id in the response) rather than mutating the missed row.
func (h *Handler) RescheduleObligation(w http.ResponseWriter, r *http.Request) {
	obligationID := r.PathValue("obligation_id")
	if !uuidutil.IsUUIDString(obligationID) {
		h.badRequest(w, r, "invalid_obligation_id", "obligation_id must be a UUID")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	defer r.Body.Close()
	var req rescheduleRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && err != io.EOF {
		h.badRequest(w, r, "invalid_body", "request body must be JSON")
		return
	}

	if req.DueAt.IsZero() {
		h.badRequest(w, r, "invalid_due_at", "due_at is required and must be RFC3339")
		return
	}
	if req.DueAt.Before(time.Now()) {
		h.badRequest(w, r, "due_at_in_past", "due_at must be in the future")
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		h.badRequest(w, r, "missing_idempotency_key", "Idempotency-Key header is required")
		return
	}

	windowStart := req.DueAt
	if req.WindowStart != nil {
		windowStart = *req.WindowStart
	}
	if req.WindowEnd != nil && req.WindowEnd.Before(windowStart) {
		h.badRequest(w, r, "invalid_window", "window_end must be greater than or equal to window_start")
		return
	}

	id, isReplay, err := h.writer.RescheduleObligationByID(r.Context(), tenantID(r), obligationID, idempotencyKey, req.DueAt, windowStart, req.WindowEnd, time.Now().UTC())
	if err != nil {
		if errors.Is(err, obligationports.ErrNotFound) {
			httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
				errorEnvelope{Code: "not_found", Message: "vaccination obligation was not found or is not open for reschedule", TraceID: traceID(r)}, nil)
			return
		}
		if errors.Is(err, obligationports.ErrIdempotencyConflict) {
			httpresponse.WriteError(w, r, h.log, http.StatusConflict,
				errorEnvelope{Code: "idempotency_conflict", Message: "Idempotency-Key was already used with a different request body", TraceID: traceID(r)}, nil)
			return
		}
		h.internal(w, r, err)
		return
	}

	httpresponse.WriteJSON(w, http.StatusOK, rescheduleResponse{
		ObligationID:     id,
		IdempotentReplay: isReplay,
	})
}

// VaccinationGaps returns the bounded, cursor-paginated animals excluded from the vaccination coverage
// denominator (missing date of birth / breed) for the mobile "Data gaps" overlay. Scoped by tenant +
// optional park_id, mirroring the existing park-scope pattern on /vaccination/execution and
// /vaccination/operations.
func (h *Handler) VaccinationGaps(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	q := vaccexecd.GapsQuery{TenantID: tenantID(r), Limit: defaultExecutionLimit}
	if parkID := query.Get("park_id"); parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
			return
		}
		q.ParkID = &parkID
	}
	if cursor := query.Get("cursor"); cursor != "" {
		if !uuidutil.IsUUIDString(cursor) {
			h.badRequest(w, r, "invalid_cursor", "cursor must be a goat UUID")
			return
		}
		q.Cursor = &cursor
	}
	if limit := query.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > maxExecutionLimit {
			n = maxExecutionLimit
		}
		q.Limit = n
	}
	resp, err := h.reader.VaccinationGaps(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// VaccinationCoverage returns the per-vaccine given-count + coverage % rollup for a scope, for the
// mobile "Doses given" overlay. Reuses the same park/as_of/due_before query parsing as
// /vaccination/operations, whose indexed cohort×protocol rollup this endpoint re-aggregates by
// protocol only (no new hot-table query).
func (h *Handler) VaccinationCoverage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	asOf := time.Now().In(biztime.DefaultLocation())
	q := vaccexecd.OperationsQuery{
		TenantID:  tenantID(r),
		AsOf:      asOf,
		DueBefore: asOf.Add(defaultExecutionHorizonDays * 24 * time.Hour),
		Limit:     defaultDrilldownLimit,
	}
	if parkID := query.Get("park_id"); parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
			return
		}
		q.ParkID = &parkID
	}
	if asOfRaw := query.Get("as_of"); asOfRaw != "" {
		parsed, err := time.Parse(time.RFC3339, asOfRaw)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return
		}
		q.AsOf = parsed.In(biztime.DefaultLocation())
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
	}
	if dueBefore := query.Get("due_before"); dueBefore != "" {
		parsed, err := time.Parse(time.RFC3339, dueBefore)
		if err != nil {
			h.badRequest(w, r, "invalid_due_before", "due_before must be RFC3339")
			return
		}
		q.DueBefore = parsed.In(biztime.DefaultLocation())
	}
	if limit := query.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > maxExecutionLimit {
			n = maxExecutionLimit
		}
		q.Limit = n
	}
	resp, err := h.reader.CoverageRollup(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

const (
	defaultShedLimit = 50
	maxShedLimit     = 200
	maxSearchLen     = 120
)

var allowedShedStatuses = map[vaccexecd.ShedStatus]bool{
	vaccexecd.ShedStatusNeedsReview: true,
	vaccexecd.ShedStatusSplit:       true,
	vaccexecd.ShedStatusOverdue:     true,
	vaccexecd.ShedStatusDue:         true,
	vaccexecd.ShedStatusScheduled:   true,
	vaccexecd.ShedStatusOnTrack:     true,
}

var allowedCapacityStatuses = map[vaccexecd.CapacityStatus]bool{
	vaccexecd.CapacityWithinCap: true,
	vaccexecd.CapacityOverCap:   true,
	vaccexecd.CapacityBreach:    true,
}

var allowedShedSorts = map[vaccexecd.ShedSummarySort]bool{
	vaccexecd.ShedSortStatus:     true,
	vaccexecd.ShedSortParkShed:   true,
	vaccexecd.ShedSortDueDesc:    true,
	vaccexecd.ShedSortAnimalDesc: true,
	vaccexecd.ShedSortNextDue:    true,
}

// ListShedSummary serves the shed-wise vaccination rollup: one animal-level row per shed with resolved
// Manager/Backup and derived Status, filterable by park/shed/status/search, sortable, offset-paginated.
func (h *Handler) ListShedSummary(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	asOf := time.Now().In(biztime.DefaultLocation())
	q := vaccexecd.ShedSummaryQuery{
		TenantID:  tenantID(r),
		AsOf:      asOf,
		DueBefore: asOf.Add(defaultExecutionHorizonDays * 24 * time.Hour),
		Sort:      vaccexecd.ShedSortStatus,
		Limit:     defaultShedLimit,
	}
	if asOfRaw := query.Get("as_of"); asOfRaw != "" {
		parsed, err := time.Parse(time.RFC3339, asOfRaw)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return
		}
		q.AsOf = parsed.In(biztime.DefaultLocation())
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
	}
	if parkID := query.Get("park_id"); parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
			return
		}
		q.ParkID = &parkID
	}
	if shedID := query.Get("shed_id"); shedID != "" {
		if !uuidutil.IsUUIDString(shedID) {
			h.badRequest(w, r, "invalid_shed_id", "shed_id must be a UUID")
			return
		}
		q.ShedID = &shedID
	}
	if status := query.Get("status"); status != "" {
		shedStatus := vaccexecd.ShedStatus(status)
		if !allowedShedStatuses[shedStatus] {
			h.badRequest(w, r, "invalid_status", "status must be needs_review, split, overdue, due, scheduled, or on_track")
			return
		}
		q.Status = &shedStatus
	}
	if capacity := query.Get("capacity"); capacity != "" {
		capStatus := vaccexecd.CapacityStatus(capacity)
		if !allowedCapacityStatuses[capStatus] {
			h.badRequest(w, r, "invalid_capacity", "capacity must be within_cap, over_cap, or capacity_breach")
			return
		}
		q.Capacity = &capStatus
	}
	if search := strings.TrimSpace(query.Get("q")); search != "" {
		if len(search) > maxSearchLen {
			search = search[:maxSearchLen]
		}
		q.Search = &search
	}
	if sort := query.Get("sort"); sort != "" {
		shedSort := vaccexecd.ShedSummarySort(sort)
		if !allowedShedSorts[shedSort] {
			h.badRequest(w, r, "invalid_sort", "sort must be park_shed, due_desc, animals_desc, or next_due")
			return
		}
		q.Sort = shedSort
	}
	if limit := query.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > maxShedLimit {
			n = maxShedLimit
		}
		q.Limit = n
	}
	// Pagination accepts either an explicit offset, or a 1-based page (offset = (page-1)*limit).
	if page := query.Get("page"); page != "" {
		n, err := strconv.Atoi(page)
		if err != nil || n < 1 {
			h.badRequest(w, r, "invalid_page", "page must be a positive integer")
			return
		}
		q.Offset = (n - 1) * q.Limit
	} else if offset := query.Get("offset"); offset != "" {
		n, err := strconv.Atoi(offset)
		if err != nil || n < 0 {
			h.badRequest(w, r, "invalid_offset", "offset must be a non-negative integer")
			return
		}
		q.Offset = n
	}
	resp, err := h.reader.ShedSummary(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// GetShedDetail returns one shed's header + per-vaccine obligation breakdown. 404 when the shed has no
// alive animals / is not an active shed.
func (h *Handler) GetShedDetail(w http.ResponseWriter, r *http.Request) {
	shedID := r.PathValue("shed_id")
	if !uuidutil.IsUUIDString(shedID) {
		h.badRequest(w, r, "invalid_shed_id", "shed_id must be a UUID")
		return
	}
	query := r.URL.Query()
	asOf := time.Now().In(biztime.DefaultLocation())
	q := vaccexecd.OperationsQuery{
		TenantID:  tenantID(r),
		AsOf:      asOf,
		DueBefore: asOf.Add(defaultExecutionHorizonDays * 24 * time.Hour),
		Limit:     defaultDrilldownLimit,
	}
	if asOfRaw := query.Get("as_of"); asOfRaw != "" {
		parsed, err := time.Parse(time.RFC3339, asOfRaw)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return
		}
		q.AsOf = parsed.In(biztime.DefaultLocation())
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
	}
	detail, found, err := h.reader.ShedDetail(r.Context(), shedID, q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	if !found {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found", Message: "shed vaccination detail was not found", TraceID: traceID(r)}, nil)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, detail)
}

// GetShedAnimals returns the shed's keyset-paginated alive-animal roster (Display ID + two tag
// identities + status) for the shed drill-down.
func (h *Handler) GetShedAnimals(w http.ResponseWriter, r *http.Request) {
	shedID := r.PathValue("shed_id")
	if !uuidutil.IsUUIDString(shedID) {
		h.badRequest(w, r, "invalid_shed_id", "shed_id must be a UUID")
		return
	}
	query := r.URL.Query()
	q := vaccexecd.ShedAnimalQuery{TenantID: tenantID(r), ShedID: shedID, Limit: 100}
	if cursor := query.Get("cursor"); cursor != "" {
		if !uuidutil.IsUUIDString(cursor) {
			h.badRequest(w, r, "invalid_cursor", "cursor must be a goat UUID")
			return
		}
		q.Cursor = &cursor
	}
	if limit := query.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n <= 0 {
			h.badRequest(w, r, "invalid_limit", "limit must be a positive integer")
			return
		}
		if n > 500 {
			n = 500
		}
		q.Limit = n
	}
	page, err := h.reader.ShedAnimals(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}

// GetCapacityConfig returns the tenant's daily vaccination cap config for the admin Config screen. Read
// authority is enforced at the permission layer (config authority: CEO/COO/superadmin).
func (h *Handler) GetCapacityConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.reader.CapacityConfig(r.Context(), tenantID(r))
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, cfg)
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
		errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, code, msg string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
		errorEnvelope{Code: code, Message: msg, TraceID: traceID(r)}, nil)
}
