// Command mcp exposes the Goat OS leadership assistant as a small, sessionless
// Streamable-HTTP-style MCP facade. It deliberately keeps business reads behind
// existing Goat OS backend APIs so auth, ceo_internal gating, tenant scope,
// audit, Cube, Toolbox, SQL guard, and read-API coverage remain the authority.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
)

const (
	defaultAddr             = ":8080"
	defaultMCPPath          = "/mcp"
	defaultAPIAskPath       = "/ceo-ai/ask"
	maxBodyBytes      int64 = 1 << 20
	firebaseTokenTTL        = 3600
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
	mux.HandleFunc("/.well-known/oauth-protected-resource", srv.handleProtectedResourceMetadata)
	mux.HandleFunc("/.well-known/oauth-authorization-server", srv.handleAuthorizationServerMetadata)
	mux.HandleFunc("/register", srv.handleRegister)
	mux.HandleFunc("/authorize", srv.handleAuthorize)
	mux.HandleFunc("/token", srv.handleToken)
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
	PublicURL       string
	TenantID        string
	DefaultParkID   string
	UpstreamBaseURL string
	UpstreamAskURL  string
	UpstreamTimeout time.Duration
	AllowedEmails   authallow.EmailSet
	TokenVerifier   tokenVerifier
	FirebaseAPIKey  string
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
		PublicURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("MESHA_MCP_PUBLIC_URL")), "/"),
		TenantID:        strings.TrimSpace(os.Getenv("MESHA_MCP_TENANT_ID")),
		DefaultParkID:   firstNonEmpty(os.Getenv("MESHA_MCP_DEFAULT_PARK_ID"), os.Getenv("GOATOS_E2E_PARK_ID")),
		UpstreamBaseURL: upstreamBaseURL(base, ask),
		UpstreamAskURL:  ask,
		UpstreamTimeout: timeout,
		AllowedEmails:   allowed,
		TokenVerifier:   verifier,
		FirebaseAPIKey:  firebaseAPIKeyFromEnv(),
	}, nil
}

func upstreamBaseURL(base, ask string) string {
	if base != "" {
		return base
	}
	trimmedAsk := strings.TrimRight(ask, "/")
	if !strings.HasSuffix(trimmedAsk, defaultAPIAskPath) {
		return ""
	}
	return strings.TrimSuffix(trimmedAsk, defaultAPIAskPath)
}

func firebaseAPIKeyFromEnv() string {
	if key := strings.TrimSpace(os.Getenv("GOATOS_FIREBASE_WEB_API_KEY")); key != "" {
		return key
	}
	raw := strings.TrimSpace(os.Getenv("GOATOS_FIREBASE_WEB_CONFIG"))
	if raw == "" {
		return ""
	}
	var cfg struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.APIKey)
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
	case "bearer", "hs256":
		issuer := strings.TrimSpace(os.Getenv("GOATOS_AUTH_ISSUER"))
		audience := strings.TrimSpace(os.Getenv("GOATOS_AUTH_AUDIENCE"))
		secret := []byte(os.Getenv("GOATOS_AUTH_HS256_SECRET"))
		if issuer == "" || audience == "" || len(secret) == 0 {
			return nil, errors.New("GOATOS_AUTH_ISSUER, GOATOS_AUTH_AUDIENCE, and GOATOS_AUTH_HS256_SECRET are required for MCP HS256 bearer verification")
		}
		return platformauth.NewHS256Verifier(platformauth.Config{
			Issuer:   issuer,
			Audience: audience,
			Secret:   secret,
			MaxTTL:   envDuration("GOATOS_AUTH_MAX_TOKEN_TTL", 0),
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
	cfg        config
	client     *http.Client
	log        *slog.Logger
	oauthCodes map[string]oauthCode
	oauthMu    sync.Mutex
}

func newServer(cfg config, client *http.Client, log *slog.Logger) *server {
	if log == nil {
		log = slog.Default()
	}
	return &server{cfg: cfg, client: client, log: log, oauthCodes: map[string]oauthCode{}}
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

func (s *server) publicURL(r *http.Request) string {
	if s.cfg.PublicURL != "" {
		return s.cfg.PublicURL
	}
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		scheme = strings.Split(proto, ",")[0]
	}
	return scheme + "://" + r.Host
}

func (s *server) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.publicURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base,
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{"goatos.read"},
		"resource_name":            "Mesha Goat OS leadership assistant",
		"resource_documentation":   "https://github.com/vgoats/goatos/blob/main/docs/ceo-ai/external-mcp-integration.md",
	})
}

func (s *server) handleAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.publicURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/authorize",
		"token_endpoint":                        base + "/token",
		"registration_endpoint":                 base + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256", "plain"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{"goatos.read", "offline_access"},
	})
}

func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  "goatos-mcp-" + randomString(12),
		"client_id_issued_at":        time.Now().Unix(),
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

