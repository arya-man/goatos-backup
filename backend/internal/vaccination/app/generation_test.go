package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestGenerateForVersionAppliesCrossVaccineGap(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) // ~14w at asOf: inside the kid start window
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
	wantDue := businessDayStart(pprAt).AddDate(0, 0, 28)
	if !obl.inserted[0].DueAt.Equal(wantDue) {
		t.Fatalf("due=%s want %s (4-week live→live gap after PPR)", obl.inserted[0].DueAt, wantDue)
	}
}

func TestGenerateForVersionChecksAllRecentVaccinesForCrossGap(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) // ~14w at asOf: inside the kid start window
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
	wantDue := businessDayStart(pprAt).AddDate(0, 0, 28)
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

// RV-03: continuation anchors on the vaccine's latest real-world administration
// regardless of protocol/version. A cross-protocol dose of the SAME vaccine must
// anchor the next due date (latest administration + current protocol interval), not
// be ignored — otherwise a missing-anchor animal whose primary is suppressed by
// cross-protocol history gets nothing scheduled at all.
func TestDueAfterPreviousCompletionAnchorsCrossProtocol(t *testing.T) {
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
	wantDue := businessDayStart(administered).AddDate(0, 0, 21)

	otherProtocol := []domain.RecentVaccineAdministration{{
		AdministeredAt:    administered,
		VaccineCode:       "ET_TT",
		Sequence:          1,
		ProtocolVersionID: "version-other",
		ProtocolID:        "protocol-other",
	}}
	due, ok := dueAfterPreviousCompletion(rule, vaccineProfile{Code: "ET_TT"}, otherProtocol)
	if !ok || !due.Equal(wantDue) {
		t.Fatalf("due=%s ok=%v, want cross-protocol completion to anchor next due at %s", due, ok, wantDue)
	}

	sameProtocolPreviousVersion := []domain.RecentVaccineAdministration{{
		AdministeredAt:    administered,
		VaccineCode:       "ET_TT",
		Sequence:          1,
		ProtocolVersionID: "version-v1",
		ProtocolID:        "protocol-vaccination",
	}}
	due, ok = dueAfterPreviousCompletion(rule, vaccineProfile{Code: "ET_TT"}, sameProtocolPreviousVersion)
	if !ok || !due.Equal(wantDue) {
		t.Fatalf("due=%s ok=%v, want same protocol lineage accepted", due, ok)
	}

	// The LATEST administration wins regardless of which protocol it came from.
	newerCrossProtocol := []domain.RecentVaccineAdministration{
		{
			AdministeredAt:    administered.AddDate(0, 0, -90),
			VaccineCode:       "ET_TT",
			Sequence:          1,
			ProtocolVersionID: "version-v1",
			ProtocolID:        "protocol-vaccination",
		},
		{
			AdministeredAt:    administered,
			VaccineCode:       "ET_TT",
			Sequence:          1,
			ProtocolVersionID: "version-other",
			ProtocolID:        "protocol-other",
		},
	}
	due, ok = dueAfterPreviousCompletion(rule, vaccineProfile{Code: "ET_TT"}, newerCrossProtocol)
	if !ok || !due.Equal(wantDue) {
		t.Fatalf("due=%s ok=%v, want latest (cross-protocol) administration to anchor next due at %s", due, ok, wantDue)
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

func TestManualCampaignHandlerRejectsFutureOccurredAtBeforeStartingRun(t *testing.T) {
	ctx := context.Background()
	proto := &generationProtoFake{}
	goats := &generationGoatFake{}
	obl := &generationObligationFake{seen: map[string]bool{}}
	runs := &generationRunRecorderFake{byKey: map[string]domain.GenerationRun{}}
	gen := NewGenerationService(proto, goats, obl).WithGenerationRunRecorder(runs)
	handler := NewManualCampaignHandler(gen)

	err := handler.HandleEvent(ctx, eventbus.Event{
		Type:       EventManualCampaignRequested,
		TenantID:   "tenant-1",
		OccurredAt: time.Now().Add(48 * time.Hour),
		Payload:    []byte(`{"protocol_version_id":"version-1","campaign_id":"catchup"}`),
	})
	if !errors.Is(err, domain.ErrFutureManualCampaign) {
		t.Fatalf("err=%v, want future manual campaign error", err)
	}
	if !eventbus.IsPermanentError(err) {
		t.Fatalf("err=%v, want permanent event error", err)
	}
	if len(runs.startInputs) != 0 || len(obl.inserted) != 0 {
		t.Fatalf("future event started work: startInputs=%#v inserted=%#v", runs.startInputs, obl.inserted)
	}
}

func TestManualCampaignEventFutureRecordedAtStaysInvalidAfterOccurredAt(t *testing.T) {
	event := eventbus.Event{
		Type:       EventManualCampaignRequested,
		TenantID:   "tenant-1",
		OccurredAt: time.Date(2026, time.June, 28, 8, 0, 0, 0, time.UTC),
		RecordedAt: time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC),
		Payload:    []byte(`{"protocol_version_id":"version-1","campaign_id":"catchup"}`),
	}
	now := time.Date(2026, time.June, 30, 8, 0, 0, 0, time.UTC)
	if !manualCampaignEventAsOfInFuture(event, now) {
		t.Fatalf("future-at-recording manual event became valid after occurred_at passed")
	}

	ctx := context.Background()
	proto := &generationProtoFake{}
	goats := &generationGoatFake{}
	obl := &generationObligationFake{seen: map[string]bool{}}
	runs := &generationRunRecorderFake{byKey: map[string]domain.GenerationRun{}}
	gen := NewGenerationService(proto, goats, obl).WithGenerationRunRecorder(runs)
	err := NewManualCampaignHandler(gen).HandleEvent(ctx, event)
	if !errors.Is(err, domain.ErrFutureManualCampaign) || !eventbus.IsPermanentError(err) {
		t.Fatalf("err=%v, want permanent future manual campaign error", err)
	}
	if len(runs.startInputs) != 0 || len(obl.inserted) != 0 {
		t.Fatalf("future-at-recording event started work: startInputs=%#v inserted=%#v", runs.startInputs, obl.inserted)
	}
}

func TestManualCampaignHTTPRunRejectsFutureAsOfBeforeStartingRun(t *testing.T) {
	ctx := context.Background()
	proto := &generationProtoFake{}
	goats := &generationGoatFake{}
	obl := &generationObligationFake{seen: map[string]bool{}}
	runs := &generationRunRecorderFake{byKey: map[string]domain.GenerationRun{}}
	gen := NewGenerationService(proto, goats, obl).WithGenerationRunRecorder(runs)

	_, _, err := gen.GenerateManualCampaignForVersionWithHTTPRun(ctx, "tenant-1", "version-1", "catchup", time.Now().Add(48*time.Hour), "manual-key-future", "hash-future")
	if !errors.Is(err, domain.ErrFutureManualCampaign) {
		t.Fatalf("err=%v, want future manual campaign error", err)
	}
	if len(runs.startInputs) != 0 || len(obl.inserted) != 0 {
		t.Fatalf("future HTTP run started work: startInputs=%#v inserted=%#v", runs.startInputs, obl.inserted)
	}
}

func TestManualCampaignHTTPRunPoisonGoatFailsRunAfterCountingFailure(t *testing.T) {
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
	_, firstResult, err := gen.GenerateManualCampaignForVersionWithHTTPRun(ctx, "tenant-1", "version-1", "catchup", firstAt, "manual-key-failed", "hash-1")
	if !errors.Is(err, errGenerationPartialFailures) {
		t.Fatalf("first manual campaign err=%v, want partial failure", err)
	}
	if firstResult.Generated != 1 || firstResult.FailedGoats != 1 || len(obl.inserted) != 1 || !runs.byKey["manual-key-failed"].StartedAt.Equal(firstAt) || runs.byKey["manual-key-failed"].Status != "failed" {
		t.Fatalf("first completed state inserted=%#v result=%#v run=%#v", obl.inserted, firstResult, runs.byKey["manual-key-failed"])
	}

	_, result, err := gen.GenerateManualCampaignForVersionWithHTTPRun(ctx, "tenant-1", "version-1", "catchup", secondAt, "manual-key-failed", "hash-1")
	if err != nil {
		t.Fatalf("replay manual campaign: %v", err)
	}
	if result.Generated != 1 || result.FailedGoats != 0 || len(obl.inserted) != 2 || runs.byKey["manual-key-failed"].Status != "completed" {
		t.Fatalf("retry result=%#v inserted=%#v run=%#v, want failed run to retry missing work and complete", result, obl.inserted, runs.byKey["manual-key-failed"])
	}
	for i, obligation := range obl.inserted {
		wantDue := businessDayStart(firstAt)
		if !obligation.DueAt.Equal(wantDue) {
			t.Fatalf("inserted[%d].DueAt = %s, want original as_of business day %s", i, obligation.DueAt, wantDue)
		}
	}
}

func TestGenerateForVersionContinuesAfterPerGoatFailureThenFailsRun(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-1", LifecycleStatus: "alive", Stage: "K1", DOB: &dob},
		{GoatID: "goat-2", LifecycleStatus: "alive", Stage: "K1", DOB: &dob},
		{GoatID: "goat-3", LifecycleStatus: "alive", Stage: "K1", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}, failOnceAfterInserted: 1}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, errGenerationPartialFailures) {
		t.Fatalf("generate err=%v, want partial failure", err)
	}
	if result.Generated != 2 || result.FailedGoats != 1 {
		t.Fatalf("result=%#v, want two generated and one failed goat", result)
	}
	if len(obl.inserted) != 2 || obl.inserted[0].TargetID != "goat-1" || obl.inserted[1].TargetID != "goat-3" {
		t.Fatalf("inserted=%#v, want scan to continue through goat-3 after goat-2 failure", obl.inserted)
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
			EligibilityJSON: []byte(`{"matrix_row_id":"kid-goat-primary","eligibility":{"species":["goat"],"animal_stage":["K1","K2"],"sex":["female"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"],"defer_states":["icu","quarantine"]},"vaccine":{"code":"ET_TT","type":"killed","pathogen_class":"bacterial"}}`),
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
	// Age/age-band exclusions still hold (too-young/too-old/wrong-band dropped),
	// but a clinical lifecycle hold is NOT an exclusion: the in-care sick goat is
	// structurally eligible (kid age band, in age window) and must receive a
	// DEFERRED obligation held for recovery, not be silently dropped (C35-010).
	statusByGoat := map[string]string{}
	for _, ob := range obl.inserted {
		statusByGoat[ob.TargetID] = ob.Status
	}
	if result.Generated != 2 || result.Deferred != 1 {
		t.Fatalf("result=%#v, want Generated=2 (eligible scheduled + sick deferred), Deferred=1", result)
	}
	if statusByGoat["goat-eligible"] != "scheduled" {
		t.Fatalf("goat-eligible status=%q, want scheduled; inserted=%#v", statusByGoat["goat-eligible"], obl.inserted)
	}
	if statusByGoat["goat-held-lifecycle"] != "deferred" {
		t.Fatalf("goat-held-lifecycle status=%q, want deferred (clinical hold, not excluded); inserted=%#v", statusByGoat["goat-held-lifecycle"], obl.inserted)
	}
	for _, dropped := range []string{"goat-too-young", "goat-too-old", "goat-wrong-band"} {
		if _, ok := statusByGoat[dropped]; ok {
			t.Fatalf("%s should be excluded by age/age-band, got status %q", dropped, statusByGoat[dropped])
		}
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
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"adult","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","under_treatment","quarantine","icu"]},"procurement_policy":{"warmup_no_vaccination_days":7,"kids_normal_schedule_until_weeks":16,"adult_prior_vaccination_allowed":true}}`),
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
	wantDue := businessDayStart(time.Date(2026, time.July, 8, 0, 0, 0, 0, time.UTC))
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
		"rule-et-7w":       businessDayStart(dob).AddDate(0, 0, 49),
		"rule-fmd-12w":     businessDayStart(dob).AddDate(0, 0, 84),
		"rule-hs-12w":      businessDayStart(dob).AddDate(0, 0, 84),
		"rule-ppr-16w":     businessDayStart(dob).AddDate(0, 0, 112),
		"rule-goatpox-20w": businessDayStart(dob).AddDate(0, 0, 140),
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

func TestGenerateForVersionContinuesAfterPoisonGoat(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["sick","quarantine","ICU"]}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-1", LifecycleStatus: "alive", HealthStatus: "healthy", Stage: "K1", DOB: &dob},
		{GoatID: "goat-2", LifecycleStatus: "alive", HealthStatus: "healthy", Stage: "K1", DOB: &dob},
		{GoatID: "goat-3", LifecycleStatus: "alive", HealthStatus: "healthy", Stage: "K1", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}, failOnceAfterInserted: 1}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, errGenerationPartialFailures) {
		t.Fatalf("generate err=%v, want partial failure", err)
	}
	if result.Generated != 2 || result.FailedGoats != 1 || len(obl.inserted) != 2 {
		t.Fatalf("result=%#v inserted=%#v, want poison goat counted, later goat processed, and run failed", result, obl.inserted)
	}
	if obl.inserted[0].TargetID != "goat-1" || obl.inserted[1].TargetID != "goat-3" {
		t.Fatalf("inserted=%#v, want goat-1 and goat-3 after goat-2 failed", obl.inserted)
	}
}

func TestGenerateForVersionAbortsOnTransientDBError(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["sick","quarantine","ICU"]}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-1", LifecycleStatus: "alive", HealthStatus: "healthy", Stage: "K1", DOB: &dob},
		{GoatID: "goat-2", LifecycleStatus: "alive", HealthStatus: "healthy", Stage: "K1", DOB: &dob},
		{GoatID: "goat-3", LifecycleStatus: "alive", HealthStatus: "healthy", Stage: "K1", DOB: &dob},
	}}
	obl := &generationObligationFake{
		seen:                  map[string]bool{},
		failOnceAfterInserted: 1,
		failErr:               fmt.Errorf("insert obligation: %w", &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}),
	}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err == nil || IsGenerationPartialFailure(err) {
		t.Fatalf("generate err=%v, want hard transient DB abort", err)
	}
	if result.Generated != 1 || result.FailedGoats != 0 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want abort after first generated goat without failed-goat masking", result, obl.inserted)
	}
	if obl.inserted[0].TargetID != "goat-1" {
		t.Fatalf("inserted=%#v, want no work after transient DB error", obl.inserted)
	}
}

func TestShouldAbortGenerationClassifiesRetryableInfrastructure(t *testing.T) {
	retryable := []error{
		context.Canceled,
		context.DeadlineExceeded,
		&pgconn.PgError{Code: "40001", Message: "could not serialize access"},
		&pgconn.PgError{Code: "40P01", Message: "deadlock detected"},
		errors.New("pgxpool: failed to acquire connection: timeout acquiring connection"),
		errors.New("read goat: server closed the connection unexpectedly"),
	}
	for _, err := range retryable {
		if !IsGenerationAbortError(err) {
			t.Fatalf("IsGenerationAbortError(%q)=false, want true", err)
		}
	}

	poison := []error{
		errors.New("forced partial failure"),
		&pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"},
	}
	for _, err := range poison {
		if IsGenerationAbortError(err) {
			t.Fatalf("IsGenerationAbortError(%q)=true, want false", err)
		}
	}
}

func TestGenerateEffectiveForAllGoatsContinuesAfterPoisonGoat(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["sick","quarantine","ICU"]}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-1", LifecycleStatus: "alive", HealthStatus: "healthy", Stage: "K1", DOB: &dob},
		{GoatID: "goat-2", LifecycleStatus: "alive", HealthStatus: "healthy", Stage: "K1", DOB: &dob},
		{GoatID: "goat-3", LifecycleStatus: "alive", HealthStatus: "healthy", Stage: "K1", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}, failOnceAfterInserted: 1}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateEffectiveForAllGoats(ctx, "tenant-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, errGenerationPartialFailures) {
		t.Fatalf("generate effective err=%v, want partial failure", err)
	}
	if result.Generated != 2 || result.FailedGoats != 1 || len(obl.inserted) != 2 {
		t.Fatalf("result=%#v inserted=%#v, want poison goat counted, later goat processed, and run failed", result, obl.inserted)
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
		ruleDSL: []byte(`{"vaccine":{"code":"FMD"},"eligibility":{"animal_stage":"K1","defer_states":["sick"]},"recovery_policy":{"max_nearby_drive_align_days":7}}`),
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
	wantDue := businessDayStart(time.Date(2026, time.June, 5, 0, 0, 0, 0, time.UTC))
	if !obl.inserted[0].DueAt.Equal(wantDue) {
		t.Fatalf("aligned due=%v, want nearby drive %v", obl.inserted[0].DueAt, wantDue)
	}
	if len(obl.nearestBatchLookups) == 0 {
		t.Fatalf("nearest planned batch lookups = %#v, want recovery lookup", obl.nearestBatchLookups)
	}
	for _, lookup := range obl.nearestBatchLookups {
		if lookup.vaccineCode != "FMD" {
			t.Fatalf("nearest planned batch lookup = %#v, want FMD vaccine code", lookup)
		}
	}
}

func TestGenerateEffectiveForAllGoatsAlignsRecoveredGoatToNearbyDrive(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["sick"]},"recovery_policy":{"max_nearby_drive_align_days":7}}`),
		rules:   []protodomain.Rule{{RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21, DueWindowDays: 7}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{{
		GoatID: "goat-1", LifecycleStatus: "alive", HealthStatus: "sick", Stage: "K1", DOB: &dob, ShedID: "shed-1", ParkID: "park-1",
	}}}
	nearby := time.Date(2026, time.June, 5, 0, 0, 0, 0, time.UTC)
	obl := &generationObligationFake{seen: map[string]bool{}, nearbyDrive: &nearby}
	gen := NewGenerationService(proto, goats, obl)

	first, err := gen.GenerateEffectiveForAllGoats(ctx, "tenant-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("initial effective generate: %v", err)
	}
	if first.Deferred != 1 || obl.inserted[0].Status != "deferred" {
		t.Fatalf("initial result=%#v inserted=%#v, want held obligation", first, obl.inserted)
	}

	goats.list[0].HealthStatus = "healthy"
	recheck, err := gen.GenerateEffectiveForAllGoats(ctx, "tenant-1", time.Date(2026, time.June, 2, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("effective recovery recheck: %v", err)
	}
	wantDue := businessDayStart(time.Date(2026, time.June, 5, 0, 0, 0, 0, time.UTC))
	if recheck.Reopened != 1 || !obl.inserted[0].DueAt.Equal(wantDue) {
		t.Fatalf("effective recovery result=%#v due=%v, want reopened on nearby drive %v", recheck, obl.inserted[0].DueAt, wantDue)
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
	wantDue := businessDayStart(recentLive).AddDate(0, 0, 28)
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

// B3 (never-received vaccine + no DOB): a missing DOB must never fabricate a
// deferred missing_due_date gap. It routes to the adult catch-up/primary path at
// the next compatible drive instead — materialized as a normal SCHEDULED
// obligation due today, not a "Deferred" (implies sickness) blocker. Confirmed bug
// #3 in the audit: 531 animals, ~1,994 doses deferred instead of continued/
// caught-up.
func TestGenerateMissingDOBRoutesToAdultCatchUpNotDeferredGap(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
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

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.SkippedNoDueDate != 0 || result.Generated != 1 || result.Deferred != 0 {
		t.Fatalf("result=%#v, want adult catch-up generated, never a missing_due_date skip/defer", result)
	}
	if len(obl.inserted) != 1 || obl.inserted[0].Status != "scheduled" || obl.inserted[0].ScopeType != "shed" {
		t.Fatalf("inserted=%#v, want a scheduled (not deferred) shed-scoped catch-up obligation", obl.inserted)
	}
	if !obl.inserted[0].DueAt.Equal(businessDayStart(asOf)) {
		t.Fatalf("due=%s, want the next-compatible-drive catch-up due today", obl.inserted[0].DueAt)
	}
}

// B7 (dynamic recompute): once DOB is backfilled, the no-DOB catch-up placeholder
// (materialized under B3) is superseded by the real DOB-anchored obligation — the
// same missingKey supersede mechanism the legacy missing_due_date placeholder used.
func TestGenerateCancelsAdultCatchUpGapWhenSourceDateBackfilled(t *testing.T) {
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
	if first.Generated != 1 || first.Deferred != 0 || len(obl.inserted) != 1 || obl.inserted[0].Status != "scheduled" {
		t.Fatalf("first result=%#v inserted=%#v, want one scheduled no-DOB catch-up obligation", first, obl.inserted)
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
		t.Fatalf("catch-up gap status=%s canceled=%#v, want canceled stale no-DOB catch-up", obl.inserted[0].Status, obl.canceledKeys)
	}
	if obl.inserted[1].Status != "scheduled" {
		t.Fatalf("new obligation status=%s, want scheduled", obl.inserted[1].Status)
	}
}

// Same B3/B7 proof for the post_arrival (missing entry-date) anchor.
func TestGenerateCancelsAdultCatchUpGapWhenEntryDateBackfilled(t *testing.T) {
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
	if first.Generated != 1 || first.Deferred != 0 || len(obl.inserted) != 1 || obl.inserted[0].Status != "scheduled" {
		t.Fatalf("first result=%#v inserted=%#v, want one scheduled no-entry-date catch-up obligation", first, obl.inserted)
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
		t.Fatalf("catch-up gap status=%s canceled=%#v, want stale no-entry-date catch-up canceled", obl.inserted[0].Status, obl.canceledKeys)
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

	t.Run("immediate waits for nearby planned drive", func(t *testing.T) {
		nearby := time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC)
		proto := &generationProtoFake{
			rules: []protodomain.Rule{{
				RuleID: "rule-immediate", DoseCode: "dose-1", Sequence: 1,
				TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, CatchUp: "immediate",
			}},
			ruleDSL: []byte(`{"missed_dose_policy":{"nearby_drive_align_days":14}}`),
		}
		obl := &generationObligationFake{seen: map[string]bool{}, nearbyDrive: &nearby}
		result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate immediate nearby: %v", err)
		}
		wantDue := businessDayStart(nearby)
		if result.Generated != 1 || !obl.inserted[0].DueAt.Equal(wantDue) || obl.inserted[0].Status != "scheduled" {
			t.Fatalf("result=%#v inserted=%#v, want catch-up aligned to %s", result, obl.inserted, wantDue)
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
		wantDue := businessDayStart(time.Date(2027, time.January, 8, 0, 0, 0, 0, time.UTC))
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
		wantDue := businessDayStart(time.Date(2026, time.June, 7, 0, 0, 0, 0, time.UTC))
		if result.Generated != 1 || len(obl.inserted) != 1 || !obl.inserted[0].DueAt.Equal(wantDue) {
			t.Fatalf("result=%#v inserted=%#v, want next cycle due %s", result, obl.inserted, wantDue)
		}
	})

	t.Run("next cycle never materializes a past repeat inside its old window", func(t *testing.T) {
		proto := &generationProtoFake{rules: []protodomain.Rule{{
			RuleID: "rule-n-days-window", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 30, MinGapDays: 182, Repeat: "every_n_days", CatchUp: "next_cycle",
		}}}
		obl := &generationObligationFake{seen: map[string]bool{}}
		result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate future repeat: %v", err)
		}
		wantDue := businessDayStart(time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC))
		if result.Generated != 1 || len(obl.inserted) != 1 || !obl.inserted[0].DueAt.Equal(wantDue) {
			t.Fatalf("result=%#v inserted=%#v, want next future repeat %s", result, obl.inserted, wantDue)
		}
		if !obl.inserted[0].DueAt.After(asOf) {
			t.Fatalf("next-cycle repeat due=%s must be after as_of=%s", obl.inserted[0].DueAt, asOf)
		}
	})

	t.Run("next cycle evidence uses advanced cycle fence", func(t *testing.T) {
		originalDue := time.Date(2026, time.January, 8, 0, 0, 0, 0, time.UTC)
		wantDue := businessDayStart(time.Date(2027, time.January, 8, 0, 0, 0, 0, time.UTC))
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

func TestGenerateDefersPostBreedingAndMilkingHolds(t *testing.T) {
	ctx := context.Background()
	entryDate := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	breedingDate := time.Date(2026, time.May, 20, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{rules: []protodomain.Rule{{
		RuleID: "rule-repro", DoseCode: "dose-1", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 1, CatchUp: "immediate",
	}}}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "bred-goat", LifecycleStatus: "alive", EntryDate: &entryDate, ReproductiveStatus: "bred", BreedingDate: &breedingDate},
		{GoatID: "milking-goat", LifecycleStatus: "alive", EntryDate: &entryDate, Stage: "MILKING"},
		{GoatID: "clear-goat", LifecycleStatus: "alive", EntryDate: &entryDate},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate reproductive holds: %v", err)
	}
	if result.Generated != 3 || result.Deferred != 2 || len(obl.inserted) != 3 {
		t.Fatalf("result=%#v inserted=%#v, want 3 generated with 2 deferred", result, obl.inserted)
	}
	statusByGoat := map[string]string{}
	for _, in := range obl.inserted {
		statusByGoat[in.TargetID] = in.Status
	}
	if statusByGoat["bred-goat"] != "deferred" || statusByGoat["milking-goat"] != "deferred" || statusByGoat["clear-goat"] != "scheduled" {
		t.Fatalf("statuses=%#v, want bred/milking deferred and clear scheduled", statusByGoat)
	}
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

// B4 (age transition): these "older goat" catch-up scenarios are ADULT (post_arrival)
// obligations, not birth_age kid doses — an animal old enough for a years-overdue
// dose is never routed through the kid-course rules regardless of DOB, only
// through its own entry-date-anchored adult path. (Previously modeled with
// birth_age + DOB, which only passed by relying on schedulePathForGoat's
// confirmed-bug blank-signal default-to-kid fallback; B4 removes that fallback, so
// these are rewritten on the adult/post_arrival path they actually represent.)
func TestOlderGoatUnknownHistoryCreatesOnlyOneHistoricalCatchUp(t *testing.T) {
	ctx := context.Background()
	entry := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{rules: []protodomain.Rule{
		{
			RuleID: "rule-dose-1", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 180, DueWindowDays: 7, CatchUp: "immediate",
		},
		{
			RuleID: "rule-dose-2", DoseCode: "dose-2", Sequence: 2,
			TriggerType: "post_arrival", OffsetDays: 300, DueWindowDays: 7, CatchUp: "immediate",
		},
	}}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "older-goat", LifecycleStatus: "alive", EntryDate: &entry, OriginType: "procured"},
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
	entry := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{rules: []protodomain.Rule{
		{
			RuleID: "rule-dose-1", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 180, DueWindowDays: 7, CatchUp: "pc_approval",
		},
		{
			RuleID: "rule-dose-2", DoseCode: "dose-2", Sequence: 2,
			TriggerType: "post_arrival", OffsetDays: 300, DueWindowDays: 7, CatchUp: "pc_approval",
		},
	}}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "older-goat", LifecycleStatus: "alive", EntryDate: &entry, OriginType: "procured"},
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
	entry := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	firstDue := entry.AddDate(0, 0, 180)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{rules: []protodomain.Rule{
		{
			RuleID: "rule-dose-1", DoseCode: "dose-1", Sequence: 1,
			TriggerType: "post_arrival", OffsetDays: 180, DueWindowDays: 7, CatchUp: "immediate",
		},
		{
			RuleID: "rule-dose-2", DoseCode: "dose-2", Sequence: 2,
			TriggerType: "post_arrival", OffsetDays: 300, DueWindowDays: 7, CatchUp: "immediate",
		},
	}}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{GoatID: "older-goat", LifecycleStatus: "alive", EntryDate: &entry, OriginType: "procured"}},
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

func TestTrustedHistorySuppressesMissingDateBlocker(t *testing.T) {
	tests := []struct {
		name string
		goat domain.EligibleGoat
		rule protodomain.Rule
	}{
		{
			// Stage="K1" keeps this goat on the kid schedule path (B4's DOB-unknown
			// routing needs a kid signal — a stage tag or kid-course vaccination
			// history — otherwise it correctly routes adult per B3).
			name: "birth age without DOB",
			goat: domain.EligibleGoat{GoatID: "goat-missing-dob", LifecycleStatus: "alive", Stage: "K1"},
			rule: protodomain.Rule{
				RuleID: "rule-primary", DoseCode: "PRIMARY", Sequence: 1,
				TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7, CatchUp: "immediate",
			},
		},
		{
			name: "post arrival without entry date",
			goat: domain.EligibleGoat{GoatID: "goat-missing-entry", LifecycleStatus: "alive", OriginType: "procured"},
			rule: protodomain.Rule{
				RuleID: "rule-arrival", DoseCode: "ARRIVAL", Sequence: 1,
				TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 7, CatchUp: "immediate",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			asOf := time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC)
			proto := &generationProtoFake{rules: []protodomain.Rule{tt.rule}}
			goats := &generationGoatFake{
				list: []domain.EligibleGoat{tt.goat},
				trustedByDue: map[string]bool{
					asOf.UTC().Format(time.RFC3339Nano): true,
				},
			}
			obl := &generationObligationFake{seen: map[string]bool{}}

			result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
			if err != nil {
				t.Fatalf("generate with accepted history and missing source date: %v", err)
			}
			if result.SuppressedByTrustedHistory != 1 || result.Generated != 0 || result.SkippedNoDueDate != 0 || len(obl.inserted) != 0 {
				t.Fatalf("result=%#v inserted=%#v, want accepted history to suppress the missing-date blocker", result, obl.inserted)
			}
			if len(goats.trustedCalls) != 1 || !goats.trustedCalls[0].Equal(asOf) {
				t.Fatalf("trusted evidence calls=%#v, want one as-of lookup", goats.trustedCalls)
			}
		})
	}
}

