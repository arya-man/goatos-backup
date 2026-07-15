package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestVaccinationDeferredGuardBlocksExcludedAnimals is the regression suite for the deferred-exit
// race: block_active_vaccination_for_procurement_excluded_goat() must reject first-time 'deferred'
// vaccination obligations for animals whose lifecycle_status is already terminal (sold/culled/etc.),
// for BOTH goat and sheep species, while still allowing 'deferred' clinical holds for active animals.
// Before migration 000206 the guard's blocked-status list omitted 'deferred', so a stale generation
// job could re-surface an exited animal in Calendar's deferred catch-up branch.
func TestVaccinationDeferredGuardBlocksExcludedAnimals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProcurementCommon(t, ctx, pool)

	// One published vaccination protocol reused across every subtest (distinct animals below).
	versionID, ruleID := seedVaccinationProtocol(t, ctx, pool, "deferred-guard")

	const guardErr = "vaccination_obligation_blocked_for_procurement_excluded_goat"

	t.Run("sold goat cannot receive a new deferred vaccination obligation", func(t *testing.T) {
		goatID := "71000000-0000-4000-8000-000000000201"
		seedLifecycleAnimal(t, ctx, pool, goatID, "goat", "sold")
		err := insertVaccinationObligation(ctx, pool, versionID, ruleID, goatID,
			"deferred-sold-goat", "deferred", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
		if err == nil || !strings.Contains(err.Error(), guardErr) {
			t.Fatalf("deferred insert for sold goat = %v, want procurement exclusion guard", err)
		}
		assertNoOpenObligations(t, ctx, pool, goatID)
	})

	t.Run("culled goat cannot receive a new deferred vaccination obligation", func(t *testing.T) {
		goatID := "71000000-0000-4000-8000-000000000202"
		seedLifecycleAnimal(t, ctx, pool, goatID, "goat", "culled")
		err := insertVaccinationObligation(ctx, pool, versionID, ruleID, goatID,
			"deferred-culled-goat", "deferred", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
		if err == nil || !strings.Contains(err.Error(), guardErr) {
			t.Fatalf("deferred insert for culled goat = %v, want procurement exclusion guard", err)
		}
		assertNoOpenObligations(t, ctx, pool, goatID)
	})

	t.Run("sold sheep cannot receive a new deferred vaccination obligation", func(t *testing.T) {
		goatID := "71000000-0000-4000-8000-000000000203"
		seedLifecycleAnimal(t, ctx, pool, goatID, "sheep", "sold")
		err := insertVaccinationObligation(ctx, pool, versionID, ruleID, goatID,
			"deferred-sold-sheep", "deferred", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
		if err == nil || !strings.Contains(err.Error(), guardErr) {
			t.Fatalf("deferred insert for sold sheep = %v, want procurement exclusion guard", err)
		}
		assertNoOpenObligations(t, ctx, pool, goatID)
	})

	t.Run("culled sheep cannot receive a new deferred vaccination obligation", func(t *testing.T) {
		goatID := "71000000-0000-4000-8000-000000000204"
		seedLifecycleAnimal(t, ctx, pool, goatID, "sheep", "culled")
		err := insertVaccinationObligation(ctx, pool, versionID, ruleID, goatID,
			"deferred-culled-sheep", "deferred", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
		if err == nil || !strings.Contains(err.Error(), guardErr) {
			t.Fatalf("deferred insert for culled sheep = %v, want procurement exclusion guard", err)
		}
		assertNoOpenObligations(t, ctx, pool, goatID)
	})

	t.Run("sick but still-active animal can still receive deferred work", func(t *testing.T) {
		goatID := "71000000-0000-4000-8000-000000000205"
		seedLifecycleAnimal(t, ctx, pool, goatID, "goat", "alive")
		if err := insertVaccinationObligation(ctx, pool, versionID, ruleID, goatID,
			"deferred-sick-active", "deferred", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("deferred insert for sick active animal = %v, want success (clinical hold)", err)
		}
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'`,
			testTenant, goatID); got != 1 {
			t.Fatalf("deferred obligations for active animal = %d, want 1", got)
		}
	})

	t.Run("stale generation after exit is rejected and Calendar stays empty", func(t *testing.T) {
		goatID := "71000000-0000-4000-8000-000000000206"
		// Generation reads an in-care (alive) animal and emits a scheduled obligation.
		seedLifecycleAnimal(t, ctx, pool, goatID, "goat", "alive")
		if err := insertVaccinationObligation(ctx, pool, versionID, ruleID, goatID,
			"stale-gen-scheduled", "scheduled", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("seed scheduled obligation: %v", err)
		}

		// Animal exits (sold).
		mustExec(t, ctx, pool,
			`UPDATE goats SET lifecycle_status='sold', updated_at=now() WHERE tenant_id=$1 AND goat_id=$2`,
			testTenant, goatID)

		// goat.exited cancellation runs (mirrors CancelOpenForGoatAt's open-status set, which
		// includes 'deferred'): every open obligation is terminally canceled.
		affected := cancelOpenForGoat(t, ctx, pool, goatID)
		if affected != 1 {
			t.Fatalf("goat.exited cancellation affected %d rows, want 1", affected)
		}

		// Stale generation now attempts its deferred insert AFTER the exit cancellation completed.
		// The guard must reject it (this is the bug: pre-000206 it would succeed).
		err := insertVaccinationObligation(ctx, pool, versionID, ruleID, goatID,
			"stale-gen-deferred", "deferred", time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC))
		if err == nil || !strings.Contains(err.Error(), guardErr) {
			t.Fatalf("stale deferred insert = %v, want procurement exclusion guard", err)
		}

		// Calendar's deferred catch-up branch selects status IN ('missed','in_progress','deferred');
		// there must be no open row left for it to surface.
		assertNoOpenObligations(t, ctx, pool, goatID)
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'`,
			testTenant, goatID); got != 0 {
			t.Fatalf("deferred obligations after exit = %d, want 0 (Calendar must stay empty)", got)
		}

		// Re-delivery of goat.exited is idempotent: re-running cancellation cancels nothing new and
		// the stale insert stays rejected.
		if again := cancelOpenForGoat(t, ctx, pool, goatID); again != 0 {
			t.Fatalf("idempotent re-cancellation affected %d rows, want 0", again)
		}
		if err := insertVaccinationObligation(ctx, pool, versionID, ruleID, goatID,
			"stale-gen-deferred-retry", "deferred", time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)); err == nil ||
			!strings.Contains(err.Error(), guardErr) {
			t.Fatalf("stale deferred re-insert = %v, want procurement exclusion guard", err)
		}
		assertNoOpenObligations(t, ctx, pool, goatID)
	})

	t.Run("completed vaccination history remains queryable after exit", func(t *testing.T) {
		goatID := "71000000-0000-4000-8000-000000000207"
		seedLifecycleAnimal(t, ctx, pool, goatID, "goat", "alive")
		// A completed dose recorded while the animal was still in care.
		if err := insertVaccinationObligation(ctx, pool, versionID, ruleID, goatID,
			"history-completed", "completed", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("seed completed obligation: %v", err)
		}
		mustExec(t, ctx, pool,
			`UPDATE obligation_instances SET completed_at=TIMESTAMPTZ '2026-06-01 10:00:00+00'
             WHERE tenant_id=$1 AND target_id=$2 AND status='completed'`, testTenant, goatID)

		// Animal exits and cancellation runs.
		mustExec(t, ctx, pool,
			`UPDATE goats SET lifecycle_status='culled', updated_at=now() WHERE tenant_id=$1 AND goat_id=$2`,
			testTenant, goatID)
		// Cancellation touches only open rows; the completed history row is left intact and is never
		// physically deleted.
		if affected := cancelOpenForGoat(t, ctx, pool, goatID); affected != 0 {
			t.Fatalf("cancellation affected %d rows, want 0 (only completed history exists)", affected)
		}

		var status string
		var completedAt time.Time
		if err := pool.QueryRow(ctx,
			`SELECT status, completed_at FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`,
			testTenant, goatID).Scan(&status, &completedAt); err != nil {
			t.Fatalf("query completed history after exit: %v", err)
		}
		if status != "completed" || completedAt.IsZero() {
			t.Fatalf("completed history after exit = status %q completed_at %v, want completed with timestamp",
				status, completedAt)
		}
	})
}

