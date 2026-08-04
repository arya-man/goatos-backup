package permissions

import "testing"

// One module, one accountable director (maintainer decision 2026-08-01). This test proves the
// segregation BOTH ways: the owner reaches its OWN module's routes, and every other director is
// refused. A one-way test would pass on a role that simply held everything.
//
// Counts has no owner-reaches probe on purpose. COUNTS IS AN OFF FEATURE (AGENTS.md):
// health_director is the recorded counts OWNER without counts ACCESS, so /counts/breakdown and
// /herd-register/summary stay ceo_internal-only. That direction is pinned instead by
// TestHealthDirectorOwnsCountsWithoutAccess below.
func TestDirectorReachesOwnModuleAndIsForbiddenOnAnothers(t *testing.T) {
	type probe struct {
		name        string
		method      string
		path        string
		ownerRole   string
		otherOwners []string
	}
	probes := []probe{
		{
			name: "feed dispatch sheet", method: "GET", path: "/feed-direction/preview",
			ownerRole:   RoleFeedDirector,
			otherOwners: []string{RolePCDirector, RoleGrowthDirector, RoleHealthDirector},
		},
		{
			name: "feed authored ration grid", method: "GET", path: "/feed-config/ration-rates",
			ownerRole:   RoleFeedDirector,
			otherOwners: []string{RolePCDirector, RoleGrowthDirector, RoleHealthDirector},
		},
	}
	for _, p := range probes {
		route, ok := Match(p.method, p.path)
		if !ok {
			t.Fatalf("%s: route %s %s is not registered", p.name, p.method, p.path)
		}
		if !RolesAuthorize([]string{p.ownerRole}, route.Permissions, route.AdminOnly) {
			t.Errorf("%s: %s must reach their OWN module route %s (permissions=%v)", p.name, p.ownerRole, p.path, route.Permissions)
		}
		for _, other := range p.otherOwners {
			if RolesAuthorize([]string{other}, route.Permissions, route.AdminOnly) {
				t.Errorf("%s: %s must NOT reach %s -- that module belongs to %s", p.name, other, p.path, p.ownerRole)
			}
		}
	}
}

// The capability-level counterpart: a director must not hold another module's permissions at
// all, not merely be blocked on today's route list. health_director is checked against the FULL
// vaccination set specifically because it is a distinct role from pc_director and must never
// drift into being an alias for it.
func TestDirectorHoldsNoOtherModulesCapabilities(t *testing.T) {
	forbidden := map[string][]string{
		RoleFeedDirector: {
			VaccinationRead, VaccinationVerify, VaccinationCampaign, VaccinationOverseeExecution,
			WeighingPlan, WeighingMonitor, WeighingExecute, WeighingOverseeOperators,
			CountsRead, CountsWrite, CountsApproveLifecycle, CountsApproveShifting, CountsApproveAccess,
			// Directs feeding; does not perform it, and does not sit in the verifier's chair.
			FeedDirectionComplete, VerificationReview, TaskExecute,
		},
		RoleHealthDirector: {
			VaccinationRead, VaccinationOverviewRead, VaccinationVerify, VaccinationCampaign, VaccinationOverseeExecution,
			WeighingPlan, WeighingMonitor, WeighingExecute, WeighingOverseeOperators,
			FeedConfigRead, FeedConfigWrite, FeedDirectionRead, FeedDirectionOversee,
			FeedPackingRead, FeedDirectionComplete,
			// COUNTS IS AN OFF FEATURE (AGENTS.md): health_director is the recorded counts OWNER
			// and the leadership recipient of a counts proof, but holds NO counts access until
			// the feature is deliberately switched on. counts.read is what lights the Counts nav,
			// so granting it here would turn the feature on by accident.
			CountsRead, CountsWrite, CountsApproveLifecycle, CountsApproveShifting, CountsApproveAccess,
			VerificationReview, TaskExecute,
		},
		RolePCDirector:     {FeedConfigRead, FeedConfigWrite, FeedDirectionOversee, CountsRead, WeighingExecute, WeighingMonitor},
		RoleGrowthDirector: {FeedConfigRead, FeedConfigWrite, FeedDirectionRead, FeedDirectionOversee, CountsRead, VaccinationRead},
	}
	for role, perms := range forbidden {
		for _, permission := range perms {
			if RoleHasPermission(role, permission) {
				t.Errorf("%s must not hold %s", role, permission)
			}
		}
	}
}

// Every permission a role DECLARES must be reachable through RoleHasPermission. This is the
// declared-vs-effective guard applied to the two new roles: the class of bug it protects against
// (a declared permission silently inert) has shipped twice on growth_director.
func TestNewDirectorRolesDeclaredPermissionsAreEffective(t *testing.T) {
	for _, role := range []string{RoleFeedDirector, RoleHealthDirector} {
		declared, ok := rolePermissions[role]
		if !ok || len(declared) == 0 {
			t.Fatalf("%s has no permission set; the role grant would authorize nothing at all", role)
		}
		if _, registered := registeredRoleOrigins[role]; !registered && len(registeredRoleOrigins) > 0 {
			t.Errorf("%s was installed without registerRole/the literal", role)
		}
		for permission := range declared {
			if !RoleHasPermission(role, permission) {
				t.Errorf("%s declares %s but RoleHasPermission says no; the permission is inert", role, permission)
			}
		}
	}
}

// The two roles must be grantable end to end, not just present in the map.
func TestNewDirectorRolesAreKnownGrantableRoles(t *testing.T) {
	for _, role := range []string{RoleFeedDirector, RoleHealthDirector} {
		if !IsKnownRole(role) {
			t.Errorf("%s is not a known grantable role; a seeded grant would be rejected", role)
		}
	}
	if RoleFeedDirector == RolePCDirector || RoleHealthDirector == RolePCDirector {
		t.Fatal("health_director/feed_director must be distinct role keys from pc_director")
	}
}

// Ownership without access, pinned in BOTH directions: health_director must not reach any counts
// route today (granting counts.read would light the Counts nav and switch the off feature on),
// and no OTHER director may reach them either.
func TestHealthDirectorOwnsCountsWithoutAccess(t *testing.T) {
	for _, path := range []string{"/counts/breakdown", "/herd-register/summary"} {
		route, ok := Match("GET", path)
		if !ok {
			t.Fatalf("route GET %s is not registered", path)
		}
		for _, role := range []string{RoleHealthDirector, RolePCDirector, RoleGrowthDirector, RoleFeedDirector} {
			if RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
				t.Errorf("%s reaches %s; counts is an OFF feature and stays ceo_internal-only", role, path)
			}
		}
		if !RolesAuthorize([]string{RoleCEOInternal}, route.Permissions, route.AdminOnly) {
			t.Errorf("ceo_internal lost access to %s", path)
		}
	}
}
