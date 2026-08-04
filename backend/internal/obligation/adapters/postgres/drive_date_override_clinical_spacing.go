package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
)

const vaccinationDriveOverrideSafeHorizonDays = 120

type vaccinationDriveClinicalShift struct {
	AppliedDate           string `json:"applied_override_date,omitempty"`
	RequestedDate         string `json:"requested_override_date,omitempty"`
	ReasonCode            string `json:"reason_code,omitempty"`
	ConflictVaccineCode   string `json:"conflicting_vaccine_code,omitempty"`
	ConflictVaccineLabel  string `json:"conflicting_vaccine_label,omitempty"`
	ConflictDate          string `json:"conflicting_date,omitempty"`
	ConflictRule          string `json:"conflicting_rule,omitempty"`
	ConflictMinimumGapDay int    `json:"conflicting_minimum_gap_days,omitempty"`
	raw                   []byte
}

func (m vaccinationDriveClinicalShift) reason() string {
	return strings.TrimSpace(m.ReasonCode)
}

func (m vaccinationDriveClinicalShift) json() string {
	if strings.TrimSpace(m.ReasonCode) == "" {
		return "{}"
	}
	data, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (m vaccinationDriveClinicalShift) applyTo(out *domain.VaccineDriveDateOverride) {
	if out == nil {
		return
	}
	if len(m.raw) > 0 {
		_ = json.Unmarshal(m.raw, &m)
	}
	out.AutoShifted = strings.TrimSpace(m.ReasonCode) == "clinical_spacing_auto_shift"
	if out.ShiftReason == "" {
		out.ShiftReason = strings.TrimSpace(m.ReasonCode)
	}
	out.ConflictVaccineCode = strings.TrimSpace(m.ConflictVaccineCode)
	out.ConflictVaccineLabel = strings.TrimSpace(m.ConflictVaccineLabel)
	out.ConflictRule = strings.TrimSpace(m.ConflictRule)
	if m.ConflictDate != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", m.ConflictDate, biztime.DefaultLocation()); err == nil {
			out.ConflictDate = parsed
		}
	}
	if out.RequestedOverrideDate.IsZero() {
		out.RequestedOverrideDate = out.OverrideDate
	}
}

type vaccinationClinicalRule struct {
	code          string
	label         string
	vaccineType   string
	pathogenClass string
	courseType    string
	minGapDays    int
}

type vaccinationClinicalConflict struct {
	vaccineCode string
	label       string
	date        time.Time
	rule        string
	minGapDays  int
}

type vaccinationDriveAffectedCohort struct {
	goats         []pgtype.UUID
	obligations   []pgtype.UUID
	currentDates  []time.Time
	hasMembership bool
}

func (r *Repository) clinicallySafeVaccinationDriveOverrideDate(ctx context.Context, tenantID, parkID, vaccineCode string, original, requested time.Time) (time.Time, vaccinationDriveClinicalShift, error) {
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return time.Time{}, vaccinationDriveClinicalShift{}, fmt.Errorf("obligation: tenant id: %w", err)
	}
	park, err := pgconv.UUID(parkID)
	if err != nil {
		return time.Time{}, vaccinationDriveClinicalShift{}, fmt.Errorf("obligation: park id: %w", err)
	}
	moved, err := r.vaccinationClinicalRuleForCode(ctx, tenant, vaccineCode)
	if err != nil {
		return time.Time{}, vaccinationDriveClinicalShift{}, err
	}
	if strings.TrimSpace(moved.code) == "" {
		return requested, vaccinationDriveClinicalShift{}, nil
	}
	cohort, err := r.affectedVaccinationDriveCohort(ctx, tenant, park, vaccineCode, original)
	if err != nil {
		return time.Time{}, vaccinationDriveClinicalShift{}, err
	}
	if len(cohort.goats) == 0 {
		return requested, vaccinationDriveClinicalShift{}, nil
	}
	var firstConflict *vaccinationClinicalConflict
	for candidate := businessDateOnly(requested); !candidate.After(businessDateOnly(requested).AddDate(0, 0, vaccinationDriveOverrideSafeHorizonDays)); candidate = candidate.AddDate(0, 0, 1) {
		conflict, err := r.vaccinationClinicalConflictForDate(ctx, tenant, park, cohort, moved, candidate, original)
		if err != nil {
			return time.Time{}, vaccinationDriveClinicalShift{}, err
		}
		if conflict == nil {
			if firstConflict == nil {
				return candidate, vaccinationDriveClinicalShift{}, nil
			}
			return candidate, vaccinationDriveClinicalShift{
				RequestedDate:         businessDateOnly(requested).Format("2006-01-02"),
				AppliedDate:           candidate.Format("2006-01-02"),
				ReasonCode:            "clinical_spacing_auto_shift",
				ConflictVaccineCode:   firstConflict.vaccineCode,
				ConflictVaccineLabel:  firstConflict.label,
				ConflictDate:          businessDateOnly(firstConflict.date).Format("2006-01-02"),
				ConflictRule:          firstConflict.rule,
				ConflictMinimumGapDay: firstConflict.minGapDays,
			}, nil
		}
		if firstConflict == nil {
			c := *conflict
			firstConflict = &c
		}
	}
	return time.Time{}, vaccinationDriveClinicalShift{}, fmt.Errorf("obligation: no clinically safe vaccination drive date found within %d days of requested override date", vaccinationDriveOverrideSafeHorizonDays)
}

