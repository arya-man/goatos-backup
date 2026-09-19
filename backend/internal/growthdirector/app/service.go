// Package app is the Growth Director application service: capability gate,
// park-scope resolution and window resolution, copied from the weighing
// module's leadership reads so this surface is not reachable on a weaker check.
package app

import (
	"context"
	"fmt"
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
func (s *Service) GetGrowthDirectorWeights(ctx context.Context, actor domain.Actor, parkID, fromBusinessDate, toBusinessDate, sex, origin, weighingCategory, sections string) (domain.GrowthDirectorWeights, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.GrowthDirectorWeights{}, ports.ErrForbidden
	}
	parkID = strings.TrimSpace(parkID)
	if parkID != "" && !uuidutil.IsUUIDString(parkID) {
		return domain.GrowthDirectorWeights{}, ports.ErrInvalidArgument
	}
	weighingCategory = strings.TrimSpace(weighingCategory)
	if weighingCategory == "all" {
		weighingCategory = ""
	}
	if weighingCategory != "" && weighingCategory != "individual_animal" && weighingCategory != "per_shed_partition" {
		return domain.GrowthDirectorWeights{}, ports.ErrInvalidArgument
	}
	if err := validateGrowthDirectorSections(sections); err != nil {
		return domain.GrowthDirectorWeights{}, err
	}
	settings, err := s.repo.GrowthSettings(ctx, actor.TenantID)
	if err != nil {
		return domain.GrowthDirectorWeights{}, err
	}
	periodStart, periodEndExclusive, err := s.resolveWindow(fromBusinessDate, toBusinessDate, settings.DefaultPeriodDays)
	if err != nil {
		return domain.GrowthDirectorWeights{}, err
	}
	parkIDs, scopeErr := s.resolveMonitorParkScope(ctx, actor, parkID)
	if scopeErr != nil {
		return domain.GrowthDirectorWeights{}, scopeErr
	}
	return s.repo.GetGrowthDirectorWeights(ctx, actor.TenantID, parkIDs, periodStart, periodEndExclusive, sex, origin, weighingCategory, sections)
}

func validateGrowthDirectorSections(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	allowed := map[string]bool{
		"road_to_sale":   true,
		"fair_fight":     true,
		"slow_growth":    true,
		"feed_vs_growth": true,
		"feed_problems":  true,
		"trust":          true,
	}
	for _, part := range strings.Split(raw, ",") {
		if !allowed[strings.TrimSpace(part)] {
			return ports.ErrInvalidArgument
		}
	}
	return nil
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

// GetFCR serves the Weighing FCR tab. Same gate, scope and window rules as
// GetGrowthDirectorWeights: this is a reporting read under the Weights screen.
func (s *Service) GetFCR(ctx context.Context, actor domain.Actor, parkID, fromBusinessDate, toBusinessDate, sex, origin, weighingCategory string) (domain.FCRReport, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.FCRReport{}, ports.ErrForbidden
	}
	parkID = strings.TrimSpace(parkID)
	if parkID != "" && !uuidutil.IsUUIDString(parkID) {
		return domain.FCRReport{}, ports.ErrInvalidArgument
	}
	weighingCategory = strings.TrimSpace(weighingCategory)
	if weighingCategory == "all" {
		weighingCategory = ""
	}
	if weighingCategory != "" && weighingCategory != "individual_animal" && weighingCategory != "per_shed_partition" {
		return domain.FCRReport{}, ports.ErrInvalidArgument
	}
	// The cohort filters are validated here, not silently widened: an unknown value is a bad
	// REQUEST, and passing it through would show a reader every pen under a heading that says
	// otherwise.
	sex = strings.ToLower(strings.TrimSpace(sex))
	if sex != "" && sex != "male" && sex != "female" {
		return domain.FCRReport{}, ports.ErrInvalidArgument
	}
	origin = strings.ToLower(strings.TrimSpace(origin))
	if origin != "" && origin != domain.OriginFarmBorn && origin != domain.OriginPurchased {
		return domain.FCRReport{}, ports.ErrInvalidArgument
	}
	settings, err := s.repo.GrowthSettings(ctx, actor.TenantID)
	if err != nil {
		return domain.FCRReport{}, err
	}
	periodStart, periodEndExclusive, err := s.resolveWindow(fromBusinessDate, toBusinessDate, settings.DefaultPeriodDays)
	if err != nil {
		return domain.FCRReport{}, err
	}
	parkIDs, scopeErr := s.resolveMonitorParkScope(ctx, actor, parkID)
	if scopeErr != nil {
		return domain.FCRReport{}, scopeErr
	}
	return s.repo.GetFCR(ctx, actor.TenantID, parkIDs, periodStart, periodEndExclusive, sex, origin, weighingCategory)
}

