package postgres

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/economics/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Adversarial coverage for the Business Economics read. Every test seeds real
// rows and asserts the numbers the screen renders, because each of these is a
// way the queries can be quietly wrong while still returning plausible data.

const (
	// Baseline-seeded rows (migration 000001).
	ecTenant   = "00000000-0000-4000-8000-000000000001"
	ecParty    = "00000000-0000-4000-8000-000000001001"
	ecPark     = "00000000-0000-4000-8000-000000003001"
	ecShed     = "f1b1bad0-47ab-4248-95dc-8fa1472d4fec" // Gandhi 1 - Part 1 (baseline)
	ecOperator = "00000000-0000-4000-8000-000000000301"

	// Fixture-created rows.
	ecPark2      = "22222222-0000-4000-8000-000000000001"
	ecShed2      = "22222222-0000-4000-8000-000000000002"
	ecCampaignW1 = "22222222-0000-4000-8000-000000000201"
	ecCampaignW2 = "22222222-0000-4000-8000-000000000202"
	ecCampaignP2 = "22222222-0000-4000-8000-000000000203"
	ecBucketW1   = "22222222-0000-4000-8000-000000000301"
	ecBucketW2   = "22222222-0000-4000-8000-000000000302"
	ecBucketP2   = "22222222-0000-4000-8000-000000000303"
	ecProof      = "22222222-0000-4000-8000-000000000401"
	ecFeedIssue  = "22222222-0000-4000-8000-000000000501"
	ecOperator2  = "22222222-0000-4000-8000-000000000101"
	ecDealClosed = "22222222-0000-4000-8000-000000000601"
	ecDealOpen   = "22222222-0000-4000-8000-000000000602"
)

func ecWindow() (time.Time, time.Time) {
	// Half-open [Jul 1, Aug 1): wide enough that window membership is never the
	// thing under test.
	return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
}

func ecDay(d, hour int) time.Time {
	return time.Date(2026, 7, d, hour, 0, 0, 0, time.UTC)
}

func execEC(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed sql failed: %v\n%s", err, sql)
	}
}

// seedEconomicsFixture lays the shared stage: a second park + shed, two
// week-grain campaigns on the first park and one on the second, their buckets,
// the shared proof artifact, and one closed weighed deal (₹40,000 over 100 kg
// = ₹400/kg realized) inside the window.
func seedEconomicsFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execEC(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status, display_order)
VALUES ($1::uuid, $2::uuid, 'park', 'Testpark', 'active', 900)
ON CONFLICT (location_id) DO NOTHING`, ecPark2, ecTenant)
	execEC(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, $2::uuid, 'shed', 'Testshed', $3::uuid, 'active', 901)
ON CONFLICT (location_id) DO NOTHING`, ecShed2, ecTenant, ecPark2)
	// The weighing schema enforces operator↔park scope on campaign sheds, so the
	// fixture grants each operator its own park before seeding buckets.
	execEC(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now()),
       ($1::uuid, $4::uuid, 'operator', 'park', $5::uuid, 'active', now())
