package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
)

// Error is the transport-facing error shape: a stable code plus a farm-worded message.
type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string { return e.Code }

// BadRequest builds a 400 with a stable code.
func BadRequest(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusBadRequest}
}

// Conflict builds a 409 with a stable code.
func Conflict(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusConflict}
}

// Unprocessable builds a 422 with a stable code.
func Unprocessable(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusUnprocessableEntity}
}

// NotFound builds a 404.
func NotFound(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusNotFound}
}

// Internal builds a 500 that never echoes storage detail onto a screen.
func Internal(message string) *Error {
	return &Error{Code: "internal_error", Message: message, HTTPStatus: http.StatusInternalServerError}
}

// detail is the part of a wrapped domain error after the sentinel's own text: the
// question title or the rule the domain named, so the screen says WHICH answer is missing.
func detail(err error, sentinel error) string {
	msg := err.Error()
	base := sentinel.Error()
	if idx := strings.Index(msg, base); idx >= 0 {
		rest := strings.TrimSpace(strings.TrimPrefix(msg[idx+len(base):], ":"))
		if rest != "" {
			return rest
		}
	}
	return ""
}

func withDetail(prefix string, err error, sentinel error) string {
	if d := detail(err, sentinel); d != "" {
		return prefix + " " + upperFirst(d) + "."
	}
	return prefix
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// HTTPError maps a module error onto the transport shape. Every branch carries a message the
// assignee or the author can act on; unknown errors fall through to a generic 500.
func HTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrTaskNotFound), errors.Is(err, domain.ErrNotAssignee):
		return NotFound("task_not_found", "This routine check is no longer available.")
	case errors.Is(err, ports.ErrRoutineNotFound):
		return NotFound("routine_not_found", "This routine is no longer available.")
	case errors.Is(err, ErrIdempotencyKeyRequired):
		return BadRequest("missing_idempotency_key", "This could not be saved safely. Try again.")
	case errors.Is(err, domain.ErrPresenceMissing):
		return Unprocessable("presence_missing", "Check in to the pen before submitting.")
	case errors.Is(err, domain.ErrPresenceState):
		return Conflict("presence_state", "That check-in does not match where you are. Reload and try again.")
	case errors.Is(err, domain.ErrProofCount):
		return Unprocessable("proof_count", "This routine asks for a different number of photos or videos.")
	case errors.Is(err, domain.ErrQuestionProofMissing):
		return Unprocessable("question_proof_missing", "A question still needs its photo or video.")
	case errors.Is(err, domain.ErrInvalidProof):
		return Unprocessable("invalid_proof", "A capture could not be verified. Take it again with the in-app camera.")
	case errors.Is(err, domain.ErrAnswerInvalid):
		return Unprocessable("answer_invalid", withDetail("An answer is missing or not valid.", err, domain.ErrAnswerInvalid))
	case errors.Is(err, domain.ErrAlreadyDone):
		return Conflict("already_submitted", "This routine check is already submitted.")
	case errors.Is(err, domain.ErrInReview):
		return Conflict("task_in_review", "This check is with the verifier. Wait for the verdict before doing it again.")
	case errors.Is(err, domain.ErrCanceled):
		return Conflict("task_cancelled", "This routine check was cancelled.")
	case errors.Is(err, domain.ErrVersionConflict):
		return Conflict("stale_task", "This routine check changed. Reload and try again.")
	case errors.Is(err, ports.ErrRoutineVersionConflict):
		return Conflict("stale_routine", "This routine changed since you opened it. Reload and try again.")
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "This was already sent with different details.")
	case errors.Is(err, ports.ErrInvalidArgument):
		return BadRequest("invalid_request", "That request could not be read.")
	case errors.Is(err, domain.ErrInvalidEvidence):
		return Unprocessable("invalid_evidence", withDetail("The questions or capture rules are not valid.", err, domain.ErrInvalidEvidence))
	case errors.Is(err, domain.ErrNoRoles):
		return Unprocessable("no_roles", "Pick at least one role the routine is for.")
	case errors.Is(err, domain.ErrInvalidRoutine):
		return Unprocessable("invalid_routine", withDetail("The routine is not valid.", err, domain.ErrInvalidRoutine))
	case errors.Is(err, ports.ErrNameTaken):
		return Conflict("name_taken", "A routine with this name already exists in this park.")
	case errors.Is(err, ports.ErrParkImmutable):
		return Unprocessable("park_immutable", "A routine's park cannot change. Retire it and create one in the other park.")
	}
	return Internal("Something went wrong. Try again.")
}
