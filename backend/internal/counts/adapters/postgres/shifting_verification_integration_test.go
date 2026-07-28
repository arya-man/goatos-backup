package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

type capturingShiftingVerificationEnqueuer struct {
	request countsapp.ShiftingVerificationEnqueueRequest
}

func (c *capturingShiftingVerificationEnqueuer) EnqueueShiftingMoveVerification(_ context.Context, in countsapp.ShiftingVerificationEnqueueRequest) error {
	c.request = in
	return nil
}

// Shifting evidence review -- proofs of the maintainer-2026-07-28 rule against the REAL identity
// Postgres adapter (the same wiring bootstrap uses, so the schema's triggers/constraints are part of
// the assertion surface).
//
// Approval + operator completion own relocation/count. Verification accepts or rejects mandatory
// video evidence afterward and cannot move or roll back the herd.
//
// These tests drive the two kernel methods the verification.verdict.approved / .rework consumer
// calls (backend/internal/counts/app/shifting_verification_handler.go), so this file is the
// production-path E2E proof registered for those events in the domain-event registry.

// submitShiftingForVerification drives operator completion with mandatory video evidence.
func submitShiftingForVerification(
	repo *Repository, ctx context.Context, key, shiftingEventID, destinationTag string,
) (domain.ShiftingExecutionResult, bool, error) {
	return repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           countsTenant,
		ShiftingEventID:    shiftingEventID,
		CompletedByUserID:  countsOperator,
		CompletedAt:        time.Now().In(biztime.DefaultLocation()),
		ProofRef:           "proof-artifact-" + key,
		DestinationTag:     destinationTag,
		IdempotencyKey:     "complete-" + key,
		RequestFingerprint: "complete-fp-" + key + ":" + destinationTag,
	})
}

// applyVerifiedShifting drives the verifier-approval consumer path: evidence state only for new rows.
func applyVerifiedShifting(
	repo *Repository, ctx context.Context, shiftingEventID string,
) (domain.ShiftingExecutionResult, bool, error) {
	return repo.ApplyVerifiedShiftingEvent(ctx, domain.ShiftingVerifiedApplyCommand{
		TenantID:         countsTenant,
		ShiftingEventID:  shiftingEventID,
		VerifiedByUserID: countsApprover,
		VerifiedAt:       time.Now().In(biztime.DefaultLocation()),
		TraceID:          "verify-trace-" + shiftingEventID,
	})
}

// TestShiftingCompletionRequiresVideo proves a completion with no video is refused BEFORE any state
// change: the movement stays authorized and nothing relocates.
func TestShiftingCompletionRequiresVideo(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d001"
	goatIDs := []string{goatA}
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "verify-novideo", goatIDs)
	if _, _, err := approveShifting(repo, ctx, "verify-novideo", approvalRequestID, shiftingEventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// No proof_ref at all.
	_, _, err := repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           countsTenant,
		ShiftingEventID:    shiftingEventID,
		CompletedByUserID:  countsOperator,
		CompletedAt:        time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "complete-novideo",
		RequestFingerprint: "complete-fp-novideo",
	})
	if !errors.Is(err, ports.ErrShiftingProofRequired) {
		t.Fatalf("err=%v, want ErrShiftingProofRequired", err)
	}
	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusAuthorized {
		t.Fatalf("event_status=%q after proofless completion, want it still authorized", got)
	}
	if got := goatShed(t, ctx, pool, goatA); got != countsShedA {
		t.Fatalf("goat shed=%s after proofless completion, want it still at source %s", got, countsShedA)
	}
}

