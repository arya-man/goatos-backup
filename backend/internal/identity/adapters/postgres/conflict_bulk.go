package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

// Bulk conflict-resolution decision types (human-driven, evidence-visible).
const (
	bulkDecisionKeepPassport  = "keep_passport_value"
	bulkDecisionUseLegacy     = "use_legacy_value"
	bulkDecisionAckLifecycle  = "acknowledge_lifecycle_flag"
	attributeConflictContext  = "bq_reconcile_attribute_conflict"
	lifecycleConflictContext  = "bq_reconcile_identifier_lifecycle_conflict"
	maxBulkResolveConflictIDs = 200
)

// Review groups (UI-safe codes; rendered labels say "legacy data", never raw BQ).
const (
	reviewGroupLegacySelfConflict = "legacy_self_conflict"
	reviewGroupSexMismatch        = "legacy_sex_mismatch"
	reviewGroupBreedMismatch      = "legacy_breed_mismatch"
	reviewGroupValueMismatch      = "legacy_value_mismatch"
	reviewGroupLifecycleFlag      = "lifecycle_reused_tag"
)

// classifyReviewGroup maps a conflict's source context + evidence reasons to a
// stable, UI-safe review group. Pure (no DB).
func classifyReviewGroup(sourceContext string, reasons []string) string {
	switch sourceContext {
	case lifecycleConflictContext:
		return reviewGroupLifecycleFlag
	case attributeConflictContext:
		hasSelf, hasSex, hasBreed := false, false, false
		for _, reason := range reasons {
			switch reason {
			case "bq_gender_self_conflict", "bq_breed_self_conflict":
				hasSelf = true
			case "gender_mismatch":
				hasSex = true
			case "breed_mismatch":
				hasBreed = true
			}
		}
		switch {
		case hasSelf:
			return reviewGroupLegacySelfConflict
		case hasSex && hasBreed:
			return reviewGroupValueMismatch
		case hasSex:
			return reviewGroupSexMismatch
		case hasBreed:
			return reviewGroupBreedMismatch
		}
	}
	return ""
}

// reviewGroupWhereClause returns a SQL predicate (over alias c) that selects
// conflicts in the given review group, or ok=false for an unknown group.
func reviewGroupWhereClause(group string) (string, bool) {
	attrSelf := `EXISTS (SELECT 1 FROM jsonb_array_elements(c.evidence->'conflicts') e WHERE e->>'Reason' IN ('bq_gender_self_conflict','bq_breed_self_conflict'))`
	attrSex := `EXISTS (SELECT 1 FROM jsonb_array_elements(c.evidence->'conflicts') e WHERE e->>'Reason' = 'gender_mismatch')`
	attrBreed := `EXISTS (SELECT 1 FROM jsonb_array_elements(c.evidence->'conflicts') e WHERE e->>'Reason' = 'breed_mismatch')`
	isAttr := `c.evidence->>'source_context' = '` + attributeConflictContext + `'`
	switch group {
	case reviewGroupLifecycleFlag:
		return `c.evidence->>'source_context' = '` + lifecycleConflictContext + `'`, true
	case reviewGroupLegacySelfConflict:
		return isAttr + " AND " + attrSelf, true
	case reviewGroupSexMismatch:
		return isAttr + " AND NOT " + attrSelf + " AND " + attrSex + " AND NOT " + attrBreed, true
	case reviewGroupBreedMismatch:
		return isAttr + " AND NOT " + attrSelf + " AND " + attrBreed + " AND NOT " + attrSex, true
	case reviewGroupValueMismatch:
		return isAttr + " AND NOT " + attrSelf + " AND " + attrSex + " AND " + attrBreed, true
	default:
		return "", false
	}
}

type bulkConflictRow struct {
	conflictID    string
	conflictType  string
	state         string
	rowVersion    int
	goatIDs       []string
	sourceContext string
	reviewGroup   string
	evidence      *domain.ConflictLegacyEvidence
}

