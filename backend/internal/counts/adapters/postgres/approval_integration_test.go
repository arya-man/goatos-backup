package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identitydomain "github.com/vgoats/goatos/backend/internal/identity/domain"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Counts lifecycle approval workflow -- Postgres proofs.

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

	// Pen-tag adoption seam (typed shifting rewrite). failAdopt simulates the destination pen
	// changing between approval and apply, which must roll the whole apply back.
	adoptCalls int
	failAdopt  error
	lastAdopt  identityports.ConfigureAdoptedShedCohortCommand

	newGoatID string
}

type fakeDeathEvidenceTxGate struct {
	ready bool
	calls int
}

func (f *fakeDeathEvidenceTxGate) PrepareDeathEvidenceForApprovalInTx(
	_ context.Context, _ pgx.Tx, _, _ string,
) (bool, error) {
	f.calls++
	return f.ready, nil
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

func (f *fakeIdentityTx) ConfigureAdoptedShedCohortInTx(_ context.Context, _ pgx.Tx, cmd identityports.ConfigureAdoptedShedCohortCommand) error {
	f.adoptCalls++
	f.lastAdopt = cmd
	return f.failAdopt
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newApprovalRepo(t *testing.T, pool *pgxpool.Pool, identity IdentityTxWriter) *Repository {
	t.Helper()
	seedCustodianParty(t, context.Background(), pool)
	return NewRepository(pool, 10*time.Second).
		WithIdentityTxWriter(identity).
		WithDeathEvidenceTxGate(&fakeDeathEvidenceTxGate{ready: true})
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

func seedPendingBirthLitter(t *testing.T, ctx context.Context, pool *pgxpool.Pool, birthEventID string, litterSize, childRows int) []string {
	t.Helper()
	var motherID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&motherID); err != nil {
		t.Fatalf("new mother id: %v", err)
	}
	seedApprovalGoat(t, ctx, pool, motherID, countsShedA)
	children := make([]string, 0, childRows)
	for ordinal := 1; ordinal <= childRows; ordinal++ {
		var childID string
		if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&childID); err != nil {
			t.Fatalf("new child id: %v", err)
		}
		seedApprovalGoat(t, ctx, pool, childID, countsShedA)
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_births (
  tenant_id, child_goat_id, mother_goat_id, birth_event_id, child_ordinal,
  litter_size, count_status
) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, 'pending')`,
			countsTenant, childID, motherID, birthEventID, ordinal, litterSize); err != nil {
			t.Fatalf("seed pending birth child %d: %v", ordinal, err)
		}
		children = append(children, childID)
	}
	return children
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

// A twin submit traverses the real Counts repository -> Identity transaction seam. Both canonical
// children and goat.created outbox rows exist immediately, but neither child enters the herd
// projection before the independent web approval. Exact replay returns the same two children.
func TestSubmitTwinBirthCreatesCanonicalChildrenButExcludesCountsUntilApproval(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := identitypg.NewRepository(pool, 10*time.Second)
	repo := newApprovalRepo(t, pool, identity)

	motherID := "00000000-0000-4000-8000-00000000b001"
	seedApprovalGoat(t, ctx, pool, motherID, countsShedA)
	before := countGoats(t, ctx, pool)
	litterSize := 2
	dob := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	stage := "kid"
	children := make([]identityports.CreateAdminGoatCommand, 0, litterSize)
	for ordinal, tag := range []string{"CPT-12345", "CPT-67890"} {
		clientKey := fmt.Sprintf("twin-child-%d", ordinal+1)
		children = append(children, identityports.CreateAdminGoatCommand{
			TenantID: countsTenant, ActorID: countsOperator,
			ClientIdempotencyKey: clientKey,
			StoredIdempotencyKey: countsTenant + ":identity.admin.goat_create:" + clientKey,
			IdempotencyScope:     "identity.admin.goat_create", RequestHash: "hash-" + clientKey,
			Identifiers: []identityports.AdminGoatCreateIdentifier{{
				IdentifierType: "temporary_tag", IdentifierValue: tag,
				NormalizedValue: tag, ScopeKey: "global", IsPrimary: true,
			}},
			CustodianPartyID: countsCustodian, ParkID: countsPark, ShedID: countsShedA,
			Species: "goat", Breed: strPtr("beetal"), Sex: "female", DOB: &dob,
			OriginType: "birth", EntryDate: dob, ManagementStage: &stage,
			DamID: &motherID, LitterSize: &litterSize,
		})
	}
	submission := birthSubmission("birth-key-twins-immediate")
	result, err := repo.CreateBirthApprovalRequest(ctx, submission, children)
	if err != nil {
		t.Fatalf("submit twin birth: %v", err)
	}
	if result.Replayed || result.Approval.Status != domain.ApprovalStatusPending {
		t.Fatalf("result=%+v, want first pending submission", result)
	}
	if len(result.Children) != 2 || result.Children[0].GoatID == result.Children[1].GoatID {
		t.Fatalf("children=%+v, want two distinct canonical goats", result.Children)
	}
	if got := countGoats(t, ctx, pool); got != before+2 {
		t.Fatalf("goats=%d, want %d immediately after twin submit", got, before+2)
	}
	var pendingBirths, projected, outbox int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM goat_births WHERE tenant_id=$1::uuid AND birth_event_id=$2::uuid AND count_status='pending'`, countsTenant, result.Approval.ApprovalRequestID).Scan(&pendingBirths); err != nil {
		t.Fatalf("count pending birth rows: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM herd_register_goat_projection WHERE tenant_id=$1::uuid AND goat_id IN ($2::uuid, $3::uuid)`, countsTenant, result.Children[0].GoatID, result.Children[1].GoatID).Scan(&projected); err != nil {
		t.Fatalf("count projected children: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'goat.created'`,
		countsTenant).Scan(&outbox); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if pendingBirths != 2 || projected != 0 || outbox != 2 {
		t.Fatalf("pending=%d projected=%d goat.created=%d, want 2/0/2", pendingBirths, projected, outbox)
	}
	replayed, err := repo.CreateBirthApprovalRequest(ctx, submission, children)
	if err != nil || !replayed.Replayed || len(replayed.Children) != 2 ||
		replayed.Children[0].GoatID != result.Children[0].GoatID || replayed.Children[1].GoatID != result.Children[1].GoatID {
		t.Fatalf("replay=%+v err=%v, want the original two children", replayed, err)
	}
	if got := countGoats(t, ctx, pool); got != before+2 {
		t.Fatalf("goats after replay=%d, want %d", got, before+2)
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
	children := seedPendingBirthLitter(t, ctx, pool, req.ApprovalRequestID, 1, 1)
	before := countGoats(t, ctx, pool)
	var projectedBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM herd_register_goat_projection WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, countsTenant, children[0]).Scan(&projectedBefore); err != nil {
		t.Fatalf("count pending projected child: %v", err)
	}
	if projectedBefore != 0 {
		t.Fatalf("pending child projected=%d, want 0 before web approval", projectedBefore)
	}

	decided, replay, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusApproved,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "decide-key-0001",
		RequestFingerprint: "decide-fp-0001",
		Effect:             &domain.ApprovalEffect{BirthCounts: &domain.BirthCountsApprovalEffect{BirthEventID: req.ApprovalRequestID}},
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
	if decided.AppliedResultType == nil || *decided.AppliedResultType != domain.ApprovalResultTypeBirthEvent {
		t.Fatalf("applied_result_type=%v, want birth_event", decided.AppliedResultType)
	}
	if decided.AppliedResultID == nil || *decided.AppliedResultID == "" {
		t.Fatal("an approved request must name the litter it activated")
	}
	if got := countGoats(t, ctx, pool); got != before {
		t.Fatalf("goats=%d, want %d — approval must not create another animal", got, before)
	}
	var projected int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM herd_register_goat_projection WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, countsTenant, children[0]).Scan(&projected); err != nil {
		t.Fatalf("count projected child: %v", err)
	}
	if projected != 1 || identity.createCalls != 0 {
		t.Fatalf("projected=%d identity creates=%d, want count activation without goat creation", projected, identity.createCalls)
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
	seedPendingBirthLitter(t, ctx, pool, req.ApprovalRequestID, 1, 1)
	decision := domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusApproved,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "decide-key-double",
		RequestFingerprint: "decide-fp-double",
		Effect:             &domain.ApprovalEffect{BirthCounts: &domain.BirthCountsApprovalEffect{BirthEventID: req.ApprovalRequestID}},
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
	if identity.createCalls != 0 {
		t.Fatalf("identity create calls=%d, want 0 across count-only approvals", identity.createCalls)
	}
}

// TestDecideSameKeyChangedPayloadConflictsAfterDecision is the CR-06 regression. Reusing a decision
// idempotency key with a CHANGED payload must be a conflict even AFTER the request is decided. The
// same-key/fingerprint check previously ran only after the terminal-status early return, so an
// already-decided request retried with the same key but a different reason fell into the "same
// decision" replay path and returned the original row as a silent success -- accepting a payload it
// never applied. The check must fire whether the request is pending or decided.
func TestDecideSameKeyChangedPayloadConflictsAfterDecision(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		reason string
	}{
		{name: "after approve", status: domain.ApprovalStatusApproved, reason: ""},
		{name: "after reject", status: domain.ApprovalStatusRejected, reason: "duplicate report"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			pool := setupCountsDB(t, ctx)
			identity := &fakeIdentityTx{}
			repo := newApprovalRepo(t, pool, identity)

			req, _, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-cr06-"+tc.name))
			if err != nil {
				t.Fatalf("submit: %v", err)
			}
			seedPendingBirthLitter(t, ctx, pool, req.ApprovalRequestID, 1, 1)
			decision := domain.ApprovalDecision{
				TenantID:           countsTenant,
				ApprovalRequestID:  req.ApprovalRequestID,
				Status:             tc.status,
				DecidedByUserID:    countsApprover,
				DecidedAt:          time.Now().In(biztime.DefaultLocation()),
				Reason:             tc.reason,
				IdempotencyKey:     "decide-cr06-" + tc.name,
				RequestFingerprint: "decide-fp-cr06-" + tc.name,
			}
			if tc.status == domain.ApprovalStatusApproved {
				decision.Effect = &domain.ApprovalEffect{BirthCounts: &domain.BirthCountsApprovalEffect{BirthEventID: req.ApprovalRequestID}}
			}
			if _, _, err := repo.DecideApprovalRequest(ctx, decision); err != nil {
				t.Fatalf("first decision: %v", err)
			}

			// Exact replay (same key, same fingerprint) still succeeds as an idempotent no-op.
			if _, replay, err := repo.DecideApprovalRequest(ctx, decision); err != nil || !replay {
				t.Fatalf("exact replay: replay=%v err=%v, want a clean replay", replay, err)
			}

			// Same key, CHANGED payload (a different fingerprint / altered reason) must CONFLICT, not
			// silently return the original decision as a replay.
			changed := decision
			changed.RequestFingerprint = "decide-fp-cr06-" + tc.name + "-altered"
			changed.Reason = "a different account of the same decision"
			_, replay, err := repo.DecideApprovalRequest(ctx, changed)
			if !errors.Is(err, ports.ErrIdempotencyConflict) {
				t.Fatalf("same-key/changed-payload after decision: err=%v replay=%v, want ErrIdempotencyConflict", err, replay)
			}
		})
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
	seedPendingBirthLitter(t, ctx, pool, req.ApprovalRequestID, 1, 1)
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

// Litter membership is fail-closed: approving a delivery that declares twins but has only one
// canonical child must roll back the approval transition and leave the existing child untouched.
func TestApproveIncompleteLitterRollsBackStatus(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := &fakeIdentityTx{}
	repo := newApprovalRepo(t, pool, identity)

	req, _, err := repo.CreateApprovalRequest(ctx, birthSubmission("birth-key-rollback"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedPendingBirthLitter(t, ctx, pool, req.ApprovalRequestID, 2, 1)
	before := countGoats(t, ctx, pool)

	if _, _, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusApproved,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "decide-key-rollback",
		RequestFingerprint: "decide-fp-rollback",
		Effect:             &domain.ApprovalEffect{BirthCounts: &domain.BirthCountsApprovalEffect{BirthEventID: req.ApprovalRequestID}},
	}); err == nil {
		t.Fatal("a failing effect must fail the approval")
	}

	// The request must still be pending: a human has to be able to retry it.
	if got := approvalStatus(t, ctx, pool, req.ApprovalRequestID); got != domain.ApprovalStatusPending {
		t.Fatalf("status=%q after a failed effect, want pending — an approved request whose effect "+
			"failed is exactly what the atomic transition rule forbids", got)
	}
	// No identity mutation is attempted at approval; the already-created child remains unchanged.
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

// Rejecting a shifting must flip the shifting_events row off 'pending', so the raiser's read-only
// Pending tab stops listing a movement the approver refused. Before this was wired, the reject
// flipped only counts_approval_requests and the event sat in the Pending tab forever (reported
// 2026-08-29: "the approver rejected, but it still shows in pending").
func TestRejectShiftingFlipsTheEventOffThePendingQueue(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})

	goatID := "00000000-0000-4000-8000-00000000b002"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)

	eventID, _, err := repo.RecordShiftingEvent(ctx, shiftingEventForApproval("shift-reject-1"))
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
		IdempotencyKey:     "shift-reject-key-1",
		RequestFingerprint: "shift-reject-fp-1",
	})
	if err != nil {
		t.Fatalf("submit shifting approval: %v", err)
	}

	// FAILING-BEFORE CHECK: the raised movement is on the Pending tab before the decision.
	page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
		TenantID: countsTenant, Status: "pending",
	})
	if err != nil {
		t.Fatalf("list pending before reject: %v", err)
	}
	found := false
	for _, row := range page.Items {
		if row.ShiftingEventID == eventID {
			found = true
		}
	}
	if !found {
		t.Fatalf("raised movement %s missing from the Pending tab before the decision", eventID)
	}

	if _, _, err := repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID:           countsTenant,
		ApprovalRequestID:  req.ApprovalRequestID,
		Status:             domain.ApprovalStatusRejected,
		DecidedByUserID:    countsApprover,
		DecidedAt:          time.Now().In(biztime.DefaultLocation()),
		Reason:             "wrong destination pen, raise it again for Castro 2",
		IdempotencyKey:     "decide-key-shift-reject",
		RequestFingerprint: "decide-fp-shift-reject",
	}); err != nil {
		t.Fatalf("reject shifting: %v", err)
	}

	// The event row itself records the refusal — not just the approval-request row.
	var authState, eventStatus string
	if err := pool.QueryRow(ctx, `
SELECT authorization_state, event_status FROM shifting_events WHERE shifting_event_id = $1::uuid`,
		eventID).Scan(&authState, &eventStatus); err != nil {
		t.Fatalf("read shifting event: %v", err)
	}
	if authState != "rejected" {
		t.Fatalf("authorization_state=%q after reject, want rejected", authState)
	}
	if eventStatus != domain.ShiftingEventStatusRejected {
		t.Fatalf("event_status=%q after reject, want %q", eventStatus, domain.ShiftingEventStatusRejected)
	}

	// And the movement left EVERY Actions tab: the Pending tab (the reported defect) and the
	// operator work list alike.
	for _, status := range []string{"pending", "all"} {
		page, err := repo.ListShiftingEventsPendingExecution(ctx, domain.ShiftingExecutionQuery{
			TenantID: countsTenant, Status: status,
		})
		if err != nil {
			t.Fatalf("list %s after reject: %v", status, err)
		}
		for _, row := range page.Items {
			if row.ShiftingEventID == eventID {
				t.Fatalf("rejected movement %s still listed on the %q tab", eventID, status)
			}
		}
	}

	// A rejected movement can never be completed: the approve-first gate still holds.
	if _, _, err := repo.CompleteShiftingEvent(ctx, domain.ShiftingCompletionCommand{
		TenantID:           countsTenant,
		ShiftingEventID:    eventID,
		CompletedByUserID:  countsOperator,
		CompletedAt:        time.Now().In(biztime.DefaultLocation()),
		ProofRef:           "proof-artifact-shift-reject-1",
		IdempotencyKey:     "complete-after-reject-1",
		RequestFingerprint: "complete-fp-after-reject-1",
	}); !errors.Is(err, ports.ErrShiftingNotAuthorized) {
		t.Fatalf("complete after reject err=%v, want ErrShiftingNotAuthorized", err)
	}
}