// TestHighPriorityShiftingRequiresEmbeddedFeedEvidence is the exact production-path regression for
// the 2026-07-29 high-priority rule. A high-priority movement includes feed packing and feeding the
// animal inside the shifting task, so the legacy single shifting video must no longer be enough to
// record operator completion or relocate the animal.
func TestHighPriorityShiftingRequiresEmbeddedFeedEvidence(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d006"
	goatIDs := []string{goatA}
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "verify-high-one-video", goatIDs)
	if _, err := pool.Exec(ctx, `
UPDATE shifting_events SET priority = 'high' WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`,
		countsTenant, shiftingEventID); err != nil {
		t.Fatalf("mark shifting high priority: %v", err)
	}
	if _, _, err := approveShifting(repo, ctx, "verify-high-one-video", approvalRequestID, shiftingEventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	_, _, err := submitShiftingForVerification(repo, ctx, "verify-high-one-video", shiftingEventID, "")
	if err == nil {
		t.Fatal("high-priority completion with only the shifting video succeeded; want mandatory feed-packing and feeding-video rejection")
	}
	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusAuthorized {
		t.Fatalf("event_status=%q after incomplete high-priority proof, want authorized", got)
	}
	if got := goatShed(t, ctx, pool, goatA); got != countsShedA {
		t.Fatalf("goat shed=%s after incomplete high-priority proof, want source %s", got, countsShedA)
	}

	// Supplying all three videos still cannot bypass missing destination Feed Config. The server
	// must fail closed before comparing a client fingerprint or applying the movement.
	_, _, err = repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:              countsTenant,
		ShiftingEventID:       shiftingEventID,
		CompletedByUserID:     countsOperator,
		CompletedAt:           time.Now().In(biztime.DefaultLocation()),
		ProofRef:              "proof-shifting-missing-config",
		FeedPackingProofRef:   "proof-packing-missing-config",
		FeedGivenProofRef:     "proof-feeding-missing-config",
		FeedConfigFingerprint: "must-not-be-guessed",
		IdempotencyKey:        "complete-high-missing-config",
		RequestFingerprint:    "complete-high-missing-config-fp",
	})
	if !errors.Is(err, ports.ErrShiftingFeedConfigBlocked) {
		t.Fatalf("high-priority completion without destination feed config error=%v, want ErrShiftingFeedConfigBlocked", err)
	}
	if got := goatShed(t, ctx, pool, goatA); got != countsShedA {
		t.Fatalf("goat shed=%s after missing feed config, want source %s", got, countsShedA)
	}
}

