package postgres

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// AUTOMATIC CLOCK-IN VIOLATIONS (maintainer decisions 2026-09-30, migration 000472). One bounded
// candidate read and one set-based insert per tick: the candidates are everyone mapped to a shift
// whose home park times that shift, on today and yesterday (IST), not on leave they applied for,
// who either clocked in past the grace (late) or had not clocked in by the shift's end (absent) and
// carries no attendance violation of that kind for that day yet.
//
// projection-review: membership=workforce_member_shifts of ONE tenant (PK tenant+member, so one shift per person) x the two business days; group_key=none, each candidate is one (member, day, kind); join_cardinality=member (PK), shift catalog (PK), park timing (PK tenant+park+shift) are 1:1 and the clock-in is a LATERAL LIMIT 1 (the first clock-in inside the shift's window), leave, holidays and existing violations are EXISTS probes, so no candidate repeats; pagination=bounded by LIMIT, the next tick picks up the rest (NOT EXISTS skips what was inserted); scope=explicit tenant + two dates.

const sqlAttendanceCandidates = `
WITH days AS (
  SELECT d::date AS d FROM unnest(ARRAY[$2::date - 1, $2::date]) AS d
),
base AS (
  SELECT wm.workforce_member_id, wm.primary_location_id AS park_id, ms.shift_code, sc.label AS shift_label,
         days.d, t.start_minute, t.end_minute,
         (days.d::timestamp + make_interval(mins => t.start_minute)) AT TIME ZONE 'Asia/Kolkata' AS starts_at,
         CASE
           WHEN t.end_minute IS NULL THEN (days.d + 1)::timestamp AT TIME ZONE 'Asia/Kolkata'
           WHEN t.end_minute > t.start_minute THEN (days.d::timestamp + make_interval(mins => t.end_minute)) AT TIME ZONE 'Asia/Kolkata'
           ELSE ((days.d + 1)::timestamp + make_interval(mins => t.end_minute)) AT TIME ZONE 'Asia/Kolkata'
         END AS ends_at
  FROM workforce_member_shifts ms
  JOIN workforce_members wm
    ON wm.tenant_id = ms.tenant_id AND wm.workforce_member_id = ms.workforce_member_id
   AND wm.status = 'active' AND wm.primary_location_id IS NOT NULL
  JOIN workforce_shift_catalog sc ON sc.shift_code = ms.shift_code AND sc.status = 'active'
  JOIN workforce_park_shift_timings t
    ON t.tenant_id = ms.tenant_id AND t.park_id = wm.primary_location_id AND t.shift_code = ms.shift_code
   AND t.start_minute IS NOT NULL
  CROSS JOIN days
  WHERE ms.tenant_id = $1::uuid
    -- Only people who CAN clock in: an app login.
    AND wm.user_id IS NOT NULL
    -- Never before the check's start date, and never on the day the shift was set: nobody is late
    -- for a shift they were given at noon.
    AND days.d >= $8::date
    AND days.d > (ms.updated_at AT TIME ZONE 'Asia/Kolkata')::date
    -- Never on the person's weekly off (ISO weekday, Monday = 1).
    AND NOT (EXTRACT(ISODOW FROM days.d)::smallint = ANY(ms.week_offs))
    -- Never on a holiday HR entered for every park or for this park.
    AND NOT EXISTS (
      SELECT 1 FROM workforce_holidays h
      WHERE h.tenant_id = ms.tenant_id AND h.status = 'active' AND h.holiday_on = days.d
        AND (h.park_id IS NULL OR h.park_id = wm.primary_location_id))
    -- A day covered by leave the person APPLIED for needs no clock-in (pending or approved).
    AND NOT EXISTS (
      SELECT 1 FROM workforce_leave_requests lr
      WHERE lr.tenant_id = ms.tenant_id AND lr.workforce_member_id = ms.workforce_member_id
        AND lr.status IN ('pending', 'approved') AND lr.starts_on <= days.d AND lr.ends_on >= days.d)
),
cand AS (
  SELECT b.*, e.clock_in_at,
         CASE WHEN e.clock_in_at IS NULL THEN 'absent' ELSE 'late' END AS kind
  FROM base b
  -- The shift's clock-in is the first one INSIDE its window (up to 6 hours early, before its end),
  -- matched on time, not on the punch's business date: a night shift's 00:30 punch belongs to the
  -- shift that started the evening before. A clock-in only after the shift ended is no clock-in.
  LEFT JOIN LATERAL (
    SELECT x.clock_in_at FROM workforce_clock_entries x
    WHERE x.tenant_id = $1::uuid AND x.workforce_member_id = b.workforce_member_id
      AND x.business_date BETWEEN b.d - 1 AND b.d + 1
      AND x.clock_in_at >= b.starts_at - interval '6 hours' AND x.clock_in_at < b.ends_at
    ORDER BY x.clock_in_at LIMIT 1
  ) e ON true
  WHERE (e.clock_in_at IS NULL AND $6::boolean AND $3::timestamptz >= b.ends_at)
     OR (e.clock_in_at IS NOT NULL AND $5::boolean
         AND date_trunc('minute', e.clock_in_at) > b.starts_at + make_interval(mins => $4::int))
)
SELECT c.workforce_member_id::text, c.park_id::text, c.shift_code, c.shift_label, c.d, c.kind,
       c.start_minute, c.clock_in_at
FROM cand c
WHERE NOT EXISTS (
  SELECT 1 FROM workforce_violations v
  WHERE v.tenant_id = $1::uuid AND v.workforce_member_id = c.workforce_member_id
    -- ONE automatic violation per person per day, of either kind: a late offline punch arriving
    -- after "did not clock in" must not add a contradictory second one (HR decides the first).
    AND v.occurred_on = c.d AND v.source = 'attendance')
ORDER BY c.d, c.workforce_member_id
LIMIT $7`