// ---------------------------------------------------------------------------
// Death guardrail through the approval path
// ---------------------------------------------------------------------------

func TestDeathSubmissionEmitsOneDurableWorkflowCommand(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})
	goatID := "00000000-0000-4000-8000-00000000c018"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	submission := domain.ApprovalRequestSubmission{
		TenantID: countsTenant, RequestType: domain.ApprovalRequestTypeDeath,
		Payload:       json.RawMessage(`{"goat_id":"` + goatID + `","lifecycle_status":"dead","exit_reason":"died"}`),
		SubjectGoatID: &goatID, RaisedByUserID: countsOperator, RaisedAt: time.Now(),
		IdempotencyKey: "death-opens-workflow", RequestFingerprint: "death-opens-workflow-fp",
	}
	req, replay, err := repo.CreateApprovalRequest(ctx, submission)
	if err != nil || replay {
		t.Fatalf("first submit: replay=%v err=%v", replay, err)
	}
	if _, replay, err := repo.CreateApprovalRequest(ctx, submission); err != nil || !replay {
		t.Fatalf("exact replay: replay=%v err=%v", replay, err)
	}
	var (
		count       int
		payloadGoat string
		envelope    []byte
	)
	if err := pool.QueryRow(ctx, `
SELECT count(*), max(payload #>> '{payload,goat_id}'), max(payload::text)::bytea
FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type=$2 AND aggregate_id=$3::uuid`,
		countsTenant, domain.EventDeathReported, req.ApprovalRequestID).Scan(&count, &payloadGoat, &envelope); err != nil {
		t.Fatalf("read workflow command: %v", err)
	}
	if count != 1 || payloadGoat != goatID {
		t.Fatalf("death workflow commands=%d goat=%q, want one for %s", count, payloadGoat, goatID)
	}
	validator, err := outboxapp.NewEnvelopeValidator(filepath.Join("..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatalf("load domain-event contract: %v", err)
	}
	if err := validator.Validate(envelope); err != nil {
		t.Fatalf("death workflow command violates domain-event contract: %v", err)
	}
}

