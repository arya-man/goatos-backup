package main

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
)

// seedMatrixProtocolID mirrors the deterministic id the seed resolves for its canonical protocol.
func seedMatrixProtocolID(tenantID string) string {
	return detUUID("protocol", tenantID, "vaccination_matrix")
}

// TestSeedRefusesForeignPublishedVaccinationMatrix covers the ownership-safety guard (VACC-REV):
// PublishVersion's overlap-retire clears every overlapping published vaccination matrix at a scope
// regardless of which protocol authored it, so the seed must refuse when a FOREIGN (non-seed) matrix
// is already published rather than silently retire user-authored configuration.
func TestSeedRefusesForeignPublishedVaccinationMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := defaultTenantID
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1,'seed-matrix-test','active') ON CONFLICT (tenant_id) DO NOTHING`, tenantID); err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	seedProtocolID := seedMatrixProtocolID(tenantID)
	if _, err := pool.Exec(ctx, `
		INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		VALUES ($1,$2,$3,'Preventive Care Vaccination Matrix','vaccination','active')
		ON CONFLICT (tenant_id, code) DO NOTHING`, seedProtocolID, tenantID, matrixProtocolCode); err != nil {
		t.Fatalf("seed protocol def: %v", err)
	}

	// No foreign matrix yet: the guard must be clear.
	if foreign, err := foreignPublishedVaccinationMatrices(ctx, pool, tenantID, seedProtocolID); err != nil {
		t.Fatalf("guard (clean): %v", err)
	} else if len(foreign) != 0 {
		t.Fatalf("guard flagged %v with no foreign matrix present", foreign)
	}

	// The seed's OWN published matrix must NEVER be flagged (a normal reseed is unaffected).
	seedOwnVersion := detUUID("protocol_version", tenantID, "vaccination_matrix", "guard_seed_own")
	if _, err := pool.Exec(ctx, `
		INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
			version_label, status, effective_from, effective_to, rule_dsl, proof_policy)
		VALUES ($1,$2,$3,'tenant',NULL,1,$4,'published',DATE '2026-01-01',NULL,
			'{"ruleset_family":"vaccination.matrix","matrix_rows":[]}'::jsonb,'{}'::jsonb)`,
		seedOwnVersion, tenantID, seedProtocolID, matrixVersionLabel); err != nil {
		t.Fatalf("seed own published version: %v", err)
	}
	if foreign, err := foreignPublishedVaccinationMatrices(ctx, pool, tenantID, seedProtocolID); err != nil {
		t.Fatalf("guard (own only): %v", err)
	} else if len(foreign) != 0 {
		t.Fatalf("guard flagged the seed's OWN matrix %v; a normal reseed must not be refused", foreign)
	}

	// A user-authored, DIFFERENT-protocol, tenant-scoped, matrix-shaped, published, overlapping
	// matrix must be detected so the seed refuses instead of retiring it.
	foreignProtocolID := detUUID("protocol", tenantID, "user_custom_vaccination_matrix")
	if _, err := pool.Exec(ctx, `
		INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		VALUES ($1,$2,'custom.vaccination.matrix','User Custom Matrix','vaccination','active')`,
		foreignProtocolID, tenantID); err != nil {
		t.Fatalf("foreign protocol def: %v", err)
	}
	foreignVersion := detUUID("protocol_version", tenantID, "user_custom", "v1")
	if _, err := pool.Exec(ctx, `
		INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
			version_label, status, effective_from, effective_to, rule_dsl, proof_policy)
		VALUES ($1,$2,$3,'tenant',NULL,1,'User Matrix','published',DATE '2026-06-01',NULL,
			'{"ruleset_family":"vaccination.matrix","matrix_rows":[]}'::jsonb,'{}'::jsonb)`,
		foreignVersion, tenantID, foreignProtocolID); err != nil {
		t.Fatalf("foreign published version: %v", err)
	}
	foreign, err := foreignPublishedVaccinationMatrices(ctx, pool, tenantID, seedProtocolID)
	if err != nil {
		t.Fatalf("guard (foreign present): %v", err)
	}
	if len(foreign) != 1 || foreign[0] != "custom.vaccination.matrix" {
		t.Fatalf("guard = %v, want exactly [custom.vaccination.matrix]", foreign)
	}
}

// TestSeedMatrixReconcilePublishReplayDoesNotChurn covers the persistence gap the string-only
// TestSeedMatrixBuildIsDeterministic cannot: it starts from a faulty published seed matrix, runs the
// seed's real reconcile+publish correction TWICE, and asserts no version churn, ownership preserved,
// correct statuses, and exactly one retire event (no duplicate) across the replay.
func TestSeedMatrixReconcilePublishReplayDoesNotChurn(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := defaultTenantID
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1,'seed-matrix-test','active') ON CONFLICT (tenant_id) DO NOTHING`, tenantID); err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		VALUES ($1,$2,$3,'Preventive Care Vaccination Matrix','vaccination','active')
		ON CONFLICT (tenant_id, code) DO NOTHING`, seedMatrixProtocolID(tenantID), tenantID, matrixProtocolCode); err != nil {
		t.Fatalf("seed protocol def: %v", err)
	}
	var seedProtocolID string
	if err := pool.QueryRow(ctx, `SELECT protocol_id::text FROM protocol_definitions WHERE tenant_id=$1 AND code=$2`, tenantID, matrixProtocolCode).Scan(&seedProtocolID); err != nil {
		t.Fatalf("resolve seed protocol id: %v", err)
	}

	// Published vaccination.drive SOP for the composite sop_version FK (resolve-or-create so the test
	// is robust to whatever the migrated template already seeds for the default tenant).
	var sopID string
	if err := pool.QueryRow(ctx, `SELECT sop_id::text FROM sop_definitions WHERE tenant_id=$1 AND code='vaccination.drive'`, tenantID).Scan(&sopID); err != nil {
		sopID = detUUID("sop", tenantID, "vaccination_drive")
		if _, ierr := pool.Exec(ctx, `INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status) VALUES ($1,$2,'vaccination.drive','Vaccination Drive','active')`, sopID, tenantID); ierr != nil {
			t.Fatalf("sop def create: %v", ierr)
		}
	}
	var sopVersionID string
	if err := pool.QueryRow(ctx, `SELECT sop_version_id::text FROM sop_versions WHERE tenant_id=$1 AND sop_id=$2 AND status='published' ORDER BY version DESC LIMIT 1`, tenantID, sopID).Scan(&sopVersionID); err != nil {
		sopVersionID = detUUID("sop_version", tenantID, "vaccination_drive", "matrix_test_v1")
		if _, ierr := pool.Exec(ctx, `INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy) VALUES ($1,$2,$3,9990,'matrix-test-v1','published','{}'::jsonb,'{}'::jsonb)`, sopVersionID, tenantID, sopID); ierr != nil {
			t.Fatalf("sop version create: %v", ierr)
		}
	}

	// Start from a FAULTY published seed matrix (a differing matrix-shaped rule_dsl under the seed's
	// own protocol + label) so the reconcile sees "published but changed" and mints a correction.
	faultyVersion := detUUID("protocol_version", tenantID, "vaccination_matrix", "v1_real")
	if _, err := pool.Exec(ctx, `
		INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
			version_label, status, effective_from, effective_to, rule_dsl, proof_policy, sop_version_id)
		VALUES ($1,$2,$3,'tenant',NULL,1,$4,'published',DATE '2026-01-01',NULL,
			'{"ruleset_family":"vaccination.matrix","matrix_rows":[],"_seed_variant":"faulty"}'::jsonb,'{}'::jsonb,$5)`,
		faultyVersion, tenantID, seedProtocolID, matrixVersionLabel, sopVersionID); err != nil {
		t.Fatalf("faulty published version: %v", err)
	}

	correctDSL, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("build matrix dsl: %v", err)
	}
	protocolService := protocolapp.NewService(protocolpg.NewRepository(pool, 10*time.Second))

	// One correction cycle: reconcile (own tx) -> publish (retires the faulty one).
	correct := func(round int) string {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("round %d begin: %v", round, err)
		}
		versionID, rerr := reconcileSeedMatrixDraftVersion(ctx, tx, tenantID, seedProtocolID, correctDSL, sopVersionID)
		if rerr != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("round %d reconcile: %v", round, rerr)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("round %d commit: %v", round, err)
		}
		if err := protocolService.PublishVersion(ctx, tenantID, versionID, nil, "seed-vaccination-real:"+versionID); err != nil {
			t.Fatalf("round %d publish: %v", round, err)
		}
		return versionID
	}

	v2 := correct(1)
	v2Again := correct(2) // replay: same corrected DSL, must NOT churn

	if v2 != v2Again {
		t.Fatalf("reconcile churned versions: round1=%s round2=%s (replay must reuse the same version)", v2, v2Again)
	}

	// Exactly two versions for the seed protocol: the retired faulty one + the single corrected one.
	var total, published, retired, drafts int
	if err := pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE status='published'),
		       count(*) FILTER (WHERE status='retired'),
		       count(*) FILTER (WHERE status='draft')
		FROM protocol_versions
		WHERE tenant_id=$1 AND protocol_id=$2 AND scope_type='tenant' AND scope_id IS NULL`,
		tenantID, seedProtocolID).Scan(&total, &published, &retired, &drafts); err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if total != 2 || published != 1 || retired != 1 || drafts != 0 {
		t.Fatalf("version counts total=%d published=%d retired=%d draft=%d, want 2/1/1/0 (no replay churn)", total, published, retired, drafts)
	}

	// Ownership preserved: the corrected version stays under the seed's own protocol and is published.
	var ownerProtocol, v2Status string
	if err := pool.QueryRow(ctx,
		`SELECT protocol_id::text, status FROM protocol_versions WHERE tenant_id=$1 AND protocol_version_id=$2`,
		tenantID, v2).Scan(&ownerProtocol, &v2Status); err != nil {
		t.Fatalf("read corrected version: %v", err)
	}
	if ownerProtocol != seedProtocolID || v2Status != "published" {
		t.Fatalf("corrected version owner=%s status=%s, want %s/published", ownerProtocol, v2Status, seedProtocolID)
	}

	// The faulty version is retired, and the retire fired exactly ONCE across both cycles (the replay
	// publish of an already-published version must not emit a duplicate retire event).
	var faultyStatus string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM protocol_versions WHERE tenant_id=$1 AND protocol_version_id=$2`,
		tenantID, faultyVersion).Scan(&faultyStatus); err != nil {
		t.Fatalf("read faulty version: %v", err)
	}
	if faultyStatus != "retired" {
		t.Fatalf("faulty version status=%s, want retired", faultyStatus)
	}
	var retiredOutbox int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM outbox_messages
		WHERE tenant_id=$1 AND event_type='protocol.version.retired' AND aggregate_id=$2::uuid`,
		tenantID, faultyVersion).Scan(&retiredOutbox); err != nil {
		t.Fatalf("count retire outbox: %v", err)
	}
	if retiredOutbox != 1 {
		t.Fatalf("retire outbox count=%d, want exactly 1 (replay must not duplicate the retire event)", retiredOutbox)
	}
}
