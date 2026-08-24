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

func TestGenerateForVersionCreatesSuccessorWhenCanceledWorkBecomesEligibleAgain(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		rules: []protodomain.Rule{{
			RuleID: "rule-fmd", DoseCode: "fmd-dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 28,
		}},
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]}}`),
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{
			GoatID: "returning-goat", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1",
			ParkID: "park-1", ShedID: "shed-1",
		}},
	}
	obl := &generationObligationFake{}
	gen := NewGenerationService(proto, goats, obl)

	first, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if first.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("first result=%#v inserted=%d, want one scheduled obligation", first, len(obl.inserted))
	}
	baseKey := obl.inserted[0].IdempotencyKey
	// The REAL persisted reason written by generation's shift-away cancel path
	// (CancelOpenVaccinationObligationsForGoatVersion at generation.go): park A -> B shift.
	obl.inserted[0].Status = "canceled"
	if obl.cancelReasonsByKey == nil {
		obl.cancelReasonsByKey = map[string]string{}
	}
	obl.cancelReasonsByKey[baseKey] = "ineligible_after_shift"

	second, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if second.Generated != 1 || len(obl.inserted) != 2 {
		t.Fatalf("second result=%#v inserted=%d, want successor obligation", second, len(obl.inserted))
	}
	if obl.inserted[0].Status != "canceled" {
		t.Fatalf("base status=%s want canceled preserved", obl.inserted[0].Status)
	}
	successor := obl.inserted[1]
	if successor.Status != "scheduled" {
		t.Fatalf("successor status=%s want scheduled", successor.Status)
	}
	if successor.IdempotencyKey == baseKey || !strings.HasPrefix(successor.IdempotencyKey, baseKey+":successor:") {
		t.Fatalf("successor key=%q base=%q", successor.IdempotencyKey, baseKey)
	}
}

// TestInsertSuccessorForCanceledGenerationReplayHasNoAttemptCeiling is R50-011: the successor
// numbering loop in insertSuccessorForCanceledGenerationReplay used to hard-stop after 32 attempts.
// That ceiling has been removed (the loop is now unbounded: `for attempt := 1; ; attempt++`). A
// base key with 32 PRIOR canceled successor attempts (":successor:01".."32") must still mint the
// 33rd successor instead of erroring.
func TestInsertSuccessorForCanceledGenerationReplayHasNoAttemptCeiling(t *testing.T) {
	ctx := context.Background()
	obl := &generationObligationFake{}
	gen := NewGenerationService(&generationProtoFake{}, &generationGoatFake{}, obl)

	const baseKey = "tenant-1:rule-fmd:goat-1:1"
	base := obldomain.NewObligation{
		TenantID: "tenant-1", ProtocolVersionID: "version-1", RuleID: "rule-fmd",
		TargetType: "goat", TargetID: "goat-1", ScopeType: "shed", ScopeID: "shed-1",
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Sequence: 1,
	}
	// Pre-seed 32 canceled prior successor attempts for this exact base key -- each one, on replay,
	// resolves to "canceled" via ReopenDeferredObligationForGeneration and must NOT stop the loop.
	for i := 1; i <= 32; i++ {
		key := fmt.Sprintf("%s:successor:%02d", baseKey, i)
		row := base
		row.IdempotencyKey = key
		row.Status = "canceled"
		if _, applied, err := obl.InsertObligation(ctx, row); err != nil || !applied {
			t.Fatalf("seed prior successor %d: applied=%v err=%v", i, applied, err)
		}
		obl.cancelReasonsByKey[key] = "ineligible_after_shift"
	}
	if got := len(obl.inserted); got != 32 {
		t.Fatalf("seeded successors = %d, want 32", got)
	}

	ref, _, generated, err := gen.insertSuccessorForCanceledGenerationReplay(
		ctx, "tenant-1", baseKey, base, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), false, "")
	if err != nil {
		t.Fatalf("insert successor: %v", err)
	}
	if !generated {
		t.Fatalf("expected a newly generated 33rd successor, got reused ref=%#v", ref)
	}
	if len(obl.inserted) != 33 {
		t.Fatalf("total inserted = %d, want 33 (32 seeded + the new 33rd)", len(obl.inserted))
	}
	wantKey := baseKey + ":successor:33"
	if got := obl.inserted[32].IdempotencyKey; got != wantKey {
		t.Fatalf("33rd successor key = %q, want %q", got, wantKey)
	}
	if ref.Status != "scheduled" {
		t.Fatalf("33rd successor status = %q, want scheduled", ref.Status)
	}
}

// TestGenerateForVersionCanceledWithoutReasonFailsClosedNoSuccessor: a canceled row whose
// cancellation event carries NO reason (legacy data) gives no evidence the goat is returning —
// generation must NOT mint a successor.
func TestGenerateForVersionCanceledWithoutReasonFailsClosedNoSuccessor(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		rules: []protodomain.Rule{{
			RuleID: "rule-fmd", DoseCode: "fmd-dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 28,
		}},
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]}}`),
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{
			GoatID: "legacy-goat", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1",
			ParkID: "park-1", ShedID: "shed-1",
		}},
	}
	obl := &generationObligationFake{}
	gen := NewGenerationService(proto, goats, obl)

	if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	obl.inserted[0].Status = "canceled" // no reason recorded anywhere

	second, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if second.Generated != 0 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%d, want NO successor for reason-less canceled row", second, len(obl.inserted))
	}
}

// TestGenerateForVersionShiftCanceledClinicallyHeldGoatGetsDeferredSuccessor is the LIFE-001
// guard: a goat whose work was canceled by a park shift ("ineligible_after_shift") and that
// requalifies while clinically held must receive a fresh DEFERRED successor — not nothing, and
// not a forced-scheduled row.
func TestGenerateForVersionShiftCanceledClinicallyHeldGoatGetsDeferredSuccessor(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		rules: []protodomain.Rule{{
			RuleID: "rule-fmd", DoseCode: "fmd-dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 28,
		}},
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":["sick"]}}`),
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{
			GoatID: "returning-sick-goat", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1",
			ParkID: "park-1", ShedID: "shed-1",
		}},
	}
	obl := &generationObligationFake{}
	gen := NewGenerationService(proto, goats, obl)

	if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	baseKey := obl.inserted[0].IdempotencyKey
	obl.inserted[0].Status = "canceled"
	if obl.cancelReasonsByKey == nil {
		obl.cancelReasonsByKey = map[string]string{}
	}
	obl.cancelReasonsByKey[baseKey] = "ineligible_after_shift"
	// Goat returns but is now clinically held.
	goats.list[0].HealthStatus = "sick"

	second, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if second.Generated != 1 || second.Deferred != 1 || len(obl.inserted) != 2 {
		t.Fatalf("result=%#v inserted=%d, want one DEFERRED successor", second, len(obl.inserted))
	}
	if obl.inserted[0].Status != "canceled" {
		t.Fatalf("base status=%s want canceled preserved", obl.inserted[0].Status)
	}
	successor := obl.inserted[1]
	if successor.Status != "deferred" {
		t.Fatalf("successor status=%s want deferred (LIFE-001)", successor.Status)
	}
	if !strings.HasPrefix(successor.IdempotencyKey, baseKey+":successor:") {
		t.Fatalf("successor key=%q base=%q", successor.IdempotencyKey, baseKey)
	}
	// R50-006: the deferred successor must record its 'deferred' status event exactly like the
	// non-successor deferred path — atomically alongside the insert, not silently skipped.
	deferredEvents := 0
	for _, ev := range obl.recordedStatusEvents {
		if ev.ObligationID == "obligation-1" && ev.EventType == "deferred" {
			deferredEvents++
		}
	}
	if deferredEvents != 1 {
		t.Fatalf("recorded %d deferred status events for deferred successor, want exactly 1: %#v", deferredEvents, obl.recordedStatusEvents)
	}
}

// TestGenerateForVersionExitCanceledGoatGetsNoSuccessor: "ineligible_after_exit" (goat left the
// herd) is terminal for this row — re-entry generation handles a returning goat fresh.
func TestGenerateForVersionExitCanceledGoatGetsNoSuccessor(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		rules: []protodomain.Rule{{
			RuleID: "rule-fmd", DoseCode: "fmd-dose-1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 28,
		}},
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","defer_states":[]}}`),
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{
			GoatID: "exited-goat", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1",
			ParkID: "park-1", ShedID: "shed-1",
		}},
	}
	obl := &generationObligationFake{}
	gen := NewGenerationService(proto, goats, obl)

	if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	baseKey := obl.inserted[0].IdempotencyKey
	obl.inserted[0].Status = "canceled"
	if obl.cancelReasonsByKey == nil {
		obl.cancelReasonsByKey = map[string]string{}
	}
	obl.cancelReasonsByKey[baseKey] = "ineligible_after_exit"

	second, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if second.Generated != 0 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%d, want NO successor for exit-canceled row", second, len(obl.inserted))
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

func TestPrimaryCourseContinuationFromHistorySchedulesAdultETTTDoseTwoAfterTwentyOneDays(t *testing.T) {
	administered := time.Date(2026, time.June, 30, 8, 0, 0, 0, time.UTC)
	rules := []protodomain.Rule{
		{RuleID: "rule-et-adult-w1", DoseCode: "et_tt_adult_w1", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 7},
		{RuleID: "rule-et-adult-w2", DoseCode: "et_tt_adult_w2", Sequence: 2, TriggerType: "post_arrival", OffsetDays: 21, MinGapDays: 21},
		{RuleID: "rule-et-revac", DoseCode: "et_tt_revac", Sequence: 3, TriggerType: "after_previous_completion", OffsetDays: 182, MinGapDays: 182, Repeat: "every_n_days"},
	}
	history := []domain.RecentVaccineAdministration{{
		AdministeredAt: administered,
		VaccineCode:    "ET_TT",
		DoseCode:       "et_tt_adult_w1",
		Sequence:       1,
	}}

	due, found, err := primaryCourseContinuationDueFromHistory(rules[1], vaccineProfile{Code: "ET_TT"}, rules, genEligibility{}, vaccineProfile{Code: "ET_TT"}, schedulePathAdultProcurement, history)
	if err != nil {
		t.Fatalf("course continuation: %v", err)
	}
	want := businessDayStart(administered).AddDate(0, 0, 21)
	if !found || !due.Equal(want) {
		t.Fatalf("adult ET+TT dose 2 due=%s found=%v, want %s", due, found, want)
	}
	wait, err := repeatMustWaitForPrimaryCourse(rules[2], vaccineProfile{Code: "ET_TT"}, rules, genEligibility{}, vaccineProfile{Code: "ET_TT"}, schedulePathAdultProcurement, history)
	if err != nil {
		t.Fatalf("repeat wait: %v", err)
	}
	if !wait {
		t.Fatal("ET+TT revac must wait until adult dose 2 has been administered")
	}
}

