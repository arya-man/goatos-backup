package e2e

import (
	"fmt"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryAN_DriveDateOverrideReplansEligibleAnimalCapacity is the regression bridge for
// the CEO/admin vaccine-date move flow. Separate stories prove the clinical defer rules and the
// operator animal-cap planner; this story proves they still compose when one vaccine is moved.
func TestKernelStoryAN_DriveDateOverrideReplansEligibleAnimalCapacity(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-an", "Moved vaccine date replans eligible animals through operator cap",
		"Five goats start due for ET+TT and PPR. Before planning, one goat becomes sick and one goat moves "+
			"into an ICU shed, so only three animals remain eligible. Admin then moves PPR off the original "+
			"drive date. The real sweeper must keep ET+TT on the original day, move only PPR to the override "+
			"day, and split assignment rows by eligible animal slots under operator cap without counting "+
			"the deferred goats or multiplying by dose cells.")
	defer story.Finish()
	story.Certify("backend kernel")
	story.Certify("operator-cap planner")

	const (
		shedID    = "ad000000-0000-4000-8000-000000000001"
		icuShedID = "ad000000-0000-4000-8000-000000000002"
		stageID   = "ad000000-0000-4000-8000-00000000000a"
		goodA     = "ad000000-0000-4000-8000-000000000010"
		goodB     = "ad000000-0000-4000-8000-000000000011"
		goodC     = "ad000000-0000-4000-8000-000000000012"
		sickGoat  = "ad000000-0000-4000-8000-000000000013"
		icuGoat   = "ad000000-0000-4000-8000-000000000014"
		opA       = "ad000000-0000-4000-8000-000000000101"
		opB       = "ad000000-0000-4000-8000-000000000102"
		opC       = "ad000000-0000-4000-8000-000000000103"
	)
	fx.SeedShed(shedID, "E2E-AN-Gandhi 1", stageID)
	fx.SeedICUShed(icuShedID, "E2E-AN-ICU", stageID)
	seedVaccinationOperatorsAN(fx, opA, opB, opC)
	assertAvailableOperatorsAN(t, fx, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC), []string{opA, opB, opC})
	assertAvailableOperatorsAN(t, fx, time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC), []string{opB, opC})
	assertAvailableOperatorsAN(t, fx, time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC), []string{opA, opB})
	assertAvailableOperatorsAN(t, fx, time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC), []string{opA, opC})

	type dose struct {
		key      string
		vaccine  string
		priority int32
	}
	doses := []dose{
		{key: "ettt", vaccine: "ET+TT", priority: 1},
		{key: "ppr", vaccine: "PPR", priority: 2},
	}
	versionByDose := map[string]string{}
	ruleByDose := map[string]string{}
	for _, d := range doses {
		versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_an."+d.key, "{}", []RuleSpec{{
			DoseCode: d.key, Sequence: d.priority, TriggerType: "birth_age", OffsetDays: 0, DueWindowDays: 7,
			EligibilityJSON: fmt.Sprintf(
				`{"vaccine":{"code":%q,"priority":%d,"compatibility_group":"ET+TT+PPR"},"eligibility":{"defer_states":["sick","icu","location_icu","quarantine","under_treatment"],"animal_stage":"K1"}}`,
				d.vaccine, d.priority),
		}})
		versionByDose[d.key] = versionID
		ruleByDose[d.key] = ruleIDs[d.key]
	}

	original := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	override := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	for _, goatID := range []string{goodA, goodB, goodC, sickGoat} {
		dob := original
		fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob, OriginType: "birth"})
	}
	icuDOB := original
	fx.SeedGoat(GoatSpec{GoatID: icuGoat, ShedID: icuShedID, DOB: &icuDOB, OriginType: "birth"})

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	for _, d := range doses {
		res, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionByDose[d.key], original)
		story.Assert(d.key+" generation ran without error", err == nil, "err=%v", err)
		story.Assert(d.key+" generated one obligation per goat", res.Generated == 5, "generated=%d", res.Generated)
	}
	story.Assert("ten generated obligation cells before clinical holds", fx.countRows(`
SELECT count(*) FROM obligation_instances
WHERE tenant_id=$1 AND protocol_version_id IN ($2::uuid, $3::uuid)`, fxTenant, versionByDose["ettt"], versionByDose["ppr"]) == 10,
		"unexpected generated obligation count")

	story.Step("Clinical holds happen before planning",
		"Sick and ICU goats are deferred by the real generation/recheck paths. They must not consume operator "+
			"animal capacity after the PPR date is moved.")
	fx.ChangeGoatHealth(sickGoat, "sick", "story-an-sick", original.Add(6*time.Hour))
	story.Assert("sick goat's two vaccine obligations are deferred", deferredCountAN(fx, sickGoat, versionByDose) == 2,
		"deferred=%d", deferredCountAN(fx, sickGoat, versionByDose))
	story.Assert("ICU-location goat's two vaccine obligations are deferred", deferredCountAN(fx, icuGoat, versionByDose) == 2,
		"deferred=%d", deferredCountAN(fx, icuGoat, versionByDose))

	story.Step("Admin moves PPR to Aug 5",
		"The override is stored through the production repository and then the real sweeper plans both vaccines. "+
			"ET+TT should remain on Jul 22; PPR should move to Aug 5; the three eligible goats are the only "+
			"animal slots assigned.")
	if _, err := fx.Obl.UpsertVaccinationDriveDateOverride(fx.Ctx, obldomain.VaccineDriveDateOverride{
		TenantID:          fxTenant,
		ParkID:            fxPark,
		VaccineCode:       "PPR",
		OriginalDriveDate: original,
		OverrideDate:      override,
		Reason:            "CEO moved PPR in E2E",
		CreatedBy:         fxParty,
	}); err != nil {
		t.Fatalf("upsert drive date override: %v", err)
	}

	sweeper := oblapp.NewSweeperService(fx.Obl, nil, nil)
	session := oblapp.NewSweepSession()
	for _, d := range doses {
		cfg := defaultParkSweepConfig()
		cfg.VaccineCode = d.vaccine
		cfg.DrivePlanner = obldomain.DrivePlannerSettings{
			Enabled:                   true,
			MaxGoatsPerDrive:          200,
			VaccinePriority:           d.priority,
			MaxBatchingHoldDays:       7,
			MaxBatchingHoldCount:      1,
			MaxShotsPerAnimalPerDrive: 2,
		}
		cfg.RuleVaccineIDs = map[string]oblapp.RuleVaccineIdentity{
			ruleByDose[d.key]: {VaccineCode: d.vaccine, VaccinePriority: d.priority, CompatibilityGrp: "ET+TT+PPR"},
		}
		res, err := sweeper.SweepVersionWithSessionAsOf(fx.Ctx, fxTenant, versionByDose[d.key], cfg, original, original.AddDate(0, 0, 7), session)
		story.Assert(d.key+" sweep ran without error", err == nil, "err=%v", err)
		story.Assert(d.key+" attached only the three eligible goats", res.Obligations == 3, "obligations=%d", res.Obligations)
	}

	story.Assert("ET+TT stayed on original date for exactly three eligible goats",
		batchedOnDateAN(fx, versionByDose["ettt"], original) == 3, "count=%d", batchedOnDateAN(fx, versionByDose["ettt"], original))
	story.Assert("PPR has no batch left on the original date",
		batchedOnDateAN(fx, versionByDose["ppr"], original) == 0, "count=%d", batchedOnDateAN(fx, versionByDose["ppr"], original))
	story.Assert("PPR has no raw assignment membership left on the original date",
		assignmentAnimalsAN(fx, original, ruleByDose["ppr"]) == 0,
		"assignment_animals=%d", assignmentAnimalsAN(fx, original, ruleByDose["ppr"]))
	story.Assert("ET+TT raw assignment membership still remains on the original date",
		assignmentAnimalsAN(fx, original, ruleByDose["ettt"]) == 3,
		"assignment_animals=%d", assignmentAnimalsAN(fx, original, ruleByDose["ettt"]))
	story.Assert("PPR moved to override date for exactly three eligible goats",
		batchedOnDateAN(fx, versionByDose["ppr"], override) == 3, "count=%d", batchedOnDateAN(fx, versionByDose["ppr"], override))
	story.Assert("deferred goats were not batched on either date",
		batchedForGoatAN(fx, sickGoat)+batchedForGoatAN(fx, icuGoat) == 0,
		"sick_batched=%d icu_batched=%d", batchedForGoatAN(fx, sickGoat), batchedForGoatAN(fx, icuGoat))

	story.Assert("override-date assignment animal_count totals three eligible animals",
		assignmentAnimalsAN(fx, override, ruleByDose["ppr"]) == 3,
		"assignment_animals=%d", assignmentAnimalsAN(fx, override, ruleByDose["ppr"]))
	story.Assert("override-date assignment dose count is also three for the single moved PPR vaccine",
		assignmentDosesAN(fx, override, ruleByDose["ppr"]) == 3,
		"assignment_doses=%d", assignmentDosesAN(fx, override, ruleByDose["ppr"]))
	story.Assert("operator cap of one animal per operator is preserved at assignment row grain",
		maxAssignmentAnimalsAN(fx, override, ruleByDose["ppr"]) <= 1,
		"max_assignment_animals=%d", maxAssignmentAnimalsAN(fx, override, ruleByDose["ppr"]))

	story.Step("Admin moves the already-overridden PPR drive back to Jul 23",
		"The second move must use the original Jul 22 drive identity, restore rows from the current Aug 5 "+
			"override date, and re-split them onto Jul 23. This is the exact CEO/CXO correction path from "+
			"the UI after a vaccine has already been pushed out.")
	backToToday := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	if _, err := fx.Obl.UpsertVaccinationDriveDateOverride(fx.Ctx, obldomain.VaccineDriveDateOverride{
		TenantID:          fxTenant,
		ParkID:            fxPark,
		VaccineCode:       "PPR",
		OriginalDriveDate: original,
		OverrideDate:      backToToday,
		Reason:            "CEO moved PPR back in E2E",
		CreatedBy:         fxParty,
	}); err != nil {
		t.Fatalf("upsert second drive date override: %v", err)
	}
	story.Assert("PPR has no assignment membership left on the old Aug 5 override date after second move",
		assignmentAnimalsAN(fx, override, ruleByDose["ppr"]) == 0,
		"assignment_animals=%d", assignmentAnimalsAN(fx, override, ruleByDose["ppr"]))
	story.Assert("PPR assignment membership moved back to Jul 23 for exactly three eligible goats",
		assignmentAnimalsAN(fx, backToToday, ruleByDose["ppr"]) == 3,
		"assignment_animals=%d", assignmentAnimalsAN(fx, backToToday, ruleByDose["ppr"]))
	story.Assert("operator cap of one animal per operator is still preserved after the second move",
		maxAssignmentAnimalsAN(fx, backToToday, ruleByDose["ppr"]) <= 1,
		"max_assignment_animals=%d", maxAssignmentAnimalsAN(fx, backToToday, ruleByDose["ppr"]))
}

