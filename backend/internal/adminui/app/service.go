// Package app builds the admin-web UI contract.
package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

type Service struct {
	repo ReferenceRepository
	// verificationModules sources the verifier-only workspace's evidence modules from the generic
	// Verification type registry. Injected via WithVerificationModules; see verifier_lens.go.
	verificationModules VerificationModuleSource
	// moduleDutyReader filters the verifier's modules by assigned duties. Optional; when nil or
	// erroring, the lens fails SAFE by showing no modules. Injected via WithModuleDutyReader.
	moduleDutyReader ModuleDutyReader
	// personPageAccess narrows the compiled contract to the pages a person is ticked for
	// (per-person access, maintainer decision 2026-08-27). Optional; see person_page_lens.go.
	personPageAccess PersonPageAccessSource
	mu               sync.Mutex
	cache            map[string]cacheEntry
	cacheTTL         time.Duration
	cacheMaxEntries  int
	now              func() time.Time
}

func NewService(repo ...ReferenceRepository) *Service {
	var r ReferenceRepository
	if len(repo) > 0 {
		r = repo[0]
	}
	return &Service{
		repo:            r,
		cache:           map[string]cacheEntry{},
		cacheTTL:        defaultContractCacheTTL,
		cacheMaxEntries: defaultContractCacheMaxEntries,
		now:             time.Now,
	}
}

func (s *Service) Bootstrap(ctx context.Context, input BootstrapInput) domain.BootstrapResponse {
	return s.bootstrapCached(ctx, input)
}

func baseBootstrap() domain.BootstrapResponse {
	return domain.BootstrapResponse{
		Source:           "api",
		SchemaVersion:    "admin-web-ui-v1",
		ContractRevision: "",
		FamilyHashes:     map[string]string{},
		CachePolicy: domain.ContractCachePolicy{
			ETag:            "",
			InProcessTTLSec: int(defaultContractCacheTTL.Seconds()),
			RedisTTLHintSec: redisTTLHintSeconds,
			RevisionSource:  "tenant-role-family-hashes",
		},
		Navigation:   navigation(),
		NavChrome:    domain.NavChromeExpanded,
		RouteLabels:  routeLabels(),
		TopBar:       topBar(),
		RoleLenses:   roleLenses(),
		Pages:        pages(),
		Copy:         chromeCopy(),
		DisplayRules: displayRules(),
	}
}

func navItem(id, label, href, icon, badgeKey string) domain.NavigationItem {
	return domain.NavigationItem{ID: id, Label: label, Href: href, Icon: icon, BadgeKey: badgeKey, Enabled: true, Extra: map[string]string{}}
}

func navItemDomain(id, label, href, icon, badgeKey, module string) domain.NavigationItem {
	item := navItem(id, label, href, icon, badgeKey)
	item.Domain = module
	return item
}

func navLeaf(id, label, href string, extra map[string]string) domain.NavigationItem {
	if extra == nil {
		extra = map[string]string{}
	}
	return domain.NavigationItem{ID: id, Label: label, Href: href, Enabled: true, Extra: extra}
}

// navLeafDomain stamps a leaf with its product module for analytics and stable
// backend contract metadata. It does not filter navigation.
func navLeafDomain(id, label, href, module string, extra map[string]string) domain.NavigationItem {
	item := navLeaf(id, label, href, extra)
	item.Domain = module
	return item
}

func navigation() domain.NavigationContract {
	return domain.NavigationContract{
		Primary: []domain.NavigationItem{
			// Calendar is parked from the sidebar (maintainer request 2026-09-09, together with
			// the four command lenses and DLQ Center below). The page stays served at its route
			// for deep links; keep the restore line so scope guards can tell it is withheld
			// deliberately, not lost. Tasks was parked the same day and RESTORED on 2026-09-11
			// (maintainer request: raise tasks from the web desk too) -- see below Verify.
			// navItem("calendar", "Calendar", "/calendar", "calendar-days", ""),
			// Approvals is a top-level decision surface (maintainer decision 2026-07-21): the queue of
			// pending birth/death/shifting requests, approved or rejected here. Moved off mobile;
			// access is gated server-side by counts.approve_access (the four org tiers + admin +
			// ceo_internal). The nav contract is static — like /verification and /config, an
			// unauthorized caller's decision/list calls fail closed at the route.
			navItem("approvals", "Approvals", "/approvals", "gavel", "approvals_open_queue"),
			// Verify is a peer decision surface directly below Approvals and before the
			// vertical/module groups. It remains independently gated by verification.review.
			//
			// Named "Verify" (maintainer decision 2026-08-12), which is what the PHONE has always
			// called it (workforce nav.verify) — one word for one job across both surfaces. It was
			// "Actions", the vaguest possible label for a screen that does exactly one thing: open a
			// proof video, check it against the facts, accept or reject.
			navItemDomain("verification-actions", "Verify", "/verify", "clipboard-check", "", "admin.verification"),
			// Tasks: leadership asks raised across CXOs, directors and park heads. Restored to
			// the sidebar 2026-09-11; the web desk raises as well as monitors.
			navItem("leadership-tasks", "Tasks", "/tasks", "clipboard-list", ""),
			// Work Board (maintainer decision 2026-09-10): the cross-module board of one park's
			// day, every module the person may see. Top-level because it is a lens over every
			// vertical, not a page inside one. Gated on work_board.read.
			navItemDomain("work-board", "Work Board", "/work-board", "square-kanban", "", "work_board"),
			// Alerts (maintainer decision 2026-09-16): directly below the Work Board -- what is
			// OFF today across the parks the person may see (a pen's feed moved with no shifting
			// to explain it, a feed running out), with the rules behind it configurable top-right
			// by the people HRMS ticks. Gated on alerts.read.
			navItemDomain("alerts", "Alerts", "/alerts", "bell-ring", "", "alerts"),
			// Routines (maintainer instruction 2026-09-16): the configurable recurring pen
			// checks -- the rule the CXO writes per park and the Today table of what it
			// raised. Beside Tasks and Work Board because it is the same shape of thing: work
			// the system owes a person. Gated on pen_routines.read.
			navItemDomain("pen-routines", "Routines", "/routines", "list-checks", "", "pen_routines"),
		},
		Groups: []domain.NavigationGroup{
			{
				ID: "counts", Label: "Counts", Icon: "bar-chart-3", DefaultOpen: false,
				Leaves: []domain.NavigationItem{
					navLeaf("counts-herd-analytics", "Herd Analytics", "/counts/analytics", nil),
					navLeaf("counts-breakdown", "Counts Breakdown", "/counts/breakdown", nil),
					// Mortality (maintainer request 2026-09-18): deaths from every angle the farm
					// asks -- stage, kid/adult, age at death, breed, load, cause, season, pen,
					// sex, days since arrival / last vaccination, and the cross tabs between
					// them. Same window calendar and denominator discipline as Herd Analytics.
					navLeaf("counts-mortality", "Mortality", "/counts/mortality", nil),
					navLeaf("counts-sops", "Herd Operations SOP", "/counts/sops", nil),
					// Herd Register is parked from the sidebar for now; keep the restore line so
					// scope guards can tell it is withheld deliberately, not lost.
					// navLeaf("counts-herd", "Herd Register", "/counts/herd", nil),
				},
			},
			{
				ID: "weighing", Label: "Weighing", Icon: "scale", DefaultOpen: false,
				Leaves: []domain.NavigationItem{
					// Weights is parked from the sidebar (maintainer request 2026-09-03: the Weight
					// menu carries ADG Analytics and the SOP only). The page itself stays
					// served at /weighing/weights for deep links; keep the restore line so scope
					// guards can tell it is withheld deliberately, not lost. The load comparison
					// is the Load-wise TAB on ADG Analytics (maintainer request, same day),
					// not a leaf of its own.
					// navLeafDomain("weighing-weights", "Weights", "/weighing/weights", "weighing.weights", nil),
					navLeafDomain("weighing-analytics", "ADG Analytics", "/weighing/analytics", "weighing.analytics", nil),
					navLeaf("weighing-sops", "Weighing SOP", "/weighing/sops", nil),
				},
			},
			{
				ID: "sales", Label: "Sales", Icon: "banknote", DefaultOpen: false,
				Leaves: []domain.NavigationItem{
					// Sold and Farm value (maintainer decision 2026-09-11): the old Sales board
					// carried the closed-sale blocks, the live-herd valuation AND the deals ledger
					// on one screen. It is divided into these two pages and retired; the ledger
					// closes the Sold page, and /sales redirects to /sales/sold.
					navLeaf("sales-sold", "Summary", "/sales/sold", nil),
					navLeaf("sales-farm-value", "Farm value", "/sales/farm-value", nil),
					navLeaf("sales-loads", "Load wise", "/sales/loads", nil),
					// Farm born (maintainer request 2026-09-18): the other half of the herd -- the
					// animals the farm did NOT buy on a load -- on farm today, sold in a window,
					// by breed / sex / stage / pen, and the money. Sits beside Load wise because
					// the two pages partition the herd.
					navLeaf("sales-farm-born", "Farm born", "/sales/farm-born", nil),
					// Market analytics (maintainer decision 2026-09-14): what goat and sheep fetch in
					// the markets phoned each morning, read back over time. Cities and questions are
					// authored on Sales Config; prices are recorded on the phone.
					navLeaf("sales-market-analytics", "Market analytics", "/sales/market-analytics", nil),
					// Buyer analytics (maintainer request 2026-09-15): who the farm sells to, one row
					// per buyer -- name, phone, category, purchases so far and whether they come back.
					navLeaf("sales-buyer-analytics", "Buyer analytics", "/sales/buyer-analytics", nil),
					// The SELLING side of the one vendor register (maintainer decision 2026-09-05).
					// Same table and same endpoint as Procurement > Vendors, narrowed to the record
					// types the farm SELLS to. It is a Sales leaf because the person recording a sale
					// is the one who meets a new butcher, and sending them to the buying desk's page
					// to add him is what this leaf ends.
					navLeaf("sales-vendors", "Vendors", "/sales/vendors", nil),
					navLeaf("sales-config", "Sales Config", "/sales/config", nil),
					// SALES SOP (maintainer instruction 2026-09-19): what happens after a sale is
					// recorded -- tag, load, the money -- and WHO does each step, authored here in
					// the same List | Flow shape as /counts/sops and run by the tasks engine.
					navLeaf("sales-sops", "Sales SOP", "/sales/sops", nil),
				},
			},
			{
				ID: "feed", Label: "Feed", Icon: "wheat", DefaultOpen: false,
				Leaves: []domain.NavigationItem{
					navLeaf("feed-config", "Feed Config", "/feed/config", nil),
					navLeaf("feed-analytics", "Feed Analytics", "/feed/analytics", nil),
					navLeaf("feed-sops", "Feed SOP", "/feed/sops", nil),
				},
			},
			{
				ID: "pc", Label: "Preventive Care", Icon: "heart-pulse", DefaultOpen: false,
				Leaves: []domain.NavigationItem{
					navLeafDomain("preventive-care-vaccination", "Vaccination", "/vaccination", "pc.vaccination", nil),
					navLeafDomain("vaccination-live-tracker", "Live Drive Tracker", "/vaccination/live-tracker", "pc.vaccination", nil),
					navLeafDomain("vaccination-plan", "Vaccination plan", "/vaccination/plan", "pc.vaccination", nil),
					navLeaf("pc-care-sops", "Preventive Care SOP", "/pc-care/sops", nil),
				},
			},
			{
				ID: "procurement", Label: "Procurement", Icon: "truck", DefaultOpen: false,
				Leaves: []domain.NavigationItem{
					navLeaf("procurement-source-entry", "Source Entry", "/procurement/source-entry", nil),
					navLeaf("procurement-vendors", "Vendors", "/procurement/vendors", nil),
					navLeaf("procurement-feed-purchases", "Feed Purchases", "/procurement/feed-purchases", nil),
					navLeaf("procurement-animal-purchases", "Animal purchases", "/procurement/animal-purchases", nil),
					navLeaf("procurement-sops", "Procurement SOP", "/procurement/sops", nil),
				},
			},
			{
				// Health is its own module group (maintainer request 2026-09-08); its two
				// pages used to sit under Others.
				ID: "health", Label: "Health", Icon: "stethoscope", DefaultOpen: false,
				Leaves: []domain.NavigationItem{
					navLeaf("health-analytics", "Health Analytics", "/health/analytics", nil),
					navLeaf("health-config", "Health Config", "/health/config", nil),
				},
			},
			{
				ID: "others", Label: "Others", Icon: "edit-3", DefaultOpen: false,
				Leaves: []domain.NavigationItem{
					// The four command lenses moved here from the primary bar (maintainer request
					// 2026-09-08) and were then parked from the sidebar altogether (maintainer
					// request 2026-09-09). They remain top-level command lenses at their own
					// routes; ADG Analytics is the landing page, so Control Tower keeps an explicit
					// /?lens=control-tower deep link.
					// navLeaf("control-tower", "Control Tower", "/?lens=control-tower", nil),
					// navLeaf("action-center", "Action Center", "/action-center", nil),
					// navLeaf("protocol-adherence", "Protocol Adherence", "/protocol-adherence", nil),
					// navLeaf("workflows", "Workflows", "/workflows", nil),
					navLeaf("milk-preparation", "Milk Preparation", "/counts/milk-preparation", nil),
					navLeaf("milk-sops", "Milk SOP", "/milk/sops", nil),
					navLeafDomain("herd-signals", "Live Monitor", "/herd-signals", "herd_signals.live", nil),
					navLeafDomain("audit-log", "Audit Log", "/operations/audit", "admin.audit", nil),
					// Parked from the sidebar (maintainer request 2026-09-09); /operations/dlq stays served.
					// navLeafDomain("dlq-center", "DLQ Center", "/operations/dlq", "admin.audit", nil),
					navLeafDomain("people", "People / HRMS", "/people", "admin.people", nil),
					// Leave (maintainer decision 2026-09-10): the park head + HR queue for leave
					// raised from the phone Clock screen, the list of every request, and the
					// CEO-only "who approves" flags. Sits beside People / HRMS rather than in the
					// primary bar, whose two decision surfaces (Approvals, Verify) are pinned.
					// Gated server-side by leave.approve (permissionsForNav); the list and the
					// flags are page-contract controls on leave.read / leave.approval.configure.
					navLeafDomain("leave", "Leave", "/leave", "admin.people", nil),
				},
			},
			{
				// Configuration (maintainer instruction 2026-09-18, from the prototype in #297):
				// the farm's reference lists -- places, animal types, catalogues -- edited on
				// screen. Its own group at the foot of the sidebar, the way the prototype places
				// it; Work instructions joins it in a later phase. Gated on configuration.read.
				ID: "configuration", Label: "Configuration", Icon: "settings", DefaultOpen: false,
				Leaves: []domain.NavigationItem{
					navLeafDomain("configuration-items", "Items and settings", "/configuration/items", "configuration", nil),
					// SOP studio phase 2 (maintainer instruction 2026-09-18): GENERAL work instructions
					// -- SOPs tied to no module, started by hand -- are authored here; module-level
					// SOPs stay on their module's SOP page. Gated on sop.read like every SOP page.
					navLeaf("configuration-work-instructions", "Work instructions", "/configuration/work-instructions", nil),
				},
			},
		},
		Footer: "Mesha · goat operating system",
	}
}

func routeLabels() []domain.RouteLabelRule {
	return []domain.RouteLabelRule{
		{Pattern: "/", Label: "Control Tower", Match: "exact"},
		{Pattern: "/action-center", Label: "Action Center", Match: "exact"},
		{Pattern: "/calendar", Label: "Calendar", Match: "exact"},
		{Pattern: "/protocol-adherence", Label: "Protocol Adherence", Match: "exact"},
		{Pattern: "/workflows/{row_id}", Label: "Workflow record", Match: "pattern"},
		{Pattern: "/workflows", Label: "Workflows", Match: "exact"},
		{Pattern: "/approvals", Label: "Approvals", Match: "exact"},
		{Pattern: "/leave", Label: "Leave", Match: "exact"},
		{Pattern: "/tasks", Label: "Tasks", Match: "exact"},
		{Pattern: "/work-board", Label: "Work Board", Match: "exact"},
		{Pattern: "/alerts", Label: "Alerts", Match: "exact"},
		{Pattern: "/routines", Label: "Routines", Match: "exact"},
		{Pattern: "/configuration/items", Label: "Items and settings", Match: "exact"},
		{Pattern: "/verify", Label: "Verify", Match: "exact"},
		{Pattern: "/vaccination/execution/sheds/{shed_id}", Label: "Vaccination execution", Match: "pattern"},
		// Most-specific-first: the live tracker's exact rule must precede /vaccination's, or the
		// crumb resolves to the parent label.
		{Pattern: "/vaccination/live-tracker", Label: "Live Drive Tracker", Match: "exact"},
		// Needed because /vaccination is an EXACT rule: without its own entry the plan
		// console's crumb silently falls back to the parent label, "Vaccination".
		{Pattern: "/vaccination/plan/edit", Label: "Edit the plan", Match: "exact"},
		{Pattern: "/vaccination/plan", Label: "Vaccination plan", Match: "exact"},
		{Pattern: "/vaccination", Label: "Vaccination", Match: "exact"},
		{Pattern: "/procurement/source-entry/loads/{load_id}", Label: "Source load", Match: "pattern"},
		{Pattern: "/procurement/source-entry", Label: "Source Entry", Match: "exact"},
		{Pattern: "/procurement/vendors", Label: "Vendors", Match: "exact"},
		{Pattern: "/procurement/feed-purchases", Label: "Feed Purchases", Match: "exact"},
		{Pattern: "/procurement/animal-purchases", Label: "Animal purchases", Match: "exact"},
		{Pattern: "/procurement/sops", Label: "Procurement SOP", Match: "exact"},
		{Pattern: "/pc-care/sops", Label: "Preventive Care SOP", Match: "exact"},
		{Pattern: "/sales/sold", Label: "Summary", Match: "exact"},
		{Pattern: "/sales/market-analytics", Label: "Market analytics", Match: "exact"},
		{Pattern: "/sales/buyer-analytics", Label: "Buyer analytics", Match: "exact"},
		{Pattern: "/sales/loads", Label: "Load wise", Match: "exact"},
		{Pattern: "/sales/farm-born", Label: "Farm born", Match: "exact"},
		{Pattern: "/sales/farm-value", Label: "Farm value", Match: "exact"},
		{Pattern: "/sales/config", Label: "Sales Config", Match: "exact"},
		{Pattern: "/counts/sops", Label: "Herd Operations SOP", Match: "exact"},
		{Pattern: "/counts/herd", Label: "Herd Register", Match: "exact"},
		{Pattern: "/counts/breakdown", Label: "Counts Breakdown", Match: "exact"},
		{Pattern: "/counts/mortality", Label: "Mortality", Match: "exact"},
		{Pattern: "/counts/milk-preparation", Label: "Milk Preparation", Match: "exact"},
		// Most-specific-first: /feed/direction and /feed/packing are exact leaves; /feed/config is
		// the Feed-owned authority screen (see the navigation() scope note).
		{Pattern: "/feed/direction", Label: "Feed Direction", Match: "exact"},
		{Pattern: "/feed/packing", Label: "Feed Packing", Match: "exact"},
		{Pattern: "/feed/sops", Label: "Feed SOP", Match: "exact"},
		{Pattern: "/feed/config", Label: "Feed Config — Ration Rules", Match: "exact"},
		{Pattern: "/feed/analytics", Label: "Feed Analytics", Match: "exact"},
		{Pattern: "/health/analytics", Label: "Health Analytics", Match: "exact"},
		{Pattern: "/health/config", Label: "Health Config — Treatment Protocols", Match: "exact"},
		{Pattern: "/operations/audit", Label: "Audit Log", Match: "exact"},
		{Pattern: "/operations/dlq", Label: "DLQ Center", Match: "exact"},
		{Pattern: "/config", Label: "Config — Protocol Rules", Match: "exact"},
		{Pattern: "/people", Label: "People / HRMS", Match: "exact"},
		{Pattern: "/goats/{goat_id}", Label: "Goat Passport", Match: "pattern"},
	}
}

func topBar() domain.TopBarContract {
	return domain.TopBarContract{
		ProductName: "Mesha",
		LogoText:    "मे",
		ScopeModeToggle: []domain.TopBarOption{
			{Key: "company", Label: "Company-wide", Title: "Company-wide rollup across all in-scope parks", Enabled: true},
			{Key: "park", Label: "Park-wise", Title: "Park-wise scope", Enabled: true},
		},
		ParkSelector: domain.TopBarControl{
			Label: "Park scope", Enabled: true,
			Hint:    "Pen scope: all pens — per-pen filtering is intentionally not wired in this slice yet.",
			Options: []domain.TopBarOption{},
		},
		DateRangeSelector: domain.TopBarControl{
			Label: "Showing data for", Enabled: true,
			Hint: "Select the Goat OS business date used by the current page. Actions shows verification evidence for that day.",
			Options: []domain.TopBarOption{
				{Key: "last_7_days", Label: "Last 7 days", Enabled: false, DisabledReason: "Backend range filtering is not defined for the current process-integrity slice."},
				{Key: "last_30_days", Label: "Last 30 days", Enabled: false, DisabledReason: "Backend range filtering is not defined for the current process-integrity slice."},
			},
		},
		// The bell is LIVE (in-app notification centre + browser push ride this slot), so the
		// control is enabled and its `label` is the bell's accessible name / tooltip. The
		// disabled_reason is empty: the shell only falls back to it when `enabled` is false. It
		// used to say "Notifications are not wired in this admin-web slice yet." while the bell
		// opened a working panel (gate-1 #5).
		Notifications: domain.TopBarControl{Label: "Notifications", Enabled: true, DisabledReason: "", Options: []domain.TopBarOption{}},
		RolePreview:   domain.RolePreviewActor{DisplayName: "Signed-in CEO/CXO", Initials: "CX", Subtitle: "Role and park scope resolved by backend RBAC"},
	}
}

func roleLenses() []domain.RoleLensContract {
	return []domain.RoleLensContract{
		{ID: "coo", Name: "CEO / CXO", AuditShort: "CXO", Scope: "all · deep", Description: "Central Command · all parks", FullAccess: true},
		{ID: "health-director", Name: "Health Director", AuditShort: "Health Dir", Scope: "health vertical · all parks", Description: "Preventive Care (PC) / health governance view"},
		{ID: "park-head", Name: "Park Head", AuditShort: "Park Head", Scope: "all verticals · assigned park", Description: "Assigned park leadership view"},
		{ID: "health-manager", Name: "Health Manager", AuditShort: "Health Mgr", Scope: "health vertical · assigned park", Description: "Assigned-park Preventive Care (PC) manager view"},
		{ID: "ground", Name: "Assist / Ground", AuditShort: "Assist", Scope: "tasks · assigned park", Description: "field execution queue"},
		{ID: "investor", Name: "Investor", AuditShort: "Investor", Scope: "read-only summary", Description: "summary-only lens"},
	}
}

func chromeCopy() map[string]string {
	return map[string]string{
		"route.unavailable":             "Route unavailable",
		"nav.expand":                    "Expand navigation",
		"nav.collapse":                  "Collapse navigation",
		"nav.back":                      "Back",
		"nav.back_to_prefix":            "Back to",
		"scope.no_parks_for_park_scope": "No parks available for park-wise scope",
		"scope.park_menu_aria":          "Park scope",
		// Why the top-bar park control is disabled on pages that carry their own park filter.
		// A disabled control must say WHY, and naming the page (the previous behaviour) did not:
		// on a route with no label rule it fell through to "Route unavailable", which is both
		// wrong -- the route is available -- and internal wording on a CEO screen.
		"scope.all_sheds":           "all pens",
		"scope.all_parks":           "All parks",
		"scope.selected_park":       "Selected park",
		"scope.company_wide":        "company-wide",
		"scope.no_parks_for_tenant": "No parks available for this tenant.",
		"date.as_of_fallback":       "As of",
		"date.menu_aria":            "Choose business date",
		"date.previous_month":       "Previous month",
		"date.next_month":           "Next month",
		"date.today":                "Today",
		"date.data_prefix":          "data",
		"date.disabled_badge":       "soon",
		"theme.switch_to_dark":      "Switch to dark theme",
		"theme.switch_to_light":     "Switch to light theme",
		"account.open_menu":         "Open account menu",
		"ceo_ai.title":              "Ask Mesha",
		"ceo_ai.subtitle":           "Ask about your operations",
		"ceo_ai.hello":              "Ask about Mesha operational data. This first version is read-only and leadership-only.",
		"ceo_ai.hello_meta":         "CEO/CXO analyst",
		"ceo_ai.checking":           "Checking Mesha data...",
		"ceo_ai.no_answer":          "No answer returned.",
		"ceo_ai.unavailable":        "The assistant could not answer right now.",
		"ceo_ai.unavailable_meta":   "unavailable",
		"ceo_ai.source_fallback":    "Mesha",
		"ceo_ai.mode_fallback":      "read-only",
		"ceo_ai.placeholder":        "Ask about sales, weighing, counts, feed...",
		"ceo_ai.open":               "Open Ask Mesha",
		"ceo_ai.close":              "Close Ask Mesha",
		"ceo_ai.send":               "Send question",
		"ceo_ai.starter_due":        "how many animals were sold this month?",
		"ceo_ai.starter_overdue":    "which pens have the lowest average weight?",
		"ceo_ai.starter_counts":     "which customers bought the most this month?",
		"ceo_ai.starter_help":       "make a graph of average weight by pen",
		"state.fresh":               "fresh",
		"state.freshness_pending":   "freshness pending",
		"state.days_old_suffix":     "d old",
		// Browser (Chrome) web push permission control, rendered inside the top-bar bell's panel
		// on EVERY admin route (components/push-permission-prompt.tsx via components/mesha-shell.tsx).
		// It lives in the bootstrap chrome copy map rather than a page contract for exactly that
		// reason: the control is shell chrome, not one screen's copy, so there is no single
		// AdminWebPageContract that owns it. Each key is also config-overridable through the
		// generic "chrome.copy.<key>" global entry the config compiler already accepts.
		"push.checking":          "Notifications",
		"push.checking_label":    "Checking notification support",
		"push.enable":            "Enable notifications",
		"push.enable_hint":       "Get notified in this browser",
		"push.enabling":          "Turning on",
		"push.enabled":           "Notifications on",
		"push.disable_hint":      "Turn off notifications in this browser",
		"push.disabling":         "Turning off",
		"push.disable_timed_out": "Turning notifications off took too long. Reload the page and try again.",
		"push.this_browser_only": "This browser only. Each browser and profile is enabled separately.",
		"push.dismissed":         "No choice was made. Click again when you are ready.",
		// Permission is 'denied': requestPermission() resolves without showing anything, so the
		// only true thing to say is where the person can undo it themselves.
		"push.blocked": "Notifications are blocked for this site. Turn them back on in your browser's site settings (the icon beside the address bar), then reload this page.",
		// The retryable timeout: permission was granted and nothing is known to be wrong, so it
		// says what happened and invites the retry the button beside it offers. These two were
		// the only push states left with no contract key, so the frontend map was their source
		// rather than a deploy-skew net.
		"push.timed_out": "That took too long and did not finish. Nothing is switched on yet — click again to retry.",
		"push.retry":     "Try again",
		// The two states nothing the reader can do will fix. They used to render the raw
		// engineering sentence behind them verbatim in the top bar on EVERY admin route
		// ("...is not a usable VAPID key..."), which is implementation vocabulary a CXO should
		// never see. The shape check that sentence documents is kept as a LOG line only.
		"push.unconfigured": "Notifications are not set up for this site yet. Ask your Goat OS administrator to switch them on.",
		"push.unsupported":  "This browser cannot show notifications here. Open the dashboard in Chrome on a laptop or an Android phone, on its secure (https) address, to get them.",
	}
}

func pages() []domain.PageContract {
	return []domain.PageContract{
		page("control-tower", "/?lens=control-tower", "/?lens=control-tower", "Control Tower", "Process-intact / not-intact leadership view for vaccination gaps.", "command-lens",
			[]domain.TableContract{table("open-gaps", "Open vaccination gaps — gap, severity, owner, next action", "/control-tower/vaccination", []string{"gap", "severity", "detail", "owner", "next_action"}, "ct_row")}),
		page("action-center", "/action-center", "/action-center", "Action Center", "Exact vaccination work and gaps to act on now.", "command-lens",
			[]domain.TableContract{
				table("work-board", "Vaccination work board", "/vaccination/action-center", []string{"work_state", "owner", "due", "task", "next_action"}, "ac_row"),
				table("verification-queue", "Awaiting verification", "/vaccination/verification-queue", []string{"goat", "administered", "doses", "verify"}, "completion_id"),
			}),
		page("calendar", "/calendar", "/calendar", "Calendar", "Vaccination due work and accepted completion history by time, owner lane, park, pen, and date.", "command-lens",
			[]domain.TableContract{
				table("calendar-events", "Due work", "/calendar/vaccination/events", []string{"due_at", "owner", "title", "status", "severity"}, "cal_event"),
				table("vaccination-open-obligations", "Open obligations", "/goats/{goat_id}/passport", []string{"scheduled_for", "vaccine", "status", "workflow"}, "obligation_id"),
				table("vaccination-history", "Vaccination", "/goats/{goat_id}/passport", []string{"administered", "vaccine", "status", "proof", "source_obligation"}, "completion_id"),
			}),
		page("protocol-adherence", "/protocol-adherence", "/protocol-adherence", "Protocol Adherence", "Expected vs actual vaccination ledger, evidence, operator assignment, and next action.", "command-lens",
			[]domain.TableContract{table("adherence-ledger", "Vaccination", "/vaccination/adherence", []string{"expected", "actual", "gap", "severity", "owner_chain", "next_action", "evidence"}, "adh_row")}),
		page("workflows", "/workflows", "/workflows", "Workflows", "Config → obligation → SOP → proof → verification → completion workflow records.", "command-lens",
			[]domain.TableContract{table("workflow-catalog", "Workflow catalog", "/vaccination/action-center", []string{"workflow", "stage", "owner", "next_action", "status"}, "wf_row")}),
		page("workflow-record", "/workflows/{row_id}", "/workflows/{row_id}", "Workflow drilldown", "One vaccination workflow chain reaction record.", "record-drilldown", nil),
		page("approvals", "/approvals", "/approvals", "Approvals", "Pending birth, death, and shifting requests raised from the field. Approve to apply the change, or reject with a reason.", "authority-screen",
			[]domain.TableContract{table("approval-requests", "Approval requests", "/admin-web/counts/approvals", []string{"request_type", "subject", "raised_at", "status", "action"}, "approval_request_id")}),
		page("leave", "/leave", "/leave", "Leave", "Leave requests raised from the Clock screen. The park head and HR both approve; either can reject with a reason.", "authority-screen",
			[]domain.TableContract{
				table("leave-approvals", "Waiting for you", "/admin-web/leave/approvals", []string{"person", "dates", "reason", "status", "action"}, "leave_request_id"),
				table("leave-requests", "All leave requests", "/admin/leave/requests", []string{"person", "park", "dates", "reason", "status", "raised_at"}, "leave_request_id"),
			}),
		// Work Board (maintainer decision 2026-09-10). Two tables: the board rows (one card
		// per module work item) and the paged subtasks inside a card's drawer. Both read
		// /work-board/rows; the drawer's subtask list is the module's own detail read and is
		// named here only so its page sizes are backend-owned.
		page("work-board", "/work-board", "/work-board", "Work Board", "Every module's work for one park and one day. Column is automatic: nothing started, started, submitted, approved.", "command-lens",
			[]domain.TableContract{
				withoutRowClick(tableP("work-board", "Board", "/work-board/rows", []string{"title", "module", "park", "pen", "owner", "clock", "work_state", "counts"}, "row_key", []int{25, 50, 100})),
				tableP("work-board-subtasks", "Subtasks", "/work-board/rows", []string{"name", "steps", "owner", "status"}, "row_key", []int{10, 25}),
			}),
		// Alerts (maintainer decision 2026-09-16). One table: the day's alerts for the parks in
		// scope, read per park from /alerts/rows and merged by the page. The Configure drawer
		// reads /alerts/config and is gated on alerts.configure (compileAlertsControls).
		page("alerts", "/alerts", "/alerts", "Alerts", "What is off today: a pen whose feed moved with no shifting recorded, a feed running out, and every rule you switch on.", "command-lens",
			[]domain.TableContract{
				withoutRowClick(tableP("alerts", "Alerts", "/alerts/rows", []string{"severity", "alert", "park", "pen", "detail", "rule"}, "key", []int{25, 50, 100})),
			}),
		page("leadership-tasks", "/tasks", "/tasks", "Tasks", "Tasks raised across CXOs, directors and park heads, with notes and attachments.", "monitoring-screen",
			[]domain.TableContract{table("leadership-task-progress", "Team progress", "/app/leadership-tasks", []string{"task", "assignee", "raised_by", "status", "evidence", "days_left"}, "task_id")}),
		// Routines (maintainer instruction 2026-09-16, docs/decisions/pen-routines.md). Two
		// tables: the routines of a park (the rule, its cadence and evidence lines, its people,
		// what it raised today) and the Today table of the tasks the kernel raised. Authoring
		// (create / edit / pause / retire) is capability-gated on pen_routines.configure on
		// BOTH halves: the three controls below and the route table.
		page("pen-routines", "/routines", "/routines", "Routines", "Recurring pen checks per park: what to ask, what to capture, who does it, and when. Today's checks below.", "module-surface",
			[]domain.TableContract{
				tableP("pen-routines", "Routines", "/admin/pen-routines", []string{"name", "park", "cadence", "evidence", "people", "status", "open_today"}, "routine_id", []int{25, 50, 100}),
				tableP("pen-routine-tasks", "Today", "/admin/pen-routines/tasks", []string{"routine", "pen", "assignee", "state_chip", "due"}, "task_id", []int{25, 50, 100}),
			}),
		// Configuration -> Items and settings (maintainer instruction 2026-09-18). ONE page, every
		// register: the rail lists the registers from /admin/configuration/registers and the
		// table shows the selected one from /admin/configuration/{register}. Column labels and
		// input types come from the register definition the backend serves, never from the page.
		// Every write is capability-gated on configuration.write on BOTH halves: the four
		// controls below and the route table.
		page("configuration-items", "/configuration/items", "/configuration/items", "Items and settings", "Farm places, animal types and catalogues: the lists every other screen picks from.", "module-surface",
			[]domain.TableContract{
				tableP("configuration-rows", "Register", "/admin/configuration/{register}", []string{"display", "fields", "status"}, "id", []int{25, 50, 100}),
			}),
		page("verification-review", "/verify", "/verify", "Verify", "Open a video, check it against the facts, and accept or reject it.", "authority-screen",
			// "vertical_module" was DROPPED (maintainer decision 2026-08-07). It rendered the
			// item's raw vertical/module tokens verbatim -- "preventive_care / vaccination" --
			// which is the config-token-as-UI-copy leak the label rules exist to stop, and it was
			// redundant besides: action_type already names the same module in human words.
			// in_queue/reviewed/review_took/watch are visible to EVERYONE who can open /verify
			// (verifier + CEO/director oversight alike) -- unlike oversight_analytics above, table
			// enrichment is not capability-gated: it is queue-row detail, not cross-module chrome.
			[]domain.TableContract{tableP("verification-actions", "Actions", "/verification/queue", []string{"action_type", "subject", "captured", "in_queue", "reviewed", "review_took", "status", "reason", "watch"}, "vi_row", []int{20, 50, 100})}),
		page("vaccination", "/vaccination", "/vaccination", "Vaccination", "Adult vaccination history, future campaigns, and current pen status.", "module-surface",
			[]domain.TableContract{
				// Shed-wise summary is the MAIN vaccination table (one row per shed, animal-level Due/Done,
				// planned Sessions, capacity, merged Status). 10 columns; default 25 rows.
				vaccinationShedTable(),
				table("full-vaccine-schedule", "Operator drive schedule", "/vaccination/drive-assignments", []string{"date", "operator", "park", "sheds", "partitions", "animals", "capacity"}, "schedule_row"),
				table("supplier-warmup", "Supplier warmup — Holding Farm", "/procurement/source-entry/loads", []string{"load", "holding_farm_supplier", "purpose", "animals", "warmup", "tagging", "vaccination_hf", "health_selection", "status"}, "warmup_load"),
			}),
		// Live drive-day tracker. Sibling of /vaccination, not a child of it: it answers a different
		// question (what is landing RIGHT NOW, per operator and per shed) at administration grain,
		// where /vaccination answers current status at animal grain.
		page("vaccination-live-tracker", "/vaccination/live-tracker", "/vaccination/live-tracker", "Live Drive Tracker",
			"Field proof arriving in real time — videos, scan captures and per-animal submissions per operator and per pen.",
			"module-surface", []domain.TableContract{
				table("live-operators", "Operators — live", "/vaccination/live-tracker",
					// `closed` sits beside `videos` on purpose: videos is a PHYSICAL upload count and closed
					// is the obligation-grain figure remaining is derived from. Collapsing them into one
					// column is what let a finished combo-day operator read as half done.
					[]string{"operator", "park", "now_at", "scheduled", "videos", "scans", "closed", "remaining", "progress", "status"}, "lt_operator"),
				tableP("live-sheds", "Pens — proof progress", "/vaccination/live-tracker",
					[]string{"shed", "vaccine", "operator", "scheduled", "received", "closed", "remaining", "progress", "last_proof", "status"}, "lt_shed", []int{25, 50, 100}),
				table("live-combo", "Combo doses", "/vaccination/live-tracker",
					[]string{"animal", "shed", "proof", "doses"}, "goat_id"),
			}),
		page("shed-execution", "/vaccination/execution/sheds/{shed_id}", "/vaccination/execution/sheds/{shed_id}", "Vaccination pen detail", "Pen-wise vaccination detail: planned sessions, per-vaccine breakdown, and the pen's animal roster.", "record-drilldown",
			[]domain.TableContract{
				table("planned-sessions", "Planned sessions", "/vaccination/sheds/{shed_id}", []string{"session_date", "vaccinations", "daily_limit", "capacity"}, "session"),
				table("shed-vaccines", "Vaccine breakdown", "/vaccination/sheds/{shed_id}", []string{"vaccine", "status", "last_dose", "next_due", "counts"}, "vaccine"),
				table("shed-animals", "Animals in pen", "/vaccination/sheds/{shed_id}/animals", []string{"display_id", "tag_1", "tag_2", "breed", "sex", "age", "lifecycle", "health", "last_vaccination_date", "next_vaccination_date", "vaccination_work"}, "goat_id"),
				table("shed-drive-rows", "Drive rows", "/vaccination/execution/sheds/{shed_id}", []string{"animal_stage", "drive", "due_date", "work_state", "proof_status", "next_action"}, "drive_row"),
			}),
		page("source-entry", "/procurement/source-entry", "/procurement/source-entry", "Source Entry Board", "Supplier warmup and accepted-intake bridge into Preventive Care (PC) vaccination.", "module-surface",
			[]domain.TableContract{table("source-loads", "Supplier warmup — Holding Farm", "/procurement/source-entry/loads", []string{"load", "holding_farm_supplier", "purpose", "animals", "warmup", "tagging", "vaccination_hf", "health_selection", "status"}, "source_load")}),
		// The procurement VENDOR REGISTER. One table, whole-filter total, keyset paging.
		//
		// Columns are the ones a person scanning the register actually needs: who they are, what
		// they supply, whether we are buying, and where they are. Banking is NOT a column -- it is
		// drawer detail behind VendorFinanceRead, because a table that renders account numbers puts
		// them on screen in every shoulder-surfing context the register is used in.
		//
		// The data source names side=procurement EXPLICITLY rather than relying on the endpoint's
		// unfiltered default. The default is "the whole register" -- correct for the vendor picklist,
		// wrong for this page -- so leaving it off would put the five buyer categories back on the
		// buying desk's screen the moment anyone read the contract literally.
		page("vendors", "/procurement/vendors", "/procurement/vendors", "Vendors", "The procurement register: livestock agents and stockists, transport, feed, manure, labour, insurance and site trades.", "module-surface",
			[]domain.TableContract{tableP("vendors", "Vendors", "/procurement/vendors?side=procurement", []string{"business_name", "record_type", "phone_number", "location_display", "status"}, "vendor_id", []int{25, 50, 100})}),
		// The SALES module: animal and manure sales. The deals
		// ledger is server-paged; the buyer board rides on GET /sales/overview and is paged in the
		// renderer, so its contract declares the page size and no row click -- there is no buyer
		// record to open, and a declared row click the page cannot honour would be a contract lie.
		//
		// SPLIT IN TWO (maintainer decision 2026-09-11): the Sales board carried the live-herd
		// valuation, the closed-sale blocks and the deals ledger on one screen. It is divided
		// into two pages and retired -- there is no "sales" page contract any more, and /sales
		// redirects to /sales/sold. SOLD holds what has sold (headline figures, sold weight
		// bands, month by month, price per kg by breed, buyers)
		// with the deals ledger LAST; FARM VALUE holds the live-herd valuation -- total farm
		// value, total meat, Over 35 kg with its error margin, and the by-category breakdown.
		// Both stay read-only by contract; entry is still /sales/config alone.
		page("sales-sold", "/sales/sold", "/sales/sold", "Summary", "What has sold across CBE and CPT — revenue, animals, price per kg, buyers and the deals ledger.", "module-surface",
			[]domain.TableContract{
				tableP("sales-deals", "Deals", "/sales/deals", []string{"sale_date", "farm", "buyer_name", "product_type", "breed", "animal_count", "total_weight_kg", "sales_value", "status"}, "deal_id", []int{25, 50, 100}),
				withoutRowClick(tableP("sales-buyers", "Buyers", "/sales/overview", []string{"buyer_name", "buyer_place", "product_types", "deals", "animals", "revenue", "share_pct"}, "", []int{10, 25, 50})),
			}),
		page("sales-farm-value", "/sales/farm-value", "/sales/farm-value", "Farm value", "What the live herd is worth today at Sales target rates, and how much of it is ready to sell.", "module-surface",
			[]domain.TableContract{}),
		// LOAD BY LOAD (maintainer decision 2026-08-31): every purchased load reconciled --
		// counts on one side, money on the other. Its OWN page under Sales, with the
		// Purchased / Farm born tabs at the top. Served whole (newest 60 loads) by the
		// procurement read; a row click opens the load-cost drawer, so the row key is the load id.
		page("sales-loads", "/sales/loads", "/sales/loads", "Load wise", "Every purchased load reconciled — bought, sold, died, still on farm — and the money on each side.", "module-surface",
			[]domain.TableContract{
				withoutRowClick(loadwiseTable()),
			}),
		// FARM BORN (maintainer request 2026-09-18): every animal the register marks BORN HERE
		// (goats.origin_type = 'birth'; maintainer decision 2026-09-22, superseding the
		// 2026-09-19 not-on-a-load rule), the counterpart of Load wise. What is on the farm today, what sold in the chosen
		// period, which breed / sex / stage / pen the sold ones came from, and what they brought
		// in; a filter bar on top (period, park, pen, species, breed, sex, stage). Served
		// by the procurement farm-born read (the load-wise shape; docs/decisions/sales-farm-born.md).
		// READ-ONLY by contract, the /sales/sold shape: no write control, so nothing here opens a
		// form. The sold ledger is the one table; its row is an animal and opens nothing.
		page("sales-farm-born", "/sales/farm-born", "/sales/farm-born", "Farm born", "Every animal born on this farm — how many are on the farm, how many sold in the period, of which breed, sex, stage and pen, and what they earned.", "module-surface",
			[]domain.TableContract{withoutRowClick(farmBornSoldTable())}),
		// MARKET ANALYTICS (maintainer decision 2026-09-14): the morning market-price calls read
		// back -- the latest price per city and question, and each one over time. READ-ONLY by
		// contract, the /sales/sold shape: it declares no write control, so nothing here can
		// open a form. Entry is the phone's Market tab; the cities and questions are Sales Config.
		page("sales-market-analytics", "/sales/market-analytics", "/sales/market-analytics", "Market analytics", "What goat and sheep fetch in the markets phoned each morning — live, carcass and offals prices by city, today and over time.", "module-surface",
			[]domain.TableContract{}),
		// BUYER ANALYTICS (maintainer request 2026-09-15): every buyer the farm has sold to, one
		// row each, served by the procurement read that joins the vendor register to the sales
		// ledger (the load-wise shape; docs/decisions/sales-buyer-analytics.md). READ-ONLY by
		// contract, the /sales/sold shape: no write control, so nothing here opens a form. The
		// phone column is a capability-gated READ control (compileBuyerAnalyticsControls): the
		// number is register data and follows VendorRead, exactly as the Vendors leaf does.
		page("sales-buyer-analytics", "/sales/buyer-analytics", "/sales/buyer-analytics", "Buyer analytics", "Who buys from the farm — each buyer's contact, how many times they have bought, how many animals, how much, and whether they come back.", "module-surface",
			[]domain.TableContract{withoutRowClick(buyerAnalyticsTable())}),
		// SALES > VENDORS (maintainer decision 2026-09-05): the vendor register's SELLING half.
		//
		// The SAME table contract and the SAME data source as /procurement/vendors, because it is
		// literally the same register -- one table, split by the side its record type declares. The
		// two pages are complementary: this one lists the five buyer categories (Agent, Butcher,
		// Company, Farmer, Slaughter House), Procurement lists everything else, and a record type
		// nobody catalogued stays on the procurement side rather than disappearing from both.
		//
		// The `side` on the data source is what performs the split, and it is part of the CONTRACT
		// rather than something the renderer appends: the page is defined as one half of the
		// register, and a renderer free to choose its own half could show a buying desk a butcher.
		page("sales-vendors", "/sales/vendors", "/sales/vendors", "Vendors", "The sales register: the agents, butchers, farmers, slaughter houses and companies the farm sells to.", "module-surface",
			[]domain.TableContract{tableP("sales-vendors", "Vendors", "/procurement/vendors?side=sales", []string{"business_name", "record_type", "phone_number", "location_display", "status"}, "vendor_id", []int{25, 50, 100})}),
		// SALES CONFIG (maintainer decision 2026-09-01): every sales WRITE, in one place.
		// Recording a sale, tagging its animals, the pipeline and evidence entry the retired Sales
		// DB sheet used to carry, and a load's landed cost all live here; /sales and /sales/loads
		// became read-only the same day. It publishes BOTH tables it writes against -- the deals
		// ledger (row click opens the deal, which carries the payment and status edits) and the
		// load list (row click opens the cost drawer) -- because a row that opens a form must
		// declare the param that form opens on.
		page("sales-config", "/sales/config", "/sales/config", "Sales Config", "Record a sale, tag its animals, and cost a purchased load. Sold and Load wise show these facts; this is where they are entered and changed.", "module-surface",
			[]domain.TableContract{
				tableP("sales-deals", "Deals", "/sales/deals", []string{"sale_date", "farm", "buyer_name", "product_type", "breed", "animal_count", "total_weight_kg", "sales_value", "status"}, "deal_id", []int{25, 50, 100}),
				loadwiseTable(),
			}),
		// FEED PURCHASES: the buying side of the feed chain (maintainer decision 2026-08-24,
		// retiring the read-only half of migration 000174). One server-paged ledger table whose
		// columns are the sheet's Purchase row, and one entry drawer behind the
		// record_feed_purchase control. Landed cost is ONE column: the feed/transport/loading/
		// unloading split is drawer detail, because a table that renders five money columns is a
		// table nobody can scan.
		// Animal purchases review (maintainer decision 2026-09-13): the CEO/CXO watches each
		// candidate animal's video and accepts or rejects it. The buying desk records loads and
		// animals on the phone; this page never records, so its only control is the decision.
		page("animal-purchases", "/procurement/animal-purchases", "/procurement/animal-purchases", "Animal purchases", "Loads on offer and the animals filmed in them. Watch each video and accept or reject the animal; the buying desk sees the answer on the phone at once.", "module-surface",
			[]domain.TableContract{animalPurchaseLoadTable(), animalPurchaseAnimalTable()}),
		page("feed-purchases", "/procurement/feed-purchases", "/procurement/feed-purchases", "Feed Purchases", "Feed bought for CBE and CPT — quantity, landed cost, vendor and payment state. These loads are what the stock and days-left cards on Feed Analytics are counted from.", "module-surface",
			[]domain.TableContract{
				feedPurchaseTable(),
			}),
		page("source-load", "/procurement/source-entry/loads/{load_id}", "/procurement/source-entry/loads/{load_id}", "Source load", "Full source-entry journey timeline, animal rows, decisions, and arrival gate.", "record-drilldown",
			[]domain.TableContract{
				table("load-goats", "Animals in load", "/procurement/source-entry/loads/{load_id}/goats", []string{"animal_ids", "selection", "current_stage", "source_entry", "ownership", "health", "warmup", "downstream"}, "load_goat"),
				table("pre-dispatch-decisions", "Pre-dispatch decisions", "/procurement/source-entry/loads/{load_id}/decisions", []string{"goat", "stage", "decision", "reason", "decided"}, "decision"),
				table("arrival-goats", "Arrival goats", "/procurement/source-entry/loads/{load_id}/arrival", []string{"goat", "arrival_state"}, "arrival_goat"),
				table("transit-handoffs", "Transit handoffs", "/procurement/source-entry/loads/{load_id}/transit", []string{"loaded", "from_location", "to_location", "dispatched", "status", "discrepancy"}, "transit"),
				table("holding-stays", "Holding stays", "/procurement/source-entry/loads/{load_id}/holding", []string{"goat", "holding", "started", "ended", "warmup", "state"}, "holding"),
				table("source-health-checks", "Source health checks", "/procurement/source-entry/loads/{load_id}/source-health", []string{"goat", "health", "checked"}, "source_health"),
				table("pc-handoffs", "Accepted intake → Preventive Care (PC) handoffs", "/procurement/source-entry/loads/{load_id}/pc-handoffs", []string{"goat", "park", "shed", "entry_date", "accepted", "event"}, "pc_handoff"),
			}),
		page("herd-register", "/counts/herd", "/counts/herd", "Herd Register", "Counts entry point for goat registration/import and vaccination trigger proof.", "module-surface",
			[]domain.TableContract{table("herd-register", "Herd Register", "/goats/search", []string{"display_id", "tag_1", "tag_2", "park", "shed", "breed", "sex", "weight", "lifecycle", "health", "breeding"}, "goat_id")}),
		// Counts -> Herd Analytics. Composition of the live herd beside the flow that
		// changed it. Every figure is backend-owned: the page derives no count of its own,
		// and the flow table's columns come from this table contract.
		page("herd-analytics", "/counts/analytics", "/counts/analytics", "Herd Analytics", "Herd composition by breed, pen tag, sex and age, beside month-by-month births, deaths and sales over a chosen window. Composition is the live herd right now; flow is counted off the canonical row that recorded each event.", "module-surface", nil),
		// Counts -> Mortality. Deaths in a window sliced by everything the animal's own row
		// froze at death, beside the live head count of each section for the rate series. No table
		// contract: every mark is drawn from the one /counts/mortality payload and the
		// recent-deaths list carries its columns in the copy map below.
		page("counts-mortality", "/counts/mortality", "/counts/mortality", "Mortality", "Deaths over a chosen window from every angle: overall rate, kids and adults, stage, age at death, breed, purchase load, vendor, cause, season, pen, sex, and the cross tabs between them. Every rate divides the deaths by the animals in that section today.", "module-surface",
			[]domain.TableContract{
				// The per-animal list under the charts. Its columns are this contract's, and so is its
				// page-size vocabulary -- the backend accepts exactly these three sizes, so a chip a
				// reader taps is always a page the server will serve. The list PAGES through the whole
				// window rather than stopping at a cap: a reader shown a rate for a vendor or a load has
				// to be able to read the animals behind it. The figures above it count the whole window
				// and do not move when the page turns.
				tableP("recent-deaths", "Deaths in this window", "/counts/mortality", []string{"died_on", "tag", "breed", "sex", "stage", "age_at_death", "farm", "shed", "load", "cause"}, "goat_id", []int{10, 25, 50}),
			}),
		page("counts-breakdown", "/counts/breakdown", "/counts/breakdown", "Counts Breakdown", "Live head count per pen, with breed, gender and stage on the same line; open a pen for the exact stage × breed × gender split.", "module-surface",
			[]domain.TableContract{
				// The page opens at PEN grain (maintainer decision 2026-09-04): one line per pen
				// carrying its whole head count with the breed, gender and stage composition
				// beside it, and the exact stage x breed x gender rows one click away. The
				// combination grain was the only view before this, and a reader wanting "how many
				// in Castro 2" had to add up to nine rows by eye.
				//
				// Every dimension sorts, including the count. Ordering applies to the PAGE the
				// operator is looking at, not to the whole filtered result — the pager states
				// the window, and the tfoot total stays the backend's whole-result figure.
				sortable(
					tableP("pen-breakdown", "Head count by pen", "/counts/breakdown?group_by=pen", []string{"farm", "shed", "stage", "breed", "gender", "female_male", "kids_adults", "count"}, "breakdown_pen", []int{10, 25, 50}),
					"farm", "shed", "stage", "breed", "gender", "count",
				),
				// The combination grain itself. An opened pen's rows render on the pen table's own
				// columns (stage · breed · gender · count under the same headings), so this contract
				// stays for the row shape the mobile client reads.
				sortable(
					tableP("detail-breakdown", "Detail Breakdown", "/counts/breakdown", []string{"farm", "stage", "breed", "gender", "shed", "count"}, "breakdown_row", []int{10, 25, 50}),
					"farm", "stage", "breed", "gender", "shed", "count",
				),
			}),
		// Weighing — the admin-web oversight read-out.
		//
		// Weighing is FREE-FLOW and ISOLATED: it records a scanned tag and a weight and
		// never resolves that tag to an animal. So this page carries NO breed, sex, age
		// or management-stage column, and no ₹ value — none of those facts exist in
		// weighing's tables and reaching into the herd tables for them is prohibited.
		// The columns below are the complete honest set.
		// /herd-signals -- BLE ear-tag telemetry. Tag-first: an UNMAPPED tag is the
		// normal state (tags are commissioned before they go on animals), so every
		// packet-derived column renders with or without an animal behind it.
		page("herd-signals", "/herd-signals", "/herd-signals", "Herd Signals",
			// The second sentence is the CLAIM BOUNDARY and is not decoration: the tag reports a
			// cumulative motion counter, and the page must say so where the reader meets the page,
			// not three tabs in. Dropping it left the subtitle reading as though the counters
			// described behaviour. Matches mock/herd-signals-mock.html verbatim.
			"BLE ear-tag signals, movement counters, and gateway coverage for mapped animals. "+
				"Values are read from the tag broadcast \u2014 the tag reports a cumulative motion counter, "+
				"not behaviour.", "module-surface",
			[]domain.TableContract{
				tableP("live", "Live tag signals", "/herd-signals/live",
					[]string{"animal", "smart_tag", "shed", "gateway", "signal", "motion_count",
						"delta_15m", "delta_1h", "activity", "pattern", "battery", "tag_temp",
						"last_seen", "status"}, "tag_id", []int{25, 50, 100}),
				tableP("gateways", "Gateways", "/herd-signals/gateways",
					[]string{"gateway", "location", "network", "status", "last_seen",
						"tags_seen", "weak_tags", "unmapped_tags"}, "gateway_id", []int{25, 50}),
			}),
		page("weighing-weights", "/weighing/weights", "/weighing/weights", "Kids — Weights", "Latest weight per pen across both capture modes, with park and period filters.", "module-surface",
			[]domain.TableContract{
				tableP("shed-weights", "Pens", "/weighing/shed-weights", []string{"park", "shed", "weighing", "animals_weighed", "average_weight", "total_weight", "last_weighed", "workflow"}, "location_id", []int{10, 25, 50}),
				tableP("losing-kids", "Kids losing weight", "/weighing/leadership/growth", []string{"tag", "shed", "previous", "latest", "change", "days_apart", "last_weighed"}, "scanned_identifier", []int{10, 25, 50}),
				// Where each purchase load's weighed animals actually sit. It rides on the
				// SAME /weighing/shed-weights response as the load chart above (the
				// placements ride on by_load), so it declares no row click -- there is no
				// load record to open, and a declared row click the page cannot honour
				// would be a contract lie.
				withoutRowClick(tableP("load-placements", "Where each load sits", "/weighing/shed-weights", []string{"load", "park", "sheds", "animals"}, "", []int{10, 25, 50})),
				// How many kids of each breed are clearing each daily-gain mark. It rides on the
				// SAME /weighing/weight-demographics response as the breed gain chart, and there
				// is no breed record to open, so it declares no row click. The breed vocabulary
				// is bounded by the herd catalogue, so it is not paged.
				weightsGainThresholdTable(),
			}),
		// Weights analytics -- six tabs over the SAME reads the Weights page uses, so the two
		// screens can never disagree about a number. It declares ONE table (the shed-wise tab's
		// figures); every other tab is a chart, and a chart is not a TableContract.
		page("weighing-analytics", "/weighing/analytics", "/weighing/analytics", "Kids — ADG Analytics", "Growth cut eight ways: overall, by breed, by farm-born vs purchased, by pen type, by weight band, by week, by purchased load, and feed conversion.", "module-surface",
			// The shed table on the General tab, which is the Weights page's own table read from
			// the same endpoint -- so the two screens cannot disagree about a shed's figures.
			//
			// ONE COLUMN MORE THAN THE WEIGHTS PAGE'S: daily_gain. This is an analytics screen, and
			// a shed table without the gain forces a reader to cross-reference the Shed-wise tab to
			// answer the question they came with. The figures are the same ones that tab draws --
			// the per-animal median for a scanned shed, the average-weight movement for a
			// whole-shed pen -- so the table and the chart cannot disagree.
			//
			// The four analysis tabs declare NOTHING here on purpose: every one of them is a
			// CHART, and a TableContract for a chart would be a contract nothing can honour.
			// The Load-wise tab is the exception below: it carries a real table.
			//
			// NO workflow column and SORTABLE on the two measures (maintainer request 2026-09-03):
			// this table answers "which pens are heavy / growing", so it sorts on average weight
			// and daily gain, and the bucket's workflow state -- an execution fact -- stays on
			// the Weights page where the work is followed. Sorting is client-side over the rows
			// already served; the window is still the backend's. The Breed column (maintainer
			// request 2026-09-21) names the pen's resident cohort from the same composition
			// chips the Weights table renders: one breed is named, several read "Mixed breeds",
			// because one pen average cannot be divided between two cohorts and naming one of
			// them would claim a herd nobody weighed.
			[]domain.TableContract{
				sortable(tableP("shed-weights", "Pens", "/weighing/shed-weights", []string{"park", "shed", "breed", "weighing", "animals_weighed", "average_weight", "daily_gain", "total_weight", "last_weighed"}, "location_id", []int{10, 25, 50}), "average_weight", "daily_gain"),
				// The FCR tab's pen table (maintainer request 2026-09-07): one row per pen weighed
				// in the period -- feed the sheet directed between its weighing rounds against the
				// kilograms it gained, the ratio between the two, and what that gain is worth at the
				// maintainer's assumed sale price. Served by /growth-director/fcr, the read-only
				// reporting module that may read weighing, herd and feed tables together. No row
				// click: the pen's detail is the row.
				withoutRowClick(tableP("fcr-pens", "Pens", "/growth-director/fcr", []string{"pen", "cohort", "animals", "weighed", "daily_gain", "head_days", "gain_kg", "feed_kg", "fcr", "feed_cost", "gain_value", "margin", "feed_cost_per_kg_gain", "feed_sheet"}, "", []int{10, 25, 50})),
				// The Load-wise tab's table (maintainer request 2026-09-03) -- one row per
				// purchased load: the average weight the load arrived at against the latest
				// weighing of the animals that came off it, and how many times over the arrival
				// weight the load now stands (a growth multiple such as 1.5x). The purchase side
				// is the procurement load ledger (/procurement/loadwise-sales, purchase_weight_kg
				// over purchased animals); the weighing side is the SAME /weighing/shed-weights
				// by-load read the Weights page's load chart uses, matched by load reference --
				// so this tab can never disagree with either source about a number. A load with
				// no weighing attribution, or no recorded purchase weight, reports absence, never
				// a zero or an invented multiple. No row click: there is no load record to open.
				withoutRowClick(tableP("load-comparison", "Loads", "/procurement/loadwise-sales", []string{"load", "vendor", "animals", "purchased_avg", "latest_avg", "multiple"}, "", []int{10, 25, 50})),
				// The Time-wise tab's two week-pivot tables (maintainer requests 2026-09-08 and
				// 2026-09-14): every pen's, and every purchased load's, daily gain in every
				// calendar week of the selected period. Only the FIXED columns are declared here;
				// the week columns are data (the weeks the response actually carries) and are
				// headed by a date, not a label. Both read the SAME demographics response the
				// breed rows use, so the three can never describe different weeks. No row click:
				// there is no record to open, and no pagination: the grid is the whole period.
				withoutRowClick(tableP("pen-week-gain", "Weekly growth by pen", "/weighing/weight-demographics", []string{"park", "shed"}, "", []int{50})),
				withoutRowClick(tableP("load-week-gain", "Weekly growth by load", "/weighing/weight-demographics", []string{"load", "source"}, "", []int{50})),
				// The Weight-wise tab's feed table (maintainer request 2026-09-18): the latest
				// locked feed direction for every pen beside the pen's latest weight evidence,
				// one row per pen-average pen and one per 5 kg band for a per-animal pen. Read
				// from the Growth Director module, which is the one place allowed to join the
				// feed sheet to weighing. No Ration column by request; the group and gender are
				// read off the sheet's own shed tag. No row click: there is no record to open,
				// and no pagination: the table is one day's sheet.
				feedWeightBandTable(),
				feedWeightBandUnmatchedTable(),
				feedWeightBandExitsTable(),
			}),
		page("milk-preparation", "/counts/milk-preparation", "/counts/milk-preparation", "Milk Preparation", "Current per-pen milk direction plus park-day step-video verification state for K1, K2, and K3 cohorts.", "module-surface",
			[]domain.TableContract{tableP("milk-preparation", "Milk preparation worklist", "/counts/milk-preparation", []string{"park", "shed", "cohort", "head_count", "session_1", "session_2", "session_3", "session_4", "daily_total", "status"}, "milk_preparation_row", []int{10, 25, 50})}),
		// ---------------------------------------------------------------------------
		// Feed vertical — three module surfaces.
		//
		// Direction and Packing are two views of the SAME generated day: Direction is the
		// per-grain sheet (what each park/shed/breed/session eats), Packing is the per-shed
		// rollup the store packs to. They therefore share /feed-direction/generation-preview as
		// their data source; Packing is a shed x session x item rollup of it, not a second
		// generation run. Config is the authored input the generation reads.
		//
		// NO `park` COLUMN ON ANY FEED TABLE. Every Feed read endpoint takes park_id as a
		// REQUIRED query parameter and scopes its SQL with `AND park_id = $2` — one park per
		// request, always. A park column therefore repeats the same value on every row of every
		// page, and the pinned park is already named by the page's Park filter control. The
		// column is not merely redundant, it is expensive: Feed Direction carries ten columns in
		// an 1130px card, and the width the repeated park label consumed was the width the
		// multi-word feed-item label needed. Without it the item label wrapped to three lines and
		// the page grew 46% taller; with it removed the label sits on one line again and
		// "Session total (kg)" — the row's whole output — stays fully visible. If Feed ever gains
		// a genuinely multi-park read, that endpoint gets its own contract; do not re-add a
		// constant column here.
		// ---------------------------------------------------------------------------
		page("feed-direction", "/feed/direction", "/feed/direction", "Feed Direction", "Generated per-pen feed sheet for the selected day: projected head count x authored ration, split across the park's sessions.", "module-surface",
			// shed_tag and breed report the ANIMALS on every workflow. The experiment arm
			// is deliberately NOT a column: it names which trial a shed is enrolled in,
			// which is authoring context rather than anything that changes what gets
			// weighed out, and it is empty on every normal row. It lives on /feed/config
			// where it is authored. What must never come back is the original defect —
			// an experiment row printing its arm ("Sheep M NEW") under SHED TAG while the
			// shed's real tag ("F2-Male") and breed ("Anantapur Sheep") went unreported,
			// which made the tag column untrustworthy on every row, not just experiment
			// ones. Dropping the column does not reinstate that: the API still returns
			// experiment_arm as its own field, it is simply not rendered here.
			[]domain.TableContract{tableP("direction-rows", "Feed Direction rows", "/feed-direction/generation-preview", []string{"shed", "shed_tag", "breed", "session", "head_count", "feed_item", "quantity_kg", "session_total_kg", "status"}, "direction_row", []int{10, 25, 50})}),
		page("feed-packing", "/feed/packing", "/feed/packing", "Feed Packing", "Per-pen packing worklist for the selected day: what the store weighs out per pen, session and feed item.", "module-surface",
			[]domain.TableContract{tableP("packing-worklist", "Packing worklist", "/feed-direction/generation-preview", []string{"shed", "session", "feed_item", "expected_kg", "status"}, "packing_row", []int{10, 25, 50})}),
		page("feed-analytics", "/feed/analytics", "/feed/analytics", "Feed Analytics", "Directed feed, ration per animal and execution adherence across the farms — served from the frozen daily sheet and the proof-gated completions. Figures run up to yesterday and state what the sheet DIRECTED, not what was eaten.", "module-surface",
			[]domain.TableContract{
				tableP("directed-items", "Directed feed by item", "/feed-analytics/directed", []string{"feed_day", "feed_item", "directed_kg", "head_days", "per_head_grams"}, "directed_item_row", []int{31, 62, 92}),
				tableP("packing-mismatches", "Packed vs directed", "/feed-analytics/execution", []string{"packing_day", "park", "shed", "session", "feed_item", "breed", "planned_kg", "verified_kg", "variance_kg"}, "variance_row", []int{25, 50, 100}),
				// The completion table pages at TEN by default (maintainer ask 2026-08-26). A park-day
				// runs to a couple of hundred pen-sessions and the reader arrives to find the handful
				// nobody fed -- the status filter and the four counts do the finding, so the page only
				// has to stay short enough to read.
				tableP("distribution-completions", "Feed direction completion", "/feed-analytics/execution", []string{"park", "pen", "session", "status", "videos", "submitted_by", "submitted_at"}, "fdc_row", []int{10, 25, 50}),
				// Purchased vs consumed (maintainer request 2026-09-19): every load in the purchase
				// ledger with its FIFO consumption, and the buyer's optional "days of stock" figure
				// checked against days consumed plus days left. Column labels are authored under
				// `loads.col.*` because the derived-from-key label would read "Days said" for a
				// figure that needs its own words.
				tableP("stock-loads", "Purchased vs consumed", "/feed-analytics/stock-loads", []string{"purchase_date", "feed_item", "status", "purchased_kg", "consumed_kg", "left_kg", "days_said", "days_consumed", "days_left", "gap_days"}, "feed_purchase_id", []int{25, 50, 100}),
				// The overview's "Feed by pen" section is CHARTS, not a table (maintainer
				// request 2026-09-14: "table is not needed, replace it by graphs"), so it
				// declares no TableContract -- a contract for a chart would be one nothing
				// can honour. Its copy is authored under `shedfeed.*` below.
			}),
		page("feed-config", "/feed/config", "/feed/config", "Feed Config — Ration Rules", "Feed-owned authority screen for the authored ration grid, per-feed factors, session template and feeding schedule.", "module-surface",
			[]domain.TableContract{
				// Every table below EXCEPT feed-items is read through a /feed-config/* endpoint that
				// requires park_id and filters on it, and they share ONE Park filter on the page — so
				// a park column would print the same value on every row of those sections.
				// valid_from/valid_to are deliberately NOT sortable: the two columns are rendered as
				// ONE merged effective-window cell (in-force vs superseded, plus the dates), so a
				// sort affordance on either header would point at a value the cell does not show
				// on its own.
				sortable(
					tableP("ration-grid", "Ration grid", "/feed-config/ration-rates", []string{"ration_group", "shed_tag", "feed_item", "grams_per_head", "valid_from", "valid_to"}, "ration_rate_id", []int{10, 25, 50}),
					"ration_group", "shed_tag", "feed_item", "grams_per_head",
				),
				// The feed-item CATALOG: the tenant's feed vocabulary, and the only table on this
				// page that is NOT park-scoped — feed_item_catalog is keyed (tenant, item), so both
				// parks author quantities against one list. Its park-freedom is therefore a
				// different fact from the other tables', which are park-scoped and simply do not
				// repeat the column.
				//
				// Directly under the ration grid because it is the vocabulary that grid's feed_item
				// column is drawn from. Before it existed, a feed item had no visible home at all:
				// the catalog surfaced only as options inside a filter select, with no way to see
				// what it holds or to confirm an addition landed.
				//
				// The three nutritional columns are declared even though every seeded row leaves
				// them NULL. That emptiness is the honest state and worth rendering: a missing
				// energy value blocks a rollup, never a feeding decision, so it is a reportable gap
				// — and hiding the columns until something fills them would make the gap invisible
				// on the one screen that can close it.
				// Status sorts too: grouping the inactive items together is the fastest way to
				// audit what is currently off the feed sheets.
				sortable(
					table("feed-items", "Feed items", "/feed-config/feed-items", []string{"feed_item", "energy_kcal_per_kg", "dry_matter_factor", "wastage_factor", "display_order", "status"}, "feed_item_id"),
					"feed_item", "energy_kcal_per_kg", "dry_matter_factor", "wastage_factor", "display_order", "status",
				),
				table("shed-factors", "Feed factors", "/feed-config/shed-factors", []string{"shed", "feed_item", "multiplier", "valid_from", "valid_to"}, "shed_factor_id"),
				// The EXPERIMENT sheds, deliberately its OWN table rather than extra rows or a
				// column on the ration grid above. The two are not two views of one thing: a
				// grid row's rate comes from the authored grid for a (group, tag, item), and an
				// experiment row's comes from a hand-entered cell for ONE pen -- and a pen may
				// still hold a LEGACY cell whose number is an absolute pen total that must never
				// be multiplied by anything (maintainer decision 2026-09-01: new cells are grams
				// per animal, old ones keep their basis). Interleaving them would put numbers with
				// different units under one heading on a screen whose output is a feeding
				// instruction, which is why each cell carries its own unit. head_count is carried
				// here only as informational context and is labelled as such (see humanLabel and
				// label.experiment_head_count_note): it is the pen's LIVE population, read per request.
				// No valid_from/valid_to pair: unlike every other table on this page,
				// feed_experiment_config is not effective-dated (migration 000006).
				// Page sizes 50/100/200, not the generic 5/10/25/50: rows here are grouped into PENS
				// and a pen holds one row per authored feed item, so a screenful of cells is a
				// fraction of a pen and a page that splits a pen is unreadable. 100 is the default
				// (~20 pens at five items); 200 is the backend's own cap and exists because a pen may
				// now carry as many cells as the catalog has items, so the row count grows with the
				// feed vocabulary rather than with the shed count.
				tableP("experiment-config", "Experiment pens", "/feed-config/experiment", []string{"park", "shed", "experiment_category", "live_head_count", "feed_item", "experiment_quantity", "status"}, "experiment_config_id", []int{10, 25, 50}),
				// `feeds` is the session's RECIPE, and it is the column that answers whether a feed
				// reaches an animal at all: generation walks these slots and looks each one up in the
				// ration grid, so a feed with a grid quantity but no slot is silently absent from the
				// sheet. It was missing from this table entirely, which is why COFS could carry
				// 2157 g/head for Anantapur Sheep bucks from 2026-08-05 to 2026-08-09 and reach zero
				// of the sheets issued in that window with nothing on this screen showing why.
				table("session-template", "Session template", "/feed-config/session-templates", []string{"session_no", "session_label", "split_fraction", "feeds", "status"}, "session_template_id"),
				// These are the three DISPATCH-CLOCK moments of a feed day, not session times. The
				// table renders direction_time / correction_time / transport_time, so it must be
				// headed by them: under the old session_no/session_label/start_time/end_time keys a
				// direction time appeared as "Session name: 14:00:00" and the pair 14:00–15:45 read
				// as WHEN THE ANIMALS ARE FED. Animals are fed the NEXT morning; these are the
				// issue, amend and cutoff times of the sheet that feeds them.
				table("schedule-config", "Feed day clock", "/feed-config/schedule", []string{"workflow", "direction_time", "correction_time", "transport_time", "status"}, "schedule_row"),
			}),
		// ---------------------------------------------------------------------------
		// Health Config — the authored treatment rulebook.
		//
		// TWO tables, and the split is the point rather than a layout choice. The catalog lists
		// one row per disease per AGE BAND, because the two bands are separately authored
		// documents that a diagnosis picks between using the goat's own age -- collapsing them
		// into one disease row would hide that a kid's dosage differs from an adult's (two of the
		// 27 imported diseases already do). The steps table is the document itself, read inside
		// the editor drawer for the selected protocol.
		//
		// No KPI cards. Every list endpoint here returns a keyset cursor and no total, because
		// counting the filtered catalog on each request is compute-on-read and a headline computed
		// from the visible page would be a false statement about the rulebook.
		// ---------------------------------------------------------------------------
		// ---------------------------------------------------------------------------
		// HEALTH -> HEALTH ANALYTICS. The Health vertical's leadership read: what the
		// herd is sick with, whether the prescribed course is being carried out, and
		// what it is dying of.
		//
		// It is a MODULE SURFACE, not an authority screen: it authors nothing, and it
		// needs no entry in the Config allowlist. Every figure and every visible string
		// is backend-owned — the four tables below declare their columns, and the page
		// re-derives no count of its own.
		//
		// The tables all read the SAME endpoint, because the whole page is one
		// aggregate response. That is deliberate: four screens' worth of figures that
		// must reconcile with each other (deaths on the mortality tab against deaths in
		// the KPI strip) cannot be served by four independently-windowed reads.
		// ---------------------------------------------------------------------------
		page("health-analytics", "/health/analytics", "/health/analytics", "Health Analytics", "What the herd is being treated for, whether the prescribed courses are actually carried out, and what it is dying of. New deaths carry the cause recorded on the death form; older deaths are attributed only where a case was open at the time.", "module-surface",
			[]domain.TableContract{
				table("health-disease-board", "Disease board", "/health/analytics", []string{"disease", "age_band", "new_cases", "open_cases", "recovered", "died", "case_fatality"}, "disease_key"),
				// The evidence trail beside the mortality counts, bounded to the latest 50.
				// `attribution` is a chip, not a disease name: an unattributed death has no
				// disease and must never be rendered as though it had one.
				table("health-deaths", "Deaths", "/health/analytics", []string{"animal", "pen", "date", "age_band", "attribution", "days_treated"}, "goat_id"),
				table("health-medicines", "Medicines given", "/health/analytics", []string{"medicine", "route", "doses", "animals"}, "medicine"),
				table("health-engine-rules", "Rules the engine proposed", "/health/analytics", []string{"rule", "proposed", "opened", "not_taken_up"}, "rule"),
			}),
		page("health-config", "/health/config", "/health/config", "Health Config", "Health-owned authority screen for the farm's whole clinical rulebook: which illness an animal is judged to have, and what it is then given.", "module-surface",
			[]domain.TableContract{
				tableP("protocol-catalog", "Treatment protocols", "/health-config/protocols", []string{"display_name", "age_band", "duration_days", "step_count", "medication_count", "critical_action_count", "published_version", "draft_state"}, "protocol_version_id", []int{10, 25, 50}),
				// The DIAGNOSIS REGISTER, the other half of the same rulebook: the questions
				// the observation form asks, the tokens each answer emits, and the rules
				// those tokens fire. One row per animal class, because a milk kid is never
				// diagnosed against the adult table.
				table("register-catalog", "Diagnosis registers", "/health-config/registers", []string{"animal_class", "register_label", "status", "question_count", "rule_count", "published"}, "register_version_id"),
				// The document, in two tables because an author works on one half at a time:
				// what the form ASKS, and what the rules MAKE of the answers.
				table("register-questions", "Questions", "/health-config/registers", []string{"title", "kind", "section", "answers", "emits", "only_if"}, "question_id"),
				table("register-rules", "Rules", "/health-config/registers", []string{"rule_id", "treats", "severity_base", "pathognomonic", "probable", "possible"}, "rule_id"),
				// WHO IS JUDGED BY WHICH RULEBOOK (migration 000395). Three tables because they
				// answer three different questions and a reader wants them apart: what types
				// exist, which stages reach them, and which stages reach NOTHING.
				table("diagnosis-types", "Diagnosis types", "/health-config/diagnosis-types", []string{"label", "type_key", "status", "route_count", "has_published_register"}, "type_key"),
				table("diagnosis-routes", "Who gets which type", "/health-config/diagnosis-types", []string{"age_band", "stage_label", "type_label", "sub_stage", "live_animals"}, "stage_code"),
				// The gap table earns its place by being the only surface that can show it. A
				// stage holding animals and reaching no type produces no error anywhere until a
				// manager is refused in a shed.
				table("diagnosis-gaps", "Stages with no type", "/health-config/diagnosis-types", []string{"stage_label", "age_band", "live_animals"}, "stage_code"),
				// The document. medicine/dosage/route are empty on action and critical-action
				// steps by design -- those steps carry an instruction instead -- so the columns are
				// deliberately sparse rather than being split into three tables an author would
				// have to reconcile in their head.
				table("protocol-steps", "Protocol steps", "/health-config/protocols", []string{"day_no", "session", "record_type", "medicine_name", "dosage_text", "dosage_denominator", "medicine_route", "instruction", "critical_action_type"}, "step_id"),
			}),
		page("audit-log", "/operations/audit", "/operations/audit", "Audit Log", "Business audit trail for built admin/operator/system actions.", "authority-screen",
			[]domain.TableContract{table("activity-trail", "Activity trail", "/operations/audit", []string{"when", "operation", "operator", "action", "target", "result", "proof"}, "audit_row")}),
		page("dlq-center", "/operations/dlq", "/operations/dlq", "DLQ Center", "System repair queue for backend events that failed after retries. Empty is healthy; this is not a vaccination worklist.", "authority-screen",
			[]domain.TableContract{table("dlq-events", "Dead-letter events", "/operations/dlq", []string{"event", "topic", "attempts", "replays", "last_error", "updated"}, "dlq_id")}),
		// The vaccination plan console. Preventive Care owns it, because the plan is
		// vaccination-only and the CEO/COO who publishes it works out of PC. It replaces
		// the generic /config screen for this category and absorbs /vaccination/sops.
		page("vaccination-plan", "/vaccination/plan", "/vaccination/plan", "Vaccination plan", "One plan decides which animal gets which vaccine, and when.", "authority-screen",
			[]domain.TableContract{
				table("vaccination-plan-versions", "Versions", "/protocols?category=vaccination", []string{"version", "status", "in_force", "published", "changed"}, "protocol_version_id"),
			}),
		// People/HRMS rewrite (maintainer request 2026-08-22): the default view is
		// the ALL-PEOPLE directory (every member with park, department, and
		// designation, plus the Add Person onboarding drawer); the former
		// vaccination-operators screen lives under the `vaccination` tab of the
		// backend-owned `people_view_tabs` option group. The dead `timetable`
		// table contract is dropped (its panel had no importers).
		page("people", "/people", "/people", "People / HRMS", "Everyone on the farm — park, department, designation, and login — with per-module staffing views", "authority-screen",
			[]domain.TableContract{
				tableP("people", "All People", "/admin/workforce/people", []string{"display_name", "park", "department", "designation", "email", "status", "clock_in_today"}, "person_id", []int{25, 50, 100}),
				table("positions", "Vaccination Operators", "/admin/roster/positions", []string{"person_display_name", "position_title", "center_label", "week_off", "vaccination_daily_animal_cap", "status"}, "position_id"),
				// Clock In / Out tab (maintainer decisions 2026-08-27/28): one row
				// per active person per selected IST day, not-clocked-in included.
				table("clock-entries", "Clock In / Out", "/admin/workforce/clock-entries", []string{"person", "park", "designation", "clock_in", "clock_out", "hours", "location", "device", "flags"}, "clock_entry_id"),
				// Notifications tab (maintainer decision 2026-09-08): which DESIGNATION hears
				// which alert. One row per configurable alert; the designation columns are
				// tenant rows carried in the data source, so only the fixed columns are named.
				table("notification-audiences", "Notifications", "/admin/notifications/designations", []string{"alert", "who_receives"}, "alert_key"),
			}),
		// SOP SPLIT (maintainer decision 2026-08-18): the /sops authority screen is retired;
		// each remaining module owns its SOP page as a module-surface, sharing the
		// sop-library table contract over /admin/sops — the page scopes which SOP codes it
		// lists. Vaccination is NOT among them: its SOP surface was absorbed into
		// Preventive Care / Vaccination plan, where the proof method is one field on the
		// plan rather than a separate document to author.
		page("counts-sops", "/counts/sops", "/counts/sops", "Herd Operations SOP", "Birth, death, shifting and reconcile SOPs: the questions, proof and timing the phone runs for the herd register.", "module-surface",
			[]domain.TableContract{table("sop-library", "Herd Operations SOPs", "/admin/sops", []string{"sop", "domain", "trigger", "steps", "gates", "status"}, "sop_id")}),
		page("feed-sops", "/feed/sops", "/feed/sops", "Feed SOP", "Distribution, packing, and transport SOP documents for the feed chain.", "module-surface",
			[]domain.TableContract{table("sop-library", "Feed SOPs", "/admin/sops", []string{"sop", "domain", "trigger", "steps", "gates", "status"}, "sop_id")}),
		// SOP split EXTENSION (maintainer decision 2026-08-22): Milk and Weighing get the same
		// module-surface SOP page shape as the three 2026-08-18 routes.
		page("milk-sops", "/milk/sops", "/milk/sops", "Milk SOP", "Preparation and feeding SOP documents for the kid-milk round.", "module-surface",
			[]domain.TableContract{table("sop-library", "Milk SOPs", "/admin/sops", []string{"sop", "domain", "trigger", "steps", "gates", "status"}, "sop_id")}),
		page("weighing-sops", "/weighing/sops", "/weighing/sops", "Weighing SOP", "The scan-and-submit weighing session document.", "module-surface",
			[]domain.TableContract{table("sop-library", "Weighing SOPs", "/admin/sops", []string{"sop", "domain", "trigger", "steps", "gates", "status"}, "sop_id")}),
		// PC CARE SOP (maintainer decision 2026-09-22): what the operator captures and answers
		// for deworming, anti protozoan, ticks removal and the two trimming jobs, and whether a
		// tablet-in-feed deworming removes feed & water the evening before.
		page("pc-care-sops", "/pc-care/sops", "/pc-care/sops", "Preventive Care SOP", "What the operator captures and answers on a deworming, ticks removal or trimming task — and the evening-before feed & water removal.", "module-surface",
			[]domain.TableContract{table("sop-library", "Preventive Care SOPs", "/admin/sops", []string{"sop", "domain", "trigger", "steps", "gates", "status"}, "sop_id")}),
		// SOP studio phase 2 (2026-09-18): general work instructions, the SOP kind tied to no
		// module. Same library + operator-steps editor shape as the module pages; the code
		// prefix `general.` and kind = general are set by the builder.
		page("configuration-work-instructions", "/configuration/work-instructions", "/configuration/work-instructions", "Work instructions", "General SOPs: farm-wide work tied to no module. Authoring remains available; the phone runner is retired.", "module-surface",
			[]domain.TableContract{table("sop-library", "Work instructions", "/admin/sops", []string{"sop", "domain", "trigger", "steps", "gates", "status"}, "sop_id")}),
		// SALES SOP (maintainer instruction 2026-09-19, docs/decisions/sales-sop.md): the steps a
		// recorded sale owes -- tag the animals, load them, the money -- and the designation that
		// does each, authored here and run by the tasks engine as one workflow per sale.
		page("sales-sops", "/sales/sops", "/sales/sops", "Sales SOP", "What happens after a sale is recorded -- tagging, loading, the money -- and who does each step.", "module-surface",
			[]domain.TableContract{table("sop-library", "Sales SOPs", "/admin/sops", []string{"sop", "domain", "trigger", "steps", "gates", "status"}, "sop_id")}),
		// PROCUREMENT SOP (maintainer decision 2026-09-14): the animal-purchase inspection --
		// its pages, questions, proof and compulsory flags -- is authored here and served to the
		// phone from the published version.
		// PROCUREMENT IS SOP-DRIVEN END TO END (maintainer decision 2026-09-20): the page now
		// carries every procurement document -- the inspection and load forms, the supplier form,
		// the animal-purchase and feed-purchase workflows, and the aflatoxin procedure.
		page("procurement-sops", "/procurement/sops", "/procurement/sops", "Procurement SOP", "Everything the buying desk runs: the purchase steps, the inspection and supplier forms, and the aflatoxin procedure.", "module-surface",
			[]domain.TableContract{table("sop-library", "Procurement SOPs", "/admin/sops", []string{"sop", "domain", "trigger", "steps", "gates", "status"}, "sop_id")}),
		page("goat-passport", "/goats/{goat_id}", "/goats/{goat_id}", "Goat Passport", "Contextual goat identity, timeline, and vaccination passport detail.", "record-drilldown",
			[]domain.TableContract{
				table("vaccination-open-obligations", "Open obligations", "/goats/{goat_id}/passport", []string{"scheduled_for", "vaccine", "status", "workflow", "action_center"}, "obligation_id"),
				table("vaccination-history", "Vaccination", "/goats/{goat_id}/passport", []string{"administered", "vaccine", "route", "status", "proof", "source_obligation", "workflow"}, "completion_id"),
			}),
	}
}

func page(id, href, pattern, title, subtitle, kind string, tables []domain.TableContract) domain.PageContract {
	if tables == nil {
		tables = []domain.TableContract{}
	}
	copy := pageCopy(id)
	copy["page.title"] = title
	copy["page.subtitle"] = subtitle
	return domain.PageContract{
		RouteID: id, Href: href, PathPattern: pattern, Title: title, Subtitle: subtitle, SurfaceKind: kind,
		SourceScope: []string{"current-admin-web-scope", "mock-goatos-dashboard", "active-vaccination-process-integrity-slice"},
		Sections:    []domain.Section{{ID: "primary", Title: title, Kind: kind, ChipKeys: []string{}}},
		Tables:      tables,
		Drawers: []domain.DrawerContract{
			{ID: "record", TitleSource: "selected_row", TriggerParam: "row_param", DataSource: "selected_object", Anatomy: "mock-record-drawer-metagrid", SummaryFields: []string{"title", "status", "owner", "next_action"}, DetailFields: []string{"all_contract_fields"}, FooterActions: []domain.Control{}},
		},
		Controls:        []domain.Control{},
		Copy:            copy,
		OptionGroups:    pageOptionGroups(id),
		MigrationStatus: "contract-published",
		ValidationNotes: []string{
			"Frontend may choose compact row fields versus full drawer fields from the same backend object.",
			"Frontend owns layout density, responsive wrapping, focus/open state, and icon token rendering only.",
		},
	}
}

// tableP is table() with an explicit page-size option set (the shed-wise list defaults to 25 with
// 25/50/100 options, unlike the generic 5/10/25/50 board default).
// withoutRowClick turns off a table's row-click rule. A board whose rows open nothing must not
// advertise a drawer the renderer cannot open -- the contract is what the client trusts.
func withoutRowClick(t domain.TableContract) domain.TableContract {
	t.RowClick = domain.RowClickRule{Enabled: false, SummaryFields: []string{}, DetailFields: []string{}}
	return t
}

func tableP(id, title, source string, cols []string, rowParam string, pageSizes []int) domain.TableContract {
	t := table(id, title, source, cols, rowParam)
	t.PageSizeOptions = pageSizes
	return t
}

// sortable marks the named columns as sortable in the compiled contract.
//
// Sort semantics are backend-owned like every other table label: the renderer draws a sort
// affordance only where the contract declares one, so a column the operator must NOT reorder --
// a composite cell, or a value whose order would read as business truth the page cannot back --
// stays inert without a frontend conditional. Naming a key the table does not declare panics at
// compile time rather than silently rendering nothing.
func sortable(t domain.TableContract, keys ...string) domain.TableContract {
	for _, key := range keys {
		found := false
		for i := range t.Columns {
			if t.Columns[i].Key == key {
				t.Columns[i].Sortable = true
				found = true
				break
			}
		}
		if !found {
			panic(fmt.Sprintf("adminui: table %q has no column %q to mark sortable", t.ID, key))
		}
	}
	return t
}

// loadwiseTable builds the load-wise reconciliation table, shared by Purchase and Born and Sales
// Config (both render the same rows for different reasons -- one to read them, one to open the
// cost form).
//
// Column labels come from the page's OWN copy map, the feedPurchaseTable precedent and for the
// same reason: the farm says "Landing price / live kg", not the humanised "Landed Price Per Kg",
// and the header must not spell a field differently from the drawer cell showing the same number.
// A key with no copy entry keeps the humanised default rather than rendering blank.
func loadwiseTable() domain.TableContract {
	t := tableP("sales-loadwise", "Load by load", "/procurement/loadwise-sales",
		[]string{"load", "farm", "purchased", "sold", "mortality", "remaining", "unaccounted", "purchase_value", "landed_price_per_kg", "sold_value", "profit_loss"},
		"load_id", []int{60})
	copy := pageCopy("sales-loads")
	for i := range t.Columns {
		if label := strings.TrimSpace(copy["column."+t.Columns[i].Key]); label != "" {
			t.Columns[i].Label = label
		}
	}
	return t
}

// buyerAnalyticsTable builds the buyer analytics table contract. Labels come from the page's own
// copy map, the loadwiseTable shape, because the farm says "Purchases so far" and "Comes back",
// not "Purchases" and "Repeat"; the header and any detail cell read ONE source.
//
// The columns are the RENDERED columns: category and place ride under the buyer's name and the
// revenue share under the revenue, so they are cell detail rather than columns of their own. Every
// column except the phone is sortable (maintainer request 2026-09-16, the pens-table shape): the
// sort reorders the served page only, the backend's own order (newest last sale first) is the
// default, and a whole-result reorder would need a backend sort parameter that does not exist.
func buyerAnalyticsTable() domain.TableContract {
	t := tableP("sales-buyer-analytics", "Buyers", "/procurement/buyer-analytics",
		[]string{"buyer_name", "phone_number", "purchases", "animals", "revenue", "repeat", "first_sale_date", "last_sale_date", "outstanding"},
		"", []int{25, 50, 100})
	copy := pageCopy("sales-buyer-analytics")
	for i := range t.Columns {
		if label := strings.TrimSpace(copy["column."+t.Columns[i].Key]); label != "" {
			t.Columns[i].Label = label
		}
	}
	return sortable(t, "buyer_name", "purchases", "animals", "revenue", "repeat", "first_sale_date", "last_sale_date", "outstanding")
}

// farmBornSoldTable is the sold-animal ledger on /sales/farm-born: one row per animal sold in the
// period. Labels come from the page's OWN copy map, the buyerAnalyticsTable shape, so the header
// and any detail cell read one source. Every column sorts (the served page only, the backend's
// newest-sale-first order is the default).
func farmBornSoldTable() domain.TableContract {
	t := tableP("sales-farm-born-sold", "Sold animals", "/procurement/farm-born-sales",
		[]string{"tag", "breed", "sex", "stage", "pen", "sale_date", "buyer_name", "sale_value"},
		"", []int{25, 50, 100})
	copy := pageCopy("sales-farm-born")
	for i := range t.Columns {
		if label := strings.TrimSpace(copy["column."+t.Columns[i].Key]); label != "" {
			t.Columns[i].Label = label
		}
	}
	return sortable(t, "tag", "breed", "sex", "stage", "pen", "sale_date", "buyer_name", "sale_value")
}

// feedPurchaseTable builds the feed purchase ledger's table contract.
//
// Column labels are taken from the page's OWN copy map rather than left to humanLabel, because the
// farm says "Bought on" and "Landed cost", not "Purchase Date" and "Total Cost". Reading them from
// pageCopy keeps ONE source: the header and the drawer's detail cells cannot drift into two
// spellings of the same field. A key with no copy entry keeps the humanised default rather than
// rendering blank.
// animalPurchaseLoadTable is the loads list on /procurement/animal-purchases; labels come from the
// page's own copy map, the feedPurchaseTable shape.
func animalPurchaseLoadTable() domain.TableContract {
	t := tableP("animal-purchase-loads", "Loads", "/app/procurement/animal-purchases/loads",
		[]string{"load_ref", "vendor_name", "farm", "expected_count", "total", "pending", "accepted", "rejected", "created_at"},
		"load_id", []int{20, 50, 100})
	copy := pageCopy("animal-purchases")
	for i := range t.Columns {
		if label := strings.TrimSpace(copy["column."+t.Columns[i].Key]); label != "" {
			t.Columns[i].Label = label
		}
	}
	return t
}

// animalPurchaseAnimalTable is the per-animal review list (one video per row, Accept / Reject).
func animalPurchaseAnimalTable() domain.TableContract {
	t := tableP("animal-purchase-animals", "Animals", "/procurement/animal-purchases/review",
		[]string{"seq_no", "species", "sex", "breed", "age_months", "weight_kg", "condition", "temp_tag", "notes", "decision"},
		// Ten animals a page (maintainer 2026-09-14): every card carries its captures and the
		// whole SOP, so twenty made the review a long scroll.
		"candidate_id", []int{10, 20, 50})
	copy := pageCopy("animal-purchases")
	for i := range t.Columns {
		if label := strings.TrimSpace(copy["column."+t.Columns[i].Key]); label != "" {
			t.Columns[i].Label = label
		}
	}
	return t
}

func feedPurchaseTable() domain.TableContract {
	t := tableP("feed-purchases", "Purchases", "/procurement/feed-purchases",
		[]string{"purchase_date", "farm", "feed_item", "batch_no", "quantity_kg", "delivery_status", "total_cost", "per_kg_cost", "vendor", "payment_status", "payment_balance"},
		"purchase_id", []int{25, 50, 100})
	copy := pageCopy("feed-purchases")
	for i := range t.Columns {
		if label := strings.TrimSpace(copy["column."+t.Columns[i].Key]); label != "" {
			t.Columns[i].Label = label
		}
	}
	return t
}

// weightsGainThresholdTable builds the breed-wise daily gain table on /weighing/weights.
//
// Column labels come from the page's OWN copy map rather than humanLabel, because the bands
// are farm figures ("200-250 g/day"), not humanised field keys ("Band 200 250"). Same
// single-source reasoning as feedPurchaseTable: the header and the cells cannot drift.
// feedWeightBandTable builds the Weight-wise tab's feed table. Column labels come from the
// page's own copy map (the loadwiseTable shape), because "Wt n" and "Pen kg/day" are the
// farm's short headings and the humanised key would spell them differently from the caption.
func feedWeightBandTable() domain.TableContract {
	t := withoutRowClick(tableP("feed-weight-band", "Feed by weight band", "/growth-director/feed-by-weight-band",
		[]string{"park", "weight_source", "band", "pen", "group", "gender", "breed", "feed_type", "feed_given", "pen_kg_per_day", "weight_animals", "average_weight"},
		"", []int{10, 25, 50}))
	copy := pageCopy("weighing-analytics")
	for i := range t.Columns {
		if label := strings.TrimSpace(copy["column.feed_band."+t.Columns[i].Key]); label != "" {
			t.Columns[i].Label = label
		}
	}
	return t
}

// feedWeightBandUnmatchedTable is the card's "Not shown" view: the feed rollups on the latest
// sheet whose pen has no qualifying weighing in the period. Same source read, same shell and
// pager as the matched view.
func feedWeightBandUnmatchedTable() domain.TableContract {
	t := withoutRowClick(tableP("feed-weight-band-unmatched", "Not shown", "/growth-director/feed-by-weight-band",
		[]string{"park", "pen", "shed_tag", "ration", "breed", "feed_type", "feed_given", "pen_kg_per_day"},
		"", []int{10, 25, 50}))
	copy := pageCopy("weighing-analytics")
	for i := range t.Columns {
		if label := strings.TrimSpace(copy["column.feed_band_unmatched."+t.Columns[i].Key]); label != "" {
			t.Columns[i].Label = label
		}
	}
	return t
}

// feedWeightBandExitsTable is the card's exited panel: every animal that exited the register
// inside the period (sold, died or other), with its last weigh in it and what its last pen is fed today.
func feedWeightBandExitsTable() domain.TableContract {
	t := withoutRowClick(tableP("feed-weight-band-exits", "Exited in period", "/growth-director/feed-by-weight-band",
		[]string{"park", "tag", "pen", "gender", "reason", "exited_at", "last_weighed", "last_band", "last_kg", "feed_type", "feed_given"},
		"", []int{10, 25, 50}))
	copy := pageCopy("weighing-analytics")
	for i := range t.Columns {
		if label := strings.TrimSpace(copy["column.feed_band_exits."+t.Columns[i].Key]); label != "" {
			t.Columns[i].Label = label
		}
	}
	return t
}

func weightsGainThresholdTable() domain.TableContract {
	t := withoutRowClick(tableP("gain-thresholds", "Breed-wise daily gain", "/weighing/weight-demographics",
		[]string{"breed", "gain_animals", "above_250", "band_200_250", "band_180_200", "upto_180"}, "", []int{10, 25, 50}))
	copy := pageCopy("weighing-weights")
	for i := range t.Columns {
		if label := strings.TrimSpace(copy["column."+t.Columns[i].Key]); label != "" {
			t.Columns[i].Label = label
		}
	}
	return t
}

func vaccinationShedTable() domain.TableContract {
	t := tableP("shed-summary", "Vaccination by pen", "/vaccination/sheds", []string{"park", "shed", "animals", "due", "done", "sessions", "next_due", "manager", "backup", "status"}, "shed", []int{25, 50, 100})
	for i := range t.Columns {
		switch t.Columns[i].Key {
		case "due":
			t.Columns[i].Label = "Needs action"
		case "done":
			t.Columns[i].Label = "Up to date"
		}
	}
	return t
}

func table(id, title, source string, cols []string, rowParam string) domain.TableContract {
	columns := make([]domain.Column, 0, len(cols))
	for _, col := range cols {
		columns = append(columns, domain.Column{Key: col, Label: humanLabel(col), Visible: true})
	}
	return domain.TableContract{
		ID: id, Title: title, DataSource: source, Columns: columns,
		Filters: []domain.Filter{
			{Key: "search", Label: "Search", Kind: "search", Enabled: true},
			{Key: "filters", Label: "Filters", Kind: "drawer", Enabled: true},
		},
		SortKeys:        []domain.SortKey{},
		PageSizeOptions: []int{5, 10, 25, 50},
		RowClick: domain.RowClickRule{
			Enabled: true, Param: rowParam, TargetDrawer: "record",
			SummaryFields: []string{"id", "title", "status", "owner", "next_action"},
			DetailFields:  []string{"contract_object"},
		},
		SummaryFields: []string{"id", "title", "status", "owner", "next_action"},
		DetailFields:  []string{"contract_object"},
	}
}

// weighingWeightsCopy is every visible string on /weighing/weights, and the SHARED base for
// /weighing/analytics beside it. ONE map rather than two on purpose: the two screens carry the
// same filter bar, the same KPI strip and the same shed table, and a second copy of those
// labels would let one screen say "Per animal" while the other said "Individual".
func weighingWeightsCopy() map[string]string {
	return map[string]string{
		"crumb":                      "Weighing",
		"filter.park.label":          "Park",
		"filter.park.all":            "All parks",
		"filter.weighing.label":      "Weighing",
		"filter.weighing.all":        "All",
		"filter.weighing.individual": "Per animal",
		"filter.weighing.lump":       "Lump sum",
		// The Assumptions drawer (maintainer decision 2026-09-19): every figure the Weighing
		// pages are valued and judged against, edited by whoever holds the Configure tick.
		"action.assumptions":                            "Assumptions",
		"disabled.assumptions":                          "Your access can read the Weighing figures but not change them.",
		"drawer.assumptions.title":                      "Assumptions",
		"drawer.assumptions.caption":                    "Figures the Weighing pages are valued and judged against. A change applies the next time a page loads, whatever period the page is showing.",
		"drawer.assumptions.prices.title":               "Live-weight sale price",
		"drawer.assumptions.prices.hint":                "₹ per kg live weight, used to value weight gained (FCR tab) and stock on hand (Load-wise tab).",
		"drawer.assumptions.prices.default":             "All stages",
		"drawer.assumptions.prices.by_stage":            "By stage and sex",
		"drawer.assumptions.prices.by_stage.hint":       "Only stages with weighed animals in them are listed. Leave a box blank to use the all-stages price above. Each animal is valued at its own stage and sex.",
		"drawer.assumptions.prices.stage":               "Stage",
		"drawer.assumptions.prices.male":                "Male",
		"drawer.assumptions.prices.female":              "Female",
		"drawer.assumptions.prices.overrides":           "set",
		"drawer.assumptions.values.title":               "Sale lines & alerts",
		"assumption.goat.label":                         "Goat",
		"assumption.sheep.label":                        "Sheep",
		"assumption.sale_ready_threshold_kg.label":      "Sale-ready weight",
		"assumption.sale_ready_threshold_kg.hint":       "The 'Over N kg' line on Farm value and the Weights cards.",
		"assumption.load_age_alert_days.label":          "Load age alert",
		"assumption.load_age_alert_days.hint":           "Days after purchase at which a load still holding animals is flagged to the CXO every morning.",
		"drawer.assumptions.rupees":                     "₹",
		"drawer.assumptions.set_by":                     "set by",
		"drawer.assumptions.save":                       "Save",
		"drawer.assumptions.cancel":                     "Cancel",
		"drawer.assumptions.saved":                      "Saved. The pages now use these figures.",
		"drawer.assumptions.error":                      "Could not save these figures.",
		"drawer.assumptions.conflict":                   "Someone else changed these figures. Reload and try again.",
		"assumption.sale_ready_lower_kg.label":          "Lower sale line",
		"assumption.sale_ready_lower_kg.hint":           "The 'Over N kg' lower count on the Weights cards.",
		"assumption.slow_growth_target_g_per_day.label": "Slow-growth target",
		"assumption.slow_growth_target_g_per_day.hint":  "A pen whose daily gain sits below this is listed as slow growth.",
		"assumption.bad_scan_loss_g_per_day.label":      "Bad-scan cut-off",
		"assumption.bad_scan_loss_g_per_day.hint":       "A kid that appears to lose more than this per day between two weighings is treated as a bad scan and left out of growth.",
		"assumption.default_period_days.label":          "Default period",
		"assumption.default_period_days.hint":           "How many days the growth widgets cover when no dates are chosen.",
		"assumption.weight_band_edges_kg.label":         "Weight bands",
		"assumption.weight_band_edges_kg.hint":          "The band edges, lowest to highest, for the band board, the Weight-wise tab and FCR by weight band. Comma-separated.",
		"drawer.assumptions.growth.title":               "Growth",
		"drawer.assumptions.window.title":               "Growth widgets",
		// The window is picked from a CALENDAR (maintainer, 2026-08-12), landing on the 15 days
		// before today (30 until 2026-08-24, then briefly 7 the same day). `filter.period.4w` / `.12w` and the `weighing_period` option group went
		// with the fixed-window select they labelled: two preset spans could only answer the two
		// questions someone thought of in advance, and a reader comparing one drive week against
		// another had no way to ask.
		"filter.period.label":            "Period",
		"filter.period.today":            "Today",
		"filter.period.single":           "Single day",
		"filter.period.range":            "Date range",
		"filter.period.aria":             "Choose which weighing days the page reports on",
		"filter.period.previous_month":   "Previous month",
		"filter.period.next_month":       "Next month",
		"filter.period.range_start_hint": "Pick the first day of the range.",
		"filter.period.range_end_hint":   "Now pick the last day of the range.",
		"filter.period.range_separator":  "to",
		"filter.period.lump_marker_hint": "lump-sum weighing done",
		// Download drawer (maintainer request 2026-08-21): whole-window export in the
		// operations Weight-check sheet's own shape, minus its video-link column. The
		// drawer's calendar reuses the filter.period.* labels above.
		"export.button":        "Download",
		"export.eyebrow":       "Weights",
		"export.title":         "Download weights",
		"export.hint":          "Pick the days, park and pens to include. The file follows the Weight check sheet, without the video column.",
		"export.period.label":  "Period",
		"export.park.label":    "Park",
		"export.park.all":      "All parks",
		"export.sheds.label":   "Pens",
		"export.sheds.all":     "All pens",
		"export.download":      "Download CSV",
		"export.preparing":     "Preparing the file…",
		"export.error":         "The file could not be prepared. Try again in a moment.",
		"export.empty":         "Nothing was weighed for this selection. The file has only the header row.",
		"kpi.kids.label":       "Kids weighed",
		"kpi.kids.sub":         "in the selected period",
		"kpi.kids.split.label": "Individual · Lump-sum",
		// SAY WHICH KIDS. This strip counts only the kids with a SECOND weigh to compare
		// against — it is the population every gain figure beside it is computed from — while
		// Road to sale further down counts every kid weighed at all. The two therefore differ
		// by the kids weighed once, and while this label read "total in the selected period"
		// the screen showed two nearly identical sentences over two different denominators
		// with nothing to tell them apart. The maintainer read them side by side and asked why
		// they disagreed, which is exactly the question a label like that produces.
		"kpi.kids.split.total_sub":  "weighed twice in the selected period",
		"kpi.total.label":           "Total weight",
		"kpi.total.sub":             "of the kids actually weighed",
		"kpi.average.label":         "Average weight",
		"kpi.average.sub":           "per kid, across every pen",
		"kpi.over30.label":          "Over {kg} kg",
		"kpi.over35.label":          "Over {kg} kg",
		"kpi.threshold.basis":       "weighed · a whole pen counts at its average",
		"kpi.sheds.label":           "Pens weighed",
		"chart.average.title":       "Average weight by pen",
		"chart.average.caption":     "Heaviest first. Scroll for the rest.",
		"chart.average.aria":        "Average weight for each pen",
		"section.sheds.title":       "Pens",
		"section.sheds.aria":        "Weight by pen",
		"composition.unknown_breed": "Unknown breed",
		"composition.unknown_sex":   "unknown sex",
		// Column headers come from the table contract's own columns via tableLabels(),
		// so they are deliberately NOT duplicated here.
		"filter.all_option":          "All",
		"filter.bar_aria":            "Filter pens",
		"filter.clear_all":           "Clear filters",
		"pager.noun":                 "pen",
		"value.weighing.individual":  "Per animal",
		"value.weighing.lump":        "Lump sum",
		"value.workflow.completed":   "Done",
		"value.workflow.closed":      "Closed",
		"value.workflow.pending":     "Pending",
		"value.workflow.open":        "Open",
		"value.workflow.in_progress": "In progress",
		"value.workflow.rework":      "Needs fix",
		"value.workflow.rejected":    "Rejected",
		"value.never_weighed":        "Not weighed yet",
		"empty.no_data.title":        "No data available",
		"empty.no_data.body":         "No pen was weighed in this period. Try a longer period or another park.",
		"empty.filtered.title":       "No data available",
		"empty.filtered.body":        "No pen matches these filters.",
		"note.total_weight":          "Total weight covers the kids actually weighed. Weighing is free flow, so it is not the whole pen.",
		"note.threshold_basis":       "Over 30 kg and over 35 kg count every kid weighed. A pen weighed as one total counts whole at its average: all of its kids clear the mark when the average does, and none when it does not.",
		"section.losing.title":       "Kids losing weight",
		"section.losing.aria":        "Kids losing weight",
		"section.losing.caption":     "Latest weigh lower than the one before it.",
		"pager.losing_noun":          "kid",
		// `kpi.gain.label` / `kpi.gain.sub` went with the sixth headline card (maintainer,
		// 2026-08-12): it restated the "All parks — daily gain" card below it — same number, same
		// denominator, same sub-line — and cost a sixth of the headline row to say it twice. The
		// gain row is now unconditional, so the figure is still on the page in every scope.
		"kpi.gain.none":          "needs a second weigh",
		"kpi.gain.blended":       "kids weighed",
		"kpi.park_gain.suffix":   "— daily gain",
		"kpi.park_gain.all":      "All parks",
		"section.park_gain.aria": "Daily gain by park",
		"chart.gain.title":       "Daily gain and pen average change",
		"chart.gain.caption":     "Kids weighed one by one, and pens weighed as one total shown by how fast their average is moving.",
		"chart.gain.aria":        "Daily gain and pen average change",
		"empty.gain.body":        "A kid has to be weighed twice before a gain can be worked out.",
		// Distinct from the above: these sheds DO have a second weigh, they are just all
		// losing. Reusing the "needs a second weigh" line there would be a lie.
		"empty.gain.all_losing":   "Every pen with a second weigh is losing weight, so there is nothing to plot. The kids are listed below.",
		"chart.gain.caption_shed": "Same-animal rows show daily gain. Lump-sum rows show average weight change for that exact pen; shifts, sales, deaths, or new animals can also move it.",
		// The same card reads as a CHART or as the exact figures (maintainer request
		// 2026-09-01), the same shape the breed-wise gain card already carries. The choice
		// lives in the URL like every other toggle on this page, so it survives a reload and
		// travels in a shared link. Column headers are the card's own copy: this card is a
		// chart-first surface with no table contract to draw them from.
		"section.shed_gain.view_aria": "Show the pen figures as a chart or a table",
		"table.shed_gain.park":        "Park",
		"table.shed_gain.shed":        "Pen",
		"table.shed_gain.basis":       "Basis",
		// Breed, sex and head count were read out of the shed label, where a mixed pen
		// pushed them past a hundred characters (maintainer request 2026-09-01). They are
		// their own columns now; the shed cell carries only the pen name. A pen holding
		// more than one cohort lists each on its own line across the three columns, and
		// the gain stays on the ROW -- a shed average is never split across breed or sex.
		// Chips under the pen name were tried instead and turned down: this card is read
		// column by column, and a chip list cannot be scanned down for one breed.
		"table.shed_gain.breed":        "Breed",
		"table.shed_gain.sex":          "Gender",
		"table.shed_gain.count":        "Count",
		"table.shed_gain.gain":         "Daily gain",
		"table.shed_gain.weight":       "Average weight",
		"section.demographics.title":   "Breed, sex and stage",
		"section.demographics.aria":    "Weight by breed, sex and stage",
		"section.demographics.caption": "Daily gain counts kids weighed twice and pens weighed as one total. Weight uses the latest weighed animals.",
		"chart.breed.title":            "Average weight by breed",
		"chart.breed.title_gain":       "Daily gain by breed",
		"chart.breed.aria":             "Average weight for each breed",
		"chart.sex.title":              "Average weight by sex",
		"chart.sex.title_gain":         "Daily gain by sex",
		"chart.sex.aria":               "Average weight for male and female",
		"chart.stage.title":            "Average weight by stage",
		"chart.stage.title_gain":       "Daily gain by stage",
		"chart.stage.aria":             "Average weight for each management stage",
		"empty.demographics.body":      "No weighed kid could be matched to the herd register in this period.",
		"note.demographics.coverage":   "Daily gain by breed, sex and stage counts animals weighed one by one, plus whole-pen weighs: every animal of a pen weighed as one total is counted at that pen's own average change.",
		// Row 2b -- how many kids of each breed are actually growing well, which a breed
		// median cannot say. The bands are DISJOINT (maintainer, 2026-08-24): a kid at
		// 260 g/day is counted in the top band ONLY, so the four columns add up to the
		// kids weighed twice and the slowest kids finally have a column of their own.
		"section.gain_thresholds.title": "Breed-wise daily gain",
		// The card opens as a CHART and can be switched to the exact figures. Both views
		// are the same numbers; the toggle is a reading preference, so it lives in the URL
		// like every other toggle on this page and survives a reload or a shared link.
		"view.chart":                        "Chart",
		"view.table":                        "Table",
		"section.gain_thresholds.view_aria": "Show the gain marks as a chart or a table",
		"chart.gain_thresholds.aria":        "Share of each breed in each daily gain band",
		// Farm nouns for the head count under a breed and the hover line behind a bar.
		"value.gain_thresholds.kids":      "kids",
		"value.gain_thresholds.of":        "of",
		"section.gain_thresholds.aria":    "Breed-wise daily gain",
		"section.gain_thresholds.caption": "Counted from kids weighed twice, at each kid's own daily gain, plus pens weighed as one total, whose kids all sit in the band that pen's average movement falls in. Each kid is counted in one band only.",
		"empty.gain_thresholds.body":      "No kid matched to a breed has a second weigh in this period yet.",
		// The page carries a SEX filter in its own filter bar, beside Weighing (maintainer,
		// 2026-08-26). It auto-selects every kid, and picking a side re-reads the WHOLE page at
		// that half: every KPI, the shed table, both leaderboards, the load chart and this card.
		// A page whose cards disagreed about which kids they counted would have no true number
		// on it, which is why this is a page filter and not a card control.
		"filter.sex.label": "Sex",
		"view.sex.male":    "Male",
		"view.sex.female":  "Female",
		// And an ORIGIN filter beside it (maintainer, 2026-09-01), on exactly the same terms:
		// the farm both breeds its own kids and buys them in loads, and the two grow
		// differently enough that reading them together answers nothing. It is a fact about the
		// PEN a load was put into, so a pen's whole-shed weighs and its scanned weighs always
		// land on the same side of this filter.
		"filter.origin.label":   "Origin",
		"view.origin.farm_born": "Farm born",
		"view.origin.purchased": "Purchased",
		// One caption per grain, because the denominator sentence has to name the kids it
		// actually counted. Reusing the combined caption under the male view would tell a
		// reader the bands add up to the kids weighed twice when they add up to the MALE kids
		// weighed twice.
		"section.gain_thresholds.caption_male":   "Counted from male kids weighed twice, at each kid's own daily gain, plus all-male pens weighed as one total, whose kids all sit in the band that pen's average movement falls in. Each kid is counted in one band only.",
		"section.gain_thresholds.caption_female": "Counted from female kids weighed twice, at each kid's own daily gain, plus all-female pens weighed as one total, whose kids all sit in the band that pen's average movement falls in. Each kid is counted in one band only.",
		"value.gain_thresholds.kids_male":        "male kids",
		"value.gain_thresholds.kids_female":      "female kids",
		"empty.gain_thresholds.male":             "No male kid matched to a breed has a second weigh in this period yet.",
		"empty.gain_thresholds.female":           "No female kid matched to a breed has a second weigh in this period yet.",
		"column.breed":                           "Breed",
		"column.gain_animals":                    "Kids weighed twice",
		"column.above_250":                       "Above 250 g/day",
		"column.band_200_250":                    "200-250 g/day",
		"column.band_180_200":                    "180-200 g/day",
		"column.upto_180":                        "180 g/day or less",
		"chart.load.title":                       "Daily gain by load",
		"chart.load.title_weight":                "Average weight by load",
		"chart.load.aria":                        "Growth for each purchase load",
		"chart.load.caption":                     "Kids are bought in loads from a supplier and put into pens. This is how each load's pens are moving, so a supplier's stock can be judged on how it grows.",
		"empty.load.body":                        "No load has a weighed pen yet. A load shows up here once the pens it went into have been weighed.",
		"note.load.unmapped":                     "pens are not counted here — they have no load recorded, or they hold more than one load and a single pen average cannot be split between two suppliers.",
		// The load chart says a supplier's stock is growing; this says WHERE. Without
		// it a reader cannot walk from a load bar to the shed table below it.
		"section.load_placements.title":   "Where each load sits",
		"section.load_placements.aria":    "Parks and pens each purchase load was placed into",
		"section.load_placements.caption": "The park and pen each load's weighed animals are in, with the head count at that pen's latest weigh. The counts add up to the load's own animal total, so this and the chart above always agree.",
		"empty.load_placements.body":      "No load has a weighed pen yet, so there is nowhere to point to.",
		"metric.weight":                   "Weight",
		"metric.gain":                     "Daily gain",
		"empty.metric.no_gain":            "No daily gain here yet — a kid has to be weighed twice before a gain exists.",
		"empty.losing.title":              "No data available",
		"empty.losing.body":               "A kid has to be weighed twice before a loss can be seen. Only a handful have a second weigh so far.",
		"note.no_cadence":                 "There is no weighing schedule, so a pen with no recent weigh is not late.",
		"error.load.title":                "Weights could not be loaded",
		"error.load.body":                 "Try again in a moment.",
		// Growth Director section. Same copy firewall as the rest of this
		// page: farm language, honest denominators (every count is kids or
		// scans actually seen — there is no expected roster, so nothing here
		// may read "of expected"), estimates labelled as estimates.
		"growth_director.section.title":       "Growth Director",
		"growth_director.section.aria":        "Growth Director widgets",
		"growth_director.error.title":         "Growth Director could not be loaded",
		"growth_director.error.body":          "The rest of the page still works. Try again in a moment.",
		"growth_director.period.note":         "Weighing weeks that overlap the period are counted in full; feed uses the exact days.",
		"growth_director.road.title":          "Road to sale weight",
		"growth_director.road.caption":        "Where every kid sits on the way to 30 kg, counted from each kid's latest weigh.",
		"growth_director.road.identities.sub": "kids weighed in this period",
		"growth_director.road.matched.sub":    "matched to the herd register",
		// Whole-shed pens joined this board on 2026-09-01. The tile names the pens as well as
		// the animals, because "379 weighed as whole pens" and "379 weighed one by one" are
		// very different evidence and the reader is owed the difference at a glance.
		"growth_director.road.lump.sub": "of them weighed as whole pens",
		// THE DENOMINATOR OF THE THREE TILES AFTER IT. Band movement needs a previous weigh to
		// compare against, so it counts a strictly smaller population than the head count two
		// tiles left — and without this tile the three movement figures added up to a number the
		// card never showed, which reads as an error. The maintainer checked the arithmetic and
		// asked why (2026-09-01). Now moved up + held + slipped back equals this exactly.
		"growth_director.road.pairs.sub":      "of them weighed twice, so able to move a band",
		"growth_director.road.moved_up":       "moved up a band since their last weigh",
		"growth_director.road.held":           "held their band",
		"growth_director.road.moved_down":     "slipped back",
		"growth_director.road.sale_marker":    "sale",
		"growth_director.road.note.pairs":     "A kid has to be weighed twice before it can move a band.",
		"growth_director.road.note.unmatched": "Tags that match nothing in the herd register still count — a scale reading is a scale reading — and are shown as unmatched.",
		// The caption has to say what a penned kid's weight actually is, because the bars do not
		// look any different for one. A pen gives one average for every animal in it, so those
		// kids all sit in the same band and all move together.
		"growth_director.road.note.lump":       "A pen weighed as one total gives every kid in it the same weight — the pen average — so those kids share a band and move bands together.",
		"growth_director.fair_fight.title":     "Fair fight — same breed, same sex",
		"growth_director.fair_fight.caption":   "Same breed, same sex, different pens — a fairer comparison that points at pen-level causes.",
		"growth_director.fair_fight.note":      "A cohort shows once the same kind of kid, weighed twice, lives in two pens. Sex comes from the herd register, never from the pen name.",
		"growth_director.fair_fight.empty":     "No cohort yet — it takes two pens each holding three kids of the same breed and sex with a second weigh.",
		"growth_director.fair_fight.pair_noun": "kids",
		// The board reads as a standings table, so it needs the words a standings table
		// uses. `spread` is the one that carries the decision: a cohort whose sheds are
		// all within a few grams is not worth a walk, and one with a wide spread is --
		// which is exactly the judgement the old watchlist tried to make FOR the reader
		// with a status chip, on a narrower basis.
		"growth_director.fair_fight.leader":        "Ahead",
		"growth_director.fair_fight.behind":        "Behind",
		"growth_director.fair_fight.spread":        "Spread, best to last",
		"growth_director.fair_fight.shed_noun":     "pens",
		"growth_director.fair_fight.rank_label":    "Position in cohort",
		"growth_director.slow.title":               "Slow-growth watchlist",
		"growth_director.slow.caption":             "Groups of kids that are not gaining — worth a walk to the pen. Same breed and sex grouped together, so it points at a pen problem, not one sick kid.",
		"growth_director.slow.note":                "Target ~200 g/day is the ops rule of thumb, not a contract. Changes within 3% of body weight count as gut fill; losses over 0.30 kg/day are treated as bad scans, not slow growth. Small groups stay hidden until 3 kids have a second weigh.",
		"growth_director.slow.col.shed":            "Pen",
		"growth_director.slow.col.breed":           "Breed",
		"growth_director.slow.col.sex":             "Sex",
		"growth_director.slow.col.pairs":           "Kids weighed twice",
		"growth_director.slow.col.median":          "Median daily gain",
		"growth_director.slow.col.wow":             "vs last week",
		"growth_director.slow.col.status":          "Status",
		"growth_director.slow.status.on_track":     "On track",
		"growth_director.slow.status.below_target": "Below target",
		"growth_director.slow.status.losing":       "Losing",
		"growth_director.slow.wow.none":            "needs two weeks of weighing",
		"growth_director.slow.empty":               "No group has three kids with a second weigh yet.",
		"growth_director.feed_growth.title":        "Feed given vs growth",
		"growth_director.feed_growth.caption":      "Feed as directed on the sheet, not as eaten — leftovers are not measured yet. Read this as an estimate.",
		"growth_director.feed_growth.estimate":     "estimate",
		// Filters for a table that is now ONE ROW PER PEN (105 live) rather than per shed.
		// "Measured gain" is the one that earns its place: 78 of those pens have no second
		// weigh yet, so the default view buries the 27 rows a reader can actually act on.
		"growth_director.feed_growth.filter.all":       "All pens",
		"growth_director.feed_growth.filter.measured":  "With measured gain",
		"growth_director.feed_growth.filter.trial":     "Trial pens",
		"growth_director.feed_growth.filter.aria":      "Filter the feed and growth rows",
		"growth_director.feed_growth.filter.empty":     "No pens match this filter in the selected window.",
		"growth_director.feed_growth.showing":          "pens shown",
		"growth_director.feed_growth.col.shed":         "Pen",
		"growth_director.feed_growth.col.feed":         "Feed directed",
		"growth_director.feed_growth.col.gain":         "Daily gain",
		"growth_director.feed_growth.col.ratio":        "Feed kg / kg gained",
		"growth_director.feed_growth.feed_unit":        "g per head per day",
		"growth_director.feed_growth.basis.per_animal": "per kid, own weighs",
		"growth_director.feed_growth.basis.whole_shed": "pen average movement",
		"growth_director.feed_growth.experiment":       "trial",
		"growth_director.feed_growth.experiment.note":  "Runs the experiment sheet (its own hand-entered quantities, not the ration grid), so its ratio is not comparable.",
		"growth_director.feed_growth.no_gain":          "needs a second weigh",
		"growth_director.feed_growth.note":             "High feed with low gain is a ration, waste, or health question for that pen this week. Growth is counted by each kid's herd-register pen — the pen the feed sheet was written for — so kids whose tag matches nothing are left out here and counted in the trust panel.",
		"growth_director.feed_growth.empty":            "No feed sheet covered these pens in this period.",
		"growth_director.feed_problems.title":          "Feed sheet problems",
		"growth_director.feed_problems.caption":        "Lines the feed sheet could not fill in — the pen may have been fed by guesswork.",
		"growth_director.feed_problems.col.shed":       "Pen",
		"growth_director.feed_problems.col.item":       "Feed item",
		"growth_director.feed_problems.col.days":       "Days blocked",
		"growth_director.feed_problems.col.reason":     "Reason",
		"growth_director.feed_problems.latest_day":     "on the latest feed day",
		"growth_director.feed_problems.history":        "in this period",
		"growth_director.feed_problems.note.zero":      "Blocked is not zero — blocked means nobody authored a ration. Zero-kg lines exist on purpose (milk-fed kids) and are not shown here.",
		"growth_director.feed_problems.empty":          "Every line on the feed sheet was filled in for this period.",
		"growth_director.trust.title":                  "Can we trust these numbers?",
		"growth_director.trust.caption":                "Every gain number on this page stands on these counts. Fixing them is the cheapest way to make the whole page better.",
		"growth_director.trust.scans_matched":          "Scans matched to a kid",
		"growth_director.trust.scans_matched.sub":      "a scan that matches nothing needs a re-scan",
		"growth_director.trust.pairs":                  "Tags weighed twice",
		"growth_director.trust.pairs.sub":              "growth can only be worked out for these — a tag is only a named kid once it matches the herd register",
		"growth_director.trust.once_only":              "Tags weighed once only",
		"growth_director.trust.once_only.sub":          "a second weigh unlocks their gain",
		"growth_director.trust.whole_shed":             "Whole-pen weighings",
		"growth_director.trust.whole_shed.sub":         "no per-kid, breed or sex view",
		"growth_director.trust.pending":                "Awaiting verification",
		"growth_director.trust.pending.sub":            "still counted — a weigh is a weigh until a verifier bounces it",
		"growth_director.trust.rework":                 "Bounced by the verifier",
		"growth_director.trust.rework.sub":             "left out of every gain number on this page",
	}
}

func pageCopy(id string) map[string]string {
	copy := map[string]string{
		"action.close":                 "Close",
		"action.back":                  "Back",
		"action.clear":                 "Clear",
		"action.apply":                 "Apply",
		"action.cancel":                "Cancel",
		"action.previous":              "Previous",
		"action.next":                  "Next",
		"action.filters":               "Filters",
		"action.search":                "Search",
		"action.success_tag":           "done",
		"action.success_message":       "Action completed.",
		"action.failed_message":        "Action could not be completed.",
		"action.error_backend":         "Backend rejected the action. Review the form values or reload the page.",
		"action.error_form":            "Check the form values and try again.",
		"action.failed_title":          "Action failed",
		"pager.rows":                   "Rows",
		"pager.page":                   "Page",
		"pager.of":                     "of",
		"pager.matching_rows":          "matching rows",
		"state.loading":                "Loading",
		"state.unavailable":            "Unavailable",
		"label.placeholder":            "—",
		"filter.search_visible_rows":   "Search visible rows",
		"filter.search_visible_cards":  "Search visible cards",
		"filter.search_rows_label":     "Search rows",
		"filter.search_placeholder":    "Search rows...",
		"filter.quick_filters_aria":    "quick filters",
		"filter.available_columns":     "Available columns",
		"filter.facets_label":          "Facets",
		"filter.column_separator":      " · ",
		"filter.apply_immediately":     "Filters apply to the visible table immediately; deeper backend filters stay on the linked source surface.",
		"filter.apply_filters":         "Apply filters",
		"filter.close_label":           "Close filters",
		"filter.no_visible_match":      "No rows match the current filters.",
		"drawer.record.title_source":   "selected row",
		"drawer.record.close_label":    "Close record drawer",
		"drawer.disabled_field_action": "Read-only here: this is the computed next action. Use enabled buttons for real mutations; open Workflow record only for audit context.",
	}
	for key, value := range pageSpecificCopy(id) {
		copy[key] = value
	}
	return copy
}

func pageSpecificCopy(id string) map[string]string {
	switch id {
	case "control-tower":
		return map[string]string{
			"crumb":                            "Central Command",
			"kpi.process":                      "Process",
			"kpi.open_gaps":                    "Open gaps",
			"kpi.critical":                     "Critical",
			"kpi.evidence":                     "Evidence",
			"section.critical_alerts.title":    "Priority exceptions",
			"section.open_gaps.title":          "Open exceptions — status, owner, next action",
			"table.open_gaps.aria":             "Open exceptions",
			"table.open_gaps.noun":             "alert",
			"filter.drawer.title":              "Filter — Control Tower",
			"filter.search_reason":             "Search gap, severity, owner, pen, next action, evidence...",
			"filter.reason":                    "Use severity and gap-state chips for the Control Tower exception list; drawer search narrows visible rows.",
			"filter.search_label":              "Search Control Tower alerts",
			"filter.rows_suffix":               "alerts · severity, owner, next action",
			"filter.click_row":                 "click a row → Control Tower alert",
			"drawer.alert.aria":                "Control Tower alert",
			"drawer.alert.close_label":         "Close Control Tower alert",
			"drawer.alert.guidance":            "Control Tower is the watch surface. Open the Action Center drawer to act on this obligation, or open the workflow/adherence/module context to inspect the chain.",
			"action.open_action_center":        "Open Action Center",
			"action.open_workflow":             "Open Workflow",
			"action.open_adherence":            "Open Adherence",
			"action.open_vaccination":          "Open Vaccination",
			"action.open_alert_for":            "Open Control Tower alert for",
			"link.action_center":               "Action Center →",
			"link.protocol_adherence":          "Protocol Adherence ledger →",
			"link.workflows":                   "Workflows →",
			"link.vaccination_ops":             "Preventive Care (PC) · Vaccination ops →",
			"link.park_shed_execution":         "Park/pen execution →",
			"alert.config_sop.singular":        "plan blocker",
			"alert.config_sop.plural":          "plan blockers",
			"alert.config_sop.action_required": "— action required.",
			"alert.config_sop.body_prefix":     "Vaccination obligations and proof need a published plan. Resolve it in",
			"alert.config_sop.config_label":    "Preventive Care / Vaccination plan",
			"alert.config_sop.joiner":          "",
			"alert.config_sop.sops_label":      "",
			"alert.config_sop.body_suffix":     ".",
			"empty.open_gaps":                  "No open vaccination gaps for this scope.",
			"empty.critical_ok_title":          "No broken or at-risk vaccination process",
			"empty.critical_unavailable":       "Vaccination process status unavailable",
			"empty.critical_ok_body":           "Every vaccination obligation is on track. Open gaps appear here the moment severity rises.",
			"empty.resolve_error":              "Resolve the error above, then reload.",
			"empty.open_gaps_detail":           "No open gaps — every vaccination obligation is on track, or none has been generated yet. Publish a plan in Preventive Care / Vaccination plan to start generating obligations.",
			"empty.open_gaps_filtered":         "No Control Tower alerts match these filters.",
			"empty.open_gaps_unavailable":      "Open gaps are unavailable until the Control Tower service responds.",
			"label.critical":                   "critical",
			"label.at_risk":                    "at risk",
			"label.all_severity":               "All severity",
			"label.all_states":                 "All states",
			"label.owner_unassigned":           "operator: unassigned",
			"label.process_intact":             "Intact",
			"label.process_not_intact":         "Not intact",
			"label.process_at_risk":            "At risk",
			"label.gap":                        "Gap",
			"label.severity":                   "Severity",
			"label.detail":                     "Detail",
			"label.owner":                      "Owner",
			"label.next_action":                "Next action",
			"label.evidence":                   "Evidence",
			"label.not_ready":                  "not ready",
			"evidence.verification_pending":    "Mobile proof submitted; awaiting verifier review",
			"evidence.proof_pending":           "Mobile proof not submitted yet",
			"evidence.rejected":                "Verifier rejected the submitted proof",
			"evidence.blocked":                 "Evidence blocked by configuration or SOP issue",
			"evidence.late":                    "Required proof is late",
			"evidence.completed":               "Verifier accepted proof and closed the vaccination work",
			"evidence.scope_prefix":            "Scope",
		}
	case "action-center":
		return map[string]string{
			"crumb":                               "Command lens · vaccination",
			"section.verification.title":          "Awaiting verification",
			"section.verification.aria":           "Awaiting verification",
			"section.verification.note":           "Recorded doses awaiting review",
			"section.work_board.title":            "Vaccination work board",
			"section.work_board.aria":             "Vaccination work board",
			"view.status_board":                   "Status board",
			"view.sop_queues":                     "SOP queues",
			"filter.domains.aria":                 "Domains",
			"filter.domain.vaccination":           "Vaccination",
			"filter.all":                          "All",
			"filter.all_severity":                 "All severity",
			"filter.overdue":                      "Overdue",
			"filter.due":                          "Due",
			"filter.awaiting_verification":        "Awaiting verification",
			"filter.drawer.title":                 "Action Center filters",
			"filter.close_label":                  "Close Action Center filters",
			"filter.close_button_label":           "Close filters",
			"filter.my_tasks.title":               "My tasks",
			"filter.search_label":                 "Search action, operator, ID",
			"filter.search_placeholder":           "Search visible cards...",
			"filter.owner_placeholder":            "Filter visible cards by owner...",
			"filter.owner_label":                  "Owner / operator",
			"filter.work_state.title":             "Work state",
			"filter.severity.title":               "Severity",
			"filter.scope_note":                   "Park and date scope come from the top bar. Work-state and severity apply immediately because those filters are backed by the Action Center API.",
			"filter.clear_all":                    "Clear all",
			"filter.clear_local":                  "Clear local",
			"filter.apply_local":                  "Apply local",
			"filter.done":                         "Done",
			"filter.rows_label":                   "work item",
			"filter.cards_label":                  "cards",
			"filter.verification.search":          "Search SOP verification queue",
			"filter.verification.title":           "Filter — SOP verification queue",
			"filter.verification.search_reason":   "Search goat, administered date, dose count, and row actions...",
			"filter.verification.filter_reason":   "Use visible-row search and quick facets on this verification queue page.",
			"filter.verification.rows_suffix":     "recorded doses awaiting review",
			"section.verification.empty":          "Nothing awaiting verification. Recorded doses surface here once a published drive runs and proof is uploaded.",
			"label.drive_over_cap_required":       "capacity shortfall",
			"label.drive_medical_defer":           "medical defer",
			"label.drive_terminal_closed":         "terminal closed",
			"label.video_proof":                   "video proof",
			"label.image_proof":                   "image proof",
			"label.open_proof":                    "open proof",
			"tooltip.drive_over_cap_required":     "{animals} animals assigned against {slots} planned operator slots; leadership action needed before latest-safe date",
			"tooltip.drive_medical_defer":         "Hard medical defer blocks vaccination until cleared",
			"tooltip.drive_terminal_closed":       "Terminal animals are closed out of the vaccination cohort",
			"action.previous":                     "Previous",
			"action.next":                         "Next",
			"label.page":                          "Page",
			"table.verification_queue.noun":       "queue row",
			"drawer.work_item.aria":               "Action Center work item",
			"drawer.work_item.close_label":        "Close Action Center drawer",
			"drawer.work_item.eyebrow":            "ACTION",
			"drawer.adherence_status_label":       "Adherence status (computed)",
			"drawer.adherence_status_help":        "computed from obligation + SOP submission + proof + timing — not manually editable",
			"drawer.owner_chain_label":            "Operator assignment",
			"drawer.owner_chain_disabled":         "Operator assignment is computed from the workflow record, not edited here",
			"drawer.due_label":                    "Due",
			"drawer.due_date_disabled":            "Due date is set by the published protocol schedule",
			"drawer.priority_label":               "Priority",
			"drawer.priority_disabled":            "Priority is derived from computed severity — not manually set",
			"drawer.protocol_label":               "Protocol",
			"drawer.dose_label":                   "Dose",
			"drawer.park_shed_label":              "Park · Pen",
			"drawer.cohort_progress_label":        "Cohort · Progress",
			"drawer.sop_checklist.note":           "SOP checklist is rendered as field-task progress, not as the obligation lifecycle chain.",
			"drawer.sop_checklist.title":          "Checklist · SOP — Vaccination Drive SOP",
			"drawer.linked_title":                 "Linked",
			"drawer.link.workflow_record":         "workflow record",
			"drawer.link.adherence":               "adherence",
			"drawer.link.vaccination":             "vaccination",
			"action.open_workflow":                "Open Workflow",
			"action.record_dose":                  "Record dose",
			"action.upload_proof":                 "Upload proof",
			"action.start_sop":                    "Start SOP",
			"action.submit_proof":                 "Submit / proof",
			"action.escalate":                     "Escalate",
			"action.workflow_record":              "Workflow record",
			"action.goat_passport":                "Goat Passport",
			"action.passport":                     "Passport",
			"action.reject":                       "Reject",
			"action.verify":                       "Verify",
			"action.request_rework":               "Request rework",
			"action.verify_accepted":              "Verification accepted.",
			"action.verify_replay":                "Verification was already applied.",
			"action.rework_requested":             "Rework requested.",
			"action.completion_rejected":          "Completion rejected.",
			"action.reassign":                     "Reassign",
			"action.open_passport":                "Open Passport",
			"action.open_config":                  "Open the vaccination plan",
			"action.open_sops":                    "Open the vaccination plan",
			"action.success_tag":                  "done",
			"action.success_title":                "Done",
			"action.success_message":              "Action completed.",
			"action.failed_title":                 "Action failed",
			"action.assign_owner_chain":           "Assign operator",
			"action.capture_vaccination_proof":    "Capture vaccination proof",
			"action.verify_vaccination_proof":     "Verify vaccination proof",
			"note.board_explainer":                "Every vaccination obligation, grouped by computed work state. Open a card to inspect the computed next action, SOP/proof gates, linked workflow record, and available controls. Disabled controls are read-only until their backing workflow handle exists. Live actions write the audit trail and ripple into",
			"note.board_explainer.link_adherence": "Protocol Adherence",
			"note.board_explainer.link_joiner":    "and",
			"note.board_explainer.link_tower":     "Control Tower",
			"empty.verification":                  "No completion rows are waiting for verification.",
			"empty.work_board":                    "No Action Center work for this scope.",
			"empty.work_board_detail":             "No vaccination obligations yet — the board fills once a protocol is published and a vaccination SOP exists. Columns below show the work-state shell.",
			"empty.work_board_filtered":           "No Action Center rows match these filters.",
			"empty.unavailable":                   "Action Center is unavailable — resolve the error above, then reload.",
			"note.board_paging":                   "Board cards are sampled per bucket for this page; chip and lane counts are server-authoritative totals. Narrow by scope, severity, or work state for exact working sets.",
			"reason.no_recorded_dose_verify":      "No recorded dose to verify yet.",
			"reason.no_recorded_dose_rework":      "No recorded dose to rework yet.",
			"reason.no_sop_review_handle":         "SOP review handle required before verification can be reviewed.",
			"label.unassigned":                    "unassigned",
			"label.owner_chain_assign":            "operator: assign",
			"label.vaccination":                   "Vaccination",
			"label.vaccination_drive":             "Vaccination drive",
			"label.shed_fallback":                 "pen",
			"label.overdue_suffix":                "overdue",
			"label.done_suffix":                   "done",
			"label.due_prefix":                    "due",
			"label.next_action":                   "Next action",
			"label.open_work_item_for":            "Open Action Center work item for",
			"label.empty_placeholder":             "—",
			"label.high":                          "High",
			"label.med":                           "Med",
			"label.low":                           "Low",
		}
	case "protocol-adherence":
		return map[string]string{
			"crumb":                          "Command lens · vaccination",
			"section.ledger.title":           "Vaccination",
			"section.ledger.aria":            "Vaccination adherence ledger",
			"section.ledger.note":            "business view: what was expected, what happened, owner, next action",
			"filter.drawer.title":            "Filter — Protocol Adherence",
			"filter.search_reason":           "Search expected, actual, operator assignment, next action, evidence...",
			"filter.reason":                  "Use severity and gap-state chips for backend filters; drawer search narrows visible rows.",
			"filter.search_label":            "Search adherence rows",
			"filter.rows_suffix":             "expected, actual, gap, operator assignment, evidence",
			"filter.click_row":               "click a row → adherence record",
			"drawer.record.aria":             "Protocol adherence record",
			"drawer.record.close_label":      "Close adherence record",
			"drawer.record.eyebrow":          "RECORD",
			"drawer.record.note":             "Read view — adherence is computed from published rules vs actual SOP submission + proof. Act on the real obligation from the Workflow record or the Action Center.",
			"action.open_workflow":           "Open Workflow",
			"action.open_action_center":      "Open Action Center",
			"action.workflow_record":         "Workflow record",
			"action.open_config":             "Vaccination plan",
			"action.open_sops":               "Vaccination plan",
			"empty.ledger":                   "No adherence rows for this scope.",
			"empty.ledger_detail":            "No open vaccination adherence gaps for this scope — every obligation is on track, deferred/explained, or none has been generated yet.",
			"empty.ledger_filtered":          "No adherence rows match these filters.",
			"empty.unavailable":              "Adherence rows are unavailable until the service responds.",
			"label.all":                      "All",
			"label.all_severity":             "All severity",
			"label.all_states":               "All states",
			"label.owner_short":              "OWNER",
			"label.unassigned":               "unassigned",
			"label.vaccination_drive":        "Vaccination drive",
			"label.rejected":                 "rejected",
			"label.video_proof":              "video proof",
			"label.image_proof":              "image proof",
			"label.open_proof":               "open proof",
			"label.proof_singular":           "proof",
			"label.proof_plural":             "proofs",
			"gap.proof_missing":              "proof missing",
			"gap.verification_pending":       "verify pending",
			"gap.deferred_explained":         "deferred / explained",
			"gap.proof_rejected":             "proof rejected",
			"gap.missed":                     "missed",
			"gap.blocked":                    "blocked",
			"gap.overdue":                    "overdue",
			"gap.capacity_shortfall":         "capacity shortfall",
			"vaccine.blue_tongue":            "Blue Tongue",
			"vaccine.goat_pox":               "Goat Pox",
			"vaccine.sheep_pox":              "Sheep Pox",
			"vaccine.et_tt":                  "ET+TT",
			"vaccine.fmd":                    "FMD",
			"vaccine.ppr":                    "PPR",
			"vaccine.hs":                     "HS",
			"vaccine.generic":                "Vaccination",
			"schedule.kid_course":            "kid course",
			"schedule.adult_course":          "adult course",
			"schedule.course":                "course",
			"schedule.weeks":                 "weeks",
			"schedule.months":                "months",
			"schedule.years":                 "years",
			"actual.deferred_with_reason":    "deferred with reason",
			"actual.not_completed_yet":       "Not completed yet",
			"adherence.help.aria":            "How adherence is calculated",
			"adherence.help.completed_label": "completed",
			"adherence.help.current_prefix":  "Example: if 100 vaccinations are expected and 67 are completed correctly, adherence is 67%. Deferred sick/quarantine work is tracked separately.",
			"adherence.help.expected_label":  "expected",
			"adherence.help.formula":         "Formula: completed expected vaccinations divided by total expected vaccinations.",
			"adherence.help.loading":         "The exact percent appears after the adherence summary loads.",
			"adherence.help.title":           "What adherence means",
			"adherence.help.window_joiner":   "to",
			"adherence.help.window_prefix":   "Adherence shows how much scheduled vaccination work was completed correctly in the selected operating window.",
			"label.due_lower":                "due",
			"label.info_icon":                "i",
			"label.overall_adherence":        "Overall adherence",
			"label.open_process_gaps":        "Open process gaps",
			"label.deferred_explained":       "Deferred / explained",
			"label.on_track":                 "On-track (no action)",
			"label.on_time_correct":          "on-time + correct",
			"label.across_rules":             "across vaccination rules",
			"label.deferred_scope":           "ICU / quarantine / sick",
			"label.done_suffix":              "done",
			"label.obligations":              "obligations",
			"table.ledger.noun":              "adherence row",
			"note.computation":               "Adherence is computed from published rules vs actual SOP submission + proof. Set the process in",
			"note.computation.joiner":        "and",
			"note.computation.tail":          "; deferred/explained rows stay visible here instead of silently disappearing.",
		}
	case "workflows":
		return map[string]string{
			"crumb":                            "Command lens · vaccination",
			"section.catalog.title":            "Live vaccination workflows",
			"section.chain.title":              "Vaccination workflow chain",
			"section.chain.aria":               "Vaccination workflow chain",
			"filter.domains.aria":              "Workflow domains",
			"filter.search_label":              "Search workflow rows",
			"filter.drawer.title":              "Filter — Workflows",
			"filter.search_reason":             "Search workflow, park, pen, state...",
			"filter.reason":                    "Use visible-row search and quick facets on this workflow catalog.",
			"filter.rows_suffix":               "workflow, park, pen, state",
			"action.open_record":               "Open workflow detail",
			"action.open_action_center":        "Open Action Center",
			"action.back_to_list":              "Back to list",
			"action.assign_owner_chain":        "Assign operator",
			"action.capture_vaccination_proof": "Capture vaccination proof",
			"action.verify_vaccination_proof":  "Verify vaccination proof",
			"empty.catalog":                    "No live workflows yet. A workflow starts when a published protocol generates an obligation — publish a plan in Preventive Care / Vaccination plan.",
			"empty.unavailable":                "Live workflows are unavailable until the service responds.",
			"label.all_domains":                "Vaccination",
			"label.workflows":                  "Workflows",
			"label.active_runs":                "Active runs",
			"label.blocked_gated":              "Blocked / gated",
			"label.avg_progress":               "Avg progress",
			"label.vaccination_chain":          "vaccination chain",
			"label.scheduled_in_progress":      "scheduled + in progress",
			"label.awaiting_proof_owner":       "awaiting proof / assignment",
			"label.across_shown":               "across shown",
			"label.done":                       "done",
			"label.current":                    "current (you are here)",
			"label.next":                       "next",
			"label.pending":                    "pending",
			"label.blocked":                    "blocked (gated)",
			"label.you_are_here":               "YOU ARE HERE",
			"label.next_upper":                 "NEXT",
			"stage.config.short":               "Config",
			"stage.obligation.short":           "Obligation",
			"stage.drive.short":                "Drive",
			"stage.sop.short":                  "SOP",
			"stage.proof.short":                "Proof",
			"stage.verify.short":               "Verify",
			"stage.close.short":                "Close",
			"label.chain_note_selected":        "Selected workflow for",
			"label.chain_note_selected_tail":   "Use the drawer/action page for the next backend-gated action; the full drilldown is available when you need the complete audit chain.",
			"label.chain_note_open":            "Open a workflow on the left to see its live chain-reaction map for that drive.",
			"label.chain_note_empty":           "No live instances to map yet — this is the template every vaccination workflow follows.",
			"note.engine":                      "Every node maps to the engine: event → obligation → SOP task → verification → closure. The SOP is the step template; the engine gates each next step on verified proof. See live work in the",
			"note.paging":                      "Showing the first 200 workflows. Filter by park, or narrow in the Action Center, to see the rest.",
			"label.unassigned":                 "unassigned",
			"label.owner_chain_assign":         "operator: assign",
			"label.vaccination":                "Vaccination",
			"label.vaccination_drive":          "Vaccination drive",
			"label.shed_fallback":              "pen",
			"label.overdue_suffix":             "overdue",
			"label.done_suffix":                "done",
			"label.due_prefix":                 "due",
			"label.open_work_item_for":         "Open Action Center work item for",
			"label.empty_placeholder":          "—",
			"section.work_board.aria":          "Action Center work board",
		}
	case "workflow-record":
		return map[string]string{
			"crumb":                            "Workflows · Vaccination",
			"fallback.title":                   "Workflow drilldown",
			"section.chain.title":              "Workflow chain",
			"section.chain.next_prefix":        "next:",
			"stage.config.short":               "Config",
			"stage.obligation.short":           "Obligation",
			"stage.drive.short":                "Drive",
			"stage.sop.short":                  "SOP",
			"stage.proof.short":                "Proof",
			"stage.verify.short":               "Verify",
			"stage.close.short":                "Close",
			"action.back_workflows":            "Workflows",
			"action.back_control":              "Back to Control Tower",
			"action.back_action":               "Back to Action Center",
			"action.goat_passport":             "Goat passport",
			"action.shed_execution":            "Pen execution detail",
			"action.action_center":             "Action Center",
			"action.protocol_adherence":        "Protocol Adherence",
			"action.assign_owner_chain":        "Assign operator",
			"action.capture_vaccination_proof": "Capture vaccination proof",
			"action.verify_vaccination_proof":  "Verify vaccination proof",
			"label.done":                       "done",
			"label.unassigned":                 "unassigned",
			"label.owner_chain_assign":         "operator: assign",
			"label.vaccination":                "Vaccination",
			"label.vaccination_drive":          "Vaccination drive",
			"label.shed_fallback":              "pen",
			"label.overdue_suffix":             "overdue",
			"label.done_suffix":                "done",
			"label.due_prefix":                 "due",
			"label.open_work_item_for":         "Open Action Center work item for",
			"label.empty_placeholder":          "—",
			"section.work_board.aria":          "Action Center work board",
			"error.unavailable_prefix":         "Workflow record unavailable",
		}
	case "leave":
		return map[string]string{
			"queue.title":               "Waiting for you",
			"queue.empty":               "No leave requests are waiting for your decision.",
			"list.title":                "All leave requests",
			"list.empty":                "No leave requests yet.",
			"column.person":             "Person",
			"column.park":               "Park",
			"column.dates":              "Dates",
			"column.reason":             "Reason",
			"column.status":             "Status",
			"column.raised_at":          "Asked on",
			"column.action":             "Decision",
			"filter.status":             "Status",
			"filter.status.all":         "All",
			"filter.status.pending":     "Waiting",
			"filter.status.approved":    "Approved",
			"filter.status.rejected":    "Rejected",
			"filter.status.withdrawn":   "Withdrawn",
			"action.approve":            "Approve",
			"action.reject":             "Reject",
			"reject.reason":             "Reason for rejecting",
			"reject.reason_hint":        "Tell the person why. They will see this.",
			"feedback.approved":         "Approved. The request moves on when every approver has signed.",
			"feedback.rejected":         "Rejected. The person has been told.",
			"feedback.already_decided":  "This request was already decided.",
			"config.title":              "Who approves leave",
			"config.help":               "Every ticked approver must accept before leave is granted. Any one of them can reject.",
			"config.park_head":          "Park head of the person's park",
			"config.hr":                 "HR",
			"config.save":               "Save",
			"config.saved":              "Saved.",
			"config.disabled_no_access": "Setting who approves leave is limited to the CEO.",
			"list.disabled_no_access":   "Seeing every leave request is limited to the CEO and HR.",
		}
	case "alerts":
		return map[string]string{
			"crumb":                                "Alerts",
			"title":                                "Alerts",
			"subtitle":                             "What is off today, park by park. Each row names the pen or feed and what to check.",
			"filter.park":                          "Park",
			"filter.park.all":                      "All parks",
			"filter.date":                          "Date",
			"filter.date.today":                    "Today",
			"filter.date.previous":                 "Previous day",
			"filter.date.next":                     "Next day",
			"filter.severity":                      "Severity",
			"filter.severity.all":                  "All",
			"severity.critical":                    "Critical",
			"severity.warning":                     "Warning",
			"kpi.total":                            "Alerts today",
			"kpi.critical":                         "Critical",
			"kpi.rules":                            "Rules checked",
			"state.empty":                          "No alerts for these parks on the selected date. Every enabled rule ran clean.",
			"state.empty.incomplete":               "No alerts to show from the available checks. Some checks were not completed; this is not an all-clear.",
			"state.empty.filtered":                 "No alerts match this severity. Other alerts exist; choose All to see them.",
			"state.empty.no_rules":                 "No alert rules are switched on. Configure them to start checking.",
			"state.error":                          "Alerts could not be loaded. Try again.",
			"state.partial":                        "Some rules could not be checked:",
			"action.retry":                         "Retry",
			"action.open":                          "Open",
			"configure.title":                      "Configure alerts",
			"configure.intro":                      "Switch each rule on or off and set the number it watches. Changes apply from the next page load.",
			"configure.enabled":                    "On",
			"configure.disabled":                   "Off",
			"configure.default":                    "Default",
			"configure.save":                       "Save",
			"configure.close":                      "Close",
			"configure.updated_by":                 "Last changed by",
			"configure.unavailable":                "Alert rules could not be loaded right now.",
			"configure.disabled_no_access":         "Changing alert rules is limited to the people HRMS names for it.",
			"configure.builtin.title":              "Checks the system runs",
			"configure.events.title":               "Your alerts",
			"configure.events.intro":               "Tell the page what to show when something happens: name it, pick the event, pick how loud.",
			"configure.events.empty":               "No alerts of your own yet.",
			"configure.events.add":                 "Add an alert",
			"configure.events.add_button":          "Add",
			"configure.events.label":               "Alert name",
			"configure.events.label.placeholder":   "e.g. Kid born",
			"configure.events.when":                "when",
			"configure.events.kind":                "Event",
			"configure.events.remove":              "Remove",
			"configure.more_rules":                 "Need to watch an event that is not in the list? Ask engineering to add it to the catalog; it appears here once it ships.",
			"action.config_event_added":            "Alert added.",
			"action.config_unknown_event":          "That is not something alerts can watch yet.",
			"action.config_invalid_event":          "Give the alert a name of up to 80 characters.",
			"action.config_event_missing":          "That alert no longer exists. Reload the page.",
			"action.config_saved":                  "Rule saved.",
			"action.config_failed":                 "The rule could not be saved. Try again.",
			"action.config_threshold_out_of_range": "That number is outside the rule's range.",
			"action.config_invalid":                "Enter a whole number for the threshold.",
			"action.config_conflict":               "That change was already sent with different values. Reload and try again.",
			"pager.noun":                           "alerts",
		}
	case "leadership-tasks":
		// The Tasks page. Urgency on this page IS the deadline, so there is no priority
		// vocabulary here. The feedback.* family is the backend error vocabulary: it is an
		// OPEN key space the screen reads with an empty default, so an unrecognised code
		// falls back to the generic sentence rather than throwing.
		// `filter.search_label` and `filter.search_hint` are separate keys: at 2000px the
		// search box holds ~160px of input and the old single placeholder ("Search tasks, or
		// type a number") clipped mid-word inside it. The short form is the placeholder; the
		// number affordance is the control's title / aria-label. `filter.range_any` is what a
		// date disclosure reads with no span applied: "Deadline · any".
		return map[string]string{
			"crumb":               "Operations",
			"filter.bar_aria":     "Filter tasks",
			"filter.search_label": "Search tasks…",
			"filter.search_hint":  "Search by title, or type a task number",
			"filter.range_any":    "any",
			"filter.all_option":   "All",
			"filter.assignee":     "Assignee",
			"filter.raiser":       "Raised by",
			"filter.sort":         "Sort",
			// The ONE dates disclosure on the toolbar (both spans inside it), the Work Board's one
			// date control restated for this desk.
			"filter.dates":         "Dates",
			"filter.deadline_from": "Deadline from",
			"filter.deadline_to":   "Deadline to",
			"filter.raised_from":   "Raised from",
			"filter.raised_to":     "Raised to",
			"filter.range_note":    "Pick both ends of a date range — a half range is not applied.",
			"sort.raised_at_desc":  "Newest first",
			"sort.raised_at_asc":   "Oldest first",
			"sort.deadline_asc":    "Deadline soonest",
			"sort.deadline_desc":   "Deadline latest",
			"scope.aria":           "Task scopes",
			// Status-board column headings. They are the SAME wording as the status chips
			// (domain.StatusChip), so a column and the cards under it never name the status
			// two different ways. The board previously had no contract key for these at all,
			// and an unlabelled column fell back to the column KEY — rendering the raw wire
			// token `in_progress` as a heading.
			"board.column.open":        "To do",
			"board.column.in_progress": "In progress",
			"board.column.done":        "Done",
			"board.column.cancelled":   "Cancelled",
			// ── WHAT A COLUMN'S "MORE" LINK PROMISES ─────────────────────────────────────
			// This read "Show only this", which described the IMPLEMENTATION (it sets
			// `filter=<status>` so the keyset pager can walk one status) and was read as
			// belonging to the card under it. The intent is the whole of that status, which
			// is the one thing a column cannot show: a column holds this page of the list,
			// and Open is 179 rows.
			// ── THE REST OF THE BOARD'S OWN WORDS ────────────────────────────────────────
			// These rendered from the screen's 3-arg `copy(contract, key, fallback)` defaults
			// because the contract never carried them; the text is the fallback verbatim so
			// nothing on the live board changes hands. The board owns its own group label,
			// per-column count sentence, the tooltip for a total the list query does not
			// publish, an empty column, and the two drag sentences: the hint under the
			// columns (desktop-only, where a drag exists) and the live-region prefix that
			// announces the move in flight to a screen reader.
			"board.aria":         "Tasks by status",
			"board.column_empty": "Nothing in this status on this page.",
			"board.drag_moving":  "Moving",
			// The board under a status filter draws ONLY that status's column, because the
			// row query can only return that status — four columns under `filter=done` left
			// Open and Doing reading "0 on this page" beneath header pills of 179 and 118.
			// These two say the board is narrowed and carry the way back.
			// Under the overdue lens the board keeps its two working columns and shows only the
			// cards past their deadline; the sentence says so where the one-status note would lie.
			// The person filter is one searchable control now (it was a `<select>` that could
			// not be typed into, plus a board avatar group whose overflow chip was dead). The
			// same control is the New task modal's "For" field. The `deadline.*` keys below
			// are that modal's deadline: the console's own calendar (a day) plus hour and
			// minute selects on the farm clock -- a native datetime picker drew Chrome's own
			// popover over the modal's buttons. "{date}" is the earliest day allowed.
			"filter.people_search":     "Type a name",
			"filter.people_no_matches": "Nobody by that name.",
			// The muted line under the title when the viewer may not edit this task (the
			// status pill is then a plain badge, not a control). Leadership monitors edit
			// anything since 2026-09-18, so this reads for a non-party without that authority.
			"detail.read_only": "Only the person who raised this task can edit it.",
			// A finished task is not editable by anyone (CanEdit requires open-for-work); saying
			// "only the raiser" to the CEO looking at a Done task was wrong (2026-09-18).
			// The feed carries its newest 20 rows; these page the rest, one window at a time.
			// The cancel confirm lives inside the status menu: two buttons, no browser dialog.
			"status.cancel_yes":       "Yes, cancel task",
			"status.cancel_no":        "Keep task",
			"activity.older":          "Show older activity",
			"activity.older_loading":  "Loading older…",
			"activity.older_failed":   "Older activity could not be loaded. Try again.",
			"detail.read_only_closed": "A finished task can't be edited. Reopen it to change the details.",
			// The drawer's ONE status control: the pill opens a menu of the task's status_options
			// (Cancel task last, in the danger tone). The confirm is asked before a cancel only,
			// because a cancelled task cannot be reopened.
			"status.menu_aria":      "Change status",
			"status.cancel_confirm": "Cancel this task? It cannot be reopened afterwards.",
			// An unprefixed parameter this page does not read. `?view=list` looks like it
			// selects a view and does not: `view` belongs to /vaccination, the calendar and
			// procurement, so Tasks cannot honour it without re-creating the collision its
			// `t_` prefix exists to prevent. It is ignored OUT LOUD instead.
			"state.ignored_params":    "This link sets a filter this page does not read. Tasks names its own filters with a t_ prefix.",
			"action.fix_link":         "Use the link this page reads",
			"column.assignee":         "Assignee",
			"column.raised_by":        "Raised by",
			"column.evidence":         "Evidence",
			"table.tasks.noun":        "task",
			"label.deadline":          "Deadline",
			"label.brief":             "Brief",
			"label.assignee_note":     "Assignee note",
			"section.selected.title":  "Selected task",
			"action.open_task":        "Open task",
			"action.edit":             "Edit",
			"state.preview":           "Preview data",
			"state.can_raise":         "Can raise",
			"state.unavailable_tasks": "Tasks could not be loaded. Try again.",
			"empty.tasks":             "No tasks match these filters.",
			"empty.tasks_detail":      "This queue is clear for the current role and park scope. When work is raised, it will appear here with the owner, evidence, and next status action.",
			"empty.selected":          "Select a task",
			"empty.selected_detail":   "Choose a row to view its brief, status actions, attachments, and activity updates.",
			"note.label":              "Activity update",
			"note.placeholder":        "Write the latest status or reply.",
			"note.send":               "Send update",
			"feed.update":             "Task update",
			"picker.voice":            "Voice note",
			"note.sending":            "Sending…",
			"note.just_now":           "just now",
			"note.you":                "You",
			"note.failed":             "The update could not be sent. Try again.",
			"picker.media":            "Photo or video",
			"picker.file":             "File",
			"deadline.day":            "Choose a day",
			"deadline.hour":           "Hour",
			"deadline.minute":         "Minute",
			"deadline.day_min":        "Pick a day on or after {date}.",
			"date.previous_month":     "Previous month",
			"date.next_month":         "Next month",
			"edit.title":              "Edit task",
			"edit.title_field":        "Title",
			"edit.body_field":         "Brief",
			"edit.deadline_field":     "Deadline",
			"edit.deadline_hint":      "Date and time the task is due, farm clock (IST). Leave it as it is to keep the stored deadline.",
			"edit.attachments":        "Attachments",
			"edit.too_many":           "A task carries at most 12 attachments.",
			"edit.save":               "Save changes",
			// The New task modal (`new-task-modal.tsx`): every label it shows, so the modal
			// never hard-codes a word the backend does not own (gate-1 #17, 2026-09-18).
			"new.open":              "New task",
			"new.title":             "New task",
			"new.for_field":         "For",
			"new.for_placeholder":   "Choose who this is for",
			"new.assigned_to":       "Assigned to",
			"new.title_field":       "Title",
			"new.body_field":        "Brief",
			"new.deadline_field":    "Deadline",
			"new.deadline_hint":     "Date and time the task is due, farm clock (IST).",
			"new.attachments":       "Attachments",
			"new.send":              "Send",
			"feedback.task_raised":  "Task raised.",
			"feedback.task_updated": "Task status updated.",
			"feedback.task_edited":  "Task saved.",
			"feedback.note_added":   "Update added to the task.",
			// ── A REFUSED WRITE ON THE TASKS DESK ────────────────────────────────────────
			// Three keys per outcome code, the screen trying the MOST SPECIFIC first:
			// `.named` (the person whose move it is resolved), `.status` (the task's current
			// chip resolved), then the plain key. A template is used only when every
			// placeholder it carries has a value, so an unresolved fact costs its CLAUSE and
			// never renders "{name}", a raw id, or the word "someone".
			//
			// WHAT CANNOT BE SAID, and why the conflict sentence names a STATUS and not a
			// PERSON: neither the 409 envelope nor the single-task read reports WHO made the
			// last status change, so "Chandrakant already moved this to Doing" is not
			// composable today. There is deliberately NO `feedback.version_conflict.named`:
			// the screen only tries that key when a name resolved, and a name never resolves
			// on a conflict (the Tasks page sends one only for a refusal that is ABOUT a
			// person). Adding `status_changed_by_name` to the leadership-task read is the
			// backend follow-up that would let this name them; until it lands, naming the new
			// status is the whole honest sentence.
			"feedback.version_conflict.status": "This task was moved to {status} while this board was open, so your move was not applied. The board now shows that — move it from what you see now.",
			"feedback.version_conflict":        "This task was changed while this board was open, so your move was not applied. The board now shows the current version.",
			// Fires on a DRAG as well as an edit, so it says what the task IS and that a
			// closed task cannot be MOVED — not that it "can no longer be edited".
			"feedback.task_closed.status": "This task is {status}, and a closed task cannot be moved.",
			"feedback.task_closed":        "This task is closed, so its status cannot be changed.",
			"feedback.forbidden":          "Only the person who raised this task can edit it.",
			// The three commonest drag refusals, which had no sentence at all and collapsed
			// into the page's generic failure line.
			"feedback.not_assignee.named":               "Only {name}, the person this task is assigned to, can move it.",
			"feedback.not_assignee":                     "Only the person this task is assigned to can move it.",
			"feedback.not_raiser.named":                 "Only {name}, who raised this task, can cancel it.",
			"feedback.not_raiser":                       "Only the person who raised this task can cancel it.",
			"feedback.invalid_status_transition.status": "This task is {status}, and that is not a move it can make from there.",
			"feedback.invalid_status_transition":        "That is not a move this task can make from where it is now.",
			"feedback.missing_title":                    "A task needs a title.",
			"feedback.missing_assignee":                 "Choose who the task is for.",
			"feedback.missing_deadline":                 "A task needs a deadline.",
			"feedback.missing_note":                     "Write the update before sending it.",
			"feedback.invalid_deadline":                 "That deadline is not a date and time.",
			"feedback.invalid_edit":                     "The task could not be saved. Reload the page and try again.",
			// Minted on the page BEFORE the write, when the dragged card carried no task, no
			// target status or no version — so NO status of this task is known at that point
			// and naming one would be an invention. It says which fact was missing instead.
			"feedback.invalid_status_change":   "That move did not say which task to move, or where to move it to. Reload the board and drag the card again.",
			"feedback.invalid_idempotency_key": "The form expired. Reload the page and try again.",
			"feedback.too_many_attachments":    "A task carries at most 12 attachments.",
			// ── A REFUSED @MENTION ───────────────────────────────────────────────────────
			// All four of these reached the screen as the generic failure line, so a reader
			// who named the wrong person on an update they can SEE was told only that
			// something went wrong. None of them carries {name}: the send stamps the outcome
			// code alone, so the offending person's name does not reach this sentence and the
			// repo's rule is to DROP the unresolvable clause rather than say "that person".
			// Each one names the RULE instead, which is the fact the reader can act on.
			"feedback.mention_not_visible":  "A person can only be named on a task they can already see. Pick from the list that appears when you type @, or share the task with them first.",
			"feedback.invalid_mention":      "A name that is not on this farm's leadership roster cannot be named on a task. Pick the person from the list that appears when you type @.",
			"feedback.too_many_mentions":    "One update can name at most 20 people.",
			"feedback.mention_without_note": "Write the update before naming anyone in it.",
			// ── THE ACTIVITY FEED (CEO instruction 2026-09-18, the Jira issue panel) ─────
			// Three views over ONE backend list (`LeadershipTask.activity`, migration
			// 000355): History is every change of fact, Comments is the notes, All is both,
			// newest first. The verb keys are the sentence AFTER the actor's name -- the
			// console renders "<name> <verb> <from> → <to>" from the row's own labels, and
			// the phone shows the backend's `summary` verbatim; both must read the same.
			"activity.tabs_aria":        "Activity views",
			"activity.tab_all":          "All",
			"activity.tab_history":      "History",
			"activity.tab_comments":     "Comments",
			"activity.created":          "created the task",
			"activity.status_changed":   "changed the status",
			"activity.assignee_changed": "changed the assignee",
			"activity.deadline_changed": "changed the deadline",
			"activity.title_changed":    "changed the title",
			"activity.brief_changed":    "edited the brief",
			"activity.commented":        "commented",
			"activity.cancelled":        "cancelled the task",
			"activity.updated":          "updated the task",
			"activity.empty":            "Nothing has happened on this task yet.",
			"activity.empty_history":    "No changes recorded on this task yet.",
			"activity.empty_comments":   "No comments on this task yet.",

			// ── NUMBERS THAT RECONCILE (Gate-1 #2, #4, #14) ─────────────────────────────
			// A search or filter with no hits is NOT an empty queue: `empty.filtered` says
			// nothing matched, and `empty.filtered_action` + `empty.filtered_rest` are the
			// sentence "Clear the filters to see all {count}." with the first half a link and
			// {count} the scope's unfiltered size (`scopes[].total`). `count.of` is the card
			// header under a status chip or any narrowing: "{shown} of {total}", so the header
			// and the chip on screen together never contradict. `board.total_on_page` is the
			// tooltip on a column pill whose whole-list total the list query does not publish
			// (the overdue lens and Cancelled), where the pill counts this page's cards instead
			// of reading "—".
			"empty.filtered":        "No tasks match.",
			"empty.filtered_action": "Clear the filters",
			"empty.filtered_rest":   "to see all {count}.",
			"count.of":              "{shown} of {total}",
			"board.total_on_page":   "The whole-list total for this column is not published; this counts the cards on this page.",
		}
	case "pen-routines":
		return map[string]string{
			"crumb":                           "Routines",
			"table.routines.title":            "Routines",
			"table.tasks.title":               "Today",
			"filter.park":                     "Park",
			"filter.business_date":            "Day",
			"filter.routine":                  "Routine",
			"action.create_routine.label":     "New routine",
			"action.edit_routine.label":       "Edit",
			"action.set_routine_status.label": "Pause / resume / retire",
			"action.pause":                    "Pause",
			"action.resume":                   "Resume",
			"action.retire":                   "Retire",
			"action.save":                     "Save",
			"drawer.routine.title":            "Routine",
			"drawer.routine.create_title":     "New routine",
			"drawer.routine.edit_title":       "Edit routine",
			"field.name":                      "Name",
			"field.instruction":               "Instruction",
			"field.park":                      "Park",
			"field.scope":                     "Pens",
			"field.occupied_only":             "Skip empty pens",
			"field.cadence":                   "When",
			"field.weekdays":                  "Weekdays",
			"field.month_days":                "Days of the month",
			"field.after_work_kinds":          "After which work",
			"field.due_offset_days":           "Days after",
			"field.notify_time":               "Push at",
			"field.review":                    "Review",
			"field.assignee_roles":            "Who does it",
			"field.interval_days":             "Every how many days",
			"field.start_date":                "Starting from",
			"field.questions":                 "Questions",
			"field.question_proof":            "Proof for this question",
			"field.question_proof_count":      "How many",
			"field.photo":                     "Photos",
			"field.video":                     "Videos",
			"field.presence":                  "Pen check-in",
			"field.min":                       "At least",
			"field.max":                       "At most",
			"hint.assignee_roles":             "Pick roles. Whoever holds a role for this park gets the task; one of them doing it is enough.",
			"hint.park_scope":                 "One task for the whole park, not one per pen.",
			"table.people.preview":            "Currently",
			"empty.role_people":               "Nobody holds this role for this park yet.",
			"hint.versions":                   "Every save is a new version. A check already raised keeps the form it was raised with.",
			"summary.due":                     "Due",
			"summary.delayed":                 "Delayed",
			"summary.in_review":               "In review",
			"summary.sent_back":               "Sent back",
			"summary.done":                    "Done",
			"empty.routines":                  "No routines yet. Create one to start raising pen checks.",
			"empty.tasks":                     "No checks on this day.",
			"configure.disabled_no_access":    "Writing routines is limited to the CEO and CXO.",
			// The drawer's question editor and the confirmations (added for the /routines page).
			"action.add_question":     "Add question",
			"action.remove_question":  "Remove question",
			"action.add_option":       "Add option",
			"action.remove_option":    "Remove option",
			"action.retire.confirm":   "Retire this routine? It will raise no more checks. Open checks stay as they are.",
			"action.routine_saved":    "Routine saved. The next occurrence uses this version.",
			"action.routine_failed":   "The routine could not be saved. Check the fields and try again.",
			"field.question_title":    "Question",
			"field.question_id":       "Key",
			"field.question_kind":     "Answer type",
			"field.question_hint":     "Hint",
			"field.question_required": "Required",
			"field.question_unit":     "Unit",
			"field.question_options":  "Choices",
			"field.option_value":      "Value",
			"field.option_label":      "Label",
			"filter.park.all":         "All parks",
		}
	case "configuration-items":
		return map[string]string{
			"crumb":                        "Items and settings",
			"rail.title":                   "Registers",
			"search.placeholder":           "Search",
			"status.active":                "Active",
			"status.archived":              "Inactive",
			"status.all":                   "All",
			"action.create_row.label":      "Add",
			"action.edit_row.label":        "Edit",
			"action.set_row_status.label":  "Make active or inactive",
			"action.delete_row.label":      "Remove",
			"action.archive":               "Make inactive",
			"action.restore":               "Make active",
			"action.delete":                "Remove",
			"action.save":                  "Save",
			"action.cancel":                "Cancel",
			"action.close":                 "Close",
			"action.edit_elsewhere":        "Edit in",
			"action.success_message":       "Saved.",
			"action.deleted_message":       "Removed.",
			"action.row_menu":              "More",
			"action.row_actions":           "Actions",
			"action.failed_message":        "That could not be saved.",
			"action.error_form":            "Check the highlighted fields.",
			"drawer.create_title":          "New",
			"drawer.edit_title":            "Edit",
			"drawer.usage_title":           "Where it is used",
			"drawer.builtin_hint":          "Built into the product: it can be renamed, but not made inactive or removed.",
			"drawer.delete_confirm":        "Remove this for good? It cannot be undone. A record something still uses can be neither removed nor made inactive.",
			"drawer.archive_hint":          "An inactive record stops being offered for anything new. A record something still uses must be freed first \u2014 move the animals, or change what names it.",
			"empty.rows":                   "Nothing here yet.",
			"empty.search":                 "No matches.",
			"pager.noun":                   "records",
			"column.display":               "Name",
			"column.status":                "Status",
			"column.counts":                "Holds",
			"column.item":                  "Item",
			"column.tracking":              "Tracking",
			"filter.department.all":        "Departments: all",
			"lists.title":                  "Lists",
			"lists.all":                    "All items",
			"lists.catalogues":             "Catalogues",
			"lists.yours":                  "Your lists",
			"lists.new":                    "New list",
			"lists.search":                 "Search",
			"lists.sublists":               "Sub-lists",
			"lists.all_in":                 "All in",
			"lists.new_under":              "New list under",
			"reference.add_list":           "Add list",
			"reference.edit_list":          "Edit list",
			"value.yes":                    "Yes",
			"value.no":                     "No",
			"tag.builtin":                  "Built in",
			"configure.disabled_no_access": "Changing these lists is limited to the CEO and CXO.",
			"sheet.download":               "Download",
			"sheet.upload":                 "Upload",
			"sheet.title":                  "Sheet",
			"sheet.download_hint":          "Download every row of this list as a spreadsheet: CSV or Excel. Ref columns carry the name; the id column lets you upload changes back.",
			"sheet.download_hint_animals":  "Download the whole herd as a spreadsheet: CSV or Excel, one row per animal with its tags, pen and stage.",
			"sheet.download_csv":           "Download CSV",
			"sheet.download_xlsx":          "Download Excel",
			"sheet.template":               "Template",
			"sheet.template_hint":          "Start from the template: it has exactly the columns the upload expects.",
			"sheet.upload_title":           "Upload a sheet",
			"sheet.upload_hint":            "A .csv or .xlsx file with a header row. Rows with an id update that row; rows without add a new one. Nothing is written until you apply.",
			"sheet.upload_hint_animals":    "A .csv or .xlsx file with a header row. Each row adds one animal to the herd register; an animal already on the register is reported, never changed.",
			"sheet.choose_file":            "Choose file",
			"sheet.upload_action":          "Upload and check",
			"sheet.uploading":              "Uploading",
			"sheet.checking":               "Checking rows",
			"sheet.applying":               "Applying rows",
			"sheet.preview_title":          "Ready to apply",
			"sheet.rows_total":             "rows in the file",
			"sheet.rows_valid":             "ready",
			"sheet.rows_invalid":           "need fixing",
			"sheet.rows_applied":           "applied",
			"sheet.rows_failed":            "failed",
			"sheet.apply":                  "Apply",
			"sheet.apply_hint":             "Only the ready rows are written; download the rows to fix, correct them and upload that file again.",
			"sheet.cancel":                 "Cancel upload",
			"sheet.download_errors":        "Download rows to fix",
			"sheet.done":                   "Done",
			"sheet.failed":                 "The upload stopped",
			"sheet.cancelled":              "Cancelled",
			"sheet.problems_title":         "First problems",
			"sheet.problem_row":            "Row",
			"sheet.recent":                 "Recent uploads",
			"sheet.status.validating":      "Checking",
			"sheet.status.previewed":       "Ready to apply",
			"sheet.status.applying":        "Applying",
			"sheet.status.applied":         "Applied",
			"sheet.status.failed":          "Stopped",
			"sheet.status.cancelled":       "Cancelled",
			"sheet.status.queued":          "Waiting its turn",
			"workbook.title":               "Setup workbook",
			"workbook.open":                "Setup workbook",
			"workbook.intro":               "Set the farm up from one Excel file: a tab for each list. Download the template, fill the tabs you need, and upload the file once.",
			"workbook.template":            "Download template",
			"workbook.template_hint":       "Every tab has exactly the columns its list expects. Leave a tab empty to skip that list.",
			"workbook.export":              "Download everything",
			"workbook.export_hint":         "Every list as a tab, with ids, so the whole setup can be edited and uploaded back.",
			"workbook.upload_title":        "Upload a workbook",
			"workbook.upload_hint":         "An .xlsx file with one tab per list. A pen may name a park that is only on the Parks tab; the tabs are checked and written in order. Nothing is written until you apply.",
			"workbook.upload_action":       "Upload and check",
			"workbook.order_hint":          "Tabs are worked in this order: species, gender, stages, parks, pens, partitions, lists, items, then animals.",
			"workbook.unknown_sheets":      "Tabs that match no list and were ignored:",
			"workbook.tab":                 "Tab",
			"workbook.list":                "List",
			"workbook.col.rows":            "Rows",
			"workbook.col.ready":           "Ready",
			"workbook.col.fix":             "Fix",
			"workbook.col.applied":         "Done",
			"workbook.col.failed":          "Failed",
			"workbook.apply":               "Apply all tabs",
			"workbook.apply_hint":          "Only the ready rows are written, tab after tab. A row that names a row of an earlier tab that was not added is not written either.",
			"workbook.cancel":              "Cancel workbook",
			"workbook.download_errors":     "Download rows to fix",
			"workbook.recent":              "Recent workbooks",
			"workbook.tabs_done":           "tabs done",
			"workbook.no_tabs":             "No tab in the file had rows.",
		}
	case "work-board":
		return map[string]string{
			"crumb":                               "Work Board",
			"board.title":                         "Board",
			"roleline.all_modules":                "All modules",
			"roleline.none":                       "No module selected",
			"section.board.aria":                  "Work board",
			"drawer.subtasks.in_module":           "Each subtask opens in its own module for now.",
			"drawer.subtasks.loading":             "Loading subtasks",
			"drawer.subtasks.error":               "The subtasks could not be loaded. Try again.",
			"drawer.subtasks.empty":               "No subtasks on this card.",
			"drawer.subtasks.open":                "Open",
			"drawer.subtasks.watch_proof":         "Watch proof",
			"step.todo":                           "To do",
			"step.in_progress":                    "In progress",
			"step.in_review":                      "In review",
			"step.done":                           "Done",
			"step.rework":                         "Rework",
			"step.needs_attention":                "Needs attention",
			"step.locked":                         "Not yet",
			"card.total":                          "subtasks",
			"board.rule":                          "Column is automatic, at every level: step, then subtask, then card. Nothing started is To do, anything started is In progress, everything submitted is In review, everything approved is Done.",
			"board.attention":                     "Amber card: something on it is not moving or past its clock.",
			"lane.todo":                           "To do",
			"lane.in_progress":                    "In progress",
			"lane.in_review":                      "In review",
			"lane.done":                           "Done",
			"lane.empty":                          "Nothing here",
			"lane.empty.other_pages":              "None on this page",
			"filter.search":                       "Search work",
			"filter.assignee":                     "Assignee",
			"filter.assignee.search":              "Search users",
			"filter.assignee.none":                "No users match",
			"filter.assignee.clear":               "Clear all",
			"filter.assignee.select_all":          "Select all",
			"filter.assignee.all":                 "All",
			"filter.park":                         "Park",
			"filter.park.all":                     "All parks",
			"filter.date":                         "Date",
			"filter.date.today":                   "Today",
			"filter.module":                       "Module",
			"filter.module.all":                   "Module · all",
			"filter.module.none":                  "Module · none",
			"filter.state":                        "Status",
			"card.done":                           "done",
			"card.in_review":                      "in review",
			"card.started":                        "started",
			"card.attention":                      "needs attention",
			"card.past_clock":                     "past clock",
			"drawer.title":                        "Work item",
			"drawer.description":                  "Description",
			"drawer.subtasks":                     "Subtasks · what is done and what is not",
			"drawer.subtasks.worst_first":         "worst first",
			"drawer.subtasks.showing":             "Showing",
			"drawer.subtasks.of":                  "of",
			"drawer.subtasks.page":                "Page",
			"drawer.activity":                     "Activity",
			"drawer.details":                      "Details",
			"drawer.status_auto":                  "auto",
			"drawer.status_auto.title":            "Derived from the subtasks; nobody sets it by hand",
			"drawer.open_module":                  "Open in module",
			"drawer.call_park_head":               "Call park head",
			"drawer.call_park_head.none":          "Nothing on this card needs a call",
			"flag_park_head.title":                "Flag to park head",
			"flag_park_head.disabled_no_access":   "Flagging work to a park head is for directors and the CEO's office.",
			"flag.note":                           "What should the park head check?",
			"flag.note.placeholder":               "Optional note for the park head",
			"flag.submit":                         "Flag to park head",
			"flag.hint":                           "Raises a task for the park head with this card's details. They acknowledge it on the phone.",
			"action.flag_raised":                  "Flagged to the park head.",
			"action.flag_park_head_missing":       "This park has no park head to flag. Fix the park's people first.",
			"action.flag_to_self":                 "You are this park's head; the flag would come back to you.",
			"action.flag_park_head_not_reachable": "This park's head cannot receive tasks on the phone yet.",
			"action.flag_row_not_found":           "That work is not on your board for this park and day. Refresh and try again.",
			"action.flag_failed":                  "The flag could not be recorded. Try again.",
			"tile.done":                           "Done",
			"tile.pending":                        "Pending",
			"tile.attention":                      "Needs attention",
			"owner.missing":                       "No one assigned",
			"owner.pool":                          "Whoever does it",
			"detail.status":                       "Status",
			"detail.module":                       "Module",
			"detail.park":                         "Park",
			"detail.pen":                          "Pen",
			"detail.owner":                        "Owner",
			"detail.clock":                        "Clock",
			"detail.business_date":                "Business date",
			"state.empty":                         "No work on this board for the day.",
			"state.empty.own_rows":                "Nothing assigned to you for this day.",
			"state.row_missing":                   "That work is not on this page of the board. It may be on another day, another park or a later page.",
			"state.error":                         "The board could not be loaded. Try again.",
			"state.partial":                       "Some work couldn't load right now:",
			"action.retry":                        "Retry",
			"pager.rows":                          "rows",
			"column.title":                        "Work",
			"column.module":                       "Module",
			"column.park":                         "Park",
			"column.pen":                          "Pen",
			"column.owner":                        "Owner",
			"column.clock":                        "Clock",
			"column.work_state":                   "Status",
			"column.counts":                       "Progress",
			"column.name":                         "Subtask",
			"column.steps":                        "Steps",
			"column.status":                       "Status",
		}
	case "verification-review":
		return map[string]string{
			// The VERTICAL this screen belongs to, the way every other page names its parent. It read
			// "Approvals" — a different top-level module that merely sits next to this one in the nav —
			// which is the one thing a breadcrumb must never do.
			"crumb": "Verification",
			// Verifier video-review board copy. These are backend-owned like every other visible
			// string here: the frontend previously carried them as local fallbacks, which is the
			// hardcoded-visible-literal defect the contract rule exists to prevent.
			"board.title":      "Verification Board",
			"filter.shed":      "Pen (optional)",
			"filter.all_sheds": "All pens",
			// The module filter row (maintainer request 2026-08-11). The module NAMES are not here:
			// they come from the verification type registry on the queue response
			// (filter_options.modules), which is the only place that knows which modules exist and
			// what each is called. Duplicating them here would put two sources of truth on one row
			// and quietly go stale the next time a module is registered.
			"filter.module":      "Module",
			"filter.all_modules": "All modules",
			// The capture-date calendar (maintainer request 2026-08-12). It lands on TODAY and the
			// queue opens filtered to today, so every label below has to make the current scope
			// legible at a glance — a verifier who cannot see that she is looking at one day will
			// read an empty board as "nothing to do" instead of "nothing captured today".
			"filter.date":                "Capture date",
			"filter.date.today":          "Today",
			"filter.date.single":         "Single day",
			"filter.date.range":          "Date range",
			"filter.date.aria":           "Choose which capture dates the board shows",
			"filter.date.previous_month": "Previous month",
			"filter.date.next_month":     "Next month",
			// Range picking is two clicks, and the half-picked state is the one people get stuck in.
			"filter.date.range_start_hint": "Pick the first day of the range.",
			"filter.date.range_end_hint":   "Now pick the last day of the range.",
			"filter.date.range_separator":  "to",
			"table.hint":                   "Open a row to review the evidence and record a verdict.",
			// Accessible label for the player's full-screen toggle.
			"drawer.media.fullscreen_label": "Full screen",
			// Accept is blocked when the proof media does not resolve, so a verdict can never be
			// recorded against evidence nobody could watch.
			// filter.action_type / filter.all_action_types were REMOVED with the action-type
			// dropdown itself (maintainer decision 2026-08-07): the left nav is the only scope
			// selector on this screen. Shed remains the one filter.
			"filter.apply":          "Apply filters",
			"filter.clear_all":      "Clear filters",
			"action.open_details":   "Details",
			"action.close":          "Close",
			"action.open_audit_log": "Open Audit Log",
			// Keyset pagination, so there is no page NUMBER the backend can hand out and no
			// OFFSET to jump with (docs/decisions/scale-anti-patterns.md bans OFFSET here). The
			// client walks forward on next_cursor and back down a trail of the cursors it has
			// already used, so "previous" is a real keyset read, not an offset.
			"pagination.next":              "Next page",
			"pagination.previous":          "Previous page",
			"pagination.position":          "Page",
			"state.queue_unavailable":      "Actions are unavailable",
			"state.queue_unavailable_body": "The verification queue could not be loaded from the backend.",
			// Deliberately no longer names an "action type": that filter was removed on
			// 2026-08-07, so mentioning it sent the verifier hunting for a control that is not on
			// the screen. The module now comes from the left nav.
			"state.empty":                   "No actions to review for this module in the selected status, park, and date.",
			"drawer.eyebrow":                "Action details",
			"drawer.aria":                   "Action details",
			"drawer.close_label":            "Close action details",
			"drawer.meta.status":            "Status",
			"drawer.meta.subject_note":      "Operator note",
			"drawer.meta.reason":            "Verdict reason",
			"drawer.meta.verified_by":       "Verified by",
			"drawer.meta.verified_at":       "Verified at",
			"drawer.meta.captured":          "Captured at",
			"drawer.meta.operator":          "Operator",
			"drawer.meta.shed":              "Pen",
			"drawer.meta.park":              "Park",
			"drawer.meta.source_module":     "Source module",
			"drawer.meta.source_task":       "Source task",
			"drawer.meta.source_submission": "Source submission",
			"drawer.media.title":            "Proof videos and media",
			"drawer.media.empty":            "No proof media is attached to this action.",
			"drawer.media.open":             "Open proof",
			"drawer.media.play_video":       "Play video",
			"drawer.media.loading_photo":    "Loading photo…",
			// DOUBLE-SPEED PLAYBACK (maintainer decision 2026-08-17). Offered only on clips longer
			// than 20 seconds -- on a short one it saves a few seconds while making it materially
			// easier to miss the single moment the proof turns on, so the control is absent rather
			// than present and discouraged. The two labels are the button's OFF and ON states.
			"player.speed_normal": "Play at 2x",
			"player.speed_fast":   "Playing at 2x",
			"player.speed_hint":   "Speeds up long videos. Available on videos longer than 20 seconds.",
			"player.play_proof":   "Play proof video",
			"drawer.note":         "Verifier decisions remain separate from source-task action. Rework and reassignment below act only on the linked SOP task.",
			"feedback.done":       "Done",
			"feedback.failed":     "Action failed",
			// feedback.<server error code>. The raw code is an internal token and must never be
			// the sentence a verifier reads -- the screen literally said "Action failed
			// missing_reason". Unmapped codes render nothing rather than leaking the token.
			"feedback.missing_reason":    "A rejection needs a reason. Say what the video showed that failed the standard, then press Reject again.",
			"feedback.permission_denied": "Recording a verdict is limited to the video verification team.",
			"feedback.conflict":          "Someone else recorded a verdict on this action first. Reload to see it.",
			// The VERIFIER's weight correction (maintainer decision 2026-08-17). The control's own
			// copy -- heading, help, field labels, button -- is declared by the PRODUCING module in
			// the verification category registry and travels on the item, so it is not repeated
			// here. These are only the outcome sentences this screen renders after the write.
			//
			// Each refusal has a distinct remedy, so each gets its own sentence: a closed bucket
			// needs a manager, an out-of-range value needs a different number, and an already-used
			// key needs a reload. One shared "that failed" would leave her with no next step.
			"feedback.weight_corrected":    "Weight corrected. The record now shows the weight you entered.",
			"feedback.missing_weight":      "Enter the correct weight in kg.",
			"feedback.weight_out_of_range": "Enter a weight in kg between 0.001 and 100000.",
			// The goat count stopped being correctable on 2026-08-24: it is recorded
			// automatically from the herd register at submit and frozen, so the one
			// count refusal left says exactly that.
			"feedback.animal_count_not_applicable": "The goat count is recorded automatically and can't be changed. Correct the weight only.",
			"feedback.weighing_bucket_closed":      "This pen's weighing is already closed. Ask a manager to reopen it before correcting the weight.",
			// The VERIFIER's feed-wastage measurement (maintainer decision 2026-08-18). Same shape
			// as the weight correction above: the control's own copy travels on the item; these are
			// only the outcome sentences.
			"feedback.wastage_recorded":     "Wastage recorded. The pen's record now shows the weight you entered.",
			"feedback.missing_wastage":      "Enter the leftover feed weight in kg.",
			"feedback.wastage_out_of_range": "Enter a leftover weight in kg between 0 and 10000.",
			// Rework / Re-assign / Penalty copy REMOVED with those panels (maintainer decision
			// 2026-08-07). mock/verifier-web-mock.SPEC.md section 1: the verifier watches a proof
			// video and accepts it, or rejects it with a reason -- "that is all. Nothing else
			// belongs on this screen." Section 3 bans source-task action here by name.
			//
			// Penalty note was additionally DEAD: no server action, no route, and its own visible
			// label said so while leaking an internal word into user-facing copy.
			//
			// Rework and Re-assign remain real writes for the authority surface that owns them;
			// only their placement on the verifier's screen was wrong. Re-add their copy THERE, not
			// here, or this screen quietly regrows the half it was just cleared of.
			// Verifier verdict copy. Approve/reject is the verifier's ONLY act: the wording must
			// not promise that approving closes or completes the underlying work, because it does
			// not -- an authority closes the submission afterwards.
			"verdict.title":                "Video verification",
			"verdict.reason_label":         "Note for this verdict",
			"verdict.reason_placeholder":   "What did the video show? Required to reject, optional to approve.",
			"verdict.approve":              "Approve",
			"verdict.reject":               "Reject",
			"verdict.reason_required":      "A rejection must say what was wrong. A note on an approval is optional.",
			"verdict.disabled_not_pending": "This action already has a verdict and cannot be reviewed again.",
			"verdict.disabled_no_access":   "Recording a verdict is limited to the video verification team.",
			"verdict.disabled_no_evidence": "No video available — accept is blocked. Reject it, or come back once the proof resolves.",
			// The TOXIN review tab (maintainer decision 2026-08-25): aflatoxin strip tests on
			// purchased feed loads, reviewed by the CEO's office alone. Deliberately NOT part of
			// the generic verification queue -- the toxin_tab / toxin_verdict controls follow
			// toxin.verdict, which the verifier never holds.
			"toxin_tab.title":                         "Toxin",
			"toxin_tab.disabled_no_access":            "Feed toxin tests are reviewed by the CEO's office.",
			"toxin_verdict.title":                     "Record toxin verdict",
			"toxin_verdict.disabled_no_access":        "Feed toxin tests are reviewed by the CEO's office.",
			"toxin.table.hint":                        "Open a test to see every step's proof and the strip reading.",
			"toxin.state.empty":                       "No toxin tests are waiting for review.",
			"toxin.drawer.title":                      "Toxin test review",
			"toxin.drawer.steps":                      "Procedure steps",
			"toxin.drawer.strip_photo":                "Strip photo",
			"toxin.drawer.reading":                    "Recorded reading",
			"toxin.drawer.reject_reason":              "Rejection reason",
			"toxin.drawer.reject_reason_hint":         "Say what failed the standard. Rejecting cancels this round and creates a retest.",
			"toxin.action.accept":                     "Accept",
			"toxin.action.reject":                     "Reject",
			"toxin.feedback.done":                     "Done",
			"toxin.feedback.version_conflict":         "This test changed since you opened it. Reload and review it again.",
			"toxin.feedback.reject_reason_required":   "A rejection needs a reason before it can be recorded.",
			"toxin.feedback.test_not_awaiting_review": "This test is not waiting for review any more. Reload to see its current state.",
			// THE APPROVE CARRIES THE NUMBER (maintainer decision 2026-08-20). Shown where the
			// reading is born on the verifier's screen — feed wastage, whose operator submits a
			// video and no number at all. Reject stays available on purpose: a value that cannot
			// be read off the clip is a rejection, never a guess.
			"verdict.disabled_measurement_required": "Enter the weight you can read in the video, then accept. If it cannot be read, reject the video instead.",
			// THE VERIFIER IS WARNED, NOT TOLD (maintainer decision 2026-09-09): a packed weight more
			// than 500 g away from the plan is refused once with a direction only -- never the
			// planned figure, never the gap, so blind entry (2026-08-21) survives -- and lands when
			// she ticks the confirmation and presses Accept again. The two direction sentences are
			// keyed by the backend field-error codes feeddirection/domain.PackingEntryAbovePlan /
			// PackingEntryBelowPlan and must read the same as that package's
			// PackingEntryVarianceMessage, which is what the phone renders.
			"feedback.measurement_confirmation_required": "One or more packed weights are more than 500 g away from the plan. Check the video again, and approve only if you are sure of your readings.",
			"verdict.variance.above_plan":                "More than 500 g above the plan. Check the video again.",
			"verdict.variance.below_plan":                "More than 500 g below the plan. Check the video again.",
			"verdict.variance_confirm_label":             "I checked the video again and I am sure of these weights",
			"verdict.disabled_variance_unconfirmed":      "Tick the confirmation above to accept with these weights, or correct them.",
			"action.disabled_no_authority":               "Acting on the source task is limited to the park head, director, or CEO.",
			"verdict.note":                               "Approving records that the video meets the standard. It does not close the work — an authority does that once every proof in the submission is approved.",
			// CEO/PC-Director oversight analytics section copy (permissions.VerificationOversee,
			// same capability as the oversight_analytics/oversight_filters controls). Language is
			// CEO-plain by design: "videos waiting for review", not internal jargon.
			"oversight_analytics.title":                   "Verification oversight",
			"oversight_analytics.hint":                    "Every module and park in the current scope",
			"oversight_analytics.open":                    "Analytics",
			"oversight_analytics.close":                   "Close analytics",
			"oversight_analytics.unavailable":             "Oversight analytics are unavailable right now.",
			"oversight_analytics.videos_waiting":          "Videos waiting for review",
			"oversight_analytics.oldest_pending":          "Oldest video still unreviewed",
			"oversight_analytics.review_speed":            "Verdicts per active day",
			"oversight_analytics.est_days_to_clear":       "Est. days to clear backlog",
			"oversight_analytics.reject_rate":             "Reject rate (30d)",
			"oversight_analytics.module_latency":          "Median review time by module",
			"oversight_analytics.pending_by_module":       "Where the backlog sits",
			"oversight_analytics.largest_backlog":         "is the biggest share",
			"oversight_analytics.oldest_hint":             "Longest any video has waited",
			"oversight_analytics.speed_hint":              "Averaged over days with verdicts",
			"oversight_analytics.clear_hint":              "At the current review pace",
			"oversight_analytics.reject_hint":             "A rejected video needs a re-shoot",
			"oversight_analytics.median_review":           "median review",
			"oversight_analytics.no_median":               "No review time recorded yet",
			"oversight_analytics.no_backlog":              "No videos are waiting for review.",
			"oversight_analytics.verifier_activity":       "Verifier activity (last 14 days)",
			"oversight_analytics.age_shape":               "How long they have been waiting",
			"oversight_analytics.age.up_to_1_day":         "Under a day",
			"oversight_analytics.age.one_to_three_days":   "1-3 days",
			"oversight_analytics.age.three_to_seven_days": "3-7 days",
			"oversight_analytics.age.over_seven_days":     "Over a week",
			"oversight_analytics.trend":                   "Reviewed vs arrived, last 14 days",
			"oversight_analytics.trend.verdicts_noun":     "reviewed",
			"oversight_analytics.trend.arrived_noun":      "arrived",
			"oversight_analytics.trend.empty":             "No videos arrived or were reviewed in the last 14 days.",
			"oversight_analytics.trend.grew":              "Backlog grew by",
			"oversight_analytics.trend.shrank":            "Backlog shrank by",
			"oversight_analytics.trend.flat":              "Backlog unchanged",
			"oversight_analytics.open_module_queue":       "Open this queue",
			"oversight_analytics.col.verifier":            "Verifier",
			"oversight_analytics.col.verdicts":            "Verdicts",
			"oversight_analytics.col.approved":            "Approved",
			"oversight_analytics.col.rejected":            "Rejected",
			"oversight_analytics.col.busiest_day":         "Busiest day",
			"oversight_analytics.col.watch_integrity":     "Watch integrity",
			"oversight_analytics.tracked":                 "tracked",
			"oversight_analytics.watched_full":            "watched in full",
			"oversight_analytics.no_play":                 "decided without playing",

			// RANDOMIZATION copy (permissions.VerificationSampling, maintainer decision
			// 2026-08-26). CEO-plain and farm-worded throughout: this panel says how much of each
			// module's proof a person actually watches, so it talks about videos and share of work,
			// never about sampling buckets, policies or percentages of a population.
			"randomization.title":          "Randomization",
			"randomization.open":           "Randomization",
			"randomization.close":          "Close randomization",
			"randomization.hint":           "How much of each module's proof the verifier reviews",
			"randomization.unavailable":    "Randomization is unavailable right now.",
			"randomization.col.module":     "Module",
			"randomization.col.share":      "Share to review",
			"randomization.col.today":      "Today",
			"randomization.col.progress":   "Progress",
			"randomization.apply":          "Apply",
			"randomization.saving":         "Saving...",
			"randomization.share_help":     "Videos are picked at random. Raising the share adds more of today's videos; it never takes back one already reviewed.",
			"randomization.videos_arrived": "videos arrived",
			"randomization.to_review":      "to review",
			"randomization.reviewed":       "reviewed",
			"randomization.settled":        "settled without review",
			"randomization.complete":       "Done for the day",
			"randomization.locked":         "Every video",
			"randomization.effective":      "In force since",
			"randomization.set_by":         "Set by",
			"randomization.empty":          "No proof arrived on this day.",
			"randomization.save_failed":    "That share could not be saved. Try again.",

			// VIDEO LOG copy (permissions.VerificationEvidenceTimeline, maintainer decision
			// 2026-08-14). Farm-plain throughout: this is read by a verifier and by leadership, and
			// the copy firewall bans implementation words on both surfaces. "Arrived" rather than
			// "uploaded" in the reading copy, because what the farm cares about is that the video
			// reached the office -- the column header still says Uploaded, which is the operator's
			// own word for the act.
			"video_log.open":                "Video Log",
			"video_log.close":               "Close video log",
			"video_log.title":               "Video Log",
			"video_log.hint":                "When each video arrived, pen by pen",
			"video_log.disabled_no_access":  "The video log is limited to the verification team and leadership.",
			"video_log.unavailable":         "The video log is unavailable right now.",
			"video_log.day":                 "Day",
			"video_log.all_sheds":           "All pens",
			"video_log.back_to_sheds":       "Back to all pens",
			"video_log.empty_day":           "No videos arrived on this day.",
			"video_log.empty_shed":          "No videos arrived from this pen on this day.",
			"video_log.filter.park":         "Park",
			"video_log.filter.shed":         "Pen",
			"video_log.filter.search":       "Search",
			"video_log.filter.search_hint":  "Pen, work, person or video",
			"video_log.filter.all_parks":    "All parks",
			"video_log.filter.all_sheds":    "All pens",
			"video_log.filter.apply":        "Apply",
			"video_log.filter.clear":        "Clear",
			"video_log.no_match":            "Nothing on this day matches those filters.",
			"video_log.col.park":            "Park",
			"video_log.col.shed":            "Pen",
			"video_log.col.work":            "Work",
			"video_log.col.video":           "Video",
			"video_log.col.uploaded":        "Uploaded",
			"video_log.col.first_last":      "First and last",
			"video_log.col.videos":          "Videos",
			"video_log.videos_count":        "videos",
			"video_log.items_count":         "pieces of work",
			"video_log.awaiting_upload":     "not arrived yet",
			"video_log.awaiting_upload_one": "Not arrived yet",
			// Shown when a proof was filmed on the day but only reached the office on a later one.
			// Without it a reader sees a time with no date and assumes same-day.
			"video_log.arrived_later":    "arrived",
			"video_log.captured_label":   "Recorded",
			"video_log.truncated":        "This pen has more work than fits here. Narrow the day or the park to see the rest.",
			"video_log.grain.animal":     "Animal",
			"video_log.grain.shed":       "Pen",
			"video_log.open_in_queue":    "Open this queue",
			"video_log.download":         "Download CSV",
			"video_log.export_truncated": "This day had more videos than one file holds. Narrow the park and download again.",
			// Shown on each summary row so the per-video drill-down is discoverable: the shed name
			// alone read as a plain label and the maintainer could not find the video times.
			"video_log.view_videos": "View videos",
		}
	case "calendar":
		return map[string]string{
			"filter.owner.aria":                       "Owner",
			"filter.workstream.aria":                  "Calendar workstream",
			"week.all_days_selected_label":            "all days selected",
			"week.all_owners_selected_label":          "all owner lanes",
			"week.legend.all_owner_lanes":             "All owner lanes",
			"week.legend.lane_suffix":                 "lane",
			"week.showing_prefix":                     "Showing",
			"week.work_only_suffix":                   "work only.",
			"week.only_suffix":                        "only.",
			"week.whole_week":                         "whole week",
			"week.empty_day_prefix":                   "on",
			"week.empty_scope_suffix":                 "in this scope.",
			"label.today":                             "TODAY",
			"action.back_previous":                    "Back to the previous screen",
			"drawer.event.aria":                       "Calendar event",
			"drawer.event.close_label":                "Close Calendar event drawer",
			"drawer.event.eyebrow":                    "CALENDAR EVENT",
			"drawer.passport.aria":                    "Animal Passport",
			"drawer.passport.close_label":             "Close Animal Passport drawer",
			"calendar.drawer.eligible_animals":        "Eligible herd animals",
			"calendar.drawer.deferred_animals":        "Deferred herd animals",
			"calendar.drawer.blocked_animals":         "Blocked herd animals",
			"calendar.drawer.linked_animals":          "Linked herd animals",
			"calendar.drawer.eligible_animals_empty":  "No herd animals are linked to this drive yet.",
			"section.vaccination.title":               "Vaccination",
			"calendar.drive.breed_header":             "Breed",
			"calendar.drive.park_header":              "Park",
			"calendar.drive.sex_header":               "Sex",
			"calendar.drive.weight_header":            "Weight",
			"action.full_change_history":              "Full change history",
			"action.send_nudge":                       "Send nudge",
			"action.snooze":                           "Snooze",
			"action.escalate":                         "Escalate",
			"action.open_workflow":                    "Open Workflow",
			"table.vaccination.aria":                  "Vaccination history",
			"vaccination.unavailable_prefix":          "Vaccination passport unavailable",
			"vaccination.next_due":                    "Next due",
			"vaccination.open_obligations":            "Open obligations",
			"vaccination.last_accepted":               "Last accepted",
			"vaccination.open_due_rows":               "Open due rows",
			"vaccination.history":                     "History",
			"vaccination.empty_history":               "No vaccination history yet. Once a protocol is published and a dose is administered + verified, it appears here with its proof/verification status and protocol version.",
			"vaccination.empty_open":                  "No open vaccination obligations for this goat.",
			"vaccination.no_upcoming":                 "no upcoming dose",
			"vaccination.clinical_due":                "clinical due",
			"vaccination.proof_verified":              "proof verified",
			"vaccination.awaiting_verify":             "awaiting verification",
			"vaccination.rework_rejected":             "rework / rejected",
			"action.open_drive":                       "Open drive",
			"action.nudge_sent":                       "Nudge sent.",
			"action.nudge_replay":                     "Nudge already sent (replay).",
			"action.snooze_recorded":                  "Event snoozed.",
			"action.snooze_replay":                    "Snooze already recorded (replay).",
			"error.presentation_missing":              "Calendar response missing backend presentation contract.",
			"reason.calendar_subworkstream_disabled":  "Sub-workstream filters need backend event_type support; this row is the module context.",
			"calendar.rhythm.title.all":               "Vaccination operating rhythm",
			"calendar.rhythm.note.all":                "from the SOP handbook - all owner lanes",
			"calendar.rhythm.title.pc":                "Preventive Care (PC) vaccination rhythm",
			"calendar.rhythm.note.pc":                 "plan drives - prepare teams - execute proof",
			"calendar.rhythm.title.inventory":         "Vaccination stock readiness rhythm",
			"calendar.rhythm.note.inventory":          "stock, FEFO, cold-chain, reorder, GRN",
			"calendar.rhythm.title.admin_data_ops":    "Vaccination governance rhythm",
			"calendar.rhythm.note.admin_data_ops":     "activation review, config approval, import, audit follow-up",
			"calendar.week.title.all":                 "This week",
			"calendar.week.scope_only.all":            "",
			"calendar.week.title.pc":                  "Preventive Care (PC) this week",
			"calendar.week.scope_only.pc":             "Showing Preventive Care (PC) work only.",
			"calendar.week.title.inventory":           "Inventory / Stock this week",
			"calendar.week.scope_only.inventory":      "Showing Inventory / Stock work only.",
			"calendar.week.title.admin_data_ops":      "Admin / Data Ops this week",
			"calendar.week.scope_only.admin_data_ops": "Showing Admin / Data Ops work only.",
			"calendar.week.clear_scope":               "all owner lanes",
			"calendar.week.whole_period":              "Showing whole week.",
			"calendar.week.all_days_selected":         "All week",
			"calendar.week.clear_day":                 "whole week",
			"calendar.week.empty":                     "No due work scheduled",
			"calendar.week.day_empty":                 "No drives scheduled",
			"calendar.week.rest_day":                  "Rest day · no drives",
			"calendar.week.reminder_title":            "Reminders & escalation",
			"calendar.week.reminder_empty":            "No reminders scheduled in this scope.",
			"calendar.week.reminder_note":             "Reminders, nudges, snoozes, and escalations are durable backend kernel state. Open an event to act.",
			// DRV-005: reminder_rail is a backend-computed, whole-filtered-week summary (see
			// domain.CalendarReminderRail / calendarReminderRailSQL) so this empty_message is the
			// backend-owned copy for reminder_rail.empty_message, never a frontend-invented fallback.
			"calendar.reminder_rail.empty_message": "No reminders scheduled in this scope.",
			"calendar.month.as_of_hint":            "month follows the top-bar as-of date",
			"calendar.month.cell_note":             "Each cell shows that day's due-work and completion markers. Tap an event for its rich detail.",
			"calendar.history.empty":               "No accepted completion history in this window.",
			"calendar.history.empty_note":          "Move the as-of date or narrow the owner lane to inspect a different completion window.",
			"calendar.picker.previous_month":       "Previous month",
			"calendar.picker.next_month":           "Next month",
			"calendar.picker.previous_year":        "Previous year",
			"calendar.picker.next_year":            "Next year",
			"calendar.picker.drive_hint":           "drive day",
			"calendar.picker.due_hint":             "due work",
			"calendar.picker.deferred_hint":        "deferred work",
			"calendar.drive.all_day":               "All day",
			"calendar.drive.sheds":                 "Pens",
			"calendar.drive.vaccines":              "Vaccines",
			"calendar.drive.name":                  "Drive name",
			"calendar.drive.total":                 "Drive total",
			"calendar.drive.animals":               "Animals",
			"calendar.drive.doses":                 "Doses",
			"calendar.drive.packets":               "Drive packets",
			"calendar.drive.vaccine_mix":           "Vaccine mix",
			"calendar.drive.shed_coverage":         "Pen coverage",
			// DRV-006: these keys replace hardcoded fallback strings that lived in
			// apps/admin-web/lib/admin-ui-contract.ts (calendar-drive-card.tsx metagrid labels/suffixes).
			"calendar.drive.done_suffix":     "done",
			"calendar.drive.summary_pending": "Drive progress detail not available yet.",
			// DRV-007: option-A completion-ring drive card copy — subtitle vaccine count, ring
			// caption text, and the footer CTA (calendar-drive-card.tsx).
			"calendar.drive.vaccines_suffix":      "vaccines",
			"calendar.drive.of":                   "of",
			"calendar.drive.sheds_done_suffix":    "pens done",
			"calendar.drive.verification_pending": "Verification pending",
			"calendar.drive.owner":                "Owner",
			"calendar.drive.open":                 "Open drive",
			// DRV-008: full-screen drive detail roster labels and breadcrumbs (calendar-drive-detail.tsx).
			"calendar.drive.animal_roster":       "Animal roster",
			"calendar.drive.display_id_header":   "Display ID",
			"calendar.drive.shed_header":         "Pen",
			"calendar.drive.tag_1_header":        "Tag 1",
			"calendar.drive.tag_2_header":        "Tag 2",
			"calendar.drive.stage_header":        "Stage",
			"calendar.drive.lifecycle_header":    "Lifecycle",
			"calendar.drive.health_header":       "Health",
			"calendar.drive.reason_header":       "Reason",
			"calendar.drive.status_header":       "Status",
			"calendar.drive.no_animals":          "No animals in this drive",
			"calendar.drive.load_more":           "Load more",
			"calendar.drive.search_placeholder":  "Search animal, tag, pen, status...",
			"calendar.drive.search_action":       "Search",
			"calendar.drive.clear_search":        "Clear",
			"calendar.drive.previous_page":       "Previous",
			"calendar.drive.next_page":           "Next",
			"calendar.drive.page_label":          "Page",
			"calendar.drive.rows_label":          "rows",
			"calendar.breadcrumb.vaccination":    "Vaccination",
			"calendar.breadcrumb.calendar":       "Calendar",
			"calendar.picker.other_hint":         "other due work",
			"calendar.picker.history_hint":       "completed history",
			"calendar.new_event.label":           "New event",
			"calendar.new_event.disabled_reason": "Calendar events are generated from configured obligations. Create a campaign/catch-up via Config or the Preventive Care (PC) catch-up path - not a free-form Calendar entry.",
			"calendar.empty.ok":                  "Configured vaccination drives, boosters, proof/rework, and stock gates will appear here when due.",
			"calendar.empty.error":               "Calendar is unavailable - resolve the error above, then reload.",
			"calendar.empty.primary":             "Config",
			"calendar.empty.secondary":           "Vaccination",
			"empty.events":                       "No vaccination due work in this scope.",
			"label.display_id":                   "Display ID",
			"label.animal_identifier_1":          "Tag 1",
			"label.animal_identifier_2":          "Tag 2",
			"label.stage":                        "Stage",
			"label.status":                       "Status",
			"label.when":                         "When",
			"label.reminder":                     "Reminder",
			"label.channel":                      "Channel",
			"label.escalates":                    "Escalates",
			"label.not_configured":               "not configured",
			"label.channels":                     "Channels",
			"label.scope":                        "Scope",
			"label.park_shed":                    "Park · Pen",
			"label.cohort_target":                "Cohort · Target",
			"label.vaccine_dose":                 "Vaccine · Dose",
			"label.owner":                        "Owner",
			"label.all_sheds":                    "all pens",
			"label.source_backed_rule":           "Rule / version",
			"label.source_backed":                "versioned",
			"label.not_source_backed":            "manual row",
			"label.execution":                    "Execution",
			"label.stock_readiness":              "Stock readiness",
			"label.proof":                        "Proof",
			"label.verification":                 "Verification",
			"label.recent_activity":              "Recent activity",
			"label.linked":                       "Linked",
			"label.placeholder":                  "—",
			"label.more":                         "more",
		}
	case "vaccination-live-tracker":
		// Every visible string on /vaccination/live-tracker originates here. The page renders no
		// English literal of its own, so a missing key throws at render rather than shipping a blank
		// cell — that is deliberate.
		return map[string]string{
			"crumb":                                 "Preventive Care (PC) · Vaccination",
			"page.heading_prefix":                   "Drive Day",
			"chip.parks_running_one":                "park running",
			"chip.parks_running_many":               "parks running",
			"chip.parks_active_one":                 "park active",
			"chip.parks_active_many":                "parks active",
			"action.full_schedule":                  "Full Schedule",
			"action.command_board":                  "Command Board",
			"action.open_verify":                    "Open Verify →",
			"action.all_combo_animals":              "All combo animals →",
			"action.reset_filters":                  "Clear all filters",
			"action.retry":                          "Retry",
			"kpi.scheduled.label":                   "Scheduled today",
			"kpi.scheduled.detail":                  "administrations",
			"kpi.proofs.label":                      "Proofs received",
			"kpi.proofs.detail":                     "administrations proofed",
			"kpi.scans.label":                       "RFID confirmed",
			"kpi.scans.detail":                      "administrations with a scan",
			"kpi.remaining.label":                   "Remaining",
			"kpi.remaining.detail":                  "administrations not yet closed",
			"kpi.remaining.awaiting_prefix":         "proofed, awaiting close",
			"kpi.combo.label":                       "Combo animals",
			"kpi.combo.detail":                      "1 proof → multiple obligations",
			"kpi.attention.label":                   "Attention",
			"kpi.attention.detail":                  "extra attempts · idle operator",
			"kpi.live_tick":                         "▲ live",
			"kpi.cross_filter_disabled":             "Tile cross-filtering is not wired. Use the filter bar above to narrow every section at once.",
			"kpi.truncated_note":                    "This drive day exceeds the tracker's per-read row budget. The totals above are still exact — they are aggregated over the whole day, not over the visible rows — but the tables below list only part of it. Narrow by park, pen or vaccine to see every row.",
			"live.badge_live":                       "LIVE",
			"live.badge_paused":                     "PAUSED",
			"live.toggle_title":                     "Click to pause or resume live updates",
			"live.updated_prefix":                   "Updated",
			"live.updated_suffix":                   "IST",
			"live.interval_label":                   "Refresh interval",
			"live.stale_prefix":                     "Live updates paused — data shown as of",
			"live.stale_suffix":                     "IST. Click LIVE to resume.",
			"live.newest_first":                     "newest first",
			"live.feed_rate_suffix":                 "/min",
			"live.feed_rate_unavailable":            "rate pending",
			"live.scan_rate_suffix":                 "animals/min recent",
			"live.scanned_label":                    "scanned",
			"live.proofed_label":                    "proofed",
			"live.events_label":                     "events",
			"live.feed_aria":                        "Live vaccination activity",
			"filter.apply_note":                     "Park, vaccine, operator and pen narrow the tiles, both tables, the combo card and the live feed. The Verification queue carries no vaccine or operator column, so it follows park and pen only. Status narrows the tiles, both tables and Attention; the combo card and the feed always show the whole drive day.",
			"filter.unlisted_selection":             "current filter — no drive work on this day",
			"filter.truncated_note":                 "The filter lists are capped server-side and this drive day exceeds one of them, so some options are not offered.",
			"filter.all_parks":                      "All parks",
			"filter.all_vaccines":                   "All vaccines",
			"filter.all_operators":                  "All operators",
			"filter.all_sheds":                      "All pens",
			"filter.all_statuses":                   "All statuses",
			"filter.park_label":                     "Park",
			"filter.vaccine_label":                  "Vaccine",
			"filter.operator_label":                 "Operator",
			"filter.shed_label":                     "Pen",
			"filter.status_label":                   "Status",
			"filter.clear_all":                      "clear all",
			"filter.remove_one":                     "Remove filter",
			"section.operators.title":               "Operators — live",
			"section.operators.count_suffix_one":    "operator",
			"section.operators.count_suffix":        "operators",
			"section.operators.park_suffix_one":     "park",
			"section.operators.park_suffix":         "parks",
			"section.operators.drilldown_note":      "Pen drill-down opens from the Pens table below",
			"section.operators.empty_title":         "No operator has drive work on this day",
			"section.operators.empty_body":          "Operator rows appear once the day's vaccination drive assignments exist for a pen in scope.",
			"section.operators.filtered_title":      "No operator matches these filters",
			"section.operators.filtered_body":       "Clear a filter to see the other operators on this drive day.",
			"section.operators.unavailable":         "Operator display code is not seeded in this environment.",
			"section.operators.truncated_note":      "Operator rows are capped server-side, so this table sums lower than the tiles above. When the rollup itself is capped the tiles say so in their own note.",
			"section.operators.unassigned_note":     "administrations on this drive day resolved to no operator assignment. They are counted in the tiles and listed under Pens below, but they have no operator to be attributed to and appear in no row here.",
			"section.operators.now_at_prefix":       "last activity",
			"section.operators.idle_prefix":         "idle",
			"section.operators.idle_suffix":         "min",
			"section.sheds.title":                   "Pens — proof progress",
			"section.sheds.empty_title":             "No pen has drive work on this day",
			"section.sheds.empty_body":              "Pen rows appear once the day's obligations resolve to a pen in scope.",
			"section.sheds.filtered_title":          "No pen matches these filters",
			"section.sheds.filtered_body":           "Clear a filter to see the other pens on this drive day.",
			"section.sheds.truncated_note":          "Pen rows are capped server-side, so this table sums lower than the tiles above. When the rollup itself is capped the tiles say so in their own note.",
			"section.combo.title":                   "Combo doses — one proof, multiple obligations",
			"section.combo.count_suffix_one":        "animal today",
			"section.combo.count_suffix":            "animals today",
			"section.combo.note":                    "When a pen gets a combo day, each animal can receive multiple administrations in one handling. The operator scans once and uploads one video proof per animal, so that proof stands as evidence for every same-day obligation on that animal — but each obligation is only closed when its own record reaches completed, which is what Remaining counts. Tiles above are at administration grain: a combo animal contributes one count per scheduled dose to Scheduled and Proofs received when its single proof lands. Animals and administrations are never mixed in one number.",
			"section.combo.empty_title":             "No combo animal on this drive day",
			"section.combo.empty_body":              "A row appears when one animal carries two or more distinct vaccination obligations on the same day.",
			"section.combo.all_listed":              "Every combo animal on this drive day is already listed.",
			"section.combo.truncated_reason":        "Only the first 200 combo animals are listed. A full combo-animal list is not built on this surface yet, so this control cannot open one.",
			"section.combo.header_animal":           "Animal",
			"section.combo.header_shed":             "Pen",
			"section.combo.header_proof":            "Proof",
			"section.combo.header_doses":            "Doses",
			"section.activity.title":                "Live activity",
			"section.activity.empty_title":          "No field activity yet on this drive day",
			"section.activity.empty_body":           "Rows appear as video proofs land, RFID scans are captured, and obligations are closed.",
			"section.activity.truncated_note":       "Newest events only — older activity on this drive day is not shown here.",
			"section.attention.title":               "Attention",
			"section.attention.empty":               "Nothing needs attention on this drive day.",
			"section.attention.truncated_note":      "Attention rows are capped server-side; the count above is the full total.",
			"section.attention.elapsed_suffix":      "min idle",
			"section.attention.nudge":               "Nudge dispatch is not recorded against drive operators.",
			"section.attention.escalation":          "An idle-escalation deadline is not configured for drive operators.",
			"section.attention.pace":                "Pen close time is not configured, so a finish estimate cannot be computed.",
			"section.attention.nudge_label":         "Nudge — not recorded",
			"section.attention.escalate_label":      "Escalation — not configured",
			"section.attention.pace_label":          "Finish estimate — not configured",
			"section.verification.title":            "Verification queue",
			"section.verification.badge":            "post-drive",
			"section.verification.awaiting":         "Awaiting verifier review (all dates)",
			"section.verification.verified":         "Verified today",
			"section.verification.rework":           "Rework requested",
			"section.verification.sheds_suffix_one": "pen",
			"section.verification.sheds_suffix":     "pens",
			"drawer.passport.aria":                  "Goat Passport",
			"drawer.passport.close_label":           "Close Goat Passport",
			"drawer.passport.tag_1":                 "Tag 1",
			"drawer.passport.tag_2":                 "Tag 2",
			"drawer.passport.shed":                  "Pen",
			"drawer.passport.next_due":              "Next due",
			"drawer.passport.no_upcoming":           "No upcoming dose",
			"drawer.passport.open_obligations":      "Open obligations",
			"drawer.passport.col_due":               "Due",
			"drawer.passport.col_dose":              "Dose",
			"drawer.passport.col_status":            "Status",
			"drawer.passport.more_suffix":           "more not shown",
			"drawer.passport.empty_open":            "No open vaccination obligation for this animal.",
			"drawer.passport.unavailable_prefix":    "Vaccination passport is unavailable",
			"drawer.passport.full_history":          "Full change history",
			"label.park_scope_note":                 "Park scope also follows the top bar.",
			"label.as_of_note":                      "The top bar's as-of date does not move this page. This is a live DRIVE DAY board, reconstructed from the day's own canonical rows; it is showing the drive day named in the heading.",
			"legend.label":                          "Legend",
			"state.error_title":                     "Live drive tracker is unavailable",
			"state.error_body":                      "The vaccination live tracker service did not return data. Resolve the error below, then reload.",
			"state.empty_title":                     "No vaccination drive work on this day",
			"state.empty_body":                      "Every section is shown at zero. Rows appear once the day's obligations, drive assignments and field evidence exist.",
			"state.empty_filtered_title":            "No vaccination drive work matches the current scope and filters",
			"state.empty_filtered_body":             "This is not a statement about the whole drive day — a park, pen, vaccine, operator or status narrowing is active. Clear it to see the rest of the day.",
		}
	case "vaccination":
		return map[string]string{
			"crumb":                                       "Preventive Care (PC) · operations",
			"section.drive_flow.aria":                     "How a vaccination drive runs",
			"section.supplier_warmup.aria":                "Supplier warmup loads",
			"section.status_matrix.title":                 "Vaccination status matrix",
			"section.status_matrix.aria":                  "Vaccination status matrix",
			"section.status_matrix.note":                  "Cells are keyed on each cohort's open obligations; last dose is the latest accepted administered dose. Overdue cells escalate via Protocol Adherence; act on individual drives in the Action Center.",
			"section.status_matrix.empty_title":           "No cohort × vaccine status yet",
			"section.status_matrix.unavailable_title":     "Status matrix is unavailable",
			"section.status_matrix.empty_body":            "Columns are the published vaccination protocols; rows are park/pen cohorts. Publish a plan in Preventive Care / Vaccination plan — obligations then generate against cohorts and fill this grid.",
			"section.status_matrix.unavailable_body":      "Operations are unavailable until the service responds; resolve the error above and reload.",
			"section.status_matrix.row_hint":              "click a cell → work context",
			"section.cohort_detail.title":                 "Per-cohort vaccination detail",
			"section.cohort_detail.aria":                  "Per-cohort vaccination detail",
			"section.cohort_detail.note":                  "Status is keyed on the cohort's open obligations + interval. Last dose is the latest accepted administered dose for the cohort; animal counts drive dose quantities and FEFO stock reserves on verify.",
			"section.cohort_detail.badge":                 "animals · age band · last dose · next due",
			"section.cohort_detail.row_hint":              "click a row → work context",
			"section.cohort_detail.empty":                 "No cohorts with vaccination obligations yet. Rows appear per park/pen cohort once a protocol is published and drives generate.",
			"section.cohort_detail.unavailable":           "Cohort detail is unavailable until the service responds; resolve the error above and reload.",
			"section.shed_events.title":                   "Drive — pen events",
			"section.shed_events.aria":                    "Vaccination pen events",
			"section.shed_events.note":                    "park → pen → drive · stock · proof · verify",
			"section.shed_events.row_hint":                "click a row → pen execution detail",
			"section.shed_events.empty_unavailable_title": "Park/pen execution is unavailable",
			"section.shed_events.empty_unavailable_body":  "The vaccination execution service did not return data. Resolve the error above, then reload.",
			"section.shed_events.empty_none_title":        "No park/pen execution rows yet",
			"section.shed_events.empty_none_body":         "Rows appear once a published vaccination drive generates obligations against a park / pen cohort.",
			"section.shed_events.empty_filtered_title":    "No pen work matches these filters",
			"section.shed_events.empty_filtered_body":     "Clear a filter to see other parks and pens.",
			"section.supplier_warmup.title":               "Supplier warmup — Holding Farm",
			"section.supplier_warmup.auth_tag":            "source-entry auth required",
			"section.supplier_warmup.auth_body":           "Sign in again to view Holding-Farm vaccination evidence.",
			"section.supplier_warmup.unavailable_tag":     "source-entry unavailable",
			"section.supplier_warmup.badge":               "journey starts at purchase",
			"section.supplier_warmup.note":                "Purchased goats start at the supplier / holding farm. HF doses import as completion evidence so accepted-intake goats do not double-dose on arrival. Source Entry owns the write actions; Preventive Care (PC) reads the evidence here.",
			"section.supplier_warmup.lifecycle":           "Lifecycle: purchase → supplier warmup (tag + vaccinate · rejectable) → load → arrival → accepted intake → Preventive Care (PC) obligations. Rejected-before-truck goats never enter park count or active vaccination work.",
			"section.supplier_warmup.row_hint":            "click a load → actions",
			"filter.cohort.title":                         "Filter — Per-cohort vaccination detail",
			"filter.cohort.search":                        "Search cohort vaccination rows",
			"filter.cohort.reason":                        "Search cohort, age band, dose, due date, status...",
			"filter.cohort.filter_reason":                 "Use visible-row search and quick facets; click a status to open the cohort record.",
			"filter.cohort.rows_suffix":                   "animals, dose history, next due",
			"filter.status_matrix.title":                  "Filter — Vaccination status matrix",
			"filter.status_matrix.search":                 "Search vaccination matrix",
			"filter.status_matrix.reason":                 "Search cohort, protocol, vaccine, status...",
			"filter.status_matrix.filter_reason":          "Use visible-row search and quick facets; click a cell to open the matching work context.",
			"filter.status_matrix.rows_suffix":            "cohort × protocol",
			"filter.supplier.title":                       "Filter — Supplier warmup",
			"filter.supplier.search":                      "Search supplier warmup loads",
			"filter.supplier.reason":                      "Search holding farm, supplier, purpose, status...",
			"filter.supplier.filter_reason":               "Use visible-row search and quick facets here; open Source Entry for the full load workflow.",
			"filter.supplier.rows_suffix":                 "source loads and HF evidence",
			"filter.shed_events.title":                    "Filter — Vaccination pen events",
			"filter.shed_events.search":                   "Search vaccination pen events",
			"filter.shed_events.reason":                   "Search pen, owner, proof, status...",
			"filter.shed_events.filter_reason":            "Use visible-row search, quick facets, severity chips, and work-state chips on this board.",
			"filter.shed_events.rows_suffix":              "park, pen, owner, proof, verify",
			"section.sheds.title":                         "Vaccination by pen",
			"section.sheds.note":                          "Current adult vaccination status by pen",
			"section.sheds.empty_none_title":              "No pens with vaccination work yet",
			"section.sheds.empty_none_body":               "Rows appear per pen once a published vaccination protocol generates obligations against the pen's animals.",
			"section.sheds.empty_filtered_title":          "No pens match these filters",
			"section.sheds.empty_filtered_body":           "Clear a filter to see other parks, pens, statuses, and capacity states.",
			"section.sheds.unavailable_title":             "Pen-wise vaccination is unavailable",
			"section.sheds.unavailable_body":              "The pen summary service did not return data. Resolve the error above, then reload.",
			"inventory_progress.title":                    "Vaccine fridge stock checks",
			"section.inventory_progress.title":            "Vaccine fridge stock checks",
			"inventory_progress.loading":                  "Loading inventory task progress...",
			"inventory_progress.unavailable":              "Inventory task progress is unavailable right now.",
			"inventory_progress.subtitle":                 "PC Care director tasks",
			"inventory_progress.metric.current":           "Current or carry-over",
			"inventory_progress.metric.overdue":           "Overdue",
			"inventory_progress.metric.verifier":          "Verifier review",
			"inventory_progress.metric.done_today":        "Done today",
			"inventory_progress.empty":                    "No current or carry-over vaccine inventory tasks.",
			"inventory_progress.empty_requirements":       "No vaccine stock requirement lines",
			"inventory_progress.time_zone":                "Asia/Kolkata",
			"section.full_schedule.title":                 "Full vaccine schedule",
			"section.full_schedule.note":                  "Planned vaccination drives for the selected month.",
			"section.full_schedule.operator_title":        "Operator drive schedule",
			"section.full_schedule.operator_note":         "Planned vaccination drives split by operator capacity and grouped by physical pen totals.",
			"section.full_schedule.loading_operator_note": "Loading planned operator assignments.",
			"section.full_schedule.empty_title":           "No schedule rows for this month",
			"section.full_schedule.empty_body":            "Rows appear once due work is clubbed into vaccination drives for the selected month.",
			"section.full_schedule.no_assignments_title":  "No operator drive rows",
			"section.full_schedule.no_assignments_body":   "No persisted operator assignments exist for the selected month.",
			// Human labels for the operator-drive-schedule CAPACITY pill. The read
			// model emits the internal machine state (within_cap / over_cap /
			// over_cap_required / capacity_breach); never render that token raw.
			"schedule.capacity.within_cap":                       "Within cap",
			"schedule.capacity.over_cap":                         "Split",
			"schedule.capacity.over_cap_required":                "Over capacity",
			"schedule.capacity.capacity_action":                  "Capacity action",
			"schedule.capacity.capacity_breach":                  "Capacity action",
			"section.full_schedule.unavailable_title":            "Full vaccine schedule is unavailable",
			"section.full_schedule.unavailable_body":             "The vaccination operations service did not return data. Resolve the error above, then reload.",
			"section.full_schedule.assignment_unavailable_title": "Drive schedule unavailable",
			"section.full_schedule.stale_title":                  "Schedule rebuild in progress",
			"section.full_schedule.stale_body":                   "Showing the last good materialized month while new vaccination changes are being rebuilt.",
			"section.full_schedule.next_rows":                    "Next rows",
			"schedule.legend.aria":                               "Full vaccine schedule status legend",
			"schedule.kpi.parks":                                 "Parks covered",
			"schedule.kpi.sheds":                                 "Pens in drives",
			"schedule.kpi.animals":                               "Animals in drives",
			"schedule.kpi.animals_assigned":                      "Animals assigned",
			"schedule.kpi.drive_rows":                            "Operator days",
			"schedule.kpi.overdue_drives":                        "Overdue drives",
			"schedule.column.date":                               "Date",
			"schedule.column.operator":                           "Operator",
			"schedule.column.park":                               "Park",
			"schedule.column.shed":                               "Pen",
			"schedule.column.sheds":                              "Pens",
			"schedule.column.partition":                          "Partition",
			"schedule.column.animals":                            "Animals",
			"schedule.column.vaccines":                           "Vaccines",
			"schedule.column.workload":                           "Workload",
			"schedule.column.total_doses":                        "Total doses",
			"schedule.column.capacity":                           "Capacity",
			"schedule.column.next_due":                           "Next due",
			"schedule.column.postpone":                           "Move date",
			"schedule.column.status":                             "Status",
			// Command board: CEO KPI summary, cohort matrix, verification queue
			"section.command_board.title":                    "Command Board",
			"section.command_board.loading":                  "Loading...",
			"section.command_board.unavailable":              "Unable to load command board",
			"command_board.kpi.targets":                      "Animals",
			"command_board.kpi.targets_dl":                   "Distinct animals in program",
			"command_board.kpi.missed":                       "Missed",
			"command_board.kpi.missed_dl":                    "Dose window closed unvaccinated",
			"command_board.kpi.rework_needed":                "Rework needed",
			"command_board.kpi.rework_dl":                    "Proof rejected",
			"command_board.shed_vaccine.title":               "Pen × Vaccine",
			"command_board.shed_vaccine.meta":                "Red = goats not vaccinated yet, past their due date. All doses of that vaccine counted together. Click a red box to see which goats.",
			"command_board.shed_vaccine.column.shed":         "Pen",
			"command_board.shed_vaccine.state.behind":        "Goats not done",
			"command_board.shed_vaccine.cell.behind_unit":    "goats",
			"command_board.shed_vaccine.state.ok":            "All done",
			"command_board.shed_vaccine.state.not_planned":   "Not given in this pen",
			"command_board.shed_vaccine.summary_behind":      "pens have goats pending",
			"command_board.shed_vaccine.summary_clean":       "Every pen is up to date on every vaccine",
			"command_board.shed_vaccine.drawer.behind_of":    "behind, of",
			"command_board.shed_vaccine.drawer.column.due":   "Was due",
			"command_board.shed_vaccine.cell.verifying_unit": "pending",
			"command_board.shed_vaccine.state.verifying":     "Video check pending",
			"command_board.shed_vaccine.cell.rework_unit":    "rework",
			"command_board.shed_vaccine.state.rework":        "Rework needed",
			"command_board.shed_vaccine.drawer.verifying_of": "given and waiting for video check, of",
			"command_board.shed_vaccine.drawer.rework_of":    "with rejected proof needing rework, of",
			"command_board.shed_vaccine.drawer.no_video":     "No video uploaded",
			"command_board.shed_vaccine.drawer.shed_videos":  "Pen video",
			"command_board.shed_vaccine.drawer.clip":         "Clip",
			"command_board.shed_vaccine.drawer.truncated":    "Showing the longest-waiting animals only — the count above is the full figure.",
			"command_board.pending_sheds.title":              "Pending vaccines by pen",
			"command_board.pending_sheds.meta":               "Overdue, rejected-proof rework, and verification-pending vaccines, grouped by pen.",
			"command_board.pending_sheds.empty":              "No pen has a pending vaccine in this scope.",
			"command_board.pending_sheds.column.shed":        "Pen",
			"command_board.pending_sheds.column.park":        "Park",
			"command_board.pending_sheds.column.vaccines":    "Pending vaccines",
			"command_board.pending_sheds.state.behind":       "overdue",
			"command_board.pending_sheds.state.verifying":    "awaiting verification",
			"command_board.pending_sheds.state.rework":       "rework needed",
			"command_board.kpi.verified":                     "Verified",
			"command_board.kpi.verified_dl":                  "Operator done + verifier accepted",
			"command_board.kpi.awaiting_verification":        "Awaiting Verification",
			"command_board.kpi.awaiting_dl":                  "Given · proof uploaded · in queue",
			"command_board.kpi.overdue":                      "Overdue",
			"command_board.kpi.overdue_dl":                   "Not given",
			"command_board.kpi.scheduled_ahead":              "Scheduled Ahead",
			"command_board.kpi.scheduled_dl":                 "Future drives",
			"command_board.cohort_matrix.title":              "Cohort Vaccine Matrix",
			// The cohort grid loads after the rest of the board (its three reads alone exceeded the
			// endpoint's latency budget), so these three cover the gap and its two failure modes.
			// An empty grid during the wait would read as "this tenant has no cohorts", which is a
			// different and far more alarming fact than "still loading".
			"command_board.cohort_matrix.loading": "Loading cohort matrix…",
			// The shed grid is likewise loaded after first paint. Interning its payload cut it from
			// 408KB to 155KB and the board's p90 barely moved, so the section itself had to move.
			"command_board.shed_dose_matrix.loading":           "Loading pen matrix…",
			"command_board.shed_dose_matrix.unavailable":       "Pen matrix is unavailable right now. The rest of the board is up to date.",
			"command_board.shed_dose_matrix.empty":             "No pen obligations in this scope",
			"command_board.cohort_matrix.unavailable":          "Cohort matrix is unavailable right now. The rest of the board is up to date.",
			"command_board.cohort_matrix.column.stage":         "Stage",
			"command_board.cohort_matrix.column.sex":           "Sex",
			"command_board.cohort_matrix.column.animals":       "Animals",
			"command_board.cohort_matrix.column.vaccine":       "Vaccine",
			"command_board.cohort_matrix.column.pending":       "Pending",
			"command_board.cohort_matrix.meta":                 "Per farm · per dose: pending / submitted / verified, with the actual operator vaccination date or date range · red when the operator still owes work",
			"command_board.cohort_matrix.empty":                "No cohort obligations in this scope",
			"command_board.cohort_matrix.no_farm":              "Farm not set",
			"command_board.cohort_matrix.pending_word":         "pending",
			"command_board.cohort_matrix.rework_word":          "rework needed",
			"command_board.cohort_matrix.submitted_word":       "submitted",
			"command_board.cohort_matrix.verified_word":        "verified",
			"command_board.cohort_matrix.date_unavailable":     "Date unavailable",
			"command_board.cohort_matrix.note":                 "Rows are cohort status by dose. Historical rows without a drive batch are shown by administration date, not drive.",
			"command_board.cohort_matrix.row_hint":             "Select a cell for the cohort breakdown",
			"command_board.cohort_matrix.detail.title":         "Cell detail",
			"command_board.cohort_matrix.detail.empty":         "Select a cell to see its cohort breakdown",
			"command_board.cohort_matrix.detail.animals":       "Animals in cohort",
			"command_board.cohort_matrix.detail.dates":         "Operator vaccination date",
			"command_board.cohort_matrix.detail.breakdown":     "Sub-cohorts",
			"command_board.cohort_matrix.detail.close":         "Close",
			"command_board.cohort_matrix.detail.per_day":       "Vaccination days",
			"command_board.cohort_matrix.detail.animals_word":  "animals",
			"command_board.cohort_matrix.detail.capped":        "Showing the first 25 animals of",
			"command_board.cohort_matrix.unbatched":            "historical / unbatched",
			"command_board.kpi.closed_without_dose_open":       "Open the animals whose work closed with no dose",
			"command_board.kpi.closed_without_dose_empty":      "No animals closed without a dose in this scope",
			"command_board.closed_drawer.animals_word":         "animals",
			"command_board.closed_drawer.column.animal":        "Animal",
			"command_board.closed_drawer.column.location":      "Location",
			"command_board.closed_drawer.column.vaccine":       "Vaccine",
			"command_board.closed_drawer.column.reason":        "Why no dose",
			"command_board.closed_drawer.capped":               "Showing the first 50 animals of",
			"command_board.filter.vaccine":                     "Vaccine",
			"command_board.filter.all_vaccines":                "All vaccines",
			"command_board.filter.drive":                       "Drive",
			"command_board.filter.all_drives":                  "All drives",
			"command_board.filter.operator_day":                "Operator day (optional)",
			"command_board.filter.all_common_drives":           "All common drives",
			"command_board.filter.completed_history":           "Completed history",
			"command_board.filter.no_drives":                   "No drives planned in this park scope yet",
			"command_board.future_drives.title":                "Scheduled Ahead — Future Vaccination Drives",
			"command_board.future_drives.count_suffix":         "drives",
			"command_board.future_drives.lines_suffix":         "treatment lines",
			"command_board.future_drives.column.campaign":      "Common drive",
			"command_board.future_drives.column.drive":         "Vaccination",
			"command_board.future_drives.column.dates":         "Operator dates",
			"command_board.future_drives.column.sheds":         "Whole pens",
			"command_board.future_drives.column.animals":       "Animals",
			"command_board.future_drives.column.doses":         "Vaccinations",
			"command_board.future_drives.campaign_animals":     "animals",
			"command_board.future_drives.campaign_doses":       "vaccinations",
			"command_board.shed_matrix.title":                  "Vaccine × Pen Status",
			"command_board.shed_matrix.meta":                   "Count of animals · per dose · waiting days on amber",
			"command_board.shed_matrix.waiting_suffix":         "d waiting",
			"command_board.shed_matrix.column.shed":            "Pen",
			"command_board.shed_matrix.column.dose":            "Dose",
			"command_board.shed_matrix.column.state":           "Status",
			"command_board.shed_matrix.column.animals":         "Animals",
			"command_board.shed_matrix.column.window":          "Dates",
			"command_board.shed_matrix.state.verified":         "Verified",
			"command_board.shed_matrix.state.awaiting":         "Awaiting verification",
			"command_board.shed_matrix.state.rework":           "Rework needed",
			"command_board.shed_matrix.state.overdue":          "Action needed",
			"command_board.shed_matrix.state.scheduled":        "Scheduled",
			"command_board.shed_matrix.legend.verified":        "Verified (actual date shown)",
			"command_board.shed_matrix.legend.awaiting":        "Given · awaiting verification",
			"command_board.shed_matrix.legend.rework":          "Rejected proof (rework needed)",
			"command_board.shed_matrix.legend.overdue":         "Action needed (not given by due date)",
			"command_board.shed_matrix.legend.scheduled":       "Scheduled (date shown)",
			"command_board.shed_matrix.legend.not_scoped":      "Not in protocol scope",
			"command_board.verification_queue.title":           "Verification Queue — Pending Closures",
			"command_board.verification_queue.meta":            "Given by operator · proof uploaded",
			"command_board.verification_queue.column.shed":     "Pen",
			"command_board.verification_queue.column.dose":     "Dose",
			"command_board.verification_queue.column.awaiting": "Awaiting Verify",
			"command_board.verification_queue.column.total":    "Total",
			"command_board.verification_queue.column.status":   "Status",
			"command_board.verification_queue.column.days":     "In Queue",
			"command_board.verification_queue.status.awaiting": "Awaiting verification",
			"schedule.partition.prefix":                        "Part",
			"schedule.partition.whole_shed":                    "Whole pen",
			"schedule.unit.animal":                             "animal",
			"schedule.unit.animals":                            "animals",
			"schedule.unit.dose":                               "dose",
			"schedule.unit.doses":                              "doses",
			"schedule.unit.shed":                               "pen",
			"schedule.unit.sheds":                              "pens",
			"schedule.load.batches":                            "batches",
			"schedule.load.deferred_short":                     "def.",
			"schedule.load.scheduled_short":                    "sched.",
			"schedule.load.single_drive":                       "single drive",
			"schedule.drawer.title":                            "Drive pens",
			"schedule.drawer.open_sheds":                       "Open pen list",
			"schedule.drawer.open_roster":                      "Open roster",
			"schedule.drawer.more":                             "more",
			"schedule.drawer.less":                             "Show less",
			"schedule.drawer.close":                            "Close pen list",
			"schedule.drawer.search":                           "Search pens...",
			"schedule.drawer.search_action":                    "Search",
			"schedule.drawer.previous_page":                    "Previous",
			"schedule.drawer.next_page":                        "Next",
			"schedule.drawer.page_label":                       "Page",
			"schedule.drawer.rows_label":                       "rows",
			"schedule.drawer.empty":                            "No pens match this search.",
			"schedule.move.open":                               "Move",
			"schedule.move.title":                              "Move vaccine date",
			"schedule.move.close":                              "Close move date",
			"schedule.move.recorded_title":                     "Move recorded",
			"schedule.move.recorded_body":                      "The backend accepted the vaccine date override. The planner will recalculate assignments from the new vaccine date.",
			"schedule.move.error_title":                        "Move failed",
			"schedule.move.error_body":                         "The backend rejected the date move. Check the vaccine/date and try again.",
			"schedule.move.missing_title":                      "Pick a date",
			"schedule.move.missing_body":                       "Choose the vaccine and new drive date before moving.",
			"schedule.move.date_placeholder":                   "Select date",
			"schedule.move.previous_month":                     "Previous month",
			"schedule.move.next_month":                         "Next month",
			"schedule.move.invalid_future_date":                "Pick a date after {date}.",
			"schedule.state.deferred_title":                    "Drive has deferred work.",
			"schedule.state.overdue_title":                     "Drive has overdue work.",
			"schedule.state.due_title":                         "Drive has due work.",
			"schedule.state.completed_title":                   "Drive is fully completed.",
			"schedule.state.scheduled_title":                   "Drive is scheduled.",
			"schedule.row.open_title":                          "Open pen schedule detail",
			"schedule.row_type.adult":                          "Adult course",
			"schedule.row_type.kid":                            "Kid course",
			"schedule.row_type.fallback":                       "Cohort",
			"schedule.cell.no_record":                          "—",
			"schedule.cell.no_record_title":                    "No vaccine record for this pen/type in the selected month.",
			"schedule.cell.outside_year_title":                 "The next due or last dose date is outside the selected month.",
			"schedule.cell.overdue_title":                      "Overdue, missed, or rejected vaccination work.",
			"schedule.cell.due_title":                          "Due soon or waiting for proof / verification.",
			"schedule.cell.scheduled_title":                    "Drive scheduled or in progress.",
			"schedule.cell.up_to_date_title":                   "Accepted or completed vaccination record.",
			"filter.sheds.search":                              "Search park or pen",
			"filter.sheds.title":                               "Filter — Vaccination by pen",
			"filter.sheds.reason":                              "Search park or pen name...",
			"filter.sheds.filter_reason":                       "Park, pen, status, and capacity filters apply server-side; search matches park or pen name.",
			"filter.sheds.rows_suffix":                         "pens, animal counts, sessions, capacity",
			"label.done":                                       "done",
			"label.all_status":                                 "All status",
			"label.all_capacity":                               "All capacity",
			"label.sheds_noun":                                 "pens",
			"label.shed_noun":                                  "pen",
			"status.scheduled_drive":                           "Drive scheduled",
			"status.no_work_due":                               "No work due",
			"label.manager_unassigned":                         "Manager: unassigned",
			"label.backup_unassigned":                          "Backup: unassigned",
			"action.open_full_schedule":                        "Full Schedule",
			"action.open_shed_board":                           "Pen board",
			"action.next_year":                                 "Next year",
			"tooltip.sessions.label":                           "About planned sessions",
			"tooltip.sessions.body":                            "Mesha splits a pen's vaccination work across multiple days when the daily limit is reached. Sessions is the number of planned visit days (usually 1). One animal getting two vaccines (e.g. FMD + HS) counts as two vaccinations, not one.",
			"note.sheds_counts":                                "Current status by pen. Up to date means no vaccination is currently due; actual and future vaccination dates are shown above.",
			"drawer.record_verify.title":                       "Vaccination work context",
			"drawer.record_verify.aria":                        "Vaccination work context",
			"drawer.record_verify.close_label":                 "Close record / verify drawer",
			"drawer.record_verify.eyebrow":                     "VACCINE",
			"drawer.record_verify.note":                        "Generated vaccination context for the selected cohort and vaccine. Execution happens through the SOP task when the row has a real task/completion handle.",
			"drawer.record_verify.record_reason":               "This panel is read-only unless a backing SOP task exists. Batch, dose, cold-chain and proof are captured on the operator SOP task, then verification accepts or sends the completion to rework.",
			"drawer.record_verify.verify_reason":               "Verify accepts/rejects one recorded dose by completion_id — exposed only by /vaccination/verification-queue (per goat), never on this cohort rollup. Act on the real obligation/completion rows in the Action Center.",
			"drawer.record_verify.proof_reason":                "Uploaded by the field worker in the SOP task — no camera capture on web. Not attachable here: this surface has no task_id for /app/proofs/* + /app/tasks/{task_id}/submissions.",
			"drawer.record_verify.no_obligations":              "No open obligations for this cohort.",
			"drawer.record_verify.all_protocols":               "All cohort protocols",
			"drawer.record_verify.form.cohort_shed":            "Cohort / pen",
			"drawer.record_verify.form.vaccine":                "Vaccine",
			"drawer.record_verify.form.batch":                  "Batch (FEFO) · lot",
			"drawer.record_verify.form.batch_placeholder":      "lot...",
			"drawer.record_verify.form.batch_value":            "selected during SOP execution",
			"drawer.record_verify.form.dose":                   "Dose / qty · route / site",
			"drawer.record_verify.form.dose_placeholder":       "e.g. 1 dose · S/C neck",
			"drawer.record_verify.form.dose_value":             "from protocol rule + SOP",
			"drawer.record_verify.form.cold_chain":             "Cold-chain intact?",
			"drawer.record_verify.form.cold_chain_value":       "captured in SOP proof",
			"drawer.record_verify.form.adverse":                "Adverse reaction?",
			"drawer.record_verify.form.proof":                  "Proof — in-app camera video per goat",
			"drawer.record_verify.form.proof_value":            "attached to SOP task",
			"drawer.warmup.aria":                               "Holding Farm load",
			"drawer.warmup.close_label":                        "Close Holding Farm load drawer",
			"drawer.warmup.eyebrow":                            "WARMUP",
			"drawer.warmup.title_prefix":                       "Holding-farm load",
			"drawer.warmup.note":                               "Source Entry owns Holding-Farm write actions. Preventive Care (PC) reads HF dose evidence here so arrival vaccination never double-doses.",
			"drawer.warmup.animals":                            "Animals in load",
			"drawer.warmup.holding_farm":                       "Holding farm",
			"drawer.warmup.purpose":                            "Purpose",
			"drawer.warmup.warmup":                             "Warmup",
			"drawer.warmup.hf_vaccination":                     "HF vaccination",
			"drawer.warmup.actions_label":                      "Action",
			"drawer.shed_event.aria":                           "Vaccination pen event",
			"drawer.shed_event.close_label":                    "Close pen event drawer",
			"drawer.shed_event.eyebrow":                        "WORK CONTEXT",
			"drawer.shed_event.shed_event":                     "Pen event",
			"drawer.shed_event.shed":                           "Pen",
			"drawer.shed_event.owner_assist":                   "Owner → assist",
			"drawer.shed_event.stock":                          "Stock (FEFO)",
			"drawer.shed_event.status":                         "Status",
			"form.proof_upload.label":                          "Upload vaccination proof",
			"form.proof_upload.select":                         "Select a video or image file",
			"form.proof_upload.disabled":                       "Proof upload disabled",
			"form.proof_upload.reason":                         "No SOP task on this pen-drive rollup yet (sopTaskId null) — proof is uploaded per-goat in the operator SOP task once the drive is assigned/advanced.",
			"form.completion.reason":                           "No single recorded completion on this rollup (completionId null) — verify a recorded dose from the verification queue.",
			"form.actions.unavailable":                         "This row is a generated rollup. Dose recording and proof happen on the operator SOP task; open the Action Center or workflow record for the live handle.",
			"form.reject.placeholder":                          "Reason for rejection (required)",
			"error.reject_reason":                              "Rejection reason required — provide a reason for requiring rework.",
			"action.import_goats":                              "Import goats",
			"action.import_sheet":                              "Import sheet",
			"action.new_drive":                                 "New drive",
			"action.source_entry":                              "Source Entry",
			"action.sop_library":                               "SOP Library",
			"action.action_center":                             "Action Center",
			"action.open_config":                               "Open the vaccination plan",
			"action.open_in_sop_library":                       "Open the vaccination plan",
			"action.open_source_entry":                         "Open Source Entry",
			"action.open_action_center":                        "Open Action Center",
			"action.open_park_action_center":                   "Open park in the Action Center",
			"action.open_shed_event_for":                       "Open vaccination pen event for",
			"action.open_protocol_rules":                       "Protocol Rules",
			"action.open_sop_library":                          "Open the vaccination plan",
			"action.open_protocol_adherence":                   "Protocol Adherence",
			"action.reset_filters":                             "Reset filters",
			"action.shed_detail":                               "Pen detail",
			"action.assign_owner_chain":                        "Assign operator",
			"action.capture_vaccination_proof":                 "Capture vaccination proof",
			"action.verify_vaccination_proof":                  "Verify vaccination proof",
			"action.record_verify":                             "Record + verify",
			"action.record_hf_dose":                            "Record HF dose",
			"action.import_vaccination_evidence":               "Import vaccination evidence",
			"action.reject_before_load":                        "Reject before load",
			"action.clear_to_ship":                             "Clear to ship",
			"action.close_drawer":                              "Close drawer",
			"action.submit_proof":                              "Submit proof",
			"action.uploading_proof":                           "Uploading proof...",
			"action.accept":                                    "Accept",
			"action.accepting":                                 "Accepting...",
			"label.drive_over_cap_required":                    "capacity shortfall",
			"label.drive_medical_defer":                        "medical defer",
			"label.drive_terminal_closed":                      "terminal closed",
			"tooltip.drive_over_cap_required":                  "{animals} animals assigned against {slots} planned operator slots; leadership action needed before latest-safe date",
			"tooltip.drive_medical_defer":                      "Hard medical defer blocks vaccination until cleared",
			"tooltip.drive_terminal_closed":                    "Terminal animal state closed this vaccination work",
			"action.reject":                                    "Reject",
			"action.confirm_reject":                            "Confirm reject",
			"action.rejecting":                                 "Rejecting...",
			"drawer.vaccination.eyebrow":                       "VACCINE",
			"drawer.import.title":                              "Import vaccination sheet",
			"drawer.import.subtitle":                           "Where vaccination drives and dose history actually enter Mesha.",
			"drawer.import.note":                               "There is no in-app bulk drive importer on this surface — admin-web never writes vaccination state directly. Drives are generated from config, and supplier dose history is imported under Source Entry. Use the real paths below; nothing on this drawer submits.",
			"drawer.import.new_drives_title":                   "New drives",
			"drawer.import.new_drives_body":                    "Publish a protocol rule in Config → obligations generate → the sweeper batches a pen drive.",
			"drawer.import.hf_history_title":                   "Supplier / HF dose history",
			"drawer.import.hf_history_body":                    "Import & review Holding-Farm vaccination evidence under Procurement · Source Entry.",
			"drawer.import.columns_label":                      "Drive sheet columns (reference)",
			"drawer.import.reference_only":                     "Reference only. The committed importer/contract is not built for this slice, so no upload control is shown rather than a fake preview-to-submit.",
			"drawer.new_drive.title":                           "New vaccination drive",
			"drawer.new_drive.subtitle":                        "A drive is generated from config — it is not hand-created here.",
			"drawer.new_drive.note":                            "A vaccination drive is the downstream effect of a published protocol rule, not a form on this screen. This drawer explains the mechanic and links to the real authoring surface; nothing here submits or is saved.",
			"drawer.new_drive.aria":                            "How a new drive is generated",
			"drawer.new_drive.publish_rule_title":              "1 · Publish rule",
			"drawer.new_drive.publish_rule_body":               "Config → vaccination category → publish a protocol version.",
			"drawer.new_drive.generation_title":                "2 · Generation",
			"drawer.new_drive.generation_body":                 "Obligations materialize per eligible goat; the sweeper batches them into a per-pen drive + SOP task.",
			"drawer.new_drive.assign_title":                    "3 · Assign / act",
			"drawer.new_drive.assign_body":                     "Assignment gaps and execution work surface in the Action Center.",
			"drawer.sop.button_title":                          "Vaccination SOP policy",
			"drawer.sop.button":                                "SOP",
			"drawer.sop.aria":                                  "Vaccination Drive SOP",
			"drawer.sop.eyebrow":                               "Preventive Care (PC) · VACCINATION",
			"drawer.sop.title":                                 "Vaccination Drive SOP",
			"drawer.sop.auth_error":                            "Couldn’t load the vaccination SOP — sign in with Google to load it (the SOP engine is tenant-scoped).",
			"drawer.sop.error_prefix":                          "Couldn’t load the vaccination SOP.",
			"drawer.sop.empty":                                 "No vaccination SOP is authored yet — the steps below are the standard drive flow. Author one in the SOP Library to attach proof gates and versioning.",
			"drawer.sop.window_note":                           "Per protocol window · booster intervals tracked",
			"drawer.sop.video_proof_required":                  "video proof required",
			"drawer.sop.library_title":                         "Versions, change history, and authoring live on the Vaccination plan page",
			"empty.status_matrix":                              "No vaccination matrix rows for this scope.",
			"empty.cohort_detail":                              "No cohort rows for this scope.",
			"empty.supplier_warmup":                            "No source-entry loads yet. Holding-Farm evidence appears after a procurement load is created.",
			"label.pre_arrival":                                "pre-arrival",
			"label.up_to_date":                                 "up to date",
			"label.due_soon":                                   "due soon",
			"label.overdue":                                    "overdue",
			"label.due_prefix":                                 "due",
			"label.all_severity":                               "All severity",
			"label.all_states":                                 "All states",
			"label.cohort":                                     "Cohort",
			"label.park":                                       "Park",
			"label.operators":                                  "Operators",
			"label.assignment":                                 "Assignment",
			"label.operators_unassigned":                       "Operators unassigned",
			"label.no_drive":                                   "No drive",
			"label.operator_count_singular":                    "operator",
			"label.operator_count_plural":                      "operators",
			"label.need_attention":                             "need attention",
			"label.on_track":                                   "on track",
			"label.operator_unassigned":                        "operator: unassigned",
			"label.park_head_unassigned":                       "park head: unassigned",
			"label.verifier_default":                           "Video Verification Team",
			"label.owner_chain_to_assign":                      "operator to assign",
			"label.stock_resolved_action_center":               "resolved in Action Center",
			"label.shed_event_noun":                            "pen event",
			"label.last_dose":                                  "last dose",
			"label.days_suffix":                                "d",
			"label.from_date_prefix":                           "from",
			"label.purchase_date_missing":                      "purchase date missing",
			"label.supplier_prefix":                            "supplier",
			"label.holding_not_set":                            "Holding not set",
			"label.mixed":                                      "mixed",
			"label.placeholder":                                "—",
			"reason.no_operator":                               "No operator assigned to this pen drive",
			"note.execution_counts":                            "Counts reflect the returned result set (max 500 rows), scoped by the top bar and filters",
			"note.execution_counts_capped":                     "— result is capped; narrow with Filters",
			"note.execution_counts_filtered":                   "Work-state filters are applied server-side; counts are hidden while filtered. Severity narrows the returned rows in this view.",
		}
	case "shed-execution":
		return map[string]string{
			"action.open_passport":           "Open Animal Passport",
			"crumb":                          "Preventive Care (PC) · Vaccination · Execution",
			"fallback.title":                 "Pen unavailable",
			"fallback.body":                  "Pen returned no vaccination execution context. It may be outside the current drive scope, or the service is unavailable.",
			"section.overview.title":         "Pen overview",
			"section.planned_sessions.title": "Operator-day plan",
			"section.planned_sessions.note":  "How this pen's open animal work is assigned across available operator days.",
			"section.planned_sessions.empty": "No planned sessions — no open vaccination work at this pen.",
			"section.vaccines.title":         "Vaccine breakdown",
			"section.vaccines.note":          "Per-vaccine obligation counts for this pen — the only place vaccine-level counts appear.",
			"section.vaccines.empty":         "No vaccines with open obligations at this pen.",
			"section.animals.title":          "Animals in pen",
			"section.animals.note":           "Display ID, health/lifecycle, last vaccination date, next vaccination date, and current vaccination work.",
			"section.animals.empty":          "No animals in this pen.",
			"animals.column.display_id":      "Display ID",
			"animals.column.tag_1":           "Tag 1",
			"animals.column.tag_2":           "Tag 2",
			"animals.column.breed":           "Breed",
			"animals.column.sex":             "Sex",
			"animals.column.age":             "Age",
			"animals.column.lifecycle":       "Lifecycle",
			"animals.column.health":          "Health",
			"animals.column.last_vax_date":   "Last vaccination date",
			"animals.column.next_vax_date":   "Next vaccination date",
			"animals.column.vax_work":        "Vaccination work",
			"animals.status.due":             "Due now",
			"animals.status.scheduled":       "Scheduled",
			"animals.status.up_to_date":      "Up to date",
			"animals.status.no_record":       "No record",
			"animals.status.sick":            "Sick",
			"animals.status.under_treatment": "Under treatment",
			"animals.status.quarantine":      "Quarantine",
			"animals.status.icu":             "ICU",
			"animals.status.dead":            "Dead",
			"animals.status.sold":            "Sold",
			"animals.status.culled":          "Culled",
			"action.load_more":               "Load more",
			"label.animals":                  "Animals",
			"label.due":                      "Due",
			"label.done_stat":                "Done",
			"label.sessions":                 "Operator days",
			"label.status":                   "Status",
			"label.manager":                  "Manager",
			"label.backup":                   "Backup",
			"label.manager_unassigned":       "Manager: unassigned",
			"label.backup_unassigned":        "Backup: unassigned",
			"tooltip.capacity.label":         "About capacity",
			"tooltip.capacity.body":          "Capacity counts unique animals per available operator per business date. One animal with multiple same-day vaccines still consumes one operator slot. Spillover dates recompute timetable, leave, role, and scope.",
			"section.work_state.title":       "Work state",
			"section.drives.title":           "Drives at this pen",
			"section.owner_chain.title":      "Operator assignment",
			"section.blocked.title":          "Blocked / deferred",
			"section.drive_rows.title":       "Drive rows",
			"table.drive_rows.aria":          "drive rows",
			"drawer.action.aria":             "Vaccination execution action",
			"action.open_workflow":           "Open Workflow",
			"action.close":                   "Close",
			"empty.drives":                   "No drives scheduled at this pen.",
			"empty.drive_rows":               "No drive rows for this pen.",
			"label.animal_stages":            "Animal stages",
			"label.drive_rows":               "drive rows",
			"label.done":                     "done",
			"label.open":                     "open",
			"label.operator_ground":          "Operator (ground)",
			"label.park_head":                "Park head",
			"label.verifier":                 "Verifier",
			"label.unassigned":               "unassigned",
			"label.verifier_default":         "Video Verification Team",
			"label.drive_fallback":           "drive",
			"label.placeholder":              "—",
			"form.proof_upload.label":        "Upload vaccination proof",
			"form.proof_upload.select":       "Select a video or image file",
			"form.proof_upload.disabled":     "Proof upload disabled",
			"form.proof_upload.reason":       "No SOP task on this pen-drive rollup yet (sopTaskId null) — proof is uploaded per-goat in the operator SOP task once the drive is assigned/advanced.",
			"form.completion.reason":         "No single recorded completion on this rollup (completionId null) — verify a recorded dose from the verification queue.",
			"form.actions.unavailable":       "This row is a generated rollup. Dose recording and proof happen on the operator SOP task; open the Action Center or workflow record for the live handle.",
			"form.reject.placeholder":        "Reason for rejection (required)",
			"error.reject_reason":            "Rejection reason required — provide a reason for requiring rework.",
			"action.submit_proof":            "Submit proof",
			"action.uploading_proof":         "Uploading proof...",
			"action.accept":                  "Accept",
			"action.accepting":               "Accepting...",
			"action.reject":                  "Reject",
			"action.confirm_reject":          "Confirm reject",
			"action.rejecting":               "Rejecting...",
			"action.cancel":                  "Cancel",
		}
	case "vendors", "sales-vendors":
		// Backend-owned copy for the register. The client renders these verbatim; per the golden
		// rule it must not hardcode a label, an empty state or a disabled reason of its own.
		//
		// BOTH SIDES OF THE REGISTER SHARE THIS MAP, and that sharing is the point (maintainer
		// decision 2026-09-05). Procurement > Vendors and Sales > Vendors are the same table, the
		// same drawer and the same form; only WHICH record types they carry differs. Two copies of
		// ~90 keys would drift, and the drift would show as one register wording a field differently
		// from the other. The handful of sentences that genuinely differ are overridden below,
		// AFTER the shared map is built, so a key added here reaches both pages automatically.
		copyMap := map[string]string{
			"crumb":                     "Procurement",
			"section.vendors.title":     "Vendors",
			"section.vendors.aria":      "Procurement vendor register",
			"section.vendors.row_hint":  "click a row to see full details",
			"filter.search_label":       "Search vendors",
			"filter.search_placeholder": "Business, contact, phone or city",
			"filter.record_type":        "Record type",
			"filter.status":             "Status",
			"filter.state":              "State",
			"filter.city":               "City",
			"filter.breed":              "Breed",
			"filter.all":                "All",
			"filter.clear":              "Clear filters",
			"filter.apply":              "Apply filters",
			"filter.applying":           "Applying...",
			// Shown on hover when Apply is disabled: the control must say WHY it cannot be pressed
			// rather than looking broken.
			"filter.apply.nothing_staged": "Change a filter to apply it.",
			"column.business_name":        "Vendor",
			"column.record_type":          "Type",
			"column.phone_number":         "Phone",
			"column.location_display":     "Location",
			"column.status":               "Status",
			"action.add":                  "Add vendor",
			"action.edit":                 "Edit details",
			"action.save_status":          "Update status",
			// Write-feedback copy. actionFeedbackCopy resolves the action_key straight through copy(),
			// which THROWS on a missing key and takes the whole page down with it -- so every key an
			// action can redirect with must exist here.
			"action.vendor_created":        "Vendor added.",
			"action.vendor_updated":        "Vendor updated.",
			"action.vendor_status_changed": "Vendor status updated.",
			"action.vendor_save_failed":    "Could not save this vendor. Check the fields and try again.",
			"action.vendor_status_failed":  "Could not update this vendor's status. Reload and try again.",
			// withActionFeedback substitutes this key when an action passes one without the "action."
			// prefix, so it must resolve rather than crash the page.
			"action.error_form":         "Could not complete that action.",
			"action.save":               "Save",
			"action.saving":             "Saving...",
			"action.cancel":             "Cancel",
			"action.close":              "Close",
			"action.next_page":          "Next",
			"action.prev_page":          "Back",
			"pager.page":                "Page",
			"pager.of":                  "of",
			"drawer.detail.title":       "Vendor details",
			"drawer.add.title":          "Add vendor",
			"drawer.edit.title":         "Edit vendor",
			"group.identity":            "Identity",
			"group.commercial":          "Commercial",
			"group.location":            "Location",
			"group.payment":             "Payment details",
			"group.notes":               "Notes",
			"field.business_name":       "Business name",
			"field.contact_person_name": "Contact person",
			"field.phone_number":        "Phone number",
			"field.record_type":         "Record type",
			"field.breed":               "Breed",
			"field.feed":                "Feed",
			"field.status":              "Status",
			"field.filtered_stock":      "Filtered stock",
			"field.price_per_goat":      "Price per goat",
			// Average animal weight (maintainer decision 2026-09-08): what size of animal this
			// buyer expects. Optional, a plain recorded value, connected to nothing else.
			"field.average_animal_weight": "Average animal weight (kg)",
			"field.ready_to_filtered":     "Ready to filtered",
			"field.eta_after_order":       "ETA after order (days)",
			"field.details":               "Details",
			"field.state":                 "State",
			"field.city":                  "City",
			"field.bank_name":             "Bank name",
			"field.account_no":            "Account number",
			"field.ifsc_code":             "IFSC code",
			"field.upi_id":                "UPI ID",
			"field.pan_number":            "PAN number",
			"field.comments":              "Comments",
			// Capacity and the voice note (maintainer decision 2026-09-03).
			"field.capacity":          "Capacity",
			"field.capacity_quantity": "How much per delivery",
			"field.capacity_unit":     "Unit",
			"field.supply_frequency":  "How often",
			"field.voice_note":        "Voice note",
			"hint.capacity":           "Optional. Fill in whatever is known.",
			"voice_note.none":         "No voice note recorded. Notes are recorded from the phone.",
			"voice_note.unavailable":  "The voice note could not be loaded right now.",
			"voice_note.play":         "Load voice note",
			"value.none":              "Not recorded",
			// Shown in place of the payment block for a caller without the finance permission, so a
			// withheld value never reads as "this vendor has no bank details".
			"payment.hidden":      "Payment details are hidden for your role.",
			"payment.none":        "No payment details recorded.",
			"empty.vendors":       "No vendors match these filters.",
			"empty.vendors.unset": "No vendors yet. Add the first one to start the register.",
			"summary.count":       "vendors",
			"error.load":          "Could not load the vendor register. Refresh to try again.",
			"error.save":          "Could not save this vendor.",
			"required.hint":       "Business name, record type, state and status are required.",
			// A NEW vendor is held to the same bar as the Slack intake questionnaire. Rows imported
			// without a contact person, phone or city stay editable, so the two hints differ on
			// purpose -- see domain.VendorWrite.ValidateForCreate.
			"required.hint.create": "Business name, record type, contact person, phone number, state, city and status are required.",
			// VENDOR FORM IS AUTHORED (2026-09-19): the drawer follows the published Vendor form SOP.
			"required.hint.form": "Fields marked * are required. This form follows the published Vendor form SOP.",
			"hint.other":         "Say what the other is",
			"disabled.write":     "Your current role can view vendors but not change them.",
		}
		if id == "sales-vendors" {
			// The five differences, and each is a sentence that would be WRONG on the other page:
			// the trail says which module you are in, the aria/empty text names which half of the
			// register you are looking at, and the search hint drops "city" for nothing -- it is
			// kept identical because the search column is the same generated column either way.
			copyMap["crumb"] = "Sales"
			copyMap["section.vendors.aria"] = "Sales vendor register"
			copyMap["empty.vendors"] = "No buyers match these filters."
			copyMap["empty.vendors.unset"] = "No buyers yet. Add the first one to start the sales register."
			copyMap["error.load"] = "Could not load the sales vendor register. Refresh to try again."
		}
		return copyMap
	case "sales-loads":
		// Load by load -- the per-load reconciliation page under Sales (maintainer decision
		// 2026-08-31). Farm language only, like every other page's copy.
		return map[string]string{
			"crumb": "Sales",

			// The page's own copy. Two tabs, Purchased (loads) and Farm born (a shell until that
			// view is built). All backend-owned; the client renders it verbatim.
			"tab.purchased":                    "Purchased",
			"tab.farm_born":                    "Farm born",
			"section.loadwise.title":           "Load by load",
			"page.tabs.aria":                   "Purchased or farm born",
			"section.loadwise.aria":            "Load-wise reconciliation",
			"section.loadwise.subtitle":        "Every purchased load reconciled: bought, sold, died, still on farm — and the money on each side.",
			"section.loadwise.showing":         "Showing the newest loads",
			"empty.loadwise":                   "No purchased loads yet. Loads appear here as source entry accepts them into the herd.",
			"empty.farm_born":                  "Sales of farm-born animals will show here. This view is not built yet.",
			"loadwise.kpi.purchased":           "Purchased",
			"loadwise.kpi.sold":                "Sold",
			"loadwise.kpi.mortality":           "Mortality",
			"loadwise.kpi.remaining":           "Still on farm",
			"loadwise.kpi.purchase_value":      "Purchase value",
			"loadwise.kpi.purchase_value.hint": "recorded costs only",
			"loadwise.kpi.sold_value":          "Sold value",
			"loadwise.kpi.sold_value.hint":     "from sales with tagged animals",
			"loadwise.kpi.profit":              "Profit / loss",
			"loadwise.kpi.profit.hint":         "sales plus stock on farm, against what the loads cost",
			"chart.loadwise_counts.title":      "Animals per load",
			"chart.loadwise_counts.empty":      "No loads to chart yet.",
			"chart.loadwise_value.title":       "Money per load",
			"chart.loadwise_value.empty":       "No load has a recorded cost or a priced sale yet.",
			"chart.series.purchased":           "Purchased",
			"chart.series.sold_count":          "Sold",
			"chart.series.mortality":           "Mortality",
			"chart.series.remaining":           "Remaining",
			"chart.series.purchase_value":      "Purchase value",
			"chart.series.sold_value":          "Sold value",
			"chart.series.profit_loss":         "Profit / loss",
			// THE GROWTH READ (maintainer request 2026-09-01): weight in vs weight out, cost per
			// kg vs price per kg, and the days between. Three charts under the money one, in the
			// same load order, so the farm reads one column of loads down the page.
			"chart.loadwise_weight.title":      "Average weight per animal",
			"chart.loadwise_weight.empty":      "No load has both a purchase weight and a sale weight yet.",
			"chart.series.avg_purchase_weight": "Bought at",
			"chart.series.avg_sale_weight":     "Sold at",
			// A load that still has animals on farm shows what those animals weigh NOW
			// (maintainer request 2026-09-03): the latest weighing of the pens the load was placed
			// into, weighted by head count. A fully sold load shows no third bar.
			"chart.series.current_avg_weight":   "Weighs now",
			"value.not_weighed_yet":             "not weighed yet",
			"value.sold_no_now":                 "sold",
			"value.weighed_now":                 "weighed at the latest weighing",
			"disabled.weights":                  "Your current role can view loads but not weighing.",
			"chart.loadwise_per_kg.title":       "Price per kg",
			"chart.loadwise_per_kg.empty":       "No load has a recorded cost or a priced sale yet.",
			"chart.series.landing_price_per_kg": "Landing price",
			"chart.series.sale_price_per_kg":    "Sale price",
			"chart.loadwise_fattening.title":    "Fattening days on farm",
			// Says only what the CHART cannot show, never why. A load holding animals whose
			// arrival date is unknown -- or that landed today, which the chart reads as no value
			// -- yields an all-null series too, so copy claiming nothing is in the sheds would
			// deny stock that is really there.
			"chart.loadwise_fattening.empty": "No load has a fattening span to show yet.",
			"chart.series.fattening_days":    "Days from arrival to sale",
			// The SAME clock still running, for the animals a load still holds. Unsold loads had a
			// blank bar here, which read as "nothing to say" about animals that have been eating on
			// this farm for months; this states the elapsed span instead (maintainer decision
			// 2026-09-01). It is arrival-to-today, NOT the purchase-date age clock, so both series
			// on this axis start on the day the animals landed. A part-sold load shows BOTH bars
			// (maintainer decision 2026-09-18; load 126 had sold some animals, its span was never
			// imported, and it stood with no bar at all), and its bar label states the split.
			"chart.series.days_on_farm_so_far": "Days on farm so far",
			"value.still_on_farm":              "still on farm",
			"value.sold_count":                 "sold",
			// The load's AGE has no chart and no column -- the maintainer removed both -- but the
			// 90-day clock still runs: it is what raises the daily CXO alert, from
			// procurement/domain.LoadAgeAlertDays. Average sale weight likewise stays a CHART
			// series only; it is not a table column.
			"value.kg":         "kg",
			"value.days":       "days",
			"value.arrived_on": "Reached the farm",
			// Said on the bar whenever the sale average covers fewer animals than the load sold:
			// the legacy sales did not all record a weight, and a sample must not read as a total.
			"value.weighed_out":  "weighed on the way out",
			"value.not_sold_yet": "Not sold yet",
			"column.load":        "Load",
			"column.purchased":   "Purchased",
			"column.sold":        "Sold",
			"column.mortality":   "Mortality",
			// Kept as copy even though the table no longer carries a column for it: culled /
			// transferred / lost are real exits, and the load-cost drawer still names them so the
			// fact is never silently dropped from a load that has one.
			"column.other_exits":    "Other exits",
			"column.remaining":      "Remaining",
			"column.unaccounted":    "Unaccounted",
			"column.purchase_value": "Purchase value",
			// LANDING PRICE PER LIVE KG (maintainer request 2026-09-01). The farm's own comparison
			// figure between vendors -- landed cost over the live weight bought. It is only
			// meaningful now that purchase value is the LANDED cost: per-kg on the ex-farm price
			// alone understated every deal by the transport nobody had recorded.
			"column.landed_price_per_kg": "Landing price / live kg",
			"value.weight_missing":       "Not weighed",
			"value.live_kg":              "kg live weight",
			"field.purchase_weight_kg":   "Live weight bought (kg)",
			"column.sold_value":          "Sold value",
			"column.profit_loss":         "Profit / loss",
			// Still published: the drawer names the stock figure the profit is partly made of, and
			// the table's profit cell explains an unrealised figure with it.
			"column.remaining_value":  "Remaining stock value (est.)",
			"value.profit_unrealised": "includes stock still on farm, not yet sold",
			// The profit of a load that has sold nothing IS its stock valuation, so the number that
			// produced it is shown beside it rather than hidden in a tooltip.
			"value.profit_incl_stock":             "incl. stock",
			"loadwise.stock_price_note":           "Animals not yet sold are valued at",
			"loadwise.stock_price_each":           "each",
			"loadwise.stock_price_unknown":        "Animals not yet sold cannot be valued: nothing has sold yet to price them against.",
			"value.profit_unavailable":            "No cost recorded, so profit cannot be worked out.",
			"value.cost_missing":                  "Cost not recorded",
			"value.price_basis.load":              "at this load's own average sold price",
			"value.price_basis.overall":           "at the overall average sold price",
			"value.price_basis.assumed":           "at the unsold animal price set on Sales Config",
			"value.price_basis.growth_sale_price": "at the stage and sex sale price",
			// The third basis, and the one a fresh tenant hits FIRST: with no sale anywhere there is
			// no price to value stock at. It must be published like the other two -- the renderer
			// resolves this key from price_basis, so an unpublished value takes the page down.
			"value.price_basis.none": "no sale yet to price them against",
			"value.sold_unpriced":    "sold without a tagged sale",
			"loadwise.row_hint":      "click a load to record its cost",
			"loadwise.prior.title":   "Before these records",
			"loadwise.prior.sold":    "Sold earlier",
			"loadwise.prior.died":    "Died earlier",
			"loadwise.prior.animals": "animals",
			// THE COST BREAKDOWN (maintainer decision 2026-09-01). Purchase value is the LANDED
			// cost -- animals plus transport plus everything else it took to bring them in -- and
			// these name the parts. One key per cost kind, resolved from the line's kind so the
			// client never renders the raw vocabulary; the farm's own words from the Procurement
			// DB sheet, except "Purchase" which is called what it buys.
			"loadwise.cost_breakdown.title":  "What this cost is made of",
			"loadwise.cost_breakdown.total":  "Landed cost",
			"loadwise.cost_breakdown.hint":   "Every cost recorded against this load, not just the animals.",
			"cost_kind.animal":               "Animals",
			"cost_kind.transport":            "Transport",
			"cost_kind.booking":              "Booking",
			"cost_kind.labour":               "Labour",
			"cost_kind.transit":              "Transit",
			"cost_kind.transition_feed":      "Transition feed",
			"cost_kind.other":                "Other",
			"drawer.load_cost.title":         "Record load cost",
			"field.animal_cost":              "Animal cost",
			"field.transport_cost":           "Transport cost",
			"field.other_cost":               "Other cost",
			"hint.load_cost":                 "What this load cost to buy and bring in. Animal cost is required; transport and other are optional.",
			"action.record_load_cost.label":  "Record cost",
			"action.load_cost_recorded":      "Load cost recorded.",
			"action.load_cost_record_failed": "Could not record this cost. Check the fields and try again.",
			"disabled.load_cost":             "Recording a load's cost needs the buying desk's access.",

			// Shared chrome this page renders itself, no longer inherited from the board.
			"value.none":   "Not recorded",
			"error.load":   "This page could not be loaded. Try again in a moment.",
			"action.close": "Close",
		}

	case "sales-config":
		// Sales Config carries every sales entry form (maintainer decision 2026-09-01), so its
		// copy is the UNION of the two read pages' copy plus its own headings. Merged rather than
		// duplicated on purpose: one source per label means the form that writes a field and the
		// page that reads it back can never disagree about what it is called.
		//
		// Order matters only for the four keys both maps carry (crumb, value.none, action.close,
		// error.load); the board's wording wins, and error.load is then replaced below because
		// neither page's sentence names this one.
		out := map[string]string{}
		for key, value := range pageSpecificCopy("sales-loads") {
			out[key] = value
		}
		for key, value := range pageSpecificCopy("sales") {
			out[key] = value
		}
		for key, value := range map[string]string{
			"error.load": "Could not load the sales records. Refresh to try again.",

			// The page's own headings: one card per entry surface, each saying what it is for so
			// the person picks the right form without opening three.
			"section.sales_entry.title":    "Sales",
			"section.sales_entry.subtitle": "Record a sale, then tag the animals it is made of. Click any sale below to add a payment or change its status.",
			"section.load_entry.title":     "Load wise",
			"section.load_entry.subtitle":  "What each purchased load cost to buy and bring in. Click a load to record or change its cost.",
			"section.load_entry.row_hint":  "click a load to record its cost",
			"section.sales_entry.row_hint": "click a sale to add a payment or change its status",
			"empty.loads":                  "No purchased loads yet. Loads appear here as source entry accepts them into the herd.",
			// Where the entered facts are READ back. Named so the person who just recorded
			// something knows where it shows up, without guessing from the sidebar.
			"link.sales_sold":       "See what has sold",
			"link.sales_farm_value": "See Farm value",
			"link.sales_loads":      "See Load wise",
			// A short lead-in for the two links, NOT a second copy of the subtitle: the header
			// already says what this page is for, and repeating that sentence four lines later
			// reads as a mistake.
			"hint.read_only": "Read these records on",

			// MARKET SURVEY config (maintainer decision 2026-09-14): the cities phoned each
			// morning and the questions asked in each. A change here reaches the phone on its
			// next refresh and applies from that day; entries already recorded keep the words
			// they were recorded against.
			// FARM VALUATION (maintainer instruction 2026-09-19): the decided figures behind Farm
			// value and Load wise, edited here. `{kg}` in a sentence is filled by the page from the
			// saved sale-ready line, so no literal 35 lives in copy.
			"action.valuation.label":          "Save valuation",
			"disabled.valuation":              "Your current role can see how the herd is valued but not change the figures.",
			"section.valuation.title":         "Farm valuation",
			"section.valuation.sub":           "How the live herd is valued on Farm value and what unsold animals are carried at on Load wise. Every figure here was decided, not measured; a change applies the moment it is saved. The sale-ready weight line lives with the Weighing assumptions.",
			"section.valuation.aria":          "Farm valuation assumptions",
			"valuation.bucket":                "Animals",
			"valuation.fixed_weight":          "Weight used (kg)",
			"valuation.fixed_weight.measured": "measured weight",
			"valuation.fixed_weight.hint":     "Leave blank to price the bucket at its animals' latest measured weight.",
			"valuation.price_per_kg":          "Price (₹ per kg)",
			"valuation.unsold_price":          "Unsold animal price (₹ each)",
			"valuation.unsold_price.hint":     "Load wise carries every unsold animal at this price. Leave blank to use the average price the farm has actually sold at.",
			"valuation.updated":               "Last saved",
			"valuation.saved":                 "Valuation saved. Farm value and Load wise use these figures from now on.",
			"valuation.error":                 "That could not be saved.",
			"section.market.title":            "Market survey",
			"section.market.sub":              "The cities phoned each morning and the questions asked in each. Prices are entered on the phone; changes here apply from today.",
			"section.market.aria":             "Market survey configuration",
			// Reporters (2026-09-19, Sales SOP page): who makes the calls.
			"market.reporters.title":        "Reported by",
			"market.reporters.sub":          "Who phones the markets each morning. Giving someone the survey puts the Market tab on their phone from their next refresh; taking it removes the tab.",
			"market.reporters.aria":         "Market survey reporters",
			"market.reporters.current":      "Reporting today",
			"market.reporters.none":         "Nobody reports the market yet. Give the survey to someone below.",
			"market.reporters.add":          "Give the survey to someone else",
			"market.reporters.give":         "Give the survey",
			"market.reporters.remove":       "Take it back",
			"market.reporters.tag":          "Reports",
			"market.reporters.people_link":  "Open People",
			"action.market_reporter_saved":  "Saved. The Market tab changes on that phone at its next refresh.",
			"market.cities.title":           "Cities",
			"market.cities.hint":            "One card per city on the phone every morning.",
			"market.questions.title":        "Questions",
			"market.call_time.title":        "Calls open at",
			"market.call_time.hint":         "The time each morning the city cards appear on the phone and the reminder is sent. Indian time.",
			"market.action.save_call_time":  "Save time",
			"action.market_call_time_saved": "Call time saved. It applies from tomorrow morning, or today if the time has not passed.",
			"market.questions.hint":         "Asked in every city. The unit is the words the price is quoted in, such as ₹/kg or ₹/500 g.",
			"market.field.city_name":        "City",
			"market.field.question_label":   "Question",
			"market.field.unit_label":       "Unit",
			"market.status.active":          "On the list",
			"market.status.retired":         "Retired",
			"market.action.add_city":        "Add city",
			"market.action.add_question":    "Add question",
			"market.action.save":            "Save",
			"market.action.retire":          "Retire",
			"market.action.restore":         "Put back",
			"market.action.edit":            "Edit",
			"market.action.cancel":          "Cancel",
			"market.empty.cities":           "No cities yet. Add the first market to phone.",
			"market.empty.questions":        "No questions yet. Add what to ask in every city.",
			// Write feedback rides the action_key query the page already renders; keys must
			// start with "action." for actionRedirect to carry them.
			"action.market_city_saved":     "City saved. It is on the phone from today.",
			"action.market_question_saved": "Question saved. It is asked from today; earlier prices keep the words they were recorded with.",
			"action.market_save_failed":    "Could not save. Check the fields and try again.",
			"action.market_duplicate":      "That name is already on the list.",
			"market.retired_note":          "Retired items stay in the history and can be put back.",
			"disabled.market_config":       "Your current role can view the market survey but not change what is asked.",
			"action.market_config.label":   "Change what is asked",
			"link.sales_market_analytics":  "See Market analytics",
		} {
			out[key] = value
		}
		return out

	case "animal-purchases":
		// Backend-owned copy for the CEO/CXO's animal purchase review. Farm language only; the
		// client renders these verbatim.
		return map[string]string{
			"crumb": "Procurement",

			"summary.loads":          "Loads",
			"summary.pending":        "Awaiting decision",
			"summary.accepted":       "Accepted",
			"summary.rejected":       "Rejected",
			"summary.hint":           "Across every load recorded from the phone.",
			"section.loads.title":    "Loads",
			"section.animals.title":  "Animals",
			"section.animals.hint":   "Pick a load to see its animals, or review everything still awaiting a decision.",
			"empty.loads":            "No purchase loads recorded yet. The buying desk adds them from the phone.",
			"empty.animals":          "No animals in this view.",
			"empty.pending":          "Nothing is waiting for a decision.",
			"filter.load":            "Load",
			"filter.load.all":        "All loads",
			"label.load_answers":     "About this load",
			"label.load.notes":       "Note",
			"label.load.no_notes":    "No note was added.",
			"label.load.recorded_by": "Recorded by",
			"label.load.added_on":    "Added on",
			"label.load.status":      "Status",
			"status.load.open":       "Open",
			"status.load.closed":     "Closed",
			"filter.decision":        "Show",
			"filter.recorded_from":   "Recorded from",
			"filter.recorded_to":     "to",
			"filter.apply":           "Apply",
			"filter.clear":           "Clear",
			"filter.pending":         "Awaiting decision",
			"filter.accepted":        "Accepted",
			"filter.rejected":        "Rejected",
			"filter.all":             "All",
			"action.next_page":       "Next",
			"action.prev_page":       "Back",
			"pager.page":             "Page",
			"pager.of":               "of",
			"pager.noun":             "animals",
			"pager.noun.one":         "animal",

			"column.load_ref":       "Load",
			"column.vendor_name":    "Vendor",
			"column.farm":           "Farm",
			"column.expected_count": "Expected",
			"column.total":          "Recorded",
			"column.pending":        "Awaiting",
			"column.accepted":       "Accepted",
			"column.rejected":       "Rejected",
			"column.created_at":     "Added on",
			"column.seq_no":         "Animal",
			"column.species":        "Species",
			"column.sex":            "Sex",
			"column.breed":          "Breed",
			"column.age_months":     "Age (months)",
			"column.weight_kg":      "Weight (kg)",
			"column.condition":      "Looks",
			"column.temp_tag":       "Temporary tag",
			"column.notes":          "Note",
			"column.decision":       "Decision",

			"video.title":                "Video",
			"video.empty":                "The video is not available right now.",
			"video.open":                 "Open video",
			"media.title":                "Photos and videos",
			"media.empty":                "Not available right now.",
			"photo.open":                 "Open big",
			"answers.title":              "Farm check",
			"verdict.section":            "Procurement director's verdict",
			"field_verdict.hint":         "The buying desk's own call on the farm; the decision here is yours.",
			"attention.hint":             "The farm check reads this answer as a reason to reject.",
			"decision.title":             "Decision",
			"decision.note":              "Note (optional, required to reject)",
			"decision.note_hint":         "Say why, so the buying desk knows what to look for next time.",
			"decision.by":                "Decided by",
			"decision.on":                "on",
			"action.accept":              "Accept",
			"action.reject":              "Reject",
			"action.deciding":            "Saving...",
			"action.close":               "Close",
			"action.decided_accepted":    "Animal accepted. The buying desk can see it on the phone.",
			"action.decided_rejected":    "Animal rejected. The buying desk can see it on the phone.",
			"action.decide_failed":       "Could not record that decision. Reload and try again.",
			"action.decide_conflict":     "This animal was already decided. Reload to see the answer.",
			"action.error_form":          "Could not complete that action.",
			"verdict.disabled_no_access": "Accepting or rejecting an animal is limited to the CEO and CXO.",
			"value.none":                 "—",
			"value.no_video":             "No video",
		}
	case "feed-purchases":
		// Backend-owned copy for the feed purchase ledger. The client renders these verbatim; per
		// the golden rule it must not hardcode a label, an empty state or a disabled reason of its
		// own. Farm language only -- no table, column or contract vocabulary reaches the screen.
		return map[string]string{
			"crumb": "Procurement",

			// Header figures. Whole-filter aggregates, labelled as what they count.
			// Both forms are published so the renderer picks one by the number rather than
			// composing "1 loads" -- pluralisation is presentation, the WORDS are backend-owned.
			"summary.count":      "loads",
			"summary.count.one":  "load",
			"summary.quantity":   "Feed bought",
			"summary.spend":      "Spent",
			"summary.hint":       "Across every load in this view.",
			"summary.stock_link": "See what is left in Feed Analytics",

			// Ledger columns.
			"column.purchase_date":  "Bought on",
			"column.farm":           "Farm",
			"column.feed_item":      "Feed",
			"column.batch_no":       "Load",
			"column.quantity_kg":    "Quantity (kg)",
			"column.total_cost":     "Landed cost",
			"column.per_kg_cost":    "Per kg",
			"column.vendor":         "Vendor",
			"column.payment_status": "Payment",
			// Whether the load has come in yet: feed on the road is not stock, and the ledger
			// says so beside the quantity rather than letting the reader assume it is in the store.
			"column.delivery_status": "Delivery",
			// The money still owed on the load, so a pending row answers "how much" without opening it.
			"column.payment_balance": "Remaining",
			"column.entry_source":    "Recorded",
			"value.entry_app":        "In app",
			"value.entry_sheet":      "From the feed book",
			"empty.purchases":        "No feed purchases match this view.",
			"empty.purchases.unset":  "No feed purchases recorded yet. Record the first load to start the ledger.",

			// Filters and paging.
			"filter.farm":         "Farm",
			"filter.all":          "All farms",
			"filter.delivery":     "Delivery",
			"filter.delivery.all": "All loads",
			"filter.clear":        "Clear filters",
			"action.next_page":    "Next",
			"action.prev_page":    "Back",
			"pager.page":          "Page",
			"pager.of":            "of",

			// Record-purchase drawer.
			"action.record_feed_purchase.label": "Record purchase",
			"drawer.record_purchase.title":      "Record a feed purchase",
			"drawer.detail.title":               "Purchase details",
			"field.purchase_date":               "Bought on",
			"field.farm":                        "Farm",
			"field.feed_item":                   "Feed",
			"field.batch_no":                    "Load number",
			"field.quantity_kg":                 "Quantity (kg)",
			"field.feed_cost":                   "Feed cost",
			"field.transport_cost":              "Transport cost",
			"field.loading_cost":                "Loading cost",
			"field.unloading_cost":              "Unloading cost",
			"field.total_cost":                  "Total cost",
			"field.per_kg_cost":                 "Per kg",
			"field.vendor":                      "Vendor",
			"field.payment_released":            "Payment released",
			"field.payment_status":              "Payment status",
			// Optional buyer's figure (maintainer request 2026-09-19): how many days of feeding
			// the load is meant to cover. Checked per load on Feed Analytics, Purchased vs consumed.
			"field.days_of_stock": "Days of stock",
			"hint.days_of_stock":  "Optional. How many days of feeding this load is meant to cover, as the buyer expects it. Feed Analytics compares it with what the sheets actually used.",
			"value.days_of_stock": "{days} days",
			"required.hint":       "Date, farm, feed, quantity, vendor and payment status are required.",
			"hint.batch_no":       "Leave blank to record this as the next load of this feed at this farm.",
			"hint.total_cost":     "Leave blank to add up the feed, transport, loading and unloading costs entered above.",

			// Delivery (maintainer decision 2026-09-03): a load is bought, travels for days, then
			// reaches. Stock counts it from the day it reached, at the weight received once that
			// is entered -- which can wait, because the weighbridge figure is often known after
			// the feed is already in use.
			"section.delivery.title":  "Delivery",
			"field.delivery_status":   "Delivery",
			"field.reached_on":        "Delivered on",
			"field.reached_weight_kg": "Weight received (kg)",
			"field.stock_kg":          "Counted as stock (kg)",
			"hint.record_reached":     "Leave the delivered date blank if the load is still in transit. Fill it in only for a load that has already come in.",
			"hint.reached_weight":     "Leave blank if the load has not been weighed on arrival yet — the buying weight counts until you enter it.",
			// THE FEED PURCHASE FORM IS AUTHORED (2026-09-20): the questions below the ledger's own
			// boxes come from the published form, editable on Procurement SOP.
			"hint.authored_questions":       "The questions above the buttons are your own — add, re-word or remove them on Procurement › Procurement SOP.",
			"hint.other":                    "Say what the other is",
			"hint.mark_reached":             "Marking the load delivered counts it as stock from that day and raises its toxin test. The received weight can be entered now or later.",
			"delivery.in_transit_note":      "This load is still in transit. It is not counted as stock and its toxin test has not started.",
			"action.mark_reached.label":     "Mark delivered",
			"action.update_delivery.label":  "Update arrival",
			"action.delivery_recorded":      "Load marked delivered. It now counts as stock and its toxin test has been raised.",
			"action.delivery_updated":       "Arrival details updated.",
			"action.delivery_record_failed": "Could not update the arrival. Check the fields and try again.",

			// The shared date control's copy: placeholder per field, month navigation, and the
			// refusal shown when a day outside the allowed range is picked. Every date on this
			// ledger has already happened, so each picker is capped at today; the arrival day is
			// also floored at the purchase date.
			"date.purchase_date.placeholder": "Pick the day it was bought",
			"date.reached_on.placeholder":    "Pick the day it was delivered",
			"date.paid_on.placeholder":       "Pick the day it was paid",
			"date.prev_month":                "Previous month",
			"date.next_month":                "Next month",
			"date.invalid_purchase_date":     "Pick the day the feed was bought. It cannot be later than {date}.",
			"date.invalid_reached_on":        "Pick the day the load was delivered. It cannot be before it was bought or later than {date}.",
			"date.invalid_paid_on":           "Pick the day the money was handed over. It cannot be later than {date}.",

			// Payment section of the detail drawer: instalment history, running totals, and the
			// add-payment / status-edit controls.
			"section.payments.title":              "Payments",
			"payments.paid_so_far":                "Paid so far",
			"payments.balance":                    "Balance to pay",
			"payments.empty":                      "No payments recorded against this load yet.",
			"payments.column.paid_on":             "Paid on",
			"payments.column.amount":              "Amount",
			"payments.column.note":                "Note",
			"field.paid_on":                       "Paid on",
			"field.amount_rupees":                 "Amount paid",
			"field.note":                          "Note",
			"hint.record_payment":                 "Record each amount as it is handed over — the balance and payment status update themselves.",
			"action.record_feed_payment.label":    "Add payment",
			"action.update_payment_status.label":  "Update status",
			"action.edit_feed_purchase.label":     "Edit purchase",
			"drawer.edit.title":                   "Edit this purchase",
			"hint.edit_identity":                  "Farm, feed and load number cannot be changed — record a new load if those are wrong.",
			"action.purchase_updated":             "Purchase updated.",
			"action.purchase_update_failed":       "Could not update this purchase. Check the fields and try again.",
			"action.payment_recorded":             "Payment recorded.",
			"action.payment_record_failed":        "Could not record this payment. Check the fields and try again.",
			"action.payment_status_updated":       "Payment status updated.",
			"action.payment_status_update_failed": "Could not update the payment status. Try again.",
			"value.none":                          "—",
			"action.save":                         "Save",
			"action.saving":                       "Saving...",
			"action.cancel":                       "Cancel",
			"action.close":                        "Close",
			"action.purchase_recorded":            "Feed purchase recorded.",
			"action.purchase_record_failed":       "Could not record this purchase. Check the fields and try again.",
			"action.error_form":                   "Could not complete that action.",
			"error.load":                          "Could not load the feed purchase ledger. Refresh to try again.",
			"error.options":                       "Could not load the purchase form options. Refresh to try again.",
			"disabled.write":                      "Your current role can view feed purchases but not record them.",
		}
	case "sales-farm-born":
		// Backend-owned copy for the Farm born page (maintainer request 2026-09-18). The client
		// renders these verbatim: every heading, filter label, column, KPI and empty state. Farm
		// language only; PEN, never shed, on screen.
		return map[string]string{
			"crumb": "Sales",

			// The filter bar. Period is the SOLD window (maintainer decision 2026-09-18: the
			// on-farm count is today's whatever the period); the rest narrow the whole page.
			"filter.period.label":            "Sold between",
			"filter.period.field":            "Period",
			"filter.period.today":            "Today",
			"filter.period.single":           "Single day",
			"filter.period.range":            "Date range",
			"filter.period.aria":             "Choose which sale dates the page reports on",
			"filter.period.previous_month":   "Previous month",
			"filter.period.next_month":       "Next month",
			"filter.period.range_start_hint": "Pick the first day of the period.",
			"filter.period.range_end_hint":   "Now pick the last day of the period.",
			"filter.period.range_separator":  "to",
			"filter.park.label":              "Park",
			"filter.pen.label":               "Pen",
			"filter.species.label":           "Species",
			"filter.breed.label":             "Breed",
			"filter.sex.label":               "Sex",
			"filter.stage.label":             "Stage",
			"filter.all_option":              "All",
			"filter.bar_aria":                "Filter farm born animals",
			"filter.apply":                   "Apply filters",
			"filter.clear_all":               "Clear filters",

			// Headline tiles -- whole-filter figures, never the page's.
			"section.headline.aria": "Farm born headline figures",
			"kpi.on_farm":           "On the farm now",
			"kpi.on_farm.detail":    "alive today, born on this farm",
			"kpi.sold":              "Sold in the period",
			"kpi.sold.detail":       "animals with a sale date in the period",
			"kpi.revenue":           "Earned",
			"kpi.revenue.detail":    "from sales with a recorded deal",
			"kpi.revenue.unpriced":  "{count} sold without a deal value",
			"kpi.avg_price":         "Average per animal",
			"kpi.avg_price.detail":  "earned over priced sales",

			// The four breakdowns: on farm now beside sold in the period, per dimension.
			"section.breakdowns.title":    "What sold, and what is still here",
			"section.breakdowns.subtitle": "Each table splits the same animals one way. On farm is today's count; Sold and Earned are the period's.",
			"section.by_breed.title":      "By breed",
			"section.by_sex.title":        "By sex",
			"section.by_stage.title":      "By stage",
			"section.by_pen.title":        "By pen",
			"column.on_farm":              "On farm",
			"column.sold":                 "Sold",
			"column.revenue":              "Earned",
			"column.share_pct":            "Share of sold",
			"empty.breakdown":             "No animals match these filters.",

			// The sold ledger.
			"section.sold.title":    "Sold animals",
			"section.sold.subtitle": "One row per animal sold in the period, newest sale first. The value is the animal's share of its deal.",
			"section.sold.aria":     "Animals sold in the period",
			"column.tag":            "Tag",
			"column.breed":          "Breed",
			"column.sex":            "Sex",
			"column.stage":          "Stage",
			"column.pen":            "Pen",
			"column.sale_date":      "Sold on",
			"column.buyer_name":     "Buyer",
			"column.sale_value":     "Value",
			"empty.sold":            "No animals sold in this period with these filters.",
			"value.no_deal":         "No deal recorded",
			"value.not_recorded":    "Not recorded",
			"pager.noun":            "animals",
			"pager.pens":            "pens",

			"error.load": "Could not load the farm born figures. Refresh to try again.",
		}
	case "sales-buyer-analytics":
		// Backend-owned copy for the Buyer analytics page (maintainer request 2026-09-15). The
		// client renders these verbatim: every heading, column, chip, KPI and empty state. Farm
		// language only. The farm toggle and pager words are the sales pages' own, copied here
		// by key so the toggle reads identically on every Sales page.
		return map[string]string{
			"crumb": "Sales",

			"filter.farm": "Farm",
			"filter.all":  "All farms",

			// Headline tiles -- whole-filter figures, never the page's.
			"section.headline.aria": "Buyer headline figures",
			"kpi.buyers":            "Buyers",
			// Keep the count-suffixed legacy copy intact for the previous frontend.
			"kpi.buyers.detail":             "not in the vendor register",
			"kpi.buyers.closed_sale_detail": "with at least one closed sale",
			"kpi.repeat_buyers":             "Come back",
			"kpi.repeat_buyers.detail":      "bought more than once",
			"kpi.repeat_revenue":            "Revenue from repeat buyers",
			"kpi.repeat_revenue.detail":     "of all revenue",
			"kpi.outstanding":               "Still owed",
			"kpi.outstanding.detail":        "across every closed sale",
			"kpi.purchases.detail":          "sales",
			"kpi.animals.detail":            "animals",

			// The buyer table.
			"section.buyers.title":    "Buyers",
			"section.buyers.subtitle": "One row per buyer. Purchases count closed sales; a buyer comes back once they have bought more than once.",
			"column.buyer_name":       "Buyer",
			"column.phone_number":     "Phone",
			"column.category":         "Category",
			"column.place":            "Place",
			"column.purchases":        "Purchases so far",
			"column.animals":          "Animals",
			"column.revenue":          "Revenue",
			"column.share_pct":        "Share",
			"column.repeat":           "Comes back",
			"column.first_sale_date":  "First sale",
			"column.last_sale_date":   "Last sale",
			"column.outstanding":      "Still owed",

			// Chips and cell copy.
			"chip.repeat":   "Repeat",
			"chip.one_time": "One-time",
			// COMPATIBILITY, not display. The 2026-09-16 frontend stopped rendering the register
			// flag, but the deploy switches backend traffic before admin-web, and the previous
			// frontend calls copy() on both keys for every name-only buyer -- and copy() throws on
			// a missing fixed key, taking the whole page down for that window. They stay served
			// until every deployed frontend is past that build; pinned by
			// TestSalesBuyerAnalyticsCopyKeepsTheRetiredRegisterKeysForOlderFrontends.
			"chip.not_in_register":   "Not in register",
			"hint.not_in_register":   "Known only by the name typed on the sale. Add them on Vendors to keep their number here.",
			"value.every_days":       "about every {days} days",
			"value.days_ago":         "{days} days ago",
			"value.today":            "today",
			"value.repeat_purchases": "{count} more after the first",
			"value.settled":          "Nothing owed",
			"value.none":             "Not recorded",
			"hint.phone_hidden":      "Phone numbers are on the vendor register, which your current role cannot open.",

			"summary.buyers":   "buyers",
			"pager.page":       "Page",
			"pager.of":         "of",
			"action.prev_page": "Back",
			"action.next_page": "Next",

			"empty.buyers": "No buyers yet. Buyers appear here as sales close.",
			"error.load":   "Could not load the buyer figures. Refresh to try again.",
		}
	case "sales-market-analytics":
		// Backend-owned copy for the Market analytics page (maintainer decision 2026-09-14). The
		// client renders these verbatim. Question labels, unit labels and city names are NOT here:
		// they are the farm's own config, served on each row as the words the price was recorded
		// against.
		return map[string]string{
			"crumb": "Sales",

			"kpi.cities.label":      "Cities phoned",
			"kpi.cities.hint":       "with a price in this window",
			"kpi.days.label":        "Days recorded",
			"kpi.days.hint":         "mornings with at least one price",
			"kpi.latest.label":      "Latest prices on",
			"kpi.latest.hint":       "the most recent morning recorded",
			"kpi.coverage.label":    "Today's calls",
			"kpi.coverage.hint":     "cities done of cities on the list",
			"filter.window.label":   "Window",
			"filter.window.30":      "Last 30 days",
			"filter.window.90":      "Last 90 days",
			"filter.window.180":     "Last 6 months",
			"filter.window.365":     "Last year",
			"filter.question.label": "Question",
			"filter.question.all":   "All questions",
			"filter.city.label":     "City",
			"filter.city.all":       "All cities",

			"section.latest.title": "Latest prices by city",
			"section.latest.sub":   "The most recent morning's answer for each question, with the change from the morning before.",
			"section.latest.aria":  "Latest market prices by city",
			"column.city":          "City",
			"column.recorded_on":   "Recorded on",
			"column.change":        "Change",
			"value.none":           "—",
			"value.no_previous":    "first entry",
			"section.trend.title":  "Price over time",
			"section.trend.sub":    "One line per city. A question whose unit was changed starts a new line from that morning, so old prices are never rescaled.",
			"section.trend.aria":   "Market price trend by city",
			"chart.trend.empty":    "No prices recorded in this window yet.",
			"empty.analytics":      "No market prices yet. They appear here as the morning calls are entered on the phone.",
			"empty.config":         "No cities on the call list yet. Add the markets to phone on Sales Config.",
			"link.sales_config":    "Manage cities and questions",
			"error.load":           "Could not load the market prices. Refresh to try again.",
		}
	case "sales-sold", "sales-farm-value":
		// Sold and Farm value (maintainer decision 2026-09-11) render the blocks of the retired
		// Sales board, so they carry its copy map verbatim -- one source per label, so a KPI
		// that moved pages cannot come back with a different name. The "sales" map below is that
		// source; no page is served under that id any more. Only the load-error sentence is
		// each page's own.
		out := map[string]string{}
		for key, value := range pageSpecificCopy("sales") {
			out[key] = value
		}
		if id == "sales-sold" {
			out["error.load"] = "Could not load the sold figures. Refresh to try again."
		} else {
			out["error.load"] = "Could not load the farm value figures. Refresh to try again."
		}
		return out
	case "sales":
		// Backend-owned copy for the sales page. The client renders these verbatim; per the golden
		// rule it must not hardcode a label, an empty state or a disabled reason of its own. Farm
		// language only.
		return map[string]string{
			// Sales is its own vertical now, not a Procurement leaf (maintainer decision
			// 2026-08-27), so the crumb names itself rather than the desk it used to sit under.
			"crumb": "Sales",

			// Section headings.
			//
			// FARM VALUE AND SOLD ARE TWO BLOCKS, NOT ONE ROW (maintainer decision 2026-09-10).
			// They were read side by side in a single headline strip, where "Total farm value"
			// sat next to "Recorded sales revenue" -- two figures about DIFFERENT herds. Farm
			// value is what is standing on the farm right now; the sales figures are what has
			// already left it. Read together they invite a subtraction that means nothing, so
			// each block now names what it is about before its first number.
			"section.headline.title":   "Sales at a glance",
			"section.headline.aria":    "Sales headline figures",
			"section.farm_value.title": "Farm value",
			"section.farm_value.sub":   "What the live herd is worth today, at Sales target rates.",
			"section.farm_value.aria":  "Live herd valuation",
			// The breakdown card's own title. It divides the total above it, so repeating
			// "Total farm value" here would name the block twice and say nothing about the card.
			"section.farm_value.breakdown": "By category",
			"section.sold.title":           "Sold",
			"section.sold.sub":             "What has actually left the farm, and what it fetched.",
			"section.sold.aria":            "Sales and sold figures",
			"section.monthly.title":        "Month by month",
			"section.monthly.aria":         "Monthly sales trend",
			"section.price_bands.title":    "Price per kg by breed",
			"section.price_bands.aria":     "Realized price bands",
			"section.market.title":         "Market check",
			"section.market.subtitle":      "What other sellers quote per kg, next to our own realized price.",
			"section.buyers.title":         "Buyers",
			"section.buyers.subtitle":      "Who buys from us, what they buy, and how much of the revenue they carry.",
			"section.ledger.title":         "Deals",
			"section.ledger.aria":          "Sales ledger",
			"section.ledger.row_hint":      "click a row to see full details",

			// Headline KPI labels.
			"kpi.revenue":             "Recorded sales revenue",
			"kpi.farm_value":          "Total farm value",
			"kpi.farm_value.detail":   "Live herd valuation from Sales target rates",
			"kpi.total_meat":          "Total meat",
			"kpi.total_meat.detail":   "Live herd kg from valuation buckets",
			"kpi.animals":             "Animals sold",
			"kpi.animals.detail":      "sheep and goats, closed deals",
			"kpi.realized_price":      "Realized price per kg",
			"kpi.realized_price.hint": "Closed live-animal revenue over live weight sold.",
			"kpi.manure":              "Manure sold",
			"kpi.manure.detail":       "kg and revenue from manure deals",
			"kpi.period":              "Covering",
			"kpi.deals":               "Closed deals",
			"kpi.live_weight":         "Live weight sold",
			// Over 35 kg (maintainer request 2026-09-03): the sale-weight count from the Weights
			// pages. Same basis as the Weights cards: every kid weighed in the sale-ready window
			// at its latest weight, a whole pen counted at its average.
			// {kg} is the tenant's sale-ready line (growth_assumptions, edited from the Weighing SOP
			// Assumptions drawer -- maintainer decision 2026-09-19); the page fills it from the
			// figure the count was actually taken against.
			"kpi.over35":             "Over {kg} kg",
			"kpi.over35.sub":         "sale-ready window · a whole pen counts at its average",
			"kpi.over35.none":        "nothing weighed in the sale-ready window",
			"kpi.over35.tolerance":   "Error margin",
			"kpi.over35.apply":       "Apply",
			"disabled.weights":       "Your current role can view sales but not weighing.",
			"value.kg_suffix":        "kg",
			"value.excluded_animals": "excluded",
			"value.live_animals":     "live animals",
			"value.not_valued":       "not valued",
			"value.valued_animals":   "valued",
			// The fattening and kid cards' sex split (maintainer request 2026-09-11): male and
			// female only -- the maintainer asked not to show the unrecorded remainder, so an
			// animal with no recorded sex is counted in the card and named in neither figure.
			"value.sex.male":      "Male",
			"value.sex.female":    "Female",
			"value.weighed":       "weighed",
			"value.per_kg_suffix": "per kg",
			"value.none":          "Not recorded",
			"value.farm_all":      "Both farms",

			// Charts: label, what the value is, and the empty state -- one set per chart.
			"chart.monthly_revenue.title": "Sales revenue by month",
			"chart.monthly_revenue.value": "Revenue",
			"chart.monthly_revenue.empty": "No closed sales in this view yet.",
			"chart.monthly_animals.title": "Animals sold by month",
			"chart.monthly_animals.value": "Animals",
			"chart.monthly_animals.sub":   "Animal revenue",
			"chart.monthly_animals.empty": "No animals sold in this view yet.",
			"chart.monthly_manure.title":  "Manure sold by month",
			"chart.monthly_manure.value":  "Manure (kg)",
			"chart.monthly_manure.sub":    "Manure revenue",
			"chart.monthly_manure.empty":  "No manure sales in this view yet.",
			"chart.price_bands.title":     "Price per kg by breed",
			"chart.price_bands.value":     "Price per kg",
			"chart.price_bands.empty":     "No weighed and priced sales to compare yet.",
			"chart.series.sheep":          "Sheep",
			"chart.series.goat":           "Goats",
			"chart.series.manure":         "Manure",

			// Buyer board.
			"column.buyer_name":    "Buyer",
			"column.buyer_place":   "Place",
			"column.product_types": "Buys",
			"column.deals":         "Deals",
			"column.animals":       "Animals",
			"column.revenue":       "Revenue",
			"column.share_pct":     "Share of revenue",
			"empty.buyers":         "No buyers on the board yet. Buyers appear as deals close.",

			// Sold animals by weight (maintainer decision 2026-09-08): the live weight recorded
			// when each animal was tagged to its sale, in the maintainer's four bands.
			"section.sold_weight.title": "Sold animals by weight",
			// The subtitle names all three kinds of evidence, because the screen shows all three
			// and a reader who is told only about tagging would read a load average as a scale
			// reading.
			"section.sold_weight.subtitle": "Weighed one by one where each animal was tagged to its sale, otherwise at the weight recorded for the load it went out in. All sales.",
			"section.sold_weight.aria":     "Sold animals by weight band",
			// Keyed by the band's own wire value (domain.SoldBand*), so the page renders
			// copy(`sold_weight.band.${band.band}`) and a band cannot arrive with no label
			// because the two vocabularies drifted apart.
			"sold_weight.band.under_20":       "Below 20 kg",
			"sold_weight.band.from_20_to_35":  "20 to 35 kg",
			"sold_weight.band.from_35_to_40":  "35 to 40 kg",
			"sold_weight.band.at_or_above_40": "40 kg and above",
			"sold_weight.unweighed":           "sold without a recorded weight",
			"sold_weight.total":               "animals sold",
			"sold_weight.source.measured":     "weighed",
			"sold_weight.source.load_average": "at load average",
			"sold_weight.source.estimated":    "estimated",
			"sold_weight.estimated.note":      "Estimated animals were never weighed; their band is worked out from what the sale fetched and is kept out of the price per kg.",
			"empty.sold_weight":               "No sales recorded yet.",
			"field.animal_weight":             "Weight (kg)",
			"hint.animal_weight":              "Enter every animal's live weight in kg before confirming.",

			// Market check table.
			"column.market":              "Market",
			"column.category":            "Animal",
			"column.breed":               "Breed",
			"column.source":              "Quoted by",
			"column.ex_farm_rate":        "Ex-farm rate",
			"column.transport_rate":      "Transport",
			"column.landing_cost_per_kg": "Landed cost per kg",
			"column.market_price_per_kg": "Market price per kg",
			"column.market_gap":          "Loss per kg",
			"empty.market":               "No market quotes recorded yet.",

			// Ledger columns.
			"column.sale_date":       "Date",
			"column.farm":            "Farm",
			"column.product_type":    "Product",
			"column.animal_count":    "Animals",
			"column.total_weight_kg": "Weight (kg)",
			"column.sales_value":     "Value",
			"column.status":          "Status",
			"empty.deals":            "No sales match this view.",
			"empty.deals.unset":      "No sales recorded yet. Record the first sale to start the ledger.",
			"summary.count":          "deals",
			"summary.buyers":         "buyers",

			// "Tag animals to sale": pick the real animals a recorded sale is made of.
			// The blockers' own sentences are composed by the identity module and rendered
			// verbatim, so they are deliberately NOT duplicated here -- two copies of a
			// medical refusal is how the two come to disagree.
			"action.tag_animals.label":  "Tag animals to sale",
			"action.tag_animals.hint":   "Pick the animals this sale is made of, then mark them sold.",
			"disabled.allocate_animals": "Tagging animals to a sale needs herd allocation access.",
			"action.done":               "Done",
			"action.confirm_sold":       "Confirm and mark sold",
			"action.load_more":          "Show more animals",
			"field.sale":                "Sale",
			"field.park":                "Park",
			"field.shed":                "Pen",
			"field.search_tag":          "Find a tag",
			"value.choose_park":         "Choose a park",
			// An empty park list must say WHY. "Cannot be read" is a grant/plumbing fact a
			// person can act on; "no pens" is a farm fact. One sentence for both would
			// leave the reader guessing which of the two they are looking at.
			"empty.parks":             "No parks with pens to sell from.",
			"empty.parks_unavailable": "Parks could not be loaded. Tagging animals needs herd allocation access.",
			"value.search_tag_hint":   "RFID or animal ID",
			"value.all_sheds":         "All pens",
			"label.selected":          "selected",
			"label.still_to_pick":     "still to pick",
			"label.all_picked":        "All picked",
			"label.cannot_sell":       "Cannot be sold yet",
			"label.marked_sold":       "animals are tagged to this sale and marked sold.",
			"hint.review":             "Check the animals below before confirming. Once confirmed they leave the herd.",
			"hint.pick_all_prefix":    "Pick all",
			"hint.pick_all_suffix":    "animals for this sale before confirming.",
			"hint.too_many":           "That is more animals than this sale is for.",
			"hint.sale_no_count":      "This sale does not say how many animals it is for, so animals cannot be tagged to it.",

			// Filters.
			"filter.farm":  "Farm",
			"filter.all":   "All farms",
			"filter.clear": "Clear filters",

			// Record-sale drawer.
			"action.record_sale.label": "Record sale",
			// Payments section of the deal drawer: receipts, running totals, and the add-payment
			// control. A buyer pays in parts — an advance, more on pickup, the balance later.
			"section.payments.title":           "Payments",
			"payments.received_so_far":         "Received so far",
			"payments.balance":                 "Balance to receive",
			"payments.empty":                   "No payments recorded against this deal yet.",
			"payments.column.received_on":      "Received on",
			"payments.column.amount":           "Amount",
			"payments.column.note":             "Note",
			"field.received_on":                "Received on",
			"field.amount_rupees":              "Amount received",
			"field.note":                       "Note",
			"hint.record_payment":              "Record each amount as the buyer hands it over — the balance updates itself.",
			"action.record_deal_payment.label": "Add payment",
			"action.update_deal_payment.label": "Save payment",
			"action.delete_deal_payment.label": "Remove payment",
			"field.status":                     "Deal status",
			"hint.status":                      "Leave as Deal Closed for a finished sale. Pick Advance Paid or In Discussion to record an expected sale — close it on the day the animals leave.",
			"action.update_deal_status.label":  "Update status",
			"action.deal_status_updated":       "Deal status updated.",
			"action.deal_status_update_failed": "Could not update the deal status. Try again.",
			"action.payment_recorded":          "Payment recorded.",
			"action.payment_updated":           "Payment updated.",
			"action.payment_deleted":           "Payment removed.",
			"action.payment_record_failed":     "Could not record this payment. Check the fields and try again.",
			"action.payment_update_failed":     "Could not update this payment. Check the fields and try again.",
			"action.payment_delete_failed":     "Could not remove this payment. Try again.",
			"drawer.record_sale.title":         "Record a sale",
			"drawer.detail.title":              "Sale details",
			"field.sale_date":                  "Sale date",
			"field.farm":                       "Farm",
			"field.product_type":               "Product",
			"field.breed":                      "Breed",
			"field.vendor":                     "Vendor",
			"field.buyer_name":                 "Buyer name",
			"field.buyer_place":                "Buyer place",
			"field.animal_count":               "Animals",
			"field.male_count":                 "Males",
			"field.female_count":               "Females",
			"field.total_weight_kg":            "Total weight (kg)",
			"field.sales_value":                "Sale value",
			"field.advance_amount":             "Advance received",
			"field.comments":                   "Comments",
			"required.hint":                    "Sale date, farm, vendor, buyer name and at least one product line with its value are required.",
			// ONE sale, MANY lines (maintainer decision 2026-09-12): sheep and goats of different
			// breeds sold to one buyer are recorded as one deal with a line per product/breed,
			// each carrying its own count, weight and value. The deal total is the sum, computed
			// by the backend; the drawer only previews it.
			"section.lines.title":        "What was sold",
			"hint.lines":                 "Add one line per product and breed. Each line carries its own animals, weight and value; the sale total adds up on its own.",
			"action.add_line":            "Add another product",
			"action.remove_line":         "Remove",
			"label.line":                 "Line",
			"field.line_animal_count":    "Animals",
			"field.line_total_weight_kg": "Weight (kg)",
			"field.line_sales_value":     "Value (₹)",
			"summary.lines.total":        "Sale total",
			"summary.lines.animals":      "animals",
			"summary.lines.weight":       "kg",
			"summary.lines.lines":        "lines",
			// The ledger row's product/breed cells for a deal whose lines disagree.
			"value.mixed":        "Mixed",
			"detail.lines.empty": "This sale has no product lines.",
			// The vendor select's own copy. Farm language, and it must name WHERE to go: a
			// required select the person cannot fill is a dead end without it.
			"select.vendor.placeholder": "Choose the vendor",
			// The register runs to a few hundred names, which is more than anyone scrolls. Typing
			// two characters narrows the list; below two it stays whole, so a stray keystroke does
			// not blank the dropdown.
			"search.vendor.placeholder": "Search vendors — type 2 letters to narrow",
			"hint.vendor_no_match":      "No vendor matches that search. Clear it, or add them on the Vendors page.",
			"action.clear_search":       "Clear",
			"hint.vendor":               "Every sale is made to a vendor. Not on this list? Add them on the Vendors page, then come back.",
			"hint.vendor_empty":         "No vendors are on the register yet. Add the buyer on the Vendors page first, then record the sale.",
			"hint.vendor_truncated":     "The vendor register is too large to show completely here. Search and confirm the buyer on the Vendors page first, then come back.",
			"action.open_vendors":       "Go to Vendors",
			"hint.vendor_prefill":       "Buyer name and place are filled in from the vendor. Change them if this sale was made under a different name.",
			"error.vendors_unavailable": "The vendor register could not be read, so a sale cannot be recorded right now. Refresh to try again.",
			// The shared date control's copy. Backend-owned like every other label: the picker is a
			// rendering component and composes none of its own words.
			"date.sale_date.placeholder": "Pick the sale date",
			"date.prev_month":            "Previous month",
			"date.next_month":            "Next month",
			"date.invalid_sale_date":     "Pick the day the sale happened. It cannot be later than {date}.",
			"action.save":                "Save",
			"action.saving":              "Saving...",
			"action.cancel":              "Cancel",
			"action.close":               "Close",
			"action.next_page":           "Next",
			"action.prev_page":           "Back",
			"pager.page":                 "Page",
			"pager.of":                   "of",
			"action.sale_recorded":       "Sale recorded.",
			"action.sale_record_failed":  "Could not record this sale. Check the fields and try again.",
			"action.error_form":          "Could not complete that action.",

			// Load/permission states.
			"error.load":     "Could not load the sales board. Refresh to try again.",
			"error.save":     "Could not record this sale.",
			"disabled.write": "Your current role can view sales but not record them.",
		}
	case "source-entry":
		return map[string]string{
			"crumb":                          "Procurement",
			"section.loads.title":            "Supplier warmup — Holding Farm",
			"section.loads.aria":             "Source-entry loads",
			"section.loads.badge":            "journey starts at purchase",
			"section.loads.note":             "purchase → tag + vaccinate → health select → pre-dispatch",
			"section.loads.row_hint":         "click a row → source-load actions",
			"section.journey.aria":           "Source-entry journey",
			"filter.drawer.title":            "Filter — Source Entry loads",
			"filter.search_label":            "Search source-entry loads",
			"filter.search_reason":           "Search load, supplier, purpose, status...",
			"filter.reason":                  "Use visible-row search, quick facets, and live status chips on this board.",
			"filter.rows_suffix":             "source loads and HF evidence",
			"filter.all_states":              "All states",
			"drawer.load.aria":               "Source load actions",
			"drawer.load.close_label":        "Close Holding Farm load drawer",
			"drawer.load.eyebrow":            "SOURCE LOAD",
			"drawer.load.title_prefix":       "Holding-farm load",
			"drawer.load.supplier":           "Supplier",
			"drawer.load.holding_farm":       "Holding farm",
			"drawer.load.expected_animals":   "Expected animals",
			"drawer.load.goats_in_load":      "Goats in load",
			"drawer.load.note":               "This load starts before park arrival: purchase/source → holding warmup → HF vaccination evidence → health selection → pre-dispatch. Accepted-intake goats then feed the Preventive Care (PC) vaccination flow.",
			"action.new_load":                "New load",
			"action.open_detail":             "Open detail",
			"action.open_load_actions":       "Open load actions",
			"action.record_hf_evidence":      "Record HF evidence",
			"action.open_source_entry":       "Open Source Entry",
			"action.load_created":            "Load created.",
			"action.source_goat_added":       "Source animal added to load.",
			"action.hf_evidence_imported":    "HF vaccination evidence imported.",
			"action.hf_evidence_reviewed":    "HF evidence reviewed.",
			"action.source_health_recorded":  "Source health recorded.",
			"action.pre_dispatch_recorded":   "Pre-dispatch decision recorded.",
			"action.dispatch_recorded":       "Dispatch / transit recorded.",
			"action.arrival_review_recorded": "Arrival review recorded.",
			"action.accept_intake_recorded":  "Accepted intake recorded; Preventive Care (PC) handoffs created when eligible.",
			"empty.loads":                    "No source-entry loads for this scope.",
			"empty.loads_detail":             "No source-entry loads for this scope. Loads appear here once a purchase/source load is created in the procurement backend.",
			"empty.loads_filtered_prefix":    "No loads in",
			"empty.loads_filtered_suffix":    "for this scope.",
			"empty.unavailable":              "Loads are unavailable until the procurement service responds.",
			"label.holding_not_set":          "Holding not set",
			"label.supplier_prefix":          "supplier",
			"label.from_date_prefix":         "from",
			"label.purchase_date_missing":    "purchase date missing",
			"label.mixed_windows":            "mixed windows",
			"label.mixed":                    "mixed",
			"label.rows":                     "rows",
			"warmup.mixed_note":              "mixed purpose load — review per-goat warmup in load detail",
			"form.new_load.title":            "New load",
			"field.source_party_id":          "Source party id (required)",
			"field.source_location_id":       "Source / holding location id",
			"field.expected_count":           "Expected count",
			"field.purchase_date":            "Purchase date",
			"field.planned_dispatch":         "Planned dispatch",
			"field.notes":                    "Notes",
			"placeholder.source_party_id":    "uuid of supplier / source party",
			"placeholder.source_location":    "holding farm location uuid (optional)",
			"placeholder.zero":               "0",
			"placeholder.optional":           "optional",
			"action.create_load":             "Create load",
			"journey.purchase_source":        "Purchase / source",
			"journey.holding_warmup":         "Holding warmup",
			"journey.source_health_sop":      "Source health SOP",
			"journey.pre_dispatch":           "Pre-dispatch",
			"journey.transit":                "Transit",
			"journey.arrival_gate":           "Arrival gate",
			"journey.accepted_intake":        "Accepted intake",
		}
	case "source-load":
		return map[string]string{
			"fallback.title":                    "Load detail",
			"section.timeline.title":            "Journey timeline",
			"section.timeline.note":             "purchase → holding → source SOP → pre-dispatch → transit → arrival → intake",
			"section.goats.title":               "Animals in load",
			"section.goats.note":                "per-animal journey state — not load totals only",
			"section.goats.boundary_note":       "Only accepted intake animals link out to Preventive Care (PC). Rejected-before-truck, arrival-rejected, dead/sold/lost, and unresolved animals stay procurement history and are never shown as Preventive Care (PC) vaccination work.",
			"section.pre_dispatch.title":        "Pre-dispatch decisions",
			"section.pre_dispatch.note":         "accept · reject before truck · defer · block — audited",
			"section.pre_dispatch.empty":        "No pre-dispatch decisions recorded yet. A decision (accept for truck, reject before truck, defer, or block) is recorded through the procurement backend with reason, proof, and audited actor/time.",
			"section.arrival_gate.title":        "Arrival gate",
			"section.arrival_gate.note":         "distinct checkpoint — reconciled before accepted herd intake",
			"section.arrival_gate.empty":        "No arrival review yet. The arrival gate reconciles expected vs arrived animals (matched / missing / extra, health and weight flags) at the park before any animal becomes accepted herd truth.",
			"section.transit.title":             "Transit handoffs",
			"section.holding.title":             "Holding stays",
			"section.holding.note":              "procurement holding 28–35d · only supervised holding-park vaccines in this window can be trusted",
			"section.source_health.title":       "Source health checks",
			"section.pc_handoffs.title":         "Accepted intake → Preventive Care (PC) handoffs",
			"section.pc_handoffs.note":          "only accepted intake makes an animal eligible for post-arrival Preventive Care (PC)",
			"section.actions.title":             "Source-entry actions",
			"section.actions.note":              "add animal · source health · pre-dispatch · dispatch · arrival · accept intake",
			"action.back_source_entry":          "Back to Source Entry",
			"action.pc_passport":                "Preventive Care (PC) passport",
			"empty.timeline":                    "No journey events recorded for this load yet.",
			"empty.load_goats":                  "No animals added to this load yet.",
			"label.event_fallback":              "event",
			"label.goat_prefix":                 "animal",
			"label.arrival_prefix":              "arrival",
			"label.park_prefix":                 "park",
			"label.matched":                     "matched",
			"label.missing":                     "missing",
			"label.extra_unknown":               "extra/unresolved",
			"label.reviewed":                    "reviewed",
			"label.expected":                    "expected",
			"label.load":                        "Load",
			"label.purchase":                    "purchase",
			"label.planned_dispatch":            "planned dispatch",
			"label.procurement_history_no_pc":   "procurement history — no Preventive Care (PC) work",
			"label.in_source_entry":             "in source entry",
			"label.holding_farm":                "Holding farm",
			"label.placeholder":                 "—",
			"label.ongoing":                     "ongoing",
			"label.accepted":                    "accepted",
			"label.proof":                       "proof",
			"label.trusted_locked":              "trusted · locked",
			"label.vaccine_name_not_set":        "vaccine name not set",
			"label.proof_ref_not_set":           "proof ref not set",
			"label.pc_handoff_note":             "Accepting intake is the only handoff that makes a procured animal eligible for post-arrival Preventive Care (PC). It runs once and is idempotent.",
			"form.add_goat.title":               "Add source animal",
			"form.hf_evidence.title":            "Holding-farm vaccination evidence",
			"form.pre_dispatch.title":           "Pre-dispatch decisions (per goat)",
			"form.dispatch.title":               "Record dispatch / transit",
			"form.arrival_review.title":         "Record arrival review",
			"form.accept_intake.title":          "Accept intake",
			"field.animal_identifier_1":         "Animal ID 1",
			"field.animal_identifier_2":         "Animal ID 2",
			"field.species":                     "Species",
			"field.sex":                         "Sex",
			"field.selection_state":             "Selection state",
			"field.health_state":                "Health state",
			"field.ownership":                   "Ownership",
			"field.purpose":                     "Purpose",
			"field.warmup_days":                 "Warmup days",
			"field.holding_location_id":         "Holding location id",
			"field.goat_in_load":                "Goat in load",
			"field.dose_code":                   "Dose code",
			"field.administered_at_hf":          "Administered at HF",
			"field.protocol_version_id":         "Protocol version id",
			"field.rule_id":                     "Rule id",
			"field.vaccine_name":                "Vaccine name",
			"field.lot_number":                  "Lot number",
			"field.proof_ref_id":                "Proof ref id",
			"field.source_ref":                  "Source ref",
			"field.source_health":               "Source health",
			"field.reason":                      "Reason",
			"field.pre_dispatch":                "Pre-dispatch",
			"field.to_location_id":              "Destination park (required)",
			"field.from_location_id":            "From location (optional)",
			"field.goat_ids":                    "Goat ids (comma separated)",
			"field.dispatched_at":               "Dispatched at",
			"field.dispatch_proof_ref_id":       "Dispatch proof ref id (required for real transit)",
			"field.park_location_id":            "Park (required)",
			"field.review_status":               "Review status",
			"field.arrival_rows":                "Per-goat arrival rows (one per line: goat_id, arrival_state)",
			"field.shed_location_id":            "Pen (required)",
			"field.entry_date":                  "Entry date",
			"field.intake_health_signal":        "Intake health signal",
			"field.count_expected":              "Expected",
			"field.count_loaded":                "Loaded",
			"field.count_arrived":               "Arrived",
			"field.count_matched":               "Matched",
			"field.count_missing":               "Missing",
			"field.count_extra":                 "Extra",
			"field.count_rejected":              "Rejected",
			"placeholder.animal_identifier_1":   "globally unique animal id 1",
			"placeholder.animal_identifier_2":   "globally unique animal id 2",
			"placeholder.species":               "select species",
			"placeholder.sex":                   "select sex",
			"placeholder.warmup_days":           "e.g. 52",
			"placeholder.holding_location_id":   "holding location uuid (optional)",
			"placeholder.dose_code":             "e.g. PPR",
			"placeholder.protocol_version_id":   "vaccination protocol version uuid",
			"placeholder.rule_id":               "protocol rule uuid",
			"placeholder.optional":              "optional",
			"placeholder.proof_ref_id":          "cold-chain / video proof uuid",
			"placeholder.source_ref":            "supplier bill / field-app ref / sheet row",
			"placeholder.reason":                "reason",
			"placeholder.to_location_id":        "destination park location uuid",
			"placeholder.from_location_id":      "source location uuid (optional)",
			"placeholder.goat_ids_dispatch":     "only accepted-for-truck goats",
			"placeholder.dispatch_proof_ref_id": "proof artifact uuid",
			"placeholder.park_location_id":      "arrival park location uuid",
			"placeholder.zero":                  "0",
			"placeholder.arrival_rows":          "goat_id, accepted\ngoat_id, rejected",
			"placeholder.goat_ids_intake":       "only arrival-accepted, eligible goats",
			"placeholder.park_id":               "park location uuid",
			"placeholder.shed_id":               "pen location uuid",
			"location.select_park":              "select park...",
			"location.select_shed":              "select pen...",
			"location.select_park_first":        "select a park first",
			"location.select_optional_location": "select location...",
			"location.no_parks":                 "No active parks are available from Location master.",
			"location.no_origins":               "No active source locations are available from Location master.",
			"location.no_sheds":                 "No vaccination-usable pens are available from Location master.",
			"location.no_sheds_for_park":        "No vaccination-usable pens are available for the selected park.",
			"location.locations_unavailable":    "Location master could not be loaded, so dispatch, arrival, and intake location writes are disabled until it reloads.",
			"action.add_source_goat":            "Add source animal",
			"action.import_hf_evidence":         "Import HF dose evidence",
			"action.review":                     "Review",
			"action.record_health":              "Record health",
			"action.record_decision":            "Record decision",
			"action.record_dispatch":            "Record dispatch",
			"action.record_arrival_review":      "Record arrival review",
			"action.accept_intake":              "Accept intake",
			"action.load_created":               "Load created.",
			"action.source_goat_added":          "Source animal added to load.",
			"action.hf_evidence_imported":       "HF vaccination evidence imported.",
			"action.hf_evidence_reviewed":       "HF evidence reviewed.",
			"action.source_health_recorded":     "Source health recorded.",
			"action.pre_dispatch_recorded":      "Pre-dispatch decision recorded.",
			"action.dispatch_recorded":          "Dispatch / transit recorded.",
			"action.arrival_review_recorded":    "Arrival review recorded.",
			"action.accept_intake_recorded":     "Accepted intake recorded; Preventive Care (PC) handoffs created when eligible.",
			"action.upload_media_disabled":      "Upload media proof (not in this slice)",
			"reason.media_disabled":             "Media capture is not built in this frontend slice — enter a known proof ref id, or upload via the field app / proof API",
			"empty.add_goats_first":             "Add source animals first. HF dose evidence must be keyed to an animal in this procurement load.",
			"empty.hf_evidence":                 "No HF vaccination evidence imported for this load yet.",
			"empty.pre_dispatch":                "No animals currently awaiting source health or a pre-dispatch decision. Accepted-intake and terminal animals are not shown here.",
			"note.source_goat_identity":         "A source animal is procurement-only until accepted intake; rejected rows stay procurement history and never become herd truth.",
			"note.arrival_rows":                 "Only animals listed as accepted advance to arrival-accepted and become eligible for intake.",
			"confirm.pre_dispatch.prefix":       "Record pre-dispatch decision for",
			"confirm.pre_dispatch.suffix":       "Rejection before truck keeps the goat in procurement history (no Preventive Care (PC) work).",
			"confirm.accept_intake":             "Accept these animals into the herd and create Preventive Care (PC) handoffs? Only run after arrival reconciliation.",
			"table.hf_evidence.aria":            "HF vaccination evidence",
			"table.hf_evidence.goat":            "Goat",
			"table.hf_evidence.dose":            "Dose",
			"table.hf_evidence.administered":    "Administered",
			"table.hf_evidence.evidence":        "Evidence",
			"table.hf_evidence.review":          "Review",
		}
	case "herd-signals":
		// Every visible string on /herd-signals. The renderer owns layout only.
		//
		// CLAIM BOUNDARY: a HoneyComm BLE tag reports tag id, MAC, RSSI, battery mV,
		// TAG temperature, a cumulative motion counter, sensor-OK bits and timestamps.
		// It detects no behaviour, no posture and no clinical state, so no label here
		// may say eating, rumination, standing, walking, fever, body temperature or
		// disease. "Tag temp" is never "Body temp".
		return map[string]string{
			"page.crumb":               "Herd Signals / Live Monitor",
			"tab.live":                 "Live Monitor",
			"tab.animals":              "Animals",
			"tab.gateways":             "Gateways",
			"tab.alerts":               "Alerts",
			"tab.mapping":              "Tag Mapping",
			"tab.insights":             "Insights",
			"kpi.tags_seen":            "Tags seen",
			"kpi.moving":               "Tags moving",
			"kpi.quiet":                "Quiet tags",
			"kpi.weak_signal":          "Weak signal",
			"kpi.missing_signal":       "Missing signal",
			"kpi.low_battery":          "Low battery",
			"table.live.animal":        "Animal",
			"table.live.smart_tag":     "Smart tag",
			"table.live.shed":          "Pen",
			"table.live.gateway":       "Gateway",
			"table.live.signal":        "Signal",
			"table.live.motion_count":  "Motion count",
			"table.live.delta_15m":     "15m delta",
			"table.live.delta_1h":      "1h delta",
			"table.live.activity":      "Activity",
			"table.live.pattern":       "Pattern",
			"table.live.battery":       "Battery",
			"table.live.tag_temp":      "Tag temp",
			"table.live.last_seen":     "Last seen",
			"table.live.status":        "Status",
			"empty.no_packets":         "No gateway packets yet",
			"empty.no_packets_detail":  "No BLE gateway has posted a packet for this tenant. Check that the gateway is powered, on the site network, and configured with the ingest URL and credentials.",
			"empty.no_mapped":          "No mapped smart tags",
			"empty.no_mapped_detail":   "No active identifier is marked smart-tag capable yet. Map a tag in Tag Mapping and its signals resolve to an animal here.",
			"empty.no_alerts":          "No signal alerts",
			"empty.no_unmapped":        "No unmapped tags",
			"empty.filtered":           "Filters exclude every row in scope",
			"empty.no_history":         "No movement history",
			"error.read_failed":        "Could not load live signals",
			"error.read_failed_detail": "The read failed and no rows were returned. This is not the same as zero tags.",
			"state.stale":              "Showing stale data",
			"state.gateway_offline":    "Gateway offline",
			"disabled.mapping_write":   "Tag mapping writes are not built yet.",
			"disabled.export":          "Export is not built yet.",
			"note.correlation":         "Overlaid markers are other recorded farm activity for the same animal or its pen. Read them as correlation, never as behaviour, cause, or a clinical finding.",
			"note.activity_basis":      "Activity uses motion-count deltas from historical packets. Quiet periods are normal; alerts use sustained patterns.",
		}
	case "weighing-weights":
		// Every visible string on /weighing/weights. The renderer owns layout only.
		//
		// COPY FIREWALL: farm language throughout. No "bucket", "observation",
		// "campaign pen" or "per_shed_partition" reaches a screen — those are storage words.
		// The operator-facing words are "Lump sum" and "Per animal".
		//
		// "Lump sum" was previously banned here as a storage word and rendered "Whole pen".
		// The maintainer reversed that on 2026-08-17: lump-sum is what the farm calls this
		// capture mode, and it is the term the top-level workspace context uses for it
		// ("lump-sum -> total weight, animal count, video(s) -- per pen"). Do not revert it
		// to "Whole pen" on the strength of the older comment.
		return weighingWeightsCopy()
	// -------------------------------------------------------------------------------
	// WEIGHING -> WEIGHTS ANALYTICS. The same weighing facts as /weighing/weights, cut five
	// ways. It SHARES that page's copy base so the filter bar, the KPI strip and the shed
	// table cannot drift into two spellings; only the tab strip and the four analysis
	// sections are its own.
	//
	// COPY FIREWALL: farm language throughout. A reader sees breeds, sheds and weeks -- never
	// "bucket", "observation", "read model" or "lump-sum grain". The ONE technical term allowed
	// is "ADG" in the page/leaf NAME ("ADG Analytics", maintainer request 2026-09-03) -- the
	// maintainer named the screen; body copy still says "gain".
	//
	// THE ONE WORD THIS PAGE LIVES ON IS *GAIN*, and every gain figure on every tab is the SAME
	// statistic the headline reports (maintainer decision 2026-08-26): the animal-weighted mean
	// over kids weighed twice PLUS whole-shed pens. The captions say so wherever a reader could
	// otherwise assume a tab is measuring something narrower than the card above it.
	// -------------------------------------------------------------------------------
	case "weighing-analytics":
		analytics := weighingWeightsCopy()
		for key, value := range map[string]string{
			// The tab strip. Farm nouns, not dimension names: a reader picks "Breed-wise",
			// never "by_breed".
			// OVERRIDDEN, not inherited. The shared base labels this figure "weighed twice in the
			// selected period", but the number under it is summary.animals_weighed -- every kid
			// weighed AT LEAST once (184 scanned + 478 whole-shed = 662 on the landing window),
			// not the smaller set weighed twice. The gain cards below are the ones that speak for
			// kids weighed twice, and they carry their own denominator.
			//
			// Corrected HERE ONLY: /weighing/weights carries the same wrong sub-line from this
			// same base, and repairing that page was explicitly out of scope for this change.
			// Flagged for the maintainer rather than fixed silently on a screen nobody asked to
			// touch.
			"kpi.kids.split.total_sub": "weighed in the selected period",

			// The pens table's own weight filter (maintainer request 2026-09-03): the feed
			// config's more-than / less-than control, applied with ONE button, so a reader can
			// ask "which pens average over 30 kg" without leaving the tab. The vocabulary is
			// the weight_kg_compare option group; the value is typed, never enumerated.
			"filter.apply":             "Apply",
			"filter.pens_bar_aria":     "Filter pens",
			"filter.weight.label":      "Average weight (kg)",
			"filter.weight.value_aria": "Average weight in kilograms",
			"filter.weight.note":       "Only pens whose average weight matches",

			// The Time-wise tab's own two controls (maintainer request 2026-09-21). They are
			// tab-local: the bucket decides how this tab's columns are cut, the pen picker narrows
			// this tab's pen grid, and neither changes what another tab counts.
			"filter.gain_bucket.label": "Show",
			"filter.gain_bucket.note":  "Monthly counts back 30 days at a time from the last day of the selected period",
			"filter.time_pen.label":    "Pen",
			"filter.time_pen.note":     "Shows this tab for one pen only: its growth, its breeds and the load it sits in",
			// A pen the selected period holds no weighing for still narrows the page, so the control
			// must name it rather than reading as "All" above an empty tab. Nothing in the period can
			// give it its own name, so the option says what it is.
			"filter.time_pen.unlisted": "Selected pen — not weighed in this period",

			"tab.aria":    "ADG Analytics view",
			"tab.general": "General",
			"tab.breed":   "Breed-wise",
			"tab.birth":   "Birth-wise",
			"tab.shed":    "Pen-wise",
			"tab.weight":  "Weight-wise",
			"tab.time":    "Time-wise",

			// General. The summary this page opens on is the Weights page's own, so a reader
			// lands on figures they already recognise before the cuts below re-slice them.

			// Breed-wise. Two measures on one chart, and they are NOT the same population:
			// weight covers every kid weighed, gain only those weighed twice. The caption has to
			// say so, or the two bars read as one fact about one set of animals.
			"section.breed.title":   "Breed-wise growth",
			"section.breed.caption": "Daily gain and average weight for each breed, under the selected weighing mode. The two bars count different kids: weight covers every kid weighed, gain only those with a second weigh or a whole pen that moved.",
			"section.breed.aria":    "Daily gain and average weight by breed",
			"series.gain":           "Daily gain",
			"series.weight":         "Average weight",
			"empty.breed.body":      "No breed has a weighed kid in this period.",

			// Birth-wise. The two halves deliberately need not add up to the whole, and saying so
			// is the difference between an honest gap and apparent missing data.
			"section.birth.title":   "Farm born vs purchased",
			"section.birth.caption": "Daily gain for each breed under the selected weighing mode, split by where the kids came from. A breed shows one bar when the farm only has one kind. Kids whose origin is not recorded are counted in neither, so the two sides need not add up to the breed's own total.",
			"section.birth.aria":    "Daily gain by breed and origin",
			"empty.birth.body":      "No breed has a farm-born or purchased kid with a second weigh in this period.",

			// Pen-wise. Exactly the comparison requested in the voice note: elevated pens
			// against ground pens within every breed.
			"section.shed.title":          "Elevated vs ground pens",
			"section.shed.caption":        "Daily gain for each breed, split by physical pen type: elevated pen against ground pen. A pen without that profile is left out rather than guessed.",
			"section.shed.aria":           "Daily gain by breed and pen type",
			"empty.shed.body":             "No pen-type profile has daily gain in this period. Add elevated or ground pen profiles, then weigh twice.",
			"view.shed_type.elevated":     "Elevated pen",
			"view.shed_type.non_elevated": "Non-elevated pen",
			// The two bars come out of a classification the reader cannot see on the chart, so each
			// legend entry carries an info affordance naming the pens behind it. Backend-owned
			// copy, rendered verbatim -- the client composes no part of this sentence.
			"section.shed.members_hint":  "Which pens count as this",
			"section.shed.members_title": "Pens counted here",
			"empty.shed.members":         "No pen of this type had a second weigh in this period.",
			"value.shed.mixed":           "Mixed breeds",
			"value.shed.unknown":         "Breed not recorded",

			// Weight-wise. The head count is the headline here and the gain rides beside it, because
			// a bracket's growth is only worth reading once you know how many animals are standing
			// in it. The caption states the two things a reader would otherwise assume wrongly:
			// that a whole pen sits in one bracket, and that the gain is drawn from a smaller set.
			"section.weight.title":   "Weight-wise",
			"section.weight.caption": "How many animals stand in each weight bracket under the selected weighing mode, and how fast each bracket is growing. A whole pen sits entirely in the bracket its average weight falls into. The daily gain beside each bracket comes only from the animals in it weighed twice, so its head count is the smaller one.",
			"section.weight.aria":    "Animals and daily gain by weight bracket",
			"empty.weight.body":      "No kid was weighed in this period.",
			"series.animals":         "Animals",
			"value.weight.no_gain":   "no second weigh",
			// The six brackets. Farm words for the keys the backend bands on -- a reader sees
			// kilograms, never "under_15".
			"band.weight.under_15": "Under 15 kg",
			"band.weight.15_20":    "15 – 20 kg",
			"band.weight.20_25":    "20 – 25 kg",
			"band.weight.25_30":    "25 – 30 kg",
			"band.weight.30_35":    "30 – 35 kg",
			"band.weight.35_plus":  "35 kg and over",

			// Feed by weight band (maintainer request 2026-09-18). The table under the bracket
			// chart: what each pen is fed today, beside what its animals weigh. The caption
			// states the two things a reader would otherwise assume wrongly: that a whole-pen
			// weigh is split across bands (it is not -- one row on the pen average), and that
			// the feed is today's sheet (it is the latest LOCKED sheet, named in the line above
			// the table).
			"section.feed_band.title":         "Feed by weight band",
			"info.feed_band.caption":          "What each pen is being fed, placed against the observed 5 kg weight band of its animals — not consumption per band.",
			"section.feed_band.aria":          "Feed given by weight bracket and pen",
			"empty.feed_band.body":            "No fed pen has a weighing in this period.",
			"empty.feed_band.filtered":        "No row matches these filters.",
			"filter.feed_band.aria":           "Filter feed by weight band",
			"filter.feed_band.feed_type":      "Feed type",
			"filter.feed_band.weight_source":  "Weight source",
			"filter.feed_band.band":           "Band",
			"filter.feed_band.pen":            "Pen",
			"filter.feed_band.group":          "Group",
			"filter.feed_band.animals":        "Animals",
			"filter.feed_band.search":         "Search pen, group, breed or feed",
			"filter.feed_band.search_aria":    "Search the feed by weight band table",
			"value.feed_band.type.normal":     "Normal",
			"value.feed_band.type.experiment": "Experiment",
			// Compact pills on the desktop table so Breed and Feed given never truncate (maintainer,
			// 2026-09-19); the full words stay on each pill's title and on the phone cards.
			"value.feed_band.type.short.normal":        "Normal",
			"value.feed_band.type.short.experiment":    "Exp.",
			"value.feed_band.source.short.pen_average": "Lump",
			"value.feed_band.source.short.per_animal":  "Animal",
			"value.feed_band.animals.on_farm":          "On farm",
			"value.feed_band.animals.all":              "Include exited",
			"value.feed_band.no_gender":                "—",
			"value.feed_band.exited_suffix":            "exited",
			// The ⓘ beside the title: what the two head counts mean, why the feed repeats per band
			// row, what Excluded counts, and what the two pills are. Backend copy, rendered verbatim.
			"info.feed_band.hint":  "How to read this table",
			"info.feed_band.title": "Reading this table",
			// The former card caption, verbatim (maintainer request 2026-09-19: one-line caption, rules in the popover).
			"info.feed_band.plan":     "Feed plan = the locked daily feed-direction record in GoATOS for that date (what each pen was issued), not a spreadsheet.",
			"info.feed_band.rules":    "What each pen is fed on the latest locked feed plan, beside its weighing in the selected period, counted the way the General tab counts: a pen weighed whole on two dates is one row on its latest average; a pen weighed per animal is one row per weight bracket, each animal at its latest weigh after an earlier one. Park, period, weighing, sex and origin apply; the period bounds the weighings only. Animals that have since exited the register (sold, died or otherwise) sit outside the head count unless included.",
			"info.feed_band.wt_n":     "Wt n is the animals behind the weight: the bracket's head count on a per-animal row, the pen's head count at its latest whole-pen weigh on a lump-sum row.",
			"info.feed_band.kg_day":   "Pen kg/day is the whole rollup's feed for the day; a per-animal pen repeats it on every bracket row, so do not add those rows up.",
			"info.feed_band.excluded": "Excluded counts feed rollups whose pen has no qualifying weighing in the period, including pens whose weighed animals have all since exited.",
			"info.feed_band.badges":   "Lump sum marks a pen weighed whole, on its average; Per animal marks a pen whose animals were scanned one by one.",
			"info.feed_band.exited":   "Exited in period uses the same rule as Herd Analytics exits: every animal that left the register inside the period, under the park, sex and origin filters. Only the weighed ones can appear on a band row; the +N sold / died / other notes and the Include exited head counts are band evidence and count weighed animals only.",
			// The card's three views (maintainer decision 2026-09-18, the STG artifact's shape): one
			// shell, one pager, one search; the view decides the rows and which filters apply.
			"view.feed_band.aria":      "Feed by weight band view",
			"view.feed_band.matched":   "Matched",
			"view.feed_band.unmatched": "Not shown",
			"view.feed_band.exited":    "Exited",
			"note.feed_band.unmatched": "Feed rollups on the latest feed plan whose pen has no qualifying weighing in the selected period. They are on the plan and counted; they are never banded or guessed.",
			// The exited panel (the app's record drawer): opened from the count chip, the stat tile,
			// or a band row's "+N sold" note, scoped to what was clicked. Three buckets (sold /
			// died / other = domain.FeedExitBucket); the stored reason is shown beside the bucket.
			"drawer.feed_band.aria":        "Animals that exited in the period",
			"drawer.feed_band.eyebrow":     "Exited in period",
			"drawer.feed_band.close":       "Close",
			"drawer.feed_band.period":      "Period",
			"drawer.feed_band.animals":     "animals",
			"drawer.feed_band.animal":      "animal",
			"drawer.feed_band.search":      "Search tag, pen, reason or feed",
			"drawer.feed_band.search_aria": "Search the exited list",
			"empty.feed_band.unmatched":    "Every fed pen has a weighing in this period.",
			"empty.feed_band.exits":        "No animal exited in this period.",
			"value.feed_band.exits.never":  "not weighed in period",
			"value.feed_band.no_feed":      "no feed row for this pen today",
			"value.feed_band.sold":         "sold",
			"value.feed_band.died":         "died",
			"value.feed_band.other":        "other",
			"value.feed_band.incl":         "incl.",
			// The stat tiles under the filters: what the filtered rows add up to. "Animals weighed"
			// counts each pen × bracket × source once, however many feed rows the pen has.
			"stat.feed_band.rows":                       "Rows shown",
			"stat.feed_band.pens":                       "Pens",
			"stat.feed_band.lump":                       "Lump sum",
			"stat.feed_band.per_animal":                 "Per animal",
			"stat.feed_band.animals":                    "Matched animals",
			"stat.feed_band.total_weighed":              "total weighed in period",
			"stat.feed_band.exited":                     "Exited in period",
			"stat.feed_band.sold":                       "Sold",
			"stat.feed_band.died":                       "Died",
			"stat.feed_band.other":                      "Other",
			"stat.feed_band.weighed":                    "weighed",
			"stat.feed_band.not_weighed":                "no weighing in period",
			"drawer.feed_band.not_weighed":              "No weighing in period",
			"column.feed_band.park":                     "Park",
			"column.feed_band_unmatched.park":           "Park",
			"column.feed_band_unmatched.pen":            "Pen",
			"column.feed_band_unmatched.shed_tag":       "Pen tag",
			"column.feed_band_unmatched.ration":         "Ration / arm",
			"column.feed_band_unmatched.breed":          "Breed",
			"column.feed_band_unmatched.feed_type":      "Feed type",
			"column.feed_band_unmatched.feed_given":     "Feed given",
			"column.feed_band_unmatched.pen_kg_per_day": "Pen kg/day",
			"column.feed_band_exits.park":               "Park",
			"column.feed_band_exits.tag":                "Tag",
			"column.feed_band_exits.pen":                "Last pen",
			"column.feed_band_exits.gender":             "Gender",
			"column.feed_band_exits.reason":             "Reason",
			"column.feed_band_exits.exited_at":          "Exit date",
			"column.feed_band_exits.last_weighed":       "Last weighed",
			"column.feed_band_exits.last_band":          "Last band",
			"column.feed_band_exits.last_kg":            "Last kg",
			"column.feed_band_exits.feed_type":          "Feed type",
			"column.feed_band_exits.feed_given":         "Pen feed today",
			// Above the table (maintainer request 2026-09-18): the sheet day as a chip and the
			// count of fed pens not shown. The full sheet-to-rows chain is in the design doc
			// (docs/weighing/feed-direction-by-weight-band.md) and on the wire, not on screen.
			"recon.feed_band.sheet":    "Feed plan",
			"recon.feed_band.sheets":   "Feed plans",
			"recon.feed_band.no_sheet": "No locked feed plan on record.",
			"pager.feed_band.noun":     "row",
			// Column labels, read by feedWeightBandTable.
			"column.feed_band.weight_source":  "Weight source",
			"column.feed_band.band":           "Band",
			"column.feed_band.pen":            "Pen",
			"column.feed_band.group":          "Group",
			"column.feed_band.gender":         "Gender",
			"column.feed_band.breed":          "Breed",
			"column.feed_band.feed_type":      "Feed type",
			"column.feed_band.feed_given":     "Feed given",
			"column.feed_band.pen_kg_per_day": "Pen kg/day",
			"column.feed_band.weight_animals": "Wt n",
			"column.feed_band.average_weight": "Avg kg",

			// Time-wise. It follows the same selected window as the other tabs so every tab slices
			// one population. The default window begins at the first dense weighing run.
			"section.time.title":   "Weekly growth",
			"section.time.caption": "Daily gain by week inside the selected period under the selected weighing mode. The default period starts on 03/08/2026, where the reliable weighing run begins. Park, sex and origin filters still apply.",
			"section.time.aria":    "Daily gain by week",
			"empty.time.body":      "No week in this period has a kid or a pen weighed twice.",
			// The chip beside every bar on these charts counts ANIMALS, not kids. Nothing in
			// weight_demographics.go filters by age: management_stage is a reporting dimension
			// there, never a predicate, so a scanned weigh counts whatever the tag resolves to and
			// a whole-pen weigh counts every resident it holds. On 2026-09-22 that was F2 animals
			// up to 519 days old on the scanned side, and 59 Non-Pregnant females up to 855 days
			// plus a Buck on the whole-pen side -- all labelled "kids". The number is honest and
			// free-flow weighing cannot ask an animal its age; the word was the defect.
			"value.time.animals":         "animals",
			"section.time.breed.title":   "Weekly growth by breed",
			"section.time.breed.caption": "The same selected period and weighing mode, one row per breed. A pen holding more than one breed is counted in the overall trend above but in no breed here, so the breed rows need not add up to it.",
			"section.time.breed.aria":    "Daily gain by breed and week",
			"empty.time.breed.body":      "No breed has a kid or a single-breed pen weighed twice in this period.",
			"note.time.gaps":             "A week nobody weighed in has no bar. It is left out rather than drawn as zero, which would read as a week the kids stopped growing.",
			// Time-wise, per pen and per load (maintainer requests 2026-09-08 and 2026-09-14): one
			// row per pen or purchased load, one column per calendar week, daily gain in each
			// cell. A blank cell is a week that pen or load was not weighed twice -- absence,
			// never a zero. The load rows are the pen rows one grain up: a pen counts toward a
			// load only when it is tagged to exactly that one load, the same attribution the
			// Load-wise tab uses.
			"section.time.pen.title":    "Weekly growth by pen",
			"section.time.pen.caption":  "Daily gain for every pen in every week of the selected period, under the selected weighing mode. A pen is listed whatever it holds; the sex filter keeps a whole-pen weigh only when the pen holds that sex alone.",
			"section.time.pen.aria":     "Daily gain by pen and week",
			"empty.time.pen.body":       "No pen has a kid or a whole-pen weigh paired inside this period.",
			"value.time.pen.blank":      "—",
			"value.time.pen.unit":       "g/day",
			"section.time.load.title":   "Weekly growth by load",
			"section.time.load.caption": "The same weeks, one row per purchased load: the daily gain of the pens that load sits in, weighted by animals. A pen tagged to two loads counts toward neither, and a load whose pens were not weighed twice in a week shows a blank for it.",
			"section.time.load.aria":    "Daily gain by purchased load and week",
			"empty.time.load.body":      "No purchased load has a pen weighed twice inside this period.",

			// The MONTHLY wording of the same four sections. A column is then a rolling 30-day
			// block counted back from the period's last day, so a heading that said "week" would
			// name something the table no longer shows. The page picks one set or the other; it
			// never edits either, and the two must stay a matched pair.
			"section.time.title.month":         "Growth every thirty days",
			"section.time.caption.month":       "Daily gain in each thirty-day block of the selected period under the selected weighing mode, counted back from the period's last day. The default period starts on 03/08/2026, where the reliable weighing run begins. Park, sex and origin filters still apply. A block the period starts part-way through is shorter than thirty days and is still headed by that block's own start date, which can fall before the period.",
			"section.time.aria.month":          "Daily gain every thirty days",
			"empty.time.body.month":            "No thirty-day block in this period has a kid or a pen weighed twice.",
			"section.time.breed.title.month":   "Growth every thirty days by breed",
			"section.time.breed.caption.month": "The same thirty-day blocks and weighing mode, one row per breed. A pen holding more than one breed is counted in the overall trend above but in no breed here, so the breed rows need not add up to it. A block the period starts part-way through is shorter than thirty days and is still headed by that block's own start date, which can fall before the period.",
			"section.time.breed.aria.month":    "Daily gain by breed and thirty-day block",
			"empty.time.breed.body.month":      "No breed has a kid or a single-breed pen weighed twice in this period.",
			"note.time.gaps.month":             "A thirty-day block nobody weighed in has no bar. It is left out rather than drawn as zero, which would read as a block the kids stopped growing in.",
			"section.time.pen.title.month":     "Growth every thirty days by pen",
			"section.time.pen.caption.month":   "Daily gain for every pen in each thirty-day block of the selected period, under the selected weighing mode. A pen is listed whatever it holds; the sex filter keeps a whole-pen weigh only when the pen holds that sex alone. A block the period starts part-way through is shorter than thirty days and is still headed by that block's own start date, which can fall before the period.",
			"section.time.pen.aria.month":      "Daily gain by pen and thirty-day block",
			"empty.time.pen.body.month":        "No pen has a kid or a whole-pen weigh paired inside this period.",
			"section.time.load.title.month":    "Growth every thirty days by load",
			"section.time.load.caption.month":  "The same thirty-day blocks, one row per purchased load: the daily gain of the pens that load sits in, weighted by animals. A pen tagged to two loads counts toward neither, and a load whose pens were not weighed twice in a block shows a blank for it. A block the period starts part-way through is shorter than thirty days and is still headed by that block's own start date, which can fall before the period.",
			"section.time.load.aria.month":     "Daily gain by purchased load and thirty-day block",
			"empty.time.load.body.month":       "No purchased load has a pen weighed twice inside this period.",

			// ---------------------------------------------------------------------------
			// LOAD-WISE (maintainer request 2026-09-03). One chart and one table: per
			// purchased load, the average weight an animal arrived at against its latest
			// weighing, and the growth multiple between the two (1.5x, 2x). Absence copy
			// is load-bearing: a load with no tagged shed has no latest weight, and a
			// load with no recorded purchase weight has no multiple — neither is ever
			// shown as zero.
			// ---------------------------------------------------------------------------
			"tab.load": "Comparison",
			// The FCR tab's strip label. Its content copy is authored with the tab itself; the
			// strip reads every tab's label on every route, so this one key is what lets the
			// other seven tabs render at all.
			"tab.fcr": "FCR",

			"section.load.title":   "Purchased weight against the latest weighing",
			"section.load.caption": "Average weight per animal in each purchased load — as bought, and at its latest weighing. The figure above each pair is how many times the arrival weight the load now stands at, followed by the pens the load's weighed animals sit in and the head count at each pen's latest weigh.",
			"section.load.aria":    "Purchased weight against latest weighing by load",

			"legend.load.purchased": "At purchase",
			"legend.load.latest":    "Latest weighing",

			// ONLY LOADS WITH A LATEST WEIGHING (maintainer request 2026-09-03): the tab is a
			// comparison, and a load nobody has weighed since it arrived has nothing to compare.
			// Said on the tab so a missing load reads as unweighed, never as lost.
			"note.load.weighed_only": "Only loads with a latest weighing are shown. A load whose pens have not been weighed since it arrived is left out.",

			// VALUE (maintainer request 2026-09-03): what the load cost against what the animals
			// still on farm are worth today at an assumed live-weight rate, and the difference.
			// The rates are the maintainer's assumptions, stated on the chart; a sold-out load
			// has no stock to value and shows nothing here rather than a zero.
			"section.load_value.title":   "Purchased value against current stock value",
			"section.load_value.caption": "What each load cost, landed, against what its animals still on farm are worth today: remaining animals × latest average weight × each animal's assumed live-weight rate for its stage and sex, followed by the pens the load's weighed animals sit in. Gain is the difference. A load that has sold out has no stock to value, and names no pen.",
			"section.load_value.aria":    "Purchased value against current stock value by load",
			"legend.load.purchase_value": "Purchased value (₹)",
			"legend.load.stock_value":    "Current stock value (₹)",
			"legend.load.gain":           "Gain / difference (₹)",
			// The assumed sale price now comes from growth_sale_price_assumptions (maintainer decision
			// 2026-09-07), read by the Comparison tab and the FCR tab alike, so the hard-coded
			// 430/450 copy is gone. The note is composed from the served prices.
			"note.load.rates.prefix":  "Assumed live-weight rates:",
			"note.load.rates.missing": "No assumed live-weight sale price is configured, so stock cannot be valued.",
			// Since 2026-09-24 a price may be set per stage and sex; the note names the all-stages
			// defaults and says how many stage prices sit on top of them.
			"note.load.rates.overrides": "set by stage and sex",
			"load.value.sold_out":       "sold out",
			"load.value.no_cost":        "cost not recorded",
			"disabled.load_value":       "Your current role can view weights but not purchase and sales money.",
			"unit.rupees":               "₹",

			// ---------------------------------------------------------------------------
			// FCR (maintainer request 2026-09-07). Feed conversion ratio: kilograms of feed
			// directed to a pen per kilogram the pen gained, over the same days. Lower is
			// better. Every figure is backend-owned; absence copy is load-bearing because a pen
			// weighed once, a pen with no feed rows and a pen that did not gain each have no
			// honest number and must not read as zero.
			// ---------------------------------------------------------------------------

			"kpi.fcr.farm.label":       "Farm FCR",
			"kpi.fcr.farm.unit":        "kg feed per kg gain",
			"kpi.fcr.cost_gain.label":  "Feed cost per kg gain",
			"kpi.fcr.feed_cost.label":  "Feed spent",
			"kpi.fcr.feed_cost.sub":    "on the pens with an FCR, at purchase prices",
			"kpi.fcr.gain_value.label": "Weight gained, at sale price",
			"kpi.fcr.gain_value.sub":   "kg gained × assumed ₹ per kg",
			"kpi.fcr.margin.label":     "Money made over feed",
			"kpi.fcr.margin.sub":       "gain value − feed spent",
			"kpi.fcr.margin.loss":      "the pens ate more value than they gained",
			"unit.fcr.rupees":          "₹",
			"kpi.fcr.break_even.label": "Break-even FCR",
			"kpi.fcr.break_even.sub":   "sale price ÷ feed cost per kg",
			"kpi.fcr.no_value":         "—",

			"fcr.price.prefix":  "Valued at",
			"fcr.price.per_kg":  "per kg live weight",
			"fcr.price.set":     "set",
			"fcr.price.by":      "by",
			"fcr.price.missing": "No assumed sale price is configured for this species, so its gain is not valued.",
			"fcr.price.shared":  "The Comparison tab values stock at the same prices.",
			// Maintainer decision 2026-09-24: prices may also be set per stage and sex.
			"fcr.price.overrides": "set by stage and sex; each animal is valued at its own",

			"section.fcr.pens.title":     "FCR by pen",
			"section.fcr.pens.caption":   "Kilograms of feed directed to the pen between its first and latest weighing in the period, per kilogram the pen gained. Grouped by park, pens A to Z; the dashed line is break-even at today's prices.",
			"section.fcr.pens.aria":      "Feed conversion ratio by pen",
			"section.fcr.money.title":    "Money by pen",
			"section.fcr.money.caption":  "What each pen's gain is worth at the assumed sale price, what its feed cost at purchase prices, and the money made between the two. A bar below the line is a pen that ate more value than it put on.",
			"section.fcr.money.aria":     "Gain value, feed cost and money made by pen",
			"legend.fcr.gain_value":      "Gain value (₹)",
			"legend.fcr.feed_cost":       "Feed cost (₹)",
			"legend.fcr.margin":          "Money made (₹)",
			"section.fcr.breed.title":    "FCR by breed",
			"section.fcr.breed.caption":  "Mixed pens are split by resident headcount share so every breed is visible.",
			"section.fcr.breed.aria":     "Feed conversion ratio by breed",
			"section.fcr.sex.title":      "FCR by sex",
			"section.fcr.sex.caption":    "A pen counts under a sex only when every resident is that sex.",
			"section.fcr.sex.aria":       "Feed conversion ratio by sex",
			"section.fcr.weekly.title":   "FCR week by week",
			"section.fcr.weekly.caption": "Each weighing round closes a segment for its pen: feed directed since the previous round over the gain across it. Weeks are grouped by the round that closed them.",
			"section.fcr.weekly.aria":    "Feed conversion ratio by week",
			"section.fcr.band.title":     "FCR by weight band",
			"section.fcr.band.caption":   "Band from the pen's average weight at its first weighing in the period.",
			"section.fcr.band.aria":      "Feed conversion ratio by weight band",
			"section.fcr.park.title":     "FCR by park",
			"section.fcr.park.aria":      "Feed conversion ratio by park",
			"section.fcr.origin.title":   "Farm born against purchased",
			"section.fcr.origin.caption": "Purchased means every live resident of the pen came off a load; a pen holding both is claimed by neither.",
			"section.fcr.origin.aria":    "Feed conversion ratio by origin",

			"series.fcr":           "FCR",
			"unit.fcr":             "kg/kg",
			"value.fcr.pens":       "pens",
			"value.fcr.kids":       "kids",
			"label.fcr.mixed":      "Mixed",
			"label.fcr.unknown":    "Not recorded",
			"label.fcr.farm_born":  "Farm born",
			"label.fcr.purchased":  "Purchased",
			"label.fcr.break_even": "Break-even",
			"label.fcr.whole_pen":  "whole pen",
			"label.fcr.scanned":    "scanned",

			"table.fcr.title":                 "Pens",
			"table.fcr.aria":                  "FCR by pen",
			"table.fcr.caption":               "One row per pen. Feed is what the sheet directed between the two weighing dates; gain is the pen's daily gain × the head-days actually fed. Blocked feed cells understate feed and are flagged.",
			"table.fcr.pen":                   "Pen",
			"table.fcr.cohort":                "Cohort",
			"table.fcr.animals":               "Kids",
			"table.fcr.weighed":               "Weighed",
			"table.fcr.daily_gain":            "ADG g/day",
			"table.fcr.head_days":             "Head-days",
			"table.fcr.gain_kg":               "Gain kg",
			"table.fcr.feed_kg":               "Feed kg",
			"table.fcr.fcr":                   "FCR",
			"table.fcr.feed_cost":             "Feed ₹",
			"table.fcr.gain_value":            "Gain value ₹",
			"table.fcr.margin":                "Money made ₹",
			"table.fcr.feed_cost_per_kg_gain": "₹ feed / kg gain",
			"table.fcr.feed_sheet":            "Feed sheet",
			"table.fcr.status.ok":             "complete",
			"table.fcr.status.blocked":        "blocked cells",
			"table.fcr.status.weighed_once":   "weighed once",
			"table.fcr.status.no_feed":        "no feed rows",
			"table.fcr.status.no_gain":        "no gain",
			"table.fcr.unpriced":              "kg unpriced",

			"note.fcr.basis":    "Feed is the quantity the feed sheet directed, not a measured intake. Park-level feed that no sheet assigns to a pen is left out.",
			"note.fcr.filters":  "Under a sex or origin filter a pen counts only when every resident matches: feed is given to the whole pen and cannot be split.",
			"note.fcr.excluded": "Pens weighed once, pens with no feed sheet rows and pens that did not gain are listed in the table without a ratio and stay out of every chart.",
			"empty.fcr.body":    "No pen has two weighings and feed sheet rows in this period yet. FCR appears once a pen has been weighed twice with feed directed in between.",
			"error.fcr.body":    "The feed conversion figures could not be loaded. Try again.",

			// The growth multiple. Composed as value + suffix ("1.8" + "x"); the dash is
			// what renders when either side of the division is missing.
			"load.multiple.suffix": "x",
			"load.no_figure":       "—",
			"unit.kg_per_head":     "kg/animal",

			"table.loads.title":         "Loads",
			"table.loads.load":          "Load",
			"table.loads.vendor":        "Source",
			"table.loads.animals":       "Animals bought",
			"table.loads.purchased_avg": "At purchase",
			"table.loads.latest_avg":    "Latest weighing",
			"table.loads.multiple":      "Growth multiple",
			"table.loads.pens":          "Pens",

			// Three honesty notes. The denominators differ once part of a load is sold; the
			// page's other filters cannot slice a load that is bought whole; and the latest
			// weighing deliberately ignores the selected period, or a narrow window would
			// erase a load's newest weigh and read as the animals shrinking.
			"note.load.denominator": "At purchase averages over every animal bought; the latest weighing averages over the animals weighed in the load's pens — sold or lost animals are no longer in it.",
			"note.load.filters":     "This tab narrows by park only. A load is bought whole, so the sex, origin, weighing-mode and period filters do not apply here; the latest weighing is each pen's newest weigh on record.",

			"empty.load.body": "No purchased loads recorded yet. When one is, its arrival weight and latest weighing will be compared here.",
			// Loads EXIST but none has a latest weighing: a different fact from "no loads", and
			// saying the latter to a farm with purchased animals in its pens would be false.
			"empty.load.unweighed.body": "No purchased load has been weighed yet. A load appears here once the pens it went into have a weighing on record.",

			// The weighing side failed but the purchase ledger answered (or the other way
			// round): name the half that is missing instead of blanking the tab.
			"error.load.weighing_unavailable": "The latest weighing figures could not be loaded. The purchase-side figures below are complete.",
			"error.load.loads_unavailable":    "The purchase ledger could not be loaded, so there is nothing to compare yet. Try again.",
		} {
			analytics[key] = value
		}
		return analytics
	// -------------------------------------------------------------------------------
	// COUNTS -> HERD ANALYTICS. Two questions on one screen, and the copy has to keep
	// them apart because they have different time grains:
	//
	//   COMPOSITION — what the herd IS, RIGHT NOW. Breed, pen tag, sex, kid/adult. The
	//   same live population Counts Breakdown reports, so a reader can move between the
	//   two screens without the denominator changing under them. It is NOT a window
	//   figure and every caption says so.
	//
	//   FLOW — what CHANGED the herd, month by month. Births in, deaths and sales out,
	//   pen movements within. Each figure is counted off the canonical row that recorded
	//   the event, dated the day the farm did the thing: a birth on the kid's own date, an
	//   exit on the exit date, a movement on the day the operator COMPLETED it rather than
	//   the day it was raised or approved.
	//
	// NET CHANGE subtracts every exit, including culled/transferred/lost, which is why
	// "Other exits" is a visible column rather than a silent remainder — a net that did not
	// reconcile with the columns beside it would read as a bug in the arithmetic.
	// -------------------------------------------------------------------------------
	case "herd-analytics":
		return map[string]string{
			"crumb":        "Counts",
			"banner.basis": "Composition is the live herd as it stands today. Births and exits are counted on the day the farm recorded them, across the window you choose.",

			// The window filter is the SHARED calendar (components/date-range-picker.tsx),
			// the same control the Verify board and the video log use — one calendar across
			// the product rather than a third one that drifts. Its label set is therefore the
			// same "filter.date.*" key shape those screens use.
			//
			// Park scope is deliberately NOT offered here: it lives in the top bar (Scope
			// Chrome Rule) and the filter only carries it forward, so the hint says where to
			// change it rather than leaving a reader hunting for a control that is not there.
			"filter.date":                  "Window",
			"filter.date.today":            "Today",
			"filter.date.single":           "Single day",
			"filter.date.range":            "Date range",
			"filter.date.aria":             "Choose the dates of herd movement to show",
			"filter.date.previous_month":   "Previous month",
			"filter.date.next_month":       "Next month",
			"filter.date.range_start_hint": "Pick the first day of the range.",
			"filter.date.range_end_hint":   "Now pick the last day of the range.",
			"filter.date.range_separator":  "to",
			"filter.scope_readonly":        "Park scope is set in the top bar.",

			"kpi.live.label":   "Animals in the herd",
			"kpi.live.sub":     "Live animals today, across the selected scope",
			"kpi.age.label":    "Kids · Adults",
			"kpi.age.sub":      "Every live animal falls in exactly one of the two",
			"kpi.births.label": "Births",
			"kpi.births.sub":   "Kids born in the window",
			"kpi.deaths.label": "Deaths",
			"kpi.deaths.sub":   "Animals recorded dead in the window",
			"kpi.sold.label":   "Sold",
			"kpi.sold.sub":     "Animals sold in the window",
			"kpi.net.label":    "Net herd change",
			"kpi.net.sub":      "Births minus every exit — deaths, sales, culled, transferred and lost",

			"section.mix.aria": "Herd composition charts",

			"chart.flow.title":      "Births and exits by month",
			"chart.flow.hint":       "One point per India calendar month. A birth counts on the kid's own date; an exit counts on the day the animal left the herd. A window that starts or ends mid-month leaves that month's point covering only the days inside it.",
			"chart.movements.title": "Animals moved between pens",
			"chart.movements.hint":  "Head count carried by shifts the operator completed that month — a shift raised or approved but not yet walked is not counted here.",
			"chart.breed.title":     "Breed mix",
			"chart.breed.hint":      "Live animals by breed, right now",
			"chart.stage.title":     "Pen tag mix",
			"chart.stage.hint":      "Live animals by the tag their pen carries, shown exactly as recorded — near-duplicate tags stay separate so a source-data gap remains visible",
			"chart.age.title":       "Kids and adults",
			"chart.age.hint":        "Age band follows the pen tag; an animal with no recorded age counts as an adult",
			"chart.sex.title":       "Male and female",
			"chart.sex.hint":        "Live animals by sex, right now",
			"chart.park.title":      "Animals by farm",
			"chart.park.hint":       "Where the live herd sits today",
			"chart.empty":           "Nothing recorded in this scope yet.",
			"chart.legend_aria":     "Chart series legend",
			"chart.value_aria":      "animals",

			"series.births":      "Births",
			"series.deaths":      "Deaths",
			"series.sold":        "Sold",
			"series.other_exits": "Other exits",
			"series.kids":        "Kids",
			"series.adults":      "Adults",

			"label.animals_noun":     "animals",
			"label.kids":             "kids",
			"label.adults":           "adults",
			"label.unassigned_breed": "No breed",
			"label.unassigned_stage": "No tag",
			"label.unassigned_sex":   "Not recorded",
			"label.unassigned_park":  "No farm",

			"state.unavailable": "Herd analytics unavailable",
			"section.kpi.aria":  "Herd headline figures",
			"empty.title":       "Nothing recorded yet",
			"empty.body":        "No live animals, and no births or exits in this scope and window. Figures appear as soon as the herd is registered and the field work is recorded.",
			"error.title":       "Herd analytics is unavailable",
			"error.body":        "The herd read failed. The Counts screens themselves are unaffected; try again shortly.",
		}
	// -------------------------------------------------------------------------------
	// COUNTS -> MORTALITY. One question -- which animals died and what did they have in
	// common -- asked from every angle the farm can ask it. Two kinds of series, and the
	// copy has to keep them apart:
	//
	//   RATE series (kids/adults, stage, breed, sex, farm, pen, load) divide the window's deaths
	//   by the animals in that same section TODAY -- the Counts Breakdown head count (maintainer
	//   decision 2026-09-18). A section with 3 deaths against 40 and one with 3 against 400
	//   read very differently.
	//
	//   COUNT series (age at death, season, cause, days since arrival, days since the last
	//   vaccination) are facts about the death alone and carry no rate; inventing a
	//   denominator for them would be an invented number.
	//
	// "No cause recorded" is a complete answer, not missing data: the operator answered
	// "normal death", or the death predates the cause field (2026-09-05). The banner and
	// the cause card both say so.
	// -------------------------------------------------------------------------------
	case "counts-mortality":
		return map[string]string{
			"crumb":        "Counts",
			"banner.basis": "Deaths are counted on the day the animal was recorded dead. Every rate divides the deaths in the window by the animals in that section today. Breed, sex, stage, pen, load and vendor are read as they stood when the animal died.",

			"filter.date":                  "Window",
			"filter.date.today":            "Today",
			"filter.date.single":           "Single day",
			"filter.date.range":            "Date range",
			"filter.date.aria":             "Choose the dates of deaths to show",
			"filter.date.previous_month":   "Previous month",
			"filter.date.next_month":       "Next month",
			"filter.date.range_start_hint": "Pick the first day of the range.",
			"filter.date.range_end_hint":   "Now pick the last day of the range.",
			"filter.date.range_separator":  "to",
			"filter.scope_readonly":        "Park scope is set in the top bar.",

			"section.kpi.aria":     "Mortality headline figures",
			"kpi.deaths.label":     "Deaths",
			"kpi.deaths.sub":       "Animals recorded dead in the window",
			"kpi.rate.label":       "Mortality rate",
			"kpi.rate.sub":         "Deaths in the window against the animals in the herd today",
			"kpi.kids.label":       "Kid deaths",
			"kpi.kids.sub":         "Kids (K-stage or kid age band) recorded dead, with their rate",
			"kpi.adults.label":     "Adult deaths",
			"kpi.adults.sub":       "Adults recorded dead, with their rate",
			"kpi.first_week.label": "Died within 7 days of birth",
			"kpi.first_week.sub":   "The highest-risk window on any goat farm",
			"kpi.cause.label":      "Cause established",
			"kpi.cause.sub":        "Deaths with a disease named on the death form or an open case at the time",
			"kpi.animals":          "animals",
			"kpi.no_rate":          "No live animals in this section",

			"chart.months.title":    "Deaths by month",
			"chart.months.hint":     "One point per India calendar month, kids and adults kept apart because their causes differ. A window that starts or ends mid-month leaves that month covering only the days inside it.",
			"chart.stage.title":     "By stage",
			"chart.stage.hint":      "Deaths and mortality rate by the tag the animal carried when it died — pregnant, non-pregnant, fattening, bucks and each kid stage",
			"chart.kid_adult.title": "Kids and adults",
			"chart.kid_adult.hint":  "Deaths and rate for each age band; every animal falls in exactly one",
			"chart.age.title":       "Age at death",
			"chart.age.hint":        "Days between date of birth and death. The first week is the neonatal window; 1–3 months is pre-weaning.",
			"chart.breed.title":     "By breed",
			"chart.breed.hint":      "Deaths and mortality rate by breed, so a breed-specific weakness shows against its own head count",
			"chart.species.title":   "Goats and sheep",
			"chart.species.hint":    "Deaths and rate by species",
			"chart.sex.title":       "Male and female",
			"chart.sex.hint":        "Deaths and rate by sex",
			"chart.park.title":      "By farm",
			"chart.park.hint":       "Deaths and rate at each farm",
			"chart.pen.title":       "By pen",
			"chart.pen.hint":        "Pens that saw a death, with the rate against the animals in that pen today — a single bad pen points at ventilation, drainage or crowding rather than a farm-wide problem",
			"chart.load.title":      "By purchase load",
			"chart.load.hint":       "Deaths and rate per load the animals came in on, beside farm-born animals — a bad batch shows here before it shows anywhere else",
			"chart.vendor.title":    "By vendor",
			"chart.vendor.hint":     "The same deaths grouped by who the animals were bought from, across every load that vendor sent — one bad load is bad luck, the same vendor twice is a supply problem",
			"chart.cause.title":     "By cause",
			"chart.cause.hint":      "The disease named on the death form. Older deaths are attributed from a case that was open when the animal died; a death with neither is counted as no cause recorded and never given a disease.",
			"chart.season.title":    "By season",
			"chart.season.hint":     "Summer heat, monsoon parasite load, the retreating monsoon and winter cold each drive a different pattern",
			"chart.arrival.title":   "Days on the farm before death",
			"chart.arrival.hint":    "Purchased animals only, from the day they arrived. A death within the first weeks points at transport stress or disease brought in with the load.",
			"chart.vaccine.title":   "Days since last vaccination",
			"chart.vaccine.hint":    "From the animal's most recent recorded or accepted vaccination to its death. Never vaccinated means no vaccination on record before the death.",
			"chart.empty":           "No deaths recorded in this scope and window.",
			"chart.legend_aria":     "Chart series legend",
			"chart.value_aria":      "deaths",
			"chart.rate_aria":       "mortality rate",

			"section.rates.aria":       "Mortality rate tables",
			"section.counts.aria":      "Death profile charts",
			"section.cross.aria":       "Cross tabs",
			"cross.season_stage.title": "Season × stage",
			"cross.season_stage.hint":  "Deaths in each season by the stage the animal carried — where kid deaths pile up in one season",
			"cross.load_cause.title":   "Load × cause",
			"cross.load_cause.hint":    "Which loads brought which diseases in",
			"cross.vendor_cause.title": "Vendor × cause",
			"cross.vendor_cause.hint":  "Which vendors send animals that die of what, across all their loads",
			"cross.breed_cause.title":  "Breed × cause",
			"cross.breed_cause.hint":   "Which breeds are prone to which diseases",
			"cross.total":              "Total",

			"series.deaths": "Deaths",
			"series.kids":   "Kids",
			"series.adults": "Adults",
			"series.rate":   "Rate",

			"label.deaths_noun":        "deaths",
			"label.share":              "Share",
			"label.unassigned_breed":   "No breed recorded",
			"label.unassigned_stage":   "No tag",
			"label.unassigned_sex":     "Not recorded",
			"label.unassigned_park":    "No farm",
			"label.unassigned_pen":     "No pen recorded",
			"label.unassigned_species": "Not recorded",
			"basis.recorded":           "Named on death form",
			"basis.inferred":           "From open case",
			"basis.none":               "No cause recorded",

			"table.recent.title": "Deaths in this window",
			"table.recent.hint":  "Every animal recorded dead in this window, most recent first. The figures above count the whole window, so they do not change as this list is paged.",
			"table.recent.noun":  "death",
			"label.days_suffix":  "days",
			"label.no_tag":       "No tag",
			"label.no_load":      "—",
			"label.no_vendor":    "—",

			"pager.page":      "Page",
			"pager.of":        "of",
			"pager.rows":      "Rows",
			"action.previous": "Previous",
			"action.next":     "Next",

			"state.unavailable": "Mortality unavailable",
			"empty.title":       "No deaths recorded",
			"empty.body":        "No animal was recorded dead in this scope and window. Figures appear as soon as a death is recorded and approved.",
			"error.title":       "Mortality is unavailable",
			"error.body":        "The mortality read failed. The Counts screens themselves are unaffected; try again shortly.",
		}
	// -------------------------------------------------------------------------------
	// HEALTH -> HEALTH ANALYTICS. Three questions, and the copy has to be honest that
	// the third one has a hole in it.
	//
	//   SICKNESS — what the herd is being treated for. Counted on the diagnosis RULE and
	//   at CASE grain: one animal treated twice for the same illness is two cases.
	//
	//   EXECUTION — whether the prescribed course is actually being carried out, at
	//   SESSION grain, with late kept apart from never-done.
	//
	//   MORTALITY — recorded causes where the death form captured one, with the
	//   old open-case inference retained for deaths recorded before that field
	//   existed. A death with neither is reported as unattributed and never given
	//   a disease. The banner says this in the operator's own language, because a
	//   reader who does not know it would read the unattributed column as missing
	//   data rather than as the detection gap it actually measures.
	// -------------------------------------------------------------------------------
	case "health-analytics":
		return map[string]string{
			"crumb": "Health",
			"banner.basis": "Deaths recorded now use the disease named on the death form. " +
				"Older deaths are attributed only when the animal had an open case at the time; every other death is counted as not attributed and is never given a disease.",

			// The window filter is the SHARED calendar every other filtered screen uses,
			// so the label set is the same "filter.date.*" shape. Park scope lives in the
			// top bar (Scope Chrome Rule) and the hint says so rather than leaving a
			// reader hunting for a control that is not on the page.
			"filter.date":                  "Window",
			"filter.date.today":            "Today",
			"filter.date.single":           "Single day",
			"filter.date.range":            "Date range",
			"filter.date.aria":             "Choose the dates of health work to show",
			"filter.date.previous_month":   "Previous month",
			"filter.date.next_month":       "Next month",
			"filter.date.range_start_hint": "Pick the first day of the range.",
			"filter.date.range_end_hint":   "Now pick the last day of the range.",
			"filter.date.range_separator":  "to",
			"filter.scope_readonly":        "Park scope is set in the top bar.",

			"tab.overview":   "Overview",
			"tab.problems":   "Health problems",
			"tab.diseases":   "Diseases",
			"tab.mortality":  "Mortality",
			"tab.treatment":  "Treatment",
			"tab.engine":     "Diagnosis engine",
			"tab.group.aria": "Health analytics sections",

			// WINDOW PRESETS. The four the farm reads by (maintainer instruction 2026-09-22).
			// "Since the reforms" opens on the first day the Health module recorded anything,
			// so it is genuinely all-time for this data rather than an arbitrary long window
			// padded with empty months.
			"filter.window.aria":        "How far back to count health problems",
			"filter.window.30":          "Last 30 days",
			"filter.window.90":          "Last 90 days",
			"filter.window.120":         "Last 120 days",
			"filter.window.all":         "Since the reforms",
			"filter.window.custom":      "Custom",
			"filter.window.custom_hint": "Pick any two dates in the calendar beside these.",

			"problems.note": "One problem is one episode of one illness in one animal. An animal treated twice is " +
				"counted twice, because a relapse is a problem the farm had twice. Every chart below cuts this same " +
				"total, so each one adds back up to it.",
			"problems.chart.month.title": "Health problems by month",
			"problems.chart.month.hint": "One column per India calendar month, so a rise or fall is read against whole " +
				"months. A window starting or ending mid-month leaves that column covering only the days inside it.",
			"problems.chart.breed.title": "Health problems by breed",
			"problems.chart.breed.hint": "Which breeds the farm is treating most. Read it against how many of each breed " +
				"the farm keeps: a breed with twice the animals will show more problems without being less healthy.",
			"problems.chart.pen_type.title": "Health problems by pen type",
			"problems.chart.pen_type.hint": "Elevated pens against non-elevated ones. A pen's type is set on its own row " +
				"in Configuration, Items and settings, Pens; problems in a pen nobody has typed are shown separately " +
				"rather than counted into either side.",
			"problems.chart.age.title": "Health problems by age",
			"problems.chart.age.hint": "How old each animal was when the case was opened, not how old it is today. " +
				"Animals with no date of birth on record are shown in their own band.",
			"problems.capped_breeds": "Busiest 15 breeds",
			// The shared bar chart draws no bar for a zero, which on a two-sided comparison
			// would leave one bar looking like the only kind of pen the farm has. Naming the
			// empty buckets keeps "nothing happened here" apart from "this was not measured".
			"problems.none_recorded": "Nothing recorded for",

			"kpi.open.label":         "Open cases now",
			"kpi.open.sub":           "Being treated today, not a window figure",
			"kpi.new.label":          "New cases",
			"kpi.new.sub":            "Courses started in the window",
			"kpi.recovery.label":     "Recovery rate",
			"kpi.recovery.sub":       "Of the cases closed in the window",
			"kpi.deaths.label":       "Deaths",
			"kpi.deaths.sub":         "Animals recorded dead in the window",
			"kpi.unattributed.label": "Not attributed",
			"kpi.unattributed.sub":   "No recorded cause or open-case link",

			"chart.deaths.title": "Deaths by month",
			"chart.deaths.hint": "One column per India calendar month. Every death falls in exactly one of the two, " +
				"so the pair always adds to that month's total. A window that starts or ends mid-month leaves that month covering only the days inside it.",
			"chart.diseases.title": "Cases by disease",
			"chart.diseases.hint": "Courses started in the window, counted on the diagnosis rule. One animal treated twice " +
				"for the same illness counts twice, because counting animals would hide a relapse.",
			"chart.trend.title":     "New cases by month",
			"chart.trend.hint":      "The busiest diseases, one chart each. Thirty-four rules on one axis is unreadable.",
			"chart.fatality.title":  "Deaths among treated animals",
			"chart.fatality.hint":   "Of the animals diagnosed with this disease in the window, the share that died. Only ever read per disease.",
			"chart.adherence.title": "Treatment sessions",
			"chart.adherence.hint": "One bar per outcome, over every session due in the window up to today. " +
				"Done late is kept apart from never done: they are different failures.",
			"chart.empty":       "Nothing recorded in this scope yet.",
			"chart.legend_aria": "Chart series legend",

			"series.attributed":   "Attributed",
			"series.unattributed": "Not attributed",
			"series.on_time":      "Done on the day",
			"series.late":         "Done late",
			"series.rework":       "Sent back",
			"series.not_done":     "Not done",

			"section.kpi.aria":       "Health headline figures",
			"section.diseases.title": "Disease board",
			"section.diseases.note": "One row per diagnosis rule, over the cases started in this window. " +
				"Open counts only the cases from this window that are still running.",
			"section.deaths.title":    "Deaths",
			"section.deaths.note":     "The most recent deaths in the window. The counts above cover the whole window and do not change with this list.",
			"section.medicines.title": "Medicines given",
			"section.medicines.note":  "Doses the operator actually recorded, never what the protocol prescribed.",
			"section.engine.title":    "Observation outcomes",
			"section.engine.note": "What the director did with the engine's proposal. Re-observations are left out of the rate — " +
				"a second look is not a disagreement.",
			"section.engine_rules.title": "Rules the engine proposed",
			"section.engine_rules.note": "Proposed against opened. There is no per-rule decline recorded anywhere, so this shows " +
				"what was proposed and what was actually taken up.",

			"stat.observations.label": "Observations",
			"stat.observations.sub":   "Sick-animal forms the engine judged in the window",
			"stat.confirmed.label":    "Confirmed",
			"stat.declined.label":     "Declined",
			"stat.pending.label":      "Not yet decided",
			"stat.superseded.label":   "Superseded",
			"stat.median.label":       "Median time to confirm",
			"stat.median.unit":        "h",
			"stat.awaiting.label":     "Awaiting the verifier",
			"stat.awaiting.sub":       "Already done, video not yet reviewed",
			"stat.sessions.label":     "Sessions due",
			"stat.sessions.sub":       "Treatment visits owed in the window, up to today",
			"stat.attributed.sub":     "Recorded cause or open-case link",
			"stat.never.label":        "Never had a case",
			"stat.never.sub":          "Of the deaths not attributed, animals with no case on record at all",

			"label.attributed":   "Attributed",
			"label.unattributed": "Not attributed",
			"label.never":        "No case on record",
			"label.no_disease":   "Not recorded",
			// A death recorded before causes existed carries only the case that happened to
			// be open at the time. The copy has to say that plainly: it is co-incidence, and
			// a reader who takes it for a recorded cause is reading a claim the farm never
			// made.
			"label.inferred":       "Open case at the time, cause not recorded",
			"label.no_pen":         "Pen not recorded",
			"label.no_tag":         "No tag recorded",
			"label.age.adult":      "Adult",
			"label.age.kid":        "Kid",
			"label.age.both":       "Both",
			"label.age.unknown":    "Not recorded",
			"label.days":           "days",
			"label.cases_noun":     "cases",
			"label.of_which":       "of which",
			"label.capped_list":    "Latest 50",
			"label.capped_board":   "Busiest 25 rules",
			"label.rule_kind.card": "Treatment card",

			"state.unavailable": "Health analytics unavailable",
			"empty.title":       "Nothing recorded yet",
			"empty.body":        "No cases, treatments or deaths in this scope and window. Figures appear as soon as the field work is recorded.",
			"empty.deaths":      "No deaths recorded in this window.",
			"empty.medicines":   "No medicine was recorded as given in this window.",
			"empty.engine":      "No observations were recorded in this window.",
			"error.title":       "Health analytics is unavailable",
			"error.body":        "The health read failed. Treatment work on the phone is unaffected; try again shortly.",
		}
	case "counts-breakdown":
		return map[string]string{
			"crumb":                     "Counts",
			"section.breakdown.title":   "Counts Breakdown",
			"section.breakdown.aria":    "Counts breakdown",
			"section.breakdown.caption": "One line per pen with its breed, gender and stage mix. Open a pen for the exact split.",
			// The pen line and its drill-down. Every string the pens table renders is here.
			"table.pens.aria":          "Head count by pen",
			"table.pens.noun":          "pen",
			"table.pens.total_row":     "Total · every matching pen, not just this page",
			"action.expand.hint":       "Click a pen to see its exact stage × breed × gender split",
			"action.expand_all":        "Open all",
			"action.collapse_all":      "Close all",
			"action.expand.open_aria":  "Open pen",
			"action.expand.close_aria": "Close pen",
			"detail.title":             "Exact split",
			"detail.aria":              "Exact stage, breed and gender split for this pen",
			"detail.combinations_one":  "combination",
			"detail.combinations_many": "combinations",
			"label.kid_short":          "kids",
			"label.adult_short":        "adults",
			// Short forms for the Female · Male column. Lower case and unabbreviated: the column
			// is narrow but "F"/"M" beside a bare number reads as a code, and the farm says
			// female and male.
			"label.female_short":                "female",
			"label.male_short":                  "male",
			"label.sex_other_short":             "not recorded",
			"label.mixed_stages":                "Mixed",
			"section.breakdown.note":            "Counts live animals only (lifecycle status alive), matching Herd Register. Stage is the raw source value recorded against each animal — near-duplicate labels are shown exactly as stored rather than merged, so source data issues stay visible.",
			"section.charts.title":              "Distribution",
			"section.charts.aria":               "Count distribution charts",
			"kpi.matching.label":                "Matching count",
			"kpi.matching.sub":                  "Live animals matching the current filters",
			"kpi.matching.unavailable":          "Count unavailable",
			"kpi.age.label":                     "Kids · Adults",
			"kpi.age.aria":                      "Kid and adult split",
			"label.kids":                        "kids",
			"label.adults":                      "adults",
			"summary_card.total_animals.label":  "Total animals",
			"summary_card.total_animals.kids":   "kids",
			"summary_card.total_animals.adults": "adults",
			"summary_card.fattening.label":      "Fattening",
			"summary_card.bucks.label":          "Bucks",
			"summary_card.breeding.label":       "Breeding stock",
			"summary_card.icu.label":            "ICU",
			"summary_card.k0.label":             "K0",
			"summary_card.k1.label":             "K1",
			"summary_card.k2.label":             "K2",
			"summary_card.k3.label":             "K3",
			"summary_card.k4.label":             "K4",
			"summary_card.group.aria":           "Stage summary cards",
			"table.breakdown.aria":              "Detail breakdown rows",
			// Says WHAT it totals (maintainer report, 2026-08-12). The value is the whole-filter sum —
			// 1,670 live animals across all 213 grain rows — sitting under a page of 10 rows that add
			// up to 463, so "Total (rows)" read as a number that did not match the table above it. The
			// value is right and must stay whole-filter (recomputing it from the page is the banned
			// capped read-time rollup); it was the LABEL that never said the page is not the whole set.
			"table.breakdown.total_row": "Total · every matching row, not just this page",
			"table.breakdown.noun":      "row",
			// The inline retag editor on the Stage cell. Every visible string it renders is here:
			// the frontend composes none of it, including the default reason that lands in the
			// audit row.
			//
			// SCOPE, because the row and the write are not the same thing: a breakdown row is a
			// census SLICE (one breed and sex within a pen) while the write moves the whole PEN, so
			// the confirm step reports the pen's own animal total from the preview rather than the
			// row's count.
			"action.retag.hint":               "Double-click a tag to change it",
			"action.retag.search_placeholder": "Type to find a tag",
			"action.retag.no_matches":         "No tag matches that",
			"action.retag.reason_label":       "Reason",
			"action.retag.default_reason":     "Tag corrected from Counts Breakdown",
			"action.retag.apply":              "Apply",
			"action.retag.cancel":             "Cancel",
			"action.retag.applying":           "Applying…",
			"action.retag.checking":           "Checking…",
			"action.retag.animals_noun":       "animals",
			"action.retag.animal_noun":        "animal",
			"action.retag.empty_scope":        "No animals here yet — this records the tag only",
			"action.retag.failed":             "That change could not be applied",
			// The kid/adult consequence. A tag carries its own band, so retagging a pen moves every
			// animal in it across that line; the picker and the confirm step say so rather than
			// leaving an operator to find out from the census afterwards.
			"action.retag.band_kid":   "kids",
			"action.retag.band_adult": "adults",
			"action.retag.becomes":    "These animals become",
			"filter.bar_aria":         "Filter breakdown rows",
			"filter.farm_label":       "Farm",
			"filter.stage_label":      "Stage",
			"filter.breed_label":      "Breed",
			"filter.shed_label":       "Pen",
			"filter.gender_label":     "Gender",
			"filter.all_option":       "All",
			"filter.clear_all":        "Clear all",
			"filter.apply":            "Apply filters",
			// Suffix rendered after the selection count on a multi-select filter button ("3 selected").
			"filter.selected_count": "selected",
			"filter.scope_readonly": "Park scope is set in the top bar.",
			"chart.breed.title":     "Count by breed",
			"chart.breed.caption":   "animals by breed",
			"chart.stage.title":     "Count by stage",
			"chart.stage.caption":   "where they are",
			// The stage x sex chart. The caption says the bar is the stage and the split is the
			// sex, because a stacked bar is the one chart shape a reader can misread as two
			// separate stages sitting end to end.
			"chart.stage_sex.title":   "Count by stage and sex",
			"chart.stage_sex.caption": "each bar is one stage, split female and male",
			"label.sex_female":        "Female",
			"label.sex_male":          "Male",
			// The third segment. Named for what it IS -- a sex the register does not hold as
			// female or male -- rather than "unknown", which reads as a system failure when the
			// truthful answer is that nobody recorded it.
			"label.sex_other":      "Not recorded",
			"chart.gender.title":   "Gender split",
			"chart.gender.caption": "animals by sex",
			"chart.shed.title":     "Pen occupancy",
			// PENS, not sheds (maintainer decision 2026-08-12): each bar is one pen, named with its
			// park because 66 of 154 shed names exist in both. The caption has to say so — a reader
			// counting twelve bars against a 44-shed estate would otherwise draw the wrong conclusion
			// about what the cap is hiding.
			// "every pen", not "top pens": the series carries no top-N cap and sums to the same
			// total the KPI above reports. Saying "top" while showing all of them would understate
			// the chart; saying it while showing 12 of 130 was what made the numbers look wrong.
			"chart.shed.caption":          "every pen by head count, park first",
			"chart.legend_aria":           "Chart series legend",
			"chart.empty":                 "No animals match these filters.",
			"chart.value_aria":            "animals",
			"label.animals_noun":          "animals",
			"label.unassigned_farm":       "No farm",
			"label.unassigned_shed":       "No pen",
			"label.unassigned_stage":      "No stage",
			"label.unassigned_breed":      "No breed",
			"empty.breakdown":             "No animals registered in this scope yet.",
			"empty.breakdown_filtered":    "No animals match these filters.",
			"state.breakdown_unavailable": "Breakdown unavailable",
			"state.stage_unrecorded":      "No stage is recorded against any animal in this scope, so every row groups under a single blank stage. This is a source-data gap, not a display error — stage is imported from the source sheet and has not been populated for this herd.",

			// PURCHASED LOADS (maintainer request 2026-09-18): the last card on the page. One
			// column group per load — what the load brought in, what is still on the farm under the
			// current filters, the male/female split — and, under the chart, the current RFID/tag
			// identifiers those animals carry NOW. The tag at purchase is deliberately not shown; the
			// maintainer asked for the current one only.
			// Purchased is the same figure as Sales -> Purchase and Born and is not
			// narrowed by the filters; the caption has to say so, or a reader filtering to one stage
			// would expect the two bars to match.
			"section.loads.title":         "Purchased loads",
			"section.loads.aria":          "Purchased loads against the live herd",
			"section.loads.caption":       "Each load's animals bought, still on farm under the current filters, and male / female. Bought is the load's own total and does not follow the filters.",
			"chart.loads.aria":            "Animals per purchased load",
			"chart.loads.empty":           "No purchased load has animals in the herd yet.",
			"chart.series.load_purchased": "Bought",
			"chart.series.load_on_farm":   "On farm",
			"chart.series.load_male":      "Male",
			"chart.series.load_female":    "Female",
			"label.load":                  "Load",
			"label.load_unnumbered":       "Unnumbered load",
			"table.loads.aria":            "Each purchased load's animals against the live herd",
			"column.load":                 "Load",
			"column.load_vendor":          "Vendor",
			"column.load_bought_on":       "Bought on",
			"column.load_purchased":       "Bought",
			"column.load_on_farm":         "On farm",
			"column.load_male":            "Male",
			"column.load_female":          "Female",

			// Whole-pen stage change (maintainer decision 2026-08-12). Copy is deliberately plain
			// farm language: the operator is retagging a pen, not "reclassifying a cohort".
			"stage_change.title":              "Change stage",
			"stage_change.heading":            "Change stage for a pen",
			"stage_change.caption":            "Every animal in the selected pen moves to the stage you pick. Kid or adult follows the stage.",
			"stage_change.shed_label":         "Pen",
			"stage_change.shed_hint":          "Pick the pen. A location split into pens lists each pen separately.",
			"stage_change.stage_label":        "New stage",
			"stage_change.reason_label":       "Reason",
			"stage_change.reason_hint":        "Recorded against every animal that changes.",
			"stage_change.preview_action":     "Check",
			"stage_change.submit_action":      "Change stage",
			"stage_change.cancel_action":      "Cancel",
			"stage_change.close_action":       "Close",
			"stage_change.preview_title":      "What will change",
			"stage_change.current_title":      "In this pen now",
			"stage_change.changing_label":     "Will change",
			"stage_change.unchanged_label":    "Already on this stage",
			"stage_change.total_label":        "Animals in pen",
			"stage_change.age_kid":            "Kids",
			"stage_change.age_adult":          "Adults",
			"stage_change.age_unchanged":      "Kid or adult stays as it is",
			"stage_change.applies_now":        "This applies straight away. There is no approval step.",
			"stage_change.no_change":          "Every animal in this pen is already on that stage. Nothing to change.",
			"stage_change.done":               "Stage changed.",
			"stage_change.disabled_no_access": "Only the CEO can change a whole pen's stage.",
		}
	case "milk-preparation":
		return map[string]string{
			"crumb":                             "Counts",
			"section.preparation.title":         "Milk preparation worklist",
			"section.preparation.aria":          "Per-pen milk preparation worklist",
			"section.preparation.caption":       "Current K1, K2 and K3 head count × approved per-session milk quantity",
			"section.preparation.note":          "This direction reads the live herd now. K0 colostrum and clinical or ICU feeding are not treated as zero; they remain outside this preparation calculation until their own quantity rules are approved.",
			"section.summary.aria":              "Milk preparation summary",
			"section.summary.note":              "Totals cover every K1, K2 and K3 cohort in the selected park, not only the visible page.",
			"section.verification.label":        "Park-day verification",
			"kpi.sheds.label":                   "Pens",
			"kpi.sheds.sub":                     "Physical pens with milk-fed cohorts",
			"kpi.kids.label":                    "Kids",
			"kpi.kids.sub":                      "K1, K2 and K3 kids in the live herd",
			"kpi.milk.label":                    "Milk required",
			"kpi.milk.sub":                      "Total litres for all active sessions",
			"kpi.citric.label":                  "Citric acid",
			"kpi.citric.sub":                    "5.5 g per litre of prepared milk",
			"table.preparation.aria":            "Milk preparation rows",
			"table.preparation.noun":            "cohort",
			"filter.bar_aria":                   "Filter milk preparation rows",
			"filter.park_label":                 "Park",
			"filter.all_option":                 "All",
			"filter.clear_all":                  "Clear all",
			"filter.scope_readonly":             "Park scope is set in the top bar.",
			"label.prepared_for":                "Prepared {preparation_date} for feeding {feeding_date}",
			"label.litres":                      "L",
			"label.grams":                       "g",
			"label.ready":                       "Ready",
			"label.blocked":                     "Blocked",
			"label.not_submitted":               "Not submitted",
			"label.pending_verification":        "Pending verification",
			"label.verified":                    "Verified",
			"label.rework":                      "Rework",
			"label.missing_shed":                "No physical pen is recorded for this cohort.",
			"label.unassigned_park":             "No park",
			"label.unassigned_shed":             "No pen",
			"label.inactive_session":            "—",
			"empty.preparation":                 "No K1, K2 or K3 kids are currently present in this scope.",
			"state.preparation_unavailable":     "Milk preparation direction unavailable",
			"state.preparation_contract_absent": "Milk preparation page contract unavailable",
			"state.try_again":                   "Try again. If the problem continues, contact Data Ops.",
			"pager.page":                        "Page",
			"pager.rows":                        "Rows",
			"action.previous":                   "Previous",
			"action.next":                       "Next",
		}
	// -------------------------------------------------------------------------------
	// Feed vertical copy.
	//
	// Every key a Feed page reads MUST exist here — copy() throws at render time on a
	// missing key, so an under-specified case is a blank screen, not a blank string.
	//
	// Four of these strings are safety copy, not decoration, and are why this block is
	// verbose:
	//
	//   BLOCKED vs CONFIGURED ZERO. An authored rate of 0 (K0/K1 kids on milk) means
	//   "feed nothing, this is correct". A MISSING rate means "we do not know what to
	//   feed this pen". The migration keeps these structurally distinct (grams_per_head
	//   is NOT NULL with no default; absence of a row is the only encoding of
	//   not-configured) precisely because collapsing them is a starvation path. The UI
	//   words must keep them distinct too — a blocked row must never read as "0 kg".
	//
	//   PROJECTED vs CURRENT count. The direction sheet plans day D using the projected
	//   head count (today's live herd plus approved-but-unexecuted movements that are
	//   feed-effective by D). It is deliberately not today's census and must not be
	//   labelled as one.
	//
	//   OVERDUE SHIFTING. A movement the feed plan already assumes has not physically
	//   happened. It keeps contributing (see FeedShiftingCountsToward) rather than
	//   silently dropping out, so the operator has to be told it is standing open.
	//
	//   EXPERIMENT vs NORMAL. Experiment sheds carry hand-entered ABSOLUTE kg for the
	//   whole shed; head count there is informational and is never multiplied in.
	// -------------------------------------------------------------------------------
	case "feed-analytics":
		return map[string]string{
			"crumb": "Feed",
			// The one word this page lives or dies on: DIRECTED. The sheet's
			// instruction, never a measured weight — leftovers are not captured.
			"banner.basis":   "Figures show feed as DIRECTED on the daily sheet, plus the milk the crew prepared. The two daily charts run through today, whose sheet is already issued; the tiles and the execution figures describe yesterday, the last finished day. Leftovers are not measured yet, so read quantities as instructions, not consumption.",
			"tab.overview":   "Consumption",
			"tab.items":      "Stock",
			"tab.peranimal":  "Per Animal",
			"tab.execution":  "Execution",
			"tab.experiment": "Experiment",
			"tab.followup":   "Follow-up",
			// Feed follow-up. The words here carry the rule, because the tab is
			// an accusation when it says "Not followed" and the reader is owed
			// exactly what was compared: the sheet before the event, and the
			// first sheet issued after it.
			// The hint carries the FARM CLOCK in the module's OWN vocabulary
			// (maintainer correction 2026-09-23): a day's FEED DIRECTION is
			// issued the morning before, amended at the correction time and
			// LOCKED at the transport cutoff. Feed PACKING is the separate
			// operator task. Saying "packed a day ahead" here conflated the
			// two. Only a locked direction is final, so only a locked one is
			// judged.
			"followup.hint":                "One line per change. A day's feed direction is issued the morning before and locked at the transport cutoff, so a change has until the day after next to reach it. Each line compares what the pen was fed on the day the animals moved with the first LOCKED direction two days later; one still being corrected is not judged. Animals moved between pens are not counted as a cause — that shows up as an unexplained change.",
			"followup.status.followed":     "Feed changed",
			"followup.status.not_followed": "Feed unchanged",
			"followup.status.pending":      "Direction not locked yet",
			"followup.table.title":         "Changes and what the feed did",
			"followup.col.pen":             "Pen",
			"followup.col.changes":         "What happened",
			"followup.col.when":            "Directions compared",
			"followup.col.animals":         "Animals fed",
			"followup.col.feed":            "Feed per day",
			"followup.col.status":          "Result",
			"followup.cause.purchased":     "Bought",
			"followup.cause.sold":          "Sold",
			"followup.cause.died":          "Died",
			"followup.unexplained.chip":    "{count} unexplained",
			"followup.unexplained.help":    "The pen's number moved by more than buying, selling and deaths account for — usually animals moved in or out of the pen.",
			// Named in the SAME order as the arrows beside them: what the pen
			// was fed when the animals moved, then what it was fed after.
			"followup.day.sheets":  "{before} → {after}",
			"followup.day.pending": "Not issued yet",
			"followup.empty":       "No animals were bought, sold or lost in this range, so there is nothing to follow up.",
			// Consumption's two readings (maintainer request 2026-09-17). Status-wise is the average
			// DIRECTED feed one animal gets per day, grouped by the PEN's tag on the sheet.
			"consumption.view.aria":          "Consumption view",
			"consumption.view.general":       "General",
			"consumption.view.status":        "Status-wise",
			"status.hint":                    "Each status is the pen's tag on the feed sheet, averaged over the selected range. Spend prices every feed at the farm's latest load rate; kg is all sheet feeds for one animal. Milk and pens with mixed tags are left out.",
			"status.chart.cap":               "₹ spent per day (solid line) and kg per animal per day (dashed) · {count} animals on average",
			"status.series.spend":            "₹ spent",
			"status.series.kg":               "kg per animal",
			"status.spend_per_day":           "₹ / day",
			"status.kg_per_animal":           "kg / animal / day",
			"status.empty":                   "No pen was fed on the sheet in this range.",
			"series.other":                   "Other feeds",
			"kpi.packing.label":              "Packing verified",
			"kpi.packing.sub":                "Bags approved by the verifier in the window",
			"kpi.distribution.label":         "Distribution verified",
			"kpi.distribution.sub":           "Pen-sessions approved in the window",
			"kpi.transport.label":            "Transport completed",
			"kpi.transport.sub":              "Daily pen transport tasks done",
			"kpi.latency.label":              "Verify latency",
			"kpi.latency.sub":                "Median submit → verdict, latest day with verdicts",
			"stock.title":                    "Feed stock",
			"stock.hint":                     "Purchased minus directed since the ledger bootstrap — stock leaves the store when the sheet locks for packing",
			"stock.days_left":                "days left",
			"stock.balance":                  "kg in store",
			"stock.per_day":                  "kg/day",
			"stock.batch":                    "latest load",
			"stock.low":                      "Low stock",
			"stock.never_directed":           "not directed recently",
			"stock.not_started":              "Feeding not started",
			"stock.not_started_sub":          "In store, none given yet",
			"stock.empty":                    "No purchase ledger yet — stock appears once the feed loads are imported.",
			"stock.farms.title":              "Mesha concentrates by farm",
			"stock.farms.hint":               "Each farm's latest load and when its consumption started — a new load is used only once the earlier stock is finished. Mesha concentrate feeds only, with the feeds they replaced counted inside them",
			"stock.farms.load_pending":       "Not started — previous stock in use",
			"stock.farms.col.farm":           "Farm",
			"stock.farms.col.item":           "Feed item",
			"stock.farms.col.directed_since": "Consumption from",
			"stock.farms.col.avg":            "Avg / day",
			"stock.farms.col.week":           "Week need",
			"forecast.title":                 "Feed needed for the next 7 days",
			"forecast.hint":                  "Each farm's requirement for the coming week at the current feeding rate, priced at that farm's latest load rate. Quantities carry every feed the farm feeds, not only the Mesha concentrates.",
			"forecast.empty":                 "No feed has been fed recently enough to project a week's requirement.",
			"forecast.col.farm":              "Farm",
			"forecast.col.item":              "Feed item",
			"forecast.col.avg":               "Avg / day",
			"forecast.col.required":          "Needed (7 days)",
			"forecast.col.stock":             "In stock",
			"forecast.col.shortfall":         "To buy",
			"forecast.col.rate":              "Rate",
			"forecast.col.required_cost":     "Cost (7 days)",
			"forecast.total":                 "Total",
			"forecast.unpriced":              "No load rate",
			"forecast.covered":               "Stock covers the week",
			"stock.farms.col.last_load":      "Last load",
			"stock.farms.col.stock":          "Stock",
			"stock.farms.col.days_left":      "Days left",
			"stock.farms.batch":              "Load",
			"stock.farms.unavailable":        "No avg/day",
			"stock.farms.empty":              "No Mesha concentrate loads in the ledger yet.",
			// Purchased vs consumed tab. Consumption is attributed load by load, oldest first
			// within one farm and feed, off the locked sheets -- the same FIFO the Stock tab's
			// "Consumption from" already uses for the latest load.
			"loads.title":                  "Purchased vs consumed, load by load",
			"loads.hint":                   "Every load still in the store, used oldest first per farm and feed. Days left runs to the end of that load, so a feed's newest load matches its stock card. The check is days said minus days used minus that load's own days left.",
			"loads.empty":                  "No loads in the store.",
			"loads.col.purchase_date":      "Bought",
			"loads.col.feed_item":          "Feed item",
			"loads.load_line":              "{farm} · Load #{batch}",
			"loads.col.status":             "Status",
			"loads.col.purchased_kg":       "Bought kg",
			"loads.col.consumed_kg":        "Used kg",
			"loads.col.left_kg":            "Left kg",
			"loads.col.days_said":          "Days said",
			"loads.col.days_consumed":      "Days used",
			"loads.col.days_left":          "Days left",
			"loads.col.gap_days":           "Check",
			"loads.status.in_transit":      "In transit",
			"loads.status.not_started":     "Not started",
			"loads.status.in_use":          "In use",
			"loads.status.finished":        "Finished",
			"loads.consumption_from":       "Used from {date}",
			"loads.finished_on":            "Finished {date}",
			"loads.days_said.none":         "Not stated",
			"loads.days_left.unknown":      "No recent rate",
			"loads.gap.none":               "—",
			"loads.gap.zero":               "Matches",
			"loads.gap.positive":           "{days} days short",
			"loads.gap.negative":           "+{days} days",
			"loads.filter.farm":            "Farm",
			"loads.filter.farm.all":        "All farms",
			"loads.filter.feed":            "Feed item",
			"loads.filter.feed.all":        "All feeds",
			"loads.pager.noun":             "load",
			"range.coverage_note":          "Data covers only {days} days so far",
			"chart.spend.title":            "Daily feed expenditure",
			"spend.week.label":             "Spent in 7 days",
			"spend.week.sub":               "Last 7 days, through yesterday",
			"spend.month.label":            "Spent this month",
			"spend.month.sub":              "From the 1st, through yesterday",
			"spend.quarter.label":          "Spent in 3 months",
			"spend.quarter.sub":            "Rolling 92 days",
			"spend.year.label":             "Spent this year",
			"spend.year.sub":               "From January 1st",
			"chart.spend.hint":             "Directed kg priced at each feed's most recent load rate · ₹ per day · from 11/08/2026",
			"chart.spend.mode.aria":        "Show the spend overall or per animal",
			"chart.spend.mode.overall":     "Overall",
			"chart.spend.mode.per_animal":  "Per animal",
			"chart.spend.per_animal.label": "Feed cost per animal",
			"chart.spend.per_animal.hint":  "Each day's feed spend over the animals on that day's sheet · ₹ per animal per day · from 11/08/2026",
			"unit.rupees":                  "₹",
			"unit.rupees_per_animal":       "₹ per animal",
			"col.pens":                     "Pens",
			"col.kg":                       "kg / day",
			"range.30":                     "30 days",
			"range.61":                     "2 months",
			"range.92":                     "3 months",
			"range.aria":                   "Choose the date range",
			"kpi.directed.label":           "Directed yesterday",
			"kpi.directed.sub":             "kg on the issued sheet",
			"kpi.head_days.label":          "Animals fed yesterday",
			"kpi.head_days.sub":            "Distinct pen head count on the sheet",
			"kpi.per_head.label":           "Avg ration per animal",
			"kpi.per_head.sub":             "g per head per day, whole herd",
			"kpi.adherence.label":          "Execution verified",
			"kpi.adherence.sub":            "Packing + distribution approved by the verifier",
			"kpi.cost_per_animal.label":    "Feed cost per animal yesterday",
			"kpi.cost_per_animal.sub":      "₹ per animal per day — yesterday's feed spend over the animals fed",
			"chart.daily.title":            "Daily directed feed",
			"chart.daily.hint":             "Total kg fed per day, stacked by feed item — the issued sheet plus the milk the crew prepared",
			"chart.spend_share.title":      "Feed spend share",
			"chart.spend_share.unit":       "/ day",
			"chart.spend_share.hint":       "Average ₹ per day on every feed the farm buys over the window, priced at each feed's most recent load rate — where the money goes",
			// The pie shows EVERY ACTIVE FEED (maintainer request 2026-09-23), which is why it
			// carries no feed list of its own any more: the earlier allowlist -- bhusa and the two
			// merged Mesha concentrates -- left UHT Milk off a chart of where the feed money goes
			// while the milk is real spend the animals drink, and every feed added after it would
			// have been missing too until someone remembered to widen the list. The pie now shares
			// the per-item cards' hidden-feed rule below, so the chart and the cards under it
			// always show the same feeds: the retired split concentrates and the odds and ends
			// stay out of both, and a feed the farm starts buying appears in both with no edit.
			"chart.mix.title": "Feed mix",
			"chart.mix.hint":  "Share of the fed kg over the window, sheet and milk together",
			"chart.item.hint": "₹ spent per day (solid line) and kg fed per day (dashed), priced at each farm's latest load rate",
			// Feeds whose per-item card is NOT shown (maintainer requests 2026-09-14): the four
			// retired split concentrates the merged Mesha Adult / Kids Concentrate replaced, plus
			// the unbranded Concentrate, Vijay Concentrate and Baking Soda -- the cards are for the
			// feeds the farm buys and watches: bhusa, the two Mesha concentrates and milk. Exact
			// feed names, comma-separated; an empty rule hides nothing. Reporting only -- the stock
			// cards, forecast and expenditure still count every one of these while they last.
			"chart.item.hidden_feeds": "Mesha Adult Concentrate Goat,Mesha Adult Concentrate Sheep,Mesha Kids Goat Concentrate,Mesha Kids Sheep Concentrate,Concentrate,Vijay Concentrate,Baking Soda",
			"item.spend.per_day":      "₹ / day",
			"item.kg.per_day":         "kg / day",
			"item.rate":               "₹ / kg",
			"item.unpriced":           "No load rate yet — kg only",
			"item.series.spend":       "Spent",
			"item.series.fed":         "Fed",
			"chart.perhead.title":     "Ration per animal",
			"chart.perhead.hint":      "Grams per head per day by feed item",
			"chart.execution.title":   "Daily execution status",
			"chart.execution.hint":    "Pen-session completions by verification outcome",
			"consumption.empty":       "No directed feed in this window.",
			"consumption.trend.title": "Packed vs given over time",
			"consumption.trend.hint":  "Daily totals across the window: what the sheet directed, against what verifiers measured. A gap means no bag was verified that day.",
			"col.consumption.target":  "Directed kg",
			"col.consumption.actual":  "Measured kg",
			"col.consumption.breed":   "Breed",
			"variance.cohort_unknown": "Not on the sheet",
			"variance.title":          "Packed vs directed",
			"variance.hint":           "Every bag a verifier measured, biggest difference first, dated by the PACKING day it was weighed out on. Readings are entered without seeing the sheet, so a match is independent confirmation. A bag within 200 g of the sheet reads as matched; more than 200 g out either way is flagged.",
			// The two tones on the difference tag, said in words for the reader who hovers and for
			// a screen reader, which cannot see a colour at all.
			"variance.within_tolerance": "Within 200 g of the sheet — counted as matching.",
			"variance.beyond_tolerance": "More than 200 g away from the sheet.",
			"variance.noun":             "measured bag",
			"variance.empty":            "No bag has been measured in this window yet.",
			"col.variance.day":          "Packing day",
			"col.variance.park":         "Farm",
			"col.variance.pen":          "Pen",
			"col.variance.session":      "Session",
			"col.variance.item":         "Feed item",
			"col.variance.planned":      "Directed kg",
			"col.variance.verified":     "Measured kg",
			"col.variance.diff":         "Difference",
			// Feed direction completion (maintainer decision 2026-08-26): who fed, who did not, and
			// what they filmed. Farm words only -- the reader sees "Nobody fed this pen", never a
			// status enum.
			"completion.title":              "Feed direction completion",
			"completion.hint":               "Every pen the sheet directed for the chosen day, and whether the feeding was filmed and approved. Open a row to see each video's time and who uploaded it.",
			"completion.empty":              "No feed sheet was issued for this day, so there was nothing to feed.",
			"completion.empty_filtered":     "No pens match these filters on this day.",
			"completion.date.label":         "Feed day",
			"completion.date.aria":          "Choose which day's feeding the table shows",
			"completion.filter.shed":        "Pen",
			"completion.filter.status":      "Status",
			"col.completion.park":           "Farm",
			"col.completion.pen":            "Pen",
			"col.completion.session":        "Session",
			"col.completion.status":         "Status",
			"col.completion.videos":         "Videos",
			"col.completion.who":            "Submitted by",
			"col.completion.when":           "Submitted at",
			"completion.status.not_started": "Nobody fed this pen",
			"completion.status.await":       "Waiting for review",
			"completion.status.rework":      "Sent back to refilm",
			"completion.status.completed":   "Fed and approved",
			"completion.kpi.not_started":    "Not fed",
			"completion.kpi.await":          "Waiting for review",
			"completion.kpi.rework":         "Sent back",
			"completion.kpi.completed":      "Fed and approved",
			"completion.kpi.sub":            "pens this day",
			"completion.slot.weight":        "Weighed feed photo",
			"completion.slot.feed":          "Feeding video",
			"completion.slot.water":         "Water video",
			"completion.slot.missing":       "Not recorded",
			"completion.slot.no_name":       "Uploader not recorded",
			"completion.videos.count":       "{done} of 3",
			"completion.action.details":     "View details",
			"completion.pager.noun":         "pen",

			// The overview's "Feed by pen" charts (maintainer request 2026-09-14, replacing
			// the feed-mix table): pick a shed and every pen in it -- Castro 1, 2, 3 --
			// gets its own chart of the LAST 7 DAYS (its own window, independent of the
			// page's range chips), one bar per day, each bar the feed directed per animal
			// that day. The charts of one shed share ONE scale, because the point is to
			// compare the pens side by side and two scales would make a taller bar mean
			// less. A day the sheet directed nothing resolvable to the pen is a gap, never
			// a zero bar; the head count behind each bar rides on its tooltip.
			"shedfeed.title":                 "Feed by pen — last 7 days",
			"shedfeed.hint":                  "Pick a pen name and each of its pens gets a chart: two bars per day, the feed directed per animal that day beside what the verifier weighed on the packing videos, up to yesterday. Pens of the same name share one scale.",
			"shedfeed.empty":                 "No feed sheet was issued in the last 7 days, so there is nothing to show.",
			"shedfeed.empty_filtered":        "No pens match these filters.",
			"shedfeed.filter.shed":           "Pen",
			"shedfeed.chart.aria":            "Feed per animal by day",
			"shedfeed.day.gap":               "Nothing directed",
			"shedfeed.day.animals":           "animals",
			"shedfeed.day.total":             "kg in total",
			"shedfeed.legend.directed":       "Directed",
			"shedfeed.legend.verified":       "Verified",
			"shedfeed.day.verified_gap":      "Not yet verified",
			"shedfeed.day.bags":              "bags verified",
			"drawer.completion.aria":         "Feeding detail",
			"drawer.completion.close_label":  "Close feeding detail",
			"drawer.completion.eyebrow":      "Feed direction",
			"drawer.completion.videos":       "Videos",
			"drawer.completion.col.video":    "Video",
			"drawer.completion.col.when":     "Uploaded at",
			"drawer.completion.col.who":      "Uploaded by",
			"drawer.completion.submitted_by": "Submitted by",
			"drawer.completion.submitted_at": "Submitted at",
			"drawer.completion.verified_by":  "Approved by",
			"drawer.completion.verified_at":  "Approved at",
			"drawer.completion.rework":       "Sent back because",
			"drawer.completion.session":      "Session",
			"drawer.completion.farm":         "Farm",
			"drawer.completion.status":       "Status",
			"drawer.completion.none":         "—",
			"pager.page":                     "Page",
			"pager.rows":                     "Rows",
			"action.previous":                "Previous",
			"action.next":                    "Next",
			"variance.planned_unknown":       "Not on sheet",
			"chart.experiment.title":         "Experiment feed — by item",
			"chart.experiment.hint":          "Authored kg per feed item per day across the experiment pens",
			"wastage.title":                  "Leftover feed (wastage)",
			"wastage.hint":                   "Verifier-recorded leftover weight per pen for the chosen day",
			"wastage.empty":                  "No experiment pens on the sheet for this day.",
			"wastage.date.label":             "Day",
			"filter.date.today":              "Today",
			"filter.date.single":             "Single day",
			"filter.date.range":              "Date range",
			"filter.date.aria":               "Choose which day's leftovers the table shows",
			"filter.date.previous_month":     "Previous month",
			"filter.date.next_month":         "Next month",
			"filter.date.range_start_hint":   "Pick the first day of the range.",
			"filter.date.range_end_hint":     "Now pick the last day of the range.",
			"filter.date.range_separator":    "to",
			"filter.park_label":              "Farm",
			"filter.all_option":              "All",
			"filter.apply":                   "Apply filters",
			"filter.clear_all":               "Clear filters",
			"filter.bar_aria":                "Filter leftover feed",
			"filter.scope_readonly":          "Park scope is set in the top bar.",
			"col.wastage.park":               "Farm",
			"col.wastage.pen":                "Pen",
			"col.wastage.kg":                 "Leftover kg",
			"col.wastage.status":             "Status",
			"wastage.status.none":            "No video yet",
			"wastage.status.await":           "Awaiting measurement",
			"wastage.status.done":            "Measured",
			"wastage.status.rework":          "Needs a new video",
			"legend.verified":                "Verified",
			"legend.awaiting":                "Awaiting verdict",
			"legend.rework":                  "Rework",
			"legend.transport_open":          "Not yet submitted",
			"unit.kg":                        "kg",
			"unit.g_per_head":                "g / head / day",
			"unit.minutes":                   "min",
			"unit.pens":                      "pens",
			"unit.heads":                     "animals",
			"table.items.aria":               "Directed feed by item",
			"table.items.noun":               "row",
			"empty.title":                    "No feed sheet in this window",
			"empty.body":                     "No issued feed direction covers the selected dates. The sheet is issued each morning for the next feed day.",
			"empty.execution.body":           "No packing, distribution or transport completions in the selected dates.",
			"empty.experiment.body":          "No experiment sheet was issued in the selected dates.",
			"error.title":                    "Feed analytics is unavailable",
			"error.body":                     "The rollup read failed. The feed screens themselves are unaffected; try again shortly.",
		}
	case "feed-direction":
		return map[string]string{
			"crumb":                      "Feed",
			"section.direction.title":    "Feed Direction",
			"section.direction.aria":     "Generated feed direction rows",
			"section.direction.caption":  "Park × pen × pen tag × breed × session for the selected feed day",
			"section.direction.note":     "Each row is projected head count × authored grams per head × feed factor, split across the park's sessions. Rows are generated for one feed day; changing the day regenerates them.",
			"section.summary.title":      "Day summary",
			"section.summary.aria":       "Feed day summary",
			"section.summary.note":       "Totals cover every row matching the current filters, not only the visible page.",
			"kpi.projected_head.label":   "Projected head count",
			"kpi.projected_head.sub":     "Live herd plus approved movements already feed-effective for this day",
			"kpi.total_kg.label":         "Total feed",
			"kpi.total_kg.sub":           "kg as-fed across all sessions for the matching rows",
			"kpi.sheds.label":            "Pens fed",
			"kpi.sheds.sub":              "Pens with at least one generated row",
			"kpi.blocked.label":          "Blocked rows",
			"kpi.blocked.sub":            "Pens that cannot be fed until a ration is configured",
			"table.direction.aria":       "Feed direction rows",
			"table.direction.noun":       "row",
			"table.direction.total_row":  "Total (rows)",
			"filter.bar_aria":            "Filter feed direction rows",
			"filter.drawer.title":        "Filter — Feed Direction",
			"filter.date_label":          "Feed day",
			"filter.park_label":          "Park",
			"filter.shed_label":          "Pen",
			"filter.shed_tag_label":      "Pen tag",
			"filter.breed_label":         "Breed",
			"filter.ration_group_label":  "Ration group",
			"filter.session_label":       "Session",
			"filter.feed_item_label":     "Feed item",
			"filter.workflow_label":      "Workflow",
			"filter.status_label":        "Row status",
			"filter.all_option":          "All",
			"filter.clear_all":           "Clear all",
			"filter.scope_readonly":      "Park scope is set in the top bar.",
			"label.animals_noun":         "animals",
			"label.kg_noun":              "kg",
			"label.formula":              "head count × grams per head × feed factor, split by session",
			"label.projected_count":      "Projected count",
			"label.projected_count_note": "This is the head count this pen is PLANNED to hold on the selected feed day — today's live herd plus approved movements that are already feed-effective. It is not today's exact census, and it is not a physical count.",
			"label.current_count":        "Current count",
			"label.current_count_note":   "Animals standing in the pen today, before any approved movement is applied.",
			"label.pending_delta":        "Pending movement",
			"label.pending_delta_note":   "Net animals this pen is due to gain or lose from approved-but-unexecuted movements that are feed-effective by the selected day.",
			"label.blocked":              "Blocked — no ration configured",
			"label.blocked_note":         "This pen's (ration group, pen tag, feed item) has NO authored rate. That is not a quantity of zero — it means we do not know what to feed these animals, so nothing is planned and this pen will not be fed until a rate is configured in Feed Config. Do not read the blank quantity as 0 kg.",
			"label.blocked_short":        "No ration configured",
			"label.configured_zero":      "Configured zero",
			"label.configured_zero_note": "An authored rate of 0 g/head — correct and deliberate, for example K0 and K1 kids that are on milk and are fed none of this solid item. This pen IS configured; it is simply fed nothing of this item.",
			// Configured-zero lines are HIDDEN on this sheet (see feed-quantity-state.ts). This is the
			// disclosure that keeps the omission honest: a reader who expects RGS Concentrate on a row
			// and does not find it must be able to tell "authored as 0" from "we dropped it". The
			// second sentence is the load-bearing half — it promises that a MISSING rate is never
			// hidden, so an absent line can always be read as a deliberate zero and never as a gap.
			"label.zero_items_omitted":           "Items authored at 0 g/head are not listed — those animals are fed none of that item, so there is nothing to weigh out. Items with NO authored rate are never hidden: they always appear as “No ration configured”.",
			"empty.nothing_to_feed":              "Nothing to feed this session — every item for this pen is authored at 0 g/head.",
			"label.overdue_shifting":             "Overdue movement",
			"label.overdue_shifting_note":        "This projected count includes an approved movement that has been standing open since before today’s packing day without the animals physically being moved. A movement counts toward the feed plan from the day it is authorized, so the plan already assumes the animals are here. Execute or cancel the movement — it will keep counting toward this pen every day until you do.",
			"label.overdue_shifting_chip":        "movement overdue",
			"label.clamped":                      "Negative projection floored",
			"label.clamped_note":                 "Recorded movements remove more animals than this grain holds, so the projection was floored at zero. That is a data problem to investigate, not a real count.",
			"label.workflow_normal":              "Per-head (normal)",
			"label.workflow_normal_note":         "Quantity is DERIVED: projected head count × the authored grams per head for this pen's ration group and tag × the feed factor. Change the ration grid to change what this pen is fed.",
			"label.workflow_experiment":          "Experiment",
			"label.workflow_experiment_note":     "An experiment pen. Its quantities are HAND-ENTERED for that pen rather than read from the ration grid: as grams per animal, multiplied by the pen's head count, or — on a cell authored before this changed — as an absolute total for the whole pen.",
			"label.session_split":                "Session split",
			"label.session_split_note":           "The day's quantity for this pen is divided across the park's sessions by the authored split; the session splits for a park add up to the whole day.",
			"label.ok":                           "Planned",
			"label.ok_note":                      "A rate is configured and a quantity was computed for this row.",
			"empty.direction":                    "No feed rows generated for this day and scope yet.",
			"empty.direction_filtered":           "No feed rows match these filters.",
			"empty.blocked":                      "No blocked rows — every pen in scope has a configured ration.",
			"state.direction_unavailable":        "Feed direction unavailable",
			"state.generation_blocked":           "Feed direction could not be generated for this day: the underlying counts projection is carrying unresolved blockers. Resolve them, then regenerate — a partial feed sheet is not published.",
			"state.generation_pending":           "The counts projection for this day is still being built. Feed rows appear once it settles.",
			"state.projected_counts_unavailable": "Projected counts unavailable",
			// Issue -> amend -> lock lifecycle banner. The default feed day is TOMORROW, which is
			// `pending` until its scheduled issue time, so this banner is what stands in for the
			// blank table on a not-yet-issued day. Aggregate state is the LEAST-ADVANCED workflow,
			// so a mixed park-day (normal issued, experiment still pending) shows the per-workflow
			// breakdown chips built from `lifecycle.state.*` and `lifecycle.workflow.*`.
			"lifecycle.aria":                 "Feed sheet status",
			"lifecycle.issued.title":         "Issued",
			"lifecycle.issued.body":          "This sheet is frozen and being packed.",
			"lifecycle.amended.title":        "Amended",
			"lifecycle.amended.body":         "Emergency shiftings have been folded into the frozen sheet.",
			"lifecycle.corrections_noun":     "correction(s)",
			"lifecycle.locked.title":         "Locked",
			"lifecycle.locked.body":          "Transport has left; changes now roll to the next feed day.",
			"lifecycle.pending.title":        "Not issued yet",
			"lifecycle.pending.body":         "Nothing is frozen for this feed day yet — the sheet is issued at its scheduled time, shown per workflow below.",
			"lifecycle.not_issued.title":     "No sheet issued",
			"lifecycle.not_issued.body":      "The issue time passed with nothing issued for this feed day. This is a real gap — raise it rather than reading it as nothing to feed.",
			"lifecycle.preview.title":        "Preview — not issued yet",
			"lifecycle.preview.body":         "This feed day has not been issued yet, so these rows are GENERATED from the current herd and config — a preview, not a frozen sheet. It will be issued at its scheduled time, shown per workflow below.",
			"lifecycle.beyond_horizon.title": "Beyond the projection window",
			"lifecycle.beyond_horizon.body":  "This feed day is outside the projection window. Feed counts are only meaningful for today (being fed) and tomorrow (being packed now) — beyond tomorrow the counts depend on movements not yet approved, and a past day's herd is not what it is now. No sheet can be generated for this day; open today or tomorrow, or wait for it to enter the window.",
			"lifecycle.draft.title":          "Draft",
			"lifecycle.draft.body":           "Live preview computed from the current herd and config — not an issued sheet.",
			"lifecycle.workflow.normal":      "Normal",
			"lifecycle.workflow.experiment":  "Experiment",
			"lifecycle.state.issued":         "issued",
			"lifecycle.state.amended":        "amended",
			"lifecycle.state.locked":         "locked",
			"lifecycle.state.pending":        "pending",
			"lifecycle.state.not_issued":     "not issued",
			"lifecycle.state.preview":        "preview",
			"lifecycle.state.beyond_horizon": "beyond window",
		}
	case "feed-packing":
		return map[string]string{
			"crumb":                   "Feed",
			"section.packing.title":   "Packing worklist",
			"section.packing.aria":    "Per-pen feed packing worklist",
			"section.packing.caption": "What the store weighs out per pen, session and feed item",
			"section.packing.note":    "This is the same generated day as Feed Direction, rolled up to what actually gets packed: one line per pen × session × feed item. It is not a second generation run.",
			// The picker axis is the PACKING day; this caption states the feed day it is for (packing
			// day + 1). {date} is filled in by the renderer with the formatted feed day.
			"caption.feed_for":        "This feed is for {date}",
			"section.summary.title":   "Pack summary",
			"section.summary.aria":    "Packing day summary",
			"section.summary.note":    "Totals cover every line matching the current filters, not only the visible page.",
			"kpi.total_kg.label":      "Total to pack",
			"kpi.total_kg.sub":        "kg as-fed across all sessions for the matching lines",
			"kpi.sheds.label":         "Pens to pack",
			"kpi.sheds.sub":           "Pens with at least one line to weigh out",
			"kpi.items.label":         "Feed items",
			"kpi.items.sub":           "Distinct items in this day's pack",
			"kpi.blocked.label":       "Blocked pens",
			"kpi.blocked.sub":         "Pens with nothing to pack because no ration is configured",
			"table.packing.aria":      "Feed packing lines",
			"table.packing.noun":      "line",
			"table.packing.total_row": "Total (lines)",
			"filter.bar_aria":         "Filter packing lines",
			"filter.drawer.title":     "Filter — Feed Packing",
			// The Feed Packing picker selects the PACKING day (the day the sheet is packed), not the
			// feed day; the caption above states which feed day it is for.
			"filter.date_label":          "Packing day",
			"filter.park_label":          "Park",
			"filter.shed_label":          "Pen",
			"filter.session_label":       "Session",
			"filter.feed_item_label":     "Feed item",
			"filter.workflow_label":      "Workflow",
			"filter.status_label":        "Line status",
			"filter.all_option":          "All",
			"filter.clear_all":           "Clear all",
			"filter.scope_readonly":      "Park scope is set in the top bar.",
			"label.kg_noun":              "kg",
			"label.expected_kg":          "Expected",
			"label.expected_kg_note":     "The quantity to weigh out for this pen, session and item. Derived from the day's feed direction — it is not entered here.",
			"label.blocked":              "Blocked — no ration configured",
			"label.blocked_note":         "Nothing is packed for this pen because its (ration group, pen tag, feed item) has NO authored rate. This is not a pack quantity of zero — the pen will not be fed at all until a rate is configured in Feed Config. Do not substitute a guess.",
			"label.blocked_short":        "No ration configured",
			"label.configured_zero":      "Configured zero",
			"label.configured_zero_note": "An authored rate of 0 g/head — nothing to pack for this item, and that is correct (for example K0 and K1 kids on milk). Distinct from blocked: this pen IS configured.",
			// See the twin note on feed-direction. A packer must never be handed a line telling them to
			// weigh out 0.000 kg, and must equally never have a blocked line quietly disappear — the
			// second sentence is the promise that makes an absent line safe to interpret.
			"label.zero_items_omitted":       "Items authored at 0 g/head are not listed — there is nothing to weigh out for them. Items with NO authored rate are never hidden: they always appear as “No ration configured”.",
			"empty.nothing_to_feed":          "Nothing to pack for this session — every item for this pen is authored at 0 g/head.",
			"label.overdue_shifting":         "Overdue movement",
			"label.overdue_shifting_note":    "This pen's pack quantity is based on a projected count that includes an approved movement standing open since before today’s packing day without the animals being moved. A movement counts toward the feed plan from the day it is authorized. Pack to the plan, and get the movement executed or cancelled.",
			"label.overdue_shifting_chip":    "movement overdue",
			"label.workflow_normal":          "Per-head (normal)",
			"label.workflow_normal_note":     "Quantity derived from projected head count × the authored ration for this pen.",
			"label.workflow_experiment":      "Experiment",
			"label.workflow_experiment_note": "Experiment pen: these quantities were hand-entered for this pen rather than read from the ration grid. Pack exactly the amount shown — it is already worked out for the animals in this pen.",
			"label.session_noun":             "session",
			"label.ok":                       "Ready to pack",
			"label.ok_note":                  "A quantity was computed and this line can be weighed out.",
			"empty.blocked":                  "No blocked pens — every pen in scope has a configured ration.",
			"empty.packing":                  "Nothing to pack for this day and scope yet.",
			"empty.packing_filtered":         "No packing lines match these filters.",
			"state.packing_unavailable":      "Packing worklist unavailable",
			"state.generation_blocked":       "No packing worklist for this day: the underlying feed direction could not be generated because the counts projection is carrying unresolved blockers. Resolve them and regenerate — a partial pack list is not published.",
			"state.generation_pending":       "The feed direction for this day is still being generated. Packing lines appear once it is ready.",
			// Twin of the feed-direction lifecycle banner copy — the same shared FeedLifecycleBanner
			// renders on both pages, so the keys and wording match. A not-yet-issued day shows this
			// instead of an empty worklist bar.
			"lifecycle.aria":                 "Feed sheet status",
			"lifecycle.issued.title":         "Issued",
			"lifecycle.issued.body":          "This sheet is frozen and being packed.",
			"lifecycle.amended.title":        "Amended",
			"lifecycle.amended.body":         "Emergency shiftings have been folded into the frozen sheet.",
			"lifecycle.corrections_noun":     "correction(s)",
			"lifecycle.locked.title":         "Locked",
			"lifecycle.locked.body":          "Transport has left; changes now roll to the next feed day.",
			"lifecycle.pending.title":        "Not issued yet",
			"lifecycle.pending.body":         "Nothing is frozen for this feed day yet — the sheet is issued at its scheduled time, shown per workflow below.",
			"lifecycle.not_issued.title":     "No sheet issued",
			"lifecycle.not_issued.body":      "The issue time passed with nothing issued for this feed day. This is a real gap — raise it rather than reading it as nothing to pack.",
			"lifecycle.preview.title":        "Preview — not issued yet",
			"lifecycle.preview.body":         "This feed day has not been issued yet, so this worklist is GENERATED from the current herd and config — a preview, not a frozen sheet. It will be issued at its scheduled time, shown per workflow below.",
			"lifecycle.beyond_horizon.title": "Beyond the projection window",
			"lifecycle.beyond_horizon.body":  "This feed day is outside the projection window. Feed counts are only meaningful for today (being fed) and tomorrow (being packed now) — beyond tomorrow the counts depend on movements not yet approved, and a past day's herd is not what it is now. No worklist can be generated for this day; open today or tomorrow, or wait for it to enter the window.",
			"lifecycle.draft.title":          "Draft",
			"lifecycle.draft.body":           "Live preview computed from the current herd and config — not an issued sheet.",
			"lifecycle.workflow.normal":      "Normal",
			"lifecycle.workflow.experiment":  "Experiment",
			"lifecycle.state.issued":         "issued",
			"lifecycle.state.amended":        "amended",
			"lifecycle.state.locked":         "locked",
			"lifecycle.state.pending":        "pending",
			"lifecycle.state.not_issued":     "not issued",
			"lifecycle.state.preview":        "preview",
			"lifecycle.state.beyond_horizon": "beyond window",
		}
	case "health-config":
		// Every visible string on /health/config. The renderer owns layout and nothing else --
		// AGENTS.md's backend-owns-labels rule applies with extra force here because these strings
		// describe a MEDICAL document, and a client-invented label ("dose", "amount", "strength")
		// would quietly redefine what an author thinks they are typing.
		return map[string]string{
			"crumb": "Health",

			"section.catalog.title":   "Treatment protocols",
			"section.catalog.aria":    "Authored disease treatment courses",
			"section.catalog.caption": "One course per disease, per age band",
			"section.catalog.note":    "The standing rulebook a diagnosis loads from. Adult and kid are separately authored because a kid's dosage is not always an adult's, and the phone picks between them using the animal's own age band. Editing never touches the live course: a change builds a draft, and publishing swaps which version is live.",

			"section.steps.title":   "Protocol steps",
			"section.steps.aria":    "The day-by-day course",
			"section.steps.caption": "What the operator does, in the order they do it",
			"section.steps.note":    "Each step is a medicine, an action, or a critical action that hands the animal to quarantine or a lifecycle exit. Steps are ordered by day and by session within the day; the order shown here is the order the operator works through.",

			"section.history.title": "Version history",
			"section.history.note":  "Every published version is kept. A goat pins the version it was diagnosed under and finishes its course on those dosages, so retiring a version never changes what an animal mid-treatment receives.",

			"label.disease":              "Disease",
			"label.age_band":             "Age band",
			"label.age_band.adult":       "Adult",
			"label.age_band.kid":         "Kid",
			"label.duration_days":        "Days",
			"label.duration_days_help":   "How many days the course runs.",
			"label.step_count":           "Steps",
			"label.medication_count":     "Medicines",
			"label.critical_count":       "Critical actions",
			"label.published_version":    "Live version",
			"label.draft_state":          "Draft",
			"label.day_no":               "Day",
			"label.session":              "Session",
			"label.session.morning":      "Morning",
			"label.session.afternoon":    "Afternoon",
			"label.session.evening":      "Evening",
			"label.session.unscheduled":  "Any time",
			"label.record_type":          "Step type",
			"label.record_type.action":   "Action",
			"label.record_type.medicine": "Medicine",
			"label.record_type.critical": "Critical action",
			"label.medicine_name":        "Medicine",
			"label.dosage_text":          "Dosage",
			"label.dosage_denominator":   "Unit",
			"label.medicine_route":       "Route",
			"label.instruction":          "Instruction",
			"label.critical_action_type": "Hands off to",
			"label.critical.quarantine":  "Quarantine or movement",
			"label.critical.exit":        "Lifecycle exit",
			"label.open_cases":           "Goats being treated on this version",

			"action.add_disease":      "Add disease",
			"action.edit_protocol":    "Edit",
			"action.publish_protocol": "Publish",
			"action.discard_draft":    "Discard draft",
			"action.save_draft":       "Save draft",
			"action.add_step":         "Add step",
			"action.remove_step":      "Remove",

			"status.live":       "Live",
			"status.draft":      "Draft",
			"status.retired":    "Retired",
			"status.no_live":    "Not published",
			"status.draft_open": "Draft open",
			"status.draft_none": "—",

			"note.unscheduled_session": "\"Any time\" is a real choice, not a missing one: it is the right session for a once-daily medicine given whenever the operator reaches the animal.",
			"note.dosage_unit":         "\"none\" is a real unit for a whole-unit dose such as one bolus, and is different from leaving the unit blank.",
			"note.publish_effect":      "Publishing replaces the live course for the NEXT diagnosis. Goats already being treated finish on the version they started.",
			"note.days_shrink":         "Shortening the number of days will not delete steps. Move or remove any step past the last day first.",
			"note.both_bands":          "Adding a disease opens a draft for both adult and kid so neither band is missing when a goat is diagnosed. Edit each one separately, then publish each.",
			"note.rename_scope":        "Renaming a disease renames it for both age bands.",

			"filter.all_option":     "All",
			"filter.bar_aria":       "Filter treatment protocols",
			"filter.clear_all":      "Clear all",
			"filter.age_band_label": "Age band",
			"filter.draft_label":    "Draft state",
			"filter.draft_any":      "All",
			"filter.draft_only":     "With an open draft",
			"filter.search_label":   "Search a disease",

			"pager.next":      "Next",
			"pager.restart":   "Back to start",
			"pager.rows_note": "Server-paginated. This screen shows one page of the rulebook, never a running total.",

			"empty.catalog": "No treatment protocols are authored yet.",
			"empty.steps":   "This course has no steps yet. Add the first one.",
			"empty.search":  "No disease matches that search.",

			"health_config.disabled_no_write": "Your current role can read the treatment protocols but cannot change them.",
			"error.validation_title":          "Fix these before continuing",
			"error.draft_exists":              "Someone else already has a draft open for this protocol.",
			"error.disease_exists":            "A disease with that name already exists.",
			"error.not_a_draft":               "Only a draft can be published or discarded.",
			"error.protocol_in_use":           "This protocol is being used by an open case.",
			// Says what happened and what to do. It must NOT claim the list "has been refreshed":
			// the dead version is still in the URL, so the notice returns on every reload until the
			// author leaves it. Telling them it self-healed when it has not is worse than silence.
			"error.stale_version": "That version is no longer there \u2014 it was published or discarded. Everything below is up to date.",
			"action.back_to_list": "Back to the list",
			// A draft that lost its steps is not "a course with no steps yet" -- the live
			// course still has them, and the two sentences sitting on one screen read as a
			// contradiction. Say which one the animals are being treated from, and that
			// discarding returns to it, because that is the recovery and it is not obvious.
			"label.pick_medicine": "Search the medicine list",
			// A medicine exists on Configuration > Items first, and only then can a course
			// name it. The sentence says where to add one, because "not in the list" without
			// a next step is a dead end.
			"warn.medicine_not_in_catalog": "Not in this farm's medicine list. Pick one from the list, or add it under Configuration \u203a Items first.",
			"warn.empty_draft_over_live":   "This draft is empty, but the live course still has its steps and is what animals are treated from. Publishing is blocked until you add one. Discard the draft to go back to the live course untouched.",

			// ---- the diagnosis register ---------------------------------------------------
			// The other half of the rulebook. The copy's job is to keep two things
			// un-missable: a version carries the FORM and the RULES together, and the two
			// halves are checked against each other rather than saved side by side.
			"tab.protocols":                     "Treatment",
			"tab.registers":                     "Diagnosis",
			"section.registers.title":           "Diagnosis registers",
			"section.registers.aria":            "Authored diagnosis registers",
			"section.registers.caption":         "One register per animal class \u2014 the questions asked about a sick animal, and the illnesses the answers point to",
			"section.registers.note":            "A register holds the observation form AND the rules together, in one published version. A question whose answer no rule reads does nothing; a rule that reads a finding no question asks can never be diagnosed. Publishing checks both directions, so the two halves can only go live together.",
			"section.questions.title":           "Questions",
			"section.questions.caption":         "What the manager is asked about the animal in front of them",
			"section.questions.note":            "Every question is compulsory. A blank cannot tell \u201cnobody looked\u201d from \u201cnormal\u201d, and the unaccounted-for findings on a proposal depend on that difference. A question that does not apply to the animal \u2014 an udder on a male \u2014 is not asked at all rather than left empty.",
			"section.rules.title":               "Illnesses",
			"section.rules.caption":             "What each illness looks like, and the treatment it opens",
			"section.rules.note":                "Rules are judged in parallel, never as a tree: an animal can have two illnesses at once, and a sign being absent does not rule one out. Treats names the treatment course the diagnosis opens; a rule with none is a field action, treated in place.",
			"table.registers.aria":              "Diagnosis register rows",
			"table.registers.noun":              "register",
			"table.questions.aria":              "Question rows",
			"table.questions.noun":              "question",
			"table.rules.aria":                  "Rule rows",
			"table.rules.noun":                  "rule",
			"empty.registers":                   "No diagnosis register is published yet.",
			"empty.questions":                   "This register asks nothing yet.",
			"empty.rules":                       "This register names no illness yet.",
			"action.edit_register":              "Edit",
			"action.publish_register":           "Publish",
			"action.discard_register_draft":     "Discard draft",
			"label.animal_class":                "Animals",
			"label.register_live":               "Live",
			"label.register_draft":              "Draft",
			"label.answers":                     "Answers",
			"label.findings":                    "Findings",
			"label.treats":                      "Opens",
			"label.field_action":                "Treated in place",
			"label.only_if":                     "Asked when",
			"label.warnings":                    "Worth knowing",
			"note.register_warnings":            "These do not stop a publish. Each one is a question no rule reads yet, or an illness whose treatment course nobody has written.",
			"register_config.disabled_no_write": "Your current role can read the diagnosis registers but cannot change them.",
			"action.register_opened":            "Editing a copy of the live register. Nothing changes for the herd until you publish it.",
			"action.register_saved":             "Saved as a draft. The live register is unchanged.",
			// "Unchanged" is a real outcome, not a silent no-op: the author pressed save and
			// nothing differed from what was already stored. Saying so beats a success that
			// looks identical to one that wrote something.
			"action.register_unchanged": "Nothing to save \u2014 this is already what the draft says.",
			"action.register_published": "Published. New observations are judged against this register from now on; proposals already made keep the version they were made under.",
			"action.register_discarded": "Draft discarded. The live register was never touched.",
			// The class as a person says it. The stored value is a machine key; a farm reads
			// "Kids on milk", which is the phrase a vet recognises from the shed.
			"label.class.adult":         "Adults",
			"label.class.kid_milk":      "Kids on milk",
			"label.class.kid_weaning":   "Kids weaning",
			"label.class.kid_fattening": "Kids fattening",
			// The three tiers a rule can reach. They are a CONFIDENCE axis, separate from
			// severity: a possible bloat outranks a confirmed pinkeye, because ranking on
			// confidence alone treats the eye while the bladder ruptures.
			"label.tier.pathognomonic": "Confirmed by",
			"label.tier.probable":      "Probable when",
			"label.tier.possible":      "Possible when",
			"label.kind.choice":        "Pick one",
			"label.kind.multi":         "Pick any",
			"label.kind.number":        "Measurement",
			"label.answer_no":          "No",
			"label.answer_yes":         "Yes",
			"action.remove_question":   "Remove question",
			"action.remove_rule":       "Remove illness",
			"action.remove_answer":     "Remove answer",
			"action.remove_band":       "Remove band",
			"action.remove_clause":     "Remove condition",
			"label.question_title":     "Question",
			"label.question_id":        "Key",
			"label.question_kind":      "Answered by",
			"label.rule_id":            "Illness",
			"label.severity":           "Severity",
			"label.answer_label":       "Answer",
			"label.answer_value":       "Key",
			"label.band_over":          "Over",
			"label.band_from":          "From",
			"label.band_under":         "Under",
			"label.band_upto":          "Up to",
			"action.add_question":      "Add a question",
			"action.add_rule":          "Add an illness",
			"action.add_answer":        "Add an answer",
			"action.add_band":          "Add a band",
			"action.add_clause":        "Add a way to recognise it",

			// ---- who is judged by which rulebook ------------------------------------------
			// The vocabulary is deliberately plain. "Type" is the farm's word for a rulebook;
			// "stage" is the word already painted on its own screens. Nothing here says class,
			// register key or route, because those are the machine's words for the same things.
			"tab.types":                "Types",
			"section.types.title":      "Diagnosis types",
			"section.types.caption":    "The rulebooks this farm diagnoses against",
			"section.types.note":       "Each type has its own questions and illnesses. Add one when a group of animals should be judged differently -- then say below which animals it covers, and write its rules on the Diagnosis tab.",
			"section.routes.title":     "Who gets which type",
			"section.routes.caption":   "The management stage an animal is on decides the type it is judged against",
			"section.routes.note":      "The most specific rule wins: a row for one stage overrides the row that covers the whole age band. An animal no row covers is not diagnosed at all -- it is refused, and the manager is told which stage to map.",
			"section.gaps.title":       "Stages with no type",
			"section.gaps.caption":     "Animals here cannot be observed until a type covers them",
			"section.gaps.note":        "Nothing else reports this. Until one of these stages is mapped, a manager opening one of its animals is turned away. Stages like ICU and Quarantine are not listed as gaps: they say where an animal is, not what it eats, so they cannot pick a rulebook.",
			"table.types.aria":         "Diagnosis type rows",
			"table.types.noun":         "type",
			"table.routes.aria":        "Stage routing rows",
			"table.routes.noun":        "rule",
			"table.gaps.aria":          "Unmapped stage rows",
			"table.gaps.noun":          "stage",
			"empty.types":              "No diagnosis type is set up yet.",
			"empty.routes":             "No animals are routed to a type yet.",
			"empty.gaps":               "Every stage that holds animals reaches a type.",
			"label.type_key":           "Key",
			"label.type_label":         "Name",
			"label.route_count":        "Stages",
			"label.has_register":       "Rules written",
			"label.stage":              "Stage",
			"label.sub_stage":          "Cohort",
			"label.live_animals":       "Animals now",
			"label.band.adult":         "Adults",
			"label.band.kid":           "Kids",
			"label.every_stage":        "Every stage",
			"label.builtin":            "Built in",
			"label.retired":            "Retired",
			"label.clinical_placement": "Where the animal is, not what it eats",
			// The sheet: download a type's rulebook, edit it in Excel, upload it back.
			"action.download_sheet":    "Download rules",
			"action.download_template": "Blank template",
			"action.upload_sheet":      "Upload a sheet",
			"action.sheet_imported":    "Sheet read. Saved as a draft \u2014 questions / illnesses",
			// Said BEFORE the file is chosen. An author who expects a publish and gets a draft has
			// to be told twice, and the second telling is after they have gone looking for the
			// change on the herd.
			"note.sheet_writes_a_draft":         "An upload never goes live on its own. It is saved as a draft on this tab; publish it when you have read it through.",
			"action.add_type":                   "Add a type",
			"action.edit_type":                  "Rename",
			"action.retire_type":                "Retire",
			"action.add_route":                  "Cover a stage",
			"action.edit_route":                 "Change",
			"action.remove_route":               "Remove",
			"action.map_stage":                  "Give it a type",
			"note.wildcard_route":               "This covers every stage in the band that no other row names. Removing it stops those animals being diagnosed until each stage has its own row.",
			"note.type_key_fixed":               "The key never changes -- past diagnoses are recorded against it. The name is yours to edit.",
			"note.no_register_yet":              "No rules written yet. Nothing is diagnosed against this type until they are.",
			"warn.retire_routed":                "Stages still send animals here. Move them to another type first.",
			"warn.retire_builtin":               "This is one of the rulebooks the system ships with and cannot be retired.",
			"diagnosis_types.disabled_no_write": "Your current role can read the diagnosis types but cannot change them.",
			"action.type_saved":                 "Saved.",
			"action.route_saved":                "Saved. Animals on that stage are judged against this type from their next observation.",
			"action.route_removed":              "Removed. Animals on that stage are not diagnosed until another row covers them.",

			// ---- the layman's vocabulary ---------------------------------------------------
			// A register is two lists joined by one idea: an answer can be a SIGN, and an
			// illness is recognised by signs. The screen says exactly that, everywhere, and
			// never shows the machine token unless the reader asks for it.
			"label.sign":             "Sign",
			"label.signs":            "Signs",
			"label.is_a_sign":        "This answer is a sign",
			"label.not_a_sign":       "Normal — not a sign",
			"label.used_by":          "Points to",
			"label.used_by_none":     "No illness uses this yet",
			"label.pick_sign":        "Add a sign",
			"label.any_one_confirms": "Any one of these is enough",
			"label.all_must_hold":    "and",
			"label.advanced":         "Machine name",
			"label.no_conditions":    "Nothing recognises this illness yet",
			"label.question_count":   "questions",
			"label.illness_count":    "illnesses",
			"note.how_it_works":      "The manager answers every question about the sick animal. Some answers are SIGNS — something a healthy animal would not show. Each illness below lists the signs that point to it, and the engine proposes every illness whose signs are present. Two illnesses can be proposed at once, because an animal can have two.",
			"note.questions_how":     "Mark an answer as a sign when it means something is wrong. \u201cEating normally\u201d is not a sign; \u201cNot eating\u201d is. Beside each sign you can see which illnesses it points to, and add it to another.",
			"note.rules_how":         "An illness is recognised by its signs. Each line is one way to recognise it, and any single line is enough. A line with two signs needs both at once. Confirmed, Probable and Possible say how sure that line makes the engine.",
		}
	case "feed-config":
		return map[string]string{
			"crumb":                        "Feed",
			"section.ration_grid.title":    "Ration grid",
			"section.ration_grid.aria":     "Authored ration rates",
			"section.ration_grid.caption":  "Ration group × pen tag × feed item → grams per head per day",
			"section.ration_grid.note":     "The standing rule the daily feed sheet is computed from. Rates are park-scoped because parks genuinely differ, and effective-dated: an edit closes the current rate and opens a new one rather than overwriting it, so past feed sheets stay explainable.",
			"section.shed_factors.title":   "Feed factors",
			"section.shed_factors.aria":    "Feed multipliers",
			"section.shed_factors.caption": "Per location and feed item multiplier applied on top of the ration grid",
			"section.shed_factors.note":    "The third term of head count × grams per head × feed factor. A location with no factor row is treated as 1.0; a factor of 0 must be authored deliberately.",
			// ---- experiment sheds -------------------------------------------------------------
			// Copy for the one section on this page that is NOT the ration grid's world. Its job is
			// to keep two facts un-missable: absolute_kg is a pen TOTAL (never per head), and
			// listing a pen here is what switches which workflow feeds it.
			"section.experiment.title":             "Experiment pens",
			"section.experiment.aria":              "Hand-authored experiment pens",
			"section.experiment.caption":           "Pens fed hand-entered quantities instead of the ration grid above",
			"section.experiment.note":              "These pens are NOT computed from the ration grid. An operator hand-enters the grams each animal in the pen gets of each item, and the feed sheet multiplies that by the number of animals in the pen on the day. The count shown here is that live number, straight from the herd register — nobody types it. While a pen is listed here, the rates and feed factors above have no effect on it.",
			"section.experiment.switch_note":       "A pen is on the experiment workflow because it is listed here, and for no other reason — there is no separate flag. Adding a pen switches it off the per-head grid; returning it switches it straight back, and only that pen: the other pens are untouched. Returning a pen keeps its authored quantities, so restoring it later does not mean re-entering them.",
			"table.experiment.aria":                "Experiment pen rows",
			"table.experiment.noun":                "experiment row",
			"label.experiment_grams_per_head":      "Grams per animal (per day)",
			"label.experiment_grams_per_head_note": "What ONE animal in this pen gets of this item each day. The feed sheet multiplies it by the number of animals in the pen that day, so the quantity follows animals in and out without anyone re-authoring it. Enter an explicit 0 to feed none of this item; a blank field is not zero. The feed factors above do not apply to an experiment pen.",
			"label.experiment_absolute_kg":         "Absolute kg (whole pen)",
			"label.experiment_absolute_kg_note":    "An older cell: a total for the WHOLE pen, already inclusive of every animal in it, and never multiplied by any head count. Cells are now authored as grams per animal — editing this one asks for grams and moves it over.",
			"label.experiment_head_count":          "Animals in this pen",
			"label.experiment_head_count_note":     "How many animals are in this pen right now, from the herd register — the same count the feed sheet multiplies the grams by, so what you see here is what the pen is fed against. Nobody types it: it follows the animals as they move, and 0 means the pen is currently empty. An older whole-pen cell is not multiplied by it at all.",
			"label.experiment_category":            "Experiment arm",
			"label.experiment_category_note":       "Which arm of the trial this pen is on. It appears in the pen-tag column of the direction sheet, where it is the operator's cue that these numbers were hand-entered rather than computed.",
			"label.experiment_active":              "On experiment (hand-entered)",
			"label.experiment_active_note":         "This pen is fed the quantities authored here — grams per animal times the pen's head count, or the whole-pen total on an older cell. The ration grid and its feed factors do not affect it.",
			"label.experiment_retired":             "On the normal grid (per head)",
			"label.experiment_retired_note":        "This pen has been returned to the ration grid and is fed projected head count × grams per head × feed factor again. Its authored experiment quantities are kept, so restoring it does not mean re-entering them.",
			"label.experiment_basis_grams":         "grams per animal",
			"label.experiment_basis_kg":            "kg for the whole pen",
			"label.experiment_not_dated_note":      "Unlike the rates above, experiment quantities are not effective-dated: an edit corrects the figure in place. They are hand-entered numbers for a running trial, not a standing rule a past feed sheet has to be explained against. Who changed what is still recorded.",
			"action.add_experiment_pen":            "Move a pen to the experiment",
			"action.add_experiment_item":           "Add feed item",
			"action.edit_experiment_cell":          "Edit",
			// "Shift to normal feed" (maintainer, 2026-08-12). It names what happens to the ANIMALS —
			// they go back to being fed from the standing ration — rather than to a table on this
			// screen. "The normal grid" only means something to someone already looking at the grid.
			"action.withdraw_experiment_shed": "Shift to normal feed",
			// Retained for an in-flight form; the table no longer lists a withdrawn pen, so nothing
			// renders this. A pen comes back through the enroller, which offers it again.
			"action.restore_experiment_shed":       "Return this pen to the experiment",
			"action.experiment_saved":              "Saved. Each animal in this pen gets the grams authored here; the sheet multiplies by however many animals are in the pen that day.",
			"action.experiment_switched":           "Workflow switched. What this pen is fed has changed — check the next Feed Direction for this park.",
			"reason.experiment_blank_is_not_zero":  "Leave the field blank only if you do not intend to author this item for this pen. To feed none of it, enter an explicit 0. A cleared field is not zero.",
			"reason.experiment_switch_consequence": "Switching a pen changes what its animals eat; it is not a display setting. Only the pen named here changes; the other pens are untouched.",
			"empty.experiment":                     "No experiment pens authored for this park. Every operational pen in it is fed from the ration grid above.",
			"empty.experiment_filtered":            "No experiment pens match these filters.",
			"empty.experiment_candidates":          "Every pen in this park already has authored experiment quantities.",
			// Shown on a pen whose every catalog item already has an authored cell. Distinct from
			// empty.experiment_candidates, which is about SHEDS not yet on the experiment workflow.
			"empty.experiment_items_authored": "Every feed item is already authored for this pen. Edit a quantity above to change one.",
			// The park this page is reading, when nothing chose it. /feed/config is single-park by
			// construction -- a ration grid, a session split and a dispatch clock are all park-scoped
			// -- so a company-wide top-bar scope cannot be honoured here and one park is shown instead.
			// Without this the screen silently reads the alphabetically first park and says nothing.
			"notice.park_scope_fallback": "Experiment pens below show BOTH parks. The ration grid, feed factors, session template and feeding schedule are authored per park and cannot be shown for all parks at once, so those four are reading the park named here — use the Park filter to change it.",
			// The enroller. It authors a PEN and every feed item of it in ONE atomic write, so its copy
			// has to say both things: which pen, and that a blank kg authors nothing rather than zero.
			"filter.pen_label": "Pen",
			// The enroller takes SEVERAL pens at once (maintainer instruction 2026-09-21). The note
			// has to say the thing an operator would otherwise learn by being wrong: the quantities
			// below are per animal and are authored onto every pen ticked, not split between them.
			"label.experiment_enrol_pens_note":  "Tick every pen that goes on this experiment. The arm and the quantities below are authored onto each one.",
			"action.choose_experiment_pens":     "Choose pens",
			"action.select_all_experiment_pens": "Tick all pens",
			"action.clear_experiment_pens":      "Clear",
			"label.experiment_enrol_items":      "Quantities",
			"label.experiment_enrol_items_note": "Enter the grams ONE animal in this pen gets of each feed item. The sheet multiplies by the number of animals in the pen that day. Leave an item blank to author nothing for it — blank is not zero. All of them are saved together, or none is.",
			// Shown instead of a park name when the top bar reads company-wide. The experiment table
			// genuinely spans both parks in that mode, so naming one park above it would be a lie.
			"label.all_parks":              "All parks",
			"reason.experiment_enrol_park": "Choose the park first: a pen belongs to one park, and the pens offered below are that park's.",
			"state.experiment_unavailable": "Experiment pens unavailable",
			// ---- feed items (the catalog) -----------------------------------------------------
			// Copy for the vocabulary section. Its job is to keep ONE fact un-missable: adding an
			// item authors no quantity, so a new item feeds nothing until a rate names it. Someone
			// who adds "RGS Concentrate" and expects it on tomorrow's sheet has to be told here.
			"section.feed_items.title":   "Feed items",
			"section.feed_items.aria":    "Feed item catalog",
			"section.feed_items.caption": "The feed vocabulary every rate, feed factor and experiment quantity is authored against",
			"section.feed_items.note":    "Shared by every park. Adding an item does NOT feed it to anything: the new name becomes selectable on the ration grid above, and each ration group and pen tag using it stays unconfigured — and therefore blocked — until a rate is authored for it. Adding an item never creates a rate, not even a zero.",
			"table.feed_items.aria":      "Feed item catalog rows",
			"table.feed_items.noun":      "feed item",
			"label.feed_item_name":       "Feed item name",
			"label.feed_item_name_note":  "The name that appears on the ration grid, the feed factors, the experiment pens and the generated feed sheet. Case and surrounding spaces do not make a second item: a name the catalog already holds is refused rather than added twice.",
			// Changing a feed item's status. Worded as ACTIVE / INACTIVE (maintainer decision
			// 2026-08-13), replacing the earlier "In feeding" / "Not fed" chip and its
			// "Remove" / "Restore" controls. The old wording read as a deletion, which this has
			// never been: an inactive item keeps every authored rate, feed factor and experiment
			// cell exactly as it was, and reactivating returns them without re-entering anything.
			// "Remove" beside a Restore button invited the opposite reading.
			//
			// The consequence line stays mandatory copy, not decoration — the wording changed, the
			// effect did not. This control changes what animals eat from the next issued sheet, and
			// its authored rates leave the ration grid at the same moment. Deliberately terse on the
			// button itself: it sits INLINE beside the status chip in a table cell, so the full
			// consequence rides on hover and again on the confirm step, where a reader needs it.
			//
			// Display copy only. STORAGE vocabulary is still `active` / `retired` — the chip resolves
			// through these keys rather than printing row.status precisely so the two can differ.
			"action.retire_feed_item":         "Deactivate",
			"action.restore_feed_item":        "Activate",
			"action.feed_item_status_changed": "Saved. What is fed has changed — check the next Feed Direction for every park.",
			"reason.retire_feed_item":         "Takes this item off every future feed sheet, and hides its authored rates on the ration grid above. Nothing is deleted: the rates, feed factors and experiment quantities are kept exactly as they are, so reactivating the item restores them without re-entering anything.",
			"reason.restore_feed_item":        "Puts this item back into feeding. Its authored rates return to the ration grid above exactly as they were.",
			"label.feed_item_active":          "Active",
			"label.feed_item_active_note":     "This item is part of the feed vocabulary. It appears on the ration grid above and is packed and served wherever a rate is authored for it.",
			"label.feed_item_retired":         "Inactive",
			"label.feed_item_retired_note":    "This item is not being fed. It is on no feed sheet and its authored rates are hidden from the ration grid above — but they are kept, so reactivating it restores them.",
			// The status cell is edited IN PLACE: double-click swaps the chip for a picker and the
			// choice applies immediately. The hint is contract copy because it is the only thing
			// telling an operator the cell is editable at all — a chip that looks like every other
			// read-only chip on the page otherwise advertises nothing.
			"hint.feed_item_status_edit": "Double-click to change whether this item is fed.",
			"label.energy_kcal_per_kg":   "Energy (kcal/kg)",
			"label.dry_matter_factor":    "Dry matter factor",
			"label.wastage_factor":       "Wastage factor",
			"label.display_order":        "Display order",
			// Every attribute hint says the same thing in its own terms: blank is "not measured",
			// which is a different statement from a measured 0 and is never turned into one.
			"label.feed_item_attributes_note": "All four are optional. Leave one blank when nobody has measured it — a blank is recorded as not measured, which is honest, and is never stored as 0. A missing energy value only blocks a nutritional rollup; it never affects how much an animal is fed.",
			"label.energy_kcal_per_kg_note":   "Metabolisable energy per kilogram. Blank means not measured; an explicit 0 means measured as carrying none.",
			"label.dry_matter_factor_note":    "Share of the item that is dry matter — greater than 0 and at most 1. Blank means not measured.",
			"label.wastage_factor_note":       "Expected wastage share — at least 0 and less than 1. Blank means not measured; 0 means no wastage is expected.",
			"label.display_order_note":        "Where the item sits in the lists on this page. Leave it blank to add the item at the end.",
			"action.add_feed_item":            "Add feed item",
			// Both lines name BOTH remaining steps on purpose. Saying only "a rate still has to be
			// entered" reads as though a quantity is sufficient, and it is not: a feed is served only
			// once a session serves it, so an authored quantity on an undeclared feed reaches nobody.
			"action.add_feed_item_open":         "Add a feed item to the catalog. It appears on the ration grid at zero — set its quantity, then add it to a session below before anything is fed it.",
			"action.feed_item_saved":            "Feed item added, and it now has a cell in every part of the ration grid at zero. Set its quantity, then add it to a session below — nothing is fed it until a session serves it.",
			"action.feed_item_rejected":         "Feed item rejected. Correct the values and try again.",
			"reason.feed_item_exists":           "The catalog already holds a feed item with this name, so nothing was added. Names that differ only in capitals or spacing are the same item.",
			"reason.feed_item_name_required":    "Enter a name for the feed item.",
			"empty.feed_items":                  "No feed items in the catalog yet. Add one before authoring any rates — a rate has to name the item it is for.",
			"state.feed_items_unavailable":      "Feed item catalog unavailable",
			"section.session_template.title":    "Session template",
			"section.session_template.aria":     "Per-park session split",
			"section.session_template.caption":  "How each park's daily quantity is divided across its feeding sessions",
			"section.session_template.note":     "The session splits for a park must add up to the whole day. A park whose splits do not add up would under- or over-feed every pen in it, so the writer rejects it.",
			"section.session_template.caption2": "Each session lists the feeds it serves. A feed is only served if it is on a session here — a quantity in the ration grid on its own feeds nobody.",
			// The split warning is stated wherever a feed is added, because it is the single most
			// likely way to author this wrong. The grid quantity is a DAILY figure and each session
			// serves its own share of it, so a feed put on the morning session only delivers the
			// morning's share, not the whole day's.
			"action.add_session_feed":         "Add a feed",
			"action.add_session_feed_open":    "Add a feed to this session. The ration grid quantity is for the whole day, and each session serves its own share of it — a feed added to one session only delivers that session's share.",
			"action.add_session_feed_label":   "Feed",
			"action.add_session_feed_submit":  "Add to this session",
			"action.remove_session_feed":      "Remove",
			"action.remove_session_feed_open": "Stop serving this feed in this session. Sheets already issued are not changed.",
			"action.session_feed_saved":       "Session updated. The next sheet issued for this park serves the feeds listed here.",
			"action.session_feed_rejected":    "The session was not changed. Correct the problem and try again.",
			"reason.slot_rates_incomplete":    "This feed has no quantity set in every part of the ration grid for this park. Serving it would stop those pens getting a sheet at all, so set its quantities first — zero is a valid answer.",
			"reason.slot_not_declared":        "This session does not serve that feed, so there was nothing to remove. Check whether you meant the other session.",
			"reason.session_feed_required":    "Choose a feed to add.",
			"empty.session_feeds":             "No feeds — this session serves nothing and its pens will not be fed. Add a feed to start serving it.",
			"section.schedule.title":          "Feed day clock",
			"section.schedule.aria":           "Per-park feed day dispatch clock",
			"section.schedule.caption":        "When tomorrow's direction is issued, amended and cut off — per park and workflow",
			"section.schedule.note":           "These are the times the SHEET moves, not the times animals eat. A direction is issued today for tomorrow's feed, packed today and transported before the cutoff, and fed the next morning. Quantities come from the ration grid and session template above; the clock never changes how much is fed.",
			"kpi.rates.label":                 "Authored rates",
			"kpi.rates.sub":                   "Currently in-force ration grid rows",
			"kpi.groups.label":                "Ration groups",
			"kpi.groups.sub":                  "Distinct groups the grid is indexed by",
			"kpi.items.label":                 "Feed items",
			"kpi.items.sub":                   "Active items in the catalog",
			"kpi.gaps.label":                  "Unconfigured combinations",
			"kpi.gaps.sub":                    "In-use group and tag combinations with no authored rate",
			"table.ration_grid.aria":          "Ration grid rows",
			"table.ration_grid.noun":          "rate",
			"table.shed_factors.aria":         "Feed factor rows",
			"table.shed_factors.noun":         "factor",
			"table.session_template.aria":     "Session template rows",
			"table.session_template.noun":     "session",
			"table.schedule.aria":             "Feeding schedule rows",
			"table.schedule.noun":             "session",
			"filter.bar_aria":                 "Filter feed configuration",
			"filter.drawer.title":             "Filter — Feed Config",
			"filter.park_label":               "Park",
			"filter.shed_label":               "Pen",
			"filter.ration_group_label":       "Ration group",
			"filter.breed_label":              "Breed",
			// Said out loud on the control, because the grid's own column keeps showing the GROUP a
			// row belongs to and the two vocabularies would otherwise look inconsistent.
			"filter.breed_note":      "Breeds are grouped for feeding: Beetal and Sirohi share one rate, so either breed shows the Beetal/Sirohi rows. Kid rates are not breed-specific and are excluded when a breed is picked.",
			"filter.shed_tag_label":  "Pen tag",
			"filter.feed_item_label": "Feed item",
			"filter.feed_item_note":  "Pick more than one to compare items side by side.",
			// The multi-select panel STAGES its ticks and commits them on this button, so the page
			// re-renders once rather than once per item ticked.
			"filter.apply":            "Apply",
			"filter.grams_label":      "Grams / head / day",
			"filter.grams_value_aria": "Grams per head per day to compare against",
			"filter.grams_note":       "Filters on the authored rate itself. \"More than 0\" hides the deliberate zeros; \"exactly 0\" shows only them. Neither reveals an unconfigured combination, which has no row to filter.",
			// The experiment section's own filters. Its quantity is labelled apart from the grid's on
			// purpose: the grid holds a per-head RATE in grams and this holds an ABSOLUTE pen total in
			// kg. One shared label is how the two come to be read as the same number.
			"filter.experiment_arm_label":    "Experiment arm",
			"filter.kg_label":                "Grams per animal",
			"filter.kg_value_aria":           "Grams per animal to compare against",
			"filter.experiment_bar_aria":     "Filter experiment pens",
			"filter.experiment_note":         "These filters narrow the experiment pens only. A pen is listed when at least one of its feed items matches, and only its matching items are shown beneath it.",
			"filter.applies_to_label":        "Applies to",
			"filter.status_label":            "Status",
			"filter.effective_label":         "Effective",
			"filter.all_option":              "All",
			"filter.clear_all":               "Clear all",
			"filter.scope_readonly":          "Park scope is set in the top bar.",
			"label.grams_noun":               "g/head/day",
			"label.kg_noun":                  "kg",
			"label.configured_zero":          "Configured zero",
			"label.configured_zero_note":     "0 g/head is a real authored rate, not a missing one. K0 and K1 kids are on milk and are correctly fed 0 g of every solid item. Saving 0 configures the combination; clearing the field does not.",
			"label.blocked":                  "Not configured",
			"label.blocked_note":             "No rate has ever been authored for this ration group, pen tag and feed item. Any pen that resolves to it is BLOCKED and will not be fed — it is not fed zero. Author a rate (including an explicit 0 if the animals should get none of this item) to unblock it.",
			"label.blocked_short":            "No ration configured",
			"label.effective_open":           "In force",
			"label.effective_open_note":      "No end date — this is the rate currently being applied.",
			"label.effective_closed":         "Superseded",
			"label.effective_closed_note":    "Closed by a later edit. Kept so past feed sheets remain explainable; it is no longer applied.",
			"label.kid_group_note":           "Kids resolve to a single ration group by age band and their breed is deliberately ignored, so one kid rate covers every breed.",
			"label.park_scoped_note":         "Rates are authored per park. A rate configured for one park is never applied to another.",
			"label.workflow_normal":          "Per-head (normal)",
			"label.workflow_normal_note":     "Pens fed from this grid: quantity = projected head count × grams per head × feed factor.",
			"label.workflow_experiment":      "Experiment (hand-entered)",
			"label.workflow_experiment_note": "Experiment pens bypass this grid entirely. An operator hand-enters the grams each animal in the pen gets, and the sheet multiplies by the pen's head count. Older cells hold a whole-pen total instead and are multiplied by nothing.",
			"label.applies_to_kid":           "Kid course",
			"label.applies_to_adult":         "Adult course",
			"label.session_split_note":       "Share of the day's quantity this session carries.",
			// Hover explanations for the feed day clock. Each says what the time DOES, so no column
			// can be read as a feeding time.
			"label.direction_time_note":          "When this workflow's packing direction is issued for the NEXT feed day. Normal parks issue in the morning; experiment pens issue in the afternoon.",
			"label.correction_time_note":         "When approved emergency-shifting corrections are batched and an amended direction is reissued. Corrections are deliberately batched rather than sent on approval, so a pen receives at most one amended sheet per day.",
			"label.transport_time_note":          "The cutoff after which a correction can no longer reach the pen — the load has left. Anything approved after this lands on the following day's direction.",
			"action.add_rate":                    "Add rate",
			"action.edit_rate":                   "Edit rate",
			"action.add_shed_factor":             "Add feed factor",
			"action.edit_shed_factor":            "Edit feed factor",
			"action.edit_schedule":               "Edit schedule",
			"action.rate_saved":                  "Rate saved. It takes effect from its effective-from date; the previous rate was closed, not overwritten.",
			"action.rate_rejected":               "Rate rejected. Correct the values and try again.",
			"reason.blank_is_not_zero":           "Leave the field blank only if you intend the combination to stay unconfigured (and therefore blocked). To feed nothing, enter an explicit 0.",
			"reason.no_write_permission":         "Your current role can read the ration grid but cannot author or change rates.",
			"empty.ration_grid":                  "No ration rates authored for this scope yet. Every pen resolving to this scope is blocked until at least one rate exists.",
			"empty.ration_grid_filtered":         "No ration rates match these filters.",
			"empty.shed_factors":                 "No feed factors authored. Every location is treated as a factor of 1.0.",
			"empty.shed_factors_filtered":        "No feed factors match these filters.",
			"empty.session_template":             "No session template authored for this park, so the daily quantity cannot be split across sessions.",
			"empty.session_template_filtered":    "No sessions match these filters.",
			"empty.schedule":                     "No feeding schedule authored for this park yet.",
			"empty.schedule_filtered":            "No schedule rows match these filters.",
			"state.ration_grid_unavailable":      "Ration grid unavailable",
			"state.shed_factors_unavailable":     "Feed factors unavailable",
			"state.session_template_unavailable": "Session template unavailable",
			"state.schedule_unavailable":         "Feeding schedule unavailable",
			"state.split_mismatch":               "This park's session splits do not add up to a whole day. Feed sheets for the park stay blocked until the template is corrected.",
		}
	case "herd-register":
		return map[string]string{
			"crumb":                                    "Counts",
			"section.herd.title":                       "Herd",
			"section.herd.aria":                        "Herd register",
			"section.herd.row_hint":                    "tap a row → Goat Passport",
			"section.summary.note":                     "Herd KPIs show exact scoped goat counts; the table pages live goats from /goats/search under the top-bar scope.",
			"section.summary.tooltip":                  "Exact scoped counts from canonical goats.",
			"section.summary.unavailable":              "Summary unavailable",
			"filter.drawer.title":                      "Filter — Counts / Herd",
			"filter.drawer.close_label":                "Close filters",
			"filter.scope_label":                       "Park",
			"filter.scope_readonly":                    "Park scope is set in the top bar — this filter is read-only.",
			"filter.herd_search_label":                 "Search (Tag / Display ID)",
			"filter.herd_search_placeholder":           "e.g. 901007000503824 or CBE-201",
			"filter.sex_label":                         "Sex",
			"filter.breed_label":                       "Breed",
			"filter.clear_all":                         "Clear all",
			"filter.search_label":                      "Search herd rows",
			"filter.toolbar_placeholder":               "Search rows...",
			"filter.toolbar_aria":                      "Search herd rows",
			"filter.rows_per_page_aria":                "Rows per page",
			"filter.active_badge":                      "on",
			"action.register_goat":                     "Register animal",
			"action.register_shed":                     "Register pen",
			"action.import_sheds":                      "Import pens",
			"action.import_sheet":                      "Import sheet",
			"action.new_report":                        "New report",
			"action.full_change_history":               "Full change history",
			"action.preview_rows":                      "Preview rows",
			"action.previewing_rows":                   "Previewing...",
			"action.re_edit":                           "Re-edit",
			"action.create_records":                    "Create records",
			"action.creating_records":                  "Creating...",
			"action.done":                              "Done",
			"action.download_template":                 "herd-register-template.csv",
			"action.download_shed_template":            "vaccination-pens-template.csv",
			"action.download_failed_goat_rows":         "herd-register-failed-rows.csv",
			"action.download_failed_shed_rows":         "vaccination-pens-failed-rows.csv",
			"action.export_failed_rows":                "Export failed rows",
			"action.commit_failed":                     "Commit failed",
			"action.preview_failed":                    "Preview failed",
			"error.preview_stale":                      "CSV changed after preview. Preview rows again before creating records.",
			"error.csv_unterminated_quote":             "CSV contains an unterminated quoted cell.",
			"error.xlsx_empty":                         "XLSX import must include at least one non-empty worksheet.",
			"error.xlsx_parse_failed":                  "XLSX import could not be read. Export the first worksheet as CSV and try again.",
			"action.working":                           "Working...",
			"action.create_sheds":                      "Create pens",
			"action.goat_registered_generation_queued": "Herd animal registered; vaccination generation queued.",
			"action.goat_registered_rejected":          "Herd animal rejected for creation; correct the source row and retry.",
			"action.goat_registered_ineligible":        "Herd animal registered; no vaccination obligations generated because the animal is ineligible by published rules.",
			"action.goat_registered_no_generation":     "Herd animal registered; no vaccination generation applicable.",
			"action.shed_registered":                   "Vaccination pen registered; it is now available for goat registration.",
			"reason.report_pending":                    "No herd report API exists in this slice. New report stays disabled.",
			"action.edit_reproductive":                 "Edit",
			"action.save_reproductive":                 "Save status",
			"action.reproductive_updated":              "Reproductive status updated; vaccination rechecks queued where applicable.",
			"drawer.reproductive.title":                "Edit reproductive status",
			"drawer.reproductive.subtitle":             "Records a reproductive status change → goat.reproductive.changed.",
			"drawer.reproductive.aria":                 "Edit reproductive status",
			"field.reproductive_status":                "Reproductive status",
			"field.reproductive_reason":                "Reason",
			"field.breeding_date":                      "Breeding / service date",
			"field.last_delivery_date":                 "Last delivery date",
			"option.select_reproductive_status":        "Select status",
			"placeholder.reproductive_reason":          "e.g. ultrasound-confirmed pregnant on 12/06/2026",
			"note.reproductive_dates_optional":         "Breeding and last-delivery dates are optional pregnancy-timing facts; leave blank to keep the stored value.",
			"reason.reproductive_unavailable":          "Reproductive status options are not configured for this tenant yet. Seed reproductive status definitions before editing.",
			"drawer.shed_register.title":               "Register pen",
			"drawer.shed_register.subtitle":            "Creates one active vaccination-usable pen under a real park.",
			"drawer.register.title":                    "Register animal",
			"drawer.register.subtitle":                 "Creates one canonical herd animal with two IDs → vaccination obligations generate.",
			"drawer.shed_import.title":                 "Import pens",
			"drawer.shed_import.subtitle":              "Bulk register vaccination-usable pens before goat entry.",
			"drawer.import.title":                      "Import sheet",
			"drawer.import.subtitle":                   "Bulk register herd animals — download template, paste/upload CSV, preview, then commit.",
			"drawer.passport.aria":                     "Animal Passport",
			"drawer.passport.close_label":              "Close Animal Passport drawer",
			"label.animal_identifier_1":                "Tag 1",
			"label.animal_identifier_2":                "Tag 2",
			"alert.locations.title":                    "Locations unavailable",
			"alert.locations.body":                     "Animal creation needs a real park and vaccination-usable pen from the locations master. Configure locations (or check the backend) before registering — no animal is created without a valid park/pen.",
			"alert.shed_locations.body":                "Pen creation needs a real active park from the locations master. Configure or seed parks before adding vaccination pens.",
			"alert.stages.title":                       "Animal stages unavailable",
			"alert.stages.body":                        "A vaccination trigger needs a real active animal stage from animal_stage_lookup. Seed or restore stages before registering animals.",
			"empty.herd":                               "No herd animals for this scope yet. Use Register animal or Import sheet to add the first animals — each valid row generates vaccination obligations.",
			"empty.herd_filtered":                      "No herd animals match these filters for this scope.",
			"empty.unavailable":                        "Herd is unavailable until the goats read API responds.",
			"field.animal_identifier_1":                "Tag 1",
			"field.animal_identifier_2":                "Tag 2",
			"field.species":                            "Species",
			"field.park_required":                      "Park (required)",
			"field.shed_required":                      "Pen (required)",
			"field.shed":                               "Pen",
			"field.shed_code":                          "Pen code",
			"field.shed_name_required":                 "Pen name (required)",
			"field.display_order":                      "Display order",
			"field.notes":                              "Notes",
			"field.farm":                               "Farm",
			"field.breed":                              "Breed",
			"field.management_stage":                   "Management stage",
			"field.sex":                                "Sex",
			"field.origin":                             "Origin",
			"field.dob":                                "DOB",
			"field.weight_kg":                          "Weight (kg)",
			"field.entry_date_required":                "Entry date (required)",
			"field.dob_estimated":                      "DOB is estimated",
			"field.dam_id":                             "Dam (mother) id — lineage",
			"field.sire_or_lot":                        "Sire / semen lot",
			"field.evidence_ref":                       "Source / evidence reference",
			"field.bulk_template":                      "1 · Download template",
			"field.bulk_upload":                        "2 · Upload CSV/XLSX",
			"field.bulk_paste":                         "…or paste CSV",
			"field.import_row":                         "Row",
			"field.import_decision":                    "Decision",
			"field.import_identity":                    "Identity",
			"field.import_notes":                       "Notes",
			"field.import_result":                      "Result",
			"field.failure_reason":                     "Failure reason",
			"placeholder.animal_identifier_1":          "scan or enter first physical ID",
			"placeholder.animal_identifier_2":          "scan or enter second physical ID",
			"placeholder.shed_code":                    "e.g. K1-A",
			"placeholder.shed_name":                    "e.g. Kid Pen K1-A",
			"placeholder.shed_notes":                   "vaccination operating note",
			"placeholder.breed":                        "e.g. Beetal",
			"placeholder.weight_kg":                    "e.g. 22.0",
			"placeholder.dam_id":                       "dam goat id",
			"placeholder.sire_or_lot":                  "e.g. BUCK-07",
			"placeholder.evidence_ref":                 "source doc / sheet row id (defaults to this registration's reference)",
			"option.no_parks":                          "No parks available",
			"option.no_vaccination_sheds":              "No vaccination-usable pens",
			"option.optional":                          "— optional —",
			"option.select_species":                    "Select species",
			"option.select_sex":                        "Select sex",
			"option.select_origin":                     "Select origin",
			"note.identifier_required":                 "Tag 1 is required now; Tag 2 is optional until double tagging is live. Tag values are never reused, even after death, sale, transfer, tag loss, or tag breakage.",
			"note.media_capture":                       "Media capture is not in this slice. Provenance is recorded as a source-record evidence ref; photo upload happens via the field app / proof API.",
			"note.shed_create":                         "The pen is created active, usable for counts, vaccination, and SOP execution, and not usable for feed/holding/quarantine/ICU in this vaccination-only entry path.",
			"note.shed_bulk_template":                  "Each row needs Park plus Pen name. Park may be an active park id, code, or name. Imported pens are created active and vaccination-usable; bad rows return per-row errors below.",
			"note.bulk_template":                       "Each row needs Tag 1, Species, Park, Pen, DOB, Sex, Origin, and Entry date. Tag 2 is optional until double tagging is live. Bad rows return per-row errors below; they are never silently dropped.",
			"note.preview_ready":                       "Previewed — review decisions below, then commit.",
			"note.committed_suffix":                    "The herd table has been refreshed.",
			"note.shed_committed_suffix":               "The location master has been refreshed; newly created pens appear in the goat registration pen selector after refresh.",
			"label.active":                             "Active",
			"label.adults":                             "Adults",
			"label.kids":                               "Kids",
			"label.untagged_kids":                      "Untagged kids",
			"label.total_records":                      "Total records",
			"label.seeded_goat_rows":                   "seeded goat rows",
			"label.dead":                               "Dead",
			"label.sold":                               "Sold",
			"label.culled":                             "Culled",
			"label.terminal_rows":                      "terminal rows",
			"label.live_scoped_register":               "live scoped register",
			"label.stage_shed_inferred":                "age band / stage",
			"label.invalid_id_rows":                    "invalid ID rows",
			"label.first_live_rows_prefix":             "first",
			"label.live_rows":                          "live rows",
			"label.all_parks":                          "all parks",
			"label.all_sheds":                          "all pens",
			"label.identifiers":                        "Identifiers",
			"label.goat_id":                            "goat_id",
			"label.display_id":                         "Display ID",
			"label.tag_1":                              "Tag 1",
			"label.tag_2":                              "Tag 2",
			"label.identity":                           "identifiers",
			"label.rows":                               "rows",
			"label.row_singular":                       "row",
			"label.row_plural":                         "rows",
			"label.create_ready":                       "create-ready",
			"label.review":                             "review",
			"label.skip":                               "skip",
			"label.created":                            "created",
			"label.committed":                          "committed",
			"label.failed":                             "failed",
			"label.of":                                 "of",
			"label.columns":                            "columns",
			"label.placeholder":                        "—",
			"pager.scale_note":                         "server-paginated at scale",
			"pager.end_note":                           "end of results",
			"tag.identity_title":                       "Identity state from the goats read model",
		}
	case "audit-log":
		return map[string]string{
			"crumb":                       "Admin / Data Ops",
			"section.span.title":          "Span of control",
			"section.activity.title":      "Activity trail",
			"section.advanced.title":      "Advanced (raw) filters",
			"table.activity.aria":         "Activity trail",
			"action.export":               "Export",
			"reason.export_pending":       "Export endpoint is outside this slice.",
			"filter.search_label":         "Search audit rows",
			"filter.search_placeholder":   "Search action, operator, ID...",
			"filter.actor_placeholder":    "Filter visible operators...",
			"filter.anomalies_only":       "Anomalies only",
			"filter.apply":                "Apply",
			"filter.clear_all":            "Clear all",
			"filter.family_title_prefix":  "Filter to",
			"drawer.record.aria":          "Audit record",
			"drawer.record.close_label":   "Close audit record",
			"drawer.record.eyebrow":       "AUDIT",
			"drawer.record.note":          "Read-only business audit projection. Raw trace IDs and replay fields stay in backend audit infrastructure; this drawer shows who did what, where, with proof, and the result.",
			"drawer.open_target":          "Open target",
			"pager.fixed_reason":          "Audit cursor contract currently fixes page size at 25 for this mock-shaped view.",
			"empty.activity":              "No audit rows for this filter set.",
			"empty.activity_unavailable":  "Audit rows are unavailable until the backend responds.",
			"empty.operators":             "No operators in the current audit page.",
			"empty.operators_filter":      "No operators match this filter.",
			"error.summary_unavailable":   "Audit summary is unavailable.",
			"error.audit_unavailable":     "audit_unavailable",
			"label.viewing_as":            "Viewing as",
			"label.previewing_as":         "Previewing as",
			"label.role_preview_note":     "Role preview is a presentation lens only — the real audit span is governed by backend RBAC and the top-bar park/date scope, not by this control.",
			"label.actions_in_view":       "Actions in view",
			"label.awaiting_verification": "Awaiting verification",
			"label.proof_coverage":        "Proof coverage",
			"label.flagged_anomalies":     "Flagged anomalies",
			"label.tap_clear_filters":     "tap to clear filters",
			"label.proof_signoff":         "proof + sign-off",
			"label.tap_proof_gaps":        "tap → proof gaps",
			"label.anomaly_sources":       "stock · deletes · skips",
			"label.operators":             "operators",
			"label.all_results":           "All results",
			"label.awaiting":              "Awaiting",
			"label.rejected":              "Rejected",
			"label.proof_gaps":            "Proof gaps",
			"label.first":                 "« First",
			"label.prev":                  "‹ Prev",
			"label.next":                  "Next ›",
			"label.last":                  "Last »",
			"label.results":               "results",
			"label.page":                  "Page",
			"label.advanced_note":         "resource / module / actor IDs — for entity-history links",
			"label.append_only_title":     "Append-only · tamper-proof.",
			"label.append_only_body":      "Stock-draw mismatches, record deletions, silent skips, and rework auto-raise anomalies into the operational trail.",
			"label.flagged_anomaly":       "Flagged anomaly",
			"field.actor_id":              "Operator / actor id",
			"field.resource_type":         "Target type",
			"field.resource_id":           "Target id",
			"field.module":                "Module",
			"field.category":              "Category",
			"placeholder.actor_uuid":      "actor uuid",
			"placeholder.goat":            "goat",
			"placeholder.uuid":            "uuid",
			"placeholder.source_entry":    "source_entry",
			"placeholder.accepted_intake": "accepted_intake",
			"reason.last_page_disabled":   "Cursor pagination cannot jump to the last page without a backend count cursor.",
		}
	case "dlq-center":
		return map[string]string{
			"crumb":                         "Admin / Data Ops",
			"section.events.title":          "Dead-letter events",
			"section.events.aria":           "Dead-letter event list",
			"section.repair.title":          "Repair controls",
			"filter.search_label":           "Search DLQ rows",
			"filter.search_placeholder":     "Search event, topic, aggregate, error...",
			"filter.event_type_label":       "Event type",
			"filter.event_type_placeholder": "goat.created",
			"filter.topic_label":            "Topic",
			"filter.topic_placeholder":      "identity.events",
			"filter.apply":                  "Apply",
			"filter.clear_all":              "Clear all",
			"action.replay":                 "Replay",
			"action.discard":                "Discard",
			"action.open_audit":             "Audit Log",
			"action.close":                  "Close",
			"action.replay_success":         "Replay queued. The relay will pick the event up again.",
			"action.discard_success":        "Event discarded and kept visible for audit.",
			"action.failed":                 "DLQ repair action failed.",
			"reason.repair_discarded":       "Discarded DLQ events are already closed and cannot be repaired again.",
			"reason.replay":                 "Replay moves a failed/dead-letter event back to pending. Idempotent consumers must prevent duplicate obligations, notifications, stock moves, or verification outcomes.",
			"reason.discard":                "Discard is for investigated poison messages that should not replay. It keeps the row visible as discarded and writes business audit history.",
			"form.reason_label":             "Repair reason",
			"form.reason_placeholder":       "What was inspected or fixed before this action?",
			"drawer.record.aria":            "DLQ event",
			"drawer.record.close_label":     "Close DLQ event drawer",
			"drawer.record.eyebrow":         "DLQ",
			"drawer.record.note":            "At-least-once event delivery is expected. This drawer shows the event, payload, error, retry count, and guarded repair actions.",
			"empty.events":                  "No outbox messages match this DLQ filter.",
			"empty.events_unavailable":      "DLQ rows are unavailable until the backend responds.",
			"error.dlq_unavailable":         "dlq_unavailable",
			"label.dead_letter_count":       "Dead-letter",
			"label.failed_count":            "Failed",
			"label.discarded_count":         "Discarded",
			"label.rows_in_view":            "Rows in view",
			"label.replay_safe":             "Replay safe",
			"label.status":                  "Status",
			"label.event_id":                "Event id",
			"label.aggregate":               "Aggregate",
			"label.idempotency_key":         "Idempotency key",
			"label.trace":                   "Trace",
			"label.created":                 "Created",
			"label.updated":                 "Updated",
			"label.headers":                 "Headers",
			"label.payload":                 "Payload",
			"label.last_error":              "Last error",
			"label.attempts":                "attempts",
			"label.replays":                 "replays",
			"label.selected":                "selected",
			"label.no_selection":            "Select a DLQ row to inspect payload and repair actions.",
			"pager.fixed_reason":            "DLQ list is bounded to 100 rows by default and 500 rows maximum.",
		}
	case "vaccination-plan":
		// This copy was inherited from the deleted /config screen and is now owned here.
		// /config was removed because nothing used it: no page linked to it, the only
		// protocol category that exists is vaccination, and Feed and Health each author
		// their own config on their own screens.
		return map[string]string{
			"crumb":                                                     "Preventive Care",
			"page.title":                                                "Vaccination plan",
			"page.subtitle":                                             "One plan decides which animal gets which vaccine, and when. Only you and the COO can publish it.",
			"capacity.title":                                            "Operator animal capacity",
			"capacity.note":                                             "How many unique animals one available operator can handle in one day",
			"capacity.info.label":                                       "About operator capacity",
			"capacity.info":                                             "This counts unique animals per available operator per business date, not vaccine doses or obligation rows. One animal receiving ET+TT Booster + PPR still consumes one operator slot. Spillover dates recompute operator timetable, leave, role, and scope.",
			"capacity.field.max_per_day":                                "Animals per operator per day",
			"capacity.field.max_per_day_placeholder":                    "e.g. 200",
			"capacity.field.capacity_scope":                             "Capacity scope",
			"capacity.field.max_buffer_days":                            "Max buffer days",
			"capacity.field.max_buffer_days_placeholder":                "e.g. 3",
			"capacity.help.max_buffer_days":                             "Extra safe-window days past the first due day before added operators or over-cap completion is required.",
			"capacity.preview.label":                                    "Under this cap",
			"capacity.status.within_cap":                                "Within cap",
			"capacity.status.over_cap":                                  "Split",
			"capacity.status.capacity_breach":                           "Capacity action",
			"capacity.field.overflow_policy":                            "Overflow policy",
			"page.lede":                                                 "What should happen.",
			"page.lede_detail":                                          "CEO/COO author + publish the business/medical config. Obligations, SOP tasks & adherence gaps all flow from published rules.",
			"security.warning":                                          "Real business/medical config — not public. Only CEO/COO publish · Directors draft/propose if granted the capability · field / verifier / park never see raw config (they get generated obligations + SOP tasks only).",
			"section.rules.title":                                       "Protocol rules",
			"section.rules.aria":                                        "Protocol rules",
			"section.rules.note":                                        "scoped config list",
			"section.process_map.title":                                 "How this rule maps to the process",
			"drawer.record.aria":                                        "Protocol rule record",
			"drawer.record.close_label":                                 "Close protocol rule record",
			"drawer.record.eyebrow":                                     "PROTOCOL RULE",
			"drawer.record.note":                                        "Read-only authority record. Editing and publishing happen through the governed draft flow; generated obligations, SOP tasks, and adherence rows come only from published versions.",
			"drawer.record.version":                                     "Version",
			"drawer.record.status":                                      "Status",
			"drawer.record.scope":                                       "Scope",
			"drawer.record.effective":                                   "Effective",
			"drawer.record.linked_sop":                                  "Linked SOP",
			"drawer.record.publisher":                                   "Publisher",
			"drawer.record.row_version":                                 "Row version",
			"drawer.record.version_id":                                  "Version ID",
			"drawer.record.rule_dsl":                                    "Stored rule_dsl",
			"drawer.record.proof_policy":                                "Proof policy",
			"drawer.record.detail_unavailable":                          "Could not load protocol version detail.",
			"action.open_protocol_record":                               "Open protocol rule record",
			"filter.search_label":                                       "Search protocol rules",
			"filter.search_placeholder":                                 "Search rules...",
			"action.new_draft_rule":                                     "New draft rule",
			"action.cancel":                                             "Cancel",
			"action.save_draft":                                         "Save draft",
			"action.re_save_draft":                                      "Re-save draft",
			"action.saving":                                             "Saving...",
			"action.preview_impact":                                     "Preview impact",
			"action.computing":                                          "Computing...",
			"action.publish":                                            "Publish",
			"action.publish_ceo":                                        "Publish (CEO/COO)",
			"action.dry_run":                                            "Dry-run",
			"action.open_adherence":                                     "Protocol Adherence",
			"empty.rules":                                               "No protocol rules yet — the engine stands up empty. Author one with New draft rule; complete versions can publish and generate obligations.",
			"empty.rules_search":                                        "No protocol rules match this search.",
			"error.rules_load":                                          "Could not load protocol rules from the backend. This is a real error, not an empty config — fix the API/connection and reload rather than treating the table as empty.",
			"error.stages_load":                                         "Could not load animal stages from the backend. This is a real error, not no stages seeded — authoring is blocked until the stage reference read succeeds, so an outage is never mistaken for missing config. Fix the API/connection and reload.",
			"error.sops_load":                                           "Could not load SOP versions from the backend. This is a real error, not no published SOP version — authoring is blocked until the SOP read succeeds, so an outage is never mistaken for an empty SOP Library. Fix the API/connection and reload.",
			"pager.rules_noun":                                          "rules",
			"process_map.text":                                          "published rule → obligations (per goat / dose) → pen-drive SOP task → proof + verify → adherence gap",
			"process_map.note":                                          "After publish, every obligation, SOP task, and adherence gap is generated from this config. Review the effect in",
			"breadcrumb.config_rule_editor":                             "Config breadcrumb",
			"modal.rule_editor.aria":                                    "Protocol rule editor",
			"modal.rule_editor.title":                                   "New draft rule",
			"modal.rule_editor.subtitle":                                "stored as rule_dsl (JSONB · JSON-Schema-validated · not YAML) · category changes fields and payload",
			"modal.rule_editor.close":                                   "Close",
			"modal.rule_editor.save_first":                              "Save the draft first",
			"modal.rule_editor.preview_title":                           "Run server-side preview validation",
			"modal.rule_editor.publish_title":                           "Publish immutable version",
			"modal.rule_editor.resolve_title":                           "Resolve validation issues before publishing",
			"modal.rule_editor.notice_ok":                               "ok",
			"modal.rule_editor.stages_error_prefix":                     "Animal stages failed to load",
			"modal.rule_editor.stages_error_body":                       "This is a real backend error, not no stages seeded — the stage picker is disabled and Save/Publish are blocked until the stage reference read succeeds. Fix the API/connection and reopen.",
			"modal.rule_editor.sops_error_prefix":                       "SOP versions failed to load",
			"modal.rule_editor.sops_error_body":                         "This is a real backend error, not no published SOP version — the SOP picker is disabled and Save/Publish are blocked until the SOP read succeeds. Fix the API/connection and reopen.",
			"modal.rule_editor.categories_empty":                        "No protocol categories are configured yet — seed protocol_definitions before authoring a draft rule.",
			"modal.rule_editor.no_stages_reason":                        "No animal stages configured — Data Ops must seed animal_stage_lookup to target a stage band",
			"modal.rule_editor.no_sop_labels_reason":                    "No published SOP labels are available for the display-only schedule label. Publish a SOP in the SOP Library first.",
			"modal.rule_editor.stage_load_block_prefix":                 "Stage reference data failed to load",
			"modal.rule_editor.sop_load_block_prefix":                   "SOP reference data failed to load",
			"modal.rule_editor.category_seed_block_prefix":              "Protocol categories are not seeded",
			"modal.rule_editor.fix_reload_suffix":                       "fix the API and reload before authoring",
			"modal.rule_editor.only_ceo_publish":                        "Only CEO/COO can publish",
			"modal.rule_editor.dirty_publish":                           "Inputs changed since the last save — save the draft again before publishing",
			"modal.rule_editor.select_sop_publish":                      "Select an executable SOP version before publishing",
			"modal.rule_editor.proof_publish":                           "Add at least one proof token before publishing (an empty proof policy is not publishable)",
			"modal.rule_editor.default_vaccination_escalation":          "miss -> Asst -> Park Head -> Preventive Care (PC) Director; overdue -> escalate",
			"modal.rule_editor.default_vaccine_lot_policy":              "FEFO lot required; cold-chain and expiry checked before verification",
			"modal.rule_editor.default_dose_primary":                    "primary",
			"modal.rule_editor.default_dose_prefix":                     "dose_",
			"modal.rule_editor.default_repeat_until":                    "-",
			"modal.rule_editor.default_proof_policy":                    "shed,vial,dose,lot,qty",
			"modal.rule_editor.field.category":                          "Module / category",
			"modal.rule_editor.option.no_categories":                    "no protocol categories seeded",
			"modal.rule_editor.field.protocol":                          "Protocol code · name",
			"modal.rule_editor.field.scope":                             "Scope · effective from",
			"modal.rule_editor.field.sop_version":                       "Executable SOP version (required to publish)",
			"modal.rule_editor.option.sops_failed":                      "SOP versions failed to load — fix the API and reload",
			"modal.rule_editor.option.no_sop":                           "no published SOP version — publish a SOP in the SOP Library first",
			"modal.rule_editor.option.select_sop":                       "select a published SOP version...",
			"modal.rule_editor.hint.sop_version":                        "Binds the obligation/SOP-task execution form. Publish requires a real published SOP version + a non-empty proof policy (derived from the proof tokens below).",
			"modal.rule_editor.field.escalation":                        "Escalation policy",
			"modal.rule_editor.field.vaccine_matrix":                    "Vaccine matrix row",
			"modal.rule_editor.field.selected_matrix_row_details":       "Selected row details",
			"modal.rule_editor.field.shared_eligibility_policy":         "Shared eligibility policy",
			"modal.rule_editor.field.vaccine_pathogen_class":            "Pathogen class",
			"modal.rule_editor.field.vaccine_course_type":               "Course type",
			"modal.rule_editor.field.compatibility_policy":              "Cross-vaccine spacing policy",
			"modal.rule_editor.field.live_to_killed_gap":                "live -> killed gap days",
			"modal.rule_editor.field.killed_to_killed_gap":              "killed -> killed gap days",
			"modal.rule_editor.field.live_to_live_gap":                  "live -> live gap days",
			"modal.rule_editor.field.kid_booster_min_gap":               "course booster min gap days",
			"modal.rule_editor.field.bacterial_viral_same_day":          "bacterial + viral same day",
			"modal.rule_editor.field.live_killed_viral_same_day":        "live viral + killed viral same day",
			"modal.rule_editor.field.procurement_policy":                "Procurement / prior vaccination policy",
			"modal.rule_editor.field.warmup_no_vaccination_days":        "warm-up hold days",
			"modal.rule_editor.field.kids_normal_schedule_until_weeks":  "kids normal schedule until weeks",
			"modal.rule_editor.field.adult_prior_vaccination_allowed":   "adult prior vaccination allowed",
			"modal.rule_editor.field.first_wave":                        "first wave vaccines",
			"modal.rule_editor.field.second_wave_after_days":            "second wave after days",
			"modal.rule_editor.field.goat_second_wave":                  "goat second wave vaccines",
			"modal.rule_editor.field.sheep_second_wave":                 "sheep second wave vaccines",
			"modal.rule_editor.field.pregnancy_policy":                  "Pregnancy / delivery policy",
			"modal.rule_editor.field.allow_until_pregnancy_month":       "allow until pregnancy month",
			"modal.rule_editor.field.skip_from_pregnancy_month":         "skip from pregnancy month",
			"modal.rule_editor.field.skip_through_pregnancy_month":      "skip through pregnancy month",
			"modal.rule_editor.field.post_delivery_catch_up_days":       "post-delivery catch-up days",
			"modal.rule_editor.placeholder.vaccine_code":                "vaccine code (e.g. ET)",
			"modal.rule_editor.placeholder.vaccine_name":                "vaccine name",
			"modal.rule_editor.placeholder.vaccine_inventory_item":      "inventory item id (optional)",
			"modal.rule_editor.placeholder.vaccine_manufacturer":        "manufacturer / detail",
			"modal.rule_editor.placeholder.vaccine_disease":             "disease / protection target",
			"modal.rule_editor.placeholder.vaccine_compatibility_group": "compatibility group / family",
			"modal.rule_editor.matrix_grid_title":                       "Vaccination matrix rows",
			"modal.rule_editor.action.add_matrix_row":                   "Add matrix row",
			"modal.rule_editor.action.copy_selected_matrix_row":         "Copy selected row",
			"modal.rule_editor.action.load_source_vaccine_matrix":       "Load vaccine matrix",
			"modal.rule_editor.action.apply_matrix_schedule":            "Apply matrix schedule",
			"modal.rule_editor.action.remove_matrix_row":                "Remove matrix row",
			"modal.rule_editor.title.add_blank_matrix_row":              "Add a blank matrix row; schedule note and dose rows stay empty until you choose or copy a matrix preset",
			"modal.rule_editor.title.copy_selected_matrix_row":          "Create an intentional copy of the selected row, including schedule note and dose rows",
			"modal.rule_editor.title.keep_one_matrix_row":               "At least one matrix row is required",
			"modal.rule_editor.field.vaccination_eligibility":           "Vaccination eligibility - animal / pen stage + sex",
			"modal.rule_editor.option.all_stages":                       "all (every stage)",
			"modal.rule_editor.hint.no_stages":                          "Stage bands (for example K1, K2) are backend reference data from animal_stage_lookup — until they are seeded you can only author an all-stages rule, not target a specific band.",
			"modal.rule_editor.field.lifecycle":                         "Lifecycle / reproductive",
			"modal.rule_editor.field.defer":                             "Defer when (obligation marked deferred + event; not hidden)",
			"modal.rule_editor.field.missed_dose":                       "Booster / catch-up / missed-dose policy",
			"modal.rule_editor.field.vaccine_lot":                       "Stock / vaccine lot requirements",
			"modal.rule_editor.label.rule_dsl":                          "Stored as rule_dsl (JSONB)",
			"modal.rule_editor.rule_dsl_aria":                           "rule_dsl preview",
			"modal.rule_editor.note.vaccination_dsl":                    "schedule = ARRAY of dose rows (multi-dose / multi-phase). Canonical keys: repeat_until_after_age, proof_policy, schedule_note. Next due from last accepted completion (not DOB) when prior history exists. JSON-Schema-validated · versioned · not YAML.",
			"modal.rule_editor.note.feed_dsl":                           "feed_direction stores source tables, parameter families, dimension keys, validation policy, calculation preview outputs, session slots, proof policy, and inventory policy. Numbers such as 80/20 are examples until reviewed and published.",
			"modal.rule_editor.table.quantity":                          "Quantity",
			"modal.rule_editor.table.proof":                             "Proof",
			"modal.rule_editor.table.schedule_title":                    "Schedule builder - dose / phase rows",
			"modal.rule_editor.action.add_dose":                         "Add dose row",
			"modal.rule_editor.table.dose":                              "Dose / sequence",
			"modal.rule_editor.table.trigger":                           "Trigger",
			"modal.rule_editor.table.offset":                            "Offset (d)",
			"modal.rule_editor.table.window":                            "Window (d)",
			"modal.rule_editor.table.dose_amount":                       "Dose amount",
			"modal.rule_editor.table.dose_unit":                         "Unit",
			"modal.rule_editor.table.route_site":                        "Route / site",
			"modal.rule_editor.table.max_delay":                         "Max delay (d)",
			"modal.rule_editor.table.course_lapse":                      "Course lapse",
			"modal.rule_editor.table.repeat":                            "Repeat",
			"modal.rule_editor.table.repeat_until":                      "repeat_until_after_age",
			"modal.rule_editor.table.min_gap":                           "Min gap (d)",
			"modal.rule_editor.table.catch_up":                          "Catch-up",
			"modal.rule_editor.table.sop_label":                         "SOP label",
			"modal.rule_editor.table.no_sop_label":                      "no SOP label",
			"modal.rule_editor.table.proof_policy":                      "Proof required",
			"modal.rule_editor.table.sop_label_title":                   "Display label only — the executable SOP binds at version level (the picker above), not per dose",
			"modal.rule_editor.table.row":                               "Row",
			"modal.rule_editor.table.vaccine_code":                      "Vaccine",
			"modal.rule_editor.table.vaccine_name":                      "Name",
			"modal.rule_editor.table.course_type":                       "Course",
			"modal.rule_editor.table.schedule_note":                     "Schedule note",
			"modal.rule_editor.table.schedule_note_derived_badge":       "matrix",
			"modal.rule_editor.table.schedule_note_derived_title":       "Read-only summary from the row's dose schedule; edit Schedule note in the Schedule builder below",
			"modal.rule_editor.table.no_schedule_note":                  "No schedule note - edit selected-row dose rows below",
			"modal.rule_editor.table.vial_doses":                        "Vial",
			"modal.rule_editor.table.revaccination":                     "Revaccination (d)",
			"modal.rule_editor.table.stage":                             "Stage",
			"modal.rule_editor.table.species":                           "Species",
			"modal.rule_editor.table.sex":                               "Sex",
			"modal.rule_editor.table.breed":                             "Breed",
			"modal.rule_editor.guided_title":                            "Vaccination plan",
			"modal.rule_editor.guided_subtitle":                         "One active version holds the full company or park vaccination plan. CEO/COO controls vaccine timing, tags, proof, and publish.",
			"modal.rule_editor.guided.who_when_title":                   "Who & when",
			"modal.rule_editor.guided.applies_to":                       "Applies to",
			"modal.rule_editor.guided.animals":                          "Animals",
			"modal.rule_editor.guided.whole_company":                    "Whole company",
			"modal.rule_editor.guided.one_park":                         "One park",
			"modal.rule_editor.guided.goats_sheep":                      "Goats + sheep",
			"modal.rule_editor.guided.goats":                            "Goats",
			"modal.rule_editor.guided.sheep":                            "Sheep",
			"modal.rule_editor.guided.draft":                            "DRAFT",
			"modal.rule_editor.guided.publishable":                      "Ready to publish after save",
			"modal.rule_editor.guided.not_publishable":                  "Not publishable yet",
			"modal.rule_editor.guided.publish_hint":                     "Draft only until a published SOP is selected and saved. Publishing activates one complete vaccination plan.",
			"modal.rule_editor.guided.plan_title":                       "Recommended vaccination plan",
			"modal.rule_editor.guided.plan_hint":                        "Toggle vaccines on/off. Click a vaccine to edit timing, dose, tag, species, and proof. Safety rules below are automatic.",
			"modal.rule_editor.guided.plan_name":                        "Vaccination plan name",
			"modal.rule_editor.guided.default_plan_name":                "Preventive Care vaccination plan",
			"modal.rule_editor.guided.info_label":                       "Info",
			"modal.rule_editor.guided.close_information":                "Close information",
			"modal.rule_editor.guided.one_active_matrix_title":          "One active plan",
			"modal.rule_editor.guided.one_active_matrix_body":           "This page saves one complete Preventive Care vaccination plan. Individual vaccine timing lives inside it, and a park or company has one active version at a time.",
			"modal.rule_editor.guided.company_park_title":               "Company vs park",
			"modal.rule_editor.guided.company_park_body":                "Company-wide is the default plan. A park-wise plan overrides it only for that park while it is active.",
			"modal.rule_editor.guided.company_default_scope":            "Company-wide default",
			"modal.rule_editor.guided.pick_effective_date":              "Pick effective date",
			"modal.rule_editor.guided.today":                            "Today",
			"modal.rule_editor.guided.tomorrow":                         "Tomorrow",
			"modal.rule_editor.guided.edit_timing":                      "Edit timing",
			"modal.rule_editor.guided.recommended_plan_info_title":      "Recommended plan",
			"modal.rule_editor.guided.recommended_plan_info_body":       "These vaccine cards come from the approved Vaccination Rules. The cards are clickable only to edit timing and animal selectors for that vaccine; turning a card off removes that vaccine from this draft.",
			"modal.rule_editor.guided.load_plan":                        "Load approved vaccine plan",
			"modal.rule_editor.guided.empty_title":                      "Load the approved vaccine plan",
			"modal.rule_editor.guided.empty_body":                       "This fills ET+TT, PPR, Goat Pox, FMD, HS, Sheep Pox, and Blue Tongue from the current Vaccination Rules.",
			"modal.rule_editor.guided.on":                               "on",
			"modal.rule_editor.guided.included":                         "Included",
			"modal.rule_editor.guided.skipped":                          "Skipped",
			"modal.rule_editor.guided.selected_timing":                  "Timing for selected vaccine",
			"modal.rule_editor.guided.age_course":                       "Age course",
			"modal.rule_editor.guided.procurement_course":               "Procurement / adult course",
			"modal.rule_editor.guided.vaccine_facts":                    "Vaccine facts",
			"modal.rule_editor.guided.fact_type":                        "Type",
			"modal.rule_editor.guided.fact_species":                     "Species",
			"modal.rule_editor.guided.fact_dose":                        "Dose",
			"modal.rule_editor.guided.fact_vial":                        "Vial",
			"modal.rule_editor.guided.fact_revaccination":               "Revaccination",
			"modal.rule_editor.guided.fact_priority":                    "Priority",
			"modal.rule_editor.guided.no_selected":                      "Select a vaccine to edit its timing.",
			"modal.rule_editor.guided.who_qualifies":                    "Who qualifies",
			"modal.rule_editor.guided.qualifies_hint":                   "Tags/stages come from the pen/tag lookup. Species, sex, and breed stay as selectors so this one plan can cover all valid animal groups.",
			"modal.rule_editor.guided.who_qualifies_info_title":         "Who qualifies",
			"modal.rule_editor.guided.who_qualifies_info_body":          "A vaccine row targets species, pen/tag stage, sex, and breed. Safety rules still run after this selector, so sick, ICU, quarantine, and pregnancy-month blocks are automatic.",
			"modal.rule_editor.guided.set_below":                        "Set below",
			"modal.rule_editor.guided.safety_title":                     "Automatic safety rules",
			"modal.rule_editor.guided.read_only":                        "read-only",
			"modal.rule_editor.guided.safety_hint":                      "These come from Vaccination Rules and are not tweakable here.",
			"modal.rule_editor.guided.safety_max_shots":                 "Max 3 vaccines per animal per doctor visit.",
			"modal.rule_editor.guided.safety_live_live":                 "Live-to-live minimum gap is 28 days.",
			"modal.rule_editor.guided.safety_killed_live":               "Live/killed and killed/killed spacing is 14 days unless same-day class rule permits it.",
			"modal.rule_editor.guided.safety_pregnancy":                 "Pregnancy months 4 and 5 skip vaccination; catch up within 14 days after delivery.",
			"modal.rule_editor.guided.safety_defer":                     "Sick, under treatment, ICU, quarantine, warm-up, and late pregnancy are deferred.",
			"modal.rule_editor.guided.safety_mother":                    "Mother vaccinated/unknown category is ignored. It is never a rule selector.",
			"modal.rule_editor.guided.safety_batch":                     "Drive batching may wait up to 7 days once; safety window beats batching.",
			"modal.rule_editor.guided.procurement_title":                "Procurement holding",
			"modal.rule_editor.guided.procurement_hint":                 "Trusted history only means vaccines given by us in our parks or supervised procurement holding parks under SOP/video/physical validation.",
			"modal.rule_editor.guided.procurement_info_title":           "Procurement holding",
			"modal.rule_editor.guided.procurement_info_body":            "Only vaccines given by us in our parks or supervised procurement holding parks count as trusted history. Other outside claims start the normal schedule after the animal reaches our pens.",
			"modal.rule_editor.guided.procurement_second_visit_note":    "Pox/live second visit happens after the live-to-live spacing window; ET+TT dose 2 uses the 21-day course gap on the ET+TT matrix row.",
			"modal.rule_editor.guided.proof_title":                      "Proof required",
			"modal.rule_editor.guided.proof_hint":                       "Choose the proof fields doctors must capture during vaccination. Publish stays blocked if proof is empty.",
			"modal.rule_editor.guided.proof_info_title":                 "Proof required",
			"modal.rule_editor.guided.proof_info_body":                  "These chips become the proof checklist for every selected dose row. Publish is blocked if proof is empty; operators later submit these fields during vaccination execution.",
			"modal.rule_editor.guided.selected_proof_tokens_aria":       "Selected proof tokens",
			"modal.rule_editor.guided.advanced_title":                   "Advanced database preview",
			"modal.rule_editor.guided.advanced_hint":                    "This is the exact JSON saved to the backend for the single active vaccination plan version.",
			"modal.rule_editor.guided.schedule_editor":                  "Edit exact dose rows",
			"modal.rule_editor.guided.selected_combo_label":             "Selected combo",
			"modal.rule_editor.guided.dose_rows_count":                  "dose rows",
			"modal.rule_editor.guided.schedule_hint":                    "These dose rows belong only to the selected vaccine/stage/species/sex/breed row. Pick another row in the plan list to edit that combo. Medical safety rules still apply automatically.",
			"modal.rule_editor.guided.vaccine_lot_policy_label":         "Vaccine lot policy",
			"modal.rule_editor.guided.no_active_vaccine":                "Turn on at least one vaccine before saving the plan.",
			"modal.rule_editor.action.remove_dose":                      "Remove dose",
			"modal.rule_editor.default_feed_escalation":                 "miss -> Feed Director -> Park Head -> COO; wrong template data blocks generation",
			"modal.rule_editor.default_feed_quantity":                   "0",
			"modal.rule_editor.placeholder.session_timing":              "09:00,15:00",
			"modal.rule_editor.placeholder.session_weights":             "50,50",
			"modal.rule_editor.placeholder.packing_proof":               "pack_qty,feed_item,lot,video",
			"modal.rule_editor.placeholder.execution_proof":             "distribution_video,consumed_qty,wastage_qty,water_check",
			"modal.rule_editor.field.feed_stage":                        "Feed cohort resolver - stage/tag + breed class",
			"modal.rule_editor.field.feed_template":                     "Source parameter template",
			"modal.rule_editor.field.feed_dimensions":                   "Runtime dimensions",
			"modal.rule_editor.field.ration":                            "Ration row / threshold sample",
			"modal.rule_editor.field.session_timing":                    "Serving slots and split weights",
			"modal.rule_editor.field.session_weights":                   "Slot weights",
			"modal.rule_editor.field.proof":                             "Packing + execution proof tokens",
			"modal.rule_editor.field.inventory":                         "Inventory reserve / consume policy",
			"modal.rule_editor.field.validation":                        "Validation gates and calculation outputs",
			"modal.rule_editor.field.ratio_policy":                      "Ratio / quantity policy",
			"modal.rule_editor.table.feed_title":                        "Feed parameter template preview",
			"modal.rule_editor.table.session":                           "Session",
			"modal.rule_editor.table.feed_item":                         "Feed item / row",
			"modal.rule_editor.table.inventory":                         "Inventory",
			"modal.rule_editor.table.weight":                            "Weight",
			"modal.rule_editor.table.packing_execution_proof":           "packing + execution",
			"modal.rule_editor.impact_title_feed":                       "Validation + calculation preview",
			"modal.rule_editor.preview_feed_pending":                    "Feed calculation preview endpoint is not wired yet - Save only drafts reviewed template policy.",
			"modal.rule_editor.preview_empty_feed":                      "Before publish, run import validation against the source tables, block bad rows, then preview ration, session, packing, transport, consumption, and reconciliation outputs.",
			"feed_config.section.title":                                 "Feed Direction parameter templates",
			"feed_config.section.note":                                  "Feed Direction Config is reopened here only as source-backed parameter/template authoring. Full Feed operational screens stay separate.",
			"feed_config.alert":                                         "80/20, 400-500g, 600g, F1/F2 ranges, pregnant windows, and 90-95% checks are source examples. Runtime values must come from reviewed template rows, validation, calculation preview, and an effective-dated published protocol.",
			"feed_config.kpi.sources":                                   "Source tables",
			"feed_config.kpi.families":                                  "Parameter families",
			"feed_config.kpi.dimensions":                                "Runtime dimensions",
			"feed_config.kpi.validations":                               "Validation gates",
			"feed_config.table.source":                                  "Source evidence",
			"feed_config.table.family":                                  "Runtime family",
			"feed_config.table.validation":                              "Validation gate",
			"feed_config.table.output":                                  "Calculation output",
			"feed_config.action.preview":                                "Run feed calculation preview",
			"feed_config.action.preview_disabled":                       "Feed import/solver preview endpoint is not built yet; the UI shows the required contract shape and blocks publish until preview support lands.",
			"modal.rule_editor.impact_title_vaccination":                "Impact preview - selected dose row",
			"modal.rule_editor.kpi.eligible_animals":                    "Eligible animals",
			"modal.rule_editor.kpi.vaccination_cells":                   "Dose rows",
			"modal.rule_editor.kpi.affected_sheds":                      "Affected pens",
			"modal.rule_editor.kpi.estimated_days":                      "Estimated days",
			"modal.rule_editor.label.daily_cap_suffix":                  "animals/operator/day cap",
			"modal.rule_editor.label.doses_available":                   "doses available",
			"modal.rule_editor.label.earliest_expiry":                   "earliest expiry",
			"modal.rule_editor.preview_empty_vaccination":               "Run a preview before publishing. It is read-only and does not create obligations, batches, notifications, or stock movements.",
			"modal.rule_editor.impact_scope_label":                      "Preview scope",
			"modal.rule_editor.impact_stock_set":                        "stock item set",
			"modal.rule_editor.impact_stock_missing":                    "no stock item",
			"modal.rule_editor.impact_plan_title":                       "Operator-day estimate",
			"modal.rule_editor.impact_plan_date":                        "Date",
			"modal.rule_editor.impact_plan_vaccinations":                "Animals",
			"modal.rule_editor.impact_plan_daily_limit":                 "Daily limit",
			"modal.rule_editor.impact_plan_capacity":                    "Capacity",
			"modal.rule_editor.impact_method_title":                     "How the numbers are calculated",
			"modal.rule_editor.impact_method_body":                      "Read-only aggregate estimate for the selected combo, read from the precomputed vaccination eligibility rollup (never a live goat scan). Eligible animals = usable in-care animals matching species, stage, sex, breed, health, and park scope. Dose rows show vaccine work volume for stock planning. Operator capacity uses eligible animals per available operator per day, so one animal with multiple same-day vaccines still consumes one operator slot. Stock appears only when the row has a vaccine inventory item.",
			"modal.rule_editor.impact_scale_note":                       "Even across a 5,000-50,000-animal herd, this panel reads a precomputed eligibility rollup and does not scan goats or load them into the browser. It is a quick planning estimate; per-animal operator, date and pen assignment happens after publish.",
			"modal.rule_editor.label.draft_saved":                       "draft saved",
			"modal.rule_editor.message.preview_failed":                  "preview failed",
			"modal.rule_editor.label.rule_singular":                     "rule",
			"modal.rule_editor.label.rule_plural":                       "rules",
			"modal.rule_editor.label.tenant":                            "tenant",
			"modal.rule_editor.label.park_scope_prefix":                 "park:",
		}
	case "people":
		// Backend-owned copy for the People/HRMS directory + Add Person drawer.
		// The client renders these verbatim; per the golden rule it must not
		// hardcode a label, an empty state, or a disabled reason of its own.
		return map[string]string{
			"crumb":                     "Admin / Data Ops",
			"section.people.title":      "All people",
			"section.people.aria":       "Farm staff directory",
			"section.people.row_hint":   "everyone with a login or roster entry",
			"filter.search_label":       "Search staff",
			"filter.search_placeholder": "Name or email...",
			"filter.park":               "Park",
			"filter.department":         "Department",
			"filter.status":             "Status",
			"filter.all":                "All",
			"column.display_name":       "Person",
			"column.park":               "Park",
			"column.department":         "Department",
			"column.designation":        "Designation",
			"column.title":              "Title",
			"title.placeholder":         "e.g. CEO, Preventive Care Director",
			"title.hint":                "Shown in the Tasks picker instead of the name",
			"action.save_title":         "Save",
			"column.email":              "Email",
			"column.status":             "Status",
			"action.add_person":         "Add person",
			"action.save":               "Create person",
			"action.saving":             "Creating...",
			"action.cancel":             "Cancel",
			"action.close":              "Close",
			"action.next_page":          "Next",
			"action.prev_page":          "Back",
			"pager.page":                "Page",
			// Write-feedback copy. actionFeedbackCopy resolves the action_key
			// straight through copy(), which THROWS on a missing key — every key
			// an action can redirect with must exist here.
			"action.person_created":          "Person added. They can sign in with their email now.",
			"action.person_created_existing": "Person added. This email already had a login — its password is unchanged.",
			"action.person_create_failed":    "Could not add this person. Check the fields and try again.",
			"action.person_duplicate":        "A person with this email already exists.",
			"action.identity_unavailable":    "The login account service is unavailable right now. Nothing was created — try again.",
			"action.error_form":              "Could not complete that action.",
			"drawer.add.title":               "Add person",
			"drawer.add.subtitle":            "Creates their login account, park access, and roster entry in one step.",
			"field.first_name":               "First name",
			"field.last_name":                "Last name",
			"field.email":                    "Email",
			"field.role":                     "Role",
			"field.park":                     "Park",
			"field.department":               "Department",
			"field.designation":              "Designation grade",
			"field.optional":                 "optional",
			"required.hint":                  "First name, email, and role are required. Operators and park heads also need their park.",
			"password.note":                  "New logins use the standing password pattern: first name (capitalized) followed by @2026. Example: Amit@2026.",
			"value.none":                     "—",
			"stats.title":                    "Proof work",
			"stats.uploaded":                 "Videos uploaded",
			"stats.approved":                 "Approved",
			"stats.rejected":                 "Rejected",
			"stats.pending":                  "Awaiting review",
			// RANDOMIZATION (maintainer decision 2026-08-26): proofs the sampling policy settled
			// without a person. Worded as a fact about the POLICY, never about the operator -- at a
			// 40% share most of a good operator's proofs land here.
			"stats.not_reviewed":   "Not selected for review",
			"stats.rejection_rate": "Rejection rate",
			// Clock In / Out tab (maintainer decisions 2026-08-27/28). The chip
			// templates keep composition backend-owned: the client substitutes
			// the backend-composed time label into %s and nothing else.
			"clock.tab.title":                     "Clock In / Out",
			"clock.summary.working":               "Working now",
			"clock.summary.worked":                "Worked",
			"clock.summary.clocked_out":           "Clocked out",
			"clock.summary.not_clocked_in":        "Not clocked in",
			"clock.summary.flagged":               "Flagged",
			"clock.column.person":                 "Person",
			"clock.column.park":                   "Park",
			"clock.column.designation":            "Designation",
			"clock.column.clock_in":               "Clock in",
			"clock.column.clock_out":              "Clock out",
			"clock.column.hours":                  "Hours",
			"clock.column.location":               "Location",
			"clock.column.device":                 "Device",
			"clock.column.flags":                  "Flags",
			"clock.filter.date":                   "Date",
			"clock.filter.bucket":                 "Status",
			"clock.filter.bucket.all":             "All",
			"clock.filter.bucket.working":         "Working now",
			"clock.filter.bucket.clocked_out":     "Clocked out",
			"clock.filter.bucket.not_clocked_in":  "Not clocked in",
			"clock.filter.bucket.flagged":         "Flagged",
			"clock.empty":                         "Nobody matches this filter.",
			"clock.value.none":                    "—",
			"clock.chip.clocked_in":               "In %s",
			"clock.chip.not_clocked_in":           "Not clocked in",
			"clock.column.clock_in_today":         "Clocked in",
			"clock.drawer.title":                  "Clocking detail",
			"clock.drawer.punches":                "Punches",
			"clock.drawer.captured_at":            "Device time",
			"clock.drawer.recorded_at":            "Server time",
			"clock.drawer.skew":                   "Clock difference",
			"clock.drawer.location":               "Location",
			"clock.drawer.coordinates":            "Coordinates",
			"clock.drawer.accuracy":               "GPS accuracy",
			"clock.drawer.map_link":               "Open map",
			"clock.drawer.device":                 "Device",
			"clock.drawer.app_version":            "App version",
			"clock.drawer.os":                     "Android version",
			"clock.drawer.network":                "Connection",
			"clock.drawer.network.online":         "Online",
			"clock.drawer.network.offline_queued": "Recorded offline",
			"clock.drawer.battery":                "Battery",
			"clock.drawer.event.clock_in":         "Clock in",
			"clock.drawer.event.clock_out":        "Clock out",
			"clock.drawer.no_location":            "No location captured",
			"clock.tab.locked":                    "Clock oversight is limited to leadership.",
			"stats.none":                          "No proof videos submitted yet.",
			"stats.no_reviews":                    "No reviews yet",
			"action.deactivate":                   "Deactivate",
			"action.activate":                     "Activate",
			"action.confirm":                      "Yes, continue",
			"confirm.deactivate.title":            "Deactivate this person?",
			"confirm.deactivate.body":             "They stay in the directory as inactive and can no longer be assigned work. Their sign-in access is removed separately.",
			"confirm.activate.title":              "Activate this person?",
			"confirm.activate.body":               "They return to the active directory and can be assigned work again.",
			"action.person_deactivated":           "Person deactivated.",
			"action.person_activated":             "Person activated.",
			"action.person_status_failed":         "Could not change this person's status. Reload and try again.",
			"action.person_title_saved":           "Title saved.",
			"action.person_title_failed":          "Could not save the title. Reload and try again.",
			"empty.people":                        "No people match these filters.",
			"empty.people.unset":                  "No people yet. Add the first person to start the directory.",
			"summary.count":                       "people",
			"error.load":                          "Could not load the staff directory. Refresh to try again.",
			"disabled.write":                      "Your current role can view people but not add them.",
			"tab.disabled_reason":                 "This staffing view is coming soon.",

			// The per-person ACCESS editor (maintainer decision 2026-08-24). Module and
			// capability names are NOT here: those come from the access endpoint's own
			// payload, beside the permissions they describe. This is the screen's chrome.
			"access.open":             "Access",
			"access.open_hint":        "What this person can see and do",
			"access.title":            "Access",
			"access.designation":      "Start from",
			"access.designation.none": "Set by hand",
			"access.designation.hint": "Pre-fills the ticks below. You can change any of them afterwards.",
			"access.scope":            "Covers",
			"access.scope.tenant":     "Every park",
			// Home park: the one park per-park modules (vaccination drives) assign this
			// person's work in. Asked only when more than one park is ticked; a single
			// ticked park is the home park.
			"access.home_park":      "Home park",
			"access.home_park.hint": "Where their drives and daily tasks are assigned. Choose one of the parks they cover.",
			"access.home_park.none": "Choose a park",
			"access.column.module":  "Module",
			"access.column.web":     "Web console",
			"access.column.mobile":  "Phone",
			// Page ticks. The label names SCREENS rather than pages, because "page" is
			// a web word and this list is read by someone deciding what a colleague
			// opens on a Monday morning.
			"access.pages.label":        "Screens they can open",
			"access.unavailable.web":    "Not on the web console",
			"access.unavailable.mobile": "Not on the phone",
			"access.summary.none":       "No modules yet",
			"access.summary.count":      "of {total} modules",
			"access.warning.title":      "Worth a second look",
			"access.action.save":        "Save access",
			"access.action.saving":      "Saving...",
			"access.loading":            "Loading this person's access...",
			"access.error.load":         "Their access could not be loaded. Close this and try again.",
			"access.error.defaults":     "Those defaults could not be loaded. Set the access by hand, or try again.",
			"access.error.save":         "That could not be saved. Reload the page to see the current settings, then try again.",
			"disabled.access_write":     "Your current role can view access but not change it.",
			// Notifications tab (maintainer decision 2026-09-08). "Desk" and "job title" are
			// the farm words for a designation; the raw code (pc_director) is never shown.
			"notifications.intro":              "Tick the job titles that should receive each alert. Everyone holding that title on an active login is told; nobody is named individually.",
			"notifications.column.alert":       "Alert",
			"notifications.column.setting":     "Setting",
			"notifications.chip.default":       "Default",
			"notifications.chip.custom":        "Customised",
			"notifications.chip.nobody":        "Nobody receives this",
			"notifications.action.save":        "Save",
			"notifications.action.saving":      "Saving...",
			"notifications.action.use_default": "Use default",
			"notifications.action.reset_hint":  "Back to the standard audience for this alert.",
			"notifications.always_told":        "The person doing the work is always told; these ticks only decide who else hears about it.",
			"notifications.saved":              "Saved. New alerts go to the ticked job titles from now on.",
			"notifications.error.load":         "The alert settings could not be loaded. Reload the page and try again.",
			"notifications.error.save":         "That could not be saved. Reload the page to see the current settings, then try again.",
			"notifications.empty":              "No alerts are configurable yet.",
			"disabled.notifications_write":     "Your current role can see who receives each alert but not change it.",
		}
	// Vaccination is deliberately absent: its SOP page is gone, and its content lives on
	// the vaccination plan console. milk and weighing arrived on main meanwhile and stay.
	case "counts-sops", "feed-sops", "milk-sops", "weighing-sops", "procurement-sops", "configuration-work-instructions", "sales-sops", "pc-care-sops":
		m := map[string]string{
			"filter.search_label":                     "Search SOPs",
			"filter.search_placeholder":               "Search SOP name, trigger, step, or proof...",
			"filter.domain.aria":                      "SOP domains",
			"action.new_sop":                          "New SOP",
			"action.cancel":                           "Cancel",
			"action.publish":                          "Publish",
			"action.previous":                         "Previous",
			"action.next":                             "Next",
			"action.close":                            "Close",
			"action.new_sop_builder":                  "New SOP in builder",
			"action.edit_operator_steps":              "Change SOP",
			"action.opening_editor":                   "Opening…",
			"label.operator_steps":                    "operator steps",
			"empty.title":                             "No vaccination SOPs yet",
			"empty.body":                              "Create a vaccination SOP or publish one from a draft to make it available to obligations.",
			"empty.no_match":                          "No SOPs match.",
			"empty.no_published_fields":               "No published version yet — this SOP has no form_dsl fields to show. Open the builder to author a draft version.",
			"auth.sign_in":                            "Sign in with Google to load the SOP Library — the admin SOP engine is tenant-scoped.",
			"status.published":                        "published",
			"status.draft":                            "draft",
			"status.retired":                          "retired",
			"label.steps":                             "steps",
			"label.no_proof_gates":                    "no proof gates",
			"label.no_published_version":              "no published version",
			"label.rows":                              "Rows",
			"label.of":                                "of",
			"label.page":                              "Page",
			"label.domain":                            "Domain",
			"label.trigger":                           "Trigger",
			"label.code":                              "Code",
			"label.version_status":                    "Version · status",
			"label.gates":                             "Gates",
			"label.steps_questions":                   "Steps & questions",
			"label.render_action_center":              "captured on the phone",
			"label.type":                              "type",
			"label.required":                          "required",
			"label.placeholder":                       "—",
			"modal.builder.aria":                      "New SOP form builder",
			"modal.builder.close_label":               "Close",
			"modal.builder.eyebrow":                   "SOP · Preventive Care (PC) / VACCINATION",
			"modal.builder.title":                     "New SOP — form builder",
			"modal.builder.notice_ok":                 "ok",
			"modal.builder.default_name":              "Vaccination session",
			"modal.builder.field.name_domain":         "SOP name & domain",
			"modal.builder.field.name":                "SOP name",
			"modal.builder.placeholder.name":          "Vaccination session",
			"modal.builder.domain_locked":             "domain locked",
			"modal.builder.code_prefix":               "sop_code",
			"modal.builder.policy_label":              "vaccination drive/session policy",
			"modal.builder.field.trigger":             "Trigger — what starts it?",
			"modal.builder.field.steps":               "Steps & questions — add/remove, pick a type, set conditional rules",
			"modal.builder.step_type_aria_prefix":     "Step",
			"modal.builder.step_type_aria_suffix":     "type",
			"modal.builder.step_label_aria_suffix":    "label",
			"modal.builder.placeholder.step":          "question / step text",
			"modal.builder.action.remove_step":        "Remove step",
			"modal.builder.condition.show":            "show:",
			"modal.builder.condition.always":          "always show",
			"modal.builder.condition.only_if_prefix":  "only if step",
			"modal.builder.condition.only_if_suffix":  "answered",
			"modal.builder.condition.on_answer":       "· on answer →",
			"modal.builder.action.add_step":           "Add step / question",
			"modal.builder.logic_title":               "Conditional logic:",
			"modal.builder.logic_body":                "a step can show only if a previous step was answered, and an answer can require this step, require proof, or block submission — emitted as declarative visible_if / required_if / proof_required_if / block_submission_if rules. Cross-domain actions are routed through Action Center ownership and verification.",
			"modal.builder.field.proof_policy":        "Proof policy",
			"modal.builder.label.proof_required":      "proof required",
			"modal.builder.label.verify_before_apply": "verify before apply",
			"modal.builder.field.proof_type":          "Proof type",
			"modal.builder.field.min_count":           "Minimum proof count",
			"modal.builder.field.subject_scope":       "Subject scope",
			"modal.builder.proof_gap":                 "Proof required but no photo/video proof step — add one or turn proof off (backend rejects otherwise).",
			"modal.builder.details_prefix":            "Emits valid form_dsl",
			"modal.builder.details_middle":            "fields, native types) + proof_policy · integration notes",
			"modal.builder.validation.valid":          "form_dsl valid",
			"modal.builder.validation.issues":         "validation issues",
			"modal.builder.label.draft":               "draft",
			"modal.builder.label.dry_run":             "dry-run",
			"modal.builder.label.workflow":            "workflow:",
			"modal.builder.label.final":               "final:",
			"modal.builder.label.blocked":             "blocked",
			"modal.builder.message.dry_run_failed":    "dry-run failed",
			"modal.builder.action.saving":             "Saving...",
			"modal.builder.action.re_save":            "Re-save draft",
			"modal.builder.action.save":               "Save draft",
			"modal.builder.action.dry_run":            "Dry-run",
			"modal.builder.title.save_first":          "Save the draft first",
			"modal.builder.title.preview":             "Run server-side preview validation",
			"modal.builder.title.publish":             "Publish immutable version",
			"modal.builder.title.resolve":             "Resolve validation issues before publishing",
			"modal.detail.aria":                       "SOP detail",
			"modal.detail.close_label":                "Close SOP detail",
			// Full-page SOP builder (Google-Forms / Typeform style, rendered at /sops?compose=1).
			"builder.crumb_current":             "New SOP",
			"builder.crumb_edit":                "Edit SOP",
			"builder.title_edit":                "Edit SOP — form builder",
			"builder.subtitle_edit":             "Editing publishes a NEW version — the current published version keeps running until you publish this one.",
			"builder.edit_blocked":              "This SOP uses rules or field types the form builder can't safely edit yet — saving here would drop them. Edit it in the source SOP tooling instead.",
			"builder.back":                      "Back to SOP Library",
			"builder.subtitle":                  "Build it like a form — add questions, choose a type, set choices and conditional logic. Publishing turns each step into operator tasks.",
			"builder.section.basics":            "SOP basics",
			"builder.section.questions":         "Questions",
			"builder.section.questions_hint":    "Add a question, choose its answer type, and set choices or conditional logic. Use the arrows or drag handle to reorder.",
			"builder.section.gates":             "Gates & proof",
			"builder.section.review":            "Review & publish",
			"builder.add_question":              "Add question",
			"builder.empty_questions":           "No questions yet — add your first question to start building.",
			"builder.question_label":            "Question",
			"builder.field.question_text":       "Question text",
			"builder.placeholder.question":      "Type your question…",
			"builder.field.help_text":           "Help text",
			"builder.placeholder.help_text":     "Add an instruction or hint for the operator (optional)…",
			"builder.field.answer_type":         "Answer type",
			"builder.required":                  "Required",
			"builder.required_hint":             "Operator must answer before submitting.",
			"builder.action.duplicate":          "Duplicate question",
			"builder.action.move_up":            "Move up",
			"builder.action.move_down":          "Move down",
			"builder.action.remove_question":    "Remove question",
			"builder.action.drag":               "Drag to reorder",
			"builder.options.label":             "Choices",
			"builder.options.add":               "Add choice",
			"builder.options.placeholder":       "Choice text",
			"builder.options.remove":            "Remove choice",
			"builder.options.empty":             "Add at least one choice for a select / multi-select question.",
			"builder.options.multi_hint":        "Operators can pick more than one.",
			"builder.options.single_hint":       "Operators pick exactly one.",
			"builder.number.min":                "Min",
			"builder.number.max":                "Max",
			"builder.number.unit":               "Unit",
			"builder.number.unit_placeholder":   "e.g. ml, kg",
			"builder.number.hint":               "Leave blank for no limit.",
			"builder.text.placeholder_label":    "Field placeholder",
			"builder.text.placeholder_hint":     "e.g. Batch lot number",
			"builder.text.long":                 "Long answer (paragraph)",
			"builder.proof.note":                "Operators capture and upload proof here; verification gates completion.",
			"builder.picker.note":               "Operators select from the live list at execution time.",
			"builder.scan.note":                 "Operators scan the Animal ID; unreadable scans are rejected.",
			"builder.scan.mode_label":           "Scan mode",
			"builder.scan.single_hint":          "Operator scans / selects one goat.",
			"builder.scan.multi_hint":           "Operator scans every goat in the pen — one batched session.",
			"builder.preview.scan_add":          "Scan goat",
			"builder.gates.subject_hint":        "Batch = one proof for the whole pen session. Per-goat = a proof per animal.",
			"builder.boolean.note":              "Yes / No answer.",
			"builder.logic.title":               "Conditional logic",
			"builder.logic.add":                 "Only show this question when…",
			"builder.logic.remove":              "Always show this question",
			"builder.logic.show_when":           "Show only when",
			"builder.logic.answer_to":           "the answer to",
			"builder.logic.first_note":          "The first question always shows. Conditions can only reference an earlier question.",
			"builder.logic.value_placeholder":   "value",
			"builder.logic.values_placeholder":  "value 1, value 2",
			"builder.preview.title":             "Operator preview",
			"builder.preview.open":              "Preview form",
			"builder.preview.close":             "Close preview",
			"builder.preview.subtitle":          "Fill it in like an operator — conditional questions show or hide live. Nothing is saved.",
			"builder.preview.reset":             "Reset",
			"builder.preview.empty":             "Add a question to preview the operator form.",
			"builder.preview.required_badge":    "required",
			"builder.preview.conditional_badge": "conditional",
			"builder.summary.fields":            "questions",
			"builder.summary.rules":             "conditional rules",
			"builder.summary.proof":             "proof gate",
			// Follow-up (operator steps) editor -- SOP-driven herd operations, 2026-09-13.
			"followup.title":               "Operator steps",
			"followup.subtitle":            "What the operator does after the event, in order. Each step names its type, the proof it needs, and when it is due. Publishing applies to workflows opened from then on; open workflows keep the steps they started with.",
			"followup.track":               "Track",
			"followup.step.type":           "Step type",
			"followup.step.title":          "Title the operator sees",
			"followup.step.detail":         "Instruction",
			"followup.step.options":        "Choices",
			"followup.step.add_option":     "Add choice",
			"followup.step.proof_videos":   "Videos required",
			"followup.step.proof_photos":   "Photos required",
			"followup.step.schedule":       "Due",
			"followup.step.offset_minutes": "Minutes after",
			"followup.step.day_offset":     "Days after the event day",
			"followup.step.time":           "Time (IST, HH:MM)",
			"followup.step.times":          "Times (IST, comma separated)",
			"followup.step.days":           "Days",
			"followup.step.pre_notify":     "Skip a same-day time if the event is within (minutes)",
			"followup.step.after_step":     "After step",
			"followup.step.section":        "Section",
			"followup.step.hard_time_gate": "Cannot be done before it is due",
			"followup.step.wait_for_all":   "Waits for every other step",
			"followup.step.requires":       "Needs these steps done first",
			"followup.step.condition":      "Include this step",
			"followup.step.key":            "Step key",
			// SOP studio phase 2 (2026-09-18): the List / Flow views and answer-driven branches.
			"studio.view.list":           "List",
			"studio.view.flow":           "Flow",
			"studio.view.label":          "View",
			"studio.flow.start":          "Start",
			"studio.flow.finish":         "Finish",
			"studio.flow.start_hint":     "The event that opens this work",
			"studio.flow.finish_hint":    "Every step on the path is done",
			"studio.flow.decision":       "Based on the answer",
			"studio.flow.otherwise":      "Otherwise",
			"studio.flow.insert":         "Insert step here",
			"studio.flow.add_branch":     "Add branch",
			"studio.flow.select_hint":    "Click a step to edit it. Click + on a line to insert a step there.",
			"studio.flow.properties":     "Selected step",
			"studio.flow.none_selected":  "Nothing selected",
			"studio.flow.fit":            "Fit",
			"studio.answer.yes_no":       "Yes / no",
			"studio.answer.select":       "Pick one",
			"studio.answer.multiselect":  "Pick many",
			"studio.answer.number":       "Number",
			"studio.answer.text":         "Text",
			"studio.flow.videos":         "videos",
			"studio.flow.photos":         "photos",
			"studio.flow.zoom_in":        "Zoom in",
			"studio.flow.zoom_out":       "Zoom out",
			"studio.branch.title":        "Only if",
			"studio.branch.always":       "Always runs",
			"studio.branch.question":     "Question",
			"studio.branch.op":           "Answer",
			"studio.branch.value":        "Value",
			"studio.branch.values_hint":  "Comma separated for more than one",
			"studio.branch.clear":        "Runs always",
			"studio.branch.yes":          "Yes",
			"studio.branch.no":           "No",
			"studio.branch.op.eq":        "is",
			"studio.branch.op.ne":        "is not",
			"studio.branch.op.in":        "is one of",
			"studio.branch.op.not_in":    "is none of",
			"studio.branch.op.gt":        "is more than",
			"studio.branch.op.gte":       "is at least",
			"studio.branch.op.lt":        "is less than",
			"studio.branch.op.lte":       "is at most",
			"studio.branch.note":         "Only if",
			"studio.kind.label":          "Kind",
			"studio.kind.module":         "Module-level",
			"studio.kind.general":        "General",
			"studio.kind.module_hint":    "Owned and run by a module: birth by Herd Operations, a weighing session by Weighing, packing by Feed.",
			"studio.kind.general_hint":   "Farm-wide work tied to no module. Authoring remains available; the phone runner is retired.",
			"followup.step.add":          "Add step",
			"followup.step.remove":       "Remove step",
			"followup.step.move_up":      "Move up",
			"followup.step.move_down":    "Move down",
			"followup.action.publish":    "Publish SOP",
			"followup.action.save_draft": "Save as draft",
			// After Publish the editor CLOSES and the library says so, loudly (maintainer report
			// 2026-09-15: "on top I'm seeing v3 published but nothing is changing visually, so I
			// can't tell whether my change is reflected"). {version} and {name} are filled in.
			"notice.published.title":       "Published v{version} — {name}",
			"notice.published.body":        "The phone uses this version from now on. Earlier records keep the version they were made with.",
			"notice.published.dismiss":     "Dismiss",
			"followup.notice.locked_key":   "This step drives server behaviour; its key and type are fixed, everything else is yours to edit.",
			"followup.notice.capture_kept": "The capture form the operator fills when recording the event is unchanged by this page.",
			"followup.empty":               "No operator steps yet.",
			// Read-side description of the authored steps (SOP drawer + the editor's series preview):
			// the rounds the engine will schedule for one animal, in farm words.
			"followup.drawer.title":                 "What the operator does after the event",
			"followup.drawer.subtitle":              "from the published SOP; a repeating round is listed once per time it runs",
			"followup.preview.series":               "Runs {n} times per animal — rounds already past when the event is recorded are skipped:",
			"followup.day.event":                    "event day",
			"followup.day.after":                    "+{d} day(s)",
			"followup.due.right_away":               "right away",
			"followup.due.after_event":              "{n} min after the event",
			"followup.due.fixed_time":               "day +{d} at {t}",
			"followup.due.series":                   "{n} rounds · {times} · {days} day(s)",
			"followup.due.series_from_event":        "{n} rounds, every {every} from the event",
			"followup.due.series_next_sessions":     "{n} rounds at the next session times ({times}) after the event",
			"followup.basis.next_sessions":          "At the farm's session times, a fixed number of rounds from the next session onward",
			"followup.preview.series_next_sessions": "Runs {n} times per animal at the farm's session times, starting from the next session after the event. Example for an event at",
			"followup.preview.example_time":         "Example event time (HH:MM)",
			"followup.step.basis":                   "Rounds run",
			"followup.basis.fixed_times":            "At the farm's session times, from the time of the event — sessions already past are skipped (default)",
			"followup.basis.from_event":             "From the event time (every N minutes, nothing skipped)",
			"followup.step.interval_minutes":        "Minutes between rounds",
			"followup.step.count":                   "Number of rounds",
			"followup.preview.series_from_event":    "Runs {n} times per animal, counted from the moment the event is recorded:",
			"followup.round.after":                  "{after} after the event",
			"followup.due.after_step":               "{n} min after \"{step}\"",
			"followup.proof.videos":                 "{n} video(s)",
			"followup.proof.photos":                 "{n} photo(s)",
			"followup.step.only_when":               "only when the kid pen could not be resolved",
			// WHO DOES A STEP (SALES SOP, 2026-09-19): every follow-up editor offers the
			// designation a step is for; blank is anyone who can open the workflow.
			"followup.step.owner":      "Done by",
			"followup.step.owner_any":  "Anyone",
			"followup.step.owner_hint": "Only this designation can do the step on the phone; everyone else sees it read-only.",
		}
		// Per-module copy: crumb names the owning vertical, and the builder's domain lock names
		// the module the page is scoped to (SOP split, maintainer decision 2026-08-18).
		switch id {
		// vaccination-sops was here. The page is gone; its copy went with it.
		case "counts-sops":
			m["crumb"] = "Counts"
			m["filter.domain.current"] = "This page shows Herd Operations SOPs (birth, death, shifting, reconcile)"
			m["modal.builder.domain_aria"] = "Domain — locked to Counts / Herd Operations"
			m["modal.builder.domain_title"] = "Domain is locked to Counts / Herd Operations on this page"
			m["modal.builder.domain_label"] = "Counts / Herd Operations"
			m["modal.builder.policy_label"] = "herd operations policy"
			m["modal.builder.default_name"] = "Herd operation"
			m["modal.builder.placeholder.name"] = "Herd operation"
			m["modal.builder.eyebrow"] = "SOP · COUNTS / HERD OPERATIONS"
			m["empty.title"] = "No herd operations SOPs yet"
			m["empty.body"] = "Publish a birth, death, shifting or reconcile SOP to drive the operator's steps on the phone."
			m["builder.subtitle"] = "Build it like a form — add questions, choose a type, set choices and conditional logic. Herd Operations SOPs also carry the operator steps the phone runs after the event."
			// SHIFTING SOP (maintainer decision 2026-09-16): the shifting cards editor's copy, kept in
			// its own helper so the herd-operations capture-card copy merges beside it.
			for k, v := range shiftingSOPEditorCopy() {
				m[k] = v
			}
			addHerdOpsCaptureCardCopy(m)
		case "sales-sops":
			m["crumb"] = "Sales"
			m["filter.domain.current"] = "This page shows the Sales SOP: what happens after a sale is recorded, and who does each step"
			m["modal.builder.domain_aria"] = "Domain — locked to Sales"
			m["modal.builder.domain_title"] = "Domain is locked to Sales on this page"
			m["modal.builder.domain_label"] = "Sales"
			m["modal.builder.default_name"] = "Sale"
			m["modal.builder.placeholder.name"] = "Sale"
			m["modal.builder.policy_label"] = "sales policy"
			m["modal.builder.eyebrow"] = "SOP · SALES"
			m["empty.title"] = "No sales SOP yet"
			m["empty.body"] = "Publish the Sale SOP to drive the steps the phone runs after a sale is recorded."
			m["followup.subtitle"] = "What happens after a sale is recorded, in order: tagging the animals, loading them, the money. Each step names who does it, its type, the proof it needs, and when it is due. Publishing applies to sales recorded from then on; a sale already recorded keeps the steps it started with."
			m["followup.notice.capture_kept"] = "Opened by the engine the moment a sale is recorded; the Record sale form itself is unchanged."
			m["filter.domain.current"] = "This page shows the Sales SOPs: what happens after a sale is recorded and who does each step, and what the vendor form asks"
			for k, v := range inspectionEditorCopy() {
				m[k] = v
			}
		case "configuration-work-instructions":
			m["crumb"] = "Configuration"
			m["filter.domain.current"] = "This page shows general work instructions: SOPs tied to no module, started by hand"
			m["modal.builder.domain_aria"] = "Domain — locked to General"
			m["modal.builder.domain_title"] = "Domain is locked to General on this page"
			m["modal.builder.domain_label"] = "General"
			m["modal.builder.default_name"] = "Work instruction"
			m["modal.builder.placeholder.name"] = "Work instruction"
			m["modal.builder.policy_label"] = "general work instruction"
			m["modal.builder.eyebrow"] = "SOP · GENERAL"
			m["followup.subtitle"] = "What the operator does once they start this work instruction, in order. Each step names its type, the proof it needs, and when it is due. Publishing applies to runs started from then on; a run already started keeps the steps it started with."
			m["followup.notice.capture_kept"] = "Started by hand from the phone; every run is its own record."
		case "pc-care-sops":
			// PC CARE SOP editor + drawer summary copy (2026-09-22). Merged FIRST so the module's
			// own builder names below still win -- the weighing page's own trap, recorded there.
			for k, v := range pcCareSOPEditorCopy() {
				m[k] = v
			}
			m["crumb"] = "Preventive Care"
			m["filter.domain.current"] = "This page shows Preventive Care SOPs (deworming, ticks removal, trimming)"
			m["modal.builder.domain_aria"] = "Domain — locked to Preventive Care"
			m["modal.builder.domain_title"] = "Domain is locked to Preventive Care on this page"
			m["modal.builder.domain_label"] = "Preventive Care"
			m["modal.builder.default_name"] = "Preventive Care task"
			m["modal.builder.placeholder.name"] = "Preventive Care task"
			m["modal.builder.policy_label"] = "preventive care policy"
			m["modal.builder.eyebrow"] = "SOP · PREVENTIVE CARE"
			m["empty.title"] = "No preventive care SOPs yet"
			m["empty.body"] = "Publish the document that says what the operator captures on a deworming, ticks removal or trimming task."
		case "feed-sops":
			m["crumb"] = "Feed"
			m["filter.domain.current"] = "This page shows Feed SOPs (distribution, packing, transport)"
			m["modal.builder.domain_aria"] = "Domain — locked to Feed"
			m["modal.builder.domain_title"] = "Domain is locked to Feed on this page"
			m["modal.builder.domain_label"] = "Feed"
			// The builder's name and policy wording are the module's own: a new Feed SOP is not a
			// "Vaccination session" under a "vaccination drive/session policy".
			m["modal.builder.default_name"] = "Feed session"
			m["modal.builder.placeholder.name"] = "Feed session"
			m["modal.builder.policy_label"] = "feed chain policy"
			m["modal.builder.eyebrow"] = "SOP · FEED"
			m["empty.title"] = "No feed SOPs yet"
			m["empty.body"] = "Publish a distribution, packing or transport SOP for the feed chain."
			for k, v := range feedSOPEditorCopy() {
				m[k] = v
			}
		case "procurement-sops":
			m["crumb"] = "Procurement"
			m["filter.domain.current"] = "This page shows Procurement SOPs (purchases, forms and the aflatoxin test)"
			m["modal.builder.domain_aria"] = "Domain — locked to Procurement"
			m["modal.builder.domain_title"] = "Domain is locked to Procurement on this page"
			m["modal.builder.domain_label"] = "Procurement"
			m["modal.builder.default_name"] = "Animal purchase inspection"
			m["modal.builder.placeholder.name"] = "Animal purchase inspection"
			m["modal.builder.policy_label"] = "procurement policy"
			m["modal.builder.eyebrow"] = "SOP · PROCUREMENT"
			m["empty.title"] = "No procurement SOPs yet"
			for k, v := range inspectionEditorCopy() {
				m[k] = v
			}
			for k, v := range toxinEditorCopy() {
				m[k] = v
			}
			// THE SPLIT IS THE POINT (maintainer decision 2026-09-20): `procurement.vendor` asks
			// about a SUPPLIER and `sales.vendor` about a BUYER, so the editor shared by both must
			// not greet the buying desk with the other side's wording. Only this page is narrowed;
			// /sales/sops keeps the generic copy it already serves.
			m["vendor_form.title"] = "Supplier form"
			m["vendor_form.subtitle"] = "What is asked when a supplier is added or edited on the buying side of the register, page by page. Each question names its kind and whether it is compulsory. Publishing applies to suppliers added or edited from then on, on the web and on the phone."
			m["vendor_form.notice.capture_kept"] = "Name, type, state and status stay compulsory; the locked questions are the supply register's own columns. Everything else -- wording, order, pages, extra questions -- is yours to change."
			m["vendor_form.drawer.title"] = "What the form asks, page by page"
		case "milk-sops":
			m["crumb"] = "Milk"
			m["filter.domain.current"] = "This page shows Milk SOPs (preparation, feeding)"
			m["modal.builder.domain_aria"] = "Domain — locked to Milk"
			m["modal.builder.domain_title"] = "Domain is locked to Milk on this page"
			m["modal.builder.domain_label"] = "Milk"
			m["modal.builder.default_name"] = "Milk round"
			m["modal.builder.placeholder.name"] = "Milk round"
			m["modal.builder.policy_label"] = "kid-milk policy"
			m["modal.builder.eyebrow"] = "SOP · MILK"
			m["empty.title"] = "No milk SOPs yet"
			m["empty.body"] = "Publish a preparation or feeding SOP for the kid-milk round."
		case "weighing-sops":
			// WEIGHING SOP rules editor + drawer summary copy (2026-09-15). Merged FIRST so the
			// module's own builder names below still win. Dropped once by a merge (e41a95e93),
			// which crashed "Change SOP" with a missing wsop.title; pinned by
			// TestWeighingSOPsPageCarriesTheWeighingEditorCopy.
			for k, v := range weighingSOPEditorCopy() {
				m[k] = v
			}
			m["crumb"] = "Weighing"
			m["filter.domain.current"] = "This page shows Weighing SOPs (scan-and-submit sessions)"
			m["modal.builder.domain_aria"] = "Domain — locked to Weighing"
			m["modal.builder.domain_title"] = "Domain is locked to Weighing on this page"
			m["modal.builder.domain_label"] = "Weighing"
			m["modal.builder.default_name"] = "Weighing session"
			m["modal.builder.placeholder.name"] = "Weighing session"
			m["modal.builder.policy_label"] = "weighing session policy"
			m["modal.builder.eyebrow"] = "SOP · WEIGHING"
			m["empty.title"] = "No weighing SOPs yet"
			m["empty.body"] = "Publish the scan-and-submit weighing session SOP."
			// The Assumptions drawer (maintainer decision 2026-09-19): every figure the Weighing
			// pages are valued and judged against, edited here by whoever holds the Configure
			// tick. Labels are keyed on the assumption key (assumption.<key>.label / .hint).
			m["action.assumptions"] = "Assumptions"
			m["disabled.assumptions"] = "Your access can read the Weighing figures but not change them."
			m["drawer.assumptions.title"] = "Assumptions"
			m["drawer.assumptions.caption"] = "Figures the Weighing pages are valued and judged against. A change applies the next time a page loads, whatever period the page is showing."
			m["drawer.assumptions.prices.title"] = "Live-weight sale price"
			m["drawer.assumptions.prices.hint"] = "₹ per kg live weight, used to value weight gained (FCR tab) and stock on hand (Load-wise tab)."
			m["drawer.assumptions.prices.default"] = "All stages"
			m["drawer.assumptions.prices.by_stage"] = "By stage and sex"
			m["drawer.assumptions.prices.by_stage.hint"] = "Only stages with weighed animals in them are listed. Leave a box blank to use the all-stages price above. Each animal is valued at its own stage and sex."
			m["drawer.assumptions.prices.stage"] = "Stage"
			m["drawer.assumptions.prices.male"] = "Male"
			m["drawer.assumptions.prices.female"] = "Female"
			m["drawer.assumptions.prices.overrides"] = "set"
			m["drawer.assumptions.values.title"] = "Sale lines & alerts"
			m["assumption.goat.label"] = "Goat"
			m["assumption.sheep.label"] = "Sheep"
			m["assumption.sale_ready_threshold_kg.label"] = "Sale-ready weight"
			m["assumption.sale_ready_threshold_kg.hint"] = "The 'Over N kg' line on Farm value and the Weights cards."
			m["assumption.load_age_alert_days.label"] = "Load age alert"
			m["assumption.load_age_alert_days.hint"] = "Days after purchase at which a load still holding animals is flagged to the CXO every morning."
			m["drawer.assumptions.rupees"] = "₹"
			m["drawer.assumptions.set_by"] = "set by"
			m["drawer.assumptions.save"] = "Save"
			m["drawer.assumptions.cancel"] = "Cancel"
			m["drawer.assumptions.saved"] = "Saved. The pages now use these figures."
			m["drawer.assumptions.error"] = "Could not save these figures."
			m["drawer.assumptions.conflict"] = "Someone else changed these figures. Reload and try again."
			m["assumption.sale_ready_lower_kg.label"] = "Lower sale line"
			m["assumption.sale_ready_lower_kg.hint"] = "The 'Over N kg' lower count on the Weights cards."
			m["assumption.slow_growth_target_g_per_day.label"] = "Slow-growth target"
			m["assumption.slow_growth_target_g_per_day.hint"] = "A pen whose daily gain sits below this is listed as slow growth."
			m["assumption.bad_scan_loss_g_per_day.label"] = "Bad-scan cut-off"
			m["assumption.bad_scan_loss_g_per_day.hint"] = "A kid that appears to lose more than this per day between two weighings is treated as a bad scan and left out of growth."
			m["assumption.default_period_days.label"] = "Default period"
			m["assumption.default_period_days.hint"] = "How many days the growth widgets cover when no dates are chosen."
			m["assumption.weight_band_edges_kg.label"] = "Weight bands"
			m["assumption.weight_band_edges_kg.hint"] = "The band edges, lowest to highest, for the band board, the Weight-wise tab and FCR by weight band. Comma-separated."
			m["drawer.assumptions.growth.title"] = "Growth"
			m["drawer.assumptions.window.title"] = "Growth widgets"
		}
		return m
	case "goat-passport":
		return map[string]string{
			"fallback.title":                 "Goat Passport",
			"section.summary.title":          "Summary",
			"section.warnings.title":         "Warnings",
			"section.identifiers.title":      "Identifiers",
			"section.add_identifier.title":   "Add identifier",
			"section.evidence.title":         "Evidence",
			"section.timeline.title":         "Timeline",
			"section.vaccination.title":      "Vaccination",
			"table.vaccination.aria":         "Vaccination history",
			"table.identifiers.aria":         "Identifiers",
			"vaccination.unavailable_prefix": "Vaccination passport unavailable",
			"vaccination.dose_singular":      "dose",
			"vaccination.dose_plural":        "doses",
			"vaccination.next_due_inline":    "next due",
			"vaccination.no_upcoming":        "no upcoming dose",
			"vaccination.next_due":           "Next due",
			"vaccination.open_obligations":   "Open obligations",
			"vaccination.last_accepted":      "Last accepted",
			"vaccination.open_due_rows":      "Open due rows",
			"vaccination.history":            "History",
			"vaccination.empty_history":      "No vaccination history yet. Once a protocol is published and a dose is administered + verified, it appears here with its proof/verification status and protocol version.",
			"vaccination.empty_open":         "No open vaccination obligations for this goat.",
			"vaccination.clinical_due":       "clinical due",
			"vaccination.proof_verified":     "proof verified",
			"vaccination.awaiting_verify":    "awaiting verification",
			"vaccination.rework_rejected":    "rework / rejected",
			"action.open_workflow":           "Open Workflow",
			"action.open_action_center":      "Open Action Center",
			"action.add_identifier":          "Add identifier",
			"action.retire_identifier":       "Retire",
			"action.done":                    "done",
			"action.completed":               "Action completed.",
			"action.identifier_added":        "Identifier added.",
			"action.identifier_retired":      "Identifier retired.",
			"empty.timeline":                 "No identity events returned for this goat.",
			"empty.evidence":                 "No evidence refs returned.",
			"empty.identifiers":              "No identifiers returned for this passport.",
			"empty.warnings":                 "No passport warnings returned.",
			"label.row_version":              "row version",
			"label.goat_id":                  "Animal record ID",
			"label.display_id":               "Display ID",
			"label.tag_1":                    "Tag 1",
			"label.tag_2":                    "Tag 2",
			"label.animal_identifier_1":      "Tag 1",
			"label.animal_identifier_2":      "Tag 2",
			"label.stage":                    "Stage",
			"label.breed_sex":                "Breed / sex",
			"label.lifecycle":                "Lifecycle",
			"label.health":                   "Health",
			"label.reproductive":             "Reproductive",
			"label.growth_cohort":            "Growth cohort",
			"label.management":               "Management",
			"label.location":                 "Location",
			"label.merged_into":              "merged into",
			"label.type":                     "Type",
			"label.value":                    "Value",
			"label.scope":                    "Scope",
			"label.status":                   "Status",
			"label.primary":                  "Primary",
			"label.valid_from":               "Valid from",
			"label.action":                   "Action",
			"label.yes":                      "yes",
			"label.no":                       "no",
			"label.select":                   "Select",
			"label.identity_events_table":    "goat_identity_events",
			"label.occurred":                 "occurred",
			"label.recorded":                 "recorded",
			"label.evidence":                 "evidence",
			"label.decision":                 "decision",
			"label.no_decision":              "no decision",
			"field.identifier_type":          "Identifier type",
			"field.identifier_value":         "Identifier value",
			"field.scope_key":                "Scope key",
			"field.evidence_type":            "Evidence type",
			"field.evidence_id":              "Evidence ID",
			"field.evidence_source":          "Evidence source",
			"placeholder.scope_key":          "global or park scope",
			"placeholder.optional":           "optional",
			"reason.retire_identifier":       "Retired from passport identifier review.",
			"confirm.retire_identifier":      "Retire identifier?",
		}
	default:
		return map[string]string{}
	}
}

// healthConfigOptionGroups holds the four authoring vocabularies of a treatment protocol.
//
// All four are FIXED SCHEMA/CLINICAL constraints, not tenant data, which is what makes it correct
// to declare them here rather than compile them from a table: session and record_type are CHECK
// constraints on health_protocol_steps, critical_action_type is a CHECK plus the policy-pack
// handoff set, and medicine_route is the closed set of ways a drug can be given.
//
// They are validate-or-reject on the backend, so a key that drifts from this list fails the save
// with a field error rather than storing an instruction nobody can follow. That is why the routes
// in particular are a vocabulary and not free text: the difference between IM and IV is clinical,
// and a stored typo renders on the operator's phone as an unfollowable instruction.
// salesOptionGroups declares the sales page's closed vocabularies: the farm scope, the sellable
// product types, and the breeds offered per product type. These are contract vocabulary (the same
// closed sets the sales_deals CHECK constraints enforce), not live tenant rows, which is why they
// may live here rather than be injected from a reference family.
//
// Breeds are grouped per product type as three groups because the option shape carries no
// grouping metadata: the record-sale form switches which breed group it offers when the product
// selection changes.
func salesOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			// Mirrors sales/domain.Statuses -- the sheet's four lifecycle words. Kept as literals
			// rather than an import, exactly as the feed payment group is: the contract compiler
			// must not depend on a feature module's package. Tones follow dealStatusTone.
			ID: "sales_deal_statuses",
			Options: []domain.Option{
				option("Deal Closed", "Deal Closed", "", "ok"),
				option("Deal Failed", "Deal Failed", "", "dng"),
				option("In Discussion", "In Discussion", "", "info"),
				option("Advance Paid", "Advance Paid", "", "warn"),
			},
		},
		{
			ID: "sales_farms",
			Options: []domain.Option{
				option("all", "All farms", "", ""),
				option("CBE", "CBE", "", ""),
				option("CPT", "CPT", "", ""),
			},
		},
		{
			ID: "sales_product_types",
			Options: []domain.Option{
				option("Sheep", "Sheep", "", ""),
				option("Goat", "Goat", "", ""),
				option("Manure", "Manure", "", ""),
			},
		},
		{
			ID: "sales_breeds_sheep",
			Options: []domain.Option{
				option("Anantapur", "Anantapur", "", ""),
				option("Kenguri", "Kenguri", "", ""),
				option("Nipani", "Nipani", "", ""),
			},
		},
		{
			ID: "sales_breeds_goat",
			Options: []domain.Option{
				option("Malai", "Malai", "", ""),
				option("Sojat", "Sojat", "", ""),
				option("Osmanabadi", "Osmanabadi", "", ""),
				option("Beetle", "Beetle", "", ""),
				option("Sirohi", "Sirohi", "", ""),
			},
		},
		{
			ID: "sales_breeds_manure",
			Options: []domain.Option{
				option("Manure", "Manure", "", ""),
			},
		},
	}
}

func healthConfigOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "health_sessions",
			Options: []domain.Option{
				option("morning", "Morning", "first working session of the day", "info"),
				option("afternoon", "Afternoon", "midday session", "info"),
				option("evening", "Evening", "last session of the day", "info"),
				// "Any time" rather than the raw key: 'unscheduled' is a real bucket for a
				// once-daily medicine given whenever the operator reaches the animal, and the raw
				// word reads to an author as "not decided yet", which is the opposite meaning.
				option("unscheduled", "Any time", "no fixed session — given whenever the operator reaches the animal", "mut"),
			},
		},
		{
			ID: "health_record_types",
			Options: []domain.Option{
				option("medication", "Medicine", "give a medicine at a dosage and route", "ok"),
				option("action", "Action", "something the operator does that is not a medicine", "info"),
				option("critical_action", "Critical action", "hands the animal to quarantine, movement or a lifecycle exit", "warn"),
			},
		},
		{
			ID: "health_medicine_routes",
			Options: []domain.Option{
				option("IM", "IM", "intramuscular", ""),
				option("SQ", "SQ", "subcutaneous", ""),
				option("IV", "IV", "intravenous", ""),
				option("Oral", "Oral", "by mouth", ""),
				option("Topical", "Topical", "applied to the skin", ""),
				option("Intra Mammary", "Intra Mammary", "into the udder", ""),
			},
		},
		{
			ID: "health_dosage_units",
			Options: []domain.Option{
				option("ml", "ml", "millilitres", ""),
				// A real authored unit, deliberately distinct from leaving the unit blank: 'none'
				// means a whole-unit dose such as one bolus, blank means the author has not said.
				option("none", "none", "a whole unit, such as one bolus", "mut"),
				option("kg", "kg", "kilograms", ""),
			},
		},
		{
			ID: "health_critical_actions",
			Options: []domain.Option{
				option("quarantine_or_movement", "Quarantine or movement", "move the animal; the policy pack owns the transition", "warn"),
				option("lifecycle_exit", "Lifecycle exit", "cull or exit; the policy pack owns the transition", "bad"),
			},
		},
	}
}

func pageOptionGroups(id string) []domain.OptionGroup {
	switch id {
	case "alerts":
		return withGenericOptionGroups([]domain.OptionGroup{
			{ID: "alert_severities", Options: []domain.Option{
				option("critical", "Critical", "Needs a look today", "dng"),
				option("warning", "Warning", "Worth confirming", "warn"),
			}},
			// Parks are tenant rows; the compiler fills this from ReferenceFamilies.Parks.
			{ID: "alerts_parks", Options: []domain.Option{}},
		})
	case "work-board":
		return withGenericOptionGroups([]domain.OptionGroup{
			// The four columns, in display order. Derived server-side from the work state
			// (workboard/domain.LaneFor); the client never moves a card between them.
			{ID: "work_board_lanes", Options: []domain.Option{
				option("todo", "To do", "Nothing started", ""),
				option("in_progress", "In progress", "Something started", "info"),
				option("in_review", "In review", "Submitted, waiting for a verdict", "info"),
				option("done", "Done", "Approved", "ok"),
			}},
			// Every module the board CAN carry. The page narrows it per person from the
			// summary read's `modules`, which is the caller's own permission set; the
			// contract lists the full vocabulary so the chip colours are backend-owned.
			{ID: "work_board_modules", Options: []domain.Option{
				option("feed", "Feed", "", "ok"),
				option("health", "Health", "", "dng"),
				option("vaccination", "Vaccination", "", "info"),
				option("weighing", "Weighing", "", "teal"),
				option("counts", "Herd Ops", "", "pur"),
				option("milk", "Milk", "", "teal"),
				option("pc_care", "PC Care", "", "info"),
				option("toxin", "Toxin", "", "warn"),
				option("procurement", "Procurement", "", "warn"),
				option("verification", "Verification", "", "info"),
				option("tasks", "Tasks", "", "pur"),
			}},
			{ID: "work_board_states", Options: []domain.Option{
				option("scheduled", "Scheduled", "", ""),
				option("due", "Due", "", ""),
				option("overdue", "Past clock", "", "dng"),
				option("in_progress", "Started", "", "info"),
				option("proof_pending", "Needs proof", "", "warn"),
				option("verification_pending", "In verification", "", "info"),
				option("rejected", "Rework", "", "warn"),
				option("deferred", "Deferred", "", ""),
				option("missed", "Missed", "", "dng"),
				option("blocked", "Blocked", "", "dng"),
				option("completed", "Approved", "", "ok"),
			}},
			// Parks are tenant rows; the compiler fills this from ReferenceFamilies.Parks.
			{ID: "work_board_parks", Options: []domain.Option{}},
		})
	case "people":
		// The module tab strip on /people (maintainer decision 2026-08-22):
		// `all` is the general directory, `vaccination` hosts the former
		// vaccination-operators screen, and the remaining module views are
		// backend-declared DISABLED placeholders carrying their reason — the
		// client renders them inert with that copy. Adding a real module tab
		// later is a backend change only.
		soonTab := func(key, label string) domain.Option {
			return domain.Option{Key: key, Label: label, Enabled: false, DisabledReason: "This staffing view is coming soon."}
		}
		return withGenericOptionGroups([]domain.OptionGroup{
			{
				ID: "people_view_tabs",
				Options: []domain.Option{
					option("all", "All People", "", ""),
					// Clock In / Out (maintainer decisions 2026-08-27/28): the
					// attendance view, seated DIRECTLY beside All People
					// (maintainer ask 2026-08-29 — attendance comes before the
					// per-module staffing views). The tab is ENABLED here for
					// everyone the page admits; the per-principal gate is the
					// view_clock control (clock.presence.read) compiled beside
					// it — the renderer combines both, per role-scoped-UI rules.
					option("clock", "Clock In / Out", "", ""),
					// Notifications (maintainer decision 2026-09-08): who hears which
					// alert, per designation, seated right after Clock In / Out (maintainer
					// ask, same day). Enabled for everyone the page admits; the WRITE is
					// gated by the edit_notifications control compiled beside it.
					option("notifications", "Notifications", "", ""),
					option("vaccination", "Vaccination", "", ""),
					soonTab("weighing", "Weighing"),
					soonTab("feed", "Feed"),
					soonTab("counts", "Herd Operations"),
					soonTab("health", "Health"),
				},
			},
			{
				// The roles the Add Person form may grant — the same closed set the
				// backend enforces (workforce/app.grantablePersonRoles). Title carries
				// the grant's SCOPE SHAPE ("park" or "tenant") so the form knows when
				// the park select is required; it is a machine hint, not display copy.
				ID: "people_roles",
				Options: []domain.Option{
					// The ground tiers, by department (maintainer decision 2026-09-23).
					// These replace Operator; it stays grantable until the last person
					// carrying it has been moved across.
					option(permissions.RoleKey(permissions.TierManager, permissions.VerticalFeed), "Feed Manager", "park", ""),
					option(permissions.RoleKey(permissions.TierManager, permissions.VerticalHealth), "Health Manager", "park", ""),
					option(permissions.RoleKey(permissions.TierManager, permissions.VerticalFarming), "Farming Manager", "park", ""),
					option(permissions.RoleKey(permissions.TierManager, permissions.VerticalCleaning), "Cleaning Manager", "park", ""),
					option(permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalFeed), "Feed Assistant Manager", "park", ""),
					option(permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalHealth), "Health Assistant Manager", "park", ""),
					option(permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalFarming), "Farming Assistant Manager", "park", ""),
					option(permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalCleaning), "Cleaning Assistant Manager", "park", ""),
					option(permissions.RoleParkHead, "Park Head", "park", ""),
					option(permissions.RoleVerifier, "Verifier", "tenant", ""),
					option(permissions.RolePCDirector, "PC Director", "tenant", ""),
					option(permissions.RoleGrowthDirector, "Growth Director", "tenant", ""),
					option(permissions.RoleFeedDirector, "Feed Director", "tenant", ""),
					option(permissions.RoleHealthDirector, "Health Director", "tenant", ""),
					option(permissions.RoleBreedingDirector, "Breeding Director", "tenant", ""),
				},
			},
			{
				ID: "people_designation_grades",
				Options: []domain.Option{
					option("cxo", "CXO", "", ""),
					option("director", "Director", "", ""),
					option("manager", "Manager", "", ""),
					option("assistant_manager", "Assistant Manager", "", ""),
				},
			},
		})
	case "animal-purchases":
		// Closed vocabularies mirrored from animalpurchase/domain (literals, not an import: the
		// contract compiler must not depend on a feature module's package).
		return withGenericOptionGroups([]domain.OptionGroup{
			{
				ID: "animal_purchase_decisions",
				Options: []domain.Option{
					option("pending", "Awaiting decision", "", "neutral"),
					option("accepted", "Accepted", "", "ok"),
					option("rejected", "Rejected", "", "bad"),
					option("all", "All", "", ""),
				},
			},
			{
				ID: "animal_purchase_conditions",
				Options: []domain.Option{
					option("healthy", "Looks healthy", "", "ok"),
					option("minor_concern", "Minor concern", "", "warn"),
					option("unwell", "Looks unwell", "", "bad"),
				},
			},
		})
	case "feed-purchases":
		// The FEED list is deliberately NOT here: it is live tenant rows (feed_item_catalog), and
		// a constant list of feed labels in contract code is the banned pattern. The form reads it
		// from GET /procurement/feed-purchase-options, which serves exactly the set the write path
		// accepts. Farms and payment states ARE closed contract vocabulary -- the same sets the
		// domain validates against -- so they belong here.
		return withGenericOptionGroups([]domain.OptionGroup{
			{
				ID: "feed_purchase_farms",
				Options: []domain.Option{
					option("all", "All farms", "", ""),
					option("CBE", "CBE", "", ""),
					option("CPT", "CPT", "", ""),
				},
			},
			{
				// Mirrors procurement/domain.FeedPaymentStatuses -- the sheet's two states. Kept as
				// literals rather than an import, exactly as salesOptionGroups does: the contract
				// compiler must not depend on a feature module's package.
				ID: "feed_purchase_payment_statuses",
				Options: []domain.Option{
					option("Paid", "Paid", "", "ok"),
					option("Pending", "Pending", "", "warn"),
				},
			},
			{
				// Mirrors procurement/domain.FeedDeliveryStatuses (maintainer decision 2026-09-03),
				// in lifecycle order. The tone is the chip: a load on the road is a warning on the
				// ledger because it is feed the farm is counting on and does not have yet.
				ID: "feed_purchase_delivery_statuses",
				Options: []domain.Option{
					option("purchased", "In transit", "", "warn"),
					option("reached", "Delivered", "", "ok"),
				},
			},
		})
	case "sales-farm-born":
		// Sex and species are fixed register vocabularies; the other filters' choices (parks,
		// pens, breeds, stages) are LIVE herd facts and ride on the data read itself, never
		// here. There is no origin group, and since 2026-09-22 there could not be one: origin IS the
		// page's membership, so every animal on it carries the same value and a filter on it would
		// offer one choice that changes nothing.
		return withGenericOptionGroups([]domain.OptionGroup{
			{
				ID: "farm_born_sexes",
				Options: []domain.Option{
					option("male", "Male", "", ""),
					option("female", "Female", "", ""),
				},
			},
			{
				ID: "farm_born_species",
				Options: []domain.Option{
					option("goat", "Goat", "", ""),
					option("sheep", "Sheep", "", ""),
				},
			},
		})
	case "sales", "sales-sold", "sales-farm-value", "sales-config", "sales-buyer-analytics":
		// The config page renders every sales form, so it needs exactly the board's vocabulary:
		// farms, product types and the per-product breed groups. Shared, never a second copy --
		// a form offering different breeds from the page that reads them back is the drift this
		// avoids.
		return withGenericOptionGroups(salesOptionGroups())
	case "sales-loads":
		return withGenericOptionGroups([]domain.OptionGroup{
			{
				// The page's view tabs, in the order shown: Purchased first, and it is the tab the
				// page selects when the URL names none. The Farm born tab is HIDDEN for now
				// (maintainer instruction 2026-09-14): the view is a shell until it is built, so
				// only Purchased is served and the page renders no toggle at all while a single
				// view is offered. Restore the option here to show the toggle again.
				ID: "sales_views",
				Options: []domain.Option{
					option("purchased", "Purchased", "", ""),
				},
			},
		})
	case "health-config":
		return withGenericOptionGroups(healthConfigOptionGroups())
	case "control-tower":
		return append(genericOptionGroups(), processIntegrityOptionGroups()...)
	case "workflows", "workflow-record":
		return append(withGenericOptionGroups([]domain.OptionGroup{{
			ID: "chain_steps",
			Options: []domain.Option{
				option("config", "Config rule published", "protocol version goes live", "ok"),
				option("obligation", "Obligation generated", "per goat / dose against eligible cohorts", "ok"),
				option("drive", "Drive / session opened", "pen-drive batch for the cohort", "info"),
				option("sop", "SOP task started", "step template expands into field tasks", "info"),
				option("proof", "Proof uploaded", "one live in-app camera video set per goat", "info"),
				option("verify", "Verification", "accept / reject / request rework", "warn"),
				option("close", "Completion posted", "dose consumed · booster scheduled if due", "ok"),
			},
		}}), processIntegrityOptionGroups()...)
	case "vaccination-live-tracker":
		return withGenericOptionGroups(liveTrackerOptionGroups())
	case "vaccination":
		return append(withGenericOptionGroups([]domain.OptionGroup{
			shedStatusOptionGroup(),
			capacityOptionGroup(),
			{
				ID: "schedule_status_legend",
				Options: []domain.Option{
					option("overdue", "Overdue", "missed, overdue, or rejected", "dng"),
					option("due_soon", "Due soon", "due, proof, or verification pending", "warn"),
					option("scheduled", "Scheduled", "drive scheduled or in progress", "info"),
					option("up_to_date", "Up to date", "accepted or completed", "ok"),
					option("no_record", "No record", "no selected-year date", "mut"),
				},
			},
			{
				// The cohort rows leadership reads down the side of the command board's
				// vaccine matrix. Fixed ladder so the matrix keeps a stable shape and a
				// cohort with no live animals still shows as a row.
				// The cohort rows the matrix can show. Adults is NO LONGER a catch-all: it is an
				// ordinary rung whose membership is declared in command_board_cohort_stage_map
				// below. A stage that maps to nothing renders as its OWN row, so an unmapped or
				// newly-introduced stage is visible rather than silently absorbed.
				ID: "command_board_cohort_ladder",
				Options: []domain.Option{
					option("K0", "K0", "", ""),
					option("K1", "K1", "", ""),
					option("K2", "K2", "", ""),
					option("K3", "K3", "", ""),
					option("Kid", "Kid", "", ""),
					option("Fattening", "Fattening", "", ""),
					option("F2", "Fattening", "", ""),
					option("Adults", "Adults", "", ""),
				},
			},
			{
				// STAGE -> COHORT ROW, declared membership (maintainer decision 2026-08-06).
				//
				// Prefix matching with an Adults catch-all is what put F2-Female/F2-Male in the
				// adult herd (372 vs the true 324) and then left ICU-Kid — a KID carrying a
				// health-state prefix — sitting in Adults too. Both are the same defect: a label
				// the ladder did not recognise fell through to the last rung.
				//
				// So membership is explicit and Adults means exactly Non-Pregnant, Buck, Mother.
				// A composite operational label classifies by its UNDERLYING cohort, never by its
				// health/operational prefix: ICU-Kid is a kid, ICU-Non-Pregnant is an adult.
				// Anything absent here is deliberately NOT guessed — it renders as its own row
				// (Warmup is an arrival/acclimation state, neither adult nor kid by label, and
				// stays visible until the CEO bucket for it is defined).
				ID: "command_board_cohort_stage_map",
				Options: []domain.Option{
					option("NON-PREGNANT", "Adults", "", ""),
					option("BUCK", "Adults", "", ""),
					option("MOTHER", "Adults", "", ""),
					option("ADULT", "Adults", "", ""),
					option("ICU-NON-PREGNANT", "Adults", "", ""),
					option("K0", "K0", "", ""),
					option("K1", "K1", "", ""),
					option("K2", "K2", "", ""),
					option("K3", "K3", "", ""),
					option("ICU-KID", "Kid", "", ""),
					option("ICU-KIDS", "Kid", "", ""),
					option("QUARANTINE KIDS", "Kid", "", ""),
					option("F2-FEMALE", "Fattening", "", ""),
					option("F2-MALE", "Fattening", "", ""),
					option("FATTENING", "Fattening", "", ""),
				},
			},
			{
				// MATCHING order above puts the Adults catch-all last, because the last rung absorbs
				// any live stage no earlier rung claims. READING order is a different question: the
				// CEO reads the true adult herd first, then the not-adult cohorts. This group owns
				// that reading order and the "not adult" qualifier, so the frontend never re-sorts
				// business rows on its own.
				ID: "command_board_cohort_row_order",
				Options: []domain.Option{
					option("Adults", "Adults", "Non-Pregnant, Buck and Mother", ""),
					option("K0", "K0", "Kid", ""),
					option("K1", "K1", "Kid", ""),
					option("K2", "K2", "Kid", ""),
					option("K3", "K3", "Kid", ""),
					// A kid in ICU is still a kid; the health state is a prefix on the label, not a
					// different cohort.
					option("Kid", "Kid", "Kid — stage not further specified", ""),
					// F2 is the fattening KID cohort split by sex, not an adult group.
					option("F2", "F2", "Kid — fattening, by sex", ""),
					option("Fattening", "Fattening", "Kid — fattening", ""),
				},
			},
			{
				ID: "drive_steps",
				Options: []domain.Option{
					option("target", "Target", "Drive target|matching goats by stage · age · park — never random individuals", "ok"),
					option("group", "Group", "drive batches|matching goats grouped by park/date, with pen drilldown", "info"),
					option("route", "Route", "PC + pen owners|park/PC owner coordinates; Pen Manager/Backup executes pen list", "info"),
					option("execute", "Execute", "proof per goat|FEFO dose consumed, posted on verify", "warn"),
				},
			},
			{
				ID: "vaccination_import_columns",
				Options: []domain.Option{
					option("drive_code", "drive_code", "", ""),
					option("protocol_code", "protocol_code", "", ""),
					option("park", "park", "", ""),
					option("cohort", "cohort", "", ""),
					option("shed", "shed", "", ""),
					option("vaccine", "vaccine", "", ""),
					option("due_date", "due_date", "", ""),
					option("animals", "animals", "", ""),
					option("proof_type", "proof_type", "", ""),
					option("owner_role", "owner_role", "", ""),
				},
			},
			{
				ID: "new_drive_steps",
				Options: []domain.Option{
					option("target", "Target", "Drive target|stage · age · park", "ok"),
					option("group", "Group", "drive batches|park/date batch with pen drilldown", "info"),
					option("route", "Route", "PC + pen owners|manager / backup assignment", "info"),
					option("execute", "Execute", "proof per goat|proof gates before consume", "warn"),
				},
			},
			{
				ID: "vaccination_drive_sop_steps",
				Options: []domain.Option{
					option("drive_scheduled", "Drive scheduled", "Cohort + vaccine; FEFO stock reserved.", "ok"),
					option("per_goat_administration", "Per-goat administration", "Dose per animal; in-app camera proof for each goat.", "pur"),
					option("consume_posted", "Consume posted (ledger)", "Verified completion posts a consume movement for doses (FEFO).", "info"),
					option("coverage_booster", "Coverage + booster", "Coverage % computed; next booster scheduled.", "ok"),
				},
			},
			{
				ID: "matrix_states",
				Options: []domain.Option{
					option("completed", "done", "", "ok"),
					option("overdue", "overdue", "", "dng"),
					option("due", "due", "", "warn"),
					option("proof_pending", "video pending", "", "warn"),
					option("scheduled", "scheduled", "", "warn"),
					option("in_progress", "in progress", "", "info"),
					option("verification_pending", "verify pending", "", "warn"),
					option("rejected", "rework", "", "dng"),
					option("deferred", "deferred", "", "mut"),
					option("missed", "missed", "", "warn"),
					option("blocked", "blocked", "", "dng"),
				},
			},
			{
				ID: "obligation_count_chips",
				Options: []domain.Option{
					option("overdue", "overdue", "", "dng"),
					option("due", "due", "", "warn"),
					option("inProgress", "in progress", "", "info"),
					option("proofPending", "proof pending", "", "warn"),
					option("missed", "missed", "", "dng"),
					option("accepted", "accepted", "", "ok"),
					option("rejected", "rework", "", "dng"),
				},
			},
			{
				ID: "status_matrix_facets",
				Options: []domain.Option{
					option("cohort", "Cohort", "", ""),
					option("protocol", "Protocol", "", ""),
					option("vaccine", "Vaccine", "", ""),
					option("due_window", "Due window", "", ""),
					option("work_state", "Work state", "", ""),
				},
			},
			{
				ID: "cohort_detail_facets",
				Options: []domain.Option{
					option("cohort", "Cohort", "", ""),
					option("age_band", "Age band", "", ""),
					option("last_dose", "Last dose", "", ""),
					option("next_due", "Next due", "", ""),
					option("status", "Status", "", ""),
				},
			},
			{
				ID: "supplier_warmup_facets",
				Options: []domain.Option{
					option("holding_farm", "Holding farm", "", ""),
					option("supplier", "Supplier", "", ""),
					option("purpose", "Purpose", "", ""),
					option("hf_evidence", "HF evidence", "", ""),
					option("health_selection", "Health / selection", "", ""),
				},
			},
			{
				ID: "shed_event_facets",
				Options: []domain.Option{
					option("owner", "Owner", "", ""),
					option("proof_status", "Proof status", "", ""),
					option("verification_status", "Verification status", "", ""),
					option("sop_status", "SOP status", "", ""),
					option("due_window", "Due window", "", ""),
				},
			},
			{
				ID: "source_load_status",
				Options: []domain.Option{
					option("source_warmup", "Source warmup", "", "info"),
					option("health_pending", "Health pending", "", "warn"),
					option("pre_dispatch_pending", "Pre-dispatch pending", "", "warn"),
					option("dispatch_ready", "Dispatch ready", "", "teal"),
					option("in_transit", "In transit", "", "info"),
					option("arrival_review", "Arrival review", "", "pur"),
					option("accepted_intake", "Accepted intake", "", "ok"),
					option("rejected", "Rejected", "", "dng"),
					option("deferred", "Deferred", "", "mut"),
					option("blocked", "Blocked", "", "dng"),
					option("canceled", "Canceled", "", "mut"),
				},
			},
			{
				ID: "warmup_evidence_states",
				Options: []domain.Option{
					option("trusted", "complete · evidence", "", "ok"),
					option("imported", "evidence imported", "", "info"),
					option("flagged", "evidence flagged", "", "dng"),
					option("due", "HF evidence due", "", "warn"),
				},
			},
			{
				ID: "health_selection_states",
				Options: []domain.Option{
					option("warming", "warming", "", "info"),
					option("health_pending", "health pending", "", "warn"),
					option("selection_ok", "selection ok", "", "ok"),
					option("blocked_rejected", "blocked / rejected", "", "dng"),
					option("review", "review", "", "warn"),
					option("cleared_forward", "cleared forward", "", "ok"),
				},
			},
		}), processIntegrityOptionGroups()...)
	case "source-entry":
		return withGenericOptionGroups(append([]domain.OptionGroup{{
			ID: "journey_stages",
			Options: []domain.Option{
				option("purchase_source", "Purchase / source", "", ""),
				option("holding_warmup", "Holding warmup", "", ""),
				option("source_health_sop", "Source health SOP", "", ""),
				option("pre_dispatch", "Pre-dispatch", "", ""),
				option("transit", "Transit", "", ""),
				option("arrival_gate", "Arrival gate", "", ""),
				option("accepted_intake", "Accepted intake", "", ""),
			},
		}}, procurementOptionGroups()...))
	case "source-load":
		return withGenericOptionGroups(procurementOptionGroups())
	case "herd-register":
		return withGenericOptionGroups(herdRegisterOptionGroups())
	case "counts-breakdown":
		return withGenericOptionGroups(countsBreakdownOptionGroups())
	case "herd-analytics", "counts-mortality":
		return withGenericOptionGroups(nil)
	// No option vocabulary of its own: the only control on the page is the shared
	// window calendar, and park scope belongs to the top bar.
	case "health-analytics":
		return withGenericOptionGroups(nil)
	case "milk-preparation":
		return withGenericOptionGroups(nil)
	case "weighing-weights", "weighing-analytics":
		// One vocabulary for both weighing screens on purpose: they carry the SAME filter bar,
		// and two copies would let the capture-mode labels drift into two spellings of one thing.
		return withGenericOptionGroups(weighingWeightsOptionGroups())
	case "feed-direction", "feed-packing", "feed-config":
		return withGenericOptionGroups(feedOptionGroups())
	case "feed-analytics":
		return withGenericOptionGroups(append(feedOptionGroups(), domain.OptionGroup{
			ID: "feed_analytics_tabs",
			Options: []domain.Option{
				option("overview", "Overview", "", ""),
				option("items", "Stock", "", ""),
				option("peranimal", "Per Animal", "", ""),
				option("experiment", "Experiment", "", ""),
				option("execution", "Execution", "", ""),
				option("followup", "Follow-up", "", ""),
			},
		}))
	case "calendar":
		return withGenericOptionGroups(calendarOptionGroups())
	case "audit-log":
		return withGenericOptionGroups([]domain.OptionGroup{
			{
				ID: "audit_operation_families",
				Options: []domain.Option{
					option("all", "All", "", "mut"),
					option("vaccination", "Vaccination", "", "info"),
					option("procurement", "Source Entry", "", "teal"),
					option("counts", "Herd Register", "", "ok"),
					option("feed", "Feed", "", "warn"),
					option("weighing", "Weighing", "", "info"),
					option("health", "Health", "", "dng"),
					option("milk", "Milk", "", "teal"),
					option("admin", "Admin / SOP", "", "mut"),
					option("other", "Other", "", "mut"),
				},
			},
			{
				ID: "audit_status_tabs",
				Options: []domain.Option{
					option("all_results", "All results", "", "mut"),
					option("awaiting", "Awaiting", "", "warn"),
					option("rejected", "Rejected", "", "dng"),
					option("proof_gaps", "Proof gaps", "", "warn"),
				},
			},
		})
	case "dlq-center":
		return withGenericOptionGroups([]domain.OptionGroup{
			{
				ID: "dlq_status_tabs",
				Options: []domain.Option{
					option("dead_letter", "Dead-letter", "", "dng"),
					option("failed", "Failed", "", "warn"),
					option("discarded", "Discarded", "", "mut"),
				},
			},
			{
				ID: "dlq_repair_actions",
				Options: []domain.Option{
					option("replay", "Replay", "return selected message to pending delivery", "ok"),
					option("discard", "Discard", "keep selected poison message out of replay", "dng"),
				},
			},
		})
	case "goat-passport":
		return withGenericOptionGroups([]domain.OptionGroup{
			{
				ID: "identifier_types",
				Options: []domain.Option{
					option("animal_identifier_1", "Tag 1", "", ""),
					option("animal_identifier_2", "Tag 2", "", ""),
				},
			},
			{
				ID: "evidence_types",
				Options: []domain.Option{
					option("source_record", "source_record", "", ""),
					option("identifier", "identifier", "", ""),
					option("goat", "goat", "", ""),
					option("event", "event", "", ""),
					option("media", "media", "", ""),
					option("decision", "decision", "", ""),
					option("import_run", "import_run", "", ""),
					option("conflict", "conflict", "", ""),
					option("location", "location", "", ""),
					option("actor", "actor", "", ""),
				},
			},
		})
	case "protocol-adherence":
		return append(genericOptionGroups(), processIntegrityOptionGroups()...)
	case "shed-execution":
		return append(append(genericOptionGroups(), processIntegrityOptionGroups()...),
			shedStatusOptionGroup(), capacityOptionGroup())
	case "vaccination-plan":
		return withGenericOptionGroups(configOptionGroups())
	// Vaccination is deliberately absent: its SOP page is gone, and its content lives on
	// the vaccination plan console. milk and weighing arrived on main meanwhile and stay.
	case "milk-sops":
		return withGenericOptionGroups(sopOptionGroupsFor(id))
	case "counts-sops":
		// SHIFTING SOP + HERD OPERATIONS CAPTURE CARD (2026-09-16): the shifting cards editor and the
		// birth/death capture editor both reuse the weighing card's question and capture-kind
		// vocabularies -- one meaning of "photo / video / either" on every card.
		return withGenericOptionGroups(append(sopOptionGroupsFor(id), shiftingSOPOptionGroups()...))
	case "feed-sops":
		// FEED SOP (2026-09-16): the feed cards editor reuses the weighing card's question and
		// capture-kind vocabularies -- one meaning of "photo / video / either" on every card.
		return withGenericOptionGroups(append(sopOptionGroupsFor(id), weighingSOPOptionGroups()...))
	case "weighing-sops":
		return withGenericOptionGroups(append(sopOptionGroupsFor(id), weighingSOPOptionGroups()...))
	case "pc-care-sops":
		// PC CARE SOP (2026-09-22): the care cards editor reuses the weighing card's question and
		// capture-kind vocabularies -- one meaning of "photo / video / either" on every card --
		// and adds its own removal-mode and category vocabularies.
		return withGenericOptionGroups(append(append(sopOptionGroupsFor(id), weighingSOPOptionGroups()...), pcCareSOPOptionGroups()...))
	case "configuration-work-instructions":
		return withGenericOptionGroups(sopOptionGroupsFor(id))
	case "sales-sops":
		// VENDOR FORM (2026-09-19): the sales.vendor SOP is edited through the inspection pages
		// editor, which picks question kinds from the same closed vocabulary.
		return withGenericOptionGroups(append(sopOptionGroupsFor(id), inspectionOptionGroups()...))
	case "procurement-sops":
		// PROCUREMENT IS SOP-DRIVEN END TO END (2026-09-20): the page edits pages-of-questions
		// documents (inspection, load form, supplier form), the two purchase WORKFLOWS through the
		// shared operator-steps editor, and the aflatoxin PROCEDURE through its own.
		groups := append(sopOptionGroupsFor(id), inspectionOptionGroups()...)
		groups = append(groups, toxinOptionGroups()...)
		return withGenericOptionGroups(groups)
	case "action-center":
		return withGenericOptionGroups([]domain.OptionGroup{
			{
				ID: "priority_chips",
				Options: []domain.Option{
					option("high", "High", "Derived from computed severity", "dng"),
					option("med", "Med", "Derived from computed severity", "warn"),
					option("low", "Low", "Derived from computed severity", "info"),
				},
			},
			{
				ID: "work_state_board_columns",
				Options: []domain.Option{
					option("ontime", "Done", "", "ok"),
					option("pending", "Pending", "", "info"),
					option("late", "Late", "", "warn"),
					option("skipped", "Skipped — silent", "", "dng"),
					option("deviated", "Deviated", "", "pur"),
				},
			},
			{
				ID: "work_state_filter_chips",
				Options: []domain.Option{
					option("overdue", "Overdue", "", "dng"),
					option("missed", "Missed", "", "warn"),
					option("blocked", "Blocked", "", "dng"),
					option("rejected", "Rejected", "", "dng"),
					option("proof_pending", "Proof pending", "", "warn"),
					option("verification_pending", "Verification pending", "", "pur"),
					option("due", "Due", "", "warn"),
					option("in_progress", "In progress", "", "info"),
					option("scheduled", "Scheduled", "", "info"),
					option("deferred", "Deferred", "", "mut"),
					option("completed", "Completed", "", "ok"),
				},
			},
			{
				ID: "severity_chips",
				Options: []domain.Option{
					option("broken", "Broken", "", "dng"),
					option("at_risk", "At risk", "", "warn"),
					option("watch", "Watch", "", "info"),
					option("ok", "OK", "", "ok"),
				},
			},
			{
				ID: "sop_state_chips",
				Options: []domain.Option{
					option("not_started", "SOP: not started", "", "mut"),
					option("in_progress", "SOP: in progress", "", "info"),
					option("submitted", "SOP: submitted", "", "warn"),
					option("accepted", "SOP: accepted", "", "ok"),
					option("rework", "SOP: rework", "", "dng"),
				},
			},
			{
				ID: "proof_state_chips",
				Options: []domain.Option{
					option("not_required", "Proof: n/a", "", "mut"),
					option("missing", "Proof: missing", "", "warn"),
					option("uploaded", "Proof: uploaded", "", "info"),
					option("accepted", "Proof: accepted", "", "ok"),
					option("rejected", "Proof: rejected", "", "dng"),
				},
			},
			{
				ID: "verification_state_chips",
				Options: []domain.Option{
					option("not_ready", "Verify: not ready", "", "mut"),
					option("pending", "Verify: pending", "", "pur"),
					option("verified", "Verify: verified", "", "ok"),
					option("accepted", "Verify: accepted", "", "ok"),
					option("rejected", "Verify: rejected", "", "dng"),
				},
			},
		})
	default:
		return genericOptionGroups()
	}
}

func configOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			// Capacity scope options for the daily-cap card. Only 'tenant' is honored by the planner today;
			// center/shed are shown disabled-with-reason (mock-fidelity: disable, never hide the future shape).
			ID: "capacity_scopes",
			Options: []domain.Option{
				option("tenant", "Whole tenant (all parks)", "One daily cap for the whole tenant", "ok"),
				{Key: "center", Label: "Per center", Enabled: false, DisabledReason: "Per-center caps are not available yet — planning uses the tenant-wide cap."},
				{Key: "shed", Label: "Per pen", Enabled: false, DisabledReason: "Per-pen caps are not available yet — planning uses the tenant-wide cap."},
			},
		},
		{
			ID: "capacity_overflow_policies",
			Options: []domain.Option{
				option("split_within_safe_window_last_safe_may_exceed_cap", "Split safely; last day may exceed cap", "Spread work across safe days at the cap; if the last legal day is reached, keep the animal in that drive instead of opening a review hatch.", ""),
			},
		},
		{
			ID: "rule_categories",
			Options: []domain.Option{
				option("vaccination", "vaccination", "", ""),
				option("feed_direction", "feed_direction", "Protocol-rule parameter templates for Feed Direction. The ration grid, feed factors, session template and dispatch clock are authored in Feed Config, not here.", "info"),
			},
		},
		{
			ID: "rule_scopes",
			Options: []domain.Option{
				option("tenant", "tenant (company default)", "", ""),
			},
		},
		{
			ID: "protocol_placeholders",
			Options: []domain.Option{
				option("vaccination.code", "Preventive Care vaccination plan", "", ""),
				option("vaccination.name", "Enterotoxaemia", "", ""),
				option("feed_direction.code", "feed.direction.template", "", ""),
				option("feed_direction.name", "Reviewed feed parameter template", "", ""),
			},
		},
		{
			ID: "animal_stage_scope",
			Options: []domain.Option{
				option("all", "all (every stage)", "", ""),
			},
		},
		{
			ID: "rule_species",
			Options: []domain.Option{
				option("all", "all", "", ""),
				option("goat", "goat", "", ""),
				option("sheep", "sheep", "", ""),
			},
		},
		{
			ID: "rule_sexes",
			Options: []domain.Option{
				option("all", "all", "", ""),
				option("female", "female", "", ""),
				option("male", "male", "", ""),
			},
		},
		{
			ID: "rule_breeds",
			Options: []domain.Option{
				option("all", "all", "", ""),
			},
		},
		{
			ID: "rule_healths",
			Options: []domain.Option{
				option("any", "any", "", ""),
			},
		},
		{
			ID: "rule_lifecycles",
			Options: []domain.Option{
				option("alive", "alive", "", ""),
				option("any", "any", "", ""),
			},
		},
		{
			ID: "rule_reproductive",
			Options: []domain.Option{
				option("any", "any", "", ""),
			},
		},
		{
			ID: "excluded_reproductive_states",
			Options: []domain.Option{
				option("pregnant", "exclude pregnant unless row explicitly allows", "pregnancy-specific timing needs a reviewed matrix row", "warn"),
				option("lactating", "exclude lactating unless row explicitly allows", "lactation-specific timing needs a reviewed matrix row", "warn"),
			},
		},
		{
			ID:      "defer_states",
			Options: []domain.Option{},
		},
		{
			ID: "missed_dose_policies",
			Options: []domain.Option{
				option("immediate", "catch up immediately", "", ""),
				option("next_cycle", "skip to next cycle", "", ""),
				option("pc_approval", "require Preventive Care (PC) approval", "", ""),
				option("defer", "defer with reason", "", ""),
			},
		},
		{
			ID: "vaccine_types",
			Options: []domain.Option{
				option("unknown_review_needed", "unknown - review needed", "publishable only when the source explicitly lacks class; fail closed for compatibility planning", "warn"),
				option("live", "live", "", ""),
				option("killed", "killed", "", ""),
				option("toxoid", "toxoid", "", ""),
				option("combo", "combo", "", ""),
			},
		},
		{
			ID: "vaccine_pathogen_classes",
			Options: []domain.Option{
				option("unknown_review_needed", "unknown - review needed", "publishable only when source lacks class; fail closed for same-day planning", "warn"),
				option("bacterial", "bacterial", "", ""),
				option("viral", "viral", "", ""),
				option("mixed", "mixed", "combination vaccine or reviewed combo row", "info"),
			},
		},
		{
			ID: "vaccine_course_types",
			Options: []domain.Option{
				option("single", "single", "source table Type = Single", ""),
				option("booster", "booster", "source table Type = Booster", ""),
			},
		},
		{
			ID: "source_vaccine_matrix_presets",
			Options: []domain.Option{
				option("ET+TT", "ET+TT", "vaccine_type=killed|pathogen_class=bacterial|course_type=booster|disease=Enterotoxaemia + Tetanus|compatibility_group=ET+TT|weeks=4,7|adult_course_gap_days=21|revaccination_days=182|dose_amount=2|vial_doses=100|priority=1|species=all", "info"),
				option("PPR", "PPR", "vaccine_type=live|pathogen_class=viral|course_type=single|disease=Peste des petits ruminants|compatibility_group=PPR|weeks=16|revaccination_days=1095|dose_amount=1|vial_doses=100|priority=2|species=all", "info"),
				option("Goat Pox", "Goat Pox", "vaccine_type=live|pathogen_class=viral|course_type=single|disease=Goat Pox|compatibility_group=Goat Pox|weeks=16|revaccination_days=365|dose_amount=1|vial_doses=25|species=goat", "goat"),
				option("FMD", "FMD", "vaccine_type=killed|pathogen_class=viral|course_type=single|disease=Foot and mouth disease|compatibility_group=FMD|weeks=12|revaccination_days=274|dose_amount=1|vial_doses=30|species=all", "info"),
				option("HS", "HS", "vaccine_type=killed|pathogen_class=bacterial|course_type=single|disease=Haemorrhagic septicaemia|compatibility_group=HS|weeks=12|revaccination_days=365|dose_amount=2|vial_doses=100|species=all", "info"),
				option("Blue Tongue", "Blue Tongue", "vaccine_type=killed|pathogen_class=viral|course_type=booster|disease=Blue Tongue|compatibility_group=Blue Tongue|weeks=16,19|revaccination_days=365|dose_amount=2|vial_doses=100|species=sheep", "sheep"),
				option("Sheep Pox", "Sheep Pox", "vaccine_type=live|pathogen_class=viral|course_type=single|disease=Sheep Pox|compatibility_group=Sheep Pox|weeks=12|revaccination_days=365|dose_amount=1|vial_doses=100|species=sheep", "sheep"),
			},
		},
		{
			ID: "procurement_wave_vaccine_options",
			Options: []domain.Option{
				option("ET+TT", "ET+TT", "", "info"),
				option("PPR", "PPR", "", "info"),
				option("Goat Pox", "Goat Pox", "", "goat"),
				option("Sheep Pox", "Sheep Pox", "", "sheep"),
			},
		},
		{
			ID: "proof_requirement_tokens",
			Options: []domain.Option{
				option("shed", "Pen proof", "capture the pen/tag where the animal was vaccinated", "info"),
				option("vial", "Vial number", "record the vial used for the dose", "info"),
				option("dose", "Dose given", "record the administered dose", "info"),
				option("lot", "Lot number", "record the vaccine batch/lot", "info"),
				option("qty", "Quantity", "record the total quantity used", "info"),
				option("video", "Video proof", "attach execution video when required", "info"),
				option("photo", "Photo proof", "attach photo proof when required", "info"),
			},
		},
		{
			ID: "dose_units",
			Options: []domain.Option{
				option("ml", "ml", "", ""),
				option("dose", "dose", "", ""),
			},
		},
		{
			ID: "route_sites",
			Options: []domain.Option{
				option("subcutaneous", "subcutaneous", "", ""),
				option("intramuscular", "intramuscular", "", ""),
				option("oral", "oral", "", ""),
				option("review_needed", "review needed", "route/site missing from source row", "warn"),
			},
		},
		{
			ID: "course_lapse_policies",
			Options: []domain.Option{
				option("pc_review", "Preventive Care (PC) review", "missed/lapsed course must be reviewed before restart", "warn"),
				option("restart_primary", "restart primary course", "", ""),
				option("continue_next_due", "continue next due", "", ""),
				option("unsupported_review_needed", "unsupported - review needed", "do not silently materialize unsupported lifetime cadence", "warn"),
			},
		},
		{
			ID: "source_systems",
			Options: []domain.Option{
				option("manual_admin", "manual admin (not publishable)", "not_source_backed", "warn"),
				option("vaccinations_db", "Vaccinations DB", "publishable", "ok"),
				option("pc", "Preventive Care (PC)", "publishable", "ok"),
				option("vet", "vet", "publishable", "ok"),
				option("feed_direction_config_pack", "Reviewed Feed Direction config pack", "publishable after source import, validation, preview, and approval", "ok"),
				option("feed_directions_automation_db", "Feed Directions Automation DB (legacy evidence)", "import/parity evidence only until converted into a reviewed config pack", "info"),
				option("counting_db_values", "Counting DB values-only workbook (legacy evidence)", "count/source evidence only; not directly publishable", "info"),
				option("feed_transfer_kt", "Feed transfer KT notes (directional evidence)", "meeting-note evidence only; noisy examples require review", "info"),
			},
		},
		{
			ID: "review_statuses",
			Options: []domain.Option{
				option("extracted", "extracted", "", ""),
				option("reviewed", "reviewed", "", ""),
				option("approved", "approved", "", ""),
			},
		},
		{
			ID: "trigger_types",
			Options: []domain.Option{
				option("birth_age", "birth / age", "Count from the animal's date of birth — for farm-born kids (needs a known DOB).", ""),
				option("post_arrival", "herd entry (procured)", "Count from when a bought-in animal arrived on our farm, after the 7-day warmup hold.", ""),
				option("calendar", "calendar (fixed date)", "Same calendar date for every animal regardless of age — a seasonal or annual whole-herd drive.", ""),
				option("after_previous_completion", "after previous dose", "Count from when the previous dose was actually given — how boosters work.", ""),
				option("manual_campaign", "manual campaign", "Never fires automatically; only when a manager launches a drive by hand (ad-hoc / outbreak).", ""),
			},
		},
		{
			ID: "repeat_policies",
			Options: []domain.Option{
				option("none", "none", "", ""),
				option("every_n_days", "every_n_days", "", ""),
				option("yearly", "yearly", "", ""),
			},
		},
		{
			ID: "catch_up_policies",
			Options: []domain.Option{
				option("immediate", "immediate", "", ""),
				option("next_cycle", "next_cycle", "", ""),
				option("pc_approval", "pc_approval", "", ""),
				option("defer", "defer", "", ""),
			},
		},
		{
			ID:      "schedule_sop_labels",
			Options: []domain.Option{},
		},
		{
			ID: "feed_classes",
			Options: []domain.Option{
				option("all_reviewed_cohorts", "all reviewed cohorts", "Resolver covers every approved feed cohort selected by source rows.", ""),
				option("adult_breed_stage", "adult: breed + pen tag/stage", "Adult ration keys come from reviewed breed plus tag/stage dimensions.", ""),
				option("kid_weight_adg", "kid: weight band + ADG", "Kid ration keys use weight band and target growth, not raw age alone.", ""),
			},
		},
		{
			ID: "feed_items",
			Options: []domain.Option{
				option("reviewed_template_rows", "reviewed template rows", "Quantities vary by imported/reviewed source dimensions; this is not a fixed item.", ""),
			},
		},
		{
			ID: "feed_units",
			Options: []domain.Option{
				option("kg_as_fed", "kg as-fed", "Field packing/distribution quantity; nutrient conversions stay internal.", ""),
				option("g_per_day", "g/day as-fed", "Per-cohort ration-table quantity before pen/session aggregation.", ""),
				option("percent", "percent", "Validation threshold or factor; never promoted from example without review.", ""),
				option("slot_weight_percent", "slot weight %", "Serving slot split weight; active slots must cover the full daily quantity.", ""),
			},
		},
		{
			ID: "feed_inventory_policies",
			Options: []domain.Option{
				option("reserve_consume_release", "reserve -> consume -> release", "Reserve before packing, consume verified quantities, release unused stock.", "ok"),
				option("preview_only", "preview only - no stock lock", "Used before the packing/staging point; no inventory move is written.", "info"),
				option("manual_bridge_review", "manual bridge review", "High-priority additions require proof and reconciliation state before closure.", "warn"),
			},
		},
		{
			ID: "feed_source_tables",
			Options: []domain.Option{
				option("counting_db_counts", "Counting DB count tabs", "Date/Farm/Pen/Pen Tag/Breed/Age/Count source grain.", ""),
				option("feed_validation_tables", "CPT/CBE Validation + Validation-BW", "Feed vectors, energy, DM, wastage, breed/tag requirements, and BW thresholds.", ""),
				option("feed_energy_protein", "Feed-Energy-Protein", "Breed and pregnancy/non-pregnancy/mother/milking energy/protein rows.", ""),
				option("supply_planning", "CPT/CBE Supply Planning", "Per-feed quantity planning by date, farm, pen, tag, breed, age, count.", ""),
				option("session_template", "Template", "Per-farm sessions and feed sets; default slots are not code constants.", ""),
				option("execution_forms", "Packing, transport, consumption/wastage forms", "Proof, processed, and reconciliation evidence for stage obligations.", ""),
			},
		},
		{
			ID: "feed_parameter_families",
			Options: []domain.Option{
				option("feed_vectors", "feed vectors", "Energy, dry-matter factor, wastage factor, net energy, cost.", ""),
				option("nutrition_cohort_keys", "nutrition cohort keys", "Breed, tag/stage, weight band, ADG, pregnancy, warm-up, feed-type policy.", ""),
				option("ration_outputs", "ration outputs", "Reviewed solver/import output rows per cohort and feed item.", ""),
				option("session_slot_policy", "session slot policy", "Admin-managed slots, times, weights, feed inclusion, and effective dates.", ""),
				option("proof_reconciliation", "proof + reconciliation policy", "Packing, transport, consumption, wastage, bridge proof, and reconciliation state.", ""),
			},
		},
		{
			ID: "feed_dimension_keys",
			Options: []domain.Option{
				option("park_shed_breed_horizon", "park + pen + breed + horizon", "Physical count/projection grain from Counts/Shifting.", ""),
				option("shed_tag_stage", "pen tag / stage", "Resolver evidence; do not infer from workbook text alone.", ""),
				option("breed_alias", "breed alias", "Normalize source labels before count matching and ration lookup.", ""),
				option("age_stage", "age / stage", "Normalize Age through reference data; raw age buckets are not ration keys.", ""),
				option("sex_tag", "sex tag", "Includes F2-Male/F2-Female style tag contamination that must be resolved.", ""),
				option("kid_weight_band_adg", "kid weight band + ADG", "Kid ration dimensions from weight/growth policy.", ""),
				option("pregnancy_lactation_warmup", "pregnancy / lactation / warm-up", "Special policy groups require explicit reviewed treatment.", ""),
			},
		},
		{
			ID: "feed_ratio_policies",
			Options: []domain.Option{
				option("source_row_variable", "source-row variable; 80/20 is only an example", "Every ratio/quantity must come from a reviewed parameter row or solver output.", "warn"),
				option("docx_default_50_50_slots", "docx default: two slots, 50/50 until changed", "Serving split starts from Feed, Shiftings and Count.docx and changes only through effective-dated config.", "info"),
			},
		},
		{
			ID: "feed_validation_checks",
			Options: []domain.Option{
				option("source_hash_required", "source hash required", "Every import/config row carries source checksum and reviewer metadata.", ""),
				option("alias_reference_match", "alias/reference match", "Unresolved breed, tag, age/stage, sex, feed item, or unit blocks.", ""),
				option("resolver_coverage", "resolver coverage", "Every pen+breed projection row must resolve to approved ration cohort context.", ""),
				option("ratio_not_global", "ratio is not global", "80/20 and similar values remain row-level reviewed data, not code constants.", ""),
				option("slot_weights_sum", "slot weights cover 100%", "Active slot weights must sum to the full daily as-fed quantity.", ""),
				option("quantity_numeric_asfed", "quantity is numeric as-fed", "Field instructions use as-fed quantity; DM/wastage are internal calculations.", ""),
				option("formula_parity_preview", "formula parity preview", "Dry-run compares workbook/source expected output before publish.", ""),
				option("broken_reference_block", "broken reference blocks", "Broken references, missing formulas, invalid dimensions, or mismatch errors go to repair/DLQ.", "warn"),
			},
		},
		{
			ID: "feed_calculation_outputs",
			Options: []domain.Option{
				option("ration_per_cohort", "ration per cohort", "Per approved cohort and feed item.", ""),
				option("projected_count_join", "projected count join", "Join count projection to ration context through reviewed resolver.", ""),
				option("daily_asfed_shed_total", "daily as-fed pen total", "Daily pen/feed totals before slot split.", ""),
				option("session_split_quantities", "session split quantities", "Per slot and feed item, using published slot weights.", ""),
				option("stage_obligations", "stage obligations", "Packing, transport, consumption/wastage, bridge, proof, verification, rework.", ""),
				option("reconciliation_state", "reconciliation state", "Generated, packed, transported, consumed, wasted, proofed, reconciled.", ""),
			},
		},
		{
			ID: "protocol_rule_status",
			Options: []domain.Option{
				option("published", "Published", "", "ok"),
				option("retired", "Retired", "", "mut"),
				option("draft", "Draft", "save, preview, SOP, proof, and impact checks decide publish readiness", "info"),
			},
		},
	}
}

// sopOptionGroupsFor is sopOptionGroups with the builder's STARTER QUESTIONS chosen for the
// module page. The starter set used to be the vaccination session for every page, so "New SOP" on
// /feed/sops opened a form asking for a vaccine batch, cold chain and dose volume (found
// 2026-09-16). Each module now starts from the questions its own work actually captures; the
// author edits from there.
func sopOptionGroupsFor(pageID string) []domain.OptionGroup {
	groups := sopOptionGroups()
	seed, ok := sopSeedStepsByModule[pageID]
	if !ok {
		return groups
	}
	for i := range groups {
		if groups[i].ID == "sop_seed_steps" {
			groups[i] = domain.OptionGroup{ID: "sop_seed_steps", Options: seed}
		}
	}
	return groups
}

// sopSeedStepsByModule is the builder's starter question list per module SOP page. Keys are
// sop_step_types keys. Vaccination keeps the historical default in sopOptionGroups.
var sopSeedStepsByModule = map[string][]domain.Option{
	"feed-sops": {
		option("shed_picker", "Pen", "", ""),
		option("number", "Session", "", ""),
		option("video_proof", "Proof video of the feed", "", ""),
	},
	"milk-sops": {
		option("shed_picker", "Pen", "", ""),
		option("number", "Quantity prepared (litres)", "", ""),
		option("video_proof", "Proof video of the feeding", "", ""),
	},
	"weighing-sops": {
		option("shed_picker", "Pen", "", ""),
		option("goat_scan", "Scan Animal ID", "", ""),
		option("number", "Weight (kg)", "", ""),
		option("video_proof", "Proof video of the weighing", "", ""),
	},
	"counts-sops": {
		option("shed_picker", "Pen", "", ""),
		option("goat_scan", "Scan Animal ID", "", ""),
		option("video_proof", "Proof video", "", ""),
	},
	"procurement-sops": {
		option("goat_scan", "Scan Animal ID", "", ""),
		option("photo_proof", "Photo of the animal", "", ""),
	},
	// A general work instruction has no animal and no module: it starts from a plain checklist,
	// not the vaccination drive's batch picker and animal scan (PR 308 review).
	"configuration-work-instructions": {
		option("yesno", "Is everything in place to start?", "", ""),
		option("text", "Note what you found", "", ""),
		option("photo_proof", "Photo of the finished work", "", ""),
	},
	// A new Preventive Care SOP starts from the animal in front of the operator.
	"pc-care-sops": {
		option("animal_id_scan", "Scan the animal", "", ""),
		option("video_proof", "Record the dose being given", "", ""),
		option("yesno", "Did the animal take the full dose?", "", ""),
	},
	// A new Sales SOP starts from what a sale owes, not an animal scan.
	"sales-sops": {
		option("yesno", "Has the buyer paid in full?", "", ""),
		option("video_proof", "Record the animals being loaded", "", ""),
		option("photo_proof", "Photo of the gate pass", "", ""),
	},
}

func sopOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		// domain_chips is retired with the SOP split (maintainer decision 2026-08-18): each
		// module page is pre-scoped to its own SOP codes, so there is no cross-domain filter.
		{
			ID: "sop_trigger_chips",
			Options: []domain.Option{
				option("form", "Form", "", ""),
				option("cron", "Schedule / cron", "", ""),
				option("sensor", "Sensor", "", ""),
				option("manual", "Manual", "", ""),
			},
		},
		{
			ID: "sop_seed_steps",
			Options: []domain.Option{
				option("vaccine_batch_picker", "Select vaccine + batch (FEFO lot)", "", ""),
				option("yesno", "Cold-chain intact (2-8°C)?", "", ""),
				option("goat_scan", "Scan Animal ID", "", ""),
				option("number", "Dose volume administered (ml)", "", ""),
				option("select", "Route / site", "", ""),
				option("video_proof", "Upload administration proof video", "", ""),
			},
		},
		{
			ID: "sop_step_types",
			Options: []domain.Option{
				option("text", "text", "", ""),
				option("number", "number", "", ""),
				option("yesno", "yes/no", "", ""),
				option("select", "select", "", ""),
				option("multiselect", "multiselect", "", ""),
				option("goat_scan", "Animal ID scan", "", ""),
				option("shed_picker", "pen picker", "", ""),
				option("vaccine_batch_picker", "vaccine batch picker", "", ""),
				option("medicine_picker", "medicine picker", "", ""),
				option("photo_proof", "photo proof", "", ""),
				option("video_proof", "video proof", "", ""),
			},
		},
		{
			ID: "sop_on_answer_actions",
			Options: []domain.Option{
				option("none", "— no rule —", "", ""),
				option("require_if", "require this if previous answered", "", ""),
				option("require_proof", "require proof if answered", "", ""),
				option("block_if_empty", "block submission if empty", "", ""),
			},
		},
		{
			// Conditional-visibility operators for the full-page builder ("show only when the answer
			// to Question N …"). Keys map 1:1 to backend form_dsl condition operators in
			// sop/app/service.go supportedConditionOperator (answered→not_empty, not_answered→empty).
			ID: "sop_condition_operators",
			Options: []domain.Option{
				option("answered", "is answered", "", ""),
				option("not_answered", "is not answered", "", ""),
				option("equals", "equals", "", ""),
				option("not_equals", "does not equal", "", ""),
				option("is_one_of", "is one of", "", ""),
				option("gt", "is greater than", "", ""),
				option("gte", "is at least", "", ""),
				option("lt", "is less than", "", ""),
				option("lte", "is at most", "", ""),
			},
		},
		{
			ID: "proof_types",
			Options: []domain.Option{
				option("video", "video proof", "", ""),
				option("photo", "photo proof", "", ""),
			},
		},
		// SOP-DRIVEN HERD OPERATIONS (maintainer decision 2026-09-13): the follow-up step
		// editor. sop_task_types / sop_task_type_answer_kinds are declared EMPTY here and
		// filled from the tenant's Task Type Registry in compilePages (never constants).
		{ID: "sop_task_types"},
		// SALES SOP (2026-09-19): the step editor's "Done by" designations, filled from the
		// designation catalog by the compiler (never a constant list here).
		{ID: "sop_step_owners"},
		{ID: "sop_task_type_answer_kinds"},
		{
			// When a step is due, relative to the event the workflow opened on. Keys are the
			// follow_up schedule kinds tasks/domain.CompileTrack accepts.
			ID: "sop_schedule_kinds",
			Options: []domain.Option{
				option("immediately", "Right away", "Due the moment the workflow opens.", ""),
				option("after_event", "After the event", "Due N minutes after the birth / death / raise moment.", ""),
				option("at_fixed_time", "At a fixed time", "Due at HH:MM IST on the event day or a later day.", ""),
				option("series", "Repeating series", "One step per listed time on the event day (when still ahead) and each following day.", ""),
				option("after_step", "After another step", "Due N minutes after the named earlier step is completed.", ""),
			},
		},
		{
			ID: "sop_step_conditions",
			Options: []domain.Option{
				option("", "Always", "", ""),
				option("kid_pen_unresolved", "Only when the kid pen could not be resolved", "", ""),
			},
		},
		{
			ID: "sop_step_sections",
			Options: []domain.Option{
				option("main", "Main sequence", "Runs in order; each step waits for the one before it.", ""),
				option("colostrum_session", "Colostrum rounds", "Counted on the Colostrum page; hard time-gated.", ""),
			},
		},
		{
			ID: "subject_scopes",
			Options: []domain.Option{
				option("batch", "subject: batch", "", ""),
				option("goat", "subject: per-goat", "", ""),
			},
		},
		{
			// Animal ID capture mode: single animal vs the whole-shed batch (many animals in one
			// session) — the real vaccination drive is grouped by shed, never random goats.
			ID: "sop_scan_modes",
			Options: []domain.Option{
				option("single", "Single goat", "", ""),
				option("multi", "Multiple goats (whole pen / batch)", "", ""),
			},
		},
	}
}

func genericOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "filter_quick_terms",
			Options: []domain.Option{
				option("all", "All", "", "info"),
				option("due", "Due", "", "warn"),
				option("done", "Done", "", "ok"),
				option("pending", "Pending", "", "info"),
				option("overdue", "Overdue", "", "dng"),
				option("review", "Review", "", "warn"),
			},
		},
		{
			ID: "yes_no",
			Options: []domain.Option{
				option("yes", "Yes", "", "ok"),
				option("no", "No", "", "mut"),
			},
		},
		{
			ID:      "park_display_chips",
			Options: []domain.Option{},
		},
		{
			ID: "adverse_reaction",
			Options: []domain.Option{
				option("none", "None", "", "ok"),
				option("mild", "Mild", "", "warn"),
				option("severe", "Severe", "", "dng"),
			},
		},
	}
}

func withGenericOptionGroups(groups []domain.OptionGroup) []domain.OptionGroup {
	return append(genericOptionGroups(), groups...)
}

// liveTrackerOptionGroups is the /vaccination/live-tracker vocabulary.
//
// Every one of these is a CLOSED set defined by this read model's own state machine (an operator is
// exactly one of four states; a shed exactly one of five; an activity row exactly one of six kinds),
// so declaring them here freezes nothing that live tenant data could extend. Parks, sheds, operators
// and vaccines are deliberately absent: those are live rows and arrive in the response's own
// filter_options, which is how an option that matches zero administrations becomes impossible to
// offer.
func liveTrackerOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "live_refresh_interval",
			Options: []domain.Option{
				option("10", "10s", "Refresh every 10 seconds", ""),
				option("30", "30s", "Refresh every 30 seconds", ""),
				option("60", "1m", "Refresh every minute", ""),
			},
		},
		{
			ID: "live_operator_state",
			Options: []domain.Option{
				option("active", "active now", "producing proof or scans right now", "live"),
				option("done", "done", "every assigned administration is closed", "ok"),
				option("not_started", "not started", "no proof and no scan yet today", "dng"),
				option("idle", "idle", "no activity for over 90 minutes with work remaining", "dng"),
			},
		},
		{
			// The mock's legend declares four shed states but its rows render five. `review` is the
			// fifth — a shed that finished but re-scanned animals along the way — and it is declared
			// here so the legend and the rows finally agree.
			ID: "live_shed_state",
			Options: []domain.Option{
				option("receiving", "receiving", "proof landing at a normal rate", "live"),
				option("slow", "slow start", "under a quarter done well into the drive", "warn"),
				option("review", "extra attempts", "finished, but animals were re-scanned — flagged for the verifier", "warn"),
				option("done", "done", "every administration closed, no extra attempts", "ok"),
				option("not_started", "not started", "no proof received yet", "dng"),
			},
		},
		{
			ID: "live_proof_state",
			Options: []domain.Option{
				option("video", "1 video", "one completed video proof for this animal today", "ok"),
				option("uploading", "uploading…", "a proof upload is in flight", "warn"),
				option("none", "—", "no proof yet", "mut"),
			},
		},
		{
			ID: "live_dose_state",
			Options: []domain.Option{
				option("closed", "closed", "obligation completed", "ok"),
				option("verification_pending", "awaiting close", "a proof landed for this animal today; this obligation is not closed yet", "warn"),
				option("awaiting_proof", "awaiting proof", "in progress, proof not yet landed", "mut"),
				option("scheduled", "scheduled", "not started yet", "mut"),
				option("missed", "missed", "window closed with no proof", "dng"),
			},
		},
		{
			ID: "live_activity_kind",
			Options: []domain.Option{
				option("proof_video", "video proof landed", "", "ok"),
				option("scan_capture", "scan capture", "", "info"),
				option("scan_duplicate", "duplicate scan attempt", "", "warn"),
				option("scan_unknown", "unrecognised scan attempt", "", "warn"),
				option("administration", "dose recorded", "", "pur"),
				// obligation_status_events is per-ANIMAL closure. Calling it "pen submitted" named a
				// shed-level action on an animal-grain count; a real shed submission lives in
				// sop_submissions and is not read by this feed at all.
				option("obligation_closed", "obligation closed", "", "info"),
			},
		},
		{
			ID: "live_attention_kind",
			Options: []domain.Option{
				option("extra_attempts", "extra attempts", "same animals re-scanned; duplicates ignored, flagged for verifier note", "warn"),
				option("idle_operator", "idle operator", "no field activity for over 90 minutes with work remaining", "dng"),
				option("slow_shed", "slow pen", "well under a quarter done this far into the drive", "warn"),
			},
		},
		{
			ID: "live_status_filter",
			Options: []domain.Option{
				option("active", "Active now", "", "live"),
				option("done", "Done", "", "ok"),
				option("pending", "Not started", "", "dng"),
				option("review", "Needs review", "", "warn"),
			},
		},
	}
}

// countsBreakdownOptionGroups holds only the option group whose vocabulary is a fixed schema
// constraint: sex is CHECK (female|male) on goats, so it can be declared here.
//
// Farm, shed, breed and stage are deliberately NOT declared here. Their values are live tenant
// data, so hardcoding them would be exactly the "CBE/CPT-style constants in backend contract
// code" the frontend rule bans. Farm and shed come from /locations; breed and stage come from
// the breakdown response's own `facets`, which reports the values actually present — so a
// dropdown can never offer an option that matches zero rows. That matters most for stage:
// animal_stage_lookup is joined to goats through shed_profiles, not through
// goats.management_stage, so the lookup and the column can legitimately disagree.
func countsBreakdownOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "counts_gender",
			Options: []domain.Option{
				option("female", "Female", "", ""),
				option("male", "Male", "", ""),
			},
		},
		{
			// The BREED CATALOG, for the inline breed correction. Declared empty here and filled by
			// the compiler from the live `breeds` reference family: breeds are tenant data and must
			// never be constants in contract code.
			//
			// Deliberately the CATALOG rather than the response's `facets.breeds`. A facet reports
			// the breeds already ON the herd, and a correction frequently needs one that is not --
			// that is the whole point of correcting a wrongly recorded breed. This is the same
			// write-picker-versus-census-facet distinction the operational-location rule draws for
			// sheds.
			ID:      "counts_breed",
			Options: []domain.Option{},
		},
	}
}

// feedOptionGroups holds ONLY the Feed vocabularies that are FIXED SCHEMA CONSTRAINTS —
// closed sets defined by a CHECK constraint or by a structural two-way distinction in
// migrations/postgres/000011_feed_ration_config.sql. Each is safe to declare here because no
// tenant can add a value to it without a migration.
//
// Deliberately NOT declared here, because they are LIVE TENANT DATA and hardcoding them would
// be exactly the "CBE/CPT-style constants in backend contract code" the frontend rule bans:
//
//   - parks and sheds          -> locations (/locations, and the response's own facets)
//   - breeds                   -> live goats.breed
//   - ration groups            -> feed_ration_groups (authored; carries the Beetal/Sirohi merge)
//   - shed tags                -> feed_shed_tags (authored, 31 rows today, tenant-editable)
//   - feed items               -> feed_item_catalog, injected as the `feed_items` group from
//     ReferenceFamilies.FeedItems in compilePages — that is the intended injection path for live
//     feed vocabulary, not a constant list here.
//   - SESSIONS                 -> feed_session_templates. A park's sessions are AUTHORED per park
//     (session_no, label, split_fraction); the only schema constraint is session_no >= 1. Two
//     sessions a day is today's seeded configuration, not a schema rule, so declaring
//     `session ∈ {1,2}` here would freeze one tenant's authored config into the contract and
//     silently mislabel any park that later runs a third session.
func feedOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			// Structural: a shed is fed EITHER from the ration grid (per-head derived) OR from
			// feed_experiment_config (hand-entered absolute kg for the operational pen). There is no
			// third path, and the distinction changes what the number means.
			ID: "feed_workflow",
			Options: []domain.Option{
				option("normal", "Per-head (normal)", "Quantity derived: projected head count × grams per head × feed factor", "ok"),
				option("experiment", "Experiment (hand-entered)", "Hand-entered grams per animal for the operational pen, multiplied by its live head count; an older cell holds a whole-pen total that is multiplied by nothing", "pur"),
			},
		},
		{
			// Structural, and the safety-critical one. grams_per_head is NOT NULL with no default
			// and absence of a row is the ONLY encoding of "not configured", so a row is exactly
			// one of: a rate was found (ok) or none exists (blocked). "Blocked" is never a
			// quantity of zero — see the CONFIGURED ZERO block in 000003.
			ID: "feed_row_status",
			Options: []domain.Option{
				option("ok", "Planned", "A rate is configured and a quantity was computed", "ok"),
				option("blocked", "Blocked", "No authored ration for this group/tag/item — this pen will NOT be fed until one exists. Not zero kg.", "dng"),
			},
		},
		{
			// Structural companion to feed_row_status: how a computed quantity reads. An authored
			// 0 and a missing rate are different states with opposite consequences, so they are
			// separate options rather than one "zero" bucket.
			ID: "feed_quantity_states",
			Options: []domain.Option{
				option("planned", "Planned", "Authored rate greater than zero", "ok"),
				option("configured_zero", "Configured zero", "Authored 0 g/head — correct and deliberate (K0/K1 kids on milk). The pen IS configured.", "info"),
				option("not_configured", "No ration configured", "No authored rate at all. Blocking, not zero.", "dng"),
			},
		},
		{
			// Structural: feed_item_catalog_status_check (000001) constrains status to exactly
			// these two values, so no tenant can add a third without a migration.
			//
			// This is the EDIT vocabulary of the feed-items status cell, which is why it is an
			// option group rather than two `label.feed_item_*` copy keys: the cell became an inline
			// picker offering BOTH states at once, and a picker's options must be backend-declared
			// so a client cannot offer a value the write would reject. The stored value stays
			// `retired`; the operator's word for it is "Inactive", which is exactly the storage-vs-
			// display split an option group exists to carry.
			ID: "feed_item_status",
			Options: []domain.Option{
				option("active", "Active", "This item is part of the feed vocabulary. It appears on the ration grid and is packed and served wherever a rate is authored for it.", "ok"),
				option("retired", "Inactive", "This item is not being fed. It is on no feed sheet and its authored rates are hidden from the ration grid — but they are kept, so reactivating it restores them.", "mut"),
			},
		},
		{
			// Structural: valid_to IS NULL (in force) vs closed by a later edit. Effective-dating
			// is enforced by the feed_ration_rates_open_row_uidx partial unique index.
			ID: "feed_effective_window",
			Options: []domain.Option{
				option("in_force", "In force", "No end date — the rate currently applied", "ok"),
				option("superseded", "Superseded", "Closed by a later edit; kept so past feed sheets stay explainable", "mut"),
			},
		},
		{
			// The comparison vocabulary of the ration grid's grams filter. Backend-owned like every
			// other option group: the keys are the wire enum /feed-config/ration-rates accepts on
			// grams_op, and the labels are the operator's words for them. A client that invented its
			// own list could offer an operator the query rejects.
			ID: "feed_grams_compare",
			Options: []domain.Option{
				option("gt", "More than", "Only rates above the value — the usual way to hide authored zeros", ""),
				option("gte", "At least", "", ""),
				option("eq", "Exactly", "With 0, isolates the deliberate zeros (milk-fed K0/K1 kids)", ""),
				option("lte", "At most", "", ""),
				option("lt", "Less than", "", ""),
				option("neq", "Not", "", ""),
			},
		},
		{
			// CHECK (applies_to = ANY (ARRAY['adult','kid'])) on feed_shed_tags.
			ID: "feed_shed_tag_applies_to",
			Options: []domain.Option{
				option("kid", "Kid course", "", ""),
				option("adult", "Adult course", "", ""),
			},
		},
		{
			// CHECK (status = ANY (ARRAY['active','retired'])) on feed_shed_tags,
			// feed_item_catalog, feed_session_templates, feed_conversions and
			// feed_experiment_config.
			ID: "feed_config_status",
			Options: []domain.Option{
				option("active", "Active", "", "ok"),
				option("retired", "Retired", "", "mut"),
			},
		},
		{
			// Fixed unit vocabulary of the schema: rates are authored in grams per head per day
			// (feed_ration_rates.grams_per_head), experiment sheds in absolute kg
			// (feed_experiment_config.absolute_kg), and the sheet is served in kg as-fed.
			ID: "feed_quantity_units",
			Options: []domain.Option{
				option("g_per_head_per_day", "g/head/day", "Authored ration rate, before the feed factor and session split", ""),
				option("kg", "kg as-fed", "Computed pen/session quantity, and the unit experiment pens are authored in", ""),
			},
		},
		{
			// ReadinessStatus in feed/domain: ready | blocked | pending. A Go-level closed set,
			// not tenant data — generation is allowed only when the bounded counts projection
			// page carries no blockers.
			ID: "feed_generation_readiness",
			Options: []domain.Option{
				option("ready", "Ready", "The counts projection this day is built from carries no blockers", "ok"),
				option("blocked", "Blocked", "Unresolved projection blockers — a partial feed sheet is not published", "dng"),
				option("pending", "Pending", "The counts projection for this day is still being built", "warn"),
			},
		},
	}
}

func herdRegisterOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "herd_sex",
			Options: []domain.Option{
				option("female", "Female", "", ""),
				option("male", "Male", "", ""),
			},
		},
		{
			ID: "herd_species",
			Options: []domain.Option{
				option("goat", "Goat", "", ""),
				option("sheep", "Sheep", "", ""),
			},
		},
		{
			ID: "herd_origin",
			Options: []domain.Option{
				option("birth", "Farm-born (birth)", "", ""),
				option("procured", "Procured", "", ""),
				option("imported", "Imported", "", ""),
			},
		},
		{
			ID: "herd_filter_breeds",
			Options: []domain.Option{
				option("Malai", "Malai", "", ""),
				option("Beetal", "Beetal", "", ""),
				option("Sojat", "Sojat", "", ""),
				option("Osmanabadi", "Osmanabadi", "", ""),
				option("Boer", "Boer", "", ""),
				option("Anantapur", "Anantapur", "", ""),
				option("Kenguri", "Kenguri", "", ""),
			},
		},
		{
			ID: "herd_filter_sexes",
			Options: []domain.Option{
				option("male", "Male", "", ""),
				option("female", "Female", "", ""),
			},
		},
		{
			ID: "herd_filter_extra_facets",
			Options: []domain.Option{
				option("age_cohort", "Age Cohort", "K0 / K1 / K2", ""),
				option("shed", "Pen", "ICU-1 / K3 / Mother", ""),
				option("repro_stage", "Pregnancy / Lactation Stage", "pregnant / milking / open", ""),
				option("lifecycle", "Status", "active / under_treatment / sold", ""),
				option("source_entry", "Source Entry State", "pending / accepted / blocked", ""),
				option("origin_farm", "Origin Farm", "source farm", ""),
				option("days_in_stage", "Days in Stage (min/max)", "e.g. 7-30", ""),
				option("weight_kg", "Weight (kg) (min/max)", "e.g. 20-35", ""),
				option("adg", "ADG (g/day) (min/max)", "e.g. 80-140", ""),
			},
		},
		{
			ID: "herd_import_columns",
			Options: []domain.Option{
				option("farm", "Farm", "", ""),
				option("animal_identifier_1", "Tag 1", "", ""),
				option("animal_identifier_2", "Tag 2", "", ""),
				option("species", "Species", "", ""),
				option("park", "Park", "", ""),
				option("shed", "Pen name", "", ""),
				option("partition_label", "Partition", "", ""),
				option("breed", "Breed", "", ""),
				option("sex", "Sex", "", ""),
				option("dob", "DOB", "", ""),
				option("management_stage", "Management stage", "", ""),
				option("entry_date", "Entry date", "", ""),
				option("weight_kg", "Weight(kg)", "", ""),
				option("dam_id", "Dam ID", "", ""),
				option("sire_or_lot", "Sire/lot", "", ""),
				option("origin", "Origin", "", ""),
				option("photo_url", "Photo URL", "", ""),
			},
		},
		{
			ID: "shed_import_columns",
			Options: []domain.Option{
				option("park", "Park", "", ""),
				option("shed_code", "Pen code", "", ""),
				option("shed_name", "Pen name", "", ""),
				option("display_order", "Display order", "", ""),
				option("notes", "Notes", "", ""),
			},
		},
		{
			ID: "herd_bulk_decisions",
			Options: []domain.Option{
				option("create", "create", "", "ok"),
				option("requires_review", "requires review", "", "warn"),
				option("conflict", "conflict", "", "dng"),
				option("skip", "skip", "", "mut"),
			},
		},
		// herd_reproductive is a source-backed live-data family: compilePages replaces these (empty)
		// options with the tenant's active reproductive status_definitions so the edit drawer never
		// hardcodes a reproductive vocabulary. Empty here means "unavailable" until seeded.
		{
			ID:      "herd_reproductive",
			Options: []domain.Option{},
		},
	}
}

func calendarOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "calendar_months",
			Options: []domain.Option{
				option("0", "January", "", ""),
				option("1", "February", "", ""),
				option("2", "March", "", ""),
				option("3", "April", "", ""),
				option("4", "May", "", ""),
				option("5", "June", "", ""),
				option("6", "July", "", ""),
				option("7", "August", "", ""),
				option("8", "September", "", ""),
				option("9", "October", "", ""),
				option("10", "November", "", ""),
				option("11", "December", "", ""),
			},
		},
		{
			ID: "calendar_weekdays",
			Options: []domain.Option{
				option("0", "Sun", "", ""),
				option("1", "Mon", "", ""),
				option("2", "Tue", "", ""),
				option("3", "Wed", "", ""),
				option("4", "Thu", "", ""),
				option("5", "Fri", "", ""),
				option("6", "Sat", "", ""),
			},
		},
		{
			ID: "calendar_view_tabs",
			Options: []domain.Option{
				option("week", "Week", "", ""),
				option("month", "Month", "", ""),
				option("history", "History", "", ""),
			},
		},
		{
			ID: "calendar_owner_tabs",
			Options: []domain.Option{
				option("all", "All", "All owner lanes", ""),
				option("pc", "Preventive Care (PC)", "Preventive Care (PC)", ""),
				option("inventory", "Inventory / Stock", "Inventory / Stock", ""),
				option("admin_data_ops", "Admin / Data Ops", "Admin / Data Ops", ""),
			},
		},
		{
			ID: "calendar_workstream_tabs_all",
			Options: []domain.Option{
				option("vaccination", "Vaccination", "Calendar is currently showing the Preventive Care (PC) vaccination module.", ""),
			},
		},
		{
			ID: "calendar_workstream_tabs_pc",
			Options: []domain.Option{
				option("vaccination", "Vaccination", "Calendar is currently showing the Preventive Care (PC) vaccination module.", ""),
			},
		},
		{
			ID: "calendar_workstream_tabs_inventory",
			Options: []domain.Option{
				option("all_inventory_stock", "All Inventory / Stock", "", ""),
				option("stock_readiness", "Stock readiness", "Sub-workstream filters need backend event_type support; this row is the module context.", ""),
				option("cold_chain", "Cold chain", "Sub-workstream filters need backend event_type support; this row is the module context.", ""),
				option("reorder_expiry", "Reorder / expiry", "Sub-workstream filters need backend event_type support; this row is the module context.", ""),
				option("grn_fefo", "GRN / FEFO", "Sub-workstream filters need backend event_type support; this row is the module context.", ""),
			},
		},
		{
			ID: "calendar_workstream_tabs_admin_data_ops",
			Options: []domain.Option{
				option("all_admin_data_ops", "All Admin / Data Ops", "", ""),
				option("source_review", "Source review", "Sub-workstream filters need backend event_type support; this row is the module context.", ""),
				option("config_approval", "Config approval", "Sub-workstream filters need backend event_type support; this row is the module context.", ""),
				option("import_replay", "Import / replay", "Sub-workstream filters need backend event_type support; this row is the module context.", ""),
				option("audit_follow_up", "Audit follow-up", "Sub-workstream filters need backend event_type support; this row is the module context.", ""),
			},
		},
		{
			ID: "calendar_rhythm_days_all",
			Options: []domain.Option{
				option("Mon", "PLAN", "", "plan"),
				option("Tue", "LOGISTICS", "", "log"),
				option("Wed", "EXECUTE", "", "exec"),
				option("Thu", "EXECUTE", "", "exec"),
				option("Fri", "EXECUTE", "", "exec"),
				option("Sat", "EXECUTE", "", "exec"),
				option("Sun", "REST", "", "rest"),
			},
		},
		{
			ID: "calendar_rhythm_days_pc",
			Options: []domain.Option{
				option("Mon", "PLAN", "", "plan"),
				option("Tue", "PREP", "", "log"),
				option("Wed", "DRIVE", "", "exec"),
				option("Thu", "DRIVE", "", "exec"),
				option("Fri", "VERIFY", "", "exec"),
				option("Sat", "CATCH-UP", "", "exec"),
				option("Sun", "REST", "", "rest"),
			},
		},
		{
			ID: "calendar_rhythm_days_inventory",
			Options: []domain.Option{
				option("Mon", "COUNT", "", "plan"),
				option("Tue", "FEFO", "", "log"),
				option("Wed", "ISSUE", "", "exec"),
				option("Thu", "MONITOR", "", "exec"),
				option("Fri", "REORDER", "", "log"),
				option("Sat", "CLOSE", "", "exec"),
				option("Sun", "REST", "", "rest"),
			},
		},
		{
			ID: "calendar_rhythm_days_admin_data_ops",
			Options: []domain.Option{
				option("Mon", "REVIEW", "", "plan"),
				option("Tue", "CONFIG", "", "log"),
				option("Wed", "IMPORT", "", "exec"),
				option("Thu", "AUDIT", "", "exec"),
				option("Fri", "APPROVE", "", "log"),
				option("Sat", "FOLLOW-UP", "", "exec"),
				option("Sun", "REST", "", "rest"),
			},
		},
		{
			ID: "calendar_event_types",
			Options: []domain.Option{
				option("vaccination_dose_due", "Dose due", "", ""),
				option("vaccination_drive", "Pen / cohort drive", "", ""),
				option("vaccination_history", "Completed vaccination history", "", ""),
				option("vaccination_campaign", "Campaign / catch-up", "", ""),
				option("vaccination_booster_due", "Booster due", "", ""),
				option("vaccination_defer_review", "Defer / waiver review", "", ""),
				option("vaccination_evidence_review", "HF / historical evidence review", "", ""),
				option("vaccination_proof_verification", "Proof verification", "", ""),
				option("vaccination_rework_due", "Rework due", "", ""),
				option("vaccine_stock_readiness", "Stock readiness", "", ""),
				option("vaccine_cold_chain_check", "Cold-chain check", "", ""),
				option("vaccine_reorder_expiry_grn", "Reorder / expiry / GRN", "", ""),
				option("pc_stock_anti_misuse", "Stock anti-misuse", "", ""),
				option("vaccination_config_activation_review", "Config activation review", "", ""),
			},
		},
		{
			ID: "calendar_status",
			Options: []domain.Option{
				option("scheduled", "Scheduled", "", "mut"),
				option("due", "Due", "", "warn"),
				option("overdue", "Overdue", "", "dng"),
				option("missed", "Missed", "", "dng"),
				option("in_progress", "In progress", "", "info"),
				option("proof_pending", "Proof pending", "", "warn"),
				option("verification_pending", "Verification pending", "", "warn"),
				option("rejected", "Rejected", "", "dng"),
				option("rework_due", "Rework due", "", "warn"),
				option("deferred", "Deferred", "", "pur"),
				option("blocked", "Blocked", "", "dng"),
				option("completed", "Completed", "", "ok"),
				option("canceled", "Canceled", "", "mut"),
			},
		},
		{
			ID: "calendar_severity",
			Options: []domain.Option{
				option("info", "Info", "", "info"),
				option("warning", "Warning", "", "warn"),
				option("critical", "Critical", "", "dng"),
			},
		},
		{
			ID: "calendar_links",
			Options: []domain.Option{
				option("vaccination", "vaccination", "", "teal"),
				option("drive", "drive", "", "teal"),
				option("workflow", "workflow record", "", "info"),
				option("action_center", "action center", "", "info"),
				option("adherence", "adherence", "", "warn"),
				option("audit", "audit", "", "mut"),
			},
		},
		{
			ID: "calendar_reminder_state",
			Options: []domain.Option{
				option("not_scheduled", "not scheduled", "", "mut"),
				option("scheduled", "scheduled", "", "info"),
				option("queued", "queued", "", "info"),
				option("nudged", "nudged", "", "info"),
				option("snoozed", "snoozed", "", "pur"),
				option("sent", "sent", "", "ok"),
				option("escalated", "escalated", "", "dng"),
			},
		},
		{
			ID: "calendar_history_status",
			Options: []domain.Option{
				option("open", "open", "", "warn"),
				option("queued", "queued", "", "info"),
				option("active", "active", "", "warn"),
				option("acknowledged", "acknowledged", "", "info"),
				option("resolved", "resolved", "", "ok"),
				option("replaced", "replaced", "", "mut"),
				option("sent", "sent", "", "ok"),
				option("snoozed", "snoozed", "", "pur"),
				option("escalated", "escalated", "", "dng"),
			},
		},
		{
			ID: "calendar_escalation_state",
			Options: []domain.Option{
				option("none", "none", "", "mut"),
				option("pending", "escalation", "", "dng"),
				option("queued", "queued", "", "warn"),
				option("escalated", "escalated", "", "dng"),
				option("level_1_open", "level 1 open", "", "dng"),
				option("level_2_open", "level 2 open", "", "dng"),
				option("level_3_open", "level 3 open", "", "dng"),
				option("level_4_open", "level 4 open", "", "dng"),
				option("level_1_acknowledged", "level 1 acknowledged", "", "info"),
				option("level_2_acknowledged", "level 2 acknowledged", "", "info"),
				option("level_3_acknowledged", "level 3 acknowledged", "", "info"),
				option("level_4_acknowledged", "level 4 acknowledged", "", "info"),
				option("acknowledged", "acknowledged", "", "info"),
				option("resolved", "resolved", "", "ok"),
			},
		},
	}
}

func procurementOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "proc_sex",
			Options: []domain.Option{
				option("female", "Female", "", ""),
				option("male", "Male", "", ""),
			},
		},
		{
			ID: "proc_species",
			Options: []domain.Option{
				option("goat", "Goat", "", ""),
				option("sheep", "Sheep", "", ""),
			},
		},
		{
			ID: "proc_selection_state",
			Options: []domain.Option{
				option("source_only", "Source only", "", "mut"),
				option("candidate", "Candidate", "", "info"),
				option("purchased", "Purchased", "", "teal"),
				option("accepted", "Accepted", "", "ok"),
				option("rejected", "Rejected", "", "dng"),
				option("deferred", "Deferred", "", "mut"),
				option("blocked", "Blocked", "", "dng"),
				option("loaded", "Loaded", "", "info"),
				option("arrival_accepted", "Arrival accepted", "", "ok"),
				option("arrival_rejected", "Arrival rejected", "", "dng"),
				option("accepted_herd_intake", "Accepted intake", "", "ok"),
				option("dead", "Dead", "", "dng"),
				option("sold", "Sold", "", "mut"),
				option("lost", "Lost", "", "dng"),
			},
		},
		{
			ID: "proc_goat_state",
			Options: []domain.Option{
				option("source_holding", "Source holding", "", "mut"),
				option("source_warmup", "Source warmup", "", "info"),
				option("source_candidate", "Source candidate", "", "info"),
				option("source_health_pending", "Health pending", "", "warn"),
				option("source_health_passed", "Health passed", "", "ok"),
				option("source_health_failed", "Health failed", "", "dng"),
				option("source_rejected", "Source rejected", "", "dng"),
				option("pre_dispatch_pending", "Pre-dispatch pending", "", "warn"),
				option("pre_dispatch_accepted", "Accepted for truck", "", "ok"),
				option("pre_dispatch_rejected", "Rejected before truck", "", "dng"),
				option("pre_dispatch_deferred", "Pre-dispatch deferred", "", "mut"),
				option("pre_dispatch_blocked", "Pre-dispatch blocked", "", "dng"),
				option("dispatch_ready", "Dispatch ready", "", "teal"),
				option("loading_pending", "Loading pending", "", "warn"),
				option("loaded", "Loaded", "", "info"),
				option("in_transit", "In transit", "", "info"),
				option("arrival_review_pending", "Arrival review", "", "pur"),
				option("arrival_accepted", "Arrival accepted", "", "ok"),
				option("arrival_rejected", "Arrival rejected", "", "dng"),
				option("accepted_herd_intake", "Accepted intake", "", "ok"),
				option("dead", "Dead", "", "dng"),
				option("sold", "Sold", "", "mut"),
				option("lost", "Lost", "", "dng"),
				option("canceled", "Canceled", "", "mut"),
			},
		},
		{
			ID: "proc_purpose",
			Options: []domain.Option{
				option("breeding", "breeding", "", ""),
				option("fattening", "fattening", "", ""),
				option("non_breeding", "non breeding", "", ""),
				option("unspecified", "unspecified", "", ""),
			},
		},
		{
			ID: "proc_health_state",
			Options: []domain.Option{
				option("pending", "Health pending", "", "warn"),
				option("passed", "Health passed", "", "ok"),
				option("failed", "Health failed", "", "dng"),
				option("deferred", "Health deferred", "", "mut"),
			},
		},
		{
			ID: "proc_ownership_state",
			Options: []domain.Option{
				option("pending", "Ownership pending", "", "warn"),
				option("shared_pending", "Shared / pending", "", "warn"),
				option("mesha_owned", "Mesha owned", "", "ok"),
				option("blocked", "Ownership blocked", "", "dng"),
				option("not_owned", "Not owned", "", "mut"),
				option("settled", "Settled", "", "ok"),
			},
		},
		{
			ID: "proc_decision_type",
			Options: []domain.Option{
				option("accepted", "accept for truck", "", "ok"),
				option("rejected", "reject before truck", "", "dng"),
				option("deferred", "deferred", "", "mut"),
				option("blocked", "blocked", "", "dng"),
			},
		},
		{
			ID: "proc_arrival_status",
			Options: []domain.Option{
				option("pending", "Pending", "", "warn"),
				option("mismatch", "Mismatch", "", "dng"),
				option("accepted", "Accepted", "", "ok"),
				option("rejected", "Rejected", "", "dng"),
				option("deferred", "Deferred", "", "mut"),
				option("blocked", "Blocked", "", "dng"),
			},
		},
		{
			ID: "proc_arrival_state",
			Options: []domain.Option{
				option("matched", "Matched", "", "ok"),
				option("missing", "Missing", "", "dng"),
				option("extra_unresolved", "Extra / unresolved", "", "dng"),
				option("health_flag", "Health flag", "", "warn"),
				option("weight_flag", "Weight flag", "", "warn"),
				option("accepted", "Accepted", "", "ok"),
				option("rejected", "Rejected", "", "dng"),
				option("deferred", "Deferred", "", "mut"),
				option("blocked", "Blocked", "", "dng"),
			},
		},
		{
			ID: "proc_source_entry_state",
			Options: []domain.Option{
				option("pending", "Source entry: pending", "", "warn"),
				option("accepted", "Source entry: accepted", "", "ok"),
				option("blocked", "Source entry: blocked", "", "dng"),
			},
		},
		{
			ID: "proc_transit_status",
			Options: []domain.Option{
				option("planned", "Planned", "", "info"),
				option("in_transit", "In transit", "", "info"),
				option("arrived", "Arrived", "", "ok"),
				option("canceled", "Canceled", "", "mut"),
			},
		},
		{
			ID: "proc_discrepancy_state",
			Options: []domain.Option{
				option("none", "None", "", "ok"),
				option("partial_load", "Partial load", "", "warn"),
				option("accepted_not_loaded", "Accepted not loaded", "", "warn"),
				option("missing", "Missing", "", "dng"),
				option("extra", "Extra", "", "dng"),
				option("mismatch", "Mismatch", "", "dng"),
				option("blocked", "Blocked", "", "dng"),
			},
		},
		{
			ID: "proc_warmup_state",
			Options: []domain.Option{
				option("not_started", "Not started", "", "mut"),
				option("in_progress", "In progress", "", "info"),
				option("completed", "Completed", "", "ok"),
				option("outside_normal_window", "Outside normal window", "", "warn"),
			},
		},
		{
			ID: "proc_handoff_status",
			Options: []domain.Option{
				option("pending", "Pending", "", "warn"),
				option("emitted", "Emitted", "", "ok"),
				option("canceled", "Canceled", "", "mut"),
			},
		},
		{
			ID: "proc_intake_signal",
			Options: []domain.Option{
				option("clear", "clear", "", "ok"),
				option("defer", "defer", "", "mut"),
				option("quarantine", "quarantine", "", "warn"),
				option("review", "review", "", "warn"),
			},
		},
		{
			ID: "proc_hf_review_status",
			Options: []domain.Option{
				option("trusted", "trusted", "", "ok"),
				option("rejected", "rejected", "", "dng"),
				option("conflicting", "conflicting", "", "warn"),
				option("duplicate", "duplicate", "", "mut"),
			},
		},
		{
			ID: "source_load_status",
			Options: []domain.Option{
				option("source_warmup", "Source warmup", "", "info"),
				option("health_pending", "Health pending", "", "warn"),
				option("pre_dispatch_pending", "Pre-dispatch pending", "", "warn"),
				option("dispatch_ready", "Dispatch ready", "", "teal"),
				option("in_transit", "In transit", "", "info"),
				option("arrival_review", "Arrival review", "", "pur"),
				option("accepted_intake", "Accepted intake", "", "ok"),
				option("rejected", "Rejected", "", "dng"),
				option("deferred", "Deferred", "", "mut"),
				option("blocked", "Blocked", "", "dng"),
				option("canceled", "Canceled", "", "mut"),
			},
		},
		{
			ID: "warmup_evidence_states",
			Options: []domain.Option{
				option("trusted", "complete · evidence", "", "ok"),
				option("imported", "evidence imported", "", "info"),
				option("flagged", "evidence flagged", "", "dng"),
				option("due", "HF evidence due", "", "warn"),
			},
		},
		{
			ID: "health_selection_states",
			Options: []domain.Option{
				option("warming", "warming", "", "info"),
				option("health_pending", "health pending", "", "warn"),
				option("selection_ok", "selection ok", "", "ok"),
				option("blocked_rejected", "blocked / rejected", "", "dng"),
				option("review", "review", "", "warn"),
				option("cleared_forward", "cleared forward", "", "ok"),
			},
		},
		{
			ID: "warmup_expectations",
			Options: []domain.Option{
				option("fattening", "28-35d", "procurement holding is 4-5 weeks for every purpose; outside claims do not suppress PC vaccination", ""),
				option("non_breeding", "28-35d", "procurement holding is 4-5 weeks for every purpose; outside claims do not suppress PC vaccination", ""),
				option("breeding", "28-35d", "procurement holding is 4-5 weeks for every purpose; outside claims do not suppress PC vaccination", ""),
				option("unspecified", "28-35d", "set purpose for operations, but trust still requires the 4-5 week governed holding window", ""),
			},
		},
		{
			ID: "proc_arrival_counts",
			Options: []domain.Option{
				option("expected_count", "Expected", "", ""),
				option("loaded_count", "Loaded", "", ""),
				option("arrived_count", "Arrived", "", ""),
				option("matched_count", "Matched", "", ""),
				option("missing_count", "Missing", "", ""),
				option("extra_count", "Extra", "", ""),
				option("rejected_count", "Rejected", "", ""),
			},
		},
	}
}

func processIntegrityOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "work_state_filter_chips",
			Options: []domain.Option{
				option("overdue", "Overdue", "", "dng"),
				option("missed", "Missed", "", "warn"),
				option("blocked", "Blocked", "", "dng"),
				option("rejected", "Rejected", "", "dng"),
				option("proof_pending", "Proof pending", "", "warn"),
				option("verification_pending", "Verification pending", "", "pur"),
				option("due", "Due", "", "warn"),
				option("in_progress", "In progress", "", "info"),
				option("scheduled", "Scheduled", "", "info"),
				option("deferred", "Deferred", "", "mut"),
				option("completed", "Completed", "", "ok"),
			},
		},
		{
			ID: "severity_chips",
			Options: []domain.Option{
				option("broken", "Broken", "", "dng"),
				option("at_risk", "At risk", "", "warn"),
				option("watch", "Watch", "", "info"),
				option("ok", "OK", "", "ok"),
			},
		},
		{
			ID: "sop_state_chips",
			Options: []domain.Option{
				option("not_started", "SOP: not started", "", "mut"),
				option("in_progress", "SOP: in progress", "", "info"),
				option("submitted", "SOP: submitted", "", "warn"),
				option("accepted", "SOP: accepted", "", "ok"),
				option("rework", "SOP: rework", "", "dng"),
			},
		},
		{
			ID: "proof_state_chips",
			Options: []domain.Option{
				option("not_required", "Proof: n/a", "", "mut"),
				option("missing", "Proof: missing", "", "warn"),
				option("uploaded", "Proof: uploaded", "", "info"),
				option("accepted", "Proof: accepted", "", "ok"),
				option("rejected", "Proof: rejected", "", "dng"),
			},
		},
		{
			ID: "verification_state_chips",
			Options: []domain.Option{
				option("not_ready", "Verify: not ready", "", "mut"),
				option("pending", "Verify: pending", "", "pur"),
				option("verified", "Verify: verified", "", "ok"),
				option("accepted", "Verify: accepted", "", "ok"),
				option("rejected", "Verify: rejected", "", "dng"),
			},
		},
	}
}

func option(key, label, title, tone string) domain.Option {
	return domain.Option{Key: key, Label: label, Title: title, Enabled: true, Tone: tone}
}

// weighingWeightsOptionGroups is the filter vocabulary for /weighing/weights.
//
// The capture-mode keys are the STORED enum values so the client round-trips them
// to the API unchanged, while the labels are farm language — the renderer must
// never show "per_shed_partition" or invent "Lump sum" of its own.
//
// weighing_parks is declared EMPTY on purpose: parks are live tenant rows, not
// contract constants, and compilePages injects them from ReferenceFamilies. A
// hardcoded park list here would be the banned pattern.
func weighingWeightsOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "weighing_mode", Options: []domain.Option{
				option("all", "All", "Both ways of weighing", ""),
				option("individual_animal", "Per animal", "Each kid scanned and weighed on its own", "info"),
				option("per_shed_partition", "Lump sum", "One total for the pen, with a head count", ""),
			},
		},
		// `weighing_period` (28 / 84 days) is deliberately GONE, not left as an unused vocabulary: the
		// window is now picked from a calendar, and a stale option group reads to the next author as
		// a control that still exists somewhere.
		{ID: "weighing_parks", Options: []domain.Option{}},
		{
			// The Time-wise tab's column width (maintainer request 2026-09-21). MONTHLY is a
			// rolling 30-day block counted back from the selected period's last day -- the last 30
			// days, then the 30 before that -- never a calendar month, because calendar months are
			// 28 to 31 days long and a steady gain would read differently in February than in March
			// purely because the bucket changed length. The labels say so in the farm's words.
			ID: "gain_bucket",
			Options: []domain.Option{
				option("week", "Weekly", "", ""),
				option("month", "Monthly (30 days)", "", ""),
			},
		},
		{
			// The pens table's average-weight comparison vocabulary, the feed config's grams
			// filter re-used for kilograms. Backend-owned like every option group: the keys are
			// the operators the page honours, the labels the reader's words for them.
			ID: "weight_kg_compare",
			Options: []domain.Option{
				option("gt", "More than", "", ""),
				option("gte", "At least", "", ""),
				option("eq", "Exactly", "", ""),
				option("lte", "At most", "", ""),
				option("lt", "Less than", "", ""),
				option("neq", "Not", "", ""),
			},
		},
	}
}

// shedStatusOptionGroup is the merged CEO status headline vocabulary for the shed-wise table + shed
// detail. The frontend renders these labels (never the raw enum, never the word "state") and uses the
// group for the Status filter chips. Priority order matches the backend headline: overdue >
// needs_review > split > due > scheduled > on_track.
func shedStatusOptionGroup() domain.OptionGroup {
	return domain.OptionGroup{
		ID: "shed_status_chips",
		Options: []domain.Option{
			option("overdue", "Overdue", "At least one overdue animal", "dng"),
			option("needs_review", "Capacity action", "Due work cannot fit the safe window at authored capacity; add operators or finish over cap", "dng"),
			option("split", "Split", "Work safely split across multiple days, none overdue", "warn"),
			option("due", "Due", "At least one due animal, none overdue", "warn"),
			option("scheduled", "Drive scheduled", "Only future scheduled work", "info"),
			option("on_track", "No work due", "No open vaccination work", "ok"),
		},
	}
}

// capacityOptionGroup renders the internal capacity machine states (within_cap/over_cap/capacity_breach)
// as CEO labels (Within cap / Split / Capacity action). Used for the capacity filter chips, the per-day
// planned-session capacity cell, and the shed detail capacity headline. The raw tokens never reach the UI.
func capacityOptionGroup() domain.OptionGroup {
	return domain.OptionGroup{
		ID: "capacity_chips",
		Options: []domain.Option{
			option("within_cap", "Within cap", "Fits within the daily vaccination limit in a single day", "ok"),
			option("over_cap", "Split", "Safely split across multiple days within the safe window", "warn"),
			option("capacity_breach", "Capacity action", "Cannot fit within the safe window at authored capacity; add operators or finish over cap", "dng"),
		},
	}
}

func humanLabel(key string) string {
	switch key {
	// Two words that are alternatives, not a compound: the default humanisation renders
	// "Profit loss", which reads as one thing rather than either-or.
	case "profit_loss":
		return "Profit / loss"
	// The diagnosis-routing columns. humanLabel DERIVES a label from the column key, and the
	// derivations here would be the machine's words on a screen a vet reads -- "Has published
	// register", "Route count", "Type key". The farm's words are what the rest of this screen
	// already uses.
	case "route_count":
		return "Stages"
	case "has_published_register":
		return "Rules written"
	case "type_key":
		return "Key"
	case "type_label":
		return "Type"
	case "stage_label":
		return "Stage"
	case "sub_stage":
		return "Cohort"
	case "live_animals":
		return "Animals now"
	case "goat_id":
		return "Goat ID"
	case "display_id":
		return "Display ID"
	case "tag_1":
		return "Tag 1"
	case "tag_2":
		return "Tag 2"
	case "breed":
		return "Breed"
	case "sex":
		return "Sex"
	case "age":
		return "Age"
	case "kids_adults":
		return "Kids · Adults"
	// Sits beside Kids · Adults and reads the same way, because it answers the same shape of
	// question about the same row. The Gender column beside it names WHICH sexes a pen holds;
	// this one says HOW MANY of each, on every row rather than only on the mixed ones.
	case "female_male":
		return "Female · Male"
	case "next_action":
		return "Next action"
	case "effective_date":
		return "Effective date"
	case "linked_sop":
		return "Linked SOP"
	case "rule_count":
		return "Rule count"
	case "source_obligation":
		return "Source obligation"
	case "weight":
		return "Weight"
	case "when":
		return "Time"
	case "holding_farm_supplier":
		return "Holding farm · supplier"
	case "vaccination_hf":
		return "Vaccination · at HF"
	case "health_selection":
		return "Health / Selection"
	case "shed_stage":
		return "Pen · stage"
	case "drive_due":
		return "Drive · due"
	case "work_state":
		return "Work state"
	case "owner_chain":
		return "Operator assignment"
	case "sop_proof_verify":
		return "SOP · proof · verify"
	case "animal_stage":
		return "Animal stage"
	case "animal_ids":
		return "Animal IDs"
	case "source_entry":
		return "Source entry"
	case "current_stage":
		return "Current stage"
	case "arrival_state":
		return "Arrival state"
	case "entry_date":
		return "Entry date"
	// The operational location is called a PEN on every screen. The COLUMN KEYS stay
	// shed_* -- they are the data contract every read model already emits -- so the label
	// is named here rather than left to the default de-underscoring, which would render
	// "Shed".
	case "shed":
		return "Pen"
	case "sheds":
		return "Pens"
	case "shed_tag":
		return "Pen tag"
	case "shed_name":
		return "Pen name"
	case "shed_code":
		return "Pen code"
	// Feed vertical. Only keys whose default de-underscored form would be wrong or
	// ambiguous are listed; park/breed/session/head_count/feed_item/status
	// already derive correctly.
	case "quantity_kg":
		return "Quantity (kg)"
	case "session_total_kg":
		return "Session total (kg)"
	case "expected_kg":
		return "Expected (kg)"
	case "absolute_kg":
		return "Absolute (kg)"
	case "experiment_quantity":
		// Deliberately unit-less in the HEADER, because the column holds two kinds of number: a
		// per-animal rate in grams (every cell authored from 2026-09-01) and a legacy absolute pen
		// total in kg. Each cell states its own unit; a header naming one of them would mislabel
		// every row of the other.
		return "Quantity"
	case "grams_per_head":
		// Unit AND period: the stored value is grams per head per DAY, before the session split.
		return "Grams / head / day"
	case "multiplier":
		return "Feed factor"
	// The experiment sheds' columns. Both need naming because their default de-underscored forms
	// ("Experiment category", "Head count") are ambiguous in exactly the place it matters.
	case "experiment_category":
		// "Arm" is the word for a trial group. "Category" reads like a taxonomy and invites someone
		// to treat it as a filterable dimension of the ration grid, which it is not.
		return "Experiment arm"
	case "live_head_count":
		// A SEPARATE key from Feed Direction's "head_count" because it is a different FACT, not a
		// different label for one: this column is the pen's population read from the herd register at
		// request time, where Feed Direction's is the projected count frozen onto an issued sheet.
		// They agree on an ordinary day and must not be conflated on any other.
		//
		// It replaced "informational_head_count" on 2026-09-01. That column carried a figure an
		// author had typed beside the quantity, which nothing maintained and which disagreed with the
		// actual population for 15 of 34 pens; the maintainer's instruction was "use live only,
		// forget recorded", and the header says which one a reader is looking at.
		return "Animals in this pen"
	// The feed-item catalog's nutritional attributes. Each carries its UNIT or its range, because
	// the bare de-underscored forms ("Energy kcal per kg", "Dry matter factor") give a reader no way
	// to tell whether a blank cell means zero or unmeasured, or what a legal value looks like.
	case "energy_kcal_per_kg":
		return "Energy (kcal/kg)"
	case "dry_matter_factor":
		return "Dry matter (0–1)"
	case "wastage_factor":
		return "Wastage (0–<1)"
	case "display_order":
		return "Order"
	case "session_no":
		return "Session"
	case "session_label":
		return "Session name"
	case "split_fraction":
		return "Session split"
	case "feeds":
		return "Feeds served"
	case "valid_from":
		return "Effective from"
	case "valid_to":
		return "Effective to"
	// The feed day's dispatch clock. Each label names the ACT, not a clock range, because the
	// times are close together (07:00/14:00/15:45) and a bare "Starts"/"Ends" pair on this row
	// reads as the feeding window for animals that are in fact fed the following morning.
	case "direction_time":
		return "Direction issued"
	case "correction_time":
		return "Corrections reissued"
	case "transport_time":
		return "Transport cutoff"
	default:
		out := []rune(key)
		for i, r := range out {
			if r == '_' {
				out[i] = ' '
			}
		}
		if len(out) > 0 && out[0] >= 'a' && out[0] <= 'z' {
			out[0] = out[0] - ('a' - 'A')
		}
		return string(out)
	}
}

func displayRules() []domain.DisplayRule {
	return []domain.DisplayRule{
		{
			ID:        "summary-vs-detail",
			AppliesTo: []string{"table rows", "cards", "drawers", "matrix cells"},
			Summary:   "Backend sends the object and presentation contract; frontend may render a compact subset in a row/card and the full object in a drawer.",
			FrontendOwns: []string{
				"how many summary fields fit at the current breakpoint",
				"drawer open/closed state and URL search-param selection",
				"responsive wrapping and icon component mapping from backend icon tokens",
			},
			BackendOwns: []string{
				"field labels, enabled/disabled action availability, route visibility, table/filter/sort semantics",
				"which object fields are summary-safe versus detail-only",
				"disabled reasons and empty/error copy",
			},
		},
		{
			ID:           "no-frontend-business-truth",
			AppliesTo:    []string{"navigation", "page titles", "filters", "chips", "sort keys", "actions"},
			Summary:      "Frontend must not invent product words, module availability, server filter semantics, or write-action availability.",
			FrontendOwns: []string{"CSS classes", "hover/focus behavior", "local menu state", "theme preview"},
			BackendOwns:  []string{"labels", "hrefs", "badges", "role lens text", "route labels", "page contracts"},
		},
	}
}

// inspectionEditorCopy is the Procurement SOP page's editor copy (PROCUREMENT SOP, maintainer
// decision 2026-09-14): the animal-purchase inspection authored as pages of questions.
func inspectionEditorCopy() map[string]string {
	return map[string]string{
		"action.edit_inspection": "Change SOP",
		// VENDOR FORM (2026-09-19): the sales.vendor SOP, edited through the same pages editor.
		"action.edit_vendor_form":            "Change form",
		"feed_form.title":                    "Feed purchase form",
		"feed_form.subtitle":                 "What is asked when a feed load is recorded, page by page. Each question names its kind and whether it is compulsory. Publishing applies to loads recorded from then on, on the web and on the phone.",
		"feed_form.drawer.title":             "What the form asks, page by page",
		"feed_form.drawer.subtitle":          "from the published SOP",
		"feed_form.notice.capture_kept":      "Date, farm, feed, quantity and vendor stay compulsory; the locked questions are the ledger's own columns, read by the stock cards, the landed rate and the aflatoxin test. Everything else -- wording, order, pages, extra questions -- is yours to change.",
		"feed_form.notice.catalog_choices":   "Choices come from the ledger's own lists:",
		"vendor_form.title":                  "Vendor form",
		"vendor_form.subtitle":               "What is asked when a vendor or buyer is added or edited, page by page. Each question names its kind and whether it is compulsory. Publishing applies to vendors added or edited from then on, on the web and on the phone.",
		"vendor_form.drawer.title":           "What the form asks, page by page",
		"vendor_form.drawer.subtitle":        "from the published SOP",
		"vendor_form.notice.capture_kept":    "Name, type, state and status stay compulsory; the locked questions are the register's own columns. Everything else -- wording, order, pages, extra questions -- is yours to change.",
		"vendor_form.notice.catalog_choices": "Choices come from the vendor catalog:",
		// A supplier and a feed load are not INSPECTED, so the two entry forms name the reader
		// the register asks rather than borrowing the inspection form's word for it.
		"vendor_form.question.title": "Question the form asks",
		"feed_form.question.title":   "Question the form asks",
		"action.opening_editor":      "Opening…",
		"label.inspection_questions": "questions",
		"label.inspection_pages":     "pages",
		// A page holding one question read "1 questions". Both forms are backend-owned; the
		// editor picks between them by the count, which is rendering, not wording.
		"label.inspection_question":         "question",
		"label.inspection_page":             "page",
		"inspection.title":                  "Animal purchase inspection",
		"inspection.subtitle":               "What the inspector answers for each animal, page by page. Each question names its kind, whether it is compulsory, and the photo or video it needs. Publishing applies to animals recorded from then on; a form already open on a phone submits on the version it rendered.",
		"inspection.drawer.title":           "What the inspector answers, page by page",
		"inspection.drawer.subtitle":        "from the published SOP",
		"inspection.page":                   "Page",
		"inspection.page.title":             "Page heading",
		"inspection.page.hint":              "Page note",
		"inspection.page.add":               "Add page",
		"inspection.page.remove":            "Remove page",
		"inspection.page.first_untitled":    "The first page has no heading; every later page needs one.",
		"inspection.question.kind":          "Question kind",
		"inspection.question.title":         "Question the inspector sees",
		"inspection.question.hint":          "Instruction",
		"inspection.question.required":      "Compulsory",
		"inspection.question.options":       "Choices",
		"inspection.question.add_option":    "Add choice",
		"inspection.question.allow_other":   "The \"other\" choice asks for free text (where / what)",
		"inspection.question.accepts":       "Capture",
		"inspection.question.max_files":     "Up to (files)",
		"inspection.problem.feed_noun":      "A feed load",
		"inspection.problem.vendor_noun":    "A vendor",
		"inspection.question.unit":          "Unit",
		"inspection.question.min":           "Min",
		"inspection.question.max":           "Max",
		"inspection.question.only_if":       "Ask only when",
		"inspection.question.only_if_value": "is",
		"inspection.question.always":        "Always asked",
		"inspection.question.add":           "Add question",
		"inspection.question.remove":        "Remove question",
		"inspection.question.move_up":       "Move up",
		"inspection.question.move_down":     "Move down",
		"inspection.question.move_page":     "Move to page",
		"inspection.question.key":           "Question key",
		"inspection.notice.locked":          "The register reads this answer; its kind and choices are fixed, everything else is yours to edit.",
		"inspection.notice.capture_kept":    "Vendor, farm and load number stay compulsory on the load form; everything else on both forms is yours to change.",
		"inspection.action.publish":         "Publish SOP",
		"inspection.action.save_draft":      "Save as draft",
		"inspection.empty":                  "No questions yet.",
		"inspection.result.saved_draft":     "Inspection saved as a draft version.",
		"inspection.result.published":       "Published. Animals recorded from now on use this inspection.",
		"inspection.kind.choice":            "Pick one",
		"inspection.kind.multi":             "Pick many",
		"inspection.kind.text":              "Free text",
		"inspection.kind.number":            "Number",
		"inspection.kind.media":             "Photo / video",
		"inspection.kind.vendor":            "Vendor (from the register)",
		"option.yes":                        "Yes",
		"option.no":                         "No",
		"inspection.loadform.title":         "Load form",
		"inspection.loadform.subtitle":      "asked once when a purchase load is opened; vendor, farm and load number stay compulsory; photos and videos are per animal",
		"inspection.accepts.photo":          "Photo only",
		"inspection.accepts.video":          "Video only",
		"inspection.accepts.both":           "Photo or video",
		"inspection.summary.required":       "compulsory",
		"inspection.summary.optional":       "optional",
		"inspection.summary.only_if":        "only when {question} is {value}",
		"inspection.summary.files":          "up to {n}",
	}
}

// weighingSOPEditorCopy is the Weighing SOP page's editor copy (WEIGHING SOP, maintainer
// decision 2026-09-15): the rules a weighing task is planned on and runs under. The question
// card reuses the inspection editor's question keys (kind / choices / range / ask-only-when),
// which is why that map is merged first.
// feedSOPEditorCopy is the copy of the feed cards editor and its drawer summary (FEED SOP,
// maintainer decision 2026-09-16). It reuses the weighing editor's question-card and capture-kind
// copy so the two editors read the same way.
func feedSOPEditorCopy() map[string]string {
	m := map[string]string{}
	for k, v := range weighingSOPEditorCopy() {
		m[k] = v
	}
	for k, v := range map[string]string{
		"action.edit_feed":            "Change SOP",
		"fsop.title":                  "Feed SOP — what the crew captures",
		"fsop.subtitle":               "Each card lists the captures the crew must record and the questions they answer. Add a photo beside a video, replace a video with a photo, drop a capture or add a step: the phone renders whatever is published here.",
		"fsop.notice.pinned":          "A sheet issued today keeps the card it was issued with; publishing changes the next sheet, and every phone on that sheet renders the new card.",
		"fsop.drawer.title":           "What the crew captures",
		"fsop.drawer.subtitle":        "Published card, per stage",
		"fsop.stage.distribution":     "Feed distribution",
		"fsop.stage.distribution.sub": "One pen per session, shared by every operator of the park. Any operator may record any capture; the pen is submitted once every compulsory capture is in.",
		"fsop.stage.wastage":          "Leftover feed (experiment pens)",
		"fsop.stage.wastage.sub":      "One card per experiment pen per day. The verifier reads the leftover weight off the clip.",
		"fsop.stage.packing":          "Feed packing",
		"fsop.stage.packing.sub":      "One card per bag: one pen and one session. Morning and evening are two bags.",
		"fsop.stage.transport":        "Feed transport",
		"fsop.stage.transport.sub":    "One card per physical location per day: the packed feed loaded and staged outside it, one trip.",
		// SOP studio phase 2 (2026-09-18): the Flow view of the feed cards.
		"fsop.flow.start":              "Feed sheet issued",
		"fsop.flow.start_hint":         "The day's cards are raised from the feed sheet",
		"fsop.flow.card":               "Card",
		"fsop.flow.verify":             "Verifier review",
		"fsop.flow.verify_hint":        "Fixed: one item per card; a reject sends the card back",
		"fsop.flow.capture":            "Capture",
		"fsop.flow.question":           "Question",
		"fsop.flow.add_capture":        "capture",
		"fsop.flow.add_question":       "question",
		"fsop.flow.compulsory":         "compulsory",
		"fsop.flow.optional":           "optional",
		"fsop.flow.hint":               "Click a capture or question to edit it. Click + on a line to insert one there, on that card.",
		"fsop.instruction":             "Instruction shown on the card",
		"fsop.proofs":                  "Captures",
		"fsop.proofs.subtitle":         "In the order the phone shows them. Compulsory captures gate the submit; optional ones may be skipped.",
		"fsop.proof.title":             "Capture name",
		"fsop.proof.hint":              "Hint for the operator",
		"fsop.proof.add":               "Add a capture",
		"fsop.proof.remove":            "Remove this capture",
		"fsop.questions":               "Questions",
		"fsop.questions.subtitle":      "Answered once per card, beside the captures. The answers reach the verifier.",
		"fsop.questions.empty":         "No questions on this card.",
		"fsop.footer.ready":            "Ready to publish. The next sheet issued runs on this card.",
		"fsop.summary.compulsory":      "compulsory",
		"fsop.summary.optional":        "optional",
		"fsop.summary.questions_one":   "1 question",
		"fsop.summary.questions_many":  "{n} questions",
		"fsop.summary.legacy_key_note": "Capture keys are what the phones stamp on uploads; changing a key makes it a new capture.",
		"fsop.locked":                  "Not authorable here: the shared session (any operator may record any capture, slots are independent), the per-bag packing grain, the one-trip transport grain, verifier review before completion.",
	} {
		m[k] = v
	}
	return m
}

// shiftingSOPEditorCopy is the copy of the shifting cards editor and its drawer summary (SHIFTING
// SOP, maintainer decision 2026-09-16): the raise extras, the completion card and the
// high-priority card. It reuses the weighing editor's question-card and capture-kind copy so the
// editors read the same way.
func shiftingSOPEditorCopy() map[string]string {
	m := map[string]string{}
	for k, v := range weighingSOPEditorCopy() {
		m[k] = v
	}
	for k, v := range map[string]string{
		"action.edit_shifting":           "Change SOP",
		"ssop.title":                     "Shifting SOP — what the operator captures",
		"ssop.subtitle":                  "Three cards: what the raise form asks beyond the movement itself, what a completion must capture and answer, and what a high-priority movement adds. Add a photo beside a video, replace a video with a photo, drop a capture or add a question: the phone renders whatever is published here.",
		"ssop.notice.pinned":             "A movement raised today keeps the cards it was raised with; publishing changes the next raise, and every phone renders the new cards on refresh.",
		"ssop.drawer.title":              "What the operator captures",
		"ssop.drawer.subtitle":           "Published cards",
		"ssop.section.raise":             "At raise",
		"ssop.section.raise.sub":         "Extras on the raise form, before approval. The park head sees the answers and captures beside the movement; the verifier sees them again with the completion. May be questions only, or empty.",
		"ssop.section.completion":        "Completion",
		"ssop.section.completion.sub":    "Every completion. At least one compulsory capture: the move must be proven by something the verifier can see.",
		"ssop.section.high_priority":     "High priority",
		"ssop.section.high_priority.sub": "Added to the completion of a high-priority movement. The ration and its fingerprint still come from Feed Config; this card only decides which captures and questions prove the feeding. At least one compulsory capture.",
		"ssop.instruction":               "Instruction shown on the card",
		"ssop.proofs":                    "Captures",
		"ssop.proofs.subtitle":           "In the order the phone shows them. Compulsory captures gate the submit; optional ones may be skipped.",
		"ssop.proof.title":               "Capture name",
		"ssop.proof.hint":                "Hint for the operator",
		"ssop.proof.add":                 "Add a capture",
		"ssop.proof.remove":              "Remove this capture",
		"ssop.questions":                 "Questions",
		"ssop.questions.subtitle":        "Answered once per card, beside the captures. The answers reach the park head and the verifier.",
		"ssop.questions.empty":           "No questions on this card.",
		"ssop.footer.ready":              "Ready to publish. The next movement raised runs on these cards.",
		"ssop.summary.optional":          "optional",
		"ssop.summary.questions_one":     "1 question",
		"ssop.summary.questions_many":    "{n} questions",
		"ssop.summary.empty":             "Nothing asked.",
		"ssop.summary.legacy_key_note":   "Capture keys are what the phones stamp on uploads; changing a key makes it a new capture.",
		"ssop.locked":                    "Not authorable here: park head approval before any completion, the Feed Config fingerprint on a high-priority movement, the atomic herd move on completion, verifier review afterwards.",
	} {
		m[k] = v
	}
	return m
}

// shiftingSOPOptionGroups is the vocabulary the shifting cards editor picks from: the weighing
// card's question kinds and capture kinds.
func shiftingSOPOptionGroups() []domain.OptionGroup {
	return weighingSOPOptionGroups()
}

func weighingSOPEditorCopy() map[string]string {
	m := map[string]string{}
	for k, v := range inspectionEditorCopy() {
		m[k] = v
	}
	for k, v := range map[string]string{
		"action.edit_weighing":                  "Change SOP",
		"wsop.title":                            "Weighing session rules",
		"wsop.subtitle":                         "What the planner may choose, whether feed and water are removed the evening before, what the removal card says and asks, and how many videos a whole pen carries. Publishing applies to tasks planned from then on; a task already on the calendar keeps the rules it was planned with.",
		"wsop.notice.pinned":                    "Each task is stamped with the version it was planned on.",
		"wsop.drawer.title":                     "The rules a weighing task runs under",
		"wsop.drawer.subtitle":                  "from the published SOP",
		"wsop.section.planning":                 "Planning",
		"wsop.section.planning.subtitle":        "what the CEO's plan wizard offers",
		"wsop.planning.modes":                   "Ways of weighing the planner may choose",
		"wsop.planning.mode.individual_animal":  "Animal by animal — scan the tag, enter the weight, one video per animal",
		"wsop.planning.mode.per_shed_partition": "Whole pen — one total weight and the pen's videos",
		"wsop.planning.default_cap":             "Default animals per day",
		"wsop.section.removal":                  "Feed & water removal the evening before",
		"wsop.section.removal.subtitle":         "the fasting precondition and its card",
		"wsop.removal.mode":                     "When it applies",
		"wsop.removal.mode.required":            "Every weighing — the plan needs a removal operator and the date must leave an evening free",
		"wsop.removal.mode.optional":            "The planner decides per task (on unless switched off)",
		"wsop.removal.mode.off":                 "Never — no removal step, weighing can be planned for today",
		"wsop.removal.instruction":              "Instruction on the removal card",
		"wsop.removal.cutoff":                   "Removal evening starts at (IST)",
		"wsop.removal.cutoff.hint":              "Before this time tomorrow is plannable; at or after it the earliest weigh day is the day after. Leave blank to use the farm-wide evening.",
		"wsop.removal.cutoff.farm":              "the farm-wide evening",
		"wsop.removal.proofs":                   "Captures each pen owes",
		"wsop.removal.proofs.subtitle":          "a live-camera video, a photo or either per capture; at least one compulsory",
		"wsop.removal.proof.title":              "What the operator captures",
		"wsop.removal.proof.hint":               "Instruction",
		"wsop.removal.proof.add":                "Add capture",
		"wsop.removal.proof.remove":             "Remove capture",
		"wsop.proof.kind.video":                 "Video",
		"wsop.proof.kind.photo":                 "Photo",
		"wsop.proof.kind.either":                "Photo or video",
		"wsop.removal.questions":                "Questions the removal operator answers per pen",
		"wsop.removal.questions.subtitle":       "answered alongside the captures; stored with the pen's evidence",
		"wsop.removal.questions.empty":          "No questions yet — the card asks only for the captures above.",
		"wsop.removal.off_note":                 "With the removal switched off, no task carries the evening-before precondition and the plan wizard offers no removal step.",
		"wsop.question.title":                   "Question the operator sees",
		"wsop.question.remove_choice":           "Remove choice",
		"wsop.section.capture":                  "Weighing capture",
		"wsop.section.capture.subtitle":         "the evidence each weigh carries",
		"wsop.capture.individual.video":         "At least one compulsory capture per animal",
		"wsop.capture.individual.locked":        "At least one capture per animal is compulsory: it is what the verifier reviews and cannot be switched off.",
		"wsop.capture.individual.locked_short":  "fixed",
		"wsop.capture.lump_sum.video_min":       "Whole pen — videos at least",
		"wsop.capture.lump_sum.video_max":       "Whole pen — videos at most",
		"wsop.capture.locked_rules":             "Not authored here, by decision: the RFID scan and the weight entry, scanning is free-flow (a tag is stored as scanned and never checked against a pen or roster), an animal cannot be scanned twice in the same pen before submit, the verifier reviews every capture and may correct a weight on Approve, and a pen closes only once nothing is pending review.",
		"wsop.summary.lump_sum_videos":          "whole pen: {min}–{max} videos",
		// THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16): two SEPARATE
		// sections, each with its own captures and questions, shown as two titled blocks.
		"wsop.capture.individual.title":              "Per animal",
		"wsop.capture.individual.subtitle":           "what the operator captures and answers for EACH animal, beside the RFID scan and the weight",
		"wsop.capture.individual.proofs":             "Captures per animal",
		"wsop.capture.individual.proofs.subtitle":    "a live-camera video, a photo or either per capture; at least one compulsory; the first capture is the one the verifier's verdict is keyed on",
		"wsop.capture.individual.add_capture":        "Add capture",
		"wsop.capture.individual.at_least_one":       "At least one capture per animal must be compulsory",
		"wsop.capture.individual.questions":          "Questions answered per animal",
		"wsop.capture.individual.questions.subtitle": "answered on each animal's row; the answers reach the verifier under \"Per-animal answers\"",
		"wsop.capture.individual.questions.empty":    "No per-animal questions — the animal row asks only for the captures above.",
		"wsop.capture.lump_sum.title":                "Whole pen",
		"wsop.capture.lump_sum.subtitle":             "what the operator captures and answers ONCE for the whole pen, beside the total weight",
		"wsop.capture.lump_sum.proofs":               "Captures per pen",
		"wsop.capture.lump_sum.proofs.subtitle":      "each capture carries a count — how many the operator must record at least and may record at most; at least one capture with a minimum of 1; at most 10 captures per pen in total",
		"wsop.capture.lump_sum.slot_min":             "At least",
		"wsop.capture.lump_sum.slot_max":             "At most",
		"wsop.capture.lump_sum.add_capture":          "Add capture",
		"wsop.capture.lump_sum.total_ceiling":        "At most 10 captures per pen in total",
		"wsop.capture.lump_sum.questions":            "Questions answered per pen",
		"wsop.capture.lump_sum.questions.subtitle":   "answered once on the pen's submit; the answers reach the verifier under \"Whole-pen answers\"",
		"wsop.capture.lump_sum.questions.empty":      "No whole-pen questions — the pen submit asks only for the captures above.",
		"wsop.capture.proof.title":                   "What the operator captures",
		"wsop.capture.proof.hint":                    "Instruction",
		"wsop.capture.proof.remove":                  "Remove capture",
		"wsop.summary.individual_captures":           "per animal: {n} captures ({required} compulsory)",
		"wsop.summary.lump_sum_captures":             "whole pen: {n} captures, up to {max} in total",
		"wsop.summary.questions_one":                 "1 question",
		"wsop.summary.questions_many":                "{n} questions",
		// The seeded slot copy (weighing/domain/sopseed/weighing_capture_slots.json), served so
		// the web model renders a document that predates slots exactly as the backend reads it.
		// Pinned equal to the embedded file by TestWeighingSOPCaptureDefaultsMatchTheEmbeddedSeed.
		"wsop.capture.defaults": weighingCaptureSlotDefaultsJSON,
		"wsop.footer.ready":     "Ready to save or publish.",
		// SOP studio phase 2 (2026-09-18): the Flow view of the weighing session.
		"wsop.flow.start":            "Weighing task planned",
		"wsop.flow.start_hint":       "The CEO's plan wizard offers these ways of weighing",
		"wsop.flow.removal":          "Feed & water removal card",
		"wsop.flow.removal_hint":     "The evening before, once per pen",
		"wsop.flow.removal_off":      "Feed & water removal is off",
		"wsop.flow.decision":         "Weighed as",
		"wsop.flow.individual":       "Per animal",
		"wsop.flow.lump_sum":         "Whole pen",
		"wsop.flow.mode_not_offered": "not offered to the planner",
		"wsop.flow.scan":             "Scan the RFID",
		"wsop.flow.scan_hint":        "Fixed: free-flow, stored as scanned",
		"wsop.flow.weight":           "Enter the weight (kg)",
		"wsop.flow.weight_hint":      "Fixed: one number per animal",
		"wsop.flow.total":            "Enter the pen's total weight (kg)",
		"wsop.flow.total_hint":       "Fixed: the head count is snapshotted at submit",
		"wsop.flow.submit":           "Submit the pen",
		"wsop.flow.submit_hint":      "Fixed: no duplicate scan in a pen before submit",
		"wsop.flow.verify":           "Verifier review",
		"wsop.flow.verify_hint":      "Fixed: the approve carries the weight; the close gate is unconditional",
		"wsop.flow.capture":          "Capture",
		"wsop.flow.question":         "Question",
		"wsop.flow.add_capture":      "capture",
		"wsop.flow.add_question":     "question",
		"wsop.flow.compulsory":       "compulsory",
		"wsop.flow.optional":         "optional",
		"wsop.flow.count":            "to",
		"wsop.flow.hint":             "Click a capture or question to edit it. Click + on a line to insert one there; a step on the Whole pen side applies to whole-pen weighs only.",
	} {
		m[k] = v
	}
	return m
}

// weighingCaptureSlotDefaultsJSON is the seeded per-animal / whole-pen capture slot document,
// byte-identical to backend/internal/weighing/domain/sopseed/weighing_capture_slots.json
// (adminui does not import the weighing domain; the test pins the two equal).
const weighingCaptureSlotDefaultsJSON = `{
  "individual": {
    "proofs": [
      {"key": "animal_video", "title": "Weighing video", "hint": "Live-camera video of this animal on the scale; it opens on the scale reading 0 kg.", "kind": "video", "required": true}
    ],
    "questions": []
  },
  "lump_sum": {
    "proofs": [
      {"key": "pen_video", "title": "Weighing video", "hint": "Live-camera video of the pen on the scale; it opens on the scale reading 0 kg.", "kind": "video", "min": 1, "max": 5}
    ],
    "questions": []
  }
}`

// weighingSOPOptionGroups are the weighing rules editor's closed vocabularies.
func weighingSOPOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "wsop_question_kinds",
			Options: []domain.Option{
				option("choice", "Pick one", "The operator picks one of the choices.", ""),
				option("multi", "Pick many", "The operator ticks every choice that applies.", ""),
				option("text", "Free text", "A short typed answer.", ""),
				option("number", "Number", "A number, optionally within a range and with a unit.", ""),
			},
		},
		{
			ID: "wsop_proof_kinds",
			Options: []domain.Option{
				option("video", "Video", "A live-camera video, recorded in the app.", ""),
				option("photo", "Photo", "A photo taken with the app camera.", ""),
				option("either", "Photo or video", "The operator chooses.", ""),
			},
		},
		{
			ID: "wsop_removal_modes",
			Options: []domain.Option{
				option("required", "Every weighing", "The plan needs a removal operator and the date must leave an evening free.", ""),
				option("optional", "Planner decides per task", "On unless the planner switches it off for a task.", ""),
				option("off", "Never", "No removal step; weighing can be planned for today.", ""),
			},
		},
	}
}

// pcCareSOPOptionGroups are the PC Care editor's OWN closed vocabularies: the removal mode
// (whose wording is about care work, not weighing) and the categories the removal may apply to.
// The question kinds and capture kinds come from weighingSOPOptionGroups -- one vocabulary.
func pcCareSOPOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "pcsop_removal_modes",
			Options: []domain.Option{
				option("required", "Always, on the work below", "Every task of a ticked category carries it; the planner is not asked.", ""),
				option("optional", "Planner decides per task", "The planner is offered the removal on a ticked category and chooses.", ""),
				option("off", "Never", "No removal step at all; a task can be planned for today.", ""),
			},
		},
		{
			ID: "pcsop_categories",
			Options: []domain.Option{
				option("deworming", "Deworming", "", ""),
				option("anti_protozoan", "Anti Protozoan", "", ""),
				option("ticks_removal", "Ticks Removal", "", ""),
				option("hoof_trimming", "Hoof Trimming", "", ""),
				option("hair_trimming", "Hair Trimming", "", ""),
			},
		},
	}
}

// pcCareSOPEditorCopy is the `pcsop.*` namespace: the rules editor and the drawer summary on
// /pc-care/sops. It starts from the weighing card's copy so the shared question and capture-slot
// rows read identically on every SOP page (the feedSOPEditorCopy shape).
func pcCareSOPEditorCopy() map[string]string {
	m := map[string]string{}
	for k, v := range weighingSOPEditorCopy() {
		m[k] = v
	}
	for k, v := range map[string]string{
		"action.edit_pc_care":               "Change SOP",
		"pcsop.title":                       "Preventive Care SOP — what the operator captures",
		"pcsop.subtitle":                    "Each work category lists the captures the operator records for every animal and the questions they answer when submitting. Add a photo beside a video, replace a video with a photo, drop a capture or ask one more question: the phone renders whatever is published here.",
		"pcsop.notice.pinned":               "A task already planned keeps the card it was planned with; publishing changes the next task planned.",
		"pcsop.drawer.title":                "What the operator captures",
		"pcsop.drawer.subtitle":             "Published card, per work category",
		"pcsop.section.removal":             "Feed & water removal",
		"pcsop.section.removal.subtitle":    "Tablets given in feed need feed and water removed the evening before. Say whether that applies, to which work, and what the evening crew records for each pen.",
		"pcsop.removal.mode":                "When does it apply",
		"pcsop.removal.applies_to":          "Work it applies to",
		"pcsop.removal.applies_to.subtitle": "The planner is only offered the removal on these categories; on any other it is refused.",
		"pcsop.removal.applies_to.empty":    "Tick at least one kind of work, or switch the removal off.",
		"pcsop.removal.instruction":         "What the evening crew is told",
		"pcsop.removal.cutoff":              "Evening from",
		"pcsop.removal.cutoff.hint":         "Leave blank to use the farm's own evening, the one weighing shares.",
		"pcsop.removal.cutoff.farm":         "Farm evening",
		"pcsop.removal.proofs":              "What the crew records, per pen",
		"pcsop.removal.proofs.subtitle":     "One capture per row. At least one must be compulsory.",
		"pcsop.removal.questions":           "What the crew answers, per pen",
		"pcsop.removal.questions.subtitle":  "Answered once for each pen, beside the captures.",
		"pcsop.removal.questions.empty":     "No questions on the removal card.",
		"pcsop.removal.off_note":            "The removal is switched off: no task carries it and the planner is never asked.",
		"pcsop.section.categories":          "The work",
		"pcsop.section.categories.subtitle": "One card per kind of work. The way the operator reaches an animal — scanning a tag, or tapping it off the pen roster — is fixed by the work itself and is not authored here.",
		"pcsop.category.instruction":        "What the operator is told",
		"pcsop.category.proofs":             "What the operator records, per animal",
		"pcsop.category.proofs.subtitle":    "One capture per row. At least one must be compulsory — the work has to be proven by something the verifier can see.",
		"pcsop.category.add_capture":        "Add a capture",
		"pcsop.category.at_least_one":       "Keep at least one compulsory capture.",
		"pcsop.category.questions":          "What the operator answers",
		"pcsop.category.questions.subtitle": "Answered once for the whole task, when it is submitted.",
		"pcsop.category.questions.empty":    "No questions on this card.",
		"pcsop.capture.min_seconds":         "Record about (seconds)",
		"pcsop.capture.min_seconds.hint":    "Guidance shown on the recorder. It never blocks a shorter clip.",
		"pcsop.capture.min_seconds.photo":   "A photo has no length.",
		"pcsop.summary.captures_one":        "1 capture",
		"pcsop.summary.captures_many":       "{count} captures",
		"pcsop.summary.questions_one":       "1 question",
		"pcsop.summary.questions_many":      "{count} questions",
		"pcsop.summary.removal.required":    "Always",
		"pcsop.summary.removal.optional":    "Planner decides",
		"pcsop.summary.removal.off":         "Never",
		"pcsop.footer.ready":                "Ready to publish.",
		// The Flow view's node chrome. Generic words, kept under this page's own prefix so the
		// Preventive Care contract never depends on the Feed page's copy being compiled.
		"pcsop.flow.card":        "Card",
		"pcsop.flow.capture":     "Capture",
		"pcsop.flow.question":    "Question",
		"pcsop.flow.compulsory":  "compulsory",
		"pcsop.flow.optional":    "optional",
		"pcsop.flow.hint":        "Click a capture or question to edit it. Click + on a line to insert one there, on that card.",
		"pcsop.summary.optional": "optional",
		// The capture card is the SHARED SlotCard (features/sops/feed-editor.tsx), which names
		// these three keys by their original Feed prefix. Every page that renders it must carry
		// them; the prefix is historical, the copy is this page's.
		"fsop.proof.title":        "Capture name",
		"fsop.proof.hint":         "Hint for the operator",
		"fsop.proof.remove":       "Remove this capture",
		"pcsop.flow.start":        "Task planned",
		"pcsop.flow.start_hint":   "The CEO picks the work, the pens, the day and the operators.",
		"pcsop.flow.removal":      "Feed & water removed",
		"pcsop.flow.removal_hint": "The evening before, pen by pen.",
		"pcsop.flow.removal_off":  "No feed & water removal",
		"pcsop.flow.decision":     "Which work",
		"pcsop.flow.reach":        "Reach the animal",
		"pcsop.flow.reach_scan":   "Scan the tag; the recorder opens.",
		"pcsop.flow.reach_roster": "Tap the animal on the pen roster.",
		"pcsop.flow.submit":       "Submit the task",
		"pcsop.flow.submit_hint":  "Every animal carries every compulsory capture.",
		"pcsop.flow.verify":       "Verifier reviews",
		"pcsop.flow.verify_hint":  "One item per task, carrying every animal's captures.",
	} {
		m[k] = v
	}
	return m
}

// inspectionOptionGroups are the editor's closed vocabularies.
// toxinOptionGroups is the aflatoxin editor's closed step vocabulary (THE TOXIN PROCEDURE IS
// AUTHORED, 2026-09-20). Exactly the three kinds the engine runs: there is no fourth, because a
// kind the engine does not know is a step no tester could complete.
func toxinOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "tsop_step_kinds",
			Options: []domain.Option{
				option("video", "Filmed step", "The tester records one in-app-camera video as the step is done.", ""),
				option("wait", "Waiting", "Nothing to do but wait. The next step unlocks on the server's clock.", ""),
				option("photo_reading", "Read the strip", "The final step: a photo of the strip and the reading. Every procedure has exactly one.", ""),
			},
		},
	}
}

// toxinEditorCopy is the aflatoxin procedure editor's copy, rendered verbatim by the web editor.
// Every sentence here is the product's, not the developer's: the screen explains what a step is
// and what the farm may change, and names the parts that stay the engine's.
func toxinEditorCopy() map[string]string {
	return map[string]string{
		"tsop.action.edit":                "Change procedure",
		"tsop.summary.kind.video":         "Filmed",
		"tsop.summary.kind.wait":          "Waiting",
		"tsop.summary.kind.photo_reading": "Read the strip",
		"tsop.title":                      "Aflatoxin test",
		"tsop.subtitle":                   "The steps of the strip test on a purchased feed load: what the tester does, what is filmed, and how long each wait lasts.",
		"tsop.notice.pinned":              "A test already running keeps the steps it started with; publishing changes the next one.",
		"tsop.steps":                      "Steps",
		"tsop.steps.unit":                 "steps",
		"tsop.steps.waiting":              "waiting",
		"tsop.step.title":                 "Title the tester sees",
		"tsop.step.instruction":           "What to do",
		"tsop.step.kind":                  "Step type",
		"tsop.step.wait_minutes":          "How long to wait (minutes)",
		"tsop.step.add":                   "Add a step",
		"tsop.step.remove":                "Remove this step",
		"tsop.step.untitled":              "Untitled step",
		"tsop.gate.title":                 "Unlocks after",
		"tsop.gate.subtitle":              "Hold this step until time has passed since an earlier one. Enforced on the server's clock, never the phone's.",
		"tsop.gate.after":                 "after",
		"tsop.gate.none":                  "Straight after the step before it",
		"tsop.gate.minutes":               "Minutes to wait",
		"tsop.unit.hour":                  "hour",
		"tsop.unit.hours":                 "hours",
		"tsop.unit.minute":                "minute",
		"tsop.unit.minutes":               "minutes",
		"tsop.flow.start":                 "A feed load reaches the farm",
		"tsop.flow.start_hint":            "Every purchased load owes one test.",
		"tsop.flow.wait":                  "Waiting",
		"tsop.unit.wait":                  "Wait",
		"tsop.flow.after":                 "after",
		"tsop.flow.locked":                "Fixed",
		"tsop.flow.review":                "Reviewed by the CEO",
		"tsop.flow.review_hint":           "The reading goes for review. A void strip or a rejection starts a fresh test on the same load.",
		"tsop.flow.finish_hint":           "The load's test is signed off.",
		"tsop.flow.hint":                  "Click a step to edit it, or + on a line to add one there.",
		"tsop.locked":                     "The review, the retest a void strip starts, and the enforcement of every wait on the server's clock are fixed. This page changes what the test IS, not what happens to its result.",
		"tsop.footer.ready":               "Ready to save or publish.",
		"tsop.problem.no_steps":           "A procedure needs at least one step.",
		"tsop.problem.one_reading":        "The procedure needs exactly one \"Read the strip\" step.",
		"tsop.problem.reading_last":       "\"Read the strip\" must be the last step — it is what ends the test.",
		"tsop.problem.too_short":          "A procedure needs at least one step before the reading.",
		"tsop.problem.title":              "This step needs a title.",
		"tsop.problem.instruction":        "This step needs an instruction — the tester is told what to do, never left to guess.",
		"tsop.problem.wait_minutes":       "A waiting step needs a duration.",
		"tsop.problem.gate_backwards":     "A step can only wait on an EARLIER step.",
		"tsop.problem.gate_working":       "A step can only wait on one that records a completion, never on another wait.",
		"tsop.problem.gate_minutes":       "Say how many minutes to wait.",
	}
}

func inspectionOptionGroups() []domain.OptionGroup {
	return []domain.OptionGroup{
		{
			ID: "inspection_question_kinds",
			Options: []domain.Option{
				option("choice", "Pick one", "The inspector picks one of the choices.", ""),
				option("multi", "Pick many", "The inspector ticks every choice that applies.", ""),
				option("text", "Free text", "A short typed answer.", ""),
				option("number", "Number", "A number, optionally within a range and with a unit.", ""),
				option("media", "Photo / video", "One or more in-app-camera captures.", ""),
			},
		},
		{
			ID: "inspection_capture_kinds",
			Options: []domain.Option{
				option("both", "Photo or video", "", ""),
				option("photo", "Photo only", "", ""),
				option("video", "Video only", "", ""),
			},
		},
	}
}

// addHerdOpsCaptureCardCopy merges the herd operations CAPTURE CARD editor and drawer copy into
// the counts-sops page contract (maintainer decision 4, 2026-09-16): the Add birth / Add death
// form's SOP extras. It reuses the feed/weighing editors' slot and question-card copy so every
// card editor reads the same way. Kept as its own helper so the counts-sops case stays a one-line
// merge.
func addHerdOpsCaptureCardCopy(m map[string]string) {
	for k, v := range feedSOPEditorCopy() {
		if _, taken := m[k]; !taken {
			m[k] = v
		}
	}
	for k, v := range map[string]string{
		"capture.title":              "Capture form extras",
		"capture.subtitle":           "what the operator records and answers beside the form's fixed fields",
		"capture.form.birth":         "Add birth",
		"capture.form.death":         "Add death",
		"capture.notice.pinned":      "A report is judged by the version in force when it was raised.",
		"capture.instruction":        "Instruction shown above the extras",
		"capture.proofs":             "Captures",
		"capture.proofs.subtitle":    "In the order the phone shows them. Compulsory captures must be recorded before the report is sent.",
		"capture.proofs.empty":       "No captures: the form asks for no photo or video.",
		"capture.proof.add":          "Add a capture",
		"capture.questions":          "Questions",
		"capture.questions.subtitle": "Answered once on the form. The answers reach the approver and the verifier.",
		"capture.questions.empty":    "No questions on this form.",
		"capture.question":           "Question",
		"capture.note.older_app":     "A report from a phone that has not refreshed this form yet is still accepted; anything compulsory it did not send is shown to the approver and the verifier as not captured.",
		"capture.footer.ready":       "Ready to publish. Reports raised from now on use this form.",
		"capture.drawer.title":       "Capture form extras",
		"capture.drawer.subtitle":    "asked on the form, beside its fixed fields",
		"capture.drawer.empty":       "The form asks nothing beyond its fixed fields.",
		"capture.summary.optional":   "optional",
		"action.edit_capture_form":   "Edit capture form",
	} {
		m[k] = v
	}
}
