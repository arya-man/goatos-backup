package reporting

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A NO-OP FILTER IS INVISIBLE, AND THAT IS WHY THIS IS PINNED.
//
// Asked which roles have no backup coverage, the planner wrote
// `coverage_status <> 'covered'`. ceo_ai.workforce_coverage_status never emits
// 'covered' — its only values are present, covered_by_backup and
// uncovered_absence — so the predicate matched all 16 rows and the answer
// named 10 roles as uncovered when 4 were: the 9 that are present and the 3
// that are explicitly backed up were reported as staffing gaps. Nothing
// downstream can catch that, because a no-op filter RETURNS ROWS. The only
// place to stop it is before the statement is written, which means the card
// has to carry the column's real vocabulary.

// coverageStatusValues is the vocabulary the card advertises to the planner.
var coverageStatusValues = []string{"covered_by_backup", "present", "uncovered_absence"}

func workforceCoverageCard(t *testing.T) SchemaCard {
	t.Helper()
	for _, c := range Cards() {
		if c.Name == "workforce_coverage_status" {
			return c
		}
	}
	t.Fatal("workforce_coverage_status card is gone")
	return SchemaCard{}
}

// TestTheCoverageStatusCardNamesEveryValueAndNoOther is the cheap half: the
// card must say what the column can be, and must never advertise a value that
// does not exist.
func TestTheCoverageStatusCardNamesEveryValueAndNoOther(t *testing.T) {
	purpose := workforceCoverageCard(t).Purpose
	for _, v := range coverageStatusValues {
		if !strings.Contains(purpose, v) {
			t.Errorf("the card does not name coverage_status value %q, so a planner has to guess it: %q", v, purpose)
		}
	}
	// 'covered' is the guess that shipped. It must not appear as a value of
	// its own — only inside covered_by_backup.
	if regexp.MustCompile(`'covered'|\bcovered\b(?:[^_]|$)`).MatchString(strings.ReplaceAll(purpose, "covered_by_backup", "")) {
		t.Errorf("the card advertises a bare 'covered' status, which the view never emits: %q", purpose)
	}
}

// TestTheCoverageStatusViewEmitsExactlyWhatTheCardAdvertises is the half that
// catches DRIFT: if the view's CASE ever gains, loses or renames a state, the
// card stops being true and this fails. Read from the installed view rather
// than from the migration text, so it reflects what the planner's statements
// will really run against. Postgres-gated.
func TestTheCoverageStatusViewEmitsExactlyWhatTheCardAdvertises(t *testing.T) {
	ctx := context.Background()
	pool, _ := newDB(t, ctx)

	var def string
	if err := pool.QueryRow(ctx,
		`SELECT pg_get_viewdef('ceo_ai.workforce_coverage_status'::regclass, true)`).Scan(&def); err != nil {
		t.Fatalf("read view definition: %v", err)
	}

	// Every string literal in the CASE that produces coverage_status.
	caseStart := strings.Index(def, "CASE")
	caseEnd := strings.Index(def, "AS coverage_status")
	if caseStart < 0 || caseEnd < 0 || caseEnd < caseStart {
		t.Fatalf("could not locate the coverage_status CASE in the view definition:\n%s", def)
	}
	literals := regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(def[caseStart:caseEnd], -1)
	seen := map[string]bool{}
	var got []string
	for _, m := range literals {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		got = append(got, m[1])
	}
	sort.Strings(got)

	want := append([]string(nil), coverageStatusValues...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ceo_ai.workforce_coverage_status emits %v, the card advertises %v — a planner filtering on the card's words would match the wrong rows, and a NEGATED filter would match every row",
			got, want)
	}
	if seen["covered"] {
		t.Error("the view now emits a bare 'covered'; the card and the toolbox tool description must be updated together")
	}
}
