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
	exec(`INSERT INTO auth_pending_email_grants (tenant_id,pending_grant_id,email,normalized_email,role,scope_type,scope_id,status,valid_from,source) VALUES ($1,$2,'other-operator@example.com','other-operator@example.com','operator','tenant',$1,'active',now(),'test')`, id(2), id(23))
	_, sql := onlyMigrationWithSuffix(t, "retire_operator_onto_manager_roles")
	// The harness applies all migrations before fixtures. Replay this data repair.
	exec(`DROP TABLE IF EXISTS public.operator_retirement_000394_undo`)
	if _, err := pool.Exec(ctx, migrationUp(sql)); err == nil || !strings.Contains(err.Error(), "live or pending, span 2 tenants") {
		t.Fatalf("cross-tenant pending operator grant did not block retirement: %v", err)
	}
	var designationStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM designation_catalog WHERE designation_code='operator'`).Scan(&designationStatus); err != nil || designationStatus != "active" {
		t.Fatalf("operator designation after refused retirement = %q, err=%v; want active", designationStatus, err)
	}
	exec(`UPDATE auth_pending_email_grants SET status='revoked' WHERE pending_grant_id=$1`, id(23))
	exec(`DROP TABLE IF EXISTS public.operator_retirement_000394_undo`)
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
}
