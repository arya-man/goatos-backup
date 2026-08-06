package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// The authored health-protocol API behind the /health/config screen.
//
// # THE IDEMPOTENCY CONTRACT, AS IMPLEMENTED HERE
//
// Every mutating route requires an Idempotency-Key header and derives the request fingerprint from
// the CANONICAL CLIENT REQUEST ONLY -- tenant, command name, route, and the normalized body.
// Nothing server-generated (the actor, a timestamp, the business date) is folded in, for the same
// reason feedconfig excludes them: a legitimate retry from a second session, or one that crossed
// midnight IST, would otherwise hash differently and be rejected as a payload conflict.
//
// The command name is part of the hash so the same body posted to two different write routes can
// never collide on one key.
//
// # VALIDATE OR REJECT
//
// duration_days arrives as *int and stays a pointer into the service, which keeps ABSENT
// distinguishable from an authored value. Absent gets the declared default; a present but
// out-of-range value is a field error and is never rewritten to a default the author did not type.
// A cleared number field in the editor sends absent, not 0.
//
// # WHY VALIDATION ERRORS ARE A LIST
//
// A 28-step protocol rejected one field per round trip is not authorable. Every field error from
// one validation pass is returned together, each naming `steps[i].field`, so the editor can mark
// every offending row at once.

const (
	protocolsRoute       = "/health-config/protocols"
	protocolDetailRoute  = "/health-config/protocols/{protocol_version_id}"
	protocolPublishRoute = "/health-config/protocols/{protocol_version_id}/publish"
	protocolDiscardRoute = "/health-config/protocols/{protocol_version_id}/discard"
	protocolDraftRoute   = "/health-config/drafts"
	protocolSaveRoute    = "/health-config/drafts/save"
	diseasesRoute        = "/health-config/diseases"

	createDiseaseCommand = "healthconfig.disease.create"
	saveDraftCommand     = "healthconfig.draft.save"
	publishDraftCommand  = "healthconfig.draft.publish"
	discardDraftCommand  = "healthconfig.draft.discard"
)

// ConfigService is the slice of health/app.ConfigService this handler needs.
type ConfigService interface {
	ListProtocolCatalog(ctx context.Context, q domain.ProtocolCatalogQuery) (domain.ProtocolCatalogPage, error)
	GetProtocolDetail(ctx context.Context, tenantID, protocolVersionID string) (domain.ProtocolDetail, error)
	GetDraftForEdit(ctx context.Context, cmd domain.ProtocolVersionCommand, diseaseKey, ageBand string) (domain.ProtocolDetail, error)
	CreateDisease(ctx context.Context, cmd domain.CreateDiseaseCommand) (domain.AuthoringResult, error)
	SaveDraft(ctx context.Context, cmd domain.SaveDraftCommand) (domain.AuthoringResult, error)
	PublishDraft(ctx context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error)
	DiscardDraft(ctx context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error)
}

type ConfigHandler struct {
	svc ConfigService
	log *slog.Logger
}

func NewConfigHandler(svc ConfigService, log *slog.Logger) *ConfigHandler {
	if log == nil {
		log = slog.Default()
	}
	return &ConfigHandler{svc: svc, log: log}
}

func RegisterConfig(mux *http.ServeMux, h *ConfigHandler) {
	mux.HandleFunc("GET "+protocolsRoute, h.ListProtocolCatalog)
	mux.HandleFunc("GET "+protocolDetailRoute, h.GetProtocolDetail)
	mux.HandleFunc("POST "+diseasesRoute, h.CreateDisease)
	mux.HandleFunc("POST "+protocolDraftRoute, h.OpenDraft)
	mux.HandleFunc("POST "+protocolSaveRoute, h.SaveDraft)
	mux.HandleFunc("POST "+protocolPublishRoute, h.PublishDraft)
	mux.HandleFunc("POST "+protocolDiscardRoute, h.DiscardDraft)
}

// ---------------------------------------------------------------------------
// Request shapes
// ---------------------------------------------------------------------------

type createDiseaseRequest struct {
	DisplayName string `json:"display_name"`
	// Optional. Omitted by the UI (the key is derived from the name); accepted so a seed command
	// can reproduce the imported keys exactly.
	DiseaseKey   string `json:"disease_key"`
	DurationDays *int   `json:"duration_days"`
}

type openDraftRequest struct {
	DiseaseKey string `json:"disease_key"`
	AgeBand    string `json:"age_band"`
}