func (s *server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.renderLogin(w, r, "")
	case http.MethodPost:
		s.completeLogin(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) renderLogin(w http.ResponseWriter, r *http.Request, message string) {
	q := r.URL.Query()
	if strings.TrimSpace(q.Get("redirect_uri")) == "" || strings.TrimSpace(q.Get("state")) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_redirect_uri_or_state"})
		return
	}
	if !allowedOAuthRedirectURI(q.Get("redirect_uri")) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_redirect_uri"})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	msg := ""
	if message != "" {
		msg = `<p class="error">` + html.EscapeString(message) + `</p>`
	}
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Connect Mesha Goat OS</title>
<style>
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#07110d;color:#eef7ef;margin:0;display:grid;min-height:100vh;place-items:center}
main{width:min(420px,calc(100vw - 32px));border:1px solid #244333;border-radius:14px;padding:28px;background:#0c1b14}
h1{font-size:24px;margin:0 0 8px}p{color:#b9cabd;line-height:1.45}.error{color:#ffb4a8}
label{display:block;margin:16px 0 6px;color:#d6e8d9}input{width:100%%;box-sizing:border-box;border:1px solid #31533d;border-radius:8px;background:#07110d;color:#fff;padding:12px;font-size:16px}
button{margin-top:20px;width:100%%;border:0;border-radius:8px;background:#7bd957;color:#07110d;font-weight:700;padding:12px;font-size:16px}
small{display:block;color:#87a38e;margin-top:14px}
</style></head><body><main>
<h1>Connect Mesha Goat OS</h1>
<p>Sign in with your approved leadership account. After this, ChatGPT, Claude, or Codex can answer Goat OS questions in plain English.</p>
%s
<form method="post" action="/authorize">
<input type="hidden" name="client_id" value="%s">
<input type="hidden" name="redirect_uri" value="%s">
<input type="hidden" name="state" value="%s">
<input type="hidden" name="code_challenge" value="%s">
<input type="hidden" name="code_challenge_method" value="%s">
<input type="hidden" name="scope" value="%s">
<label>Email</label><input name="email" type="email" autocomplete="username" required autofocus>
<label>Password</label><input name="password" type="password" autocomplete="current-password" required>
<button type="submit">Connect Goat OS</button>
<small>Allowed: ravi, manohar, manju, aryaman at mesha.sg. Read-only leadership access.</small>
</form></main></body></html>`,
		msg,
		html.EscapeString(q.Get("client_id")),
		html.EscapeString(q.Get("redirect_uri")),
		html.EscapeString(q.Get("state")),
		html.EscapeString(q.Get("code_challenge")),
		html.EscapeString(q.Get("code_challenge_method")),
		html.EscapeString(q.Get("scope")),
	)
}

func (s *server) completeLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_form"})
		return
	}
	redirectURI := strings.TrimSpace(r.Form.Get("redirect_uri"))
	state := strings.TrimSpace(r.Form.Get("state"))
	if redirectURI == "" || state == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_redirect_uri_or_state"})
		return
	}
	if !allowedOAuthRedirectURI(redirectURI) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_redirect_uri"})
		return
	}
	signIn, claims, err := s.signInWithFirebase(r.Context(), strings.TrimSpace(r.Form.Get("email")), r.Form.Get("password"))
	if err != nil {
		s.log.Warn("goatos_mcp_login_failed", slog.Any("error", err))
		s.renderLogin(w, r, "Login failed. Check the Goat OS staging email and password.")
		return
	}
	email := normalizedEmail(claims.Email)
	if len(s.cfg.AllowedEmails) > 0 && !s.cfg.AllowedEmails.Allows(email, claims.EmailVerified) {
		s.renderLogin(w, r, "This account is not allowed to connect Goat OS MCP.")
		return
	}
	code := randomString(32)
	s.oauthMu.Lock()
	s.oauthCodes[code] = oauthCode{
		Token:               signIn.IDToken,
		RefreshToken:        signIn.RefreshToken,
		Email:               email,
		ExpiresAt:           time.Now().Add(5 * time.Minute),
		ClientID:            strings.TrimSpace(r.Form.Get("client_id")),
		RedirectURI:         redirectURI,
		CodeChallenge:       strings.TrimSpace(r.Form.Get("code_challenge")),
		CodeChallengeMethod: strings.TrimSpace(r.Form.Get("code_challenge_method")),
	}
	s.oauthMu.Unlock()
	u, err := url.Parse(redirectURI)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_redirect_uri"})
		return
	}
	q := u.Query()
	q.Set("code", code)
	q.Set("state", state)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

type oauthCode struct {
	Token               string
	RefreshToken        string
	Email               string
	ExpiresAt           time.Time
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
}

func (s *server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if strings.TrimSpace(r.Form.Get("grant_type")) == "refresh_token" {
		s.handleRefreshToken(w, r)
		return
	}
	code := strings.TrimSpace(r.Form.Get("code"))
	s.oauthMu.Lock()
	entry, ok := s.oauthCodes[code]
	delete(s.oauthCodes, code)
	s.oauthMu.Unlock()
	if !ok || time.Now().After(entry.ExpiresAt) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	if !entry.clientAllows(strings.TrimSpace(r.Form.Get("client_id"))) || !entry.redirectAllows(strings.TrimSpace(r.Form.Get("redirect_uri"))) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	if !entry.pkceAllows(strings.TrimSpace(r.Form.Get("code_verifier"))) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  entry.Token,
		"refresh_token": entry.RefreshToken,
		"token_type":    "Bearer",
		"expires_in":    firebaseTokenTTL,
		"scope":         "goatos.read",
	})
}

func (s *server) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	refreshToken := strings.TrimSpace(r.Form.Get("refresh_token"))
	if refreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	refreshed, claims, err := s.refreshFirebaseToken(r.Context(), refreshToken)
	if err != nil {
		s.log.Warn("goatos_mcp_refresh_failed", slog.Any("error", err))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	email := normalizedEmail(claims.Email)
	if len(s.cfg.AllowedEmails) > 0 && !s.cfg.AllowedEmails.Allows(email, claims.EmailVerified) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	out := map[string]any{
		"access_token": refreshed.IDToken,
		"token_type":   "Bearer",
		"expires_in":   firebaseTokenTTL,
		"scope":        "goatos.read",
	}
	if strings.TrimSpace(refreshed.RefreshToken) != "" {
		out["refresh_token"] = refreshed.RefreshToken
	}
	writeJSON(w, http.StatusOK, out)
}

func allowedOAuthRedirectURI(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch strings.ToLower(u.Scheme) {
	case "https":
		return host == "chatgpt.com" || strings.HasSuffix(host, ".chatgpt.com") ||
			host == "claude.ai" || strings.HasSuffix(host, ".claude.ai")
	case "http":
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	default:
		return false
	}
}

func (c oauthCode) clientAllows(clientID string) bool {
	if c.ClientID == "" || clientID == "" {
		return true
	}
	return clientID == c.ClientID
}

func (c oauthCode) redirectAllows(redirectURI string) bool {
	if c.RedirectURI == "" {
		return true
	}
	return redirectURI == c.RedirectURI
}

func (c oauthCode) pkceAllows(verifier string) bool {
	if c.CodeChallenge == "" {
		return true
	}
	if verifier == "" {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(c.CodeChallengeMethod)) {
	case "", "PLAIN":
		return verifier == c.CodeChallenge
	case "S256":
		sum := sha256.Sum256([]byte(verifier))
		return base64.RawURLEncoding.EncodeToString(sum[:]) == c.CodeChallenge
	default:
		return false
	}
}

type firebaseAuthTokens struct {
	IDToken      string
	RefreshToken string
}

func (s *server) signInWithFirebase(ctx context.Context, email, password string) (firebaseAuthTokens, platformauth.Claims, error) {
	if s.cfg.FirebaseAPIKey == "" {
		return firebaseAuthTokens{}, platformauth.Claims{}, errors.New("firebase_api_key_missing")
	}
	payload, _ := json.Marshal(map[string]any{
		"email":             email,
		"password":          password,
		"returnSecureToken": true,
	})
	endpoint := "https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=" + url.QueryEscape(s.cfg.FirebaseAPIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return firebaseAuthTokens{}, platformauth.Claims{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return firebaseAuthTokens{}, platformauth.Claims{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return firebaseAuthTokens{}, platformauth.Claims{}, fmt.Errorf("firebase_sign_in_status_%d", resp.StatusCode)
	}
	var out struct {
		IDToken      string `json:"idToken"`
		RefreshToken string `json:"refreshToken"`
		Email        string `json:"email"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return firebaseAuthTokens{}, platformauth.Claims{}, err
	}
	if strings.TrimSpace(out.IDToken) == "" {
		return firebaseAuthTokens{}, platformauth.Claims{}, errors.New("firebase_id_token_missing")
	}
	if strings.TrimSpace(out.RefreshToken) == "" {
		return firebaseAuthTokens{}, platformauth.Claims{}, errors.New("firebase_refresh_token_missing")
	}
	claims, err := s.cfg.TokenVerifier.Verify(out.IDToken)
	if err != nil {
		return firebaseAuthTokens{}, platformauth.Claims{}, err
	}
	return firebaseAuthTokens{IDToken: out.IDToken, RefreshToken: out.RefreshToken}, claims, nil
}

func (s *server) refreshFirebaseToken(ctx context.Context, refreshToken string) (firebaseAuthTokens, platformauth.Claims, error) {
	if s.cfg.FirebaseAPIKey == "" {
		return firebaseAuthTokens{}, platformauth.Claims{}, errors.New("firebase_api_key_missing")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	endpoint := "https://securetoken.googleapis.com/v1/token?key=" + url.QueryEscape(s.cfg.FirebaseAPIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return firebaseAuthTokens{}, platformauth.Claims{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return firebaseAuthTokens{}, platformauth.Claims{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return firebaseAuthTokens{}, platformauth.Claims{}, fmt.Errorf("firebase_refresh_status_%d", resp.StatusCode)
	}
	var out struct {
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return firebaseAuthTokens{}, platformauth.Claims{}, err
	}
	if strings.TrimSpace(out.IDToken) == "" {
		return firebaseAuthTokens{}, platformauth.Claims{}, errors.New("firebase_id_token_missing")
	}
	claims, err := s.cfg.TokenVerifier.Verify(out.IDToken)
	if err != nil {
		return firebaseAuthTokens{}, platformauth.Claims{}, err
	}
	if strings.TrimSpace(out.RefreshToken) == "" {
		out.RefreshToken = refreshToken
	}
	return firebaseAuthTokens{IDToken: out.IDToken, RefreshToken: out.RefreshToken}, claims, nil
}

func randomString(bytesLen int) string {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func (s *server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, rpcError(nil, -32000, "method_not_allowed"))
		return
	}
	if s.cfg.TokenVerifier != nil && strings.TrimSpace(r.Header.Get("Authorization")) == "" {
		s.writeUnauthorized(w, r)
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

	isNotification := req.ID == nil
	switch req.Method {
	case "initialize":
		if isNotification {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, rpcResult(req.ID, initializeResult()))
	case "notifications/initialized":
		w.WriteHeader(http.StatusNoContent)
	case "tools/list":
		if isNotification {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, rpcResult(req.ID, map[string]any{"tools": tools()}))
	case "tools/call":
		if isNotification {
			_, _, _ = s.callTool(r.Context(), r, req.Params)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		result, code, msg := s.callTool(r.Context(), r, req.Params)
		if msg != "" {
			writeJSON(w, http.StatusOK, rpcError(req.ID, code, msg))
			return
		}
		writeJSON(w, http.StatusOK, rpcResult(req.ID, result))
	default:
		if isNotification {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, rpcError(req.ID, -32601, "method_not_found"))
	}
}

func (s *server) writeUnauthorized(w http.ResponseWriter, r *http.Request) {
	base := s.publicURL(r)
	w.Header().Set("WWW-Authenticate", `Bearer realm="goatos-mcp", resource_metadata="`+base+`/.well-known/oauth-protected-resource"`)
	writeJSON(w, http.StatusUnauthorized, map[string]any{
		"error":             "authorization_required",
		"resource_metadata": base + "/.well-known/oauth-protected-resource",
	})
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
	out := []map[string]any{
		{
			"name":        "ask_goatos",
			"description": "Ask the Goat OS leadership assistant a natural-language, read-only business question. Use this only when no specific Mesha MCP tool fits. Prefer typed tools for exact operations answers: vaccination, action center, verification, feed, procurement, sales, and weighing.",
			"annotations": readOnlyToolAnnotations(),
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
			"name":        "get_vaccination_today",
			"description": "Get the exact vaccination work scheduled today. Use this for natural CEO questions like 'what vaccination is scheduled today?' It reads the operator drive schedule first, then includes live proof/progress when available; do not use backlog/action-center rows for this scheduled-work answer.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"business_date":   map[string]any{"type": "string", "description": "Optional Goat OS business date in YYYY-MM-DD. Defaults to today."},
					"park_id":         map[string]any{"type": "string", "description": "Optional park UUID; backend RBAC still clamps scope."},
					"shed_id":         map[string]any{"type": "string", "description": "Optional shed UUID."},
					"partition_label": map[string]any{"type": "string", "description": "Optional partition label."},
					"operator_id":     map[string]any{"type": "string", "description": "Optional operator workforce member UUID."},
					"vaccine_code":    map[string]any{"type": "string", "description": "Optional vaccine family code such as goat_pox, et_tt, ppr, blue_tongue, sheep_pox."},
					"status":          map[string]any{"type": "string", "enum": []string{"active", "done", "pending", "review"}, "description": "Optional live tracker status filter."},
				},
			},
		},
		{
			"name":        "get_health_today",
			"description": "Get adult health, kids health, and kid milk-feeding health tasks for one day in a single call. Use this for broad CEO health questions so clinical and milk-feeding work are not accidentally reported as a partial answer.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": commonReadProperties("date", "status", "disease_key", "park_id", "shed_id", "session", "limit"),
			},
		},
		{
			"name":        "list_goatos_capabilities",
			"description": "List what the Goat OS MCP connector can answer and how access is controlled.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			"name":        "goatos_mcp_health",
			"description": "Check whether the Goat OS MCP connector is configured and reachable.",
			"annotations": readOnlyToolAnnotations(),
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
	for _, def := range apiReadTools() {
		out = append(out, def.mcpTool())
	}
	return out
}

func readOnlyToolAnnotations() map[string]any {
	return map[string]any{
		"title":           "Read-only Goat OS data",
		"readOnlyHint":    true,
		"destructiveHint": false,
		"openWorldHint":   false,
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
	case "get_vaccination_today":
		return s.getVaccinationToday(ctx, r, params.Arguments)
	case "get_health_today":
		return s.getHealthToday(ctx, r, params.Arguments)
	case "list_goatos_capabilities":
		return textToolResult("Goat OS MCP exposes read-only leadership tools. Use typed tools for exact operational answers: get_vaccination_today, get_action_center, get_verification_backlog, get_feed_today, get_procurement_pipeline, get_sales_overview, get_sales_deals, get_counts_summary, get_health_today, get_health_work_items, get_milk_feeding_today, get_workforce_coverage, get_weighing_progress, get_weighing_growth_adg, get_weighing_shed_weights, get_weighing_process_state, and get_weighing_weight_demographics. Use ask_goatos only as fallback for broader covered questions. Access is restricted to the configured CEO allowlist and the upstream Goat OS backend remains the authority for tenant scope, ceo_internal role, auditing, and safety."), 0, ""
	case "goatos_mcp_health":
		return textToolResult("Goat OS MCP is running. Upstream assistant endpoint: " + s.cfg.UpstreamAskURL), 0, ""
	default:
		if def, ok := apiReadToolByName(params.Name); ok {
			return s.getAPIReadTool(ctx, r, params.Arguments, def)
		}
		return nil, -32602, "unknown_tool"
	}
}

type apiReadTool struct {
	Name        string
	Description string
	Path        string
	Source      string
	Properties  map[string]any
	Required    []string
	BuildQuery  func(apiReadArgs) (url.Values, error)
}

func (t apiReadTool) mcpTool() map[string]any {
	return map[string]any{
		"name":        t.Name,
		"description": t.Description,
		"annotations": readOnlyToolAnnotations(),
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": t.Properties,
			"required":   t.Required,
		},
	}
}

func apiReadTools() []apiReadTool {
	return []apiReadTool{
		{
			Name:        "get_action_center",
			Description: "Get canonical leadership Action Center process-integrity rows across vaccination and feed direction. Use this for blocked/overdue/at-risk work; it is not the same grain as scheduled administrations or verification verdicts.",
			Path:        "/action-center/obligations",
			Source:      "GET /action-center/obligations",
			Properties:  commonReadProperties("category", "park_id", "shed_id", "work_state", "severity", "owner_id", "as_of", "due_after", "due_before", "cursor", "limit"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addEnum(q, "category", a.Category, "vaccination", "feed_direction"); err != nil {
					return nil, err
				}
				if err := addUUID(q, "park_id", a.ParkID); err != nil {
					return nil, err
				}
				if err := addUUID(q, "shed_id", a.ShedID); err != nil {
					return nil, err
				}
				if err := addEnum(q, "work_state", a.WorkState, "overdue", "due", "blocked", "rejected", "proof_pending", "verification_pending", "in_progress", "scheduled", "deferred", "completed", "missed", "ok"); err != nil {
					return nil, err
				}
				if err := addStringMax(q, "severity", a.Severity, 80); err != nil {
					return nil, err
				}
				if err := addUUID(q, "owner_id", a.OwnerID); err != nil {
					return nil, err
				}
				if err := addRFC3339(q, "as_of", a.AsOf); err != nil {
					return nil, err
				}
				if err := addRFC3339(q, "due_after", a.DueAfter); err != nil {
					return nil, err
				}
				if err := addRFC3339(q, "due_before", a.DueBefore); err != nil {
					return nil, err
				}
				addOpaque(q, "cursor", a.Cursor)
				addLimit(q, a.Limit, 500)
				return q, nil
			},
		},
		{
			Name:        "get_verification_backlog",
			Description: "Get the canonical verification evidence queue. Use this for pending/approved/rejected proofs and media review backlog. Do not treat pending verification as completed operational work.",
			Path:        "/verification/queue",
			Source:      "GET /verification/queue",
			Properties:  commonReadProperties("category", "vertical", "module", "nav_module", "status", "business_date", "business_date_from", "business_date_to", "missed", "park_id", "shed_id", "cursor", "limit"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				for _, item := range []struct{ name, raw string }{{"category", a.Category}, {"vertical", a.Vertical}, {"module", a.Module}, {"nav_module", a.NavModule}} {
					if err := addStringMax(q, item.name, item.raw, 120); err != nil {
						return nil, err
					}
				}
				if err := addEnum(q, "status", a.Status, "all", "pending", "approved", "rejected"); err != nil {
					return nil, err
				}
				for _, item := range []struct{ name, raw string }{{"business_date", a.BusinessDate}, {"business_date_from", a.BusinessDateFrom}, {"business_date_to", a.BusinessDateTo}} {
					if err := addDate(q, item.name, item.raw); err != nil {
						return nil, err
					}
				}
				if err := addBool(q, "missed", a.Missed); err != nil {
					return nil, err
				}
				if err := addUUID(q, "park_id", a.ParkID); err != nil {
					return nil, err
				}
				if err := addStringMax(q, "shed_id", a.ShedID, 120); err != nil {
					return nil, err
				}
				addOpaque(q, "cursor", a.Cursor)
				addLimit(q, a.Limit, 100)
				return q, nil
			},
		},
		{
			Name:        "get_feed_today",
			Description: "Get the frozen issued Feed Direction sheet for one park and feed day. Use this for today planned/needed feed quantities, blocked feed cells, ration/config gaps, and session/shed feed work. This does not prove feed was actually completed; feed actuals/adherence are not covered until the feed_adherence source ships. Blocked/null feed must not be treated as zero.",
			Path:        "/feed-direction/preview",
			Source:      "GET /feed-direction/preview",
			Properties:  commonReadProperties("park_id", "target_date", "shed_id", "session", "workflow", "draft", "limit", "offset"),
			Required:    []string{"park_id", "target_date"},
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addRequiredUUID(q, "park_id", a.ParkID); err != nil {
					return nil, err
				}
				if err := addRequiredDate(q, "target_date", firstNonEmpty(a.TargetDate, a.BusinessDate)); err != nil {
					return nil, err
				}
				if err := addUUID(q, "shed_id", a.ShedID); err != nil {
					return nil, err
				}
				if err := addSessionNumber(q, "session", a.Session, 99); err != nil {
					return nil, err
				}
				if err := addEnum(q, "workflow", a.Workflow, "normal", "experiment"); err != nil {
					return nil, err
				}
				if err := addBool(q, "draft", a.Draft); err != nil {
					return nil, err
				}
				addLimit(q, a.Limit, 200)
				addOffset(q, a.Offset)
				return q, nil
			},
		},
		{
			Name:        "get_procurement_pipeline",
			Description: "Get leadership-visible procurement source-entry loads. Use this for supplier warmup, transit/source-entry pipeline, expected/accepted/rejected load follow-up. Do not turn a single load into company totals. Procurement Action Center/Control Tower are intentionally not exposed until their top-level command routes are mounted.",
			Path:        "/procurement/source-entry/loads",
			Source:      "GET /procurement/source-entry/loads",
			Properties:  commonReadProperties("status", "cursor", "limit"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addStringMax(q, "status", a.Status, 80); err != nil {
					return nil, err
				}
				addOpaque(q, "cursor", a.Cursor)
				addLimit(q, a.Limit, 500)
				return q, nil
			},
		},
		{
			Name:        "get_sales_overview",
			Description: "Get the canonical Sales dashboard overview in one read: closed-deal revenue, animals sold, realized price per kg, manure sold, monthly charts, buyers, demand pipeline, tag evidence, weight audit, and market benchmarks. Only Deal Closed rows feed closed-sales KPIs; demand and evidence panels keep their own grains.",
			Path:        "/sales/overview",
			Source:      "GET /sales/overview",
			Properties:  commonReadProperties("farm"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addSalesFarm(q, a.Farm); err != nil {
					return nil, err
				}
				return q, nil
			},
		},
		{
			Name:        "get_sales_deals",
			Description: "Get a bounded page of the Sales ledger. Use this for deal-level sales evidence and reconciliation. This is a read-only ledger page; it must not be used to record or mutate sales.",
			Path:        "/sales/deals",
			Source:      "GET /sales/deals",
			Properties:  commonReadProperties("farm", "limit", "offset"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addSalesFarm(q, a.Farm); err != nil {
					return nil, err
				}
				addLimit(q, a.Limit, 100)
				addOffset(q, a.Offset)
				return q, nil
			},
		},
		{
			Name:        "get_counts_summary",
			Description: "Get canonical herd/census counts grouped by park, shed, stage, breed, and sex. Use this for active animal counts, census splits, mortality/movement follow-up starts, and count breakdowns. Keep lifecycle status explicit.",
			Path:        "/counts/breakdown",
			Source:      "GET /counts/breakdown",
			Properties:  commonReadProperties("park_id", "shed_id", "partition_label", "management_stage", "breed", "sex", "lifecycle_status", "limit", "offset"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addUUID(q, "park_id", a.ParkID); err != nil {
					return nil, err
				}
				if err := addUUID(q, "shed_id", a.ShedID); err != nil {
					return nil, err
				}
				for _, item := range []struct{ name, raw string }{
					{"partition_label", a.PartitionLabel},
					{"management_stage", a.ManagementStage},
					{"breed", a.Breed},
					{"sex", a.Sex},
					{"lifecycle_status", a.LifecycleStatus},
				} {
					if err := addStringMax(q, item.name, item.raw, 120); err != nil {
						return nil, err
					}
				}
				addLimit(q, a.Limit, 100)
				addOffset(q, a.Offset)
				return q, nil
			},
		},
		{
			Name:        "get_health_work_items",
			Description: "Get one day's Adult or Kids Health treatment sessions. Use this for open health cases, due treatment sessions, disease/protocol work, and held/canceled-death state. For all-health CEO questions, call once with age_band=adult and once with age_band=kid, then combine summaries. Sick/open work is not mortality unless the health workflow says death was approved.",
			Path:        "/app/health/work-items",
			Source:      "GET /app/health/work-items",
			Properties:  commonReadProperties("age_band", "date", "status", "disease_key", "park_id", "shed_id", "session", "cursor", "limit"),
			Required:    []string{"age_band"},
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addEnum(q, "age_band", a.AgeBand, "adult", "kid"); err != nil {
					return nil, err
				}
				if strings.TrimSpace(a.AgeBand) == "" {
					return nil, errors.New("age_band_required")
				}
				if err := addDate(q, "date", a.Date); err != nil {
					return nil, err
				}
				if err := addEnum(q, "status", a.Status, "scheduled", "due", "in_progress", "completed", "rework", "held", "canceled_death"); err != nil {
					return nil, err
				}
				if err := addStringMax(q, "disease_key", a.DiseaseKey, 120); err != nil {
					return nil, err
				}
				if err := addUUID(q, "park_id", a.ParkID); err != nil {
					return nil, err
				}
				if err := addUUID(q, "shed_id", a.ShedID); err != nil {
					return nil, err
				}
				if err := addSessionLabel(q, "session", a.Session, "morning", "afternoon", "evening", "unscheduled"); err != nil {
					return nil, err
				}
				addOpaque(q, "cursor", a.Cursor)
				addLimit(q, a.Limit, 20)
				return q, nil
			},
		},
		{
			Name:        "get_milk_feeding_today",
			Description: "Get today's kid milk-feeding farm-session tasks. Use this with health answers for kid milk-feeding work, not-submitted sessions, head count, and verification status. Milk Feeding is a separate Milk action, not clinical Adult/Kids Health.",
			Path:        "/app/counts/milk-feeding/tasks",
			Source:      "GET /app/counts/milk-feeding/tasks",
			Properties:  commonReadProperties("feeding_date", "date", "park_id", "session_no", "limit", "offset"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addDate(q, "feeding_date", firstNonEmpty(a.FeedingDate, a.Date, a.TargetDate, a.BusinessDate)); err != nil {
					return nil, err
				}
				if err := addUUID(q, "park_id", a.ParkID); err != nil {
					return nil, err
				}
				if err := addSmallInt(q, "session_no", a.SessionNo, 4); err != nil {
					return nil, err
				}
				addLimit(q, a.Limit, 20)
				addOffset(q, a.Offset)
				return q, nil
			},
		},
		{
			Name:        "get_workforce_coverage",
			Description: "Get canonical workforce/roster coverage rows. Use this for which sheds, parks, modules, positions, or backup-manager seats are uncovered or weakly covered. Do not answer workforce coverage from Action Center obligations.",
			Path:        "/admin/roster/coverage",
			Source:      "GET /admin/roster/coverage",
			Properties:  commonReadProperties("scope_type", "scope_id", "limit"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addStringMax(q, "scope_type", a.ScopeType, 80); err != nil {
					return nil, err
				}
				if err := addStringMax(q, "scope_id", a.ScopeID, 120); err != nil {
					return nil, err
				}
				addLimit(q, a.Limit, 500)
				return q, nil
			},
		},
		{
			Name:        "get_weighing_progress",
			Description: "Get leadership-visible weighing campaigns. Use this for weighing progress and campaign state. Pending verification weight is not verified weight.",
			Path:        "/weighing/campaigns",
			Source:      "GET /weighing/campaigns",
			Properties:  commonReadProperties("park_id", "cursor", "limit"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addUUID(q, "park_id", a.ParkID); err != nil {
					return nil, err
				}
				addOpaque(q, "cursor", a.Cursor)
				addLimit(q, a.Limit, 5000)
				return q, nil
			},
		},
		{
			Name:        "get_weighing_growth_adg",
			Description: "Get CEO-tier Average Daily Gain/growth across authorized parks or one park. Use this for 'are weights improving', growth trend, and park-level weight performance questions.",
			Path:        "/weighing/leadership/growth",
			Source:      "GET /weighing/leadership/growth",
			Properties:  commonReadProperties("park_id", "from", "to", "sex"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addUUID(q, "park_id", a.ParkID); err != nil {
					return nil, err
				}
				if err := addDate(q, "from", a.From); err != nil {
					return nil, err
				}
				if err := addDate(q, "to", a.To); err != nil {
					return nil, err
				}
				if err := addEnum(q, "sex", a.Sex, "male", "female"); err != nil {
					return nil, err
				}
				return q, nil
			},
		},
		{
			Name:        "get_weighing_shed_weights",
			Description: "Get CEO-tier shed weight rows and KPI rollup across authorized parks or one park. Use this for which sheds are lagging, latest shed weights, and weight coverage questions.",
			Path:        "/weighing/shed-weights",
			Source:      "GET /weighing/shed-weights",
			Properties:  commonReadProperties("park_id", "from", "to", "sex"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addUUID(q, "park_id", a.ParkID); err != nil {
					return nil, err
				}
				if err := addDate(q, "from", a.From); err != nil {
					return nil, err
				}
				if err := addDate(q, "to", a.To); err != nil {
					return nil, err
				}
				if err := addEnum(q, "sex", a.Sex, "male", "female"); err != nil {
					return nil, err
				}
				return q, nil
			},
		},
		{
			Name:        "get_weighing_process_state",
			Description: "Get weighing process state for calendar/control-tower gaps across a date range. Use this for overdue weighing, pending proof/review, and process health questions.",
			Path:        "/weighing/process-state",
			Source:      "GET /weighing/process-state",
			Properties:  commonReadProperties("campaign_id", "from", "to"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addUUID(q, "campaign_id", a.CampaignID); err != nil {
					return nil, err
				}
				if strings.TrimSpace(a.From) == "" && strings.TrimSpace(a.To) == "" {
					today := businessDateToday()
					a.From = today
					a.To = today
				}
				if err := addDate(q, "from", a.From); err != nil {
					return nil, err
				}
				if err := addDate(q, "to", a.To); err != nil {
					return nil, err
				}
				return q, nil
			},
		},
		{
			Name:        "get_weighing_weight_demographics",
			Description: "Get CEO-tier breed/sex/stage weight demographics across authorized parks or one park. Use this for demographic weight mix and group comparison questions.",
			Path:        "/weighing/weight-demographics",
			Source:      "GET /weighing/weight-demographics",
			Properties:  commonReadProperties("park_id", "from", "to", "sex"),
			BuildQuery: func(a apiReadArgs) (url.Values, error) {
				q := url.Values{}
				if err := addUUID(q, "park_id", a.ParkID); err != nil {
					return nil, err
				}
				if err := addDate(q, "from", a.From); err != nil {
					return nil, err
				}
				if err := addDate(q, "to", a.To); err != nil {
					return nil, err
				}
				if err := addEnum(q, "sex", a.Sex, "male", "female"); err != nil {
					return nil, err
				}
				return q, nil
			},
		},
	}
}

func apiReadToolByName(name string) (apiReadTool, bool) {
	for _, def := range apiReadTools() {
		if def.Name == name {
			return def, true
		}
	}
	return apiReadTool{}, false
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
	authz, email, code, msg := s.verifiedAuthorization(r)
	if msg != "" {
		return nil, code, msg
	}
	if vaccinationScheduleQuestion(question) {
		toolArgs := vaccinationTodayArgs{BusinessDate: firstYYYYMMDD(question)}
		payload, _ := json.Marshal(toolArgs)
		return s.getVaccinationToday(ctx, r, payload)
	}
	if route, ok := s.naturalLanguageReadRoute(question); ok {
		payload, _ := json.Marshal(route.Args)
		if route.Special == "health_today" {
			return s.getHealthToday(ctx, r, payload)
		}
		if def, found := apiReadToolByName(route.Tool); found {
			return s.getAPIReadTool(ctx, r, payload, def)
		}
		return nil, -32603, "natural_language_route_not_configured"
	}
	return s.proxyAskGoatOS(ctx, r, authz, email, question, strings.TrimSpace(args.ConversationID))
}

type naturalLanguageRoute struct {
	Tool    string
	Special string
	Args    apiReadArgs
}

func (s *server) naturalLanguageReadRoute(question string) (naturalLanguageRoute, bool) {
	if unsafeMutationOrTenantBypassIntent(question) {
		return naturalLanguageRoute{}, false
	}
	q := normalizeIntentText(question)
	args := apiReadArgs{
		Date:         businessDateToday(),
		BusinessDate: businessDateToday(),
		TargetDate:   businessDateToday(),
		FeedingDate:  businessDateToday(),
		Limit:        20,
	}
	if farm := farmFromQuestion(q); farm != "" {
		args.Farm = farm
	}
	if sex := sexFromQuestion(q); sex != "" {
		args.Sex = sex
	}
	defaultPark := strings.TrimSpace(s.cfg.DefaultParkID)
	if defaultPark != "" && (strings.Contains(q, "this park") || strings.Contains(q, "active animal count in park") || strings.Contains(q, "feed today")) {
		args.ParkID = defaultPark
	}

	switch {
	case containsAny(q, "sales deal", "sale deal", "deals ledger", "latst sales deals", "latest sales deals"):
		args.Limit = 10
		return naturalLanguageRoute{Tool: "get_sales_deals", Args: args}, true
	case containsAny(q, "sales", "sale numbers", "sale number", "revnue", "revenue", "animals sold", "manure sold", "price per kg"):
		return naturalLanguageRoute{Tool: "get_sales_overview", Args: args}, true
	case containsAny(q, "demograpics", "demographics", "weight mix", "breed stage"):
		return naturalLanguageRoute{Tool: "get_weighing_weight_demographics", Args: args}, true
	case containsAny(q, "pending weighing proof", "weighing proof", "review overdue", "proces state", "process state"):
		return naturalLanguageRoute{Tool: "get_weighing_process_state", Args: args}, true
	case containsAny(q, "shed are lagging", "sheds are lagging", "shed weights", "shed weight", "latest shed weights"):
		return naturalLanguageRoute{Tool: "get_weighing_shed_weights", Args: args}, true
	case containsAny(q, "weighng", "weighing", "weight gain", "daily gain", "avg wt", "average weight", "total weight", "over 30", "over 35"):
		return naturalLanguageRoute{Tool: "get_weighing_growth_adg", Args: args}, true
	case containsAny(q, "milk feedng", "milk feeding", "milk task"):
		args.Limit = 20
		return naturalLanguageRoute{Tool: "get_milk_feeding_today", Args: args}, true
	case containsAny(q, "helth today", "health today", "adult and kid", "adult kid"):
		args.Limit = 20
		return naturalLanguageRoute{Special: "health_today", Args: args}, true
	case containsAny(q, "feed today", "qty gaps", "quantity gaps", "blocked feed"):
		args.Limit = 50
		return naturalLanguageRoute{Tool: "get_feed_today", Args: args}, true
	case containsAny(q, "active animal count", "count in park", "animal count", "counts split"):
		args.LifecycleStatus = "alive"
		return naturalLanguageRoute{Tool: "get_counts_summary", Args: args}, true
	case containsAny(q, "pending verification", "verification backlog", "proof videos", "waiting"):
		args.Status = "pending"
		return naturalLanguageRoute{Tool: "get_verification_backlog", Args: args}, true
	case containsAny(q, "procuremnet", "procurement", "pipeline loads", "warmup transit", "accepted rejected"):
		return naturalLanguageRoute{Tool: "get_procurement_pipeline", Args: args}, true
	case containsAny(q, "workfrce", "workforce", "coverage gaps", "uncovered sheds", "backup manager"):
		return naturalLanguageRoute{Tool: "get_workforce_coverage", Args: args}, true
	default:
		return naturalLanguageRoute{}, false
	}
}

func unsafeMutationOrTenantBypassIntent(question string) bool {
	q := normalizeIntentText(question)
	for _, safeNegation := range []string{
		"dont create", "don't create", "do not create",
		"dont record", "don't record", "do not record",
		"dont update", "don't update", "do not update",
		"dont delete", "don't delete", "do not delete",
	} {
		q = strings.ReplaceAll(q, safeNegation, "")
	}
	return containsAny(q,
		"record a", "create", "delete", "update", "fake sale", "ignore tenant", "all tenants",
		"secret db", "raw sql", "bypass", "mutate",
	)
}

func normalizeIntentText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func farmFromQuestion(q string) string {
	switch {
	case strings.Contains(q, "cbe"):
		return "CBE"
	case strings.Contains(q, "cpt"):
		return "CPT"
	default:
		return ""
	}
}

func sexFromQuestion(q string) string {
	switch {
	case strings.Contains(q, "female"):
		return "female"
	case strings.Contains(q, "male"):
		return "male"
	default:
		return ""
	}
}

func vaccinationScheduleQuestion(question string) bool {
	q := strings.ToLower(question)
	if !strings.Contains(q, "vaccin") {
		return false
	}
	scheduleWord := strings.Contains(q, "scheduled") || strings.Contains(q, "schedule") || strings.Contains(q, "today") || strings.Contains(q, "due")
	if !scheduleWord {
		return false
	}
	return !strings.Contains(q, "missed") && !strings.Contains(q, "overdue") && !strings.Contains(q, "backlog")
}

func firstYYYYMMDD(text string) string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '-' || (r >= '0' && r <= '9'))
	})
	for _, field := range fields {
		if len(field) == len("2006-01-02") {
			if _, err := time.Parse("2006-01-02", field); err == nil {
				return field
			}
		}
	}
	return ""
}

