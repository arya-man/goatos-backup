package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

func TestBootstrapPublishesAdminWebContract(t *testing.T) {
	resp := NewService().Bootstrap(context.Background(), BootstrapInput{})
	if resp.SchemaVersion != "admin-web-ui-v1" {
		t.Fatalf("schema version = %q", resp.SchemaVersion)
	}
	if resp.ContractRevision == "" || resp.CachePolicy.ETag == "" || len(resp.FamilyHashes) == 0 {
		t.Fatalf("revision/cache metadata missing: revision=%q cache=%#v hashes=%#v", resp.ContractRevision, resp.CachePolicy, resp.FamilyHashes)
	}
	if len(resp.Navigation.Primary) == 0 || len(resp.Navigation.Groups) == 0 {
		t.Fatalf("navigation contract is empty: %#v", resp.Navigation)
	}
	if len(resp.RouteLabels) == 0 || len(resp.Pages) == 0 {
		t.Fatalf("route/page contracts missing: labels=%d pages=%d", len(resp.RouteLabels), len(resp.Pages))
	}
	if resp.Navigation.Primary[0].Label != "Control Tower" {
		t.Fatalf("first primary nav = %#v", resp.Navigation.Primary[0])
	}
	for _, group := range resp.Navigation.Groups {
		if group.ID == "pc" && group.Label != "Preventive Care" {
			t.Fatalf("preventive care group label = %q, want %q", group.Label, "Preventive Care")
		}
	}
	wantGroups := []struct {
		id    string
		label string
	}{
		{"counts", "Counts"},
		{"weighing", "Weight"},
		{"sales", "Sales"},
		{"feed", "Feed"},
		{"pc", "Preventive Care"},
		{"procurement", "Procurement"},
		{"others", "Others"},
	}
	if len(resp.Navigation.Groups) != len(wantGroups) {
		t.Fatalf("navigation groups = %#v, want %#v", resp.Navigation.Groups, wantGroups)
	}
	for i, want := range wantGroups {
		got := resp.Navigation.Groups[i]
		if got.ID != want.id || got.Label != want.label {
			t.Fatalf("navigation group %d = %s/%q, want %s/%q", i, got.ID, got.Label, want.id, want.label)
		}
	}
}

func TestBootstrapJSONDoesNotPublishNullCollections(t *testing.T) {
	raw, err := json.Marshal(NewService().Bootstrap(context.Background(), BootstrapInput{}))
	if err != nil {
		t.Fatalf("marshal bootstrap: %v", err)
	}
	if strings.Contains(string(raw), ":null") {
		t.Fatalf("bootstrap contract contains null collection fields: %s", raw)
	}
}

func TestBootstrapDoesNotPublishHardcodedLocationTruth(t *testing.T) {
	raw, err := json.Marshal(NewService().Bootstrap(context.Background(), BootstrapInput{}))
	if err != nil {
		t.Fatalf("marshal bootstrap: %v", err)
	}
	for _, forbidden := range []string{
		"00000000-0000-4000-8000-000000003001",
		"00000000-0000-4000-8000-000000003002",
		"park:CBE",
		"park:CPT",
		"Coimbatore",
		"Channapatna",
		"R. Teja",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("bootstrap contract contains hardcoded live-location/person truth %q", forbidden)
		}
	}
}

func TestVaccinationLeadershipCopyUsesActionablePenAndDateLanguage(t *testing.T) {
	page := pageByRouteID(t, NewService().Bootstrap(context.Background(), BootstrapInput{}).Pages, "vaccination")
	if got := page.Copy["note.sheds_counts"]; got != "Current status by pen. Up to date means no vaccination is currently due; actual and future vaccination dates are shown above." {
		t.Fatalf("vaccination shed note = %q", got)
	}
	if page.Copy["command_board.cohort_matrix.date_unavailable"] == "" {
		t.Fatal("vaccination cohort matrix must publish explicit unavailable-date copy")
	}
	if page.Copy["command_board.filter.operator_day"] != "Operator day (optional)" {
		t.Fatalf("operator-day filter copy = %q", page.Copy["command_board.filter.operator_day"])
	}

	var found bool
	for _, table := range page.Tables {
		if table.ID != "shed-summary" {
			continue
		}
		found = true
		labels := map[string]string{}
		for _, column := range table.Columns {
			labels[column.Key] = column.Label
		}
		if labels["due"] != "Needs action" || labels["done"] != "Up to date" {
			t.Fatalf("shed status labels = %#v", labels)
		}
	}
	if !found {
		t.Fatal("vaccination shed-summary table missing")
	}
}

func TestActionCenterParkDisplayChipsAreOptionalDbCompiledOverrides(t *testing.T) {
	page := pageByRouteID(t, NewService().Bootstrap(context.Background(), BootstrapInput{}).Pages, "action-center")
	group := optionGroupByID(t, page.OptionGroups, "park_display_chips")

	if len(group.Options) != 0 {
		t.Fatalf("phase-0 bootstrap must not publish static park chip options; locations must be DB-compiled, got %#v", group.Options)
	}
}

