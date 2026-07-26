package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Shifting VERIFICATION gate -- proofs of the maintainer-2026-07-26 rule against the REAL identity
// Postgres adapter (the same wiring bootstrap uses, so the schema's triggers/constraints are part of
// the assertion surface).
//
// The rule: an operator's completion no longer relocates. It records a MANDATORY video and flips the
// movement to 'pending_verification'; the animals move (and the count moves) ONLY when a verifier
// approves the video, via ApplyVerifiedShiftingEvent. A rejected video bounces the movement back to
// 'authorized' with nothing moved.
//
// These tests drive the two kernel methods the verification.verdict.approved / .rework consumer
// calls (backend/internal/counts/app/shifting_verification_handler.go), so this file is the
// production-path E2E proof registered for those events in the domain-event registry.

// submitShiftingForVerification drives the operator completion path with the mandatory video, which
// under the 2026-07-26 rule submits the movement for verification rather than relocating.
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

// applyVerifiedShifting drives the verifier-approval consumer path: relocate + flip to applied.
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

// TestShiftingCompletionSubmitsForVerificationWithoutMoving proves the core of the rule: the operator
// completes with a video, the movement becomes pending_verification, and NOTHING relocates yet -- no
// location change, no location.changed event, and the census still reads the source shed.
func TestShiftingCompletionSubmitsForVerificationWithoutMoving(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatA := "00000000-0000-4000-8000-00000000d011"
	goatIDs := []string{goatA}
	seedApprovalGoat(t, ctx, pool, goatA, countsShedA)
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	shiftingEventID, approvalRequestID := submitShiftingApproval(t, ctx, repo, "verify-submit", goatIDs)
	if _, _, err := approveShifting(repo, ctx, "verify-submit", approvalRequestID, shiftingEventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	result, replayed, err := submitShiftingForVerification(repo, ctx, "verify-submit", shiftingEventID, "")
	if err != nil {
		t.Fatalf("submit for verification: %v", err)
	}
	if replayed {
		t.Fatalf("first submit reported replayed=true, want a fresh submission")
	}
	if result.EventStatus != domain.ShiftingEventStatusPendingVerification {
		t.Fatalf("event_status=%q, want %q", result.EventStatus, domain.ShiftingEventStatusPendingVerification)
	}

	// The row is pending_verification, carries the video, and has NO applied stamp.
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
	if eventStatus != domain.ShiftingEventStatusPendingVerification {
		t.Fatalf("event_status=%q, want pending_verification", eventStatus)
	}
	if verifState != "unverified" {
		t.Fatalf("verification_state=%q, want unverified", verifState)
	}
	if proofRef == nil || *proofRef == "" {
		t.Fatalf("proof_ref=%v, want the operator's video id stored", proofRef)
	}
	if appliedAt != nil {
		t.Fatalf("applied_at=%v before verification, want NULL", appliedAt)
	}

	// NOTHING relocated: the animal is still at the source shed, and no location.changed event/outbox
	// row exists.
	if got := goatShed(t, ctx, pool, goatA); got != countsShedA {
		t.Fatalf("goat shed=%s after completion, want it STILL at source %s until a verifier approves",
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

// TestShiftingVerifierApprovalRelocatesAndMovesCount proves the second half: once a verifier approves,
// ApplyVerifiedShiftingEvent relocates the animals, flips to applied with the verifier stamp, emits
// the location.changed event, and the census now reads the DESTINATION shed. This is the moment the
// count moves.
func TestShiftingVerifierApprovalRelocatesAndMovesCount(t *testing.T) {
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

	// Premise: still at the source shed before the verifier acts.
	if got := goatShed(t, ctx, pool, goatA); got != countsShedA {
		t.Fatalf("goat shed=%s before verifier approval, want source %s", got, countsShedA)
	}

	result, applied, err := applyVerifiedShifting(repo, ctx, shiftingEventID)
	if err != nil {
		t.Fatalf("apply verified shifting: %v", err)
	}
	if !applied {
		t.Fatalf("apply reported applied=false, want a fresh relocation")
	}
	if result.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("event_status=%q, want applied", result.EventStatus)
	}

	// The animal is now at the destination, the row is applied+verified with the verifier stamp, and
	// the location.changed event/outbox exist.
	if got := goatShed(t, ctx, pool, goatA); got != countsShedB {
		t.Fatalf("goat shed=%s after verifier approval, want destination %s -- approval MOVES the animal",
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
	if appliedBy == nil || *appliedBy != countsApprover {
		t.Fatalf("applied_by=%v, want the verifier %s", appliedBy, countsApprover)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 1 {
		t.Fatalf("location.changed identity events after approval=%d, want 1", got)
	}

	// Idempotency: a re-delivered verdict (the durable bus may redeliver) relocates nobody again.
	if _, applied, err := applyVerifiedShifting(repo, ctx, shiftingEventID); err != nil {
		t.Fatalf("re-delivered verdict: %v", err)
	} else if applied {
		t.Fatalf("re-delivered verdict reported applied=true, want a no-op replay")
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 1 {
		t.Fatalf("location.changed identity events after re-delivered verdict=%d, want still 1", got)
	}
}

// TestShiftingVerifierRejectionBouncesToAuthorized proves a rejected video returns the movement to
// 'authorized' (so the operator re-records) and relocates nobody.
func TestShiftingVerifierRejectionBouncesToAuthorized(t *testing.T) {
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

	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusAuthorized {
		t.Fatalf("event_status=%q after rejection, want it bounced back to authorized", got)
	}
	if got := goatShed(t, ctx, pool, goatA); got != countsShedA {
		t.Fatalf("goat shed=%s after rejection, want it still at source %s (nothing moved)", got, countsShedA)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 0 {
		t.Fatalf("location.changed events after rejection=%d, want 0", got)
	}

	// After the bounce the operator can re-submit with a fresh video -- the movement is authorized
	// again, so completion is accepted.
	if _, _, err := submitShiftingForVerification(repo, ctx, "verify-reject-2", shiftingEventID, ""); err != nil {
		t.Fatalf("re-submit after rework: %v", err)
	}
	if got := shiftingEventStatus(t, ctx, pool, shiftingEventID); got != domain.ShiftingEventStatusPendingVerification {
		t.Fatalf("event_status=%q after re-submit, want pending_verification", got)
	}
}
