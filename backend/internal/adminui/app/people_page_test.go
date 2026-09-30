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

// TestPeoplePagesAreSplitOutOfTheTabStrip pins the HRMS split (maintainer request 2026-09-30):
// /people is the ALL-PEOPLE directory alone, and each view that was a tab of it -- Clock In /
// Out, Notifications, Vaccination operators -- is its own page contract at its own route with
// its own table. The old tab strip (people_view_tabs) and its "coming soon" placeholders are
// gone; the Add Person form's role set survives on every People page.
func TestPeoplePagesAreSplitOutOfTheTabStrip(t *testing.T) {
	want := map[string]struct{ href, table, source string }{
		"people":               {"/people", "people", "/admin/workforce/people"},
		"people-clock":         {"/people/clock", "clock-entries", "/admin/workforce/clock-entries"},
		"people-notifications": {"/people/notifications", "notification-audiences", "/admin/notifications/designations"},
		"people-vaccination":   {"/people/vaccination", "positions", "/admin/roster/positions"},
		"people-timetable":     {"/people/timetable", "timetable-people", "/admin/workforce/timetable"},
	}
	for id, w := range want {
		page := pageByRouteID(t, pages(), id)
		if page.Href != w.href {
			t.Fatalf("%s href = %q, want %q", id, page.Href, w.href)
		}
		if len(page.Tables) != 1 || page.Tables[0].ID != w.table || page.Tables[0].DataSource != w.source {
			t.Fatalf("%s must carry exactly its own table %s over %s, got %#v", id, w.table, w.source, page.Tables)
		}
	}
	for _, id := range []string{"people", "people-clock", "people-notifications", "people-vaccination"} {
		var roles []string
		for _, group := range pageOptionGroups(id) {
			if group.ID == "people_view_tabs" {
				t.Fatalf("%s: the retired people_view_tabs strip must not come back", id)
			}
			if group.ID == "people_roles" {
				for _, opt := range group.Options {
					roles = append(roles, opt.Key)
					if opt.Title != "park" && opt.Title != "tenant" {
						t.Fatalf("role option %q must carry its scope shape in Title, got %q", opt.Key, opt.Title)
					}
				}
			}
		}
		if len(roles) == 0 {
			t.Fatalf("%s: people_roles must declare the grantable role set", id)
		}
		for _, role := range roles {
			if role == permissions.RoleCEOInternal {
				t.Fatalf("ceo_internal must never be grantable from the Add Person form")
			}
		}
		if pageSpecificCopy(id)["crumb"] != "HRMS" {
			t.Fatalf("%s must share the People copy map (crumb HRMS)", id)
		}
	}
}
