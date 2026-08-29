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
	// Existed is true when the email already had an account. The caller must NOT
	// treat the supplied password as that account's password in this case — an
	// existing account's password is never reset by this flow.
	Existed bool
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
