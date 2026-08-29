package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// Feed WASTAGE verification gate write path (maintainer decision, 2026-08-18). This file owns the
// module's FOURTH write path — the NEW feed_wastage_completions table — mirroring the packing gate
// (packing_completions.go) at the PEN-DAY grain, plus one write no sibling has: the VERIFIER'S
// measured leftover weight, recorded off the video she is reviewing.
//
//	CompleteWastage          -> writes 'pending_verification' with the video (NOTHING completed)
//	ApplyVerifiedWastage     -> verifier approved: 'pending_verification' -> 'completed'; emits
//	                            feed.wastage.completed (the ONLY producer of that event)
//	BounceWastageForRework   -> verifier rejected: 'pending_verification' -> 'rework'
//	RecordWastageMeasurement -> stores the verifier's kg reading; REPLACES on re-entry; never
//	                            changes status (the verdict owns the lifecycle)
//
// It is registered in context/architecture/domain-event-registry.json; the literal event type below
// plus the outbox_messages INSERT are what the domain-event-architecture guard matches against.
const (
	feedWastageCompletedEventType     = "feed.wastage.completed"
	feedWastageCompletedSchemaVersion = "1.0.0"
	feedWastageCompletedSchemaRef     = "domain-event-envelope.v1"
	feedWastageCompletedTopic         = "feed.events"
	feedWastageCompletedAggregateType = "feed_wastage_completion"
	feedWastageIdemScope              = "feed.wastage.complete"
	feedWastageMeasurementIdemScope   = "feed.wastage.measurement"
	feedWastageResourceType           = "feed_wastage_completion"

	feedWastagePendingAction     = "feed.wastage.pending_verification"
	feedWastageCompletedAction   = "feed.wastage.completed"
	feedWastageMeasurementAction = "feed.wastage.measurement_recorded"
)

// maxRecordableWastageKg is a TYPO GUARD, not a biological claim: no pen wastes ten tonnes of feed
// in a day, so a value past this is a slipped keypress refused at the door rather than stored and
// dragged through every later analytics read. Zero is VALID — an empty trough is a real measurement.
const maxRecordableWastageKg = 10000

var _ ports.WastageCompletionStore = (*Repository)(nil)

