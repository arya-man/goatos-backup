package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestExtractGooseUp(t *testing.T) {
	got, err := extractGooseUp(`-- +goose Up
CREATE TABLE t (id int);
-- +goose Down
DROP TABLE t;`)
	if err != nil {
		t.Fatalf("extractGooseUp: %v", err)
	}
	if strings.Contains(got, "DROP TABLE") || !strings.Contains(got, "CREATE TABLE") {
		t.Fatalf("unexpected up SQL: %s", got)
	}
}

func TestLoadMigrationsMarksNoTransaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "000001_no_tx.sql")
	if err := os.WriteFile(path, []byte(`-- +goose Up
-- +goose NO TRANSACTION
CREATE INDEX CONCURRENTLY IF NOT EXISTS t_idx ON t (id);
-- +goose Down
DROP INDEX IF EXISTS t_idx;`), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}
	migrations, err := loadMigrations(dir)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("migrations = %d, want 1", len(migrations))
	}
	if !migrations[0].NoTx {
		t.Fatalf("NoTx = false, want true for goose NO TRANSACTION marker")
	}
}

func TestAllowedHistoricalChecksumOnlyAcceptsKnownBaselineDrift(t *testing.T) {
	migration := migrationFile{
		Version:  "000001_goatos_clean_slate_baseline",
		Filename: "000001_goatos_clean_slate_baseline.sql",
		Checksum: "sha256:2ecaf35d57ff448fcd2f293e502074c1fe5c36e509a807fa482ab609659d6bb0",
	}
	if !isAllowedHistoricalChecksum(migration, "sha256:b29305e89e75b2ef15bb80a79c704720941d1d3b8cc8e10de085655b6290d349") {
		t.Fatal("known historical baseline checksum was rejected")
	}
	if isAllowedHistoricalChecksum(migration, "sha256:unexpected") {
		t.Fatal("unexpected baseline checksum was accepted")
	}
	migration.Version = "000002_restore_operator_assignment_selected_ids"
	if isAllowedHistoricalChecksum(migration, "sha256:b29305e89e75b2ef15bb80a79c704720941d1d3b8cc8e10de085655b6290d349") {
		t.Fatal("historical checksum allowance applied to a non-baseline migration")
	}
}

func TestApplyMigrationsRollsBackOrdinaryMigrationOnFailure(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	err := applyMigrations(ctx, pool, []migrationFile{{
		Version:  "999999_tx_probe",
		Filename: "999999_tx_probe.sql",
		Checksum: "sha256:test",
		SQL: `CREATE TABLE tx_probe (id int);
INSERT INTO tx_probe VALUES (1);
INSERT INTO missing_tx_probe VALUES (1);`,
	}}, false, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("applyMigrations succeeded, want failure")
	}
	var tableExists bool
	if scanErr := pool.QueryRow(ctx, `SELECT to_regclass('public.tx_probe') IS NOT NULL`).Scan(&tableExists); scanErr != nil {
		t.Fatalf("check tx_probe: %v", scanErr)
	}
	if tableExists {
		t.Fatal("ordinary migration left tx_probe behind after failure; want transaction rollback")
	}
	var recorded int
	if scanErr := pool.QueryRow(ctx, `SELECT count(*) FROM goatos_schema_migrations WHERE version='999999_tx_probe'`).Scan(&recorded); scanErr != nil {
		t.Fatalf("check schema version: %v", scanErr)
	}
	if recorded != 0 {
		t.Fatalf("schema version recorded = %d, want 0", recorded)
	}
}

