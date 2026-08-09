package domain

import "testing"

// The client's capture gate rests on this mapping, so it is pinned here rather than left implicit.
//
// A verifier-rejected session must reach the operator's list as PENDING. If raw 'rework' ever
// passed through as its own bucket, the phone's feedSessionCanCapture would still admit it (that is
// asserted on the client side too), but every list filter, chip and count that speaks the
// three-bucket vocabulary would silently gain a fourth value nothing renders.
func TestNormalizeSessionStatusMergesReworkIntoPending(t *testing.T) {
	cases := map[string]string{
		"rework":               SessionStatusPending,
		"":                     SessionStatusPending,
		"something_unexpected": SessionStatusPending,
		"pending":              SessionStatusPending,
		"pending_verification": SessionStatusAwaitingVerification,
		"completed":            SessionStatusCompleted,
	}
	for raw, want := range cases {
		if got := NormalizeSessionStatus(raw); got != want {
			t.Errorf("NormalizeSessionStatus(%q) = %q, want %q", raw, got, want)
		}
	}
}
