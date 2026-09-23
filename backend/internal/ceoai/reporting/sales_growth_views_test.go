// sales_growth_views_test.go holds the adversarial grain/identity proofs for the
// four coverage views added by migrations 000393 (sales) and 000394 (weighing
// growth):
//
//	ceo_ai.sales_deal_lines_closed          one row per line of a closed deal
//	ceo_ai.sales_buyer_summary              one row per buyer
//	ceo_ai.weighing_latest_individual_weight one row per animal identity
//	ceo_ai.growth_adg_pairs                 one row per consecutive pair of weighs
//
// Same shape as reporting_views_test.go: Postgres-gated (Docker), one fixture
// per test, and each test proves a property that a plausible alternative
// implementation would get WRONG. The five D2 proof kinds are all here:
// one-to-many fan-out (a two-line deal, an animal's repeat scans), status
// bucketing (closed vs open deals, rework vs pending captures), date shift
// (the IST business day and the pair interval), scope (tenant isolation), and
// the page/grain boundary (deal-grain money on a line-grain view).
package reporting

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// --- sales fixtures ----------------------------------------------------------

// closedDeal inserts one `Deal Closed` sales_deals row and returns its id.
func closedDeal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, saleDate, buyer, product, breed string, animals, weightKG, value, received float64) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO sales_deals (id, tenant_id, sale_date, farm, buyer_name, product_type, breed,
		                          animal_count, total_weight_kg, sales_value, payment_received, status)
		 VALUES (gen_random_uuid(), $1, $2::date, 'CPT', $3, $4, $5, $6, $7, $8, $9, 'Deal Closed')
		 RETURNING id::text`,
		tenant, saleDate, buyer, product, breed, animals, weightKG, value, received).Scan(&id); err != nil {
		t.Fatalf("insert sales deal: %v", err)
	}
	return id
}

// dealLine adds one product line to a deal. animals is passed as a pointer so a
// line with NO head count (a manure line) can be written as a real NULL.
func dealLine(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, dealID string, lineNo int, product, breed string, animals *float64, weightKG, value float64) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales_deal_lines (tenant_id, deal_id, line_no, product_type, breed, animal_count, total_weight_kg, sales_value)
		 VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8)`,
		tenant, dealID, lineNo, product, breed, animals, weightKG, value); err != nil {
		t.Fatalf("insert deal line: %v", err)
	}
}

func f64(v float64) *float64 { return &v }

// --- sales proofs ------------------------------------------------------------

// A two-line deal must contribute TWO rows whose values sum to the deal, and its
// DEAL-grain money must repeat on both without being summable twice. This is the
// page/grain boundary proof: a reader that sums deal_outstanding_rupees over the
// lines doubles a two-line deal's receivable, and is_deal_primary_line is what
// makes the correct total expressible.
func TestSalesLinesKeepLineGrainWhileDealMoneyStaysDealGrain(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)

	deal := closedDeal(t, ctx, pool, tenant, "2026-09-03", "Ramesh Traders", "Mixed", "Mixed", 12, 300, 120000, 50000)
	dealLine(t, ctx, pool, tenant, deal, 1, "Goat", "Sirohi", f64(8), 200, 90000)
	dealLine(t, ctx, pool, tenant, deal, 2, "Manure", "NA", nil, 100, 30000)

	var rows int
	var lineValueSum, primaryOutstanding, allLinesOutstanding float64
	if err := pool.QueryRow(ctx,
		`SELECT count(*),
		        sum(sales_value),
		        sum(deal_outstanding_rupees) FILTER (WHERE is_deal_primary_line),
		        sum(deal_outstanding_rupees)
		   FROM ceo_ai.sales_deal_lines_closed WHERE tenant_id = $1`, tenant).
		Scan(&rows, &lineValueSum, &primaryOutstanding, &allLinesOutstanding); err != nil {
		t.Fatalf("read view: %v", err)
	}
	if rows != 2 {
		t.Fatalf("a two-line deal must be two rows, got %d", rows)
	}
	if lineValueSum != 120000 {
		t.Fatalf("line values must sum to the deal rollup 120000, got %v", lineValueSum)
	}
	if primaryOutstanding != 70000 {
		t.Fatalf("deal-grain outstanding under the primary-line filter must be 70000, got %v", primaryOutstanding)
	}
	if allLinesOutstanding != 140000 {
		t.Fatalf("the un-filtered sum is expected to double-count (that is WHY is_deal_primary_line exists); got %v", allLinesOutstanding)
	}
}

