package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
)

const matrixOwnerTenant = defaultTenantID

func seedMatrixProtocolID(tenantID string) string {
	return detUUID("protocol", tenantID, "vaccination_matrix")
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// setupSeedMatrixFixture seeds tenant + seed's canonical vaccination.matrix protocol + a published
// vaccination.drive SOP (composite FK). Returns (seedProtocolID, seedActorID, sopVersionID). Robust to
// whatever the migrated template already seeds for the default tenant.
func setupSeedMatrixFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string, string) {
	t.Helper()
	tenantID := matrixOwnerTenant
	mustExec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1,'seed-matrix-test','active') ON CONFLICT (tenant_id) DO NOTHING`, tenantID)
	mustExec(t, ctx, pool, `
		INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		VALUES ($1,$2,$3,'Preventive Care Vaccination Matrix','vaccination','active')
		ON CONFLICT (tenant_id, code) DO NOTHING`, seedMatrixProtocolID(tenantID), tenantID, matrixProtocolCode)
	var seedProtocolID string
	if err := pool.QueryRow(ctx, `SELECT protocol_id::text FROM protocol_definitions WHERE tenant_id=$1 AND code=$2`, tenantID, matrixProtocolCode).Scan(&seedProtocolID); err != nil {
		t.Fatalf("resolve seed protocol id: %v", err)
	}
	seedActorID := detUUID("seed-actor", tenantID)

	var sopID string
	if err := pool.QueryRow(ctx, `SELECT sop_id::text FROM sop_definitions WHERE tenant_id=$1 AND code='vaccination.drive'`, tenantID).Scan(&sopID); err != nil {
		sopID = detUUID("sop", tenantID, "vaccination_drive")
		mustExec(t, ctx, pool, `INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status) VALUES ($1,$2,'vaccination.drive','Vaccination Drive','active')`, sopID, tenantID)
	}
	var sopVersionID string
	if err := pool.QueryRow(ctx, `SELECT sop_version_id::text FROM sop_versions WHERE tenant_id=$1 AND sop_id=$2 AND status='published' ORDER BY version DESC LIMIT 1`, tenantID, sopID).Scan(&sopVersionID); err != nil {
		sopVersionID = detUUID("sop_version", tenantID, "vaccination_drive", "matrix_test_v1")
		mustExec(t, ctx, pool, `INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy) VALUES ($1,$2,$3,9990,'matrix-test-v1','published','{}'::jsonb,'{}'::jsonb)`, sopVersionID, tenantID, sopID)
	}
	return seedProtocolID, seedActorID, sopVersionID
}

// insertPublishedMatrix inserts a published, tenant-scoped, matrix-shaped vaccination version with an
// explicit drafted_by (nil = legacy/unstamped) under protocolID.
func insertPublishedMatrix(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, protocolID, versionID, label string, version int, draftedBy *string) {
	t.Helper()
	mustExec(t, ctx, pool, `
		INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
			version_label, status, effective_from, effective_to, rule_dsl, proof_policy, drafted_by)
		VALUES ($1,$2,$3,'tenant',NULL,$4,$5,'published',DATE '2026-01-01',NULL,
			'{"ruleset_family":"vaccination.matrix","matrix_rows":[]}'::jsonb,'{}'::jsonb,$6)`,
		versionID, tenantID, protocolID, version, label, draftedBy)
}

// TestForeignPublishedVaccinationMatricesDetectsNonSeedOwned proves the ownership pre-check keys on
// explicit seed provenance (drafted_by = seed actor), NOT protocol_id: a user-authored version under
// the SAME canonical protocol is flagged, while the seed's own and legacy-unstamped versions are not.
func TestForeignPublishedVaccinationMatricesDetectsNonSeedOwned(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProtocolID, seedActorID, _ := setupSeedMatrixFixture(t, ctx, pool)
	tenantID := matrixOwnerTenant

	// The protocol_versions_published_no_overlap EXCLUDE constraint allows only ONE published matrix at
	// a scope/window at a time, and published rows are immutable/undeletable, so each case inserts a
	// fresh published matrix (unique id), asserts, then retires it (the one allowed transition) to clear
	// the published slot for the next case.
	caseNo := 0
	check := func(msg string, draftedBy *string, protocolID string, wantFlagged bool) {
		t.Helper()
		caseNo++
		pvID := detUUID("pv", tenantID, "probe", string(rune('a'+caseNo)))
		insertPublishedMatrix(t, ctx, pool, tenantID, protocolID, pvID, "Probe Matrix", caseNo, draftedBy)
		got, err := foreignPublishedVaccinationMatrices(ctx, pool, tenantID, seedActorID)
		if err != nil {
			t.Fatalf("%s: guard err: %v", msg, err)
		}
		flagged := len(got) > 0
		if flagged != wantFlagged {
			t.Fatalf("%s: guard = %v (flagged=%v), want flagged=%v", msg, got, flagged, wantFlagged)
		}
		mustExec(t, ctx, pool, `UPDATE protocol_versions SET status='retired', retired_at=now(), row_version=row_version+1 WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, pvID)
	}

	if got, err := foreignPublishedVaccinationMatrices(ctx, pool, tenantID, seedActorID); err != nil || len(got) != 0 {
		t.Fatalf("clean: got %v err %v, want none", got, err)
	}
	seedActor := seedActorID
	check("seed-owned (drafted_by=seed actor)", &seedActor, seedProtocolID, false)
	// A PUBLISHED matrix with no provenance stamp cannot be positively identified as the seed's (it is
	// immutable and NULL is schema-permitted), so it is flagged — the seed refuses rather than clobber
	// it (VAX-SEED-R1). Legacy seed DRAFTS are back-stamped before publish and so never reach here.
	check("unstamped published (drafted_by NULL) is NOT provably seed", nil, seedProtocolID, true)
	userA := detUUID("user", tenantID, "alice")
	check("user-authored under SAME seed protocol (VAX-SEED-01)", &userA, seedProtocolID, true)

	foreignProtocol := detUUID("protocol", tenantID, "user_other")
	mustExec(t, ctx, pool, `INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status) VALUES ($1,$2,'custom.matrix.other','Other','vaccination','active') ON CONFLICT (tenant_id, code) DO NOTHING`, foreignProtocol, tenantID)
	userB := detUUID("user", tenantID, "bob")
	check("user-authored under a DIFFERENT protocol", &userB, foreignProtocol, true)
}

