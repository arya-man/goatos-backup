package permissions

import "testing"

func TestRoleKeyAndParseRoleKeyRoundTrip(t *testing.T) {
	for _, tier := range AllTiers {
		for _, vertical := range AllVerticals {
			key := RoleKey(tier, vertical)
			gotTier, gotVertical, ok := ParseRoleKey(key)
			if !ok {
				t.Fatalf("ParseRoleKey(%q) ok=false, want true", key)
			}
			if gotTier != tier || gotVertical != vertical {
				t.Fatalf("ParseRoleKey(%q)=(%q,%q), want (%q,%q)", key, gotTier, gotVertical, tier, vertical)
			}
			if !IsOrgRoleKey(key) {
				t.Fatalf("IsOrgRoleKey(%q)=false, want true", key)
			}
		}
	}
}

func TestParseRoleKeyRejectsFlatLegacyRoles(t *testing.T) {
	for _, role := range []string{RoleAdmin, RoleVerifier, RoleParkHead, RolePCDirector, RoleOperator, RoleCEOInternal} {
		if _, _, ok := ParseRoleKey(role); ok {
			t.Fatalf("ParseRoleKey(%q) ok=true, want false (flat legacy role has no vertical)", role)
		}
		if IsOrgRoleKey(role) {
			t.Fatalf("IsOrgRoleKey(%q)=true, want false", role)
		}
	}
}

func TestIsKnownRoleCoversLegacyAndCompositeRoles(t *testing.T) {
	for _, role := range []string{RoleAdmin, RoleVerifier, RoleParkHead, RolePCDirector, RoleOperator, RoleCEOInternal} {
		if !IsKnownRole(role) {
			t.Fatalf("IsKnownRole(%q)=false, want true", role)
		}
	}
	if !IsKnownRole(RoleKey(TierManager, VerticalFeed)) {
		t.Fatal("IsKnownRole(manager_feed)=false, want true")
	}
	if IsKnownRole("director_atlantis") {
		t.Fatal("IsKnownRole(director_atlantis)=true, want false (unknown vertical)")
	}
	if IsKnownRole("astronaut_feed") {
		t.Fatal("IsKnownRole(astronaut_feed)=true, want false (unknown tier)")
	}
}

// TestRoleAuthorizedForVertical proves the core separation the org role
// model requires: a Feed Manager cannot act on Health, and vice versa,
// because vertical is baked into the composite role key itself.
func TestRoleAuthorizedForVertical(t *testing.T) {
	feedManager := RoleKey(TierManager, VerticalFeed)
	healthManager := RoleKey(TierManager, VerticalHealth)

	if !RoleAuthorizedForVertical(feedManager, VerticalFeed) {
		t.Fatal("feed manager must authorize for its own vertical (feed)")
	}
	if RoleAuthorizedForVertical(feedManager, VerticalHealth) {
		t.Fatal("feed manager must NOT authorize for a different vertical (health)")
	}
	if !RoleAuthorizedForVertical(healthManager, VerticalHealth) {
		t.Fatal("health manager must authorize for its own vertical (health)")
	}
	if RoleAuthorizedForVertical(healthManager, VerticalFeed) {
		t.Fatal("health manager must NOT authorize for a different vertical (feed)")
	}

	// Flat legacy roles have no vertical yet -- cross-vertical by definition
	// (see org-role-model.md's gap table); must not be narrowed implicitly.
	for _, vertical := range AllVerticals {
		if !RoleAuthorizedForVertical(RoleCEOInternal, vertical) {
			t.Fatalf("ceo_internal must authorize every vertical, failed for %q", vertical)
		}
		if !RoleAuthorizedForVertical(RoleVerifier, vertical) {
			t.Fatalf("verifier must authorize every vertical (cross-vertical video verification team), failed for %q", vertical)
		}
	}
}

// TestManagerAndAssistantManagerTiersCanCapture proves ground tiers (Manager
// + Assistant Manager) keep the capture affordance across every vertical.
func TestManagerAndAssistantManagerTiersCanCapture(t *testing.T) {
	for _, tier := range []Tier{TierManager, TierAssistantManager} {
		for _, vertical := range AllVerticals {
			role := RoleKey(tier, vertical)
			if !RoleHasPermission(role, TaskExecute) {
				t.Fatalf("%q should have TaskExecute (ground tiers capture)", role)
			}
		}
	}
}

