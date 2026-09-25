package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// OperatorShiftWriter is the read + write slice for a park's authored vaccination operator shifts
// (vaccination_operator_shift_config). It is the ONLY production write path for that table: without
// it a park added on Configuration > Items & settings could never be given vaccination operators,
// because the operator-assignment config refuses any operator without a shift row for the park.
type OperatorShiftWriter interface {
	ListOperatorShifts(ctx context.Context, tenantID, parkID string) ([]vaccexecd.OperatorShift, error)
	SetOperatorShift(ctx context.Context, req vaccexecapp.OperatorShiftRequest, in vaccexecd.OperatorShiftInput) (vaccexecd.OperatorShift, bool, *vaccexecd.OperatorShiftFieldError, error)
	ClearOperatorShift(ctx context.Context, req vaccexecapp.OperatorShiftRequest, parkID, operatorID string) (bool, error)
}

// WithOperatorShiftWriter attaches the operator-shift read/write path. A handler without it 500s
// those routes rather than silently no-op-ing.
func (h *Handler) WithOperatorShiftWriter(w OperatorShiftWriter) *Handler {
	h.operatorShiftW = w
	return h
}

// RegisterOperatorShiftRoutes mounts the operator-shift routes.
func RegisterOperatorShiftRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /vaccination/operator-shifts", h.ListOperatorShifts)
	mux.HandleFunc("PUT /vaccination/operator-shifts", h.PutOperatorShift)
	mux.HandleFunc("DELETE /vaccination/operator-shifts", h.DeleteOperatorShift)
}

// operatorShiftFieldEnvelope is a 400 that names the refused request field, so the screen can put
// the farm-worded message beside it.
type operatorShiftFieldEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
	TraceID string `json:"trace_id"`
}

type operatorShiftListResponse struct {
	ParkID string                    `json:"parkId"`
	Shifts []vaccexecd.OperatorShift `json:"shifts"`
}

type putOperatorShiftRequest struct {
	ParkID         string  `json:"park_id"`
	OperatorID     string  `json:"operator_id"`
	ShiftLabel     string  `json:"shift_label"`
	ShiftStart     string  `json:"shift_start"`
	ShiftEnd       string  `json:"shift_end"`
	WeekOffWeekday *string `json:"week_off_weekday"`
}

type putOperatorShiftResponse struct {
	Shift            vaccexecd.OperatorShift `json:"shift"`
	IdempotentReplay bool                    `json:"idempotentReplay"`
}

type clearOperatorShiftResponse struct {
	ParkID           string `json:"parkId"`
	OperatorID       string `json:"operatorId"`
	Cleared          bool   `json:"cleared"`
	IdempotentReplay bool   `json:"idempotentReplay"`
}

func (h *Handler) operatorShiftFieldError(w http.ResponseWriter, r *http.Request, code, field, message string) {
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
		operatorShiftFieldEnvelope{Code: code, Message: message, Field: field, TraceID: traceID(r)}, nil)
}

// ListOperatorShifts serves GET /vaccination/operator-shifts?park_id=: every authored shift of the
// park, whether or not the park has a drive-operator assignment yet (a brand-new park has none, and
// its shifts must still be visible so they can be set up first).
func (h *Handler) ListOperatorShifts(w http.ResponseWriter, r *http.Request) {
	if h.operatorShiftW == nil {
		h.internal(w, r, errors.New("vaccination execution: operator shift writer is not wired"))
		return
	}
	requested := strings.TrimSpace(r.URL.Query().Get("park_id"))
	if !uuidutil.IsUUIDString(requested) {
		h.operatorShiftFieldError(w, r, "invalid_park_id", "park_id", "Choose the park whose operator shifts you want to see.")
		return
	}
	parkID, ok := h.authorizedParkID(w, r, requested)
	if !ok {
		return
	}
	if parkID == "" {
		parkID = requested
	}
	shifts, err := h.operatorShiftW.ListOperatorShifts(r.Context(), tenantID(r), parkID)
	if err != nil {
		h.internal(w, r, err)
		return
	}
	if shifts == nil {
		shifts = []vaccexecd.OperatorShift{}
	}
	httpresponse.WriteJSON(w, http.StatusOK, operatorShiftListResponse{ParkID: parkID, Shifts: shifts})
}

