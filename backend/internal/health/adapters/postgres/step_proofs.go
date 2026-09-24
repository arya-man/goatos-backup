package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
)

// ONE VIDEO PER TREATMENT STEP (maintainer decision 2026-09-23). See domain/step_proofs.go.

const sqlStepBelongsToSession = `
SELECT hs.status
FROM health_session_steps ss
JOIN health_treatment_sessions hs
  ON hs.tenant_id = ss.tenant_id AND hs.health_session_id = ss.health_session_id
WHERE ss.tenant_id = $1::uuid AND ss.health_session_id = $2::uuid AND ss.health_session_step_id = $3::uuid
FOR UPDATE OF hs`

const sqlInsertStepProofAttempt = `
INSERT INTO health_session_step_proof_attempts (
  tenant_id, health_session_id, health_session_step_id, idempotency_key,
  request_fingerprint, proof_ref, captured_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7::uuid)
ON CONFLICT (tenant_id, health_session_step_id, idempotency_key) DO NOTHING
RETURNING health_session_step_id::text, proof_ref, captured_by::text, captured_at`

const sqlGetStepProofAttempt = `
SELECT health_session_step_id::text, proof_ref, captured_by::text, captured_at, request_fingerprint
FROM health_session_step_proof_attempts
WHERE tenant_id = $1::uuid AND health_session_step_id = $2::uuid AND idempotency_key = $3`

// A re-shoot REPLACES the step's clip. The verifier receives exactly one video per step, never a
// pile of attempts, and the idempotency key is refreshed so a retry of the NEW capture collapses
// onto the new row rather than the one it replaced.
const sqlUpsertStepProof = `
INSERT INTO health_session_step_proofs (
  tenant_id, health_session_id, health_session_step_id, proof_ref, captured_by, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5::uuid, $6)
ON CONFLICT (tenant_id, health_session_step_id) DO UPDATE
SET proof_ref = EXCLUDED.proof_ref,
    captured_by = EXCLUDED.captured_by,
    captured_at = now(),
    idempotency_key = EXCLUDED.idempotency_key,
    updated_at = now()
RETURNING health_session_step_id::text, proof_ref, captured_by::text, captured_at`

const sqlListStepProofs = `
SELECT p.health_session_step_id::text, p.proof_ref, p.captured_by::text, p.captured_at
FROM health_session_step_proofs p
JOIN health_session_steps s
  ON s.tenant_id = p.tenant_id AND s.health_session_step_id = p.health_session_step_id
WHERE p.tenant_id = $1::uuid AND p.health_session_id = $2::uuid
ORDER BY s.seq`

