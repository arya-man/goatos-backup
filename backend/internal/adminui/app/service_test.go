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

	// rule_categories is a fixed PRODUCT vocabulary, not live tenant data: an empty DB must still
	// expose {vaccination, feed_direction} so the first protocol rule can be authored.
	categories := optionGroupByID(t, config.OptionGroups, "rule_categories")
	if got := optionKeys(categories); len(got) != 2 || !got["vaccination"] || !got["feed_direction"] {
		t.Fatalf("empty DB rule_categories must keep the fixed product vocabulary, got %#v", categories.Options)
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
		t.Fatalf("empty DB feed_items must expose only the custom sentinel, got %#v", feedItems.Options)
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

func TestBootstrapConfigCompilesFeedItemsAndCategories(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleAdmin, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	config := pageByRouteID(t, resp.Pages, "config")

	// feed_items: DB-backed active feed inventory item + the static 'custom' sentinel.
	feedItems := optionGroupByID(t, config.OptionGroups, "feed_items")
	if got := optionKeys(feedItems); !got["custom"] || !got["feed-1"] {
		t.Fatalf("feed_items must include DB items and the custom sentinel, got %#v", feedItems.Options)
	}

	// rule_categories: fixed vocab + DB categories, with 'vaccination' deduped to a single option.
	categories := optionGroupByID(t, config.OptionGroups, "rule_categories")
	if got := optionKeys(categories); !got["vaccination"] || !got["feed_direction"] {
		t.Fatalf("rule_categories must include the product vocabulary, got %#v", categories.Options)
	}
	vaccinationCount := 0
	for _, o := range categories.Options {
		if o.Key == "vaccination" {
			vaccinationCount++
		}
	}
	if vaccinationCount != 1 {
		t.Fatalf("rule_categories must dedup 'vaccination' across static+DB, got %d", vaccinationCount)
	}
}

func TestBootstrapAppliesDBBackedStableUIConfigEntries(t *testing.T) {
	resp := NewService(fakeUIConfigFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleAdmin, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
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
	if got := optionLabelFromPage(t, actionCenter, "work_state_filter_chips", "owner_missing"); got != "Needs owner" {
		t.Fatalf("work_state owner_missing label = %q", got)
	}
	if got := optionLabelFromPage(t, actionCenter, "park_display_chips", "park-1"); got != "P1" {
		t.Fatalf("live park chip label must remain DB-owned, got %q", got)
	}
	if actionCenter.Copy["empty.work_board"] != "No backend work for this scope." {
		t.Fatalf("page copy was not config-overridden: %q", actionCenter.Copy["empty.work_board"])
	}
	config := pageByRouteID(t, resp.Pages, "config")
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
			{Role: permissions.RoleAdmin, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
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

func TestBootstrapUsesRevisionCacheBeforeFullFamilyLoad(t *testing.T) {
	repo := &revisionAwareFamilies{revision: "rev-1"}
	service := NewService(repo)
	input := BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleAdmin, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
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
			{Role: permissions.RoleAdmin, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
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
		{RouteID: "action-center", Key: "option.work_state_filter_chips.owner_missing.label", Value: "Needs owner"},
		{RouteID: "action-center", Key: "option.park_display_chips.park-1.label", Value: "Wrong park label"},
		{RouteID: "config", Key: "option.rule_scopes.park:park-1.label", Value: "Wrong park scope"},
		{RouteID: "config", Key: "option.rule_breeds.DB Breed.label", Value: "Wrong breed"},
		{RouteID: "config", Key: "option.schedule_sop_labels.sop-v1.label", Value: "Wrong SOP"},
		{RouteID: "config", Key: "option.feed_items.feed-1.label", Value: "Wrong feed"},
		{RouteID: "config", Key: "option.source_systems.manual_admin.label", Value: "Wrong source label"},
		{RouteID: "config", Key: "option.source_systems.manual_admin.tone", Value: "ok"},
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
