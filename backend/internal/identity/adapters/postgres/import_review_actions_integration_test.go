package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const reviewImportRunID = "30000000-0000-4000-8000-0000000000a1"

func TestReviewImportRowWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	insertSyntheticImportRun(t, pool, reviewImportRunID, meshaTenant)

	t.Run("reject terminates a needs_review row and replays", func(t *testing.T) {
		rowID := insertSyntheticImportRow(t, pool, reviewImportRunID, meshaTenant, 11, "needs_review", nil)
		cmd := reviewImportRowCommand(t, "idem-review-reject-0001", reviewImportRunID, rowID, "reject", 1)
		result, err := repo.ReviewImportRow(ctx, cmd)
		if err != nil {
			t.Fatalf("ReviewImportRow reject: %v", err)
		}
		if result.Replayed || result.Row.RowState != "rejected" || result.Row.RowVersion != 2 {
			t.Fatalf("unexpected reject result: %#v", result.Row)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM legacy_import_rows WHERE legacy_row_id = $1 AND processing_state = 'rejected' AND row_version = 2`, rowID); got != 1 {
			t.Fatalf("rejected row state rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM audit_log WHERE action = 'identity.import_row.reject' AND resource_id = $1`, rowID); got != 1 {
			t.Fatalf("reject audit rows = %d", got)
		}
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, rowID)

		replay, err := repo.ReviewImportRow(ctx, cmd)
		if err != nil {
			t.Fatalf("ReviewImportRow reject replay: %v", err)
		}
		if !replay.Replayed || replay.Row.RowState != "rejected" {
			t.Fatalf("unexpected reject replay: %#v", replay)
		}

		// Same idempotency key with a different request body (different action ->
		// different request hash) must conflict instead of replaying.
		changed := reviewImportRowCommand(t, "idem-review-reject-0001", reviewImportRunID, rowID, "fix", 1)
		if _, err := repo.ReviewImportRow(ctx, changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("expected idempotency conflict, got %v", err)
		}
	})

	t.Run("reject with stale row_version conflicts without writing", func(t *testing.T) {
		rowID := insertSyntheticImportRow(t, pool, reviewImportRunID, meshaTenant, 12, "needs_review", nil)
		cmd := reviewImportRowCommand(t, "idem-review-stale-0001", reviewImportRunID, rowID, "reject", 2)
		if _, err := repo.ReviewImportRow(ctx, cmd); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected stale write conflict, got %v", err)
		}
		assertNoRows(t, pool, "stale review idempotency", `SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1`, cmd.StoredIdempotencyKey)
		if got := countRows(t, pool, `SELECT count(*) FROM legacy_import_rows WHERE legacy_row_id = $1 AND processing_state = 'needs_review' AND row_version = 1`, rowID); got != 1 {
			t.Fatalf("stale row unchanged rows = %d", got)
		}
	})

	t.Run("reject of a wrong-tenant row is not found", func(t *testing.T) {
		rowID := insertSyntheticImportRow(t, pool, reviewImportRunID, meshaTenant, 13, "needs_review", nil)
		cmd := reviewImportRowCommand(t, "idem-review-wrong-tenant-0001", reviewImportRunID, rowID, "reject", 1)
		cmd.TenantID = secondTenant
		cmd.StoredIdempotencyKey = secondTenant + ":reviewImportRow:" + rowID + ":idem-review-wrong-tenant-0001"
		if _, err := repo.ReviewImportRow(ctx, cmd); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("expected not found, got %v", err)
		}
	})

	t.Run("fix patches only whitelisted sex and keeps the row in review", func(t *testing.T) {
		rowID := insertSyntheticImportRow(t, pool, reviewImportRunID, meshaTenant, 14, "needs_review", map[string]string{"gender": "unknown"})
		cmd := reviewImportRowCommand(t, "idem-review-fix-0001", reviewImportRunID, rowID, "fix", 1)
		sex := "female"
		cmd.FixSex = &sex
		result, err := repo.ReviewImportRow(ctx, cmd)
		if err != nil {
			t.Fatalf("ReviewImportRow fix: %v", err)
		}
		if result.Row.RowState != "needs_review" || result.Row.RowVersion != 2 {
			t.Fatalf("unexpected fix result: %#v", result.Row)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM legacy_import_rows WHERE legacy_row_id = $1 AND normalized_payload->>'sex' = 'female' AND normalized_payload->>'gender' = 'unknown' AND processing_state = 'needs_review'`, rowID); got != 1 {
			t.Fatalf("fix did not patch canonical sex without rewriting source gender; rows = %d", got)
		}
	})

	t.Run("reapply requeues an eligible needs_review row to pending", func(t *testing.T) {
		rowID := insertSyntheticImportRow(t, pool, reviewImportRunID, meshaTenant, 15, "needs_review", nil)
		cmd := reviewImportRowCommand(t, "idem-review-reapply-0001", reviewImportRunID, rowID, "reapply", 1)
		result, err := repo.ReviewImportRow(ctx, cmd)
		if err != nil {
			t.Fatalf("ReviewImportRow reapply: %v", err)
		}
		if result.Row.RowState != "pending" || result.Row.RowVersion != 2 {
			t.Fatalf("unexpected reapply result: %#v", result.Row)
		}
		// No goat is minted by the endpoint; the row is only requeued for the apply path.
		if got := countRows(t, pool, `SELECT count(*) FROM legacy_import_rows WHERE legacy_row_id = $1 AND processing_state = 'pending'`, rowID); got != 1 {
			t.Fatalf("reapply requeue rows = %d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM legacy_import_rows WHERE legacy_row_id = $1 AND normalized_payload ? 'processing_reasons'`, rowID); got != 0 {
			t.Fatalf("reapply left stale processing_reasons rows = %d", got)
		}
	})

	t.Run("reapply of an ineligible created_goat row conflicts", func(t *testing.T) {
		rowID := insertSyntheticImportRow(t, pool, reviewImportRunID, meshaTenant, 16, "created_goat", nil)
		cmd := reviewImportRowCommand(t, "idem-review-reapply-bad-0001", reviewImportRunID, rowID, "reapply", 1)
		if _, err := repo.ReviewImportRow(ctx, cmd); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected conflict for ineligible reapply, got %v", err)
		}
	})
}

