package postgres

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestR50InvalidIndexRecovery reproduces the P0 bug where a failed CREATE INDEX CONCURRENTLY
// leaves an INVALID index. The buggy migration sees the invalid index "exists" and skips rebuilding,
// then drops the working old index, leaving the table with only an invalid unique index that breaks
// all writes. The fixed migration drops the invalid _v2 BEFORE attempting to create a valid one.
func TestR50InvalidIndexRecovery(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Create a minimal table for testing (mimics outbox_messages structure)
	setupSQL := `
		CREATE TABLE IF NOT EXISTS test_table (
			id UUID PRIMARY KEY,
			tenant_id UUID NOT NULL,
			idempotency_key TEXT,
			event_type TEXT,
			created_at TIMESTAMPTZ
		);
		CREATE UNIQUE INDEX IF NOT EXISTS test_table_idx
			ON test_table (tenant_id, idempotency_key)
			WHERE event_type = 'old_event';
	`
	if _, err := pool.Exec(ctx, setupSQL); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Insert rows with a duplicate (tenant_id, idempotency_key) to simulate the pre-existing condition
	insertSQL := `
		INSERT INTO test_table (id, tenant_id, idempotency_key, event_type, created_at) VALUES
		(gen_random_uuid(), 'aaaaaaaa-aaaa-4000-8000-000000000001'::uuid, 'key1', 'old_event', now()),
		(gen_random_uuid(), 'aaaaaaaa-aaaa-4000-8000-000000000001'::uuid, 'key1', 'new_event', now() + interval '1 second');
	`
	if _, err := pool.Exec(ctx, insertSQL); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Simulate a failed CREATE INDEX CONCURRENTLY that leaves an INVALID index
	invalidIndexSQL := `
		CREATE UNIQUE INDEX CONCURRENTLY test_table_idx_v2
			ON test_table (tenant_id, idempotency_key)
			WHERE event_type = ANY (ARRAY['old_event'::text, 'new_event'::text]);
	`
	// We expect this to fail
	_, err := pool.Exec(ctx, invalidIndexSQL)
	if err == nil {
		t.Fatal("expected CREATE INDEX to fail due to duplicate (tenant_id, idempotency_key), but it succeeded")
	}

	// Verify the _v2 index now exists but is INVALID
	var isValid bool
	err = pool.QueryRow(ctx, `
		SELECT indisvalid FROM pg_index
		WHERE indexrelid = (
			SELECT oid FROM pg_class WHERE relname = 'test_table_idx_v2'
		)
	`).Scan(&isValid)
	if err != nil {
		t.Fatalf("failed to query pg_index: %v", err)
	}
	if isValid {
		t.Fatal("expected _v2 index to be INVALID after failed creation, but it is VALID")
	}

	t.Run("BuggyPath_DropsWorkingIndexLeavesBroken", func(t *testing.T) {
		// Now run the BUGGY migration sequence:
		// CREATE ... IF NOT EXISTS (skips the invalid _v2)
		// DROP the old index (removes the working one)
		// Result: table left with only the invalid _v2 index

		// CONCURRENTLY operations cannot run inside a transaction, so run them separately
		if _, err := pool.Exec(ctx, `
			CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS test_table_idx_v2
				ON test_table (tenant_id, idempotency_key)
				WHERE event_type = ANY (ARRAY['old_event'::text, 'new_event'::text]);
		`); err != nil {
			t.Fatalf("CREATE INDEX CONCURRENTLY IF NOT EXISTS failed: %v", err)
		}

		if _, err := pool.Exec(ctx, `
			DROP INDEX CONCURRENTLY IF EXISTS test_table_idx;
		`); err != nil {
			t.Fatalf("DROP INDEX CONCURRENTLY failed: %v", err)
		}

		// Verify: old index is gone
		var oldExists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes WHERE indexname = 'test_table_idx'
			)
		`).Scan(&oldExists)
		if err != nil {
			t.Fatalf("failed to check old index: %v", err)
		}
		if oldExists {
			t.Fatal("old index should be dropped")
		}

		// Verify: _v2 index still exists but is INVALID
		err = pool.QueryRow(ctx, `
			SELECT indisvalid FROM pg_index WHERE indexrelid = (
				SELECT oid FROM pg_class WHERE relname = 'test_table_idx_v2'
			)
		`).Scan(&isValid)
		if err != nil {
			t.Fatalf("failed to query _v2 index: %v", err)
		}
		if isValid {
			t.Fatal("_v2 index should still be INVALID")
		}

		// The buggy migration leaves the table with ONLY an invalid _v2 index.
		// The old working index is gone, so the table is in a broken state.
		// Verify both indexes: old should be gone, _v2 should be INVALID
		var oldStillExists bool
		err = pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes WHERE indexname = 'test_table_idx'
			)
		`).Scan(&oldStillExists)
		if err != nil {
			t.Fatalf("failed to check old index: %v", err)
		}
		if oldStillExists {
			t.Fatal("BUG DEMONSTRATED: old working index was dropped, table left with only invalid index")
		}

		// Verify _v2 is still INVALID
		var v2StillInvalid bool
		err = pool.QueryRow(ctx, `
			SELECT indisvalid FROM pg_index WHERE indexrelid = (
				SELECT oid FROM pg_class WHERE relname = 'test_table_idx_v2'
			)
		`).Scan(&v2StillInvalid)
		if err != nil {
			t.Fatalf("failed to query _v2 index: %v", err)
		}
		if v2StillInvalid {
			t.Fatal("BUG NOT DEMONSTRATED: _v2 should still be INVALID")
		}

		t.Logf("BUG VERIFIED: After buggy migration, old valid index is GONE and only INVALID _v2 index remains. This breaks production!")
	})

	t.Run("FixedMigration_DropsInvalidBeforeCreating", func(t *testing.T) {
		// Clean up and reset for the fixed migration test
		cleanup := `
			DROP TABLE IF EXISTS test_table_fixed CASCADE;
		`
		if _, err := pool.Exec(ctx, cleanup); err != nil {
			t.Fatalf("cleanup failed: %v", err)
		}

		// Recreate the table and simulate the invalid index scenario again
		setupFixed := `
			CREATE TABLE IF NOT EXISTS test_table_fixed (
				id UUID PRIMARY KEY,
				tenant_id UUID NOT NULL,
				idempotency_key TEXT,
				event_type TEXT,
				created_at TIMESTAMPTZ
			);
			CREATE UNIQUE INDEX IF NOT EXISTS test_table_fixed_idx
				ON test_table_fixed (tenant_id, idempotency_key)
				WHERE event_type = 'old_event';
		`
		if _, err := pool.Exec(ctx, setupFixed); err != nil {
			t.Fatalf("setup failed: %v", err)
		}

		insertFixed := `
			INSERT INTO test_table_fixed (id, tenant_id, idempotency_key, event_type, created_at) VALUES
			(gen_random_uuid(), 'bbbbbbbb-bbbb-4000-8000-000000000001'::uuid, 'key1', 'old_event', now()),
			(gen_random_uuid(), 'bbbbbbbb-bbbb-4000-8000-000000000001'::uuid, 'key1', 'new_event', now() + interval '1 second');
		`
		if _, err := pool.Exec(ctx, insertFixed); err != nil {
			t.Fatalf("insert failed: %v", err)
		}

		// Simulate failed CREATE that leaves invalid index
		_, _ = pool.Exec(ctx, `
			CREATE UNIQUE INDEX CONCURRENTLY test_table_fixed_idx_v2
				ON test_table_fixed (tenant_id, idempotency_key)
				WHERE event_type = ANY (ARRAY['old_event'::text, 'new_event'::text]);
		`)

		// Now run the FIXED sequence:
		// 1. DROP the invalid _v2 (if exists)
		// 2. DELETE duplicates
		// 3. CREATE the valid _v2
		// 4. DROP the old index (only after _v2 is valid)

		// Step 1: Drop any leftover invalid _v2 index
		if _, err := pool.Exec(ctx, `
			DROP INDEX CONCURRENTLY IF EXISTS test_table_fixed_idx_v2;
		`); err != nil {
			t.Fatalf("DROP INDEX CONCURRENTLY failed: %v", err)
		}

		// Step 2: Delete duplicates (keep earliest)
		if _, err := pool.Exec(ctx, `
			WITH duplicate_rows AS (
				SELECT id FROM test_table_fixed dupe
				WHERE dupe.idempotency_key IS NOT NULL
				AND EXISTS (
					SELECT 1 FROM test_table_fixed keep
					WHERE keep.tenant_id = dupe.tenant_id
					AND keep.idempotency_key = dupe.idempotency_key
					AND keep.idempotency_key IS NOT NULL
					AND (keep.created_at, keep.id) < (dupe.created_at, dupe.id)
				)
				LIMIT 10000
			)
			DELETE FROM test_table_fixed WHERE id IN (SELECT id FROM duplicate_rows);
		`); err != nil {
			t.Fatalf("DELETE duplicates failed: %v", err)
		}

		// Step 3: Create the new index (should succeed now without duplicates)
		if _, err := pool.Exec(ctx, `
			CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS test_table_fixed_idx_v2
				ON test_table_fixed (tenant_id, idempotency_key)
				WHERE event_type = ANY (ARRAY['old_event'::text, 'new_event'::text]);
		`); err != nil {
			t.Fatalf("CREATE INDEX CONCURRENTLY failed: %v", err)
		}

		// Step 4: Drop the old index only after _v2 is valid
		if _, err := pool.Exec(ctx, `
			DROP INDEX CONCURRENTLY IF EXISTS test_table_fixed_idx;
		`); err != nil {
			t.Fatalf("final DROP INDEX CONCURRENTLY failed: %v", err)
		}

		// Verify: _v2 index exists and is VALID
		var isValidFixed bool
		err := pool.QueryRow(ctx, `
			SELECT indisvalid FROM pg_index WHERE indexrelid = (
				SELECT oid FROM pg_class WHERE relname = 'test_table_fixed_idx_v2'
			)
		`).Scan(&isValidFixed)
		if err != nil {
			t.Fatalf("failed to query _v2 index: %v", err)
		}
		if !isValidFixed {
			t.Fatal("_v2 index should be VALID after fixed migration")
		}

		// Verify: old index is gone
		var oldExists bool
		err = pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes WHERE indexname = 'test_table_fixed_idx'
			)
		`).Scan(&oldExists)
		if err != nil {
			t.Fatalf("failed to check old index: %v", err)
		}
		if oldExists {
			t.Fatal("old index should be dropped")
		}

		// Verify: A second insert with the same (tenant_id, idempotency_key) can be attempted
		// With a valid unique index, the insert would conflict (no row inserted)
		// or succeed with ON CONFLICT DO NOTHING (current row unchanged)
		conflictSQL := `
			INSERT INTO test_table_fixed (id, tenant_id, idempotency_key, event_type, created_at)
			VALUES (gen_random_uuid(), 'bbbbbbbb-bbbb-4000-8000-000000000001'::uuid, 'key1', 'old_event', now())
		`
		// This should fail due to unique constraint
		_, err = pool.Exec(ctx, conflictSQL)
		if err == nil {
			t.Fatal("expected unique constraint violation, but insert succeeded")
		}

		// Verify: only one row per (tenant_id, idempotency_key) exists
		var count int
		err = pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM test_table_fixed
			WHERE tenant_id = 'bbbbbbbb-bbbb-4000-8000-000000000001'::uuid
			AND idempotency_key = 'key1'
		`).Scan(&count)
		if err != nil {
			t.Fatalf("failed to count rows: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 row with tenant_id/idempotency_key, got %d", count)
		}
	})
}