// BulkResolveConflicts applies one human decision to many conflicts in a single
// transaction. The whole batch aborts (no writes) if any conflict is missing,
// stale (row_version mismatch), not open, or invalid for the decision. Each
// resolved conflict gets its own audit row sharing the bulk_request_id. Goats
// left with no remaining open conflict are returned to identity_state='clean'.
// Counters are NOT written here (cross-module); the result flags rebuild needed.
func (r *Repository) BulkResolveConflicts(ctx context.Context, cmd ports.BulkResolveConflictsCommand) (*domain.BulkResolveConflictsResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, "identity-bulk-resolve:"+cmd.TenantID); err != nil {
		return nil, err
	}

	rows, err := loadBulkConflicts(ctx, tx, tenantUUID, cmd.ConflictIDs)
	if err != nil {
		return nil, err
	}
	// Every requested conflict must exist.
	if len(rows) != len(cmd.ConflictIDs) {
		return nil, ports.ErrNotFound
	}

	// Validate every row before applying anything (abort-if-any-invalid).
	for i := range rows {
		if err := validateBulkConflict(rows[i], cmd); err != nil {
			return nil, err
		}
	}

	result := &domain.BulkResolveConflictsResult{
		BulkRequestID: cmd.BulkRequestID,
		DecisionType:  cmd.DecisionType,
		TraceID:       cmd.TraceID,
	}
	affectedGoats := map[string]struct{}{}

	for i := range rows {
		row := rows[i]
		if cmd.DecisionType == bulkDecisionUseLegacy {
			mutated, err := r.applyUseLegacy(ctx, tx, tenantUUID, row, cmd)
			if err != nil {
				return nil, err
			}
			if mutated {
				result.GoatsMutated++
			}
		}
		if err := resolveBulkConflictRow(ctx, tx, tenantUUID, row, cmd); err != nil {
			return nil, err
		}
		for _, g := range row.goatIDs {
			affectedGoats[g] = struct{}{}
		}
		result.ResolvedConflictIDs = append(result.ResolvedConflictIDs, row.conflictID)
	}

	cleaned, err := r.returnGoatsCleanAfterBulk(ctx, tx, tenantUUID, affectedGoats, cmd)
	if err != nil {
		return nil, err
	}
	result.GoatsReturnedClean = cleaned
	result.CountersRebuildRequired = len(result.ResolvedConflictIDs) > 0

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}

