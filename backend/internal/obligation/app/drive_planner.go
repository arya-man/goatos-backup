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
	ObligationID           string
	DueAt                  time.Time
	WindowStart            *time.Time
	WindowEnd              *time.Time
	BatchingHoldCount      int32
	FirstBatchingHoldUntil *time.Time
	BreedingReadyPriority  bool
}

func driveCandidatesFromUnbatched(rows []domain.UnbatchedDue) []driveCandidate {
	out := make([]driveCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, driveCandidate{
			ObligationID:           row.ObligationID,
			DueAt:                  row.DueAt,
			WindowStart:            row.WindowStart,
			WindowEnd:              row.WindowEnd,
			BatchingHoldCount:      row.BatchingHoldCount,
			FirstBatchingHoldUntil: row.FirstBatchingHoldUntil,
			BreedingReadyPriority:  strings.Contains(strings.ToUpper(strings.TrimSpace(row.TargetAnimalStage)), "FLUSHING"),
		})
	}
	return out
}

func normalizedDrivePlannerSettings(cfg domain.DrivePlannerSettings, vaccineCode string) domain.DrivePlannerSettings {
	return resolvedDrivePlannerSettings(cfg, vaccineCode)
}

func comboSessionKey(vaccineCode string) string {
	code := strings.ToLower(strings.TrimSpace(vaccineCode))
	if code == "" {
		return ""
	}
	return vaccineComboSession[code]
}

func batchSession(ruleID, vaccineCode string) string {
	if session := comboSessionKey(vaccineCode); session != "" {
		return session
	}
	if ruleID == "" {
		return ""
	}
	return "rule:" + ruleID
}

func speciesGroupingKey(stage, species, policy string) string {
	if strings.EqualFold(strings.TrimSpace(policy), "species_specific") {
		return "species:" + normalizeSpeciesCode(species)
	}
	stageKey := strings.ToUpper(strings.TrimSpace(stage))
	switch stageKey {
	case "K0", "K1", "K2", "KID", "KIDS":
		return "kid_mixed"
	default:
		return "species:" + normalizeSpeciesCode(species)
	}
}

func normalizeSpeciesCode(species string) string {
	species = strings.ToLower(strings.TrimSpace(species))
	if species == "" {
		return "goat"
	}
	return species
}

func sweepWindowGroupKey(r domain.UnbatchedDue, speciesPolicy string) string {
	groupSpecies := speciesGroupingKey(r.TargetAnimalStage, r.TargetSpecies, speciesPolicy)
	return r.ScopeType + "|" + r.ScopeID + "|" + r.RuleID + "|" + groupSpecies + "|" + dueDateKey(r.DueAt) + "|" + timeKey(r.WindowStart) + "|" + timeKey(r.WindowEnd)
}

func dueDateKey(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return businessDate(t).Format("2006-01-02")
}

func pickBestDriveDate(now time.Time, rows []driveCandidate, priority int32) *time.Time {
	picked, _, _ := pickBestDriveDateRespectingHold(now, rows, priority, domain.DefaultDrivePlannerSettings())
	return picked
}

func pickBestDriveDateRespectingHold(now time.Time, rows []driveCandidate, priority int32, planner domain.DrivePlannerSettings) (*time.Time, []string, bool) {
	if len(rows) == 0 {
		return nil, nil, false
	}
	maxHoldDays := planner.MaxBatchingHoldDays
	if maxHoldDays <= 0 {
		maxHoldDays = domain.DefaultDrivePlannerSettings().MaxBatchingHoldDays
	}
	maxHoldCount := planner.MaxBatchingHoldCount
	if maxHoldCount <= 0 {
		maxHoldCount = domain.DefaultDrivePlannerSettings().MaxBatchingHoldCount
	}
	for _, row := range rows {
		if row.BatchingHoldCount >= maxHoldCount {
			earliest := earliestCandidateDueDay(rows)
			ids := obligationsFeasibleOnDriveDate(earliest, rows)
			if len(ids) == 0 {
				return nil, nil, false
			}
			return &earliest, ids, false
		}
	}
	candidates := candidateDriveDates(now, rows)
	nowDay := businessDate(now)
	bestScore := -1
	var bestDate *time.Time
	for _, candidate := range candidates {
		if candidate.Before(nowDay) {
			continue
		}
		ids := obligationsFeasibleOnDriveDate(candidate, rows)
		if len(ids) == 0 {
			continue
		}
		score := scoreDriveDate(candidate, rows, ids, now, priority)
		if score > bestScore || (score == bestScore && bestDate != nil && candidate.Before(*bestDate)) {
			day := candidate
			bestDate = &day
			bestScore = score
		}
	}
	if bestDate == nil {
		earliest := earliestCandidateDueDay(rows)
		ids := obligationsFeasibleOnDriveDate(earliest, rows)
		if len(ids) == 0 {
			return nil, nil, false
		}
		return &earliest, ids, false
	}
	earliest := earliestCandidateDueDay(rows)
	if !businessDate(*bestDate).After(businessDate(earliest)) {
		ids := obligationsFeasibleOnDriveDate(*bestDate, rows)
		return bestDate, ids, false
	}
	holdCap := businessDate(earliest).AddDate(0, 0, int(maxHoldDays))
	chosen := *bestDate
	recordHold := true
	if chosen.After(holdCap) {
		chosen = holdCap
	}
	ids := obligationsFeasibleOnDriveDate(chosen, rows)
	if len(ids) == 0 {
		ids = obligationsFeasibleOnDriveDate(*bestDate, rows)
		return bestDate, ids, false
	}
	return &chosen, ids, recordHold && businessDate(chosen).After(businessDate(earliest))
}

func earliestCandidateDueDay(rows []driveCandidate) time.Time {
	earliest := businessDate(rows[0].DueAt)
	for _, row := range rows[1:] {
		day := businessDate(row.DueAt)
		if day.Before(earliest) {
			earliest = day
		}
	}
	return earliest
}

func scoreDriveDate(day time.Time, rows []driveCandidate, feasibleIDs []string, now time.Time, vaccinePriority int32) int {
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
		latest := driveLatestDate(row)
		daysLeft := int(businessDate(latest).Sub(day).Hours() / 24)
		if daysLeft <= 3 {
			score += 40
		} else if daysLeft <= 7 {
			score += 20
		}
		overdue := int(businessDate(now).Sub(businessDate(row.DueAt)).Hours() / 24)
		if overdue > 0 {
			score += 10 + overdue*5
		}
		if row.BreedingReadyPriority {
			score += 25
		}
	}
	return score
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
		earliest := driveEarliestDate(row)
		latest := driveLatestDate(row)
		if day.Before(earliest) || day.After(latest) {
			continue
		}
		ids = append(ids, row.ObligationID)
	}
	return ids
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
	return businessDate(row.DueAt)
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

func businessDate(t time.Time) time.Time {
	return biztime.BusinessDayStart(t)
}
