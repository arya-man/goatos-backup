package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// THE DISTRIBUTION VERIFIER RECORDS THE TOTAL FEED (maintainer decision 2026-09-28), against the
// REAL schema (migration 000455's columns and CHECK, the audit writer, the packing/issue tables the
// plan reads).
//
// Pins, each on a DB round trip:
//   - the plan is the frozen sheet summed over EVERY feed item of the pen-session (the trough gets
//     them mixed), falling back from -- and overridden by -- the same pen-session's packed-against
//     snapshot, and a bag in rework is NOT trusted as the plan;
//   - the reading lands on the completion with the plan it was checked against and her
//     confirmation, audited;
//   - an out-of-range total writes nothing;
//   - a rework re-submit CLEARS the reading, so the fresh item cannot approve blank on the strength
//     of a number read off the rejected video.
func TestDistributionVerifiedFeedPlanRecordAndReworkClear(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)

	// Frozen sheet, session 1 of shed A on 2026-07-22: concentrate 2.000 + hay 1.000 = 3.000 kg.
	issuedAt := time.Date(2026, 7, 21, 9, 0, 0, 0, biztime.DefaultLocation())
	conc, hay := "2.000", "1.000"
	cell := func(label, key string, qty *string, seq int32) domain.StoredCell {
		return domain.StoredCell{
			ParkID: fdPark, ParkLabel: "CBE", ShedID: fdShedA, ShedLabel: "Castro",
			PartitionLabel: "", ShedTag: "Non-Pregnant", Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
			HeadCount: 10, Workflow: domain.WorkflowNormal,
			FeedItemLabel: label, FeedItemKey: key, QuantityKg: qty,
			SessionTotalKg: "3.000", RowSeq: 0, ItemSeq: seq,
		}
	}
	if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
		TenantID: fdTenant, ParkID: fdPark, FeedDay: "2026-07-22", Workflow: domain.WorkflowNormal,
		IssuedAt: issuedAt, Fingerprint: "fp-distribution-total",
		IdempotencyKey: "issue:distribution-total:1", GeneratedBy: "test",
		Cells: []domain.StoredCell{cell("Concentrate", "concentrate", &conc, 0), cell("Hay", "hay", &hay, 1)},
	}); err != nil {
		t.Fatalf("PersistIssue: %v", err)
	}

	dist, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatalf("CompleteDistribution: %v", err)
	}

	// 1. No packing bag yet -> the frozen sheet, every item summed.
	assertPlan := func(label string, want float64, wantOK bool) {
		t.Helper()
		got, ok, err := repo.DistributionPlannedFeedKg(ctx, fdTenant, dist.CompletionID)
		if err != nil {
			t.Fatalf("%s: DistributionPlannedFeedKg: %v", label, err)
		}
		if ok != wantOK || (wantOK && (got < want-1e-9 || got > want+1e-9)) {
			t.Fatalf("%s: plan = (%v, %v), want (%v, %v)", label, got, ok, want, wantOK)
		}
	}
	assertPlan("sheet only", 3, true)

	// 2. The same pen-session's bag was packed against a snapshot of 3.4 kg -> the bag wins.
	withSnap := packingParams()
	withSnap.PackedAgainst = &ports.PackedAgainstSnapshot{
		HeadCount: 10, TotalKg: "3.400",
		Items: []ports.PackedItemSnapshot{
			{Key: "concentrate", Label: "Concentrate", QuantityKg: "2.400"},
			{Key: "hay", Label: "Hay", QuantityKg: "1.000"},
		},
	}
	packed, err := repo.CompletePacking(ctx, withSnap)
	if err != nil {
		t.Fatalf("CompletePacking: %v", err)
	}
	assertPlan("packed snapshot", 3.4, true)

	// 3. The bag is sent back for re-packing -> its snapshot is stale, the sheet answers again.
	if _, err := repo.BouncePackingForRework(ctx, ports.BouncePackingParams{
		TenantID: fdTenant, CompletionID: packed.CompletionID, Reason: "re-pack", TraceID: "trace-pack-rework",
	}); err != nil {
		t.Fatalf("BouncePackingForRework: %v", err)
	}
	assertPlan("bag in rework", 3, true)

	// Nothing recorded yet.
	if recorded, err := repo.DistributionVerifiedFeedRecorded(ctx, fdTenant, dist.CompletionID); err != nil || recorded {
		t.Fatalf("recorded before any reading = (%v, %v), want (false, nil)", recorded, err)
	}

	// 4. An out-of-range total writes nothing.
	if err := repo.RecordDistributionVerifiedFeed(ctx, ports.RecordDistributionVerifiedFeedParams{
		TenantID: fdTenant, CompletionID: dist.CompletionID, EnteredKg: domain.MaxDistributionTotalFeedKg + 1, RecordedBy: fdActor,
	}); !errors.Is(err, ports.ErrDistributionFeedOutOfRange) {
		t.Fatalf("out-of-range: want ErrDistributionFeedOutOfRange, got %v", err)
	}

	// 5. The reading lands with its plan and her confirmation, audited.
	plan := 3.0
	if err := repo.RecordDistributionVerifiedFeed(ctx, ports.RecordDistributionVerifiedFeedParams{
		TenantID: fdTenant, CompletionID: dist.CompletionID, EnteredKg: 3.6, PlannedKg: &plan,
		VarianceAcknowledged: true, RecordedBy: fdActor, IdempotencyKey: "verdict-1:measurement",
	}); err != nil {
		t.Fatalf("RecordDistributionVerifiedFeed: %v", err)
	}
	var (
		enteredKg, plannedKg string
		acknowledged         bool
		recordedBy           string
	)
	if err := pool.QueryRow(ctx, `
SELECT verified_feed_kg::text, verified_planned_feed_kg::text, verified_feed_variance_acknowledged,
       verified_feed_recorded_by::text
FROM feed_distribution_completions WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`,
		fdTenant, dist.CompletionID).Scan(&enteredKg, &plannedKg, &acknowledged, &recordedBy); err != nil {
		t.Fatalf("read reading: %v", err)
	}
	if enteredKg != "3.600" || plannedKg != "3.000" || !acknowledged || recordedBy != fdActor {
		t.Fatalf("stored reading = (%s, %s, %v, %s), want (3.600, 3.000, true, %s)", enteredKg, plannedKg, acknowledged, recordedBy, fdActor)
	}
	var audits int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM audit_log
