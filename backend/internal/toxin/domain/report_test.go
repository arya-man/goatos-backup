package domain

import "testing"

// TestReportFiltersPartitionEveryStatusAndOutcome proves the chip row is a PARTITION:
// every state a load's latest round can be in lands in exactly one chip, and no chip is
// unreachable. A report whose chips overlap double-counts a delivery; one with a gap hides
// a load that nobody will then test.
func TestReportFiltersPartitionEveryStatusAndOutcome(t *testing.T) {
	statuses := []string{StatusInProgress, StatusPendingReview, StatusAccepted, StatusCancelled}
	outcomes := []string{"", OutcomeNegative, OutcomePositive, OutcomeInvalid}

	valid := map[string]bool{}
	for _, f := range ReportFilters() {
		if f.Key != ReportFilterAll {
			valid[f.Key] = true
		}
	}
	reached := map[string]bool{}
	for _, status := range statuses {
		for _, outcome := range outcomes {
			bucket := ReportBucketFor(status, outcome)
			if !valid[bucket] {
				t.Fatalf("status %q outcome %q fell outside the chip row (got %q)", status, outcome, bucket)
			}
			reached[bucket] = true
		}
	}
	for key := range valid {
		if !reached[key] {
			t.Fatalf("chip %q is unreachable — no status/outcome pair lands in it", key)
		}
	}
}

// TestAcceptedPositiveIsFlaggedNotCleared is the single most consequential line in the
// report: a positive load that the CEO accepted is an ACCEPTED task, and reading Cleared
// off the status alone would tell the farm a contaminated load is safe to feed.
func TestAcceptedPositiveIsFlaggedNotCleared(t *testing.T) {
	if got := ReportBucketFor(StatusAccepted, OutcomePositive); got != ReportFilterFlagged {
		t.Fatalf("accepted positive = %q, want %q", got, ReportFilterFlagged)
	}
	if got := ReportBucketFor(StatusAccepted, OutcomeNegative); got != ReportFilterCleared {
		t.Fatalf("accepted negative = %q, want %q", got, ReportFilterCleared)
	}
	if got := ReportResultTone(StatusAccepted, OutcomePositive); got != "danger" {
		t.Fatalf("accepted positive tone = %q, want danger", got)
	}
}

// TestWaitingCopyNamesWhyTheRoundIsOpen keeps the three waiting reasons apart. "Not tested
// yet" on a load whose first strip came back void reads as nobody having touched it, and
// hides that a tester already burned a strip on it.
func TestWaitingCopyNamesWhyTheRoundIsOpen(t *testing.T) {
	for _, tc := range []struct{ origin, want string }{
		{OriginPurchase, "Not tested yet"},
		{OriginInvalidRetest, "Retest due — strip was void"},
		{OriginRejectedRetest, "Retest due — sent back"},
	} {
		if got := ReportResultLabel(StatusInProgress, "", tc.origin); got != tc.want {
			t.Fatalf("origin %q = %q, want %q", tc.origin, got, tc.want)
		}
	}
}

// TestEveryChipCarriesItsOwnEmptyLine pins that empty copy is per-slice. A shared line
// ("No feed loads recorded") is actively wrong under Flagged, where empty is good news.
func TestEveryChipCarriesItsOwnEmptyLine(t *testing.T) {
	seen := map[string]string{}
	for _, f := range ReportFilters() {
		if f.Label == "" || f.EmptyMessage == "" {
			t.Fatalf("chip %q is missing a label or empty line", f.Key)
		}
		if prev, dup := seen[f.EmptyMessage]; dup {
			t.Fatalf("chips %q and %q share an empty line: %q", prev, f.Key, f.EmptyMessage)
		}
		seen[f.EmptyMessage] = f.Key
	}
	if ReportFilters()[0].Key != ReportFilterAll {
		t.Fatal("All must be the first chip; it is the default slice")
	}
	if got := ReportFilterKeyOrDefault("not-a-chip"); got != ReportFilterAll {
		t.Fatalf("unknown chip key = %q, want the All fallback", got)
	}
}
