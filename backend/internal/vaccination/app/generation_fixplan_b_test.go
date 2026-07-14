package app

import (
	"context"
	"testing"
	"time"

	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// This file covers the vaccination GENERATION-rule fixes in
// docs/runbooks/vaccination-seed-audit-and-fix-plan-2026-07-14.md, Fix Plan section B (B1-B8),
// against the approved schedule matrix (docs/preventive-care-vaccination/APPROVED-SCHEDULE-MATRIX.md,
// docs/preventive-care-vaccination/vaccination-rules.md). B6 (clinical defer/recovery) and the
// version/eligibility-obsolescence half of B7 already have dedicated coverage elsewhere in
// generation_test.go (TestGenerateForVersionDefaultsClinicalHoldStatesToDeferred,
// TestGoatRecheckHandlerReopensOnHealthRecovery, TestGenerateForGoatCancelsOpenWorkForNoLongerEffectiveVersions,
// TestGenerateForGoatCancelsOpenWorkWhenEligibilityNoLongerMatches) and are not duplicated here.

// B1: each vaccine's next dose comes from its OWN latest accepted administration, never the
// earliest reachable one. FMD is "single + repeat": next due = latest FMD administration + 9
// months (274 days), even when an earlier FMD administration exists in the same history and is
// listed AFTER the latest one in the slice (proves explicit max-selection, not first-match/order
// dependence — the confirmed bug: 285/308 goats anchored to the first dose instead of the latest).
func TestFMDRevacAnchorsToLatestNotEarliestAdministration(t *testing.T) {
	ctx := context.Background()
	earliestFMD := time.Date(2024, time.March, 20, 0, 0, 0, 0, time.UTC)
	latestFMD := time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"adult"}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-fmd-revac", DoseCode: "fmd_revac", Sequence: 1,
			TriggerType: "after_previous_completion", Repeat: "every_n_days",
			OffsetDays: 274, MinGapDays: 274, DueWindowDays: 30, CatchUp: "next_cycle",
		}},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{GoatID: "goat-901007000503822", LifecycleStatus: "alive", Stage: "adult"}},
		// Latest administration listed FIRST here but the code must not assume slice order —
		// latestVaccineCompletion selects max(administered_at) explicitly.
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"goat-901007000503822": {
				{AdministeredAt: latestFMD, VaccineCode: "FMD", VaccineType: "killed", PathogenClass: "viral", DoseCode: "fmd_revac", Sequence: 1},
				{AdministeredAt: earliestFMD, VaccineCode: "FMD", VaccineType: "killed", PathogenClass: "viral", DoseCode: "fmd_revac", Sequence: 1},
			},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate FMD revac: %v", err)
	}
	wantDue := businessDayStart(latestFMD).AddDate(0, 0, 274)
	if result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want one FMD revac obligation", result, obl.inserted)
	}
	if got := obl.inserted[0].DueAt; !got.Equal(wantDue) {
		t.Fatalf("FMD revac due=%s, want latest-administration + 9 months = %s (not earliest + 9 months)", got, wantDue)
	}
}