func TestGenerateMaterializesAdultETTTDoseTwoWhenDueTodayOrOverdue(t *testing.T) {
	ctx := context.Background()
	administered := time.Date(2026, time.June, 30, 8, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC)
	entryDate := time.Date(2026, time.June, 23, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		rules: []protodomain.Rule{
			{RuleID: "rule-et-adult-w1", DoseCode: "et_tt_adult_w1", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 7},
			{RuleID: "rule-et-adult-w2", DoseCode: "et_tt_adult_w2", Sequence: 2, TriggerType: "post_arrival", OffsetDays: 21, MinGapDays: 21, CatchUp: "immediate"},
		},
		ruleDSL: []byte(`{"eligibility":{"species":["goat"],"lifecycle":["alive"],"health":["healthy"]},"missed_dose_policy":{"materialize_only_future_open_work":true},"matrix_rows":[{"vaccine":{"code":"ET_TT","name":"ET+TT"},"eligibility":{"species":["goat"],"lifecycle":["alive"],"health":["healthy"]},"schedule":[{"dose_code":"et_tt_adult_w1"},{"dose_code":"et_tt_adult_w2"}]}]}`),
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{
			GoatID: "goat-1", Species: "goat", LifecycleStatus: "alive", HealthStatus: "healthy", EntryDate: &entryDate,
		}},
		trustedByDue: map[string]bool{
			businessDayStart(administered).UTC().Format(time.RFC3339Nano): true,
		},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"goat-1": {{
				AdministeredAt:    administered,
				VaccineCode:       "ET_TT",
				DoseCode:          "et_tt_adult_w1",
				Sequence:          1,
				ProtocolVersionID: "version-1",
				ProtocolID:        "",
			}},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("GenerateForVersion: %v", err)
	}
	var got obldomain.NewObligation
	found := false
	for _, inserted := range obl.inserted {
		if inserted.RuleID == "rule-et-adult-w2" {
			got = inserted
			found = true
			break
		}
	}
	if result.Generated == 0 || !found {
		t.Fatalf("result=%#v inserted=%#v, want adult ET+TT dose 2 generated", result, obl.inserted)
	}
	wantDue := businessDayStart(administered).AddDate(0, 0, 21)
	wantLastSafe := wantDue.AddDate(0, 0, 7)
	if got.RuleID != "rule-et-adult-w2" || got.DueAt.Before(wantDue) || got.DueAt.After(wantLastSafe) {
		t.Fatalf("inserted=%#v, want rule-et-adult-w2 inside %s..%s", got, wantDue, wantLastSafe)
	}
}

func TestRepeatDoesNotWaitAfterPrimaryCourseComplete(t *testing.T) {
	administeredW2 := time.Date(2026, time.July, 21, 8, 0, 0, 0, time.UTC)
	rules := []protodomain.Rule{
		{RuleID: "rule-et-adult-w1", DoseCode: "et_tt_adult_w1", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 7},
		{RuleID: "rule-et-adult-w2", DoseCode: "et_tt_adult_w2", Sequence: 2, TriggerType: "post_arrival", OffsetDays: 21, MinGapDays: 21},
		{RuleID: "rule-et-revac", DoseCode: "et_tt_revac", Sequence: 3, TriggerType: "after_previous_completion", OffsetDays: 182, MinGapDays: 182, Repeat: "every_n_days"},
	}
	history := []domain.RecentVaccineAdministration{{
		AdministeredAt: administeredW2,
		VaccineCode:    "ET_TT",
		DoseCode:       "et_tt_adult_w2",
		Sequence:       2,
	}}

	wait, err := repeatMustWaitForPrimaryCourse(rules[2], vaccineProfile{Code: "ET_TT"}, rules, genEligibility{}, vaccineProfile{Code: "ET_TT"}, schedulePathAdultProcurement, history)
	if err != nil {
		t.Fatalf("repeat wait: %v", err)
	}
	if wait {
		t.Fatal("ET+TT revac should not wait after adult dose 2 exists")
	}
	due, ok := dueAfterPreviousCompletion(rules[2], vaccineProfile{Code: "ET_TT"}, history)
	want := businessDayStart(administeredW2).AddDate(0, 0, 182)
	if !ok || !due.Equal(want) {
		t.Fatalf("revac due=%s ok=%v, want dose 2 + 182d = %s", due, ok, want)
	}
}

func TestApprovedVaccineRepeatIntervalsContinueAcrossFutureCycles(t *testing.T) {
	start := time.Date(2026, time.June, 30, 8, 0, 0, 0, time.UTC)
	assertDue := func(t *testing.T, rule protodomain.Rule, vaccineCode, doseCode string, sequence int32, administered, want time.Time) {
		t.Helper()
		due, ok := dueAfterPreviousCompletion(rule, vaccineProfile{Code: vaccineCode}, []domain.RecentVaccineAdministration{{
			AdministeredAt: administered,
			VaccineCode:    vaccineCode,
			DoseCode:       doseCode,
			Sequence:       sequence,
		}})
		if !ok || !due.Equal(want) {
			t.Fatalf("%s due=%s ok=%v, want %s", rule.DoseCode, due, ok, want)
		}
	}

	etRevac := protodomain.Rule{RuleID: "rule-et-revac", DoseCode: "et_tt_revac", Sequence: 3, TriggerType: "after_previous_completion", OffsetDays: 182, MinGapDays: 182, Repeat: "every_n_days"}
	etDose2 := start.AddDate(0, 0, 21)
	assertDue(t, etRevac, "ET_TT", "et_tt_adult_w2", 2, etDose2, businessDayStart(etDose2).AddDate(0, 0, 182))
	firstETRevac := time.Date(2027, time.January, 19, 8, 0, 0, 0, time.UTC)
	assertDue(t, etRevac, "ET_TT", "et_tt_revac", 3, firstETRevac, businessDayStart(firstETRevac).AddDate(0, 0, 182))

	cases := []struct {
		name         string
		rule         protodomain.Rule
		vaccineCode  string
		doseCode     string
		sequence     int32
		administered time.Time
		want         time.Time
	}{
		{
			name:         "FMD repeats every 274 days",
			rule:         protodomain.Rule{RuleID: "rule-fmd-revac", DoseCode: "fmd_revac", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 274, MinGapDays: 274, Repeat: "every_n_days"},
			vaccineCode:  "FMD",
			doseCode:     "fmd_adult_w9",
			sequence:     1,
			administered: time.Date(2026, time.March, 20, 8, 0, 0, 0, time.UTC),
			want:         businessDayStart(time.Date(2026, time.December, 19, 8, 0, 0, 0, time.UTC)),
		},
		{
			name:         "HS repeats yearly",
			rule:         protodomain.Rule{RuleID: "rule-hs-revac", DoseCode: "hs_revac", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 365, MinGapDays: 365, Repeat: "yearly"},
			vaccineCode:  "HS",
			doseCode:     "hs_adult_w9",
			sequence:     1,
			administered: time.Date(2026, time.February, 21, 8, 0, 0, 0, time.UTC),
			want:         businessDayStart(time.Date(2027, time.February, 21, 8, 0, 0, 0, time.UTC)),
		},
		{
			name:         "PPR repeats every 1095 days",
			rule:         protodomain.Rule{RuleID: "rule-ppr-revac", DoseCode: "ppr_revac", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 1095, MinGapDays: 1095, Repeat: "every_n_days"},
			vaccineCode:  "PPR",
			doseCode:     "ppr_adult_w1",
			sequence:     1,
			administered: time.Date(2025, time.December, 9, 8, 0, 0, 0, time.UTC),
			want:         businessDayStart(time.Date(2028, time.December, 8, 8, 0, 0, 0, time.UTC)),
		},
		{
			name:         "Goat Pox repeats yearly",
			rule:         protodomain.Rule{RuleID: "rule-goat-pox-revac", DoseCode: "goat_pox_revac", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 365, MinGapDays: 365, Repeat: "yearly"},
			vaccineCode:  "GOAT_POX",
			doseCode:     "goat_pox_adult_w5",
			sequence:     1,
			administered: time.Date(2026, time.March, 19, 8, 0, 0, 0, time.UTC),
			want:         businessDayStart(time.Date(2027, time.March, 19, 8, 0, 0, 0, time.UTC)),
		},
		{
			name:         "Sheep Pox repeats yearly",
			rule:         protodomain.Rule{RuleID: "rule-sheep-pox-revac", DoseCode: "sheep_pox_revac", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 365, MinGapDays: 365, Repeat: "yearly"},
			vaccineCode:  "SHEEP_POX",
			doseCode:     "sheep_pox_adult_w5",
			sequence:     1,
			administered: time.Date(2026, time.March, 19, 8, 0, 0, 0, time.UTC),
			want:         businessDayStart(time.Date(2027, time.March, 19, 8, 0, 0, 0, time.UTC)),
		},
		{
			name:         "Blue Tongue repeats yearly after kid dose 2",
			rule:         protodomain.Rule{RuleID: "rule-blue-tongue-revac", DoseCode: "blue_tongue_revac", Sequence: 3, TriggerType: "after_previous_completion", OffsetDays: 365, MinGapDays: 365, Repeat: "yearly"},
			vaccineCode:  "BLUE_TONGUE",
			doseCode:     "blue_tongue_kid_20w",
			sequence:     2,
			administered: time.Date(2026, time.August, 1, 8, 0, 0, 0, time.UTC),
			want:         businessDayStart(time.Date(2027, time.August, 1, 8, 0, 0, 0, time.UTC)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertDue(t, tc.rule, tc.vaccineCode, tc.doseCode, tc.sequence, tc.administered, tc.want)
		})
	}

	nextET, ok := nextRepeatCycle(etRevac, time.Date(2027, time.January, 19, 0, 0, 0, 0, time.UTC), time.Date(2027, time.August, 1, 0, 0, 0, 0, time.UTC))
	if !ok || !nextET.Equal(businessDayStart(time.Date(2028, time.January, 18, 8, 0, 0, 0, time.UTC))) {
		t.Fatalf("ET+TT skipped-cycle next=%s ok=%v, want 2028-01-18", nextET, ok)
	}
	nextHS, ok := nextRepeatCycle(protodomain.Rule{Repeat: "yearly"}, time.Date(2027, time.February, 21, 0, 0, 0, 0, time.UTC), time.Date(2027, time.December, 1, 0, 0, 0, 0, time.UTC))
	if !ok || !nextHS.Equal(businessDayStart(time.Date(2028, time.February, 21, 8, 0, 0, 0, time.UTC))) {
		t.Fatalf("HS skipped-cycle next=%s ok=%v, want 2028-02-21", nextHS, ok)
	}
}