// seedLifecycleAnimal inserts a canonical animal (goat or sheep) with an explicit lifecycle_status so
// the exclusion view/guard can be exercised directly.
func seedLifecycleAnimal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, species, lifecycle string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, custodian_party_id, species, sex, current_location_id, park_id)
VALUES ($1, $2, $3, '00000000-0000-4000-8000-000000001001', $4, 'female', $5, $5)
ON CONFLICT (goat_id) DO UPDATE SET lifecycle_status = EXCLUDED.lifecycle_status, species = EXCLUDED.species`,
		goatID, testTenant, lifecycle, species, testPark)
	if err != nil {
		t.Fatalf("seed %s %s animal %s: %v", lifecycle, species, goatID, err)
	}
}

// insertVaccinationObligation inserts one obligation row against the seeded vaccination protocol at a
// caller-chosen status/due date, so the BEFORE INSERT guard can be probed for each status.
func insertVaccinationObligation(ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, goatID, key, status string, dueAt time.Time) error {
	_, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key
) VALUES (
  $1, $2, $3, 'goat', $4, 'park', $5, $6, $7, $8
)`, testTenant, versionID, ruleID, goatID, testPark, dueAt, status, key)
	return err
}

// cancelOpenForGoat mirrors CancelOpenForGoatAt's terminal-cancel SQL (open-status set includes
// 'deferred') and returns the number of rows canceled, so tests can assert idempotency.
func cancelOpenForGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) int64 {
	t.Helper()
	tag, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status = 'canceled', row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1
  AND target_type = 'goat'
  AND target_id = $2
  AND status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed')`, testTenant, goatID)
	if err != nil {
		t.Fatalf("cancel open for goat %s: %v", goatID, err)
	}
	return tag.RowsAffected()
}

func assertNoOpenObligations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) {
	t.Helper()
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances
         WHERE tenant_id=$1 AND target_id=$2 AND status NOT IN ('canceled', 'completed', 'superseded', 'waived')`,
		testTenant, goatID); got != 0 {
		t.Fatalf("open (non-terminal) obligations for %s = %d, want 0", goatID, got)
	}
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}
