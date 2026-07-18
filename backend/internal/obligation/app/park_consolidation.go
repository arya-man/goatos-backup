package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func (s *SweeperService) consolidateParkDrives(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time) (domain.SweepResult, error) {
	planner := normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode)
	return s.consolidateParkDrivesWithVisitCounts(ctx, tenantID, versionID, cfg, time.Time{}, dueBefore, planner, NewSweepSession(), time.Time{}, nil)
}

func (s *SweeperService) consolidateParkDrivesWithVisitCounts(ctx context.Context, tenantID, versionID string, cfg SweepConfig, asOf, dueBefore time.Time, planner domain.DrivePlannerSettings, session *SweepSession, createdAtHWM time.Time, candidateIDs []string) (domain.SweepResult, error) {
	var res domain.SweepResult
	settings := cfg.ParkConsolidation
	if !settings.Enabled {
		return res, nil
	}
	minMergeTargets := settings.MinParkMergeTargets
	if minMergeTargets < 1 {
		minMergeTargets = domain.DefaultParkConsolidationSettings().MinParkMergeTargets
	}

	groups := make(map[string][]domain.ParkConsolidationCandidate)
	if candidateIDs != nil {
		for _, chunk := range snapshotIDChunks(candidateIDs, s.page) {
			rows, err := s.listUnbatchedShedDueForParkConsolidationBounded(ctx, tenantID, versionID, dueBefore, s.page, nil, createdAtHWM, chunk)
			if err != nil {
				return res, err
			}
			for _, row := range rows {
				key := row.ParkID + "|" + speciesGroupingKey(row.TargetSpecies, row.TargetAnimalStage, planner.SpeciesGroupingPolicy)
				groups[key] = append(groups[key], row)
			}
		}
	} else {
		var after *domain.ParkConsolidationCursor
		seenCursors := map[string]struct{}{}
		for {
			rows, err := s.listUnbatchedShedDueForParkConsolidationBounded(ctx, tenantID, versionID, dueBefore, s.page, after, createdAtHWM, nil)
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
	}

	now := biztime.BusinessDayStart(asOf)
	for _, rows := range groups {
		if len(rows) == 0 {
			continue
		}
		parkID := rows[0].ParkID
		remaining := append([]domain.ParkConsolidationCandidate(nil), rows...)
		fullDates := make(map[string]struct{})
		for uniqueParkTargetCount(remaining) >= int(minMergeTargets) {
			next, attached, plannedDate, animalCapReached, stop, err := s.parkMergeStep(ctx, tenantID, versionID, cfg, planner, now, asOf, session, parkID, remaining, minMergeTargets, fullDates)
			if err != nil {
				return res, err
			}
			remaining = next
			if attached > 0 {
				res.ParkBatches++
				res.ParkObligations += int(attached)
				if animalCapReached && plannedDate != nil {
					fullDates[plannedDate.Format("2006-01-02")] = struct{}{}
				}
			}
			if stop {
				break
			}
		}
	}
	return res, nil
}

// parkMergeStep performs one park-consolidation merge attempt: picks the best shared drive date
// for remaining, shot-cap-selects the animals that fit (walking later feasible overflow dates
// exactly like the shed-batching path), and creates one park batch from the result. It seeds session with the
// persisted, cross-pass shot count for every candidate target before selecting (VAX-REV-01), and --
// when the repo supports it -- holds a per-visit advisory lock across the select+create sequence
// so a concurrent sweeper worker cannot commit a conflicting claim for the same visit in between.
// stop is true when the caller's merge loop should not attempt another iteration for this park
// (nothing left to merge, or a hard cap/error condition), whether or not this call itself attached
// anything.
func (s *SweeperService) parkMergeStep(ctx context.Context, tenantID, versionID string, cfg SweepConfig, planner domain.DrivePlannerSettings, now, asOf time.Time, session *SweepSession, parkID string, remaining []domain.ParkConsolidationCandidate, minMergeTargets int32, excludedDates map[string]struct{}) (newRemaining []domain.ParkConsolidationCandidate, attached int64, plannedDate *time.Time, animalCapReached bool, stop bool, err error) {
	plannedDate, selected := pickBestParkDriveDateExcluding(now, remaining, minMergeTargets, excludedDates)
	if plannedDate == nil {
		return remaining, 0, nil, false, true, nil
	}
	targetIDs := distinctParkTargetIDs(remaining)
	// R2-05(b): resolve each candidate's OWN rule to its real vaccine identity instead of the single
	// version-level wrapper (cfg.VaccineCode/planner.VaccinePriority) -- a park merge routinely mixes
	// several distinct matrix vaccines in one candidate set.
	orderedRemaining := orderParkCandidatesByVaccinePriority(remaining, cfg.getRuleVaccineIdentity)
	var release func(context.Context) error
	var shotClaims []shotCapReservation
	for {
		release, err = s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session)
		if err != nil {
			return remaining, 0, plannedDate, false, true, err
		}
		selected, shotClaims, err = selectParkIDsWithinVisitShotCapForSession(orderedRemaining, selected, plannedDate, planner.MaxShotsPerAnimalPerDrive, cfg.getRuleVaccineIdentity, session)
		if err != nil {
			_ = release(ctx)
			return remaining, 0, plannedDate, false, true, err
		}
		if len(selected) > 0 || plannedDate == nil || planner.MaxShotsPerAnimalPerDrive <= 0 {
			break
		}
		overflowDate := nextFeasibleParkDriveDateAfter(*plannedDate, remaining)
		if overflowDate == nil {
			if relErr := release(ctx); relErr != nil {
				return remaining, 0, plannedDate, false, true, relErr
			}
			return remaining, 0, plannedDate, false, true, nil
		}
		if relErr := release(ctx); relErr != nil {
			return remaining, 0, plannedDate, false, true, relErr
		}
		plannedDate = overflowDate
		selected = obligationsFeasibleOnDate(*plannedDate, remaining)
		orderedRemaining = orderParkCandidatesByVaccinePriority(remaining, cfg.getRuleVaccineIdentity)
	}
	defer func() {
		if relErr := release(ctx); relErr != nil && err == nil {
			err = relErr
		}
	}()

	selectedRows := filterRows(remaining, selected)
	if plannedDate == nil || int32(uniqueParkTargetCount(selectedRows)) < minMergeTargets {
		return remaining, 0, plannedDate, false, true, nil
	}
	if planner.MaxGoatsPerDrive > 0 {
		capped := limitParkSelectionByDistinctTargets(orderedRemaining, selected, *plannedDate, planner.MaxGoatsPerDrive)
		cappedRows := filterRows(remaining, capped)
		if len(capped) < len(selected) && uniqueParkTargetCount(cappedRows) > 0 {
			animalCapReached = true
			cappedClaims := splitShotCapReservations(shotClaims, selected, [][]string{capped})
			session.releaseClaims(claimsOutsideSelection(shotClaims, capped))
			if len(cappedClaims) > 0 {
				shotClaims = cappedClaims[0]
			}
			selected = capped
			selectedRows = cappedRows
		}
	}
	if int32(uniqueParkTargetCount(selectedRows)) < minMergeTargets {
		session.releaseClaims(shotClaims)
		return remaining, 0, plannedDate, false, true, nil
	}
	windowStart, windowEnd := parkDriveWindow(selectedRows, selected)
	var batchingHoldUntil *time.Time
	if parkDriveDateUsesBatchingHold(*plannedDate, selectedRows) {
		holdUntil := *plannedDate
		batchingHoldUntil = &holdUntil
	}
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
		EstimatedTargets:  int32(uniqueParkTargetCount(selectedRows)),
		PlannedQuantity:   parkDrivePlannedQuantity(cfg, selectedRows),
		QuantityUnit:      "dose",
		BatchingHoldUntil: batchingHoldUntil,
	}, selected)
	if createErr != nil {
		session.releaseClaims(shotClaims)
		return remaining, 0, plannedDate, animalCapReached, true, createErr
	}
	if n == 0 {
		session.releaseClaims(shotClaims)
		return remaining, 0, plannedDate, animalCapReached, true, nil
	}
	if int(n) < len(selected) {
		session.releaseClaims(shotClaims)
	}
	return removeRows(remaining, selected), n, plannedDate, animalCapReached, false, nil
}

