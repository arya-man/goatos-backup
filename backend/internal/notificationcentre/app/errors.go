package app

import (
	"errors"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/domain"
	"github.com/vgoats/goatos/backend/internal/notificationcentre/ports"
)

// Error is the transport-facing error shape: a stable code plus a message a worker can act on.
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

// Internal builds a 500 that never echoes storage detail onto a screen.
func Internal(message string) *Error {
	return &Error{Code: "internal_error", Message: message, HTTPStatus: http.StatusInternalServerError}
}

// HTTPError maps a module error onto the transport shape.
//
// NOTE what is NOT here: there is no "not yours" / 403 branch. An id the caller does not own
// is IGNORED by the mark-as-read predicate and simply does not appear in read_count.
// Answering 403 for it would confirm the row exists and belongs to somebody, which is an
// existence oracle over other people's notifications -- exactly what this module's scoping
// rule exists to prevent.
func HTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrInvalidLimit):
		return BadRequest("invalid_limit", "That page size is not valid.")
	case errors.Is(err, domain.ErrInvalidCursor), errors.Is(err, ports.ErrInvalidArgument):
		return BadRequest("invalid_cursor", "That page is not valid. Refresh your notifications.")
	case errors.Is(err, domain.ErrNoIDs):
		return BadRequest("no_notifications", "Pick at least one notification to mark as read.")
	case errors.Is(err, domain.ErrTooManyIDs):
		return BadRequest("too_many_notifications", "Mark at most 200 notifications at a time.")
	case errors.Is(err, domain.ErrInvalidID):
		return BadRequest("invalid_notification_id", "One of those notifications is not valid. Refresh your notifications.")
	case errors.Is(err, domain.ErrIdempotencyKeyRequired):
		return BadRequest("missing_idempotency_key", "This could not be saved safely. Try again.")
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "Those notifications changed after this was sent. Refresh and try again.")
	default:
		return Internal("Your notifications could not be loaded. Try again.")
	}
}
