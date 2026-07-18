package app

import (
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// approvedDriveCombos are PDF/source-approved same-day vaccine bundles. Members share a combo session
// key so cross-version sweeps can align planned dates on one shed visit. See drive_planner_config.go.

type driveCandidate struct {
	ObligationID             string
	TargetID                 string
	TargetReproductiveStatus string
	DueAt                    time.Time
	WindowStart              *time.Time
	WindowEnd                *time.Time
	BatchingHoldCount        int32
	FirstBatchingHoldUntil   *time.Time
}

func driveCandidatesFromUnbatched(rows []domain.UnbatchedDue) []driveCandidate {
	out := make([]driveCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, driveCandidate{
			ObligationID:             row.ObligationID,
			TargetID:                 row.TargetID,
			TargetReproductiveStatus: row.TargetReproductiveStatus,
			DueAt:                    row.DueAt,
			WindowStart:              row.WindowStart,
			WindowEnd:                row.WindowEnd,
			BatchingHoldCount:        row.BatchingHoldCount,
			FirstBatchingHoldUntil:   row.FirstBatchingHoldUntil,
		})
	}
	return out
}

func normalizedDrivePlannerSettings(cfg domain.DrivePlannerSettings, vaccineCode string) domain.DrivePlannerSettings {
	return resolvedDrivePlannerSettings(cfg, vaccineCode)
}

func comboSessionKey(vaccineCode string) string {
	return domain.VaccineComboSession(vaccineCode)
}

func batchSession(ruleID, vaccineCode string) string {
	return domain.BatchSession(ruleID, vaccineCode)
}

func sweepWindowGroupKey(r domain.UnbatchedDue, speciesPolicy string) string {
	return r.ScopeType + "|" + r.ScopeID + "|" + r.RuleID + "|" + speciesGroupingKey(r.TargetSpecies, r.TargetAnimalStage, speciesPolicy)
}

func speciesGroupingKey(species, stage, policy string) string {
	if strings.EqualFold(strings.TrimSpace(policy), "species_specific") {
		return "species:" + normalizedSpeciesCode(species)
	}
	if isKidDriveStage(stage) {
		return "kid_mixed"
	}
	return "species:" + normalizedSpeciesCode(species)
}

func normalizedSpeciesCode(species string) string {
	species = strings.ToLower(strings.TrimSpace(species))
	if species == "" {
		return "goat"
	}
	return species
}

func isKidDriveStage(stage string) bool {
	stage = strings.ToUpper(strings.TrimSpace(stage))
	if stage == "" {
		return false
	}
	if strings.HasPrefix(stage, "K") {
		return true
	}
	return strings.Contains(stage, "KID")
}

func dueDateKey(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return businessDate(t).Format("2006-01-02")
}

func pickBestDriveDate(now time.Time, rows []driveCandidate, priority int32) *time.Time {
	return pickBestDriveDateUntil(now, rows, priority, nil)
}

func pickBestDriveDateUntil(now time.Time, rows []driveCandidate, priority int32, latestAllowed *time.Time) *time.Time {
	if len(rows) == 0 {
		return nil
	}
	candidates := candidateDriveDates(now, rows)
	nowDay := businessDate(now)
	var latestDay *time.Time
	if latestAllowed != nil && !latestAllowed.IsZero() {
		day := businessDate(*latestAllowed)
		latestDay = &day
	}
	bestCount := 0
	bestScore := -1
	var bestDate *time.Time
	for _, candidate := range candidates {
		if candidate.Before(nowDay) {
			continue
		}
		if latestDay != nil && candidate.After(*latestDay) {
			continue
		}
		ids := obligationsFeasibleOnDriveDate(candidate, rows)
		if len(ids) == 0 {
			continue
		}
		score := scoreDriveDate(candidate, rows, ids, now, priority)
		if len(ids) > bestCount ||
			(len(ids) == bestCount && (score > bestScore || (score == bestScore && bestDate != nil && candidate.Before(*bestDate)))) {
			day := candidate
			bestDate = &day
			bestCount = len(ids)
			bestScore = score
		}
	}
	if bestDate != nil {
		return bestDate
	}
	if fallback := pickEarliestFeasibleDriveDateAtOrAfter(now, rows); fallback != nil {
		return fallback
	}
	return nil
}

