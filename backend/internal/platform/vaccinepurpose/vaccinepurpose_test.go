package vaccinepurpose

import "testing"

func TestResolve(t *testing.T) {
	delay := int32(21)
	withPlans := Policy{PurposePlans: map[string]Plan{
		"fattening": {FirstWave: StringList{"ET+TT", "PPR"}, GoatSecondWave: StringList{"Goat Pox"}, SheepSecondWave: StringList{"Sheep Pox"}},
		"breeding":  {FirstWave: StringList{"ET+TT"}, SecondWaveAfterDays: &delay, GoatSecondWave: StringList{"PPR"}},
	}}
	legacy := Policy{FirstWave: StringList{"ET+TT", "PPR"}, GoatSecondWave: StringList{"Goat Pox"}}
	for _, tc := range []struct {
		name                           string
		policy                         Policy
		purpose, species, vaccine      string
		applicable, governed, secondWv bool
	}{
		{"non_breeding", withPlans, "non_breeding", "goat", "ET+TT", false, false, false},
		{"fattening_first", withPlans, "fattening", "goat", "et_tt", true, true, false},
		{"fattening_goat_pox", withPlans, "Fattening", "goat", "GOAT_POX", true, true, true},
		{"fattening_sheep_gets_no_goat_pox", withPlans, "fattening", "sheep", "Goat Pox", false, true, false},
		{"fattening_sheep_pox", withPlans, "fattening", "sheep", "Sheep Pox", true, true, true},
		{"fattening_excludes_fmd", withPlans, "fattening", "goat", "FMD", false, true, false},
		{"fattening_missing_plan", Policy{PurposePlans: map[string]Plan{"breeding": {FirstWave: StringList{"ET+TT"}}}}, "fattening", "goat", "ET+TT", false, false, false},
		{"fattening_legacy_top_level", legacy, "fattening", "goat", "Goat Pox", true, true, true},
		{"fattening_nothing_authored", Policy{}, "fattening", "goat", "ET+TT", false, false, false},
		{"breeding_authored", withPlans, "breeding", "goat", "PPR", true, true, true},
		{"breeding_authored_excludes", withPlans, "breeding", "goat", "FMD", false, true, false},
		{"breeding_ungoverned", legacy, "breeding", "goat", "FMD", true, false, false},
		{"unspecified_ungoverned", withPlans, "", "goat", "FMD", true, false, false},
		{"unknown_purpose", withPlans, "dairy", "goat", "ET+TT", false, false, false},
		{"empty_plan", Policy{PurposePlans: map[string]Plan{"breeding": {}}}, "breeding", "goat", "ET+TT", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := Resolve(tc.policy, tc.purpose, tc.species, tc.vaccine)
			if d.Applicable != tc.applicable || d.PlanGoverned != tc.governed || d.SecondWave != tc.secondWv {
				t.Fatalf("decision=%#v, want applicable=%v governed=%v second=%v", d, tc.applicable, tc.governed, tc.secondWv)
			}
			if !d.Applicable && d.Reason == "" {
				t.Fatal("non-applicable decision must carry a reason")
			}
		})
	}
}

func TestPlanDelayAndSpecies(t *testing.T) {
	if (Plan{}).Delay() != DefaultSecondWaveAfterDays {
		t.Fatal("default delay")
	}
	p := Plan{GoatSecondWave: StringList{"Goat Pox"}, SheepSecondWave: StringList{"Sheep Pox"}}
	if !Contains(p.SecondWave("Sheep"), "sheep_pox") || !Contains(p.SecondWave(""), "goat pox") {
		t.Fatal("species second wave selection")
	}
}
