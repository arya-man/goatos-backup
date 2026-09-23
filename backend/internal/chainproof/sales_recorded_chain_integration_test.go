package chainproof

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/chaintest"
	salespg "github.com/vgoats/goatos/backend/internal/sales/adapters/postgres"
	salesdomain "github.com/vgoats/goatos/backend/internal/sales/domain"
	taskspg "github.com/vgoats/goatos/backend/internal/tasks/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// TestSaleRecordedChainOpensTheWorkflow proves the sales.deal.recorded CHAIN end to end --
// sales.Repository.CreateDeal -> outbox_messages -> the bus eventwiring.RegisterWorkflowConsumers
// wires -> tasks/app.SaleRecordedWorkflowHandler -> the sale's workflow.
//
// It exists because the chain's only registered proof,
// TestSaleWorkflowRunsTheSalesSOP, calls repo.OpenWorkflow directly -- one layer BELOW the
// handler and two below the producer. That test is a good test of the SOP's compiled steps, and
// it stays; what it cannot see is whether the event still flows. Deleting emitSaleRecorded, or
// gutting SaleRecordedWorkflowHandler.HandleEvent, leaves it green while a recorded sale silently
// opens no work at all.
//
// Both mutations are caught HERE, and by different assertions:
//
//	delete the emission   -> DrainExpecting finds no sales.deal.recorded
//	no-op the handler     -> WorkflowIDBySubjectRef finds no workflow for the deal
//
// Fixtures seed only external inputs -- the tenant, the park the farm code resolves to, and the
// vendor-free sale body a caller supplies. The deal row, the outbox envelope and the workflow are
// all produced by the same code production runs.
func TestSaleRecordedChainOpensTheWorkflow(t *testing.T) {
	pool, ctx := newChainDB(t)
	repo := taskspg.NewRepository(pool, 0)

	// External input: the park the producer resolves 'CBE' to, so the opened workflow is
	// park-scoped the way a real sale's is.
	seedPark(t, ctx, pool, "CBE", "Coimbatore")

	// The production bus, wired by the ONE registration every bus process calls.
	bus := workflowBus(t, pool)

	// PRODUCER, through its real service path. Nothing about the workflow is seeded.
	sales := salespg.NewRepository(pool, 0)
	write := salesdomain.DealWrite{
		SaleDate: "2026-07-27", Farm: "CBE", ProductType: "Goat", Breed: "Malai",
		BuyerName: "Kumar Traders", SalesValue: 150000, AnimalCount: f64(12), Status: "Deal Closed",
	}
	deal, err := sales.CreateDeal(ctx, chainTenant, write.Normalize(), "", "chain-sale-1")
	if err != nil {
		t.Fatalf("record the sale: %v", err)
	}

	// The chain itself: the rows the sale's own transaction committed, decoded and dispatched the
	// way the relay does. Red if the producer stopped emitting.
	chaintest.DrainExpecting(t, ctx, pool, bus, chainTenant, "sales.deal.recorded")

	// CONSUMER state. Red if the handler became a no-op.
	workflowID, err := repo.WorkflowIDBySubjectRef(ctx, chainTenant, domain.TemplateKeySalesDeal, deal.DealID)
	if err != nil {
		t.Fatalf("the recorded sale opened no workflow: %v", err)
	}
	detail, err := repo.GetWorkflow(ctx, chainTenant, workflowID, chainEventAt)
	if err != nil {
		t.Fatalf("read the opened workflow: %v", err)
	}
	if detail.Card.SubjectRefID != deal.DealID || detail.Card.Module != domain.ModuleSales {
		t.Fatalf("workflow = ref %q module %s; want the recorded deal under sales", detail.Card.SubjectRefID, detail.Card.Module)
	}
	// The card reads back the SALE's own facts, which only the real producer payload can carry.
	if detail.Card.SubjectLabel != "Kumar Traders · 12 animals · CBE" {
		t.Fatalf("subject label = %q", detail.Card.SubjectLabel)
	}

	// A redelivered event opens nothing: the handler is idempotent on the deal.
	if _, err := pool.Exec(ctx, `UPDATE outbox_messages SET status = 'pending' WHERE tenant_id = $1::uuid AND event_type = 'sales.deal.recorded'`, chainTenant); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	chaintest.DrainExpecting(t, ctx, pool, bus, chainTenant, "sales.deal.recorded")
	var workflows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_instances WHERE tenant_id = $1::uuid AND subject_ref_id = $2`, chainTenant, deal.DealID).Scan(&workflows); err != nil {
		t.Fatalf("count workflows: %v", err)
	}
	if workflows != 1 {
		t.Fatalf("a redelivery opened %d workflows for one sale; want 1", workflows)
	}
}

