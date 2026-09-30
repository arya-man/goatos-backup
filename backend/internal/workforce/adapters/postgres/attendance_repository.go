package postgres

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// AUTOMATIC CLOCK-IN VIOLATIONS (maintainer decisions 2026-09-30, migration 000459). One bounded
// candidate read and one set-based insert per tick: the candidates are everyone mapped to a shift
// whose home park times that shift, on today and yesterday (IST), not on leave they applied for,
// who either clocked in past the grace (late) or had not clocked in by the shift's end (absent) and
// carries no attendance violation of that kind for that day yet.
//
// projection-review: membership=workforce_member_shifts of ONE tenant (PK tenant+member, so one shift per person) x the two business days; group_key=none, each candidate is one (member, day, kind); join_cardinality=member (PK), shift catalog (PK), park timing (PK tenant+park+shift) and clock entry (UNIQUE tenant+member+business_date) are each 1:1, leave and existing violations are EXISTS probes, so no candidate repeats; pagination=bounded by LIMIT, the next tick picks up the rest (NOT EXISTS skips what was inserted); scope=explicit tenant + two dates.

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
    -- Never on the day the shift was set: nobody is late for a shift they were given at noon.
    AND days.d > (ms.updated_at AT TIME ZONE 'Asia/Kolkata')::date
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
  LEFT JOIN workforce_clock_entries e
    ON e.tenant_id = $1::uuid AND e.workforce_member_id = b.workforce_member_id AND e.business_date = b.d
  WHERE (e.clock_in_at IS NULL AND $6::boolean AND $3::timestamptz >= b.ends_at)
     OR (e.clock_in_at IS NOT NULL AND $5::boolean
         AND e.clock_in_at > b.starts_at + make_interval(mins => $4::int) AND e.clock_in_at < b.ends_at)
)
SELECT c.workforce_member_id::text, c.park_id::text, c.shift_code, c.shift_label, c.d, c.kind,
       c.start_minute, c.clock_in_at
FROM cand c
WHERE NOT EXISTS (
  SELECT 1 FROM workforce_violations v
  WHERE v.tenant_id = $1::uuid AND v.workforce_member_id = c.workforce_member_id
    AND v.occurred_on = c.d AND v.source = 'attendance' AND v.attendance_kind = c.kind)
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
	rows, err := r.pool.Query(ctx, sqlAttendanceCandidates, q.TenantID, q.Today, q.Now, q.GraceMinutes, q.LateOn, q.AbsentOn, q.Limit)
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
