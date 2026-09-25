package postgres

import (
	"strings"
	"testing"
)

// TestCanonicalWindowSupersetKeysAreHashedSubplansOneToManyPageBoundaryDateShiftParkScopeStatusMatrix
// pins the cold-read fix for /vaccination/action-center(/counts), /vaccination/adherence and
// /control-tower/vaccination. The effective-due-date superset (bare due_at window OR batch moved into
// the window OR drive-assignment member moved into the window) must keep all three arms, for every
// status the canonical read admits, in both the raw grain and the legacy-binding pre-filter, and the
// two override key lists must be uncorrelated IN-subqueries (hashed SubPlans). The old
// "= ANY (ARRAY(SELECT ...))" form was evaluated as a per-row linear scan of a ~6k-element array
// inside the OR and cost ~2.3 s cold on STG; results are byte-identical either way (proved on STG).
// Membership, one-to-many assignment splits, pagination and park/shed scope are downstream of this
// pre-filter and unchanged; their behaviour is covered by the integration tests in this package.
func TestCanonicalWindowSupersetKeysAreHashedSubplansOneToManyPageBoundaryDateShiftParkScopeStatusMatrix(t *testing.T) {
	for name, sql := range map[string]string{
		"rows":        processIntegrityCanonicalRowsSQL,
		"counts":      processIntegrityCanonicalCountsSQL,
		"rows+counts": processIntegrityCanonicalRowsAndCountsSQL,
		"adherence":   processIntegrityCanonicalAdherenceSummarySQL,
	} {
		if strings.Contains(sql, "ARRAY(SELECT batch_id FROM due_window_batches)") ||
			strings.Contains(sql, "ARRAY(SELECT obligation_id FROM due_window_members)") {
			t.Fatalf("%s: override keys regressed to a linear-scan InitPlan array", name)
		}
		for fragment, want := range map[string]int{
			"(oi.due_at <= $5::timestamptz AND ($4::timestamptz IS NULL OR oi.due_at >= $4::timestamptz))":  2,
			"OR oi.batch_id IN (SELECT batch_id FROM due_window_batches)":                                   2,
			"OR oi.obligation_id IN (SELECT obligation_id FROM due_window_members)":                         2,
			"oi.status IN ('scheduled', 'due', 'in_progress', 'deferred', 'completed', 'missed', 'waived')": 1,
		} {
			if got := strings.Count(sql, fragment); got < want {
				t.Fatalf("%s: superset arm %q appears %d times, want >= %d", name, fragment, got, want)
			}
		}
	}
}
