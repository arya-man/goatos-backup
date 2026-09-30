package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// HRMS Timetable (maintainer request 2026-09-30, migration 000457). Staff-sized reads: every
// query is scoped to one tenant and one park, the people list is keyset-paged on
// (lower(display_name), workforce_member_id), and every join is 1:1 (member -> person_access ->
// designation, member -> department, member -> shift on the member PK).
//
// projection-review: grain = person. ParkShiftHeadcount groups the park's ACTIVE members by
// their one shift code ("" = no row), so each person lands in exactly one bucket and the buckets
// sum to the park's headcount; ListTimetablePeople ranges over the SAME member set (tenant,
// primary_location_id = park, status active) with the same LEFT JOIN on the member PK, so the
// counts the page prints and the rows it pages can never disagree.

const (
	sqlTimetableParks = `
SELECT location_id::text, COALESCE(location_code, ''), name
FROM locations
WHERE tenant_id = $1::uuid AND location_type = 'park' AND status = 'active'
ORDER BY COALESCE(location_code, name), name, location_id`

	sqlParkShiftTimings = `
SELECT c.shift_code, c.label, t.start_minute, t.end_minute, COALESCE(t.row_version, 0)
FROM workforce_shift_catalog c
LEFT JOIN workforce_park_shift_timings t
  ON t.tenant_id = $1::uuid AND t.park_id = $2::uuid AND t.shift_code = c.shift_code
WHERE c.status = 'active'
ORDER BY c.sort_order, c.shift_code`

	// projection-review: membership=workforce_members of ONE tenant whose primary_location_id is the park and status is active; group_key=the member's one shift_code ('' = no workforce_member_shifts row); join_cardinality=workforce_member_shifts is keyed (tenant_id, workforce_member_id) so the LEFT JOIN is 1:0..1 and no member is counted twice; pagination=whole-park aggregate taken before and independent of the keyset people page; scope=explicit tenant + park_id, the same member set ListTimetablePeople pages.
	sqlParkShiftHeadcount = `
SELECT COALESCE(ms.shift_code, ''), count(*)::int
FROM workforce_members wm
LEFT JOIN workforce_member_shifts ms
  ON ms.tenant_id = wm.tenant_id AND ms.workforce_member_id = wm.workforce_member_id
WHERE wm.tenant_id = $1::uuid
  AND wm.primary_location_id = $2::uuid
  AND wm.status = 'active'
GROUP BY 1`

	sqlTimetablePersonSelect = `
SELECT
  wm.workforce_member_id::text,
  wm.display_name,
  COALESCE(dc.label, ''),
  COALESCE(wm.primary_role_hint, ''),
  COALESCE(wm.hr_designation_grade, ''),
  COALESCE(d.label, ''),
  COALESCE(wm.primary_location_id::text, ''),
  COALESCE(ms.shift_code, ''),
  COALESCE(ms.row_version, 0)
FROM workforce_members wm
LEFT JOIN person_access pa
  ON pa.tenant_id = wm.tenant_id AND pa.workforce_member_id = wm.workforce_member_id
-- Only an ACTIVE catalog row names anybody, the People directory's rule.
LEFT JOIN designation_catalog dc
  ON dc.designation_code = pa.designation_code AND dc.status = 'active'
LEFT JOIN departments d
  ON d.tenant_id = wm.tenant_id AND d.department_id = wm.department_id
LEFT JOIN workforce_member_shifts ms
  ON ms.tenant_id = wm.tenant_id AND ms.workforce_member_id = wm.workforce_member_id
`

	sqlTimetablePeople = sqlTimetablePersonSelect + `
WHERE wm.tenant_id = $1::uuid
  AND wm.primary_location_id = $2::uuid
  AND wm.status = 'active'
  AND (
    $3 = ''
    OR ($3 = 'unassigned' AND ms.shift_code IS NULL)
    OR ms.shift_code = $3
  )
  AND ($4 = '' OR (lower(wm.display_name), wm.workforce_member_id::text) > ($4, $5))
ORDER BY lower(wm.display_name), wm.workforce_member_id
LIMIT $6`

	sqlTimetablePerson = sqlTimetablePersonSelect + `
WHERE wm.tenant_id = $1::uuid AND wm.workforce_member_id = $2::uuid`

	sqlTimetableParkLock = `
SELECT 1 FROM locations
WHERE tenant_id = $1::uuid AND location_id = $2::uuid AND location_type = 'park' AND status = 'active'
FOR SHARE`

	sqlTimetableShiftExists = `
SELECT 1 FROM workforce_shift_catalog WHERE shift_code = $1 AND status = 'active'`

	sqlTimetableTimingCurrent = `
SELECT start_minute, end_minute, row_version
FROM workforce_park_shift_timings
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shift_code = $3
FOR UPDATE`

	sqlTimetableTimingInsert = `
INSERT INTO workforce_park_shift_timings (tenant_id, park_id, shift_code, start_minute, end_minute, updated_by, updated_at, row_version)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::uuid, now(), 1)`

	sqlTimetableTimingUpdate = `
UPDATE workforce_park_shift_timings
SET start_minute = $4, end_minute = $5, updated_by = $6::uuid, updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shift_code = $3 AND row_version = $7`

	sqlTimetableMemberLock = `
SELECT 1 FROM workforce_members
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND status = 'active'
FOR SHARE`

	sqlTimetableMemberShiftCurrent = `
SELECT shift_code, row_version
FROM workforce_member_shifts
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid
FOR UPDATE`

	sqlTimetableMemberShiftInsert = `
INSERT INTO workforce_member_shifts (tenant_id, workforce_member_id, shift_code, updated_by, updated_at, row_version)
VALUES ($1::uuid, $2::uuid, $3, $4::uuid, now(), 1)`

	sqlTimetableMemberShiftUpdate = `
UPDATE workforce_member_shifts
SET shift_code = $3, updated_by = $4::uuid, updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND row_version = $5`

	sqlTimetableMemberShiftDelete = `
DELETE FROM workforce_member_shifts
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND row_version = $3`
)

