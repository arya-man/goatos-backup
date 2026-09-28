package domain

// FEED VERIFICATION LOG (maintainer decisions 2026-09-28): for ONE feed day, one row per park, pen
// and session with three TOTALS side by side --
//
//	planned  the feed direction sheet's total for that pen-session;
//	packed   the total the verifier entered when she approved YESTERDAY's packing video (the sum of
//	         her per-feed readings on that bag);
//	fed      the total the verifier entered when she approved TODAY's feeding video (one combined
//	         weight; the feed is mixed by the trough);
//
// and the question the maintainer asked: did yesterday's packed total reach the animals today --
// fed minus packed.
//
// Feed day D is the day the animals eat: the bag was packed on D-1 (feed_packing_completions for
// target_date D) and fed on D (feed_distribution_completions for target_date D). Both days travel on
// the result so no surface does IST date arithmetic.
//
// THE NUMBERS APPEAR ONLY ONCE THE FEEDING VERDICT STANDS. Both verifiers enter blind. A row whose
// feeding is not yet approved carries no planned, packed or fed figure -- the packed total would
// hand the feeding verifier her answer before she weighs the feed. Packed additionally needs the
// packing verdict to stand. Withheld by the query and again in the repository, never by a renderer.
// See permissions.VerificationFeedPackingLog.

// Per-stage statuses. Machine keys; the page contract owns the words.
const (
	FeedCheckVerified             = "verified"
	FeedCheckAwaitingVerification = "awaiting_verification"
	FeedCheckRework               = "rework"
	FeedCheckNotDone              = "not_done"
)

// FeedCheckStatus maps a completion status (or its absence) to its stage bucket. The same four
// buckets serve packing and feeding; they are disjoint.
func FeedCheckStatus(rawCompletionStatus string) string {
	switch rawCompletionStatus {
	case "completed":
		return FeedCheckVerified
	case "pending_verification":
		return FeedCheckAwaitingVerification
	case "rework":
		return FeedCheckRework
	default:
		return FeedCheckNotDone
	}
}

// FeedCheckFiguresVisible is THE blind-entry boundary: a row's figures are visible only once its
// FEEDING verdict stands (the last check in the chain). Packed additionally needs its own verdict;
// see PackedVisible.
func FeedCheckFiguresVisible(feedingStatus string) bool {
	return feedingStatus == FeedCheckVerified
}

// PackedVisible: the packed total shows only when both verdicts stand.
func PackedVisible(packingStatus, feedingStatus string) bool {
	return FeedCheckFiguresVisible(feedingStatus) && packingStatus == FeedCheckVerified
}

// FeedCheckRow is one park, pen and session (and workflow) of the feed day. Kg figures are decimal
// strings; "" means absent, never zero.
type FeedCheckRow struct {
	ParkID                     string
	ParkLabel                  string
	ShedID                     string
	ShedLabel                  string
	PartitionLabel             string
	OperationalLocationDisplay string
	SessionNo                  int
	SessionLabel               string
	Workflow                   string
	PackingStatus              string
	FeedingStatus              string
	PlannedKg                  string
	PackedKg                   string
	FedKg                      string
	// DifferenceKg is fed minus packed: positive means more reached the trough than was packed.
	DifferenceKg string
}

// FeedCheckTotals: Rows is every pen-session of the day; Compared counts rows with BOTH packed and
// fed visible (the rows the kg totals range over, so planned/packed/fed are always summed over the
// same set and their difference is meaningful).
type FeedCheckTotals struct {
	Rows            int
	Compared        int
	AwaitingPacking int
	AwaitingFeeding int
	PlannedKg       string
	PackedKg        string
	FedKg           string
	DifferenceKg    string
}

// PackingVerificationLog is the whole feed day in the caller's park scope. Not paginated: one day is
// bounded by the parks' pens x sessions -- physical infrastructure, never herd size.
type PackingVerificationLog struct {
	FeedDay    string
	PackingDay string
	Rows       []FeedCheckRow
	Totals     FeedCheckTotals
}

// CountFeedCheckRows builds the status counts from exactly the rows returned. The kg totals come
// from the query (sums over the compared rows) and are passed through.
func CountFeedCheckRows(rows []FeedCheckRow, planned, packed, fed, diff string) FeedCheckTotals {
	t := FeedCheckTotals{Rows: len(rows), PlannedKg: planned, PackedKg: packed, FedKg: fed, DifferenceKg: diff}
	for _, r := range rows {
		if r.PackedKg != "" && r.FedKg != "" {
			t.Compared++
		}
		if r.PackingStatus == FeedCheckAwaitingVerification {
			t.AwaitingPacking++
		}
		if r.FeedingStatus == FeedCheckAwaitingVerification {
			t.AwaitingFeeding++
		}
	}
	return t
}
