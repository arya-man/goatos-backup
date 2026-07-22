package app

import "errors"

// Sentinel errors the HTTP adapter maps to status codes. Business refusals are
// returned as ModeRefused answers (200), NOT errors — only true failures error.
var (
	// ErrForbidden => the actor is not a leadership/ceo_internal principal, or
	// has no tenant scope. Maps to 403.
	ErrForbidden = errors.New("ceoai: leadership access required")
	// ErrEmptyQuestion => blank question. Maps to 400.
	ErrEmptyQuestion = errors.New("ceoai: question required")
)
