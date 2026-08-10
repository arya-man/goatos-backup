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

// Feed PACKING verification gate write path (maintainer decision, 2026-07-26, SUPERSEDING the "packing
// stays instant" rule). This file owns the module's THIRD write path -- the NEW feed_packing_completions
// table -- entirely separate from completions.go (the old instant feed_direction_session_completions
// path, now inert) and distribution_completions.go (feed_distribution_completions). A packing completion
// is a two-phase, verifier-gated fact requiring ONE mandatory video:
//
//	CompletePacking        -> writes 'pending_verification' with the video (NOTHING completed)
//	ApplyVerifiedPacking   -> verifier approved: 'pending_verification' -> 'completed'; emits
//	                          feed.packing.completed (the ONLY producer of that event)
//	BouncePackingForRework -> verifier rejected: 'pending_verification' -> 'rework'
//
// It is registered in context/architecture/domain-event-registry.json; the literal event type below
// plus the outbox_messages INSERT are what the domain-event-architecture guard matches against.
const (
	feedPackingCompletedEventType     = "feed.packing.completed"
	feedPackingCompletedSchemaVersion = "1.0.0"
	feedPackingCompletedSchemaRef     = "domain-event-envelope.v1"
	feedPackingCompletedTopic         = "feed.events"
	feedPackingCompletedAggregateType = "feed_packing_completion"
	feedPackingIdemScope              = "feed.packing.complete"
	feedPackingResourceType           = "feed_packing_completion"

	feedPackingPendingAction   = "feed.packing.pending_verification"
	feedPackingReworkAction    = "feed.packing.rework"
	feedPackingCompletedAction = "feed.packing.completed"
)

var _ ports.PackingCompletionStore = (*Repository)(nil)

