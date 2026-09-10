// Package http serves the Work Board read: one route pair for the admin-web board and
// the phone's My Work, scoped by park through the capability-aware resolver and by
// owner through work_board.oversee.
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

	ltdomain "github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/workboard/app"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// Service is what this transport needs from the board.
type Service interface {
	List(ctx context.Context, q domain.Query) (domain.Page, error)
	Summary(ctx context.Context, q domain.Query) (domain.Summary, error)
	RegisteredModules() []domain.Module
}

// Handler serves the board routes.
type Handler struct {
	service Service
	flags   FlagService
	log     *slog.Logger
	now     func() time.Time
}

// NewHandler constructs the transport.
func NewHandler(service Service, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{service: service, log: log, now: time.Now}
}

// Register mounts the routes. Patterns must stay byte-identical to permissions/routes.go.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /work-board/rows", h.Rows)
	mux.HandleFunc("GET /work-board/summary", h.Summary)
	mux.HandleFunc("POST /work-board/flags", h.Flag)
}

// rowsPayload is the wire shape of one page.
type rowsPayload struct {
	Rows         []domain.Row `json:"rows"`
	NextCursor   string       `json:"next_cursor,omitempty"`
	BusinessDate string       `json:"business_date"`
	ParkID       string       `json:"park_id"`
	// Modules is the set the caller MAY see, in board order, after any module filter.
	Modules []domain.Module `json:"modules"`
	// OwnRowsOnly says the read was clamped to the caller's own rows (no oversee).
	OwnRowsOnly bool `json:"own_rows_only"`
}

type summaryPayload struct {
	domain.Summary
	BusinessDate string `json:"business_date"`
	ParkID       string `json:"park_id"`
	OwnRowsOnly  bool   `json:"own_rows_only"`
}

// Rows serves GET /work-board/rows.
func (h *Handler) Rows(w http.ResponseWriter, r *http.Request) {
	q, own, ok := h.query(w, r)
	if !ok {
		return
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			h.writeErr(w, r, http.StatusBadRequest, "invalid_limit", "That page size is not valid.")
			return
		}
		q.Limit = n
	}
	cursor, err := domain.ParseCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_cursor", "That page marker is not valid. Start from the first page.")
		return
	}
	q.Cursor = cursor
	page, err := h.service.List(r.Context(), q)
	if err != nil {
		h.writeServiceErr(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, rowsPayload{
		Rows: page.Rows, NextCursor: page.NextCursor,
		BusinessDate: q.BusinessDate, ParkID: q.ParkID, Modules: q.Modules, OwnRowsOnly: own,
	})
}

// Summary serves GET /work-board/summary.
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	q, own, ok := h.query(w, r)
	if !ok {
		return
	}
	sum, err := h.service.Summary(r.Context(), q)
	if err != nil {
		h.writeServiceErr(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, summaryPayload{Summary: sum, BusinessDate: q.BusinessDate, ParkID: q.ParkID, OwnRowsOnly: own})
}