type authoredStepRequest struct {
	DayNo              int    `json:"day_no"`
	Session            string `json:"session"`
	RecordType         string `json:"record_type"`
	MedicineName       string `json:"medicine_name"`
	DosageText         string `json:"dosage_text"`
	DosageDenominator  string `json:"dosage_denominator"`
	MedicineRoute      string `json:"medicine_route"`
	Instruction        string `json:"instruction"`
	CriticalActionType string `json:"critical_action_type"`
}

type saveDraftRequest struct {
	DiseaseKey   string                `json:"disease_key"`
	AgeBand      string                `json:"age_band"`
	DisplayName  string                `json:"display_name"`
	DurationDays *int                  `json:"duration_days"`
	Steps        []authoredStepRequest `json:"steps"`
}

// validationErrorResponse carries the field errors alongside the standard error envelope, so a
// client can render the summary message AND mark the individual rows.
type validationErrorResponse struct {
	Code      string              `json:"code"`
	Message   string              `json:"message"`
	TraceID   string              `json:"trace_id"`
	Retryable bool                `json:"retryable"`
	Errors    []domain.FieldError `json:"errors"`
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

func (h *ConfigHandler) ListProtocolCatalog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_limit", "limit must be positive", err)
			return
		}
		limit = n
	}
	page, err := h.svc.ListProtocolCatalog(r.Context(), domain.ProtocolCatalogQuery{
		TenantID:  httpmiddleware.TenantIDFromContext(r.Context()),
		AgeBand:   q.Get("age_band"),
		Search:    q.Get("search"),
		DraftOnly: strings.EqualFold(strings.TrimSpace(q.Get("draft_only")), "true"),
		Cursor:    q.Get("cursor"),
		Limit:     limit,
	})
	if err != nil {
		h.writeConfigError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, page)
}

func (h *ConfigHandler) GetProtocolDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := h.svc.GetProtocolDetail(r.Context(),
		httpmiddleware.TenantIDFromContext(r.Context()), r.PathValue("protocol_version_id"))
	if err != nil {
		h.writeConfigError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, detail)
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

func (h *ConfigHandler) CreateDisease(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeConfigRequest[createDiseaseRequest](w, r, h)
	if !ok {
		return
	}
	idem, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	duration := 0
	if req.DurationDays != nil {
		duration = *req.DurationDays
	}
	res, err := h.svc.CreateDisease(r.Context(), domain.CreateDiseaseCommand{
		TenantID:           httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:            httpmiddleware.ActorIDFromContext(r.Context()),
		DiseaseKey:         req.DiseaseKey,
		DisplayName:        req.DisplayName,
		DurationDays:       duration,
		IdempotencyKey:     idem,
		RequestFingerprint: configFingerprint(createDiseaseCommand, body),
	})
	if err != nil {
		h.writeConfigError(w, r, err)
		return
	}
	status := http.StatusCreated
	if res.IdempotentReplay {
		status = http.StatusOK
	}
	httpresponse.WriteJSON(w, status, res)
}

// OpenDraft returns the open draft for a protocol, creating it as a copy of the published version
// if none is open. It is a POST because it may create a row, and it carries no idempotency key
// because at most one draft can exist per protocol -- the uniqueness constraint IS the idempotency.
func (h *ConfigHandler) OpenDraft(w http.ResponseWriter, r *http.Request) {
	_, req, ok := decodeConfigRequest[openDraftRequest](w, r, h)
	if !ok {
		return
	}
	detail, err := h.svc.GetDraftForEdit(r.Context(), domain.ProtocolVersionCommand{
		TenantID: httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:  httpmiddleware.ActorIDFromContext(r.Context()),
	}, req.DiseaseKey, req.AgeBand)
	if err != nil {
		h.writeConfigError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, detail)
}

func (h *ConfigHandler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeConfigRequest[saveDraftRequest](w, r, h)
	if !ok {
		return
	}
	idem, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	// Absent duration takes the declared default. A PRESENT out-of-range value is passed through
	// verbatim so the service rejects it -- it is never coerced.
	duration := domain.DefaultDurationDays
	if req.DurationDays != nil {
		duration = *req.DurationDays
	}
	steps := make([]domain.AuthoredStep, 0, len(req.Steps))
	for _, s := range req.Steps {
		steps = append(steps, domain.AuthoredStep{
			DayNo:              s.DayNo,
			Session:            s.Session,
			RecordType:         s.RecordType,
			MedicineName:       s.MedicineName,
			DosageText:         s.DosageText,
			DosageDenominator:  s.DosageDenominator,
			MedicineRoute:      s.MedicineRoute,
			Instruction:        s.Instruction,
			CriticalActionType: s.CriticalActionType,
		})
	}
	res, err := h.svc.SaveDraft(r.Context(), domain.SaveDraftCommand{
		TenantID:   httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:    httpmiddleware.ActorIDFromContext(r.Context()),
		DiseaseKey: req.DiseaseKey,
		AgeBand:    req.AgeBand,
		Protocol: domain.AuthoredProtocol{
			DisplayName:  req.DisplayName,
			DurationDays: duration,
			Steps:        steps,
		},
		IdempotencyKey:     idem,
		RequestFingerprint: configFingerprint(saveDraftCommand, body),
	})
	if err != nil {
		h.writeConfigError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, res)
}

