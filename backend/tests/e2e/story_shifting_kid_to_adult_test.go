package e2e

import (
	"testing"
	"time"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// TestKernelStory_ShiftingPromotesKidToAdult is the production-path proof for the 2026-08-05
// maintainer rule: a goat's KID/ADULT classification is a property of the cohort tag it carries, and
// it changes when — and only when — a shifting changes that tag.
//
// The defect this closes: nothing in backend/internal/** ever wrote goats.age_band. It was set once
// at seed/import and never again, so a kid moved into an adult shed adopted the adult cohort tag
// while its age_band kept saying "kid" forever, and the Herd Register kept counting it as a kid.
//
// The rule is NOT age-derived, and the fixture is built to prove that: the mover is deliberately old
// enough (60 weeks) that any DOB-based promotion would already call it an adult, yet it starts as a
// kid because its COHORT says so — exactly like the 667 F2-Male/F2-Female fattening animals in the
// live CBE/CPT herd, which the farm classifies as kids at up to 67 weeks of age.
//
//	PRODUCER  counts.CompleteShiftingEvent -> identity.RelocateGoatsToShedInTx
//	          • reads the destination cohort's age_band from animal_stage_lookup (the tenant's
//	            editable stage vocabulary — the band is CONFIG, not a Go constant)
//	          • writes shed_id + management_stage + age_band in ONE UPDATE, so an animal can never
//	            sit in an adult cohort while still counted as a kid
//
// Three cases, because the interesting behaviour is what it declines to do:
//
//	kid  -> adult cohort   : reclassifies (K3 -> Non-Pregnant, age_band kid -> adult)
//	kid  -> kid cohort     : tag changes, band does NOT (K3 -> K2, still kid)
//	kid  -> unclassified   : neither changes (a clinical cohort must never reclassify an animal)
//
// The payoff assertion is the Herd Register: its kid/adult split is maintained by a trigger on
// goats.age_band, so the counts move as a consequence of the production write, not of anything this
// test seeds.
func TestKernelStory_ShiftingPromotesKidToAdult(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-shifting-kid-to-adult",
		"Shifting promotes a kid to an adult: kid/adult follows the cohort tag",
		"A goat's kid/adult band is a property of the cohort tag it carries, held in the tenant's "+
			"animal_stage_lookup vocabulary. When an approved shifting is COMPLETED, the same transaction "+
			"that moves the animal and stamps the destination cohort also stamps that cohort's kid/adult "+
			"band — so a kid that joins an adult cohort stops being counted as a kid. A move between two "+
			"kid cohorts changes the tag only, and a move whose destination cohort is deliberately "+
			"unclassified (ICU, Quarantine) changes neither.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx

	// The counts shifting producer wired to the REAL identity relocation adapter, exactly as
	// internal/bootstrap/api.go wires it (WithIdentityTxWriter).
	shiftRepo := countspg.NewRepository(fx.Pool, 10*time.Second).
		WithIdentityTxWriter(identitypg.NewRepository(fx.Pool, 10*time.Second))

	const (
		kidShed    = "5f000000-0000-4000-8000-000000000101" // K3, weaned kids
		adultShed  = "5f000000-0000-4000-8000-000000000102" // Non-Pregnant, an adult breeding cohort
		kid2Shed   = "5f000000-0000-4000-8000-000000000103" // K2, still a kid cohort
		icuShed    = "5f000000-0000-4000-8000-000000000104" // ICU, deliberately unclassified
		promoted   = "5f000000-0000-4000-8000-000000000110"
		sideways   = "5f000000-0000-4000-8000-000000000111"
		clinical   = "5f000000-0000-4000-8000-000000000112"
		stageK3    = "5f000000-0000-4000-8000-0000000001a3"
		stageNonPr = "5f000000-0000-4000-8000-0000000001a4"
		stageK2    = "5f000000-0000-4000-8000-0000000001a2"
		stageICU   = "5f000000-0000-4000-8000-0000000001a5"
	)

	story.Step("The tenant's stage vocabulary declares which cohorts are kid and which are adult",
		"animal_stage_lookup.age_band is the authority. K2/K3 are kid cohorts, Non-Pregnant is an adult "+
			"breeding cohort, and ICU carries NO band at all — a clinical placement must not reclassify an "+
			"animal. This is config a farm can edit, not a rule compiled into the backend.")
	seedCohortShed(fx, kidShed, "E2E-KA-KID", stageK3, "K3", "kid")
	seedCohortShed(fx, adultShed, "E2E-KA-ADULT", stageNonPr, "Non-Pregnant", "adult")
	seedCohortShed(fx, kid2Shed, "E2E-KA-KID2", stageK2, "K2", "kid")
	seedCohortShed(fx, icuShed, "E2E-KA-ICU", stageICU, "ICU", "")

	completedAt := time.Date(2026, 8, 4, 9, 0, 0, 0, biztime.DefaultLocation())
	// 60 weeks old: OLDER than the 20-week vaccination kid cutoff on purpose. If kid/adult were
	// derived from age, this animal would already be an adult. It is a kid because its cohort is.
	dob := completedAt.AddDate(0, 0, -420)

	for _, goatID := range []string{promoted, sideways, clinical} {
		// External input fact only: the animal arrives already classified by its current cohort, which
		// is what the source import produces. The band is set on the INTAKE insert, never by a later
		// UPDATE -- every band change asserted below is written by the production relocation path.
		fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: kidShed, Stage: "K3", DOB: &dob, AgeBand: "kid"})
	}

	kidsBefore := fx.countRows(
		`SELECT count(*) FROM herd_register_goat_projection WHERE tenant_id=$1 AND is_kid AND goat_id = ANY($2::uuid[])`,
		fxTenant, []string{promoted, sideways, clinical})
	story.Assert("all three animals start as kids in the Herd Register", kidsBefore == 3, "kid_count=%d", kidsBefore)

	story.Step("A kid moved into an adult cohort is reclassified by the completion itself",
		"The approved K3 -> Non-Pregnant shifting is completed. RelocateGoatsToShedInTx writes shed_id, "+
			"management_stage and age_band in the same UPDATE, so the animal cannot land in the adult shed "+
			"still counted as a kid.")
	promoteEvent := recordAndApproveShifting(t, ctx, shiftRepo, "ka-promote", []string{promoted}, kidShed, adultShed, completedAt)
	promoteResult, _, promoteErr := completeShiftingE2E(shiftRepo, ctx, "ka-promote", promoteEvent, "Non-Pregnant", completedAt)
	story.Assert("completion succeeded", promoteErr == nil, "err=%v", promoteErr)
	story.Assert("completion applied the movement",
		promoteResult.EventStatus == countsdomain.ShiftingEventStatusApplied, "status=%s", promoteResult.EventStatus)

	promotedStage := fx.scanText(`SELECT COALESCE(management_stage,'') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, promoted)
	promotedBand := fx.scanText(`SELECT COALESCE(age_band,'') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, promoted)
	story.Assert("the animal adopted the destination adult cohort", promotedStage == "Non-Pregnant", "management_stage=%s", promotedStage)
	story.Assert("and is no longer a kid — age_band moved with the cohort", promotedBand == "adult", "age_band=%s", promotedBand)

	story.Step("The Herd Register kid/adult split follows, without anything recomputing it by hand",
		"herd_register_goat_projection is maintained by a trigger on goats.age_band, so the production "+
			"write is what moves the count. This is the miscount the rule exists to fix: before age_band was "+
			"maintained, this animal stayed on the kid side of the register forever.")
	promotedIsKid := fx.countRows(
		`SELECT count(*) FROM herd_register_goat_projection WHERE tenant_id=$1 AND goat_id=$2 AND is_kid`, fxTenant, promoted)
	story.Assert("the promoted animal now counts as an adult in the Herd Register", promotedIsKid == 0, "is_kid rows=%d", promotedIsKid)

	// The COUNTS surface, not just the per-animal row: herd_register_summary_projection is the
	// aggregate the register renders. The destination shed must now show one adult and no kids, and
	// the source shed must have lost that kid — a promotion that moved the detail row but not the
	// summary would be the classic "projection and its rollup disagree" defect.
	destAdults := fx.countRows(
		`SELECT COALESCE(sum(adult_count),0) FROM herd_register_summary_projection WHERE tenant_id=$1 AND current_location_id=$2`, fxTenant, adultShed)
	destKids := fx.countRows(
		`SELECT COALESCE(sum(kid_count),0) FROM herd_register_summary_projection WHERE tenant_id=$1 AND current_location_id=$2`, fxTenant, adultShed)
	srcKids := fx.countRows(
		`SELECT COALESCE(sum(kid_count),0) FROM herd_register_summary_projection WHERE tenant_id=$1 AND current_location_id=$2`, fxTenant, kidShed)
	story.Assert("the destination shed's summary counts one adult", destAdults == 1, "adult_count=%d", destAdults)
	story.Assert("and counts no kids", destKids == 0, "kid_count=%d", destKids)
	story.Assert("the source kid shed is down to the two animals that stayed", srcKids == 2, "kid_count=%d", srcKids)

	story.Step("A move between two KID cohorts changes the tag and nothing else",
		"K3 -> K2 is a real reclassification — feed and the vaccination generator both key on "+
			"management_stage — but both cohorts are kid cohorts, so the band must not move. This is the "+
			"case a 'stage changed, therefore reclassify' shortcut gets wrong.")
	sidewaysEvent := recordAndApproveShifting(t, ctx, shiftRepo, "ka-sideways", []string{sideways}, kidShed, kid2Shed, completedAt)
	_, _, sidewaysErr := completeShiftingE2E(shiftRepo, ctx, "ka-sideways", sidewaysEvent, "K2", completedAt)
	story.Assert("completion succeeded", sidewaysErr == nil, "err=%v", sidewaysErr)

	sidewaysStage := fx.scanText(`SELECT COALESCE(management_stage,'') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, sideways)
	sidewaysBand := fx.scanText(`SELECT COALESCE(age_band,'') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, sideways)
	story.Assert("the cohort tag changed", sidewaysStage == "K2", "management_stage=%s", sidewaysStage)
	story.Assert("the animal is still a kid", sidewaysBand == "kid", "age_band=%s", sidewaysBand)

	story.Step("A destination cohort with no declared band reclassifies nothing",
		"ICU and Quarantine carry NULL age_band on purpose: moving a sick animal into a clinical shed is "+
			"a placement decision, never a statement about its age class. The relocation preserves the "+
			"animal's existing band rather than blanking it.")
	// The clinical tag is rejected before it can be stamped (ErrClinicalDestinationTag), so the move is
	// driven as a keep-current relocation — which is what the raise-time resolver produces for these
	// sheds anyway. Either way the animal must come out of it still a kid.
	clinicalEvent := recordAndApproveShifting(t, ctx, shiftRepo, "ka-clinical", []string{clinical}, kidShed, icuShed, completedAt)
	_, _, clinicalErr := completeShiftingE2E(shiftRepo, ctx, "ka-clinical", clinicalEvent, "", completedAt)
	story.Assert("completion succeeded", clinicalErr == nil, "err=%v", clinicalErr)

	clinicalShed := fx.scanText(`SELECT shed_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, clinical)
	clinicalStage := fx.scanText(`SELECT COALESCE(management_stage,'') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, clinical)
	clinicalBand := fx.scanText(`SELECT COALESCE(age_band,'') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, clinical)
	story.Assert("the animal moved into the clinical shed", clinicalShed == icuShed, "shed_id=%s", clinicalShed)
	story.Assert("its cohort tag was preserved", clinicalStage == "K3", "management_stage=%s", clinicalStage)
	story.Assert("and so was its kid/adult band", clinicalBand == "kid", "age_band=%s", clinicalBand)
}

// seedCohortShed creates a shed whose configured profile points at a stage-vocabulary row carrying an
// explicit kid/adult band. ageBand "" seeds the row with NULL age_band, which is how the clinical
// cohorts (ICU, Quarantine) are declared: present in the vocabulary, deliberately unclassified.
func seedCohortShed(f *Fixture, shedID, shedCode, stageID, stageCode, ageBand string) {
	f.T.Helper()
	f.exec("cohort shed location",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', $3, $3, $4, 'active')`,
		shedID, fxTenant, shedCode, fxPark)
	f.exec("cohort stage lookup",
		`INSERT INTO animal_stage_lookup (animal_stage_id, tenant_id, stage_code, name, status, age_band)
		 VALUES ($1, $2, $3, $3, 'active', NULLIF($4::text, ''))`,
		stageID, fxTenant, stageCode, ageBand)
	f.exec("cohort shed profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'mixed', 500)`,
		shedID, fxTenant, stageID)
	f.exec("cohort shed operational attributes",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, true, false, false)`,
		fxTenant, shedID)
}
