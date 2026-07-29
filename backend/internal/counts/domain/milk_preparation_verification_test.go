package domain

import "testing"

func TestMilkPreparationProofsRequireOneDistinctVideoPerApplicableStep(t *testing.T) {
	all := MilkPreparationProofs{
		GoatMilkQuantityProofRef:   "proof-goat-quantity",
		BoilingTemperatureProofRef: "proof-boiling",
		CooledTemperatureProofRef:  "proof-cooled",
		UHTMilkQuantityProofRef:    "proof-uht",
		CitricAcidMixingProofRef:   "proof-citric",
	}
	if err := all.Validate(true); err != nil {
		t.Fatalf("five applicable step videos should validate: %v", err)
	}
	if got := all.OrderedRefs(true); len(got) != 5 {
		t.Fatalf("ordered proof count=%d, want 5", len(got))
	}

	missing := all
	missing.CooledTemperatureProofRef = ""
	if err := missing.Validate(true); err == nil {
		t.Fatal("goat-milk preparation must reject a missing step video")
	}

	noGoatMilk := MilkPreparationProofs{
		UHTMilkQuantityProofRef:  "proof-uht",
		CitricAcidMixingProofRef: "proof-citric",
	}
	if err := noGoatMilk.Validate(false); err != nil {
		t.Fatalf("the two applicable UHT preparation videos should validate: %v", err)
	}
	if got := noGoatMilk.OrderedRefs(false); len(got) != 2 {
		t.Fatalf("ordered proof count=%d, want 2", len(got))
	}

	duplicate := all
	duplicate.CitricAcidMixingProofRef = duplicate.UHTMilkQuantityProofRef
	if err := duplicate.Validate(true); err == nil {
		t.Fatal("one video must not satisfy two preparation steps")
	}
}
