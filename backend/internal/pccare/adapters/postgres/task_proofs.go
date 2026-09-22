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
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

const pcCareTaskProofAction = "pc_care.task.proof_recorded"

// RegisterTaskProof stores one task-level proof ref for the task-proof capture-mode
// categories: inventory_vaccine's fridge-stock evidence and feed_water_removal's two removal
// videos — whole-task proof rather than a per-animal clip.
func (r *Repository) RegisterTaskProof(ctx context.Context, p ports.RegisterTaskProofParams) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

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

	// Slot validity is judged against the TASK's own card, under the capture lock — never
	// against a hardcoded category, which would misfile one category's slot into another.
	// inventory_vaccine is NOT authored (the fridge photo + video are the kernel's own stock
	// check) so it keeps the fixed pair; a removal card's slots are the PINNED card's, judged by
	// the service and re-checked here against the task's own snapshot.
	category, slotKeys, err := lockTaskForCaptureWithSlots(ctx, tx, p.TenantID, p.TaskID)
	if err != nil {
		return err
	}
	if domain.CaptureModeForCategory(category) != domain.CaptureModeTaskProof {
		return domain.ErrInvalidSlotForCategory
	}
	if category == domain.CategoryInventoryVaccine {
		if !domain.IsValidSlotForCategory(category, strings.TrimSpace(p.SlotKey)) {
			return domain.ErrInvalidSlotForCategory
		}
	} else if !acceptsSlot(category, slotKeys, strings.TrimSpace(p.SlotKey)) {
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

// taskProofMediaRefs composes the labeled media set for a task-proof capture-mode submit:
// EVERY slot the category declares must carry a proof (a missing one is ErrProofIncomplete),
// returned in the category's declared slot order.
func (r *Repository) taskProofMediaRefs(ctx context.Context, tx pgx.Tx, tenantID, taskID, category string, removalSlots []authored.ProofSlot) ([]ports.LabeledRef, int, error) {
	// A single-task removal card runs the PINNED card's slots (PC CARE SOP, 2026-09-22);
	// inventory_vaccine's fridge pair is the kernel's own and is not authored.
	slots := make([]domain.Slot, 0, len(removalSlots))
	if category == domain.CategoryInventoryVaccine {
		slots = domain.SlotsForCategory(domain.CategoryInventoryVaccine)
	} else {
		for _, p := range removalSlots {
			slots = append(slots, domain.Slot{FieldKey: p.Key, Label: p.Title, Description: p.Hint, Kind: p.Kind, Required: p.Required})
		}
		if len(slots) == 0 {
			// A caller that passed no card (a kernel write, a fixture) runs the SEEDED removal
			// card -- version 0 -- never a card that demands nothing.
			slots = seededRemovalSlots()
		}
	}
	slotKeys := make([]string, 0, len(slots))
	for _, slot := range slots {
		slotKeys = append(slotKeys, slot.FieldKey)
	}
	rows, err := tx.Query(ctx, `
SELECT slot_key, proof_ref
FROM pc_care_task_proofs
WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND slot_key = ANY($3)`,
		tenantID, taskID, slotKeys)
	if err != nil {
		return nil, 0, fmt.Errorf("pccare: read task proof: %w", err)
	}
	defer rows.Close()
	bySlot := map[string]string{}
	for rows.Next() {
		var slotKey, proofRef string
		if err := rows.Scan(&slotKey, &proofRef); err != nil {
			return nil, 0, fmt.Errorf("pccare: scan task proof: %w", err)
		}
		bySlot[slotKey] = proofRef
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("pccare: iterate task proof: %w", err)
	}
	out := make([]ports.LabeledRef, 0, len(slots))
	for _, slot := range slots {
		proofRef := strings.TrimSpace(bySlot[slot.FieldKey])
		if proofRef == "" {
			if slot.Required {
				return nil, 0, domain.ErrProofIncomplete
			}
			continue
		}
		kind := slot.Kind
		if kind == authored.KindEither {
			kind = ""
		}
		out = append(out, ports.LabeledRef{ProofRef: proofRef, Label: slot.Label, Kind: kind})
	}
	return out, 0, nil
}
