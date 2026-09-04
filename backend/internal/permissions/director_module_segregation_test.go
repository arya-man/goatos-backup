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
		{
			name: "feed transport worklist", method: "GET", path: "/feed-transport/tasks",
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

func TestProcurementDirectorIsStockOnlyOnAdminWeb(t *testing.T) {
	allowed, ok := Match("GET", "/feed-analytics/stock")
	if !ok {
		t.Fatal("feed stock route is not registered")
	}
	if !RolesAuthorize([]string{RoleProcurementDirector}, allowed.Permissions, allowed.AdminOnly) &&
		!RolesAuthorizeAny([]string{RoleProcurementDirector}, allowed.AnyPermissions) {
		t.Fatal("procurement_director must reach Feed Analytics stock")
	}
	for _, tt := range []struct {
		method string
		path   string
	}{
		{"GET", "/procurement/source-entry/loads"},
		{"GET", "/feed-analytics/directed"},
		{"GET", "/feed-analytics/execution"},
		{"GET", "/feed-analytics/experiment"},
	} {
		route, ok := Match(tt.method, tt.path)
		if !ok {
			t.Fatalf("%s %s is not registered", tt.method, tt.path)
		}
		if RolesAuthorize([]string{RoleProcurementDirector}, route.Permissions, route.AdminOnly) ||
			RolesAuthorizeAny([]string{RoleProcurementDirector}, route.AnyPermissions) {
			t.Fatalf("procurement_director must not authorize %s %s", tt.method, tt.path)
		}
	}
	sales, ok := Match("GET", "/sales/deals")
	if !ok {
		t.Fatal("sales route is not registered")
	}
	if !RolesAuthorize([]string{RoleProcurementDirector}, sales.Permissions, sales.AdminOnly) {
		t.Fatal("procurement_director must keep Sales Config data access")
	}
	for _, tt := range []struct {
		method string
		path   string
	}{
		{"GET", "/procurement/vendors"},
		{"GET", "/procurement/feed-purchases"},
	} {
		route, ok := Match(tt.method, tt.path)
		if !ok {
			t.Fatalf("%s %s is not registered", tt.method, tt.path)
		}
		if !RolesAuthorize([]string{RoleProcurementDirector}, route.Permissions, route.AdminOnly) &&
			!RolesAuthorizeAny([]string{RoleProcurementDirector}, route.AnyPermissions) {
			t.Fatalf("procurement_director must authorize %s %s", tt.method, tt.path)
		}
	}
}

func TestProcurementDirectorPlusFeedDirectorStillGetsStockOnlyFeedAnalytics(t *testing.T) {
	roles := []string{RoleProcurementDirector, RoleFeedDirector}
	held := permissionsForRoles(roles...)
	stock, ok := Match("GET", "/feed-analytics/stock")
	if !ok {
		t.Fatal("feed stock route is not registered")
	}
	if !AuthorizeRoute(stock, roles) {
		t.Fatal("real Hemant role stack must keep Feed Analytics stock")
	}
	if allowed, decidable := AuthorizePermissionSet(stock, held); !decidable || !allowed {
		t.Fatal("real Hemant resolved permissions must keep Feed Analytics stock")
	}
	for _, tt := range []struct {
		method string
		path   string
	}{
		{"GET", "/procurement/source-entry/loads"},
		{"GET", "/feed-analytics/directed"},
		{"GET", "/feed-analytics/execution"},
		{"GET", "/feed-analytics/experiment"},
		{"GET", "/feed-analytics/shed-feed"},
	} {
		route, ok := Match(tt.method, tt.path)
		if !ok {
			t.Fatalf("%s %s is not registered", tt.method, tt.path)
		}
		if AuthorizeRoute(route, roles) {
			t.Fatalf("real Hemant role stack must not authorize %s %s", tt.method, tt.path)
		}
		if allowed, decidable := AuthorizePermissionSet(route, held); !decidable || allowed {
			t.Fatalf("real Hemant resolved permissions must not authorize %s %s", tt.method, tt.path)
		}
		if !AuthorizeRoute(route, []string{RoleCEOInternal, RoleProcurementDirector, RoleFeedDirector}) {
			t.Fatalf("CEO/CXO must keep %s %s even when carrying procurement/feed roles", tt.method, tt.path)
		}
		if allowed, decidable := AuthorizePermissionSet(route, permissionsForRoles(RoleCEOInternal, RoleProcurementDirector, RoleFeedDirector)); !decidable || !allowed {
			t.Fatalf("CEO/CXO resolved permissions must keep %s %s even when carrying procurement/feed roles", tt.method, tt.path)
		}
	}
	tagAnimals, ok := Match("POST", "/admin/goats/sale-allocations/confirm")
	if !ok {
		t.Fatal("sale allocation confirm route is not registered")
	}
	if !AuthorizeRoute(tagAnimals, roles) {
		t.Fatal("real Hemant role stack must keep Tag animals to sale")
	}
	if allowed, decidable := AuthorizePermissionSet(tagAnimals, held); !decidable || !allowed {
		t.Fatal("real Hemant resolved permissions must keep Tag animals to sale")
	}
}

func permissionsForRoles(roles ...string) []string {
	set := map[string]struct{}{}
	for _, role := range roles {
		for permission := range rolePermissions[role] {
			set[permission] = struct{}{}
		}
	}
	held := make([]string, 0, len(set))
	for permission := range set {
		held = append(held, permission)
	}
	return held
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
			FeedPackingRead, FeedDirectionComplete, FeedTransportRead,
			// COUNTS IS AN OFF FEATURE (AGENTS.md): health_director is the recorded counts OWNER
			// and the leadership recipient of a counts proof, but holds NO counts access until
			// the feature is deliberately switched on. counts.read is what lights the Counts nav,
			// so granting it here would turn the feature on by accident.
			CountsRead, CountsWrite, CountsApproveLifecycle, CountsApproveShifting, CountsApproveAccess,
			VerificationReview, TaskExecute,
		},
		RolePCDirector:     {FeedConfigRead, FeedConfigWrite, FeedDirectionOversee, FeedTransportRead, CountsRead, WeighingExecute, WeighingMonitor},
		RoleGrowthDirector: {FeedConfigRead, FeedConfigWrite, FeedDirectionRead, FeedDirectionOversee, FeedTransportRead, CountsRead, VaccinationRead},
		// breeding_director (maintainer decision 2026-09-04) plans HOOF / HAIR TRIMMING and nothing
		// beyond the Preventive Care board: no whole-module plan, no execute (a planner must not film
		// the work they planned), no stock verdict, and no other module at all.
		RoleBreedingDirector: {
			PCCarePlan, PCCareExecute, PCCareStockApprove, PCCareOverseeOperators,
			VaccinationRead, VaccinationVerify, VaccinationCampaign, VaccinationOverseeExecution,
			WeighingPlan, WeighingMonitor, WeighingExecute, WeighingOverseeOperators,
			CountsRead, CountsWrite, CountsApproveLifecycle, CountsApproveShifting, CountsApproveAccess,
			FeedConfigRead, FeedConfigWrite, FeedDirectionRead, FeedDirectionOversee, FeedDirectionComplete, FeedTransportRead,
			VerificationReview, VerificationAct, VerificationVerdict,
			HealthConfigRead, HealthConfigWrite, HealthRead, HealthDiagnose,
			GoatRead, TaskExecute, TaskAssign,
		},
		// procurement_director READS the feed chain by explicit maintainer decision 2026-08-21
		// ("he should see only the Procurement and Feed modules in web"), so the feed reads are
		// absent from this list on purpose. Everything that RUNS the feed chain, and every other
		// module, stays forbidden: this role must never drift into a second feed_director or into
		// the modules its web lens hides.
		RoleProcurementDirector: {
			VaccinationRead, VaccinationVerify, VaccinationCampaign, VaccinationOverseeExecution,
			WeighingPlan, WeighingMonitor, WeighingExecute, WeighingOverseeOperators,
			CountsRead, CountsWrite, CountsApproveLifecycle, CountsApproveShifting, CountsApproveAccess,
			FeedConfigRead, FeedConfigWrite, FeedDirectionOversee, FeedDirectionComplete,
			VerificationReview, VerificationAct, VerificationVerdict,
			GoatRead, CalendarRead, CalendarAction, TaskExecute, TaskRead, TaskAssign,
			HealthConfigRead, HealthConfigWrite, HealthRead, HealthDiagnose,
			AppBootstrap,
		},
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
	for _, role := range []string{RoleFeedDirector, RoleHealthDirector, RoleProcurementDirector} {
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
	for _, role := range []string{RoleFeedDirector, RoleHealthDirector, RoleProcurementDirector} {
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
		for _, role := range []string{RoleHealthDirector, RolePCDirector, RoleGrowthDirector, RoleFeedDirector, RoleProcurementDirector} {
			if RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
				t.Errorf("%s reaches %s; counts is an OFF feature and stays ceo_internal-only", role, path)
			}
		}
		if !RolesAuthorize([]string{RoleCEOInternal}, route.Permissions, route.AdminOnly) {
			t.Errorf("ceo_internal lost access to %s", path)
		}
	}
}

