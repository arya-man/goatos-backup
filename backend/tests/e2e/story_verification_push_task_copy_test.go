package e2e

// Verification pushes are TASK-level and lead with the item's own subject (maintainer decision
// 2026-09-12). The phone showed, for one pen move, "Counts video pending · Pen move · Gandhi 2 ·
// from Ho Chi Minh 1 · 1 animals recorded; ..." followed by "Counts proof verified · counts proof
// for Sumathi 1 (Coimbatore) is verified." -- the second push had lost what was verified.
//
// This story drives the PRODUCTION chain end to end and reads the pushes off notification_requests:
//
//	counts.ShiftingExecutionService.Complete (real producer, real identity relocation)
//	  -> countsbridge.ShiftingVerificationEnqueuer -> verification.CreateItem
//	  -> outbox verification.item.pending -> outbox relay -> domain consumer bus
//	  -> notificationbridge.VerificationEventConsumer (real resolvers) -> notification_requests
//	verification.RecordVerdict(approved)
//	  -> outbox verification.verdict.approved -> same relay/bus -> notification_requests
//
// projection-review: one verification item per shifting event (CreateItem is idempotent on the
// enqueue key), one notification_requests row per (event, recipient device); the assertions read
// rows by target item and recipient token, so no join can fan out.

import (
	"strings"
	"testing"
	"time"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/countsbridge"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
)

