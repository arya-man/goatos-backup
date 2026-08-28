package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// Clock In / Clock Out repository (docs/features/clock-in-out/plan.md).
//
// RecordClockPunch is ONE transaction: idempotency reservation, auto-close of
// the member's stale open days, event insert, entry insert/close, idempotency
// completion. The entry table is the events' owned read model, so keeping both
// writes in one transaction is the atomic transition + read-model rule, not an
// optimization.
//
// projection-review: producer grain = one workforce_clock_events row per punch
// (append-only). Consumer grain = one workforce_clock_entries row per
// (tenant_id, workforce_member_id, business_date), enforced by
// workforce_clock_entries_day_uq — the event->entry join is 1:1 by
// construction, and worked_minutes' numerator and denominator range over that
// single entry row. The presence read below LEFT JOINs workforce_members
// (grain: one row per active member) to entries on the FULL unique key
// (tenant_id, workforce_member_id, business_date), so it cannot fan out.

// clockEntrySnapshot is the idempotency snapshot: the raw pairing row this
// punch produced, replayed verbatim on an exact retry.
type clockEntrySnapshot struct {
	Entry ports.ClockEntryRow `json:"entry"`
}

func (r *Repository) RecordClockPunch(ctx context.Context, cmd ports.ClockPunchCommand) (ports.ClockPunchRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.ClockPunchRecord{}, err
	}
	defer rollback(ctx, tx)

	scope := "clock_" + cmd.EventType
	fingerprint := requestFingerprint(
		cmd.TenantID, cmd.WorkforceMemberID, cmd.EventType, cmd.BusinessDate,
		cmd.CapturedAt.UTC().Format(time.RFC3339),
	)
	reservation, err := reserveIdempotency(ctx, tx, cmd.TenantID, scope, cmd.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.ClockPunchRecord{}, err
	}
	if !reservation.proceed {
		var snap clockEntrySnapshot
		if len(reservation.snapshot) == 0 {
			return ports.ClockPunchRecord{}, ports.ErrNotFound
		}
		if err := json.Unmarshal(reservation.snapshot, &snap); err != nil {
			return ports.ClockPunchRecord{}, err
		}
		return ports.ClockPunchRecord{Entry: snap.Entry, Replayed: true}, nil
	}

	// Self-healing auto-close (maintainer decision D4): the member's stale
	// open days from BEFORE this punch's business day close now, with NO
	// invented end time — worked_minutes stays NULL forever.
	if _, err := tx.Exec(ctx, `
UPDATE workforce_clock_entries
SET status = 'auto_closed', row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid
  AND workforce_member_id = $2::uuid
  AND status = 'open'
  AND business_date < $3::date`,
		cmd.TenantID, cmd.WorkforceMemberID, cmd.BusinessDate); err != nil {
		return ports.ClockPunchRecord{}, err
	}

	locationMissing := cmd.Location.Status != "captured"
	offline := cmd.NetworkType == "offline_queued"

	metadata := map[string]any{}
	if len(cmd.Integrity.MockProviderPackages) > 0 {
		metadata["mock_provider_packages"] = cmd.Integrity.MockProviderPackages
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return ports.ClockPunchRecord{}, err
	}

	var eventID string
	if err := tx.QueryRow(ctx, `
INSERT INTO workforce_clock_events (
  tenant_id, workforce_member_id, user_id, event_type, business_date,
  captured_at, recorded_at, clock_skew_ms,
  location_status, latitude, longitude, gps_accuracy_m, address,
  mock_location, mock_provider_packages, developer_options_enabled,
  device_id, app_install_id, app_version, app_version_code, build_type,
  os_version, sdk_version, device_model, network_type, battery_pct, metadata
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4, $5::date,
  $6, now(), $7,
  $8, $9, $10, $11, nullif($12, ''),
  $13, $14, $15,
  nullif($16, '')::uuid, nullif($17, ''), nullif($18, ''), nullif($19, ''), nullif($20, ''),
  nullif($21, ''), nullif($22, ''), nullif($23, ''), $24, $25, $26::jsonb
)
RETURNING clock_event_id::text`,
		cmd.TenantID, cmd.WorkforceMemberID, cmd.UserID, cmd.EventType, cmd.BusinessDate,
		cmd.CapturedAt, cmd.ClockSkewMs,
		cmd.Location.Status, cmd.Location.Latitude, cmd.Location.Longitude, cmd.Location.AccuracyM, cmd.Location.Address,
		cmd.Integrity.MockLocation, cmd.Integrity.MockProviderPackages, cmd.Integrity.DeveloperOptionsEnabled,
		cmd.DeviceID, cmd.AppInstallID, cmd.AppVersion, cmd.AppVersionCode, cmd.BuildType,
		cmd.OSVersion, cmd.SDKVersion, cmd.DeviceModel, cmd.NetworkType, cmd.BatteryPct, metaJSON,
	).Scan(&eventID); err != nil {
		return ports.ClockPunchRecord{}, err
	}

	// Lock the day row so a racing in+out pair for the same person serializes.
	var existing ports.ClockEntryRow
	var existingOut *time.Time
	haveExisting := true
	err = tx.QueryRow(ctx, `
SELECT clock_entry_id::text, status, clock_in_at, clock_out_at, worked_minutes,
       offline_punch, location_missing
FROM workforce_clock_entries
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND business_date = $3::date
FOR UPDATE`,
		cmd.TenantID, cmd.WorkforceMemberID, cmd.BusinessDate,
	).Scan(&existing.ClockEntryID, &existing.Status, &existing.ClockInAt, &existingOut,
		&existing.WorkedMinutes, &existing.OfflinePunch, &existing.LocationMissing)
	if errors.Is(err, pgx.ErrNoRows) {
		haveExisting = false
	} else if err != nil {
		return ports.ClockPunchRecord{}, err
	}
	existing.ClockOutAt = existingOut

	var entry ports.ClockEntryRow
	switch cmd.EventType {
	case "clock_in":
		if haveExisting {
			return ports.ClockPunchRecord{}, ports.ErrAlreadyClockedIn
		}
		if err := tx.QueryRow(ctx, `
INSERT INTO workforce_clock_entries (
  tenant_id, workforce_member_id, business_date, clock_in_event_id,
  clock_in_at, status, offline_punch, location_missing
) VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, $5, 'open', $6, $7)
RETURNING clock_entry_id::text`,
			cmd.TenantID, cmd.WorkforceMemberID, cmd.BusinessDate, eventID,
			cmd.EffectiveAt, offline, locationMissing,
		).Scan(&entry.ClockEntryID); err != nil {
			return ports.ClockPunchRecord{}, err
		}
		entry.WorkforceMemberID = cmd.WorkforceMemberID
		entry.BusinessDate = cmd.BusinessDate
		entry.Status = "open"
		entry.ClockInAt = cmd.EffectiveAt
		entry.OfflinePunch = offline
		entry.LocationMissing = locationMissing
	case "clock_out":
		if !haveExisting {
			return ports.ClockPunchRecord{}, ports.ErrNotClockedIn
		}
		if existing.Status != "open" {
			return ports.ClockPunchRecord{}, ports.ErrAlreadyClockedOut
		}
		worked := int(cmd.EffectiveAt.Sub(existing.ClockInAt) / time.Minute)
		if worked < 0 {
			// A skewed offline device clock cannot produce negative hours;
			// the honest floor is zero and the skew column carries the story.
			worked = 0
		}
		if err := tx.QueryRow(ctx, `
UPDATE workforce_clock_entries
SET clock_out_event_id = $4::uuid, clock_out_at = $5, worked_minutes = $6,
    status = 'closed',
    offline_punch = offline_punch OR $7,
    location_missing = location_missing OR $8,
    row_version = row_version + 1, updated_at = now()
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND business_date = $3::date
RETURNING clock_entry_id::text`,
			cmd.TenantID, cmd.WorkforceMemberID, cmd.BusinessDate, eventID,
			cmd.EffectiveAt, worked, offline, locationMissing,
		).Scan(&entry.ClockEntryID); err != nil {
			return ports.ClockPunchRecord{}, err
		}
		entry = existing
		entry.WorkforceMemberID = cmd.WorkforceMemberID
		entry.BusinessDate = cmd.BusinessDate
		entry.Status = "closed"
		out := cmd.EffectiveAt
		entry.ClockOutAt = &out
		entry.WorkedMinutes = &worked
		entry.OfflinePunch = existing.OfflinePunch || offline
		entry.LocationMissing = existing.LocationMissing || locationMissing
	default:
		return ports.ClockPunchRecord{}, ports.ErrInvalidFilter
	}

	if err := completeIdempotency(ctx, tx, cmd.TenantID, scope, cmd.IdempotencyKey,
		"clock_entry", entry.ClockEntryID, clockEntrySnapshot{Entry: entry}); err != nil {
		return ports.ClockPunchRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.ClockPunchRecord{}, err
	}
	return ports.ClockPunchRecord{Entry: entry}, nil
}

