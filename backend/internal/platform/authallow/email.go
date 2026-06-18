package authallow

import (
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

func validEmail(email string) bool {
	if email == "" || strings.ContainsAny(email, " \t\r\n,") {
		return false
	}
	local, domain, ok := strings.Cut(email, "@")
	return ok && local != "" && domain != "" && !strings.Contains(domain, "@")
}