func pickEarliestFeasibleDriveDateAtOrAfter(now time.Time, rows []driveCandidate) *time.Time {
	candidates := candidateDriveDates(now, rows)
	nowDay := businessDate(now)
	var bestDate *time.Time
	for _, candidate := range candidates {
		if candidate.Before(nowDay) {
			continue
		}
		if len(obligationsFeasibleOnDriveDate(candidate, rows)) == 0 {
			continue
		}
		if bestDate == nil || candidate.Before(*bestDate) {
			day := candidate
			bestDate = &day
		}
	}
	return bestDate
}

func pickBestDriveDateWithHold(now time.Time, rows []driveCandidate, planner domain.DrivePlannerSettings) *time.Time {
	if len(rows) == 0 {
		return nil
	}
	earliest := earliestDriveDueDate(rows)
	if alreadyHeld(rows, planner.MaxBatchingHoldCount) {
		if earliest.Before(businessDate(now)) {
			return pickEarliestFeasibleDriveDateAtOrAfter(now, rows)
		}
		return &earliest
	}
	if planner.MaxBatchingHoldDays <= 0 {
		return pickBestDriveDate(now, rows, planner.VaccinePriority)
	}
	holdUntil := earliest.AddDate(0, 0, int(planner.MaxBatchingHoldDays))
	if holdUntil.Before(businessDate(now)) {
		return pickEarliestFeasibleDriveDateAtOrAfter(now, rows)
	}
	return pickBestDriveDateUntil(now, rows, planner.VaccinePriority, &holdUntil)
}

func driveDateUsesBatchingHold(planned time.Time, rows []driveCandidate) bool {
	planned = businessDate(planned)
	for _, row := range rows {
		if planned.After(businessDate(row.DueAt)) {
			return true
		}
	}
	return false
}

func alreadyHeld(rows []driveCandidate, maxHoldCount int32) bool {
	if maxHoldCount <= 0 {
		return false
	}
	for _, row := range rows {
		if row.BatchingHoldCount >= maxHoldCount {
			return true
		}
	}
	return false
}

func earliestDriveDueDate(rows []driveCandidate) time.Time {
	earliest := businessDate(rows[0].DueAt)
	for _, row := range rows[1:] {
		day := businessDate(row.DueAt)
		if day.Before(earliest) {
			earliest = day
		}
	}
	return earliest
}

func scoreDriveDate(_ time.Time, rows []driveCandidate, feasibleIDs []string, now time.Time, vaccinePriority int32) int {
	feasible := make(map[string]struct{}, len(feasibleIDs))
	for _, id := range feasibleIDs {
		feasible[id] = struct{}{}
	}
	score := len(feasibleIDs) * 100
	if vaccinePriority > 0 {
		score += int(100-vaccinePriority) * 2
	}
	for _, row := range rows {
		if _, ok := feasible[row.ObligationID]; !ok {
			continue
		}
		overdue := int(businessDate(now).Sub(businessDate(row.DueAt)).Hours() / 24)
		if overdue > 0 {
			score += 10 + overdue*5
		}
		if breedingReadyForDrivePriority(row.TargetReproductiveStatus) {
			score += 15
		}
	}
	return score
}

func breedingReadyForDrivePriority(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "breeding_ready", "breeding-ready", "flushing", "ready_for_breeding":
		return true
	default:
		return false
	}
}

func candidateDriveDates(now time.Time, rows []driveCandidate) []time.Time {
	seen := make(map[string]time.Time)
	add := func(t time.Time) {
		if t.IsZero() {
			return
		}
		day := businessDate(t)
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

func obligationsFeasibleOnDriveDate(day time.Time, rows []driveCandidate) []string {
	day = businessDate(day)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if !driveCandidateFeasibleOnDate(day, row) {
			continue
		}
		ids = append(ids, row.ObligationID)
	}
	return ids
}

func driveCandidateFeasibleOnDate(day time.Time, row driveCandidate) bool {
	if row.DueAt.IsZero() &&
		(row.WindowStart == nil || row.WindowStart.IsZero()) &&
		(row.WindowEnd == nil || row.WindowEnd.IsZero()) {
		return true
	}
	day = businessDate(day)
	earliest := driveEarliestDate(row)
	latest := driveLatestDate(row)
	return !day.Before(earliest) && !day.After(latest)
}

func driveEarliestDate(row driveCandidate) time.Time {
	if row.WindowStart != nil && !row.WindowStart.IsZero() {
		return businessDate(*row.WindowStart)
	}
	return businessDate(row.DueAt)
}

func driveLatestDate(row driveCandidate) time.Time {
	if row.WindowEnd != nil && !row.WindowEnd.IsZero() {
		return businessDate(*row.WindowEnd)
	}
	if !row.DueAt.IsZero() {
		return businessDate(row.DueAt)
	}
	return driveEarliestDate(row)
}

func splitObligationIDs(ids []string, max int32) [][]string {
	if max <= 0 || int32(len(ids)) <= max {
		return [][]string{ids}
	}
	chunks := make([][]string, 0, (len(ids)+int(max)-1)/int(max))
	for start := 0; start < len(ids); start += int(max) {
		end := start + int(max)
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[start:end])
	}
	return chunks
}

