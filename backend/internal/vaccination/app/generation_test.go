package app

import (
	"context"
	"errors"
	"testing"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestGenerateForVersionAppliesCrossVaccineGap(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	pprAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		rules: []protodomain.Rule{{
			RuleID: "rule-gpox", DoseCode: "gpox-dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 98,
		}},
		ruleDSL: []byte(`{"vaccine":{"code":"Goat Pox","type":"live","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]},"compatibility_policy":{"live_to_live_gap_days":28}}`),
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{GoatID: "goat-1", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1"}},
		lastVaccine: map[string]domain.RecentVaccineAdministration{
			"goat-1": {
				AdministeredAt: pprAt,
				VaccineCode:    "PPR",
				VaccineType:    "live",
				PathogenClass:  "viral",
			},
		},
	}
	obl := &generationObligationFake{}
	gen := NewGenerationService(proto, goats, obl)
	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 1 {
		t.Fatalf("generated=%d want 1", result.Generated)
	}
	if len(obl.inserted) != 1 {
		t.Fatalf("inserted=%d want 1", len(obl.inserted))
	}
	wantDue := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	if !obl.inserted[0].DueAt.Equal(wantDue) {
		t.Fatalf("due=%s want %s (4-week live→live gap after PPR)", obl.inserted[0].DueAt, wantDue)
	}
}

func TestGenerateForVersionChecksAllRecentVaccinesForCrossGap(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	pprAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	killedAt := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		rules: []protodomain.Rule{{
			RuleID: "rule-gpox", DoseCode: "gpox-dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 98,
		}},
		ruleDSL: []byte(`{"vaccine":{"code":"Goat Pox","type":"live","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]},"compatibility_policy":{"live_to_live_gap_days":28}}`),
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{GoatID: "goat-1", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1"}},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"goat-1": {
				{
					AdministeredAt: killedAt,
					VaccineCode:    "ET_TT",
					VaccineType:    "killed",
					PathogenClass:  "bacterial",
				},
				{
					AdministeredAt: pprAt,
					VaccineCode:    "PPR",
					VaccineType:    "live",
					PathogenClass:  "viral",
				},
			},
		},
	}
	obl := &generationObligationFake{}
	gen := NewGenerationService(proto, goats, obl)
	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	wantDue := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	if result.Generated != 1 || len(obl.inserted) != 1 || !obl.inserted[0].DueAt.Equal(wantDue) {
		t.Fatalf("result=%#v inserted=%#v, want live-live gap from older PPR through %s", result, obl.inserted, wantDue)
	}
}

func TestDueAfterPreviousCompletionRequiresPositiveGap(t *testing.T) {
	administered := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	rule := protodomain.Rule{
		RuleID:      "rule-et-booster",
		DoseCode:    "et_booster",
		Sequence:    2,
		TriggerType: "after_previous_completion",
		OffsetDays:  0,
		MinGapDays:  0,
	}
	history := []domain.RecentVaccineAdministration{{
		AdministeredAt: administered,
		VaccineCode:    "ET_TT",
		Sequence:       1,
	}}

	if due, ok := dueAfterPreviousCompletion(rule, vaccineProfile{Code: "ET_TT"}, history); ok {
		t.Fatalf("due=%s, want no due date for non-positive after_previous_completion gap", due)
	}
}

func TestDueAfterPreviousCompletionRequiresSameProtocolLineage(t *testing.T) {
	administered := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	rule := protodomain.Rule{
		RuleID:            "rule-et-booster",
		ProtocolVersionID: "version-v2",
		ProtocolID:        "protocol-vaccination",
		DoseCode:          "et_booster",
		Sequence:          2,
		TriggerType:       "after_previous_completion",
		OffsetDays:        21,
	}
	otherProtocol := []domain.RecentVaccineAdministration{{
		AdministeredAt:    administered,
		VaccineCode:       "ET_TT",
		Sequence:          1,
		ProtocolVersionID: "version-other",
		ProtocolID:        "protocol-other",
	}}
	if due, ok := dueAfterPreviousCompletion(rule, vaccineProfile{Code: "ET_TT"}, otherProtocol); ok {
		t.Fatalf("due=%s, want unrelated protocol completion ignored", due)
	}

	sameProtocolPreviousVersion := []domain.RecentVaccineAdministration{{
		AdministeredAt:    administered,
		VaccineCode:       "ET_TT",
		Sequence:          1,
		ProtocolVersionID: "version-v1",
		ProtocolID:        "protocol-vaccination",
	}}
	due, ok := dueAfterPreviousCompletion(rule, vaccineProfile{Code: "ET_TT"}, sameProtocolPreviousVersion)
	if !ok || !due.Equal(administered.AddDate(0, 0, 21)) {
		t.Fatalf("due=%s ok=%v, want same protocol lineage accepted", due, ok)
	}
}

func TestManualCampaignHTTPRunReplaysByIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	proto := &generationProtoFake{}
	goats := &generationGoatFake{}
	obl := &generationObligationFake{seen: map[string]bool{}}
	runs := &generationRunRecorderFake{byKey: map[string]domain.GenerationRun{}}
	gen := NewGenerationService(proto, goats, obl).WithGenerationRunRecorder(runs)

	firstAt := time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(5 * time.Minute)
	run, result, err := gen.GenerateManualCampaignForVersionWithHTTPRun(ctx, "tenant-1", "version-1", "catchup", firstAt, "manual-key-1", "hash-1")
	if err != nil {
		t.Fatalf("first manual campaign: %v", err)
	}
	if run.IdempotencyKey != "manual-key-1" || result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("first run=%#v result=%#v inserted=%#v", run, result, obl.inserted)
	}

	replay, replayResult, err := gen.GenerateManualCampaignForVersionWithHTTPRun(ctx, "tenant-1", "version-1", "catchup", secondAt, "manual-key-1", "hash-1")
	if err != nil {
		t.Fatalf("replay manual campaign: %v", err)
	}
	if replay.RunID != run.RunID || replayResult.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("replay run=%#v result=%#v inserted=%#v", replay, replayResult, obl.inserted)
	}
	if len(runs.startInputs) != 2 || runs.startInputs[0].IdempotencyKey != "manual-key-1" || runs.startInputs[1].IdempotencyKey != "manual-key-1" {
		t.Fatalf("run recorder inputs=%#v", runs.startInputs)
	}
}

