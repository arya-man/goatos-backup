package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identitydomain "github.com/vgoats/goatos/backend/internal/identity/domain"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Counts lifecycle approval workflow -- Postgres proofs.
//
// These exercise the properties that only a real database can demonstrate: that submitting applies
// NOTHING, that approving applies the effect and the status flip in ONE transaction, that a failing
// effect rolls the approval back rather than leaving a request reading 'approved' with nothing
// behind it, and that an approved shifting genuinely relocates the animal.

const (
	countsApprover  = "00000000-0000-4000-8000-000000009001"
	countsOperator  = "00000000-0000-4000-8000-000000009002"
	countsCustodian = "00000000-0000-4000-8000-000000009003"
)

// ---------------------------------------------------------------------------
// Fake identity seam
// ---------------------------------------------------------------------------

// fakeIdentityTx stands in for identity's Postgres repository. It writes through the SAME
// transaction the approval uses, which is what lets these tests prove atomicity: when the fake is
// told to fail, the goat row it already wrote must disappear along with the approval's status flip.
type fakeIdentityTx struct {
	createCalls int
	exitCalls   int
	relocCalls  int

	// failCreate/failExit/failRelocate simulate the effect failing AFTER it has written rows, which
	// is the case that distinguishes a genuine shared transaction from two sequential ones.
	failCreate   error
	failExit     error
	failRelocate error

	// relocateMoved caps how many of the requested animals the relocate reports as moved, so the
	// fail-closed shortfall path can be exercised.
	relocateMoved *int

	lastCreate   identityports.CreateAdminGoatCommand
	lastExit     identityports.ExitGoatCommand
	lastRelocate identityports.RelocateGoatsCommand

	newGoatID string
}

func (f *fakeIdentityTx) CreateAdminGoatInTx(ctx context.Context, tx pgx.Tx, cmd identityports.CreateAdminGoatCommand) (*identityports.AdminGoatMutationResult, error) {
	f.createCalls++
	f.lastCreate = cmd
	goatID := f.newGoatID
	if goatID == "" {
		goatID = "00000000-0000-4000-8000-00000000a001"
	}
	// Write a real row in the caller's transaction so a rollback is observable.
	if _, err := tx.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, species, sex, lifecycle_status, custodian_party_id, park_id, shed_id, current_location_id, origin_type, dob, entry_date)
VALUES ($1::uuid, $2::uuid, 'goat', 'female', 'alive', $5::uuid, $3::uuid, $4::uuid, $4::uuid, 'birth', DATE '2026-07-01', DATE '2026-07-01')`,
		goatID, cmd.TenantID, countsPark, countsShedA, countsCustodian); err != nil {
		return nil, err
	}
	if f.failCreate != nil {
		return nil, f.failCreate
	}
	return &identityports.AdminGoatMutationResult{
		Goat: identitydomain.GoatSummary{GoatID: goatID},
	}, nil
}

func (f *fakeIdentityTx) ExitGoatInTx(ctx context.Context, tx pgx.Tx, cmd identityports.ExitGoatCommand) (*identityports.AdminGoatMutationResult, error) {
	f.exitCalls++
	f.lastExit = cmd
	// The guardrail is identity's, and it is re-checked on this exact path. Mirroring it here keeps
	// the fake from being a weaker gate than the real adapter.
	if cmd.LifecycleStatus != "dead" || cmd.ExitReason != "died" {
		return nil, fmt.Errorf("critical death guardrail: got lifecycle=%q exit_reason=%q", cmd.LifecycleStatus, cmd.ExitReason)
	}
	if !cmd.GuardrailApproved {
		return nil, errors.New("critical death guardrail not approved")
	}
	if _, err := tx.Exec(ctx, `
UPDATE goats SET lifecycle_status = 'dead', exit_reason = 'died', exited_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, cmd.TenantID, cmd.GoatID); err != nil {
		return nil, err
	}
	if f.failExit != nil {
		return nil, f.failExit
	}
	return &identityports.AdminGoatMutationResult{Goat: identitydomain.GoatSummary{GoatID: cmd.GoatID}}, nil
}

