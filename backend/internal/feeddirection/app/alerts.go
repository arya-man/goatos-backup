package app

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// ErrAlertsUnavailable is returned when ListAlerts is called on a Service that was never wired
// with an AlertsRepository (mirrors ErrCompletionUnavailable's fail-closed shape above).
var ErrAlertsUnavailable = errors.New("feeddirection: alerts repository is not configured")

// ListAlerts passes the feed alerts feed through to the repository.
//
// There is deliberately NO extra permission gate here, mirroring vaccinationexecution's
// ListAlerts. The route itself is gated (permissions.FeedDirectionRead / VerificationReview -- see
// routes.go), and the query's audience filter is member_id equality against the caller, so the
// only rows reachable are ones a producer explicitly addressed to this person. A second capability
// check would add no safety and risks re-creating the 2026-08-04 vaccination defect where an
// operator's own Alerts tab 403'd because it was gated on a leadership oversight bundle.
func (s *Service) ListAlerts(
	ctx context.Context,
	tenantID, memberOrUserID string,
	tenantWide bool,
	parkIDs []string,
	cursor string,
	limit int,
) (domain.AlertPage, error) {
	if s.alerts == nil {
		return domain.AlertPage{}, ErrAlertsUnavailable
	}
	if limit <= 0 {
		limit = domain.AlertPageSize
	}
	if limit > domain.MaxAlertPageSize {
		limit = domain.MaxAlertPageSize
	}
	return s.alerts.ListAlerts(ctx, tenantID, memberOrUserID, tenantWide, parkIDs, cursor, limit)
}

// WithAlertsRepository wires the feed alerts feed reader. Without it, ListAlerts fails closed with
// ErrAlertsUnavailable -- mirrors WithCompletionStore/WithDistributionStore/WithPackingStore above.
func (s *Service) WithAlertsRepository(repo ports.AlertsRepository) *Service {
	s.alerts = repo
	return s
}