func TestManualCampaignHTTPRunFailedRetryReusesOriginalAsOf(t *testing.T) {
	ctx := context.Background()
	proto := &generationProtoFake{rules: []protodomain.Rule{
		{RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "manual_campaign"},
		{RuleID: "rule-2", DoseCode: "dose-2", Sequence: 2, TriggerType: "manual_campaign"},
	}}
	goats := &generationGoatFake{}
	obl := &generationObligationFake{seen: map[string]bool{}, failOnceAfterInserted: 1}
	runs := &generationRunRecorderFake{byKey: map[string]domain.GenerationRun{}}
	gen := NewGenerationService(proto, goats, obl).WithGenerationRunRecorder(runs)

	firstAt := time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(5 * time.Minute)
	if _, _, err := gen.GenerateManualCampaignForVersionWithHTTPRun(ctx, "tenant-1", "version-1", "catchup", firstAt, "manual-key-failed", "hash-1"); err == nil {
		t.Fatalf("first manual campaign should fail after partial insert")
	}
	if len(obl.inserted) != 1 || !runs.byKey["manual-key-failed"].StartedAt.Equal(firstAt) || runs.byKey["manual-key-failed"].Status != "failed" {
		t.Fatalf("first failed state inserted=%#v run=%#v", obl.inserted, runs.byKey["manual-key-failed"])
	}

	_, result, err := gen.GenerateManualCampaignForVersionWithHTTPRun(ctx, "tenant-1", "version-1", "catchup", secondAt, "manual-key-failed", "hash-1")
	if err != nil {
		t.Fatalf("retry manual campaign: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 2 {
		t.Fatalf("retry result=%#v inserted=%#v, want one remaining insert only", result, obl.inserted)
	}
	for i, obligation := range obl.inserted {
		if !obligation.DueAt.Equal(firstAt) {
			t.Fatalf("inserted[%d].DueAt = %s, want original as_of %s", i, obligation.DueAt, firstAt)
		}
	}
}

func TestProtocolPublishedHandlerStartsGenerationRun(t *testing.T) {
	ctx := context.Background()
	proto := &generationProtoFake{}
	goats := &generationGoatFake{}
	obl := &generationObligationFake{seen: map[string]bool{}}
	runs := &generationRunRecorderFake{byKey: map[string]domain.GenerationRun{}}
	gen := NewGenerationService(proto, goats, obl).WithGenerationRunRecorder(runs)
	handler := NewProtocolPublishedHandler(gen)

	occurred := time.Date(2026, time.June, 27, 9, 0, 0, 0, time.UTC)
	err := handler.HandleEvent(ctx, eventbus.Event{
		Type:       EventProtocolVersionPublished,
		TenantID:   "tenant-1",
		Key:        "version-1",
		OccurredAt: occurred,
		Payload:    []byte(`{"protocol_version_id":"version-1","category":"vaccination"}`),
	})
	if err != nil {
		t.Fatalf("handle protocol published: %v", err)
	}
	if len(runs.startInputs) != 1 {
		t.Fatalf("start inputs = %#v, want one publish run", runs.startInputs)
	}
	got := runs.startInputs[0]
	if got.TriggerType != "publish" || got.TriggerRef != "version-1" || got.IdempotencyKey != generationRunKey("tenant-1", "version-1", "publish", "version-1") || !got.StartedAt.Equal(occurred) {
		t.Fatalf("publish generation input = %#v", got)
	}
}

func TestGenerateForVersionUsesConfigAnimalStageEligibility(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-k1", LifecycleStatus: "alive", Stage: "K1", DOB: &dob},
		{GoatID: "goat-adult", LifecycleStatus: "alive", Stage: "adult", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	asOf := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 || obl.inserted[0].TargetID != "goat-k1" {
		t.Fatalf("result=%#v inserted=%#v, want only K1 goat generated", result, obl.inserted)
	}
	if len(goats.filters) != 1 || goats.filters[0].Stage != "K1" || goats.filters[0].ProtocolVersionID != "version-1" || !goats.filters[0].AsOf.Equal(asOf) {
		t.Fatalf("impact filters=%#v, want Config animal_stage, version, and asOf propagated", goats.filters)
	}
}

func TestGenerateForVersionAcceptsMatrixArraySelectors(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"all","species":["goat","sheep"],"sex":["female","male"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"],"defer_states":["icu","quarantine"]}}`),
		rules: []protodomain.Rule{{
			RuleID:          "rule-kid-array",
			DoseCode:        "kid-primary",
			Sequence:        1,
			TriggerType:     "birth_age",
			OffsetDays:      28,
			EligibilityJSON: []byte(`{"matrix_row_id":"kid-goat-primary","eligibility":{"species":["goat"],"animal_stage":["K1","K2"],"sex":["female"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"],"defer_states":["icu","quarantine"]},"vaccine":{"code":"ET_TT","type":"killed","pathogen_class":"killed"}}`),
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-k1-female", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "goat", Sex: "female", Stage: "K1", Breed: "Beetal", DOB: &dob},
		{GoatID: "goat-k2-female", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "goat", Sex: "female", Stage: "K2", Breed: "Osmanabadi", DOB: &dob},
		{GoatID: "goat-k1-male", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "goat", Sex: "male", Stage: "K1", Breed: "Beetal", DOB: &dob},
		{GoatID: "sheep-k1-female", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "sheep", Sex: "female", Stage: "K1", Breed: "Anantapur Sheep", DOB: &dob},
		{GoatID: "goat-adult-female", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "goat", Sex: "female", Stage: "adult", Breed: "Beetal", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate with array selectors: %v", err)
	}
	if result.Generated != 2 || len(obl.inserted) != 2 {
		t.Fatalf("result=%#v inserted=%#v, want two female goat kid obligations", result, obl.inserted)
	}
	got := map[string]bool{}
	for _, ob := range obl.inserted {
		got[ob.TargetID] = true
	}
	if !got["goat-k1-female"] || !got["goat-k2-female"] {
		t.Fatalf("inserted targets=%#v, want K1 and K2 female goats", got)
	}
}

func TestGenerateForVersionHonorsLifecycleAgeAndAgeBandEligibility(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.June, 30, 0, 0, 0, 0, time.UTC)
	minAge := int32(21)
	maxAge := int32(45)
	daysAgo := func(days int) *time.Time {
		t := asOf.AddDate(0, 0, -days)
		return &t
	}
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"lifecycle":"alive","min_age_days":21,"max_age_days":45,"age_band":"kid"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-eligible", LifecycleStatus: "alive", DOB: daysAgo(int(minAge + 9)), AgeBand: "kid"},
		{GoatID: "goat-too-young", LifecycleStatus: "alive", DOB: daysAgo(10), AgeBand: "kid"},
		{GoatID: "goat-too-old", LifecycleStatus: "alive", DOB: daysAgo(int(maxAge + 10)), AgeBand: "kid"},
		{GoatID: "goat-held-lifecycle", LifecycleStatus: "sick", DOB: daysAgo(30), AgeBand: "kid"},
		{GoatID: "goat-wrong-band", LifecycleStatus: "alive", DOB: daysAgo(30), AgeBand: "adult"},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 || obl.inserted[0].TargetID != "goat-eligible" {
		t.Fatalf("result=%#v inserted=%#v, want only lifecycle/age/age-band eligible goat", result, obl.inserted)
	}
}

func TestGenerateForVersionSkipsExitedGoatsEvenIfRepositoryReturnsThem(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"defer_states":["sick","quarantine","ICU"]}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-alive", LifecycleStatus: "alive", HealthStatus: "healthy", DOB: &dob},
		{GoatID: "goat-dead", LifecycleStatus: "dead", HealthStatus: "sick", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 || obl.inserted[0].TargetID != "goat-alive" {
		t.Fatalf("result=%#v inserted=%#v, want only in-care goat generated", result, obl.inserted)
	}
}

func TestGenerateForVersionDefaultsClinicalHoldStatesToDeferred(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-sick", LifecycleStatus: "alive", HealthStatus: "sick", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 1 || result.Deferred != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want one deferred hold obligation", result, obl.inserted)
	}
	if obl.inserted[0].Status != "deferred" {
		t.Fatalf("inserted=%#v, want deferred status for sick goat", obl.inserted[0])
	}
}

func TestGenerateForVersionHonorsVaccinationRulesClinicalAndPregnancyRules(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","under_treatment","quarantine","icu"]}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "primary", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21, DueWindowDays: 30,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-ok", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Stage: "K1", DOB: &dob},
		{GoatID: "goat-pregnant", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "pregnant", Stage: "K1", DOB: &dob},
		{GoatID: "goat-lactating", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "lactating", Stage: "K1", DOB: &dob},
		{GoatID: "goat-sick", LifecycleStatus: "alive", HealthStatus: "sick", ReproductiveStatus: "open", Stage: "K1", DOB: &dob},
		{GoatID: "goat-quarantine", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Stage: "K1", DOB: &dob, LocationIsQuarantine: true},
		{GoatID: "goat-icu", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Stage: "K1", DOB: &dob, LocationIsICU: true},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 4 || result.Deferred != 3 || len(obl.inserted) != 4 {
		t.Fatalf("result=%#v inserted=%#v, want eligible plus sick/quarantine/ICU visible holds only", result, obl.inserted)
	}
	got := map[string]string{}
	for _, inserted := range obl.inserted {
		got[inserted.TargetID] = inserted.Status
	}
	if got["goat-ok"] != "scheduled" || got["goat-sick"] != "deferred" || got["goat-quarantine"] != "deferred" || got["goat-icu"] != "deferred" {
		t.Fatalf("statuses=%#v, want scheduled healthy goat and deferred clinical holds", got)
	}
	if got["goat-pregnant"] != "" || got["goat-lactating"] != "" {
		t.Fatalf("statuses=%#v, pregnant/lactating goats should be excluded until a reviewed row allows them", got)
	}
}

func TestGenerateForVersionDefersClinicalHoldEvenWhenRuleTargetsHealthy(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"healthy","reproductive":"any","defer_states":["sick","under_treatment","quarantine","icu"]}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "primary", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21, DueWindowDays: 7,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-sick", LifecycleStatus: "alive", HealthStatus: "sick", ReproductiveStatus: "open", Stage: "K1", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 1 || result.Deferred != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want visible deferred work for sick animal", result, obl.inserted)
	}
	if got := obl.inserted[0].Status; got != "deferred" {
		t.Fatalf("status=%q, want deferred", got)
	}
	if len(goats.filters) != 1 || goats.filters[0].Health != "" {
		t.Fatalf("filters=%#v, health must not prefilter clinical holds out of SM-1", goats.filters)
	}
}