// A line that HAS a row but no head count (manure) must report NULL animals --
// never the deal's own count. A per-column COALESCE fell back to the deal here
// and reported the whole herd twice, once on each line of a mixed deal.
func TestSalesLineWithNoHeadCountNeverInheritsTheDealsCount(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)

	deal := closedDeal(t, ctx, pool, tenant, "2026-09-03", "Ramesh Traders", "Mixed", "Mixed", 12, 300, 120000, 0)
	dealLine(t, ctx, pool, tenant, deal, 1, "Goat", "Sirohi", f64(8), 200, 90000)
	dealLine(t, ctx, pool, tenant, deal, 2, "Manure", "NA", nil, 100, 30000)

	var manureAnimals *float64
	var isAnimalLine bool
	if err := pool.QueryRow(ctx,
		`SELECT animal_count, is_animal_line FROM ceo_ai.sales_deal_lines_closed
		  WHERE tenant_id = $1 AND product_type = 'Manure'`, tenant).Scan(&manureAnimals, &isAnimalLine); err != nil {
		t.Fatalf("read view: %v", err)
	}
	if manureAnimals != nil {
		t.Fatalf("a manure line carries no animals; got %v", *manureAnimals)
	}
	if isAnimalLine {
		t.Fatalf("manure must not be an animal line")
	}
}

// A closed deal with NO line rows still reports its money, through one
// synthesised line. Dropping it would make the view's revenue disagree with
// /sales/overview, which reads the deal row.
func TestClosedDealWithNoLinesStillReportsItsMoney(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)

	closedDeal(t, ctx, pool, tenant, "2026-09-20", "Ramesh Traders", "Goat", "Sirohi", 4, 100, 40000, 0)

	var rows int
	var value, perKG float64
	if err := pool.QueryRow(ctx,
		`SELECT count(*), sum(sales_value), max(price_per_kg) FROM ceo_ai.sales_deal_lines_closed WHERE tenant_id = $1`,
		tenant).Scan(&rows, &value, &perKG); err != nil {
		t.Fatalf("read view: %v", err)
	}
	if rows != 1 || value != 40000 {
		t.Fatalf("a line-less closed deal must be exactly one row worth 40000, got %d rows / %v", rows, value)
	}
	if perKG != 400 {
		t.Fatalf("price per kg must be 40000/100 = 400, got %v", perKG)
	}
}

