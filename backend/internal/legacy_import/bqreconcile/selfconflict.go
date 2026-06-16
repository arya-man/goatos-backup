package bqreconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// selfConflictResolution is the deterministic decision recorded when BQ
// contradicts itself: the Mesha passport value is kept, no field changes.
const selfConflictResolution = "keep_passport_bq_self_contradicts"

// SelfConflictResolveSummary is the JSON result of the deterministic
// self-conflict bulk resolve (dry-run or execute).
type SelfConflictResolveSummary struct {
	DryRun             bool   `json:"dry_run"`
	TenantID           string `json:"tenant_id"`
	TraceID            string `json:"trace_id"`
	EligibleConflicts  int    `json:"eligible_conflicts"`
	ResolvedConflicts  int    `json:"resolved_conflicts"`
	GoatsReturnedClean int    `json:"goats_returned_clean"`
}

// eligibleSelfConflict is one open attribute conflict whose every reason is a
// BQ self-contradiction (no real passport-vs-BQ mismatch present).
type eligibleSelfConflict struct {
	ConflictID string
	GoatIDs    []string
}

// RunSelfConflictResolve closes the open attribute conflicts that are PURELY
// BQ self-contradiction (BQ logged both sexes/breeds for the goat). For those,
// keeping the Mesha passport is deterministic — there is no side to pick — so
// they are resolved in bulk with audit rows, and any goat left with no other
// open conflict is returned to identity_state='clean'. Conflicts that contain
// any real gender_mismatch/breed_mismatch are left untouched for human review.
// Dry-run by default; --execute applies. Idempotent (only 'open' rows match).
func RunSelfConflictResolve(ctx context.Context, pool *pgxpool.Pool, opts Options) (*SelfConflictResolveSummary, error) {
	if strings.TrimSpace(opts.TenantID) == "" {
		return nil, errors.New("tenant-id is required")
	}
	summary := &SelfConflictResolveSummary{
		DryRun:   !opts.Execute,
		TenantID: opts.TenantID,
		TraceID:  opts.TraceID,
	}

	eligible, err := selectSelfConflictEligible(ctx, pool, opts.TenantID)
	if err != nil {
		return nil, err
	}
	summary.EligibleConflicts = len(eligible)
	if !opts.Execute {
		return summary, nil
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin self-conflict resolve transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Share the reconcile lock so this never races reconcile/backfill on the
	// same tenant's conflict + goat rows.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "bq-reconcile:"+opts.TenantID); err != nil {
		return nil, fmt.Errorf("acquire self-conflict resolve lock: %w", err)
	}

	// Re-select inside the transaction for a race-free, idempotent apply.
	eligible, err = selectSelfConflictEligible(ctx, tx, opts.TenantID)
	if err != nil {
		return nil, err
	}
	if len(eligible) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit self-conflict resolve: %w", err)
		}
		return summary, nil
	}

	conflictIDs := make([]string, 0, len(eligible))
	goatIDSet := map[string]struct{}{}
	for _, c := range eligible {
		conflictIDs = append(conflictIDs, c.ConflictID)
		for _, g := range c.GoatIDs {
			if strings.TrimSpace(g) != "" {
				goatIDSet[g] = struct{}{}
			}
		}
		if err := auditSelfConflictResolved(ctx, tx, opts.TenantID, opts.TraceID, c); err != nil {
			return nil, err
		}
	}

	resolved, err := resolveSelfConflictRows(ctx, tx, opts.TenantID, conflictIDs)
	if err != nil {
		return nil, err
	}
	summary.ResolvedConflicts = resolved

	goatIDs := make([]string, 0, len(goatIDSet))
	for g := range goatIDSet {
		goatIDs = append(goatIDs, g)
	}
	cleaned, err := returnSelfConflictGoatsClean(ctx, tx, opts.TenantID, opts.TraceID, goatIDs)
	if err != nil {
		return nil, err
	}
	summary.GoatsReturnedClean = cleaned

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit self-conflict resolve: %w", err)
	}
	return summary, nil
}

