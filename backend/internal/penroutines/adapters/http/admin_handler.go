package http

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/penroutines/app"
	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// AuthoringService is the slice of the authoring service the admin transport needs.
type AuthoringService interface {
	List(ctx context.Context, tenantID, parkID string) ([]ports.RoutineListRow, []ports.Park, error)
	Get(ctx context.Context, tenantID, routineID string) (domain.Definition, error)
	Catalog(ctx context.Context, tenantID, parkID string) (app.Catalog, error)
	Create(ctx context.Context, w ports.WriteParams, d domain.Definition) (domain.Definition, error)
	Update(ctx context.Context, w ports.WriteParams, d domain.Definition) (domain.Definition, error)
	SetStatus(ctx context.Context, w ports.WriteParams, routineID, status string, rowVersion int) (domain.Definition, error)
	ListTasks(ctx context.Context, p ports.ParkListParams) (ports.ParkPage, error)
	ListTabs(ctx context.Context, tenantID string) ([]domain.Tab, error)
	CreateTab(ctx context.Context, w ports.WriteParams, t domain.Tab) (domain.Tab, error)
	UpdateTab(ctx context.Context, w ports.WriteParams, t domain.Tab) (domain.Tab, error)
	SetTabStatus(ctx context.Context, w ports.WriteParams, tabID, status string, rowVersion int) (domain.Tab, error)
	Today() string
}

// defaultIntervalDays is what the drawer pre-fills when the author picks "Every few days".
const defaultIntervalDays = 3

// AdminHandler serves the authoring routes behind /routines.
type AdminHandler struct {
	service AuthoringService
	log     *slog.Logger
}

// NewAdminHandler constructs the authoring transport.
func NewAdminHandler(service AuthoringService, log *slog.Logger) *AdminHandler {
	if log == nil {
		log = slog.Default()
	}
	return &AdminHandler{service: service, log: log}
}

// RegisterAdmin mounts the authoring routes. Patterns must stay byte-identical to
// permissions/routes.go. The two literal sub-paths (catalog, tasks) are registered beside the
// {routine_id} pattern; Go's mux prefers the literal.
func RegisterAdmin(mux *http.ServeMux, h *AdminHandler) {
	mux.HandleFunc("GET /admin/pen-routines", h.List)
	mux.HandleFunc("GET /admin/pen-routines/catalog", h.Catalog)
	mux.HandleFunc("GET /admin/pen-routines/tasks", h.ListTasks)
	mux.HandleFunc("POST /admin/pen-routines", h.Create)
	mux.HandleFunc("GET /admin/pen-routines/{routine_id}", h.Get)
	mux.HandleFunc("PUT /admin/pen-routines/{routine_id}", h.Update)
	mux.HandleFunc("POST /admin/pen-routines/{routine_id}/status", h.SetStatus)
	mux.HandleFunc("GET /admin/pen-routines/tabs", h.ListTabs)
	mux.HandleFunc("POST /admin/pen-routines/tabs", h.CreateTab)
	mux.HandleFunc("PUT /admin/pen-routines/tabs/{tab_id}", h.UpdateTab)
	mux.HandleFunc("POST /admin/pen-routines/tabs/{tab_id}/status", h.SetTabStatus)
}

// ListTabs serves GET /admin/pen-routines/tabs: every phone tab plus the drawer's vocabularies.
func (h *AdminHandler) ListTabs(w http.ResponseWriter, r *http.Request) {
	tabs, err := h.service.ListTabs(r.Context(), tenantID(r))
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	modules, icons, filters := tabVocabulary()
	out := tabListPayload{Tabs: make([]tabPayload, 0, len(tabs)), Modules: modules, Icons: icons, Filters: filters, TraceID: traceID(r)}
	for _, t := range tabs {
		out.Tabs = append(out.Tabs, toTabPayload(t))
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

// CreateTab serves POST /admin/pen-routines/tabs.
func (h *AdminHandler) CreateTab(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body tabWrite
	if !decode(w, r, h.log, &body) {
		return
	}
	t, err := h.service.CreateTab(r.Context(), writeParams(r, key), body.toTab())
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, tabDetailPayload{Tab: toTabPayload(t), TraceID: traceID(r)})
}

// UpdateTab serves PUT /admin/pen-routines/tabs/{tab_id}.
func (h *AdminHandler) UpdateTab(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body tabWrite
	if !decode(w, r, h.log, &body) {
		return
	}
	t := body.toTab()
	t.TabID = r.PathValue("tab_id")
	out, err := h.service.UpdateTab(r.Context(), writeParams(r, key), t)
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, tabDetailPayload{Tab: toTabPayload(out), TraceID: traceID(r)})
}

