package permissions

import "testing"

// TestConfigurationIsCEOOnlyOnTheRole pins the access decision for Configuration -> Items and
// settings (maintainer instruction 2026-09-18): ceo_internal holds read + write on the role;
// no other role holds either, so anyone else reaches the page only by a per-person tick on
// /people. Both ticks resolve as declared: View is the read, Configure adds the write.
//
// Mutation-tested when written: granting ConfigurationRead to RolePCDirector turns this red.
func TestConfigurationIsCEOOnlyOnTheRole(t *testing.T) {
	if !RoleHasPermission(RoleCEOInternal, ConfigurationRead) || !RoleHasPermission(RoleCEOInternal, ConfigurationWrite) {
		t.Fatal("ceo_internal must hold configuration.read and configuration.write")
	}
	for _, role := range []string{RoleParkHead, RolePCDirector, RoleBreedingDirector, RoleGrowthDirector, RoleFeedDirector, RoleHealthDirector, RoleProcurementDirector, RoleOperator, RoleVerifier, RoleCountsApprover, RoleToxinTester, RoleProcurementManager} {
		if RoleHasPermission(role, ConfigurationRead) || RoleHasPermission(role, ConfigurationWrite) {
			t.Errorf("%s must NOT hold configuration.* on the role", role)
		}
	}
	for _, tc := range []struct {
		level string
		read  bool
		write bool
	}{{LevelView, true, false}, {LevelConfigure, true, true}} {
		perms := PermissionsForAssignments([]ModuleAssignment{{Surface: SurfaceWeb, Module: "configuration", Capabilities: []string{tc.level}}})
		gotRead, gotWrite := false, false
		for _, p := range perms {
			if p == ConfigurationRead {
				gotRead = true
			}
			if p == ConfigurationWrite {
				gotWrite = true
			}
		}
		if gotRead != tc.read || gotWrite != tc.write {
			t.Errorf("configuration tick %s carries read=%v write=%v, want %v/%v", tc.level, gotRead, gotWrite, tc.read, tc.write)
		}
	}
	if ModuleSupportsSurface("configuration", SurfaceMobile) {
		t.Error("configuration is web-only; the phone renders these lists, it never authors them")
	}
}

// TestConfigurationRoutesAreGatedOnReadAndWrite pins the route-table half of the capability
// lock: every read on configuration.read, every write on configuration.write.
func TestConfigurationRoutesAreGatedOnReadAndWrite(t *testing.T) {
	want := map[string][]string{
		"GET /admin/configuration/registers":                   {ConfigurationRead},
		"GET /admin/configuration/{register}":                  {ConfigurationRead},
		"POST /admin/configuration/{register}":                 {ConfigurationWrite},
		"GET /admin/configuration/{register}/options":          {ConfigurationRead},
		"GET /admin/configuration/{register}/{row_id}":         {ConfigurationRead},
		"PUT /admin/configuration/{register}/{row_id}":         {ConfigurationWrite},
		"DELETE /admin/configuration/{register}/{row_id}":      {ConfigurationWrite},
		"GET /admin/configuration/{register}/{row_id}/usage":   {ConfigurationRead},
		"POST /admin/configuration/{register}/{row_id}/status": {ConfigurationWrite},
	}
	found := map[string]bool{}
	for _, route := range ProtectedRoutes() {
		key := route.Method + " " + route.Pattern
		expected, ok := want[key]
		if !ok {
			continue
		}
		found[key] = true
		if len(route.Permissions) != 1 || route.Permissions[0] != expected[0] {
			t.Fatalf("%s gated on %v want %v", key, route.Permissions, expected)
		}
	}
	for key := range want {
		if !found[key] {
			t.Fatalf("route %s missing from the permission table", key)
		}
	}
}
