package app

import (
	"context"
	"testing"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func purposeTestPolicy() genProcurementPolicy {
	delay := int32(21)
	fatteningDelay := int32(28)
	return genProcurementPolicy{PurposePlans: map[string]genProcurementPurposePlan{
		"breeding": {FirstWave: genStringList{"ET+TT"}, SecondWaveAfterDays: &delay, GoatSecondWave: genStringList{"PPR"}},
		"fattening": {
			FirstWave: genStringList{"ET+TT", "PPR"}, SecondWaveAfterDays: &fatteningDelay,
			GoatSecondWave: genStringList{"Goat Pox"}, SheepSecondWave: genStringList{"Sheep Pox"},
		},
	}}
}

func TestProcurementPurposeDecisionFailsClosed(t *testing.T) {
	breedingOnly := genProcurementPolicy{PurposePlans: map[string]genProcurementPurposePlan{
		"breeding": {FirstWave: genStringList{"ET+TT"}},
	}}
	for _, tc := range []struct {
		name     string
		purpose  string
		policy   genProcurementPolicy
		want     bool
		governed bool
	}{
		{name: "non_breeding", purpose: "non_breeding", policy: purposeTestPolicy()},
		{name: "fattening_without_plan_on_purpose_plan_version", purpose: "fattening", policy: breedingOnly},
		{name: "fattening_without_any_authored_plan", purpose: "fattening", policy: genProcurementPolicy{}},
		{name: "unknown_purpose", purpose: "dairy", policy: purposeTestPolicy()},
		{name: "legacy_fattening_top_level", purpose: "fattening", policy: genProcurementPolicy{FirstWave: genStringList{"ET+TT"}}, want: true, governed: true},
		{name: "unspecified_without_plan_keeps_schedule", purpose: "", policy: purposeTestPolicy(), want: true},
		{name: "breeding_without_plan_keeps_schedule", purpose: "breeding", policy: genProcurementPolicy{}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			goat := domain.EligibleGoat{Species: "goat", ProcurementPurpose: tc.purpose}
			got := procurementPurposeDecision(goat, vaccineProfile{Code: "ET_TT", Name: "ET+TT"}, tc.policy)
			if got.Applicable != tc.want || got.PlanGoverned != tc.governed {
				t.Fatalf("decision=%#v, want applicable=%v governed=%v", got, tc.want, tc.governed)
			}
		})
	}
}

func TestAuthoredBreedingPlanGovernsBreedingAnimals(t *testing.T) {
	policy := purposeTestPolicy()
	goat := domain.EligibleGoat{Species: "goat", ProcurementPurpose: "breeding"}
	if d := procurementPurposeDecision(goat, vaccineProfile{Name: "ET+TT"}, policy); !d.Applicable || !d.PlanGoverned || d.SecondWave {
		t.Fatalf("ET+TT decision=%#v, want governed first wave", d)
	}
	d := procurementPurposeDecision(goat, vaccineProfile{Name: "PPR"}, policy)
	if !d.Applicable || !d.PlanGoverned || !d.SecondWave || d.Plan.Delay() != 21 {
		t.Fatalf("PPR decision=%#v, want governed second wave with 21d delay", d)
	}
	for _, excluded := range []string{"FMD", "Goat Pox", "HS"} {
		if procurementPurposeDecision(goat, vaccineProfile{Name: excluded}, policy).Applicable {
			t.Fatalf("%s must not be applicable to a breeding goat under an authored breeding plan", excluded)
		}
	}
	sheep := domain.EligibleGoat{Species: "sheep", ProcurementPurpose: "breeding"}
	if procurementPurposeDecision(sheep, vaccineProfile{Name: "PPR"}, policy).Applicable {
		t.Fatal("goat-only breeding second wave must not apply to sheep")
	}
}

func TestPurposeSecondWaveFloorRequiresCompleteFirstWave(t *testing.T) {
	policy := purposeTestPolicy()
	goat := domain.EligibleGoat{Species: "goat", ProcurementPurpose: "fattening"}
	decision := procurementPurposeDecision(goat, vaccineProfile{Code: "GOAT_POX", Name: "Goat Pox"}, policy)
	due := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	etAt := time.Date(2026, time.September, 20, 15, 30, 0, 0, time.UTC)
	pprAt := time.Date(2026, time.September, 22, 9, 0, 0, 0, time.UTC)

	if _, ok := applyPurposeSecondWaveFloor(due, decision, nil); ok {
		t.Fatal("second wave allowed with no first-wave history")
	}
	if _, ok := applyPurposeSecondWaveFloor(due, decision, []domain.RecentVaccineAdministration{{AdministeredAt: etAt, VaccineCode: "ET_TT", Source: domain.AdministrationSourceCompletion}}); ok {
		t.Fatal("second wave allowed with PPR missing")
	}
	complete := []domain.RecentVaccineAdministration{{AdministeredAt: etAt, VaccineCode: "ET_TT", Source: domain.AdministrationSourceCompletion}, {AdministeredAt: pprAt, VaccineCode: "PPR", Source: domain.AdministrationSourceCompletion}}
	got, ok := applyPurposeSecondWaveFloor(due, decision, complete)
	want := businessDayStart(pprAt).AddDate(0, 0, 28)
	if !ok || !got.Equal(want) {
		t.Fatalf("due=%s ok=%v, want latest first-wave + 28d = %s", got, ok, want)
	}
	later := want.AddDate(0, 0, 5)
	if got, _ := applyPurposeSecondWaveFloor(later, decision, complete); !got.Equal(later) {
		t.Fatalf("due=%s, a later due must not be pulled earlier", got)
	}
	firstWave := procurementPurposeDecision(goat, vaccineProfile{Name: "ET+TT"}, policy)
	if got, ok := applyPurposeSecondWaveFloor(due, firstWave, nil); !ok || !got.Equal(due) {
		t.Fatalf("first-wave due=%s ok=%v, want unchanged", got, ok)
	}
}

