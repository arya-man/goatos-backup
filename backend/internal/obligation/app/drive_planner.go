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
	ObligationID string
	DueAt        time.Time
	WindowStart  *time.Time
	WindowEnd    *time.Time
}

func driveCandidatesFromUnbatched(rows []domain.UnbatchedDue) []driveCandidate {
	out := make([]driveCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, driveCandidate{
			ObligationID: row.ObligationID,
			DueAt:        row.DueAt,
			WindowStart:  row.WindowStart,
			WindowEnd:    row.WindowEnd,
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

func sweepWindowGroupKey(r domain.UnbatchedDue) string {
	return r.ScopeType + "|" + r.ScopeID + "|" + r.RuleID + "|" + r.TargetSpecies + "|" + dueDateKey(r.DueAt) + "|" + timeKey(r.WindowStart) + "|" + timeKey(r.WindowEnd)
}

func dueDateKey(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return businessDate(t).Format("2006-01-02")
}

func pickBestDriveDate(now time.Time, rows []driveCandidate, priority int32) *time.Time {
	if len(rows) == 0 {
		return nil
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
	if bestDate != nil {
		return bestDate
	}
	earliest := businessDate(rows[0].DueAt)
	for _, row := range rows[1:] {
		day := businessDate(row.DueAt)
		if day.Before(earliest) {
			earliest = day
		}
	}
	return &earliest
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
