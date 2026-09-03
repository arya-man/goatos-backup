package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
)

const milkPreparationResourceType = "milk_preparation_completion"

// SubmitMilkPreparation stores a new immutable proof attempt and moves the farm-day preparation to
// pending_verification in one transaction. Exact idempotency replay returns NeedsEnqueue=true while
// the same attempt is pending, allowing the caller to heal a prior verifier-queue enqueue failure.
func (r *Repository) SubmitMilkPreparation(ctx context.Context, in domain.MilkPreparationSubmission) (domain.MilkPreparationSubmissionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	steps := in.Proofs.OrderedStepProofs(in.GoatMilkUsed)
	proofMap := make(map[string]string, len(steps))
	for _, step := range steps {
		proofMap[step.StepCode] = step.ProofRef
	}
	proofJSON, err := json.Marshal(proofMap)
	if err != nil {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: marshal milk preparation proofs: %w", err)
	}
	answersJSON, err := json.Marshal(in.Answers)
	if err != nil {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: marshal milk preparation answers: %w", err)
	}
	fingerprint := milkPreparationFingerprint(in, steps)
	preparationDate := in.PreparationDate.Format("2006-01-02")
	feedingDate := in.FeedingDate.Format("2006-01-02")

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: begin milk preparation submission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Idempotency is attempt-grained. An exact replay may safely re-run only the idempotent generic
	// verification enqueue; it never inserts another proof attempt or audit row.
	var replay domain.MilkPreparationSubmissionResult
	var storedFingerprint string
	err = tx.QueryRow(ctx, `
SELECT a.request_fingerprint, c.completion_id::text, c.status, a.attempt_no, c.row_version
FROM milk_preparation_proof_attempts a
JOIN milk_preparation_completions c
  ON c.tenant_id = a.tenant_id AND c.completion_id = a.completion_id
WHERE a.tenant_id = $1::uuid AND a.idempotency_key = $2`,
		in.TenantID, in.IdempotencyKey).Scan(
		&storedFingerprint, &replay.CompletionID, &replay.Status, &replay.AttemptNo, &replay.RowVersion)
	if err == nil {
		if storedFingerprint != fingerprint {
			return domain.MilkPreparationSubmissionResult{}, ports.ErrIdempotencyConflict
		}
		replay.NeedsEnqueue = replay.Status == domain.MilkPreparationVerificationPending
		if err := tx.Commit(ctx); err != nil {
			return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: commit milk preparation replay: %w", err)
		}
		return replay, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: read milk preparation idempotency: %w", err)
	}

	var result domain.MilkPreparationSubmissionResult
	var currentStatus string
	err = tx.QueryRow(ctx, `
SELECT completion_id::text, status, current_attempt_no, row_version
FROM milk_preparation_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id IS NULL
  AND preparation_date = $3::date AND status <> 'retired'
FOR UPDATE`, in.TenantID, in.ParkID, preparationDate).
		Scan(&result.CompletionID, &currentStatus, &result.AttemptNo, &result.RowVersion)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		result.AttemptNo = 1
		err = tx.QueryRow(ctx, `
INSERT INTO milk_preparation_completions (
  tenant_id, park_id, shed_id, preparation_date, feeding_date, status, current_attempt_no,
  goat_milk_used, submitted_by, submitted_at
) SELECT $1::uuid, p.location_id, NULL, $3::date, $4::date,
         'pending_verification', 1, $5, $6::uuid, $7
FROM locations p
WHERE p.tenant_id = $1::uuid AND p.location_id = $2::uuid
  AND p.location_type = 'park' AND p.status = 'active'
RETURNING completion_id::text, status, current_attempt_no, row_version`,
			in.TenantID, in.ParkID, preparationDate, feedingDate, in.GoatMilkUsed,
			in.SubmittedBy, in.SubmittedAt.UTC()).
			Scan(&result.CompletionID, &result.Status, &result.AttemptNo, &result.RowVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: active milk preparation farm not found")
		}
	case err != nil:
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: lock milk preparation: %w", err)
	case currentStatus == domain.MilkPreparationVerificationPending:
		return domain.MilkPreparationSubmissionResult{}, ports.ErrMilkPreparationPending
	case currentStatus == domain.MilkPreparationVerificationCompleted:
		return domain.MilkPreparationSubmissionResult{}, ports.ErrMilkPreparationCompleted
	case currentStatus == domain.MilkPreparationVerificationRework:
		result.AttemptNo++
		err = tx.QueryRow(ctx, `
UPDATE milk_preparation_completions
SET status = 'pending_verification', current_attempt_no = $4, goat_milk_used = $5,
    submitted_by = $6::uuid, submitted_at = $7, verified_by = NULL, verified_at = NULL,
    rework_reason = NULL, row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND preparation_date = $3::date
RETURNING status, row_version`, in.TenantID, result.CompletionID, preparationDate,
			result.AttemptNo, in.GoatMilkUsed, in.SubmittedBy, in.SubmittedAt.UTC()).
			Scan(&result.Status, &result.RowVersion)
	default:
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: unsupported milk preparation status %q", currentStatus)
	}
	if err != nil {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: persist milk preparation: %w", err)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO milk_preparation_proof_attempts (
  tenant_id, completion_id, attempt_no, goat_milk_used, proof_refs, answers,
  submitted_by, submitted_at, idempotency_key, request_fingerprint
) VALUES ($1::uuid, $2::uuid, $3, $4, $5::jsonb, $6::jsonb, $7::uuid, $8, $9, $10)`,
		in.TenantID, result.CompletionID, result.AttemptNo, in.GoatMilkUsed, proofJSON, answersJSON,
		in.SubmittedBy, in.SubmittedAt.UTC(), in.IdempotencyKey, fingerprint); err != nil {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: insert milk preparation proof attempt: %w", err)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID: in.TenantID, ActorID: in.SubmittedBy, ActorType: "operator",
		Action: "milk.preparation.pending_verification", ResourceType: milkPreparationResourceType,
		ResourceID: result.CompletionID, ScopeType: "park", ScopeID: in.ParkID,
		AfterState: map[string]any{"preparation_date": preparationDate, "feeding_date": feedingDate,
			"status": domain.MilkPreparationVerificationPending, "attempt_no": result.AttemptNo,
			"step_count": len(steps)},
		Metadata: map[string]any{"source": "milk-preparation", "park_id": in.ParkID, "proof_steps": proofMap}, TraceID: in.TraceID,
	}); err != nil {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: audit milk preparation submission: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: commit milk preparation submission: %w", err)
	}
	result.NeedsEnqueue = true
	return result, nil
}

func milkPreparationFingerprint(in domain.MilkPreparationSubmission, steps []domain.MilkPreparationStepProof) string {
	answerJSON, _ := json.Marshal(in.Answers)
	parts := []string{in.ParkID, in.PreparationDate.Format("2006-01-02"), fmt.Sprintf("%t", in.GoatMilkUsed), string(answerJSON)}
	for _, step := range steps {
		parts = append(parts, step.StepCode, step.ProofRef)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (r *Repository) ApplyVerifiedMilkPreparation(ctx context.Context, in domain.MilkPreparationVerdictCommand) (bool, error) {
	return r.transitionMilkPreparationVerdict(ctx, in, domain.MilkPreparationVerificationCompleted)
}

func (r *Repository) BounceMilkPreparationForRework(ctx context.Context, in domain.MilkPreparationVerdictCommand) (bool, error) {
	return r.transitionMilkPreparationVerdict(ctx, in, domain.MilkPreparationVerificationRework)
}

func (r *Repository) transitionMilkPreparationVerdict(ctx context.Context, in domain.MilkPreparationVerdictCommand, target string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("counts: begin milk preparation verdict: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var parkID, preparationDate string
	var attemptNo int32
	err = tx.QueryRow(ctx, `
