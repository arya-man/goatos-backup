package cubeclient

import (
	"errors"
	"fmt"
)

// Sentinel errors let callers branch on failure class (route to fallback,
// surface a friendly degrade, etc.) without string matching.
var (
	// ErrMissingTenant is returned before any network call when the caller did
	// not supply a tenant id. Tenant scope must come from the server session.
	ErrMissingTenant = errors.New("cubeclient: missing tenant id")
	// ErrNotConfigured is returned when the client has no base URL / secret.
	ErrNotConfigured = errors.New("cubeclient: not configured")
	// ErrTimeout indicates the request exceeded the configured deadline / retries.
	ErrTimeout = errors.New("cubeclient: timeout")
	// ErrUnauthorized indicates Cube rejected the signed token / tenant.
	ErrUnauthorized = errors.New("cubeclient: unauthorized")
)

// APIError is a structured Cube-side error (HTTP >= 400 or an error body).
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("cubeclient: cube error (status %d): %s", e.StatusCode, e.Body)
}