func (r *Repository) clinicallySafeVaccinationDriveAvailability(ctx context.Context, tenantID, parkID, vaccineCode string, original time.Time, availability []vaccexecapp.DriveDateAvailability) ([]vaccexecapp.DriveDateAvailability, error) {
	if len(availability) == 0 {
		return availability, nil
	}
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("obligation: tenant id: %w", err)
	}
	park, err := pgconv.UUID(parkID)
	if err != nil {
		return nil, fmt.Errorf("obligation: park id: %w", err)
	}
	moved, err := r.vaccinationClinicalRuleForCode(ctx, tenant, vaccineCode)
	if err != nil {
		return nil, err
	}
	cohort, err := r.affectedVaccinationDriveCohort(ctx, tenant, park, vaccineCode, original)
	if err != nil {
		return nil, err
	}
	if len(cohort.goats) == 0 {
		return availability, nil
	}
	out := make([]vaccexecapp.DriveDateAvailability, 0, len(availability))
	for _, day := range availability {
		conflict, err := r.vaccinationClinicalConflictForDate(ctx, tenant, park, cohort, moved, day.Date, original)
		if err != nil {
			return nil, err
		}
		if conflict != nil {
			continue
		}
		out = append(out, day)
	}
	return out, nil
}

func (r *Repository) vaccinationClinicalRuleForCode(ctx context.Context, tenant pgtype.UUID, vaccineCode string) (vaccinationClinicalRule, error) {
	var out vaccinationClinicalRule
	err := r.pool.QueryRow(ctx, `
SELECT COALESCE(NULLIF(pr.vaccine_code, ''), prd.vaccine_code, $2),
       COALESCE(NULLIF(pr.vaccine_code, ''), prd.vaccine_code, $2),
       COALESCE(NULLIF(pr.vaccine_type, ''), 'killed'),
       COALESCE(NULLIF(pr.pathogen_class, ''), ''),
       COALESCE(NULLIF(pr.course_type, ''), ''),
       COALESCE(NULLIF(pr.min_gap_days, 0), 0)
FROM protocol_rule_dimensions prd
JOIN protocol_rules pr
  ON pr.tenant_id = prd.tenant_id
 AND pr.rule_id = prd.rule_id
WHERE prd.tenant_id = $1
  AND lower(btrim(prd.vaccine_code)) = lower(btrim($2))
ORDER BY pr.sort_order, pr.created_at
LIMIT 1`, tenant, strings.TrimSpace(vaccineCode)).Scan(&out.code, &out.label, &out.vaccineType, &out.pathogenClass, &out.courseType, &out.minGapDays)
	if err == nil {
		out.vaccineType = normalizedVaccineType(out.vaccineType)
		return out, nil
	}
	if err == pgx.ErrNoRows {
		return vaccinationClinicalRule{code: strings.TrimSpace(vaccineCode), label: strings.TrimSpace(vaccineCode), vaccineType: "killed"}, nil
	}
	return vaccinationClinicalRule{}, fmt.Errorf("obligation: read vaccination clinical rule: %w", err)
}

