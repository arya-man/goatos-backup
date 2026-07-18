package app

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// unbatchedDueKeysetLister is implemented by the production Postgres repo so the preflight can
// keyset-scan EVERY unbatched-due candidate (RV-02), not just the first page. A repo that does not
// implement it (a simple in-memory test fake) gets the first-page-only fallback.
type unbatchedDueKeysetLister interface {
	ListUnbatchedDueForVersionKeyset(ctx context.Context, tenantID, versionID string, dueBefore time.Time, after *domain.UnbatchedDueCursor, limit int32) ([]domain.UnbatchedDue, error)
}

// unbatchedDueKeysetListerHWM is the RV-05 sibling of unbatchedDueKeysetLister: it also bounds the
// keyset scan by createdAtHWM, excluding newly inserted rows. The returned SweepCandidateSnapshot
// additionally freezes membership against pre-existing rows reopened or rescheduled after
// preflight. A repo that implements unbatchedDueKeysetLister but not this (a hand-rolled test fake
// with no concurrent writer) is used through the plain, unbounded keyset method.
type unbatchedDueKeysetListerHWM interface {
	ListUnbatchedDueForVersionKeysetHWM(ctx context.Context, tenantID, versionID string, dueBefore time.Time, after *domain.UnbatchedDueCursor, limit int32, createdAtHWM time.Time) ([]domain.UnbatchedDue, error)
}

// maxPreflightUnbatchedPages backstops the keyset scan against a runaway loop (the cursor strictly
// advances by unique obligation_id, so this is only a safety ceiling, never hit in practice).
const maxPreflightUnbatchedPages = 100000

// SweepCandidateSnapshot is the exact obligation-id membership observed by the write-free
// preflight, partitioned by protocol version. Production real-sweep reads are constrained to this
// allowlist, so a pre-existing row that is reopened/rescheduled after preflight cannot enter the
// current cycle merely because its old created_at remains below the cycle HWM. Rows may still leave
// the snapshot if their current status/health/batch state becomes ineligible; membership can never
// grow until the next cycle runs its own preflight.
type SweepCandidateSnapshot struct {
	byVersion map[string][]string
}

func newSweepCandidateSnapshot(plans []SweepVersionPriority) *SweepCandidateSnapshot {
	snapshot := &SweepCandidateSnapshot{byVersion: make(map[string][]string, len(plans))}
	for _, plan := range plans {
		// Preserve an explicit non-nil empty slice: nil means "legacy/unbounded" at the repository
		// seam, while an empty snapshot means "preflight observed no candidates".
		snapshot.byVersion[plan.VersionID] = []string{}
	}
	return snapshot
}

func (s *SweepCandidateSnapshot) candidateIDs(versionID string) []string {
	if s == nil {
		return nil
	}
	ids, ok := s.byVersion[versionID]
	if !ok {
		return []string{}
	}
	return ids
}

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
// land in the same first page together. The park-consolidation replay, by contrast, DOES page its
// cursor-based ListUnbatchedShedDueForParkConsolidation to exhaustion (that cursor advances by row
// identity, not by rows becoming batched), so park ties are fully covered. The shed fallback pass
// (batchRemainingShedObligations) is also replayed against the unclaimed snapshot rows, because
// park thresholds can deliberately leave deferred shed groups for fallback batching.
//
// It also does NOT mutate the caller's real session: callers should construct their real
// SweepSession and pass it to the subsequent SweepVersionWithSession calls only after this
// preflight returns nil.
//
// createdAtHWM is the RV-05 insertion high-water mark (zero = unbounded). Production callers use
// PreflightVisitShotCapTiesWithSnapshot and pass both its returned snapshot and this same HWM to
// SweepVersionWithSessionNoFinalizeSnapshot. The HWM excludes later inserts; the snapshot excludes
// pre-existing rows that become eligible later (for example deferred -> scheduled recovery).
func (s *SweeperService) PreflightVisitShotCapTies(ctx context.Context, tenantID string, plans []SweepVersionPriority, dueBefore time.Time, createdAtHWM time.Time) error {
	_, err := s.PreflightVisitShotCapTiesWithSnapshot(ctx, tenantID, plans, dueBefore, createdAtHWM)
	return err
}

