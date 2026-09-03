// Package http exposes the PC Care module: planner reads/writes (CEO), the operator worklist,
// scan-to-add + per-animal slot proof registration, the peer-visibility captures poll, and the
// whole-task submit. Verdicts ride the generic /verification routes; media bytes ride
// /app/proofs — this handler stores only references.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/app"
	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// Service is the app boundary this handler renders.
type Service interface {
	PlannerCatalog(ctx context.Context, actor domain.Actor) (ports.PlannerCatalog, error)
	PlannerParkSheds(ctx context.Context, actor domain.Actor, parkID, category, plannedBusinessDate, cursor string, limit int) (ports.PlannerParkSheds, error)
	CreateTask(ctx context.Context, actor domain.Actor, in app.CreateTaskInput) (ports.TaskRow, error)
	CancelTask(ctx context.Context, actor domain.Actor, taskID, traceID string) error
	ListTasks(ctx context.Context, actor domain.Actor, parkID, category, dueBusinessDate, cursor string, limit int, currentOrCarry bool) (ports.TaskPage, error)
	Worklist(ctx context.Context, actor domain.Actor, category, dueBusinessDate, cursor string, limit int) (ports.TaskPage, error)
	GetTask(ctx context.Context, actor domain.Actor, taskID string) (ports.TaskRow, error)
	ListTaskAnimals(ctx context.Context, actor domain.Actor, taskID, cursor string, limit int) ([]ports.AnimalRow, string, error)
	ScanAnimal(ctx context.Context, actor domain.Actor, in app.ScanAnimalInput) (ports.ScanAnimalResult, error)
	RegisterSlotProof(ctx context.Context, actor domain.Actor, in app.RegisterSlotProofInput) error
	RegisterTaskProof(ctx context.Context, actor domain.Actor, in app.RegisterTaskProofInput) error
	SubmitTask(ctx context.Context, actor domain.Actor, in app.SubmitTaskInput) (ports.SubmitTaskResult, error)
	TaskRoster(ctx context.Context, actor domain.Actor, taskID, cursor string, limit int) (ports.TaskRosterPage, error)
	RecordStockVerdict(ctx context.Context, actor domain.Actor, in app.StockVerdictInput) (ports.TaskRow, error)
}

// Handler renders the PC Care HTTP surface.
type Handler struct {
	service Service
	log     *slog.Logger
}

// NewHandler constructs the handler.
func NewHandler(service Service, log *slog.Logger) *Handler {
	return &Handler{service: service, log: log}
}

// Register wires the routes. Every route string here has a matching permissions row in
// permissions/routes.go — a nav item's permission MUST equal its backing route's permission.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /app/pc-care/planner/catalog", h.GetPlannerCatalog)
	mux.HandleFunc("GET /app/pc-care/planner/parks/{park_id}/sheds", h.GetPlannerParkSheds)
	mux.HandleFunc("POST /app/pc-care/tasks", h.PostCreateTask)
	mux.HandleFunc("POST /app/pc-care/tasks/{task_id}/cancel", h.PostCancelTask)
	mux.HandleFunc("GET /app/pc-care/tasks", h.GetTasks)
	mux.HandleFunc("GET /app/pc-care/worklist", h.GetWorklist)
	mux.HandleFunc("GET /app/pc-care/tasks/{task_id}", h.GetTask)
	// The peer-visibility poll: which animals are scanned and which slots each already holds,
	// by ANY assignee. Read-only; it is what lets several phones split one task's videos.
	mux.HandleFunc("GET /app/pc-care/tasks/{task_id}/captures", h.GetTaskCaptures)
	// The roster-pick tap list: the RFIDs of animals currently in the task's pen (trimming
	// categories). Read-only; tapping one records a normal free-flow scan.
	mux.HandleFunc("GET /app/pc-care/tasks/{task_id}/roster", h.GetTaskRoster)
	mux.HandleFunc("POST /app/pc-care/tasks/{task_id}/animals", h.PostScanAnimal)
	mux.HandleFunc("PUT /app/pc-care/tasks/{task_id}/animals/{animal_row_id}/proofs/{slot}", h.PutSlotProof)
	mux.HandleFunc("PUT /app/pc-care/tasks/{task_id}/proofs/{slot}", h.PutTaskProof)
	mux.HandleFunc("POST /app/pc-care/tasks/{task_id}/submit", h.PostSubmitTask)
	// The PC Director's approve/reject on a submitted vaccine-stock task (maintainer decision
	// 2026-09-02). Gated on pc_care.stock_approve in permissions/routes.go — the operators who
	// recorded the fridge cannot accept their own evidence, and the tenant verifier never sees
	// stock work.
	mux.HandleFunc("POST /app/pc-care/tasks/{task_id}/stock-verdict", h.PostStockVerdict)
}