type apiReadArgs struct {
	BusinessDate     string `json:"business_date"`
	BusinessDateFrom string `json:"business_date_from"`
	BusinessDateTo   string `json:"business_date_to"`
	TargetDate       string `json:"target_date"`
	ParkID           string `json:"park_id"`
	ShedID           string `json:"shed_id"`
	Category         string `json:"category"`
	Vertical         string `json:"vertical"`
	Module           string `json:"module"`
	NavModule        string `json:"nav_module"`
	Status           string `json:"status"`
	WorkState        string `json:"work_state"`
	Severity         string `json:"severity"`
	OwnerID          string `json:"owner_id"`
	PartitionLabel   string `json:"partition_label"`
	ManagementStage  string `json:"management_stage"`
	Breed            string `json:"breed"`
	Sex              string `json:"sex"`
	LifecycleStatus  string `json:"lifecycle_status"`
	AgeBand          string `json:"age_band"`
	Date             string `json:"date"`
	FeedingDate      string `json:"feeding_date"`
	From             string `json:"from"`
	To               string `json:"to"`
	DiseaseKey       string `json:"disease_key"`
	AsOf             string `json:"as_of"`
	DueAfter         string `json:"due_after"`
	DueBefore        string `json:"due_before"`
	Workflow         string `json:"workflow"`
	Farm             string `json:"farm"`
	CampaignID       string `json:"campaign_id"`
	ScopeType        string `json:"scope_type"`
	ScopeID          string `json:"scope_id"`
	Cursor           string `json:"cursor"`
	Missed           any    `json:"missed"`
	Draft            any    `json:"draft"`
	Session          any    `json:"session"`
	SessionNo        int    `json:"session_no"`
	Limit            int    `json:"limit"`
	Offset           int    `json:"offset"`
}

