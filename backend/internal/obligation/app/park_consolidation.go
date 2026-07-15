package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func (s *SweeperService) consolidateParkDrives(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time) (domain.SweepResult, error) {
	planner := normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode)
	return s.consolidateParkDrivesWithVisitCounts(ctx, tenantID, versionID, cfg, dueBefore, planner, NewSweepSession())
}

func (s *SweeperService) consolidateParkDrivesWithVisitCounts(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time, planner domain.DrivePlannerSettings, session *SweepSession) (domain.SweepResult, error) {
	var res domain.SweepResult
	settings := cfg.ParkConsolidation
	if !settings.Enabled {
		return res, nil
	}
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
		rows, err := s.repo.ListUnbatchedShedDueForParkConsolidation(ctx, tenantID, versionID, dueBefore, s.page, after)
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			key := row.ParkID + "|" + speciesGroupingKey(row.TargetSpecies, row.TargetAnimalStage, planner.SpeciesGroupingPolicy)
			groups[key] = append(groups[key], row)
		}
		if int32(len(rows)) < s.page {
			break
		}
		next := parkConsolidationCursor(rows[len(rows)-1])
		key := parkConsolidationCursorKey(next)
		if key == "" {
			return res, fmt.Errorf("obligation: park consolidation pagination did not produce an advance cursor")
		}
		if _, ok := seenCursors[key]; ok {
			return res, fmt.Errorf("obligation: park consolidation pagination did not advance after cursor %s", key)
		}
		seenCursors[key] = struct{}{}
		after = next
	}

	now := biztime.BusinessDayStart(dueBefore)
	for _, rows := range groups {
		if len(rows) == 0 {
			continue
		}
		parkID := rows[0].ParkID
		remaining := append([]domain.ParkConsolidationCandidate(nil), rows...)
		for len(remaining) >= int(minMergeTargets) && uniqueShedCount(remaining) >= int(minMergeSheds) {
			next, attached, stop, err := s.parkMergeStep(ctx, tenantID, versionID, cfg, planner, now, dueBefore, session, parkID, remaining, minMergeTargets, minMergeSheds)
			if err != nil {
				return res, err
			}
			remaining = next
			if attached > 0 {
				res.ParkBatches++
				res.ParkObligations += int(attached)
			}
			if stop {
				break
			}
		}
	}
	return res, nil
}