func TestPurposeSecondWaveSelectsSpecies(t *testing.T) {
	policy := purposeTestPolicy()
	goat := domain.EligibleGoat{Species: "goat", ProcurementPurpose: "fattening"}
	sheep := domain.EligibleGoat{Species: "sheep", ProcurementPurpose: "fattening"}
	for _, tc := range []struct {
		animal  domain.EligibleGoat
		vaccine string
		want    bool
	}{
		{goat, "Goat Pox", true}, {goat, "Sheep Pox", false},
		{sheep, "Sheep Pox", true}, {sheep, "Goat Pox", false},
	} {
		d := procurementPurposeDecision(tc.animal, vaccineProfile{Name: tc.vaccine}, policy)
		if d.Applicable != tc.want || (tc.want && !d.SecondWave) {
			t.Fatalf("%s/%s decision=%#v, want applicable=%v second wave", tc.animal.Species, tc.vaccine, d, tc.want)
		}
	}
}

func TestBreedingPlanSupportsOneTwoAndThreeVaccines(t *testing.T) {
	three := int32(14)
	for _, tc := range []struct {
		name   string
		plan   genProcurementPurposePlan
		counts int
	}{
		{name: "one", plan: genProcurementPurposePlan{FirstWave: genStringList{"ET+TT"}}, counts: 1},
		{name: "two", plan: genProcurementPurposePlan{FirstWave: genStringList{"ET+TT"}, GoatSecondWave: genStringList{"PPR"}}, counts: 2},
		{name: "three", plan: genProcurementPurposePlan{FirstWave: genStringList{"ET+TT", "PPR"}, SecondWaveAfterDays: &three, GoatSecondWave: genStringList{"Goat Pox"}}, counts: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := genProcurementPolicy{PurposePlans: map[string]genProcurementPurposePlan{"breeding": tc.plan}}
			goat := domain.EligibleGoat{Species: "goat", ProcurementPurpose: "breeding"}
			got := 0
			for _, v := range []string{"ET+TT", "PPR", "Goat Pox", "FMD", "HS"} {
				if procurementPurposeDecision(goat, vaccineProfile{Name: v}, policy).Applicable {
					got++
				}
			}
			if got != tc.counts {
				t.Fatalf("applicable=%d, want %d", got, tc.counts)
			}
		})
	}
}