func TestGenerateForVersionHonorsProcurementWarmupOffset(t *testing.T) {
	ctx := context.Background()
	entryDate := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"adult","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","under_treatment","quarantine","icu"]},"procurement_policy":{"warmup_no_vaccination_days":7,"kids_normal_schedule_until_weeks":16,"adult_source_vaccination_allowed":true}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-et-ppr-wave", DoseCode: "source_warmup_wave_1", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 2,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "adult-procured", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Stage: "adult", EntryDate: &entryDate},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	wantDue := time.Date(2026, time.July, 8, 0, 0, 0, 0, time.UTC)
	if result.Generated != 1 || len(obl.inserted) != 1 || !obl.inserted[0].DueAt.Equal(wantDue) {
		t.Fatalf("result=%#v inserted=%#v, want post-arrival warmup due %s", result, obl.inserted, wantDue)
	}
}

func TestGenerateForVersionHonorsVaccinationRulesSourceScheduleWithTrustedHistory(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	et4w := dob.AddDate(0, 0, 28)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","under_treatment","quarantine","icu"]},"compatibility_policy":{"live_to_killed_gap_days":14,"killed_to_killed_gap_days":14,"live_to_live_gap_days":28,"kid_booster_min_gap_days":21},"pregnancy_policy":{"allow_until_pregnancy_month":3,"skip_from_pregnancy_month":4,"skip_through_pregnancy_month":5,"post_delivery_catch_up_days":14}}`),
		rules: []protodomain.Rule{
			{RuleID: "rule-et-4w", DoseCode: "et_tt_4w", Sequence: 1, TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7, CatchUp: "pc_approval"},
			{RuleID: "rule-et-7w", DoseCode: "et_tt_7w", Sequence: 2, TriggerType: "birth_age", OffsetDays: 49, DueWindowDays: 7, MinGapDays: 21, Repeat: "none", CatchUp: "pc_approval"},
			{RuleID: "rule-fmd-12w", DoseCode: "fmd_12w", Sequence: 3, TriggerType: "birth_age", OffsetDays: 84, DueWindowDays: 7, CatchUp: "pc_approval"},
			{RuleID: "rule-hs-12w", DoseCode: "hs_12w", Sequence: 4, TriggerType: "birth_age", OffsetDays: 84, DueWindowDays: 7, CatchUp: "pc_approval"},
			{RuleID: "rule-ppr-16w", DoseCode: "ppr_16w", Sequence: 5, TriggerType: "birth_age", OffsetDays: 112, DueWindowDays: 7, CatchUp: "pc_approval"},
			// Goat Pox source is 16 weeks, but V1 same-day live-live spacing moves the effective row
			// four weeks after PPR when the full Vaccination Rules matrix is loaded.
			{RuleID: "rule-goatpox-20w", DoseCode: "goat_pox_20w", Sequence: 6, TriggerType: "birth_age", OffsetDays: 140, DueWindowDays: 7, CatchUp: "pc_approval"},
		},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{GoatID: "goat-source-table", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Stage: "K1", DOB: &dob}},
		trustedByDue: map[string]bool{
			et4w.UTC().Format(time.RFC3339Nano): true,
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate vaccination rules schedule: %v", err)
	}
	if result.Generated != 5 || result.SuppressedByTrustedHistory != 1 || len(obl.inserted) != 5 {
		t.Fatalf("result=%#v inserted=%#v, want ET 4w suppressed and five source-table obligations generated", result, obl.inserted)
	}
	got := map[string]time.Time{}
	for _, inserted := range obl.inserted {
		got[inserted.RuleID] = inserted.DueAt
	}
	want := map[string]time.Time{
		"rule-et-7w":       dob.AddDate(0, 0, 49),
		"rule-fmd-12w":     dob.AddDate(0, 0, 84),
		"rule-hs-12w":      dob.AddDate(0, 0, 84),
		"rule-ppr-16w":     dob.AddDate(0, 0, 112),
		"rule-goatpox-20w": dob.AddDate(0, 0, 140),
	}
	for ruleID, due := range want {
		if !got[ruleID].Equal(due) {
			t.Fatalf("due for %s = %s, want %s; all=%#v", ruleID, got[ruleID], due, got)
		}
	}
	if _, ok := got["rule-et-4w"]; ok {
		t.Fatalf("trusted ET+TT 4-week dose should not create a duplicate obligation: %#v", got)
	}
}

func TestGenerateEffectiveForAllGoatsSkipsExitedBeforeVersionLookup(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"defer_states":["sick","quarantine","ICU"]}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-dead", LifecycleStatus: "dead", HealthStatus: "sick", DOB: &dob, ParkID: "park-dead"},
		{GoatID: "goat-alive", LifecycleStatus: "alive", HealthStatus: "healthy", DOB: &dob, ParkID: "park-alive"},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateEffectiveForAllGoats(ctx, "tenant-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate effective: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 || obl.inserted[0].TargetID != "goat-alive" {
		t.Fatalf("result=%#v inserted=%#v, want only in-care goat generated", result, obl.inserted)
	}
	if len(proto.effectiveParkID) != 1 || proto.effectiveParkID[0] != "park-alive" {
		t.Fatalf("effective park lookups=%#v, want no lookup for exited goat", proto.effectiveParkID)
	}
}

func TestGenerateForGoatRecheckDefersExistingOpenObligation(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["sick","quarantine","ICU"]}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{
		goat: domain.EligibleGoat{GoatID: "goat-1", LifecycleStatus: "alive", HealthStatus: "healthy", Stage: "K1", DOB: &dob},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	first, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("initial generate: %v", err)
	}
	if first.Generated != 1 || len(obl.inserted) != 1 || obl.inserted[0].Status != "scheduled" {
		t.Fatalf("initial result=%#v inserted=%#v, want one scheduled obligation", first, obl.inserted)
	}

	goats.goat.HealthStatus = "sick"
	recheck, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("recheck generate: %v", err)
	}
	if recheck.Generated != 0 || recheck.Deferred != 1 {
		t.Fatalf("recheck result=%#v, want existing obligation deferred", recheck)
	}
	if obl.inserted[0].Status != "deferred" || len(obl.deferredKeys) != 1 || obl.deferReasons[0] != "sick" {
		t.Fatalf("defer state inserted=%#v keys=%#v reasons=%#v", obl.inserted, obl.deferredKeys, obl.deferReasons)
	}
}

func TestGenerateForGoatRecheckReopensRecoveredObligation(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["sick","quarantine","ICU"]}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{
		goat: domain.EligibleGoat{GoatID: "goat-1", LifecycleStatus: "alive", HealthStatus: "sick", Stage: "K1", DOB: &dob},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	first, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("initial generate: %v", err)
	}
	if first.Deferred != 1 || obl.inserted[0].Status != "deferred" {
		t.Fatalf("initial result=%#v inserted=%#v, want one deferred obligation", first, obl.inserted)
	}

	goats.goat.HealthStatus = "healthy"
	recheck, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("recovery recheck: %v", err)
	}
	if recheck.Reopened != 1 || recheck.Generated != 0 {
		t.Fatalf("recheck result=%#v, want recovered obligation reopened", recheck)
	}
	if obl.inserted[0].Status != "scheduled" || len(obl.reopenedKeys) != 1 {
		t.Fatalf("reopen state inserted=%#v reopened=%#v", obl.inserted, obl.reopenedKeys)
	}
}