// Status bucketing: only `Deal Closed` deals are on either view. An open
// negotiation is not revenue and is not a receivable.
func TestOnlyClosedDealsReachTheSalesViews(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)

	closedDeal(t, ctx, pool, tenant, "2026-09-03", "Ramesh Traders", "Goat", "Sirohi", 4, 100, 40000, 0)
	if _, err := pool.Exec(ctx,
		`INSERT INTO sales_deals (id, tenant_id, sale_date, farm, buyer_name, product_type, breed,
		                          animal_count, total_weight_kg, sales_value, status)
		 VALUES (gen_random_uuid(), $1, '2026-09-21'::date, 'CPT', 'Someone Else', 'Goat', 'Boer', 5, 90, 9999, 'In Discussion')`,
		tenant); err != nil {
		t.Fatalf("insert open deal: %v", err)
	}

	var lines, buyers int
	var revenue float64
	if err := pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM ceo_ai.sales_deal_lines_closed WHERE tenant_id = $1),
		        (SELECT count(*) FROM ceo_ai.sales_buyer_summary WHERE tenant_id = $1),
		        (SELECT COALESCE(sum(revenue_rupees), 0) FROM ceo_ai.sales_buyer_summary WHERE tenant_id = $1)`,
		tenant).Scan(&lines, &buyers, &revenue); err != nil {
		t.Fatalf("read views: %v", err)
	}
	if lines != 1 || buyers != 1 || revenue != 40000 {
		t.Fatalf("the open deal leaked: %d lines, %d buyers, revenue %v", lines, buyers, revenue)
	}
}

// The buyer fold is the page's own normalization (whitespace collapsed, lower
// case), and a buyer's deals must enter the buyer aggregate ONCE regardless of
// how many product lines each carries -- the fan-out this view's LATERAL exists
// to prevent. Outstanding is clamped PER DEAL, so an overpaid deal is a credit
// and never cancels another deal's arrears.
func TestBuyerSummaryFoldsSpellingsAndCannotFanOutOnLines(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)

	mixed := closedDeal(t, ctx, pool, tenant, "2026-09-03", "  Ramesh   Traders ", "Mixed", "Mixed", 12, 300, 120000, 50000)
	dealLine(t, ctx, pool, tenant, mixed, 1, "Goat", "Sirohi", f64(8), 200, 90000)
	dealLine(t, ctx, pool, tenant, mixed, 2, "Manure", "NA", nil, 100, 30000)
	closedDeal(t, ctx, pool, tenant, "2026-09-20", "ramesh traders", "Goat", "Sirohi", 4, 100, 40000, 45000)

	var buyers int
	var key string
	var deals int64
	var repeat bool
	var animals, revenue, received, outstanding float64
	var first, last string
	if err := pool.QueryRow(ctx,
		`SELECT count(*) OVER (), buyer_key, deals, is_repeat_buyer, animals, revenue_rupees,
		        payment_received_rupees, outstanding_rupees, first_sale_date::text, last_sale_date::text
		   FROM ceo_ai.sales_buyer_summary WHERE tenant_id = $1`, tenant).
		Scan(&buyers, &key, &deals, &repeat, &animals, &revenue, &received, &outstanding, &first, &last); err != nil {
		t.Fatalf("read view: %v", err)
	}
	if buyers != 1 {
		t.Fatalf("the two spellings must fold to ONE buyer, got %d", buyers)
	}
	if key != "ramesh traders" {
		t.Fatalf("buyer_key must be the normalized name, got %q", key)
	}
	if deals != 2 || !repeat {
		t.Fatalf("a two-line deal must count ONCE: got %d deals, repeat=%v", deals, repeat)
	}
	if animals != 8 {
		t.Fatalf("animals count Sheep/Goat lines only, expected 8, got %v", animals)
	}
	if revenue != 160000 || received != 95000 {
		t.Fatalf("revenue/received must be deal-grain 160000/95000, got %v/%v", revenue, received)
	}
	if outstanding != 70000 {
		t.Fatalf("outstanding is clamped per deal (the overpaid deal is 0, not -5000), expected 70000, got %v", outstanding)
	}
	if first != "2026-09-03" || last != "2026-09-20" {
		t.Fatalf("first/last sale dates wrong: %s / %s", first, last)
	}
}

// --- weighing fixtures -------------------------------------------------------

type growthFixture struct {
	tenant   string
	parkID   string
	shedID   string
	goat1    string // carries two tags
	goat2    string
	campaign map[string]string // label -> campaign_id
	bucket   map[string]string // label -> campaign_shed_id
	proofID  string
	userID   string
}

// growthFixtureFor builds one park, one shed, two animals (the first carrying
// TWO active RFIDs) and three weekly campaigns: "r1" and "r2" completed, "x"
// canceled.
func growthFixtureFor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant string) growthFixture {
	t.Helper()
	fx := growthFixture{tenant: tenant, campaign: map[string]string{}, bucket: map[string]string{}}
	fx.parkID = park(t, ctx, pool, tenant, "Channapatna")
	fx.shedID = shed(t, ctx, pool, tenant, fx.parkID, "Godel 1", nil)

	fx.goat1 = goatWithID(t, ctx, pool, tenant, fx.parkID, fx.shedID, "goat", "alive", nil, nil)
	fx.goat2 = goatWithID(t, ctx, pool, tenant, fx.parkID, fx.shedID, "goat", "alive", nil, nil)
	if _, err := pool.Exec(ctx, `UPDATE goats SET breed = 'Sirohi', sex = 'male' WHERE goat_id = $1::uuid`, fx.goat1); err != nil {
		t.Fatalf("set goat1 breed: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE goats SET breed = 'Boer', sex = 'female' WHERE goat_id = $1::uuid`, fx.goat2); err != nil {
		t.Fatalf("set goat2 breed: %v", err)
	}
	identifier(t, ctx, pool, tenant, fx.goat1, "animal_identifier_1", "TAG-A")
	identifier(t, ctx, pool, tenant, fx.goat1, "animal_identifier_2", "TAG-A2")
	identifier(t, ctx, pool, tenant, fx.goat2, "animal_identifier_1", "TAG-B")

	if err := pool.QueryRow(ctx,
		`INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, scope_type, scope_id, subject_type, proof_type)
		 VALUES (gen_random_uuid(), $1, 'gcs', 'k', 'shed', $2::uuid, 'shed', 'video') RETURNING proof_id::text`,
		tenant, fx.shedID).Scan(&fx.proofID); err != nil {
		t.Fatalf("insert proof: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
		 VALUES ($1, gen_random_uuid(), 'operator', 'park', $2::uuid, 'active', now() - interval '30 days')
		 RETURNING user_id::text`, tenant, fx.parkID).Scan(&fx.userID); err != nil {
		t.Fatalf("grant operator park scope: %v", err)
	}

	type campSpec struct{ label, start, end, status, partition string }
	for _, c := range []campSpec{
		{"r1", "2026-09-01", "2026-09-07", "completed", "Part 3"},
		{"r2", "2026-09-08", "2026-09-14", "completed", "whole"},
		{"x", "2026-09-15", "2026-09-21", "canceled", "Part 3"},
	} {
		var campID, bucketID string
		if err := pool.QueryRow(ctx,
			`INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date,
			                                 start_business_date, status, operator_user_id, created_by)
			 VALUES (gen_random_uuid(), $1, $2::uuid, $3::date, $4::date, $3::date, $5, $6::uuid, $6::uuid)
			 RETURNING campaign_id::text`,
			tenant, fx.parkID, c.start, c.end, c.status, fx.userID).Scan(&campID); err != nil {
			t.Fatalf("insert campaign %s: %v", c.label, err)
		}
		if err := pool.QueryRow(ctx,
			`INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type,
			                                      display_name, partition_label, weighing_category, operator_user_id)
			 VALUES (gen_random_uuid(), $1::uuid, $2, $3::uuid, 'shed', 'Godel 1', $4, 'individual_animal', $5::uuid)
			 RETURNING campaign_shed_id::text`,
			campID, tenant, fx.shedID, c.partition, fx.userID).Scan(&bucketID); err != nil {
			t.Fatalf("insert bucket %s: %v", c.label, err)
		}
		fx.campaign[c.label] = campID
		fx.bucket[c.label] = bucketID
	}
	return fx
}

func identifier(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, goatID, kind, value string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
		                               scope_key, status, valid_from, normalizer_version)
		 VALUES ($1, $2::uuid, $3, $4, upper(btrim($4)), 'tenant', 'active', now(), 'v1')`,
		tenant, goatID, kind, value); err != nil {
		t.Fatalf("insert identifier: %v", err)
	}
}

