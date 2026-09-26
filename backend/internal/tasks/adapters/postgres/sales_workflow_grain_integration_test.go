package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// The sale card read joins sales_deals for its subject line (SALES SOP, 2026-09-19). These pin
// the grain of that read against the shapes the aggregate-projection review names: a deal with
// MANY lines is still one card; a keyset page boundary loses no card and the chips stay whole;
// two sales on either side of an IST midnight land on their own business dates; a park's card
// names its park and another tenant's deal never leaks; and every filter bucket counts exactly
// the cards it lists.

func seedSaleDeal(t *testing.T, pool *pgxpool.Pool, ctx context.Context, dealID, buyer string, animals int, saleDate string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sales_deals (id, tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status)
VALUES ($1::uuid, $2::uuid, $3::date, 'CBE', $4, 'Goat', 'Malai', $5, 100000, 'Deal Closed')`, dealID, wfTenant, saleDate, buyer, animals); err != nil {
		t.Fatalf("seed deal %s: %v", buyer, err)
	}
}

func openSale(t *testing.T, repo *Repository, ctx context.Context, dealID string, at time.Time, parkID *string) string {
	t.Helper()
	ref := dealID
	created, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: domain.TemplateKeySalesDeal, EventAt: at, SubjectRefID: &ref, ParkID: parkID})
	if err != nil || !created {
		t.Fatalf("open sale %s: created=%v err=%v", dealID, created, err)
	}
	id, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, domain.TemplateKeySalesDeal, dealID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func listSales(t *testing.T, repo *Repository, ctx context.Context, date string, filter string, size int, cursor *domain.WorkflowCursor) domain.WorkflowListPage {
	t.Helper()
	page, err := repo.ListWorkflows(ctx, domain.WorkflowListQuery{TenantID: wfTenant, Module: domain.ModuleSales, EventDate: date, TodayDate: date, Filter: filter, PageSize: size, Cursor: cursor, Now: wfEventAt})
	if err != nil {
		t.Fatalf("list sales %s/%s: %v", date, filter, err)
	}
	return page
}

// TestSaleWorkflowCardOneToManyDealLinesStayOneCard: a deal with three product lines and two
// payments joins 1:1 on sales_deals.id, so the card read never fans out.
func TestSaleWorkflowCardOneToManyDealLinesStayOneCard(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const dealID = "5a1e5a1e-0000-4000-8000-00000000a001"
	seedSaleDeal(t, pool, ctx, dealID, "Many Lines", 9, "2026-07-27")
	for i := 1; i <= 3; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO sales_deal_lines (tenant_id, deal_id, line_no, product_type, product_code, product_kind, breed, animal_count, sales_value) VALUES ($1::uuid, $2::uuid, $3, 'Goat', 'goat', 'animal', 'Malai', 3, 30000)`, wfTenant, dealID, i); err != nil {
			t.Fatalf("seed line %d: %v", i, err)
		}
	}
	for i := 1; i <= 2; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO sales_deal_payments (tenant_id, deal_id, received_on, amount_rupees) VALUES ($1::uuid, $2::uuid, DATE '2026-07-27', 1000)`, wfTenant, dealID); err != nil {
			t.Fatalf("seed payment %d: %v", i, err)
		}
	}
	openSale(t, repo, ctx, dealID, wfEventAt, nil)
	page := listSales(t, repo, ctx, biztime.BusinessDate(wfEventAt), domain.FilterAll, 20, nil)
	if len(page.Items) != 1 || page.Chips.All != 1 {
		t.Fatalf("one deal with 3 lines and 2 payments must be ONE card: items=%d chips.all=%d", len(page.Items), page.Chips.All)
	}
	if page.Items[0].SubjectLabel != "Many Lines · 9 animals · CBE" {
		t.Fatalf("subject label = %q", page.Items[0].SubjectLabel)
	}
}

// TestSaleWorkflowListPageBoundaryKeepsChipsWhole: five sales on one day paged two at a time
// visit every card once, and every page's chips are the whole-day counts.
func TestSaleWorkflowListPageBoundaryKeepsChipsWhole(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("5a1e5a1e-0000-4000-8000-00000000b%03d", i)
		seedSaleDeal(t, pool, ctx, id, fmt.Sprintf("Buyer %d", i), i, "2026-07-27")
		openSale(t, repo, ctx, id, wfEventAt.Add(time.Duration(i)*time.Minute), nil)
	}
	date := biztime.BusinessDate(wfEventAt)
	seen := map[string]bool{}
	var cursor *domain.WorkflowCursor
	pages := 0
	for {
		page := listSales(t, repo, ctx, date, domain.FilterAll, 2, cursor)
		pages++
		if page.Chips.All != 5 {
			t.Fatalf("page %d chips.all = %d, want the whole day (5)", pages, page.Chips.All)
		}
		for _, c := range page.Items {
			if seen[c.WorkflowID] {
				t.Fatalf("card %s served twice across pages", c.WorkflowID)
			}
			seen[c.WorkflowID] = true
		}
		if page.NextCursor == nil {
			break
		}
		next, err := domain.DecodeWorkflowCursor(*page.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
		cursor = next
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 5 || pages != 3 {
		t.Fatalf("visited %d cards over %d pages, want 5 over 3", len(seen), pages)
	}
}

// TestSaleWorkflowScheduledDateSplitsSalesAcrossBusinessDates: a sale recorded at 23:50 IST and
// one at 00:10 IST the next day are on different business dates, whatever their UTC clock says.
func TestSaleWorkflowScheduledDateSplitsSalesAcrossBusinessDates(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const late, early = "5a1e5a1e-0000-4000-8000-00000000c001", "5a1e5a1e-0000-4000-8000-00000000c002"
	seedSaleDeal(t, pool, ctx, late, "Late", 1, "2026-07-27")
	seedSaleDeal(t, pool, ctx, early, "Early", 1, "2026-07-28")
	lateAt := time.Date(2026, 7, 27, 23, 50, 0, 0, biztime.DefaultLocation())
	earlyAt := time.Date(2026, 7, 28, 0, 10, 0, 0, biztime.DefaultLocation())
	openSale(t, repo, ctx, late, lateAt, nil)
	openSale(t, repo, ctx, early, earlyAt, nil)
	d27 := listSales(t, repo, ctx, "2026-07-27", domain.FilterAll, 20, nil)
	d28 := listSales(t, repo, ctx, "2026-07-28", domain.FilterAll, 20, nil)
	if len(d27.Items) != 1 || d27.Items[0].SubjectRefID != late || len(d28.Items) != 1 || d28.Items[0].SubjectRefID != early {
		t.Fatalf("27th=%d (%v) 28th=%d: the IST midnight must split the two sales", len(d27.Items), d27.Items, len(d28.Items))
	}
}

// TestSaleWorkflowScopeHierarchyIsTenantAndPark: the card names the park it was opened in, and a
// deal recorded under ANOTHER tenant never appears on this tenant's list or subject read.
func TestSaleWorkflowScopeHierarchyIsTenantAndPark(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const park = "aaaaaaa1-0000-0000-0000-00000000aa01"
	const otherTenant = "aaaaaaa1-0000-0000-0000-000000000002"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'WF-PARK', 'Channapatna', 'active') ON CONFLICT (location_id) DO NOTHING`, wfTenant, park); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Other', 'active') ON CONFLICT DO NOTHING`, otherTenant); err != nil {
		t.Fatalf("seed other tenant: %v", err)
	}
	const mine, theirs = "5a1e5a1e-0000-4000-8000-00000000d001", "5a1e5a1e-0000-4000-8000-00000000d002"
	seedSaleDeal(t, pool, ctx, mine, "Mine", 2, "2026-07-27")
	if _, err := pool.Exec(ctx, `
INSERT INTO sales_deals (id, tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status)
VALUES ($1::uuid, $2::uuid, DATE '2026-07-27', 'CBE', 'Theirs', 'Goat', 'Malai', 2, 100000, 'Deal Closed')`, theirs, otherTenant); err != nil {
		t.Fatalf("seed other tenant deal: %v", err)
	}
	p := park
	openSale(t, repo, ctx, mine, wfEventAt, &p)
	ref := theirs
	if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: otherTenant, TemplateKey: domain.TemplateKeySalesDeal, EventAt: wfEventAt, SubjectRefID: &ref}); err != nil {
		t.Fatalf("open other tenant's sale: %v", err)
	}
	page := listSales(t, repo, ctx, biztime.BusinessDate(wfEventAt), domain.FilterAll, 20, nil)
	if len(page.Items) != 1 || page.Items[0].SubjectRefID != mine || page.Items[0].ParkLabel != "Channapatna" {
		t.Fatalf("list = %+v: only this tenant's sale, naming its park", page.Items)
	}
	if _, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, domain.TemplateKeySalesDeal, theirs); err == nil {
		t.Fatal("another tenant's deal resolved through this tenant's subject read")
	}
}

