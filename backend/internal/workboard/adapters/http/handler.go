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
	ltports "github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
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
	// FindRow resolves one row on the caller's board; the subtask read is gated on it.
	FindRow(ctx context.Context, q domain.Query, rowKey string) (domain.Row, bool, error)
	// ListSubtasks serves one page of that row's subtasks.
	ListSubtasks(ctx context.Context, q domain.Query, rowKey, afterKey string, limit int) (domain.SubtaskPage, error)
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
	mux.HandleFunc("GET /work-board/page", h.Page)
	mux.HandleFunc("GET /work-board/rows/{row_key}/subtasks", h.Subtasks)
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
	// Degraded names modules whose read failed on this request; the rest of the board still
	// serves, so a client shows those as "couldn't load" instead of blanking.
	Degraded []domain.Module `json:"degraded,omitempty"`
}

type summaryPayload struct {
	domain.Summary
	BusinessDate string `json:"business_date"`
	ParkID       string `json:"park_id"`
	OwnRowsOnly  bool   `json:"own_rows_only"`
}

type lanePagePayload struct {
	Rows       []domain.Row    `json:"rows"`
	NextCursor string          `json:"next_cursor,omitempty"`
	Degraded   []domain.Module `json:"degraded,omitempty"`
}

type pagePayload struct {
	Summary           summaryPayload                  `json:"summary"`
	VocabularySummary *summaryPayload                 `json:"vocabulary_summary,omitempty"`
	Lanes             map[domain.Lane]lanePagePayload `json:"lanes"`
	BusinessDate      string                          `json:"business_date"`
	ParkID            string                          `json:"park_id"`
	Modules           []domain.Module                 `json:"modules"`
	OwnRowsOnly       bool                            `json:"own_rows_only"`
	Degraded          []domain.Module                 `json:"degraded,omitempty"`
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
		Degraded: page.Degraded,
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

// Page serves the admin board's first-class read: one park, one day, all lane pages and
// the aggregate summary. The old web page fired this as five separate backend requests
// per park; doing it here keeps the user-facing lane cursors while avoiding an SSR
// request stampede into the shared API/DB pool.
func (h *Handler) Page(w http.ResponseWriter, r *http.Request) {
	q, own, ok := h.query(w, r)
	if !ok {
		return
	}
	visibleModules := app.StrictIntersect(app.VisibleModules(callerPermissions(r.Context())), h.service.RegisteredModules())
	pageLanes, ok := h.pageLanes(w, r)
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
	sum, err := h.service.Summary(r.Context(), q)
	if err != nil {
		h.writeServiceErr(w, r, err)
		return
	}
	out := pagePayload{
		Summary:      summaryPayload{Summary: sum, BusinessDate: q.BusinessDate, ParkID: q.ParkID, OwnRowsOnly: own},
		Lanes:        map[domain.Lane]lanePagePayload{},
		BusinessDate: q.BusinessDate,
		ParkID:       q.ParkID,
		Modules:      q.Modules,
		OwnRowsOnly:  own,
	}
	out.Degraded = appendModules(out.Degraded, sum.Degraded...)
	if strings.TrimSpace(r.URL.Query().Get("module")) != "" && len(q.Modules) > 0 && !q.NoModules {
		vocabQ := q
		vocabQ.Modules = visibleModules
		vocabQ.WorkStates = nil
		vocabQ.Cursor = domain.Cursor{}
		vocab, err := h.service.Summary(r.Context(), vocabQ)
		if err != nil {
			h.writeServiceErr(w, r, err)
			return
		}
		out.VocabularySummary = &summaryPayload{Summary: vocab, BusinessDate: vocabQ.BusinessDate, ParkID: vocabQ.ParkID, OwnRowsOnly: own}
		out.Degraded = appendModules(out.Degraded, vocab.Degraded...)
	}
	for _, lane := range pageLanes {
		laneQ := q
		laneQ.WorkStates = intersectStates(q.WorkStates, domain.StatesInLane(lane))
		if len(laneQ.WorkStates) == 0 && len(q.WorkStates) > 0 {
			laneQ.WorkStates = []domain.WorkState{domain.WorkStateNone}
		}
		cursor, err := domain.ParseCursor(r.URL.Query().Get("cursor_" + string(lane)))
		if err != nil {
			out.Lanes[lane] = lanePagePayload{Rows: []domain.Row{}, Degraded: visibleModules}
			out.Degraded = appendModules(out.Degraded, visibleModules...)
			continue
		}
		laneQ.Cursor = cursor
		page, err := h.service.List(r.Context(), laneQ)
		if err != nil {
			out.Lanes[lane] = lanePagePayload{Rows: []domain.Row{}, Degraded: visibleModules}
			out.Degraded = appendModules(out.Degraded, visibleModules...)
			continue
		}
		out.Lanes[lane] = lanePagePayload{Rows: page.Rows, NextCursor: page.NextCursor, Degraded: page.Degraded}
		out.Degraded = appendModules(out.Degraded, page.Degraded...)
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

// subtasksPayload is the wire shape of one subtask page.
type subtasksPayload struct {
	Subtasks     []domain.Subtask `json:"subtasks"`
	NextCursor   string           `json:"next_cursor,omitempty"`
	Total        int              `json:"total"`
	RowKey       string           `json:"row_key"`
	BusinessDate string           `json:"business_date"`
	ParkID       string           `json:"park_id"`
}

// Subtasks serves GET /work-board/rows/{row_key}/subtasks: the issue view's list of the
// row's units of work, worst first. Scope is the SAME as /work-board/rows -- park through
// the grants, modules through the caller's permissions, owner through work_board.oversee --
// and the row is first resolved on that board through FindRow, so a caller cannot drill into
// a row their own board would not have shown them (404 row_not_found, never a leak).
func (h *Handler) Subtasks(w http.ResponseWriter, r *http.Request) {
	q, _, ok := h.query(w, r)
	if !ok {
		return
	}
	rowKey := strings.TrimSpace(r.PathValue("row_key"))
	if _, err := domain.ParseCursor(rowKey); err != nil || rowKey == "" {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_row_key", "That work is not on the board.")
		return
	}
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			h.writeErr(w, r, http.StatusBadRequest, "invalid_limit", "That page size is not valid.")
			return
		}
		limit = n
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if _, _, err := domain.ParseSubtaskKey(cursor); err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_cursor", "That page marker is not valid. Start from the first page.")
		return
	}
	// The row must be on the CALLER's board: their park, that day, the modules their
	// permissions open, and -- without oversee -- their own rows only.
	if _, found, err := h.service.FindRow(r.Context(), q, rowKey); err != nil {
		h.writeServiceErr(w, r, err)
		return
	} else if !found {
		h.writeErr(w, r, http.StatusNotFound, "row_not_found", "That work is not on your board for this park and day. Refresh and try again.")
		return
	}
	page, err := h.service.ListSubtasks(r.Context(), q, rowKey, cursor, limit)
	if err != nil {
		h.writeServiceErr(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, subtasksPayload{
		Subtasks: page.Subtasks, NextCursor: page.NextCursor, Total: page.Total,
		RowKey: rowKey, BusinessDate: q.BusinessDate, ParkID: q.ParkID,
	})
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
	visible := app.StrictIntersect(app.VisibleModules(perms), h.service.RegisteredModules())
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
	// `lane` is a column read: it expands to the lane's states so each column can page on
	// its own. With `state` as well, the two intersect.
	if lanes := splitCSV(qs.Get("lane")); len(lanes) > 0 {
		laneStates := []domain.WorkState{}
		for _, raw := range lanes {
			if !domain.IsLane(raw) {
				h.writeErr(w, r, http.StatusBadRequest, "invalid_lane", "That column is not on the board.")
				return domain.Query{}, false, false
			}
			laneStates = append(laneStates, domain.StatesInLane(domain.Lane(raw))...)
		}
		if len(states) == 0 {
			states = laneStates
		} else {
			keep := map[domain.WorkState]struct{}{}
			for _, s := range laneStates {
				keep[s] = struct{}{}
			}
			both := []domain.WorkState{}
			for _, s := range states {
				if _, ok := keep[s]; ok {
					both = append(both, s)
				}
			}
			states = both
			if len(states) == 0 {
				// A state outside the lane: an empty column, never an error.
				states = []domain.WorkState{domain.WorkStateNone}
			}
		}
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
	// authorised the caller to open the board; modules are a lens. The wire carries an
	// empty modules list, never a sentinel.
	if len(modules) == 0 {
		q.NoModules = true
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

func (h *Handler) pageLanes(w http.ResponseWriter, r *http.Request) ([]domain.Lane, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("page_lane"))
	if raw == "" {
		return domain.Lanes(), true
	}
	if raw == "__none__" {
		return []domain.Lane{}, true
	}
	out := []domain.Lane{}
	seen := map[domain.Lane]struct{}{}
	for _, part := range splitCSV(raw) {
		if !domain.IsLane(part) {
			h.writeErr(w, r, http.StatusBadRequest, "invalid_lane", "That column is not on the board.")
			return nil, false
		}
		lane := domain.Lane(part)
		if _, ok := seen[lane]; ok {
			continue
		}
		seen[lane] = struct{}{}
		out = append(out, lane)
	}
	return out, true
}

func intersectStates(requested, allowed []domain.WorkState) []domain.WorkState {
	if len(requested) == 0 {
		return append([]domain.WorkState(nil), allowed...)
	}
	keep := map[domain.WorkState]struct{}{}
	for _, state := range allowed {
		keep[state] = struct{}{}
	}
	out := []domain.WorkState{}
	for _, state := range requested {
		if _, ok := keep[state]; ok {
			out = append(out, state)
		}
	}
	return out
}

func appendModules(into []domain.Module, modules ...domain.Module) []domain.Module {
	for _, module := range modules {
		found := false
		for _, existing := range into {
			if existing == module {
				found = true
				break
			}
		}
		if !found {
			into = append(into, module)
		}
	}
	return into
}

func (h *Handler) writeServiceErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidCursor), errors.Is(err, domain.ErrInvalidSubtaskCursor):
		h.writeErr(w, r, http.StatusBadRequest, "invalid_cursor", "That page marker is not valid. Start from the first page.")
	case errors.Is(err, domain.ErrInvalidRowKey):
		h.writeErr(w, r, http.StatusBadRequest, "invalid_row_key", "That work is not on the board.")
	case errors.Is(err, app.ErrRowNotFound):
		h.writeErr(w, r, http.StatusNotFound, "row_not_found", "That work is not on your board for this park and day. Refresh and try again.")
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

// flagPayload names WHICH row on WHICH board; the row's own copy is never accepted from
// the client (the service looks the row up on the caller's board).
type flagPayload struct {
	RowKey       string `json:"row_key"`
	ParkID       string `json:"park_id"`
	BusinessDate string `json:"business_date"`
	Note         string `json:"note"`
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
	businessDate := strings.TrimSpace(body.BusinessDate)
	if businessDate == "" {
		businessDate = biztime.BusinessDate(h.now())
	} else if _, err := time.Parse("2006-01-02", businessDate); err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_business_date", "That date is not valid.")
		return
	}
	// The board the flag is looked up on is the caller's own: their park scope, the day
	// they were looking at, and the modules their permissions open. Oversee is required
	// by the route, so there is no own-rows narrowing here.
	visible := app.StrictIntersect(app.VisibleModules(callerPermissions(ctx)), h.service.RegisteredModules())
	board := domain.Query{
		TenantID: tenantID, ParkID: scope.ParkID, BusinessDate: businessDate,
		Modules: visible,
		// An empty visible set means NO module, never every module (the same rule the reads
		// apply); FindRow then resolves nothing and the flag is refused as not on the board.
		NoModules: len(visible) == 0,
	}
	result, err := h.flags.Flag(ctx, ports.FlagParams{
		TenantID: tenantID, ActorID: strings.TrimSpace(httpmiddleware.ActorIDFromContext(ctx)),
		ActorDesignation: raiseDesignation(ctx), Board: board,
		RowKey: body.RowKey, Note: body.Note, IdempotencyKey: key,
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
	case errors.Is(err, domain.ErrInvalidRowKey):
		h.writeErr(w, r, http.StatusBadRequest, "invalid_row_key", "That work is not on the board.")
	case errors.Is(err, app.ErrFlagRowNotFound):
		h.writeErr(w, r, http.StatusNotFound, "row_not_found", "That work is not on your board for this park and day. Refresh and try again.")
	case errors.Is(err, app.ErrFlagNoteTooLong):
		h.writeErr(w, r, http.StatusBadRequest, "note_too_long", "Keep the note under 1000 characters.")
	case errors.Is(err, ltdomain.ErrSelfAssignment):
		h.writeErr(w, r, http.StatusUnprocessableEntity, "flag_to_self", "You are this park's head; the flag would come back to you.")
	case errors.Is(err, ltdomain.ErrAssigneeNotAssignable):
		h.writeErr(w, r, http.StatusUnprocessableEntity, "park_head_not_reachable", "This park's head cannot receive tasks on the phone yet.")
	case errors.Is(err, ltports.ErrIdempotencyConflict):
		// The same key with a different note: a retried submit that changed on the way.
		h.writeErr(w, r, http.StatusConflict, "idempotency_conflict", "This flag was already sent with different words. Refresh and try again.")
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, map[string]any{
			"error": "internal_error", "message": "The flag could not be recorded. Try again.",
		}, err)
	}
}
