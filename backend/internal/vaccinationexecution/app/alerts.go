package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// ListAlerts passes the vaccination alerts feed through to the repository.
//
// There is deliberately NO extra permission gate here. The route itself is gated
// (permissions.VaccinationAlertsRead), and the query's audience filter is
// member_id equality against the caller -- so the only rows reachable are ones a
// producer explicitly addressed to this person. A second capability check would
// add no safety and would re-create the 2026-08-04 defect where an operator's own
// Alerts tab 403'd because it was gated on the leadership oversight bundle.
func (s *Service) ListAlerts(
	ctx context.Context,
	tenantID, memberOrUserID string,
	tenantWide bool,
	parkIDs []string,
	cursor string,
	limit int,
) (domain.AlertPage, error) {
	if limit <= 0 {
		limit = domain.AlertPageSize
	}
	if limit > domain.MaxAlertPageSize {
		limit = domain.MaxAlertPageSize
	}
	return s.repo.ListAlerts(ctx, tenantID, memberOrUserID, tenantWide, parkIDs, cursor, limit)
}
