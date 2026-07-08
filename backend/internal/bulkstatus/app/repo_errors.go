package app

import "errors"

// Repository sentinel errors. Adapters return these; the service maps them to
// app.Error envelopes.
var (
	// ErrIdempotencyConflict means the same Idempotency-Key was reused for a
	// different row set (fingerprint mismatch).
	ErrIdempotencyConflict = errors.New("bulk status idempotency key reused with different request")
)

func mapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrIdempotencyConflict) {
		return Conflict("idempotency_conflict", "Idempotency-Key was reused with a different row set")
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal("bulk status repository error")
}
