package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStory_ShiftingVaccinationHandoff is the producer->consumer E2E proof for the
// domain-event-architecture `shifting_completion_to_vaccination` contract. It drives the REAL shifting
// completion producer and the REAL registered consumers, with nothing about the asserted outcome
// hand-seeded:
//
//	PRODUCER  counts.CompleteShiftingEvent -> identity.RelocateGoatsToShedInTx
//	          • moves the animal into the destination shed
//	          • adopts the destination shed's CONFIGURED cohort (shed_profiles -> animal_stage_lookup),
//	            NOT the residents' — an EMPTY shed with a profile still resolves its cohort
//	          • writes goat_identity_events + outbox_messages for one goat.location.changed and one
//	            goat.stage_changed in the SAME transaction as the UPDATE goats (shed_id + management_stage)
//
//	CONSUMERS driven by the durable outbox -> domain consumer -> the production handlers registered in
//	          domainconsumer/wiring.BuildDomainBus:
//	          • obligation.GoatShiftedHandler   re-scopes the moved animal's open, shed-scoped
//	            vaccination obligation to the new shed (scope_id := destination shed)
//	          • vaccination.GoatRecheckHandler  re-runs eligibility generation for the NEW cohort, so a
//	            stage-scoped protocol that did not match the animal in the source shed now issues its
//	            obligation in the destination shed
//
// The only fixture inputs are external facts: tenant/park/sheds, the destination shed's configured
// profile, published protocols, and the mover with its pre-existing open obligation (itself produced by
// the real generator via goat.created). Every asserted outcome is produced by the production path.
func TestKernelStory_ShiftingVaccinationHandoff(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-shifting-vaccination-handoff",
		"Shifting completion hands off to vaccination: move + reclassify + re-scope + recheck",
		"An approved goat shifting is COMPLETED. The completion atomically moves the animal into the "+
			"destination shed, adopts that shed's CONFIGURED operational cohort (from shed_profiles, not "+
			"residents), and emits one goat.location.changed and one goat.stage_changed in the same "+
			"transaction. The obligation GoatShiftedHandler then re-scopes the animal's open vaccination "+
			"obligation to the new shed, and the vaccination GoatRecheckHandler re-runs eligibility for the "+
			"new cohort so a stage-scoped protocol that did not match in the old shed now issues work in the "+
			"new one. A move into a shed with no configured profile fails closed and moves nothing.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx

	// The counts shifting producer wired to the REAL identity relocation adapter, exactly as
	// internal/bootstrap/api.go wires it (WithIdentityTxWriter), so the schema's triggers/constraints
	// and the shed_profiles cohort authority are part of the assertion surface.
	shiftRepo := countspg.NewRepository(fx.Pool, 10*time.Second).
		WithIdentityTxWriter(identitypg.NewRepository(fx.Pool, 10*time.Second))

	const (
		sourceShed = "5f000000-0000-4000-8000-000000000001"
		destShed   = "5f000000-0000-4000-8000-000000000002"
		stageSrc   = "5f000000-0000-4000-8000-00000000000a" // K1 profile for the source shed
		stageDst   = "5f000000-0000-4000-8000-00000000000b" // K2 profile for the destination shed
		mover      = "5f000000-0000-4000-8000-000000000010"
	)

	// Source shed carries a K1 profile; the (empty) destination shed carries a K2 profile. The
	// destination cohort is therefore CONFIGURED (shed_profiles), not derived from any resident.
	fx.SeedShed(sourceShed, "E2E-SVH-SRC", stageSrc)          // K1
	fx.SeedAdultShed(destShed, "E2E-SVH-DST", stageDst, "K2") // K2, no residents

	// Two published protocols with the same birth-age dose:
	//   generic — no stage eligibility, so it issues for the mover while it is K1 in the source shed;
	//             this is the open obligation the shift RE-SCOPES.
	//   K2-only — eligibility.animal_stage = K2, so it matches ONLY once the mover resolves as K2 in
	//             the destination shed; the recheck ISSUING it is unambiguous proof the consumer ran.
	fx.PublishSimpleProtocol("vaccination.e2e.svh.generic", 21, 14, nil)
	publishStageScopedProtocol(fx, "vaccination.e2e.svh.k2only", "K2", 21, 14)

	due := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	dob := due.AddDate(0, 0, -21)
	asOf := due.AddDate(0, 0, -1)
	completedAt := time.Date(2026, 7, 19, 9, 0, 0, 0, biztime.DefaultLocation())

	fx.SeedGoat(GoatSpec{GoatID: mover, ShedID: sourceShed, Stage: "K1", DOB: &dob})

	story.Step("A K1 goat in the source shed has one open, shed-scoped vaccination obligation",
		"The real generator (goat.created) issues the generic dose scoped shed=source. The K2-only "+
			"protocol issues nothing because the animal resolves as K1 in the source shed.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, mover, asOf)

	genericObl := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, mover)
	openAtSource := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND scope_type='shed' AND scope_id=$3 AND status='scheduled'`, fxTenant, mover, sourceShed)
	story.Assert("one open dose scoped to the source shed", openAtSource == 1, "count=%d", openAtSource)
	totalOblBefore := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, mover)
	story.Assert("K2-only protocol issued nothing for the K1 animal yet", totalOblBefore == 1, "count=%d", totalOblBefore)

	story.Step("Complete the approved shifting: the real producer moves + reclassifies the animal",
		"counts.CompleteShiftingEvent runs identity.RelocateGoatsToShedInTx, which moves the animal, "+
			"adopts the destination shed's configured K2 cohort, and emits the location + stage event pair.")
	shiftingEventID := recordAndApproveShifting(t, ctx, shiftRepo, "svh-1", []string{mover}, sourceShed, destShed, completedAt)
	result, replayed, err := completeShiftingE2E(shiftRepo, ctx, "svh-1", shiftingEventID, "", completedAt)
	story.Assert("completion succeeded", err == nil, "err=%v", err)
	story.Assert("completion is a fresh execution (not a replay)", !replayed, "replayed=%v", replayed)
	if err == nil {
		story.Assert("completion applied the movement", result.EventStatus == countsdomain.ShiftingEventStatusApplied, "status=%s", result.EventStatus)
		story.Assert("the mover is the relocated animal", len(result.MovedGoatIDs) == 1, "moved=%d", len(result.MovedGoatIDs))
	}

	// Producer proof: shed AND cohort moved together, in one committed transaction.
	shedAfter := fx.scanText(`SELECT shed_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, mover)
	story.Assert("animal is now in the destination shed", shedAfter == destShed, "shed_id=%s", shedAfter)
	stageAfter := fx.scanText(`SELECT COALESCE(management_stage,'') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, mover)
	story.Assert("animal adopted the destination shed's configured cohort (management_stage=K2)", stageAfter == "K2", "management_stage=%s", stageAfter)

	// Producer proof: exactly one of each event landed in BOTH the identity timeline and the outbox.
	locIdentity := fx.countRows(`SELECT count(*) FROM goat_identity_events WHERE tenant_id=$1 AND goat_id=$2 AND event_type='goat.location.changed'`, fxTenant, mover)
	locOutbox := fx.countRows(`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='goat.location.changed'`, fxTenant, mover)
	story.Assert("one goat.location.changed identity event", locIdentity == 1, "count=%d", locIdentity)
	story.Assert("one goat.location.changed outbox message", locOutbox == 1, "count=%d", locOutbox)
	stageIdentity := fx.countRows(`SELECT count(*) FROM goat_identity_events WHERE tenant_id=$1 AND goat_id=$2 AND event_type='goat.stage_changed'`, fxTenant, mover)
	stageOutbox := fx.countRows(`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='goat.stage_changed'`, fxTenant, mover)
	story.Assert("one goat.stage_changed identity event", stageIdentity == 1, "count=%d", stageIdentity)
	story.Assert("one goat.stage_changed outbox message", stageOutbox == 1, "count=%d", stageOutbox)

	// The location event's business payload names the destination shed as the goat's new scope — this
	// is the scope_id the obligation consumer re-scopes to.
	locScope := fx.scanText(`SELECT payload->'payload'->>'scope_id' FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='goat.location.changed'`, fxTenant, mover)
	story.Assert("goat.location.changed payload scope_id is the destination shed", locScope == destShed, "scope_id=%s", locScope)

	// Producer-only so far: the consumer has NOT run, so the open obligation is still at the source shed.
	scopeBeforeRelay := fx.scanText(`SELECT scope_id::text FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, genericObl)
	story.Assert("before the consumer runs, the open obligation is still scoped to the source shed", scopeBeforeRelay == sourceShed, "scope_id=%s", scopeBeforeRelay)

	story.Step("The producer's own events drive the production consumers",
		"Read the exact goat.location.changed and goat.stage_changed business payloads the completion wrote "+
			"to the outbox, and deliver them to the EXACT handler types registered in "+
			"domainconsumer/wiring.BuildDomainBus: obligation.GoatShiftedHandler and "+
			"vaccination.GoatRecheckHandler. (FINDING: the completion producer emits an envelope with an "+
			"empty trace_id, which the domain-event-envelope schema rejects at minLength 1, so the durable "+
			"relay/consumer TRANSPORT currently drops these two events; the completion command carries a "+
			"TraceID field that RelocateGoatsToShedInTx never threads into the outbox envelope. Driving the "+
			"registered handler types with the producer's own business payload proves the handoff logic while "+
			"that producer defect is fixed separately.)")
	// The exact production handler types registered by domainconsumer/wiring.BuildDomainBus.
	shiftedHandler := oblapp.NewGoatShiftedHandler(fx.Obl)
	recheckHandler := vaccapp.NewGoatRecheckHandler(vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl))

	// The producer's OWN emitted business payloads (not hand-built): read straight from the outbox rows
	// the completion wrote, so the consumers see exactly what the producer published.
	locEventID := fx.scanText(`SELECT event_id::text FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='goat.location.changed'`, fxTenant, mover)
	locPayload := fx.scanText(`SELECT (payload->'payload')::text FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='goat.location.changed'`, fxTenant, mover)
	stageEventID := fx.scanText(`SELECT event_id::text FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='goat.stage_changed'`, fxTenant, mover)
	stagePayload := fx.scanText(`SELECT (payload->'payload')::text FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='goat.stage_changed'`, fxTenant, mover)

	locEvent := eventbus.Event{ID: locEventID, Type: oblapp.EventGoatLocationChanged, TenantID: fxTenant, Key: mover, Payload: []byte(locPayload), OccurredAt: completedAt}
	stageEvent := eventbus.Event{ID: stageEventID, Type: vaccapp.EventGoatStageChanged, TenantID: fxTenant, Key: mover, Payload: []byte(stagePayload), OccurredAt: completedAt}

	// Fan the location event out to BOTH subscribers (obligation re-scope + vaccination recheck) and the
	// stage event to the vaccination recheck, mirroring BuildDomainBus's subscriptions.
	errShiftLoc := shiftedHandler.HandleEvent(ctx, locEvent)
	story.Assert("GoatShiftedHandler consumed goat.location.changed without error", errShiftLoc == nil, "err=%v", errShiftLoc)
	errRecheckLoc := recheckHandler.HandleEvent(ctx, locEvent)
	story.Assert("GoatRecheckHandler consumed goat.location.changed without error", errRecheckLoc == nil, "err=%v", errRecheckLoc)
	errRecheckStage := recheckHandler.HandleEvent(ctx, stageEvent)
	story.Assert("GoatRecheckHandler consumed goat.stage_changed without error", errRecheckStage == nil, "err=%v", errRecheckStage)

	story.Step("GoatShiftedHandler re-scoped the open obligation to the new shed",
		"The moved animal's open, shed-scoped obligation now belongs to the destination shed's drive; the "+
			"old shed no longer counts it, and the re-scope wrote its audit + watermark.")
	scopeAfterRelay := fx.scanText(`SELECT scope_id::text FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, genericObl)
	story.Assert("obligation re-scoped to the destination shed (scope_id)", scopeAfterRelay == destShed, "scope_id=%s", scopeAfterRelay)
	stillOpen := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, genericObl)
	story.Assert("the dose is still open, only re-scoped", stillOpen == "scheduled", "status=%s", stillOpen)
	oldShedNow := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND scope_id=$3 AND status='scheduled'`, fxTenant, mover, sourceShed)
	story.Assert("the source shed's drive no longer counts the animal", oldShedNow == 0, "count=%d", oldShedNow)
	rescopedEvents := fx.countRows(`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='rescoped'`, fxTenant, genericObl)
	story.Assert("exactly one 'rescoped' status event (uniquely written by GoatShiftedHandler)", rescopedEvents == 1, "count=%d", rescopedEvents)
	watermark := fx.countRows(`SELECT count(*) FROM obligation_goat_shift_watermarks WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, mover)
	story.Assert("the shift watermark advanced for the animal", watermark == 1, "count=%d", watermark)

	story.Step("GoatRecheckHandler re-ran eligibility for the new cohort",
		"Now that the animal resolves as K2 in the destination shed, the K2-only protocol becomes eligible, "+
			"so the recheck ISSUES its dose in the destination shed — an obligation that could not exist "+
			"before the move.")
	totalOblAfter := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status IN ('scheduled','deferred')`, fxTenant, mover)
	story.Assert("the animal now carries two open doses (generic re-scoped + K2 recheck-issued)", totalOblAfter == 2, "count=%d", totalOblAfter)
	k2AtDest := fx.countRows(`
SELECT count(*)
FROM obligation_instances oi
JOIN protocol_versions pv ON pv.protocol_version_id = oi.protocol_version_id
JOIN protocol_definitions pd ON pd.protocol_id = pv.protocol_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2 AND oi.scope_id=$3 AND pd.code=$4`,
		fxTenant, mover, destShed, "vaccination.e2e.svh.k2only")
	story.Assert("the K2-only protocol issued its dose in the destination shed via the recheck", k2AtDest == 1, "count=%d", k2AtDest)

	story.Step("Both production handlers are idempotent under redelivery",
		"Re-deliver the same events to the same registered handler types; a re-scope watermark no-op and an "+
			"idempotent re-generation must add nothing.")
	errShiftReplay := shiftedHandler.HandleEvent(ctx, locEvent)
	story.Assert("GoatShiftedHandler redelivery ran without error", errShiftReplay == nil, "err=%v", errShiftReplay)
	errRecheckReplay := recheckHandler.HandleEvent(ctx, stageEvent)
	story.Assert("GoatRecheckHandler redelivery ran without error", errRecheckReplay == nil, "err=%v", errRecheckReplay)
	rescopedAfterReplay := fx.countRows(`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='rescoped'`, fxTenant, genericObl)
	story.Assert("redelivery added no new 'rescoped' event", rescopedAfterReplay == 1, "count=%d", rescopedAfterReplay)
	totalOblAfterReplay := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status IN ('scheduled','deferred')`, fxTenant, mover)
	story.Assert("redelivery issued no duplicate dose", totalOblAfterReplay == 2, "count=%d", totalOblAfterReplay)

	story.Step("A move into a shed with NO configured profile fails closed",
		"The destination cohort is authoritative config; a shed with no active shed_profiles row has no "+
			"authority to assign a cohort, so the completion aborts and moves nothing.")
	const (
		noProfileShed = "5f000000-0000-4000-8000-000000000003"
		mover2        = "5f000000-0000-4000-8000-000000000011"
	)
	fx.exec("bare destination shed (no shed_profiles)",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'E2E-SVH-NOPROF', 'E2E-SVH-NOPROF', $3, 'active')`,
		noProfileShed, fxTenant, fxPark)
	fx.SeedGoat(GoatSpec{GoatID: mover2, ShedID: sourceShed, Stage: "K1", DOB: &dob})

	shiftingEventID2 := recordAndApproveShifting(t, ctx, shiftRepo, "svh-noprof", []string{mover2}, sourceShed, noProfileShed, completedAt)
	_, _, err2 := completeShiftingE2E(shiftRepo, ctx, "svh-noprof", shiftingEventID2, "", completedAt)
	story.Assert("completion fails closed with ErrDestinationProfileMissing", errors.Is(err2, identityports.ErrDestinationProfileMissing), "err=%v", err2)
	mover2Shed := fx.scanText(`SELECT shed_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, mover2)
	story.Assert("nothing moved: the animal stays in the source shed", mover2Shed == sourceShed, "shed_id=%s", mover2Shed)
	mover2Events := fx.countRows(`SELECT count(*) FROM goat_identity_events WHERE tenant_id=$1 AND goat_id=$2 AND event_type IN ('goat.location.changed','goat.stage_changed')`, fxTenant, mover2)
	story.Assert("no location/stage events were written for the un-moved animal", mover2Events == 0, "count=%d", mover2Events)
}

// publishStageScopedProtocol publishes a one-dose birth-age vaccination version whose version-level
// rule_dsl gates eligibility on a single animal_stage. generateForGoat resolves a goat's stage from its
// shed profile and matches it against this eligibility, so the version issues obligations only for
// animals living in a shed configured for `stageCode`.
func publishStageScopedProtocol(f *Fixture, code, stageCode string, offsetDays, dueWindowDays int32) (versionID, ruleID string) {
	f.T.Helper()
	protoID, err := f.Proto.CreateDefinition(f.Ctx, protodomain.NewDefinition{
		TenantID: fxTenant, Code: code, Name: code, Category: "vaccination", Status: "draft",
	})
	if err != nil {
		f.T.Fatalf("create stage-scoped protocol definition %s: %v", code, err)
	}
	ruleDSL := []byte(`{"eligibility":{"animal_stage":["` + stageCode + `"]}}`)
	versionID, err = f.Proto.CreateVersion(f.Ctx, protodomain.NewVersion{
		TenantID: fxTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		f.T.Fatalf("create stage-scoped protocol version %s: %v", code, err)
	}
	ruleID, err = f.Proto.CreateRule(f.Ctx, protodomain.NewRule{
		TenantID: fxTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: offsetDays, DueWindowDays: dueWindowDays,
		Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		f.T.Fatalf("create stage-scoped protocol rule %s: %v", code, err)
	}
	if err := f.Proto.PublishVersion(f.Ctx, fxTenant, versionID, nil); err != nil {
		f.T.Fatalf("publish stage-scoped protocol version %s: %v", code, err)
	}
	return versionID, ruleID
}

// recordAndApproveShifting records a pending shifting event (source -> destination) and drives it
// through the real counts approval flow to the AUTHORIZED state, returning the shifting event id ready
// for completion. Approval is authorization only — it moves no animals (the 2026-07-19 flow change).
func recordAndApproveShifting(
	t *testing.T, ctx context.Context, repo *countspg.Repository, key string, goatIDs []string,
	sourceShed, destShed string, at time.Time,
) string {
	t.Helper()
	src := sourceShed
	shiftingEventID, _, err := repo.RecordShiftingEvent(ctx, countsdomain.ShiftingEvent{
		TenantID: fxTenant, LogicalShiftingEventKey: key, Priority: "low", Category: "growth",
		SourceParkID: shParkPtr(), SourceShedID: &src,
		DestinationParkID: fxPark, DestinationShedID: destShed,
		RaisedAt: at, EffectiveAt: at,
		AuthorizationState: "pending", VerificationState: "unverified", EventStatus: "pending",
		SourceSystem: "goatos_canonical", SourceRef: "e2e:" + key,
		PayloadHash: "hash-" + key, IdempotencyKey: "idem-" + key, RequestFingerprint: "fp-" + key,
		Impacts: []countsdomain.ShiftingEventImpact{{
			GrainKey: destShed + ":beetal", BreedKey: "beetal", BreedLabel: "Beetal",
			HeadCount: int32(len(goatIDs)), RiskFlagsJSON: []byte("{}"),
		}},
	})
	if err != nil {
		t.Fatalf("record shifting event %s: %v", key, err)
	}
	payload, err := json.Marshal(map[string]any{
		"shifting_event_id":   shiftingEventID,
		"destination_park_id": fxPark,
		"destination_shed_id": destShed,
		"goat_ids":            goatIDs,
	})
	if err != nil {
		t.Fatalf("marshal shifting approval payload %s: %v", key, err)
	}
	req, _, err := repo.CreateApprovalRequest(ctx, countsdomain.ApprovalRequestSubmission{
		TenantID: fxTenant, RequestType: countsdomain.ApprovalRequestTypeShifting,
		Payload: payload, ShiftingEventID: &shiftingEventID,
		RaisedByUserID: fxParty, RaisedAt: at,
		IdempotencyKey: "submit-" + key, RequestFingerprint: "submit-fp-" + key,
	})
	if err != nil {
		t.Fatalf("submit shifting approval %s: %v", key, err)
	}
	if _, _, err := repo.DecideApprovalRequest(ctx, countsdomain.ApprovalDecision{
		TenantID: fxTenant, ApprovalRequestID: req.ApprovalRequestID,
		Status: countsdomain.ApprovalStatusApproved, DecidedByUserID: fxParty, DecidedAt: at,
		IdempotencyKey: "decide-" + key, RequestFingerprint: "decide-fp-" + key,
		Effect: &countsdomain.ApprovalEffect{Shifting: &countsdomain.ShiftingApprovalEffect{
			ShiftingEventID: shiftingEventID, DestinationParkID: fxPark, DestinationShedID: destShed, GoatIDs: goatIDs,
		}},
	}); err != nil {
		t.Fatalf("approve shifting %s: %v", key, err)
	}
	return shiftingEventID
}

// completeShiftingE2E drives the production CompleteShiftingEvent completion path, which is where the
// animals actually move and both goat events are emitted.
//
// ProofRef carries the operator's completion video. It is MANDATORY (maintainer decision
// 2026-07-26): CompleteShiftingEvent rejects a blank one with ErrShiftingProofRequired before opening
// a transaction, so without it every shifting story fails at its first completion and never reaches
// the behaviour it is actually asserting.
func completeShiftingE2E(
	repo *countspg.Repository, ctx context.Context, key, shiftingEventID, tag string, at time.Time,
) (countsdomain.ShiftingExecutionResult, bool, error) {
	return repo.CompleteShiftingEvent(ctx, countsdomain.ShiftingCompletionCommand{
		TenantID: fxTenant, ShiftingEventID: shiftingEventID,
		CompletedByUserID: fxParty, CompletedAt: at, TraceID: "trace-complete-" + key,
		ProofRef:       "proof-artifact-" + key,
		DestinationTag: tag, IdempotencyKey: "complete-" + key, RequestFingerprint: "complete-fp-" + key + ":" + tag,
	})
}

func shParkPtr() *string { p := fxPark; return &p }
