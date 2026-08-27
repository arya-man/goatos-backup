package ports

import (
	"context"
	"errors"
)

// ErrIdentityUnavailable signals that the identity provider (Firebase Identity
// Toolkit) could not be reached or is not configured in this environment. The
// create-person flow fails closed on it: no DB rows are written for a person
// whose login account could not be ensured.
var ErrIdentityUnavailable = errors.New("identity provider unavailable")

// EnsuredUser is the outcome of EnsureEmailUser.
type EnsuredUser struct {
	// UID is the identity provider's user id (Firebase Auth UID). The backend's
	// internal user_id is derived from it via platformauth.StableSubjectID.
	UID string
	// Existed is true when the email already had an account. An existing
	// account's chosen password is never reset by this flow; whether the
	// supplied convention password now works is reported by PasswordSet.
	Existed bool
	// PasswordSet is true when the supplied password was installed on an
	// EXISTING account that had no password provider at all (an account born
	// from a Google sign-in before onboarding ran). It is false for a fresh
	// account (the password is set by construction) and for an existing
	// account that already had its own password (which is left untouched).
	PasswordSet bool
}

// IdentityProvider ensures a login account exists for an email address.
//
// Implementations must be idempotent by construction: lookup-by-email first,
// create only when absent, and always return the (stable) provider UID. The
// account must end email-verified — the auth middleware's allowlist rejects
// unverified emails, so an unverified account can never log in.
type IdentityProvider interface {
	EnsureEmailUser(ctx context.Context, email, displayName, password string) (EnsuredUser, error)
}
