package app

import (
	"context"
	"testing"
	"time"

	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestGenerateForVersionDefersDuringWarmupHold(t *testing.T) {
	ctx := context.Background()
	entry := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"adult","lifecycle":"alive","defer_states":["icu","quarantine"]},"procurement_policy":{"warmup_no_vaccination_days":7,"kids_normal_schedule_until_weeks":16}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-wave-1", DoseCode: "et_ppr", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 2,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{{
		GoatID: "procured-adult", LifecycleStatus: "alive", HealthStatus: "healthy",
		ReproductiveStatus: "open", Stage: "adult", OriginType: "procured",
		EntryDate: &entry, WarmingEntryAt: &entry,
	}}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.July, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 1 || result.Deferred != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want one deferred warmup obligation", result, obl.inserted)
	}
	if obl.inserted[0].Status != "deferred" {
		t.Fatalf("status=%q, want deferred", obl.inserted[0].Status)
	}
}

func TestGenerateForVersionSkipsAdultRulesForKidPath(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","lifecycle":"alive","defer_states":["icu","quarantine"]}}`),
		rules: []protodomain.Rule{
			{RuleID: "kid-rule", DoseCode: "et_4w", Sequence: 1, TriggerType: "birth_age", OffsetDays: 28},
			{RuleID: "adult-rule", DoseCode: "et_ppr", Sequence: 2, TriggerType: "post_arrival", OffsetDays: 0},
		},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{{
		GoatID: "farm-kid", LifecycleStatus: "alive", HealthStatus: "healthy",
		ReproductiveStatus: "open", Stage: "K1", OriginType: "birth", DOB: &dob,
	}}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 || obl.inserted[0].RuleID != "kid-rule" {
		t.Fatalf("result=%#v inserted=%#v, want only kid birth_age rule", result, obl.inserted)
	}
}

func TestGenerateForVersionUsesProcurementPurposePlans(t *testing.T) {
	ctx := context.Background()
	entry := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{
			"eligibility":{"animal_stage":"adult","species":["goat","sheep"],"sex":["female"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"]},
			"procurement_policy":{
				"kids_normal_schedule_until_weeks":16,
				"purpose_plans":{
					"breeding":{"first_wave":["ET+TT"],"second_wave_after_days":28,"goat_second_wave":["FMD"],"sheep_second_wave":[]},
					"fattening":{"first_wave":["ET+TT","PPR"],"second_wave_after_days":28,"goat_second_wave":["Goat Pox"],"sheep_second_wave":["Sheep Pox"]}
				}
			}
		}`),
		rules: []protodomain.Rule{
			{RuleID: "rule-et", DoseCode: "et_tt_adult_w1", Sequence: 1, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"ET_TT","name":"ET+TT","type":"killed","pathogen_class":"bacterial"}}`)},
			{RuleID: "rule-ppr", DoseCode: "ppr_adult_w1", Sequence: 2, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","name":"PPR","type":"live","pathogen_class":"viral"}}`)},
			{RuleID: "rule-fmd", DoseCode: "fmd_adult_w1", Sequence: 3, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"FMD","name":"FMD","type":"killed","pathogen_class":"viral"}}`)},
			{RuleID: "rule-hs", DoseCode: "hs_adult_w1", Sequence: 4, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"HS","name":"HS","type":"killed","pathogen_class":"bacterial"}}`)},
			{RuleID: "rule-goat-pox", DoseCode: "goat_pox_adult_w1", Sequence: 5, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"GOAT_POX","name":"Goat Pox","type":"live","pathogen_class":"viral"}}`)},
			{RuleID: "rule-sheep-pox", DoseCode: "sheep_pox_adult_w1", Sequence: 6, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"SHEEP_POX","name":"Sheep Pox","type":"live","pathogen_class":"viral"}}`)},
		},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{
			GoatID: "breeding-goat", LifecycleStatus: "alive", HealthStatus: "healthy",
			ReproductiveStatus: "open", Species: "goat", Sex: "female", Breed: "barbari",
			Stage: "adult", OriginType: "procured", ProcurementPurpose: "breeding",
			EntryDate: &entry, WarmingEntryAt: &entry, ShedID: "shed-1", ParkID: "park-1",
		},
		{
			GoatID: "fattening-goat", LifecycleStatus: "alive", HealthStatus: "healthy",
			ReproductiveStatus: "open", Species: "goat", Sex: "female", Breed: "barbari",
			Stage: "adult", OriginType: "procured", ProcurementPurpose: "fattening",
			EntryDate: &entry, WarmingEntryAt: &entry, ShedID: "shed-1", ParkID: "park-1",
		},
		{
			GoatID: "fattening-sheep", LifecycleStatus: "alive", HealthStatus: "healthy",
			ReproductiveStatus: "open", Species: "sheep", Sex: "female", Breed: "mandya",
			Stage: "adult", OriginType: "procured", ProcurementPurpose: "fattening",
			EntryDate: &entry, WarmingEntryAt: &entry, ShedID: "shed-1", ParkID: "park-1",
		},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", entry)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 5 || len(obl.inserted) != 5 {
		t.Fatalf("result=%#v inserted=%#v, want five first-wave purpose-filtered obligations", result, obl.inserted)
	}
	got := map[string]map[string]bool{}
	for _, in := range obl.inserted {
		if got[in.TargetID] == nil {
			got[in.TargetID] = map[string]bool{}
		}
		got[in.TargetID][in.RuleID] = true
	}
	if !got["breeding-goat"]["rule-et"] || len(got["breeding-goat"]) != 1 {
		t.Fatalf("breeding rules = %#v, want ET+TT only until first-wave completion anchors FMD", got["breeding-goat"])
	}
	if !got["fattening-goat"]["rule-et"] || !got["fattening-goat"]["rule-ppr"] || len(got["fattening-goat"]) != 2 {
		t.Fatalf("fattening rules = %#v, want ET+TT and PPR until first-wave completion anchors Goat Pox", got["fattening-goat"])
	}
	if !got["fattening-sheep"]["rule-et"] || !got["fattening-sheep"]["rule-ppr"] || len(got["fattening-sheep"]) != 2 {
		t.Fatalf("fattening sheep rules = %#v, want ET+TT and PPR until first-wave completion anchors Sheep Pox", got["fattening-sheep"])
	}
}

