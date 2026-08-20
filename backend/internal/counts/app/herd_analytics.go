package app

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// ErrHerdAnalyticsUnavailable is returned when GetHerdAnalytics is called on a
// HerdRegisterService whose wired repo does not implement
// ports.HerdAnalyticsRepository. It fails closed on purpose: an empty payload
// would render as a farm holding no animals, which is a different claim from
// "this read is not wired here".
var ErrHerdAnalyticsUnavailable = errors.New("counts: herd analytics repository is not configured")

// GetHerdAnalytics returns the Counts -> Herd Analytics payload: the live census
// rolled up by breed, tag, sex, age band and park, beside one row per India
// calendar month of births, exits and applied pen movements.
//
// The window length is clamped in the adapter against the same bounds the
// handler validates, so a caller cannot ask for an unbounded history.
func (s *HerdRegisterService) GetHerdAnalytics(ctx context.Context, req domain.HerdAnalyticsQuery) (domain.HerdAnalytics, error) {
	if s.herdAnalytics == nil {
		return domain.HerdAnalytics{}, ErrHerdAnalyticsUnavailable
	}
	return s.herdAnalytics.GetHerdAnalytics(ctx, req)
}