WHERE tenant_id = $1::uuid AND action = 'feed.distribution.total_feed_recorded' AND resource_id = $2`,
		fdTenant, dist.CompletionID).Scan(&audits); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if audits != 1 {
		t.Fatalf("audit rows = %d, want 1", audits)
	}
	if recorded, err := repo.DistributionVerifiedFeedRecorded(ctx, fdTenant, dist.CompletionID); err != nil || !recorded {
		t.Fatalf("recorded after reading = (%v, %v), want (true, nil)", recorded, err)
	}

	// 6. Rejected and re-shot: the fresh submission carries NO reading.
	if _, err := repo.BounceDistributionForRework(ctx, ports.BounceDistributionParams{
		TenantID: fdTenant, CompletionID: dist.CompletionID, Reason: "scale not visible", TraceID: "trace-dist-rework",
	}); err != nil {
		t.Fatalf("BounceDistributionForRework: %v", err)
	}
	resubmit := distributionParams()
	resubmit.IdempotencyKey = "feed-distribution-key-0002"
	resubmit.FeedWeightProofRef = "proof-feed-weight-photo-0002"
	resubmit.DistributionProofRef = "proof-distribution-0002"
	resubmit.WaterProofRef = "proof-water-0002"
	if _, err := repo.CompleteDistribution(ctx, resubmit); err != nil {
		t.Fatalf("re-submit: %v", err)
	}
	if recorded, err := repo.DistributionVerifiedFeedRecorded(ctx, fdTenant, dist.CompletionID); err != nil || recorded {
		t.Fatalf("recorded after re-submit = (%v, %v), want (false, nil): a reading off the rejected video must not carry over", recorded, err)
	}
	var leftover bool
	if err := pool.QueryRow(ctx, `
SELECT verified_planned_feed_kg IS NOT NULL OR verified_feed_variance_acknowledged OR verified_feed_recorded_by IS NOT NULL
FROM feed_distribution_completions WHERE tenant_id = $1::uuid AND completion_id = $2::uuid`,
		fdTenant, dist.CompletionID).Scan(&leftover); err != nil {
		t.Fatalf("read cleared columns: %v", err)
	}
	if leftover {
		t.Fatal("re-submit left part of the old reading behind")
	}

	// 7. An unknown completion is named, on every entry point.
	const unknown = "fd000000-0000-4000-8000-00000000dead"
	if _, _, err := repo.DistributionPlannedFeedKg(ctx, fdTenant, unknown); !errors.Is(err, ports.ErrDistributionCompletionNotFound) {
		t.Fatalf("plan for unknown: %v", err)
	}
	if err := repo.RecordDistributionVerifiedFeed(ctx, ports.RecordDistributionVerifiedFeedParams{
		TenantID: fdTenant, CompletionID: unknown, EnteredKg: 3, RecordedBy: fdActor,
	}); !errors.Is(err, ports.ErrDistributionCompletionNotFound) {
		t.Fatalf("record for unknown: %v", err)
	}
	if _, err := repo.DistributionVerifiedFeedRecorded(ctx, fdTenant, unknown); !errors.Is(err, ports.ErrDistributionCompletionNotFound) {
		t.Fatalf("recorded for unknown: %v", err)
	}
}

// A pen-session with no readable plan (no live issue) answers ok=false, so the applier checks
// nothing rather than warning against zero.
func TestDistributionPlannedFeedKgWithNoSheetIsNotAPlanOfZero(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	dist, err := repo.CompleteDistribution(ctx, distributionParams())
	if err != nil {
		t.Fatalf("CompleteDistribution: %v", err)
	}
	got, ok, err := repo.DistributionPlannedFeedKg(ctx, fdTenant, dist.CompletionID)
	if err != nil {
		t.Fatalf("DistributionPlannedFeedKg: %v", err)
	}
	if ok || got != 0 {
		t.Fatalf("plan = (%v, %v), want (0, false)", got, ok)
	}
}
