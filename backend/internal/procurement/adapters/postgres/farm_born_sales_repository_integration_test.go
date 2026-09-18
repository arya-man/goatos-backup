package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// farmBornFixture is the herd the farm-born tests read: two parks, three pens (one partitioned),
// animals on and off loads, sold with and without deals, inside and outside the window.
type farmBornFixture struct {
	cbePark, cptPark string
	cbeShed, cptShed string
	facts            map[string]string // name -> goat id
}

func seedFarmBornFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) farmBornFixture {
	t.Helper()
	fx := farmBornFixture{facts: map[string]string{}}
	fx.cbePark = parkIDByCode(t, ctx, pool, "CBE")
	fx.cptPark = parkIDByCode(t, ctx, pool, "CPT")

	shed := func(park, code, name string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($1, 'shed', $2, $3, $4::uuid, 'active')
RETURNING location_id::text`, testTenant, code, name, park).Scan(&id); err != nil {
			t.Fatalf("seed shed %s: %v", name, err)
		}
		return id
	}
	fx.cbeShed = shed(fx.cbePark, "FB_GODEL1", "Godel 1")
	fx.cptShed = shed(fx.cptPark, "FB_GANDHI", "Gandhi")

	var party string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_type, display_name, status) VALUES ('org', 'Farm Born Vendor', 'active') RETURNING party_id::text`).Scan(&party); err != nil {
		t.Fatalf("seed party: %v", err)
	}

	goat := func(name, origin, species, breed, sex, stage, lifecycle, exitReason, park, shedID, partition, exitedAt string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, species, breed, sex, management_stage, lifecycle_status, exit_reason, origin_type, custodian_party_id, park_id, shed_id, exited_at)
VALUES ($1, $2, nullif($3, ''), $4, nullif($5, ''), $6, nullif($7, ''), nullif($8, ''), $9, $10::uuid, $11::uuid, nullif($12, '')::timestamptz)
RETURNING goat_id::text`, testTenant, species, breed, sex, stage, lifecycle, exitReason, origin, party, park, shedID, exitedAt).Scan(&id); err != nil {
			t.Fatalf("seed goat %s: %v", name, err)
		}
		if partition != "" || shedID != "" {
			if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2::uuid, $3::uuid, COALESCE(nullif($4, ''), 'whole'), 'seed')`, testTenant, id, shedID, partition); err != nil {
				t.Fatalf("seed partition %s: %v", name, err)
			}
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, lower($3), $3, true, 'active', now(), 'v1')`, testTenant, id, "TAG-"+name); err != nil {
			t.Fatalf("seed identifier %s: %v", name, err)
		}
		fx.facts[name] = id
		return id
	}

	// On farm, farm born: two in CBE Godel 1 - Part 3, one in CBE Godel 1 - Part 5, one in CPT.
	goat("alive-cbe-p3-a", "birth", "goat", "Malai", "female", "F2-Female", "alive", "", fx.cbePark, fx.cbeShed, "Part 3", "")
	goat("alive-cbe-p3-b", "birth", "goat", "Malai", "male", "F2-Male", "alive", "", fx.cbePark, fx.cbeShed, "Part 3", "")
	goat("alive-cbe-p5", "birth", "sheep", "Anantapur Sheep", "female", "Non-Pregnant", "icu", "", fx.cbePark, fx.cbeShed, "Part 5", "")
	goat("alive-cpt", "birth", "goat", "Sirohi", "male", "F2-Male", "alive", "", fx.cptPark, fx.cptShed, "", "")
	// Sold farm born, in window, with a deal (two animals on one 20000 deal -> 10000 each).
	soldDealA := goat("sold-deal-a", "birth", "goat", "Malai", "male", "F2-Male", "sold", "sold", fx.cbePark, fx.cbeShed, "Part 3", "2026-09-05T04:00:00Z")
	soldDealB := goat("sold-deal-b", "birth", "goat", "malai", "female", "F2-Female", "sold", "sold", fx.cbePark, fx.cbeShed, "Part 3", "2026-09-05T04:00:00Z")
	// Sold farm born, in window, NO deal: exited 2026-09-16 19:30Z = 2026-09-17 01:00 IST.
	goat("sold-nodeal", "birth", "goat", "Osmanabadi", "female", "F2-Female", "sold", "sold", fx.cbePark, fx.cbeShed, "Part 5", "2026-09-16T19:30:00Z")
	// Sold farm born OUTSIDE the window (July).
	goat("sold-old", "birth", "goat", "Malai", "male", "F2-Male", "sold", "sold", fx.cbePark, fx.cbeShed, "Part 3", "2026-07-01T04:00:00Z")
	// Dead farm born: neither on farm nor sold.
	goat("dead", "birth", "goat", "Malai", "male", "F2-Male", "dead", "died", fx.cbePark, fx.cbeShed, "Part 3", "2026-09-02T04:00:00Z")
	// Merged: excluded outright.
	goat("merged", "birth", "goat", "Malai", "male", "F2-Male", "merged", "", fx.cbePark, fx.cbeShed, "Part 3", "")
	// Other origin readings.
	goat("procured-noload", "procured", "goat", "Beetal", "female", "Non-Pregnant", "alive", "", fx.cbePark, fx.cbeShed, "", "")
	goat("origin-null", "", "sheep", "Anantapur Sheep", "female", "Non-Pregnant", "alive", "", fx.cptPark, fx.cptShed, "", "")
	// On a load, marked birth by mistake: belongs to Load wise, excluded here.
	onLoad := goat("on-load", "birth", "goat", "Malai", "male", "F2-Male", "alive", "", fx.cbePark, fx.cbeShed, "Part 3", "")
	// On a load but NEVER accepted: not on Load wise, so it stays here.
	rejected := goat("load-rejected", "birth", "goat", "Malai", "male", "F2-Male", "alive", "", fx.cbePark, fx.cbeShed, "Part 3", "")

	var load string
	if err := pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, status, idempotency_key)
VALUES ($1, $2, '2026-08-01', 'accepted_intake', 'fb-load') RETURNING load_id::text`, testTenant, party).Scan(&load); err != nil {
		t.Fatalf("seed load: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', now())`, testTenant, load, onLoad); err != nil {
		t.Fatalf("seed load goat: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, exit_reason)
VALUES ($1, $2::uuid, $3::uuid, 'rejected', 'source_rejected', 'canceled')`, testTenant, load, rejected); err != nil {
		t.Fatalf("seed rejected load goat: %v", err)
	}

	// The deal: 20000 over two tagged animals, dated 2026-09-06 (differs from the exit day, and
	// the deal's date must win). A released allocation on the same deal must not dilute the share.
	var dealID string
	if err := pool.QueryRow(ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value)
VALUES ($1, '2026-09-06', 'CBE', 'Farm Born Buyer', 'Goat', 'Malai', 2, 20000) RETURNING id::text`, testTenant).Scan(&dealID); err != nil {
		t.Fatalf("seed deal: %v", err)
	}
	tag := func(goatID, status, key string) {
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_sale_allocations (tenant_id, goat_id, sales_deal_id, status, idempotency_key, park_id, shed_id, partition_label, released_at, release_reason)
VALUES ($1, $2::uuid, $3::uuid, $4, $5, $6::uuid, $7::uuid, 'Part 3',
        CASE WHEN $4 = 'released' THEN now() END, CASE WHEN $4 = 'released' THEN 'test' END)`,
			testTenant, goatID, dealID, status, key, fx.cbePark, fx.cbeShed); err != nil {
			t.Fatalf("seed allocation: %v", err)
		}
	}
	tag(soldDealA, "tagged", "fb-alloc-a")
	tag(soldDealB, "tagged", "fb-alloc-b")
	tag(fx.facts["sold-nodeal"], "released", "fb-alloc-released")
	return fx
}

func farmBornByName(fx farmBornFixture, facts []domain.FarmBornAnimalFact) map[string]domain.FarmBornAnimalFact {
	byID := map[string]string{}
	for name, id := range fx.facts {
		byID[id] = name
	}
	out := map[string]domain.FarmBornAnimalFact{}
	for _, f := range facts {
		out[byID[f.GoatID]] = f
	}
	return out
}

// TestFarmBornAnimalsOneToManyLoadsDealsAndTagsResolveToOneFactPerAnimal is the DB round-trip behind the
// farm-born grain proof: exactly one fact per admitted animal, where a released allocation, a
// rejected load row and a second identifier can never fan an animal out, the deal share divides
// over the TAGGED animals only, the deal's date beats the exit day, an exit stamped at 01:00 IST
// lands on its IST calendar day, the pen reads back as the farm spells it, and a merged animal,
// an accepted load member and a dead animal are absent.
func TestFarmBornAnimalsOneToManyLoadsDealsAndTagsResolveToOneFactPerAnimal(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	fx := seedFarmBornFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	facts, err := repo.FarmBornAnimals(ctx, testTenant, domain.FarmBornFilter{From: "2026-08-18", To: "2026-09-18", Origin: domain.FarmBornOriginBirth})
	if err != nil {
		t.Fatal(err)
	}
	got := farmBornByName(fx, facts)
	want := map[string]string{
		"alive-cbe-p3-a": domain.FarmBornOnFarm, "alive-cbe-p3-b": domain.FarmBornOnFarm, "alive-cbe-p5": domain.FarmBornOnFarm,
		"alive-cpt": domain.FarmBornOnFarm, "load-rejected": domain.FarmBornOnFarm,
		"sold-deal-a": domain.FarmBornSold, "sold-deal-b": domain.FarmBornSold, "sold-nodeal": domain.FarmBornSold,
	}
	if len(facts) != len(want) {
		names := []string{}
		for n := range got {
			names = append(names, n)
		}
		t.Fatalf("facts = %d %v, want %d", len(facts), names, len(want))
	}
	for name, bucket := range want {
		if got[name].Bucket != bucket {
			t.Fatalf("%s bucket = %q, want %q", name, got[name].Bucket, bucket)
		}
	}
	a := got["sold-deal-a"]
	if a.SaleValue == nil || *a.SaleValue != 10000 || a.SaleDate != "2026-09-06" || a.BuyerName != "Farm Born Buyer" || a.Tag != "TAG-sold-deal-a" {
		t.Fatalf("sold-deal-a = %+v", a)
	}
	if a.PenDisplay() != "Godel 1 - Part 3" || a.ParkName == "" || a.PenKey() != fx.cbeShed+"|Part 3" {
		t.Fatalf("sold-deal-a pen = %q key %q park %q", a.PenDisplay(), a.PenKey(), a.ParkName)
	}
	nd := got["sold-nodeal"]
	if nd.SaleValue != nil || nd.SaleDate != "2026-09-17" || nd.BuyerName != "" || nd.PenDisplay() != "Godel 1 - Part 5" {
		t.Fatalf("sold-nodeal = %+v (date must be the IST day of the exit, value absent)", nd)
	}
	if got["alive-cpt"].PenDisplay() != "Gandhi" || got["alive-cpt"].PenKey() != fx.cptShed {
		t.Fatalf("undivided pen = %q key %q", got["alive-cpt"].PenDisplay(), got["alive-cpt"].PenKey())
	}
	if got["alive-cbe-p5"].Bucket != domain.FarmBornOnFarm {
		t.Fatal("an animal in ICU is still on the farm")
	}

	// The domain's headline over these facts: the breakdowns sum to it and the breed fold is
	// case-insensitive ("Malai" and "malai" are one breed).
	page := domain.BuildFarmBornSales(facts, domain.FarmBornFilter{From: "2026-08-18", To: "2026-09-18", Origin: domain.FarmBornOriginBirth}, 25, 0)
	if page.Summary.OnFarm != 5 || page.Summary.Sold != 3 || page.Summary.SoldPriced != 2 || page.Summary.Revenue != 20000 || page.Summary.AvgPrice != 10000 {
		t.Fatalf("summary = %+v", page.Summary)
	}
	malai := page.ByBreed[0]
	if malai.Label != "Malai" || malai.Sold != 2 || malai.Revenue != 20000 {
		t.Fatalf("by breed[0] = %+v", malai)
	}
}

// TestFarmBornAnimalsStatusMatrixAndOriginBuckets pins every lifecycle bucket and every origin
// reading: sold is lifecycle OR exit reason, the live set is alive + clinical, dead / merged are
// absent, a sale outside the window is absent while its animal is still absent from on-farm, and
// the three origin readings partition the not-on-a-load population with no overlap.
func TestFarmBornAnimalsStatusMatrixAndOriginBuckets(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	fx := seedFarmBornFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	read := func(origin, from, to string) map[string]domain.FarmBornAnimalFact {
		t.Helper()
		facts, err := repo.FarmBornAnimals(ctx, testTenant, domain.FarmBornFilter{From: from, To: to, Origin: origin})
		if err != nil {
			t.Fatal(err)
		}
		return farmBornByName(fx, facts)
	}
	birth := read(domain.FarmBornOriginBirth, "2026-08-18", "2026-09-18")
	for _, absent := range []string{"dead", "merged", "on-load", "sold-old", "procured-noload", "origin-null"} {
		if _, ok := birth[absent]; ok {
			t.Fatalf("%s must be absent from the farm-born read", absent)
		}
	}
	// Widen the window back to July: the old sale appears, nothing else moves.
	wide := read(domain.FarmBornOriginBirth, "2026-06-01", "2026-09-18")
	if wide["sold-old"].Bucket != domain.FarmBornSold || len(wide) != len(birth)+1 {
		t.Fatalf("widened window: sold-old = %+v, %d facts vs %d", wide["sold-old"], len(wide), len(birth))
	}
	// A window with no sales still lists every on-farm animal.
	empty := read(domain.FarmBornOriginBirth, "2026-01-01", "2026-01-31")
	if len(empty) != 5 {
		t.Fatalf("empty window facts = %d, want the 5 on-farm animals", len(empty))
	}
	for _, f := range empty {
		if f.Bucket != domain.FarmBornOnFarm {
			t.Fatalf("empty window must carry no sold fact: %+v", f)
		}
	}
	bought := read(domain.FarmBornOriginBoughtNoLoad, "2026-08-18", "2026-09-18")
	if len(bought) != 1 || bought["procured-noload"].Bucket != domain.FarmBornOnFarm {
		t.Fatalf("bought-no-load = %+v", bought)
	}
	unknown := read(domain.FarmBornOriginNotRecorded, "2026-08-18", "2026-09-18")
	if len(unknown) != 1 || unknown["origin-null"].Bucket != domain.FarmBornOnFarm {
		t.Fatalf("not-recorded = %+v", unknown)
	}

	// Options for the farm-born reading: parks CBE before CPT, both pens of Godel 1 and the
	// undivided Gandhi, both species, the breeds folded case-insensitively, every stage seen.
	options, err := repo.FarmBornOptions(ctx, testTenant, domain.FarmBornOriginBirth)
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Parks) != 2 || options.Parks[0].Key != fx.cbePark || options.Parks[1].Key != fx.cptPark {
		t.Fatalf("parks = %+v", options.Parks)
	}
	penLabels := []string{}
	for _, p := range options.Pens {
		penLabels = append(penLabels, p.Label)
	}
	if len(options.Pens) != 3 || penLabels[0] != "Godel 1 - Part 3" || penLabels[1] != "Godel 1 - Part 5" || penLabels[2] != "Gandhi" || options.Pens[2].ParkID != fx.cptPark {
		t.Fatalf("pens = %+v", options.Pens)
	}
	if options.Pens[0].Key != fx.cbeShed+"|Part 3" {
		t.Fatalf("pen key = %q", options.Pens[0].Key)
	}
	breedKeys := []string{}
	for _, b := range options.Breeds {
		breedKeys = append(breedKeys, b.Key)
	}
	if len(options.Breeds) != 4 || breedKeys[0] != "anantapur sheep" || breedKeys[1] != "malai" || breedKeys[2] != "osmanabadi" || breedKeys[3] != "sirohi" {
		t.Fatalf("breeds = %+v", options.Breeds)
	}
	if len(options.Species) != 2 || len(options.Sexes) != 2 || len(options.Stages) != 3 {
		t.Fatalf("species/sexes/stages = %d/%d/%d", len(options.Species), len(options.Sexes), len(options.Stages))
	}
}

// TestFarmBornAnimalsParkScopeAndPenPredicates pins the dimension predicates on BOTH sides of the
// read (on farm and sold): park, the pen key with a partition (matched through the normalized
// key, so "3" finds "Part 3"), the undivided pen, species, a case-insensitive breed, sex and stage.
func TestFarmBornAnimalsParkScopeAndPenPredicates(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	fx := seedFarmBornFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	read := func(f domain.FarmBornFilter) map[string]domain.FarmBornAnimalFact {
		t.Helper()
		f.From, f.To, f.Origin = "2026-08-18", "2026-09-18", domain.FarmBornOriginBirth
		facts, err := repo.FarmBornAnimals(ctx, testTenant, f)
		if err != nil {
			t.Fatal(err)
		}
		return farmBornByName(fx, facts)
	}
	names := func(m map[string]domain.FarmBornAnimalFact) map[string]bool {
		out := map[string]bool{}
		for n := range m {
			out[n] = true
		}
		return out
	}
	assertExactly := func(label string, got map[string]domain.FarmBornAnimalFact, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: got %v, want %v", label, names(got), want)
		}
		for _, w := range want {
			if _, ok := got[w]; !ok {
				t.Fatalf("%s: missing %s in %v", label, w, names(got))
			}
		}
	}
	assertExactly("park CPT", read(domain.FarmBornFilter{ParkID: fx.cptPark}), "alive-cpt")
	assertExactly("pen Part 3 via normalized key", read(domain.FarmBornFilter{ShedID: fx.cbeShed, Partition: "3"}),
		"alive-cbe-p3-a", "alive-cbe-p3-b", "load-rejected", "sold-deal-a", "sold-deal-b")
	assertExactly("pen Part 5", read(domain.FarmBornFilter{ShedID: fx.cbeShed, Partition: "Part 5"}), "alive-cbe-p5", "sold-nodeal")
	assertExactly("undivided pen", read(domain.FarmBornFilter{ShedID: fx.cptShed}), "alive-cpt")
	assertExactly("species sheep", read(domain.FarmBornFilter{Species: "sheep"}), "alive-cbe-p5")
	assertExactly("breed MALAI", read(domain.FarmBornFilter{Breed: "MALAI"}), "alive-cbe-p3-a", "alive-cbe-p3-b", "load-rejected", "sold-deal-a", "sold-deal-b")
	assertExactly("sex female", read(domain.FarmBornFilter{Sex: "female"}), "alive-cbe-p3-a", "alive-cbe-p5", "sold-deal-b", "sold-nodeal")
	assertExactly("stage f2-male", read(domain.FarmBornFilter{Stage: "f2-male"}), "alive-cbe-p3-b", "alive-cpt", "load-rejected", "sold-deal-a")
	assertExactly("park CBE + sex male", read(domain.FarmBornFilter{ParkID: fx.cbePark, Sex: "male"}), "alive-cbe-p3-b", "load-rejected", "sold-deal-a")
}

// TestFarmBornSalesPaginationSlicesTheLedgerOnly pins that the page boundary applies to the sold
// ledger alone on a real read: page two of a three-row ledger holds one row, while the summary,
// the breakdowns and total_sold are the same whole-filter figures on every page.
func TestFarmBornSalesPaginationSlicesTheLedgerOnly(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedFarmBornFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	filter := domain.FarmBornFilter{From: "2026-08-18", To: "2026-09-18", Origin: domain.FarmBornOriginBirth}
	facts, err := repo.FarmBornAnimals(ctx, testTenant, filter)
	if err != nil {
		t.Fatal(err)
	}
	first := domain.BuildFarmBornSales(facts, filter, 2, 0)
	second := domain.BuildFarmBornSales(facts, filter, 2, 2)
	if first.TotalSold != 3 || second.TotalSold != 3 || len(first.Sold) != 2 || len(second.Sold) != 1 {
		t.Fatalf("pages = %d/%d rows of %d/%d", len(first.Sold), len(second.Sold), first.TotalSold, second.TotalSold)
	}
	if first.Summary != second.Summary {
		t.Fatalf("summary must not move across pages: %+v vs %+v", first.Summary, second.Summary)
	}
	if len(first.ByPen) != len(second.ByPen) || first.ByPen[0] != second.ByPen[0] {
		t.Fatalf("breakdowns must not move across pages: %+v vs %+v", first.ByPen, second.ByPen)
	}
	// Newest sale first: the 17 Sep no-deal sale leads, the two 6 Sep deal sales follow.
	if first.Sold[0].SaleDate != "2026-09-17" || first.Sold[1].SaleDate != "2026-09-06" || second.Sold[0].SaleDate != "2026-09-06" {
		t.Fatalf("ledger order = %s, %s | %s", first.Sold[0].SaleDate, first.Sold[1].SaleDate, second.Sold[0].SaleDate)
	}
}
