package authaudit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const (
	ActionSignIn         = "auth.sign_in"
	ActionSessionRefresh = "auth.session_refresh"
	ActionSignOut        = "auth.sign_out"
	ActionFailedSignIn   = "auth.failed_sign_in"

	resourceTypeAuthSession = "auth_session"
	actorTypeUser           = "user"
	headerSessionUserAgent  = "X-GoatOS-Session-User-Agent"
)

type TokenVerifier interface {
	Verify(token string) (platformauth.Claims, error)
}

type Recorder interface {
	Record(ctx context.Context, event Event) error
}

type Handler struct {
	verifier         TokenVerifier
	recorder         Recorder
	log              *slog.Logger
	allowedTenantIDs map[string]struct{}
	allowedEmails    authallow.EmailSet
	dynamicEmails    authallow.DynamicEmailSource
	emailConfigErr   error
	rateLimiter      *RateLimiter
	grantClaimer     permissions.PendingEmailGrantClaimer
}

type Option func(*Handler)

func WithAllowedTenantIDs(tenantIDs []string) Option {
	return func(h *Handler) {
		allowed := make(map[string]struct{}, len(tenantIDs))
		for _, tenantID := range tenantIDs {
			tenantID = strings.ToLower(strings.TrimSpace(tenantID))
			if tenantID == "" {
				continue
			}
			allowed[tenantID] = struct{}{}
		}
		if len(allowed) > 0 {
			h.allowedTenantIDs = allowed
		}
	}
}

func WithAllowedEmails(emails []string) Option {
	return func(h *Handler) {
		allowed, err := authallow.NewEmailSet(emails)
		if err != nil {
			h.emailConfigErr = err
			return
		}
		h.allowedEmails = allowed
	}
}

// WithDynamicAllowedEmails adds the DB-backed allowlist source, consulted in
// UNION with the static env set (authallow.AllowsWithDynamic) so a person
// created in-app can record sign-in session events without a deploy.
func WithDynamicAllowedEmails(source authallow.DynamicEmailSource) Option {
	return func(h *Handler) {
		h.dynamicEmails = source
	}
}

func WithRateLimiter(limiter *RateLimiter) Option {
	return func(h *Handler) {
		h.rateLimiter = limiter
	}
}

func WithPendingEmailGrantClaimer(claimer permissions.PendingEmailGrantClaimer) Option {
	return func(h *Handler) {
		h.grantClaimer = claimer
	}
}

func NewHandler(verifier TokenVerifier, recorder Recorder, log *slog.Logger, opts ...Option) *Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &Handler{verifier: verifier, recorder: recorder, log: log}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("POST /auth/session-events", h.RecordSessionEvent)
}