func selectIDsWithinVisitShotCap(rows []domain.UnbatchedDue, plannedDate *time.Time, maxShots int32, visitCounts map[string]int32) []string {
	selected := make([]string, 0, len(rows))
	for _, row := range rows {
		if plannedDate != nil && !driveCandidateFeasibleOnDate(*plannedDate, driveCandidate{
			ObligationID:             row.ObligationID,
			TargetID:                 row.TargetID,
			TargetReproductiveStatus: row.TargetReproductiveStatus,
			DueAt:                    row.DueAt,
			WindowStart:              row.WindowStart,
			WindowEnd:                row.WindowEnd,
			BatchingHoldCount:        row.BatchingHoldCount,
			FirstBatchingHoldUntil:   row.FirstBatchingHoldUntil,
		}) {
			continue
		}
		if maxShots <= 0 || plannedDate == nil {
			selected = append(selected, row.ObligationID)
			continue
		}
		if strings.TrimSpace(row.TargetID) == "" {
			selected = append(selected, row.ObligationID)
			continue
		}
		key := visitShotCountKey(*plannedDate, row.TargetID)
		if visitCounts[key] >= maxShots {
			continue
		}
		visitCounts[key]++
		selected = append(selected, row.ObligationID)
	}
	return selected
}

func nextFeasibleUnbatchedDriveDateAfter(plannedDate time.Time, rows []domain.UnbatchedDue) *time.Time {
	next := businessDate(plannedDate).AddDate(0, 0, 1)
	candidates := driveCandidatesFromUnbatched(rows)
	latest := latestUnbatchedDriveDate(candidates)
	for !latest.IsZero() && !next.After(latest) {
		if len(obligationsFeasibleOnDriveDate(next, candidates)) > 0 {
			day := next
			return &day
		}
		next = next.AddDate(0, 0, 1)
	}
	return nil
}

func latestUnbatchedDriveDate(candidates []driveCandidate) time.Time {
	var latest time.Time
	for _, row := range candidates {
		rowLatest := driveLatestDate(row)
		if latest.IsZero() || rowLatest.After(latest) {
			latest = rowLatest
		}
	}
	return latest
}

func selectParkIDsWithinVisitShotCap(rows []domain.ParkConsolidationCandidate, selected []string, plannedDate *time.Time, maxShots int32, visitCounts map[string]int32) []string {
	if maxShots <= 0 || plannedDate == nil || len(selected) == 0 {
		return selected
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	out := make([]string, 0, len(selected))
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			continue
		}
		if strings.TrimSpace(row.TargetID) == "" {
			out = append(out, row.ObligationID)
			continue
		}
		key := visitShotCountKey(*plannedDate, row.TargetID)
		if visitCounts[key] >= maxShots {
			continue
		}
		visitCounts[key]++
		out = append(out, row.ObligationID)
	}
	return out
}

func nextFeasibleParkDriveDateAfter(plannedDate time.Time, rows []domain.ParkConsolidationCandidate) *time.Time {
	next := businessDate(plannedDate).AddDate(0, 0, 1)
	latest := latestParkDriveDate(rows)
	for !latest.IsZero() && !next.After(latest) {
		if len(obligationsFeasibleOnDate(next, rows)) > 0 {
			day := next
			return &day
		}
		next = next.AddDate(0, 0, 1)
	}
	return nil
}

func latestParkDriveDate(rows []domain.ParkConsolidationCandidate) time.Time {
	var latest time.Time
	for _, row := range rows {
		rowLatest := obligationLatestDate(row)
		if latest.IsZero() || rowLatest.After(latest) {
			latest = rowLatest
		}
	}
	return latest
}

func visitShotCountKey(plannedDate time.Time, targetID string) string {
	return businessDate(plannedDate).Format("2006-01-02") + "|" + targetID
}

func businessDate(t time.Time) time.Time {
	return biztime.BusinessDayStart(t)
}