func TestApproveDeathWaitsForBothOperatorVideos(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	identity := &fakeIdentityTx{}
	gate := &fakeDeathEvidenceTxGate{ready: false}
	repo := NewRepository(pool, 10*time.Second).
		WithIdentityTxWriter(identity).
		WithDeathEvidenceTxGate(gate)
	seedCustodianParty(t, ctx, pool)

	goatID := "00000000-0000-4000-8000-00000000c019"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	req, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID: countsTenant, RequestType: domain.ApprovalRequestTypeDeath,
		Payload:       json.RawMessage(`{"goat_id":"` + goatID + `","lifecycle_status":"dead","exit_reason":"died"}`),
		SubjectGoatID: &goatID, RaisedByUserID: countsOperator, RaisedAt: time.Now(),
		IdempotencyKey: "death-await-videos", RequestFingerprint: "death-await-videos-fp",
	})
	if err != nil {
		t.Fatalf("submit death: %v", err)
	}
	_, _, err = repo.DecideApprovalRequest(ctx, domain.ApprovalDecision{
		TenantID: countsTenant, ApprovalRequestID: req.ApprovalRequestID,
		Status: domain.ApprovalStatusApproved, DecidedByUserID: countsApprover, DecidedAt: time.Now(),
		IdempotencyKey: "approve-before-videos", RequestFingerprint: "approve-before-videos-fp",
		Effect: &domain.ApprovalEffect{ExitGoat: identityports.ExitGoatCommand{
			TenantID: countsTenant, GoatID: goatID, LifecycleStatus: "dead", ExitReason: "died", GuardrailApproved: true,
		}},
	})
	if !errors.Is(err, ports.ErrDeathEvidenceIncomplete) {
		t.Fatalf("approve before videos err = %v, want ErrDeathEvidenceIncomplete", err)
	}
	if got := approvalStatus(t, ctx, pool, req.ApprovalRequestID); got != domain.ApprovalStatusPending {
		t.Fatalf("approval status = %q, want pending", got)
	}
	if identity.exitCalls != 0 {
		t.Fatalf("identity exit calls = %d, want 0 before both videos", identity.exitCalls)
	}
}

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