// PreflightVisitShotCapTiesWithSnapshot performs the same write-free medical-plan validation as
// PreflightVisitShotCapTies and also returns the exact candidate membership that passed it. The
// production orchestration must pass this snapshot to every real per-version sweep in the cycle.
func (s *SweeperService) PreflightVisitShotCapTiesWithSnapshot(ctx context.Context, tenantID string, plans []SweepVersionPriority, dueBefore time.Time, createdAtHWM time.Time) (*SweepCandidateSnapshot, error) {
	return s.PreflightVisitShotCapTiesWithSnapshotAsOf(ctx, tenantID, plans, time.Time{}, dueBefore, createdAtHWM)
}

// PreflightVisitShotCapTiesWithSnapshotAsOf is PreflightVisitShotCapTiesWithSnapshot with split
// asOf/dueBefore semantics. It must mirror the real sweep's asOf-driven planned-date math.
func (s *SweeperService) PreflightVisitShotCapTiesWithSnapshotAsOf(ctx context.Context, tenantID string, plans []SweepVersionPriority, asOf, dueBefore time.Time, createdAtHWM time.Time) (*SweepCandidateSnapshot, error) {
	snapshot := newSweepCandidateSnapshot(plans)
	if len(plans) == 0 {
		return snapshot, nil
	}
	// RV-01: do NOT skip a single plan. One protocol version can carry several per-rule vaccines
	// (round-2 per-rule identity, cfg.getRuleVaccineIdentity), so three equal-priority vaccines in
	// ONE plan can still tie for a two-shot visit -- equating plan count with vaccine count let that
	// single-version matrix tie slip past the gate and partial-commit at real-sweep time.
	preflight := NewSweepSession()
	// Obligation IDs the main-loop replay has already claimed onto preflight; the park replay must
	// skip them so a shed obligation batched by the main loop is not counted twice (which would
	// fabricate a tie that the real, write-advancing sweep never hits).
	claimed := make(map[string]struct{})
	// RV-02: page ALL unbatched-due candidates via keyset, not just the first page. The real sweep
	// drains its pages by batching rows out of the unbatched set; a write-free preflight cannot, so
	// it advances a stable ORDER-BY-tuple cursor and replays each page's groups exactly as the real
	// sweep replays each of its own pages -- a conflicting obligation beyond page one is now caught
	// here instead of committed-then-aborted at real-sweep time. A repo that does not implement the
	// keyset lister (simple test fakes) falls back to the pre-RV-02 first-page-only coverage.
	keysetLister, keyset := s.repo.(unbatchedDueKeysetLister)
	keysetListerHWM, keysetHWM := s.repo.(unbatchedDueKeysetListerHWM)
	for _, plan := range plans {
		planner := normalizedDrivePlannerSettings(plan.Config.DrivePlanner, plan.Config.VaccineCode)
		var parkCandidateIDs []string
		var after *domain.UnbatchedDueCursor
		seenCandidates := make(map[string]struct{})
		for page := 0; page < maxPreflightUnbatchedPages; page++ {
			var rows []domain.UnbatchedDue
			var err error
			switch {
			case keysetHWM && !createdAtHWM.IsZero():
				// scale-guard:ignore: keyset pagination, bounded to s.page rows per read; mirrors the real sweep's own paged unbatched-due scan, ALSO bounded by the RV-05 high-water mark.
				rows, err = keysetListerHWM.ListUnbatchedDueForVersionKeysetHWM(ctx, tenantID, plan.VersionID, dueBefore, after, s.page, createdAtHWM)
			case keyset:
				// scale-guard:ignore: keyset pagination, bounded to s.page rows per read; mirrors the real sweep's own paged unbatched-due scan.
				rows, err = keysetLister.ListUnbatchedDueForVersionKeyset(ctx, tenantID, plan.VersionID, dueBefore, after, s.page)
			default:
				// scale-guard:ignore: fallback for non-keyset repos; one bounded read, first page only.
				rows, err = s.repo.ListUnbatchedDueForVersion(ctx, tenantID, plan.VersionID, dueBefore, s.page)
			}
			if err != nil {
				return nil, fmt.Errorf("obligation: preflight list unbatched due for version %s: %w", plan.VersionID, err)
			}
			if len(rows) == 0 {
				break
			}
			last := rows[len(rows)-1]
			uniqueRows := rows[:0]
			for _, row := range rows {
				if _, duplicate := seenCandidates[row.ObligationID]; duplicate {
					continue
				}
				seenCandidates[row.ObligationID] = struct{}{}
				snapshot.byVersion[plan.VersionID] = append(snapshot.byVersion[plan.VersionID], row.ObligationID)
				uniqueRows = append(uniqueRows, row)
			}
			if plan.Config.ParkConsolidation.Enabled {
				for _, row := range uniqueRows {
					parkCandidateIDs = append(parkCandidateIDs, row.ObligationID)
				}
			} else {
				order, groups := groupUnbatchedDue(uniqueRows, planner.SpeciesGroupingPolicy)
				order = orderDueGroupsByVaccinePriority(order, groups, plan.Config)
				for _, k := range order {
					g := groups[k]
					claimedAny, err := s.preflightGroup(ctx, tenantID, plan.Config, planner, asOf, dueBefore, preflight, g, claimed)
					if err != nil {
						return nil, err
					}
					if claimedAny {
						continue
					}
					parkCandidateIDs = append(parkCandidateIDs, g.ids...)
				}
			}
			if !keyset || int32(len(rows)) < s.page {
				break
			}
			after = &domain.UnbatchedDueCursor{
				ScopeType:    last.ScopeType,
				ScopeID:      last.ScopeID,
				RuleID:       last.RuleID,
				DueAt:        last.DueAt,
				ObligationID: last.ObligationID,
			}
		}
		if _, ok := s.repo.(snapshotParkConsolidationLister); ok {
			if parkCandidateIDs == nil {
				parkCandidateIDs = []string{}
			}
		}
		parkClaimed := make(map[string]struct{})
		if err := s.preflightParkConsolidation(ctx, tenantID, plan, asOf, dueBefore, preflight, claimed, parkClaimed, createdAtHWM, parkCandidateIDs); err != nil {
			return nil, err
		}
		if err := s.preflightRemainingShedObligations(ctx, tenantID, plan, asOf, dueBefore, preflight, claimed, parkClaimed, createdAtHWM, parkCandidateIDs); err != nil {
			return nil, err
		}
	}
	return snapshot, nil
}

