package domain

import (
	"slices"
	"testing"
)

// A pregnant female is PAST Non-Pregnant (the reverse edge returns her there, it does not make her
// owe the move); every rung below Non-Pregnant is before it; nothing on the kid ladder changes.
func TestGrowthStagesBeforeIgnoresTheReverseEdge(t *testing.T) {
	np := GrowthStagesBefore("Non-Pregnant")
	if slices.Contains(np, "Pregnant") {
		t.Fatalf("Pregnant counted as before Non-Pregnant: %v", np)
	}
	for _, st := range []string{"K0", "K1", "K2", "K3", "F2", "F2-Female"} {
		if !slices.Contains(np, st) {
			t.Errorf("%s missing from before Non-Pregnant: %v", st, np)
		}
	}
	if got := GrowthStagesBefore("K2"); !slices.Equal(got, []string{"K0", "K1"}) {
		t.Fatalf("before K2 = %v", got)
	}
	if !IsGrowthLadderStage("Mother") || IsGrowthLadderStage("ICU-Kid") {
		t.Fatal("ladder membership wrong for Mother / ICU-Kid")
	}
}
