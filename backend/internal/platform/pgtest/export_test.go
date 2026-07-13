package pgtest

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AdminDSNForTemplate returns a DSN pointing at the (locked) template database, for the harness's
// own tests to prove that direct connections are rejected. Returns "" if the harness has not
// started. Test-only.
func AdminDSNForTemplate(t *testing.T) string {
	t.Helper()
	if !pkg.started.Load() || pkg.port == "" || pkg.template == "" {
		return ""
	}
	return dsn(pkg.port, pkg.template)
}

// CloneForTest creates a fresh clone database and returns its pool plus name WITHOUT registering
// automatic cleanup, so a test can drive dropClonedDatabase directly (e.g. the leaked-connection
// bounded-cleanup regression). The caller owns dropping it. Test-only.
func CloneForTest(t *testing.T, ctx context.Context) (*pgxpool.Pool, string) {
	t.Helper()
	pkg.ensure(t, ctx)
	clone := fmt.Sprintf("goatos_test_%s_leak_%d", processTag, pkg.cloneSeq.Add(1))
	if _, err := pkg.admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s TEMPLATE %s`, quoteIdent(clone), quoteIdent(pkg.template))); err != nil {
		t.Fatalf("CloneForTest: %v", err)
	}
	return openPool(t, ctx, pkg.port, clone), clone
}

// DropClonedDatabaseForTest exposes the bounded teardown plus its close-completion channel for the
// leaked-connection regression test. The channel closes when Pool.Close finally returns.
func DropClonedDatabaseForTest(pool *pgxpool.Pool, db string) (error, <-chan struct{}) {
	return dropClonedDatabaseAsync(pkg.admin, pool, db)
}

// DatabaseExistsForTest reports whether db still exists, for the regression test to prove the clone
// was actually dropped (bounded teardown, not a hang).
func DatabaseExistsForTest(t *testing.T, ctx context.Context, db string) bool {
	t.Helper()
	var exists bool
	if err := pkg.admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, db).Scan(&exists); err != nil {
		t.Fatalf("DatabaseExistsForTest: %v", err)
	}
	return exists
}

// RepoRootForTest exposes the repo-root walk for the lifecycle guard test.
func RepoRootForTest(t *testing.T) string {
	t.Helper()
	root, err := repoRootFromWD()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return root
}
