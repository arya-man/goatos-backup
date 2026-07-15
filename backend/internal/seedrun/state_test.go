package seedrun

import "testing"

// TestEvaluateGate is the VACC-REV-02 closeout/promotion decision table: a failed run is always
// rejected, promotion additionally demands a verified run, and a legacy no-run database is allowed
// through closeout but not promotion.
func TestEvaluateGate(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		hasRun     bool
		latest     string
		verified   bool
		wantReject bool
	}{
		{"closeout rejects failed", ModeCloseout, true, StateFailed, false, true},
		{"closeout allows verified", ModeCloseout, true, StateVerified, true, false},
		{"closeout allows legacy no-run", ModeCloseout, false, "", false, false},
		{"closeout allows mid-run generating", ModeCloseout, true, StateGenerating, false, false},
		{"promotion rejects failed", ModePromotion, true, StateFailed, false, true},
		{"promotion rejects never-verified", ModePromotion, true, StateGenerating, false, true},
		{"promotion rejects legacy no-run", ModePromotion, false, "", false, true},
		{"promotion allows verified", ModePromotion, true, StateVerified, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateGate(tc.mode, tc.hasRun, tc.latest, tc.verified)
			if (got != nil) != tc.wantReject {
				t.Fatalf("reject=%v want=%v (err=%v)", got != nil, tc.wantReject, got)
			}
		})
	}
}