ON CONFLICT DO NOTHING`, ecTenant, ecOperator, ecPark, ecOperator2, ecPark2)
	execEC(t, ctx, pool, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES
  ($1::uuid, $4::uuid, $5::uuid, '2026-07-06', '2026-07-12', '2026-07-06', 'completed', 100, $7::uuid, $7::uuid),
  ($2::uuid, $4::uuid, $5::uuid, '2026-07-13', '2026-07-19', '2026-07-13', 'published', 100, $7::uuid, $7::uuid),
  ($3::uuid, $4::uuid, $6::uuid, '2026-07-06', '2026-07-19', '2026-07-06', 'published', 100, $8::uuid, $8::uuid)`,
		ecCampaignW1, ecCampaignW2, ecCampaignP2, ecTenant, ecPark, ecPark2, ecOperator, ecOperator2)
	execEC(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES
  ($1::uuid, $4::uuid, $6::uuid, $7::uuid, 'shed', 'Gandhi 1 - Part 1', 'individual_animal', $9::uuid, 0),
  ($2::uuid, $5::uuid, $6::uuid, $7::uuid, 'shed', 'Gandhi 1 - Part 1', 'individual_animal', $9::uuid, 0),
  ($3::uuid, $10::uuid, $6::uuid, $8::uuid, 'shed', 'Testshed', 'individual_animal', $11::uuid, 0)`,
		ecBucketW1, ecBucketW2, ecBucketP2, ecCampaignW1, ecCampaignW2, ecTenant, ecShed, ecShed2, ecOperator, ecCampaignP2, ecOperator2)
	execEC(t, ctx, pool, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at)
VALUES ($1::uuid, $2::uuid, 'local', 'economics-test/' || $1, 'video/mp4', 'completed', 'shed', $3::uuid, 'shed', $3::uuid, 'video', $4::uuid, now())`,
		ecProof, ecTenant, ecShed, ecOperator)
	// The realized price the whole page prices gain at: one CLOSED weighed deal.
	execEC(t, ctx, pool, `
INSERT INTO sales_deals (id, tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, total_weight_kg, sales_value, status)
VALUES ($1::uuid, $2::uuid, '2026-07-20', 'CBE', 'Test Buyer', 'Sheep', 'Sojat', 2, 100, 40000, 'Deal Closed')`,
		ecDealClosed, ecTenant)
}

func seedEcScan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaign, bucket, tag string, weightKg float64, at time.Time, verification string) {
	t.Helper()
	execEC(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at, verification_status)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8, $9::timestamptz, $9::timestamptz, $10)`,
		ecTenant, campaign, bucket, tag, weightKg, ecProof, ecOperator,
		fmt.Sprintf("ec:%s:%s:%d", bucket, tag, at.UnixNano()), at, verification)
}

// seedEcGoat creates a live herd animal (with stage, breed, park, shed and pen)
// and its lifetime-unique tag mapping.
func seedEcGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, displaySeq, tag, breed, sex, stage, parkID, shedID, pen string) {
	t.Helper()
	execEC(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, 'adult', 'alive', $6, $7::uuid, $8::uuid, $9::uuid, $8::uuid)`,
		goatID, ecTenant, "G-98"+displaySeq, breed, sex, stage, ecParty, shedID, parkID)
	execEC(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, upper(btrim($3)), 'tenant', true, 'active', now(), 'test')`,
		ecTenant, goatID, tag)
	if pen != "" {
		execEC(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'Gandhi 1 - ' || $4)
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
			ecTenant, goatID, shedID, pen)
	}
}

func seedEcFeedRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, issueID, shedID, pen, shedTag, breed, item string, quantity any, reasonCode any, heads int, informational bool, workflow string, itemSeq int) {
	t.Helper()
	execEC(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label, shed_id, shed_label, partition_label, shed_tag, breed, session_no, head_count, head_count_informational, workflow, feed_item_label, quantity_kg, blocked_reason_code, blocked_reason_detail, session_total_kg, overdue_pending, row_seq, item_seq)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Gandhi 1 - Part 1', $5, $6, $7, 1, $8, $9, $10, $11, $12, $13, $13, 0, false, 0, $14)`,
		ecTenant, issueID, ecPark, shedID, pen, shedTag, breed, heads, informational, workflow, item, quantity, reasonCode, itemSeq)
}

