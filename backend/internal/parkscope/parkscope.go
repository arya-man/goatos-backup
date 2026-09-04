package parkscope

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ONE SOURCE FOR "WHICH PARK" (maintainer decision 2026-09-04).
//
// A person's park scope used to be answered by three records that nothing kept in step:
// the scope on each user_scope_grants row, the People screen's person_access.scope_mode +
// person_park_scope ticks, and workforce_members.primary_location_id. Vaccination trusted
// the home park, weighing trusted grants, the capability resolver trusted the ticks -- and
// on 2026-09-01 a Channapatna operator claimed Coimbatore milk work because a grant had
// been added to him without touching either of the other two.
//
// The ticks are now the ONLY authored answer. Grants and the home park are DERIVED from
// them, inside the same transaction that writes the ticks, by the helpers in this package
// (shared by the People editor, person creation, the operators grant API, the login-time
// email claim and the STG seeder -- every writer of a grant row). No
// other code may choose a scope for a grant row; a writer that needs a grant asks for the
// ROLE and lets the person's scope decide where it applies.
//
// What is derived, exactly:
//
//   - scope_mode = 'tenant'  -> every role the person holds gets ONE active tenant-scoped
//                              row; park rows for those roles are revoked.
//   - scope_mode = 'parks'   -> every role gets one active row PER TICKED PARK; tenant rows
//                              and rows for unticked parks are revoked.
//
// Roles are NOT decided here. The set of roles is whatever active rows the person already
// holds (plus any role the caller is adding in the same write); this helper only moves
// those roles onto the person's scope. Rows scoped to something other than tenant/park
// (shed, cohort, custodian_party, farm) are left alone: they are not park membership.
//
// A person with no login (workforce_members.user_id NULL) has no grant rows to derive and
// is skipped; their ticks still bind the moment a login is attached.

// Result reports what the derivation changed, for audit rows and seed output.
type Result struct {
	Inserted int
	Revoked  int
}

// SyncGrantScope rewrites the active tenant/park grant rows of one user so they equal
// roles x scope. `roles` may add roles the caller is granting in this same write; roles
// already active on the user are always kept.
func SyncGrantScope(ctx context.Context, tx pgx.Tx, tenantID, userID, actorID, scopeMode string, parkIDs []string, roles []string) (Result, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Result{}, nil
	}
	scopeTypes, scopeIDs, err := DesiredScopes(tenantID, scopeMode, parkIDs)
	if err != nil {
		return Result{}, err
	}
	// Roles are read ONCE, before anything is revoked. Reading them again inside the insert
	// would see the rows the revoke just closed and derive nothing -- a person moved to
	// tenant mode would come out holding no roles at all.
	roleRows, err := tx.Query(ctx, `
SELECT DISTINCT g.role FROM user_scope_grants g
 WHERE g.tenant_id = $1::uuid AND g.user_id = $2::uuid AND g.status = 'active'
   AND g.scope_type IN ('tenant', 'park')
UNION
SELECT unnest($3::text[])`, tenantID, userID, roles)
	if err != nil {
		return Result{}, fmt.Errorf("read roles: %w", err)
	}
	allRoles := make([]string, 0, 4)
	for roleRows.Next() {
		var role string
		if err := roleRows.Scan(&role); err != nil {
			roleRows.Close()
			return Result{}, err
		}
		if strings.TrimSpace(role) != "" {
			allRoles = append(allRoles, role)
		}
	}
	roleRows.Close()
	if err := roleRows.Err(); err != nil {
		return Result{}, err
	}
	if len(allRoles) == 0 {
		return Result{}, nil
	}
	// Set-based on both sides: one statement revokes every active tenant/park row that is
	// not in roles x scope, one statement inserts every missing pair. A per-role loop would
	// be the N+1 this repo bans, and would also let a crash between two roles leave a person
	// half-moved.
	var revoked int
	if err := tx.QueryRow(ctx, `
WITH desired AS (
  SELECT r.role, s.scope_type, s.scope_id
    FROM unnest($3::text[]) AS r(role)
    CROSS JOIN unnest($4::text[], $5::uuid[]) AS s(scope_type, scope_id)
),
revoked AS (
  UPDATE user_scope_grants g
     SET status = 'revoked', valid_to = COALESCE(g.valid_to, now())
   WHERE g.tenant_id = $1::uuid AND g.user_id = $2::uuid AND g.status = 'active'
     AND g.scope_type IN ('tenant', 'park')
     AND NOT EXISTS (
       SELECT 1 FROM desired d
        WHERE d.role = g.role AND d.scope_type = g.scope_type AND d.scope_id = g.scope_id)
   RETURNING 1
)
SELECT count(*) FROM revoked`,
		tenantID, userID, allRoles, scopeTypes, scopeIDs).Scan(&revoked); err != nil {
		return Result{}, fmt.Errorf("revoke grants outside scope: %w", err)
	}
	var inserted int
	if err := tx.QueryRow(ctx, `
WITH desired AS (
  SELECT r.role, s.scope_type, s.scope_id
    FROM unnest($3::text[]) AS r(role)
    CROSS JOIN unnest($4::text[], $5::uuid[]) AS s(scope_type, scope_id)
),
inserted AS (
  INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from, created_by)
  SELECT $1::uuid, $2::uuid, d.role, d.scope_type, d.scope_id, 'active', now(), nullif($6, '')::uuid
    FROM desired d
   WHERE NOT EXISTS (
     SELECT 1 FROM user_scope_grants g
      WHERE g.tenant_id = $1::uuid AND g.user_id = $2::uuid AND g.status = 'active'
        AND g.role = d.role AND g.scope_type = d.scope_type AND g.scope_id = d.scope_id)
  RETURNING 1
)
SELECT count(*) FROM inserted`,
		tenantID, userID, allRoles, scopeTypes, scopeIDs, actorID).Scan(&inserted); err != nil {
		return Result{}, fmt.Errorf("insert grants for scope: %w", err)
	}
	return Result{Inserted: inserted, Revoked: revoked}, nil
}

