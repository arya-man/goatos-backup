package browserpushhttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/browserpush"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type fakeBrowserPushRepo struct {
	eventCalls int
}

func (f *fakeBrowserPushRepo) Upsert(context.Context, string, string, browserpush.RegisterRequest, time.Time) (browserpush.Registration, bool, error) {
	return browserpush.Registration{}, false, nil
}

func (f *fakeBrowserPushRepo) MarkUnsubscribed(context.Context, string, string, string, time.Time) (bool, error) {
	return false, nil
}

func (f *fakeBrowserPushRepo) ListForMember(context.Context, string, string) ([]browserpush.Registration, error) {
	return nil, nil
}

func (f *fakeBrowserPushRepo) ResolveMemberRecipients(context.Context, string, string) ([]browserpush.Recipient, error) {
	return nil, nil
}

func (f *fakeBrowserPushRepo) ResolveModuleDutyRecipients(context.Context, string, string, string, string, string, time.Time) ([]browserpush.Recipient, error) {
	return nil, nil
}

func (f *fakeBrowserPushRepo) ResolvePositionRecipients(context.Context, string, string, string, string, time.Time) ([]browserpush.Recipient, error) {
	return nil, nil
}

func (f *fakeBrowserPushRepo) ResolveModuleDutyRecipientsBatch(context.Context, string, string, []string, string, []string, time.Time) (map[string][]browserpush.Recipient, error) {
	return nil, nil
}

func (f *fakeBrowserPushRepo) ResolvePositionRecipientsBatch(context.Context, string, string, []string, []string, time.Time) (map[string][]browserpush.Recipient, error) {
	return nil, nil
}

func (f *fakeBrowserPushRepo) MarkTokenStale(context.Context, string, string, string, time.Time) (int, error) {
	return 0, nil
}

func (f *fakeBrowserPushRepo) RecordEvent(context.Context, string, string, browserpush.EventRequest, time.Time) (bool, error) {
	f.eventCalls++
	return true, nil
}

func TestRecordBrowserEventInvalidNotificationRequestIDIsValidationError(t *testing.T) {
	repo := &fakeBrowserPushRepo{}
	handler := NewHandler(
		browserpush.NewService(repo),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	body := `{"notification_request_id":"not-a-uuid","browser_install_id":"web-1","event_type":"opened"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/notifications/browser-events", strings.NewReader(body))
	ctx := httpmiddleware.WithTenantID(req.Context(), "tenant-1")
	ctx = httpmiddleware.WithActorID(ctx, "user-1")
	req = req.WithContext(ctx)
	res := httptest.NewRecorder()

	handler.RecordBrowserEvent(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusBadRequest, res.Body.String())
	}
	var got errorEnvelope
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Code != "browser_push_invalid_request" {
		t.Fatalf("code = %q, want browser_push_invalid_request", got.Code)
	}
	if got.Retryable {
		t.Fatal("validation errors must not be retryable")
	}
	if repo.eventCalls != 0 {
		t.Fatalf("invalid request reached repository %d times", repo.eventCalls)
	}
}
