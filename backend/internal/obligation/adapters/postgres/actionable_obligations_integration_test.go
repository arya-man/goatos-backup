package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// TestListActionableObligationsForGoatsFiltersTerminalAndKeysByGoat is the R2-01 / NEW-01 SQL guard:
// the bulk loader returns only ACTIONABLE (non-terminal) obligations, keyed goatID -> idempotencyKey,
// with the persisted due_at -- one query for a page of goats. Terminal (completed) obligations and
// other goats' rows are excluded.
func TestListActionableObligationsForGoatsFiltersTerminalAndKeysByGoat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protocolpg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protocoldomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.actionable", Name: "Actionable", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protocoldomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	repo := NewRepository(pool, 5*time.Second)

	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4), ($5, $2, 'alive', 'goat', $3, 'female', $4, $4)`,
		testGoatID, tenantID, meshaParty, cbePark, "10000000-0000-4000-8000-0000000000bb"); err != nil {
		t.Fatalf("seed goats: %v", err)
	}

	scheduledDue := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	insert := func(goatID, key, status string, due time.Time) {
		if _, _, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: status, IdempotencyKey: key, Sequence: 1,
		}); err != nil {
			t.Fatalf("insert %s: %v", key, err)
		}
	}
	insert(testGoatID, "act-scheduled", "scheduled", scheduledDue)
	insert(testGoatID, "act-completed", "completed", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	insert("10000000-0000-4000-8000-0000000000bb", "act-other-goat", "scheduled", scheduledDue)

	got, err := repo.ListActionableObligationsForGoats(ctx, tenantID, []string{testGoatID})
	if err != nil {
		t.Fatalf("ListActionableObligationsForGoats: %v", err)
	}
	byKey := got[testGoatID]
	if len(byKey) != 1 {
		t.Fatalf("actionable for goat = %#v, want exactly the scheduled obligation", byKey)
	}
	ref, ok := byKey["act-scheduled"]
	if !ok {
		t.Fatalf("missing act-scheduled; got keys %#v", byKey)
	}
	if !ref.DueAt.Equal(scheduledDue) {
		t.Fatalf("due = %s, want persisted %s", ref.DueAt, scheduledDue)
	}
	if _, leaked := byKey["act-completed"]; leaked {
		t.Fatalf("terminal (completed) obligation must be excluded")
	}
	if _, leaked := got["10000000-0000-4000-8000-0000000000bb"]; leaked {
		t.Fatalf("another goat's obligation must not appear when it was not requested")
	}
}
