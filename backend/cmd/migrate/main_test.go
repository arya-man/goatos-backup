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

func TestAllowedHistoricalChecksumsAcceptOnlyAuditedGoatosDBPairs(t *testing.T) {
	cases := []struct {
		version string
		current string
		applied string
	}{
		{"000035_death_upload_before_approval", "sha256:cf443ab9807a012553d04ad764577f1885ea990e0fd4b370b58bd9f8f1a95efb", "sha256:daca76c460dba159de9d9dff35c1694638e093e91ae7773ce13a6a9b1d6a15f5"},
		{"000036_death_two_operator_actions", "sha256:306c83248d800baba7d47b39681e8133a4f3d4fe989402f6ebf74461b4a0752a", "sha256:fa64a6e997e28a0a0222271ad7862f9afd522192e42b002cc3ac21f3feb91abd"},
		{"000038_counts_submitter_pending_index", "sha256:99fa47f84cc38475acc609ee20926cbb5a1f7f3cf8f43a8fae436dbd2377e6bd", "sha256:7ec8991c8283f36c55fc87025512f671d1378e93df7b021bf353b46056dc4c3c"},
		{"000041_birth_mother_video_medicine", "sha256:764c18b63e8dcb6cdacfbfad22f91aed7f718b0b43aa56466385620d6b7c56b6", "sha256:b1350ee397b966168b3b27e188530de94d50d2e89114b76dfe98842b4b79751b"},
		{"000042_birth_litter_video_contract", "sha256:064fae166174f4397d8baedfc318a5adf9e94ff01df69db3bae174ae639e7ae6", "sha256:3bfea97c8be106a3b2a2acaa155c39f569b989cdcd497ce7f5dd4e5aadfcbd2e"},
		{"000044_birth_ors_second_round_gate", "sha256:36ba3dbc1043da7f2aa99d92bd0b558007469f321ecc3e1f47765e29f20d859b", "sha256:e069f6eb6a7bcd1db8c57cb0d50e4b34e5a439cee2a4122723b2319bba35e59a"},
		{"000045_birth_ors_reopened_card_sync", "sha256:e97e70e0a86531a451f21ae3cc65f9ff774404c2b4b8a9873f8aeba7061d0464", "sha256:dce6a89bff449645c04e0de43ff1bcdb60fe52aba1eb9658b3d7ef4b30658fc5"},
		{"000046_birth_weight_and_colostrum_repair", "sha256:0f0873a5149c5582ccfd96d830674669cd343fbf1efb29a4168c96b5eb0a8d06", "ef8eb3e8f4ab306b9270831d79aaaa910b646a08ecc70d3988ac5ac073d5e0d7"},
		{"000047_birth_colostrum_card_counts", "sha256:0abe9e413b9793a3b0a133c09e828adac0e8d7ac8f57f974d880a3c62ddbdacf", "15053660bb0686a60e496ed645bad7db72d1cab7915aa29e7e859cf3fe9e6274"},
		{"000052_shifting_management_stage_selection", "sha256:a0b12a06829e63aed9204b5755f522d86c265be46d32778c4efdd22c13070662", "sha256:65e4e4b2dc1cde852eadd602f06a6baa8b306a54f0538cbbf14ee327bea8be64"},
	}

	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			migration := migrationFile{Version: tc.version, Filename: tc.version + ".sql", Checksum: tc.current}
			if !isAllowedHistoricalChecksum(migration, tc.applied) {
				t.Fatal("audited historical checksum pair was rejected")
			}
			if isAllowedHistoricalChecksum(migration, "sha256:unexpected") {
				t.Fatal("unexpected applied checksum was accepted")
			}
			migration.Checksum = "sha256:unexpected-current"
			if isAllowedHistoricalChecksum(migration, tc.applied) {
				t.Fatal("historical checksum was accepted for unexpected current migration content")
			}
		})
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