func seedVaccinationOperatorsAN(fx *Fixture, opA, opB, opC string) {
	fx.T.Helper()
	fx.exec("story-an vaccination operators", `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES
  ($1, $4, 'AN-A', 'Story AN Operator A', 'active', 'operator', $5),
  ($2, $4, 'AN-B', 'Story AN Operator B', 'active', 'operator', $5),
  ($3, $4, 'AN-C', 'Story AN Operator C', 'active', 'operator', $5)`,
		opA, opB, opC, fxTenant, fxPark)
	fx.exec("story-an vaccination duties", `
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES
  ($1, 'story_an_vacc_operator_a', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active'),
  ($1, 'story_an_vacc_operator_b', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active'),
  ($1, 'story_an_vacc_operator_c', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active')`,
		fxTenant)
	fx.exec("story-an vaccination positions", `
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES
  ($1, $2, 'center', $5, 'story_an_vacc_operator_a', 'manager', 'friday', 1, 'active', '2026-01-01'),
  ($1, $3, 'center', $5, 'story_an_vacc_operator_b', 'manager', 'sunday', 1, 'active', '2026-01-01'),
  ($1, $4, 'center', $5, 'story_an_vacc_operator_c', 'manager', 'saturday', 1, 'active', '2026-01-01')`,
		fxTenant, opA, opB, opC, fxPark)
}

