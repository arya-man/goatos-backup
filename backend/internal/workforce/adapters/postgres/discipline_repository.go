package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// HRMS violations and enquiries (maintainer decisions 2026-09-30, migration 000458). Staff-sized
// reads: every query is one tenant, optionally narrowed to a park list, keyset-paged where it
// lists, with 1:1 joins only (member -> person_access -> designation; member -> park; recorder
// via a LATERAL LIMIT 1).
//
// projection-review: membership=workforce_violations of ONE tenant whose occurred_on falls in the filter's month and whose park is in the caller's park scope, status as filtered; group_key=workforce_member_id for the by-person totals, none for the summary; join_cardinality=each violation row joins its member (PK), that member's person_access (PK) and designation (PK) and its park (PK), so no violation is counted twice; pagination=the summary and the totals are whole-filter aggregates taken apart from the keyset violation page; scope=explicit tenant + park_id list + occurred_on range, the same predicate the list pages.

const violationSelect = `
SELECT v.violation_id::text, v.workforce_member_id::text, wm.display_name,
       COALESCE(dc.label, ''), COALESCE(wm.primary_role_hint, ''), COALESCE(wm.hr_designation_grade, ''),
       COALESCE(v.park_id::text, ''), COALESCE(l.name, ''),
       v.type_key, v.type_label, v.fine_rupees, v.occurred_on, v.note, v.source,
       COALESCE(v.enquiry_id::text, ''), COALESCE(rb.display_name, ''), v.recorded_at,
       v.status, COALESCE(v.withdraw_reason, ''), v.sop_version, v.row_version
FROM workforce_violations v
JOIN workforce_members wm ON wm.workforce_member_id = v.workforce_member_id
LEFT JOIN person_access pa ON pa.tenant_id = wm.tenant_id AND pa.workforce_member_id = wm.workforce_member_id
LEFT JOIN designation_catalog dc ON dc.designation_code = pa.designation_code AND dc.status = 'active'
LEFT JOIN locations l ON l.tenant_id = v.tenant_id AND l.location_id = v.park_id
LEFT JOIN LATERAL (
  SELECT x.display_name FROM workforce_members x
  WHERE x.tenant_id = v.tenant_id AND x.user_id = v.recorded_by
  ORDER BY (x.status = 'active') DESC, x.updated_at DESC LIMIT 1
) rb ON true
`

// The shared filter: $1 tenant, $2 all-parks flag, $3 park list, $4 from, $5 to, $6 status.
const violationFilterWhere = `
WHERE v.tenant_id = $1::uuid
  AND ($2::boolean OR v.park_id = ANY($3::uuid[]))
  AND v.occurred_on >= $4::date AND v.occurred_on < $5::date
  AND ($6 = '' OR v.status = $6)
`

const sqlViolationList = violationSelect + violationFilterWhere + `
  AND ($7 = '' OR (v.occurred_on, v.violation_id::text) < ($7::date, $8))
ORDER BY v.occurred_on DESC, v.violation_id DESC
LIMIT $9`

const sqlViolationSummary = `
SELECT count(*)::int, COALESCE(sum(v.fine_rupees), 0)::int, count(DISTINCT v.workforce_member_id)::int
FROM workforce_violations v` + violationFilterWhere

const sqlViolationTotals = `
SELECT v.workforce_member_id::text, min(wm.display_name), COALESCE(min(dc.label), ''),
       COALESCE(min(wm.primary_role_hint), ''), COALESCE(min(wm.hr_designation_grade), ''),
       COALESCE(min(l.name), ''), count(*)::int, COALESCE(sum(v.fine_rupees), 0)::int
FROM workforce_violations v
JOIN workforce_members wm ON wm.workforce_member_id = v.workforce_member_id
LEFT JOIN person_access pa ON pa.tenant_id = wm.tenant_id AND pa.workforce_member_id = wm.workforce_member_id
LEFT JOIN designation_catalog dc ON dc.designation_code = pa.designation_code AND dc.status = 'active'
LEFT JOIN locations l ON l.tenant_id = wm.tenant_id AND l.location_id = wm.primary_location_id
` + violationFilterWhere + `
GROUP BY v.workforce_member_id
ORDER BY sum(v.fine_rupees) DESC, count(*) DESC, min(wm.display_name)
LIMIT $7`