func (h *ConfigHandler) PublishDraft(w http.ResponseWriter, r *http.Request) {
	h.versionCommand(w, r, publishDraftCommand, h.svc.PublishDraft)
}

func (h *ConfigHandler) DiscardDraft(w http.ResponseWriter, r *http.Request) {
	h.versionCommand(w, r, discardDraftCommand, h.svc.DiscardDraft)
}

// versionCommand is the shared shape of publish and discard: both address one version by id,
// carry no body beyond the path, and return an AuthoringResult.
//
// The fingerprint covers the command name and the version id rather than the (empty) body, so two
// different versions published under one reused key are correctly detected as a payload conflict
// instead of silently replaying the first publish.
func (h *ConfigHandler) versionCommand(
	w http.ResponseWriter, r *http.Request, command string,
	run func(context.Context, domain.ProtocolVersionCommand) (domain.AuthoringResult, error),
) {
	versionID := r.PathValue("protocol_version_id")
	idem, ok := h.idempotencyKey(w, r)
	if !ok {
		return
	}
	res, err := run(r.Context(), domain.ProtocolVersionCommand{
		TenantID:           httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:            httpmiddleware.ActorIDFromContext(r.Context()),
		ProtocolVersionID:  versionID,
		IdempotencyKey:     idem,
		RequestFingerprint: configFingerprint(command, []byte(versionID)),
	})
	if err != nil {
		h.writeConfigError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, res)
}

// ---------------------------------------------------------------------------
// Plumbing
// ---------------------------------------------------------------------------

func (h *ConfigHandler) idempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idem == "" {
		h.writeError(w, r, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key is required", nil)
		return "", false
	}
	return idem, true
}

func decodeConfigRequest[T any](w http.ResponseWriter, r *http.Request, h *ConfigHandler) ([]byte, T, bool) {
	var zero T
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_body", "invalid request body", err)
		return nil, zero, false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	// Unknown fields are REJECTED, not ignored. A client that renamed a field would otherwise
	// silently author a protocol with the old value still in place -- the accept-and-discard
	// failure mode AGENTS.md bans for fixture loaders, applied to the write path.
	dec.DisallowUnknownFields()
	var v T
	if err := dec.Decode(&v); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_body", "invalid request body", err)
		return nil, zero, false
	}
	return body, v, true
}

func configFingerprint(command string, body []byte) string {
	sum := sha256.Sum256(append([]byte(command+"\x1f"), body...))
	return hex.EncodeToString(sum[:])
}

func (h *ConfigHandler) writeConfigError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, validationErrorResponse{
			Code:      "invalid_protocol",
			Message:   "Fix the highlighted fields and try again.",
			TraceID:   httpmiddleware.TraceIDFromContext(r.Context()),
			Retryable: false,
			Errors:    ve.Errors,
		}, err)
		return
	}
	switch {
	case errors.Is(err, ports.ErrNotFound):
		h.writeError(w, r, http.StatusNotFound, "not_found", "That protocol no longer exists.", err)
	case errors.Is(err, ports.ErrDraftExists):
		h.writeError(w, r, http.StatusConflict, "draft_exists", "Someone else already has a draft open for this protocol.", err)
	case errors.Is(err, ports.ErrDiseaseExists):
		h.writeError(w, r, http.StatusConflict, "disease_exists", "A disease with that name already exists.", err)
	case errors.Is(err, ports.ErrNotADraft):
		h.writeError(w, r, http.StatusConflict, "not_a_draft", "Only a draft can be published or discarded.", err)
	case errors.Is(err, ports.ErrProtocolInUse):
		h.writeError(w, r, http.StatusConflict, "protocol_in_use", "This protocol is being used by an open case.", err)
	case errors.Is(err, ports.ErrConflict):
		h.writeError(w, r, http.StatusConflict, "idempotency_conflict", "That change was already submitted with different content.", err)
	default:
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal error", err)
	}
}

func (h *ConfigHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error) {
	httpresponse.WriteError(w, r, h.log, status, errorResponse{
		Code:      code,
		Message:   message,
		TraceID:   httpmiddleware.TraceIDFromContext(r.Context()),
		Retryable: status >= 500,
	}, cause)
}
