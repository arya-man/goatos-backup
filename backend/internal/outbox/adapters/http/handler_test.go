package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	outboxdomain "github.com/vgoats/goatos/backend/internal/outbox/domain"
	"github.com/vgoats/goatos/backend/internal/outbox/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

const (
	testTenantID = "00000000-0000-4000-8000-000000000001"
	testActorID  = "00000000-0000-4000-8000-000000000002"
	testOutboxID = "10000000-0000-4000-8000-000000000001"
)

func TestListDeadLetters(t *testing.T) {
	repo := &fakeDLQRepo{
		items: []outboxdomain.DeadLetterMessage{{
			OutboxID:       testOutboxID,
			TenantID:       testTenantID,
			EventID:        "20000000-0000-4000-8000-000000000001",
			EventType:      "goat.created",
			SchemaVersion:  "1.0.0",
			AggregateType:  "goat",
			AggregateID:    "30000000-0000-4000-8000-000000000001",
			Topic:          "identity.events",
			Status:         outboxdomain.StatusDeadLetter,
			AttemptCount:   5,
			ReplayCount:    0,
			LastError:      "max_attempts_exhausted",
			IdempotencyKey: "test-key",
			Headers:        json.RawMessage(`{}`),
			Payload:        json.RawMessage(`{"event_type":"goat.created"}`),
			CreatedAt:      time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC),
			UpdatedAt:      time.Date(2026, 6, 27, 10, 1, 0, 0, time.UTC),
		}},
	}
	handler := NewHandler(repo, nil)
	req := request(t, http.MethodGet, "/operations/dlq?status=failed&event_type=goat.created&topic=identity.events&limit=12", nil)
	rec := httptest.NewRecorder()

	handler.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if repo.query.TenantID != testTenantID || repo.query.Status != outboxdomain.StatusFailed || repo.query.EventType != "goat.created" || repo.query.Topic != "identity.events" || repo.query.Limit != 12 {
		t.Fatalf("query=%#v", repo.query)
	}
	var resp listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 || resp.Items[0].OutboxID != testOutboxID || resp.TraceID != "missing-trace" {
		t.Fatalf("resp=%#v", resp)
	}
}

