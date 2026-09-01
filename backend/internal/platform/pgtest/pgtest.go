// Package pgtest provides a docker-backed Postgres harness for integration tests.
//
// Lifecycle model (see context/execution/postgres-test-harness-optimization-handoff-20260713.md):
//   - ONE migrated Postgres container per Go test package/process, started lazily on the
//     first StartPostgres call and torn down once when the package's TestMain returns.
//   - All committed migrations are applied exactly once to a locked template database.
//   - Each test gets its OWN database cloned from that template plus its own pgxpool.Pool;
//     the clone and pool are dropped/closed on that test's cleanup.
//
// This replaces the previous "one fresh container + full migration replay per test" model,
// which paid container startup, readiness polling, migration replay, and teardown for every
// StartPostgres call (28 serial times in the calendar integration file alone).
//
// Every package that calls StartPostgres MUST run the package singleton teardown from its
// TestMain: either call pgtest.RunMain(m) directly, or call pgtest.Shutdown() before its own
// os.Exit. Without that, the package's container is left running after the test binary exits.
//
// It imports "testing" and is intended for use only from _test.go files (so production
// binaries never link it).
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultPostgresImage = "postgres:16.9-alpine"

// ExternalAdminDSN is the sanctioned NO-DOCKER path for a deliberate database gate.
//
// It mirrors GOATOS_SQLC_PLAN_ADMIN_DSN, which validate-sqlc-query-plans.sh already uses for the
// same reason: a plan gate needs POSTGRES, not laptop Docker specifically, and on machines where
// Docker/Colima is disallowed the alternative is not "run it another way" but "the gate never
// runs" -- which is how a guard silently stops guarding.
//
// When set, the harness creates its migrated template and per-test clones on that server instead
// of starting a container, and SkipIfNoDocker no longer skips. It must point at a THROWAWAY server
// or one the operator is content to have databases created and dropped on: every clone is named
// goatos_test_* and dropped on test cleanup, and the template is dropped at package teardown.
func ExternalAdminDSN() string {
	return strings.TrimSpace(os.Getenv("GOATOS_PGTEST_ADMIN_DSN"))
}

// Enabled reports whether this process was explicitly authorized to run Postgres tests.
// Postgres tests are intentionally opt-in because container startup dominates normal local and
// pull-request CI time. Set GOATOS_RUN_POSTGRES_TESTS=1 only for a deliberate database run.
func Enabled() bool {
	v := strings.TrimSpace(os.Getenv("GOATOS_RUN_POSTGRES_TESTS"))
	return v == "1" || strings.EqualFold(v, "true")
}

func skipUnlessEnabled(t *testing.T) {
	t.Helper()
	if !Enabled() {
		t.Skip("Postgres tests are opt-in; set GOATOS_RUN_POSTGRES_TESTS=1 to run")
	}
}

// SkipIfNoDocker skips unless Postgres tests were explicitly enabled, then verifies Docker is
// available. GOATOS_REQUIRE_DOCKER=1 keeps deliberate database gates fail-closed when Docker is
// missing; it does not itself opt a default test run into Postgres.
func SkipIfNoDocker(t *testing.T) {
	t.Helper()
	skipUnlessEnabled(t)
	if ExternalAdminDSN() != "" {
		// A database was supplied; Docker is irrelevant to whether this gate can run.
		return
	}
	if _, err := exec.LookPath("docker"); err != nil {
		if requireDocker() {
			t.Fatalf("GOATOS_REQUIRE_DOCKER is set but docker is unavailable: the required Postgres integration gate must run, not skip")
		}
		t.Skip("docker not available")
	}
}

// requireDocker reports whether the CI required-gate flag is set. When true, SkipIfNoDocker fails
// rather than skips, so the Postgres gate cannot silently pass without actually running.
func requireDocker() bool {
	v := strings.TrimSpace(os.Getenv("GOATOS_REQUIRE_DOCKER"))
	return v == "1" || strings.EqualFold(v, "true")
}

// processTag returns a collision-resistant identity for this test binary/process, combining the
// process ID with a cryptographically random suffix. Container, template, and clone database
// names embed it so concurrently running package test binaries (go test ./...) never collide.
var processTag = func() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		// rand.Read failing is effectively impossible; fall back to the nanosecond clock.
		return fmt.Sprintf("%d_%d", os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("%d_%s", os.Getpid(), hex.EncodeToString(buf))
}()