func (r *Repository) ClockDayForMember(ctx context.Context, params ports.ClockStatusParams) (*ports.ClockEntryRow, []ports.ClockEntryRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := params.RecentLimit
	if limit <= 0 || limit > 60 {
		limit = 14
	}
	rows, err := r.pool.Query(ctx, `
SELECT clock_entry_id::text, workforce_member_id::text, business_date::text, status,
       clock_in_at, clock_out_at, worked_minutes, offline_punch, location_missing
FROM workforce_clock_entries
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid
  AND business_date <= $3::date
ORDER BY business_date DESC
LIMIT $4`,
		params.TenantID, params.WorkforceMemberID, params.BusinessDate, limit+1)
	if err != nil {
		return nil, nil, err
	}
	entries, err := scanClockEntries(rows)
	if err != nil {
		return nil, nil, err
	}
	var day *ports.ClockEntryRow
	recent := make([]ports.ClockEntryRow, 0, len(entries))
	for i := range entries {
		if entries[i].BusinessDate == params.BusinessDate {
			e := entries[i]
			day = &e
			continue
		}
		recent = append(recent, entries[i])
	}
	if len(recent) > limit {
		recent = recent[:limit]
	}
	return day, recent, nil
}

func scanClockEntries(rows pgx.Rows) ([]ports.ClockEntryRow, error) {
	defer rows.Close()
	items := []ports.ClockEntryRow{}
	for rows.Next() {
		var e ports.ClockEntryRow
		if err := rows.Scan(&e.ClockEntryID, &e.WorkforceMemberID, &e.BusinessDate, &e.Status,
			&e.ClockInAt, &e.ClockOutAt, &e.WorkedMinutes, &e.OfflinePunch, &e.LocationMissing); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

// clockPresenceWhere is the shared filter for the page AND the summary, so a
// tile can never advertise people the list hides. Cursor/limit/bucket are page
// concerns and are deliberately NOT part of it: the tiles always show all
// buckets of the current park/designation/search filter.
// The name search is a staff-directory-sized LIKE (hundreds of rows per
// tenant, never herd-scale), the same shape as the baselined ListPeople search
// one file over; the marker below suppresses non-sargable-like for the literal.
const clockPresenceWhere = /* scale-guard:ignore: staff-directory-sized search */ `
FROM workforce_members wm
LEFT JOIN workforce_clock_entries e
  ON e.tenant_id = wm.tenant_id
 AND e.workforce_member_id = wm.workforce_member_id
 AND e.business_date = $2::date
LEFT JOIN workforce_clock_events ein
  ON ein.tenant_id = e.tenant_id AND ein.clock_event_id = e.clock_in_event_id
LEFT JOIN locations l
  ON l.tenant_id = wm.tenant_id AND l.location_id = wm.primary_location_id
LEFT JOIN departments d
  ON d.tenant_id = wm.tenant_id AND d.department_id = wm.department_id
WHERE wm.tenant_id = $1::uuid
  AND wm.status = 'active'
  AND ($3 = '' OR wm.primary_location_id = $3::uuid)
  AND ($4 = '' OR wm.primary_role_hint = $4)
  AND (
    $5 = ''
    OR lower(wm.display_name) LIKE '%' || lower($5) || '%' -- scale-guard:ignore: staff-directory-sized search, same shape as the baselined ListPeople search
  )`

const clockBucketExpr = `
CASE
  WHEN e.clock_entry_id IS NULL THEN 'not_clocked_in'
  WHEN e.status = 'open' THEN 'working'
  ELSE 'clocked_out'
END`

const clockFlaggedExpr = `
(e.clock_entry_id IS NOT NULL AND (
   e.offline_punch OR e.location_missing OR e.status = 'auto_closed'
   OR (e.status = 'open' AND e.business_date < $6::date)))`

func (r *Repository) ListClockPresence(ctx context.Context, params ports.ClockPresenceParams) (ports.ClockPresencePage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cursorName, cursorID := "", ""
	if strings.TrimSpace(params.Cursor) != "" {
		var ok bool
		cursorName, cursorID, ok = decodePeopleCursor(params.Cursor)
		if !ok {
			return ports.ClockPresencePage{}, ports.ErrInvalidFilter
		}
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 25
	}
	today := biztime.BusinessDate(time.Now())

	page := ports.ClockPresencePage{}
	// Whole-filter summary first — never derived from the page.
	err := r.pool.QueryRow(ctx, `
SELECT
  count(*) FILTER (WHERE `+clockBucketExpr+` = 'working'),
  count(*) FILTER (WHERE `+clockBucketExpr+` = 'clocked_out'),
  count(*) FILTER (WHERE `+clockBucketExpr+` = 'not_clocked_in'),
  count(*) FILTER (WHERE `+clockFlaggedExpr+`)
`+clockPresenceWhere,
		params.TenantID, params.BusinessDate, params.ParkID, params.RoleHint, params.Search, today,
	).Scan(&page.Summary.Working, &page.Summary.ClockedOut, &page.Summary.NotClockedIn, &page.Summary.Flagged)
	if err != nil {
		return ports.ClockPresencePage{}, err
	}

	// scale-guard:ignore: non-sargable-like — staff-directory-sized search over
	// workforce_members (hundreds of rows per tenant), the same shape as the
	// baselined ListPeople search one file over.
	rows, err := r.pool.Query(ctx, `
SELECT
  wm.workforce_member_id::text,
  wm.display_name,
  wm.primary_role_hint,
  COALESCE(wm.hr_designation_grade, ''),
  COALESCE(wm.primary_location_id::text, ''),
  COALESCE(l.name, ''),
  COALESCE(d.label, ''),
  e.clock_entry_id::text,
  e.business_date::text,
  e.status,
  e.clock_in_at,
  e.clock_out_at,
  e.worked_minutes,
  e.offline_punch,
  e.location_missing,
  COALESCE(ein.address, ''),
  COALESCE(ein.device_model, ''),
  COALESCE(ein.app_version, '')
`+clockPresenceWhere+`
  AND ($7 = '' OR (
        ($7 = 'flagged' AND `+clockFlaggedExpr+`)
     OR ($7 <> 'flagged' AND `+clockBucketExpr+` = $7)))
  AND ($8 = '' OR (lower(wm.display_name), wm.workforce_member_id::text) > ($8, $9))
ORDER BY lower(wm.display_name), wm.workforce_member_id
LIMIT $10`,
		params.TenantID, params.BusinessDate, params.ParkID, params.RoleHint, params.Search,
		today, params.Bucket, cursorName, cursorID, limit+1)
	if err != nil {
		return ports.ClockPresencePage{}, err
	}
	defer rows.Close()
	items := []ports.ClockPresenceRawRow{}
	for rows.Next() {
		var (
			row       ports.ClockPresenceRawRow
			entryID   *string
			entryDate *string
			status    *string
			inAt      *time.Time
			outAt     *time.Time
			worked    *int
			offline   *bool
			locMiss   *bool
			address   string
			deviceMdl string
			appVer    string
		)
		if err := rows.Scan(&row.WorkforceMemberID, &row.PersonName, &row.RoleHint,
			&row.DesignationGrade, &row.ParkID, &row.ParkLabel, &row.DepartmentLabel,
			&entryID, &entryDate, &status, &inAt, &outAt, &worked, &offline, &locMiss,
			&address, &deviceMdl, &appVer); err != nil {
			return ports.ClockPresencePage{}, err
		}
		if entryID != nil && inAt != nil {
			row.Entry = &ports.ClockEntryRow{
				ClockEntryID:      *entryID,
				WorkforceMemberID: row.WorkforceMemberID,
				BusinessDate:      derefString(entryDate),
				Status:            derefString(status),
				ClockInAt:         *inAt,
				ClockOutAt:        outAt,
				WorkedMinutes:     worked,
				OfflinePunch:      offline != nil && *offline,
				LocationMissing:   locMiss != nil && *locMiss,
				Address:           address,
				DeviceModel:       deviceMdl,
				AppVersion:        appVer,
			}
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return ports.ClockPresencePage{}, err
	}
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		page.NextCursor = encodePeopleCursor(last.PersonName, last.WorkforceMemberID)
	}
	page.Rows = items
	return page, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (r *Repository) ClockPersonDayDetail(ctx context.Context, tenantID, workforceMemberID, businessDate string) (ports.ClockPersonDay, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out := ports.ClockPersonDay{}
	err := r.pool.QueryRow(ctx, `
SELECT wm.workforce_member_id::text, wm.display_name, wm.primary_role_hint,
       COALESCE(wm.hr_designation_grade, ''), COALESCE(wm.primary_location_id::text, ''),
       COALESCE(l.name, ''), COALESCE(d.label, '')
FROM workforce_members wm
LEFT JOIN locations l ON l.tenant_id = wm.tenant_id AND l.location_id = wm.primary_location_id
LEFT JOIN departments d ON d.tenant_id = wm.tenant_id AND d.department_id = wm.department_id
WHERE wm.tenant_id = $1::uuid AND wm.workforce_member_id = $2::uuid`,
		tenantID, workforceMemberID,
	).Scan(&out.Person.WorkforceMemberID, &out.Person.PersonName, &out.Person.RoleHint,
		&out.Person.DesignationGrade, &out.Person.ParkID, &out.Person.ParkLabel, &out.Person.DepartmentLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ClockPersonDay{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.ClockPersonDay{}, err
	}

	day, recent, err := r.ClockDayForMember(ctx, ports.ClockStatusParams{
		TenantID: tenantID, WorkforceMemberID: workforceMemberID,
		BusinessDate: businessDate, RecentLimit: 7,
	})
	if err != nil {
		return ports.ClockPersonDay{}, err
	}
	out.Entry = day
	out.RecentDays = recent

	events, err := r.clockEventsForDay(ctx, tenantID, workforceMemberID, businessDate)
	if err != nil {
		return ports.ClockPersonDay{}, err
	}
	out.Events = events
	return out, nil
}

func (r *Repository) ClockEntryDetail(ctx context.Context, tenantID, clockEntryID string) (ports.ClockPersonDay, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var memberID, businessDate string
	err := r.pool.QueryRow(ctx, `
SELECT workforce_member_id::text, business_date::text
FROM workforce_clock_entries
WHERE tenant_id = $1::uuid AND clock_entry_id = $2::uuid`,
		tenantID, clockEntryID).Scan(&memberID, &businessDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ClockPersonDay{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.ClockPersonDay{}, err
	}
	return r.ClockPersonDayDetail(ctx, tenantID, memberID, businessDate)
}

func (r *Repository) clockEventsForDay(ctx context.Context, tenantID, workforceMemberID, businessDate string) ([]ports.ClockEventRow, error) {
	rows, err := r.pool.Query(ctx, `
SELECT clock_event_id::text, event_type, business_date::text, captured_at, recorded_at,
       clock_skew_ms, location_status, latitude, longitude, gps_accuracy_m,
       COALESCE(address, ''), mock_location, developer_options_enabled,
       COALESCE(device_model, ''), COALESCE(app_version, ''), COALESCE(os_version, ''),
       network_type, battery_pct
FROM workforce_clock_events
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND business_date = $3::date
ORDER BY recorded_at`,
		tenantID, workforceMemberID, businessDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ports.ClockEventRow{}
	for rows.Next() {
		var e ports.ClockEventRow
		var battery *int16
		if err := rows.Scan(&e.ClockEventID, &e.EventType, &e.BusinessDate, &e.CapturedAt, &e.RecordedAt,
			&e.ClockSkewMs, &e.LocationStatus, &e.Latitude, &e.Longitude, &e.GpsAccuracyM,
			&e.Address, &e.MockLocation, &e.DeveloperOptionsEnabled,
			&e.DeviceModel, &e.AppVersion, &e.OSVersion, &e.NetworkType, &battery); err != nil {
			return nil, err
		}
		if battery != nil {
			b := int(*battery)
			e.BatteryPct = &b
		}
		items = append(items, e)
	}
	return items, rows.Err()
}