func seedEcFeedIssue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, issueID, feedDay, workflow string) {
	t.Helper()
	execEC(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow, state, issued_at, generation_input_fingerprint, idempotency_key, request_fingerprint, source_contract, source_contract_version, generated_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $5, 'issued', now(), 'fp:'||$1, 'idem:'||$1, 'fp:'||$1, 'economics-test', '1', 'test')`,
		issueID, ecTenant, ecPark, feedDay, workflow)
}

func seedEcPurchase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, item string, batch int, date string, quantityKg, totalCost float64) {
	t.Helper()
	execEC(t, ctx, pool, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg, total_cost, depletes_from)
VALUES ($1::uuid, $2::uuid, 'CBE', $3, $4, $5::date, $6, $7, $5::date)`,
		ecTenant, ecPark, item, batch, date, quantityKg, totalCost)
}

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// ONE-TO-MANY FEED CELLS. A mixed pen's sheet row carries '+'-joined composite
// keys, and the same animal can be a member of MORE than one of its pen's grain
// cells. The membership join must price the animal from those cells WITHOUT
// duplicating its row, and the latest-load pricing must pick the newest
// purchase on or before the feed day, never an older or later one.
func TestEconomicsOneToManyFeedCellsCollapsePerAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedEconomicsFixture(t, ctx, pool)

	// One paired animal: 14.0 → 15.4 over 7 days = 200 g/day.
	seedEcGoat(t, ctx, pool, "22222222-0000-4000-8000-000000000701", "0001", "EC-A", "Sojat", "female", "F2-Female", ecPark, ecShed, "Part 1")
	seedEcScan(t, ctx, pool, ecCampaignW1, ecBucketW1, "EC-A", 14.0, ecDay(8, 6), "pending")
	seedEcScan(t, ctx, pool, ecCampaignW2, ecBucketW2, "EC-A", 15.4, ecDay(15, 6), "pending")

	// Latest-load pricing: an old expensive load and a newer ₹10/kg load. The
	// newer one (still before the feed day) must win; a load AFTER the feed day
	// must be invisible.
	seedEcPurchase(t, ctx, pool, "Maize Crush", 1, "2026-06-01", 100, 9900) // ₹99/kg, superseded
	seedEcPurchase(t, ctx, pool, "Maize Crush", 2, "2026-07-01", 100, 1000) // ₹10/kg, the live rate
	seedEcPurchase(t, ctx, pool, "Maize Crush", 3, "2026-07-30", 100, 5000) // after the feed day: invisible

	// One feed day, TWO grain cells of the same pen that both cover this animal:
	// a composite-key mixed row (₹20 over 4 heads = ₹5/head) and a single-key row
	// (₹10 over 2 heads = ₹5/head). The animal must collapse to ONE row at ₹5.
	seedEcFeedIssue(t, ctx, pool, ecFeedIssue, "2026-07-15", "normal")
	seedEcFeedRow(t, ctx, pool, ecFeedIssue, ecShed, "Part 1", "F2-Female + F2-Male", "Sojat", "Maize Crush", 2.0, nil, 4, false, "normal", 1)
	seedEcFeedRow(t, ctx, pool, ecFeedIssue, ecShed, "Part 1", "F2-Female", "Sojat", "Maize Crush", 1.0, nil, 2, false, "normal", 2)

	from, to := ecWindow()
	repo := NewRepository(pool, 5*time.Second)
	out, err := repo.GetBusinessEconomics(ctx, ecTenant, []string{ecPark, ecPark2}, from, to)
	if err != nil {
		t.Fatalf("GetBusinessEconomics: %v", err)
	}

	if len(out.Animals) != 1 {
		t.Fatalf("one paired animal in two matching feed cells must stay ONE row, got %d", len(out.Animals))
	}
	row := out.Animals[0]
	if row.TagDisplay != "ec-a" || row.DisplayID != "G-980001" {
		t.Fatalf("row identity = %q/%q", row.TagDisplay, row.DisplayID)
	}
	if !almostEqual(row.ADGGPerDay, 200) {
		t.Fatalf("adg = %v, want 200 g/day", row.ADGGPerDay)
	}
	if row.FeedCostPerDayRupees == nil || !almostEqual(*row.FeedCostPerDayRupees, 5) {
		t.Fatalf("feed cost/day = %v, want ₹5 (both cells price at ₹5/head; a duplicate-row bug would double it)", row.FeedCostPerDayRupees)
	}
	if row.CostPerKgGainRupees == nil || !almostEqual(*row.CostPerKgGainRupees, 25) {
		t.Fatalf("cost/kg gain = %v, want ₹25 (₹5 over 0.2 kg)", row.CostPerKgGainRupees)
	}
	// Realized ₹400/kg from the fixture deal → value 0.2 × 400 = ₹80, net ₹75.
	if out.Pulse.RealizedPricePerKg == nil || !almostEqual(*out.Pulse.RealizedPricePerKg, 400) {
		t.Fatalf("realized price = %v, want 400", out.Pulse.RealizedPricePerKg)
	}
	if out.Pulse.PriceBasis != domain.PriceBasisWindow {
		t.Fatalf("price basis = %q", out.Pulse.PriceBasis)
	}
	if row.NetPerDayRupees == nil || !almostEqual(*row.NetPerDayRupees, 75) {
		t.Fatalf("net/day = %v, want ₹75", row.NetPerDayRupees)
	}
	if row.Signal != domain.SignalEarning {
		t.Fatalf("signal = %q, want earning", row.Signal)
	}
}

