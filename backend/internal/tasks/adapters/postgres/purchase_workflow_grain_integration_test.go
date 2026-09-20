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

// PROCUREMENT IS SOP-DRIVEN END TO END (2026-09-20): the two purchase cards join the load they
// are about for their subject line, the way the sale card joins its deal. These pin the grain of
// that read against the shapes the aggregate-projection review names -- the twin of
// sales_workflow_grain_integration_test.go, and for the same reason.
//
// projection-review: membership=workflow_instances for (tenant, module, event_date), one row per
// workflow; group_key=workflow_id; join_cardinality=animal_purchase_loads is unique on load_id
// (PRIMARY KEY) and feed_purchases on feed_purchase_id (PRIMARY KEY), and each is matched on
// exactly that column against wi.subject_ref_id, so both joins are 1:0..1 and NEITHER can fan a
// workflow into two cards no matter how many candidates or payments hang off the load; the
// numerator (cards listed) and denominator (chips) range over the identical workflow_id key set,
// which the status test asserts filter by filter; pagination=keyset on (next_due_at, workflow_id)
// so page size changes rows only and the chips stay whole-filter; scope=tenant_id on the instance
// AND on both joins, plus module and event_date.

func seedAnimalLoad(t *testing.T, pool *pgxpool.Pool, ctx context.Context, loadID, ref, vendor, farm string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO animal_purchase_loads (load_id, tenant_id, load_ref, vendor_id, vendor_name, farm_label, expected_count, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3, gen_random_uuid(), $4, $5, 10, $3)`, loadID, wfTenant, ref, vendor, farm); err != nil {
		t.Fatalf("seed animal load %s: %v", ref, err)
	}
}

func seedFeedLoad(t *testing.T, pool *pgxpool.Pool, ctx context.Context, id, feed, vendor, farm string, batch int) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (feed_purchase_id, tenant_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg, depletes_from, vendor, entry_source)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, DATE '2026-07-27', 1000, DATE '2026-07-27', $6, 'app')`,
		id, wfTenant, farm, feed, batch, vendor); err != nil {
		t.Fatalf("seed feed load %s: %v", feed, err)
	}
}

func openPurchase(t *testing.T, repo *Repository, ctx context.Context, template, subject string, at time.Time, parkID *string) string {
	t.Helper()
	ref := subject
	created, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: wfTenant, TemplateKey: template, EventAt: at, SubjectRefID: &ref, ParkID: parkID})
	if err != nil || !created {
		t.Fatalf("open %s %s: created=%v err=%v", template, subject, created, err)
	}
	id, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, template, subject)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func listPurchases(t *testing.T, repo *Repository, ctx context.Context, date, filter string, size int, cursor *domain.WorkflowCursor) domain.WorkflowListPage {
	t.Helper()
	page, err := repo.ListWorkflows(ctx, domain.WorkflowListQuery{TenantID: wfTenant, Module: domain.ModuleProcurement, EventDate: date, TodayDate: date, Filter: filter, PageSize: size, Cursor: cursor, Now: wfEventAt})
	if err != nil {
		t.Fatalf("list purchases %s/%s: %v", date, filter, err)
	}
	return page
}