func (f *fakeIdentityTx) RelocateGoatsToShedInTx(ctx context.Context, tx pgx.Tx, cmd identityports.RelocateGoatsCommand) (identityports.RelocateGoatsResult, error) {
	f.relocCalls++
	f.lastRelocate = cmd
	if f.failRelocate != nil {
		return identityports.RelocateGoatsResult{}, f.failRelocate
	}
	moved := cmd.GoatIDs
	if f.relocateMoved != nil && *f.relocateMoved < len(moved) {
		moved = moved[:*f.relocateMoved]
	}
	for _, goatID := range moved {
		if _, err := tx.Exec(ctx, `
UPDATE goats SET shed_id = $3::uuid, current_location_id = $3::uuid, park_id = $4::uuid, row_version = row_version + 1
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, cmd.TenantID, goatID, cmd.ToShedID, cmd.ToParkID); err != nil {
			return identityports.RelocateGoatsResult{}, err
		}
	}
	return identityports.RelocateGoatsResult{MovedGoatIDs: moved}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newApprovalRepo(t *testing.T, pool *pgxpool.Pool, identity IdentityTxWriter) *Repository {
	t.Helper()
	seedCustodianParty(t, context.Background(), pool)
	return NewRepository(pool, 10*time.Second).WithIdentityTxWriter(identity)
}

// goats.custodian_party_id is a real FK. Seeding the party keeps these tests exercising the actual
// schema constraints rather than a relaxed copy of it.
func seedCustodianParty(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'org', 'Counts Approval Test Custodian', 'active')
ON CONFLICT (party_id) DO NOTHING`, countsCustodian); err != nil {
		t.Fatalf("seed custodian party: %v", err)
	}
}

func seedApprovalGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, shedID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, species, sex, lifecycle_status, custodian_party_id, park_id, shed_id, current_location_id, origin_type, dob, entry_date)
VALUES ($1::uuid, $2::uuid, 'goat', 'female', 'alive', $5::uuid, $3::uuid, $4::uuid, $4::uuid, 'procured', DATE '2025-01-01', DATE '2025-01-01')
ON CONFLICT (goat_id) DO NOTHING`, goatID, countsTenant, countsPark, shedID, countsCustodian); err != nil {
		t.Fatalf("seed goat: %v", err)
	}
}

func birthSubmission(key string) domain.ApprovalRequestSubmission {
	return domain.ApprovalRequestSubmission{
		TenantID:           countsTenant,
		RequestType:        domain.ApprovalRequestTypeBirth,
		Payload:            json.RawMessage(`{"species":"goat","sex":"female","dob":"2026-07-01","origin_type":"birth"}`),
		RaisedByUserID:     countsOperator,
		RaisedAt:           time.Date(2026, 7, 19, 6, 0, 0, 0, time.UTC),
		IdempotencyKey:     key,
		RequestFingerprint: "fingerprint-" + key,
	}
}

func countGoats(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM goats WHERE tenant_id = $1::uuid`, countsTenant).Scan(&n); err != nil {
		t.Fatalf("count goats: %v", err)
	}
	return n
}

func goatShed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) string {
	t.Helper()
	var shed string
	if err := pool.QueryRow(ctx, `SELECT shed_id::text FROM goats WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		countsTenant, goatID).Scan(&shed); err != nil {
		t.Fatalf("read goat shed: %v", err)
	}
	return shed
}

func approvalStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM counts_approval_requests WHERE approval_request_id = $1::uuid`, id).Scan(&status); err != nil {
		t.Fatalf("read approval status: %v", err)
	}
	return status
}

// ---------------------------------------------------------------------------
// Pending, not applied
// ---------------------------------------------------------------------------

