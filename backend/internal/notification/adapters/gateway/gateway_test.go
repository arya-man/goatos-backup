package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/notification/domain"
	"github.com/vgoats/goatos/backend/internal/notification/ports"
	"golang.org/x/oauth2"
)

func TestSendEmailPostsVendorPayload(t *testing.T) {
	var gotAuth string
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	gateway := New(Config{
		EmailWebhookURL: server.URL,
		EmailAuthToken:  "email-token",
	}, nil)
	err := gateway.Send(context.Background(), request("email", "pc@example.com"))
	if err != nil {
		t.Fatalf("Send email: %v", err)
	}
	if gotAuth != "Bearer email-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	to, _ := got["to"].([]any)
	if len(to) != 1 || to[0] != "pc@example.com" {
		t.Fatalf("to = %#v", got["to"])
	}
	if got["subject"] != "Vaccination overdue" || got["text"] != "Shed A vaccination is overdue." {
		t.Fatalf("unexpected email body: %#v", got)
	}
}

func TestSendEmailUsesDefaultRecipient(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := New(Config{EmailWebhookURL: server.URL, EmailDefaultTo: "ops@example.com"}, nil)
	err := gateway.Send(context.Background(), request("email", "park_head"))
	if err != nil {
		t.Fatalf("Send email: %v", err)
	}
	to, _ := got["to"].([]any)
	if len(to) != 1 || to[0] != "ops@example.com" {
		t.Fatalf("to = %#v", got["to"])
	}
}

func TestSendFCMPostsHTTPV1PayloadToToken(t *testing.T) {
	var gotAuth string
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if !strings.HasSuffix(r.URL.Path, "/messages:send") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"projects/goatos-dev/messages/provider-123"}`))
	}))
	defer server.Close()

	gateway := New(Config{
		FCMEndpoint:    server.URL + "/v1/projects/goatos-dev/messages:send",
		FCMBearerToken: "fcm-token",
	}, nil)
	result, err := gateway.SendWithResult(context.Background(), request("push_fcm", "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP"))
	if err != nil {
		t.Fatalf("Send FCM: %v", err)
	}
	if result.ProviderMessageID != "projects/goatos-dev/messages/provider-123" {
		t.Fatalf("ProviderMessageID = %q", result.ProviderMessageID)
	}
	if gotAuth != "Bearer fcm-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	message, _ := got["message"].(map[string]any)
	if message["token"] != "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP" {
		t.Fatalf("message target = %#v", message)
	}
	notification, _ := message["notification"].(map[string]any)
	if notification["title"] != "Vaccination overdue" || notification["body"] != "Shed A vaccination is overdue." {
		t.Fatalf("notification = %#v", notification)
	}
	data, _ := message["data"].(map[string]any)
	if data["title"] != "Vaccination overdue" || data["body"] != "Shed A vaccination is overdue." {
		t.Fatalf("data title/body missing: %#v", data)
	}
}

func TestSendFCMBackfillsBlankDisplayText(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"projects/goatos-dev/messages/provider-blank"}`))
	}))
	defer server.Close()

	req := request("push_fcm", "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP")
	req.Title = " "
	req.Body = "\t"
	req.NotificationType = "verification_pending"

	gateway := New(Config{
		FCMEndpoint:    server.URL + "/v1/projects/goatos-dev/messages:send",
		FCMBearerToken: "fcm-token",
	}, nil)
	if _, err := gateway.SendWithResult(context.Background(), req); err != nil {
		t.Fatalf("Send FCM: %v", err)
	}

	message, _ := got["message"].(map[string]any)
	notification, _ := message["notification"].(map[string]any)
	if notification["title"] != "Mesha" || notification["body"] != "verification pending" {
		t.Fatalf("notification display text = %#v", notification)
	}
	data, _ := message["data"].(map[string]any)
	if data["title"] != "Mesha" || data["body"] != "verification pending" {
		t.Fatalf("data display fallback = %#v", data)
	}
}

