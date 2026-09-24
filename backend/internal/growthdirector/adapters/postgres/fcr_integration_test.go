package postgres

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// FCR integration coverage. Two pens, one per weighing arm, each with the exact shape the live
// estate has and each asserted on the NUMBER the tab renders -- not on field presence.
//
//   - A WHOLE-SHED pen ("Lump 1", undivided): two rounds a week apart, 25 animals, average 20.0 ->
//     20.7 kg (100 g/day). The sheet directs 10 kg/day for the 7 days between, head count 25, one
//     cell blocked. 175 head-days x 100 g = 17.5 kg gain; 70 kg feed; FCR 4.0; at ₹20/kg feed
//     and ₹425/kg sale the pen is worth ₹7,437.50 of gain against ₹1,400 of feed.
//   - A SCANNED pen reached THROUGH THE BRIDGE: the bucket names the legacy alias location
//     "Fcr Shed - Part 2" with a blank label, while the feed sheet keys on the physical shed
//     "Fcr Shed" + partition "Part 2". Two kids weighed in both rounds (200 and 100 g/day), a
//     third weighed once. Mean 150 g/day x 14 head-days (2 heads on the sheet) = 2.1 kg gain over
//     7 kg feed: FCR 3.333. The once-weighed kid must not pair.
const (
	fcrShedPhys  = "22222222-0000-4000-8000-000000000101" // "Fcr Shed", physical
	fcrShedAlias = "22222222-0000-4000-8000-000000000102" // "Fcr Shed - Part 2", legacy alias
	fcrBucketW1L = "22222222-0000-4000-8000-000000000301"
	fcrBucketW1S = "22222222-0000-4000-8000-000000000302"
	fcrBucketW2S = "22222222-0000-4000-8000-000000000303"
	fcrIssueBase = "22222222-0000-4000-8000-0000000005"
)

func fcrNear(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: nil, want %.3f", name, want)
	}
	if math.Abs(*got-want) > 0.005 {
		t.Fatalf("%s: got %.4f, want %.4f", name, *got, want)
	}
}

func seedFCRFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	seedGrowthDirectorFixture(t, ctx, pool)

	// The bridge's two shapes: a physical shed and a legacy alias row named after one of its pens.
	for _, loc := range [][2]string{{fcrShedPhys, "Fcr Shed"}, {fcrShedAlias, "Fcr Shed - Part 2"}} {
		execGD(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', $3, $4::uuid, 'active', 600)
ON CONFLICT (location_id) DO NOTHING`, loc[0], gdTenant, loc[1], gdPark)
	}
	execGD(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES
  ($1::uuid, $4::uuid, $6::uuid, $7::uuid, 'shed', 'Lump 1',            'per_shed_partition', $9::uuid, 0),
  ($2::uuid, $4::uuid, $6::uuid, $8::uuid, 'shed', 'Fcr Shed - Part 2', 'individual_animal',  $9::uuid, 0),
  ($3::uuid, $5::uuid, $6::uuid, $8::uuid, 'shed', 'Fcr Shed - Part 2', 'individual_animal',  $9::uuid, 0)`,
		fcrBucketW1L, fcrBucketW1S, fcrBucketW2S, gdCampaignW1, gdCampaignW2, gdTenant, gdShedLump, fcrShedAlias, gdOperator)

	// Whole-shed rounds: W1 on Jul 8, W2 on Jul 15 (the fixture's W2 lump bucket).
	execGD(t, ctx, pool, `
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, accepted_at)
VALUES
  ($1::uuid, $2::uuid, $3::uuid, 500.0,  20.0, 25, $6::uuid, $7::uuid, 'fcr:lump:w1', $8::timestamptz),
  ($1::uuid, $4::uuid, $5::uuid, 517.5,  20.7, 25, $6::uuid, $7::uuid, 'fcr:lump:w2', $9::timestamptz)`,
		gdTenant, gdCampaignW1, fcrBucketW1L, gdCampaignW2, gdBucketW2L, gdProof, gdOperator, day(8, 10), day(15, 10))

	// 25 live goats in the lump pen, one breed, one sex, all goats, none bought.
	execGD(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id, species)
SELECT gen_random_uuid(), $1::uuid, 'G-88' || lpad(i::text, 4, '0'), 'Beetal', 'male', 'kid', 'alive', 'kid', $2::uuid, $3::uuid, $4::uuid, $3::uuid, 'goat'
FROM generate_series(1, 25) AS i`, gdTenant, gdParty, gdShedLump, gdPark)

	// Scanned pen: three kids in the physical shed's pen "Part 2"; two weighed both rounds.
	type kid struct {
		goatID, seq, tag string
		first, last      float64
		once             bool
	}
	for _, k := range []kid{
		{"33333333-0000-4000-8000-000000000001", "0001", "FCR-A", 10.0, 11.4, false},
		{"33333333-0000-4000-8000-000000000002", "0002", "FCR-B", 10.0, 10.7, false},
		{"33333333-0000-4000-8000-000000000003", "0003", "FCR-C", 0, 12.0, true},
	} {
		seedGoatWithTag(t, ctx, pool, k.goatID, k.seq, k.tag, "Sirohi", "female")
		execGD(t, ctx, pool, `UPDATE goats SET shed_id = $2::uuid, current_location_id = $2::uuid WHERE goat_id = $1::uuid`, k.goatID, fcrShedPhys)
		execGD(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Part 2', 'Fcr Shed')`, gdTenant, k.goatID, fcrShedPhys)
		if !k.once {
			seedScanInCampaign(t, ctx, pool, gdCampaignW1, fcrBucketW1S, k.tag, k.first, day(8, 9), "verified")
		}
		seedScanInCampaign(t, ctx, pool, gdCampaignW2, fcrBucketW2S, k.tag, k.last, day(15, 9), "verified")
	}

	// Seven feed days Jul 8..14 for both pens. The lump pen gets 10 kg Maize a day for 25 heads,
	// plus a BLOCKED Bran cell on Jul 9. The scanned pen gets 1 kg a day for 2 heads, keyed on the
	// PHYSICAL shed + "Part 2".
	for i := 0; i < 7; i++ {
		feedDay := time.Date(2026, 7, 8+i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		issueID := fcrIssueBase + string(rune('0'+i)) + "1"
		execGD(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow, state, issued_at, generation_input_fingerprint, idempotency_key, request_fingerprint, source_contract, source_contract_version, generated_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, 'normal', 'issued', now(), 'fp:' || $4, 'idem:fcr:' || $4, 'fp:' || $4, 'growthdirector-test', '1', 'test')`,
			issueID, gdTenant, gdPark, feedDay)
		execGD(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label, shed_id, shed_label, partition_label, shed_tag, breed, session_no, head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg, overdue_pending, row_seq, item_seq)
VALUES
  ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Lump 1',   NULL,     '', '', 1, 25, false, 'normal', 'Maize Crush', 10.0, 10.0, false, 0, 1),
  ($1::uuid, $2::uuid, $3::uuid, 'CBE', $5::uuid, 'Fcr Shed', 'Part 2', '', '', 1, 2,  false, 'normal', 'Maize Crush', 1.0,  1.0,  false, 1, 1)`,
			gdTenant, issueID, gdPark, gdShedLump, fcrShedPhys)
		if i == 1 {
			execGD(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label, shed_id, shed_label, partition_label, shed_tag, breed, session_no, head_count, head_count_informational, workflow, feed_item_label, quantity_kg, blocked_reason_code, blocked_reason_detail, session_total_kg, overdue_pending, row_seq, item_seq)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Lump 1', NULL, '', '', 1, 25, false, 'normal', 'Bran', NULL, 'no_ration', 'test', 0, false, 0, 2)`,
				gdTenant, issueID, gdPark, gdShedLump)
		}
	}
	// Feed price: same-farm latest load on or before the day, ₹20/kg.
	execGD(t, ctx, pool, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg, total_cost, per_kg_cost, depletes_from)
