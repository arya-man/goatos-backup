package app

import "fmt"

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

func NotFound(message string) *Error {
	return &Error{Code: "not_found_or_not_allowed", Message: message, HTTPStatus: 404}
}

func Internal(message string) *Error {
	return &Error{Code: "internal_error", Message: message, HTTPStatus: 500, Retryable: true}
}
