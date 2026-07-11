package app

import (
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// Bootstrap copy is backend-owned presentation text for /app/bootstrap. Android renders
// these values from the payload; Android strings.xml owns only client-static text.

type navigationTemplate struct {
	key      string
	labelKey string
	href     string
}

// leadershipNavigation is the fixed backend-owned mobile nav for a leadership
// principal. The client adds a "You" tab locally; the backend owns exactly
// these three so Overview/Overdue/Reschedule stay reachable.
var leadershipNavigation = []navigationTemplate{
	{key: "leadership", labelKey: "nav.leadership", href: "/leadership"},
	{key: "calendar", labelKey: "nav.calendar", href: "/calendar"},
	{key: "alerts", labelKey: "nav.alerts", href: "/alerts"},
}

// operatorNavigation is the fixed backend-owned mobile nav for a field operator:
// Drives (the shed execution flow) first, then Calendar, then Alerts. The client
// adds "You" locally and lands on Calendar.
// There is no Overview/Home for operators — they execute, they don't oversee.
var operatorNavigation = []navigationTemplate{
	{key: "vaccination", labelKey: "nav.drives", href: "/vaccination"},
	{key: "calendar", labelKey: "nav.calendar", href: "/calendar"},
	{key: "alerts", labelKey: "nav.alerts", href: "/alerts"},
}

func visibleNavigationFor(grants []domain.GrantSummary, localeTag string) []domain.BootstrapNavigationItem {
	if isLeadershipPrincipal(grants) {
		return navigationFor(leadershipNavigation, localeTag)
	}
	return navigationFor(operatorNavigation, localeTag)
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

func navigationFor(items []navigationTemplate, localeTag string) []domain.BootstrapNavigationItem {
	out := make([]domain.BootstrapNavigationItem, 0, len(items))
	for _, item := range items {
		out = append(out, domain.BootstrapNavigationItem{
			Key:   item.key,
			Label: localizedBootstrapLabel(localeTag, item.labelKey),
			Href:  item.href,
		})
	}
	return out
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