func TestTrustedPreviousCompletionAllowsAfterPreviousCompletionDose(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 13, 0, 0, 0, 0, time.UTC)
	firstAdmin := time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC)
	firstDue := dob.AddDate(0, 0, 28)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"ET+TT","type":"killed","pathogen_class":"bacterial"},"eligibility":{}}`),
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
				VaccineType:    "killed",
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
	wantDue := businessDayStart(firstAdmin).AddDate(0, 0, 21)
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
	firstDue := businessDayStart(dob).AddDate(0, 0, 28)
	secondDue := businessDayStart(firstAdmin).AddDate(0, 0, 21)
	asOf := time.Date(2026, time.July, 10, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"ET+TT","type":"killed","pathogen_class":"bacterial"},"eligibility":{}}`),
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
				VaccineType:    "killed",
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
		ruleDSL: []byte(`{"vaccine":{"code":"ET+TT","type":"killed","pathogen_class":"bacterial"},"eligibility":{}}`),
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
				VaccineType:    "killed",
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
	wantDue := businessDayStart(firstAdmin).AddDate(0, 0, 28)
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
	wantDue := businessDayStart(lastAdmin).AddDate(1, 0, 0)
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

func TestGenerateEffectiveForAllGoatsCachesEffectiveVersionsPerPark(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "goat-park-a-1", LifecycleStatus: "alive", DOB: &dob, ParkID: "park-a"},
		{GoatID: "goat-park-a-2", LifecycleStatus: "alive", DOB: &dob, ParkID: "park-a"},
		{GoatID: "goat-tenant", LifecycleStatus: "alive", DOB: &dob},
		{GoatID: "goat-tenant-2", LifecycleStatus: "alive", DOB: &dob},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	asOf := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)
	result, err := gen.GenerateEffectiveForAllGoats(ctx, "tenant-1", asOf)
	if err != nil {
		t.Fatalf("effective cohort generate: %v", err)
	}
	if result.Generated != 4 || len(obl.inserted) != 4 {
		t.Fatalf("result=%#v inserted=%#v, want four effective goat obligations", result, obl.inserted)
	}
	if len(proto.effectiveParkID) != 2 || proto.effectiveParkID[0] != "park-a" || proto.effectiveParkID[1] != "" {
		t.Fatalf("effective park IDs=%#v, want one lookup per park/fallback scope", proto.effectiveParkID)
	}
	if proto.getVersionCalls != 0 || proto.listRulesCalls != 0 {
		t.Fatalf("scalar rulebook calls get=%d rules=%d, want batched cohort path", proto.getVersionCalls, proto.listRulesCalls)
	}
	if proto.batchGetVersionCalls != 1 || proto.batchListRulesCalls != 1 || proto.batchEffectiveCalls != 1 {
		t.Fatalf("batch calls versions=%d rules=%d effective=%d, want one page-level call each", proto.batchGetVersionCalls, proto.batchListRulesCalls, proto.batchEffectiveCalls)
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
	rules                []protodomain.Rule
	ruleDSL              []byte
	ruleDSLByVersion     map[string][]byte
	effectiveVersions    []string
	effectiveAsOf        []time.Time
	effectiveParkID      []string
	getVersionCalls      int
	listRulesCalls       int
	batchGetVersionCalls int
	batchListRulesCalls  int
	batchEffectiveCalls  int
}