func TestMilkPreparationPageContractAndMilkNavigation(t *testing.T) {
	bootstrap := NewService().Bootstrap(context.Background(), BootstrapInput{})
	page := pageByRouteID(t, bootstrap.Pages, "milk-preparation")
	if page.Href != "/counts/milk-preparation" {
		t.Fatalf("milk preparation path=%q", page.Href)
	}
	if page.Title != "Milk Preparation" {
		t.Fatalf("milk preparation title=%q", page.Title)
	}
	for _, key := range []string{
		"section.preparation.title", "section.preparation.caption", "section.summary.aria",
		"kpi.sheds.label", "kpi.kids.label", "kpi.milk.label", "kpi.citric.label",
		"state.preparation_unavailable", "empty.preparation", "label.prepared_for",
	} {
		if page.Copy[key] == "" {
			t.Errorf("milk preparation contract missing copy key %q", key)
		}
	}

	// Milk Preparation stays out of Counts. It is now filed under the catch-all Others menu group
	// so the top-level order remains Counts, Weight, Sales, Feed, Preventive Care, Procurement,
	// Others (maintainer request 2026-09-02). The href is deliberately unchanged: this is a nav
	// regrouping, not a route change.
	found := false
	for _, group := range bootstrap.Navigation.Groups {
		for _, leaf := range group.Leaves {
			if leaf.Href != "/counts/milk-preparation" {
				continue
			}
			if group.ID == "counts" {
				t.Fatalf("Milk Preparation must not appear under the Counts group (leaf %q)", leaf.ID)
			}
			if group.ID != "others" {
				t.Fatalf("Milk Preparation leaf %q is under group %q, want group \"others\"", leaf.ID, group.ID)
			}
			if group.Label != "Others" {
				t.Fatalf("others group label=%q, want \"Others\"", group.Label)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("Milk navigation group is missing the Milk Preparation leaf")
	}
}

func TestActionCenterPublishesDriveCapacityBadgeCopy(t *testing.T) {
	page := pageByRouteID(t, NewService().Bootstrap(context.Background(), BootstrapInput{}).Pages, "action-center")
	for _, key := range []string{
		"label.drive_over_cap_required",
		"label.drive_medical_defer",
		"label.drive_terminal_closed",
		"tooltip.drive_over_cap_required",
		"tooltip.drive_medical_defer",
		"tooltip.drive_terminal_closed",
	} {
		if page.Copy[key] == "" {
			t.Fatalf("action-center contract missing copy key %s", key)
		}
	}
}

func TestActionsPageContractAndNavigation(t *testing.T) {
	bootstrap := NewService().Bootstrap(context.Background(), BootstrapInput{})
	if bootstrap.TopBar.DateRangeSelector.Label != "Showing data for" {
		t.Fatalf("top-bar date label must explain which day is rendered, got %q", bootstrap.TopBar.DateRangeSelector.Label)
	}
	for _, key := range []string{"date.menu_aria", "date.previous_month", "date.next_month", "date.today"} {
		if bootstrap.Copy[key] == "" {
			t.Fatalf("top-bar calendar copy %q must be backend-defined", key)
		}
	}
	page := pageByRouteID(t, bootstrap.Pages, "verification-review")
	if page.Href != "/verify" || page.Title != "Verify" {
		t.Fatalf("verify page = %#v", page)
	}
	if got := page.Tables[0]; got.ID != "verification-actions" || got.DataSource != "/verification/queue" || got.RowClick.Param != "vi_row" {
		t.Fatalf("actions table = %#v", got)
	}
	// The breadcrumb names the VERTICAL this screen belongs to. It said "Approvals" — a DIFFERENT
	// top-level module that merely sits next to it in the nav — so the screen advertised itself as
	// living somewhere it does not (maintainer, 2026-08-12).
	if page.Copy["crumb"] != "Verification" {
		t.Fatalf("Verify breadcrumb must name its own vertical, got %q", page.Copy["crumb"])
	}
	if page.Copy["crumb"] == "Approvals" {
		t.Fatalf("the breadcrumb must never name a sibling module")
	}
	for _, key := range []string{
		"state.empty",
		"drawer.media.title", "drawer.media.open", "action.open_details",
	} {
		if page.Copy[key] == "" {
			t.Fatalf("actions contract missing copy key %q", key)
		}
	}
	// The action-type filter was removed (maintainer decision 2026-08-07): the left nav is the
	// only scope selector on this screen. Asserted as ABSENT so the control cannot quietly
	// return through its copy keys.
	for _, key := range []string{"filter.action_type", "filter.all_action_types"} {
		if page.Copy[key] != "" {
			t.Fatalf("actions contract still carries removed action-type filter copy %q = %q", key, page.Copy[key])
		}
	}
	// The raw-token "vertical_module" column was dropped in the same decision: it rendered
	// "preventive_care / vaccination" verbatim on a verifier-facing screen.
	for _, column := range page.Tables[0].Columns {
		if column.Key == "vertical_module" {
			t.Fatalf("actions table still declares the raw-token vertical_module column: %#v", page.Tables[0].Columns)
		}
	}
	actions := primaryNavByID(t, bootstrap.Navigation.Primary, "verification-actions")
	// "Verify" is what the PHONE calls this (workforce nav.verify), so one word covers both surfaces.
	if actions.Href != "/verify" || actions.Label != "Verify" {
		t.Fatalf("verify nav = %#v", actions)
	}
	approvalsIndex := primaryNavIndex(bootstrap.Navigation.Primary, "approvals")
	actionsIndex := primaryNavIndex(bootstrap.Navigation.Primary, "verification-actions")
	if approvalsIndex < 0 || actionsIndex != approvalsIndex+1 {
		t.Fatalf("Actions must sit immediately below Approvals and above grouped modules: approvals=%d actions=%d primary=%#v", approvalsIndex, actionsIndex, bootstrap.Navigation.Primary)
	}
	operator := NewService().Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		Grants: []permissions.ActiveGrant{{
			Role: permissions.RoleOperator, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001",
		}},
	})
	blocked := primaryNavByID(t, operator.Navigation.Primary, "verification-actions")
	if blocked.Enabled || blocked.DisabledReason == "" {
		t.Fatalf("operator actions nav must fail closed = %#v", blocked)
	}
}

// TestVerifyPageOversightFiltersControlIsCapabilityGated pins the fix for the STG incident where
// the CEO's oversight filters (module chips, capture-date range) on /verify rendered for every
// role, including RoleVerifier. The renderer gates on the "oversight_filters" control, which must
// be enabled for CEO/CxO and directors who hold permissions.VerificationOversee and disabled for
// RoleVerifier. See docs/decisions/role-scoped-ui-is-capability-gated.md.
func TestVerifyPageOversightFiltersControlIsCapabilityGated(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    string
		enabled bool
	}{
		{"ceo_internal", permissions.RoleCEOInternal, true},
		{"pc_director", permissions.RolePCDirector, true},
		{"growth_director", permissions.RoleGrowthDirector, false},
		{"verifier", permissions.RoleVerifier, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: "00000000-0000-4000-8000-000000000001",
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants: []permissions.ActiveGrant{
					{Role: tc.role, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
				},
			})
			control := controlByID(t, pageByRouteID(t, resp.Pages, "verification-review").Controls, "oversight_filters")
			if control.Enabled != tc.enabled {
				t.Fatalf("%s oversight_filters.enabled = %v want %v (%#v)", tc.name, control.Enabled, tc.enabled, control)
			}
			if !tc.enabled && control.DisabledReason == "" {
				t.Fatalf("%s: disabled oversight_filters control must carry a backend disabled reason", tc.name)
			}
		})
	}
}

