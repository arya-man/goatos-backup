package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
)

// PC CARE SOP seam (maintainer decision 2026-09-22, docs/decisions/pc-care-sop.md).
//
// The module holds this interface; the Postgres adapter that names the SOP tables lives
// OUTSIDE backend/internal/pccare (backend/internal/pccaresop) -- the weighingsop / feedsop
// shape -- and hands PC Care a compiled, versioned domain.Rules value.

// ErrSOPVersionUnknown: a task is pinned to a version the farm never published.
var ErrSOPVersionUnknown = domain.ErrSOPVersionUnknown

// SOPRulesSource answers "which rules does PC Care run under".
type SOPRulesSource interface {
	// PublishedRules is what a task planned NOW is stamped with and runs under.
	PublishedRules(ctx context.Context, tenantID string) (domain.Rules, error)
	// RulesVersion is the exact rule set a task was planned on. Version 0 is the seeded
	// document; an unpublished version is ErrSOPVersionUnknown.
	RulesVersion(ctx context.Context, tenantID string, version int) (domain.Rules, error)
}

// StaticRules is a SOPRulesSource over one fixed rule set -- for tests and fakes, so a service
// under test still reads its rules through the seam. Err, when set, is returned instead.
type StaticRules struct {
	Rules domain.Rules
	Err   error
}

func (s StaticRules) PublishedRules(context.Context, string) (domain.Rules, error) {
	if s.Err != nil {
		return domain.Rules{}, s.Err
	}
	return s.Rules, nil
}

func (s StaticRules) RulesVersion(_ context.Context, _ string, version int) (domain.Rules, error) {
	if s.Err != nil {
		return domain.Rules{}, s.Err
	}
	if version == 0 {
		return domain.SeededRules(), nil
	}
	if version != s.Rules.Version {
		return domain.Rules{}, ErrSOPVersionUnknown
	}
	return s.Rules, nil
}

var _ SOPRulesSource = StaticRules{}

// IsSOPVersionUnknown reports the pinned-but-unpublished case.
func IsSOPVersionUnknown(err error) bool { return errors.Is(err, ErrSOPVersionUnknown) }
