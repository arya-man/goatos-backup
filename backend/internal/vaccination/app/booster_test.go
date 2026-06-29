package app

import (
	"context"
	"testing"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestScheduleNextDoseUsesImmediateNextSequence(t *testing.T) {
	ctx := context.Background()
	proto := &boosterRuleReaderFake{rules: []protodomain.Rule{
		{RuleID: "rule-1", DoseCode: "dose-a", Sequence: 1, TriggerType: "birth_age"},
		{RuleID: "rule-2", DoseCode: "dose-b", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 14},
		{RuleID: "rule-3", DoseCode: "dose-c", Sequence: 3, TriggerType: "after_previous_completion", OffsetDays: 21},
	}}
	obl := &boosterObligationWriterFake{}
	svc := NewBoosterService(proto, obl)
	administered := time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC)

	scheduled, err := svc.ScheduleNextDose(ctx, ScheduleNextInput{
		TenantID:          "tenant-1",
		ProtocolVersionID: "version-1",
		GoatID:            "goat-1",
		ScopeType:         "shed",
		ScopeID:           "shed-1",
		PrevSequence:      1,
		AdministeredAt:    administered,
	})

	if err != nil {
		t.Fatalf("schedule next dose: %v", err)
	}
	if !scheduled || len(obl.inserted) != 1 {
		t.Fatalf("scheduled=%v inserted=%d, want one booster", scheduled, len(obl.inserted))
	}
	got := obl.inserted[0]
	if got.RuleID != "rule-2" || got.Sequence != 2 {
		t.Fatalf("inserted rule=%s sequence=%d, want rule-2 sequence 2", got.RuleID, got.Sequence)
	}
	if !got.DueAt.Equal(administered.AddDate(0, 0, 14)) {
		t.Fatalf("due_at=%s, want %s", got.DueAt, administered.AddDate(0, 0, 14))
	}
}

func TestScheduleNextDoseDoesNotSkipCalendarRuleBeforeNextCompletionBooster(t *testing.T) {
	ctx := context.Background()
	proto := &boosterRuleReaderFake{rules: []protodomain.Rule{
		{RuleID: "rule-1", DoseCode: "dose-a", Sequence: 1, TriggerType: "birth_age"},
		{RuleID: "rule-2", DoseCode: "dose-b", Sequence: 2, TriggerType: "calendar", OffsetDays: 7},
		{RuleID: "rule-3", DoseCode: "dose-c", Sequence: 3, TriggerType: "after_previous_completion", OffsetDays: 21},
	}}
	obl := &boosterObligationWriterFake{}
	svc := NewBoosterService(proto, obl)
	administered := time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC)

	scheduled, err := svc.ScheduleNextDose(ctx, ScheduleNextInput{
		TenantID:          "tenant-1",
		ProtocolVersionID: "version-1",
		GoatID:            "goat-1",
		ScopeType:         "shed",
		ScopeID:           "shed-1",
		PrevSequence:      1,
		AdministeredAt:    administered,
	})

	if err != nil {
		t.Fatalf("schedule next dose: %v", err)
	}
	if scheduled || len(obl.inserted) != 0 {
		t.Fatalf("scheduled=%v inserted=%d, want no booster because sequence 2 is SM-1 calendar work", scheduled, len(obl.inserted))
	}
}

func TestScheduleNextDoseDefersWhenCurrentGoatIsInDeferState(t *testing.T) {
	ctx := context.Background()
	proto := &boosterRuleReaderFake{
		version: protodomain.Version{
			RuleDsl: []byte(`{"eligibility":{"defer_states":["icu","quarantine"]}}`),
		},
		rules: []protodomain.Rule{
			{RuleID: "rule-1", DoseCode: "dose-a", Sequence: 1, TriggerType: "birth_age"},
			{RuleID: "rule-2", DoseCode: "dose-b", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 14},
		},
	}
	obl := &boosterObligationWriterFake{}
	goats := &boosterGoatReaderFake{goat: vaccdomain.EligibleGoat{
		GoatID:          "goat-1",
		LifecycleStatus: "alive",
		HealthStatus:    "healthy",
		LocationIsICU:   true,
	}, found: true}
	svc := NewBoosterService(proto, obl).WithGoatReader(goats)
	administered := time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC)

	scheduled, err := svc.ScheduleNextDose(ctx, ScheduleNextInput{
		TenantID:          "tenant-1",
		ProtocolVersionID: "version-1",
		GoatID:            "goat-1",
		ScopeType:         "shed",
		ScopeID:           "shed-1",
		PrevSequence:      1,
		AdministeredAt:    administered,
	})

	if err != nil {
		t.Fatalf("schedule next dose: %v", err)
	}
	if !scheduled || len(obl.inserted) != 1 {
		t.Fatalf("scheduled=%v inserted=%d, want deferred booster obligation", scheduled, len(obl.inserted))
	}
	if got := obl.inserted[0].Status; got != "deferred" {
		t.Fatalf("status=%s, want deferred", got)
	}
	if len(obl.events) != 1 || obl.events[0].EventType != "deferred" {
		t.Fatalf("events=%#v, want one deferred status event", obl.events)
	}
}

type boosterRuleReaderFake struct {
	version protodomain.Version
	rules   []protodomain.Rule
}

func (f *boosterRuleReaderFake) ListRules(context.Context, string, string) ([]protodomain.Rule, error) {
	return f.rules, nil
}

func (f *boosterRuleReaderFake) GetVersion(context.Context, string, string) (protodomain.Version, error) {
	return f.version, nil
}

type boosterGoatReaderFake struct {
	goat  vaccdomain.EligibleGoat
	found bool
}

func (f *boosterGoatReaderFake) GetGoatForGeneration(context.Context, string, string) (vaccdomain.EligibleGoat, bool, error) {
	return f.goat, f.found, nil
}

type boosterObligationWriterFake struct {
	inserted []obldomain.NewObligation
	events   []obldomain.NewStatusEvent
}

func (f *boosterObligationWriterFake) InsertObligation(_ context.Context, in obldomain.NewObligation) (string, bool, error) {
	f.inserted = append(f.inserted, in)
	return "obligation-1", true, nil
}

func (f *boosterObligationWriterFake) DeferOpenObligationByIdempotencyKey(context.Context, string, string, string, time.Time) (string, bool, error) {
	return "", false, nil
}

func (f *boosterObligationWriterFake) ReopenDeferredObligationByIdempotencyKey(context.Context, string, string, time.Time) (string, bool, error) {
	return "", false, nil
}

func (f *boosterObligationWriterFake) CancelOpenObligationByIdempotencyKey(context.Context, string, string, string, time.Time) (string, bool, error) {
	return "", false, nil
}

func (f *boosterObligationWriterFake) CancelOpenVaccinationObligationsForGoatExceptVersions(context.Context, string, string, []string, string, time.Time) (int, error) {
	return 0, nil
}

func (f *boosterObligationWriterFake) CancelOpenVaccinationObligationsForGoatVersion(context.Context, string, string, string, string, time.Time) (int, error) {
	return 0, nil
}

func (f *boosterObligationWriterFake) RecordStatusEvent(_ context.Context, ev obldomain.NewStatusEvent) (string, bool, error) {
	f.events = append(f.events, ev)
	return "event-1", true, nil
}