// SetTabStatus serves POST /admin/pen-routines/tabs/{tab_id}/status.
func (h *AdminHandler) SetTabStatus(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body statusWrite
	if !decode(w, r, h.log, &body) {
		return
	}
	out, err := h.service.SetTabStatus(r.Context(), writeParams(r, key), r.PathValue("tab_id"), body.Status, body.RowVersion)
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, tabDetailPayload{Tab: toTabPayload(out), TraceID: traceID(r)})
}

// List serves GET /admin/pen-routines?park_id.
func (h *AdminHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, parks, err := h.service.List(r.Context(), tenantID(r), r.URL.Query().Get("park_id"))
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	out := routineListPayload{Rows: make([]routineRow, 0, len(rows)), Parks: make([]parkPayload, 0, len(parks)), TraceID: traceID(r)}
	for _, row := range rows {
		out.Rows = append(out.Rows, toRoutineRow(row))
	}
	for _, p := range parks {
		out.Parks = append(out.Parks, parkPayload{ParkID: p.ParkID, Name: p.Name})
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

// Get serves GET /admin/pen-routines/{routine_id}.
func (h *AdminHandler) Get(w http.ResponseWriter, r *http.Request) {
	d, err := h.service.Get(r.Context(), tenantID(r), r.PathValue("routine_id"))
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, routineDetailPayload{Routine: toRoutineRow(ports.RoutineListRow{Definition: d}), TraceID: traceID(r)})
}

// Catalog serves GET /admin/pen-routines/catalog?park_id.
func (h *AdminHandler) Catalog(w http.ResponseWriter, r *http.Request) {
	c, err := h.service.Catalog(r.Context(), tenantID(r), strings.TrimSpace(r.URL.Query().Get("park_id")))
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	out := catalogPayload{
		Pens:      make([]catalogPenPayload, 0, len(c.Pens)),
		Roles:     make([]catalogRolePayload, 0, len(c.Roles)),
		People:    []catalogPersonPayload{},
		WorkKinds: workKindOptions(),
		QuestionKinds: options(
			[2]string{domain.QuestionYesNo, "Yes / No"},
			[2]string{domain.QuestionChoice, "One choice"},
			[2]string{domain.QuestionMultiChoice, "Several choices"},
			[2]string{domain.QuestionNumber, "Number"},
			[2]string{domain.QuestionText, "Text"},
		),
		QuestionProofKinds: questionProofKindOptions(),
		QuestionProofCounts: options(
			[2]string{domain.QuestionProofSingle, "One"},
			[2]string{domain.QuestionProofMultiple, "Up to " + strconv.Itoa(domain.MaxProofPerKind)},
		),
		CadenceKinds: options(
			[2]string{domain.CadenceDaily, "Every day"},
			[2]string{domain.CadenceWeekly, "Chosen weekdays"},
			[2]string{domain.CadenceMonthly, "Chosen days of the month"},
			[2]string{domain.CadenceEveryNDays, "Every few days"},
			[2]string{domain.CadenceAfterWork, "After work in the pen"},
		),
		ReviewKinds: options(
			[2]string{domain.ReviewVerifier, "Verifier reviews it"},
			[2]string{domain.ReviewNone, "No review"},
		),
		PresenceKinds: options(
			[2]string{domain.PresenceRequired, "Check in to the pen first"},
			[2]string{domain.PresenceOff, "No check-in"},
		),
		ScopeKinds: options(
			[2]string{domain.ScopeAllPens, "Every pen"},
			[2]string{domain.ScopeSelectedPens, "Chosen pens"},
			[2]string{domain.ScopePark, "Whole park (one task)"},
		),
		Defaults: catalogDefaults{NotifyTime: "07:00", DueOffsetDays: 0, StartDate: h.service.Today(), IntervalDays: defaultIntervalDays},
		TraceID:  traceID(r),
	}
	for _, p := range c.Pens {
		out.Pens = append(out.Pens, catalogPenPayload{ShedID: p.ShedID, ShedName: p.ShedName, PartitionLabel: p.Partition, Display: p.Label, Occupied: p.Occupied})
	}
	for _, role := range c.Roles {
		people := make([]personPayload, 0, len(role.People))
		for _, p := range role.People {
			people = append(people, personPayload{UserID: p.UserID, DisplayName: p.DisplayName})
		}
		out.Roles = append(out.Roles, catalogRolePayload{Key: role.Role, Label: domain.RoleLabel(role.Role), People: people})
	}
	out.People = catalogPeople(c.Roles)
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

func questionProofKindOptions() []optionPayload {
	kinds := [][2]string{{"none", "No proof"}}
	if questionProofAuthoringEnabled() {
		kinds = append(kinds,
			[2]string{domain.QuestionProofPhoto, "Photo"},
			[2]string{domain.QuestionProofVideo, "Video"},
			[2]string{domain.QuestionProofPhotoOrVideo, "Photo or video"},
		)
	}
	return options(kinds...)
}

func questionProofAuthoringEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_PEN_ROUTINE_QUESTION_PROOF_AUTHORING"))) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

// Create serves POST /admin/pen-routines.
func (h *AdminHandler) Create(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body routineWrite
	if !decode(w, r, h.log, &body) {
		return
	}
	d, err := h.service.Create(r.Context(), writeParams(r, key), body.toDefinition())
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, routineDetailPayload{Routine: toRoutineRow(ports.RoutineListRow{Definition: d}), TraceID: traceID(r)})
}

// Update serves PUT /admin/pen-routines/{routine_id}.
func (h *AdminHandler) Update(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body routineWrite
	if !decode(w, r, h.log, &body) {
		return
	}
	d := body.toDefinition()
	d.RoutineID = r.PathValue("routine_id")
	out, err := h.service.Update(r.Context(), writeParams(r, key), d)
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, routineDetailPayload{Routine: toRoutineRow(ports.RoutineListRow{Definition: out}), TraceID: traceID(r)})
}