// selectSelfConflictEligible returns open bq_reconcile attribute conflicts whose
// every reason is a BQ self-contradiction (none is gender_mismatch or
// breed_mismatch). Those genuine-mismatch conflicts are deliberately excluded.
func selectSelfConflictEligible(ctx context.Context, q queryer, tenantID string) ([]eligibleSelfConflict, error) {
	rows, err := q.Query(ctx, `
SELECT c.conflict_id::text, COALESCE(c.goat_ids, '{}')::text[]
FROM identity_conflicts c
WHERE c.tenant_id = $1::uuid
  AND c.conflict_type = 'status_mismatch'
  AND c.state = 'open'
  AND c.evidence->>'source_context' = 'bq_reconcile_attribute_conflict'
  AND EXISTS (
    SELECT 1 FROM jsonb_array_elements(c.evidence->'conflicts') e
    WHERE e->>'Reason' IN ('bq_gender_self_conflict', 'bq_breed_self_conflict')
  )
  AND NOT EXISTS (
    SELECT 1 FROM jsonb_array_elements(c.evidence->'conflicts') e
    WHERE e->>'Reason' IN ('gender_mismatch', 'breed_mismatch')
  )
ORDER BY c.conflict_id::text`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("select self-conflict eligible conflicts: %w", err)
	}
	defer rows.Close()
	out := []eligibleSelfConflict{}
	for rows.Next() {
		var id string
		var goatIDs []string
		if err := rows.Scan(&id, &goatIDs); err != nil {
			return nil, fmt.Errorf("scan self-conflict eligible conflict: %w", err)
		}
		out = append(out, eligibleSelfConflict{ConflictID: id, GoatIDs: goatIDs})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func resolveSelfConflictRows(ctx context.Context, tx pgx.Tx, tenantID string, conflictIDs []string) (int, error) {
	tag, err := tx.Exec(ctx, `
UPDATE identity_conflicts
SET state = 'resolved',
    resolved_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND state = 'open'
  AND conflict_type = 'status_mismatch'
  AND evidence->>'source_context' = 'bq_reconcile_attribute_conflict'
  AND conflict_id = ANY($2::uuid[])`, tenantID, conflictIDs)
	if err != nil {
		return 0, fmt.Errorf("resolve self-conflict rows: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// returnSelfConflictGoatsClean flips goats back to identity_state='clean' when
// they have no remaining open/needs_field_check conflict after the resolve, and
// writes an audit row for each.
func returnSelfConflictGoatsClean(ctx context.Context, tx pgx.Tx, tenantID, traceID string, goatIDs []string) (int, error) {
	if len(goatIDs) == 0 {
		return 0, nil
	}
	rows, err := tx.Query(ctx, `
UPDATE goats g
SET identity_state = 'clean',
    row_version = row_version + 1,
    updated_at = now()
WHERE g.tenant_id = $1::uuid
  AND g.goat_id = ANY($2::uuid[])
  AND g.identity_state = 'needs_review'
  AND NOT EXISTS (
    SELECT 1
    FROM identity_conflict_goats cg
    JOIN identity_conflicts c
      ON c.tenant_id = cg.tenant_id
     AND c.conflict_id = cg.conflict_id
    WHERE cg.tenant_id = g.tenant_id
      AND cg.goat_id = g.goat_id
      AND c.state IN ('open', 'needs_field_check')
  )
RETURNING g.goat_id::text`, tenantID, goatIDs)
	if err != nil {
		return 0, fmt.Errorf("return self-conflict goats clean: %w", err)
	}
	cleanedIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan cleaned goat: %w", err)
		}
		cleanedIDs = append(cleanedIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	for _, goatID := range cleanedIDs {
		metadata, _ := json.Marshal(map[string]any{
			"source_context": "bq_self_conflict_resolve",
			"resolution":     selfConflictResolution,
			"command":        "bq-reconcile --resolve-self-conflicts",
		})
		before, _ := json.Marshal(map[string]string{"identity_state": "needs_review"})
		after, _ := json.Marshal(map[string]string{"identity_state": "clean"})
		if _, err := tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id, actor_type, action, resource_type, resource_id,
  before_state, after_state, metadata, trace_id
) VALUES (
  $1::uuid, 'system', 'goat.identity_clean_after_self_conflict_resolve', 'goat', $2::uuid,
  $3::jsonb, $4::jsonb, $5::jsonb, $6
)`, tenantID, goatID, before, after, metadata, traceID); err != nil {
			return 0, fmt.Errorf("audit cleaned goat %s: %w", goatID, err)
		}
	}
	return len(cleanedIDs), nil
}

func auditSelfConflictResolved(ctx context.Context, tx pgx.Tx, tenantID, traceID string, c eligibleSelfConflict) error {
	before, _ := json.Marshal(map[string]string{"state": "open"})
	after, _ := json.Marshal(map[string]any{"state": "resolved", "resolution": selfConflictResolution})
	metadata, _ := json.Marshal(map[string]any{
		"source_context": "bq_self_conflict_resolve",
		"resolution":     selfConflictResolution,
		"command":        "bq-reconcile --resolve-self-conflicts",
		"goat_ids":       c.GoatIDs,
		"review_note":    "BQ contradicts itself for this goat (logged multiple sexes/breeds); kept the Mesha passport value unchanged and closed the review flag.",
	})
	_, err := tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id, actor_type, action, resource_type, resource_id,
  before_state, after_state, metadata, trace_id
) VALUES (
  $1::uuid, 'system', 'identity_conflict.resolved', 'identity_conflict', $2::uuid,
  $3::jsonb, $4::jsonb, $5::jsonb, $6
)`, tenantID, c.ConflictID, before, after, metadata, traceID)
	if err != nil {
		return fmt.Errorf("audit resolved self-conflict %s: %w", c.ConflictID, err)
	}
	return nil
}
