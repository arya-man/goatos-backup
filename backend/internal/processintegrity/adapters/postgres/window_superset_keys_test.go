package postgres

import (
	"strings"
	"testing"
)

// TestCanonicalWindowSupersetKeepsAllArmsAsIndexProbeArraysDateShiftStatusMatrix pins the effective-due-date
// superset pre-filter behind /vaccination/action-center(/counts), /vaccination/adherence and
// /control-tower/vaccination. The superset (bare due_at window OR batch moved into the window OR
// drive-assignment member moved into the window) must keep all three arms, for every status the
// canonical read admits, in both the raw grain and the legacy-binding pre-filter.
//
// The override key lists stay "= ANY (ARRAY(SELECT ...))" InitPlan arrays. An uncorrelated
// "IN (SELECT ...)" inside this OR was tried (#415) and seq-scans obligation_instances at scale
// (TestProcessIntegrityCanonicalAggregateQueryPlanUsesIndexesAtScale), so it must not come back.
func TestCanonicalWindowSupersetKeepsAllArmsAsIndexProbeArraysDateShiftStatusMatrix(t *testing.T) {
	for name, sql := range map[string]string{
		"rows":         processIntegrityCanonicalRowsSQL,
		"counts":       processIntegrityCanonicalCountsSQL,
		"rows+counts":  processIntegrityCanonicalRowsAndCountsSQL,
		"adherence":    processIntegrityCanonicalAdherenceSummarySQL,
		"rows+summary": processIntegrityCanonicalRowsAndSummarySQL,
	} {
		if strings.Contains(sql, "oi.batch_id IN (SELECT batch_id FROM due_window_batches)") ||
			strings.Contains(sql, "oi.obligation_id IN (SELECT obligation_id FROM due_window_members)") {
			t.Fatalf("%s: override keys became IN-subqueries inside the OR; that form seq-scans at scale", name)
		}
		for fragment, want := range map[string]int{
			"(oi.due_at <= $5::timestamptz AND ($4::timestamptz IS NULL OR oi.due_at >= $4::timestamptz))":  2,
			"OR oi.batch_id = ANY (ARRAY(SELECT batch_id FROM due_window_batches))":                         2,
			"OR oi.obligation_id = ANY (ARRAY(SELECT obligation_id FROM due_window_members))":               2,
			"oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')": 1,
		} {
			if got := strings.Count(sql, fragment); got < want {
				t.Fatalf("%s: superset arm %q appears %d times, want >= %d", name, fragment, got, want)
			}
		}
	}
}