// TestSeedGuardedPublishRefusesUserMatrixUnderSameProtocol proves the ATOMIC ownership guard
// (VAX-SEED-01 + VAX-SEED-03): publishing the seed matrix while a user-authored version exists under
// the SAME protocol fails closed with ErrVaccinationMatrixOwnershipConflict, and the user's version is
// left published (never retired). The check runs inside the publish txn under the matrix advisory
// lock, so it holds even against a version that appears after any earlier pre-check.
func TestSeedGuardedPublishRefusesUserMatrixUnderSameProtocol(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProtocolID, seedActorID, sopVersionID := setupSeedMatrixFixture(t, ctx, pool)
	tenantID := matrixOwnerTenant

	userA := detUUID("user", tenantID, "alice")
	userVersion := detUUID("pv", tenantID, "user_same")
	insertPublishedMatrix(t, ctx, pool, tenantID, seedProtocolID, userVersion, "Alice Custom Matrix", 7, &userA)

	correctDSL, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("build matrix dsl: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	versionID, rerr := reconcileSeedMatrixDraftVersion(ctx, tx, tenantID, seedProtocolID, correctDSL, sopVersionID, seedActorID)
	if rerr != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("reconcile: %v", rerr)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	svc := protocolapp.NewService(protocolpg.NewRepository(pool, 10*time.Second))
	perr := svc.PublishSeedOwnedVaccinationMatrixVersion(ctx, tenantID, versionID, seedActorID, "seed-vaccination-real:"+versionID)
	if perr == nil {
		t.Fatalf("guarded publish succeeded, want ownership conflict")
	}
	if !errors.Is(perr, protocolpg.ErrVaccinationMatrixOwnershipConflict) {
		t.Fatalf("publish err = %v, want ErrVaccinationMatrixOwnershipConflict", perr)
	}

	var userStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM protocol_versions WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, userVersion).Scan(&userStatus); err != nil {
		t.Fatalf("read user version: %v", err)
	}
	if userStatus != "published" {
		t.Fatalf("user version status = %s, want published (unchanged)", userStatus)
	}
}