// Widening the transport LIST to a read permission must not widen the SUBMIT with it. The Feed
// Director sees every page of the chain they own (maintainer decision 2026-08-05) and still
// cannot record a transport task as done -- Feed_Director.pdf puts field execution on the Park
// Head, and "executing is not directing" is the line the split was built to preserve.
//
// Pinned as a pair because the read/write split is the ONLY thing holding it: before this,
// both routes shared FeedDirectionComplete, so any grant that revealed the list also conferred
// the ability to submit.
func TestFeedTransportReadDoesNotConferSubmit(t *testing.T) {
	list, ok := Match("GET", "/feed-transport/tasks")
	if !ok {
		t.Fatal("GET /feed-transport/tasks is not a registered route")
	}
	submit, ok := Match("POST", "/feed-transport/tasks/{task_id}/submit")
	if !ok {
		t.Fatal("POST /feed-transport/tasks/{task_id}/submit is not a registered route")
	}
	if !RolesAuthorize([]string{RoleFeedDirector}, list.Permissions, list.AdminOnly) {
		t.Errorf("feed_director must READ the transport worklist; route requires %v", list.Permissions)
	}
	if RolesAuthorize([]string{RoleFeedDirector}, submit.Permissions, submit.AdminOnly) {
		t.Error("feed_director must NOT submit a transport proof: directing is not executing")
	}
	// Ground execution keeps both halves. CEO joins feed_director on the read-only side: leadership
	// can inspect status, but must not submit feed transport/direction proof.
	for _, role := range []string{RoleOperator, RoleParkHead} {
		if !RolesAuthorize([]string{role}, list.Permissions, list.AdminOnly) {
			t.Errorf("%s lost the transport worklist read", role)
		}
		if !RolesAuthorize([]string{role}, submit.Permissions, submit.AdminOnly) {
			t.Errorf("%s lost the transport submit", role)
		}
	}
	if !RolesAuthorize([]string{RoleCEOInternal}, list.Permissions, list.AdminOnly) {
		t.Error("ceo_internal lost the transport worklist read")
	}
	if RolesAuthorize([]string{RoleCEOInternal}, submit.Permissions, submit.AdminOnly) {
		t.Error("ceo_internal must NOT submit a transport proof: leadership is read/oversee, not execute")
	}
}

