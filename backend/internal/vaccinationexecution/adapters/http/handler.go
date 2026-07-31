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

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	obligationports "github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// Reader is the vaccination execution read slice required by this handler.
type Reader interface {
	VaccinationExecution(ctx context.Context, q vaccexecd.ExecutionQuery) ([]vaccexecd.ExecutionRow, error)
	VaccinationExecutionPage(ctx context.Context, q vaccexecd.ExecutionQuery) (vaccexecd.ExecutionResponse, error)
	ShedDrilldown(ctx context.Context, q vaccexecd.ExecutionQuery) (vaccexecd.ShedDrilldown, bool, error)
	VaccinationOperations(ctx context.Context, q vaccexecd.OperationsQuery) (vaccexecd.OperationsResponse, error)
	VaccinationSchedule(ctx context.Context, q vaccexecd.ScheduleQuery) (vaccexecd.OperationsResponse, error)
	DriveAssignments(ctx context.Context, q vaccexecd.DriveAssignmentQuery) (vaccexecd.DriveAssignmentResponse, error)
	ScanRoster(ctx context.Context, q vaccexecd.ScanRosterQuery) (vaccexecd.ScanRosterResult, error)
	TaskOptionValues(ctx context.Context, tenantID, taskID string) (vaccexecd.TaskOptionValuesResponse, error)
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
	// CapacityConfig backs the admin Config screen's read of the tenant daily operator animal cap.
	CapacityConfig(ctx context.Context, tenantID string) (vaccexecd.CapacityConfig, error)
	// OperatorAssignmentConfig backs the admin Config screen's read of the park's N-active-operators +
	// default-operator config plus every operator's authored shift.
	GetOperatorAssignmentConfig(ctx context.Context, tenantID, parkID string) (vaccexecapp.OperatorAssignmentConfigView, error)
	// AuthorizedParkOptions returns the canonical Postgres-backed park vocabulary the caller may act
	// in. parkIDs empty means "no park-scoped grant narrowing" (a tenant-wide actor), i.e. every
	// active park of the tenant. Ids and labels are `locations` data compiled by the backend -- the
	// park option list is never assembled or labelled in the frontend.
	AuthorizedParkOptions(ctx context.Context, tenantID string, parkIDs []string) ([]vaccexecd.ParkOption, error)

	// VaccinationCommandBoard returns the CEO closure view: KPIs, cohort matrix, shed dose matrix,
	// weekly given, and verification queue.
	VaccinationCommandBoard(ctx context.Context, q vaccexecd.CommandBoardQuery) (vaccexecd.CommandBoardResponse, error)
}

// OperatorAssignmentConfigWriter is the write slice for the operator assignment admin screen.
type OperatorAssignmentConfigWriter interface {
	UpdateOperatorAssignmentConfig(ctx context.Context, tenantID string, cfg vaccexecd.OperatorAssignmentConfig) (vaccexecd.OperatorAssignmentConfig, string, string, error)
}

// CapacityConfigWriter is the write slice for the admin capacity-config screen (daily operator animal
// cap + per-animal shot-cap override).
type CapacityConfigWriter interface {
	UpdateCapacityConfig(ctx context.Context, tenantID string, cfg vaccexecd.CapacityConfig) (vaccexecd.CapacityConfig, string, string, error)
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
	RescheduleObligationByID(ctx context.Context, tenantID, obligationID, idempotencyKey string, authorizedParkIDs []string, dueAt, windowStart time.Time, windowEnd *time.Time, occurredAt time.Time) (string, bool, error)
	UpsertVaccinationDriveDateOverride(ctx context.Context, override domain.VaccineDriveDateOverride) (*domain.VaccineDriveDateOverride, error)
}

// Handler serves vaccination execution endpoints (park/shed execution context for PC Vaccination).
type Handler struct {
	reader       Reader
	writer       Writer
	operatorCfgW OperatorAssignmentConfigWriter
	capacityCfgW CapacityConfigWriter
	log          *slog.Logger
	clock        func() time.Time
}

// NewHandler constructs the vaccination execution handler.
func NewHandler(reader Reader, writer Writer, log ...*slog.Logger) *Handler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &Handler{reader: reader, writer: writer, log: l, clock: time.Now}
}

// WithOperatorAssignmentConfigWriter attaches the operator assignment write path.
// Kept as a separate opt-in setter (rather than a NewHandler parameter) so existing call sites are
// unaffected; a handler without this set 500s the PUT route rather than silently no-op-ing.
func (h *Handler) WithOperatorAssignmentConfigWriter(w OperatorAssignmentConfigWriter) *Handler {
	h.operatorCfgW = w
	return h
}

// WithCapacityConfigWriter attaches the capacity-config write path. Kept as a separate opt-in setter
// (rather than a NewHandler parameter) so existing call sites are unaffected; a handler without this
// set 500s the PUT route rather than silently no-op-ing.
func (h *Handler) WithCapacityConfigWriter(w CapacityConfigWriter) *Handler {
	h.capacityCfgW = w
	return h
}

// WithClock overrides the wall clock for tests that need deterministic as-of
// clamping. Production uses time.Now.
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

