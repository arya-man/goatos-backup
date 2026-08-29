package app

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// ErrIdempotencyKeyRequired reports a mutating call with no Idempotency-Key header.
var ErrIdempotencyKeyRequired = errors.New("toxin: idempotency key required")

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
	return &Error{Code: "not_found", Message: message, HTTPStatus: http.StatusNotFound}
}

// Internal builds a 500 that never echoes storage detail onto a screen.
func Internal(message string) *Error {
	return &Error{Code: "internal_error", Message: message, HTTPStatus: http.StatusInternalServerError}
}

// HTTPError maps a toxin error onto the transport shape. Every branch carries a message
// the tester or reviewer can act on; unknown errors fall through to a generic 500.
func HTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrTaskNotFound):
		return NotFound("Toxin test not found.")
	case errors.Is(err, domain.ErrProofRequired), errors.Is(err, ports.ErrInvalidProof):
		return Unprocessable("proof_required",
			"A capture taken with the in-app camera is required for this step.")
	case errors.Is(err, domain.ErrInvalidOutcome):
		return BadRequest("invalid_outcome", "Record the strip as Negative, Positive, or Invalid strip.")
	case errors.Is(err, domain.ErrTaskNotOpen):
		return Conflict("test_not_open", "This test is not open for step work. Refresh the list.")
	case errors.Is(err, domain.ErrUnknownStep):
		return BadRequest("unknown_step", "That step is not part of this test.")
	case errors.Is(err, domain.ErrStepNotCompletable):
		return BadRequest("step_not_completable", "That step is not completed this way.")
	case errors.Is(err, domain.ErrStepOutOfOrder):
		return Conflict("step_out_of_order", "Finish the earlier steps first.")
	case errors.Is(err, domain.ErrProofAlreadyUsed):
		return Conflict("proof_already_used",
			"That recording is already saved against another step. Record this step on its own.")
	case errors.Is(err, domain.ErrStepAlreadyDone):
		return Conflict("step_already_done", "This step is already completed. Refresh the test.")
	case errors.Is(err, domain.ErrWaitNotElapsed):
		var wait domain.WaitNotElapsed
		if errors.As(err, &wait) {
			remaining := time.Until(wait.OpensAt).Round(time.Minute)
			minutes := int(remaining / time.Minute)
			if minutes < 1 {
				minutes = 1
			}
			return Unprocessable("wait_not_elapsed",
				fmt.Sprintf("The waiting time is not over. This step opens in about %d min.", minutes))
		}
		return Unprocessable("wait_not_elapsed", "The waiting time is not over yet.")
	case errors.Is(err, domain.ErrNotReviewable):
		return Conflict("test_not_awaiting_review", "This test is not waiting for review. Refresh the list.")
	case errors.Is(err, domain.ErrInvalidVerdict):
		return BadRequest("invalid_verdict", "The review must accept or reject the test.")
	case errors.Is(err, domain.ErrRejectReasonRequired):
		return BadRequest("reject_reason_required", "Tell the testers why this test is being sent back.")
	case errors.Is(err, ports.ErrVersionConflict):
		return Conflict("version_conflict", "This test changed after it was opened. Reload and review it again.")
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "This form changed after it was submitted. Reload and try again.")
	case errors.Is(err, ports.ErrInvalidArgument):
		return BadRequest("invalid_cursor", "That page cursor is not valid. Refresh the list.")
	case errors.Is(err, ErrIdempotencyKeyRequired):
		return BadRequest("missing_idempotency_key", "This could not be recorded safely. Try again.")
	default:
		return Internal("That could not be recorded. Try again.")
	}
}
