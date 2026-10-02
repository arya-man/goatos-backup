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
func TestSalesActivitiesOneToManyRowsResolveOneFactPerActivity(t *testing.T) {
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
	total, imported, err := repo.VendorRegisterTotals(ctx, testTenant)
	if err != nil || total != 4 || imported != 2 {
		t.Fatalf("totals = %d/%d (%v), want 4/2", total, imported, err)
	}
}

// TestLatestVendorsPageBoundaryContinuesWithoutGapsOrRepeats walks the register two rows a page,
// including vendors created at the SAME instant (the vendor_id tie-break is what keeps the order
// total), and asserts the pages concatenate to the one-shot newest-first list with no row lost or
// repeated across a boundary, and that a page past the end is empty rather than an error.
func TestLatestVendorsPageBoundaryContinuesWithoutGapsOrRepeats(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)

	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	for i, name := range []string{"A", "B", "C", "D", "E"} {
		created := at
		if i >= 3 {
			created = at.Add(time.Hour) // D and E share one instant
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_vendors (tenant_id, record_type, business_name, status, state, created_at, updated_at)
VALUES ($1, 'Agent', $2, 'active', 'Tamil Nadu', $3, $3)`, testTenant, "Page Vendor "+name, created); err != nil {
			t.Fatal(err)
		}
	}
	all, err := repo.LatestVendors(ctx, testTenant, 10, 0)
	if err != nil || len(all) != 5 {
		t.Fatalf("one-shot list = %d (%v), want 5", len(all), err)
	}
	var walked []string
	for offset := 0; offset < 6; offset += 2 {
		page, err := repo.LatestVendors(ctx, testTenant, 2, offset)
		if err != nil {
			t.Fatalf("page at %d: %v", offset, err)
		}
		for _, v := range page {
			walked = append(walked, v.VendorID)
		}
	}
	if len(walked) != len(all) {
		t.Fatalf("pages returned %d rows, want %d", len(walked), len(all))
	}
	for i := range all {
		if walked[i] != all[i].VendorID {
			t.Fatalf("row %d differs across a page boundary: %s vs %s", i, walked[i], all[i].VendorID)
		}
	}
	past, err := repo.LatestVendors(ctx, testTenant, 2, 40)
	if err != nil || len(past) != 0 {
		t.Fatalf("past the end = %d (%v), want empty", len(past), err)
	}
}

// TestSalesActivitiesPersonNameStatusMatrix pins the name a person's activity is credited to over
// every workforce_members status shape: an ACTIVE row wins over an inactive one with a different
// name (a re-hired person); inactive rows that AGREE give that name; inactive rows that DISAGREE
// give no name rather than picking one (agree-or-go-bare); and an actor with no row at all is
// still counted, unnamed. A fan-out here would credit one activity twice.
func TestSalesActivitiesPersonNameStatusMatrix(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)

	const (
		rehired   = "11111111-1111-4111-8111-111111111111"
		leftAgree = "22222222-2222-4222-8222-222222222222"
		leftSplit = "33333333-3333-4333-8333-333333333333"
		noRow     = "44444444-4444-4444-8444-444444444444"
	)
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (tenant_id, display_code, display_name, status, user_id) VALUES
 ($1, 'SM-1', 'Old Name', 'inactive', $2), ($1, 'SM-2', 'Current Name', 'active', $2),
 ($1, 'SM-3', 'Left Person', 'inactive', $3), ($1, 'SM-4', 'Left Person', 'inactive', $3),
 ($1, 'SM-5', 'First Spelling', 'inactive', $4), ($1, 'SM-6', 'Second Spelling', 'inactive', $4)`,
		testTenant, rehired, leftAgree, leftSplit); err != nil {
		t.Fatalf("seed people: %v", err)
	}
	today := biztime.BusinessDayStart(time.Now())
	for i, actor := range []string{rehired, leftAgree, leftSplit, noRow} {
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_vendors (tenant_id, record_type, business_name, status, state, created_by, updated_by, created_at, updated_at)
VALUES ($1, 'Agent', $2, 'active', 'Tamil Nadu', $3, $3, $4, $4)`,
			testTenant, "Status Vendor "+string(rune('A'+i)), actor, today.Add(time.Duration(i+1)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	facts, err := repo.SalesActivities(ctx, testTenant, today.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{rehired: "Current Name", leftAgree: "Left Person", leftSplit: "", noRow: ""}
	seen := map[string]int{}
	for _, f := range facts {
		if f.Kind != domain.ActivityVendorAdded {
			continue
		}
		seen[f.ActorID]++
		if name, ok := want[f.ActorID]; ok && f.ActorName != name {
			t.Fatalf("actor %s credited as %q, want %q", f.ActorID, f.ActorName, name)
		}
	}
	for actor := range want {
		if seen[actor] != 1 {
			t.Fatalf("actor %s has %d vendor_added facts, want exactly 1 (no fan-out, no drop)", actor, seen[actor])
		}
	}
}