func TestPrimaryCourseContinuationFromHistoryKeepsKidAndBlueTongueGaps(t *testing.T) {
	kidETTT := []protodomain.Rule{
		{RuleID: "rule-et-kid-4w", DoseCode: "et_tt_kid_4w", Sequence: 1, TriggerType: "birth_age", OffsetDays: 28},
		{RuleID: "rule-et-kid-7w", DoseCode: "et_tt_kid_7w", Sequence: 2, TriggerType: "birth_age", OffsetDays: 49, MinGapDays: 21},
	}
	kidETTTAt := time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC)
	due, found, err := primaryCourseContinuationDueFromHistory(kidETTT[1], vaccineProfile{Code: "ET_TT"}, kidETTT, genEligibility{}, vaccineProfile{Code: "ET_TT"}, schedulePathKid, []domain.RecentVaccineAdministration{{
		AdministeredAt: kidETTTAt,
		VaccineCode:    "ET_TT",
		DoseCode:       "et_tt_kid_4w",
		Sequence:       1,
	}})
	if err != nil {
		t.Fatalf("kid ET+TT continuation: %v", err)
	}
	want := businessDayStart(kidETTTAt).AddDate(0, 0, 21)
	if !found || !due.Equal(want) {
		t.Fatalf("kid ET+TT due=%s found=%v, want %s", due, found, want)
	}

	blueTongue := []protodomain.Rule{
		{RuleID: "rule-bt-16w", DoseCode: "blue_tongue_kid_16w", Sequence: 1, TriggerType: "birth_age", OffsetDays: 112},
		{RuleID: "rule-bt-20w", DoseCode: "blue_tongue_kid_20w", Sequence: 2, TriggerType: "birth_age", OffsetDays: 140, MinGapDays: 28},
	}
	blueTongueAt := time.Date(2026, time.July, 3, 8, 0, 0, 0, time.UTC)
	due, found, err = primaryCourseContinuationDueFromHistory(blueTongue[1], vaccineProfile{Code: "BLUE_TONGUE"}, blueTongue, genEligibility{}, vaccineProfile{Code: "BLUE_TONGUE"}, schedulePathKid, []domain.RecentVaccineAdministration{{
		AdministeredAt: blueTongueAt,
		VaccineCode:    "BLUE_TONGUE",
		DoseCode:       "blue_tongue_kid_16w",
		Sequence:       1,
	}})
	if err != nil {
		t.Fatalf("blue tongue continuation: %v", err)
	}
	want = businessDayStart(blueTongueAt).AddDate(0, 0, 28)
	if !found || !due.Equal(want) {
		t.Fatalf("blue tongue due=%s found=%v, want %s", due, found, want)
	}
}

func TestSingleDoseRepeatDoesNotWaitForMissingPrimaryCourseDose(t *testing.T) {
	administered := time.Date(2026, time.March, 20, 8, 0, 0, 0, time.UTC)
	rules := []protodomain.Rule{
		{RuleID: "rule-fmd-adult-w1", DoseCode: "fmd_adult_w1", Sequence: 1, TriggerType: "manual_campaign", OffsetDays: 63},
		{RuleID: "rule-fmd-revac", DoseCode: "fmd_revac", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 274, MinGapDays: 274, Repeat: "every_n_days"},
	}
	history := []domain.RecentVaccineAdministration{{
		AdministeredAt: administered,
		VaccineCode:    "FMD",
		DoseCode:       "fmd_adult_w1",
		Sequence:       1,
	}}

	wait, err := repeatMustWaitForPrimaryCourse(rules[1], vaccineProfile{Code: "FMD"}, rules, genEligibility{}, vaccineProfile{Code: "FMD"}, schedulePathAdultProcurement, history)
	if err != nil {
		t.Fatalf("repeat wait: %v", err)
	}
	if wait {
		t.Fatal("single-dose FMD repeat must not wait for a nonexistent course booster")
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
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","under_treatment","recovering","quarantine","icu"]}}`),
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
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"healthy","reproductive":"any","defer_states":["sick","under_treatment","recovering","quarantine","icu"]}}`),
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
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"adult","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","under_treatment","recovering","quarantine","icu"]},"procurement_policy":{"warmup_no_vaccination_days":7,"kids_normal_schedule_until_weeks":16,"adult_prior_vaccination_allowed":true}}`),
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

func TestGenerateForVersionAutomaticallySchedulesAdultBlankHistoryCampaignByShed(t *testing.T) {
	ctx := context.Background()
	entryEarly := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	entryLate := time.Date(2026, time.June, 28, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"adult","species":"sheep","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","under_treatment","recovering","quarantine","icu"]}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-sheep-pox", DoseCode: "sheep_pox_adult_w1", Sequence: 1, TriggerType: "manual_campaign", OffsetDays: 35, DueWindowDays: 7,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "godel-main", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Species: "sheep", Stage: "adult", EntryDate: &entryEarly, ShedID: "shed-godel-1", ParkID: "cpt", PartitionLabel: "Part 1"},
		// A physical-shed placement is sufficient. Partition metadata is optional and
		// must never block an adult from joining the normal catch-up drive.
		{GoatID: "godel-late-singleton", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Species: "sheep", Stage: "adult", EntryDate: &entryLate, ShedID: "shed-godel-1", ParkID: "cpt"},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	asOf := time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC)
	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("normal generate: %v", err)
	}
	wantDue := adultCampaignStart(asOf)
	if result.Generated != 2 || len(obl.inserted) != 2 {
		t.Fatalf("normal result=%#v inserted=%#v, want both blank-history adults in the normal campaign", result, obl.inserted)
	}
	for _, inserted := range obl.inserted {
		if !inserted.DueAt.Equal(wantDue) {
			t.Fatalf("goat %s due=%s, want shared adult campaign due %s", inserted.TargetID, inserted.DueAt, wantDue)
		}
	}

	result, err = gen.GenerateManualCampaignForVersion(ctx, "tenant-1", "version-1", "cpt-adult-campaign", asOf)
	if err != nil {
		t.Fatalf("manual campaign generate: %v", err)
	}
	if result.Generated != 0 || result.Deferred != 0 || len(obl.inserted) != 2 {
		t.Fatalf("explicit replay result=%#v inserted=%#v, want no duplicate campaign obligations", result, obl.inserted)
	}
	for _, inserted := range obl.inserted {
		if !inserted.DueAt.Equal(wantDue) {
			t.Fatalf("goat %s due=%s, want manual campaign due %s", inserted.TargetID, inserted.DueAt, wantDue)
		}
		if inserted.Status != "scheduled" {
			t.Fatalf("goat %s status=%q, want scheduled normal campaign row", inserted.TargetID, inserted.Status)
		}
	}
}

func TestGenerateForVersionClubsAdultBlankHistoryWithSameVaccineRepeatDate(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)
	earlyHistoryAt := time.Date(2026, time.March, 19, 0, 0, 0, 0, time.UTC)
	lateHistoryAt := time.Date(2026, time.April, 7, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"adult","species":"goat","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any"}}`),
		rules: []protodomain.Rule{
			{RuleID: "rule-fmd-adult-w1", DoseCode: "fmd_adult_w1", Sequence: 1, TriggerType: "manual_campaign", OffsetDays: 63, DueWindowDays: 30},
			{RuleID: "rule-fmd-adult-repeat", DoseCode: "fmd_adult_repeat", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 274, DueWindowDays: 30, Repeat: "every_n_days"},
		},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{
			{GoatID: "blank-history", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Species: "goat", Stage: "adult", ShedID: "shed-godel-1", ParkID: "cpt"},
			{GoatID: "repeat-history-early", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Species: "goat", Stage: "adult", ShedID: "shed-gandhi", ParkID: "cpt", PartitionLabel: "Part 2"},
			{GoatID: "repeat-history-late", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Species: "goat", Stage: "adult", ShedID: "shed-godel-1", ParkID: "cpt", PartitionLabel: "Part 4"},
		},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"repeat-history-early": {{AdministeredAt: earlyHistoryAt, VaccineCode: "FMD", VaccineType: "killed", PathogenClass: "viral", DoseCode: "fmd_adult_repeat", Sequence: 2}},
			"repeat-history-late":  {{AdministeredAt: lateHistoryAt, VaccineCode: "FMD", VaccineType: "killed", PathogenClass: "viral", DoseCode: "fmd_adult_repeat", Sequence: 2}},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}
	staleDue := businessDayStart(earlyHistoryAt).AddDate(0, 0, 274)
	staleKey := obligationKey("tenant-1", "version-1", "rule-fmd-adult-repeat", "goat", "repeat-history-early", staleDue.UTC().Format(time.RFC3339), "2")
	if _, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID: "tenant-1", ProtocolVersionID: "version-1", RuleID: "rule-fmd-adult-repeat",
		TargetType: "goat", TargetID: "repeat-history-early", ScopeType: "shed", ScopeID: "shed-gandhi",
		DueAt: staleDue, Status: "scheduled", IdempotencyKey: staleKey, Sequence: 2,
	}); err != nil || !applied {
		t.Fatalf("seed stale earlier repeat: applied=%v err=%v", applied, err)
	}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	wantDue := businessDayStart(lateHistoryAt).AddDate(0, 0, 274)
	if result.Generated != 3 || len(obl.inserted) != 4 {
		t.Fatalf("result=%#v inserted=%#v, want stale predecessor plus one blank-history and two repeat obligations", result, obl.inserted)
	}
	seen := map[string]string{}
	for _, inserted := range obl.inserted {
		if inserted.Status == "canceled" {
			continue
		}
		if !inserted.DueAt.Equal(wantDue) {
			t.Fatalf("goat %s due=%s, want shared drive date %s", inserted.TargetID, inserted.DueAt, wantDue)
		}
		seen[inserted.TargetID] = inserted.RuleID
	}
	if seen["blank-history"] != "rule-fmd-adult-w1" || seen["repeat-history-early"] != "rule-fmd-adult-repeat" || seen["repeat-history-late"] != "rule-fmd-adult-repeat" {
		t.Fatalf("generated rules=%#v, want catch-up and repeat dose instructions inside one date cohort", seen)
	}
	if got := obl.cancelReasonsByKey[staleKey]; got != "adult_campaign_date_realigned" {
		t.Fatalf("stale repeat cancellation reason=%q, want adult_campaign_date_realigned", got)
	}
}

func TestGenerateForVersionRealignsExistingStableBlankHistoryWhenRepeatHistoryArrivesLater(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)
	historyAt := time.Date(2026, time.April, 7, 0, 0, 0, 0, time.UTC)
	rules := []protodomain.Rule{
		{RuleID: "rule-fmd-adult-w1", DoseCode: "fmd_adult_w1", Sequence: 1, TriggerType: "manual_campaign", DueWindowDays: 30},
		{RuleID: "rule-fmd-adult-repeat", DoseCode: "fmd_adult_repeat", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 274, DueWindowDays: 30, Repeat: "every_n_days"},
	}
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"adult","species":"goat","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any"}}`),
		rules:   rules,
	}
	blank := domain.EligibleGoat{GoatID: "blank-history", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Species: "goat", Stage: "adult", ShedID: "shed-godel-1", ParkID: "cpt"}
	historyGoat := domain.EligibleGoat{GoatID: "history-arrived-later", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Species: "goat", Stage: "adult", ShedID: "shed-gandhi", ParkID: "cpt"}
	goats := &generationGoatFake{list: []domain.EligibleGoat{blank}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	if first, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil || first.Generated != 1 {
		t.Fatalf("first blank-history generation result=%#v err=%v", first, err)
	}
	stableKey := stableAdultCampaignObligationKey("tenant-1", "version-1", rules[0], blank)
	if got := obl.inserted[0].DueAt; !got.Equal(adultCampaignStart(asOf)) {
		t.Fatalf("initial due=%s, want standalone campaign %s", got, adultCampaignStart(asOf))
	}

	goats.list = append(goats.list, historyGoat)
	goats.vaccineHistory = map[string][]domain.RecentVaccineAdministration{
		historyGoat.GoatID: {{AdministeredAt: historyAt, VaccineCode: "FMD", VaccineType: "killed", PathogenClass: "viral", DoseCode: "fmd_adult_repeat", Sequence: 2}},
	}
	if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("second generation after history import: %v", err)
	}
	want := businessDayStart(historyAt).AddDate(0, 0, 274)
	idx := obl.keyIndex[stableKey]
	if got := obl.inserted[idx].DueAt; !got.Equal(want) {
		t.Fatalf("stable blank-history due=%s, want cohort date %s", got, want)
	}
	if len(obl.realignedKeys) != 1 || obl.realignedKeys[0] != stableKey {
		t.Fatalf("realigned keys=%v, want [%s]", obl.realignedKeys, stableKey)
	}
}

