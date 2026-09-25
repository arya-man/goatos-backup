package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

type prefixRepo struct {
	ports.Repository
	code string
}

func (r prefixRepo) BirthProvisionalPrefix(context.Context, string, string) (string, error) {
	return r.code, nil
}

// Any active park records a birth; the code only has to be tag-safe.
func TestBirthProvisionalPrefixAcceptsEveryParkNotOnlyCBEOrCPT(t *testing.T) {
	for _, code := range []string{"CBE", "CPT", "hsr", "PARK3"} {
		s := &Service{repo: prefixRepo{code: code}}
		got, err := s.BirthProvisionalPrefix(context.Background(), "t", "p")
		if err != nil {
			t.Fatalf("park %q must record a birth: %v", code, err)
		}
		if got == "" || got != strings.ToUpper(code) {
			t.Fatalf("prefix for %q = %q", code, got)
		}
	}
	s := &Service{repo: prefixRepo{code: "Hosur farm"}}
	var appErr *Error
	if _, err := s.BirthProvisionalPrefix(context.Background(), "t", "p"); !errors.As(err, &appErr) || appErr.Code != "unsupported_birth_park" {
		t.Fatalf("a code that cannot start a tag must be refused, got %v", err)
	}
}
