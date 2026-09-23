package permissions

import "testing"

// TestCEOFloorReachesEveryWebModuleAndPage is the catalog half of the CEO/CXO floor
// (ceo_floor.go). It is the build-time check that a module or page shipped tomorrow cannot
// leave the CEO's sidebar: the role must open every page, and the CEO row map -- what a
// fresh backfill or a new CEO's designation default writes -- must hold every web module and
// tick every page.
//
// Mutation-tested when written: removing the pc_trimming row from RoleCEOInternal's rows
// turns the module check red, and dropping ObligationRead from the ceo_internal role turns
// the page check red.
func TestCEOFloorReachesEveryWebModuleAndPage(t *testing.T) {
	if !CEOFloorApplies([]string{RoleParkHead, RoleCEOInternal}) || CEOFloorApplies([]string{RolePCDirector}) {
		t.Fatal("CEOFloorApplies must key on ceo_internal and nothing else")
	}

	for _, page := range ModulePages() {
		for _, required := range page.Permissions {
			if !RoleHasPermission(RoleCEOInternal, required) {
				t.Errorf("page %q (%s) needs %s, which the ceo_internal ROLE does not hold; grant it in permissions.go", page.Key, page.Href, required)
			}
		}
	}

	rows := AssignmentsForRoles([]string{RoleCEOInternal})
	webRows := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.Surface == SurfaceWeb {
			webRows[row.Module] = struct{}{}
		}
	}
	for _, mod := range ModuleCapabilities() {
		if !ModuleSupportsSurface(mod.Key, SurfaceWeb) {
			continue
		}
		if _, ok := webRows[mod.Key]; !ok {
			t.Errorf("web module %q has no ceo_internal row in roleAssignments; a migrated CEO would hold none of it", mod.Key)
		}
	}

	access := PageAccessForAssignments(FillDefaultPages(rows))
	for _, page := range ModulePages() {
		if _, ok := access.Pages[page.Key]; !ok {
			t.Errorf("page %q (%s) is not ticked by the ceo_internal row map", page.Key, page.Href)
		}
	}
}
