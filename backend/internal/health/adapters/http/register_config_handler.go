package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// The authored diagnosis-register API behind the second tab of /health/config.
//
// It shares the protocol handler's idempotency contract exactly -- an Idempotency-Key
// header on every write, a fingerprint over the command name and the canonical client
// body only, nothing server-generated folded in, so a legitimate retry from a second
// session or one that crossed midnight IST is still a replay rather than a conflict.
//
// Unknown fields are REJECTED on the way in, twice: once by the request decoder and
// again by the strict document loader. A register key nothing reads is an
// accept-and-discard -- it looks authored, changes no behaviour, and reads to the next
// author as already honoured -- and on a clinical rule table that is the worst
// available failure mode.

const (
	registersRoute       = "/health-config/registers"
	registerDetailRoute  = "/health-config/registers/{register_version_id}"
	registerPublishRoute = "/health-config/registers/{register_version_id}/publish"
	registerDiscardRoute = "/health-config/registers/{register_version_id}/discard"
	registerDraftRoute   = "/health-config/registers/drafts"
	registerSaveRoute    = "/health-config/registers/drafts/save"

	openRegisterDraftCommand    = "healthconfig.register.draft.open"
	saveRegisterDraftCommand    = "healthconfig.register.draft.save"
	publishRegisterDraftCommand = "healthconfig.register.draft.publish"
	discardRegisterDraftCommand = "healthconfig.register.draft.discard"
)

// RegisterConfigService is the slice of health/app.RegisterConfigService this handler needs.
type RegisterConfigService interface {
	ListRegisters(ctx context.Context, tenantID string) ([]domain.RegisterSummary, error)
	GetRegister(ctx context.Context, tenantID, registerVersionID string) (domain.RegisterDetail, error)
	GetDraftForEdit(ctx context.Context, cmd domain.RegisterVersionCommand, animalClass string) (domain.RegisterDetail, error)
	SaveDraft(ctx context.Context, cmd domain.SaveRegisterDraftCommand) (domain.RegisterAuthoringResult, error)
	PublishDraft(ctx context.Context, cmd domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error)
	DiscardDraft(ctx context.Context, cmd domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error)
}

type RegisterConfigHandler struct {
	svc RegisterConfigService
	log *slog.Logger
}

func NewRegisterConfigHandler(svc RegisterConfigService, log *slog.Logger) *RegisterConfigHandler {
	if log == nil {
		log = slog.Default()
	}
	return &RegisterConfigHandler{svc: svc, log: log}
}

func RegisterRegisterConfig(mux *http.ServeMux, h *RegisterConfigHandler) {
	mux.HandleFunc("GET "+registersRoute, h.ListRegisters)
	mux.HandleFunc("GET "+registerDetailRoute, h.GetRegister)
	mux.HandleFunc("POST "+registerDraftRoute, h.OpenDraft)
	mux.HandleFunc("POST "+registerSaveRoute, h.SaveDraft)
	mux.HandleFunc("POST "+registerPublishRoute, h.PublishDraft)
	mux.HandleFunc("POST "+registerDiscardRoute, h.DiscardDraft)
}

type openRegisterDraftRequest struct {
	AnimalClass string `json:"animal_class"`
}

type saveRegisterDraftRequest struct {
	AnimalClass string                      `json:"animal_class"`
	Document    *diagnosis.AuthoredRegister `json:"document"`
}

func (h *RegisterConfigHandler) ListRegisters(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListRegisters(r.Context(), httpmiddleware.TenantIDFromContext(r.Context()))
	if err != nil {
		h.writeRegisterError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]any{"registers": out})
}

