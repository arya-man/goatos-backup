package reporting

import (
	"context"
	"strings"
	"testing"
)

// A NULL TEST IS A NO-OP FILTER WEARING ANOTHER COLUMN'S NAME.
//
// With the coverage_status vocabulary in place, "which roles have no backup
// coverage" answers with exactly the 4 uncovered_absence rows — verified live
// on this branch. "Which staff have no backup" does not: the planner reached
// for `backup_label IS NULL` instead and named 13 of the 16 rows, among them
// `Backup 6`, a person who IS somebody's backup. The view fills backup_label
// only on the covered_by_backup rows, so a blank one is the ordinary state of
// a role nobody is away from — and like the `<> 'covered'` filter before it,
// the mistake RETURNS ROWS, so nothing downstream can see it fire. The card
// has to say which column decides.
func TestTheCardSaysCoverageStatusDecidesAndABlankBackupDoesNot(t *testing.T) {
	purpose := workforceCoverageCard(t).Purpose
	if !strings.Contains(purpose, "backup_label") {
		t.Errorf("the card never mentions backup_label, so a planner is free to read its NULL as a coverage gap: %q", purpose)
	}
	// The sentence must name coverage_status as the test BEFORE it disclaims
	// backup_label — a card that merely lists backup_label among its columns
	// (the Cols line already does) tells a planner nothing.
	status := strings.Index(purpose, "coverage_status")
	backup := strings.Index(purpose, "backup_label")
	if status < 0 || backup < 0 || status > backup {
		t.Errorf("the card must name coverage_status as the test BEFORE it disclaims backup_label: %q", purpose)
	}
	if !strings.Contains(strings.ToLower(purpose), "not") {
		t.Errorf("the card does not deny backup_label any authority over coverage: %q", purpose)
	}
}

// TestABlankBackupLabelIsNotACoverageGap is the DB half, and it is written as
// a STRUCTURAL claim rather than a count so that it says something on an empty
// database too: a count-based test passes vacuously on the throwaway cluster
// this lane usually runs against, which is how the card could go on asserting
// something the view had stopped doing.
//
// The view builds backup_label from `rep`, joined through the SAME
// `replacement_member_id` the coverage_status CASE reads to decide
// covered_by_backup. That is why a blank backup is not a gap: the column is
// populated on exactly the covered_by_backup rows and nowhere else. If anyone
// ever sources backup_label from the member's own row instead, the card's
// sentence stops being true and this fails.
//
// Postgres-gated.
func TestABlankBackupLabelIsNotACoverageGap(t *testing.T) {
	ctx := context.Background()
	pool, _ := newDB(t, ctx)

	var def string
	if err := pool.QueryRow(ctx,
		`SELECT pg_get_viewdef('ceo_ai.workforce_coverage_status'::regclass, true)`).Scan(&def); err != nil {
		t.Fatalf("read view definition: %v", err)
	}
	backupAlias := backupLabelSource(t, def)
	if !strings.Contains(def, "JOIN workforce_members "+backupAlias+" ON "+backupAlias+".workforce_member_id = aa.replacement_member_id") {
		t.Errorf("backup_label comes from %q, which is not joined on the absence's replacement_member_id — a blank backup_label may no longer mean 'nobody is away', and the card says it does:\n%s",
			backupAlias, def)
	}

	// And the measurable half, which speaks whenever the database has rows:
	// the two columns must agree on every one of them.
	var disagreeing, total int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE (coalesce(backup_label, '') <> '') <> (coverage_status = 'covered_by_backup')),
		       count(*)
		FROM ceo_ai.workforce_coverage_status`,
	).Scan(&disagreeing, &total); err != nil {
		t.Fatalf("compare backup_label with coverage_status: %v", err)
	}
	if disagreeing > 0 {
		t.Errorf("%d of %d rows have a backup_label that disagrees with coverage_status: the card's sentence is no longer true",
			disagreeing, total)
	}
	t.Logf("backup_label agrees with coverage_status on all %d rows", total)
}

// backupLabelSource returns the table alias the view's backup_label column is
// selected from ("rep" today), read out of the definition rather than assumed.
func backupLabelSource(t *testing.T, def string) string {
	t.Helper()
	const suffix = ".display_name AS backup_label"
	idx := strings.Index(def, suffix)
	if idx < 0 {
		t.Fatalf("backup_label is no longer a <alias>.display_name column; re-read the view before trusting the card:\n%s", def)
	}
	start := idx
	for start > 0 && (isIdentByte(def[start-1]) || def[start-1] == '.') {
		start--
	}
	return strings.TrimSpace(def[start:idx])
}