// DesiredScopes turns a scope mode into the (scope_type, scope_id) pairs every role row
// must carry. Parallel arrays because they are bound as two typed arrays and zipped by
// unnest in SQL; both are built from the same list in the same loop, so they cannot drift.
func DesiredScopes(tenantID, scopeMode string, parkIDs []string) ([]string, []string, error) {
	switch strings.TrimSpace(scopeMode) {
	case "tenant":
		return []string{"tenant"}, []string{tenantID}, nil
	case "parks", "":
		if len(parkIDs) == 0 {
			return nil, nil, errors.New("park scope with no parks: nothing to derive")
		}
		types := make([]string, 0, len(parkIDs))
		ids := make([]string, 0, len(parkIDs))
		seen := make(map[string]struct{}, len(parkIDs))
		for _, id := range parkIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			types = append(types, "park")
			ids = append(ids, id)
		}
		return types, ids, nil
	default:
		return nil, nil, fmt.Errorf("unknown scope mode %q", scopeMode)
	}
}

// WritePersonScope replaces the person's ticks and home park in one go. It is the
// ONE writer of person_access.scope_mode, person_park_scope and
// workforce_members.primary_location_id, and it always finishes by deriving the grant
// rows, so a caller cannot write the ticks and forget the grants.
//
// homeParkID: in 'parks' mode it must be one of parkIDs (validated by the service); in
// 'tenant' mode it is optional and records where a director sits. Empty leaves the column
// NULL.
//
// designation: the editor records which job title pre-filled the ticks; every other
// caller passes nil and the stored value is kept; a pointer to "" clears it.
func WritePersonScope(ctx context.Context, tx pgx.Tx, tenantID, actorID, memberID, scopeMode, homeParkID string, parkIDs []string, designation *string, addRoles []string) (Result, error) {
	if scopeMode == "tenant" {
		parkIDs = nil
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO person_access (tenant_id, workforce_member_id, scope_mode, designation_code, updated_at, updated_by, row_version)
		 VALUES ($1::uuid, $2::uuid, $3, nullif($5, ''), now(), nullif($4, '')::uuid, 1)
		 ON CONFLICT (tenant_id, workforce_member_id)
		 DO UPDATE SET scope_mode = EXCLUDED.scope_mode,
		               designation_code = CASE WHEN $6 THEN EXCLUDED.designation_code ELSE person_access.designation_code END,
		               updated_at = now(),
		               updated_by = EXCLUDED.updated_by,
		               row_version = person_access.row_version + 1`,
		tenantID, memberID, scopeMode, actorID, designation, designation != nil); err != nil {
		return Result{}, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM person_park_scope WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid`,
		tenantID, memberID); err != nil {
		return Result{}, err
	}
	if len(parkIDs) > 0 {
		if _, err := tx.Exec(ctx,
			`INSERT INTO person_park_scope (tenant_id, workforce_member_id, park_id)
			 SELECT $1::uuid, $2::uuid, p::uuid FROM unnest($3::text[]) AS p
			 ON CONFLICT DO NOTHING`,
			tenantID, memberID, parkIDs); err != nil {
			return Result{}, err
		}
	}
	var userID *string
	if err := tx.QueryRow(ctx,
		`UPDATE workforce_members
		    SET primary_location_id = nullif($3, '')::uuid,
		        updated_at = now(),
		        row_version = row_version + 1
		  WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid
		  RETURNING user_id::text`,
		tenantID, memberID, strings.TrimSpace(homeParkID)).Scan(&userID); err != nil {
		return Result{}, fmt.Errorf("set home park: %w", err)
	}
	if userID == nil {
		return Result{}, nil
	}
	return SyncGrantScope(ctx, tx, tenantID, *userID, actorID, scopeMode, parkIDs, addRoles)
}

