package permissions

import "testing"

// The Health Config authority boundary (maintainer decision 2026-08-06,
// docs/decisions/health-config-authoring.md).
//
// A dosage is an instruction a field operator administers to an animal without re-deriving it, so
// who may change one is the highest-consequence question in the Health module. These tests pin the
// answer in both directions: exactly two roles hold the write, and the roles that must NOT hold it
// are named individually so a future grant cannot widen it by accident.

func TestHealthConfigWriteIsCEOAndHealthDirectorOnly(t *testing.T) {
	want := map[string]bool{
		RoleCEOInternal:    true,
		RoleHealthDirector: true,
	}
	for role, grants := range rolePermissions {
		_, has := grants[HealthConfigWrite]
		if has != want[role] {
			if has {
				t.Fatalf("role %q must NOT hold %s — authoring a treatment protocol is a clinical authority, see docs/decisions/health-config-authoring.md", role, HealthConfigWrite)
			}
			t.Fatalf("role %q must hold %s", role, HealthConfigWrite)
		}
	}
}

// Read follows write here, plus nobody else. A principal who can inspect the standing dosages but
// not change them is a legitimate future case, but it is not granted today and must be a deliberate
// decision rather than a side effect.
func TestHealthConfigReadIsCEOAndHealthDirectorOnly(t *testing.T) {
	for role, grants := range rolePermissions {
		_, hasRead := grants[HealthConfigRead]
		expected := role == RoleCEOInternal || role == RoleHealthDirector
		if hasRead != expected {
			t.Fatalf("role %q: health.config.read = %v, want %v", role, hasRead, expected)
		}
	}
}

// The named exclusions, each for its own reason. Listed explicitly rather than derived, so the
// reason survives in the failure message when someone tries to add one.
func TestHealthConfigWriteIsWithheldFromTheRolesThatMustNotHaveIt(t *testing.T) {
	cases := []struct {
		role   string
		reason string
	}{
		{RoleOperator, "an operator executes a course; executing is not authoring"},
		{RoleParkHead, "a park head runs a park's execution, not the tenant-wide clinical standard"},
		{RolePCDirector, "Preventive Care and Health are SEPARATE departments and merging them is prohibited"},
		{RoleVerifier, "separation of duty: the verifier must not rewrite the standard the work is judged against"},
		{RoleGrowthDirector, "Weighing and ONLY Weighing"},
		{RoleFeedDirector, "the feed chain and only the feed chain"},
	}
	for _, tc := range cases {
		grants, ok := rolePermissions[tc.role]
		if !ok {
			t.Fatalf("role %q is not in the catalog", tc.role)
		}
		if _, has := grants[HealthConfigWrite]; has {
			t.Fatalf("role %q must not hold %s — %s", tc.role, HealthConfigWrite, tc.reason)
		}
	}
}

// health_director gains HEALTH authority and nothing else. The counts-ownership-without-access
// property recorded in AGENTS.md must survive this change: granting counts.read here would light
// the Counts nav and thereby switch on a feature that is deliberately off.
func TestHealthDirectorGainsHealthAuthorityWithoutCountsAccessOrVaccination(t *testing.T) {
	grants := rolePermissions[RoleHealthDirector]

	for _, required := range []string{HealthConfigRead, HealthConfigWrite, GoatWriteHealth} {
		if _, has := grants[required]; !has {
			t.Fatalf("health_director must hold %s", required)
		}
	}
	// Counts stays OFF for this role: ownership is not access.
	for _, forbidden := range []string{CountsRead, CountsWrite} {
		if _, has := grants[forbidden]; has {
			t.Fatalf("health_director must NOT hold %s — Counts is an OFF feature and this grant would enable it (AGENTS.md)", forbidden)
		}
	}
	// And no Preventive Care authority: authoring VACCINATION protocol rules stays on /config
	// behind ProtocolWrite, which is pc_director/CEO territory.
	for _, forbidden := range []string{ProtocolWrite, ProtocolPublish} {
		if _, has := grants[forbidden]; has {
			t.Fatalf("health_director must NOT hold %s — pc_director and health_director are separate departments", forbidden)
		}
	}
}

// The routes. Reads carry the read permission; every write carries the write one — INCLUDING the
// draft-open route, which reads like a read but CREATES a draft row when none is open.
func TestHealthConfigRoutesCarryTheRightPermission(t *testing.T) {
	cases := []struct {
		method, pattern, want string
	}{
		{"GET", "/health-config/protocols", HealthConfigRead},
		{"GET", "/health-config/protocols/{protocol_version_id}", HealthConfigRead},
		{"POST", "/health-config/diseases", HealthConfigWrite},
		// Opening a draft copies the published version into a new row. It is a write.
		{"POST", "/health-config/drafts", HealthConfigWrite},
		{"POST", "/health-config/drafts/save", HealthConfigWrite},
		{"POST", "/health-config/protocols/{protocol_version_id}/publish", HealthConfigWrite},
		{"POST", "/health-config/protocols/{protocol_version_id}/discard", HealthConfigWrite},
	}
	for _, tc := range cases {
		route, ok := Match(tc.method, tc.pattern)
		if !ok {
			t.Fatalf("%s %s is not registered — an unregistered route 403s for everyone", tc.method, tc.pattern)
		}
		if len(route.Permissions) != 1 || route.Permissions[0] != tc.want {
			t.Fatalf("%s %s permissions=%v, want exactly [%s]", tc.method, tc.pattern, route.Permissions, tc.want)
		}
	}
}

// The operator's treatment WORK and the authored RULEBOOK are separate authorities. An operator
// must see the steps for the case in front of them and must not be able to edit the standard.
func TestOperatorReadsTheirWorkButNotTheRulebook(t *testing.T) {
	grants := rolePermissions[RoleOperator]
	if _, has := grants[HealthRead]; !has {
		t.Fatal("an operator must keep health.read — that is the work queue for the animal in front of them")
	}
	if _, has := grants[HealthConfigRead]; has {
		t.Fatal("an operator must not hold health.config.read — the rulebook is a separate authority from the work")
	}
}
