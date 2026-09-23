package app

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// EVERY COPY KEY THE HEALTH SCREENS NAME MUST BE IN THE SERVED CONTRACT.
//
// `copy()` THROWS on a missing key rather than rendering a blank, deliberately -- a silent blank
// label on an authority screen is worse than a loud failure. The consequence is that ONE missing
// key takes the whole page down, and it has happened twice on this screen in one day: the tab
// strip crashed /health/config against an older backend, and `action.save` was added to the wrong
// block, so the Add-a-type form opened to nothing at all.
//
// Both were missed because the check was a GREP OF THE SOURCE, which cannot tell which map literal
// a key landed in. This reads the SERVED bootstrap -- the thing the browser actually gets -- and
// the components' own source, so the two cannot drift.
func TestHealthConfigServesEveryCopyKeyItsScreensName(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleHealthDirector, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	page := pageByRouteID(t, resp.Pages, "health-config")

	served := map[string]bool{}
	for k := range page.Copy {
		served[k] = true
	}

	// The feature's own components, which are where the keys are named.
	dir := filepath.Join("..", "..", "..", "..", "apps", "admin-web", "features", "health")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("admin-web sources are not reachable from here: %v", err)
	}

	// `copy(pageContract, "x")` is the throwing form. optionalCopy returns nil instead, so a key
	// only ever read through it is allowed to be absent and is deliberately not collected.
	naming := regexp.MustCompile(`[^l]copy\(pageContract,\s*"([a-z0-9_.]+)"\)`)

	missing := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, ".test.mjs") ||
			(!strings.HasSuffix(name, ".tsx") && !strings.HasSuffix(name, ".ts")) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, m := range naming.FindAllStringSubmatch(string(src), -1) {
			if !served[m[1]] {
				missing[m[1]] = append(missing[m[1]], name)
			}
		}
	}

	if len(missing) > 0 {
		keys := make([]string, 0, len(missing))
		for k := range missing {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t.Errorf("health-config does not serve %q, named by %v -- the page will throw when that "+
				"control renders", k, missing[k])
		}
	}
}

// The three routing tables are on the page, so the screen has something to render them from.
func TestHealthConfigCarriesTheRoutingTables(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleHealthDirector, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	page := pageByRouteID(t, resp.Pages, "health-config")

	want := map[string]bool{"diagnosis-types": false, "diagnosis-routes": false, "diagnosis-gaps": false}
	for _, tbl := range page.Tables {
		if _, ok := want[tbl.ID]; ok {
			want[tbl.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("health-config serves no %q table", id)
		}
	}
}

// The routing columns speak the farm's words. humanLabel DERIVES a label from the column key, and
// the derivations here would put "Has published register" and "Route count" on a screen a vet reads.
func TestRoutingColumnsSpeakTheFarmsWords(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleHealthDirector, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	page := pageByRouteID(t, resp.Pages, "health-config")

	want := map[string]string{
		"route_count": "Stages", "has_published_register": "Rules written",
		"type_key": "Key", "type_label": "Type", "stage_label": "Stage",
		"sub_stage": "Cohort", "live_animals": "Animals now",
	}
	for _, tbl := range page.Tables {
		if !strings.HasPrefix(tbl.ID, "diagnosis-") {
			continue
		}
		for _, col := range tbl.Columns {
			if expect, ok := want[col.Key]; ok && col.Label != expect {
				t.Errorf("%s column %q reads %q, want %q", tbl.ID, col.Key, col.Label, expect)
			}
		}
	}
}