// query parses and SCOPES the request. Every rule that decides what a caller sees is
// here, once: park through the capability-aware resolver, owner through
// work_board.oversee, and modules through the caller's own module permissions.
func (h *Handler) query(w http.ResponseWriter, r *http.Request) (domain.Query, bool, bool) {
	ctx := r.Context()
	tenantID := strings.TrimSpace(httpmiddleware.TenantIDFromContext(ctx))
	qs := r.URL.Query()

	requestedPark := strings.TrimSpace(qs.Get("park"))
	if requestedPark == "" {
		requestedPark = strings.TrimSpace(qs.Get("park_id"))
	}
	if requestedPark != "" && !uuidutil.IsUUIDString(requestedPark) {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_park_id", "That park is not valid.")
		return domain.Query{}, false, false
	}
	scope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(ctx, tenantID, requestedPark, permissions.WorkBoardOversee, permissions.WorkBoardRead)
	if !scope.Allowed {
		h.writeErr(w, r, scope.Status, scope.Code, scope.Message)
		return domain.Query{}, false, false
	}
	if scope.ParkID == "" {
		// A tenant-wide caller must still name the park: the board is bounded to one park
		// per request so every source stays on its own index. "All parks" is two requests.
		h.writeErr(w, r, http.StatusBadRequest, "park_required", "Choose a park to see its board.")
		return domain.Query{}, false, false
	}

	businessDate := strings.TrimSpace(qs.Get("business_date"))
	if businessDate == "" {
		businessDate = biztime.BusinessDate(h.now())
	} else if _, err := time.Parse("2006-01-02", businessDate); err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_business_date", "That date is not valid.")
		return domain.Query{}, false, false
	}

	perms := callerPermissions(ctx)
	// Visible = what the caller's permissions open AND what has a registered source: a
	// module with no source yet (procurement, toxin) never shows an empty lane or chip.
	visible := app.IntersectModules(app.VisibleModules(perms), h.service.RegisteredModules())
	requested := []domain.Module{}
	for _, raw := range splitCSV(qs.Get("module")) {
		if !domain.IsModule(raw) {
			h.writeErr(w, r, http.StatusBadRequest, "invalid_module", "That module is not on the board.")
			return domain.Query{}, false, false
		}
		requested = append(requested, domain.Module(raw))
	}
	modules := app.IntersectModules(requested, visible)

	states := []domain.WorkState{}
	for _, raw := range splitCSV(qs.Get("state")) {
		if !domain.IsWorkState(raw) {
			h.writeErr(w, r, http.StatusBadRequest, "invalid_state", "That work state is not on the board.")
			return domain.Query{}, false, false
		}
		states = append(states, domain.WorkState(raw))
	}

	actor := strings.TrimSpace(httpmiddleware.ActorIDFromContext(ctx))
	oversee := hasPermission(perms, permissions.WorkBoardOversee)
	owner := strings.TrimSpace(qs.Get("owner"))
	ownRowsOnly := false
	switch {
	case !oversee:
		// The operator lens: read alone never shows another person's work.
		owner = actor
		ownRowsOnly = true
	case owner == "me":
		owner = actor
	case owner == "":
		// everyone in scope
	case !uuidutil.IsUUIDString(owner):
		h.writeErr(w, r, http.StatusBadRequest, "invalid_owner", "That person is not valid.")
		return domain.Query{}, false, false
	}

	q := domain.Query{
		TenantID: tenantID, ParkID: scope.ParkID, BusinessDate: businessDate,
		Modules: modules, WorkStates: states, OwnerUserID: owner,
	}
	// An empty visible set is served as an empty board, never a 403: the route already
	// authorised the caller to open the board; modules are a lens.
	if len(modules) == 0 {
		q.Modules = []domain.Module{domain.Module("none")}
	}
	return q, ownRowsOnly, true
}