const sqlViolationByID = violationSelect + `WHERE v.tenant_id = $1::uuid AND v.violation_id = $2::uuid`

const sqlViolationsForEnquiry = violationSelect + `
WHERE v.tenant_id = $1::uuid AND v.enquiry_id = $2::uuid
ORDER BY v.recorded_at, v.violation_id`

const sqlPersonOptions = `
SELECT wm.workforce_member_id::text, wm.display_name,
       COALESCE(dc.label, ''), COALESCE(wm.primary_role_hint, ''), COALESCE(wm.hr_designation_grade, ''),
       COALESCE(wm.primary_location_id::text, ''), COALESCE(l.name, '')
FROM workforce_members wm
LEFT JOIN person_access pa ON pa.tenant_id = wm.tenant_id AND pa.workforce_member_id = wm.workforce_member_id
LEFT JOIN designation_catalog dc ON dc.designation_code = pa.designation_code AND dc.status = 'active'
LEFT JOIN locations l ON l.tenant_id = wm.tenant_id AND l.location_id = wm.primary_location_id
WHERE wm.tenant_id = $1::uuid AND wm.status = 'active'
  AND ($2::boolean OR wm.primary_location_id = ANY($3::uuid[]))
ORDER BY lower(wm.display_name), wm.workforce_member_id
LIMIT 1000`

const sqlMemberForViolation = `
SELECT COALESCE(primary_location_id::text, '') FROM workforce_members
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND status = 'active'
FOR SHARE`

const sqlViolationInsert = `
INSERT INTO workforce_violations (
  tenant_id, workforce_member_id, park_id, type_key, type_label, fine_rupees, occurred_on, note,
  source, enquiry_id, sop_version, recorded_by, idempotency_key, request_fingerprint)
VALUES ($1::uuid, $2::uuid, NULLIF($3, '')::uuid, $4, $5, $6, $7::date, $8,
  $9, NULLIF($10, '')::uuid, $11, $12::uuid, NULLIF($13, ''), NULLIF($14, ''))
ON CONFLICT (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
RETURNING violation_id::text`

const sqlViolationByKey = `
SELECT violation_id::text, COALESCE(request_fingerprint, '')
FROM workforce_violations WHERE tenant_id = $1::uuid AND idempotency_key = $2`

const sqlViolationWithdraw = `
UPDATE workforce_violations
SET status = 'withdrawn', withdrawn_by = $3::uuid, withdrawn_at = now(), withdraw_reason = $4,
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND violation_id = $2::uuid AND status = 'recorded' AND row_version = $5`

const enquirySelect = `
SELECT e.enquiry_id::text, e.trigger_key, e.subject_type, e.subject_id::text, e.park_id::text,
       COALESCE(l.name, ''), e.subject_label, e.occurred_at, e.opened_at, e.due_at, e.sop_version,
       e.status, e.answers, COALESCE(sb.display_name, ''), e.submitted_at,
       (SELECT count(*)::int FROM workforce_violations v WHERE v.enquiry_id = e.enquiry_id AND v.status = 'recorded'),
       e.row_version
FROM workforce_enquiries e
LEFT JOIN locations l ON l.tenant_id = e.tenant_id AND l.location_id = e.park_id
LEFT JOIN LATERAL (
  SELECT x.display_name FROM workforce_members x
  WHERE x.tenant_id = e.tenant_id AND x.user_id = e.submitted_by
  ORDER BY (x.status = 'active') DESC, x.updated_at DESC LIMIT 1
) sb ON true
`

