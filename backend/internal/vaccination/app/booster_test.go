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
	wantDue := businessDayStart(administered).AddDate(0, 0, 14)
	if !got.DueAt.Equal(wantDue) {
		t.Fatalf("due_at=%s, want %s", got.DueAt, wantDue)
	}
}

func TestScheduleNextDoseSkipsNonPositiveCompletionGap(t *testing.T) {
	ctx := context.Background()
	proto := &boosterRuleReaderFake{rules: []protodomain.Rule{
		{RuleID: "rule-1", DoseCode: "dose-a", Sequence: 1, TriggerType: "birth_age"},
		{RuleID: "rule-2", DoseCode: "dose-b", Sequence: 2, TriggerType: "after_previous_completion"},
	}}
	obl := &boosterObligationWriterFake{}
	svc := NewBoosterService(proto, obl)

	scheduled, err := svc.ScheduleNextDose(ctx, ScheduleNextInput{
		TenantID:          "tenant-1",
		ProtocolVersionID: "version-1",
		GoatID:            "goat-1",
		ScopeType:         "shed",
		ScopeID:           "shed-1",
		PrevSequence:      1,
		AdministeredAt:    time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC),
	})

	if err != nil {
		t.Fatalf("schedule next dose: %v", err)
	}
	if scheduled || len(obl.inserted) != 0 {
		t.Fatalf("scheduled=%v inserted=%d, want no obligation for non-positive completion gap", scheduled, len(obl.inserted))
	}
}

func TestScheduleNextDoseUsesNextHigherNonContiguousSequence(t *testing.T) {
	ctx := context.Background()
	proto := &boosterRuleReaderFake{rules: []protodomain.Rule{
		{RuleID: "rule-10", DoseCode: "dose-a", Sequence: 10, TriggerType: "birth_age"},
		{RuleID: "rule-20", DoseCode: "dose-b", Sequence: 20, TriggerType: "after_previous_completion", OffsetDays: 14},
		{RuleID: "rule-30", DoseCode: "dose-c", Sequence: 30, TriggerType: "after_previous_completion", OffsetDays: 21},
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
		PrevSequence:      10,
		AdministeredAt:    administered,
	})

	if err != nil {
		t.Fatalf("schedule next dose: %v", err)
	}
	if !scheduled || len(obl.inserted) != 1 {
		t.Fatalf("scheduled=%v inserted=%d, want one booster", scheduled, len(obl.inserted))
	}
	got := obl.inserted[0]
	if got.RuleID != "rule-20" || got.Sequence != 20 {
		t.Fatalf("inserted rule=%s sequence=%d, want rule-20 sequence 20", got.RuleID, got.Sequence)
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

func TestScheduleNextDoseRepeatsAdultRevaccinationRule(t *testing.T) {
	ctx := context.Background()
	proto := &boosterRuleReaderFake{rules: []protodomain.Rule{
		{RuleID: "rule-et-primary", DoseCode: "et_tt_7w", Sequence: 2, TriggerType: "birth_age"},
		{RuleID: "rule-et-adult", DoseCode: "et_tt_adult_revac_182d", Sequence: 3, TriggerType: "after_previous_completion", OffsetDays: 182, MinGapDays: 182, Repeat: "every_n_days"},
	}}
	obl := &boosterObligationWriterFake{}
	svc := NewBoosterService(proto, obl)
	administered := time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC)

	scheduled, err := svc.ScheduleNextDose(ctx, ScheduleNextInput{
		TenantID:          "tenant-1",
		ProtocolVersionID: "version-1",
		GoatID:            "goat-1",
		ScopeType:         "shed",
		ScopeID:           "shed-1",
		PrevSequence:      3,
		AdministeredAt:    administered,
	})

	if err != nil {
		t.Fatalf("schedule adult repeat: %v", err)
	}
	if !scheduled || len(obl.inserted) != 1 {
		t.Fatalf("scheduled=%v inserted=%d, want one adult repeat obligation", scheduled, len(obl.inserted))
	}
	got := obl.inserted[0]
	wantDue := businessDayStart(administered).AddDate(0, 0, 182)
	if got.RuleID != "rule-et-adult" || got.Sequence != 3 || !got.DueAt.Equal(wantDue) {
		t.Fatalf("inserted=%#v, want same adult rule sequence 3 due %s", got, wantDue)
	}
}

func TestScheduleNextDoseRepeatsYearlyAdultRule(t *testing.T) {
	ctx := context.Background()
	proto := &boosterRuleReaderFake{rules: []protodomain.Rule{
		{RuleID: "rule-goat-pox-adult", DoseCode: "goat_pox_adult_revac_365d", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 365, MinGapDays: 365, Repeat: "yearly"},
	}}
	obl := &boosterObligationWriterFake{}
	svc := NewBoosterService(proto, obl)
	administered := time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC)

	scheduled, err := svc.ScheduleNextDose(ctx, ScheduleNextInput{
		TenantID:          "tenant-1",
		ProtocolVersionID: "version-1",
		GoatID:            "goat-1",
		ScopeType:         "shed",
		ScopeID:           "shed-1",
		PrevSequence:      2,
		AdministeredAt:    administered,
	})

	if err != nil {
		t.Fatalf("schedule yearly adult repeat: %v", err)
	}
	if !scheduled || len(obl.inserted) != 1 {
		t.Fatalf("scheduled=%v inserted=%d, want one yearly repeat obligation", scheduled, len(obl.inserted))
	}
	wantDue := businessDayStart(administered).AddDate(1, 0, 0)
	if got := obl.inserted[0]; got.RuleID != "rule-goat-pox-adult" || !got.DueAt.Equal(wantDue) {
		t.Fatalf("inserted=%#v, want yearly repeat due %s", got, wantDue)
	}
}