// The insert and its system audit rows in ONE statement; ON CONFLICT on the natural key makes an
// overlapping tick insert nothing twice.
const sqlAttendanceInsert = `
WITH ins AS (
  INSERT INTO workforce_violations (
    tenant_id, workforce_member_id, park_id, type_key, type_label, fine_rupees, occurred_on, note,
    source, sop_version, recorded_by, status, attendance_kind, shift_code, detail)
  SELECT $1::uuid, u.member, u.park, u.type_key, u.type_label, 0, u.d, '',
         'attendance', $2::int, NULL, 'pending', u.kind, u.shift, u.detail
  FROM unnest($3::uuid[], $4::uuid[], $5::text[], $6::text[], $7::date[], $8::text[], $9::text[], $10::text[])
       AS u(member, park, type_key, type_label, d, kind, shift, detail)
  ON CONFLICT (tenant_id, workforce_member_id, occurred_on, attendance_kind) WHERE source = 'attendance' DO NOTHING
  RETURNING violation_id, workforce_member_id, occurred_on, attendance_kind
)
INSERT INTO audit_log (tenant_id, actor_id, actor_type, action, resource_type, resource_id, scope_type, scope_id, metadata)
SELECT $1::uuid, NULL, 'system', 'workforce.violation.raised', 'workforce_violation', ins.violation_id, 'tenant', $1::uuid,
       jsonb_build_object('person_id', ins.workforce_member_id, 'occurred_on', ins.occurred_on, 'kind', ins.attendance_kind,
                          'sop_version', $2::int, 'source', 'attendance')
FROM ins`