func TestSplitSQLStatementsPreservesFunctionBodies(t *testing.T) {
	sql := `
CREATE FUNCTION f()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.value := 'a;b';
  RETURN NEW;
END;
$$;
CREATE INDEX CONCURRENTLY IF NOT EXISTS t_idx ON t (id);
`
	statements, err := splitSQLStatements(sql)
	if err != nil {
		t.Fatalf("splitSQLStatements: %v", err)
	}
	if len(statements) != 2 {
		t.Fatalf("statements=%d, want 2: %#v", len(statements), statements)
	}
	if !strings.Contains(statements[0], "BEGIN") || !strings.Contains(statements[0], "a;b") {
		t.Fatalf("function body was split incorrectly: %s", statements[0])
	}
	if !strings.Contains(statements[1], "CREATE INDEX CONCURRENTLY") {
		t.Fatalf("missing index statement: %s", statements[1])
	}
}

func TestSplitSQLStatementsRejectsUnterminatedQuote(t *testing.T) {
	if _, err := splitSQLStatements("SELECT 'unterminated;"); err == nil {
		t.Fatal("unterminated quote accepted")
	}
}

func TestValidateMigrationTargetRequiresExplicitDevCloudSQL(t *testing.T) {
	t.Setenv("GOATOS_ENV", "dev")
	t.Setenv("GOATOS_ALLOW_DEV_CLOUDSQL_TARGET", "true")
	t.Setenv("GOATOS_DEV_CLOUDSQL_CONNECTION_NAME", "goatos-dev:asia-south1:goatos-dev-core-db")

	validURL := "user=postgres password=goatos dbname=goatos host=/cloudsql/goatos-dev:asia-south1:goatos-dev-core-db sslmode=disable"
	if err := validateMigrationTarget(validURL); err != nil {
		t.Fatalf("valid dev Cloud SQL target rejected: %v", err)
	}
}

func TestValidateMigrationTargetAllowsLocalLoopback(t *testing.T) {
	t.Setenv("GOATOS_ENV", "local")
	localURL := "postgres://postgres:goatos@localhost:5432/goatos?sslmode=disable"
	if err := validateMigrationTarget(localURL); err != nil {
		t.Fatalf("local target rejected: %v", err)
	}
}

func TestValidateMigrationTargetRejectsStagingLookingTarget(t *testing.T) {
	t.Setenv("GOATOS_ENV", "local")
	stagingURL := "postgres://postgres:goatos@127.0.0.1:5432/goatos-stg?sslmode=disable"
	if err := validateMigrationTarget(stagingURL); err == nil {
		t.Fatal("staging-looking target accepted")
	}
}

func TestValidateMigrationTargetRequiresExplicitStagingCloudSQL(t *testing.T) {
	t.Setenv("GOATOS_ENV", "stg")
	t.Setenv("GOATOS_ALLOW_STG_CLOUDSQL_TARGET", "true")
	t.Setenv("GOATOS_STG_CLOUDSQL_CONNECTION_NAME", "goatos-stg:asia-south1:goatos-stg-core-db")

	validURL := "user=goatos_app password=goatos dbname=goatos host=/cloudsql/goatos-stg:asia-south1:goatos-stg-core-db sslmode=disable"
	if err := validateMigrationTarget(validURL); err != nil {
		t.Fatalf("valid stg Cloud SQL target rejected: %v", err)
	}
}

func TestValidateLocalChecksumDriftTargetOnlyAllowsLocalLoopback(t *testing.T) {
	localURL := "postgres://postgres:goatos@127.0.0.1:5432/goatos?sslmode=disable"

	t.Setenv("GOATOS_ENV", "dev")
	if err := validateLocalChecksumDriftTarget(localURL); err == nil {
		t.Fatal("checksum drift allowance accepted without GOATOS_ENV=local")
	}

	t.Setenv("GOATOS_ENV", "local")
	cloudSQLURL := "user=postgres password=goatos dbname=goatos host=/cloudsql/goatos-dev:asia-south1:goatos-dev-core-db sslmode=disable"
	if err := validateLocalChecksumDriftTarget(cloudSQLURL); err == nil {
		t.Fatal("checksum drift allowance accepted for Cloud SQL target")
	}

	if err := validateLocalChecksumDriftTarget(localURL); err != nil {
		t.Fatalf("local checksum drift allowance rejected: %v", err)
	}
}
