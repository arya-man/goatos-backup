package app

import (
	"errors"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
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

// Forbidden builds a 403 with a stable code.
func Forbidden(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusForbidden}
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

// HTTPError maps a module error onto the transport shape. Every branch carries a message
// the director or CXO can act on; unknown errors fall through to a generic 500.
func HTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrTaskNotFound):
		return NotFound("This task is no longer available.")
	case errors.Is(err, domain.ErrTitleRequired):
		return BadRequest("title_required", "Give the task a short title.")
	case errors.Is(err, domain.ErrTitleTooLong):
		return BadRequest("title_too_long", "Keep the title under 80 characters.")
	case errors.Is(err, domain.ErrBodyTooLong):
		return BadRequest("body_too_long", "Keep the brief under 4000 characters.")
	case errors.Is(err, domain.ErrCommentTooLong):
		return BadRequest("comment_too_long", "Keep the comment under 2000 characters.")
	case errors.Is(err, domain.ErrAssigneeRequired):
		return BadRequest("assignee_required", "Choose who this task is for.")
	case errors.Is(err, domain.ErrSelfAssignment):
		return BadRequest("self_assignment", "A task is raised for someone else, not for yourself.")
	case errors.Is(err, domain.ErrAssigneeNotCXO):
		return BadRequest("assignee_not_available", "That person is not on the leadership desk. Choose from the list.")
	case errors.Is(err, domain.ErrTooManyAttachments):
		return BadRequest("too_many_attachments", "Attach at most 12 items to one task.")
	case errors.Is(err, domain.ErrInvalidAttachmentKind):
		return BadRequest("invalid_attachment_kind", "That attachment type is not supported.")
	case errors.Is(err, domain.ErrDuplicateAttachment):
		return BadRequest("duplicate_attachment", "That attachment is already on the task.")
	case errors.Is(err, domain.ErrFileNameTooLong):
		return BadRequest("file_name_too_long", "That file name is too long.")
	case errors.Is(err, domain.ErrAttachmentProofRequired), errors.Is(err, ports.ErrInvalidAttachment),
		errors.Is(err, domain.ErrAttachmentNotCompleted), errors.Is(err, domain.ErrAttachmentNotAnAttachmnt):
		return Unprocessable("invalid_attachment", "One of the attachments did not finish uploading. Remove it and try again.")
	case errors.Is(err, domain.ErrInvalidStatus):
		return BadRequest("invalid_status", "That status is not one this task can take.")
	case errors.Is(err, domain.ErrInvalidStatusTransition):
		return Unprocessable("invalid_status_transition", "This task cannot move to that status from where it is. Refresh and try again.")
	case errors.Is(err, domain.ErrTaskClosed):
		return Conflict("task_closed", "This task is closed and can no longer be changed.")
	case errors.Is(err, domain.ErrNotRaiser):
		return Forbidden("not_raiser", "Only the person who raised this task can change it.")
	case errors.Is(err, domain.ErrNotAssignee):
		return Forbidden("not_assignee", "Only the person this task is for can update its status.")
	case errors.Is(err, ports.ErrVersionConflict):
		return Conflict("version_conflict", "This task changed after it was opened. Reload and try again.")
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "This form changed after it was sent. Reload and try again.")
	case errors.Is(err, ports.ErrInvalidArgument):
		return BadRequest("invalid_cursor", "That page is not valid. Refresh the list.")
	case errors.Is(err, ErrIdempotencyKeyRequired):
		return BadRequest("missing_idempotency_key", "This could not be saved safely. Try again.")
	default:
		return Internal("That could not be saved. Try again.")
	}
}
