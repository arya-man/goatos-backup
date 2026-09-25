package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	salespg "github.com/vgoats/goatos/backend/internal/sales/adapters/postgres"
	salesdomain "github.com/vgoats/goatos/backend/internal/sales/domain"
)

// A FAILED SALE (maintainer decisions 2026-09-25, docs/decisions/sales-sop.md -> "A failed sale"),
// across the two modules on real Postgres, through the production path: sales.SetDealStatus emits
// sales.deal.status_changed in its own transaction, and identity's SaleFailedReleaseHandler --
// the consumer every bus registers -- releases every tagged animal back into the herd.

const saleDealFailed = "77777777-7777-4777-8777-7777777777f1"

// startSaleWriteDBOnPgtest is startCorrectionWriteDB on the shared pgtest harness (a migrated
// throwaway database; OCI admin DSN or Docker), seeded with the same synthetic fixture.
func startSaleWriteDBOnPgtest(t *testing.T, ctx context.Context) (*pgxpool.Pool, *Repository) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, repositorySeedSQL()); err != nil {
		t.Fatalf("seed synthetic fixture: %v", err)
	}
	return pool, NewRepository(pool, 5*time.Second)
}

// deliverStatusChanged hands the NEWEST sales.deal.status_changed outbox row for the deal to the
// identity consumer, exactly as a bus would.
func deliverStatusChanged(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository, dealID string) {
	t.Helper()
	var envelope []byte
	if err := pool.QueryRow(ctx, `
SELECT payload FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'sales.deal.status_changed' AND aggregate_id = $2::uuid
ORDER BY created_at DESC LIMIT 1`, ssTenant, dealID).Scan(&envelope); err != nil {
		t.Fatalf("read status_changed event: %v", err)
	}
	var env struct {
		EventID string          `json:"event_id"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(envelope, &env); err != nil {
		t.Fatal(err)
	}
	h := identityapp.NewSaleFailedReleaseHandler(repo)
	if err := h.HandleEvent(ctx, eventbus.Event{ID: env.EventID, Type: identityapp.EventSalesDealStatusChanged, TenantID: ssTenant,
		Key: dealID, Payload: env.Payload, OccurredAt: time.Date(2026, 8, 21, 6, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("release consumer: %v", err)
	}
}

type goatPlace struct {
	lifecycle, shed, partition string
	exitReason                 *string
	exited                     bool
}

func readGoatPlace(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) goatPlace {
	t.Helper()
	var p goatPlace
	if err := pool.QueryRow(ctx, `
SELECT g.lifecycle_status, g.shed_id::text, COALESCE(gsp.partition_label, ''), g.exit_reason, g.exited_at IS NOT NULL
FROM goats g LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
WHERE g.tenant_id = $1::uuid AND g.goat_id = $2::uuid`, ssTenant, goatID).Scan(&p.lifecycle, &p.shed, &p.partition, &p.exitReason, &p.exited); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAFailedSaleReleasesItsTaggedAnimalsBackIntoTheirPens(t *testing.T) {
	ctx := context.Background()
	pool, repo := startSaleWriteDBOnPgtest(t, ctx)
	f := seedShedStageFixture(t, ctx, pool)
	one := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	two := seedStageGoat(t, ctx, pool, f.castroShed, "2", "F2", "adult")
	before := map[string]goatPlace{one: readGoatPlace(t, ctx, pool, one), two: readGoatPlace(t, ctx, pool, two)}
	seedSaleAllocationDeal(t, ctx, pool, saleDealFailed, 2)
	if _, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealFailed, []ports.SaleAllocationRow{
		{GoatID: one, RowVersion: goatRowVersion(t, pool, one)},
		{GoatID: two, RowVersion: goatRowVersion(t, pool, two)},
	}, "tag-then-fail")); err != nil {
		t.Fatalf("tag: %v", err)
	}
	for _, id := range []string{one, two} {
		if p := readGoatPlace(t, ctx, pool, id); p.lifecycle != "sold" {
			t.Fatalf("precondition: tagged animal must be sold, got %+v", p)
		}
	}

	sales := salespg.NewRepository(pool, 15*time.Second)
	if _, err := sales.SetDealStatus(ctx, ssTenant, saleDealFailed, salesdomain.StatusDealFailed, ssActor); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	for i := 0; i < 2; i++ { // a redelivered event releases nothing twice
		deliverStatusChanged(t, ctx, pool, repo, saleDealFailed)
	}

	// Each animal is alive again, in the SAME pen, and only the exit was undone.
	for _, id := range []string{one, two} {
		got, was := readGoatPlace(t, ctx, pool, id), before[id]
		if got.lifecycle != "alive" || got.exitReason != nil || got.exited || got.shed != was.shed || got.partition != was.partition {
			t.Fatalf("released animal %s = %+v, want alive in %s / %q with no exit", id, got, was.shed, was.partition)
		}
	}
	// History kept: the allocations are released with who / when / why, never deleted.
	var released, tagged, withWho int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE status = 'released'), count(*) FILTER (WHERE status = 'tagged'),
       count(*) FILTER (WHERE status = 'released' AND released_at IS NOT NULL AND released_by = $3::uuid AND release_reason <> '')
FROM goat_sale_allocations WHERE tenant_id = $1::uuid AND sales_deal_id = $2::uuid`, ssTenant, saleDealFailed, ssActor).Scan(&released, &tagged, &withWho); err != nil {
		t.Fatal(err)
	}
	if released != 2 || tagged != 0 || withWho != 2 {
		t.Fatalf("allocations released=%d tagged=%d attributed=%d, want 2 / 0 / 2", released, tagged, withWho)
	}
	// Every sold-animal read counts status='tagged' only: the sale's own tagged list is empty.
	groups, err := repo.ListSaleAllocations(ctx, ssTenant, saleDealFailed)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("a failed sale must list no tagged animals, got %+v", groups)
	}
	// One goat.reinstated event, audit row and identity event per animal -- and not twice.
	var events, audits, identityEvents int
	if err := pool.QueryRow(ctx, `
SELECT (SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'goat.reinstated' AND aggregate_id = ANY($2::uuid[])),
       (SELECT count(*) FROM audit_log WHERE tenant_id = $1::uuid AND action = 'goat.reinstated' AND resource_id = ANY($2::uuid[])),
       (SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.reinstated' AND goat_id = ANY($2::uuid[]))`,
		ssTenant, []string{one, two}).Scan(&events, &audits, &identityEvents); err != nil {
		t.Fatal(err)
	}
	if events != 2 || audits != 2 || identityEvents != 2 {
		t.Fatalf("goat.reinstated outbox=%d audit=%d identity events=%d, want 2 each", events, audits, identityEvents)
	}
	// ONE goat.sale_released for the deal, with the pens the animals went back to (the Feed
	// Director's message), even though the event was delivered twice.
	var releasedEvents int
	var releasedPayload []byte
	if err := pool.QueryRow(ctx, `
SELECT count(*) OVER (), payload->'payload' FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'goat.sale_released' AND aggregate_id = $2::uuid
LIMIT 1`, ssTenant, saleDealFailed).Scan(&releasedEvents, &releasedPayload); err != nil {
		t.Fatalf("read goat.sale_released: %v", err)
	}
	var rel struct {
		Animals int    `json:"animals"`
		Buyer   string `json:"buyer_name"`
		Pens    []struct {
			Display string `json:"operational_location_display"`
			Code    string `json:"park_code"`
			Animals int    `json:"animals"`
		} `json:"pens"`
	}
	if err := json.Unmarshal(releasedPayload, &rel); err != nil {
		t.Fatal(err)
	}
	if releasedEvents != 1 || rel.Animals != 2 || rel.Buyer == "" || len(rel.Pens) != 2 || rel.Pens[0].Code == "" {
		t.Fatalf("goat.sale_released count=%d payload=%s, want one event, 2 animals over 2 park-coded pens and the buyer", releasedEvents, releasedPayload)
	}
	// The released animals can be sold again, on a NEW sale.
	seedSaleAllocationDeal(t, ctx, pool, saleDealB, 1)
	if _, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealB, []ports.SaleAllocationRow{
		{GoatID: one, RowVersion: goatRowVersion(t, pool, one)},
	}, "sell-again")); err != nil {
		t.Fatalf("a released animal must be taggable to a new sale: %v", err)
	}
}

