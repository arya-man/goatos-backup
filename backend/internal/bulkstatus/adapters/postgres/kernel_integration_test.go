package postgres

// Focused integration tests for the bulk status-update kernel against a real
// Postgres (docker) with all migrations applied. These exercise the full
// worker -> identitybridge -> identity transition path so the real domain
// event, decision record, outbox message, vocabulary check and no-clobber
// guardrail all fire (the app-level kernel_test.go covers the same logic with
// fakes; this proves the wiring end to end).
//
// The whole package shares ONE container (TestMain). Each test creates its own
// tenant so seeded goats/jobs never collide. If docker is unavailable the tests
// skip, matching the repo's other *_integration_test.go conventions.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	bulkapp "github.com/vgoats/goatos/backend/internal/bulkstatus/app"
	"github.com/vgoats/goatos/backend/internal/bulkstatus/identitybridge"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	itParty     = "00000000-0000-4000-8000-000000001001" // seeded Mesha party (parties are not tenant scoped)
	itActor     = "90000000-0000-4000-8000-000000000101" // arbitrary actor uuid (no FK)
	reproTarget = "pregnant"
	reproStart  = "non_pregnant"
)

var (
	itPool      *pgxpool.Pool
	itContainer string
)

func TestMain(m *testing.M) {
	os.Exit(func() int {
		if _, err := exec.LookPath("docker"); err != nil {
			fmt.Fprintln(os.Stderr, "docker not available; bulkstatus integration tests will skip")
			return m.Run()
		}
		image := os.Getenv("GOATOS_POSTGRES_IMAGE")
		if image == "" {
			image = "postgres:16.9-alpine"
		}
		itContainer = fmt.Sprintf("goatos-bulkstatus-it-%d", time.Now().UnixNano())
		if err := exec.Command("docker", "run", "--rm", "--name", itContainer,
			"-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos",
			"-p", "127.0.0.1::5432", "-d", image).Run(); err != nil {
			fmt.Fprintln(os.Stderr, "docker run failed; skipping:", err)
			return m.Run()
		}
		defer func() { _ = exec.Command("docker", "rm", "-f", itContainer).Run() }()
		if !itWaitReady(itContainer) {
			fmt.Fprintln(os.Stderr, "postgres container did not become ready; skipping")
			return m.Run()
		}
		if err := itApplyMigrations(itContainer); err != nil {
			fmt.Fprintln(os.Stderr, "migration apply failed:", err)
			return 1
		}
		ctx := context.Background()
		pool, err := itOpenPool(ctx, itContainer)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pool open failed:", err)
			return 1
		}
		defer pool.Close()
		itPool = pool
		return m.Run()
	}())
}