// B2 (anchor fallback): DOB unknown but the goat already has an accepted administration of this
// rule's own vaccine — per-vaccine continuation (B1) already owns the next due date via the
// vaccine's after_previous_completion/revac rule. The birth_age primary rule instance must be
// silently suppressed (SuppressedByTrustedHistory), NEVER a missing_due_date deferral (confirmed
// bug #3: 531 animals, ~1,994 doses wrongly deferred despite having vaccination history).
func TestNoDOBWithVaccineHistoryContinuesPerVaccineNotDeferred(t *testing.T) {
	ctx := context.Background()
	lastFMD := time.Date(2025, time.September, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{}}`),
		rules: []protodomain.Rule{
			{
				RuleID: "rule-fmd-12w", DoseCode: "fmd_kid_12w", Sequence: 1,
				TriggerType: "birth_age", OffsetDays: 84, DueWindowDays: 7, CatchUp: "immediate",
			},
			{
				RuleID: "rule-fmd-revac", DoseCode: "fmd_revac", Sequence: 2,
				TriggerType: "after_previous_completion", Repeat: "every_n_days",
				OffsetDays: 274, MinGapDays: 274, DueWindowDays: 30, CatchUp: "next_cycle",
			},
		},
	}
	goats := &generationGoatFake{
		// Stage="K1" keeps this goat on the kid schedule path so the birth_age rule is even
		// considered (B4's DOB-unknown routing needs a kid signal); DOB itself stays nil to
		// exercise B2's anchor fallback.
		list: []domain.EligibleGoat{{GoatID: "goat-no-dob-history", LifecycleStatus: "alive", Stage: "K1"}},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"goat-no-dob-history": {
				{AdministeredAt: lastFMD, VaccineCode: "FMD", VaccineType: "killed", PathogenClass: "viral", DoseCode: "fmd_kid_12w", Sequence: 1},
			},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate no-DOB with history: %v", err)
	}
	if result.SkippedNoDueDate != 0 || result.Deferred != 0 {
		t.Fatalf("result=%#v, want zero skipped/deferred — a vaccination anchor exists", result)
	}
	if result.SuppressedByTrustedHistory != 1 {
		t.Fatalf("result=%#v, want the birth_age primary suppressed by existing vaccine history (B2)", result)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want exactly the revac continuation generated", result, obl.inserted)
	}
	if got := obl.inserted[0]; got.RuleID != "rule-fmd-revac" {
		t.Fatalf("inserted=%#v, want the per-vaccine revac continuation, not a fabricated kid_12w gap", got)
	}
	wantDue := businessDayStart(lastFMD).AddDate(0, 0, 274)
	if !obl.inserted[0].DueAt.Equal(wantDue) {
		t.Fatalf("due=%s, want latest FMD administration + 9 months = %s", obl.inserted[0].DueAt, wantDue)
	}
}

// B3: DOB unknown AND the vaccine was never received (no history at all) must route to the adult
// catch-up/primary path at the next compatible drive — never a fabricated kid_12w/kid_16w
// obligation, deferred or otherwise. Only the adult (post_arrival) wave for the same vaccine may
// materialize; the kid (birth_age) rule for the never-received vaccine must produce nothing.
func TestNoDOBNeverReceivedVaccineRoutesAdultCatchUpNoKidDose(t *testing.T) {
	ctx := context.Background()
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"},"eligibility":{}}`),
		rules: []protodomain.Rule{
			{
				RuleID: "rule-ppr-16w", DoseCode: "ppr_kid_16w", Sequence: 1,
				TriggerType: "birth_age", OffsetDays: 112, DueWindowDays: 7, CatchUp: "immediate",
			},
			{
				RuleID: "rule-ppr-adult-w1", DoseCode: "ppr_adult_w1", Sequence: 2,
				TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 7, CatchUp: "immediate",
			},
		},
	}
	goats := &generationGoatFake{
		// No stage tag, no DOB, no entry date, no vaccination history at all: the 257-animal
		// blank-origin/no-DOB/no-arrival cohort from the audit.
		list: []domain.EligibleGoat{{GoatID: "goat-never-vaccinated", LifecycleStatus: "alive"}},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate no-DOB never-vaccinated goat: %v", err)
	}
	if result.SkippedNoDueDate != 0 || result.Deferred != 0 {
		t.Fatalf("result=%#v, want zero skipped/deferred — B3 never defers for a missing anchor", result)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want exactly one adult catch-up obligation", result, obl.inserted)
	}
	got := obl.inserted[0]
	if got.RuleID != "rule-ppr-adult-w1" {
		t.Fatalf("inserted=%#v, want the adult catch-up wave, NEVER a kid_16w obligation", got)
	}
	if !got.DueAt.Equal(businessDayStart(asOf)) {
		t.Fatalf("due=%s, want catch-up due at the next compatible drive (today)", got.DueAt)
	}
}

// B4: a NEW kid course may only START through 16 weeks. An animal long past the 20-week finishing
// window with no kid-management stage tag gets NO new kid course at all — the confirmed defect
// (186 kid doses scheduled onto adults; origin_type="birth" previously bypassed age with no upper
// bound whatsoever).
func TestKidPastCutoffGetsNoNewKidCourse(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	asOf := dob.AddDate(0, 0, 400) // well over a year old
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-et-4w", DoseCode: "et_tt_kid_4w", Sequence: 1,
			TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7, CatchUp: "immediate",
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "old-adult-born-on-farm", LifecycleStatus: "alive", DOB: &dob, OriginType: "birth", Stage: "adult"},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate past-cutoff goat: %v", err)
	}
	if result.Generated != 0 || len(obl.inserted) != 0 {
		t.Fatalf("result=%#v inserted=%#v, want NO kid obligation for an animal long past the cutoff", result, obl.inserted)
	}
}

