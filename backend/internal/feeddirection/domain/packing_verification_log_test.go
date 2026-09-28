package domain

import "testing"

func TestFeedCheckStatusBucketsCoverEveryCompletionState(t *testing.T) {
	cases := map[string]string{
		"completed":            FeedCheckVerified,
		"pending_verification": FeedCheckAwaitingVerification,
		"rework":               FeedCheckRework,
		"":                     FeedCheckNotDone,
	}
	for raw, want := range cases {
		if got := FeedCheckStatus(raw); got != want {
			t.Fatalf("FeedCheckStatus(%q) = %q, want %q", raw, got, want)
		}
	}
}

// The blind-entry boundary: nothing shows until the FEEDING verdict stands, and packed also needs
// the packing verdict.
func TestFeedCheckFiguresWaitForTheFeedingVerdict(t *testing.T) {
	for _, feeding := range []string{FeedCheckAwaitingVerification, FeedCheckRework, FeedCheckNotDone} {
		if FeedCheckFiguresVisible(feeding) {
			t.Fatalf("figures visible while feeding is %q", feeding)
		}
		if PackedVisible(FeedCheckVerified, feeding) {
			t.Fatalf("packed visible while feeding is %q: it would hand the feeding verifier her answer", feeding)
		}
	}
	if PackedVisible(FeedCheckAwaitingVerification, FeedCheckVerified) {
		t.Fatal("packed visible while the packing verdict is still pending")
	}
	if !PackedVisible(FeedCheckVerified, FeedCheckVerified) || !FeedCheckFiguresVisible(FeedCheckVerified) {
		t.Fatal("figures hidden although both verdicts stand")
	}
}

func TestCountFeedCheckRowsCountsOnlyRowsWithBothTotals(t *testing.T) {
	rows := []FeedCheckRow{
		{PackingStatus: FeedCheckVerified, FeedingStatus: FeedCheckVerified, PackedKg: "10.000", FedKg: "9.800"},
		{PackingStatus: FeedCheckVerified, FeedingStatus: FeedCheckAwaitingVerification},
		{PackingStatus: FeedCheckAwaitingVerification, FeedingStatus: FeedCheckNotDone},
	}
	got := CountFeedCheckRows(rows, "10.000", "10.000", "9.800", "-0.200")
	if got.Rows != 3 || got.Compared != 1 || got.AwaitingFeeding != 1 || got.AwaitingPacking != 1 {
		t.Fatalf("totals = %+v", got)
	}
	if got.DifferenceKg != "-0.200" {
		t.Fatalf("difference not passed through: %+v", got)
	}
}
