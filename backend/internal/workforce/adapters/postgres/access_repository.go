package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// AccessRepository owns person_access / person_module_access / person_park_scope
// and the designation catalog (migration 000215).
type AccessRepository struct {
	pool *pgxpool.Pool
}

func NewAccessRepository(pool *pgxpool.Pool) *AccessRepository {
	return &AccessRepository{pool: pool}
}

var _ ports.PersonAccessRepository = (*AccessRepository)(nil)

// loadPersonSQL reads the header, the module rows and the park rows in ONE
// round trip. The two child sets are aggregated to JSON rather than joined flat:
// a flat join multiplies module rows by park rows, and de-duplicating that in Go
// is the aggregate-grain defect this repo has shipped five times.
const loadPersonSQL = `
SELECT m.display_name,
       coalesce(m.email, '')                       AS email,
       coalesce(a.designation_code, '')            AS designation_code,
       coalesce(a.scope_mode, 'parks')             AS scope_mode,
       coalesce(a.row_version, 0)                  AS row_version,
       coalesce(
         (SELECT jsonb_agg(jsonb_build_object(
                   'module', ma.module_key,
                   'surface', ma.surface,
                   'capabilities', to_jsonb(ma.capabilities),
                   'pages', to_jsonb(ma.pages))
                 ORDER BY ma.module_key, ma.surface)
            FROM person_module_access ma
           WHERE ma.tenant_id = m.tenant_id
             AND ma.workforce_member_id = m.workforce_member_id),
         '[]'::jsonb)                              AS modules,
       coalesce(
         (SELECT jsonb_agg(ps.park_id::text ORDER BY ps.park_id::text)
            FROM person_park_scope ps
           WHERE ps.tenant_id = m.tenant_id
             AND ps.workforce_member_id = m.workforce_member_id),
         '[]'::jsonb)                              AS park_ids
  FROM workforce_members m
  LEFT JOIN person_access a
         ON a.tenant_id = m.tenant_id
        AND a.workforce_member_id = m.workforce_member_id
 WHERE m.tenant_id = $1::uuid
   AND m.workforce_member_id = $2::uuid
   AND m.status = 'active'`

type moduleRowJSON struct {
	Module       string   `json:"module"`
	Surface      string   `json:"surface"`
	Capabilities []string `json:"capabilities"`
	Pages        []string `json:"pages"`
}

func (r *AccessRepository) LoadPersonAccess(ctx context.Context, tenantID, personID string) (ports.PersonAccessRecord, error) {
	var (
		rec        ports.PersonAccessRecord
		modulesRaw []byte
		parksRaw   []byte
	)
	err := r.pool.QueryRow(ctx, loadPersonSQL, tenantID, personID).Scan(
		&rec.DisplayName, &rec.Email, &rec.DesignationCode, &rec.ScopeMode, &rec.RowVersion,
		&modulesRaw, &parksRaw,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.PersonAccessRecord{}, ports.ErrPersonNotFound
	}
	if err != nil {
		return ports.PersonAccessRecord{}, err
	}
	rec.PersonID = personID

	var rows []moduleRowJSON
	if err := json.Unmarshal(modulesRaw, &rows); err != nil {
		return ports.PersonAccessRecord{}, fmt.Errorf("decode module rows: %w", err)
	}
	rec.Assignments = make([]permissions.ModuleAssignment, 0, len(rows))
	for _, row := range rows {
		rec.Assignments = append(rec.Assignments, permissions.ModuleAssignment{
			Module:       row.Module,
			Surface:      row.Surface,
			Capabilities: row.Capabilities,
			Pages:        row.Pages,
		})
	}
	if err := json.Unmarshal(parksRaw, &rec.ParkIDs); err != nil {
		return ports.PersonAccessRecord{}, fmt.Errorf("decode park scope: %w", err)
	}
	if rec.ParkIDs == nil {
		rec.ParkIDs = []string{}
	}
	return rec, nil
}