func insertSyntheticImportRun(t *testing.T, pool *pgxpool.Pool, runID, tenantID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO legacy_import_runs (import_run_id, tenant_id, source_name, source_system, source_dataset, policy_version, dry_run, status, row_count, created_goat_count, error_count)
VALUES ($1, $2, 'Synthetic review import', 'legacy_rfid_db', 'rfid_db_first_import', 'phase1-rfid-db-import-v1', false, 'completed', 0, 0, 0)
ON CONFLICT (import_run_id) DO NOTHING`, runID, tenantID); err != nil {
		t.Fatal(err)
	}
}

func insertSyntheticImportRow(t *testing.T, pool *pgxpool.Pool, runID, tenantID string, rowNumber int, state string, extraNormalized map[string]string) string {
	t.Helper()
	normalized := map[string]any{"rfid": "", "normalized_old_tag": "826", "gender": "male", "breed": "Boer", "farm": "CBE", "shed": "S1", "partition": "", "processing_reasons": []string{"duplicate_old_tag_same_scope"}}
	for k, v := range extraNormalized {
		normalized[k] = v
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	var rowID string
	if err := pool.QueryRow(context.Background(), `
INSERT INTO legacy_import_rows (
  tenant_id, import_run_id, row_number, source_system, source_dataset, source_record_id,
  source_row_key, source_key_recipe_version, source_row_version_hash, hash_recipe_version,
  raw_payload, normalized_payload, processing_state
) VALUES (
  $1, $2, $3, 'legacy_rfid_db', 'rfid_db_first_import', $4,
  $5, 'phase1-rfid-source-key-v1', $6, 'phase1-rfid-row-hash-v1',
  '{}'::jsonb, $7::jsonb, $8
)
RETURNING legacy_row_id::text`,
		tenantID, runID, rowNumber,
		"synthetic-review-row-"+itoa(rowNumber),
		"review-source-key-"+itoa(rowNumber),
		"sha256:review"+itoa(rowNumber),
		string(payload), state,
	).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	return rowID
}

func reviewImportRowCommand(t *testing.T, key, runID, rowID, action string, rowVersion int) ports.ReviewImportRowCommand {
	t.Helper()
	rawBody := `{"action":"` + action + `","row_version":` + itoa(rowVersion) + `}`
	route := "/admin/import-runs/" + runID + "/rows/" + rowID + "/review"
	hash, err := app.CanonicalRequestHashWithSubject(meshaTenant, "reviewImportRow", route, rowID, []byte(rawBody))
	if err != nil {
		t.Fatal(err)
	}
	return ports.ReviewImportRowCommand{
		TenantID:             meshaTenant,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: meshaTenant + ":reviewImportRow:" + rowID + ":" + key,
		IdempotencyScope:     "reviewImportRow",
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		ImportRunID:          runID,
		ImportRowID:          rowID,
		Action:               action,
		RowVersion:           rowVersion,
		Reason:               "synthetic import row review",
		EvidenceRefs: []domain.EvidenceRef{{
			EvidenceType: "import_run",
			EvidenceID:   runID,
			SourceSystem: strPtr("legacy_rfid_db"),
		}},
	}
}
