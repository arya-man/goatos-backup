package app

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// GetFeedWeightBand serves the "Feed by weight band" table on the ADG Analytics
// Weight-wise tab. Same capability and scope rules as GetGrowthDirectorWeights:
// WeighingMonitor gate, then the caller's own authorized-park scope.
//
// `from`/`to` are inclusive Asia/Kolkata business dates bounding the weight
// evidence only (the feed side is always the latest sheet), defaulting like the
// other Growth Director read. `sex`, `origin` and `weighing_category` are the
// Weights page's own filters, resolved by the weighing module's resolvers so
// this table and the General tab narrow the identical animals. `includeExited`
// counts sold / dead animals in the band rows; by default they are noted beside
// the row and listed below it instead.
func (s *Service) GetFeedWeightBand(ctx context.Context, actor domain.Actor, parkID, fromBusinessDate, toBusinessDate, sex, origin, weighingCategory string, includeExited bool) (domain.FeedWeightBand, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.FeedWeightBand{}, ports.ErrForbidden
	}
	parkID = strings.TrimSpace(parkID)
	if parkID != "" && !uuidutil.IsUUIDString(parkID) {
		return domain.FeedWeightBand{}, ports.ErrInvalidArgument
	}
	sex = strings.ToLower(strings.TrimSpace(sex))
	switch sex {
	case "", "all", "male", "female":
	default:
		return domain.FeedWeightBand{}, ports.ErrInvalidArgument
	}
	if sex == "all" {
		sex = ""
	}
	origin = strings.ToLower(strings.TrimSpace(origin))
	switch origin {
	case "", "all", "farm_born", "purchased":
	default:
		return domain.FeedWeightBand{}, ports.ErrInvalidArgument
	}
	if origin == "all" {
		origin = ""
	}
	weighingCategory = strings.TrimSpace(weighingCategory)
	if weighingCategory == "all" {
		weighingCategory = ""
	}
	if weighingCategory != "" && weighingCategory != "individual_animal" && weighingCategory != "per_shed_partition" {
		return domain.FeedWeightBand{}, ports.ErrInvalidArgument
	}
	periodStart, periodEndExclusive, err := s.resolveWindow(fromBusinessDate, toBusinessDate)
	if err != nil {
		return domain.FeedWeightBand{}, err
	}
	parkIDs, scopeErr := s.resolveMonitorParkScope(ctx, actor, parkID)
	if scopeErr != nil {
		return domain.FeedWeightBand{}, scopeErr
	}
	source, err := s.repo.GetFeedWeightBandSource(ctx, actor.TenantID, parkIDs, periodStart, periodEndExclusive, sex, origin, weighingCategory, includeExited)
	if err != nil {
		return domain.FeedWeightBand{}, err
	}
	return BuildFeedWeightBand(source, includeExited), nil
}