// RecordStepProof attaches ONE video to ONE step.
//
// It is its own write, separate from submitting the session, because the proof business-ack
// contract says a blob reaching storage is not the business fact. THIS is the business fact: it
// retries on its own, and if it fails after the upload succeeded only this small write is retried
// -- the video is never re-uploaded to repair the link.
func (r *Repository) RecordStepProof(ctx context.Context, in domain.RecordStepProofInput) (domain.StepProof, error) {
	if err := in.Validate(); err != nil {
		return domain.StepProof{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.StepProof{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// The step must belong to THIS session. Refused rather than ignored: a caller that could
	// attach a video to another session's step could file evidence of one animal's treatment
	// onto another animal's card.
	var status string
	err = tx.QueryRow(ctx, sqlStepBelongsToSession, in.TenantID, in.SessionID, in.StepID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StepProof{}, domain.ErrStepNotInSession
	}
	if err != nil {
		return domain.StepProof{}, fmt.Errorf("health: resolve session step: %w", err)
	}
	out, replayed, err := replayStepProofAttempt(ctx, tx, in)
	if err != nil {
		return domain.StepProof{}, err
	}
	if replayed {
		if err := tx.Commit(ctx); err != nil {
			return domain.StepProof{}, err
		}
		committed = true
		return out, nil
	}
	switch status {
	case "completed", "held_death_review", "canceled_death", "canceled":
		return domain.StepProof{}, domain.ErrStepProofClosed
	}

	err = tx.QueryRow(ctx, sqlInsertStepProofAttempt,
		in.TenantID, in.SessionID, in.StepID, in.IdempotencyKey, in.RequestFingerprint, in.ProofRef, in.ActorID,
	).Scan(&out.StepID, &out.ProofRef, &out.CapturedBy, &out.CapturedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		out, replayed, err = replayStepProofAttempt(ctx, tx, in)
		if err != nil {
			return domain.StepProof{}, err
		}
		if !replayed {
			return domain.StepProof{}, fmt.Errorf("health: replay step proof: %w", pgx.ErrNoRows)
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.StepProof{}, err
		}
		committed = true
		return out, nil
	}
	if err != nil {
		return domain.StepProof{}, fmt.Errorf("health: record step proof attempt: %w", err)
	}

	if err := tx.QueryRow(ctx, sqlUpsertStepProof,
		in.TenantID, in.SessionID, in.StepID, in.ProofRef, in.ActorID, in.IdempotencyKey,
	).Scan(&out.StepID, &out.ProofRef, &out.CapturedBy, &out.CapturedAt); err != nil {
		return domain.StepProof{}, fmt.Errorf("health: record step proof: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.StepProof{}, err
	}
	committed = true
	return out, nil
}

func replayStepProofAttempt(ctx context.Context, tx pgx.Tx, in domain.RecordStepProofInput) (domain.StepProof, bool, error) {
	var out domain.StepProof
	var priorFingerprint string
	err := tx.QueryRow(ctx, sqlGetStepProofAttempt,
		in.TenantID, in.StepID, in.IdempotencyKey,
	).Scan(&out.StepID, &out.ProofRef, &out.CapturedBy, &out.CapturedAt, &priorFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StepProof{}, false, nil
	}
	if err != nil {
		return domain.StepProof{}, false, fmt.Errorf("health: replay step proof: %w", err)
	}
	if priorFingerprint != in.RequestFingerprint {
		return domain.StepProof{}, false, ports.ErrConflict
	}
	return out, true, nil
}

// StepProofs returns every clip recorded for this session, IN STEP ORDER.
//
// The order is the verifier's: she receives one item holding the whole set and steps through it,
// so the clips must arrive in the order the work was done rather than the order they were filmed.
func (r *Repository) StepProofs(ctx context.Context, tenantID, sessionID string) ([]domain.StepProof, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlListStepProofs, tenantID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("health: list step proofs: %w", err)
	}
	defer rows.Close()
	out := []domain.StepProof{}
	for rows.Next() {
		var p domain.StepProof
		if err := rows.Scan(&p.StepID, &p.ProofRef, &p.CapturedBy, &p.CapturedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// stepProofsInTx and sessionStepsInTx read inside the completion's own transaction, under the
// same row lock, so a capture landing between the check and the write cannot let a session
// through half-filmed.
func stepProofsInTx(ctx context.Context, tx pgx.Tx, tenantID, sessionID string) ([]domain.StepProof, error) {
	rows, err := tx.Query(ctx, sqlListStepProofs, tenantID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("health: read step proofs: %w", err)
	}
	defer rows.Close()
	out := []domain.StepProof{}
	for rows.Next() {
		var p domain.StepProof
		if err := rows.Scan(&p.StepID, &p.ProofRef, &p.CapturedBy, &p.CapturedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const sqlSessionStepsForProof = `
SELECT health_session_step_id::text, seq, record_type, medicine_name, dosage_text,
       dosage_denominator, medicine_route, instruction
FROM health_session_steps
WHERE tenant_id = $1::uuid AND health_session_id = $2::uuid
ORDER BY seq`

func sessionStepsInTx(ctx context.Context, tx pgx.Tx, tenantID, sessionID string) ([]domain.ProtocolStep, error) {
	rows, err := tx.Query(ctx, sqlSessionStepsForProof, tenantID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("health: read session steps: %w", err)
	}
	defer rows.Close()
	out := []domain.ProtocolStep{}
	for rows.Next() {
		var s domain.ProtocolStep
		if err := rows.Scan(&s.StepID, &s.Seq, &s.RecordType, &s.MedicineName, &s.DosageText,
			&s.DosageDenominator, &s.MedicineRoute, &s.Instruction); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// stepMediaFor pairs each clip with the NAME of the step it proves, in step order.
//
// The verifier opens one item holding the whole set; without a name per clip she cannot tell
// which injection each video is, which is the only thing that makes a twelve-clip item reviewable.
func stepMediaFor(steps []domain.ProtocolStep, proofs []domain.StepProof) []domain.StepMedia {
	byStep := make(map[string]domain.StepProof, len(proofs))
	for _, p := range proofs {
		byStep[p.StepID] = p
	}
	out := make([]domain.StepMedia, 0, len(proofs))
	for _, s := range steps {
		p, ok := byStep[s.StepID]
		if !ok {
			continue
		}
		out = append(out, domain.StepMedia{
			StepID:   s.StepID,
			Label:    domain.StepLabel(s),
			ProofRef: p.ProofRef,
		})
	}
	return out
}

var _ = ports.ErrNotFound
