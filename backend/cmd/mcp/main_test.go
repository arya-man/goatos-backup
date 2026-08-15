package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	for _, want := range []string{"ask_goatos", "get_vaccination_today", "list_goatos_capabilities", "goatos_mcp_health"} {
		if !names[want] {
			t.Fatalf("missing tool %s in %+v", want, names)
		}
	}
	if !strings.Contains(rec.Body.String(), `"readOnlyHint":true`) || !strings.Contains(rec.Body.String(), `"destructiveHint":false`) {
		t.Fatalf("tools should advertise read-only annotations: %s", rec.Body.String())
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

func TestAskUsesConfiguredTenantWhenClientDoesNotSendTenantHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-GoatOS-Tenant-ID") != "00000000-0000-4000-8000-000000000001" {
			t.Fatalf("tenant not defaulted: %q", r.Header.Get("X-GoatOS-Tenant-ID"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answer": "tenant ok"})
	}))
	defer upstream.Close()

	s := newServer(config{
		UpstreamAskURL: upstream.URL,
		MCPPath:        "/mcp",
		TenantID:       "00000000-0000-4000-8000-000000000001",
		AllowedEmails:  mustEmailSet(t, "aryaman@mesha.sg"),
		TokenVerifier:  staticTokenVerifier{claims: platformauth.Claims{Email: "aryaman@mesha.sg", EmailVerified: boolPtr(true)}},
	}, upstream.Client(), nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"x","method":"tools/call","params":{"name":"ask_goatos","arguments":{"question":"Which farm is behind?"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tenant ok") {
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

func TestVaccinationTodayCallsLiveTrackerAndReturnsStructuredContent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vaccination/live-tracker" {
			t.Fatalf("path=%s want /vaccination/live-tracker", r.URL.Path)
		}
		if r.URL.Query().Get("business_date") != "2026-08-14" || r.URL.Query().Get("status") != "pending" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer user-token" {
			t.Fatalf("Authorization not proxied: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Mesha-Actor-Email") != "aryaman@mesha.sg" {
			t.Fatalf("verified actor email not proxied: %q", r.Header.Get("X-Mesha-Actor-Email"))
		}
		if r.Header.Get("X-GoatOS-Tenant-ID") != "00000000-0000-4000-8000-000000000001" {
			t.Fatalf("tenant not proxied: %q", r.Header.Get("X-GoatOS-Tenant-ID"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"business_date": "2026-08-14",
			"generated_at":  "2026-08-14T17:45:00Z",
			"is_live_day":   true,
			"kpis": map[string]any{
				"scheduled_administrations": 10,
				"closed_administrations":    0,
				"proof_videos_received":     0,
				"scan_captures":             0,
				"remaining":                 10,
			},
			"operators": []map[string]any{{
				"operator_name":             "Darshan Talwar",
				"park_name":                 "Channapatna",
				"current_shed_label":        "Yashoda",
				"current_partition_label":   "Part 2",
				"current_vaccine_label":     "ET+TT",
				"scheduled_administrations": 4,
				"closed_administrations":    0,
				"proof_videos":              0,
				"scan_captures":             0,
				"remaining":                 4,
				"state":                     "not_started",
			}},
			"sheds": []map[string]any{{
				"park_name":                 "Channapatna",
				"shed_label":                "Yashoda - Part 2",
				"vaccine_label":             "ET+TT",
				"operator_name":             "Darshan Talwar",
				"scheduled_administrations": 4,
				"closed_administrations":    0,
				"proof_videos_received":     0,
				"remaining":                 4,
				"state":                     "not_started",
			}},
			"unassigned_administrations": 0,
		})
	}))
	defer upstream.Close()

	s := newServer(config{
		UpstreamBaseURL: upstream.URL,
		UpstreamAskURL:  upstream.URL + "/ceo-ai/ask",
		MCPPath:         "/mcp",
		UpstreamTimeout: time.Second,
		AllowedEmails:   mustEmailSet(t, "aryaman@mesha.sg"),
		TokenVerifier:   staticTokenVerifier{claims: platformauth.Claims{Email: "aryaman@mesha.sg", EmailVerified: boolPtr(true)}},
	}, upstream.Client(), nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"vax","method":"tools/call","params":{"name":"get_vaccination_today","arguments":{"business_date":"2026-08-14","status":"pending"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	req.Header.Set("X-GoatOS-Tenant-ID", "00000000-0000-4000-8000-000000000001")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Vaccination drive-day progress for 2026-08-14", "Darshan Talwar", `"structuredContent"`, `"scheduled_administrations":10`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in body=%s", want, body)
		}
	}
}

func TestVaccinationTodayRejectsInvalidArgsBeforeUpstream(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	s := newServer(config{
		UpstreamBaseURL: upstream.URL,
		UpstreamAskURL:  upstream.URL + "/ceo-ai/ask",
		MCPPath:         "/mcp",
		AllowedEmails:   mustEmailSet(t, "aryaman@mesha.sg"),
		TokenVerifier:   staticTokenVerifier{claims: platformauth.Claims{Email: "aryaman@mesha.sg", EmailVerified: boolPtr(true)}},
	}, upstream.Client(), nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"vax","method":"tools/call","params":{"name":"get_vaccination_today","arguments":{"business_date":"14-08-2026"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if called {
		t.Fatal("upstream should not be called for invalid arguments")
	}
	if !strings.Contains(rec.Body.String(), "business_date_must_be_yyyy_mm_dd") {
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

func TestUnknownNotificationWithoutIDReturnsNoContent(t *testing.T) {
	s := newServer(config{UpstreamAskURL: "http://example.invalid/ceo-ai/ask", MCPPath: "/mcp", UpstreamTimeout: time.Second}, http.DefaultClient, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/unknown"}`))
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("notification response body=%q, want empty", rec.Body.String())
	}
}

func TestMCPWithoutBearerAdvertisesOAuthDiscovery(t *testing.T) {
	s := newServer(config{
		PublicURL:      "https://goatos-mcp-stg.example.com",
		UpstreamAskURL: "http://example.invalid/ceo-ai/ask",
		MCPPath:        "/mcp",
		TokenVerifier:  staticTokenVerifier{claims: platformauth.Claims{Email: "ravi@mesha.sg", EmailVerified: boolPtr(true)}},
	}, http.DefaultClient, nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, `resource_metadata="https://goatos-mcp-stg.example.com/.well-known/oauth-protected-resource"`) {
		t.Fatalf("WWW-Authenticate=%q", got)
	}
}

func TestOAuthMetadataAndCodeExchange(t *testing.T) {
	s := newServer(config{
		PublicURL:      "https://goatos-mcp-stg.example.com",
		UpstreamAskURL: "http://example.invalid/ceo-ai/ask",
		MCPPath:        "/mcp",
	}, http.DefaultClient, nil)

	metaReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	metaRec := httptest.NewRecorder()
	s.handleProtectedResourceMetadata(metaRec, metaReq)
	if metaRec.Code != http.StatusOK || !strings.Contains(metaRec.Body.String(), "authorization_servers") {
		t.Fatalf("metadata status=%d body=%s", metaRec.Code, metaRec.Body.String())
	}

	verifier := "codex-pkce-verifier"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	s.oauthCodes["code-1"] = oauthCode{
		Token:               "firebase-id-token",
		ExpiresAt:           time.Now().Add(time.Minute),
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "code-1")
	form.Set("code_verifier", verifier)
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	s.handleToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"access_token":"firebase-id-token"`) {
		t.Fatalf("body=%s", rec.Body.String())
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
