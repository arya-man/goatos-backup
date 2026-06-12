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
		if result.CorrectionRequest.RowVersion == nil || *result.CorrectionRequest.RowVersion != 1 {
			t.Fatalf("create row_version missing or wrong: %#v", result.CorrectionRequest.RowVersion)
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
			if result.CorrectionRequest.RowVersion == nil || *result.CorrectionRequest.RowVersion != 1 {
				t.Fatalf("concurrent create/replay row_version missing or wrong: replay=%v row_version=%#v", result.Replayed, result.CorrectionRequest.RowVersion)
			}
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

	t.Run("resolve approved writes decision audit outbox and schema-valid payloads", func(t *testing.T) {
		create := correctionCommand(t, meshaTenant, "idem-resolve-create-0001", "synthetic resolve approved correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		created, err := repo.CreateCorrectionRequest(ctx, create)
		if err != nil {
			t.Fatalf("create for resolve: %v", err)
		}
		correctionID := created.CorrectionRequest.CorrectionRequestID
		cmd := resolveCommand(t, meshaTenant, "idem-resolve-0001", correctionID, "approved", 1, "synthetic approval after manual review")
		result, err := repo.ResolveCorrectionRequest(ctx, cmd)
		if err != nil {
			t.Fatalf("ResolveCorrectionRequest: %v", err)
		}
		if result.Replayed || result.CorrectionRequest.State != "approved" {
			t.Fatalf("unexpected resolve result: %#v", result)
		}
		if result.CorrectionRequest.RowVersion == nil || *result.CorrectionRequest.RowVersion != 2 || result.CorrectionRequest.ResolvedAt == nil {
			t.Fatalf("row_version/resolved_at not updated: %#v", result.CorrectionRequest)
		}
		if result.Decision.DecisionType != "resolve_correction_request" || result.Decision.DecisionState != "approved" || result.Decision.DecisionResult != "approved" {
			t.Fatalf("unexpected decision summary: %#v", result.Decision)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM identity_decisions WHERE decision_id = $1 AND tenant_id = $2", result.Decision.DecisionID, meshaTenant); got != 1 {
			t.Fatalf("decision rows = %d", got)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM audit_log WHERE action = 'identity.correction_request.updated' AND resource_id = $1", correctionID); got != 1 {
			t.Fatalf("resolve audit rows = %d", got)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM outbox_messages WHERE event_type = 'identity.correction_request.updated' AND aggregate_id = $1", correctionID); got != 1 {
			t.Fatalf("resolve outbox rows = %d", got)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM goat_identity_events WHERE idempotency_key = $1", cmd.StoredIdempotencyKey); got != 0 {
			t.Fatalf("resolve wrote goat_identity_events rows = %d", got)
		}
		decisionPayload := queryBytes(t, pool, "SELECT evidence->'decision_record' FROM identity_decisions WHERE decision_id = $1", result.Decision.DecisionID)
		validateDecisionRecord(t, decisionPayload)
		var decisionRecord map[string]any
		if err := json.Unmarshal(decisionPayload, &decisionRecord); err != nil {
			t.Fatal(err)
		}
		if decisionRecord["policy_version"] != "phase1-manual-correction-review-v1" || decisionRecord["decision_result"] != "approved" {
			t.Fatalf("unexpected decision record: %#v", decisionRecord)
		}
		evidence := decisionRecord["evidence"].(map[string]any)
		refs := evidence["evidence_refs"].([]any)
		firstRef := refs[0].(map[string]any)
		if firstRef["source_system"] != "synthetic_import" || firstRef["description"] != "Synthetic manual review note." {
			t.Fatalf("evidence refs not round-tripped: %#v", refs)
		}
		payload := queryBytes(t, pool, "SELECT payload FROM outbox_messages WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		validateDomainEventEnvelope(t, payload)
		var envelope map[string]any
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope["event_type"] != "identity.correction_request.updated" || envelope["aggregate_type"] != "correction_request" || envelope["subject_type"] != "correction_request" {
			t.Fatalf("unexpected resolve envelope: %#v", envelope)
		}
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, correctionID)
	})

	t.Run("resolve needs field check stays nonterminal", func(t *testing.T) {
		create := correctionCommand(t, meshaTenant, "idem-resolve-create-0002", "synthetic needs field check correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		created, err := repo.CreateCorrectionRequest(ctx, create)
		if err != nil {
			t.Fatalf("create for resolve: %v", err)
		}
		cmd := resolveCommand(t, meshaTenant, "idem-resolve-0002", created.CorrectionRequest.CorrectionRequestID, "needs_field_check", 1, "synthetic field check requested")
		result, err := repo.ResolveCorrectionRequest(ctx, cmd)
		if err != nil {
			t.Fatalf("ResolveCorrectionRequest needs_field_check: %v", err)
		}
		if result.CorrectionRequest.State != "needs_field_check" || result.CorrectionRequest.ResolvedAt != nil {
			t.Fatalf("expected nonterminal field-check state, got %#v", result.CorrectionRequest)
		}
		if result.CorrectionRequest.RowVersion == nil || *result.CorrectionRequest.RowVersion != 2 {
			t.Fatalf("row_version not incremented: %#v", result.CorrectionRequest.RowVersion)
		}
		if result.Decision.DecisionState != "needs_review" || result.Decision.DecisionResult != "needs_field_check" {
			t.Fatalf("unexpected needs_field_check decision: %#v", result.Decision)
		}
	})

	t.Run("resolve closed maps to approved decision state and closed result", func(t *testing.T) {
		create := correctionCommand(t, meshaTenant, "idem-resolve-create-0003", "synthetic closed correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		created, err := repo.CreateCorrectionRequest(ctx, create)
		if err != nil {
			t.Fatalf("create for resolve: %v", err)
		}
		cmd := resolveCommand(t, meshaTenant, "idem-resolve-0003", created.CorrectionRequest.CorrectionRequestID, "closed", 1, "synthetic close without identity mutation")
		result, err := repo.ResolveCorrectionRequest(ctx, cmd)
		if err != nil {
			t.Fatalf("ResolveCorrectionRequest closed: %v", err)
		}
		if result.CorrectionRequest.State != "closed" || result.CorrectionRequest.ResolvedAt == nil {
			t.Fatalf("expected terminal closed state, got %#v", result.CorrectionRequest)
		}
		if result.Decision.DecisionState != "approved" || result.Decision.DecisionResult != "closed" {
			t.Fatalf("unexpected closed decision: %#v", result.Decision)
		}
	})

	t.Run("resolve rejects terminal same-state stale-version and wrong-tenant writes", func(t *testing.T) {
		terminalCreate := correctionCommand(t, meshaTenant, "idem-resolve-create-0004", "synthetic terminal conflict correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		terminalCreated, err := repo.CreateCorrectionRequest(ctx, terminalCreate)
		if err != nil {
			t.Fatalf("create terminal conflict: %v", err)
		}
		terminalID := terminalCreated.CorrectionRequest.CorrectionRequestID
		if _, err := repo.ResolveCorrectionRequest(ctx, resolveCommand(t, meshaTenant, "idem-resolve-0004", terminalID, "approved", 1, "synthetic first terminal resolution")); err != nil {
			t.Fatalf("first terminal resolve: %v", err)
		}
		terminalConflict := resolveCommand(t, meshaTenant, "idem-resolve-0005", terminalID, "rejected", 2, "synthetic terminal conflict attempt")
		if _, err := repo.ResolveCorrectionRequest(ctx, terminalConflict); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected terminal write conflict, got %v", err)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", terminalConflict.StoredIdempotencyKey); got != 0 {
			t.Fatalf("terminal conflict left idempotency row count=%d", got)
		}

		fieldCheckCreate := correctionCommand(t, meshaTenant, "idem-resolve-create-0005", "synthetic same-state conflict correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		fieldCheckCreated, err := repo.CreateCorrectionRequest(ctx, fieldCheckCreate)
		if err != nil {
			t.Fatalf("create same-state conflict: %v", err)
		}
		fieldCheckID := fieldCheckCreated.CorrectionRequest.CorrectionRequestID
		if _, err := repo.ResolveCorrectionRequest(ctx, resolveCommand(t, meshaTenant, "idem-resolve-0006", fieldCheckID, "needs_field_check", 1, "synthetic first field check")); err != nil {
			t.Fatalf("first field-check resolve: %v", err)
		}
		sameState := resolveCommand(t, meshaTenant, "idem-resolve-0007", fieldCheckID, "needs_field_check", 2, "synthetic same-state attempt")
		if _, err := repo.ResolveCorrectionRequest(ctx, sameState); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected same-state write conflict, got %v", err)
		}

		staleCreate := correctionCommand(t, meshaTenant, "idem-resolve-create-0006", "synthetic stale row version correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		staleCreated, err := repo.CreateCorrectionRequest(ctx, staleCreate)
		if err != nil {
			t.Fatalf("create stale conflict: %v", err)
		}
		stale := resolveCommand(t, meshaTenant, "idem-resolve-0008", staleCreated.CorrectionRequest.CorrectionRequestID, "approved", 2, "synthetic stale row version attempt")
		if _, err := repo.ResolveCorrectionRequest(ctx, stale); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected stale row_version conflict, got %v", err)
		}

		wrongTenant := resolveCommand(t, secondTenant, "idem-resolve-0009", staleCreated.CorrectionRequest.CorrectionRequestID, "approved", 1, "synthetic wrong tenant attempt")
		if _, err := repo.ResolveCorrectionRequest(ctx, wrongTenant); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("expected wrong tenant not found, got %v", err)
		}
	})

	t.Run("resolve exact replay works and changed body conflicts", func(t *testing.T) {
		create := correctionCommand(t, meshaTenant, "idem-resolve-create-0007", "synthetic replay correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		created, err := repo.CreateCorrectionRequest(ctx, create)
		if err != nil {
			t.Fatalf("create replay correction: %v", err)
		}
		correctionID := created.CorrectionRequest.CorrectionRequestID
		cmd := resolveCommand(t, meshaTenant, "idem-resolve-0010", correctionID, "approved", 1, "synthetic replay approval")
		first, err := repo.ResolveCorrectionRequest(ctx, cmd)
		if err != nil {
			t.Fatalf("first resolve replay test: %v", err)
		}
		replay, err := repo.ResolveCorrectionRequest(ctx, cmd)
		if err != nil {
			t.Fatalf("replay resolve: %v", err)
		}
		if !replay.Replayed || replay.FirstResultID == nil || *replay.FirstResultID != correctionID {
			t.Fatalf("expected replay metadata, got %#v", replay)
		}
		if replay.CorrectionRequest.CorrectionRequestID != first.CorrectionRequest.CorrectionRequestID || replay.Decision.DecisionID != first.Decision.DecisionID {
			t.Fatalf("replay did not refetch original result: first=%#v replay=%#v", first, replay)
		}
		changed := resolveCommand(t, meshaTenant, "idem-resolve-0010", correctionID, "approved", 1, "synthetic changed replay body")
		if _, err := repo.ResolveCorrectionRequest(ctx, changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("expected changed-body idempotency conflict, got %v", err)
		}
	})

	t.Run("resolve forced failure rolls back decision correction audit outbox and idempotency", func(t *testing.T) {
		create := correctionCommand(t, meshaTenant, "idem-resolve-create-0008", "synthetic resolve rollback correction", domain.LocationScope{ParkID: strPtr(cbeLocation)}, nil)
		created, err := repo.CreateCorrectionRequest(ctx, create)
		if err != nil {
			t.Fatalf("create rollback correction: %v", err)
		}
		correctionID := created.CorrectionRequest.CorrectionRequestID
		cmd := resolveCommand(t, meshaTenant, "idem-resolve-0011", correctionID, "approved", 1, "synthetic rollback approval")
		cmd.TraceID = "trace-resolve-forced-rollback"
		repo.afterAuditHook = func(context.Context) error { return errors.New("forced resolve rollback") }
		_, err = repo.ResolveCorrectionRequest(ctx, cmd)
		repo.afterAuditHook = nil
		if err == nil {
			t.Fatal("expected forced resolve rollback error")
		}
		var state string
		var rowVersion int
		var decisionID string
		var hasResolvedAt bool
		if err := pool.QueryRow(ctx, `SELECT state, row_version, COALESCE(decision_id::text, ''), resolved_at IS NOT NULL FROM identity_correction_requests WHERE correction_request_id = $1`, correctionID).Scan(&state, &rowVersion, &decisionID, &hasResolvedAt); err != nil {
			t.Fatal(err)
		}
		if state != "open" || rowVersion != 1 || decisionID != "" || hasResolvedAt {
			t.Fatalf("correction mutated despite rollback: state=%s row_version=%d decision=%q resolved=%v", state, rowVersion, decisionID, hasResolvedAt)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", cmd.StoredIdempotencyKey); got != 0 {
			t.Fatalf("idempotency rows after resolve rollback = %d", got)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM identity_decisions WHERE evidence->'decision_record'->>'trace_id' = $1", cmd.TraceID); got != 0 {
			t.Fatalf("decision rows after resolve rollback = %d", got)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM audit_log WHERE trace_id = $1", cmd.TraceID); got != 0 {
			t.Fatalf("audit rows after resolve rollback = %d", got)
		}
		if got := countRows(t, pool, "SELECT count(*) FROM outbox_messages WHERE trace_id = $1", cmd.TraceID); got != 0 {
			t.Fatalf("outbox rows after resolve rollback = %d", got)
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

func resolveCommand(t *testing.T, tenantID, key, correctionID, state string, rowVersion int, reason string) ports.ResolveCorrectionRequestCommand {
	t.Helper()
	evidenceRefs := []domain.EvidenceRef{{
		EvidenceType: "source_record",
		EvidenceID:   "synthetic-row-1",
		SourceSystem: strPtr("synthetic_import"),
		Description:  strPtr("Synthetic manual review note."),
	}}
	body := map[string]any{
		"state":         state,
		"reason":        reason,
		"evidence_refs": evidenceRefs,
		"row_version":   rowVersion,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	route := "/admin/identity/correction-requests/" + correctionID + "/resolve"
	hash, err := app.CanonicalRequestHashWithSubject(tenantID, "resolveCorrectionRequest", route, correctionID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.ResolveCorrectionRequestCommand{
		TenantID:             tenantID,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: tenantID + ":resolveCorrectionRequest:" + key,
		IdempotencyScope:     "resolveCorrectionRequest",
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		CorrectionRequestID:  correctionID,
		TargetState:          state,
		Reason:               reason,
		EvidenceRefs:         evidenceRefs,
		RowVersion:           rowVersion,
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