// A birth_age second-wave rule (not only adult campaigns) is held until the first wave is complete,
// and then floored to the latest first-wave administration plus the authored delay.
func TestGenerateBirthAgeSecondWaveFollowsFirstWave(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.September, 24, 0, 0, 0, 0, time.UTC)
	newProto := func() *generationProtoFake {
		return &generationProtoFake{
			ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1"},"procurement_policy":{"purpose_plans":{
				"fattening":{"first_wave":["ET+TT","PPR"],"second_wave_after_days":28,"goat_second_wave":["Goat Pox"],"sheep_second_wave":["Sheep Pox"]}}}}`),
			rules: []protodomain.Rule{{
				RuleID: "rule-pox", DoseCode: "goat_pox_kid", Sequence: 1, TriggerType: "birth_age", OffsetDays: 60, DueWindowDays: 7,
				EligibilityJSON: []byte(`{"vaccine":{"code":"GOAT_POX","name":"Goat Pox","type":"live","pathogen_class":"viral"}}`),
			}},
		}
	}
	goat := domain.EligibleGoat{
		GoatID: "kid-1", LifecycleStatus: "alive", Stage: "K1", Species: "goat", OriginType: "birth",
		ProcurementPurpose: "fattening", DOB: &dob, ParkID: "park-1", ShedID: "shed-1",
	}

	obl := &generationObligationFake{seen: map[string]bool{}}
	if _, err := NewGenerationService(newProto(), &generationGoatFake{list: []domain.EligibleGoat{goat}}, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(obl.inserted) != 0 {
		t.Fatalf("inserted=%#v, want no Goat Pox before the first wave", obl.inserted)
	}

	pprAt := time.Date(2026, time.September, 20, 10, 0, 0, 0, time.UTC)
	history := map[string][]domain.RecentVaccineAdministration{"kid-1": {
		{AdministeredAt: time.Date(2026, time.September, 18, 10, 0, 0, 0, time.UTC), VaccineCode: "ET_TT", Source: domain.AdministrationSourceCompletion},
		{AdministeredAt: pprAt, VaccineCode: "PPR", Source: domain.AdministrationSourceCompletion},
	}}
	obl = &generationObligationFake{seen: map[string]bool{}}
	if _, err := NewGenerationService(newProto(), &generationGoatFake{list: []domain.EligibleGoat{goat}, vaccineHistory: history}, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(obl.inserted) != 1 {
		t.Fatalf("inserted=%#v, want one Goat Pox row after the complete first wave", obl.inserted)
	}
	if floor := businessDayStart(pprAt).AddDate(0, 0, 28); obl.inserted[0].DueAt.Before(floor) {
		t.Fatalf("due=%s, want >= latest first-wave + 28d = %s", obl.inserted[0].DueAt, floor)
	}
}

func TestGenerateMarksOnlyAnchorMissingCatchUpRow(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1"}}`),
		rules:   []protodomain.Rule{{RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-no-dob", LifecycleStatus: "alive", Stage: "K1", ParkID: "park-1", ShedID: "shed-1"},
		{GoatID: "goat-dob", LifecycleStatus: "alive", Stage: "K1", ParkID: "park-1", ShedID: "shed-1", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	if _, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("generate: %v", err)
	}
	basis := map[string]string{}
	for _, row := range obl.inserted {
		basis[row.TargetID] = row.ScheduleBasis
	}
	if len(basis) != 2 {
		t.Fatalf("inserted=%#v, want one row per goat", obl.inserted)
	}
	if basis["goat-no-dob"] != obldomain.ScheduleBasisAnchorMissingCatchUp {
		t.Fatalf("no-DOB basis=%q, want %q", basis["goat-no-dob"], obldomain.ScheduleBasisAnchorMissingCatchUp)
	}
	if basis["goat-dob"] != "" {
		t.Fatalf("anchored basis=%q, want empty (anchored)", basis["goat-dob"])
	}
}

// Scoped vaccination anchor events chain schedules but never prove an individual animal received
// the first wave; only individual administration evidence unlocks a governed second wave.
func TestPurposeSecondWaveIgnoresAnchorEvents(t *testing.T) {
	goat := domain.EligibleGoat{Species: "goat", ProcurementPurpose: "fattening"}
	decision := procurementPurposeDecision(goat, vaccineProfile{Name: "Goat Pox"}, purposeTestPolicy())
	anchorAt := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	anchors := []domain.RecentVaccineAdministration{
		{AdministeredAt: anchorAt, VaccineCode: "ET_TT", Source: domain.AdministrationSourceAnchor},
		{AdministeredAt: anchorAt, VaccineCode: "PPR", Source: domain.AdministrationSourceAnchor},
	}
	if _, ok := applyPurposeSecondWaveFloor(anchorAt, decision, anchors); ok {
		t.Fatal("anchor-only history unlocked the second wave")
	}
	unlabelled := []domain.RecentVaccineAdministration{{AdministeredAt: anchorAt, VaccineCode: "ET_TT"}, {AdministeredAt: anchorAt, VaccineCode: "PPR"}}
	if _, ok := applyPurposeSecondWaveFloor(anchorAt, decision, unlabelled); ok {
		t.Fatal("unlabelled history must fail closed")
	}
	pprAt := time.Date(2026, time.September, 22, 9, 0, 0, 0, time.UTC)
	mixed := append([]domain.RecentVaccineAdministration{
		{AdministeredAt: time.Date(2026, time.September, 20, 9, 0, 0, 0, time.UTC), VaccineCode: "ET+TT", Source: domain.AdministrationSourcePrearrival},
		{AdministeredAt: pprAt, VaccineCode: "PPR", Source: domain.AdministrationSourceTrusted},
	}, anchors...)
	got, ok := applyPurposeSecondWaveFloor(anchorAt, decision, mixed)
	if want := businessDayStart(pprAt).AddDate(0, 0, 28); !ok || !got.Equal(want) {
		t.Fatalf("due=%s ok=%v, want real administrations to unlock at %s", got, ok, want)
	}
	onlyOneReal := append([]domain.RecentVaccineAdministration{{AdministeredAt: pprAt, VaccineCode: "PPR", Source: domain.AdministrationSourceCompletion}}, anchors...)
	if _, ok := applyPurposeSecondWaveFloor(anchorAt, decision, onlyOneReal); ok {
		t.Fatal("an anchor event must not stand in for the missing ET+TT administration")
	}
}
