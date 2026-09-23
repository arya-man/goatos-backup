package chainproof

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	appg "github.com/vgoats/goatos/backend/internal/animalpurchase/adapters/postgres"
	apdomain "github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	apports "github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
	"github.com/vgoats/goatos/backend/internal/platform/chaintest"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// TestAnimalPurchaseIntakeChain proves THREE registered chains in the order a purchase actually
// happens, because they share one workflow and cannot be judged apart:
//
//	procurement.animal_purchase.load_recorded      -> the load's intake workflow opens
//	procurement.animal_purchase.candidate_recorded -> its decision step counts work still pending
//	procurement.animal_purchase.decided            -> the step completes once nothing is pending
//
// candidate_recorded's registered proofs are an envelope test on the producer side and a
// consumer-side test that calls ReconcileAnimalPurchaseDecisionStep directly, so neither can see
// the event stop flowing. The decision step is the interesting one: it is not a simple "done"
// flag but a MONOTONIC RECEIPT (pending + 2*decided), so a candidate recorded AFTER the office has
// decided the others must REOPEN it. That reopening is the behaviour a no-op'd handler silently
// loses, and a producer-side outbox count cannot see at all.
//
// Which assertion catches which mutation:
//
//	drop the load emission       -> DrainExpecting finds no ...load_recorded
//	no-op the load handler       -> no intake workflow for the load
//	drop the candidate emission  -> DrainExpecting finds no ...candidate_recorded
//	no-op the decision handler   -> the decision step never completes after both decisions
//	drop the decided emission    -> DrainExpecting finds no ...decided
//
// Fixtures seed only external inputs: tenant, park, vendor, and the buyer's workforce row. The
// load, the candidates, the outbox envelopes and the workflow all come from production code.
func TestAnimalPurchaseIntakeChain(t *testing.T) {
	pool, ctx := newChainDB(t)
	seedPark(t, ctx, pool, "CPT", "Channapatna")
	const buyer = "7c0f1a2b-0000-4000-8000-0000000000b1"
	const buyerUser = "7c0f1a2b-0000-4000-8000-0000000000b2"
	var vendorID string
	if err := pool.QueryRow(ctx, `
INSERT INTO procurement_vendors (tenant_id, record_type, business_name, status, state)
VALUES ($1::uuid, 'Livestock Agent', 'Ramesh Traders', 'active', 'Karnataka') RETURNING vendor_id::text`,
		chainTenant).Scan(&vendorID); err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Ravi', 'Ravi', 'active', 'park_head') ON CONFLICT DO NOTHING`,
		buyer, chainTenant, buyerUser); err != nil {
		t.Fatalf("seed buyer: %v", err)
	}

	bus := workflowBus(t, pool)
	repo := appg.NewRepository(pool, 10*time.Second)

	// PRODUCER 1: the office records the load.
	load, err := repo.CreateLoad(ctx, apports.CreateLoadParams{
		TenantID: chainTenant, ActorID: buyerUser, IdempotencyKey: "chain-load",
		Write: apdomain.LoadWrite{LoadRef: "CHAIN-1", VendorID: vendorID, FarmLabel: "CPT", ExpectedCount: 2},
	})
	if err != nil {
		t.Fatalf("record the load: %v", err)
	}
	chaintest.DrainExpecting(t, ctx, pool, bus, chainTenant, "procurement.animal_purchase.load_recorded")

	var workflowID string
	if err := pool.QueryRow(ctx, `
SELECT workflow_id::text FROM workflow_instances
WHERE tenant_id = $1::uuid AND subject_ref_id = $2 AND template_key = $3`,
		chainTenant, load.LoadID, tasksdomain.TemplateKeyAnimalPurchaseIntake).Scan(&workflowID); err != nil {
		t.Fatalf("the recorded load opened no intake workflow: %v", err)
	}

	// PRODUCER 2: the inspector records two animals on the load.
	add := func(key, breed, ref string) apdomain.Candidate {
		t.Helper()
		c, err := repo.AddCandidate(ctx, apports.AddCandidateParams{
			TenantID: chainTenant, LoadID: load.LoadID, ActorID: buyerUser,
			QuestionnaireVersion: apdomain.QuestionnaireVersion, IdempotencyKey: key,
			Write: chainInspection(breed, ref),
		})
		if err != nil {
			t.Fatalf("record candidate %s: %v", key, err)
		}
		return c
	}
	first := add("chain-animal-1", "B1", "7c0f1a2b-0000-4000-8000-0000000000f1")
	second := add("chain-animal-2", "B2", "7c0f1a2b-0000-4000-8000-0000000000f2")

	chaintest.DrainExpecting(t, ctx, pool, bus, chainTenant, "procurement.animal_purchase.candidate_recorded")

	// CONSUMER state: the receipt names two animals waiting on the office, and the step is NOT done.
	if pending, decided := decisionReceipt(t, ctx, pool, load.LoadID); pending != 2 || decided != 0 {
		t.Fatalf("decision receipt = pending %d decided %d; want 2 and 0", pending, decided)
	}
	if status := hookActionStatus(t, ctx, pool, workflowID, string(tasksdomain.EngineHookAnimalPurchaseDecision)); status == "completed" {
		t.Fatalf("the decision step is complete while two animals are still undecided")
	}

	// PRODUCER 3: the office decides both.
	decide := func(c apdomain.Candidate, key, verdict string) {
		t.Helper()
		// The service normalizes before the repository is reached; this test drives the
		// repository directly, exactly as the animalpurchase package's own tests do.
		write := apdomain.DecisionWrite{Decision: verdict, RowVersion: c.RowVersion}
		write.Normalize()
		if _, err := repo.Decide(ctx, apports.DecideParams{
			TenantID: chainTenant, CandidateID: c.CandidateID, ActorID: buyerUser, ActorName: "Ravi",
			IdempotencyKey: key,
			Write:          write,
		}); err != nil {
			t.Fatalf("decide %s: %v", key, err)
		}
	}
	decide(first, "chain-decide-1", "accept")
	decide(second, "chain-decide-2", "reject")

	chaintest.DrainExpecting(t, ctx, pool, bus, chainTenant, "procurement.animal_purchase.decided")

	if pending, decided := decisionReceipt(t, ctx, pool, load.LoadID); pending != 0 || decided != 2 {
		t.Fatalf("decision receipt = pending %d decided %d; want 0 and 2", pending, decided)
	}
	if status := hookActionStatus(t, ctx, pool, workflowID, string(tasksdomain.EngineHookAnimalPurchaseDecision)); status != "completed" {
		t.Fatalf("the decision step is %q after every animal was decided; want completed", status)
	}

	// A LATE candidate REOPENS the step. This is the property the receipt exists for, and the one
	// a producer-side outbox count can never see: the office is not finished after all.
	add("chain-animal-3", "B3", "7c0f1a2b-0000-4000-8000-0000000000f3")
	chaintest.DrainExpecting(t, ctx, pool, bus, chainTenant, "procurement.animal_purchase.candidate_recorded")
	if pending, decided := decisionReceipt(t, ctx, pool, load.LoadID); pending != 1 || decided != 2 {
		t.Fatalf("after a late animal the receipt = pending %d decided %d; want 1 and 2", pending, decided)
	}
	if status := hookActionStatus(t, ctx, pool, workflowID, string(tasksdomain.EngineHookAnimalPurchaseDecision)); status == "completed" {
		t.Fatalf("a late animal left the decision step complete; the office still owes a decision")
	}
}

