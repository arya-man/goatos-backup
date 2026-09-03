package permissions

import (
	"sort"
	"strings"
)

// Page-grain access (maintainer decision 2026-08-27).
//
// Module ticks answer "may this person touch Feed at all". They cannot answer "Feed
// Analytics and Feed SOP, but not Feed Config" -- and that sentence is the actual shape
// of the Procurement Director's job, which is why it had to be written as a hand-coded
// LENS over the compiled contract (adminui/app/procurement_director_lens.go, maintainer
// decision 2026-08-21). That lens is now RETIRED and this file is what replaces it: the
// same narrowing, expressed as data the People screen can edit, instead of code only a
// developer can change.
//
// Three properties, each load-bearing:
//
//  1. PAGES ARE WEB-ONLY -- but MODULES are not. A page tick narrows the admin-web sidebar
//     and page contracts and never reaches Android. The phone reads the MODULE ticks
//     (workforce/app.Bootstrap), because the alternative shipped a real defect: permissions
//     came from the person's rows and the phone BAR came from department_module_grants, so
//     removing a module took the ability away in 0.02s and left the icon in place for ever
//     -- not on a refresh and not on a log out and log in, because nothing was stale.
//
//  2. AN EMPTY PAGE LIST MEANS EVERY PAGE OF THAT MODULE. This is what makes a NEW page
//     ship to whoever already holds the module rather than silently to nobody. The
//     narrowing is opt-in: you have to say "not that one".
//
//  3. THE CATALOG IS THE CONTRACT. Every admin-web nav leaf must appear here exactly
//     once, and every page contract's route must be owned by exactly one module. Both are
//     asserted against the real navigation() and pages() in adminui, so a leaf added
//     there without a row here fails the build rather than becoming invisible or
//     unwithholdable.

// ModulePage is one individually tickable admin-web screen.
type ModulePage struct {
	// Key is the nav item id. It is the id, not the href, because the href is a routing
	// detail that can change (the milk pages still live under /counts) while the tick
	// must survive that change.
	Key string
	// Module is the capability module this page belongs to. A page is only reachable when
	// its module carries at least one capability on the web surface.
	Module string
	// Label is the farm word, rendered verbatim by the access editor. It matches the
	// sidebar label so whoever assigns access is ticking the words the person will see.
	Label string
	// Href is the admin-web route. Used to filter the compiled nav/page contracts.
	Href string
	// Permissions are what the SCREEN itself needs, ANDed, matching the navigation
	// contract's own RBAC gate (adminui/app.permissionsForNav, pinned identical by
	// TestPageCatalogPermissionsMatchTheNavigationGate).
	//
	// A module tick is coarser than a screen. Health at `view` is a real grant, but Health
	// Config needs health.config.read, and Feed at `view` does not open the ration grid.
	// Without this the page was ticked, shown, and then rendered GREYED by that second RBAC
	// pass -- a visible row nobody could ever open, which is precisely the "you should not
	// even see it" case this model exists to remove. A page whose permissions the person's
	// capabilities do not produce is never ticked, so it is never shown.
	Permissions []string
}

