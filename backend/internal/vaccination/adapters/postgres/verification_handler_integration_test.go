package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// TestVerificationHandlerRoutesViaBus checks the verify event seam: an accepted event completes the
// obligation, a rejected event leaves its obligation open. No drive/stock — pure routing + outcome.
func TestVerificationHandlerRoutesViaBus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.vh", Name: "VH", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "phc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	const ga = "30000000-0000-4000-8000-0000000000a7"
	const gb = "30000000-0000-4000-8000-0000000000b7"
	seedGenGoat(t, ctx, pool, ga, "alive")
	seedGenGoat(t, ctx, pool, gb, "alive")

	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	mkOblAndCompletion := func(goat, key string) (string, string) {
		obID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goat, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: asOf, Status: "scheduled", IdempotencyKey: "vh-" + key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert obligation %s: applied=%v err=%v", goat, applied, err)
		}
		cid, applied, err := vacc.RecordCompletion(ctx, vaccdomain.NewCompletion{
			TenantID: impTenant, ObligationID: obID, GoatID: goat, RouteSite: "SC",
			AdministeredAt: asOf, Status: "recorded", IdempotencyKey: "vhc-" + key,
		})
		if err != nil || !applied {
			t.Fatalf("record %s: applied=%v err=%v", goat, applied, err)
		}
		return obID, cid
	}
	obA, cidA := mkOblAndCompletion(ga, "a")
	obB, cidB := mkOblAndCompletion(gb, "b")

	completion := vaccapp.NewCompletionService(vaccapp.NewService(vacc), obl, nil)
	bus := eventbus.NewInProcessBus()
	vaccapp.NewVerificationHandler(completion).Register(bus)

	acc, _ := json.Marshal(vaccapp.VerificationEvent{CompletionID: cidA})
	if err := bus.Publish(ctx, eventbus.Event{Type: vaccapp.EventVaccinationVerifyAccepted, TenantID: impTenant, Payload: acc}); err != nil {
		t.Fatalf("publish accepted: %v", err)
	}
	rej, _ := json.Marshal(vaccapp.VerificationEvent{CompletionID: cidB, Reason: "blurry"})
	if err := bus.Publish(ctx, eventbus.Event{Type: vaccapp.EventVaccinationVerifyRejected, TenantID: impTenant, Payload: rej}); err != nil {
		t.Fatalf("publish rejected: %v", err)
	}

	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, obA); got != "completed" {
		t.Fatalf("obA via accepted event: want completed, got %s", got)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM vaccination_completions WHERE tenant_id=$1 AND completion_id=$2`, impTenant, cidA); got != "accepted" {
		t.Fatalf("completion A: want accepted, got %s", got)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, impTenant, obB); got == "completed" {
		t.Fatalf("obB via rejected event must stay open, got %s", got)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM vaccination_completions WHERE tenant_id=$1 AND completion_id=$2`, impTenant, cidB); got != "rejected" {
		t.Fatalf("completion B: want rejected, got %s", got)
	}
}
