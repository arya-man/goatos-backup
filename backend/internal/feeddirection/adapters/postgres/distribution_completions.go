package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// Feed DISTRIBUTION verification gate write path (maintainer decision, 2026-07-26). This file owns the
// module's SECOND write path -- the NEW feed_distribution_completions table -- entirely separate from
// completions.go (feed PACKING, the untouched feed_direction_session_completions path). A distribution
// completion is a two-phase, verifier-gated fact:
//
//	CompleteDistribution        -> writes 'pending_verification' with both proofs (NOTHING completed)
//	ApplyVerifiedDistribution   -> verifier approved: 'pending_verification' -> 'completed'; emits
//	                               feed.distribution.completed (the ONLY producer of that event)
//	BounceDistributionForRework -> verifier rejected: 'pending_verification' -> 'rework'
//
// It is registered in context/architecture/domain-event-registry.json; the literal event type below
// plus the outbox_messages INSERT are what the domain-event-architecture guard matches against.
const (
	feedDistributionCompletedEventType     = "feed.distribution.completed"
	feedDistributionCompletedSchemaVersion = "1.0.0"
	feedDistributionCompletedSchemaRef     = "domain-event-envelope.v1"
	feedDistributionCompletedTopic         = "feed.events"
	feedDistributionCompletedAggregateType = "feed_distribution_completion"
	feedDistributionIdemScope              = "feed.distribution.complete"
	feedDistributionResourceType           = "feed_distribution_completion"

	feedDistributionPendingAction   = "feed.distribution.pending_verification"
	feedDistributionReworkAction    = "feed.distribution.rework"
	feedDistributionCompletedAction = "feed.distribution.completed"
)

var _ ports.DistributionCompletionStore = (*Repository)(nil)

