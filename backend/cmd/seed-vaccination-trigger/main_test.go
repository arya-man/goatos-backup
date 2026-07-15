package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
)

// TestSeedProtocolVersionsPassPublishContract is the drift guard: every rule_dsl / proof_policy
// literal the seed publishes must satisfy the real publish-time validators (ValidateRuleDSL +
// ValidateExecutionContract). Because the seed now publishes through the protocol service instead of a
// raw SQL status flip, invalid JSON would fail the publish at runtime; this test catches such drift in
// plain CI without a database.
func TestSeedProtocolVersionsPassPublishContract(t *testing.T) {
	// validateSeedProtocolVersions runs ValidateRuleDSL + ValidateExecutionContract on every seed
	// version and returns errors tagged with the version ID, so a failure already points at the
	// offending fixture. Here we only additionally guard the finalStatus vocabulary.
	if err := validateSeedProtocolVersions(); err != nil {
		t.Fatalf("seed protocol version literals must satisfy the publish contract: %v", err)
	}
	for _, sv := range seedProtocolVersions() {
		if sv.finalStatus != "published" && sv.finalStatus != "retired" {
			t.Fatalf("version %s has unexpected finalStatus %q", sv.versionID, sv.finalStatus)
		}
	}
}

// TestSeedPublishEmitsOutboxEvent (Postgres, opt-in) proves the seed's publish path emits the durable
// protocol.version.published outbox message + audit log that vaccination generation consumes — the
// behavior the previous raw-SQL status flip bypassed. It seeds the minimal FK parents, inserts a draft
// via upsertDraftProtocolVersion, publishes through the protocol service, and asserts the outbox row
// exists and the version is published.
func TestSeedPublishEmitsOutboxEvent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := defaultTenantID
	setupSeedTriggerFixture(t, ctx, pool, tenantID)

	sv := seedProtocolVersions()[1] // the "published" v1 fixture
	isDraft, err := upsertDraftProtocolVersion(ctx, pool, tenantID, sv)
	if err != nil {
		t.Fatalf("upsert draft protocol version: %v", err)
	}
	if !isDraft {
		t.Fatalf("first upsert of a fresh version must report a draft row")
	}

	service := protocolapp.NewService(protocolpg.NewRepository(pool, platformpg.Config{}.QueryTimeout))
	if err := service.PublishVersion(ctx, tenantID, sv.versionID, nil, "seed-vaccination-trigger-test:"+sv.versionID); err != nil {
		t.Fatalf("publish through protocol service: %v", err)
	}

	// Re-running the upsert after publish must report the version is no longer a draft, so the
	// caller skips PublishVersion (which would otherwise fail with ErrVersionNotDraft on re-run).
	isDraft, err = upsertDraftProtocolVersion(ctx, pool, tenantID, sv)
	if err != nil {
		t.Fatalf("re-upsert already-published version: %v", err)
	}
	if isDraft {
		t.Fatalf("re-upsert of an already-published version must report isDraft=false")
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
