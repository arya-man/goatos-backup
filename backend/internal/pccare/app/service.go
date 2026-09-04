// Package app is the PC Care module's application layer: planner writes/reads (CEO), operator
// capture writes (assignee-gated), the whole-task submit that enqueues ONE verification item,
// and the verdict consumer. Templates: feeddirection/app/packing_service.go (gated submit) and
// weighing/app/service.go (planner + park-scope idioms).
package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// VerificationEnqueuer enqueues the ONE verification item a submitted PC Care task produces. The
// composition layer adapts verification's CreateItem to this narrow port so pccare never touches
// verification's tables directly.
type VerificationEnqueuer interface {
	EnqueuePCCareVerification(ctx context.Context, in VerificationEnqueueRequest) error
}

// VerificationEnqueueRequest is one submitted task (all its animals' videos) handed to the
// verifier queue.
type VerificationEnqueueRequest struct {
	TenantID            string
	TaskID              string
	Category            string
	ParkID              string
	ShedID              string
	ShedName            string
	PartitionLabel      string
	VaccineLabel        string
	PlannedBusinessDate string
	MediaRefs           []ports.LabeledRef
	AnimalCount         int32
	OperatorID          string
	CapturedAt          time.Time
	IdempotencyKey      string
}

// Service is the PC Care application service.
type Service struct {
	store    ports.TaskStore
	proofs   ports.ProofValidator
	enqueuer VerificationEnqueuer
	now      func() time.Time
}

// NewService constructs the service over the task store.
func NewService(store ports.TaskStore) *Service {
	return &Service{store: store, now: time.Now}
}

// WithProofValidator wires proof-honesty validation (optional in pure unit tests, wired in
// production).
func (s *Service) WithProofValidator(v ports.ProofValidator) *Service {
	s.proofs = v
	return s
}

// WithVerificationEnqueuer wires the verifier-queue seam.
func (s *Service) WithVerificationEnqueuer(e VerificationEnqueuer) *Service {
	s.enqueuer = e
	return s
}

// WithNow overrides the clock (tests).
func (s *Service) WithNow(now func() time.Time) *Service {
	s.now = now
	return s
}

// planCapabilities are the capabilities that PLAN: pc_care.plan covers every planner category
// (CEO, the weighing.plan precedent); pc_care.plan_trimming covers hoof and hair trimming only
// (the Breeding Director, maintainer decision 2026-09-04). The route table admits either on the
// planner routes; WHICH category a holder may write is decided here, per request, because a
// route cannot see a category.
var planCapabilities = []string{permissions.PCCarePlan, permissions.PCCarePlanTrimming}

// planOrMonitorParkCapabilities is the alternative set behind every planner/oversight surface.
var planOrMonitorParkCapabilities = []string{permissions.PCCarePlan, permissions.PCCarePlanTrimming, permissions.PCCareMonitor, permissions.PCCareOverseeOperators}

// canPlanOrMonitor is the planner's read gate: writes belong to the plan capabilities, but
// read-only oversight (monitor/oversee) may look at the same vocabulary.
func (s *Service) canPlanOrMonitor(actor domain.Actor) bool {
	return permissions.RolesAuthorizeAny(actor.Roles, planOrMonitorParkCapabilities)
}

// canPlanAny reports whether the actor holds ANY planning capability -- the first gate on a
// planner write, answered before the category is even parsed so a non-planner is refused the
// same way whatever they send.
func canPlanAny(actor domain.Actor) bool {
	return permissions.RolesAuthorizeAny(actor.Roles, planCapabilities)
}

// planCapabilitiesForCategory names the capabilities that authorize planning THIS category.
// pc_care.plan always does; pc_care.plan_trimming only for domain.TrimmingCategories. The same
// list feeds the park-scope check, so a trimming planner is scoped by the parks their trimming
// grant covers and never by a broader capability they do not hold.
func planCapabilitiesForCategory(category string) []string {
	if domain.IsTrimmingCategory(category) {
		return planCapabilities
	}
	return []string{permissions.PCCarePlan}
}