// CompleteDistribution records the operator's two mandatory proofs at 'pending_verification' (or moves
// a 'rework' row back to it on a re-submit). Canonical write + audit + idempotency reservation are one
// transaction; idempotent on both the request key and the shed-session natural key. It does NOT emit
// feed.distribution.completed -- that fires only when a verifier approves (ApplyVerifiedDistribution).
func (r *Repository) CompleteDistribution(ctx context.Context, p ports.CompleteDistributionParams) (ports.CompleteDistributionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	distProof := strings.TrimSpace(p.DistributionProofRef)
	waterProof := strings.TrimSpace(p.WaterProofRef)
	targetDate := p.TargetDate.Format("2006-01-02")

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.CompleteDistributionResult{}, fmt.Errorf("feeddirection: begin distribution completion tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// Shed must be an active shed of the addressed park. Fail closed rather than record a completion
	// against a shed that does not belong to the park being fed.
	if err := requireShedInPark(ctx, tx, p.TenantID, p.ParkID, p.ShedID); err != nil {
		return ports.CompleteDistributionResult{}, err
	}

	fingerprint := requestFingerprint(
		p.ParkID,
		p.ShedID,
		fmt.Sprintf("%d", p.SessionNo),
		targetDate,
		p.Workflow,
		distProof,
		waterProof,
	)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, feedDistributionIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.CompleteDistributionResult{}, err
	}
	if !reservation.proceed {
		// Exact replay of the same request: return the original result, run NO side effects. Read the
		// row so the caller still sees its current status/row_version, but NewlyPending stays false so no
		// verification item is re-enqueued.
		status, rowVersion, readErr := r.readDistributionByID(ctx, tx, p.TenantID, reservation.resultID)
		if readErr != nil {
			return ports.CompleteDistributionResult{}, readErr
		}
		if err := tx.Commit(ctx); err != nil {
			return ports.CompleteDistributionResult{}, fmt.Errorf("feeddirection: commit idempotent distribution replay: %w", err)
		}
		committed = true
		return ports.CompleteDistributionResult{CompletionID: reservation.resultID, Status: status, RowVersion: rowVersion, NewlyPending: false}, nil
	}

	var (
		completionID string
		rowVersion   int32
		status       = domain.DistributionStatusPendingVerification
		newlyPending bool
	)
	err = tx.QueryRow(ctx, `
INSERT INTO feed_distribution_completions (
  tenant_id, park_id, shed_id, session_no, target_date, workflow, status,
  distribution_proof_ref, water_proof_ref, completed_by, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4, $5::date, $6, 'pending_verification',
  $7, $8, nullif($9::text, '')::uuid, $10
)
ON CONFLICT (tenant_id, park_id, shed_id, session_no, target_date, workflow) DO NOTHING
RETURNING completion_id::text, row_version`,
		p.TenantID, p.ParkID, p.ShedID, p.SessionNo, targetDate, p.Workflow,
		distProof, waterProof, p.CompletedBy, p.IdempotencyKey).Scan(&completionID, &rowVersion)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Natural-key conflict: a row for this shed-session already exists. Its state decides the outcome.
		var existingStatus string
		if err := tx.QueryRow(ctx, `
SELECT completion_id::text, status, row_version
FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
  AND session_no = $4 AND target_date = $5::date AND workflow = $6`,
			p.TenantID, p.ParkID, p.ShedID, p.SessionNo, targetDate, p.Workflow).
			Scan(&completionID, &existingStatus, &rowVersion); err != nil {
			return ports.CompleteDistributionResult{}, fmt.Errorf("feeddirection: read existing distribution completion: %w", err)
		}
		switch existingStatus {
		case domain.DistributionStatusRework:
			// A verifier rejected the prior video; the operator re-recorded. Move the row back to
			// pending_verification with the NEW proofs, bump row_version, clear the rework reason. This is
			// a fresh pending transition, so it enqueues a fresh verification item.
			if err := tx.QueryRow(ctx, `
UPDATE feed_distribution_completions
SET status = 'pending_verification',
    distribution_proof_ref = $3,
    water_proof_ref = $4,
    rework_reason = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'rework'
RETURNING row_version`,
				p.TenantID, completionID, distProof, waterProof).Scan(&rowVersion); err != nil {
				return ports.CompleteDistributionResult{}, fmt.Errorf("feeddirection: resubmit distribution for verification: %w", err)
			}
			status = domain.DistributionStatusPendingVerification
			newlyPending = true
			if err := writeDistributionAudit(ctx, tx, p, completionID, feedDistributionPendingAction); err != nil {
				return ports.CompleteDistributionResult{}, err
			}
		case domain.DistributionStatusPendingVerification:
			// Already awaiting verification: idempotent no-op, no new verification item.
			status = domain.DistributionStatusPendingVerification
		case domain.DistributionStatusCompleted:
			// Already verified/completed: no-op.
			status = domain.DistributionStatusCompleted
		default:
			return ports.CompleteDistributionResult{}, fmt.Errorf("feeddirection: unexpected distribution status %q", existingStatus)
		}
	case err != nil:
		return ports.CompleteDistributionResult{}, fmt.Errorf("feeddirection: insert distribution completion: %w", err)
	default:
		// Fresh insert: a brand-new pending_verification row.
		newlyPending = true
		if err := writeDistributionAudit(ctx, tx, p, completionID, feedDistributionPendingAction); err != nil {
			return ports.CompleteDistributionResult{}, err
		}
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, feedDistributionIdemScope, p.IdempotencyKey, feedDistributionResourceType, completionID); err != nil {
		return ports.CompleteDistributionResult{}, fmt.Errorf("feeddirection: complete distribution idempotency: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ports.CompleteDistributionResult{}, fmt.Errorf("feeddirection: commit distribution completion: %w", err)
	}
	committed = true
	return ports.CompleteDistributionResult{CompletionID: completionID, Status: status, RowVersion: rowVersion, NewlyPending: newlyPending}, nil
}

