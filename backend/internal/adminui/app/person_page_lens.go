package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// Per-person page narrowing (maintainer decision 2026-08-27), the replacement for the
// retired procurement-director lens.
//
// WHAT CHANGED, AND WHY IT IS THE SAME MECHANISM. The verifier lens one file over composes
// a DIFFERENT workspace: a queue, its own module leaves, its own landing. That is a product
// decision and it stays. The procurement-director lens did something narrower -- it deleted
// nav leaves and page contracts a person was not meant to reach ("only Procurement and
// Feed", "hide Feed Config"). Every such sentence used to require a developer. It is now
// data: the person's own ticked pages, edited on /people.
//
// THE PHONE IS NOT INVOLVED. This runs inside the admin-web bootstrap only. The Android
// module bar is composed from the mobile registry and is deliberately untouched.
//
// FAIL-OPEN ON ABSENCE, NEVER ON ERROR-TO-NARROWER. A person with no stored rows is not
// narrowed at all -- they are still on the retired role path, and narrowing them to nothing
// would lock out anyone the backfill has not reached. A source ERROR is likewise not a
// narrowing: it is logged and the full role-composed contract is served, because the route
// layer behind every one of those pages is independently permission-gated. The sidebar is a
// convenience; the 403 is the lockout.

// PersonPageAccessSource resolves a principal's per-person page access. Optional: when nil,
// or reporting no rows, the compiled contract is served unnarrowed.
type PersonPageAccessSource interface {
	ResolvePageAccess(ctx context.Context, tenantID, userID string) (permissions.PageAccess, bool, error)
}

// WithPersonPageAccess injects the per-person page resolver. A setter for the same reason
// the verifier lens uses one: the admin-web service is constructed before the workforce
// repository exists in backend/internal/bootstrap/api.go.
func (s *Service) WithPersonPageAccess(source PersonPageAccessSource) *Service {
	s.personPageAccess = source
	return s
}

// personPageAccessFor reads the principal's page access, reporting false when there is nothing to
// narrow by, and returning the error so the caller can DECLARE the degradation in the contract.
//
// This package deliberately holds no logger -- the verifier lens beside it has none either -- and
// it already has a way to say "a dependency failed and what I served is degraded": a DisplayRule,
// the same shape compile() uses for a failed reference-family load. That is where this error goes.
// It is swallowed nowhere, and it is visible in the thing that was actually served.
func (s *Service) personPageAccessFor(ctx context.Context, input BootstrapInput) (permissions.PageAccess, bool, error) {
	if s.personPageAccess == nil || input.ActorID == "" || input.TenantID == "" {
		return permissions.PageAccess{}, false, nil
	}
	access, assigned, err := s.personPageAccess.ResolvePageAccess(ctx, input.TenantID, input.ActorID)
	if err != nil {
		// Not fatal, and deliberately NOT a narrowing -- see the header.
		return permissions.PageAccess{}, false, err
	}
	if !assigned {
		return permissions.PageAccess{}, false, nil
	}
	return access, true, nil
}

// personPageAccessUnavailableRule declares that this contract was compiled WITHOUT the person's own
// screen assignments because they could not be read. Naming the degradation matters: without it a
// full sidebar is indistinguishable from a deliberate grant of everything.
func personPageAccessUnavailableRule(err error) domain.DisplayRule {
	return domain.DisplayRule{
		ID:        "admin_ui_person_page_access_unavailable",
		AppliesTo: []string{"admin-web"},
		Summary: "This person's own screen assignments could not be read, so the full navigation was compiled. " +
			"Every page behind it stays permission-gated at its own route. Cause: " + err.Error(),
		FrontendOwns: []string{"layout", "responsive density"},
		BackendOwns:  []string{"navigation composition", "page contracts", "retry through SSR bootstrap"},
	}
}

// applyPersonPageLens drops what this person's ticks withhold.
//
// It runs AFTER normal compilation (and after compileNavigation's RBAC pass), so a kept
// page carries exactly the controls, copy and option groups every other principal gets:
// this narrows what is REACHABLE, it does not fork the contract.
//
// A withheld leaf is REMOVED rather than disabled. A greyed row that can never be enabled
// is an advertisement for work the person is not part of, and the ticks now say precisely
// who is; disabling is still what compileNavItems does for someone the backfill has not
// reached, whose access is still role-derived.
// enableTickedLeaf clears the role-path RBAC disable from a leaf the person's own ticks kept.
//
// compileNavItems greys a leaf by asking grantsAuthorize -- the ROLE path. That is the layer
// this rewrite replaced, and for a page-ticked principal the two can disagree: a persona sweep
// found a manager whose per-person access grants protocol.read (a recorded benign gain of the
// backfill) while the role behind it does not, so Vaccination plan was ticked, shown, and
// rendered dead.
//
// Re-enabling is safe rather than a widening, and it is worth being precise about why: a page
// only survives the tick when PageAccessForAssignments has already checked the SCREEN's own
// permissions against what this person's capabilities produce (pageIsOpenable). The tick IS
// that authorization decision, taken from the same catalog. The route behind the leaf is
// independently gated on the same permissions, so a leaf that somehow slipped through would
// meet a 403 rather than data.
func enableTickedLeaf(item domain.NavigationItem) domain.NavigationItem {
	item.Enabled = true
	item.DisabledReason = ""
	return item
}

func applyPersonPageLens(resp domain.BootstrapResponse, access permissions.PageAccess) domain.BootstrapResponse {
	primary := make([]domain.NavigationItem, 0, len(resp.Navigation.Primary))
	for _, item := range resp.Navigation.Primary {
		if access.Allows(item.ID, item.Href) {
			primary = append(primary, enableTickedLeaf(item))
		}
	}
	resp.Navigation.Primary = primary

	groups := make([]domain.NavigationGroup, 0, len(resp.Navigation.Groups))
	for _, group := range resp.Navigation.Groups {
		leaves := make([]domain.NavigationItem, 0, len(group.Leaves))
		for _, leaf := range group.Leaves {
			if access.Allows(leaf.ID, leaf.Href) {
				leaves = append(leaves, enableTickedLeaf(leaf))
			}
		}
		if len(leaves) == 0 {
			// An empty module group is a heading over nothing.
			continue
		}
		group.Leaves = leaves
		groups = append(groups, group)
	}
	// A short sidebar collapsed by default is a click that hides the whole workspace, so a
	// person left with two or three groups gets them open.
	if len(groups) <= 3 {
		for i := range groups {
			groups[i].DefaultOpen = true
		}
	}
	resp.Navigation.Groups = groups

	pages := make([]domain.PageContract, 0, len(resp.Pages))
	for _, page := range resp.Pages {
		// Page contracts are matched on ROUTE, not on the nav id: a drilldown has no leaf,
		// and requireAdminWebPageContract is what makes a typed URL fail closed.
		if access.Allows("", page.Href) {
			pages = append(pages, page)
		}
	}
	resp.Pages = pages

	labels := make([]domain.RouteLabelRule, 0, len(resp.RouteLabels))
	for _, rule := range resp.RouteLabels {
		if access.Allows("", rule.Pattern) {
			labels = append(labels, rule)
		}
	}
	resp.RouteLabels = labels
	return resp
}
