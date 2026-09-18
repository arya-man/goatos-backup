package ports

import (
	"context"
	"errors"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	"time"
)

var (
	ErrTransportProofRequired     = errors.New("feeddirection: a live feed-transport video is required")
	ErrTransportTaskNotActionable = errors.New("feeddirection: feed-transport task is not actionable")
	// ErrTransportRejectedProofReuse: the clip (or any capture of the card) was already on an
	// attempt the verifier rejected for this task. A rework needs a NEW video (feed-transport
	// verification rule); re-sending the rejected one is the operator not redoing the work --
	// weighing refuses the same thing by name (edge-case audit 2026-09-18).
	ErrTransportRejectedProofReuse = errors.New("feeddirection: this video was already rejected for this task; record a new one")
	// ErrTransportParkForbidden: the task belongs to a park the caller is not scoped to. This is the
	// CLAMP that lets a park-scoped operator submit at all -- httpmiddleware.routeAllowsScopedGrants
	// admits a park grant on this route only because the park is checked here, against the TASK's own
	// park rather than a park named by the request (this route names none).
	ErrTransportParkForbidden             = errors.New("feeddirection: feed-transport task is outside the actor's park scope")
	ErrTransportAssignedToAnotherOperator = errors.New("feeddirection: feed-transport task is assigned to another operator")
	ErrInvalidTransportStatus             = errors.New("feeddirection: invalid feed-transport status")
)

// FeedTransportTask keeps PartitionLabel for pre-000152 rows only: transport tasks are created per
// physical shed and every row written since then carries an empty partition. Do not reintroduce a
// pen grain here -- packing and distribution own that.
type FeedTransportTask struct {
	TaskID, ParkID, ParkLabel, ShedID, ShedLabel, BusinessDate, Status string
	OperatorID, CurrentAttemptID, ReworkReason                         string
	PartitionLabel, OperationalLocationDisplay                         string
	ScheduledAt                                                        time.Time
	// SOPVersion is the feed.transport SOP version the task was materialized under (FEED SOP,
	// 2026-09-16); 0 = the seeded card. SOP is that card, compiled by the service for the phone.
	SOPVersion int
	SOP        *domain.CardContract
}

// FeedTransportFilterOption.ID is a park UUID or a shed UUID -- never a composite pen key.
type FeedTransportFilterOption struct {
	ID, Label, PartitionLabel string
}

type FeedTransportFilterOptions struct {
	Parks, Sheds []FeedTransportFilterOption
}

type FeedTransportTaskPage struct {
	Items      []FeedTransportTask
	NextCursor string
	Filters    FeedTransportFilterOptions
}

// ListTransportTasksParams has NO partition filter. Transport is one task per physical shed, so
// there is no pen to narrow to; see MaterializeTransportTasks for why the grain is the shed.
type ListTransportTasksParams struct {
	TenantID, ActorID, ParkID, ShedID, Status, Cursor string
	Day                                               time.Time
	Limit                                             int
	AuthorizedParkIDs                                 []string
}

type MaterializeTransportParams struct {
	TenantID string
	AsOf     time.Time
	// SOPVersion pins every task materialized by this call to the feed.transport version in
	// force now (0 = seeded).
	SOPVersion int
}
type MaterializeTransportResult struct {
	BusinessDate string
	Inserted     int64
}

type SubmitTransportParams struct {
	TenantID, TaskID, ProofRef, OperatorID, IdempotencyKey, ActorID, ActorType, TraceID string
	// SOPProofs / SOPAnswers: the task's pinned transport card's judged captures and answers
	// (FEED SOP, 2026-09-16); ProofRef mirrors the seeded slot (or the first capture).
	SOPProofs  authored.ProofRefs
	SOPAnswers authored.Answers
}
type SubmitTransportResult struct {
	AttemptID, Status, ParkID, ShedID string
	ShedName, PartitionLabel          string
	AttemptNo                         int32
	NewlyPending                      bool
	// SOPProofs / SOPAnswers are what the ATTEMPT ROW stores. The submit fingerprint does not cover
	// the card's captures, so an idempotent replay may carry different ones; the verifier item is
	// always built from the row, like distribution's.
	SOPProofs  authored.ProofRefs
	SOPAnswers authored.Answers
}

type ApplyTransportParams struct{ TenantID, AttemptID, VerifiedBy, TraceID string }
type BounceTransportParams struct{ TenantID, AttemptID, Reason, VerifiedBy, TraceID string }

type TransportStore interface {
	MaterializeTransportTasks(context.Context, MaterializeTransportParams) (MaterializeTransportResult, error)
	GetTransportTask(context.Context, string, string) (FeedTransportTask, error)
	ListTransportTasks(context.Context, ListTransportTasksParams) (FeedTransportTaskPage, error)
	SubmitTransportAttempt(context.Context, SubmitTransportParams) (SubmitTransportResult, error)
	ApplyVerifiedTransport(context.Context, ApplyTransportParams) (bool, error)
	BounceTransportForRework(context.Context, BounceTransportParams) (bool, error)
}
