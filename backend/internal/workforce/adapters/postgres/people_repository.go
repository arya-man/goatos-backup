package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// People/HRMS directory adapter: the cross-park member list behind
// GET /admin/workforce/people and the single-transaction create behind
// POST /admin/workforce/people.
//
// projection-review: grain = workforce_member (one row per member).
//   producer unique columns:  workforce_members (tenant_id, workforce_member_id PK)
//   consumer match columns:   LEFT JOIN locations ON (tenant_id, location_id PK)   -> 1:1
//                             LEFT JOIN departments ON (tenant_id, department_id PK) -> 1:1
//   Both joins land on their target's primary key, so the list can never fan
//   out or collapse members.
//   Proof-stat aggregate: LEFT JOIN LATERAL over verification_items pre-
//   aggregates the many side to ONE row per member BEFORE the join (GROUP-free
//   FILTER counts over vi.tenant_id = wm.tenant_id AND vi.operator_id =
//   wm.user_id, served by the partial index
//   verification_items_operator_status_idx, migration 000189). Grain of the
//   counts = verification ITEM (one item = one submitted proof set), withdrawn
//   excluded everywhere, so uploads = approved + rejected + pending and the
//   rejection ratio's numerator and denominator range over the same key set
//   (this member's non-withdrawn items). operator_id carries the USER id (the
//   auth actor recorded at enqueue), which is why the join key is wm.user_id
//   and never wm.workforce_member_id.

const peopleCursorSeparator = "\x1f"

func encodePeopleCursor(displayName, memberID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.ToLower(displayName) + peopleCursorSeparator + memberID))
}

func decodePeopleCursor(cursor string) (nameKey, memberID string, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(cursor))
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(raw), peopleCursorSeparator, 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func peopleSelectSQL(where string) string {
	return `
SELECT
  wm.workforce_member_id::text,
  wm.user_id::text,
  wm.first_name,
  wm.last_name,
  wm.display_name,
  wm.email,
  wm.status,
  wm.primary_role_hint,
  wm.hr_designation_grade,
  wm.primary_location_id::text,
  l.name,
  wm.department_id::text,
  d.label,
  wm.created_at,
  wm.row_version,
  COALESCE(proof.uploads, 0),
  COALESCE(proof.approved, 0),
  COALESCE(proof.rejected, 0),
  COALESCE(proof.pending, 0)
FROM workforce_members wm
LEFT JOIN locations l
  ON l.tenant_id = wm.tenant_id AND l.location_id = wm.primary_location_id
LEFT JOIN departments d
  ON d.tenant_id = wm.tenant_id AND d.department_id = wm.department_id
LEFT JOIN LATERAL (
  SELECT
    count(*) FILTER (WHERE vi.status <> 'withdrawn') AS uploads,
    count(*) FILTER (WHERE vi.status = 'approved')   AS approved,
    count(*) FILTER (WHERE vi.status = 'rejected')   AS rejected,
    count(*) FILTER (WHERE vi.status = 'pending')    AS pending
  FROM verification_items vi
  WHERE vi.tenant_id = wm.tenant_id
    AND vi.operator_id = wm.user_id
) proof ON true
` + where
}