func TestGoatRecheckHandlerReopensOnHealthRecovery(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["sick","quarantine","ICU"]}}`),
		rules:   []protodomain.Rule{{RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21}},
	}
	goats := &generationGoatFake{goat: domain.EligibleGoat{GoatID: "goat-1", LifecycleStatus: "alive", HealthStatus: "sick", Stage: "K1", DOB: &dob}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	if _, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("initial generate: %v", err)
	}
	if obl.inserted[0].Status != "deferred" {
		t.Fatalf("precondition: want held obligation, got %s", obl.inserted[0].Status)
	}

	// Pure health recovery (no stage/location change) must reopen via the dedicated health event.
	goats.goat.HealthStatus = "healthy"
	bus := eventbus.NewInProcessBus()
	NewGoatRecheckHandler(gen).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		Type: EventGoatHealthChanged, TenantID: "tenant-1", Key: "goat-1",
		OccurredAt: time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.health.changed: %v", err)
	}
	if obl.inserted[0].Status != "scheduled" || len(obl.reopenedKeys) != 1 {
		t.Fatalf("health recovery must reopen the held obligation via the bus, inserted=%#v reopened=%#v", obl.inserted, obl.reopenedKeys)
	}
	if !obl.inserted[0].DueAt.Equal(time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("micro-drive due=%v, want recovery time", obl.inserted[0].DueAt)
	}
}

func TestGoatRecheckHandlerAlignsRecoveredGoatToNearbyDrive(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["sick"]},"recovery_policy":{"max_nearby_drive_align_days":7}}`),
		rules:   []protodomain.Rule{{RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21, DueWindowDays: 7}},
	}
	goats := &generationGoatFake{goat: domain.EligibleGoat{GoatID: "goat-1", LifecycleStatus: "alive", HealthStatus: "sick", Stage: "K1", DOB: &dob, ShedID: "shed-1", ParkID: "park-1"}}
	nearby := time.Date(2026, time.June, 5, 0, 0, 0, 0, time.UTC)
	obl := &generationObligationFake{seen: map[string]bool{}, nearbyDrive: &nearby}
	gen := NewGenerationService(proto, goats, obl)

	if _, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("initial generate: %v", err)
	}
	goats.goat.HealthStatus = "healthy"
	bus := eventbus.NewInProcessBus()
	NewGoatRecheckHandler(gen).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		Type: EventGoatHealthChanged, TenantID: "tenant-1", Key: "goat-1",
		OccurredAt: time.Date(2026, time.June, 2, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.health.changed: %v", err)
	}
	wantDue := time.Date(2026, time.June, 5, 0, 0, 0, 0, time.UTC)
	if !obl.inserted[0].DueAt.Equal(wantDue) {
		t.Fatalf("aligned due=%v, want nearby drive %v", obl.inserted[0].DueAt, wantDue)
	}
}

