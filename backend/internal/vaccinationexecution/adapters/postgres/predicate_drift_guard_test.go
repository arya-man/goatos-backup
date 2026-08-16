package postgres

import (
	"regexp"
	"strings"
	"testing"
)

// This file guards against a FOURTH occurrence of the drift that has already hit
// cardSummariesSQL three times:
//
//	round 1: the summary aggregated the paginated page instead of the full filtered set.
//	round 2: the summary applied no work_state/severity/open-only predicates at all.
//	round 3: the summary applied those predicates against a per-row "eff_status" PROXY in its own
//	  "filtered_by_state" CTE instead of the card-grain "work_state"/"severity" the page itself
//	  renders (computed in "stateful"/"classified" from aggregated signals: operator presence,
//	  task_state, completion_rejected, quarantine/ICU, display_open_count, ...). That proxy could
//	  never produce 'blocked' or 'rejected', so filtering the card list to Blocked/Sent Back, or to
//	  at_risk severity, permanently undercounted the badges relative to the list the operator saw.
//
// Rounds 1-3 were each fixed by hand-copying more predicate/derivation text from
// vaccinationExecutionSQL into a parallel pipeline in cardSummariesSQL. Hand-copied SQL can always
// re-diverge on the next edit to either query, so round 3's fix is structural instead: both queries
// now embed the SAME Go string constant, executionClassifiedCTE (completion_candidates through
// classified, including the card-grain work_state/severity derivation), so the classification
// itself cannot diverge -- there is only one copy of that SQL text to edit.
//
// TestSharedClassificationCTEIsEmbeddedVerbatim below is the primary guard for that: it asserts
// both query constants literally start with executionClassifiedCTE, byte for byte. The three
// predicate-block guards further down (raw obligation-row filter, assignment partition match,
// vda_guess partition match) predate the full-sharing refactor and targeted sub-blocks inside what
// were then two SEPARATE "raw"/"located" CTEs. They are structurally redundant now -- both queries
// read those blocks out of the identical shared prefix, so they can only ever pass -- but they are
// kept as a cheap, specific tripwire: if a future change ever re-forks executionClassifiedCTE back
// into per-query copies (defeating the primary guard's purpose), these will name the exact block
// that silently changed on only one side, the same way they did for rounds 1-2.
//
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

// TestSharedClassificationCTEIsEmbeddedVerbatim is the PRIMARY drift guard: it asserts both
// vaccinationExecutionSQL and cardSummariesSQL literally begin with the exact bytes of
// executionClassifiedCTE. As long as this holds, "classified.work_state" and
// "classified.severity" cannot diverge between the page and its card-summary badges, because
// there is only one copy of the SQL text that derives them.
func TestSharedClassificationCTEIsEmbeddedVerbatim(t *testing.T) {
	if !strings.HasPrefix(vaccinationExecutionSQL, executionClassifiedCTE) {
		t.Fatal("vaccinationExecutionSQL no longer starts with executionClassifiedCTE verbatim; " +
			"the page and card-summary classification can now diverge again")
	}
	if !strings.HasPrefix(cardSummariesSQL, executionClassifiedCTE) {
		t.Fatal("cardSummariesSQL no longer starts with executionClassifiedCTE verbatim; " +
			"the page and card-summary classification can now diverge again")
	}
	if !strings.Contains(executionClassifiedCTE, "classified AS (") {
		t.Fatal("executionClassifiedCTE no longer contains the classified CTE; " +
			"card-summary aggregation depends on classified.work_state/severity/display_open_count")
	}
}

// TestRawObligationRowFilterPredicateStaysInSyncBetweenQueries asserts that the tenant/status/
// canceled-batch/canceled-task/operator-scope/due-date/open-window predicate on the "raw" CTE's
// obligation_instances scan is byte-for-byte (modulo whitespace) identical between
// vaccinationExecutionSQL and cardSummariesSQL. Now structurally guaranteed by
// TestSharedClassificationCTEIsEmbeddedVerbatim (both queries read this block out of the same
// executionClassifiedCTE prefix); kept as a named, specific tripwire in case that sharing is ever
// undone.
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
// same partition-label matching condition in both queries. Now structurally guaranteed by
// TestSharedClassificationCTEIsEmbeddedVerbatim; kept as a named tripwire (see file header).
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
// LATERAL fallback used when an obligation has no vaccination_drive_assignment_members row. Now
// structurally guaranteed by TestSharedClassificationCTEIsEmbeddedVerbatim; kept as a named
// tripwire (see file header).
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

// TestClassificationDerivationStaysInSyncBetweenQueries guards the actual defect from round 3:
// the card-grain work_state/severity derivation in "stateful"/"classified" itself. Structurally
// guaranteed by TestSharedClassificationCTEIsEmbeddedVerbatim (both queries read the SAME
// "stateful AS (" ... "classified AS (" ... text), kept as a named tripwire so a future change
// that re-forks classification reports exactly this block as the divergence.
func TestClassificationDerivationStaysInSyncBetweenQueries(t *testing.T) {
	const startMarker = "stateful AS ("

	pageBlock := extractBetween(t, vaccinationExecutionSQL, startMarker, "\n),\nfiltered AS (")
	summaryBlock := extractBetween(t, cardSummariesSQL, startMarker, "\n)\nSELECT")

	if pageBlock != summaryBlock {
		t.Fatalf("card-grain work_state/severity classification has diverged between "+
			"vaccinationExecutionSQL and cardSummariesSQL.\npage:    %s\nsummary: %s", pageBlock, summaryBlock)
	}
}