func TestScheduleNextDoseChecksAllRecentVaccinesForCrossGap(t *testing.T) {
	ctx := context.Background()
	proto := &boosterRuleReaderFake{
		version: protodomain.Version{
			RuleDsl: []byte(`{
				"eligibility":{"species":"goat","animal_stage":"K3","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]},
				"vaccine":{"code":"vaccination.matrix","type":"matrix","pathogen_class":"mixed"},
				"compatibility_policy":{"live_to_live_gap_days":28,"live_to_killed_gap_days":14,"killed_to_killed_gap_days":14}
			}`),
		},
		rules: []protodomain.Rule{
			{
				RuleID:      "rule-ppr",
				DoseCode:    "ppr",
				Sequence:    1,
				TriggerType: "birth_age",
				EligibilityJSON: []byte(`{
					"eligibility":{"species":"goat","animal_stage":"K3","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]},
					"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}
				}`),
			},
			{
				RuleID:      "rule-goat-pox",
				DoseCode:    "goat_pox",
				Sequence:    2,
				TriggerType: "after_previous_completion",
				OffsetDays:  7,
				EligibilityJSON: []byte(`{
					"eligibility":{"species":"goat","animal_stage":"K3","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]},
					"vaccine":{"code":"Goat Pox","type":"live","pathogen_class":"viral"}
				}`),
			},
		},
	}
	obl := &boosterObligationWriterFake{}
	goats := &boosterGoatReaderFake{goat: vaccdomain.EligibleGoat{
		GoatID:          "goat-1",
		Species:         "goat",
		LifecycleStatus: "alive",
		HealthStatus:    "healthy",
		Sex:             "female",
		Stage:           "K3",
	}, found: true}
	administered := time.Date(2026, time.July, 15, 8, 0, 0, 0, time.UTC)
	history := &boosterCrossVaccineFake{history: map[string][]vaccdomain.RecentVaccineAdministration{
		"goat-1": {
			{
				VaccineCode:    "HS",
				VaccineType:    "killed",
				PathogenClass:  "bacterial",
				AdministeredAt: administered.AddDate(0, 0, -1),
			},
			{
				VaccineCode:    "PPR",
				VaccineType:    "live",
				PathogenClass:  "viral",
				AdministeredAt: administered.AddDate(0, 0, -7),
			},
		},
	}}
	svc := NewBoosterService(proto, obl).WithGoatReader(goats).WithCrossVaccineGapReader(history)

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
		t.Fatalf("scheduled=%v inserted=%d, want one next live vaccine obligation", scheduled, len(obl.inserted))
	}
	wantDue := businessDayStart(administered).AddDate(0, 0, 21)
	if got := obl.inserted[0].DueAt; !got.Equal(wantDue) {
		t.Fatalf("due_at=%s, want live-live floor %s", got, wantDue)
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

type boosterCrossVaccineFake struct {
	history map[string][]vaccdomain.RecentVaccineAdministration
}

func (f *boosterCrossVaccineFake) LastRecentVaccineAdministrationsForGoats(_ context.Context, _ string, goatIDs []string, _ time.Time) (map[string]vaccdomain.RecentVaccineAdministration, error) {
	out := make(map[string]vaccdomain.RecentVaccineAdministration, len(goatIDs))
	for _, goatID := range goatIDs {
		if admins := f.history[goatID]; len(admins) > 0 {
			out[goatID] = admins[0]
		}
	}
	return out, nil
}

func (f *boosterCrossVaccineFake) RecentVaccineAdministrationsForGoats(_ context.Context, _ string, goatIDs []string, _ time.Time) (map[string][]vaccdomain.RecentVaccineAdministration, error) {
	out := make(map[string][]vaccdomain.RecentVaccineAdministration, len(goatIDs))
	for _, goatID := range goatIDs {
		if admins := f.history[goatID]; len(admins) > 0 {
			out[goatID] = admins
		}
	}
	return out, nil
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

func (f *boosterObligationWriterFake) ReopenDeferredObligationByIdempotencyKey(context.Context, string, string, time.Time, *obldomain.RecoveryReschedule) (string, bool, error) {
	return "", false, nil
}

func (f *boosterObligationWriterFake) FindNearestPlannedBatchDate(context.Context, string, string, string, string, string, string, time.Time, time.Time) (*time.Time, error) {
	return nil, nil
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
