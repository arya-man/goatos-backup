package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

func hasModule(mods []domain.BootstrapModule, key string) bool {
	for _, m := range mods {
		if m.Key == key {
			return true
		}
	}
	return false
}

// TestNoPhoneWorkBoardForCXOsAndDirectors pins the maintainer decision of 2026-09-25 ("no mobile
// view for CXOs and directors"): the phone's My Work reads one park and has no park picker, so a
// CEO/CXO or a director never gets it -- not from the leadership curation, not from a department
// grant, not from their own ticks -- while a park head (a director who is also one included) and a
// field principal still do.
func TestNoPhoneWorkBoardForCXOsAndDirectors(t *testing.T) {
	for _, role := range []string{
		permissions.RoleCEOInternal, permissions.RolePCDirector, permissions.RoleGrowthDirector,
		permissions.RoleFeedDirector, permissions.RoleHealthDirector, permissions.RoleProcurementDirector,
		permissions.RoleBreedingDirector,
	} {
		grants := []domain.GrantSummary{grantWithRole(role)}
		for _, fromTicks := range []bool{false, true} {
			if hasModule(modulesForFrom(grants, []string{"work_board"}, "en", fromTicks), "work_board") {
				t.Errorf("%s (fromTicks=%v) is offered My Work on the phone", role, fromTicks)
			}
		}
		// Inclusive: a director who is ALSO a park head works the board on the phone as the park
		// head (maintainer correction 2026-09-25 -- Dinakar and Chandrakant have it in both places).
		both := append(grants, grantWithRole(permissions.RoleParkHead))
		if !hasModule(modulesFor(both, []string{"work_board"}, "en"), "work_board") {
			t.Errorf("%s + park_head lost My Work on the phone", role)
		}
	}
	if !hasModule(modulesFor([]domain.GrantSummary{grantWithRole(permissions.RoleParkHead)}, nil, "en"), "work_board") {
		t.Error("a park head lost My Work on the phone")
	}
}