// preflightGroup replays batchDueGroup's date-resolution and shot-cap-selection decision for one
// dueGroup, write-free. See PreflightVisitShotCapTies for why this must mirror batchDueGroup
// exactly. Every obligation it claims is recorded in claimed so the park-consolidation replay does
// not double-count it.
func (s *SweeperService) preflightGroup(ctx context.Context, tenantID string, cfg SweepConfig, planner domain.DrivePlannerSettings, asOf, dueBefore time.Time, session *SweepSession, g *dueGroup, claimed map[string]struct{}) (bool, error) {
	operationalAsOf := dueGroupOperationalAsOf(asOf, dueBefore, g)
	plannedDate := batchPlannedDate(g.rows[0].DueAt)
	if planner.Enabled {
		if picked := pickBestDriveDateWithHold(operationalAsOf, driveCandidatesFromUnbatched(g.rows), planner); picked != nil {
			plannedDate = picked
		} else {
			return false, nil
		}
	}
	targetIDs := distinctUnbatchedTargetIDs(g.rows)
	// BUG #1: use per-rule vaccine identity instead of version-level wrapper.
	ruleVaccineID := cfg.getRuleVaccineIdentity(g.ruleID)
	cellsPerObligation := normalizedDosesPerGoat(cfg.forRule(g.ruleID).DosesPerGoat)
	_, selectedIDs, _, err := s.preflightBestUnbatchedDriveDateWithVisitCap(ctx, tenantID, g.rows, targetIDs, plannedDate, planner, ruleVaccineID, cellsPerObligation, session)
	if err != nil {
		return false, err
	}
	recordClaimed(claimed, selectedIDs)
	return len(selectedIDs) > 0, nil
}

