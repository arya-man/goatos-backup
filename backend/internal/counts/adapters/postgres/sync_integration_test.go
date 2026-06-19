package postgres

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	locationspg "github.com/vgoats/goatos/backend/internal/locations/adapters/postgres"
	locationsapp "github.com/vgoats/goatos/backend/internal/locations/app"
)

const (
	countsTestPostgresImage = "postgres:16.9-alpine"
	countsTestTenantID      = "00000000-0000-4000-8000-000000000001"
	countsTestActorID       = "90000000-0000-4000-8000-000000000001"
	countsTestLocationID    = "00000000-0000-4000-8000-000000009101"
)

func TestCountsSyncWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool, repo := startCountsSyncDB(t, ctx)
	defer pool.Close()
	seedCountsLocationAlias(t, pool)
	seedCountsSourceRows(t, pool)

	locationRepo := locationspg.NewRepository(pool, 5*time.Second)
	service := countsapp.NewServiceWithResolver(repo, locationsapp.NewService(locationRepo))
	result, err := service.Sync(ctx, countsapp.SyncCountsInput{
		TenantID:       countsTestTenantID,
		ActorID:        countsTestActorID,
		IdempotencyKey: "idem-counts-sync-0001",
		TraceID:        "trace-counts-sync-0001",
		RawBody:        []byte(`{"snapshot_date":"2026-06-18"}`),
	})
	if err != nil {
		t.Fatalf("Sync counts: %v", err)
	}
	if result.SourceRowsRead != 2 || result.ProjectionRowsWritten != 2 || result.UnresolvedLocationLabels != 1 || result.Freshness.ServingState != "fresh" {
		t.Fatalf("unexpected counts sync result: %#v", result)
	}
	if result.SnapshotDate == nil || *result.SnapshotDate != "2026-06-18" {
		t.Fatalf("snapshot date = %#v", result.SnapshotDate)
	}
	if got := countCountsRows(t, pool, `SELECT count(*) FROM location_review_items WHERE tenant_id = $1::uuid AND source_context = 'legacy_bq_counts' AND normalized_source_label = 'unknown counts yard' AND status = 'open'`, countsTestTenantID); got != 1 {
		t.Fatalf("unknown counts review rows = %d", got)
	}
	if got := countCountsRows(t, pool, `SELECT count(*) FROM counts_projection_rows WHERE tenant_id = $1::uuid AND view_id = 'overall' AND snapshot_date = '2026-06-18'`, countsTestTenantID); got != 2 {
		t.Fatalf("counts projection rows = %d", got)
	}
	dashboard, err := repo.GetDashboard(ctx, ports.DashboardParams{
		TenantID:     countsTestTenantID,
		View:         "overall",
		SnapshotDate: "2026-06-18",
	}, "trace-counts-dashboard")
	if err != nil {
		t.Fatalf("GetDashboard: %v", err)
	}
	if dashboard.Freshness.ServingState != "fresh" || len(dashboard.Summary) != 1 || len(dashboard.Sections["farm"]) != 1 {
		t.Fatalf("unexpected dashboard: %#v", dashboard)
	}

	replay, err := service.Sync(ctx, countsapp.SyncCountsInput{
		TenantID:       countsTestTenantID,
		ActorID:        countsTestActorID,
		IdempotencyKey: "idem-counts-sync-0001",
		TraceID:        "trace-counts-sync-replay",
		RawBody:        []byte(`{"snapshot_date":"2026-06-18"}`),
	})
	if err != nil {
		t.Fatalf("Sync counts replay: %v", err)
	}
	if !replay.Idempotency.Replayed || replay.SyncRunID != result.SyncRunID {
		t.Fatalf("unexpected replay: %#v", replay)
	}
	second, err := service.Sync(ctx, countsapp.SyncCountsInput{
		TenantID:       countsTestTenantID,
		ActorID:        countsTestActorID,
		IdempotencyKey: "idem-counts-sync-0002",
		TraceID:        "trace-counts-sync-0002",
		RawBody:        []byte(`{"snapshot_date":"2026-06-18"}`),
	})
	if err != nil {
		t.Fatalf("Sync counts second: %v", err)
	}
	if second.ProjectionRowsWritten != 2 {
		t.Fatalf("second projection rows = %d", second.ProjectionRowsWritten)
	}
	if got := countCountsRows(t, pool, `SELECT count(*) FROM location_review_items WHERE tenant_id = $1::uuid AND source_context = 'legacy_bq_counts' AND normalized_source_label = 'unknown counts yard' AND status = 'open'`, countsTestTenantID); got != 1 {
		t.Fatalf("review rows after second sync = %d", got)
	}

	// P2a: GetSyncRun for a missing id must surface a not-found sentinel that the
	// service maps to 404, not a raw error mapped to 500.
	missingRunID := "11111111-1111-4111-8111-111111111111"
	if _, err := repo.GetSyncRun(ctx, countsTestTenantID, missingRunID, "trace-counts-missing"); !errors.Is(err, ports.ErrSyncRunNotFound) {
		t.Fatalf("GetSyncRun(missing) err = %v, want ErrSyncRunNotFound", err)
	}
	if _, err := service.GetSyncRun(ctx, countsTestTenantID, missingRunID, "trace-counts-missing"); err == nil {
		t.Fatalf("service.GetSyncRun(missing) err = nil, want 404")
	} else {
		var appErr *countsapp.Error
		if !errors.As(err, &appErr) || appErr.HTTPStatus != 404 {
			t.Fatalf("service.GetSyncRun(missing) err = %#v, want HTTP 404", err)
		}
	}

	// P1c: a no-source sync (snapshot with no seeded source rows) must not wipe
	// the previously-served 'overall' projection. It should stay served-but-stale,
	// keep its projection rows, and not flip to source_unavailable.
	noSource, err := service.Sync(ctx, countsapp.SyncCountsInput{
		TenantID:       countsTestTenantID,
		ActorID:        countsTestActorID,
		IdempotencyKey: "idem-counts-sync-nosrc",
		TraceID:        "trace-counts-sync-nosrc",
		RawBody:        []byte(`{"snapshot_date":"2026-01-01"}`),
	})
	if err != nil {
		t.Fatalf("Sync counts no-source: %v", err)
	}
	if noSource.SourceRowsRead != 0 {
		t.Fatalf("no-source sync read %d rows, want 0", noSource.SourceRowsRead)
	}
	if got := countCountsRows(t, pool, `SELECT count(*) FROM counts_projection_rows WHERE tenant_id = $1::uuid AND view_id = 'overall' AND snapshot_date = '2026-06-18'`, countsTestTenantID); got != 2 {
		t.Fatalf("projection rows after no-source sync = %d, want 2 preserved", got)
	}
	preserved, err := repo.GetDashboard(ctx, ports.DashboardParams{
		TenantID:     countsTestTenantID,
		View:         "overall",
		SnapshotDate: "2026-06-18",
	}, "trace-counts-dashboard-preserved")
	if err != nil {
		t.Fatalf("GetDashboard after no-source: %v", err)
	}
	if preserved.Freshness.ServingState == "source_unavailable" {
		t.Fatalf("overall serving_state = source_unavailable after no-source sync; prior success should be preserved")
	}
	if preserved.Freshness.ServingState != "stale" || len(preserved.Summary) != 1 {
		t.Fatalf("unexpected preserved dashboard: serving=%q summary=%d", preserved.Freshness.ServingState, len(preserved.Summary))
	}
}