// canPlanCategory is the category gate: does the actor hold a capability that plans `category`?
// A holder of pc_care.plan_trimming asking for deworming is refused here as ErrForbidden, the
// same answer a non-planner gets, because to them that category is not theirs to plan.
func canPlanCategory(actor domain.Actor, category string) bool {
	return permissions.RolesAuthorizeAny(actor.Roles, planCapabilitiesForCategory(category))
}

// plannableCategories is the wizard vocabulary for this actor: every planner category for a
// pc_care.plan holder, the trimming pair for a pc_care.plan_trimming holder, and the full
// planner list for a read-only monitor (who is never offered the wizard; the list still labels
// the board's category filter). Order follows domain.PlannerCategories.
func plannableCategories(actor domain.Actor) []string {
	if !canPlanAny(actor) {
		return append([]string(nil), domain.PlannerCategories...)
	}
	out := make([]string, 0, len(domain.PlannerCategories))
	for _, category := range domain.PlannerCategories {
		if canPlanCategory(actor, category) {
			out = append(out, category)
		}
	}
	return out
}

// authorizedParkSet returns the parks in which the actor holds any of `capabilities`, and
// whether the actor is tenant-wide for one of them (weighing authorizedParkSet clone).
func authorizedParkSet(ctx context.Context, tenantID string, capabilities ...string) (parks map[string]struct{}, tenantWide bool) {
	grants := httpmiddleware.AuthGrantsFromContext(ctx)
	// No grants at all = internal/service context (CLI, integration test), unrestricted.
	if len(grants) == 0 {
		return nil, true
	}
	parks = map[string]struct{}{}
	for _, capability := range capabilities {
		if hasTenantWideCapability(grants, tenantID, capability) {
			return nil, true
		}
		for _, parkID := range httpmiddleware.AuthorizedParkIDsForCapability(grants, capability) {
			parks[parkID] = struct{}{}
		}
	}
	return parks, false
}

func hasTenantWideCapability(grants []permissions.ActiveGrant, tenantID, capability string) bool {
	for _, grant := range grants {
		if grant.ScopeType == "tenant" && grant.ScopeID == tenantID && permissions.RoleHasPermission(grant.Role, capability) {
			return true
		}
	}
	return false
}

// checkParkScopeForAnyCapability admits the actor when ANY of `capabilities` is held in
// `parkID` (weighing clone: a role check alone answers "somewhere", never "here").
func checkParkScopeForAnyCapability(ctx context.Context, tenantID, parkID string, capabilities ...string) error {
	parks, tenantWide := authorizedParkSet(ctx, tenantID, capabilities...)
	if tenantWide {
		return nil
	}
	if _, ok := parks[parkID]; ok {
		return nil
	}
	return ports.ErrNotFound
}

// authorizedParkSlice renders the set as the slice the store's clamped reads take.
func authorizedParkSlice(parks map[string]struct{}) []string {
	out := make([]string, 0, len(parks))
	for id := range parks {
		out = append(out, id)
	}
	return out
}

func isBusinessDate(v string) bool {
	_, err := time.Parse("2006-01-02", v)
	return err == nil
}

// ---------------------------------------------------------------------------
// Planner (CEO)
// ---------------------------------------------------------------------------

// PlannerCatalog returns the park-grain create-wizard vocabulary: pickable parks + assignable
// operators, filtered to the actor's capability-scoped parks (weighing PlannerCatalog shape).
func (s *Service) PlannerCatalog(ctx context.Context, actor domain.Actor) (ports.PlannerCatalog, error) {
	if !s.canPlanOrMonitor(actor) {
		return ports.PlannerCatalog{}, ports.ErrForbidden
	}
	catalog, err := s.store.PlannerCatalog(ctx, actor.TenantID)
	if err != nil {
		return ports.PlannerCatalog{}, err
	}
	// The category vocabulary is the ACTOR's, not the module's: a trimming-only planner's
	// wizard offers hoof and hair trimming and nothing else, so the phone never shows a
	// category the create write would refuse.
	catalog.Categories = plannableCategories(actor)
	authorizedParks, tenantWide := authorizedParkSet(ctx, actor.TenantID, planOrMonitorParkCapabilities...)
	if tenantWide {
		return catalog, nil
	}
	parks := make([]ports.PlannerPark, 0, len(catalog.Parks))
	for _, park := range catalog.Parks {
		if _, ok := authorizedParks[park.ParkID]; ok {
			parks = append(parks, park)
		}
	}
	catalog.Parks = parks
	// The operator picker follows the same filter; an operator with EMPTY ParkIDs means "every
	// park" (cross-park director) and stays offered (weighing precedent).
	operators := make([]ports.PlannerOperator, 0, len(catalog.Operators))
	for _, operator := range catalog.Operators {
		if len(operator.ParkIDs) == 0 {
			operators = append(operators, operator)
			continue
		}
		for _, parkID := range operator.ParkIDs {
			if _, ok := authorizedParks[parkID]; ok {
				operators = append(operators, operator)
				break
			}
		}
	}
	catalog.Operators = operators
	return catalog, nil
}

