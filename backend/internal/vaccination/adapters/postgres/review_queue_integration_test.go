package postgres

import (
	"context"
	"testing"
	"time"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// TestListRecordedCompletionsQueue checks the Verification queue read: only still-recorded
// completions, earliest administered first, accepted ones excluded.
func TestListRecordedCompletionsQueue(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.queue", Name: "Queue", Category: "vaccination", Status: "draft",
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

	const g = "30000000-0000-4000-8000-0000000000a9"
	seedGenGoat(t, ctx, pool, g, "alive")

	doses := int32(1)
	mk := func(key string, administered time.Time) string {
		obID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: g, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: administered, Status: "scheduled", IdempotencyKey: "q-" + key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("obligation %s: applied=%v err=%v", key, applied, err)
		}
		cid, applied, err := vacc.RecordCompletion(ctx, vaccdomain.NewCompletion{
			TenantID: impTenant, ObligationID: obID, GoatID: g, Doses: &doses, RouteSite: "SC",
			AdministeredAt: administered, Status: "recorded", IdempotencyKey: "qc-" + key,
		})
		if err != nil || !applied {
			t.Fatalf("record %s: applied=%v err=%v", key, applied, err)
		}
		return cid
	}
	cidEarly := mk("early", time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC))
	cidLate := mk("late", time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC))
	cidAccepted := mk("accepted", time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC))
	if _, applied, err := vacc.AcceptCompletion(ctx, impTenant, cidAccepted, nil, nil); err != nil || !applied {
		t.Fatalf("accept: applied=%v err=%v", applied, err)
	}

	queue, err := vacc.ListRecordedCompletions(ctx, impTenant, 100)
	if err != nil {
		t.Fatalf("list recorded: %v", err)
	}
	if len(queue) != 2 {
		t.Fatalf("want 2 recorded (accepted excluded), got %d", len(queue))
	}
	if queue[0].CompletionID != cidEarly || queue[1].CompletionID != cidLate {
		t.Fatalf("want earliest administered first [%s,%s], got [%s,%s]", cidEarly, cidLate, queue[0].CompletionID, queue[1].CompletionID)
	}
}