func assertAvailableOperatorsAN(t *testing.T, fx *Fixture, date time.Time, want []string) {
	t.Helper()
	got, err := fx.Obl.AvailableVaccinationOperatorsForDrive(fx.Ctx, fxTenant, fxPark, date, 200)
	if err != nil {
		t.Fatalf("available vaccination operators for %s: %v", date.Format("2006-01-02"), err)
	}
	if len(got) != len(want) {
		t.Fatalf("available operators for %s = %v, want %v", date.Format("2006-01-02"), got, want)
	}
	seen := map[string]bool{}
	for _, operator := range got {
		seen[operator.OperatorID] = true
		if operator.Cap != 1 {
			t.Fatalf("available operator %s cap = %d, want HRMS cap 1 despite fallback 200", operator.OperatorID, operator.Cap)
		}
	}
	for _, id := range want {
		if !seen[id] {
			t.Fatalf("available operators for %s = %v, missing %s from want %v", date.Format("2006-01-02"), got, id, want)
		}
	}
}

func deferredCountAN(fx *Fixture, goatID string, versions map[string]string) int {
	return fx.countRows(`
SELECT count(*)
FROM obligation_instances
WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'
  AND protocol_version_id IN ($3::uuid, $4::uuid)`, fxTenant, goatID, versions["ettt"], versions["ppr"])
}

