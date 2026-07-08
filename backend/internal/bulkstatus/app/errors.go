package app

import "fmt"

// Error is the app-layer error carrying a stable code + HTTP status so the HTTP
// adapter can render a consistent envelope. It mirrors the identity module's
// error shape on purpose so both modules render identically.
type Error struct {
	Code       string
	Message    string
	HTTPStatus int
	Retryable  bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func BadRequest(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: 400}
}

func Unauthorized(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: 401}
}

func Conflict(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: 409}
}

func NotImplemented(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: 501}
}

func Internal(message string) *Error {
	return &Error{Code: "internal_error", Message: message, HTTPStatus: 500, Retryable: true}
}
