package authallow

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidEmailAllowlist = errors.New("invalid auth email allowlist")

type EmailSet map[string]struct{}

func NewEmailSet(emails []string) (EmailSet, error) {
	if len(emails) == 0 {
		return nil, nil
	}
	set := make(EmailSet, len(emails))
	for _, email := range emails {
		normalized := NormalizeEmail(email)
		if !validEmail(normalized) {
			return nil, fmt.Errorf("%w: %q", ErrInvalidEmailAllowlist, email)
		}
		set[normalized] = struct{}{}
	}
	if len(set) == 0 {
		return nil, nil
	}
	return set, nil
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s EmailSet) Allows(email string, verified *bool) bool {
	if len(s) == 0 {
		return true
	}
	if verified == nil || !*verified {
		return false
	}
	_, ok := s[NormalizeEmail(email)]
	return ok
}

// DynamicEmailSource is a runtime-managed allowlist source (the DB-backed
// auth_allowed_emails table) consulted IN UNION with the static env set.
// Implementations must be cheap per call (in-process cached) and fail closed:
// an unreachable source returns false, never an error the middleware would
// have to interpret.
type DynamicEmailSource interface {
	EmailAllowed(ctx context.Context, tenantID, normalizedEmail string) bool
}

// AllowsWithDynamic is the union rule for the static env set plus a dynamic
// source. Enforcement stays keyed on the STATIC set being non-empty — an empty
// env allowlist means "allowlist disabled" (local dev), exactly as
// EmailSet.Allows has always behaved, regardless of what the dynamic source
// holds. When enforced, an email passes if EITHER set contains it; the
// email-verified requirement applies to both.
func AllowsWithDynamic(ctx context.Context, set EmailSet, dynamic DynamicEmailSource, tenantID, email string, verified *bool) bool {
	if len(set) == 0 {
		return true
	}
	if verified == nil || !*verified {
		return false
	}
	normalized := NormalizeEmail(email)
	if _, ok := set[normalized]; ok {
		return true
	}
	return dynamic != nil && dynamic.EmailAllowed(ctx, tenantID, normalized)
}

func validEmail(email string) bool {
	if email == "" || strings.ContainsAny(email, " \t\r\n,") {
		return false
	}
	local, domain, ok := strings.Cut(email, "@")
	return ok && local != "" && domain != "" && !strings.Contains(domain, "@")
}