func TestProcurementPurposePlanAppliesToBirthAgeRules(t *testing.T) {
	policy := genProcurementPolicy{PurposePlans: map[string]genProcurementPurposePlan{
		"fattening": {FirstWave: genStringList{"ET+TT", "PPR"}, GoatSecondWave: genStringList{"Goat Pox"}, SheepSecondWave: genStringList{"Sheep Pox"}},
	}}
	goat := domain.EligibleGoat{Species: "goat", ProcurementPurpose: "fattening"}
	for _, tc := range []struct {
		vaccine string
		want    bool
	}{
		{"ET+TT", true}, {"PPR", true}, {"Goat Pox", true},
		{"Sheep Pox", false}, {"FMD", false}, {"HS", false}, {"Z1+Z3", false},
	} {
		t.Run(tc.vaccine, func(t *testing.T) {
			got := procurementPurposeDecision(goat, vaccineProfile{Name: tc.vaccine}, policy).Applicable
			if got != tc.want {
				t.Fatalf("applicable=%v, want %v", got, tc.want)
			}
		})
	}
	goat.Species = "sheep"
	if got := procurementPurposeDecision(goat, vaccineProfile{Name: "Sheep Pox"}, policy).Applicable; !got {
		t.Fatal("Sheep Pox must be applicable to fattening sheep")
	}
	if got := procurementPurposeDecision(goat, vaccineProfile{Name: "Goat Pox"}, policy).Applicable; got {
		t.Fatal("Goat Pox must not be applicable to fattening sheep")
	}
	if got := procurementPurposeDecision(goat, vaccineProfile{Name: "ET+TT"}, genProcurementPolicy{}).Applicable; got {
		t.Fatal("fattening must fail closed when its authored purpose plan is missing")
	}
}

func TestFatteningPurposePlanSupportsOneTwoAndThreeVaccinesWithoutChangingIdentity(t *testing.T) {
	policy := genProcurementPolicy{PurposePlans: map[string]genProcurementPurposePlan{
		"fattening": {FirstWave: genStringList{"ET+TT", "PPR"}, GoatSecondWave: genStringList{"Goat Pox"}, SheepSecondWave: genStringList{"Sheep Pox"}},
	}}
	goat := domain.EligibleGoat{Species: "goat", ProcurementPurpose: "fattening"}
	for _, tc := range []struct {
		name       string
		vaccines   []string
		applicable int
	}{
		{name: "one", vaccines: []string{"ET+TT"}, applicable: 1},
		{name: "two", vaccines: []string{"ET+TT", "PPR"}, applicable: 2},
		{name: "three", vaccines: []string{"ET+TT", "PPR", "Goat Pox"}, applicable: 3},
		{name: "excluded_do_not_expand", vaccines: []string{"ET+TT", "PPR", "Goat Pox", "FMD", "HS", "Z1+Z3"}, applicable: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := 0
			for _, vaccine := range tc.vaccines {
				if ok := procurementPurposeDecision(goat, vaccineProfile{Name: vaccine}, policy).Applicable; ok {
					got++
				}
			}
			if got != tc.applicable {
				t.Fatalf("applicable vaccines=%d, want %d", got, tc.applicable)
			}
		})
	}
}

