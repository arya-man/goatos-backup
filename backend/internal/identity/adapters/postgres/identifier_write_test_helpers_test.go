package postgres

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const correctionActor = "90000000-0000-4000-8000-000000000001"

func startCorrectionWriteDB(t *testing.T, ctx context.Context) (*pgxpool.Pool, *Repository) {
	t.Helper()
	container := fmt.Sprintf("goatos-identifier-write-test-%d", time.Now().UnixNano())
	postgresImage := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if postgresImage == "" {
		postgresImage = defaultPostgresImage
	}
	run(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", postgresImage)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	})
	ready := false
	for range 60 {
		if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos").Run() == nil {
			ready = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("postgres container did not become ready:\n%s", runOutput(t, "docker", "logs", container))
	}
	applyMigrations(t, container)
	seedRepositoryData(t, container)
	pool := openPool(t, ctx, container)
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