func (r *AccessRepository) SavePersonAccess(ctx context.Context, cmd ports.SavePersonAccessCommand) (ports.PersonAccessRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ports.PersonAccessRecord{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the person's header for the length of the write. Without this, two
	// admins saving at once both pass the version check and the second silently
	// wins -- on an access screen a lost update is invisible until someone cannot
	// do their job.
	var currentVersion int
	err = tx.QueryRow(ctx,
		`SELECT row_version FROM person_access
		  WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid
		  FOR UPDATE`,
		cmd.TenantID, cmd.PersonID).Scan(&currentVersion)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Never set up before. The editor sends row_version 0 for that case; any
		// other value means it was looking at a record that has since been deleted.
		if cmd.ExpectedRowVersion != 0 {
			return ports.PersonAccessRecord{}, ports.ErrAccessVersionConflict
		}
		currentVersion = 0
	case err != nil:
		return ports.PersonAccessRecord{}, err
	default:
		if cmd.ExpectedRowVersion != currentVersion {
			return ports.PersonAccessRecord{}, ports.ErrAccessVersionConflict
		}
	}

	// Reject an unknown or inactive park rather than dropping it: silently discarding a
	// park the admin selected narrows someone's scope with no notice.
	//
	// ONE round trip for the whole selection, not one per park. The column stays bare and
	// the BIND ARRAY is cast (`location_id = ANY($2::uuid[])`, never `location_id::text =
	// ANY(...)`), so the ordinary index on location_id is still usable. The error still
	// names WHICH park was refused -- the admin has to know which pill to unpick -- so the
	// valid ids come back and the missing one is found in Go.
	if len(cmd.ParkIDs) > 0 {
		rows, err := tx.Query(ctx,
			`SELECT location_id::text FROM locations
			  WHERE tenant_id = $1::uuid
			    AND location_id = ANY($2::uuid[])
			    AND location_type = 'park'
			    AND status = 'active'`,
			cmd.TenantID, cmd.ParkIDs)
		if err != nil {
			return ports.PersonAccessRecord{}, err
		}
		valid := make(map[string]struct{}, len(cmd.ParkIDs))
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return ports.PersonAccessRecord{}, err
			}
			valid[id] = struct{}{}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return ports.PersonAccessRecord{}, err
		}
		for _, parkID := range cmd.ParkIDs {
			if _, ok := valid[parkID]; !ok {
				return ports.PersonAccessRecord{}, fmt.Errorf("%w: %s", ports.ErrUnknownPark, parkID)
			}
		}
	}

	var designation *string
	if cmd.DesignationCode != "" {
		code := cmd.DesignationCode
		designation = &code
	}
	// The target must be an ACTIVE member OF THIS TENANT, checked inside the write
	// transaction rather than trusted from the request.
	//
	// tenant_id comes from the authenticated context, but person_id comes from the URL and
	// the foreign key is on workforce_member_id ALONE -- so without this a PUT could write
	// (this tenant, another tenant's member). Those rows would never resolve, because every
	// read joins on tenant AND member, but they would be persisted and AUDITED as though
	// someone's access had really been changed. An audit trail that records a change that
	// never happened is worse than no row at all.
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM workforce_members
		   WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND status = 'active')`,
		cmd.TenantID, cmd.PersonID).Scan(&exists); err != nil {
		return ports.PersonAccessRecord{}, err
	}
	if !exists {
		return ports.PersonAccessRecord{}, ports.ErrPersonNotFound
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO person_access (tenant_id, workforce_member_id, scope_mode, designation_code, updated_at, updated_by, row_version)
		 VALUES ($1::uuid, $2::uuid, $3, $4, now(), nullif($5, '')::uuid, 1)
		 ON CONFLICT (tenant_id, workforce_member_id)
		 DO UPDATE SET scope_mode = EXCLUDED.scope_mode,
		               designation_code = EXCLUDED.designation_code,
		               updated_at = now(),
		               updated_by = EXCLUDED.updated_by,
		               row_version = person_access.row_version + 1`,
		cmd.TenantID, cmd.PersonID, cmd.ScopeMode, designation, cmd.ActorID); err != nil {
		return ports.PersonAccessRecord{}, err
	}

	// Wholesale replace. The editor always sends every module it rendered, so a
	// delete-then-insert is the honest shape: an unticked module must lose its row,
	// and an upsert-only write would leave it behind.
	if _, err := tx.Exec(ctx,
		`DELETE FROM person_module_access WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid`,
		cmd.TenantID, cmd.PersonID); err != nil {
		return ports.PersonAccessRecord{}, err
	}
	if len(cmd.Assignments) > 0 {
		// One set-based insert via jsonb_to_recordset. NOT unnest() over parallel arrays:
		// unnest on a text[][] FLATTENS it to scalars, so capabilities arrived as text and
		// the insert failed 42804 -- Postgres cannot unnest an array-of-arrays into rows of
		// arrays. JSON also avoids inventing a delimiter for a value that is a set.
		//
		// jsonb_array_elements_TEXT, never jsonb_array_elements(x)::text: the latter keeps
		// JSON quoting and turns a JSON null into the 4-character string "null", which would
		// store a capability no level matches.
		rows := make([]map[string]any, 0, len(cmd.Assignments))
		for _, a := range cmd.Assignments {
			caps := a.Capabilities
			if caps == nil {
				caps = []string{}
			}
			// Pages are stored as sent, EMPTY INCLUDED: an empty list is read back as
			// "every page of this module" (permissions.PageAccessForAssignments), which is
			// what makes a page shipped tomorrow reach whoever already holds the module.
			pages := a.Pages
			if pages == nil {
				pages = []string{}
			}
			rows = append(rows, map[string]any{
				"module_key":   a.Module,
				"surface":      a.Surface,
				"capabilities": caps,
				"pages":        pages,
			})
		}
		payload, err := json.Marshal(rows)
		if err != nil {
			return ports.PersonAccessRecord{}, err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, pages, updated_at, updated_by)
			 SELECT $1::uuid, $2::uuid, r.surface, r.module_key,
			        ARRAY(SELECT jsonb_array_elements_text(r.capabilities)),
			        ARRAY(SELECT jsonb_array_elements_text(r.pages)),
			        now(), nullif($4, '')::uuid
			   FROM jsonb_to_recordset($3::jsonb) AS r(module_key text, surface text, capabilities jsonb, pages jsonb)`,
			cmd.TenantID, cmd.PersonID, payload, cmd.ActorID); err != nil {
			return ports.PersonAccessRecord{}, err
		}
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM person_park_scope WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid`,
		cmd.TenantID, cmd.PersonID); err != nil {
		return ports.PersonAccessRecord{}, err
	}
	if len(cmd.ParkIDs) > 0 {
		if _, err := tx.Exec(ctx,
			`INSERT INTO person_park_scope (tenant_id, workforce_member_id, park_id)
			 SELECT $1::uuid, $2::uuid, p::uuid FROM unnest($3::text[]) AS p
			 ON CONFLICT DO NOTHING`,
			cmd.TenantID, cmd.PersonID, cmd.ParkIDs); err != nil {
			return ports.PersonAccessRecord{}, err
		}
	}

	// Audit in the SAME transaction as the change. An access grant with no audit
	// row is exactly the record an investigation needs and cannot find.
	after, err := json.Marshal(map[string]any{
		"scope_mode":       cmd.ScopeMode,
		"designation_code": cmd.DesignationCode,
		"park_ids":         cmd.ParkIDs,
		"assignments":      cmd.Assignments,
	})
	if err != nil {
		return ports.PersonAccessRecord{}, err
	}
	// audit_log.metadata is NOT NULL with no default, so it is passed explicitly. It
	// carries the SHAPE of the change (how many module rows, how wide the scope) so an
	// investigation can read what happened without re-deriving it from after_state.
	metadata, err := json.Marshal(map[string]any{
		"module_rows": len(cmd.Assignments),
		"scope_mode":  cmd.ScopeMode,
		"park_count":  len(cmd.ParkIDs),
	})
	if err != nil {
		return ports.PersonAccessRecord{}, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO audit_log (tenant_id, actor_id, actor_type, action, resource_type, resource_id, after_state, metadata)
		 VALUES ($1::uuid, nullif($2, '')::uuid, 'user', 'person_access.replaced', 'workforce_member', $3::uuid, $4::jsonb, $5::jsonb)`,
		cmd.TenantID, cmd.ActorID, cmd.PersonID, after, metadata); err != nil {
		return ports.PersonAccessRecord{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ports.PersonAccessRecord{}, err
	}
	return r.LoadPersonAccess(ctx, cmd.TenantID, cmd.PersonID)
}

func (r *AccessRepository) ListParks(ctx context.Context, tenantID string) ([]ports.AccessCatalogOption, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT location_id::text, name FROM locations
		  WHERE tenant_id = $1::uuid AND location_type = 'park' AND status = 'active'
		  ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ports.AccessCatalogOption, 0, 8)
	for rows.Next() {
		var o ports.AccessCatalogOption
		if err := rows.Scan(&o.Code, &o.Label); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *AccessRepository) ListDesignations(ctx context.Context) ([]ports.AccessCatalogOption, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT designation_code, label, coalesce(grade, '') FROM designation_catalog
		  WHERE status = 'active' ORDER BY sort_order, label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ports.AccessCatalogOption, 0, 16)
	for rows.Next() {
		var o ports.AccessCatalogOption
		if err := rows.Scan(&o.Code, &o.Label, &o.Grade); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *AccessRepository) DesignationDefaults(ctx context.Context, code string) ([]permissions.ModuleAssignment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT module_key, surface, capabilities, pages FROM designation_module_defaults
		  WHERE designation_code = $1 ORDER BY module_key, surface`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]permissions.ModuleAssignment, 0, 32)
	for rows.Next() {
		var a permissions.ModuleAssignment
		if err := rows.Scan(&a.Module, &a.Surface, &a.Capabilities, &a.Pages); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ResolvePageAccess is the ADMIN-WEB BOOTSTRAP read: which pages this person keeps.
//
// Separate from ResolvePermissions because they answer different questions and are asked
// at different moments -- every authenticated request needs the permission set, while only
// the bootstrap composes a sidebar. Reports assigned=false for a person with no rows, and
// the caller then serves the unnarrowed contract (see adminui/app/person_page_lens.go): a
// person the backfill has not reached must not be narrowed to nothing.
func (r *AccessRepository) ResolvePageAccess(ctx context.Context, tenantID, userID string) (permissions.PageAccess, bool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT ma.module_key, ma.surface, ma.capabilities, ma.pages
		   FROM person_module_access ma
		   JOIN workforce_members m
		     ON m.tenant_id = ma.tenant_id
		    AND m.workforce_member_id = ma.workforce_member_id
		  WHERE ma.tenant_id = $1::uuid
		    AND m.user_id = $2::uuid
		    AND m.status = 'active'
		    AND ma.surface = 'web'`,
		tenantID, userID)
	if err != nil {
		return permissions.PageAccess{}, false, err
	}
	defer rows.Close()
	assignments := make([]permissions.ModuleAssignment, 0, 24)
	for rows.Next() {
		var a permissions.ModuleAssignment
		if err := rows.Scan(&a.Module, &a.Surface, &a.Capabilities, &a.Pages); err != nil {
			return permissions.PageAccess{}, false, err
		}
		assignments = append(assignments, a)
	}
	if err := rows.Err(); err != nil {
		return permissions.PageAccess{}, false, err
	}
	if len(assignments) == 0 {
		return permissions.PageAccess{}, false, nil
	}
	return permissions.PageAccessForAssignments(assignments), true, nil
}

// ResolvePermissions is the REQUEST PATH read: one indexed lookup keyed on the
// person, expanded through the catalog in Go.
//
// The expansion is deliberately NOT done in SQL. What a capability means lives in
// permissions.PermissionsForAssignments, and a second implementation in SQL is
// how the write path and the enforcement path come to disagree.
func (r *AccessRepository) ResolvePermissions(ctx context.Context, tenantID, userID string) ([]string, bool, error) {
	var (
		hasAccess bool
		rowsRaw   []byte
	)
	err := r.pool.QueryRow(ctx,
		`SELECT a.workforce_member_id IS NOT NULL AS has_access,
		        coalesce(
		          (SELECT jsonb_agg(jsonb_build_object(
		                    'module', ma.module_key,
		                    'surface', ma.surface,
		                    'capabilities', to_jsonb(ma.capabilities))
		                  ORDER BY ma.module_key, ma.surface)
		             FROM person_module_access ma
		            WHERE ma.tenant_id = m.tenant_id
		              AND ma.workforce_member_id = m.workforce_member_id),
		          '[]'::jsonb) AS modules
		   FROM workforce_members m
		   LEFT JOIN person_access a
		     ON a.tenant_id = m.tenant_id
		    AND a.workforce_member_id = m.workforce_member_id
		  WHERE m.tenant_id = $1::uuid
		    AND m.user_id = $2::uuid
		    AND m.status = 'active'`,
		tenantID, userID,
	).Scan(&hasAccess, &rowsRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return provisionedOrErr(err)
	}
	if !hasAccess {
		return nil, false, nil
	}
	var rows []moduleRowJSON
	if err := json.Unmarshal(rowsRaw, &rows); err != nil {
		return nil, true, fmt.Errorf("decode runtime module rows: %w", err)
	}
	assignments := make([]permissions.ModuleAssignment, 0, 32)
	for _, row := range rows {
		assignments = append(assignments, permissions.ModuleAssignment{
			Module:       row.Module,
			Surface:      row.Surface,
			Capabilities: row.Capabilities,
		})
	}
	return permissions.PermissionsForAssignmentsWithBaseline(assignments), true, nil
}

// ResolveParkScope is the REQUEST PATH scope read for a principal whose per-person permissions
// are deciding the route. It intentionally returns only the header mode and selected parks; the
// capability question stays in ResolvePermissions/permissions.
func (r *AccessRepository) ResolveParkScope(ctx context.Context, tenantID, userID string) (string, []string, bool, error) {
	var (
		scopeMode string
		parksRaw  []byte
	)
	err := r.pool.QueryRow(ctx,
		`SELECT coalesce(a.scope_mode, 'parks') AS scope_mode,
		        coalesce(
		          (SELECT jsonb_agg(ps.park_id::text ORDER BY ps.park_id::text)
		             FROM person_park_scope ps
		            WHERE ps.tenant_id = m.tenant_id
		              AND ps.workforce_member_id = m.workforce_member_id),
		          '[]'::jsonb) AS park_ids
		   FROM workforce_members m
		   JOIN person_access a
		     ON a.tenant_id = m.tenant_id
		    AND a.workforce_member_id = m.workforce_member_id
		  WHERE m.tenant_id = $1::uuid
		    AND m.user_id = $2::uuid
		    AND m.status = 'active'`,
		tenantID, userID,
	).Scan(&scopeMode, &parksRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, false, nil
	}
	if err != nil {
		if isUndefinedTable(err) {
			return "", nil, false, nil
		}
		return "", nil, true, err
	}
	var parkIDs []string
	if err := json.Unmarshal(parksRaw, &parkIDs); err != nil {
		return "", nil, true, fmt.Errorf("decode runtime park scope: %w", err)
	}
	if parkIDs == nil {
		parkIDs = []string{}
	}
	return scopeMode, parkIDs, true, nil
}

// provisionedOrErr separates "this deployment has not created the access tables yet" from
// every other read failure, because the two are handled OPPOSITELY by the caller.
//
// The request path fails CLOSED on a read error: a person whose ticks deliberately REMOVED
// a role-derived permission would otherwise get that authority back from a transient
// database blip -- fail-open on exactly the path this model exists to control.
//
// An undefined table is not a blip. It is the deploy window before the migration has run,
// and failing closed there would 403 the entire farm. Postgres names that case precisely
// (SQLSTATE 42P01), so it is reported as "not provisioned" and the caller takes the role
// path, exactly as it does for a person with no rows.
func provisionedOrErr(err error) ([]string, bool, error) {
	if isUndefinedTable(err) {
		return nil, false, nil
	}
	return nil, true, err
}

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		return true
	}
	return false
}
