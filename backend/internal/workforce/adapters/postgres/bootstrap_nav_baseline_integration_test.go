package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestEnsureBaselineDepartmentModuleGrantsGivesEveryDepartmentNonEmptyNav is the P1-NAV regression.
//
// Nav is composed purely from department_module_grants. A non-leadership operator whose department
// holds no ACTIVE grant to an AVAILABLE module composes a BLANK bottom bar. The specialised seed
// grant map covers only some department codes and maps feed/breeding to 'soon' modules that
// contribute no available nav, so a member of an unmapped department (here "milk") gets an empty
// ListGrantedModuleKeys -> blank nav. The baseline grant must give every active department the
// cross-cutting Counts module so no operator lands on a blank bar.
//
// This drives the REAL grant path (EnsureBaselineDepartmentModuleGrants, the same logic the roster
// seed runs) against a real department + member, not a hand-set grantedModules fixture.
func TestEnsureBaselineDepartmentModuleGrantsGivesEveryDepartmentNonEmptyNav(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID = "00000000-0000-4000-8000-0000000009a1"
		deptID   = "00000000-0000-4000-8000-0000000009a2"
		memberID = "00000000-0000-4000-8000-0000000009a3"
		userID   = "00000000-0000-4000-8000-0000000009a4"
	)

	if _, err := pool.Exec(ctx, `
INSERT INTO public.tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Nav Baseline Test', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	// A department whose code is ABSENT from the seed's specialised grant map. Its members are the
	// operators the blank-nav bug strands.
	if _, err := pool.Exec(ctx, `
INSERT INTO public.departments (department_id, tenant_id, code, label, status)
VALUES ($1::uuid, $2::uuid, 'milk', 'Milk', 'active')`, deptID, tenantID); err != nil {
		t.Fatalf("seed department: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO public.workforce_members (
  workforce_member_id, tenant_id, user_id, department_id, display_code, display_name, status, primary_role_hint
) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'MILK-01', 'Milk Operator', 'active', 'operator')`,
		memberID, tenantID, userID, deptID); err != nil {
		t.Fatalf("seed workforce_member: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)

	// PRE-FIX CONDITION: with no baseline grant, this department's operator composes a BLANK nav.
	before, err := repo.ListGrantedModuleKeys(ctx, tenantID, userID)
	if err != nil {
		t.Fatalf("list granted modules (before): %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("granted modules before baseline=%v, want empty (premise: an unmapped department starts blank)", before)
	}

	// THE FIX: baseline-grant Counts to every active department.
	granted, err := repo.EnsureBaselineDepartmentModuleGrants(ctx, tenantID, "counts")
	if err != nil {
		t.Fatalf("ensure baseline grants: %v", err)
	}
	if granted != 1 {
		t.Fatalf("baseline grants inserted=%d, want 1 (one active department)", granted)
	}

	after, err := repo.ListGrantedModuleKeys(ctx, tenantID, userID)
	if err != nil {
		t.Fatalf("list granted modules (after): %v", err)
	}
	if len(after) == 0 {
		t.Fatal("granted modules after baseline is empty -- the operator would still land on a blank bottom bar")
	}
	found := false
	for _, k := range after {
		if k == "counts" {
			found = true
		}
	}
	if !found {
		t.Fatalf("granted modules after baseline=%v, want it to include the baseline 'counts' module", after)
	}

	// Idempotent: a second run reactivates/inserts nothing new.
	again, err := repo.EnsureBaselineDepartmentModuleGrants(ctx, tenantID, "counts")
	if err != nil {
		t.Fatalf("ensure baseline grants (idempotent run): %v", err)
	}
	if again != 0 {
		t.Fatalf("second baseline run inserted=%d, want 0 (idempotent)", again)
	}
}
