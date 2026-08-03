package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/outbox/domain"
	"github.com/vgoats/goatos/backend/internal/outbox/ports"
)

// W-23 (backend half): a domain event the relay abandons must reach the SERVICE LOG, not just a
// metric counter. A counter is only visible to whoever happens to be watching a dashboard; the
// log is what a human actually reads when investigating "the write never showed up". These
// assertions are on emitted log records, never on the absence of an error.

type capturingHandler struct {
	records []slog.Record
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

func (h *capturingHandler) find(msg string) (slog.Record, bool) {
	for _, r := range h.records {
		if r.Message == msg {
			return r, true
		}
	}
	return slog.Record{}, false
}

func recordAttrs(r slog.Record) map[string]string {
	attrs := map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	return attrs
}

type deadLetterFakeRepo struct {
	serviceFakeRepo
	claimDeadLetters int
	deadLettered     []string
	markedFailed     []string
}

func (r *deadLetterFakeRepo) ClaimPending(ctx context.Context, p ports.ClaimParams) (*ports.ClaimResult, error) {
	result, err := r.serviceFakeRepo.ClaimPending(ctx, p)
	if err != nil {
		return nil, err
	}
	result.DeadLetterCount = r.claimDeadLetters
	r.claimDeadLetters = 0
	return result, nil
}

func (r *deadLetterFakeRepo) MarkDeadLetter(_ context.Context, id string, _ string, _ time.Time) error {
	r.deadLettered = append(r.deadLettered, id)
	return nil
}

func (r *deadLetterFakeRepo) MarkFailed(_ context.Context, id string, _ string, _ time.Time) error {
	r.markedFailed = append(r.markedFailed, id)
	return nil
}

type failingPublisher struct {
	err error
}

func (p *failingPublisher) Publish(context.Context, ports.PublishMessage) error { return p.err }

func visibilityService(t *testing.T, repo ports.Repository, publisher ports.Publisher) (*Service, *capturingHandler) {
	t.Helper()
	handler := &capturingHandler{}
	service := NewService(repo, publisher, serviceTestValidator(t), Config{
		Limit:       10,
		MaxAttempts: 5,
		Now:         func() time.Time { return serviceTestNow },
	}, slog.New(handler))
	return service, handler
}

func TestDeadLetteredMessageIsLogged(t *testing.T) {
	message := serviceTestMessage(t, 1, validServiceEnvelope(t, 1))
	message.AttemptCount = 5
	repo := &deadLetterFakeRepo{serviceFakeRepo: serviceFakeRepo{messages: []domain.Message{message}}}
	publisher := &failingPublisher{err: errors.New("broker unreachable")}
	service, logs := visibilityService(t, repo, publisher)

	result, err := service.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.DeadLetterCount != 1 || len(repo.deadLettered) != 1 {
		t.Fatalf("expected the message to be dead-lettered, got %#v", result)
	}

	record, ok := logs.find("outbox_message_dead_lettered")
	if !ok {
		t.Fatalf("a permanently abandoned event produced no log line; records=%v", logs.records)
	}
	if record.Level != slog.LevelError {
		t.Fatalf("dead-letter logged at %v, want ERROR — abandoned data is not routine", record.Level)
	}
	attrs := recordAttrs(record)
	if attrs["outbox_id"] != message.OutboxID {
		t.Fatalf("outbox_id=%q want %q — without it the row cannot be joined to an operator report", attrs["outbox_id"], message.OutboxID)
	}
	if attrs["reason"] != "max_attempts_exhausted" {
		t.Fatalf("reason=%q want max_attempts_exhausted", attrs["reason"])
	}
	if attrs["event_type"] == "" {
		t.Fatal("event_type missing — the log must say WHAT was abandoned")
	}
}

func TestPermanentPublishFailureIsLogged(t *testing.T) {
	message := serviceTestMessage(t, 2, validServiceEnvelope(t, 2))
	repo := &deadLetterFakeRepo{serviceFakeRepo: serviceFakeRepo{messages: []domain.Message{message}}}
	publisher := &failingPublisher{err: ports.PermanentPublishError(errors.New("schema rejected"))}
	service, logs := visibilityService(t, repo, publisher)

	if _, err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(repo.markedFailed) != 1 {
		t.Fatalf("expected a permanent failure mark, got %v", repo.markedFailed)
	}

	record, ok := logs.find("outbox_message_permanently_failed")
	if !ok {
		t.Fatalf("a permanently failed event produced no log line; records=%v", logs.records)
	}
	if record.Level != slog.LevelError {
		t.Fatalf("permanent failure logged at %v, want ERROR", record.Level)
	}
	if recordAttrs(record)["outbox_id"] != message.OutboxID {
		t.Fatal("permanent-failure log must name the row that died")
	}
}

func TestDeadLettersDuringClaimAreLogged(t *testing.T) {
	repo := &deadLetterFakeRepo{claimDeadLetters: 7}
	service, logs := visibilityService(t, repo, &serviceFakePublisher{})

	if _, err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	record, ok := logs.find("outbox_messages_dead_lettered_on_claim")
	if !ok {
		t.Fatalf("SQL-side dead-lettering produced no log line; records=%v", logs.records)
	}
	if attrs := recordAttrs(record); attrs["count"] != "7" {
		t.Fatalf("count=%q want 7", attrs["count"])
	}
}

func TestDeadLetterLogNeverCarriesTheEventPayload(t *testing.T) {
	message := serviceTestMessage(t, 3, validServiceEnvelope(t, 3))
	message.AttemptCount = 5
	repo := &deadLetterFakeRepo{serviceFakeRepo: serviceFakeRepo{messages: []domain.Message{message}}}
	service, logs := visibilityService(t, repo, &failingPublisher{err: errors.New("broker unreachable")})

	if _, err := service.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	record, _ := logs.find("outbox_message_dead_lettered")
	for key, value := range recordAttrs(record) {
		if strings.Contains(value, "visibility_scope") || strings.Contains(value, "idempotency_key") {
			t.Fatalf("attr %q leaked the event payload into the log: %q", key, value)
		}
	}
}
