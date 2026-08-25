// Package app is the Growth Director application service: capability gate,
// park-scope resolution and window resolution, copied from the weighing
// module's leadership reads so this surface is not reachable on a weaker check.
package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

type Service struct {
	repo ports.Repository
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

// GetGrowthDirectorWeights serves the Growth Director widgets on the admin-web
// Weights screen. Same capability and scope rules as the weighing leadership
// reads (GetShedWeights / GetWeightDemographics): WeighingMonitor gate, then
// the caller's own authorized-park scope, never wider.
func (s *Service) GetGrowthDirectorWeights(ctx context.Context, actor domain.Actor, parkID, fromBusinessDate, toBusinessDate, sex string) (domain.GrowthDirectorWeights, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.GrowthDirectorWeights{}, ports.ErrForbidden
	}
	parkID = strings.TrimSpace(parkID)
	if parkID != "" && !uuidutil.IsUUIDString(parkID) {
		return domain.GrowthDirectorWeights{}, ports.ErrInvalidArgument
	}
	periodStart, periodEndExclusive, err := s.resolveWindow(fromBusinessDate, toBusinessDate)
	if err != nil {
		return domain.GrowthDirectorWeights{}, err
	}
	parkIDs, scopeErr := s.resolveMonitorParkScope(ctx, actor, parkID)
	if scopeErr != nil {
		return domain.GrowthDirectorWeights{}, scopeErr
	}
	return s.repo.GetGrowthDirectorWeights(ctx, actor.TenantID, parkIDs, periodStart, periodEndExclusive, sex)
}

// resolveMonitorParkScope turns an OPTIONAL park_id into the concrete park list
// this read may aggregate over, under permissions.WeighingMonitor.
//
// If the caller named a park: WeighingMonitor is a CAPABILITY check, not a
// scope check, so the requested park must be inside the actor's authorized
// parks — otherwise a park-scoped monitor could read another park's herd by
// naming its id. If park_id was omitted, the answer is exactly the caller's own
// authorized-park set, never a wider one. Copied from the weighing module's
// resolveMonitorParkScope so both surfaces resolve scope identically.
func (s *Service) resolveMonitorParkScope(ctx context.Context, actor domain.Actor, parkID string) ([]string, error) {
	if parkID != "" {
		grants := httpmiddleware.AuthGrantsFromContext(ctx)
		if !hasTenantWideCapability(grants, actor.TenantID, permissions.WeighingMonitor) {
			authorizedParkIDs := httpmiddleware.AuthorizedParkIDsForCapability(grants, permissions.WeighingMonitor)
			found := false
			for _, id := range authorizedParkIDs {
				if id == parkID {
					found = true
					break
				}
			}
			if !found {
				return nil, ports.ErrNotFound
			}
		}
		return []string{parkID}, nil
	}

	authorizedParks, tenantWide := authorizedParkSet(ctx, actor.TenantID, permissions.WeighingMonitor)
	var parkIDs []string
	if tenantWide {
		allParks, err := s.repo.ListParks(ctx, actor.TenantID)
		if err != nil {
			return nil, err
		}
		for _, p := range allParks {
			parkIDs = append(parkIDs, p.ParkID)
		}
	} else {
		for id := range authorizedParks {
			parkIDs = append(parkIDs, id)
		}
	}
	if len(parkIDs) == 0 {
		// Passed the flat role gate but owns no park here: answering
		// unrestricted would be the escalation this path exists to prevent.
		return nil, ports.ErrNotFound
	}
	return parkIDs, nil
}

// resolveWindow turns optional inclusive business dates into the half-open
// [start, end) window, business-day grain, Asia/Kolkata. The caller's LAST day
// is inclusive, so the exclusive boundary is midnight the day AFTER it.
func (s *Service) resolveWindow(fromBusinessDate, toBusinessDate string) (time.Time, time.Time, error) {
	from := strings.TrimSpace(fromBusinessDate)
	to := strings.TrimSpace(toBusinessDate)
	if from != "" && !isBusinessDate(from) {
		return time.Time{}, time.Time{}, ports.ErrInvalidArgument
	}
	if to != "" && !isBusinessDate(to) {
		return time.Time{}, time.Time{}, ports.ErrInvalidArgument
	}
	loc := biztime.DefaultLocation()
	now := time.Now().In(loc)
	var periodStart, periodEndInclusive time.Time
	var err error
	if to == "" {
		periodEndInclusive = biztime.BusinessDayStart(now)
	} else {
		periodEndInclusive, err = time.ParseInLocation("2006-01-02", to, loc)
		if err != nil {
			return time.Time{}, time.Time{}, ports.ErrInvalidArgument
		}
	}
	if from == "" {
		periodStart = periodEndInclusive.AddDate(0, 0, -(domain.DefaultPeriodDays - 1))
	} else {
		periodStart, err = time.ParseInLocation("2006-01-02", from, loc)
		if err != nil {
			return time.Time{}, time.Time{}, ports.ErrInvalidArgument
		}
	}
	if periodEndInclusive.Before(periodStart) {
		return time.Time{}, time.Time{}, ports.ErrInvalidArgument
	}
	return periodStart, periodEndInclusive.AddDate(0, 0, 1), nil
}

// isBusinessDate accepts only a business DATE (YYYY-MM-DD); anything finer is
// rejected because this report has business-day grain and nothing finer.
func isBusinessDate(value string) bool {
	if len(value) != 10 {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

// authorizedParkSet returns the parks in which the actor holds any of
// `capabilities`, and whether the actor is tenant-wide for one of them (in
// which case the set is meaningless and no filtering applies). No grants at
// all = internal/service context, unrestricted.
func authorizedParkSet(ctx context.Context, tenantID string, capabilities ...string) (parks map[string]struct{}, tenantWide bool) {
	grants := httpmiddleware.AuthGrantsFromContext(ctx)
	if len(grants) == 0 {
		return nil, true
	}
	parks = map[string]struct{}{}
	for _, capability := range capabilities {
		if hasTenantWideCapability(grants, tenantID, capability) {
			return nil, true
		}
		for _, parkID := range httpmiddleware.AuthorizedParkIDsForCapability(grants, capability) {
			parks[parkID] = struct{}{}
		}
	}
	return parks, false
}

// hasTenantWideCapability reports whether any grant is scoped to the whole
// tenant AND carries a role that has the given capability.
func hasTenantWideCapability(grants []permissions.ActiveGrant, tenantID, capability string) bool {
	for _, grant := range grants {
		if grant.ScopeType == "tenant" && grant.ScopeID == tenantID && permissions.RoleHasPermission(grant.Role, capability) {
			return true
		}
	}
	return false
}