func loadBulkConflicts(ctx context.Context, tx pgx.Tx, tenantUUID any, conflictIDs []string) ([]bulkConflictRow, error) {
	dbRows, err := tx.Query(ctx, `
SELECT conflict_id::text, conflict_type, state, row_version,
       COALESCE(goat_ids, '{}')::text[],
       COALESCE(evidence->>'source_context', ''),
       evidence
FROM identity_conflicts
WHERE tenant_id = $1
  AND conflict_id = ANY($2::uuid[])
FOR UPDATE`, tenantUUID, conflictIDs)
	if err != nil {
		return nil, fmt.Errorf("load bulk conflicts: %w", err)
	}
	defer dbRows.Close()
	out := []bulkConflictRow{}
	for dbRows.Next() {
		var row bulkConflictRow
		var rawEvidence []byte
		if err := dbRows.Scan(&row.conflictID, &row.conflictType, &row.state, &row.rowVersion, &row.goatIDs, &row.sourceContext, &rawEvidence); err != nil {
			return nil, fmt.Errorf("scan bulk conflict: %w", err)
		}
		evidence, err := parseConflictLegacyEvidence(rawEvidence)
		if err != nil {
			return nil, err
		}
		row.evidence = evidence
		row.reviewGroup = classifyReviewGroup(row.sourceContext, evidenceReasons(evidence))
		out = append(out, row)
	}
	if err := dbRows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func evidenceReasons(ev *domain.ConflictLegacyEvidence) []string {
	if ev == nil {
		return nil
	}
	reasons := make([]string, 0, len(ev.Conflicts))
	for _, item := range ev.Conflicts {
		reasons = append(reasons, item.Reason)
	}
	return reasons
}

// validateBulkConflict enforces row_version, open state, and decision
// applicability for a single conflict. Returns a ports error to abort the batch.
func validateBulkConflict(row bulkConflictRow, cmd ports.BulkResolveConflictsCommand) error {
	expected, ok := cmd.RowVersions[row.conflictID]
	if !ok {
		return ports.ErrWriteConflict
	}
	if row.rowVersion != expected {
		return ports.ErrWriteConflict
	}
	if row.state != "open" && row.state != "needs_field_check" {
		return ports.ErrWriteConflict
	}
	switch cmd.DecisionType {
	case bulkDecisionAckLifecycle:
		if row.sourceContext != lifecycleConflictContext {
			return ports.ErrBulkDecisionNotApplicable
		}
	case bulkDecisionKeepPassport:
		if row.sourceContext != attributeConflictContext {
			return ports.ErrBulkDecisionNotApplicable
		}
	case bulkDecisionUseLegacy:
		if row.sourceContext != attributeConflictContext {
			return ports.ErrBulkDecisionNotApplicable
		}
		if row.reviewGroup == reviewGroupLegacySelfConflict {
			return ports.ErrBulkDecisionNotApplicable
		}
		if len(row.goatIDs) != 1 {
			return ports.ErrBulkDecisionNotApplicable
		}
		// Every mismatched attribute must have exactly one clean legacy value.
		for _, item := range mismatchAttributeItems(row.evidence) {
			if !hasSingleCleanLegacyValue(item) {
				return ports.ErrBulkDecisionNotApplicable
			}
		}
	default:
		return ports.ErrBulkDecisionNotApplicable
	}
	return nil
}

func mismatchAttributeItems(ev *domain.ConflictLegacyEvidence) []domain.ConflictLegacyEvidenceItem {
	if ev == nil {
		return nil
	}
	out := []domain.ConflictLegacyEvidenceItem{}
	for _, item := range ev.Conflicts {
		if item.Reason == "gender_mismatch" || item.Reason == "breed_mismatch" {
			out = append(out, item)
		}
	}
	return out
}

// hasSingleCleanLegacyValue is true only when the legacy value is present and is
// a single value (self-conflicts pipe-join multiple values, e.g. "female|male").
func hasSingleCleanLegacyValue(item domain.ConflictLegacyEvidenceItem) bool {
	if item.LegacyValue == nil {
		return false
	}
	v := strings.TrimSpace(*item.LegacyValue)
	return v != "" && !strings.Contains(v, "|")
}

// applyUseLegacy mutates the conflict goat's sex/breed to the single clean
// legacy value. Breed must map to an APPROVED (status='active') canonical breed
// or the batch aborts (no junk labels). Writes a goat audit row.
func (r *Repository) applyUseLegacy(ctx context.Context, tx pgx.Tx, tenantUUID any, row bulkConflictRow, cmd ports.BulkResolveConflictsCommand) (bool, error) {
	goatID := row.goatIDs[0]
	setSex, sexValue := "", ""
	setBreed, breedName, breedID := false, "", ""
	for _, item := range mismatchAttributeItems(row.evidence) {
		legacy := strings.TrimSpace(*item.LegacyValue)
		switch item.Attribute {
		case "sex":
			normalized := normalizeBulkSex(legacy)
			if normalized == "" {
				return false, ports.ErrBulkDecisionNotApplicable
			}
			setSex, sexValue = "set", normalized
		case "breed":
			id, canonical, err := r.lookupApprovedBreed(ctx, tx, legacy)
			if err != nil {
				return false, err
			}
			if id == "" {
				// Not an approved canonical breed: do not create junk; abort.
				return false, ports.ErrBulkBreedNotCanonical
			}
			setBreed, breedName, breedID = true, canonical, id
		default:
			return false, ports.ErrBulkDecisionNotApplicable
		}
	}
	if setSex == "" && !setBreed {
		return false, nil
	}

	var beforeSex, beforeBreed *string
	if err := tx.QueryRow(ctx, `SELECT sex, breed FROM goats WHERE tenant_id=$1 AND goat_id=$2::uuid FOR UPDATE`, tenantUUID, goatID).Scan(&beforeSex, &beforeBreed); err != nil {
		return false, fmt.Errorf("load goat for use_legacy: %w", err)
	}

	_, err := tx.Exec(ctx, `
UPDATE goats
SET sex = CASE WHEN $3 THEN $4 ELSE sex END,
    breed = CASE WHEN $5 THEN $6 ELSE breed END,
    breed_id = CASE WHEN $5 THEN $7::uuid ELSE breed_id END,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1 AND goat_id = $2::uuid`,
		tenantUUID, goatID, setSex != "", nullableBulkText(sexValue), setBreed, nullableBulkText(breedName), nullableBulkUUID(breedID))
	if err != nil {
		return false, fmt.Errorf("apply use_legacy goat mutation: %w", err)
	}

	before, _ := json.Marshal(map[string]any{"sex": beforeSex, "breed": beforeBreed})
	after := map[string]any{}
	if setSex != "" {
		after["sex"] = sexValue
	}
	if setBreed {
		after["breed"] = breedName
		after["breed_id"] = breedID
	}
	afterJSON, _ := json.Marshal(after)
	metadata, _ := json.Marshal(map[string]any{
		"bulk_request_id": cmd.BulkRequestID,
		"conflict_id":     row.conflictID,
		"decision_type":   cmd.DecisionType,
		"review_group":    row.reviewGroup,
		"reason":          cmd.Reason,
		"source":          "legacy_data",
	})
	if err := insertBulkAudit(ctx, tx, tenantUUID, cmd.ActorID, cmd.TraceID,
		"goat.attribute_resolved_from_legacy", "goat", goatID, before, afterJSON, metadata); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) lookupApprovedBreed(ctx context.Context, tx pgx.Tx, legacyValue string) (string, string, error) {
	var breedID, canonical string
	err := tx.QueryRow(ctx, `
SELECT b.breed_id::text, b.canonical_name
FROM breeds b
WHERE b.status = 'active'
  AND (
    lower(b.canonical_name) = lower($1)
    OR EXISTS (
      SELECT 1 FROM breed_aliases a
      WHERE a.breed_id = b.breed_id AND lower(a.alias) = lower($1)
    )
  )
ORDER BY b.canonical_name
LIMIT 1`, legacyValue).Scan(&breedID, &canonical)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("lookup approved breed: %w", err)
	}
	return breedID, canonical, nil
}

