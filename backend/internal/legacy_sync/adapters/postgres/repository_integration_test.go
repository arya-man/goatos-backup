package postgres

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/legacy_sync/domain"
	"github.com/vgoats/goatos/backend/internal/legacy_sync/ports"
)

const (
	legacySyncDefaultPostgresImage = "postgres:16.9-alpine"
	legacySyncMeshaTenant          = "00000000-0000-4000-8000-000000000001"
	legacySyncActorID              = "90000000-0000-4000-8000-000000000101"
)

func TestRepositoryPathsWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool := startLegacySyncDB(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	t.Run("list sources hides bridge config and exposes unknown sources", func(t *testing.T) {
		sources, err := repo.ListSources(ctx, ports.SourceFilter{TenantID: legacySyncMeshaTenant})
		if err != nil {
			t.Fatalf("ListSources: %v", err)
		}
		if len(sources) != 4 {
			t.Fatalf("seeded sources=%d want 4", len(sources))
		}
		foundActiveCount := false
		for _, source := range sources {
			if source.SourceRef == nil || *source.SourceRef == "" {
				t.Fatalf("source %s missing generic source_ref: %#v", source.SourceID, source)
			}
			if strings.Contains(*source.SourceRef, "goatos-sheets") || strings.Contains(*source.SourceRef, "goatsDB") || strings.Contains(*source.SourceRef, "bounded_bq") {
				t.Fatalf("source_ref leaked backend bridge config: %s", *source.SourceRef)
			}
			if source.SourceID == "phase1_active_count_parity" {
				foundActiveCount = true
				if source.GreenWithinSeconds != 46800 || source.YellowWithinSeconds != 54000 {
					t.Fatalf("active-count thresholds=%d/%d want 46800/54000", source.GreenWithinSeconds, source.YellowWithinSeconds)
				}
			}
		}
		if !foundActiveCount {
			t.Fatal("phase1_active_count_parity source was not seeded")
		}

		if _, err := pool.Exec(ctx, `
INSERT INTO legacy_sync_source_watermarks (
  tenant_id,
  source_id,
  last_success_window_start,
  last_success_window_end,
  last_success_at,
  checkpoint
) VALUES (
  $1,
  'phase1_current_location_evidence',
  now() - interval '20 minutes',
  now() - interval '10 minutes',
  now() - interval '5 minutes',
  '{}'::jsonb
)`, legacySyncMeshaTenant); err != nil {
			t.Fatalf("seed source watermark: %v", err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO legacy_sync_source_status (
  tenant_id,
  source_id,
  freshness_status,
  status_reason,
  source_watermark_at,
  observed_at,
  rows_seen,
  is_unknown_source
) VALUES (
  $1,
  'phase1_current_location_evidence',
  'red',
  'stale status row should not hide a fresh success watermark',
  now() - interval '2 hours',
  now() - interval '2 hours',
  99,
  false
)`, legacySyncMeshaTenant); err != nil {
			t.Fatalf("seed stale source status: %v", err)
		}
		freshSources, err := repo.ListSources(ctx, ports.SourceFilter{TenantID: legacySyncMeshaTenant, Domain: domain.DomainCurrentLocation})
		if err != nil {
			t.Fatalf("ListSources with watermark: %v", err)
		}
		if len(freshSources) != 1 {
			t.Fatalf("current-location sources=%d want 1: %#v", len(freshSources), freshSources)
		}
		fresh := freshSources[0]
		if fresh.FreshnessStatus != domain.FreshnessGreen {
			t.Fatalf("watermark-derived freshness=%s want green: %#v", fresh.FreshnessStatus, fresh)
		}
		if fresh.StatusReason != "source watermark is within green threshold" {
			t.Fatalf("watermark-derived reason=%q want green threshold", fresh.StatusReason)
		}
		if fresh.SourceWatermarkAt == nil {
			t.Fatalf("watermark-derived source should expose source_watermark_at: %#v", fresh)
		}
		if time.Since(*fresh.SourceWatermarkAt) > 15*time.Minute {
			t.Fatalf("source_watermark_at=%s should use fresh success watermark over stale status", fresh.SourceWatermarkAt.Format(time.RFC3339))
		}

		if _, err := pool.Exec(ctx, `
INSERT INTO legacy_sync_source_status (
  tenant_id,
  source_id,
  freshness_status,
  status_reason,
  source_watermark_at,
  observed_at,
  rows_seen,
  is_unknown_source
) VALUES (
  $1,
  'unregistered_live_source',
  'green',
  '',
  now(),
  now(),
  7,
  true
)`, legacySyncMeshaTenant); err != nil {
			t.Fatalf("seed unknown source status: %v", err)
		}
		unknownSources, err := repo.ListSources(ctx, ports.SourceFilter{TenantID: legacySyncMeshaTenant, Domain: "unknown"})
		if err != nil {
			t.Fatalf("ListSources unknown: %v", err)
		}
		if len(unknownSources) != 1 {
			t.Fatalf("unknown sources=%d want 1: %#v", len(unknownSources), unknownSources)
		}
		unknown := unknownSources[0]
		if !unknown.IsUnknownSource || unknown.FreshnessStatus != domain.FreshnessUnknown || unknown.StatusReason != domain.EvidenceReasonUnregisteredSource {
			t.Fatalf("unknown source was not safely flagged: %#v", unknown)
		}
	})

	t.Run("create detail and terminal cancel paths", func(t *testing.T) {
		run, err := repo.CreateRun(ctx, ports.CreateRunParams{
			TenantID:           legacySyncMeshaTenant,
			ActorID:            legacySyncActorID,
			Mode:               domain.ModeDryRun,
			Domain:             domain.DomainIdentity,
			Status:             domain.StatusCompleted,
			CounterCheckStatus: domain.CounterCheckSkipped,
			FreshnessStatus:    domain.FreshnessGreen,
			TraceID:            "legacy-sync-repo-test",
			Steps: []ports.CreateStepParams{
				{
					StepName:  "source_window",
					Status:    domain.StepCompleted,
					Details:   map[string]any{"bounded_window": true},
					Completed: true,
				},
				{
					StepName:  "plan_delta",
					Status:    domain.StepCompleted,
					Details:   map[string]any{"rows_planned": 0},
					Completed: true,
				},
			},
		})
		if err != nil {
			t.Fatalf("CreateRun completed: %v", err)
		}
		if run.Status != domain.StatusCompleted || run.CompletedAt == nil {
			t.Fatalf("created run should be terminal completed: %#v", run)
		}

		if _, err := pool.Exec(ctx, `
INSERT INTO legacy_sync_run_conflicts (
  tenant_id,
  sync_run_id,
  source_id,
  source_record_id,
  source_conflict_key,
  evidence_reason,
  result,
  old_goatos_value,
  new_legacy_value
) VALUES (
  $1,
  $2,
  'phase1_identity_attribute_evidence',
  'record-1',
  'legacy-sync-repo-test-preserved-human-decision',
  'legacy_changed_after_human_review',
  'preserved_human_decision',
  'goat-os-value',
  'legacy-value'
)`, legacySyncMeshaTenant, run.SyncRunID); err != nil {
			t.Fatalf("seed source correction log: %v", err)
		}

		runs, err := repo.ListRuns(ctx, ports.RunFilter{TenantID: legacySyncMeshaTenant, Limit: 10})
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if len(runs) == 0 || runs[0].SyncRunID != run.SyncRunID {
			t.Fatalf("latest run not returned first: %#v", runs)
		}

		detail, err := repo.GetRun(ctx, legacySyncMeshaTenant, run.SyncRunID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if len(detail.Steps) != 2 {
			t.Fatalf("detail steps=%d want 2", len(detail.Steps))
		}
		if len(detail.Sources) < 4 {
			t.Fatalf("detail sources=%d want at least 4", len(detail.Sources))
		}
		if len(detail.SourceCorrectionLog) != 1 || detail.SourceCorrectionLog[0].Result != "preserved_human_decision" {
			t.Fatalf("unexpected source correction log: %#v", detail.SourceCorrectionLog)
		}

		canceled, err := repo.CancelRun(ctx, legacySyncMeshaTenant, run.SyncRunID)
		if err != nil {
			t.Fatalf("CancelRun terminal: %v", err)
		}
		if canceled.Status != domain.StatusCompleted {
			t.Fatalf("terminal cancel status=%s want completed", canceled.Status)
		}
		if canceled.CancelRequestedAt != nil {
			t.Fatalf("terminal cancel should not stamp cancel_requested_at: %#v", canceled)
		}
	})

	t.Run("cancel running run marks it canceled", func(t *testing.T) {
		run, err := repo.CreateRun(ctx, ports.CreateRunParams{
			TenantID:           legacySyncMeshaTenant,
			ActorID:            legacySyncActorID,
			Mode:               domain.ModeExecute,
			Domain:             domain.DomainLifecycle,
			Status:             domain.StatusRunning,
			CounterCheckStatus: domain.CounterCheckPending,
			FreshnessStatus:    domain.FreshnessYellow,
			TraceID:            "legacy-sync-repo-test-running",
		})
		if err != nil {
			t.Fatalf("CreateRun running: %v", err)
		}
		canceled, err := repo.CancelRun(ctx, legacySyncMeshaTenant, run.SyncRunID)
		if err != nil {
			t.Fatalf("CancelRun running: %v", err)
		}
		if canceled.Status != domain.StatusCanceled {
			t.Fatalf("running cancel status=%s want canceled", canceled.Status)
		}
		if canceled.CancelRequestedAt == nil || canceled.CompletedAt == nil {
			t.Fatalf("running cancel should stamp cancel/completed times: %#v", canceled)
		}
	})

	t.Run("source correction log key is unique per run", func(t *testing.T) {
		firstRun, err := repo.CreateRun(ctx, ports.CreateRunParams{
			TenantID:           legacySyncMeshaTenant,
			ActorID:            legacySyncActorID,
			Mode:               domain.ModeDryRun,
			Domain:             domain.DomainIdentity,
			Status:             domain.StatusCompleted,
			CounterCheckStatus: domain.CounterCheckSkipped,
			FreshnessStatus:    domain.FreshnessGreen,
			TraceID:            "legacy-sync-repo-log-unique-first",
		})
		if err != nil {
			t.Fatalf("CreateRun first log run: %v", err)
		}
		const sourceConflictKey = "legacy-sync-repo-test-refreshable-key"
		insertRunConflict(t, pool, firstRun.SyncRunID, sourceConflictKey, "conflict_opened")

		if _, err := pool.Exec(ctx, `
INSERT INTO legacy_sync_run_conflicts (
  tenant_id,
  sync_run_id,
  source_id,
  source_record_id,
  source_conflict_key,
  evidence_reason,
  result
) VALUES (
  $1,
  $2,
  'phase1_identity_attribute_evidence',
  'record-duplicate',
  $3,
  'legacy_changed_after_human_review',
  'conflict_refreshed'
)`, legacySyncMeshaTenant, firstRun.SyncRunID, sourceConflictKey); err == nil {
			t.Fatal("duplicate source_conflict_key within the same run should fail")
		}

		secondRun, err := repo.CreateRun(ctx, ports.CreateRunParams{
			TenantID:           legacySyncMeshaTenant,
			ActorID:            legacySyncActorID,
			Mode:               domain.ModeDryRun,
			Domain:             domain.DomainIdentity,
			Status:             domain.StatusCompleted,
			CounterCheckStatus: domain.CounterCheckSkipped,
			FreshnessStatus:    domain.FreshnessGreen,
			TraceID:            "legacy-sync-repo-log-unique-second",
		})
		if err != nil {
			t.Fatalf("CreateRun second log run: %v", err)
		}
		insertRunConflict(t, pool, secondRun.SyncRunID, sourceConflictKey, "conflict_refreshed")
	})
}

func insertRunConflict(t *testing.T, pool *pgxpool.Pool, runID, sourceConflictKey, result string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO legacy_sync_run_conflicts (
  tenant_id,
  sync_run_id,
  source_id,
  source_record_id,
  source_conflict_key,
  evidence_reason,
  result
) VALUES (
  $1,
  $2,
  'phase1_identity_attribute_evidence',
  'record-' || $3,
  $3,
  'legacy_changed_after_human_review',
  $4
)`, legacySyncMeshaTenant, runID, sourceConflictKey, result); err != nil {
		t.Fatalf("insert run conflict %s in run %s: %v", sourceConflictKey, runID, err)
	}
}

func startLegacySyncDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container := fmt.Sprintf("goatos-legacy-sync-repo-test-%d", time.Now().UnixNano())
	postgresImage := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if postgresImage == "" {
		postgresImage = legacySyncDefaultPostgresImage
	}
	runLegacySyncCommand(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", postgresImage)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	})
	waitForLegacySyncPostgres(t, container)
	applyLegacySyncMigrations(t, container)
	return openLegacySyncPool(t, ctx, container)
}

func waitForLegacySyncPostgres(t *testing.T, container string) {
	t.Helper()
	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos").Run() == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres container did not become ready:\n%s", legacySyncCommandOutput(t, "docker", "logs", container))
}

func applyLegacySyncMigrations(t *testing.T, container string) {
	t.Helper()
	root := legacySyncRepoRoot(t)
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
		legacySyncPSQL(t, container, extractLegacySyncGooseUp(string(sqlBytes)))
	}
}

func openLegacySyncPool(t *testing.T, ctx context.Context, container string) *pgxpool.Pool {
	t.Helper()
	out := legacySyncCommandOutput(t, "docker", "port", container, "5432/tcp")
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

func legacySyncPSQL(t *testing.T, container, sqlText string) {
	t.Helper()
	cmd := exec.Command("docker", "exec", "-i", container, "psql", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos")
	cmd.Stdin = strings.NewReader(sqlText)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("psql failed: %v\n%s\nSQL:\n%s", err, out, sqlText)
	}
}

func extractLegacySyncGooseUp(sqlText string) string {
	var out []string
	inUp := false
	for _, line := range strings.Split(sqlText, "\n") {
		switch {
		case strings.HasPrefix(line, "-- +goose Up"):
			inUp = true
			continue
		case strings.HasPrefix(line, "-- +goose Down"):
			inUp = false
		}
		if inUp {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func legacySyncRepoRoot(t *testing.T) string {
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

func runLegacySyncCommand(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, stderr.String())
	}
}

func legacySyncCommandOutput(t *testing.T, name string, args ...string) string {
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