func (s *SweeperService) preflightBestUnbatchedDriveDateWithVisitCap(ctx context.Context, tenantID string, rows []domain.UnbatchedDue, targetIDs []string, plannedDate *time.Time, planner domain.DrivePlannerSettings, ruleVaccineID RuleVaccineIdentity, cellsPerObligation int32, session *SweepSession) (*time.Time, []string, []shotCapReservation, error) {
	if cellsPerObligation <= 0 {
		cellsPerObligation = 1
	}
	parkID := firstUnbatchedParkID(rows)
	if plannedDate == nil || (planner.MaxShotsPerAnimalPerDrive <= 0 && planner.MaxGoatsPerDrive <= 0) {
		visitRelease, err := s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session)
		if err != nil {
			return plannedDate, nil, nil, err
		}
		driveRelease, err := s.lockAndRefreshDriveCapacity(ctx, tenantID, parkID, plannedDate, planner.MaxGoatsPerDrive, session)
		if err != nil {
			_ = visitRelease(ctx)
			return plannedDate, nil, nil, err
		}
		release := combineReleases(driveRelease, visitRelease)
		selectedIDs, shotClaims, err := selectIDsWithinVisitShotCapForSession(rows, plannedDate, planner, ruleVaccineID.VaccineCode, ruleVaccineID.VaccinePriority, session)
		if err != nil {
			_ = release(ctx)
			return plannedDate, nil, nil, err
		}
		cappedIDs := limitUnbatchedSelectionByDriveCells(rows, selectedIDs, plannedDate, planner, session, cellsPerObligation)
		if len(cappedIDs) < len(selectedIDs) {
			session.releaseClaims(claimsOutsideSelection(shotClaims, cappedIDs))
			shotClaims = claimsOutsideSelection(shotClaims, differenceIDs(selectedIDs, cappedIDs))
			selectedIDs = cappedIDs
		}
		claimPreflightUnbatchedDriveCapacity(session, rows, selectedIDs, plannedDate, cellsPerObligation)
		return plannedDate, selectedIDs, shotClaims, release(ctx)
	}

	candidates := driveCandidatesFromUnbatched(rows)
	latest := latestUnbatchedDriveDate(candidates)
	if latest.IsZero() {
		return plannedDate, nil, nil, nil
	}

	var bestDate *time.Time
	var bestIDs []string
	bestAnimals := -1
	for probe := businessDate(*plannedDate); !probe.After(latest); {
		if len(obligationsFeasibleOnDriveDateForPlanner(probe, candidates, planner)) > 0 {
			day := probe
			if err := s.seedVisitShotCounts(ctx, tenantID, targetIDs, &day, planner.MaxShotsPerAnimalPerDrive, session); err != nil {
				return plannedDate, nil, nil, err
			}
			driveRelease, err := s.lockAndRefreshDriveCapacity(ctx, tenantID, parkID, &day, planner.MaxGoatsPerDrive, session)
			if err != nil {
				return plannedDate, nil, nil, err
			}
			selectedIDs, shotClaims, err := selectIDsWithinVisitShotCapForSession(rows, &day, planner, ruleVaccineID.VaccineCode, ruleVaccineID.VaccinePriority, session)
			if err != nil {
				_ = driveRelease(ctx)
				return plannedDate, nil, nil, err
			}
			cappedIDs := limitUnbatchedSelectionByDriveCells(rows, selectedIDs, &day, planner, session, cellsPerObligation)
			session.releaseClaims(claimsOutsideSelection(shotClaims, cappedIDs))
			shotClaims = claimsOutsideSelection(shotClaims, differenceIDs(selectedIDs, cappedIDs))
			animals := uniqueUnbatchedTargetCount(selectedUnbatchedRows(rows, cappedIDs))
			session.releaseClaims(shotClaims)
			if err := driveRelease(ctx); err != nil {
				return plannedDate, nil, nil, err
			}
			if animals > bestAnimals || (animals == bestAnimals && len(cappedIDs) > len(bestIDs)) {
				chosen := day
				bestDate = &chosen
				bestIDs = append(bestIDs[:0], cappedIDs...)
				bestAnimals = animals
			}
		}
		next := nextFeasibleUnbatchedDriveDateAfter(probe, rows, planner)
		if next == nil {
			break
		}
		probe = *next
	}
	if bestDate == nil || len(bestIDs) == 0 {
		return plannedDate, nil, nil, nil
	}

	if err := s.seedVisitShotCounts(ctx, tenantID, targetIDs, bestDate, planner.MaxShotsPerAnimalPerDrive, session); err != nil {
		return bestDate, nil, nil, err
	}
	driveRelease, err := s.lockAndRefreshDriveCapacity(ctx, tenantID, parkID, bestDate, planner.MaxGoatsPerDrive, session)
	if err != nil {
		return bestDate, nil, nil, err
	}
	selectedIDs, shotClaims, err := selectIDsWithinVisitShotCapForSession(rows, bestDate, planner, ruleVaccineID.VaccineCode, ruleVaccineID.VaccinePriority, session)
	if err != nil {
		_ = driveRelease(ctx)
		return bestDate, nil, nil, err
	}
	cappedIDs := limitUnbatchedSelectionByDriveCells(rows, selectedIDs, bestDate, planner, session, cellsPerObligation)
	if len(cappedIDs) < len(selectedIDs) {
		session.releaseClaims(claimsOutsideSelection(shotClaims, cappedIDs))
		shotClaims = claimsOutsideSelection(shotClaims, differenceIDs(selectedIDs, cappedIDs))
		selectedIDs = cappedIDs
	}
	claimPreflightUnbatchedDriveCapacity(session, rows, selectedIDs, bestDate, cellsPerObligation)
	if err := driveRelease(ctx); err != nil {
		return bestDate, nil, nil, err
	}
	return bestDate, selectedIDs, shotClaims, nil
}