func seedCountsLocationAlias(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'COUNT_SYNC_SHED', 'Counts Sync Shed', 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTestLocationID, countsTestTenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination, usable_for_sop)
VALUES ($1::uuid, $2::uuid, true, true, true, true)
ON CONFLICT (location_id) DO NOTHING`, countsTestTenantID, countsTestLocationID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO location_aliases (tenant_id, canonical_location_id, alias_code, source_context, status)
VALUES ($1::uuid, $2::uuid, 'Counts Shed A', 'legacy_bq_counts', 'active')
ON CONFLICT DO NOTHING`, countsTestTenantID, countsTestLocationID); err != nil {
		t.Fatal(err)
	}
}

func seedCountsSourceRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	rows := []struct {
		key     string
		payload string
		hash    string
	}{
		{
			key: "summary-total",
			payload: `{
				"snapshot_date":"2026-06-18",
				"view_id":"overall",
				"section":"summary",
				"grain":"metric",
				"dimension_key":"total",
				"dimension_label":"Total goats",
				"metric_key":"goat_count",
				"count_value":42,
				"unit":"count",
				"farm_label":"Counts Shed A",
				"logical_fact_key":"fixture-total"
			}`,
			hash: "sha256:counts-summary-total",
		},
		{
			key: "unknown-farm",
			payload: `{
				"snapshot_date":"2026-06-18",
				"view_id":"overall",
				"section":"farm",
				"grain":"farm",
				"dimension_key":"unknown_counts_yard",
				"dimension_label":"Unknown Counts Yard",
				"metric_key":"goat_count",
				"count_value":3,
				"unit":"count",
				"farm_label":"Unknown Counts Yard",
				"logical_fact_key":"fixture-unknown-farm"
			}`,
			hash: "sha256:counts-unknown-farm",
		},
	}
	for _, row := range rows {
		if _, err := pool.Exec(ctx, `
INSERT INTO counts_source_rows (
  tenant_id, source_system, source_id, source_table, source_row_key,
  source_watermark_date, payload_json, payload_hash, row_status
) VALUES (
  $1::uuid, 'legacy_bigquery', 'fixture_counts', 'fixture_counts_table', $2,
  '2026-06-18', $3::jsonb, $4, 'current'
)`, countsTestTenantID, row.key, row.payload, row.hash); err != nil {
			t.Fatal(err)
		}
	}
}

