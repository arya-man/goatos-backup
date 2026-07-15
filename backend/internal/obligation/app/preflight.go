package app

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// PreflightVisitShotCapTies is the VAX-REV-04 fix: a write-free dry run over every plan (already
// priority-sorted -- see SortSweepVersionsByPriority) that surfaces a cross-version
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
// CountVisitShotsForTargets); it never calls CreateBatchWithObligations or any other write path, so
// a clean preflight guarantees zero batches/tasks/reservations exist before the caller proceeds.
//
// Scope note: this preflight replays each version's FIRST page of unbatched due rows (s.page, the
// same page size the real sweep's own pagination uses) rather than paging to exhaustion, because a
// write-free dry run has no way to advance a cursor that depends on rows becoming batched (the
// real sweep's own pagination loop advances precisely because CreateBatchWithObligations removes
// rows from the "unbatched" set). A same-priority tie can only occur between two obligations that
// share the exact same (target, date) visit, which -- given ListUnbatchedDueForVersion orders by
// due date -- means they are overwhelmingly likely to land in the same first page together. A tie
// that only manifests beyond the first page for a single version is not caught here, but is still
// caught (and still aborts the run) by the real sweep's own selectIDsWithinVisitShotCapForSession
// call; only the "zero committed writes on abort" guarantee is narrowed to the first-page case for
// that specific residual scenario. It only applies to the main per-version group loop -- park
// consolidation, the shed fallback pass, and combo-drive alignment are not replayed here.
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
	for _, plan := range plans {
		planner := normalizedDrivePlannerSettings(plan.Config.DrivePlanner, plan.Config.VaccineCode)
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
			if err := s.preflightGroup(ctx, tenantID, plan.Config, planner, dueBefore, preflight, g); err != nil {
				return err
			}
		}
	}
	return nil
}

// preflightGroup replays batchDueGroup's date-resolution and shot-cap-selection decision for one
// dueGroup, write-free. See PreflightVisitShotCapTies for why this must mirror batchDueGroup
// exactly.
func (s *SweeperService) preflightGroup(ctx context.Context, tenantID string, cfg SweepConfig, planner domain.DrivePlannerSettings, dueBefore time.Time, session *SweepSession, g *dueGroup) error {
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
	selectedIDs, err := selectIDsWithinVisitShotCapForSession(g.rows, plannedDate, planner.MaxShotsPerAnimalPerDrive, cfg.VaccineCode, planner.VaccinePriority, session)
	if err != nil {
		return err
	}
	if len(selectedIDs) == 0 && plannedDate != nil && planner.MaxShotsPerAnimalPerDrive > 0 {
		if overflowDate := nextFeasibleUnbatchedDriveDateAfter(*plannedDate, g.rows); overflowDate != nil {
			plannedDate = overflowDate
			if err := s.seedVisitShotCounts(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session); err != nil {
				return err
			}
			if _, err := selectIDsWithinVisitShotCapForSession(g.rows, plannedDate, planner.MaxShotsPerAnimalPerDrive, cfg.VaccineCode, planner.VaccinePriority, session); err != nil {
				return err
			}
		}
	}
	return nil
}