// parkMergeStep performs one park-consolidation merge attempt: picks the best shared drive date
// for remaining, shot-cap-selects the animals that fit (with an overflow-date retry identical to
// the shed-batching path), and creates one park batch from the result. It seeds session with the
// persisted, cross-pass shot count for every candidate target before selecting (VAX-REV-01), and --
// when the repo supports it -- holds a per-visit advisory lock across the select+create sequence
// so a concurrent sweeper worker cannot commit a conflicting claim for the same visit in between.
// stop is true when the caller's merge loop should not attempt another iteration for this park
// (nothing left to merge, or a hard cap/error condition), whether or not this call itself attached
// anything.
func (s *SweeperService) parkMergeStep(ctx context.Context, tenantID, versionID string, cfg SweepConfig, planner domain.DrivePlannerSettings, now, dueBefore time.Time, session *SweepSession, parkID string, remaining []domain.ParkConsolidationCandidate, minMergeTargets, minMergeSheds int32) (newRemaining []domain.ParkConsolidationCandidate, attached int64, stop bool, err error) {
	plannedDate, selected := pickBestParkDriveDate(now, remaining)
	targetIDs := distinctParkTargetIDs(remaining)
	release, err := s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session)
	if err != nil {
		return remaining, 0, true, err
	}
	selected, err = selectParkIDsWithinVisitShotCapForSession(remaining, selected, plannedDate, planner.MaxShotsPerAnimalPerDrive, cfg.VaccineCode, planner.VaccinePriority, session)
	if err != nil {
		_ = release(ctx)
		return remaining, 0, true, err
	}
	if len(selected) == 0 && plannedDate != nil && planner.MaxShotsPerAnimalPerDrive > 0 {
		if overflowDate := nextFeasibleParkDriveDateAfter(*plannedDate, remaining); overflowDate != nil {
			if relErr := release(ctx); relErr != nil {
				return remaining, 0, true, relErr
			}
			plannedDate = overflowDate
			selected = obligationsFeasibleOnDate(*plannedDate, remaining)
			release, err = s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session)
			if err != nil {
				return remaining, 0, true, err
			}
			selected, err = selectParkIDsWithinVisitShotCapForSession(remaining, selected, plannedDate, planner.MaxShotsPerAnimalPerDrive, cfg.VaccineCode, planner.VaccinePriority, session)
			if err != nil {
				_ = release(ctx)
				return remaining, 0, true, err
			}
		}
	}
	defer func() {
		if relErr := release(ctx); relErr != nil && err == nil {
			err = relErr
		}
	}()

	if plannedDate == nil || int32(len(selected)) < minMergeTargets || uniqueShedCount(filterRows(remaining, selected)) < int(minMergeSheds) {
		return remaining, 0, true, nil
	}
	selectedRows := filterRows(remaining, selected)
	windowStart, windowEnd := parkDriveWindow(selectedRows, selected)
	_, n, createErr := s.repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		ScopeType:         "park",
		ScopeID:           parkID,
		Session:           parkConsolidationSession(selected),
		PlannedDate:       plannedDate,
		WindowStart:       windowStart,
		WindowEnd:         windowEnd,
		Status:            "planned",
		EstimatedTargets:  int32(len(selected)),
		PlannedQuantity:   parkDrivePlannedQuantity(cfg, selectedRows),
		QuantityUnit:      "dose",
	}, selected)
	if createErr != nil {
		return remaining, 0, true, createErr
	}
	if n == 0 {
		return remaining, 0, true, nil
	}
	if holdErr := s.recordParkBatchingHoldIfNeeded(ctx, tenantID, selected, selectedRows, plannedDate, dueBefore); holdErr != nil {
		return remaining, 0, true, holdErr
	}
	return removeRows(remaining, selected), n, false, nil
}

func parkConsolidationCursorKey(cursor *domain.ParkConsolidationCursor) string {
	if cursor == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s",
		cursor.ParkID,
		cursor.RuleID,
		cursor.TargetSpecies,
		cursor.TargetAnimalStage,
		cursor.DueAt.UTC().Format(time.RFC3339Nano),
		cursor.ObligationID,
	)
}

func parkConsolidationCursor(row domain.ParkConsolidationCandidate) *domain.ParkConsolidationCursor {
	return &domain.ParkConsolidationCursor{
		ParkID:            row.ParkID,
		RuleID:            row.RuleID,
		TargetSpecies:     row.TargetSpecies,
		TargetAnimalStage: row.TargetAnimalStage,
		DueAt:             row.DueAt,
		ObligationID:      row.ObligationID,
	}
}

func (s *SweeperService) recordParkBatchingHoldIfNeeded(ctx context.Context, tenantID string, ids []string, rows []domain.ParkConsolidationCandidate, plannedDate *time.Time, occurredAt time.Time) error {
	if plannedDate == nil || len(ids) == 0 || !parkDriveDateUsesBatchingHold(*plannedDate, rows) {
		return nil
	}
	recorder, ok := s.repo.(batchingHoldRecorder)
	if !ok {
		return nil
	}
	_, err := recorder.RecordBatchingHoldForObligations(ctx, tenantID, ids, *plannedDate, occurredAt)
	return err
}

func parkDriveDateUsesBatchingHold(planned time.Time, rows []domain.ParkConsolidationCandidate) bool {
	planned = biztime.BusinessDayStart(planned)
	for _, row := range rows {
		if planned.After(biztime.BusinessDayStart(row.DueAt)) {
			return true
		}
	}
	return false
}

func parkConsolidationSession(selected []string) string {
	if len(selected) == 0 {
		return "park-consolidation"
	}
	return "park-consolidation:" + selected[0]
}

