package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// MortalityRepository serves the Counts -> Mortality read: deaths in a window sliced by
// every attribute the animal's own row froze at death, beside each section's live head count
// for the rate series.
//
// OPTIONAL capability resolved by type assertion, exactly like HerdAnalyticsRepository, so
// adding it cannot break the fakes implementing Repository across the counts suite. A repo
// that does not implement it fails closed with app.ErrMortalityUnavailable rather than
// serving a page that reads as a farm where nothing dies.
type MortalityRepository interface {
	GetMortality(ctx context.Context, req domain.MortalityQuery) (domain.Mortality, error)
}

// DeathCauseLabeler turns a stored cause-of-death key into the farm's word for the
// disease. The vocabulary belongs to Health (the diagnosis register); Counts holds this
// narrow port so the mortality board can name a cause without importing Health's types --
// the same shape the death form's DeathCauseValidator already takes.
type DeathCauseLabeler interface {
	LabelDeathCause(ctx context.Context, tenantID, key string) string
}