// TestSeedGuardedPublishRefusesUserDraftedTarget covers VAX-SEED-R2: a seed-guarded publish must
// verify the TARGET version is seed-drafted. Pointing it at a user-drafted draft (as any internal
// caller could) must fail closed and leave that draft untouched, never laundering it into a seed op.
func TestSeedGuardedPublishRefusesUserDraftedTarget(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProtocolID, seedActorID, sopVersionID := setupSeedMatrixFixture(t, ctx, pool)
	tenantID := matrixOwnerTenant

	userA := detUUID("user", tenantID, "alice")
	userDraft := detUUID("pv", tenantID, "user_draft")
	correctDSL, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("build matrix dsl: %v", err)
	}
	mustExec(t, ctx, pool, `
		INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
			version_label, status, effective_from, effective_to, rule_dsl, proof_policy, sop_version_id, drafted_by)
		VALUES ($1,$2,$3,'tenant',NULL,1,'Alice Draft','draft',DATE '2026-01-01',NULL,$4::jsonb,
			'{"required_proofs":["shed","vial_lot","administration"]}'::jsonb,$5,$6)`,
		userDraft, tenantID, seedProtocolID, correctDSL, sopVersionID, userA)

	svc := protocolapp.NewService(protocolpg.NewRepository(pool, 10*time.Second))
	perr := svc.PublishSeedOwnedVaccinationMatrixVersion(ctx, tenantID, userDraft, seedActorID, "seed-vaccination-real:"+userDraft)
	if perr == nil || !errors.Is(perr, protocolpg.ErrVaccinationMatrixOwnershipConflict) {
		t.Fatalf("publish err = %v, want ErrVaccinationMatrixOwnershipConflict", perr)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM protocol_versions WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, userDraft).Scan(&status); err != nil {
		t.Fatalf("read user draft: %v", err)
	}
	if status != "draft" {
		t.Fatalf("user draft status = %s, want draft (untouched)", status)
	}
}

// TestSeedMatrixReconcilePublishReplayDoesNotChurn (VAX-SEED-02) starts from a faulty published SEED
// matrix and runs the real reconcile+guarded-publish correction twice: no version churn, ownership
// preserved, correct statuses, exactly one retire event. Also proves the guard lets the seed supersede
// its OWN prior (here legacy-unstamped) version.
func TestSeedMatrixReconcilePublishReplayDoesNotChurn(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProtocolID, seedActorID, sopVersionID := setupSeedMatrixFixture(t, ctx, pool)
	tenantID := matrixOwnerTenant

	faultyVersion := detUUID("protocol_version", tenantID, "vaccination_matrix", "v1_real")
	mustExec(t, ctx, pool, `
		INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
			version_label, status, effective_from, effective_to, rule_dsl, proof_policy, sop_version_id, drafted_by)
		VALUES ($1,$2,$3,'tenant',NULL,1,$4,'published',DATE '2026-01-01',NULL,
			'{"ruleset_family":"vaccination.matrix","matrix_rows":[],"_seed_variant":"faulty"}'::jsonb,'{}'::jsonb,$5,$6)`,
		faultyVersion, tenantID, seedProtocolID, matrixVersionLabel, sopVersionID, seedActorID)

	correctDSL, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("build matrix dsl: %v", err)
	}
	svc := protocolapp.NewService(protocolpg.NewRepository(pool, 10*time.Second))
	correct := func(round int) string {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("round %d begin: %v", round, err)
		}
		versionID, rerr := reconcileSeedMatrixDraftVersion(ctx, tx, tenantID, seedProtocolID, correctDSL, sopVersionID, seedActorID)
		if rerr != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("round %d reconcile: %v", round, rerr)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("round %d commit: %v", round, err)
		}
		if err := svc.PublishSeedOwnedVaccinationMatrixVersion(ctx, tenantID, versionID, seedActorID, "seed-vaccination-real:"+versionID); err != nil {
			t.Fatalf("round %d publish: %v", round, err)
		}
		return versionID
	}

	v2 := correct(1)
	v2Again := correct(2)
	if v2 != v2Again {
		t.Fatalf("reconcile churned versions: round1=%s round2=%s", v2, v2Again)
	}

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
		t.Fatalf("version counts total=%d published=%d retired=%d draft=%d, want 2/1/1/0", total, published, retired, drafts)
	}

	var ownerProtocol, v2Status string
	if err := pool.QueryRow(ctx, `SELECT protocol_id::text, status FROM protocol_versions WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, v2).Scan(&ownerProtocol, &v2Status); err != nil {
		t.Fatalf("read corrected version: %v", err)
	}
	if ownerProtocol != seedProtocolID || v2Status != "published" {
		t.Fatalf("corrected version owner=%s status=%s, want %s/published", ownerProtocol, v2Status, seedProtocolID)
	}

	var faultyStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM protocol_versions WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, faultyVersion).Scan(&faultyStatus); err != nil {
		t.Fatalf("read faulty version: %v", err)
	}
	if faultyStatus != "retired" {
		t.Fatalf("faulty version status = %s, want retired", faultyStatus)
	}
	var retiredOutbox int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='protocol.version.retired' AND aggregate_id=$2::uuid`, tenantID, faultyVersion).Scan(&retiredOutbox); err != nil {
		t.Fatalf("count retire outbox: %v", err)
	}
	if retiredOutbox != 1 {
		t.Fatalf("retire outbox count=%d, want exactly 1", retiredOutbox)
	}
}