// Submitting a birth must create the request and NOTHING else. This is the whole maintainer
// decision in one assertion: before it, the same submit created the animal immediately, which meant
// an unapproved birth was already in the census and had already generated the kid's vaccination
// obligations.
func TestSubmitBirthRequestCreatesNoGoat(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := &fakeIdentityTx{}
	repo := newApprovalRepo(t, pool, identity)

	before := countGoats(t, ctx, pool)
	req, replay, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-key-0001"))
	if err != nil {
		t.Fatalf("create approval request: %v", err)
	}
	if replay {
		t.Fatal("first submission must not report a replay")
	}
	if req.Status != domain.ApprovalStatusPending {
		t.Fatalf("status=%q, want pending", req.Status)
	}
	if got := countGoats(t, ctx, pool); got != before {
		t.Fatalf("goats=%d, want %d — a PENDING birth must not create an animal", got, before)
	}
	if identity.createCalls != 0 {
		t.Fatalf("identity create calls=%d, want 0 — submission must apply nothing", identity.createCalls)
	}
	// No goat.created means the vaccination generator never sees the kid, which is the downstream
	// consequence the decision is really about.
	var outbox int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'goat.created'`,
		countsTenant).Scan(&outbox); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outbox != 0 {
		t.Fatalf("goat.created outbox rows=%d, want 0", outbox)
	}
}

// An exact resubmission must collapse onto the original request rather than queueing a second
// approval for the same real-world birth — otherwise a flaky mobile connection produces two kids.
func TestSubmitApprovalRequestExactReplayIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})

	first, _, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-key-replay"))
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	second, replay, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-key-replay"))
	if err != nil {
		t.Fatalf("replay submit: %v", err)
	}
	if !replay {
		t.Fatal("replay must be reported as a replay")
	}
	if second.ApprovalRequestID != first.ApprovalRequestID {
		t.Fatalf("replay returned %s, want the original %s", second.ApprovalRequestID, first.ApprovalRequestID)
	}
}

// Reusing a key with a different payload must be refused outright: silently returning the original
// would tell the operator their correction was saved when it was not.
func TestSubmitApprovalRequestSameKeyDifferentPayloadConflicts(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})

	if _, _, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-key-conflict")); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	changed := birthSubmission("birth-key-conflict")
	changed.RequestFingerprint = "a-different-payload"
	if _, _, err := repo.CreateApprovalRequest(ctx, changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("err=%v, want ErrIdempotencyConflict", err)
	}
}

// ---------------------------------------------------------------------------
// Approve applies
// ---------------------------------------------------------------------------

func TestApproveBirthAppliesExactlyOnce(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := &fakeIdentityTx{}
	repo := newApprovalRepo(t, pool, identity)

	req, _, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-key-approve"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	before := countGoats(t, ctx, pool)

	decided, replay, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusApproved,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "decide-key-0001",
		RequestFingerprint: "decide-fp-0001",
		Effect:             &domain.ApprovalEffect{CreateGoat: identityports.CreateAdminGoatCommand{TenantID: countsTenant}},
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if replay {
		t.Fatal("first approve must not report a replay")
	}
	if decided.Status != domain.ApprovalStatusApproved {
		t.Fatalf("status=%q, want approved", decided.Status)
	}
	// The applied result is stamped in the same transaction; the schema's
	// counts_approval_requests_applied_result_check makes an approved row without it impossible.
	if decided.AppliedResultType == nil || *decided.AppliedResultType != domain.ApprovalResultTypeGoat {
		t.Fatalf("applied_result_type=%v, want goat", decided.AppliedResultType)
	}
	if decided.AppliedResultID == nil || *decided.AppliedResultID == "" {
		t.Fatal("an approved request must name the goat it created")
	}
	if got := countGoats(t, ctx, pool); got != before+1 {
		t.Fatalf("goats=%d, want %d — approval must create exactly one animal", got, before+1)
	}
	if identity.createCalls != 1 {
		t.Fatalf("identity create calls=%d, want exactly 1", identity.createCalls)
	}
}

// A second approve must not create a second animal. This is the property that matters most in the
// field: an approver double-taps, or the response is lost and the phone retries, and the herd must
// not gain a phantom kid.
func TestDoubleApproveDoesNotDoubleApply(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := &fakeIdentityTx{}
	repo := newApprovalRepo(t, pool, identity)

	req, _, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-key-double"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	decision := domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusApproved,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "decide-key-double",
		RequestFingerprint: "decide-fp-double",
		Effect:             &domain.ApprovalEffect{CreateGoat: identityports.CreateAdminGoatCommand{TenantID: countsTenant}},
	}
	if _, _, err := repo.DecideApprovalRequest(ctx, decision); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	after := countGoats(t, ctx, pool)

	// Same idempotency key: the decision-level replay path.
	if _, replay, err := repo.DecideApprovalRequest(ctx, decision); err != nil || !replay {
		t.Fatalf("same-key replay: replay=%v err=%v, want replay with no error", replay, err)
	}
	// DIFFERENT idempotency key on an already-approved request: the already-decided path. This is
	// the one a naive implementation gets wrong, because the key no longer matches.
	fresh := decision
	fresh.IdempotencyKey = "decide-key-double-2"
	fresh.RequestFingerprint = "decide-fp-double-2"
	if _, replay, err := repo.DecideApprovalRequest(ctx, fresh); err != nil || !replay {
		t.Fatalf("second approve with a new key: replay=%v err=%v, want replay with no error", replay, err)
	}
	if got := countGoats(t, ctx, pool); got != after {
		t.Fatalf("goats=%d after double approve, want %d — approving twice must not create a second animal", got, after)
	}
	if identity.createCalls != 1 {
		t.Fatalf("identity create calls=%d, want exactly 1 across both approves", identity.createCalls)
	}
}

// ---------------------------------------------------------------------------
// Reject applies nothing
// ---------------------------------------------------------------------------

func TestRejectAppliesNothing(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := &fakeIdentityTx{}
	repo := newApprovalRepo(t, pool, identity)

	req, _, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-key-reject"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	before := countGoats(t, ctx, pool)

	decided, _, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusRejected,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		Reason:             "duplicate report, kid already recorded this morning",
		IdempotencyKey:     "decide-key-reject",
		RequestFingerprint: "decide-fp-reject",
	})
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if decided.Status != domain.ApprovalStatusRejected {
		t.Fatalf("status=%q, want rejected", decided.Status)
	}
	if decided.AppliedResultType != nil || decided.AppliedResultID != nil {
		t.Fatalf("a rejection must record no applied result, got type=%v id=%v",
			decided.AppliedResultType, decided.AppliedResultID)
	}
	if got := countGoats(t, ctx, pool); got != before {
		t.Fatalf("goats=%d, want %d — a rejection must not create an animal", got, before)
	}
	if identity.createCalls != 0 {
		t.Fatalf("identity create calls=%d, want 0 on a rejection", identity.createCalls)
	}
}

// A rejection without a reason is unactionable for the operator who raised it, so the DATABASE
// refuses it too — not just the API layer, which a future caller could bypass.
func TestRejectWithoutReasonIsRefusedByTheDatabase(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})

	req, _, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-key-noreason"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, _, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusRejected,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "decide-key-noreason",
		RequestFingerprint: "decide-fp-noreason",
	}); err == nil {
		t.Fatal("rejecting with no reason must fail on counts_approval_requests_reject_reason_check")
	}
	if got := approvalStatus(t, ctx, pool, req.ApprovalRequestID); got != domain.ApprovalStatusPending {
		t.Fatalf("status=%q after a refused rejection, want it to stay pending", got)
	}
}

// ---------------------------------------------------------------------------
// Atomicity
// ---------------------------------------------------------------------------

// THE atomicity proof. The effect writes a real goats row and then fails. If the status flip and
// the effect were separate transactions, the goat would survive and/or the request would read
// 'approved' with nothing behind it. Both must be gone.
func TestApproveRollsBackStatusFlipWhenEffectFails(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := &fakeIdentityTx{failCreate: errors.New("identity write failed after inserting the row")}
	repo := newApprovalRepo(t, pool, identity)

	req, _, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-key-rollback"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	before := countGoats(t, ctx, pool)

	if _, _, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusApproved,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "decide-key-rollback",
		RequestFingerprint: "decide-fp-rollback",
		Effect:             &domain.ApprovalEffect{CreateGoat: identityports.CreateAdminGoatCommand{TenantID: countsTenant}},
	}); err == nil {
		t.Fatal("a failing effect must fail the approval")
	}

	// The request must still be pending: a human has to be able to retry it.
	if got := approvalStatus(t, ctx, pool, req.ApprovalRequestID); got != domain.ApprovalStatusPending {
		t.Fatalf("status=%q after a failed effect, want pending — an approved request whose effect "+
			"failed is exactly what the atomic transition rule forbids", got)
	}
	// And the row the effect wrote before failing must be gone with it.
	if got := countGoats(t, ctx, pool); got != before {
		t.Fatalf("goats=%d, want %d — the failed effect's partial write must roll back with the approval", got, before)
	}
}

// ---------------------------------------------------------------------------
// Shifting: approval AUTHORIZES and moves NOTHING
// ---------------------------------------------------------------------------

// Maintainer decision (2026-07-19): approving a shifting is AUTHORIZATION, not evidence that
// anybody walked the animals anywhere. The relocation moved to the completion path
// (CompleteShiftingEvent); this test pins that approve no longer touches the animals at all.
func TestApproveShiftingAuthorizesWithoutMovingTheAnimals(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := &fakeIdentityTx{}
	repo := newApprovalRepo(t, pool, identity)

	goatID := "00000000-0000-4000-8000-00000000b001"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)

	eventID, _, err := repo.RecordShiftingEvent(ctx, shiftingEventForApproval("shift-approve-1"))
	if err != nil {
		t.Fatalf("record shifting event: %v", err)
	}
	payload, _ := json.Marshal(map[string]any{
		"shifting_event_id":   eventID,
		"destination_park_id": countsPark,
		"destination_shed_id": countsShedB,
		"goat_ids":            []string{goatID},
	})
	req, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID:           countsTenant,
		RequestType:        domain.ApprovalRequestTypeShifting,
		Payload:            payload,
		ShiftingEventID:    &eventID,
		RaisedByUserID:     countsOperator,
		RaisedAt:           time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "shift-approval-key-1",
		RequestFingerprint: "shift-approval-fp-1",
	})
	if err != nil {
		t.Fatalf("submit shifting approval: %v", err)
	}

	// Before approval the animal must still be in the SOURCE shed: a reported movement is not a
	// movement.
	if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
		t.Fatalf("shed=%s before approval, want the source shed %s", got, countsShedA)
	}

	if _, _, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusApproved,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "decide-key-shift",
		RequestFingerprint: "decide-fp-shift",
		Effect: &domain.ApprovalEffect{Shifting: &domain.ShiftingApprovalEffect{
			ShiftingEventID:   eventID,
			DestinationParkID: countsPark,
			DestinationShedID: countsShedB,
			GoatIDs:           []string{goatID},
		}},
	}); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// THE LOAD-BEARING ASSERTION. The animal is still in the SOURCE shed: approval authorized the
	// movement, it did not perform it. Relocating here would have asserted that the animal was
	// standing in the destination shed because a manager pressed a button -- putting the herd
	// register, the census, and every shed-scoped vaccination obligation in a shed the animal was
	// not in for as long as the real movement took, or forever if it never happened.
	if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
		t.Fatalf("shed=%s after approval, want it STILL at the source shed %s -- approving a shifting "+
			"must AUTHORIZE the movement, not perform it; the animals relocate at completion", got, countsShedA)
	}
	if identity.relocCalls != 0 {
		t.Fatalf("relocate calls=%d after approval, want 0 -- approve must not call the identity "+
			"relocation at all", identity.relocCalls)
	}

	// The paperwork DID advance: both the authorization state and the event status now say the
	// movement is permitted, which is what puts it on the operator's pending-execution queue.
	var authState, eventStatus string
	if err := pool.QueryRow(ctx, `
SELECT authorization_state, event_status FROM shifting_events WHERE shifting_event_id = $1::uuid`,
		eventID).Scan(&authState, &eventStatus); err != nil {
		t.Fatalf("read shifting event: %v", err)
	}
	if authState != "authorized" {
		t.Fatalf("authorization_state=%q, want authorized", authState)
	}
	if eventStatus != domain.ShiftingEventStatusAuthorized {
		t.Fatalf("event_status=%q, want %q", eventStatus, domain.ShiftingEventStatusAuthorized)
	}

	// And nothing downstream of a relocation fired either: no location event, no outbox message.
	// An approval that quietly emitted goat.location.changed would re-scope the animal's
	// vaccination obligations to a shed it had not reached.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.location.changed'`,
		countsTenant); got != 0 {
		t.Fatalf("location identity events after approval=%d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Death guardrail through the approval path
// ---------------------------------------------------------------------------

// The dead+died pairing must still be enforced when the exit runs from an APPROVAL rather than from
// the direct route. An approval path that reached a weaker exit would be a way around the
// guardrail.
func TestApproveDeathEnforcesCriticalDeathGuardrail(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := &fakeIdentityTx{}
	repo := newApprovalRepo(t, pool, identity)

	goatID := "00000000-0000-4000-8000-00000000c001"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)

	req, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID:           countsTenant,
		RequestType:        domain.ApprovalRequestTypeDeath,
		Payload:            json.RawMessage(`{"goat_id":"` + goatID + `","lifecycle_status":"dead","exit_reason":"died"}`),
		SubjectGoatID:      &goatID,
		RaisedByUserID:     countsOperator,
		RaisedAt:           time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "death-key-guardrail",
		RequestFingerprint: "death-fp-guardrail",
	})
	if err != nil {
		t.Fatalf("submit death: %v", err)
	}

	// A command that is NOT the dead+died pairing must be refused at apply time.
	if _, _, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusApproved,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "decide-key-badguard",
		RequestFingerprint: "decide-fp-badguard",
		Effect: &domain.ApprovalEffect{ExitGoat: identityports.ExitGoatCommand{
			TenantID: countsTenant, GoatID: goatID,
			LifecycleStatus: "sold", ExitReason: "sold", GuardrailApproved: true,
		}},
	}); err == nil {
		t.Fatal("approving a death whose command is not the dead+died pairing must fail")
	}
	if got := approvalStatus(t, ctx, pool, req.ApprovalRequestID); got != domain.ApprovalStatusPending {
		t.Fatalf("status=%q, want pending after a guardrail rejection", got)
	}

	// The correctly-paired, guardrail-approved command applies.
	if _, _, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusApproved,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "decide-key-goodguard",
		RequestFingerprint: "decide-fp-goodguard",
		Effect: &domain.ApprovalEffect{ExitGoat: identityports.ExitGoatCommand{
			TenantID: countsTenant, GoatID: goatID,
			LifecycleStatus: "dead", ExitReason: "died", GuardrailApproved: true,
		}},
	}); err != nil {
		t.Fatalf("approve death: %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT lifecycle_status FROM goats WHERE goat_id = $1::uuid`, goatID).Scan(&status); err != nil {
		t.Fatalf("read goat: %v", err)
	}
	if status != "dead" {
		t.Fatalf("lifecycle_status=%q, want dead", status)
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

// The pending list must be restricted to the types the caller may decide. This is the read-side
// half of the authority split: a park_head must not even be able to page through births.
func TestListApprovalRequestsRestrictsToDecidableTypes(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})

	if _, _, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-list-1")); err != nil {
		t.Fatalf("submit birth: %v", err)
	}
	eventID, _, err := repo.RecordShiftingEvent(ctx, shiftingEventForApproval("shift-list-1"))
	if err != nil {
		t.Fatalf("record shifting event: %v", err)
	}
	if _, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID:           countsTenant,
		RequestType:        domain.ApprovalRequestTypeShifting,
		Payload:            json.RawMessage(`{"destination_park_id":"` + countsPark + `","destination_shed_id":"` + countsShedB + `"}`),
		ShiftingEventID:    &eventID,
		RaisedByUserID:     countsOperator,
		RaisedAt:           time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "shift-list-key-1",
		RequestFingerprint: "shift-list-fp-1",
	}); err != nil {
		t.Fatalf("submit shifting: %v", err)
	}

	page, err := repo.ListApprovalRequests(ctx, domain.ApprovalRequestQuery{
		TenantID: countsTenant, Status: domain.ApprovalStatusPending,
		RequestTypes: []string{domain.ApprovalRequestTypeShifting}, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].RequestType != domain.ApprovalRequestTypeShifting {
		t.Fatalf("items=%+v, want only the shifting request", page.Items)
	}

	// An approver with no decidable types sees nothing at all, rather than the predicate being
	// dropped and the whole queue leaking.
	empty, err := repo.ListApprovalRequests(ctx, domain.ApprovalRequestQuery{
		TenantID: countsTenant, Status: domain.ApprovalStatusPending, RequestTypes: nil, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("list with no decidable types: %v", err)
	}
	if len(empty.Items) != 0 {
		t.Fatalf("items=%d, want 0 for a caller who may decide nothing", len(empty.Items))
	}
}

func shiftingEventForApproval(key string) domain.ShiftingEvent {
	return domain.ShiftingEvent{
		TenantID:                countsTenant,
		LogicalShiftingEventKey: key,
		Priority:                "normal",
		Category:                "routine",
		SourceParkID:            strPtr(countsPark),
		SourceShedID:            strPtr(countsShedA),
		DestinationParkID:       countsPark,
		DestinationShedID:       countsShedB,
		RaisedAt:                time.Now().In(biztime.DefaultLocation()),
		EffectiveAt:             time.Now().In(biztime.DefaultLocation()),
		// These are the defaults counts/app.Service applies; this helper writes through the
		// repository directly, so it must set them itself. They are also the premise of the test:
		// a reported movement starts PENDING, and approval is what authorizes it.
		AuthorizationState: "pending",
		VerificationState:  "unverified",
		EventStatus:        "pending",
		SourceSystem:       "goatos_canonical",
		SourceRef:          "test:" + key,
		PayloadHash:        "hash-" + key,
		IdempotencyKey:     "idem-" + key,
		RequestFingerprint: "fp-" + key,
		Impacts: []domain.ShiftingEventImpact{{
			GrainKey: countsShedB + ":beetal", BreedKey: "beetal", BreedLabel: "Beetal",
			HeadCount: 1, RiskFlagsJSON: []byte("{}"),
		}},
	}
}
