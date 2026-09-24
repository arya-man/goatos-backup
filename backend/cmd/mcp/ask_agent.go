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
	"net/url"
	"strings"
	"sync"
	"time"
)

// Ask Mesha agent path for ask_goatos (MESHA_MCP_AGENT_URL). The agent runs on Cloud Run
// behind IAM: the MCP's runtime service account needs roles/run.invoker on it and proves that
// with a Google ID token in X-Serverless-Authorization. The user's own bearer stays in
// Authorization; the agent validates it against the STG API (/ceo-ai/starters) itself.

const (
	defaultAgentTimeout        = 240 * time.Second
	defaultMetadataIdentityURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity"
	agentAskPath               = "/ceo-ai/ask"
)

// toolList is tools() with the ask_goatos description matching the active backend.
func (s *server) toolList() []map[string]any {
	out := tools()
	if s.cfg.AgentURL == "" {
		return out
	}
	for _, t := range out {
		if t["name"] == "ask_goatos" {
			t["description"] = askGoatOSDescription(true)
		}
	}
	return out
}

type metadataIDTokenSource struct {
	url    string
	client *http.Client
	mu     sync.Mutex
	cache  map[string]cachedIDToken
}

type cachedIDToken struct {
	token   string
	expires time.Time
}

func newMetadataIDTokenSource(identityURL string) *metadataIDTokenSource {
	return &metadataIDTokenSource{url: identityURL, client: &http.Client{Timeout: 5 * time.Second}, cache: map[string]cachedIDToken{}}
}

// Token returns a Google-signed ID token for audience from the metadata server.
// Tokens live 1h; reuse one for 50 minutes.
func (m *metadataIDTokenSource) Token(ctx context.Context, audience string) (string, error) {
	m.mu.Lock()
	if c, ok := m.cache[audience]; ok && time.Now().Before(c.expires) {
		m.mu.Unlock()
		return c.token, nil
	}
	m.mu.Unlock()
	u := m.url + "?format=full&audience=" + url.QueryEscape(audience)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("metadata identity status %d", resp.StatusCode)
	}
	tok := strings.TrimSpace(string(body))
	if tok == "" {
		return "", errors.New("metadata identity returned an empty token")
	}
	m.mu.Lock()
	m.cache[audience] = cachedIDToken{token: tok, expires: time.Now().Add(50 * time.Minute)}
	m.mu.Unlock()
	return tok, nil
}

func (s *server) proxyAskMeshaAgent(ctx context.Context, r *http.Request, authz, email, question, conversationID string) (map[string]any, int, string) {
	payload := map[string]any{"question": question, "stream": false}
	if conversationID != "" {
		payload["conversation_id"] = conversationID
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.AgentURL+agentAskPath, bytes.NewReader(body))
	if err != nil {
		s.log.Error("goatos_mcp_agent_request_build_failed", slog.Any("error", err))
		return nil, -32603, "agent_request_build_failed"
	}
	if s.idToken == nil {
		return nil, -32603, "agent_identity_not_configured"
	}
	tok, err := s.idToken(ctx, firstNonEmpty(s.cfg.AgentAudience, s.cfg.AgentURL))
	if err != nil {
		s.log.Error("goatos_mcp_agent_id_token_failed", slog.Any("error", err))
		return nil, -32603, "agent_identity_unavailable"
	}
	req.Header.Set("X-Serverless-Authorization", "Bearer "+tok)
	req.Header.Set("Authorization", authz)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mesha-Client", "mcp")
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
	client := s.agentClient
	if client == nil {
		client = &http.Client{Timeout: defaultAgentTimeout}
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		s.log.Warn("goatos_mcp_agent_error", slog.Any("error", err), slog.Duration("elapsed", time.Since(start)))
		var ne interface{ Timeout() bool }
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return nil, -32603, "agent_timeout"
		}
		return nil, -32603, "agent_unreachable"
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	var ans struct {
		Answer         string          `json:"answer"`
		Chart          json.RawMessage `json:"chart"`
		ConversationID string          `json:"conversation_id"`
		MessageID      string          `json:"message_id"`
		RequestID      string          `json:"request_id"`
		Source         string          `json:"source"`
		Error          string          `json:"error"`
		Message        string          `json:"message"`
	}
	_ = json.Unmarshal(respBody, &ans)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.log.Warn("goatos_mcp_agent_non_2xx", slog.Int("status", resp.StatusCode), slog.String("error", ans.Error))
		// The agent's message is CEO-facing plain wording (busy chat, deleted chat, too long…).
		if ans.Message != "" && resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			text := ans.Message
			if ans.ConversationID != "" {
				text += "\nConversation: " + ans.ConversationID
			}
			res := textToolResult(text)
			res["isError"] = true
			return res, 0, ""
		}
		return nil, -32002, "agent_rejected_request"
	}
	if ans.Error != "" && ans.Answer == "" {
		return nil, -32603, "agent_invalid_answer"
	}
	text := ans.Answer
	text += "\n\nSource: Ask Mesha agent"
	if ans.ConversationID != "" {
		text += "\nConversation: " + ans.ConversationID + " (pass as conversation_id for follow-ups)"
	}
	if ans.RequestID != "" {
		text += "\nRequest: " + ans.RequestID
	}
	res := textToolResult(text)
	structured := map[string]any{"answer": ans.Answer, "conversation_id": ans.ConversationID, "message_id": ans.MessageID, "request_id": ans.RequestID}
	if len(ans.Chart) > 0 && string(ans.Chart) != "null" {
		structured["chart"] = ans.Chart
	}
	res["structuredContent"] = structured
	return res, 0, ""
}
