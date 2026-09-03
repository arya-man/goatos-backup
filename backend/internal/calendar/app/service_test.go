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
	svc := NewService(&fakeRepo{})
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
	svc := NewService(&fakeRepo{})
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
	if len(resp.Presentation.ViewTabs) != 2 {
		t.Fatalf("view tabs = %#v, want week/month tabs", resp.Presentation.ViewTabs)
	}
	if resp.Presentation.PageSubtitle != "Vaccination due work and accepted completion history by time, owner lane, park, pen, and date." {
		t.Fatalf("page subtitle = %q", resp.Presentation.PageSubtitle)
	}
	if resp.Presentation.ViewTabs[0].Key != "week" || resp.Presentation.ViewTabs[1].Key != "month" {
		t.Fatalf("view tabs = %#v, want week/month tabs", resp.Presentation.ViewTabs)
	}
	foundHistoryLabel := false
	for _, item := range resp.Presentation.EventTypes {
		if item.Key == domain.EventVaccinationHistory && item.Label == "Completed vaccination history" {
			foundHistoryLabel = true
			break
		}
	}
	if !foundHistoryLabel {
		t.Fatalf("event types = %#v, want completed vaccination history label published", resp.Presentation.EventTypes)
	}
	if resp.Presentation.Month.CellNote != "Each cell shows that day's due-work and completion markers. Tap an event for its rich detail." {
		t.Fatalf("month cell note = %q", resp.Presentation.Month.CellNote)
	}
}