func (p *generationProtoFake) GetVersion(_ context.Context, _ string, versionID string) (protodomain.Version, error) {
	p.getVersionCalls++
	return p.versionFor(versionID), nil
}

func (p *generationProtoFake) versionFor(versionID string) protodomain.Version {
	ruleDSL := p.ruleDSL
	if p.ruleDSLByVersion != nil {
		ruleDSL = p.ruleDSLByVersion[versionID]
	}
	if versionID == "" {
		versionID = "version-1"
	}
	return protodomain.Version{ProtocolVersionID: versionID, Status: "published", ScopeType: "tenant", RuleDsl: ruleDSL}
}

func (p *generationProtoFake) ListRules(context.Context, string, string) ([]protodomain.Rule, error) {
	p.listRulesCalls++
	return p.rulesForVersion(), nil
}

func (p *generationProtoFake) rulesForVersion() []protodomain.Rule {
	if len(p.rules) > 0 {
		return p.rules
	}
	return []protodomain.Rule{{
		RuleID: "rule-1", DoseCode: "dose-1", Sequence: 1, TriggerType: "manual_campaign",
	}}
}

func (p *generationProtoFake) ListEffectiveVaccinationVersionsForGoat(_ context.Context, _ string, parkID string, asOf time.Time) ([]string, error) {
	p.effectiveAsOf = append(p.effectiveAsOf, asOf)
	p.effectiveParkID = append(p.effectiveParkID, parkID)
	if len(p.effectiveVersions) > 0 {
		return p.effectiveVersions, nil
	}
	return []string{"version-1"}, nil
}

