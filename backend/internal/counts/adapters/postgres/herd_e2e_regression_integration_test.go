package postgres

// Regressions found by the herd-operations E2E run (2026-09-17) against a live API. Opt-in like
// every DB test.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
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

// TestSecondDeathReportForAPendingGoatIsRefusedPg: two operators reporting the same death opened
// TWO pending approvals for one animal. Approving one killed the goat and left the other stuck
// (its approve 500'd on the identity row version), and REJECTING the leftover as a duplicate
// canceled the applied death's workflow under a pending verifier item (E2E 2026-09-17). While a
// death report for an animal is pending, a second one is refused -- sequentially and under a race.
func TestSecondDeathReportForAPendingGoatIsRefusedPg(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})
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
	if _, _, err := submit("death-dup-2"); !errors.Is(err, ports.ErrDeathAlreadyReported) {
		t.Fatalf("second report err = %v, want ErrDeathAlreadyReported", err)
	}
	// A race of fresh keys on another goat lands exactly one pending report.
	raceGoat := "00000000-0000-4000-8000-00000000c0d2"
	seedApprovalGoat(t, ctx, pool, raceGoat, countsShedA)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, _ = repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
				TenantID: countsTenant, RequestType: domain.ApprovalRequestTypeDeath,
				Payload:       json.RawMessage(`{"goat_id":"` + raceGoat + `","lifecycle_status":"dead","exit_reason":"died"}`),
				SubjectGoatID: strPtr(raceGoat), RaisedByUserID: countsOperator, RaisedAt: time.Now(),
				IdempotencyKey: fmt.Sprintf("death-race-%d", i), RequestFingerprint: fmt.Sprintf("death-race-%d-fp", i),
			})
		}(i)
	}
	wg.Wait()
	var pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM counts_approval_requests WHERE subject_goat_id=$1::uuid AND request_type='death' AND status='pending'`, raceGoat).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("pending death reports after a race = %d, want 1", pending)
	}
}
