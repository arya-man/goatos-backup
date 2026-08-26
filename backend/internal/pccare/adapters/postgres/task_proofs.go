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
)

const pcCareTaskProofAction = "pc_care.task.proof_recorded"

// RegisterTaskProof stores one task-level proof ref. It is currently used by
// inventory_vaccine, where the expected proof is a fridge-stock video/photo for
// the whole task rather than a per-animal clip.
func (r *Repository) RegisterTaskProof(ctx context.Context, p ports.RegisterTaskProofParams) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	if !domain.IsValidSlotForCategory(domain.CategoryInventoryVaccine, strings.TrimSpace(p.SlotKey)) {
		return domain.ErrInvalidSlotForCategory
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("pccare: begin task proof tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	category, err := lockTaskForCapture(ctx, tx, p.TenantID, p.TaskID)
	if err != nil {
		return err
	}
	if category != domain.CategoryInventoryVaccine {
		return domain.ErrInvalidSlotForCategory
	}

	fingerprint := requestFingerprint(p.TaskID, p.SlotKey, p.ProofRef)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, pcCareSlotIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("pccare: commit idempotent task proof replay: %w", err)
		}
		committed = true
		return nil
	}

	var previousRef string
	err = tx.QueryRow(ctx, `
SELECT coalesce(proof_ref, '')
FROM pc_care_task_proofs
WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND slot_key = $3
FOR UPDATE`, p.TenantID, p.TaskID, p.SlotKey).Scan(&previousRef)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("pccare: lock task proof: %w", err)
	}

	if previousRef != p.ProofRef {
		if _, err := tx.Exec(ctx, `
INSERT INTO pc_care_task_proofs (
  tenant_id, task_id, slot_key, proof_ref, captured_by, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5::uuid, $6
)
ON CONFLICT (tenant_id, task_id, slot_key)
DO UPDATE SET
  proof_ref = EXCLUDED.proof_ref,
  captured_by = EXCLUDED.captured_by,
  captured_at = now(),
  idempotency_key = EXCLUDED.idempotency_key,
  updated_at = now()`,
			p.TenantID, p.TaskID, p.SlotKey, p.ProofRef, p.CapturedBy, p.IdempotencyKey); err != nil {
			return fmt.Errorf("pccare: upsert task proof: %w", err)
		}
		actorType := strings.TrimSpace(p.ActorType)
		if actorType == "" {
			actorType = "operator"
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     p.TenantID,
			ActorID:      p.ActorID,
			ActorType:    actorType,
			Action:       pcCareTaskProofAction,
			ResourceType: pcCareTaskResourceType,
			ResourceID:   p.TaskID,
			ScopeType:    "task",
			ScopeID:      p.TaskID,
			AfterState: map[string]any{
				"task_id":            p.TaskID,
				"slot":               p.SlotKey,
				"proof_ref":          p.ProofRef,
				"replaced_proof_ref": previousRef,
			},
			Metadata: map[string]any{"source": "pc-care-task-proof"},
			TraceID:  p.TraceID,
		}); err != nil {
			return fmt.Errorf("pccare: write task proof audit: %w", err)
		}
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, pcCareSlotIdemScope, p.IdempotencyKey, pcCareTaskResourceType, p.TaskID); err != nil {
		return fmt.Errorf("pccare: complete task proof idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pccare: commit task proof: %w", err)
	}
	committed = true
	return nil
}

// ListTaskProofs reads task-level proofs with recorder names, bounded by the
// category's small slot set.
func (r *Repository) ListTaskProofs(ctx context.Context, tenantID, taskID string) ([]ports.TaskProofRow, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx, `
SELECT p.slot_key, p.proof_ref, p.captured_by::text, coalesce(m.display_name, ''), p.captured_at
FROM pc_care_task_proofs p
LEFT JOIN workforce_members m
  ON m.tenant_id = p.tenant_id AND m.user_id = p.captured_by AND m.status = 'active'
WHERE p.tenant_id = $1::uuid AND p.task_id = $2::uuid
ORDER BY p.slot_key`, tenantID, taskID)
	if err != nil {
		return nil, fmt.Errorf("pccare: list task proofs: %w", err)
	}
	defer rows.Close()

	out := []ports.TaskProofRow{}
	for rows.Next() {
		var p ports.TaskProofRow
		if err := rows.Scan(&p.SlotKey, &p.ProofRef, &p.CapturedBy, &p.CapturedByName, &p.CapturedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) inventoryTaskProofMediaRefs(ctx context.Context, tx pgx.Tx, tenantID, taskID string) ([]ports.LabeledRef, int, error) {
	rows, err := tx.Query(ctx, `
SELECT slot_key, proof_ref
FROM pc_care_task_proofs
WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND slot_key = ANY($3)`,
		tenantID, taskID, []string{domain.SlotStockFridgePhoto, domain.SlotStockFridgeVideo})
	if err != nil {
		return nil, 0, fmt.Errorf("pccare: read inventory task proof: %w", err)
	}
	defer rows.Close()
	bySlot := map[string]string{}
	for rows.Next() {
		var slotKey, proofRef string
		if err := rows.Scan(&slotKey, &proofRef); err != nil {
			return nil, 0, fmt.Errorf("pccare: scan inventory task proof: %w", err)
		}
		bySlot[slotKey] = proofRef
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("pccare: iterate inventory task proof: %w", err)
	}
	out := make([]ports.LabeledRef, 0, len(domain.SlotsForCategory(domain.CategoryInventoryVaccine)))
	for _, slot := range domain.SlotsForCategory(domain.CategoryInventoryVaccine) {
		proofRef := strings.TrimSpace(bySlot[slot.FieldKey])
		if proofRef == "" {
			return nil, 0, domain.ErrProofIncomplete
		}
		out = append(out, ports.LabeledRef{ProofRef: proofRef, Label: slot.Label})
	}
	return out, 0, nil
}
