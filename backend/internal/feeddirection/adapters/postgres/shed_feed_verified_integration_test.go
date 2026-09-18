package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The VERIFIED bar beside the DIRECTED bar on the Feed by pen chart (maintainer
// request 2026-09-18): the packed kg the verifier typed when approving each bag,
// folded to the pen-day. Every row is written through the production paths --
// PersistIssue for the sheet, CompletePacking / RecordPackingVerifiedQuantities /
// ApplyVerifiedPacking / BouncePackingForRework for the bags -- never by hand.
//
// Adversarial on the three ways a verified figure goes wrong:
//   - a reading on a bag whose verdict is still PENDING must not count (only
//     status='completed' stands), so a half-verified day reports "1 of 2 bags";
//   - a bag sent back to REWORK stays out, reading and all, until it is re-shot
//     and approved;
//   - the per-head figure divides by the SAME head count the directed bar uses,
//     so 1.8 kg over the pen's 10 head is 180.0 g beside directed 300.0 g;
//   - a day with no approved bag is EMPTY ("not yet verified"), never "0".
//
// Name carries the guard's grain dimensions: ONE-TO-MANY (two bags x their items fold to one
// day figure), PARK SCOPE (the caller's park set bounds the readings side too), STATUS BUCKETS
// (pending / completed / rework), and NO PAGE BOUNDARY (a whole-window aggregate with no
// limit/offset input, so no page can split a day).
func TestShedFeedVerifiedBarOneToManyParkScopeStatusBucketsNoPageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 8, 5, 9, 0, 0, 0, biztime.DefaultLocation())

	// CompletePacking resolves the pen against the catalog: the shed must sit in
	// the park and the partition must exist on shed_partitions.
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($2::uuid, $1::uuid, 'shed', 'CASTRO', 'Castro', 'active', $3::uuid, 1)
ON CONFLICT (location_id) DO NOTHING`, fdiTenant, fdiShedA, fdiPark); err != nil {
		t.Fatalf("seed shed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, '1', $3, 'active', 'manual')
ON CONFLICT (tenant_id, shed_id, normalized_label) DO NOTHING`, fdiTenant, fdiShedA, domain.PartitionMatchKey("1")); err != nil {
		t.Fatalf("seed partition: %v", err)
	}

	cell := func(session int32, qty string, rowSeq int32) domain.StoredCell {
		q := qty
		return domain.StoredCell{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
			PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: session, SessionLabel: "S",
			HeadCount: 10, Workflow: domain.WorkflowNormal,
			FeedItemLabel: "Masur Busa", FeedItemKey: "masur busa", QuantityKg: &q,
			SessionTotalKg: "0.000", RowSeq: rowSeq, ItemSeq: 0,
		}
	}
	persist := func(feedDay, fingerprint string) {
		t.Helper()
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: feedDay, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: fingerprint,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + feedDay + ":shedfeedverified",
			GeneratedBy:    "test",
			// Two bags on the sheet: 2.0 kg morning, 1.0 kg evening, 10 head -> 300.0 g directed.
			Cells: []domain.StoredCell{cell(1, "2.000", 0), cell(2, "1.000", 1)},
		}); err != nil {
			t.Fatalf("persist %s: %v", feedDay, err)
		}
	}
	persist("2026-08-06", "fp-sfv-1")
	persist("2026-08-07", "fp-sfv-2") // the never-verified day

	day := time.Date(2026, 8, 6, 0, 0, 0, 0, biztime.DefaultLocation())
	pack := func(session int32, shot string) string {
		t.Helper()
		res, err := repo.CompletePacking(ctx, ports.CompletePackingParams{
			TenantID: fdiTenant, ParkID: fdiPark, ShedID: fdiShedA, PartitionLabel: "1",
			SessionNo: session, TargetDate: day, Workflow: domain.WorkflowNormal,
			PackingProofRef: "proof-sfv-" + shot,
			CompletedBy:     "00000000-0000-4000-8000-000000000099",
			IdempotencyKey:  "feed-packing-sfv-" + shot,
			ActorID:         "00000000-0000-4000-8000-000000000099", ActorType: "operator",
		})
		if err != nil {
			t.Fatalf("complete packing session %d: %v", session, err)
		}
		return res.CompletionID
	}
	record := func(completionID string, enteredKg float64) {
		t.Helper()
		if err := repo.RecordPackingVerifiedQuantities(ctx, ports.RecordPackingVerifiedQuantitiesParams{
			TenantID: fdiTenant, CompletionID: completionID,
			Entries:    []ports.PackingVerifiedQuantity{{FeedItemKey: "masur busa", FeedItemLabel: "Masur Busa", EnteredKg: enteredKg}},
			RecordedBy: "00000000-0000-4000-8000-000000000077", IdempotencyKey: "rec-" + completionID,
		}); err != nil {
			t.Fatalf("record quantities %s: %v", completionID, err)
		}
	}
	approve := func(completionID string) {
		t.Helper()
		if _, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{
			TenantID: fdiTenant, CompletionID: completionID, VerifiedBy: "00000000-0000-4000-8000-000000000077",
		}); err != nil {
			t.Fatalf("approve %s: %v", completionID, err)
		}
	}
	read := func() []domain.ShedFeedPenDay {
		t.Helper()
		got, err := repo.ShedFeedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
			DateFrom: day, DateTo: time.Date(2026, 8, 7, 0, 0, 0, 0, biztime.DefaultLocation()),
		})
		if err != nil {
			t.Fatalf("ShedFeedAnalytics: %v", err)
		}
		if len(got.Rows) != 1 || got.Rows[0].OperationalLocationDisplay != "Castro 1" || len(got.Rows[0].Days) != 2 {
			t.Fatalf("want one pen Castro 1 with 2 days, got %+v", got.Rows)
		}
		return got.Rows[0].Days
	}

	// Sheet issued, nothing packed: both days say "not yet verified", never 0.
	for _, d := range read() {
		if d.VerifiedKg != "" || d.VerifiedPerHeadGrams != "" || d.VerifiedBags != 0 || d.PlannedBags != 2 {
			t.Errorf("unverified day %s: want empty verified figures and 0 of 2 bags, got %+v", d.FeedDay, d)
		}
		if d.PerHeadGrams != "300.0" {
			t.Errorf("directed side must be untouched: want 300.0 g, got %+v", d)
		}
	}

	// Bag 1 approved at 1.8 kg; bag 2 has a reading but its verdict is still PENDING.
	bag1 := pack(1, "s1")
	record(bag1, 1.8)
	approve(bag1)
	bag2 := pack(2, "s2-first-shot")
	record(bag2, 1.2)

	d := read()[0]
	if d.VerifiedKg != "1.800" || d.VerifiedPerHeadGrams != "180.0" || d.VerifiedBags != 1 || d.PlannedBags != 2 {
		t.Fatalf("half-verified day: want 1.800 kg = 180.0 g over the same 10 head, 1 of 2 bags (the pending bag must not count), got %+v", d)
	}

	// Bag 2 sent back for REWORK: still out, its reading with it.
	if bounced, err := repo.BouncePackingForRework(ctx, ports.BouncePackingParams{
		TenantID: fdiTenant, CompletionID: bag2, Reason: "video unclear",
	}); err != nil || !bounced {
		t.Fatalf("bounce bag 2: bounced=%v err=%v", bounced, err)
	}
	d = read()[0]
	if d.VerifiedKg != "1.800" || d.VerifiedBags != 1 {
		t.Fatalf("after rework of bag 2: want bag 1's 1.800 kg alone, 1 of 2 bags, got %+v", d)
	}

	// Bag 2 re-shot and approved at 1.4 kg: both bags stand, 3.2 kg = 320.0 g, 2 of 2.
	if again := pack(2, "s2-second-shot"); again != bag2 {
		t.Fatalf("re-shoot must update the same completion row in place: got %s, want %s", again, bag2)
	}
	record(bag2, 1.4)
	approve(bag2)
	d = read()[0]
	if d.VerifiedKg != "3.200" || d.VerifiedPerHeadGrams != "320.0" || d.VerifiedBags != 2 || d.PlannedBags != 2 {
		t.Fatalf("fully verified day: want 3.200 kg = 320.0 g, 2 of 2 bags, got %+v", d)
	}

	// Park scope binds the readings side as it binds the sheet side: the pen's own
	// park returns the same figure, a foreign park returns no pen at all -- never a
	// pen whose directed side is present and whose verified side leaked in from
	// another park (the two sides share $2).
	own, err := repo.ShedFeedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
		ParkIDs: []uuid.UUID{uuid.MustParse(fdiPark)}, DateFrom: day, DateTo: day,
	})
	if err != nil {
		t.Fatalf("own-park read: %v", err)
	}
	if len(own.Rows) != 1 || len(own.Rows[0].Days) != 1 || own.Rows[0].Days[0].VerifiedKg != "3.200" {
		t.Fatalf("own-park scope: want the same 3.200 kg verified, got %+v", own.Rows)
	}
	foreign, err := repo.ShedFeedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
		ParkIDs: []uuid.UUID{uuid.New()}, DateFrom: day, DateTo: day,
	})
	if err != nil {
		t.Fatalf("foreign-park read: %v", err)
	}
	if len(foreign.Rows) != 0 {
		t.Fatalf("foreign-park scope: want no rows, got %+v", foreign.Rows)
	}

	// The other day never had a bag packed and stays "not yet verified".
	other := read()[1]
	if other.FeedDay != "2026-08-07" || other.VerifiedKg != "" || other.VerifiedBags != 0 || other.PlannedBags != 2 {
		t.Errorf("never-packed day: want empty verified figures and 0 of 2 bags, got %+v", other)
	}
}
