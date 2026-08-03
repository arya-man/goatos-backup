package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// ErrStaleEvidence is the canonical ports error, re-exported so the existing
// adapter-level callers and tests keep one name for it. The definition moved to
// ports because the event consumer in weighing/app must classify it too, and app
// must not import an adapter.
var ErrStaleEvidence = ports.ErrStaleEvidence

// Weighing side of the generic verification verdict.
//
// Weighing has always ENQUEUED a verification item for every observation
// (weighing/adapters/verificationbridge) but had no consumer for the verdict, so
// every approve/rework a verifier sent was a silent drop: the observation kept
// reading as unverified forever and a rejected observation never came back to the
// operator. This is the missing consumer's write side.
//
// Idempotency is keyed on the VERIFICATION EVENT ID, not on a client key: the
// durable bus is at-least-once, so the same verdict event can be delivered many
// times and must apply exactly once. An exact replay returns the original result
// and performs no new side effects (no status rewrite, no second outbox row, no
// second audit row).
const (
	eventTypeObservationVerified = "weighing.observation.verified"
	eventTypeObservationRework   = "weighing.observation.rework"

	idempotencyEventVerdictApplied = "weighing.verification_verdict_applied"
)

// ApplyVerificationVerdict marks the observation verified or bounces it back for
// rework. On rework the owning bucket becomes operator-actionable again.
func (r *Repository) ApplyVerificationVerdict(ctx context.Context, verdict domain.VerificationVerdict) (domain.VerificationVerdictResult, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.VerificationVerdictResult{}, err
	}
	defer tx.Rollback(ctx)

	fingerprint := idempotencyFingerprint(map[string]any{
		"observation_id": verdict.ObservationID,
		"ref_type":       verdict.RefType,
		"status":         verdict.Status,
		"verified_by":    verdict.VerifiedBy,
		"reason":         verdict.Reason,
	})
	if result, ok, err := r.verdictByIdempotency(ctx, tx, verdict, fingerprint); err != nil || ok {
		if err != nil {
			return domain.VerificationVerdictResult{}, err
		}
		return result, tx.Commit(ctx)
	}

	scope, err := r.lockObservationScope(ctx, tx, verdict)
	if err != nil {
		return domain.VerificationVerdictResult{}, err
	}
	if err := checkVerdictEvidenceCurrent(verdict, scope); err != nil {
		return domain.VerificationVerdictResult{}, err
	}

	var decidedAt time.Time
	switch verdict.Status {
	case domain.VerificationStatusVerified:
		decidedAt, err = r.markObservationVerified(ctx, tx, verdict)
		if err != nil {
			return domain.VerificationVerdictResult{}, err
		}
	case domain.VerificationStatusRework:
		decidedAt, err = r.markObservationRework(ctx, tx, verdict, scope)
		if err != nil {
			return domain.VerificationVerdictResult{}, err
		}
	default:
		return domain.VerificationVerdictResult{}, ports.ErrInvalidArgument
	}

	result := domain.VerificationVerdictResult{
		Applied:        true,
		CampaignID:     scope.CampaignID,
		CampaignShedID: scope.CampaignShedID,
		ObservationID:  verdict.ObservationID,
		OperatorID:     scope.OperatorID,
		Status:         verdict.Status,
		// India business time: every Goat OS business meaning derives from
		// Asia/Kolkata, never UTC (AGENTS.md). Recorded into the idempotency
		// snapshot below, so a redelivery replays this instant verbatim.
		DecidedAt: decidedAt.In(biztime.DefaultLocation()),
	}
	if err := r.auditVerdict(ctx, tx, verdict, scope, result); err != nil {
		return domain.VerificationVerdictResult{}, err
	}
	if err := r.recordIdempotency(
		ctx, tx, verdict.TenantID, idempotencyEventVerdictApplied, verdict.EventID, fingerprint,
		verdict.RefType, verdict.ObservationID, result,
	); err != nil {
		return domain.VerificationVerdictResult{}, err
	}
	if err := r.enqueueVerdictApplied(ctx, tx, verdict, scope, result); err != nil {
		return domain.VerificationVerdictResult{}, err
	}
	return result, tx.Commit(ctx)
}

