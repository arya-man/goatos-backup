package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// The ROUTING API behind the Types panel of /health/config: which diagnosis types a farm has,
// and which animals reach each one (migration 000395).
//
// It shares the register handler's idempotency contract exactly -- an Idempotency-Key header on
// every write, a fingerprint over the command name and the canonical client body only -- so a
// retry from a second session, or one that crossed midnight IST, is a replay rather than a
// conflict. Unknown fields are REJECTED, for the same reason they are on the register: a key
// nothing reads looks authored, changes no behaviour, and reads to the next author as honoured.

const (
	diagnosisTypesRoute       = "/health-config/diagnosis-types"
	diagnosisTypeSaveRoute    = "/health-config/diagnosis-types/save"
	diagnosisRouteSaveRoute   = "/health-config/diagnosis-routes/save"
	diagnosisRouteDeleteRoute = "/health-config/diagnosis-routes/delete"

	saveDiagnosisTypeCommand    = "healthconfig.diagnosis_type.save"
	saveDiagnosisRouteCommand   = "healthconfig.diagnosis_route.save"
	deleteDiagnosisRouteCommand = "healthconfig.diagnosis_route.delete"
)

// DiagnosisTypeService is the handler's view of the routing service.
type DiagnosisTypeService interface {
	Routing(ctx context.Context, tenantID string) (domain.DiagnosisRoutingView, error)
	SaveType(ctx context.Context, cmd domain.SaveDiagnosisTypeCommand) (domain.DiagnosisType, error)
	SaveRoute(ctx context.Context, cmd domain.SaveStageRouteCommand) (domain.StageRouteRow, error)
	DeleteRoute(ctx context.Context, cmd domain.DeleteStageRouteCommand) error
}

type DiagnosisTypeHandler struct {
	svc DiagnosisTypeService
	log *slog.Logger
}

func NewDiagnosisTypeHandler(svc DiagnosisTypeService, log *slog.Logger) *DiagnosisTypeHandler {
	if log == nil {
		log = slog.Default()
	}
	return &DiagnosisTypeHandler{svc: svc, log: log}
}

func RegisterDiagnosisTypes(mux *http.ServeMux, h *DiagnosisTypeHandler) {
	mux.HandleFunc("GET "+diagnosisTypesRoute, h.Routing)
	mux.HandleFunc("POST "+diagnosisTypeSaveRoute, h.SaveType)
	mux.HandleFunc("POST "+diagnosisRouteSaveRoute, h.SaveRoute)
	mux.HandleFunc("POST "+diagnosisRouteDeleteRoute, h.DeleteRoute)
}

