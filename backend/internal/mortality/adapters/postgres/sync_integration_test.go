package postgres

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	locationspg "github.com/vgoats/goatos/backend/internal/locations/adapters/postgres"
	locationsapp "github.com/vgoats/goatos/backend/internal/locations/app"
	mortalityapp "github.com/vgoats/goatos/backend/internal/mortality/app"
	"github.com/vgoats/goatos/backend/internal/mortality/ports"
)

const (
	mortalityTestPostgresImage = "postgres:16.9-alpine"
	mortalityTestTenantID      = "00000000-0000-4000-8000-000000000001"
	mortalityTestActorID       = "90000000-0000-4000-8000-000000000001"
	mortalityTestLocationID    = "00000000-0000-4000-8000-000000009201"
)

func TestMortalitySyncWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool, repo := startMortalitySyncDB(t, ctx)
	defer pool.Close()
	seedMortalityLocationAlias(t, pool)
	seedMortalitySourceRows(t, pool)

	locationRepo := locationspg.NewRepository(pool, 5*time.Second)
	service := mortalityapp.NewServiceWithResolver(repo, locationsapp.NewService(locationRepo))
	result, err := service.Sync(ctx, mortalityapp.SyncMortalityInput{
		TenantID:       mortalityTestTenantID,
		ActorID:        mortalityTestActorID,
		IdempotencyKey: "idem-mortality-sync-0001",
		TraceID:        "trace-mortality-sync-0001",
		RawBody:        []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Sync mortality: %v", err)
	}
	if result.SourceRowsRead != 4 || result.EventsUpserted != 4 || result.DedupCandidateEvents != 2 || result.UnresolvedLocationLabels != 2 || result.Freshness.ServingState != "fresh" {
		t.Fatalf("unexpected mortality sync result: %#v", result)
	}
	if got := countMortalityRows(t, pool, `SELECT count(*) FROM mortality_events WHERE tenant_id = $1::uuid`, mortalityTestTenantID); got != 3 {
		t.Fatalf("mortality events = %d", got)
	}
	if got := countMortalityRows(t, pool, `SELECT count(*) FROM mortality_events WHERE tenant_id = $1::uuid AND dedup_confidence = 'candidate_review'`, mortalityTestTenantID); got != 2 {
		t.Fatalf("candidate review events = %d", got)
	}
	if got := countMortalityRows(t, pool, `SELECT count(*) FROM location_review_items WHERE tenant_id = $1::uuid AND source_context = 'legacy_bq_mortality' AND normalized_source_label = 'unknown mortality pen' AND status = 'open'`, mortalityTestTenantID); got != 1 {
		t.Fatalf("unknown mortality review rows = %d", got)
	}
	if got := countMortalityRows(t, pool, `SELECT count(*) FROM mortality_projection_rows WHERE tenant_id = $1::uuid AND period = 'overall'`, mortalityTestTenantID); got == 0 {
		t.Fatal("expected overall mortality projection rows")
	}
	dashboard, err := repo.GetDashboard(ctx, ports.DashboardParams{TenantID: mortalityTestTenantID, Period: "overall"}, "trace-mortality-dashboard")
	if err != nil {
		t.Fatalf("GetDashboard: %v", err)
	}
	if dashboard.Freshness.ServingState != "fresh" || len(dashboard.Summary) != 2 {
		t.Fatalf("unexpected mortality dashboard: %#v", dashboard)
	}

	replay, err := service.Sync(ctx, mortalityapp.SyncMortalityInput{
		TenantID:       mortalityTestTenantID,
		ActorID:        mortalityTestActorID,
		IdempotencyKey: "idem-mortality-sync-0001",
		TraceID:        "trace-mortality-sync-replay",
		RawBody:        []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("Sync mortality replay: %v", err)
	}
	if !replay.Idempotency.Replayed || replay.SyncRunID != result.SyncRunID {
		t.Fatalf("unexpected mortality replay: %#v", replay)
	}
	if _, err := service.Sync(ctx, mortalityapp.SyncMortalityInput{
		TenantID:       mortalityTestTenantID,
		ActorID:        mortalityTestActorID,
		IdempotencyKey: "idem-mortality-sync-0002",
		TraceID:        "trace-mortality-sync-0002",
		RawBody:        []byte(`{}`),
	}); err != nil {
		t.Fatalf("Sync mortality second: %v", err)
	}
	if got := countMortalityRows(t, pool, `SELECT count(*) FROM mortality_events WHERE tenant_id = $1::uuid`, mortalityTestTenantID); got != 3 {
		t.Fatalf("mortality events after second sync = %d", got)
	}
	if got := countMortalityRows(t, pool, `SELECT count(*) FROM location_review_items WHERE tenant_id = $1::uuid AND source_context = 'legacy_bq_mortality' AND normalized_source_label = 'unknown mortality pen' AND status = 'open'`, mortalityTestTenantID); got != 1 {
		t.Fatalf("review rows after second sync = %d", got)
	}
}

func seedMortalityLocationAlias(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'MORTALITY_SYNC_SHED', 'Mortality Sync Shed', 'active')
ON CONFLICT (location_id) DO NOTHING`, mortalityTestLocationID, mortalityTestTenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination, usable_for_sop)
VALUES ($1::uuid, $2::uuid, true, true, true, true)
ON CONFLICT (location_id) DO NOTHING`, mortalityTestTenantID, mortalityTestLocationID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO location_aliases (tenant_id, canonical_location_id, alias_code, source_context, status)
VALUES ($1::uuid, $2::uuid, 'Mortality Shed A', 'legacy_bq_mortality', 'active')
ON CONFLICT DO NOTHING`, mortalityTestTenantID, mortalityTestLocationID); err != nil {
		t.Fatal(err)
	}
}

func seedMortalitySourceRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	rows := []struct {
		key     string
		payload string
		hash    string
	}{
		{
			key: "stable-death-a",
			payload: `{
				"event_date":"2026-06-12",
				"event_type":"death",
				"source_goat_identifier":"M-100",
				"source_identifier_kind":"old_tag",
				"farm_label":"Mortality Shed A",
				"housing_label":"Mortality Shed A",
				"breed":"Boer",
				"sex":"female"
			}`,
			hash: "sha256:mortality-stable-a",
		},
		{
			key: "stable-death-a-duplicate",
			payload: `{
				"event_date":"2026-06-12",
				"event_type":"death",
				"source_goat_identifier":"M-100",
				"source_identifier_kind":"old_tag",
				"farm_label":"Mortality Shed A",
				"housing_label":"Mortality Shed A",
				"breed":"Boer",
				"sex":"female"
			}`,
			hash: "sha256:mortality-stable-a-duplicate",
		},
		{
			key: "candidate-one",
			payload: `{
				"event_date":"2026-06-12",
				"event_type":"death",
				"farm_label":"Mortality Shed A",
				"housing_label":"Unknown Mortality Pen",
				"breed":"Sirohi",
				"sex":"male"
			}`,
			hash: "sha256:mortality-candidate-one",
		},
		{
			key: "candidate-two",
			payload: `{
				"event_date":"2026-06-12",
				"event_type":"death",
				"farm_label":"Mortality Shed A",
				"housing_label":"Unknown Mortality Pen",
				"breed":"Sirohi",
				"sex":"male"
			}`,
			hash: "sha256:mortality-candidate-two",
		},
	}
	for _, row := range rows {
		if _, err := pool.Exec(ctx, `
INSERT INTO mortality_source_rows (
  tenant_id, source_system, source_table, source_row_key,
  source_observed_at, source_watermark, payload_json, payload_hash, row_status
) VALUES (
  $1::uuid, 'legacy_bigquery', 'fixture_mortality_table', $2,
  '2026-06-12T10:00:00Z', '2026-06-12', $3::jsonb, $4, 'current'
)`, mortalityTestTenantID, row.key, row.payload, row.hash); err != nil {
			t.Fatal(err)
		}
	}
}

func startMortalitySyncDB(t *testing.T, ctx context.Context) (*pgxpool.Pool, *Repository) {
	t.Helper()
	container := "goatos-mortality-sync-test-" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "")
	postgresImage := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if postgresImage == "" {
		postgresImage = mortalityTestPostgresImage
	}
	runMortality(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", postgresImage)
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
		t.Fatalf("postgres container did not become ready:\n%s", runMortalityOutput(t, "docker", "logs", container))
	}
	applyMortalityMigrations(t, container)
	pool := openMortalityPool(t, ctx, container)
	return pool, NewRepository(pool, 5*time.Second)
}

func applyMortalityMigrations(t *testing.T, container string) {
	t.Helper()
	root := mortalityRepoRoot(t)
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
		mortalityPSQL(t, container, extractMortalityGooseUp(string(sqlBytes)))
	}
}

func openMortalityPool(t *testing.T, ctx context.Context, container string) *pgxpool.Pool {
	t.Helper()
	out := runMortalityOutput(t, "docker", "port", container, "5432/tcp")
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

func mortalityPSQL(t *testing.T, container, sqlText string) {
	t.Helper()
	cmd := exec.Command("docker", "exec", "-i", container, "psql", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos")
	cmd.Stdin = strings.NewReader(sqlText)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("psql failed: %v\n%s", err, stderr.String())
	}
}

func extractMortalityGooseUp(sqlText string) string {
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

func mortalityRepoRoot(t *testing.T) string {
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

func countMortalityRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func runMortality(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, stderr.String())
	}
}

func runMortalityOutput(t *testing.T, name string, args ...string) string {
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
