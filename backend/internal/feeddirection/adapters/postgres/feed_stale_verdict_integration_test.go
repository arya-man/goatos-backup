package postgres

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// A verdict names the capture it judged (source.evidence_id = the item's first media ref). E2E
// 2026-09-17 on the QA clone: the rework verdict of a bag's FIRST video, delivered again after the
// crew had re-shot the bag, bounced the NEW, never-judged video to rework (with the old reason) and
// left its verifier item pending on a row already in rework. A late APPROVE of a first video --
// the afternoon correction reopens the bag, the crew re-shoots, the delayed approve lands -- would
// likewise complete a bag nobody had looked at. A verdict now applies only while the capture it
// judged is still the one the row holds.
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
