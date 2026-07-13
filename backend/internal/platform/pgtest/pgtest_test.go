package pgtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// scratch is a table that only exists once migrations have run; using a neutral temp-ish table on
// the migrated schema would couple these tests to product tables, so each test creates its own.
const createScratch = `CREATE TABLE IF NOT EXISTS pgtest_scratch (id int primary key, note text)`

func TestStartPostgresGivesMigratedIsolatedDatabase(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()

	a := pgtest.StartPostgres(t, ctx)
	b := pgtest.StartPostgres(t, ctx)

	for _, p := range []*pgxpool.Pool{a, b} {
		if _, err := p.Exec(ctx, createScratch); err != nil {
			t.Fatalf("create scratch: %v", err)
		}
	}

	// Same primary key inserted into both databases must not collide: they are separate databases.
	if _, err := a.Exec(ctx, `INSERT INTO pgtest_scratch (id, note) VALUES (1, 'a')`); err != nil {
		t.Fatalf("insert a: %v", err)
	}
	if _, err := b.Exec(ctx, `INSERT INTO pgtest_scratch (id, note) VALUES (1, 'b')`); err != nil {
		t.Fatalf("insert b (should be isolated from a): %v", err)
	}

	// A committed row in a is invisible in b.
	var count int
	if err := b.QueryRow(ctx, `SELECT count(*) FROM pgtest_scratch WHERE note = 'a'`).Scan(&count); err != nil {
		t.Fatalf("select from b: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected b to be isolated from a, saw %d rows written by a", count)
	}

	// The migrated schema is present (a real product table from migration 000001 exists in the clone).
	var reg string
	if err := a.QueryRow(ctx, `SELECT to_regclass('public.tenants')::text`).Scan(&reg); err != nil {
		t.Fatalf("regclass check: %v", err)
	}
	if reg == "" {
		t.Fatalf("expected migrated schema in clone; tenants table missing")
	}
}

func TestStartPostgresCommittedWriteVisibleAcrossConnectionsInSameTest(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	if _, err := pool.Exec(ctx, createScratch); err != nil {
		t.Fatalf("create scratch: %v", err)
	}

	// Two independent connections from the same pool: a committed write on one is visible on the
	// other, preserving the multi-connection/committed-state semantics real suites rely on.
	c1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire c1: %v", err)
	}
	defer c1.Release()
	c2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire c2: %v", err)
	}
	defer c2.Release()

	if _, err := c1.Exec(ctx, `INSERT INTO pgtest_scratch (id, note) VALUES (7, 'committed')`); err != nil {
		t.Fatalf("commit write on c1: %v", err)
	}
	var note string
	if err := c2.QueryRow(ctx, `SELECT note FROM pgtest_scratch WHERE id = 7`).Scan(&note); err != nil {
		t.Fatalf("read committed write on c2: %v", err)
	}
	if note != "committed" {
		t.Fatalf("expected c2 to see committed write, got %q", note)
	}
}

func TestTemplateRejectsDirectConnections(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()

	// Force the package harness to initialize (builds and locks the template).
	pool := pgtest.StartPostgres(t, ctx)
	dsn := pgtest.AdminDSNForTemplate(t)
	if dsn == "" {
		t.Fatal("expected template dsn once harness started")
	}
	_ = pool

	if _, err := pgx.Connect(ctx, dsn); err == nil {
		t.Fatal("expected connection to the locked template database to be rejected")
	}
}
