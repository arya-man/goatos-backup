package domain

import "strings"

// The Toxin report (admin-web /feed/toxin) answers four questions the task list cannot:
// which loads arrived, who tested them, what the strip said, and how the screening is
// keeping up. Its GRAIN IS THE FEED LOAD, not the task.
//
// One load can carry several rounds — an Invalid strip or a reject cancels a round and
// mints a retest — so a per-task report would count the same delivery two or three times
// and read as more work than the farm actually received. Every figure on the page is
// therefore taken from the load's LATEST round, selected by the exact key
// (tenant_id, feed_purchase_id, round_no) that toxin_test_tasks_round_uq already makes
// unique. Cancelled earlier rounds stay readable as history on the task detail; they are
// never a row here.
//
// Backend owns every label, chip and empty line below: the page renders them verbatim.

// Report filter keys. Disjoint and exhaustive over the four task statuses — asserted by
// TestReportFiltersPartitionEveryStatusAndOutcome.
const (
	ReportFilterAll     = "all"
	ReportFilterWaiting = "waiting"
	ReportFilterReview  = "review"
	ReportFilterCleared = "cleared"
	ReportFilterFlagged = "flagged"
)

// ReportFilter is one backend-composed chip above the loads table.
type ReportFilter struct {
	Key   string
	Label string
	// EmptyMessage is what the table says when this slice holds nothing. Per-slice, because
	// "No loads have arrived yet" is a lie under the Flagged chip, where empty is good news.
	EmptyMessage string
}

// ReportFilters is the chip row, in render order. All is first and is the default.
func ReportFilters() []ReportFilter {
	return []ReportFilter{
		{ReportFilterAll, "All loads", "No feed loads have been recorded yet."},
		{ReportFilterWaiting, "Waiting", "Every load has been tested."},
		{ReportFilterReview, "In review", "Nothing is waiting on a reviewer."},
		{ReportFilterCleared, "Cleared", "No load has been cleared yet."},
		{ReportFilterFlagged, "Flagged", "No load has come back positive."},
	}
}

// ReportFilterKeyOrDefault falls back to All for a blank or unknown key, so a stale
// bookmark opens the page rather than erroring.
func ReportFilterKeyOrDefault(key string) string {
	for _, f := range ReportFilters() {
		if f.Key == strings.TrimSpace(key) {
			return f.Key
		}
	}
	return ReportFilterAll
}

// ReportBucketFor places one load's latest round in exactly one chip. The pairing of
// status with outcome is deliberate: 'accepted' alone does not say whether the load is
// safe to feed, so an accepted POSITIVE belongs under Flagged beside the cancelled rounds
// and never under Cleared.
func ReportBucketFor(status, outcome string) string {
	switch status {
	case StatusInProgress:
		return ReportFilterWaiting
	case StatusPendingReview:
		return ReportFilterReview
	case StatusAccepted:
		if outcome == OutcomeNegative {
			return ReportFilterCleared
		}
		return ReportFilterFlagged
	case StatusCancelled:
		// A cancelled round is normally superseded in the same transaction that mints its
		// retest, so it is almost never a load's latest round. If it ever is, the load has
		// an unusable reading and no live round — Flagged, never Cleared.
		return ReportFilterFlagged
	}
	return ReportFilterAll
}

// ReportResultLabel is the chip inside the Result column: what the strip said, in farm
// words, for a load whose latest round is in the given state. Origin carries WHY a waiting
// round exists, which is the difference between a load nobody has started and one whose
// first strip was void.
func ReportResultLabel(status, outcome, origin string) string {
	switch status {
	case StatusInProgress:
		switch origin {
		case OriginInvalidRetest:
			return "Retest due — strip was void"
		case OriginRejectedRetest:
			return "Retest due — sent back"
		}
		return "Not tested yet"
	case StatusPendingReview:
		switch outcome {
		case OutcomePositive:
			return "Positive — in review"
		default:
			return "Negative — in review"
		}
	case StatusAccepted:
		switch outcome {
		case OutcomePositive:
			return "Positive"
		case OutcomeInvalid:
			return "Strip was void"
		default:
			return "Negative"
		}
	case StatusCancelled:
		return "Round cancelled"
	}
	return ""
}

// ReportResultTone maps a row to the three semantic tones the table renders. Kept beside
// the label so a new state cannot gain copy without gaining a tone.
func ReportResultTone(status, outcome string) string {
	switch ReportBucketFor(status, outcome) {
	case ReportFilterCleared:
		return "ok"
	case ReportFilterFlagged:
		return "danger"
	case ReportFilterReview:
		return "info"
	default:
		return "muted"
	}
}