// B4: a kid already in course (DOB known, age within the 16-20w finishing window) DOES receive its
// spacing-shifted 20-week dose (e.g. Goat Pox derived to 20w after a 16w PPR dose per the approved
// matrix's live-live gap rule) — the finishing half of B4, distinct from the "no new course past
// 16w" half proven above.
func TestInCourseKidFinishesTwentyWeekDose(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	asOf := dob.AddDate(0, 0, 140) // exactly 20 weeks: the Goat Pox 20w-derived due point
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"eligibility":{}}`),
		rules: []protodomain.Rule{{
			RuleID: "rule-goatpox-20w", DoseCode: "goat_pox_kid_20w", Sequence: 1,
			TriggerType: "birth_age", OffsetDays: 140, DueWindowDays: 7, CatchUp: "immediate",
		}},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "in-course-kid", LifecycleStatus: "alive", DOB: &dob, Stage: "adult"},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate in-course finishing dose: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want the 20w finishing dose generated", result, obl.inserted)
	}
	if got := obl.inserted[0]; got.RuleID != "rule-goatpox-20w" || !got.DueAt.Equal(businessDayStart(dob).AddDate(0, 0, 140)) {
		t.Fatalf("inserted=%#v, want the spacing-shifted 20w dose on schedule", got)
	}
}

// B5: procurement primary is recognized ONLY from a real 2-visit administration record.
// origin_type="procured" ALONE must not mark the primary complete — with no administration record,
// the adult two-visit course schedules normally (both waves), exactly as it would for any other
// procured goat. Nothing in schedulePathForGoat/dueAt/genOneGoat inspects OriginType for
// completion-suppression; only real evidence/history can suppress a wave.
func TestProcuredOriginAloneDoesNotCompletePrimary(t *testing.T) {
	ctx := context.Background()
	entry := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"GOAT_POX","type":"live","pathogen_class":"viral"},"eligibility":{}}`),
		rules: []protodomain.Rule{
			{RuleID: "rule-wave-1", DoseCode: "goat_pox_adult_w1", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 7},
			{RuleID: "rule-wave-2", DoseCode: "goat_pox_adult_w2", Sequence: 2, TriggerType: "post_arrival", OffsetDays: 35, DueWindowDays: 7},
		},
	}
	goats := &generationGoatFake{list: []domain.EligibleGoat{
		{GoatID: "procured-no-record", LifecycleStatus: "alive", OriginType: "procured", EntryDate: &entry},
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate procured no-record goat: %v", err)
	}
	// Both waves materialize (SM-1 schedules the whole future course up front, same as any other
	// goat) — origin_type="procured" alone suppresses NEITHER wave, proving completion is never
	// inferred from the flag.
	if result.Generated != 2 || len(obl.inserted) != 2 {
		t.Fatalf("result=%#v inserted=%#v, want both adult waves scheduled — origin_type=procured alone must not suppress/complete either", result, obl.inserted)
	}
	for _, got := range obl.inserted {
		if got.Status != "scheduled" {
			t.Fatalf("inserted=%#v, want every wave scheduled normally, none pre-completed by origin_type alone", got)
		}
	}
}