// CompleteWastage records the operator's mandatory wastage video at 'pending_verification' (or
// moves a 'rework' row back to it on a re-submit). Canonical write + audit + idempotency
// reservation are one transaction; idempotent on both the request key and the pen-day natural key.
// It does NOT emit feed.wastage.completed — that fires only when a verifier approves.
func (r *Repository) CompleteWastage(ctx context.Context, p ports.CompleteWastageParams) (ports.CompleteWastageResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	wastageProof := strings.TrimSpace(p.WastageProofRef)
	targetDate := p.TargetDate.Format("2006-01-02")

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.CompleteWastageResult{}, fmt.Errorf("feeddirection: begin wastage completion tx: %w", err)
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
		return ports.CompleteWastageResult{}, err
	}

	// The shed's display NAME, for the verification item's subject label. Only the NAME is taken
	// from the canonical resolver — this completion is for ONE named pen, and agree-or-go-bare at
	// shed grain would resolve a multi-pen shed bare and lose it. The caller's own partition is the
	// honest value (same reasoning as CompletePacking).
	shedLocation, err := oploc.ResolveShedLocation(ctx, tx.QueryRow(ctx, oploc.ShedScopedLocationSQL, p.TenantID, p.ShedID))
	if err != nil {
		return ports.CompleteWastageResult{}, fmt.Errorf("feeddirection: resolve wastage shed location: %w", err)
	}
	shedName := shedLocation.ShedName

	fingerprint := requestFingerprint(
		p.ParkID,
		p.ShedID,
		domain.PartitionMatchKey(p.PartitionLabel),
		targetDate,
		wastageProof,
	)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, feedWastageIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.CompleteWastageResult{}, err
	}
	if !reservation.proceed {
		// Exact replay: return the original result, run NO side effects. NewlyPending stays false so
		// no verification item is re-enqueued.
		status, rowVersion, readErr := r.readWastageByID(ctx, tx, p.TenantID, reservation.resultID)
		if readErr != nil {
			return ports.CompleteWastageResult{}, readErr
		}
		if err := tx.Commit(ctx); err != nil {
			return ports.CompleteWastageResult{}, fmt.Errorf("feeddirection: commit idempotent wastage replay: %w", err)
		}
		committed = true
		return ports.CompleteWastageResult{
			CompletionID: reservation.resultID, Status: status, RowVersion: rowVersion, NewlyPending: false,
			ShedName: shedName, PartitionLabel: p.PartitionLabel,
		}, nil
	}

	var (
		completionID string
		rowVersion   int32
		status       = domain.WastageStatusPendingVerification
		newlyPending bool
	)
	err = tx.QueryRow(ctx, `
INSERT INTO feed_wastage_completions (
  tenant_id, park_id, shed_id, partition_label, target_date, workflow, status,
  wastage_proof_ref, completed_by, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, nullif($4::text, ''), $5::date, 'experiment', 'pending_verification',
  $6, nullif($7::text, '')::uuid, $8
)
ON CONFLICT (tenant_id, park_id, shed_id, partition_key, target_date, workflow) DO NOTHING
RETURNING completion_id::text, row_version`,
		p.TenantID, p.ParkID, p.ShedID, p.PartitionLabel, targetDate,
		wastageProof, p.CompletedBy, p.IdempotencyKey).Scan(&completionID, &rowVersion)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Natural-key conflict: a row for this PEN-DAY already exists. Its state AND its stored
		// video decide the outcome — a second DIFFERENT video is a conflict, not a replay. See
		// ports.ErrWastageAlreadyRecorded.
		var existingStatus, existingProof string
		if err := tx.QueryRow(ctx, `
SELECT completion_id::text, status, row_version, coalesce(wastage_proof_ref, '')
FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
  AND partition_key = $5 AND target_date = $4::date AND workflow = 'experiment'`,
			p.TenantID, p.ParkID, p.ShedID, targetDate,
			domain.PartitionMatchKey(p.PartitionLabel)).
			Scan(&completionID, &existingStatus, &rowVersion, &existingProof); err != nil {
			return ports.CompleteWastageResult{}, fmt.Errorf("feeddirection: read existing wastage completion: %w", err)
		}
		switch existingStatus {
		case domain.WastageStatusRework:
			// A verifier rejected the prior video; the operator re-recorded. Move the row back to
			// pending_verification with the NEW video, bump row_version, clear the rework reason.
			// This is a fresh pending transition, so it enqueues a fresh verification item.
			if err := tx.QueryRow(ctx, `
UPDATE feed_wastage_completions
SET status = 'pending_verification',
    wastage_proof_ref = $3,
    rework_reason = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'rework'
RETURNING row_version`,
				p.TenantID, completionID, wastageProof).Scan(&rowVersion); err != nil {
				return ports.CompleteWastageResult{}, fmt.Errorf("feeddirection: resubmit wastage for verification: %w", err)
			}
			status = domain.WastageStatusPendingVerification
			newlyPending = true
			if err := writeWastageAudit(ctx, tx, p, completionID, feedWastagePendingAction); err != nil {
				return ports.CompleteWastageResult{}, err
			}
		case domain.WastageStatusPendingVerification, domain.WastageStatusCompleted:
			// SAME proof -> a genuine re-send under a different idempotency key: idempotent no-op.
			// DIFFERENT proof -> a second, distinct recording for a unit that accepts exactly one:
			// refuse loudly rather than acknowledge a video nothing stored.
			if wastageProof != existingProof {
				return ports.CompleteWastageResult{}, ports.ErrWastageAlreadyRecorded
			}
			status = existingStatus
		default:
			return ports.CompleteWastageResult{}, fmt.Errorf("feeddirection: unexpected wastage status %q", existingStatus)
		}
	case err != nil:
		return ports.CompleteWastageResult{}, fmt.Errorf("feeddirection: insert wastage completion: %w", err)
	default:
		// Fresh insert: a brand-new pending_verification row.
		newlyPending = true
		if err := writeWastageAudit(ctx, tx, p, completionID, feedWastagePendingAction); err != nil {
			return ports.CompleteWastageResult{}, err
		}
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, feedWastageIdemScope, p.IdempotencyKey, feedWastageResourceType, completionID); err != nil {
		return ports.CompleteWastageResult{}, fmt.Errorf("feeddirection: complete wastage idempotency: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ports.CompleteWastageResult{}, fmt.Errorf("feeddirection: commit wastage completion: %w", err)
	}
	committed = true
	return ports.CompleteWastageResult{
		CompletionID: completionID, Status: status, RowVersion: rowVersion, NewlyPending: newlyPending,
		// Carried on EVERY path, including the idempotent replay above: the enqueue reads these to
		// compose the verifier's subject label.
		ShedName: shedName, PartitionLabel: p.PartitionLabel,
	}, nil
}