// TestVerifyPageOversightAnalyticsControlIsCapabilityGated pins the same capability gate
// (permissions.VerificationOversee) for the /verify oversight analytics section as
// TestVerifyPageOversightFiltersControlIsCapabilityGated pins for the filter chrome: CEO/directors
// who hold VerificationOversee get "oversight_analytics" enabled, the verifier does not.
// TestVerifyPageVideoLogControlIsCapabilityGated pins that the video_log control follows
// permissions.VerificationEvidenceTimeline and NOT VerificationOversee.
//
// The verifier row is the load-bearing one: she must get video_log ENABLED and oversight_analytics
// DISABLED from the same compile. Asserting both in one case is what stops a later change from
// collapsing the two controls back together in either direction.
func TestVerifyPageVideoLogControlIsCapabilityGated(t *testing.T) {
	for _, tc := range []struct {
		name             string
		role             string
		videoLogEnabled  bool
		oversightEnabled bool
	}{
		{"ceo_internal", permissions.RoleCEOInternal, true, true},
		{"pc_director", permissions.RolePCDirector, true, true},
		// The whole point of the separate capability.
		{"verifier", permissions.RoleVerifier, true, false},
		// Holds neither: no verification.review, so it cannot open /verify at all.
		{"growth_director", permissions.RoleGrowthDirector, false, false},
		{"operator", permissions.RoleOperator, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: "00000000-0000-4000-8000-000000000001",
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants: []permissions.ActiveGrant{
					{Role: tc.role, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
				},
			})
			controls := pageByRouteID(t, resp.Pages, "verification-review").Controls

			videoLog := controlByID(t, controls, "video_log")
			if videoLog.Enabled != tc.videoLogEnabled {
				t.Fatalf("%s video_log.enabled = %v want %v (%#v)", tc.name, videoLog.Enabled, tc.videoLogEnabled, videoLog)
			}
			if !tc.videoLogEnabled && videoLog.DisabledReason == "" {
				t.Fatalf("%s: disabled video_log control must carry a backend disabled reason", tc.name)
			}
			// The endpoint the panel reads must be declared on the control, so the contract states
			// which read this visibility gate is standing in front of.
			if videoLog.Action != "GET /verification/video-log" {
				t.Fatalf("%s video_log.action = %q want the video-log read", tc.name, videoLog.Action)
			}

			oversight := controlByID(t, controls, "oversight_analytics")
			if oversight.Enabled != tc.oversightEnabled {
				t.Fatalf("%s oversight_analytics.enabled = %v want %v — the video log must not widen a caller into the oversight chrome",
					tc.name, oversight.Enabled, tc.oversightEnabled)
			}
		})
	}
}

// TestVerifyPageRandomizationControlIsCapabilityGated pins the NARROWEST capability on /verify:
// permissions.VerificationSampling, which is CEO-ONLY (maintainer decision 2026-08-26).
//
// The pc_director row is the load-bearing one, and it is deliberately the opposite of every other
// control on this page: he holds VerificationOversee, so he gets the module chips, the capture-date
// range and the analytics -- and must NOT get this. Oversight WATCHES the verification workload;
// randomization DECIDES how much of it a human is required to watch, and a director setting that
// for his own department's work is the separation of duty that keeps verdict authority off
// leadership in the first place. Asserting oversight_analytics ENABLED and randomization DISABLED
// from the same compile is what stops a later change from collapsing the two capabilities together.
func TestVerifyPageRandomizationControlIsCapabilityGated(t *testing.T) {
	for _, tc := range []struct {
		name             string
		role             string
		randomization    bool
		oversightEnabled bool
	}{
		{"ceo_internal", permissions.RoleCEOInternal, true, true},
		// Holds oversight, must not hold this.
		{"pc_director", permissions.RolePCDirector, false, true},
		{"verifier", permissions.RoleVerifier, false, false},
		{"park_head", permissions.RoleParkHead, false, false},
		{"operator", permissions.RoleOperator, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: "00000000-0000-4000-8000-000000000001",
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants: []permissions.ActiveGrant{
					{Role: tc.role, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
				},
			})
			controls := pageByRouteID(t, resp.Pages, "verification-review").Controls

			randomization := controlByID(t, controls, "randomization")
			if randomization.Enabled != tc.randomization {
				t.Fatalf("%s randomization.enabled = %v want %v (%#v)", tc.name, randomization.Enabled, tc.randomization, randomization)
			}
			if !tc.randomization && randomization.DisabledReason == "" {
				t.Fatalf("%s: disabled randomization control must carry a backend disabled reason", tc.name)
			}
			// The endpoint the panel reads must be declared on the control, so the contract states
			// which read this visibility gate stands in front of.
			if randomization.Action != "GET /verification/sampling" {
				t.Fatalf("%s randomization.action = %q want the sampling read", tc.name, randomization.Action)
			}
			oversight := controlByID(t, controls, "oversight_analytics")
			if oversight.Enabled != tc.oversightEnabled {
				t.Fatalf("%s oversight_analytics.enabled = %v want %v -- randomization must not move the oversight gate",
					tc.name, oversight.Enabled, tc.oversightEnabled)
			}
		})
	}
}

func TestVerifyPageOversightAnalyticsControlIsCapabilityGated(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    string
		enabled bool
	}{
		{"ceo_internal", permissions.RoleCEOInternal, true},
		{"pc_director", permissions.RolePCDirector, true},
		{"growth_director", permissions.RoleGrowthDirector, false},
		{"verifier", permissions.RoleVerifier, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: "00000000-0000-4000-8000-000000000001",
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants: []permissions.ActiveGrant{
					{Role: tc.role, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
				},
			})
			control := controlByID(t, pageByRouteID(t, resp.Pages, "verification-review").Controls, "oversight_analytics")
			if control.Enabled != tc.enabled {
				t.Fatalf("%s oversight_analytics.enabled = %v want %v (%#v)", tc.name, control.Enabled, tc.enabled, control)
			}
			if !tc.enabled && control.DisabledReason == "" {
				t.Fatalf("%s: disabled oversight_analytics control must carry a backend disabled reason", tc.name)
			}
		})
	}
}

