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

// Forbidden is 403 — the caller holds the verification permission but is asking for a
// module they do not verify. It is deliberately NOT 404: hiding the queue behind
// "not found" would be indistinguishable from an empty park and would send a verifier
// hunting a data problem that does not exist.
func Forbidden(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusForbidden}
}

// Unprocessable is 422 — used for the mandatory reject-reason gate (a syntactically valid request
// that cannot pass the verification business rule).
func Unprocessable(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusUnprocessableEntity}
}
