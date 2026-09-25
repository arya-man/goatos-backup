// Command pgtest-template ensures the reusable migrated pgtest template exists on the server named by
// the admin DSN argument (or GOATOS_PGTEST_ADMIN_DSN), reaps stale pgtest databases, and prints the
// template database name on stdout. Used by backend/tests/integration/validate-sqlc-query-plans.sh.
//
//	go run ./internal/platform/pgtemplate/cmd/pgtest-template [admin-dsn]
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtemplate"
)

func main() {
	base := strings.TrimSpace(os.Getenv("GOATOS_PGTEST_ADMIN_DSN"))
	if len(os.Args) > 1 {
		base = os.Args[1]
	}
	if base == "" {
		fail(fmt.Errorf("usage: pgtest-template <admin-dsn> (or set GOATOS_PGTEST_ADMIN_DSN)"))
	}
	root, err := repoRoot()
	if err != nil {
		fail(err)
	}
	migrations, err := pgtemplate.LoadMigrations(root)
	if err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, pgtemplate.DSNFor(base, "postgres"))
	if err != nil {
		fail(err)
	}
	defer admin.Close()
	name, err := pgtemplate.Ensure(ctx, admin, base, migrations, os.Stderr)
	if err != nil {
		fail(err)
	}
	pgtemplate.Cleanup(ctx, admin, pgtemplate.Hash(migrations), os.Stderr)
	fmt.Println(name)
}

func repoRoot() (string, error) {
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
			return "", fmt.Errorf("repo root not found")
		}
		dir = next
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "pgtest-template:", err)
	os.Exit(1)
}