func TestKernelStory_VerificationPushCopyIsTaskLevel(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-verification-push-task-copy",
		"Verification pushes name the task and carry the item's own subject through every stage",
		"A pen move is completed with its video. The pending push to leadership, the verifier's own "+
			"push, and the approved push after the verdict must all lead with the SAME subject the "+
			"producer composed -- 'Pen move · <to> · from <from> · N animals' plus the park -- and be "+
			"titled by the task ('Pen move …'), never by the module ('Counts …'). Before this, the "+
			"approved push rebuilt a sentence from module + shed and dropped the movement entirely.")
	defer story.Finish()
	story.Certify("backend kernel")
	ctx := fx.Ctx

	const (
		sourceShed = "7c000000-0000-4000-8000-000000000001"
		destShed   = "7c000000-0000-4000-8000-000000000002"
		stageSrc   = "7c000000-0000-4000-8000-00000000000a"
		stageDst   = "7c000000-0000-4000-8000-00000000000b"
		goatA      = "7c000000-0000-4000-8000-000000000010"
		goatB      = "7c000000-0000-4000-8000-000000000011"

		operator       = "7c000000-0000-4000-8000-000000000101"
		parkHead       = "7c000000-0000-4000-8000-000000000102"
		verifier       = "7c000000-0000-4000-8000-000000000103"
		healthDirector = "7c000000-0000-4000-8000-000000000104"
		ceo            = "7c000000-0000-4000-8000-000000000105"

		tokenOperator = "tok-7c-operator"
		tokenParkHead = "tok-7c-parkhead"
		tokenVerifier = "tok-7c-verifier"
		tokenDirector = "tok-7c-health-director"
		tokenCEO      = "tok-7c-ceo"
	)

	// External input facts only: two named sheds in the baseline park, two animals in the source
	// shed, and the people + phones every push resolves to (the same roster tables production reads).
	fx.SeedShed(sourceShed, "Ho Chi Minh 1", stageSrc)
	fx.SeedAdultShed(destShed, "Gandhi 2", stageDst, "K2")
	dob := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatA, ShedID: sourceShed, Stage: "K1", DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: goatB, ShedID: sourceShed, Stage: "K1", DOB: &dob})
	seedPushPeople(fx, sourceShed, operator, parkHead, verifier, healthDirector, ceo,
		tokenOperator, tokenParkHead, tokenVerifier, tokenDirector, tokenCEO)
	parkName := fx.scanText(`SELECT name FROM locations WHERE tenant_id=$1 AND location_id=$2`, fxTenant, fxPark)

	// The production wiring: counts repo with the real identity relocation writer, the verification
	// service with the SAME shifting category bootstrap registers, and the bridge that turns a
	// completion into a verification item.
	shiftRepo := countspg.NewRepository(fx.Pool, 10*time.Second).
		WithIdentityTxWriter(identitypg.NewRepository(fx.Pool, 10*time.Second))
	verification := verificationapp.NewService(fx.VerifRepo, fx.VerifMedia)
	if err := verification.RegisterCategory(verificationcatalog.Shifting); err != nil {
		t.Fatalf("register shifting verification category: %v", err)
	}
	completedAt := time.Date(2026, 9, 12, 9, 30, 0, 0, biztime.DefaultLocation())
	shifting := countsapp.NewShiftingExecutionService(shiftRepo, func() time.Time { return completedAt }).
		WithVerificationEnqueuer(countsbridge.NewShiftingVerificationEnqueuer(verification))

	story.Step("An approved pen move is completed with its video through the real producer",
		"counts.ShiftingExecutionService.Complete relocates both animals and enqueues ONE verification "+
			"item whose subject the producer composes: destination, source and animal count.")
	shiftingEventID := recordAndApproveShifting(t, ctx, shiftRepo, "push-copy-1", []string{goatA, goatB}, sourceShed, destShed, completedAt)
	result, replayed, err := shifting.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: fxTenant, ShiftingEventID: shiftingEventID, CompletedByUserID: operator,
		TraceID: "trace-push-copy-1", ProofRef: "proof-push-copy-1",
		IdempotencyKey: "complete-push-copy-1", RequestFingerprint: "complete-push-copy-1-fp",
	})
	if !story.Assert("completion succeeded", err == nil && !replayed, "err=%v replayed=%v", err, replayed) {
		return
	}
	story.Assert("completion applied the movement", result.EventStatus == countsdomain.ShiftingEventStatusApplied, "status=%s", result.EventStatus)

	itemID := fx.scanText(`SELECT item_id::text FROM verification_items WHERE tenant_id=$1 AND source_ref_id=$2`, fxTenant, shiftingEventID)
	subject := fx.scanText(`SELECT coalesce(subject_label,'') FROM verification_items WHERE tenant_id=$1 AND item_id=$2`, fxTenant, itemID)
	const wantSubject = "Pen move · Gandhi 2 · from Ho Chi Minh 1 · 2 animals"
	story.Assert("the item's subject names the move: to, from, and how many", subject == wantSubject, "subject=%q", subject)
	head := wantSubject + " (" + parkName + ")"

	story.Step("The outbox relay delivers verification.item.pending to the production consumer",
		"The same relay + domain bus production runs. The verifier's push and the leadership push "+
			"are read straight off notification_requests.")
	fx.RelayOutboxEvents()

	verifierPush := pushFor(fx, itemID, tokenVerifier, "verification_pending")
	story.Assert("verifier title names the TASK, not the module", verifierPush.title == "Pen move video to verify", "title=%q", verifierPush.title)
	story.Assert("verifier body leads with the item's own subject and park",
		verifierPush.body == head+" — video is waiting for your verification.", "body=%q", verifierPush.body)

	for _, who := range []struct{ name, token string }{{"park head", tokenParkHead}, {"health director", tokenDirector}, {"CEO", tokenCEO}} {
		push := pushFor(fx, itemID, who.token, "verification_pending")
		story.Assert(who.name+" pending title is 'Pen move video pending'", push.title == "Pen move video pending", "title=%q", push.title)
		story.Assert(who.name+" pending body leads with the same subject",
			push.body == head+" — video verification is pending.", "body=%q", push.body)
		story.Assert(who.name+" pending copy never says 'counts'", !strings.Contains(strings.ToLower(push.title+push.body), "counts"), "%q / %q", push.title, push.body)
	}
	story.Assert("the operator is not told about a pending review of their own video",
		fx.countRows(`SELECT count(*) FROM notification_requests WHERE tenant_id=$1 AND target_id=$2 AND recipient_ref=$3 AND notification_type='verification_pending'`,
			fxTenant, itemID, tokenOperator) == 0, "operator received a pending push")

	story.Step("The verifier approves; the approved push carries the SAME subject",
		"verification.RecordVerdict(approved) writes verification.verdict.approved to the outbox; "+
			"relayed through the same bus, the approved push must read 'Pen move verified' with the "+
			"movement in the body -- the exact push that used to say 'counts proof for <pen> is verified.'")
	rowVersion := fx.countRows(`SELECT row_version FROM verification_items WHERE tenant_id=$1 AND item_id=$2`, fxTenant, itemID)
	if _, err := verification.RecordVerdict(ctx, verificationdomain.Verdict{
		TenantID: fxTenant, ItemID: itemID, Decision: verificationdomain.DecisionApproved,
		VerifierID: verifier, RowVersion: rowVersion, IdempotencyKey: "verdict-push-copy-1",
	}); !story.Assert("verdict recorded", err == nil, "err=%v", err) {
		return
	}
	itemStatus := fx.scanText(`SELECT status FROM verification_items WHERE tenant_id=$1 AND item_id=$2`, fxTenant, itemID)
	story.Assert("the item is approved", itemStatus == verificationdomain.StatusApproved, "status=%s", itemStatus)
	verdictOutbox := fx.countRows(`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='verification.verdict.approved' AND aggregate_id=$2`, fxTenant, itemID)
	story.Assert("the verdict wrote one verification.verdict.approved outbox row", verdictOutbox == 1, "count=%d", verdictOutbox)
	fx.RelayOutboxEvents()
	verdictOutboxState := fx.scanText(`SELECT status || ' ' || coalesce(last_error,'') FROM outbox_messages WHERE tenant_id=$1 AND event_type='verification.verdict.approved' AND aggregate_id=$2`, fxTenant, itemID)
	story.Assert("the verdict outbox row was published", strings.HasPrefix(verdictOutboxState, "published"), "state=%q", verdictOutboxState)

	for _, who := range []struct{ name, token string }{{"park head", tokenParkHead}, {"health director", tokenDirector}, {"CEO", tokenCEO}} {
		push := pushFor(fx, itemID, who.token, "verification_approved")
		story.Assert(who.name+" approved title is 'Pen move verified'", push.title == "Pen move verified", "title=%q", push.title)
		story.Assert(who.name+" approved body is the subject + park + verdict",
			push.body == head+" — video verified.", "body=%q", push.body)
		story.Assert(who.name+" approved copy keeps the counts.* localization key",
			push.messageKey == "counts.proof.approved", "message_key=%q", push.messageKey)
	}
	pendingBody := pushFor(fx, itemID, tokenDirector, "verification_pending").body
	approvedBody := pushFor(fx, itemID, tokenDirector, "verification_approved").body
	story.Assert("pending and approved pushes share one subject line",
		strings.TrimSuffix(pendingBody, " — video verification is pending.") == strings.TrimSuffix(approvedBody, " — video verified."),
		"pending=%q approved=%q", pendingBody, approvedBody)
}