func startCountsSyncDB(t *testing.T, ctx context.Context) (*pgxpool.Pool, *Repository) {
	t.Helper()
	container := "goatos-counts-sync-test-" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "")
	postgresImage := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if postgresImage == "" {
		postgresImage = countsTestPostgresImage
	}
	runCounts(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", postgresImage)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	})
	ready := false
	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos").Run() == nil {
			ready = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("postgres container did not become ready:\n%s", runCountsOutput(t, "docker", "logs", container))
	}
	applyCountsMigrations(t, container)
	pool := openCountsPool(t, ctx, container)
	return pool, NewRepository(pool, 5*time.Second)
}

func applyCountsMigrations(t *testing.T, container string) {
	t.Helper()
	root := countsRepoRoot(t)
	migrations, err := filepath.Glob(filepath.Join(root, "backend", "migrations", "postgres", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(migrations)
	for _, migration := range migrations {
		sqlBytes, err := os.ReadFile(migration)
		if err != nil {
			t.Fatal(err)
		}
		countsPSQL(t, container, extractCountsGooseUp(string(sqlBytes)))
	}
}

func openCountsPool(t *testing.T, ctx context.Context, container string) *pgxpool.Pool {
	t.Helper()
	out := runCountsOutput(t, "docker", "port", container, "5432/tcp")
	parts := strings.Split(strings.TrimSpace(out), ":")
	port := parts[len(parts)-1]
	url := "postgres://postgres:goatos@127.0.0.1:" + port + "/goatos?sslmode=disable"
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	return pool
}

func countsPSQL(t *testing.T, container, sqlText string) {
	t.Helper()
	cmd := exec.Command("docker", "exec", "-i", container, "psql", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos")
	cmd.Stdin = strings.NewReader(sqlText)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("psql failed: %v\n%s", err, stderr.String())
	}
}

func extractCountsGooseUp(sqlText string) string {
	var out []string
	inUp := false
	for _, line := range strings.Split(sqlText, "\n") {
		if strings.HasPrefix(line, "-- +goose Up") {
			inUp = true
			continue
		}
		if strings.HasPrefix(line, "-- +goose Down") {
			break
		}
		if inUp {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func countsRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "backend", "migrations", "postgres")); err == nil {
			return wd
		}
		next := filepath.Dir(wd)
		if next == wd {
			t.Fatal("repo root not found")
		}
		wd = next
	}
}

func countCountsRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func runCounts(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, stderr.String())
	}
}

func runCountsOutput(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, stderr.String())
	}
	return string(out)
}
