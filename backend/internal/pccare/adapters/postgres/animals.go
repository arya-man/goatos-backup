package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
)

const (
	pcCareScanIdemScope   = "pc_care.animal.scan"
	pcCareSlotIdemScope   = "pc_care.animal.slot"
	pcCareSubmitIdemScope = "pc_care.task.submit"

	pcCareScannedAction  = "pc_care.animal.scanned"
	pcCareSlotAction     = "pc_care.animal.slot_recorded"
	pcCarePendingAction  = "pc_care.task.pending_verification"
	pcCareAnimalResource = "pc_care_task_animal"
)

// lockTaskForCapture locks the task row and asserts it is open for capture writes: status
// open|rework and work_state scheduled|delayed. Returns the task's category.
func lockTaskForCapture(ctx context.Context, tx pgx.Tx, tenantID, taskID string) (category string, err error) {
	var status, workState string
	err = tx.QueryRow(ctx, `
SELECT category, status, work_state
FROM pc_care_tasks
WHERE tenant_id = $1::uuid AND task_id = $2::uuid
FOR UPDATE`, tenantID, taskID).Scan(&category, &status, &workState)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("pccare: lock task: %w", err)
	}
	if (status != domain.StatusOpen && status != domain.StatusRework) ||
		(workState != domain.WorkStateScheduled && workState != domain.WorkStateDelayed) {
		return "", domain.ErrTaskNotOpen
	}
	return category, nil
}

// ScanAnimal inserts one scanned tag into the task, VERBATIM, at scan time. The ONE business
// rule — no duplicate tag in the same task — is the partial-free unique index
// pc_care_task_animals_tag_uidx; its 23505 is translated to domain.ErrDuplicateScan (the
// weighing 000073 translation pattern).
func (r *Repository) ScanAnimal(ctx context.Context, p ports.ScanAnimalParams) (ports.ScanAnimalResult, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	tag := strings.TrimSpace(p.ScannedIdentifier)

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.ScanAnimalResult{}, fmt.Errorf("pccare: begin scan tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := lockTaskForCapture(ctx, tx, p.TenantID, p.TaskID); err != nil {
		return ports.ScanAnimalResult{}, err
	}

	fingerprint := requestFingerprint(p.TaskID, domain.PartitionMatchKey(tag))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, pcCareScanIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.ScanAnimalResult{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return ports.ScanAnimalResult{}, fmt.Errorf("pccare: commit idempotent scan replay: %w", err)
		}
		committed = true
		return ports.ScanAnimalResult{AnimalRowID: reservation.resultID, Replayed: true}, nil
	}

	var animalRowID string
	err = tx.QueryRow(ctx, `
INSERT INTO pc_care_task_animals (tenant_id, task_id, scanned_identifier, scanned_by, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5)
RETURNING animal_row_id::text`,
		p.TenantID, p.TaskID, tag, p.ScannedBy, p.IdempotencyKey).Scan(&animalRowID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "pc_care_task_animals_tag_uidx" {
			return ports.ScanAnimalResult{}, domain.ErrDuplicateScan
		}
		return ports.ScanAnimalResult{}, fmt.Errorf("pccare: insert scan: %w", err)
	}

	actorType := strings.TrimSpace(p.ActorType)
	if actorType == "" {
		actorType = "operator"
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       pcCareScannedAction,
		ResourceType: pcCareAnimalResource,
		ResourceID:   animalRowID,
		ScopeType:    "task",
		ScopeID:      p.TaskID,
		AfterState: map[string]any{
			"task_id": p.TaskID,
			// Goat identifiers are operational livestock data, not PII: log the tag so a
			// failure is traceable to the exact animal row.
			"scanned_identifier": tag,
		},
		Metadata: map[string]any{"source": "pc-care-scan"},
		TraceID:  p.TraceID,
	}); err != nil {
		return ports.ScanAnimalResult{}, fmt.Errorf("pccare: write scan audit: %w", err)
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, pcCareScanIdemScope, p.IdempotencyKey, pcCareAnimalResource, animalRowID); err != nil {
		return ports.ScanAnimalResult{}, fmt.Errorf("pccare: complete scan idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.ScanAnimalResult{}, fmt.Errorf("pccare: commit scan: %w", err)
	}
	committed = true
	return ports.ScanAnimalResult{AnimalRowID: animalRowID}, nil
}

// slotColumn maps a slot field key to its column triple prefix. The map is the ONLY place the
// four column names are spelled, so a slot cannot address another slot's columns.
func slotColumn(fieldKey string) (prefix string, ok bool) {
	switch fieldKey {
	case domain.SlotVideo:
		return "video", true
	case domain.SlotBefore:
		return "before", true
	case domain.SlotDuring:
		return "during", true
	case domain.SlotAfter:
		return "after", true
	}
	return "", false
}

