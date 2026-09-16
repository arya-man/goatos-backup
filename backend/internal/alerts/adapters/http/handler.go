// Package http serves the Alerts page: the day's alerts for one park, and the rule
// configuration behind them. Patterns must stay byte-identical to permissions/routes.go.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/alerts/app"
	"github.com/vgoats/goatos/backend/internal/alerts/domain"
	"github.com/vgoats/goatos/backend/internal/alerts/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// Service is what this transport needs.
type Service interface {
	List(ctx context.Context, tenantID, parkID, businessDate string) (app.Page, error)
	Config(ctx context.Context, tenantID string) ([]domain.RuleConfig, error)
	SetConfig(ctx context.Context, in domain.SetRuleConfig) (domain.RuleConfig, error)
	EventRules(ctx context.Context, tenantID string) ([]domain.EventRule, error)
	SetEventRule(ctx context.Context, in domain.SetEventRule) (domain.EventRule, error)
	DeleteEventRule(ctx context.Context, tenantID, ruleID string) error
}

// Handler serves the three routes.
type Handler struct {
	service Service
	log     *slog.Logger
	now     func() time.Time
}

// NewHandler constructs the transport.
func NewHandler(service Service, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{service: service, log: log, now: time.Now}
}

// Register mounts the routes.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /alerts/rows", h.Rows)
	mux.HandleFunc("GET /alerts/config", h.GetConfig)
	mux.HandleFunc("PUT /alerts/config/{rule_key}", h.SetConfig)
	mux.HandleFunc("POST /alerts/config/events", h.CreateEventRule)
	mux.HandleFunc("PUT /alerts/config/events/{rule_id}", h.UpdateEventRule)
	mux.HandleFunc("DELETE /alerts/config/events/{rule_id}", h.DeleteEventRule)
}

// rowsPayload is one park-day of alerts.
type rowsPayload struct {
	Rows         []domain.Alert   `json:"rows"`
	Total        int              `json:"total"`
	Critical     int              `json:"critical"`
	BusinessDate string           `json:"business_date"`
	ParkID       string           `json:"park_id"`
	RulesRun     []domain.RuleKey `json:"rules_run"`
	// Degraded names rules whose read failed; the rest still serve.
	Degraded []domain.RuleKey `json:"degraded,omitempty"`
}

type configPayload struct {
	Rules []domain.RuleConfig `json:"rules"`
	// EventRules are the farm's composed alerts; EventKinds the catalog they compose from.
	EventRules []domain.EventRule     `json:"event_rules"`
	EventKinds []domain.EventKindInfo `json:"event_kinds"`
}

// Rows serves GET /alerts/rows?park=&business_date=.
func (h *Handler) Rows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := strings.TrimSpace(httpmiddleware.TenantIDFromContext(ctx))
	qs := r.URL.Query()
	requestedPark := strings.TrimSpace(qs.Get("park"))
	if requestedPark == "" {
		requestedPark = strings.TrimSpace(qs.Get("park_id"))
	}
	if requestedPark != "" && !uuidutil.IsUUIDString(requestedPark) {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_park_id", "That park is not valid.")
		return
	}
	scope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(ctx, tenantID, requestedPark, permissions.AlertsRead)
	if !scope.Allowed {
		h.writeErr(w, r, scope.Status, scope.Code, scope.Message)
		return
	}
	if scope.ParkID == "" {
		// One park per request: every rule's read is bounded to a park-day. "All parks" is one
		// request per park, composed by the page from the parks option group.
		h.writeErr(w, r, http.StatusBadRequest, "park_required", "Choose a park to see its alerts.")
		return
	}
	businessDate := strings.TrimSpace(qs.Get("business_date"))
	if businessDate == "" {
		businessDate = biztime.BusinessDate(h.now())
	} else if _, err := time.Parse("2006-01-02", businessDate); err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_business_date", "That date is not valid.")
		return
	}
	page, err := h.service.List(ctx, tenantID, scope.ParkID, businessDate)
	if err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "alerts_unavailable", "Alerts could not be read right now.")
		return
	}
	critical := 0
	for _, row := range page.Rows {
		if row.Severity == domain.SeverityCritical {
			critical++
		}
	}
	rulesRun := page.RulesRun
	if rulesRun == nil {
		rulesRun = []domain.RuleKey{}
	}
	httpresponse.WriteJSON(w, http.StatusOK, rowsPayload{
		Rows: page.Rows, Total: len(page.Rows), Critical: critical,
		BusinessDate: businessDate, ParkID: scope.ParkID, RulesRun: rulesRun, Degraded: page.Degraded,
	})
}

// GetConfig serves GET /alerts/config.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(httpmiddleware.TenantIDFromContext(r.Context()))
	rules, err := h.service.Config(r.Context(), tenantID)
	if err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "alerts_config_unavailable", "Alert rules could not be read right now.")
		return
	}
	eventRules, err := h.service.EventRules(r.Context(), tenantID)
	if err != nil {
		h.writeErr(w, r, http.StatusInternalServerError, "alerts_config_unavailable", "Alert rules could not be read right now.")
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, configPayload{Rules: rules, EventRules: eventRules, EventKinds: domain.EventKinds()})
}

type eventRuleRequest struct {
	Label    *string `json:"label"`
	Kind     *string `json:"kind"`
	Severity *string `json:"severity"`
	Enabled  *bool   `json:"enabled"`
}

// CreateEventRule serves POST /alerts/config/events.
func (h *Handler) CreateEventRule(w http.ResponseWriter, r *http.Request) {
	h.writeEventRule(w, r, "")
}

