package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

// TestBulkResolveConflictsWithDockerPostgres covers the human bulk-review path:
// keep-passport, use-legacy (sex + approved canonical breed), acknowledge
// lifecycle, decision applicability rejects, optimistic-concurrency aborts,
// abort-if-any-invalid, return-to-clean, and the counters-rebuild flag.
func TestBulkResolveConflictsWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	t.Run("keep_passport_value resolves attribute conflicts without mutating the goat", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		conflictID := insertBulkAttributeConflict(t, pool, meshaTenant, goatID, []map[string]any{
			attrConflictItem("sex", "gender_mismatch", "female", "male"),
		})
		cmd := bulkCmd(meshaTenant, bulkDecisionKeepPassport, "bulk-keep-1", []string{conflictID}, map[string]int{conflictID: 1})

		result, err := repo.BulkResolveConflicts(ctx, cmd)
		if err != nil {
			t.Fatalf("BulkResolveConflicts: %v", err)
		}
		if len(result.ResolvedConflictIDs) != 1 || result.ResolvedConflictIDs[0] != conflictID {
			t.Fatalf("resolved ids = %#v", result.ResolvedConflictIDs)
		}
		if result.GoatsMutated != 0 || result.GoatsReturnedClean != 1 || !result.CountersRebuildRequired {
			t.Fatalf("unexpected result: %#v", result)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_conflicts WHERE conflict_id = $1 AND state = 'resolved' AND row_version = 2 AND resolved_at IS NOT NULL`, conflictID); got != 1 {
			t.Fatalf("resolved conflict rows = %d", got)
		}
		sex, _, state := goatSexBreedState(t, pool, goatID)
		if sex != "female" || state != "clean" {
			t.Fatalf("goat mutated/uncleaned: sex=%s state=%s", sex, state)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM audit_log WHERE action = 'identity_conflict.bulk_resolved' AND resource_id = $1 AND actor_type = 'human' AND metadata->>'bulk_request_id' = $2`, conflictID, cmd.BulkRequestID); got != 1 {
			t.Fatalf("bulk_resolved audit rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM audit_log WHERE action = 'goat.identity_clean_after_bulk_resolve' AND resource_id = $1`, goatID); got != 1 {
			t.Fatalf("clean audit rows = %d", got)
		}
		assertNoRows(t, pool, "no legacy mutation audit on keep_passport", `SELECT count(*) FROM audit_log WHERE action = 'goat.attribute_resolved_from_legacy' AND resource_id = $1`, goatID)
	})

	t.Run("use_legacy_value applies single clean legacy sex", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		conflictID := insertBulkAttributeConflict(t, pool, meshaTenant, goatID, []map[string]any{
			attrConflictItem("sex", "gender_mismatch", "female", "male"),
		})
		cmd := bulkCmd(meshaTenant, bulkDecisionUseLegacy, "bulk-legacy-sex-1", []string{conflictID}, map[string]int{conflictID: 1})

		result, err := repo.BulkResolveConflicts(ctx, cmd)
		if err != nil {
			t.Fatalf("BulkResolveConflicts: %v", err)
		}
		if result.GoatsMutated != 1 || result.GoatsReturnedClean != 1 {
			t.Fatalf("unexpected result: %#v", result)
		}
		sex, _, state := goatSexBreedState(t, pool, goatID)
		if sex != "male" || state != "clean" {
			t.Fatalf("legacy sex not applied: sex=%s state=%s", sex, state)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM audit_log WHERE action = 'goat.attribute_resolved_from_legacy' AND resource_id = $1 AND after_state->>'sex' = 'male' AND metadata->>'source' = 'legacy_data'`, goatID); got != 1 {
			t.Fatalf("legacy mutation audit rows = %d", got)
		}
	})

	t.Run("use_legacy_value applies approved canonical breed", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		conflictID := insertBulkAttributeConflict(t, pool, meshaTenant, goatID, []map[string]any{
			attrConflictItem("breed", "breed_mismatch", "Synthetic Boer", "Beetal"),
		})
		cmd := bulkCmd(meshaTenant, bulkDecisionUseLegacy, "bulk-legacy-breed-1", []string{conflictID}, map[string]int{conflictID: 1})

		result, err := repo.BulkResolveConflicts(ctx, cmd)
		if err != nil {
			t.Fatalf("BulkResolveConflicts: %v", err)
		}
		if result.GoatsMutated != 1 {
			t.Fatalf("breed not mutated: %#v", result)
		}
		_, breed, _ := goatSexBreedState(t, pool, goatID)
		if breed != "Beetal" {
			t.Fatalf("legacy breed not applied: breed=%s", breed)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goats WHERE goat_id = $1 AND breed = 'Beetal' AND breed_id = '00000000-0000-4000-8000-000000002002'`, goatID); got != 1 {
			t.Fatalf("breed_id not set to approved canonical breed, rows = %d", got)
		}
	})

	t.Run("use_legacy_value rejects a legacy self-conflict and aborts the whole batch", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		conflictID := insertBulkAttributeConflict(t, pool, meshaTenant, goatID, []map[string]any{
			attrConflictItem("sex", "bq_gender_self_conflict", "female", "male|female"),
		})
		cmd := bulkCmd(meshaTenant, bulkDecisionUseLegacy, "bulk-legacy-self-1", []string{conflictID}, map[string]int{conflictID: 1})

		if _, err := repo.BulkResolveConflicts(ctx, cmd); !errors.Is(err, ports.ErrBulkDecisionNotApplicable) {
			t.Fatalf("expected ErrBulkDecisionNotApplicable, got %v", err)
		}
		assertConflictUntouched(t, pool, conflictID, goatID, cmd.TraceID)
	})

	t.Run("use_legacy_value rejects a non-canonical breed", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		conflictID := insertBulkAttributeConflict(t, pool, meshaTenant, goatID, []map[string]any{
			attrConflictItem("breed", "breed_mismatch", "Synthetic Boer", "Anantapur"),
		})
		cmd := bulkCmd(meshaTenant, bulkDecisionUseLegacy, "bulk-legacy-noncanon-1", []string{conflictID}, map[string]int{conflictID: 1})

		if _, err := repo.BulkResolveConflicts(ctx, cmd); !errors.Is(err, ports.ErrBulkBreedNotCanonical) {
			t.Fatalf("expected ErrBulkBreedNotCanonical, got %v", err)
		}
		assertConflictUntouched(t, pool, conflictID, goatID, cmd.TraceID)
		_, breed, _ := goatSexBreedState(t, pool, goatID)
		if breed != "Synthetic Boer" {
			t.Fatalf("breed mutated despite non-canonical reject: %s", breed)
		}
	})

	t.Run("acknowledge_lifecycle_flag resolves a lifecycle conflict without mutating the goat", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		conflictID := insertBulkLifecycleConflict(t, pool, meshaTenant, goatID)
		cmd := bulkCmd(meshaTenant, bulkDecisionAckLifecycle, "bulk-ack-1", []string{conflictID}, map[string]int{conflictID: 1})

		result, err := repo.BulkResolveConflicts(ctx, cmd)
		if err != nil {
			t.Fatalf("BulkResolveConflicts: %v", err)
		}
		if result.GoatsMutated != 0 || result.GoatsReturnedClean != 1 {
			t.Fatalf("unexpected result: %#v", result)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_conflicts WHERE conflict_id = $1 AND state = 'resolved'`, conflictID); got != 1 {
			t.Fatalf("lifecycle conflict not resolved, rows = %d", got)
		}
	})

	t.Run("acknowledge_lifecycle_flag rejects an attribute-context conflict", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		conflictID := insertBulkAttributeConflict(t, pool, meshaTenant, goatID, []map[string]any{
			attrConflictItem("sex", "gender_mismatch", "female", "male"),
		})
		cmd := bulkCmd(meshaTenant, bulkDecisionAckLifecycle, "bulk-ack-wrong-1", []string{conflictID}, map[string]int{conflictID: 1})

		if _, err := repo.BulkResolveConflicts(ctx, cmd); !errors.Is(err, ports.ErrBulkDecisionNotApplicable) {
			t.Fatalf("expected ErrBulkDecisionNotApplicable, got %v", err)
		}
		assertConflictUntouched(t, pool, conflictID, goatID, cmd.TraceID)
	})

	t.Run("stale row_version aborts the whole batch", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		conflictA := insertBulkAttributeConflict(t, pool, meshaTenant, goatID, []map[string]any{
			attrConflictItem("sex", "gender_mismatch", "female", "male"),
		})
		otherGoat := insertSyntheticGoat(t, pool, meshaTenant, cptLocation)
		conflictB := insertBulkAttributeConflict(t, pool, meshaTenant, otherGoat, []map[string]any{
			attrConflictItem("breed", "breed_mismatch", "Synthetic Boer", "Beetal"),
		})
		// conflictB advertises a stale expected row_version.
		cmd := bulkCmd(meshaTenant, bulkDecisionKeepPassport, "bulk-stale-1", []string{conflictA, conflictB}, map[string]int{conflictA: 1, conflictB: 99})

		if _, err := repo.BulkResolveConflicts(ctx, cmd); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected ErrWriteConflict, got %v", err)
		}
		// abort-if-any-invalid: the valid conflict must remain untouched too.
		if got := countRows(t, pool, `SELECT count(*) FROM identity_conflicts WHERE conflict_id = ANY($1::uuid[]) AND state = 'open' AND row_version = 1`, []string{conflictA, conflictB}); got != 2 {
			t.Fatalf("expected both conflicts still open at v1, rows = %d", got)
		}
	})

	t.Run("missing conflict id returns not found and touches nothing", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		conflictID := insertBulkAttributeConflict(t, pool, meshaTenant, goatID, []map[string]any{
			attrConflictItem("sex", "gender_mismatch", "female", "male"),
		})
		missing := "11111111-1111-4111-8111-111111111111"
		cmd := bulkCmd(meshaTenant, bulkDecisionKeepPassport, "bulk-missing-1", []string{conflictID, missing}, map[string]int{conflictID: 1, missing: 1})

		if _, err := repo.BulkResolveConflicts(ctx, cmd); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
		assertConflictUntouched(t, pool, conflictID, goatID, cmd.TraceID)
	})

	t.Run("keep_passport_value keeps the goat in review while another open conflict remains", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		first := insertBulkAttributeConflict(t, pool, meshaTenant, goatID, []map[string]any{
			attrConflictItem("sex", "gender_mismatch", "female", "male"),
		})
		// Second open conflict on the same goat (not part of the batch).
		_ = insertBulkAttributeConflict(t, pool, meshaTenant, goatID, []map[string]any{
			attrConflictItem("breed", "breed_mismatch", "Synthetic Boer", "Beetal"),
		})
		cmd := bulkCmd(meshaTenant, bulkDecisionKeepPassport, "bulk-partial-1", []string{first}, map[string]int{first: 1})

		result, err := repo.BulkResolveConflicts(ctx, cmd)
		if err != nil {
			t.Fatalf("BulkResolveConflicts: %v", err)
		}
		if result.GoatsReturnedClean != 0 {
			t.Fatalf("goat should stay in review, returned clean = %d", result.GoatsReturnedClean)
		}
		_, _, state := goatSexBreedState(t, pool, goatID)
		if state != "needs_review" {
			t.Fatalf("goat state = %s, want needs_review", state)
		}
	})
}

func bulkCmd(tenantID, decisionType, key string, ids []string, versions map[string]int) ports.BulkResolveConflictsCommand {
	trace := "trace-" + key
	return ports.BulkResolveConflictsCommand{
		TenantID:      tenantID,
		ActorID:       correctionActor,
		TraceID:       trace,
		BulkRequestID: trace,
		DecisionType:  decisionType,
		ConflictIDs:   ids,
		RowVersions:   versions,
		Reason:        "synthetic " + decisionType + " after manual evidence review",
	}
}

func attrConflictItem(attribute, reason, localValue, bqValue string) map[string]any {
	return map[string]any{
		"Attribute":  attribute,
		"Reason":     reason,
		"LocalValue": localValue,
		"BQValue":    bqValue,
	}
}

func insertBulkAttributeConflict(t *testing.T, pool *pgxpool.Pool, tenantID, goatID string, items []map[string]any) string {
	t.Helper()
	evidence := map[string]any{
		"source_context": attributeConflictContext,
		"source_system":  "legacy_bigquery",
		"review_note":    "BQ attributes and Goat OS passport attributes disagree.",
		"conflicts":      items,
	}
	return insertBulkConflict(t, pool, tenantID, goatID, evidence)
}

func insertBulkLifecycleConflict(t *testing.T, pool *pgxpool.Pool, tenantID, goatID string) string {
	t.Helper()
	evidence := map[string]any{
		"source_context": lifecycleConflictContext,
		"source_system":  "legacy_bigquery",
		"reason":         "rfid_terminal_then_later_activity",
		"review_note":    "BQ lifecycle evidence has terminal-plus-later activity; review reuse.",
	}
	return insertBulkConflict(t, pool, tenantID, goatID, evidence)
}

func insertBulkConflict(t *testing.T, pool *pgxpool.Pool, tenantID, goatID string, evidence map[string]any) string {
	t.Helper()
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	var conflictID string
	if err := pool.QueryRow(context.Background(), `
INSERT INTO identity_conflicts (
  tenant_id, conflict_type, severity, state, goat_ids, source_record_ids, evidence
) VALUES (
  $1, 'status_mismatch', 'medium', 'open', ARRAY[$2::uuid], ARRAY['synthetic-bulk-source-1'], $3::jsonb
)
RETURNING conflict_id::text`, tenantID, goatID, evidenceJSON).Scan(&conflictID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO identity_conflict_goats (conflict_id, tenant_id, goat_id, role) VALUES ($1, $2, $3, 'affected')`, conflictID, tenantID, goatID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE goats SET identity_state = 'needs_review' WHERE goat_id = $1`, goatID); err != nil {
		t.Fatal(err)
	}
	return conflictID
}

func goatSexBreedState(t *testing.T, pool *pgxpool.Pool, goatID string) (sex, breed, state string) {
	t.Helper()
	var sexPtr, breedPtr *string
	if err := pool.QueryRow(context.Background(), `SELECT sex, breed, identity_state FROM goats WHERE goat_id = $1`, goatID).Scan(&sexPtr, &breedPtr, &state); err != nil {
		t.Fatal(err)
	}
	if sexPtr != nil {
		sex = *sexPtr
	}
	if breedPtr != nil {
		breed = *breedPtr
	}
	return sex, breed, state
}

// assertConflictUntouched verifies a rejected/aborted batch wrote nothing for
// the conflict: still open at v1, goat still needs_review, and no audit trail.
func assertConflictUntouched(t *testing.T, pool *pgxpool.Pool, conflictID, goatID, traceID string) {
	t.Helper()
	if got := countRows(t, pool, `SELECT count(*) FROM identity_conflicts WHERE conflict_id = $1 AND state = 'open' AND row_version = 1 AND resolved_at IS NULL`, conflictID); got != 1 {
		t.Fatalf("conflict was mutated despite abort, open-v1 rows = %d", got)
	}
	if _, _, state := goatSexBreedState(t, pool, goatID); state != "needs_review" {
		t.Fatalf("goat state changed despite abort: %s", state)
	}
	assertNoRows(t, pool, "no audit on aborted bulk batch", `SELECT count(*) FROM audit_log WHERE trace_id = $1`, traceID)
}