func resolveBulkConflictRow(ctx context.Context, tx pgx.Tx, tenantUUID any, row bulkConflictRow, cmd ports.BulkResolveConflictsCommand) error {
	tag, err := tx.Exec(ctx, `
UPDATE identity_conflicts
SET state = 'resolved',
    resolved_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1
  AND conflict_id = $2::uuid
  AND state IN ('open', 'needs_field_check')
  AND row_version = $3`, tenantUUID, row.conflictID, row.rowVersion)
	if err != nil {
		return fmt.Errorf("resolve bulk conflict %s: %w", row.conflictID, err)
	}
	if tag.RowsAffected() != 1 {
		return ports.ErrWriteConflict
	}
	before, _ := json.Marshal(map[string]any{"state": row.state})
	after, _ := json.Marshal(map[string]any{"state": "resolved", "decision_type": cmd.DecisionType})
	metadata, _ := json.Marshal(map[string]any{
		"bulk_request_id": cmd.BulkRequestID,
		"decision_type":   cmd.DecisionType,
		"review_group":    row.reviewGroup,
		"reason":          cmd.Reason,
		"goat_ids":        row.goatIDs,
	})
	return insertBulkAudit(ctx, tx, tenantUUID, cmd.ActorID, cmd.TraceID,
		"identity_conflict.bulk_resolved", "identity_conflict", row.conflictID, before, after, metadata)
}

func (r *Repository) returnGoatsCleanAfterBulk(ctx context.Context, tx pgx.Tx, tenantUUID any, goatSet map[string]struct{}, cmd ports.BulkResolveConflictsCommand) (int, error) {
	if len(goatSet) == 0 {
		return 0, nil
	}
	goatIDs := make([]string, 0, len(goatSet))
	for g := range goatSet {
		goatIDs = append(goatIDs, g)
	}
	dbRows, err := tx.Query(ctx, `
UPDATE goats g
SET identity_state = 'clean', row_version = row_version + 1, updated_at = now()
WHERE g.tenant_id = $1
  AND g.goat_id = ANY($2::uuid[])
  AND g.identity_state = 'needs_review'
  AND NOT EXISTS (
    SELECT 1 FROM identity_conflict_goats cg
    JOIN identity_conflicts c ON c.tenant_id = cg.tenant_id AND c.conflict_id = cg.conflict_id
    WHERE cg.tenant_id = g.tenant_id AND cg.goat_id = g.goat_id
      AND c.state IN ('open', 'needs_field_check')
  )
RETURNING g.goat_id::text`, tenantUUID, goatIDs)
	if err != nil {
		return 0, fmt.Errorf("return goats clean after bulk: %w", err)
	}
	cleaned := []string{}
	for dbRows.Next() {
		var id string
		if err := dbRows.Scan(&id); err != nil {
			dbRows.Close()
			return 0, err
		}
		cleaned = append(cleaned, id)
	}
	if err := dbRows.Err(); err != nil {
		dbRows.Close()
		return 0, err
	}
	dbRows.Close()
	for _, goatID := range cleaned {
		before, _ := json.Marshal(map[string]string{"identity_state": "needs_review"})
		after, _ := json.Marshal(map[string]string{"identity_state": "clean"})
		metadata, _ := json.Marshal(map[string]any{"bulk_request_id": cmd.BulkRequestID, "decision_type": cmd.DecisionType})
		if err := insertBulkAudit(ctx, tx, tenantUUID, cmd.ActorID, cmd.TraceID,
			"goat.identity_clean_after_bulk_resolve", "goat", goatID, before, after, metadata); err != nil {
			return 0, err
		}
	}
	return len(cleaned), nil
}

func insertBulkAudit(ctx context.Context, tx pgx.Tx, tenantUUID any, actorID, traceID, action, resourceType, resourceID string, before, after, metadata []byte) error {
	_, err := tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id, actor_id, actor_type, action, resource_type, resource_id,
  before_state, after_state, metadata, trace_id
) VALUES (
  $1, $2::uuid, 'human', $3, $4, $5::uuid, $6::jsonb, $7::jsonb, $8::jsonb, $9
)`, tenantUUID, nullableBulkUUID(actorID), action, resourceType, resourceID, before, after, metadata, traceID)
	if err != nil {
		return fmt.Errorf("write bulk audit (%s): %w", action, err)
	}
	return nil
}

func normalizeBulkSex(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "male", "m":
		return "male"
	case "female", "f":
		return "female"
	case "unknown":
		return "unknown"
	default:
		return ""
	}
}

func nullableBulkText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableBulkUUID(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
