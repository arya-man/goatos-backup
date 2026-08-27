package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// The procurement-director workspace (maintainer decision 2026-08-21): a principal holding
// procurement_director sees ONLY the Procurement and Feed modules on admin-web. Mutation checks
// performed when written: removing the compile() wiring, dropping either group ID from
// procurementDirectorLensGroupIDs, or skipping the page filter each turns at least one of these
// red.

func bootstrapWithGrants(s *Service, roles ...string) domain.BootstrapResponse {
	grants := make([]permissions.ActiveGrant, 0, len(roles))
	for _, role := range roles {
		grants = append(grants, permissions.ActiveGrant{Role: role, ScopeType: "tenant", ScopeID: lensTenantID})
	}
	return s.Bootstrap(context.Background(), BootstrapInput{TenantID: lensTenantID, Grants: grants})
}

// lensKeptPath mirrors the workspace's kept namespaces. "/sales" is matched EXACTLY or with a
// trailing slash, never as a bare prefix, so a future "/sales-something" route cannot slip in on a
// string match the way a HasPrefix check would let it.
func lensKeptPath(path string) bool {
	return strings.HasPrefix(path, "/procurement") ||
		strings.HasPrefix(path, "/feed") ||
		path == "/sales" || strings.HasPrefix(path, "/sales/")
}

func assertProcurementFeedWorkspace(t *testing.T, resp domain.BootstrapResponse) {
	t.Helper()
	if len(resp.Navigation.Primary) != 0 {
		t.Errorf("lens must drop every primary nav item (command lenses); got %d", len(resp.Navigation.Primary))
	}
	gotGroups := make([]string, 0, len(resp.Navigation.Groups))
	for _, group := range resp.Navigation.Groups {
		gotGroups = append(gotGroups, group.ID)
		if !group.DefaultOpen {
			t.Errorf("group %q must be DefaultOpen in a two-group sidebar", group.ID)
		}
	}
	// Sales joined this list on 2026-08-27 when it split out of the Procurement group. It is the
	// same content this workspace always carried -- the leaf moved out of Procurement into its own
	// vertical -- and this principal is the one non-founder holder of sales.read / sales.write.
	if len(gotGroups) != 3 || gotGroups[0] != "procurement" || gotGroups[1] != "sales" || gotGroups[2] != "feed" {
		t.Fatalf("lens must keep exactly the procurement, sales and feed groups in canonical order; got %v", gotGroups)
	}
	// Feed Config is withheld from this workspace (maintainer decision 2026-08-21): no sidebar
	// leaf, and no page contract, so a typed /feed/config URL fails closed.
	for _, group := range resp.Navigation.Groups {
		for _, leaf := range group.Leaves {
			if leaf.ID == "feed-config" || strings.HasPrefix(leaf.Href, "/feed/config") {
				t.Errorf("feed-config leaf leaked into the workspace sidebar")
			}
		}
	}
	if len(resp.Pages) == 0 {
		t.Fatal("lens dropped every page contract; the workspace would render nothing")
	}
	wantPages := map[string]bool{"vendors": false, "sales": false, "source-entry": false, "feed-analytics": false}
	for _, page := range resp.Pages {
		path := page.Href
		if i := strings.IndexAny(path, "?#"); i >= 0 {
			path = path[:i]
		}
		if !lensKeptPath(path) {
			t.Errorf("page %q (%s) escaped the lens; a typed URL to it would render", page.RouteID, page.Href)
		}
		if page.RouteID == "feed-config" || path == "/feed/config" {
			t.Errorf("feed-config page contract leaked into the workspace; /feed/config would render")
		}
		if _, tracked := wantPages[page.RouteID]; tracked {
			wantPages[page.RouteID] = true
		}
	}
	for routeID, found := range wantPages {
		if !found {
			t.Errorf("page contract %q missing; its screen would fail closed for the one principal it exists for", routeID)
		}
	}
	for _, rule := range resp.RouteLabels {
		if !lensKeptPath(rule.Pattern) {
			t.Errorf("route label %q escaped the lens", rule.Pattern)
		}
		if strings.HasPrefix(rule.Pattern, "/feed/config") {
			t.Errorf("feed-config route label leaked into the workspace")
		}
	}
}

func TestProcurementDirectorLensKeepsOnlyProcurementAndFeed(t *testing.T) {
	resp := bootstrapWithGrants(lensService(), permissions.RoleProcurementDirector)
	assertProcurementFeedWorkspace(t, resp)

	// The role's own permissions must actually light the kept leaves: a sidebar of disabled
	// entries would be a workspace this principal can see and not use.
	for _, group := range resp.Navigation.Groups {
		for _, leaf := range group.Leaves {
			if !leaf.Enabled {
				t.Errorf("leaf %q (%s) is disabled inside the role's own workspace: %s", leaf.ID, leaf.Href, leaf.DisabledReason)
			}
		}
	}
}

// The current holder carries feed_director ALONGSIDE procurement_director (his phone access and
// feed-proof ownership ride on feed_director). The union must still be narrowed — that is the
// entire point of the grant.
func TestProcurementDirectorLensAppliesOverAFeedDirectorUnion(t *testing.T) {
	resp := bootstrapWithGrants(lensService(), permissions.RoleFeedDirector, permissions.RoleProcurementDirector)
	assertProcurementFeedWorkspace(t, resp)
}

// The lens must never narrow a leadership principal (same rule as the verifier lens): ceo_internal
// keeps the full admin IA even if it ever holds this role.
func TestProcurementDirectorLensNeverNarrowsLeadership(t *testing.T) {
	resp := bootstrapWithGrants(lensService(), permissions.RoleCEOInternal, permissions.RoleProcurementDirector)
	if len(resp.Navigation.Primary) == 0 {
		t.Fatal("ceo_internal lost the primary nav; the lens narrowed a leadership principal")
	}
	foundControlTower := false
	for _, page := range resp.Pages {
		if page.RouteID == "control-tower" {
			foundControlTower = true
		}
	}
	if !foundControlTower {
		t.Fatal("ceo_internal lost the control-tower page contract; the lens narrowed a leadership principal")
	}
}

// A bare feed_director (no procurement_director grant) keeps today's full IA: the lens is carried
// by the new role, not by holding feed permissions.
func TestFeedDirectorAloneIsNotNarrowed(t *testing.T) {
	resp := bootstrapWithGrants(lensService(), permissions.RoleFeedDirector)
	if len(resp.Navigation.Primary) == 0 {
		t.Fatal("feed_director lost the primary nav without holding procurement_director")
	}
	foundBeyondLens := false
	for _, page := range resp.Pages {
		if page.RouteID == "control-tower" {
			foundBeyondLens = true
		}
	}
	if !foundBeyondLens {
		t.Fatal("feed_director lost page contracts outside /procurement and /feed without holding procurement_director")
	}
}