func batchedOnDateAN(fx *Fixture, versionID string, planned time.Time) int {
	return fx.countRows(`
SELECT count(DISTINCT oi.target_id)
FROM obligation_instances oi
JOIN obligation_batches ob ON ob.tenant_id=oi.tenant_id AND ob.batch_id=oi.batch_id
WHERE oi.tenant_id=$1 AND oi.protocol_version_id=$2
  AND (ob.planned_date AT TIME ZONE 'Asia/Kolkata')::date = ($3::timestamptz AT TIME ZONE 'Asia/Kolkata')::date`,
		fxTenant, versionID, planned)
}

func batchedForGoatAN(fx *Fixture, goatID string) int {
	return fx.countRows(`
SELECT count(*)
FROM obligation_instances
WHERE tenant_id=$1 AND target_id=$2 AND batch_id IS NOT NULL`, fxTenant, goatID)
}

func assignmentAnimalsAN(fx *Fixture, planned time.Time, ruleID string) int {
	return fx.countRows(`
SELECT COALESCE(sum(animal_count), 0)::int
FROM vaccination_drive_assignments
WHERE tenant_id=$1
  AND planned_date = ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
  AND $3::uuid = ANY(vaccine_rule_ids)`, fxTenant, planned, ruleID)
}

func assignmentDosesAN(fx *Fixture, planned time.Time, ruleID string) int {
	return fx.countRows(`
SELECT COALESCE(sum(total_doses), 0)::int
FROM vaccination_drive_assignments
WHERE tenant_id=$1
  AND planned_date = ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
  AND $3::uuid = ANY(vaccine_rule_ids)`, fxTenant, planned, ruleID)
}

func maxAssignmentAnimalsAN(fx *Fixture, planned time.Time, ruleID string) int {
	return fx.countRows(`
SELECT COALESCE(max(animal_count), 0)::int
FROM vaccination_drive_assignments
WHERE tenant_id=$1
  AND planned_date = ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
  AND $3::uuid = ANY(vaccine_rule_ids)`, fxTenant, planned, ruleID)
}