type queuedPush struct{ title, body, messageKey string }

// pushFor reads ONE queued push by item, recipient device token and type -- a DB round-trip on the
// real notification_requests table, never a field-presence check on an in-memory fake.
func pushFor(fx *Fixture, itemID, token, notificationType string) queuedPush {
	fx.T.Helper()
	var p queuedPush
	if err := fx.Pool.QueryRow(fx.Ctx, `
SELECT title, body, coalesce(context->>'message_key','')
FROM notification_requests
WHERE tenant_id=$1::uuid AND target_type='verification_item' AND target_id=$2 AND recipient_ref=$3 AND notification_type=$4
ORDER BY created_at DESC LIMIT 1`, fxTenant, itemID, token, notificationType).Scan(&p.title, &p.body, &p.messageKey); err != nil {
		fx.T.Errorf("no %s push for item %s to %s: %v", notificationType, itemID, token, err)
	}
	return p
}

// seedPushPeople seeds the roster rows, positions, the counts verify duty and one active phone per
// person -- the input facts the production recipient resolver and audience catalog read.
func seedPushPeople(fx *Fixture, shedID, operator, parkHead, verifier, healthDirector, ceo string, tokens ...string) {
	fx.T.Helper()
	member := func(id, code, name, hint, locationID string) {
		fx.exec("member "+code,
			`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
			 VALUES ($1, $2, $3, $4, 'active', $5, $6)`, id, fxTenant, code, name, hint, locationID)
	}
	member(operator, "PC-OP", "Push Operator", "operator", shedID)
	member(parkHead, "PC-PH", "Push Park Head", "park_head", fxPark)
	member(verifier, "PC-VER", "Push Verifier", "verifier", fxPark)
	member(healthDirector, "PC-HD", "Push Health Director", "other", fxPark)
	member(ceo, "PC-CEO", "Push CEO", "other", fxPark)
	fx.exec("operator user id",
		`UPDATE workforce_members SET user_id = $1 WHERE tenant_id = $2 AND workforce_member_id = $3`, operator, fxTenant, operator)

	position := func(memberID, code, scopeType, scopeID, tier string) {
		fx.exec("position "+code,
			`INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, status, valid_from)
			 VALUES ($1, $2, $3, $4, $5, $6, 'active', now() - interval '1 hour')`, fxTenant, memberID, scopeType, scopeID, code, tier)
	}
	position(verifier, "counts_verifier", "center", fxPark, "manager")
	position(parkHead, "park_head", "center", fxPark, "head")
	position(healthDirector, "health_director", "tenant", fxTenant, "director")
	position(ceo, "ceo_internal", "tenant", fxTenant, "cxo")
	fx.exec("counts verify duty",
		`INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, status, effective_from)
		 VALUES ($1, 'counts_verifier', 'counts', 'verify', 'active', now() - interval '1 hour')`, fxTenant)

	people := []string{operator, parkHead, verifier, healthDirector, ceo}
	for i, memberID := range people {
		fx.exec("device "+tokens[i],
			`INSERT INTO workforce_member_devices (device_id, tenant_id, workforce_member_id, platform, app_install_id, fcm_token, app_version, os_version, status, last_seen_at, registered_by)
			 VALUES (gen_random_uuid(), $1, $2, 'android', $3, $3, '1.0.0', '14', 'active', now(), $2)`, fxTenant, memberID, tokens[i])
	}
}
