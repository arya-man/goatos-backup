package app

import (
	"context"
	"testing"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

func TestScheduleNextDoseUsesNextHigherSequence(t *testing.T) {
	ctx := context.Background()
	proto := &boosterRuleReaderFake{rules: []protodomain.Rule{
		{RuleID: "rule-1", DoseCode: "dose-a", Sequence: 1, TriggerType: "birth_age"},
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
	if got.RuleID != "rule-3" || got.Sequence != 3 {
		t.Fatalf("inserted rule=%s sequence=%d, want rule-3 sequence 3", got.RuleID, got.Sequence)
	}
	if !got.DueAt.Equal(administered.AddDate(0, 0, 21)) {
		t.Fatalf("due_at=%s, want %s", got.DueAt, administered.AddDate(0, 0, 21))
	}
}

func TestScheduleNextDoseSkipsCalendarRuleBeforeNextCompletionBooster(t *testing.T) {
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
	if !scheduled || len(obl.inserted) != 1 {
		t.Fatalf("scheduled=%v inserted=%d, want one booster", scheduled, len(obl.inserted))
	}
	got := obl.inserted[0]
	if got.RuleID != "rule-3" || got.Sequence != 3 {
		t.Fatalf("inserted rule=%s sequence=%d, want rule-3 sequence 3", got.RuleID, got.Sequence)
	}
}

type boosterRuleReaderFake struct {
	rules []protodomain.Rule
}

func (f *boosterRuleReaderFake) ListRules(context.Context, string, string) ([]protodomain.Rule, error) {
	return f.rules, nil
}

type boosterObligationWriterFake struct {
	inserted []obldomain.NewObligation
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

func (f *boosterObligationWriterFake) RecordStatusEvent(context.Context, obldomain.NewStatusEvent) (string, bool, error) {
	return "", false, nil
}
