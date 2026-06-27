package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
)

func TestServiceRejectsCalendarRangeOver45Days(t *testing.T) {
	svc := NewService(fakeRepo{})
	_, err := svc.ListEvents(context.Background(), domain.Query{
		TenantID: "00000000-0000-4000-8000-000000000001",
		DateFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
	})
	var appErr Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_date_range" {
		t.Fatalf("err = %v, want invalid_date_range", err)
	}
}

func TestServiceAddsBackendControlledPresentation(t *testing.T) {
	svc := NewService(fakeRepo{})
	resp, err := svc.ListEvents(context.Background(), domain.Query{
		TenantID: "00000000-0000-4000-8000-000000000001",
		OwnerKey: domain.OwnerInventory,
		DateFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if resp.Presentation.ActiveOwnerKey != domain.OwnerInventory {
		t.Fatalf("active owner = %q, want %q", resp.Presentation.ActiveOwnerKey, domain.OwnerInventory)
	}
	if len(resp.Presentation.OwnerTabs) != 4 || resp.Presentation.OwnerTabs[2].Label != "Inventory / Stock" || !resp.Presentation.OwnerTabs[2].Active {
		t.Fatalf("owner tabs = %#v, want backend-owned inventory tab active in configured order", resp.Presentation.OwnerTabs)
	}
	if len(resp.Presentation.WorkstreamTabs) == 0 || resp.Presentation.WorkstreamTabs[0].Label != "All Inventory / Stock" || !resp.Presentation.WorkstreamTabs[0].Active {
		t.Fatalf("workstream tabs = %#v, want inventory workstream copy", resp.Presentation.WorkstreamTabs)
	}
	if len(resp.Presentation.Rhythm.Days) < 2 || resp.Presentation.Rhythm.Days[1].Label != "FEFO" {
		t.Fatalf("rhythm days = %#v, want inventory rhythm labels", resp.Presentation.Rhythm.Days)
	}
}

func TestServiceMapsIdempotencyConflictToConflict(t *testing.T) {
	svc := NewService(fakeRepo{nudgeErr: ports.ErrIdempotencyConflict})
	_, err := svc.SendNudge(context.Background(), ports.SendNudge{
		TenantID:       "00000000-0000-4000-8000-000000000001",
		EventID:        "obligation:86000000-0000-4000-8000-000000001001",
		ActorID:        "86000000-0000-4000-8000-000000009999",
		IdempotencyKey: "same-key",
	})
	var appErr Error
	if !errors.As(err, &appErr) || appErr.Code != "idempotency_conflict" {
		t.Fatalf("err = %v, want idempotency_conflict", err)
	}
}

func TestServiceRejectsInvalidNudgeActionBody(t *testing.T) {
	svc := NewService(fakeRepo{})
	_, err := svc.SendNudge(context.Background(), ports.SendNudge{
		TenantID:       "00000000-0000-4000-8000-000000000001",
		EventID:        "obligation:86000000-0000-4000-8000-000000001001",
		ActorID:        "86000000-0000-4000-8000-000000009999",
		IdempotencyKey: "same-key",
		Channel:        "sms",
	})
	var appErr Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_channel" {
		t.Fatalf("err = %v, want invalid_channel", err)
	}

	_, err = svc.SendNudge(context.Background(), ports.SendNudge{
		TenantID:       "00000000-0000-4000-8000-000000000001",
		EventID:        "obligation:86000000-0000-4000-8000-000000001001",
		ActorID:        "86000000-0000-4000-8000-000000009999",
		IdempotencyKey: "same-key",
		Message:        string(make([]byte, 2001)),
	})
	if !errors.As(err, &appErr) || appErr.Code != "invalid_message" {
		t.Fatalf("err = %v, want invalid_message", err)
	}
}

type fakeRepo struct {
	nudgeErr error
}

func (f fakeRepo) ListEvents(context.Context, domain.Query) (domain.CalendarEventListResponse, error) {
	return domain.CalendarEventListResponse{Source: domain.SourceAPI}, nil
}

func (f fakeRepo) GetEventDetail(context.Context, domain.EventQuery) (domain.CalendarEventDetail, error) {
	return domain.CalendarEventDetail{}, nil
}

func (f fakeRepo) History(context.Context, domain.HistoryQuery) (domain.CalendarHistoryResponse, error) {
	return domain.CalendarHistoryResponse{}, nil
}

func (f fakeRepo) SendNudge(context.Context, ports.SendNudge) (domain.CalendarActionResponse, error) {
	return domain.CalendarActionResponse{}, f.nudgeErr
}

func (f fakeRepo) Snooze(context.Context, ports.Snooze) (domain.CalendarActionResponse, error) {
	return domain.CalendarActionResponse{}, nil
}

func (f fakeRepo) SweepDueReminders(context.Context, string, int) (int, error) {
	return 0, nil
}

func (f fakeRepo) SweepEscalations(context.Context, ports.SweepEscalations) (int, error) {
	return 0, nil
}

func (f fakeRepo) RefreshVaccinationProjection(context.Context, ports.RefreshVaccinationProjection) (int, error) {
	return 0, nil
}
