package app

import (
	"context"
	"testing"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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

func TestScheduleNextDoseUsesEachOperatorSubmissionDateAfterDelayedVerification(t *testing.T) {
	proto := &boosterRuleReaderFake{rules: []protodomain.Rule{
		{RuleID: "rule-et-adult", DoseCode: "et_tt_adult_revac_182d", Sequence: 3, TriggerType: "after_previous_completion", OffsetDays: 182, MinGapDays: 182, Repeat: "every_n_days"},
	}}
	submissions := []struct {
		name           string
		goats          int
		administeredAt time.Time
	}{
		{name: "gandhi", goats: 114, administeredAt: time.Date(2026, time.July, 24, 15, 0, 0, 0, biztime.DefaultLocation())},
		{name: "godel-and-yashoda", goats: 163, administeredAt: time.Date(2026, time.July, 25, 15, 0, 0, 0, biztime.DefaultLocation())},
		{name: "mandela", goats: 47, administeredAt: time.Date(2026, time.July, 26, 15, 0, 0, 0, biztime.DefaultLocation())},
	}
	if total := submissions[0].goats + submissions[1].goats + submissions[2].goats; total != 324 {
		t.Fatalf("fixture total=%d, want 324", total)
	}

	for _, submission := range submissions {
		t.Run(submission.name, func(t *testing.T) {
			obl := &boosterObligationWriterFake{}
			svc := NewBoosterService(proto, obl)
			scheduled, err := svc.ScheduleNextDose(context.Background(), ScheduleNextInput{
				TenantID:          "tenant-1",
				ProtocolVersionID: "version-1",
				GoatID:            "goat-" + submission.name,
				ScopeType:         "shed",
				ScopeID:           "shed-1",
				PrevSequence:      3,
				AdministeredAt:    submission.administeredAt,
			})
			if err != nil {
				t.Fatalf("schedule repeat: %v", err)
			}
			if !scheduled || len(obl.inserted) != 1 {
				t.Fatalf("scheduled=%v inserted=%d, want one repeat", scheduled, len(obl.inserted))
			}
			wantDue := businessDayStart(submission.administeredAt).AddDate(0, 0, 182)
			if got := obl.inserted[0].DueAt; !got.Equal(wantDue) {
				t.Fatalf("due=%s, want %s from operator submission; delayed verification/closure must not be the anchor", got, wantDue)
			}
		})
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

// matrixVaccineRule builds a rule shaped like a published CPT-matrix row: the
// per-rule eligibility_json carries the row's own vaccine identity, exactly as
// protocol publish materializes it (matrixRuleEligibilityJSON).
func matrixVaccineRule(ruleID, doseCode, vaccineCode, vaccineType, pathogen, trigger string, sequence, offset, minGap int32, repeat string) protodomain.Rule {
	return protodomain.Rule{
		RuleID:      ruleID,
		DoseCode:    doseCode,
		Sequence:    sequence,
		TriggerType: trigger,
		OffsetDays:  offset,
		MinGapDays:  minGap,
		Repeat:      repeat,
		EligibilityJSON: []byte(`{"eligibility":{"species":"goat","animal_stage":"all","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]},` +
			`"vaccine":{"code":"` + vaccineCode + `","type":"` + vaccineType + `","pathogen_class":"` + pathogen + `"}}`),
	}
}

// publishedCPTMatrixRules mirrors the real seeded CPT matrix (cmd/seed-vaccination-real):
// ONE published protocol version carrying all 7 vaccines, whose rule sequences are
// assigned from a single global counter, so every vaccine's revac row is followed by
// the NEXT VACCINE's primary rows instead of being the end of "the series".
func publishedCPTMatrixRules() []protodomain.Rule {
	return []protodomain.Rule{
		matrixVaccineRule("rule-et-tt-adult-w1", "et_tt_adult_w1", "ET+TT", "killed", "bacterial", "post_arrival", 3, 7, 0, "none"),
		matrixVaccineRule("rule-et-tt-adult-w2", "et_tt_adult_w2", "ET+TT", "killed", "bacterial", "post_arrival", 4, 21, 21, "none"),
		matrixVaccineRule("rule-et-tt-revac", "et_tt_revac", "ET+TT", "killed", "bacterial", "after_previous_completion", 5, 182, 182, "every_n_days"),
		matrixVaccineRule("rule-ppr-kid-16w", "ppr_kid_16w", "PPR", "live", "viral", "birth_age", 6, 112, 0, "none"),
		matrixVaccineRule("rule-ppr-adult-w1", "ppr_adult_w1", "PPR", "live", "viral", "post_arrival", 7, 7, 0, "none"),
		matrixVaccineRule("rule-ppr-revac", "ppr_revac", "PPR", "live", "viral", "after_previous_completion", 8, 1095, 1095, "every_n_days"),
		matrixVaccineRule("rule-fmd-kid-12w", "fmd_kid_12w", "FMD", "killed", "viral", "birth_age", 9, 84, 0, "none"),
		matrixVaccineRule("rule-fmd-adult-w1", "fmd_adult_w1", "FMD", "killed", "viral", "post_arrival", 10, 63, 0, "none"),
		matrixVaccineRule("rule-fmd-revac", "fmd_revac", "FMD", "killed", "viral", "after_previous_completion", 11, 274, 274, "every_n_days"),
	}
}

// TestScheduleNextDoseRepeatsRevacInPublishedMultiVaccineMatrix pins the reported
// requirement on the REAL published shape: completing a revac dose must schedule that
// same vaccine's next revac at the rule's own authored interval (ET+TT 182, PPR 1095,
// FMD 274). The repeat series is per VACCINE, not per protocol version — a following
// row that belongs to a DIFFERENT vaccine must not end this vaccine's series.
func TestScheduleNextDoseRepeatsRevacInPublishedMultiVaccineMatrix(t *testing.T) {
	ctx := context.Background()
	administered := time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC)

	cases := []struct {
		name        string
		prevSeq     int32
		wantRuleID  string
		wantSeq     int32
		wantGapDays int
	}{
		{name: "et_tt_revac repeats at 182", prevSeq: 5, wantRuleID: "rule-et-tt-revac", wantSeq: 5, wantGapDays: 182},
		{name: "ppr_revac repeats at 1095", prevSeq: 8, wantRuleID: "rule-ppr-revac", wantSeq: 8, wantGapDays: 1095},
		{name: "fmd_revac repeats at 274", prevSeq: 11, wantRuleID: "rule-fmd-revac", wantSeq: 11, wantGapDays: 274},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proto := &boosterRuleReaderFake{rules: publishedCPTMatrixRules()}
			obl := &boosterObligationWriterFake{}
			svc := NewBoosterService(proto, obl)

			scheduled, err := svc.ScheduleNextDose(ctx, ScheduleNextInput{
				TenantID:          "tenant-1",
				ProtocolVersionID: "version-1",
				GoatID:            "goat-1",
				ScopeType:         "shed",
				ScopeID:           "shed-1",
				PrevSequence:      tc.prevSeq,
				AdministeredAt:    administered,
			})
			if err != nil {
				t.Fatalf("schedule next dose: %v", err)
			}
			if !scheduled || len(obl.inserted) != 1 {
				t.Fatalf("scheduled=%v inserted=%d, want one repeat obligation for %s", scheduled, len(obl.inserted), tc.wantRuleID)
			}
			got := obl.inserted[0]
			wantDue := businessDayStart(administered).AddDate(0, 0, tc.wantGapDays)
			if got.RuleID != tc.wantRuleID || got.Sequence != tc.wantSeq || !got.DueAt.Equal(wantDue) {
				t.Fatalf("inserted rule=%s sequence=%d due=%s, want rule=%s sequence=%d due=%s",
					got.RuleID, got.Sequence, got.DueAt, tc.wantRuleID, tc.wantSeq, wantDue)
			}
		})
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
	if f.goat.ParkID == "" {
		f.goat.ParkID = "park-1"
	}
	if f.goat.ShedID == "" {
		f.goat.ShedID = "shed-1"
	}
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

func (f *boosterObligationWriterFake) InsertDeferredObligation(ctx context.Context, in obldomain.NewObligation, reason string, occurredAt time.Time) (string, bool, error) {
	return f.InsertObligation(ctx, in)
}

func (f *boosterObligationWriterFake) DeferOpenObligationForGeneration(context.Context, string, string, string, time.Time) (obldomain.ObligationRef, bool, error) {
	return obldomain.ObligationRef{}, false, nil
}

func (f *boosterObligationWriterFake) ReopenDeferredObligationForGeneration(context.Context, string, string, time.Time, *obldomain.RecoveryReschedule) (obldomain.ObligationRef, bool, error) {
	return obldomain.ObligationRef{}, false, nil
}

func (f *boosterObligationWriterFake) RealignOpenObligationForGeneration(context.Context, string, string, time.Time, *time.Time, time.Time) (obldomain.ObligationRef, bool, error) {
	return obldomain.ObligationRef{}, false, nil
}

func (f *boosterObligationWriterFake) FindNearestPlannedBatchDate(context.Context, string, string, string, string, string, string, time.Time, time.Time) (*time.Time, error) {
	return nil, nil
}

func (f *boosterObligationWriterFake) CancelOpenObligationByIdempotencyKey(context.Context, string, string, string, time.Time) (string, bool, error) {
	return "", false, nil
}

func (f *boosterObligationWriterFake) OpenObligationForRepeatCycle(context.Context, string, string, string, string, string, int32, string) (obldomain.ObligationRef, bool, error) {
	return obldomain.ObligationRef{}, false, nil
}

func (f *boosterObligationWriterFake) GoatsWithVaccinationObligationsOutsideVersions(context.Context, string, []string, []string) ([]string, error) {
	return nil, nil
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

func (f *boosterObligationWriterFake) NextSuccessorSuffix(_ context.Context, _, _ string) (int, error) {
	return 1, nil
}

// The successor a completion mints must carry the cause that produced it, in the SAME
// vocabulary generation uses when it recomputes that cycle from history. Without this
// assertion the whole writer half of repeat-cycle identity is unprotected: deleting the
// metadata block leaves every other booster test green, and the duplicates come back.
func TestScheduleNextDoseStampsTheCauseThatProducedTheSuccessor(t *testing.T) {
	ctx := context.Background()
	proto := &boosterRuleReaderFake{rules: []protodomain.Rule{
		{
			RuleID: "rule-et-adult", DoseCode: "et_tt_adult_revac_182d", Sequence: 3,
			TriggerType: "after_previous_completion", OffsetDays: 182, MinGapDays: 182, Repeat: "every_n_days",
			EligibilityJSON: []byte(`{"vaccine":{"code":"ET_TT"}}`),
		},
	}}
	obl := &boosterObligationWriterFake{}
	svc := NewBoosterService(proto, obl)
	administered := time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC)

	if _, err := svc.ScheduleNextDose(ctx, ScheduleNextInput{
		TenantID: "tenant-1", ProtocolVersionID: "version-1", GoatID: "goat-1",
		ScopeType: "shed", ScopeID: "shed-1", PrevSequence: 3, AdministeredAt: administered,
		CompletedObligationID: "obligation-that-was-given",
	}); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(obl.inserted) != 1 {
		t.Fatalf("inserted %d obligations, want 1", len(obl.inserted))
	}
	rc := obl.inserted[0].RepeatCycle
	if rc == nil {
		t.Fatal("successor carries no cause: it is identified by its due date, which moves")
	}
	if want := obldomain.RepeatCycleRef("ET_TT", administered, 3); rc.SourceRef != want {
		t.Fatalf("cause = %q, want %q -- generation names the same cause this way", rc.SourceRef, want)
	}
	if rc.Source != obldomain.RepeatCycleSourceTrustedHistory {
		t.Fatalf("source = %q, want %q", rc.Source, obldomain.RepeatCycleSourceTrustedHistory)
	}
	if rc.AnchorObligationID == nil || *rc.AnchorObligationID != "obligation-that-was-given" {
		t.Fatalf("anchor obligation = %v, want the completed dose", rc.AnchorObligationID)
	}
}

// A one-off dose keeps its due-date identity: its due date does not move on its own, and
// stamping it would make two unrelated doses of one vaccine collide.
func TestScheduleNextDoseLeavesNonRepeatDosesUnstamped(t *testing.T) {
	ctx := context.Background()
	proto := &boosterRuleReaderFake{rules: []protodomain.Rule{
		{RuleID: "rule-et-primary", DoseCode: "et_tt_7w", Sequence: 2, TriggerType: "birth_age"},
		{
			RuleID: "rule-et-w2", DoseCode: "et_tt_adult_w2", Sequence: 3, TriggerType: "fixed_offset",
			OffsetDays: 21, Repeat: "none", EligibilityJSON: []byte(`{"vaccine":{"code":"ET_TT"}}`),
		},
	}}
	obl := &boosterObligationWriterFake{}
	svc := NewBoosterService(proto, obl)

	if _, err := svc.ScheduleNextDose(ctx, ScheduleNextInput{
		TenantID: "tenant-1", ProtocolVersionID: "version-1", GoatID: "goat-1",
		ScopeType: "shed", ScopeID: "shed-1", PrevSequence: 2,
		AdministeredAt:        time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC),
		CompletedObligationID: "obligation-that-was-given",
	}); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(obl.inserted) == 1 && obl.inserted[0].RepeatCycle != nil {
		t.Fatalf("one-off dose was stamped with a repeat cause: %+v", obl.inserted[0].RepeatCycle)
	}
}
