package authaudit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

const (
	ActionSignIn         = "auth.sign_in"
	ActionSessionRefresh = "auth.session_refresh"
	ActionSignOut        = "auth.sign_out"
	ActionFailedSignIn   = "auth.failed_sign_in"

	resourceTypeAuthSession = "auth_session"
	actorTypeUser           = "user"
)

type TokenVerifier interface {
	Verify(token string) (platformauth.Claims, error)
}

type Recorder interface {
	Record(ctx context.Context, event Event) error
}

type Handler struct {
	verifier TokenVerifier
	recorder Recorder
	log      *slog.Logger
}

func NewHandler(verifier TokenVerifier, recorder Recorder, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{verifier: verifier, recorder: recorder, log: log}
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /auth/session-events", h.RecordSessionEvent)
}

func (h *Handler) RecordSessionEvent(w http.ResponseWriter, r *http.Request) {
	if h.verifier == nil || h.recorder == nil {
		writeError(w, r, http.StatusServiceUnavailable, "auth_audit_unconfigured", "auth audit is not configured")
		return
	}

	var body struct {
		EventType string `json:"event_type"`
		Source    string `json:"source"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 2048)).Decode(&body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	action, ok := normalizeAction(body.EventType)
	if !ok {
		writeError(w, r, http.StatusBadRequest, "invalid_auth_event_type", "auth event type is not supported")
		return
	}

	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "missing_bearer_token", "Authorization: Bearer token is required")
		return
	}

	claims, err := h.verifier.Verify(token)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, "invalid_bearer_token", "bearer token is invalid")
		return
	}

	tenantID, tenantSource := tenantFromClaimsOrHeader(claims, r)
	if !isUUIDString(tenantID) {
		_ = h.record(r, Event{
			TenantID:     "",
			ActorID:      claims.Subject,
			ActorType:    actorTypeUser,
			Action:       ActionFailedSignIn,
			ResourceType: resourceTypeAuthSession,
			Metadata: metadataForClaims(claims, map[string]any{
				"reason":              "missing_tenant_context",
				"source":              cleanMetadataString(body.Source, 64),
				"requested_tenant_id": strings.TrimSpace(r.Header.Get(httpmiddleware.TenantContextHeader)),
				"token_tenant_source": tenantSource,
			}),
			TraceID: httpmiddleware.TraceIDFromContext(r.Context()),
		})
		writeError(w, r, http.StatusUnauthorized, "missing_tenant_context", "tenant context is required")
		return
	}

	event := Event{
		TenantID:     tenantID,
		ActorID:      claims.Subject,
		ActorType:    actorTypeUser,
		Action:       action,
		ResourceType: resourceTypeAuthSession,
		ScopeType:    "tenant",
		ScopeID:      tenantID,
		Metadata: metadataForClaims(claims, map[string]any{
			"source":              cleanMetadataString(body.Source, 64),
			"token_tenant_source": tenantSource,
			"user_agent":          cleanMetadataString(r.Header.Get("X-Mesha-Session-User-Agent"), 512),
		}),
		TraceID: httpmiddleware.TraceIDFromContext(r.Context()),
	}
	if err := h.record(r, event); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, errorEnvelope{
			Code:        "auth_audit_write_failed",
			Message:     "auth audit event could not be recorded",
			FieldErrors: []fieldError{},
			TraceID:     traceID(r),
			Retryable:   true,
		}, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) record(r *http.Request, event Event) error {
	if h.recorder == nil {
		return errors.New("auth audit recorder is nil")
	}
	return h.recorder.Record(r.Context(), event)
}

func normalizeAction(value string) (string, bool) {
	switch strings.TrimSpace(value) {
	case ActionSignIn:
		return ActionSignIn, true
	case ActionSessionRefresh, "":
		return ActionSessionRefresh, true
	case ActionSignOut:
		return ActionSignOut, true
	default:
		return "", false
	}
}

func tenantFromClaimsOrHeader(claims platformauth.Claims, r *http.Request) (string, string) {
	if strings.TrimSpace(claims.TenantID) != "" {
		return strings.TrimSpace(claims.TenantID), "token_claim"
	}
	return strings.TrimSpace(r.Header.Get(httpmiddleware.TenantContextHeader)), "header"
}

func metadataForClaims(claims platformauth.Claims, extra map[string]any) map[string]any {
	metadata := map[string]any{
		"issuer":           claims.Issuer,
		"audience":         claims.Audience,
		"external_subject": claims.ExternalSubject,
		"token_expires_at": claims.Expires,
	}
	if !claims.NotBefore.IsZero() {
		metadata["token_not_before_at"] = claims.NotBefore
	}
	if claims.Email != "" {
		metadata["email"] = claims.Email
	}
	if claims.EmailVerified != nil {
		metadata["email_verified"] = *claims.EmailVerified
	}
	for key, value := range extra {
		if value == nil {
			continue
		}
		if stringValue, ok := value.(string); ok && stringValue == "" {
			continue
		}
		metadata[key] = value
	}
	return metadata
}

func bearerToken(value string) (string, bool) {
	value = strings.TrimSpace(value)
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	return token, token != ""
}

func cleanMetadataString(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen > 0 && len(value) > maxLen {
		return value[:maxLen]
	}
	return value
}

func isUUIDString(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
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

type errorEnvelope struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	FieldErrors []fieldError `json:"field_errors"`
	TraceID     string       `json:"trace_id"`
	Retryable   bool         `json:"retryable"`
}

type fieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	httpresponse.WriteJSON(w, status, errorEnvelope{
		Code:        code,
		Message:     message,
		FieldErrors: []fieldError{},
		TraceID:     traceID(r),
		Retryable:   false,
	})
}

func traceID(r *http.Request) string {
	traceID := httpmiddleware.TraceIDFromContext(r.Context())
	if traceID == "" {
		return "missing-trace"
	}
	return traceID
}