// ---------------------------------------------------------------------------
// DTOs
// ---------------------------------------------------------------------------

type slotDTO struct {
	FieldKey string `json:"field_key"`
	Label    string `json:"label"`
	// Description is backend-owned farm copy saying what this video must show.
	Description string `json:"description,omitempty"`
	// MinDurationHintSeconds is recorder-chrome guidance, never a client-enforced cap.
	MinDurationHintSeconds int `json:"min_duration_hint_seconds,omitempty"`
}

type taskDTO struct {
	TaskID    string `json:"task_id"`
	Category  string `json:"category"`
	ParkID    string `json:"park_id"`
	ParkLabel string `json:"park_label"`
	// ShedID/ShedLabel are empty for per-vaccine inventory_vaccine tasks — fridge stock has no
	// shed (maintainer decision 2026-08-27).
	ShedID         string `json:"shed_id"`
	ShedLabel      string `json:"shed_label"`
	PartitionLabel string `json:"partition_label,omitempty"`
	// VaccineLabel names the vaccine a per-vaccine inventory_vaccine task covers ("FMD").
	VaccineLabel string `json:"vaccine_label,omitempty"`
	// TaskLabel is the backend-owned card headline, rendered verbatim: the pen display for
	// shed-scoped tasks, the vaccine label for per-vaccine stock tasks.
	TaskLabel string `json:"task_label"`
	// OperationalLocationDisplay is the backend-composed "Castro - 2" (oploc.Display) — the
	// Operational Location convention's mandatory display half; clients render it verbatim.
	// Empty for per-vaccine stock tasks, which have no operational location.
	OperationalLocationDisplay string     `json:"operational_location_display"`
	PlannedBusinessDate        string     `json:"planned_business_date"`
	DueBusinessDate            string     `json:"due_business_date"`
	WorkState                  string     `json:"work_state"`
	Status                     string     `json:"status"`
	ReworkReason               string     `json:"rework_reason,omitempty"`
	RowVersion                 int32      `json:"row_version"`
	SubmittedAt                *time.Time `json:"submitted_at,omitempty"`
	AssigneeUserIDs            []string   `json:"assignee_user_ids"`
	AssigneeNames              []string   `json:"assignee_names"`
	AnimalCount                int32      `json:"animal_count"`
	// CaptureMode is the BACKEND-OWNED capture flow for this task's category: "scan_record"
	// (scan a tag → the recorder opens immediately) or "roster_pick" (tap an RFID off the pen
	// roster → record). Clients branch on it verbatim and never hardcode a category→mode map.
	CaptureMode string `json:"capture_mode"`
	// ExpectedSlots is the BACKEND-OWNED slot contract for this task's category: clients
	// iterate it verbatim and never hardcode a category→slot map (proof grain is backend-owned).
	ExpectedSlots []slotDTO `json:"expected_slots"`
	// InventoryRequirements is present for inventory_vaccine tasks and names the stock the
	// director must show in the fridge proof.
	InventoryRequirements []inventoryRequirementDTO `json:"inventory_requirements,omitempty"`
	// TaskProofs is present for task-level proof categories such as inventory_vaccine so a
	// second device can render already-captured fridge proof media.
	TaskProofs []taskProofDTO `json:"task_proofs,omitempty"`
}

type inventoryRequirementDTO struct {
	VaccineLabel   string   `json:"vaccine_label"`
	RequiredDoses  int32    `json:"required_doses"`
	SourceBatchIDs []string `json:"source_batch_ids,omitempty"`
}