// AttendanceCandidates reads who is owed an automatic clock-in violation now.
func (r *Repository) AttendanceCandidates(ctx context.Context, q ports.AttendanceQuery) ([]ports.AttendanceCandidate, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	startsOn := q.StartsOn
	if startsOn == "" {
		startsOn = "2000-01-01"
	}
	rows, err := r.pool.Query(ctx, sqlAttendanceCandidates, q.TenantID, q.Today, q.Now, q.GraceMinutes, q.LateOn, q.AbsentOn, q.Limit, startsOn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.AttendanceCandidate{}
	for rows.Next() {
		var c ports.AttendanceCandidate
		if err := rows.Scan(&c.PersonID, &c.ParkID, &c.ShiftCode, &c.ShiftLabel, &c.Day, &c.Kind, &c.StartMinute, &c.ClockInAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertAttendanceViolations inserts WAITING violations, one per (person, day, kind); returns how
// many were new.
func (r *Repository) InsertAttendanceViolations(ctx context.Context, tenantID string, sopVersion int, items []ports.NewAttendanceViolation) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	n := len(items)
	members, parks, keys, labels, kinds, shifts, details := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	days := make([]time.Time, n)
	for i, it := range items {
		members[i], parks[i], keys[i], labels[i] = it.PersonID, it.ParkID, it.TypeKey, it.TypeLabel
		days[i], kinds[i], shifts[i], details[i] = it.Day, it.Kind, it.ShiftCode, it.Detail
	}
	tag, err := r.pool.Exec(ctx, sqlAttendanceInsert, tenantID, sopVersion, members, parks, keys, labels, days, kinds, shifts, details)
	if err != nil {
		return 0, mapWriteErr(err)
	}
	return int(tag.RowsAffected()), nil
}

// A waiting automatic violation whose day is NOW excused -- leave the person applied for, a holiday
// HR entered for their park or every park, or their weekly off -- closes itself with that reason.
// Bounded: it only ever looks at WAITING attendance rows (the pending partial index).
const sqlAttendanceCloseExcused = `
WITH excused AS (
  SELECT v.violation_id,
         CASE
           WHEN EXISTS (SELECT 1 FROM workforce_leave_requests lr
                        WHERE lr.tenant_id = v.tenant_id AND lr.workforce_member_id = v.workforce_member_id
                          AND lr.status IN ('pending', 'approved') AND lr.starts_on <= v.occurred_on AND lr.ends_on >= v.occurred_on)
             THEN $2
           WHEN EXISTS (SELECT 1 FROM workforce_holidays h
                        WHERE h.tenant_id = v.tenant_id AND h.status = 'active' AND h.holiday_on = v.occurred_on
                          AND (h.park_id IS NULL OR h.park_id = v.park_id))
             THEN $3
           WHEN EXISTS (SELECT 1 FROM workforce_member_shifts ms
                        WHERE ms.tenant_id = v.tenant_id AND ms.workforce_member_id = v.workforce_member_id
                          AND EXTRACT(ISODOW FROM v.occurred_on)::smallint = ANY(ms.week_offs))
             THEN $4
         END AS reason
  FROM workforce_violations v
  WHERE v.tenant_id = $1::uuid AND v.status = 'pending' AND v.source = 'attendance'
  ORDER BY v.occurred_on DESC, v.violation_id DESC
  LIMIT 500
),
closed AS (
  UPDATE workforce_violations v
  SET status = 'closed', decided_at = now(), decision_note = x.reason, row_version = v.row_version + 1
  FROM excused x
  WHERE v.violation_id = x.violation_id AND x.reason IS NOT NULL AND v.status = 'pending'
  RETURNING v.violation_id, v.workforce_member_id, x.reason
)
INSERT INTO audit_log (tenant_id, actor_id, actor_type, action, resource_type, resource_id, scope_type, scope_id, metadata)
SELECT $1::uuid, NULL, 'system', 'workforce.violation.closed', 'workforce_violation', closed.violation_id, 'tenant', $1::uuid,
       jsonb_build_object('person_id', closed.workforce_member_id, 'reason', closed.reason, 'source', 'attendance')
FROM closed`

func (r *Repository) CloseExcusedAttendance(ctx context.Context, tenantID string, reasons ports.AttendanceCloseCopy) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, sqlAttendanceCloseExcused, tenantID, reasons.Leave, reasons.Holiday, reasons.WeekOff)
	if err != nil {
		return 0, mapWriteErr(err)
	}
	return int(tag.RowsAffected()), nil
}
