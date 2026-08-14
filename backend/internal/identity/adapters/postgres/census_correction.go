package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// censusSliceScopeSQL is the predicate every part of this write shares -- preview, count, and the
// UPDATE itself -- so the number an operator confirms is the number of rows that change.
//
// Every column of the Counts Breakdown row participates. Dropping one would widen the correction
// to animals the operator never saw: without `sex` it would take the pen's females as well, without
// `management_stage` it would take a cohort that merely shares the pen. Breed is compared through
// COALESCE so the census's blank-breed row -- a real row, and a common thing to correct -- matches
// as itself rather than matching nothing.
//
// The partition is normalized the same way the reclassify path normalizes it, so 'Part 1' and '1'
// name one pen, and a shed with no pens matches on the 'whole' sentinel.
const censusSliceScopeSQL = `
    FROM goats g
    LEFT JOIN goat_shed_partitions gsp
      ON gsp.tenant_id = g.tenant_id
     AND gsp.goat_id = g.goat_id
    WHERE g.tenant_id = $1::uuid
      AND g.shed_id = $2::uuid
      AND g.lifecycle_status = 'alive'
      AND g.merged_into_goat_id IS NULL
      AND g.exited_at IS NULL
      AND regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') = $3::text
      AND btrim(COALESCE(g.management_stage, '')) = btrim($4::text)
      AND btrim(COALESCE(g.breed, '')) = btrim($5::text)
      AND btrim(COALESCE(g.sex, '')) = btrim($6::text)`

// PreviewCorrectCensusSlice answers "how many animals would this change" without writing.
//
// Read-only and lock-free: the commit re-derives the same scope under its own transaction, so a
// preview that is a few seconds stale cannot cause a wrong write -- it can only cause the confirm
// step to name a number the commit then reports differently, which the result carries back.
func (r *Repository) PreviewCorrectCensusSlice(ctx context.Context, cmd ports.CorrectCensusSliceCommand) (*ports.CensusSlicePreview, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: preview correct census slice: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	location, err := r.resolveCensusSliceLocation(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	total, err := r.countCensusSlice(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}

	return &ports.CensusSlicePreview{
		ShedID:                     cmd.ShedID,
		ShedName:                   location.ShedName,
		PartitionLabel:             location.PartitionLabel,
		OperationalLocationDisplay: location.Display(),
		Field:                      cmd.Field,
		CurrentValue:               currentSliceValue(cmd),
		Value:                      cmd.Value,
		TotalLive:                  total,
	}, nil
}

// CorrectCensusSlice applies the correction inside one transaction, behind the same idempotency
// discipline as every other identity write: an exact replay returns the original result without
// touching a row twice.
func (r *Repository) CorrectCensusSlice(ctx context.Context, cmd ports.CorrectCensusSliceCommand) (*ports.CensusSliceCorrectionResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: correct census slice: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	if _, err := qtx.InsertIdempotencyStarted(ctx, insertIdempotencyStartedParams(ports.ReclassifyShedStageCommand{
		TenantID: cmd.TenantID, StoredIdempotencyKey: cmd.StoredIdempotencyKey,
		IdempotencyScope: cmd.IdempotencyScope, RequestHash: cmd.RequestHash,
	})); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return r.replayCensusSliceCorrection(ctx, tx, cmd)
		}
		return nil, fmt.Errorf("identity: correct census slice: reserve idempotency: %w", err)
	}

	location, err := r.resolveCensusSliceLocation(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	total, err := r.countCensusSlice(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, ports.ErrCensusCorrectionEmptyScope
	}
	if total > ports.MaxCensusCorrectionGoats {
		return nil, ports.ErrCensusCorrectionScopeTooLarge
	}

	if err := r.assertCensusSliceValue(ctx, tx, cmd); err != nil {
		return nil, err
	}
	corrected, err := r.applyCensusSliceCorrection(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}

	result := &ports.CensusSliceCorrectionResult{
		ShedID:                     cmd.ShedID,
		ShedName:                   location.ShedName,
		PartitionLabel:             location.PartitionLabel,
		OperationalLocationDisplay: location.Display(),
		Field:                      cmd.Field,
		CurrentValue:               currentSliceValue(cmd),
		Value:                      cmd.Value,
		TotalLive:                  total,
		Corrected:                  corrected,
	}

	if err := r.recordCensusSliceAudit(ctx, tx, cmd, result); err != nil {
		return nil, err
	}
	if err := completeReclassifyIdempotency(ctx, qtx, ports.ReclassifyShedStageCommand{
		StoredIdempotencyKey: cmd.StoredIdempotencyKey,
	}, &ports.ReclassifyShedStageResult{ShedID: result.ShedID}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("identity: correct census slice: commit: %w", err)
	}
	return result, nil
}

