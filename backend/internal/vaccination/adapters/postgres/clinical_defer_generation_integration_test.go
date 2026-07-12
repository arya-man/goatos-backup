package postgres

import (
	"context"
	"testing"
	"time"

	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
)

// C35-010 real-Postgres proof: a published vaccination version whose authored
// eligibility.defer_states is PARTIAL (only icu + quarantine, omitting the
// mandatory clinical safety states sick + under_treatment) must still DEFER a
// sick or under-treatment animal's obligation through the production generation
// service and Postgres persistence — never leave it scheduled/cancelled.
//
// Before the fix, deferStateSet honored only the authored list, so sick and
// under_treatment animals were persisted as "scheduled" (or, under a
// health-scoped rule, excluded entirely) — the wrong medical workflow state.
// After the fix, the engine unions in the mandatory clinical set regardless of
// the authored list, so all four clinical states persist as status='deferred'.
func TestGenerationDefersClinicalStatesUnderPartialAuthoring(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.partialdefer", Name: "PartialDefer", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	// PARTIAL authored defer_states: icu + quarantine only. sick and
	// under_treatment are deliberately omitted to reproduce the authoring gap.
	ruleDSL := []byte(`{"eligibility":{"animal_stage":"K1","defer_states":["icu","quarantine"]},` +
		`"source":{"source_system":"pc","source_ref":"PC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 21, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// One healthy (alive) plus one of each clinical state. The candidate query
	// surfaces all of them (lifecycle_status IN alive/sick/under_treatment/
	// quarantine/icu); the generator decides scheduled vs deferred.
	const (
		aliveID   = "30000000-0000-4000-8000-0000000000c1"
		sickID    = "30000000-0000-4000-8000-0000000000c2"
		underID   = "30000000-0000-4000-8000-0000000000c3"
		quarantID = "30000000-0000-4000-8000-0000000000c4"
	)
	seedGenGoat(t, ctx, pool, aliveID, "alive")
	seedGenGoat(t, ctx, pool, sickID, "sick")
	seedGenGoat(t, ctx, pool, underID, "under_treatment")
	seedGenGoat(t, ctx, pool, quarantID, "quarantine")

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)

	res, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 4 {
		t.Fatalf("generated: want 4 obligations, got %d", res.Generated)
	}
	// The union of the mandatory clinical set means sick + under_treatment +
	// quarantine are all deferred (icu was authored; the other three are
	// enforced). Before the fix this was 1 (quarantine only).
	if res.Deferred != 3 {
		t.Fatalf("deferred: want 3 (sick, under_treatment, quarantine), got %d — a sick/under_treatment animal is NOT being held for recovery", res.Deferred)
	}

	// Per-animal persisted status: the sick and under-treatment animals must be
	// held in 'deferred', not left 'scheduled'.
	for _, id := range []string{sickID, underID, quarantID} {
		if got := countRowsVacc(t, ctx, pool,
			`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'`,
			impTenant, id); got != 1 {
			t.Fatalf("expected 1 deferred obligation for clinically-blocked goat %s, got %d", id, got)
		}
		// A deferred ledger event must exist for the audit/as-of trail.
		if got := countRowsVacc(t, ctx, pool,
			`SELECT count(*) FROM obligation_status_events e
			 JOIN obligation_instances o ON o.tenant_id=e.tenant_id AND o.obligation_id=e.obligation_id
			 WHERE e.tenant_id=$1 AND e.event_type='deferred' AND o.target_id=$2`,
			impTenant, id); got != 1 {
			t.Fatalf("expected 1 deferred event for clinically-blocked goat %s, got %d", id, got)
		}
	}

	// The healthy animal is scheduled, not deferred.
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`,
		impTenant, aliveID); got != 1 {
		t.Fatalf("expected the healthy goat's obligation to be scheduled, got %d scheduled rows", got)
	}
}
