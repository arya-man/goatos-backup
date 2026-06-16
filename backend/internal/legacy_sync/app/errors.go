package app

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
	return &Error{Code: code, Message: message, HTTPStatus: 400}
}

func Unauthorized(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: 401}
}

func NotFound(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: 404}
}

func Conflict(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: 409}
}

func Internal(message string) *Error {
	return &Error{Code: "internal_error", Message: message, HTTPStatus: 500, Retryable: true}
}