func (r *Repository) affectedVaccinationDriveCohort(ctx context.Context, tenant, park pgtype.UUID, vaccineCode string, original time.Time) (vaccinationDriveAffectedCohort, error) {
	rows, err := r.pool.Query(ctx, `
WITH moved_rules AS (
  SELECT COALESCE(array_agg(DISTINCT rule_id), '{}'::uuid[]) AS rule_ids
  FROM protocol_rule_dimensions
  WHERE tenant_id = $1
    AND lower(btrim(vaccine_code)) = lower(btrim($3))
), active_override_starts AS (
  SELECT override_date AS planned_date
  FROM vaccination_drive_date_overrides
  WHERE tenant_id = $1
    AND park_id = $2
    AND lower(btrim(vaccine_code)) = lower(btrim($3))
    AND original_drive_date = $4
    AND canceled_at IS NULL
), active_dates AS (
  SELECT $4::date AS planned_date
  UNION
  SELECT generate_series(
    planned_date,
    planned_date + ($5::int * INTERVAL '1 day'),
    INTERVAL '1 day'
  )::date AS planned_date
  FROM active_override_starts
)
SELECT DISTINCT m.goat_id, m.obligation_id, vda.planned_date
FROM vaccination_drive_assignments vda
JOIN moved_rules ON true
JOIN active_dates ad ON ad.planned_date = vda.planned_date
LEFT JOIN obligation_batches ob
  ON ob.tenant_id = vda.tenant_id
 AND ob.batch_id = vda.batch_id
JOIN vaccination_drive_assignment_members m
  ON m.tenant_id = vda.tenant_id
 AND m.assignment_id = vda.assignment_id
JOIN obligation_instances oi
  ON oi.tenant_id = m.tenant_id
 AND oi.obligation_id = m.obligation_id
WHERE vda.tenant_id = $1
  AND vda.park_id = $2
  AND (
    vda.planned_date = $4
    OR ob.planned_date = $4
  )
  AND oi.rule_id = ANY(moved_rules.rule_ids)
  AND oi.status <> 'canceled'`, tenant, park, strings.TrimSpace(vaccineCode), businessDateOnly(original), vaccinationDriveOverrideSafeHorizonDays)
	if err != nil {
		return vaccinationDriveAffectedCohort{}, fmt.Errorf("obligation: read affected vaccination drive cohort: %w", err)
	}
	defer rows.Close()
	var out vaccinationDriveAffectedCohort
	seenGoats := make(map[pgtype.UUID]struct{})
	seenObligations := make(map[pgtype.UUID]struct{})
	seenDates := make(map[string]struct{})
	for rows.Next() {
		var goatID, obligationID pgtype.UUID
		var plannedDate time.Time
		if err := rows.Scan(&goatID, &obligationID, &plannedDate); err != nil {
			return vaccinationDriveAffectedCohort{}, fmt.Errorf("obligation: scan affected vaccination drive cohort: %w", err)
		}
		if _, ok := seenGoats[goatID]; !ok {
			seenGoats[goatID] = struct{}{}
			out.goats = append(out.goats, goatID)
		}
		if _, ok := seenObligations[obligationID]; !ok {
			seenObligations[obligationID] = struct{}{}
			out.obligations = append(out.obligations, obligationID)
		}
		dateKey := businessDateOnly(plannedDate).Format("2006-01-02")
		if _, ok := seenDates[dateKey]; !ok {
			seenDates[dateKey] = struct{}{}
			out.currentDates = append(out.currentDates, businessDateOnly(plannedDate))
		}
	}
	if err := rows.Err(); err != nil {
		return vaccinationDriveAffectedCohort{}, fmt.Errorf("obligation: affected vaccination drive cohort rows: %w", err)
	}
	out.hasMembership = len(out.goats) > 0
	return out, nil
}