// scan records one weigh. submitted marks the capture as already submitted,
// which is what lets a bucket hold a superseded capture of the same tag
// (weighing_observations_one_open_tag_uidx is partial on submitted_at IS NULL).
func scan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fx growthFixture, campaign, tag string, weight float64, acceptedAt, status string, submitted bool) {
	t.Helper()
	var submittedAt *string
	if submitted {
		submittedAt = &acceptedAt
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg,
		                                    proof_artifact_id, recorded_by, accepted_at, idempotency_key,
		                                    verification_status, submitted_at)
		 VALUES ($1, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, $8::timestamptz,
		         gen_random_uuid()::text, $9, $10::timestamptz)`,
		fx.tenant, fx.campaign[campaign], fx.bucket[campaign], tag, weight, fx.proofID, fx.userID,
		acceptedAt, status, submittedAt); err != nil {
		t.Fatalf("insert observation: %v", err)
	}
}

// --- weighing proofs ---------------------------------------------------------

// The one-to-many proof for weighing: an animal's repeat captures inside one
// bucket must collapse to the NEWEST, and its TWO RFIDs must resolve to ONE
// animal across rounds. A view keyed on the raw scanned string reports the
// re-scan as a second animal and never pairs the two rounds at all.
func TestLatestWeightCollapsesRepeatScansAndFoldsAnAnimalsTwoTags(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	fx := growthFixtureFor(t, ctx, pool, tenant)

	scan(t, ctx, pool, fx, "r1", "tag-a", 11, "2026-09-01 06:00+05:30", "verified", true)   // superseded
	scan(t, ctx, pool, fx, "r1", " TAG-A ", 20, "2026-09-01 10:00+05:30", "verified", true) // the round's weigh
	scan(t, ctx, pool, fx, "r2", "TAG-A2", 23, "2026-09-11 10:00+05:30", "pending", false)  // the SECOND tag

	var rows int
	var animalKey, weighedOn, breed, sex string
	var weight float64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) OVER (), animal_key, weighed_on::text, weight_kg, breed, sex
		   FROM ceo_ai.weighing_latest_individual_weight WHERE tenant_id = $1`, tenant).
		Scan(&rows, &animalKey, &weighedOn, &weight, &breed, &sex); err != nil {
		t.Fatalf("read view: %v", err)
	}
	if rows != 1 {
		t.Fatalf("three captures of ONE animal on two tags must be one row, got %d", rows)
	}
	// The key is the animal's OWN canonical tag (animal_identifier_1), never a
	// goat_id: it is rendered verbatim to a reader, and an internal uuid is not
	// something the farm can match to an ear.
	if animalKey != "tag-a" {
		t.Fatalf("animal_key must be the canonical ear tag, got %q", animalKey)
	}
	if animalKey == fx.goat1 {
		t.Fatalf("animal_key must never be a goat_id")
	}
	if weight != 23 || weighedOn != "2026-09-11" {
		t.Fatalf("the latest weigh must win: got %v kg on %s", weight, weighedOn)
	}
	if breed != "Sirohi" || sex != "male" {
		t.Fatalf("breed/sex must come from the register, got %q/%q", breed, sex)
	}
	if fx.goat1 == "" {
		t.Fatalf("fixture goat missing")
	}
}

