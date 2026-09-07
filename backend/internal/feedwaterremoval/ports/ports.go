// Package ports is the seam through which weighing, PC Care and the phone
// bootstrap read the tenant's feed & water removal cutoff. Consumers hold the
// interface, never the table: weighing in particular is ISOLATED from every
// non-weighing table (AGENTS.md), so it receives the cutoff as an opaque value
// and binds it into its own SQL rather than joining feed_water_removal_config.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/feedwaterremoval/domain"
)

// ErrCutoffNotConfigured is returned when the tenant has no
// feed_water_removal_config row. Callers FAIL CLOSED on it — refuse the plan,
// hide nothing silently — rather than substituting a literal hour; the whole
// point of the config is that code carries no evening of its own.
var ErrCutoffNotConfigured = errors.New("feedwaterremoval: cutoff not configured for tenant")

// CutoffReader answers "when does this farm's removal evening open".
type CutoffReader interface {
	FeedWaterRemovalCutoff(ctx context.Context, tenantID string) (domain.Cutoff, error)
}

// StaticCutoff is a CutoffReader over one fixed value — for tests, fixtures
// and fakes, so a service under test still reads the cutoff through the seam.
type StaticCutoff struct {
	Cutoff domain.Cutoff
	// Err, when set, is returned instead of the cutoff (e.g. ErrCutoffNotConfigured).
	Err error
}

func (s StaticCutoff) FeedWaterRemovalCutoff(context.Context, string) (domain.Cutoff, error) {
	if s.Err != nil {
		return domain.Cutoff{}, s.Err
	}
	if !s.Cutoff.Valid() {
		return domain.Cutoff{}, ErrCutoffNotConfigured
	}
	return s.Cutoff, nil
}
