package e2e

import (
	"encoding/json"
	"testing"
	"time"

	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// ChangeGoatIdentity drives the PRODUCTION identity DOB/entry-date correction command and delivers
// its durable goat.identity.changed outbox event to the registered vaccination recheck consumer.
func (f *Fixture) ChangeGoatIdentity(goatID string, dob, entryDate *time.Time, key string, occurredAt time.Time) {
	f.T.Helper()
	bodyMap := map[string]any{
		"reason":        "E2E identity correction through identity service",
		"occurred_at":   occurredAt,
		"evidence_refs": []map[string]string{{"evidence_type": "source_record", "evidence_id": "e2e-identity-" + key}},
		"row_version":   f.goatRowVersion(goatID),
	}
	if dob != nil {
		bodyMap["dob"] = dob.Format("2006-01-02")
	}
	if entryDate != nil {
		bodyMap["entry_date"] = entryDate.Format("2006-01-02")
	}
	body, _ := json.Marshal(bodyMap)
	if _, err := f.Identity.IdentityGoat(f.Ctx, identityapp.IdentityGoatInput{
		TenantID: fxTenant, ActorID: fxParty, IdempotencyKey: key, TraceID: "trace-" + key,
		GoatID: goatID, RawBody: body,
	}); err != nil {
		f.T.Fatalf("correct goat identity %s: %v", goatID, err)
	}
	f.dispatchIdentityOutbox(goatID, vaccapp.EventGoatIdentityChanged)
}

// TestKernelStoryVaccRev_IdentityCorrectionRecompute is the VACC-REV-05 proof that "later data
// entry" automatically recomputes. A goat is created with NO DOB, so its birth_age kid obligation
// cannot anchor and is not generated. The PRODUCTION IdentityGoat command then supplies the DOB,
// emitting a durable goat.identity.changed outbox event; the REGISTERED vaccination recheck consumer
// recomputes and materializes the obligation from the corrected anchor — no manual generation call.
func TestKernelStoryVaccRev_IdentityCorrectionRecompute(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-vaccrev-identity", "DOB correction auto-recomputes obligations",
		"A goat with no DOB has no birth_age obligation. The production IdentityGoat command supplies "+
			"the DOB, emits a durable goat.identity.changed event, and the registered recheck consumer "+
			"materializes the obligation from the corrected anchor.")
	defer story.Finish()
	story.Certify("backend kernel")

	serverNow := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_vaccrev_identity", 28, 7, nil)

	const shedID = "ea000000-0000-4000-8000-000000000001"
	const stageID = "ea000000-0000-4000-8000-00000000000a"
	fx.SeedShed(shedID, "E2E-VACCREV-ID", stageID)

	const goatID = "ea000000-0000-4000-8000-000000000010"
	// Seeded with NO DOB: the birth_age rule has no anchor.
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID})

	story.Step("Generation schedules the no-DOB goat against a generation-time anchor",
		"Publish goat.created. Without a DOB the birth_age dose is anchored to the generation instant, not "+
			"the true birth date — exactly the anchor this correction must repair.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatID, serverNow)
	beforeActive := fx.countActiveObligations(goatID, versionID)
	story.Assert("one open obligation exists before the correction", beforeActive == 1, "active=%d", beforeActive)

	story.Step("IdentityGoat command supplies the DOB; the registered consumer recomputes and supersedes",
		"The production IdentityGoat command corrects the DOB, emits a durable goat.identity.changed event, "+
			"and the recheck consumer supersedes the obsolete obligation and re-anchors a new one at DOB+28d.")
	dob := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC) // corrected true birth date
	fx.ChangeGoatIdentity(goatID, &dob, nil, "story-vaccrev-identity-dob", serverNow)

	story.Assert("IdentityGoat persisted a durable goat.identity.changed outbox event",
		fx.countOutbox(goatID, vaccapp.EventGoatIdentityChanged) >= 1,
		"outbox_count=%d", fx.countOutbox(goatID, vaccapp.EventGoatIdentityChanged))

	gotDOB := fx.scanText(`SELECT to_char(dob,'YYYY-MM-DD') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatID)
	story.Assert("the goat's DOB is persisted by the correction", gotDOB == "2026-06-20", "dob=%q", gotDOB)

	superseded := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3 AND status IN ('canceled','superseded')`,
		fxTenant, goatID, versionID)
	story.Assert("the obsolete pre-correction obligation was superseded/canceled by the recompute", superseded >= 1, "superseded=%d", superseded)

	activeDue := fx.scanText(`SELECT to_char((due_at AT TIME ZONE 'Asia/Kolkata')::date,'YYYY-MM-DD')
		FROM obligation_instances
		WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3 AND status NOT IN ('canceled','superseded')`,
		fxTenant, goatID, versionID)
	story.Assert("the recomputed obligation is re-anchored to the corrected DOB + 28 days", activeDue == "2026-07-18", "active_due=%q", activeDue)
}