func TestServiceAllowsMissedStatusFilter(t *testing.T) {
	svc := NewService(&fakeRepo{})
	status := domain.StatusMissed
	_, err := svc.ListEvents(context.Background(), domain.Query{
		TenantID: "00000000-0000-4000-8000-000000000001",
		Status:   &status,
		DateFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ListEvents missed status error = %v", err)
	}
}

func TestServiceAddsBoundedFilterChromeToRepositoryOptions(t *testing.T) {
	repo := &fakeRepo{listResp: domain.CalendarEventListResponse{
		Source: domain.SourceAPI,
		FilterOptions: &domain.CalendarFilterOptions{
			Parks: []domain.CalendarFilterOption{{Value: "70000000-0000-4000-8000-000000000001", Label: "CPT"}},
		},
	}}
	svc := NewService(repo)
	svc.now = func() time.Time { return time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC) }
	resp, err := svc.ListEvents(context.Background(), domain.Query{
		TenantID:             "00000000-0000-4000-8000-000000000001",
		DateFrom:             time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		DateTo:               time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
		IncludeFilterOptions: true,
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if resp.FilterOptions == nil || len(resp.FilterOptions.Parks) != 1 {
		t.Fatalf("filter options = %#v, want repository-scoped park", resp.FilterOptions)
	}
	if len(resp.FilterOptions.Statuses) != 13 || len(resp.FilterOptions.Months) != 12 || len(resp.FilterOptions.Years) != 3 {
		t.Fatalf("filter chrome = %#v, want bounded status/month/year options", resp.FilterOptions)
	}
	if got := resp.FilterOptions.Years[1].Key; got != "2026" {
		t.Fatalf("middle year = %q, want 2026", got)
	}
}

func TestServiceRejectsUnsupportedOwnerKey(t *testing.T) {
	svc := NewService(&fakeRepo{})
	_, err := svc.ListEvents(context.Background(), domain.Query{
		TenantID: "00000000-0000-4000-8000-000000000001",
		OwnerKey: "not_a_real_owner",
		DateFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC),
	})
	var appErr Error
	if err == nil || !errors.As(err, &appErr) || appErr.Code != "invalid_owner_key" {
		t.Fatalf("err = %v, want invalid_owner_key", err)
	}
}

func TestServiceMapsIdempotencyConflictToConflict(t *testing.T) {
	svc := NewService(&fakeRepo{nudgeErr: ports.ErrIdempotencyConflict})
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

func TestServiceMapsIdempotencyInProgressToConflict(t *testing.T) {
	svc := NewService(&fakeRepo{nudgeErr: ports.ErrIdempotencyInProgress})
	_, err := svc.SendNudge(context.Background(), ports.SendNudge{
		TenantID:       "00000000-0000-4000-8000-000000000001",
		EventID:        "obligation:86000000-0000-4000-8000-000000001001",
		ActorID:        "86000000-0000-4000-8000-000000009999",
		IdempotencyKey: "same-key",
	})
	var appErr Error
	if !errors.As(err, &appErr) || appErr.Code != "idempotency_in_progress" {
		t.Fatalf("err = %v, want idempotency_in_progress", err)
	}
}

func TestServiceRejectsInvalidNudgeActionBody(t *testing.T) {
	svc := NewService(&fakeRepo{})
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

func TestServiceRejectsInvalidEscalationActions(t *testing.T) {
	svc := NewService(&fakeRepo{})
	_, err := svc.AcknowledgeEscalation(context.Background(), ports.AcknowledgeEscalation{
		TenantID:       "00000000-0000-4000-8000-000000000001",
		EventID:        "obligation:86000000-0000-4000-8000-000000001001",
		ActorID:        "86000000-0000-4000-8000-000000009999",
		IdempotencyKey: "ack-test-key",
		Reason:         string(make([]byte, 1001)),
	})
	var appErr Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_reason" {
		t.Fatalf("ack err = %v, want invalid_reason", err)
	}

	_, err = svc.ResolveEscalation(context.Background(), ports.ResolveEscalation{
		TenantID:       "00000000-0000-4000-8000-000000000001",
		EventID:        "obligation:86000000-0000-4000-8000-000000001001",
		ActorID:        "86000000-0000-4000-8000-000000009999",
		IdempotencyKey: "resolve-key",
	})
	if !errors.As(err, &appErr) || appErr.Code != "invalid_reason" {
		t.Fatalf("resolve err = %v, want invalid_reason", err)
	}
}

func TestServiceKeepsBoundedLimitAndCursorTruth(t *testing.T) {
	next := "cursor-2"
	repo := &fakeRepo{
		listResp: domain.CalendarEventListResponse{
			Source:     domain.SourceAPI,
			NextCursor: &next,
		},
	}
	svc := NewService(repo)
	resp, err := svc.ListEvents(context.Background(), domain.Query{
		TenantID: "00000000-0000-4000-8000-000000000001",
		OwnerKey: domain.OwnerPC,
		Limit:    999,
		DateFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DateTo:   time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if repo.lastListQuery.Limit != maxListLimit {
		t.Fatalf("repo query limit = %d, want bounded max %d with no raw-rollup bump", repo.lastListQuery.Limit, maxListLimit)
	}
	if resp.NextCursor == nil || *resp.NextCursor != next {
		t.Fatalf("next cursor = %#v, want preserved pagination cursor %q", resp.NextCursor, next)
	}
}

type fakeRepo struct {
	nudgeErr      error
	listErr       error
	listResp      domain.CalendarEventListResponse
	lastListQuery domain.Query
}

func (f *fakeRepo) ListEvents(_ context.Context, q domain.Query) (domain.CalendarEventListResponse, error) {
	f.lastListQuery = q
	if f.listErr != nil {
		return domain.CalendarEventListResponse{}, f.listErr
	}
	if f.listResp.Source != "" || f.listResp.NextCursor != nil || len(f.listResp.Items) > 0 || f.listResp.FilterOptions != nil {
		return f.listResp, nil
	}
	return domain.CalendarEventListResponse{Source: domain.SourceAPI}, nil
}

func TestServiceMapsStaleCalendarProjectionToUnavailableError(t *testing.T) {
	svc := NewService(&fakeRepo{listErr: ports.ErrProjectionStale})
	_, err := svc.ListEvents(context.Background(), domain.Query{
		TenantID: "00000000-0000-4000-8000-000000000001",
		DateFrom: time.Now(), DateTo: time.Now().Add(24 * time.Hour),
	})
	var appErr Error
	if !errors.As(err, &appErr) || appErr.Code != "projection_stale" {
		t.Fatalf("err=%v, want projection_stale", err)
	}
}

func (f *fakeRepo) GetEventDetail(context.Context, domain.EventQuery) (domain.CalendarEventDetail, error) {
	return domain.CalendarEventDetail{}, nil
}

func (f *fakeRepo) ListDriveTargets(context.Context, domain.DriveTargetQuery) (domain.CalendarDriveTargetListResponse, error) {
	return domain.CalendarDriveTargetListResponse{Source: domain.SourceAPI}, nil
}

func (f *fakeRepo) History(context.Context, domain.HistoryQuery) (domain.CalendarHistoryResponse, error) {
	return domain.CalendarHistoryResponse{}, nil
}

func (f *fakeRepo) SendNudge(context.Context, ports.SendNudge) (domain.CalendarActionResponse, error) {
	return domain.CalendarActionResponse{}, f.nudgeErr
}

func (f *fakeRepo) Snooze(context.Context, ports.Snooze) (domain.CalendarActionResponse, error) {
	return domain.CalendarActionResponse{}, nil
}

func (f *fakeRepo) AcknowledgeEscalation(context.Context, ports.AcknowledgeEscalation) (domain.CalendarActionResponse, error) {
	return domain.CalendarActionResponse{}, nil
}

func (f *fakeRepo) ResolveEscalation(context.Context, ports.ResolveEscalation) (domain.CalendarActionResponse, error) {
	return domain.CalendarActionResponse{}, nil
}

func (f *fakeRepo) SweepDueReminders(context.Context, string, int) (int, error) {
	return 0, nil
}

func (f *fakeRepo) SweepEscalations(context.Context, ports.SweepEscalations) (int, error) {
	return 0, nil
}

func (f *fakeRepo) ResolveVaccinationCompletionContext(context.Context, string, string) (ports.VaccinationCompletionContext, error) {
	return ports.VaccinationCompletionContext{}, nil
}

func (f *fakeRepo) QueueRoleNotifications(context.Context, ports.QueueRoleNotifications) (int, error) {
	return 0, nil
}

func (f *fakeRepo) SweepReminderCadence(context.Context, ports.ReminderCadenceQuery) ([]ports.ReminderCadenceFire, error) {
	return nil, nil
}

func (f *fakeRepo) SweepReminderCadencePage(context.Context, ports.ReminderCadenceQuery) ([]ports.ReminderCadenceFire, ports.ReminderCadenceSweepCursor, error) {
	return nil, ports.ReminderCadenceSweepCursor{}, nil
}

func (f *fakeRepo) QueueReminderCadenceBatch(context.Context, ports.QueueReminderCadenceBatch) (int, error) {
	return 0, nil
}

func (f *fakeRepo) ReconcileEventReferences(context.Context, string) ([]ports.OrphanedCalendarEventReference, error) {
	return nil, nil
}

func (f *fakeRepo) ResolveMissedObligationContext(context.Context, string, string) (ports.MissedObligationContext, error) {
	return ports.MissedObligationContext{}, nil
}

func (f *fakeRepo) ReconcileEventReferencesPage(context.Context, string, string, string, int) ([]ports.OrphanedCalendarEventReference, error) {
	return nil, nil
}

func (f *fakeRepo) LoadReconcilerCursor(context.Context, string) (string, string, error) {
	return "", "", nil
}

func (f *fakeRepo) SaveReconcilerCursor(context.Context, string, string, string, time.Time) error {
	return nil
}

func (f *fakeRepo) LoadReminderCadenceCursor(context.Context, string) (time.Time, string, error) {
	return time.Time{}, "", nil
}

func (f *fakeRepo) SaveReminderCadenceCursor(context.Context, string, time.Time, string, time.Time) error {
	return nil
}