type taskProofDTO struct {
	SlotKey        string     `json:"slot_key"`
	ProofRef       string     `json:"proof_ref"`
	CapturedBy     string     `json:"captured_by,omitempty"`
	CapturedByName string     `json:"captured_by_name,omitempty"`
	CapturedAt     *time.Time `json:"captured_at,omitempty"`
}

func taskDTOFrom(t ports.TaskRow) taskDTO {
	slots := domain.SlotsForCategory(t.Category)
	slotDTOs := make([]slotDTO, 0, len(slots))
	for _, s := range slots {
		slotDTOs = append(slotDTOs, slotDTO{FieldKey: s.FieldKey, Label: s.Label, Description: s.Description, MinDurationHintSeconds: s.MinDurationHintSeconds})
	}
	assigneeIDs := t.AssigneeUserIDs
	if assigneeIDs == nil {
		assigneeIDs = []string{}
	}
	assigneeNames := t.AssigneeNames
	if assigneeNames == nil {
		assigneeNames = []string{}
	}
	requirements := make([]inventoryRequirementDTO, 0, len(t.InventoryRequirements))
	for _, req := range t.InventoryRequirements {
		requirements = append(requirements, inventoryRequirementDTO{
			VaccineLabel:   req.VaccineLabel,
			RequiredDoses:  req.RequiredDoses,
			SourceBatchIDs: req.SourceBatchIDs,
		})
	}
	taskProofs := make([]taskProofDTO, 0, len(t.TaskProofs))
	for _, proof := range t.TaskProofs {
		capturedAt := proof.CapturedAt
		taskProofs = append(taskProofs, taskProofDTO{
			SlotKey:        proof.SlotKey,
			ProofRef:       proof.ProofRef,
			CapturedBy:     proof.CapturedBy,
			CapturedByName: proof.CapturedByName,
			CapturedAt:     &capturedAt,
		})
	}
	locationDisplay := oploc.OperationalLocation{
		ShedName: t.ShedName, PartitionLabel: t.PartitionLabel,
	}.Display()
	taskLabel := locationDisplay
	if taskLabel == "" {
		taskLabel = t.VaccineLabel
	}
	return taskDTO{
		TaskID:                     t.TaskID,
		Category:                   t.Category,
		ParkID:                     t.ParkID,
		ParkLabel:                  t.ParkName,
		ShedID:                     t.ShedID,
		ShedLabel:                  t.ShedName,
		PartitionLabel:             t.PartitionLabel,
		VaccineLabel:               t.VaccineLabel,
		TaskLabel:                  taskLabel,
		OperationalLocationDisplay: locationDisplay,
		PlannedBusinessDate:        t.PlannedBusinessDate,
		DueBusinessDate:            t.DueBusinessDate,
		WorkState:                  t.WorkState,
		Status:                     t.Status,
		ReworkReason:               t.ReworkReason,
		RowVersion:                 t.RowVersion,
		SubmittedAt:                t.SubmittedAt,
		AssigneeUserIDs:            assigneeIDs,
		AssigneeNames:              assigneeNames,
		AnimalCount:                t.AnimalCount,
		CaptureMode:                domain.CaptureModeForCategory(t.Category),
		ExpectedSlots:              slotDTOs,
		InventoryRequirements:      requirements,
		TaskProofs:                 taskProofs,
	}
}

type taskPageDTO struct {
	Items      []taskDTO `json:"items"`
	NextCursor string    `json:"next_cursor"`
}

type animalSlotDTO struct {
	FieldKey       string     `json:"field_key"`
	ProofRef       string     `json:"proof_ref,omitempty"`
	CapturedBy     string     `json:"captured_by,omitempty"`
	CapturedByName string     `json:"captured_by_name,omitempty"`
	CapturedAt     *time.Time `json:"captured_at,omitempty"`
}

type animalRowDTO struct {
	AnimalRowID       string          `json:"animal_row_id"`
	ScannedIdentifier string          `json:"scanned_identifier"`
	ScannedBy         string          `json:"scanned_by,omitempty"`
	ScannedByName     string          `json:"scanned_by_name,omitempty"`
	ScannedAt         time.Time       `json:"scanned_at"`
	Slots             []animalSlotDTO `json:"slots"`
}