func TestCalendarOptionGroupsCoverProjectionStates(t *testing.T) {
	page := pageByRouteID(t, NewService().Bootstrap(context.Background(), BootstrapInput{}).Pages, "calendar")

	reminderKeys := optionKeys(optionGroupByID(t, page.OptionGroups, "calendar_reminder_state"))
	for _, key := range []string{"not_scheduled", "scheduled", "queued", "nudged", "snoozed", "sent", "escalated"} {
		if !reminderKeys[key] {
			t.Fatalf("calendar_reminder_state missing projected key %q", key)
		}
	}

	escalationKeys := optionKeys(optionGroupByID(t, page.OptionGroups, "calendar_escalation_state"))
	for _, key := range []string{
		"none", "pending", "queued", "escalated", "acknowledged", "resolved",
		"level_1_open", "level_2_open", "level_3_open", "level_4_open",
		"level_1_acknowledged", "level_2_acknowledged", "level_3_acknowledged", "level_4_acknowledged",
	} {
		if !escalationKeys[key] {
			t.Fatalf("calendar_escalation_state missing projected key %q", key)
		}
	}

	historyKeys := optionKeys(optionGroupByID(t, page.OptionGroups, "calendar_history_status"))
	for _, key := range []string{"open", "queued", "active", "acknowledged", "resolved", "replaced", "sent", "snoozed", "escalated"} {
		if !historyKeys[key] {
			t.Fatalf("calendar_history_status missing projected key %q", key)
		}
	}
}

func TestCalendarOwnerTabsHaveFallbackPresentationGroups(t *testing.T) {
	page := pageByRouteID(t, NewService().Bootstrap(context.Background(), BootstrapInput{}).Pages, "calendar")
	ownerTabs := optionGroupByID(t, page.OptionGroups, "calendar_owner_tabs")

	if len(ownerTabs.Options) == 0 {
		t.Fatal("calendar_owner_tabs must publish at least the all owner tab")
	}

	expectedWorkstreamGroups := map[string]bool{}
	expectedRhythmGroups := map[string]bool{}
	for _, owner := range ownerTabs.Options {
		workstreamID := "calendar_workstream_tabs_" + owner.Key
		rhythmID := "calendar_rhythm_days_" + owner.Key
		expectedWorkstreamGroups[workstreamID] = true
		expectedRhythmGroups[rhythmID] = true

		workstreams := optionGroupByID(t, page.OptionGroups, workstreamID)
		if len(workstreams.Options) == 0 {
			t.Fatalf("%s must publish at least one fallback workstream tab", workstreamID)
		}
		rhythm := optionGroupByID(t, page.OptionGroups, rhythmID)
		if len(rhythm.Options) != 7 {
			t.Fatalf("%s must publish one rhythm entry per week day, got %d", rhythmID, len(rhythm.Options))
		}
	}

	for _, group := range page.OptionGroups {
		if strings.HasPrefix(group.ID, "calendar_workstream_tabs_") && !expectedWorkstreamGroups[group.ID] {
			t.Fatalf("calendar workstream group %q has no matching calendar_owner_tabs option", group.ID)
		}
		if strings.HasPrefix(group.ID, "calendar_rhythm_days_") && !expectedRhythmGroups[group.ID] {
			t.Fatalf("calendar rhythm group %q has no matching calendar_owner_tabs option", group.ID)
		}
	}
}

func TestCalendarBootstrapPublishesHistoryAwareCopy(t *testing.T) {
	page := pageByRouteID(t, NewService().Bootstrap(context.Background(), BootstrapInput{}).Pages, "calendar")

	if page.Subtitle != "Vaccination due work and accepted completion history by time, owner lane, park, pen, and date." {
		t.Fatalf("calendar subtitle = %q", page.Subtitle)
	}
	if page.Copy["calendar.month.cell_note"] != "Each cell shows that day's due-work and completion markers. Tap an event for its rich detail." {
		t.Fatalf("calendar month cell note = %q", page.Copy["calendar.month.cell_note"])
	}
	for _, key := range []string{
		"calendar.drawer.eligible_animals",
		"calendar.drawer.deferred_animals",
		"calendar.drawer.blocked_animals",
		"calendar.drawer.linked_animals",
	} {
		if page.Copy[key] == "" {
			t.Fatalf("calendar drawer target copy %q missing", key)
		}
	}
}

func TestShedExecutionBootstrapPublishesAnimalRowActionCopy(t *testing.T) {
	page := pageByRouteID(t, NewService().Bootstrap(context.Background(), BootstrapInput{}).Pages, "shed-execution")
	if got := page.Copy["action.open_passport"]; got != "Open Animal Passport" {
		t.Fatalf("shed-execution action.open_passport copy = %q", got)
	}
	foundDriveRows := false
	for _, table := range page.Tables {
		if table.ID == "shed-drive-rows" {
			foundDriveRows = true
			break
		}
	}
	if !foundDriveRows {
		t.Fatal("shed-execution missing shed-drive-rows table contract")
	}
}

func TestSourceLoadContractPublishesProcurementSexSelector(t *testing.T) {
	page := pageByRouteID(t, NewService().Bootstrap(context.Background(), BootstrapInput{}).Pages, "source-load")
	if got := page.Copy["field.sex"]; got != "Sex" {
		t.Fatalf("source-load field.sex copy = %q", got)
	}
	if got := page.Copy["placeholder.sex"]; got == "" {
		t.Fatal("source-load placeholder.sex copy is missing")
	}
	sexes := optionKeys(optionGroupByID(t, page.OptionGroups, "proc_sex"))
	if len(sexes) != 2 || !sexes["female"] || !sexes["male"] || sexes["unknown"] {
		t.Fatalf("source-load proc_sex options must be female/male only, got %#v", sexes)
	}
}

func TestBootstrapCompilesDBBackedFamilies(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	if got := resp.TopBar.ParkSelector.Options; len(got) != 1 || got[0].Key != "park-1" || got[0].Label != "P1" {
		t.Fatalf("park selector options = %#v", got)
	}
	actionCenter := pageByRouteID(t, resp.Pages, "action-center")
	parkChips := optionGroupByID(t, actionCenter.OptionGroups, "park_display_chips")
	if len(parkChips.Options) != 1 || parkChips.Options[0].Key != "park-1" {
		t.Fatalf("park display chips = %#v", parkChips.Options)
	}
	config := pageByRouteID(t, resp.Pages, "vaccination-plan")
	ruleScopes := optionGroupByID(t, config.OptionGroups, "rule_scopes")
	if len(ruleScopes.Options) != 2 || ruleScopes.Options[1].Key != "park:park-1" || !strings.Contains(ruleScopes.Options[1].Label, "P1") {
		t.Fatalf("rule scopes were not DB compiled: %#v", ruleScopes.Options)
	}
	health := optionGroupByID(t, config.OptionGroups, "rule_healths")
	if len(health.Options) < 3 || health.Options[0].Key != "any" || !optionKeys(health)["sick"] || !optionKeys(health)["recovering"] {
		t.Fatalf("rule_healths must default to any and expose DB health states, got %#v", health.Options)
	}
	breeds := optionGroupByID(t, config.OptionGroups, "rule_breeds")
	if len(breeds.Options) < 2 || breeds.Options[1].Key != "DB Breed" {
		t.Fatalf("breed options were not DB compiled: %#v", breeds.Options)
	}
	deferStates := optionGroupByID(t, config.OptionGroups, "defer_states")
	deferKeys := optionKeys(deferStates)
	if !deferKeys["sick"] || !deferKeys["recovering"] || !deferKeys["quarantine"] {
		t.Fatalf("defer states must include hold-worthy health statuses, got %#v", deferStates.Options)
	}
	for _, opt := range deferStates.Options {
		if opt.Key == "healthy" {
			t.Fatalf("defer states must not include healthy status: %#v", deferStates.Options)
		}
	}
	if resp.FamilyHashes["locations"] == "" || resp.FamilyHashes["config"] == "" || resp.FamilyHashes["db:locations"] == "" {
		t.Fatalf("family hashes missing: %#v", resp.FamilyHashes)
	}
}

