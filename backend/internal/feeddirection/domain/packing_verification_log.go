package domain

// FEED VERIFICATION LOG (maintainer decision 2026-09-28): for ONE feed day, every packed bag -- one
// per park, pen and session -- with each feed item's planned quantity from the feed direction sheet
// beside the weight the verifier entered when she approved the packing video.
//
// The bag was packed the DAY BEFORE the feed day (a packer works day P on the sheet the animals eat
// on day P+1), so choosing today shows yesterday's packing. Both days are carried on the result so
// no surface does IST date arithmetic on a browser clock.
//
// THE PLAN IS SHOWN ONLY FOR A DECIDED BAG. The verifier enters a bag's weight blind (2026-08-21);
// this read relaxes that only once her verdict on the bag is cast. A bag awaiting her verdict, sent
// back for rework, or not packed yet carries no planned figure here -- withheld by the query, so no
// renderer can leak it. See permissions.VerificationFeedPackingLog.

// Packing verification bag statuses. Machine keys; the page contract owns the words.
const (
	PackingLogStatusVerified             = "verified"
	PackingLogStatusAwaitingVerification = "awaiting_verification"
	PackingLogStatusRework               = "rework"
	PackingLogStatusNotPacked            = "not_packed"
)

// PackingLogStatus maps a feed_packing_completions.status (or its absence) to the log's bucket.
// Exactly one bucket per bag; the four are disjoint.
func PackingLogStatus(rawCompletionStatus string) string {
	switch rawCompletionStatus {
	case "completed":
		return PackingLogStatusVerified
	case "pending_verification":
		return PackingLogStatusAwaitingVerification
	case "rework":
		return PackingLogStatusRework
	default:
		return PackingLogStatusNotPacked
	}
}

// PackingLogPlanVisible is THE blind-entry boundary for this read: the plan is visible for a bag
// only once its verdict stands. Kept as one named function so the SQL filter it mirrors and the
// test that pins it name the same rule.
func PackingLogPlanVisible(status string) bool {
	return status == PackingLogStatusVerified
}

// PackingLogItem is one feed item of one bag. PlannedKg, EnteredKg and DifferenceKg are decimal
// strings ("" = absent): planned is absent on an undecided bag and on a cell the sheet blocked;
// entered is absent until the verifier records it; difference needs both.
type PackingLogItem struct {
	FeedItemKey   string
	FeedItemLabel string
	PlannedKg     string
	EnteredKg     string
	DifferenceKg  string
	// VarianceAcknowledged is true when the verifier's reading was far from plan and she confirmed
	// it after re-checking the video (2026-09-09).
	VarianceAcknowledged bool
}

// PackingLogBag is one bag: one park, pen and session (and workflow) of the feed day.
type PackingLogBag struct {
	ParkID                     string
	ParkLabel                  string
	ShedID                     string
	ShedLabel                  string
	PartitionLabel             string
	OperationalLocationDisplay string
	SessionNo                  int
	SessionLabel               string
	Workflow                   string
	Status                     string
	VerifiedAt                 string // RFC3339, "" until verified
	VerifiedByName             string
	PlannedTotalKg             string
	EnteredTotalKg             string
	Items                      []PackingLogItem
}

// PackingLogTotals counts the day's bags by status (disjoint; they sum to Bags) and totals the
// planned and entered kilograms over VERIFIED bags only -- the only bags carrying either figure.
type PackingLogTotals struct {
	Bags                 int
	Verified             int
	AwaitingVerification int
	Rework               int
	NotPacked            int
	PlannedKg            string
	EnteredKg            string
}

// PackingVerificationLog is the whole feed day in the caller's park scope. Not paginated: one day
// is bounded by the parks' pens x sessions x items -- physical infrastructure, never herd size --
// so the totals are computed over exactly the rows returned.
type PackingVerificationLog struct {
	FeedDay    string
	PackingDay string
	Bags       []PackingLogBag
	Totals     PackingLogTotals
}

// CountPackingLogBags builds the day's totals from the bags actually returned, so the counts can
// never describe a different set than the table. The kg totals come from the query (window sums
// over the same rows) and are passed through.
func CountPackingLogBags(bags []PackingLogBag, plannedKg, enteredKg string) PackingLogTotals {
	t := PackingLogTotals{Bags: len(bags), PlannedKg: plannedKg, EnteredKg: enteredKg}
	for _, b := range bags {
		switch b.Status {
		case PackingLogStatusVerified:
			t.Verified++
		case PackingLogStatusAwaitingVerification:
			t.AwaitingVerification++
		case PackingLogStatusRework:
			t.Rework++
		default:
			t.NotPacked++
		}
	}
	return t
}