// PlannerParkSheds pages one park's pens for the wizard, decorated with any existing live task
// for the chosen category+date so the wizard greys taken pens instead of letting create 409.
func (s *Service) PlannerParkSheds(ctx context.Context, actor domain.Actor, parkID, category, plannedBusinessDate, cursor string, limit int) (ports.PlannerParkSheds, error) {
	if !s.canPlanOrMonitor(actor) {
		return ports.PlannerParkSheds{}, ports.ErrForbidden
	}
	parkID = strings.TrimSpace(parkID)
	if !uuidutil.IsUUIDString(parkID) {
		return ports.PlannerParkSheds{}, ports.ErrInvalidArgument
	}
	if err := checkParkScopeForAnyCapability(ctx, actor.TenantID, parkID, planOrMonitorParkCapabilities...); err != nil {
		return ports.PlannerParkSheds{}, err
	}
	category = strings.TrimSpace(category)
	if !domain.IsValidCategory(category) {
		return ports.PlannerParkSheds{}, domain.ErrInvalidCategory
	}
	if domain.IsKernelOwnedCategory(category) {
		return ports.PlannerParkSheds{}, domain.ErrKernelOwnedCategory
	}
	// A planner may page pens only for a category they can plan; a monitor/overseer, who plans
	// nothing, keeps the read-only look at the whole vocabulary they had before.
	if canPlanAny(actor) && !canPlanCategory(actor, category) {
		return ports.PlannerParkSheds{}, ports.ErrForbidden
	}
	plannedBusinessDate = strings.TrimSpace(plannedBusinessDate)
	if !isBusinessDate(plannedBusinessDate) {
		return ports.PlannerParkSheds{}, ports.ErrInvalidArgument
	}
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	return s.store.PlannerParkSheds(ctx, actor.TenantID, parkID, category, plannedBusinessDate, strings.TrimSpace(cursor), limit)
}

// CreateTaskInput is the planner's create request as built by the HTTP handler.
type CreateTaskInput struct {
	Category            string
	ParkID              string
	ShedID              string
	PartitionLabel      string
	PlannedBusinessDate string
	AssigneeUserIDs     []string
	IdempotencyKey      string
	ActorID             string
	ActorType           string
	TraceID             string
}

