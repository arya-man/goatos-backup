// Command mcp exposes the Goat OS leadership assistant as a small, sessionless
// Streamable-HTTP-style MCP facade. It deliberately does not implement business
// reads itself: tool calls proxy to the Mesha backend /ceo-ai/ask endpoint so
// existing auth, ceo_internal gating, tenant scope, planning, audit, Cube,
// Toolbox, SQL guard, and read-API coverage remain the single authority.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
)

const (
	defaultAddr             = ":8080"
	defaultMCPPath          = "/mcp"
	defaultAPIAskPath       = "/ceo-ai/ask"
	maxBodyBytes      int64 = 1 << 20
)

func main() {
	log := slog.Default()
	cfg, err := configFromEnv()
	if err != nil {
		log.Error("mcp_config_invalid", slog.Any("error", err))
		os.Exit(2)
	}

	srv := newServer(cfg, &http.Client{Timeout: cfg.UpstreamTimeout}, log)
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", srv.handleLive)
	mux.HandleFunc("/readyz", srv.handleReady)
	mux.HandleFunc(cfg.MCPPath, srv.handleMCP)

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Info("goatos_mcp_starting", slog.String("addr", cfg.Addr), slog.String("mcp_path", cfg.MCPPath), slog.String("upstream", cfg.UpstreamAskURL))
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("goatos_mcp_failed", slog.Any("error", err))
		os.Exit(1)
	}
}

type config struct {
	Addr            string
	MCPPath         string
	UpstreamAskURL  string
	UpstreamTimeout time.Duration
	AllowedEmails   authallow.EmailSet
	TokenVerifier   tokenVerifier
}

type tokenVerifier interface {
	Verify(token string) (platformauth.Claims, error)
}

func configFromEnv() (config, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("MESHA_MCP_UPSTREAM_URL")), "/")
	ask := strings.TrimSpace(os.Getenv("MESHA_MCP_UPSTREAM_ASK_URL"))
	if ask == "" && base != "" {
		ask = base + defaultAPIAskPath
	}
	if ask == "" {
		return config{}, errors.New("MESHA_MCP_UPSTREAM_URL or MESHA_MCP_UPSTREAM_ASK_URL is required")
	}
	timeout := envDuration("MESHA_MCP_UPSTREAM_TIMEOUT", 25*time.Second)
	allowed, err := parseAllowedEmails(os.Getenv("MESHA_MCP_ALLOWED_EMAILS"))
	if err != nil {
		return config{}, err
	}
	verifier, err := tokenVerifierFromEnv()
	if err != nil {
		return config{}, err
	}
	return config{
		Addr:            envOr("PORT_ADDR", envOr("GOATOS_HTTP_ADDR", defaultAddr)),
		MCPPath:         envOr("MESHA_MCP_PATH", defaultMCPPath),
		UpstreamAskURL:  ask,
		UpstreamTimeout: timeout,
		AllowedEmails:   allowed,
		TokenVerifier:   verifier,
	}, nil
}

func tokenVerifierFromEnv() (tokenVerifier, error) {
	mode := strings.ToLower(strings.TrimSpace(envOr("GOATOS_AUTH_MODE", "jwks")))
	switch mode {
	case "jwks":
		jwksURL := strings.TrimSpace(os.Getenv("GOATOS_AUTH_JWKS_URL"))
		issuer := strings.TrimSpace(os.Getenv("GOATOS_AUTH_ISSUER"))
		audience := strings.TrimSpace(os.Getenv("GOATOS_AUTH_AUDIENCE"))
		if jwksURL == "" || issuer == "" || audience == "" {
			return nil, errors.New("GOATOS_AUTH_JWKS_URL, GOATOS_AUTH_ISSUER, and GOATOS_AUTH_AUDIENCE are required for MCP bearer verification")
		}
		return platformauth.NewJWKSVerifier(platformauth.JWKSConfig{
			JWKSURL:     jwksURL,
			Issuer:      issuer,
			Audience:    audience,
			AllowedAlgs: authAllowedAlgsFromEnv(),
			ClockSkew:   envDuration("GOATOS_AUTH_CLOCK_SKEW", 0),
			MaxTTL:      envDuration("GOATOS_AUTH_MAX_TOKEN_TTL", 0),
			CacheTTL:    envDuration("GOATOS_AUTH_JWKS_CACHE_TTL", 0),
		})
	default:
		return nil, fmt.Errorf("unsupported GOATOS_AUTH_MODE for MCP: %q", mode)
	}
}

func authAllowedAlgsFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("GOATOS_AUTH_ALLOWED_ALGS"))
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if alg := strings.TrimSpace(part); alg != "" {
			out = append(out, alg)
		}
	}
	return out
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if key == "PORT_ADDR" {
			if _, err := strconv.Atoi(v); err == nil {
				return ":" + v
			}
		}
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

func parseAllowedEmails(raw string) (authallow.EmailSet, error) {
	var emails []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		email := strings.ToLower(strings.TrimSpace(part))
		if email == "" {
			continue
		}
		emails = append(emails, email)
	}
	return authallow.NewEmailSet(emails)
}

type server struct {
	cfg    config
	client *http.Client
	log    *slog.Logger
}

func newServer(cfg config, client *http.Client, log *slog.Logger) *server {
	if log == nil {
		log = slog.Default()
	}
	return &server{cfg: cfg, client: client, log: log}
}

func (s *server) handleLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "goatos-mcp"})
}

func (s *server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.UpstreamAskURL == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "missing_upstream"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, rpcError(nil, -32000, "method_not_allowed"))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, rpcError(nil, -32700, "request_too_large"))
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, rpcError(nil, -32700, "parse_error"))
		return
	}
	if req.JSONRPC != "2.0" {
		writeJSON(w, http.StatusBadRequest, rpcError(req.ID, -32600, "invalid_jsonrpc"))
		return
	}

	switch req.Method {
	case "initialize":
		writeJSON(w, http.StatusOK, rpcResult(req.ID, initializeResult()))
	case "notifications/initialized":
		if req.ID == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, rpcResult(req.ID, map[string]any{}))
	case "tools/list":
		writeJSON(w, http.StatusOK, rpcResult(req.ID, map[string]any{"tools": tools()}))
	case "tools/call":
		result, code, msg := s.callTool(r.Context(), r, req.Params)
		if msg != "" {
			writeJSON(w, http.StatusOK, rpcError(req.ID, code, msg))
			return
		}
		writeJSON(w, http.StatusOK, rpcResult(req.ID, result))
	default:
		writeJSON(w, http.StatusOK, rpcError(req.ID, -32601, "method_not_found"))
	}
}

type rpcRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

func rpcResult(id any, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func rpcError(id any, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}

func initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": "2025-06-18",
		"serverInfo": map[string]any{
			"name":    "goatos-mcp",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
	}
}

func tools() []map[string]any {
	return []map[string]any{
		{
			"name":        "ask_goatos",
			"description": "Ask the Goat OS leadership assistant a natural-language, read-only business question. Use this for Mesha/Goat OS operations, animals, vaccination, feed, procurement, workforce, verification, audit, inventory, SOP, and exception questions.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question":        map[string]any{"type": "string", "description": "The user's Mesha/Goat OS question."},
					"conversation_id": map[string]any{"type": "string", "description": "Optional conversation id returned by a previous answer."},
				},
				"required": []string{"question"},
			},
		},
		{
			"name":        "list_goatos_capabilities",
			"description": "List what the Goat OS MCP connector can answer and how access is controlled.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "goatos_mcp_health",
			"description": "Check whether the Goat OS MCP connector is configured and reachable.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

func (s *server) callTool(ctx context.Context, r *http.Request, raw json.RawMessage) (map[string]any, int, string) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, -32602, "invalid_tool_params"
	}
	switch params.Name {
	case "ask_goatos":
		return s.askGoatOS(ctx, r, params.Arguments)
	case "list_goatos_capabilities":
		return textToolResult("Goat OS MCP exposes the existing Mesha leadership assistant as read-only tools. It can answer CEO-level operational questions across covered Goat OS read APIs, Cube metrics, curated MCP Toolbox views, and validated SQL fallback. Access is restricted to the configured CEO allowlist and the upstream Goat OS backend remains the authority for tenant scope, ceo_internal role, auditing, and safety."), 0, ""
	case "goatos_mcp_health":
		return textToolResult("Goat OS MCP is running. Upstream assistant endpoint: " + s.cfg.UpstreamAskURL), 0, ""
	default:
		return nil, -32602, "unknown_tool"
	}
}