func TestFeedDirectionDistributionCompleteIsGroundExecutionOnly(t *testing.T) {
	route, ok := Match("POST", "/feed-direction/distribution/complete")
	if !ok {
		t.Fatal("POST /feed-direction/distribution/complete is not a registered route")
	}
	for _, role := range []string{RoleOperator, RoleParkHead} {
		if !RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
			t.Errorf("%s must keep feed distribution execution", role)
		}
	}
	for _, role := range []string{RoleFeedDirector, RoleCEOInternal} {
		if RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
			t.Errorf("%s must NOT execute feed distribution proof", role)
		}
	}
}

// The feed vertical's org-role oversight tiers reach the dispatch sheet through the feed
// vertical's own permission, not as a side effect of the tier-wide vaccination protocol read.
// Before this they held ProtocolRead and NOT FeedDirectionRead, so the Feed Direction tab
// rendered for them while the route behind it refused: a 403 on arrival.
func TestFeedVerticalOrgRolesReachTheDispatchSheetTheyAreShown(t *testing.T) {
	route, ok := Match("GET", "/feed-direction/preview")
	if !ok {
		t.Fatal("GET /feed-direction/preview is not a registered route")
	}
	for _, tier := range []Tier{TierDirector, TierHead} {
		role := RoleKey(tier, VerticalFeed)
		if !RolesAuthorize([]string{role}, route.Permissions, route.AdminOnly) {
			t.Errorf("%s is shown the Feed Direction tab but cannot open it (route requires %v)", role, route.Permissions)
		}
	}
	// Scoped to the FEED vertical: no other vertical's director gains a feed read.
	for _, vertical := range []Vertical{VerticalPreventiveCare, VerticalGrowth, VerticalHealth} {
		role := RoleKey(TierDirector, vertical)
		if RoleHasPermission(role, FeedDirectionRead) {
			t.Errorf("%s must not hold %s -- one module, one director", role, FeedDirectionRead)
		}
	}
}
