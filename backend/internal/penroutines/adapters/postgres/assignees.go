package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// ROLE RESOLUTION (2026-09-17 revision, docs/decisions/pen-routines.md): a routine is for ROLES,
// and whoever holds one of them FOR THE ROUTINE'S PARK owes its tasks. This file is the ONE
// place that question is answered; every read that needs it -- the phone list and its counts,
// one task (and so the submit's assignee check), the badge, the kernel's digest recipients, the
// Work Board owner and the authoring preview -- composes RoleHoldersFromSQL. Do not restate the
// coverage rule anywhere else: two copies drift, and a drift here hands a park head the other
// park's work.
//
// A user holds a role for park P when they have an ACTIVE grant (status 'active', valid_to null
// or future) with that role, on an ACTIVE workforce_members row (which carries the user_id), and
// the grant covers P:
//
//	scope_type = 'park'   AND scope_id = P
//	scope_type = 'tenant' AND role <> 'park_head'                       -- a director covers every park
//	scope_type = 'tenant' AND role =  'park_head' AND primary_location_id = P
//
// The last line is the farm's two park heads: both hold park_head at TENANT scope on the live
// data, so tenant scope alone would hand each of them both parks' tasks. A tenant-scoped park
// head covers only the park his HRMS profile names; one with no home park covers none.
//
// Cardinality: workforce_members carries a partial unique index on (tenant_id, user_id) WHERE
// status = 'active', so the member join is at most 1:1 per grant; a user may still hold several
// matching grants, so every consumer either EXISTS over this fragment or DISTINCTs the user.

// RoleHoldersFromSQL returns a FROM ... WHERE fragment over user_scope_grants rg joined to its
// active workforce_members rm, restricted to grants of one of `roles` covering `park` in
// `tenant`. Each argument is a SQL expression (a column of the enclosing query or a bind), so
// the caller composes it as `SELECT ... <fragment> [AND ...]` or `EXISTS (SELECT 1 <fragment>)`.
// rg.user_id, rg.role and rm.display_name are the columns a consumer selects.
func RoleHoldersFromSQL(tenant, roles, park string) string {
	return `
FROM user_scope_grants rg
JOIN workforce_members rm
  ON rm.tenant_id = rg.tenant_id AND rm.user_id = rg.user_id AND rm.status = 'active'
WHERE rg.tenant_id = ` + tenant + `
  AND rg.status = 'active'
  AND (rg.valid_to IS NULL OR rg.valid_to > now())
  AND rg.role = ANY(` + roles + `)
  AND ((rg.scope_type = 'park' AND rg.scope_id = ` + park + `)
    OR (rg.scope_type = 'tenant' AND rg.role <> 'park_head')
    OR (rg.scope_type = 'tenant' AND rg.role = 'park_head' AND rm.primary_location_id = ` + park + `))`
}

// RoutineRolesSQL is the assignee_roles of the routine a pen_routine_tasks row (aliased by
// taskAlias) belongs to, as a scalar array subquery, for reads that do not already join the
// definition. The trailing ::text[] cast is load-bearing: without it `= ANY((SELECT ...))` parses
// as the SUBQUERY form of ANY and compares a role to the whole array (text = text[]).
func RoutineRolesSQL(taskAlias string) string {
	return `(SELECT prd.assignee_roles FROM pen_routine_definitions prd WHERE prd.tenant_id = ` + taskAlias + `.tenant_id AND prd.routine_id = ` + taskAlias + `.routine_id)::text[]`
}

// CallerHoldsRoutineRoleSQL is the row predicate "the user bound at userBind holds one of this
// task's routine roles for the task's park", as an EXISTS semijoin (a user holding several
// matching grants never multiplies the task row). The task's park IS its routine's park.
func CallerHoldsRoutineRoleSQL(taskAlias, userBind string) string {
	return `EXISTS (SELECT 1 ` + RoleHoldersFromSQL(taskAlias+".tenant_id", RoutineRolesSQL(taskAlias), taskAlias+".park_id") + `
  AND rg.user_id = ` + userBind + `)`
}