func TestKernelHealth(t *testing.T) {
	repo := &fakeDLQRepo{health: outboxdomain.Health{Status: "degraded", DeadLetterCount: 2}}
	rec := httptest.NewRecorder()
	NewHandler(repo, nil).Health(rec, request(t, http.MethodGet, "/operations/kernel-health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if repo.healthTenant != testTenantID {
		t.Fatalf("tenant = %q", repo.healthTenant)
	}
	if !strings.Contains(rec.Body.String(), `"dead_letter_count":2`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestListRejectsInvalidStatus(t *testing.T) {
	handler := NewHandler(&fakeDLQRepo{}, nil)
	req := request(t, http.MethodGet, "/operations/dlq?status=published", nil)
	rec := httptest.NewRecorder()

	handler.List(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReplayDeadLettersRecordsAudit(t *testing.T) {
	repo := &fakeDLQRepo{replayUpdated: 1}
	handler := NewHandler(repo, nil)
	body := []byte(`{"outbox_ids":["` + testOutboxID + `"],"reason":"operator fixed poison payload"}`)
	req := request(t, http.MethodPost, "/operations/dlq/replay", body)
	req.Header.Set("Idempotency-Key", "dlq-replay-test-0001")
	rec := httptest.NewRecorder()

	handler.Replay(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if repo.replayParams.TenantID != testTenantID || len(repo.replayParams.OutboxIDs) != 1 || repo.replayParams.OutboxIDs[0] != testOutboxID {
		t.Fatalf("replay params=%#v", repo.replayParams)
	}
	if repo.replayParams.IdempotencyKey != "dlq-replay-test-0001" || repo.replayParams.RequestHash == "" {
		t.Fatalf("replay idempotency params=%#v", repo.replayParams)
	}
	if repo.auditParams.Action != "replay" || repo.auditParams.ActorID != testActorID || repo.auditParams.Updated != 1 {
		t.Fatalf("audit params=%#v", repo.auditParams)
	}
	if repo.auditParams.IdempotencyKey != "dlq-replay-test-0001" || repo.auditParams.RequestHash == "" {
		t.Fatalf("audit idempotency params=%#v", repo.auditParams)
	}
}

func TestReplayDeadLettersRecordsAuditOnStoredIdempotencyReplay(t *testing.T) {
	repo := &fakeDLQRepo{replayResult: ports.DLQActionResult{Updated: 1, Replayed: true}}
	handler := NewHandler(repo, nil)
	body := []byte(`{"outbox_ids":["` + testOutboxID + `"],"reason":"operator fixed poison payload"}`)
	req := request(t, http.MethodPost, "/operations/dlq/replay", body)
	req.Header.Set("Idempotency-Key", "dlq-replay-test-0001")
	rec := httptest.NewRecorder()

	handler.Replay(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if repo.auditCalls != 1 || repo.auditParams.Action != "replay" || repo.auditParams.Updated != 1 {
		t.Fatalf("audit retry params calls=%d params=%#v", repo.auditCalls, repo.auditParams)
	}
}

func TestDiscardDeadLettersRecordsAudit(t *testing.T) {
	repo := &fakeDLQRepo{discardUpdated: 1}
	handler := NewHandler(repo, nil)
	body := []byte(`{"outbox_ids":["` + testOutboxID + `"],"reason":"poison message obsolete after schema repair"}`)
	req := request(t, http.MethodPost, "/operations/dlq/discard", body)
	req.Header.Set("Idempotency-Key", "dlq-discard-test-0001")
	rec := httptest.NewRecorder()

	handler.Discard(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if repo.discardParams.TenantID != testTenantID || len(repo.discardParams.OutboxIDs) != 1 || repo.discardParams.OutboxIDs[0] != testOutboxID {
		t.Fatalf("discard params=%#v", repo.discardParams)
	}
	if repo.discardParams.IdempotencyKey != "dlq-discard-test-0001" || repo.discardParams.RequestHash == "" {
		t.Fatalf("discard idempotency params=%#v", repo.discardParams)
	}
	if repo.auditParams.Action != "discard" || repo.auditParams.IdempotencyKey != "dlq-discard-test-0001" {
		t.Fatalf("audit params=%#v", repo.auditParams)
	}
}

func TestReplayDeadLettersReturnsErrorWhenAuditRecoveryFails(t *testing.T) {
	repo := &fakeDLQRepo{replayResult: ports.DLQActionResult{Updated: 1, Replayed: true}, auditErr: errors.New("synthetic audit unavailable")}
	handler := NewHandler(repo, nil)
	body := []byte(`{"outbox_ids":["` + testOutboxID + `"],"reason":"operator fixed poison payload"}`)
	req := request(t, http.MethodPost, "/operations/dlq/replay", body)
	req.Header.Set("Idempotency-Key", "dlq-replay-test-0001")
	rec := httptest.NewRecorder()

	handler.Replay(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if repo.auditCalls != 1 {
		t.Fatalf("audit calls=%d want 1", repo.auditCalls)
	}
}

func TestActionRejectsMissingReason(t *testing.T) {
	handler := NewHandler(&fakeDLQRepo{}, nil)
	body := []byte(`{"outbox_ids":["` + testOutboxID + `"]}`)
	req := request(t, http.MethodPost, "/operations/dlq/replay", body)
	req.Header.Set("Idempotency-Key", "dlq-replay-test-0002")
	rec := httptest.NewRecorder()

	handler.Replay(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestActionRejectsMissingIdempotencyKey(t *testing.T) {
	handler := NewHandler(&fakeDLQRepo{}, nil)
	body := []byte(`{"outbox_ids":["` + testOutboxID + `"],"reason":"operator fixed poison payload"}`)
	req := request(t, http.MethodPost, "/operations/dlq/replay", body)
	rec := httptest.NewRecorder()

	handler.Replay(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "missing_idempotency_key") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func request(t *testing.T, method, target string, body []byte) *http.Request {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	ctx := req.Context()
	ctx = httpmiddleware.WithTenantID(ctx, testTenantID)
	ctx = httpmiddleware.WithActorID(ctx, testActorID)
	return req.WithContext(ctx)
}

type fakeDLQRepo struct {
	query          ports.DeadLetterQuery
	items          []outboxdomain.DeadLetterMessage
	replayParams   ports.ReplayDeadLettersParams
	replayUpdated  int64
	replayResult   ports.DLQActionResult
	discardParams  ports.DiscardDeadLettersParams
	discardUpdated int64
	discardResult  ports.DLQActionResult
	auditParams    ports.RecordDLQActionAuditParams
	auditCalls     int
	auditErr       error
	health         outboxdomain.Health
	healthTenant   string
}

func (r *fakeDLQRepo) ListDeadLetters(_ context.Context, q ports.DeadLetterQuery) ([]outboxdomain.DeadLetterMessage, error) {
	r.query = q
	return r.items, nil
}

func (r *fakeDLQRepo) ReplayDeadLetters(_ context.Context, params ports.ReplayDeadLettersParams) (ports.DLQActionResult, error) {
	r.replayParams = params
	if r.replayResult.Updated != 0 || r.replayResult.Replayed {
		return r.replayResult, nil
	}
	return ports.DLQActionResult{Updated: r.replayUpdated}, nil
}

func (r *fakeDLQRepo) Health(_ context.Context, tenantID string, _ time.Time) (outboxdomain.Health, error) {
	r.healthTenant = tenantID
	return r.health, nil
}

func (r *fakeDLQRepo) DiscardDeadLetters(_ context.Context, params ports.DiscardDeadLettersParams) (ports.DLQActionResult, error) {
	r.discardParams = params
	if r.discardResult.Updated != 0 || r.discardResult.Replayed {
		return r.discardResult, nil
	}
	return ports.DLQActionResult{Updated: r.discardUpdated}, nil
}

func (r *fakeDLQRepo) RecordDLQActionAudit(_ context.Context, params ports.RecordDLQActionAuditParams) error {
	r.auditCalls++
	r.auditParams = params
	return r.auditErr
}
