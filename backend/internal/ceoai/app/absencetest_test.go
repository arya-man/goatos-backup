package app

import (
	"fmt"
	"strings"
	"testing"
)

// THE MEASURED DEFECT, AS A TEST. Asked which staff have no backup, the live
// planner wrote this statement and the answer named 13 of the view's 16 rows —
// among them `Backup 6`, a person who IS somebody's backup — because the view
// fills backup_label only on the covered_by_backup rows.
const liveBackupNullDraft = `SELECT role_label AS label, CAST(count(*) AS text) AS value, role_label AS scope ` +
	`FROM ceo_ai.workforce_coverage_status ` +
	`WHERE tenant_id = '11111111-1111-1111-1111-111111111111' AND backup_label IS NULL ` +
	`GROUP BY role_label LIMIT 100`

func TestTheStatementThatNamedTenRolesIsRefused(t *testing.T) {
	err := validateAbsenceTests(liveBackupNullDraft)
	if err == nil {
		t.Fatal("the live ten-role draft was accepted; a filter that returns rows is invisible once it runs")
	}
	// The refusal has to name the column that DOES decide, or the re-plan has
	// nowhere to go and the model writes the same question a third way.
	if !strings.Contains(err.Error(), "coverage_status") {
		t.Errorf("the refusal does not name the authority column: %v", err)
	}
	if !strings.Contains(err.Error(), "backup_label") {
		t.Errorf("the refusal does not name the column it refused: %v", err)
	}
}

// coverageStatement wraps a predicate in a complete, tenant-scoped statement
// against the view that declares the guarded column, so every case below goes
// through exactly what the registry hands the gate.
func coverageStatement(predicate string) string {
	return "SELECT role_label AS label, CAST(count(*) AS text) AS value FROM ceo_ai.workforce_coverage_status w " +
		"WHERE tenant_id = '11111111-1111-1111-1111-111111111111' AND " + predicate + " GROUP BY role_label LIMIT 100"
}

// TestEverySpellingOfTheSameFalseQuestionIsRefused. A model that is told no
// rephrases, and the two rephrasings it reaches for first are exactly the two
// this gate used to miss: after a NULL test is refused, `coalesce` and `nullif`
// are the idiomatic re-drafts, and both return rows. Every entry below was
// measured passing through the previous, shape-matching gate.
func TestEverySpellingOfTheSameFalseQuestionIsRefused(t *testing.T) {
	for _, predicate := range []string{
		// the spellings the shape-matcher did catch
		"backup_label IS NULL",
		"backup_label is null",
		"backup_label = ''",
		"'' = backup_label",
		"coalesce(backup_label, '') = ''",
		"w.backup_label IS NULL",
		"btrim(backup_label) = ''",
		"CASE WHEN backup_label IS NULL THEN 1 ELSE 0 END = 1",

		// the seven it did NOT, confirmed end-to-end by review round 6
		"nullif(backup_label, '') IS NULL",
		"coalesce(backup_label, 'none') = 'none'",
		"backup_label::text IS NULL",
		"length(backup_label) = 0",
		"upper(backup_label) IS NULL",
		"backup_label || '' = ''",
		"coalesce(backup_label) IS NULL",

		// and the neighbourhood around them, which a spelling list would have
		// to grow one entry at a time
		"backup_label IS NOT DISTINCT FROM NULL",
		"lower(backup_label) IS NULL",
		"trim(backup_label) = ''",
		"char_length(coalesce(backup_label, '')) = 0",
		"length(btrim(coalesce(backup_label, ''))) < 1",
		"coalesce(nullif(btrim(backup_label), ''), 'x') = 'x'",
		"nullif(upper(backup_label), '') IS NULL",
		"concat(backup_label, '') = ''",
		"coalesce(backup_label, '') IN ('')",
		"NOT (backup_label IS NOT NULL)",
		"(backup_label IS NULL)",
		"coalesce(backup_label::text, '') = ''",
		"'' = coalesce(backup_label, '')",
		"HAVING_PLACEHOLDER",
	} {
		if predicate == "HAVING_PLACEHOLDER" {
			continue
		}
		sql := coverageStatement(predicate)
		if err := validateAbsenceTests(sql); err == nil {
			t.Errorf("%q was accepted", predicate)
		}
	}
}