type saveDiagnosisTypeRequest struct {
	TypeKey   string `json:"type_key"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	SortOrder int    `json:"sort_order"`
}

type saveDiagnosisRouteRequest struct {
	AgeBand   string `json:"age_band"`
	StageCode string `json:"stage_code"`
	TypeKey   string `json:"type_key"`
	SubStage  string `json:"sub_stage"`
}

type deleteDiagnosisRouteRequest struct {
	AgeBand   string `json:"age_band"`
	StageCode string `json:"stage_code"`
}

// Routing serves the whole Types panel in one read: the types, the routes with their live
// animal counts, and the stages that hold animals and reach nothing.
func (h *DiagnosisTypeHandler) Routing(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Routing(r.Context(), httpmiddleware.TenantIDFromContext(r.Context()))
	if err != nil {
		h.writeTypeError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

func (h *DiagnosisTypeHandler) SaveType(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeTypeRequest[saveDiagnosisTypeRequest](w, r, h)
	if !ok {
		return
	}
	idem, ok := h.typeIdempotencyKey(w, r)
	if !ok {
		return
	}
	out, err := h.svc.SaveType(r.Context(), domain.SaveDiagnosisTypeCommand{
		TenantID:           httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:            httpmiddleware.ActorIDFromContext(r.Context()),
		IdempotencyKey:     idem,
		RequestFingerprint: configFingerprint(saveDiagnosisTypeCommand, body),
		TypeKey:            req.TypeKey,
		Label:              req.Label,
		Status:             req.Status,
		SortOrder:          req.SortOrder,
	})
	if err != nil {
		h.writeTypeError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

func (h *DiagnosisTypeHandler) SaveRoute(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeTypeRequest[saveDiagnosisRouteRequest](w, r, h)
	if !ok {
		return
	}
	idem, ok := h.typeIdempotencyKey(w, r)
	if !ok {
		return
	}
	out, err := h.svc.SaveRoute(r.Context(), domain.SaveStageRouteCommand{
		TenantID:           httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:            httpmiddleware.ActorIDFromContext(r.Context()),
		IdempotencyKey:     idem,
		RequestFingerprint: configFingerprint(saveDiagnosisRouteCommand, body),
		AgeBand:            req.AgeBand,
		StageCode:          req.StageCode,
		TypeKey:            req.TypeKey,
		SubStage:           req.SubStage,
	})
	if err != nil {
		h.writeTypeError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

func (h *DiagnosisTypeHandler) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	body, req, ok := decodeTypeRequest[deleteDiagnosisRouteRequest](w, r, h)
	if !ok {
		return
	}
	idem, ok := h.typeIdempotencyKey(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteRoute(r.Context(), domain.DeleteStageRouteCommand{
		TenantID:           httpmiddleware.TenantIDFromContext(r.Context()),
		ActorID:            httpmiddleware.ActorIDFromContext(r.Context()),
		IdempotencyKey:     idem,
		RequestFingerprint: configFingerprint(deleteDiagnosisRouteCommand, body),
		AgeBand:            req.AgeBand,
		StageCode:          req.StageCode,
	}); err != nil {
		h.writeTypeError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h *DiagnosisTypeHandler) typeIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	return (&ConfigHandler{log: h.log}).idempotencyKey(w, r)
}

func decodeTypeRequest[T any](w http.ResponseWriter, r *http.Request, h *DiagnosisTypeHandler) ([]byte, T, bool) {
	return decodeConfigRequest[T](w, r, &ConfigHandler{log: h.log})
}

// writeTypeError maps each refusal to the status that says what the author should do next.
//
// The distinctions are the point. A key already taken and a still-routed type are both 409s
// but different sentences; a route naming an inactive type is a 422 because the fix is in the
// request, not in the world. Every message is the domain's own words, which is what keeps the
// screen from having to compose its own explanation of a rule it does not own.
func (h *DiagnosisTypeHandler) writeTypeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrTypeKeyInUse):
		h.writeError(w, r, http.StatusConflict, "diagnosis_type_exists", err.Error(), err)
	case errors.Is(err, domain.ErrBuiltinTypeNotRetirable):
		h.writeError(w, r, http.StatusConflict, "diagnosis_type_builtin", err.Error(), err)
	case errors.Is(err, domain.ErrTypeStillRouted):
		h.writeError(w, r, http.StatusConflict, "diagnosis_type_still_routed", err.Error(), err)
	case errors.Is(err, domain.ErrRouteTypeUnknown):
		h.writeError(w, r, http.StatusUnprocessableEntity, "diagnosis_route_type_unknown", err.Error(), err)
	case errors.Is(err, domain.ErrInvalidHealthConfig):
		h.writeError(w, r, http.StatusUnprocessableEntity, "invalid_diagnosis_config", err.Error(), err)
	case errors.Is(err, domain.ErrTypeNotFound), errors.Is(err, ports.ErrNotFound):
		h.writeError(w, r, http.StatusNotFound, "not_found", "That is no longer there.", err)
	default:
		h.log.Error("diagnosis type config", "error", err)
		h.writeError(w, r, http.StatusInternalServerError, "internal_error", "Something went wrong.", err)
	}
}

func (h *DiagnosisTypeHandler) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, cause error) {
	(&ConfigHandler{log: h.log}).writeError(w, r, status, code, message, cause)
}
