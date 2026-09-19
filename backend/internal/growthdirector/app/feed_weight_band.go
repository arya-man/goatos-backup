package app

import (
	"context"
	"fmt"
	"math"
	"sort"
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
// this table and the General tab narrow the identical animals. Every row carries
// both head-count variants (on farm / including exited animals) and the exited
// count split sold / died / other, so the screen's Animals toggle needs no read.
func (s *Service) GetFeedWeightBand(ctx context.Context, actor domain.Actor, parkID, fromBusinessDate, toBusinessDate, sex, origin, weighingCategory string) (domain.FeedWeightBand, error) {
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
	// Same default period as the Growth Director widgets (the tenant's default_period_days).
	settings, err := s.repo.GrowthSettings(ctx, actor.TenantID)
	if err != nil {
		return domain.FeedWeightBand{}, err
	}
	periodStart, periodEndExclusive, err := s.resolveWindow(fromBusinessDate, toBusinessDate, settings.DefaultPeriodDays)
	if err != nil {
		return domain.FeedWeightBand{}, err
	}
	parkIDs, scopeErr := s.resolveMonitorParkScope(ctx, actor, parkID)
	if scopeErr != nil {
		return domain.FeedWeightBand{}, scopeErr
	}
	source, err := s.repo.GetFeedWeightBandSource(ctx, actor.TenantID, parkIDs, periodStart, periodEndExclusive, sex, origin, weighingCategory, settings.BandEdgesKg)
	if err != nil {
		return domain.FeedWeightBand{}, err
	}
	return BuildFeedWeightBand(source, settings.BandEdgesKg), nil
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
func BuildFeedWeightBand(source ports.FeedWeightBandSource, bandEdgesKg []float64) domain.FeedWeightBand {
	bandKeys := domain.FeedBandKeysFor(bandEdgesKg)
	bandOptions := domain.FeedBandOptionsFor(bandEdgesKg)
	labelFor := func(key string) string {
		for _, opt := range bandOptions {
			if opt.Key == key {
				return opt.Label
			}
		}
		return key
	}
	out := domain.FeedWeightBand{
		Reconciliation: domain.FeedWeightBandReconciliation{
			FeedDay:                  source.FeedDay,
			PositiveRows:             source.PositiveRows,
			CollapsedItems:           source.CollapsedItems,
			IndividualAnimalsWeighed: source.IndividualAnimalsWeighed,
			LumpSumAnimalsWeighed:    source.LumpSumAnimalsWeighed,
			ExitedAnimals:            len(source.Exited),
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
			Bucket:          domain.FeedExitBucket(x.LifecycleStatus, x.ExitReason),
			ExitedAt:        x.ExitedAt.In(loc).Format("2006-01-02"),
		}
		if x.LastWeighedAt != nil {
			exit.WeighedInPeriod = true
			out.Reconciliation.ExitedWeighed++
			kg := x.LastWeightKg
			exit.LastWeighedAt = x.LastWeighedAt.In(loc).Format("2006-01-02")
			exit.LastBand = domain.FeedBandForKg(kg, bandEdgesKg)
			exit.LastBandLabel = labelFor(exit.LastBand)
			exit.LastWeightKg = &kg
		}
		if x.Pen != "" {
			if rollup, ok := feedByPen[penKey{x.ParkID, x.Pen}]; ok {
				exit.FeedType = rollup.Workflow
				exit.FeedGiven = feedGivenOf(rollup)
			}
		}
		if !exit.WeighedInPeriod {
			out.Reconciliation.ExitedNotWeighed++
		}
		switch exit.Bucket {
		case domain.FeedExitSold:
			out.Reconciliation.ExitedSold++
		case domain.FeedExitDied:
			out.Reconciliation.ExitedDied++
		default:
			out.Reconciliation.ExitedOther++
		}
		out.Exited = append(out.Exited, exit)
	}
	sheetsSeen := map[[2]string]bool{}
	for _, rollup := range source.Rollups {
		if k := [2]string{rollup.ParkID, rollup.Workflow}; !sheetsSeen[k] && rollup.FeedDay != "" {
			sheetsSeen[k] = true
			out.Reconciliation.FeedSheets = append(out.Reconciliation.FeedSheets, domain.FeedSheetUsed{ParkID: rollup.ParkID, ParkName: rollup.ParkName, Workflow: rollup.Workflow, FeedDay: rollup.FeedDay})
		}
		group := domain.ShedTagGroup(rollup.ShedTag)
		out.Reconciliation.Rollups++
		feedGiven := strings.Split(feedGivenOf(rollup), " + ")
		if len(rollup.Items) == 0 {
			feedGiven = nil
		}
		matched, matchedAll := false, false
		for _, evidence := range rollup.Evidence {
			gender := domain.GenderDisplay(evidence.FemaleCount, evidence.MaleCount)
			if evidence.Animals > 0 {
				matched = true
			}
			if evidence.AnimalsAll > 0 {
				matchedAll = true
			}
			out.Rows = append(out.Rows, domain.FeedWeightBandRow{
				ParkID:             rollup.ParkID,
				ParkName:           rollup.ParkName,
				WeightSource:       evidence.Source,
				Band:               evidence.Band,
				Pen:                rollup.Pen,
				ShedTag:            rollup.ShedTag,
				Group:              group,
				Gender:             gender,
				Breed:              domain.BreedDisplay(rollup.Breed),
				FeedType:           rollup.Workflow,
				RationGroup:        rollup.RationGroup,
				ExperimentArm:      rollup.ExperimentArm,
				FeedGiven:          strings.Join(feedGiven, " + "),
				PenKgPerDay:        rollup.KgPerDay,
				WeightAnimals:      evidence.Animals,
				AverageWeightKg:    evidence.AverageWeightKg,
				WeightAnimalsAll:   evidence.AnimalsAll,
				AverageWeightKgAll: evidence.AverageWeightKgAll,
				GenderAll:          domain.GenderDisplay(evidence.FemaleCountAll, evidence.MaleCountAll),
				ExitedAnimals:      evidence.ExitedAnimals,
				ExitedSold:         evidence.ExitedSold,
				ExitedDied:         evidence.ExitedDied,
				ExitedOther:        evidence.ExitedAnimals - evidence.ExitedSold - evidence.ExitedDied,
			})
		}
		if matched {
			out.Reconciliation.MatchedRollups++
		} else {
			out.Reconciliation.ExcludedRollups++
		}
		if matchedAll {
			out.Reconciliation.MatchedRollupsAll++
		} else {
			out.Reconciliation.ExcludedRollupsAll++
			// The Not shown list is the rollups with NO weighed animal at all; a rollup whose
			// only weighed animals have since left keeps its band rows (n = 0) and the screen
			// files it under Not shown itself when reading on-farm.
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
	sort.Slice(out.Reconciliation.FeedSheets, func(i, j int) bool {
		a, b := out.Reconciliation.FeedSheets[i], out.Reconciliation.FeedSheets[j]
		if a.ParkName != b.ParkName {
			return a.ParkName < b.ParkName
		}
		return a.Workflow < b.Workflow
	})
	if out.Reconciliation.FeedSheets == nil {
		out.Reconciliation.FeedSheets = []domain.FeedSheetUsed{}
	}
	domain.DisambiguateGroups(out.Rows)
	for i := range out.Rows {
		out.Rows[i].BandLabel = labelFor(out.Rows[i].Band)
	}
	out.Bands = bandOptions
	domain.SortFeedWeightBandRows(out.Rows, bandKeys)
	domain.SortFeedWeightBandUnmatched(out.Unmatched)
	out.Reconciliation.OutputRows = len(out.Rows)
	return out
}