func (p *generationProtoFake) GetVersionsByIDs(_ context.Context, _ string, versionIDs []string) (map[string]protodomain.Version, error) {
	p.batchGetVersionCalls++
	out := make(map[string]protodomain.Version, len(versionIDs))
	for _, versionID := range versionIDs {
		v := p.versionFor(versionID)
		out[v.ProtocolVersionID] = v
	}
	return out, nil
}

func (p *generationProtoFake) ListRulesForVersions(_ context.Context, _ string, versionIDs []string) (map[string][]protodomain.Rule, error) {
	p.batchListRulesCalls++
	out := make(map[string][]protodomain.Rule, len(versionIDs))
	for _, versionID := range versionIDs {
		out[versionID] = p.rulesForVersion()
	}
	return out, nil
}

func (p *generationProtoFake) ListEffectiveVaccinationVersionsForParks(_ context.Context, _ string, parkIDs []string, asOf time.Time) (map[string][]string, error) {
	p.batchEffectiveCalls++
	out := make(map[string][]string, len(parkIDs))
	for _, parkID := range parkIDs {
		p.effectiveAsOf = append(p.effectiveAsOf, asOf)
		p.effectiveParkID = append(p.effectiveParkID, parkID)
		if len(p.effectiveVersions) > 0 {
			out[parkID] = append([]string(nil), p.effectiveVersions...)
			continue
		}
		out[parkID] = []string{"version-1"}
	}
	return out, nil
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
	if trusted := g.trustedByDue[dueAt.UTC().Format(time.RFC3339Nano)]; trusted {
		return true, nil
	}
	dueDay := businessDayStart(dueAt)
	for key, trusted := range g.trustedByDue {
		if !trusted {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, key)
		if err == nil && businessDayStart(parsed).Equal(dueDay) {
			return true, nil
		}
	}
	return false, nil
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
	nearestBatchLookups    []nearestBatchLookup
	failOnceAfterInserted  int
	failErr                error
	failed                 bool
	rescheduledDue         map[string]time.Time // R2-01: simulate a persisted reschedule per idempotency key
}

type nearestBatchLookup struct {
	ruleID      string
	vaccineCode string
	shedID      string
	parkID      string
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
		if o.failErr != nil {
			return "", false, o.failErr
		}
		return "", false, errors.New("forced partial failure")
	}
	o.seen[in.IdempotencyKey] = true
	o.keyIndex[in.IdempotencyKey] = len(o.inserted)
	o.inserted = append(o.inserted, in)
	return "obligation-1", true, nil
}

func (o *generationObligationFake) GetByIdempotencyKey(_ context.Context, _, idempotencyKey string) (obldomain.ObligationRef, error) {
	idx, ok := o.keyIndex[idempotencyKey]
	if !ok {
		return obldomain.ObligationRef{}, ports.ErrNotFound
	}
	in := o.inserted[idx]
	// rescheduledDue lets a test simulate a persisted reschedule (R2-01): the persisted DueAt then
	// differs from what generation recomputes.
	due := in.DueAt
	if o.rescheduledDue != nil {
		if d, ok := o.rescheduledDue[idempotencyKey]; ok {
			due = d
		}
	}
	return obldomain.ObligationRef{ObligationID: "obligation-1", Status: in.Status, DueAt: due}, nil
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

func (o *generationObligationFake) FindNearestPlannedBatchDate(_ context.Context, _, _, ruleID, vaccineCode, shedID, parkID string, _, _ time.Time) (*time.Time, error) {
	o.nearestBatchLookups = append(o.nearestBatchLookups, nearestBatchLookup{
		ruleID:      ruleID,
		vaccineCode: vaccineCode,
		shedID:      shedID,
		parkID:      parkID,
	})
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
			run.FailedGoats = 0
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
		run.FailedGoats = result.FailedGoats
		run.SkippedNoDueDate = result.SkippedNoDueDate
		run.SuppressedByTrustedHistory = result.SuppressedByTrustedHistory
		run.LastError = lastError
		r.byKey[key] = run
	}
	return nil
}