// PAGINATION CAP. The per-animal table is capped at MaxAnimalRows; the pulse
// denominators are whole-filter aggregates computed independently, so the cap
// must never bend them.
func TestEconomicsPaginationCapNeverBendsSummaries(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedEconomicsFixture(t, ctx, pool)

	total := domain.MaxAnimalRows + 5
	for i := 0; i < total; i++ {
		tag := fmt.Sprintf("EC-P%04d", i)
		goatID := fmt.Sprintf("22222222-0000-4000-8000-0000000%05d", 10000+i)
		seedEcGoat(t, ctx, pool, goatID, fmt.Sprintf("2%04d", i), tag, "Sojat", "male", "F2-Male", ecPark, ecShed, "")
		seedEcScan(t, ctx, pool, ecCampaignW1, ecBucketW1, tag, 20.0, ecDay(8, 6), "pending")
		seedEcScan(t, ctx, pool, ecCampaignW2, ecBucketW2, tag, 21.4, ecDay(15, 6), "pending")
	}

	from, to := ecWindow()
	repo := NewRepository(pool, 30*time.Second)
	out, err := repo.GetBusinessEconomics(ctx, ecTenant, []string{ecPark}, from, to)
	if err != nil {
		t.Fatalf("GetBusinessEconomics: %v", err)
	}
	if len(out.Animals) != domain.MaxAnimalRows {
		t.Fatalf("animal rows = %d, want the %d cap", len(out.Animals), domain.MaxAnimalRows)
	}
	if out.Pulse.PairedAnimals != total {
		t.Fatalf("pulse paired = %d, want the uncapped %d", out.Pulse.PairedAnimals, total)
	}
	if out.Pulse.WeighedIdentities != total {
		t.Fatalf("pulse weighed = %d, want %d", out.Pulse.WeighedIdentities, total)
	}
	// The band counts are whole-filter too: every animal (21.4 kg latest) sits
	// in 20-25.
	for _, band := range out.Bands {
		want := 0
		if band.Band == "20-25" {
			want = total
		}
		if band.Animals != want {
			t.Fatalf("band %s animals = %d, want %d", band.Band, band.Animals, want)
		}
	}
}

