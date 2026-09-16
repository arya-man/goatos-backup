package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// A verdict names the capture it judged (source.evidence_id = the item's first media ref). E2E
// 2026-09-17 on the QA clone: the rework verdict of a bag's FIRST video, delivered again after the
// crew had re-shot the bag, bounced the NEW, never-judged video to rework (with the old reason) and
// left its verifier item pending on a row already in rework. A late APPROVE of a first video --
// the afternoon correction reopens the bag, the crew re-shoots, the delayed approve lands -- would
// likewise complete a bag nobody had looked at. A verdict now applies only while the capture it
// judged is still the one the row holds.
//
// With identical captures (deploy-day parity still allows a resubmit to name the rejected capture
// again) the evidence check cannot tell the rounds apart, so the ROUND fence decides: the verdict's
// item must still be the newest verification item for the completion. Pinned by
// TestAStaleVerdictIsIgnoredEvenWhenTheResubmitReusesTheJudgedCapture.
func TestAStaleVerdictNeverAppliesToTheResubmittedWork(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	first, err := repo.CompletePacking(ctx, packingParams())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.BouncePackingForRework(ctx, ports.BouncePackingParams{TenantID: fdTenant, CompletionID: first.CompletionID, Reason: "bag not visible", EvidenceID: "proof-packing-0001"}); err != nil || !ok {
		t.Fatalf("first bounce: %v %v", ok, err)
	}
	again := packingParams()
	again.IdempotencyKey, again.PackingProofRef = "feed-packing-key-stale-2", "proof-packing-0002"
	if _, err := repo.CompletePacking(ctx, again); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.BouncePackingForRework(ctx, ports.BouncePackingParams{TenantID: fdTenant, CompletionID: first.CompletionID, Reason: "bag not visible", EvidenceID: "proof-packing-0001"}); err != nil || ok {
		t.Fatalf("replayed stale rework verdict applied=%v err=%v; want it ignored", ok, err)
	}
	if ok, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{TenantID: fdTenant, CompletionID: first.CompletionID, EvidenceID: "proof-packing-0001"}); err != nil || ok {
		t.Fatalf("stale approve applied=%v err=%v; want it ignored", ok, err)
	}
	if ok, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{TenantID: fdTenant, CompletionID: first.CompletionID, EvidenceID: "proof-packing-0002"}); err != nil || !ok {
		t.Fatalf("the real approve of the new video applied=%v err=%v", ok, err)
	}

	d, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.ApplyVerifiedDistribution(ctx, ports.ApplyDistributionParams{TenantID: fdTenant, CompletionID: d.CompletionID, EvidenceID: "some-other-capture"}); err != nil || ok {
		t.Fatalf("distribution approve naming a capture the row does not hold applied=%v err=%v", ok, err)
	}
	w, err := repo.CompleteWastage(ctx, wastageParams())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.BounceWastageForRework(ctx, ports.BounceWastageParams{TenantID: fdTenant, CompletionID: w.CompletionID, Reason: "x", EvidenceID: "some-other-capture"}); err != nil || ok {
		t.Fatalf("wastage rework naming a capture the row does not hold applied=%v err=%v", ok, err)
	}
	// A verdict with no evidence id (an older producer's event) keeps the pre-existing behaviour.
	if ok, err := repo.BounceWastageForRework(ctx, ports.BounceWastageParams{TenantID: fdTenant, CompletionID: w.CompletionID, Reason: "x"}); err != nil || !ok {
		t.Fatalf("evidence-less rework applied=%v err=%v; want applied", ok, err)
	}
}

// seedFeedItem inserts a verification item for a feed completion -- the queue row the real enqueue
// would have created for one submission round (the round fence reads only these columns).
func seedFeedItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID, refType, category, completionID, evidence string, createdAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_ref_type, source_ref_id,
  media_refs, status, captured_at, idempotency_key, created_at, updated_at)