// $1 tenant, $2 all-parks, $3 parks, $4 status filter, $5 now.
const enquiryFilterWhere = `
WHERE e.tenant_id = $1::uuid
  AND ($2::boolean OR e.park_id = ANY($3::uuid[]))
  AND ($4 = ''
    OR ($4 = 'open' AND e.status = 'open')
    OR ($4 = 'overdue' AND e.status = 'open' AND e.due_at < $5::timestamptz)
    OR ($4 = 'submitted' AND e.status = 'submitted'))
`

const sqlEnquiryList = enquirySelect + enquiryFilterWhere + `
  AND ($6 = '' OR (e.opened_at, e.enquiry_id::text) < ($6::timestamptz, $7))
ORDER BY e.opened_at DESC, e.enquiry_id DESC
LIMIT $8`

const sqlEnquirySummary = `
SELECT count(*) FILTER (WHERE e.status = 'open')::int,
       count(*) FILTER (WHERE e.status = 'open' AND e.due_at < $4::timestamptz)::int,
       count(*) FILTER (WHERE e.status = 'submitted')::int
FROM workforce_enquiries e
WHERE e.tenant_id = $1::uuid AND ($2::boolean OR e.park_id = ANY($3::uuid[]))`

const sqlEnquiryByID = enquirySelect + `WHERE e.tenant_id = $1::uuid AND e.enquiry_id = $2::uuid`

const sqlEnquiryLock = `
SELECT status, row_version FROM workforce_enquiries
WHERE tenant_id = $1::uuid AND enquiry_id = $2::uuid FOR UPDATE`

const sqlEnquirySubmit = `
UPDATE workforce_enquiries
SET status = 'submitted', answers = $3::jsonb, submitted_by = $4::uuid, submitted_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND enquiry_id = $2::uuid AND status = 'open' AND row_version = $5`

const sqlEnquiryInsert = `
INSERT INTO workforce_enquiries (tenant_id, trigger_key, subject_type, subject_id, park_id, subject_label,
  occurred_at, opened_at, due_at, sop_version)
VALUES ($1::uuid, $2, $3, $4::uuid, $5::uuid, $6, $7::timestamptz, $8::timestamptz,
        $8::timestamptz + make_interval(hours => $9::int), $10::int)
ON CONFLICT (tenant_id, trigger_key, subject_type, subject_id) DO NOTHING
RETURNING enquiry_id::text`

// The animal's tag for the enquiry line: its first RFID, else any active identifier.
const sqlGoatTag = `
SELECT identifier_value FROM goat_identifiers
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND status = 'active'
ORDER BY (identifier_type = 'animal_identifier_1') DESC, (identifier_type = 'animal_identifier_2') DESC, identifier_type
LIMIT 1`

// The animal's own pen at death: its partition row names the pen inside the shed.
const sqlGoatPartition = `
SELECT COALESCE(partition_label, '') FROM goat_shed_partitions
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`

const sqlShedName = `
SELECT COALESCE(NULLIF(name, ''), location_code, '') FROM locations
WHERE tenant_id = $1::uuid AND location_id = $2::uuid`

const sqlParkHeadParks = `
SELECT DISTINCT (CASE WHEN g.scope_type = 'park' THEN g.scope_id ELSE m.primary_location_id END)::text
FROM user_scope_grants g
LEFT JOIN workforce_members m
  ON m.tenant_id = g.tenant_id AND m.user_id = g.user_id AND m.status = 'active'
WHERE g.tenant_id = $1::uuid AND g.user_id = $2::uuid AND g.role = 'park_head' AND g.status = 'active'
  AND (g.valid_to IS NULL OR g.valid_to > now())
  AND (CASE WHEN g.scope_type = 'park' THEN g.scope_id ELSE m.primary_location_id END) IS NOT NULL`

const sqlMemberHomePark = `
SELECT COALESCE(primary_location_id::text, '') FROM workforce_members
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid`

func parkScopeArgs(parks []string) (bool, []string) {
	if parks == nil {
		return true, []string{}
	}
	return false, parks
}

func encodeKeyset(a, b string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(a + "\x1f" + b))
}