// callerPermissions is the caller's resolved permission set: the per-person rows when the
// cutover attached them, else the union of the roles on the caller's active grants.
func callerPermissions(ctx context.Context) []string {
	if perms, ok := httpmiddleware.PersonPermissionsFromContext(ctx); ok {
		return perms
	}
	candidates := append(app.VisibilityPermissions(), permissions.WorkBoardRead, permissions.WorkBoardOversee)
	out := []string{}
	for _, grant := range httpmiddleware.AuthGrantsFromContext(ctx) {
		for _, p := range candidates {
			if permissions.RoleHasPermission(grant.Role, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

func hasPermission(perms []string, want string) bool {
	for _, p := range perms {
		if p == want {
			return true
		}
	}
	return false
}

func splitCSV(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (h *Handler) writeServiceErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidCursor):
		h.writeErr(w, r, http.StatusBadRequest, "invalid_cursor", "That page marker is not valid. Start from the first page.")
	case errors.Is(err, domain.ErrInvalidQuery):
		h.writeErr(w, r, http.StatusBadRequest, "invalid_query", "That board request is not valid.")
	case errors.Is(err, context.DeadlineExceeded):
		h.writeErr(w, r, http.StatusServiceUnavailable, "board_unavailable", "The board is taking too long to load. Try again.")
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, map[string]any{
			"error": "internal_error", "message": "The board could not be loaded. Try again.",
		}, err)
	}
}

func (h *Handler) writeErr(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	httpresponse.WriteError(w, r, h.log, status, map[string]any{"error": code, "message": message}, errors.New(code))
}

// ---- flags ---------------------------------------------------------------------------

// FlagService is the raise-to-park-head write the drawer's Flag button calls.
type FlagService interface {
	Flag(ctx context.Context, p ports.FlagParams) (ports.FlagResult, error)
}

// WithFlags attaches the flag write. Register mounts its route only when it is present.
func (h *Handler) WithFlags(f FlagService) *Handler {
	h.flags = f
	return h
}

type flagPayload struct {
	RowKey      string `json:"row_key"`
	ParkID      string `json:"park_id"`
	RowTitle    string `json:"row_title"`
	RowSubtitle string `json:"row_subtitle"`
	PenDisplay  string `json:"pen_display"`
	ClockLabel  string `json:"clock_label"`
	Note        string `json:"note"`
}

type flagResultPayload struct {
	TaskID       string `json:"task_id"`
	TaskNo       int64  `json:"task_no"`
	AssigneeName string `json:"assignee_name"`
}

const maxFlagBodyBytes = 16 * 1024

// Flag serves POST /work-board/flags: the director's phone call made visible. Park scope is
// resolved the same way the reads are; the route table gates it on leadership_tasks.raise.
func (h *Handler) Flag(w http.ResponseWriter, r *http.Request) {
	if h.flags == nil {
		h.writeErr(w, r, http.StatusNotFound, "not_found", "Flags are not available.")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, http.StatusBadRequest, "missing_idempotency_key", "This flag could not be recorded safely. Try again.")
		return
	}
	var body flagPayload
	dec := json.NewDecoder(io.LimitReader(r.Body, maxFlagBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_body", "That flag could not be read.")
		return
	}
	ctx := r.Context()
	tenantID := strings.TrimSpace(httpmiddleware.TenantIDFromContext(ctx))
	parkID := strings.TrimSpace(body.ParkID)
	if parkID == "" || !uuidutil.IsUUIDString(parkID) {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_park_id", "That park is not valid.")
		return
	}
	scope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(ctx, tenantID, parkID, permissions.WorkBoardOversee)
	if !scope.Allowed {
		h.writeErr(w, r, scope.Status, scope.Code, scope.Message)
		return
	}
	result, err := h.flags.Flag(ctx, ports.FlagParams{
		TenantID: tenantID, ActorID: strings.TrimSpace(httpmiddleware.ActorIDFromContext(ctx)),
		ActorDesignation: raiseDesignation(ctx), ParkID: scope.ParkID,
		RowKey: body.RowKey, RowTitle: body.RowTitle, RowSubtitle: body.RowSubtitle,
		PenDisplay: body.PenDisplay, ClockLabel: body.ClockLabel, Note: body.Note,
		IdempotencyKey: key,
	})
	if err != nil {
		h.writeFlagErr(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, flagResultPayload{TaskID: result.TaskID, TaskNo: result.TaskNo, AssigneeName: result.AssigneeName})
}

// raiseDesignation is the director desk the flag is raised from: the first grant role that
// carries leadership_tasks.raise, exactly as the Leadership Tasks handler resolves it.
func raiseDesignation(ctx context.Context) string {
	for _, grant := range httpmiddleware.AuthGrantsFromContext(ctx) {
		if permissions.RoleHasPermission(grant.Role, permissions.LeadershipTasksRaise) {
			return grant.Role
		}
	}
	return ""
}

func (h *Handler) writeFlagErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrParkHeadMissing):
		h.writeErr(w, r, http.StatusUnprocessableEntity, "park_head_missing", "This park has no park head to flag. Fix the park's people first.")
	case errors.Is(err, app.ErrFlagRowRequired), errors.Is(err, app.ErrFlagParkRequired):
		h.writeErr(w, r, http.StatusBadRequest, "invalid_flag", "That flag is missing the work it is about.")
	case errors.Is(err, app.ErrFlagNoteTooLong):
		h.writeErr(w, r, http.StatusBadRequest, "note_too_long", "Keep the note under 1000 characters.")
	case errors.Is(err, ltdomain.ErrSelfAssignment):
		h.writeErr(w, r, http.StatusUnprocessableEntity, "flag_to_self", "You are this park's head; the flag would come back to you.")
	case errors.Is(err, ltdomain.ErrAssigneeNotAssignable):
		h.writeErr(w, r, http.StatusUnprocessableEntity, "park_head_not_reachable", "This park's head cannot receive tasks on the phone yet.")
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, map[string]any{
			"error": "internal_error", "message": "The flag could not be recorded. Try again.",
		}, err)
	}
}