func (h *Handler) RecordSessionEvent(w http.ResponseWriter, r *http.Request) {
	if h.verifier == nil || h.recorder == nil {
		writeError(w, r, http.StatusServiceUnavailable, "auth_audit_unconfigured", "auth audit is not configured")
		return
	}
	if h.emailConfigErr != nil {
		writeError(w, r, http.StatusServiceUnavailable, "auth_email_allowlist_invalid", "auth email allowlist is invalid")
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
	if !authallow.AllowsWithDynamic(r.Context(), h.allowedEmails, h.dynamicEmails, tenantID, claims.Email, claims.EmailVerified) {
		requestedTenantID := strings.TrimSpace(r.Header.Get(httpmiddleware.TenantContextHeader))
		if !h.allowRateLimited(r, claims, "email_not_allowed|"+tenantID) {
			writeError(w, r, http.StatusTooManyRequests, "auth_session_rate_limited", "too many auth session events")
			return
		}
		if err := h.record(r, Event{
			TenantID:     "",
			ActorID:      claims.Subject,
			ActorType:    actorTypeUser,
			Action:       ActionFailedSignIn,
			ResourceType: resourceTypeAuthSession,
			Metadata: metadataForClaims(claims, map[string]any{
				"reason":              "email_not_allowed",
				"source":              cleanMetadataString(body.Source, 64),
				"requested_tenant_id": requestedTenantID,
				"resolved_tenant_id":  tenantID,
				"token_tenant_source": tenantSource,
			}),
			TraceID: httpmiddleware.TraceIDFromContext(r.Context()),
		}); err != nil {
			h.log.WarnContext(r.Context(), "auth_failed_sign_in_audit_write_failed",
				slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
				slog.String("trace_id", traceID(r)),
				slog.String("actor_id", claims.Subject),
				slog.String("email", authallow.NormalizeEmail(claims.Email)),
				slog.String("requested_tenant_id", cleanMetadataString(requestedTenantID, 64)),
				slog.String("resolved_tenant_id", tenantID),
				slog.String("token_tenant_source", tenantSource),
				slog.String("error", err.Error()),
			)
		}
		writeError(w, r, http.StatusForbidden, "email_not_allowed", "this Google account is not allowed for Mesha Admin")
		return
	}
	if !uuidutil.IsUUIDString(tenantID) {
		requestedTenantID := strings.TrimSpace(r.Header.Get(httpmiddleware.TenantContextHeader))
		if !h.allowRateLimited(r, claims, "missing_tenant_context") {
			writeError(w, r, http.StatusTooManyRequests, "auth_session_rate_limited", "too many auth session events")
			return
		}
		if err := h.record(r, Event{
			TenantID:     "",
			ActorID:      claims.Subject,
			ActorType:    actorTypeUser,
			Action:       ActionFailedSignIn,
			ResourceType: resourceTypeAuthSession,
			Metadata: metadataForClaims(claims, map[string]any{
				"reason":              "missing_tenant_context",
				"source":              cleanMetadataString(body.Source, 64),
				"requested_tenant_id": requestedTenantID,
				"token_tenant_source": tenantSource,
			}),
			TraceID: httpmiddleware.TraceIDFromContext(r.Context()),
		}); err != nil {
			h.log.WarnContext(r.Context(), "auth_failed_sign_in_audit_write_failed",
				slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
				slog.String("trace_id", traceID(r)),
				slog.String("actor_id", claims.Subject),
				slog.String("requested_tenant_id", cleanMetadataString(requestedTenantID, 64)),
				slog.String("token_tenant_source", tenantSource),
				slog.String("error", err.Error()),
			)
		}
		writeError(w, r, http.StatusUnauthorized, "missing_tenant_context", "tenant context is required")
		return
	}
	if !h.tenantAllowed(tenantID) {
		if !h.allowRateLimited(r, claims, tenantID) {
			writeError(w, r, http.StatusTooManyRequests, "auth_session_rate_limited", "too many auth session events")
			return
		}
		requestedTenantID := strings.TrimSpace(r.Header.Get(httpmiddleware.TenantContextHeader))
		if err := h.record(r, Event{
			TenantID:     "",
			ActorID:      claims.Subject,
			ActorType:    actorTypeUser,
			Action:       ActionFailedSignIn,
			ResourceType: resourceTypeAuthSession,
			Metadata: metadataForClaims(claims, map[string]any{
				"reason":              "tenant_not_allowed",
				"source":              cleanMetadataString(body.Source, 64),
				"requested_tenant_id": requestedTenantID,
				"resolved_tenant_id":  tenantID,
				"token_tenant_source": tenantSource,
			}),
			TraceID: httpmiddleware.TraceIDFromContext(r.Context()),
		}); err != nil {
			h.log.WarnContext(r.Context(), "auth_failed_sign_in_audit_write_failed",
				slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
				slog.String("trace_id", traceID(r)),
				slog.String("actor_id", claims.Subject),
				slog.String("requested_tenant_id", cleanMetadataString(requestedTenantID, 64)),
				slog.String("resolved_tenant_id", tenantID),
				slog.String("token_tenant_source", tenantSource),
				slog.String("error", err.Error()),
			)
		}
		writeError(w, r, http.StatusForbidden, "tenant_not_allowed", "tenant context is not allowed for auth session audit")
		return
	}
	if !h.allowRateLimited(r, claims, tenantID) {
		writeError(w, r, http.StatusTooManyRequests, "auth_session_rate_limited", "too many auth session events")
		return
	}

	grantClaim, err := h.claimPendingEmailGrant(r, action, claims, tenantID, body.Source)
	if err != nil {
		h.log.ErrorContext(r.Context(), "auth_pending_email_grant_failed",
			slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
			slog.String("trace_id", traceID(r)),
			slog.String("actor_id", claims.Subject),
			slog.String("email", authallow.NormalizeEmail(claims.Email)),
			slog.String("tenant_id", tenantID),
			slog.String("error", err.Error()),
		)
		writeError(w, r, http.StatusInternalServerError, "auth_pending_email_grant_failed", "pending email grant could not be claimed")
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
			"user_agent":          cleanMetadataString(r.Header.Get(headerSessionUserAgent), 512),
			"pending_email_grant": grantClaimMetadata(grantClaim),
		}),
		TraceID: httpmiddleware.TraceIDFromContext(r.Context()),
	}
	if err := h.record(r, event); err != nil {
		h.log.ErrorContext(r.Context(), "auth_session_event_failed",
			slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
			slog.String("trace_id", traceID(r)),
			slog.String("event_type", action),
			slog.String("actor_id", claims.Subject),
			slog.String("email", authallow.NormalizeEmail(claims.Email)),
			slog.String("firebase_uid", claims.ExternalSubject),
			slog.String("tenant_id", tenantID),
			slog.String("source", cleanMetadataString(body.Source, 64)),
			slog.String("error", err.Error()),
		)
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError, errorEnvelope{
			Code:        "auth_audit_write_failed",
			Message:     "auth audit event could not be recorded",
			FieldErrors: []fieldError{},
			TraceID:     traceID(r),
			Retryable:   true,
		}, err)
		return
	}
	h.log.InfoContext(r.Context(), "auth_session_event_succeeded",
		slog.String("request_id", httpmiddleware.RequestIDFromContext(r.Context())),
		slog.String("trace_id", traceID(r)),
		slog.String("event_type", action),
		slog.String("actor_id", claims.Subject),
		slog.String("email", authallow.NormalizeEmail(claims.Email)),
		slog.String("firebase_uid", claims.ExternalSubject),
		slog.String("tenant_id", tenantID),
		slog.String("source", cleanMetadataString(body.Source, 64)),
		slog.String("token_tenant_source", tenantSource),
		slog.Bool("pending_email_grant_matched", grantClaim.Matched),
		slog.Int("pending_email_grant_inserted", len(grantClaim.InsertedGrants)),
		slog.Int("pending_email_grant_existing", len(grantClaim.ExistingGrants)),
	)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) claimPendingEmailGrant(r *http.Request, action string, claims platformauth.Claims, tenantID, source string) (permissions.PendingEmailGrantResult, error) {
	if h.grantClaimer == nil || action != ActionSignIn {
		return permissions.PendingEmailGrantResult{}, nil
	}
	if claims.EmailVerified == nil || !*claims.EmailVerified || strings.TrimSpace(claims.Email) == "" {
		return permissions.PendingEmailGrantResult{}, nil
	}
	return h.grantClaimer.ClaimPendingEmailGrant(r.Context(), permissions.PendingEmailGrantClaim{
		TenantID:        tenantID,
		UserID:          claims.Subject,
		Email:           claims.Email,
		ExternalSubject: claims.ExternalSubject,
		Issuer:          claims.Issuer,
		Source:          cleanMetadataString(source, 64),
		TraceID:         httpmiddleware.TraceIDFromContext(r.Context()),
	})
}