func TestSendFCMBackfillsBlankDataTextForMessageKeyPush(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"projects/goatos-dev/messages/provider-data"}`))
	}))
	defer server.Close()

	req := request("push_fcm", "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP")
	req.Title = ""
	req.Body = ""
	req.NotificationType = "weighing_due"
	req.Context = []byte(`{"message_key":"weighing_due_today","category":"weighing"}`)

	gateway := New(Config{
		FCMEndpoint:    server.URL + "/v1/projects/goatos-dev/messages:send",
		FCMBearerToken: "fcm-token",
	}, nil)
	if _, err := gateway.SendWithResult(context.Background(), req); err != nil {
		t.Fatalf("Send FCM: %v", err)
	}

	message, _ := got["message"].(map[string]any)
	notification, _ := message["notification"].(map[string]any)
	if notification["title"] != "Mesha" || notification["body"] != "weighing due" {
		t.Fatalf("message_key push without recipient_locale must still have English notification fallback: %#v", notification)
	}
	data, _ := message["data"].(map[string]any)
	if data["title"] != "Mesha" || data["body"] != "weighing due" {
		t.Fatalf("data display fallback = %#v", data)
	}
}

func TestSendFCMContextCannotOverwriteDisplayText(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"projects/goatos-dev/messages/provider-context"}`))
	}))
	defer server.Close()

	req := request("push_fcm", "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP")
	req.NotificationType = "weighing_due"
	req.Context = []byte(`{"message_key":"weighing_due_today","category":"weighing","title":"","body":" "}`)

	gateway := New(Config{
		FCMEndpoint:    server.URL + "/v1/projects/goatos-dev/messages:send",
		FCMBearerToken: "fcm-token",
	}, nil)
	if _, err := gateway.SendWithResult(context.Background(), req); err != nil {
		t.Fatalf("Send FCM: %v", err)
	}

	message, _ := got["message"].(map[string]any)
	data, _ := message["data"].(map[string]any)
	if data["title"] != "Vaccination overdue" || data["body"] != "Shed A vaccination is overdue." {
		t.Fatalf("context overwrote display fallback: %#v", data)
	}
}

func TestSendFCMWithoutRecipientFailsInsteadOfBroadcasting(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := New(Config{FCMEndpoint: server.URL, FCMBearerToken: "token"}, nil)
	err := gateway.Send(context.Background(), request("push_fcm", ""))
	// ErrRecipientUnusable, not ErrInvalidRecipient: no device was named, so nothing is known
	// about any device. ErrInvalidRecipient additionally suppresses that recipient's other
	// queued pushes, which would be wrong here -- and catastrophic on the calendar path, where
	// recipient_ref carries a ROLE NAME and suppression would kill every queued push for that
	// role tenant-wide, permanently.
	if !errors.Is(err, ports.ErrRecipientUnusable) {
		t.Fatalf("error=%v, want ErrRecipientUnusable", err)
	}
	if errors.Is(err, ports.ErrInvalidRecipient) {
		t.Fatal("a recipient-less request must not suppress the recipient's other pushes")
	}
	if calls != 0 {
		t.Fatalf("provider calls=%d, want 0 (a recipient-less reminder must never broadcast)", calls)
	}
}

func TestSendFCMRejectsRoleNameRecipientWithoutSending(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := New(Config{FCMEndpoint: server.URL, FCMBearerToken: "token"}, nil)
	for _, roleName := range []string{"park_head", "pc_director", "vaccination_operator_amit"} {
		err := gateway.Send(context.Background(), request("push_fcm", roleName))
		if !errors.Is(err, ports.ErrRecipientUnusable) {
			t.Fatalf("recipient %q error=%v, want ErrRecipientUnusable", roleName, err)
		}
		// Must NOT be ErrInvalidRecipient: that sentinel suppresses every other queued push for
		// this recipient_ref. Since the ref IS the role name, one badly-addressed reminder would
		// terminally silence that whole role across the tenant.
		if errors.Is(err, ports.ErrInvalidRecipient) {
			t.Fatalf("recipient %q must not trigger recipient suppression", roleName)
		}
	}
	if calls != 0 {
		t.Fatalf("provider calls=%d, want 0 (a role name must never be posted as a device token)", calls)
	}
}

func TestSendFCMExplicitTopicRecipientStillWorks(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := New(Config{FCMEndpoint: server.URL, FCMBearerToken: "token"}, nil)
	if err := gateway.Send(context.Background(), request("push_fcm", "topic:pc-dev")); err != nil {
		t.Fatalf("Send FCM: %v", err)
	}
	message, _ := got["message"].(map[string]any)
	if message["topic"] != "pc-dev" {
		t.Fatalf("message target = %#v", message)
	}
}