// modulePages is the catalog, in sidebar order. Command lenses (Control Tower, Action
// Center, Protocol Adherence, Workflows) belong to `vaccination`: every one of them
// describes vaccination process gaps today, and giving them a module of their own would
// have created a module nobody's backfilled role holds.
var modulePages = []ModulePage{
	{Key: "control-tower", Module: "vaccination", Label: "Control Tower", Href: "/", Permissions: []string{ObligationRead, VaccinationRead}},
	{Key: "action-center", Module: "vaccination", Label: "Action Center", Href: "/action-center", Permissions: []string{ObligationRead, VaccinationRead}},
	{Key: "protocol-adherence", Module: "vaccination", Label: "Protocol Adherence", Href: "/protocol-adherence", Permissions: []string{ObligationRead, VaccinationRead}},
	{Key: "workflows", Module: "vaccination", Label: "Workflows", Href: "/workflows", Permissions: []string{ObligationRead, VaccinationRead}},
	{Key: "calendar", Module: "calendar", Label: "Calendar", Href: "/calendar", Permissions: []string{CalendarRead, VaccinationRead, ObligationRead}},
	{Key: "approvals", Module: "counts", Label: "Approvals", Href: "/approvals", Permissions: []string{CountsApproveAccess}},
	{Key: "verification-actions", Module: "verification", Label: "Verify", Href: "/verify", Permissions: []string{VerificationReview}},

	{Key: "preventive-care-vaccination", Module: "vaccination", Label: "Vaccination", Href: "/vaccination", Permissions: []string{ObligationRead, VaccinationRead}},
	{Key: "vaccination-live-tracker", Module: "vaccination", Label: "Live Drive Tracker", Href: "/vaccination/live-tracker", Permissions: []string{LocationsRead, ObligationRead, VaccinationRead}},
	{Key: "vaccination-plan", Module: "vaccination", Label: "Vaccination plan", Href: "/vaccination/plan", Permissions: []string{ProtocolRead}},

	{Key: "procurement-source-entry", Module: "procurement", Label: "Source Entry", Href: "/procurement/source-entry", Permissions: []string{ProcurementRead}},
	{Key: "procurement-vendors", Module: "vendors", Label: "Vendors", Href: "/procurement/vendors", Permissions: []string{VendorRead}},
	{Key: "sales-board", Module: "sales", Label: "Sales", Href: "/sales", Permissions: []string{SalesRead}},
	// Purchase and Born: per-load reconciliation and profit (maintainer decision 2026-08-31). Its
	// READ is the same commercial fact the board carries, so it ticks with the sales module; the
	// load-cost write on it is gated separately on LoadCostWrite.
	{Key: "sales-loads", Module: "sales", Label: "Purchase and Born", Href: "/sales/loads", Permissions: []string{SalesRead}},
	// Sales Config: every sales entry form in one place (maintainer decision 2026-09-01). Ticked
	// with the sales module and reached on SalesRead -- the WRITES on it carry their own keys
	// (SalesWrite, and LoadCostWrite for a load's cost), so a read-only holder sees the page with
	// its controls disabled rather than a missing leaf.
	{Key: "sales-config", Module: "sales", Label: "Sales Config", Href: "/sales/config", Permissions: []string{SalesRead}},
	{Key: "procurement-feed-purchases", Module: "feed_purchases", Label: "Feed Purchases", Href: "/procurement/feed-purchases", Permissions: []string{FeedPurchaseRead}},

	{Key: "counts-herd-analytics", Module: "counts", Label: "Herd Analytics", Href: "/counts/analytics", Permissions: []string{CountsRead}},
	{Key: "counts-breakdown", Module: "counts", Label: "Counts Breakdown", Href: "/counts/breakdown", Permissions: []string{CountsRead}},
	{Key: "counts-sops", Module: "counts", Label: "Herd Operations SOP", Href: "/counts/sops", Permissions: []string{SOPRead}},
	{Key: "milk-preparation", Module: "milk", Label: "Milk Preparation", Href: "/counts/milk-preparation", Permissions: []string{CountsRead}},
	{Key: "milk-sops", Module: "milk", Label: "Milk SOP", Href: "/milk/sops", Permissions: []string{SOPRead}},

	{Key: "herd-signals", Module: "herd_signals", Label: "Live Monitor", Href: "/herd-signals", Permissions: []string{HerdSignalsRead}},

	// weighing-weights is parked from the sidebar (maintainer request 2026-09-03), so it has no
	// catalog row: a tickable page must be a nav leaf. The page stays served for deep links.
	// The load comparison is ADG Analytics' Load-wise tab, not a page of its own.
	{Key: "weighing-analytics", Module: "weighing", Label: "ADG Analytics", Href: "/weighing/analytics", Permissions: []string{WeighingMonitor}},
	{Key: "weighing-sops", Module: "weighing", Label: "Weighing SOP", Href: "/weighing/sops", Permissions: []string{SOPRead}},

	{Key: "feed-config", Module: "feed_direction", Label: "Feed Config", Href: "/feed/config", Permissions: []string{FeedConfigRead}},
	{Key: "feed-analytics", Module: "feed_direction", Label: "Feed Analytics", Href: "/feed/analytics", Permissions: []string{FeedAnalyticsStockRead}},
	{Key: "feed-sops", Module: "feed_direction", Label: "Feed SOP", Href: "/feed/sops", Permissions: []string{SOPRead}},

	{Key: "health-config", Module: "aas_health", Label: "Health Config", Href: "/health/config", Permissions: []string{HealthConfigRead}},

	{Key: "audit-log", Module: "operations", Label: "Audit Log", Href: "/operations/audit", Permissions: []string{OperatorsViewAudit}},
	{Key: "dlq-center", Module: "operations", Label: "DLQ Center", Href: "/operations/dlq", Permissions: []string{OperatorsViewAudit}},
	{Key: "people", Module: "people", Label: "People / HRMS", Href: "/people", Permissions: []string{OperatorsRead}},
}

