package postgres

// Regressions found by the herd-operations E2E run (2026-09-17) against a live API. Opt-in like
// every DB test.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
)

// TestReplayedBirthStillResolvesItsMotherPg: the phone's outbox retries a birth with the SAME
// Idempotency-Key when a response is lost. The birth handler re-prepares every child through
// identity and canonicalises dam_id from the prepared mother to rebuild the byte-identical
// fingerprint. Identity's completed-idempotency short-circuit returned an EMPTY validation, so the
// replay came back 400 mother_not_found for a birth that was recorded -- and the outbox marks a 400
// as a terminal failure. The replay must still resolve the mother (read-only).
func TestReplayedBirthStillResolvesItsMotherPg(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := identitypg.NewRepository(pool, 10*time.Second)
	repo := newApprovalRepo(t, pool, identity)
	motherID := "00000000-0000-4000-8000-00000000b3a1"
	seedApprovalGoat(t, ctx, pool, motherID, countsShedA)
	dob := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	stage, litter := "kid", 1
	child := identityports.CreateAdminGoatCommand{
		TenantID: countsTenant, ActorID: countsOperator, ClientIdempotencyKey: "replay-child-1",
		StoredIdempotencyKey: countsTenant + ":identity.admin.goat_create:replay-child-1",
		IdempotencyScope:     "identity.admin.goat_create", RequestHash: "hash-replay-child-1",
		Identifiers:      []identityports.AdminGoatCreateIdentifier{{IdentifierType: "temporary_tag", IdentifierValue: "CPT-32177", NormalizedValue: "CPT-32177", ScopeKey: "global", IsPrimary: true}},
		CustodianPartyID: countsCustodian, ParkID: countsPark, ShedID: countsShedA,
		Species: "goat", Breed: strPtr("beetal"), Sex: "female", DOB: &dob, OriginType: "birth", EntryDate: dob,
		ManagementStage: &stage, DamID: &motherID, LitterSize: &litter,
	}
	submission := birthSubmission("birth-replay-pg")
	submission.Payload = json.RawMessage(fmt.Sprintf(`{"species":"goat","park_id":%q,"shed_id":%q}`, countsPark, countsShedA))
	if _, err := repo.CreateBirthApprovalRequest(ctx, submission, []identityports.CreateAdminGoatCommand{child}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	park, shed := countsPark, countsShedA
	replay, err := identity.ValidateAdminGoatCreate(ctx, identityports.ValidateAdminGoatCreateCommand{
		TenantID:             countsTenant,
		StoredIdempotencyKey: child.StoredIdempotencyKey,
		RequestHash:          child.RequestHash,
		ParkID:               &park,
		ShedID:               &shed,
		BirthDamRef:          &motherID,
		Species:              "goat",
	})
	if err != nil {
		t.Fatalf("replayed child validation: %v", err)
	}
	if replay.DamGoatID == nil || *replay.DamGoatID != motherID {
		t.Fatalf("replayed birth resolved mother=%v, want %s (the handler answers 400 mother_not_found)", replay.DamGoatID, motherID)
	}
}

// TestSecondDeathReportForAPendingGoatIsAcceptedAsOnMainPg: a second death report for an animal
// whose first report is still pending is ACCEPTED, exactly as origin/main does (2026-09-17 parity
// revert of a submit-time 409 death_already_reported that no SOP edit asked for). The duplicate is
// handled at DECIDE time instead: once the first report is approved the animal has left the herd,
// and approving the second is refused cleanly with ErrDeathAlreadyApplied (409, never a 500),
// leaving it pending to be rejected.
func TestSecondDeathReportForAPendingGoatIsAcceptedAsOnMainPg(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := identitypg.NewRepository(pool, 10*time.Second)
	repo := newApprovalRepo(t, pool, identity)
	goatID := "00000000-0000-4000-8000-00000000c0d1"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	submit := func(key string) (domain.ApprovalRequest, bool, error) {
		return repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
			TenantID: countsTenant, RequestType: domain.ApprovalRequestTypeDeath,
			Payload:       json.RawMessage(`{"goat_id":"` + goatID + `","lifecycle_status":"dead","exit_reason":"died"}`),
			SubjectGoatID: strPtr(goatID), RaisedByUserID: countsOperator, RaisedAt: time.Now(),
			IdempotencyKey: key, RequestFingerprint: key + "-fp",
		})
	}
	first, _, err := submit("death-dup-1")
	if err != nil {
		t.Fatalf("first report: %v", err)
	}
	// The exact replay is still the original.
	if again, replay, err := submit("death-dup-1"); err != nil || !replay || again.ApprovalRequestID != first.ApprovalRequestID {
		t.Fatalf("replay = %v %v %v", again.ApprovalRequestID, replay, err)
	}
	second, replay, err := submit("death-dup-2")
	if err != nil || replay || second.ApprovalRequestID == first.ApprovalRequestID {
		t.Fatalf("second report = %v replay=%v err=%v; want a new pending request (origin/main accepts it)", second.ApprovalRequestID, replay, err)
	}
	approve := func(id, key string) error {
		_, _, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
			TenantID: countsTenant, ApprovalRequestID: id, Status: domain.ApprovalStatusApproved,
			DecidedByUserID: countsApprover, DecidedAt: time.Now(),
			IdempotencyKey: key, RequestFingerprint: key + "-fp",
			Effect: &domain.ApprovalEffect{ExitGoat: identityports.ExitGoatCommand{
				TenantID: countsTenant, ActorID: countsApprover, ClientIdempotencyKey: "exit-" + key,
				StoredIdempotencyKey: countsTenant + ":identity.admin.goat_exit:exit-" + key,
				IdempotencyScope:     "identity.admin.goat_exit", RequestHash: "exit-hash-" + key,
				GoatID: goatID, LifecycleStatus: "dead", ExitReason: "died", Reason: "found dead",
				OccurredAt: time.Now(), RowVersion: 1, GuardrailApproved: true,
			}},
		})
		return err
	}
	if err := approve(first.ApprovalRequestID, "decide-dup-1"); err != nil {
		t.Fatalf("approve first report: %v", err)
	}
	if err := approve(second.ApprovalRequestID, "decide-dup-2"); !errors.Is(err, ports.ErrDeathAlreadyApplied) {
		t.Fatalf("approve second report err = %v, want ErrDeathAlreadyApplied (409 death_already_applied)", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM counts_approval_requests WHERE approval_request_id = $1::uuid`, second.ApprovalRequestID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != domain.ApprovalStatusPending {
		t.Fatalf("refused duplicate status = %s, want pending (to be rejected)", status)
	}
}

// TestApprovingADuplicateDeathAfterTheAnimalDiedIsRefusedPg: a database written before the
// one-pending-report fix (STG) can still hold TWO pending death reports for one animal. Approving
// the first kills the goat; approving the second then failed inside identity's exit on the stale
// row version and surfaced as a 500. It must be refused cleanly (409 death_already_applied), leave
// the duplicate pending so it can be rejected, and change nothing about the applied death.
func TestApprovingADuplicateDeathAfterTheAnimalDiedIsRefusedPg(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := identitypg.NewRepository(pool, 10*time.Second)
	repo := newApprovalRepo(t, pool, identity)
	goatID := "00000000-0000-4000-8000-00000000c0d9"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	// Pre-existing bad data: two pending reports inserted directly, as the pre-fix code allowed.
	ids := make([]string, 2)
	for i := range ids {
		if err := pool.QueryRow(ctx, `
INSERT INTO counts_approval_requests (tenant_id, request_type, payload, subject_goat_id, status, raised_by_user_id, raised_at, idempotency_key, request_fingerprint)
VALUES ($1::uuid, 'death', $2::jsonb, $3::uuid, 'pending', $4::uuid, now(), $5, $5)
RETURNING approval_request_id::text`,
			countsTenant, `{"goat_id":"`+goatID+`","lifecycle_status":"dead","exit_reason":"died"}`, goatID, countsOperator,
			fmt.Sprintf("dup-death-%d", i)).Scan(&ids[i]); err != nil {
			t.Fatalf("seed duplicate death report %d: %v", i, err)
		}
	}
	approve := func(i int) (domain.ApprovalRequest, bool, error) {
		return repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
			TenantID: countsTenant, ApprovalRequestID: ids[i], Status: domain.ApprovalStatusApproved,
			DecidedByUserID: countsApprover, DecidedAt: time.Now(),
			IdempotencyKey: fmt.Sprintf("dup-decide-%d", i), RequestFingerprint: fmt.Sprintf("dup-decide-%d-fp", i),
			Effect: &domain.ApprovalEffect{ExitGoat: identityports.ExitGoatCommand{
				TenantID: countsTenant, ActorID: countsApprover, ClientIdempotencyKey: fmt.Sprintf("exit-%d", i),
				StoredIdempotencyKey: fmt.Sprintf("%s:identity.admin.goat_exit:exit-%d", countsTenant, i),
				IdempotencyScope:     "identity.admin.goat_exit", RequestHash: fmt.Sprintf("exit-hash-%d", i),
				GoatID: goatID, LifecycleStatus: "dead", ExitReason: "died", Reason: "found dead",
				OccurredAt: time.Now(), RowVersion: 1, GuardrailApproved: true,
			}},
		})
	}
	if _, _, err := approve(0); err != nil {
		t.Fatalf("approve first report: %v", err)
	}
	if _, _, err := approve(1); !errors.Is(err, ports.ErrDeathAlreadyApplied) {
		t.Fatalf("approve duplicate err = %v, want ErrDeathAlreadyApplied (the handler answers 409, not 500)", err)
	}
	var status, lifecycle string
	if err := pool.QueryRow(ctx, `SELECT status FROM counts_approval_requests WHERE approval_request_id = $1::uuid`, ids[1]).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT lifecycle_status FROM goats WHERE goat_id = $1::uuid`, goatID).Scan(&lifecycle); err != nil {
		t.Fatal(err)
	}
	if status != domain.ApprovalStatusPending || lifecycle != "dead" {
		t.Fatalf("after the refused duplicate: request=%s goat=%s, want pending / dead", status, lifecycle)
	}
}
