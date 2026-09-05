package ports

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
)

// A ROUND is one planner create covering one or more pens (maintainer decision
// 2026-09-05). The pen bucket is still ports.TaskRow — a round does not replace the task,
// it groups the tasks one create planned — so every existing read, submit and verdict
// path keeps working unchanged.

// CreateRoundParams is the planner's round create. It replaces the per-pen
// CreateTaskParams on the create route; CreateTaskParams itself stays for the kernel's
// own single-task writes.
type CreateRoundParams struct {
	TenantID string
	Category string
	ParkID   string
	// Pens are the pens this round covers, already validated by the domain
	// (non-empty, bounded, no duplicates) before the store sees them.
	Pens []domain.RoundPen
	// PlannedBusinessDate is the immutable date the CEO chose; every pen task starts
	// with due == planned.
	PlannedBusinessDate time.Time
	// AssigneeUserIDs are the operators authorized to work EVERY pen of this round.
	// The wizard asks once, so the same crew works the round; a pen-level operator
	// split would be a different product decision and is deliberately not modelled.
	AssigneeUserIDs []string
	// FeedRemovalRequired (deworming only) creates, in the SAME transaction, ONE
	// round-grain feed_water_removal task dated one day earlier, carrying
	// gates_round_id and one pc_care_removal_pen_proofs row per pen.
	FeedRemovalRequired bool
	// RemovalOperatorUserIDs are the operators for that removal card.
	RemovalOperatorUserIDs []string
	IdempotencyKey         string
	CreatedBy              string
	ActorID                string
	ActorType              string
	TraceID                string
}

// RoundRow is one round as served to planner/monitor/worklist reads and echoed by the
// create.
type RoundRow struct {
	RoundID             string
	Category            string
	ParkID              string
	ParkName            string
	PlannedBusinessDate string
	// Status is the backend-owned roll-up over Pens (domain.RoundStatusRollup). Clients
	// render it verbatim and must never re-derive their own from the pen list — that is
	// how two surfaces come to disagree about whether a round is done.
	Status string
	// PenCount is len(Pens), carried explicitly so a list read can serve the count
	// without shipping every pen row.
	PenCount int32
	// Pens are this round's pen buckets, each a full task row. Empty on list reads.
	Pens []TaskRow
	// RemovalTaskID names the round-grain feed & water removal card when one gates this
	// round; empty when the round needs no removal.
	RemovalTaskID string
	// RemovalStatus mirrors that card's status ("" when there is no removal).
	RemovalStatus string
	CreatedAt     time.Time
}

// RemovalPenProofRow is ONE pen's removal evidence on a round's removal card. The card is
// round-grain (one evening, one crew, one submit) while the evidence is per pen, because
// one clip stretched over four pens proves nothing and the verifier cannot tell which pen
// was actually emptied.
type RemovalPenProofRow struct {
	RemovalPenID string
	// GatedTaskID is the pen's own work task inside the gated round. Naming the TASK
	// rather than a (shed, partition) pair is what makes the removal and the work it
	// gates provably the same pen set.
	GatedTaskID string
	// PenLabel is the backend-composed operational location display ("Godel 1 - Part 3"),
	// rendered verbatim on the operator's slot header and the verifier's item.
	PenLabel      string
	FeedProofRef  string
	WaterProofRef string
	Status        string
	ReworkReason  string
	RowVersion    int32
}

// RegisterRemovalPenProofParams stores ONE pen's feed or water video on a round-grain
// removal card. The pen is named by the WORK TASK it gates, never by a (shed, partition)
// pair, so a client cannot aim a pen's evidence at a pen outside the gated round.
type RegisterRemovalPenProofParams struct {
	TenantID      string
	RemovalTaskID string
	GatedTaskID   string
	// SlotKey is domain.SlotFeedVideo or domain.SlotWaterVideo.
	SlotKey        string
	ProofRef       string
	CapturedBy     string
	IdempotencyKey string
	ActorID        string
	ActorType      string
	TraceID        string
}

// RemovalPenRef is one pen's finished evidence as it travels on the submit event: the
// verifier gets ONE item per pen, so each needs its own id, label and two refs.
type RemovalPenRef struct {
	RemovalPenID  string
	GatedTaskID   string
	PenLabel      string
	FeedProofRef  string
	WaterProofRef string
	RowVersion    int32
}

// ApplyRemovalPenVerdictParams applies one verifier verdict to one pen's evidence row.
// Reason is empty on an approve and mandatory on a rework.
type ApplyRemovalPenVerdictParams struct {
	TenantID     string
	RemovalPenID string
	VerifiedBy   string
	Reason       string
	TraceID      string
}

// CloseRoundParams ends a whole round.
type CloseRoundParams struct {
	TenantID string
	RoundID  string
	Reason   string
	ClosedBy string
	ActorID  string
	TraceID  string
}

// RoundStore is the round half of the PC Care write surface. It is a separate interface
// so a reader of the task store is not handed round writes it has no business making;
// the Postgres adapter implements both.
type RoundStore interface {
	// CreateRound plans a round and its pen tasks in ONE transaction, plus — when the
	// round asks for it — the round-grain removal card and its per-pen evidence rows.
	// Either the whole round lands or none of it does: a deworming shipped without the
	// gate it was planned with would run on unfasted animals.
	CreateRound(ctx context.Context, p CreateRoundParams) (RoundRow, error)

	// CloseRound ends a whole round: every pen that is not already completed or closed goes
	// to closed, and so does the round's feed & water removal card. A COMPLETED pen keeps
	// that status — completed is accepted work and a close must never rewrite it. REFUSED
	// (domain.ErrVerificationPending) when ANY pen of the round holds evidence awaiting a
	// verdict; the question is "does this round hold unverified work", not "which rows would
	// this UPDATE touch".
	CloseRound(ctx context.Context, p CloseRoundParams) error

	// GetRound reads one round with its pen buckets, clamped to the authorized parks.
	GetRound(ctx context.Context, tenantID, roundID string, authorizedParkIDs []string, tenantWide bool) (RoundRow, error)

	// ListRemovalPenProofs reads the per-pen evidence rows of a removal card, in pen
	// label order.
	ListRemovalPenProofs(ctx context.Context, tenantID, removalTaskID string) ([]RemovalPenProofRow, error)

	// RegisterRemovalPenProof stores one pen's feed or water video on a removal card.
	RegisterRemovalPenProof(ctx context.Context, p RegisterRemovalPenProofParams) error

	// ApplyVerifiedRemovalPen flips ONE pen's evidence row to completed and rolls the parent
	// card up (completed only when every pen is). Stale/duplicate verdicts return false with
	// no side effects.
	ApplyVerifiedRemovalPen(ctx context.Context, p ApplyRemovalPenVerdictParams) (bool, error)

	// BounceRemovalPenForRework flips ONE pen's evidence row to rework with the verifier's
	// reason and puts the parent card back in rework so the crew can re-shoot that pen.
	BounceRemovalPenForRework(ctx context.Context, p ApplyRemovalPenVerdictParams) (bool, error)
}
