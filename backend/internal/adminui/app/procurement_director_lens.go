package app

import (
	"strings"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// The procurement-director admin-web workspace (maintainer decision 2026-08-21).
//
// "When he logs in he should see only the Procurement and Feed modules in web" — the same shape as
// the verifier lens one file over: a LENS over the fully-compiled contract, not a parallel product.
// The kept groups/pages are exactly the canonical navigation()'s Procurement and Feed content, so a
// leaf added to either group in service.go appears here with no change to this file — this filters
// the single nav source of truth by module, it does not hardcode a second nav template
// (docs/decisions/role-module-nav-composition.md).
//
// Every other page contract is DROPPED, which is the route-level half of the lockout:
// requireAdminWebPageContract throws on a route with no contract, so a hand-typed /vaccination or
// /counts URL fails closed instead of rendering a shell this principal must not see. The data
// endpoints behind the dropped pages stay independently permission-gated regardless — the role
// simply does not hold them (permissions.RoleProcurementDirector).
//
// The landing redirect needs no code here: app/(admin)/page.tsx already redirects a principal with
// no control-tower contract to their first enabled nav leaf with a published page — Procurement
// Source Entry for this workspace.

// procurementDirectorLensGroupIDs are the canonical navigation() group IDs this workspace keeps.
// These are the two module groups the maintainer named; both IDs are pinned by
// TestProcurementDirectorLensKeepsOnlyProcurementAndFeed.
var procurementDirectorLensGroupIDs = map[string]struct{}{
	"procurement": {},
	"feed":        {},
}

// procurementDirectorLensRoutePrefixes are the route namespaces of the kept modules. Pages and
// route labels are filtered by path prefix rather than an enumerated route_id list so a new
// /procurement/* or /feed/* page ships into this workspace automatically.
var procurementDirectorLensRoutePrefixes = []string{"/procurement", "/feed"}

// procurementDirectorLensHiddenRoutes are routes inside the kept modules that this workspace
// still withholds (maintainer decision 2026-08-21, second half: "hide the Feed Config page for
// him under Feed"). Feed Config is the AUTHORED ration grid — feed_director's own instrument —
// and this workspace is read-only feed oversight, so the leaf is dropped from the sidebar AND the
// page contract is dropped so a typed /feed/config URL fails closed. The role also does not hold
// feed_config.read (permissions.go), so this list and the permission set state the same decision.
var procurementDirectorLensHiddenRoutes = map[string]struct{}{
	"/feed/config": {},
}

// isProcurementDirectorLensPrincipal reports whether this principal gets the narrowed
// Procurement + Feed workspace.
//
// Keyed on the procurement_director role at TENANT grain, the same layer compileRoleLenses and
// highestRole already read. The role may be held ALONGSIDE feed_director (the current holder is
// both — his phone access and feed-proof ownership ride on feed_director), and the lens still
// applies: the whole point of the grant is the narrowed web workspace, and the feed half of his
// job is inside it. The lens must never narrow a leadership principal (same rule as the verifier
// lens), so ceo_internal keeps the full admin IA even if it ever holds this role.
func isProcurementDirectorLensPrincipal(input BootstrapInput) bool {
	roles := tenantRoles(input.Grants, input.TenantID)
	if hasAnyRole(roles, permissions.RoleCEOInternal) {
		return false
	}
	return hasAnyRole(roles, permissions.RoleProcurementDirector)
}

// procurementDirectorLensHrefKept reports whether a contract href lives inside the kept module
// namespaces. Compared on the PATH only, so a query-carrying href cannot dodge the filter.
func procurementDirectorLensHrefKept(href string) bool {
	path := strings.TrimSpace(href)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if _, hidden := procurementDirectorLensHiddenRoutes[path]; hidden {
		return false
	}
	for _, prefix := range procurementDirectorLensRoutePrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// applyProcurementDirectorLens re-scopes a compiled bootstrap response down to the Procurement +
// Feed workspace.
//
// It runs AFTER normal compilation (and after compileNavigation's RBAC pass), so the kept pages
// carry the same controls, copy, option groups and enabled/disabled leaf states every other
// principal gets — the lens narrows what this principal can reach, it does not fork the contract
// they render.
func applyProcurementDirectorLens(resp domain.BootstrapResponse) domain.BootstrapResponse {
	// No primary items: every one of them is a cross-module command lens (Control Tower, Action
	// Center, Calendar, ...) or a decision surface this role holds no permission for.
	resp.Navigation.Primary = []domain.NavigationItem{}
	groups := make([]domain.NavigationGroup, 0, len(procurementDirectorLensGroupIDs))
	for _, group := range resp.Navigation.Groups {
		if _, kept := procurementDirectorLensGroupIDs[group.ID]; kept {
			// Open by default: with two groups in the whole sidebar, a collapsed module is a
			// click that hides the workspace.
			group.DefaultOpen = true
			leaves := make([]domain.NavigationItem, 0, len(group.Leaves))
			for _, leaf := range group.Leaves {
				if procurementDirectorLensHrefKept(leaf.Href) {
					leaves = append(leaves, leaf)
				}
			}
			group.Leaves = leaves
			groups = append(groups, group)
		}
	}
	resp.Navigation.Groups = groups

	pages := make([]domain.PageContract, 0, len(resp.Pages))
	for _, page := range resp.Pages {
		if procurementDirectorLensHrefKept(page.Href) {
			pages = append(pages, page)
		}
	}
	resp.Pages = pages

	labels := make([]domain.RouteLabelRule, 0, len(resp.RouteLabels))
	for _, rule := range resp.RouteLabels {
		if procurementDirectorLensHrefKept(rule.Pattern) {
			labels = append(labels, rule)
		}
	}
	resp.RouteLabels = labels
	return resp
}