func (s *server) getHealthToday(ctx context.Context, r *http.Request, raw json.RawMessage) (map[string]any, int, string) {
	var args apiReadArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, -32602, "invalid_health_today_arguments"
		}
	}
	authz, email, code, msg := s.verifiedAuthorization(r)
	if msg != "" {
		return nil, code, msg
	}
	if s.cfg.UpstreamBaseURL == "" {
		return nil, -32603, "upstream_base_url_not_configured"
	}
	results := map[string]any{}
	queries := map[string]any{}
	healthDef, ok := apiReadToolByName("get_health_work_items")
	if !ok {
		return nil, -32603, "health_work_items_not_configured"
	}
	milkDef, ok := apiReadToolByName("get_milk_feeding_today")
	if !ok {
		return nil, -32603, "milk_feeding_today_not_configured"
	}
	for _, ageBand := range []string{"adult", "kid"} {
		next := args
		next.AgeBand = ageBand
		q, err := healthDef.BuildQuery(next)
		if err != nil {
			return nil, -32602, err.Error()
		}
		var payload any
		if err := s.getUpstreamJSON(ctx, r, authz, email, healthDef.Path, q, &payload); err != nil {
			s.log.Warn("goatos_mcp_health_today_failed", slog.String("age_band", ageBand), slog.Any("error", err))
			return nil, -32603, "health_today_unreachable"
		}
		results[ageBand] = payload
		queries[ageBand] = queryObject(q)
	}
	milkQ, err := milkDef.BuildQuery(args)
	if err != nil {
		return nil, -32602, err.Error()
	}
	var milkPayload any
	if err := s.getUpstreamJSON(ctx, r, authz, email, milkDef.Path, milkQ, &milkPayload); err != nil {
		s.log.Warn("goatos_mcp_health_today_milk_feeding_failed", slog.Any("error", err))
		return nil, -32603, "health_today_unreachable"
	}
	results["milk_feeding"] = milkPayload
	queries["milk_feeding"] = queryObject(milkQ)
	return structuredTextToolResult(summarizeHealthToday(results), map[string]any{
		"tool":    "get_health_today",
		"source":  "GET /app/health/work-items age_band=adult + kid; GET /app/counts/milk-feeding/tasks",
		"queries": queries,
		"data":    results,
	}), 0, ""
}