func TestADealFailedIsFinalAndTakesNoAnimals(t *testing.T) {
	ctx := context.Background()
	pool, repo := startSaleWriteDBOnPgtest(t, ctx)
	f := seedShedStageFixture(t, ctx, pool)
	one := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	seedSaleAllocationDeal(t, ctx, pool, saleDealFailed, 1)

	sales := salespg.NewRepository(pool, 15*time.Second)
	for i := 0; i < 2; i++ { // the second is a no-op: no second event
		if _, err := sales.SetDealStatus(ctx, ssTenant, saleDealFailed, salesdomain.StatusDealFailed, ssActor); err != nil {
			t.Fatalf("mark failed (pass %d): %v", i+1, err)
		}
	}
	for _, next := range []string{salesdomain.StatusDealClosed, salesdomain.StatusInDiscussion, salesdomain.StatusAdvancePaid} {
		if _, err := sales.SetDealStatus(ctx, ssTenant, saleDealFailed, next, ssActor); !errors.Is(err, salesdomain.ErrDealFailedIsFinal) {
			t.Fatalf("moving a failed deal to %s must be refused as final, got %v", next, err)
		}
	}
	var status string
	var events int
	if err := pool.QueryRow(ctx, `
SELECT d.status, (SELECT count(*) FROM outbox_messages o WHERE o.tenant_id = d.tenant_id AND o.event_type = 'sales.deal.status_changed' AND o.aggregate_id = d.id)
FROM sales_deals d WHERE d.tenant_id = $1::uuid AND d.id = $2::uuid`, ssTenant, saleDealFailed).Scan(&status, &events); err != nil {
		t.Fatal(err)
	}
	if status != salesdomain.StatusDealFailed || events != 1 {
		t.Fatalf("failed deal status=%q events=%d, want Deal Failed and exactly one event", status, events)
	}
	// A failed sale with nothing tagged releases nothing and tells the Feed Director nothing.
	deliverStatusChanged(t, ctx, pool, repo, saleDealFailed)
	var released int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'goat.sale_released' AND aggregate_id = $2::uuid`,
		ssTenant, saleDealFailed).Scan(&released); err != nil {
		t.Fatal(err)
	}
	if released != 0 {
		t.Fatalf("a failed sale with no tagged animals must emit no goat.sale_released, got %d", released)
	}
	_, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealFailed, []ports.SaleAllocationRow{
		{GoatID: one, RowVersion: goatRowVersion(t, pool, one)},
	}, "tag-failed"))
	if !errors.Is(err, ports.ErrSaleDealFailed) {
		t.Fatalf("tagging onto a failed sale must be refused, got %v", err)
	}
	if got := goatLifecycle(t, ctx, pool, one); got != "alive" {
		t.Fatalf("a refused tagging must leave the animal in the herd, got %q", got)
	}
}
