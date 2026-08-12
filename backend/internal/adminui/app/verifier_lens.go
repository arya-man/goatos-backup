package app

import (
	"context"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/verification"
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
	// verifierLensRoute is the only admin-web route the lens exposes. /verify already reads the
	// real /verification/queue contract, filters by the registry's disjoint categories, and plays
	// proof media -- so the lens re-scopes an existing screen instead of forking a second one.
	verifierLensRoute = "/verify"
	// verifierLensPageID is the page contract /verify renders from. It is the ONLY page contract
	// a lens principal receives; requireAdminWebPageContract throws on every other route_id, which
	// is what makes a hand-typed URL fail closed instead of rendering a shell she cannot use.
	verifierLensPageID = "verification-review"
)

// verifierLensIconForModule returns the icon token for an evidence module's sidebar group,
// mapping each vertical to the icon it uses in the console (maintainer decision 2026-08-06).
// All tokens must exist in the admin-web icon map (apps/admin-web/components/mesha-shell.tsx).
func verifierLensIconForModule(moduleKey string) string {
	// Keys are the registry's navigation-module keys, which are NOT always the bare vertical name
	// ("feed_direction", "aas_health"). Both spellings are mapped so a registry rename cannot
	// silently drop an icon back to the default.
	icons := map[string]string{
		"counts":         "bar-chart-3",
		"feed":           "tower-control",
		"feed_direction": "tower-control",
		"health":         "heart-pulse",
		"aas_health":     "heart-pulse",
		"vaccination":    "heart-pulse",
		"weighing":       "bar-chart-3",
	}
	if icon, ok := icons[moduleKey]; ok {
		return icon
	}
	// Fallback for any future module the icon map does not name yet.
	return "clipboard-check"
}

// VerificationNavPage is one backend-defined page tab inside a verifier evidence module -- the web
// twin of the mobile top-tab row. Category is the disjoint queue predicate /verify filters on.
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

// ModuleDutyReader is the port adminui uses to filter modules by the verifier's assigned duties.
// The workforce/roster repository satisfies it; the interface keeps the dependency loose for testing.
type ModuleDutyReader interface {
	ListVerifyModuleKeys(ctx context.Context, tenantID, userID string) ([]string, error)
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

// WithModuleDutyReader injects the duty-module reader as an optional filter. When wired, the lens
// will show only the modules the verifier actually holds verify duties for. When nil or erroring,
// the lens fails SAFE by showing NO modules (not all modules) -- this prevents the 403 bug where
// a nav item points to an unauthorized module.
func (s *Service) WithModuleDutyReader(reader ModuleDutyReader) *Service {
	s.moduleDutyReader = reader
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

// verifierNavModules returns the lens's modules for the given principal, filtered to only those
// they hold verify duties for. If no source is wired, no duty reader is wired, or the duty reader
// errors, the lens fails SAFE by returning nil/empty modules rather than showing everything.
func (s *Service) verifierNavModules(ctx context.Context, input BootstrapInput) []VerificationNavModule {
	if s.verificationModules == nil {
		return nil
	}
	allModules := s.verificationModules.VerifierNavModules()

	// If no duty reader is wired, fail SAFE: show no modules rather than all modules.
	if s.moduleDutyReader == nil {
		return nil
	}

	// Load the verifier's assigned duty modules.
	dutyModuleKeys, err := s.moduleDutyReader.ListVerifyModuleKeys(ctx, input.TenantID, input.ActorID)
	if err != nil {
		// Fail SAFE: if duty resolution errors, show no modules rather than all modules.
		return nil
	}

	// Build a set of navigation keys the verifier holds duties for.
	authorizedNavKeys := make(map[string]bool, len(dutyModuleKeys))
	for _, dutyCode := range dutyModuleKeys {
		navKey := verification.NavigationModuleForDutyCode(dutyCode)
		authorizedNavKeys[navKey] = true
	}

	// Filter: keep only modules whose navigation key is in the authorized set.
	filtered := make([]VerificationNavModule, 0, len(allModules))
	for _, module := range allModules {
		if authorizedNavKeys[module.Key] {
			filtered = append(filtered, module)
		}
	}

	return sortedVerifierModules(filtered)
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

// verifierLensNavigation composes the verifier sidebar: one group per evidence module, whose leaves
// are that module's page tabs.
//
// There is deliberately NO cross-category "All evidence" landing (maintainer decision 2026-08-06,
// matching mock/verifier-web-mock.SPEC.md): the verifier always picks the feature she is reviewing,
// so an aggregate row is one more thing to explain and the mock's sidebar does not have it. Do not
// reintroduce it. `category` staying optional on the queue read is still correct and load-bearing
// for API callers; it just no longer has a nav entry.
//
// Every leaf points at the SAME route and differs only by its category query parameter, which the
// shell appends from Extra. That is why the queue page needs no new route per module.
func verifierLensNavigation(modules []VerificationNavModule, footer string) domain.NavigationContract {
	nav := domain.NavigationContract{
		Primary: []domain.NavigationItem{},
		Groups:  make([]domain.NavigationGroup, 0, len(modules)),
		Footer:  footer,
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
		// Per-vertical icon: map each evidence module to the icon its vertical uses in the console
		nav.Groups = append(nav.Groups, domain.NavigationGroup{
			ID:          "verification-" + module.Key,
			Label:       module.Label,
			Icon:        verifierLensIconForModule(module.Key),
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
// option groups every other principal's /verify gets -- the lens narrows what she can reach, it
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