type capturesResponse struct {
	Animals    []animalRowDTO `json:"animals"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type plannerCatalogResponse struct {
	Parks     []plannerParkDTO     `json:"parks"`
	Operators []plannerOperatorDTO `json:"operators"`
	// Categories is the module's backend-owned category vocabulary for the wizard.
	Categories []categoryDTO `json:"categories"`
}

type categoryDTO struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type plannerParkDTO struct {
	ParkID    string `json:"park_id"`
	ParkLabel string `json:"park_label"`
}

type plannerOperatorDTO struct {
	UserID      string   `json:"user_id"`
	DisplayName string   `json:"display_name"`
	ParkIDs     []string `json:"park_ids"`
}

type plannerShedDTO struct {
	ShedID         string `json:"shed_id"`
	ShedLabel      string `json:"shed_label"`
	PartitionLabel string `json:"partition_label,omitempty"`
	// OperationalLocationDisplay is the backend-composed pen display (oploc.Display).
	OperationalLocationDisplay string `json:"operational_location_display"`
	ExistingTaskID             string `json:"existing_task_id,omitempty"`
}

type plannerShedsResponse struct {
	Sheds      []plannerShedDTO `json:"sheds"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type createTaskRequest struct {
	Category            string   `json:"category"`
	ParkID              string   `json:"park_id"`
	ShedID              string   `json:"shed_id"`
	PartitionLabel      string   `json:"partition_label"`
	PlannedBusinessDate string   `json:"planned_business_date"`
	AssigneeUserIDs     []string `json:"assignee_user_ids"`
	// FeedRemovalRequired (deworming only) also plans the evening-before feed & water removal
	// task in the same write; RemovalOperatorUserIDs (>=1 when the toggle is on) are its
	// operators. On any other category these fields are rejected, never dropped.
	FeedRemovalRequired    bool     `json:"feed_removal_required"`
	RemovalOperatorUserIDs []string `json:"removal_operator_user_ids"`
}

type scanAnimalRequest struct {
	ScannedIdentifier string `json:"scanned_identifier"`
}

type scanAnimalResponse struct {
	AnimalRowID string `json:"animal_row_id"`
}

type slotProofRequest struct {
	ProofRef string `json:"proof_ref"`
}

type stockVerdictRequest struct {
	// Verdict is "approve" or "reject" (domain.StockVerdict*).
	Verdict string `json:"verdict"`
	// Reason is mandatory on a reject; it becomes the operators' rework banner, verbatim.
	Reason string `json:"reason"`
}

type submitTaskResponse struct {
	TaskID      string `json:"task_id"`
	Status      string `json:"status"`
	RowVersion  int32  `json:"row_version"`
	AnimalCount int32  `json:"animal_count"`
}

type codedError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

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

func (h *Handler) requireAuthed(w http.ResponseWriter, r *http.Request) (domain.Actor, bool) {
	a := actor(r)
	if a.TenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return domain.Actor{}, false
	}
	return a, true
}

func idempotencyKey(w http.ResponseWriter, r *http.Request, h *Handler) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "Idempotency-Key header is required", nil)
		return "", false
	}
	if len(key) < 8 || len(key) > 200 {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "Idempotency-Key must be between 8 and 200 characters", nil)
		return "", false
	}
	return key, true
}

