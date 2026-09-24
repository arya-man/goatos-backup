package postgres

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// TestClosedBuyerDealsOneToManyLinesAndVendorsResolveToOneFactPerDeal is the DB round-trip behind the buyer
// analytics grain proof: one fact per CLOSED deal, each claimed by exactly one buyer, where
//
//   - a deal whose buyer_vendor_id still exists is claimed by that vendor, whatever it is called;
//   - a deal whose vendor row is GONE (STG carries two such) falls back to the name match;
//   - a typed name matching exactly ONE vendor's business name (case/whitespace-insensitively) is
//     folded onto that vendor -- so sheet history and app deals for one man are one buyer;
//   - a typed name held by TWO vendors is claimed by neither (agree-or-go-bare) and stays a
//     name-only buyer, because picking one would hand a stranger's phone to the row;
//   - the contact person's name never matches;
//   - a mixed-line deal reports its lines' animals and product types, a pre-lines deal its own;
//   - non-closed deals, the farm filter and another tenant are all excluded.
func TestClosedBuyerDealsOneToManyLinesAndVendorsResolveToOneFactPerDeal(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)

	const otherTenant = "00000000-0000-4000-8000-000000000002"
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Other', 'active') ON CONFLICT DO NOTHING`, otherTenant); err != nil {
		t.Fatalf("seed other tenant: %v", err)
	}

	vendor := func(tenant, name, contact, phone, recordType, city string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO procurement_vendors (tenant_id, record_type, business_name, contact_person_name, phone_number, status, state, city)
VALUES ($1, $2, $3, nullif($4, ''), nullif($5, ''), 'active', 'Tamil Nadu', nullif($6, ''))
RETURNING vendor_id::text`, tenant, recordType, name, contact, phone, city).Scan(&id); err != nil {
			t.Fatalf("seed vendor %s: %v", name, err)
		}
		return id
	}
	mahendran := vendor(testTenant, "Mahendran", "Mahendran", "9750078019", "Agent", "Pollachi")
	// Two vendors share the business name "Riyaz": the typed name must resolve to neither.
	vendor(testTenant, "Riyaz", "", "1111111111", "Agent", "Coimbatore")
	vendor(testTenant, "riyaz ", "", "2222222222", "Butcher", "Salem")
	// A vendor whose CONTACT is called Firoz; the business name is something else, so a deal
	// typed "Firoz" must NOT fold onto it.
	vendor(testTenant, "Palladam Meats", "Firoz", "3333333333", "Butcher", "Palladam")
	// The same business name in ANOTHER tenant must not be matched.
	vendor(otherTenant, "Al Madina", "", "4444444444", "Company", "Coimbatore")

	deal := func(date, farm, buyerName, vendorID, product string, animals *float64, value, received float64, status string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, buyer_place, buyer_vendor_id, product_type, breed, animal_count, sales_value, payment_received, status)
VALUES ($1, $2, $3, $4, 'Somewhere', nullif($5, '')::uuid, $6, 'Local', $7, $8, nullif($9, 0), $10)
RETURNING id::text`, testTenant, date, farm, buyerName, vendorID, product, animals, value, received, status).Scan(&id); err != nil {
			t.Fatalf("seed deal %s: %v", buyerName, err)
		}
		return id
	}
	n := func(v float64) *float64 { return &v }
	const dangling = "565eda2a-f41b-476c-a531-e671a7310a66"

	// Mahendran: one by vendor id under a different spelling, one sheet row by name, one whose
	// vendor row is gone and falls back to the name.
	byID := deal("2026-09-09", "CBE", "MAHENDRAN AGENCIES", mahendran, "Sheep", n(40), 341964, 100000, "Deal Closed")
	byName := deal("2025-12-03", "CBE", "  mahendran ", "", "Goat", n(10), 60000, 0, "Deal Closed")
	byDangling := deal("2026-09-02", "CPT", "Mahendran", dangling, "Goat", n(25), 197415, 197415, "Deal Closed")
	// Riyaz: ambiguous name, stays name-only.
	riyaz := deal("2026-05-17", "CBE", "Riyaz", "", "Sheep", n(12), 100000, 0, "Deal Closed")
	// Firoz: matches only a contact person, stays name-only.
	firoz := deal("2026-03-14", "CBE", "Firoz", "", "Goat", n(5), 40000, 0, "Deal Closed")
	// A mixed-line deal recorded in the app: sheep + goat + manure lines, deal-level rollup.
	mixed := deal("2026-08-29", "CBE", "Al Madina", "", "Mixed", nil, 227160, 0, "Deal Closed")
	if _, err := pool.Exec(ctx, `
INSERT INTO sales_deal_lines (tenant_id, deal_id, line_no, product_type, product_code, product_kind, breed, animal_count, male_count, female_count, sales_value) VALUES
($1, $2, 1, 'Sheep', 'sheep', 'animal', 'Local', 12, NULL, NULL, 150000),
($1, $2, 2, 'Goat', 'goat', 'animal', 'Local', NULL, 3, 5, 70000),
($1, $2, 3, 'Manure', 'manure', 'other', 'Manure', NULL, NULL, NULL, 7160)`, testTenant, mixed); err != nil {
		t.Fatalf("seed lines: %v", err)
	}
	// Excluded: a failed deal, an advance-paid deal, and another tenant's closed deal.
	deal("2026-08-01", "CBE", "Mahendran", mahendran, "Sheep", n(9), 90000, 0, "Deal Failed")
	deal("2026-08-02", "CBE", "Mahendran", mahendran, "Sheep", n(9), 90000, 0, "Advance Paid")
	if _, err := pool.Exec(ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status)
