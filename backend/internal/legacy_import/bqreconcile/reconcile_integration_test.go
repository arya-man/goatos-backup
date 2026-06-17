package bqreconcile

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	bqReconcileTestPostgresImage = "postgres:16.9-alpine"
	bqReconcileTestTenantID      = "00000000-0000-4000-8000-000000000001"
	bqReconcileTestCustodianID   = "00000000-0000-4000-8000-000000001001"
)

func TestCloseStaleLifecycleConflictsAuditsCleanedGoat(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool := startBQReconcileTestDB(t, ctx)
	defer pool.Close()

	goatID := "00000000-0000-4000-8000-00000000a101"
	conflictID := "00000000-0000-4000-8000-00000000c101"
	traceID := "test-stale-lifecycle-cleanup"
	seedStaleLifecycleConflict(t, pool, goatID, conflictID)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	closed, cleaned, err := closeStaleLifecycleConflicts(ctx, tx, bqReconcileTestTenantID, traceID, map[string]struct{}{})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if closed != 1 || cleaned != 1 {
		t.Fatalf("closed=%d cleaned=%d want 1/1", closed, cleaned)
	}
	if got := queryScalarString(t, pool, "SELECT identity_state FROM goats WHERE goat_id = $1", goatID); got != "clean" {
		t.Fatalf("goat identity_state=%q want clean", got)
	}
	if got := queryScalarString(t, pool, "SELECT state FROM identity_conflicts WHERE conflict_id = $1", conflictID); got != "closed" {
		t.Fatalf("conflict state=%q want closed", got)
	}
	if got := countRows(t, pool, `
SELECT count(*)::int
FROM audit_log
WHERE tenant_id = $1::uuid
  AND action = 'goat.bq_lifecycle_conflict_cleaned'
  AND resource_type = 'goat'
  AND resource_id = $2::uuid
  AND trace_id = $3`, bqReconcileTestTenantID, goatID, traceID); got != 1 {
		t.Fatalf("cleanup audit rows=%d want 1", got)
	}
}