// A scoped manager can receive a retried response after the first approval already changed the
// goat to dead. Scope still comes from the canonical goat park; lifecycle state must not make that
// idempotent replay look forbidden.
func TestApprovalSubjectParkRemainsReadableAfterApprovedDeath(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedCustodianParty(t, ctx, pool)

	goatID := "00000000-0000-4000-8000-00000000c020"
	seedApprovalGoat(t, ctx, pool, goatID, countsShedA)
	if _, err := pool.Exec(ctx, `
UPDATE goats
SET lifecycle_status = 'dead', exit_reason = 'died', exited_at = now()
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, countsTenant, goatID); err != nil {
		t.Fatalf("mark goat dead: %v", err)
	}

	repo := NewRepository(pool, 10*time.Second)
	parkID, err := repo.ApprovalSubjectPark(ctx, countsTenant, goatID)
	if err != nil {
		t.Fatalf("read approval subject park after death: %v", err)
	}
	if parkID != countsPark {
		t.Fatalf("approval subject park = %q, want %q", parkID, countsPark)
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
		Priority:                "low",
		Category:                "growth",
		SourceParkID:            strPtr(countsPark),
		SourceShedID:            strPtr(countsShedA),
		DestinationParkID:       countsPark,
		DestinationShedID:       countsShedB,
		// Raised two days ago so the shared fixture is past the ACTIONS LEAD TIME (maintainer
		// decision 2026-08-09): a LOW-priority movement -- which this fixture is -- does not enter
		// the operator's work list until the day after its raise day, or the one after that when it
		// was raised past 13:30 IST. Leaving this at "now" would make every queue test's visibility
		// depend on the hour the suite happened to run. The lead time itself is proven directly by
		// TestListPendingExecutionAppliesTheActionsLeadTime and by the counts/domain unit tests.
		RaisedAt:    time.Now().In(biztime.DefaultLocation()).AddDate(0, 0, -2),
		EffectiveAt: time.Now().In(biztime.DefaultLocation()).AddDate(0, 0, -2),
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
