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

// The FEED VERIFICATION panel on /verify (maintainer decision 2026-09-28): one feed day's packed
// bags, per park, pen and session, the plan beside the verifier's reading. Every row is written
// through the production paths -- PersistIssue for the sheet, CompletePacking /
// RecordPackingVerifiedQuantities / ApplyVerifiedPacking / BouncePackingForRework for the bags.
//
// Adversarial on the one property that makes this read safe to show a verifier -- THE PLAN IS
// WITHHELD UNTIL THE VERDICT STANDS -- in every state a bag passes through:
//   - not packed: the bag lists with its feeds and NO plan;
//   - packed and read, verdict PENDING: still no plan, and her reading is not echoed either;
//   - sent back for REWORK: no plan;
//   - approved: plan, reading and difference, and the plan she was checked against wins over the
//     sheet's;
//   - a feed the sheet authors at 0 kg is not in the bag and is never listed;
//
// plus park scope on both sides, the four status buckets summing to the bag count, day kg totals
// over verified bags only, and the operational-location display.
//
// Name carries the guard's grain dimensions: ONE-TO-MANY (two feed items per bag), PARK SCOPE,
// STATUS BUCKETS, and NO PAGE BOUNDARY (one whole feed day, no limit/offset input).
func TestPackingVerificationLogOneToManyParkScopeStatusBucketsNoPageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 8, 5, 9, 0, 0, 0, biztime.DefaultLocation())

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

	cell := func(session int32, label, key, qty string, rowSeq, itemSeq int32) domain.StoredCell {
		q := qty
		return domain.StoredCell{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
			PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: session, SessionLabel: map[int32]string{1: "Morning", 2: "Evening"}[session],
			HeadCount: 10, Workflow: domain.WorkflowNormal,
			FeedItemLabel: label, FeedItemKey: key, QuantityKg: &q,
			SessionTotalKg: "0.000", RowSeq: rowSeq, ItemSeq: itemSeq,
		}
	}
	const feedDay = "2026-08-06"
	if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: feedDay, Workflow: domain.WorkflowNormal,
		IssuedAt: issuedAt, Fingerprint: "fp-pvl-1",
		IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + feedDay + ":pvl",
		GeneratedBy:    "test",
		// Two bags, two feeds each: morning 2.0 + 0.5, evening 1.0 + 0.25.
		Cells: []domain.StoredCell{
			cell(1, "Masur Busa", domain.NormalizeConfigKey("Masur Busa"), "2.000", 0, 0),
			cell(1, "Concentrate", domain.NormalizeConfigKey("Concentrate"), "0.500", 0, 1),
			// Authored at 0 kg for this pen: not in the bag, so never listed.
			cell(1, "Baking Soda", domain.NormalizeConfigKey("Baking Soda"), "0.000", 0, 2),
			cell(2, "Masur Busa", domain.NormalizeConfigKey("Masur Busa"), "1.000", 1, 0),
			cell(2, "Concentrate", domain.NormalizeConfigKey("Concentrate"), "0.250", 1, 1),
		},
	}); err != nil {
		t.Fatalf("persist sheet: %v", err)
	}

	day := time.Date(2026, 8, 6, 0, 0, 0, 0, biztime.DefaultLocation())
	pack := func(session int32, shot string) string {
		t.Helper()
		res, err := repo.CompletePacking(ctx, ports.CompletePackingParams{
			TenantID: fdiTenant, ParkID: fdiPark, ShedID: fdiShedA, PartitionLabel: "1",
			SessionNo: session, TargetDate: day, Workflow: domain.WorkflowNormal,
			PackingProofRef: "proof-pvl-" + shot,
			CompletedBy:     "00000000-0000-4000-8000-000000000099",
			IdempotencyKey:  "feed-packing-pvl-" + shot,
			ActorID:         "00000000-0000-4000-8000-000000000099", ActorType: "operator",
		})
		if err != nil {
			t.Fatalf("complete packing session %d: %v", session, err)
		}
		return res.CompletionID
	}
	record := func(completionID string, busa, conc float64, busaChecked *float64) {
		t.Helper()
		if err := repo.RecordPackingVerifiedQuantities(ctx, ports.RecordPackingVerifiedQuantitiesParams{
			TenantID: fdiTenant, CompletionID: completionID,
			Entries: []ports.PackingVerifiedQuantity{
				{FeedItemKey: domain.NormalizeConfigKey("Masur Busa"), FeedItemLabel: "Masur Busa", EnteredKg: busa, PlannedKg: busaChecked},
				{FeedItemKey: domain.NormalizeConfigKey("Concentrate"), FeedItemLabel: "Concentrate", EnteredKg: conc},
			},
			RecordedBy: "00000000-0000-4000-8000-000000000077", IdempotencyKey: "rec-" + completionID,
		}); err != nil {
			t.Fatalf("record quantities %s: %v", completionID, err)
		}
	}
	read := func(parkIDs []uuid.UUID) domain.PackingVerificationLog {
		t.Helper()
		got, err := repo.PackingVerificationLog(ctx, fdiTenant, parkIDs, day)
		if err != nil {
			t.Fatalf("PackingVerificationLog: %v", err)
		}
		return got
	}
	assertNoPlan := func(bag domain.PackingLogBag, state string) {
		t.Helper()
		if bag.PlannedTotalKg != "" || bag.EnteredTotalKg != "" {
			t.Fatalf("%s bag leaked a figure: planned=%q entered=%q", state, bag.PlannedTotalKg, bag.EnteredTotalKg)
		}
		if len(bag.Items) != 2 {
			t.Fatalf("%s bag: want its two feeds listed, got %+v", state, bag.Items)
		}
		for _, it := range bag.Items {
			if it.PlannedKg != "" || it.EnteredKg != "" || it.DifferenceKg != "" {
				t.Fatalf("%s bag leaked item figures: %+v", state, it)
			}
		}
	}

	// Nothing packed: two bags listed, no plan anywhere.
	got := read(nil)
	if got.FeedDay != feedDay || got.PackingDay != "2026-08-05" {
		t.Fatalf("days: want feed 2026-08-06 packed 2026-08-05, got %q / %q", got.FeedDay, got.PackingDay)
	}
	if len(got.Bags) != 2 {
		t.Fatalf("want 2 bags, got %+v", got.Bags)
	}
	for _, bag := range got.Bags {
		if bag.Status != domain.PackingLogStatusNotPacked || bag.OperationalLocationDisplay != "Castro 1" || bag.ParkLabel != "CBE" {
			t.Fatalf("unpacked bag: %+v", bag)
		}
		assertNoPlan(bag, "not packed")
	}
	if got.Bags[0].SessionNo != 1 || got.Bags[0].SessionLabel != "Morning" || got.Bags[0].Items[0].FeedItemLabel != "Masur Busa" {
		t.Fatalf("order: want session 1 Morning first with the sheet's item order, got %+v", got.Bags[0])
	}

	// Both packed and read; verdicts PENDING. Still no plan, and her pending reading is not echoed.
	bag1 := pack(1, "s1")
	record(bag1, 1.8, 0.5, floatPtr(1.9))
	bag2 := pack(2, "s2")
	record(bag2, 1.2, 0.3, nil)
	for _, bag := range read(nil).Bags {
		if bag.Status != domain.PackingLogStatusAwaitingVerification {
			t.Fatalf("want awaiting verification, got %+v", bag)
		}
		assertNoPlan(bag, "pending")
	}

	// Bag 1 approved; bag 2 sent back.
	if _, err := repo.ApplyVerifiedPacking(ctx, ports.ApplyPackingParams{
		TenantID: fdiTenant, CompletionID: bag1, VerifiedBy: "00000000-0000-4000-8000-000000000077",
	}); err != nil {
		t.Fatalf("approve bag 1: %v", err)
	}
	if bounced, err := repo.BouncePackingForRework(ctx, ports.BouncePackingParams{
		TenantID: fdiTenant, CompletionID: bag2, Reason: "video unclear",
	}); err != nil || !bounced {
		t.Fatalf("bounce bag 2: bounced=%v err=%v", bounced, err)
	}

	got = read(nil)
	verified, rework := got.Bags[0], got.Bags[1]
	if verified.Status != domain.PackingLogStatusVerified || rework.Status != domain.PackingLogStatusRework {
		t.Fatalf("statuses: %q / %q", verified.Status, rework.Status)
	}
	assertNoPlan(rework, "rework")
	// The plan she was CHECKED against (1.9) wins over the sheet's 2.0; an item with no stored
	// check falls back to the sheet (0.5).
	busa, conc := verified.Items[0], verified.Items[1]
	if busa.FeedItemLabel != "Masur Busa" || busa.PlannedKg != "1.900" || busa.EnteredKg != "1.800" || busa.DifferenceKg != "-0.100" {
		t.Fatalf("verified Masur Busa: %+v", busa)
	}
	if conc.FeedItemLabel != "Concentrate" || conc.PlannedKg != "0.500" || conc.EnteredKg != "0.500" || conc.DifferenceKg != "0.000" {
		t.Fatalf("verified Concentrate: %+v", conc)
	}
	if verified.PlannedTotalKg != "2.400" || verified.EnteredTotalKg != "2.300" {
		t.Fatalf("verified bag totals: planned %q entered %q", verified.PlannedTotalKg, verified.EnteredTotalKg)
	}
	tot := got.Totals
	if tot.Bags != 2 || tot.Verified != 1 || tot.Rework != 1 || tot.AwaitingVerification != 0 || tot.NotPacked != 0 {
		t.Fatalf("status buckets: %+v", tot)
	}
	if tot.PlannedKg != "2.400" || tot.EnteredKg != "2.300" {
		t.Fatalf("day totals must cover the verified bag only: %+v", tot)
	}

	// Park scope binds both sides: own park returns the same bags, a foreign park none.
	if own := read([]uuid.UUID{uuid.MustParse(fdiPark)}); len(own.Bags) != 2 || own.Totals.EnteredKg != "2.300" {
		t.Fatalf("own-park scope: %+v", own)
	}
	if foreign := read([]uuid.UUID{uuid.New()}); len(foreign.Bags) != 0 {
		t.Fatalf("foreign-park scope: want no bags, got %+v", foreign.Bags)
	}
}

func floatPtr(v float64) *float64 { return &v }
