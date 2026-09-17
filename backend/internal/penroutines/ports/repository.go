// Package ports declares the seams the Pen Routines module depends on.
package ports

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
)

// Sentinel errors the adapters return; the transport maps them.
var (
	ErrTaskNotFound           = errors.New("pen routine: task not found")
	ErrRoutineNotFound        = errors.New("pen routine: routine not found")
	ErrIdempotencyConflict    = errors.New("pen routine: idempotency conflict")
	ErrInvalidArgument        = errors.New("pen routine: invalid argument")
	ErrNameTaken              = errors.New("pen routine: a routine with this name already exists in the park")
	ErrParkImmutable          = errors.New("pen routine: a routine's park cannot change")
	ErrRoutineVersionConflict = errors.New("pen routine: the routine changed since it was loaded")
)

// ListParams selects one page of the tasks owed on the routines whose roles the caller holds
// for the routine's park.
type ListParams struct {
	TenantID string
	UserID   string
	States   []string
	Limit    int
	Cursor   string
}

// Page is one page plus the whole-list counts the chips show.
type Page struct {
	Rows       []domain.Task
	NextCursor string
	// StateCounts range over the SAME role-holder predicate as the rows (never page-local,
	// never tenant-wide), keyed by work state.
	StateCounts map[string]int
}

// PresenceParams records one check-in / check-out punch on a task.
type PresenceParams struct {
	TenantID       string
	Actor          domain.Actor
	TaskID         string
	EventType      string
	CapturedAt     time.Time
	Location       domain.PresenceLocation
	Integrity      domain.PresenceIntegrity
	RowVersion     int
	IdempotencyKey string
	TraceID        string
}

// SubmitParams records the answers and captures and either completes the task or hands it
// to the verifier, per the routine's review kind.
type SubmitParams struct {
	TenantID string
	Actor    domain.Actor
	TaskID   string
	// Answers are the raw JSON values keyed by question id; the repository validates them
	// against the PINNED version's evidence under the row lock and stores the normalized map.
	Answers    map[string]json.RawMessage
	Proofs     []domain.ProofItem
	RowVersion int
	// CapturedAt / Location / Integrity describe the submit punch: when presence is in play
	// the submit records the leave with them. Zero values are accepted.
	CapturedAt     time.Time
	Location       domain.PresenceLocation
	Integrity      domain.PresenceIntegrity
	IdempotencyKey string
	TraceID        string
}

// VerdictParams is the verifier's decision on one task, applied by the verdict consumer.
type VerdictParams struct {
	TenantID   string
	TaskID     string
	VerifiedBy string
	Reason     string
	TraceID    string
}

// VerdictResult is what a verdict write did.
type VerdictResult struct {
	Applied bool
	Task    domain.Task
}

// RoutineRef names a routine in a log line.
type RoutineRef struct {
	RoutineID string
	Name      string
	ParkID    string
	// Roles are the routine's assignee roles, named so the log says WHICH role nobody holds.
	Roles []string
}

// MaterializeResult is what one materializer pass did, for the log line.
type MaterializeResult struct {
	BusinessDate string
	Created      int
	Widened      int
	// RoutinesWithoutAssignee names active routines that would have raised work but whose
	// roles resolve to NOBODY in their park -- the loud gap; nothing was written for them.
	RoutinesWithoutAssignee []RoutineRef
	// PensSkipped counts the tasks those routines would have raised (pens, or 1 for a
	// whole-park routine).
	PensSkipped int
}

// DueDigest is what the notifier pushes: one routine, one due date, the pens still owed.
type DueDigest struct {
	RoutineID   string
	RoutineName string
	ParkID      string
	ParkName    string
	// NotifyTime is the routine's local IST "HH:MM".
	NotifyTime string
	// AssigneeIDs are the role holders resolved for the routine's park at read time.
	AssigneeIDs []string
	DueDate     string
	Tasks       []domain.Task
}

// SweepResult is one roll-forward pass.
type SweepResult struct {
	RolledForward int
	Truncated     bool
}

// ParkListParams selects the web Today table: one park, one due date, optionally one routine.
type ParkListParams struct {
	TenantID     string
	ParkID       string
	BusinessDate string
	RoutineID    string
	Limit        int
	Cursor       string
}

// ParkSummary is the whole-filter count of the Today table by farm bucket.
type ParkSummary struct {
	Due      int `json:"due"`
	Delayed  int `json:"delayed"`
	InReview int `json:"in_review"`
	SentBack int `json:"sent_back"`
	Done     int `json:"done"`
}

// ParkPage is one page of the Today table plus its whole-filter summary.
type ParkPage struct {
	Rows       []domain.Task
	NextCursor string
	Summary    ParkSummary
}

// RoutineListParams selects routines for the web table.
type RoutineListParams struct {
	TenantID string
	// ParkID optional: "" lists every park.
	ParkID string
	// Today is the business date the open_today count is answered for.
	Today string
}

