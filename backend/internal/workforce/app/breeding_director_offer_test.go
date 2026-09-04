package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestBreedingDirectorIsOfferedPreventiveCareAndThePlanWizard pins the phone half of the
// 2026-09-04 maintainer decision: the Breeding Director plans hoof / hair trimming, so the
// PC Care module is offered to the role on its own (a bare holder must not resolve an empty
// nav, the feed/health-director defect) and the `pc_care_plan` wizard flag lights for
// pc_care.plan_trimming exactly as it does for pc_care.plan. The wizard's CATEGORY list is
// narrowed server-side by the planner catalog, not here.
//
// The negative half: a bare pc_director still gets no wizard, so the carve-out did not leak
// onto the job it sits beside on Dinakar's stack.
func TestBreedingDirectorIsOfferedPreventiveCareAndThePlanWizard(t *testing.T) {
	const en = localization.DefaultTag
	hasKey := func(keys []string, want string) bool {
		for _, k := range keys {
			if k == want {
				return true
			}
		}
		return false
	}

	bare := []domain.GrantSummary{grantWithRole(permissions.RoleBreedingDirector)}
	if keys := leadershipModuleKeys(bare); !hasKey(keys, "pc_care") {
		t.Fatalf("leadership module keys = %v, want pc_care offered to a bare breeding_director", keys)
	}
	if _, ok := moduleKeySet(modulesFor(bare, nil, en))["pc_care"]; !ok {
		t.Fatal("Preventive Care module did not render for a bare breeding_director")
	}
	if !canPlanPCCare(bare, nil) {
		t.Fatal("pc_care_plan must light for pc_care.plan_trimming -- the wizard is how trimming gets planned")
	}
	if canExecutePCCare(bare, nil) {
		t.Fatal("pc_care_execute must NOT light for breeding_director -- a planner does not film the work")
	}

	// Dinakar's real stack: the wizard lights, and it lit because of the new desk, not pc_director.
	dinakar := []domain.GrantSummary{
		grantWithRole(permissions.RoleGrowthDirector),
		grantWithRole(permissions.RolePCDirector),
		grantWithRole(permissions.RoleBreedingDirector),
	}
	if !canPlanPCCare(dinakar, nil) {
		t.Fatal("pc_care_plan must light on Dinakar's stack")
	}
	if canPlanPCCare([]domain.GrantSummary{grantWithRole(permissions.RolePCDirector)}, nil) {
		t.Fatal("a bare pc_director must still get no plan wizard -- the carve-out belongs to breeding_director")
	}
}

// TestPlanWizardFlagFollowsThePersonsTicksNotTheirJob closes the bootstrap half of PR #181
// finding PC-181-002: once a person has access rows, `pc_care_plan` must answer from those
// rows -- lit for a pc_trimming@Configure tick with no breeding_director job, dark for a
// breeding_director whose ticks dropped planning -- because that is what the route and the
// service will say when the wizard's create lands.
func TestPlanWizardFlagFollowsThePersonsTicksNotTheirJob(t *testing.T) {
	tickedOnly := navScope{
		grants: []domain.GrantSummary{grantWithRole(permissions.RoleOperator)},
		held:   map[string]struct{}{permissions.PCCareMonitor: {}, permissions.PCCarePlanTrimming: {}},
	}
	if !canPlanPCCareScoped(tickedOnly, []string{"pc_trimming"}, true) {
		t.Fatal("pc_care_plan must light for pc_trimming@configure without the breeding_director job")
	}
	if _, ok := moduleKeySet(modulesForScope(tickedOnly, []string{"pc_trimming"}, localization.DefaultTag, true, []string{"pc_trimming"}))["pc_care"]; !ok {
		t.Fatal("a pc_trimming mobile tick must render the shared Preventive Care module")
	}
	if nav := visibleNavigationForTicks(tickedOnly, []string{"pc_trimming"}, localization.DefaultTag, []string{"pc_trimming"}); len(nav) == 0 {
		t.Fatal("a pc_trimming mobile tick must render the Preventive Care bottom navigation")
	}
	roleButUnticked := navScope{
		grants: []domain.GrantSummary{grantWithRole(permissions.RoleBreedingDirector)},
		held:   map[string]struct{}{permissions.PCCareMonitor: {}},
	}
	if canPlanPCCareScoped(roleButUnticked, []string{"pc_trimming"}, true) {
		t.Fatal("pc_care_plan must NOT light for a breeding_director whose ticks removed planning")
	}
	// No rows yet: the role map still decides, as before the cutover.
	if !canPlanPCCareScoped(scopeOf([]domain.GrantSummary{grantWithRole(permissions.RoleBreedingDirector)}), nil, false) {
		t.Fatal("with no access rows the role path must still light the wizard for breeding_director")
	}
}

func TestPCCareBootstrapFlagsFollowTicksNotRoles(t *testing.T) {
	scope := navScope{
		grants: []domain.GrantSummary{grantWithRole(permissions.RolePCDirector)},
		held:   map[string]struct{}{permissions.PCCareMonitor: {}},
	}
	if canExecutePCCareScoped(scope, []string{"pc_care"}, true) {
		t.Fatal("pc_care_execute must not light when person ticks removed pc_care.execute")
	}
	if canApproveVaccineStockScoped(scope) {
		t.Fatal("pc_care_stock_approve must not light when person ticks removed pc_care.stock_approve")
	}
	if canPlanPCCareScoped(scope, []string{"pc_care"}, true) {
		t.Fatal("pc_care_plan must not light when person ticks include monitor only")
	}
}
