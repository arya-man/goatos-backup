package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestPeopleNavLeafIsGatedOnOperatorsRead pins the fix for the nav-leak found
// during the 2026-08-22 People/HRMS rewrite: permissionsForNav("people") used
// to fall through to the nil default, so the People leaf rendered enabled for
// ANY principal holding admin_web.bootstrap. Written red-first against the old
// code (it returned nil).
func TestPeopleNavLeafIsGatedOnOperatorsRead(t *testing.T) {
	required := permissionsForNav("people")
	if len(required) != 1 || required[0] != permissions.OperatorsRead {
		t.Fatalf("permissionsForNav(people) = %v, want [%s]", required, permissions.OperatorsRead)
	}
}

// TestPeoplePageContractCarriesDirectoryAndTabs pins the rewrite's page shape:
// the default table is the ALL-PEOPLE directory over /admin/workforce/people,
// the vaccination positions table survives for the Vaccination tab, and the
// backend owns the tab strip (people_view_tabs) with the module placeholders
// DISABLED and carrying a reason.
func TestPeoplePageContractCarriesDirectoryAndTabs(t *testing.T) {
	var people *struct {
		hasPeopleTable    bool
		hasPositionsTable bool
	}
	for _, page := range pages() {
		if page.RouteID != "people" {
			continue
		}
		state := struct {
			hasPeopleTable    bool
			hasPositionsTable bool
		}{}
		for _, tbl := range page.Tables {
			switch tbl.ID {
			case "people":
				state.hasPeopleTable = true
				if tbl.DataSource != "/admin/workforce/people" {
					t.Fatalf("people table source = %q, want /admin/workforce/people", tbl.DataSource)
				}
			case "positions":
				state.hasPositionsTable = true
			case "timetable":
				t.Fatalf("the dead timetable table contract must stay deleted")
			}
		}
		people = &state
	}
	if people == nil {
		t.Fatalf("people page contract not found")
	}
	if !people.hasPeopleTable || !people.hasPositionsTable {
		t.Fatalf("people page must carry both the directory and positions tables: %+v", *people)
	}

	groups := pageOptionGroups("people")
	var tabs, roles []string
	disabledWithReason := 0
	for _, group := range groups {
		switch group.ID {
		case "people_view_tabs":
			for _, opt := range group.Options {
				tabs = append(tabs, opt.Key)
				if !opt.Enabled {
					if opt.DisabledReason == "" {
						t.Fatalf("disabled tab %q must carry a reason", opt.Key)
					}
					disabledWithReason++
				}
			}
		case "people_roles":
			for _, opt := range group.Options {
				roles = append(roles, opt.Key)
				if opt.Title != "park" && opt.Title != "tenant" {
					t.Fatalf("role option %q must carry its scope shape in Title, got %q", opt.Key, opt.Title)
				}
			}
		}
	}
	wantEnabled := map[string]bool{"all": true, "vaccination": true}
	for _, group := range groups {
		if group.ID != "people_view_tabs" {
			continue
		}
		for _, opt := range group.Options {
			if wantEnabled[opt.Key] && !opt.Enabled {
				t.Fatalf("tab %q must be enabled", opt.Key)
			}
		}
	}
	if len(tabs) < 4 || disabledWithReason == 0 {
		t.Fatalf("people_view_tabs must declare the module strip with disabled placeholders, got %v", tabs)
	}
	// people_roles is declared EMPTY on the static contract: compilePeopleRoles fills it from
	// the designation catalog (TestAddPersonRolesComeFromTheDesignationCatalog).
	if len(roles) != 0 {
		t.Fatalf("people_roles must not carry a constant role list; it is compiled from designation_catalog, got %v", roles)
	}
}

// TestAddPersonRolesComeFromTheDesignationCatalog: the Add Person role dropdown is the
// designation catalog, filtered to roles the form may grant, each carrying its scope shape.
// The old literal list left out Sales Director, Sales Manager, Procurement Director / Manager
// and HR (People / HRMS fixes, 2026-10-02).
func TestAddPersonRolesComeFromTheDesignationCatalog(t *testing.T) {
	catalog := []ReferenceOption{
		{Key: "ceo_internal", Label: "CEO / CXO"},
		{Key: "director_sales", Label: "Sales Director"},
		{Key: "procurement_director", Label: "Procurement Director"},
		{Key: "hr", Label: "HR"},
		{Key: "park_head", Label: "Park Head"},
		{Key: "manager_sales", Label: "Sales Manager"},
		{Key: "operator", Label: "Operator"},
		{Key: "made_up_title", Label: "Not a role"},
	}
	got := compilePeopleRoles(catalog)
	want := []struct{ key, label, scope string }{
		{"director_sales", "Sales Director", "tenant"},
		{"procurement_director", "Procurement Director", "tenant"},
		{"hr", "HR", "tenant"},
		{"park_head", "Park Head", "park"},
		{"manager_sales", "Sales Manager", "park"},
	}
	if len(got) != len(want) {
		t.Fatalf("people_roles = %+v, want %d options", got, len(want))
	}
	for i, w := range want {
		if got[i].Key != w.key || got[i].Label != w.label || got[i].Title != w.scope {
			t.Errorf("option %d = {%s %s %s}, want {%s %s %s}", i, got[i].Key, got[i].Label, got[i].Title, w.key, w.label, w.scope)
		}
	}
	for _, opt := range got {
		if opt.Key == permissions.RoleCEOInternal {
			t.Fatalf("ceo_internal must never be grantable from the Add Person form")
		}
	}
}