// TestHeadAndDirectorTiersCannotCapture proves the org-role-model.md
// correction holds: capture affordance is ground-only. Head/Director see the
// work but never record proof on a goat themselves.
func TestHeadAndDirectorTiersCannotCapture(t *testing.T) {
	for _, tier := range []Tier{TierHead, TierDirector} {
		for _, vertical := range AllVerticals {
			role := RoleKey(tier, vertical)
			if RoleHasPermission(role, TaskExecute) {
				t.Fatalf("%q must NOT have TaskExecute (capture is ground-only: Manager + Assistant Manager)", role)
			}
		}
	}
}

// TestNoOrgTierCanVerify proves separation of duty: nobody in the tier
// catalog (Manager, Assistant Manager, Head, Director) can verify their own
// or anyone else's work -- verification is RoleVerifier's job alone (plus
// RoleCEOInternal override), matching "operator can't verify" /
// "nobody both captures and verifies the same work" in org-role-model.md.
func TestNoOrgTierCanVerify(t *testing.T) {
	for _, tier := range AllTiers {
		for _, vertical := range AllVerticals {
			role := RoleKey(tier, vertical)
			if RoleHasPermission(role, TaskVerify) {
				t.Fatalf("%q must NOT have TaskVerify (verify is Verifier-only)", role)
			}
			if RoleHasPermission(role, VaccinationVerify) {
				t.Fatalf("%q must NOT have VaccinationVerify (verify is Verifier-only)", role)
			}
		}
	}
	// The flat legacy operator role must also not verify -- pre-existing
	// behavior, reasserted here alongside the new tier coverage above.
	if RoleHasPermission(RoleOperator, TaskVerify) {
		t.Fatal("operator must NOT have TaskVerify")
	}
}

// TestHeadAndDirectorTiersActOnVerdictAndManageRoster proves Head/Director
// retain oversight + roster-management + calendar-action capability even
// though they cannot capture or verify.
func TestHeadAndDirectorTiersActOnVerdictAndManageRoster(t *testing.T) {
	for _, tier := range []Tier{TierHead, TierDirector} {
		role := RoleKey(tier, VerticalHealth)
		if !RoleHasPermission(role, CalendarAction) {
			t.Fatalf("%q should have CalendarAction (act on verdict)", role)
		}
		if !RoleHasPermission(role, OperatorsManageRoster) {
			t.Fatalf("%q should have OperatorsManageRoster (manage roster/operators)", role)
		}
	}
}

// TestScopeIDsForPermissionIsParkScoped proves the park-scope hard filter:
// a grant scoped to one park's scope_id never authorizes a different park,
// even for a permission the role otherwise has everywhere.
func TestScopeIDsForPermissionIsParkScoped(t *testing.T) {
	const cbeParkID = "11111111-0000-4000-8000-000000000001"
	const cptParkID = "22222222-0000-4000-8000-000000000002"

	feedManager := RoleKey(TierManager, VerticalFeed)
	grants := []ActiveGrant{
		{Role: feedManager, ScopeType: "park", ScopeID: cbeParkID},
	}

	ids := ScopeIDsForPermission(grants, TaskExecute, "park")
	if len(ids) != 1 || ids[0] != cbeParkID {
		t.Fatalf("ScopeIDsForPermission()=%v, want [%s]", ids, cbeParkID)
	}
	for _, id := range ids {
		if id == cptParkID {
			t.Fatal("a CBE-only grant must not authorize CPT (cross-park)")
		}
	}

	// A permission the role does not have yields no scope IDs at all, even
	// though the grant row exists -- permission gates before scope.
	if got := ScopeIDsForPermission(grants, TaskVerify, "park"); len(got) != 0 {
		t.Fatalf("ScopeIDsForPermission(TaskVerify)=%v, want empty (feed manager cannot verify)", got)
	}

	// Multi-park grants union correctly and stay deduplicated.
	multiPark := []ActiveGrant{
		{Role: feedManager, ScopeType: "park", ScopeID: cbeParkID},
		{Role: feedManager, ScopeType: "park", ScopeID: cptParkID},
		{Role: feedManager, ScopeType: "park", ScopeID: cbeParkID},
	}
	got := ScopeIDsForPermission(multiPark, TaskExecute, "park")
	if len(got) != 2 {
		t.Fatalf("ScopeIDsForPermission(multiPark)=%v, want 2 distinct park IDs", got)
	}
}