// RoutineListRow is a definition plus the two counts the web table shows.
type RoutineListRow struct {
	Definition domain.Definition
	OpenToday  int
	Delayed    int
}

// Park is one park option.
type Park struct {
	ParkID string
	Name   string
}

// CatalogPen is one active pen of a park, with whether it holds live animals today.
type CatalogPen struct {
	ShedID    string
	ShedName  string
	Partition string
	Label     string
	Occupied  bool
}

// Person is one person who holds a role in a park.
type Person struct {
	UserID      string
	DisplayName string
}

// RoleHolders is one assignable role and who currently holds it for a park.
type RoleHolders struct {
	Role   string
	People []Person
}

// WriteParams carries who is writing and under which idempotency key.
type WriteParams struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
}

// Repository persists routines and their tasks.
type Repository interface {
	// --- routines (authoring) ---
	ListRoutines(ctx context.Context, p RoutineListParams) ([]RoutineListRow, error)
	GetRoutine(ctx context.Context, tenantID, routineID string) (domain.Definition, error)
	// CreateRoutine writes the definition (with its assignee roles), version 1 and the pens in
	// one transaction. The definition must already be normalized and validated.
	CreateRoutine(ctx context.Context, w WriteParams, d domain.Definition) (domain.Definition, error)
	// UpdateRoutine writes a NEW version row, bumps current_version, replaces the pens and the
	// roles, fenced on d.RowVersion. Open tasks keep the version they pinned.
	UpdateRoutine(ctx context.Context, w WriteParams, d domain.Definition) (domain.Definition, error)
	SetRoutineStatus(ctx context.Context, w WriteParams, routineID, status string, rowVersion int) (domain.Definition, error)
	ListParks(ctx context.Context, tenantID string) ([]Park, error)
	// CatalogPens lists every ACTIVE pen of a park from the partition catalog -- the same
	// source the herd-register write pickers use -- with an occupied flag from live animals.
	CatalogPens(ctx context.Context, tenantID, parkID string) ([]CatalogPen, error)
	// RoleHoldersForPark answers, for every assignable role in vocabulary order, who holds it
	// for the park -- the SAME resolution every task read uses (an active grant on an active
	// member; park scope on the park, tenant scope everywhere, except a tenant-scoped
	// park_head who covers only his HRMS home park).
	RoleHoldersForPark(ctx context.Context, tenantID, parkID string) ([]RoleHolders, error)
	// ListRoutineAssignees is the batched preview of who currently holds each routine's roles
	// in its park, keyed by routine id, bounded per routine.
	ListRoutineAssignees(ctx context.Context, tenantID string, routineIDs []string) (map[string][]domain.Assignee, error)

	// --- tasks ---
	ListMine(ctx context.Context, p ListParams) (Page, error)
	GetTask(ctx context.Context, tenantID, taskID string) (domain.Task, error)
	RecordPresence(ctx context.Context, p PresenceParams) (domain.Task, error)
	Submit(ctx context.Context, p SubmitParams) (domain.Task, error)
	// ApplyVerified flips an approved task pending_verification -> completed on both
	// dimensions. Idempotent: a replay, or a verdict on a row no longer pending, applies
	// nothing and says so.
	ApplyVerified(ctx context.Context, p VerdictParams) (VerdictResult, error)
	// BounceForRework flips a rejected task pending_verification -> rework with the
	// verifier's reason; the answers and captures stay as history and the next submit
	// replaces them.
	BounceForRework(ctx context.Context, p VerdictParams) (VerdictResult, error)
	// OpenCount answers the badge for one person: tasks still awaiting work on the routines
	// whose roles they hold for the routine's park.
	OpenCount(ctx context.Context, tenantID, userID string) (int, error)
	// ListForPark is the web Today table: one park, one due date.
	ListForPark(ctx context.Context, p ParkListParams) (ParkPage, error)
	// Materialize writes one task per pen (or ONE for a whole-park routine) per active routine
	// that raises on businessDate, due on the later of the planned date and today. Idempotent
	// on the natural key: a replay inserts nothing. Routines whose roles nobody holds in their
	// park raise nothing and are named.
	Materialize(ctx context.Context, tenantID, businessDate, today string, now time.Time) (MaterializeResult, error)
	// DueDigests reads, per active routine, the open tasks due on the given date, for the
	// day's push. Bounded by that day's open tasks.
	DueDigests(ctx context.Context, tenantID, dueDate string) ([]DueDigest, error)
	// SweepRollForward moves unfinished tasks whose due date has passed to today as delayed.
	SweepRollForward(ctx context.Context, tenantID string, asOf time.Time, chunkSize, maxChunks int) (SweepResult, error)
}

// ProofValidator asserts every capture is a finished, tenant-owned, live-camera proof whose
// declared type matches the kind the submit claims for it.
type ProofValidator interface {
	ValidateLiveCameraProofs(ctx context.Context, tenantID string, proofs []domain.ProofItem) error
}