// PersonScope reads the person's authored scope inside a transaction. provisioned=false
// means this person has never been set up on the People screen.
func PersonScope(ctx context.Context, tx pgx.Tx, tenantID, memberID string) (scopeMode, homeParkID string, parkIDs []string, provisioned bool, err error) {
	var mode *string
	var home *string
	err = tx.QueryRow(ctx,
		`SELECT a.scope_mode, m.primary_location_id::text,
		        coalesce((SELECT array_agg(ps.park_id::text ORDER BY ps.park_id::text)
		                    FROM person_park_scope ps
		                   WHERE ps.tenant_id = m.tenant_id AND ps.workforce_member_id = m.workforce_member_id), '{}')
		   FROM workforce_members m
		   LEFT JOIN person_access a ON a.tenant_id = m.tenant_id AND a.workforce_member_id = m.workforce_member_id
		  WHERE m.tenant_id = $1::uuid AND m.workforce_member_id = $2::uuid`,
		tenantID, memberID).Scan(&mode, &home, &parkIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, false, nil
	}
	if err != nil {
		return "", "", nil, false, err
	}
	if home != nil {
		homeParkID = *home
	}
	if mode == nil {
		return "", homeParkID, parkIDs, false, nil
	}
	return *mode, homeParkID, parkIDs, true, nil
}

// ReconcileUser re-derives one user's grant rows from their authored scope, for a writer
// that only knows the user (the login-time email claim) after it has added a role. A user
// whose person has never been set up on the People screen is left as written: there is no
// authored scope to derive from yet, and the backfill / editor will bind them.
func ReconcileUser(ctx context.Context, tx pgx.Tx, tenantID, userID, actorID string) (Result, bool, error) {
	var memberID string
	err := tx.QueryRow(ctx,
		`SELECT workforce_member_id::text FROM workforce_members
		  WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active'
		  ORDER BY created_at ASC LIMIT 1`,
		tenantID, userID).Scan(&memberID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	scopeMode, _, parkIDs, provisioned, err := PersonScope(ctx, tx, tenantID, memberID)
	if err != nil || !provisioned {
		return Result{}, false, err
	}
	if scopeMode != "tenant" && len(parkIDs) == 0 {
		// Set up with no park ticked: nothing can be derived, and inventing a park would
		// widen someone. Leave the rows and let the editor's own validation catch it.
		return Result{}, false, nil
	}
	res, err := SyncGrantScope(ctx, tx, tenantID, userID, actorID, scopeMode, parkIDs, nil)
	return res, true, err
}
