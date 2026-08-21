package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// SubmitTask flips the WHOLE task open|rework -> pending_verification once every scanned
// animal carries its full slot set, in one transaction: readiness check, task flip
// (row_version bump), animal submitted_at stamps, audit, idempotency. It composes the labeled
// media set the ONE verification item carries. It completes NOTHING — the task is completed
// only when a verifier approves (ApplyVerifiedTask).
func (r *Repository) SubmitTask(ctx context.Context, p ports.SubmitTaskParams) (ports.SubmitTaskResult, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: begin submit tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var (
		category, parkID, shedID, partitionLabel, status, workState, plannedDate string
		rowVersion                                                               int32
	)
	err = tx.QueryRow(ctx, `
SELECT category, park_id::text, shed_id::text, coalesce(partition_label, ''), status, work_state,
       planned_business_date::text, row_version
FROM pc_care_tasks
WHERE tenant_id = $1::uuid AND task_id = $2::uuid
FOR UPDATE`, p.TenantID, p.TaskID).Scan(
		&category, &parkID, &shedID, &partitionLabel, &status, &workState, &plannedDate, &rowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.SubmitTaskResult{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: lock task for submit: %w", err)
	}

	// The shed's display NAME for the verifier's subject label — carried on EVERY return path
	// (the feed packing declared-but-never-populated lesson).
	shedLocation, err := oploc.ResolveShedLocation(ctx, tx.QueryRow(ctx, oploc.ShedScopedLocationSQL, p.TenantID, shedID))
	if err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: resolve submit shed location: %w", err)
	}

	base := ports.SubmitTaskResult{
		TaskID: p.TaskID, Category: category, ParkID: parkID, ShedID: shedID,
		ShedName: shedLocation.ShedName, PartitionLabel: partitionLabel,
		PlannedBusinessDate: plannedDate,
	}

	fingerprint := requestFingerprint(p.TaskID)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, pcCareSubmitIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.SubmitTaskResult{}, err
	}
	if !reservation.proceed {
		// Exact replay: echo the current state, enqueue nothing.
		if err := tx.Commit(ctx); err != nil {
			return ports.SubmitTaskResult{}, fmt.Errorf("pccare: commit idempotent submit replay: %w", err)
		}
		committed = true
		result := base
		result.Status = status
		result.RowVersion = rowVersion
		return result, nil
	}

	switch status {
	case domain.StatusPendingVerification, domain.StatusCompleted:
		// A different-key re-send of an already-submitted task: the work IS submitted, so this
		// is an idempotent no-op (feed packing same-proof precedent), never an error and never
		// a second enqueue.
		if err := completeIdempotency(ctx, tx, p.TenantID, pcCareSubmitIdemScope, p.IdempotencyKey, pcCareTaskResourceType, p.TaskID); err != nil {
			return ports.SubmitTaskResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ports.SubmitTaskResult{}, fmt.Errorf("pccare: commit already-submitted no-op: %w", err)
		}
		committed = true
		result := base
		result.Status = status
		result.RowVersion = rowVersion
		return result, nil
	case domain.StatusOpen, domain.StatusRework:
		// The submitting states.
	default:
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: unexpected task status %q", status)
	}
	if workState != domain.WorkStateScheduled && workState != domain.WorkStateDelayed {
		return ports.SubmitTaskResult{}, domain.ErrTaskNotOpen
	}

	// Readiness: every scanned animal must carry every slot the category demands. ONE bounded
	// count per submit (a write, not a list), category-aware.
	var animalCount, missingCount int
	if err := tx.QueryRow(ctx, `
SELECT count(*)::int,
       count(*) FILTER (
         WHERE CASE WHEN $3::bool
           THEN video_proof_ref IS NULL
           ELSE before_proof_ref IS NULL OR during_proof_ref IS NULL OR after_proof_ref IS NULL
         END
       )::int
FROM pc_care_task_animals
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`,
		p.TenantID, p.TaskID,
		category == domain.CategoryDeworming || category == domain.CategoryTicksRemoval,
	).Scan(&animalCount, &missingCount); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: submit readiness count: %w", err)
	}
	if animalCount == 0 {
		return ports.SubmitTaskResult{}, domain.ErrNoAnimals
	}
	if missingCount > 0 {
		return ports.SubmitTaskResult{}, domain.ErrProofIncomplete
	}

	if err := tx.QueryRow(ctx, `
UPDATE pc_care_tasks
SET status = 'pending_verification',
    submitted_by = nullif($3::text, '')::uuid,
    submitted_at = now(),
    rework_reason = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND status IN ('open', 'rework')
RETURNING row_version`, p.TenantID, p.TaskID, p.SubmittedBy).Scan(&rowVersion); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: flip task pending: %w", err)
	}

	if _, err := tx.Exec(ctx, `
UPDATE pc_care_task_animals
SET submitted_at = now(), updated_at = now()
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, p.TenantID, p.TaskID); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: stamp animal submits: %w", err)
	}

	mediaRefs, err := r.composeSubmitMediaRefs(ctx, tx, p.TenantID, p.TaskID, category)
	if err != nil {
		return ports.SubmitTaskResult{}, err
	}

	actorType := strings.TrimSpace(p.ActorType)
	if actorType == "" {
		actorType = "operator"
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       pcCarePendingAction,
		ResourceType: pcCareTaskResourceType,
		ResourceID:   p.TaskID,
		ScopeType:    "shed",
		ScopeID:      shedID,
		AfterState: map[string]any{
			"category":              category,
			"park_id":               parkID,
			"shed_id":               shedID,
			"partition_label":       partitionLabel,
			"planned_business_date": plannedDate,
			"status":                domain.StatusPendingVerification,
			"animal_count":          animalCount,
		},
		Metadata: map[string]any{"source": "pc-care-submit"},
		TraceID:  p.TraceID,
	}); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: write submit audit: %w", err)
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, pcCareSubmitIdemScope, p.IdempotencyKey, pcCareTaskResourceType, p.TaskID); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: complete submit idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.SubmitTaskResult{}, fmt.Errorf("pccare: commit submit: %w", err)
	}
	committed = true

	result := base
	result.Status = domain.StatusPendingVerification
	result.RowVersion = rowVersion
	result.NewlyPending = true
	result.MediaRefs = mediaRefs
	result.AnimalCount = int32(animalCount)
	return result, nil
}

// composeSubmitMediaRefs builds the verification item's labeled media set: every animal's slot
// videos in scan order then slot order, each labeled "<tag> · <slot label>" so the verifier
// can tell which animal and which step every clip proves. One bounded read of one task's rows.
func (r *Repository) composeSubmitMediaRefs(ctx context.Context, tx pgx.Tx, tenantID, taskID, category string) ([]ports.LabeledRef, error) {
	rows, err := tx.Query(ctx, `
SELECT scanned_identifier,
       coalesce(video_proof_ref, ''), coalesce(before_proof_ref, ''),
       coalesce(during_proof_ref, ''), coalesce(after_proof_ref, '')
FROM pc_care_task_animals
WHERE tenant_id = $1::uuid AND task_id = $2::uuid
ORDER BY scanned_at, animal_row_id`, tenantID, taskID)
	if err != nil {
		return nil, fmt.Errorf("pccare: read submit media refs: %w", err)
	}
	defer rows.Close()

	slots := domain.SlotsForCategory(category)
	out := make([]ports.LabeledRef, 0, 64)
	for rows.Next() {
		var tag, videoRef, beforeRef, duringRef, afterRef string
		if err := rows.Scan(&tag, &videoRef, &beforeRef, &duringRef, &afterRef); err != nil {
			return nil, fmt.Errorf("pccare: scan submit media row: %w", err)
		}
		byKey := map[string]string{
			domain.SlotVideo:  videoRef,
			domain.SlotBefore: beforeRef,
			domain.SlotDuring: duringRef,
			domain.SlotAfter:  afterRef,
		}
		for _, slot := range slots {
			ref := byKey[slot.FieldKey]
			if ref == "" {
				continue
			}
			out = append(out, ports.LabeledRef{ProofRef: ref, Label: tag + " · " + slot.Label})
		}
	}
	return out, rows.Err()
}
