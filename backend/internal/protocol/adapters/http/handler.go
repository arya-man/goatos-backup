// Package http exposes the protocol config API (definitions, versions, rules, publish).
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/protocol/app"
	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

// ProtocolConfig is the slice of the protocol service this handler needs.
type ProtocolConfig interface {
	CreateDefinition(ctx context.Context, in domain.NewDefinition) (string, error)
	CreateVersion(ctx context.Context, in domain.NewVersion) (string, error)
	AddRule(ctx context.Context, in domain.NewRule) (string, error)
	GetVersion(ctx context.Context, tenantID, versionID string) (domain.Version, error)
	PublishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string, idempotencyKey ...string) error
	ListConfigs(ctx context.Context, tenantID, category string) ([]domain.ConfigListItem, error)
	ListAnimalStages(ctx context.Context, tenantID string) ([]domain.AnimalStage, error)
}

// Handler serves the protocol config endpoints.
type Handler struct {
	config ProtocolConfig
	log    *slog.Logger
}

// NewHandler constructs the handler with an optional logger.
func NewHandler(config ProtocolConfig, log ...*slog.Logger) *Handler {
	var l *slog.Logger
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	} else {
		l = slog.Default()
	}
	return &Handler{config: config, log: l}
}

// Register mounts the protocol config routes.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /protocols", h.ListConfigs)
	mux.HandleFunc("GET /protocols/animal-stages", h.ListAnimalStages)
	mux.HandleFunc("POST /protocols", h.CreateDefinition)
	mux.HandleFunc("POST /protocols/{protocol_id}/versions", h.CreateVersion)
	mux.HandleFunc("POST /protocols/versions/{version_id}/rules", h.AddRule)
	mux.HandleFunc("GET /protocols/versions/{version_id}", h.GetVersion)
	mux.HandleFunc("POST /protocols/versions/{version_id}/publish", h.PublishVersion)
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

// ---- create definition ----

type createDefinitionRequest struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

func (h *Handler) CreateDefinition(w http.ResponseWriter, r *http.Request) {
	idempotencyKey, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var req createDefinitionRequest
	if !h.decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Category) == "" {
		h.badRequest(w, r, "missing_required_field", "code, name, and category are required")
		return
	}
	id, err := h.config.CreateDefinition(r.Context(), domain.NewDefinition{
		TenantID: tenantID(r), Code: req.Code, Name: req.Name, Category: req.Category,
		Status: "draft", CreatedBy: actorPtr(r), IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, ports.ErrIdempotencyConflict) {
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "idempotency_conflict", Message: "idempotency key was reused with a different protocol definition payload", TraceID: traceID(r)}, nil)
		return
	}
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, map[string]string{"protocol_id": id})
}

// ---- create version (always draft) ----

type createVersionRequest struct {
	ScopeType     string          `json:"scope_type"`
	ScopeID       *string         `json:"scope_id"`
	Version       int32           `json:"version"`
	VersionLabel  string          `json:"version_label"`
	EffectiveFrom time.Time       `json:"effective_from"`
	EffectiveTo   *time.Time      `json:"effective_to"`
	RuleDsl       json.RawMessage `json:"rule_dsl"`
	ProofPolicy   json.RawMessage `json:"proof_policy"`
	SopVersionID  *string         `json:"sop_version_id"`
}

func (h *Handler) CreateVersion(w http.ResponseWriter, r *http.Request) {
	idempotencyKey, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var req createVersionRequest
	if !h.decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(r.PathValue("protocol_id")) == "" || strings.TrimSpace(req.ScopeType) == "" || req.Version <= 0 || req.EffectiveFrom.IsZero() || len(req.RuleDsl) == 0 {
		h.badRequest(w, r, "missing_required_field", "protocol_id, scope_type, version, effective_from, and rule_dsl are required")
		return
	}
	id, err := h.config.CreateVersion(r.Context(), domain.NewVersion{
		TenantID: tenantID(r), ProtocolID: r.PathValue("protocol_id"),
		ScopeType: req.ScopeType, ScopeID: req.ScopeID, Version: req.Version, VersionLabel: req.VersionLabel,
		Status: "draft", EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo,
		RuleDsl: rawOrEmpty(req.RuleDsl), ProofPolicy: rawOrEmpty(req.ProofPolicy),
		SopVersionID: req.SopVersionID, DraftedBy: actorPtr(r), IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, ports.ErrIdempotencyConflict) {
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "idempotency_conflict", Message: "idempotency key was reused with a different protocol version payload", TraceID: traceID(r)}, nil)
		return
	}
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, map[string]string{"protocol_version_id": id})
}

