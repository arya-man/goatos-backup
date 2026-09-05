package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

func healthAnalyticsPage(t *testing.T) (page struct {
	Title       string
	Subtitle    string
	SurfaceKind string
	Href        string
	Tables      map[string][]string
	Copy        map[string]string
}) {
	t.Helper()
	bootstrap := NewService().Bootstrap(context.Background(), BootstrapInput{})
	for _, p := range bootstrap.Pages {
		if p.RouteID != "health-analytics" {
			continue
		}
		page.Title = p.Title
		page.Subtitle = p.Subtitle
		page.SurfaceKind = p.SurfaceKind
		page.Href = p.Href
		page.Tables = map[string][]string{}
		for _, table := range p.Tables {
			cols := make([]string, 0, len(table.Columns))
			for _, col := range table.Columns {
				cols = append(cols, col.Key)
			}
			page.Tables[table.ID] = cols
		}
		page.Copy = p.Copy
		return page
	}
	t.Fatal("no health-analytics page contract is served")
	return page
}

// The screen exists, is a MODULE SURFACE (it authors nothing, so it needs no
// entry in the Config allowlist), and is named exactly what the sidebar says.
func TestHealthAnalyticsPageIsAModuleSurface(t *testing.T) {
	page := healthAnalyticsPage(t)
	if page.Title != "Health Analytics" {
		t.Errorf("title = %q, want %q", page.Title, "Health Analytics")
	}
	if page.SurfaceKind != "module-surface" {
		t.Errorf("surface kind = %q, want module-surface -- an authority screen would need a recorded Config exception", page.SurfaceKind)
	}
	if page.Href != "/health/analytics" {
		t.Errorf("href = %q, want /health/analytics", page.Href)
	}
}

// THE HONEST DISCLOSURE IS PART OF THE CONTRACT, not a nicety.
//
// Goat OS now records a coded cause of death when the death form captured one,
// while older deaths still have only the legacy open-case inference. The banner
// has to say both pieces: a reader who does not know that split would read the
// unattributed column as missing data rather than as the detection gap it is.
//
// Pinned because it is the kind of sentence a later copy pass deletes for being
// long, which would leave the chart making a claim the data cannot support.
func TestHealthAnalyticsBannerDeclaresRecordedAndLegacyAttributionBasis(t *testing.T) {
	page := healthAnalyticsPage(t)
	banner := page.Copy["banner.basis"]
	if banner == "" {
		t.Fatal("no banner.basis copy: the attribution limit must be stated on the page")
	}
	for _, phrase := range []string{"death form", "older deaths", "open case", "not attributed"} {
		if !strings.Contains(strings.ToLower(banner), phrase) {
			t.Errorf("banner.basis does not say %q; it reads %q", phrase, banner)
		}
	}
}

// An unattributed death has NO disease, and the page must carry its own word for
// that rather than leaving a renderer to compose one -- a client-side "Unknown"
// is exactly the copy-firewall break this contract exists to stop.
func TestHealthAnalyticsCarriesCopyForTheUnattributedDeath(t *testing.T) {
	page := healthAnalyticsPage(t)
	for _, key := range []string{
		"label.attributed", "label.unattributed", "label.never",
		"series.attributed", "series.unattributed",
		"kpi.unattributed.label", "stat.never.label", "stat.never.sub", "stat.attributed.sub",
	} {
		if strings.TrimSpace(page.Copy[key]) == "" {
			t.Errorf("copy %q is missing; the renderer would have to invent it", key)
		}
	}
}

// The tables' columns are the data contract. Named explicitly so a column cannot
// be dropped from the backend and quietly disappear from the screen.
func TestHealthAnalyticsDeclaresItsFourTables(t *testing.T) {
	page := healthAnalyticsPage(t)
	want := map[string][]string{
		"health-disease-board": {"disease", "age_band", "new_cases", "open_cases", "recovered", "died", "case_fatality"},
		"health-deaths":        {"animal", "pen", "date", "age_band", "attribution", "days_treated"},
		"health-medicines":     {"medicine", "route", "doses", "animals"},
		"health-engine-rules":  {"rule", "proposed", "opened", "not_taken_up"},
	}
	for id, cols := range want {
		got, ok := page.Tables[id]
		if !ok {
			t.Errorf("table %q is not declared", id)
			continue
		}
		if strings.Join(got, ",") != strings.Join(cols, ",") {
			t.Errorf("table %q columns = %v, want %v", id, got, cols)
		}
	}
}

// The leaf sits in Others, directly above Health Config (maintainer request).
// Order is asserted rather than mere presence: "on top of Health Config" is the
// instruction, and a leaf appended to the end of the group satisfies presence
// while ignoring it.
func TestHealthAnalyticsLeafSitsDirectlyAboveHealthConfig(t *testing.T) {
	for _, group := range navigation().Groups {
		if group.ID != "others" {
			continue
		}
		for i, leaf := range group.Leaves {
			if leaf.ID != "health-analytics" {
				continue
			}
			if leaf.Label != "Health Analytics" || leaf.Href != "/health/analytics" {
				t.Fatalf("leaf = %q -> %q, want Health Analytics -> /health/analytics", leaf.Label, leaf.Href)
			}
			if i+1 >= len(group.Leaves) || group.Leaves[i+1].ID != "health-config" {
				t.Fatalf("the leaf after Health Analytics is %v, want health-config directly below it", group.Leaves[i+1:])
			}
			return
		}
		t.Fatal("Health Analytics is not a leaf of the Others group")
	}
	t.Fatal("no Others navigation group")
}

// The leaf's gate and the page catalog's gate must be the SAME permission, and
// that permission must be the one the screen's own data route requires. A
// mismatch is the dead-leaf class: the row renders, the reader clicks, the data
// call 403s.
func TestHealthAnalyticsGateMatchesItsOwnDataRoute(t *testing.T) {
	nav := permissionsForNav("health-analytics")
	if len(nav) != 1 || nav[0] != permissions.HealthRead {
		t.Fatalf("nav gate = %v, want exactly [%s]", nav, permissions.HealthRead)
	}

	var routePermissions []string
	for _, route := range permissions.ProtectedRoutes() {
		if route.Method == "GET" && route.Pattern == "/health/analytics" {
			routePermissions = route.Permissions
		}
	}
	if len(routePermissions) != 1 || routePermissions[0] != permissions.HealthRead {
		t.Fatalf("route gate = %v, want exactly [%s]", routePermissions, permissions.HealthRead)
	}
}
