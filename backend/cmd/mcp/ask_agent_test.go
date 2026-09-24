package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
)

func agentTestServer(t *testing.T, agentURL, legacyURL string, timeout time.Duration) *server {
	t.Helper()
	s := newServer(config{
		UpstreamAskURL:  legacyURL,
		MCPPath:         "/mcp",
		UpstreamTimeout: time.Second,
		TenantID:        "00000000-0000-4000-8000-000000000001",
		AllowedEmails:   mustEmailSet(t, "aryaman@mesha.sg"),
		TokenVerifier:   staticTokenVerifier{claims: platformauth.Claims{Email: "aryaman@mesha.sg", EmailVerified: boolPtr(true)}},
		AgentURL:        agentURL,
		AgentAudience:   "https://goatos-ask-mesha-stg.example.run.app",
		AgentTimeout:    timeout,
	}, http.DefaultClient, nil)
	if agentURL != "" {
		s.idToken = func(_ context.Context, audience string) (string, error) {
			if audience != "https://goatos-ask-mesha-stg.example.run.app" {
				t.Errorf("audience=%q", audience)
			}
			return "google-id-token", nil
		}
	}
	return s
}

func callAsk(s *server, args string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"x","method":"tools/call","params":{"name":"ask_goatos","arguments":`+args+`}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()
	s.handleMCP(rec, req)
	return rec
}

func TestAskViaAgentSendsIDTokenBearerAndConversation(t *testing.T) {
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("legacy /ceo-ai/ask called with the agent flag on")
	}))
	defer legacy.Close()
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ceo-ai/ask" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if got := r.Header.Get("X-Serverless-Authorization"); got != "Bearer google-id-token" {
			t.Errorf("X-Serverless-Authorization=%q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer user-token" {
			t.Errorf("user bearer not forwarded: %q", got)
		}
		if got := r.Header.Get("X-Mesha-Client"); got != "mcp" {
			t.Errorf("X-Mesha-Client=%q", got)
		}
		if got := r.Header.Get("X-Mesha-Actor-Email"); got != "aryaman@mesha.sg" {
			t.Errorf("actor email=%q", got)
		}
		if got := r.Header.Get("X-GoatOS-Tenant-ID"); got != "00000000-0000-4000-8000-000000000001" {
			t.Errorf("tenant=%q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != false || body["conversation_id"] != "chat-7" || body["question"] != "What vaccination is scheduled today?" {
			t.Errorf("body=%v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answer": "Two sheds today.", "conversation_id": "chat-7", "message_id": "m1", "request_id": "r1", "chart": nil})
	}))
	defer agent.Close()

	// A schedule question would hit the typed-tool shortcut on the legacy path; the agent answers it here.
	rec := callAsk(agentTestServer(t, agent.URL, legacy.URL, 5*time.Second), `{"question":"What vaccination is scheduled today?","conversation_id":"chat-7"}`)
	out := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(out, "Two sheds today.") || !strings.Contains(out, "Conversation: chat-7") || !strings.Contains(out, `"structuredContent"`) {
		t.Fatalf("status=%d body=%s", rec.Code, out)
	}
}

func TestAskViaAgentTimeout(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(400 * time.Millisecond):
		}
	}))
	defer agent.Close()
	rec := callAsk(agentTestServer(t, agent.URL, "http://legacy.invalid/ceo-ai/ask", 100*time.Millisecond), `{"question":"why is ADG down?"}`)
	if !strings.Contains(rec.Body.String(), "agent_timeout") {
		t.Fatalf("want agent_timeout, got %s", rec.Body.String())
	}
}

func TestAskViaAgentBusyChatIsToolError(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "chat_busy", "message": "I'm still answering your previous question in this chat."})
	}))
	defer agent.Close()
	rec := callAsk(agentTestServer(t, agent.URL, "http://legacy.invalid/ceo-ai/ask", time.Second), `{"question":"and last week?","conversation_id":"c1"}`)
	out := rec.Body.String()
	if !strings.Contains(out, "still answering") || !strings.Contains(out, `"isError":true`) {
		t.Fatalf("body=%s", out)
	}
}

func TestAskFlagOffUsesLegacyUpstream(t *testing.T) {
	hit := false
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if r.Header.Get("X-Serverless-Authorization") != "" || r.Header.Get("X-Mesha-Client") != "" {
			t.Errorf("agent headers leaked to legacy upstream")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answer": "legacy", "conversation_id": "conv-1"})
	}))
	defer legacy.Close()
	s := agentTestServer(t, "", legacy.URL, 0)
	if s.agentClient != nil || s.idToken != nil {
		t.Fatal("agent client configured with the flag off")
	}
	rec := callAsk(s, `{"question":"Which farm is behind?"}`)
	if !hit || !strings.Contains(rec.Body.String(), "legacy") {
		t.Fatalf("legacy not used: %s", rec.Body.String())
	}
	for _, tl := range s.toolList() {
		if tl["name"] == "ask_goatos" && tl["description"] != askGoatOSDescription(false) {
			t.Fatal("flag off must keep the old ask_goatos description")
		}
	}
}

func TestAgentToolDescriptionAndEnv(t *testing.T) {
	s := agentTestServer(t, "http://agent.invalid", "http://legacy.invalid/ceo-ai/ask", time.Second)
	for _, tl := range s.toolList() {
		if tl["name"] == "ask_goatos" && !strings.Contains(tl["description"].(string), "conversation_id") {
			t.Fatalf("agent description missing follow-up guidance: %v", tl["description"])
		}
	}
	t.Setenv("MESHA_MCP_UPSTREAM_ASK_URL", "http://legacy.invalid/ceo-ai/ask")
	t.Setenv("MESHA_MCP_AGENT_URL", "https://agent.example.run.app/")
	t.Setenv("MESHA_MCP_AGENT_AUDIENCE", "")
	t.Setenv("MESHA_MCP_AGENT_TIMEOUT", "")
	t.Setenv("GOATOS_AUTH_MODE", "bearer")
	t.Setenv("GOATOS_AUTH_ISSUER", "goatos-local")
	t.Setenv("GOATOS_AUTH_AUDIENCE", "goatos-admin")
	t.Setenv("GOATOS_AUTH_HS256_SECRET", "0123456789abcdef0123456789abcdef")
	cfg, err := configFromEnv()
	if err != nil {
		t.Fatalf("configFromEnv: %v", err)
	}
	if cfg.AgentURL != "https://agent.example.run.app" || cfg.AgentAudience != cfg.AgentURL || cfg.AgentTimeout != 240*time.Second {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestMetadataIDTokenSourceCachesPerAudience(t *testing.T) {
	calls := 0
	md := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Metadata-Flavor") != "Google" || r.URL.Query().Get("audience") != "https://a" {
			t.Errorf("bad metadata request %v %v", r.Header, r.URL)
		}
		_, _ = w.Write([]byte("tok-1\n"))
	}))
	defer md.Close()
	src := newMetadataIDTokenSource(md.URL)
	for i := 0; i < 2; i++ {
		tok, err := src.Token(context.Background(), "https://a")
		if err != nil || tok != "tok-1" {
			t.Fatalf("tok=%q err=%v", tok, err)
		}
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1 (cached)", calls)
	}
}