// PARK SCOPE. A park filter must narrow the animal and feed figures to that
// park — and must NOT narrow the deal figures, which are recorded against a
// farm label, not a park id, and are deliberately tenant-wide.
func TestEconomicsParkScopeNarrowsAnimalsNotDeals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedEconomicsFixture(t, ctx, pool)

	seedEcGoat(t, ctx, pool, "22222222-0000-4000-8000-000000000801", "0801", "EC-S1", "Sojat", "male", "F2-Male", ecPark, ecShed, "")
	seedEcScan(t, ctx, pool, ecCampaignW1, ecBucketW1, "EC-S1", 20.0, ecDay(8, 6), "pending")
	seedEcScan(t, ctx, pool, ecCampaignW2, ecBucketW2, "EC-S1", 21.4, ecDay(15, 6), "pending")

	seedEcGoat(t, ctx, pool, "22222222-0000-4000-8000-000000000802", "0802", "EC-S2", "Sojat", "male", "F2-Male", ecPark2, ecShed2, "")
	seedEcScan(t, ctx, pool, ecCampaignP2, ecBucketP2, "EC-S2", 30.0, ecDay(8, 6), "pending")
	seedEcScan(t, ctx, pool, ecCampaignP2, ecBucketP2, "EC-S2", 31.4, ecDay(15, 7), "pending")

	from, to := ecWindow()
	repo := NewRepository(pool, 5*time.Second)

	both, err := repo.GetBusinessEconomics(ctx, ecTenant, []string{ecPark, ecPark2}, from, to)
	if err != nil {
		t.Fatalf("both parks: %v", err)
	}
	// EC-S2's two scans are one campaign (one round): not paired. Both-parks
	// scope still counts its identity in the weighed denominator.
	if both.Pulse.WeighedIdentities != 2 || both.Pulse.PairedAnimals != 1 {
		t.Fatalf("both parks weighed/paired = %d/%d, want 2/1", both.Pulse.WeighedIdentities, both.Pulse.PairedAnimals)
	}

	one, err := repo.GetBusinessEconomics(ctx, ecTenant, []string{ecPark}, from, to)
	if err != nil {
		t.Fatalf("one park: %v", err)
	}
	if one.Pulse.WeighedIdentities != 1 || len(one.Animals) != 1 || one.Animals[0].TagDisplay != "ec-s1" {
		t.Fatalf("park filter must keep only the first park's animal, got weighed=%d animals=%+v", one.Pulse.WeighedIdentities, one.Animals)
	}
	if len(one.Parks) != 1 || one.Parks[0].ParkID != ecPark {
		t.Fatalf("park vocabulary must carry exactly the scoped park, got %+v", one.Parks)
	}
	// Deal figures are tenant-wide on BOTH scopes.
	for name, out := range map[string]domain.BusinessEconomics{"both": both, "one": one} {
		if out.Pulse.ClosedDeals != 1 || !almostEqual(out.Pulse.SoldRevenueRupees, 40000) {
			t.Fatalf("%s: deal figures must be tenant-wide, got deals=%d revenue=%v", name, out.Pulse.ClosedDeals, out.Pulse.SoldRevenueRupees)
		}
		if out.Pulse.RealizedPricePerKg == nil || !almostEqual(*out.Pulse.RealizedPricePerKg, 400) {
			t.Fatalf("%s: realized price = %v, want 400", name, out.Pulse.RealizedPricePerKg)
		}
	}
}