func TestFatteningSecondWaveWaitsForActualFirstWaveAdministration(t *testing.T) {
	days := int32(28)
	entry := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	goat := domain.EligibleGoat{Species: "goat", ProcurementPurpose: "fattening", WarmingEntryAt: &entry}
	policy := genProcurementPolicy{PurposePlans: map[string]genProcurementPurposePlan{
		"fattening": {
			FirstWave: genStringList{"ET+TT", "PPR"}, SecondWaveAfterDays: &days,
			GoatSecondWave: genStringList{"Goat Pox"}, SheepSecondWave: genStringList{"Sheep Pox"},
		},
	}}
	rule := protodomain.Rule{TriggerType: "manual_campaign", DueWindowDays: 7}
	vaccine := vaccineProfile{Code: "GOAT_POX", Name: "Goat Pox"}

	if _, ok := procurementPurposePrimaryDue(goat, rule, vaccine, policy, nil); ok {
		t.Fatal("second wave became schedulable before any first-wave administration")
	}
	lateETTT := time.Date(2026, time.September, 20, 15, 30, 0, 0, time.UTC)
	latePPR := time.Date(2026, time.September, 22, 9, 0, 0, 0, time.UTC)
	if _, ok := procurementPurposePrimaryDue(goat, rule, vaccine, policy, []domain.RecentVaccineAdministration{{AdministeredAt: lateETTT, VaccineCode: "ET_TT", Source: domain.AdministrationSourceCompletion}}); ok {
		t.Fatal("second wave became schedulable with PPR still missing")
	}
	if _, ok := procurementPurposePrimaryDue(goat, rule, vaccine, policy, []domain.RecentVaccineAdministration{{AdministeredAt: latePPR, VaccineCode: "PPR", Source: domain.AdministrationSourceCompletion}}); ok {
		t.Fatal("second wave became schedulable with ET+TT still missing")
	}
	due, ok := procurementPurposePrimaryDue(goat, rule, vaccine, policy, []domain.RecentVaccineAdministration{
		{AdministeredAt: lateETTT, VaccineCode: "ET_TT", Source: domain.AdministrationSourceCompletion},
		{AdministeredAt: latePPR, VaccineCode: "PPR", Source: domain.AdministrationSourceCompletion},
	})
	if !ok {
		t.Fatal("second wave did not become schedulable after first-wave administration")
	}
	want := businessDayStart(latePPR).AddDate(0, 0, 28)
	if !due.Equal(want) {
		t.Fatalf("second-wave due=%s, want latest actual first-wave administration + 28d = %s", due, want)
	}
}

func TestGenerateForVersionUsesTopLevelProcurementFirstWaveWhenPurposeBlank(t *testing.T) {
	ctx := context.Background()
	entry := time.Date(2026, time.September, 16, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{
			"eligibility":{"animal_stage":"adult","species":["goat"],"sex":["female"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"]},
			"procurement_policy":{
				"warmup_no_vaccination_days":7,
				"kids_normal_schedule_until_weeks":16,
				"first_wave":["ET+TT"],
				"goat_second_wave":["Goat Pox"],
				"sheep_second_wave":["Sheep Pox"],
				"second_wave_after_days":28
			}
		}`),
		rules: []protodomain.Rule{
			{RuleID: "rule-et", DoseCode: "et_tt_adult_w1", Sequence: 1, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"ET_TT","name":"ET+TT","type":"killed","pathogen_class":"bacterial"}}`)},
			{RuleID: "rule-ppr", DoseCode: "ppr_adult_w1", Sequence: 2, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","name":"PPR","type":"live","pathogen_class":"viral"}}`)},
		},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{{
		GoatID: "cbe-godel2-procured", LifecycleStatus: "alive", HealthStatus: "healthy",
		ReproductiveStatus: "open", Species: "goat", Sex: "female", Breed: "barbari",
		Stage: "adult", OriginType: "procured", ProcurementPurpose: "",
		EntryDate: &entry, WarmingEntryAt: &entry, ShedID: "shed-godel2", ParkID: "park-cbe",
	}}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.September, 23, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want one ET+TT first-wave obligation", result, obl.inserted)
	}
	if obl.inserted[0].RuleID != "rule-et" {
		t.Fatalf("inserted rule=%q, want ET+TT first-wave rule", obl.inserted[0].RuleID)
	}
	if got, want := obl.inserted[0].DueAt.Format("2006-01-02"), "2026-09-23"; got != want {
		t.Fatalf("due date=%s, want %s", got, want)
	}
}

func TestGenerateForVersionSchedulesPregnantWithoutBreedingDate(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","lifecycle":"alive","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["icu","quarantine"]},"pregnancy_policy":{"allow_until_pregnancy_month":3,"skip_from_pregnancy_month":4,"skip_through_pregnancy_month":5,"post_delivery_catch_up_days":14}}`),
		rules: []protodomain.Rule{{
			RuleID: "kid-rule", DoseCode: "et_4w", Sequence: 1, TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{{
		GoatID: "pregnant-kid", LifecycleStatus: "alive", HealthStatus: "healthy",
		ReproductiveStatus: "pregnant", Stage: "K1", OriginType: "birth", DOB: &dob,
	}}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// Locked rule: pregnant with an unknown month (no breeding date) schedules normally — it is NOT
	// deferred. Only a proven pregnancy month 4-5 defers.
	if result.Generated != 1 || result.Deferred != 0 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want pregnant-unknown-month scheduled normally (not deferred)", result, obl.inserted)
	}
}