// CompletePacking records the operator's mandatory packing video at 'pending_verification' (or moves a
// 'rework' row back to it on a re-submit). Canonical write + audit + idempotency reservation are one
// transaction; idempotent on both the request key and the shed-session natural key. It does NOT emit
// feed.packing.completed -- that fires only when a verifier approves (ApplyVerifiedPacking).
func (r *Repository) CompletePacking(ctx context.Context, p ports.CompletePackingParams) (ports.CompletePackingResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	packingProof := strings.TrimSpace(p.PackingProofRef)
	targetDate := p.TargetDate.Format("2006-01-02")

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: begin packing completion tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// The completion identity is a real operational location: parent shed plus catalog partition
	// when the shed is subdivided. Blank means whole shed only for undivided sheds.
	if err := requireShedPartitionInPark(ctx, tx, p.TenantID, p.ParkID, p.ShedID, p.PartitionLabel); err != nil {
		return ports.CompletePackingResult{}, err
	}

	fingerprint := requestFingerprint(
		p.ParkID,
		p.ShedID,
		domain.PartitionMatchKey(p.PartitionLabel),
		fmt.Sprintf("%d", p.SessionNo),
		targetDate,
		p.Workflow,
		packingProof,
	)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, feedPackingIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.CompletePackingResult{}, err
	}
	if !reservation.proceed {
		// Exact replay of the same request: return the original result, run NO side effects. Read the row
		// so the caller still sees its current status/row_version, but NewlyPending stays false so no
		// verification item is re-enqueued.
		status, rowVersion, readErr := r.readPackingByID(ctx, tx, p.TenantID, reservation.resultID)
		if readErr != nil {
			return ports.CompletePackingResult{}, readErr
		}
		if err := tx.Commit(ctx); err != nil {
			return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: commit idempotent packing replay: %w", err)
		}
		committed = true
		return ports.CompletePackingResult{CompletionID: reservation.resultID, Status: status, RowVersion: rowVersion, NewlyPending: false}, nil
	}

	var (
		completionID string
		rowVersion   int32
		status       = domain.PackingStatusPendingVerification
		newlyPending bool
	)
	err = tx.QueryRow(ctx, `
INSERT INTO feed_packing_completions (
  tenant_id, park_id, shed_id, partition_label, session_no, target_date, workflow, status,
  packing_proof_ref, completed_by, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, nullif($4::text, ''), $5, $6::date, $7, 'pending_verification',
  $8, nullif($9::text, '')::uuid, $10
)
ON CONFLICT (tenant_id, park_id, shed_id, partition_key, session_no, target_date, workflow) DO NOTHING
RETURNING completion_id::text, row_version`,
		p.TenantID, p.ParkID, p.ShedID, p.PartitionLabel, p.SessionNo, targetDate, p.Workflow,
		packingProof, p.CompletedBy, p.IdempotencyKey).Scan(&completionID, &rowVersion)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Natural-key conflict: a row for this shed-session already exists. Its state decides the outcome.
		var existingStatus string
		if err := tx.QueryRow(ctx, `
SELECT completion_id::text, status, row_version
FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
  AND partition_key = $7 AND session_no = $4 AND target_date = $5::date AND workflow = $6`,
			p.TenantID, p.ParkID, p.ShedID, p.SessionNo, targetDate, p.Workflow,
			domain.PartitionMatchKey(p.PartitionLabel)).
			Scan(&completionID, &existingStatus, &rowVersion); err != nil {
			return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: read existing packing completion: %w", err)
		}
		switch existingStatus {
		case domain.PackingStatusRework:
			// A verifier rejected the prior video; the operator re-recorded. Move the row back to
			// pending_verification with the NEW video, bump row_version, clear the rework reason. This is a
			// fresh pending transition, so it enqueues a fresh verification item.
			if err := tx.QueryRow(ctx, `
UPDATE feed_packing_completions
SET status = 'pending_verification',
    packing_proof_ref = $3,
    rework_reason = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'rework'
RETURNING row_version`,
				p.TenantID, completionID, packingProof).Scan(&rowVersion); err != nil {
				return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: resubmit packing for verification: %w", err)
			}
			status = domain.PackingStatusPendingVerification
			newlyPending = true
			if err := writePackingAudit(ctx, tx, p, completionID, feedPackingPendingAction); err != nil {
				return ports.CompletePackingResult{}, err
			}
		case domain.PackingStatusPendingVerification:
			// Already awaiting verification: idempotent no-op, no new verification item.
			status = domain.PackingStatusPendingVerification
		case domain.PackingStatusCompleted:
			// Already verified/completed: no-op.
			status = domain.PackingStatusCompleted
		default:
			return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: unexpected packing status %q", existingStatus)
		}
	case err != nil:
		return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: insert packing completion: %w", err)
	default:
		// Fresh insert: a brand-new pending_verification row.
		newlyPending = true
		if err := writePackingAudit(ctx, tx, p, completionID, feedPackingPendingAction); err != nil {
			return ports.CompletePackingResult{}, err
		}
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, feedPackingIdemScope, p.IdempotencyKey, feedPackingResourceType, completionID); err != nil {
		return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: complete packing idempotency: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ports.CompletePackingResult{}, fmt.Errorf("feeddirection: commit packing completion: %w", err)
	}
	committed = true
	return ports.CompletePackingResult{CompletionID: completionID, Status: status, RowVersion: rowVersion, NewlyPending: newlyPending}, nil
}