func decodeKeyset(cursor string) (string, string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(cursor))
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(raw), "\x1f", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func scanViolations(rows pgx.Rows) ([]ports.ViolationRow, error) {
	defer rows.Close()
	out := []ports.ViolationRow{}
	for rows.Next() {
		var v ports.ViolationRow
		if err := rows.Scan(&v.ViolationID, &v.PersonID, &v.PersonName, &v.DesignationLabel, &v.RoleHint, &v.DesignationGrade,
			&v.ParkID, &v.ParkLabel, &v.TypeKey, &v.TypeLabel, &v.FineRupees, &v.OccurredOn, &v.Note, &v.Source,
			&v.EnquiryID, &v.RecordedByName, &v.RecordedAt, &v.Status, &v.WithdrawReason, &v.SOPVersion, &v.RowVersion); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) ListViolations(ctx context.Context, f ports.ViolationFilter, cursor string, limit int) ([]ports.ViolationRow, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	curDate, curID := "", ""
	if strings.TrimSpace(cursor) != "" {
		var ok bool
		if curDate, curID, ok = decodeKeyset(cursor); !ok {
			return nil, "", ports.ErrInvalidFilter
		}
	}
	all, parks := parkScopeArgs(f.ParkIDs)
	rows, err := r.pool.Query(ctx, sqlViolationList, f.TenantID, all, parks, f.From, f.To, f.Status, curDate, curID, limit+1)
	if err != nil {
		return nil, "", err
	}
	items, err := scanViolations(rows)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeKeyset(last.OccurredOn.Format("2006-01-02"), last.ViolationID)
	}
	return items, next, nil
}

func (r *Repository) ViolationSummary(ctx context.Context, f ports.ViolationFilter) (ports.ViolationSummaryRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	all, parks := parkScopeArgs(f.ParkIDs)
	var out ports.ViolationSummaryRow
	err := r.pool.QueryRow(ctx, sqlViolationSummary, f.TenantID, all, parks, f.From, f.To, f.Status).Scan(&out.Count, &out.FineRupees, &out.People)
	return out, err
}