// SetStatus serves POST /admin/pen-routines/{routine_id}/status.
func (h *AdminHandler) SetStatus(w http.ResponseWriter, r *http.Request) {
	key, ok := idempotencyKey(w, r, h.log)
	if !ok {
		return
	}
	var body statusWrite
	if !decode(w, r, h.log, &body) {
		return
	}
	out, err := h.service.SetStatus(r.Context(), writeParams(r, key), r.PathValue("routine_id"), body.Status, body.RowVersion)
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, routineDetailPayload{Routine: toRoutineRow(ports.RoutineListRow{Definition: out}), TraceID: traceID(r)})
}

// ListTasks serves GET /admin/pen-routines/tasks?park_id&business_date&routine_id&cursor&limit.
func (h *AdminHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeErr(w, r, h.log, app.BadRequest("invalid_limit", "That page size is not valid."))
			return
		}
		limit = parsed
	}
	page, err := h.service.ListTasks(r.Context(), ports.ParkListParams{
		TenantID:     tenantID(r),
		ParkID:       strings.TrimSpace(q.Get("park_id")),
		BusinessDate: strings.TrimSpace(q.Get("business_date")),
		RoutineID:    strings.TrimSpace(q.Get("routine_id")),
		Limit:        limit,
		Cursor:       strings.TrimSpace(q.Get("cursor")),
	})
	if err != nil {
		writeErr(w, r, h.log, toAppError(err))
		return
	}
	today := h.service.Today()
	reader := domain.Actor{UserID: strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context()))}
	rows := make([]taskRow, 0, len(page.Rows))
	for _, t := range page.Rows {
		rows = append(rows, taskRow{Step: domain.StepFor(t, reader, today), AssigneeNames: assigneeNames(t)})
	}
	var next *string
	if page.NextCursor != "" {
		c := page.NextCursor
		next = &c
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskListPayload{Rows: rows, NextCursor: next, Summary: page.Summary, TraceID: traceID(r)})
}

// assigneeNames is what the Today table shows under "assignee": the people holding the
// routine's roles for the park, by name, resolved by the task read; a name the register cannot resolve is dropped rather
// than rendered as an id.
func assigneeNames(t domain.Task) []string {
	out := make([]string, 0, len(t.AssigneeNames))
	for _, n := range t.AssigneeNames {
		if strings.TrimSpace(n) != "" {
			out = append(out, n)
		}
	}
	return out
}

func writeParams(r *http.Request, key string) ports.WriteParams {
	return ports.WriteParams{
		TenantID:       tenantID(r),
		ActorID:        strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context())),
		IdempotencyKey: key,
		TraceID:        traceID(r),
	}
}

// catalogPeople flattens the per-role holders into the "who does it" list: each person once, their
// titles in role-vocabulary order, people ordered by their first role and then by name. c.Roles
// already arrives in vocabulary order, so first-seen order is role order.
func catalogPeople(roles []ports.RoleHolders) []catalogPersonPayload {
	type entry struct {
		name   string
		titles []string
		rank   int
	}
	byUser := map[string]*entry{}
	order := []string{}
	for rank, role := range roles {
		for _, p := range role.People {
			e, ok := byUser[p.UserID]
			if !ok {
				e = &entry{name: p.DisplayName, rank: rank}
				byUser[p.UserID] = e
				order = append(order, p.UserID)
			}
			e.titles = append(e.titles, domain.RoleLabel(role.Role))
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := byUser[order[i]], byUser[order[j]]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})
	out := make([]catalogPersonPayload, 0, len(order))
	for _, id := range order {
		e := byUser[id]
		out = append(out, catalogPersonPayload{UserID: id, DisplayName: e.name, Title: strings.Join(e.titles, ", ")})
	}
	return out
}
