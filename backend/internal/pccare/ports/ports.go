// Package ports declares the PC Care module's boundaries: the task store (the module's ONE
// owned write surface over pc_care_tasks / pc_care_task_assignees / pc_care_task_animals), the
// proof validator seam, and the verification enqueue seam. The shapes clone the feed packing
// gate (feeddirection/ports/packing_ports.go), the closest verified template.
package ports

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrStoreUnavailable is returned when a write is attempted but no TaskStore is wired — a
	// deployment/wiring error, surfaced as a 500.
	ErrStoreUnavailable = errors.New("pccare: task store is not configured")
	// ErrIdempotencyConflict is returned when the same idempotency key is replayed with a
	// different request payload.
	ErrIdempotencyConflict = errors.New("pccare: idempotency key reused with a different request")
	// ErrIdempotencyRequired is returned when a mutating request omits its idempotency key.
	ErrIdempotencyRequired = errors.New("pccare: idempotency key is required")
	// ErrNotFound hides both "no such task" and "task outside your park scope" (existence must
	// not leak across park boundaries).
	ErrNotFound = errors.New("pccare: not found")
	// ErrForbidden is the role-gate failure for a caller whose roles lack the capability.
	ErrForbidden = errors.New("pccare: forbidden")
	// ErrInvalidArgument covers malformed ids/dates/paging.
	ErrInvalidArgument = errors.New("pccare: invalid argument")
	// ErrShedNotInPark is returned when the addressed shed is not an active shed of the park.
	ErrShedNotInPark = errors.New("pccare: shed is not an active shed of the addressed park")
	// ErrInvalidPartition is returned when a partitioned shed is addressed without a real
	// catalog partition, or an undivided shed with a fabricated one.
	ErrInvalidPartition = errors.New("pccare: partition_label is required and must match the shed partition catalog")
	// ErrInvalidProof is returned when a supplied proof reference does not resolve to a real,
	// completed, tenant-owned, live-camera proof upload of the expected media kind.
	ErrInvalidProof = errors.New("pccare: proof reference is invalid")
	// ErrProofRequired is returned when a slot registration omits its proof ref.
	ErrProofRequired = errors.New("pccare: a proof reference is required")
)

// CreateTaskParams is the planner's create write (CEO-only route).
type CreateTaskParams struct {
	TenantID string
	Category string
	ParkID   string
	ShedID   string
	// PartitionLabel is the pen the task covers ("2", "Part 3"); empty for an undivided shed.
	PartitionLabel string
	// PlannedBusinessDate is the immutable date the CEO chose (due starts equal to it).
	PlannedBusinessDate time.Time
	// AssigneeUserIDs are the operators authorized to work this task (one or more).
	AssigneeUserIDs []string
	IdempotencyKey  string
	CreatedBy       string
	ActorID         string
	ActorType       string
	TraceID         string
}

// TaskRow is one task as served to planner/monitor/worklist reads and echoed by writes.
type TaskRow struct {
	TaskID              string
	Category            string
	ParkID              string
	ParkName            string
	ShedID              string
	ShedName            string
	PartitionLabel      string
	PlannedBusinessDate string
	DueBusinessDate     string
	WorkState           string
	Status              string
	ReworkReason        string
	RowVersion          int32
	SubmittedBy         string
	SubmittedAt         *time.Time
	AssigneeUserIDs     []string
	AssigneeNames       []string
	// AnimalCount is this task's scanned-animal count (a per-task COUNT bounded by one task).
	AnimalCount int32
	// InventoryRequirements snapshots vaccine stock requirements for inventory_vaccine tasks.
	InventoryRequirements []InventoryRequirement
	// TaskProofs snapshots task-level proof rows for inventory_vaccine tasks.
	TaskProofs []TaskProofRow
}

// InventoryRequirement is one vaccine/count line displayed on the inventory_vaccine card.
type InventoryRequirement struct {
	VaccineLabel   string
	RequiredDoses  int32
	SourceBatchIDs []string
}

// ScanAnimalParams records one RFID into a task, at scan time, so peers see it and dedup
// happens at the scan.
type ScanAnimalParams struct {
	TenantID string
	TaskID   string
	// ScannedIdentifier is stored VERBATIM (free-flow; no herd lookup).
	ScannedIdentifier string
	ScannedBy         string
	IdempotencyKey    string
	ActorID           string
	ActorType         string
	TraceID           string
}

// ScanAnimalResult echoes the durable scan row.
type ScanAnimalResult struct {
	AnimalRowID string
	// Replayed is true when the request was an exact idempotent replay of an earlier scan.
	Replayed bool
}

// RegisterSlotProofParams attaches one slot's video to one scanned animal. Any assignee may
// fill any slot; a same-ref re-send is an idempotent no-op, and while the task is still open a
// DIFFERENT ref REPLACES the slot's clip (the phone retires the old clip only after the new
// one is SYNCED — "Manohar ordering"). A locked task refuses the write upstream.
type RegisterSlotProofParams struct {
	TenantID    string
	TaskID      string
	AnimalRowID string
	// SlotFieldKey is one of the task category's expected slots (domain.Slot*).
	SlotFieldKey   string
	ProofRef       string
	CapturedBy     string
	IdempotencyKey string
	ActorID        string
	ActorType      string
	TraceID        string
}

