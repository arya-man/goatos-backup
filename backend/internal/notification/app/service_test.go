package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/notification/domain"
	"github.com/vgoats/goatos/backend/internal/notification/ports"
)

func TestServiceRunOnceMarksSentAndFailed(t *testing.T) {
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	repo := &fakeRepo{requests: []domain.Request{
		{TenantID: testTenant, NotificationRequestID: "86000000-0000-4000-8000-000000000001", LeaseToken: "86000000-0000-4000-8000-000000000101", Channel: "local-stub", DeliveryAttempts: 1},
		{TenantID: testTenant, NotificationRequestID: "86000000-0000-4000-8000-000000000002", LeaseToken: "86000000-0000-4000-8000-000000000102", Channel: "slack", DeliveryAttempts: 1},
	}}
	gateway := &fakeGateway{failChannel: "slack"}
	service := NewService(repo, gateway, Config{Limit: 10, MaxAttempts: 5, BackoffBase: time.Minute, Now: func() time.Time { return now }}, nil)
	result, err := service.RunOnce(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.ClaimedCount != 2 || result.SentCount != 1 || result.FailedCount != 1 {
		t.Fatalf("result=%#v", result)
	}
	if len(repo.sent) != 1 || len(repo.failed) != 1 {
		t.Fatalf("sent=%#v failed=%#v", repo.sent, repo.failed)
	}
	if repo.failed[0].nextAttemptAt == nil || !repo.failed[0].nextAttemptAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("next attempt=%v want %v", repo.failed[0].nextAttemptAt, now.Add(time.Minute))
	}
}

func TestServiceLeavesFinalFailureWithoutNextAttempt(t *testing.T) {
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	repo := &fakeRepo{requests: []domain.Request{
		{TenantID: testTenant, NotificationRequestID: "86000000-0000-4000-8000-000000000003", LeaseToken: "86000000-0000-4000-8000-000000000103", Channel: "slack", DeliveryAttempts: 5},
	}}
	service := NewService(repo, &fakeGateway{failChannel: "slack"}, Config{MaxAttempts: 5, Now: func() time.Time { return now }}, nil)
	result, err := service.RunOnce(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FailedCount != 0 || result.ExhaustedCount != 1 || len(repo.failed) != 1 {
		t.Fatalf("result=%#v failed=%#v", result, repo.failed)
	}
	if repo.failed[0].nextAttemptAt != nil {
		t.Fatalf("final failure next attempt=%v, want nil", repo.failed[0].nextAttemptAt)
	}
}

func TestServiceDoesNotRetryPermanentChannelMisconfiguration(t *testing.T) {
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	repo := &fakeRepo{requests: []domain.Request{
		{TenantID: testTenant, NotificationRequestID: "86000000-0000-4000-8000-000000000004", LeaseToken: "86000000-0000-4000-8000-000000000104", Channel: "email", DeliveryAttempts: 1},
	}}
	service := NewService(repo, &fakeGateway{err: ports.ErrChannelNotConfigured}, Config{MaxAttempts: 5, Now: func() time.Time { return now }}, nil)
	result, err := service.RunOnce(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FailedCount != 0 || result.ExhaustedCount != 1 || len(repo.failed) != 1 {
		t.Fatalf("result=%#v failed=%#v", result, repo.failed)
	}
	if repo.failed[0].nextAttemptAt != nil {
		t.Fatalf("permanent config failure next attempt=%v, want nil", repo.failed[0].nextAttemptAt)
	}
}

// TestServiceRunOnceExportsBacklogAgeBestEffort proves the dispatcher stage
// queries the backlog age every run (so the 1-minute fast-lane SLO metric is
// exported after the near-term Cloud Tasks path + notification-dispatcher job
// were retired), and that a backlog-age read error is best-effort — it must
// never fail the dispatch run or drop the delivery that already happened.
func TestServiceRunOnceExportsBacklogAgeBestEffort(t *testing.T) {
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	repo := &fakeRepo{
		requests: []domain.Request{
			{TenantID: testTenant, NotificationRequestID: "86000000-0000-4000-8000-000000000010", LeaseToken: "86000000-0000-4000-8000-000000000110", Channel: "local-stub", DeliveryAttempts: 1},
		},
		backlogErr: errors.New("backlog probe unavailable"),
	}
	service := NewService(repo, &fakeGateway{}, Config{Limit: 10, MaxAttempts: 5, Now: func() time.Time { return now }}, nil)

	result, err := service.RunOnce(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("RunOnce must not fail on a backlog-age read error (best-effort): %v", err)
	}
	if result.SentCount != 1 {
		t.Fatalf("delivery lost: result=%#v", result)
	}
	if repo.backlogCalls != 1 {
		t.Fatalf("backlog age not queried exactly once: calls=%d", repo.backlogCalls)
	}
}

const testTenant = "00000000-0000-4000-8000-000000000001"

type fakeRepo struct {
	requests []domain.Request
	sent     []string
	failed   []failedMark

	backlogAge   time.Duration
	backlogFound bool
	backlogErr   error
	backlogCalls int
}

func (f *fakeRepo) OldestDuePendingAge(context.Context, string, time.Time) (time.Duration, bool, error) {
	f.backlogCalls++
	return f.backlogAge, f.backlogFound, f.backlogErr
}

type failedMark struct {
	id            string
	nextAttemptAt *time.Time
}

func (f *fakeRepo) ReclaimStaleSending(context.Context, string, time.Time, time.Duration) (int, error) {
	return 0, nil
}

func (f *fakeRepo) ClaimDue(context.Context, ports.ClaimParams) ([]domain.Request, error) {
	return f.requests, nil
}

func (f *fakeRepo) MarkSent(_ context.Context, _, notificationRequestID, _, _ string, _ time.Time) error {
	f.sent = append(f.sent, notificationRequestID)
	return nil
}

func (f *fakeRepo) MarkFailed(_ context.Context, _, notificationRequestID, _, _, _ string, nextAttemptAt *time.Time, _ time.Time) error {
	f.failed = append(f.failed, failedMark{id: notificationRequestID, nextAttemptAt: nextAttemptAt})
	return nil
}

type fakeGateway struct {
	failChannel string
	err         error
}

func (f *fakeGateway) Name() string { return "fake" }

func (f *fakeGateway) Send(_ context.Context, request domain.Request) error {
	if f.err != nil {
		return f.err
	}
	if request.Channel == f.failChannel {
		return errors.New("synthetic failure")
	}
	return nil
}
