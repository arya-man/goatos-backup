package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/outbox/domain"
	"github.com/vgoats/goatos/backend/internal/outbox/ports"
)

// A write that dies must be VISIBLE.
//
// Three outbox paths are terminal -- the event will never be delivered by any
// later attempt:
//
//	invalid_event_envelope   -> status 'failed'      (attempt 1, never retried)
//	publish_permanent_failure-> status 'failed'      (never retried)
//	max_attempts_exhausted   -> status 'dead_letter' (retries exhausted)
//
// Before this test, the first of those emitted NOTHING: no metric, no log --
// only a status column somebody had to think to query. That is exactly how 12
// permanently undeliverable weighing events survived a whole real device run
// unnoticed. A dead-letter (or a 'failed' row) that nothing reads is the same as
// a drop.
//
// This asserts the OBSERVABLE signal, not the retry policy: nothing here makes
// the outbox retry longer, and no idempotency key is touched.
func TestTerminalOutboxStatesAreAnnounced(t *testing.T) {
	for _, tc := range []struct {
		name       string
		payload    json.RawMessage
		publishErr error
		attempt    int
		wantReason string
	}{
		{
			name: "invalid_envelope",
			// The tonight-shaped failure: a structurally valid row whose envelope the
			// relay's own validator rejects. Marked failed on attempt 1, never retried.
			payload:    json.RawMessage(`{"event_type":"goat.created"}`),
			attempt:    1,
			wantReason: "invalid_event_envelope",
		},
		{
			name:       "permanent_publish_failure",
			publishErr: ports.PermanentPublishError(errors.New("topic does not exist")),
			attempt:    1,
			wantReason: "publish_permanent_failure",
		},
		{
			name: "attempts_exhausted",
			// A retryable failure on the last allowed attempt: the budget is spent, so
			// this is the dead-letter terminal, not another retry.
			publishErr: errors.New("transient: connection reset"),
			attempt:    5,
			wantReason: "max_attempts_exhausted",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := tc.payload
			if payload == nil {
				payload = validServiceEnvelope(t, 1)
			}
			message := serviceTestMessage(t, 1, payload)
			message.AttemptCount = tc.attempt

			logs := &bytes.Buffer{}
			log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
			repo := &serviceFakeRepo{messages: []domain.Message{message}}
			service := NewService(repo, &terminalFakePublisher{err: tc.publishErr}, serviceTestValidator(t), Config{
				Limit:       10,
				MaxAttempts: 5,
				Now:         func() time.Time { return serviceTestNow },
			}, log)

			if _, err := service.RunOnce(context.Background()); err != nil {
				t.Fatalf("RunOnce: %v", err)
			}

			line := logs.String()
			if !strings.Contains(line, "outbox_message_undeliverable") {
				t.Fatalf("terminal state %q produced no undeliverable warning — a dead event nothing announces is a silent drop.\nlogs: %s", tc.wantReason, line)
			}
			if !strings.Contains(line, tc.wantReason) {
				t.Errorf("undeliverable warning does not carry reason %q\nlogs: %s", tc.wantReason, line)
			}
			// The row has to be identifiable from the log alone, or "visible" is a
			// claim nobody can act on.
			for _, want := range []string{message.OutboxID, message.EventType, message.TenantID} {
				if !strings.Contains(line, want) {
					t.Errorf("undeliverable warning omits %q\nlogs: %s", want, line)
				}
			}
		})
	}
}

type terminalFakePublisher struct{ err error }

func (p *terminalFakePublisher) Publish(context.Context, ports.PublishMessage) error { return p.err }
