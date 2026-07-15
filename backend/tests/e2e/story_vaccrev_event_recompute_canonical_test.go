package e2e

import (
	"net/http"
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccexehttp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/http"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestKernelStoryVaccRev_EventDrivenRecomputeCanonicalRead is the VACC-REV-02 gate-5 proof: the
// PRODUCTION identity HealthGoat and ReproductiveGoat commands, delivered through the durable outbox
// to the REGISTERED vaccination recheck consumer, drive the dynamic recompute end to end and the
// result is visible on the CANONICAL, projection-free read path — not a seeded/deleted screen
// projection.
//
// For each command it asserts the whole chain:
//   - the command PERSISTED a durable outbox event (outbox_messages row),
//   - the registered handler moved the canonical obligation into `deferred`,
//   - the durable obligation_status_events history recorded the defer with its reason,
//   - (health path) the canonical /vaccination/sheds HTTP read reflects the hold and the reopen.
//
// The four screen projection tables were retired by the 5k-50k envelope ADR, so /vaccination/sheds
// is served from canonical indexed SQL; asserting through that route proves the canonical API
// response, not a projection stand-in.
func TestKernelStoryVaccRev_EventDrivenRecomputeCanonicalRead(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-vaccrev", "Event-driven recompute is visible on the canonical read",
		"The production HealthGoat and ReproductiveGoat commands emit durable outbox events; the "+
			"registered vaccination recheck consumer defers and reopens the canonical obligation, and the "+
			"canonical (projection-free) /vaccination/sheds read reflects it.")
	defer story.Finish()
	story.Certify("backend kernel")

	// ---- Health path: sick defers, recovery reopens, asserted through the canonical HTTP read. ----
	serverNow := time.Date(2026, 7, 11, 18, 15, 0, 0, time.FixedZone("IST", 5*3600+1800))
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_vaccrev_health", 21, 14,
		[]string{"sick", "under_treatment", "quarantine", "icu"})

	const shedHealthID = "e9000000-0000-4000-8000-000000000001"
	const stageHealthID = "e9000000-0000-4000-8000-00000000000a"
	fx.SeedShed(shedHealthID, "E2E-VACCREV-H", stageHealthID)

	const goatHealth = "e9000000-0000-4000-8000-000000000010"
	dueAt := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	dob := dueAt.AddDate(0, 0, -21)
	fx.SeedGoat(GoatSpec{GoatID: goatHealth, ShedID: shedHealthID, DOB: &dob})

	story.Step("Generate the primary dose through the production goat.created path",
		"Publish goat.created; the registered generation consumer creates one scheduled obligation due 2026-07-18.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatHealth, serverNow)
	oblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		fxTenant, goatHealth, versionID)
	story.Assert("one scheduled obligation exists", oblID != "", "obligation_id=%q", oblID)

	// Canonical read wired exactly like production: real HTTP route, real service, backend clock.
	svc := vaccexecapp.NewService(fx.VaccExec)
	handler := vaccexehttp.NewHandler(svc, nil).WithClock(func() time.Time { return serverNow })
	mux := http.NewServeMux()
	vaccexehttp.Register(mux, handler)

	beforeRow, ok := requestShedSummaryRow(t, mux, shedHealthID, serverNow.Format(time.RFC3339))
	story.Assert("canonical shed read returns the shed before the health change", ok, "found=%v", ok)
	story.Assert("canonical read shows scheduled future work (NextDue set)",
		beforeRow.Status == vaccexecdomain.ShedStatusScheduled && beforeRow.NextDue != nil,
		"status=%q next_due=%v", beforeRow.Status, beforeRow.NextDue)

	story.Step("HealthGoat command marks the goat sick; the registered consumer defers the dose",
		"The production HealthGoat command emits a durable goat.health.changed outbox event; the "+
			"registered recheck consumer holds the open dose.")
	fx.ChangeGoatHealth(goatHealth, "sick", "story-vaccrev-sick", serverNow)

	story.Assert("HealthGoat persisted a durable outbox event",
		fx.countOutbox(goatHealth, vaccapp.EventGoatHealthChanged) >= 1,
		"outbox_count=%d", fx.countOutbox(goatHealth, vaccapp.EventGoatHealthChanged))
	deferredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, oblID)
	story.Assert("the canonical obligation is now deferred", deferredStatus == "deferred", "status=%q", deferredStatus)
	deferReason := fx.scanText(`SELECT payload->>'defer_status' FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`, fxTenant, oblID)
	story.Assert("the durable status-event history records defer_status='sick'", deferReason == "sick", "defer_status=%q", deferReason)

	heldRow, ok := requestShedSummaryRow(t, mux, shedHealthID, serverNow.Format(time.RFC3339))
	story.Assert("canonical shed read still returns the shed while held", ok, "found=%v", ok)
	story.Assert("canonical read drops the held dose from open work (no NextDue, not scheduled)",
		heldRow.NextDue == nil && heldRow.Status != vaccexecdomain.ShedStatusScheduled,
		"status=%q next_due=%v", heldRow.Status, heldRow.NextDue)

	story.Step("HealthGoat command marks the goat healthy; the consumer reopens and realigns",
		"The production healthy command emits goat.health.changed; the recovery recheck reopens the "+
			"deferred dose so it is scheduled again on the canonical read.")
	fx.ChangeGoatHealth(goatHealth, "healthy", "story-vaccrev-recover", serverNow.Add(72*time.Hour))
	reopenedStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, oblID)
	story.Assert("the canonical obligation is scheduled again", reopenedStatus == "scheduled", "status=%q", reopenedStatus)
	reopenedRow, ok := requestShedSummaryRow(t, mux, shedHealthID, serverNow.Add(72*time.Hour).Format(time.RFC3339))
	story.Assert("canonical read shows the reopened dose (NextDue set again)",
		ok && reopenedRow.NextDue != nil, "found=%v next_due=%v", ok, reopenedRow.NextDue)

	// ---- Reproductive path: ReproductiveGoat command defers via pregnancy month 4-5. ----
	// A pregnant doe is an ADULT, so her obligation comes from the adult (post_arrival) path — never
	// a kid dose (VACC-RULE-02) — and the pregnancy hold applies to that adult obligation.
	const pregnancyRuleDSL = `{"pregnancy_policy":{"skip_from_pregnancy_month":4,"skip_through_pregnancy_month":5}}`
	reproVersionID, _ := fx.PublishScheduleProtocol("vaccination.e2e.story_vaccrev_repro", pregnancyRuleDSL,
		[]RuleSpec{{DoseCode: "primary", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 21, DueWindowDays: 0}})

	// Reuse the health shed's stage lookup (shared animal_stage_lookup; stage_code 'K1' is unique per
	// tenant, so a second distinct stage id would collide).
	const shedReproID = "e9000000-0000-4000-8000-000000000002"
	fx.SeedShed(shedReproID, "E2E-VACCREV-R", stageHealthID)

	const goatRepro = "e9000000-0000-4000-8000-000000000020"
	dobRepro := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	reproEntry := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatRepro, ShedID: shedReproID, DOB: &dobRepro, EntryDate: &reproEntry, Stage: "adult", OriginType: "procured"})

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	genAsOf := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) // adult, not yet pregnant
	if _, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatRepro, genAsOf); err != nil {
		t.Fatalf("generate reproductive goat obligation: %v", err)
	}
	reproOblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		fxTenant, goatRepro, reproVersionID)
	story.Assert("reproductive goat has one obligation", reproOblID != "", "obligation_id=%q", reproOblID)

	story.Step("ReproductiveGoat command records pregnancy month 4; the consumer defers the dose",
		"The production ReproductiveGoat command emits a durable goat.reproductive.changed outbox event; "+
			"the registered recheck consumer holds the open dose with late_pregnancy_hold.")
	checkAsOf := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	breedingDate := checkAsOf.AddDate(0, 0, -100) // pregnancy month 4, inside the 4-5 hold window
	fx.ChangeGoatReproductive(goatRepro, "pregnant", "story-vaccrev-pregnant", checkAsOf, &breedingDate)

	story.Assert("ReproductiveGoat persisted a durable outbox event",
		fx.countOutbox(goatRepro, vaccapp.EventGoatReproductiveChanged) >= 1,
		"outbox_count=%d", fx.countOutbox(goatRepro, vaccapp.EventGoatReproductiveChanged))
	reproDeferred := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, reproOblID)
	story.Assert("the reproductive obligation is now deferred", reproDeferred == "deferred", "status=%q", reproDeferred)
	reproReason := fx.scanText(`SELECT payload->>'defer_status' FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`, fxTenant, reproOblID)
	story.Assert("the durable status-event history records defer_status='late_pregnancy_hold'",
		reproReason == "late_pregnancy_hold", "defer_status=%q", reproReason)
}

// countOutbox counts durable outbox events persisted for a goat aggregate, proving the identity
// command wrote an event (regardless of whether the relay has since marked it published).
func (f *Fixture) countOutbox(aggregateID, eventType string) int {
	f.T.Helper()
	var n int
	if err := f.Pool.QueryRow(f.Ctx,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type=$3`,
		fxTenant, aggregateID, eventType).Scan(&n); err != nil {
		f.T.Fatalf("count outbox %s for %s: %v", eventType, aggregateID, err)
	}
	return n
}
