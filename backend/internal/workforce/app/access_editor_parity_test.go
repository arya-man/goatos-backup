package app

import (
	"sort"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// openableFromScreen is the access editor's rule, written against the editor PAYLOAD only
// (moduleRows): a screen is openable when every permission it needs is granted by some WEB
// level ticked on screen. The admin-web modal applies exactly this
// (features/people/access-pages.ts); keeping a Go copy here lets the backend tests prove the
// payload carries what the rule needs and that the save agrees with it.
func openableFromScreen(rows []domain.AccessModuleRow, webTicks map[string][]string, module string) []string {
	held := map[string]struct{}{}
	for _, row := range rows {
		for _, level := range webTicks[row.ModuleKey] {
			for _, p := range row.WebLevelPermissions[level] {
				held[p] = struct{}{}
			}
		}
	}
	var out []string
	for _, row := range rows {
		if row.ModuleKey != module {
			continue
		}
		for _, page := range row.Pages {
			ok := true
			for _, need := range page.RequiredPermissions {
				if _, has := held[need]; !has {
					ok = false
					break
				}
			}
			if ok {
				out = append(out, page.PageKey)
			}
		}
	}
	return out
}

func writesFrom(webTicks map[string][]string, mobileTicks map[string][]string, pages map[string][]string) []domain.AccessModuleWrite {
	keys := map[string]struct{}{}
	for k := range webTicks {
		keys[k] = struct{}{}
	}
	for k := range mobileTicks {
		keys[k] = struct{}{}
	}
	out := make([]domain.AccessModuleWrite, 0, len(keys))
	for k := range keys {
		out = append(out, domain.AccessModuleWrite{ModuleKey: k, Web: webTicks[k], Mobile: mobileTicks[k], Pages: pages[k]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModuleKey < out[j].ModuleKey })
	return out
}

// TestAModuleSwitchedOnForTheFirstTimeOffersItsScreensAndSavesInOneGo is bugs 2 and 3 of the
// People / HRMS fixes (2026-10-02): for someone with no saved access, switching Sales on for
// the web showed no screens to tick, and the save was refused "Sales needs at least one screen
// ticked" -- the editor's screens came from the STORED rows, the save's from the payload. The
// workaround was two saves, phone first.
func TestAModuleSwitchedOnForTheFirstTimeOffersItsScreensAndSavesInOneGo(t *testing.T) {
	rows := moduleRows(nil) // a brand-new person: nothing saved
	web := map[string][]string{"sales": {permissions.LevelView}}
	pages := openableFromScreen(rows, web, "sales")
	if len(pages) == 0 {
		t.Fatal("switching Sales on for the web offers no screens to tick")
	}
	for _, want := range []string{"sales-sold", "sales-market-analytics"} {
		found := false
		for _, p := range pages {
			found = found || p == want
		}
		if !found {
			t.Errorf("Sales at view should offer %s, offered %v", want, pages)
		}
	}
	assignments, err := validatedAssignments(writesFrom(web, nil, map[string][]string{"sales": pages}))
	if err != nil {
		t.Fatalf("the first save of a newly switched-on module was refused: %v", err)
	}
	got := permissions.PageAccessForAssignments(assignments)
	for _, p := range pages {
		if _, ok := got.Pages[p]; !ok {
			t.Errorf("%s was ticked and saved but the sidebar does not show it", p)
		}
	}
}

// TestEditorOffersExactlyTheScreensTheSidebarShows is bug 5: the editor counted PHONE ticks
// when offering screens and the sidebar counted WEB ticks only, so a screen could be offered,
// ticked and saved and then never appear. The rule is now one: web ticks open web screens.
// For each case the screens the editor offers, the screens the save accepts, and the screens
// the sidebar shows must be the same set.
func TestEditorOffersExactlyTheScreensTheSidebarShows(t *testing.T) {
	cases := []struct {
		name   string
		web    map[string][]string
		mobile map[string][]string
		module string
	}{
		// The STG sales-hire case: Sales on the web, the vendor register only on the phone.
		{"sales web, vendors phone only", map[string][]string{"sales": {permissions.LevelView}}, map[string][]string{"vendors": {permissions.LevelView}}, "sales"},
		// Feed configured on the phone, only viewed on the web: Feed Config is not a web screen.
		{"feed configure on phone, view on web", map[string][]string{"feed_direction": {permissions.LevelView}}, map[string][]string{"feed_direction": {permissions.LevelConfigure}}, "feed_direction"},
		{"sales and vendors both on the web", map[string][]string{"sales": {permissions.LevelView}, "vendors": {permissions.LevelView}}, nil, "sales"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := moduleRows(nil)
			offered := openableFromScreen(rows, tc.web, tc.module)
			// The editor fills every web-held module with its offered screens, not just this one.
			allPages := map[string][]string{}
			for m := range tc.web {
				allPages[m] = openableFromScreen(rows, tc.web, m)
			}
			assignments, err := validatedAssignments(writesFrom(tc.web, tc.mobile, allPages))
			if err != nil {
				t.Fatalf("saving every offered screen was refused: %v", err)
			}
			sidebar := permissions.PageAccessForAssignments(assignments)
			var shown []string
			for _, p := range permissions.PagesForModule(tc.module) {
				if _, ok := sidebar.Pages[p.Key]; ok {
					shown = append(shown, p.Key)
				}
			}
			sort.Strings(offered)
			sort.Strings(shown)
			if len(offered) != len(shown) {
				t.Fatalf("editor offers %v, sidebar shows %v", offered, shown)
			}
			for i := range offered {
				if offered[i] != shown[i] {
					t.Fatalf("editor offers %v, sidebar shows %v", offered, shown)
				}
			}
			// And a screen the editor did NOT offer must be refused, never stored and dropped.
			for _, p := range permissions.PagesForModule(tc.module) {
				isOffered := false
				for _, o := range offered {
					isOffered = isOffered || o == p.Key
				}
				if isOffered {
					continue
				}
				withExtra := map[string][]string{}
				for m, ps := range allPages {
					withExtra[m] = ps
				}
				withExtra[tc.module] = append(append([]string{}, offered...), p.Key)
				if _, err := validatedAssignments(writesFrom(tc.web, tc.mobile, withExtra)); err == nil {
					t.Errorf("%s was not offered but the save accepted it", p.Key)
				}
			}
		})
	}
}