// B5 (with record): a REAL 2-visit administration record suppresses both procurement primary
// waves (evidence-backed, not origin_type-backed) and the vaccine's revac rule continues correctly
// from the second (latest) recorded visit (B1 composed with B5).
func TestProcuredWithRecordedTwoVisitPrimarySuppressesAndContinuesToBoosters(t *testing.T) {
	ctx := context.Background()
	entry := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	visit1Due := businessDayStart(entry).AddDate(0, 0, 7)
	visit2 := time.Date(2026, time.February, 5, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"GOAT_POX","type":"live","pathogen_class":"viral"},"eligibility":{}}`),
		rules: []protodomain.Rule{
			{RuleID: "rule-wave-1", DoseCode: "goat_pox_adult_w1", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 7, DueWindowDays: 7},
			{RuleID: "rule-wave-2", DoseCode: "goat_pox_adult_w2", Sequence: 2, TriggerType: "post_arrival", OffsetDays: 35, DueWindowDays: 7},
			{
				RuleID: "rule-revac", DoseCode: "goat_pox_revac", Sequence: 3,
				TriggerType: "after_previous_completion", Repeat: "yearly", DueWindowDays: 30, CatchUp: "next_cycle",
			},
		},
	}
	visit2Due := businessDayStart(entry).AddDate(0, 0, 35)
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{GoatID: "procured-with-record", LifecycleStatus: "alive", OriginType: "procured", EntryDate: &entry}},
		trustedByDue: map[string]bool{
			// Both real visit dates are trusted evidence — the actual 2-visit record required by
			// B5, not the origin_type flag.
			visit1Due.UTC().Format(time.RFC3339Nano): true,
			visit2Due.UTC().Format(time.RFC3339Nano): true,
		},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"procured-with-record": {
				{AdministeredAt: visit2, VaccineCode: "GOAT_POX", VaccineType: "live", PathogenClass: "viral", DoseCode: "goat_pox_adult_w2", Sequence: 2},
			},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate procured with-record goat: %v", err)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want only the revac continuation generated", result, obl.inserted)
	}
	if got := obl.inserted[0]; got.RuleID != "rule-revac" {
		t.Fatalf("inserted=%#v, want the booster/revac continuation, not a re-issued primary wave", got)
	}
	wantDue := businessDayStart(visit2).AddDate(1, 0, 0)
	if !obl.inserted[0].DueAt.Equal(wantDue) {
		t.Fatalf("due=%s, want revac due one year after the real second-visit record %s", obl.inserted[0].DueAt, wantDue)
	}
}

// B7: imported vaccination history triggers recomputation. A goat first generates a B3 no-DOB
// adult catch-up placeholder (no history yet); once a real administration for that vaccine is
// imported/recorded, the very next generation pass must supersede the no-history placeholder and
// continue per-vaccine (B1/B2) instead of leaving a stale duplicate.
func TestImportedVaccinationHistorySupersedesNoHistoryCatchUp(t *testing.T) {
	ctx := context.Background()
	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"FMD","type":"killed","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1"}}`),
		rules: []protodomain.Rule{
			{
				RuleID: "rule-fmd-12w", DoseCode: "fmd_kid_12w", Sequence: 1,
				TriggerType: "birth_age", OffsetDays: 84, DueWindowDays: 7, CatchUp: "immediate",
			},
			{
				RuleID: "rule-fmd-revac", DoseCode: "fmd_revac", Sequence: 2,
				TriggerType: "after_previous_completion", Repeat: "every_n_days",
				OffsetDays: 274, MinGapDays: 274, DueWindowDays: 30, CatchUp: "next_cycle",
			},
		},
	}
	goats := &generationGoatFake{goat: domain.EligibleGoat{
		GoatID: "goat-1", LifecycleStatus: "alive", Stage: "K1",
	}}
	obl := &generationObligationFake{seen: map[string]bool{}}
	gen := NewGenerationService(proto, goats, obl)
	asOf := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	first, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", asOf)
	if err != nil {
		t.Fatalf("initial generate (no history): %v", err)
	}
	if first.Generated != 1 || len(obl.inserted) != 1 || obl.inserted[0].Status != "scheduled" {
		t.Fatalf("first result=%#v inserted=%#v, want one no-history catch-up placeholder", first, obl.inserted)
	}

	// Vaccination history is imported/recorded for this goat.
	importedAt := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	goats.vaccineHistory = map[string][]domain.RecentVaccineAdministration{
		"goat-1": {{AdministeredAt: importedAt, VaccineCode: "FMD", VaccineType: "killed", PathogenClass: "viral", DoseCode: "fmd_kid_12w", Sequence: 1}},
	}
	second, err := gen.GenerateForGoat(ctx, "tenant-1", "goat-1", asOf.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("recompute after history import: %v", err)
	}
	if second.Generated != 1 || len(obl.inserted) != 2 {
		t.Fatalf("second result=%#v inserted=%#v, want one new per-vaccine continuation after import", second, obl.inserted)
	}
	if obl.inserted[0].Status != "canceled" || len(obl.canceledKeys) != 1 {
		t.Fatalf("no-history placeholder status=%s canceled=%#v, want it superseded by the imported history", obl.inserted[0].Status, obl.canceledKeys)
	}
	wantDue := businessDayStart(importedAt).AddDate(0, 0, 274)
	if got := obl.inserted[1]; got.RuleID != "rule-fmd-revac" || !got.DueAt.Equal(wantDue) {
		t.Fatalf("inserted=%#v, want the revac continuation anchored to the imported administration %s", got, wantDue)
	}
}