// TestHasSameDoseAdministration_CrossProtocol_EqualDoseCode verifies VACC-REV-06:
// cross-protocol vaccines with the same dose code must NOT suppress work.
func TestHasSameDoseAdministration_CrossProtocol_EqualDoseCode(t *testing.T) {
	rule := protodomain.Rule{
		RuleID:            "rule-id-1",
		ProtocolID:        "protocol-v1",
		ProtocolVersionID: "version-v1",
		DoseCode:          "dose1",
		Sequence:          1,
		TriggerType:       "birth_age",
	}
	vaccine := vaccineProfile{Code: "FMD"}

	// Administration from a DIFFERENT protocol with the same dose code should NOT suppress.
	history := []domain.RecentVaccineAdministration{
		{
			VaccineCode:       "FMD",
			DoseCode:          "dose1",
			Sequence:          1,
			ProtocolID:        "protocol-v2", // Different protocol
			ProtocolVersionID: "version-v2",
			AdministeredAt:    time.Now().Add(-7 * 24 * time.Hour),
		},
	}

	result := hasSameDoseAdministration(rule, vaccine, history)
	if result {
		t.Errorf("VACC-REV-06 bug: cross-protocol dose with same code suppressed work; expected false, got true")
	}
}

// TestHasSameDoseAdministration_CrossProtocol_EqualSequence verifies VACC-REV-06:
// cross-protocol vaccines with the same sequence must NOT suppress work.
func TestHasSameDoseAdministration_CrossProtocol_EqualSequence(t *testing.T) {
	rule := protodomain.Rule{
		RuleID:            "rule-id-1",
		ProtocolID:        "protocol-v1",
		ProtocolVersionID: "version-v1",
		DoseCode:          "", // No dose code
		Sequence:          1,
		TriggerType:       "birth_age",
	}
	vaccine := vaccineProfile{Code: "FMD"}

	// Administration from a DIFFERENT protocol with the same sequence should NOT suppress.
	history := []domain.RecentVaccineAdministration{
		{
			VaccineCode:       "FMD",
			DoseCode:          "",
			Sequence:          1,             // Same sequence
			ProtocolID:        "protocol-v2", // Different protocol
			ProtocolVersionID: "version-v2",
			AdministeredAt:    time.Now().Add(-7 * 24 * time.Hour),
		},
	}

	result := hasSameDoseAdministration(rule, vaccine, history)
	if result {
		t.Errorf("VACC-REV-06 bug: cross-protocol dose with same sequence suppressed work; expected false, got true")
	}
}

// TestHasSameDoseAdministration_SameProtocol_EqualDoseCode verifies the normal case:
// same-protocol vaccines with the same dose code SHOULD suppress work.
func TestHasSameDoseAdministration_SameProtocol_EqualDoseCode(t *testing.T) {
	rule := protodomain.Rule{
		RuleID:            "rule-id-1",
		ProtocolID:        "protocol-v1",
		ProtocolVersionID: "version-v1",
		DoseCode:          "dose1",
		Sequence:          1,
		TriggerType:       "birth_age",
	}
	vaccine := vaccineProfile{Code: "FMD"}

	// Administration from the SAME protocol with the same dose code SHOULD suppress.
	history := []domain.RecentVaccineAdministration{
		{
			VaccineCode:       "FMD",
			DoseCode:          "dose1",
			Sequence:          1,
			ProtocolID:        "protocol-v1", // Same protocol
			ProtocolVersionID: "version-v1",
			AdministeredAt:    time.Now().Add(-7 * 24 * time.Hour),
		},
	}

	result := hasSameDoseAdministration(rule, vaccine, history)
	if !result {
		t.Errorf("same-protocol dose code should suppress; expected true, got false")
	}
}

// TestMissingReviewRecorderFailsOnStaleKidStage verifies VACC-REV-10A:
// generation fails loudly if it detects a stale kid stage but has no recorder to persist the review item.
func TestMissingReviewRecorderFailsOnStaleKidStage(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) // Well past 20-week kid cutoff
	asOf := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	goat := domain.EligibleGoat{
		GoatID:          "goat-stale-kid",
		DOB:             &dob,
		LifecycleStatus: "alive",
		Species:         "goat",
		Stage:           "K1", // Stale kid stage
	}

	svc := &GenerationService{
		proto: &generationProtoFake{
			rules:   []protodomain.Rule{},
			ruleDSL: []byte(`{"vaccine":{"code":"test","type":"live","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]}}`),
		},
		goats: &generationGoatFake{list: []domain.EligibleGoat{goat}},
		obl:   &generationObligationFake{},
		// review is nil (missing recorder) - this should cause a failure when stale kid stage is detected
	}

	res := &domain.GenerateResult{}
	err := svc.genOneGoat(ctx, "tenant-1", "version-1", []protodomain.Rule{}, []string{}, genEligibility{}, goat, asOf, generationOptions{}, genVersionPolicies{Procurement: genProcurementPolicy{}}, vaccineProfile{}, []domain.RecentVaccineAdministration{}, newTrustedEvidenceLookup(), res)

	if err == nil {
		t.Errorf("VACC-REV-10A bug: missing review recorder should fail when stale kid stage detected; expected error, got nil")
	}
	if !strings.Contains(err.Error(), "review recorder is absent") {
		t.Errorf("error should mention missing review recorder; got: %v", err)
	}
}

type fakeReviewRecorder struct{}

func (f *fakeReviewRecorder) RecordStageReviewItem(ctx context.Context, tenantID, goatID, reason, observedStage string, observedAgeWeeks int, idempotencyKey string) error {
	return nil
}

// AnchorMissingCatchUpKey must stay byte-identical to the key genOneGoat stamps on the §89 option-4
// catch-up path (missingDueDateKey with anchorMissingReason). Seed reconciliation classifies
// missing-anchor obligations against the exported helper, so any drift between the two would either
// mis-count a legitimate routed catch-up as a defect (false seed failure) or hide a fabricated due.
func TestAnchorMissingCatchUpKeyMatchesGeneratorKey(t *testing.T) {
	tenantID := "11111111-1111-4111-8111-111111111111"
	versionID := "22222222-2222-4222-8222-222222222222"
	cases := []struct {
		triggerType string
		reason      string
	}{
		{"birth_age", "missing_dob"},
		{"post_arrival", "missing_entry_date"},
	}
	for _, tc := range cases {
		rule := protodomain.Rule{RuleID: "rule-abc", Sequence: 3, TriggerType: tc.triggerType}
		goat := domain.EligibleGoat{GoatID: "goat-xyz"}
		want := missingDueDateKey(tenantID, versionID, rule, goat, tc.reason)
		got := AnchorMissingCatchUpKey(tenantID, versionID, rule.RuleID, goat.GoatID, tc.triggerType, rule.Sequence)
		if got != want {
			t.Fatalf("%s: AnchorMissingCatchUpKey=%s, want generator key %s", tc.triggerType, got, want)
		}
	}
}

// gpoxProfile is the reviewed Goat Pox vaccine profile shared by the VAX-REV-02 per-vaccine-anchor
// regressions (formerly the VAX-SEED-01 goat-wide-checkpoint regressions the bug fix replaced).
func gpoxProfile() vaccineProfile {
	return vaccineProfile{Code: "Goat Pox", Type: "live", PathogenClass: "viral"}
}

