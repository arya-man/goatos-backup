package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestLatestVersionsForReturnsHighestVersionPerSOPInOneQuery proves the C35-015 batch loader: one
// DISTINCT ON query returns the single latest (highest-version) row per requested sop_id, so the SOP
// Library list no longer fans out one detail query per SOP.
func TestLatestVersionsForReturnsHighestVersionPerSOPInOneQuery(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant = "00000000-0000-4000-8000-000000000001"
		sopA   = "73000000-0000-4000-8000-000000000001"
		sopB   = "73000000-0000-4000-8000-000000000002"
		sopC   = "73000000-0000-4000-8000-000000000003" // has no versions
		verA1  = "73000000-0000-4000-8000-0000000000a1"
		verA2  = "73000000-0000-4000-8000-0000000000a2"
		verA3  = "73000000-0000-4000-8000-0000000000a3"
		verB1  = "73000000-0000-4000-8000-0000000000b1"
	)

	for _, sop := range []struct{ id, code, name string }{
		{sopA, "vaccination.batchlatest.a", "Vaccination A"},
		{sopB, "vaccination.batchlatest.b", "Vaccination B"},
		{sopC, "vaccination.batchlatest.c", "Vaccination C"},
	} {
		execFanout(t, ctx, pool, "sop definition",
			`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, description, status)
			 VALUES ($1, $2, $3, $4, 'batch latest-version regression', 'active')`,
			sop.id, tenant, sop.code, sop.name)
	}

	// SOP A: versions 1,2,3 inserted out of order so ordering (not insert order) must decide the latest.
	// Only one version per SOP may be 'published' (partial unique index), so older ones are 'retired'.
	seedVersion(t, ctx, pool, verA2, tenant, sopA, 2, "A v2", "retired")
	seedVersion(t, ctx, pool, verA1, tenant, sopA, 1, "A v1", "retired")
	seedVersion(t, ctx, pool, verA3, tenant, sopA, 3, "A v3", "published")
	// SOP B: single version.
	seedVersion(t, ctx, pool, verB1, tenant, sopB, 1, "B v1", "published")

	repo := NewRepository(pool, 5*time.Second)
	got, err := repo.LatestVersionsFor(ctx, tenant, []string{sopA, sopB, sopC})
	if err != nil {
		t.Fatalf("LatestVersionsFor() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("latest versions = %d want 2 (SOP C has no version): %#v", len(got), got)
	}
	if v := got[sopA]; v.SOPVersionID != verA3 || v.Version != 3 {
		t.Fatalf("SOP A latest = %s v%d, want %s v3", v.SOPVersionID, v.Version, verA3)
	}
	if v := got[sopB]; v.SOPVersionID != verB1 || v.Version != 1 {
		t.Fatalf("SOP B latest = %s v%d, want %s v1", v.SOPVersionID, v.Version, verB1)
	}
	if _, ok := got[sopC]; ok {
		t.Fatalf("SOP C has no version but appeared in the map")
	}

	// Empty input is a no-op that never touches the DB.
	empty, err := repo.LatestVersionsFor(ctx, tenant, nil)
	if err != nil {
		t.Fatalf("LatestVersionsFor(nil) error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty input returned %d rows", len(empty))
	}
}

func seedVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, tenant, sopID string, version int, label, status string) {
	t.Helper()
	execFanout(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1, $2, $3, $4, $5, $6,
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":true,"subject_scope":"batch","types":["video"],"minimum_count":1,"verify_before_apply":true}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		versionID, tenant, sopID, version, label, status)
}