// TestAFilterClauseAndAnAggregateAreNotAHidingPlace. The predicate does not
// have to be in the WHERE clause to be the same false question, and it does not
// have to be a predicate at all: count() reads blankness as arithmetic.
func TestAFilterClauseAndAnAggregateAreNotAHidingPlace(t *testing.T) {
	for name, sql := range map[string]string{
		"inside a FILTER clause": "SELECT role_label AS label, CAST(count(*) FILTER (WHERE backup_label IS NULL) AS text) AS value " +
			"FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' GROUP BY role_label LIMIT 100",
		"in a HAVING clause": "SELECT role_label AS label, CAST(count(*) AS text) AS value " +
			"FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' GROUP BY role_label " +
			"HAVING max(coalesce(backup_label, '')) = '' LIMIT 100",
		// Renaming the column does not rename the question.
		"aliased in the projection": "SELECT role_label AS label, backup_label AS bl, CAST(count(*) AS text) AS value " +
			"FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' AND bl IS NULL GROUP BY role_label, backup_label LIMIT 100",
	} {
		if err := validateAbsenceTests(sql); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// TestCountingTheColumnSaysSoInTheRefusal. count() reads blankness as
// arithmetic: it skips the blank rows silently, so count(backup_label) against
// count(*) reproduces the exact ten-versus-four framing as a ratio with no NULL
// keyword written anywhere.
//
// The statement would be refused either way -- count is a function the
// expression reader cannot evaluate, so the column reaches a value through an
// unreadable wrapper -- and the refusal would then say "wraps it in something
// that cannot be read", which tells the model to try a different wrapper. THE
// MESSAGE IS THE POINT: a re-plan is steered by what the refusal says, and this
// one has to say that counting the column IS the question, not that the gate
// could not follow the arithmetic.
func TestCountingTheColumnSaysSoInTheRefusal(t *testing.T) {
	for name, sql := range map[string]string{
		"counted instead of tested": "SELECT role_label AS label, CAST(count(backup_label) AS text) AS value " +
			"FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' GROUP BY role_label LIMIT 100",
		"counted through a wrapper": "SELECT role_label AS label, CAST(count(nullif(backup_label, '')) AS text) AS value " +
			"FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' GROUP BY role_label LIMIT 100",
		"counted bare": "SELECT role_label AS label, count(backup_label) AS value " +
			"FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' GROUP BY role_label LIMIT 100",
	} {
		err := validateAbsenceTests(sql)
		if err == nil {
			t.Errorf("%s was accepted", name)
			continue
		}
		if !strings.Contains(err.Error(), "counting") {
			t.Errorf("%s: the refusal does not say the COUNT is the false question: %v", name, err)
		}
		if !strings.Contains(err.Error(), "coverage_status") {
			t.Errorf("%s: the refusal does not name the authority column: %v", name, err)
		}
	}
}

// TestAWrapperTheGateCannotReadIsRefusedRatherThanWavedThrough. The gate reads
// predicates by evaluating them. An unmodelled function applied to THIS column
// inside a predicate is the one case where "I cannot tell" has to mean no: it
// is precisely the shape every bypass took.
func TestAWrapperTheGateCannotReadIsRefusedRatherThanWavedThrough(t *testing.T) {
	for _, predicate := range []string{
		"regexp_replace(backup_label, '[a-z]', '') = ''",
		"to_char(backup_label, 'FM999') IS NULL",
		"array_length(string_to_array(backup_label, ','), 1) IS NULL",
	} {
		err := validateAbsenceTests(coverageStatement(predicate))
		if err == nil {
			t.Errorf("%q was accepted although the gate cannot read it", predicate)
			continue
		}
		if !strings.Contains(err.Error(), "backup_label") {
			t.Errorf("%q: the refusal does not name the column: %v", predicate, err)
		}
	}
}

// TestTheLegitimateInverseIsAllowed. `backup_label IS NOT NULL` is not "the
// inverse of the same false question" — it is the CORRECT predicate for "which
// roles do have a named backup, and who is it", and backup_label is the only
// column carrying the backup's name. The shape-matching gate refused it, which
// was a false refusal shipped to close a false answer.
func TestTheLegitimateInverseIsAllowed(t *testing.T) {
	for _, predicate := range []string{
		"backup_label IS NOT NULL",
		"backup_label <> ''",
		"backup_label != ''",
		"coalesce(backup_label, '') <> ''",
		"length(backup_label) > 0",
		"nullif(backup_label, '') IS NOT NULL",
		"upper(backup_label) IS NOT NULL",
	} {
		if err := validateAbsenceTests(coverageStatement(predicate)); err != nil {
			t.Errorf("the legitimate inverse %q was refused: %v", predicate, err)
		}
	}
}

// TestTheGateRefusesNothingElse is the half that keeps this narrow. It fires
// only on a test for emptiness, on a column a card declares, and never on a
// legitimate question about the same column or about any other view.
func TestTheGateRefusesNothingElse(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{
			"a real comparison on the same column",
			"SELECT owner_label AS label, backup_label AS value FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' AND backup_label = 'Backup 6' LIMIT 100",
		},
		{
			"a pattern match on the same column",
			"SELECT owner_label AS label, backup_label AS value FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' AND backup_label LIKE 'Backup%' LIMIT 100",
		},
		{
			"a case-folded comparison on the same column",
			"SELECT owner_label AS label, backup_label AS value FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' AND upper(btrim(backup_label)) = 'BACKUP 6' LIMIT 100",
		},
		{
			"a membership test against real values",
			"SELECT owner_label AS label, backup_label AS value FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' AND backup_label IN ('Backup 6', 'Backup 7') LIMIT 100",
		},
		{
			"rendering the column without judging by it",
			"SELECT role_label AS label, backup_label AS value, coverage_status AS scope FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' GROUP BY role_label, backup_label, coverage_status LIMIT 100",
		},
		{
			"ordering by the column",
			"SELECT role_label AS label, backup_label AS value FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' ORDER BY backup_label LIMIT 100",
		},
		{
			"the question the card points at",
			"SELECT role_label AS label, CAST(count(*) AS text) AS value FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' AND coverage_status = 'uncovered_absence' GROUP BY role_label LIMIT 100",
		},
		{
			"a NULL test on a view that declares no such column",
			"SELECT breed AS label, CAST(count(*) AS text) AS value FROM ceo_ai.sales_deal_lines_closed WHERE tenant_id = '1' AND buyer_key IS NULL GROUP BY breed LIMIT 100",
		},
		{
			// The bound on the atom. The atom climbs out of a coalesce
			// wrapper, so it does not stop at the token next to the column —
			// which means it must stop at the boolean boundary, or a statement
			// that merely RENDERS backup_label gets refused because some other
			// predicate later in the clause compares something to ''.
			"another column's empty-string test, in the same clause",
			"SELECT role_label AS label, backup_label AS value FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' AND owner_label <> '' LIMIT 100",
		},
		{
			"another column's NULL test beside a real comparison on this one",
			"SELECT role_label AS label, backup_label AS value FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' AND backup_label = 'Backup 6' AND owner_label IS NULL LIMIT 100",
		},
		{
			"the column's name inside a string literal",
			"SELECT 'backup_label IS NULL' AS label, CAST(count(*) AS text) AS value FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' LIMIT 100",
		},
		{
			// The same word in a literal, in the exact position that WOULD be
			// refused if it were the column. A gate that matched on spelling
			// rather than on what the lexer says a token IS would refuse this.
			"a string literal that happens to read like the column, tested for null",
			"SELECT role_label AS label, CAST(count(*) AS text) AS value FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' AND 'backup_label' IS NULL GROUP BY role_label LIMIT 100",
		},
	} {
		if err := validateAbsenceTests(tc.sql); err != nil {
			t.Errorf("%s was refused: %v", tc.name, err)
		}
	}
}

// TestAColumnNamedInACommentIsNotAPredicate. The lexer had no comment rule, so
// a model that wrote a self-justifying comment — "not using backup_label IS
// NULL here" — had its otherwise-correct statement refused, with a message
// describing a predicate the statement does not contain. sqlguard rejects
// comments a moment later, so this was never reachable in production; a refusal
// that names a predicate the statement does not contain is still dishonest, and
// the ordering that saved it is not a design.
func TestAColumnNamedInACommentIsNotAPredicate(t *testing.T) {
	for _, sql := range []string{
		"SELECT role_label AS label, CAST(count(*) AS text) AS value FROM ceo_ai.workforce_coverage_status w\n" +
			"WHERE tenant_id = '1' AND role_label <> ''  -- backup_label IS NULL is the wrong test\n" +
			"GROUP BY role_label LIMIT 100",
		"SELECT role_label AS label, CAST(count(*) AS text) AS value FROM ceo_ai.workforce_coverage_status w " +
			"/* deliberately not coalesce(backup_label, '') = '' */ WHERE tenant_id = '1' GROUP BY role_label LIMIT 100",
	} {
		if err := validateAbsenceTests(sql); err != nil {
			t.Errorf("a column named only in a comment was read as a predicate: %v", err)
		}
	}
}

// TestACommentCannotPerturbTheIdentityRewrite is the other consumer of the same
// lexer. An identity key named inside a comment must not be folded, and the
// bytes of a comment must survive a rewrite untouched.
func TestACommentCannotPerturbTheIdentityRewrite(t *testing.T) {
	sql := "SELECT animal_key AS label, CAST(latest_weight_kg AS text) AS value FROM ceo_ai.weighing_latest_individual_weight " +
		"WHERE tenant_id = '1' -- animal_key = 'MG-1' would be folded here\n AND animal_key = 'MG-1' LIMIT 100"
	out := normalizeIdentityFilters(sql)
	comment := "-- animal_key = 'MG-1' would be folded here"
	if !strings.Contains(out, comment) {
		t.Fatalf("the comment was rewritten: %s", out)
	}
	if strings.Count(out, "upper(trim(animal_key))") != 1 {
		t.Errorf("the fold did not apply exactly once, outside the comment: %s", out)
	}
}

// ---------------------------------------------------------------------------
// the generated pin
// ---------------------------------------------------------------------------

// nullPreservingWrappers map NULL to NULL and a filled value to a filled value.
// That is a fact about these functions, not about this gate, which is what
// makes the generated assertion below independent of the implementation: for
// EVERY stack of them, `<stack> IS NULL` is true exactly when the column is
// blank, so every one of them is the same false question and must be refused.
var nullPreservingWrappers = []string{
	"upper(%s)", "lower(%s)", "btrim(%s)", "trim(%s)", "%s::text",
	"coalesce(%s)", "(%s)",
}

// emptyPreservingWrappers map the empty string to the empty string and a filled
// value to a filled value, so `<stack> = ”` is a blankness test for every
// stack of them.
var emptyPreservingWrappers = []string{
	"upper(%s)", "lower(%s)", "btrim(%s)", "coalesce(%s, '')",
	"concat(%s, '')", "(%s)", "%s || ''",
}

// wrapperStacks builds every composition of the given wrappers up to depth.
func wrapperStacks(wrappers []string, depth int) []string {
	out := []string{"backup_label"}
	frontier := []string{"backup_label"}
	for d := 0; d < depth; d++ {
		var next []string
		for _, inner := range frontier {
			for _, w := range wrappers {
				next = append(next, fmt.Sprintf(w, inner))
			}
		}
		out = append(out, next...)
		frontier = next
	}
	return out
}

// TestGeneratedWrapperStacksCannotLaunderTheFalseQuestion is the pin that a
// spelling list cannot be. It does not enumerate the phrasings a reviewer
// happened to think of; it generates several hundred of them mechanically from
// two algebraic properties, and asserts both directions on the same space — the
// blankness test is refused, and the ordinary comparison through the identical
// wrapper is not.
func TestGeneratedWrapperStacksCannotLaunderTheFalseQuestion(t *testing.T) {
	refused, allowed := 0, 0

	for _, stack := range wrapperStacks(nullPreservingWrappers, 3) {
		predicate := stack + " IS NULL"
		if err := validateAbsenceTests(coverageStatement(predicate)); err != nil {
			refused++
		} else {
			t.Errorf("a NULL-preserving wrapper laundered the false question: %q", predicate)
		}
		// The inverse through the same wrapper is a real question.
		inverse := stack + " IS NOT NULL"
		if err := validateAbsenceTests(coverageStatement(inverse)); err != nil {
			t.Errorf("the legitimate inverse was refused: %q: %v", inverse, err)
		} else {
			allowed++
		}
	}

	for _, stack := range wrapperStacks(emptyPreservingWrappers, 3) {
		predicate := stack + " = ''"
		if err := validateAbsenceTests(coverageStatement(predicate)); err != nil {
			refused++
		} else {
			t.Errorf("an empty-preserving wrapper laundered the false question: %q", predicate)
		}
		// Comparing the SAME wrapped expression to a real value is an ordinary
		// question and must survive. This is the direction a gate tightened by
		// hand always breaks.
		ordinary := stack + " = 'Backup 6'"
		if err := validateAbsenceTests(coverageStatement(ordinary)); err != nil {
			t.Errorf("an ordinary comparison was refused: %q: %v", ordinary, err)
		} else {
			allowed++
		}
	}

	if refused < 300 || allowed < 300 {
		t.Fatalf("the generated sweep shrank: %d refusals, %d allowances", refused, allowed)
	}
	t.Logf("generated sweep: %d blankness tests refused, %d ordinary questions allowed", refused, allowed)
}

// TestTheThreePointTestIsWhatDecides pins the rule itself, in the terms the
// gate is written in, so a future author can see what "tests for emptiness"
// means without reading the parser: TRUE for a blank column, FALSE for a filled
// one. Nothing else is a blankness test.
func TestTheThreePointTestIsWhatDecides(t *testing.T) {
	guarded := map[string]bool{"backup_label": true}
	judge := func(expr string) blankVerdict {
		toks := significantTokens(lexSQLTokens(expr))
		n, ok := parseSQLExpression(toks)
		if !ok {
			t.Fatalf("could not read %q", expr)
		}
		return judgeBlanknessTest(n, guarded)
	}
	for expr, want := range map[string]blankVerdict{
		"backup_label IS NULL":                       blankTestsForEmptiness,
		"backup_label IS NOT NULL":                   blankNotATest,
		"backup_label = 'Backup 6'":                  blankNotATest,
		"nullif(backup_label, '') IS NULL":           blankTestsForEmptiness,
		"coalesce(backup_label, 'x') = 'x'":          blankTestsForEmptiness,
		"length(backup_label) = 0":                   blankTestsForEmptiness,
		"length(backup_label) > 0":                   blankNotATest,
		"owner_label IS NULL":                        blankNotATest,
		"regexp_replace(backup_label, 'a', '') = ''": blankUnreadable,
	} {
		if got := judge(expr); got != want {
			t.Errorf("%q: verdict %d, want %d", expr, got, want)
		}
	}
}

// TestTheGateIsWiredIntoTheOnlyPathModelSQLTakes. The gate is worth nothing
// unless validateModelSQL calls it: every model-drafted statement goes through
// that one function on its way to the executor, and every other test in this
// file calls validateAbsenceTests DIRECTLY, so every one of them would still
// pass with the call deleted.
//
// This pin existed at the round-6 head and was deleted by the commit that
// introduced the expression evaluator. Deleting the `validateAbsenceTests` call
// from validateModelSQL left this whole package green — ~1300 assertions about
// what the gate SAYS, and not one that it RUNS. Do not remove it again.
func TestTheGateIsWiredIntoTheOnlyPathModelSQLTakes(t *testing.T) {
	err := validateModelSQL(liveBackupNullDraft, map[string]any{"sql": liveBackupNullDraft})
	if err == nil {
		t.Fatal("validateModelSQL accepted the ten-role draft; the gate is not on the path the planner's SQL takes")
	}
	if !strings.Contains(err.Error(), "backup_label") {
		t.Errorf("validateModelSQL refused for some other reason: %v", err)
	}
}

// absenceDraftWith puts one predicate into the statement the live planner
// actually wrote, and runs it through the WHOLE guard sequence rather than
// through validateAbsenceTests alone. Every pin below goes through
// validateModelSQL for two reasons: it keeps the gate's wiring load-bearing for
// these cases too, and it means a pin cannot pass because the test handed the
// judge an atom the real atom splitter would never have produced — which is
// exactly how the inverted CASE survived a round with ~1300 assertions.
func absenceDraftWith(predicate string) string {
	return `SELECT role_label AS label, CAST(count(*) AS text) AS value, role_label AS scope ` +
		`FROM ceo_ai.workforce_coverage_status ` +
		`WHERE tenant_id = '11111111-1111-1111-1111-111111111111' AND ` + predicate +
		` GROUP BY role_label LIMIT 100`
}

func absenceGateVerdict(t *testing.T, predicate string) error {
	t.Helper()
	sql := absenceDraftWith(predicate)
	return validateModelSQL(sql, map[string]any{"sql": sql})
}

// TestInvertingTheCaseArmsDoesNotHideTheBlanknessTest. THE ROUND-7 BYPASS.
// Told no to `backup_label IS NULL`, the natural next draft is a CASE whose
// WHEN condition is the LEGITIMATE inverse and whose ARMS carry the inversion:
// `CASE WHEN backup_label IS NOT NULL THEN 0 ELSE 1 END = 1` is a different
// statement byte for byte and the same wrong answer row for row — the ten roles
// again, `Backup 6` among them.
//
// The gate saw only the WHEN condition, because the atom splitter cut at
// `when`, and judged it — correctly, in isolation — as not a blankness test.
// The fix is that the atom is the WHOLE conditional and the comparison that
// selects an arm, so the evaluation answers about the expression the executor
// would actually run.
func TestInvertingTheCaseArmsDoesNotHideTheBlanknessTest(t *testing.T) {
	for _, predicate := range []string{
		// The inverted arms, in the spellings the refusal text invites.
		"CASE WHEN backup_label IS NOT NULL THEN 0 ELSE 1 END = 1",
		"CASE WHEN backup_label IS NOT NULL THEN 1 ELSE 0 END = 0",
		"CASE WHEN backup_label IS NOT NULL THEN 'has' ELSE 'none' END = 'none'",
		"CASE WHEN backup_label <> '' THEN 0 ELSE 1 END = 1",
		"CASE WHEN length(backup_label) > 0 THEN 0 ELSE 1 END > 0",
		// The same thing behind a grouping parenthesis, which used to stop the
		// atom before the comparison that decides which arm is selected.
		"(CASE WHEN backup_label IS NOT NULL THEN 0 ELSE 1 END) = 1",
		// And the non-inverted spelling, kept so that a fix which only handles
		// inversion is still visibly wrong here.
		"CASE WHEN backup_label IS NULL THEN 1 ELSE 0 END = 1",
		"CASE WHEN backup_label = '' THEN 1 ELSE 0 END = 1",
	} {
		err := absenceGateVerdict(t, predicate)
		if err == nil {
			t.Errorf("ALLOWED %q — this returns the ten roles the live planner returned", predicate)
			continue
		}
		if !strings.Contains(err.Error(), "backup_label") || !strings.Contains(err.Error(), "coverage_status") {
			t.Errorf("%q refused without naming the column or the authority: %v", predicate, err)
		}
	}
}

// TestTheInvertedCaseIsAlsoRefusedInsideAnAggregateFilter. The same inversion
// one level down, where the count never leaves the SELECT list and the wrong
// number arrives as a perfectly ordinary aggregate.
func TestTheInvertedCaseIsAlsoRefusedInsideAnAggregateFilter(t *testing.T) {
	sql := `SELECT role_label AS label, ` +
		`CAST(count(*) FILTER (WHERE CASE WHEN backup_label IS NOT NULL THEN 0 ELSE 1 END = 1) AS text) AS value, ` +
		`role_label AS scope FROM ceo_ai.workforce_coverage_status ` +
		`WHERE tenant_id = '11111111-1111-1111-1111-111111111111' GROUP BY role_label LIMIT 100`
	if err := validateModelSQL(sql, map[string]any{"sql": sql}); err == nil {
		t.Fatal("the inverted CASE inside FILTER (WHERE ...) was allowed; that count is the ten-role answer as a number")
	}
}

// TestAnOrderingComparisonCannotSeparateBlankFromFilled. THE SECOND ROUND-7
// BYPASS, and it was the gate's OWN probe value that hid it. The "filled" leg
// was a string chosen so that no statement could write it — but its first byte
// sorts below every printable character, so every ordering leg answered the
// opposite of what real data would, and `coalesce(backup_label,”) < 'A'` came
// back "true for a filled value too" and was allowed.
//
// In Postgres that predicate is TRUE for exactly the blank rows and FALSE for
// `Backup 6`: a perfect blankness test. The filled probe now stands for a
// GENERIC non-blank value whose collation position is unknown, so a predicate
// the blank value satisfies and which otherwise turns on where a value sorts is
// the same false question with a comparison operator instead of a NULL keyword.
func TestAnOrderingComparisonCannotSeparateBlankFromFilled(t *testing.T) {
	for _, predicate := range []string{
		"coalesce(backup_label, '') < 'A'",
		"coalesce(backup_label, '') < 'Backup'",
		"coalesce(backup_label, '') <= ''",
		"NOT (coalesce(backup_label, '') >= 'A')",
		"coalesce(backup_label, '') BETWEEN '' AND 'A'",
		"coalesce(backup_label, '') NOT BETWEEN 'A' AND 'zzzz'",
		"length(backup_label) < 1",
		"coalesce(length(backup_label), 0) < 1",
	} {
		if err := absenceGateVerdict(t, predicate); err == nil {
			t.Errorf("ALLOWED %q — in Postgres this is TRUE for exactly the blank rows", predicate)
		}
	}
}

// TestAnOrderingQuestionAboutRealValuesIsStillAllowed. The ordering rule must
// not become "no comparison operator may touch this column". A predicate the
// BLANK value does not satisfy is not separating blank from filled, whatever
// operator it uses, and a leader may legitimately ask for a range.
func TestAnOrderingQuestionAboutRealValuesIsStillAllowed(t *testing.T) {
	for _, predicate := range []string{
		"backup_label >= 'A'",
		"backup_label > ''",
		"length(backup_label) > 0",
		"length(backup_label) >= 3",
		"backup_label BETWEEN 'A' AND 'zzzz'",
	} {
		if err := absenceGateVerdict(t, predicate); err != nil {
			t.Errorf("REFUSED %q — the blank value does not satisfy it, so it is not a blankness test: %v", predicate, err)
		}
	}
}

// TestComparingTheColumnToAnotherColumnIsAnOrdinaryQuestion. THE ROUND-7
// DISHONEST REFUSAL. `backup_label = owner_label` asks whether the backup is
// the same person as the owner. It contains no wrapper and tests nothing for
// emptiness, and it was refused with a message saying it "wraps it in something
// that cannot be read as a plain comparison" — a message describing a predicate
// the statement does not contain. A refusal that misdescribes the statement is
// worse than no refusal, because the re-plan has nowhere to go.
//
// The cause was that opacity from ANY other column was reported as laundering
// THIS column. Opacity now launders only when the opaque value is itself
// derived from the guarded column — that is, when something unreadable was
// wrapped AROUND it.
func TestComparingTheColumnToAnotherColumnIsAnOrdinaryQuestion(t *testing.T) {
	for _, predicate := range []string{
		"backup_label = owner_label",
		"backup_label <> owner_label",
		"coalesce(owner_label, backup_label) = 'Backup 6'",
		"backup_label || ' covers ' || owner_label <> ''",
		"substring(backup_label, 1, 6) = 'Backup'",
		"coalesce(backup_label, owner_label) = 'Backup 6'",
	} {
		if err := absenceGateVerdict(t, predicate); err != nil {
			t.Errorf("REFUSED %q — this is not a blankness test: %v", predicate, err)
		}
	}
}

// TestAnUnreadableWrapperIsStillRefusedWhenItIsPointedAtBlankness is the other
// side of the test above: loosening the unreadable case must not reopen it. A
// wrapper this reader cannot evaluate is still refused when what its result is
// compared against could be blank — and the legitimate inverse through the same
// unreadable wrapper still passes, because that is the question with a real
// answer.
func TestAnUnreadableWrapperIsStillRefusedWhenItIsPointedAtBlankness(t *testing.T) {
	guarded := map[string]bool{"backup_label": true}
	judge := func(expr string) blankVerdict {
		toks := significantTokens(lexSQLTokens(expr))
		n, ok := parseSQLExpression(toks)
		if !ok {
			t.Fatalf("could not read %q", expr)
		}
		return judgeBlanknessTest(n, guarded)
	}
	for expr, want := range map[string]blankVerdict{
		"regexp_replace(backup_label, 'a', '') IS NULL":           blankUnreadable,
		"regexp_replace(backup_label, 'a', '') = ''":              blankUnreadable,
		"NOT (regexp_replace(backup_label, 'a', '') IS NOT NULL)": blankUnreadable,
		"regexp_replace(backup_label, 'a', '') IS NOT NULL":       blankNotATest,
		"regexp_replace(backup_label, 'a', '') <> ''":             blankNotATest,
		"regexp_replace(backup_label, '0', '') = 'Backup '":       blankNotATest,
		"backup_label = owner_label":                              blankNotATest,
	} {
		if got := judge(expr); got != want {
			t.Errorf("%q: verdict %d, want %d", expr, got, want)
		}
	}
}
