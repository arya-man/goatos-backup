package pgtest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunMain is the standard TestMain body for a package that uses StartPostgres. It runs the package
// tests, tears down the shared package container/template once, and exits with the test status:
//
//	func TestMain(m *testing.M) { pgtest.RunMain(m) }
//
// A package that needs its own TestMain (for example to write an E2E report) must instead call
// Shutdown() before its os.Exit.
func RunMain(m *testing.M) {
	code := m.Run()
	Shutdown()
	os.Exit(code)
}

// Shutdown tears down the package-scoped container and admin pool started by StartPostgres. It is
// idempotent and a no-op when the harness never started. Packages with a custom TestMain must call
// it before exiting.
func Shutdown() {
	pkg.teardown()
}

// StartDedicatedPostgres is the escape hatch: it launches a throwaway container of its own, applies
// all migrations to its POSTGRES_DB, and returns a connected pool. The container is removed on
// t.Cleanup. Use it ONLY for tests that validate database-level state the template-clone model
// cannot isolate: migration application/failure, startup behavior, database settings, extensions,
// roles/grants, or container restart/crash recovery. Ordinary event/outbox/projection/trigger/
// SKIP-LOCKED concurrency tests must use StartPostgres and separate connections inside one clone.
func StartDedicatedPostgres(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	skipUnlessEnabled(t)
	container := fmt.Sprintf("goatos-pgtest-ded-%s-%d", processTag, time.Now().UnixNano())
	image := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if image == "" {
		image = defaultPostgresImage
	}
	if out, err := exec.Command("docker", "run", "--name", container,
		"-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos",
		"-p", "127.0.0.1::5432", "-d", image).CombinedOutput(); err != nil {
		t.Fatalf("pgtest: dedicated docker run: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", "-v", container).Run() })

	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos").Run() == nil {
			if err := applyMigrations(container, "goatos"); err != nil {
				t.Fatalf("pgtest: dedicated migrate: %v", err)
			}
			port, err := containerPort(container)
			if err != nil {
				t.Fatalf("pgtest: dedicated port: %v", err)
			}
			return openPool(t, ctx, port, "goatos")
		}
		time.Sleep(500 * time.Millisecond)
	}
	out, _ := exec.Command("docker", "logs", container).CombinedOutput()
	t.Fatalf("pgtest: dedicated container did not become ready:\n%s", out)
	return nil
}