// moduleRoutePrefixes says which module owns a ROUTE NAMESPACE, for the page contracts
// and route labels that have no sidebar leaf of their own -- drilldowns (/goats/{id},
// /workflows/{row_id}), and pages deliberately withheld from the sidebar but still
// reachable by deep link (/counts/herd, /feed/packing, /feed/direction).
//
// Matched LONGEST PREFIX FIRST, which is what lets /counts/herd belong to the Herd
// Register module while the rest of /counts belongs to Counts. "/" is matched exactly and
// never as a prefix, or it would own every route in the product.
var moduleRoutePrefixes = map[string]string{
	"/":                           "vaccination",
	"/action-center":              "vaccination",
	"/protocol-adherence":         "vaccination",
	"/workflows":                  "vaccination",
	"/vaccination":                "vaccination",
	"/calendar":                   "calendar",
	"/approvals":                  "counts",
	"/verify":                     "verification",
	"/procurement/source-entry":   "procurement",
	"/procurement/vendors":        "vendors",
	"/admin/goats/sale":           "sale_allocation",
	"/sales":                      "sales",
	"/sales/loads":                "sales",
	"/procurement/feed-purchases": "feed_purchases",
	"/counts":                     "counts",
	"/counts/herd":                "herd_register",
	"/milk":                       "milk",
	"/counts/milk-preparation":    "milk",
	"/goats":                      "herd_register",
	"/herd-signals":               "herd_signals",
	"/weighing":                   "weighing",
	"/feed":                       "feed_direction",
	"/health":                     "aas_health",
	"/operations":                 "operations",
	"/people":                     "people",
}

var modulePageIndex = func() map[string]ModulePage {
	out := make(map[string]ModulePage, len(modulePages))
	for _, p := range modulePages {
		if _, dup := out[p.Key]; dup {
			panic("permissions: duplicate module page key " + p.Key)
		}
		if _, known := moduleCapabilityIndex[p.Module]; !known {
			panic("permissions: module page " + p.Key + " names unknown module " + p.Module)
		}
		out[p.Key] = p
	}
	return out
}()

var pagesByModule = func() map[string][]ModulePage {
	out := make(map[string][]ModulePage, len(moduleCapabilities))
	for _, p := range modulePages {
		out[p.Module] = append(out[p.Module], p)
	}
	return out
}()

// ModulePages returns the whole page catalog, in sidebar order.
func ModulePages() []ModulePage {
	out := make([]ModulePage, len(modulePages))
	copy(out, modulePages)
	return out
}

// PagesForModule returns a module's individually tickable pages, in sidebar order. A
// module with none (pc_care, toxin, herd_register, config, locations, verification_policy)
// has no admin-web sidebar leaf of its own -- its screens are either phone-only or reached
// from inside another page.
func PagesForModule(moduleKey string) []ModulePage {
	src := pagesByModule[moduleKey]
	out := make([]ModulePage, len(src))
	copy(out, src)
	return out
}

// ModuleOwningRoute reports which module owns an admin-web route, by longest matching
// prefix. Query strings and fragments are stripped first, so a href carrying ?category=
// cannot dodge the match.
func ModuleOwningRoute(href string) (string, bool) {
	path := strings.TrimSpace(href)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		return "", false
	}
	if path == "/" {
		return moduleRoutePrefixes["/"], true
	}
	best, bestModule := "", ""
	for prefix, module := range moduleRoutePrefixes {
		if prefix == "/" {
			continue
		}
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			if len(prefix) > len(best) {
				best, bestModule = prefix, module
			}
		}
	}
	if bestModule == "" {
		return "", false
	}
	return bestModule, true
}

// PageAccess is the resolved answer for one principal: which admin-web pages they may
// reach, and which modules they hold on the web at all.
type PageAccess struct {
	// Pages is the set of tickable page keys this person keeps.
	Pages map[string]struct{}
	// Modules is the set of module keys they carry at least one web capability on. Page
	// contracts and route labels for routes owned by a module NOT in this set are dropped
	// entirely, which is the deep-link half of the narrowing: a typed URL must fail closed
	// rather than render a shell the person is not meant to see.
	Modules map[string]struct{}
}

// Allows reports whether a compiled nav leaf or page contract route survives.
func (a PageAccess) Allows(pageKey, href string) bool {
	if pageKey != "" {
		if _, tickable := modulePageIndex[pageKey]; tickable {
			_, ok := a.Pages[pageKey]
			return ok
		}
	}
	module, owned := ModuleOwningRoute(href)
	if !owned {
		// An unowned route is kept. Dropping it would make a new page invisible to
		// EVERYONE the moment it shipped, which is the failure mode this model must never
		// have; the catalog test is what stops a route staying unowned.
		return true
	}
	if _, held := a.Modules[module]; !held {
		return false
	}
	// Inside a held module, a route that IS a tickable page is subject to its own tick --
	// this is what withholds /feed/config from someone who holds Feed. A route that is not
	// a tickable page (a drilldown) rides with the module.
	for _, p := range pagesByModule[module] {
		if p.Href == hrefPath(href) {
			_, ok := a.Pages[p.Key]
			return ok
		}
	}
	return true
}

