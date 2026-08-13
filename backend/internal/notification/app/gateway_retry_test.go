package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/notification/adapters/gateway"
	"github.com/vgoats/goatos/backend/internal/notification/domain"
)

// A push credential lookup talks to the metadata server / ADC over the network. When it fails, the
// dispatcher must schedule another attempt -- one credential hiccup must not permanently exhaust every
// claimed reminder in that tick. This drives the REAL gateway through the REAL dispatch path.
func TestServiceRetriesWhenPushCredentialsCannotBeResolved(t *testing.T) {
	// Force credential discovery to fail deterministically, regardless of the machine's own ADC.
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", t.TempDir()+"/absent-service-account.json")

	now := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	deviceToken := "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP"
	repo := &fakeRepo{requests: []domain.Request{{
		TenantID:              testTenant,
		NotificationRequestID: "86000000-0000-4000-8000-000000000007",
		LeaseToken:            "86000000-0000-4000-8000-000000000107",
		Channel:               "push_fcm",
		RecipientRef:          deviceToken,
		DeliveryAttempts:      1,
	}}}
	realGateway := gateway.New(gateway.Config{FCMProjectID: "goatos-test"}, nil)
	service := NewService(repo, realGateway, Config{
		Limit:       10,
		MaxAttempts: 5,
		BackoffBase: time.Minute,
		Now:         func() time.Time { return now },
	}, nil)

	result, err := service.RunOnce(context.Background(), testTenant)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(repo.failed) != 1 {
		t.Fatalf("failed marks=%#v, want 1", repo.failed)
	}
	if repo.failed[0].nextAttemptAt == nil {
		t.Fatalf("credential failure scheduled no retry; result=%#v", result)
	}
	if result.ExhaustedCount != 0 || result.FailedCount != 1 {
		t.Fatalf("result=%#v, want a retryable failure not an exhaustion", result)
	}
	if len(repo.invalidRecipients) != 0 {
		t.Fatalf("credential failure must not suppress the device: %#v", repo.invalidRecipients)
	}
}
