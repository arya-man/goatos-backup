package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// TestSeedProtocolVersionsPassPublishContract is the drift guard: every rule_dsl / proof_policy
// literal the seed publishes must satisfy the real publish-time validators (ValidateRuleDSL +
// ValidateExecutionContract). Because the seed now publishes through the protocol service instead of a
// raw SQL status flip, invalid JSON would fail the publish at runtime; this test catches such drift in
// plain CI without a database.
func TestSeedProtocolVersionsPassPublishContract(t *testing.T) {
	if err := validateSeedProtocolVersions(); err != nil {
		t.Fatalf("seed protocol version literals must satisfy the publish contract: %v", err)
	}

	// Directly assert each version individually for clearer failure attribution.
	for _, sv := range seedProtocolVersions() {
		if err := protocolapp.ValidateRuleDSL([]byte(sv.ruleDSL)); err != nil {
			t.Fatalf("version %s rule_dsl invalid: %v", sv.versionID, err)
		}
		v := protocoldomain.Version{
			Category:     "vaccination",
			SopVersionID: localSOPVersionID,
			ProofPolicy:  []byte(sv.proofPolicy),
			RuleDsl:      []byte(sv.ruleDSL),
		}
		if err := protocolapp.ValidateExecutionContract(v); err != nil {
			t.Fatalf("version %s execution contract invalid: %v", sv.versionID, err)
		}
		if sv.finalStatus != "published" && sv.finalStatus != "retired" {
			t.Fatalf("version %s has unexpected finalStatus %q", sv.versionID, sv.finalStatus)
		}
	}
}

// TestSeedPublishEmitsOutboxEvent (Postgres, opt-in) proves the seed's publish path emits the durable
// protocol.version.published outbox message + audit log that vaccination generation consumes — the
// behavior the previous raw-SQL status flip bypassed. It seeds the minimal FK parents, inserts a draft
// via insertDraftProtocolVersion, publishes through the protocol service, and asserts the outbox row
// exists and the version is published.
func TestSeedPublishEmitsOutboxEvent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := defaultTenantID
	setupSeedTriggerFixture(t, ctx, pool, tenantID)

	sv := seedProtocolVersions()[1] // the "published" v1 fixture
	if err := insertDraftProtocolVersion(ctx, pool, tenantID, sv); err != nil {
		t.Fatalf("insert draft protocol version: %v", err)
	}

	service := protocolapp.NewService(protocolpg.NewRepository(pool, platformpg.Config{}.QueryTimeout))
	if err := service.PublishVersion(ctx, tenantID, sv.versionID, nil, "seed-vaccination-trigger-test:"+sv.versionID); err != nil {
		t.Fatalf("publish through protocol service: %v", err)
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM protocol_versions WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, sv.versionID).Scan(&status); err != nil {
		t.Fatalf("read published status: %v", err)
	}
	if status != "published" {
		t.Fatalf("version status = %q, want published", status)
	}

	var outboxCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='protocol.version.published' AND aggregate_id=$2::uuid`, tenantID, sv.versionID).Scan(&outboxCount); err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	if outboxCount == 0 {
		t.Fatalf("expected a protocol.version.published outbox message for %s, found none", sv.versionID)
	}

	// The publish path regenerates derived protocol_rules; the seed never hand-inserts them.
	var ruleCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM protocol_rules WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, sv.versionID).Scan(&ruleCount); err != nil {
		t.Fatalf("read protocol_rules: %v", err)
	}
	if ruleCount == 0 {
		t.Fatalf("expected publish to regenerate derived protocol_rules, found none")
	}
}

// setupSeedTriggerFixture creates the minimal FK parents the v1 seed protocol version needs: tenant,
// SOP definition + published version (matching localSOPVersionID), and the v1 protocol definition.
func setupSeedTriggerFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture exec failed: %v\nsql=%s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1,'seed-trigger-test','active') ON CONFLICT (tenant_id) DO NOTHING`, tenantID)

	sopID := "b0000000-0000-4000-8000-000000000001"
	exec(`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status) VALUES ($1,$2,'vaccination.trigger.seed','Vaccination Trigger Seed SOP','active') ON CONFLICT (sop_id) DO NOTHING`, sopID, tenantID)
	exec(`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy) VALUES ($1,$2,$3,1,'seed-v1','published','{}'::jsonb,'{}'::jsonb) ON CONFLICT (sop_version_id) DO NOTHING`, localSOPVersionID, tenantID, sopID)

	exec(`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status) VALUES ($1,$2,'vaccination.matrix.trigger_seed','Vaccination Rule Matrix Trigger Seed','vaccination','active') ON CONFLICT (protocol_id) DO NOTHING`, localV1ProtocolID, tenantID)
}