func TestGenerateForVersionAcceptedHistoryRetiresStableAdultCampaignAndStartsRepeat(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)
	rules := []protodomain.Rule{
		{RuleID: "rule-fmd-adult-w1", DoseCode: "fmd_adult_w1", Sequence: 1, TriggerType: "manual_campaign", DueWindowDays: 30},
		{RuleID: "rule-fmd-adult-repeat", DoseCode: "fmd_adult_repeat", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 182, DueWindowDays: 30, Repeat: "every_n_days"},
	}
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"adult","species":"goat","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any"}}`),
		rules:   rules,
	}
	goat := domain.EligibleGoat{
		GoatID: "late-verified", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open",
		Species: "goat", Stage: "adult", ParkID: "cpt", ShedID: "shed-gandhi",
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{goat}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	first, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("first blank-history generate: %v", err)
	}
	if first.Generated != 1 || len(obl.inserted) != 1 || obl.inserted[0].Status != "scheduled" {
		t.Fatalf("first result=%#v inserted=%#v, want one normal adult campaign row", first, obl.inserted)
	}
	stableKey := stableAdultCampaignObligationKey("tenant-1", "version-1", rules[0], goat)
	if obl.inserted[0].IdempotencyKey != stableKey {
		t.Fatalf("campaign key=%q, want stable key %q", obl.inserted[0].IdempotencyKey, stableKey)
	}

	administeredAt := time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC)
	goats.vaccineHistory = map[string][]domain.RecentVaccineAdministration{
		goat.GoatID: {{
			AdministeredAt: administeredAt, VaccineCode: "FMD", VaccineType: "killed", PathogenClass: "viral",
			DoseCode: "fmd_adult_w1", Sequence: 1,
		}},
	}
	second, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("second accepted-history generate: %v", err)
	}
	if second.SuppressedByTrustedHistory != 1 || second.Generated != 1 || len(obl.inserted) != 2 {
		t.Fatalf("second result=%#v inserted=%#v, want primary retired and one repeat generated", second, obl.inserted)
	}
	if obl.inserted[0].Status != "canceled" || obl.cancelReasonsByKey[stableKey] != "vaccine_history_outranks_adult_campaign" {
		t.Fatalf("stable row=%#v reason=%q, want canceled after accepted history", obl.inserted[0], obl.cancelReasonsByKey[stableKey])
	}
	wantRepeatDue := businessDayStart(administeredAt).AddDate(0, 0, 182)
	if got := obl.inserted[1]; got.RuleID != "rule-fmd-adult-repeat" || got.Status != "scheduled" || !got.DueAt.Equal(wantRepeatDue) {
		t.Fatalf("repeat=%#v, want accepted medical date + 182 days = %s", got, wantRepeatDue)
	}
}

func TestCampaignDueOverridesDoesNotExtendExpiredAuthoredWindow(t *testing.T) {
	asOf := time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)
	rules := []protodomain.Rule{
		{RuleID: "rule-fmd-adult-w1", DoseCode: "fmd_adult_w1", Sequence: 1, TriggerType: "manual_campaign", DueWindowDays: 30},
		{RuleID: "rule-fmd-adult-repeat", DoseCode: "fmd_adult_repeat", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 182, DueWindowDays: 7, Repeat: "every_n_days"},
	}
	profile := vaccineProfile{Code: "FMD", Type: "killed", PathogenClass: "viral", Class: immunoKilledViral}
	plans := []goatGenerationPlan{
		{
			versionID: "version-1", rules: rules, vaccineProfile: profile,
			goat: domain.EligibleGoat{GoatID: "expired-repeat", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "goat", Stage: "adult", ParkID: "cpt", ShedID: "gandhi"},
		},
		{
			versionID: "version-1", rules: rules, vaccineProfile: profile,
			goat: domain.EligibleGoat{GoatID: "blank-history", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "goat", Stage: "adult", ParkID: "cpt", ShedID: "godel-1"},
		},
	}
	history := map[string][]domain.RecentVaccineAdministration{
		"expired-repeat": {{
			AdministeredAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
			VaccineCode:    "FMD", VaccineType: "killed", PathogenClass: "viral", DoseCode: "fmd_adult_repeat", Sequence: 2,
		}},
	}
	overrides, _, err := campaignDueOverrides(plans, asOf, history)
	if err != nil {
		t.Fatalf("campaignDueOverrides: %v", err)
	}
	if _, found := overrides[campaignDueGoatKey("version-1", "rule-fmd-adult-repeat", "expired-repeat")]; found {
		t.Fatalf("expired repeat received a shared-drive override: %#v; readiness clamping must not extend its authored safety window", overrides)
	}
	wantBlankDue := adultCampaignStart(asOf)
	if got, found := overrides[campaignDueGoatKey("version-1", "rule-fmd-adult-w1", "blank-history")]; !found || !got.Equal(wantBlankDue) {
		t.Fatalf("blank-history due=%s found=%v, want independent normal campaign %s", got, found, wantBlankDue)
	}
}

func TestCampaignDueOverridesIgnoresPrimaryContinuationWhenAligningETTTRepeat(t *testing.T) {
	asOf := time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)
	rules := []protodomain.Rule{
		{RuleID: "rule-et-adult-w1", DoseCode: "et_tt_adult_w1", Sequence: 3, TriggerType: "manual_campaign", DueWindowDays: 7},
		{RuleID: "rule-et-adult-w2", DoseCode: "et_tt_adult_w2", Sequence: 4, TriggerType: "after_previous_completion", OffsetDays: 21, MinGapDays: 21, DueWindowDays: 7},
		{RuleID: "rule-et-revac", DoseCode: "et_tt_revac", Sequence: 5, TriggerType: "after_previous_completion", OffsetDays: 182, MinGapDays: 182, DueWindowDays: 30, Repeat: "every_n_days"},
	}
	historyDates := map[string]time.Time{
		"day-1": time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC),
		"day-2": time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC),
		"day-3": time.Date(2026, time.July, 26, 0, 0, 0, 0, time.UTC),
	}
	plans := make([]goatGenerationPlan, 0, len(historyDates))
	history := make(map[string][]domain.RecentVaccineAdministration, len(historyDates))
	for goatID, administeredAt := range historyDates {
		plans = append(plans, goatGenerationPlan{
			versionID: "version-1",
			rules:     rules,
			goat: domain.EligibleGoat{
				GoatID: goatID, LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open",
				Species: "goat", Stage: "adult", ParkID: "cpt", ShedID: "gandhi",
			},
			vaccineProfile: vaccineProfile{Code: "ET_TT", Type: "killed", PathogenClass: "bacterial", Class: immunoKilledBacterial},
		})
		history[goatID] = []domain.RecentVaccineAdministration{{
			AdministeredAt: administeredAt, VaccineCode: "ET_TT", VaccineType: "killed", PathogenClass: "bacterial",
			DoseCode: "et_tt_adult_w2", Sequence: 4,
		}}
	}

	overrides, aligned, err := campaignDueOverrides(plans, asOf, history)
	if err != nil {
		t.Fatalf("campaignDueOverrides: %v", err)
	}
	want := businessDayStart(historyDates["day-3"]).AddDate(0, 0, 182)
	for goatID := range historyDates {
		key := campaignDueGoatKey("version-1", "rule-et-revac", goatID)
		if got, ok := overrides[key]; !ok || !got.Equal(want) {
			t.Fatalf("repeat override goat=%s due=%s found=%v, want shared %s", goatID, got, ok, want)
		}
		if !aligned[key] {
			t.Fatalf("repeat override goat=%s was not marked cohort-aligned", goatID)
		}
		if _, found := overrides[campaignDueGoatKey("version-1", "rule-et-adult-w2", goatID)]; found {
			t.Fatalf("primary continuation for goat=%s must not enter recurring campaign alignment", goatID)
		}
	}
}

func TestGenerateManualCampaignForVersionCoalescesAdultPartitionCampaignDueAcrossPages(t *testing.T) {
	ctx := context.Background()
	entryEarly := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	entryLate := time.Date(2026, time.June, 28, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"adult","species":"sheep","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","under_treatment","recovering","quarantine","icu"]}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-sheep-pox", DoseCode: "sheep_pox_adult_w1", Sequence: 1, TriggerType: "manual_campaign", OffsetDays: 35, DueWindowDays: 7,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "godel-page-1", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Species: "sheep", Stage: "adult", EntryDate: &entryEarly, ShedID: "shed-godel-1", ParkID: "cpt", PartitionLabel: "Part 1"},
		{GoatID: "godel-page-2", LifecycleStatus: "alive", HealthStatus: "healthy", ReproductiveStatus: "open", Species: "sheep", Stage: "adult", EntryDate: &entryLate, ShedID: "shed-godel-1", ParkID: "cpt", PartitionLabel: "Part 1"},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)
	gen.page = 1

	result, err := gen.GenerateManualCampaignForVersion(ctx, "tenant-1", "version-1", "cpt-adult-campaign", asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	wantDue := adultCampaignStart(asOf)
	if result.Generated != 2 || len(obl.inserted) != 2 {
		t.Fatalf("result=%#v inserted=%#v, want two normal campaign obligations across pages", result, obl.inserted)
	}
	for _, inserted := range obl.inserted {
		if !inserted.DueAt.Equal(wantDue) {
			t.Fatalf("goat %s due=%s, want cross-page coalesced campaign due %s", inserted.TargetID, inserted.DueAt, wantDue)
		}
	}
}

func TestCampaignDueOverridesDoNotReplaceAdultSameVaccineHistory(t *testing.T) {
	entryEarly := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	entryLate := time.Date(2026, time.June, 28, 0, 0, 0, 0, time.UTC)
	rule := protodomain.Rule{
		RuleID: "rule-fmd-adult-w1", DoseCode: "fmd_adult_w1", Sequence: 1, TriggerType: "manual_campaign", OffsetDays: 63,
		EligibilityJSON: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"}}`),
	}
	plans := []goatGenerationPlan{
		{
			versionID: "version-1", rules: []protodomain.Rule{rule},
			goat: domain.EligibleGoat{GoatID: "godel-main", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "goat", Stage: "adult", EntryDate: &entryEarly, ShedID: "shed-godel-1", ParkID: "cpt", PartitionLabel: "Part 1"},
		},
		{
			versionID: "version-1", rules: []protodomain.Rule{rule},
			goat: domain.EligibleGoat{GoatID: "godel-history", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "goat", Stage: "adult", EntryDate: &entryLate, ShedID: "shed-godel-1", ParkID: "cpt", PartitionLabel: "Part 1"},
		},
	}
	overrides, _, err := campaignDueOverrides(plans, time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC), map[string][]domain.RecentVaccineAdministration{
		"godel-history": {{AdministeredAt: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC), VaccineCode: "FMD"}},
	})
	if err != nil {
		t.Fatalf("campaign due overrides: %v", err)
	}
	historyKey := campaignDueGoatKey("version-1", "rule-fmd-adult-w1", "godel-history")
	if _, found := overrides[historyKey]; found {
		t.Fatalf("history-backed adult goat received campaign override %#v; same-vaccine history must keep last-vaccination precedence", overrides[historyKey])
	}
	blankKey := campaignDueGoatKey("version-1", "rule-fmd-adult-w1", "godel-main")
	wantBlankDue := adultCampaignStart(time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC))
	if got, found := overrides[blankKey]; !found || !got.Equal(wantBlankDue) {
		t.Fatalf("blank-history adult override=%s found=%v, want normal campaign date %s", got, found, wantBlankDue)
	}
}

