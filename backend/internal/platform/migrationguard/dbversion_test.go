package migrationguard

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestAppliedVersionNeverMigratedDatabase covers the real-world "never
// migrated" case: pgtest provisions its template database by piping every
// migration's `-- +goose Up` SQL through psql directly (see pgtest.go's
// applyMigrations), the same fast path cmd/migrate itself never takes in
// production/dev - it always records one goatos_schema_migrations row per
// applied migration. So a fresh pgtest database, despite having every table
// pgtest's migrations create, has no goatos_schema_migrations bookkeeping
// table at all, exactly like a real database nobody has ever pointed
// cmd/migrate at yet. AppliedVersion must treat that as "" (never migrated),
// not as a query failure.
func TestAppliedVersionNeverMigratedDatabase(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	got, err := AppliedVersion(ctx, pool)
	if err != nil {
		t.Fatalf("AppliedVersion() on a never-migrated database error = %v, want nil error with \"\"", err)
	}
	if got != "" {
		t.Fatalf("AppliedVersion() = %q, want \"\" for a never-migrated database", got)
	}

	binaryVersion, err := BinaryVersion()
	if err != nil {
		t.Fatalf("BinaryVersion() error = %v", err)
	}
	status, err := Check(got, binaryVersion)
	if err == nil {
		t.Fatal("Check(\"\", binaryVersion) = nil error, want error (binary ahead of a never-migrated database)")
	}
	if !status.BinaryAhead {
		t.Fatalf("Check(\"\", %q) Status.BinaryAhead = false, want true", binaryVersion)
	}
}

// TestAppliedVersion exercises the real query path (SELECT ... ORDER BY
// version DESC LIMIT 1) against a real Postgres instance, reproducing the
// goatos_schema_migrations table shape AND the actual stored value shape
// cmd/migrate uses (backend/cmd/migrate/main.go's
// `Version: strings.TrimSuffix(entry.Name(), ".sql")`) since pgtest's own
// template build does not populate this table at all (see
// TestAppliedVersionNeverMigratedDatabase above). A prior draft of this test
// inserted bare numeric strings ("000188") as the version column value; a
// live smoke test against a real migrated database (go run ./cmd/migrate,
// then go run ./cmd/api) caught that this does not match reality -
// goatos_schema_migrations.version is the full filename stem, e.g.
// "000188_drop_process_integrity_projection_summaries" - which is why
// AppliedVersion normalizes it.
func TestAppliedVersion(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	if _, err := pool.Exec(ctx, `
CREATE TABLE goatos_schema_migrations (
  version text PRIMARY KEY,
  filename text NOT NULL,
  checksum text NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		t.Fatalf("create goatos_schema_migrations: %v", err)
	}
	// Insert full filename stems, out of order, to prove AppliedVersion both
	// sorts (rather than trusting insertion order) and normalizes to the bare
	// numeric shape.
	for _, stem := range []string{
		"000150_some_migration",
		"000188_drop_process_integrity_projection_summaries",
		"000005_initial_schema",
		"000099_another_migration",
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO goatos_schema_migrations (version, filename, checksum) VALUES ($1, $2, 'sha256:test')`,
			stem, stem+".sql",
		); err != nil {
			t.Fatalf("insert migration row %s: %v", stem, err)
		}
	}

	got, err := AppliedVersion(ctx, pool)
	if err != nil {
		t.Fatalf("AppliedVersion() error = %v", err)
	}
	if got != "000188" {
		t.Fatalf("AppliedVersion() = %q, want %q (normalized from the highest inserted stem)", got, "000188")
	}

	if _, err := Check(got, "000188"); err != nil {
		t.Fatalf("Check(%q, \"000188\") = %v, want nil (equal versions)", got, err)
	}
}

// TestAppliedVersionMalformedVersionRow guards the normalization added after
// TestAppliedVersion's fix above: a goatos_schema_migrations.version value
// that does not start with digits-then-underscore is a hard error, not a
// silently wrong comparison.
func TestAppliedVersionMalformedVersionRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	if _, err := pool.Exec(ctx, `
CREATE TABLE goatos_schema_migrations (
  version text PRIMARY KEY,
  filename text NOT NULL,
  checksum text NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		t.Fatalf("create goatos_schema_migrations: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO goatos_schema_migrations (version, filename, checksum) VALUES ('not-a-version', 'not-a-version.sql', 'sha256:test')`,
	); err != nil {
		t.Fatalf("insert malformed migration row: %v", err)
	}

	if _, err := AppliedVersion(ctx, pool); err == nil {
		t.Fatal("AppliedVersion() with a malformed version row = nil error, want error")
	}
}

// TestAppliedVersionNumericOrderingWidthCrossing verifies that AppliedVersion
// finds the highest version using numeric comparison, not lexicographic order.
// This test covers the hypothetical case where migration numbering crosses a
// digit-width boundary (e.g. 999999 → 1000000). If the SQL used lexicographic
// ordering (ORDER BY version DESC), it would incorrectly return "999999" as
// higher than "1000000". Numeric ordering (ORDER BY
// (split_part(version,'_',1))::int64 DESC) returns the correct max.
func TestAppliedVersionNumericOrderingWidthCrossing(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	if _, err := pool.Exec(ctx, `
CREATE TABLE goatos_schema_migrations (
  version text PRIMARY KEY,
  filename text NOT NULL,
  checksum text NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		t.Fatalf("create goatos_schema_migrations: %v", err)
	}
	// Test width-crossing cases: 000099 vs 000100, and hypothetical 999999 vs 1000000.
	// Lexicographic order would prefer "999999" (compare '9' > '1'); numeric order
	// correctly returns 1000000 and 000100 respectively.
	for _, stem := range []string{
		"000099_before_width_cross",
		"000100_after_width_cross",
		"999999_before_million_cross",
		"1000000_after_million_cross",
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO goatos_schema_migrations (version, filename, checksum) VALUES ($1, $2, 'sha256:test')`,
			stem, stem+".sql",
		); err != nil {
			t.Fatalf("insert migration row %s: %v", stem, err)
		}
	}

	got, err := AppliedVersion(ctx, pool)
	if err != nil {
		t.Fatalf("AppliedVersion() error = %v", err)
	}
	// Should return 1000000, not 999999 (which would win lexicographically).
	if got != "1000000" {
		t.Fatalf("AppliedVersion() = %q, want %q (numeric max, not lexicographic)", got, "1000000")
	}
}