func TestGoatRecheckRecoveryRescheduleKeepsCrossVaccineGapFloor(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"GOAT_POX","type":"live","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1","defer_states":["sick"]},"compatibility_policy":{"live_to_live_gap_days":28},"recovery_policy":{"max_nearby_drive_align_days":7}}`),
		rules:   []protodomain.Rule{{RuleID: "rule-1", DoseCode: "goat_pox", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21, DueWindowDays: 7}},
	}
	recentLive := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	goats := &generationGoatFake{
		goat: domain.EligibleGoat{GoatID: "goat-1", LifecycleStatus: "alive", HealthStatus: "sick", Stage: "K1", DOB: &dob, ShedID: "shed-1", ParkID: "park-1"},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"goat-1": {{
				VaccineCode:    "PPR",
				VaccineType:    "live",
				PathogenClass:  "viral",
				AdministeredAt: recentLive,
			}},
		},
	}
	nearby := time.Date(2026, time.June, 5, 0, 0, 0, 0, time.UTC)
	obl := &generationObligationFake{seen: map[string]bool{}, nearbyDrive: &nearby}
	gen := NewGenerationService(proto, goats, obl)

	if _, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", recentLive); err != nil {
		t.Fatalf("initial generate: %v", err)
	}
	if obl.inserted[0].Status != "deferred" {
		t.Fatalf("precondition: status=%s, want deferred", obl.inserted[0].Status)
	}
	goats.goat.HealthStatus = "healthy"
	bus := eventbus.NewInProcessBus()
	NewGoatRecheckHandler(gen).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		Type: EventGoatHealthChanged, TenantID: "tenant-1", Key: "goat-1",
		OccurredAt: time.Date(2026, time.June, 2, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.health.changed: %v", err)
	}
	wantDue := time.Date(2026, time.June, 29, 0, 0, 0, 0, time.UTC)
	if !obl.inserted[0].DueAt.Equal(wantDue) {
		t.Fatalf("recovered due=%v, want live-live floor %v instead of nearby drive %v", obl.inserted[0].DueAt, wantDue, nearby)
	}
}

func TestGenerateScopesObligationToShed(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-1", LifecycleStatus: "alive", Stage: "K1", DOB: &dob, ParkID: "park-1", ShedID: "shed-1"},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(obl.inserted) != 1 || obl.inserted[0].ScopeType != "shed" || obl.inserted[0].ScopeID != "shed-1" {
		t.Fatalf("inserted=%#v, want shed scope so the sweeper batches one drive per shed", obl.inserted)
	}
}

func TestGenerateMissingDOBCreatesVisibleDeferredGap(t *testing.T) {
	ctx := context.Background()
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-no-dob", LifecycleStatus: "alive", Stage: "K1", ParkID: "park-1", ShedID: "shed-1"},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.SkippedNoDueDate != 1 || result.Generated != 1 || result.Deferred != 1 {
		t.Fatalf("result=%#v, want one counted skip and one visible deferred gap", result)
	}
	if len(obl.inserted) != 1 || obl.inserted[0].Status != "deferred" || obl.inserted[0].ScopeType != "shed" {
		t.Fatalf("inserted=%#v, want deferred shed-scoped missing-DOB obligation", obl.inserted)
	}
}

func TestGenerateCancelsMissingDueDateGapWhenSourceDateBackfilled(t *testing.T) {
	ctx := context.Background()
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{goat: domain.EligibleGoat{
		GoatID:          "goat-1",
		LifecycleStatus: "alive",
		Stage:           "K1",
		ShedID:          "shed-1",
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)
	asOf := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	first, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", asOf)
	if err != nil {
		t.Fatalf("initial generate: %v", err)
	}
	if first.SkippedNoDueDate != 1 || first.Deferred != 1 || len(obl.inserted) != 1 || obl.inserted[0].Status != "deferred" {
		t.Fatalf("first result=%#v inserted=%#v, want one visible missing-DOB gap", first, obl.inserted)
	}

	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	goats.goat.DOB = &dob
	second, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", asOf.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("backfill recheck: %v", err)
	}
	if second.Generated != 1 || len(obl.inserted) != 2 {
		t.Fatalf("second result=%#v inserted=%#v, want one real obligation after DOB backfill", second, obl.inserted)
	}
	if obl.inserted[0].Status != "canceled" || len(obl.canceledKeys) != 1 {
		t.Fatalf("missing gap status=%s canceled=%#v, want canceled stale missing-DOB gap", obl.inserted[0].Status, obl.canceledKeys)
	}
	if obl.inserted[1].Status != "scheduled" {
		t.Fatalf("new obligation status=%s, want scheduled", obl.inserted[1].Status)
	}
}

func TestGenerateCancelsMissingPostArrivalGapWhenEntryDateBackfilled(t *testing.T) {
	ctx := context.Background()
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"adult"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-arrival", DoseCode: "wave-1", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 7,
		}},
	}
	goats := &generationGoatFake{goat: domain.EligibleGoat{
		GoatID:          "goat-1",
		LifecycleStatus: "alive",
		Stage:           "adult",
		ShedID:          "shed-1",
		OriginType:      "procured",
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)
	asOf := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	first, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", asOf)
	if err != nil {
		t.Fatalf("initial generate: %v", err)
	}
	if first.SkippedNoDueDate != 1 || first.Deferred != 1 || len(obl.inserted) != 1 || obl.inserted[0].Status != "deferred" {
		t.Fatalf("first result=%#v inserted=%#v, want one visible missing-entry gap", first, obl.inserted)
	}

	entry := time.Date(2026, time.May, 28, 0, 0, 0, 0, time.UTC)
	goats.goat.EntryDate = &entry
	second, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", asOf.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("entry backfill recheck: %v", err)
	}
	if second.Generated != 1 || len(obl.inserted) != 2 {
		t.Fatalf("second result=%#v inserted=%#v, want one real obligation after entry-date backfill", second, obl.inserted)
	}
	if obl.inserted[0].Status != "canceled" || len(obl.canceledKeys) != 1 {
		t.Fatalf("missing gap status=%s canceled=%#v, want stale missing-entry gap canceled", obl.inserted[0].Status, obl.canceledKeys)
	}
	if obl.inserted[1].Status != "scheduled" {
		t.Fatalf("new obligation status=%s, want scheduled", obl.inserted[1].Status)
	}
}

func TestGenerateAppliesMissedDosePolicy(t *testing.T) {
	ctx := context.Background()
	entryDate := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-1", LifecycleStatus: "alive", EntryDate: &entryDate},
	}}

	t.Run("immediate", func(t *testing.T) {
		proto := &generationProtoFake{rules: []protodomain.Rule{{
			RuleID: "rule-immediate", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, CatchUp: "immediate",
		}}}
		obl := &generationObligationFake{seen: map[string]bool{}}
		result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate immediate: %v", err)
		}
		if result.Generated != 1 || !obl.inserted[0].DueAt.Equal(asOf) || obl.inserted[0].Status != "scheduled" {
			t.Fatalf("result=%#v inserted=%#v, want immediate catch-up due now", result, obl.inserted)
		}
	})

	t.Run("pc approval", func(t *testing.T) {
		proto := &generationProtoFake{rules: []protodomain.Rule{{
			RuleID: "rule-pc", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, CatchUp: "pc_approval",
		}}}
		obl := &generationObligationFake{seen: map[string]bool{}}
		result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate pc approval: %v", err)
		}
		if result.Deferred != 1 || len(obl.deferReasons) != 0 || obl.inserted[0].Status != "deferred" {
			t.Fatalf("result=%#v inserted=%#v reasons=%#v, want deferred PC approval gap", result, obl.inserted, obl.deferReasons)
		}
	})

	t.Run("next cycle yearly", func(t *testing.T) {
		proto := &generationProtoFake{rules: []protodomain.Rule{{
			RuleID: "rule-yearly", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, Repeat: "yearly", CatchUp: "next_cycle",
		}}}
		obl := &generationObligationFake{seen: map[string]bool{}}
		result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate yearly: %v", err)
		}
		wantDue := time.Date(2027, time.January, 8, 0, 0, 0, 0, time.UTC)
		if result.Generated != 1 || !obl.inserted[0].DueAt.Equal(wantDue) {
			t.Fatalf("result=%#v inserted=%#v, want next yearly cycle %s", result, obl.inserted, wantDue)
		}
	})

	t.Run("next cycle every n days requires explicit min gap", func(t *testing.T) {
		proto := &generationProtoFake{rules: []protodomain.Rule{{
			RuleID: "rule-n-days", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, Repeat: "every_n_days", CatchUp: "next_cycle",
		}}}
		obl := &generationObligationFake{seen: map[string]bool{}}
		result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate every_n_days without min gap: %v", err)
		}
		if result.Generated != 0 || len(obl.inserted) != 0 {
			t.Fatalf("result=%#v inserted=%#v, want no unsafe every_n_days next-cycle obligation", result, obl.inserted)
		}
	})

	t.Run("next cycle every n days uses min gap", func(t *testing.T) {
		proto := &generationProtoFake{rules: []protodomain.Rule{{
			RuleID: "rule-n-days", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, MinGapDays: 30, Repeat: "every_n_days", CatchUp: "next_cycle",
		}}}
		obl := &generationObligationFake{seen: map[string]bool{}}
		result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate every_n_days: %v", err)
		}
		wantDue := time.Date(2026, time.June, 7, 0, 0, 0, 0, time.UTC)
		if result.Generated != 1 || len(obl.inserted) != 1 || !obl.inserted[0].DueAt.Equal(wantDue) {
			t.Fatalf("result=%#v inserted=%#v, want next cycle due %s", result, obl.inserted, wantDue)
		}
	})

	t.Run("next cycle evidence uses advanced cycle fence", func(t *testing.T) {
		originalDue := time.Date(2026, time.January, 8, 0, 0, 0, 0, time.UTC)
		wantDue := time.Date(2027, time.January, 8, 0, 0, 0, 0, time.UTC)
		proto := &generationProtoFake{rules: []protodomain.Rule{{
			RuleID: "rule-yearly", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, Repeat: "yearly", CatchUp: "next_cycle",
		}}}
		goats := &generationGoatFake{
			list: []domain.EligibleGoat{{GoatID: "goat-1", LifecycleStatus: "alive", EntryDate: &entryDate}},
			trustedByDue: map[string]bool{
				originalDue.UTC().Format(time.RFC3339Nano): true,
			},
		}
		obl := &generationObligationFake{seen: map[string]bool{}}
		result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate yearly with prior-cycle evidence: %v", err)
		}
		if result.Generated != 1 || result.SuppressedByTrustedHistory != 0 || len(obl.inserted) != 1 || !obl.inserted[0].DueAt.Equal(wantDue) {
			t.Fatalf("result=%#v inserted=%#v, want next cycle obligation despite prior-cycle evidence", result, obl.inserted)
		}
		if len(goats.trustedCalls) != 1 || !goats.trustedCalls[0].Equal(wantDue) {
			t.Fatalf("trusted evidence due calls=%#v, want advanced cycle fence %s", goats.trustedCalls, wantDue)
		}
	})

	t.Run("next cycle without repeat skips", func(t *testing.T) {
		proto := &generationProtoFake{rules: []protodomain.Rule{{
			RuleID: "rule-skip", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, Repeat: "none", CatchUp: "next_cycle",
		}}}
		obl := &generationObligationFake{seen: map[string]bool{}}
		result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate next-cycle skip: %v", err)
		}
		if result.Generated != 0 || len(obl.inserted) != 0 {
			t.Fatalf("result=%#v inserted=%#v, want no unsafe non-repeat next-cycle obligation", result, obl.inserted)
		}
	})

	t.Run("next cycle rerun keeps original cycle key", func(t *testing.T) {
		proto := &generationProtoFake{rules: []protodomain.Rule{{
			RuleID: "rule-yearly", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, Repeat: "yearly", CatchUp: "next_cycle",
		}}}
		obl := &generationObligationFake{seen: map[string]bool{}}
		gen := NewGenerationService(proto, goats, obl)

		first, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("first next-cycle generate: %v", err)
		}
		if first.Generated != 1 || len(obl.inserted) != 1 {
			t.Fatalf("first result=%#v inserted=%#v, want one next-cycle obligation", first, obl.inserted)
		}

		second, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2027, time.February, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("second next-cycle generate: %v", err)
		}
		if second.Generated != 0 || len(obl.inserted) != 1 {
			t.Fatalf("second result=%#v inserted=%#v, want no duplicate when asOf advances past next cycle", second, obl.inserted)
		}
	})
}

func TestImmediateCatchUpRecheckKeepsOriginalCycleKey(t *testing.T) {
	ctx := context.Background()
	entryDate := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{rules: []protodomain.Rule{{
		RuleID: "rule-immediate", DoseCode: "dose-1", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, CatchUp: "immediate",
	}}}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-1", LifecycleStatus: "alive", EntryDate: &entryDate},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	firstAsOf := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	first, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", firstAsOf)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if first.Generated != 1 || len(obl.inserted) != 1 || !obl.inserted[0].DueAt.Equal(firstAsOf) {
		t.Fatalf("first result=%#v inserted=%#v, want one immediate catch-up obligation", first, obl.inserted)
	}

	second, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", firstAsOf.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if second.Generated != 0 || len(obl.inserted) != 1 {
		t.Fatalf("second result=%#v inserted=%#v, want no duplicate when asOf moves", second, obl.inserted)
	}
}

func TestOlderGoatUnknownHistoryCreatesOnlyOneHistoricalCatchUp(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{rules: []protodomain.Rule{
		{
			RuleID: "rule-dose-1", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "birth_age", OffsetDays: 180, DueWindowDays: 7, CatchUp: "immediate",
		},
		{
			RuleID: "rule-dose-2", DoseCode: "dose-2", Sequence: 2,
			TriggerType: "birth_age", OffsetDays: 300, DueWindowDays: 7, CatchUp: "immediate",
		},
	}}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "older-goat", LifecycleStatus: "alive", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate older unknown-history goat: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want one historical catch-up only", result, obl.inserted)
	}
	got := obl.inserted[0]
	if got.RuleID != "rule-dose-1" || got.Sequence != 1 || !got.DueAt.Equal(asOf) || got.Status != "scheduled" {
		t.Fatalf("inserted=%#v, want first missed dose as the single safe catch-up", got)
	}
}

func TestOlderGoatUnknownHistoryCreatesOnlyOnePCReviewCatchUp(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{rules: []protodomain.Rule{
		{
			RuleID: "rule-dose-1", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "birth_age", OffsetDays: 180, DueWindowDays: 7, CatchUp: "pc_approval",
		},
		{
			RuleID: "rule-dose-2", DoseCode: "dose-2", Sequence: 2,
			TriggerType: "birth_age", OffsetDays: 300, DueWindowDays: 7, CatchUp: "pc_approval",
		},
	}}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "older-goat", LifecycleStatus: "alive", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate older unknown-history goat: %v", err)
	}
	if result.Generated != 1 || result.Deferred != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want one PC-review catch-up only", result, obl.inserted)
	}
	if got := obl.inserted[0]; got.RuleID != "rule-dose-1" || got.Status != "deferred" || !got.DueAt.Equal(asOf) {
		t.Fatalf("inserted=%#v, want first missed dose as one deferred review item", got)
	}
}

func TestOlderGoatTrustedFirstDoseAllowsNextMissingDose(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	firstDue := dob.AddDate(0, 0, 180)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{rules: []protodomain.Rule{
		{
			RuleID: "rule-dose-1", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "birth_age", OffsetDays: 180, DueWindowDays: 7, CatchUp: "immediate",
		},
		{
			RuleID: "rule-dose-2", DoseCode: "dose-2", Sequence: 2,
			TriggerType: "birth_age", OffsetDays: 300, DueWindowDays: 7, CatchUp: "immediate",
		},
	}}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{GoatID: "older-goat", LifecycleStatus: "alive", DOB: &dob}},
		trustedByDue: map[string]bool{
			firstDue.UTC().Format(time.RFC3339Nano): true,
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate older goat with trusted first dose: %v", err)
	}
	if result.SuppressedByTrustedHistory != 1 || result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want first dose suppressed and next missing dose generated", result, obl.inserted)
	}
	got := obl.inserted[0]
	if got.RuleID != "rule-dose-2" || got.Sequence != 2 || !got.DueAt.Equal(asOf) {
		t.Fatalf("inserted=%#v, want second dose catch-up after trusted first-dose history", got)
	}
}

func TestTrustedPreviousCompletionAllowsAfterPreviousCompletionDose(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 13, 0, 0, 0, 0, time.UTC)
	firstAdmin := time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC)
	firstDue := dob.AddDate(0, 0, 28)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"ET+TT","type":"toxoid","pathogen_class":"bacterial"},"eligibility":{}}`),
		rules: []protodomain.Rule{
			{
				RuleID: "rule-et-4w", DoseCode: "ET_TT_4W", Sequence: 1,
				TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7, CatchUp: "immediate",
			},
			{
				RuleID: "rule-et-7w", DoseCode: "ET_TT_7W", Sequence: 2,
				TriggerType: "after_previous_completion", OffsetDays: 21, DueWindowDays: 7, CatchUp: "immediate",
			},
		},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{GoatID: "kid-1", LifecycleStatus: "alive", DOB: &dob}},
		trustedByDue: map[string]bool{
			firstDue.UTC().Format(time.RFC3339Nano): true,
		},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"kid-1": {{
				AdministeredAt: firstAdmin,
				VaccineCode:    "ET+TT",
				VaccineType:    "toxoid",
				PathogenClass:  "bacterial",
				DoseCode:       "ET_TT_4W",
				Sequence:       1,
			}},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate after previous trusted dose: %v", err)
	}
	wantDue := firstAdmin.AddDate(0, 0, 21)
	if result.SuppressedByTrustedHistory != 1 || result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want dose 1 suppressed and dose 2 generated", result, obl.inserted)
	}
	got := obl.inserted[0]
	if got.RuleID != "rule-et-7w" || got.Sequence != 2 || !got.DueAt.Equal(wantDue) {
		t.Fatalf("inserted=%#v, want ET+TT dose 2 due %s", got, wantDue)
	}
}