func parkDrivePlannedQuantity(cfg SweepConfig, rows []domain.ParkConsolidationCandidate) string {
	total := int64(0)
	for _, row := range rows {
		total += int64(normalizedDosesPerGoat(cfg.forRule(row.RuleID).DosesPerGoat))
	}
	return strconv.FormatInt(total, 10)
}

func filterRows(rows []domain.ParkConsolidationCandidate, selected []string) []domain.ParkConsolidationCandidate {
	if len(selected) == 0 {
		return nil
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	out := make([]domain.ParkConsolidationCandidate, 0, len(selected))
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; ok {
			out = append(out, row)
		}
	}
	return out
}

func removeRows(rows []domain.ParkConsolidationCandidate, selected []string) []domain.ParkConsolidationCandidate {
	if len(selected) == 0 {
		return rows
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	out := make([]domain.ParkConsolidationCandidate, 0, len(rows))
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			out = append(out, row)
		}
	}
	return out
}

func uniqueShedCount(rows []domain.ParkConsolidationCandidate) int {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.ShedID == "" {
			continue
		}
		seen[row.ShedID] = struct{}{}
	}
	return len(seen)
}

func pickBestParkDriveDate(now time.Time, rows []domain.ParkConsolidationCandidate) (*time.Time, []string) {
	candidates := parkDriveCandidateDates(now, rows)
	nowDay := biztime.BusinessDayStart(now)
	bestCount := 0
	var bestDate *time.Time
	var bestIDs []string
	for _, candidate := range candidates {
		if candidate.Before(nowDay) {
			continue
		}
		ids := obligationsFeasibleOnDate(candidate, rows)
		if len(ids) > bestCount {
			bestCount = len(ids)
			day := candidate
			bestDate = &day
			bestIDs = ids
			continue
		}
		if len(ids) == bestCount && len(ids) > 0 && bestDate != nil && candidate.Before(*bestDate) {
			day := candidate
			bestDate = &day
			bestIDs = ids
		}
	}
	return bestDate, bestIDs
}

func parkDriveCandidateDates(now time.Time, rows []domain.ParkConsolidationCandidate) []time.Time {
	seen := make(map[string]time.Time)
	add := func(t time.Time) {
		if t.IsZero() {
			return
		}
		day := biztime.BusinessDayStart(t)
		seen[day.Format("2006-01-02")] = day
	}
	add(now)
	for _, row := range rows {
		add(row.DueAt)
		if row.WindowStart != nil {
			add(*row.WindowStart)
		}
		if row.WindowEnd != nil {
			add(*row.WindowEnd)
		}
	}
	out := make([]time.Time, 0, len(seen))
	for _, day := range seen {
		out = append(out, day)
	}
	return out
}

func obligationsFeasibleOnDate(day time.Time, rows []domain.ParkConsolidationCandidate) []string {
	day = biztime.BusinessDayStart(day)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		earliest := obligationEarliestDate(row)
		latest := obligationLatestDate(row)
		if day.Before(earliest) || day.After(latest) {
			continue
		}
		ids = append(ids, row.ObligationID)
	}
	return ids
}

func obligationEarliestDate(row domain.ParkConsolidationCandidate) time.Time {
	if row.WindowStart != nil && !row.WindowStart.IsZero() {
		return biztime.BusinessDayStart(*row.WindowStart)
	}
	return biztime.BusinessDayStart(row.DueAt)
}

func obligationLatestDate(row domain.ParkConsolidationCandidate) time.Time {
	if row.WindowEnd != nil && !row.WindowEnd.IsZero() {
		return biztime.BusinessDayStart(*row.WindowEnd)
	}
	return biztime.BusinessDayStart(row.DueAt)
}

func parkDriveWindow(rows []domain.ParkConsolidationCandidate, selected []string) (*time.Time, *time.Time) {
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	var windowStart *time.Time
	var windowEnd *time.Time
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			continue
		}
		start := obligationEarliestDate(row)
		end := obligationLatestDate(row)
		if windowStart == nil || start.Before(*windowStart) {
			s := start
			windowStart = &s
		}
		if windowEnd == nil || end.After(*windowEnd) {
			e := end
			windowEnd = &e
		}
	}
	return windowStart, windowEnd
}