// readWastageByID reads a row's status and row_version within the transaction, for the idempotent
// replay echo.
func (r *Repository) readWastageByID(ctx context.Context, tx pgx.Tx, tenantID, completionID string) (string, int32, error) {
	if strings.TrimSpace(completionID) == "" {
		return "", 0, nil
	}
	var status string
	var rowVersion int32
	err := tx.QueryRow(ctx, `
SELECT status, row_version
FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, tenantID, completionID).Scan(&status, &rowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("feeddirection: read wastage completion by id: %w", err)
	}
	return status, rowVersion, nil
}

// ListWastageCompletionStatuses returns EVERY pen with a feed_wastage_completions row for one
// park-day plus its RAW status and any recorded measurement — the wastage serve path's status
// overlay + filter source.
func (r *Repository) ListWastageCompletionStatuses(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]ports.WastageCompletionStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded read of ONE park-day's wastage rows, covered by feed_wastage_completions_serving_idx (tenant_id, park_id, target_date, workflow). Bounded by the park's pen catalog (physical infrastructure), never by herd size; binds are cast, indexed columns stay bare.
	rows, err := r.pool.Query(ctx, `
SELECT shed_id::text, coalesce(partition_label, ''), status, coalesce(rework_reason, ''),
       coalesce(wastage_kg::text, '')
FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND target_date = $3::date`,
		tenantID, parkID, targetDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("feeddirection: list wastage completion statuses: %w", err)
	}
	defer rows.Close()
	out := make([]ports.WastageCompletionStatus, 0)
	for rows.Next() {
		var d ports.WastageCompletionStatus
		if err := rows.Scan(&d.ShedID, &d.PartitionLabel, &d.Status, &d.ReworkReason, &d.WastageKg); err != nil {
			return nil, fmt.Errorf("feeddirection: scan wastage completion status: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ApplyVerifiedWastage flips a wastage completion whose video a verifier APPROVED
// 'pending_verification' -> 'completed' and emits feed.wastage.completed, in one transaction. It
// runs from the verification.verdict.approved consumer, never from the operator's phone.
// IDEMPOTENT + stale-guarded, same contract as ApplyVerifiedPacking.
func (r *Repository) ApplyVerifiedWastage(ctx context.Context, p ports.ApplyWastageParams) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("feeddirection: begin apply wastage tx: %w", err)
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
		targetDate time.Time
		wastageKg  *float64
	)
	err = tx.QueryRow(ctx, `
SELECT status, park_id::text, shed_id::text, target_date, wastage_kg::float8
FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid
FOR UPDATE`, p.TenantID, p.CompletionID).Scan(&status, &parkID, &shedID, &targetDate, &wastageKg)
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("feeddirection: lock wastage completion: %w", err)
	}
	if status != domain.WastageStatusPendingVerification {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}
	if wastageKg == nil {
		return false, ports.ErrWastageMeasurementRequired
	}

	tag, err := tx.Exec(ctx, `
UPDATE feed_wastage_completions
SET status = 'completed',
    verified_by = nullif($3::text, '')::uuid,
    verified_at = now(),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'pending_verification'`,
		p.TenantID, p.CompletionID, p.VerifiedBy)
	if err != nil {
		return false, fmt.Errorf("feeddirection: apply verified wastage: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return false, commitErr
		}
		committed = true
		return false, nil
	}

	if err := insertFeedWastageCompletedOutbox(ctx, tx, feedWastageCompletedOutbox{
		TenantID:     p.TenantID,
		CompletionID: p.CompletionID,
		ParkID:       parkID,
		ShedID:       shedID,
		TargetDate:   targetDate.Format("2006-01-02"),
		WastageKg:    wastageKg,
		VerifiedBy:   strings.TrimSpace(p.VerifiedBy),
		TraceID:      p.TraceID,
	}); err != nil {
		return false, err
	}
	if err := writeWastageVerdictAudit(ctx, tx, p.TenantID, shedID, parkID, p.CompletionID, feedWastageCompletedAction, p.VerifiedBy, p.TraceID); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("feeddirection: commit apply verified wastage: %w", err)
	}
	committed = true
	return true, nil
}

// BounceWastageForRework flips a wastage completion whose video a verifier REJECTED
// 'pending_verification' -> 'rework'. Idempotent + stale-guarded: only a pending row is bounced.
func (r *Repository) BounceWastageForRework(ctx context.Context, p ports.BounceWastageParams) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tag, err := r.pool.Exec(ctx, `
UPDATE feed_wastage_completions
SET status = 'rework',
    rework_reason = nullif($3, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'pending_verification'`,
		p.TenantID, p.CompletionID, strings.TrimSpace(p.Reason))
	if err != nil {
		return false, fmt.Errorf("feeddirection: bounce wastage for rework: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// RecordWastageMeasurement stores the VERIFIER'S measured leftover weight on the completion row.
// A later entry REPLACES the value (recorded_by/at move with it); status is never touched — the
// verdict owns the lifecycle. One transaction: idempotency reservation + write + audit.
func (r *Repository) RecordWastageMeasurement(ctx context.Context, p ports.RecordWastageMeasurementParams) (ports.RecordWastageMeasurementResult, error) {
	if math.IsNaN(p.WastageKg) || math.IsInf(p.WastageKg, 0) || p.WastageKg < 0 || p.WastageKg > maxRecordableWastageKg {
		return ports.RecordWastageMeasurementResult{}, ports.ErrWastageValueOutOfRange
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: begin wastage measurement tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	fingerprint := requestFingerprint(p.CompletionID, strconv.FormatFloat(p.WastageKg, 'f', 3, 64))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, feedWastageMeasurementIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.RecordWastageMeasurementResult{}, err
	}
	if !reservation.proceed {
		if len(reservation.snapshot) > 0 {
			var result ports.RecordWastageMeasurementResult
			if err := json.Unmarshal(reservation.snapshot, &result); err != nil {
				return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: decode wastage measurement idempotency snapshot: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: commit idempotent measurement snapshot replay: %w", err)
			}
			committed = true
			return result, nil
		}
		// Exact replay: the value on the row IS this request's value (same fingerprint), so read it
		// back and run no side effects. This fallback is only for legacy keys written before
		// result_snapshot was used by this feed write.
		result, readErr := r.readWastageMeasurement(ctx, tx, p.TenantID, p.CompletionID)
		if readErr != nil {
			return ports.RecordWastageMeasurementResult{}, readErr
		}
		if err := tx.Commit(ctx); err != nil {
			return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: commit idempotent measurement replay: %w", err)
		}
		committed = true
		return result, nil
	}

	var (
		previous       *float64
		shedID         string
		shedName       string
		partitionLabel string
		recordedAt     time.Time
	)
	// Lock the row and read the prior value first, then write. Two statements in one transaction
	// rather than an UPDATE...FROM with a locking subquery, which Postgres refuses.
	err = tx.QueryRow(ctx, `
SELECT wastage_kg::float8, shed_id::text, coalesce(partition_label, '')
FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid
FOR UPDATE`, p.TenantID, p.CompletionID).Scan(&previous, &shedID, &partitionLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.RecordWastageMeasurementResult{}, ports.ErrWastageCompletionNotFound
	}
	if err != nil {
		return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: lock wastage completion for measurement: %w", err)
	}
	if err := tx.QueryRow(ctx, `
UPDATE feed_wastage_completions
SET wastage_kg = $3,
    wastage_recorded_by = $4::uuid,
    wastage_recorded_at = now(),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid
RETURNING wastage_recorded_at`,
		p.TenantID, p.CompletionID, p.WastageKg, p.RecordedBy).Scan(&recordedAt); err != nil {
		return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: record wastage measurement: %w", err)
	}

	shedLocation, err := oploc.ResolveShedLocation(ctx, tx.QueryRow(ctx, oploc.ShedScopedLocationSQL, p.TenantID, shedID))
	if err != nil {
		return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: resolve wastage measurement shed location: %w", err)
	}
	shedName = shedLocation.ShedName

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      strings.TrimSpace(p.RecordedBy),
		ActorType:    "verifier",
		Action:       feedWastageMeasurementAction,
		ResourceType: feedWastageResourceType,
		ResourceID:   p.CompletionID,
		ScopeType:    "shed",
		ScopeID:      shedID,
		AfterState: map[string]any{
			"wastage_kg":          p.WastageKg,
			"previous_wastage_kg": previous,
		},
		Metadata: map[string]any{"source": "feed-wastage-measurement"},
		TraceID:  p.TraceID,
	}); err != nil {
		return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: write wastage measurement audit: %w", err)
	}

	result := ports.RecordWastageMeasurementResult{
		CompletionID:      p.CompletionID,
		WastageKg:         p.WastageKg,
		PreviousWastageKg: previous,
		RecordedBy:        p.RecordedBy,
		RecordedAt:        recordedAt,
		SubjectLabel:      wastageSubjectLabel(shedName, partitionLabel, &p.WastageKg),
	}
	if err := completeIdempotencyWithSnapshot(ctx, tx, p.TenantID, feedWastageMeasurementIdemScope, p.IdempotencyKey, feedWastageResourceType, p.CompletionID, result); err != nil {
		return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: complete wastage measurement idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: commit wastage measurement: %w", err)
	}
	committed = true

	return result, nil
}

// readWastageMeasurement composes the replay readback from the row's current state.
func (r *Repository) readWastageMeasurement(ctx context.Context, tx pgx.Tx, tenantID, completionID string) (ports.RecordWastageMeasurementResult, error) {
	var (
		kg             *float64
		recordedBy     *string
		recordedAt     *time.Time
		shedID         string
		partitionLabel string
	)
	err := tx.QueryRow(ctx, `
SELECT wastage_kg::float8, wastage_recorded_by::text, wastage_recorded_at, shed_id::text, coalesce(partition_label, '')
FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, tenantID, completionID).
		Scan(&kg, &recordedBy, &recordedAt, &shedID, &partitionLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.RecordWastageMeasurementResult{}, ports.ErrWastageCompletionNotFound
	}
	if err != nil {
		return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: read wastage measurement: %w", err)
	}
	out := ports.RecordWastageMeasurementResult{CompletionID: completionID}
	if kg != nil {
		out.WastageKg = *kg
	}
	if recordedBy != nil {
		out.RecordedBy = *recordedBy
	}
	if recordedAt != nil {
		out.RecordedAt = *recordedAt
	}
	shedLocation, err := oploc.ResolveShedLocation(ctx, tx.QueryRow(ctx, oploc.ShedScopedLocationSQL, tenantID, shedID))
	if err != nil {
		return ports.RecordWastageMeasurementResult{}, fmt.Errorf("feeddirection: resolve wastage replay shed location: %w", err)
	}
	out.SubjectLabel = wastageSubjectLabel(shedLocation.ShedName, partitionLabel, kg)
	return out, nil
}

// wastageSubjectLabel composes the verifier-facing subject for a wastage item: the pen, plus the
// recorded value once one exists — "Castro - 2 · 3.5 kg wastage recorded". Backend owns this copy
// (dumb-renderer rule). Degrades to the bare pen when no value is recorded, and to nothing when the
// location cannot be resolved (the caller then leaves the existing label alone).
func wastageSubjectLabel(shedName, partitionLabel string, kg *float64) string {
	loc := oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}
	display := loc.Display()
	if display == "" {
		return ""
	}
	if kg == nil {
		return display
	}
	return display + " · " + strconv.FormatFloat(*kg, 'f', -1, 64) + " kg wastage recorded"
}

