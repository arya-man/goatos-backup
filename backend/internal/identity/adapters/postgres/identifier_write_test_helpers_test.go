package postgres

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const correctionActor = "90000000-0000-4000-8000-000000000001"

// startCorrectionWriteDB starts the package's migrated Postgres and seeds the synthetic register.
//
// It used to run `docker run` itself. That bypassed platform/pgtest and therefore ignored
// GOATOS_PGTEST_ADMIN_DSN -- the sanctioned no-Docker path pgtest exists for, because on a machine
// where Docker is disallowed the alternative is not "run it another way", it is "the gate never
// runs". This package is a named proof for four registered chains (goat.created,
// goat.stage_changed, goat.identity.changed, goat.identifier.added), and on such a machine none of
// them could execute at all. The package's TestMain already ran pgtest.RunMain, so the conversion
// was begun and left unfinished.
//
// pgtest applies every committed migration to its template, so applyMigrations is gone with it.
func startCorrectionWriteDB(t *testing.T, ctx context.Context) (*pgxpool.Pool, *Repository) {
	t.Helper()
	pool := pgtest.StartPostgres(t, ctx)
	seedRepositoryData(t, ctx, pool)
	return pool, NewRepository(pool, 5*time.Second)
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func queryBytes(t *testing.T, pool *pgxpool.Pool, query string, args ...any) []byte {
	t.Helper()
	var out []byte
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertIdempotencyCompleted(t *testing.T, pool *pgxpool.Pool, key, resultID string) {
	t.Helper()
	var status, storedResult string
	if err := pool.QueryRow(context.Background(), `SELECT status, result_id::text FROM idempotency_keys WHERE idempotency_key = $1`, key).Scan(&status, &storedResult); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || storedResult != resultID {
		t.Fatalf("idempotency row status=%s result=%s, want completed %s", status, storedResult, resultID)
	}
}

func validateDomainEventEnvelope(t *testing.T, payload []byte) {
	t.Helper()
	validateJSONSchema(t, "domain-event-envelope.schema.json", payload)
}

func validateDecisionRecord(t *testing.T, payload []byte) {
	t.Helper()
	validateJSONSchema(t, "decision-record.schema.json", payload)
}

func validateJSONSchema(t *testing.T, schemaName string, payload []byte) {
	t.Helper()
	root := repoRoot(t)
	schemaPath := filepath.Join(root, "contracts", "jsonschema", schemaName)
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaName, schemaDoc); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(schemaName)
	if err != nil {
		t.Fatal(err)
	}
	payloadDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(payloadDoc); err != nil {
		t.Fatalf("payload failed %s validation: %v\n%s", schemaName, err, string(payload))
	}
}