// VAX-REV-02: Contract §89 anchors PER VACCINE FAMILY, never goat-wide, and there is no separate
// "seed cutover" generation mode any more (GenerateSeedCutoverForAllGoats/seedCutoverCohort were
// removed — the seed importer now calls the exact same GenerateEffectiveForAllGoats path as runtime).
// A family WITH its own accepted history is course continuation: the already-administered dose
// (gpox_kid_w4) must never be regenerated (hasSameDoseAdministration, option 1), but a STILL-REQUIRED
// different dose of that same course (gpox_kid_w6, due now, no matching administration) is not a
// "family blanket" suppression target and must still generate — alongside the family's own
// after_previous_completion recurrence, anchored to its own latest administration.
func TestSameFamilyDifferentDoseGeneratesAlongsideOwnRecurrence(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	dob := asOf.AddDate(0, 0, -42) // ~6 weeks: birth_age (w6) is due now
	gpoxAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	svc := NewGenerationService(&generationProtoFake{}, &generationGoatFake{}, &generationObligationFake{seen: map[string]bool{}})
	goat := domain.EligibleGoat{GoatID: "imported-goat", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1"}
	rules := []protodomain.Rule{
		{RuleID: "rule-gpox-w6", DoseCode: "gpox_kid_w6", Sequence: 1, TriggerType: "birth_age", OffsetDays: 42, DueWindowDays: 7},
		{RuleID: "rule-gpox-revac", DoseCode: "gpox_revac", Sequence: 2, TriggerType: "after_previous_completion", Repeat: "every_n_days", MinGapDays: 270},
	}
	// Goat Pox history exists (gpox_kid_w4 given), but NOT for this specific dose (gpox_kid_w6).
	history := []domain.RecentVaccineAdministration{{AdministeredAt: gpoxAt, VaccineCode: "Goat Pox", VaccineType: "live", PathogenClass: "viral", DoseCode: "gpox_kid_w4", Sequence: 0}}

	res := &domain.GenerateResult{}
	obl := svc.obl.(*generationObligationFake)
	if err := svc.genOneGoat(ctx, "tenant-1", "version-1", rules, nil, genEligibility{}, goat, asOf,
		generationOptions{healthRecoveryAlign: true}, genVersionPolicies{}, gpoxProfile(), history, newTrustedEvidenceLookup(), res); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 2 || len(obl.inserted) != 2 {
		t.Fatalf("result=%#v inserted=%#v, want both the still-required gpox_kid_w6 dose and the recurrence generated", res, obl.inserted)
	}
	var sawPrimary, sawRecurrence bool
	for _, got := range obl.inserted {
		switch got.RuleID {
		case "rule-gpox-w6":
			sawPrimary = true
		case "rule-gpox-revac":
			sawRecurrence = true
			if !got.DueAt.Equal(businessDayStart(gpoxAt).AddDate(0, 0, 270)) {
				t.Fatalf("inserted=%#v, want recurrence anchored to the family's own latest administration", got)
			}
		}
	}
	if !sawPrimary || !sawRecurrence {
		t.Fatalf("inserted=%#v, want both rule-gpox-w6 and rule-gpox-revac", obl.inserted)
	}
}

// VAX-REV-02 guard: a family WITH its own accepted history of the EXACT SAME dose is still
// suppressed (option 1, course continuation) — this must not regress when the goat-wide checkpoint
// suppression is removed.
func TestSameFamilySameDoseStillSuppressed(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	dob := asOf.AddDate(0, 0, -42)
	gpoxAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	svc := NewGenerationService(&generationProtoFake{}, &generationGoatFake{}, &generationObligationFake{seen: map[string]bool{}})
	goat := domain.EligibleGoat{GoatID: "same-dose-goat", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1"}
	rules := []protodomain.Rule{
		{RuleID: "rule-gpox-w6", DoseCode: "gpox_kid_w6", Sequence: 1, TriggerType: "birth_age", OffsetDays: 42, DueWindowDays: 7},
	}
	// The SAME dose (gpox_kid_w6) has already been administered.
	history := []domain.RecentVaccineAdministration{{AdministeredAt: gpoxAt, VaccineCode: "Goat Pox", VaccineType: "live", PathogenClass: "viral", DoseCode: "gpox_kid_w6", Sequence: 1}}

	res := &domain.GenerateResult{}
	obl := svc.obl.(*generationObligationFake)
	if err := svc.genOneGoat(ctx, "tenant-1", "version-1", rules, nil, genEligibility{}, goat, asOf,
		generationOptions{healthRecoveryAlign: true}, genVersionPolicies{}, gpoxProfile(), history, newTrustedEvidenceLookup(), res); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 0 || len(obl.inserted) != 0 {
		t.Fatalf("result=%#v inserted=%#v, want the already-administered dose suppressed", res, obl.inserted)
	}
	if res.SuppressedByTrustedHistory < 1 {
		t.Fatalf("result=%#v, want the suppression counted", res)
	}
}

// VAX-REV-02 runtime rule: a zero-history goat still receives a safe historical catch-up (nothing
// proves it was already enrolled).
func TestRuntimeZeroHistoryGoatStillReceivesCatchUp(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	dob := asOf.AddDate(0, 0, -120) // ~17 weeks: kid dose window has already elapsed → catch-up
	svc := NewGenerationService(&generationProtoFake{}, &generationGoatFake{}, &generationObligationFake{seen: map[string]bool{}})
	goat := domain.EligibleGoat{GoatID: "fresh-kid", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1"}
	rules := []protodomain.Rule{
		{RuleID: "rule-gpox-w6", DoseCode: "gpox_kid_w6", Sequence: 1, TriggerType: "birth_age", OffsetDays: 42, DueWindowDays: 7, CatchUp: "immediate"},
	}
	res := &domain.GenerateResult{}
	obl := svc.obl.(*generationObligationFake)
	if err := svc.genOneGoat(ctx, "tenant-1", "version-1", rules, nil, genEligibility{}, goat, asOf,
		generationOptions{healthRecoveryAlign: true}, genVersionPolicies{}, gpoxProfile(), nil, newTrustedEvidenceLookup(), res); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want a zero-history goat to still receive safe catch-up", res, obl.inserted)
	}
}

// VAX-REV-02 (was the VAX-SEED-01 bug): an already-enrolled goat (>=1 accepted dose in ANY OTHER
// family) MUST still get a historical catch-up reconstructed for a BLANK family — a dose recorded in
// an unrelated vaccine family (PPR here) is never proof that Goat Pox was administered. Per Contract
// §89 this blank family falls through DOB → entry → adult catch-up exactly like a zero-history goat.
func TestRuntimeEnrolledGoatStillGetsBlankFamilyCatchUp(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	dob := asOf.AddDate(0, 0, -120) // kid dose window elapsed → catch-up
	pprAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	svc := NewGenerationService(&generationProtoFake{}, &generationGoatFake{}, &generationObligationFake{seen: map[string]bool{}})
	goat := domain.EligibleGoat{GoatID: "enrolled-kid", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1"}
	rules := []protodomain.Rule{
		{RuleID: "rule-gpox-w6", DoseCode: "gpox_kid_w6", Sequence: 1, TriggerType: "birth_age", OffsetDays: 42, DueWindowDays: 7, CatchUp: "immediate"},
	}
	// Enrolled via a DIFFERENT family (PPR); Goat Pox is blank for this goat.
	history := []domain.RecentVaccineAdministration{{AdministeredAt: pprAt, VaccineCode: "PPR", VaccineType: "live", PathogenClass: "viral", DoseCode: "ppr_kid", Sequence: 0}}
	res := &domain.GenerateResult{}
	obl := svc.obl.(*generationObligationFake)
	if err := svc.genOneGoat(ctx, "tenant-1", "version-1", rules, nil, genEligibility{}, goat, asOf,
		generationOptions{healthRecoveryAlign: true}, genVersionPolicies{}, gpoxProfile(), history, newTrustedEvidenceLookup(), res); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want the blank Goat Pox family to still receive catch-up for an enrolled goat", res, obl.inserted)
	}
	if got := obl.inserted[0]; got.DueAt.Before(businessDayStart(asOf)) {
		t.Fatalf("inserted=%#v, want the catch-up due date never before the business date", got)
	}
}

// VAX-REV-02 guard (partial history, missing anchor): a goat with accepted PPR history but NO DOB, NO
// entry date, and a blank FMD family must get FMD's adult catch-up routed to the next compatible
// drive — never suppressed by the PPR history, and never a backdated/overdue card (it lands on or
// after the business date, per Contract §89 option 4: "adult catch-up/primary at the next compatible
// drive when neither DOB nor entry is available").
func TestPartialHistoryGoatBlankFamilyGetsFutureAdultCatchUp(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	pprAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	svc := NewGenerationService(&generationProtoFake{}, &generationGoatFake{}, &generationObligationFake{seen: map[string]bool{}})
	// No DOB, no entry date, no kid-management stage: routes adult (schedulePathForGoat B3).
	goat := domain.EligibleGoat{GoatID: "partial-history-goat", LifecycleStatus: "alive", Species: "goat"}
	rules := []protodomain.Rule{
		{RuleID: "rule-fmd-adult", DoseCode: "fmd_adult_primary", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 0, DueWindowDays: 7, CatchUp: "immediate"},
	}
	// PPR history exists (goat is not zero-history), but FMD is blank — a different family.
	history := []domain.RecentVaccineAdministration{{AdministeredAt: pprAt, VaccineCode: "PPR", VaccineType: "live", PathogenClass: "viral", DoseCode: "ppr_adult", Sequence: 0}}
	fmdProfile := vaccineProfile{Code: "FMD", Type: "killed", PathogenClass: "viral"}
	res := &domain.GenerateResult{}
	obl := svc.obl.(*generationObligationFake)
	if err := svc.genOneGoat(ctx, "tenant-1", "version-1", rules, nil, genEligibility{}, goat, asOf,
		generationOptions{healthRecoveryAlign: true}, genVersionPolicies{}, fmdProfile, history, newTrustedEvidenceLookup(), res); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want the blank FMD family to still receive an adult catch-up", res, obl.inserted)
	}
	if got := obl.inserted[0]; got.DueAt.Before(businessDayStart(asOf)) {
		t.Fatalf("inserted=%#v, want the catch-up due date never before the business date (no backdated overdue card)", got)
	}
}

// VAX-REV-03: GenerateSeedCutoverForAllGoats/seedCutoverCohort no longer exist — the seed importer and
// runtime generation both call GenerateEffectiveForAllGoats over the SAME tenant-wide scope, so a
// mixed cohort (a goat that would have been "freshly imported" alongside a pre-existing goat) is
// generated by identical per-vaccine rules with no whole-tenant or partial-cohort cutover marking: a
// blank-family goat with an elapsed catch-up window materializes it, while a goat whose exact dose is
// already accepted history is suppressed — both in the SAME pass, over the SAME scope, via the SAME
// function.
func TestGenerateEffectiveForAllGoatsMixedCohortNoCutoverScope(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	catchUpDOB := asOf.AddDate(0, 0, -120)  // window elapsed → catch-up due
	satisfiedDOB := asOf.AddDate(0, 0, -42) // in-window, but the exact dose is already given
	gpoxAt := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"Goat Pox","type":"live","pathogen_class":"viral"},"eligibility":{}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-gpox-w6", DoseCode: "gpox_kid_w6", Sequence: 1,
			TriggerType: "birth_age", OffsetDays: 42, DueWindowDays: 7, CatchUp: "immediate",
		}},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{
			{GoatID: "blank-family-goat", LifecycleStatus: "alive", DOB: &catchUpDOB, Stage: "K1", Species: "goat"},
			{GoatID: "already-dosed-goat", LifecycleStatus: "alive", DOB: &satisfiedDOB, Stage: "K1", Species: "goat"},
		},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"already-dosed-goat": {{AdministeredAt: gpoxAt, VaccineCode: "Goat Pox", VaccineType: "live", PathogenClass: "viral", DoseCode: "gpox_kid_w6", Sequence: 1}},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateEffectiveForAllGoats(ctx, "tenant-1", asOf)
	if err != nil {
		t.Fatalf("mixed cohort generate: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want only the blank-family goat's catch-up generated", result, obl.inserted)
	}
	if obl.inserted[0].TargetID != "blank-family-goat" {
		t.Fatalf("inserted=%#v, want the blank-family goat's obligation, not the already-dosed goat", obl.inserted)
	}
	if result.SuppressedByTrustedHistory < 1 {
		t.Fatalf("result=%#v, want the already-dosed goat's exact dose suppressed by its own recorded history", result)
	}
}

