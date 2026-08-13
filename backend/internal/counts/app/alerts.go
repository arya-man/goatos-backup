package app

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// ErrAlertsUnavailable is returned when ListAlerts is called on a HerdRegisterService whose
// wired repo does not implement ports.AlertsRepository.
var ErrAlertsUnavailable = errors.New("counts: alerts repository is not configured")

// ListAlerts passes the counts alerts feed through to the repository.
//
// There is deliberately NO extra permission gate here, mirroring vaccinationexecution's and
// feeddirection's ListAlerts. The route itself is gated (permissions.CountsAlertsRead /
// CountsWrite / VerificationReview -- see routes.go), and the query's audience filter is member_id
// equality against the caller, so the only rows reachable are ones a producer explicitly addressed
// to this person. A second capability check would add no safety here, and COUNTS IS AN OFF FEATURE
// (AGENTS.md) precisely because counts.read/counts.write are withheld from health_director -- this
// feed must never be gated on either of those, or the inbox re-enables the feature it must stay
// independent of.
func (s *HerdRegisterService) ListAlerts(
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