func (h *RegisterConfigHandler) GetRegister(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.GetRegister(r.Context(),
		httpmiddleware.TenantIDFromContext(r.Context()), r.PathValue("register_version_id"))
	if err != nil {
		h.writeRegisterError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

// OpenDraft returns the open draft for a class, creating it from the live register when
// there is none. Opening the editor is what creates a draft, so a vet never has to
// decide to start one -- and an edit always begins from what is currently live.
func (h *RegisterConfigHandler) OpenDraft(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeRegisterRequest[openRegisterDraftRequest](w, r, h)
	if !ok {
		return
	}
	idem, ok := h.registerIdempotencyKey(w, r)
	if !ok {
		return
	}
	out, err := h.svc.GetDraftForEdit(r.Context(), domain.RegisterVersionCommand{
		TenantID:           httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:            httpmiddleware.ActorIDFromContext(r.Context()),
		IdempotencyKey:     idem,
		RequestFingerprint: configFingerprint(openRegisterDraftCommand, body),
	}, req.AnimalClass)
	if err != nil {
		h.writeRegisterError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

func (h *RegisterConfigHandler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeRegisterRequest[saveRegisterDraftRequest](w, r, h)
	if !ok {
		return
	}
	if req.Document == nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_body", "The register document is missing.", nil)
		return
	}
	idem, ok := h.registerIdempotencyKey(w, r)
	if !ok {
		return
	}
	res, err := h.svc.SaveDraft(r.Context(), domain.SaveRegisterDraftCommand{
		TenantID:           httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:            httpmiddleware.ActorIDFromContext(r.Context()),
		AnimalClass:        req.AnimalClass,
		Document:           *req.Document,
		IdempotencyKey:     idem,
		RequestFingerprint: configFingerprint(saveRegisterDraftCommand, body),
	})
	if err != nil {
		h.writeRegisterError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, res)
}

func (h *RegisterConfigHandler) PublishDraft(w http.ResponseWriter, r *http.Request) {
	h.registerVersionCommand(w, r, publishRegisterDraftCommand, h.svc.PublishDraft)
}

func (h *RegisterConfigHandler) DiscardDraft(w http.ResponseWriter, r *http.Request) {
	h.registerVersionCommand(w, r, discardRegisterDraftCommand, h.svc.DiscardDraft)
}

// registerVersionCommand is the shared shape of publish and discard. The fingerprint
// covers the command name and the version id rather than the empty body, so two
// different versions published under one reused key are detected as a payload conflict
// instead of silently replaying the first publish.
func (h *RegisterConfigHandler) registerVersionCommand(
	w http.ResponseWriter, r *http.Request, command string,
	run func(context.Context, domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error),
) {
	versionID := r.PathValue("register_version_id")
	idem, ok := h.registerIdempotencyKey(w, r)
	if !ok {
		return
	}
	res, err := run(r.Context(), domain.RegisterVersionCommand{
		TenantID:           httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:            httpmiddleware.ActorIDFromContext(r.Context()),
		RegisterVersionID:  versionID,
		IdempotencyKey:     idem,
		RequestFingerprint: configFingerprint(command, []byte(versionID)),
	})
	if err != nil {
		h.writeRegisterError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, res)
}

func (h *RegisterConfigHandler) registerIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	return (&ConfigHandler{log: h.log}).idempotencyKey(w, r)
}

func (h *RegisterConfigHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error) {
	(&ConfigHandler{log: h.log}).writeError(w, r, status, code, message, cause)
}

func decodeRegisterRequest[T any](w http.ResponseWriter, r *http.Request, h *RegisterConfigHandler) ([]byte, T, bool) {
	return decodeConfigRequest[T](w, r, &ConfigHandler{log: h.log})
}

func (h *RegisterConfigHandler) writeRegisterError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		// Every problem from one validation pass comes back together, each naming its
		// own path. A register with forty questions rejected one field per round trip
		// is not authorable -- and the two-direction check usually fires in pairs,
		// because an author adding a symptom and the rule that reads it gets both
		// halves wrong at once or neither.
		httpresponse.WriteError(w, r, h.log, http.StatusUnprocessableEntity, validationErrorResponse{
			Code:      "invalid_register",
			Message:   "Fix the highlighted problems and try again.",
			TraceID:   httpmiddleware.TraceIDFromContext(r.Context()),
			Retryable: false,
			Errors:    ve.Errors,
		}, err)
		return
	}
	switch {
	case errors.Is(err, ports.ErrRegisterNotFound):
		h.writeError(w, r, http.StatusNotFound, "not_found", "That register no longer exists.", err)
	case errors.Is(err, ports.ErrRegisterDraftExists):
		h.writeError(w, r, http.StatusConflict, "draft_exists", "Someone else already has a draft open for this register.", err)
	case errors.Is(err, ports.ErrRegisterNotADraft):
		h.writeError(w, r, http.StatusConflict, "not_a_draft", "Only a draft can be published or discarded.", err)
	case errors.Is(err, ports.ErrConflict):
		h.writeError(w, r, http.StatusConflict, "idempotency_conflict", "That change was already submitted with different content.", err)
	default:
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "internal error", err)
	}
}