// RoutinesHeldBySQL is the predicate "the task's routine is one whose roles the user bound at
// userBind holds for the routine's park", as an IN over the tenant's routines (a handful per
// park) -- the phone list, its counts and the badge key on it, so the planner resolves the
// caller's routines once and semijoins the tasks on pen_routine_tasks_routine_idx.
func RoutinesHeldBySQL(taskAlias, tenantBind, userBind string) string {
	return taskAlias + `.routine_id IN (SELECT prd.routine_id FROM pen_routine_definitions prd
  WHERE prd.tenant_id = ` + tenantBind + `
    AND EXISTS (SELECT 1 ` + RoleHoldersFromSQL("prd.tenant_id", "prd.assignee_roles", "prd.park_id") + `
      AND rg.user_id = ` + userBind + `))`
}

// maxPeoplePreview bounds the role-holder preview per routine and per role: the drawer names
// who holds a role today, it is not a roster.
const maxPeoplePreview = 50

// assigneesJSONSQL is the per-task-row array of the resolved assignees, [{user_id,
// display_name}] ordered by name, for the task projection (d is the joined definition).
var assigneesJSONSQL = `COALESCE((SELECT jsonb_agg(jsonb_build_object('user_id', h.user_id, 'display_name', h.display_name) ORDER BY h.display_name, h.user_id)
  FROM (SELECT DISTINCT rg.user_id::text AS user_id, COALESCE(rm.display_name, '') AS display_name ` +
	RoleHoldersFromSQL("t.tenant_id", "d.assignee_roles", "t.park_id") + `) h), '[]'::jsonb)`

// sqlRoutineAssignees is the batched preview: for each routine id, the people holding one of
// its roles in its park, each once, labelled with the FIRST of the routine's roles they hold in
// vocabulary order ($3), at most maxPeoplePreview per routine.
//
// projection-review: membership=pen_routine_definitions rows of ONE tenant named in $2 (PK, one row per routine) x their role holders; group_key=(routine_id, user_id) -- DISTINCT ON inside the LATERAL folds a user holding several matching grants to one row; join_cardinality=grants->members at most 1:1 (partial unique active (tenant,user)), the LATERAL bounded by LIMIT; pagination=none, bounded by maxPeoplePreview per routine; scope=tenant_id + routine ids + each routine's own park
var sqlRoutineAssignees = routineAssigneesSQL(`d.tenant_id = $1::uuid AND d.routine_id = ANY($2::uuid[])`)

// sqlRoutineAssigneesForScope is the same preview keyed by the routine LIST's own scope (tenant
// $1, optional park $2) instead of an id list, so it rides the list's batch rather than waiting
// for the routine ids; the list attaches by routine id and ignores any it did not read.
var sqlRoutineAssigneesForScope = routineAssigneesSQL(`d.tenant_id = $1::uuid AND ($2::uuid IS NULL OR d.park_id = $2::uuid)`)

// routineAssigneesSQLTemplate is the preview projection; %[1]s is the role-holder fragment,
// %[2]d the per-routine bound and %[3]s the routine predicate over d.
const routineAssigneesSQLTemplate = `
SELECT d.routine_id::text, h.user_id, h.display_name, h.role
FROM pen_routine_definitions d
CROSS JOIN LATERAL (
  SELECT x.user_id, x.display_name, x.role
  FROM (
    SELECT DISTINCT ON (rg.user_id) rg.user_id::text AS user_id, COALESCE(rm.display_name, '') AS display_name, rg.role
    %[1]s
    ORDER BY rg.user_id, array_position($3::text[], rg.role)
  ) x
  ORDER BY x.display_name, x.user_id
  LIMIT %[2]d
) h
WHERE %[3]s
ORDER BY d.routine_id, h.display_name, h.user_id`

