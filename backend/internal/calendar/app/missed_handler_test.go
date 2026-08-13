package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

func TestObligationMissedHandlerSweepsEscalations(t *testing.T) {
	repo := &missedHandlerRepo{}
	svc := NewService(repo)
	now := time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	handler := NewObligationMissedHandler(svc)

	err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type:     EventObligationMissed,
		TenantID: "00000000-0000-4000-8000-000000000001",
		Key:      "86000000-0000-4000-8000-000000000001",
		Payload:  []byte(`{"status":"missed","obligation_id":"86000000-0000-4000-8000-000000000001"}`),
	})
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("sweep calls=%d want 1", repo.calls)
	}
	if repo.last.TenantID != "00000000-0000-4000-8000-000000000001" ||
		repo.last.ObligationID != "86000000-0000-4000-8000-000000000001" ||
		repo.last.Limit != missedObligationEscalationLimit ||
		!repo.last.Now.Equal(now) {
		t.Fatalf("sweep input=%#v", repo.last)
	}
}

func TestObligationMissedHandlerIgnoresNonMissedPayload(t *testing.T) {
	repo := &missedHandlerRepo{}
	handler := NewObligationMissedHandler(NewService(repo))

	err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type:     EventObligationMissed,
		TenantID: "00000000-0000-4000-8000-000000000001",
		Key:      "86000000-0000-4000-8000-000000000001",
		Payload:  []byte(`{"status":"completed","obligation_id":"86000000-0000-4000-8000-000000000001"}`),
	})
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if repo.calls != 0 {
		t.Fatalf("sweep calls=%d want 0", repo.calls)
	}
}

type missedHandlerRepo struct {
	fakeRepo
	calls int
	last  ports.SweepEscalations
}

func (r *missedHandlerRepo) SweepEscalations(_ context.Context, in ports.SweepEscalations) (int, error) {
	r.calls++
	r.last = in
	return 1, nil
}

// recordingMissedNotifier captures the missed-work notification the handler must fire.
type recordingMissedNotifier struct {
	calls        int
	tenantID     string
	obligationID string
	err          error
}

func (n *recordingMissedNotifier) NotifyObligationMissed(_ context.Context, tenantID, obligationID string) error {
	n.calls++
	n.tenantID = tenantID
	n.obligationID = obligationID
	return n.err
}

// TestObligationMissedHandlerNotifiesTheMiss is the BLOCKER-3 guard. Opening an escalation row is a
// screen state, not a message: before this, a missed obligation -- the exact failure the operational
// kernel exists to catch -- was the one lifecycle state that told nobody anything. The handler must
// now also deliver the miss.
func TestObligationMissedHandlerNotifiesTheMiss(t *testing.T) {
	repo := &missedHandlerRepo{}
	svc := NewService(repo)
	svc.now = func() time.Time { return time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC) }
	notifier := &recordingMissedNotifier{}
	handler := NewObligationMissedHandler(svc).WithNotifier(notifier)

	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type:     EventObligationMissed,
		TenantID: "00000000-0000-4000-8000-000000000001",
		Key:      "86000000-0000-4000-8000-000000000001",
		Payload:  []byte(`{"status":"missed","obligation_id":"86000000-0000-4000-8000-000000000001"}`),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if notifier.calls != 1 {
		t.Fatalf("missed obligation produced %d notifications, want 1 — a missed obligation must tell someone", notifier.calls)
	}
	if notifier.tenantID != "00000000-0000-4000-8000-000000000001" ||
		notifier.obligationID != "86000000-0000-4000-8000-000000000001" {
		t.Fatalf("notified tenant/obligation = %q/%q", notifier.tenantID, notifier.obligationID)
	}
}

// TestObligationMissedHandlerDoesNotNotifyOnNonMissedPayload keeps the notification on the missed
// transition only.
func TestObligationMissedHandlerDoesNotNotifyOnNonMissedPayload(t *testing.T) {
	notifier := &recordingMissedNotifier{}
	handler := NewObligationMissedHandler(NewService(&missedHandlerRepo{})).WithNotifier(notifier)

	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type:     EventObligationMissed,
		TenantID: "00000000-0000-4000-8000-000000000001",
		Key:      "86000000-0000-4000-8000-000000000001",
		Payload:  []byte(`{"status":"completed","obligation_id":"86000000-0000-4000-8000-000000000001"}`),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if notifier.calls != 0 {
		t.Fatalf("non-missed payload notified %d times, want 0", notifier.calls)
	}
}