// PutOperatorShift serves PUT /vaccination/operator-shifts: create or replace one operator's shift for
// a park. Idempotent via the Idempotency-Key header (exact replay returns the original result, the
// same key with a different body is 409). Validate-or-reject: a bad field is a 400 naming it.
func (h *Handler) PutOperatorShift(w http.ResponseWriter, r *http.Request) {
	if h.operatorShiftW == nil {
		h.internal(w, r, errors.New("vaccination execution: operator shift writer is not wired"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	defer r.Body.Close()
	var req putOperatorShiftRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && err != io.EOF {
		h.badRequest(w, r, "invalid_body", "The shift form could not be read. Reload and try again.")
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		h.badRequest(w, r, "missing_idempotency_key", "Idempotency-Key header is required")
		return
	}
	parkID, ok := h.operatorShiftParkAndOperator(w, r, req.ParkID, req.OperatorID)
	if !ok {
		return
	}
	shift, replay, ferr, err := h.operatorShiftW.SetOperatorShift(r.Context(), h.operatorShiftRequest(r, idempotencyKey), vaccexecd.OperatorShiftInput{
		ParkID:         parkID,
		OperatorID:     strings.TrimSpace(req.OperatorID),
		ShiftLabel:     req.ShiftLabel,
		ShiftStart:     req.ShiftStart,
		ShiftEnd:       req.ShiftEnd,
		WeekOffWeekday: req.WeekOffWeekday,
	})
	if ferr != nil {
		h.operatorShiftFieldError(w, r, ferr.Code, ferr.Field, ferr.Message)
		return
	}
	if err != nil {
		h.operatorShiftWriteError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, putOperatorShiftResponse{Shift: shift, IdempotentReplay: replay})
}

// DeleteOperatorShift serves DELETE /vaccination/operator-shifts?park_id=&operator_id=: clear one
// operator's shift for a park. Refused (409) while the park's drive-operator assignment still names
// the operator. Idempotent via the Idempotency-Key header.
func (h *Handler) DeleteOperatorShift(w http.ResponseWriter, r *http.Request) {
	if h.operatorShiftW == nil {
		h.internal(w, r, errors.New("vaccination execution: operator shift writer is not wired"))
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		h.badRequest(w, r, "missing_idempotency_key", "Idempotency-Key header is required")
		return
	}
	operatorID := strings.TrimSpace(r.URL.Query().Get("operator_id"))
	parkID, ok := h.operatorShiftParkAndOperator(w, r, r.URL.Query().Get("park_id"), operatorID)
	if !ok {
		return
	}
	replay, err := h.operatorShiftW.ClearOperatorShift(r.Context(), h.operatorShiftRequest(r, idempotencyKey), parkID, operatorID)
	if err != nil {
		h.operatorShiftWriteError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, clearOperatorShiftResponse{ParkID: parkID, OperatorID: operatorID, Cleared: true, IdempotentReplay: replay})
}

// operatorShiftParkAndOperator shape-checks the two ids and clamps the park to the caller's grants.
func (h *Handler) operatorShiftParkAndOperator(w http.ResponseWriter, r *http.Request, rawPark, rawOperator string) (string, bool) {
	requested := strings.TrimSpace(rawPark)
	if !uuidutil.IsUUIDString(requested) {
		h.operatorShiftFieldError(w, r, "invalid_park_id", "park_id", "Choose the park this shift is for.")
		return "", false
	}
	if !uuidutil.IsUUIDString(strings.TrimSpace(rawOperator)) {
		h.operatorShiftFieldError(w, r, "invalid_operator_id", "operator_id", "Choose the operator this shift is for.")
		return "", false
	}
	parkID, ok := h.authorizedParkID(w, r, requested)
	if !ok {
		return "", false
	}
	if parkID == "" {
		parkID = requested
	}
	return parkID, true
}

func (h *Handler) operatorShiftRequest(r *http.Request, idempotencyKey string) vaccexecapp.OperatorShiftRequest {
	return vaccexecapp.OperatorShiftRequest{
		TenantID:       tenantID(r),
		ActorID:        strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context())),
		IdempotencyKey: idempotencyKey,
		TraceID:        traceID(r),
	}
}

func (h *Handler) operatorShiftWriteError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, vaccexecapp.ErrOperatorNotActiveInPark):
		h.operatorShiftFieldError(w, r, "operator_not_in_park", "operator_id",
			"This person is not an active worker of this park, so they cannot be a vaccination operator here. Set their home park on People first.")
	case errors.Is(err, vaccexecapp.ErrOperatorShiftInUse):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "operator_shift_in_use", Message: "This operator is the park's default or selected drive operator. Choose another operator under Drive operator assignment and save, then clear this shift.", TraceID: traceID(r)}, nil)
	case errors.Is(err, vaccexecapp.ErrOperatorShiftNotFound):
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			errorEnvelope{Code: "operator_shift_not_found", Message: "This operator has no shift set for this park.", TraceID: traceID(r)}, nil)
	case errors.Is(err, vaccexecapp.ErrOperatorShiftIdempotencyConflict):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			errorEnvelope{Code: "idempotency_conflict", Message: "Idempotency-Key was already used with a different request body", TraceID: traceID(r)}, nil)
	default:
		h.internal(w, r, err)
	}
}