// ---- add rule ----

type addRuleRequest struct {
	DoseCode            string          `json:"dose_code"`
	Sequence            int32           `json:"sequence"`
	TriggerType         string          `json:"trigger_type"`
	OffsetDays          int32           `json:"offset_days"`
	DueWindowDays       int32           `json:"due_window_days"`
	MinGapDays          int32           `json:"min_gap_days"`
	Repeat              string          `json:"repeat"`
	RepeatUntilAfterAge string          `json:"repeat_until_after_age"`
	CatchUp             string          `json:"catch_up"`
	EligibilityJSON     json.RawMessage `json:"eligibility_json"`
	ProofPolicy         json.RawMessage `json:"proof_policy"`
	WithdrawalDays      *int32          `json:"withdrawal_days"`
	SortOrder           int32           `json:"sort_order"`
}

func (h *Handler) AddRule(w http.ResponseWriter, r *http.Request) {
	idempotencyKey, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	var req addRuleRequest
	if !h.decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(r.PathValue("version_id")) == "" ||
		strings.TrimSpace(req.DoseCode) == "" ||
		req.Sequence <= 0 ||
		strings.TrimSpace(req.TriggerType) == "" ||
		strings.TrimSpace(req.Repeat) == "" ||
		strings.TrimSpace(req.CatchUp) == "" ||
		len(req.EligibilityJSON) == 0 {
		h.badRequest(w, r, "missing_required_field", "version_id, dose_code, sequence, trigger_type, repeat, catch_up, and eligibility_json are required")
		return
	}
	id, err := h.config.AddRule(r.Context(), domain.NewRule{
		TenantID: tenantID(r), ProtocolVersionID: r.PathValue("version_id"),
		DoseCode: req.DoseCode, Sequence: req.Sequence, TriggerType: req.TriggerType,
		OffsetDays: req.OffsetDays, DueWindowDays: req.DueWindowDays, MinGapDays: req.MinGapDays,
		Repeat: req.Repeat, RepeatUntilAfterAge: req.RepeatUntilAfterAge, CatchUp: req.CatchUp,
		EligibilityJSON: rawOrEmpty(req.EligibilityJSON), ProofPolicy: rawOrEmpty(req.ProofPolicy),
		WithdrawalDays: req.WithdrawalDays, SortOrder: req.SortOrder, CreatedBy: actorPtr(r), IdempotencyKey: idempotencyKey,
	})
	if errors.Is(err, ports.ErrIdempotencyConflict) {
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "idempotency_conflict", Message: "idempotency key was reused with a different protocol rule payload", TraceID: traceID(r)}, nil)
		return
	}
	if errors.Is(err, ports.ErrVersionNotDraft) {
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "version_not_draft", Message: "published protocol versions are immutable; create a new draft version", TraceID: traceID(r)}, nil)
		return
	}
	if errors.Is(err, app.ErrUnsupportedRepeatPolicy) {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			errorEnvelope{Code: "unsupported_repeat_policy", Message: "repeat must be one of none, every_n_days, or yearly", TraceID: traceID(r)}, nil)
		return
	}
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, map[string]string{"rule_id": id})
}

// ---- list configs (Config authority screen, B3) ----

