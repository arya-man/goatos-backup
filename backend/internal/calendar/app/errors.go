package app

type Error struct {
	Code    string
	Message string
	// cause is the original error that produced this classification (e.g. the
	// underlying SQLSTATE/driver error behind an Internal("...") result). It is
	// intentionally unexported so callers cannot depend on its shape, but it
	// keeps the real error reachable via errors.Unwrap for logging and
	// diagnostics instead of being silently dropped.
	cause error
}

func (e Error) Error() string {
	return e.Message
}

func (e Error) Unwrap() error {
	return e.cause
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

// InternalWrap is Internal, but preserves the original error via Unwrap so
// the real cause (SQLSTATE, driver error, etc.) is not dropped when a
// generic user-facing message is returned. Use this whenever the source
// error is available at the call site — mapRepoError's default branch in
// particular must never discard err.
func InternalWrap(message string, cause error) error {
	return Error{Code: "internal_error", Message: message, cause: cause}
}

func Unavailable(code, message string) error {
	return Error{Code: code, Message: message}
}