// BUG #2: test that two co-due pending vaccines (both becoming due on the same day in this pass)
// are spaced against each other by the cross-vaccine compatibility policy, not left same-day.
func TestGenerateForVersionSpacesCoDuePendingVaccines(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	kidDOB := asOf.AddDate(0, 0, -84) // 12 weeks old: eligible for both PPR and Goat Pox at 12w

	proto := &generationProtoFake{
		// Two DIFFERENT live-viral vaccines (PPR and Goat Pox), each with its OWN vaccine code set via
		// per-rule metadata, both due at 12 weeks. live-viral→live-viral is not a same-day-allowed pair,
		// so they must be spaced LiveToLiveGapDays (28 days) apart when generated in the same pass.
		// Distinct per-rule codes matter: cross-vaccine spacing applies only between different products,
		// never between two doses of one course (which would share a code). No prior history for either.
		ruleDSL: []byte(`{
			"eligibility":{},
			"compatibility_policy":{"live_to_live_gap_days":28}
		}`),
		rules: []protodomain.Rule{
			{
				RuleID: "ppr-12w", DoseCode: "ppr_kid_12w", Sequence: 1,
				TriggerType: "birth_age", OffsetDays: 84, DueWindowDays: 7,
				EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`),
			},
			{
				RuleID: "gpox-12w", DoseCode: "gpox_kid_12w", Sequence: 2,
				TriggerType: "birth_age", OffsetDays: 84, DueWindowDays: 7,
				EligibilityJSON: []byte(`{"vaccine":{"code":"Goat Pox","type":"live","pathogen_class":"viral"}}`),
			},
		},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{
			{GoatID: "kid-goat", LifecycleStatus: "alive", DOB: &kidDOB, Stage: "K1", Species: "goat"},
		},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"kid-goat": {}, // no prior history
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 2 || len(obl.inserted) != 2 {
		t.Fatalf("result=%#v inserted=%#v, want both PPR and Goat Pox generated", result, obl.inserted)
	}

	// Both should be due on the same business day (12 weeks from DOB).
	expectedDue := businessDayStart(kidDOB.AddDate(0, 0, 84))

	// Find PPR and Goat Pox obligations.
	var pprObl, gpoxObl *obldomain.NewObligation
	for i := range obl.inserted {
		if obl.inserted[i].RuleID == "ppr-12w" {
			pprObl = &obl.inserted[i]
		}
		if obl.inserted[i].RuleID == "gpox-12w" {
			gpoxObl = &obl.inserted[i]
		}
	}

	if pprObl == nil || gpoxObl == nil {
		t.Fatalf("inserted=%#v, want both PPR and Goat Pox obligations", obl.inserted)
	}

	// Both base due is the same (12 weeks from DOB).
	if !pprObl.DueAt.Equal(expectedDue) {
		t.Errorf("PPR due=%s, want %s", pprObl.DueAt.Format(time.RFC3339), expectedDue.Format(time.RFC3339))
	}

	// Goat Pox should be floored out by LiveToLiveGapDays (28 days) after PPR.
	// Since PPR has sequence 1 (processed first) and Goat Pox has sequence 2 (processed second),
	// Goat Pox should be floored to PPR's due + 28 days.
	expectedGpoxDue := businessDayStart(expectedDue).AddDate(0, 0, 28)
	if !gpoxObl.DueAt.Equal(expectedGpoxDue) {
		t.Errorf("Goat Pox due=%s, want %s (should be floored 28 days after PPR)",
			gpoxObl.DueAt.Format(time.RFC3339), expectedGpoxDue.Format(time.RFC3339))
	}
}

// R2-01: Verify that pending spacing is preserved on idempotent replay.
// When generation inserts vaccine A successfully but then fails before inserting vaccine B,
// the retry must still floor B against A's already-committed due date, not treat A as if
// it never existed just because A returns applied=false on replay.
func TestGenerateForVersionPendingSpacingPreservedOnIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	kidDOB := asOf.AddDate(0, 0, -84) // 12 weeks old: eligible for both PPR and Goat Pox at 12w

	proto := &generationProtoFake{
		// Two DIFFERENT live-viral vaccines (PPR and Goat Pox), each with its OWN vaccine code,
		// both due at 12 weeks. live-viral→live-viral requires a 28-day gap.
		// This test verifies that on idempotent replay, PPR's already-inserted obligation still
		// floors Goat Pox even though PPR's second insert returns applied=false.
		ruleDSL: []byte(`{
			"eligibility":{},
			"compatibility_policy":{"live_to_live_gap_days":28}
		}`),
		rules: []protodomain.Rule{
			{
				RuleID: "ppr-12w", DoseCode: "ppr_kid_12w", Sequence: 1,
				TriggerType: "birth_age", OffsetDays: 84, DueWindowDays: 7,
				EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`),
			},
			{
				RuleID: "gpox-12w", DoseCode: "gpox_kid_12w", Sequence: 2,
				TriggerType: "birth_age", OffsetDays: 84, DueWindowDays: 7,
				EligibilityJSON: []byte(`{"vaccine":{"code":"Goat Pox","type":"live","pathogen_class":"viral"}}`),
			},
		},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{
			{GoatID: "kid-goat", LifecycleStatus: "alive", DOB: &kidDOB, Stage: "K1", Species: "goat"},
		},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"kid-goat": {}, // no prior history
		},
	}

	// FIRST ATTEMPT: fail after the first obligation is inserted (simulating partial failure).
	obl := &generationObligationFake{seen: map[string]bool{}, failOnceAfterInserted: 1}
	gen := NewGenerationService(proto, goats, obl)

	_, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err == nil {
		t.Fatalf("first generate: expected error after partial insert, got nil")
	}
	if len(obl.inserted) != 1 {
		t.Fatalf("first attempt inserted=%d, want 1 (PPR only before failure)", len(obl.inserted))
	}

	// SECOND ATTEMPT: retry generation without clearing the obligation cache.
	// The fake will see PPR's idempotency key again and return applied=false.
	// Goat Pox should STILL be floored 28 days after PPR's already-inserted due date,
	// NOT treated as if PPR never existed.
	obl.failOnceAfterInserted = 0 // don't fail again
	obl.failed = false              // reset failure flag
	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}

	// Both insertions should succeed on the second attempt (one was already-seen/applied=false, one new).
	if len(obl.inserted) != 2 {
		t.Fatalf("second attempt inserted=%d, want 2 (PPR replay + Goat Pox new)", len(obl.inserted))
	}

	// result.Generated should count only the newly-inserted one (Goat Pox).
	if result.Generated != 1 {
		t.Fatalf("result.Generated=%d, want 1 (only Goat Pox is newly generated)", result.Generated)
	}

	// Both should be due on the same business day (12 weeks from DOB).
	expectedDue := businessDayStart(kidDOB.AddDate(0, 0, 84))

	// Find PPR and Goat Pox obligations from the final set.
	var pprObl, gpoxObl *obldomain.NewObligation
	for i := range obl.inserted {
		if obl.inserted[i].RuleID == "ppr-12w" {
			pprObl = &obl.inserted[i]
		}
		if obl.inserted[i].RuleID == "gpox-12w" {
			gpoxObl = &obl.inserted[i]
		}
	}

	if pprObl == nil || gpoxObl == nil {
		t.Fatalf("inserted=%#v, want both PPR and Goat Pox obligations", obl.inserted)
	}

	if !pprObl.DueAt.Equal(expectedDue) {
		t.Errorf("PPR due=%s, want %s", pprObl.DueAt.Format(time.RFC3339), expectedDue.Format(time.RFC3339))
	}

	// Goat Pox should be floored 28 days after PPR, even though PPR was a replay (applied=false).
	// This is the key bug fix: pending spacing must account for already-persisted obligations.
	expectedGpoxDue := businessDayStart(expectedDue).AddDate(0, 0, 28)
	if !gpoxObl.DueAt.Equal(expectedGpoxDue) {
		t.Errorf("Goat Pox due=%s, want %s (should be floored 28 days after PPR on replay)",
			gpoxObl.DueAt.Format(time.RFC3339), expectedGpoxDue.Format(time.RFC3339))
	}
}

