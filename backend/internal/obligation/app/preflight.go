package app

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// unbatchedDueKeysetLister is the optional read-only pagination port used by the tie preflight to
// drain EVERY page of a version's unbatched due rows without any row becoming batched. Only the
// production Postgres adapter implements it; an in-memory test fake that doesn't falls back to the
// single-page ListUnbatchedDueForVersion (correct for the small fixtures those tests use).
type unbatchedDueKeysetLister interface {
	ListUnbatchedDueForVersionKeyset(ctx context.Context, tenantID, versionID string, dueBefore time.Time, after *domain.UnbatchedDueCursor, limit int32) ([]domain.UnbatchedDue, error)
}

// maxUnbatchedDuePreflightPages bounds the keyset pagination in listAllUnbatchedDueForPreflight so
// a repo bug that fails to advance the cursor cannot loop forever.
const maxUnbatchedDuePreflightPages = 10000

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
// The main per-version loop replays ALL of each version's unbatched due rows, not just the first
// page: listAllUnbatchedDueForPreflight drains the read-only, obligation_id-keyset
// ListUnbatchedDueForVersionKeyset to exhaustion (that cursor advances by row identity, not by
// rows becoming batched, so a write-free dry run can page it fully). Without this, a same-priority
// cross-vaccine tie among due rows BEYOND the first page slipped past the preflight and aborted the
// real sweep mid-run -- after earlier pages had already committed batches/tasks/stock, the exact
// partial commit this preflight exists to prevent. The park-consolidation replay likewise pages its
// cursor-based ListUnbatchedShedDueForParkConsolidation to exhaustion, so park ties are fully
// covered. The shed fallback pass (batchRemainingShedObligations) reuses the same batchDueGroup
// selection as the main loop over the same shed rows and cannot introduce a NEW same-priority tie
// the main-loop or park replay has not already claimed against.
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
		// Drain ALL due rows for this version read-only (see listAllUnbatchedDueForPreflight), not
		// just the first page, so a tie among rows beyond the first page is detected before any
		// real write commits.
		rows, err := s.listAllUnbatchedDueForPreflight(ctx, tenantID, plan.VersionID, dueBefore)
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

// listAllUnbatchedDueForPreflight returns EVERY unbatched due row for one version, draining the
// read-only obligation_id keyset pager (ListUnbatchedDueForVersionKeyset) to exhaustion so the
// write-free tie preflight is not limited to the first page. The keyset cursor advances by row
// identity, so it drains cleanly without any row becoming batched. Rows come back ordered by
// obligation_id (the stable keyset), so they are re-sorted into the canonical sweep order
// (matching ListUnbatchedDueForVersion's ORDER BY) before grouping, keeping the preflight's
// grouping/date-resolution byte-for-byte identical to the real sweep. A repo that does not
// implement the keyset lister falls back to the single-page ListUnbatchedDueForVersion (correct
// for the small in-memory test fixtures that model a single page).
func (s *SweeperService) listAllUnbatchedDueForPreflight(ctx context.Context, tenantID, versionID string, dueBefore time.Time) ([]domain.UnbatchedDue, error) {
	lister, ok := s.repo.(unbatchedDueKeysetLister)
	if !ok {
		// scale-guard:ignore: bounded per published vaccination version, one read per version, not per-row.
		return s.repo.ListUnbatchedDueForVersion(ctx, tenantID, versionID, dueBefore, s.page)
	}
	var all []domain.UnbatchedDue
	var after *domain.UnbatchedDueCursor
	for pages := 0; pages < maxUnbatchedDuePreflightPages; pages++ {
		// scale-guard:ignore: bounded keyset pagination (cursor advances by obligation_id, LIMIT s.page per query), read-only preflight drain.
		rows, err := lister.ListUnbatchedDueForVersionKeyset(ctx, tenantID, versionID, dueBefore, after, s.page)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		all = append(all, rows...)
		if int32(len(rows)) < s.page {
			break
		}
		last := rows[len(rows)-1]
		if last.ObligationID == "" {
			return nil, fmt.Errorf("obligation: preflight unbatched-due pagination produced a blank cursor")
		}
		if after != nil && after.ObligationID == last.ObligationID {
			return nil, fmt.Errorf("obligation: preflight unbatched-due pagination did not advance after %s", last.ObligationID)
		}
		after = &domain.UnbatchedDueCursor{ObligationID: last.ObligationID}
	}
	sortUnbatchedDueCanonical(all)
	return all, nil
}

// sortUnbatchedDueCanonical orders rows to match ListUnbatchedDueForVersion's ORDER BY
// (scope_type, scope_id, rule_id, target_species, target_animal_stage, due_at, obligation_id) so
// the preflight groups rows exactly as the real sweep does.
func sortUnbatchedDueCanonical(rows []domain.UnbatchedDue) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.ScopeType != b.ScopeType {
			return a.ScopeType < b.ScopeType
		}
		if a.ScopeID != b.ScopeID {
			return a.ScopeID < b.ScopeID
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.TargetSpecies != b.TargetSpecies {
			return a.TargetSpecies < b.TargetSpecies
		}
		if a.TargetAnimalStage != b.TargetAnimalStage {
			return a.TargetAnimalStage < b.TargetAnimalStage
		}
		if !a.DueAt.Equal(b.DueAt) {
			return a.DueAt.Before(b.DueAt)
		}
		return a.ObligationID < b.ObligationID
	})
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
	selected, err = selectParkIDsWithinVisitShotCapForSession(remaining, selected, plannedDate, planner.MaxShotsPerAnimalPerDrive, cfg.getRuleVaccineIdentity, session)
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
			selected, err = selectParkIDsWithinVisitShotCapForSession(remaining, selected, plannedDate, planner.MaxShotsPerAnimalPerDrive, cfg.getRuleVaccineIdentity, session)
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
