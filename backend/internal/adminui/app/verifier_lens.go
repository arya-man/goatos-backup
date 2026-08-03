package app

import (
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// The verifier-only admin-web workspace (maintainer decision 2026-08-03).
//
// context/architecture/verifier-app-and-flow.md defines the Verifier as a standing, independent
// second check on every execution SOP across every vertical. That workspace shipped on Android
// first; this file is the same workspace composed for admin-web, so the verifier reviews the same
// evidence on a laptop screen big enough to actually watch the video.
//
// It is a LENS, not a second product. A principal who holds verification.review and NOT
// verification.act gets ONLY this lens: the drawer modules come from the generic Verification type
// registry, and every other admin-web page contract is dropped from her bootstrap. Nothing here
// enumerates a role or names a vertical -- add a producer to the registry and its evidence appears
// in both the mobile drawer and this sidebar with no change to this file. That is the requirement
// docs/decisions/role-module-nav-composition.md sets out ("a fixed nav template array hardcoded per
// role or per module is banned"), and the reason this composes from registry data rather than a
// literal list of the five modules that happen to be registered today.
const (
	// verifierLensRoute is the only admin-web route the lens exposes. /actions already reads the
	// real /verification/queue contract, filters by the registry's disjoint categories, and plays
	// proof media -- so the lens re-scopes an existing screen instead of forking a second one.
	verifierLensRoute = "/actions"
	// verifierLensPageID is the page contract /actions renders from. It is the ONLY page contract
	// a lens principal receives; requireAdminWebPageContract throws on every other route_id, which
	// is what makes a hand-typed URL fail closed instead of rendering a shell she cannot use.
	verifierLensPageID = "verification-review"
	// verifierLensIcon is shared by every evidence module on purpose. These groups are not the
	// operational verticals they are named after -- they are all the same thing to a verifier
	// (a queue of video to review), so giving each one a distinct domain icon would imply she has
	// the operational surface behind it. A per-module icon map would also be exactly the per-module
	// nav template the composition rule bans.
	verifierLensIcon = "clipboard-check"
)

// VerificationNavPage is one backend-defined page tab inside a verifier evidence module -- the web
// twin of the mobile top-tab row. Category is the disjoint queue predicate /actions filters on.
type VerificationNavPage struct {
	Key      string
	Label    string
	Category string
	Order    int
}

// VerificationNavModule is one verifier evidence module (the mobile drawer identity), carrying the
// pages registered against it.
type VerificationNavModule struct {
	Key   string
	Label string
	Pages []VerificationNavPage
}

// VerificationModuleSource is the port adminui reads verifier evidence modules from. The
// Verification module satisfies it; adminui deliberately does not import verification/domain, so
// the two bounded contexts stay decoupled and this package can be tested with a fake.
type VerificationModuleSource interface {
	VerifierNavModules() []VerificationNavModule
}

// WithVerificationModules injects the Verification type registry as the verifier lens's module
// source. Wiring is a setter rather than a constructor argument because the admin-web service is
// built before the Verification service exists in backend/internal/bootstrap/api.go.
//
// Without a source the lens cannot be composed. That case FAILS CLOSED (see verifierLensBootstrap):
// an unwired binary shows a lens principal an empty workspace rather than silently falling through
// to the full admin IA she must never see.
func (s *Service) WithVerificationModules(source VerificationModuleSource) *Service {
	s.verificationModules = source
	return s
}

// isVerifierLensPrincipal reports whether this principal's whole job is recording verdicts.
//
// The test is verification.verdict AND NOT verification.act -- the person who DECIDES and does not
// act. It keys on the verdict permission rather than the read because reading the evidence queue is
// leadership visibility (CEO holds verification.review and must keep the full admin IA), while the
// verdict is the Verifier's alone. A future read-only evidence viewer therefore does not get
// silently narrowed into a workspace whose one action it cannot perform.
func isVerifierLensPrincipal(input BootstrapInput) bool {
	if len(input.Grants) == 0 {
		return false
	}
	if !grantsAuthorize(input.Grants, input.TenantID, []string{permissions.VerificationVerdict}) {
		return false
	}
	return !grantsAuthorize(input.Grants, input.TenantID, []string{permissions.VerificationAct})
}

// verifierNavModules returns the lens's modules, or nil when no source is wired.
func (s *Service) verifierNavModules() []VerificationNavModule {
	if s.verificationModules == nil {
		return nil
	}
	return sortedVerifierModules(s.verificationModules.VerifierNavModules())
}

// sortedVerifierModules orders modules by label and their pages by the registry's declared
// PageOrder.
//
// Page order is backend-authored data (CategoryDefinition.PageOrder), so it is honoured verbatim.
// Module order is NOT declared anywhere in the registry, so this sorts by label rather than
// inventing a priority -- alphabetical is deterministic and admits it is arbitrary, where a
// hand-written module ranking here would be an undocumented product decision. If the maintainer
// wants the sidebar to match the mobile drawer's exact order, the fix is a module-order field on
// CategoryDefinition, not a ranking table in this file.
func sortedVerifierModules(modules []VerificationNavModule) []VerificationNavModule {
	out := make([]VerificationNavModule, 0, len(modules))
	for _, module := range modules {
		module.Key = strings.TrimSpace(module.Key)
		module.Label = strings.TrimSpace(module.Label)
		if module.Key == "" || module.Label == "" {
			continue
		}
		pages := make([]VerificationNavPage, 0, len(module.Pages))
		for _, page := range module.Pages {
			page.Key = strings.TrimSpace(page.Key)
			page.Label = strings.TrimSpace(page.Label)
			page.Category = strings.TrimSpace(page.Category)
			if page.Key == "" || page.Label == "" || page.Category == "" {
				continue
			}
			pages = append(pages, page)
		}
		if len(pages) == 0 {
			// A module with no renderable page is dropped rather than shown empty: an evidence
			// group that opens onto nothing advertises a queue that cannot exist.
			continue
		}
		sort.SliceStable(pages, func(i, j int) bool {
			if pages[i].Order != pages[j].Order {
				return pages[i].Order < pages[j].Order
			}
			return pages[i].Label < pages[j].Label
		})
		module.Pages = pages
		out = append(out, module)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// verifierLensNavigation composes the verifier sidebar: one "All evidence" landing plus one group
// per evidence module, whose leaves are that module's page tabs.
//
// Every leaf points at the SAME route and differs only by its category query parameter, which the
// shell appends from Extra. That is why the queue page needs no new route per module.
func verifierLensNavigation(modules []VerificationNavModule, footer string) domain.NavigationContract {
	nav := domain.NavigationContract{
		Primary: []domain.NavigationItem{
			navItemDomain("verification-actions", "All evidence", verifierLensRoute, verifierLensIcon, "", "admin.verification"),
		},
		Groups: make([]domain.NavigationGroup, 0, len(modules)),
		Footer: footer,
	}
	for _, module := range modules {
		leaves := make([]domain.NavigationItem, 0, len(module.Pages))
		for _, page := range module.Pages {
			leaves = append(leaves, navLeafDomain(
				"verification-"+module.Key+"-"+page.Key,
				page.Label,
				verifierLensRoute,
				"admin.verification",
				map[string]string{"category": page.Category},
			))
		}
		nav.Groups = append(nav.Groups, domain.NavigationGroup{
			ID:          "verification-" + module.Key,
			Label:       module.Label,
			Icon:        verifierLensIcon,
			DefaultOpen: true,
			Leaves:      leaves,
		})
	}
	return nav
}

// verifierLensPages keeps only the queue page contract. Dropping the rest is the route-level half
// of the lockout: hiding a nav item is not access control, but a missing page contract makes the
// route itself throw before it renders (the data endpoints behind those pages stay independently
// permission-gated regardless).
func verifierLensPages(pages []domain.PageContract) []domain.PageContract {
	for _, page := range pages {
		if page.RouteID == verifierLensPageID {
			return []domain.PageContract{page}
		}
	}
	return nil
}

// verifierLensRouteLabels keeps only the label rules for routes the lens can reach, so the shell
// cannot title a page she has no contract for.
func verifierLensRouteLabels(rules []domain.RouteLabelRule) []domain.RouteLabelRule {
	out := make([]domain.RouteLabelRule, 0, 1)
	for _, rule := range rules {
		if rule.Pattern == verifierLensRoute {
			out = append(out, rule)
		}
	}
	return out
}

// applyVerifierLens re-scopes a compiled bootstrap response down to the verifier workspace.
//
// It runs AFTER normal compilation so the queue page still receives the same controls, copy, and
// option groups every other principal's /actions gets -- the lens narrows what she can reach, it
// does not fork the contract she renders.
func applyVerifierLens(resp domain.BootstrapResponse, modules []VerificationNavModule) domain.BootstrapResponse {
	resp.Navigation = verifierLensNavigation(modules, resp.Navigation.Footer)
	resp.Pages = verifierLensPages(resp.Pages)
	resp.RouteLabels = verifierLensRouteLabels(resp.RouteLabels)
	// Five evidence groups is a sidebar, not a bottom bar; but with no wired module source there is
	// nothing to show, and NavChromeMinimal keeps the shell from rendering an empty rail.
	if len(modules) == 0 {
		resp.NavChrome = domain.NavChromeMinimal
	}
	return resp
}