func (s *server) getAPIReadTool(ctx context.Context, r *http.Request, raw json.RawMessage, def apiReadTool) (map[string]any, int, string) {
	var args apiReadArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, -32602, "invalid_" + def.Name + "_arguments"
		}
	}
	q, err := def.BuildQuery(args)
	if err != nil {
		return nil, -32602, err.Error()
	}
	authz, email, code, msg := s.verifiedAuthorization(r)
	if msg != "" {
		return nil, code, msg
	}
	if s.cfg.UpstreamBaseURL == "" {
		return nil, -32603, "upstream_base_url_not_configured"
	}
	var payload any
	if err := s.getUpstreamJSON(ctx, r, authz, email, def.Path, q, &payload); err != nil {
		s.log.Warn("goatos_mcp_api_read_failed", slog.String("tool", def.Name), slog.String("source", def.Source), slog.Any("error", err))
		return nil, -32603, def.Name + "_unreachable"
	}
	return structuredTextToolResult(summarizeAPIRead(def, q, payload), map[string]any{
		"tool":   def.Name,
		"source": def.Source,
		"query":  queryObject(q),
		"data":   payload,
	}), 0, ""
}

func (s *server) verifiedAuthorization(r *http.Request) (authz, email string, code int, msg string) {
	authz = strings.TrimSpace(r.Header.Get("Authorization"))
	if authz == "" {
		return "", "", -32001, "missing_authorization_bearer"
	}
	token, ok := strings.CutPrefix(authz, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		return "", "", -32001, "invalid_authorization_bearer"
	}
	if s.cfg.TokenVerifier == nil {
		return "", "", -32603, "token_verifier_not_configured"
	}
	claims, err := s.cfg.TokenVerifier.Verify(strings.TrimSpace(token))
	if err != nil {
		s.log.Warn("goatos_mcp_bearer_verify_failed", slog.Any("error", err))
		return "", "", -32001, "invalid_authorization_bearer"
	}
	email = normalizedEmail(claims.Email)
	if len(s.cfg.AllowedEmails) > 0 && !s.cfg.AllowedEmails.Allows(email, claims.EmailVerified) {
		return "", "", -32001, "actor_email_not_allowed"
	}
	return authz, email, 0, ""
}