func requirePool(t *testing.T) {
	t.Helper()
	if itPool == nil {
		t.Skip("docker postgres unavailable")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func drainSettings() bulkapp.WorkerSettings {
	return bulkapp.WorkerSettings{
		Concurrency: 8, RowsPerSecond: 0, Burst: 1, BatchSize: 100,
		MaxRetries: 3, MaxIterations: 100000, ClaimLease: 30 * time.Second,
	}
}

func buildWorker(settings bulkapp.WorkerSettings) *bulkapp.WorkerService {
	bulkRepo := NewRepository(itPool, 30*time.Second)
	idSvc := identityapp.NewService(identitypg.NewRepository(itPool, 30*time.Second))
	return bulkapp.NewWorkerService(bulkRepo, identitybridge.New(idSvc), settings, testLogger())
}

func newTenant(t *testing.T, ctx context.Context) string {
	t.Helper()
	var id string
	if err := itPool.QueryRow(ctx,
		`INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), $1, 'active') RETURNING tenant_id::text`,
		"bulkstatus-it-"+t.Name()).Scan(&id); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return id
}

// copyGoats bulk-loads n goats via the COPY protocol (goat_id/display_id take
// their column defaults). Returns nothing: assertions run in aggregate SQL.
func copyGoats(t *testing.T, ctx context.Context, tenant, species, repro string, n int) {
	t.Helper()
	conn, err := itPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	copied, err := conn.Conn().CopyFrom(ctx,
		pgx.Identifier{"goats"},
		[]string{"tenant_id", "species", "sex", "lifecycle_status", "reproductive_status", "custodian_party_id"},
		pgx.CopyFromSlice(n, func(int) ([]any, error) {
			return []any{tenant, species, "female", "alive", repro, itParty}, nil
		}))
	if err != nil {
		t.Fatalf("copy goats: %v", err)
	}
	if int(copied) != n {
		t.Fatalf("copied %d goats, want %d", copied, n)
	}
}

// enqueueReproductiveJob seeds a job + one row per eligible goat with a set-based
// INSERT ... SELECT that captures each goat's live row_version as
// expected_row_version (the same no-clobber capture the real EnqueueJob does).
func enqueueReproductiveJob(t *testing.T, ctx context.Context, tenant, actor, target string) (string, int) {
	t.Helper()
	var jobID string
	if err := itPool.QueryRow(ctx, `
INSERT INTO bulk_status_job (tenant_id, actor_id, axis, params, total_rows, state, idempotency_key)
VALUES ($1::uuid, $2::uuid, 'reproductive', '{}'::jsonb, 0, 'pending', $3)
RETURNING bulk_status_job_id::text`, tenant, actor, "it-"+t.Name()+"-"+target).Scan(&jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	ct, err := itPool.Exec(ctx, `
INSERT INTO bulk_status_job_row (tenant_id, job_id, goat_id, axis, target, expected_row_version, row_state)
SELECT tenant_id, $1::uuid, goat_id, 'reproductive', $2, row_version, 'pending'
FROM goats
WHERE tenant_id = $3::uuid AND reproductive_status IS DISTINCT FROM $2`, jobID, target, tenant)
	if err != nil {
		t.Fatalf("insert job rows: %v", err)
	}
	total := int(ct.RowsAffected())
	if _, err := itPool.Exec(ctx, `UPDATE bulk_status_job SET total_rows = $2 WHERE bulk_status_job_id = $1::uuid`, jobID, total); err != nil {
		t.Fatalf("update total_rows: %v", err)
	}
	return jobID, total
}

func scalarInt(t *testing.T, ctx context.Context, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := itPool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return n
}

// ---- end-to-end apply ---------------------------------------------------------

func TestBulkStatusWorkerAppliesReproductiveEndToEnd(t *testing.T) {
	requirePool(t)
	ctx := context.Background()
	tenant := newTenant(t, ctx)
	const n = 200
	copyGoats(t, ctx, tenant, "goat", reproStart, n)
	jobID, total := enqueueReproductiveJob(t, ctx, tenant, itActor, reproTarget)
	if total != n {
		t.Fatalf("enqueued %d rows, want %d", total, n)
	}

	drain, err := buildWorker(drainSettings()).RunUntilDrained(ctx, tenant, jobID)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if drain.FinalCounts.State != bulkapp.JobStateCompleted || drain.FinalCounts.Remaining != 0 {
		t.Fatalf("job not completed cleanly: %+v", drain.FinalCounts)
	}
	if drain.FinalCounts.Applied != n {
		// Surface the actual row failure reason to make a kernel defect obvious.
		var reason string
		_ = itPool.QueryRow(ctx, `SELECT COALESCE(failure_reason,'') FROM bulk_status_job_row WHERE job_id=$1::uuid AND row_state<>'applied' LIMIT 1`, jobID).Scan(&reason)
		t.Fatalf("applied=%d want %d (first non-applied failure_reason=%q)", drain.FinalCounts.Applied, n, reason)
	}

	// Zero rows stuck in a non-terminal (or failed/skipped) state.
	if stuck := scalarInt(t, ctx, `SELECT count(*) FROM bulk_status_job_row WHERE job_id=$1::uuid AND row_state <> 'applied'`, jobID); stuck != 0 {
		t.Fatalf("expected 0 non-applied rows, got %d", stuck)
	}
	// event_id set on every applied row, and distinct (one event per row).
	if missing := scalarInt(t, ctx, `SELECT count(*) FROM bulk_status_job_row WHERE job_id=$1::uuid AND event_id IS NULL`, jobID); missing != 0 {
		t.Fatalf("expected event_id on every applied row, %d missing", missing)
	}
	if distinct := scalarInt(t, ctx, `SELECT count(DISTINCT event_id) FROM bulk_status_job_row WHERE job_id=$1::uuid`, jobID); distinct != n {
		t.Fatalf("expected %d distinct event_ids, got %d", n, distinct)
	}
	// Exactly one goat.reproductive.changed identity event per applied row.
	if ev := scalarInt(t, ctx, `
SELECT count(*) FROM goat_identity_events e
JOIN bulk_status_job_row r ON r.event_id = e.identity_event_id
WHERE r.job_id=$1::uuid AND e.event_type='goat.reproductive.changed'`, jobID); ev != n {
		t.Fatalf("expected %d reproductive events, got %d", n, ev)
	}
	// Zero missing outbox events for applied rows.
	if ob := scalarInt(t, ctx, `
SELECT count(*) FROM outbox_messages o
JOIN bulk_status_job_row r ON r.event_id = o.event_id
WHERE r.job_id=$1::uuid`, jobID); ob != n {
		t.Fatalf("expected %d outbox messages, got %d", n, ob)
	}
	// No double-apply: every goat advanced exactly one row_version and reached target.
	if applied := scalarInt(t, ctx, `SELECT count(*) FROM goats WHERE tenant_id=$1::uuid AND reproductive_status=$2 AND row_version=2`, tenant, reproTarget); applied != n {
		t.Fatalf("expected %d goats at target with row_version=2, got %d", n, applied)
	}
	if over := scalarInt(t, ctx, `SELECT count(*) FROM goats WHERE tenant_id=$1::uuid AND row_version > 2`, tenant); over != 0 {
		t.Fatalf("double-apply detected: %d goats have row_version > 2", over)
	}
}

// ---- per-goat reproductive endpoint emits event + outbox (recompute trigger) --

func TestReproductiveEndpointEmitsEventAndOutbox(t *testing.T) {
	requirePool(t)
	ctx := context.Background()
	tenant := newTenant(t, ctx)
	var goatID string
	if err := itPool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, species, sex, lifecycle_status, reproductive_status, custodian_party_id)
VALUES ($1::uuid, 'goat', 'female', 'alive', $2, $3::uuid) RETURNING goat_id::text`, tenant, reproStart, itParty).Scan(&goatID); err != nil {
		t.Fatalf("insert goat: %v", err)
	}
	idSvc := identityapp.NewService(identitypg.NewRepository(itPool, 30*time.Second))
	body := []byte(fmt.Sprintf(`{"reproductive_status":%q,"reason":"integration reproductive update","row_version":1,"evidence_refs":[{"evidence_type":"source_record","evidence_id":"it-repro-1","source_system":"integration"}]}`, reproTarget))
	resp, err := idSvc.ReproductiveGoat(ctx, identityapp.ReproductiveGoatInput{
		TenantID: tenant, ActorID: itActor, IdempotencyKey: "it-repro-endpoint-0001",
		TraceID: "it-trace", GoatID: goatID, RawBody: body,
	})
	if err != nil {
		t.Fatalf("ReproductiveGoat: %v", err)
	}
	if len(resp.Events) == 0 || resp.Events[0].EventType != "goat.reproductive.changed" {
		t.Fatalf("expected goat.reproductive.changed event, got %+v", resp.Events)
	}
	eventID := resp.Events[0].EventID
	if got := scalarInt(t, ctx, `SELECT count(*) FROM goats WHERE goat_id=$1::uuid AND reproductive_status=$2 AND row_version=2`, goatID, reproTarget); got != 1 {
		t.Fatalf("goat not updated to target: %d", got)
	}
	if got := scalarInt(t, ctx, `SELECT count(*) FROM goat_identity_events WHERE identity_event_id=$1::uuid AND event_type='goat.reproductive.changed'`, eventID); got != 1 {
		t.Fatalf("reproductive event not persisted: %d", got)
	}
	if got := scalarInt(t, ctx, `SELECT count(*) FROM outbox_messages WHERE event_id=$1::uuid AND event_type='goat.reproductive.changed'`, eventID); got != 1 {
		t.Fatalf("outbox message (recompute trigger) not emitted: %d", got)
	}
}

// ---- sheep species coverage ---------------------------------------------------

func TestSheepSpeciesCoverage(t *testing.T) {
	requirePool(t)
	ctx := context.Background()
	tenant := newTenant(t, ctx)
	const n = 25
	copyGoats(t, ctx, tenant, "sheep", reproStart, n)
	jobID, total := enqueueReproductiveJob(t, ctx, tenant, itActor, reproTarget)
	if total != n {
		t.Fatalf("enqueued %d rows, want %d", total, n)
	}
	drain, err := buildWorker(drainSettings()).RunUntilDrained(ctx, tenant, jobID)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if drain.FinalCounts.Applied != n || drain.FinalCounts.Remaining != 0 {
		t.Fatalf("sheep apply incomplete: %+v", drain.FinalCounts)
	}
	if got := scalarInt(t, ctx, `SELECT count(*) FROM goats WHERE tenant_id=$1::uuid AND species='sheep' AND reproductive_status=$2 AND row_version=2`, tenant, reproTarget); got != n {
		t.Fatalf("expected %d sheep updated, got %d", n, got)
	}
}

// ---- row_version no-clobber ---------------------------------------------------

func TestRowVersionNoClobber(t *testing.T) {
	requirePool(t)
	ctx := context.Background()
	tenant := newTenant(t, ctx)
	var goatID string
	if err := itPool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, species, sex, lifecycle_status, reproductive_status, custodian_party_id)
VALUES ($1::uuid, 'goat', 'female', 'alive', $2, $3::uuid) RETURNING goat_id::text`, tenant, reproStart, itParty).Scan(&goatID); err != nil {
		t.Fatalf("insert goat: %v", err)
	}
	// Enqueue captures expected_row_version = 1.
	jobID, total := enqueueReproductiveJob(t, ctx, tenant, itActor, reproTarget)
	if total != 1 {
		t.Fatalf("expected 1 enqueued row, got %d", total)
	}
	// Simulate a concurrent write that advances the live row_version past the
	// captured expectation, so the bulk apply must NOT clobber it.
	if _, err := itPool.Exec(ctx, `UPDATE goats SET reproductive_status='mother', row_version=row_version+1 WHERE goat_id=$1::uuid`, goatID); err != nil {
		t.Fatalf("concurrent bump: %v", err)
	}
	drain, err := buildWorker(drainSettings()).RunUntilDrained(ctx, tenant, jobID)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if drain.FinalCounts.Skipped != 1 || drain.FinalCounts.Applied != 0 {
		t.Fatalf("expected the stale row to be skipped, got %+v", drain.FinalCounts)
	}
	// The concurrent write is intact (not clobbered to the bulk target).
	var status string
	var version int
	if err := itPool.QueryRow(ctx, `SELECT reproductive_status, row_version FROM goats WHERE goat_id=$1::uuid`, goatID).Scan(&status, &version); err != nil {
		t.Fatalf("read goat: %v", err)
	}
	if status != "mother" || version != 2 {
		t.Fatalf("no-clobber violated: status=%q version=%d (want mother/2)", status, version)
	}
}

// ---- enqueue idempotency: duplicate delivery / replay -------------------------

func TestEnqueueIdempotentReplayAndConflict(t *testing.T) {
	requirePool(t)
	ctx := context.Background()
	tenant := newTenant(t, ctx)
	repo := NewRepository(itPool, 30*time.Second)
	rows := []bulkapp.EnqueueRow{
		{GoatID: "10000000-0000-4000-8000-000000000101", Target: reproTarget},
		{GoatID: "10000000-0000-4000-8000-000000000102", Target: reproTarget},
	}
	params := bulkapp.EnqueueJobParams{
		TenantID: tenant, ActorID: itActor, Axis: bulkapp.AxisReproductive,
		IdempotencyKey: "it-dup-0001", Fingerprint: "fp-abc", Rows: rows,
	}
	first, err := repo.EnqueueJob(ctx, params)
	if err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if first.Replayed || first.TotalRows != 2 {
		t.Fatalf("first enqueue should not be a replay: %+v", first)
	}
	// Exact replay: same key + same fingerprint -> original job, no new rows.
	replay, err := repo.EnqueueJob(ctx, params)
	if err != nil {
		t.Fatalf("replay enqueue: %v", err)
	}
	if !replay.Replayed || replay.JobID != first.JobID {
		t.Fatalf("replay must return original job: %+v", replay)
	}
	if rowCount := scalarInt(t, ctx, `SELECT count(*) FROM bulk_status_job_row WHERE job_id=$1::uuid`, first.JobID); rowCount != 2 {
		t.Fatalf("replay must not add rows, got %d", rowCount)
	}
	if jobCount := scalarInt(t, ctx, `SELECT count(*) FROM bulk_status_job WHERE tenant_id=$1::uuid AND idempotency_key='it-dup-0001'`, tenant); jobCount != 1 {
		t.Fatalf("replay must not add a job, got %d", jobCount)
	}
	// Same key, different payload fingerprint -> conflict.
	conflictParams := params
	conflictParams.Fingerprint = "fp-different"
	if _, err := repo.EnqueueJob(ctx, conflictParams); err == nil {
		t.Fatal("expected ErrIdempotencyConflict for same key + different fingerprint")
	}
}

// ---- import matched-existing reproductive update path -------------------------

func TestImportMatchedExistingReproductiveUpdatePath(t *testing.T) {
	requirePool(t)
	ctx := context.Background()
	tenant := newTenant(t, ctx)
	var goatID string
	if err := itPool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, species, sex, lifecycle_status, reproductive_status, custodian_party_id)
VALUES ($1::uuid, 'goat', 'female', 'alive', $2, $3::uuid) RETURNING goat_id::text`, tenant, reproStart, itParty).Scan(&goatID); err != nil {
		t.Fatalf("insert goat: %v", err)
	}
	if _, err := itPool.Exec(ctx, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'A1-IMP-001', 'A1-IMP-001', 'global', true, 'active', now(), 'test_v1')`, tenant, goatID); err != nil {
		t.Fatalf("insert identifier: %v", err)
	}
	idRepo := identitypg.NewRepository(itPool, 30*time.Second)
	match, err := idRepo.ResolveGoatForReproductiveBulkUpdate(ctx, identityports.ResolveReproductiveMatchCommand{
		TenantID: tenant,
		Identifiers: []identityports.AdminGoatCreateIdentifier{
			{IdentifierType: "animal_identifier_1", NormalizedValue: "A1-IMP-001"},
		},
	})
	if err != nil {
		t.Fatalf("resolve match: %v", err)
	}
	if !match.Matched || match.GoatID != goatID || match.RowVersion != 1 || match.CurrentReproductiveStatus != reproStart {
		t.Fatalf("unexpected match result: %+v", match)
	}
	// Apply the matched-existing update through the SAME per-goat transition,
	// using the captured row_version as no-clobber and a stable idempotency key.
	idSvc := identityapp.NewService(idRepo)
	body := []byte(fmt.Sprintf(`{"reproductive_status":%q,"reason":"import matched-existing update","row_version":%d,"evidence_refs":[{"evidence_type":"source_record","evidence_id":"import-row-1","source_system":"bulk_import"}]}`, reproTarget, match.RowVersion))
	resp, err := idSvc.ReproductiveGoat(ctx, identityapp.ReproductiveGoatInput{
		TenantID: tenant, ActorID: itActor, IdempotencyKey: "it-import-repro-0001",
		TraceID: "it-import", GoatID: match.GoatID, RawBody: body,
	})
	if err != nil {
		t.Fatalf("apply matched update: %v", err)
	}
	if len(resp.Events) == 0 || resp.Events[0].EventType != "goat.reproductive.changed" {
		t.Fatalf("matched update must emit goat.reproductive.changed: %+v", resp.Events)
	}
	if got := scalarInt(t, ctx, `SELECT count(*) FROM goats WHERE tenant_id=$1::uuid`, tenant); got != 1 {
		t.Fatalf("matched-existing update must not create a goat, tenant goat count=%d", got)
	}
	if got := scalarInt(t, ctx, `SELECT count(*) FROM goats WHERE goat_id=$1::uuid AND reproductive_status=$2 AND row_version=2`, goatID, reproTarget); got != 1 {
		t.Fatalf("matched goat not updated in place: %d", got)
	}
}

