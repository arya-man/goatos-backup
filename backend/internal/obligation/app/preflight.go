package app

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// PreflightVisitShotCapTies is the VAX-REV-04 / RV-01 fix: a write-free dry run over every plan
// (already priority-sorted -- see SortSweepVersionsByPriority) that surfaces a cross-version
// *ShotCapPriorityTieError BEFORE the caller sweeps a single plan for real.
//
// Without this, the production caller (kernelstages.ObligationSweeperStage.Run and
// cmd/obligation-sweeper) swept plans one at a time against a shared SweepSession, committing each
// plan's batches/SOP tasks/stock reservations as it went. When a LATER plan's obligation competed
// for an over-cap same-priority visit against an EARLIER plan's already-claimed slot,
// selectIDsWithinVisitShotCapForSession returned *ShotCapPriorityTieError and the run aborted -- but
// every earlier plan's writes were already durably committed. The operator saw a failed run yet the
// earlier, arbitrary (version-processing-order) winners were left fully executable: a silent
// partial commit.
//
// This function mirrors the REAL per-plan, per-group decision path byte-for-byte -- same
// groupUnbatchedDue grouping, same batchPlannedDate/pickBestDriveDateWithHold date resolution, same
// selectIDsWithinVisitShotCapForSession claim/tie logic, same nextFeasibleUnbatchedDriveDateAfter
// overflow retry -- using a THROWAWAY session seeded from the same persisted, cross-pass shot
// counts the real sweep will also seed from (see seedVisitShotCounts), so it cannot pass here and
// then fail differently once the real writes run. It only reads (ListUnbatchedDueForVersion,
// ListUnbatchedShedDueForParkConsolidation, CountVisitShotsForTargets); it never calls
// CreateBatchWithObligations or any other write path, so a clean preflight guarantees zero
// batches/tasks/reservations exist before the caller proceeds.
//
// Coverage (RV-01): the main per-version group loop AND the park-consolidation pass are both
// replayed. Park consolidation is where the main loop's DEFERRED small shed groups
// (deferShedGroupToPark) are actually batched, so a tie that only manifests when those singletons
// merge into a shared park drive -- invisible to the main-loop replay because that replay skips the
// deferred groups -- is caught here. To avoid a false-positive tie from double-counting, the park
// replay ignores every obligation the main-loop replay already claimed (in the real sweep those
// rows are batched and removed before park consolidation lists them).
//
// Residual scope note: the main per-version loop replays only each version's FIRST page (s.page) of
// unbatched due rows, because a write-free dry run cannot advance ListUnbatchedDueForVersion's
// cursor (it depends on rows becoming batched, which only CreateBatchWithObligations does). A
// same-priority tie can only occur between two obligations sharing the exact same (target, date)
// visit, which -- given ListUnbatchedDueForVersion orders by due date -- means they overwhelmingly
// land in the same first page together. A tie that only manifests beyond the first page for a single
// version is still caught (and still aborts the run) by the real sweep's own
// selectIDsWithinVisitShotCapForSession call; only the "zero committed writes on abort" guarantee is
// narrowed to the first-page case for that residual scenario. The park-consolidation replay, by
// contrast, DOES page its cursor-based ListUnbatchedShedDueForParkConsolidation to exhaustion (that
// cursor advances by row identity, not by rows becoming batched), so park ties are fully covered.
// The shed fallback pass (batchRemainingShedObligations) reuses the same batchDueGroup selection as
// the main loop over the same shed rows and cannot introduce a NEW same-priority tie the main-loop
// or park replay has not already claimed against.
//
// It also does NOT mutate the caller's real session: callers should construct their real
// SweepSession and pass it to the subsequent SweepVersionWithSession calls only after this
// preflight returns nil.
func (s *SweeperService) PreflightVisitShotCapTies(ctx context.Context, tenantID string, plans []SweepVersionPriority, dueBefore time.Time) error {
	if len(plans) < 2 {
		// A tie needs two DIFFERENT vaccine codes competing for the same visit; a single plan can
		// never trip rejectOrTie against itself (see SweepSession.rejectOrTie).
		return nil
	}
	preflight := NewSweepSession()
	// Obligation IDs the main-loop replay has already claimed onto preflight; the park replay must
	// skip them so a shed obligation batched by the main loop is not counted twice (which would
	// fabricate a tie that the real, write-advancing sweep never hits).
	claimed := make(map[string]struct{})
	for _, plan := range plans {
		planner := normalizedDrivePlannerSettings(plan.Config.DrivePlanner, plan.Config.VaccineCode)
		// One read per published vaccination version for one tenant (a small fixed set), not per-row;
		// mirrors the real sweep's own per-version list in ObligationSweeperStage.Run.
		// scale-guard:ignore: bounded per published vaccination version, one read per version, not per-row.
		rows, err := s.repo.ListUnbatchedDueForVersion(ctx, tenantID, plan.VersionID, dueBefore, s.page)
		if err != nil {
			return fmt.Errorf("obligation: preflight list unbatched due for version %s: %w", plan.VersionID, err)
		}
		if len(rows) == 0 {
			continue
		}
		order, groups := groupUnbatchedDue(rows, planner.SpeciesGroupingPolicy)
		for _, k := range order {
			g := groups[k]
			if deferShedGroupToPark(plan.Config, g.scopeType, len(g.ids)) {
				continue
			}
			if err := s.preflightGroup(ctx, tenantID, plan.Config, planner, dueBefore, preflight, g, claimed); err != nil {
				return err
			}
		}
	}
	for _, plan := range plans {
		if err := s.preflightParkConsolidation(ctx, tenantID, plan, dueBefore, preflight, claimed); err != nil {
			return err
		}
	}
	return nil
}

