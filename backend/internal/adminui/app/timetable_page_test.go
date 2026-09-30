package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// HRMS Timetable (maintainer request 2026-09-30, maintainer answer the same day: HR and the
// CEO/CXO edit; park heads get a phone editor later). The page opens on the timetable READ and
// its two write controls ride the timetable WRITE. HR reaches the page WITHOUT the staff
// directory -- the reason Timetable is its own module rather than a level on People.
func TestTimetableIsEditedByHRAndTheCEOOnly(t *testing.T) {
	for _, tc := range []struct {
		name      string
		role      string
		reachable bool
		editable  bool
	}{
		{"ceo_internal", permissions.RoleCEOInternal, true, true},
		{"hr", permissions.RoleHR, true, true},
		// The mutation test for gating the controls on a broader key: the PC Director opens the
		// directory (OperatorsRead) and must still neither reach nor edit the timetable.
		{"pc_director", permissions.RolePCDirector, false, false},
		{"park_head", permissions.RoleParkHead, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := permissions.RoleHasPermission(tc.role, permissions.WorkforceTimetableRead); got != tc.reachable {
				t.Fatalf("%s timetable read = %v, want %v", tc.role, got, tc.reachable)
			}
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: "00000000-0000-4000-8000-000000000001",
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants: []permissions.ActiveGrant{
					{Role: tc.role, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
				},
			})
			page := optionalPageByRouteID(resp.Pages, "people-timetable")
			if page == nil {
				if tc.editable {
					t.Fatalf("%s must receive the timetable page contract", tc.role)
				}
				return
			}
			for _, id := range []string{"edit_person_shift", "edit_shift_timing"} {
				control := controlByID(t, page.Controls, id)
				if control.Enabled != tc.editable {
					t.Fatalf("%s %s enabled = %v, want %v", tc.role, id, control.Enabled, tc.editable)
				}
				if !control.Enabled && control.DisabledReason == "" {
					t.Fatalf("%s %s is disabled without a reason", tc.role, id)
				}
			}
		})
	}
	if got := permissions.RoleHasPermission(permissions.RoleHR, permissions.OperatorsRead); got {
		t.Fatal("HR must reach the timetable without the staff directory (OperatorsRead)")
	}
	if got := permissionsForNav("people-timetable"); len(got) != 1 || got[0] != permissions.WorkforceTimetableRead {
		t.Fatalf("permissionsForNav(people-timetable) = %v, want [%s]", got, permissions.WorkforceTimetableRead)
	}
}