// applyCensusSliceCorrection writes ONE column, chosen by cmd.Field.
//
// The column is never interpolated from caller input: Field is matched to a fixed statement, so a
// value that somehow reached here cannot name a column. For breed, breed_id moves with the text --
// `goats` carries both, and leaving the id pointing at the old breed would make the two disagree
// for every reader that joins through it.
func (r *Repository) applyCensusSliceCorrection(ctx context.Context, tx pgx.Tx, cmd ports.CorrectCensusSliceCommand) (int, error) {
	partitionKey := oploc.NormalizePartition(stringValue(cmd.PartitionLabel))
	args := []any{cmd.TenantID, cmd.ShedID, partitionKey, cmd.ManagementStage, cmd.Breed, cmd.Sex, cmd.Value}

	var statement string
	switch cmd.Field {
	case "breed":
		// breed_id moves WITH the text: `goats` carries both, and leaving the id on the old breed
		// would make them disagree for every reader that joins through it. ORDER BY breed_id keeps
		// the pick deterministic -- the catalog currently holds a duplicate canonical_name, and a
		// correction must not resolve to a different row on a retry.
		statement = `
UPDATE goats
SET breed = (SELECT btrim(b.canonical_name) FROM breeds b
             WHERE b.status = 'active' AND btrim(lower(b.canonical_name)) = btrim(lower($7::text))
             ORDER BY b.breed_id LIMIT 1),
    breed_id = (SELECT b.breed_id FROM breeds b
                WHERE b.status = 'active' AND btrim(lower(b.canonical_name)) = btrim(lower($7::text))
                ORDER BY b.breed_id LIMIT 1),
    updated_at = now()
WHERE goat_id IN (SELECT g.goat_id ` + censusSliceScopeSQL + `)`
	case "sex":
		statement = `
UPDATE goats
SET sex = btrim($7::text),
    updated_at = now()
WHERE goat_id IN (SELECT g.goat_id ` + censusSliceScopeSQL + `)`
	default:
		return 0, ports.ErrCensusCorrectionField
	}

	tag, err := tx.Exec(ctx, statement, args...)
	if err != nil {
		return 0, fmt.Errorf("identity: correct census slice: apply: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// assertCensusSliceValue fails a correction whose target value is not in the tenant's vocabulary.
//
// It exists because the UPDATE would otherwise ACCEPT an unknown breed and write NULL into
// breed_id via its subquery -- a silent half-write leaving the text and the id disagreeing. Sex is
// already closed at the service boundary and again by goats_sex_check, so only breed needs the
// catalog lookup.
func (r *Repository) assertCensusSliceValue(ctx context.Context, tx pgx.Tx, cmd ports.CorrectCensusSliceCommand) error {
	if cmd.Field != "breed" {
		return nil
	}
	var known bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM breeds b
  WHERE b.status = 'active' AND btrim(lower(b.canonical_name)) = btrim(lower($1::text)))`,
		cmd.Value).Scan(&known); err != nil {
		return fmt.Errorf("identity: correct census slice: validate breed: %w", err)
	}
	if !known {
		return ports.ErrCensusCorrectionValue
	}
	return nil
}

func (r *Repository) countCensusSlice(ctx context.Context, tx pgx.Tx, cmd ports.CorrectCensusSliceCommand) (int, error) {
	partitionKey := oploc.NormalizePartition(stringValue(cmd.PartitionLabel))
	var total int
	err := tx.QueryRow(ctx, `SELECT count(*)`+censusSliceScopeSQL,
		cmd.TenantID, cmd.ShedID, partitionKey, cmd.ManagementStage, cmd.Breed, cmd.Sex).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("identity: correct census slice: count scope: %w", err)
	}
	return total, nil
}

// resolveCensusSliceLocation names the pen through the SHARED oploc resolver, so the confirm step
// and the audit row read "Gandhi - 3" exactly as every other screen spells it.
func (r *Repository) resolveCensusSliceLocation(ctx context.Context, tx pgx.Tx, cmd ports.CorrectCensusSliceCommand) (oploc.OperationalLocation, error) {
	location, err := oploc.ResolveShedLocation(ctx, tx.QueryRow(ctx, oploc.ShedScopedLocationSQL, cmd.TenantID, cmd.ShedID))
	if err != nil {
		return oploc.OperationalLocation{}, fmt.Errorf("identity: correct census slice: resolve location: %w", err)
	}
	// The command's own partition wins when it has one: the shed-scoped resolver answers for the
	// SHED, and this write is scoped to one pen of it.
	if label := strings.TrimSpace(stringValue(cmd.PartitionLabel)); label != "" {
		location.PartitionLabel = label
	}
	return location, nil
}

// currentSliceValue is the value being corrected AWAY from -- the row's own breed or sex.
func currentSliceValue(cmd ports.CorrectCensusSliceCommand) string {
	if cmd.Field == "sex" {
		return cmd.Sex
	}
	return cmd.Breed
}

// replayCensusSliceCorrection answers an exact replay from the audit row this key already wrote,
// so a double-click or a retried network failure reports the first result instead of correcting a
// second time. A same-key different-payload replay is rejected on the request hash by the
// idempotency row itself.
func (r *Repository) replayCensusSliceCorrection(ctx context.Context, tx pgx.Tx, cmd ports.CorrectCensusSliceCommand) (*ports.CensusSliceCorrectionResult, error) {
	qtx := r.queries.WithTx(tx)
	idempotency, err := qtx.GetIdempotencyKey(ctx, cmd.StoredIdempotencyKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrIdempotencyPending
	}
	if err != nil {
		return nil, fmt.Errorf("identity: correct census slice: read idempotency: %w", err)
	}
	if strings.TrimSpace(idempotency.ResultType) == "" {
		// Reserved but not completed: the first call is still in flight (or died mid-write). The
		// caller retries rather than getting a half-answer.
		return nil, ports.ErrIdempotencyPending
	}

	var payload []byte
	err = tx.QueryRow(ctx, `
SELECT after_state FROM audit_log
WHERE tenant_id = $1::uuid AND action = 'identity_census_slice_correction'
  AND metadata->>'idempotency_key' = $2
ORDER BY created_at DESC LIMIT 1`, cmd.TenantID, cmd.ClientIdempotencyKey).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrIdempotencyPending
	}
	if err != nil {
		return nil, fmt.Errorf("identity: correct census slice: read replay: %w", err)
	}

	var result ports.CensusSliceCorrectionResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("identity: correct census slice: decode replay: %w", err)
	}
	return &result, nil
}

// recordCensusSliceAudit writes the accountability row. It records the WHOLE decision -- which pen,
// which row, which field, from what to what, how many animals -- because there is no approval step
// and no proof behind this write, so the audit row is the only account of it.
func (r *Repository) recordCensusSliceAudit(ctx context.Context, tx pgx.Tx, cmd ports.CorrectCensusSliceCommand, result *ports.CensusSliceCorrectionResult) error {
	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return err
	}
	actorUUID, err := uuidParam(cmd.ActorID)
	if err != nil {
		return err
	}
	shedUUID, err := uuidParam(result.ShedID)
	if err != nil {
		return err
	}
	afterState, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("identity: correct census slice: encode audit: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"idempotency_key":  cmd.ClientIdempotencyKey,
		"trace_id":         cmd.TraceID,
		"reason":           cmd.Reason,
		"field":            cmd.Field,
		"from":             result.CurrentValue,
		"to":               result.Value,
		"management_stage": cmd.ManagementStage,
		"breed":            cmd.Breed,
		"sex":              cmd.Sex,
	})
	if err != nil {
		return fmt.Errorf("identity: correct census slice: encode audit metadata: %w", err)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO audit_log (tenant_id, actor_id, action, entity_type, entity_id, after_state, metadata, occurred_at)
VALUES ($1, $2, 'identity_census_slice_correction', 'shed', $3, $4, $5, $6)`,
		tenantUUID, actorUUID, shedUUID, afterState, metadata, cmd.OccurredAt); err != nil {
		return fmt.Errorf("identity: correct census slice: audit: %w", err)
	}
	return nil
}
