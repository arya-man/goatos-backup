package main

import (
	"strings"
	"testing"
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
