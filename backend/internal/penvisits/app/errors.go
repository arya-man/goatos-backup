package app

import (
	"errors"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/penvisits/ports"
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
func NotFound(message string) *Error {
	return &Error{Code: "task_not_found", Message: message, HTTPStatus: http.StatusNotFound}
}

// Internal builds a 500 that never echoes storage detail onto a screen.
func Internal(message string) *Error {
	return &Error{Code: "internal_error", Message: message, HTTPStatus: http.StatusInternalServerError}
}

// HTTPError maps a module error onto the transport shape. Every branch carries a message the
// park head can act on; unknown errors fall through to a generic 500.
func HTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrTaskNotFound):
		return NotFound("This pen visit is no longer available.")
	case errors.Is(err, ErrIdempotencyKeyRequired):
		return BadRequest("missing_idempotency_key", "This could not be saved safely. Try again.")
	case errors.Is(err, domain.ErrProofRequired):
		return Unprocessable("proof_required", "Record the pen video before submitting.")
	case errors.Is(err, domain.ErrInvalidProof):
		return Unprocessable("invalid_proof", "The video could not be verified. Record it again.")
	case errors.Is(err, domain.ErrNotAssignee):
		return NotFound("This pen visit is no longer available.")
	case errors.Is(err, domain.ErrAlreadyDone):
		return Conflict("already_submitted", "This pen visit is already submitted.")
	case errors.Is(err, domain.ErrInReview):
		return Conflict("visit_in_review", "The visit video is with the verifier. Wait for the verdict before recording again.")
	case errors.Is(err, domain.ErrCanceled):
		return Conflict("visit_cancelled", "This pen visit was cancelled.")
	case errors.Is(err, domain.ErrVersionConflict):
		return Conflict("stale_task", "This pen visit changed. Reload and try again.")
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "This was already sent with different details.")
	case errors.Is(err, ports.ErrInvalidArgument):
		return BadRequest("invalid_request", "That request could not be read.")
	}
	return Internal("Something went wrong. Try again.")
}
