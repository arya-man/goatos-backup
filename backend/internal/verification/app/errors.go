package app

import "net/http"

// FieldError names the exact request field a validation error is about. It is a plain local copy
// (not domain.FieldError) so this package does not need to import domain just for error shaping;
// the HTTP layer maps it onto domain.FieldError when writing the response envelope.
type FieldError struct {
	Field   string
	Code    string
	Message string
}

type Error struct {
	Code        string
	Message     string
	HTTPStatus  int
	Retryable   bool
	FieldErrors []FieldError
}

func (e *Error) Error() string {
	return e.Code + ": " + e.Message
}

func BadRequest(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusBadRequest}
}

// BadRequestField is a 400 that names the OFFENDING FIELD precisely, for a well-formed request
// whose value is invalid (a bad UUID, a scope-mismatched item_id) -- as opposed to a body that
// failed to parse as JSON at all. Coordinator-reported bug (2026-08-06): every such error had been
// answering the generic decodeJSON `invalid_json` code, which sent real debugging time down the
// wrong path because the body WAS valid JSON; only one field's value was wrong.
func BadRequestField(code, message, field, fieldCode, fieldMessage string) *Error {
	return &Error{
		Code: code, Message: message, HTTPStatus: http.StatusBadRequest,
		FieldErrors: []FieldError{{Field: field, Code: fieldCode, Message: fieldMessage}},
	}
}

// UnprocessableField is the 422 sibling of BadRequestField for a syntactically valid, well-typed
// value that fails a BUSINESS rule rather than a type/format rule (e.g. a queue-scoped event that
// carries an item_id at all, which parses as a fine UUID but is scope-wrong for its event_type).
func UnprocessableField(code, message, field, fieldCode, fieldMessage string) *Error {
	return &Error{
		Code: code, Message: message, HTTPStatus: http.StatusUnprocessableEntity,
		FieldErrors: []FieldError{{Field: field, Code: fieldCode, Message: fieldMessage}},
	}
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
