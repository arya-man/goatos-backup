package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// HerdAnalyticsRepository serves the Counts -> Herd Analytics read: the live
// census composition beside month-by-month herd movement.
//
// It is a SEPARATE, OPTIONAL capability rather than another method on
// Repository, for the same reason AlertsRepository is: the service resolves it
// by type assertion, so adding this read cannot break the fakes that implement
// Repository across the counts test suite. A repo that does not implement it
// fails closed with app.ErrHerdAnalyticsUnavailable instead of serving an empty
// page that would read as a farm with no herd.
type HerdAnalyticsRepository interface {
	GetHerdAnalytics(ctx context.Context, req domain.HerdAnalyticsQuery) (domain.HerdAnalytics, error)
}