// STATUS MATRIX. The read must honour every status boundary at once: a rework
// weigh never makes a pair, a non-closed deal never prices anything, a blocked
// feed cell (NULL) never becomes zero, and an unpriced feed item is disclosed
// instead of invented.
func TestEconomicsStatusMatrixReworkDealsAndBlockedFeed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedEconomicsFixture(t, ctx, pool)

	// EC-R's second round is rework: one trusted round only -> not paired.
	seedEcGoat(t, ctx, pool, "22222222-0000-4000-8000-000000000901", "0901", "EC-R", "Sojat", "male", "F2-Male", ecPark, ecShed, "Part 1")
	seedEcScan(t, ctx, pool, ecCampaignW1, ecBucketW1, "EC-R", 20.0, ecDay(8, 6), "pending")
	seedEcScan(t, ctx, pool, ecCampaignW2, ecBucketW2, "EC-R", 24.0, ecDay(15, 6), "rework")

	// EC-K pairs cleanly (20.0 -> 21.4 is a 7% change, clear of the noise floor)
	// and sits in a pen whose ONLY cells are a blocked row and an unpriced item:
	// cost must stay NULL (blocked is not zero, unpriced is not free) and the
	// item must be disclosed.
	seedEcGoat(t, ctx, pool, "22222222-0000-4000-8000-000000000902", "0902", "EC-K", "Sojat", "female", "F2-Female", ecPark, ecShed, "Part 2")
	seedEcScan(t, ctx, pool, ecCampaignW1, ecBucketW1, "EC-K", 20.0, ecDay(8, 6), "pending")
	seedEcScan(t, ctx, pool, ecCampaignW2, ecBucketW2, "EC-K", 21.4, ecDay(15, 6), "pending")
	seedEcFeedIssue(t, ctx, pool, ecFeedIssue, "2026-07-15", "normal")
	seedEcFeedRow(t, ctx, pool, ecFeedIssue, ecShed, "Part 2", "F2-Female", "Sojat", "Mineral Mix", nil, "no_ration_rate", 3, false, "normal", 1)
	seedEcFeedRow(t, ctx, pool, ecFeedIssue, ecShed, "Part 2", "F2-Female", "Sojat", "Mystery Bran", 2.0, nil, 3, false, "normal", 2)

	// An open deal must price nothing.
	execEC(t, ctx, pool, `
INSERT INTO sales_deals (id, tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, total_weight_kg, sales_value, status)
VALUES ($1::uuid, $2::uuid, '2026-07-21', 'CBE', 'Open Buyer', 'Sheep', 'Sojat', 3, 100, 99999, 'In Discussion')`,
		ecDealOpen, ecTenant)

	from, to := ecWindow()
	repo := NewRepository(pool, 5*time.Second)
	out, err := repo.GetBusinessEconomics(ctx, ecTenant, []string{ecPark}, from, to)
	if err != nil {
		t.Fatalf("GetBusinessEconomics: %v", err)
	}

	if len(out.Animals) != 1 || out.Animals[0].TagDisplay != "ec-k" {
		t.Fatalf("animals = %+v, want exactly ec-k (rework leaves EC-R unpaired)", out.Animals)
	}
	row := out.Animals[0]
	if row.FeedCostPerDayRupees != nil {
		t.Fatalf("blocked + unpriced cells must leave cost NULL, got %v", *row.FeedCostPerDayRupees)
	}
	if row.Signal != domain.SignalWatch {
		t.Fatalf("signal = %q, want watch (no cost side)", row.Signal)
	}
	if out.Pulse.UnpricedFeedItems != 1 {
		t.Fatalf("unpriced feed items = %d, want 1 (Mystery Bran)", out.Pulse.UnpricedFeedItems)
	}
	if out.Pulse.FeedCostPerDayRupees != nil {
		t.Fatalf("burn/day must be NULL when no directed kg can be priced, got %v", *out.Pulse.FeedCostPerDayRupees)
	}

	// Deals: the open deal changes neither the count nor the price.
	if out.Pulse.ClosedDeals != 1 || !almostEqual(out.Pulse.SoldRevenueRupees, 40000) {
		t.Fatalf("closed deals/revenue = %d/%v, want 1/40000", out.Pulse.ClosedDeals, out.Pulse.SoldRevenueRupees)
	}
	if out.Pulse.RealizedPricePerKg == nil || !almostEqual(*out.Pulse.RealizedPricePerKg, 400) {
		t.Fatalf("realized price = %v, want 400 (the In-Discussion deal must not price anything)", out.Pulse.RealizedPricePerKg)
	}
}