// TestOrgTierPermissionsDoNotLeakBeyondKnownPermissions guards against a
// tierPermissions typo referencing a permission constant that does not exist
// as a route-checked permission.
func TestOrgTierPermissionsDoNotLeakBeyondKnownPermissions(t *testing.T) {
	known := map[string]struct{}{
		GoatRead: {}, GoatWriteIdentity: {}, GoatWriteHealth: {},
		LocationsRead: {}, LocationsWrite: {}, LocationsReview: {}, LocationsRetire: {},
		OperatorsRead: {}, OperatorsWrite: {}, OperatorsActivate: {}, OperatorsDeactivate: {},
		OperatorsManageDevice: {}, OperatorsManageCapability: {},
		OperatorsManageRoster: {}, OperatorsViewAudit: {}, OperationsRepair: {}, AppBootstrap: {}, AdminWebBootstrap: {},
		SOPRead: {}, SOPWrite: {}, SOPPublish: {}, TaskRead: {}, TaskAssign: {}, TaskExecute: {}, TaskVerify: {},
		ProtocolRead: {}, ProtocolWrite: {}, ProtocolPublish: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationVerify: {}, VaccinationCampaign: {},
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {}, ProcurementWrite: {}, ProcurementReview: {},
		RosterRead: {}, RosterManage: {},
	}
	for tier, perms := range tierPermissions {
		for permission := range perms {
			if _, ok := known[permission]; !ok {
				t.Fatalf("tierPermissions[%q] references unknown permission %q", tier, permission)
			}
		}
	}
}

// TestOrgRoleCatalogHasEntryForEveryComposedRole is a compile-time-adjacent
// guard: every (tier, vertical) pair in AllTiers x AllVerticals must appear
// in the hand-written seed list mirrored here from
// migrations/postgres/000178_org_role_catalog.sql, so the Go tier catalog and
// the Postgres org_role_catalog seed cannot silently drift apart (one adds a
// vertical/tier without the other).
func TestOrgRoleCatalogHasEntryForEveryComposedRole(t *testing.T) {
	seeded := map[string]struct{}{
		"director_procurement": {}, "director_preventive_care": {}, "director_breeding": {},
		"director_health": {}, "director_growth": {}, "director_infrastructure": {},
		"director_feed": {}, "director_milk": {}, "director_sales": {},
		"head_procurement": {}, "head_preventive_care": {}, "head_breeding": {},
		"head_health": {}, "head_growth": {}, "head_infrastructure": {},
		"head_feed": {}, "head_milk": {}, "head_sales": {},
		"manager_procurement": {}, "manager_preventive_care": {}, "manager_breeding": {},
		"manager_health": {}, "manager_growth": {}, "manager_infrastructure": {},
		"manager_feed": {}, "manager_milk": {}, "manager_sales": {},
		"am_procurement": {}, "am_preventive_care": {}, "am_breeding": {},
		"am_health": {}, "am_growth": {}, "am_infrastructure": {},
		"am_feed": {}, "am_milk": {}, "am_sales": {},
	}
	if len(seeded) != len(AllTiers)*len(AllVerticals) {
		t.Fatalf("seeded fixture has %d entries, want %d (len(AllTiers)*len(AllVerticals))", len(seeded), len(AllTiers)*len(AllVerticals))
	}
	for _, tier := range AllTiers {
		for _, vertical := range AllVerticals {
			key := RoleKey(tier, vertical)
			if _, ok := seeded[key]; !ok {
				t.Fatalf("role key %q is composed by Go but missing from the migration 000174 seed list mirrored in this test", key)
			}
			if !IsKnownRole(key) {
				t.Fatalf("role key %q is not in rolePermissions after init()", key)
			}
		}
	}
}