func scanPeople(rows pgx.Rows) ([]domain.PersonSummary, error) {
	defer rows.Close()
	items := []domain.PersonSummary{}
	for rows.Next() {
		var (
			p         domain.PersonSummary
			createdAt time.Time
		)
		if err := rows.Scan(
			&p.PersonID,
			&p.UserID,
			&p.FirstName,
			&p.LastName,
			&p.DisplayName,
			&p.Email,
			&p.Status,
			&p.RoleHint,
			&p.DesignationGrade,
			&p.ParkID,
			&p.ParkLabel,
			&p.DepartmentID,
			&p.DepartmentLabel,
			&createdAt,
			&p.RowVersion,
			&p.ProofUploads,
			&p.ProofApproved,
			&p.ProofRejected,
			&p.ProofPending,
		); err != nil {
			return nil, err
		}
		p.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		if decided := p.ProofApproved + p.ProofRejected; decided > 0 {
			pct := int((float64(p.ProofRejected)/float64(decided))*100 + 0.5)
			p.ProofRejectionPct = &pct
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

// ListPeople pages the directory with a keyset cursor on
// (lower(display_name), workforce_member_id) — no OFFSET, bounded LIMIT.
func (r *Repository) ListPeople(ctx context.Context, params ports.ListPeopleParams) ([]domain.PersonSummary, string, error) {
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
		limit = 25
	}

	// scale-guard:ignore: non-sargable-like — the staff directory search runs over workforce_members, a staff-sized table (hundreds of rows per tenant, never herd-scale); same shape as the baselined ListOperators search.
	rows, err := r.pool.Query(ctx, peopleSelectSQL(`
WHERE wm.tenant_id = $1::uuid
  AND ($2 = '' OR wm.primary_location_id = $2::uuid)
  AND ($3 = '' OR wm.department_id = $3::uuid)
  AND ($4 = '' OR wm.status = $4)
  AND (
    $5 = ''
    OR lower(wm.display_name) LIKE '%' || lower($5) || '%'
    OR lower(coalesce(wm.email, '')) LIKE '%' || lower($5) || '%'
  )
  AND ($6 = '' OR (lower(wm.display_name), wm.workforce_member_id::text) > ($6, $7))
ORDER BY lower(wm.display_name), wm.workforce_member_id
LIMIT $8`),
		params.TenantID, params.ParkID, params.DepartmentID, params.Status,
		params.Search, cursorName, cursorID, limit+1)
	if err != nil {
		return nil, "", err
	}
	items, err := scanPeople(rows)
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

// PeopleCatalog loads the real parks and departments for the directory's
// filters and the Add Person form. Both sets are tiny (2 parks, 9 departments).
func (r *Repository) PeopleCatalog(ctx context.Context, tenantID string) (domain.PeopleCatalog, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	catalog := domain.PeopleCatalog{Parks: []domain.PeopleCatalogOption{}, Departments: []domain.PeopleCatalogOption{}}
	rows, err := r.pool.Query(ctx, `
SELECT location_id::text, location_code, name
FROM locations
WHERE tenant_id = $1::uuid AND location_type = 'park' AND status = 'active'
ORDER BY name`, tenantID)
	if err != nil {
		return domain.PeopleCatalog{}, err
	}
	for rows.Next() {
		var opt domain.PeopleCatalogOption
		if err := rows.Scan(&opt.ID, &opt.Code, &opt.Label); err != nil {
			rows.Close()
			return domain.PeopleCatalog{}, err
		}
		catalog.Parks = append(catalog.Parks, opt)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.PeopleCatalog{}, err
	}

	rows, err = r.pool.Query(ctx, `
SELECT department_id::text, code, label
FROM departments
WHERE tenant_id = $1::uuid AND status = 'active'
ORDER BY label`, tenantID)
	if err != nil {
		return domain.PeopleCatalog{}, err
	}
	for rows.Next() {
		var opt domain.PeopleCatalogOption
		if err := rows.Scan(&opt.ID, &opt.Code, &opt.Label); err != nil {
			rows.Close()
			return domain.PeopleCatalog{}, err
		}
		catalog.Departments = append(catalog.Departments, opt)
	}
	rows.Close()
	return catalog, rows.Err()
}

// CreatePerson runs the whole onboarding write as ONE transaction: idempotency
// reservation, workforce_members insert, active user_scope_grants row,
// auth_allowed_emails admission, and audit. A failure at any step rolls the
// whole person back — there is no half-created person with a grant but no
// allowlist row.
//
// The Firebase account is ensured BEFORE this call (outside the tx, naturally
// idempotent by email lookup), so a created-account-but-failed-tx retry with
// the same Idempotency-Key converges: EnsureEmailUser finds the same UID and
// this transaction completes.
func (r *Repository) CreatePerson(ctx context.Context, cmd ports.CreatePersonCommand) (domain.PersonSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.PersonSummary{}, err
	}
	defer rollback(ctx, tx)

	fingerprint := requestFingerprint(
		cmd.TenantID, cmd.NormalizedEmail, cmd.FirstName, cmd.LastName,
		cmd.Role, cmd.ScopeType, cmd.ScopeID, cmd.DepartmentID, cmd.DesignationGrade,
	)
	reservation, err := reserveIdempotency(ctx, tx, cmd.TenantID, "create_person", cmd.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.PersonSummary{}, err
	}
	if !reservation.proceed {
		person, ok, err := replayPerson(reservation)
		if err != nil {
			return domain.PersonSummary{}, err
		}
		if ok {
			return person, nil
		}
		person, err = txPerson(ctx, tx, cmd.TenantID, reservation.resultID)
		if err != nil {
			return domain.PersonSummary{}, err
		}
		return person, nil
	}

	// Duplicate probe before insert so the caller gets a specific 409 rather
	// than a generic constraint conflict.
	var existingID string
	err = tx.QueryRow(ctx, `
SELECT workforce_member_id::text
FROM workforce_members
WHERE tenant_id = $1::uuid AND lower(email) = $2
LIMIT 1`, cmd.TenantID, cmd.NormalizedEmail).Scan(&existingID)
	if err == nil {
		return domain.PersonSummary{}, ports.ErrDuplicateEmail
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.PersonSummary{}, err
	}

	var personID string
	err = tx.QueryRow(ctx, `
INSERT INTO workforce_members (
  tenant_id, user_id, display_code, display_name, first_name, last_name, email,
  status, primary_role_hint, hr_designation_grade, primary_location_id,
  department_id, metadata, created_by
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6, $7,
  'active', $8, nullif($9, ''), nullif($10, '')::uuid,
  nullif($11, '')::uuid,
  jsonb_build_object('source', 'admin_create_person'),
  $12::uuid
)
RETURNING workforce_member_id::text`,
		cmd.TenantID, cmd.UserID, "person:"+cmd.NormalizedEmail, cmd.DisplayName,
		cmd.FirstName, cmd.LastName, cmd.Email,
		cmd.RoleHint, cmd.DesignationGrade, cmd.ParkID, cmd.DepartmentID,
		cmd.ActorID).Scan(&personID)
	if err != nil {
		return domain.PersonSummary{}, mapPersonWriteErr(err)
	}

	// Active scope grant, idempotent on replayed UID: an identical active grant
	// already present (e.g. from a prior partial run) is reused, not duplicated.
	var grantID string
	err = tx.QueryRow(ctx, `
SELECT grant_id::text FROM user_scope_grants
WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND role = $3
  AND scope_type = $4 AND scope_id = $5::uuid AND status = 'active'
  AND (valid_to IS NULL OR valid_to > now())
LIMIT 1`, cmd.TenantID, cmd.UserID, cmd.Role, cmd.ScopeType, cmd.ScopeID).Scan(&grantID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from, created_by)
VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, 'active', now(), $6::uuid)
RETURNING grant_id::text`,
			cmd.TenantID, cmd.UserID, cmd.Role, cmd.ScopeType, cmd.ScopeID, cmd.ActorID).Scan(&grantID)
	}
	if err != nil {
		return domain.PersonSummary{}, mapPersonWriteErr(err)
	}

	// Allowlist admission: this is the row that lets the email through the auth
	// middleware without a Secret Manager + Cloud Run step.
	if _, err := tx.Exec(ctx, `
INSERT INTO auth_allowed_emails (tenant_id, email, normalized_email, status, source, created_by)
VALUES ($1::uuid, $2, $3, 'active', 'workforce_create_person', $4::uuid)
ON CONFLICT (tenant_id, normalized_email) WHERE status = 'active' DO NOTHING`,
		cmd.TenantID, cmd.Email, cmd.NormalizedEmail, cmd.ActorID); err != nil {
		return domain.PersonSummary{}, mapPersonWriteErr(err)
	}

	if err := insertAudit(ctx, tx, cmd.TenantID, cmd.ActorID, "workforce.person.created",
		"workforce_member", personID, &cmd.ScopeType, map[string]any{
			"scope_id":   cmd.ScopeID,
			"role":       cmd.Role,
			"email":      cmd.NormalizedEmail,
			"park_id":    cmd.ParkID,
			"department": cmd.DepartmentID,
			"grant_id":   grantID,
		}); err != nil {
		return domain.PersonSummary{}, err
	}

	person, err := txPerson(ctx, tx, cmd.TenantID, personID)
	if err != nil {
		return domain.PersonSummary{}, err
	}
	if err := completeIdempotency(ctx, tx, cmd.TenantID, "create_person", cmd.IdempotencyKey, "workforce_member", personID, person); err != nil {
		return domain.PersonSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.PersonSummary{}, err
	}
	return person, nil
}

// txPerson reads one person INSIDE the write transaction so the idempotency
// snapshot records exactly the state this transaction produced.
func txPerson(ctx context.Context, tx pgx.Tx, tenantID, personID string) (domain.PersonSummary, error) {
	rows, err := tx.Query(ctx, peopleSelectSQL(`
WHERE wm.tenant_id = $1::uuid AND wm.workforce_member_id = $2::uuid
LIMIT 1`), tenantID, personID)
	if err != nil {
		return domain.PersonSummary{}, err
	}
	items, err := scanPeople(rows)
	if err != nil {
		return domain.PersonSummary{}, err
	}
	if len(items) == 0 {
		return domain.PersonSummary{}, ports.ErrNotFound
	}
	return items[0], nil
}

// replayPerson decodes the ORIGINAL create response recorded for an exact
// replay; ok is false for a key with no snapshot (caller falls back to a read
// by result id).
func replayPerson(res idemReservation) (domain.PersonSummary, bool, error) {
	if len(res.snapshot) == 0 {
		return domain.PersonSummary{}, false, nil
	}
	var person domain.PersonSummary
	if err := json.Unmarshal(res.snapshot, &person); err != nil {
		return domain.PersonSummary{}, false, err
	}
	return person, true, nil
}

func mapPersonWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		strings.Contains(pgErr.ConstraintName, "email") {
		return ports.ErrDuplicateEmail
	}
	return mapWriteErr(err)
}
