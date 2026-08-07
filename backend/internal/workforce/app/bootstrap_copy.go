package app

import (
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Bootstrap copy is backend-owned presentation text for /app/bootstrap. Android renders
// these values from the payload; Android strings.xml owns only client-static text.

// moduleNavContribution declares the nav items that a module contributes.
// shared_key allows items to be deduped across modules (e.g., "calendar" is shared
// by Vaccination, Feed Direction, and future modules).
type moduleNavContribution struct {
	key        string // e.g., "vaccination", "overview", "calendar"
	labelKey   string // i18n key in bootstrapLabels
	href       string
	shared_key string // "" if not shared; if set, dedupe by this key across modules
	priority   int    // lower = earlier in nav; shared items use the first module's priority
	// requiredPermission gates this single nav item. "" means the item is ungated and
	// visible to anyone holding the module. This is what lets ONE module expose
	// different pages to different jobs without a per-role nav template (banned by
	// docs/decisions/role-module-nav-composition.md): the registry declares the
	// permission, the role->permission table decides who holds it.
	//
	// Hiding the item is NOT the access control -- the matching route in
	// permissions.routePermissions requires the same permission, so an unlisted page is
	// unreachable rather than merely invisible.
	requiredPermission string
	// requiredAnyPermission gates a nav item when any one of several authorities can
	// use the surface, e.g. Weighing is visible to planners, monitors, and executors
	// but each command remains route/API-authorized by its own permission.
	requiredAnyPermission []string
	// excludedPermission suppresses a field-lens item when the principal holds a
	// higher-level module lens. This keeps one Vaccination module reusable without
	// turning the nav builder into a per-role template: the role table grants the
	// lens permission, and the registry declares how that lens changes the bar.
	excludedPermission string
}

// moduleDefinition is a module's drawer identity plus the nav items it contributes
// to its own bottom bar. The drawer entry (label/landing/status) used to be hardcoded
// in the Android client (GoatOsShell.kt SOON_MODULES + a literal Vaccination row);
// it is backend-owned here so a new module is a registry entry, not a client change.
type moduleDefinition struct {
	key         string // stable module id, also the department_module_grants.module_key
	labelKey    string // i18n key in bootstrapLabels for the drawer row
	landingHref string // route opened when the drawer row is tapped
	// reviewLandingHref/reviewContributions are the evidence-review lens contributed by
	// this SAME operational module. A standalone verifier receives these pages instead of
	// the operator/leadership pages; the drawer identity remains Vaccination, Weighing,
	// Counts, Feed, or Health. This keeps verification reusable without a synthetic
	// client-side module template.
	reviewLandingHref   string
	reviewContributions []moduleNavContribution
	status              string // moduleStatusAvailable | moduleStatusSoon
	priority            int    // drawer ordering; lower first
	contributions       []moduleNavContribution
}

const (
	moduleStatusAvailable = "available"
	moduleStatusSoon      = "soon"
)

// moduleNavRegistry maps module IDs to their drawer identity and nav contributions.
// Each module declares which nav items it owns or contributes to shared screens.
// New modules should register here rather than hardcode nav templates.
// This IS the source of truth for navigation composition; routes here are intentional
// registry definitions, not hardcoded per-role templates.
//
// Bottom-bar shape is MODULE-SCOPED: a module's bar is its own contributions, so
// switching modules in the drawer switches the bar. Cross-module dedupe by shared_key
// still applies when several modules are composed into one flat bar.
// See docs/decisions/role-module-nav-composition.md.
var moduleNavRegistry = map[string]moduleDefinition{ //nav-composition:ignore: this is the module registry, not a hardcoded per-role template
	// "vaccination" is the Preventive Care (PC) Vaccination module.
	"vaccination": {
		key:               "vaccination",
		labelKey:          "module.vaccination",
		landingHref:       "/vaccination",        //nav-composition:ignore: registry entry
		reviewLandingHref: "/verify/vaccination", //nav-composition:ignore: registry entry
		status:            moduleStatusAvailable,
		priority:          1,
		contributions: []moduleNavContribution{
			{key: "overview", labelKey: "nav.overview", href: "/vaccination", shared_key: "", priority: 1, requiredPermission: permissions.VaccinationOverviewRead}, //nav-composition:ignore: registry entry
			{key: "vaccination", labelKey: "nav.drives", href: "/vaccination", shared_key: "", priority: 1, excludedPermission: permissions.CalendarAction},         //nav-composition:ignore: registry entry
			{key: "calendar", labelKey: "nav.calendar", href: "/calendar", shared_key: "calendar", priority: 2, requiredPermission: permissions.CalendarAction},     //nav-composition:ignore: registry entry
			// Leadership's Videos tab is a REVIEW/audit surface (context/architecture/
			// verifier-app-and-flow.md; verdict-exclusivity rule in AGENTS.md): it must show the
			// complete evidence trail -- pending, approved, rejected, AND already-closed proofs --
			// not just work still open for action. It used to point at "/verify/action"
			// (permission-gated the same way, since only VerificationAct-holding leadership ever
			// reaches this non-review-lens contribution), which hits GET /verification/action-queue
			// with OpenOnly=true and silently drops every item whose closed_at is set. A CEO who had
			// already closed half his approved vaccination proofs saw only the other half plus the
			// rejected one -- the closed half of his own audit trail vanished with no error. Routing
			// through verifyQueueHref instead lands on GET /verification/queue (OpenOnly=false,
			// status-filterable, "All" reachable), the same review endpoint the standalone verifier's
			// reviewContributions videos tab already uses correctly. Never revert this to
			// "/verify/action" or otherwise flip OpenOnly for the action queue itself -- the verifier's
			// action queue must stay open-items-only (see the reviewContributions entry below and
			// ListActionQueue in verification/adapters/http/handler.go).
			{key: "videos", labelKey: "nav.videos", href: "/vaccination/videos", shared_key: "", priority: 3, requiredPermission: permissions.VerificationAct}, //nav-composition:ignore: registry entry
			// Vaccination's OWN alerts feed. The href names the feature that owns it, the same
			// way weighing's does: alerts are feature-scoped by rule, and a generically-named
			// "/alerts" is what once got copied into weighing's bar, where it 403'd for a
			// weighing operator (docs/decisions/module-alerts-tab.md). The legacy "/alerts"
			// route stays hosted on the phone for alerts already delivered; it is no longer
			// what any bar points at.
			{key: "alerts", labelKey: "nav.alerts", href: "/vaccination/alerts", shared_key: "alerts", priority: 20}, //nav-composition:ignore: registry entry
			{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100},
		},
		reviewContributions: []moduleNavContribution{
			{key: "videos", labelKey: "nav.videos", href: "/verify/vaccination", priority: 1, requiredPermission: permissions.VerificationReview}, //nav-composition:ignore: registry entry
			{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100},                                                     //nav-composition:ignore: registry entry
		},
	},
	// "weighing" is its own Preventive Care vertical. It is listed beside
	// Vaccination in the module drawer; its bottom bar is only weighing-owned
	// destinations, not a Vaccination tab.
	"weighing": {
		key:               "weighing",
		labelKey:          "module.weighing",
		landingHref:       "/weighing",        //nav-composition:ignore: registry entry
		reviewLandingHref: "/verify/weighing", //nav-composition:ignore: registry entry
		status:            moduleStatusAvailable,
		priority:          2,
		contributions: []moduleNavContribution{
			// Three SEPARATE weighing destinations, each gated on its own capability so no screen
			// has to branch on who is looking:
			//
			//   /weighing           my own assigned sheds, the only list with a scan action.
			//   /weighing/tasks     the planner's flat all-tasks list across parks.
			//   /weighing/operators read-only oversight of other people's work.
			//
			// Tab order is priority, and PLAN WINS OVER EXECUTE: the CEO holds plan but not
			// execute, so "My work" is gated away for him, landingHref falls through to the first
			// permitted item, and he lands on the flat list instead of an empty my-work page.
			{key: "tasks", labelKey: "nav.tasks", href: "/weighing/tasks", shared_key: "", priority: 1, requiredPermission: permissions.WeighingPlan},                         //nav-composition:ignore: registry entry
			{key: "weighing", labelKey: "nav.my_work", href: "/weighing", shared_key: "", priority: 2, requiredPermission: permissions.WeighingExecute},                       //nav-composition:ignore: registry entry
			{key: "operators", labelKey: "nav.operators", href: "/weighing/operators", shared_key: "", priority: 3, requiredPermission: permissions.WeighingOverseeOperators}, //nav-composition:ignore: registry entry
			{key: "videos", labelKey: "nav.videos", href: "/weighing/videos", shared_key: "", priority: 4, requiredPermission: permissions.WeighingMonitor},                   //nav-composition:ignore: registry entry
			// Weighing's OWN alerts feed. This is NOT /alerts -- that is the vaccination
			// process-integrity feed, whose upstream needs ObligationRead+VaccinationRead and
			// whose label reads "Vaccination alerts" in all four languages. Carried in the
			// weighing bar it gave a weighing operator a permanently-empty cross-module tab
			// that 403s, so it was removed until weighing had a module-scoped feed of its own.
			// It now does: /app/weighing/alerts reads the weighing lifecycle notifications
			// already routed to the caller (assigned/submitted/reopened/rework/closed), gated
			// on weighing capabilities only.
			//
			// The LABEL is just "Alerts" (maintainer ruling 2026-08-03): the tab never names
			// the feature, the href carries the scoping. No shared_key -- this destination is
			// weighing's alone and must never dedupe against the vaccination "alerts" item.
			//
			// It also fixes the degenerate single-tab bar: an operator holding only
			// WeighingExecute previously got [My work] alone, a switcher with nothing to
			// switch to.
			// Weights and Growth are the PLANNER's read-outs, so they gate on WeighingPlan
			// (CEO-only, see RoleGrowthDirector's own "NOT WeighingPlan" note), not on
			// WeighingMonitor. Monitor is held by the Growth Director too, which put seven
			// tabs in that role's bottom bar -- My work, Operators, Videos, Weights, Growth,
			// Alerts, You -- for two destinations they do not own (maintainer ruling
			// 2026-08-05). The Growth Director keeps My work / Operators / Videos / Alerts.
			{key: "weights", labelKey: "nav.weights", href: "/weighing/weights", shared_key: "", priority: 5, requiredPermission: permissions.WeighingPlan}, //nav-composition:ignore: registry entry
			{key: "growth", labelKey: "nav.growth", href: "/weighing/growth", shared_key: "", priority: 6, requiredPermission: permissions.WeighingPlan},    //nav-composition:ignore: registry entry
			{key: "weighing_alerts", labelKey: "nav.alerts", href: "/weighing/alerts", shared_key: "", priority: 7},                                         //nav-composition:ignore: registry entry
			{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100},
		},
		reviewContributions: []moduleNavContribution{
			{key: "videos", labelKey: "nav.videos", href: "/verify/weighing", priority: 1, requiredPermission: permissions.VerificationReview}, //nav-composition:ignore: registry entry
			{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100},                                                  //nav-composition:ignore: registry entry
		},
	},
	// "counts" is the Counts vertical: the field events that move the herd register.
	// Birth and death are goat-lifecycle writes that feed the herd-register projection;
	// shifting records an animal movement between sheds/parks; milk prep/feeding are the
	// daily kid-milk tasks.
	//
	// The tenant-wide census READ page (/counts) was REMOVED from mobile (maintainer
	// decision 2026-07-30): the phone module is capture-only work lists, and population
	// visibility stays on admin-web. Its permission (counts.read) still gates admin-web.
	"counts": {
		key:               "counts",
		labelKey:          "module.counts",
		landingHref:       "/counts/birth",  //nav-composition:ignore: registry entry
		reviewLandingHref: "/verify/counts", //nav-composition:ignore: registry entry
		status:            moduleStatusAvailable,
		priority:          3,
		contributions: []moduleNavContribution{
			// Every page here follows CountsWrite: the phone module is field capture only.
			// Birth and Death split into two modules-with-work-lists (maintainer decision
			// 2026-07-27, docs/decisions/birth-death-workflows.md): each opens on the
			// outstanding per-goat SOP actions; recording moves behind the ＋ button.
			{key: "birth", labelKey: "nav.birth", href: "/counts/birth", shared_key: "", priority: 2, requiredPermission: permissions.CountsWrite},          //nav-composition:ignore: registry entry
			{key: "death", labelKey: "nav.death", href: "/counts/death", shared_key: "", priority: 3, requiredPermission: permissions.CountsWrite},          //nav-composition:ignore: registry entry
			{key: "shifting", labelKey: "nav.shifting", href: "/counts/shifting", shared_key: "", priority: 4, requiredPermission: permissions.CountsWrite}, //nav-composition:ignore: registry entry
			// Milk prep/feeding MOVED OUT to the "milk" module (maintainer decision 2026-07-31).
			// Counts contributes NO approval tab, and must not regain one. Approvals returned to the
			// phone on 2026-08-05 (superseding their 2026-07-21 removal) as their OWN module -- see
			// the "approvals" registry entry below. Approving is not capturing: the approvers hold
			// no counts.write and operators hold no approval authority, so the two ride separate
			// modules with separate gates. This bar stays capture-only (birth, death, shifting).
		},
		reviewContributions: []moduleNavContribution{
			{key: "videos", labelKey: "nav.videos", href: "/verify/counts", priority: 1, requiredPermission: permissions.VerificationReview}, //nav-composition:ignore: registry entry
			{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100},                                                //nav-composition:ignore: registry entry
		},
	},
	// "feed_direction" is the Feed vertical on the phone. Its bar is the three surfaces an operator
	// dispatches from: the generated feed sheet (Feed Direction), the per-shed bag worklist (Feed
	// Packing), and the daily per-shed transport tasks (Feed Transport).
	//
	// Each tab gates on a DIFFERENT authority on purpose (see permissions.go): FeedDirectionRead,
	// FeedPackingRead, FeedTransportRead. All three are READS, and each one is the same permission
	// its backing route requires -- pinned by TestFeedNavGatesEqualTheirBackingRoutePermissions,
	// because both ways of drifting apart have already shipped here. Direction gated on ProtocolRead
	// (the VACCINATION protocol read) while its route had moved to FeedDirectionRead, so the Feed
	// Director was authorized on the route and hidden from the tab, while the dormant org-role
	// director_feed saw a tab that 403'd on arrival. Transport gated on FeedDirectionComplete, a
	// WRITE, so only someone entitled to record transport could look at the list.
	//
	// Recording transport still needs FeedDirectionComplete on the submit route: these tabs let the
	// Feed Director SEE every page of the chain they own without letting them execute it.
	"feed_direction": {
		key:               "feed_direction",
		labelKey:          "module.feed_direction",
		landingHref:       "/feed/direction", //nav-composition:ignore: registry entry
		reviewLandingHref: "/verify/feed",    //nav-composition:ignore: registry entry
		status:            moduleStatusAvailable,
		priority:          4,
		contributions: []moduleNavContribution{
			{key: "feed_direction", labelKey: "nav.feed_direction", href: "/feed/direction", shared_key: "", priority: 1, requiredPermission: permissions.FeedDirectionRead}, //nav-composition:ignore: registry entry
			{key: "feed_packing", labelKey: "nav.feed_packing", href: "/feed/packing", shared_key: "", priority: 2, requiredPermission: permissions.FeedPackingRead},         //nav-composition:ignore: registry entry
			{key: "feed_transport", labelKey: "nav.feed_transport", href: "/feed/transport", shared_key: "", priority: 3, requiredPermission: permissions.FeedTransportRead}, //nav-composition:ignore: registry entry
		},
		reviewContributions: []moduleNavContribution{
			{key: "videos", labelKey: "nav.videos", href: "/verify/feed", priority: 1, requiredPermission: permissions.VerificationReview}, //nav-composition:ignore: registry entry
			{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100},                                              //nav-composition:ignore: registry entry
		},
	},
	"aas_health": {
		key:               "aas_health",
		labelKey:          "module.health",
		landingHref:       "/health/adults", //nav-composition:ignore: registry entry
		reviewLandingHref: "/verify/health", //nav-composition:ignore: registry entry
		status:            moduleStatusAvailable,
		priority:          5,
		contributions: []moduleNavContribution{
			{key: "health_adults", labelKey: "nav.health_adults", href: "/health/adults", priority: 1, requiredPermission: permissions.HealthRead}, //nav-composition:ignore: registry entry
			{key: "health_kids", labelKey: "nav.health_kids", href: "/health/kids", priority: 2, requiredPermission: permissions.HealthRead},       //nav-composition:ignore: registry entry
			{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100},                                                      //nav-composition:ignore: registry entry
		},
		reviewContributions: []moduleNavContribution{
			{key: "videos", labelKey: "nav.videos", href: "/verify/health", priority: 1, requiredPermission: permissions.VerificationReview}, //nav-composition:ignore: registry entry
			{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100},                                                //nav-composition:ignore: registry entry
		},
	},
	// "milk" is the kid-milk vertical: the two daily tasks that prepare the feed and give it.
	// Split out of Counts (maintainer decision 2026-07-31) because Counts owns herd-register
	// events (birth, death, shifting) while milk prep/feeding are a daily operational routine
	// that shares neither their grain nor their read models.
	//
	// The two pages KEEP their existing /counts/... hrefs: the move is a nav regrouping, not a
	// route change, so installed deep links, the Android L0 route set, and the admin-web
	// milk-preparation page all keep working. Verification also stays in the Counts verify
	// lens (bootstrap/api.go), so there is deliberately no reviewContributions here — a milk
	// proof is still reviewed where every other Counts proof is reviewed.
	"milk": {
		key:         "milk",
		labelKey:    "module.milk",
		landingHref: "/counts/milk-preparation", //nav-composition:ignore: registry entry
		status:      moduleStatusAvailable,
		priority:    6,
		contributions: []moduleNavContribution{
			// CountsWrite is deliberately retained: these are the same capture routes with the
			// same server-side authority (permissions/routes.go). Regrouping the drawer must not
			// silently widen or narrow who may write — hiding an item is not access control.
			{key: "milk_preparation", labelKey: "nav.milk_preparation", href: "/counts/milk-preparation", shared_key: "", priority: 1, requiredPermission: permissions.CountsWrite}, //nav-composition:ignore: registry entry
			{key: "milk_feeding", labelKey: "nav.milk_feeding", href: "/counts/milk-feeding", shared_key: "", priority: 2, requiredPermission: permissions.CountsWrite},             //nav-composition:ignore: registry entry
			// Colostrum is the newborn end of the same daily milk round: the feeds due today for
			// kids born today or yesterday (docs/decisions/colostrum-milk-module.md). It renders
			// EXISTING birth-workflow feed tasks under a day-scoped lens — completing one here and
			// completing it in Birth are the same write to the same row, so this leaf widens no
			// authority and duplicates no state. Same CountsWrite grant as its siblings, and Milk
			// Prep deliberately keeps the landing slot (maintainer decision 2026-08-06).
			{key: "colostrum", labelKey: "nav.colostrum", href: "/counts/colostrum", shared_key: "", priority: 3, requiredPermission: permissions.CountsWrite}, //nav-composition:ignore: registry entry
		},
	},
	// "approvals" is the decision surface for work RAISED in the field and applied only once
	// someone with authority says yes: birth, death, and shifting requests.
	//
	// Maintainer decision 2026-08-05, SUPERSEDING the 2026-07-21 decision that removed approvals
	// from mobile and moved them to admin-web only. Approvals are back on the phone, and this time
	// as their OWN module rather than a tab inside Counts. That distinction is the whole design:
	//
	//   - Counts stays capture-only (birth, death, shifting recording) for the operators who hold
	//     CountsWrite. It does not regain an approval tab, so TestCountsModuleRoleMatrix and
	//     TestCountsModuleBarIsCaptureOnlyAndOmitsYouTab keep asserting exactly what they assert
	//     today. Approving is not capturing, and the two audiences barely overlap.
	//   - Approvals is a separate drawer entry gated on a separate authority, so an approver who
	//     holds no CountsWrite (both named directors hold none) gets the queue and no capture
	//     tabs, while an operator gets capture tabs and no queue.
	//
	// The single nav item is gated on CountsApproveAccess -- the same coarse permission its
	// backing route requires (permissions/routes.go: GET /app/counts/approvals), so the tab and
	// the route agree. Hiding the item is NOT the access control: the handler re-checks per row
	// via DecidableApprovalRequestTypes, and the list only returns types this caller may decide.
	//
	// No "you" contribution, deliberately. Per the nav-placement rule, "You" belongs in the drawer
	// for any principal holding 2+ modules, and nobody can hold this module alone --
	// RoleCountsApprover grants no AppBootstrap, so every holder also carries a job role (and its
	// module) to have anywhere to render. A "you" here would be the exact repeat-it-in-every-bar
	// defect that rule bans.
	"approvals": {
		key:         "approvals",
		labelKey:    "module.approvals",
		landingHref: "/counts/approvals", //nav-composition:ignore: registry entry
		status:      moduleStatusAvailable,
		priority:    7,
		contributions: []moduleNavContribution{
			// labelKey reuses the pre-existing "nav.approval" key rather than minting a new one:
			// it survived the 2026-07-21 removal already translated into all four locales.
			{key: "approvals", labelKey: "nav.approval", href: "/counts/approvals", shared_key: "", priority: 1, requiredPermission: permissions.CountsApproveAccess}, //nav-composition:ignore: registry entry
		},
	},
	// Declared-but-unbuilt modules. They render as disabled "Soon" drawer rows so the
	// client no longer needs its own hardcoded coming-soon list.
	"breeding": {
		key:         "breeding",
		labelKey:    "module.breeding",
		landingHref: "",
		status:      moduleStatusSoon,
		priority:    6,
	},
}

// soonModuleKeys are surfaced to every principal as disabled drawer rows regardless of
// grants; they advertise the roadmap, they do not confer access.
var soonModuleKeys = []string{"breeding"}

// visibleNavigationFor composes navigation from the person's granted modules.
// If the person has any leadership grant, they see the shared module set for that tier.
// Otherwise, they see the union of their granted modules' nav contributions,
// deduped by shared_key and ordered by priority.
func visibleNavigationFor(grants []domain.GrantSummary, grantedModules []string, localeTag string) []domain.BootstrapNavigationItem {
	// A standalone verifier shows the active module's bottom bar -- the first feature from
	// verifierFeatureKeys, same resolution modulesFor uses for the drawer, so
	// visible_navigation always equals modules[0].NavItems. Built via
	// verificationModuleForFeature (not the static registry) so the bar carries the
	// feature-scoped [Verify, Alerts, You] items, never the registry's bare
	// "verification" entry. Leadership principals may also hold review permission, but
	// they still land in their leadership module rather than the verifier-only app.
	if isStandaloneVerifierPrincipal(grants) {
		features := verifierFeatureKeys(grantedModules)
		if len(features) == 0 {
			return []domain.BootstrapNavigationItem{}
		}
		return verificationModuleForFeature(features[0], grants, localeTag).NavItems
	}

	// Leadership principals default to their curated module set. There is no synthetic
	// leadership/overview screen; preventive-care leaders land on the shared
	// Vaccination module, while CEO gets Vaccination plus org-level modules.
	if isLeadershipPrincipal(grants) {
		keys := leadershipModuleKeys(grants)
		if len(keys) == 0 {
			return []domain.BootstrapNavigationItem{}
		}
		return composeNavigationFromModules([]string{keys[0]}, grants, localeTag)
	}

	// Non-leadership operators get the bar of their ACTIVE module. The bar is
	// module-scoped, so composing every granted module into one flat bar would produce
	// an unusable 6+ tab bar as modules are added. The client switches the active module
	// via the drawer and renders that module's items from BootstrapResponse.Modules;
	// VisibleNavigation carries the default (first available) module's bar.
	active := activeModuleKey(grants, grantedModules)
	if active == "" {
		return []domain.BootstrapNavigationItem{}
	}
	return composeNavigationFromModules([]string{active}, grants, localeTag)
}

// grantsHavePermission reports whether ANY of the principal's active grant roles holds
// the permission. Mirrors how routePermissions is evaluated, so a nav item and its route
// agree on who may reach it.
func grantsHavePermission(grants []domain.GrantSummary, permission string) bool {
	if permission == "" {
		return true
	}
	for _, g := range grants {
		if permissions.RoleHasPermission(g.Role, permission) {
			return true
		}
	}
	return false
}

func grantsHaveAnyPermission(grants []domain.GrantSummary, required []string) bool {
	if len(required) == 0 {
		return true
	}
	for _, permission := range required {
		if grantsHavePermission(grants, permission) {
			return true
		}
	}
	return false
}

// permittedContributions returns the module's nav items this principal may actually
// reach. A module whose every item is gated away is not renderable for them.
func permittedContributions(def moduleDefinition, grants []domain.GrantSummary) []moduleNavContribution {
	contributions := def.contributions
	if usesVerificationReviewLens(grants) {
		contributions = def.reviewContributions
	}
	out := make([]moduleNavContribution, 0, len(contributions))
	for _, contrib := range contributions {
		if !grantsHavePermission(grants, contrib.requiredPermission) {
			continue
		}
		if !grantsHaveAnyPermission(grants, contrib.requiredAnyPermission) {
			continue
		}
		if contrib.excludedPermission != "" && grantsHavePermission(grants, contrib.excludedPermission) {
			continue
		}
		out = append(out, contrib)
	}
	return out
}

// candidateModuleKeys are the modules a principal may be offered before permission
// filtering. Leadership is org-level and is NOT department-scoped: a CEO is not a member
// of a department, so gating them on department_module_grants would hide every module
// from them. Their access is decided by permission alone. Everyone else is limited to
// the modules their department is granted.
//
// For a verifier:
//   - Multi-module verifier (≥2 verify duties): grantedModules contains the features they verify.
//     The returned keys are the module keys as they appear in grantedModules (e.g. "vaccination",
//     "weighing", etc.), and modulesFor will compose per-feature verification modules for each.
//   - Single-module verifier (0-1 verify duties): return ["verification"] for the generic module.
func candidateModuleKeys(grants []domain.GrantSummary, grantedModules []string) []string {
	if isStandaloneVerifierPrincipal(grants) {
		// Every standalone verifier is scoped to the feature(s) their verify duties name,
		// or -- for a coarse department-level "verification" grant / no duties at all --
		// every built feature. See verifierFeatureKeys.
		features := verifierFeatureKeys(grantedModules)
		normalized := make([]string, 0, len(features))
		for _, key := range features {
			normalized = append(normalized, normalizeModuleFeatureKey(key))
		}
		return normalized
	}
	if !isLeadershipPrincipal(grants) {
		return grantedModules
	}
	return leadershipModuleKeys(grants)
}

// usesVerificationReviewLens selects the cross-module evidence workspace by authority,
// not by a hardcoded role template. Leadership roles also hold verification.act and keep
// their operational/action lens; a review-only principal receives module review pages.
func usesVerificationReviewLens(grants []domain.GrantSummary) bool {
	return grantsHavePermission(grants, permissions.VerificationReview) &&
		!grantsHavePermission(grants, permissions.VerificationAct)
}

// reviewableModuleKeys derives the verifier drawer from module registry entries. Adding a
// module's evidence lens is therefore one registry edit, not another per-role module array.
func reviewableModuleKeys() []string {
	defs := make([]moduleDefinition, 0, len(moduleNavRegistry))
	for _, def := range moduleNavRegistry {
		if def.status == moduleStatusAvailable && len(def.reviewContributions) > 0 {
			defs = append(defs, def)
		}
	}
	// Total order: the registry is a map, so equal priorities would leave the drawer order
	// at the mercy of Go's randomized map iteration (the same principal could get a
	// different module order on two requests). Priorities are unique today; the key
	// tie-break keeps that a lock rather than a convention.
	sort.SliceStable(defs, func(i, j int) bool {
		if defs[i].priority != defs[j].priority {
			return defs[i].priority < defs[j].priority
		}
		return defs[i].key < defs[j].key
	})
	keys := make([]string, 0, len(defs))
	for _, def := range defs {
		keys = append(keys, def.key)
	}
	return keys
}

// leadershipModuleKeys is the curated drawer set for a leadership principal, before
// permission filtering. Leadership is org-level (not department-scoped), so the set
// is decided by leadership TIER, not by department_module_grants:
//
//   - CEO/CXO (ceo_internal) is whole-org: Vaccination, Weighing, Counts, built Feed and
//     Health, plus the roadmap "soon" Breeding module.
//   - Preventive-Care leadership (PC Director, Park Head) is specialty-scoped to
//     preventive care: Vaccination, Weighing, and Health. Counts, Feed, and Breeding
//     are not preventive-care surfaces. Park Head is further
//     limited to his own park by his grant scope (data scope), not by nav.
//   - Growth Director is specialty-scoped to Weighing only.
//
// Merge note (2026-07-31): main had narrowed PC leadership to Vaccination only while
// this branch was giving it Weighing + Health. Maintainer chose the union, so PC
// leadership keeps Weighing and Health and Growth Director is added alongside.
//
// Verification belongs to the verifier role, not leadership nav.
func leadershipModuleKeys(grants []domain.GrantSummary) []string {
	keys := make([]string, 0, 8)
	if hasRole(grants, permissions.RoleCEOInternal) {
		// Not an early return any more: the approvals offer below is keyed on a PERMISSION and
		// must apply to the CEO too. Returning here would have made the one module the CEO most
		// obviously owns the one module the CEO could not see.
		keys = appendMissing(keys, "vaccination", "weighing", "counts", "feed_direction", "aas_health", "milk", "breeding")
	}
	// PC Director / Park Head: preventive-care specialty verticals.
	// Growth Director is a separate specialty and may be held alongside them, so the sets are
	// unioned rather than returned early. appendMissing keeps the result duplicate-free: a
	// principal holding BOTH would otherwise contribute "weighing" twice and render it twice.
	if hasRole(grants, permissions.RolePCDirector) || hasRole(grants, permissions.RoleParkHead) {
		keys = appendMissing(keys, "vaccination", "weighing", "aas_health")
	}
	if hasRole(grants, permissions.RoleGrowthDirector) {
		keys = appendMissing(keys, "weighing")
	}
	// Feed Director -> Feed, Health Director -> Counts (maintainer decision 2026-08-01, one
	// module per director). Both roles are in leadershipGrantRoles, so WITHOUT these entries the
	// leadership branch above resolved len(keys)==0 and /app/bootstrap returned an EMPTY nav and
	// an EMPTY drawer for them -- a role that can log in and see nothing.
	//
	// The keys are the OFFER; permission filtering still decides what renders. Counts is an OFF
	// feature (AGENTS.md) and health_director deliberately holds no counts.read/counts.write, so
	// every Counts nav item is gated away from him and the Counts module contributes nothing
	// until the feature is switched on. Feed is a declared roadmap module (moduleStatusSoon), so
	// feed_director sees its "Soon" drawer row and no bottom bar until the Feed surface is built.
	// Both are asserted in bootstrap_copy_test.go so the offer cannot silently become access.
	if hasRole(grants, permissions.RoleFeedDirector) {
		keys = appendMissing(keys, "feed_direction")
	}
	if hasRole(grants, permissions.RoleHealthDirector) {
		keys = appendMissing(keys, "counts")
	}
	// Approvals is offered by PERMISSION, not by role (maintainer decision 2026-08-05, "rbac per
	// person, not per group"). Every entry above asks "which job is this?"; this one asks "may
	// this person approve?", which is the only question that has a per-person answer.
	//
	// That is what makes the authority portable: RoleCountsApprover is granted to named
	// individuals on their own user_scope_grants row, and their job role (pc_director,
	// growth_director) is untouched. Keying the offer on the role instead would have forced a
	// second edit here every time another person is granted the authority, and keying it on the
	// JOB would have handed it to every future holder of that job -- the exact widening this
	// design exists to avoid.
	//
	// The CEO tier reaches this through ceo_internal, which carries the same permission directly.
	if grantsHavePermission(grants, permissions.CountsApproveAccess) {
		keys = appendMissing(keys, "approvals")
	}
	// Herd Operations (Counts) capture is offered the SAME per-person way as approvals above, and
	// for the same reason (maintainer decision 2026-08-07, extending "rbac per person, not per
	// group"). A leadership principal who has been granted counts.write ON THEIR OWN GRANT ROW --
	// today Chandrakant and Dinakar, each holding a tenant `operator` grant alongside their
	// director job -- gets the capture module. Their job roles are untouched.
	//
	// Read this together with the health_director branch above, because the two look contradictory
	// and are not. That branch offers "counts" on the JOB because health_director is the documented
	// Counts OWNER; it renders nothing today precisely because that role holds no counts.write.
	// This branch offers it on the PERMISSION, so ownership and access stay separate decisions.
	//
	// Deliberately NOT keyed on pc_director / growth_director. Keying it on either job would hand
	// Counts to every future holder of that job, reverse the one-module-one-director segregation
	// lock (permissions.TestDirectorHoldsNoOtherModulesCapabilities asserts pc_director holds no
	// CountsRead), and override health_director as the Counts owner. That exact alternative was
	// offered to the maintainer and declined; see AGENTS.md -> Approvals-on-mobile rule.
	//
	// Nor is it keyed on the counts.write PERMISSION, which reads like the natural choice and is
	// wrong: park_head holds counts.write on the ROLE, and TestCountsModuleRoleMatrix pins that a
	// park head does NOT get the capture module. Keying on the permission therefore widened the
	// offer to that job as well -- the same defect one layer over. The explicit `operator` GRANT is
	// the per-person fact: it is what perPersonGrants layers onto a named individual (and what the
	// stg-operator-scope guard makes them justify), so it names the person, not the job.
	if hasRole(grants, permissions.RoleOperator) {
		keys = appendMissing(keys, "counts")
	}
	return keys
}

// appendMissing appends each key not already present, preserving order.
func appendMissing(keys []string, add ...string) []string {
	for _, key := range add {
		found := false
		for _, existing := range keys {
			if existing == key {
				found = true
				break
			}
		}
		if !found {
			keys = append(keys, key)
		}
	}
	return keys
}

// hasRole reports whether any active grant carries the given role.
func hasRole(grants []domain.GrantSummary, role string) bool {
	for _, g := range grants {
		if g.Role == role {
			return true
		}
	}
	return false
}

func hasPermission(grants []domain.GrantSummary, permission string) bool {
	for _, g := range grants {
		if permissions.RoleHasPermission(g.Role, permission) {
			return true
		}
	}
	return false
}

func canViewProtocolAdherenceCard(grants []domain.GrantSummary) bool {
	return hasRole(grants, permissions.RoleCEOInternal)
}

func canExecuteVaccination(grants []domain.GrantSummary, grantedModules []string) bool {
	return hasPermission(grants, permissions.TaskExecute) && canUseModule(grants, grantedModules, "vaccination")
}

func canExecuteWeighing(grants []domain.GrantSummary, grantedModules []string) bool {
	return hasPermission(grants, permissions.WeighingExecute) && canUseModule(grants, grantedModules, "weighing")
}

// canOverseeWeighingOperators gates the read-only Operators surface -- weighing shed tasks
// assigned to SOMEONE ELSE. It mirrors canExecuteWeighing so the client never has to infer the
// surface from a role name; the write path still requires the caller to be the shed's assignee,
// so this flag widens what is visible and never what is recordable.
func canOverseeWeighingOperators(grants []domain.GrantSummary, grantedModules []string) bool {
	return hasPermission(grants, permissions.WeighingOverseeOperators) && canUseModule(grants, grantedModules, "weighing")
}

func canUseVerificationVideoControls(grants []domain.GrantSummary) bool {
	return isLeadershipPrincipal(grants)
}

func canUseModule(grants []domain.GrantSummary, grantedModules []string, module string) bool {
	for _, key := range candidateModuleKeys(grants, grantedModules) {
		if key == module {
			return true
		}
	}
	return false
}

// countAvailableModules counts modules the principal can actually render: known,
// available, and with at least one permitted nav item. Unknown, "soon", or
// fully-gated-away modules do not count toward the drawer threshold.
func countAvailableModules(grants []domain.GrantSummary, modules []string) int {
	seen := make(map[string]bool, len(modules))
	count := 0
	for _, key := range modules {
		def, ok := moduleNavRegistry[key]
		if !ok || def.status != moduleStatusAvailable || seen[def.key] {
			continue
		}
		if len(permittedContributions(def, grants)) == 0 {
			continue
		}
		seen[def.key] = true
		count++
	}
	return count
}

// activeModuleKey picks the default module for a principal: the lowest-priority
// available module with at least one permitted item. Returns "" when none is renderable.
func activeModuleKey(grants []domain.GrantSummary, grantedModules []string) string {
	best := ""
	bestPriority := 0
	for _, key := range grantedModules {
		def, ok := moduleNavRegistry[key]
		if !ok || def.status != moduleStatusAvailable {
			continue
		}
		if len(permittedContributions(def, grants)) == 0 {
			continue
		}
		if best == "" || def.priority < bestPriority {
			best = def.key
			bestPriority = def.priority
		}
	}
	return best
}

// normalizeModuleFeatureKey maps a raw feature/module key to its base module id.
// Keys may come as "vaccination", "pc.vaccination", "weighing", "feed.direction", etc.
// ("pc." prefix stripped, dots converted to underscores, e.g. "feed.direction" →
// "feed_direction"). Shared by verificationModuleForFeature and the verifier feature
// resolution in candidateModuleKeys/modulesFor/visibleNavigationFor so all three agree
// on the same module identity for a given raw key.
func normalizeModuleFeatureKey(key string) string {
	if strings.HasPrefix(key, "pc.") {
		key = strings.TrimPrefix(key, "pc.")
	}
	key = strings.ReplaceAll(key, ".", "_")
	return key
}

// builtVerifiableFeatures lists the shipped feature modules a verifier's [Verify, Alerts]
// bar can be scoped to, in drawer priority order. Only "available" (built) modules are
// eligible -- verifiers review evidence for shipped features, not roadmap ones.
var builtVerifiableFeatures = []string{"vaccination", "weighing", "counts"}

// verifierFeatureKeys resolves a verifier's grantedModules (from ListGrantedModuleKeys)
// into the feature keys their per-module [Verify, Alerts] bar is built for.
// grantedModules mixes two sources:
//   - feature-specific verify duties from position_module_duties, e.g. "vaccination" or
//     "pc.vaccination" -- these carry real feature identity.
//   - a coarse department-level "verification" grant (department_module_grants.module_key
//     = "verification") with no feature attached.
//
// The literal "verification" key carries no feature identity, so it is dropped here. If
// nothing feature-specific remains -- a department-level grant only, or no duties
// recorded at all -- the verifier is scoped to every built feature, the same "review
// everything shipped" default a CEO gets. This is the only way to honor the binding
// [Verify, Alerts]-per-module ruling (drawer + per-feature bar, never a merged/un-scoped
// Alerts tab) for a verifier whose grant does not itself name a feature.
// Dedupe is on the NORMALIZED key, and that ordering is the whole point. grantedModules is a
// SQL UNION of two tables that spell the same module differently -- department_module_grants
// says "vaccination" and "feed_direction" where position_module_duties says "pc.vaccination"
// and "feed.direction". UNION only collapses byte-identical strings, so both spellings arrive
// here. Comparing raw keys (as this did until 2026-08-07) let each pair through, and since
// normalizeModuleFeatureKey collapses them a step LATER, at render time, the drawer built two
// modules with the identical key "verify_vaccination" and showed a verifier two rows labelled
// "Vaccination" that she could not tell apart. Counts and Weighing were spelled the same in
// both tables, which is the only reason they were not duplicated too.
//
// Normalizing BEFORE the dedupe fixes every present and future spelling drift. Fixing only the
// data (renaming the duty rows) would clear the symptom and leave the next spelling to
// re-break it. Returning normalized keys is safe: normalizeModuleFeatureKey is idempotent and
// candidateModuleKeys already normalizes this result again.
func verifierFeatureKeys(grantedModules []string) []string {
	out := make([]string, 0, len(grantedModules))
	seen := make(map[string]bool, len(grantedModules))
	for _, key := range grantedModules {
		if key == "verification" {
			continue
		}
		normalized := normalizeModuleFeatureKey(key)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return append([]string(nil), builtVerifiableFeatures...)
	}
	return out
}

// verificationModuleForFeature builds a per-feature verification module for a verifier
// who has verify duty on that feature. It uses the feature's label and contributes:
//   - Verify: the verify/video queue for that feature
//   - Alerts: the process-integrity alerts feed for that feature, scoped via
//     verificationCategoryForFeature so the category the nav emits matches the category
//     the /verify/alerts endpoint actually filters on
//   - You: the account tab (shared across all modules)
func verificationModuleForFeature(featureKey string, grants []domain.GrantSummary, localeTag string) domain.BootstrapModule {
	normalized := normalizeModuleFeatureKey(featureKey)

	// This map exists for features whose module key and copy key DIFFER. The
	// "module."+normalized fallback below is a GUESS that happens to be right when the two
	// coincide (milk, breeding) and silently wrong when they do not: aas_health guessed
	// "module.aas_health" while the catalog spells Health "module.health", so
	// localizedBootstrapLabel returned "" and a verifier got a NAMELESS drawer row -- in
	// every locale, on a real phone, with nothing failing anywhere (2026-08-07).
	//
	// A missing catalog key is invisible by construction, so the guard is a test rather than
	// a runtime error: TestEveryVerifierModuleLabelResolvesInTheCopyCatalog builds every
	// feature the drawer can emit, in every locale, and fails on an empty label. Add the
	// entry HERE when a new feature's copy key differs from its module key.
	labelKeys := map[string]string{
		"vaccination":    "module.vaccination",
		"weighing":       "module.weighing",
		"counts":         "module.counts",
		"feed_direction": "module.feed_direction",
		"aas_health":     "module.health",
	}
	labelKey, ok := labelKeys[normalized]
	if !ok {
		labelKey = "module." + normalized
	}

	// Compose nav items: verify + alerts. Alerts hits the real /verify/alerts
	// endpoint (internal/verification/adapters/http/handler.go ListAlerts), scoped with
	// the category verificationCategoryForFeature maps THIS feature to -- that mapping
	// must match the category value the feature's own verification-bridge writes onto
	// verification_items.category, or the tab renders 200-with-empty-list forever.
	//
	// Maintainer decision 2026-08-03, two parts:
	//   1. The tab is titled just "Alerts". The alerts ARE feature-scoped -- the href still
	//      carries the category -- but the LABEL must not name the feature. The verifier is
	//      already standing in that module, so "Vaccination alerts" / "Weighing alerts" only
	//      repeats it back at them.
	//   2. "You" is CONTRIBUTED here but its final home is decided downstream, by
	//      applyProfileEntryPlacement in service.go, on the same >=2-modules threshold that
	//      decides the drawer exists:
	//        - verifier with ONE feature  -> minimal chrome, no drawer, You stays on this bar
	//          (it is his only route to /you).
	//        - verifier with TWO OR MORE  -> expanded chrome, and You is stripped from this
	//          bar and from every other feature's bar; the drawer footer carries it once.
	//      There is no verifier exception to that rule -- the carve-out that used to exist is
	//      what put You in the drawer footer AND in every verify feature's bottom bar.
	//      Note that shared_key is inert on this path: this function builds NavItems by hand
	//      and never calls composeNavigationFromModules, the only reader of shared_key. It is
	//      the placement rule, not the dedupe, that keeps You single.
	// Both hrefs carry the feature's verification CATEGORY, and the alerts one names the client
	// destination rather than the API path. Two separate defects lived here:
	//
	//  1. module= alone did not survive the trip. The client resolves a queue by category, and its
	//     module->category map knows only weighing; every other value (counts, feed_direction, and
	//     any feature added later) fell through to vaccination, so a Counts verifier's Verify tab
	//     opened VACCINATION proofs -- other people's work, in the wrong module. The category is
	//     the identity that actually scopes the queue, so it is sent explicitly instead of being
	//     re-derived from a key the client has to keep a private table for. module= stays for the
	//     drawer's own active-entry comparison.
	//  2. Alerts pointed at "/verify/alerts", which was the backend API path
	//     (internal/verification/adapters/http/handler.go ListAlerts) and NOT a destination the
	//     app hosted -- a dead tab whose tap resolved to nothing. That half is closed on the
	//     client, which now registers "/verify/alerts?category=" as a real destination reading
	//     the same pending queue; the href stays as-is precisely because it is that contract, and
	//     it must keep carrying the category or the feed stops being feature-scoped.
	category := verificationCategoryForFeature(normalized)
	items := []moduleNavContribution{
		{key: "verify", labelKey: "nav.verify", href: verifyQueueHref(normalized), shared_key: "", priority: 0, requiredPermission: permissions.VerificationReview},
		{key: "alerts", labelKey: "nav.alerts", href: "/verify/alerts?category=" + category, shared_key: "", priority: 20, requiredPermission: ""},
		{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100, requiredPermission: ""},
	}

	// Filter to permitted items
	permittedItems := make([]moduleNavContribution, 0, len(items))
	for _, item := range items {
		if grantsHavePermission(grants, item.requiredPermission) {
			permittedItems = append(permittedItems, item)
		}
	}

	// Build nav items in order
	navItems := make([]domain.BootstrapNavigationItem, 0, len(permittedItems))
	for _, item := range permittedItems {
		navItems = append(navItems, domain.BootstrapNavigationItem{
			Key:   item.key,
			Label: localizedBootstrapLabel(localeTag, item.labelKey),
			Href:  item.href,
		})
	}

	// Use a distinct key for the verifier's per-feature module (e.g., "verify_vaccination"
	// instead of "vaccination") to avoid colliding with operator/leadership modules.
	// The drawer shows the feature name as the label, but the key uniquely identifies
	// this as a verification module.
	verifyModuleKey := "verify_" + normalized

	return domain.BootstrapModule{
		Key:      verifyModuleKey,
		Label:    localizedBootstrapLabel(localeTag, labelKey),
		Href:     verifyQueueHref(normalized),
		Status:   moduleStatusAvailable,
		NavItems: navItems,
	}
}

// modulesFor builds the drawer: every module the principal can render (with its own
// permission-filtered, module-scoped bar), followed by the declared "soon" modules as
// disabled rows. A module the registry does not know, or whose every page is gated away
// from this principal, contributes nothing.
//
// For a multi-module verifier (≥2 verify duties), per-feature verification modules are
// composed synthetically (verificationModuleForFeature) rather than looked up in the registry.
// For a single-module verifier, the generic "verification" module from the registry is used.
func modulesFor(grants []domain.GrantSummary, grantedModules []string, localeTag string) []domain.BootstrapModule {
	// Standalone verifier: ALWAYS compose per-feature verification modules (one drawer
	// entry per feature, each with its own [Verify, Alerts, You] bar) rather than looking
	// up registry modules. This applies uniformly regardless of how many verify duties the
	// principal holds -- one, several, or a coarse department-level "verification" grant
	// with none named -- because the binding nav ruling bans a merged/un-scoped Alerts
	// tab (docs: maintainer ruling "Alerts are NOT one merged tab"; see
	// verifierFeatureKeys for how the feature set is resolved). Verifiers only verify
	// built features, not "soon" roadmap modules.
	if isStandaloneVerifierPrincipal(grants) {
		features := verifierFeatureKeys(grantedModules)
		out := make([]domain.BootstrapModule, 0, len(features))
		for _, featureKey := range features {
			normalized := normalizeModuleFeatureKey(featureKey)
			// Check if the module exists in the registry. Verifiers can verify both
			// available and "soon" modules if they have explicit duties on them (unchanged
			// from the pre-existing multi-module verifier behavior this generalizes).
			_, ok := moduleNavRegistry[normalized]
			if !ok {
				continue
			}
			module := verificationModuleForFeature(featureKey, grants, localeTag)
			// Only include the module if it has at least one permitted nav item.
			if len(module.NavItems) > 0 {
				out = append(out, module)
			}
		}
		return out
	}

	keys := candidateModuleKeys(grants, grantedModules)

	// Standard path: look up modules in the registry (for operators and leadership).
	available := make([]moduleDefinition, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		def, ok := moduleNavRegistry[key]
		if !ok || def.status != moduleStatusAvailable || seen[def.key] {
			continue
		}
		if len(permittedContributions(def, grants)) == 0 {
			continue
		}
		seen[def.key] = true
		available = append(available, def)
	}
	// Same total-order rule as reviewableModuleKeys: never let map iteration decide the
	// drawer order when two modules share a priority.
	sort.SliceStable(available, func(i, j int) bool {
		if available[i].priority != available[j].priority {
			return available[i].priority < available[j].priority
		}
		return available[i].key < available[j].key
	})

	out := make([]domain.BootstrapModule, 0, len(available)+len(soonModuleKeys))
	for _, def := range available {
		items := composeNavigationFromModules([]string{def.key}, grants, localeTag)
		// Land on the first page this principal may actually open. The declared
		// landingHref can be gated away (an Operator holds Counts but not the census
		// page at /counts), and landing them on a route that 403s would be a
		// self-inflicted dead end.
		href := def.landingHref
		if usesVerificationReviewLens(grants) {
			href = def.reviewLandingHref
		}
		if len(items) > 0 && !navItemsContainHref(items, href) {
			href = items[0].Href
		}
		out = append(out, domain.BootstrapModule{
			Key:      def.key,
			Label:    localizedBootstrapLabel(localeTag, def.labelKey),
			Href:     href,
			Status:   def.status,
			NavItems: items,
		})
	}
	// "Soon" roadmap rows advertise unbuilt modules, but only when that module is
	// in the principal's offered module set. A field operator granted Vaccination
	// and Weighing should not see unrelated modules such as Counts, Feed, or Breeding.
	offered := make(map[string]bool, len(keys))
	for _, k := range keys {
		offered[k] = true
	}
	allowSoon := func(k string) bool { return offered[k] }
	for _, key := range soonModuleKeys {
		def, ok := moduleNavRegistry[key]
		if !ok || seen[def.key] || !allowSoon(key) {
			continue
		}
		out = append(out, domain.BootstrapModule{
			Key:      def.key,
			Label:    localizedBootstrapLabel(localeTag, def.labelKey),
			Href:     def.landingHref,
			Status:   def.status,
			NavItems: []domain.BootstrapNavigationItem{},
		})
	}
	return out
}

// navItemsContainHref reports whether href is one of the composed items.
func navItemsContainHref(items []domain.BootstrapNavigationItem, href string) bool {
	if href == "" {
		return false
	}
	for _, item := range items {
		if item.Href == href {
			return true
		}
	}
	return false
}

// composeNavigationFromModules unions nav items from the given modules,
// deduping by shared_key and ordering by priority, and dropping items whose
// requiredPermission this principal does not hold.
func composeNavigationFromModules(modules []string, grants []domain.GrantSummary, localeTag string) []domain.BootstrapNavigationItem {
	// Collect all contributions, tracking which shared_key we've seen
	collected := make([]moduleNavContribution, 0)
	seenSharedKey := make(map[string]bool)
	seenKey := make(map[string]bool)

	for _, mod := range modules {
		def, ok := moduleNavRegistry[mod]
		if !ok {
			continue
		}
		for _, contrib := range permittedContributions(def, grants) {
			if contrib.shared_key != "" {
				// Shared item: keep the first module's version; skip duplicates
				if !seenSharedKey[contrib.shared_key] {
					seenSharedKey[contrib.shared_key] = true
					collected = append(collected, contrib)
				}
			} else {
				// Non-shared item: keep it once per key
				if !seenKey[contrib.key] {
					seenKey[contrib.key] = true
					collected = append(collected, contrib)
				}
			}
		}
	}

	// Convert to output in the same order (priority ordering happens within module registry)
	out := make([]domain.BootstrapNavigationItem, 0, len(collected))
	for _, item := range collected {
		out = append(out, domain.BootstrapNavigationItem{
			Key:   item.key,
			Label: localizedBootstrapLabel(localeTag, item.labelKey),
			Href:  item.href,
		})
	}

	return out
}

func queuesFor(caps []domain.CapabilityAssignment, localeTag string) []domain.BootstrapTaskQueue {
	items := []domain.BootstrapTaskQueue{
		{Key: "assigned", Label: localizedBootstrapLabel(localeTag, "queue.assigned"), RequiredCapabilities: []string{}},
	}
	if hasCapability(caps, "movement.execute") {
		items = append(items, domain.BootstrapTaskQueue{Key: "shifting", Label: localizedBootstrapLabel(localeTag, "queue.shifting"), RequiredCapabilities: []string{"movement.execute"}})
	}
	if hasCapability(caps, "proof.verify") {
		items = append(items, domain.BootstrapTaskQueue{Key: "proof_review", Label: localizedBootstrapLabel(localeTag, "queue.proof_review"), RequiredCapabilities: []string{"proof.verify"}})
	}
	return items
}

func localizedBootstrapLabel(localeTag, key string) string {
	tag := localization.Normalize(localeTag)
	if labels, ok := bootstrapLabels[tag]; ok {
		if value := labels[key]; value != "" {
			return value
		}
	}
	return bootstrapLabels[localization.DefaultTag][key]
}

var bootstrapLabels = map[string]map[string]string{
	"en": {
		"nav.overview":         "Overview",
		"nav.calendar":         "Calendar",
		"nav.alerts":           "Alerts",
		"nav.drives":           "Drives",
		"nav.birth":            "Birth",
		"nav.death":            "Death",
		"nav.shifting":         "Shifting",
		"nav.milk_preparation": "Milk Prep",
		"nav.milk_feeding":     "Milk Feeding",
		"nav.colostrum":        "Colostrum",
		"nav.feed_direction":   "Feed Direction",
		"nav.feed_packing":     "Feed Packing",
		"nav.feed_transport":   "Feed Transport",
		"nav.birth_death":      "Birth/Death",
		"nav.approval":         "Approval",
		"nav.weighing":         "Weighing",
		"nav.videos":           "Videos",
		"nav.you":              "You",
		"nav.health_adults":    "Adults",
		"nav.health_kids":      "Kids",
		"nav.verify":           "Verify",
		"nav.counts":           "Counts",
		"nav.my_work":          "My work",
		"nav.tasks":            "Tasks",
		"nav.operators":        "Operators",
		"nav.weights":          "Weights",
		"nav.growth":           "Growth",

		"module.vaccination":    "Vaccination",
		"module.weighing":       "Weighing",
		"module.counts":         "Herd Operations",
		"module.feed_direction": "Feed",
		"module.breeding":       "Breeding",
		"module.health":         "Health",
		"module.milk":           "Milk",
		"module.approvals":      "Approvals",
		"queue.assigned":        "Assigned work",
		"queue.shifting":        "Shifting",
		"queue.proof_review":    "Proof review",
	},
	"hi": {
		"nav.overview":         "अवलोकन",
		"nav.calendar":         "कैलेंडर",
		"nav.alerts":           "अलर्ट",
		"nav.drives":           "ड्राइव",
		"nav.birth":            "जन्म",
		"nav.death":            "मृत्यु",
		"nav.shifting":         "शिफ्टिंग",
		"nav.milk_preparation": "दूध तैयारी",
		"nav.milk_feeding":     "दूध पिलाना",
		"nav.colostrum":        "खीस",
		"nav.feed_direction":   "फ़ीड दिशा",
		"nav.feed_packing":     "फ़ीड पैकिंग",
		"nav.feed_transport":   "फ़ीड परिवहन",
		"nav.birth_death":      "जन्म/मृत्यु",
		"nav.approval":         "अनुमोदन",
		"nav.weighing":         "वजन",
		"nav.videos":           "वीडियो",
		"nav.you":              "आप",
		"nav.health_adults":    "वयस्क",
		"nav.health_kids":      "बच्चे",
		"nav.verify":           "सत्यापित करें",
		"nav.counts":           "गिनती",
		"nav.my_work":          "मेरा काम",
		"nav.tasks":            "कार्य",
		"nav.operators":        "ऑपरेटर",
		"nav.weights":          "वज़न",
		"nav.growth":           "वृद्धि",

		"module.vaccination":    "टीकाकरण",
		"module.weighing":       "वजन",
		"module.counts":         "झुंड संचालन",
		"module.feed_direction": "फ़ीड",
		"module.breeding":       "प्रजनन",
		"module.health":         "स्वास्थ्य",
		"module.milk":           "दूध",
		"module.approvals":      "अनुमोदन",
		"queue.assigned":        "सौंपा गया काम",
		"queue.shifting":        "शिफ्टिंग",
		"queue.proof_review":    "प्रूफ समीक्षा",
	},
	"kn": {
		"nav.overview":         "ಅವಲೋಕನ",
		"nav.calendar":         "ಕ್ಯಾಲೆಂಡರ್",
		"nav.alerts":           "ಎಚ್ಚರಿಕೆಗಳು",
		"nav.drives":           "ಡ್ರೈವ್‌ಗಳು",
		"nav.birth":            "ಜನನ",
		"nav.death":            "ಮರಣ",
		"nav.shifting":         "ಸ್ಥಳಾಂತರ",
		"nav.milk_preparation": "ಹಾಲು ತಯಾರಿ",
		"nav.milk_feeding":     "ಹಾಲು ಕುಡಿಸುವುದು",
		"nav.colostrum":        "ಗಿಣ್ಣು ಹಾಲು",
		"nav.feed_direction":   "ಆಹಾರ ನಿರ್ದೇಶನ",
		"nav.feed_packing":     "ಆಹಾರ ಪ್ಯಾಕಿಂಗ್",
		"nav.feed_transport":   "ಆಹಾರ ಸಾಗಣೆ",
		"nav.birth_death":      "ಜನನ/ಮರಣ",
		"nav.approval":         "ಅನುಮೋದನೆ",
		"nav.weighing":         "ತೂಕ",
		"nav.videos":           "ವೀಡಿಯೊಗಳು",
		"nav.you":              "ನೀವು",
		"nav.health_adults":    "ವಯಸ್ಕರು",
		"nav.health_kids":      "ಮಕ್ಕಳು",
		"nav.verify":           "ಪರಿಶೀಲಿಸಿ",
		"nav.counts":           "ಎಣಿಕೆ",
		"nav.my_work":          "ನನ್ನ ಕೆಲಸ",
		"nav.tasks":            "ಕಾರ್ಯಗಳು",
		"nav.operators":        "ಆಪರೇಟರ್‌ಗಳು",
		"nav.weights":          "ತೂಕ",
		"nav.growth":           "ಬೆಳವಣಿಗೆ",

		"module.vaccination":    "ಲಸಿಕೆ",
		"module.weighing":       "ತೂಕ",
		"module.counts":         "ಹಿಂಡು ಕಾರ್ಯಾಚರಣೆ",
		"module.feed_direction": "ಆಹಾರ",
		"module.breeding":       "ಸಂತಾನೋತ್ಪತ್ತಿ",
		"module.health":         "ಆರೋಗ್ಯ",
		"module.milk":           "ಹಾಲು",
		"module.approvals":      "ಅನುಮೋದನೆ",
		"queue.assigned":        "ನಿಯೋಜಿಸಿದ ಕೆಲಸ",
		"queue.shifting":        "ಸ್ಥಳಾಂತರ",
		"queue.proof_review":    "ಪುರಾವೆ ಪರಿಶೀಲನೆ",
	},
	"te": {
		"nav.overview":         "అవలోకనం",
		"nav.calendar":         "క్యాలెండర్",
		"nav.alerts":           "అలర్ట్లు",
		"nav.drives":           "డ్రైవ్‌లు",
		"nav.birth":            "జననం",
		"nav.death":            "మరణం",
		"nav.shifting":         "షిఫ్టింగ్",
		"nav.milk_preparation": "పాల తయారీ",
		"nav.milk_feeding":     "పాలు పట్టించడం",
		"nav.colostrum":        "జున్నుపాలు",
		"nav.feed_direction":   "ఫీడ్ దిశ",
		"nav.feed_packing":     "ఫీడ్ ప్యాకింగ్",
		"nav.feed_transport":   "ఫీడ్ రవాణా",
		"nav.birth_death":      "జననం/మరణం",
		"nav.approval":         "ఆమోదం",
		"nav.weighing":         "బరువు",
		"nav.videos":           "వీడియోలు",
		"nav.you":              "మీరు",
		"nav.health_adults":    "పెద్దవి",
		"nav.health_kids":      "పిల్లలు",
		"nav.verify":           "ధృవీకరించండి",
		"nav.counts":           "లెక్కలు",
		"nav.my_work":          "నా పని",
		"nav.tasks":            "పనులు",
		"nav.operators":        "ఆపరేటర్లు",
		"nav.weights":          "బరువులు",
		"nav.growth":           "పెరుగుదల",

		"module.vaccination":    "టీకా",
		"module.weighing":       "బరువు",
		"module.counts":         "మంద కార్యకలాపాలు",
		"module.feed_direction": "ఫీడ్",
		"module.breeding":       "సంతానోత్పత్తి",
		"module.health":         "ఆరోగ్యం",
		"module.milk":           "పాలు",
		"module.approvals":      "ఆమోదం",
		"queue.assigned":        "కేటాయించిన పని",
		"queue.shifting":        "షిఫ్టింగ్",
		"queue.proof_review":    "ప్రూఫ్ సమీక్ష",
	},
}

// The per-feature alerts LABEL keys ("nav.alerts.vaccination", ".weighing", ".counts",
// ".feed_direction") and their alertsLabelKeyForFeature resolver were removed by the
// maintainer decision of 2026-08-03: every verifier alerts tab is titled just "Alerts".
// The alerts themselves remain feature-scoped through verificationCategoryForFeature on
// the href -- only the label stopped naming the module the verifier is already inside.

// verifyQueueHref is the ONE place a feature key becomes a verify-queue link, so the drawer entry
// and the bar's Verify tab can never disagree about which module's proofs open.
//
// It carries the category as well as the module because the module key alone is not a queue scope:
// the client filters by category, and anything it does not recognise as a module lands on
// vaccination. Naming the category makes the scope explicit instead of guessable.
// leadershipVideosHref is the REVIEW queue plus an explicit no-status-filter selection.
//
// verifyQueueHref alone is not enough here. The review read defaults a BLANK status to `pending`
// (verification/app/service.go), and leadership's Videos tab is an audit surface where pending is
// routinely EMPTY -- every proof already has a verdict. Landing there with no status therefore
// showed the CEO an empty screen, which is how "only the rejected one is showing" would have
// become "nothing is showing" once the action-queue href was corrected. `status=all` is the
// backend's documented no-filter sentinel (handler.statusAll) and returns pending + approved +
// rejected, including already-CLOSED items, which is exactly the trail leadership must see.
func leadershipVideosHref(normalizedFeatureKey string) string {
	return verifyQueueHref(normalizedFeatureKey) + "&status=all"
}

func verifyQueueHref(normalizedFeatureKey string) string {
	return "/verify?module=" + normalizedFeatureKey + "&category=" + verificationCategoryForFeature(normalizedFeatureKey)
}

// verificationCategoryForFeature maps a MODULE key (the vocabulary nav and
// position_module_duties speak: "vaccination", "weighing", "feed_direction",
// "counts") to the VERIFICATION CATEGORY the /verify/alerts endpoint filters on
// ("vaccination_proof", "weighing_proof", ...). The two vocabularies are not the
// same, and getting this wrong is silent: the endpoint answers 200 with an empty
// list rather than an error, so the Alerts tab would look permanently empty
// instead of broken. An unmapped module falls back to "<module>_proof", which is
// the convention every current category follows.
func verificationCategoryForFeature(normalizedFeatureKey string) string {
	switch normalizedFeatureKey {
	case "vaccination":
		return "vaccination_proof"
	case "weighing":
		return "weighing_proof"
	case "counts":
		// NOT "counts_proof" -- counts has no such category. The only counts write path
		// that goes through verification is shifting execution
		// (internal/countsbridge/shifting_verification_enqueue.go), which enqueues with
		// counts/domain.VerificationCategoryShifting = "shifting_move". Emitting
		// "counts_proof" here matched nothing in verification_items.category and made
		// the counts Alerts tab silently, permanently empty (HTTP 200, zero rows).
		return "shifting_move"
	case "feed_direction":
		return "feed_distribution"
	default:
		return normalizedFeatureKey + "_proof"
	}
}