func decisionReceipt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, loadID string) (int, int) {
	t.Helper()
	var pending, decided int
	if err := pool.QueryRow(ctx, `
SELECT pending, decided FROM workflow_animal_purchase_decisions
WHERE tenant_id = $1::uuid AND load_id = $2::uuid`, chainTenant, loadID).Scan(&pending, &decided); err != nil {
		t.Fatalf("read the decision receipt: %v", err)
	}
	return pending, decided
}

func hookActionStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowID, hook string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `
SELECT status FROM workflow_actions
WHERE tenant_id = $1::uuid AND workflow_id = $2::uuid AND engine_hook = $3`,
		chainTenant, workflowID, hook).Scan(&status); err != nil {
		t.Fatalf("read the %s step: %v", hook, err)
	}
	return status
}

// chainInspection is the minimal complete animal-purchase questionnaire the production validator
// accepts. It mirrors the animalpurchase package's own `inspection` fixture.
func chainInspection(breed, animalRef string) apdomain.CandidateWrite {
	j := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	a := apdomain.Answers{
		"species": j("goat"), "goat_id": j("GW-" + breed), "well_fed": j("yes"), "teeth": j(4), "sex": j("female"),
		"weight_kg": j(20.0), "rectal_temp_c": j(39.0),
		"anaemic": j("no"), "mouth_breathing": j("no"), "watery_eyes": j("no"), "eye_colour": j("no"), "nasal_discharge": j("no"),
		"face_scabs": j("no"), "acidosis": j("no"), "diarrhea": j("no"), "ticks_hair_loss": j("no"), "wounds": j("no"),
		"body_scabs": j("no"), "lumps": j("no"), "arthritis": j("no"), "udder_state": j([]string{"normal"}),
		"field_verdict": j("selected"), "breed": j(breed),
		"pregnant": j("no"), "lactating": j("no"), "teats": j("2"), "teat_discharge": j("no"),
	}
	w := apdomain.CandidateWrite{Catalog: apdomain.SeededCatalog(), Answers: a, Media: apdomain.MediaRefs{
		apdomain.SlotTeeth: {"teeth-" + animalRef}, apdomain.SlotAnimal: {animalRef}, apdomain.SlotUdder: {"udder-" + animalRef},
	}}
	w.Normalize()
	return w
}
