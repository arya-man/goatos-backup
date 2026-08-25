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
			"eligibility":{"animal_stage":"adult","species":["goat"],"sex":["female"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"]},
			"procurement_policy":{
				"kids_normal_schedule_until_weeks":16,
				"purpose_plans":{
					"breeding":{"first_wave":["ET+TT"],"second_wave_after_days":28,"goat_second_wave":["FMD"],"sheep_second_wave":[]},
					"fattening":{"first_wave":["PPR"],"second_wave_after_days":28,"goat_second_wave":["HS"],"sheep_second_wave":[]}
				}
			}
		}`),
		rules: []protodomain.Rule{
			{RuleID: "rule-et", DoseCode: "et_tt_adult_w1", Sequence: 1, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"ET_TT","name":"ET+TT","type":"killed","pathogen_class":"bacterial"}}`)},
			{RuleID: "rule-ppr", DoseCode: "ppr_adult_w1", Sequence: 2, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","name":"PPR","type":"live","pathogen_class":"viral"}}`)},
			{RuleID: "rule-fmd", DoseCode: "fmd_adult_w1", Sequence: 3, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"FMD","name":"FMD","type":"killed","pathogen_class":"viral"}}`)},
			{RuleID: "rule-hs", DoseCode: "hs_adult_w1", Sequence: 4, TriggerType: "manual_campaign", DueWindowDays: 7, EligibilityJSON: []byte(`{"vaccine":{"code":"HS","name":"HS","type":"killed","pathogen_class":"bacterial"}}`)},
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
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", entry)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 4 || len(obl.inserted) != 4 {
		t.Fatalf("result=%#v inserted=%#v, want four purpose-filtered obligations", result, obl.inserted)
	}
	got := map[string]map[string]bool{}
	for _, in := range obl.inserted {
		if got[in.TargetID] == nil {
			got[in.TargetID] = map[string]bool{}
		}
		got[in.TargetID][in.RuleID] = true
	}
	if !got["breeding-goat"]["rule-et"] || !got["breeding-goat"]["rule-fmd"] || len(got["breeding-goat"]) != 2 {
		t.Fatalf("breeding rules = %#v, want ET+TT and FMD only", got["breeding-goat"])
	}
	if !got["fattening-goat"]["rule-ppr"] || !got["fattening-goat"]["rule-hs"] || len(got["fattening-goat"]) != 2 {
		t.Fatalf("fattening rules = %#v, want PPR and HS only", got["fattening-goat"])
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