// TestSendFCMWithMessageKeyWithoutLocaleKeepsEnglishNotification asserts the fallback contract:
// a push whose context carries message_key but no recipient_locale still includes a nonblank
// English `notification` block for OS display, while preserving message_key + structured params in
// `data` for the client to render locally when onMessageReceived runs.
func TestSendFCMWithMessageKeyWithoutLocaleKeepsEnglishNotification(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := New(Config{FCMEndpoint: server.URL, FCMBearerToken: "token"}, nil)
	req := request("push_fcm", "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP")
	req.Context = []byte(`{"message_key":"vaccination.reminder.overdue","park_name":"Gandhi Park","obligation_count":"3","target":"/vaccination"}`)
	if err := gateway.Send(context.Background(), req); err != nil {
		t.Fatalf("Send FCM: %v", err)
	}

	message, _ := got["message"].(map[string]any)
	notification, _ := message["notification"].(map[string]any)
	if notification["title"] != "Vaccination overdue" || notification["body"] != "Shed A vaccination is overdue." {
		t.Fatalf("message-key push without locale must keep English notification block: %#v", notification)
	}
	data, _ := message["data"].(map[string]any)
	if data["message_key"] != "vaccination.reminder.overdue" {
		t.Fatalf("data.message_key = %#v, want vaccination.reminder.overdue", data["message_key"])
	}
	if data["park_name"] != "Gandhi Park" || data["obligation_count"] != "3" {
		t.Fatalf("structured params missing from data: %#v", data)
	}
	if data["target"] != "/vaccination" {
		t.Fatalf("target field not preserved: %#v", data["target"])
	}
	android, _ := message["android"].(map[string]any)
	if android["priority"] != "high" {
		t.Fatalf("android.priority = %#v, want high", android["priority"])
	}
}

func TestSendFCMStringifiesScalarContextValues(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := New(Config{FCMEndpoint: server.URL, FCMBearerToken: "token"}, nil)
	req := request("push_fcm", "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP")
	req.Context = []byte(`{"message_key":"vaccination.reminder.overdue","obligation_count":3,"urgent":true,"target":"/vaccination"}`)
	if err := gateway.Send(context.Background(), req); err != nil {
		t.Fatalf("Send FCM: %v", err)
	}

	message, _ := got["message"].(map[string]any)
	data, _ := message["data"].(map[string]any)
	if data["message_key"] != "vaccination.reminder.overdue" || data["obligation_count"] != "3" || data["urgent"] != "true" {
		t.Fatalf("scalar context values not preserved as FCM data strings: %#v", data)
	}
	if data["target"] != "/vaccination" {
		t.Fatalf("target field not preserved: %#v", data["target"])
	}
}

// TestSendFCMWithoutMessageKeyKeepsServerRenderedNotification is the hybrid safety net: a caller
// that has not migrated to message_key (no message_key in context) still gets the legacy
// server-rendered notification block from Title/Body, so it is never silently dropped.
func TestSendFCMWithoutMessageKeyKeepsServerRenderedNotification(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := New(Config{FCMEndpoint: server.URL, FCMBearerToken: "token"}, nil)
	if err := gateway.Send(context.Background(), request("push_fcm", "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP")); err != nil {
		t.Fatalf("Send FCM: %v", err)
	}
	message, _ := got["message"].(map[string]any)
	notification, _ := message["notification"].(map[string]any)
	if notification["title"] != "Vaccination overdue" || notification["body"] != "Shed A vaccination is overdue." {
		t.Fatalf("legacy notification block missing/altered: %#v", notification)
	}
	android, _ := message["android"].(map[string]any)
	if android["priority"] != "high" {
		t.Fatalf("android.priority = %#v, want high even for legacy sends", android["priority"])
	}
}

// TestSendFCMWithRecipientLocaleAddsLocalizedGenericNotification covers the forward-compatible
// hybrid branch: once a producer supplies `recipient_locale` (not populated by anything today --
// see the DDL/locale-persistence note in the localization-decision comment), the gateway must ALSO
// send a locale-correct, category-generic `notification` block alongside the client-render data
// payload, so a killed app that can't run onMessageReceived still shows something in the
// recipient's language instead of nothing.
func TestSendFCMWithRecipientLocaleAddsLocalizedGenericNotification(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := New(Config{FCMEndpoint: server.URL, FCMBearerToken: "token"}, nil)
	req := request("push_fcm", "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP")
	req.Context = []byte(`{"message_key":"vaccination.reminder.overdue","recipient_locale":"hi","category":"vaccination","target":"/vaccination"}`)
	if err := gateway.Send(context.Background(), req); err != nil {
		t.Fatalf("Send FCM: %v", err)
	}

	message, _ := got["message"].(map[string]any)
	notification, _ := message["notification"].(map[string]any)
	if notification == nil {
		t.Fatalf("expected a locale-aware notification block when recipient_locale is known, got none: %#v", message)
	}
	if notification["title"] != "टीकाकरण" {
		t.Fatalf("notification.title = %#v, want the Hindi vaccination title", notification["title"])
	}
	data, _ := message["data"].(map[string]any)
	if data["message_key"] != "vaccination.reminder.overdue" {
		t.Fatalf("data.message_key missing/altered: %#v", data)
	}
	if data["target"] != "/vaccination" {
		t.Fatalf("target field not preserved: %#v", data["target"])
	}
}