// CreateTask plans one task. Write authority is a plan capability -- PCCarePlan (CEO) for any
// planner category, PCCarePlanTrimming (Breeding Director) for hoof/hair trimming -- park-scoped
// through whichever of those authorizes the requested category.
func (s *Service) CreateTask(ctx context.Context, actor domain.Actor, in CreateTaskInput) (ports.TaskRow, error) {
	if !canPlanAny(actor) {
		return ports.TaskRow{}, ports.ErrForbidden
	}
	in.ParkID = strings.TrimSpace(in.ParkID)
	in.ShedID = strings.TrimSpace(in.ShedID)
	if !uuidutil.IsUUIDString(in.ParkID) || !uuidutil.IsUUIDString(in.ShedID) {
		return ports.TaskRow{}, ports.ErrInvalidArgument
	}
	in.Category = strings.TrimSpace(in.Category)
	if !domain.IsValidCategory(in.Category) {
		return ports.TaskRow{}, domain.ErrInvalidCategory
	}
	if domain.IsKernelOwnedCategory(in.Category) {
		return ports.TaskRow{}, domain.ErrKernelOwnedCategory
	}
	// The category gate comes AFTER the category is known to be real and human-plannable, so a
	// trimming planner sending deworming is told "not yours" rather than "no such category".
	if !canPlanCategory(actor, in.Category) {
		return ports.TaskRow{}, ports.ErrForbidden
	}
	if err := checkParkScopeForAnyCapability(ctx, actor.TenantID, in.ParkID, planCapabilitiesForCategory(in.Category)...); err != nil {
		return ports.TaskRow{}, err
	}
	in.PlannedBusinessDate = strings.TrimSpace(in.PlannedBusinessDate)
	if !isBusinessDate(in.PlannedBusinessDate) {
		return ports.TaskRow{}, ports.ErrInvalidArgument
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return ports.TaskRow{}, ports.ErrIdempotencyRequired
	}
	assignees := make([]string, 0, len(in.AssigneeUserIDs))
	seen := map[string]struct{}{}
	for _, id := range in.AssigneeUserIDs {
		id = strings.TrimSpace(id)
		if !uuidutil.IsUUIDString(id) {
			return ports.TaskRow{}, ports.ErrInvalidArgument
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		assignees = append(assignees, id)
	}
	if len(assignees) == 0 {
		return ports.TaskRow{}, domain.ErrAssigneesRequired
	}
	planned, err := time.ParseInLocation("2006-01-02", in.PlannedBusinessDate, biztime.DefaultLocation())
	if err != nil {
		return ports.TaskRow{}, ports.ErrInvalidArgument
	}
	return s.store.CreateTask(ctx, ports.CreateTaskParams{
		TenantID:            actor.TenantID,
		Category:            in.Category,
		ParkID:              in.ParkID,
		ShedID:              in.ShedID,
		PartitionLabel:      strings.TrimSpace(in.PartitionLabel),
		PlannedBusinessDate: planned,
		AssigneeUserIDs:     assignees,
		IdempotencyKey:      strings.TrimSpace(in.IdempotencyKey),
		CreatedBy:           actor.UserID,
		ActorID:             in.ActorID,
		ActorType:           in.ActorType,
		TraceID:             in.TraceID,
	})
}

// CancelTask cancels an unfinished task (planner authority, park-scoped through the task read).
// The task's own category decides which plan capability must be held: a trimming planner can
// cancel a hoof-trimming task and is refused a deworming one, exactly as on create.
func (s *Service) CancelTask(ctx context.Context, actor domain.Actor, taskID, traceID string) error {
	if !canPlanAny(actor) {
		return ports.ErrForbidden
	}
	taskID = strings.TrimSpace(taskID)
	if !uuidutil.IsUUIDString(taskID) {
		return ports.ErrInvalidArgument
	}
	parks, tenantWide := authorizedParkSet(ctx, actor.TenantID, planCapabilities...)
	task, err := s.store.GetTask(ctx, actor.TenantID, taskID, authorizedParkSlice(parks), tenantWide)
	if err != nil {
		return err
	}
	if !canPlanCategory(actor, task.Category) {
		return ports.ErrForbidden
	}
	// Re-clamp the park to the capability that actually authorizes THIS category: the read
	// above admitted any planning grant so the task could be found, but a trimming grant
	// scoped to one park must not cancel a trimming task in another.
	if err := checkParkScopeForAnyCapability(ctx, actor.TenantID, task.ParkID, planCapabilitiesForCategory(task.Category)...); err != nil {
		return err
	}
	return s.store.CancelTask(ctx, actor.TenantID, task.TaskID, actor.UserID, traceID)
}

// ---------------------------------------------------------------------------
// Reads (monitor list, worklist, detail, captures poll)
// ---------------------------------------------------------------------------

// monitorReadCapabilities admit the flat task list.
var monitorReadCapabilities = []string{permissions.PCCarePlan, permissions.PCCarePlanTrimming, permissions.PCCareMonitor, permissions.PCCareOverseeOperators}

// ListTasks is the plan/monitor/oversee flat list for one due date, park-clamped.
func (s *Service) ListTasks(ctx context.Context, actor domain.Actor, parkID, category, dueBusinessDate, cursor string, limit int, currentOrCarry bool) (ports.TaskPage, error) {
	if !permissions.RolesAuthorizeAny(actor.Roles, monitorReadCapabilities) {
		return ports.TaskPage{}, ports.ErrForbidden
	}
	if !isBusinessDate(strings.TrimSpace(dueBusinessDate)) {
		return ports.TaskPage{}, ports.ErrInvalidArgument
	}
	if category = strings.TrimSpace(category); category != "" && !domain.IsValidCategory(category) {
		return ports.TaskPage{}, domain.ErrInvalidCategory
	}
	parks, tenantWide := authorizedParkSet(ctx, actor.TenantID, monitorReadCapabilities...)
	if parkID = strings.TrimSpace(parkID); parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			return ports.TaskPage{}, ports.ErrInvalidArgument
		}
		if err := checkParkScopeForAnyCapability(ctx, actor.TenantID, parkID, monitorReadCapabilities...); err != nil {
			return ports.TaskPage{}, err
		}
	}
	return s.store.ListTasks(ctx, ports.ListTasksQuery{
		TenantID:          actor.TenantID,
		AuthorizedParkIDs: authorizedParkSlice(parks),
		TenantWide:        tenantWide,
		ParkID:            parkID,
		Category:          category,
		DueBusinessDate:   strings.TrimSpace(dueBusinessDate),
		CurrentOrCarry:    currentOrCarry,
		Limit:             clampLimit(limit),
		Cursor:            strings.TrimSpace(cursor),
	})
}

