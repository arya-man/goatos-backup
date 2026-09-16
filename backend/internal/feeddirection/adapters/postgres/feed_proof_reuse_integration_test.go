package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// E2E 2026-09-17 on the QA clone. "ONE CLIP CANNOT PROVE TWO BAGS" (AGENTS.md feed packing lock) and
// "every rework requires a new video" (docs/decisions/feed-transport-verification.md) had no
// executable check on the write path:
//   - a verifier REJECTED a packing video / a transport video, and the resubmit carrying the SAME
//     rejected capture was accepted and queued to the verifier again;
//   - pen A's three distribution captures submitted for pen B were accepted -- two verifier items
//     "proving" two pens with one set of clips.
// The store now refuses both, naming the slot, inside the write transaction.

func wantSlotRefusal(t *testing.T, err error, slot string) {
	t.Helper()
	var pe *authored.ProofError
	if !errors.Is(err, ports.ErrSOPProofSlotInvalid) || !errors.As(err, &pe) || pe.SlotKey != slot {
		t.Fatalf("err = %v, want feed_proof_slot_invalid naming %s", err, slot)
	}
}

func TestPackingReworkRefusesTheRejectedVideoAndAcceptsANewOne(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)
	first, err := repo.CompletePacking(ctx, packingParams())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.BouncePackingForRework(ctx, ports.BouncePackingParams{TenantID: fdTenant, CompletionID: first.CompletionID, Reason: "bag not visible"}); err != nil || !ok {
		t.Fatalf("bounce: %v %v", ok, err)
	}
	again := packingParams()
	again.IdempotencyKey = "feed-packing-key-rework-same"
	_, err = repo.CompletePacking(ctx, again)
	wantSlotRefusal(t, err, domain.SlotPackingVideo)

	fresh := packingParams()
	fresh.IdempotencyKey = "feed-packing-key-rework-new"
	fresh.PackingProofRef = "proof-packing-0002"
	res, err := repo.CompletePacking(ctx, fresh)
	if err != nil || !res.NewlyPending || res.CompletionID != first.CompletionID {
		t.Fatalf("fresh rework resubmit = %+v, %v", res, err)
	}
}

func TestDistributionReworkRefusesAnyRejectedCapture(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)
	first, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.BounceDistributionForRework(ctx, ports.BounceDistributionParams{TenantID: fdTenant, CompletionID: first.CompletionID, Reason: "water not visible"}); err != nil || !ok {
		t.Fatalf("bounce: %v %v", ok, err)
	}
	again := distributionParams()
	again.IdempotencyKey = "feed-distribution-key-rework-partial"
	again.WaterProofRef = "proof-water-0002" // only the water re-shot; weight photo + feed video are the rejected ones
	_, err = repo.CompleteDistribution(ctx, again)
	wantSlotRefusal(t, err, domain.SlotFeedWeightPhoto)
}

func TestWastageReworkRefusesTheRejectedVideo(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)
	first, err := repo.CompleteWastage(ctx, wastageParams())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.BounceWastageForRework(ctx, ports.BounceWastageParams{TenantID: fdTenant, CompletionID: first.CompletionID, Reason: "trough not visible"}); err != nil || !ok {
		t.Fatalf("bounce: %v %v", ok, err)
	}
	again := wastageParams()
	again.IdempotencyKey = "feed-wastage-key-rework-same"
	_, err = repo.CompleteWastage(ctx, again)
	wantSlotRefusal(t, err, domain.SlotWastageVideo)
}

func TestTransportReworkRefusesTheRejectedVideo(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)
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
		t.Fatalf("bounce: %v %v", ok, err)
	}
	submit.IdempotencyKey = "transport-reuse-2"
	_, err = repo.SubmitTransportAttempt(ctx, submit)
	wantSlotRefusal(t, err, domain.SlotTransportVideo)
	submit.IdempotencyKey, submit.ProofRef = "transport-reuse-3", "proof-transport-new"
	if res, err := repo.SubmitTransportAttempt(ctx, submit); err != nil || res.AttemptNo != 2 {
		t.Fatalf("fresh rework = %+v, %v", res, err)
	}
}

// One clip, two pens: pen B's submit naming pen A's captures is refused; a same-pen teammate
// re-send of the same captures is still the one write it always was.
func TestACaptureAlreadyProvingOnePenCannotProveAnother(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)
	if _, err := repo.CompleteDistribution(ctx, distributionParams()); err != nil {
		t.Fatal(err)
	}
	teammate := distributionParams()
	teammate.IdempotencyKey = "feed-distribution-key-teammate"
	if res, err := repo.CompleteDistribution(ctx, teammate); err != nil || res.NewlyPending {
		t.Fatalf("teammate re-send = %+v, %v; want the same pending write", res, err)
	}
	other := distributionParams()
	other.ShedID = fdShedB
	other.IdempotencyKey = "feed-distribution-key-other-pen"
	_, err := repo.CompleteDistribution(ctx, other)
	wantSlotRefusal(t, err, domain.SlotFeedWeightPhoto)

	// Across stages too: the packing video of a bag cannot be the distribution video of a pen.
	pack := packingParams()
	pack.PackingProofRef = "proof-distribution-0001"
	pack.IdempotencyKey = "feed-packing-key-cross-stage"
	_, err = repo.CompletePacking(ctx, pack)
	wantSlotRefusal(t, err, domain.SlotPackingVideo)
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