func seedHighPriorityShiftingFeedConfig(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, eventID string) {
	t.Helper()
	statements := []struct {
		sql  string
		args []any
	}{
		{`UPDATE goats SET breed='Beetal', management_stage='Adult' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, []any{countsTenant, goatID}},
		{`UPDATE shifting_events SET priority='high', management_stage_mode='select_stage', target_management_stage='Adult' WHERE tenant_id=$1::uuid AND shifting_event_id=$2::uuid`, []any{countsTenant, eventID}},
		{`INSERT INTO feed_shed_tags (tenant_id, shed_tag_label, applies_to, status) VALUES ($1::uuid, 'Adult', 'adult', 'active')`, []any{countsTenant}},
		{`INSERT INTO feed_ration_groups (tenant_id, breed_label, ration_group_label) VALUES ($1::uuid, 'Beetal', 'Beetal/Sirohi')`, []any{countsTenant}},
		{`INSERT INTO feed_session_templates (tenant_id, park_id, session_no, session_label, split_fraction, status) VALUES ($1::uuid, $2::uuid, 1, 'Morning', 1.0, 'active')`, []any{countsTenant, countsPark}},
		{`INSERT INTO feed_session_template_items (tenant_id, park_id, session_no, slot_no, feed_item_label, status, valid_from) VALUES ($1::uuid, $2::uuid, 1, 1, 'Concentrate', 'active', CURRENT_DATE)`, []any{countsTenant, countsPark}},
		{`INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label, grams_per_head, valid_from) VALUES ($1::uuid, $2::uuid, 'Beetal/Sirohi', 'Adult', 'Concentrate', 250, CURRENT_DATE)`, []any{countsTenant, countsPark}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed high-priority shifting feed config: %v", err)
		}
	}
}

func TestHighPriorityShiftingStoresThreeVideosAndConfiguredFeedSnapshot(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)
	goatID := "00000000-0000-4000-8000-00000000d007"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")
	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "verify-high-complete", []string{goatID})
	seedHighPriorityShiftingFeedConfig(t, ctx, pool, goatID, eventID)
	if _, _, err := approveShifting(repo, ctx, "verify-high-complete", approvalID, eventID, []string{goatID}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}
	requirements, err := loadShiftingFeedRequirements(ctx, pool, countsTenant, []string{eventID}, time.Now())
	if err != nil {
		t.Fatalf("resolve feed requirement: %v", err)
	}
	requirement := requirements[eventID]
	if requirement.Status != "ready" || len(requirement.Items) != 1 || requirement.Items[0].QuantityGrams != "250.0000" {
		t.Fatalf("feed requirement=%+v, want one ready 250g concentrate instruction", requirement)
	}
	page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant, Status: "all", PageSize: 20,
	})
	if err != nil || len(page.Items) != 1 || page.Items[0].FeedRequirement == nil ||
		page.Items[0].FeedRequirement.Fingerprint != requirement.Fingerprint {
		t.Fatalf("Actions feed requirement page=%+v err=%v, want the same backend-owned fingerprint", page, err)
	}
	capture := &capturingShiftingVerificationEnqueuer{}
	service := countsapp.NewShiftingExecutionService(repo, time.Now).WithVerificationEnqueuer(capture)
	result, replay, err := service.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		ProofRef:            "proof-shifting",
		FeedPackingProofRef: "proof-packing", FeedGivenProofRef: "proof-feeding",
		FeedConfigFingerprint: requirement.Fingerprint, IdempotencyKey: "complete-high-three",
		RequestFingerprint: "complete-high-three-fp",
	})
	if err != nil || replay || result.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("complete high priority result=%+v replay=%v err=%v", result, replay, err)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
		t.Fatalf("goat shed=%s, want destination %s", got, countsShedB)
	}
	if want := []string{"proof-shifting", "proof-packing", "proof-feeding"}; !reflect.DeepEqual(capture.request.MediaRefs, want) {
		t.Fatalf("verification media_refs=%v, want all three together %v", capture.request.MediaRefs, want)
	}
	var shiftingProof, packingProof, feedingProof, fingerprint string
	var snapshot []byte
	if err := pool.QueryRow(ctx, `
SELECT proof_ref, feed_packing_proof_ref, feed_given_proof_ref, feed_config_fingerprint,
       feed_requirement_snapshot
FROM shifting_events WHERE tenant_id=$1::uuid AND shifting_event_id=$2::uuid`, countsTenant, eventID).
		Scan(&shiftingProof, &packingProof, &feedingProof, &fingerprint, &snapshot); err != nil {
		t.Fatalf("read stored high-priority evidence: %v", err)
	}
	if shiftingProof != "proof-shifting" || packingProof != "proof-packing" ||
		feedingProof != "proof-feeding" || fingerprint != requirement.Fingerprint || len(snapshot) == 0 {
		t.Fatalf("stored evidence shifting=%q packing=%q feeding=%q fingerprint=%q snapshot=%s",
			shiftingProof, packingProof, feedingProof, fingerprint, snapshot)
	}
	if err := repo.BounceShiftingEventForRework(ctx, domain.ShiftingReworkCommand{
		TenantID: countsTenant, ShiftingEventID: eventID, VerifiedBy: countsApprover, Reason: "feed not visible",
	}); err != nil {
		t.Fatalf("reject high-priority evidence: %v", err)
	}
	result, replay, err = service.Complete(ctx, countsapp.CompleteShiftingInput{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		ProofRef: "proof-shifting-rework", FeedPackingProofRef: "proof-packing-rework",
		FeedGivenProofRef: "proof-feeding-rework", FeedConfigFingerprint: requirement.Fingerprint,
		IdempotencyKey: "complete-high-three-rework", RequestFingerprint: "complete-high-three-rework-fp",
	})
	if err != nil || replay || result.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("rework completion result=%+v replay=%v err=%v", result, replay, err)
	}
	if want := []string{"proof-shifting-rework", "proof-packing-rework", "proof-feeding-rework"}; !reflect.DeepEqual(capture.request.MediaRefs, want) {
		t.Fatalf("rework verification media_refs=%v, want replacement set %v", capture.request.MediaRefs, want)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
		t.Fatalf("rework rolled back goat shed=%s, want destination %s", got, countsShedB)
	}
}

func TestHighPriorityShiftingRejectsChangedFeedConfig(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)
	goatID := "00000000-0000-4000-8000-00000000d008"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	eventID, _ := submitShiftingApproval(t, ctx, repo, "verify-high-stale", []string{goatID})
	seedHighPriorityShiftingFeedConfig(t, ctx, pool, goatID, eventID)
	requirements, err := loadShiftingFeedRequirements(ctx, pool, countsTenant, []string{eventID}, time.Now())
	if err != nil {
		t.Fatalf("resolve feed requirement: %v", err)
	}
	staleFingerprint := requirements[eventID].Fingerprint
	if _, err := pool.Exec(ctx, `UPDATE feed_ration_rates SET grams_per_head=300, updated_at=now()+interval '1 second'
WHERE tenant_id=$1::uuid AND park_id=$2::uuid`, countsTenant, countsPark); err != nil {
		t.Fatalf("change feed config: %v", err)
	}
	_, _, err = repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID: countsTenant, ShiftingEventID: eventID, CompletedByUserID: countsOperator,
		CompletedAt: time.Now(), ProofRef: "proof-shifting", FeedPackingProofRef: "proof-packing",
		FeedGivenProofRef: "proof-feeding", FeedConfigFingerprint: staleFingerprint,
		IdempotencyKey: "complete-high-stale", RequestFingerprint: "complete-high-stale-fp",
	})
	if !errors.Is(err, ports.ErrShiftingFeedConfigChanged) {
		t.Fatalf("err=%v, want ErrShiftingFeedConfigChanged", err)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
		t.Fatalf("goat moved after stale feed config: shed=%s want source=%s", got, countsShedA)
	}
}

// TestShiftingCompletionBeforeApprovalDoesNotMove proves that operator completion can arrive first:
// the movement remains pending with completion stamps, and nothing relocates until Park Head approval.
func TestShiftingCompletionBeforeApprovalDoesNotMove(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d011"
	goatIDs := []string{goatA}
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	shiftingEventID, _ := submitShiftingApproval(t, ctx, repo, "verify-submit", goatIDs)

	result, replayed, err := submitShiftingForVerification(repo, ctx, "verify-submit", shiftingEventID, "")
	if err != nil {
		t.Fatalf("submit for verification: %v", err)
	}
	if replayed {
		t.Fatalf("first submit reported replayed=true, want a fresh submission")
	}
	if result.EventStatus != domain.ShiftingEventStatusPending {
		t.Fatalf("event_status=%q, want %q", result.EventStatus, domain.ShiftingEventStatusPending)
	}

	// The pending row carries the completion video and has no applied stamp.
	var (
		eventStatus string
		verifState  string
		proofRef    *string
		appliedAt   *time.Time
	)
	if err := pool.QueryRow(ctx, `
SELECT event_status, verification_state, proof_ref, applied_at
FROM shifting_events WHERE shifting_event_id = $1::uuid`, shiftingEventID).
		Scan(&eventStatus, &verifState, &proofRef, &appliedAt); err != nil {
		t.Fatalf("read shifting event: %v", err)
	}
	if eventStatus != domain.ShiftingEventStatusPending {
		t.Fatalf("event_status=%q, want pending", eventStatus)
	}
	if verifState != "unverified" {
		t.Fatalf("verification_state=%q, want unverified", verifState)
	}
	if proofRef == nil || *proofRef == "" {
		t.Fatalf("proof_ref=%v, want the operator's video id stored", proofRef)
	}
	if appliedAt != nil {
		t.Fatalf("applied_at=%v before approval, want NULL", appliedAt)
	}

	// NOTHING relocated: the animal is still at the source shed, and no location.changed event/outbox
	// row exists.
	if got := goatShed(t, ctx, pool, goatA); got != countsShedA {
		t.Fatalf("goat shed=%s after completion, want it STILL at source %s until Park Head approval",
			got, countsShedA)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 0 {
		t.Fatalf("location.changed identity events after completion=%d, want 0 (nothing moved yet)", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 0 {
		t.Fatalf("location.changed outbox messages after completion=%d, want 0 (nothing moved yet)", got)
	}
}

// TestShiftingVerifierApprovalOnlyMarksEvidence proves verification changes evidence state only;
// approval + operator completion already moved the animal and census.
func TestShiftingVerifierApprovalOnlyMarksEvidence(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d021"
	goatIDs := []string{goatA}
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "verify-approve", goatIDs)
	if _, _, err := approveShifting(repo, ctx, "verify-approve", approvalRequestID, shiftingEventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}
	if _, _, err := submitShiftingForVerification(repo, ctx, "verify-approve", shiftingEventID, ""); err != nil {
		t.Fatalf("submit for verification: %v", err)
	}

	// Premise: both business gates already applied the move before the verifier acts.
	if got := goatShed(t, ctx, pool, goatA); got != countsShedB {
		t.Fatalf("goat shed=%s before verifier approval, want destination %s", got, countsShedB)
	}

	result, evidenceChanged, err := applyVerifiedShifting(repo, ctx, shiftingEventID)
	if err != nil {
		t.Fatalf("apply verified shifting: %v", err)
	}
	if !evidenceChanged {
		t.Fatalf("verification reported changed=false, want a fresh evidence verdict")
	}
	if result.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("event_status=%q, want applied", result.EventStatus)
	}

	// The animal remains at the destination and the row becomes applied+verified. Verification does
	// not create another location event.
	if got := goatShed(t, ctx, pool, goatA); got != countsShedB {
		t.Fatalf("goat shed=%s after verifier approval, want unchanged destination %s",
			got, countsShedB)
	}
	var (
		eventStatus string
		verifState  string
		appliedBy   *string
	)
	if err := pool.QueryRow(ctx, `
SELECT event_status, verification_state, applied_by::text
FROM shifting_events WHERE shifting_event_id = $1::uuid`, shiftingEventID).
		Scan(&eventStatus, &verifState, &appliedBy); err != nil {
		t.Fatalf("read shifting event: %v", err)
	}
	if eventStatus != domain.ShiftingEventStatusApplied || verifState != "verified" {
		t.Fatalf("event_status=%q verification_state=%q, want applied/verified", eventStatus, verifState)
	}
	if appliedBy == nil || *appliedBy != countsOperator {
		t.Fatalf("applied_by=%v, want the completing operator %s", appliedBy, countsOperator)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 1 {
		t.Fatalf("location.changed identity events after approval=%d, want 1", got)
	}

	// Idempotency: a re-delivered verdict (the durable bus may redeliver) relocates nobody again.
	if _, evidenceChanged, err := applyVerifiedShifting(repo, ctx, shiftingEventID); err != nil {
		t.Fatalf("re-delivered verdict: %v", err)
	} else if evidenceChanged {
		t.Fatalf("re-delivered verdict reported changed=true, want a no-op replay")
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 1 {
		t.Fatalf("location.changed identity events after re-delivered verdict=%d, want still 1", got)
	}
}

// TestShiftingVerifierRejectionCreatesEvidenceReworkWithoutRollback proves rejected evidence is
// rework only: applied movement and census truth remain unchanged.
func TestShiftingVerifierRejectionCreatesEvidenceReworkWithoutRollback(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d031"
	goatIDs := []string{goatA}
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "verify-reject", goatIDs)
	if _, _, err := approveShifting(repo, ctx, "verify-reject", approvalRequestID, shiftingEventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}
	if _, _, err := submitShiftingForVerification(repo, ctx, "verify-reject", shiftingEventID, ""); err != nil {
		t.Fatalf("submit for verification: %v", err)
	}

	if err := repo.BounceShiftingEventForRework(ctx, domain.ShiftingReworkCommand{
		TenantID:        countsTenant,
		ShiftingEventID: shiftingEventID,
		VerifiedBy:      countsApprover,
		Reason:          "video does not show the animals entering the destination shed",
	}); err != nil {
		t.Fatalf("bounce for rework: %v", err)
	}

	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusApplied {
		t.Fatalf("event_status=%q after rejection, want it to remain applied", got)
	}
	if got := goatShed(t, ctx, pool, goatA); got != countsShedB {
		t.Fatalf("goat shed=%s after rejection, want destination %s (no rollback)", got, countsShedB)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 1 {
		t.Fatalf("location.changed events after rejection=%d, want still 1", got)
	}

	// The operator can re-submit a fresh video while the already-applied movement stays applied.
	if _, _, err := submitShiftingForVerification(repo, ctx, "verify-reject-2", shiftingEventID, ""); err != nil {
		t.Fatalf("re-submit after rework: %v", err)
	}
	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusApplied {
		t.Fatalf("event_status=%q after evidence re-submit, want applied", got)
	}
}