// preflightParkConsolidation replays consolidateParkDrivesWithVisitCounts' merge decision for one
// plan, write-free, onto the shared preflight session (RV-01). Obligations already claimed by the
// main-loop replay are excluded up front, matching the real flow where they are batched and removed
// before park consolidation lists them.
func (s *SweeperService) preflightParkConsolidation(ctx context.Context, tenantID string, plan SweepVersionPriority, asOf, dueBefore time.Time, session *SweepSession, claimed map[string]struct{}, parkClaimed map[string]struct{}, createdAtHWM time.Time, candidateIDs []string) error {
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

	groups := make(map[string][]domain.ParkConsolidationCandidate)
	addRows := func(rows []domain.ParkConsolidationCandidate) {
		for _, row := range rows {
			if _, done := claimed[row.ObligationID]; done {
				continue
			}
			key := row.ParkID + "|" + speciesGroupingKey(row.TargetSpecies, row.TargetAnimalStage, planner.SpeciesGroupingPolicy)
			groups[key] = append(groups[key], row)
		}
	}
	if candidateIDs != nil {
		for _, chunk := range snapshotIDChunks(candidateIDs, s.page) {
			rows, err := s.listUnbatchedShedDueForParkConsolidationBounded(ctx, tenantID, plan.VersionID, dueBefore, s.page, nil, createdAtHWM, chunk)
			if err != nil {
				return fmt.Errorf("obligation: preflight list park consolidation for version %s: %w", plan.VersionID, err)
			}
			addRows(rows)
		}
	} else {
		var after *domain.ParkConsolidationCursor
		seenCursors := map[string]struct{}{}
		for {
			// scale-guard:ignore: bounded keyset pagination (cursor advances by row identity, LIMIT s.page per query); mirrors consolidateParkDrivesWithVisitCounts' own park listing, not a per-row round trip.
			rows, err := s.listUnbatchedShedDueForParkConsolidationBounded(ctx, tenantID, plan.VersionID, dueBefore, s.page, after, createdAtHWM, nil)
			if err != nil {
				return fmt.Errorf("obligation: preflight list park consolidation for version %s: %w", plan.VersionID, err)
			}
			if len(rows) == 0 {
				break
			}
			addRows(rows)
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
	}

	now := biztime.BusinessDayStart(asOf)
	for _, rows := range groups {
		if len(rows) == 0 {
			continue
		}
		remaining := append([]domain.ParkConsolidationCandidate(nil), rows...)
		fullDates := make(map[string]struct{})
		for uniqueParkTargetCount(remaining) >= int(minMergeTargets) {
			next, selected, plannedDate, animalCapReached, stop, err := s.preflightParkMergeStep(ctx, tenantID, cfg, planner, now, dueBefore, session, remaining, minMergeTargets, fullDates)
			if err != nil {
				return err
			}
			recordClaimed(parkClaimed, selected)
			remaining = next
			if animalCapReached && plannedDate != nil {
				fullDates[plannedDate.Format("2006-01-02")] = struct{}{}
			}
			if stop {
				break
			}
		}
	}
	return nil
}

// preflightParkMergeStep mirrors parkMergeStep's date-resolution + shot-cap-selection decision for
// one merge attempt, write-free: same pickBestParkDriveDate, same seed, same
// selectParkIDsWithinVisitShotCapForSession claim/tie logic, same overflow walk -- but it never
// calls CreateBatchWithObligations. It returns the rows still un-merged and whether the caller's
// merge loop should stop.
func (s *SweeperService) preflightParkMergeStep(ctx context.Context, tenantID string, cfg SweepConfig, planner domain.DrivePlannerSettings, now, dueBefore time.Time, session *SweepSession, remaining []domain.ParkConsolidationCandidate, minMergeTargets int32, excludedDates map[string]struct{}) (newRemaining []domain.ParkConsolidationCandidate, selectedIDs []string, plannedDate *time.Time, animalCapReached bool, stop bool, err error) {
	// VAXCAP-005: mirror parkMergeStep's capacity-aware date scoring exactly so the write-free
	// preflight prediction matches the real sweep's date choice.
	plannedDate, err = s.selectBestParkDriveDateWithCapacity(ctx, tenantID, cfg, planner, now, remaining, minMergeTargets, excludedDates, session)
	if err != nil {
		return remaining, nil, nil, false, true, err
	}
	if plannedDate == nil {
		return remaining, nil, nil, false, true, nil
	}
	targetIDs := distinctParkTargetIDs(remaining)
	// R2-05(b): mirror parkMergeStep's real per-rule identity resolution (not the version-level
	// wrapper) so this write-free replay cannot disagree with the real merge decision it exists to
	// preview.
	orderedRemaining := orderParkCandidatesByVaccinePriority(remaining, cfg.getRuleVaccineIdentity)
	visitRelease, err := s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session)
	if err != nil {
		return remaining, nil, plannedDate, false, true, err
	}
	driveRelease, err := s.lockAndRefreshDriveCapacity(ctx, tenantID, firstParkID(remaining), plannedDate, planner.MaxGoatsPerDrive, session)
	if err != nil {
		_ = visitRelease(ctx)
		return remaining, nil, plannedDate, false, true, err
	}
	release := combineReleases(driveRelease, visitRelease)
	selected, shotClaims, err := selectParkIDsWithinVisitShotCapForSession(orderedRemaining, obligationsFeasibleOnDateForPlanner(*plannedDate, remaining, planner), plannedDate, planner, cfg.getRuleVaccineIdentity, session)
	if err != nil {
		_ = release(ctx)
		return remaining, nil, plannedDate, false, true, err
	}
	capped := limitParkSelectionByDriveCells(orderedRemaining, selected, *plannedDate, planner, session, cfg)
	if len(capped) < len(selected) {
		animalCapReached = true
		cappedClaims := splitShotCapReservations(shotClaims, selected, [][]string{capped})
		session.releaseClaims(claimsOutsideSelection(shotClaims, capped))
		if len(cappedClaims) > 0 {
			shotClaims = cappedClaims[0]
		} else {
			shotClaims = nil
		}
		selected = capped
	}
	if err := release(ctx); err != nil {
		return remaining, nil, plannedDate, false, true, err
	}
	selectedRows := filterRows(remaining, selected)
	if int32(uniqueParkTargetCount(selectedRows)) < minMergeTargets {
		session.releaseClaims(shotClaims)
		return remaining, nil, plannedDate, animalCapReached, !animalCapReached, nil
	}
	for _, row := range filterRows(remaining, selected) {
		session.claimDriveCapacity(row.ParkID, *plannedDate, row.ObligationID, parkCandidateDriveCells(cfg, row))
	}
	return removeRows(remaining, selected), selected, plannedDate, animalCapReached, false, nil
}