// TestPurchaseWorkflowCardOneToManyCandidatesStayOneCard: a load of many animals -- and a feed
// load carrying many instalments -- each joins on its own primary key, so the card read cannot
// fan out. This is the shape that would break first if either join were widened to a column the
// load is not unique on.
func TestPurchaseWorkflowCardOneToManyCandidatesStayOneCard(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const loadID = "9a11a11a-0000-4000-8000-00000000a001"
	seedAnimalLoad(t, pool, ctx, loadID, "LOAD-A1", "Many Animals Vendor", "CBE")
	for i := 1; i <= 4; i++ {
		if _, err := pool.Exec(ctx, `
INSERT INTO animal_purchase_candidates (tenant_id, load_id, seq_no, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3, $2::text || $3::text)`, wfTenant, loadID, i); err != nil {
			t.Fatalf("seed candidate %d: %v", i, err)
		}
	}
	openPurchase(t, repo, ctx, domain.TemplateKeyAnimalPurchaseIntake, loadID, wfEventAt, nil)

	const feedID = "9a11a11a-0000-4000-8000-00000000a002"
	seedFeedLoad(t, pool, ctx, feedID, "Dry Masoor Bhusa", "Sanchit", "CBE", 355)
	for i := 1; i <= 3; i++ {
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchase_payments (tenant_id, feed_purchase_id, paid_on, amount_rupees, idempotency_key)
VALUES ($1::uuid, $2::uuid, DATE '2026-07-27', 500, $2::text || $3::text)`, wfTenant, feedID, i); err != nil {
			t.Skipf("feed_purchase_payments not present in this schema: %v", err)
		}
	}
	openPurchase(t, repo, ctx, domain.TemplateKeyFeedPurchaseIntake, feedID, wfEventAt.Add(time.Minute), nil)

	page := listPurchases(t, repo, ctx, biztime.BusinessDate(wfEventAt), domain.FilterAll, 20, nil)
	if len(page.Items) != 2 || page.Chips.All != 2 {
		t.Fatalf("one animal load with 4 candidates and one feed load with 3 payments must be TWO cards: items=%d chips.all=%d", len(page.Items), page.Chips.All)
	}
	labels := map[string]string{}
	for _, c := range page.Items {
		labels[c.SubjectRefID] = c.SubjectLabel
	}
	if labels[loadID] == "" || labels[feedID] == "" {
		t.Fatalf("both purchases must carry a subject line, got %+v", labels)
	}
	if labels[loadID] == labels[feedID] {
		t.Fatalf("the two purchases must not read identically: %q", labels[loadID])
	}
}

// TestPurchaseWorkflowListPageBoundaryKeepsChipsWhole: five loads on one day paged two at a time
// visit every card once, and every page's chips are the whole-day counts.
func TestPurchaseWorkflowListPageBoundaryKeepsChipsWhole(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("9a11a11a-0000-4000-8000-00000000b%03d", i)
		seedAnimalLoad(t, pool, ctx, id, fmt.Sprintf("LOAD-B%d", i), fmt.Sprintf("Vendor %d", i), "CBE")
		openPurchase(t, repo, ctx, domain.TemplateKeyAnimalPurchaseIntake, id, wfEventAt.Add(time.Duration(i)*time.Minute), nil)
	}
	date := biztime.BusinessDate(wfEventAt)
	seen := map[string]bool{}
	var cursor *domain.WorkflowCursor
	pages := 0
	for {
		page := listPurchases(t, repo, ctx, date, domain.FilterAll, 2, cursor)
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

// TestPurchaseWorkflowScopeHierarchyIsTenantAndPark: a load's card names its park, and another
// tenant's load never leaks into this tenant's list or through the subject read.
func TestPurchaseWorkflowScopeHierarchyIsTenantAndPark(t *testing.T) {
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
	const mine, theirs = "9a11a11a-0000-4000-8000-00000000d001", "9a11a11a-0000-4000-8000-00000000d002"
	seedAnimalLoad(t, pool, ctx, mine, "LOAD-MINE", "Mine", "CPT")
	if _, err := pool.Exec(ctx, `
INSERT INTO animal_purchase_loads (load_id, tenant_id, load_ref, vendor_id, vendor_name, farm_label, expected_count, idempotency_key)
VALUES ($1::uuid, $2::uuid, 'LOAD-THEIRS', gen_random_uuid(), 'Theirs', 'CBE', 1, 'LOAD-THEIRS')`, theirs, otherTenant); err != nil {
		t.Fatalf("seed other tenant load: %v", err)
	}
	p := park
	openPurchase(t, repo, ctx, domain.TemplateKeyAnimalPurchaseIntake, mine, wfEventAt, &p)
	ref := theirs
	if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{TenantID: otherTenant, TemplateKey: domain.TemplateKeyAnimalPurchaseIntake, EventAt: wfEventAt, SubjectRefID: &ref}); err != nil {
		t.Fatalf("open other tenant's load: %v", err)
	}
	page := listPurchases(t, repo, ctx, biztime.BusinessDate(wfEventAt), domain.FilterAll, 20, nil)
	if len(page.Items) != 1 || page.Items[0].SubjectRefID != mine || page.Items[0].ParkLabel != "Channapatna" {
		t.Fatalf("list = %+v: only this tenant's load, naming its park", page.Items)
	}
	if _, err := repo.WorkflowIDBySubjectRef(ctx, wfTenant, domain.TemplateKeyAnimalPurchaseIntake, theirs); err == nil {
		t.Fatal("another tenant's load resolved through this tenant's subject read")
	}
}

// TestPurchaseWorkflowStatusBucketsCoverEveryFilter: an open purchase sits in `due`, a completed
// one in `completed`, `all` is their sum, and each filter lists exactly what its chip counts.
func TestPurchaseWorkflowStatusBucketsCoverEveryFilter(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const open, done = "9a11a11a-0000-4000-8000-00000000e001", "9a11a11a-0000-4000-8000-00000000e002"
	seedAnimalLoad(t, pool, ctx, open, "LOAD-OPEN", "Open", "CBE")
	seedAnimalLoad(t, pool, ctx, done, "LOAD-DONE", "Done", "CBE")
	openPurchase(t, repo, ctx, domain.TemplateKeyAnimalPurchaseIntake, open, wfEventAt, nil)
	doneID := openPurchase(t, repo, ctx, domain.TemplateKeyAnimalPurchaseIntake, done, wfEventAt, nil)
	if _, err := pool.Exec(ctx, `UPDATE workflow_actions SET status = 'completed', completed_at = now() WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`, wfTenant, doneID); err != nil {
		t.Fatalf("complete actions: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instances SET state = 'completed', actions_done = actions_total, next_action_key = NULL, next_action_title = NULL, next_due_at = NULL WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid`, wfTenant, doneID); err != nil {
		t.Fatalf("complete workflow: %v", err)
	}
	date := biztime.BusinessDate(wfEventAt)
	all := listPurchases(t, repo, ctx, date, domain.FilterAll, 20, nil)
	if all.Chips.All != 2 || all.Chips.Completed != 1 || all.Chips.Due+all.Chips.Overdue != 1 {
		t.Fatalf("chips = %+v, want all 2 = completed 1 + open 1", all.Chips)
	}
	for _, f := range []struct {
		filter string
		want   int
	}{{domain.FilterAll, all.Chips.All}, {domain.FilterCompleted, all.Chips.Completed}, {domain.FilterDue, all.Chips.Due}, {domain.FilterOverdue, all.Chips.Overdue}, {domain.FilterAwaitingVideo, all.Chips.AwaitingVideo}} {
		page := listPurchases(t, repo, ctx, date, f.filter, 20, nil)
		if len(page.Items) != f.want {
			t.Fatalf("filter %s lists %d cards but its chip counts %d", f.filter, len(page.Items), f.want)
		}
	}
}
