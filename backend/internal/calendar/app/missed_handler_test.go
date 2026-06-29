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
	if repo.refreshCalls != 1 || repo.lastRefresh.TenantID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("refresh calls=%d input=%#v, want one tenant refresh before escalation", repo.refreshCalls, repo.lastRefresh)
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
	calls        int
	refreshCalls int
	last         ports.SweepEscalations
	lastRefresh  ports.RefreshVaccinationProjection
}

func (r *missedHandlerRepo) RefreshVaccinationProjection(_ context.Context, in ports.RefreshVaccinationProjection) (int, error) {
	r.refreshCalls++
	r.lastRefresh = in
	return 1, nil
}

func (r *missedHandlerRepo) SweepEscalations(_ context.Context, in ports.SweepEscalations) (int, error) {
	r.calls++
	r.last = in
	return 1, nil
}