// R2-03: Verify that AdultPriorVaccinationAllowed flag controls whether adult prior vaccination
// history is used for revac (after_previous_completion) scheduling.
// When false: adult prior vaccination history must NOT be used to schedule revac doses
// When true: adult prior vaccination history MAY be used for revac scheduling
func TestGenerateForVersionAdultPriorVaccinationAllowedFlag(t *testing.T) {
	ctx := context.Background()
	entryDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) // 30 days after entry
	// Adult animal (no DOB), post_arrival trigger
	adult := domain.EligibleGoat{
		GoatID:         "adult-goat",
		LifecycleStatus: "alive",
		EntryDate:      &entryDate,
		Species:        "goat",
	}

	// This adult already has a PPR vaccination history from before entry.
	priorPPRAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	vaccineHistory := []domain.RecentVaccineAdministration{{
		AdministeredAt: priorPPRAt,
		VaccineCode:    "PPR",
		VaccineType:    "live",
		PathogenClass:  "viral",
	}}

	// Revac rule: after_previous_completion, 3-year gap (PPR revacs every 3 years)
	// Set warmup_no_vaccination_days to make the procurement policy active so the flag takes effect.
	proto := &generationProtoFake{
		ruleDSL: []byte(`{
			"eligibility":{},
			"compatibility_policy":{"live_to_live_gap_days":28},
			"procurement_policy":{"warmup_no_vaccination_days":7,"adult_prior_vaccination_allowed":true}
		}`),
		rules: []protodomain.Rule{{
			RuleID: "ppr-revac", DoseCode: "ppr_revac_yearly", Sequence: 1,
			TriggerType: "after_previous_completion", Repeat: "yearly",
			EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`),
		}},
	}

	// TEST 1: When AdultPriorVaccinationAllowed=true, revac is scheduled based on prior history.
	{
		goats := &generationGoatFake{
			list: []domain.EligibleGoat{adult},
			vaccineHistory: map[string][]domain.RecentVaccineAdministration{
				"adult-goat": vaccineHistory,
			},
		}
		obl := &generationObligationFake{}
		gen := NewGenerationService(proto, goats, obl)

		result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate with flag=true: %v", err)
		}
		if result.Generated != 1 {
			t.Errorf("with AdultPriorVaccinationAllowed=true: Generated=%d, want 1 (revac should be scheduled from history)",
				result.Generated)
		}
		if len(obl.inserted) != 1 {
			t.Errorf("with flag=true: inserted=%d obligations, want 1", len(obl.inserted))
		}
		// Revac should be due 1 year after prior PPR (using after_previous_completion)
		expectedRevacDue := businessDayStart(priorPPRAt).AddDate(1, 0, 0) // 1 year later
		if len(obl.inserted) > 0 && !obl.inserted[0].DueAt.Equal(expectedRevacDue) {
			t.Errorf("revac due with flag=true: %s, want %s (1 year after prior PPR)",
				obl.inserted[0].DueAt.Format(time.RFC3339), expectedRevacDue.Format(time.RFC3339))
		}
	}

	// TEST 2: When AdultPriorVaccinationAllowed=false in an active procurement policy, revac is NOT scheduled.
	// We need to set warmup_no_vaccination_days to make the policy active (it checks active() to gate the flag).
	proto.ruleDSL = []byte(`{
		"eligibility":{},
		"compatibility_policy":{"live_to_live_gap_days":28},
		"procurement_policy":{"warmup_no_vaccination_days":7,"adult_prior_vaccination_allowed":false}
	}`)

	{
		goats := &generationGoatFake{
			list: []domain.EligibleGoat{adult},
			vaccineHistory: map[string][]domain.RecentVaccineAdministration{
				"adult-goat": vaccineHistory,
			},
		}
		obl := &generationObligationFake{}
		gen := NewGenerationService(proto, goats, obl)

		result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
		if err != nil {
			t.Fatalf("generate with flag=false: %v", err)
		}
		if result.Generated != 0 {
			t.Errorf("with AdultPriorVaccinationAllowed=false: Generated=%d, want 0 (revac should NOT be scheduled when flag=false)",
				result.Generated)
		}
		if len(obl.inserted) != 0 {
			t.Errorf("with flag=false: inserted=%d obligations, want 0 (revac scheduling disabled by flag)", len(obl.inserted))
		}
	}
}

// TestGenerateForVersionAdultPriorFalseWithoutWarmup is the R2-03 guard: an EXPLICIT
// adult_prior_vaccination_allowed:false must be honored even when it is the ONLY configured
// procurement field (no warmup, no kid cutoff). The prior bug made active() require a positive field,
// so a policy of just {"adult_prior_vaccination_allowed":false} read as inactive and the false was
// silently ignored -- adult prior history then still scheduled the revac. This fixture removes the
// unrelated warmup the sibling test used to mask the defect.
func TestGenerateForVersionAdultPriorFalseWithoutWarmup(t *testing.T) {
	ctx := context.Background()
	entryDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	adult := domain.EligibleGoat{GoatID: "adult-goat", LifecycleStatus: "alive", EntryDate: &entryDate, Species: "goat"}
	priorPPRAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	vaccineHistory := []domain.RecentVaccineAdministration{{
		AdministeredAt: priorPPRAt, VaccineCode: "PPR", VaccineType: "live", PathogenClass: "viral",
	}}
	proto := &generationProtoFake{
		// ONLY the adult-prior flag, explicitly false -- no warmup, no kid cutoff.
		ruleDSL: []byte(`{
			"eligibility":{},
			"compatibility_policy":{"live_to_live_gap_days":28},
			"procurement_policy":{"adult_prior_vaccination_allowed":false}
		}`),
		rules: []protodomain.Rule{{
			RuleID: "ppr-revac", DoseCode: "ppr_revac_yearly", Sequence: 1,
			TriggerType: "after_previous_completion", Repeat: "yearly",
			EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`),
		}},
	}
	goats := &generationGoatFake{
		list:           []domain.EligibleGoat{adult},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{"adult-goat": vaccineHistory},
	}
	obl := &generationObligationFake{}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 0 || len(obl.inserted) != 0 {
		t.Fatalf("explicit adult_prior_vaccination_allowed:false (no warmup) must NOT schedule adult revac from prior history: Generated=%d inserted=%d, want 0/0", result.Generated, len(obl.inserted))
	}
}

// TestGenerateReplayCoDueSpacingUsesPersistedRescheduledDate is the R2-01 guard: when vaccine A is
// generated, later rescheduled, and generation reruns, a co-due incompatible vaccine B (generated
// fresh in the same pass while A replays) must be spaced from A's PERSISTED (rescheduled) date, not
// the freshly recalculated one. Insert A -> reschedule it -> add B and regenerate -> B's cross-vaccine
// gap floor must anchor on A's persisted date.
func TestGenerateReplayCoDueSpacingUsesPersistedRescheduledDate(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	ruleA := protodomain.Rule{
		RuleID: "ppr", DoseCode: "ppr_primary", Sequence: 1, TriggerType: "birth_age", OffsetDays: 30, DueWindowDays: 7,
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`),
	}
	ruleB := protodomain.Rule{
		RuleID: "goatpox", DoseCode: "goatpox_primary", Sequence: 1, TriggerType: "birth_age", OffsetDays: 30, DueWindowDays: 7,
		EligibilityJSON: []byte(`{"vaccine":{"code":"GOAT_POX","type":"live","pathogen_class":"viral"}}`),
	}
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{},"compatibility_policy":{"live_to_live_gap_days":28}}`),
		rules:   []protodomain.Rule{ruleA},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{{GoatID: "goat-1", LifecycleStatus: "alive", DOB: &dob, Species: "goat"}}}
	obl := &generationObligationFake{}
	gen := NewGenerationService(proto, goats, obl)

	// Pass 1: only PPR exists -> A generated at DOB+30 = July 31.
	if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	if len(obl.inserted) != 1 || obl.inserted[0].RuleID != "ppr" {
		t.Fatalf("pass 1 inserted = %#v, want exactly PPR", obl.inserted)
	}

	// A is rescheduled to a LATER persisted date (e.g. an operator moved the drive).
	rescheduled := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	obl.inserted[0].DueAt = rescheduled

	// Pass 2: PPR now replays (applied=false) and Goat Pox is generated fresh, co-due + incompatible.
	proto.rules = []protodomain.Rule{ruleA, ruleB}
	if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("pass 2: %v", err)
	}

	var goatPox *obldomain.NewObligation
	for i := range obl.inserted {
		if obl.inserted[i].RuleID == "goatpox" {
			goatPox = &obl.inserted[i]
		}
	}
	if goatPox == nil {
		t.Fatalf("Goat Pox obligation was not generated in pass 2: inserted=%#v", obl.inserted)
	}
	// B must be floored to A's PERSISTED date + the 28-day live-live gap, not the recalculated July 31.
	wantFloor := businessDayStart(rescheduled).AddDate(0, 0, 28)
	staleFloor := businessDayStart(businessDayStart(dob).AddDate(0, 0, 30)).AddDate(0, 0, 28)
	if goatPox.DueAt.Equal(staleFloor) {
		t.Fatalf("Goat Pox spaced from the OBSOLETE calculated date %s (R2-01 regression)", staleFloor.Format("2006-01-02"))
	}
	if !goatPox.DueAt.Equal(wantFloor) {
		t.Fatalf("Goat Pox due = %s, want %s (A's persisted %s + 28d)", goatPox.DueAt.Format("2006-01-02"), wantFloor.Format("2006-01-02"), rescheduled.Format("2006-01-02"))
	}
}
