package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// TestSalesActivitiesResolveOneFactPerActivityAtTheDocumentedGrain is the DB round-trip behind
// the sales executive analytics grain proof (docs/decisions/sales-executive-analytics.md):
//
//   - a vendor with a known adder is one vendor_added; a sheet-imported vendor (no adder) is none;
//   - several saves of one vendor by one person on one business day are ONE vendor_edited, even
//     when both the audit row and the register's last-edit stamp record the same save;
//   - one market city called by one person on one day is ONE market_call however many prices;
//   - lead calls, recorded sales and payments come from the sales audit trail with the buyer named
//     and the money carried; an audit row with no actor is not attributed to anyone;
//   - the person's name resolves through workforce_members.user_id;
//   - rows before the window and another tenant's rows are excluded;
//   - the vendor edit audit written by UpdateVendor / UpdateVendorStatus is counted.
func TestSalesActivitiesResolveOneFactPerActivityAtTheDocumentedGrain(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)

	const (
		hemant      = "ad6198a2-24eb-5b8c-a885-c322a7a3de56"
		manohar     = "d1408eef-58e5-52e2-9a0f-6e38237627d1"
		otherTenant = "00000000-0000-4000-8000-000000000002"
	)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Other', 'active') ON CONFLICT DO NOTHING`, otherTenant)
	exec(`INSERT INTO workforce_members (tenant_id, display_code, display_name, status, user_id)
VALUES ($1, 'SE-1', 'Hemant', 'active', $2), ($1, 'SE-2', 'Manohar', 'active', $3)`, testTenant, hemant, manohar)

	now := time.Now()
	today := biztime.BusinessDayStart(now)
	since := today.AddDate(0, 0, -13)

	vendor := func(tenant, name, adder string, createdAt time.Time) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO procurement_vendors (tenant_id, record_type, business_name, status, state, created_by, updated_by, created_at, updated_at)
VALUES ($1, 'Butcher', $2, 'active', 'Tamil Nadu', nullif($3, '')::uuid, nullif($3, '')::uuid, $4, $4)
RETURNING vendor_id::text`, tenant, name, adder, createdAt).Scan(&id); err != nil {
			t.Fatalf("seed vendor %s: %v", name, err)
		}
		return id
	}
	added := vendor(testTenant, "Palladam Meats", hemant, today.Add(10*time.Hour))
	vendor(testTenant, "Imported From Sheet", "", today.Add(9*time.Hour))
	vendor(testTenant, "Before The Window", hemant, since.AddDate(0, 0, -3))
	vendor(otherTenant, "Other Tenant Vendor", hemant, today.Add(10*time.Hour))
	edited := vendor(testTenant, "Mahendran", "", since.AddDate(0, 0, -30))

	// Two audited saves by Manohar today plus the register's own stamp of the second: one edit.
	exec(`INSERT INTO audit_log (tenant_id, actor_id, actor_type, action, resource_type, resource_id, metadata, recorded_at)
VALUES ($1, $2, 'human', 'procurement.vendor.update', 'procurement_vendor', $3, '{}', $4),
       ($1, $2, 'human', 'procurement.vendor.update', 'procurement_vendor', $3, '{}', $5)`,
		testTenant, manohar, edited, today.Add(11*time.Hour), today.Add(12*time.Hour))
	exec(`UPDATE procurement_vendors SET updated_by = $2, updated_at = $3 WHERE vendor_id = $1`,
		edited, manohar, today.Add(12*time.Hour))
	// The real write path: a status flip through the repository writes its own audit row
	// (Hemant, today), a second edit of a different person on the same vendor and day.
	var rowVersion int64
	if err := pool.QueryRow(ctx, `SELECT row_version FROM procurement_vendors WHERE vendor_id = $1`, added).Scan(&rowVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateVendorStatus(ctx, testTenant, added, "inactive", rowVersion, hemant); err != nil {
		t.Fatalf("status flip: %v", err)
	}

	// Market: one city, three prices, one person, one day -> one call.
	var cityID, q1, q2, q3 string
	if err := pool.QueryRow(ctx, `INSERT INTO market_cities (tenant_id, name) VALUES ($1, 'Pollachi') RETURNING id::text`, testTenant).Scan(&cityID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []*string{&q1, &q2, &q3} {
		if err := pool.QueryRow(ctx, `INSERT INTO market_questions (tenant_id, label, unit_label) VALUES ($1, gen_random_uuid()::text, 'per kg') RETURNING id::text`, testTenant).Scan(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{q1, q2, q3} {
		exec(`INSERT INTO market_price_entries (tenant_id, city_id, question_id, business_date, price, city_name, question_label, unit_label, recorded_by, recorded_at)
VALUES ($1, $2, $3, $4::date, 600, 'Pollachi', 'Live', 'per kg', $5, $6)`, testTenant, cityID, q, today.Format("2006-01-02"), hemant, today.Add(8*time.Hour))
	}

	// Sales: a recorded deal, a payment on it, a buyer lead call, and an actorless backfill.
	var dealID string
	if err := pool.QueryRow(ctx, `