UPDATE milk_preparation_completions
SET status = $3,
    verified_by = CASE WHEN $3 = 'completed' THEN nullif($4::text, '')::uuid ELSE NULL END,
    verified_at = CASE WHEN $3 = 'completed' THEN $5::timestamptz ELSE NULL END,
    rework_reason = CASE WHEN $3 = 'rework' THEN nullif($6, '') ELSE NULL END,
    row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid AND completion_id = $2::uuid AND status = 'pending_verification'
RETURNING park_id::text, preparation_date::text, current_attempt_no`,
		in.TenantID, in.CompletionID, target, in.VerifiedBy, in.OccurredAt.UTC(), strings.TrimSpace(in.Reason)).
		Scan(&parkID, &preparationDate, &attemptNo)
	if errors.Is(err, pgx.ErrNoRows) {
		// Verdict events are at-least-once. A stale duplicate is a successful no-op when the row exists.
		var exists bool
		if readErr := tx.QueryRow(ctx, `SELECT EXISTS (
  SELECT 1 FROM milk_preparation_completions WHERE tenant_id = $1::uuid AND completion_id = $2::uuid
)`, in.TenantID, in.CompletionID).Scan(&exists); readErr != nil {
			return false, fmt.Errorf("counts: read milk preparation verdict target: %w", readErr)
		}
		if !exists {
			return false, ports.ErrMilkPreparationNotFound
		}
		if err := tx.Commit(ctx); err != nil {
			return false, fmt.Errorf("counts: commit milk preparation verdict replay: %w", err)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("counts: transition milk preparation verdict: %w", err)
	}
	action := "milk.preparation.completed"
	if target == domain.MilkPreparationVerificationRework {
		action = "milk.preparation.rework"
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID: in.TenantID, ActorID: in.VerifiedBy, ActorType: "verifier", Action: action,
		ResourceType: milkPreparationResourceType, ResourceID: in.CompletionID,
		ScopeType: "park", ScopeID: parkID,
		AfterState: map[string]any{"preparation_date": preparationDate, "status": target,
			"attempt_no": attemptNo, "reason": strings.TrimSpace(in.Reason)},
		Metadata: map[string]any{"source": "milk-preparation-verification", "park_id": parkID}, TraceID: in.TraceID,
	}); err != nil {
		return false, fmt.Errorf("counts: audit milk preparation verdict: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("counts: commit milk preparation verdict: %w", err)
	}
	return true, nil
}
