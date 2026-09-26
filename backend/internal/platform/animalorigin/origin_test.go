package animalorigin

import "testing"

func TestClassifyPutsTheLoadFirstAndGuessesNothing(t *testing.T) {
	cases := []struct {
		onLoad bool
		origin string
		want   string
	}{
		{false, "birth", FarmBorn},
		{false, "procured", ProcuredNoLoad},
		{true, "procured", ProcuredLoad},
		{true, "", ProcuredLoad},
		// A load wins even over a register that says born: the load is what the buyer wrote.
		{true, "birth", ProcuredLoad},
		// The defect this package exists for: no load row is NOT farm born.
		{false, "", ""},
		{false, "imported", ""},
		{false, " Birth ", FarmBorn},
	}
	for _, c := range cases {
		if got := Classify(c.onLoad, c.origin); got != c.want {
			t.Errorf("Classify(%v, %q) = %q, want %q", c.onLoad, c.origin, got, c.want)
		}
	}
}

func TestNormalizeKeepsTheLegacyPurchasedMeaningAndRefusesUnknowns(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "farm_born": FarmBorn, "procured_no_load": ProcuredNoLoad,
		"procured_load": ProcuredLoad, "purchased": ProcuredLoad, " PURCHASED ": ProcuredLoad,
	} {
		got, ok := Normalize(in)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	if _, ok := Normalize("bought"); ok {
		t.Error("an unknown origin must be refused, never widened to no filter")
	}
	if _, ok := Normalize("all"); ok {
		t.Error("an unknown origin must be refused, never widened to no filter")
	}
}