func writeWastageAudit(ctx context.Context, tx pgx.Tx, p ports.CompleteWastageParams, completionID, action string) error {
	actorType := strings.TrimSpace(p.ActorType)
	if actorType == "" {
		actorType = "operator"
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       action,
		ResourceType: feedWastageResourceType,
		ResourceID:   completionID,
		ScopeType:    "shed",
		ScopeID:      p.ShedID,
		AfterState: map[string]any{
			"park_id":         p.ParkID,
			"shed_id":         p.ShedID,
			"partition_label": p.PartitionLabel,
			"target_date":     p.TargetDate.Format("2006-01-02"),
			"workflow":        domain.WorkflowExperiment,
			"status":          domain.WastageStatusPendingVerification,
		},
		Metadata: map[string]any{"source": "feed-wastage-completion"},
		TraceID:  p.TraceID,
	}); err != nil {
		return fmt.Errorf("feeddirection: write wastage audit: %w", err)
	}
	return nil
}

func writeWastageVerdictAudit(ctx context.Context, tx pgx.Tx, tenantID, shedID, parkID, completionID, action, verifiedBy, traceID string) error {
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      strings.TrimSpace(verifiedBy),
		ActorType:    "verifier",
		Action:       action,
		ResourceType: feedWastageResourceType,
		ResourceID:   completionID,
		ScopeType:    "shed",
		ScopeID:      shedID,
		AfterState: map[string]any{
			"park_id": parkID,
			"shed_id": shedID,
			"status":  domain.WastageStatusCompleted,
		},
		Metadata: map[string]any{"source": "feed-wastage-verification"},
		TraceID:  traceID,
	}); err != nil {
		return fmt.Errorf("feeddirection: write wastage verdict audit: %w", err)
	}
	return nil
}