func grantClaimMetadata(result permissions.PendingEmailGrantResult) any {
	if !result.Matched {
		return nil
	}
	return map[string]any{
		"pending_grant_ids": result.PendingGrantIDs,
		"inserted_grants":   result.InsertedGrants,
		"existing_grants":   result.ExistingGrants,
	}
}

func (h *Handler) record(r *http.Request, event Event) error {
	if h.recorder == nil {
		return errors.New("auth audit recorder is nil")
	}
	return h.recorder.Record(r.Context(), event)
}

func (h *Handler) tenantAllowed(tenantID string) bool {
	if len(h.allowedTenantIDs) == 0 {
		return true
	}
	_, ok := h.allowedTenantIDs[strings.ToLower(strings.TrimSpace(tenantID))]
	return ok
}

func (h *Handler) allowRateLimited(r *http.Request, claims platformauth.Claims, tenantID string) bool {
	if h.rateLimiter == nil {
		return true
	}
	keyParts := []string{
		strings.TrimSpace(claims.Subject),
		strings.TrimSpace(tenantID),
		clientIP(r),
	}
	return h.rateLimiter.Allow(strings.Join(keyParts, "|"))
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

func clientIP(r *http.Request) string {
	if forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwardedFor != "" {
		if first, _, ok := strings.Cut(forwardedFor, ","); ok {
			return strings.TrimSpace(first)
		}
		return forwardedFor
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func cleanMetadataString(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 {
		return value
	}
	count := 0
	for i := range value {
		if count == maxLen {
			return value[:i]
		}
		count++
	}
	return value
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
