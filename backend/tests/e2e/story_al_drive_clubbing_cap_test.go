package e2e

import (
	"fmt"
	"testing"
	"time"

	calendardomain "github.com/vgoats/goatos/backend/internal/calendar/domain"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryAL_DriveClubbingHonorsShotCapAndThreeWeekCourse is the production E2E guard for
// the failure class the smaller unit tests cannot prove alone: a 7-week kid-course visit has ET+TT
// and PPR due together, while a lower-priority vaccine also competes for the same animals. The
// sweeper must maximize the drive without violating MaxShotsPerAnimalPerDrive=2: ET+TT+PPR stay on
// the planned drive, the overflow vaccine moves to the next feasible safe day, and nothing is
// dropped or backdated.
func TestKernelStoryAL_DriveClubbingHonorsShotCapAndThreeWeekCourse(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-al", "Drive clubbing: 3-week course plus shot cap",
		"Three kids reach the seven-week course date. ET+TT #2 and PPR are both valid on the drive day, "+
			"while FMD also competes for the same visit. SM-4 must keep the higher-priority ET+TT/PPR pair "+
			"together, overflow FMD to the next safe day because of the two-shot cap, and still render the "+
			"calendar from the planned batch dates.")
	defer story.Finish()
	story.Certify("backend kernel + Calendar projection")

	const (
		shedID = "ab000000-0000-4000-8000-000000000001"
		stage  = "ab000000-0000-4000-8000-000000000002"
		goatA  = "ab000000-0000-4000-8000-000000000010"
		goatB  = "ab000000-0000-4000-8000-000000000011"
		goatC  = "ab000000-0000-4000-8000-000000000012"
	)
	fx.SeedShed(shedID, "E2E-AL", stage)

	type storyALDose struct {
		code          string
		vaccine       string
		priority      int32
		compatibility string
	}
	doses := []storyALDose{
		{code: "ettt_2", vaccine: "ET+TT", priority: 1, compatibility: "ET+TT+PPR"},
		{code: "ppr", vaccine: "PPR", priority: 2, compatibility: "ET+TT+PPR"},
		{code: "fmd", vaccine: "FMD", priority: 5, compatibility: "FMD+HS"},
	}
	versionByDose := make(map[string]string, len(doses))
	ruleByDose := make(map[string]string, len(doses))
	for _, dose := range doses {
		versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_al."+dose.code, "{}", []RuleSpec{{
			DoseCode: dose.code, Sequence: dose.priority, TriggerType: "birth_age", OffsetDays: 49, DueWindowDays: 7,
			EligibilityJSON: fmt.Sprintf(
				`{"vaccine":{"code":%q,"priority":%d,"compatibility_group":%q},"eligibility":{"animal_stage":"K1"}}`,
				dose.vaccine, dose.priority, dose.compatibility),
		}})
		versionByDose[dose.code] = versionID
		ruleByDose[dose.code] = ruleIDs[dose.code]
	}

	dueDay := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	dob := dueDay.AddDate(0, 0, -49)
	for _, goatID := range []string{goatA, goatB, goatC} {
		fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob, OriginType: "birth"})
	}

	story.Step("Generate competing course obligations",
		"Run the production generation service over the published protocol. Each kid gets ET+TT #2, "+
			"PPR, and FMD due on the same seven-week day, all safe through the next week.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	totalGenerated := 0
	for _, dose := range doses {
		genRes, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionByDose[dose.code], dueDay)
		story.Assert(fmt.Sprintf("%s generation ran without error", dose.code), err == nil, "err=%v", err)
		story.Assert(fmt.Sprintf("%s generation created one obligation per goat", dose.code), err == nil && genRes.Generated == 3,
			"generated=%d", genRes.Generated)
		totalGenerated += genRes.Generated
	}
	total := fx.countRows(`
SELECT count(*) FROM obligation_instances
WHERE tenant_id=$1
  AND protocol_version_id IN ($2::uuid, $3::uuid, $4::uuid)
  AND status='scheduled'`, fxTenant, versionByDose["ettt_2"], versionByDose["ppr"], versionByDose["fmd"])
	story.Assert("all nine competing obligations exist before sweep", total == 9, "scheduled=%d", total)
	story.Assert("all nine obligations were generated through production code", totalGenerated == 9, "generated=%d", totalGenerated)

	story.Step("Preflight and sweep through the production snapshot path",
		"The write-free cap preflight must pass because priorities are resolved. The real sweep then "+
			"uses the same snapshot/HWM path as production so partial cap leftovers cannot be stranded.")
	plans := make([]oblapp.SweepVersionPriority, 0, len(doses))
	for _, dose := range doses {
		cfg := defaultParkSweepConfig()
		cfg.VaccineCode = dose.vaccine
		cfg.DrivePlanner = obldomain.DrivePlannerSettings{
			Enabled:                   true,
			MaxShotsPerAnimalPerDrive: 2,
			MaxBatchingHoldDays:       7,
			MaxBatchingHoldCount:      1,
		}
		cfg.RuleVaccineIDs = map[string]oblapp.RuleVaccineIdentity{
			ruleByDose[dose.code]: {
				VaccineCode:      dose.vaccine,
				VaccinePriority:  dose.priority,
				CompatibilityGrp: dose.compatibility,
			},
		}
		plans = append(plans, oblapp.SweepVersionPriority{VersionID: versionByDose[dose.code], Config: cfg})
	}
	plans = oblapp.SortSweepVersionsByPriority(plans)
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, nil)
	dueBefore := dueDay.AddDate(0, 0, 7)
	snapshot, err := sweeper.PreflightVisitShotCapTiesWithSnapshotAsOf(fx.Ctx, fxTenant, plans, dueDay, dueBefore, time.Time{})
	story.Assert("shot-cap preflight accepts the resolved ET+TT/PPR/FMD priorities", err == nil, "err=%v", err)
	session := oblapp.NewSweepSession()
	totalAttached := 0
	totalBatches := 0
	for _, plan := range plans {
		sweepRes, err := sweeper.SweepVersionWithSessionNoFinalizeSnapshotAsOf(fx.Ctx, fxTenant, plan.VersionID, plan.Config, dueDay, dueBefore, session, time.Time{}, snapshot)
		story.Assert("snapshot sweep ran without error", err == nil, "version=%s err=%v", plan.VersionID, err)
		totalAttached += sweepRes.Obligations
		totalBatches += sweepRes.Batches
	}
	story.Assert("every obligation was attached despite cap overflow", totalAttached == 9, "obligations=%d", totalAttached)
	story.Assert("planned batches were produced", totalBatches > 0, "batches=%d", totalBatches)
	for _, plan := range plans {
		err := sweeper.FinalizePlannedBatches(fx.Ctx, fxTenant, plan.VersionID, plan.Config)
		story.Assert("finalization after cap-safe planning succeeds", err == nil, "version=%s err=%v", plan.VersionID, err)
	}

	story.Step("Assert the cap outcome per animal",
		"Every goat gets exactly two shots on Aug 10. ET+TT and PPR stay together; FMD moves one day "+
			"later and still remains inside the authored seven-day safe window.")
	wantPrimary := biztime.BusinessDate(dueDay)
	wantOverflow := biztime.BusinessDate(dueDay.AddDate(0, 0, 1))
	for _, goatID := range []string{goatA, goatB, goatC} {
		primaryCount := fx.countRows(`
SELECT count(*)
FROM obligation_instances oi
JOIN obligation_batches ob ON ob.tenant_id=oi.tenant_id AND ob.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2 AND oi.protocol_version_id=$3
  AND to_char(ob.planned_date AT TIME ZONE 'Asia/Kolkata', 'YYYY-MM-DD')=$4`,
			fxTenant, goatID, versionByDose["ettt_2"], wantPrimary)
		primaryCount += fx.countRows(`
SELECT count(*)
FROM obligation_instances oi
JOIN obligation_batches ob ON ob.tenant_id=oi.tenant_id AND ob.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2 AND oi.protocol_version_id=$3
  AND to_char(ob.planned_date AT TIME ZONE 'Asia/Kolkata', 'YYYY-MM-DD')=$4`,
			fxTenant, goatID, versionByDose["ppr"], wantPrimary)
		primaryCount += fx.countRows(`
SELECT count(*)
FROM obligation_instances oi
JOIN obligation_batches ob ON ob.tenant_id=oi.tenant_id AND ob.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2 AND oi.protocol_version_id=$3
  AND to_char(ob.planned_date AT TIME ZONE 'Asia/Kolkata', 'YYYY-MM-DD')=$4`,
			fxTenant, goatID, versionByDose["fmd"], wantPrimary)
		story.Assert(fmt.Sprintf("%s has exactly two shots on the primary drive day", goatID), primaryCount == 2,
			"goat=%s primary_count=%d", goatID, primaryCount)
		for _, doseCode := range []string{"ettt_2", "ppr"} {
			planned := plannedDateForStoryAL(fx, goatID, ruleByDose[doseCode])
			story.Assert(fmt.Sprintf("%s %s stays on primary drive", goatID, doseCode), planned == wantPrimary,
				"goat=%s dose=%s planned=%s want=%s", goatID, doseCode, planned, wantPrimary)
		}
		fmdPlanned := plannedDateForStoryAL(fx, goatID, ruleByDose["fmd"])
		story.Assert(fmt.Sprintf("%s FMD overflows off the capped visit", goatID), fmdPlanned == wantOverflow,
			"goat=%s fmd_planned=%s want=%s", goatID, fmdPlanned, wantOverflow)
	}
	unsafe := fx.countRows(`
SELECT count(*)
FROM obligation_instances oi
JOIN obligation_batches ob ON ob.tenant_id=oi.tenant_id AND ob.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.protocol_version_id=$2
  AND (ob.planned_date AT TIME ZONE 'Asia/Kolkata')::date > (oi.window_end AT TIME ZONE 'Asia/Kolkata')::date`,
		fxTenant, versionByDose["ettt_2"])
	unsafe += fx.countRows(`
SELECT count(*)
FROM obligation_instances oi
JOIN obligation_batches ob ON ob.tenant_id=oi.tenant_id AND ob.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.protocol_version_id=$2
  AND (ob.planned_date AT TIME ZONE 'Asia/Kolkata')::date > (oi.window_end AT TIME ZONE 'Asia/Kolkata')::date`,
		fxTenant, versionByDose["ppr"])
	unsafe += fx.countRows(`
SELECT count(*)
FROM obligation_instances oi
JOIN obligation_batches ob ON ob.tenant_id=oi.tenant_id AND ob.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.protocol_version_id=$2
  AND (ob.planned_date AT TIME ZONE 'Asia/Kolkata')::date > (oi.window_end AT TIME ZONE 'Asia/Kolkata')::date`,
		fxTenant, versionByDose["fmd"])
	story.Assert("no capped overflow batch is outside the safe window", unsafe == 0, "unsafe=%d", unsafe)

	story.Step("Read through Calendar",
		"Calendar must show the same planned execution dates. The primary date is not a one-animal "+
			"micro-drive; any one-animal leftover is only the cap-created overflow day.")
	events, err := fx.Calendar.ListEvents(fx.Ctx, calendardomain.Query{
		TenantID: fxTenant, OwnerKey: calendardomain.OwnerAll,
		DateFrom: dueDay.AddDate(0, 0, -1), DateTo: dueDay.AddDate(0, 0, 2),
		Limit: 20, Scope: calendardomain.ScopeFilter{TenantWide: true},
	})
	story.Assert("Calendar read succeeded", err == nil, "err=%v", err)
	primaryEvents := 0
	primaryMinTargets := 999
	overflowEvents := 0
	for _, item := range events.Items {
		switch biztime.BusinessDate(item.DueAt) {
		case wantPrimary:
			primaryEvents++
			if item.TargetCount < primaryMinTargets {
				primaryMinTargets = item.TargetCount
			}
		case wantOverflow:
			overflowEvents++
		}
	}
	story.Assert("Calendar has primary drive events", primaryEvents > 0, "primary_events=%d", primaryEvents)
	story.Assert("primary drive events are not one-animal leftovers", primaryMinTargets >= 3,
		"primary_min_targets=%d", primaryMinTargets)
	story.Assert("Calendar includes the cap overflow drive", overflowEvents > 0, "overflow_events=%d", overflowEvents)
}

func plannedDateForStoryAL(fx *Fixture, goatID, ruleID string) string {
	fx.T.Helper()
	return fx.scanText(`
SELECT to_char(ob.planned_date AT TIME ZONE 'Asia/Kolkata', 'YYYY-MM-DD')
FROM obligation_instances oi
JOIN obligation_batches ob ON ob.tenant_id=oi.tenant_id AND ob.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2 AND oi.rule_id=$3`, fxTenant, goatID, ruleID)
}
