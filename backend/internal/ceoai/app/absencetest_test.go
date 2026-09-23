package app

import (
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

// TestEverySpellingOfTheSameFalseQuestionIsRefused. A model that is told no
// rephrases, so the gate reads the PREDICATE rather than one wording of it.
func TestEverySpellingOfTheSameFalseQuestionIsRefused(t *testing.T) {
	for _, predicate := range []string{
		"backup_label IS NULL",
		"backup_label is null",
		"backup_label IS NOT NULL",
		"backup_label = ''",
		"backup_label <> ''",
		"backup_label != ''",
		"'' = backup_label",
		"coalesce(backup_label, '') = ''",
		"w.backup_label IS NULL",
	} {
		sql := "SELECT role_label AS label, CAST(count(*) AS text) AS value FROM ceo_ai.workforce_coverage_status w " +
			"WHERE tenant_id = '1' AND " + predicate + " GROUP BY role_label LIMIT 100"
		if err := validateAbsenceTests(sql); err == nil {
			t.Errorf("%q was accepted", predicate)
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
			"rendering the column without judging by it",
			"SELECT role_label AS label, backup_label AS value, coverage_status AS scope FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' GROUP BY role_label, backup_label, coverage_status LIMIT 100",
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
			// The bound on the look-ahead. The scan steps over a coalesce
			// wrapper, so it does not stop at the token next to the column —
			// which means it must stop at everything else, or a statement that
			// merely RENDERS backup_label gets refused because some other
			// predicate later in the clause compares something to ''.
			"another column's empty-string test, in the same clause",
			"SELECT role_label AS label, backup_label AS value FROM ceo_ai.workforce_coverage_status WHERE tenant_id = '1' AND owner_label <> '' LIMIT 100",
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

// TestTheScanStopsAtAnythingItCannotStepOver pins the look-ahead's NARROWNESS
// directly, because it is hard to reach from a whole statement and easy to
// widen by accident. The scan steps over a coalesce wrapper's comma, literal
// and parens; everything else ends it. Widening the default to keep going
// turns the gate from "this column is tested for emptiness" into "an empty
// string appears somewhere nearby", which refuses statements that never asked.
func TestTheScanStopsAtAnythingItCannotStepOver(t *testing.T) {
	if comparesToEmptyString([]string{"||", "'x'", "=", "''"}) {
		t.Error("the scan walked past an operator it does not understand and charged the '' to this column")
	}
	if comparesToEmptyString([]string{"AND", "owner_label", "=", "''"}) {
		t.Error("the scan walked into the next predicate")
	}
	// What it MUST still step over: the coalesce wrapper it exists for.
	if !comparesToEmptyString([]string{",", "''", ")", "=", "''"}) {
		t.Error("the scan no longer sees through a coalesce wrapper")
	}
}

// TestTheGateIsWiredIntoTheOnlyPathModelSQLTakes. The gate is worth nothing
// unless validateModelSQL calls it: every model-drafted statement goes through
// that one function on its way to the executor, and every test above would
// still pass with the call deleted.
func TestTheGateIsWiredIntoTheOnlyPathModelSQLTakes(t *testing.T) {
	err := validateModelSQL(liveBackupNullDraft, map[string]any{"sql": liveBackupNullDraft})
	if err == nil {
		t.Fatal("validateModelSQL accepted the ten-role draft; the gate is not on the path the planner's SQL takes")
	}
	if !strings.Contains(err.Error(), "backup_label") {
		t.Errorf("validateModelSQL refused for some other reason: %v", err)
	}
}
