package e2e

import (
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryQ_SickAndICULocationDefer extends Story B's health-defer coverage (which only ever
// drove the health_status='quarantine' path) with the two dynamic-defer paths it left untested:
//
//  1. health_status='sick' (a distinct defer_states value from quarantine), and
//  2. the LOCATION-based ICU path: a goat whose own health_status stays 'healthy' the whole time, but
//     which is physically housed in a shed flagged location_operational_attributes.is_icu=true.
//     deferredReason (backend/internal/vaccination/app/generation.go) checks this as an independent
//     signal from health_status -- a goat can defer purely because of where it is standing, even if
//     nobody ever updated its own health_status field. No existing story drove this location-flag
//     branch before.
//
// Both goats use the same real GenerateForGoat / GenerateRecoveryRepairForGoat entrypoints Story B
// uses -- no shortcut, no new kernel code.
func TestKernelStoryQ_SickAndICULocationDefer(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-q", "Dynamic defer: health_status='sick' and location-flagged ICU",
		"Two goats hit two different dynamic defer paths. G-Sick's own health_status flips to 'sick' -- "+
			"the next recheck must defer its open dose. G-ICU-Shed's health_status never changes (it stays "+
			"'healthy'), but it is moved into a shed flagged is_icu=true -- the recheck must defer it purely "+
			"from the location flag. Both doses reopen and realign once the goat is healthy/relocated again.")
	defer story.Finish()

	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_q", 21, 0, []string{"sick", "icu", "quarantine", "under_treatment"})

	const shedNormalID = "e4000000-0000-4000-8000-000000000001"
	const stageNormalID = "e4000000-0000-4000-8000-00000000000a"
	fx.SeedShed(shedNormalID, "E2E-Q-NORMAL", stageNormalID)

	const shedICUID = "e4000000-0000-4000-8000-000000000002"
	const stageICUID = "e4000000-0000-4000-8000-00000000000b"
	fx.seedICUShed(shedICUID, "E2E-Q-ICU", stageICUID)

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)

	// ---- G-Sick: health_status-driven defer/recovery. ----
	const goatSick = "e4000000-0000-4000-8000-000000000010"
	dobSick := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatSick, ShedID: shedNormalID, DOB: &dobSick})

	story.Step("G-Sick: generate the primary dose",
		"Generate obligations for G-Sick as of 2026-06-23 (53 days after birth, 21-day-offset rule): one "+
			"scheduled obligation.")
	genAsOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	sickGen1, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatSick, genAsOf)
	story.Assert("G-Sick generation ran without error", err == nil, "err=%v", err)
	story.Assert("G-Sick: exactly one obligation was generated", sickGen1.Generated == 1, "generated=%d", sickGen1.Generated)

	sickOblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		fxTenant, goatSick, versionID)
	sickOriginalDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, sickOblID)

	story.Step("G-Sick becomes sick; the next recheck defers the open dose",
		"Flip health_status='sick' and recheck the next day. defer_states includes 'sick', so the open "+
			"dose must be held (deferred), with a durable event recording defer_status='sick'.")
	fx.exec("mark goat sick", `UPDATE goats SET health_status='sick' WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatSick)
	sickDeferAsOf := genAsOf.AddDate(0, 0, 1)
	sickGen2, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatSick, sickDeferAsOf)
	story.Assert("G-Sick recheck (sick) ran without error", err == nil, "err=%v", err)
	story.Assert("G-Sick recheck deferred the open obligation", sickGen2.Deferred == 1, "deferred=%d", sickGen2.Deferred)

	sickDeferredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, sickOblID)
	story.Assert("G-Sick's obligation is now deferred", sickDeferredStatus == "deferred", "status=%q", sickDeferredStatus)
	sickDeferStatus := fx.scanText(`SELECT payload->>'defer_status' FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`, fxTenant, sickOblID)
	story.Assert("the durable defer event records defer_status='sick' (not quarantine/icu)", sickDeferStatus == "sick", "defer_status=%q", sickDeferStatus)

	story.Step("G-Sick recovers; recovery-repair reopens and realigns the dose",
		"Flip health_status='healthy' and run the real GenerateRecoveryRepairForGoat recheck. It must "+
			"reopen the deferred dose and realign its due date onto the recovery-time calendar.")
	fx.exec("mark goat healthy", `UPDATE goats SET health_status='healthy' WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatSick)
	sickRecoverAsOf := sickDeferAsOf.AddDate(0, 0, 3)
	sickGen3, err := gen.GenerateRecoveryRepairForGoat(fx.Ctx, fxTenant, goatSick, sickRecoverAsOf)
	story.Assert("G-Sick recovery recheck ran without error", err == nil, "err=%v", err)
	story.Assert("G-Sick recovery recheck reopened the obligation", sickGen3.Reopened == 1, "reopened=%d", sickGen3.Reopened)
	sickRecoveredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, sickOblID)
	story.Assert("G-Sick's obligation is scheduled again", sickRecoveredStatus == "scheduled", "status=%q", sickRecoveredStatus)
	sickRecoveredDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, sickOblID)
	story.Assert("G-Sick's due date was realigned forward, not left stale",
		sickRecoveredDue.After(sickOriginalDue), "original_due=%s recovered_due=%s", sickOriginalDue.Format("2006-01-02"), sickRecoveredDue.Format("2006-01-02"))

	// ---- G-ICU-Shed: location-flag-driven defer/recovery (health_status never changes). ----
	const goatICU = "e4000000-0000-4000-8000-000000000020"
	dobICU := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatICU, ShedID: shedNormalID, DOB: &dobICU})

	story.Step("G-ICU-Shed: generate the primary dose while healthy in the normal shed",
		"Generate obligations for G-ICU-Shed as of 2026-06-23, same rule. One scheduled obligation, "+
			"health_status='healthy' throughout -- only its shed will change.")
	icuGen1, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatICU, genAsOf)
	story.Assert("G-ICU-Shed generation ran without error", err == nil, "err=%v", err)
	story.Assert("G-ICU-Shed: exactly one obligation was generated", icuGen1.Generated == 1, "generated=%d", icuGen1.Generated)

	icuOblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		fxTenant, goatICU, versionID)
	icuOriginalDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, icuOblID)
	healthBefore := fx.scanText(`SELECT health_status FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatICU)
	story.Assert("G-ICU-Shed's own health_status is 'healthy' before the shed move", healthBefore == "healthy", "health_status=%q", healthBefore)

	story.Step("G-ICU-Shed is moved into the ICU-flagged shed; the recheck defers it from the location alone",
		"Move the goat's shed_id AND current_location_id to the ICU shed (is_icu=true). health_status is "+
			"NOT touched -- it must still read 'healthy'. The recheck must defer the open dose purely "+
			"because the goat's current location is ICU-flagged (deferredReason's g.LocationIsICU branch), "+
			"recording defer_status='location_icu'.")
	fx.exec("move goat into ICU shed",
		`UPDATE goats SET shed_id=$3, current_location_id=$3 WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatICU, shedICUID)
	icuDeferAsOf := genAsOf.AddDate(0, 0, 1)
	icuGen2, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatICU, icuDeferAsOf)
	story.Assert("G-ICU-Shed recheck ran without error", err == nil, "err=%v", err)
	story.Assert("G-ICU-Shed recheck deferred the open obligation from the location flag alone", icuGen2.Deferred == 1, "deferred=%d", icuGen2.Deferred)

	icuDeferredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, icuOblID)
	story.Assert("G-ICU-Shed's obligation is now deferred", icuDeferredStatus == "deferred", "status=%q", icuDeferredStatus)
	icuDeferReason := fx.scanText(`SELECT payload->>'defer_status' FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`, fxTenant, icuOblID)
	story.Assert("the durable defer event records defer_status='location_icu' (location-flag path, not a health_status value)",
		icuDeferReason == "location_icu", "defer_status=%q", icuDeferReason)
	healthDuringDefer := fx.scanText(`SELECT health_status FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatICU)
	story.Assert("G-ICU-Shed's own health_status is STILL 'healthy' -- the defer came from the shed, not the goat's health field",
		healthDuringDefer == "healthy", "health_status=%q", healthDuringDefer)

	story.Step("G-ICU-Shed is moved back to a normal shed; recovery-repair reopens and realigns",
		"Move the goat back to the normal (non-ICU) shed and run GenerateRecoveryRepairForGoat. It must "+
			"reopen the held dose and realign due_at forward, exactly like a health recovery.")
	fx.exec("move goat back to normal shed",
		`UPDATE goats SET shed_id=$3, current_location_id=$3 WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatICU, shedNormalID)
	icuRecoverAsOf := icuDeferAsOf.AddDate(0, 0, 3)
	icuGen3, err := gen.GenerateRecoveryRepairForGoat(fx.Ctx, fxTenant, goatICU, icuRecoverAsOf)
	story.Assert("G-ICU-Shed recovery recheck ran without error", err == nil, "err=%v", err)
	story.Assert("G-ICU-Shed recovery recheck reopened the obligation", icuGen3.Reopened == 1, "reopened=%d", icuGen3.Reopened)
	icuRecoveredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, icuOblID)
	story.Assert("G-ICU-Shed's obligation is scheduled again", icuRecoveredStatus == "scheduled", "status=%q", icuRecoveredStatus)
	icuRecoveredDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, icuOblID)
	story.Assert("G-ICU-Shed's due date was realigned forward, not left stale",
		icuRecoveredDue.After(icuOriginalDue), "original_due=%s recovered_due=%s", icuOriginalDue.Format("2006-01-02"), icuRecoveredDue.Format("2006-01-02"))
}

// seedICUShed creates a shed location under the baseline CBE park with
// location_operational_attributes.is_icu=true (mirroring Fixture.SeedShed, but flagged ICU instead of
// plain-usable). This is the location-based ICU signal deferredReason checks independently of
// goats.health_status -- no existing harness helper creates an ICU-flagged shed.
func (f *Fixture) seedICUShed(shedID, shedCode, stageID string) {
	f.T.Helper()
	f.exec("ICU shed location",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', $3, $3, $4, 'active')`,
		shedID, fxTenant, shedCode, fxPark)
	f.exec("ICU shed stage lookup",
		`INSERT INTO animal_stage_lookup (animal_stage_id, tenant_id, stage_code, name, status)
		 VALUES ($1, $2, 'K1-ICU', 'K1 ICU kids', 'active')`,
		stageID, fxTenant)
	f.exec("ICU shed profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'mixed', 50)`,
		shedID, fxTenant, stageID)
	f.exec("ICU shed operational attributes (is_icu=true)",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, false, false, true)`,
		fxTenant, shedID)
}
