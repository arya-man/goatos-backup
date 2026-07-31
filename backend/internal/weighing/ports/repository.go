package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

var (
	ErrForbidden           = errors.New("weighing: forbidden")
	ErrInvalidArgument     = errors.New("weighing: invalid argument")
	ErrNotFound            = errors.New("weighing: not found")
	ErrIdempotencyConflict = errors.New("weighing: idempotency conflict")
	ErrImmutable           = errors.New("weighing: immutable")
	ErrScopeIncomplete     = errors.New("weighing: scope incomplete")
	// ErrAnimalUnavailable is the critical-animal-action gate: the scanned
	// identifier resolved to a REAL animal that is clinically held (sick, under
	// treatment, recovering, quarantine, ICU) or has already left the herd. It is
	// deliberately distinct from ErrNotFound (the animal exists) and from
	// ErrInvalidArgument (the caller did nothing wrong) — it reports a state, not
	// a mistake. Free-flow is unaffected: an identifier that resolves to NO animal
	// never reaches this error.
	ErrAnimalUnavailable = errors.New("weighing: animal unavailable")
)

type Repository interface {
	CreateCampaign(ctx context.Context, cmd domain.CreateCampaign) (domain.Campaign, error)
	UpdateCampaign(ctx context.Context, campaignID string, cmd domain.UpdateCampaign) (domain.Campaign, error)
	PublishCampaign(ctx context.Context, tenantID, campaignID, actorID, idempotencyKey string) (domain.Campaign, error)
	ListCampaigns(ctx context.Context, tenantID string, cursor string, limit int) (domain.CampaignPage, error)
	ListCampaignsForOperator(ctx context.Context, tenantID, operatorUserID string, cursor string, limit int) (domain.CampaignPage, error)
	PlannerCatalog(ctx context.Context, tenantID string, periodStartDate string) (domain.PlannerCatalog, error)
	ListScopeRoster(ctx context.Context, tenantID, campaignID, campaignShedID string, cursor string, observationsCursor string, limit int) (domain.RosterPage, error)
	ListScopeRosterForOperator(ctx context.Context, tenantID, campaignID, campaignShedID, operatorUserID string, cursor string, observationsCursor string, limit int) (domain.RosterPage, error)
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