func TestTrustedAfterPreviousCompletionSuppressesAlreadyAcceptedDose(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 13, 0, 0, 0, 0, time.UTC)
	firstAdmin := time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC)
	firstDue := dob.AddDate(0, 0, 28)
	secondDue := firstAdmin.AddDate(0, 0, 21)
	asOf := time.Date(2026, time.July, 10, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"ET+TT","type":"toxoid","pathogen_class":"bacterial"},"eligibility":{}}`),
		rules: []protodomain.Rule{
			{
				RuleID: "rule-et-4w", DoseCode: "ET_TT_4W", Sequence: 1,
				TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7, CatchUp: "immediate",
			},
			{
				RuleID: "rule-et-7w", DoseCode: "ET_TT_7W", Sequence: 2,
				TriggerType: "after_previous_completion", OffsetDays: 21, DueWindowDays: 7, CatchUp: "immediate",
			},
		},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{GoatID: "kid-1", LifecycleStatus: "alive", DOB: &dob}},
		trustedByDue: map[string]bool{
			firstDue.UTC().Format(time.RFC3339Nano):  true,
			secondDue.UTC().Format(time.RFC3339Nano): true,
		},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"kid-1": {{
				AdministeredAt: firstAdmin,
				VaccineCode:    "ET+TT",
				VaccineType:    "toxoid",
				PathogenClass:  "bacterial",
				DoseCode:       "ET_TT_4W",
				Sequence:       1,
			}},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate already accepted after-previous dose: %v", err)
	}
	if result.SuppressedByTrustedHistory != 2 || result.Generated != 0 || len(obl.inserted) != 0 {
		t.Fatalf("result=%#v inserted=%#v, want both doses suppressed by trusted history", result, obl.inserted)
	}
	if len(goats.trustedCalls) != 2 || !goats.trustedCalls[1].Equal(secondDue) {
		t.Fatalf("trusted evidence calls=%#v, want history-derived booster due %s checked", goats.trustedCalls, secondDue)
	}
}

func TestAfterPreviousCompletionFromHistoryRespectsMinGap(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 13, 0, 0, 0, 0, time.UTC)
	firstAdmin := time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC)
	firstDue := dob.AddDate(0, 0, 28)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"ET+TT","type":"toxoid","pathogen_class":"bacterial"},"eligibility":{}}`),
		rules: []protodomain.Rule{
			{
				RuleID: "rule-et-4w", DoseCode: "ET_TT_4W", Sequence: 1,
				TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7, CatchUp: "immediate",
			},
			{
				RuleID: "rule-et-7w", DoseCode: "ET_TT_7W", Sequence: 2,
				TriggerType: "after_previous_completion", OffsetDays: 21, MinGapDays: 28, DueWindowDays: 7, CatchUp: "immediate",
			},
		},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{GoatID: "kid-1", LifecycleStatus: "alive", DOB: &dob}},
		trustedByDue: map[string]bool{
			firstDue.UTC().Format(time.RFC3339Nano): true,
		},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"kid-1": {{
				AdministeredAt: firstAdmin,
				VaccineCode:    "ET+TT",
				VaccineType:    "toxoid",
				PathogenClass:  "bacterial",
				DoseCode:       "ET_TT_4W",
				Sequence:       1,
			}},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate min-gap after-previous dose: %v", err)
	}
	wantDue := firstAdmin.AddDate(0, 0, 28)
	if result.SuppressedByTrustedHistory != 1 || result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want first dose suppressed and booster generated", result, obl.inserted)
	}
	if got := obl.inserted[0]; got.RuleID != "rule-et-7w" || !got.DueAt.Equal(wantDue) {
		t.Fatalf("inserted=%#v, want min-gap booster due %s", got, wantDue)
	}
}