// Worklist is the operator's assigned-task list for one category tab and one due date.
func (s *Service) Worklist(ctx context.Context, actor domain.Actor, category, dueBusinessDate, cursor string, limit int) (ports.TaskPage, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.PCCareExecute}, false) {
		return ports.TaskPage{}, ports.ErrForbidden
	}
	category = strings.TrimSpace(category)
	if !domain.IsValidCategory(category) {
		return ports.TaskPage{}, domain.ErrInvalidCategory
	}
	if !isBusinessDate(strings.TrimSpace(dueBusinessDate)) {
		return ports.TaskPage{}, ports.ErrInvalidArgument
	}
	parks, tenantWide := authorizedParkSet(ctx, actor.TenantID, permissions.PCCareExecute)
	return s.store.ListTasks(ctx, ports.ListTasksQuery{
		TenantID:          actor.TenantID,
		AuthorizedParkIDs: authorizedParkSlice(parks),
		TenantWide:        tenantWide,
		Category:          category,
		DueBusinessDate:   strings.TrimSpace(dueBusinessDate),
		CurrentOrCarry:    true,
		AssigneeUserID:    actor.UserID,
		Limit:             clampLimit(limit),
		Cursor:            strings.TrimSpace(cursor),
	})
}

// taskReadCapabilities admit the task detail / captures poll: workers and overseers alike.
var taskReadCapabilities = []string{permissions.PCCareExecute, permissions.PCCarePlan, permissions.PCCarePlanTrimming, permissions.PCCareMonitor, permissions.PCCareOverseeOperators}

// GetTask reads one task (detail contract: row + expected slots composed by the handler).
func (s *Service) GetTask(ctx context.Context, actor domain.Actor, taskID string) (ports.TaskRow, error) {
	if !permissions.RolesAuthorizeAny(actor.Roles, taskReadCapabilities) {
		return ports.TaskRow{}, ports.ErrForbidden
	}
	taskID = strings.TrimSpace(taskID)
	if !uuidutil.IsUUIDString(taskID) {
		return ports.TaskRow{}, ports.ErrInvalidArgument
	}
	parks, tenantWide := authorizedParkSet(ctx, actor.TenantID, taskReadCapabilities...)
	return s.store.GetTask(ctx, actor.TenantID, taskID, authorizedParkSlice(parks), tenantWide)
}