func TestSendFCMSlowProviderTimesOutAsRetryableFailure(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	gateway := New(Config{
		FCMEndpoint:    server.URL,
		FCMBearerToken: "token",
		HTTPTimeout:    50 * time.Millisecond,
	}, nil)
	err := gateway.Send(context.Background(), request("push_fcm", "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP"))
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "did not respond within") {
		t.Fatalf("error=%v, want a provider timeout", err)
	}
	if errors.Is(err, ports.ErrChannelNotConfigured) || errors.Is(err, ports.ErrInvalidRecipient) {
		t.Fatalf("timeout must stay retryable, got %v", err)
	}
}

func TestSendFCMClassifiesNotRegisteredAsInvalidRecipient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{
		  "error": {
		    "code": 404,
		    "message": "NotRegistered",
		    "status": "NOT_FOUND",
		    "details": [{"@type": "type.googleapis.com/google.firebase.fcm.v1.FcmError", "errorCode": "UNREGISTERED"}]
		  }
		}`))
	}))
	defer server.Close()

	gateway := New(Config{FCMEndpoint: server.URL, FCMBearerToken: "fcm-token"}, nil)
	err := gateway.Send(context.Background(), request("push_fcm", "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP"))
	if !errors.Is(err, ports.ErrInvalidRecipient) {
		t.Fatalf("error=%v, want ErrInvalidRecipient", err)
	}
}

func TestWebhookNotRegisteredBodyKeepsGenericRetryableFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"unregistered customer webhook"}`))
	}))
	defer server.Close()

	gateway := New(Config{WebhookURL: server.URL}, nil)
	err := gateway.Send(context.Background(), request("webhook", "customer-webhook"))
	if err == nil {
		t.Fatal("expected webhook delivery failure")
	}
	if errors.Is(err, ports.ErrInvalidRecipient) {
		t.Fatalf("webhook error must not be classified as invalid FCM recipient: %v", err)
	}
}

func TestSendIncidentPostsDedupePayload(t *testing.T) {
	var gotAuth string
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	gateway := New(Config{IncidentWebhookURL: server.URL, IncidentAuthToken: "incident-token"}, nil)
	err := gateway.Send(context.Background(), request("incident", "pc_director"))
	if err != nil {
		t.Fatalf("Send incident: %v", err)
	}
	if gotAuth != "Bearer incident-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if got["dedupe_key"] != "86000000-0000-4000-8000-000000000001" {
		t.Fatalf("dedupe payload = %#v", got)
	}
	if got["routing_key"] != "incident" || got["title"] != "Vaccination overdue" {
		t.Fatalf("incident payload = %#v", got)
	}
}

func TestSendIncidentFallsBackToSlackWhenIncidentWebhookMissing(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	gateway := New(Config{SlackWebhookURL: server.URL}, nil)
	err := gateway.Send(context.Background(), request("incident", "pc_director"))
	if err != nil {
		t.Fatalf("Send incident fallback: %v", err)
	}
	if got["text"] != "Vaccination overdue\nShed A vaccination is overdue." {
		t.Fatalf("slack fallback payload = %#v", got)
	}
}

func TestSendFCMUsesCachedTokenSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer adc-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	source := &fakeTokenSource{token: &oauth2.Token{AccessToken: "adc-token"}}
	gateway := New(Config{FCMEndpoint: server.URL}, nil)
	gateway.fcmTokenSource = source
	for i := 0; i < 2; i++ {
		if err := gateway.Send(context.Background(), request("push_fcm", "cJ3q7Xl2Rk6:APA91bH_test_device_registration_token_0123456789abcdefghijklmnopqrstuvwxyz-ABCDEFGHIJKLMNOP")); err != nil {
			t.Fatalf("Send FCM %d: %v", i, err)
		}
	}
	if source.calls != 2 {
		t.Fatalf("token source calls=%d, want one token fetch per send from cached source", source.calls)
	}
}

