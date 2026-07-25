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

func TestServiceSuppressesInvalidFCMRecipientWithoutRetry(t *testing.T) {
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	repo := &fakeRepo{requests: []domain.Request{
		{
			TenantID:              testTenant,
			NotificationRequestID: "86000000-0000-4000-8000-000000000006",
			LeaseToken:            "86000000-0000-4000-8000-000000000106",
			Channel:               "push_fcm",
			RecipientRef:          "dead-fcm-token",
			DeliveryAttempts:      1,
		},
	}}
	service := NewService(repo, &fakeGateway{err: ports.ErrInvalidRecipient}, Config{MaxAttempts: 5, Now: func() time.Time { return now }}, nil)
	result, err := service.RunOnce(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FailedCount != 0 || result.ExhaustedCount != 1 || len(repo.failed) != 1 {
		t.Fatalf("result=%#v failed=%#v", result, repo.failed)
	}
	if repo.failed[0].nextAttemptAt != nil {
		t.Fatalf("invalid recipient next attempt=%v, want nil", repo.failed[0].nextAttemptAt)
	}
	if len(repo.invalidRecipients) != 1 {
		t.Fatalf("invalid recipient cleanup calls=%#v, want 1", repo.invalidRecipients)
	}
	if got := repo.invalidRecipients[0]; got.recipientRef != "dead-fcm-token" {
		t.Fatalf("invalid recipient cleanup=%#v, want dead token", got)
	}
}

func TestServicePersistsProviderAcknowledgementWhenGatewayAndRepositorySupportIt(t *testing.T) {
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	baseRepo := &fakeRepo{requests: []domain.Request{{
		TenantID:              testTenant,
		NotificationRequestID: "86000000-0000-4000-8000-000000000005",
		LeaseToken:            "86000000-0000-4000-8000-000000000105",
		Channel:               "push_fcm",
		DeliveryAttempts:      1,
	}}}
	repo := &providerResultRepo{fakeRepo: baseRepo}
	gateway := &providerResultGateway{providerMessageID: "projects/goatos-dev/messages/provider-123"}
	service := NewService(repo, gateway, Config{Now: func() time.Time { return now }}, nil)

	result, err := service.RunOnce(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.SentCount != 1 {
		t.Fatalf("result=%#v", result)
	}
	if repo.providerMessageID != gateway.providerMessageID {
		t.Fatalf("providerMessageID=%q want %q", repo.providerMessageID, gateway.providerMessageID)
	}
}

// TestServiceRunOnceExportsGlobalBacklogAge proves the KERN-REV-06A fix: the
// backlog age comes from the repository's GLOBAL oldest-due query (not the
// claimed batch), so a saturated batch of newer requests cannot hide it, and a
// probe error is best-effort — it must never fail dispatch or drop a delivery.
func TestServiceRunOnceExportsGlobalBacklogAge(t *testing.T) {
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	repo := &fakeRepo{
		requests: []domain.Request{
			{TenantID: testTenant, NotificationRequestID: "86000000-0000-4000-8000-000000000010", LeaseToken: "86000000-0000-4000-8000-000000000110", Channel: "local-stub", DeliveryAttempts: 1},
		},
		oldestDueErr: errors.New("backlog probe unavailable"),
	}
	service := NewService(repo, &fakeGateway{}, Config{Limit: 10, MaxAttempts: 5, Now: func() time.Time { return now }}, nil)

	result, err := service.RunOnce(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("RunOnce must not fail on a backlog-probe error (best-effort): %v", err)
	}
	if result.SentCount != 1 {
		t.Fatalf("delivery lost: result=%#v", result)
	}
	if repo.oldestDueCalls != 1 {
		t.Fatalf("global backlog query not called exactly once: calls=%d", repo.oldestDueCalls)
	}
}

const testTenant = "00000000-0000-4000-8000-000000000001"

type fakeRepo struct {
	requests          []domain.Request
	sent              []string
	failed            []failedMark
	invalidRecipients []invalidRecipientSuppression

	oldestDue      time.Time
	oldestDueFound bool
	oldestDueErr   error
	oldestDueCalls int
}

func (f *fakeRepo) OldestDueRequestedAt(context.Context, string, time.Time) (time.Time, bool, error) {
	f.oldestDueCalls++
	return f.oldestDue, f.oldestDueFound, f.oldestDueErr
}

type failedMark struct {
	id            string
	nextAttemptAt *time.Time
}

type invalidRecipientSuppression struct {
	recipientRef string
	reason       string
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

func (f *fakeRepo) SuppressInvalidRecipient(_ context.Context, _, recipientRef, reason string, _ time.Time) (int, error) {
	f.invalidRecipients = append(f.invalidRecipients, invalidRecipientSuppression{recipientRef: recipientRef, reason: reason})
	return 1, nil
}

type fakeGateway struct {
	failChannel string
	err         error
}

type providerResultRepo struct {
	*fakeRepo
	providerMessageID string
}

func (r *providerResultRepo) MarkSentWithResult(
	_ context.Context,
	_, notificationRequestID, _, _, providerMessageID string,
	_ time.Time,
) error {
	r.sent = append(r.sent, notificationRequestID)
	r.providerMessageID = providerMessageID
	return nil
}

type providerResultGateway struct {
	providerMessageID string
}

func (g *providerResultGateway) Name() string { return "provider-result" }

func (g *providerResultGateway) Send(context.Context, domain.Request) error { return nil }

func (g *providerResultGateway) SendWithResult(context.Context, domain.Request) (ports.DeliveryResult, error) {
	return ports.DeliveryResult{ProviderMessageID: g.providerMessageID}, nil
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
