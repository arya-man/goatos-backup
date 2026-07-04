package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/outbox/domain"
	"github.com/vgoats/goatos/backend/internal/outbox/ports"
)

const (
	defaultPostgresImage = "postgres:16.9-alpine"
	meshaTenant          = "00000000-0000-4000-8000-000000000001"
	meshaParty           = "00000000-0000-4000-8000-000000001001"
	outboxGoatID         = "91000000-0000-4000-8000-000000000001"
)

var outboxTestNow = time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)

func TestOutboxRelayWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool := startOutboxDB(t, ctx)
	defer pool.Close()
	seedOutboxGoat(t, pool)

	t.Run("fresh pending null next attempt is claimed and future next attempt is skipped", func(t *testing.T) {
		publisher := &fakePublisher{}
		service := newRelayService(t, pool, publisher, outboxapp.Config{Limit: 10, MaxAttempts: 5, Now: fixedNow})
		due := insertOutboxMessage(t, pool, outboxRow{Suffix: 1, Status: domain.StatusPending})
		future := insertOutboxMessage(t, pool, outboxRow{Suffix: 2, Status: domain.StatusPending, NextAttemptAt: timePtr(outboxTestNow.Add(time.Hour))})

		result, err := service.RunOnce(ctx)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if result.ClaimedCount != 1 || result.PublishedCount != 1 {
			t.Fatalf("unexpected result: %#v", result)
		}
		if publisher.CallCount() != 1 || !publisher.Called(due.OutboxID) {
			t.Fatalf("publisher calls=%#v want only %s", publisher.Calls(), due.OutboxID)
		}
		assertOutboxState(t, pool, due.OutboxID, domain.StatusPublished, 1, "", false, true)
		assertOutboxState(t, pool, future.OutboxID, domain.StatusPending, 0, "", true, false)
	})

	t.Run("two workers do not publish the same row", func(t *testing.T) {
		row := insertOutboxMessage(t, pool, outboxRow{Suffix: 10, Status: domain.StatusPending})
		started := make(chan struct{})
		release := make(chan struct{})
		blockingPublisher := &fakePublisher{blockOutboxID: row.OutboxID, started: started, release: release}
		first := newRelayService(t, pool, blockingPublisher, outboxapp.Config{Limit: 1, MaxAttempts: 5, Now: fixedNow})
		secondPublisher := &fakePublisher{}
		second := newRelayService(t, pool, secondPublisher, outboxapp.Config{Limit: 1, MaxAttempts: 5, Now: fixedNow})

		errs := make(chan error, 1)
		go func() {
			_, err := first.RunOnce(ctx)
			errs <- err
		}()
		<-started
		secondResult, err := second.RunOnce(ctx)
		if err != nil {
			t.Fatalf("second RunOnce: %v", err)
		}
		if secondResult.ClaimedCount != 0 || secondPublisher.CallCount() != 0 {
			t.Fatalf("second worker claimed/published same row: result=%#v calls=%#v", secondResult, secondPublisher.Calls())
		}
		close(release)
		if err := <-errs; err != nil {
			t.Fatalf("first RunOnce: %v", err)
		}
		if blockingPublisher.CallCount() != 1 {
			t.Fatalf("first publisher calls=%d", blockingPublisher.CallCount())
		}
		assertOutboxState(t, pool, row.OutboxID, domain.StatusPublished, 1, "", false, true)
	})

	t.Run("retryable publish failure schedules retry with sanitized error", func(t *testing.T) {
		row := insertOutboxMessage(t, pool, outboxRow{Suffix: 20, Status: domain.StatusPending})
		publisher := &fakePublisher{fail: map[string]error{
			row.OutboxID: errors.New("publish failed for 9900000000000000000000000000001 SYNTHETIC_PRIVATE_TAG"),
		}}
		service := newRelayService(t, pool, publisher, outboxapp.Config{Limit: 10, MaxAttempts: 5, BackoffBase: time.Minute, Now: fixedNow})

		result, err := service.RunOnce(ctx)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if result.RetryScheduledCount != 1 {
			t.Fatalf("unexpected result: %#v", result)
		}
		state := queryOutboxState(t, pool, row.OutboxID)
		if state.Status != domain.StatusPending || state.AttemptCount != 1 || !state.NextAttemptAt.Valid || !state.NextAttemptAt.Time.After(outboxTestNow) {
			t.Fatalf("retry state mismatch: %#v", state)
		}
		assertNoRawErrorLeak(t, state.LastError)
	})

	t.Run("exhausted attempts dead letter after publish failure", func(t *testing.T) {
		row := insertOutboxMessage(t, pool, outboxRow{Suffix: 30, Status: domain.StatusPending, AttemptCount: 4})
		publisher := &fakePublisher{fail: map[string]error{row.OutboxID: errors.New("temporary publish failure")}}
		service := newRelayService(t, pool, publisher, outboxapp.Config{Limit: 10, MaxAttempts: 5, Now: fixedNow})

		result, err := service.RunOnce(ctx)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if result.DeadLetterCount != 1 {
			t.Fatalf("unexpected result: %#v", result)
		}
		assertOutboxState(t, pool, row.OutboxID, domain.StatusDeadLetter, 5, "max_attempts_exhausted", false, false)
	})

	t.Run("claim time max attempts dead letters without publish", func(t *testing.T) {
		row := insertOutboxMessage(t, pool, outboxRow{
			Suffix:        40,
			Status:        domain.StatusPending,
			AttemptCount:  5,
			NextAttemptAt: timePtr(outboxTestNow.Add(-time.Minute)),
		})
		publisher := &fakePublisher{}
		service := newRelayService(t, pool, publisher, outboxapp.Config{Limit: 10, MaxAttempts: 5, Now: fixedNow})

		result, err := service.RunOnce(ctx)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if result.DeadLetterCount != 1 || result.ClaimedCount != 0 || publisher.CallCount() != 0 {
			t.Fatalf("unexpected result/calls: %#v calls=%#v", result, publisher.Calls())
		}
		assertOutboxState(t, pool, row.OutboxID, domain.StatusDeadLetter, 5, "max_attempts_exhausted_before_publish", false, false)
	})

	t.Run("stale publishing reclaimed and fresh publishing not stolen", func(t *testing.T) {
		repo := NewRepository(pool, 5*time.Second)
		stale := insertOutboxMessage(t, pool, outboxRow{Suffix: 50, Status: domain.StatusPublishing, AttemptCount: 2, UpdatedAt: outboxTestNow.Add(-10 * time.Minute)})
		fresh := insertOutboxMessage(t, pool, outboxRow{Suffix: 51, Status: domain.StatusPublishing, AttemptCount: 3, UpdatedAt: outboxTestNow})

		reclaimed, err := repo.ReclaimStalePublishing(ctx, outboxTestNow, 5*time.Minute)
		if err != nil {
			t.Fatalf("ReclaimStalePublishing: %v", err)
		}
		if reclaimed != 1 {
			t.Fatalf("reclaimed=%d want 1", reclaimed)
		}
		assertOutboxState(t, pool, stale.OutboxID, domain.StatusPending, 2, "", false, false)
		assertOutboxState(t, pool, fresh.OutboxID, domain.StatusPublishing, 3, "", false, false)
		if _, err := pool.Exec(ctx, `UPDATE outbox_messages SET status = 'failed', last_error = 'synthetic_test_complete' WHERE outbox_id = $1`, stale.OutboxID); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("publisher panic is isolated per row", func(t *testing.T) {
		panicRow := insertOutboxMessage(t, pool, outboxRow{Suffix: 60, Status: domain.StatusPending})
		successRow := insertOutboxMessage(t, pool, outboxRow{Suffix: 61, Status: domain.StatusPending})
		publisher := &fakePublisher{panicRows: map[string]bool{panicRow.OutboxID: true}}
		service := newRelayService(t, pool, publisher, outboxapp.Config{Limit: 10, MaxAttempts: 5, Now: fixedNow})

		result, err := service.RunOnce(ctx)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if result.RetryScheduledCount != 1 || result.PublishedCount != 1 {
			t.Fatalf("unexpected result: %#v", result)
		}
		assertOutboxState(t, pool, panicRow.OutboxID, domain.StatusPending, 1, "publish_retry_scheduled", true, false)
		assertOutboxState(t, pool, successRow.OutboxID, domain.StatusPublished, 1, "", false, true)
	})

	t.Run("invalid envelope fails terminal without publish and sanitizes error", func(t *testing.T) {
		rawPayload := json.RawMessage(`{"animal_identifier_1":"AID-SYNTHETIC-001","animal_identifier_2":"AID-SYNTHETIC-002"}`)
		row := insertOutboxMessage(t, pool, outboxRow{Suffix: 70, Status: domain.StatusPending, Payload: rawPayload})
		publisher := &fakePublisher{}
		service := newRelayService(t, pool, publisher, outboxapp.Config{Limit: 10, MaxAttempts: 5, Now: fixedNow})

		result, err := service.RunOnce(ctx)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if result.FailedCount != 1 || publisher.CallCount() != 0 {
			t.Fatalf("unexpected result/calls: %#v calls=%#v", result, publisher.Calls())
		}
		state := queryOutboxState(t, pool, row.OutboxID)
		if state.Status != domain.StatusFailed || state.AttemptCount != 1 || state.LastError != "invalid_event_envelope" {
			t.Fatalf("invalid envelope state=%#v", state)
		}
		assertNoRawErrorLeak(t, state.LastError)
	})

	t.Run("terminal rows are skipped", func(t *testing.T) {
		published := insertOutboxMessage(t, pool, outboxRow{Suffix: 80, Status: domain.StatusPublished, AttemptCount: 1})
		failed := insertOutboxMessage(t, pool, outboxRow{Suffix: 81, Status: domain.StatusFailed, AttemptCount: 1, LastError: "invalid_event_envelope"})
		dead := insertOutboxMessage(t, pool, outboxRow{Suffix: 82, Status: domain.StatusDeadLetter, AttemptCount: 5, LastError: "max_attempts_exhausted"})
		publisher := &fakePublisher{}
		service := newRelayService(t, pool, publisher, outboxapp.Config{Limit: 10, MaxAttempts: 5, Now: fixedNow})

		result, err := service.RunOnce(ctx)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if result.ClaimedCount != 0 || publisher.CallCount() != 0 {
			t.Fatalf("terminal rows should not be claimed: result=%#v calls=%#v", result, publisher.Calls())
		}
		assertOutboxState(t, pool, published.OutboxID, domain.StatusPublished, 1, "", false, false)
		assertOutboxState(t, pool, failed.OutboxID, domain.StatusFailed, 1, "invalid_event_envelope", false, false)
		assertOutboxState(t, pool, dead.OutboxID, domain.StatusDeadLetter, 5, "max_attempts_exhausted", false, false)
	})

	t.Run("dead letters can be listed and replayed to pending", func(t *testing.T) {
		repo := NewRepository(pool, 5*time.Second)
		dead := insertOutboxMessage(t, pool, outboxRow{Suffix: 85, Status: domain.StatusDeadLetter, AttemptCount: 5, LastError: "max_attempts_exhausted"})
		messages, err := repo.ListDeadLetters(ctx, ports.DeadLetterQuery{
			TenantID: meshaTenant,
			Status:   domain.StatusDeadLetter,
			Limit:    10,
		})
		if err != nil {
			t.Fatalf("ListDeadLetters: %v", err)
		}
		found := false
		for _, message := range messages {
			if message.OutboxID == dead.OutboxID {
				found = true
				if message.Status != domain.StatusDeadLetter || message.AttemptCount != 5 {
					t.Fatalf("listed dead letter=%#v", message)
				}
			}
		}
		if !found {
			t.Fatalf("dead letter %s not listed in %#v", dead.OutboxID, messages)
		}
		replayed, err := repo.ReplayDeadLetters(ctx, ports.ReplayDeadLettersParams{
			TenantID:  meshaTenant,
			OutboxIDs: []string{dead.OutboxID},
			Reason:    "operator verified payload",
			Now:       outboxTestNow,
		})
		if err != nil {
			t.Fatalf("ReplayDeadLetters: %v", err)
		}
		if replayed.Updated != 1 || replayed.Replayed {
			t.Fatalf("replayed=%#v want updated 1 fresh", replayed)
		}
		state := queryOutboxState(t, pool, dead.OutboxID)
		if state.Status != domain.StatusPending || state.AttemptCount != 0 || !state.NextAttemptAt.Valid || state.PublishedAt.Valid {
			t.Fatalf("replayed state=%#v", state)
		}
		if state.ReplayCount != 1 {
			t.Fatalf("replayed replay_count=%d want 1", state.ReplayCount)
		}
		if !strings.Contains(state.LastError, "replayed_from_dlq") {
			t.Fatalf("replayed last_error=%q", state.LastError)
		}
		if _, err := pool.Exec(ctx, `UPDATE outbox_messages SET status='dead_letter', replay_count=3 WHERE outbox_id=$1`, dead.OutboxID); err != nil {
			t.Fatalf("set replay cap: %v", err)
		}
		replayed, err = repo.ReplayDeadLetters(ctx, ports.ReplayDeadLettersParams{
			TenantID:  meshaTenant,
			OutboxIDs: []string{dead.OutboxID},
			Reason:    "operator tried fourth replay",
			Now:       outboxTestNow.Add(time.Minute),
		})
		if err != nil {
			t.Fatalf("ReplayDeadLetters capped: %v", err)
		}
		if replayed.Updated != 0 {
			t.Fatalf("capped replayed=%#v want updated 0", replayed)
		}
	})

	t.Run("dlq action audit is idempotent on repair retry", func(t *testing.T) {
		repo := NewRepository(pool, 5*time.Second)
		dead := insertOutboxMessage(t, pool, outboxRow{Suffix: 86, Status: domain.StatusDeadLetter, AttemptCount: 5, LastError: "max_attempts_exhausted"})
		reason := "operator verified payload after schema fix"
		ids := []string{dead.OutboxID}
		key := "dlq-replay-audit-test-0001"
		hash := dlqRepoActionHash("replay", ids, reason)
		replayed, err := repo.ReplayDeadLetters(ctx, ports.ReplayDeadLettersParams{
			TenantID:       meshaTenant,
			OutboxIDs:      ids,
			Reason:         reason,
			Now:            outboxTestNow,
			IdempotencyKey: key,
			RequestHash:    hash,
		})
		if err != nil {
			t.Fatalf("ReplayDeadLetters: %v", err)
		}
		if err := repo.RecordDLQActionAudit(ctx, ports.RecordDLQActionAuditParams{
			TenantID:       meshaTenant,
			Action:         "replay",
			OutboxIDs:      ids,
			Reason:         reason,
			Updated:        replayed.Updated,
			ActorID:        meshaParty,
			TraceID:        "trace-dlq-audit-test",
			IdempotencyKey: key,
			RequestHash:    hash,
		}); err != nil {
			t.Fatalf("RecordDLQActionAudit first: %v", err)
		}
		replayed, err = repo.ReplayDeadLetters(ctx, ports.ReplayDeadLettersParams{
			TenantID:       meshaTenant,
			OutboxIDs:      ids,
			Reason:         reason,
			Now:            outboxTestNow.Add(time.Minute),
			IdempotencyKey: key,
			RequestHash:    hash,
		})
		if err != nil {
			t.Fatalf("ReplayDeadLetters retry: %v", err)
		}
		if !replayed.Replayed {
			t.Fatalf("retry result=%#v want stored replay", replayed)
		}
		if err := repo.RecordDLQActionAudit(ctx, ports.RecordDLQActionAuditParams{
			TenantID:       meshaTenant,
			Action:         "replay",
			OutboxIDs:      ids,
			Reason:         reason,
			Updated:        replayed.Updated,
			ActorID:        meshaParty,
			TraceID:        "trace-dlq-audit-test",
			IdempotencyKey: key,
			RequestHash:    hash,
		}); err != nil {
			t.Fatalf("RecordDLQActionAudit retry: %v", err)
		}
		if got := countDLQAuditRows(t, pool, dead.OutboxID, key, hash); got != 1 {
			t.Fatalf("audit rows=%d want 1", got)
		}
	})

	t.Run("batch limit respected", func(t *testing.T) {
		insertOutboxMessage(t, pool, outboxRow{Suffix: 90, Status: domain.StatusPending})
		insertOutboxMessage(t, pool, outboxRow{Suffix: 91, Status: domain.StatusPending})
		insertOutboxMessage(t, pool, outboxRow{Suffix: 92, Status: domain.StatusPending})
		publisher := &fakePublisher{}
		service := newRelayService(t, pool, publisher, outboxapp.Config{Limit: 2, MaxAttempts: 5, Now: fixedNow})

		result, err := service.RunOnce(ctx)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if result.ClaimedCount != 2 || publisher.CallCount() != 2 {
			t.Fatalf("limit not respected: result=%#v calls=%#v", result, publisher.Calls())
		}
	})
}

func countDLQAuditRows(t *testing.T, pool *pgxpool.Pool, outboxID, key, hash string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `
SELECT count(*)::int
FROM audit_log
WHERE tenant_id = $1
  AND action = 'operations.dlq.replay'
  AND resource_type = 'outbox_message'
  AND resource_id = $2
  AND metadata->>'idempotency_key' = $3
  AND metadata->>'request_hash' = $4`, meshaTenant, outboxID, key, hash).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

type outboxRow struct {
	Suffix        int
	Status        string
	AttemptCount  int
	NextAttemptAt *time.Time
	LastError     string
	UpdatedAt     time.Time
	Payload       json.RawMessage
}

type insertedOutbox struct {
	OutboxID string
	EventID  string
}

func insertOutboxMessage(t *testing.T, pool *pgxpool.Pool, row outboxRow) insertedOutbox {
	t.Helper()
	if row.Status == "" {
		row.Status = domain.StatusPending
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = outboxTestNow
	}
	outboxID := testUUID("10000000", row.Suffix)
	eventID := testUUID("20000000", row.Suffix)
	traceID := fmt.Sprintf("trace-outbox-%03d", row.Suffix)
	payload := row.Payload
	if len(payload) == 0 {
		payload = validEnvelopePayload(t, eventID, traceID)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO goat_identity_events (
  identity_event_id, tenant_id, goat_id, event_type, event_version,
  occurred_at, recorded_at, payload, idempotency_key
) VALUES (
  $1, $2, $3, 'goat.created', 1,
  $4, $4, '{}'::jsonb, $5
) ON CONFLICT DO NOTHING`, eventID, meshaTenant, outboxGoatID, outboxTestNow, "outbox-test-event-"+eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO outbox_messages (
  outbox_id, tenant_id, event_id, event_type, schema_version,
  aggregate_type, aggregate_id, topic, payload, headers, idempotency_key,
  trace_id, status, attempt_count, next_attempt_at, last_error, created_at, updated_at
) VALUES (
  $1, $2, $3, 'goat.created', '1.0.0',
  'goat', $4, 'identity.events', $5::jsonb, '{"source":"outbox-test"}'::jsonb, $6,
  $7, $8, $9, $10, NULLIF($11, ''), $12, $13
)`, outboxID, meshaTenant, eventID, outboxGoatID, string(payload), "outbox-test-"+eventID, traceID, row.Status, row.AttemptCount, row.NextAttemptAt, row.LastError, outboxTestNow.Add(time.Duration(row.Suffix)*time.Second), row.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	return insertedOutbox{OutboxID: outboxID, EventID: eventID}
}

func validEnvelopePayload(t *testing.T, eventID string, traceID string) json.RawMessage {
	t.Helper()
	envelope := map[string]any{
		"event_id":        eventID,
		"event_type":      "goat.created",
		"schema_version":  "1.0.0",
		"schema_ref":      "contracts/jsonschema/domain-event-envelope.schema.json",
		"aggregate_type":  "goat",
		"aggregate_id":    outboxGoatID,
		"occurred_at":     outboxTestNow.Format(time.RFC3339),
		"recorded_at":     outboxTestNow.Format(time.RFC3339),
		"producer":        map[string]any{"service": "goatos-test", "module": "outbox"},
		"idempotency_key": "outbox-test-" + eventID,
		"actor":           map[string]any{"actor_type": "system_rule", "actor_id": nil, "actor_ref": "outbox-test"},
		"subject_type":    "goat",
		"subject_id":      outboxGoatID,
		"visibility_scope": map[string]any{
			"tenant_id": meshaTenant,
		},
		"evidence_refs": []any{},
		"payload":       map[string]any{"source": "synthetic_outbox_test"},
		"trace_id":      traceID,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

type outboxState struct {
	Status        string
	AttemptCount  int
	ReplayCount   int
	LastError     string
	NextAttemptAt pgtype.Timestamptz
	PublishedAt   pgtype.Timestamptz
}

func queryOutboxState(t *testing.T, pool *pgxpool.Pool, outboxID string) outboxState {
	t.Helper()
	var state outboxState
	if err := pool.QueryRow(context.Background(), `
SELECT status, attempt_count, replay_count, COALESCE(last_error, ''), next_attempt_at, published_at
FROM outbox_messages
WHERE outbox_id = $1`, outboxID).Scan(&state.Status, &state.AttemptCount, &state.ReplayCount, &state.LastError, &state.NextAttemptAt, &state.PublishedAt); err != nil {
		t.Fatal(err)
	}
	return state
}

func assertOutboxState(t *testing.T, pool *pgxpool.Pool, outboxID string, wantStatus string, wantAttempt int, wantError string, wantNext bool, wantPublished bool) {
	t.Helper()
	state := queryOutboxState(t, pool, outboxID)
	if state.Status != wantStatus || state.AttemptCount != wantAttempt {
		t.Fatalf("%s state=%#v want status=%s attempt=%d", outboxID, state, wantStatus, wantAttempt)
	}
	if wantError != "" && state.LastError != wantError {
		t.Fatalf("%s last_error=%q want %q", outboxID, state.LastError, wantError)
	}
	if wantError == "" && state.LastError != "" {
		t.Fatalf("%s unexpected last_error=%q", outboxID, state.LastError)
	}
	if state.NextAttemptAt.Valid != wantNext {
		t.Fatalf("%s next_attempt_at valid=%v want %v", outboxID, state.NextAttemptAt.Valid, wantNext)
	}
	if state.PublishedAt.Valid != wantPublished {
		t.Fatalf("%s published_at valid=%v want %v", outboxID, state.PublishedAt.Valid, wantPublished)
	}
}

func assertNoRawErrorLeak(t *testing.T, lastError string) {
	t.Helper()
	for _, forbidden := range []string{"9900000000000000000000000000001", "SYNTHETIC_PRIVATE_TAG"} {
		if strings.Contains(lastError, forbidden) {
			t.Fatalf("last_error leaked raw payload/source value %q in %q", forbidden, lastError)
		}
	}
}

func newRelayService(t *testing.T, pool *pgxpool.Pool, publisher ports.Publisher, config outboxapp.Config) *outboxapp.Service {
	t.Helper()
	schemaPath := filepath.Join(repoRoot(t), "contracts", "jsonschema", "domain-event-envelope.schema.json")
	validator, err := outboxapp.NewEnvelopeValidator(schemaPath)
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}
	return outboxapp.NewService(NewRepository(pool, 5*time.Second), publisher, validator, config)
}

func fixedNow() time.Time {
	return outboxTestNow
}

type fakePublisher struct {
	mu            sync.Mutex
	calls         []ports.PublishMessage
	fail          map[string]error
	panicRows     map[string]bool
	blockOutboxID string
	started       chan struct{}
	release       chan struct{}
}

func (p *fakePublisher) Publish(ctx context.Context, message ports.PublishMessage) error {
	p.mu.Lock()
	p.calls = append(p.calls, message)
	shouldPanic := p.panicRows[message.OutboxID]
	err := p.fail[message.OutboxID]
	shouldBlock := p.blockOutboxID == message.OutboxID
	started := p.started
	release := p.release
	p.mu.Unlock()

	if shouldBlock {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if shouldPanic {
		panic("synthetic outbox publisher panic with hidden payload")
	}
	if err != nil {
		return ports.RetryablePublishError(err)
	}
	return nil
}

func (p *fakePublisher) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func (p *fakePublisher) Called(outboxID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, call := range p.calls {
		if call.OutboxID == outboxID {
			return true
		}
	}
	return false
}

func (p *fakePublisher) Calls() []ports.PublishMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ports.PublishMessage, len(p.calls))
	copy(out, p.calls)
	return out
}

func startOutboxDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container := fmt.Sprintf("goatos-outbox-test-%d", time.Now().UnixNano())
	image := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if image == "" {
		image = defaultPostgresImage
	}
	run(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", image)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	})
	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos").Run() == nil {
			applyMigrations(t, container)
			return openPool(t, ctx, container)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres container did not become ready:\n%s", runOutput(t, "docker", "logs", container))
	return nil
}

func seedOutboxGoat(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO goats (
  goat_id, tenant_id, species, breed, sex, lifecycle_status,
  custodian_party_id
) VALUES (
  $1, $2, 'goat', 'Boer', 'female', 'alive', $3
) ON CONFLICT DO NOTHING`, outboxGoatID, meshaTenant, meshaParty); err != nil {
		t.Fatal(err)
	}
}

func testUUID(prefix string, suffix int) string {
	return fmt.Sprintf("%s-0000-4000-8000-%012d", prefix, suffix)
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func applyMigrations(t *testing.T, container string) {
	t.Helper()
	root := repoRoot(t)
	migrations, err := filepath.Glob(filepath.Join(root, "backend", "migrations", "postgres", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(migrations)
	for _, migration := range migrations {
		sqlBytes, err := os.ReadFile(migration)
		if err != nil {
			t.Fatal(err)
		}
		psql(t, container, extractGooseUp(string(sqlBytes)))
	}
}

func openPool(t *testing.T, ctx context.Context, container string) *pgxpool.Pool {
	t.Helper()
	out := runOutput(t, "docker", "port", container, "5432/tcp")
	parts := strings.Split(strings.TrimSpace(out), ":")
	port := parts[len(parts)-1]
	url := "postgres://postgres:goatos@127.0.0.1:" + port + "/goatos?sslmode=disable"
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	return pool
}

func psql(t *testing.T, container, sqlText string) {
	t.Helper()
	cmd := exec.Command("docker", "exec", "-i", container, "psql", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos")
	cmd.Stdin = strings.NewReader(sqlText)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("psql failed: %v\n%s\nSQL:\n%s", err, out, sqlText)
	}
}

func extractGooseUp(sqlText string) string {
	var out []string
	inUp := false
	for _, line := range strings.Split(sqlText, "\n") {
		switch {
		case strings.HasPrefix(line, "-- +goose Up"):
			inUp = true
			continue
		case strings.HasPrefix(line, "-- +goose Down"):
			inUp = false
		}
		if inUp {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir)
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("repo root not found")
		}
		dir = next
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func runOutput(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
	return string(out)
}
