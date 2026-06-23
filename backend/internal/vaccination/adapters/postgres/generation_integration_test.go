package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

func seedGenGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, lifecycle string) {
	t.Helper()
	_, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id,
		   current_location_id, park_id, management_stage, dob)
		 VALUES ($1, $2, $3, 'clean', $4, $5, $5, 'K1', DATE '2026-05-01')`,
		id, impTenant, lifecycle, impParty, impCbe)
	if err != nil {
		t.Fatalf("seed gen goat %s: %v", id, err)
	}
}

func TestSM1GenerationIdempotentAndDeferVisible(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	// Published vaccination version: K1 eligibility, defer ICU/quarantine/sick, one birth_age rule.
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.gen", Name: "Gen", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"stage":"K1","defer_states":["ICU","quarantine","sick"]},` +
		`"source":{"source_system":"phc","source_ref":"PHC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 21, Repeat: "none", CatchUp: "phc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// 2 alive + 1 quarantine (defer) K1 goats.
	seedGenGoat(t, ctx, pool, "30000000-0000-4000-8000-0000000000a1", "alive")
	seedGenGoat(t, ctx, pool, "30000000-0000-4000-8000-0000000000a2", "alive")
	seedGenGoat(t, ctx, pool, "30000000-0000-4000-8000-0000000000d1", "quarantine")

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)

	res, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if res.Generated != 3 {
		t.Fatalf("generated: want 3, got %d", res.Generated)
	}
	if res.Deferred != 1 {
		t.Fatalf("deferred: want 1 (quarantine goat), got %d", res.Deferred)
	}

	// Idempotent re-run: no new obligations.
	res2, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if res2.Generated != 0 {
		t.Fatalf("re-gen should be a no-op, generated %d", res2.Generated)
	}

	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=$2`, impTenant, versionID); got != 3 {
		t.Fatalf("expected 3 obligations, got %d", got)
	}
	// Defer is visible: a 'deferred' status event exists for the quarantine goat's obligation.
	if got := countRowsVacc(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events e
		 JOIN obligation_instances o ON o.tenant_id=e.tenant_id AND o.obligation_id=e.obligation_id
		 WHERE e.tenant_id=$1 AND e.event_type='deferred' AND o.target_id=$2`,
		impTenant, "30000000-0000-4000-8000-0000000000d1"); got != 1 {
		t.Fatalf("expected 1 deferred event for quarantine goat, got %d", got)
	}
}

func countRowsVacc(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}