VALUES ($1::uuid, $2::uuid, 'CBE', 'Maize Crush', 1, '2026-07-01', 1000, 20000, 20, '2026-07-01')`, gdTenant, gdPark)
	// Sale price: the newest row on or before the period end wins; an older, higher goat row must lose.
	execGD(t, ctx, pool, `
INSERT INTO growth_sale_price_assumptions (tenant_id, species, price_per_kg_inr, effective_from, set_by)
VALUES ($1::uuid, 'goat', 500, '2026-06-01', 'old'), ($1::uuid, 'goat', 425, '2026-07-01', 'maintainer'), ($1::uuid, 'sheep', 430, '2026-07-01', 'maintainer')
ON CONFLICT (tenant_id, species, management_stage, sex, effective_from) DO UPDATE SET price_per_kg_inr = EXCLUDED.price_per_kg_inr, set_by = EXCLUDED.set_by`, gdTenant)
}

func TestFCRLumpAndScannedPensThroughTheBridge(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFCRFixture(t, ctx, pool)

	repo := NewRepository(pool, 30*time.Second)
	got, err := repo.GetFCR(ctx, gdTenant, []string{gdPark},
		time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "", "", "")
	if err != nil {
		t.Fatalf("GetFCR: %v", err)
	}
	if got.Basis != domain.FCRBasisDirectedFeed {
		t.Fatalf("basis = %q", got.Basis)
	}
	price, ok := domain.SalePrices{Prices: got.SalePrices}.PriceFor("goat")
	if !ok || price != 425 {
		t.Fatalf("goat price = %v %v, want the 2026-07-01 row (425), not the older 500", price, ok)
	}

	byDisplay := map[string]domain.FCRPen{}
	for _, pen := range got.Pens {
		byDisplay[pen.OperationalLocationDisplay] = pen
	}
	lump, ok := byDisplay["CBE · Lump 1"]
	if !ok {
		t.Fatalf("no lump pen row; got %v", keysOf(byDisplay))
	}
	fcrNear(t, "lump fcr", lump.FCR, 4.0)
	fcrNear(t, "lump gain", lump.GainKg, 17.5)
	fcrNear(t, "lump feed", lump.FeedKg, 70)
	fcrNear(t, "lump head-days", lump.HeadDays, 175)
	fcrNear(t, "lump adg", lump.ADGGPerDay, 100)
	fcrNear(t, "lump feed cost", lump.FeedCostINR, 1400)
	fcrNear(t, "lump gain value", lump.GainValueINR, 17.5*425)
	if lump.BlockedCells != 1 || lump.Status != domain.FCRPenOK || lump.Animals != 25 {
		t.Fatalf("lump: blocked=%d status=%s animals=%d", lump.BlockedCells, lump.Status, lump.Animals)
	}
	if lump.Breed != "Beetal" || lump.Sex != "male" || lump.Species != "goat" || lump.Origin != domain.OriginFarmBorn || lump.WeightBand != "20-25" {
		t.Fatalf("lump cohort = breed %s sex %s species %s origin %s band %s", lump.Breed, lump.Sex, lump.Species, lump.Origin, lump.WeightBand)
	}

	// THE BRIDGE: the bucket named the alias location; the row must be keyed on the PHYSICAL shed
	// with the sheet's partition label, and must have found the feed rows written there.
	scan, ok := byDisplay["CBE · Fcr Shed - Part 2"]
	if !ok {
		t.Fatalf("no scanned pen row through the bridge; got %v", keysOf(byDisplay))
	}
	if scan.LocationID != fcrShedPhys || scan.PartitionLabel != "Part 2" {
		t.Fatalf("scanned pen keyed on %s/%q, want the physical shed + 'Part 2'", scan.LocationID, scan.PartitionLabel)
	}
	fcrNear(t, "scan adg (mean of the two paired kids)", scan.ADGGPerDay, 150)
	fcrNear(t, "scan gain", scan.GainKg, 2.1)
	fcrNear(t, "scan feed", scan.FeedKg, 7)
	fcrNear(t, "scan fcr", scan.FCR, 7.0/2.1)
	if scan.Animals != 3 || scan.Rounds != 2 || scan.Breed != "Sirohi" || scan.Sex != "female" || scan.WeightBand != "<15" {
		t.Fatalf("scan pen = animals %d rounds %d breed %s sex %s band %s", scan.Animals, scan.Rounds, scan.Breed, scan.Sex, scan.WeightBand)
	}

	// Farm totals are sum-over-sum across both pens: (70+7)/(17.5+2.1).
	fcrNear(t, "farm fcr", got.Summary.FCR, 77/19.6)
	if got.Summary.PensWithFCR != 2 {
		t.Fatalf("pens with fcr = %d (%+v)", got.Summary.PensWithFCR, got.Summary)
	}
	// Park, then pen A to Z (maintainer decision 2026-09-16) -- never worst first: the bars carry the
	// ratio, the order is where a reader looks a pen up.
	if got.Pens[0].OperationalLocationDisplay != "CBE · Fcr Shed - Part 2" || got.Pens[1].OperationalLocationDisplay != "CBE · Lump 1" {
		t.Fatalf("pens A to Z, got %s, %s", got.Pens[0].OperationalLocationDisplay, got.Pens[1].OperationalLocationDisplay)
	}
	// Weekly: both segments closed in the week of Jul 13.
	if len(got.Weekly) != 1 || got.Weekly[0].WeekStart != "2026-07-13" || got.Weekly[0].Pens != 2 {
		t.Fatalf("weekly = %+v", got.Weekly)
	}
	// Sex filter at pen grain: only the all-male lump pen survives.
	male, err := repo.GetFCR(ctx, gdTenant, []string{gdPark},
		time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "male", "", "")
	if err != nil {
		t.Fatalf("GetFCR male: %v", err)
	}
	if len(male.Pens) != 1 || male.Pens[0].OperationalLocationDisplay != "CBE · Lump 1" {
		t.Fatalf("male filter pens = %+v", male.Pens)
	}
	// Weighing-mode filter narrows the rounds themselves.
	scannedOnly, err := repo.GetFCR(ctx, gdTenant, []string{gdPark},
		time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "", "", "individual_animal")
	if err != nil {
		t.Fatalf("GetFCR scanned: %v", err)
	}
	for _, pen := range scannedOnly.Pens {
		if pen.OperationalLocationDisplay == "CBE · Lump 1" {
			t.Fatalf("mode filter leaked the lump pen: %+v", scannedOnly.Pens)
		}
	}
}

// A pen weighed once carries its window feed and no ratio; the summary counts it as coverage lost,
// never as a zero FCR.
func TestFCRPenWeighedOnceReportsAbsence(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFCRFixture(t, ctx, pool)

	repo := NewRepository(pool, 30*time.Second)
	// Window holding only W2: every pen has one round.
	got, err := repo.GetFCR(ctx, gdTenant, []string{gdPark},
		time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "", "", "")
	if err != nil {
		t.Fatalf("GetFCR: %v", err)
	}
	if got.Summary.PensWithFCR != 0 || got.Summary.FCR != nil || got.Summary.PensWeighedOnce != len(got.Pens) || len(got.Pens) == 0 {
		t.Fatalf("summary = %+v pens = %d", got.Summary, len(got.Pens))
	}
	for _, pen := range got.Pens {
		if pen.Status != domain.FCRPenWeighedOnce || pen.FCR != nil {
			t.Fatalf("pen %s = %+v", pen.OperationalLocationDisplay, pen)
		}
	}
}

// A window that ends BEFORE the first price row was set is still valued, at the earliest row: the
// assumption is what the reader holds today, and the tab prints which row applied.
func TestFCRDateShiftSalePriceFallsBackToTheEarliestRowForAnOlderWindow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFCRFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)

	early, err := repo.GetSalePrices(ctx, gdTenant, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetSalePrices: %v", err)
	}
	goat, ok := early.PriceFor("goat")
	if !ok || goat != 500 {
		t.Fatalf("before any row was effective the EARLIEST goat row (500, 2026-06-01) must apply, got %v %v", goat, ok)
	}
	later, err := repo.GetSalePrices(ctx, gdTenant, time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetSalePrices: %v", err)
	}
	if goat, _ := later.PriceFor("goat"); goat != 425 {
		t.Fatalf("once a row is effective the newest effective row applies, got %v", goat)
	}
}

// The price is attached to each feed row BEFORE the segment join (the 2026-09-24 burst fix), so
// this pins that the reorder neither multiplies a feed row nor lets a price cross parks:
//
//   - ONE-TO-MANY loads: the same feed item gets two more loads on Jul 11 (batch 2 at ₹30, batch 3
//     at ₹32). A join that matched every load on or before the day would count a Jul 11..14 feed
//     row two or three times; the rule is ONE price per (park, item, day) -- the latest load, the
//     higher batch on a tie -- so feed kg must stay exactly 70 / 7 and only the price moves.
//   - PARK SCOPE: a cheaper-looking ₹99 load of the same item for ANOTHER park on Jul 5 must never
//     price this park's feed.
//
// Expected: lump 10 kg/day -> Jul 8..10 at ₹20 (600) + Jul 11..14 at ₹32 (1,280) = ₹1,880;
// scanned pen 1 kg/day -> 60 + 128 = ₹188.
func TestFCRFeedCostOneToManyLoadsKeepOnePricePerFeedRowWithinParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFCRFixture(t, ctx, pool)
	const otherPark = "22222222-0000-4000-8000-000000003099"
	execGD(t, ctx, pool, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg, total_cost, per_kg_cost, depletes_from)
VALUES
  ($1::uuid, $2::uuid, 'CBE', 'Maize Crush', 2, '2026-07-11', 1000, 30000, 30, '2026-07-11'),
  ($1::uuid, $2::uuid, 'CBE', 'Maize Crush', 3, '2026-07-11', 1000, 32000, 32, '2026-07-11'),
  ($1::uuid, $3::uuid, 'CPT', 'Maize Crush', 1, '2026-07-05', 1000, 99000, 99, '2026-07-05')`,
		gdTenant, gdPark, otherPark)

	repo := NewRepository(pool, 30*time.Second)
	got, err := repo.GetFCR(ctx, gdTenant, []string{gdPark},
		time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), "", "", "")
	if err != nil {
		t.Fatalf("GetFCR: %v", err)
	}
	byDisplay := map[string]domain.FCRPen{}
	for _, pen := range got.Pens {
		byDisplay[pen.OperationalLocationDisplay] = pen
	}
	lump, ok := byDisplay["Coimbatore · Lump 1"]
	if !ok {
		t.Fatalf("no lump pen row; got %v", keysOf(byDisplay))
	}
	fcrNear(t, "lump feed kg is not multiplied by the extra loads", lump.FeedKg, 70)
	fcrNear(t, "lump feed cost at one price per row", lump.FeedCostINR, 1880)
	if lump.BlockedCells != 1 {
		t.Fatalf("lump blocked cells = %d, want 1 (the join must not multiply the blocked row either)", lump.BlockedCells)
	}
	scan, ok := byDisplay["Coimbatore · Fcr Shed - Part 2"]
	if !ok {
		t.Fatalf("no scanned pen row; got %v", keysOf(byDisplay))
	}
	fcrNear(t, "scan feed kg is not multiplied by the extra loads", scan.FeedKg, 7)
	fcrNear(t, "scan feed cost at one price per row", scan.FeedCostINR, 188)
}

func keysOf(m map[string]domain.FCRPen) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
