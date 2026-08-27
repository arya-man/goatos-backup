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
//  1. PAGES ARE WEB-ONLY. The phone composes its own navigation from the module registry
//     and is deliberately untouched by this file (maintainer instruction 2026-08-27:
//     "operator and phone nothing should change"). A page tick narrows the admin-web
//     sidebar and page contracts; it never reaches Android.
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
}

// modulePages is the catalog, in sidebar order. Command lenses (Control Tower, Action
// Center, Protocol Adherence, Workflows) belong to `vaccination`: every one of them
// describes vaccination process gaps today, and giving them a module of their own would
// have created a module nobody's backfilled role holds.
var modulePages = []ModulePage{
	{Key: "control-tower", Module: "vaccination", Label: "Control Tower", Href: "/"},
	{Key: "action-center", Module: "vaccination", Label: "Action Center", Href: "/action-center"},
	{Key: "protocol-adherence", Module: "vaccination", Label: "Protocol Adherence", Href: "/protocol-adherence"},
	{Key: "workflows", Module: "vaccination", Label: "Workflows", Href: "/workflows"},
	{Key: "calendar", Module: "calendar", Label: "Calendar", Href: "/calendar"},
	{Key: "approvals", Module: "counts", Label: "Approvals", Href: "/approvals"},
	{Key: "verification-actions", Module: "verification", Label: "Verify", Href: "/verify"},

	{Key: "preventive-care-vaccination", Module: "vaccination", Label: "Vaccination", Href: "/vaccination"},
	{Key: "vaccination-live-tracker", Module: "vaccination", Label: "Live Drive Tracker", Href: "/vaccination/live-tracker"},
	{Key: "vaccination-plan", Module: "vaccination", Label: "Vaccination plan", Href: "/vaccination/plan"},

	{Key: "procurement-source-entry", Module: "procurement", Label: "Source Entry", Href: "/procurement/source-entry"},
	{Key: "procurement-vendors", Module: "vendors", Label: "Vendors", Href: "/procurement/vendors"},
	{Key: "procurement-sales", Module: "sales", Label: "Sales", Href: "/procurement/sales"},
	{Key: "procurement-feed-purchases", Module: "feed_purchases", Label: "Feed Purchases", Href: "/procurement/feed-purchases"},

	{Key: "counts-herd-analytics", Module: "counts", Label: "Herd Analytics", Href: "/counts/analytics"},
	{Key: "counts-breakdown", Module: "counts", Label: "Counts Breakdown", Href: "/counts/breakdown"},
	{Key: "counts-sops", Module: "counts", Label: "Herd Operations SOP", Href: "/counts/sops"},
	{Key: "milk-preparation", Module: "counts", Label: "Milk Preparation", Href: "/counts/milk-preparation"},
	{Key: "milk-sops", Module: "counts", Label: "Milk SOP", Href: "/milk/sops"},

	{Key: "herd-signals", Module: "herd_signals", Label: "Live Monitor", Href: "/herd-signals"},

	{Key: "weighing-weights", Module: "weighing", Label: "Weights", Href: "/weighing/weights"},
	{Key: "weighing-sops", Module: "weighing", Label: "Weighing SOP", Href: "/weighing/sops"},

	{Key: "feed-config", Module: "feed_direction", Label: "Feed Config", Href: "/feed/config"},
	{Key: "feed-analytics", Module: "feed_direction", Label: "Feed Analytics", Href: "/feed/analytics"},
	{Key: "feed-sops", Module: "feed_direction", Label: "Feed SOP", Href: "/feed/sops"},

	{Key: "health-config", Module: "aas_health", Label: "Health Config", Href: "/health/config"},

	{Key: "audit-log", Module: "operations", Label: "Audit Log", Href: "/operations/audit"},
	{Key: "dlq-center", Module: "operations", Label: "DLQ Center", Href: "/operations/dlq"},
	{Key: "people", Module: "people", Label: "People / HRMS", Href: "/people"},
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
	"/procurement/sales":          "sales",
	"/procurement/feed-purchases": "feed_purchases",
	"/counts":                     "counts",
	"/counts/herd":                "herd_register",
	"/milk":                       "counts",
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
				access.Pages[p.Key] = struct{}{}
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
			access.Pages[key] = struct{}{}
		}
	}
	return access
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
