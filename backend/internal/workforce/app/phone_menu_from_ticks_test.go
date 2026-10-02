package app

import (
	"sort"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// phoneFromTicks composes the phone exactly as Bootstrap does for a person with stored rows:
// the ticks are both the granted modules and the narrowing, and permissions come from them.
func phoneFromTicks(grants []domain.GrantSummary, rows []permissions.ModuleAssignment) map[string][]string {
	ticked := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Surface == permissions.SurfaceMobile {
			ticked = append(ticked, r.Module)
		}
	}
	scope := scopeOf(grants)
	scope.held = map[string]struct{}{}
	for _, p := range permissions.PermissionsForAssignmentsWithBaseline(rows) {
		scope.held[p] = struct{}{}
	}
	out := map[string][]string{}
	for _, m := range modulesForScope(scope, ticked, "en", true, ticked) {
		items := []string{}
		for _, it := range m.NavItems {
			items = append(items, it.Key)
		}
		sort.Strings(items)
		out[m.Key] = items
	}
	return out
}

// TestThePhoneMenuIsThePersonsTicksWhateverTheirRole is bug 6 of the People / HRMS fixes
// (2026-10-02): a person added with a "leadership" role (Feed / Growth / PC / Health / Breeding
// Director, Park Head) got only that role's own modules on the phone, narrowed by their ticks --
// so a sales hire added as Feed Director and ticked for the buyers and the market survey, saw only
// Clock. The ticks are the answer for every principal now.
func TestThePhoneMenuIsThePersonsTicksWhateverTheirRole(t *testing.T) {
	mohsin := []permissions.ModuleAssignment{
		{Module: "vendors_sales", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelView, permissions.LevelDo}},
		{Module: "market_survey", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelDo}},
	}
	for _, role := range []string{permissions.RoleFeedDirector, permissions.RoleParkHead, permissions.RoleKey(permissions.TierDirector, permissions.VerticalSales)} {
		t.Run(role, func(t *testing.T) {
			got := phoneFromTicks([]domain.GrantSummary{grantWithRole(role)}, mohsin)
			sales, ok := got["sales"]
			if !ok {
				t.Fatalf("%s ticked for the buyers and the market survey sees %v; want the Sales module", role, got)
			}
			// Item 7: the three Sales tabs are given one at a time -- no ledger without its tick.
			if len(sales) != 2 || sales[0] != "market" || sales[1] != "sales_vendors" {
				t.Fatalf("Sales tabs = %v, want [market sales_vendors] (no ledger tab without the Sales tick)", sales)
			}
			if _, leak := got["feed_direction"]; leak {
				t.Fatalf("the role's own module came back without a tick: %v", got)
			}
			if _, ok := got[clockModuleKey]; !ok {
				t.Fatalf("Clock is baseline for everyone; got %v", got)
			}
			if _, ok := got["vendors"]; ok {
				t.Fatalf("the suppliers (Procurement module) must not appear for a buyers-only person: %v", got)
			}
		})
	}
}

// TestDirectorsStillGetNoPhoneWorkBoardFromTicks: the 2026-09-25 exclusion survives -- a ticked
// work board does not put My Work on a director's phone.
func TestDirectorsStillGetNoPhoneWorkBoardFromTicks(t *testing.T) {
	rows := []permissions.ModuleAssignment{
		{Module: "work_board", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelView}},
		{Module: "sales", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelView}},
	}
	got := phoneFromTicks([]domain.GrantSummary{grantWithRole(permissions.RoleFeedDirector)}, rows)
	if _, ok := got["work_board"]; ok {
		t.Fatalf("a director's phone must not carry My Work: %v", got)
	}
	if _, ok := got["sales"]; !ok {
		t.Fatalf("the ticked Sales module is missing: %v", got)
	}
}
