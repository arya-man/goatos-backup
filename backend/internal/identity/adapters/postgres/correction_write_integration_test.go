package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const correctionActor = "90000000-0000-4000-8000-000000000001"

func TestCorrectionRequestWritePathWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	t.Run("goatless success writes correction audit outbox and schema-valid payload", func(t *testing.T) {
		cmd := correctionCommand(t, meshaTenant, "idem-write-0001", "synthetic goatless correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		result, err := repo.CreateCorrectionRequest(ctx, cmd)
		if err != nil {
			t.Fatalf("CreateCorrectionRequest: %v", err)
		}
		if result.Replayed || result.CorrectionRequest.State != "open" || result.CorrectionRequest.GoatID != nil {
			t.Fatalf("unexpected result: %#v", result)
		}
		correctionID := result.CorrectionRequest.CorrectionRequestID
		if got := countRows(t, pool, "SELECT count(*) FROM identity_correction_requests WHERE correction_request_id = $1", correctionID); got != 1 {
			t.Fatalf("correction rows = %d", got)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM audit_log WHERE resource_type = 'correction_request' AND resource_id = $1", correctionID); got != 1 {
			t.Fatalf("audit rows = %d", got)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM outbox_messages WHERE aggregate_type = 'correction_request' AND aggregate_id = $1", correctionID); got != 1 {
			t.Fatalf("outbox rows = %d", got)
		}
		payload := queryBytes(t, pool, "SELECT payload FROM outbox_messages WHERE aggregate_id = $1", correctionID)
		validateDomainEventEnvelope(t, payload)
		var envelope map[string]any
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope["aggregate_type"] != "correction_request" || envelope["subject_type"] != "correction_request" {
			t.Fatalf("unexpected envelope aggregate/subject: %#v", envelope)
		}
		actor := envelope["actor"].(map[string]any)
		if actor["actor_id"] != correctionActor {
			t.Fatalf("actor missing from envelope: %#v", actor)
		}
		visibility := envelope["visibility_scope"].(map[string]any)
		if visibility["tenant_id"] != meshaTenant || visibility["park_id"] != cbeLocation {
			t.Fatalf("visibility scope mismatch: %#v", visibility)
		}
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, correctionID)
	})

	t.Run("location scope maps to database columns", func(t *testing.T) {
		scope := domain.LocationScope{FarmID: strPtr(cbeLocation), ParkID: strPtr(cbeLocation), ShedID: strPtr(cbeLocation), CohortID: strPtr(cbeLocation)}
		cmd := correctionCommand(t, meshaTenant, "idem-write-0002", "synthetic scoped correction", scope, nil)
		result, err := repo.CreateCorrectionRequest(ctx, cmd)
		if err != nil {
			t.Fatalf("CreateCorrectionRequest: %v", err)
		}
		var farmID, parkID, shedID, cohortID string
		if err := pool.QueryRow(ctx, `SELECT farm_id::text, park_id::text, shed_id::text, cohort_id::text FROM identity_correction_requests WHERE correction_request_id = $1`, result.CorrectionRequest.CorrectionRequestID).Scan(&farmID, &parkID, &shedID, &cohortID); err != nil {
			t.Fatal(err)
		}
		if farmID != cbeLocation || parkID != cbeLocation || shedID != cbeLocation || cohortID != cbeLocation {
			t.Fatalf("location scope columns = %s %s %s %s", farmID, parkID, shedID, cohortID)
		}
	})

	t.Run("goat linked request validates tenant ownership", func(t *testing.T) {
		goatID := "10000000-0000-4000-8000-000000000001"
		cmd := correctionCommand(t, meshaTenant, "idem-write-0003", "synthetic goat linked correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, &goatID)
		result, err := repo.CreateCorrectionRequest(ctx, cmd)
		if err != nil {
			t.Fatalf("CreateCorrectionRequest linked: %v", err)
		}
		if result.CorrectionRequest.GoatID == nil || *result.CorrectionRequest.GoatID != goatID {
			t.Fatalf("goat id not round-tripped: %#v", result.CorrectionRequest.GoatID)
		}

		otherTenantGoat := "10000000-0000-4000-8000-000000000101"
		bad := correctionCommand(t, meshaTenant, "idem-write-0004", "synthetic wrong tenant goat", domain.LocationScope{ParkID: strPtr(cbeLocation)}, &otherTenantGoat)
		if _, err := repo.CreateCorrectionRequest(ctx, bad); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("expected tenant ownership ErrNotFound, got %v", err)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", bad.StoredIdempotencyKey); got != 0 {
			t.Fatalf("failed transaction left idempotency row count=%d", got)
		}
	})

	t.Run("forced failure rolls back every write", func(t *testing.T) {
		cmd := correctionCommand(t, meshaTenant, "idem-write-0005", "synthetic rollback correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		cmd.TraceID = "trace-forced-rollback"
		repo.afterAuditHook = func(context.Context) error { return errors.New("forced rollback") }
		_, err := repo.CreateCorrectionRequest(ctx, cmd)
		repo.afterAuditHook = nil
		if err == nil {
			t.Fatal("expected forced rollback error")
		}
		if got := countRows(t, pool, "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", cmd.StoredIdempotencyKey); got != 0 {
			t.Fatalf("idempotency rows after rollback = %d", got)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM identity_correction_requests WHERE description = $1", cmd.Description); got != 0 {
			t.Fatalf("correction rows after rollback = %d", got)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM audit_log WHERE trace_id = $1", cmd.TraceID); got != 0 {
			t.Fatalf("audit rows after rollback = %d", got)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM outbox_messages WHERE trace_id = $1", cmd.TraceID); got != 0 {
			t.Fatalf("outbox rows after rollback = %d", got)
		}
	})

	t.Run("concurrent same key creates once and replays loser", func(t *testing.T) {
		cmd := correctionCommand(t, meshaTenant, "idem-write-0006", "synthetic concurrent correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		var hookCalls int32
		repo.afterAuditHook = func(context.Context) error {
			if atomic.AddInt32(&hookCalls, 1) == 1 {
				time.Sleep(300 * time.Millisecond)
			}
			return nil
		}
		defer func() { repo.afterAuditHook = nil }()

		start := make(chan struct{})
		results := make(chan *ports.CreateCorrectionRequestResult, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				result, err := repo.CreateCorrectionRequest(ctx, cmd)
				if err != nil {
					errs <- err
					return
				}
				results <- result
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			t.Fatalf("concurrent request error: %v", err)
		}
		var fresh, replay int
		var correctionID string
		for result := range results {
			if result.Replayed {
				replay++
			} else {
				fresh++
			}
			correctionID = result.CorrectionRequest.CorrectionRequestID
		}
		if fresh != 1 || replay != 1 {
			t.Fatalf("fresh=%d replay=%d", fresh, replay)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM identity_correction_requests WHERE description = $1", cmd.Description); got != 1 {
			t.Fatalf("correction rows for concurrent key = %d", got)
		}
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, correctionID)
	})

	t.Run("same client key in different tenants is namespaced", func(t *testing.T) {
		key := "idem-cross-tenant"
		cmdA := correctionCommand(t, meshaTenant, key, "synthetic tenant A correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		cmdB := correctionCommand(t, secondTenant, key, "synthetic tenant B correction", domain.LocationScope{ParkID: strPtr(t2Location)}, nil)
		resultA, err := repo.CreateCorrectionRequest(ctx, cmdA)
		if err != nil {
			t.Fatalf("tenant A create: %v", err)
		}
		resultB, err := repo.CreateCorrectionRequest(ctx, cmdB)
		if err != nil {
			t.Fatalf("tenant B create: %v", err)
		}
		if resultA.Replayed || resultB.Replayed {
			t.Fatalf("cross-tenant creates should both be fresh: A=%#v B=%#v", resultA, resultB)
		}
		if cmdA.StoredIdempotencyKey == cmdB.StoredIdempotencyKey {
			t.Fatal("stored idempotency keys collided across tenants")
		}
	})

	t.Run("same idempotency key with different body conflicts", func(t *testing.T) {
		key := "idem-write-0007"
		first := correctionCommand(t, meshaTenant, key, "synthetic first body", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		if _, err := repo.CreateCorrectionRequest(ctx, first); err != nil {
			t.Fatalf("first create: %v", err)
		}
		second := correctionCommand(t, meshaTenant, key, "synthetic changed body", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		if _, err := repo.CreateCorrectionRequest(ctx, second); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("expected idempotency conflict, got %v", err)
		}
	})
}

func startCorrectionWriteDB(t *testing.T, ctx context.Context) (*pgxpool.Pool, *Repository) {
	t.Helper()
	container := fmt.Sprintf("goatos-correction-write-test-%d", time.Now().UnixNano())
	postgresImage := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if postgresImage == "" {
		postgresImage = defaultPostgresImage
	}
	run(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", postgresImage)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	})
	ready := false
	for i := 0; i < 60; i++ {
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

func correctionCommand(t *testing.T, tenantID, key, description string, scope domain.LocationScope, goatID *string) ports.CreateCorrectionRequestCommand {
	t.Helper()
	body := map[string]any{
		"request_type":     "missing_tag",
		"location_scope":   scope,
		"description":      description,
		"evidence_refs":    []domain.EvidenceRef{{EvidenceType: "source_record", EvidenceID: "synthetic-row-1", SourceSystem: strPtr("synthetic_import")}},
		"identifier_type":  "old_tag",
		"identifier_value": "synthetic-tag-1",
	}
	if goatID != nil {
		body["goat_id"] = *goatID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := app.CanonicalRequestHash(tenantID, "createCorrectionRequest", "/identity/correction-requests", raw)
	if err != nil {
		t.Fatal(err)
	}
	identifierType := "old_tag"
	identifierValue := "synthetic-tag-1"
	return ports.CreateCorrectionRequestCommand{
		TenantID:             tenantID,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: tenantID + ":createCorrectionRequest:" + key,
		IdempotencyScope:     "createCorrectionRequest",
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		RequestType:          "missing_tag",
		GoatID:               goatID,
		IdentifierType:       &identifierType,
		IdentifierValue:      &identifierValue,
		LocationScope:        scope,
		Description:          description,
		EvidenceRefs:         []domain.EvidenceRef{{EvidenceType: "source_record", EvidenceID: "synthetic-row-1", SourceSystem: strPtr("synthetic_import")}},
	}
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
	root := repoRoot(t)
	schemaPath := filepath.Join(root, "contracts", "jsonschema", "domain-event-envelope.schema.json")
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("domain-event-envelope.schema.json", schemaDoc); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("domain-event-envelope.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	payloadDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(payloadDoc); err != nil {
		t.Fatalf("outbox payload failed domain event schema validation: %v\n%s", err, string(payload))
	}
}