func (s *server) askGoatOS(ctx context.Context, r *http.Request, raw json.RawMessage) (map[string]any, int, string) {
	var args struct {
		Question       string `json:"question"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, -32602, "invalid_ask_arguments"
	}
	question := strings.TrimSpace(args.Question)
	if question == "" {
		return nil, -32602, "question_required"
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if authz == "" {
		return nil, -32001, "missing_authorization_bearer"
	}
	token, ok := strings.CutPrefix(authz, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		return nil, -32001, "invalid_authorization_bearer"
	}
	if s.cfg.TokenVerifier == nil {
		return nil, -32603, "token_verifier_not_configured"
	}
	claims, err := s.cfg.TokenVerifier.Verify(strings.TrimSpace(token))
	if err != nil {
		s.log.Warn("goatos_mcp_bearer_verify_failed", slog.Any("error", err))
		return nil, -32001, "invalid_authorization_bearer"
	}
	email := normalizedEmail(claims.Email)
	if len(s.cfg.AllowedEmails) > 0 && !s.cfg.AllowedEmails.Allows(email, claims.EmailVerified) {
		return nil, -32001, "actor_email_not_allowed"
	}

	payload := map[string]any{
		"question":        question,
		"conversation_id": strings.TrimSpace(args.ConversationID),
		"stream":          false,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.UpstreamAskURL, bytes.NewReader(body))
	if err != nil {
		s.log.Error("goatos_mcp_upstream_request_build_failed", slog.Any("error", err))
		return nil, -32603, "upstream_request_build_failed"
	}
	req.Header.Set("Authorization", authz)
	req.Header.Set("Content-Type", "application/json")
	if email != "" {
		req.Header.Set("X-Mesha-Actor-Email", email)
	}
	if tenant := strings.TrimSpace(r.Header.Get("X-GoatOS-Tenant-ID")); tenant != "" {
		req.Header.Set("X-GoatOS-Tenant-ID", tenant)
	}
	if trace := strings.TrimSpace(r.Header.Get("X-Request-ID")); trace != "" {
		req.Header.Set("X-Request-ID", trace)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		s.log.Warn("goatos_mcp_upstream_error", slog.Any("error", err))
		return nil, -32603, "upstream_unreachable"
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.log.Warn("goatos_mcp_upstream_non_2xx", slog.Int("status", resp.StatusCode), slog.String("body", string(respBody)))
		return nil, -32002, "upstream_assistant_rejected_request"
	}
	var ans struct {
		Answer         string `json:"answer"`
		Source         string `json:"source"`
		Mode           string `json:"mode"`
		RequestID      string `json:"request_id"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal(respBody, &ans); err != nil {
		return nil, -32603, "upstream_invalid_answer"
	}
	text := ans.Answer
	if ans.Source != "" {
		text += "\n\nSource: " + ans.Source
	}
	if ans.Mode != "" {
		text += "\nMode: " + ans.Mode
	}
	if ans.ConversationID != "" {
		text += "\nConversation: " + ans.ConversationID
	}
	if ans.RequestID != "" {
		text += "\nRequest: " + ans.RequestID
	}
	return textToolResult(text), 0, ""
}

func normalizedEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func textToolResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