// BuildFeedWeightBand turns the repository's raw rollups into display rows and
// the stage-by-stage reconciliation. Pure, so the mapping is unit-testable
// without a database.
//
// The sex / origin / weighing filters were already applied by the repository
// through the weighing module's own scope resolvers, so every evidence row here
// is in scope. A rollup is MATCHED when it has at least one evidence row; the
// rollup count itself is the feed side and is never narrowed by the period or
// the filters, so Rollups - Matched is always the fed pens with no qualifying
// weighing in the period (including pens whose weighed animals have all exited).
func BuildFeedWeightBand(source ports.FeedWeightBandSource, includeExited bool) domain.FeedWeightBand {
	out := domain.FeedWeightBand{
		Reconciliation: domain.FeedWeightBandReconciliation{
			FeedDay:                  source.FeedDay,
			PositiveRows:             source.PositiveRows,
			CollapsedItems:           source.CollapsedItems,
			IndividualAnimalsWeighed: source.IndividualAnimalsWeighed,
			LumpSumAnimalsWeighed:    source.LumpSumAnimalsWeighed,
			ExitedAnimals:            len(source.Exited),
			IncludeExited:            includeExited,
		},
		Rows:      []domain.FeedWeightBandRow{},
		Unmatched: []domain.FeedWeightBandUnmatched{},
		Exited:    []domain.FeedWeightBandExit{},
	}
	loc := biztime.DefaultLocation()
	// The first rollup of each pen, for the exit list's "pen feed today": the artifact's
	// feedByPen rule. Rollups arrive ordered park, pen, shed tag.
	type penKey struct{ park, pen string }
	feedByPen := map[penKey]ports.FeedRollup{}
	parkNameByID := map[string]string{}
	for _, rollup := range source.Rollups {
		parkNameByID[rollup.ParkID] = rollup.ParkName
		k := penKey{rollup.ParkID, rollup.Pen}
		if _, seen := feedByPen[k]; !seen {
			feedByPen[k] = rollup
		}
	}
	feedGivenOf := func(rollup ports.FeedRollup) string {
		parts := make([]string, 0, len(rollup.Items))
		for _, item := range rollup.Items {
			parts = append(parts, fmt.Sprintf("%s %dg/head", domain.FeedItemDisplay(item.Label), int(math.Round(item.GramsPerHead))))
		}
		return strings.Join(parts, " + ")
	}
	for _, x := range source.Exited {
		exit := domain.FeedWeightBandExit{
			ParkID:          x.ParkID,
			ParkName:        parkNameByID[x.ParkID],
			Tag:             x.Tag,
			Pen:             x.Pen,
			Gender:          domain.SexDisplay(x.Sex),
			Reason:          x.ExitReason,
			LifecycleStatus: x.LifecycleStatus,
			ExitedAt:        x.ExitedAt.In(loc).Format("2006-01-02"),
		}
		if x.LastWeighedAt != nil {
			kg := x.LastWeightKg
			exit.LastWeighedAt = x.LastWeighedAt.In(loc).Format("2006-01-02")
			exit.LastBand = domain.FeedBandForKg(kg)
			exit.LastWeightKg = &kg
		}
		if x.Pen != "" {
			if rollup, ok := feedByPen[penKey{x.ParkID, x.Pen}]; ok {
				exit.FeedType = rollup.Workflow
				exit.FeedGiven = feedGivenOf(rollup)
			}
		}
		out.Exited = append(out.Exited, exit)
	}
	for _, rollup := range source.Rollups {
		group := domain.ShedTagGroup(rollup.ShedTag)
		out.Reconciliation.Rollups++
		feedGiven := strings.Split(feedGivenOf(rollup), " + ")
		if len(rollup.Items) == 0 {
			feedGiven = nil
		}
		matched := false
		for _, evidence := range rollup.Evidence {
			gender := domain.GenderDisplay(evidence.FemaleCount, evidence.MaleCount)
			matched = true
			out.Rows = append(out.Rows, domain.FeedWeightBandRow{
				ParkID:          rollup.ParkID,
				ParkName:        rollup.ParkName,
				WeightSource:    evidence.Source,
				Band:            evidence.Band,
				Pen:             rollup.Pen,
				ShedTag:         rollup.ShedTag,
				Group:           group,
				Gender:          gender,
				Breed:           domain.BreedDisplay(rollup.Breed),
				FeedType:        rollup.Workflow,
				RationGroup:     rollup.RationGroup,
				ExperimentArm:   rollup.ExperimentArm,
				FeedGiven:       strings.Join(feedGiven, " + "),
				PenKgPerDay:     rollup.KgPerDay,
				WeightAnimals:   evidence.Animals,
				AverageWeightKg: evidence.AverageWeightKg,
				ExitedAnimals:   evidence.ExitedAnimals,
				ExitedSold:      evidence.ExitedSold,
				ExitedDied:      evidence.ExitedAnimals - evidence.ExitedSold,
			})
		}
		if matched {
			out.Reconciliation.MatchedRollups++
		} else {
			out.Reconciliation.ExcludedRollups++
			out.Unmatched = append(out.Unmatched, domain.FeedWeightBandUnmatched{
				ParkID:        rollup.ParkID,
				ParkName:      rollup.ParkName,
				Pen:           rollup.Pen,
				ShedTag:       rollup.ShedTag,
				Group:         group,
				RationGroup:   rollup.RationGroup,
				ExperimentArm: rollup.ExperimentArm,
				Breed:         domain.BreedDisplay(rollup.Breed),
				FeedType:      rollup.Workflow,
				FeedGiven:     strings.Join(feedGiven, " + "),
				PenKgPerDay:   rollup.KgPerDay,
			})
		}
	}
	domain.DisambiguateGroups(out.Rows)
	domain.SortFeedWeightBandRows(out.Rows)
	domain.SortFeedWeightBandUnmatched(out.Unmatched)
	out.Reconciliation.OutputRows = len(out.Rows)
	return out
}
