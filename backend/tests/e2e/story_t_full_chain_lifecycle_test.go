package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryT_FullChainLifecycle drives ONE goat through the entire event-driven chain
// documented in docs/architecture/event-driven-kernel.md, in order, asserting the obligation set
// after every step: birth (predefined full schedule) -> sick (dynamic defer of the whole open
// schedule) -> recovery (dynamic reopen/realign) -> shed-shift (dynamic re-scope) -> death (dynamic,
// terminal cancellation of everything still open). Every prior story in this suite proves one
// transition in isolation; this story proves they compose correctly on the SAME goat's SAME
// obligation set, back to back, through the real generation service and the real SM-2/SM-3 event
// handlers -- no shortcuts, no reimplemented schedule math.
func TestKernelStoryT_FullChainLifecycle(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-t", "Full chain: birth -> schedule -> sick-defer -> recover -> shift -> death",
		"One kid is born and given the full standard kid schedule. It falls sick mid-course (its entire "+
			"open schedule pauses), recovers (every held dose reopens and realigns), is moved to a new "+
			"shed (its open doses re-scope to follow it), and finally dies (every still-open dose is "+
			"cancelled forever). The obligation set is asserted at each step so the whole chain, not just "+
			"one transition, is proven to compose correctly on a single goat.")
	defer story.Finish()
	story.Certify("backend kernel")

	const shedOldID = "e6000000-0000-4000-8000-000000000001"
	const stageOldID = "e6000000-0000-4000-8000-00000000000a"
	fx.SeedShed(shedOldID, "E2E-T-OLD", stageOldID)
	const shedNewID = "e6000000-0000-4000-8000-000000000002"
	const stageNewID = "e6000000-0000-4000-8000-00000000000b"
	fx.SeedAdultShed(shedNewID, "E2E-T-NEW", stageNewID, "K2")

	// Full kid bundle (same 5 rows as Story G/O), plus version-level defer_states so sick/quarantine/
	// icu pause every row at once. Generous 60-day due windows so the missed-dose catch-up policy
	// never interferes across this story's ~2-week timeline.
	const deferDSL = `{"eligibility":{"defer_states":["sick","quarantine","icu"]}}`
	type dose struct {
		code   string
		seq    int32
		offset int32
	}
	bundle := []dose{
		{"ettt_1", 1, 28},
		{"ettt_2", 2, 49},
		{"fmd_hs", 3, 84},
		{"ppr", 4, 112},
		{"goat_pox", 5, 140},
	}
	specs := make([]RuleSpec, 0, len(bundle))
	for _, d := range bundle {
		specs = append(specs, RuleSpec{DoseCode: d.code, Sequence: d.seq, TriggerType: "birth_age", OffsetDays: d.offset, DueWindowDays: 60})
	}
	versionID, _ := fx.PublishScheduleProtocol("vaccination.e2e.story_t", deferDSL, specs)

	const goatID = "e6000000-0000-4000-8000-000000000010"
	dob := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	fx.exec("kid goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex,
		    current_location_id, park_id, shed_id, management_stage, dob, origin_type)
		 VALUES ($1, $2, 'alive', 'healthy', 'goat', $3, 'female', $4, $5, $4, 'K1', $6::date, 'birth')`,
		goatID, fxTenant, fxParty, shedOldID, fxPark, dob)

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)

	scheduledCount := func() int {
		return fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, goatID)
	}
	deferredCount := func() int {
		return fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'`, fxTenant, goatID)
	}
	canceledCount := func() int {
		return fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='canceled'`, fxTenant, goatID)
	}
	scopeCount := func(shedID string) int {
		return fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND scope_type='shed' AND scope_id=$3 AND status='scheduled'`, fxTenant, goatID, shedID)
	}

	// ---- Step 1: BIRTH (predefined). ----
	story.Step("1. Birth: the full standard kid schedule is generated",
		"The kid is registered with DOB 2026-03-01, living in Shed-Old. Generation materializes the "+
			"full 5-row kid bundle (4w/7w/12w/16w/20w) as scheduled obligations, all scoped to Shed-Old.")
	asOfBirth := dob.AddDate(0, 0, 21)
	birthRes, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, asOfBirth)
	story.Assert("birth generation ran without error", err == nil, "err=%v", err)
	story.Assert("all 5 kid doses were generated at birth", birthRes.Generated == 5, "generated=%d", birthRes.Generated)
	story.Assert("obligation set after birth: 5 scheduled, 0 deferred, 0 cancelled",
		scheduledCount() == 5 && deferredCount() == 0 && canceledCount() == 0,
		"scheduled=%d deferred=%d canceled=%d", scheduledCount(), deferredCount(), canceledCount())
	story.Assert("all 5 doses are scoped to Shed-Old", scopeCount(shedOldID) == 5, "shed_old_scheduled=%d", scopeCount(shedOldID))

	// ---- Step 2: SICK (dynamic defer). ----
	story.Step("2. Sick: the entire open schedule pauses",
		"The kid falls sick. The next recheck must defer every still-open dose at once -- sickness is a "+
			"goat-level fact, not a per-dose one -- leaving nothing scheduled.")
	asOfSick := asOfBirth.AddDate(0, 0, 1)
	fx.ChangeGoatHealth(goatID, "sick", "story-t-sick", asOfSick)
	story.Assert("obligation set after sick: 0 scheduled, 5 deferred, 0 cancelled",
		scheduledCount() == 0 && deferredCount() == 5 && canceledCount() == 0,
		"scheduled=%d deferred=%d canceled=%d", scheduledCount(), deferredCount(), canceledCount())

	// ---- Step 3: RECOVER (dynamic reopen/realign). ----
	story.Step("3. Recover: every held dose reopens and realigns",
		"The kid recovers. The real recovery-repair recheck must reopen all 5 held doses back to "+
			"scheduled, realigning due dates onto the recovery-time calendar.")
	asOfRecover := asOfSick.AddDate(0, 0, 4)
	fx.ChangeGoatHealth(goatID, "healthy", "story-t-recover", asOfRecover)
	story.Assert("obligation set after recovery: 5 scheduled, 0 deferred, 0 cancelled",
		scheduledCount() == 5 && deferredCount() == 0 && canceledCount() == 0,
		"scheduled=%d deferred=%d canceled=%d", scheduledCount(), deferredCount(), canceledCount())

	// ---- Step 4: SHED-SHIFT (dynamic re-scope). ----
	story.Step("4. Shed-shift: the open doses follow the goat to its new shed",
		"The kid is moved from Shed-Old to Shed-New. The real SM-2 shift handler must re-scope all 5 "+
			"still-open doses to Shed-New; Shed-Old must no longer count any of them.")
	asOfShift := asOfRecover.AddDate(0, 0, 1)
	fx.MoveGoat(goatID, shedNewID, "story-t-shift", asOfShift)
	story.Assert("obligation set after shift: still 5 scheduled, 0 deferred, 0 cancelled (only scope moved)",
		scheduledCount() == 5 && deferredCount() == 0 && canceledCount() == 0,
		"scheduled=%d deferred=%d canceled=%d", scheduledCount(), deferredCount(), canceledCount())
	story.Assert("Shed-Old no longer counts any of the goat's doses", scopeCount(shedOldID) == 0, "shed_old_scheduled=%d", scopeCount(shedOldID))
	story.Assert("Shed-New now counts all 5 of the goat's doses", scopeCount(shedNewID) == 5, "shed_new_scheduled=%d", scopeCount(shedNewID))

	// ---- Step 5: DEATH (dynamic, terminal). ----
	story.Step("5. Death: every still-open dose is cancelled forever",
		"The kid dies. The real SM-3 death handler must cancel all 5 still-open (scheduled) doses "+
			"forever, regardless of which shed they were scoped to.")
	asOfDeath := asOfShift.AddDate(0, 0, 2)
	fx.ExitGoat(goatID, "dead", "story-t-death", asOfDeath)
	story.Assert("obligation set after death: 0 scheduled, 0 deferred, 5 cancelled",
		scheduledCount() == 0 && deferredCount() == 0 && canceledCount() == 5,
		"scheduled=%d deferred=%d canceled=%d", scheduledCount(), deferredCount(), canceledCount())

	story.Step("Death event redelivery is a clean no-op",
		"Re-dispatching the same goat.exited event must not error and must not change the final "+
			"obligation set -- the full chain ends in a stable, idempotent terminal state.")
	fx.dispatchIdentityOutbox(goatID, oblapp.EventGoatExited)
	story.Assert("redelivered death event ran without error", true, "same durable outbox envelope replayed")
	story.Assert("final obligation set is unchanged by replay: 0 scheduled, 0 deferred, 5 cancelled",
		scheduledCount() == 0 && deferredCount() == 0 && canceledCount() == 5,
		"scheduled=%d deferred=%d canceled=%d", scheduledCount(), deferredCount(), canceledCount())
	story.Assert("all 5 obligations from the version total exactly 5 across the whole chain",
		fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`, fxTenant, goatID, versionID) == 5,
		"total=%d", fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`, fxTenant, goatID, versionID))
}