INSERT INTO sales_deals (tenant_id, sale_date, farm, buyer_name, buyer_place, product_type, breed, animal_count, sales_value, status)
VALUES ($1, $2::date, 'CBE', 'Mahendran', 'Pollachi', 'Goat', 'Local', 12, 120000, 'Deal Closed') RETURNING id::text`,
		testTenant, today.Format("2006-01-02")).Scan(&dealID); err != nil {
		t.Fatal(err)
	}
	var leadID string
	if err := pool.QueryRow(ctx, `INSERT INTO sales_buyer_leads (tenant_id, buyer_name, call_status) VALUES ($1, 'Riyaz', 'Interested') RETURNING id::text`, testTenant).Scan(&leadID); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO audit_log (tenant_id, actor_id, actor_type, action, resource_type, resource_id, metadata, recorded_at) VALUES
($1, $2, 'human', 'sales.deal.record', 'sales_deal', $3, '{}', $5),
($1, $2, 'human', 'sales.deal.payment_record', 'sales_deal', $3, '{"amount_rupees": 50000}', $6),
($1, $2, 'human', 'sales.buyer_lead.status', 'sales_buyer_lead', $4, '{"call_status": "Interested"}', $6),
($1, NULL, 'system', 'sales.deal.record', 'sales_deal', $3, '{}', $6),
($1, $2, 'human', 'sales.deal.record', 'sales_deal', $3, '{}', $7)`,
		testTenant, hemant, dealID, leadID, today.Add(14*time.Hour), today.Add(15*time.Hour), since.AddDate(0, 0, -1))

	facts, err := repo.SalesActivities(ctx, testTenant, since)
	if err != nil {
		t.Fatalf("SalesActivities: %v", err)
	}
	byKind := map[string][]domain.SalesActivityFact{}
	for _, f := range facts {
		byKind[f.Kind] = append(byKind[f.Kind], f)
	}
	expect := map[string]int{
		domain.ActivityVendorAdded:     1,
		domain.ActivityVendorEdited:    2, // Manohar on Mahendran, Hemant's status flip on Palladam Meats
		domain.ActivityMarketCall:      1,
		domain.ActivityLeadCall:        1,
		domain.ActivitySaleRecorded:    1,
		domain.ActivityPaymentRecorded: 1,
	}
	for kind, want := range expect {
		if got := len(byKind[kind]); got != want {
			t.Fatalf("%s: %d facts, want %d (all facts: %+v)", kind, got, want, facts)
		}
	}
	if len(facts) != 7 {
		t.Fatalf("%d facts, want 7: %+v", len(facts), facts)
	}
	if f := byKind[domain.ActivityVendorAdded][0]; f.ActorName != "Hemant" || f.Subject != "Palladam Meats" || f.Category != "Butcher" {
		t.Fatalf("vendor_added = %+v", f)
	}
	if f := byKind[domain.ActivityMarketCall][0]; f.Subject != "Pollachi" || f.Animals != 3 {
		t.Fatalf("market_call = %+v, want Pollachi with 3 prices", f)
	}
	if f := byKind[domain.ActivitySaleRecorded][0]; f.Subject != "Mahendran" || f.Amount != 120000 || f.Animals != 12 {
		t.Fatalf("sale_recorded = %+v", f)
	}
	if f := byKind[domain.ActivityPaymentRecorded][0]; f.Amount != 50000 {
		t.Fatalf("payment_recorded = %+v", f)
	}
	if f := byKind[domain.ActivityLeadCall][0]; f.Subject != "Riyaz" || f.Category != "Interested" {
		t.Fatalf("lead_call = %+v", f)
	}
	for _, f := range facts {
		if f.BusinessDate != today.Format("2006-01-02") {
			t.Fatalf("fact on %s, want today: %+v", f.BusinessDate, f)
		}
	}

	latest, err := repo.LatestVendors(ctx, testTenant, 10, 0)
	if err != nil {
		t.Fatalf("LatestVendors: %v", err)
	}
	if len(latest) != 4 || latest[0].BusinessName != "Palladam Meats" || latest[0].AddedByName != "Hemant" {
		t.Fatalf("latest vendors = %+v", latest)
	}
	if latest[1].BusinessName != "Imported From Sheet" || latest[1].AddedByKnown {
		t.Fatalf("an imported vendor must read as imported: %+v", latest[1])
	}
	page2, err := repo.LatestVendors(ctx, testTenant, 2, 2)
	if err != nil || len(page2) != 2 || page2[0].VendorID != latest[2].VendorID {
		t.Fatalf("page 2 of 2 = %+v (%v), want to continue at %s", page2, err, latest[2].BusinessName)
	}
	total, imported, err := repo.VendorRegisterTotals(ctx, testTenant)
	if err != nil || total != 4 || imported != 2 {
		t.Fatalf("totals = %d/%d (%v), want 4/2", total, imported, err)
	}
}