// preflightGroup replays batchDueGroup's date-resolution and shot-cap-selection decision for one
// dueGroup, write-free. See PreflightVisitShotCapTies for why this must mirror batchDueGroup
// exactly. Every obligation it claims is recorded in claimed so the park-consolidation replay does
// not double-count it.
func (s *SweeperService) preflightGroup(ctx context.Context, tenantID string, cfg SweepConfig, planner domain.DrivePlannerSettings, dueBefore time.Time, session *SweepSession, g *dueGroup, claimed map[string]struct{}) error {
	plannedDate := batchPlannedDate(g.rows[0].DueAt)
	if planner.Enabled {
		if picked := pickBestDriveDateWithHold(dueBefore, driveCandidatesFromUnbatched(g.rows), planner); picked != nil {
			plannedDate = picked
		}
	}
	targetIDs := distinctUnbatchedTargetIDs(g.rows)
	if err := s.seedVisitShotCounts(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session); err != nil {
		return err
	}
	// BUG #1: use per-rule vaccine identity instead of version-level wrapper.
	ruleVaccineID := cfg.getRuleVaccineIdentity(g.ruleID)
	selectedIDs, err := selectIDsWithinVisitShotCapForSession(g.rows, plannedDate, planner.MaxShotsPerAnimalPerDrive, ruleVaccineID.VaccineCode, ruleVaccineID.VaccinePriority, session)
	if err != nil {
		return err
	}
	recordClaimed(claimed, selectedIDs)
	if len(selectedIDs) == 0 && plannedDate != nil && planner.MaxShotsPerAnimalPerDrive > 0 {
		if overflowDate := nextFeasibleUnbatchedDriveDateAfter(*plannedDate, g.rows); overflowDate != nil {
			plannedDate = overflowDate
			if err := s.seedVisitShotCounts(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session); err != nil {
				return err
			}
			overflowIDs, err := selectIDsWithinVisitShotCapForSession(g.rows, plannedDate, planner.MaxShotsPerAnimalPerDrive, ruleVaccineID.VaccineCode, ruleVaccineID.VaccinePriority, session)
			if err != nil {
				return err
			}
			recordClaimed(claimed, overflowIDs)
		}
	}
	return nil
}