func parkConsolidationCursorKey(cursor *domain.ParkConsolidationCursor) string {
	if cursor == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s",
		cursor.ParkID,
		cursor.TargetSpecies,
		cursor.TargetAnimalStage,
		cursor.DueAt.UTC().Format(time.RFC3339Nano),
		cursor.RuleID,
		cursor.ObligationID,
	)
}

func parkConsolidationCursor(row domain.ParkConsolidationCandidate) *domain.ParkConsolidationCursor {
	return &domain.ParkConsolidationCursor{
		ParkID:            row.ParkID,
		TargetSpecies:     row.TargetSpecies,
		TargetAnimalStage: row.TargetAnimalStage,
		DueAt:             row.DueAt,
		RuleID:            row.RuleID,
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

func claimsOutsideSelection(claims []shotCapReservation, selected []string) []shotCapReservation {
	if len(claims) == 0 {
		return nil
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	out := make([]shotCapReservation, 0, len(claims))
	for _, claim := range claims {
		if _, ok := selectedSet[claim.obligationID]; !ok {
			out = append(out, claim)
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

func uniqueParkTargetCount(rows []domain.ParkConsolidationCandidate) int {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.TargetID == "" {
			continue
		}
		seen[row.TargetID] = struct{}{}
	}
	return len(seen)
}

func pickBestParkDriveDate(now time.Time, rows []domain.ParkConsolidationCandidate, minMergeTargets int32) (*time.Time, []string) {
	return pickBestParkDriveDateExcluding(now, rows, minMergeTargets, nil)
}

func pickBestParkDriveDateExcluding(now time.Time, rows []domain.ParkConsolidationCandidate, minMergeTargets int32, excludedDates map[string]struct{}) (*time.Time, []string) {
	candidates := parkDriveCandidateDates(now, rows)
	nowDay := biztime.BusinessDayStart(now)
	bestTargets := 0
	bestObligations := 0
	var bestDate *time.Time
	var bestIDs []string
	for _, candidate := range candidates {
		if candidate.Before(nowDay) {
			continue
		}
		if _, excluded := excludedDates[candidate.Format("2006-01-02")]; excluded {
			continue
		}
		ids := obligationsFeasibleOnDate(candidate, rows)
		selectedRows := filterRows(rows, ids)
		targets := uniqueParkTargetCount(selectedRows)
		if int32(targets) < minMergeTargets {
			continue
		}
		obligations := len(ids)
		if targets > bestTargets || (targets == bestTargets && obligations > bestObligations) {
			bestTargets = targets
			bestObligations = obligations
			day := candidate
			bestDate = &day
			bestIDs = ids
			continue
		}
		if targets == bestTargets && obligations == bestObligations && obligations > 0 && bestDate != nil && candidate.Before(*bestDate) {
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
		start := obligationEarliestDate(row)
		if row.WindowEnd != nil && !row.WindowEnd.IsZero() {
			for day, end, offset := start, obligationLatestDate(row), 0; !day.After(end) && offset < 14; day, offset = day.AddDate(0, 0, 1), offset+1 {
				add(day)
			}
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
		if !parkObligationFeasibleOnDate(day, row) {
			continue
		}
		ids = append(ids, row.ObligationID)
	}
	return ids
}

func limitParkSelectionByDistinctTargets(rows []domain.ParkConsolidationCandidate, selected []string, plannedDate time.Time, maxTargets int32) []string {
	if maxTargets <= 0 || len(selected) == 0 {
		return selected
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	rowsByTarget := make(map[string][]domain.ParkConsolidationCandidate)
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			continue
		}
		targetID := parkTargetCapKey(row)
		rowsByTarget[targetID] = append(rowsByTarget[targetID], row)
	}
	targets := make(map[string]struct{})
	out := make([]string, 0, len(selected))
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			continue
		}
		targetID := parkTargetCapKey(row)
		if _, ok := targets[targetID]; !ok {
			if int32(len(targets)) >= maxTargets && parkTargetCanMoveAfter(plannedDate, rowsByTarget[targetID]) {
				continue
			}
			targets[targetID] = struct{}{}
		}
		out = append(out, row.ObligationID)
	}
	return out
}

func parkTargetCapKey(row domain.ParkConsolidationCandidate) string {
	targetID := strings.TrimSpace(row.TargetID)
	if targetID == "" {
		return row.ObligationID
	}
	return targetID
}

func parkTargetCanMoveAfter(plannedDate time.Time, rows []domain.ParkConsolidationCandidate) bool {
	if len(rows) == 0 {
		return false
	}
	next := businessDate(plannedDate).AddDate(0, 0, 1)
	for offset := 0; offset < 14; offset++ {
		ok := true
		for _, row := range rows {
			if !parkObligationFeasibleOnDate(next, row) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
		next = next.AddDate(0, 0, 1)
	}
	return false
}

func parkObligationFeasibleOnDate(day time.Time, row domain.ParkConsolidationCandidate) bool {
	if row.DueAt.IsZero() &&
		(row.WindowStart == nil || row.WindowStart.IsZero()) &&
		(row.WindowEnd == nil || row.WindowEnd.IsZero()) {
		return true
	}
	day = biztime.BusinessDayStart(day)
	earliest := obligationEarliestDate(row)
	latest := obligationLatestDate(row)
	return !day.Before(earliest) && !day.After(latest)
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
	return time.Date(9999, 12, 31, 0, 0, 0, 0, biztime.DefaultLocation())
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
		if windowStart == nil || start.After(*windowStart) {
			s := start
			windowStart = &s
		}
		if row.WindowEnd != nil && !row.WindowEnd.IsZero() && (windowEnd == nil || end.Before(*windowEnd)) {
			e := end
			windowEnd = &e
		}
	}
	return windowStart, windowEnd
}