func TestGenerateForVersionSuppressesAdultSameDoseCompletedLaterOnAsOfDay(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.July, 24, 0, 0, 0, 0, time.UTC)
	entry := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	completedLaterSameDay := time.Date(2026, time.July, 24, 14, 30, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"ET_TT","type":"toxoid","pathogen_class":"bacterial"},"eligibility":{"animal_stage":"adult","species":"goat","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-ettt-adult-w2", DoseCode: "et_tt_adult_w2", Sequence: 2, TriggerType: "post_arrival", OffsetDays: 49, DueWindowDays: 7,
		}},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{
			GoatID: "done-today", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "goat", Stage: "adult", EntryDate: &entry,
			ParkID: "cpt", ShedID: "shed-godel-1", PartitionLabel: "Part 1",
		}},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"done-today": {{
				AdministeredAt: completedLaterSameDay,
				VaccineCode:    "ET_TT",
				VaccineType:    "toxoid",
				PathogenClass:  "bacterial",
				DoseCode:       "et_tt_adult_w2",
				Sequence:       2,
			}},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 0 || result.SuppressedByTrustedHistory != 1 || len(obl.inserted) != 0 {
		t.Fatalf("result=%#v inserted=%#v, want same-dose completion on as-of day suppressed", result, obl.inserted)
	}
}

func TestGenerateForVersionKeepsKidDOBTimingStrict(t *testing.T) {
	ctx := context.Background()
	olderDOB := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	youngerDOB := time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1","species":"goat","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-ppr-kid", DoseCode: "ppr_kid_16w", Sequence: 1, TriggerType: "birth_age", OffsetDays: 112, DueWindowDays: 7,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "older-kid", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "goat", Stage: "K1", DOB: &olderDOB, ShedID: "shed-godel-1", ParkID: "cpt", PartitionLabel: "Part 1"},
		{GoatID: "younger-kid", LifecycleStatus: "alive", HealthStatus: "healthy", Species: "goat", Stage: "K1", DOB: &youngerDOB, ShedID: "shed-godel-1", ParkID: "cpt", PartitionLabel: "Part 1"},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	result, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Generated != 2 || len(obl.inserted) != 2 {
		t.Fatalf("result=%#v inserted=%#v, want both kid obligations", result, obl.inserted)
	}
	got := map[string]time.Time{}
	for _, inserted := range obl.inserted {
		got[inserted.TargetID] = inserted.DueAt
	}
	if !got["older-kid"].Equal(businessDayStart(olderDOB).AddDate(0, 0, 112)) {
		t.Fatalf("older kid due=%s, want DOB strict due", got["older-kid"])
	}
	if !got["younger-kid"].Equal(businessDayStart(youngerDOB).AddDate(0, 0, 112)) {
		t.Fatalf("younger kid due=%s, want DOB strict due", got["younger-kid"])
	}
	if got["older-kid"].Equal(got["younger-kid"]) {
		t.Fatalf("kid DOB timing was coalesced: %#v", got)
	}
}