func (r *Repository) TimetableParks(ctx context.Context, tenantID string) ([]ports.TimetableParkRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlTimetableParks, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.TimetableParkRow{}
	for rows.Next() {
		var p ports.TimetableParkRow
		if err := rows.Scan(&p.ParkID, &p.Code, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) ParkShiftTimings(ctx context.Context, tenantID, parkID string) ([]ports.ShiftTimingRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return parkShiftTimings(ctx, r.pool, tenantID, parkID)
}

type timetableQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func parkShiftTimings(ctx context.Context, q timetableQuerier, tenantID, parkID string) ([]ports.ShiftTimingRow, error) {
	rows, err := q.Query(ctx, sqlParkShiftTimings, tenantID, parkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.ShiftTimingRow{}
	for rows.Next() {
		var s ports.ShiftTimingRow
		if err := rows.Scan(&s.ShiftCode, &s.Label, &s.StartMinute, &s.EndMinute, &s.RowVersion); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) ParkShiftHeadcount(ctx context.Context, tenantID, parkID string) (map[string]int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlParkShiftHeadcount, tenantID, parkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var code string
		var n int
		if err := rows.Scan(&code, &n); err != nil {
			return nil, err
		}
		out[code] = n
	}
	return out, rows.Err()
}

func (r *Repository) ListTimetablePeople(ctx context.Context, params ports.ListTimetablePeopleParams) ([]ports.TimetablePersonRow, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cursorName, cursorID := "", ""
	if strings.TrimSpace(params.Cursor) != "" {
		var ok bool
		cursorName, cursorID, ok = decodePeopleCursor(params.Cursor)
		if !ok {
			return nil, "", ports.ErrInvalidFilter
		}
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	bound, err := sqlbind.Bind(sqlTimetablePeople,
		params.TenantID, params.ParkID, params.Shift, cursorName, cursorID, limit+1)
	if err != nil {
		return nil, "", err
	}
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, "", err
	}
	items, err := scanTimetablePeople(rows)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodePeopleCursor(last.DisplayName, last.PersonID)
	}
	return items, next, nil
}

func scanTimetablePeople(rows pgx.Rows) ([]ports.TimetablePersonRow, error) {
	defer rows.Close()
	out := []ports.TimetablePersonRow{}
	for rows.Next() {
		var p ports.TimetablePersonRow
		if err := rows.Scan(&p.PersonID, &p.DisplayName, &p.DesignationLabel, &p.RoleHint, &p.DesignationGrade,
			&p.Department, &p.ParkID, &p.ShiftCode, &p.RowVersion); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) SetParkShiftTiming(ctx context.Context, tenantID, actorUserID, parkID, shiftCode string, start, end *int, rowVersion int) (ports.ShiftTimingRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.ShiftTimingRow{}, err
	}
	defer rollback(ctx, tx)

	var one int
	if err := missingAs(tx.QueryRow(ctx, sqlTimetableParkLock, tenantID, parkID).Scan(&one), ports.ErrUnknownPark); err != nil {
		return ports.ShiftTimingRow{}, err
	}
	if err := missingAs(tx.QueryRow(ctx, sqlTimetableShiftExists, shiftCode).Scan(&one), ports.ErrUnknownShift); err != nil {
		return ports.ShiftTimingRow{}, err
	}

	var curStart, curEnd *int
	curVersion := 0
	err = tx.QueryRow(ctx, sqlTimetableTimingCurrent, tenantID, parkID, shiftCode).Scan(&curStart, &curEnd, &curVersion)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ports.ShiftTimingRow{}, err
	}
	// An exact replay (the values already stored) is a success that writes nothing -- the
	// retried Save after a lost response, or two people setting the same hours.
	if exists && intPtrEqual(curStart, start) && intPtrEqual(curEnd, end) {
		return r.shiftTimingIn(ctx, tx, tenantID, parkID, shiftCode)
	}
	if curVersion != rowVersion {
		return ports.ShiftTimingRow{}, ports.ErrTimetableVersionConflict
	}
	if exists {
		tag, err := tx.Exec(ctx, sqlTimetableTimingUpdate, tenantID, parkID, shiftCode, start, end, actorUserID, rowVersion)
		if err != nil {
			return ports.ShiftTimingRow{}, mapWriteErr(err)
		}
		if tag.RowsAffected() != 1 {
			return ports.ShiftTimingRow{}, ports.ErrTimetableVersionConflict
		}
	} else {
		if start == nil && end == nil {
			// Clearing a timing that was never set: nothing to store.
			return r.shiftTimingIn(ctx, tx, tenantID, parkID, shiftCode)
		}
		if _, err := tx.Exec(ctx, sqlTimetableTimingInsert, tenantID, parkID, shiftCode, start, end, actorUserID); err != nil {
			return ports.ShiftTimingRow{}, mapWriteErr(err)
		}
	}
	scope := "park"
	if err := insertAudit(ctx, tx, tenantID, actorUserID, "workforce.timetable.shift_timing", "workforce_park_shift_timing", parkID, &scope, map[string]any{
		"scope_id": parkID, "shift_code": shiftCode,
		"start_minute": start, "end_minute": end,
		"previous_start_minute": curStart, "previous_end_minute": curEnd,
	}); err != nil {
		return ports.ShiftTimingRow{}, err
	}
	row, err := r.shiftTimingIn(ctx, tx, tenantID, parkID, shiftCode)
	if err != nil {
		return ports.ShiftTimingRow{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.ShiftTimingRow{}, err
	}
	return row, nil
}

func (r *Repository) shiftTimingIn(ctx context.Context, tx pgx.Tx, tenantID, parkID, shiftCode string) (ports.ShiftTimingRow, error) {
	rows, err := parkShiftTimings(ctx, tx, tenantID, parkID)
	if err != nil {
		return ports.ShiftTimingRow{}, err
	}
	for _, row := range rows {
		if row.ShiftCode == shiftCode {
			return row, nil
		}
	}
	return ports.ShiftTimingRow{}, ports.ErrUnknownShift
}

func (r *Repository) SetMemberShift(ctx context.Context, tenantID, actorUserID, personID, shiftCode string, rowVersion int) (ports.TimetablePersonRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.TimetablePersonRow{}, err
	}
	defer rollback(ctx, tx)

	var one int
	if err := missingAs(tx.QueryRow(ctx, sqlTimetableMemberLock, tenantID, personID).Scan(&one), ports.ErrPersonNotFound); err != nil {
		return ports.TimetablePersonRow{}, err
	}
	if shiftCode != "" {
		if err := missingAs(tx.QueryRow(ctx, sqlTimetableShiftExists, shiftCode).Scan(&one), ports.ErrUnknownShift); err != nil {
			return ports.TimetablePersonRow{}, err
		}
	}

	current, curVersion := "", 0
	err = tx.QueryRow(ctx, sqlTimetableMemberShiftCurrent, tenantID, personID).Scan(&current, &curVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ports.TimetablePersonRow{}, err
	}
	// Exact replay: already on that shift (or already on none).
	if current == shiftCode {
		return timetablePersonIn(ctx, tx, tenantID, personID)
	}
	if curVersion != rowVersion {
		return ports.TimetablePersonRow{}, ports.ErrTimetableVersionConflict
	}
	switch {
	case shiftCode == "":
		tag, err := tx.Exec(ctx, sqlTimetableMemberShiftDelete, tenantID, personID, rowVersion)
		if err != nil {
			return ports.TimetablePersonRow{}, mapWriteErr(err)
		}
		if tag.RowsAffected() != 1 {
			return ports.TimetablePersonRow{}, ports.ErrTimetableVersionConflict
		}
	case current == "":
		if _, err := tx.Exec(ctx, sqlTimetableMemberShiftInsert, tenantID, personID, shiftCode, actorUserID); err != nil {
			return ports.TimetablePersonRow{}, mapWriteErr(err)
		}
	default:
		tag, err := tx.Exec(ctx, sqlTimetableMemberShiftUpdate, tenantID, personID, shiftCode, actorUserID, rowVersion)
		if err != nil {
			return ports.TimetablePersonRow{}, mapWriteErr(err)
		}
		if tag.RowsAffected() != 1 {
			return ports.TimetablePersonRow{}, ports.ErrTimetableVersionConflict
		}
	}
	scope := "tenant"
	if err := insertAudit(ctx, tx, tenantID, actorUserID, "workforce.timetable.member_shift", "workforce_member_shift", personID, &scope, map[string]any{
		"shift_code": shiftCode, "previous_shift_code": current,
	}); err != nil {
		return ports.TimetablePersonRow{}, err
	}
	row, err := timetablePersonIn(ctx, tx, tenantID, personID)
	if err != nil {
		return ports.TimetablePersonRow{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.TimetablePersonRow{}, err
	}
	return row, nil
}

func timetablePersonIn(ctx context.Context, tx pgx.Tx, tenantID, personID string) (ports.TimetablePersonRow, error) {
	rows, err := tx.Query(ctx, sqlTimetablePerson, tenantID, personID)
	if err != nil {
		return ports.TimetablePersonRow{}, err
	}
	items, err := scanTimetablePeople(rows)
	if err != nil {
		return ports.TimetablePersonRow{}, err
	}
	if len(items) == 0 {
		return ports.TimetablePersonRow{}, ports.ErrPersonNotFound
	}
	return items[0], nil
}

// missingAs turns an existence probe's "no row" into the port error that names what is missing.
func missingAs(err, missing error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return missing
	}
	return err
}

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
