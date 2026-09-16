package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	shiftingsoppg "github.com/vgoats/goatos/backend/internal/shiftingsop/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// SHIFTING SOP (2026-09-16) on the REAL adapter: the completion transaction stores the judged
// {slot: ref} map and answers, mirrors the legacy columns, satisfies the re-added feed-evidence
// CHECK with an authored high-priority card, still refuses a changed Feed Config fingerprint, keeps
// an in-flight movement on its pinned version after a later publish, and a rework resubmit queues a
// fresh item while the applied move stays applied.

// publishShiftingSOP inserts a published `shifting` version for the counts test tenant at the next
// free version number (the migrated template may already carry the seeded one) and returns it.
// Earlier published versions are retired, as the library does.
func publishShiftingSOP(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rules domain.ShiftingSOP) int {
	t.Helper()
	var sopID string
	err := pool.QueryRow(ctx, `SELECT sop_id::text FROM sop_definitions WHERE tenant_id = $1::uuid AND code = 'shifting'`, countsTenant).Scan(&sopID)
	if err != nil {
		if err := pool.QueryRow(ctx, `INSERT INTO sop_definitions (tenant_id, code, name, description, status)
VALUES ($1::uuid, 'shifting', 'Shifting', 'test', 'active') RETURNING sop_id::text`, countsTenant).Scan(&sopID); err != nil {
			t.Fatalf("seed shifting definition: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE sop_versions SET status = 'retired' WHERE tenant_id = $1::uuid AND sop_id = $2::uuid AND status = 'published'`, countsTenant, sopID); err != nil {
		t.Fatalf("retire: %v", err)
	}
	var version int
	if err := pool.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM sop_versions WHERE tenant_id = $1::uuid AND sop_id = $2::uuid`, countsTenant, sopID).Scan(&version); err != nil {
		t.Fatalf("next version: %v", err)
	}
	form := map[string]any{"schema_version": "goatos.sop-form.v1", "sop_code": "shifting", "title": "Shifting", "fields": []any{}, "rules": []any{}, "shifting": rules}
	raw, _ := json.Marshal(form)
	if _, err := pool.Exec(ctx, `INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
VALUES ($1::uuid, $2::uuid, $3, $4, 'published', $5::jsonb, '{}'::jsonb, '{}'::jsonb, '{"valid": true}'::jsonb, now())`, countsTenant, sopID, version, fmt.Sprintf("v%d", version), raw); err != nil {
		t.Fatalf("publish v%d: %v", version, err)
	}
	return version
}

func seededShiftingDoc(t *testing.T) domain.ShiftingSOP {
	t.Helper()
	dsl, err := domain.ParseShiftingSOP(map[string]any{"shifting": json.RawMessage(domain.SeededShiftingSOPJSON())})
	if err != nil {
		t.Fatal(err)
	}
	return dsl
}

func sopExecutionService(pool *pgxpool.Pool, repo *Repository, capture *capturingShiftingVerificationEnqueuer) *countsapp.ShiftingExecutionService {
	return countsapp.NewShiftingExecutionService(repo, time.Now).
		WithVerificationEnqueuer(capture).
		WithSOPRules(shiftingsoppg.NewRulesSource(pool, 10*time.Second), repo)
}

func readStoredShiftingSOP(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID string) (proofRef, packing, feeding *string, sopProofs authored.ProofRefs, sopAnswers authored.Answers, version *int) {
	t.Helper()
	var proofsRaw, answersRaw []byte
	if err := pool.QueryRow(ctx, `SELECT proof_ref, feed_packing_proof_ref, feed_given_proof_ref, sop_proofs, sop_answers, sop_version
FROM shifting_events WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`, countsTenant, eventID).
		Scan(&proofRef, &packing, &feeding, &proofsRaw, &answersRaw, &version); err != nil {
		t.Fatalf("read stored sop: %v", err)
	}
	_ = json.Unmarshal(proofsRaw, &sopProofs)
	_ = json.Unmarshal(answersRaw, &sopAnswers)
	return
}

func TestCompleteShiftingStoresSOPProofsAndMirrorsLegacyColumns(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)
	goatID := "00000000-0000-4000-8000-00000000e001"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "sop-store", []string{goatID})
	if _, _, err := approveShifting(repo, ctx, "sop-store", approvalID, eventID, []string{goatID}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	capture := &capturingShiftingVerificationEnqueuer{}
	svc := sopExecutionService(pool, repo, capture)
	// A NEW-shaped request on the seeded card (no published version: the seed, version 0).
	result, replay, err := svc.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		SOPProofs:      authored.ProofRefs{domain.SlotShiftingVideo: "proof-sop-1"},
		IdempotencyKey: "complete-sop-store", RequestFingerprint: "complete-sop-store-fp",
	})
	if err != nil || replay || result.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("complete result=%+v replay=%v err=%v", result, replay, err)
	}
	proofRef, packing, feeding, stored, answers, version := readStoredShiftingSOP(t, ctx, pool, eventID)
	if proofRef == nil || *proofRef != "proof-sop-1" || packing != nil || feeding != nil {
		t.Fatalf("legacy mirrors = %v %v %v", proofRef, packing, feeding)
	}
	if stored[domain.SlotShiftingVideo] != "proof-sop-1" || len(stored) != 1 || len(answers) != 0 || version != nil {
		t.Fatalf("stored sop = %v answers=%v version=%v", stored, answers, version)
	}
	if capture.request.IdempotencyKey != "counts-shifting-verification:"+eventID+":proof-sop-1" ||
		len(capture.request.MediaMeta) != 1 || capture.request.MediaMeta[0].Label != "Shifting video" {
		t.Fatalf("enqueue = %+v", capture.request)
	}
	// An exact replay answers from the row and re-queues the same key.
	capture.request = countsapp.ShiftingVerificationEnqueueRequest{}
	result, replay, err = svc.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		SOPProofs:      authored.ProofRefs{domain.SlotShiftingVideo: "proof-sop-1"},
		IdempotencyKey: "complete-sop-store", RequestFingerprint: "complete-sop-store-fp",
	})
	if err != nil || !replay || len(result.SOPProofs) != 1 {
		t.Fatalf("replay result=%+v replay=%v err=%v", result, replay, err)
	}
	if capture.request.IdempotencyKey != "counts-shifting-verification:"+eventID+":proof-sop-1" {
		t.Fatalf("replay re-keyed the item: %s", capture.request.IdempotencyKey)
	}
}

// An authored high-priority card with ONE `either` slot (no seeded feed keys): the row stores the
// capture under its own key, the legacy feed columns stay NULL beside a non-blank fingerprint, and
// the re-added CHECK (000325) accepts it where the 000053 shape would have refused.
func TestHighPrioritySectionWithoutSeededFeedSlotsSatisfiesAuthoredCheck(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)
	doc := seededShiftingDoc(t)
	doc.HighPriority.Proofs = []authored.ProofSlot{{Key: "feed_clip", Title: "Feed clip", Kind: authored.KindEither, Required: true}}
	pinned := publishShiftingSOP(t, ctx, pool, doc)

	goatID := "00000000-0000-4000-8000-00000000e002"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "sop-high", []string{goatID})
	seedHighPriorityShiftingFeedConfig(t, ctx, pool, goatID, eventID)
	if _, err := pool.Exec(ctx, `UPDATE shifting_events SET sop_version = $3 WHERE tenant_id=$1::uuid AND shifting_event_id=$2::uuid`, countsTenant, eventID, pinned); err != nil {
		t.Fatal(err)
	}
	if _, _, err := approveShifting(repo, ctx, "sop-high", approvalID, eventID, []string{goatID}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	requirements, err := loadShiftingFeedRequirements(ctx, pool, countsTenant, []string{eventID}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := requirements[eventID].Fingerprint
	capture := &capturingShiftingVerificationEnqueuer{}
	svc := sopExecutionService(pool, repo, capture)

	// The seeded feed keys are NOT on this card: refused by name.
	_, _, err = svc.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		SOPProofs:      authored.ProofRefs{domain.SlotShiftingVideo: "v1", domain.SlotShiftingPackingVideo: "p1"},
		IdempotencyKey: "complete-sop-high-bad", RequestFingerprint: "fp-bad", FeedConfigFingerprint: fingerprint,
	})
	if key, _, ok := countsapp.SOPProofSlotError(err); !ok || key != domain.SlotShiftingPackingVideo {
		t.Fatalf("err=%v key=%q", err, key)
	}
	result, _, err := svc.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		SOPProofs:      authored.ProofRefs{domain.SlotShiftingVideo: "v1", "feed_clip": "f1"},
		IdempotencyKey: "complete-sop-high", RequestFingerprint: "fp-high", FeedConfigFingerprint: fingerprint,
	})
	if err != nil || result.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	proofRef, packing, feeding, stored, _, _ := readStoredShiftingSOP(t, ctx, pool, eventID)
	if proofRef == nil || *proofRef != "v1" || packing != nil || feeding != nil || stored["feed_clip"] != "f1" {
		t.Fatalf("stored proof_ref=%v packing=%v feeding=%v sop=%v", proofRef, packing, feeding, stored)
	}
	var storedFingerprint string
	if err := pool.QueryRow(ctx, `SELECT feed_config_fingerprint FROM shifting_events WHERE tenant_id=$1::uuid AND shifting_event_id=$2::uuid`, countsTenant, eventID).Scan(&storedFingerprint); err != nil || storedFingerprint != fingerprint {
		t.Fatalf("fingerprint=%q err=%v", storedFingerprint, err)
	}
	if strings.Join(capture.request.MediaRefs, ",") != "v1,f1" || capture.request.MediaMeta[1].Label != "Feed clip" {
		t.Fatalf("enqueue = %+v", capture.request)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
		t.Fatalf("goat shed=%s want %s", got, countsShedB)
	}
}

func TestFeedConfigChangedStillRefusedUnderAuthoredHighSection(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)
	doc := seededShiftingDoc(t)
	doc.HighPriority.Proofs = []authored.ProofSlot{{Key: "feed_clip", Title: "Feed clip", Kind: authored.KindVideo, Required: true}}
	pinned := publishShiftingSOP(t, ctx, pool, doc)
	goatID := "00000000-0000-4000-8000-00000000e003"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "sop-high-changed", []string{goatID})
	seedHighPriorityShiftingFeedConfig(t, ctx, pool, goatID, eventID)
	if _, err := pool.Exec(ctx, `UPDATE shifting_events SET sop_version = $3 WHERE tenant_id=$1::uuid AND shifting_event_id=$2::uuid`, countsTenant, eventID, pinned); err != nil {
		t.Fatal(err)
	}
	if _, _, err := approveShifting(repo, ctx, "sop-high-changed", approvalID, eventID, []string{goatID}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	svc := sopExecutionService(pool, repo, &capturingShiftingVerificationEnqueuer{})
	_, _, err := svc.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		SOPProofs:      authored.ProofRefs{domain.SlotShiftingVideo: "v1", "feed_clip": "f1"},
		IdempotencyKey: "complete-sop-high-changed", RequestFingerprint: "fp", FeedConfigFingerprint: "stale-fingerprint",
	})
	if !errors.Is(err, ports.ErrShiftingFeedConfigChanged) {
		t.Fatalf("err=%v, want ErrShiftingFeedConfigChanged", err)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
		t.Fatalf("goat moved on a refused completion: %s", got)
	}
}

// Raised on v1 (a required completion question), then v2 is published without it: the in-flight
// completion is still judged on v1 and the row keeps its pin.
func TestPublishAfterRaiseLeavesInFlightMoveOnItsPin(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)
	v1 := seededShiftingDoc(t)
	v1.Completion.Questions = []authored.Question{{ID: "calm", Kind: authored.QuestionChoice, Title: "Animals calm?", Required: true,
		Options: []authored.Option{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}}}
	v1Version := publishShiftingSOP(t, ctx, pool, v1)
	goatID := "00000000-0000-4000-8000-00000000e004"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	event := shiftingEventForApproval("sop-pin")
	event.SOPVersion = intPtr(v1Version)
	eventID, _, err := repo.RecordShiftingEvent(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"shifting_event_id": eventID, "destination_park_id": countsPark, "destination_shed_id": countsShedB, "goat_ids": []string{goatID}})
	req, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID: countsTenant, RequestType: domain.ApprovalRequestTypeShifting, Payload: payload, ShiftingEventID: &eventID,
		RaisedByUserID: countsOperator, RaisedAt: time.Now(), IdempotencyKey: "submit-sop-pin", RequestFingerprint: "submit-fp-sop-pin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := approveShifting(repo, ctx, "sop-pin", req.ApprovalRequestID, eventID, []string{goatID}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// v2 published AFTER the raise: no question at all.
	publishShiftingSOP(t, ctx, pool, seededShiftingDoc(t))
	capture := &capturingShiftingVerificationEnqueuer{}
	svc := sopExecutionService(pool, repo, capture)
	_, _, err = svc.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		SOPProofs: authored.ProofRefs{domain.SlotShiftingVideo: "v1"}, IdempotencyKey: "complete-sop-pin-1", RequestFingerprint: "fp1",
	})
	if id, _, ok := countsapp.SOPAnswerError(err); !ok || id != "calm" {
		t.Fatalf("v1's question not demanded after v2 publish: err=%v", err)
	}
	yes, _ := json.Marshal("yes")
	result, _, err := svc.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		SOPProofs: authored.ProofRefs{domain.SlotShiftingVideo: "v1"}, SOPAnswers: authored.Answers{"calm": yes},
		IdempotencyKey: "complete-sop-pin-2", RequestFingerprint: "fp2",
	})
	if err != nil || result.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	_, _, _, _, answers, version := readStoredShiftingSOP(t, ctx, pool, eventID)
	if version == nil || *version != v1Version || string(answers["calm"]) != `"yes"` {
		t.Fatalf("pin=%v answers=%v", version, answers)
	}
	found := false
	for _, row := range capture.request.ContextRows {
		if row.Label == "Animals calm?" && row.Value == "Yes" && row.Group == "Completion" {
			found = true
		}
	}
	if !found {
		t.Fatalf("answer row missing: %+v", capture.request.ContextRows)
	}
}

func TestReworkResubmitCreatesFreshItemAndKeepsApplied(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)
	goatID := "00000000-0000-4000-8000-00000000e005"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "sop-rework", []string{goatID})
	if _, _, err := approveShifting(repo, ctx, "sop-rework", approvalID, eventID, []string{goatID}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	capture := &capturingShiftingVerificationEnqueuer{}
	svc := sopExecutionService(pool, repo, capture)
	if _, _, err := svc.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		SOPProofs: authored.ProofRefs{domain.SlotShiftingVideo: "first"}, IdempotencyKey: "complete-sop-rework-1", RequestFingerprint: "fp1",
	}); err != nil {
		t.Fatal(err)
	}
	firstKey := capture.request.IdempotencyKey
	if err := repo.BounceShiftingEventForRework(ctx, domain.ShiftingReworkCommand{TenantID: countsTenant, ShiftingEventID: eventID, VerifiedBy: countsApprover, Reason: "dark", EvidenceRefs: []string{"first"}}); err != nil {
		t.Fatal(err)
	}
	// A re-shoot with a FRESH capture queues a fresh item and keeps the move applied.
	result, _, err := svc.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		SOPProofs: authored.ProofRefs{domain.SlotShiftingVideo: "second"}, IdempotencyKey: "complete-sop-rework-2", RequestFingerprint: "fp2",
	})
	if err != nil || result.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("rework result=%+v err=%v", result, err)
	}
	if capture.request.IdempotencyKey == firstKey || capture.request.MediaRefs[0] != "second" {
		t.Fatalf("rework did not queue a fresh item: %+v", capture.request)
	}
	secondKey := capture.request.IdempotencyKey
	if err := repo.BounceShiftingEventForRework(ctx, domain.ShiftingReworkCommand{TenantID: countsTenant, ShiftingEventID: eventID, VerifiedBy: countsApprover, Reason: "dark again", EvidenceRefs: []string{"second"}}); err != nil {
		t.Fatal(err)
	}
	// Resubmitting the rejected clip is accepted exactly as before the SOP card existed
	// (deploy-day parity) and the move stays applied -- but it is a NEW review round, so it queues a
	// fresh item instead of collapsing onto the rejected one (coordinator follow-up 2026-09-17).
	reused, _, err := svc.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		SOPProofs: authored.ProofRefs{domain.SlotShiftingVideo: "second"}, IdempotencyKey: "complete-sop-rework-3", RequestFingerprint: "fp3",
	})
	if err != nil || reused.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("reuse result=%+v err=%v", reused, err)
	}
	if capture.request.IdempotencyKey == secondKey || capture.request.MediaRefs[0] != "second" {
		t.Fatalf("reused clip key = %q collapsed onto the rejected item %q", capture.request.IdempotencyKey, secondKey)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
		t.Fatalf("rework rolled the move back: %s", got)
	}
	proofRef, _, _, stored, _, _ := readStoredShiftingSOP(t, ctx, pool, eventID)
	if proofRef == nil || *proofRef != "second" || stored[domain.SlotShiftingVideo] != "second" {
		t.Fatalf("stored after rework = %v %v", proofRef, stored)
	}
}

func intPtr(v int) *int { return &v }