// preflightParkConsolidation replays consolidateParkDrivesWithVisitCounts' merge decision for one
// plan, write-free, onto the shared preflight session (RV-01). Obligations already claimed by the
// main-loop replay are excluded up front, matching the real flow where they are batched and removed
// before park consolidation lists them.
func (s *SweeperService) preflightParkConsolidation(ctx context.Context, tenantID string, plan SweepVersionPriority, dueBefore time.Time, session *SweepSession, claimed map[string]struct{}) error {
	cfg := plan.Config
	settings := cfg.ParkConsolidation
	if !settings.Enabled {
		return nil
	}
	planner := normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode)
	minMergeTargets := settings.MinParkMergeTargets
	if minMergeTargets < 1 {
		minMergeTargets = domain.DefaultParkConsolidationSettings().MinParkMergeTargets
	}
	minMergeSheds := settings.MinParkMergeSheds
	if minMergeSheds < 1 {
		minMergeSheds = domain.DefaultParkConsolidationSettings().MinParkMergeSheds
	}

	groups := make(map[string][]domain.ParkConsolidationCandidate)
	var after *domain.ParkConsolidationCursor
	seenCursors := map[string]struct{}{}
	for {
		// scale-guard:ignore: bounded keyset pagination (cursor advances by row identity, LIMIT s.page per query); mirrors consolidateParkDrivesWithVisitCounts' own park listing, not a per-row round trip.
		rows, err := s.repo.ListUnbatchedShedDueForParkConsolidation(ctx, tenantID, plan.VersionID, dueBefore, s.page, after)
		if err != nil {
			return fmt.Errorf("obligation: preflight list park consolidation for version %s: %w", plan.VersionID, err)
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if _, done := claimed[row.ObligationID]; done {
				continue
			}
			key := row.ParkID + "|" + speciesGroupingKey(row.TargetSpecies, row.TargetAnimalStage, planner.SpeciesGroupingPolicy)
			groups[key] = append(groups[key], row)
		}
		if int32(len(rows)) < s.page {
			break
		}
		next := parkConsolidationCursor(rows[len(rows)-1])
		key := parkConsolidationCursorKey(next)
		if key == "" {
			return fmt.Errorf("obligation: preflight park consolidation pagination did not produce an advance cursor")
		}
		if _, ok := seenCursors[key]; ok {
			return fmt.Errorf("obligation: preflight park consolidation pagination did not advance after cursor %s", key)
		}
		seenCursors[key] = struct{}{}
		after = next
	}

	now := biztime.BusinessDayStart(dueBefore)
	for _, rows := range groups {
		if len(rows) == 0 {
			continue
		}
		remaining := append([]domain.ParkConsolidationCandidate(nil), rows...)
		for len(remaining) >= int(minMergeTargets) && uniqueShedCount(remaining) >= int(minMergeSheds) {
			next, stop, err := s.preflightParkMergeStep(ctx, tenantID, cfg, planner, now, dueBefore, session, remaining, minMergeTargets, minMergeSheds)
			if err != nil {
				return err
			}
			remaining = next
			if stop {
				break
			}
		}
	}
	return nil
}

// preflightParkMergeStep mirrors parkMergeStep's date-resolution + shot-cap-selection decision for
// one merge attempt, write-free: same pickBestParkDriveDate, same seed, same
// selectParkIDsWithinVisitShotCapForSession claim/tie logic, same overflow retry -- but it never
// calls CreateBatchWithObligations. It returns the rows still un-merged and whether the caller's
// merge loop should stop.
func (s *SweeperService) preflightParkMergeStep(ctx context.Context, tenantID string, cfg SweepConfig, planner domain.DrivePlannerSettings, now, dueBefore time.Time, session *SweepSession, remaining []domain.ParkConsolidationCandidate, minMergeTargets, minMergeSheds int32) (newRemaining []domain.ParkConsolidationCandidate, stop bool, err error) {
	plannedDate, selected := pickBestParkDriveDate(now, remaining)
	targetIDs := distinctParkTargetIDs(remaining)
	if err := s.seedVisitShotCounts(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session); err != nil {
		return remaining, true, err
	}
	selected, err = selectParkIDsWithinVisitShotCapForSession(remaining, selected, plannedDate, planner.MaxShotsPerAnimalPerDrive, cfg.VaccineCode, planner.VaccinePriority, session)
	if err != nil {
		return remaining, true, err
	}
	if len(selected) == 0 && plannedDate != nil && planner.MaxShotsPerAnimalPerDrive > 0 {
		if overflowDate := nextFeasibleParkDriveDateAfter(*plannedDate, remaining); overflowDate != nil {
			plannedDate = overflowDate
			selected = obligationsFeasibleOnDate(*plannedDate, remaining)
			if err := s.seedVisitShotCounts(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session); err != nil {
				return remaining, true, err
			}
			selected, err = selectParkIDsWithinVisitShotCapForSession(remaining, selected, plannedDate, planner.MaxShotsPerAnimalPerDrive, cfg.VaccineCode, planner.VaccinePriority, session)
			if err != nil {
				return remaining, true, err
			}
		}
	}
	if plannedDate == nil || int32(len(selected)) < minMergeTargets || uniqueShedCount(filterRows(remaining, selected)) < int(minMergeSheds) {
		return remaining, true, nil
	}
	return removeRows(remaining, selected), false, nil
}

// recordClaimed adds every non-blank obligation id to claimed.
func recordClaimed(claimed map[string]struct{}, ids []string) {
	for _, id := range ids {
		if id == "" {
			continue
		}
		claimed[id] = struct{}{}
	}
}
