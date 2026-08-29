package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
				Name        string `json:"name"`
				InputSchema struct {
					Required []string `json:"required"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	requiredByTool := map[string][]string{}
	for _, tool := range got.Result.Tools {
		names[tool.Name] = true
		requiredByTool[tool.Name] = tool.InputSchema.Required
	}
	for _, want := range []string{"ask_goatos", "get_vaccination_today", "get_action_center", "get_verification_backlog", "get_feed_today", "get_procurement_pipeline", "get_sales_overview", "get_sales_deals", "get_counts_summary", "get_health_today", "get_health_work_items", "get_milk_feeding_today", "get_workforce_coverage", "get_weighing_progress", "get_weighing_growth_adg", "get_weighing_shed_weights", "get_weighing_process_state", "get_weighing_weight_demographics", "list_goatos_capabilities", "goatos_mcp_health"} {
		if !names[want] {
			t.Fatalf("missing tool %s in %+v", want, names)
		}
	}
	if !strings.Contains(rec.Body.String(), `"readOnlyHint":true`) || !strings.Contains(rec.Body.String(), `"destructiveHint":false`) {
		t.Fatalf("tools should advertise read-only annotations: %s", rec.Body.String())
	}
	for tool, want := range map[string][]string{
		"get_feed_today":        {"park_id", "target_date"},
		"get_health_work_items": {"age_band"},
	} {
		gotRequired := map[string]bool{}
		for _, item := range requiredByTool[tool] {
			gotRequired[item] = true
		}
		for _, item := range want {
			if !gotRequired[item] {
				t.Fatalf("%s required=%v, missing %s", tool, requiredByTool[tool], item)
			}
		}
	}
}

func TestTokenVerifierFromEnvSupportsLocalHS256Bearer(t *testing.T) {
	t.Setenv("GOATOS_AUTH_MODE", "bearer")
	t.Setenv("GOATOS_AUTH_ISSUER", "goatos-local")
	t.Setenv("GOATOS_AUTH_AUDIENCE", "goatos-admin")
	t.Setenv("GOATOS_AUTH_HS256_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("GOATOS_AUTH_MAX_TOKEN_TTL", "2h")

	cfg := platformauth.Config{
		Issuer:   "goatos-local",
		Audience: "goatos-admin",
		Secret:   []byte("0123456789abcdef0123456789abcdef"),
		MaxTTL:   2 * time.Hour,
	}
	token, err := platformauth.MintHS256Token(cfg, "10000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000001", time.Hour)
	if err != nil {
		t.Fatalf("MintHS256Token: %v", err)
	}
	verifier, err := tokenVerifierFromEnv()
	if err != nil {
		t.Fatalf("tokenVerifierFromEnv: %v", err)
	}
	claims, err := verifier.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "10000000-0000-4000-8000-000000000001" || claims.TenantID != "20000000-0000-4000-8000-000000000001" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestAPIReadToolPathsAreInOpenAPI(t *testing.T) {
	spec, err := os.ReadFile("../../../contracts/openapi/app-api.yaml")
	if err != nil {
		t.Fatal(err)
	}
	body := string(spec)
	for _, def := range apiReadTools() {
		want := "\n  " + def.Path + ":"
		if !strings.Contains(body, want) {
			t.Fatalf("%s points at %s, but that path is not declared in contracts/openapi/app-api.yaml", def.Name, def.Path)
		}
	}
}

func TestAPIReadToolsCallCanonicalUpstreamPaths(t *testing.T) {
	tests := []struct {
		name      string
		tool      string
		args      string
		wantPath  string
		wantQuery map[string]string
		response  map[string]any
	}{
		{
			name:     "action center",
			tool:     "get_action_center",
			args:     `{"category":"vaccination","work_state":"blocked","limit":25}`,
			wantPath: "/action-center/obligations",
			wantQuery: map[string]string{
				"category":   "vaccination",
				"work_state": "blocked",
				"limit":      "25",
			},
			response: map[string]any{"items": []map[string]any{{"title": "proof missing"}}},
		},
		{
			name:     "verification backlog",
			tool:     "get_verification_backlog",
			args:     `{"category":"vaccination_proof","status":"pending","business_date":"2026-08-15","limit":20}`,
			wantPath: "/verification/queue",
			wantQuery: map[string]string{
				"category":      "vaccination_proof",
				"status":        "pending",
				"business_date": "2026-08-15",
				"limit":         "20",
			},
			response: map[string]any{"items": []map[string]any{{"subject": "video"}}},
		},
		{
			name:     "feed today",
			tool:     "get_feed_today",
			args:     `{"park_id":"10000000-0000-4000-8000-000000000001","target_date":"2026-08-15","workflow":"normal","limit":10}`,
			wantPath: "/feed-direction/preview",
			wantQuery: map[string]string{
				"park_id":     "10000000-0000-4000-8000-000000000001",
				"target_date": "2026-08-15",
				"workflow":    "normal",
				"limit":       "10",
			},
			response: map[string]any{"items": []map[string]any{{"shed": "Gandhi", "status": "resolved"}}},
		},
		{
			name:     "procurement pipeline",
			tool:     "get_procurement_pipeline",
			args:     `{"status":"warmup","limit":5}`,
			wantPath: "/procurement/source-entry/loads",
			wantQuery: map[string]string{
				"status": "warmup",
				"limit":  "5",
			},
			response: map[string]any{"loads": []map[string]any{{"load_id": "load-1"}}},
		},
		{
			name:     "sales overview",
			tool:     "get_sales_overview",
			args:     `{"farm":"CBE"}`,
			wantPath: "/sales/overview",
			wantQuery: map[string]string{
				"farm": "CBE",
			},
			response: map[string]any{"summary": map[string]any{"revenue": 7398979, "animals_sold": 544}},
		},
		{
			name:     "sales deals",
			tool:     "get_sales_deals",
			args:     `{"farm":"CPT","limit":500,"offset":25}`,
			wantPath: "/sales/deals",
			wantQuery: map[string]string{
				"farm":   "CPT",
				"limit":  "100",
				"offset": "25",
			},
			response: map[string]any{"deals": []map[string]any{{"deal_id": "deal-1"}}},
		},
		{
			name:     "weighing progress",
			tool:     "get_weighing_progress",
			args:     `{"park_id":"10000000-0000-4000-8000-000000000001","limit":3}`,
			wantPath: "/weighing/campaigns",
			wantQuery: map[string]string{
				"park_id": "10000000-0000-4000-8000-000000000001",
				"limit":   "3",
			},
			response: map[string]any{"campaigns": []map[string]any{{"name": "week 1"}}},
		},
		{
			name:     "weighing growth",
			tool:     "get_weighing_growth_adg",
			args:     `{"park_id":"10000000-0000-4000-8000-000000000001","from":"2026-08-01","to":"2026-08-15","sex":"male"}`,
			wantPath: "/weighing/leadership/growth",
			wantQuery: map[string]string{
				"park_id": "10000000-0000-4000-8000-000000000001",
				"from":    "2026-08-01",
				"to":      "2026-08-15",
				"sex":     "male",
			},
			response: map[string]any{"summary": map[string]any{"average_daily_gain_g": 92}},
		},
		{
			name:     "weighing shed weights",
			tool:     "get_weighing_shed_weights",
			args:     `{"from":"2026-08-01","to":"2026-08-15"}`,
			wantPath: "/weighing/shed-weights",
			wantQuery: map[string]string{
				"from": "2026-08-01",
				"to":   "2026-08-15",
			},
			response: map[string]any{"rows": []map[string]any{{"shed": "Yashoda", "latest_average_weight_kg": 22.4}}},
		},
		{
			name:     "weighing process state",
			tool:     "get_weighing_process_state",
			args:     `{"campaign_id":"10000000-0000-4000-8000-000000000001","from":"2026-08-01","to":"2026-08-15"}`,
			wantPath: "/weighing/process-state",
			wantQuery: map[string]string{
				"campaign_id": "10000000-0000-4000-8000-000000000001",
				"from":        "2026-08-01",
				"to":          "2026-08-15",
			},
			response: map[string]any{"items": []map[string]any{{"state": "pending_verification"}}},
		},
		{
			name:     "weighing demographics",
			tool:     "get_weighing_weight_demographics",
			args:     `{"from":"2026-08-01","to":"2026-08-15","sex":"female"}`,
			wantPath: "/weighing/weight-demographics",
			wantQuery: map[string]string{
				"from": "2026-08-01",
				"to":   "2026-08-15",
				"sex":  "female",
			},
			response: map[string]any{"buckets": []map[string]any{{"breed": "Sirohi", "count": 12}}},
		},
		{
			name:     "counts summary",
			tool:     "get_counts_summary",
			args:     `{"park_id":"10000000-0000-4000-8000-000000000001","lifecycle_status":"alive","limit":10}`,
			wantPath: "/counts/breakdown",
			wantQuery: map[string]string{
				"park_id":          "10000000-0000-4000-8000-000000000001",
				"lifecycle_status": "alive",
				"limit":            "10",
			},
			response: map[string]any{"items": []map[string]any{{"park": "Channapatna", "total_count": 12}}, "total_count": 12},
		},
		{
			name:     "health work items",
			tool:     "get_health_work_items",
			args:     `{"age_band":"adult","date":"2026-08-15","status":"due","limit":20}`,
			wantPath: "/app/health/work-items",
			wantQuery: map[string]string{
				"age_band": "adult",
				"date":     "2026-08-15",
				"status":   "due",
				"limit":    "20",
			},
			response: map[string]any{"items": []map[string]any{{"disease_key": "fever", "status": "due"}}},
		},
		{
			name:     "workforce coverage",
			tool:     "get_workforce_coverage",
			args:     `{"scope_type":"shed","scope_id":"10000000-0000-4000-8000-000000000001","limit":20}`,
			wantPath: "/admin/roster/coverage",
			wantQuery: map[string]string{
				"scope_type": "shed",
				"scope_id":   "10000000-0000-4000-8000-000000000001",
				"limit":      "20",
			},
			response: map[string]any{"items": []map[string]any{{"scope_type": "shed", "backup_status": "missing"}}},
		},
		{
			name:     "milk feeding today",
			tool:     "get_milk_feeding_today",
			args:     `{"feeding_date":"2026-08-16","session_no":2,"limit":20}`,
			wantPath: "/app/counts/milk-feeding/tasks",
			wantQuery: map[string]string{
				"feeding_date": "2026-08-16",
				"session_no":   "2",
				"limit":        "20",
			},
			response: map[string]any{"items": []map[string]any{{"verification_status": "not_submitted", "head_count": 58}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.wantPath {
					t.Fatalf("path=%s want %s", r.URL.Path, tt.wantPath)
				}
				for key, want := range tt.wantQuery {
					if got := r.URL.Query().Get(key); got != want {
						t.Fatalf("query %s=%q want %q; raw=%s", key, got, want, r.URL.RawQuery)
					}
				}
				if r.Header.Get("Authorization") != "Bearer user-token" {
					t.Fatalf("Authorization not proxied: %q", r.Header.Get("Authorization"))
				}
				if r.Header.Get("X-Mesha-Actor-Email") != "aryaman@mesha.sg" {
					t.Fatalf("verified actor email not proxied: %q", r.Header.Get("X-Mesha-Actor-Email"))
				}
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer upstream.Close()

			s := newServer(config{
				UpstreamBaseURL: upstream.URL,
				UpstreamAskURL:  upstream.URL + "/ceo-ai/ask",
				MCPPath:         "/mcp",
				AllowedEmails:   mustEmailSet(t, "aryaman@mesha.sg"),
				TokenVerifier:   staticTokenVerifier{claims: platformauth.Claims{Email: "aryaman@mesha.sg", EmailVerified: boolPtr(true)}},
			}, upstream.Client(), nil)
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"api","method":"tools/call","params":{"name":"`+tt.tool+`","arguments":`+tt.args+`}}`))
			req.Header.Set("Authorization", "Bearer user-token")
			rec := httptest.NewRecorder()

			s.handleMCP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, tt.wantPath) || !strings.Contains(body, `"structuredContent"`) {
				t.Fatalf("body missing source/structured content: %s", body)
			}
		})
	}
}

