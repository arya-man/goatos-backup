package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// DEPLOY-DAY PARITY lets a rework resubmit name the SAME capture the verifier rejected. Verification
// items are idempotent on (tenant, idempotency_key) alone, so an enqueue key built from the refs
// would collapse the new round onto the already-rejected item and leave nothing pending (the defect
// the shifting agent found). Every feed stage keys the item on the ROUND instead: the completion's
// row_version (distribution, packing, wastage -- a verifier bounce, the afternoon reopen and the
// resubmit each bump it) or the transport attempt. Same refs, new round => new key => a fresh item;
// an exact retry of one round => the same key => no duplicate.

func TestEveryStageQueuesAFreshItemForAResubmitNamingTheSameCaptures(t *testing.T) {
	ctx := context.Background()
	refs := authored.ProofRefs{domain.SlotPackingVideo: "same-video"}

	// PACKING (also the bag the afternoon correction reopened: reopen bumps row_version too).
	pstore := &metaPackingStore{}
	penq := &recordingPackingEnqueuer{}
	psvc := packingSvc(t, pstore, penq, seededAtMetaVersion())
	for _, rv := range []int32{1, 1, 3, 5} { // submit, exact retry, rework resubmit, resubmit after reopen
		pstore.result = ports.CompletePackingResult{CompletionID: "pk-1", Status: domain.PackingStatusPendingVerification, RowVersion: rv, NewlyPending: true, SOPProofs: refs}
		in := packingIn()
		in.PackingProofRef = "same-video"
		if _, err := psvc.CompletePacking(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	assertRoundKeys(t, "packing", keysOf(len(penq.calls), func(i int) string { return penq.calls[i].IdempotencyKey }),
		[]string{"feed-packing-verification:pk-1:1", "feed-packing-verification:pk-1:1", "feed-packing-verification:pk-1:3", "feed-packing-verification:pk-1:5"})

	// DISTRIBUTION
	dstore := &fakeDistributionStore{}
	denq := &fakeDistributionEnqueuer{}
	dsvc := NewService(nil, nil).WithIssueStore(metaIssueStore(t, domain.WorkflowNormal)).WithSOPRules(seededAtMetaVersion()).WithDistributionStore(dstore).WithDistributionVerificationEnqueuer(denq).WithProofValidator(&fakeProofValidator{})
	var dkeys []string
	for _, rv := range []int32{1, 1, 3} {
		dstore.completeResult = ports.CompleteDistributionResult{CompletionID: "d-1", Status: domain.DistributionStatusPendingVerification, RowVersion: rv, NewlyPending: true}
		if _, err := dsvc.CompleteDistribution(ctx, validCompleteDistributionInput()); err != nil {
			t.Fatal(err)
		}
		dkeys = append(dkeys, denq.last.IdempotencyKey)
	}
	assertRoundKeys(t, "distribution", dkeys, []string{"feed-distribution-verification:d-1:1", "feed-distribution-verification:d-1:1", "feed-distribution-verification:d-1:3"})

	// WASTAGE
	wstore := &fakeWastageStore{}
	wenq := &recordingWastageEnqueuer{}
	wsvc, _ := newWastageService(wstore, wenq)
	for _, rv := range []int32{1, 1, 3} {
		wstore.result = ports.CompleteWastageResult{CompletionID: "w-1", Status: domain.WastageStatusPendingVerification, RowVersion: rv, NewlyPending: true}
		if _, err := wsvc.CompleteWastage(ctx, wastageInput(shedA)); err != nil {
			t.Fatal(err)
		}
	}
	assertRoundKeys(t, "wastage", keysOf(len(wenq.calls), func(i int) string { return wenq.calls[i].IdempotencyKey }),
		[]string{"feed-wastage-verification:w-1:1", "feed-wastage-verification:w-1:1", "feed-wastage-verification:w-1:3"})

	// TRANSPORT: a rework is a new attempt row.
	tstore := &transportServiceStore{task: ports.FeedTransportTask{TaskID: "task-1", ParkID: "park-1", ShedID: "shed-1"}}
	tenq := &recordingTransportEnqueuer{}
	tsvc := NewService(nil, nil).WithTransportStore(tstore).WithProofValidator(&transportProofValidator{}).WithTransportVerificationEnqueuer(tenq)
	for _, a := range []struct {
		id string
		no int32
	}{{"a-1", 1}, {"a-1", 1}, {"a-2", 2}} {
		tstore.result = ports.SubmitTransportResult{AttemptID: a.id, AttemptNo: a.no, Status: "verification_due", NewlyPending: true}
		if _, err := tsvc.SubmitTransport(ctx, SubmitTransportInput{TenantID: "tenant-1", TaskID: "task-1", OperatorID: "op", IdempotencyKey: "transport-round-1", ProofRef: "same-video"}); err != nil {
			t.Fatal(err)
		}
	}
	assertRoundKeys(t, "transport", keysOf(len(tenq.calls), func(i int) string { return tenq.calls[i].IdempotencyKey }),
		[]string{"feed-transport-verification:a-1:1", "feed-transport-verification:a-1:1", "feed-transport-verification:a-2:2"})
}

func keysOf(n int, at func(int) string) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, at(i))
	}
	return out
}

func assertRoundKeys(t *testing.T, stage string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s enqueue keys = %v, want %v", stage, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s enqueue keys = %v, want %v (same captures, new round => new item key)", stage, got, want)
		}
	}
}

// seededAtMetaVersion is the seeded cards published at the version metaIssueStore pins.
func seededAtMetaVersion() ports.StaticSOPRules {
	by := map[string]domain.Rules{}
	for _, stage := range []string{domain.StageDistribution, domain.StagePacking, domain.StageWastage, domain.StageTransport} {
		r := domain.SeededRules(stage)
		r.Version = metaCardVersion
		by[stage] = r
	}
	return ports.StaticSOPRules{ByStage: by}
}