// ListTaskAnimals is the peer-visibility poll: one task's scanned animals + slot maps.
func (s *Service) ListTaskAnimals(ctx context.Context, actor domain.Actor, taskID, cursor string, limit int) ([]ports.AnimalRow, string, error) {
	if _, err := s.GetTask(ctx, actor, taskID); err != nil {
		return nil, "", err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	return s.store.ListTaskAnimals(ctx, actor.TenantID, strings.TrimSpace(taskID), strings.TrimSpace(cursor), limit)
}

// TaskRoster pages the RFIDs of animals currently in the task's pen — the roster-pick capture
// mode's tap list. Read-gated exactly like the captures poll (any principal who can read the
// task); it never gates a scan.
func (s *Service) TaskRoster(ctx context.Context, actor domain.Actor, taskID, cursor string, limit int) (ports.TaskRosterPage, error) {
	if _, err := s.GetTask(ctx, actor, taskID); err != nil {
		return ports.TaskRosterPage{}, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	return s.store.TaskShedRoster(ctx, actor.TenantID, strings.TrimSpace(taskID), strings.TrimSpace(cursor), limit)
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 25
	}
	if limit > 100 {
		return 100
	}
	return limit
}

// ---------------------------------------------------------------------------
// Capture writes (assignee-gated)
// ---------------------------------------------------------------------------

// requireAssignee enforces the assigned-only rule: pc_care.execute alone never authorizes a
// write — the caller must also be named on the task (maintainer decision 2026-08-21).
func (s *Service) requireAssignee(ctx context.Context, actor domain.Actor, taskID string) error {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.PCCareExecute}, false) {
		return ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(taskID) {
		return ports.ErrInvalidArgument
	}
	// Internal/service contexts (no user id) skip the membership check the same way the park
	// scope helpers skip grant checks off context.Background().
	if strings.TrimSpace(actor.UserID) == "" {
		return nil
	}
	assigned, err := s.store.IsAssignee(ctx, actor.TenantID, taskID, actor.UserID)
	if err != nil {
		return err
	}
	if !assigned {
		return domain.ErrTaskNotAssigned
	}
	return nil
}

// ScanAnimalInput is one RFID scan-add.
type ScanAnimalInput struct {
	TaskID            string
	ScannedIdentifier string
	IdempotencyKey    string
	ActorID           string
	ActorType         string
	TraceID           string
}

// ScanAnimal records one scanned tag into the task, verbatim, at scan time.
func (s *Service) ScanAnimal(ctx context.Context, actor domain.Actor, in ScanAnimalInput) (ports.ScanAnimalResult, error) {
	in.TaskID = strings.TrimSpace(in.TaskID)
	if err := s.requireAssignee(ctx, actor, in.TaskID); err != nil {
		return ports.ScanAnimalResult{}, err
	}
	tag := strings.TrimSpace(in.ScannedIdentifier)
	if tag == "" {
		return ports.ScanAnimalResult{}, ports.ErrInvalidArgument
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return ports.ScanAnimalResult{}, ports.ErrIdempotencyRequired
	}
	return s.store.ScanAnimal(ctx, ports.ScanAnimalParams{
		TenantID:          actor.TenantID,
		TaskID:            in.TaskID,
		ScannedIdentifier: tag,
		ScannedBy:         actor.UserID,
		IdempotencyKey:    strings.TrimSpace(in.IdempotencyKey),
		ActorID:           in.ActorID,
		ActorType:         in.ActorType,
		TraceID:           in.TraceID,
	})
}

// RegisterSlotProofInput attaches one slot's video to one animal.
type RegisterSlotProofInput struct {
	TaskID         string
	AnimalRowID    string
	SlotFieldKey   string
	ProofRef       string
	IdempotencyKey string
	ActorID        string
	ActorType      string
	TraceID        string
}

// RegisterTaskProofInput attaches one task-level proof to a task.
type RegisterTaskProofInput struct {
	TaskID         string
	SlotKey        string
	ProofRef       string
	IdempotencyKey string
	ActorID        string
	ActorType      string
	TraceID        string
}