// pkgHarness is the package-scoped singleton: one container + one migrated template database,
// shared by every StartPostgres call in the package process.
type pkgHarness struct {
	once      sync.Once
	initErr   error
	container string
	port      string
	template  string
	admin     *pgxpool.Pool // connected to the neutral "postgres" maintenance database

	cloneMu  sync.Mutex // serializes CREATE DATABASE ... TEMPLATE; also the future-parallel guard
	cloneSeq atomic.Uint64
	started  atomic.Bool

	// external is the no-Docker model: template and clones are created on a Postgres server the
	// operator supplied through GOATOS_PGTEST_ADMIN_DSN instead of on a container this harness
	// starts. baseDSN is that connection string; dsnFor swaps its database name per clone.
	external bool
	baseDSN  string
}

var pkg = &pkgHarness{}

// StartPostgres returns a pgxpool.Pool connected to a fresh, migration-complete database that is
// private to the calling test. The database is cloned from the package template; it and the pool
// are dropped/closed on t.Cleanup. The shared package container is started lazily on first use.
func StartPostgres(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	// Enforce the opt-in at the resource boundary too, so a test that forgets to call
	// SkipIfNoDocker can never start a database during the default suite.
	skipUnlessEnabled(t)
	// Escape hatch: GOATOS_PGTEST_DEDICATED=1 forces every StartPostgres onto its own throwaway
	// migrated container (the pre-optimization model). Used to bisect clone-model vs pre-existing
	// failures and to unblock any test that proves it is unsafe under template cloning.
	if os.Getenv("GOATOS_PGTEST_DEDICATED") == "1" {
		// The dedicated path is Docker-only by construction, so the two escape hatches are mutually
		// exclusive. Saying so here turns a confusing "docker run" failure deep in the harness into
		// a statement about the caller's environment.
		if ExternalAdminDSN() != "" {
			t.Fatalf("pgtest: GOATOS_PGTEST_DEDICATED=1 needs Docker and cannot be combined with GOATOS_PGTEST_ADMIN_DSN; unset one")
		}
		return StartDedicatedPostgres(t, ctx)
	}
	pkg.ensure(t, ctx)

	clone := fmt.Sprintf("goatos_test_%s_%d", processTag, pkg.cloneSeq.Add(1))
	pkg.cloneMu.Lock()
	_, err := pkg.admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s TEMPLATE %s`, quoteIdent(clone), quoteIdent(pkg.template)))
	pkg.cloneMu.Unlock()
	if err != nil {
		t.Fatalf("pgtest: clone database from template: %v", err)
	}

	pool := openPool(t, ctx, pkg.dsnFor(clone), clone)
	t.Cleanup(func() { dropTestDatabase(t, pool, pkg.admin, clone) })
	return pool
}

// ensure lazily brings up the package container and migrated template exactly once. Any
// initialization failure is reported to every caller (via t.Fatal) rather than deadlocking.
func (h *pkgHarness) ensure(t *testing.T, ctx context.Context) {
	t.Helper()
	h.once.Do(func() { h.initErr = h.start(ctx) })
	if h.initErr != nil {
		t.Fatalf("pgtest: package harness init failed: %v", h.initErr)
	}
}

func (h *pkgHarness) start(ctx context.Context) error {
	h.container = "goatos-pgtest-" + processTag
	h.template = "goatos_tmpl_" + processTag

	if base := ExternalAdminDSN(); base != "" {
		h.external = true
		h.baseDSN = base
		admin, err := pgxpool.New(ctx, h.dsnFor("postgres"))
		if err != nil {
			return fmt.Errorf("open admin pool on GOATOS_PGTEST_ADMIN_DSN: %w", err)
		}
		if err := admin.Ping(ctx); err != nil {
			admin.Close()
			return fmt.Errorf("ping admin pool on GOATOS_PGTEST_ADMIN_DSN: %w", err)
		}
		h.admin = admin
		return h.buildTemplate(ctx)
	}

	image := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if image == "" {
		image = defaultPostgresImage
	}
	// -v on removal reaps the anonymous data volume (postgres declares VOLUME /var/lib/postgresql/data);
	// without it every container leaked one orphan volume that eventually filled the Docker VM disk.
	if out, err := exec.Command("docker", "run", "--name", h.container,
		"-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos",
		"-p", "127.0.0.1::5432", "-d", image).CombinedOutput(); err != nil {
		return fmt.Errorf("docker run: %v\n%s", err, out)
	}
	h.started.Store(true)

	ready := false
	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", h.container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "postgres").Run() == nil {
			ready = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		out, _ := exec.Command("docker", "logs", h.container).CombinedOutput()
		return fmt.Errorf("postgres container did not become ready:\n%s", out)
	}

	port, err := containerPort(h.container)
	if err != nil {
		return err
	}
	h.port = port

	admin, err := pgxpool.New(ctx, h.dsnFor("postgres"))
	if err != nil {
		return fmt.Errorf("open admin pool: %w", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		return fmt.Errorf("ping admin pool: %w", err)
	}
	h.admin = admin

	if err := h.buildTemplate(ctx); err != nil {
		return err
	}
	return nil
}

// buildTemplate creates the dedicated template database, applies every migration Up section to it
// through a single psql process (the migration's only connection phase), then locks the template
// against further connections so it can always be used as a CREATE DATABASE source.
func (h *pkgHarness) buildTemplate(ctx context.Context) error {
	if _, err := h.admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, quoteIdent(h.template))); err != nil {
		return fmt.Errorf("create template database: %w", err)
	}
	if err := h.applyMigrations(h.template); err != nil {
		return err
	}
	// psql has exited, so the template has no live sessions. Forbid future connections so a stray
	// pool can never hold the template open and block clone copying (a hard invariant of the design).
	if _, err := h.admin.Exec(ctx, fmt.Sprintf(`ALTER DATABASE %s WITH ALLOW_CONNECTIONS false`, quoteIdent(h.template))); err != nil {
		return fmt.Errorf("lock template database: %w", err)
	}
	return nil
}

// teardown closes the admin pool and force-removes the package container and its volume. Safe to
// call multiple times and safe when the harness never started.
func (h *pkgHarness) teardown() {
	// In the external model nothing is thrown away with a container, so the template must be
	// dropped explicitly or it accumulates one abandoned database per test process on a server the
	// operator did not volunteer for that.
	if h.external && h.admin != nil && h.template != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, _ = h.admin.Exec(ctx, fmt.Sprintf(`ALTER DATABASE %s WITH ALLOW_CONNECTIONS true`, quoteIdent(h.template)))
		_, _ = h.admin.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, quoteIdent(h.template)))
		cancel()
	}
	if h.admin != nil {
		h.admin.Close()
		h.admin = nil
	}
	if h.started.Load() && h.container != "" {
		_ = exec.Command("docker", "rm", "-f", "-v", h.container).Run()
		h.started.Store(false)
	}
}

// applyMigrations extracts each migration's goose Up section, concatenates them in filename order,
// and pipes the whole script through one psql process targeting the given database.
//
// The psql process runs INSIDE the container in the Docker model and on the HOST against the
// supplied DSN in the external model; the script is byte-identical either way.
func (h *pkgHarness) applyMigrations(db string) error {
	root, err := repoRootFromWD()
	if err != nil {
		return err
	}
	migrations, err := filepath.Glob(filepath.Join(root, "backend", "migrations", "postgres", "*.sql"))
	if err != nil {
		return err
	}
	sort.Strings(migrations)
	var script strings.Builder
	for _, migration := range migrations {
		sqlBytes, err := os.ReadFile(migration)
		if err != nil {
			return err
		}
		upSQL := strings.TrimSpace(extractGooseUp(string(sqlBytes)))
		if upSQL == "" {
			continue
		}
		fmt.Fprintf(&script, "\\echo applying %s\n", filepath.Base(migration))
		script.WriteString(upSQL)
		script.WriteByte('\n')
	}
	var cmd *exec.Cmd
	if h.external {
		if _, err := exec.LookPath("psql"); err != nil {
			return fmt.Errorf("GOATOS_PGTEST_ADMIN_DSN is set but psql is not on PATH: %w", err)
		}
		cmd = exec.Command("psql", "-v", "ON_ERROR_STOP=1", h.dsnFor(db))
	} else {
		cmd = exec.Command("docker", "exec", "-i", h.container, "psql", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-U", "postgres", "-d", db)
	}
	cmd.Stdin = strings.NewReader(script.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("psql migrate %s failed: %v\n%s", db, err, out)
	}
	return nil
}

// dropTestDatabase drops a test clone and closes its pool as a test-cleanup step, surfacing any
// failure via t.Error without masking the original test failure.
func dropTestDatabase(t *testing.T, pool, admin *pgxpool.Pool, db string) {
	t.Helper()
	if err := dropClonedDatabase(admin, pool, db); err != nil {
		t.Errorf("pgtest: %v", err)
	}
}

// dropClonedDatabase tears down a test clone with bounded waiting and honest reporting. See
// dropClonedDatabaseAsync for the mechanism; the returned close-completion channel is only of
// interest to the harness's own regression test, so it is discarded here.
func dropClonedDatabase(admin, pool *pgxpool.Pool, db string) error {
	err, _ := dropClonedDatabaseAsync(admin, pool, db)
	return err
}

// dropClonedDatabaseAsync drops a test clone with bounded waiting so a connection a test leaked
// (acquired and never released) cannot hang cleanup forever, and reports the leak instead of
// masking it.
//
// Order is load-bearing. In pinned pgx v5, Pool.Close blocks until every acquired connection is
// returned, so it MUST NOT run first: a leaked acquisition would block before termination/DROP ever
// executed. Instead:
//
//  1. terminate the clone's server-side backends — this frees the database for DROP even when a
//     client still holds a leaked *pgxpool.Conn, because the backend behind it is gone;
//  2. close the pool under a bounded wait — instant in the normal (no-leak) path;
//  3. DROP with a short retry to absorb the brief post-termination window.
//
// If the bounded close did NOT complete (a connection was leaked), the DROP still succeeds but this
// returns a non-nil error so the caller (the t.Cleanup wrapper) reports the leak rather than
// claiming clean teardown. The returned channel closes when Pool.Close eventually returns — i.e.
// once the leaked connection is released — which the regression test uses to prove the abandoned
// close goroutine actually terminates.
func dropClonedDatabaseAsync(admin, pool *pgxpool.Pool, db string) (error, <-chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	closed := make(chan struct{})

	if _, err := admin.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`,
		db); err != nil {
		return fmt.Errorf("terminate sessions for %s: %w", db, err), closed
	}

	go func() { pool.Close(); close(closed) }()
	closeCompleted := false
	select {
	case <-closed:
		closeCompleted = true
	case <-time.After(5 * time.Second):
		// A leaked acquisition is blocking Pool.Close. Server backends are already terminated, so
		// proceed to DROP; the goroutine ends if/when the connection is finally released.
	}

	var dropErr error
	dropped := false
	for i := 0; i < 10; i++ {
		// scale-guard:ignore: bounded 10-attempt test-DB drop retry in the test harness (transient-lock backoff on teardown), not a request/worker path
		if _, dropErr = admin.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, quoteIdent(db))); dropErr == nil {
			dropped = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !dropped {
		return fmt.Errorf("drop database %s: %w", db, dropErr), closed
	}
	if !closeCompleted {
		return fmt.Errorf("dropped database %s, but pool.Close did not complete within 5s: a connection was acquired and never released", db), closed
	}
	return nil, closed
}

