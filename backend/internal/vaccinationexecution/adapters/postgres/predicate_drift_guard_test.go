package postgres

import (
	"regexp"
	"strings"
	"testing"
)

// This file guards against a THIRD occurrence of the drift that has already hit
// cardSummariesSQL twice:
//   round 1: the summary aggregated the paginated page instead of the full filtered set.
//   round 2: the summary applied no work_state/severity/open-only predicates at all.
// Both were fixed by hand-copying predicate text from vaccinationExecutionSQL into
// cardSummariesSQL rather than sharing one Go-level source of truth. Hand-copied SQL
// predicates can silently re-diverge on the next edit to either query, so this test makes
// the specific blocks that MUST stay identical between the two queries fail loudly the
// moment only one side is edited.
//
// Full extraction of the two queries into a single shared CTE fragment is not done here:
// vaccinationExecutionSQL's "raw"/"located" CTEs select several columns (rule_id,
// physical_shed, source_shed_name, sop_version_id, assigned_to, goat_lifecycle_status,
// scanned, proofed, ...) that cardSummariesSQL's narrower "raw"/"located" projection does
// not need, and cardSummariesSQL's card-grain (shed, partition, sop_task_id, batch) is not
// the same grouping grain as the page's (park, shed, partition, batch) "classified" cards.
// Reconciling those two shapes into one fragment is a real refactor, not a copy-paste fix,
// so it is called out as a follow-up rather than attempted under this change's budget.

// extractBetween returns the substring of s strictly between the first occurrence of start
// and the next occurrence of end after it, with each line trimmed of trailing/leading
// whitespace so cosmetic re-indentation does not count as drift.
func extractBetween(t *testing.T, s, start, end string) string {
	t.Helper()
	i := strings.Index(s, start)
	if i < 0 {
		t.Fatalf("marker %q not found", start)
	}
	rest := s[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("marker %q not found after %q", end, start)
	}
	return normalizeSQLBlock(rest[:j])
}

var (
	sqlWhitespaceRE  = regexp.MustCompile(`\s+`)
	sqlLineCommentRE = regexp.MustCompile(`--[^\n]*`)
)

// normalizeSQLBlock strips `--` line comments and collapses whitespace so that a comment
// added/removed/reworded on only one side of the two queries is not reported as predicate
// drift -- only changes to the actual SQL tokens are.
func normalizeSQLBlock(s string) string {
	s = sqlLineCommentRE.ReplaceAllString(s, "")
	return strings.TrimSpace(sqlWhitespaceRE.ReplaceAllString(s, " "))
}

// TestRawObligationRowFilterPredicateStaysInSyncBetweenQueries asserts that the tenant/status/
// canceled-batch/canceled-task/operator-scope/due-date/open-window predicate on the "raw" CTE's
// obligation_instances scan is byte-for-byte (modulo whitespace) identical between
// vaccinationExecutionSQL and cardSummariesSQL. If a future change narrows or widens this
// predicate on only one side, the two queries will silently start scanning different obligation
// populations again -- exactly the class of bug this change fixed.
func TestRawObligationRowFilterPredicateStaysInSyncBetweenQueries(t *testing.T) {
	const startMarker = "WHERE oi.tenant_id = $1::uuid\n    AND oi.status IN"
	const endMarker = "),\nlocated AS ("

	pagePredicate := extractBetween(t, vaccinationExecutionSQL, startMarker, endMarker)
	summaryPredicate := extractBetween(t, cardSummariesSQL, startMarker, endMarker)

	if pagePredicate != summaryPredicate {
		t.Fatalf("raw obligation row filter predicate has diverged between vaccinationExecutionSQL "+
			"and cardSummariesSQL.\npage:    %s\nsummary: %s\n"+
			"If this divergence is intentional, update BOTH queries together and this test's "+
			"expectations; if not, the two queries are about to disagree on which obligations are in scope.",
			pagePredicate, summaryPredicate)
	}
}

// TestAssignmentPartitionMatchStaysInSyncBetweenQueries asserts that the LEFT JOIN
// vaccination_drive_assignments ("assignment") membership join in the "raw" CTE applies the
// same partition-label matching condition in both queries. cardSummariesSQL previously omitted
// this condition entirely, so on a shed split into multiple partitions it could resolve
// conducted_by/assignment_planned_at from the WRONG partition's assignment -- diverging from the
// page query without tripping any work_state/severity test.
func TestAssignmentPartitionMatchStaysInSyncBetweenQueries(t *testing.T) {
	const startMarker = "LEFT JOIN vaccination_drive_assignments assignment"
	const endMarker = "LEFT JOIN LATERAL (\n    SELECT\n      vda_guess.operator_id,"

	pageBlock := extractBetween(t, vaccinationExecutionSQL, startMarker, endMarker)
	summaryBlock := extractBetween(t, cardSummariesSQL, startMarker, endMarker)

	if pageBlock != summaryBlock {
		t.Fatalf("assignment membership join has diverged between vaccinationExecutionSQL and "+
			"cardSummariesSQL.\npage:    %s\nsummary: %s", pageBlock, summaryBlock)
	}
}

// TestVdaGuessPartitionMatchStaysInSyncBetweenQueries is the equivalent guard for the "guess"
// LATERAL fallback used when an obligation has no vaccination_drive_assignment_members row.
func TestVdaGuessPartitionMatchStaysInSyncBetweenQueries(t *testing.T) {
	const startMarker = "FROM vaccination_drive_assignments vda_guess"
	const endMarker = "ORDER BY vda_guess.created_at DESC"

	pageBlock := extractBetween(t, vaccinationExecutionSQL, startMarker, endMarker)
	summaryBlock := extractBetween(t, cardSummariesSQL, startMarker, endMarker)

	if pageBlock != summaryBlock {
		t.Fatalf("vda_guess LATERAL fallback has diverged between vaccinationExecutionSQL and "+
			"cardSummariesSQL.\npage:    %s\nsummary: %s", pageBlock, summaryBlock)
	}
}