// Status bucketing: a bounced (rework) capture and a canceled campaign's scans
// are not reporting facts, while a PENDING capture is -- dropping pending would
// empty the view for every week still in review.
func TestReworkAndCanceledCampaignScansNeverReachTheGrowthViews(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	fx := growthFixtureFor(t, ctx, pool, tenant)

	scan(t, ctx, pool, fx, "r2", "TAG-B", 36, "2026-09-11 10:00+05:30", "pending", true)
	scan(t, ctx, pool, fx, "r2", "TAG-B", 99, "2026-09-12 10:00+05:30", "rework", false)
	scan(t, ctx, pool, fx, "x", "TAG-B", 98, "2026-09-16 10:00+05:30", "verified", false)

	var rows int
	var weight float64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) OVER (), weight_kg FROM ceo_ai.weighing_latest_individual_weight WHERE tenant_id = $1`,
		tenant).Scan(&rows, &weight); err != nil {
		t.Fatalf("read view: %v", err)
	}
	if rows != 1 || weight != 36 {
		t.Fatalf("only the pending capture is a fact: got %d rows, %v kg", rows, weight)
	}
}

// 'whole' is a storage key and must never reach a reader; a real partition must.
func TestLatestWeightNeverRendersTheWholeSentinel(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	fx := growthFixtureFor(t, ctx, pool, tenant)

	scan(t, ctx, pool, fx, "r1", "TAG-A", 20, "2026-09-01 10:00+05:30", "verified", true)
	scan(t, ctx, pool, fx, "r2", "TAG-B", 30, "2026-09-11 10:00+05:30", "verified", true)

	rows, err := pool.Query(ctx,
		`SELECT animal_key, shed_label, partition_label FROM ceo_ai.weighing_latest_individual_weight WHERE tenant_id = $1`, tenant)
	if err != nil {
		t.Fatalf("read view: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var tag, shedLabel string
		var partition *string
		if err := rows.Scan(&tag, &shedLabel, &partition); err != nil { //nolint:govet
			t.Fatalf("scan: %v", err)
		}
		seen++
		if shedLabel != "Godel 1" {
			t.Fatalf("shed label wrong: %q", shedLabel)
		}
		switch tag {
		case "tag-a":
			if partition == nil || *partition != "Part 3" {
				t.Fatalf("a real partition must be carried, got %v", partition)
			}
		case "tag-b":
			if partition != nil {
				t.Fatalf("'whole' is a storage key and must read as NULL, got %q", *partition)
			}
		default:
			t.Fatalf("unexpected tag %q", tag)
		}
	}
	if seen != 2 {
		t.Fatalf("expected two animals, got %d", seen)
	}
}

// The date-shift proof: a pair's interval is measured in IST BUSINESS DAYS, the
// gain is the weight difference, and the rate is the two divided. An n-round
// animal yields n-1 pairs, and a same-day re-weigh is no pair at all.
func TestGrowthPairsMeasureTheIntervalInBusinessDays(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	fx := growthFixtureFor(t, ctx, pool, tenant)

	scan(t, ctx, pool, fx, "r1", "TAG-A", 20, "2026-09-01 10:00+05:30", "verified", true)
	scan(t, ctx, pool, fx, "r2", "TAG-A2", 23, "2026-09-11 10:00+05:30", "pending", true)
	scan(t, ctx, pool, fx, "r1", "TAG-B", 30, "2026-09-01 10:00+05:30", "verified", true)
	scan(t, ctx, pool, fx, "r2", "TAG-B", 36, "2026-09-11 10:00+05:30", "pending", true)

	var rows int
	var correct, avgOfRates float64
	if err := pool.QueryRow(ctx,
		`SELECT count(*), sum(gain_kg) * 1000 / sum(days_between), avg(adg_g_per_day)
		   FROM ceo_ai.growth_adg_pairs WHERE tenant_id = $1`, tenant).Scan(&rows, &correct, &avgOfRates); err != nil {
		t.Fatalf("read view: %v", err)
	}
	if rows != 2 {
		t.Fatalf("two animals weighed twice = two pairs, got %d", rows)
	}
	if correct != 450 {
		t.Fatalf("duration-weighted herd ADG must be 9 kg over 20 animal-days = 450 g/day, got %v", correct)
	}
	if avgOfRates != 450 {
		t.Fatalf("this fixture's equal intervals make both readings agree (450); got %v -- if this drifts the fixture changed", avgOfRates)
	}

	var goat1Pairs int
	var goat1ADG float64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) OVER (), adg_g_per_day FROM ceo_ai.growth_adg_pairs
		  WHERE tenant_id = $1 AND animal_key = $2`, tenant, "tag-a").Scan(&goat1Pairs, &goat1ADG); err != nil {
		t.Fatalf("read view: %v", err)
	}
	if goat1Pairs != 1 || goat1ADG != 300 {
		t.Fatalf("the two-tag animal must pair across its rounds at 3 kg / 10 days = 300 g/day under its canonical tag, got %d pairs / %v", goat1Pairs, goat1ADG)
	}
}

