package chainproof

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	consumerwiring "github.com/vgoats/goatos/backend/internal/domainconsumer/wiring"
	"github.com/vgoats/goatos/backend/internal/platform/chaintest"
	procpg "github.com/vgoats/goatos/backend/internal/procurement/adapters/postgres"
	procdomain "github.com/vgoats/goatos/backend/internal/procurement/domain"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// TestFeedPurchaseReachedChain proves two registered chains, and it drives them through
// domainconsumer/wiring.BuildDomainBus -- the PRODUCTION bus builder, not a hand-picked set of
// handlers -- so a consumer that is registered nowhere fails here too:
//
//	procurement.feed_purchase.recorded -> the feed load's intake workflow opens
//	procurement.feed_purchase.reached  -> the load owes an aflatoxin strip test, and the
//	                                      workflow's "reached" step completes
//
// The toxin half could not previously be proven by any E2E at all: FeedPurchaseReachedHandler was
// registered on three of the five bus compositions and MISSING from this builder, which this
// file's own comment calls out as the bus the story fixture relays through. That omission is
// fixed in the same change; this test is what keeps it fixed.
//
// Which assertion catches which mutation:
//
//	drop the recorded emission -> DrainExpecting finds no ...feed_purchase.recorded
//	no-op the intake opener    -> no intake workflow for the purchase
//	drop the reached emission  -> DrainExpecting finds no ...feed_purchase.reached
//	no-op the toxin handler    -> the reached load owes no toxin test
//	no-op the reached step     -> the workflow's reached step never completes
//
// Fixtures seed only external inputs: the tenant, its parks, and the feed catalog the farm
// authored. The purchase, the envelopes, the workflow and the toxin task are production output.
func TestFeedPurchaseReachedChain(t *testing.T) {
	pool, ctx := newChainDB(t)
	seedPark(t, ctx, pool, "CPT", "Channapatna")
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_item_catalog (tenant_id, feed_item_label, display_order, status)
VALUES ($1::uuid, 'Dry Sorghum Forage', 1, 'active')
ON CONFLICT (tenant_id, feed_item_key) DO NOTHING`, chainTenant); err != nil {
		t.Fatalf("seed feed catalog: %v", err)
	}

	bus := consumerwiring.BuildDomainBus(pool, 10*time.Second, slog.Default())
	repo := procpg.NewRepository(pool, 10*time.Second)

	// PRODUCER 1: the desk records the purchase. Not yet arrived, so it owes no test yet.
	write := procdomain.FeedPurchaseWrite{
		PurchaseDate: "2026-08-20", FarmLabel: procdomain.FeedFarmCPT,
		FeedItemLabel: "Dry Sorghum Forage", QuantityKg: 5420,
		FeedCost: f64(48980), TransportCost: f64(28000),
		Vendor: "Siddi Srilekha", PaymentStatus: procdomain.FeedPaymentPaid,
	}
	write.Normalize()
	purchase, err := repo.CreateFeedPurchase(ctx, chainTenant, write, "", "chain-feed-1")
	if err != nil {
		t.Fatalf("record the feed purchase: %v", err)
	}
	chaintest.DrainExpecting(t, ctx, pool, bus, chainTenant, "procurement.feed_purchase.recorded")

	var workflowID string
	if err := pool.QueryRow(ctx, `
SELECT workflow_id::text FROM workflow_instances
WHERE tenant_id = $1::uuid AND subject_ref_id = $2 AND template_key = $3`,
		chainTenant, purchase.FeedPurchaseID, tasksdomain.TemplateKeyFeedPurchaseIntake).Scan(&workflowID); err != nil {
		t.Fatalf("the recorded purchase opened no intake workflow: %v", err)
	}
	if n := toxinRounds(t, ctx, pool, purchase.FeedPurchaseID); n != 0 {
		t.Fatalf("a purchase that has not ARRIVED already owes %d toxin test(s); the test is born on arrival", n)
	}

	// PRODUCER 2: the load reaches the farm.
	delivery := procdomain.FeedPurchaseDeliveryWrite{ReachedOn: "2026-08-24", ReachedWeightKg: f64(5400)}
	delivery.Normalize()
	if _, err := repo.RecordFeedPurchaseDelivery(ctx, chainTenant, purchase.FeedPurchaseID, delivery, ""); err != nil {
		t.Fatalf("mark the load reached: %v", err)
	}
	chaintest.DrainExpecting(t, ctx, pool, bus, chainTenant, "procurement.feed_purchase.reached")

	// CONSUMER 1: the arrived load owes exactly one live aflatoxin round.
	if n := toxinRounds(t, ctx, pool, purchase.FeedPurchaseID); n != 1 {
		t.Fatalf("the reached load owes %d toxin test rounds; want exactly 1", n)
	}
	// CONSUMER 2: the intake workflow's reached step is done.
	if status := hookActionStatus(t, ctx, pool, workflowID, string(tasksdomain.EngineHookFeedPurchaseReached)); status != "completed" {
		t.Fatalf("the intake workflow's reached step is %q after the load arrived; want completed", status)
	}

	// A redelivery mints no second round: the task is keyed on (purchase, round_no).
	if _, err := pool.Exec(ctx, `
UPDATE outbox_messages SET status = 'pending'
WHERE tenant_id = $1::uuid AND event_type = 'procurement.feed_purchase.reached'`, chainTenant); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	chaintest.DrainExpecting(t, ctx, pool, bus, chainTenant, "procurement.feed_purchase.reached")
	if n := toxinRounds(t, ctx, pool, purchase.FeedPurchaseID); n != 1 {
		t.Fatalf("a redelivery left the load owing %d toxin rounds; want 1", n)
	}
}

func toxinRounds(t *testing.T, ctx context.Context, pool *pgxpool.Pool, purchaseID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM toxin_test_tasks WHERE tenant_id = $1::uuid AND feed_purchase_id = $2::uuid`,
		chainTenant, purchaseID).Scan(&n); err != nil {
		t.Fatalf("count toxin rounds: %v", err)
	}
	return n
}