func routineAssigneesSQL(where string) string {
	return fmt.Sprintf(routineAssigneesSQLTemplate, RoleHoldersFromSQL("d.tenant_id", "d.assignee_roles", "d.park_id"), maxPeoplePreview, where)
}

// sqlRoleHoldersForPark answers, per assignable role ($3, in order), who holds it for one park.
//
// projection-review: membership=the closed role vocabulary ($3, WITH ORDINALITY) x the holders of each role for ONE park; group_key=(role, user_id) -- DISTINCT inside the LATERAL; join_cardinality=grants->members at most 1:1 (partial unique active (tenant,user)), a role with no holder contributes no row; pagination=none, bounded by maxPeoplePreview per role; scope=tenant_id + park_id
var sqlRoleHoldersForPark = fmt.Sprintf(`
SELECT r.role, h.user_id, h.display_name
FROM unnest($3::text[]) WITH ORDINALITY AS r(role, ord)
CROSS JOIN LATERAL (
  SELECT x.user_id, x.display_name
  FROM (
    SELECT DISTINCT rg.user_id::text AS user_id, COALESCE(rm.display_name, '') AS display_name
    %s
  ) x
  ORDER BY x.display_name, x.user_id
  LIMIT %d
) h
ORDER BY r.ord, h.display_name, h.user_id`, RoleHoldersFromSQL("$1::uuid", "ARRAY[r.role]", "$2::uuid"), maxPeoplePreview)

// ListRoutineAssignees implements ports.Repository.
func (r *Repository) ListRoutineAssignees(ctx context.Context, tenantID string, routineIDs []string) (map[string][]domain.Assignee, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return listRoutineAssignees(ctx, r.pool, tenantID, routineIDs)
}

func listRoutineAssignees(ctx context.Context, q querier, tenantID string, routineIDs []string) (map[string][]domain.Assignee, error) {
	if len(routineIDs) == 0 {
		return map[string][]domain.Assignee{}, nil
	}
	bound := sqlbind.MustBind(sqlRoutineAssignees, tenantID, routineIDs, domain.AssignableRoles)
	rows, err := q.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("pen routine: routine assignees: %w", err)
	}
	defer rows.Close()
	return scanRoutineAssignees(rows)
}

// scanRoutineAssignees reads (routine_id, user_id, display_name, role) preview rows, keyed by
// routine in row order.
func scanRoutineAssignees(rows pgx.Rows) (map[string][]domain.Assignee, error) {
	out := map[string][]domain.Assignee{}
	for rows.Next() {
		var routineID string
		var a domain.Assignee
		if err := rows.Scan(&routineID, &a.UserID, &a.DisplayName, &a.RoleKey); err != nil {
			return nil, fmt.Errorf("pen routine: scan routine assignee: %w", err)
		}
		a.DisplayName = strings.TrimSpace(a.DisplayName)
		out[routineID] = append(out[routineID], a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pen routine: routine assignees: %w", err)
	}
	return out, nil
}

// scanRoleHolders reads sqlRoleHoldersForPark rows into one entry per assignable role, in
// vocabulary order, a role nobody holds carrying an empty list.
func scanRoleHolders(rows pgx.Rows) ([]ports.RoleHolders, error) {
	byRole := map[string][]ports.Person{}
	for rows.Next() {
		var role string
		var p ports.Person
		if err := rows.Scan(&role, &p.UserID, &p.DisplayName); err != nil {
			return nil, fmt.Errorf("pen routine: scan role holder: %w", err)
		}
		p.DisplayName = strings.TrimSpace(p.DisplayName)
		byRole[role] = append(byRole[role], p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pen routine: role holders: %w", err)
	}
	out := make([]ports.RoleHolders, 0, len(domain.AssignableRoles))
	for _, role := range domain.AssignableRoles {
		people := byRole[role]
		if people == nil {
			people = []ports.Person{}
		}
		out = append(out, ports.RoleHolders{Role: role, People: people})
	}
	return out, nil
}
