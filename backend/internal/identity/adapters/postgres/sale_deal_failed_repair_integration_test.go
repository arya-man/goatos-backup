package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	salesdomain "github.com/vgoats/goatos/backend/internal/sales/domain"
	taskspg "github.com/vgoats/goatos/backend/internal/tasks/adapters/postgres"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
	tasksports "github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// TestRepair000450ReleasesAnimalsOfSalesThatFailedBeforeTheRelease (docs/decisions/sales-sop.md ->
// "A failed sale" -> "Existing data"). A deal marked Deal Failed BEFORE the release consumer
// existed never emits sales.deal.status_changed again (SetDealStatus emits only on a change, and
// Deal Failed is final), so its tagged animals stayed 'sold' for ever. Migration 000450
// re-announces the failure in the exact envelope SetDealStatus writes; the SAME consumers the
// relay delivers to then release the animals and cancel the workflow. Proven here on real Postgres
// through the relay's own envelope validator and envelope decoder, twice, with a Deal Closed sale
// beside it that must not move.
func TestRepair000450ReleasesAnimalsOfSalesThatFailedBeforeTheRelease(t *testing.T) {
	ctx := context.Background()
	pool, repo := startSaleWriteDBOnPgtest(t, ctx)
	f := seedShedStageFixture(t, ctx, pool)
	one := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	two := seedStageGoat(t, ctx, pool, f.castroShed, "2", "F2", "adult")
	kept := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	before := map[string]goatPlace{one: readGoatPlace(t, ctx, pool, one), two: readGoatPlace(t, ctx, pool, two)}

	// Legacy data: both deals tagged, then the failed one marked Deal Failed DIRECTLY -- no event,
	// exactly as a deal failed before the status event existed.
	seedSaleAllocationDeal(t, ctx, pool, saleDealFailed, 2)
	seedSaleAllocationDeal(t, ctx, pool, saleDealB, 1)
	if _, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealFailed, []ports.SaleAllocationRow{
		{GoatID: one, RowVersion: goatRowVersion(t, pool, one)},
		{GoatID: two, RowVersion: goatRowVersion(t, pool, two)},
	}, "legacy-fail")); err != nil {
		t.Fatalf("tag failed deal: %v", err)
	}
	if _, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealB, []ports.SaleAllocationRow{
		{GoatID: kept, RowVersion: goatRowVersion(t, pool, kept)},
	}, "legacy-closed")); err != nil {
		t.Fatalf("tag closed deal: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sales_deals SET status = $3 WHERE tenant_id = $1::uuid AND id = $2::uuid`,
		ssTenant, saleDealFailed, salesdomain.StatusDealFailed); err != nil {
		t.Fatal(err)
	}
	// The failed deal's workflow is still open (opened before it failed).
	tasksRepo := taskspg.NewRepository(pool, 10*time.Second)
	ref := saleDealFailed
	if _, err := tasksRepo.OpenWorkflow(ctx, tasksports.OpenWorkflowCommand{TenantID: ssTenant, TemplateKey: tasksdomain.TemplateKeySalesDeal,
		EventAt: time.Date(2026, 8, 20, 6, 0, 0, 0, time.UTC), SubjectRefID: &ref}); err != nil {
		t.Fatalf("open sale workflow: %v", err)
	}

	// The relay's own path: envelope validator, envelope decoder, a bus with the production handlers.
	validator, err := outboxapp.NewEnvelopeValidator(filepath.Join("..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	bus := eventbus.NewInProcessBus()
	identityapp.NewSaleFailedReleaseHandler(repo).Register(bus)
	tasksapp.NewSaleStatusChangedWorkflowHandler(tasksapp.NewService(tasksRepo, nil)).Register(bus)
	deliver := func() int {
		t.Helper()
		rows, err := pool.Query(ctx, `
SELECT tenant_id::text, event_type, payload FROM outbox_messages
WHERE event_type = 'sales.deal.status_changed' AND idempotency_key LIKE '%:repair-000450'
ORDER BY created_at`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			var tenant, eventType string
			var envelope []byte
			if err := rows.Scan(&tenant, &eventType, &envelope); err != nil {
				t.Fatal(err)
			}
			if err := validator.Validate(envelope); err != nil {
				t.Fatalf("the repair envelope must pass the relay's validator: %v\n%s", err, envelope)
			}
			ev, err := eventbus.EventFromEnvelope(envelope, eventbus.Event{Type: eventType, TenantID: tenant})
			if err != nil {
				t.Fatal(err)
			}
			if err := bus.Publish(ctx, ev); err != nil {
				t.Fatalf("deliver repair event: %v", err)
			}
			n++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// RED without the repair: nothing announces the failure, the animals stay sold.
	if n := deliver(); n != 0 {
		t.Fatalf("precondition: no repair event before 000450 runs, got %d", n)
	}
	for _, id := range []string{one, two} {
		if p := readGoatPlace(t, ctx, pool, id); p.lifecycle != "sold" {
			t.Fatalf("precondition: a legacy failed sale's animal is still sold, got %+v", p)
		}
	}

	apply000450(t, ctx, pool)
	apply000450(t, ctx, pool) // a re-run inserts nothing
	var announced int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages WHERE event_type = 'sales.deal.status_changed' AND aggregate_id = ANY($1::uuid[])`,
		[]string{saleDealFailed, saleDealB}).Scan(&announced); err != nil {
		t.Fatal(err)
	}
	if announced != 1 {
		t.Fatalf("000450 must announce the failed deal once and the closed deal never, got %d events", announced)
	}
	for i := 0; i < 2; i++ { // a redelivery releases nothing twice
		if n := deliver(); n != 1 {
			t.Fatalf("deliver pass %d: want 1 repair event, got %d", i+1, n)
		}
	}

	for _, id := range []string{one, two} {
		got, was := readGoatPlace(t, ctx, pool, id), before[id]
		if got.lifecycle != "alive" || got.exitReason != nil || got.exited || got.shed != was.shed || got.partition != was.partition {
			t.Fatalf("released animal %s = %+v, want alive in %s / %q with no exit", id, got, was.shed, was.partition)
		}
	}
	// Released with who / when / why: blank actor on a deal with no status audit -> the tagger.
	var released, tagged, attributed int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE status = 'released'), count(*) FILTER (WHERE status = 'tagged'),
       count(*) FILTER (WHERE status = 'released' AND released_at IS NOT NULL AND released_by = $3::uuid AND release_reason <> '')
FROM goat_sale_allocations WHERE tenant_id = $1::uuid AND sales_deal_id = $2::uuid`, ssTenant, saleDealFailed, ssActor).Scan(&released, &tagged, &attributed); err != nil {
		t.Fatal(err)
	}
	if released != 2 || tagged != 0 || attributed != 2 {
		t.Fatalf("allocations released=%d tagged=%d attributed=%d, want 2 / 0 / 2", released, tagged, attributed)
	}
	var reinstated, saleReleased int
	if err := pool.QueryRow(ctx, `
SELECT (SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'goat.reinstated' AND aggregate_id = ANY($2::uuid[])),
       (SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'goat.sale_released' AND aggregate_id = $3::uuid)`,
		ssTenant, []string{one, two}, saleDealFailed).Scan(&reinstated, &saleReleased); err != nil {
		t.Fatal(err)
	}
	if reinstated != 2 || saleReleased != 1 {
		t.Fatalf("goat.reinstated=%d goat.sale_released=%d, want 2 and 1", reinstated, saleReleased)
	}
	// The failed sale's workflow is cancelled by the same delivery.
	workflowID, err := tasksRepo.WorkflowIDBySubjectRef(ctx, ssTenant, tasksdomain.TemplateKeySalesDeal, saleDealFailed)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := tasksRepo.GetWorkflow(ctx, ssTenant, workflowID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if detail.Card.State != tasksdomain.WorkflowStateCanceled {
		t.Fatalf("failed sale workflow state = %s, want canceled", detail.Card.State)
	}
	// The Deal Closed sale is untouched: still tagged, its animal still sold.
	var closedTagged int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM goat_sale_allocations WHERE tenant_id = $1::uuid AND sales_deal_id = $2::uuid AND status = 'tagged'`,
		ssTenant, saleDealB).Scan(&closedTagged); err != nil {
		t.Fatal(err)
	}
	if closedTagged != 1 || readGoatPlace(t, ctx, pool, kept).lifecycle != "sold" {
		t.Fatalf("a Deal Closed sale must keep its tagged animal sold (tagged=%d)", closedTagged)
	}
	// Nothing left tagged on a failed deal: a third run of the migration announces nothing new.
	apply000450(t, ctx, pool)
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE event_type = 'sales.deal.status_changed' AND aggregate_id = $1::uuid`,
		saleDealFailed).Scan(&announced); err != nil {
		t.Fatal(err)
	}
	if announced != 1 {
		t.Fatalf("after the release, 000450 must stay a no-op, got %d events", announced)
	}
}

// apply000450 runs migration 000450's own Up SQL.
func apply000450(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "migrations", "postgres", "000450_sales_failed_deal_release_repair.sql"))
	if err != nil {
		t.Fatalf("read 000450: %v", err)
	}
	up := strings.SplitN(string(raw), "-- +goose Down", 2)[0]
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("apply 000450: %v", err)
	}
}
