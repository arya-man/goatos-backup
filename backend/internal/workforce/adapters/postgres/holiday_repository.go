package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// WEEKLY OFFS AND HOLIDAYS (maintainer decisions 2026-09-30, migration 000473): the days the
// clock-in check never checks. A person's weekly offs sit on their shift row; holidays are dates
// HR enters for every park or one park.

const sqlMemberWeekOffsUpdate = `
UPDATE workforce_member_shifts
SET week_offs = $3::smallint[], updated_by = $4::uuid, row_version = row_version + 1
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND row_version = $5`

const sqlMemberWeekOffsCurrent = `
SELECT week_offs::int[], row_version FROM workforce_member_shifts
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid
FOR UPDATE`

// $1 tenant, $2 park ('' = only every-park holidays are asked about), $3 from, $4 to.
const sqlHolidays = `
SELECT h.holiday_id::text, to_char(h.holiday_on, 'YYYY-MM-DD'), COALESCE(h.park_id::text, ''), COALESCE(l.name, ''),
       h.label, h.row_version
FROM workforce_holidays h
LEFT JOIN locations l ON l.tenant_id = h.tenant_id AND l.location_id = h.park_id
WHERE h.tenant_id = $1::uuid AND h.status = 'active'
  AND h.holiday_on >= $3::date AND h.holiday_on <= $4::date
  AND (h.park_id IS NULL OR ($2 <> '' AND h.park_id = NULLIF($2, '')::uuid))
ORDER BY h.holiday_on, l.name NULLS FIRST
LIMIT 200`

const sqlHolidayInsert = `
INSERT INTO workforce_holidays (tenant_id, holiday_on, park_id, label, created_by)
VALUES ($1::uuid, $2::date, NULLIF($3, '')::uuid, $4, $5::uuid)
ON CONFLICT (tenant_id, holiday_on, COALESCE(park_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE status = 'active'
DO NOTHING
RETURNING holiday_id::text`

const sqlHolidayExisting = `
SELECT holiday_id::text FROM workforce_holidays
WHERE tenant_id = $1::uuid AND holiday_on = $2::date AND status = 'active'
  AND COALESCE(park_id, '00000000-0000-0000-0000-000000000000'::uuid) = COALESCE(NULLIF($3, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)`

const sqlHolidayByID = `
SELECT h.holiday_id::text, to_char(h.holiday_on, 'YYYY-MM-DD'), COALESCE(h.park_id::text, ''), COALESCE(l.name, ''),
       h.label, h.row_version
FROM workforce_holidays h
LEFT JOIN locations l ON l.tenant_id = h.tenant_id AND l.location_id = h.park_id
WHERE h.tenant_id = $1::uuid AND h.holiday_id = $2::uuid`

const sqlHolidayRemove = `
UPDATE workforce_holidays
SET status = 'removed', removed_by = $3::uuid, removed_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND holiday_id = $2::uuid AND status = 'active'`

func (r *Repository) SetMemberWeekOffs(ctx context.Context, tenantID, actorUserID, personID string, weekOffs []int, rowVersion int) (ports.TimetablePersonRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.TimetablePersonRow{}, err
	}
	defer rollback(ctx, tx)
	var current []int
	var version int
	if err := tx.QueryRow(ctx, sqlMemberWeekOffsCurrent, tenantID, personID).Scan(&current, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.TimetablePersonRow{}, ports.ErrNoShift
		}
		return ports.TimetablePersonRow{}, err
	}
	// Exact replay: already these days.
	if sameDays(current, weekOffs) {
		row, err := timetablePersonIn(ctx, tx, tenantID, personID)
		if err != nil {
			return ports.TimetablePersonRow{}, err
		}
		return row, tx.Commit(ctx)
	}
	if version != rowVersion {
		return ports.TimetablePersonRow{}, ports.ErrTimetableVersionConflict
	}
	tag, err := tx.Exec(ctx, sqlMemberWeekOffsUpdate, tenantID, personID, weekOffs, actorUserID, rowVersion)
	if err != nil {
		return ports.TimetablePersonRow{}, mapWriteErr(err)
	}
	if tag.RowsAffected() != 1 {
		return ports.TimetablePersonRow{}, ports.ErrTimetableVersionConflict
	}
	scope := "tenant"
	if err := insertAudit(ctx, tx, tenantID, actorUserID, "workforce.week_offs.set", "workforce_member", personID, &scope, map[string]any{
		"week_offs": weekOffs, "previous": current,
	}); err != nil {
		return ports.TimetablePersonRow{}, err
	}
	row, err := timetablePersonIn(ctx, tx, tenantID, personID)
	if err != nil {
		return ports.TimetablePersonRow{}, err
	}
	return row, tx.Commit(ctx)
}

func sameDays(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[int]bool{}
	for _, d := range a {
		seen[d] = true
	}
	for _, d := range b {
		if !seen[d] {
			return false
		}
	}
	return true
}

func scanHolidays(rows pgx.Rows) ([]ports.HolidayRow, error) {
	defer rows.Close()
	out := []ports.HolidayRow{}
	for rows.Next() {
		var h ports.HolidayRow
		if err := rows.Scan(&h.HolidayID, &h.HolidayOn, &h.ParkID, &h.ParkLabel, &h.Label, &h.RowVersion); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *Repository) Holidays(ctx context.Context, tenantID, parkID, from, to string) ([]ports.HolidayRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlHolidays, tenantID, parkID, from, to)
	if err != nil {
		return nil, err
	}
	return scanHolidays(rows)
}

// AddHoliday enters one holiday; entering the same date for the same scope again returns the
// existing one (idempotent on the natural key).
func (r *Repository) AddHoliday(ctx context.Context, tenantID, actorUserID, holidayOn, parkID, label string) (ports.HolidayRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.HolidayRow{}, err
	}
	defer rollback(ctx, tx)
	var id string
	err = tx.QueryRow(ctx, sqlHolidayInsert, tenantID, holidayOn, parkID, label, actorUserID).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, sqlHolidayExisting, tenantID, holidayOn, parkID).Scan(&id); err != nil {
			return ports.HolidayRow{}, err
		}
	case err != nil:
		return ports.HolidayRow{}, mapWriteErr(err)
	default:
		scope := "tenant"
		if err := insertAudit(ctx, tx, tenantID, actorUserID, "workforce.holiday.added", "workforce_holiday", id, &scope, map[string]any{
			"holiday_on": holidayOn, "park_id": parkID, "label": label,
		}); err != nil {
			return ports.HolidayRow{}, err
		}
	}
	rows, err := tx.Query(ctx, sqlHolidayByID, tenantID, id)
	if err != nil {
		return ports.HolidayRow{}, err
	}
	items, err := scanHolidays(rows)
	if err != nil {
		return ports.HolidayRow{}, err
	}
	if len(items) == 0 {
		return ports.HolidayRow{}, ports.ErrNotFound
	}
	return items[0], tx.Commit(ctx)
}

// RemoveHoliday takes a holiday off (kept on record). Removing one already removed is a no-op.
func (r *Repository) RemoveHoliday(ctx context.Context, tenantID, actorUserID, holidayID string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	tag, err := tx.Exec(ctx, sqlHolidayRemove, tenantID, holidayID, actorUserID)
	if err != nil {
		return mapWriteErr(err)
	}
	if tag.RowsAffected() == 1 {
		scope := "tenant"
		if err := insertAudit(ctx, tx, tenantID, actorUserID, "workforce.holiday.removed", "workforce_holiday", holidayID, &scope, nil); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