func TestBootstrapEmptyDBBackedFamiliesDoNotFallBackToStaticValues(t *testing.T) {
	resp := NewService(fakeEmptyFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	config := pageByRouteID(t, resp.Pages, "vaccination-plan")

	// rule_categories is a fixed visible vocabulary for this deploy: an empty DB must still expose
	// vaccination and the reopened Feed Direction Config template category. Future categories stay hidden.
	categories := optionGroupByID(t, config.OptionGroups, "rule_categories")
	if got := optionKeys(categories); len(got) != 2 || !got["vaccination"] || !got["feed_direction"] || got["deworming"] {
		t.Fatalf("empty DB rule_categories must expose only reopened categories, got %#v", categories.Options)
	}
	breeds := optionGroupByID(t, config.OptionGroups, "rule_breeds")
	if got := optionKeys(breeds); len(got) != 1 || !got["all"] || got["Beetal"] || got["Sirohi"] {
		t.Fatalf("empty DB rule_breeds must expose only the all sentinel, got %#v", breeds.Options)
	}
	health := optionGroupByID(t, config.OptionGroups, "rule_healths")
	if got := optionKeys(health); len(got) != 1 || !got["any"] || got["healthy"] {
		t.Fatalf("empty DB rule_healths must expose only the any sentinel, got %#v", health.Options)
	}
	repro := optionGroupByID(t, config.OptionGroups, "rule_reproductive")
	if got := optionKeys(repro); len(got) != 1 || !got["any"] || got["pregnant_only"] {
		t.Fatalf("empty DB rule_reproductive must expose only the any sentinel, got %#v", repro.Options)
	}
	deferStates := optionGroupByID(t, config.OptionGroups, "defer_states")
	deferKeys := optionKeys(deferStates)
	if len(deferKeys) != 5 || !deferKeys["sick"] || !deferKeys["under_treatment"] || !deferKeys["recovering"] || !deferKeys["quarantine"] || !deferKeys["icu"] {
		t.Fatalf("empty DB defer_states must expose kernel hold states only, got %#v", deferStates.Options)
	}
	sopLabels := optionGroupByID(t, config.OptionGroups, "schedule_sop_labels")
	if len(sopLabels.Options) != 0 {
		t.Fatalf("empty DB schedule_sop_labels must stay empty, got %#v", sopLabels.Options)
	}
	feedItems := optionGroupByID(t, config.OptionGroups, "feed_items")
	if got := optionKeys(feedItems); len(got) != 1 || !got["reviewed_template_rows"] || got["feed-1"] {
		t.Fatalf("empty DB config must expose only the feed template sentinel, got %#v", feedItems.Options)
	}
}

func TestBootstrapDisablesUnauthorizedNavFromRequestGrants(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleOperator, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	item := primaryNavByID(t, resp.Navigation.Primary, "action-center")
	if item.Enabled {
		t.Fatalf("action-center should be disabled for operator-only admin-web grant: %#v", item)
	}
	if item.DisabledReason == "" {
		t.Fatalf("disabled nav item must carry backend disabled reason: %#v", item)
	}
}

func TestDLQCenterSeparatesReadNavFromRepairActions(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RolePCDirector, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	item := navLeafByID(t, resp.Navigation.Groups, "dlq-center")
	if !item.Enabled {
		t.Fatalf("dlq-center should remain visible to DLQ read users: %#v", item)
	}
	page := pageByRouteID(t, resp.Pages, "dlq-center")
	actions := optionGroupByID(t, page.OptionGroups, "dlq_repair_actions")
	for _, action := range actions.Options {
		if action.Enabled {
			t.Fatalf("repair action %q should be disabled without operations.repair: %#v", action.Key, actions.Options)
		}
		if action.DisabledReason == "" {
			t.Fatalf("disabled repair action must explain the RBAC gate: %#v", action)
		}
	}
}

func TestBootstrapConfigSeparatesReadNavFromPublishAction(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RolePCDirector, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	item := navLeafByID(t, resp.Navigation.Groups, "vaccination-plan")
	if !item.Enabled {
		t.Fatalf("config should remain visible to protocol.read users: %#v", item)
	}
	control := controlByID(t, pageByRouteID(t, resp.Pages, "vaccination-plan").Controls, "publish_protocol_version")
	if control.Enabled {
		t.Fatalf("publish control should be disabled without protocol.publish: %#v", control)
	}
	if control.DisabledReason == "" {
		t.Fatalf("disabled publish control must carry backend disabled reason: %#v", control)
	}

	publisher := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	publishControl := controlByID(t, pageByRouteID(t, publisher.Pages, "vaccination-plan").Controls, "publish_protocol_version")
	if !publishControl.Enabled || publishControl.DisabledReason != "" {
		t.Fatalf("publish control should be enabled for protocol.publish: %#v", publishControl)
	}
}

func TestBootstrapConfigPublishesReopenedRuleAuthoringGroups(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	config := pageByRouteID(t, resp.Pages, "vaccination-plan")

	feedItems := optionGroupByID(t, config.OptionGroups, "feed_items")
	if got := optionKeys(feedItems); !got["reviewed_template_rows"] || !got["feed-1"] {
		t.Fatalf("config must expose feed template sentinel plus DB feed items, got %#v", feedItems.Options)
	}
	for _, groupID := range []string{"feed_classes", "feed_units", "feed_inventory_policies", "feed_source_tables", "feed_parameter_families", "feed_dimension_keys", "feed_ratio_policies", "feed_validation_checks", "feed_calculation_outputs"} {
		if !optionGroupExists(config.OptionGroups, groupID) {
			t.Fatalf("config must expose reopened Feed Direction option group %s", groupID)
		}
	}

	// rule_categories: static vocabulary for currently reopened Config slices.
	categories := optionGroupByID(t, config.OptionGroups, "rule_categories")
	if got := optionKeys(categories); len(got) != 2 || !got["vaccination"] || !got["feed_direction"] || got["deworming"] {
		t.Fatalf("rule_categories must expose only reopened categories, got %#v", categories.Options)
	}
	placeholders := optionGroupByID(t, config.OptionGroups, "protocol_placeholders")
	if got := optionKeys(placeholders); !got["vaccination.code"] || !got["vaccination.name"] || !got["feed_direction.code"] || !got["feed_direction.name"] || got["deworming.code"] {
		t.Fatalf("protocol placeholders must expose reopened categories only, got %#v", placeholders.Options)
	}
	sourceSystems := optionGroupByID(t, config.OptionGroups, "source_systems")
	if got := optionKeys(sourceSystems); !got["manual_admin"] || !got["vaccinations_db"] || !got["pc"] || !got["vet"] || !got["feed_direction_config_pack"] || !got["feed_directions_automation_db"] || !got["counting_db_values"] || !got["feed_transfer_kt"] || got["feed_master"] || got["nutritionist"] || got["ops_source"] {
		t.Fatalf("source systems must expose reviewed feed evidence sources without old aliases, got %#v", sourceSystems.Options)
	}
}

func TestBootstrapAppliesDBBackedStableUIConfigEntries(t *testing.T) {
	resp := NewService(fakeUIConfigFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})

	if resp.TopBar.ProductName != "Goat OS" {
		t.Fatalf("top bar product name was not config-overridden: %#v", resp.TopBar)
	}
	if item := primaryNavByID(t, resp.Navigation.Primary, "action-center"); item.Label != "Work Queue" {
		t.Fatalf("action-center nav label = %q", item.Label)
	}
	actionCenter := pageByRouteID(t, resp.Pages, "action-center")
	if actionCenter.Title != "Backend Work Queue" || actionCenter.Copy["page.title"] != "Backend Work Queue" {
		t.Fatalf("page title was not config-overridden: title=%q copy=%q", actionCenter.Title, actionCenter.Copy["page.title"])
	}
	if actionCenter.Subtitle != "Backend queue subtitle" {
		t.Fatalf("page subtitle was not config-overridden: %q", actionCenter.Subtitle)
	}
	if got := routeLabelByPattern(t, resp.RouteLabels, "/action-center"); got != "Backend Work Queue" {
		t.Fatalf("route label was not config-overridden: %q", got)
	}
	if labels := tableLabels(actionCenter, "work-board"); len(labels) < 2 || labels[1] != "Responsible" {
		t.Fatalf("work-board labels were not config-overridden: %#v", labels)
	}
	if got := optionLabelFromPage(t, actionCenter, "park_display_chips", "park-1"); got != "P1" {
		t.Fatalf("live park chip label must remain DB-owned, got %q", got)
	}
	if actionCenter.Copy["empty.work_board"] != "No backend work for this scope." {
		t.Fatalf("page copy was not config-overridden: %q", actionCenter.Copy["empty.work_board"])
	}
	config := pageByRouteID(t, resp.Pages, "vaccination-plan")
	for _, blocked := range []struct {
		group string
		key   string
		want  string
	}{
		{group: "rule_scopes", key: "park:park-1", want: "park: P1"},
		{group: "rule_breeds", key: "DB Breed", want: "DB Breed"},
		{group: "schedule_sop_labels", key: "sop-v1", want: "SOP v1"},
		{group: "feed_items", key: "feed-1", want: "Mesha concentrate"},
		{group: "source_systems", key: "manual_admin", want: "manual admin (not publishable)"},
	} {
		if got := optionLabelFromPage(t, config, blocked.group, blocked.key); got != blocked.want {
			t.Fatalf("DB-owned option %s.%s label must not be UI-config overridden: got %q want %q", blocked.group, blocked.key, got, blocked.want)
		}
	}
	if got := optionFromPage(t, config, "source_systems", "manual_admin").Tone; got != "warn" {
		t.Fatalf("source_systems.manual_admin tone must remain backend semantic metadata, got %q", got)
	}
}

