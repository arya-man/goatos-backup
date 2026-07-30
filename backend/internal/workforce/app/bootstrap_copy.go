package app

import (
	"sort"

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
	key           string // stable module id, also the department_module_grants.module_key
	labelKey      string // i18n key in bootstrapLabels for the drawer row
	landingHref   string // route opened when the drawer row is tapped
	status        string // moduleStatusAvailable | moduleStatusSoon
	priority      int    // drawer ordering; lower first
	contributions []moduleNavContribution
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
		key:         "vaccination",
		labelKey:    "module.vaccination",
		landingHref: "/vaccination", //nav-composition:ignore: registry entry
		status:      moduleStatusAvailable,
		priority:    1,
		contributions: []moduleNavContribution{
			{key: "overview", labelKey: "nav.overview", href: "/vaccination", shared_key: "", priority: 1, requiredPermission: permissions.VaccinationOverviewRead}, //nav-composition:ignore: registry entry
			{key: "vaccination", labelKey: "nav.drives", href: "/vaccination", shared_key: "", priority: 1, excludedPermission: permissions.CalendarAction},         //nav-composition:ignore: registry entry
			{key: "calendar", labelKey: "nav.calendar", href: "/calendar", shared_key: "calendar", priority: 2, requiredPermission: permissions.CalendarAction},     //nav-composition:ignore: registry entry
			{key: "videos", labelKey: "nav.videos", href: "/verify/action", shared_key: "", priority: 3, requiredPermission: permissions.VerificationAct},           //nav-composition:ignore: registry entry
			{key: "alerts", labelKey: "nav.alerts", href: "/alerts", shared_key: "alerts", priority: 20},
			{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100},
		},
	},
	// "weighing" is its own Preventive Care vertical. It is listed beside
	// Vaccination in the module drawer; its bottom bar is only weighing-owned
	// destinations, not a Vaccination tab.
	"weighing": {
		key:         "weighing",
		labelKey:    "module.weighing",
		landingHref: "/weighing", //nav-composition:ignore: registry entry
		status:      moduleStatusAvailable,
		priority:    2,
		contributions: []moduleNavContribution{
			{key: "weighing", labelKey: "nav.weighing", href: "/weighing", shared_key: "", priority: 1, requiredAnyPermission: []string{permissions.WeighingPlan, permissions.WeighingMonitor, permissions.WeighingExecute}}, //nav-composition:ignore: registry entry
			{key: "videos", labelKey: "nav.videos", href: "/weighing/videos", shared_key: "", priority: 2, requiredPermission: permissions.WeighingMonitor},                                                                  //nav-composition:ignore: registry entry
			{key: "alerts", labelKey: "nav.alerts", href: "/alerts", shared_key: "alerts", priority: 20},
			{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100},
		},
	},
	// "counts" is the Counts vertical: herd census plus the field events that move it.
	// Birth and death are goat-lifecycle writes that feed the herd-register projection;
	// shifting records an animal movement between sheds/parks.
	"counts": {
		key:         "counts",
		labelKey:    "module.counts",
		landingHref: "/counts", //nav-composition:ignore: registry entry
		status:      moduleStatusAvailable,
		priority:    2,
		contributions: []moduleNavContribution{
			// The census page is Admin/CEO-only: field capture and tenant-wide population
			// visibility are different authorities (maintainer decision 2026-07-18).
			{key: "counts", labelKey: "nav.counts", href: "/counts", shared_key: "", priority: 1, requiredPermission: permissions.CountsRead}, //nav-composition:ignore: registry entry
			// The two capture pages follow CountsWrite, so an Operator or Park Head gets a
			// two-tab Counts module while Admin/CEO get all three.
			{key: "birth_death", labelKey: "nav.birth_death", href: "/counts/birth-death", shared_key: "", priority: 2, requiredPermission: permissions.CountsWrite}, //nav-composition:ignore: registry entry
			{key: "shifting", labelKey: "nav.shifting", href: "/counts/shifting", shared_key: "", priority: 3, requiredPermission: permissions.CountsWrite},          //nav-composition:ignore: registry entry
			// Counts takes the trailing bar slot for the APPROVER's queue instead of the global
			// You tab (maintainer decision 2026-07-19). The Counts module is where lifecycle
			// requests are raised, so it is where they are decided; an operator holding no
			// approval permission simply does not receive this item and gets a two-tab module.
			// CountsApproveAccess is the coarse surface gate -- WHICH request types the caller
			// may actually decide is resolved server-side per row
			// (permissions.DecidableApprovalRequestTypes), never re-derived on the phone.
			{key: "approval", labelKey: "nav.approval", href: "/counts/approvals", shared_key: "", priority: 4, requiredPermission: permissions.CountsApproveAccess}, //nav-composition:ignore: registry entry
		},
	},
	// Declared-but-unbuilt modules. They render as disabled "Soon" drawer rows so the
	// client no longer needs its own hardcoded coming-soon list.
	"feed_direction": {
		key:         "feed_direction",
		labelKey:    "module.feed_direction",
		landingHref: "",
		status:      moduleStatusSoon,
		priority:    3,
	},
	"breeding": {
		key:         "breeding",
		labelKey:    "module.breeding",
		landingHref: "",
		status:      moduleStatusSoon,
		priority:    4,
	},
	// The cross-vertical verifier app is intentionally standalone. Verifiers review
	// evidence; they never inherit operator capture or leadership action navigation.
	"verification": {
		key:         "verification",
		labelKey:    "module.verification",
		landingHref: "/verify", //nav-composition:ignore: registry entry
		status:      moduleStatusAvailable,
		priority:    0,
		contributions: []moduleNavContribution{
			{key: "verify", labelKey: "nav.verify", href: "/verify", shared_key: "", priority: 0, requiredPermission: permissions.VerificationReview}, //nav-composition:ignore: registry entry
			{key: "you", labelKey: "nav.you", href: "/you", shared_key: "you", priority: 100},                                                         //nav-composition:ignore: registry entry
		},
	},
}

