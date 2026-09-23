package reporting

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A NULL TEST IS A NO-OP FILTER WEARING ANOTHER COLUMN'S NAME, AND THE ONLY
// PLACE IT COULD BE STOPPED FOR GOOD WAS THE VIEW.
//
// `backup_label` used to be filled only on the covered_by_backup rows, so a
// blank one meant "nobody is away from this role" and not "this role has no
// cover". Asked which staff have no backup the planner wrote `backup_label IS
// NULL` and named 10 of the 16 rows as staffing gaps when 4 are -- among them
// `Backup 6`, a person who IS somebody's backup. Like the `<> 'covered'`
// filter before it, the mistake RETURNS ROWS, so nothing downstream could see
// it fire.
//
// Three repairs were tried at the READING end and each lost the same argument:
// prose on the card, a gate matching the predicate's SHAPE, and a gate that
// read the predicate as an expression and evaluated it three times. Each
// closed the spellings that had been measured, and the next reviewer found
// more (`CASE WHEN backup_label IS NULL THEN 1 END = 1`, `(backup_label IS NOT
// NULL) = false`, `coalesce(backup_label,'') = substring('abc',1,0)`). A model
// can spell one predicate infinitely many ways; a reader-side gate has to
// close all of them.
//
// Migration 000397 removed the AMBIGUITY instead. These tests pin what makes
// that work, at both ends.

// TestTheCardRendersTheBackupVocabularyNotADisclaimer is the cheap half. The
// planner has to guess VALUES, not columns, so the card must SAY the three
// values rather than warn the model off a column.
func TestTheCardRendersTheBackupVocabularyNotADisclaimer(t *testing.T) {
	purpose := workforceCoverageCard(t).Purpose
	for _, value := range []string{"No backup named", "Nobody away"} {
		if !strings.Contains(purpose, value) {
			t.Errorf("the card never renders %q, so a planner cannot write the predicate that asks the question correctly: %q", value, purpose)
		}
	}
	if !strings.Contains(purpose, "never blank") {
		t.Errorf("the card does not say the column is never blank, so a planner has no reason not to test it for emptiness: %q", purpose)
	}
	// coverage_status is still the authority and must still be rendered with
	// its own vocabulary; the two columns agree by construction now, and the
	// card advertising only one of them sends half the questions astray.
	for _, value := range []string{"present", "covered_by_backup", "uncovered_absence"} {
		if !strings.Contains(purpose, value) {
			t.Errorf("the card stopped rendering coverage_status value %q: %q", value, purpose)
		}
	}
}

// TestBackupLabelIsNeverBlankSoEmptinessCannotBeAsked is the DB half and the
// real pin. It is written as a claim about EVERY ROW rather than about the
// view's text, because the text can be rewritten a dozen ways and the property
// is what matters: there must be no row on which an emptiness test can be true.
//
// It seeds the three shapes itself -- present, covered_by_backup and
// uncovered_absence -- so it cannot pass vacuously on an empty cluster the way
// a bare count over whatever rows happen to exist would. The database is this
// test.s own clone and is dropped with it.
//
// Postgres-gated.
func TestBackupLabelIsNeverBlankSoEmptinessCannotBeAsked(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	seedTheThreeCoverageShapes(t, ctx, pool, tenant)

	// Every spelling of "is it empty" the three review rounds found, asked of
	// the live view. Each must return NOTHING -- an empty answer a reader can
	// see -- rather than the ordinary state of a role nobody is away from.
	for _, predicate := range []string{
		"backup_label IS NULL",
		"backup_label = ''",
		"coalesce(backup_label, '') = ''",
		"length(coalesce(backup_label, '')) = 0",
		"nullif(backup_label, '') IS NULL",
		"backup_label <= ''",
		"coalesce(backup_label, '') < 'A'",
		"(CASE WHEN backup_label IS NULL THEN 1 ELSE 0 END) = 1",
		"(CASE WHEN backup_label IS NULL THEN 1 END) = 1",
		"(backup_label IS NOT NULL) = false",
		"(backup_label IS NULL) = true",
		"coalesce(backup_label, '') = substring('abc', 1, 0)",
		"backup_label IS NOT DISTINCT FROM ''",
	} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM ceo_ai.workforce_coverage_status WHERE `+predicate).Scan(&n); err != nil {
			t.Fatalf("%s: %v", predicate, err)
		}
		if n != 0 {
			t.Errorf("`%s` returned %d rows: the column is ambiguous again, and this is the ten-versus-four defect, not a guard bypass",
				predicate, n)
		}
	}

	// And the question the leader actually asked has a correct spelling: the
	// two columns must agree row for row on which roles are uncovered.
	var byLabel, byStatus, total int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE backup_label = 'No backup named'),
		       count(*) FILTER (WHERE coverage_status = 'uncovered_absence'),
		       count(*)
		FROM ceo_ai.workforce_coverage_status`).Scan(&byLabel, &byStatus, &total); err != nil {
		t.Fatalf("compare the two spellings: %v", err)
	}
	if byLabel != byStatus {
		t.Errorf("`backup_label = 'No backup named'` returned %d rows and `coverage_status = 'uncovered_absence'` returned %d of %d: the two columns are computed from the same absence row and must not disagree",
			byLabel, byStatus, total)
	}

	// The whole point is that neither spelling is a no-op filter: a predicate
	// that matched EVERY row would be just as wrong and just as invisible.
	if total > 0 && byLabel == total {
		t.Errorf("every one of the %d rows reads as uncovered; a filter that matches everything returns rows and so cannot be seen to have failed", total)
	}
	t.Logf("%d rows: %d uncovered by both spellings", total, byLabel)
}

