package ports

import (
	"context"
	"errors"
	"strings"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

var (
	ErrForbidden           = errors.New("weighing: forbidden")
	ErrInvalidArgument     = errors.New("weighing: invalid argument")
	ErrNotFound            = errors.New("weighing: not found")
	ErrIdempotencyConflict = errors.New("weighing: idempotency conflict")
	ErrImmutable           = errors.New("weighing: immutable")
	ErrScopeIncomplete     = errors.New("weighing: scope incomplete")
	// ErrDuplicateScan is returned when a scanned_identifier was already
	// captured AND SUBMITTED in an earlier round for the same campaign_shed_id
	// and business day. It is deliberately distinct from ErrIdempotencyConflict:
	// an idempotency conflict means the SAME idempotency key was reused with a
	// DIFFERENT payload (a client replay bug), whereas a duplicate scan is a
	// brand-new request (new idempotency key, no fingerprint to compare) that
	// collides with a different row already committed as submitted. Mapped to
	// 409 Conflict — the resource (this tag, in this bucket, today) already
	// exists as a submitted capture, so the request cannot proceed as issued.
	ErrDuplicateScan = errors.New("weighing: duplicate scan")

	// ErrVerificationPending is the leadership close gate: a bucket may only be
	// closed on the NORMAL path once every submitted video has a verdict. It is
	// deliberately distinct from ErrImmutable ("alreadyterminal") because the
	// bucket is perfectly writable — it is the CLOSER who is early, and the
	// operator and verifier are both still free to work.
	ErrVerificationPending = errors.New("weighing: verification pending")

	// ErrShedAlreadyScheduled is the duplicate-work block: one open weighing row
	// per (park, weigh date, shed). It is distinct from ErrImmutable (the target
	// is in a state that refuses the write) because nothing here is in a wrong
	// state — the requested buckets are simply already somebody's work on that
	// date, and the planner must be told WHICH ones so it can render the reason.
	// Carried to the client as 409 weighing_shed_already_scheduled.
	ErrShedAlreadyScheduled = errors.New("weighing: shed already scheduled")
)

// ShedScheduleConflict names the buckets that blocked a create/update/publish so
// the client can say "Cannot publish - Shed 4, Shed 7 already scheduled" without
// a second round trip. It unwraps to ErrShedAlreadyScheduled, so callers that
// only care about the class keep using errors.Is.
type ShedScheduleConflict struct {
	// WeighDate is the Asia/Kolkata business DATE the conflict is on.
	WeighDate string `json:"weigh_date"`
	// Sheds are the human-readable bucket names, ordered, deduplicated.
	Sheds []string `json:"sheds"`
}

func (c *ShedScheduleConflict) Error() string {
	return "weighing: sheds already scheduled on " + c.WeighDate + ": " + strings.Join(c.Sheds, ", ")
}

func (c *ShedScheduleConflict) Unwrap() error { return ErrShedAlreadyScheduled }

type Repository interface {
	CreateCampaign(ctx context.Context, cmd domain.CreateCampaign) (domain.Campaign, error)
	UpdateCampaign(ctx context.Context, campaignID string, cmd domain.UpdateCampaign) (domain.Campaign, error)
	PublishCampaign(ctx context.Context, tenantID, campaignID, actorID, idempotencyKey string) (domain.Campaign, error)
	// ListCampaigns / ListCampaignsForOperator page the task list. parkID is an
	// optional row filter; the page's whole-filter Counts are computed over the
	// scope and are deliberately NOT narrowed by it (see domain.CampaignCounts).
	ListCampaigns(ctx context.Context, tenantID, parkID string, cursor string, limit int) (domain.CampaignPage, error)
	ListCampaignsForOperator(ctx context.Context, tenantID, operatorUserID, parkID string, cursor string, limit int) (domain.CampaignPage, error)
	// PlannerCatalog reports shed availability for ONE weigh date. excludeCampaignID
	// is the task being edited, whose own buckets must not read back as "taken".
	PlannerCatalog(ctx context.Context, tenantID string, periodStartDate string, excludeCampaignID string) (domain.PlannerCatalog, error)
	ListScopeRoster(ctx context.Context, tenantID, campaignID, campaignShedID string, cursor string, observationsCursor string, limit int, includeRoster bool) (domain.RosterPage, error)
	ListScopeRosterForOperator(ctx context.Context, tenantID, campaignID, campaignShedID, operatorUserID string, cursor string, observationsCursor string, limit int, includeRoster bool) (domain.RosterPage, error)
	GetLeadershipShedVideos(ctx context.Context, tenantID, campaignID, campaignShedID string) (domain.LeadershipShedVideos, error)
	RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error)
	RecordShedObservation(ctx context.Context, cmd domain.RecordShedObservation) (domain.Observation, error)
	SubmitIndividualScope(ctx context.Context, tenantID, campaignID, campaignShedID, actorID, idempotencyKey string, scannedIdentifiers []string) error
	ReopenScope(ctx context.Context, tenantID, campaignID, campaignShedID, actorID, idempotencyKey, reason string) error
	// CloseScope / CloseCampaign are the EXPLICIT terminal actions. Both are
	// allowed while work is still not accepted, and neither may mark unaccepted
	// work accepted. Both follow the ReopenScope transaction shape (fingerprint ->
	// exact-replay readback -> state change -> audit -> idempotency record ->
	// outbox enqueue, all in ONE transaction).
	CloseScope(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error)
	// AbandonScope ends a bucket whose work will never finish. Separate from
	// CloseScope on purpose: it skips the verification gate, demands a reason, and
	// records itself distinguishably so it can never read as a verified close.
	AbandonScope(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error)
	CloseCampaign(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error)
	RefreshAvailability(ctx context.Context, tenantID, campaignID string) error
}

// VerificationVerdictStore is the narrow write side the weighing verdict consumer
// drives. It is deliberately NOT part of Repository: the consumer runs on the
// durable event bus, not behind the HTTP service, so it must not be able to reach
// the planner/execution writes.
type VerificationVerdictStore interface {
	ApplyVerificationVerdict(ctx context.Context, verdict domain.VerificationVerdict) (domain.VerificationVerdictResult, error)
}

// WeighingKernelStore is the PHASE 2 time-driven kernel write/read side. It is
// deliberately NOT part of Repository: the kernel worker runs on a cadence, not
// behind the HTTP service, so it must not be able to reach planner/execution
// writes. Weighing has no separate worker binary — this store is driven by the
// consolidated kernel worker (backend/cmd/kernel-worker) operational cadence.
type WeighingKernelStore interface {
	// SweepWorkItems is one BOUNDED, resumable, forward-progressing tick:
	// terminal reconciliation, roll-forward, delayed/escalation, day-start. Every
	// pass is keyset-chunked with FOR UPDATE SKIP LOCKED and every cadence event is
	// enqueued in the same transaction as the state change.
	SweepWorkItems(ctx context.Context, params domain.KernelSweepParams) (domain.KernelSweepResult, error)
}

// WeighingProcessStateReader is the Calendar + Control Tower binding: dot-grain
// day markers plus a whole-filter, disjoint-bucket summary at
// `weighing_work_item` grain.
type WeighingProcessStateReader interface {
	WeighingProcessState(ctx context.Context, tenantID, campaignID, fromBusinessDate, toBusinessDate string) (domain.ProcessState, error)
}
