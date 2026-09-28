package domain

import "testing"

func TestPackingLogStatusBucketsAreDisjointAndCoverEveryCompletionState(t *testing.T) {
	cases := map[string]string{
		"completed":            PackingLogStatusVerified,
		"pending_verification": PackingLogStatusAwaitingVerification,
		"rework":               PackingLogStatusRework,
		"":                     PackingLogStatusNotPacked,
	}
	for raw, want := range cases {
		if got := PackingLogStatus(raw); got != want {
			t.Fatalf("PackingLogStatus(%q) = %q, want %q", raw, got, want)
		}
	}
}

// The blind-entry boundary: the plan is visible ONLY once the verdict stands.
func TestPackingLogPlanIsVisibleOnlyForAVerifiedBag(t *testing.T) {
	for _, status := range []string{PackingLogStatusAwaitingVerification, PackingLogStatusRework, PackingLogStatusNotPacked} {
		if PackingLogPlanVisible(status) {
			t.Fatalf("plan visible for %q; an undecided bag must never carry its plan", status)
		}
	}
	if !PackingLogPlanVisible(PackingLogStatusVerified) {
		t.Fatal("plan hidden for a verified bag")
	}
}

func TestCountPackingLogBagsSumsToTheBagsReturned(t *testing.T) {
	bags := []PackingLogBag{
		{Status: PackingLogStatusVerified}, {Status: PackingLogStatusVerified},
		{Status: PackingLogStatusAwaitingVerification}, {Status: PackingLogStatusRework},
		{Status: PackingLogStatusNotPacked},
	}
	got := CountPackingLogBags(bags, "10.000", "9.500")
	if got.Bags != 5 || got.Verified != 2 || got.AwaitingVerification != 1 || got.Rework != 1 || got.NotPacked != 1 {
		t.Fatalf("totals = %+v", got)
	}
	if got.Verified+got.AwaitingVerification+got.Rework+got.NotPacked != got.Bags {
		t.Fatal("buckets do not sum to the bag count")
	}
	if got.PlannedKg != "10.000" || got.EnteredKg != "9.500" {
		t.Fatalf("kg totals not passed through: %+v", got)
	}
}