// seedTheThreeCoverageShapes puts one row of each kind into the view: a role
// nobody is away from, a role covered by a named backup, and an absence with
// NOBODY covering it. Without all three the assertions above are satisfiable by
// an empty cluster, which is exactly how a test passes by construction.
func seedTheThreeCoverageShapes(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant string) {
	t.Helper()
	member := func(name string) string {
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, metadata, row_version, created_at, updated_at)
			 VALUES (gen_random_uuid(), $1, encode(gen_random_bytes(4),'hex'), $2, 'active', $3, '{}'::jsonb, 1, now(), now())
			 RETURNING workforce_member_id::text`, tenant, name, "operator").Scan(&id); err != nil {
			t.Fatalf("insert member %s: %v", name, err)
		}
		return id
	}
	member("Present Owner")
	coveredOwner := member("Covered Owner")
	uncoveredOwner := member("Uncovered Owner")
	backup := member("Backup 6")

	absence := func(owner string, replacement *string) {
		if _, err := pool.Exec(ctx,
			`INSERT INTO workforce_absences (tenant_id, workforce_member_id, scope_type, scope_id, starts_at, ends_at, reason_code, status, replacement_member_id)
			 VALUES ($1, $2, 'tenant', $1, now() - interval '1 day', now() + interval '1 day', 'leave', 'approved', $3)`,
			tenant, owner, replacement); err != nil {
			t.Fatalf("insert absence for %s: %v", owner, err)
		}
	}
	absence(coveredOwner, &backup)
	absence(uncoveredOwner, nil)

	// Prove the seed really produced all three shapes before asserting on it.
	var present0, covered, uncovered int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE coverage_status = 'present'),
		       count(*) FILTER (WHERE coverage_status = 'covered_by_backup'),
		       count(*) FILTER (WHERE coverage_status = 'uncovered_absence')
		FROM ceo_ai.workforce_coverage_status WHERE tenant_id = $1`, tenant,
	).Scan(&present0, &covered, &uncovered); err != nil {
		t.Fatalf("count the seeded shapes: %v", err)
	}
	if present0 == 0 || covered == 0 || uncovered == 0 {
		t.Fatalf("the seed did not produce all three shapes (present=%d covered=%d uncovered=%d); the assertions below would pass vacuously",
			present0, covered, uncovered)
	}
}

// TestTheDisagreementBetweenTheTwoColumnsIsStructural reads the view's own
// definition and checks that both columns are decided by the SAME absence
// join. It is the claim the old card's prose used to make in English: if
// anyone ever sources backup_label from the member's own row instead, the two
// columns can disagree on real data and the seeded test above may not be
// dense enough to show it.
//
// Postgres-gated.
func TestTheDisagreementBetweenTheTwoColumnsIsStructural(t *testing.T) {
	ctx := context.Background()
	pool, _ := newDB(t, ctx)

	var def string
	if err := pool.QueryRow(ctx,
		`SELECT pg_get_viewdef('ceo_ai.workforce_coverage_status'::regclass, true)`).Scan(&def); err != nil {
		t.Fatalf("read view definition: %v", err)
	}
	if !strings.Contains(def, "replacement_member_id") {
		t.Errorf("the view no longer reads the absence's replacement_member_id, so backup_label and coverage_status are no longer two readings of one fact:\n%s", def)
	}
	// The arms must be exhaustive: a CASE with no ELSE is NULL for an untaken
	// arm, which is precisely the ambiguity this change removed.
	low := strings.ToLower(def)
	backupArm := strings.Index(low, "as backup_label")
	if backupArm < 0 {
		t.Fatalf("backup_label is gone from the view:\n%s", def)
	}
	window := low[:backupArm]
	if !strings.Contains(window, "else") {
		t.Errorf("backup_label's expression has no ELSE arm, so it can still be NULL:\n%s", def)
	}
}