func openPool(t *testing.T, ctx context.Context, connString, db string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("pgtest: open pool for %s: %v", db, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("pgtest: ping pool for %s: %v", db, err)
	}
	return pool
}

func containerPort(container string) (string, error) {
	out, err := exec.Command("docker", "port", container, "5432/tcp").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker port: %v\n%s", err, out)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	return parts[len(parts)-1], nil
}

func dsn(port, db string) string {
	return "postgres://postgres:goatos@127.0.0.1:" + port + "/" + db + "?sslmode=disable"
}

// dsnFor returns a connection string for one database on whichever server this harness is using.
//
// In the external model the supplied DSN's PATH is swapped for the target database and every other
// component -- host, port, credentials, sslmode and any other query parameters -- is preserved, so
// a tunnelled or TLS-bearing DSN keeps working.
func (h *pkgHarness) dsnFor(db string) string {
	if !h.external {
		return dsn(h.port, db)
	}
	// Detected by PREFIX, not by parse failure. url.Parse accepts a libpq keyword DSN
	// ("host=h port=5432 dbname=x") without error, treating the whole thing as an opaque path, so
	// the err branch never fired and the keyword form silently produced the literal string
	// "/postgres".
	if !strings.HasPrefix(h.baseDSN, "postgres://") && !strings.HasPrefix(h.baseDSN, "postgresql://") {
		// libpq keyword form: a later dbname overrides an earlier one.
		return h.baseDSN + " dbname=" + db
	}
	parsed, err := url.Parse(h.baseDSN)
	if err != nil {
		return h.baseDSN + " dbname=" + db
	}
	parsed.Path = "/" + db
	return parsed.String()
}

// quoteIdent double-quotes a SQL identifier. Database names here are generated internally from
// safe characters ([a-z0-9_]); this defensively guards CREATE/DROP/ALTER DATABASE statements,
// which cannot use bind parameters for identifiers.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func extractGooseUp(sqlText string) string {
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

func repoRootFromWD() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir), nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", fmt.Errorf("repo root not found from %s", dir)
		}
		dir = next
	}
}