type configItemResponse struct {
	ProtocolID        string     `json:"protocol_id"`
	Code              string     `json:"code"`
	Name              string     `json:"name"`
	Category          string     `json:"category"`
	ProtocolVersionID string     `json:"protocol_version_id"`
	Version           int32      `json:"version"`
	VersionLabel      string     `json:"version_label"`
	ScopeType         string     `json:"scope_type"`
	ScopeID           string     `json:"scope_id,omitempty"`
	ScopeLabel        string     `json:"scope_label"`
	Status            string     `json:"status"`
	EffectiveFrom     *time.Time `json:"effective_from,omitempty"`
	EffectiveTo       *time.Time `json:"effective_to,omitempty"`
	SopVersionID      string     `json:"sop_version_id,omitempty"`
	PublishedBy       string     `json:"published_by,omitempty"`
	PublishedAt       *time.Time `json:"published_at,omitempty"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
	SourceSystem      string     `json:"source_system"`
	SourceRef         string     `json:"source_ref"`
	ReviewStatus      string     `json:"review_status"`
	ApprovedBy        string     `json:"approved_by"`
	ApprovedAt        string     `json:"approved_at"`
	RuleCount         int32      `json:"rule_count"`
}

type configListResponse struct {
	Category string               `json:"category"`
	Items    []configItemResponse `json:"items"`
}

// ListConfigs serves GET /protocols?category=… — the generic Config authority list. Defaults to the
// vaccination category (the only visible content slice today). Read-only; the source-backed publish
// gate is enforced by PublishVersion, this list simply surfaces each version's source-review state.
func (h *Handler) ListConfigs(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	if category == "" {
		category = "vaccination"
	}
	items, err := h.config.ListConfigs(r.Context(), tenantID(r), category)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	resp := configListResponse{Category: category, Items: make([]configItemResponse, 0, len(items))}
	for _, it := range items {
		resp.Items = append(resp.Items, configItemResponse{
			ProtocolID: it.ProtocolID, Code: it.Code, Name: it.Name, Category: it.Category,
			ProtocolVersionID: it.ProtocolVersionID, Version: it.Version, VersionLabel: it.VersionLabel,
			ScopeType: it.ScopeType, ScopeID: it.ScopeID, ScopeLabel: it.ScopeLabel, Status: it.Status,
			EffectiveFrom: it.EffectiveFrom, EffectiveTo: it.EffectiveTo,
			SopVersionID: it.SopVersionID, PublishedBy: it.PublishedBy,
			PublishedAt: it.PublishedAt, UpdatedAt: it.UpdatedAt,
			SourceSystem: it.SourceSystem, SourceRef: it.SourceRef,
			ReviewStatus: it.ReviewStatus, ApprovedBy: it.ApprovedBy,
			ApprovedAt: it.ApprovedAt, RuleCount: it.RuleCount,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// ---- list animal stages (Config authoring reference data) ----

type animalStageResponse struct {
	AnimalStageID string `json:"animal_stage_id"`
	StageCode     string `json:"stage_code"`
	Name          string `json:"name"`
	MinAgeDays    *int32 `json:"min_age_days,omitempty"`
	MaxAgeDays    *int32 `json:"max_age_days,omitempty"`
	SortOrder     int32  `json:"sort_order"`
}

type animalStageListResponse struct {
	Items []animalStageResponse `json:"items"`
}

// ListAnimalStages serves GET /protocols/animal-stages — the tenant's active animal_stage_lookup
// rows, so the Config authoring stage picker is backend-driven (PHC vaccination TRD: stage bands
// live in the lookup, not in frontend literals). Read-only; an empty list is honest (no stages
// seeded yet) and the UI shows a seed-stages empty state rather than falling back to hardcoded codes.
func (h *Handler) ListAnimalStages(w http.ResponseWriter, r *http.Request) {
	stages, err := h.config.ListAnimalStages(r.Context(), tenantID(r))
	if err != nil {
		h.internal(w, r, err)
		return
	}
	resp := animalStageListResponse{Items: make([]animalStageResponse, 0, len(stages))}
	for _, s := range stages {
		resp.Items = append(resp.Items, animalStageResponse{
			AnimalStageID: s.AnimalStageID, StageCode: s.StageCode, Name: s.Name,
			MinAgeDays: s.MinAgeDays, MaxAgeDays: s.MaxAgeDays, SortOrder: s.SortOrder,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// ---- get version ----

type versionResponse struct {
	ProtocolVersionID string          `json:"protocol_version_id"`
	ProtocolID        string          `json:"protocol_id"`
	ScopeType         string          `json:"scope_type"`
	ScopeID           string          `json:"scope_id"`
	Version           int32           `json:"version"`
	Status            string          `json:"status"`
	EffectiveFrom     *time.Time      `json:"effective_from,omitempty"`
	EffectiveTo       *time.Time      `json:"effective_to,omitempty"`
	RuleDsl           json.RawMessage `json:"rule_dsl"`
	ProofPolicy       json.RawMessage `json:"proof_policy"`
	SopVersionID      string          `json:"sop_version_id,omitempty"`
	RowVersion        int32           `json:"row_version"`
}

func (h *Handler) GetVersion(w http.ResponseWriter, r *http.Request) {
	v, err := h.config.GetVersion(r.Context(), tenantID(r), r.PathValue("version_id"))
	if errors.Is(err, ports.ErrNotFound) {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found", Message: "protocol version not found", TraceID: traceID(r)}, nil)
		return
	}
	if err != nil {
		h.internal(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, versionResponse{
		ProtocolVersionID: v.ProtocolVersionID, ProtocolID: v.ProtocolID,
		ScopeType: v.ScopeType, ScopeID: v.ScopeID, Version: v.Version, Status: v.Status,
		EffectiveFrom: v.EffectiveFrom, EffectiveTo: v.EffectiveTo,
		RuleDsl: rawOrNull(v.RuleDsl), ProofPolicy: rawOrNull(v.ProofPolicy),
		SopVersionID: v.SopVersionID, RowVersion: v.RowVersion,
	})
}

// ---- publish (source-backed gate) ----

func (h *Handler) PublishVersion(w http.ResponseWriter, r *http.Request) {
	idempotencyKey, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	err := h.config.PublishVersion(r.Context(), tenantID(r), r.PathValue("version_id"), actorPtr(r), idempotencyKey)
	if errors.Is(err, app.ErrNotPublishable) {
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity,
			errorEnvelope{Code: "not_publishable", Message: err.Error(), TraceID: traceID(r)}, nil)
		return
	}
	if errors.Is(err, ports.ErrIdempotencyConflict) {
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "idempotency_conflict", Message: "idempotency key was reused with a different protocol publish payload", TraceID: traceID(r)}, nil)
		return
	}
	if errors.Is(err, ports.ErrVersionNotDraft) {
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "version_not_draft", Message: "only draft protocol versions can be published", TraceID: traceID(r)}, nil)
		return
	}
	if errors.Is(err, ports.ErrNotFound) {
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "not_found", Message: "protocol version not found", TraceID: traceID(r)}, nil)
		return
	}
	if err != nil {
		h.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.badRequest(w, r, "invalid_body", "request body could not be read")
		return false
	}
	if len(body) == 0 {
		h.badRequest(w, r, "empty_body", "request body is required")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		h.badRequest(w, r, "invalid_json", "request body is not valid JSON")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		h.badRequest(w, r, "invalid_json", "request body must contain exactly one JSON object")
		return false
	}
	return true
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, code, msg string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
		errorEnvelope{Code: code, Message: msg, TraceID: traceID(r)}, nil)
}

func (h *Handler) idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 8 || len(key) > 200 {
		h.badRequest(w, r, "invalid_idempotency_key", "Idempotency-Key header must be 8-200 characters")
		return "", false
	}
	return key, true
}

func (h *Handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
		errorEnvelope{Code: "internal_error", Message: "internal server error", TraceID: traceID(r)}, err)
}

func rawOrEmpty(m json.RawMessage) []byte {
	if len(m) == 0 {
		return []byte(`{}`)
	}
	return m
}

func rawOrNull(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(`null`)
	}
	return json.RawMessage(b)
}

func tenantID(r *http.Request) string { return httpmiddleware.TenantIDFromContext(r.Context()) }

func actorPtr(r *http.Request) *string {
	if a := httpmiddleware.ActorIDFromContext(r.Context()); a != "" {
		return &a
	}
	return nil
}

func traceID(r *http.Request) string {
	if t := httpmiddleware.TraceIDFromContext(r.Context()); t != "" {
		return t
	}
	return "missing-trace"
}