func (r *Repository) ViolationTotalsByPerson(ctx context.Context, f ports.ViolationFilter, limit int) ([]ports.ViolationTotalRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	all, parks := parkScopeArgs(f.ParkIDs)
	rows, err := r.pool.Query(ctx, sqlViolationTotals, f.TenantID, all, parks, f.From, f.To, f.Status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.ViolationTotalRow{}
	for rows.Next() {
		var t ports.ViolationTotalRow
		if err := rows.Scan(&t.PersonID, &t.PersonName, &t.DesignationLabel, &t.RoleHint, &t.DesignationGrade, &t.ParkLabel, &t.Count, &t.FineRupees); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) PersonOptions(ctx context.Context, tenantID string, parkIDs []string) ([]ports.PersonOptionRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	all, parks := parkScopeArgs(parkIDs)
	rows, err := r.pool.Query(ctx, sqlPersonOptions, tenantID, all, parks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.PersonOptionRow{}
	for rows.Next() {
		var p ports.PersonOptionRow
		if err := rows.Scan(&p.PersonID, &p.Name, &p.DesignationLabel, &p.RoleHint, &p.DesignationGrade, &p.ParkID, &p.ParkLabel); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// insertViolationTx inserts one violation inside tx. source/enquiryID say where it came from.
func insertViolationTx(ctx context.Context, tx pgx.Tx, tenantID, actorUserID, source, enquiryID string, v ports.NewViolation) (string, bool, error) {
	var park string
	if err := missingAs(tx.QueryRow(ctx, sqlMemberForViolation, tenantID, v.PersonID).Scan(&park), ports.ErrPersonNotFound); err != nil {
		return "", false, err
	}
	var id string
	err := tx.QueryRow(ctx, sqlViolationInsert, tenantID, v.PersonID, park, v.TypeKey, v.TypeLabel, v.FineRupees,
		v.OccurredOn.Format("2006-01-02"), v.Note, source, enquiryID, v.SOPVersion, actorUserID, v.IdempotencyKey, v.Fingerprint).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, mapWriteErr(err)
	}
	// The key was used before: an exact replay returns the original, anything else is refused.
	var existing, fingerprint string
	if err := tx.QueryRow(ctx, sqlViolationByKey, tenantID, v.IdempotencyKey).Scan(&existing, &fingerprint); err != nil {
		return "", false, err
	}
	if fingerprint != v.Fingerprint {
		return "", false, ports.ErrIdempotencyConflict
	}
	return existing, false, nil
}

func (r *Repository) RecordViolation(ctx context.Context, tenantID, actorUserID string, v ports.NewViolation) (ports.ViolationRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.ViolationRow{}, err
	}
	defer rollback(ctx, tx)
	id, created, err := insertViolationTx(ctx, tx, tenantID, actorUserID, "manual", "", v)
	if err != nil {
		return ports.ViolationRow{}, err
	}
	if created {
		scope := "tenant"
		if err := insertAudit(ctx, tx, tenantID, actorUserID, "workforce.violation.recorded", "workforce_violation", id, &scope, map[string]any{
			"person_id": v.PersonID, "type_key": v.TypeKey, "fine_rupees": v.FineRupees,
			"occurred_on": v.OccurredOn.Format("2006-01-02"), "sop_version": v.SOPVersion, "source": "manual",
		}); err != nil {
			return ports.ViolationRow{}, err
		}
	}
	row, err := violationByIDTx(ctx, tx, tenantID, id)
	if err != nil {
		return ports.ViolationRow{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.ViolationRow{}, err
	}
	return row, nil
}

func violationByIDTx(ctx context.Context, tx pgx.Tx, tenantID, id string) (ports.ViolationRow, error) {
	rows, err := tx.Query(ctx, sqlViolationByID, tenantID, id)
	if err != nil {
		return ports.ViolationRow{}, err
	}
	items, err := scanViolations(rows)
	if err != nil {
		return ports.ViolationRow{}, err
	}
	if len(items) == 0 {
		return ports.ViolationRow{}, ports.ErrNotFound
	}
	return items[0], nil
}

func (r *Repository) WithdrawViolation(ctx context.Context, tenantID, actorUserID, violationID, reason string, rowVersion int) (ports.ViolationRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.ViolationRow{}, err
	}
	defer rollback(ctx, tx)
	current, err := violationByIDTx(ctx, tx, tenantID, violationID)
	if err != nil {
		return ports.ViolationRow{}, err
	}
	// Exact replay: already withdrawn for this very reason.
	if current.Status == "withdrawn" && current.WithdrawReason == reason {
		return current, tx.Commit(ctx)
	}
	tag, err := tx.Exec(ctx, sqlViolationWithdraw, tenantID, violationID, actorUserID, reason, rowVersion)
	if err != nil {
		return ports.ViolationRow{}, mapWriteErr(err)
	}
	if tag.RowsAffected() != 1 {
		return ports.ViolationRow{}, ports.ErrViolationVersionConflict
	}
	scope := "tenant"
	if err := insertAudit(ctx, tx, tenantID, actorUserID, "workforce.violation.withdrawn", "workforce_violation", violationID, &scope, map[string]any{
		"reason": reason, "person_id": current.PersonID, "fine_rupees": current.FineRupees,
	}); err != nil {
		return ports.ViolationRow{}, err
	}
	row, err := violationByIDTx(ctx, tx, tenantID, violationID)
	if err != nil {
		return ports.ViolationRow{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.ViolationRow{}, err
	}
	return row, nil
}

func (r *Repository) ViolationsForEnquiry(ctx context.Context, tenantID, enquiryID string) ([]ports.ViolationRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlViolationsForEnquiry, tenantID, enquiryID)
	if err != nil {
		return nil, err
	}
	return scanViolations(rows)
}

func (r *Repository) OpenEnquiry(ctx context.Context, cmd ports.OpenEnquiryCommand) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer rollback(ctx, tx)
	label, err := enquirySubjectLabel(ctx, tx, cmd)
	if err != nil {
		return false, err
	}
	var id string
	err = tx.QueryRow(ctx, sqlEnquiryInsert, cmd.TenantID, cmd.TriggerKey, cmd.SubjectType, cmd.SubjectID, cmd.ParkID, label,
		cmd.OccurredAt, cmd.OpenedAt, cmd.DueHours, cmd.SOPVersion).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Already opened for this subject: a replayed event opens nothing.
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, mapWriteErr(err)
	}
	if err := insertSystemAudit(ctx, tx, cmd.TenantID, "workforce.enquiry.opened", "workforce_enquiry", id, map[string]any{
		"trigger": cmd.TriggerKey, "subject_id": cmd.SubjectID, "park_id": cmd.ParkID, "sop_version": cmd.SOPVersion,
	}); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// enquirySubjectLabel is the enquiry's line: the animal's tag and its pen ("1234567 · Castro 1").
// A fact that cannot be resolved is left out rather than shown as an id.
func enquirySubjectLabel(ctx context.Context, tx pgx.Tx, cmd ports.OpenEnquiryCommand) (string, error) {
	var parts []string
	var tag string
	if err := tx.QueryRow(ctx, sqlGoatTag, cmd.TenantID, cmd.SubjectID).Scan(&tag); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if strings.TrimSpace(tag) != "" {
		parts = append(parts, strings.TrimSpace(tag))
	}
	if cmd.ShedID != "" {
		var shed, partition string
		if err := tx.QueryRow(ctx, sqlShedName, cmd.TenantID, cmd.ShedID).Scan(&shed); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		if err := tx.QueryRow(ctx, sqlGoatPartition, cmd.TenantID, cmd.SubjectID).Scan(&partition); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		if pen := (oploc.OperationalLocation{ShedName: shed, PartitionLabel: partition}).Display(); strings.TrimSpace(pen) != "" {
			parts = append(parts, pen)
		}
	}
	return strings.Join(parts, " · "), nil
}

func scanEnquiries(rows pgx.Rows) ([]ports.EnquiryRow, error) {
	defer rows.Close()
	out := []ports.EnquiryRow{}
	for rows.Next() {
		var e ports.EnquiryRow
		var answers []byte
		if err := rows.Scan(&e.EnquiryID, &e.TriggerKey, &e.SubjectType, &e.SubjectID, &e.ParkID, &e.ParkLabel, &e.SubjectLabel,
			&e.OccurredAt, &e.OpenedAt, &e.DueAt, &e.SOPVersion, &e.Status, &answers, &e.SubmittedByName, &e.SubmittedAt,
			&e.PenaltyCount, &e.RowVersion); err != nil {
			return nil, err
		}
		e.Answers = map[string]any{}
		if len(answers) > 0 {
			if err := json.Unmarshal(answers, &e.Answers); err != nil {
				return nil, err
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) ListEnquiries(ctx context.Context, f ports.EnquiryFilter) ([]ports.EnquiryRow, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	curAt, curID := "", ""
	if strings.TrimSpace(f.Cursor) != "" {
		var ok bool
		if curAt, curID, ok = decodeKeyset(f.Cursor); !ok {
			return nil, "", ports.ErrInvalidFilter
		}
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 25
	}
	all, parks := parkScopeArgs(f.ParkIDs)
	rows, err := r.pool.Query(ctx, sqlEnquiryList, f.TenantID, all, parks, f.Status, f.Now, curAt, curID, limit+1)
	if err != nil {
		return nil, "", err
	}
	items, err := scanEnquiries(rows)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeKeyset(last.OpenedAt.UTC().Format(time.RFC3339Nano), last.EnquiryID)
	}
	return items, next, nil
}

func (r *Repository) EnquirySummary(ctx context.Context, f ports.EnquiryFilter) (int, int, int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	all, parks := parkScopeArgs(f.ParkIDs)
	var open, overdue, submitted int
	err := r.pool.QueryRow(ctx, sqlEnquirySummary, f.TenantID, all, parks, f.Now).Scan(&open, &overdue, &submitted)
	return open, overdue, submitted, err
}

func (r *Repository) GetEnquiry(ctx context.Context, tenantID, enquiryID string) (ports.EnquiryRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlEnquiryByID, tenantID, enquiryID)
	if err != nil {
		return ports.EnquiryRow{}, err
	}
	items, err := scanEnquiries(rows)
	if err != nil {
		return ports.EnquiryRow{}, err
	}
	if len(items) == 0 {
		return ports.EnquiryRow{}, ports.ErrNotFound
	}
	return items[0], nil
}

// SubmitEnquiry records the report and every violation it names in ONE transaction: a report
// that says who was responsible can never land without their violations, nor the reverse.
func (r *Repository) SubmitEnquiry(ctx context.Context, cmd ports.SubmitEnquiryCommand) (ports.EnquiryRow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.EnquiryRow{}, err
	}
	defer rollback(ctx, tx)
	var status string
	var version int
	if err := missingAs(tx.QueryRow(ctx, sqlEnquiryLock, cmd.TenantID, cmd.EnquiryID).Scan(&status, &version), ports.ErrNotFound); err != nil {
		return ports.EnquiryRow{}, err
	}
	if status != "open" {
		return ports.EnquiryRow{}, ports.ErrEnquiryNotOpen
	}
	if version != cmd.RowVersion {
		return ports.EnquiryRow{}, ports.ErrEnquiryVersionConflict
	}
	for _, p := range cmd.Penalties {
		if _, _, err := insertViolationTx(ctx, tx, cmd.TenantID, cmd.ActorUserID, "enquiry", cmd.EnquiryID, p); err != nil {
			return ports.EnquiryRow{}, err
		}
	}
	answers, err := json.Marshal(cmd.Answers)
	if err != nil {
		return ports.EnquiryRow{}, err
	}
	tag, err := tx.Exec(ctx, sqlEnquirySubmit, cmd.TenantID, cmd.EnquiryID, string(answers), cmd.ActorUserID, cmd.RowVersion)
	if err != nil {
		return ports.EnquiryRow{}, mapWriteErr(err)
	}
	if tag.RowsAffected() != 1 {
		return ports.EnquiryRow{}, ports.ErrEnquiryVersionConflict
	}
	scope := "tenant"
	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorUserID, "workforce.enquiry.submitted", "workforce_enquiry", cmd.EnquiryID, &scope, map[string]any{
		"penalties": len(cmd.Penalties),
	}); err != nil {
		return ports.EnquiryRow{}, err
	}
	rows, err := tx.Query(ctx, sqlEnquiryByID, cmd.TenantID, cmd.EnquiryID)
	if err != nil {
		return ports.EnquiryRow{}, err
	}
	items, err := scanEnquiries(rows)
	if err != nil {
		return ports.EnquiryRow{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.EnquiryRow{}, err
	}
	return items[0], nil
}

func (r *Repository) ParkHeadParks(ctx context.Context, tenantID, userID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlParkHeadParks, tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *Repository) MemberHomePark(ctx context.Context, tenantID, personID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var park string
	err := r.pool.QueryRow(ctx, sqlMemberHomePark, tenantID, personID).Scan(&park)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrPersonNotFound
	}
	return park, err
}

// insertSystemAudit records a write the SYSTEM made (an enquiry opened by a farm event): no human
// actor, the same audit_log row shape insertAudit writes for a person.
func insertSystemAudit(ctx context.Context, tx pgx.Tx, tenantID, action, resourceType, resourceID string, metadata map[string]any) error {
	raw, err := json.Marshal(nonNilMap(metadata))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit_log (tenant_id, actor_id, actor_type, action, resource_type, resource_id, scope_type, scope_id, metadata)
VALUES ($1::uuid, NULL, 'system', $2, $3, $4::uuid, 'tenant', $1::uuid, $5::jsonb)`,
		tenantID, action, resourceType, resourceID, string(raw))
	return err
}