// countActiveObligations counts OPEN (non-terminal) obligations only — scheduled/due/in_progress/
// deferred — excluding completed/canceled/superseded/missed/waived history.
func (f *Fixture) countActiveObligations(goatID, versionID string) int {
	f.T.Helper()
	return f.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3 AND status IN ('scheduled','due','in_progress','deferred')`,
		fxTenant, goatID, versionID)
}

// TestKernelStoryVaccRev_HistoryOutranksDOBCorrection is the VACC-REV-05 proof that vaccination
// history outranks DOB/arrival per vaccine. A goat with NO DOB/arrival is given a same-vaccine
// administration through the PRODUCTION SOP submit + verify path; after_previous_completion then owns
// the next dose (anchored to that administration) and the birth_age primary is suppressed. A later
// DOB correction via IdentityGoat runs the real command -> outbox -> recheck path, and the
// completion-anchored schedule is UNCHANGED — no replacement, no duplicate primary obligation.
func TestKernelStoryVaccRev_HistoryOutranksDOBCorrection(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-vaccrev-history", "Vaccination history outranks a later DOB correction",
		"A goat with no DOB gets an accepted ET+TT dose through the production SOP path; "+
			"after_previous_completion owns the next dose. A later DOB correction must not replace, "+
			"duplicate, or replay that completion-anchored dose.")
	defer story.Finish()
	story.Certify("backend kernel")

	// The production SOP verify stamps verified_at with the real wall clock, so the whole timeline is
	// anchored to real "now" (not a fixed past date) — otherwise the accepted completion would sort
	// after any fixed as_of and drop out of the history read. Re-generation and the recheck run a
	// little later so the just-verified completion is visible.
	serverNow := time.Now().UTC()
	laterAsOf := serverNow.Add(2 * time.Hour)
	vaccineDSL := `{"vaccine":{"code":"ETTT","type":"killed","pathogen_class":"bacterial"}}`
	versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_vaccrev_history", vaccineDSL,
		[]RuleSpec{
			{DoseCode: "primary", Sequence: 1, TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7},
			{DoseCode: "revac", Sequence: 2, TriggerType: "after_previous_completion", OffsetDays: 180},
		})
	birthRuleID := ruleIDs["primary"]

	const shedID = "eb000000-0000-4000-8000-000000000001"
	const stageID = "eb000000-0000-4000-8000-00000000000a"
	fx.SeedShed(shedID, "E2E-VACCREV-HIST", stageID)

	const goatID = "eb000000-0000-4000-8000-000000000010"
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, NoDOB: true}) // no DOB, no arrival

	story.Step("The no-DOB goat's primary dose is generated, then administered through the production SOP path",
		"goat.created generates the primary (anchored to the generation instant without a DOB); the sweeper "+
			"+ SOP submit + independent verify then produce a real accepted ET+TT completion — the goat's history.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatID, serverNow)
	completeVaccinationDriveThroughSOP(t, fx, versionID, shedID, []string{goatID}, serverNow, "story-vaccrev-hist")

	story.Step("after_previous_completion owns the next dose; the birth_age primary is suppressed",
		"Re-running generation: the same-vaccine history means after_previous_completion schedules the next "+
			"dose from the administration and the birth_age primary is suppressed (no replay).")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	if _, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, laterAsOf); err != nil {
		t.Fatalf("regenerate after completion: %v", err)
	}

	activeBefore := fx.countActiveObligations(goatID, versionID)
	story.Assert("exactly one open obligation exists (the completion-anchored revac)", activeBefore == 1, "active=%d", activeBefore)
	primaryBefore := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3 AND status NOT IN ('canceled','superseded','completed')`,
		fxTenant, goatID, birthRuleID)
	story.Assert("no open birth_age primary obligation exists", primaryBefore == 0, "open_primary=%d", primaryBefore)
	revacRuleID := fx.scanText(`SELECT oi.rule_id::text FROM obligation_instances oi WHERE oi.tenant_id=$1 AND oi.target_id=$2 AND oi.protocol_version_id=$3 AND oi.status NOT IN ('canceled','superseded','completed')`,
		fxTenant, goatID, versionID)
	story.Assert("the open obligation is the after_previous_completion revac (not the primary)", revacRuleID != "" && revacRuleID != birthRuleID, "rule=%s birth=%s", revacRuleID, birthRuleID)
	revacDueBefore := fx.scanText(`SELECT to_char((due_at AT TIME ZONE 'Asia/Kolkata')::date,'YYYY-MM-DD')
		FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3 AND status NOT IN ('canceled','superseded','completed')`,
		fxTenant, goatID, versionID)
	story.Assert("the revac is anchored to the administration (well in the future, not the generation instant)",
		revacDueBefore > "2026-12-01", "revac_due=%q", revacDueBefore)
	revacIDBefore := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3 AND status NOT IN ('canceled','superseded','completed')`,
		fxTenant, goatID, versionID)

	story.Step("A later DOB correction must NOT disturb the completion-anchored schedule",
		"IdentityGoat supplies a DOB and emits goat.identity.changed; the recheck runs, but vaccination "+
			"history still outranks the anchor, so the birth_age primary stays suppressed and the revac is unchanged.")
	dob := serverNow.AddDate(0, 0, -45) // a recent kid DOB; history must still outrank it
	fx.ChangeGoatIdentity(goatID, &dob, nil, "story-vaccrev-history-dob", laterAsOf)

	story.Assert("IdentityGoat persisted a durable goat.identity.changed outbox event",
		fx.countOutbox(goatID, vaccapp.EventGoatIdentityChanged) >= 1,
		"outbox_count=%d", fx.countOutbox(goatID, vaccapp.EventGoatIdentityChanged))

	activeAfter := fx.countActiveObligations(goatID, versionID)
	story.Assert("still exactly one open obligation — no duplicate primary was created", activeAfter == 1, "active=%d", activeAfter)
	primaryAfter := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3 AND status NOT IN ('canceled','superseded','completed')`,
		fxTenant, goatID, birthRuleID)
	story.Assert("the birth_age primary remains suppressed after the DOB correction", primaryAfter == 0, "open_primary=%d", primaryAfter)
	revacDueAfter := fx.scanText(`SELECT to_char((due_at AT TIME ZONE 'Asia/Kolkata')::date,'YYYY-MM-DD')
		FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, revacIDBefore)
	story.Assert("the completion-anchored revac due is unchanged", revacDueAfter == revacDueBefore, "before=%q after=%q", revacDueBefore, revacDueAfter)
	revacStatusAfter := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, revacIDBefore)
	story.Assert("the revac obligation was neither canceled nor superseded", revacStatusAfter == "scheduled", "status=%q", revacStatusAfter)
}

func (f *Fixture) countObligations(goatID, versionID string) int {
	f.T.Helper()
	var n int
	if err := f.Pool.QueryRow(f.Ctx,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		fxTenant, goatID, versionID).Scan(&n); err != nil {
		f.T.Fatalf("count obligations for %s: %v", goatID, err)
	}
	return n
}
