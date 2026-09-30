package domain

import "testing"

// AT 10 WEEKS A FARM-BORN FEMALE GOES TO NON-PREGNANT, WHATEVER HER STAGE (maintainer decision
// 2026-09-30). The age is the Non-Pregnant stage's own "From (days)" (70 here), never a constant.
func TestGrowthTakesAnOldEnoughFemaleStraightToNonPregnant(t *testing.T) {
	age := func(d int) *int { return &d }
	vocab := []string{"K1", "K2", "K3", "F2", "F2-Female", "Non-Pregnant", "Mother"}
	minAges := map[string]int{"Non-Pregnant": 70}
	ctx := func(stage, sex string, days *int, residents []string, penStage string, head int, ages map[string]int) ShiftTypeContext {
		return ShiftTypeContext{
			Type: ShiftTypeGrowth, DestinationKnown: true,
			DestinationConfiguredStage: penStage, DestinationResidentStages: residents, DestinationHeadCount: head,
			Animals:        []ShiftTypeAnimal{{GoatID: "g", Stage: stage, Sex: sex, AgeDays: days}},
			WritableStages: vocab, StageMinAgeDays: ages,
		}
	}
	for _, stage := range []string{"K2", "K3", "F2", "F2-Female"} {
		d, r := ResolveShiftTypeDecision(ctx(stage, "female", age(70), []string{"Non-Pregnant"}, "", 4, minAges))
		if r != nil || d.TargetStage != "Non-Pregnant" {
			t.Errorf("%s female at 70 days into a Non-Pregnant pen: %+v %+v, want Non-Pregnant", stage, d, r)
		}
	}
	if _, r := ResolveShiftTypeDecision(ctx("K3", "female", age(60), []string{"Non-Pregnant"}, "", 4, minAges)); r == nil {
		t.Error("a 60-day female was taken to Non-Pregnant before the stage's From (days)")
	}
	if _, r := ResolveShiftTypeDecision(ctx("K3", "male", age(90), []string{"Non-Pregnant"}, "", 4, minAges)); r == nil {
		t.Error("a male was taken to Non-Pregnant")
	}
	if _, r := ResolveShiftTypeDecision(ctx("K3", "female", age(90), []string{"Non-Pregnant"}, "", 4, nil)); r == nil {
		t.Error("with no From (days) on Non-Pregnant, the ladder must apply unchanged")
	}
	d, r := ResolveShiftTypeDecision(ctx("K3", "female", age(70), nil, "Non-Pregnant", 0, minAges))
	if r != nil || d.TargetStage != "Non-Pregnant" || d.AdoptPenTag != "Non-Pregnant" {
		t.Errorf("into an EMPTY pen set Non-Pregnant: %+v %+v", d, r)
	}
	// The ordinary ladder is untouched for a young kid.
	d, r = ResolveShiftTypeDecision(ctx("K1", "female", age(5), nil, "", 0, minAges))
	if r != nil || d.TargetStage != "K2" {
		t.Errorf("a young K1 into an empty pen: %+v %+v, want K2", d, r)
	}
}