// RegisterTaskProofParams attaches task-level proof, used by inventory_vaccine fridge-stock checks.
type RegisterTaskProofParams struct {
	TenantID       string
	TaskID         string
	SlotKey        string
	ProofRef       string
	CapturedBy     string
	IdempotencyKey string
	ActorID        string
	ActorType      string
	TraceID        string
}

// TaskProofRow is one task-level proof row.
type TaskProofRow struct {
	SlotKey        string
	ProofRef       string
	CapturedBy     string
	CapturedByName string
	CapturedAt     time.Time
}

// AnimalRow is one scanned animal with its slot map, for the task detail / captures poll.
type AnimalRow struct {
	AnimalRowID       string
	ScannedIdentifier string
	ScannedBy         string
	ScannedByName     string
	ScannedAt         time.Time
	Slots             []AnimalSlot
}

// AnimalSlot is one slot's server-side state on one animal.
type AnimalSlot struct {
	FieldKey       string
	ProofRef       string
	CapturedBy     string
	CapturedByName string
	CapturedAt     *time.Time
}

// LabeledRef pairs a media ref with the verifier-facing label naming the animal and slot
// ("954000012345 · Before trimming").
type LabeledRef struct {
	ProofRef string
	Label    string
}

// SubmitTaskParams submits the WHOLE task (any assignee) once every scanned animal carries its
// full slot set.
type SubmitTaskParams struct {
	TenantID       string
	TaskID         string
	SubmittedBy    string
	IdempotencyKey string
	ActorID        string
	ActorType      string
	TraceID        string
	Now            time.Time
}

// SubmitTaskResult reports the submit outcome, mirroring feed's CompletePackingResult: the
// NewlyPending flag drives the exactly-once verification enqueue, and RowVersion keys it.
type SubmitTaskResult struct {
	TaskID   string
	Category string
	Status   string
	// RowVersion after the write; keys the verification item's idempotency so a rework
	// re-submit enqueues a fresh item while a retry collapses onto one.
	RowVersion int32
	// NewlyPending is true ONLY when the task entered pending_verification on THIS call.
	NewlyPending        bool
	ParkID              string
	ShedID              string
	ShedName            string
	PartitionLabel      string
	PlannedBusinessDate string
	// MediaRefs are every animal's slot videos with their verifier-facing labels, in scan
	// order then slot order — the ONE verification item's media set.
	MediaRefs []LabeledRef
	// AnimalCount is the number of scanned animals covered by the submit.
	AnimalCount int32
}

// ApplyVerifiedTaskParams flips an approved task pending_verification -> completed (status AND
// work_state) and emits pc_care.task.completed, in one transaction. Issued by the
// verification.verdict.approved consumer.
type ApplyVerifiedTaskParams struct {
	TenantID   string
	TaskID     string
	VerifiedBy string
	TraceID    string
}

// BounceTaskParams flips a rejected task pending_verification -> rework with the verifier's
// reason. Issued by the verification.verdict.rework consumer.
type BounceTaskParams struct {
	TenantID string
	TaskID   string
	Reason   string
	TraceID  string
}

// ListTasksQuery scopes the monitor list / operator worklist.
type ListTasksQuery struct {
	TenantID string
	// AuthorizedParkIDs clamps the read to the caller's park grants (empty + TenantWide=false
	// means no parks — serve nothing).
	AuthorizedParkIDs []string
	TenantWide        bool
	// ParkID optionally narrows to one park (must be inside the authorized set).
	ParkID string
	// Category optionally narrows to one category (the per-tab worklist read).
	Category string
	// DueBusinessDate is the day being worked ("2026-08-21").
	DueBusinessDate string
	// CurrentOrCarry returns open carry-over tasks due on or before DueBusinessDate, plus that
	// day's just-finished cards. Used by operator/director worklists so old finished cards do not
	// keep resurfacing.
	CurrentOrCarry bool
	// AssigneeUserID, when set, narrows to tasks assigned to this operator (the worklist).
	AssigneeUserID string
	Limit          int
	Cursor         string
}

// TaskPage is one bounded page of tasks.
type TaskPage struct {
	Items      []TaskRow
	NextCursor string
}

// PlannerShed is one shed/pen option for the create wizard, decorated with any existing live
// task for the chosen category+date.
type PlannerShed struct {
	ShedID         string
	ShedName       string
	PartitionLabel string
	// ExistingTaskID is non-empty when a live (non-canceled) task already covers this pen for
	// the chosen category+date — the wizard greys the row rather than letting create 409.
	ExistingTaskID string
}

// PlannerOperator is one assignable operator for the create wizard.
type PlannerOperator struct {
	UserID      string
	DisplayName string
	ParkIDs     []string
}

// PlannerCatalog is the park-grain wizard vocabulary (parks + operator picker), weighing
// PlannerCatalog shape.
type PlannerCatalog struct {
	Parks     []PlannerPark
	Operators []PlannerOperator
}