func TestBootstrapCacheKeyIncludesDBFamilyRevisionInputs(t *testing.T) {
	repo := &revisionFamilies{revision: "rev-1"}
	service := NewService(repo)
	input := BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	}

	first := service.Bootstrap(context.Background(), input)
	repo.revision = "rev-2"
	second := service.Bootstrap(context.Background(), input)

	if first.ContractRevision == second.ContractRevision {
		t.Fatalf("contract revision did not change after DB family revision input changed: %q", first.ContractRevision)
	}
	if first.CachePolicy.ETag == second.CachePolicy.ETag {
		t.Fatalf("ETag did not change after DB family revision input changed: %q", first.CachePolicy.ETag)
	}
}

func TestBootstrapContractRevisionCanonicalizesGrantOrder(t *testing.T) {
	grants := []permissions.ActiveGrant{
		{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		{Role: permissions.RolePCDirector, ScopeType: "park", ScopeID: "park-1"},
	}
	reversed := []permissions.ActiveGrant{grants[1], grants[0]}

	first := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants:   grants,
	})
	second := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants:   reversed,
	})

	if first.ContractRevision != second.ContractRevision {
		t.Fatalf("same grant set in different order changed contract revision: first=%q second=%q", first.ContractRevision, second.ContractRevision)
	}
	if first.CachePolicy.ETag != second.CachePolicy.ETag {
		t.Fatalf("same grant set in different order changed ETag: first=%q second=%q", first.CachePolicy.ETag, second.CachePolicy.ETag)
	}
}