func TestGenerateForVersionHonorsVaccinationRulesSourceScheduleWithTrustedHistory(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	et4w := dob.AddDate(0, 0, 28)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","under_treatment","recovering","quarantine","icu"]},"compatibility_policy":{"live_to_killed_gap_days":14,"killed_to_killed_gap_days":14,"live_to_live_gap_days":28,"kid_booster_min_gap_days":21},"pregnancy_policy":{"allow_until_pregnancy_month":3,"skip_from_pregnancy_month":4,"skip_through_pregnancy_month":5,"post_delivery_catch_up_days":14}}`),
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

func TestGenerationScopeRequiresRealShedPlacement(t *testing.T) {
	for _, goat := range []domain.EligibleGoat{
		{GoatID: "park-only", ParkID: "park-1"},
		{GoatID: "shed-only", ShedID: "shed-1"},
		{GoatID: "no-placement"},
	} {
		if _, _, err := generationScope("tenant-1", goat); err == nil {
			t.Fatalf("generationScope(%+v) err=nil, want missing-placement failure", goat)
		}
	}

	scopeType, scopeID, err := generationScope("tenant-1", domain.EligibleGoat{
		GoatID: "placed",
		ParkID: "park-1",
		ShedID: "shed-1",
	})
	if err != nil {
		t.Fatalf("generationScope placed goat: %v", err)
	}
	if scopeType != "shed" || scopeID != "shed-1" {
		t.Fatalf("scope=%s/%s, want shed/shed-1", scopeType, scopeID)
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
				TriggerType: "after_previous_completion", OffsetDays: 21, MinGapDays: 21, DueWindowDays: 7, CatchUp: "immediate",
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
	wantDue := businessDayStart(firstAdmin).AddDate(0, 0, 21)
	if result.SuppressedByTrustedHistory != 1 || result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want first dose suppressed and booster generated", result, obl.inserted)
	}
	if got := obl.inserted[0]; got.RuleID != "rule-et-7w" || !got.DueAt.Equal(wantDue) {
		t.Fatalf("inserted=%#v, want ET+TT 21-day booster due %s", got, wantDue)
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
	if len(proto.effectiveParkID) != 2 || proto.effectiveParkID[0] != "park-a" || proto.effectiveParkID[1] != "park-1" {
		t.Fatalf("effective park IDs=%#v, want one lookup per real park scope", proto.effectiveParkID)
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
	// Set to test a park-scoped plan: versionFor then reports that scope instead of tenant.
	scopeType string
	scopeID   string
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
	scopeType := p.scopeType
	if scopeType == "" {
		scopeType = "tenant"
	}
	return protodomain.Version{
		ProtocolVersionID: versionID, Status: "published",
		ScopeType: scopeType, ScopeID: p.scopeID, RuleDsl: ruleDSL,
	}
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

func (g *generationGoatFake) ListEligibleGoatsForGeneration(_ context.Context, f domain.ImpactFilter, after string, limit int32) ([]domain.EligibleGoat, error) {
	g.filters = append(g.filters, f)
	if len(g.list) > 0 {
		start := 0
		if after != "" {
			start = len(g.list)
			for i, goat := range g.list {
				if goat.GoatID == after {
					start = i + 1
					break
				}
			}
		}
		if start >= len(g.list) {
			return nil, nil
		}
		end := len(g.list)
		if limit > 0 && start+int(limit) < end {
			end = start + int(limit)
		}
		return defaultPlacedGoats(g.list[start:end]), nil
	}
	return []domain.EligibleGoat{defaultPlacedGoat(domain.EligibleGoat{GoatID: "goat-1", LifecycleStatus: "alive"})}, nil
}

func (g *generationGoatFake) GetGoatForGeneration(context.Context, string, string) (domain.EligibleGoat, bool, error) {
	if g.goat.GoatID != "" {
		return defaultPlacedGoat(g.goat), true, nil
	}
	return defaultPlacedGoat(domain.EligibleGoat{GoatID: "goat-1", LifecycleStatus: "alive"}), true, nil
}

func defaultPlacedGoats(in []domain.EligibleGoat) []domain.EligibleGoat {
	out := make([]domain.EligibleGoat, len(in))
	for i, goat := range in {
		out[i] = defaultPlacedGoat(goat)
	}
	return out
}

func defaultPlacedGoat(goat domain.EligibleGoat) domain.EligibleGoat {
	if goat.ParkID == "" {
		goat.ParkID = "park-1"
	}
	if goat.ShedID == "" {
		goat.ShedID = "shed-1"
	}
	return goat
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

func (g *generationGoatFake) LastRecentVaccineAdministrationsForGoats(_ context.Context, _ string, goatIDs []string, before time.Time) (map[string]domain.RecentVaccineAdministration, error) {
	out := make(map[string]domain.RecentVaccineAdministration, len(goatIDs))
	for _, id := range goatIDs {
		if admin, ok := g.lastVaccine[id]; ok {
			if !admin.AdministeredAt.IsZero() && admin.AdministeredAt.After(before) {
				continue
			}
			out[id] = admin
		}
	}
	return out, nil
}

func (g *generationGoatFake) RecentVaccineAdministrationsForGoats(_ context.Context, _ string, goatIDs []string, before time.Time) (map[string][]domain.RecentVaccineAdministration, error) {
	out := make(map[string][]domain.RecentVaccineAdministration, len(goatIDs))
	for _, id := range goatIDs {
		if admins, ok := g.vaccineHistory[id]; ok {
			for _, admin := range admins {
				if !admin.AdministeredAt.IsZero() && admin.AdministeredAt.After(before) {
					continue
				}
				out[id] = append(out[id], admin)
			}
			continue
		}
		if admin, ok := g.lastVaccine[id]; ok {
			if !admin.AdministeredAt.IsZero() && admin.AdministeredAt.After(before) {
				continue
			}
			out[id] = []domain.RecentVaccineAdministration{admin}
		}
	}
	return out, nil
}

type generationObligationFake struct {
	reconcileByIdentity    map[string]obldomain.ObligationRef
	reconciledIdentities   []string
	carriedOverGoats       [][]string
	carriedOverVersions    [][]string
	carryOverCount         int
	seen                   map[string]bool
	keyIndex               map[string]int
	inserted               []obldomain.NewObligation
	deferredKeys           []string
	deferReasons           []string
	reopenedKeys           []string
	realignedKeys          []string
	canceledKeys           []string
	canceledVersions       []string
	canceledExceptVersions [][]string
	cancelReasons          []string
	cancelReasonsByKey     map[string]string // Track cancellation reason for each key
	nearbyDrive            *time.Time
	nearestBatchLookups    []nearestBatchLookup
	failOnceAfterInserted  int
	failErr                error
	failed                 bool
	recordedStatusEvents   []obldomain.NewStatusEvent
}

type nearestBatchLookup struct {
	ruleID      string
	vaccineCode string
	shedID      string
	parkID      string
}

func (o *generationObligationFake) InsertObligation(ctx context.Context, in obldomain.NewObligation) (string, bool, error) {
	if o.seen == nil {
		o.seen = map[string]bool{}
	}
	if o.keyIndex == nil {
		o.keyIndex = map[string]int{}
	}
	if o.cancelReasonsByKey == nil {
		o.cancelReasonsByKey = map[string]string{}
	}
	if o.seen[in.IdempotencyKey] {
		return "obligation-1", false, nil
	}
	// The database refuses a repeat cycle whose CAUSE already has an open row, whatever key
	// it arrives under. Without that here, a moved due date would look like a fresh insert
	// and the fake would quietly disagree with production about the one behaviour that
	// matters most in this area.
	if in.RepeatCycle.Valid() {
		if _, found, _ := o.OpenObligationForRepeatCycle(ctx, in.TenantID, in.ProtocolVersionID, in.RuleID, in.TargetType, in.TargetID, in.Sequence, in.RepeatCycle.SourceRef); found {
			return "obligation-1", false, nil
		}
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

func (o *generationObligationFake) InsertDeferredObligation(ctx context.Context, in obldomain.NewObligation, _ string, occurredAt time.Time) (string, bool, error) {
	id, applied, err := o.InsertObligation(ctx, in)
	if err == nil && applied {
		o.recordedStatusEvents = append(o.recordedStatusEvents, obldomain.NewStatusEvent{
			TenantID: in.TenantID, ObligationID: id, EventType: "deferred", OccurredAt: occurredAt,
		})
	}
	return id, applied, err
}

func (o *generationObligationFake) DeferOpenObligationForGeneration(_ context.Context, _, idempotencyKey, reason string, _ time.Time) (obldomain.ObligationRef, bool, error) {
	idx, ok := o.keyIndex[idempotencyKey]
	if !ok {
		return obldomain.ObligationRef{}, false, errors.New("obligation not found")
	}
	if o.cancelReasonsByKey == nil {
		o.cancelReasonsByKey = map[string]string{}
	}
	switch o.inserted[idx].Status {
	case "deferred", "completed", "waived", "canceled", "superseded":
		return obldomain.ObligationRef{ObligationID: "obligation-1", Status: o.inserted[idx].Status, DueAt: o.inserted[idx].DueAt, Reason: o.cancelReasonsByKey[idempotencyKey]}, false, nil
	}
	o.inserted[idx].Status = "deferred"
	o.deferredKeys = append(o.deferredKeys, idempotencyKey)
	o.deferReasons = append(o.deferReasons, reason)
	return obldomain.ObligationRef{ObligationID: "obligation-1", Status: "deferred", DueAt: o.inserted[idx].DueAt, Reason: ""}, true, nil
}

func (o *generationObligationFake) ReopenDeferredObligationForGeneration(_ context.Context, _, idempotencyKey string, _ time.Time, reschedule *obldomain.RecoveryReschedule) (obldomain.ObligationRef, bool, error) {
	idx, ok := o.keyIndex[idempotencyKey]
	if !ok {
		return obldomain.ObligationRef{}, false, nil
	}
	if o.cancelReasonsByKey == nil {
		o.cancelReasonsByKey = map[string]string{}
	}
	if o.inserted[idx].Status != "deferred" {
		return obldomain.ObligationRef{ObligationID: "obligation-1", Status: o.inserted[idx].Status, DueAt: o.inserted[idx].DueAt, Reason: o.cancelReasonsByKey[idempotencyKey]}, false, nil
	}
	o.inserted[idx].Status = "scheduled"
	if reschedule != nil {
		o.inserted[idx].DueAt = reschedule.DueAt
		o.inserted[idx].WindowStart = &reschedule.WindowStart
		o.inserted[idx].WindowEnd = reschedule.WindowEnd
	}
	o.reopenedKeys = append(o.reopenedKeys, idempotencyKey)
	return obldomain.ObligationRef{ObligationID: "obligation-1", Status: "scheduled", DueAt: o.inserted[idx].DueAt, Reason: ""}, true, nil
}

func (o *generationObligationFake) RealignOpenObligationForGeneration(_ context.Context, _, idempotencyKey string, dueAt time.Time, windowEnd *time.Time, _ time.Time) (obldomain.ObligationRef, bool, error) {
	idx, ok := o.keyIndex[idempotencyKey]
	if !ok {
		return obldomain.ObligationRef{}, false, nil
	}
	status := o.inserted[idx].Status
	if (status != "scheduled" && status != "due") || o.inserted[idx].DueAt.Equal(dueAt) {
		return obldomain.ObligationRef{ObligationID: "obligation-1", Status: status, DueAt: o.inserted[idx].DueAt}, false, nil
	}
	o.inserted[idx].DueAt = dueAt
	o.inserted[idx].WindowStart = &dueAt
	o.inserted[idx].WindowEnd = windowEnd
	o.realignedKeys = append(o.realignedKeys, idempotencyKey)
	return obldomain.ObligationRef{ObligationID: "obligation-1", Status: status, DueAt: dueAt}, true, nil
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

func (o *generationObligationFake) CancelOpenObligationByIdempotencyKey(_ context.Context, _, idempotencyKey, reason string, _ time.Time) (string, bool, error) {
	idx, ok := o.keyIndex[idempotencyKey]
	if !ok {
		return "", false, nil
	}
	if o.cancelReasonsByKey == nil {
		o.cancelReasonsByKey = map[string]string{}
	}
	switch o.inserted[idx].Status {
	case "completed", "canceled", "superseded", "waived":
		return "obligation-1", false, nil
	}
	o.inserted[idx].Status = "canceled"
	o.cancelReasonsByKey[idempotencyKey] = reason
	o.canceledKeys = append(o.canceledKeys, idempotencyKey)
	return "obligation-1", true, nil
}

// Returns every animal asked about, so the generation tests exercise the supersede path
// rather than silently skipping it.
// Finds an open row by the cause it descends from, exactly as the database does: the
// production suppression is by cause, so a fake that could not answer this would let the
// stale-survivor bug pass unnoticed.
func (o *generationObligationFake) OpenObligationForRepeatCycle(_ context.Context, _, versionID, ruleID, targetType, targetID string, sequence int32, sourceRef string) (obldomain.ObligationRef, bool, error) {
	for i := range o.inserted {
		row := o.inserted[i]
		if row.RepeatCycle == nil || row.RepeatCycle.SourceRef != sourceRef {
			continue
		}
		if row.ProtocolVersionID != versionID || row.RuleID != ruleID ||
			row.TargetType != targetType || row.TargetID != targetID || row.Sequence != sequence {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(row.Status)) {
		case "scheduled", "due", "in_progress", "deferred":
		default:
			continue
		}
		return obldomain.ObligationRef{
			ObligationID:   fmt.Sprintf("obligation-%d", i+1),
			Status:         row.Status,
			DueAt:          row.DueAt,
			IdempotencyKey: row.IdempotencyKey,
		}, true, nil
	}
	return obldomain.ObligationRef{}, false, nil
}

func (o *generationObligationFake) GoatsWithVaccinationObligationsOutsideVersions(_ context.Context, _ string, goatIDs, _ []string) ([]string, error) {
	return goatIDs, nil
}

// The fake mirrors the real reconcile: an identity the fake has already seen is treated as the
// animal's existing work and moved, never inserted beside itself.
func (o *generationObligationFake) ReconcileOpenObligationForRuleIdentity(_ context.Context, _ string, in obldomain.NewObligation, _ time.Time) (obldomain.ObligationRef, bool, error) {
	if o.reconcileByIdentity == nil {
		return obldomain.ObligationRef{}, false, nil
	}
	ref, ok := o.reconcileByIdentity[in.RuleIdentityKey]
	if !ok {
		return obldomain.ObligationRef{}, false, nil
	}
	o.reconciledIdentities = append(o.reconciledIdentities, in.RuleIdentityKey)
	return ref, true, nil
}

func (o *generationObligationFake) CarryOverUnchangedVaccinationObligations(_ context.Context, _ string, goatIDs, versionIDs []string) (int, error) {
	o.carriedOverGoats = append(o.carriedOverGoats, append([]string(nil), goatIDs...))
	o.carriedOverVersions = append(o.carriedOverVersions, append([]string(nil), versionIDs...))
	return o.carryOverCount, nil
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

func (o *generationObligationFake) RecordStatusEvent(_ context.Context, ev obldomain.NewStatusEvent) (string, bool, error) {
	o.recordedStatusEvents = append(o.recordedStatusEvents, ev)
	return "event-1", true, nil
}

func (o *generationObligationFake) NextSuccessorSuffix(_ context.Context, _, _ string) (int, error) {
	return 1, nil
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
	goat := defaultPlacedGoat(domain.EligibleGoat{GoatID: "imported-goat", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1"})
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
	goat := defaultPlacedGoat(domain.EligibleGoat{GoatID: "fresh-kid", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1"})
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
	goat := defaultPlacedGoat(domain.EligibleGoat{GoatID: "enrolled-kid", DOB: &dob, LifecycleStatus: "alive", Species: "goat", Stage: "K1"})
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
	goat := defaultPlacedGoat(domain.EligibleGoat{GoatID: "partial-history-goat", LifecycleStatus: "alive", Species: "goat"})
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
	obl.failed = false            // reset failure flag
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
		GoatID:          "adult-goat",
		LifecycleStatus: "alive",
		EntryDate:       &entryDate,
		Species:         "goat",
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

// TestGenerateReplayTerminalObligationDoesNotDelayCoDueVaccine is the R2-01 follow-up guard: a
// TERMINAL persisted obligation (waived/completed/superseded — no remaining shot) must NOT space an
// unrelated co-due vaccine on replay. Goat Pox must stay on its own recomputed date, not be pushed
// out by a PPR obligation that is no longer planned.
func TestGenerateReplayTerminalObligationDoesNotDelayCoDueVaccine(t *testing.T) {
	for _, terminal := range []string{"waived", "completed", "superseded", "canceled"} {
		t.Run(terminal, func(t *testing.T) {
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

			if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
				t.Fatalf("pass 1: %v", err)
			}
			// PPR reaches a terminal state (given, waived, replaced, or canceled) -- no upcoming shot.
			obl.inserted[0].Status = terminal
			// Inject the REAL producer reason string written by generation's history
			// reconciliation (generation.go CancelOpenObligationByIdempotencyKey call site):
			// "vaccine_history_outranks_anchor" is terminal — must never mint a successor.
			if obl.cancelReasonsByKey == nil {
				obl.cancelReasonsByKey = make(map[string]string)
			}
			baseKey := obl.inserted[0].IdempotencyKey
			obl.cancelReasonsByKey[baseKey] = "vaccine_history_outranks_anchor"

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
				t.Fatalf("Goat Pox was not generated: inserted=%#v", obl.inserted)
			}
			ownDue := businessDayStart(businessDayStart(dob).AddDate(0, 0, 30))
			if !goatPox.DueAt.Equal(ownDue) {
				t.Fatalf("Goat Pox due = %s, want its own date %s (a %s PPR must not delay it)", goatPox.DueAt.Format("2006-01-02"), ownDue.Format("2006-01-02"), terminal)
			}
		})
	}
}

// TestGenerateRecoveryReplayTerminalObligationDoesNotInventSpacingDate is the R2-01 concurrency/
// recovery guard: recovery alignment is only a proposal. If the persisted obligation is already
// terminal, reopening is a no-op and that proposal must not be treated as an upcoming vaccination.
// Otherwise a waived PPR invents an August 1 anchor and delays co-due Goat Pox to August 29.
func TestGenerateRecoveryReplayTerminalObligationDoesNotInventSpacingDate(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	ruleA := protodomain.Rule{
		RuleID: "ppr", DoseCode: "ppr_primary", Sequence: 1, TriggerType: "birth_age", OffsetDays: 30, DueWindowDays: 7,
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`),
	}
	ruleB := protodomain.Rule{
		RuleID: "goatpox", DoseCode: "goatpox_primary", Sequence: 1, TriggerType: "birth_age", OffsetDays: 30, DueWindowDays: 7,
		EligibilityJSON: []byte(`{"vaccine":{"code":"GOAT_POX","type":"live","pathogen_class":"viral"}}`),
	}
	goat := defaultPlacedGoat(domain.EligibleGoat{GoatID: "goat-1", LifecycleStatus: "alive", DOB: &dob, Species: "goat"})
	obl := &generationObligationFake{}
	svc := NewGenerationService(&generationProtoFake{}, &generationGoatFake{}, obl)
	policies := genVersionPolicies{Compatibility: genCompatibilityPolicy{LiveToLiveGapDays: 28}}

	if err := svc.genOneGoat(ctx, "tenant-1", "version-1", []protodomain.Rule{ruleA}, nil, genEligibility{}, goat, asOf,
		generationOptions{}, policies, vaccineProfile{}, nil, newTrustedEvidenceLookup(), &domain.GenerateResult{}); err != nil {
		t.Fatalf("initial PPR generation: %v", err)
	}
	if len(obl.inserted) != 1 {
		t.Fatalf("initial obligations = %#v, want one PPR", obl.inserted)
	}
	obl.inserted[0].Status = "waived"
	nearby := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	obl.nearbyDrive = &nearby

	if err := svc.genOneGoat(ctx, "tenant-1", "version-1", []protodomain.Rule{ruleA, ruleB}, nil, genEligibility{}, goat, asOf,
		generationOptions{healthRecoveryAlign: true}, policies, vaccineProfile{}, nil, newTrustedEvidenceLookup(), &domain.GenerateResult{}); err != nil {
		t.Fatalf("recovery replay: %v", err)
	}
	var goatPox *obldomain.NewObligation
	for i := range obl.inserted {
		if obl.inserted[i].RuleID == "goatpox" {
			goatPox = &obl.inserted[i]
		}
	}
	if goatPox == nil {
		t.Fatalf("Goat Pox was not generated: %#v", obl.inserted)
	}
	want := businessDayStart(dob).AddDate(0, 0, 30)
	if !goatPox.DueAt.Equal(want) {
		t.Fatalf("Goat Pox due = %s, want %s; terminal PPR recovery proposal must not create spacing", goatPox.DueAt.Format("2006-01-02"), want.Format("2006-01-02"))
	}
}

// Generation's history-driven repeat path must name the administration that caused the cycle.
// This is the writer half of repeat-cycle identity: without it the row is identified by a due
// date that moves every time a newer administration lands, which is how one cycle came to be
// minted twice a day apart 207 times in the staging baseline.
func TestGenerationStampsTheAdministrationThatCausedTheRepeat(t *testing.T) {
	ctx := context.Background()
	administered := time.Date(2026, time.January, 6, 9, 30, 0, 471_000_000, time.UTC)
	asOf := time.Date(2026, time.October, 12, 6, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"adult","species":"goat","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-fmd-repeat", DoseCode: "fmd_adult_repeat", Sequence: 2,
			TriggerType: "after_previous_completion", OffsetDays: 274, DueWindowDays: 30, Repeat: "every_n_days",
		}},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{
			GoatID: "repeat-goat", LifecycleStatus: "alive", HealthStatus: "healthy",
			ReproductiveStatus: "open", Species: "goat", Stage: "adult", ShedID: "shed-1", ParkID: "cpt",
		}},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"repeat-goat": {{
				AdministeredAt: administered, VaccineCode: "FMD", VaccineType: "killed",
				PathogenClass: "viral", DoseCode: "fmd_adult_repeat", Sequence: 2,
			}},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("generate: %v", err)
	}
	// Sub-second administration times are the case the two writers disagreed on: RFC3339
	// keeps fractional seconds, the SQL that reconstructs this reference does not. Asserting
	// with a whole-second administration would pass either way and prove nothing.
	var stamped int
	for _, inserted := range obl.inserted {
		if inserted.Status == "canceled" || inserted.RuleID != "rule-fmd-repeat" {
			continue
		}
		rc := inserted.RepeatCycle
		if rc == nil {
			t.Fatal("repeat obligation carries no cause: it is identified by a due date that moves")
		}
		// The same string the completion path writes for this administration, and the same
		// one the repair job reconstructs. Diverge and one cycle becomes two open rows.
		if want := "fmd|2026-01-06T09:30:00Z|2"; rc.SourceRef != want {
			t.Fatalf("cause = %q, want %q -- the reference is lowercased and whole-second, "+
				"because the repair job reconstructs it from SQL and must produce the same bytes", rc.SourceRef, want)
		}
		if rc.Source != obldomain.RepeatCycleSourceTrustedHistory {
			t.Fatalf("source = %q, want %q", rc.Source, obldomain.RepeatCycleSourceTrustedHistory)
		}
		stamped++
	}
	if stamped != 1 {
		t.Fatalf("stamped %d repeat obligations, want 1", stamped)
	}
}

// The duplicate is gone -- but the move must still land somewhere.
//
// A repeat cycle is suppressed by its CAUSE, so the surviving row can be one another writer
// created under a different idempotency key -- a booster-minted successor, most often, since
// completion mints the next cycle the moment a dose is recorded. Every reconciliation in
// generation is keyed, so a pass that keeps using the key it just computed reconciles
// nothing: no duplicate, but a row frozen on the booster's date, never realigned to the
// drive, never deferred for a sick animal, never reopened for a recovered one. Suppression is
// not the same as being finished.
func TestGenerationMovesTheSurvivingRepeatCycleInsteadOfLeavingItStale(t *testing.T) {
	ctx := context.Background()
	administered := time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"adult","species":"goat","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-fmd-repeat", DoseCode: "fmd_adult_repeat", Sequence: 2,
			TriggerType: "after_previous_completion", OffsetDays: 274, DueWindowDays: 30, Repeat: "every_n_days",
		}},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{
			GoatID: "repeat-goat", LifecycleStatus: "alive", HealthStatus: "healthy",
			ReproductiveStatus: "open", Species: "goat", Stage: "adult", ShedID: "shed-1", ParkID: "cpt",
		}},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"repeat-goat": {{
				AdministeredAt: administered, VaccineCode: "FMD", VaccineType: "killed",
				PathogenClass: "viral", DoseCode: "fmd_adult_repeat", Sequence: 2,
			}},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	// The successor the completion path already minted for this cause, on ITS date and under
	// ITS key -- five days off what generation is about to compute.
	cause := obldomain.RepeatCycleRef("FMD", administered, 2)
	boosterDue := businessDayStart(administered).AddDate(0, 0, 269)
	anchorID := "obligation-that-was-given"
	if _, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID: "tenant-1", ProtocolVersionID: "version-1", RuleID: "rule-fmd-repeat",
		TargetType: "goat", TargetID: "repeat-goat", ScopeType: "shed", ScopeID: "shed-1",
		DueAt: boosterDue, Status: "scheduled", IdempotencyKey: "booster:next-cycle", Sequence: 2,
		RepeatCycle: &obldomain.RepeatCycleSource{
			Source: obldomain.RepeatCycleSourceTrustedHistory, SourceRef: cause,
			AnchorObligationID: &anchorID, AnchorAt: &administered, DueAt: &boosterDue,
		},
	}); err != nil || !applied {
		t.Fatalf("seed booster successor: applied=%v err=%v", applied, err)
	}

	gen := NewGenerationService(proto, goats, obl)
	asOf := time.Date(2026, time.September, 1, 6, 0, 0, 0, time.UTC)
	if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(obl.inserted) != 1 {
		t.Fatalf("generation created a second row for a cycle that already existed: %d rows", len(obl.inserted))
	}
	got := obl.inserted[0]
	if got.IdempotencyKey != "booster:next-cycle" {
		t.Fatalf("survivor key = %q, want the booster-minted row", got.IdempotencyKey)
	}
	want := businessDayStart(administered).AddDate(0, 0, 274)
	if !got.DueAt.Equal(want) {
		t.Fatalf("survivor due %s, want %s -- the recomputed date never reached the row it suppressed against", got.DueAt, want)
	}
}

// A park override has to retire the plan it replaces, same as a tenant plan does.
//
// The effective-version map was filled only for tenant-scoped runs. A park-scoped run left it
// empty, so the plan-replacement sweep saw no effective version for that park and skipped --
// and the previous plan's open work sat beside the new plan's for exactly the animals a park
// override exists to move. Nothing failed; the lists just showed both.
func TestParkScopedGenerationStillSupersedesTheWorkItReplaces(t *testing.T) {
	ctx := context.Background()
	parkGoatDOB := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"adult","species":"goat","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-fmd-adult", DoseCode: "fmd_adult_w1", Sequence: 1,
			TriggerType: "birth_age", OffsetDays: 63, DueWindowDays: 30, Repeat: "none",
		}},
		// This park's effective plan is the override being generated. Anything else the animals
		// still hold is the plan it replaced.
		effectiveVersions: []string{"version-park-override"},
		scopeType:         "park",
		scopeID:           "cpt",
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{
			GoatID: "park-goat", LifecycleStatus: "alive", HealthStatus: "healthy",
			ReproductiveStatus: "open", Species: "goat", Stage: "adult", ShedID: "shed-1", ParkID: "cpt",
			DOB: &parkGoatDOB,
		}},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-park-override",
		time.Date(2026, time.September, 1, 6, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("generate for park-scoped version: %v", err)
	}

	if len(obl.canceledExceptVersions) == 0 {
		t.Fatal("a park-scoped run superseded nothing: the plan it replaced keeps its open work forever")
	}
	got := obl.canceledExceptVersions[0]
	if len(got) != 1 || got[0] != "version-park-override" {
		t.Fatalf("kept effective versions = %#v, want only the park's own override", got)
	}
	if obl.cancelReasons[0] != "protocol_version_replaced" {
		t.Fatalf("cancel reason = %q, want protocol_version_replaced", obl.cancelReasons[0])
	}
}

// A held animal's repeat dose must actually BE held, even after its plan is replaced.
//
// The row under generation's own key is canceled by the replacement, the cycle exists again
// under another writer's key, and the animal is sick. Reconciling the key just computed
// would reconcile nothing and leave that existing row SCHEDULED: work an operator sees as
// due, on an animal that is not fit for it.
//
// This drives the main generation path, which is the one that reaches this state. The same
// by-cause resolution exists in insertSuccessorForCanceledGenerationReplay, and is
// deliberately belt-and-braces: with the main path resolving first I could not construct an
// input that reaches the successor branch, so that copy is defensive rather than proven.
func TestHeldGoatKeepsItsExistingRepeatCycleDeferredAfterPlanReplacement(t *testing.T) {
	ctx := context.Background()
	administered := time.Date(2026, time.January, 6, 9, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.September, 1, 6, 0, 0, 0, time.UTC)
	newProto := func() *generationProtoFake {
		return &generationProtoFake{
			ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"adult","species":"goat","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any"}}`),
			rules: []protodomain.Rule{{
				RuleID: "rule-fmd-repeat", DoseCode: "fmd_adult_repeat", Sequence: 2,
				TriggerType: "after_previous_completion", OffsetDays: 274, DueWindowDays: 30, Repeat: "every_n_days",
			}},
		}
	}
	goat := domain.EligibleGoat{
		GoatID: "held-goat", LifecycleStatus: "alive", HealthStatus: "healthy",
		ReproductiveStatus: "open", Species: "goat", Stage: "adult", ShedID: "shed-1", ParkID: "cpt",
	}
	history := map[string][]domain.RecentVaccineAdministration{
		"held-goat": {{
			AdministeredAt: administered, VaccineCode: "FMD", VaccineType: "killed",
			PathogenClass: "viral", DoseCode: "fmd_adult_repeat", Sequence: 2,
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{goat}, vaccineHistory: history}
	obl := &generationObligationFake{seen: map[string]bool{}}

	// 1. A healthy pass, so the row exists under GENERATION's own key.
	if _, err := NewGenerationService(newProto(), goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if len(obl.inserted) != 1 {
		t.Fatalf("first pass inserted %d rows, want 1", len(obl.inserted))
	}
	generationKey := obl.inserted[0].IdempotencyKey

	// 2. That row is canceled by a plan replacement -- a reason that MINTS a successor, which
	//    is the only way into the replay path this test exists to cover.
	obl.inserted[0].Status = "canceled"
	if obl.cancelReasonsByKey == nil {
		obl.cancelReasonsByKey = map[string]string{}
	}
	obl.cancelReasonsByKey[generationKey] = "protocol_version_replaced"

	// 3. Meanwhile the cycle exists again, SCHEDULED, under another writer's key.
	cause := obldomain.RepeatCycleRef("FMD", administered, 2)
	anchorAt := administered
	if _, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID: "tenant-1", ProtocolVersionID: "version-1", RuleID: "rule-fmd-repeat",
		TargetType: "goat", TargetID: "held-goat", ScopeType: "shed", ScopeID: "shed-1",
		DueAt: businessDayStart(administered).AddDate(0, 0, 269), Status: "scheduled",
		IdempotencyKey: "booster:next-cycle", Sequence: 2,
		RepeatCycle: &obldomain.RepeatCycleSource{
			Source: obldomain.RepeatCycleSourceTrustedHistory, SourceRef: cause, AnchorAt: &anchorAt,
		},
	}); err != nil || !applied {
		t.Fatalf("seed the existing scheduled cycle: applied=%v err=%v", applied, err)
	}

	// 4. The animal is now held, and generation runs again.
	goats.list = []domain.EligibleGoat{{
		GoatID: "held-goat", LifecycleStatus: "alive", HealthStatus: "sick",
		ReproductiveStatus: "open", Species: "goat", Stage: "adult", ShedID: "shed-1", ParkID: "cpt",
	}}
	if _, err := NewGenerationService(newProto(), goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf); err != nil {
		t.Fatalf("held pass: %v", err)
	}

	var survivor *obldomain.NewObligation
	for i := range obl.inserted {
		if obl.inserted[i].IdempotencyKey == "booster:next-cycle" {
			survivor = &obl.inserted[i]
		}
	}
	if survivor == nil {
		t.Fatal("the pre-existing cycle row disappeared")
	}
	if survivor.Status != "deferred" {
		t.Fatalf("the existing repeat dose is still %q for a sick animal; it must be held", survivor.Status)
	}
	if len(obl.inserted) != 2 {
		t.Fatalf("rows = %d, want the canceled original and the one surviving cycle", len(obl.inserted))
	}
}

// Carry-over must run BEFORE supersede, and against the same effective-version list.
//
// The order is what makes "add a sixth vaccine, the other five stay put" work. Superseding first
// would cancel the unchanged work before anything could rebind it; rebinding after generation
// would collide with obligation_instances_dup_guard, because generation would already hold that
// key. This test pins the sequence at the seam where a future refactor would most easily lose it.
func TestGenerationCarriesOverBeforeItSupersedes(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"adult","species":"goat","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-fmd-adult", DoseCode: "fmd_adult_w1", Sequence: 1,
			TriggerType: "birth_age", OffsetDays: 63, DueWindowDays: 30, Repeat: "none",
		}},
		effectiveVersions: []string{"version-2"},
		scopeType:         "park",
		scopeID:           "cpt",
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{
			GoatID: "carry-goat", LifecycleStatus: "alive", HealthStatus: "healthy",
			ReproductiveStatus: "open", Species: "goat", Stage: "adult", ShedID: "shed-1", ParkID: "cpt",
			DOB: &dob,
		}},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)

	if _, err := gen.GenerateForVersion(ctx, "tenant-1", "version-2",
		time.Date(2026, time.September, 1, 6, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(obl.carriedOverVersions) == 0 {
		t.Fatal("carry-over never ran: every publish would cancel and re-mint the whole plan again")
	}
	if len(obl.canceledExceptVersions) == 0 {
		t.Fatal("supersede never ran: work whose rule really did change would survive forever")
	}
	if got := obl.carriedOverVersions[0]; len(got) != 1 || got[0] != "version-2" {
		t.Fatalf("carry-over effective versions = %#v, want only the version being generated", got)
	}
	if got := obl.carriedOverGoats[0]; len(got) != 1 || got[0] != "carry-goat" {
		t.Fatalf("carry-over goats = %#v, want the park's animals", got)
	}
	// Both sweeps must agree on what "effective" means, or one would rebind work the other cancels.
	if got, want := obl.canceledExceptVersions[0], obl.carriedOverVersions[0]; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("supersede kept %#v but carry-over targeted %#v: the two sweeps disagree", got, want)
	}
}
