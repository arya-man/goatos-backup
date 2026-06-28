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

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 || obl.inserted[0].TargetID != "goat-k1" {
		t.Fatalf("result=%#v inserted=%#v, want only K1 goat generated", result, obl.inserted)
	}
	if len(goats.filters) != 1 || goats.filters[0].Stage != "K1" {
		t.Fatalf("impact filters=%#v, want Config animal_stage propagated", goats.filters)
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

	t.Run("phc approval", func(t *testing.T) {
		proto := &generationProtoFake{rules: []protodomain.Rule{{
			RuleID: "rule-phc", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, CatchUp: "phc_approval",
		}}}
		obl := &generationObligationFake{seen: map[string]bool{}}
		result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate phc approval: %v", err)
		}
		if result.Deferred != 1 || len(obl.deferReasons) != 0 || obl.inserted[0].Status != "deferred" {
			t.Fatalf("result=%#v inserted=%#v reasons=%#v, want deferred PHC approval gap", result, obl.inserted, obl.deferReasons)
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

type generationProtoFake struct {
	rules           []protodomain.Rule
	ruleDSL         []byte
	effectiveAsOf   []time.Time
	effectiveParkID []string
}

func (p *generationProtoFake) GetVersion(context.Context, string, string) (protodomain.Version, error) {
	return protodomain.Version{ProtocolVersionID: "version-1", Status: "published", ScopeType: "tenant", RuleDsl: p.ruleDSL}, nil
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
	return []string{"version-1"}, nil
}

type generationGoatFake struct {
	list         []domain.EligibleGoat
	goat         domain.EligibleGoat
	filters      []domain.ImpactFilter
	trustedByDue map[string]bool
	trustedCalls []time.Time
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

type generationObligationFake struct {
	seen                  map[string]bool
	keyIndex              map[string]int
	inserted              []obldomain.NewObligation
	deferredKeys          []string
	deferReasons          []string
	reopenedKeys          []string
	failOnceAfterInserted int
	failed                bool
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

func (o *generationObligationFake) ReopenDeferredObligationByIdempotencyKey(_ context.Context, _, idempotencyKey string, _ time.Time) (string, bool, error) {
	idx, ok := o.keyIndex[idempotencyKey]
	if !ok {
		return "", false, nil
	}
	if o.inserted[idx].Status != "deferred" {
		return "", false, nil
	}
	o.inserted[idx].Status = "scheduled"
	o.reopenedKeys = append(o.reopenedKeys, idempotencyKey)
	return "obligation-1", true, nil
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