// RegisterTaskProof stores a task-level proof. It is currently used by
// inventory_vaccine, where the director proves fridge stock for the whole task.
func (s *Service) RegisterTaskProof(ctx context.Context, actor domain.Actor, in RegisterTaskProofInput) error {
	in.TaskID = strings.TrimSpace(in.TaskID)
	if err := s.requireAssignee(ctx, actor, in.TaskID); err != nil {
		return err
	}
	in.ProofRef = strings.TrimSpace(in.ProofRef)
	if in.ProofRef == "" {
		return ports.ErrProofRequired
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return ports.ErrIdempotencyRequired
	}
	if s.proofs != nil {
		requiredKind := ""
		switch strings.TrimSpace(in.SlotKey) {
		case domain.SlotStockFridgePhoto:
			requiredKind = "photo"
		case domain.SlotStockFridgeVideo:
			requiredKind = "video"
		}
		if requiredKind != "" {
			if err := s.proofs.ValidateLiveCameraProofKind(ctx, actor.TenantID, []string{in.ProofRef}, requiredKind); err != nil {
				return err
			}
		} else if err := s.proofs.ValidateLiveCameraMedia(ctx, actor.TenantID, []string{in.ProofRef}); err != nil {
			return err
		}
	}
	return s.store.RegisterTaskProof(ctx, ports.RegisterTaskProofParams{
		TenantID:       actor.TenantID,
		TaskID:         in.TaskID,
		SlotKey:        strings.TrimSpace(in.SlotKey),
		ProofRef:       in.ProofRef,
		CapturedBy:     actor.UserID,
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey),
		ActorID:        in.ActorID,
		ActorType:      in.ActorType,
		TraceID:        in.TraceID,
	})
}

// RegisterSlotProof stores one slot's video. The slot must belong to the task's category, and
// the proof must be a real, completed, tenant-owned, live-camera video.
func (s *Service) RegisterSlotProof(ctx context.Context, actor domain.Actor, in RegisterSlotProofInput) error {
	in.TaskID = strings.TrimSpace(in.TaskID)
	if err := s.requireAssignee(ctx, actor, in.TaskID); err != nil {
		return err
	}
	in.AnimalRowID = strings.TrimSpace(in.AnimalRowID)
	if !uuidutil.IsUUIDString(in.AnimalRowID) {
		return ports.ErrInvalidArgument
	}
	in.ProofRef = strings.TrimSpace(in.ProofRef)
	if in.ProofRef == "" {
		return ports.ErrProofRequired
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return ports.ErrIdempotencyRequired
	}
	// Slot validity against the CATEGORY is enforced in the store write, which reads the task
	// row in the same transaction; validating here would race a concurrent cancel.
	if s.proofs != nil {
		if err := s.proofs.ValidateLiveCameraVideos(ctx, actor.TenantID, []string{in.ProofRef}); err != nil {
			return err
		}
	}
	return s.store.RegisterSlotProof(ctx, ports.RegisterSlotProofParams{
		TenantID:       actor.TenantID,
		TaskID:         in.TaskID,
		AnimalRowID:    in.AnimalRowID,
		SlotFieldKey:   strings.TrimSpace(in.SlotFieldKey),
		ProofRef:       in.ProofRef,
		CapturedBy:     actor.UserID,
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey),
		ActorID:        in.ActorID,
		ActorType:      in.ActorType,
		TraceID:        in.TraceID,
	})
}

// SubmitTaskInput submits the whole task.
type SubmitTaskInput struct {
	TaskID         string
	IdempotencyKey string
	ActorID        string
	ActorType      string
	TraceID        string
}