// Register mounts the vaccination execution routes (owned by PC Vaccination, park/shed scope).
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /vaccination/command", h.GetVaccinationCommandBoard)
	mux.HandleFunc("GET /vaccination/execution", h.ListVaccinationExecution)
	mux.HandleFunc("GET /vaccination/execution/sheds/{shed_id}", h.GetShedDrilldown)
	mux.HandleFunc("GET /vaccination/operations", h.VaccinationOperations)
	mux.HandleFunc("GET /vaccination/schedule", h.VaccinationSchedule)
	mux.HandleFunc("POST /vaccination/schedule/drive-date-overrides", h.UpsertDriveDateOverride)
	mux.HandleFunc("GET /vaccination/drive-assignments", h.DriveAssignments)
	mux.HandleFunc("GET /vaccination/sheds", h.ListShedSummary)
	mux.HandleFunc("GET /vaccination/sheds/{shed_id}", h.GetShedDetail)
	mux.HandleFunc("GET /vaccination/sheds/{shed_id}/animals", h.GetShedAnimals)
	mux.HandleFunc("GET /vaccination/capacity-config", h.GetCapacityConfig)
	mux.HandleFunc("PUT /vaccination/capacity-config", h.PutCapacityConfig)
	mux.HandleFunc("GET /vaccination/operator-assignment/config", h.GetOperatorAssignmentConfig)
	mux.HandleFunc("PUT /vaccination/operator-assignment/config", h.PutOperatorAssignmentConfig)
	mux.HandleFunc("GET /app/vaccination/execution", h.ListVaccinationExecution)
	mux.HandleFunc("GET /app/vaccination/execution/sheds/{shed_id}", h.GetShedDrilldown)
	mux.HandleFunc("GET /app/vaccination/execution/sheds/{shed_id}/roster", h.ScanRoster)
	mux.HandleFunc("GET /app/vaccination/tasks/{task_id}/option-values", h.TaskOptionValues)
	mux.HandleFunc("POST /app/vaccination/obligations/{obligation_id}/reschedule", h.RescheduleObligation)
	mux.HandleFunc("GET /app/vaccination/gaps", h.VaccinationGaps)
	mux.HandleFunc("GET /app/vaccination/coverage", h.VaccinationCoverage)
}

type driveDateOverrideRequest struct {
	ParkID            string `json:"park_id"`
	VaccineCode       string `json:"vaccine_code"`
	OriginalDriveDate string `json:"original_drive_date"`
	OverrideDate      string `json:"override_date"`
	Reason            string `json:"reason"`
}

type driveDateOverrideResponse struct {
	ParkID            string `json:"park_id"`
	VaccineCode       string `json:"vaccine_code"`
	OriginalDriveDate string `json:"original_drive_date"`
	OverrideDate      string `json:"override_date"`
	Reason            string `json:"reason"`
	CreatedBy         string `json:"created_by"`
	CreatedAt         string `json:"created_at"`
}

func (h *Handler) UpsertDriveDateOverride(w http.ResponseWriter, r *http.Request) {
	if h.writer == nil {
		httpresponse.WriteError(w, r, h.log, http.StatusServiceUnavailable,
			errorEnvelope{Code: "override_unavailable", Message: "vaccination drive date override writer is not wired", TraceID: traceID(r)}, nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || len(body) == 0 {
		h.badRequest(w, r, "invalid_body", "request body is required")
		return
	}
	var req driveDateOverrideRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.badRequest(w, r, "invalid_json", "request body is not valid JSON")
		return
	}
	if !uuidutil.IsUUIDString(req.ParkID) {
		h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
		return
	}
	req.VaccineCode = strings.TrimSpace(req.VaccineCode)
	if req.VaccineCode == "" {
		h.badRequest(w, r, "invalid_vaccine_code", "vaccine_code is required")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		h.badRequest(w, r, "invalid_reason", "reason is required")
		return
	}
	original, err := time.ParseInLocation("2006-01-02", req.OriginalDriveDate, biztime.DefaultLocation())
	if err != nil {
		h.badRequest(w, r, "invalid_original_drive_date", "original_drive_date must be YYYY-MM-DD")
		return
	}
	overrideDate, err := time.ParseInLocation("2006-01-02", req.OverrideDate, biztime.DefaultLocation())
	if err != nil {
		h.badRequest(w, r, "invalid_override_date", "override_date must be YYYY-MM-DD")
		return
	}
	nowLocal := h.now().In(biztime.DefaultLocation())
	today := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, biztime.DefaultLocation())
	if overrideDate.Before(today) {
		h.badRequest(w, r, "invalid_override_date", "override_date must not be before today")
		return
	}
	if overrideDate.Before(original) {
		h.badRequest(w, r, "invalid_override_date", "override_date must not be before the original drive date")
		return
	}
	actorID := strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context()))
	if !uuidutil.IsUUIDString(actorID) {
		h.badRequest(w, r, "missing_actor", "authenticated actor id is required")
		return
	}
	out, err := h.writer.UpsertVaccinationDriveDateOverride(r.Context(), domain.VaccineDriveDateOverride{
		TenantID:          tenantID(r),
		ParkID:            req.ParkID,
		VaccineCode:       req.VaccineCode,
		OriginalDriveDate: original,
		OverrideDate:      overrideDate,
		Reason:            req.Reason,
		CreatedBy:         actorID,
		CreatedAt:         h.now(),
	})
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, driveDateOverrideResponse{
		ParkID:            out.ParkID,
		VaccineCode:       out.VaccineCode,
		OriginalDriveDate: out.OriginalDriveDate.In(biztime.DefaultLocation()).Format("2006-01-02"),
		OverrideDate:      out.OverrideDate.In(biztime.DefaultLocation()).Format("2006-01-02"),
		Reason:            out.Reason,
		CreatedBy:         out.CreatedBy,
		CreatedAt:         out.CreatedAt.In(biztime.DefaultLocation()).Format(time.RFC3339),
	})
}