func TestAfterPreviousCompletionHistoryContinuesAdultRepeatCycle(t *testing.T) {
	ctx := context.Background()
	lastAdmin := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"adult"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-fmd-adult", DoseCode: "FMD_ADULT", Sequence: 1,
			TriggerType: "after_previous_completion", Repeat: "yearly", DueWindowDays: 7, CatchUp: "immediate",
		}},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{GoatID: "adult-1", LifecycleStatus: "alive", Stage: "adult"}},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"adult-1": {{
				AdministeredAt: lastAdmin,
				VaccineCode:    "FMD",
				VaccineType:    "killed",
				PathogenClass:  "viral",
				DoseCode:       "FMD_ADULT",
				Sequence:       1,
			}},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate adult repeat from history: %v", err)
	}
	wantDue := lastAdmin.AddDate(1, 0, 0)
	if result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want adult repeat generated from accepted history", result, obl.inserted)
	}
	if got := obl.inserted[0]; got.RuleID != "rule-fmd-adult" || got.Sequence != 1 || !got.DueAt.Equal(wantDue) {
		t.Fatalf("inserted=%#v, want adult repeat due %s", got, wantDue)
	}
}

func TestGenerateForGoatUsesEffectiveVersionsAsOf(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{goat: domain.EligibleGoat{GoatID: "goat-1", LifecycleStatus: "alive", DOB: &dob}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	asOf := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)
	if _, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", asOf); err != nil {
		t.Fatalf("generate for goat: %v", err)
	}
	if len(proto.effectiveAsOf) != 1 || !proto.effectiveAsOf[0].Equal(asOf) {
		t.Fatalf("effectiveAsOf=%#v, want per-goat generation to select the version effective at asOf", proto.effectiveAsOf)
	}
}

func TestGenerateEffectiveForAllGoatsUsesPerGoatParkPrecedence(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-park-a", LifecycleStatus: "alive", DOB: &dob, ParkID: "park-a"},
		{GoatID: "goat-park-b", LifecycleStatus: "alive", DOB: &dob, ParkID: "park-b"},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	asOf := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)
	result, err := gen.GenerateEffectiveForAllGoats(ctx, "tenant-1", asOf)
	if err != nil {
		t.Fatalf("effective cohort generate: %v", err)
	}
	if result.Generated != 2 || len(obl.inserted) != 2 {
		t.Fatalf("result=%#v inserted=%#v, want two effective goat obligations", result, obl.inserted)
	}
	if len(proto.effectiveParkID) != 2 || proto.effectiveParkID[0] != "park-a" || proto.effectiveParkID[1] != "park-b" {
		t.Fatalf("effective park IDs=%#v, want per-goat park lookup", proto.effectiveParkID)
	}
}

