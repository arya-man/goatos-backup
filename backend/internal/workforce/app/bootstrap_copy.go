package app

import (
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Bootstrap copy is backend-owned presentation text for /app/bootstrap. Android renders
// these values from the payload; Android strings.xml owns only client-static text.

// moduleNavContribution declares the nav items that a module contributes.
// shared_key allows items to be deduped across modules (e.g., "calendar" is shared
// by Vaccination, Feed Direction, and future modules).
type moduleNavContribution struct {
	key       string // e.g., "vaccination", "overview", "calendar"
	labelKey  string // i18n key in bootstrapLabels
	href      string
	shared_key string // "" if not shared; if set, dedupe by this key across modules
	priority  int    // lower = earlier in nav; shared items use the first module's priority
}

// moduleNavRegistry maps module IDs to their nav contributions.
// Each module declares which nav items it owns or contributes to shared screens.
// New modules should register here rather than hardcode nav templates.
// This IS the source of truth for navigation composition; routes here are intentional
// registry definitions, not hardcoded per-role templates.
var moduleNavRegistry = map[string][]moduleNavContribution{ //nav-composition:ignore: this is the module registry, not a hardcoded per-role template
	// "vaccination" is the Preventive Care (PC) Vaccination module.
	"vaccination": {
		{key: "vaccination", labelKey: "nav.drives", href: "/vaccination", shared_key: "", priority: 1}, //nav-composition:ignore: registry entry
		{key: "calendar", labelKey: "nav.calendar", href: "/calendar", shared_key: "calendar", priority: 10},
		{key: "alerts", labelKey: "nav.alerts", href: "/alerts", shared_key: "alerts", priority: 20},
	},
	// Leadership principals (overview/overdue management).
	// This is a synthetic "module" representing the leadership nav state.
	// When a person has >=1 leadership grant, they get overview + calendar + alerts
	// (the shared cross-module nav) instead of the module-specific nav.
	"leadership": {
		{key: "leadership", labelKey: "nav.leadership", href: "/leadership", shared_key: "", priority: 0}, //nav-composition:ignore: registry entry
		{key: "calendar", labelKey: "nav.calendar", href: "/calendar", shared_key: "calendar", priority: 10},
		{key: "alerts", labelKey: "nav.alerts", href: "/alerts", shared_key: "alerts", priority: 20},
	},
}

// visibleNavigationFor composes navigation from the person's granted modules.
// If the person has any leadership grant, they see the leadership nav.
// Otherwise, they see the union of their granted modules' nav contributions,
// deduped by shared_key and ordered by priority.
func visibleNavigationFor(grants []domain.GrantSummary, localeTag string) []domain.BootstrapNavigationItem {
	// Leadership principals get the fixed leadership nav (Overview + Calendar + Alerts)
	if isLeadershipPrincipal(grants) {
		return composeNavigationFromModules([]string{"leadership"}, localeTag)
	}

	// Non-leadership operators get nav composed from their granted modules.
	// For now, all non-leadership grants get "vaccination" module access.
	// As more modules ship, this will be based on actual module grants
	// from department_module_grants (when that table is populated).
	grantedModules := []string{"vaccination"}

	return composeNavigationFromModules(grantedModules, localeTag)
}

// composeNavigationFromModules unions nav items from the given modules,
// deduping by shared_key and ordering by priority.
func composeNavigationFromModules(modules []string, localeTag string) []domain.BootstrapNavigationItem {
	// Collect all contributions, tracking which shared_key we've seen
	collected := make([]moduleNavContribution, 0)
	seenSharedKey := make(map[string]bool)
	seenKey := make(map[string]bool)

	for _, mod := range modules {
		contributions := moduleNavRegistry[mod]
		for _, contrib := range contributions {
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
		"nav.leadership":     "Overview",
		"nav.calendar":       "Calendar",
		"nav.alerts":         "Alerts",
		"nav.drives":         "Drives",
		"queue.assigned":     "Assigned work",
		"queue.shifting":     "Shifting",
		"queue.proof_review": "Proof review",
	},
	"hi": {
		"nav.leadership":     "अवलोकन",
		"nav.calendar":       "कैलेंडर",
		"nav.alerts":         "अलर्ट",
		"nav.drives":         "ड्राइव",
		"queue.assigned":     "सौंपा गया काम",
		"queue.shifting":     "शिफ्टिंग",
		"queue.proof_review": "प्रूफ समीक्षा",
	},
	"kn": {
		"nav.leadership":     "ಅವಲೋಕನ",
		"nav.calendar":       "ಕ್ಯಾಲೆಂಡರ್",
		"nav.alerts":         "ಎಚ್ಚರಿಕೆಗಳು",
		"nav.drives":         "ಡ್ರೈವ್‌ಗಳು",
		"queue.assigned":     "ನಿಯೋಜಿಸಿದ ಕೆಲಸ",
		"queue.shifting":     "ಸ್ಥಳಾಂತರ",
		"queue.proof_review": "ಪುರಾವೆ ಪರಿಶೀಲನೆ",
	},
	"te": {
		"nav.leadership":     "అవలోకనం",
		"nav.calendar":       "క్యాలెండర్",
		"nav.alerts":         "అలర్ట్లు",
		"nav.drives":         "డ్రైవ్‌లు",
		"queue.assigned":     "కేటాయించిన పని",
		"queue.shifting":     "షిఫ్టింగ్",
		"queue.proof_review": "ప్రూఫ్ సమీక్ష",
	},
}