func (r *Repository) verdictByIdempotency(
	ctx context.Context,
	tx pgx.Tx,
	verdict domain.VerificationVerdict,
	fingerprint string,
) (domain.VerificationVerdictResult, bool, error) {
	_, resourceType, snapshot, ok, err := r.idempotencyResource(
		ctx, tx, verdict.TenantID, idempotencyEventVerdictApplied, verdict.EventID, fingerprint,
	)
	if err != nil || !ok {
		return domain.VerificationVerdictResult{}, ok, err
	}
	if resourceType != verdict.RefType {
		return domain.VerificationVerdictResult{}, true, ports.ErrIdempotencyConflict
	}
	var result domain.VerificationVerdictResult
	if len(snapshot) > 0 {
		if err := json.Unmarshal(snapshot, &result); err != nil {
			return domain.VerificationVerdictResult{}, true, err
		}
	}
	return result, true, nil
}

// observationScope is the routing context of the verified observation: which
// campaign/bucket it belongs to, which shed it was captured in, and the single
// operator who owns that bucket.
type observationScope struct {
	CampaignID     string
	CampaignShedID string
	ShedID         string
	ShedLabel      string
	ParkID         string
	OperatorID     string
	// ProofArtifactID is the proof/video id CURRENTLY attached to the
	// observation, read under the same row lock as the rest of the scope
	// (FOR UPDATE OF observation), so it cannot change out from under the
	// comparison in checkVerdictEvidenceCurrent below.
	ProofArtifactID string
}

func (r *Repository) lockObservationScope(ctx context.Context, tx pgx.Tx, verdict domain.VerificationVerdict) (observationScope, error) {
	var scope observationScope
	var query string
	switch verdict.RefType {
	case domain.VerificationRefTypeAnimal:
		query = `
SELECT observation.campaign_id::text,
  COALESCE(observation.campaign_shed_id::text, ''),
  COALESCE(cs.location_id::text, ''),
  COALESCE(cs.display_name, ''),
  wc.park_id::text,
  COALESCE(cs.operator_user_id::text, wc.operator_user_id::text),
  COALESCE(observation.proof_artifact_id::text, '')
FROM weighing_observations observation
JOIN weighing_campaigns wc
  ON wc.tenant_id=observation.tenant_id
 AND wc.campaign_id=observation.campaign_id
LEFT JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=observation.tenant_id
 AND cs.campaign_shed_id=observation.campaign_shed_id
WHERE observation.tenant_id=$1::uuid
  AND observation.observation_id=$2::uuid
FOR UPDATE OF observation`
	case domain.VerificationRefTypeShed:
		query = `
SELECT observation.campaign_id::text,
  observation.campaign_shed_id::text,
  cs.location_id::text,
  cs.display_name,
  wc.park_id::text,
  COALESCE(cs.operator_user_id::text, wc.operator_user_id::text),
  COALESCE(observation.proof_artifact_id::text, '')
FROM weighing_shed_observations observation
JOIN weighing_campaigns wc
  ON wc.tenant_id=observation.tenant_id
 AND wc.campaign_id=observation.campaign_id
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=observation.tenant_id
 AND cs.campaign_shed_id=observation.campaign_shed_id
WHERE observation.tenant_id=$1::uuid
  AND observation.shed_observation_id=$2::uuid
FOR UPDATE OF observation`
	default:
		return observationScope{}, ports.ErrInvalidArgument
	}
	if err := tx.QueryRow(ctx, query, verdict.TenantID, verdict.ObservationID).Scan(
		&scope.CampaignID,
		&scope.CampaignShedID,
		&scope.ShedID,
		&scope.ShedLabel,
		&scope.ParkID,
		&scope.OperatorID,
		&scope.ProofArtifactID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return observationScope{}, ports.ErrNotFound
		}
		return observationScope{}, err
	}
	return scope, nil
}

// checkVerdictEvidenceCurrent guards against approving/reworking evidence
// that is no longer the evidence attached to the observation. A verdict
// minted before EvidenceProofID existed (durable-bus in-flight events at
// deploy time) carries an empty EvidenceProofID; that is NOT treated as a
// mismatch -- crashing or rejecting a legitimate in-flight verdict over a
// field it predates would be worse than the gap it closes. It is logged so
// the skip stays visible without breaking delivery.
func checkVerdictEvidenceCurrent(verdict domain.VerificationVerdict, scope observationScope) error {
	if verdict.EvidenceProofID == "" {
		slog.Default().Warn("weighing: verification verdict has no evidence id, skipping stale-evidence check",
			"observation_id", verdict.ObservationID,
			"ref_type", verdict.RefType,
			"event_id", verdict.EventID,
		)
		return nil
	}
	if scope.ProofArtifactID == "" || verdict.EvidenceProofID != scope.ProofArtifactID {
		return ErrStaleEvidence
	}
	return nil
}