// PlannerPark is one pickable park.
type PlannerPark struct {
	ParkID   string
	ParkName string
}

// PlannerParkSheds is one keyset page of a park's pens for the wizard.
type PlannerParkSheds struct {
	Sheds      []PlannerShed
	NextCursor string
}

// TaskRosterPage is one keyset page of the RFIDs of animals currently resident in a task's pen —
// the roster-pick capture mode's tap list. Identifiers are served verbatim; tapping one records a
// normal free-flow scan, so the roster NEVER gates what a scan may store.
type TaskRosterPage struct {
	Identifiers []string
	NextCursor  string
}

// TaskStore owns the three pc_care_* tables. All writes are transactional with audit +
// request-level idempotency, mirroring the feed packing store contract.
type TaskStore interface {
	// CreateTask inserts the task + its assignees, idempotent on the request key; a live
	// natural-key collision returns domain.ErrTaskAlreadyPlanned.
	CreateTask(ctx context.Context, p CreateTaskParams) (TaskRow, error)

	// CancelTask flips work_state -> canceled (planner authority). Idempotent; a terminal task
	// is a no-op.
	CancelTask(ctx context.Context, tenantID, taskID, actorID, traceID string) error

	// GetTask reads one task row (with assignees + animal count), clamped to the authorized
	// parks. Returns ErrNotFound outside scope.
	GetTask(ctx context.Context, tenantID, taskID string, authorizedParkIDs []string, tenantWide bool) (TaskRow, error)

	// ListTasks serves the monitor list and (with AssigneeUserID) the operator worklist, one
	// bounded page, newest due date first.
	ListTasks(ctx context.Context, q ListTasksQuery) (TaskPage, error)

	// IsAssignee reports whether userID is an assignee of the task.
	IsAssignee(ctx context.Context, tenantID, taskID, userID string) (bool, error)

	// ScanAnimal inserts the scan row; a duplicate tag in the same task returns
	// domain.ErrDuplicateScan; a locked/terminal task returns domain.ErrTaskNotOpen.
	ScanAnimal(ctx context.Context, p ScanAnimalParams) (ScanAnimalResult, error)

	// RegisterSlotProof stores one slot's video ref + attribution on one animal row.
	RegisterSlotProof(ctx context.Context, p RegisterSlotProofParams) error

	// RegisterTaskProof stores one task-level proof ref + attribution.
	RegisterTaskProof(ctx context.Context, p RegisterTaskProofParams) error

	// ListTaskProofs reads task-level proof rows.
	ListTaskProofs(ctx context.Context, tenantID, taskID string) ([]TaskProofRow, error)

	// ListTaskAnimals pages one task's scanned animals with their slot maps (the peer
	// visibility poll). Keyset on animal_row_id.
	ListTaskAnimals(ctx context.Context, tenantID, taskID, cursor string, limit int) ([]AnimalRow, string, error)

	// TaskShedRoster pages the active RFIDs of alive animals currently in the task's shed
	// (narrowed to the task's partition when one is set), keyset on identifier value — the
	// roster-pick capture list for the trimming categories.
	TaskShedRoster(ctx context.Context, tenantID, taskID, cursor string, limit int) (TaskRosterPage, error)

	// SubmitTask flips open/rework -> pending_verification when every scanned animal carries
	// its full slot set, stamps submitted_by/at on the task and submitted_at on the animal
	// rows, bumps row_version, and composes the labeled media set. Idempotent on the request
	// key; NewlyPending is true only on a real transition.
	SubmitTask(ctx context.Context, p SubmitTaskParams) (SubmitTaskResult, error)

	// ApplyVerifiedTask flips pending_verification -> completed (both state columns), stamps
	// verified_by/at, and emits pc_care.task.completed in ONE transaction. Stale/duplicate
	// verdicts return false with no side effects.
	ApplyVerifiedTask(ctx context.Context, p ApplyVerifiedTaskParams) (bool, error)

	// BounceTaskForRework flips pending_verification -> rework with the reason. Idempotent and
	// stale-guarded.
	BounceTaskForRework(ctx context.Context, p BounceTaskParams) (bool, error)

	// PlannerCatalog returns the tenant's parks + assignable operators (service filters to the
	// caller's authorized parks).
	PlannerCatalog(ctx context.Context, tenantID string) (PlannerCatalog, error)

	// PlannerParkSheds pages one park's pens (shed_partitions catalog grain), each decorated
	// with any existing live task for category+date.
	PlannerParkSheds(ctx context.Context, tenantID, parkID, category, plannedBusinessDate, cursor string, limit int) (PlannerParkSheds, error)
}

// ProofValidator asserts every referenced proof is real, completed, tenant-owned, and live-camera
// captured (mime-aware on both the declared proof_type and stored mime).
type ProofValidator interface {
	ValidateLiveCameraVideos(ctx context.Context, tenantID string, proofIDs []string) error
	ValidateLiveCameraMedia(ctx context.Context, tenantID string, proofIDs []string) error
	ValidateLiveCameraProofKind(ctx context.Context, tenantID string, proofIDs []string, requiredKind string) error
}