// readPackingByID reads a row's status and row_version within the transaction, for the idempotent
// replay echo.
func (r *Repository) readPackingByID(ctx context.Context, tx pgx.Tx, tenantID, completionID string) (string, int32, error) {
	if strings.TrimSpace(completionID) == "" {
		return "", 0, nil
	}
	var status string
	var rowVersion int32
	err := tx.QueryRow(ctx, `
SELECT status, row_version
FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, tenantID, completionID).Scan(&status, &rowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("feeddirection: read packing completion by id: %w", err)
	}
	return status, rowVersion, nil
}

// ListVerifiedPacking returns every VERIFIED (status='completed') (shed, session, workflow) for one
// park-day in one bounded indexed read -- the packing serving-read overlay.
func (r *Repository) ListVerifiedPacking(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]ports.VerifiedPacking, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded read of ONE park-day's VERIFIED packing shed-sessions, covered by feed_packing_completions_serving_idx (tenant_id, park_id, target_date, workflow). Bounded by the park's shed catalog x sessions (physical infrastructure), never by herd size; binds are cast, indexed columns stay bare.
	rows, err := r.pool.Query(ctx, `
SELECT shed_id::text, session_no, workflow
FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND target_date = $3::date AND status = 'completed'`,
		tenantID, parkID, targetDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("feeddirection: list verified packing: %w", err)
	}
	defer rows.Close()
	out := make([]ports.VerifiedPacking, 0)
	for rows.Next() {
		var d ports.VerifiedPacking
		if err := rows.Scan(&d.ShedID, &d.SessionNo, &d.Workflow); err != nil {
			return nil, fmt.Errorf("feeddirection: scan verified packing: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListPackingSessionStatuses returns EVERY (shed, session, workflow) with a feed_packing_completions
// row for one park-day plus its RAW status -- the packing serve path's status overlay + filter
// source (includes pending_verification and rework, not just completed).
func (r *Repository) ListPackingSessionStatuses(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]ports.SessionCompletionStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded read of ONE park-day's packing shed-session statuses, covered by feed_packing_completions_serving_idx (tenant_id, park_id, target_date, workflow). Bounded by the park's shed catalog x sessions (physical infrastructure), never by herd size; binds are cast, indexed columns stay bare.
	rows, err := r.pool.Query(ctx, `
SELECT shed_id::text, coalesce(partition_label, ''), session_no, workflow, status
FROM feed_packing_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND target_date = $3::date`,
		tenantID, parkID, targetDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("feeddirection: list packing session statuses: %w", err)
	}
	defer rows.Close()
	out := make([]ports.SessionCompletionStatus, 0)
	for rows.Next() {
		var d ports.SessionCompletionStatus
		if err := rows.Scan(&d.ShedID, &d.PartitionLabel, &d.SessionNo, &d.Workflow, &d.Status); err != nil {
			return nil, fmt.Errorf("feeddirection: scan packing session status: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ApplyVerifiedPacking flips a packing completion whose video a verifier APPROVED
// 'pending_verification' -> 'completed' and emits feed.packing.completed, in one transaction. It runs
// from the verification.verdict.approved consumer, never from the operator's phone.
//
// IDEMPOTENT: a re-delivered verdict on an already-'completed' row returns false with no side effects.
// A verdict for a row no longer 'pending_verification' (bounced to rework, or otherwise moved on) is
// ignored as stale (returns false).
func (r *Repository) ApplyVerifiedPacking(ctx context.Context, p ports.ApplyPackingParams) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("feeddirection: begin apply packing tx: %w", err)
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
FROM feed_packing_completions
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
		return false, fmt.Errorf("feeddirection: lock packing completion: %w", err)
	}
	// Already completed (re-delivered verdict) or no longer pending (stale delivery): no side effects.
	if status != domain.PackingStatusPendingVerification {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}

	tag, err := tx.Exec(ctx, `
UPDATE feed_packing_completions
SET status = 'completed',
    verified_by = nullif($3::text, '')::uuid,
    verified_at = now(),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'pending_verification'`,
		p.TenantID, p.CompletionID, p.VerifiedBy)
	if err != nil {
		return false, fmt.Errorf("feeddirection: apply verified packing: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Lost the race to a concurrent transition; treat as stale.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}

	if err := insertFeedPackingCompletedOutbox(ctx, tx, feedPackingCompletedOutbox{
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
	if err := writePackingVerdictAudit(ctx, tx, p.TenantID, shedID, parkID, p.CompletionID, feedPackingCompletedAction, p.VerifiedBy, p.TraceID); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("feeddirection: commit apply verified packing: %w", err)
	}
	committed = true
	return true, nil
}

// BouncePackingForRework flips a packing completion whose video a verifier REJECTED
// 'pending_verification' -> 'rework'. It runs from the verification.verdict.rework consumer. NOTHING is
// completed. Idempotent + stale-guarded: only a 'pending_verification' row is bounced.
func (r *Repository) BouncePackingForRework(ctx context.Context, p ports.BouncePackingParams) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tag, err := r.pool.Exec(ctx, `
UPDATE feed_packing_completions
SET status = 'rework',
    rework_reason = nullif($3, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'pending_verification'`,
		p.TenantID, p.CompletionID, strings.TrimSpace(p.Reason))
	if err != nil {
		return false, fmt.Errorf("feeddirection: bounce packing for rework: %w", err)
	}
	// Zero rows affected is an accepted stale/duplicate verdict; no error.
	return tag.RowsAffected() > 0, nil
}

func writePackingAudit(ctx context.Context, tx pgx.Tx, p ports.CompletePackingParams, completionID, action string) error {
	actorType := strings.TrimSpace(p.ActorType)
	if actorType == "" {
		actorType = "operator"
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       action,
		ResourceType: feedPackingResourceType,
		ResourceID:   completionID,
		ScopeType:    "shed",
		ScopeID:      p.ShedID,
		AfterState: map[string]any{
			"park_id":     p.ParkID,
			"shed_id":     p.ShedID,
			"session_no":  p.SessionNo,
			"target_date": p.TargetDate.Format("2006-01-02"),
			"workflow":    p.Workflow,
			"status":      domain.PackingStatusPendingVerification,
		},
		Metadata: map[string]any{"source": "feed-packing-completion"},
		TraceID:  p.TraceID,
	}); err != nil {
		return fmt.Errorf("feeddirection: write packing audit: %w", err)
	}
	return nil
}

func writePackingVerdictAudit(ctx context.Context, tx pgx.Tx, tenantID, shedID, parkID, completionID, action, verifiedBy, traceID string) error {
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      strings.TrimSpace(verifiedBy),
		ActorType:    "verifier",
		Action:       action,
		ResourceType: feedPackingResourceType,
		ResourceID:   completionID,
		ScopeType:    "shed",
		ScopeID:      shedID,
		AfterState: map[string]any{
			"park_id": parkID,
			"shed_id": shedID,
			"status":  domain.PackingStatusCompleted,
		},
		Metadata: map[string]any{"source": "feed-packing-verification"},
		TraceID:  traceID,
	}); err != nil {
		return fmt.Errorf("feeddirection: write packing verdict audit: %w", err)
	}
	return nil
}

// feedPackingCompletedOutbox is the input for the feed.packing.completed outbox envelope.
type feedPackingCompletedOutbox struct {
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

func insertFeedPackingCompletedOutbox(ctx context.Context, tx pgx.Tx, o feedPackingCompletedOutbox) error {
	idempotencyKey := feedPackingCompletedEventType + ":" + o.CompletionID
	eventID := platformoutbox.DeterministicUUID(feedPackingCompletedEventType + ":" + o.TenantID + ":" + o.CompletionID)

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
		"event_type":      feedPackingCompletedEventType,
		"schema_version":  feedPackingCompletedSchemaVersion,
		"schema_ref":      feedPackingCompletedSchemaRef,
		"aggregate_type":  feedPackingCompletedAggregateType,
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
		return fmt.Errorf("feeddirection: marshal packing outbox envelope: %w", err)
	}
	headersJSON, err := json.Marshal(map[string]any{"content_type": "application/json"})
	if err != nil {
		return fmt.Errorf("feeddirection: marshal packing outbox headers: %w", err)
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
		o.TenantID, eventID, feedPackingCompletedEventType, feedPackingCompletedSchemaVersion,
		feedPackingCompletedAggregateType, o.CompletionID, feedPackingCompletedTopic,
		envelopeJSON, headersJSON, idempotencyKey, o.TraceID)
	if err != nil {
		return fmt.Errorf("feeddirection: insert packing outbox: %w", err)
	}
	return nil
}
