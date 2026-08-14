package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
)

func TestToolsList(t *testing.T) {
	s := newServer(config{UpstreamAskURL: "http://example.invalid/ceo-ai/ask", MCPPath: "/mcp", UpstreamTimeout: time.Second}, http.DefaultClient, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range got.Result.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"ask_goatos", "list_goatos_capabilities", "goatos_mcp_health"} {
		if !names[want] {
			t.Fatalf("missing tool %s in %+v", want, names)
		}
	}
}

func TestAskProxiesBearerAndTenantToUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-token" {
			t.Fatalf("Authorization not proxied: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Mesha-Actor-Email") != "aryaman@mesha.sg" {
			t.Fatalf("verified actor email not proxied: %q", r.Header.Get("X-Mesha-Actor-Email"))
		}
		if r.Header.Get("X-GoatOS-Tenant-ID") != "00000000-0000-4000-8000-000000000001" {
			t.Fatalf("tenant not proxied: %q", r.Header.Get("X-GoatOS-Tenant-ID"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["stream"] != false {
			t.Fatalf("stream=%v want false", body["stream"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answer":          "hello",
			"source":          "Mesha read API",
			"mode":            "fallback",
			"request_id":      "req-1",
			"conversation_id": "conv-1",
		})
	}))
	defer upstream.Close()

	s := newServer(config{
		UpstreamAskURL:  upstream.URL,
		MCPPath:         "/mcp",
		UpstreamTimeout: time.Second,
		AllowedEmails:   mustEmailSet(t, "aryaman@mesha.sg"),
		TokenVerifier:   staticTokenVerifier{claims: platformauth.Claims{Email: "aryaman@mesha.sg", EmailVerified: boolPtr(true)}},
	}, upstream.Client(), nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"x","method":"tools/call","params":{"name":"ask_goatos","arguments":{"question":"Which farm is behind?"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	req.Header.Set("X-Mesha-Actor-Email", "aryaman@mesha.sg")
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hello") || !strings.Contains(rec.Body.String(), "Conversation: conv-1") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestAskRejectsEmailOutsideAllowlistBeforeUpstream(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	s := newServer(config{
		UpstreamAskURL: upstream.URL,
		MCPPath:        "/mcp",
		AllowedEmails:  mustEmailSet(t, "aryaman@mesha.sg"),
		TokenVerifier:  staticTokenVerifier{claims: platformauth.Claims{Email: "someone@example.com", EmailVerified: boolPtr(true)}},
	}, upstream.Client(), nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_goatos","arguments":{"question":"hi"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	req.Header.Set("X-Mesha-Actor-Email", "aryaman@mesha.sg")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if called {
		t.Fatal("upstream should not be called for disallowed email")
	}
	if !strings.Contains(rec.Body.String(), "actor_email_not_allowed") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestInitializedNotificationWithoutIDReturnsNoContent(t *testing.T) {
	s := newServer(config{UpstreamAskURL: "http://example.invalid/ceo-ai/ask", MCPPath: "/mcp", UpstreamTimeout: time.Second}, http.DefaultClient, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("notification response body=%q, want empty", rec.Body.String())
	}
}

type staticTokenVerifier struct {
	claims platformauth.Claims
	err    error
}

func (v staticTokenVerifier) Verify(string) (platformauth.Claims, error) {
	return v.claims, v.err
}

func mustEmailSet(t *testing.T, emails ...string) authallow.EmailSet {
	t.Helper()
	set, err := authallow.NewEmailSet(emails)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func boolPtr(v bool) *bool {
	return &v
}
