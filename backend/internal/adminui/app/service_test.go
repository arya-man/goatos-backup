package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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

func TestActionCenterParkDisplayChipsAreOptionalDbCompiledOverrides(t *testing.T) {
	page := pageByRouteID(t, NewService().Bootstrap(context.Background(), BootstrapInput{}).Pages, "action-center")
	group := optionGroupByID(t, page.OptionGroups, "park_display_chips")

	if len(group.Options) != 0 {
		t.Fatalf("phase-0 bootstrap must not publish static park chip options; locations must be DB-compiled, got %#v", group.Options)
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

func TestBootstrapCompilesDBBackedFamilies(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleAdmin, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
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
	config := pageByRouteID(t, resp.Pages, "config")
	ruleScopes := optionGroupByID(t, config.OptionGroups, "rule_scopes")
	if len(ruleScopes.Options) != 2 || ruleScopes.Options[1].Key != "park:park-1" || !strings.Contains(ruleScopes.Options[1].Label, "P1") {
		t.Fatalf("rule scopes were not DB compiled: %#v", ruleScopes.Options)
	}
	breeds := optionGroupByID(t, config.OptionGroups, "rule_breeds")
	if len(breeds.Options) < 2 || breeds.Options[1].Key != "DB Breed" {
		t.Fatalf("breed options were not DB compiled: %#v", breeds.Options)
	}
	deferStates := optionGroupByID(t, config.OptionGroups, "defer_states")
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
			{Role: permissions.RoleAdmin, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	config := pageByRouteID(t, resp.Pages, "config")

	categories := optionGroupByID(t, config.OptionGroups, "rule_categories")
	if len(categories.Options) != 0 {
		t.Fatalf("empty DB rule_categories must stay empty, got %#v", categories.Options)
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
	if len(deferStates.Options) != 0 {
		t.Fatalf("empty DB defer_states must stay empty, got %#v", deferStates.Options)
	}
	sopLabels := optionGroupByID(t, config.OptionGroups, "schedule_sop_labels")
	if len(sopLabels.Options) != 0 {
		t.Fatalf("empty DB schedule_sop_labels must stay empty, got %#v", sopLabels.Options)
	}
	feedItems := optionGroupByID(t, config.OptionGroups, "feed_items")
	if got := optionKeys(feedItems); len(got) != 1 || !got["custom"] || got["Mesha concentrate"] {
		t.Fatalf("feed_items must expose only the custom sentinel until a DB family exists, got %#v", feedItems.Options)
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
			{Role: permissions.RolePHCDirector, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
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

type fakeFamilies struct{}

func (fakeFamilies) LoadContractFamilies(context.Context, string) (ReferenceFamilies, error) {
	return ReferenceFamilies{
		Parks:              []ReferenceOption{{Key: "park-1", Label: "P1", Title: "Park One", Tone: "info"}},
		RuleCategories:     []ReferenceOption{{Key: "vaccination", Label: "vaccination"}},
		Breeds:             []ReferenceOption{{Key: "DB Breed", Label: "DB Breed"}},
		HealthStatuses:     []ReferenceOption{{Key: "healthy", Label: "healthy"}},
		ReproductiveStates: []ReferenceOption{{Key: "pregnant", Label: "pregnant"}},
		DeferStates:        []ReferenceOption{{Key: "healthy", Label: "healthy"}, {Key: "quarantine", Label: "quarantine"}},
		SOPLabels:          []ReferenceOption{{Key: "sop-v1", Label: "SOP v1"}},
		RevisionInputs:     map[string]string{"locations": "park-1"},
	}, nil
}

type fakeEmptyFamilies struct{}

func (fakeEmptyFamilies) LoadContractFamilies(context.Context, string) (ReferenceFamilies, error) {
	return ReferenceFamilies{RevisionInputs: map[string]string{}}, nil
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

func optionKeys(group domain.OptionGroup) map[string]bool {
	keys := make(map[string]bool, len(group.Options))
	for _, option := range group.Options {
		keys[option.Key] = true
	}
	return keys
}