// preflightRemainingShedObligations mirrors batchRemainingShedObligationsWithVisitCounts, write-free,
// for shed rows not already claimed by the main-loop or park-consolidation replays.
func (s *SweeperService) preflightRemainingShedObligations(ctx context.Context, tenantID string, plan SweepVersionPriority, asOf, dueBefore time.Time, session *SweepSession, claimed map[string]struct{}, parkClaimed map[string]struct{}, createdAtHWM time.Time, candidateIDs []string) error {
	cfg := plan.Config
	if !cfg.ParkConsolidation.Enabled {
		return nil
	}
	planner := normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode)
	var rows []domain.UnbatchedDue
	addRows := func(in []domain.UnbatchedDue) {
		for _, row := range filterShedRows(in) {
			if _, done := claimed[row.ObligationID]; done {
				continue
			}
			if _, done := parkClaimed[row.ObligationID]; done {
				continue
			}
			rows = append(rows, row)
		}
	}
	if candidateIDs != nil {
		for _, chunk := range snapshotIDChunks(candidateIDs, s.page) {
			page, err := s.listUnbatchedDueForVersionBounded(ctx, tenantID, plan.VersionID, dueBefore, s.page, createdAtHWM, chunk)
			if err != nil {
				return fmt.Errorf("obligation: preflight list shed fallback for version %s: %w", plan.VersionID, err)
			}
			addRows(page)
		}
	} else {
		page, err := s.listUnbatchedDueForVersionBounded(ctx, tenantID, plan.VersionID, dueBefore, s.page, createdAtHWM, nil)
		if err != nil {
			return fmt.Errorf("obligation: preflight list shed fallback for version %s: %w", plan.VersionID, err)
		}
		addRows(page)
	}
	order, groups := groupUnbatchedDue(rows, planner.SpeciesGroupingPolicy)
	order = orderDueGroupsByVaccinePriority(order, groups, cfg)
	for _, k := range order {
		if _, err := s.preflightGroup(ctx, tenantID, cfg, planner, asOf, dueBefore, session, groups[k], claimed); err != nil {
			return err
		}
	}
	return nil
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

func claimPreflightUnbatchedDriveCapacity(session *SweepSession, rows []domain.UnbatchedDue, selected []string, plannedDate *time.Time, cellsPerObligation int32) {
	if session == nil || plannedDate == nil || len(selected) == 0 {
		return
	}
	if cellsPerObligation <= 0 {
		cellsPerObligation = 1
	}
	for _, row := range selectedUnbatchedRows(rows, selected) {
		session.claimDriveCapacity(row.ParkID, *plannedDate, row.ObligationID, cellsPerObligation)
	}
}