func TestBootstrapUsesRevisionCacheBeforeFullFamilyLoad(t *testing.T) {
	repo := &revisionAwareFamilies{revision: "rev-1"}
	service := NewService(repo)
	input := BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	}

	first := service.Bootstrap(context.Background(), input)
	second := service.Bootstrap(context.Background(), input)
	if first.ContractRevision != second.ContractRevision {
		t.Fatalf("unchanged revisions should return cached contract revision: first=%q second=%q", first.ContractRevision, second.ContractRevision)
	}
	if repo.fullLoads != 1 {
		t.Fatalf("unchanged revisions should perform one full family load, got %d", repo.fullLoads)
	}
	if repo.revisionLoads != 2 {
		t.Fatalf("each bootstrap should perform cheap revision probe, got %d", repo.revisionLoads)
	}

	repo.revision = "rev-2"
	third := service.Bootstrap(context.Background(), input)
	if third.ContractRevision == first.ContractRevision {
		t.Fatalf("changed revision input should invalidate cached contract revision: %q", third.ContractRevision)
	}
	if repo.fullLoads != 2 {
		t.Fatalf("changed revision should trigger second full family load, got %d", repo.fullLoads)
	}
}

func TestBootstrapCacheIsBoundedAndSweepsExpiredEntries(t *testing.T) {
	repo := &revisionAwareFamilies{revision: "rev-0"}
	service := NewService(repo)
	now := time.Date(2026, 6, 28, 8, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.cacheTTL = time.Second
	service.cacheMaxEntries = 4
	input := BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	}

	for i := 0; i < 10; i++ {
		repo.revision = fmt.Sprintf("rev-%02d", i)
		service.Bootstrap(context.Background(), input)
		if len(service.cache) > service.cacheMaxEntries {
			t.Fatalf("bootstrap cache exceeded max entries: len=%d max=%d", len(service.cache), service.cacheMaxEntries)
		}
	}

	now = now.Add(2 * time.Second)
	repo.revision = "rev-after-expiry"
	service.Bootstrap(context.Background(), input)
	if len(service.cache) > 2 {
		t.Fatalf("expired cache entries were not swept before store: len=%d cache=%#v", len(service.cache), service.cache)
	}
	for key, entry := range service.cache {
		if !entry.expiresAt.After(now) {
			t.Fatalf("cache contains expired entry %q: expires_at=%s now=%s", key, entry.expiresAt, now)
		}
	}
}

func TestBootstrapKeepsModeledNavAndAppliesRBACDisable(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleOperator, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})

	if len(resp.Navigation.Primary) != 7 {
		t.Fatalf("primary command-lens items must stay present, got %d", len(resp.Navigation.Primary))
	}
	// Approvals is present but RBAC-disabled for an operator, who holds no counts.approve_access.
	approvals := primaryNavByID(t, resp.Navigation.Primary, "approvals")
	if approvals.Enabled {
		t.Fatalf("approvals nav must be RBAC-disabled for an operator: %#v", approvals)
	}
	if approvals.DisabledReason == "" {
		t.Fatalf("approvals RBAC disable reason must be published: %#v", approvals)
	}
	leaf := navLeafByID(t, resp.Navigation.Groups, "preventive-care-vaccination")
	if leaf.Enabled {
		t.Fatalf("unauthorized leaf must stay RBAC-disabled: %#v", leaf)
	}
	if leaf.DisabledReason == "" {
		t.Fatalf("RBAC disable reason must be published: %#v", leaf)
	}
	if resp.NavChrome != domain.NavChromeExpanded {
		t.Fatalf("admin web nav chrome should stay expanded, got %q", resp.NavChrome)
	}
}

func TestBootstrapNavLeavesOnlyPointAtPublishedAdminWebPages(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})

	pageHrefs := map[string]bool{}
	for _, page := range resp.Pages {
		pageHrefs[page.Href] = true
	}
	for _, item := range resp.Navigation.Primary {
		if !pageHrefs[item.Href] {
			t.Fatalf("primary nav item %q points at %q, but no admin-web page contract publishes that href", item.ID, item.Href)
		}
	}
	for _, group := range resp.Navigation.Groups {
		for _, leaf := range group.Leaves {
			if !pageHrefs[leaf.Href] {
				t.Fatalf("nav leaf %s/%s points at %q, but no admin-web page contract publishes that href", group.ID, leaf.ID, leaf.Href)
			}
		}
	}
}

// Weighing is MOBILE ONLY (maintainer decision 2026-08-03). The admin-web weighing
// frontend was deleted; the backend admin-web bootstrap contract must never publish a
// weighing nav leaf, route label, or page contract again. Backend weighing APIs stay --
// the Android app is their only client.
func TestWeighingAdminWebSurfaceStaysMobileOnly(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RolePCDirector, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})

	if leaf := optionalNavLeafByID(resp.Navigation.Groups, "preventive-care-weighing"); leaf != nil {
		t.Fatalf("weighing is mobile-only: no admin-web sidebar leaf: %#v", leaf)
	}
	if label := optionalRouteLabelByPattern(resp.RouteLabels, "/weighing"); label != nil {
		t.Fatalf("weighing is mobile-only: no /weighing admin-web route label: %#v", label)
	}
	if page := optionalPageByRouteID(resp.Pages, "weighing"); page != nil {
		t.Fatalf("weighing is mobile-only: no admin-web page contract: %#v", page)
	}
}

type fakeFamilies struct{}

func (fakeFamilies) LoadContractFamilies(context.Context, string) (ReferenceFamilies, error) {
	return ReferenceFamilies{
		Parks:              []ReferenceOption{{Key: "park-1", Label: "P1", Title: "Park One", Tone: "info"}},
		RuleCategories:     []ReferenceOption{{Key: "vaccination", Label: "vaccination"}},
		Breeds:             []ReferenceOption{{Key: "DB Breed", Label: "DB Breed"}},
		HealthStatuses:     []ReferenceOption{{Key: "healthy", Label: "healthy"}, {Key: "sick", Label: "sick"}, {Key: "recovering", Label: "recovering"}},
		ReproductiveStates: []ReferenceOption{{Key: "pregnant", Label: "pregnant"}},
		DeferStates:        []ReferenceOption{{Key: "healthy", Label: "healthy"}, {Key: "sick", Label: "sick"}, {Key: "recovering", Label: "recovering"}, {Key: "quarantine", Label: "quarantine"}},
		SOPLabels:          []ReferenceOption{{Key: "sop-v1", Label: "SOP v1"}},
		FeedItems:          []ReferenceOption{{Key: "feed-1", Label: "Mesha concentrate", Title: "Mesha concentrate"}},
		RevisionInputs:     map[string]string{"locations": "park-1"},
	}, nil
}

type fakeEmptyFamilies struct{}

func (fakeEmptyFamilies) LoadContractFamilies(context.Context, string) (ReferenceFamilies, error) {
	return ReferenceFamilies{RevisionInputs: map[string]string{}}, nil
}

type fakeUIConfigFamilies struct{}