// RegisterSlotProof stores one slot's video ref + attribution on one animal row. While the
// task is open a re-record REPLACES the slot (the phone's capture pipeline retires the old
// clip only after the new one is SYNCED — "Manohar ordering" — so the replacement here is the
// durable half of that flow); a same-ref re-send is an idempotent no-op. A locked task
// (pending_verification/terminal) refuses the write upstream in lockTaskForCapture.
func (r *Repository) RegisterSlotProof(ctx context.Context, p ports.RegisterSlotProofParams) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	prefix, ok := slotColumn(p.SlotFieldKey)
	if !ok {
		return domain.ErrInvalidSlotForCategory
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("pccare: begin slot tx: %w", err)
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
	if !domain.IsValidSlotForCategory(category, p.SlotFieldKey) {
		return domain.ErrInvalidSlotForCategory
	}

	fingerprint := requestFingerprint(p.TaskID, p.AnimalRowID, p.SlotFieldKey, p.ProofRef)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, pcCareSlotIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("pccare: commit idempotent slot replay: %w", err)
		}
		committed = true
		return nil
	}

	// The column names are compiled from the slotColumn map above, never from request input.
	var previousRef string
	err = tx.QueryRow(ctx, `
SELECT coalesce(`+prefix+`_proof_ref, '')
FROM pc_care_task_animals
WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND animal_row_id = $3::uuid
FOR UPDATE`, p.TenantID, p.TaskID, p.AnimalRowID).Scan(&previousRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("pccare: lock animal row: %w", err)
	}

	if previousRef != p.ProofRef {
		if _, err := tx.Exec(ctx, `
UPDATE pc_care_task_animals
SET `+prefix+`_proof_ref = $4,
    `+prefix+`_captured_by = $5::uuid,
    `+prefix+`_captured_at = now(),
    updated_at = now()
WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND animal_row_id = $3::uuid`,
			p.TenantID, p.TaskID, p.AnimalRowID, p.ProofRef, p.CapturedBy); err != nil {
			return fmt.Errorf("pccare: register slot proof: %w", err)
		}
		actorType := strings.TrimSpace(p.ActorType)
		if actorType == "" {
			actorType = "operator"
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     p.TenantID,
			ActorID:      p.ActorID,
			ActorType:    actorType,
			Action:       pcCareSlotAction,
			ResourceType: pcCareAnimalResource,
			ResourceID:   p.AnimalRowID,
			ScopeType:    "task",
			ScopeID:      p.TaskID,
			AfterState: map[string]any{
				"task_id":   p.TaskID,
				"slot":      p.SlotFieldKey,
				"proof_ref": p.ProofRef,
				// The replaced ref, when there was one — the audit is what says a re-record
				// happened and which clip it superseded.
				"replaced_proof_ref": previousRef,
			},
			Metadata: map[string]any{"source": "pc-care-slot-proof"},
			TraceID:  p.TraceID,
		}); err != nil {
			return fmt.Errorf("pccare: write slot audit: %w", err)
		}
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, pcCareSlotIdemScope, p.IdempotencyKey, pcCareAnimalResource, p.AnimalRowID); err != nil {
		return fmt.Errorf("pccare: complete slot idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("pccare: commit slot proof: %w", err)
	}
	committed = true
	return nil
}

// animalScanRow is the raw scan of one pc_care_task_animals row.
type animalScanRow struct {
	animalRowID string
	tag         string
	scannedBy   string
	scannedAt   time.Time
	refs        map[string]string
	capturedBy  map[string]string
	capturedAt  map[string]*time.Time
}