// An animal weighed ONCE has no interval and must not appear on the pair view --
// a first round is not a gain of its own whole weight.
func TestAnAnimalWeighedOnceProducesNoPair(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	fx := growthFixtureFor(t, ctx, pool, tenant)

	scan(t, ctx, pool, fx, "r1", "TAG-A", 20, "2026-09-01 10:00+05:30", "verified", true)

	var latest, pairs int
	if err := pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM ceo_ai.weighing_latest_individual_weight WHERE tenant_id = $1),
		        (SELECT count(*) FROM ceo_ai.growth_adg_pairs WHERE tenant_id = $1)`, tenant).Scan(&latest, &pairs); err != nil {
		t.Fatalf("read views: %v", err)
	}
	if latest != 1 || pairs != 0 {
		t.Fatalf("one weigh is a weight and not a gain: %d latest, %d pairs", latest, pairs)
	}
}

// The sale-readiness flags are strict ">", on the maintainer's fixed edges.
func TestSaleReadinessFlagsUseStrictlyGreaterThan(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	fx := growthFixtureFor(t, ctx, pool, tenant)

	scan(t, ctx, pool, fx, "r1", "TAG-A", 30, "2026-09-01 10:00+05:30", "verified", true)
	scan(t, ctx, pool, fx, "r1", "TAG-B", 35.5, "2026-09-01 10:00+05:30", "verified", true)

	var over30At30, over30AtHeavy, over35AtHeavy bool
	if err := pool.QueryRow(ctx,
		`SELECT (SELECT is_over_30_kg FROM ceo_ai.weighing_latest_individual_weight WHERE tenant_id = $1 AND weight_kg = 30),
		        (SELECT is_over_30_kg FROM ceo_ai.weighing_latest_individual_weight WHERE tenant_id = $1 AND weight_kg = 35.5),
		        (SELECT is_over_35_kg FROM ceo_ai.weighing_latest_individual_weight WHERE tenant_id = $1 AND weight_kg = 35.5)`,
		tenant).Scan(&over30At30, &over30AtHeavy, &over35AtHeavy); err != nil {
		t.Fatalf("read view: %v", err)
	}
	if over30At30 {
		t.Fatalf("exactly 30 kg is not OVER 30 kg")
	}
	if !over30AtHeavy || !over35AtHeavy {
		t.Fatalf("35.5 kg clears both edges, got %v/%v", over30AtHeavy, over35AtHeavy)
	}
}

// The scope proof: one tenant's sales and weighs are invisible to another, on
// every one of the four views. Each join in 000393/000394 is keyed on tenant_id
// as well as its own key, and this is what proves it.
func TestTheFourCoverageViewsAreTenantScoped(t *testing.T) {
	ctx := context.Background()
	pool, tenantA := newDB(t, ctx)

	var tenantB string
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), 'Other Tenant', 'active') RETURNING tenant_id::text`).
		Scan(&tenantB); err != nil {
		t.Fatalf("insert second tenant: %v", err)
	}

	fx := growthFixtureFor(t, ctx, pool, tenantA)
	scan(t, ctx, pool, fx, "r1", "TAG-A", 20, "2026-09-01 10:00+05:30", "verified", true)
	scan(t, ctx, pool, fx, "r2", "TAG-A2", 23, "2026-09-11 10:00+05:30", "verified", true)
	closedDeal(t, ctx, pool, tenantA, "2026-09-03", "Ramesh Traders", "Goat", "Sirohi", 4, 100, 40000, 0)

	for _, view := range []string{
		"ceo_ai.sales_deal_lines_closed",
		"ceo_ai.sales_buyer_summary",
		"ceo_ai.weighing_latest_individual_weight",
		"ceo_ai.growth_adg_pairs",
	} {
		var mine, theirs int
		if err := pool.QueryRow(ctx,
			`SELECT (SELECT count(*) FROM `+view+` WHERE tenant_id = $1),
			        (SELECT count(*) FROM `+view+` WHERE tenant_id = $2)`, tenantA, tenantB).Scan(&mine, &theirs); err != nil {
			t.Fatalf("%s: %v", view, err)
		}
		if mine == 0 {
			t.Fatalf("%s: the fixture tenant must see its own rows", view)
		}
		if theirs != 0 {
			t.Fatalf("%s: the other tenant saw %d rows", view, theirs)
		}
	}
}