func (fakeUIConfigFamilies) LoadContractFamilies(ctx context.Context, tenantID string) (ReferenceFamilies, error) {
	families, err := fakeFamilies{}.LoadContractFamilies(ctx, tenantID)
	families.UIConfig = []ConfigEntry{
		{Key: "top_bar.product_name", Value: "Goat OS"},
		{Key: "nav.primary.action-center.label", Value: "Work Queue"},
		{RouteID: "action-center", Key: "page.title", Value: "Backend Work Queue"},
		{Key: "page.action-center.subtitle", Value: "Backend queue subtitle"},
		{RouteID: "action-center", Key: "copy.empty.work_board", Value: "No backend work for this scope."},
		{RouteID: "action-center", Key: "table.work-board.column.owner.label", Value: "Responsible"},
		{RouteID: "action-center", Key: "option.park_display_chips.park-1.label", Value: "Wrong park label"},
		{RouteID: "vaccination-plan", Key: "option.rule_scopes.park:park-1.label", Value: "Wrong park scope"},
		{RouteID: "vaccination-plan", Key: "option.rule_breeds.DB Breed.label", Value: "Wrong breed"},
		{RouteID: "vaccination-plan", Key: "option.schedule_sop_labels.sop-v1.label", Value: "Wrong SOP"},
		{RouteID: "vaccination-plan", Key: "option.feed_items.feed-1.label", Value: "Wrong feed"},
		{RouteID: "vaccination-plan", Key: "option.source_systems.manual_admin.label", Value: "Wrong source label"},
		{RouteID: "vaccination-plan", Key: "option.source_systems.manual_admin.tone", Value: "ok"},
	}
	families.RevisionInputs["admin-ui-config-values"] = "ui-config-rev-1"
	return families, err
}

type revisionFamilies struct {
	revision string
}

func (r *revisionFamilies) LoadContractFamilies(context.Context, string) (ReferenceFamilies, error) {
	families, err := fakeFamilies{}.LoadContractFamilies(context.Background(), "")
	families.RevisionInputs["admin-ui:config"] = r.revision
	return families, err
}

type revisionAwareFamilies struct {
	revision      string
	fullLoads     int
	revisionLoads int
}

func (r *revisionAwareFamilies) LoadContractFamilyRevisions(context.Context, string) (map[string]string, error) {
	r.revisionLoads++
	return map[string]string{"admin-ui:config": r.revision}, nil
}

func (r *revisionAwareFamilies) LoadContractFamilies(context.Context, string) (ReferenceFamilies, error) {
	r.fullLoads++
	families, err := fakeFamilies{}.LoadContractFamilies(context.Background(), "")
	families.RevisionInputs["admin-ui:config"] = r.revision
	return families, err
}

func pageByRouteID(t *testing.T, pages []domain.PageContract, routeID string) domain.PageContract {
	t.Helper()
	for _, page := range pages {
		if page.RouteID == routeID {
			return page
		}
	}
	t.Fatalf("missing page contract %q", routeID)
	return domain.PageContract{}
}

func optionalPageByRouteID(pages []domain.PageContract, routeID string) *domain.PageContract {
	for i := range pages {
		if pages[i].RouteID == routeID {
			return &pages[i]
		}
	}
	return nil
}

func primaryNavByID(t *testing.T, items []domain.NavigationItem, id string) domain.NavigationItem {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("missing primary nav item %q", id)
	return domain.NavigationItem{}
}

func primaryNavIndex(items []domain.NavigationItem, id string) int {
	for i, item := range items {
		if item.ID == id {
			return i
		}
	}
	return -1
}

func optionalNavLeafByID(groups []domain.NavigationGroup, id string) *domain.NavigationItem {
	for _, group := range groups {
		for i := range group.Leaves {
			if group.Leaves[i].ID == id {
				return &group.Leaves[i]
			}
		}
	}
	return nil
}

func navLeafByID(t *testing.T, groups []domain.NavigationGroup, id string) domain.NavigationItem {
	t.Helper()
	for _, group := range groups {
		for _, item := range group.Leaves {
			if item.ID == id {
				return item
			}
		}
	}
	t.Fatalf("missing nav leaf %q", id)
	return domain.NavigationItem{}
}

func controlByID(t *testing.T, controls []domain.Control, id string) domain.Control {
	t.Helper()
	for _, control := range controls {
		if control.ID == id {
			return control
		}
	}
	t.Fatalf("missing control %q", id)
	return domain.Control{}
}

func routeLabelByPattern(t *testing.T, labels []domain.RouteLabelRule, pattern string) string {
	t.Helper()
	for _, label := range labels {
		if label.Pattern == pattern {
			return label.Label
		}
	}
	t.Fatalf("missing route label pattern %q", pattern)
	return ""
}

func optionalRouteLabelByPattern(labels []domain.RouteLabelRule, pattern string) *domain.RouteLabelRule {
	for i := range labels {
		if labels[i].Pattern == pattern {
			return &labels[i]
		}
	}
	return nil
}

func optionGroupByID(t *testing.T, groups []domain.OptionGroup, id string) domain.OptionGroup {
	t.Helper()
	for _, group := range groups {
		if group.ID == id {
			return group
		}
	}
	t.Fatalf("missing option group %q", id)
	return domain.OptionGroup{}
}

func optionGroupExists(groups []domain.OptionGroup, id string) bool {
	for _, group := range groups {
		if group.ID == id {
			return true
		}
	}
	return false
}

func tableLabels(page domain.PageContract, tableID string) []string {
	for _, table := range page.Tables {
		if table.ID != tableID {
			continue
		}
		labels := make([]string, 0, len(table.Columns))
		for _, column := range table.Columns {
			if column.Visible {
				labels = append(labels, column.Label)
			}
		}
		return labels
	}
	return nil
}

func optionLabelFromPage(t *testing.T, page domain.PageContract, groupID, key string) string {
	t.Helper()
	return optionFromPage(t, page, groupID, key).Label
}

func optionFromPage(t *testing.T, page domain.PageContract, groupID, key string) domain.Option {
	t.Helper()
	group := optionGroupByID(t, page.OptionGroups, groupID)
	for _, option := range group.Options {
		if option.Key == key {
			return option
		}
	}
	t.Fatalf("missing option %s.%s", groupID, key)
	return domain.Option{}
}

func optionKeys(group domain.OptionGroup) map[string]bool {
	keys := make(map[string]bool, len(group.Options))
	for _, option := range group.Options {
		keys[option.Key] = true
	}
	return keys
}
