package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// DEPLOY-DAY PARITY (maintainer rule, 2026-09-17): nothing in daily operations changes unless
// someone edits an SOP. The two stricter capture rules the Phase A feed E2E recommended are
// written up in docs/decisions/feed-sop.md under "Recommended, awaiting maintainer approval (not
// enabled)". These tests pin TODAY's behaviour so that enabling either is a deliberate change that
// turns them red, never an accident.

// Today a rework resubmit may name the very capture the verifier rejected (and a bag reopened by
// the afternoon correction may be resubmitted with its old video): the row goes back to
// pending_verification and a fresh verifier item is queued.
func TestReworkResubmitMayNameTheRejectedCaptureAsToday(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	pack, err := repo.CompletePacking(ctx, packingParams())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.BouncePackingForRework(ctx, ports.BouncePackingParams{TenantID: fdTenant, CompletionID: pack.CompletionID, Reason: "bag not visible"}); err != nil || !ok {
		t.Fatalf("bounce packing: %v %v", ok, err)
	}
	packAgain := packingParams()
	packAgain.IdempotencyKey = "feed-packing-key-rework-same"
	if res, err := repo.CompletePacking(ctx, packAgain); err != nil || !res.NewlyPending {
		t.Fatalf("packing resubmit with the rejected video = %+v, %v; want accepted as today", res, err)
	}

	dist, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.BounceDistributionForRework(ctx, ports.BounceDistributionParams{TenantID: fdTenant, CompletionID: dist.CompletionID, Reason: "water not visible"}); err != nil || !ok {
		t.Fatalf("bounce distribution: %v %v", ok, err)
	}
	distAgain := distributionParams()
	distAgain.IdempotencyKey = "feed-distribution-key-rework-partial"
	distAgain.WaterProofRef = "proof-water-0002"
	if res, err := repo.CompleteDistribution(ctx, distAgain); err != nil || !res.NewlyPending {
		t.Fatalf("distribution resubmit keeping two rejected captures = %+v, %v; want accepted as today", res, err)
	}

	w, err := repo.CompleteWastage(ctx, wastageParams())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.BounceWastageForRework(ctx, ports.BounceWastageParams{TenantID: fdTenant, CompletionID: w.CompletionID, Reason: "trough not visible"}); err != nil || !ok {
		t.Fatalf("bounce wastage: %v %v", ok, err)
	}
	wAgain := wastageParams()
	wAgain.IdempotencyKey = "feed-wastage-key-rework-same"
	if res, err := repo.CompleteWastage(ctx, wAgain); err != nil || !res.NewlyPending {
		t.Fatalf("wastage resubmit with the rejected video = %+v, %v; want accepted as today", res, err)
	}

	day := time.Date(2026, 7, 29, 0, 0, 0, 0, biztime.DefaultLocation())
	if _, err := repo.MaterializeTransportTasks(ctx, ports.MaterializeTransportParams{TenantID: fdTenant, AsOf: day.Add(16 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListTransportTasks(ctx, ports.ListTransportTasksParams{TenantID: fdTenant, Day: day, ActorID: transportOperator, Limit: 20})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("transport tasks: %v (%d)", err, len(page.Items))
	}
	submit := ports.SubmitTransportParams{TenantID: fdTenant, TaskID: page.Items[0].TaskID, ProofRef: "proof-transport-rejected", OperatorID: transportOperator, IdempotencyKey: "transport-reuse-1", ActorID: transportOperator, ActorType: "operator"}
	first, err := repo.SubmitTransportAttempt(ctx, submit)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.BounceTransportForRework(ctx, ports.BounceTransportParams{TenantID: fdTenant, AttemptID: first.AttemptID, Reason: "load not visible"}); err != nil || !ok {
		t.Fatalf("bounce transport: %v %v", ok, err)
	}
	submit.IdempotencyKey = "transport-reuse-2"
	if res, err := repo.SubmitTransportAttempt(ctx, submit); err != nil || res.AttemptNo != 2 {
		t.Fatalf("transport rework with the rejected video = %+v, %v; want attempt 2 accepted as today", res, err)
	}
}

// Today a capture already proving one pen (or one bag) is accepted for another pen or another
// stage: each write queues its own verifier item.
func TestACaptureProvingOnePenIsAcceptedForAnotherAsToday(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)
	a, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatal(err)
	}
	other := distributionParams()
	other.ShedID = fdShedB
	other.IdempotencyKey = "feed-distribution-key-other-pen"
	b, err := repo.CompleteDistribution(ctx, other)
	if err != nil || !b.NewlyPending || b.CompletionID == a.CompletionID {
		t.Fatalf("pen B with pen A's captures = %+v, %v; want a second pending completion as today", b, err)
	}
	pack := packingParams()
	pack.PackingProofRef = "proof-distribution-0001"
	pack.IdempotencyKey = "feed-packing-key-cross-stage"
	if res, err := repo.CompletePacking(ctx, pack); err != nil || !res.NewlyPending {
		t.Fatalf("packing naming a distribution video = %+v, %v; want accepted as today", res, err)
	}
}

// HEAL support: a packing / wastage replay hands back the ROW's captures and answers.
func TestPackingAndWastageReplayReturnTheStoredCard(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)
	p := packingParams()
	p.SOPAnswers = authored.Answers{"bags": []byte("2")}
	if _, err := repo.CompletePacking(ctx, p); err != nil {
		t.Fatal(err)
	}
	replay, err := repo.CompletePacking(ctx, p)
	if err != nil || replay.SOPProofs[domain.SlotPackingVideo] != "proof-packing-0001" || string(replay.SOPAnswers["bags"]) != "2" {
		t.Fatalf("packing replay = %+v, %v; want the stored card", replay, err)
	}
	w := wastageParams()
	w.SOPAnswers = authored.Answers{"left": answer("bhusa")}
	if _, err := repo.CompleteWastage(ctx, w); err != nil {
		t.Fatal(err)
	}
	wr, err := repo.CompleteWastage(ctx, w)
	if err != nil || wr.SOPProofs[domain.SlotWastageVideo] != "proof-wastage-0001" || string(wr.SOPAnswers["left"]) != `"bhusa"` {
		t.Fatalf("wastage replay = %+v, %v; want the stored card", wr, err)
	}
}
