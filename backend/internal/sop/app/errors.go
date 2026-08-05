package app

import "net/http"

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
	Retryable  bool
}

func (e *Error) Error() string {
	return e.Code + ": " + e.Message
}

func BadRequest(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusBadRequest}
}

func NotFound(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusNotFound}
}

func Conflict(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusConflict}
}

// RetryableConflict is Conflict for the subset of 409s that a bounded client retry can plausibly
// resolve on its own: SubmitTask's ports.ErrConflict ("write_conflict") fires when sop_tasks.state
// was 'accepted' at read time. Before ReopenTaskForRework existed, that state never changed back on
// its own, so retrying was pointless and Conflict's default Retryable: false was correct. Now a
// concurrent verifier rejection can flip 'accepted' -> 'rework_requested' moments after this read,
// so the SAME conflict the client hit on attempt 1 can legitimately succeed on attempt 2 without any
// client-side state change. Scoped to ports.ErrConflict only -- the sibling SOP-version conflicts
// (stale_sop_version, missing_sop_version, sop_version_not_executable) stay non-retryable via plain
// Conflict: no amount of retrying fixes a client pinned to the wrong/unpublished SOP version.
func RetryableConflict(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusConflict, Retryable: true}
}

func Forbidden(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusForbidden}
}