// markObservationVerified RETURNS the persisted verified_at rather than letting
// the caller read a second wall clock. The payload/audit decision time and the
// stored row must be the same instant; two independent clock reads can disagree,
// and on an at-least-once redelivery a fresh read would invent a decision time
// that never happened.
func (r *Repository) markObservationVerified(ctx context.Context, tx pgx.Tx, verdict domain.VerificationVerdict) (time.Time, error) {
	table, idColumn := verdictTable(verdict.RefType)
	var decidedAt time.Time
	err := tx.QueryRow(ctx, `
UPDATE `+table+`
SET verification_status='verified',
  verified_by=$3::uuid,
  verified_at=now(),
  rework_reason=NULL
WHERE tenant_id=$1::uuid
  AND `+idColumn+`=$2::uuid
RETURNING verified_at`, verdict.TenantID, verdict.ObservationID, nullUUID(verdict.VerifiedBy)).Scan(&decidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ports.ErrNotFound
	}
	return decidedAt, err
}

func (r *Repository) markObservationRework(
	ctx context.Context,
	tx pgx.Tx,
	verdict domain.VerificationVerdict,
	scope observationScope,
) (time.Time, error) {
	table, idColumn := verdictTable(verdict.RefType)
	var decidedAt time.Time
	if err := tx.QueryRow(ctx, `
UPDATE `+table+`
SET verification_status='rework',
  verified_by=$3::uuid,
  verified_at=now(),
  rework_reason=NULLIF($4, '')
WHERE tenant_id=$1::uuid
  AND `+idColumn+`=$2::uuid
RETURNING verified_at`, verdict.TenantID, verdict.ObservationID, nullUUID(verdict.VerifiedBy), verdict.Reason).Scan(&decidedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, ports.ErrNotFound
		}
		return time.Time{}, err
	}

	// Free the slot the rejected proof is holding, exactly as ReopenScope does.
	//
	// A rework verdict is the COMMON way a lump-sum shed proof comes back for a re-shoot -- far
	// more common than a leadership reopen. Without this the rejected row keeps
	// weighing_shed_observations_one_open_scope_uidx and its idempotency record, so the
	// operator's resubmit trips 23505 and is mapped to a permanent 409: the operator is told to
	// redo the work and then structurally prevented from filing it. Withdrawing preserves the
	// rejected attempt as history (AGENTS.md requires rejected proof attempts stay immutable)
	// while letting the next attempt take the open slot.
	//
	// Shed grain only: weighing_observations is per-animal and carries no open-scope index, so
	// there is no slot to free there.
	if verdict.RefType == domain.VerificationRefTypeShed {
		if _, err := tx.Exec(ctx, `
UPDATE weighing_shed_observations
SET withdrawn_at=now()
WHERE tenant_id=$1::uuid
  AND shed_observation_id=$2::uuid
  AND withdrawn_at IS NULL`, verdict.TenantID, verdict.ObservationID); err != nil {
			return time.Time{}, err
		}
		// Same transaction: a surviving idempotency record would make the operator's replay
		// return the withdrawn observation and report success over work that was never filed.
		if _, err := tx.Exec(ctx, `
DELETE FROM weighing_idempotency_records
WHERE tenant_id=$1::uuid
  AND event_type='weighing.shed_observation_accepted'
  AND resource_id=$2::uuid`, verdict.TenantID, verdict.ObservationID); err != nil {
			return time.Time{}, err
		}
	}

	// Make the owning bucket operator-actionable again. This mirrors ReopenScope:
	// the bucket leaves its terminal state so the operator's app shows it as work.
	// A CLOSED bucket is a deliberate leadership decision and is NOT reopened by a
	// verifier verdict; only an auto-completed bucket comes back.
	if scope.CampaignShedID != "" {
		if _, err := tx.Exec(ctx, `
UPDATE weighing_campaign_sheds
SET status='in_progress', completed_at=NULL, updated_at=now()
WHERE tenant_id=$1::uuid
  AND campaign_shed_id=$2::uuid
  AND status='completed'`, verdict.TenantID, scope.CampaignShedID); err != nil {
			return time.Time{}, err
		}
		// B09: the bucket just left its terminal status, so the kernel work item the
		// sweeper terminalized for it must be reactivated in the SAME transaction --
		// reconcileTerminalWorkItems only drives work items TOWARD terminal, nothing
		// moves one back on its own, so without this call the work item stays
		// terminal forever and Calendar/Control Tower keep reporting the bucket as
		// finished even though the operator has real rework to do again.
		if _, err := r.ReactivateWorkItemsForBucket(ctx, tx, verdict.TenantID, scope.CampaignShedID); err != nil {
			return time.Time{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE weighing_campaigns
SET status='in_progress', completed_at=NULL, updated_at=now(), row_version=row_version+1
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status='completed'`, verdict.TenantID, scope.CampaignID); err != nil {
		return time.Time{}, err
	}
	// Free-flow: an individual scan observation never carries an animal_id (the
	// column does not exist -- 000078_weighing_observations_drop_animal_id.sql)
	// and so never has a weighing_expected_animals roster row to return to
	// 'pending' on rework. The roster is a planner/catalog label, populated and
	// read independently of the scan write path; it is never validated against
	// herd/vaccination tables and a rejected scan has nothing there to restore.
	return decidedAt, nil
}

// verdictTable maps the generic verification ref_type onto the weighing table
// that owns it. Callers pass only the two weighing ref types (the consumer filters
// on them), so the default is unreachable-by-contract and stays on the animal
// grain rather than interpolating attacker-controlled text.
func verdictTable(refType string) (string, string) {
	if refType == domain.VerificationRefTypeShed {
		return "weighing_shed_observations", "shed_observation_id"
	}
	return "weighing_observations", "observation_id"
}

func (r *Repository) auditVerdict(
	ctx context.Context,
	tx pgx.Tx,
	verdict domain.VerificationVerdict,
	scope observationScope,
	result domain.VerificationVerdictResult,
) error {
	action := "weighing.observation_verified"
	if verdict.Status == domain.VerificationStatusRework {
		action = "weighing.observation_rework"
	}
	return audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     verdict.TenantID,
		ActorID:      verdict.VerifiedBy,
		ActorType:    "user",
		Action:       action,
		ResourceType: "weighing_observation",
		ResourceID:   verdict.ObservationID,
		ScopeType:    "weighing.campaign_shed",
		ScopeID:      scope.CampaignShedID,
		AfterState:   result,
		Metadata: map[string]any{
			"campaign_id":            scope.CampaignID,
			"campaign_shed_id":       scope.CampaignShedID,
			"ref_type":               verdict.RefType,
			"verification_status":    verdict.Status,
			"reason":                 verdict.Reason,
			"verification_event_id":  verdict.EventID,
			"client_idempotency_key": verdict.EventID,
		},
	})
}

type weighingObservationVerdictPayload struct {
	TenantID           string    `json:"tenant_id"`
	CampaignID         string    `json:"campaign_id"`
	CampaignShedID     string    `json:"campaign_shed_id"`
	ObservationID      string    `json:"observation_id"`
	RefType            string    `json:"ref_type"`
	ParkID             string    `json:"park_id"`
	ShedID             string    `json:"shed_id"`
	ShedLabel          string    `json:"shed_label"`
	OperatorID         string    `json:"operator_id"`
	VerifiedBy         string    `json:"verified_by,omitempty"`
	Reason             string    `json:"reason,omitempty"`
	VerificationStatus string    `json:"verification_status"`
	OperatorActionable bool      `json:"operator_actionable"`
	DecidedAt          time.Time `json:"decided_at"`
}

func (r *Repository) enqueueVerdictApplied(
	ctx context.Context,
	tx pgx.Tx,
	verdict domain.VerificationVerdict,
	scope observationScope,
	result domain.VerificationVerdictResult,
) error {
	eventType := eventTypeObservationVerified
	if verdict.Status == domain.VerificationStatusRework {
		eventType = eventTypeObservationRework
	}
	payload := weighingObservationVerdictPayload{
		TenantID:           verdict.TenantID,
		CampaignID:         scope.CampaignID,
		CampaignShedID:     scope.CampaignShedID,
		ObservationID:      verdict.ObservationID,
		RefType:            verdict.RefType,
		ParkID:             scope.ParkID,
		ShedID:             scope.ShedID,
		ShedLabel:          scope.ShedLabel,
		OperatorID:         scope.OperatorID,
		VerifiedBy:         verdict.VerifiedBy,
		Reason:             verdict.Reason,
		VerificationStatus: result.Status,
		// A rework verdict is the operator's next action; an approval is not.
		OperatorActionable: verdict.Status == domain.VerificationStatusRework,
		DecidedAt:          result.DecidedAt,
	}
	return r.enqueue(
		ctx,
		tx,
		verdict.TenantID,
		eventType,
		verdict.ObservationID,
		eventType+":"+verdict.EventID,
		"",
		payload,
	)
}