func TestGenerateForGoatCancelsOpenWorkWhenEligibilityNoLongerMatches(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.June, 3, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{goat: domain.EligibleGoat{
		GoatID:          "goat-1",
		LifecycleStatus: "alive",
		Stage:           "adult",
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", asOf)
	if err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if result.Generated != 0 || len(obl.inserted) != 0 {
		t.Fatalf("result=%#v inserted=%#v, want no new work", result, obl.inserted)
	}
	lastReason := obl.cancelReasons[len(obl.cancelReasons)-1]
	if len(obl.canceledVersions) != 1 || obl.canceledVersions[0] != "version-1" || lastReason != "ineligible_after_shift" {
		t.Fatalf("version cancellations=%#v reasons=%#v", obl.canceledVersions, obl.cancelReasons)
	}
}

func TestGenerateForGoatCancelsOpenWorkForNoLongerEffectiveVersions(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.June, 3, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		effectiveVersions: []string{"version-2"},
		ruleDSLByVersion:  map[string][]byte{"version-2": []byte(`{"eligibility":{"animal_stage":"adult"}}`)},
		rules: []protodomain.Rule{{
			RuleID: "rule-2", DoseCode: "dose-2", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	goats := &generationGoatFake{goat: domain.EligibleGoat{
		GoatID:          "goat-1",
		LifecycleStatus: "alive",
		Stage:           "adult",
		DOB:             &dob,
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", asOf)
	if err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want current effective version generated", result, obl.inserted)
	}
	if len(obl.canceledExceptVersions) != 1 || obl.canceledExceptVersions[0][0] != "version-2" || obl.cancelReasons[0] != "version_no_longer_effective_after_recheck" {
		t.Fatalf("except cancellations=%#v reasons=%#v", obl.canceledExceptVersions, obl.cancelReasons)
	}
}

type generationProtoFake struct {
	rules             []protodomain.Rule
	ruleDSL           []byte
	ruleDSLByVersion  map[string][]byte
	effectiveVersions []string
	effectiveAsOf     []time.Time
	effectiveParkID   []string
}

func (p *generationProtoFake) GetVersion(_ context.Context, _ string, versionID string) (protodomain.Version, error) {
	ruleDSL := p.ruleDSL
	if p.ruleDSLByVersion != nil {
		ruleDSL = p.ruleDSLByVersion[versionID]
	}
	if versionID == "" {
		versionID = "version-1"
	}
	return protodomain.Version{ProtocolVersionID: versionID, Status: "published", ScopeType: "tenant", RuleDsl: ruleDSL}, nil
}

func (p *generationProtoFake) ListRules(context.Context, string, string) ([]protodomain.Rule, error) {
	if len(p.rules) > 0 {
		return p.rules, nil
	}
	return []protodomain.Rule{{
		RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "manual_campaign",
	}}, nil
}

func (p *generationProtoFake) ListEffectiveVaccinationVersionsForGoat(_ context.Context, _ string, parkID string, asOf time.Time) ([]string, error) {
	p.effectiveAsOf = append(p.effectiveAsOf, asOf)
	p.effectiveParkID = append(p.effectiveParkID, parkID)
	if len(p.effectiveVersions) > 0 {
		return p.effectiveVersions, nil
	}
	return []string{"version-1"}, nil
}

type generationGoatFake struct {
	list           []domain.EligibleGoat
	goat           domain.EligibleGoat
	filters        []domain.ImpactFilter
	trustedByDue   map[string]bool
	trustedCalls   []time.Time
	lastVaccine    map[string]domain.RecentVaccineAdministration
	vaccineHistory map[string][]domain.RecentVaccineAdministration
}

func (g *generationGoatFake) ListEligibleGoatsForGeneration(_ context.Context, f domain.ImpactFilter, _ string, _ int32) ([]domain.EligibleGoat, error) {
	g.filters = append(g.filters, f)
	if len(g.list) > 0 {
		return g.list, nil
	}
	return []domain.EligibleGoat{{GoatID: "goat-1", LifecycleStatus: "alive"}}, nil
}

func (g *generationGoatFake) GetGoatForGeneration(context.Context, string, string) (domain.EligibleGoat, bool, error) {
	if g.goat.GoatID != "" {
		return g.goat, true, nil
	}
	return domain.EligibleGoat{GoatID: "goat-1", LifecycleStatus: "alive"}, true, nil
}

func (g *generationGoatFake) HasTrustedCompletionEvidence(_ context.Context, _, _, _, _, _ string, dueAt, _ time.Time) (bool, error) {
	g.trustedCalls = append(g.trustedCalls, dueAt)
	if g.trustedByDue == nil {
		return false, nil
	}
	return g.trustedByDue[dueAt.UTC().Format(time.RFC3339Nano)], nil
}

func (g *generationGoatFake) LastRecentVaccineAdministrationsForGoats(_ context.Context, _ string, goatIDs []string, _ time.Time) (map[string]domain.RecentVaccineAdministration, error) {
	out := make(map[string]domain.RecentVaccineAdministration, len(goatIDs))
	for _, id := range goatIDs {
		if admin, ok := g.lastVaccine[id]; ok {
			out[id] = admin
		}
	}
	return out, nil
}

func (g *generationGoatFake) RecentVaccineAdministrationsForGoats(_ context.Context, _ string, goatIDs []string, _ time.Time) (map[string][]domain.RecentVaccineAdministration, error) {
	out := make(map[string][]domain.RecentVaccineAdministration, len(goatIDs))
	for _, id := range goatIDs {
		if admins, ok := g.vaccineHistory[id]; ok {
			out[id] = admins
			continue
		}
		if admin, ok := g.lastVaccine[id]; ok {
			out[id] = []domain.RecentVaccineAdministration{admin}
		}
	}
	return out, nil
}

type generationObligationFake struct {
	seen                   map[string]bool
	keyIndex               map[string]int
	inserted               []obldomain.NewObligation
	deferredKeys           []string
	deferReasons           []string
	reopenedKeys           []string
	canceledKeys           []string
	canceledVersions       []string
	canceledExceptVersions [][]string
	cancelReasons          []string
	nearbyDrive            *time.Time
	failOnceAfterInserted  int
	failed                 bool
}

func (o *generationObligationFake) InsertObligation(_ context.Context, in obldomain.NewObligation) (string, bool, error) {
	if o.seen == nil {
		o.seen = map[string]bool{}
	}
	if o.keyIndex == nil {
		o.keyIndex = map[string]int{}
	}
	if o.seen[in.IdempotencyKey] {
		return "obligation-1", false, nil
	}
	if o.failOnceAfterInserted > 0 && len(o.inserted) >= o.failOnceAfterInserted && !o.failed {
		o.failed = true
		return "", false, errors.New("forced partial failure")
	}
	o.seen[in.IdempotencyKey] = true
	o.keyIndex[in.IdempotencyKey] = len(o.inserted)
	o.inserted = append(o.inserted, in)
	return "obligation-1", true, nil
}

func (o *generationObligationFake) DeferOpenObligationByIdempotencyKey(_ context.Context, _, idempotencyKey, reason string, _ time.Time) (string, bool, error) {
	idx, ok := o.keyIndex[idempotencyKey]
	if !ok {
		return "", false, errors.New("obligation not found")
	}
	switch o.inserted[idx].Status {
	case "deferred", "completed", "waived", "canceled", "superseded":
		return "obligation-1", false, nil
	}
	o.inserted[idx].Status = "deferred"
	o.deferredKeys = append(o.deferredKeys, idempotencyKey)
	o.deferReasons = append(o.deferReasons, reason)
	return "obligation-1", true, nil
}

func (o *generationObligationFake) ReopenDeferredObligationByIdempotencyKey(_ context.Context, _, idempotencyKey string, _ time.Time, reschedule *obldomain.RecoveryReschedule) (string, bool, error) {
	idx, ok := o.keyIndex[idempotencyKey]
	if !ok {
		return "", false, nil
	}
	if o.inserted[idx].Status != "deferred" {
		return "", false, nil
	}
	o.inserted[idx].Status = "scheduled"
	if reschedule != nil {
		o.inserted[idx].DueAt = reschedule.DueAt
		o.inserted[idx].WindowStart = &reschedule.WindowStart
		o.inserted[idx].WindowEnd = reschedule.WindowEnd
	}
	o.reopenedKeys = append(o.reopenedKeys, idempotencyKey)
	return "obligation-1", true, nil
}

func (o *generationObligationFake) FindNearestPlannedBatchDate(_ context.Context, _, _, _, _, _ string, _, _ time.Time) (*time.Time, error) {
	return o.nearbyDrive, nil
}

func (o *generationObligationFake) CancelOpenObligationByIdempotencyKey(_ context.Context, _, idempotencyKey, _ string, _ time.Time) (string, bool, error) {
	idx, ok := o.keyIndex[idempotencyKey]
	if !ok {
		return "", false, nil
	}
	switch o.inserted[idx].Status {
	case "completed", "canceled", "superseded", "waived":
		return "obligation-1", false, nil
	}
	o.inserted[idx].Status = "canceled"
	o.canceledKeys = append(o.canceledKeys, idempotencyKey)
	return "obligation-1", true, nil
}

func (o *generationObligationFake) CancelOpenVaccinationObligationsForGoatExceptVersions(_ context.Context, _, _ string, versionIDs []string, reason string, _ time.Time) (int, error) {
	o.canceledExceptVersions = append(o.canceledExceptVersions, append([]string(nil), versionIDs...))
	o.cancelReasons = append(o.cancelReasons, reason)
	return 0, nil
}

func (o *generationObligationFake) CancelOpenVaccinationObligationsForGoatVersion(_ context.Context, _, _, versionID, reason string, _ time.Time) (int, error) {
	o.canceledVersions = append(o.canceledVersions, versionID)
	o.cancelReasons = append(o.cancelReasons, reason)
	return 1, nil
}

func (o *generationObligationFake) RecordStatusEvent(context.Context, obldomain.NewStatusEvent) (string, bool, error) {
	return "event-1", true, nil
}

type generationRunRecorderFake struct {
	byKey       map[string]domain.GenerationRun
	startInputs []domain.GenerationRunInput
}

func (r *generationRunRecorderFake) StartGenerationRun(_ context.Context, in domain.GenerationRunInput) (domain.GenerationRun, bool, error) {
	r.startInputs = append(r.startInputs, in)
	if run, ok := r.byKey[in.IdempotencyKey]; ok {
		if run.Status == "failed" {
			run.Status = "running"
			run.CompletedAt = nil
			run.Generated = 0
			run.Deferred = 0
			run.Reopened = 0
			run.SkippedNoDueDate = 0
			run.SuppressedByTrustedHistory = 0
			run.LastError = ""
			r.byKey[in.IdempotencyKey] = run
			return run, true, nil
		}
		return run, false, nil
	}
	run := domain.GenerationRun{
		RunID: "run-1", TenantID: in.TenantID, ProtocolVersionID: in.ProtocolVersionID,
		TriggerType: in.TriggerType, TriggerRef: in.TriggerRef, Status: "running",
		StartedAt: in.StartedAt, IdempotencyKey: in.IdempotencyKey, RequestHash: in.RequestHash,
	}
	r.byKey[in.IdempotencyKey] = run
	return run, true, nil
}

func (r *generationRunRecorderFake) HeartbeatGenerationRun(_ context.Context, _, _ string) error {
	return nil
}

func (r *generationRunRecorderFake) FinishGenerationRun(_ context.Context, tenantID, runID string, result domain.GenerateResult, _ string, lastError string, completedAt time.Time) error {
	for key, run := range r.byKey {
		if run.RunID != runID {
			continue
		}
		run.TenantID = tenantID
		run.Status = "completed"
		if lastError != "" {
			run.Status = "failed"
		}
		run.CompletedAt = &completedAt
		run.Generated = result.Generated
		run.Deferred = result.Deferred
		run.Reopened = result.Reopened
		run.SkippedNoDueDate = result.SkippedNoDueDate
		run.SuppressedByTrustedHistory = result.SuppressedByTrustedHistory
		run.LastError = lastError
		r.byKey[key] = run
	}
	return nil
}