// GetSalePrices serves the assumed live-weight sale prices the Weighing tabs value gain and stock
// at. Gated like every other Weights-screen read; the prices are the ones effective today.
func (s *Service) GetSalePrices(ctx context.Context, actor domain.Actor) (domain.SalePrices, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.SalePrices{}, ports.ErrForbidden
	}
	return s.repo.GetSalePrices(ctx, actor.TenantID, biztime.BusinessDayStart(time.Now().In(biztime.DefaultLocation())))
}

// GetAssumptions serves the Assumptions drawer's read: the prices effective today and every keyed
// figure. Gated like every other Weights-screen read -- a reader is owed the figures the page
// is valued at, whether or not they may change them.
func (s *Service) GetAssumptions(ctx context.Context, actor domain.Actor) (domain.Assumptions, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.Assumptions{}, ports.ErrForbidden
	}
	return s.repo.GetAssumptions(ctx, actor.TenantID, biztime.BusinessDayStart(time.Now().In(biztime.DefaultLocation())))
}

// PutAssumptions lands an edit from the drawer. WHO may call this is decided at the route
// (weighing.assumptions.write, person-aware through /people ticks) and NOT re-derived from roles
// here: a role check would refuse the very person a Configure tick was meant to admit. This
// layer owns the business bands (ValidateAssumptionsUpdate) and the effective date, which is the
// server's business day, never the client's.
func (s *Service) PutAssumptions(ctx context.Context, actor domain.Actor, update domain.AssumptionsUpdate) (domain.Assumptions, error) {
	if err := domain.ValidateAssumptionsUpdate(update); err != nil {
		return domain.Assumptions{}, fmt.Errorf("%w: %s", ports.ErrInvalidArgument, err.Error())
	}
	current, err := s.repo.GrowthSettings(ctx, actor.TenantID)
	if err != nil {
		return domain.Assumptions{}, err
	}
	lower, threshold := current.SaleReadyLowerKg, current.SaleReadyThresholdKg
	for _, v := range update.Values {
		key, _ := domain.LookupAssumptionKey(v.Key)
		switch key.Key {
		case domain.AssumptionSaleReadyLowerKg:
			lower = v.Value
		case domain.AssumptionSaleReadyThresholdKg:
			threshold = v.Value
		}
	}
	if lower >= threshold {
		return domain.Assumptions{}, fmt.Errorf("%w: sale_ready_lower_kg must be below sale_ready_threshold_kg", ports.ErrInvalidArgument)
	}
	return s.repo.PutAssumptions(ctx, actor.TenantID, actor.UserID, biztime.BusinessDayStart(time.Now().In(biztime.DefaultLocation())), update)
}

// resolveWindow turns optional inclusive business dates into the half-open
// [start, end) window, business-day grain, Asia/Kolkata. The caller's LAST day
// is inclusive, so the exclusive boundary is midnight the day AFTER it.
func (s *Service) resolveWindow(fromBusinessDate, toBusinessDate string, defaultPeriodDays int) (time.Time, time.Time, error) {
	if defaultPeriodDays < 1 {
		defaultPeriodDays = domain.DefaultPeriodDays
	}
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
		periodStart = periodEndInclusive.AddDate(0, 0, -(defaultPeriodDays - 1))
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
