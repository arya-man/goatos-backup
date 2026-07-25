package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	result, err := gateway.SendWithResult(context.Background(), request("push_fcm", "device-token-1"))
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
	if message["token"] != "device-token-1" {
		t.Fatalf("message target = %#v", message)
	}
	notification, _ := message["notification"].(map[string]any)
	if notification["title"] != "Vaccination overdue" {
		t.Fatalf("notification = %#v", notification)
	}
}

func TestSendFCMUsesDefaultTopic(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gateway := New(Config{
		FCMEndpoint:     server.URL,
		FCMBearerToken:  "fcm-token",
		FCMDefaultTopic: "pc-dev",
	}, nil)
	err := gateway.Send(context.Background(), request("push_fcm", ""))
	if err != nil {
		t.Fatalf("Send FCM: %v", err)
	}
	message, _ := got["message"].(map[string]any)
	if message["topic"] != "pc-dev" {
		t.Fatalf("message target = %#v", message)
	}
}

func TestSendFCMRequiresRecipientOrDefaultTopic(t *testing.T) {
	gateway := New(Config{FCMEndpoint: "https://fcm.example/messages:send", FCMBearerToken: "token"}, nil)
	err := gateway.Send(context.Background(), request("push_fcm", ""))
	if err == nil || !strings.Contains(err.Error(), "push_fcm recipient") {
		t.Fatalf("expected recipient error, got %v", err)
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
	err := gateway.Send(context.Background(), request("push_fcm", "dead-device-token"))
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
		if err := gateway.Send(context.Background(), request("push_fcm", "device-token-1")); err != nil {
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