// SCALE-NOISE FLOOR. This module DIVIDES BY the gain, so a sub-noise weight
// change would turn scale drift into a confident rupee figure: on the live herd
// 21% of pairs sit inside the 3% band and rendered "cost per kg" in the
// thousands. A flat animal keeps its row and its real feed cost, but must carry
// NO cost-per-kg, NO value-added and the Watch verdict.
//
// Mutation test: delete the CASE in adgCTE and EC-FLAT starts reporting a
// four-figure cost per kg, turning this red.
func TestEconomicsSubNoiseGainScoresFlatAndIsNeverPriced(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedEconomicsFixture(t, ctx, pool)

	// EC-FLAT: 20.00 -> 20.40 over 7 days. That is +2% of body weight — inside
	// the 3% gut-fill band — so it is NOT growth, even though the arithmetic
	// would happily say 57 g/day and (at a real ration) hundreds of rupees a kg.
	seedEcGoat(t, ctx, pool, "22222222-0000-4000-8000-000000000a11", "1011", "EC-FLAT", "Sojat", "male", "F2-Male", ecPark, ecShed, "Part 1")
	seedEcScan(t, ctx, pool, ecCampaignW1, ecBucketW1, "EC-FLAT", 20.0, ecDay(8, 6), "pending")
	seedEcScan(t, ctx, pool, ecCampaignW2, ecBucketW2, "EC-FLAT", 20.4, ecDay(15, 6), "pending")

	// EC-REAL: 20.0 -> 21.4 over 7 days = +7%, real growth at 200 g/day.
	seedEcGoat(t, ctx, pool, "22222222-0000-4000-8000-000000000a12", "1012", "EC-REAL", "Sojat", "male", "F2-Male", ecPark, ecShed, "Part 1")
	seedEcScan(t, ctx, pool, ecCampaignW1, ecBucketW1, "EC-REAL", 20.0, ecDay(8, 6), "pending")
	seedEcScan(t, ctx, pool, ecCampaignW2, ecBucketW2, "EC-REAL", 21.4, ecDay(15, 6), "pending")

	// Both animals share one priced ration cell: ₹10/kg × 2 kg over 4 heads = ₹5/head/day.
	seedEcPurchase(t, ctx, pool, "Maize Crush", 1, "2026-07-01", 100, 1000)
	seedEcFeedIssue(t, ctx, pool, ecFeedIssue, "2026-07-15", "normal")
	seedEcFeedRow(t, ctx, pool, ecFeedIssue, ecShed, "Part 1", "F2-Male", "Sojat", "Maize Crush", 2.0, nil, 4, false, "normal", 1)

	from, to := ecWindow()
	repo := NewRepository(pool, 5*time.Second)
	out, err := repo.GetBusinessEconomics(ctx, ecTenant, []string{ecPark}, from, to)
	if err != nil {
		t.Fatalf("GetBusinessEconomics: %v", err)
	}

	byTag := map[string]domain.AnimalEconomics{}
	for _, row := range out.Animals {
		byTag[row.TagDisplay] = row
	}
	flat, ok := byTag["ec-flat"]
	if !ok {
		t.Fatal("a flat animal must KEEP its row — its feed cost is real and worth seeing")
	}
	if flat.ADGGPerDay != 0 {
		t.Fatalf("sub-noise change must score 0 g/day, got %v", flat.ADGGPerDay)
	}
	if flat.CostPerKgGainRupees != nil {
		t.Fatalf("a gain that was not measured cannot be priced: cost/kg = %v", *flat.CostPerKgGainRupees)
	}
	if flat.ValueAddedPerDayRupees != nil || flat.NetPerDayRupees != nil {
		t.Fatalf("flat animal must carry no value/net, got %v/%v", flat.ValueAddedPerDayRupees, flat.NetPerDayRupees)
	}
	if flat.Signal != domain.SignalWatch {
		t.Fatalf("flat animal signal = %q, want watch", flat.Signal)
	}
	if flat.FeedCostPerDayRupees == nil || !almostEqual(*flat.FeedCostPerDayRupees, 5) {
		t.Fatalf("flat animal must still show its real feed cost, got %v", flat.FeedCostPerDayRupees)
	}

	real, ok := byTag["ec-real"]
	if !ok {
		t.Fatal("the genuinely growing animal must be present")
	}
	if !almostEqual(real.ADGGPerDay, 200) {
		t.Fatalf("real gain = %v, want 200 g/day", real.ADGGPerDay)
	}
	if real.CostPerKgGainRupees == nil || !almostEqual(*real.CostPerKgGainRupees, 25) {
		t.Fatalf("real cost/kg = %v, want 25", real.CostPerKgGainRupees)
	}

	// The flat animal must not drag the medians either: only measured growth is
	// priced, so cost_animals counts EC-REAL alone.
	if out.Pulse.CostAnimals != 1 {
		t.Fatalf("cost animals = %d, want 1 (the flat animal has no priceable gain)", out.Pulse.CostAnimals)
	}
	if out.Pulse.PairedAnimals != 2 {
		t.Fatalf("paired animals = %d, want 2 (both were weighed twice)", out.Pulse.PairedAnimals)
	}
}