func TestUpdateExistingBackfillLifecyclesBumpsRowVersionAndAudits(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool := startBQReconcileTestDB(t, ctx)
	defer pool.Close()

	goatID := "00000000-0000-4000-8000-00000000b101"
	traceID := "test-old-tag-backfill-lifecycle-repair"
	seedExistingBackfillGoat(t, pool, goatID, "G-009952", "952", "park:CPT", "inactive")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := updateExistingBackfillLifecycles(ctx, tx, bqReconcileTestTenantID, traceID, []Candidate{
		{
			RowNumber: 2,
			Source:    "census_plus_bq_unique_farm",
			Farm:      "CPT",
			ScopeKey:  "park:CPT",
			OldTag:    "952",
			Breed:     "Beetal",
			Gender:    "Male",
			Status:    "Active",
		},
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("updated=%d want 1", updated)
	}
	if got := queryScalarString(t, pool, "SELECT lifecycle_status FROM goats WHERE goat_id = $1", goatID); got != "alive" {
		t.Fatalf("goat lifecycle=%q want alive", got)
	}
	if got := queryScalarString(t, pool, "SELECT row_version::text FROM goats WHERE goat_id = $1", goatID); got != "2" {
		t.Fatalf("goat row_version=%q want 2", got)
	}
	if got := countRows(t, pool, `
SELECT count(*)::int
FROM audit_log
WHERE tenant_id = $1::uuid
  AND action = 'goat.old_tag_backfill_lifecycle_repaired'
  AND resource_type = 'goat'
  AND resource_id = $2::uuid
  AND before_state->>'lifecycle_status' = 'inactive'
  AND after_state->>'lifecycle_status' = 'alive'
  AND before_state->>'row_version' = '1'
  AND after_state->>'row_version' = '2'
  AND metadata->>'candidate_source' = 'census_plus_bq_unique_farm'
  AND metadata->>'candidate_row' = '2'
  AND trace_id = $3`, bqReconcileTestTenantID, goatID, traceID); got != 1 {
		t.Fatalf("repair audit rows=%d want 1", got)
	}
}

func seedStaleLifecycleConflict(t *testing.T, pool *pgxpool.Pool, goatID, conflictID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO goats (
  goat_id,
  tenant_id,
  species,
  breed,
  sex,
  lifecycle_status,
  identity_state,
  custodian_party_id
) VALUES (
  $1::uuid,
  $2::uuid,
  'goat',
  'Sojat',
  'female',
  'alive',
  'needs_review',
  $3::uuid
)`, goatID, bqReconcileTestTenantID, bqReconcileTestCustodianID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO identity_conflicts (
  conflict_id,
  tenant_id,
  conflict_type,
  severity,
  state,
  goat_ids,
  evidence
) VALUES (
  $3::uuid,
  $2::uuid,
  'status_mismatch',
  'medium',
  'open',
  ARRAY[$1::uuid],
  '{"source_context":"bq_reconcile_identifier_lifecycle_conflict","reason":"test_stale"}'::jsonb
)`, goatID, bqReconcileTestTenantID, conflictID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO identity_conflict_goats (
  conflict_id,
  tenant_id,
  goat_id,
  role
) VALUES (
  $3::uuid,
  $2::uuid,
  $1::uuid,
  'affected'
)`, goatID, bqReconcileTestTenantID, conflictID); err != nil {
		t.Fatal(err)
	}
}

func seedExistingBackfillGoat(t *testing.T, pool *pgxpool.Pool, goatID, displayID, oldTag, scopeKey, lifecycle string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO goats (
  goat_id,
  tenant_id,
  display_id,
  species,
  breed,
  sex,
  lifecycle_status,
  identity_state,
  custodian_party_id
) VALUES (
  $1::uuid,
  $2::uuid,
  $3,
  'goat',
  'Beetal',
  'male',
  $4,
  'clean',
  $5::uuid
)`, goatID, bqReconcileTestTenantID, displayID, lifecycle, bqReconcileTestCustodianID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO goat_identifiers (
  tenant_id,
  goat_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  is_primary_for_goat,
  status,
  valid_from,
  source_system,
  source_record_id,
  normalizer_version,
  confidence
) VALUES (
  $1::uuid,
  $2::uuid,
  'old_tag',
  $3,
  $3,
  $4,
  true,
  'active',
  now(),
  $5,
  $6,
  $7,
  0.7
)`,
		bqReconcileTestTenantID,
		goatID,
		oldTag,
		scopeKey,
		backfillSourceSystem,
		backfillSourceContext+":"+scopeKey+":"+oldTag,
		backfillNormalizerVersion,
	); err != nil {
		t.Fatal(err)
	}
}

func startBQReconcileTestDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	image := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if image == "" {
		image = bqReconcileTestPostgresImage
	}
	container := "goatos-bqreconcile-test-" + time.Now().UTC().Format("20060102150405") + "-" + strings.ToLower(randomTestSuffix(t))
	run(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", image)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	})
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos").Run() == nil {
			applyMigrations(t, container)
			return openPool(t, ctx, container)
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("postgres container did not become ready:\n%s", runOutput(t, "docker", "logs", container))
	return nil
}

func randomTestSuffix(t *testing.T) string {
	t.Helper()
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}

func applyMigrations(t *testing.T, container string) {
	t.Helper()
	root := repoRoot(t)
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
		psql(t, container, extractGooseUp(string(sqlBytes)))
	}
}

func openPool(t *testing.T, ctx context.Context, container string) *pgxpool.Pool {
	t.Helper()
	out := runOutput(t, "docker", "port", container, "5432/tcp")
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

func psql(t *testing.T, container, sqlText string) {
	t.Helper()
	if strings.TrimSpace(sqlText) == "" {
		return
	}
	cmd := exec.Command("docker", "exec", "-i", container, "psql", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos")
	cmd.Stdin = strings.NewReader(sqlText)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("psql failed: %v\n%s", err, stderr.String())
	}
}

func extractGooseUp(sqlText string) string {
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

func repoRoot(t *testing.T) string {
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

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if output, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, string(output))
	}
}

func runOutput(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func queryScalarString(t *testing.T, pool *pgxpool.Pool, query string, args ...any) string {
	t.Helper()
	var out string
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
