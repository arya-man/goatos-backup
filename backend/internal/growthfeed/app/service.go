package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthfeed/domain"
	"github.com/vgoats/goatos/backend/internal/growthfeed/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// DefaultPeriodDays matches the Weights screen's own default window, so the two
// surfaces describe the same span unless the caller says otherwise.
const DefaultPeriodDays = 28

// Actor is the authenticated caller, kept minimal on purpose: this module makes no
// decision that depends on anything else about them.
type Actor struct {
	TenantID string
	Roles    []string
}

// readCapabilities are the capabilities this read is offered to. BOTH directors
// who need it hold one: the Growth Director owns weighing and therefore
// weighing.monitor, and the Feed Director owns the ration and therefore
// feed config read. Requiring both would lock each of them out of half of their
// own question.
var readCapabilities = []string{permissions.WeighingMonitor, permissions.FeedConfigRead}

type Service struct {
	repo ports.Repository
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

// GetPenGrowthFeed answers "which comparable pens are growing at different rates,
// and what is each of them being fed".
//
// It reads growth and ration SEPARATELY and joins them here rather than in one
// query. That is a deliberate choice, not a convenience: the growth half is then
// byte-for-byte the definition weighing's own screens use, and the ration half
// stays a pure feed-config read. Neither module gains a dependency on the other,
// and the correlation — the part that is genuinely new and genuinely arguable —
// sits in one testable place.
func (s *Service) GetPenGrowthFeed(ctx context.Context, actor Actor, parkID, fromBusinessDate, toBusinessDate string) (domain.PenGrowthFeed, error) {
	// RolesAuthorizeAny, never RolesAuthorize: the latter ANDs its list, which would
	// demand both capabilities and lock out BOTH directors this screen is for — the
	// Growth Director holds weighing.monitor without feed config read, and the Feed
	// Director the reverse.
	if !permissions.RolesAuthorizeAny(actor.Roles, readCapabilities) {
		return domain.PenGrowthFeed{}, ports.ErrForbidden
	}

	periodStart, periodEndExclusive, err := resolveWindow(fromBusinessDate, toBusinessDate)
	if err != nil {
		return domain.PenGrowthFeed{}, err
	}
	parkIDs, err := s.resolveParkScope(ctx, actor, strings.TrimSpace(parkID))
	if err != nil {
		return domain.PenGrowthFeed{}, err
	}

	loc := biztime.DefaultLocation()
	// periodEnd arrives half-open; the label and the ration as-of date both use the
	// INCLUSIVE last day, because the ration whose effect these gains reflect is the
	// one in force at the end of the window, not the day after it.
	lastDay := periodEndExclusive.In(loc).AddDate(0, 0, -1)
	out := domain.PenGrowthFeed{
		PeriodStart: periodStart.In(loc).Format("2006-01-02"),
		PeriodEnd:   lastDay.Format("2006-01-02"),
		Rows:        []domain.Pen{},
	}

	growth, err := s.repo.ListPenGrowth(ctx, actor.TenantID, parkIDs, periodStart, periodEndExclusive)
	if err != nil {
		return domain.PenGrowthFeed{}, err
	}
	if len(growth) == 0 {
		return out, nil
	}

	locationIDs := make([]string, 0, len(growth))
	seen := make(map[string]struct{}, len(growth))
	for _, pen := range growth {
		// A physical shed with several partitions produces several pen rows sharing
		// one location_id only when partition_label differs; deduplicating keeps the
		// cohort lookup one row per location and avoids asking the same question twice.
		if _, dup := seen[pen.LocationID]; dup {
			continue
		}
		seen[pen.LocationID] = struct{}{}
		locationIDs = append(locationIDs, pen.LocationID)
	}

	cohorts, err := s.repo.ListPenCohortRation(ctx, actor.TenantID, parkIDs, locationIDs, lastDay.Format("2006-01-02"))
	if err != nil {
		return domain.PenGrowthFeed{}, err
	}
	byLocation := make(map[string]ports.PenCohortRation, len(cohorts))
	for _, row := range cohorts {
		byLocation[row.LocationID] = row
	}

	rows := make([]domain.Pen, 0, len(growth))
	for _, g := range growth {
		pen := domain.Pen{
			ParkID: g.ParkID, ParkName: g.ParkName,
			LocationID: g.LocationID, ShedName: g.ShedName, PartitionLabel: g.PartitionLabel,
			// Composed by the backend through the canonical helper. A renderer that
			// joins shed name and partition itself produces "Godel 1 1" (OL-3).
			OperationalLocationDisplay: (oploc.OperationalLocation{
				ShedID:         g.LocationID,
				ShedName:       g.ShedName,
				PartitionLabel: derefLabel(g.PartitionLabel),
			}).Display(),
			WeighingCategory: g.WeighingCategory,
			AnimalsWeighed:   g.AnimalsWeighed,
			AverageWeightKg:  g.AverageWeightKg,
			ADGGPerDay:       g.ADGGPerDay,
			ADGBasis:         g.ADGBasis,
			ADGSampleCount:   g.ADGSampleCount,
			ADGSpanDays:      g.ADGSpanDays,
		}

		cohort, found := byLocation[g.LocationID]
		if !found {
			// A pen that was weighed but resolves to no live animal in the herd
			// register. It stays in the table — it is a real pen with real weights —
			// and is reported as having no cohort rather than dropped.
			pen.FeedPlanStatus = domain.FeedPlanUnknownCohort
			rows = append(rows, pen)
			continue
		}

		pen.Breed = nilIfEmpty(cohort.Breed)
		pen.Sex = nilIfEmpty(cohort.Sex)
		pen.Stage = nilIfEmpty(cohort.Stage)
		pen.LiveAnimals = cohort.LiveAnimals
		pen.RationGroupLabel = nilIfEmpty(cohort.RationGroupLabel)
		pen.ShedTagLabel = nilIfEmpty(cohort.ShedTagLabel)
		pen.FeedItemsConfigured = cohort.Plan.ItemsConfigured
		pen.FeedItemsBlocked = cohort.Plan.ItemsBlocked

		status, grams, kcal := domain.ResolveFeedPlan(cohort.Plan)
		pen.FeedPlanStatus = status
		pen.PlannedFeedGPerHeadDay = grams
		pen.PlannedEnergyKcalPerHeadDay = kcal

		rows = append(rows, pen)
	}

	out.PensWithoutCohort, out.PensWithoutRation, out.PensWithoutGain, out.ComparablePens = domain.Benchmark(rows)
	out.Rows = rows
	return out, nil
}

// resolveParkScope mirrors weighing's own monitor scope resolution: an explicit
// park must be inside the caller's grants, and an omitted park means every park
// they may see — never every park in the tenant.
func (s *Service) resolveParkScope(ctx context.Context, actor Actor, parkID string) ([]string, error) {
	grants := httpmiddleware.AuthGrantsFromContext(ctx)

	if parkID != "" {
		if !uuidutil.IsUUIDString(parkID) {
			return nil, ports.ErrInvalidArgument
		}
		if tenantWide(grants, actor.TenantID) {
			return []string{parkID}, nil
		}
		for _, capability := range readCapabilities {
			for _, id := range httpmiddleware.AuthorizedParkIDsForCapability(grants, capability) {
				if id == parkID {
					return []string{parkID}, nil
				}
			}
		}
		// Not found rather than forbidden: telling the caller the park exists but is
		// off-limits confirms the existence of a park they may not see.
		return nil, ports.ErrNotFound
	}

	if tenantWide(grants, actor.TenantID) {
		parkIDs, err := s.repo.ListParkIDs(ctx, actor.TenantID)
		if err != nil {
			return nil, err
		}
		if len(parkIDs) == 0 {
			return nil, ports.ErrNotFound
		}
		return parkIDs, nil
	}

	unique := map[string]struct{}{}
	parkIDs := []string{}
	for _, capability := range readCapabilities {
		for _, id := range httpmiddleware.AuthorizedParkIDsForCapability(grants, capability) {
			if _, dup := unique[id]; dup {
				continue
			}
			unique[id] = struct{}{}
			parkIDs = append(parkIDs, id)
		}
	}
	if len(parkIDs) == 0 {
		// Passed the flat role gate but owns no park here. An unrestricted read would
		// be exactly the escalation the park scoping exists to prevent.
		return nil, ports.ErrNotFound
	}
	return parkIDs, nil
}

// tenantWide mirrors weighing's own hasTenantWideCapability: the grant's ROLE must
// carry the capability AND its scope must be the tenant. Checking the two
// separately would let an actor combine an unrelated tenant-scoped grant with a
// capability held only in one park.
func tenantWide(grants []permissions.ActiveGrant, tenantID string) bool {
	for _, grant := range grants {
		if grant.ScopeType != "tenant" || grant.ScopeID != tenantID {
			continue
		}
		for _, capability := range readCapabilities {
			if permissions.RoleHasPermission(grant.Role, capability) {
				return true
			}
		}
	}
	return false
}

// resolveWindow turns two optional INCLUSIVE Asia/Kolkata business dates into the
// half-open instant window the repository expects. UTC never defines a Goat OS
// business day, and nothing finer than a day is accepted: a weigh belongs to the
// day it happened on.
func resolveWindow(fromBusinessDate, toBusinessDate string) (time.Time, time.Time, error) {
	from := strings.TrimSpace(fromBusinessDate)
	to := strings.TrimSpace(toBusinessDate)
	loc := biztime.DefaultLocation()

	var endInclusive time.Time
	var err error
	if to == "" {
		endInclusive = biztime.BusinessDayStart(time.Now().In(loc))
	} else if endInclusive, err = time.ParseInLocation("2006-01-02", to, loc); err != nil {
		return time.Time{}, time.Time{}, ports.ErrInvalidArgument
	}

	var start time.Time
	if from == "" {
		start = endInclusive.AddDate(0, 0, -(DefaultPeriodDays - 1))
	} else if start, err = time.ParseInLocation("2006-01-02", from, loc); err != nil {
		return time.Time{}, time.Time{}, ports.ErrInvalidArgument
	}

	if endInclusive.Before(start) {
		return time.Time{}, time.Time{}, ports.ErrInvalidArgument
	}
	return start, endInclusive.AddDate(0, 0, 1), nil
}

// derefLabel is nil-safe on purpose: oploc treats an empty label as "no partition"
// and renders the bare shed name, which is exactly right for an undivided shed.
func derefLabel(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nilIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