// readDistributionByID reads a row's status and row_version within the transaction, for the idempotent
// replay echo.
func (r *Repository) readDistributionByID(ctx context.Context, tx pgx.Tx, tenantID, completionID string) (string, int32, error) {
	if strings.TrimSpace(completionID) == "" {
		return "", 0, nil
	}
	var status string
	var rowVersion int32
	err := tx.QueryRow(ctx, `
SELECT status, row_version
FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, tenantID, completionID).Scan(&status, &rowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("feeddirection: read distribution completion by id: %w", err)
	}
	return status, rowVersion, nil
}

// ListVerifiedDistributions returns every VERIFIED (status='completed') (shed, session, workflow) for
// one park-day in one bounded indexed read -- the direction serving-read overlay.
func (r *Repository) ListVerifiedDistributions(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]ports.VerifiedDistribution, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded read of ONE park-day's VERIFIED distribution shed-sessions, covered by feed_distribution_completions_serving_idx (tenant_id, park_id, target_date, workflow). Bounded by the park's shed catalog x sessions (physical infrastructure), never by herd size; binds are cast, indexed columns stay bare.
	rows, err := r.pool.Query(ctx, `
SELECT shed_id::text, session_no, workflow
FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND target_date = $3::date AND status = 'completed'`,
		tenantID, parkID, targetDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("feeddirection: list verified distributions: %w", err)
	}
	defer rows.Close()
	out := make([]ports.VerifiedDistribution, 0)
	for rows.Next() {
		var d ports.VerifiedDistribution
		if err := rows.Scan(&d.ShedID, &d.SessionNo, &d.Workflow); err != nil {
			return nil, fmt.Errorf("feeddirection: scan verified distribution: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ApplyVerifiedDistribution flips a distribution completion whose video a verifier APPROVED
// 'pending_verification' -> 'completed' and emits feed.distribution.completed, in one transaction. It
// runs from the verification.verdict.approved consumer, never from the operator's phone.
//
// IDEMPOTENT: a re-delivered verdict on an already-'completed' row returns false with no side effects.
// A verdict for a row no longer 'pending_verification' (bounced to rework, or otherwise moved on) is
// ignored as stale (returns false).
func (r *Repository) ApplyVerifiedDistribution(ctx context.Context, p ports.ApplyDistributionParams) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("feeddirection: begin apply distribution tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var (
		status     string
		parkID     string
		shedID     string
		workflow   string
		sessionNo  int32
		targetDate time.Time
	)
	err = tx.QueryRow(ctx, `
SELECT status, park_id::text, shed_id::text, workflow, session_no, target_date
FROM feed_distribution_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid
FOR UPDATE`, p.TenantID, p.CompletionID).Scan(&status, &parkID, &shedID, &workflow, &sessionNo, &targetDate)
	if errors.Is(err, pgx.ErrNoRows) {
		// No such row for this tenant: a stale/foreign verdict. Ignore.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("feeddirection: lock distribution completion: %w", err)
	}
	// Already completed (re-delivered verdict) or no longer pending (stale delivery): no side effects.
	if status != domain.DistributionStatusPendingVerification {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}

	tag, err := tx.Exec(ctx, `
UPDATE feed_distribution_completions
SET status = 'completed',
    verified_by = nullif($3::text, '')::uuid,
    verified_at = now(),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'pending_verification'`,
		p.TenantID, p.CompletionID, p.VerifiedBy)
	if err != nil {
		return false, fmt.Errorf("feeddirection: apply verified distribution: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Lost the race to a concurrent transition; treat as stale.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}

	if err := insertFeedDistributionCompletedOutbox(ctx, tx, feedDistributionCompletedOutbox{
		TenantID:     p.TenantID,
		CompletionID: p.CompletionID,
		ParkID:       parkID,
		ShedID:       shedID,
		SessionNo:    sessionNo,
		TargetDate:   targetDate.Format("2006-01-02"),
		Workflow:     workflow,
		VerifiedBy:   strings.TrimSpace(p.VerifiedBy),
		TraceID:      p.TraceID,
	}); err != nil {
		return false, err
	}
	if err := writeDistributionVerdictAudit(ctx, tx, p.TenantID, shedID, parkID, p.CompletionID, feedDistributionCompletedAction, p.VerifiedBy, p.TraceID); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("feeddirection: commit apply verified distribution: %w", err)
	}
	committed = true
	return true, nil
}

// BounceDistributionForRework flips a distribution completion whose video a verifier REJECTED
// 'pending_verification' -> 'rework'. It runs from the verification.verdict.rework consumer. NOTHING is
// completed. Idempotent + stale-guarded: only a 'pending_verification' row is bounced.
func (r *Repository) BounceDistributionForRework(ctx context.Context, p ports.BounceDistributionParams) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tag, err := r.pool.Exec(ctx, `
UPDATE feed_distribution_completions
SET status = 'rework',
    rework_reason = nullif($3, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'pending_verification'`,
		p.TenantID, p.CompletionID, strings.TrimSpace(p.Reason))
	if err != nil {
		return false, fmt.Errorf("feeddirection: bounce distribution for rework: %w", err)
	}
	// Zero rows affected is an accepted stale/duplicate verdict; no error.
	return tag.RowsAffected() > 0, nil
}

func writeDistributionAudit(ctx context.Context, tx pgx.Tx, p ports.CompleteDistributionParams, completionID, action string) error {
	actorType := strings.TrimSpace(p.ActorType)
	if actorType == "" {
		actorType = "operator"
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       action,
		ResourceType: feedDistributionResourceType,
		ResourceID:   completionID,
		ScopeType:    "shed",
		ScopeID:      p.ShedID,
		AfterState: map[string]any{
			"park_id":     p.ParkID,
			"shed_id":     p.ShedID,
			"session_no":  p.SessionNo,
			"target_date": p.TargetDate.Format("2006-01-02"),
			"workflow":    p.Workflow,
			"status":      domain.DistributionStatusPendingVerification,
		},
		Metadata: map[string]any{"source": "feed-distribution-completion"},
		TraceID:  p.TraceID,
	}); err != nil {
		return fmt.Errorf("feeddirection: write distribution audit: %w", err)
	}
	return nil
}

func writeDistributionVerdictAudit(ctx context.Context, tx pgx.Tx, tenantID, shedID, parkID, completionID, action, verifiedBy, traceID string) error {
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      strings.TrimSpace(verifiedBy),
		ActorType:    "verifier",
		Action:       action,
		ResourceType: feedDistributionResourceType,
		ResourceID:   completionID,
		ScopeType:    "shed",
		ScopeID:      shedID,
		AfterState: map[string]any{
			"park_id": parkID,
			"shed_id": shedID,
			"status":  domain.DistributionStatusCompleted,
		},
		Metadata: map[string]any{"source": "feed-distribution-verification"},
		TraceID:  traceID,
	}); err != nil {
		return fmt.Errorf("feeddirection: write distribution verdict audit: %w", err)
	}
	return nil
}

// feedDistributionCompletedOutbox is the input for the feed.distribution.completed outbox envelope.
type feedDistributionCompletedOutbox struct {
	TenantID     string
	CompletionID string
	ParkID       string
	ShedID       string
	SessionNo    int32
	TargetDate   string
	Workflow     string
	VerifiedBy   string
	TraceID      string
}

func insertFeedDistributionCompletedOutbox(ctx context.Context, tx pgx.Tx, o feedDistributionCompletedOutbox) error {
	idempotencyKey := feedDistributionCompletedEventType + ":" + o.CompletionID
	eventID := platformoutbox.DeterministicUUID(feedDistributionCompletedEventType + ":" + o.TenantID + ":" + o.CompletionID)

	payload := map[string]any{
		"completion_id": o.CompletionID,
		"park_id":       o.ParkID,
		"shed_id":       o.ShedID,
		"session_no":    o.SessionNo,
		"target_date":   o.TargetDate,
		"workflow":      o.Workflow,
		"verified_by":   o.VerifiedBy,
	}
	envelope := map[string]any{
		"event_id":        eventID,
		"event_type":      feedDistributionCompletedEventType,
		"schema_version":  feedDistributionCompletedSchemaVersion,
		"schema_ref":      feedDistributionCompletedSchemaRef,
		"aggregate_type":  feedDistributionCompletedAggregateType,
		"aggregate_id":    o.CompletionID,
		"producer":        "feeddirection",
		"idempotency_key": idempotencyKey,
		"subject_type":    "shed",
		"subject_id":      o.ShedID,
		"payload":         payload,
		"trace_id":        o.TraceID,
	}
	envelopeJSON, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("feeddirection: marshal distribution outbox envelope: %w", err)
	}
	headersJSON, err := json.Marshal(map[string]any{"content_type": "application/json"})
	if err != nil {
		return fmt.Errorf("feeddirection: marshal distribution outbox headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, $11, 'pending', now()
)
ON CONFLICT DO NOTHING`,
		o.TenantID, eventID, feedDistributionCompletedEventType, feedDistributionCompletedSchemaVersion,
		feedDistributionCompletedAggregateType, o.CompletionID, feedDistributionCompletedTopic,
		envelopeJSON, headersJSON, idempotencyKey, o.TraceID)
	if err != nil {
		return fmt.Errorf("feeddirection: insert distribution outbox: %w", err)
	}
	return nil
}