// feedWastageCompletedOutbox is the input for the feed.wastage.completed outbox envelope.
type feedWastageCompletedOutbox struct {
	TenantID     string
	CompletionID string
	ParkID       string
	ShedID       string
	TargetDate   string
	// WastageKg is the verifier's recorded measurement IF she recorded it before approving; nil
	// otherwise. Consumers must treat absence as "not yet measured", never as zero.
	WastageKg  *float64
	VerifiedBy string
	TraceID    string
}

func insertFeedWastageCompletedOutbox(ctx context.Context, tx pgx.Tx, o feedWastageCompletedOutbox) error {
	idempotencyKey := feedWastageCompletedEventType + ":" + o.CompletionID
	eventID := platformoutbox.DeterministicUUID(feedWastageCompletedEventType + ":" + o.TenantID + ":" + o.CompletionID)

	payload := map[string]any{
		"completion_id": o.CompletionID,
		"park_id":       o.ParkID,
		"shed_id":       o.ShedID,
		"target_date":   o.TargetDate,
		"workflow":      domain.WorkflowExperiment,
		"verified_by":   o.VerifiedBy,
	}
	if o.WastageKg != nil {
		payload["wastage_kg"] = *o.WastageKg
	}
	envelope := feedEventEnvelope{
		EventID:        eventID,
		EventType:      feedWastageCompletedEventType,
		SchemaVersion:  feedWastageCompletedSchemaVersion,
		SchemaRef:      feedWastageCompletedSchemaRef,
		AggregateType:  feedWastageCompletedAggregateType,
		AggregateID:    o.CompletionID,
		IdempotencyKey: idempotencyKey,
		TenantID:       o.TenantID,
		ParkID:         o.ParkID,
		ShedID:         o.ShedID,
		ActorID:        o.VerifiedBy,
		// The FEED DAY, not the verdict instant: the business fact is that this pen's wastage for
		// that day is proved.
		OccurredAt: businessInstant(o.TargetDate),
		Payload:    payload,
		TraceID:    o.TraceID,
	}.build()
	envelopeJSON, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("feeddirection: marshal wastage outbox envelope: %w", err)
	}
	headersJSON, err := json.Marshal(map[string]any{"content_type": "application/json"})
	if err != nil {
		return fmt.Errorf("feeddirection: marshal wastage outbox headers: %w", err)
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
		o.TenantID, eventID, feedWastageCompletedEventType, feedWastageCompletedSchemaVersion,
		feedWastageCompletedAggregateType, o.CompletionID, feedWastageCompletedTopic,
		envelopeJSON, headersJSON, idempotencyKey, o.TraceID)
	if err != nil {
		return fmt.Errorf("feeddirection: insert wastage outbox: %w", err)
	}
	return nil
}

// WastageMeasurementRecorded reports whether a completion already carries a measured leftover
// weight, for the approve gate. One primary-key read on (tenant_id, completion_id).
//
// A completion that does not exist answers ErrWastageCompletionNotFound rather than "no
// measurement": the two mean different things to the verifier, and folding them together would
// tell her to enter a number against a record that is not there.
func (r *Repository) WastageMeasurementRecorded(ctx context.Context, tenantID, completionID string) (bool, error) {
	var kg *float64
	err := r.pool.QueryRow(ctx, `
SELECT wastage_kg::float8
FROM feed_wastage_completions
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`, tenantID, completionID).Scan(&kg)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ports.ErrWastageCompletionNotFound
	}
	if err != nil {
		return false, fmt.Errorf("feeddirection: read wastage measurement flag: %w", err)
	}
	return kg != nil, nil
}