func (s *server) proxyAskGoatOS(ctx context.Context, r *http.Request, authz, email, question, conversationID string) (map[string]any, int, string) {
	payload := map[string]any{
		"question":        question,
		"conversation_id": conversationID,
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
	} else if tenant := strings.TrimSpace(s.cfg.TenantID); tenant != "" {
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

func (s *server) getVaccinationToday(ctx context.Context, r *http.Request, raw json.RawMessage) (map[string]any, int, string) {
	var args vaccinationTodayArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, -32602, "invalid_vaccination_today_arguments"
		}
	}
	q, err := args.query()
	if err != nil {
		return nil, -32602, err.Error()
	}
	authz, email, code, msg := s.verifiedAuthorization(r)
	if msg != "" {
		return nil, code, msg
	}
	if s.cfg.UpstreamBaseURL == "" {
		return nil, -32603, "upstream_base_url_not_configured"
	}
	businessDate := args.businessDateOrToday()
	driveQuery := args.driveAssignmentsQuery(businessDate)
	var drive vaccinationDriveAssignmentResponse
	if err := s.getUpstreamJSON(ctx, r, authz, email, "/vaccination/drive-assignments", driveQuery, &drive); err != nil {
		s.log.Warn("goatos_mcp_vaccination_schedule_failed", slog.Any("error", err))
		return nil, -32603, "vaccination_schedule_unreachable"
	}
	drive.Rows = filterDriveAssignmentsForDate(drive.Rows, businessDate)

	var tracker vaccinationLiveTrackerResponse
	trackerAvailable := true
	if err := s.getUpstreamJSON(ctx, r, authz, email, "/vaccination/live-tracker", q, &tracker); err != nil {
		trackerAvailable = false
		s.log.Warn("goatos_mcp_vaccination_live_tracker_failed", slog.Any("error", err))
	}
	summary := summarizeVaccinationToday(businessDate, drive, tracker, trackerAvailable)
	return structuredTextToolResult(summary, map[string]any{
		"tool":   "get_vaccination_today",
		"source": "GET /vaccination/drive-assignments + GET /vaccination/live-tracker",
		"data": map[string]any{
			"schedule":                 drive,
			"live_tracker":             tracker,
			"live_tracker_available":   trackerAvailable,
			"schedule_rows_for_date":   len(drive.Rows),
			"requested_business_date":  businessDate,
			"schedule_source_endpoint": "GET /vaccination/drive-assignments",
			"progress_source_endpoint": "GET /vaccination/live-tracker",
		},
	}), 0, ""
}

func (s *server) getUpstreamJSON(ctx context.Context, r *http.Request, authz, email, path string, query url.Values, out any) error {
	endpoint := s.cfg.UpstreamBaseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authz)
	if email != "" {
		req.Header.Set("X-Mesha-Actor-Email", email)
	}
	if tenant := strings.TrimSpace(r.Header.Get("X-GoatOS-Tenant-ID")); tenant != "" {
		req.Header.Set("X-GoatOS-Tenant-ID", tenant)
	} else if tenant := strings.TrimSpace(s.cfg.TenantID); tenant != "" {
		req.Header.Set("X-GoatOS-Tenant-ID", tenant)
	}
	if trace := strings.TrimSpace(r.Header.Get("X-Request-ID")); trace != "" {
		req.Header.Set("X-Request-ID", trace)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upstream status %d: %s", resp.StatusCode, string(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return err
	}
	return nil
}

type vaccinationTodayArgs struct {
	BusinessDate   string `json:"business_date"`
	ParkID         string `json:"park_id"`
	ShedID         string `json:"shed_id"`
	PartitionLabel string `json:"partition_label"`
	OperatorID     string `json:"operator_id"`
	VaccineCode    string `json:"vaccine_code"`
	Status         string `json:"status"`
}

func (a vaccinationTodayArgs) businessDateOrToday() string {
	if date := strings.TrimSpace(a.BusinessDate); date != "" {
		return date
	}
	return businessDateToday()
}

func businessDateToday() string {
	return time.Now().In(time.FixedZone("IST", 5*60*60+30*60)).Format("2006-01-02")
}

func (a vaccinationTodayArgs) driveAssignmentsQuery(businessDate string) url.Values {
	q := url.Values{}
	if t, err := time.Parse("2006-01-02", businessDate); err == nil {
		q.Set("year", strconv.Itoa(t.Year()))
		q.Set("month", strconv.Itoa(int(t.Month())))
	}
	if parkID := strings.TrimSpace(a.ParkID); parkID != "" {
		q.Set("park_id", parkID)
	}
	q.Set("limit", "500")
	return q
}

func (a vaccinationTodayArgs) query() (url.Values, error) {
	q := url.Values{}
	addDateParam := func(name, raw string) error {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil
		}
		if _, err := time.Parse("2006-01-02", raw); err != nil {
			return fmt.Errorf("%s_must_be_yyyy_mm_dd", name)
		}
		q.Set(name, raw)
		return nil
	}
	if err := addDateParam("business_date", a.BusinessDate); err != nil {
		return nil, err
	}
	for _, item := range []struct {
		name string
		raw  string
	}{
		{"park_id", a.ParkID},
		{"shed_id", a.ShedID},
		{"operator_id", a.OperatorID},
	} {
		raw := strings.TrimSpace(item.raw)
		if raw == "" {
			continue
		}
		if !isUUID(raw) {
			return nil, fmt.Errorf("invalid_%s", item.name)
		}
		q.Set(item.name, raw)
	}
	if label := strings.TrimSpace(a.PartitionLabel); label != "" {
		if len(label) > 64 {
			return nil, errors.New("invalid_partition_label")
		}
		q.Set("partition_label", label)
	}
	if vaccine := strings.TrimSpace(a.VaccineCode); vaccine != "" {
		if len(vaccine) > 64 {
			return nil, errors.New("invalid_vaccine_code")
		}
		q.Set("vaccine_code", vaccine)
	}
	if status := strings.TrimSpace(a.Status); status != "" {
		switch status {
		case "active", "done", "pending", "review":
			q.Set("status", status)
		default:
			return nil, errors.New("invalid_status")
		}
	}
	return q, nil
}

type vaccinationDriveAssignmentResponse struct {
	Source string                          `json:"source"`
	Rows   []vaccinationDriveAssignmentRow `json:"rows"`
}

type vaccinationDriveAssignmentRow struct {
	PlannedDate     string   `json:"plannedDate"`
	OperatorName    string   `json:"operatorName"`
	ParkName        string   `json:"parkName"`
	PhysicalShed    string   `json:"physicalShed"`
	PartitionLabel  string   `json:"partitionLabel"`
	Animals         int      `json:"animals"`
	DueAnimals      int      `json:"dueAnimals"`
	DoneAnimals     int      `json:"doneAnimals"`
	DeferredAnimals int      `json:"deferredAnimals"`
	OverdueAnimals  int      `json:"overdueAnimals"`
	VaccineNames    []string `json:"vaccineNames"`
	TotalDoses      int      `json:"totalDoses"`
}

