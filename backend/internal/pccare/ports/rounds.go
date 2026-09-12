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
	Status        string
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

// ListRoundCardsQuery is the planner list's filter. It mirrors ListTasksQuery's clamp and
// day semantics so the two lists cannot disagree about which work exists.
type ListRoundCardsQuery struct {
	TenantID          string
	AuthorizedParkIDs []string
	TenantWide        bool
	ParkID            string
	Category          string
	// DueBusinessDate pins the list to ONE day. It is OPTIONAL: the planner's list mirrors
	// weighing's, which has no date strip at all — a planner reads the live work, not a day.
	DueBusinessDate string
	CurrentOrCarry  bool
	// Filter is "active" (work still owed: scheduled or delayed) or "completed" (finished or
	// ended). Only consulted when DueBusinessDate is empty; a day-pinned read shows that day
	// whole, exactly as it did before.
	Filter string
	// DateFrom / DateTo (both set or both empty) window the DATELESS read on the card's due
	// date, inclusive (maintainer request 2026-09-10, the phone's filter bar). The active
	// bucket carries still-open work due BEFORE the window -- a delayed round keeps its date
	// and must not vanish behind "today onwards"; the completed bucket is the window exactly.
	DateFrom string
	DateTo   string
	// PenShedID / PenPartitionKey name ONE pen; a card is kept when any of its pens is that
	// pen, and the card still lists every pen it holds. PenShedID empty means every pen.
	PenShedID       string
	PenPartitionKey string
	// Today is the caller's business date, filled by the service: the active carry rule
	// applies only when the window starts on or before today (a future week is asked about
	// alone).
	Today  string
	Cursor string
	Limit  int
}

// RoundCardsWindow is the filter bar's optional narrowing of the planner's list, as the
// service receives it from the wire: a business-date window and one pen.
type RoundCardsWindow struct {
	DateFrom          string
	DateTo            string
	PenShedID         string
	PenPartitionLabel string
}

// RoundCardCounts is the WHOLE-FILTER card tally behind the Pending / Completed pills:
// cards, never pens, over the same window and pen the list applies, status-split so each
// pill is its own bucket. Never derived from a page.
type RoundCardCounts struct {
	Active    int32
	Completed int32
}

// RoundPenOption is one pen the planner's list can be narrowed to: the pen's own identity
// (shed + partition), its backend-composed display, and how many cards it sits on inside
// the window. Status-blind on purpose so a pick survives switching tabs.
type RoundPenOption struct {
	ShedID         string
	PartitionLabel string
	Label          string
	ParkID         string
	ParkName       string
	CardCount      int32
}

// Round card list filters. These are CONTRACT tokens, never user-facing copy.
const (
	RoundCardsFilterActive    = "active"
	RoundCardsFilterCompleted = "completed"
)

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

	// ListRoundCards serves the PLANNER's list at ROUND grain: one card per round, and one
	// card per round-less legacy task. The operator worklist is deliberately untouched — an
	// operator works a pen, so their list stays pen-grained.
	ListRoundCards(ctx context.Context, q ListRoundCardsQuery) (RoundCardPage, error)

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

// RoundCard is ONE row of the planner's round-grained list (maintainer decision 2026-09-05):
// one card per ROUND, not one per pen. A task planned before rounds existed has no round and
// appears as a round of ONE — the list has a single grain either way, so the screen never has
// to merge two shapes.
type RoundCard struct {
	// RoundID is empty for a legacy round-less task; CardKey is what the client keys on.
	RoundID string
	// CardKey is the stable identity of this card: the round id, or the task id when the task
	// belongs to no round. It is also the list's keyset cursor component.
	CardKey string
	// SingleTaskID is set ONLY on a round-less card, so the client can open the pen's task
	// directly instead of drilling into a round of one.
	SingleTaskID string
	Category     string
	ParkID       string
	ParkName     string
	// PlannedBusinessDate is the round's own date; pens that rolled forward keep their own due
	// dates, which is why the card also carries the earliest of them.
	PlannedBusinessDate string
	DueBusinessDate     string
	// Status is the backend-owned roll-up over the card's pens (domain.RoundStatusRollup).
	Status string
	// WorkState is the roll-up over the pens' work states (domain.RoundWorkStateRollup):
	// "closed" when the round was ended, "completed" when its work finished, empty while it is
	// still live. Status alone cannot tell ENDED work from open work, because closing leaves a
	// pen's status at 'open' and moves only its work_state.
	WorkState string
	// PenCount is how many pens this card covers (1 for a legacy task).
	PenCount int32
	// PenLabels are the pens' operational displays in order, for the card's subtitle. The
	// client renders them verbatim and never composes a pen name of its own.
	PenLabels []string
	// AssigneeNames are the crew working this round.
	AssigneeNames []string
	// AnimalCount is the total scanned so far across the card's pens.
	AnimalCount int32
	// RemovalTaskID / RemovalStatus name the round's feed & water removal card when one gates
	// it, so the planner can see the evening job from the round.
	RemovalTaskID string
	RemovalStatus string
	// VisitOwedTaskIDs are the pens whose videos are verified while their clock is still open
	// -- the pens that owe the next-day visit (maintainer decision 2026-09-12). The repository
	// fills it; the service rolls the pens' visits up into PenVisitChip / PenVisitTone, the
	// card's chip once its own videos are done. Empty chip means the card's status stands.
	VisitOwedTaskIDs []string
	PenVisitChip     string
	PenVisitTone     string
}

// RoundCardPage is one bounded keyset page of the round-grained list.
type RoundCardPage struct {
	Cards      []RoundCard
	NextCursor string
	// Counts and Pens are whole-filter companions of the page (empty Pens without a window).
	Counts RoundCardCounts
	Pens   []RoundPenOption
}
