package app

type Error struct {
	Code    string
	Message string
}

func (e Error) Error() string {
	return e.Message
}

func BadRequest(code, message string) error {
	return Error{Code: code, Message: message}
}

func NotFound(message string) error {
	return Error{Code: "not_found", Message: message}
}

func Forbidden(code, message string) error {
	return Error{Code: code, Message: message}
}

func Conflict(code, message string) error {
	return Error{Code: code, Message: message}
}

func Internal(message string) error {
	return Error{Code: "internal_error", Message: message}
}