func TestHealthTodayCombinesAdultKidAndMilkFeeding(t *testing.T) {
	seen := map[string]bool{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/health/work-items":
			ageBand := r.URL.Query().Get("age_band")
			seen[ageBand] = true
			if r.URL.Query().Get("date") != "2026-08-16" {
				t.Fatalf("date=%q", r.URL.Query().Get("date"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"age_band": ageBand}}})
		case "/app/counts/milk-feeding/tasks":
			seen["milk_feeding"] = true
			if r.URL.Query().Get("feeding_date") != "2026-08-16" {
				t.Fatalf("feeding_date=%q", r.URL.Query().Get("feeding_date"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"verification_status": "not_submitted", "head_count": 58}}})
		default:
			t.Fatalf("path=%s", r.URL.Path)
		}
	}))
	defer upstream.Close()

	s := newServer(config{
		UpstreamBaseURL: upstream.URL,
		UpstreamAskURL:  upstream.URL + "/ceo-ai/ask",
		MCPPath:         "/mcp",
		AllowedEmails:   mustEmailSet(t, "aryaman@mesha.sg"),
		TokenVerifier:   staticTokenVerifier{claims: platformauth.Claims{Email: "aryaman@mesha.sg", EmailVerified: boolPtr(true)}},
	}, upstream.Client(), nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"api","method":"tools/call","params":{"name":"get_health_today","arguments":{"date":"2026-08-16","limit":20}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !seen["adult"] || !seen["kid"] || !seen["milk_feeding"] {
		t.Fatalf("seen age bands=%v", seen)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"adult"`) || !strings.Contains(body, `"kid"`) || !strings.Contains(body, `"milk_feeding"`) || !strings.Contains(body, `"structuredContent"`) {
		t.Fatalf("body=%s", body)
	}
}

func TestAPIReadToolsRejectInvalidArgsBeforeUpstream(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"api","method":"tools/call","params":{"name":"get_feed_today","arguments":{"target_date":"15-08-2026"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if called {
		t.Fatal("upstream should not be called for invalid arguments")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "park_id_required") {
		t.Fatalf("body=%s", body)
	}
}

func TestSalesReadToolRejectsUnknownFarmBeforeUpstream(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"api","method":"tools/call","params":{"name":"get_sales_overview","arguments":{"farm":"MYSORE"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if called {
		t.Fatal("upstream should not be called for invalid sales farm")
	}
	if !strings.Contains(rec.Body.String(), "invalid_farm") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestWeighingReadToolRejectsUnknownSexBeforeUpstream(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"api","method":"tools/call","params":{"name":"get_weighing_growth_adg","arguments":{"sex":"mixed"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if called {
		t.Fatal("upstream should not be called for invalid weighing sex")
	}
	if !strings.Contains(rec.Body.String(), "invalid_sex") {
		t.Fatalf("body=%s", rec.Body.String())
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

func TestAskRoutesNaturalVaccinationScheduleQuestionToTypedScheduleTool(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vaccination/drive-assignments":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"source": "api",
				"rows": []map[string]any{{
					"plannedDate":    time.Now().In(time.FixedZone("IST", 5*60*60+30*60)).Format("2006-01-02"),
					"operatorName":   "Sagar Mahoor",
					"parkName":       "Channapatna",
					"physicalShed":   "Yashoda",
					"partitionLabel": "Part 4",
					"animals":        3,
					"dueAnimals":     3,
					"doneAnimals":    0,
					"vaccineNames":   []string{"Sheep Pox adult course dose 1"},
					"totalDoses":     3,
				}},
			})
		case "/vaccination/live-tracker":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"business_date": time.Now().In(time.FixedZone("IST", 5*60*60+30*60)).Format("2006-01-02"),
				"kpis":          map[string]any{},
			})
		default:
			t.Fatalf("ask_goatos schedule router should not call %s", r.URL.Path)
		}
	}))
	defer upstream.Close()

	s := newServer(config{
		UpstreamBaseURL: upstream.URL,
		UpstreamAskURL:  upstream.URL + "/ceo-ai/ask",
		MCPPath:         "/mcp",
		AllowedEmails:   mustEmailSet(t, "aryaman@mesha.sg"),
		TokenVerifier:   staticTokenVerifier{claims: platformauth.Claims{Email: "aryaman@mesha.sg", EmailVerified: boolPtr(true)}},
	}, upstream.Client(), nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"x","method":"tools/call","params":{"name":"ask_goatos","arguments":{"question":"What vaccination is scheduled today?"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Vaccination scheduled today", "Sagar Mahoor", "3 animals scheduled", "GET /vaccination/drive-assignments"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in body=%s", want, body)
		}
	}
}

func TestAskRoutesMessySalesQuestionToTypedReadTool(t *testing.T) {
	var seenPath, seenFarm string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenFarm = r.URL.Query().Get("farm")
		if r.URL.Path != "/sales/overview" {
			t.Fatalf("ask_goatos sales router called %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"summary": map[string]any{"revenue": 4115172, "animals_sold": 373}})
	}))
	defer upstream.Close()

	s := newServer(config{
		UpstreamBaseURL: upstream.URL,
		UpstreamAskURL:  upstream.URL + "/ceo-ai/ask",
		MCPPath:         "/mcp",
		AllowedEmails:   mustEmailSet(t, "aryaman@mesha.sg"),
		TokenVerifier:   staticTokenVerifier{claims: platformauth.Claims{Email: "aryaman@mesha.sg", EmailVerified: boolPtr(true)}},
	}, upstream.Client(), nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"x","method":"tools/call","params":{"name":"ask_goatos","arguments":{"question":"for CBE frm only wat are sales revenue and animls sold?"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if seenPath != "/sales/overview" || seenFarm != "CBE" {
		t.Fatalf("path=%s farm=%s", seenPath, seenFarm)
	}
	for _, want := range []string{"Applied filters: farm=CBE", "4115172"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("missing %q in body=%s", want, rec.Body.String())
		}
	}
}

func TestAskRoutesNegatedSalesDealMutationToReadOnlyLedger(t *testing.T) {
	var seenPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if r.URL.Path != "/sales/deals" {
			t.Fatalf("ask_goatos sales deals router called %s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "10" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"deals": []map[string]any{{"buyer_name": "Keethiraj", "status": "closed"}}})
	}))
	defer upstream.Close()

	s := newServer(config{
		UpstreamBaseURL: upstream.URL,
		UpstreamAskURL:  upstream.URL + "/ceo-ai/ask",
		MCPPath:         "/mcp",
		AllowedEmails:   mustEmailSet(t, "aryaman@mesha.sg"),
		TokenVerifier:   staticTokenVerifier{claims: platformauth.Claims{Email: "aryaman@mesha.sg", EmailVerified: boolPtr(true)}},
	}, upstream.Client(), nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"x","method":"tools/call","params":{"name":"ask_goatos","arguments":{"question":"show latst sales deals, dont create anything just read"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if seenPath != "/sales/deals" {
		t.Fatalf("path=%s", seenPath)
	}
	if !strings.Contains(rec.Body.String(), "Keethiraj") || !strings.Contains(rec.Body.String(), "read-only") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestAskRoutesMessyMaleWeighingQuestionWithFilterInAnswer(t *testing.T) {
	var seenPath, seenSex string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenSex = r.URL.Query().Get("sex")
		if r.URL.Path != "/weighing/leadership/growth" {
			t.Fatalf("ask_goatos weighing router called %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"headline": map[string]any{"average_adg_g_per_day": 197}})
	}))
	defer upstream.Close()

	s := newServer(config{
		UpstreamBaseURL: upstream.URL,
		UpstreamAskURL:  upstream.URL + "/ceo-ai/ask",
		MCPPath:         "/mcp",
		AllowedEmails:   mustEmailSet(t, "aryaman@mesha.sg"),
		TokenVerifier:   staticTokenVerifier{claims: platformauth.Claims{Email: "aryaman@mesha.sg", EmailVerified: boolPtr(true)}},
	}, upstream.Client(), nil)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"x","method":"tools/call","params":{"name":"ask_goatos","arguments":{"question":"male only weight gain numbers pls any spelling ok"}}}`))
	req.Header.Set("Authorization", "Bearer user-token")
	rec := httptest.NewRecorder()

	s.handleMCP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if seenPath != "/weighing/leadership/growth" || seenSex != "male" {
		t.Fatalf("path=%s sex=%s", seenPath, seenSex)
	}
	if !strings.Contains(rec.Body.String(), "Applied filters:") || !strings.Contains(rec.Body.String(), "sex=male") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestVaccinationTodayCallsLiveTrackerAndReturnsStructuredContent(t *testing.T) {
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
		switch r.URL.Path {
		case "/vaccination/drive-assignments":
			if r.URL.Query().Get("year") != "2026" || r.URL.Query().Get("month") != "8" {
				t.Fatalf("drive assignment query=%s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"source": "api",
				"rows": []map[string]any{{
					"plannedDate":     "2026-08-14",
					"operatorName":    "Darshan Talwar",
					"parkName":        "Channapatna",
					"physicalShed":    "Yashoda",
					"partitionLabel":  "Part 2",
					"animals":         4,
					"dueAnimals":      4,
					"doneAnimals":     0,
					"deferredAnimals": 0,
					"overdueAnimals":  0,
					"vaccineNames":    []string{"ET+TT"},
					"totalDoses":      4,
				}},
			})
		case "/vaccination/live-tracker":
			if r.URL.Query().Get("business_date") != "2026-08-14" || r.URL.Query().Get("status") != "pending" {
				t.Fatalf("query=%s", r.URL.RawQuery)
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
		default:
			t.Fatalf("path=%s", r.URL.Path)
		}
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
	for _, want := range []string{"Vaccination scheduled today (2026-08-14)", "Darshan Talwar", "4 animals scheduled", `"structuredContent"`, `"schedule_source_endpoint":"GET /vaccination/drive-assignments"`} {
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
	asReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	asRec := httptest.NewRecorder()
	s.handleAuthorizationServerMetadata(asRec, asReq)
	if asRec.Code != http.StatusOK {
		t.Fatalf("auth metadata status=%d body=%s", asRec.Code, asRec.Body.String())
	}
	if !strings.Contains(asRec.Body.String(), "refresh_token") || !strings.Contains(asRec.Body.String(), "offline_access") {
		t.Fatalf("auth metadata must advertise refresh-token support: %s", asRec.Body.String())
	}

	verifier := "codex-pkce-verifier"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	s.oauthCodes["code-1"] = oauthCode{
		Token:               "firebase-id-token",
		RefreshToken:        "firebase-refresh-token",
		ExpiresAt:           time.Now().Add(time.Minute),
		ClientID:            "goatos-mcp-client",
		RedirectURI:         "https://chatgpt.com/connector/oauth/goatos",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "code-1")
	form.Set("client_id", "goatos-mcp-client")
	form.Set("redirect_uri", "https://chatgpt.com/connector/oauth/goatos")
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
	if !strings.Contains(rec.Body.String(), `"refresh_token":"firebase-refresh-token"`) {
		t.Fatalf("body missing refresh token: %s", rec.Body.String())
	}
}

func TestOAuthRefreshTokenExchange(t *testing.T) {
	s := newServer(config{
		FirebaseAPIKey: "firebase-api-key",
		PublicURL:      "https://goatos-mcp-stg.example.com",
		UpstreamAskURL: "http://example.invalid/ceo-ai/ask",
		MCPPath:        "/mcp",
		AllowedEmails:  mustEmailSet(t, "ravi@mesha.sg"),
		TokenVerifier:  staticTokenVerifier{claims: platformauth.Claims{Email: "ravi@mesha.sg", EmailVerified: boolPtr(true)}},
	}, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "securetoken.googleapis.com" {
			t.Fatalf("unexpected host %s", r.URL.Host)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Fatalf("grant_type=%q", got)
		}
		if got := r.Form.Get("refresh_token"); got != "firebase-refresh-token" {
			t.Fatalf("refresh_token=%q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"id_token":"new-firebase-id-token","refresh_token":"new-firebase-refresh-token"}`)),
		}, nil
	})}, nil)

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", "firebase-refresh-token")
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	s.handleToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"access_token":"new-firebase-id-token"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"refresh_token":"new-firebase-refresh-token"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestOAuthRejectsUntrustedRedirectURI(t *testing.T) {
	s := newServer(config{
		PublicURL:      "https://goatos-mcp-stg.example.com",
		UpstreamAskURL: "http://example.invalid/ceo-ai/ask",
		MCPPath:        "/mcp",
	}, http.DefaultClient, nil)
	req := httptest.NewRequest(http.MethodGet, "/authorize?client_id=client&redirect_uri=https%3A%2F%2Fevil.example%2Fcb&state=abc", nil)
	rec := httptest.NewRecorder()

	s.handleAuthorize(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_redirect_uri") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestOAuthCodeExchangeRequiresOriginalRedirectURI(t *testing.T) {
	s := newServer(config{
		PublicURL:      "https://goatos-mcp-stg.example.com",
		UpstreamAskURL: "http://example.invalid/ceo-ai/ask",
		MCPPath:        "/mcp",
	}, http.DefaultClient, nil)
	s.oauthCodes["code-1"] = oauthCode{
		Token:       "firebase-id-token",
		ExpiresAt:   time.Now().Add(time.Minute),
		ClientID:    "goatos-mcp-client",
		RedirectURI: "https://chatgpt.com/connector/oauth/goatos",
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "code-1")
	form.Set("client_id", "goatos-mcp-client")
	form.Set("redirect_uri", "https://claude.ai/oauth/callback")
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	s.handleToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_grant") {
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
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