// ListTaskAnimals pages one task's scanned animals with their slot maps, keyset on
// animal_row_id (the peer-visibility poll). Names resolve in ONE batched read.
func (r *Repository) ListTaskAnimals(ctx context.Context, tenantID, taskID, cursor string, limit int) ([]ports.AnimalRow, string, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	if limit <= 0 {
		limit = 50
	}

	var category string
	err := r.pool.QueryRow(ctx, `
SELECT category FROM pc_care_tasks WHERE tenant_id = $1::uuid AND task_id = $2::uuid`,
		tenantID, taskID).Scan(&category)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ports.ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("pccare: read task category: %w", err)
	}

	// scale-guard:ignore: one keyset page of ONE task's animal rows, covered by pc_care_task_animals_task_idx (tenant_id, task_id, animal_row_id).
	rows, err := r.pool.Query(ctx, `
SELECT animal_row_id::text, scanned_identifier, scanned_by::text, scanned_at,
       coalesce(video_proof_ref, ''), coalesce(video_captured_by::text, ''), video_captured_at,
       coalesce(before_proof_ref, ''), coalesce(before_captured_by::text, ''), before_captured_at,
       coalesce(during_proof_ref, ''), coalesce(during_captured_by::text, ''), during_captured_at,
       coalesce(after_proof_ref, ''), coalesce(after_captured_by::text, ''), after_captured_at
FROM pc_care_task_animals
WHERE tenant_id = $1::uuid AND task_id = $2::uuid
  AND ($3::text = '' OR animal_row_id > $3::uuid)
ORDER BY animal_row_id
LIMIT $4`, tenantID, taskID, strings.TrimSpace(cursor), limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("pccare: list task animals: %w", err)
	}
	defer rows.Close()

	raw := make([]animalScanRow, 0, limit)
	for rows.Next() {
		a := animalScanRow{
			refs:       map[string]string{},
			capturedBy: map[string]string{},
			capturedAt: map[string]*time.Time{},
		}
		var (
			videoRef, videoBy, beforeRef, beforeBy, duringRef, duringBy, afterRef, afterBy string
			videoAt, beforeAt, duringAt, afterAt                                           *time.Time
		)
		if err := rows.Scan(
			&a.animalRowID, &a.tag, &a.scannedBy, &a.scannedAt,
			&videoRef, &videoBy, &videoAt,
			&beforeRef, &beforeBy, &beforeAt,
			&duringRef, &duringBy, &duringAt,
			&afterRef, &afterBy, &afterAt,
		); err != nil {
			return nil, "", fmt.Errorf("pccare: scan animal row: %w", err)
		}
		a.refs[domain.SlotVideo], a.capturedBy[domain.SlotVideo], a.capturedAt[domain.SlotVideo] = videoRef, videoBy, videoAt
		a.refs[domain.SlotBefore], a.capturedBy[domain.SlotBefore], a.capturedAt[domain.SlotBefore] = beforeRef, beforeBy, beforeAt
		a.refs[domain.SlotDuring], a.capturedBy[domain.SlotDuring], a.capturedAt[domain.SlotDuring] = duringRef, duringBy, duringAt
		a.refs[domain.SlotAfter], a.capturedBy[domain.SlotAfter], a.capturedAt[domain.SlotAfter] = afterRef, afterBy, afterAt
		raw = append(raw, a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	nextCursor := ""
	if len(raw) > limit {
		raw = raw[:limit]
		nextCursor = raw[len(raw)-1].animalRowID
	}

	// Resolve every referenced person's display name in ONE batched read (never per row).
	idSet := map[string]struct{}{}
	for _, a := range raw {
		if a.scannedBy != "" {
			idSet[a.scannedBy] = struct{}{}
		}
		for _, by := range a.capturedBy {
			if by != "" {
				idSet[by] = struct{}{}
			}
		}
	}
	names, err := r.memberNames(ctx, tenantID, idSet)
	if err != nil {
		return nil, "", err
	}

	slots := domain.SlotsForCategory(category)
	out := make([]ports.AnimalRow, 0, len(raw))
	for _, a := range raw {
		row := ports.AnimalRow{
			AnimalRowID:       a.animalRowID,
			ScannedIdentifier: a.tag,
			ScannedBy:         a.scannedBy,
			ScannedByName:     names[a.scannedBy],
			ScannedAt:         a.scannedAt,
			Slots:             make([]ports.AnimalSlot, 0, len(slots)),
		}
		for _, slot := range slots {
			row.Slots = append(row.Slots, ports.AnimalSlot{
				FieldKey:       slot.FieldKey,
				ProofRef:       a.refs[slot.FieldKey],
				CapturedBy:     a.capturedBy[slot.FieldKey],
				CapturedByName: names[a.capturedBy[slot.FieldKey]],
				CapturedAt:     a.capturedAt[slot.FieldKey],
			})
		}
		out = append(out, row)
	}
	return out, nextCursor, nil
}

// memberNames resolves user ids to display names in one bounded read.
func (r *Repository) memberNames(ctx context.Context, tenantID string, idSet map[string]struct{}) (map[string]string, error) {
	names := map[string]string{}
	if len(idSet) == 0 {
		return names, nil
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	rows, err := r.pool.Query(ctx, `
SELECT user_id::text, display_name
FROM workforce_members
WHERE tenant_id = $1::uuid AND user_id = ANY($2::uuid[])`, tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("pccare: resolve member names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}
