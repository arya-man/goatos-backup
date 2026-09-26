package postgres

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestLiveTrackerSQLPreparesAgainstTheRealSchema is the gate the live tracker did not have.
//
// On 2026-09-22 `0def56a5a` added `AND m.canceled_at IS NULL` to the two
// vaccination_drive_assignment_members joins. That table has no canceled_at column -- membership is
// PHYSICALLY removed, cascading from its assignment and from its obligation (000001 baseline) -- so
// every request to /vaccination/live-tracker answered 500 with
// `column m.canceled_at does not exist (SQLSTATE 42703)`. The page was down.
//
// It shipped because every test over this file matches STRINGS. The commit came with
// TestLiveTrackerAssignmentMemberCancellationDoesNotResurrectWork, which asserted the predicate was
// PRESENT -- so the suite went green precisely because the broken text was there. A rule with no
// executable check is not a gate.
//
// PREPARE is the whole test: Postgres resolves every table and column at prepare time without
// running the query, so a reference to a column that does not exist fails here for the same reason
// it failed in production, with the same SQLSTATE. It costs one round trip per statement and needs
// no fixture, because nothing is executed.
//
// Opt-in per repo convention (GOATOS_RUN_POSTGRES_TESTS=1), and Docker-free against an existing
// server via GOATOS_PGTEST_ADMIN_DSN.
func TestLiveTrackerSQLPreparesAgainstTheRealSchema(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Every statement the live tracker serves a section from. A new section belongs in this list:
	// the point is that no live-tracker SQL reaches production unparsed by a real server.
	for name, sql := range map[string]string{
		"cells":          liveTrackerCellsSQL,
		"actors":         liveTrackerActorSQL,
		"combo":          liveTrackerComboSQL,
		"activity":       liveTrackerActivitySQL,
		"filter_options": liveTrackerFilterOptionsSQL,
		"verification":   liveTrackerVerificationSQL,
	} {
		t.Run(name, func(t *testing.T) {
			// A fresh connection per statement: a failed prepare aborts the session's current
			// transaction, and one bad statement must not report the rest as broken too.
			c, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			defer c.Release()
			if _, err := c.Conn().Prepare(ctx, "live_tracker_"+name, sql); err != nil {
				t.Fatalf("live tracker %s SQL does not resolve against the migrated schema: %v", name, err)
			}
		})
	}
}