// VaccinationOperations serves the source-backed cohort × protocol matrix + per-cohort detail for the
// /vaccination screen. Park scope (park_id) + as_of + due_before come from the top bar.
func (h *Handler) VaccinationOperations(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	asOf := h.now()
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
		parsed, err := biztime.ParseLiveAsOfRFC3339(asOfRaw, asOf)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return
		}
		q.AsOf = parsed
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
		if h.rejectHistoricalAsOf(w, r, parsed, asOf) {
			return
		}
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
	if rawCursor := query.Get("cursor"); rawCursor != "" {
		cursor, err := vaccexecd.DecodeOperationsCursor(rawCursor)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor must be a valid vaccination operations cursor")
			return
		}
		q.Cursor = &cursor
	}
	if !h.applyOperationsParkScope(w, r, &q) {
		return
	}
	resp, err := h.reader.VaccinationOperations(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// VaccinationSchedule serves the Full Schedule month/window directly from canonical vaccination
// obligations and completions.
func (h *Handler) VaccinationSchedule(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	now := h.now()
	year := now.Year()
	month := int(now.Month())
	if raw := query.Get("year"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < now.Year()-1 || n > now.Year()+5 {
			h.badRequest(w, r, "invalid_year", "year must be within the supported schedule window")
			return
		}
		year = n
	}
	if raw := query.Get("month"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 12 {
			h.badRequest(w, r, "invalid_month", "month must be 1-12")
			return
		}
		month = n
	}
	q := vaccexecd.ScheduleQuery{
		TenantID:   tenantID(r),
		MonthStart: time.Date(year, time.Month(month), 1, 0, 0, 0, 0, biztime.DefaultLocation()),
		Limit:      defaultDrilldownLimit,
	}
	if parkID := query.Get("park_id"); parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
			return
		}
		q.ParkID = &parkID
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
	if rawCursor := query.Get("cursor"); rawCursor != "" {
		cursor, err := vaccexecd.DecodeOperationsCursor(rawCursor)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor must be a valid vaccination schedule cursor")
			return
		}
		q.Cursor = &cursor
	}
	if !h.applyScheduleParkScope(w, r, &q) {
		return
	}
	resp, err := h.reader.VaccinationSchedule(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// DriveAssignments serves the persisted operator-cap schedule ledger. This is the date/operator/shed/
// partition table used by the vaccination drive UI; it is not a vaccine-dose aggregate.
func (h *Handler) DriveAssignments(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	now := h.now()
	year := now.Year()
	month := int(now.Month())
	if raw := query.Get("year"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < now.Year()-1 || n > now.Year()+5 {
			h.badRequest(w, r, "invalid_year", "year must be within the supported schedule window")
			return
		}
		year = n
	}
	if raw := query.Get("month"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 12 {
			h.badRequest(w, r, "invalid_month", "month must be 1-12")
			return
		}
		month = n
	}
	q := vaccexecd.DriveAssignmentQuery{
		TenantID:   tenantID(r),
		MonthStart: time.Date(year, time.Month(month), 1, 0, 0, 0, 0, biztime.DefaultLocation()),
		Limit:      defaultDrilldownLimit,
	}
	if parkID := query.Get("park_id"); parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
			return
		}
		q.ParkID = &parkID
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
	if !h.applyDriveAssignmentParkScope(w, r, &q) {
		return
	}
	resp, err := h.reader.DriveAssignments(r.Context(), q)
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

var allowedSeverities = map[vaccexecd.Severity]bool{
	vaccexecd.SeverityOK:     true,
	vaccexecd.SeverityWatch:  true,
	vaccexecd.SeverityAtRisk: true,
	vaccexecd.SeverityBroken: true,
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
	page, err := h.reader.VaccinationExecutionPage(r.Context(), q)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	// A leadership read on an app execution route is OVERSIGHT: read-only, park-scoped, every
	// shed. Vaccination execution belongs to the operator the drive is assigned to -- CBE to one
	// operator, CPT to the other -- so a director sees both parks and opens neither into the
	// scan/submit loop (maintainer decision; supersedes the earlier carve-out that kept the shed
	// click open for a PC Director because the role happens to hold task.execute).
	//
	// Holding task.execute is no longer sufficient here: the scan and submit writes are refused
	// `task_not_assigned` for a non-assignee anyway, so leaving the click open produced a scan
	// screen that recorded a proof video and then failed every write in background sync.
	//
	// Weighing is deliberately NOT gated this way: it is free-flow, and a director is allowed to
	// weigh anything. This branch is scoped to the vaccination execution routes only.
	if isAppExecutionRoute(r) && h.isLeadershipExecutionActor(r) {
		page.ViewerReadOnly = true
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
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
	asOf := h.now()
	q := vaccexecd.ExecutionQuery{
		TenantID:  tenantID(r),
		AsOf:      asOf,
		DueBefore: asOf.Add(defaultExecutionHorizonDays * 24 * time.Hour),
		Limit:     defaultLimit,
	}
	// On the app/mobile execution routes a field OPERATOR sees only their assigned work
	// (OperatorScopeActorID). A leadership principal (CEO/CXO, PC Director, Park Head) is not
	// an assigned operator, so operator scoping would return zero rows; they instead get a
	// read-only view of ALL sheds/partitions in their authorized park(s) (park scope is applied
	// by applyExecutionParkScope -> ResolveAuthorizedParkScope: CEO = all parks, Park Head = his
	// park). Scan/capture stays blocked on the client (operator capability); this only opens the
	// read.
	if isAppExecutionRoute(r) && !h.isLeadershipExecutionActor(r) {
		q.OperatorScopeActorID = httpmiddleware.ActorIDFromContext(r.Context())
		if q.OperatorScopeActorID == "" || !uuidutil.IsUUIDString(q.OperatorScopeActorID) {
			h.badRequest(w, r, "operator_scope_required", "app vaccination execution requires an authenticated operator scope")
			return vaccexecd.ExecutionQuery{}, false
		}
	}

	if asOfRaw := query.Get("as_of"); asOfRaw != "" {
		parsed, err := biztime.ParseLiveAsOfRFC3339(asOfRaw, asOf)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return vaccexecd.ExecutionQuery{}, false
		}
		q.AsOf = parsed
		// Re-anchor the default horizon to as_of; an explicit due_before below still wins.
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
		if h.rejectHistoricalAsOf(w, r, parsed, asOf) {
			return vaccexecd.ExecutionQuery{}, false
		}
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
	if raw := query.Get("severity"); raw != "" {
		severity := vaccexecd.Severity(raw)
		if !allowedSeverities[severity] {
			h.badRequest(w, r, "invalid_severity", "severity must be a vaccination execution severity")
			return vaccexecd.ExecutionQuery{}, false
		}
		q.Severity = &severity
	}
	if raw := query.Get("open_only"); raw != "" {
		openOnly, err := strconv.ParseBool(raw)
		if err != nil {
			h.badRequest(w, r, "invalid_open_only", "open_only must be true or false")
			return vaccexecd.ExecutionQuery{}, false
		}
		q.OpenOnly = openOnly
	}
	if raw := query.Get("include_filter_options"); raw != "" {
		includeFilterOptions, err := strconv.ParseBool(raw)
		if err != nil {
			h.badRequest(w, r, "invalid_include_filter_options", "include_filter_options must be true or false")
			return vaccexecd.ExecutionQuery{}, false
		}
		q.IncludeFilterOptions = includeFilterOptions
	}
	if raw := query.Get("cursor"); raw != "" {
		cursor, err := vaccexecd.DecodeExecutionCursor(raw)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor must be a valid vaccination execution cursor")
			return vaccexecd.ExecutionQuery{}, false
		}
		q.Cursor = &cursor
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
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	if len(grants) > 0 && !httpmiddleware.HasTenantWideGrant(grants, tenantID(r)) {
		q.AuthorizedParkIDs = httpmiddleware.AuthorizedParkIDs(grants)
		if q.AuthorizedParkIDs == nil {
			q.AuthorizedParkIDs = []string{}
		}
	}
	if !h.applyExecutionParkScope(w, r, &q) {
		return vaccexecd.ExecutionQuery{}, false
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
	// task_id is OPTIONAL: present -> task-scoped roster; absent -> shed-wide roster (keeps the
	// current app, which does not yet send task_id, working instead of 400-ing).
	taskID := query.Get("task_id")
	if taskID != "" && !uuidutil.IsUUIDString(taskID) {
		h.badRequest(w, r, "invalid_task_id", "task_id must be a UUID")
		return
	}
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
	actorID := httpmiddleware.ActorIDFromContext(r.Context())
	if actorID == "" || !uuidutil.IsUUIDString(actorID) {
		h.badRequest(w, r, "operator_scope_required", "app vaccination roster requires an authenticated operator scope")
		return
	}
	q := vaccexecd.ScanRosterQuery{
		TenantID:             tenantID(r),
		ShedID:               shedID,
		TaskID:               taskID,
		OperatorScopeActorID: actorID,
		Limit:                limit,
	}
	// The scan roster stays OPERATOR-ASSIGNMENT scoped for every caller, leadership included.
	// Vaccination execution belongs to the operator the drive is assigned to; a director oversees
	// both parks read-only and must never receive scan-roster animals, because the writes that
	// screen exists to make are refused as `task_not_assigned` anyway. Widening this read for
	// leadership (briefly done to explain an empty roster) handed a director a fully populated
	// scan screen whose every write then failed silently in background sync.
	//
	// Weighing is deliberately NOT like this: it is free-flow, so it has its own gate and must not
	// inherit this assignment scoping.
	if rawCursor := query.Get("cursor"); rawCursor != "" {
		cursor, err := vaccexecd.DecodeScanRosterCursor(rawCursor)
		if err != nil {
			h.badRequest(w, r, "invalid_cursor", "cursor must be a valid scan roster cursor")
			return
		}
		q.Cursor = &cursor
	}
	result, err := h.reader.ScanRoster(r.Context(), q)
	if err != nil {
		h.writeReadError(w, r, err)
		return
	}
	response := map[string]interface{}{
		"source": "api",
		"rows":   result.Rows,
	}
	if len(result.Rows) > 0 {
		row := result.Rows[0]
		response["taskId"] = row.TaskID
		response["batchId"] = row.BatchID
		response["sopVersionId"] = row.SOPVersionID
		response["taskRowVersion"] = row.TaskRowVersion
	}
	if result.NextCursor != nil {
		encoded, err := vaccexecd.EncodeScanRosterCursor(*result.NextCursor)
		if err != nil {
			h.internal(w, r, err)
			return
		}
		response["next_cursor"] = encoded
	}
	httpresponse.WriteJSON(w, http.StatusOK, response)
}

func isAppExecutionRoute(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/app/vaccination/execution")
}

// leadershipExecutionRoles get the park-scoped read-only oversight view of vaccination
// execution on the app routes, rather than operator-assignment-scoped work.
var leadershipExecutionRoles = map[string]bool{
	permissions.RoleCEOInternal: true,
	permissions.RolePCDirector:  true,
	permissions.RoleParkHead:    true,
}

// isLeadershipExecutionActor reports whether any of the caller's active grants is a
// leadership role, in which case the app execution read is NOT operator-assignment scoped.
func (h *Handler) isLeadershipExecutionActor(r *http.Request) bool {
	for _, g := range httpmiddleware.AuthGrantsFromContext(r.Context()) {
		if leadershipExecutionRoles[g.Role] {
			return true
		}
	}
	return false
}

func (h *Handler) canExecuteTasks(r *http.Request) bool {
	for _, g := range httpmiddleware.AuthGrantsFromContext(r.Context()) {
		if permissions.RoleHasPermission(g.Role, permissions.TaskExecute) {
			return true
		}
	}
	return false
}

func (h *Handler) TaskOptionValues(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("task_id")
	if !uuidutil.IsUUIDString(taskID) {
		h.badRequest(w, r, "invalid_task_id", "task_id must be a UUID")
		return
	}
	response, err := h.reader.TaskOptionValues(r.Context(), tenantID(r), taskID)
	if err != nil {
		h.writeReadError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, response)
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
	if req.DueAt.Before(h.now()) {
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

	requestTenantID := tenantID(r)
	var authorizedParkIDs []string
	grants := httpmiddleware.AuthGrantsFromContext(r.Context())
	if len(grants) > 0 && !httpmiddleware.HasTenantWideGrant(grants, requestTenantID) {
		authorizedParkIDs = httpmiddleware.AuthorizedParkIDs(grants)
		if authorizedParkIDs == nil {
			authorizedParkIDs = []string{}
		}
	}
	// india-date-guard:ignore: owner=ravi issue=GH-india-date scope=reschedule-decision-comparison-instant expiry=2026-12-31
	id, isReplay, err := h.writer.RescheduleObligationByID(r.Context(), requestTenantID, obligationID, idempotencyKey, authorizedParkIDs, req.DueAt, windowStart, req.WindowEnd, h.now().UTC())
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
	if !h.applyGapsParkScope(w, r, &q) {
		return
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
	asOf := h.now()
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
		parsed, err := parseScheduleDrilldownAsOfRFC3339(asOfRaw)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return
		}
		q.AsOf = parsed
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
		if h.rejectHistoricalAsOf(w, r, parsed, asOf) {
			return
		}
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
	if !h.applyOperationsParkScope(w, r, &q) {
		return
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
	asOf := h.now()
	q := vaccexecd.ShedSummaryQuery{
		TenantID:  tenantID(r),
		AsOf:      asOf,
		DueBefore: asOf.Add(defaultExecutionHorizonDays * 24 * time.Hour),
		Sort:      vaccexecd.ShedSortStatus,
		Limit:     defaultShedLimit,
	}
	if asOfRaw := query.Get("as_of"); asOfRaw != "" {
		parsed, err := biztime.ParseLiveAsOfRFC3339(asOfRaw, asOf)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return
		}
		q.AsOf = parsed
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
		if h.rejectHistoricalAsOf(w, r, parsed, asOf) {
			return
		}
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
			h.badRequest(w, r, "invalid_status", "status must be overdue, needs_review, split, due, scheduled, or on_track")
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
	if !h.applyShedSummaryParkScope(w, r, &q) {
		return
	}
	resp, err := h.reader.ShedSummary(r.Context(), q)
	if err != nil {
		h.readShedError(w, r, err)
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
	asOf := h.now()
	q := vaccexecd.OperationsQuery{
		TenantID:  tenantID(r),
		AsOf:      asOf,
		DueBefore: asOf.Add(defaultExecutionHorizonDays * 24 * time.Hour),
		Limit:     defaultDrilldownLimit,
	}
	if asOfRaw := query.Get("as_of"); asOfRaw != "" {
		parsed, err := parseScheduleDrilldownAsOfRFC3339(asOfRaw)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return
		}
		q.AsOf = parsed
		q.DueBefore = q.AsOf.Add(defaultExecutionHorizonDays * 24 * time.Hour)
		if h.rejectHistoricalAsOf(w, r, parsed, asOf) {
			return
		}
	}
	detail, found, err := h.reader.ShedDetail(r.Context(), shedID, q)
	if err != nil {
		h.readShedError(w, r, err)
		return
	}
	if !found {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found", Message: "shed vaccination detail was not found", TraceID: traceID(r)}, nil)
		return
	}
	if !h.allowParkID(w, r, detail.ParkID) {
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
	asOf := h.now()
	q := vaccexecd.ShedAnimalQuery{TenantID: tenantID(r), ShedID: shedID, AsOf: asOf, Limit: 100}
	if asOfRaw := query.Get("as_of"); asOfRaw != "" {
		parsed, err := parseScheduleDrilldownAsOfRFC3339(asOfRaw)
		if err != nil {
			h.badRequest(w, r, "invalid_as_of", "as_of must be RFC3339")
			return
		}
		if h.rejectHistoricalAsOf(w, r, parsed, asOf) {
			return
		}
		asOf = parsed
		q.AsOf = parsed
	}
	if driveDueRaw := query.Get("drive_due_date"); driveDueRaw != "" {
		parsed, err := time.ParseInLocation("2006-01-02", driveDueRaw, biztime.DefaultLocation())
		if err != nil {
			h.badRequest(w, r, "invalid_drive_due_date", "drive_due_date must be YYYY-MM-DD")
			return
		}
		q.DriveDueDate = &parsed
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
		if n > 500 {
			n = 500
		}
		q.Limit = n
	}
	detail, found, err := h.reader.ShedDetail(r.Context(), shedID, vaccexecd.OperationsQuery{
		TenantID:  tenantID(r),
		AsOf:      asOf,
		DueBefore: asOf.Add(defaultExecutionHorizonDays * 24 * time.Hour),
		Limit:     1,
	})
	if err != nil {
		h.readShedError(w, r, err)
		return
	}
	if !found {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found", Message: "shed vaccination detail was not found", TraceID: traceID(r)}, nil)
		return
	}
	if !h.allowParkID(w, r, detail.ParkID) {
		return
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

// GetCapacityConfig returns the tenant's daily operator animal cap config for the admin Config screen. Read
// authority is enforced at the permission layer (config authority: CEO/CXO).
func (h *Handler) GetCapacityConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.reader.CapacityConfig(r.Context(), tenantID(r))
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, cfg)
}

// updateCapacityConfigRequest is the PUT body: max animals/operator/day + the optional per-animal
// shot-cap override + the row_version the admin last read (optimistic concurrency).
type updateCapacityConfigRequest struct {
	MaxPerDay                 int    `json:"maxPerDay"`
	CapacityScope             string `json:"capacityScope"`
	MaxBufferDays             int    `json:"maxBufferDays"`
	OverflowPolicy            string `json:"overflowPolicy"`
	RowVersion                int    `json:"rowVersion"`
	MaxShotsPerAnimalPerDrive *int   `json:"maxShotsPerAnimalPerDrive"`
}

// PutCapacityConfig writes the tenant's daily operator animal cap + per-animal shot-cap override.
// Write authority is enforced at the permission layer (config authority: CEO/CXO). Validate-or-reject:
// an invalid maxPerDay or out-of-range maxShotsPerAnimalPerDrive returns 400, never a silently-applied
// default. A row_version mismatch returns 409. A successful write cascades vaccination.capacity.changed
// (one per active park -- see UpsertCapacityConfig) which re-plans future vaccination drives.
func (h *Handler) PutCapacityConfig(w http.ResponseWriter, r *http.Request) {
	if h.capacityCfgW == nil {
		h.internal(w, r, errors.New("vaccination execution: capacity config writer is not wired"))
		return
	}
	var req updateCapacityConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.badRequest(w, r, "invalid_body", "request body must be valid JSON")
		return
	}
	// capacityScope/maxBufferDays/overflowPolicy default to the current tenant row's values when the
	// request omits them, so a PUT that only sets maxPerDay/maxShotsPerAnimalPerDrive never clobbers
	// the other fields with zero values.
	current, err := h.reader.CapacityConfig(r.Context(), tenantID(r))
	if err != nil {
		h.internal(w, r, err)
		return
	}
	cfg := vaccexecd.CapacityConfig{
		MaxPerDay:                 req.MaxPerDay,
		CapacityScope:             current.CapacityScope,
		MaxBufferDays:             current.MaxBufferDays,
		OverflowPolicy:            current.OverflowPolicy,
		RowVersion:                req.RowVersion,
		MaxShotsPerAnimalPerDrive: req.MaxShotsPerAnimalPerDrive,
	}
	if strings.TrimSpace(req.CapacityScope) != "" {
		cfg.CapacityScope = req.CapacityScope
	}
	if strings.TrimSpace(req.OverflowPolicy) != "" {
		cfg.OverflowPolicy = req.OverflowPolicy
	}
	if req.MaxBufferDays != 0 {
		cfg.MaxBufferDays = req.MaxBufferDays
	}
	updated, code, msg, err := h.capacityCfgW.UpdateCapacityConfig(r.Context(), tenantID(r), cfg)
	if code != "" {
		h.badRequest(w, r, code, msg)
		return
	}
	if err != nil {
		if errors.Is(err, vaccexecapp.ErrCapacityConfigConflict) {
			httpresponse.WriteError(w, r, h.log, http.StatusConflict,
				errorEnvelope{Code: "row_version_conflict", Message: "capacity config was updated by someone else; reload and retry", TraceID: traceID(r)}, nil)
			return
		}
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, updated)
}

// operatorAssignmentConfigResponse is the wire shape for GET/PUT operator-assignment config: the N/
// default config plus every operator's authored shift, so the admin Config screen renders in one call.
type operatorAssignmentConfigResponse struct {
	ParkID                string                    `json:"parkId"`
	ActiveOperatorsPerDay int                       `json:"activeOperatorsPerDay"`
	DefaultOperatorID     string                    `json:"defaultOperatorId"`
	SelectedOperatorIDs   []string                  `json:"selectedOperatorIds,omitempty"`
	RowVersion            int64                     `json:"rowVersion"`
	Shifts                []vaccexecd.OperatorShift `json:"shifts"`
}

// GetOperatorAssignmentConfig returns the park's N-active-operators + default operator config, plus
// every operator's authored shift. Read authority is enforced at the permission layer (config authority:
// CEO/CXO). The drive scheduler consumes the saved row when selecting daily operators.
func (h *Handler) GetOperatorAssignmentConfig(w http.ResponseWriter, r *http.Request) {
	// BUG-019: park scope is BACKEND-owned. park_id is optional; when omitted the
	// actor's own authorized park scope resolves it, and the resolved id is echoed
	// in the response so the client scopes its roster reads to exactly one park
	// instead of inferring a park from an unscoped roster's first row.
	requested := strings.TrimSpace(r.URL.Query().Get("park_id"))
	if requested != "" && !uuidutil.IsUUIDString(requested) {
		h.badRequest(w, r, "invalid_park_id", "park_id must be a UUID")
		return
	}
	parkID, ok := h.authorizedParkID(w, r, requested)
	if !ok {
		return
	}
	if parkID == "" {
		// The caller's grants did not narrow to one park. Resolve the park vocabulary they may act
		// in from canonical `locations` data before deciding: a tenant whose caller can only reach a
		// single park is NOT ambiguous and stays zero-click; a genuinely multi-park caller is refused
		// -- silently picking one would let a CEO save a default operator for park A while reading a
		// roster blended across A+B+C -- but the refusal carries the backend-owned options so the
		// client renders a selector instead of dead-ending.
		grants := httpmiddleware.AuthGrantsFromContext(r.Context())
		var scopedParkIDs []string
		if len(grants) > 0 && !httpmiddleware.HasTenantWideGrant(grants, tenantID(r)) {
			scopedParkIDs = httpmiddleware.AuthorizedParkIDs(grants)
		}
		parks, err := h.reader.AuthorizedParkOptions(r.Context(), tenantID(r), scopedParkIDs)
		if err != nil {
			h.internal(w, r, err)
			return
		}
		if len(parks) == 1 {
			parkID = parks[0].ParkID
		} else {
			httpresponse.WriteError(w, r, h.log, http.StatusConflict,
				parkScopeAmbiguousEnvelope{
					Code:           "park_scope_ambiguous",
					Message:        parkScopeAmbiguousMessage(len(parks)),
					TraceID:        traceID(r),
					AvailableParks: append([]vaccexecd.ParkOption{}, parks...),
				}, nil)
			return
		}
	}
	view, err := h.reader.GetOperatorAssignmentConfig(r.Context(), tenantID(r), parkID)
	if err != nil {
		if errors.Is(err, vaccexecapp.ErrOperatorAssignmentConfigNotFound) {
			httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
				errorEnvelope{Code: "not_found", Message: "no operator assignment config authored for this park yet", TraceID: traceID(r)}, nil)
			return
		}
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, operatorAssignmentConfigResponse{
		ParkID:                parkID,
		ActiveOperatorsPerDay: view.Config.ActiveOperatorsPerDay,
		DefaultOperatorID:     view.Config.DefaultOperatorID,
		SelectedOperatorIDs:   append([]string{}, view.Config.SelectedOperatorIDs...),
		RowVersion:            view.Config.RowVersion,
		Shifts:                view.Shifts,
	})
}

// parkScopeAmbiguousEnvelope is the 409 body for GET /vaccination/operator-assignment/config when the
// caller's authorized scope covers more than one park. It carries the BACKEND-OWNED park vocabulary
// (canonical Postgres ids + labels) the caller may choose from, so admin-web renders a selector and
// re-requests with park_id instead of assembling a park list of its own or dead-ending on an error.
type parkScopeAmbiguousEnvelope struct {
	Code           string                 `json:"code"`
	Message        string                 `json:"message"`
	TraceID        string                 `json:"trace_id"`
	AvailableParks []vaccexecd.ParkOption `json:"availableParks"`
}

// parkScopeAmbiguousMessage is the user-facing disabled/blocked reason. Zero parks is a genuinely
// different situation from several parks and must not be reported as "choose one".
func parkScopeAmbiguousMessage(parkCount int) string {
	if parkCount == 0 {
		return "no active park is available for your access; ask an admin to grant a park scope"
	}
	return "your scope covers more than one park; choose the park to configure"
}

// updateOperatorAssignmentConfigRequest is the PUT body: N + default operator + the row_version the
// admin last read (optimistic concurrency -- 0 means "no config exists yet, create it").
type updateOperatorAssignmentConfigRequest struct {
	ParkID                string   `json:"parkId"`
	ActiveOperatorsPerDay int      `json:"activeOperatorsPerDay"`
	DefaultOperatorID     string   `json:"defaultOperatorId"`
	SelectedOperatorIDs   []string `json:"selectedOperatorIds,omitempty"`
	RowVersion            int64    `json:"rowVersion"`
}

// PutOperatorAssignmentConfig writes the park's N + default operator config. Write authority is enforced
// at the permission layer (config authority: CEO/CXO). Validate-or-reject: an invalid N or a missing
// default operator returns 400, never a silently-applied default. A row_version mismatch returns 409.
func (h *Handler) PutOperatorAssignmentConfig(w http.ResponseWriter, r *http.Request) {
	if h.operatorCfgW == nil {
		h.internal(w, r, errors.New("vaccination execution: operator assignment config writer is not wired"))
		return
	}
	var req updateOperatorAssignmentConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.badRequest(w, r, "invalid_body", "request body must be valid JSON")
		return
	}
	if !uuidutil.IsUUIDString(req.ParkID) {
		h.badRequest(w, r, "invalid_park_id", "parkId must be a UUID")
		return
	}
	updated, code, msg, err := h.operatorCfgW.UpdateOperatorAssignmentConfig(r.Context(), tenantID(r), vaccexecd.OperatorAssignmentConfig{
		ParkID:                req.ParkID,
		ActiveOperatorsPerDay: req.ActiveOperatorsPerDay,
		DefaultOperatorID:     req.DefaultOperatorID,
		SelectedOperatorIDs:   append([]string{}, req.SelectedOperatorIDs...),
		RowVersion:            req.RowVersion,
	})
	if code != "" {
		h.badRequest(w, r, code, msg)
		return
	}
	if err != nil {
		if errors.Is(err, vaccexecapp.ErrOperatorAssignmentConfigConflict) {
			httpresponse.WriteError(w, r, h.log, http.StatusConflict,
				errorEnvelope{Code: "row_version_conflict", Message: "operator assignment config was updated by someone else; reload and retry", TraceID: traceID(r)}, nil)
			return
		}
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
		errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
}

// writeReadError maps a read error to HTTP: a missing row (pgx.ErrNoRows — e.g. an unknown task,
// or one resolved outside the actor's park scope) is a 404, not a 500.
func (h *Handler) writeReadError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found", Message: "resource not found or outside your scope", TraceID: traceID(r)}, nil)
		return
	}
	h.internal(w, r, err)
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, code, msg string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
		errorEnvelope{Code: code, Message: msg, TraceID: traceID(r)}, nil)
}

func parseScheduleDrilldownAsOfRFC3339(raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.In(biztime.DefaultLocation()), nil
}

// isHistoricalAsOf reports whether the requested as_of resolves to any instant before now. Past
// point-in-time reads are not supported because there is no immutable historical snapshot store. Future
// schedule drilldowns are allowed so a schedule row can open its own planned date instead of today's
// current view.
func isHistoricalAsOf(parsed, now time.Time) bool {
	return parsed.Before(now)
}

// rejectHistoricalAsOf writes a 400 and returns true when as_of is any past instant. Historical
// point-in-time reads are not supported: there is no per-as_of snapshot store, so advertising them
// only produced misleading current-view reads. Persisting immutable historical snapshots is the follow-up
// that would re-enable this cleanly.
func (h *Handler) rejectHistoricalAsOf(w http.ResponseWriter, r *http.Request, parsed, now time.Time) bool {
	if isHistoricalAsOf(parsed, now) {
		h.badRequest(w, r, "historical_as_of_unsupported",
			"historical as_of is not supported; omit as_of for the current view")
		return true
	}
	return false
}

// readShedError maps a shed-summary/shed-detail read-path error to HTTP.
func (h *Handler) readShedError(w http.ResponseWriter, r *http.Request, err error) {
	h.internal(w, r, err)
}

func (h *Handler) applyScheduleParkScope(w http.ResponseWriter, r *http.Request, q *vaccexecd.ScheduleQuery) bool {
	parkID, ok := h.authorizedParkID(w, r, optionalString(q.ParkID))
	if !ok {
		return false
	}
	if parkID != "" {
		q.ParkID = &parkID
	}
	return true
}

func (h *Handler) applyDriveAssignmentParkScope(w http.ResponseWriter, r *http.Request, q *vaccexecd.DriveAssignmentQuery) bool {
	parkID, ok := h.authorizedParkID(w, r, optionalString(q.ParkID))
	if !ok {
		return false
	}
	if parkID != "" {
		q.ParkID = &parkID
	}
	return true
}

func (h *Handler) applyOperationsParkScope(w http.ResponseWriter, r *http.Request, q *vaccexecd.OperationsQuery) bool {
	parkID, ok := h.authorizedParkID(w, r, optionalString(q.ParkID))
	if !ok {
		return false
	}
	if parkID != "" {
		q.ParkID = &parkID
	}
	return true
}

func (h *Handler) applyExecutionParkScope(w http.ResponseWriter, r *http.Request, q *vaccexecd.ExecutionQuery) bool {
	parkID, ok := h.authorizedParkID(w, r, optionalString(q.ParkID))
	if !ok {
		return false
	}
	if parkID != "" {
		q.ParkID = &parkID
	}
	return true
}

func (h *Handler) applyGapsParkScope(w http.ResponseWriter, r *http.Request, q *vaccexecd.GapsQuery) bool {
	parkID, ok := h.authorizedParkID(w, r, optionalString(q.ParkID))
	if !ok {
		return false
	}
	if parkID != "" {
		q.ParkID = &parkID
	}
	return true
}

func (h *Handler) applyShedSummaryParkScope(w http.ResponseWriter, r *http.Request, q *vaccexecd.ShedSummaryQuery) bool {
	parkID, ok := h.authorizedParkID(w, r, optionalString(q.ParkID))
	if !ok {
		return false
	}
	if parkID != "" {
		q.ParkID = &parkID
	}
	return true
}

func (h *Handler) allowParkID(w http.ResponseWriter, r *http.Request, parkID string) bool {
	_, ok := h.authorizedParkID(w, r, parkID)
	return ok
}

func (h *Handler) authorizedParkID(w http.ResponseWriter, r *http.Request, requested string) (string, bool) {
	decision := httpmiddleware.ResolveAuthorizedParkScope(r.Context(), tenantID(r), requested)
	if decision.Allowed {
		return decision.ParkID, true
	}
	httpresponse.WriteError(w, r, h.log, decision.Status,
		errorEnvelope{Code: decision.Code, Message: decision.Message, TraceID: traceID(r)}, nil)
	return "", false
}

func optionalString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// GetVaccinationCommandBoard returns the CEO closure view: KPIs, cohort matrix, shed dose matrix,
// weekly given, and verification queue. GET /vaccination/command
func (h *Handler) GetVaccinationCommandBoard(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantID(r)
	asOf := h.now()
	if asOfStr := r.URL.Query().Get("as_of"); asOfStr != "" {
		t, err := time.Parse(time.RFC3339, asOfStr)
		if err == nil {
			asOf = t.In(biztime.DefaultLocation())
		}
	}

	driveBatchID := r.URL.Query().Get("drive_batch_id")
	var driveBatchIDPtr *string
	if driveBatchID != "" {
		driveBatchIDPtr = &driveBatchID
	}

	parkID := r.URL.Query().Get("park_id")
	var parkIDPtr *string
	if parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
				errorEnvelope{Code: "invalid_park_id", Message: "park_id must be a valid UUID", TraceID: traceID(r)}, nil)
			return
		}
		parkIDPtr = &parkID
	}

	resp, err := h.reader.VaccinationCommandBoard(r.Context(), vaccexecd.CommandBoardQuery{
		TenantID:     tenantID,
		DriveBatchID: driveBatchIDPtr,
		ParkID:       parkIDPtr,
		AsOf:         asOf,
	})
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			errorEnvelope{Code: "read_error", Message: err.Error(), TraceID: traceID(r)}, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
