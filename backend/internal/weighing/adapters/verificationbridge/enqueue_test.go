package verificationbridge

// The bridge must carry the park through to the generic verification item. A verification_item with
// a NULL park_id publishes a verification.item.pending payload with a blank park, which the
// notification consumer cannot route -- the operator's proof then waits with nobody told.

import (
	"context"
	"testing"
	"time"

	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

type captureCreator struct {
	received       verificationdomain.CreateItem
	withdrawnCalls []withdrawCall
	appliedCalls   []appliedCall
}

// appliedCall records the apply-RECEIPT weighing sends verification once a verdict
// has landed on the observation, so a decided item stops reading as "not yet in
// effect" on the verifier's surface.
type appliedCall struct {
	tenantID        string
	module          string
	refType         string
	refIDs          []string
	appliedByModule string
}

func (c *captureCreator) MarkVerdictApplied(_ context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string, appliedByModule string) (int, error) {
	c.appliedCalls = append(c.appliedCalls, appliedCall{tenantID, sourceModule, sourceRefType, append([]string(nil), sourceRefIDs...), appliedByModule})
	return len(sourceRefIDs), nil
}

type withdrawCall struct {
	tenantID string
	module   string
	refType  string
	refIDs   []string
}

func (c *captureCreator) WithdrawItemsBySource(_ context.Context, tenantID, sourceModule, sourceRefType string, sourceRefIDs []string) (int, error) {
	c.withdrawnCalls = append(c.withdrawnCalls, withdrawCall{tenantID, sourceModule, sourceRefType, append([]string(nil), sourceRefIDs...)})
	return len(sourceRefIDs), nil
}

func (c *captureCreator) CreateItem(_ context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error) {
	c.received = in
	return verificationdomain.CreateItemResult{Created: true}, nil
}

func TestEnqueueWeighingVerificationCarriesPark(t *testing.T) {
	creator := &captureCreator{}
	if err := New(creator).EnqueueWeighingVerification(context.Background(), weighingapp.VerificationEnqueueRequest{
		TenantID:       "11111111-1111-4111-8111-111111111111",
		Category:       "shed",
		ObservationID:  "22222222-2222-4222-8222-222222222222",
		OperatorID:     "33333333-3333-4333-8333-333333333333",
		ShedID:         "44444444-4444-4444-8444-444444444444",
		ParkID:         "55555555-5555-4555-8555-555555555555",
		CapturedAt:     time.Now(),
		IdempotencyKey: "weighing:shed:22222222-2222-4222-8222-222222222222",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if creator.received.ParkID == nil {
		t.Fatal("verification item was created with NO park; the pending notification cannot route")
	}
	if got := *creator.received.ParkID; got != "55555555-5555-4555-8555-555555555555" {
		t.Fatalf("park=%q", got)
	}
}

// A reopened lump-sum bucket withdraws its submission. The verification item raised
// for that submission points at the SAME observation id (Source.RefID above), and it
// must be retired through verification's own port -- otherwise a verifier approves a
// submission the bucket no longer counts and the UI reports that non-decision as a
// success.
func TestWithdrawWeighingVerificationRetiresBySourceRef(t *testing.T) {
	creator := &captureCreator{}
	observationID := "22222222-2222-4222-8222-222222222222"
	if err := New(creator).WithdrawWeighingVerification(
		context.Background(),
		"11111111-1111-4111-8111-111111111111",
		weighingdomain.VerificationRefTypeShed,
		[]string{observationID},
	); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if len(creator.withdrawnCalls) != 1 {
		t.Fatalf("withdraw calls=%d, want 1", len(creator.withdrawnCalls))
	}
	call := creator.withdrawnCalls[0]
	if call.module != weighingdomain.VerificationModuleWeighing {
		t.Fatalf("source module=%q, want %q", call.module, weighingdomain.VerificationModuleWeighing)
	}
	if call.refType != weighingdomain.VerificationRefTypeShed {
		t.Fatalf("source ref_type=%q, want %q", call.refType, weighingdomain.VerificationRefTypeShed)
	}
	if len(call.refIDs) != 1 || call.refIDs[0] != observationID {
		t.Fatalf("source ref_ids=%v, want [%s]", call.refIDs, observationID)
	}
}

// An empty withdrawal must not reach verification at all: a reopen of an INDIVIDUAL
// bucket supersedes no lump-sum submission, and a no-op call would still cost a
// cross-module round trip.
func TestWithdrawWeighingVerificationNoopOnEmpty(t *testing.T) {
	creator := &captureCreator{}
	if err := New(creator).WithdrawWeighingVerification(context.Background(), "11111111-1111-4111-8111-111111111111", weighingdomain.VerificationRefTypeShed, nil); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if len(creator.withdrawnCalls) != 0 {
		t.Fatalf("withdraw calls=%d, want 0", len(creator.withdrawnCalls))
	}
}

// The weighing bridge must DECLARE that it acks, and must actually ack. Those two
// halves have to travel together: a declaration with no ack parks every decided
// weighing item in "applying" forever, and an ack with no declaration is never
// looked at. This pins both against the same fake.
func TestWeighingBridgeDeclaresAndSendsTheApplyReceipt(t *testing.T) {
	creator := &captureCreator{}
	bridge := New(creator)

	if err := bridge.EnqueueWeighingVerification(context.Background(), weighingapp.VerificationEnqueueRequest{
		TenantID:       "tenant-1",
		ObservationID:  "obs-1",
		Category:       weighingdomain.VerificationRefTypeAnimal,
		ParkID:         "park-1",
		CapturedAt:     time.Now().UTC(),
		IdempotencyKey: "idem-ack-1",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if !creator.received.ApplierAckExpected {
		t.Fatal("weighing enqueued an item without applier_ack_expected; its decided items could never read as awaiting application")
	}

	if err := bridge.AckWeighingVerificationApplied(context.Background(), "tenant-1", weighingdomain.VerificationRefTypeAnimal, []string{"obs-1"}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if len(creator.appliedCalls) != 1 {
		t.Fatalf("apply-receipt calls=%d, want 1", len(creator.appliedCalls))
	}
	got := creator.appliedCalls[0]
	if got.tenantID != "tenant-1" || got.module != weighingdomain.VerificationModuleWeighing ||
		got.refType != weighingdomain.VerificationRefTypeAnimal || len(got.refIDs) != 1 || got.refIDs[0] != "obs-1" {
		t.Fatalf("apply-receipt targeted %+v, want weighing's own animal observation obs-1", got)
	}
	if got.appliedByModule != weighingdomain.VerificationModuleWeighing {
		t.Fatalf("applied_by_module=%q, want %q -- an ack must name the module that sent it", got.appliedByModule, weighingdomain.VerificationModuleWeighing)
	}

	// An empty batch must not manufacture a call.
	if err := bridge.AckWeighingVerificationApplied(context.Background(), "tenant-1", weighingdomain.VerificationRefTypeAnimal, nil); err != nil {
		t.Fatalf("ack empty: %v", err)
	}
	if len(creator.appliedCalls) != 1 {
		t.Fatalf("apply-receipt calls after an empty batch=%d, want 1", len(creator.appliedCalls))
	}
}