func isUUID(raw string) bool {
	if len(raw) != 36 {
		return false
	}
	for i, r := range raw {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

func commonReadProperties(names ...string) map[string]any {
	props := map[string]any{}
	for _, name := range names {
		switch name {
		case "business_date", "business_date_from", "business_date_to", "target_date", "from", "to", "date", "feeding_date":
			props[name] = map[string]any{"type": "string", "description": "Asia/Kolkata business date in YYYY-MM-DD."}
		case "as_of", "due_after", "due_before":
			props[name] = map[string]any{"type": "string", "description": "RFC3339 timestamp."}
		case "park_id", "shed_id", "owner_id", "campaign_id":
			props[name] = map[string]any{"type": "string", "description": "UUID or backend-supported opaque key where documented."}
		case "limit", "offset", "session", "session_no":
			props[name] = map[string]any{"type": "integer"}
		case "missed", "draft":
			props[name] = map[string]any{"type": "boolean"}
		default:
			props[name] = map[string]any{"type": "string"}
		}
	}
	return props
}

func addDate(q url.Values, name, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", raw); err != nil {
		return fmt.Errorf("%s_must_be_yyyy_mm_dd", name)
	}
	q.Set(name, raw)
	return nil
}

func addRequiredDate(q url.Values, name, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s_required", name)
	}
	return addDate(q, name, raw)
}

func addRFC3339(q url.Values, name, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, raw); err != nil {
		return fmt.Errorf("%s_must_be_rfc3339", name)
	}
	q.Set(name, raw)
	return nil
}

func addUUID(q url.Values, name, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if !isUUID(raw) {
		return fmt.Errorf("invalid_%s", name)
	}
	q.Set(name, raw)
	return nil
}

func addRequiredUUID(q url.Values, name, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s_required", name)
	}
	return addUUID(q, name, raw)
}

func addEnum(q url.Values, name, raw string, allowed ...string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, value := range allowed {
		if raw == value {
			q.Set(name, raw)
			return nil
		}
	}
	return fmt.Errorf("invalid_%s", name)
}

func addSalesFarm(q url.Values, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "all" {
		return nil
	}
	switch raw {
	case "CBE", "CPT":
		q.Set("farm", raw)
		return nil
	default:
		return errors.New("invalid_farm")
	}
}

func addStringMax(q url.Values, name, raw string, maxLen int) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if len(raw) > maxLen {
		return fmt.Errorf("invalid_%s", name)
	}
	q.Set(name, raw)
	return nil
}

func addBool(q url.Values, name string, raw any) error {
	switch v := raw.(type) {
	case nil:
		return nil
	case bool:
		q.Set(name, strconv.FormatBool(v))
		return nil
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return nil
		}
		if v != "true" && v != "false" {
			return fmt.Errorf("invalid_%s", name)
		}
		q.Set(name, v)
		return nil
	default:
		return fmt.Errorf("invalid_%s", name)
	}
}

func addSmallInt(q url.Values, name string, value, max int) error {
	if value == 0 {
		return nil
	}
	if value < 0 || value > max {
		return fmt.Errorf("invalid_%s", name)
	}
	q.Set(name, strconv.Itoa(value))
	return nil
}

func addSessionNumber(q url.Values, name string, raw any, max int) error {
	switch v := raw.(type) {
	case nil:
		return nil
	case float64:
		if v != float64(int(v)) {
			return fmt.Errorf("invalid_%s", name)
		}
		return addSmallInt(q, name, int(v), max)
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid_%s", name)
		}
		return addSmallInt(q, name, n, max)
	default:
		return fmt.Errorf("invalid_%s", name)
	}
}

func addSessionLabel(q url.Values, name string, raw any, allowed ...string) error {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		return addEnum(q, name, v, allowed...)
	default:
		return fmt.Errorf("invalid_%s", name)
	}
}

func addLimit(q url.Values, value, max int) {
	if value <= 0 {
		return
	}
	if value > max {
		value = max
	}
	q.Set("limit", strconv.Itoa(value))
}

func addOffset(q url.Values, value int) {
	if value > 0 {
		q.Set("offset", strconv.Itoa(value))
	}
}