// soonModuleKeys are surfaced to every principal as disabled drawer rows regardless of
// grants; they advertise the roadmap, they do not confer access.
var soonModuleKeys = []string{"feed_direction", "breeding"}

// visibleNavigationFor composes navigation from the person's granted modules.
// If the person has any leadership grant, they see the shared module set for that tier.
// Otherwise, they see the union of their granted modules' nav contributions,
// deduped by shared_key and ordered by priority.
func visibleNavigationFor(grants []domain.GrantSummary, grantedModules []string, localeTag string) []domain.BootstrapNavigationItem {
	// A standalone verifier sees only the generic media-verification module.
	// Leadership principals may also hold review permission, but they still land
	// in their leadership module rather than the verifier-only app.
	if isStandaloneVerifierPrincipal(grants) {
		return composeNavigationFromModules([]string{"verification"}, grants, localeTag)
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
	out := make([]moduleNavContribution, 0, len(def.contributions))
	for _, contrib := range def.contributions {
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
func candidateModuleKeys(grants []domain.GrantSummary, grantedModules []string) []string {
	if isStandaloneVerifierPrincipal(grants) {
		return []string{"verification"}
	}
	if !isLeadershipPrincipal(grants) {
		return grantedModules
	}
	return leadershipModuleKeys(grants)
}

// leadershipModuleKeys is the curated drawer set for a leadership principal, before
// permission filtering. Leadership is org-level (not department-scoped), so the set
// is decided by leadership TIER, not by department_module_grants:
//
//   - CEO/CXO (ceo_internal) is whole-org: Vaccination plus Counts, plus the
//     roadmap "soon" modules (Feed direction, Breeding).
//   - Preventive-Care leadership (PC Director, Park Head) is specialty-scoped to
//     preventive care: ONLY the shared Vaccination module. Counts, Feed, and Breeding
//     are not preventive-care surfaces, so they never appear. Park Head is further
//     limited to his own park by his grant scope (data scope), not by nav.
//
// Verification belongs to the verifier role, not leadership nav.
func leadershipModuleKeys(grants []domain.GrantSummary) []string {
	if hasRole(grants, permissions.RoleCEOInternal) {
		return []string{"vaccination", "weighing", "counts", "feed_direction", "breeding"}
	}
	// PC Director / Park Head: preventive-care specialty verticals.
	return []string{"vaccination", "weighing"}
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
	return hasRole(grants, permissions.RoleCEOInternal) || hasRole(grants, permissions.RolePCDirector)
}

func canExecuteVaccination(grants []domain.GrantSummary) bool {
	return hasPermission(grants, permissions.TaskExecute)
}

func canExecuteWeighing(grants []domain.GrantSummary) bool {
	return hasPermission(grants, permissions.WeighingExecute)
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

// modulesFor builds the drawer: every module the principal can render (with its own
// permission-filtered, module-scoped bar), followed by the declared "soon" modules as
// disabled rows. A module the registry does not know, or whose every page is gated away
// from this principal, contributes nothing.
func modulesFor(grants []domain.GrantSummary, grantedModules []string, localeTag string) []domain.BootstrapModule {
	keys := candidateModuleKeys(grants, grantedModules)

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
	sort.SliceStable(available, func(i, j int) bool { return available[i].priority < available[j].priority })

	out := make([]domain.BootstrapModule, 0, len(available)+len(soonModuleKeys))
	for _, def := range available {
		items := composeNavigationFromModules([]string{def.key}, grants, localeTag)
		// Land on the first page this principal may actually open. The declared
		// landingHref can be gated away (an Operator holds Counts but not the census
		// page at /counts), and landing them on a route that 403s would be a
		// self-inflicted dead end.
		href := def.landingHref
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
		"nav.verify":      "Verify",
		"nav.overview":    "Overview",
		"nav.calendar":    "Calendar",
		"nav.alerts":      "Alerts",
		"nav.drives":      "Drives",
		"nav.counts":      "Counts",
		"nav.birth_death": "Birth/Death",
		"nav.shifting":    "Shifting",
		"nav.approval":    "Approval",
		"nav.weighing":    "Weighing",
		"nav.videos":      "Videos",
		"nav.you":         "You",

		"module.verification":   "Verification",
		"module.vaccination":    "Vaccination",
		"module.weighing":       "Weighing",
		"module.counts":         "Counts",
		"module.feed_direction": "Feed direction",
		"module.breeding":       "Breeding",
		"queue.assigned":        "Assigned work",
		"queue.shifting":        "Shifting",
		"queue.proof_review":    "Proof review",
	},
	"hi": {
		"nav.verify":      "सत्यापित करें",
		"nav.overview":    "अवलोकन",
		"nav.calendar":    "कैलेंडर",
		"nav.alerts":      "अलर्ट",
		"nav.drives":      "ड्राइव",
		"nav.counts":      "गिनती",
		"nav.birth_death": "जन्म/मृत्यु",
		"nav.shifting":    "शिफ्टिंग",
		"nav.approval":    "अनुमोदन",
		"nav.weighing":    "वजन",
		"nav.videos":      "वीडियो",
		"nav.you":         "आप",

		"module.verification":   "सत्यापन",
		"module.vaccination":    "टीकाकरण",
		"module.weighing":       "वजन",
		"module.counts":         "गिनती",
		"module.feed_direction": "फ़ीड दिशा",
		"module.breeding":       "प्रजनन",
		"queue.assigned":        "सौंपा गया काम",
		"queue.shifting":        "शिफ्टिंग",
		"queue.proof_review":    "प्रूफ समीक्षा",
	},
	"kn": {
		"nav.verify":      "ಪರಿಶೀಲಿಸಿ",
		"nav.overview":    "ಅವಲೋಕನ",
		"nav.calendar":    "ಕ್ಯಾಲೆಂಡರ್",
		"nav.alerts":      "ಎಚ್ಚರಿಕೆಗಳು",
		"nav.drives":      "ಡ್ರೈವ್‌ಗಳು",
		"nav.counts":      "ಎಣಿಕೆ",
		"nav.birth_death": "ಜನನ/ಮರಣ",
		"nav.shifting":    "ಸ್ಥಳಾಂತರ",
		"nav.approval":    "ಅನುಮೋದನೆ",
		"nav.weighing":    "ತೂಕ",
		"nav.videos":      "ವೀಡಿಯೊಗಳು",
		"nav.you":         "ನೀವು",

		"module.verification":   "ಪರಿಶೀಲನೆ",
		"module.vaccination":    "ಲಸಿಕೆ",
		"module.weighing":       "ತೂಕ",
		"module.counts":         "ಎಣಿಕೆ",
		"module.feed_direction": "ಆಹಾರ ನಿರ್ದೇಶನ",
		"module.breeding":       "ಸಂತಾನೋತ್ಪತ್ತಿ",
		"queue.assigned":        "ನಿಯೋಜಿಸಿದ ಕೆಲಸ",
		"queue.shifting":        "ಸ್ಥಳಾಂತರ",
		"queue.proof_review":    "ಪುರಾವೆ ಪರಿಶೀಲನೆ",
	},
	"te": {
		"nav.verify":      "ధృవీకరించండి",
		"nav.overview":    "అవలోకనం",
		"nav.calendar":    "క్యాలెండర్",
		"nav.alerts":      "అలర్ట్లు",
		"nav.drives":      "డ్రైవ్‌లు",
		"nav.counts":      "లెక్కలు",
		"nav.birth_death": "జననం/మరణం",
		"nav.shifting":    "షిఫ్టింగ్",
		"nav.approval":    "ఆమోదం",
		"nav.weighing":    "బరువు",
		"nav.videos":      "వీడియోలు",
		"nav.you":         "మీరు",

		"module.verification":   "ధృవీకరణ",
		"module.vaccination":    "టీకా",
		"module.weighing":       "బరువు",
		"module.counts":         "లెక్కలు",
		"module.feed_direction": "ఫీడ్ దిశ",
		"module.breeding":       "సంతానోత్పత్తి",
		"queue.assigned":        "కేటాయించిన పని",
		"queue.shifting":        "షిఫ్టింగ్",
		"queue.proof_review":    "ప్రూఫ్ సమీక్ష",
	},
}
