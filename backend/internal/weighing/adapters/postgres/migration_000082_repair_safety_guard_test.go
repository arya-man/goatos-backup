package postgres

import (
	"os"
	"strings"
	"testing"
)

// Migration 000082 rewrites submitted_at inside duplicate open-tag groups, which
// is the same column the live scan/submit path writes. Its safety therefore rests
// on properties that live in the SQL TEXT and are invisible to any Go test: the
// only end-to-end proof is running the procedure against a real database under a
// real concurrent writer. These guards read the file, so the properties cannot be
// silently edited away by someone tidying the migration later.
//
// Both were genuinely absent when the migration was written, and both are the
// kind of defect that only shows up on a big production table under load:
//
//  1. The slice planned from an UNLOCKED read and then wrote the rows it had
//     planned on. An operator write landing in that gap was overwritten with the
//     planned value -- a lost update on the operator's own submit.
//  2. The candidate scan -- the one whole-table statement -- ran BEFORE any
//     timeout was armed and into a TEMP table, so it could run unbounded and, if
//     the run was killed, every retry paid the whole scan again from zero.
func migration000082(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../../../migrations/postgres/000082_weighing_duplicate_open_tag_residual_repair.sql")
	if err != nil {
		t.Fatalf("read migration 000082: %v", err)
	}
	return string(raw)
}

func TestMigration000082LocksTheRowsItPlansOnBeforeItPlans(t *testing.T) {
	sql := migration000082(t)

	lock := strings.Index(sql, "FOR UPDATE OF wo")
	if lock < 0 {
		t.Fatal("migration 000082 plans its slice from an unlocked read of weighing_observations. Phase 1 writes back the accepted_at it read, so an operator write committing between the read and the write is silently overwritten: lock the popped groups' rows (FOR UPDATE OF wo) before planning.")
	}
	plan := strings.Index(sql, "CREATE TEMP TABLE weighing_dup_slice_plan")
	if plan < 0 {
		t.Fatal("migration 000082 no longer builds a slice plan; the two-phase ordering this guard protects cannot be checked")
	}
	if lock > plan {
		t.Fatal("migration 000082 takes its row lock AFTER building the plan. A lock taken after the read it is meant to protect protects nothing.")
	}
	// A stable lock order is what keeps two slices (or a resumed run overlapping a
	// straggler) from deadlocking each other instead of queueing.
	if !strings.Contains(sql[:lock], "ORDER BY wo.observation_id") {
		t.Fatal("migration 000082 locks its rows in an unspecified order; lock in observation_id order so concurrent slices queue rather than deadlock")
	}
}

func TestMigration000082BoundsAndPreservesItsWholeTableCandidateScan(t *testing.T) {
	sql := migration000082(t)

	scan := strings.Index(sql, "weighing_dup_candidates_000082 AS")
	if scan < 0 {
		t.Fatal("migration 000082's candidate scan was renamed or removed; this guard must be updated with it")
	}
	before := sql[:scan]
	if !strings.Contains(before, "set_config('statement_timeout'") || !strings.Contains(before, "set_config('lock_timeout'") {
		t.Fatal("migration 000082 runs its whole-table candidate scan BEFORE lock_timeout/statement_timeout are armed. That statement is the one unbounded read in the migration; arm the timeouts before it, not only inside the slice loop.")
	}
	if strings.Contains(sql, "CREATE TEMP TABLE weighing_dup_candidates") {
		t.Fatal("migration 000082 holds its candidate queue in a TEMP table. The completed scan then dies with the session, so a killed run rescans the whole table from zero on every retry; use an ordinary table committed as soon as it is populated.")
	}
	if !strings.Contains(sql, "IF to_regclass('public.weighing_dup_candidates_000082') IS NULL THEN") {
		t.Fatal("migration 000082 must skip the scan when a previous run left a queue behind, or resuming is just rescanning under another name")
	}
	if !strings.Contains(sql, "DROP TABLE IF EXISTS public.weighing_dup_candidates_000082;") {
		t.Fatal("migration 000082 must drop its queue once drained; a drained work list left on a live schema invites a future reader to treat it as state")
	}
}

// The reason this migration exists at all: 000074 could not repair these groups
// because reopening the winner while a loser still held the open key raises
// 23505 on weighing_observations_one_open_tag_uidx. Phase 1 (freeze the losers)
// must therefore stay ahead of phase 2 (reopen the winner). A refactor that
// reorders them reintroduces exactly the failure the migration was written for.
func TestMigration000082KeepsFreezeBeforeReopen(t *testing.T) {
	sql := migration000082(t)
	freeze := strings.Index(sql, "-- PHASE 1: retire/normalise the losers")
	reopen := strings.Index(sql, "-- PHASE 2: reopen the rightful winner")
	if freeze < 0 || reopen < 0 {
		t.Fatal("migration 000082's two repair phases are no longer identifiable; the ordering that makes 23505 impossible cannot be checked")
	}
	if freeze > reopen {
		t.Fatal("migration 000082 reopens the winner before freezing the losers, so the reopen lands on a key another row still holds open: 23505, the exact hazard that forced 000074 to skip these groups")
	}
}