type fakeTokenSource struct {
	token *oauth2.Token
	calls int
}

func (f *fakeTokenSource) Token() (*oauth2.Token, error) {
	f.calls++
	return f.token, nil
}

func request(channel, recipientRef string) domain.Request {
	return domain.Request{
		NotificationRequestID: "86000000-0000-4000-8000-000000000001",
		TenantID:              "00000000-0000-4000-8000-000000000001",
		CalendarEventID:       "calendar:86000000-0000-4000-8000-000000000001",
		TargetType:            "shed",
		TargetID:              "00000000-0000-4000-8000-000000004001",
		NotificationType:      "escalation",
		Channel:               channel,
		RecipientRef:          recipientRef,
		Title:                 "Vaccination overdue",
		Body:                  "Shed A vaccination is overdue.",
		TraceID:               "trace-test",
		Context:               []byte(`{"escalation_level":2}`),
	}
}

func TestFCMUnregisteredTokenDetection(t *testing.T) {
	tests := []struct {
		name        string
		errMsg      string
		wantInvalid bool
	}{
		{
			name:        "404 with UNREGISTERED",
			errMsg:      "notification webhook status 404: {\"error\":{\"code\":404,\"message\":\"Requested entity was not found.\",\"status\":\"NOT_FOUND\",\"details\":[{\"errorCode\":\"UNREGISTERED\"}]}}",
			wantInvalid: true,
		},
		{
			name:        "404 with unregistered lowercase",
			errMsg:      "notification webhook status 404: the token is unregistered",
			wantInvalid: true,
		},
		{
			name:        "404 with notregistered keyword",
			errMsg:      "notification webhook status 404: errorcode=notregistered",
			wantInvalid: true,
		},
		{
			// P0 regression: a bare/generic INVALID_ARGUMENT must NOT be treated as a dead
			// recipient. This is exactly the shape FCM returns for a malformed PAYLOAD (wrong
			// field, bad data block) -- identical for every recipient in the fleet -- not just for
			// a dead token. Suppressing on this signal previously mass-revoked every addressed
			// device on a single bad push (device-lockout P0). See isInvalidFCMRecipientResponse.
			name:        "400 with bare INVALID_ARGUMENT is ambiguous, must not suppress",
			errMsg:      "notification webhook status 400: {\"error\":{\"code\":400,\"message\":\"Invalid argument.\",\"status\":\"INVALID_ARGUMENT\"}}",
			wantInvalid: false,
		},
		{
			name:        "400 with bare invalid argument text is ambiguous, must not suppress",
			errMsg:      "notification webhook status 400: the request has an invalid argument",
			wantInvalid: false,
		},
		{
			name:        "400 with invalid-registration-token names the token specifically",
			errMsg:      "notification webhook status 400: invalid-registration-token",
			wantInvalid: true,
		},
		{
			name:        "400 with explicit not-a-valid-token message",
			errMsg:      "notification webhook status 400: {\"error\":{\"code\":400,\"message\":\"The registration token is not a valid FCM registration token\",\"status\":\"INVALID_ARGUMENT\"}}",
			wantInvalid: true,
		},
		{
			name:        "400 malformed payload error must not suppress even though status is INVALID_ARGUMENT",
			errMsg:      "notification webhook status 400: {\"error\":{\"code\":400,\"message\":\"Invalid JSON payload received. Unknown name \\\"badfield\\\" at 'message': Cannot find field.\",\"status\":\"INVALID_ARGUMENT\"}}",
			wantInvalid: false,
		},
		{
			name:        "503 server error (transient, not invalid)",
			errMsg:      "notification webhook status 503: service unavailable",
			wantInvalid: false,
		},
		{
			name:        "404 without UNREGISTERED keyword",
			errMsg:      "notification webhook status 404: not found",
			wantInvalid: false,
		},
		{
			name:        "nil error",
			errMsg:      "",
			wantInvalid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = errors.New(tt.errMsg)
			}
			got := isInvalidFCMRecipientResponse(err)
			if got != tt.wantInvalid {
				t.Errorf("isInvalidFCMRecipientResponse() = %v, want %v\nError: %v", got, tt.wantInvalid, err)
			}
		})
	}
}