// SubmitTask flips the task to pending_verification (readiness enforced in the store write) and
// enqueues ONE verification item carrying every animal's labeled videos or, for inventory_vaccine,
// the task-level fridge proof. Enqueue fires only when the task actually entered pending on THIS
// call (feed packing NewlyPending contract), keyed "pc-care-verification:<task_id>:<row_version>"
// so a rework re-submit mints a fresh item while a retry collapses onto one.
func (s *Service) SubmitTask(ctx context.Context, actor domain.Actor, in SubmitTaskInput) (ports.SubmitTaskResult, error) {
	if s.store == nil {
		return ports.SubmitTaskResult{}, ports.ErrStoreUnavailable
	}
	in.TaskID = strings.TrimSpace(in.TaskID)
	if err := s.requireAssignee(ctx, actor, in.TaskID); err != nil {
		return ports.SubmitTaskResult{}, err
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return ports.SubmitTaskResult{}, ports.ErrIdempotencyRequired
	}

	result, err := s.store.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID:       actor.TenantID,
		TaskID:         in.TaskID,
		SubmittedBy:    actor.UserID,
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey),
		ActorID:        in.ActorID,
		ActorType:      in.ActorType,
		TraceID:        in.TraceID,
		Now:            s.now().UTC(),
	})
	if err != nil {
		return ports.SubmitTaskResult{}, err
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Vaccine-stock verdict (PC Director)
// ---------------------------------------------------------------------------

// StockVerdictInput is the director's decision on one submitted inventory_vaccine task.
type StockVerdictInput struct {
	TaskID string
	// Verdict is domain.StockVerdictApprove or domain.StockVerdictReject.
	Verdict string
	// Reason is mandatory on a reject — it becomes the task's rework_reason, rendered verbatim
	// to the operators who must re-record the fridge. An approve never carries one.
	Reason  string
	TraceID string
}

// RecordStockVerdict applies the PC Director's approve/reject to a submitted vaccine-stock task
// (maintainer decision 2026-09-02). This is the module's own approval gate — the toxin shape —
// deliberately NOT a verification.verdict: the tenant verifier never sees stock work, and the
// operators who filmed the fridge cannot accept their own evidence (the route requires
// pc_care.stock_approve, which no operator holds).
//
// Approve reuses ApplyVerifiedTask (pending_verification -> completed on both state columns,
// pc_care.task.completed emitted in the same transaction); reject reuses BounceTaskForRework
// (-> rework with the director's reason). Both writes are state-guarded, so a replay of an
// already-applied verdict reads the task back and answers idempotently instead of erroring.
func (s *Service) RecordStockVerdict(ctx context.Context, actor domain.Actor, in StockVerdictInput) (ports.TaskRow, error) {
	if s.store == nil {
		return ports.TaskRow{}, ports.ErrStoreUnavailable
	}
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.PCCareStockApprove}, false) {
		return ports.TaskRow{}, ports.ErrForbidden
	}
	in.TaskID = strings.TrimSpace(in.TaskID)
	if !uuidutil.IsUUIDString(in.TaskID) {
		return ports.TaskRow{}, ports.ErrInvalidArgument
	}
	verdict := strings.TrimSpace(in.Verdict)
	if verdict != domain.StockVerdictApprove && verdict != domain.StockVerdictReject {
		return ports.TaskRow{}, domain.ErrInvalidStockVerdict
	}
	reason := strings.TrimSpace(in.Reason)
	if verdict == domain.StockVerdictReject && reason == "" {
		return ports.TaskRow{}, domain.ErrStockRejectReasonRequired
	}

	parks, tenantWide := authorizedParkSet(ctx, actor.TenantID, permissions.PCCareStockApprove)
	task, err := s.store.GetTask(ctx, actor.TenantID, in.TaskID, authorizedParkSlice(parks), tenantWide)
	if err != nil {
		return ports.TaskRow{}, err
	}
	if !domain.IsDirectorApprovedCategory(task.Category) {
		return ports.TaskRow{}, domain.ErrNotStockTask
	}

	var applied bool
	switch verdict {
	case domain.StockVerdictApprove:
		applied, err = s.store.ApplyVerifiedTask(ctx, ports.ApplyVerifiedTaskParams{
			TenantID:   actor.TenantID,
			TaskID:     task.TaskID,
			VerifiedBy: actor.UserID,
			TraceID:    in.TraceID,
		})
	case domain.StockVerdictReject:
		applied, err = s.store.BounceTaskForRework(ctx, ports.BounceTaskParams{
			TenantID: actor.TenantID,
			TaskID:   task.TaskID,
			Reason:   reason,
			TraceID:  in.TraceID,
		})
	}
	if err != nil {
		return ports.TaskRow{}, err
	}

	refreshed, err := s.store.GetTask(ctx, actor.TenantID, in.TaskID, authorizedParkSlice(parks), tenantWide)
	if err != nil {
		return ports.TaskRow{}, err
	}
	if applied {
		return refreshed, nil
	}
	// State-guarded no-op: idempotent when the task already sits where this verdict would have
	// put it (a retried tap), a conflict otherwise (it was never submitted, or the other verdict
	// landed first).
	switch {
	case verdict == domain.StockVerdictApprove && refreshed.Status == domain.StatusCompleted:
		return refreshed, nil
	case verdict == domain.StockVerdictReject && refreshed.Status == domain.StatusRework && strings.TrimSpace(refreshed.ReworkReason) == reason:
		return refreshed, nil
	}
	return ports.TaskRow{}, domain.ErrStockVerdictNotPending
}