// TestSaleWorkflowStatusBucketsCoverEveryFilter: an open sale sits in `due`, a completed one in
// `completed`, `all` is their sum, and each filter lists exactly what its chip counts.
func TestSaleWorkflowStatusBucketsCoverEveryFilter(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const open, done = "5a1e5a1e-0000-4000-8000-00000000e001", "5a1e5a1e-0000-4000-8000-00000000e002"
	seedSaleDeal(t, pool, ctx, open, "Open", 1, "2026-07-27")
	seedSaleDeal(t, pool, ctx, done, "Done", 1, "2026-07-27")
	openSale(t, repo, ctx, open, wfEventAt, nil)
	doneID := openSale(t, repo, ctx, done, wfEventAt, nil)
	// Walk the second sale to completion on the short (paid in full) path, as the engine would.
	if _, err := pool.Exec(ctx, `UPDATE workflow_actions SET status = 'completed', completed_at = now() WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`, wfTenant, doneID); err != nil {
		t.Fatalf("complete actions: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instances SET state = 'completed', actions_done = actions_total, next_action_key = NULL, next_action_title = NULL, next_due_at = NULL WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`, wfTenant, doneID); err != nil {
		t.Fatalf("complete workflow: %v", err)
	}
	date := biztime.BusinessDate(wfEventAt)
	all := listSales(t, repo, ctx, date, domain.FilterAll, 20, nil)
	if all.Chips.All != 2 || all.Chips.Completed != 1 || all.Chips.Due+all.Chips.Overdue != 1 {
		t.Fatalf("chips = %+v, want all 2 = completed 1 + open 1", all.Chips)
	}
	for _, f := range []struct {
		filter string
		want   int
	}{{domain.FilterAll, all.Chips.All}, {domain.FilterCompleted, all.Chips.Completed}, {domain.FilterDue, all.Chips.Due}, {domain.FilterOverdue, all.Chips.Overdue}, {domain.FilterAwaitingVideo, all.Chips.AwaitingVideo}} {
		page := listSales(t, repo, ctx, date, f.filter, 20, nil)
		if len(page.Items) != f.want {
			t.Fatalf("filter %s lists %d cards but its chip counts %d", f.filter, len(page.Items), f.want)
		}
	}
}