func addOpaque(q url.Values, name, value string) {
	if value = strings.TrimSpace(value); value != "" {
		q.Set(name, value)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func queryObject(q url.Values) map[string]any {
	out := map[string]any{}
	for key, values := range q {
		if len(values) == 1 {
			out[key] = values[0]
		} else if len(values) > 1 {
			out[key] = values
		}
	}
	return out
}

func summarizeAPIRead(def apiReadTool, query url.Values, payload any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", def.Description)
	fmt.Fprintf(&b, "\nSource: %s\n", def.Source)
	if filters := queryObject(query); len(filters) > 0 {
		parts := make([]string, 0, len(filters))
		for key, value := range filters {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
		}
		fmt.Fprintf(&b, "Applied filters: %s\n", strings.Join(parts, ", "))
	}
	keys := topLevelKeys(payload)
	if len(keys) > 0 {
		fmt.Fprintf(&b, "Returned keys: %s\n", strings.Join(keys, ", "))
	}
	counts := payloadCounts(payload)
	if len(counts) > 0 {
		b.WriteString("Observed collection sizes:\n")
		for _, item := range counts {
			fmt.Fprintf(&b, "- %s: %d\n", item.name, item.count)
		}
	}
	switch def.Name {
	case "get_verification_backlog":
		b.WriteString("\nJudge note: pending verification is evidence waiting for review; it is not completed work.\n")
	case "get_sales_overview":
		b.WriteString("\nJudge note: Sales overview KPIs count only Deal Closed rows; demand pipeline and evidence panels have their own grains and must not be summed into closed revenue or animals sold.\n")
	case "get_sales_deals":
		b.WriteString("\nJudge note: Sales deals are ledger rows for reconciliation; this MCP tool is read-only and must not record or mutate sales.\n")
	case "get_feed_today":
		b.WriteString("\nJudge note: blocked/null feed quantities are configuration gaps, not zero feed.\n")
		b.WriteString("Judge note: this is planned/issued feed, not proof that feed actually happened; feed actuals/adherence are not covered by this MCP tool yet.\n")
	case "get_action_center":
		b.WriteString("\nJudge note: Action Center rows are process-integrity obligations; do not mix them with operator schedule totals.\n")
	case "get_weighing_progress":
		b.WriteString("\nJudge note: pending verification weight is not verified weight.\n")
	case "get_health_work_items":
		b.WriteString("\nJudge note: open health/treatment work is not a death or mortality event unless the health workflow explicitly reports approved death state.\n")
	case "get_milk_feeding_today":
		b.WriteString("\nJudge note: Milk Feeding is kid feeding work under Milk; it is not clinical Adult/Kids Health and should be reported separately from treatment sessions.\n")
	case "get_workforce_coverage":
		b.WriteString("\nJudge note: workforce coverage rows are roster/backup ownership facts; do not substitute Action Center obligations for coverage ownership.\n")
	case "get_counts_summary":
		b.WriteString("\nJudge note: counts are aggregate census facts; lifecycle status must stay explicit when comparing active, exited, or dead animals.\n")
	}
	return b.String()
}

func summarizeHealthToday(payload map[string]any) string {
	var b strings.Builder
	b.WriteString("Get adult health, kids health, and kid milk-feeding health tasks for one day in a single call. Use this for broad CEO health questions so clinical and milk-feeding work are not accidentally reported as a partial answer.\n")
	b.WriteString("\nSource: GET /app/health/work-items age_band=adult + kid; GET /app/counts/milk-feeding/tasks\n")
	for _, label := range []string{"adult", "kid", "milk_feeding"} {
		counts := payloadCounts(payload[label])
		if len(counts) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s collection sizes:\n", label)
		for _, item := range counts {
			fmt.Fprintf(&b, "- %s: %d\n", item.name, item.count)
		}
	}
	b.WriteString("\nJudge note: this combines adult health, kids health, and kid milk-feeding tasks. Milk Feeding is a separate Milk action; open health/treatment work is not a death or mortality event unless the health workflow explicitly reports approved death state.\n")
	return b.String()
}

func topLevelKeys(payload any) []string {
	obj, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sortStrings(keys)
	if len(keys) > 12 {
		return keys[:12]
	}
	return keys
}

type namedCount struct {
	name  string
	count int
}

func payloadCounts(payload any) []namedCount {
	obj, ok := payload.(map[string]any)
	if !ok {
		if rows, ok := payload.([]any); ok {
			return []namedCount{{name: "rows", count: len(rows)}}
		}
		return nil
	}
	var out []namedCount
	for _, key := range []string{"items", "rows", "loads", "campaigns", "operators", "sheds", "queue", "data"} {
		if rows, ok := obj[key].([]any); ok {
			out = append(out, namedCount{name: key, count: len(rows)})
		}
	}
	return out
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

type vaccinationLiveTrackerResponse struct {
	BusinessDate              string                         `json:"business_date"`
	GeneratedAt               time.Time                      `json:"generated_at"`
	IsLiveDay                 bool                           `json:"is_live_day"`
	KPIs                      vaccinationLiveTrackerKPIs     `json:"kpis"`
	Operators                 []vaccinationLiveTrackerWorker `json:"operators"`
	Sheds                     []vaccinationLiveTrackerShed   `json:"sheds"`
	OperatorsTotal            int                            `json:"operators_total"`
	OperatorsTruncated        bool                           `json:"operators_truncated"`
	ShedsTotal                int                            `json:"sheds_total"`
	ShedsTruncated            bool                           `json:"sheds_truncated"`
	CellsTruncated            bool                           `json:"cells_truncated"`
	UnassignedAdministrations int                            `json:"unassigned_administrations"`
	AttentionTotal            int                            `json:"attention_total"`
	AttentionTruncated        bool                           `json:"attention_truncated"`
	Verification              map[string]any                 `json:"verification"`
}

type vaccinationLiveTrackerKPIs struct {
	ScheduledAdministrations int `json:"scheduled_administrations"`
	ProofVideosReceived      int `json:"proof_videos_received"`
	ClosedAdministrations    int `json:"closed_administrations"`
	AwaitingClose            int `json:"awaiting_close"`
	ScanCaptures             int `json:"scan_captures"`
	Remaining                int `json:"remaining"`
	ComboAnimals             int `json:"combo_animals"`
	AttentionCount           int `json:"attention_count"`
	ActiveParks              int `json:"active_parks"`
}

type vaccinationLiveTrackerWorker struct {
	OperatorName          string `json:"operator_name"`
	ParkName              string `json:"park_name"`
	CurrentShedLabel      string `json:"current_shed_label"`
	CurrentPartitionLabel string `json:"current_partition_label"`
	CurrentVaccineLabel   string `json:"current_vaccine_label"`
	ScheduledAdmins       int    `json:"scheduled_administrations"`
	ProofVideos           int    `json:"proof_videos"`
	ScanCaptures          int    `json:"scan_captures"`
	ClosedAdmins          int    `json:"closed_administrations"`
	Remaining             int    `json:"remaining"`
	State                 string `json:"state"`
}

type vaccinationLiveTrackerShed struct {
	ParkName            string `json:"park_name"`
	ShedLabel           string `json:"shed_label"`
	VaccineLabel        string `json:"vaccine_label"`
	OperatorName        string `json:"operator_name"`
	ScheduledAdmins     int    `json:"scheduled_administrations"`
	ClosedAdmins        int    `json:"closed_administrations"`
	ProofVideosReceived int    `json:"proof_videos_received"`
	Remaining           int    `json:"remaining"`
	State               string `json:"state"`
}

func summarizeVaccinationLiveTracker(v vaccinationLiveTrackerResponse) string {
	k := v.KPIs
	var b strings.Builder
	fmt.Fprintf(&b, "Vaccination drive-day progress for %s:\n", v.BusinessDate)
	fmt.Fprintf(&b, "- Scheduled administrations: %d\n", k.ScheduledAdministrations)
	fmt.Fprintf(&b, "- Closed/completed administrations: %d\n", k.ClosedAdministrations)
	fmt.Fprintf(&b, "- Proof videos received: %d\n", k.ProofVideosReceived)
	fmt.Fprintf(&b, "- Scan captures: %d\n", k.ScanCaptures)
	fmt.Fprintf(&b, "- Remaining administrations: %d\n", k.Remaining)
	if v.UnassignedAdministrations > 0 {
		fmt.Fprintf(&b, "- Unassigned scheduled administrations: %d\n", v.UnassignedAdministrations)
	}
	if len(v.Operators) > 0 {
		b.WriteString("\nOperator schedule:\n")
		for _, op := range v.Operators {
			fmt.Fprintf(&b, "- %s: %d scheduled, %d completed, %d proofs, %d scans, %d remaining at %s / %s",
				emptyAs(op.OperatorName, "Unassigned"), op.ScheduledAdmins, op.ClosedAdmins, op.ProofVideos, op.ScanCaptures, op.Remaining, op.ParkName, op.CurrentShedLabel)
			if op.CurrentPartitionLabel != "" {
				fmt.Fprintf(&b, " / %s", op.CurrentPartitionLabel)
			}
			if op.CurrentVaccineLabel != "" {
				fmt.Fprintf(&b, " (%s)", op.CurrentVaccineLabel)
			}
			if op.State != "" {
				fmt.Fprintf(&b, " [%s]", op.State)
			}
			b.WriteByte('\n')
		}
	}
	if len(v.Sheds) > 0 {
		b.WriteString("\nShed progress:\n")
		for _, shed := range v.Sheds {
			fmt.Fprintf(&b, "- %s / %s", shed.ParkName, shed.ShedLabel)
			if shed.VaccineLabel != "" {
				fmt.Fprintf(&b, " (%s)", shed.VaccineLabel)
			}
			fmt.Fprintf(&b, ": %d scheduled, %d completed, %d proofs, %d remaining",
				shed.ScheduledAdmins, shed.ClosedAdmins, shed.ProofVideosReceived, shed.Remaining)
			if shed.OperatorName != "" {
				fmt.Fprintf(&b, "; operator %s", shed.OperatorName)
			}
			if shed.State != "" {
				fmt.Fprintf(&b, " [%s]", shed.State)
			}
			b.WriteByte('\n')
		}
	}
	if v.CellsTruncated || v.OperatorsTruncated || v.ShedsTruncated || v.AttentionTruncated {
		fmt.Fprintf(&b, "\nWarning: response was truncated; cells=%v operators=%v sheds=%v attention=%v.\n",
			v.CellsTruncated, v.OperatorsTruncated, v.ShedsTruncated, v.AttentionTruncated)
	}
	b.WriteString("\nSource: GET /vaccination/live-tracker")
	return b.String()
}

func filterDriveAssignmentsForDate(rows []vaccinationDriveAssignmentRow, businessDate string) []vaccinationDriveAssignmentRow {
	out := make([]vaccinationDriveAssignmentRow, 0, len(rows))
	for _, row := range rows {
		if row.PlannedDate == businessDate {
			out = append(out, row)
		}
	}
	return out
}

func summarizeVaccinationToday(businessDate string, drive vaccinationDriveAssignmentResponse, tracker vaccinationLiveTrackerResponse, trackerAvailable bool) string {
	var totalScheduled, totalDone, totalDue, totalOverdue, totalDeferred, totalDoses int
	for _, row := range drive.Rows {
		totalScheduled += row.Animals
		totalDone += row.DoneAnimals
		totalDue += row.DueAnimals
		totalOverdue += row.OverdueAnimals
		totalDeferred += row.DeferredAnimals
		totalDoses += row.TotalDoses
	}
	remaining := totalScheduled - totalDone - totalDeferred
	if remaining < 0 {
		remaining = 0
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Vaccination scheduled today (%s):\n", businessDate)
	fmt.Fprintf(&b, "- Animals scheduled: %d\n", totalScheduled)
	fmt.Fprintf(&b, "- Done: %d\n", totalDone)
	fmt.Fprintf(&b, "- Remaining: %d\n", remaining)
	fmt.Fprintf(&b, "- Due now: %d\n", totalDue)
	fmt.Fprintf(&b, "- Overdue in today's schedule: %d\n", totalOverdue)
	if totalDeferred > 0 {
		fmt.Fprintf(&b, "- Deferred: %d\n", totalDeferred)
	}
	if totalDoses > 0 {
		fmt.Fprintf(&b, "- Total doses planned: %d\n", totalDoses)
	}
	if len(drive.Rows) == 0 {
		b.WriteString("\nNo operator drive schedule rows are planned for this date.\n")
	} else {
		b.WriteString("\nOperator schedule:\n")
		for _, row := range drive.Rows {
			fmt.Fprintf(&b, "- %s / %s", row.ParkName, row.PhysicalShed)
			if row.PartitionLabel != "" {
				fmt.Fprintf(&b, " / %s", row.PartitionLabel)
			}
			fmt.Fprintf(&b, ": operator %s; %d animals scheduled, %d done, %d remaining",
				emptyAs(row.OperatorName, "Unassigned"), row.Animals, row.DoneAnimals, maxInt(0, row.Animals-row.DoneAnimals-row.DeferredAnimals))
			if row.DueAnimals > 0 {
				fmt.Fprintf(&b, ", %d due", row.DueAnimals)
			}
			if row.OverdueAnimals > 0 {
				fmt.Fprintf(&b, ", %d overdue", row.OverdueAnimals)
			}
			if len(row.VaccineNames) > 0 {
				fmt.Fprintf(&b, "; vaccines: %s", strings.Join(row.VaccineNames, ", "))
			}
			b.WriteByte('\n')
		}
	}

	if trackerAvailable {
		k := tracker.KPIs
		b.WriteString("\nLive proof/progress tracker:\n")
		fmt.Fprintf(&b, "- Tracker scheduled administrations: %d\n", k.ScheduledAdministrations)
		fmt.Fprintf(&b, "- Closed administrations: %d\n", k.ClosedAdministrations)
		fmt.Fprintf(&b, "- Proof videos: %d\n", k.ProofVideosReceived)
		fmt.Fprintf(&b, "- Scan captures: %d\n", k.ScanCaptures)
		fmt.Fprintf(&b, "- Tracker remaining administrations: %d\n", k.Remaining)
	} else {
		b.WriteString("\nLive proof/progress tracker was unavailable; schedule rows above are still from the operator drive schedule.\n")
	}
	b.WriteString("\nSource: GET /vaccination/drive-assignments + GET /vaccination/live-tracker")
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func emptyAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizedEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func textToolResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

func structuredTextToolResult(text string, structured map[string]any) map[string]any {
	result := textToolResult(text)
	result["structuredContent"] = structured
	return result
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