func (r *Repository) vaccinationClinicalConflictForDate(ctx context.Context, tenant, park pgtype.UUID, cohort vaccinationDriveAffectedCohort, moved vaccinationClinicalRule, candidate, original time.Time) (*vaccinationClinicalConflict, error) {
	rows, err := r.pool.Query(ctx, `
WITH affected(goat_id) AS (SELECT unnest($3::uuid[])),
events AS (
  SELECT vc.goat_id,
         vc.administered_at::date AS event_date,
         COALESCE(NULLIF(pr.vaccine_code, ''), prd.vaccine_code, '') AS vaccine_code,
         COALESCE(NULLIF(pr.vaccine_code, ''), prd.vaccine_code, '') AS vaccine_label,
         COALESCE(NULLIF(pr.vaccine_type, ''), 'killed') AS vaccine_type,
         COALESCE(NULLIF(pr.pathogen_class, ''), '') AS pathogen_class,
         COALESCE(NULLIF(pr.course_type, ''), '') AS course_type,
         COALESCE(NULLIF(pr.min_gap_days, 0), 0) AS min_gap_days
  FROM vaccination_completions vc
  JOIN obligation_instances oi ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
  LEFT JOIN protocol_rules pr ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
  LEFT JOIN protocol_rule_dimensions prd ON prd.tenant_id = oi.tenant_id AND prd.rule_id = oi.rule_id
  JOIN affected a ON a.goat_id = vc.goat_id
  WHERE vc.tenant_id = $1
    AND vc.status = 'accepted'
    AND NOT (vc.obligation_id = ANY($7::uuid[]))
  UNION ALL
  SELECT oi.target_id AS goat_id,
         COALESCE(vda.planned_date, oi.due_at::date) AS event_date,
         COALESCE(NULLIF(pr.vaccine_code, ''), prd.vaccine_code, '') AS vaccine_code,
         COALESCE(NULLIF(pr.vaccine_code, ''), prd.vaccine_code, '') AS vaccine_label,
         COALESCE(NULLIF(pr.vaccine_type, ''), 'killed') AS vaccine_type,
         COALESCE(NULLIF(pr.pathogen_class, ''), '') AS pathogen_class,
         COALESCE(NULLIF(pr.course_type, ''), '') AS course_type,
         COALESCE(NULLIF(pr.min_gap_days, 0), 0) AS min_gap_days
  FROM obligation_instances oi
  JOIN affected a ON a.goat_id = oi.target_id
  LEFT JOIN vaccination_drive_assignment_members m ON m.tenant_id = oi.tenant_id AND m.obligation_id = oi.obligation_id
  LEFT JOIN vaccination_drive_assignments vda ON vda.tenant_id = m.tenant_id AND vda.assignment_id = m.assignment_id
  LEFT JOIN protocol_rules pr ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
  LEFT JOIN protocol_rule_dimensions prd ON prd.tenant_id = oi.tenant_id AND prd.rule_id = oi.rule_id
  WHERE oi.tenant_id = $1
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred')
    AND NOT (oi.obligation_id = ANY($7::uuid[]))
)
SELECT vaccine_code, vaccine_label, event_date, vaccine_type, pathogen_class, course_type, min_gap_days
FROM events
WHERE event_date BETWEEN ($5::date - INTERVAL '60 days')::date AND ($5::date + INTERVAL '60 days')::date
ORDER BY ABS(event_date - $5::date), event_date
LIMIT 50`, tenant, park, cohort.goats, moved.code, businessDateOnly(candidate), businessDateOnly(original), cohort.obligations)
	if err != nil {
		return nil, fmt.Errorf("obligation: read vaccination clinical conflicts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var other vaccinationClinicalRule
		var eventDate time.Time
		if err := rows.Scan(&other.code, &other.label, &eventDate, &other.vaccineType, &other.pathogenClass, &other.courseType, &other.minGapDays); err != nil {
			return nil, fmt.Errorf("obligation: scan vaccination clinical conflict: %w", err)
		}
		other.vaccineType = normalizedVaccineType(other.vaccineType)
		if rule, gap := vaccinationSpacingRule(moved, other, candidate, eventDate); gap > 0 {
			return &vaccinationClinicalConflict{vaccineCode: other.code, label: other.label, date: eventDate, rule: rule, minGapDays: gap}, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("obligation: vaccination clinical conflict rows: %w", err)
	}
	return nil, nil
}

func normalizedVaccineType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.Contains(value, "live") {
		return "live"
	}
	return "killed"
}

func vaccinationSpacingRule(moved, other vaccinationClinicalRule, candidate, eventDate time.Time) (string, int) {
	if sameDayCompatible(moved, other, candidate, eventDate) {
		return "", 0
	}
	gap := 14
	rule := "live_killed_min_gap"
	if moved.vaccineType == "live" && other.vaccineType == "live" {
		gap = 28
		rule = "live_live_min_gap"
	} else if moved.vaccineType == "killed" && other.vaccineType == "killed" {
		rule = "killed_killed_min_gap"
	}
	if strings.EqualFold(moved.code, other.code) {
		if moved.minGapDays > gap {
			gap = moved.minGapDays
		}
		if other.minGapDays > gap {
			gap = other.minGapDays
		}
		rule = "booster_min_gap"
	}
	days := int(businessDateOnly(candidate).Sub(businessDateOnly(eventDate)).Hours() / 24)
	if days < 0 {
		days = -days
	}
	if days < gap {
		return rule, gap
	}
	return "", 0
}

func sameDayCompatible(moved, other vaccinationClinicalRule, candidate, eventDate time.Time) bool {
	if !businessDateOnly(candidate).Equal(businessDateOnly(eventDate)) {
		return false
	}
	if strings.EqualFold(moved.code, other.code) {
		return false
	}
	return domain.VaccinesShareApprovedCombo(moved.code, other.code)
}