func intQuery(r *http.Request, name string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func (h *Handler) GetPlannerCatalog(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	catalog, err := h.service.PlannerCatalog(r.Context(), a)
	if err != nil {
		h.writeServiceError(w, r, "pc care planner catalog", err)
		return
	}
	resp := plannerCatalogResponse{
		Parks:      make([]plannerParkDTO, 0, len(catalog.Parks)),
		Operators:  make([]plannerOperatorDTO, 0, len(catalog.Operators)),
		Categories: make([]categoryDTO, 0, len(domain.PlannerCategories)),
	}
	for _, park := range catalog.Parks {
		resp.Parks = append(resp.Parks, plannerParkDTO{ParkID: park.ParkID, ParkLabel: park.ParkName})
	}
	for _, op := range catalog.Operators {
		parkIDs := op.ParkIDs
		if parkIDs == nil {
			parkIDs = []string{}
		}
		resp.Operators = append(resp.Operators, plannerOperatorDTO{UserID: op.UserID, DisplayName: op.DisplayName, ParkIDs: parkIDs})
	}
	for _, category := range domain.PlannerCategories {
		resp.Categories = append(resp.Categories, categoryDTO{Key: category, Label: domain.CategoryLabel(category)})
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetPlannerParkSheds(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	page, err := h.service.PlannerParkSheds(
		r.Context(), a,
		r.PathValue("park_id"),
		strings.TrimSpace(r.URL.Query().Get("category")),
		strings.TrimSpace(r.URL.Query().Get("date")),
		strings.TrimSpace(r.URL.Query().Get("cursor")),
		intQuery(r, "limit", 25),
	)
	if err != nil {
		h.writeServiceError(w, r, "pc care planner park sheds", err)
		return
	}
	resp := plannerShedsResponse{Sheds: make([]plannerShedDTO, 0, len(page.Sheds)), NextCursor: page.NextCursor}
	for _, shed := range page.Sheds {
		resp.Sheds = append(resp.Sheds, plannerShedDTO{
			ShedID: shed.ShedID, ShedLabel: shed.ShedName,
			PartitionLabel: shed.PartitionLabel,
			OperationalLocationDisplay: oploc.OperationalLocation{
				ShedName: shed.ShedName, PartitionLabel: shed.PartitionLabel,
			}.Display(),
			ExistingTaskID: shed.ExistingTaskID,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) PostCreateTask(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r, h)
	if !ok {
		return
	}
	var body createTaskRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	task, err := h.service.CreateTask(r.Context(), a, app.CreateTaskInput{
		Category:               body.Category,
		ParkID:                 body.ParkID,
		ShedID:                 body.ShedID,
		PartitionLabel:         body.PartitionLabel,
		PlannedBusinessDate:    body.PlannedBusinessDate,
		AssigneeUserIDs:        body.AssigneeUserIDs,
		FeedRemovalRequired:    body.FeedRemovalRequired,
		RemovalOperatorUserIDs: body.RemovalOperatorUserIDs,
		IdempotencyKey:         key,
		ActorID:             a.UserID,
		ActorType:           "human",
		TraceID:             httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, r, "pc care create task", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDTOFrom(task))
}

func (h *Handler) PostCancelTask(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	if err := h.service.CancelTask(r.Context(), a, r.PathValue("task_id"), httpmiddleware.TraceIDFromContext(r.Context())); err != nil {
		h.writeServiceError(w, r, "pc care cancel task", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}

func (h *Handler) GetTasks(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListTasks(
		r.Context(), a,
		strings.TrimSpace(r.URL.Query().Get("park_id")),
		strings.TrimSpace(r.URL.Query().Get("category")),
		strings.TrimSpace(r.URL.Query().Get("date")),
		strings.TrimSpace(r.URL.Query().Get("cursor")),
		intQuery(r, "limit", 25),
		strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("current_or_carry")), "true"),
	)
	if err != nil {
		h.writeServiceError(w, r, "pc care list tasks", err)
		return
	}
	h.writeTaskPage(w, page)
}

func (h *Handler) GetWorklist(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	page, err := h.service.Worklist(
		r.Context(), a,
		strings.TrimSpace(r.URL.Query().Get("category")),
		strings.TrimSpace(r.URL.Query().Get("date")),
		strings.TrimSpace(r.URL.Query().Get("cursor")),
		intQuery(r, "limit", 25),
	)
	if err != nil {
		h.writeServiceError(w, r, "pc care worklist", err)
		return
	}
	h.writeTaskPage(w, page)
}

func (h *Handler) writeTaskPage(w http.ResponseWriter, page ports.TaskPage) {
	resp := taskPageDTO{Items: make([]taskDTO, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, t := range page.Items {
		resp.Items = append(resp.Items, taskDTOFrom(t))
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	task, err := h.service.GetTask(r.Context(), a, r.PathValue("task_id"))
	if err != nil {
		h.writeServiceError(w, r, "pc care get task", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDTOFrom(task))
}

func (h *Handler) GetTaskCaptures(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	animals, nextCursor, err := h.service.ListTaskAnimals(
		r.Context(), a,
		r.PathValue("task_id"),
		strings.TrimSpace(r.URL.Query().Get("cursor")),
		intQuery(r, "limit", 50),
	)
	if err != nil {
		h.writeServiceError(w, r, "pc care task captures", err)
		return
	}
	resp := capturesResponse{Animals: make([]animalRowDTO, 0, len(animals)), NextCursor: nextCursor}
	for _, animal := range animals {
		row := animalRowDTO{
			AnimalRowID:       animal.AnimalRowID,
			ScannedIdentifier: animal.ScannedIdentifier,
			ScannedBy:         animal.ScannedBy,
			ScannedByName:     animal.ScannedByName,
			ScannedAt:         animal.ScannedAt,
			Slots:             make([]animalSlotDTO, 0, len(animal.Slots)),
		}
		for _, slot := range animal.Slots {
			row.Slots = append(row.Slots, animalSlotDTO{
				FieldKey:       slot.FieldKey,
				ProofRef:       slot.ProofRef,
				CapturedBy:     slot.CapturedBy,
				CapturedByName: slot.CapturedByName,
				CapturedAt:     slot.CapturedAt,
			})
		}
		resp.Animals = append(resp.Animals, row)
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

type rosterResponse struct {
	// Identifiers are the pen's resident RFIDs, verbatim, in identifier order.
	Identifiers []string `json:"identifiers"`
	NextCursor  string   `json:"next_cursor,omitempty"`
}

func (h *Handler) GetTaskRoster(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	page, err := h.service.TaskRoster(
		r.Context(), a,
		r.PathValue("task_id"),
		strings.TrimSpace(r.URL.Query().Get("cursor")),
		intQuery(r, "limit", 50),
	)
	if err != nil {
		h.writeServiceError(w, r, "pc care task roster", err)
		return
	}
	identifiers := page.Identifiers
	if identifiers == nil {
		identifiers = []string{}
	}
	httpresponse.WriteJSON(w, http.StatusOK, rosterResponse{Identifiers: identifiers, NextCursor: page.NextCursor})
}

func (h *Handler) PostScanAnimal(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r, h)
	if !ok {
		return
	}
	var body scanAnimalRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	result, err := h.service.ScanAnimal(r.Context(), a, app.ScanAnimalInput{
		TaskID:            r.PathValue("task_id"),
		ScannedIdentifier: body.ScannedIdentifier,
		IdempotencyKey:    key,
		ActorID:           a.UserID,
		ActorType:         "operator",
		TraceID:           httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, r, "pc care scan animal", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, scanAnimalResponse{AnimalRowID: result.AnimalRowID})
}

func (h *Handler) PutSlotProof(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r, h)
	if !ok {
		return
	}
	var body slotProofRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	err := h.service.RegisterSlotProof(r.Context(), a, app.RegisterSlotProofInput{
		TaskID:         r.PathValue("task_id"),
		AnimalRowID:    r.PathValue("animal_row_id"),
		SlotFieldKey:   r.PathValue("slot"),
		ProofRef:       body.ProofRef,
		IdempotencyKey: key,
		ActorID:        a.UserID,
		ActorType:      "operator",
		TraceID:        httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, r, "pc care slot proof", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (h *Handler) PutTaskProof(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r, h)
	if !ok {
		return
	}
	var body slotProofRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	err := h.service.RegisterTaskProof(r.Context(), a, app.RegisterTaskProofInput{
		TaskID:         r.PathValue("task_id"),
		SlotKey:        r.PathValue("slot"),
		ProofRef:       body.ProofRef,
		IdempotencyKey: key,
		ActorID:        a.UserID,
		ActorType:      "operator",
		TraceID:        httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, r, "pc care task proof", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (h *Handler) PostSubmitTask(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r, h)
	if !ok {
		return
	}
	result, err := h.service.SubmitTask(r.Context(), a, app.SubmitTaskInput{
		TaskID:         r.PathValue("task_id"),
		IdempotencyKey: key,
		ActorID:        a.UserID,
		ActorType:      "operator",
		TraceID:        httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, r, "pc care submit task", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, submitTaskResponse{
		TaskID:      result.TaskID,
		Status:      result.Status,
		RowVersion:  result.RowVersion,
		AnimalCount: result.AnimalCount,
	})
}

func (h *Handler) PostStockVerdict(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	var body stockVerdictRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	task, err := h.service.RecordStockVerdict(r.Context(), a, app.StockVerdictInput{
		TaskID:  r.PathValue("task_id"),
		Verdict: body.Verdict,
		Reason:  body.Reason,
		TraceID: httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, r, "pc care stock verdict", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, taskDTOFrom(task))
}

// writeServiceError maps the module's sentinel errors onto status codes with stable machine
// codes a client can branch on without parsing prose.
func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, op string, err error) {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound, codedError{Code: "not_found", Message: "not found"}, nil)
	case errors.Is(err, ports.ErrForbidden):
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden, codedError{Code: "forbidden", Message: "you do not have access to this"}, nil)
	case errors.Is(err, domain.ErrTaskNotAssigned):
		httpresponse.WriteError(w, r, h.log, http.StatusForbidden, codedError{Code: "task_not_assigned", Message: "this task is assigned to someone else"}, nil)
	case errors.Is(err, domain.ErrDuplicateScan):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, codedError{Code: "duplicate_scan", Message: "already scanned in this task"}, nil)
	case errors.Is(err, domain.ErrTaskNotOpen):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, codedError{Code: "task_locked", Message: "this task is not open for changes"}, nil)
	case errors.Is(err, domain.ErrTaskAlreadyPlanned):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, codedError{Code: "task_already_planned", Message: "a task for this pen, work and date already exists"}, nil)
	case errors.Is(err, ports.ErrIdempotencyConflict):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, domain.ErrProofIncomplete):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "proof_incomplete", Message: "some animals are still missing required videos"}, nil)
	case errors.Is(err, domain.ErrNoAnimals):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "no_animals", Message: "no animals scanned in this task yet"}, nil)
	case errors.Is(err, ports.ErrProofRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "proof_required", Message: "a video is required"}, nil)
	case errors.Is(err, ports.ErrInvalidProof):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "invalid_proof", Message: "the video could not be verified; record it again"}, nil)
	case errors.Is(err, domain.ErrInvalidSlotForCategory):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "invalid_slot", Message: "this video step does not belong to this work"}, nil)
	case errors.Is(err, domain.ErrInvalidCategory):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "invalid_category", Message: "unknown work category"}, nil)
	case errors.Is(err, domain.ErrKernelOwnedCategory):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "kernel_owned_category", Message: "this work is created automatically"}, nil)
	case errors.Is(err, domain.ErrNotStockTask):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "not_stock_task", Message: "this task is not a vaccine stock task"}, nil)
	case errors.Is(err, domain.ErrStockVerdictNotPending):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict, codedError{Code: "verdict_not_pending", Message: "this task is not awaiting approval"}, nil)
	case errors.Is(err, domain.ErrInvalidStockVerdict):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "invalid_verdict", Message: "unknown decision"}, nil)
	case errors.Is(err, domain.ErrStockRejectReasonRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "reason_required", Message: "say why this is being sent back"}, nil)
	case errors.Is(err, domain.ErrAssigneesRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "assignees_required", Message: "assign at least one operator"}, nil)
	case errors.Is(err, domain.ErrFastingWindowClosed):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "fasting_window_closed", Message: "feed & water removal happens the evening before, and there is no evening left before this date — pick a later deworming date"}, nil)
	case errors.Is(err, domain.ErrRemovalOperatorsRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "removal_operators_required", Message: "name at least one operator for the feed & water removal"}, nil)
	case errors.Is(err, domain.ErrFeedRemovalNotApplicable):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, codedError{Code: "feed_removal_not_applicable", Message: "feed & water removal applies to deworming only"}, nil)
	case errors.Is(err, ports.ErrShedNotInPark), errors.Is(err, ports.ErrInvalidPartition):
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, err.Error(), nil)
	case errors.Is(err, ports.ErrInvalidArgument), errors.Is(err, ports.ErrIdempotencyRequired):
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, ports.ErrStoreUnavailable):
		// A wiring/deployment fault, not a client error.
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, op, err)
	default:
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, op, err)
	}
}
