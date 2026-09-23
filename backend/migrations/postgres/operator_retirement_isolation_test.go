package postgres

import (
	"context"
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"strings"
	"testing"
)

func TestOperatorRetirementTenantIsolationAndExactRollback(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	id := func(n int) string { return fmt.Sprintf("03940000-0000-4000-8000-%012d", n) }
	for _, n := range []int{1, 2} {
		exec(`INSERT INTO tenants (tenant_id,name,status) VALUES ($1,'Retirement test','active')`, id(n))
		exec(`INSERT INTO locations (tenant_id,location_id,location_type,location_code,name,status) VALUES ($1,$2,'park',$3,'Test park','active')`, id(n), id(100+n), fmt.Sprint(n))
	}
	seed := func(tenant, person int, name, role, status string) {
		t.Helper()
		exec(`INSERT INTO workforce_members (tenant_id,workforce_member_id,user_id,display_code,display_name,status,primary_role_hint) VALUES ($1,$2,$2,$3,$4,$5,'operator')`, id(tenant), id(person), fmt.Sprint(person), name, status)
		exec(`INSERT INTO user_scope_grants (tenant_id,grant_id,user_id,role,scope_type,scope_id,status,valid_from) VALUES ($1,$2,$2,$3,'park',$4,'active',now())`, id(tenant), id(person), role, id(100+tenant))
		exec(`INSERT INTO person_access (tenant_id,workforce_member_id,scope_mode,designation_code) VALUES ($1,$2,'parks',$3)`, id(tenant), id(person), role)
	}
	seed(1, 11, "Amit Kumar", "operator", "active")
	seed(1, 12, "Darshan Talwar", "operator", "inactive")
	seed(2, 21, "Amit Kumar", "manager_health", "active")
	seed(2, 22, "Darshan Talwar", "manager_feed", "active")
	exec(`UPDATE workforce_members SET email='amit@example.com' WHERE workforce_member_id=$1`, id(11))
	exec(`INSERT INTO auth_pending_email_grants (tenant_id,pending_grant_id,email,normalized_email,role,scope_type,scope_id,status,valid_from,source) VALUES ($1,$2,'amit@example.com','amit@example.com','operator','tenant',$1,'active',now(),'test')`, id(1), id(15))
	exec(`INSERT INTO auth_pending_email_grants (tenant_id,pending_grant_id,email,normalized_email,role,scope_type,scope_id,status,valid_from,source) VALUES ($1,$2,'other-operator@example.com','other-operator@example.com','operator','tenant',$1,'active',now(),'test')`, id(2), id(23))
	_, sql := onlyMigrationWithSuffix(t, "retire_operator_onto_manager_roles")
	// The harness applies all migrations before fixtures. Replay this data repair.
	exec(`DROP TABLE IF EXISTS public.operator_retirement_000394_undo`)
	exec(`DROP TABLE IF EXISTS public.operator_retirement_000394_pending_scope_undo`)
	if _, err := pool.Exec(ctx, migrationUp(sql)); err == nil || !strings.Contains(err.Error(), "live or pending, span 2 tenants") {
		t.Fatalf("cross-tenant pending operator grant did not block retirement: %v", err)
	}
	var designationStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM designation_catalog WHERE designation_code='operator'`).Scan(&designationStatus); err != nil || designationStatus != "active" {
		t.Fatalf("operator designation after refused retirement = %q, err=%v; want active", designationStatus, err)
	}
	exec(`UPDATE auth_pending_email_grants SET status='revoked' WHERE pending_grant_id=$1`, id(23))
	exec(`DROP TABLE IF EXISTS public.operator_retirement_000394_undo`)
	exec(`DROP TABLE IF EXISTS public.operator_retirement_000394_pending_scope_undo`)
	exec(migrationUp(sql))
	check := func(person int, role, status, designation string) {
		t.Helper()
		var r, s, d string
		err := pool.QueryRow(ctx, `SELECT g.role,g.status,pa.designation_code FROM user_scope_grants g JOIN person_access pa ON pa.tenant_id=g.tenant_id AND pa.workforce_member_id=g.user_id WHERE g.grant_id=$1`, id(person)).Scan(&r, &s, &d)
		if err != nil {
			t.Fatal(err)
		}
		if r != role || s != status || d != designation {
			t.Fatalf("person %d: got %s/%s/%s, want %s/%s/%s", person, r, s, d, role, status, designation)
		}
	}
	check(11, "manager_health", "active", "manager_health")
	check(12, "operator", "revoked", "operator")
	check(22, "manager_feed", "active", "manager_feed")
	var pendingRole, pendingStatus string
	if err := pool.QueryRow(ctx, `SELECT role,status FROM auth_pending_email_grants WHERE pending_grant_id=$1`, id(23)).Scan(&pendingRole, &pendingStatus); err != nil || pendingRole != "operator" || pendingStatus != "revoked" {
		t.Fatalf("cross-tenant pending grant got %s/%s, err=%v; want operator/revoked", pendingRole, pendingStatus, err)
	}
	var pendingScope string
	var pendingScopeID string
	if err := pool.QueryRow(ctx, `SELECT role,scope_type,scope_id::text,status FROM auth_pending_email_grants WHERE pending_grant_id=$1`, id(15)).Scan(&pendingRole, &pendingScope, &pendingScopeID, &pendingStatus); err != nil || pendingRole != "manager_health" || pendingScope != "park" || pendingScopeID != id(101) || pendingStatus != "active" {
		t.Fatalf("ground pending grant got %s/%s/%s/%s, err=%v; want manager_health/park/%s/active", pendingRole, pendingScope, pendingScopeID, pendingStatus, err, id(101))
	}
	// A later same-name hire and a new grant are not undo targets. Renaming the
	// original person must not prevent restoration of their original grant.
	exec(`UPDATE workforce_members SET display_name='Renamed original' WHERE workforce_member_id=$1`, id(11))
	seed(1, 13, "Amit Kumar", "manager_health", "active")
	exec(`INSERT INTO user_scope_grants (tenant_id,grant_id,user_id,role,scope_type,scope_id,status,valid_from) VALUES ($1,$2,$3,'manager_feed','park',$4,'active',now())`, id(1), id(14), id(11), id(101))
	exec(strings.SplitN(sql, "-- +goose Down", 2)[1])
	check(11, "operator", "active", "operator")
	check(12, "operator", "revoked", "operator")
	check(13, "manager_health", "active", "manager_health")
	check(21, "manager_health", "active", "manager_health")
	check(22, "manager_feed", "active", "manager_feed")
	var newRole string
	if err := pool.QueryRow(ctx, `SELECT role FROM user_scope_grants WHERE grant_id=$1`, id(14)).Scan(&newRole); err != nil || newRole != "manager_feed" {
		t.Fatalf("later grant: %q, %v", newRole, err)
	}
	if err := pool.QueryRow(ctx, `SELECT role,scope_type,scope_id::text,status FROM auth_pending_email_grants WHERE pending_grant_id=$1`, id(15)).Scan(&pendingRole, &pendingScope, &pendingScopeID, &pendingStatus); err != nil || pendingRole != "operator" || pendingScope != "tenant" || pendingScopeID != id(1) || pendingStatus != "active" {
		t.Fatalf("rollback ground pending grant got %s/%s/%s/%s, err=%v; want operator/tenant/%s/active", pendingRole, pendingScope, pendingScopeID, pendingStatus, err, id(1))
	}
}

// The four tests below are the adversarial proof for the `_op_pending_targets`
// aggregate in migration 000394 -- the projection-review marker on that hunk
// claims a one-row-per-pending-grant grain, a deliberate one-to-many collapse,
// no pagination boundary, single-tenant scope and an active-only status bucket,
// and each claim is asserted here rather than only described.

// opRetirementFixture boots Postgres and seeds one tenant with two parks. The
// migration resolves a SINGLE tenant (v_tenant) and maps operators BY DISPLAY
// NAME (_op_mapping), so every person seeded here carries a name that mapping
// knows.
func opRetirementFixture(t *testing.T, ctx context.Context) (
	exec func(sql string, args ...any),
	scan func(sql string, args []any, dest ...any) error,
	id func(n int) string,
	runUp func(),
) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)
	exec = func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	scan = func(sql string, args []any, dest ...any) error {
		return pool.QueryRow(ctx, sql, args...).Scan(dest...)
	}
	id = func(n int) string { return fmt.Sprintf("03940000-0000-4000-8000-%012d", n) }
	exec(`INSERT INTO tenants (tenant_id,name,status) VALUES ($1,'Retirement aggregate test','active')`, id(1))
	for park, code := range map[int]string{101: "1", 102: "2"} {
		exec(`INSERT INTO locations (tenant_id,location_id,location_type,location_code,name,status) VALUES ($1,$2,'park',$3,'Test park','active')`, id(1), id(park), code)
	}
	_, sql := onlyMigrationWithSuffix(t, "retire_operator_onto_manager_roles")
	runUp = func() {
		t.Helper()
		// The harness applies all migrations before fixtures. Replay this data repair.
		exec(`DROP TABLE IF EXISTS public.operator_retirement_000394_undo`)
		exec(`DROP TABLE IF EXISTS public.operator_retirement_000394_pending_scope_undo`)
		exec(migrationUp(sql))
	}
	return exec, scan, id, runUp
}

// seedMappedOperator seeds one person who is a genuine migration target: a
// workforce member whose display_name is in _op_mapping, holding an ACTIVE
// park-scoped `operator` grant. grantOffset keeps each grant_id distinct so one
// person can hold a grant in more than one park.
func seedMappedOperator(t *testing.T, exec func(string, ...any), id func(int) string, person int, name string, parks ...int) {
	t.Helper()
	exec(`INSERT INTO workforce_members (tenant_id,workforce_member_id,user_id,display_code,display_name,status,primary_role_hint) VALUES ($1,$2,$2,$3,$4,'active','operator')`, id(1), id(person), fmt.Sprint(person), name)
	exec(`INSERT INTO person_access (tenant_id,workforce_member_id,scope_mode,designation_code) VALUES ($1,$2,'parks','operator')`, id(1), id(person))
	for offset, park := range parks {
		exec(`INSERT INTO user_scope_grants (tenant_id,grant_id,user_id,role,scope_type,scope_id,status,valid_from) VALUES ($1,$2,$3,'operator','park',$4,'active',now())`,
			id(1), id(person+1000*offset), id(person), id(park))
	}
}

func seedPendingOperatorInvite(t *testing.T, exec func(string, ...any), id func(int) string, person, pendingID int, email, status string) {
	t.Helper()
	exec(`UPDATE workforce_members SET email=$2 WHERE workforce_member_id=$1`, id(person), email)
	exec(`INSERT INTO auth_pending_email_grants (tenant_id,pending_grant_id,email,normalized_email,role,scope_type,scope_id,status,valid_from,source) VALUES ($1,$2,$3,$3,'operator','tenant',$1,$4,now(),'test')`,
		id(1), id(pendingID), email, status)
}

// A person ticked for TWO parks holds one active new_role park grant per park,
// so the user_scope_grants join fans the pending row out one-to-many. The
// aggregate must collapse that back to exactly ONE invite on min(park), not
// duplicate the invite and not trip the ambiguity assertion.
func TestOperatorRetirementPendingTargetsOneToManyParkGrantsCollapse(t *testing.T) {
	ctx := context.Background()
	exec, scan, id, runUp := opRetirementFixture(t, ctx)
	seedMappedOperator(t, exec, id, 11, "Amit Kumar", 101, 102)
	seedPendingOperatorInvite(t, exec, id, 11, 15, "amit@example.com", "active")
	runUp()

	var invites int
	if err := scan(`SELECT count(*) FROM public.operator_retirement_000394_pending_scope_undo WHERE pending_grant_id=$1`, []any{id(15)}, &invites); err != nil {
		t.Fatal(err)
	}
	if invites != 1 {
		t.Fatalf("two park grants produced %d undo rows for one pending invite; want exactly 1 (the min(park) collapse)", invites)
	}
	var role, scopeType, scopeID string
	if err := scan(`SELECT role,scope_type,scope_id::text FROM auth_pending_email_grants WHERE pending_grant_id=$1`, []any{id(15)}, &role, &scopeType, &scopeID); err != nil {
		t.Fatal(err)
	}
	if role != "manager_health" || scopeType != "park" || scopeID != id(101) {
		t.Fatalf("collapsed invite = %s/%s/%s; want manager_health/park/%s (the lowest of the two parks)", role, scopeType, scopeID, id(101))
	}
}

// The aggregate is a one-shot temp table, not a paged read: there is no LIMIT
// or OFFSET at which an invite could be skipped or double-counted. Seed well
// past any plausible batch size and assert EVERY invite moved in the one pass.
func TestOperatorRetirementMigratesEveryPendingGrantWithoutPagination(t *testing.T) {
	ctx := context.Background()
	exec, scan, id, runUp := opRetirementFixture(t, ctx)
	names := []string{
		"Amit Kumar", "Sagar Mahoor", "Kumar Sharath", "Natheswar", "Naveen", "Pramod",
		"Bipin", "Dheeraj Singh", "Mohd Shami", "Rajniti Kumar", "Ravi Kumbar", "Santosh Kumar",
		"Manikanth Yadav", "Dilkush Kumar", "Chandan Kumar", "Mithun", "Indrajit", "Irfan Gazi",
	}
	for i, name := range names {
		seedMappedOperator(t, exec, id, 20+i, name, 101)
		seedPendingOperatorInvite(t, exec, id, 20+i, 200+i, fmt.Sprintf("person%d@example.com", i), "active")
	}
	runUp()

	var leftover int
	if err := scan(`SELECT count(*) FROM auth_pending_email_grants WHERE role='operator' AND status='active'`, nil, &leftover); err != nil {
		t.Fatal(err)
	}
	if leftover != 0 {
		t.Fatalf("%d active operator invite(s) survived a %d-row migration; the aggregate must process every row in one pass", leftover, len(names))
	}
	var migrated int
	if err := scan(`SELECT count(*) FROM public.operator_retirement_000394_pending_scope_undo`, nil, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated != len(names) {
		t.Fatalf("undo recorded %d invites; want %d (one per pending grant, no page boundary)", migrated, len(names))
	}
}

// The invite must narrow onto the invitee's OWN park. Two people in different
// parks must not inherit each other's scope, which is what a join that lost its
// per-person predicate would produce.
func TestOperatorRetirementPendingInviteLandsOnOwnParkScope(t *testing.T) {
	ctx := context.Background()
	exec, scan, id, runUp := opRetirementFixture(t, ctx)
	seedMappedOperator(t, exec, id, 11, "Amit Kumar", 101)
	seedPendingOperatorInvite(t, exec, id, 11, 15, "amit@example.com", "active")
	seedMappedOperator(t, exec, id, 12, "Bipin", 102)
	seedPendingOperatorInvite(t, exec, id, 12, 16, "bipin@example.com", "active")
	runUp()

	for _, want := range []struct {
		pending int
		role    string
		park    int
	}{{15, "manager_health", 101}, {16, "manager_feed", 102}} {
		var role, scopeType, scopeID string
		if err := scan(`SELECT role,scope_type,scope_id::text FROM auth_pending_email_grants WHERE pending_grant_id=$1`, []any{id(want.pending)}, &role, &scopeType, &scopeID); err != nil {
			t.Fatal(err)
		}
		if role != want.role || scopeType != "park" || scopeID != id(want.park) {
			t.Fatalf("invite %d = %s/%s/%s; want %s/park/%s (its own park)", want.pending, role, scopeType, scopeID, want.role, id(want.park))
		}
	}
}

// Membership is the ACTIVE bucket only. A revoked invite is history and must be
// left exactly as it is -- neither migrated onto a manager role nor recorded in
// the undo table, which would resurrect it on rollback.
func TestOperatorRetirementPendingInviteStatusBuckets(t *testing.T) {
	ctx := context.Background()
	exec, scan, id, runUp := opRetirementFixture(t, ctx)
	seedMappedOperator(t, exec, id, 11, "Amit Kumar", 101)
	seedPendingOperatorInvite(t, exec, id, 11, 15, "amit@example.com", "active")
	seedMappedOperator(t, exec, id, 12, "Bipin", 101)
	seedPendingOperatorInvite(t, exec, id, 12, 16, "bipin@example.com", "revoked")
	runUp()

	var role, scopeType, status string
	if err := scan(`SELECT role,scope_type,status FROM auth_pending_email_grants WHERE pending_grant_id=$1`, []any{id(16)}, &role, &scopeType, &status); err != nil {
		t.Fatal(err)
	}
	if role != "operator" || scopeType != "tenant" || status != "revoked" {
		t.Fatalf("revoked invite = %s/%s/%s; want operator/tenant/revoked (untouched)", role, scopeType, status)
	}
	var undone int
	if err := scan(`SELECT count(*) FROM public.operator_retirement_000394_pending_scope_undo WHERE pending_grant_id=$1`, []any{id(16)}, &undone); err != nil {
		t.Fatal(err)
	}
	if undone != 0 {
		t.Fatalf("revoked invite recorded %d undo row(s); a non-member must not be restorable into the active bucket", undone)
	}
	if err := scan(`SELECT role,scope_type,status FROM auth_pending_email_grants WHERE pending_grant_id=$1`, []any{id(15)}, &role, &scopeType, &status); err != nil {
		t.Fatal(err)
	}
	if role != "manager_health" || scopeType != "park" || status != "active" {
		t.Fatalf("active invite = %s/%s/%s; want manager_health/park/active", role, scopeType, status)
	}
}
