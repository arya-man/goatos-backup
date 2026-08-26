package domain

import "testing"

// The leadership execution table's FOUR buckets, and specifically the two things the operator
// vocabulary (NormalizeSessionStatus) deliberately does differently.
func TestDistributionCompletionStatusKeepsReworkAndNotStartedApart(t *testing.T) {
	cases := map[string]string{
		// No completion row: nobody submitted anything. The operator vocabulary calls this
		// "pending"; here it has its own name so it cannot be confused with a bounced video.
		"":                     DistributionCompletionNotStarted,
		"pending_verification": DistributionCompletionAwaitingVerification,
		// THE ONE THAT MATTERS: 'rework' must NOT fold into not_started. NormalizeSessionStatus
		// folds it into "pending" (from the phone, both mean "my turn again"); on this table that
		// same fold makes a pen whose video the verifier BOUNCED look like a pen nobody went to,
		// which is the exact distinction the screen exists to draw.
		"rework":    DistributionCompletionRework,
		"completed": DistributionCompletionCompleted,
		// An unexpected value lands on not_started rather than on any bucket that reads as
		// progress: over-reporting completion is the failure this table was added to catch.
		"who_knows": DistributionCompletionNotStarted,
	}
	for raw, want := range cases {
		if got := NormalizeDistributionCompletionStatus(raw); got != want {
			t.Errorf("NormalizeDistributionCompletionStatus(%q) = %q, want %q", raw, got, want)
		}
	}

	// The operator vocabulary is unchanged and still merges the two. Asserted here so a future
	// author who "unifies" the two functions sees both contracts in one place and fails loudly.
	if NormalizeSessionStatus("rework") != SessionStatusPending {
		t.Fatal("NormalizeSessionStatus must keep folding rework into pending for the operator list")
	}
	if NormalizeDistributionCompletionStatus("rework") == NormalizeDistributionCompletionStatus("") {
		t.Fatal("the leadership table must not fold rework into not_started")
	}
}

func TestDistributionCompletionStatusFilterVocabulary(t *testing.T) {
	// "" is "every status" and is a valid filter.
	for _, ok := range []string{"", DistributionCompletionNotStarted, DistributionCompletionRework,
		DistributionCompletionAwaitingVerification, DistributionCompletionCompleted} {
		if !IsValidDistributionCompletionStatus(ok) {
			t.Errorf("%q should be a valid filter value", ok)
		}
	}
	if IsValidDistributionCompletionStatus("pending") {
		t.Error("'pending' is the OPERATOR bucket name and must not be accepted here — accepting it " +
			"would quietly answer a rework filter with untouched pens")
	}
}

func TestDistributionSlotOrderIsTheCaptureOrder(t *testing.T) {
	// Three slots, in the order they are shot on the ground, so a reader scanning a row sees the
	// sequence break where it actually happened.
	want := []string{
		DistributionSlotFeedWeightPhoto,
		DistributionSlotFeedVideo,
		DistributionSlotWaterVideo,
	}
	if len(DistributionSlotOrder) != len(want) {
		t.Fatalf("want %d slots, got %d", len(want), len(DistributionSlotOrder))
	}
	for i, slot := range want {
		if DistributionSlotOrder[i] != slot {
			t.Errorf("slot %d = %q, want %q", i, DistributionSlotOrder[i], slot)
		}
	}
}