VALUES ($1::uuid, $2::uuid, 'feed', 'feed', $3, 'feed', $4, $5::uuid, jsonb_build_array($6::text), 'pending', $7, $1::text, $7, $7)`,
		itemID, fdTenant, category, refType, completionID, evidence, createdAt); err != nil {
		t.Fatalf("seed verification item: %v", err)
	}
}

func TestAStaleVerdictIsIgnoredEvenWhenTheResubmitReusesTheJudgedCapture(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)
	t0 := time.Date(2026, 7, 21, 18, 0, 0, 0, biztime.DefaultLocation())
	const oldItem, newItem = "fd000000-0000-4000-8000-00000000a001", "fd000000-0000-4000-8000-00000000a002"

	first, err := repo.CompletePacking(ctx, packingParams())
	if err != nil {
		t.Fatal(err)
	}
	seedFeedItem(t, ctx, pool, oldItem, "feed_packing_completion", "feed_packing", first.CompletionID, "proof-packing-0001", t0)
	if ok, err := repo.BouncePackingForRework(ctx, ports.BouncePackingParams{TenantID: fdTenant, CompletionID: first.CompletionID, Reason: "bag not visible", EvidenceID: "proof-packing-0001", ItemID: oldItem}); err != nil || !ok {
		t.Fatalf("first bounce (the old item is still the newest): %v %v", ok, err)
	}
	same := packingParams()
	same.IdempotencyKey = "feed-packing-key-stale-same"
	if res, err := repo.CompletePacking(ctx, same); err != nil || !res.NewlyPending {
		t.Fatalf("resubmit naming the rejected video (accepted, as today) = %+v, %v", res, err)
	}
	seedFeedItem(t, ctx, pool, newItem, "feed_packing_completion", "feed_packing", first.CompletionID, "proof-packing-0001", t0.Add(time.Hour))

	if ok, err := repo.BouncePackingForRework(ctx, ports.BouncePackingParams{TenantID: fdTenant, CompletionID: first.CompletionID, Reason: "bag not visible", EvidenceID: "proof-packing-0001", ItemID: oldItem}); err != nil || ok {
		t.Fatalf("re-delivered OLD rework verdict applied=%v err=%v; want ignored (a newer item exists)", ok, err)
	}
	if ok, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{TenantID: fdTenant, CompletionID: first.CompletionID, EvidenceID: "proof-packing-0001", ItemID: oldItem}); err != nil || ok {
		t.Fatalf("late OLD approve applied=%v err=%v; want ignored", ok, err)
	}
	if ok, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{TenantID: fdTenant, CompletionID: first.CompletionID, EvidenceID: "proof-packing-0001", ItemID: newItem}); err != nil || !ok {
		t.Fatalf("the NEW item's approve applied=%v err=%v; want applied", ok, err)
	}

	// Same round fence on distribution and wastage.
	d, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatal(err)
	}
	seedFeedItem(t, ctx, pool, "fd000000-0000-4000-8000-00000000b001", "feed_distribution_completion", "feed_distribution", d.CompletionID, "proof-feed-weight-photo-0001", t0)
	seedFeedItem(t, ctx, pool, "fd000000-0000-4000-8000-00000000b002", "feed_distribution_completion", "feed_distribution", d.CompletionID, "proof-feed-weight-photo-0001", t0.Add(time.Hour))
	if ok, err := repo.ApplyVerifiedDistribution(ctx, ports.ApplyDistributionParams{TenantID: fdTenant, CompletionID: d.CompletionID, EvidenceID: "proof-feed-weight-photo-0001", ItemID: "fd000000-0000-4000-8000-00000000b001"}); err != nil || ok {
		t.Fatalf("distribution old-item approve applied=%v err=%v; want ignored", ok, err)
	}
	w, err := repo.CompleteWastage(ctx, wastageParams())
	if err != nil {
		t.Fatal(err)
	}
	seedFeedItem(t, ctx, pool, "fd000000-0000-4000-8000-00000000c001", "feed_wastage_completion", "feed_wastage", w.CompletionID, "proof-wastage-0001", t0)
	seedFeedItem(t, ctx, pool, "fd000000-0000-4000-8000-00000000c002", "feed_wastage_completion", "feed_wastage", w.CompletionID, "proof-wastage-0001", t0.Add(time.Hour))
	if ok, err := repo.BounceWastageForRework(ctx, ports.BounceWastageParams{TenantID: fdTenant, CompletionID: w.CompletionID, Reason: "x", EvidenceID: "proof-wastage-0001", ItemID: "fd000000-0000-4000-8000-00000000c001"}); err != nil || ok {
		t.Fatalf("wastage old-item rework applied=%v err=%v; want ignored", ok, err)
	}
	if ok, err := repo.BounceWastageForRework(ctx, ports.BounceWastageParams{TenantID: fdTenant, CompletionID: w.CompletionID, Reason: "x", EvidenceID: "proof-wastage-0001", ItemID: "fd000000-0000-4000-8000-00000000c002"}); err != nil || !ok {
		t.Fatalf("wastage newest-item rework applied=%v err=%v; want applied", ok, err)
	}
}