func hrefPath(href string) string {
	path := strings.TrimSpace(href)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	return path
}

// PageAccessForAssignments resolves a person's stored rows into their page access.
//
// Only WEB rows contribute (property 1 above). A module row carrying no capability grants
// nothing -- it is the stored form of "deliberately removed". A row with an empty Pages
// list grants every page of its module (property 2).
func PageAccessForAssignments(assignments []ModuleAssignment) PageAccess {
	access := PageAccess{
		Pages:   make(map[string]struct{}, len(modulePages)),
		Modules: make(map[string]struct{}, len(moduleCapabilities)),
	}
	// A screen is openable from the person's WHOLE permission set, not from the module it is
	// grouped under. The two are genuinely different: Feed SOP is grouped under Feed and
	// needs sop.read, which lives in the Protocols & SOPs module. Checking only the owning
	// module hid Feed SOP from the CEO, who plainly holds sop.read -- the module is where a
	// screen is TICKED, not where its authority comes from.
	granted := make(map[string]struct{}, 48)
	for _, p := range PermissionsForAssignments(assignments) {
		granted[p] = struct{}{}
	}
	for _, a := range assignments {
		if a.Surface != SurfaceWeb {
			continue
		}
		if !ModuleSupportsSurface(a.Module, SurfaceWeb) {
			continue
		}
		held := false
		for _, level := range a.Capabilities {
			if level != "" && level != LevelNone && LevelOffered(a.Module, level) {
				held = true
				break
			}
		}
		if !held {
			continue
		}
		access.Modules[a.Module] = struct{}{}
		if len(a.Pages) == 0 {
			for _, p := range pagesByModule[a.Module] {
				if pageIsOpenable(p, granted) {
					access.Pages[p.Key] = struct{}{}
				}
			}
			continue
		}
		for _, key := range a.Pages {
			p, known := modulePageIndex[key]
			if !known || p.Module != a.Module {
				// A page key from another module, or a stale one, grants nothing. Failing
				// closed here keeps a typo from widening access silently.
				continue
			}
			if !pageIsOpenable(p, granted) {
				continue
			}
			access.Pages[key] = struct{}{}
		}
	}
	return access
}

// permissionsForModuleLevels is the permission set a person's capabilities on ONE module
// produce. It reuses the same catalog the request path resolves through, so the screen the
// editor offers and the screen the sidebar shows can never disagree.
func permissionsForModuleLevels(moduleKey string, levels []string) map[string]struct{} {
	out := make(map[string]struct{}, 16)
	mod, ok := moduleCapabilityIndex[moduleKey]
	if !ok {
		return out
	}
	for _, level := range levels {
		for _, perm := range mod.Levels[level] {
			out[perm] = struct{}{}
		}
	}
	return out
}

// pageIsOpenable reports whether a permission set opens a screen. The page's permissions
// are ANDed, matching how the route layer and the navigation gate both read them.
func pageIsOpenable(page ModulePage, granted map[string]struct{}) bool {
	for _, required := range page.Permissions {
		if _, ok := granted[required]; !ok {
			return false
		}
	}
	return true
}

// OpenablePagesForModule is the module's screens that these capabilities can actually open,
// in sidebar order. A module held at a level too low for any of its screens returns NONE,
// which is a real answer: Health at `view` opens no Health Config, and offering the tick
// would produce a row that renders and cannot be used.
func OpenablePagesForModule(moduleKey string, levels []string) []ModulePage {
	return OpenablePagesForModuleWithHeld(moduleKey, levels, nil)
}

// OpenablePagesForModuleWithHeld is the same question asked with the person's OTHER modules
// in hand. `held` carries permissions they hold elsewhere -- sop.read from Protocols & SOPs
// is the case that matters, since every module's SOP screen depends on it.
func OpenablePagesForModuleWithHeld(moduleKey string, levels []string, held []string) []ModulePage {
	granted := permissionsForModuleLevels(moduleKey, levels)
	for _, p := range held {
		granted[p] = struct{}{}
	}
	src := pagesByModule[moduleKey]
	out := make([]ModulePage, 0, len(src))
	for _, p := range src {
		if pageIsOpenable(p, granted) {
			out = append(out, p)
		}
	}
	return out
}

// PageKeysForModule is the "all pages" list used when a module is granted without
// narrowing, sorted so stored rows compare and diff cleanly.
func PageKeysForModule(moduleKey string) []string {
	src := pagesByModule[moduleKey]
	out := make([]string, 0, len(src))
	for _, p := range src {
		out = append(out, p.Key)
	}
	sort.Strings(out)
	return out
}