// UpdateEventRule serves PUT /alerts/config/events/{rule_id}.
func (h *Handler) UpdateEventRule(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("rule_id"))
	if !uuidutil.IsUUIDString(id) {
		h.writeErr(w, r, http.StatusNotFound, "event_rule_not_found", "That alert does not exist.")
		return
	}
	h.writeEventRule(w, r, id)
}

// writeEventRule is the create/update body: every field required, the drawer sends the whole
// row it shows (a blank is not "keep the old value").
func (h *Handler) writeEventRule(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	tenantID := strings.TrimSpace(httpmiddleware.TenantIDFromContext(ctx))
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<10))
	if err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_body", "That request could not be read.")
		return
	}
	var req eventRuleRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Label == nil || req.Kind == nil || req.Severity == nil || req.Enabled == nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_body", "Send the alert's name, what it watches, its severity and whether it is on.")
		return
	}
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idem == "" {
		h.writeErr(w, r, http.StatusBadRequest, "idempotency_key_required", "An Idempotency-Key header is required.")
		return
	}
	rule, err := h.service.SetEventRule(ctx, domain.SetEventRule{
		TenantID:       tenantID,
		ActorID:        strings.TrimSpace(httpmiddleware.ActorIDFromContext(ctx)),
		ID:             id,
		Label:          *req.Label,
		Kind:           domain.EventKind(strings.TrimSpace(*req.Kind)),
		Severity:       domain.Severity(strings.TrimSpace(*req.Severity)),
		Enabled:        *req.Enabled,
		IdempotencyKey: idem,
	})
	switch {
	case errors.Is(err, domain.ErrUnknownEventKind):
		h.writeErr(w, r, http.StatusUnprocessableEntity, "unknown_event_kind", "That is not something alerts can watch yet.")
		return
	case errors.Is(err, domain.ErrInvalidEventRule):
		h.writeErr(w, r, http.StatusUnprocessableEntity, "invalid_event_rule", "Give the alert a name of up to 80 characters and a severity.")
		return
	case errors.Is(err, domain.ErrEventRuleNotFound):
		h.writeErr(w, r, http.StatusNotFound, "event_rule_not_found", "That alert does not exist.")
		return
	case errors.Is(err, ports.ErrIdempotencyConflict):
		h.writeErr(w, r, http.StatusConflict, "idempotency_conflict", "That change was already sent with different values.")
		return
	case err != nil:
		h.writeErr(w, r, http.StatusInternalServerError, "alerts_config_write_failed", "The alert could not be saved right now.")
		return
	}
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	httpresponse.WriteJSON(w, status, rule)
}

// DeleteEventRule serves DELETE /alerts/config/events/{rule_id}.
func (h *Handler) DeleteEventRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := strings.TrimSpace(httpmiddleware.TenantIDFromContext(ctx))
	id := strings.TrimSpace(r.PathValue("rule_id"))
	if !uuidutil.IsUUIDString(id) {
		h.writeErr(w, r, http.StatusNotFound, "event_rule_not_found", "That alert does not exist.")
		return
	}
	err := h.service.DeleteEventRule(ctx, tenantID, id)
	switch {
	case errors.Is(err, domain.ErrEventRuleNotFound):
		h.writeErr(w, r, http.StatusNotFound, "event_rule_not_found", "That alert does not exist.")
		return
	case err != nil:
		h.writeErr(w, r, http.StatusInternalServerError, "alerts_config_write_failed", "The alert could not be removed right now.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type setConfigRequest struct {
	Enabled   *bool `json:"enabled"`
	Threshold *int  `json:"threshold"`
}

// SetConfig serves PUT /alerts/config/{rule_key}. Both fields are required: a blank is not
// a zero and not "keep the old value" -- the drawer sends the whole row it shows.
func (h *Handler) SetConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := strings.TrimSpace(httpmiddleware.TenantIDFromContext(ctx))
	ruleKey := strings.TrimSpace(r.PathValue("rule_key"))
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<10))
	if err != nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_body", "That request could not be read.")
		return
	}
	var req setConfigRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Enabled == nil || req.Threshold == nil {
		h.writeErr(w, r, http.StatusBadRequest, "invalid_body", "Send whether the rule is on and its threshold.")
		return
	}
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idem == "" {
		h.writeErr(w, r, http.StatusBadRequest, "idempotency_key_required", "An Idempotency-Key header is required.")
		return
	}
	cfg, err := h.service.SetConfig(ctx, domain.SetRuleConfig{
		TenantID:       tenantID,
		ActorID:        strings.TrimSpace(httpmiddleware.ActorIDFromContext(ctx)),
		Key:            domain.RuleKey(ruleKey),
		Enabled:        *req.Enabled,
		Threshold:      *req.Threshold,
		IdempotencyKey: idem,
	})
	switch {
	case errors.Is(err, domain.ErrUnknownRule):
		h.writeErr(w, r, http.StatusNotFound, "unknown_rule", "That alert rule does not exist.")
		return
	case errors.Is(err, domain.ErrThresholdOutOfRange):
		h.writeErr(w, r, http.StatusUnprocessableEntity, "threshold_out_of_range", "That threshold is outside the rule's range.")
		return
	case errors.Is(err, ports.ErrIdempotencyConflict):
		h.writeErr(w, r, http.StatusConflict, "idempotency_conflict", "That change was already sent with different values.")
		return
	case err != nil:
		h.writeErr(w, r, http.StatusInternalServerError, "alerts_config_write_failed", "The rule could not be saved right now.")
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, cfg)
}

// writeErr carries the code under BOTH keys: admin-web's envelope parser reads `code` (and
// drops the envelope without it, so the drawer would show the generic failure instead of the
// range refusal), while the older `error` key is what the log and other clients read.
func (h *Handler) writeErr(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	httpresponse.WriteError(w, r, h.log, status, map[string]any{"code": code, "error": code, "message": message}, errors.New(code))
}