VALUES ($1, '2026-08-03', 'CBE', 'Al Madina', 'Sheep', 'Local', 9, 90000, 'Deal Closed')`, otherTenant); err != nil {
		t.Fatalf("seed other tenant deal: %v", err)
	}

	facts, err := repo.ClosedBuyerDeals(ctx, testTenant, "")
	if err != nil {
		t.Fatalf("ClosedBuyerDeals: %v", err)
	}
	byDeal := map[string]domain.BuyerDealFact{}
	for _, f := range facts {
		if _, dup := byDeal[f.DealID]; dup {
			t.Fatalf("deal %s returned twice: the read fanned out", f.DealID)
		}
		byDeal[f.DealID] = f
	}
	if len(facts) != 6 {
		ids := make([]string, 0, len(facts))
		for _, f := range facts {
			ids = append(ids, f.DealID+"/"+f.BuyerName+"/"+f.BuyerKey)
		}
		sort.Strings(ids)
		t.Fatalf("facts = %d, want 6 closed deals: %v", len(facts), ids)
	}

	for _, id := range []string{byID, byName, byDangling} {
		f := byDeal[id]
		if f.BuyerKey != "vendor:"+mahendran || f.VendorID != mahendran || f.VendorName != "Mahendran" || f.VendorPhone != "9750078019" || f.VendorCategory != "Agent" || f.VendorPlace != "Pollachi, Tamil Nadu" {
			t.Fatalf("deal %s should resolve to Mahendran's register row: %+v", id, f)
		}
	}
	if f := byDeal[byID]; f.Revenue != 341964 || f.Outstanding != 241964 || f.Animals != 40 || len(f.ProductTypes) != 1 || f.ProductTypes[0] != "Sheep" {
		t.Fatalf("deal-level rollup = %+v", f)
	}
	if f := byDeal[byDangling]; f.Outstanding != 0 || f.Farm != "CPT" {
		t.Fatalf("fully paid deal should owe nothing: %+v", f)
	}
	if f := byDeal[riyaz]; f.BuyerKey != "name:riyaz" || f.VendorID != "" || f.VendorPhone != "" {
		t.Fatalf("an ambiguous name must stay name-only: %+v", f)
	}
	if f := byDeal[firoz]; f.BuyerKey != "name:firoz" || f.VendorID != "" {
		t.Fatalf("a contact-person match must not fold: %+v", f)
	}
	if f := byDeal[mixed]; f.BuyerKey != "name:al madina" || f.VendorID != "" || f.Animals != 20 || f.Revenue != 227160 ||
		len(f.ProductTypes) != 3 || f.ProductTypes[0] != "Goat" || f.ProductTypes[1] != "Manure" || f.ProductTypes[2] != "Sheep" {
		t.Fatalf("mixed-line deal = %+v", f)
	}

	// The farm filter narrows the facts, never widens them.
	cpt, err := repo.ClosedBuyerDeals(ctx, testTenant, "CPT")
	if err != nil {
		t.Fatalf("ClosedBuyerDeals CPT: %v", err)
	}
	if len(cpt) != 1 || cpt[0].DealID != byDangling {
		t.Fatalf("CPT facts = %+v", cpt)
	}

	// And through the domain: three buyers, Mahendran folded to one with three purchases.
	out := domain.BuildBuyerAnalytics(facts, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), 25, 0)
	if out.TotalBuyers != 4 || out.Summary.NotInRegister != 3 || out.Summary.RepeatBuyers != 1 {
		t.Fatalf("analytics = %+v", out.Summary)
	}
	if top := out.Buyers[0]; top.BuyerKey != "vendor:"+mahendran || top.Purchases != 3 || top.Phone != "9750078019" || top.Revenue != 599379 {
		t.Fatalf("top buyer = %+v", top)
	}
}

// TestClosedBuyerDealsStatusMatrixAdmitsOnlyClosedDeals seeds one deal in every ledger status for
// the same buyer and pins that exactly the Deal Closed row reaches the analytics: a pipeline
// state (In Discussion, Advance Paid) is not a purchase, and a failed deal never was.
func TestClosedBuyerDealsStatusMatrixAdmitsOnlyClosedDeals(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := pool.Exec(ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status) VALUES
($1, '2026-06-01', 'CBE', 'Status Buyer', 'Sheep', 'Local', 5, 50000, 'Deal Closed'),
($1, '2026-06-02', 'CBE', 'Status Buyer', 'Sheep', 'Local', 5, 50000, 'Deal Failed'),
($1, '2026-06-03', 'CBE', 'Status Buyer', 'Sheep', 'Local', 5, 50000, 'In Discussion'),
($1, '2026-06-04', 'CBE', 'Status Buyer', 'Sheep', 'Local', 5, 50000, 'Advance Paid')`, testTenant); err != nil {
		t.Fatalf("seed status matrix: %v", err)
	}
	facts, err := repo.ClosedBuyerDeals(ctx, testTenant, "")
	if err != nil {
		t.Fatalf("ClosedBuyerDeals: %v", err)
	}
	if len(facts) != 1 || facts[0].SaleDate != "2026-06-01" || facts[0].BuyerKey != "name:status buyer" {
		t.Fatalf("facts = %+v, want only the closed deal", facts)
	}
	out := domain.BuildBuyerAnalytics(facts, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), 25, 0)
	if out.TotalBuyers != 1 || out.Buyers[0].Purchases != 1 || out.Buyers[0].Repeat {
		t.Fatalf("a buyer with one closed and three open deals is a one-time buyer: %+v", out.Buyers[0])
	}
}