// ---- docker/migration harness helpers ----------------------------------------

func itWaitReady(container string) bool {
	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos").Run() == nil {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func itApplyMigrations(container string) error {
	root, err := itRepoRoot()
	if err != nil {
		return err
	}
	migrations, err := filepath.Glob(filepath.Join(root, "backend", "migrations", "postgres", "*.sql"))
	if err != nil {
		return err
	}
	sort.Strings(migrations)
	for _, migration := range migrations {
		sqlBytes, err := os.ReadFile(migration)
		if err != nil {
			return err
		}
		if err := itPsql(container, extractGooseUpSQL(string(sqlBytes))); err != nil {
			return fmt.Errorf("apply %s: %w", filepath.Base(migration), err)
		}
	}
	return nil
}

func itPsql(container, sqlText string) error {
	cmd := exec.Command("docker", "exec", "-i", container, "psql", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos")
	cmd.Stdin = strings.NewReader(sqlText)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

func itOpenPool(ctx context.Context, container string) (*pgxpool.Pool, error) {
	out, err := exec.Command("docker", "port", container, "5432/tcp").Output()
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	port := parts[len(parts)-1]
	cfg, err := pgxpool.ParseConfig("postgres://postgres:goatos@127.0.0.1:" + port + "/goatos?sslmode=disable")
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 12
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func extractGooseUpSQL(sqlText string) string {
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

// TestRunTenantRediscoversCrashOrphanAndReportsFailures covers the two paths the
// 1M scale gate (job-specific RunUntilDrained) does not: (P1) the tenant-wide
// worker rediscovers a job whose last rows were left 'claimed' by a crashed
// worker, and (P2) tenant-wide mode surfaces a failed job instead of hiding it
// behind a later completed one.
func TestRunTenantRediscoversCrashOrphanAndReportsFailures(t *testing.T) {
	requirePool(t)
	ctx := context.Background()
	tenant := newTenant(t, ctx)

	// Job 1 (goats): a completable job whose rows are stuck 'claimed' with a stale
	// lease — the signature of a worker that crashed after claiming the last rows.
	// Without the 'claimed' clause in ListJobIDsWithClaimableRows, RunTenant would
	// never rediscover it.
	copyGoats(t, ctx, tenant, "goat", reproStart, 5)
	var job1 string
	if err := itPool.QueryRow(ctx, `
INSERT INTO bulk_status_job (tenant_id, actor_id, axis, params, total_rows, state, idempotency_key)
VALUES ($1::uuid, $2::uuid, 'reproductive', '{}'::jsonb, 5, 'running', $3)
RETURNING bulk_status_job_id::text`, tenant, itActor, "it-"+t.Name()+"-orphan").Scan(&job1); err != nil {
		t.Fatalf("insert job1: %v", err)
	}
	if _, err := itPool.Exec(ctx, `
INSERT INTO bulk_status_job_row (tenant_id, job_id, goat_id, axis, target, expected_row_version, row_state, claimed_at)
SELECT tenant_id, $1::uuid, goat_id, 'reproductive', $2, row_version, 'claimed', now() - interval '2 minutes'
FROM goats WHERE tenant_id=$3::uuid AND species='goat'`, job1, reproTarget, tenant); err != nil {
		t.Fatalf("insert job1 rows: %v", err)
	}

	// Job 2 (sheep, separate goats so no row_version interaction): rows with an
	// invalid reproductive target -> apply is a permanent bad reference -> rows
	// settle 'error' -> job drains to 'failed'.
	copyGoats(t, ctx, tenant, "sheep", reproStart, 3)
	var job2 string
	if err := itPool.QueryRow(ctx, `
INSERT INTO bulk_status_job (tenant_id, actor_id, axis, params, total_rows, state, idempotency_key)
VALUES ($1::uuid, $2::uuid, 'reproductive', '{}'::jsonb, 3, 'pending', $3)
RETURNING bulk_status_job_id::text`, tenant, itActor, "it-"+t.Name()+"-fail").Scan(&job2); err != nil {
		t.Fatalf("insert job2: %v", err)
	}
	if _, err := itPool.Exec(ctx, `
INSERT INTO bulk_status_job_row (tenant_id, job_id, goat_id, axis, target, expected_row_version, row_state)
SELECT tenant_id, $1::uuid, goat_id, 'reproductive', 'not_a_reproductive_status', row_version, 'pending'
FROM goats WHERE tenant_id=$2::uuid AND species='sheep'`, job2, tenant); err != nil {
		t.Fatalf("insert job2 rows: %v", err)
	}

	drain, err := buildWorker(drainSettings()).RunTenant(ctx, tenant, 100)
	if err != nil {
		t.Fatalf("RunTenant: %v", err)
	}

	// Both jobs must have been discovered + drained (the orphan proves P1).
	if len(drain.Jobs) != 2 {
		t.Fatalf("RunTenant should report 2 jobs, got %d: %+v", len(drain.Jobs), drain.Jobs)
	}
	byID := make(map[string]bulkapp.JobCounts, len(drain.Jobs))
	for _, j := range drain.Jobs {
		byID[j.JobID] = j.FinalCounts
	}
	if c := byID[job1]; c.State != bulkapp.JobStateCompleted || c.Applied != 5 || c.Remaining != 0 {
		t.Fatalf("orphan job1 not completed cleanly: %+v", c)
	}
	if c := byID[job2]; c.State != bulkapp.JobStateFailed || c.Failed != 3 {
		t.Fatalf("job2 not reported failed with 3 error rows: %+v", c)
	}
	// The failed job must surface (P2: tenant mode must not hide it).
	failed := drain.FailedJobs()
	if len(failed) != 1 || failed[0].JobID != job2 {
		t.Fatalf("expected exactly job2 (%s) reported failed, got %+v", job2, failed)
	}
}

func itRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "backend", "migrations", "postgres")); err == nil {
			return wd, nil
		}
		next := filepath.Dir(wd)
		if next == wd {
			return "", fmt.Errorf("repo root not found from %s", wd)
		}
		wd = next
	}
}
