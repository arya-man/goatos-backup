package app

import "net/http"

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
	// Field names the form question a refusal is about, "" when the error is not about one.
	// The transport carries it as `field` so a form can show the reason in place.
	Field string
}

func (e *Error) Error() string {
	return e.Code + ": " + e.Message
}

func BadRequest(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusBadRequest}
}

func NotFound(message string) *Error {
	return &Error{Code: "not_found_or_not_allowed", Message: message, HTTPStatus: http.StatusNotFound}
}

func Conflict(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusConflict}
}

func Internal(message string) *Error {
	return &Error{Code: "internal_error", Message: message, HTTPStatus: http.StatusInternalServerError}
}
