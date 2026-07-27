package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

const (
	defaultContractCacheTTL        = 60 * time.Second
	defaultContractCacheMaxEntries = 512
	redisTTLHintSeconds            = 600
)

type BootstrapInput struct {
	TenantID string
	ActorID  string
	Grants   []permissions.ActiveGrant
	TraceID  string
}

type ReferenceRepository interface {
	LoadContractFamilies(ctx context.Context, tenantID string) (ReferenceFamilies, error)
}

type ReferenceRevisionRepository interface {
	LoadContractFamilyRevisions(ctx context.Context, tenantID string) (map[string]string, error)
}

type ReferenceFamilies struct {
	Parks              []ReferenceOption
	RuleCategories     []ReferenceOption
	Breeds             []ReferenceOption
	HealthStatuses     []ReferenceOption
	ReproductiveStates []ReferenceOption
	DeferStates        []ReferenceOption
	SOPLabels          []ReferenceOption
	FeedItems          []ReferenceOption
	UIConfig           []ConfigEntry
	RevisionInputs     map[string]string
}

type ReferenceOption struct {
	Key   string
	Label string
	Title string
	Tone  string
}

type ConfigEntry struct {
	RouteID string
	Key     string
	Value   string
}

type cacheEntry struct {
	expiresAt time.Time
	response  domain.BootstrapResponse
}

func (s *Service) bootstrapCached(ctx context.Context, input BootstrapInput) domain.BootstrapResponse {
	now := s.now()
	revisionKey := ""
	if revisions, ok := s.loadFamilyRevisions(ctx, input.TenantID); ok {
		revisionKey = s.cacheKey(input, ReferenceFamilies{RevisionInputs: revisions}, nil)
		if cached, ok := s.cached(revisionKey, now); ok {
			return cached
		}
	}
	families, familyErr := s.loadFamilies(ctx, input.TenantID)
	key := s.cacheKey(input, families, familyErr)
	if cached, ok := s.cached(key, now); ok {
		return cached
	}
	resp := s.compile(input, families, familyErr)
	expiresAt := now.Add(s.cacheTTL)
	s.storeCache(key, resp, now, expiresAt)
	if revisionKey != "" && familyErr == nil {
		s.storeCache(revisionKey, resp, now, expiresAt)
	}
	return resp
}

func (s *Service) cacheKey(input BootstrapInput, families ReferenceFamilies, familyErr error) string {
	roles := rolesFromGrants(input.Grants)
	sort.Strings(roles)
	grantParts := canonicalGrantParts(input.Grants)
	revisionParts := make([]string, 0, len(families.RevisionInputs))
	for key, value := range families.RevisionInputs {
		revisionParts = append(revisionParts, key+"="+value)
	}
	sort.Strings(revisionParts)
	errPart := ""
	if familyErr != nil {
		errPart = familyErr.Error()
	}
	return strings.Join([]string{
		strings.TrimSpace(input.TenantID),
		strings.Join(roles, ","),
		strings.Join(grantParts, ","),
		strings.Join(revisionParts, ","),
		errPart,
	}, "::")
}

func (s *Service) cached(key string, now time.Time) (domain.BootstrapResponse, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		return domain.BootstrapResponse{}, false
	}
	entry, ok := s.cache[key]
	if !ok || !entry.expiresAt.After(now) {
		delete(s.cache, key)
		return domain.BootstrapResponse{}, false
	}
	return entry.response, true
}

func (s *Service) storeCache(key string, resp domain.BootstrapResponse, now time.Time, expiresAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = map[string]cacheEntry{}
	}
	s.sweepExpiredCacheLocked(now)
	maxEntries := s.cacheMaxEntries
	if maxEntries <= 0 {
		maxEntries = 1
	}
	if _, exists := s.cache[key]; !exists {
		for len(s.cache) >= maxEntries {
			s.evictCacheEntryLocked()
		}
	}
	s.cache[key] = cacheEntry{expiresAt: expiresAt, response: resp}
}

func (s *Service) sweepExpiredCacheLocked(now time.Time) {
	for key, entry := range s.cache {
		if !entry.expiresAt.After(now) {
			delete(s.cache, key)
		}
	}
}

func (s *Service) evictCacheEntryLocked() {
	victimKey := ""
	var victimExpiresAt time.Time
	for key, entry := range s.cache {
		if victimKey == "" || entry.expiresAt.Before(victimExpiresAt) || (entry.expiresAt.Equal(victimExpiresAt) && key < victimKey) {
			victimKey = key
			victimExpiresAt = entry.expiresAt
		}
	}
	if victimKey != "" {
		delete(s.cache, victimKey)
	}
}

func (s *Service) compile(input BootstrapInput, families ReferenceFamilies, familyErr error) domain.BootstrapResponse {
	families.UIConfig = applicableConfigEntries(families.UIConfig)
	resp := baseBootstrap()
	resp = compileRequestContext(resp, input, families)
	resp = applyConfigEntries(resp, families.UIConfig)
	hashes := familyHashes(resp, families, input, familyErr)
	resp.FamilyHashes = hashes
	resp.ContractRevision = hashStruct(hashes)
	resp.CachePolicy = domain.ContractCachePolicy{
		ETag:            `W/"` + resp.ContractRevision + `"`,
		InProcessTTLSec: int(s.cacheTTL.Seconds()),
		RedisTTLHintSec: redisTTLHintSeconds,
		RevisionSource:  "tenant-role-family-hashes",
	}
	if familyErr != nil {
		resp.DisplayRules = append(resp.DisplayRules, domain.DisplayRule{
			ID:        "admin_ui_contract_family_load_error",
			AppliesTo: []string{"admin-web"},
			Summary:   "DB-backed admin-web contract families could not be loaded; stable product shell compiled without live entity options.",
			FrontendOwns: []string{
				"layout",
				"responsive density",
			},
			BackendOwns: []string{
				"contract-unavailable/family-load error visibility",
				"retry through SSR bootstrap",
			},
		})
	}
	return resp
}

func (s *Service) loadFamilies(ctx context.Context, tenantID string) (ReferenceFamilies, error) {
	if s.repo == nil || strings.TrimSpace(tenantID) == "" {
		return ReferenceFamilies{RevisionInputs: map[string]string{}}, nil
	}
	families, err := s.repo.LoadContractFamilies(ctx, tenantID)
	if families.RevisionInputs == nil {
		families.RevisionInputs = map[string]string{}
	}
	return families, err
}

func (s *Service) loadFamilyRevisions(ctx context.Context, tenantID string) (map[string]string, bool) {
	if s.repo == nil || strings.TrimSpace(tenantID) == "" {
		return map[string]string{}, true
	}
	repo, ok := s.repo.(ReferenceRevisionRepository)
	if !ok {
		return nil, false
	}
	revisions, err := repo.LoadContractFamilyRevisions(ctx, tenantID)
	if err != nil {
		return nil, false
	}
	if revisions == nil {
		revisions = map[string]string{}
	}
	return revisions, true
}

func compileRequestContext(resp domain.BootstrapResponse, input BootstrapInput, families ReferenceFamilies) domain.BootstrapResponse {
	resp.TopBar = compileTopBar(resp.TopBar, input, families)
	resp.RoleLenses = compileRoleLenses(input)
	resp.Navigation = compileNavigation(resp.Navigation, input)
	resp.NavChrome = domain.NavChromeExpanded
	resp.Pages = compilePages(resp.Pages, families, input)
	return resp
}

func applicableConfigEntries(entries []ConfigEntry) []ConfigEntry {
	out := make([]ConfigEntry, 0, len(entries))
	for _, entry := range entries {
		if configEntryApplies(entry) {
			out = append(out, entry)
		}
	}
	return out
}

func configEntryApplies(entry ConfigEntry) bool {
	key := strings.TrimSpace(entry.Key)
	if key == "" {
		return false
	}
	if strings.TrimSpace(entry.RouteID) != "" {
		return pageConfigEntryApplies(key)
	}
	return globalConfigEntryApplies(key)
}

func globalConfigEntryApplies(key string) bool {
	switch key {
	case "navigation.footer",
		"top_bar.product_name",
		"top_bar.logo_text",
		"top_bar.park_selector.label",
		"top_bar.park_selector.hint",
		"top_bar.date_range_selector.label",
		"top_bar.date_range_selector.hint",
		"top_bar.notifications.label",
		"top_bar.notifications.disabled_reason":
		return true
	}
	switch {
	case strings.HasPrefix(key, "chrome.copy."):
		return strings.TrimPrefix(key, "chrome.copy.") != ""
	case strings.HasPrefix(key, "nav.primary."):
		parts := strings.Split(key, ".")
		return len(parts) == 4 && parts[3] == "label"
	case strings.HasPrefix(key, "nav.group."):
		parts := strings.Split(key, ".")
		return len(parts) == 4 && parts[3] == "label"
	case strings.HasPrefix(key, "nav.leaf."):
		parts := strings.Split(key, ".")
		return len(parts) == 4 && parts[3] == "label"
	case strings.HasPrefix(key, "top_bar.scope_mode."):
		return topBarOptionConfigEntryApplies(key)
	case strings.HasPrefix(key, "top_bar.date_range."):
		return topBarOptionConfigEntryApplies(key)
	case strings.HasPrefix(key, "page."):
		parts := strings.Split(key, ".")
		return len(parts) == 3 && (parts[2] == "title" || parts[2] == "subtitle")
	default:
		return false
	}
}

func pageConfigEntryApplies(key string) bool {
	switch {
	case key == "title" || key == "page.title" || key == "subtitle" || key == "page.subtitle":
		return true
	case strings.HasPrefix(key, "copy."):
		return strings.TrimPrefix(key, "copy.") != ""
	case strings.HasPrefix(key, "section."):
		parts := strings.Split(key, ".")
		return len(parts) == 3 && parts[2] == "title"
	case strings.HasPrefix(key, "table."):
		parts := strings.Split(key, ".")
		return (len(parts) == 3 && parts[2] == "title") ||
			(len(parts) == 5 && parts[2] == "column" && parts[4] == "label") ||
			(len(parts) == 5 && parts[2] == "filter" && parts[4] == "label")
	case strings.HasPrefix(key, "option."):
		parts := strings.Split(key, ".")
		return len(parts) == 4 && stableUIConfigOptionField(parts[1], parts[3])
	default:
		return false
	}
}

func topBarOptionConfigEntryApplies(key string) bool {
	parts := strings.Split(key, ".")
	return len(parts) == 4 && (parts[3] == "label" || parts[3] == "title" || parts[3] == "disabled_reason")
}

func applyConfigEntries(resp domain.BootstrapResponse, entries []ConfigEntry) domain.BootstrapResponse {
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		value := entry.Value
		if key == "" {
			continue
		}
		if strings.TrimSpace(entry.RouteID) != "" {
			resp.Pages = applyPageConfigEntry(resp.Pages, entry.RouteID, key, value)
			if key == "title" || key == "page.title" {
				resp.RouteLabels = applyRouteLabel(resp.RouteLabels, resp.Pages, entry.RouteID, value)
			}
			continue
		}
		resp = applyGlobalConfigEntry(resp, key, value)
	}
	return resp
}

func applyGlobalConfigEntry(resp domain.BootstrapResponse, key, value string) domain.BootstrapResponse {
	switch {
	case key == "navigation.footer":
		resp.Navigation.Footer = value
	case key == "top_bar.product_name":
		resp.TopBar.ProductName = value
	case key == "top_bar.logo_text":
		resp.TopBar.LogoText = value
	case key == "top_bar.park_selector.label":
		resp.TopBar.ParkSelector.Label = value
	case key == "top_bar.park_selector.hint":
		resp.TopBar.ParkSelector.Hint = value
	case key == "top_bar.date_range_selector.label":
		resp.TopBar.DateRangeSelector.Label = value
	case key == "top_bar.date_range_selector.hint":
		resp.TopBar.DateRangeSelector.Hint = value
	case key == "top_bar.notifications.label":
		resp.TopBar.Notifications.Label = value
	case key == "top_bar.notifications.disabled_reason":
		resp.TopBar.Notifications.DisabledReason = value
	case strings.HasPrefix(key, "chrome.copy."):
		copyKey := strings.TrimPrefix(key, "chrome.copy.")
		if copyKey != "" {
			resp.Copy[copyKey] = value
		}
	case strings.HasPrefix(key, "nav.primary."):
		parts := strings.Split(key, ".")
		if len(parts) == 4 && parts[3] == "label" {
			resp.Navigation.Primary = applyNavItemLabel(resp.Navigation.Primary, parts[2], value)
		}
	case strings.HasPrefix(key, "nav.group."):
		parts := strings.Split(key, ".")
		if len(parts) == 4 && parts[3] == "label" {
			for i := range resp.Navigation.Groups {
				if resp.Navigation.Groups[i].ID == parts[2] {
					resp.Navigation.Groups[i].Label = value
				}
			}
		}
	case strings.HasPrefix(key, "nav.leaf."):
		parts := strings.Split(key, ".")
		if len(parts) == 4 && parts[3] == "label" {
			for i := range resp.Navigation.Groups {
				resp.Navigation.Groups[i].Leaves = applyNavItemLabel(resp.Navigation.Groups[i].Leaves, parts[2], value)
			}
		}
	case strings.HasPrefix(key, "top_bar.scope_mode."):
		parts := strings.Split(key, ".")
		if len(parts) == 4 {
			resp.TopBar.ScopeModeToggle = applyTopBarOptionValue(resp.TopBar.ScopeModeToggle, parts[2], parts[3], value)
		}
	case strings.HasPrefix(key, "top_bar.date_range."):
		parts := strings.Split(key, ".")
		if len(parts) == 4 {
			resp.TopBar.DateRangeSelector.Options = applyTopBarOptionValue(resp.TopBar.DateRangeSelector.Options, parts[2], parts[3], value)
		}
	case strings.HasPrefix(key, "page."):
		parts := strings.Split(key, ".")
		if len(parts) == 3 && (parts[2] == "title" || parts[2] == "subtitle") {
			resp.Pages = applyPageConfigEntry(resp.Pages, parts[1], parts[2], value)
			if parts[2] == "title" {
				resp.RouteLabels = applyRouteLabel(resp.RouteLabels, resp.Pages, parts[1], value)
			}
		}
	}
	return resp
}

func applyPageConfigEntry(pages []domain.PageContract, routeID, key, value string) []domain.PageContract {
	out := make([]domain.PageContract, len(pages))
	copy(out, pages)
	for i := range out {
		if out[i].RouteID != routeID {
			continue
		}
		switch {
		case key == "title" || key == "page.title":
			out[i].Title = value
			out[i].Copy["page.title"] = value
			out[i].Sections = applySectionTitle(out[i].Sections, "primary", value)
		case key == "subtitle" || key == "page.subtitle":
			out[i].Subtitle = value
			out[i].Copy["page.subtitle"] = value
		case strings.HasPrefix(key, "copy."):
			copyKey := strings.TrimPrefix(key, "copy.")
			if copyKey != "" {
				out[i].Copy[copyKey] = value
			}
		case strings.HasPrefix(key, "section."):
			parts := strings.Split(key, ".")
			if len(parts) == 3 && parts[2] == "title" {
				out[i].Sections = applySectionTitle(out[i].Sections, parts[1], value)
			}
		case strings.HasPrefix(key, "table."):
			out[i].Tables = applyTableConfigEntry(out[i].Tables, key, value)
		case strings.HasPrefix(key, "option."):
			out[i].OptionGroups = applyOptionConfigEntry(out[i].OptionGroups, key, value)
		}
		return out
	}
	return out
}

func applyNavItemLabel(items []domain.NavigationItem, id, value string) []domain.NavigationItem {
	out := make([]domain.NavigationItem, len(items))
	copy(out, items)
	for i := range out {
		if out[i].ID == id {
			out[i].Label = value
		}
	}
	return out
}

func applyTopBarOptionValue(options []domain.TopBarOption, key, field, value string) []domain.TopBarOption {
	out := make([]domain.TopBarOption, len(options))
	copy(out, options)
	for i := range out {
		if out[i].Key != key {
			continue
		}
		switch field {
		case "label":
			out[i].Label = value
		case "title":
			out[i].Title = value
		case "disabled_reason":
			out[i].DisabledReason = value
		}
	}
	return out
}

func applyRouteLabel(labels []domain.RouteLabelRule, pages []domain.PageContract, routeID, value string) []domain.RouteLabelRule {
	pattern := ""
	for _, page := range pages {
		if page.RouteID == routeID {
			pattern = page.PathPattern
			break
		}
	}
	if pattern == "" {
		return labels
	}
	out := make([]domain.RouteLabelRule, len(labels))
	copy(out, labels)
	for i := range out {
		if out[i].Pattern == pattern {
			out[i].Label = value
		}
	}
	return out
}

func applySectionTitle(sections []domain.Section, id, value string) []domain.Section {
	out := make([]domain.Section, len(sections))
	copy(out, sections)
	for i := range out {
		if out[i].ID == id {
			out[i].Title = value
		}
	}
	return out
}

func applyTableConfigEntry(tables []domain.TableContract, key, value string) []domain.TableContract {
	parts := strings.Split(key, ".")
	if len(parts) < 3 {
		return tables
	}
	tableID := parts[1]
	out := make([]domain.TableContract, len(tables))
	copy(out, tables)
	for i := range out {
		if out[i].ID != tableID {
			continue
		}
		if len(parts) == 3 && parts[2] == "title" {
			out[i].Title = value
			return out
		}
		if len(parts) == 5 && parts[2] == "column" && parts[4] == "label" {
			columns := make([]domain.Column, len(out[i].Columns))
			copy(columns, out[i].Columns)
			for j := range columns {
				if columns[j].Key == parts[3] {
					columns[j].Label = value
				}
			}
			out[i].Columns = columns
			return out
		}
		if len(parts) == 5 && parts[2] == "filter" && parts[4] == "label" {
			filters := make([]domain.Filter, len(out[i].Filters))
			copy(filters, out[i].Filters)
			for j := range filters {
				if filters[j].Key == parts[3] {
					filters[j].Label = value
				}
			}
			out[i].Filters = filters
			return out
		}
	}
	return out
}

func applyOptionConfigEntry(groups []domain.OptionGroup, key, value string) []domain.OptionGroup {
	parts := strings.Split(key, ".")
	if len(parts) != 4 {
		return groups
	}
	groupID := parts[1]
	optionKey := parts[2]
	field := parts[3]
	if !stableUIConfigOptionField(groupID, field) {
		return groups
	}
	out := make([]domain.OptionGroup, len(groups))
	copy(out, groups)
	for i := range out {
		if out[i].ID != groupID {
			continue
		}
		options := make([]domain.Option, len(out[i].Options))
		copy(options, out[i].Options)
		for j := range options {
			if options[j].Key != optionKey {
				continue
			}
			switch field {
			case "label":
				options[j].Label = value
			case "title":
				options[j].Title = value
			case "tone":
				options[j].Tone = value
			case "disabled_reason":
				options[j].DisabledReason = value
			}
		}
		out[i].Options = options
		return out
	}
	return out
}

func stableUIConfigOptionField(groupID, field string) bool {
	fields, ok := stableUIConfigOptionGroups[groupID]
	if !ok {
		return false
	}
	_, ok = fields[field]
	return ok
}

var presentationOptionFields = map[string]struct{}{
	"label":           {},
	"title":           {},
	"tone":            {},
	"disabled_reason": {},
}

var stableUIConfigOptionGroups = map[string]map[string]struct{}{
	"adverse_reaction":                        presentationOptionFields,
	"audit_operation_families":                presentationOptionFields,
	"audit_status_tabs":                       presentationOptionFields,
	"calendar_escalation_state":               presentationOptionFields,
	"calendar_event_types":                    presentationOptionFields,
	"calendar_history_status":                 presentationOptionFields,
	"calendar_links":                          presentationOptionFields,
	"calendar_months":                         presentationOptionFields,
	"calendar_owner_tabs":                     presentationOptionFields,
	"calendar_reminder_state":                 presentationOptionFields,
	"calendar_rhythm_days_admin_data_ops":     presentationOptionFields,
	"calendar_rhythm_days_all":                presentationOptionFields,
	"calendar_rhythm_days_inventory":          presentationOptionFields,
	"calendar_rhythm_days_pc":                 presentationOptionFields,
	"calendar_severity":                       presentationOptionFields,
	"calendar_status":                         presentationOptionFields,
	"calendar_view_tabs":                      presentationOptionFields,
	"calendar_weekdays":                       presentationOptionFields,
	"calendar_workstream_tabs_admin_data_ops": presentationOptionFields,
	"calendar_workstream_tabs_all":            presentationOptionFields,
	"calendar_workstream_tabs_inventory":      presentationOptionFields,
	"calendar_workstream_tabs_pc":             presentationOptionFields,
	"chain_steps":                             presentationOptionFields,
	"cohort_detail_facets":                    presentationOptionFields,
	"dlq_repair_actions":                      presentationOptionFields,
	"dlq_status_tabs":                         presentationOptionFields,
	"domain_chips":                            presentationOptionFields,
	"drive_steps":                             presentationOptionFields,
	"evidence_types":                          presentationOptionFields,
	"filter_quick_terms":                      presentationOptionFields,
	"health_selection_states":                 presentationOptionFields,
	"herd_filter_extra_facets":                presentationOptionFields,
	"herd_import_columns":                     presentationOptionFields,
	"journey_stages":                          presentationOptionFields,
	"matrix_states":                           presentationOptionFields,
	"new_drive_steps":                         presentationOptionFields,
	"obligation_count_chips":                  presentationOptionFields,
	"priority_chips":                          presentationOptionFields,
	"proc_arrival_counts":                     presentationOptionFields,
	"proc_arrival_state":                      presentationOptionFields,
	"proc_arrival_status":                     presentationOptionFields,
	"proc_decision_type":                      presentationOptionFields,
	"proc_discrepancy_state":                  presentationOptionFields,
	"proc_goat_state":                         presentationOptionFields,
	"proc_handoff_status":                     presentationOptionFields,
	"proc_health_state":                       presentationOptionFields,
	"proc_hf_review_status":                   presentationOptionFields,
	"proc_source_entry_state":                 presentationOptionFields,
	"proc_intake_signal":                      presentationOptionFields,
	"proc_ownership_state":                    presentationOptionFields,
	"proc_purpose":                            presentationOptionFields,
	"proc_selection_state":                    presentationOptionFields,
	"proc_transit_status":                     presentationOptionFields,
	"proc_warmup_state":                       presentationOptionFields,
	"proof_state_chips":                       presentationOptionFields,
	"proof_types":                             presentationOptionFields,
	"protocol_rule_status":                    presentationOptionFields,
	"shed_event_facets":                       presentationOptionFields,
	"sop_seed_steps":                          presentationOptionFields,
	"sop_state_chips":                         presentationOptionFields,
	"sop_trigger_chips":                       presentationOptionFields,
	"source_load_status":                      presentationOptionFields,
	"status_matrix_facets":                    presentationOptionFields,
	"supplier_warmup_facets":                  presentationOptionFields,
	"vaccination_drive_sop_steps":             presentationOptionFields,
	"vaccination_import_columns":              presentationOptionFields,
	"verification_state_chips":                presentationOptionFields,
	"warmup_evidence_states":                  presentationOptionFields,
	"warmup_expectations":                     presentationOptionFields,
	"work_state_board_columns":                presentationOptionFields,
	"work_state_filter_chips":                 presentationOptionFields,
}

func compileTopBar(top domain.TopBarContract, input BootstrapInput, families ReferenceFamilies) domain.TopBarContract {
	top.ParkSelector.Options = topBarOptions(families.Parks)
	top.RolePreview = rolePreview(input, len(families.Parks))
	return top
}

func topBarOptions(options []ReferenceOption) []domain.TopBarOption {
	out := make([]domain.TopBarOption, 0, len(options))
	for _, option := range options {
		out = append(out, domain.TopBarOption{
			Key:     option.Key,
			Label:   option.Label,
			Title:   option.Title,
			Enabled: true,
		})
	}
	return out
}

func rolePreview(input BootstrapInput, parkCount int) domain.RolePreviewActor {
	roles := rolesFromGrants(input.Grants)
	role := highestRole(roles)
	name := roleLensName(role)
	scope := scopeSummary(input.Grants, parkCount)
	return domain.RolePreviewActor{
		DisplayName: name,
		Initials:    roleInitials(role),
		Subtitle:    scope,
	}
}

func compileRoleLenses(input BootstrapInput) []domain.RoleLensContract {
	roles := rolesFromGrants(input.Grants)
	if len(roles) == 0 {
		return roleLenses()
	}
	if hasAnyRole(roles, permissions.RoleCEOInternal) {
		return roleLenses()
	}
	out := make([]domain.RoleLensContract, 0, len(roles))
	for _, role := range roles {
		out = append(out, roleLensForRole(role))
	}
	return out
}

func compileNavigation(nav domain.NavigationContract, input BootstrapInput) domain.NavigationContract {
	// RBAC enable/disable: disables unauthorized items in place when the actor carries grants.
	if len(input.Grants) > 0 {
		nav.Primary = compileNavItems(nav.Primary, input)
		for i := range nav.Groups {
			nav.Groups[i].Leaves = compileNavItems(nav.Groups[i].Leaves, input)
		}
	}
	return nav
}

func compileNavItems(items []domain.NavigationItem, input BootstrapInput) []domain.NavigationItem {
	out := make([]domain.NavigationItem, 0, len(items))
	for _, item := range items {
		required := permissionsForNav(item.ID)
		if len(required) > 0 && !grantsAuthorize(input.Grants, input.TenantID, required) {
			item.Enabled = false
			item.DisabledReason = "Your current role scope does not include this admin-web route."
		}
		out = append(out, item)
	}
	return out
}

func compilePages(pages []domain.PageContract, families ReferenceFamilies, input BootstrapInput) []domain.PageContract {
	out := make([]domain.PageContract, len(pages))
	copy(out, pages)
	for i := range out {
		switch out[i].RouteID {
		case "action-center", "vaccination", "shed-execution":
			out[i].OptionGroups = replaceOptionGroup(out[i].OptionGroups, "park_display_chips", optionsFromReferences(families.Parks, "info"))
		case "config":
			out[i].OptionGroups = compileConfigOptionGroups(out[i].OptionGroups, families)
			out[i].Controls = compileConfigControls(out[i].Controls, input, out[i].Copy)
		case "herd-register":
			// Source-backed reproductive vocabulary for the Herd Register reproductive edit drawer.
			// Same status_definitions family the Config rule editor uses, minus the "any" sentinel.
			out[i].OptionGroups = replaceOptionGroup(out[i].OptionGroups, "herd_reproductive", optionsFromReferences(families.ReproductiveStates, ""))
		case "feed-direction", "feed-packing", "feed-config":
			// Live feed vocabulary. feedOptionGroups() declares only fixed schema constraints;
			// the actual feed items are tenant data from feed_item_catalog and arrive here as
			// ReferenceFamilies.FeedItems. This is the intended injection path — the alternative
			// (a constant list of item labels in contract code) is the banned pattern.
			out[i].OptionGroups = mergeOptionGroupReferences(out[i].OptionGroups, "feed_items", families.FeedItems, "")
			out[i].OptionGroups = replaceOptionGroup(out[i].OptionGroups, "feed_parks", optionsFromReferences(families.Parks, "info"))
			out[i].OptionGroups = replaceOptionGroup(out[i].OptionGroups, "feed_breeds", optionsFromReferences(families.Breeds, ""))
		case "dlq-center":
			out[i].OptionGroups = compileDLQOptionGroups(out[i].OptionGroups, input)
		}
	}
	return out
}

func compileConfigControls(controls []domain.Control, input BootstrapInput, copy map[string]string) []domain.Control {
	allowed := len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.ProtocolPublish})
	reason := ""
	if !allowed {
		reason = strings.TrimSpace(copy["modal.rule_editor.only_ceo_publish"])
		if reason == "" {
			reason = "Your current role cannot publish protocol versions."
		}
	}
	return upsertControl(controls, domain.Control{
		ID:             "publish_protocol_version",
		Label:          copy["action.publish"],
		Kind:           "primary_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "POST /protocols/versions/{version_id}/publish",
	})
}

func compileConfigOptionGroups(groups []domain.OptionGroup, families ReferenceFamilies) []domain.OptionGroup {
	out := groups
	// rule_categories is intentionally a bounded visible vocabulary for reopened Config slices.
	// DB-discovered future categories must not leak into the deployed Config contract.
	out = replaceOptionGroup(out, "rule_scopes", ruleScopeOptions(families.Parks))
	out = replaceOptionGroup(out, "rule_breeds", prependOption("all", "all", "", "", optionsFromReferences(families.Breeds, "")))
	out = replaceOptionGroup(out, "rule_healths", prependOption("any", "any", "", "", optionsFromReferences(families.HealthStatuses, "")))
	out = replaceOptionGroup(out, "rule_reproductive", prependOption("any", "any", "", "", optionsFromReferences(families.ReproductiveStates, "")))
	out = replaceOptionGroup(out, "defer_states", optionsFromReferences(defaultedDeferableStates(families.DeferStates), ""))
	out = replaceOptionGroup(out, "schedule_sop_labels", optionsFromReferences(families.SOPLabels, ""))
	out = mergeOptionGroupReferences(out, "feed_items", families.FeedItems, "")
	return out
}

func compileDLQOptionGroups(groups []domain.OptionGroup, input BootstrapInput) []domain.OptionGroup {
	if len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.OperationsRepair}) {
		return groups
	}
	out := make([]domain.OptionGroup, len(groups))
	copy(out, groups)
	for i := range out {
		if out[i].ID != "dlq_repair_actions" {
			continue
		}
		options := make([]domain.Option, len(out[i].Options))
		copy(options, out[i].Options)
		for j := range options {
			options[j].Enabled = false
			options[j].DisabledReason = "Your current role can inspect DLQ events but cannot replay or discard them."
		}
		out[i].Options = options
	}
	return out
}

func ruleScopeOptions(parks []ReferenceOption) []domain.Option {
	options := []domain.Option{option("tenant", "tenant (company default)", "", "")}
	for _, park := range parks {
		label := strings.TrimSpace(park.Label)
		if label == "" {
			label = park.Key
		}
		options = append(options, option("park:"+park.Key, "park: "+label, park.Title, "info"))
	}
	return options
}

func deferableStates(options []ReferenceOption) []ReferenceOption {
	out := make([]ReferenceOption, 0, len(options))
	for _, option := range options {
		switch strings.ToLower(strings.TrimSpace(option.Key)) {
		case "", "healthy", "normal", "ok":
			continue
		default:
			out = append(out, option)
		}
	}
	return out
}

func defaultedDeferableStates(options []ReferenceOption) []ReferenceOption {
	defaults := []ReferenceOption{
		{Key: "sick", Label: "sick"},
		{Key: "under_treatment", Label: "under treatment"},
		{Key: "recovering", Label: "recovering"},
		{Key: "quarantine", Label: "quarantine"},
		{Key: "icu", Label: "ICU"},
	}
	out := deferableStates(options)
	seen := make(map[string]bool, len(out)+len(defaults))
	merged := make([]ReferenceOption, 0, len(defaults)+len(out))
	for _, option := range append(defaults, out...) {
		key := strings.ToLower(strings.TrimSpace(option.Key))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, option)
	}
	return merged
}

func replaceOptionGroup(groups []domain.OptionGroup, id string, options []domain.Option) []domain.OptionGroup {
	out := make([]domain.OptionGroup, len(groups))
	copy(out, groups)
	for i := range out {
		if out[i].ID == id {
			out[i].Options = options
			return out
		}
	}
	return append(out, domain.OptionGroup{ID: id, Options: options})
}

func upsertControl(controls []domain.Control, control domain.Control) []domain.Control {
	out := make([]domain.Control, len(controls))
	copy(out, controls)
	for i := range out {
		if out[i].ID == control.ID {
			out[i] = control
			return out
		}
	}
	return append(out, control)
}

// mergeOptionGroupReferences keeps a group's existing options as a bounded vocabulary / sentinel and appends
// DB-discovered references not already present (dedup by key). Use this only when a visible group is expected
// to merge static and DB-backed values; replaceOptionGroup is for pure live-data families.
func mergeOptionGroupReferences(groups []domain.OptionGroup, id string, refs []ReferenceOption, defaultTone string) []domain.OptionGroup {
	out := make([]domain.OptionGroup, len(groups))
	copy(out, groups)
	for i := range out {
		if out[i].ID != id {
			continue
		}
		seen := make(map[string]struct{}, len(out[i].Options)+len(refs))
		merged := make([]domain.Option, len(out[i].Options))
		copy(merged, out[i].Options)
		for _, o := range out[i].Options {
			seen[o.Key] = struct{}{}
		}
		for _, ref := range refs {
			if _, ok := seen[ref.Key]; ok {
				continue
			}
			seen[ref.Key] = struct{}{}
			tone := ref.Tone
			if tone == "" {
				tone = defaultTone
			}
			merged = append(merged, option(ref.Key, ref.Label, ref.Title, tone))
		}
		out[i].Options = merged
		return out
	}
	return append(out, domain.OptionGroup{ID: id, Options: optionsFromReferences(refs, defaultTone)})
}

func optionsFromReferences(options []ReferenceOption, defaultTone string) []domain.Option {
	out := make([]domain.Option, 0, len(options))
	for _, ref := range options {
		tone := ref.Tone
		if tone == "" {
			tone = defaultTone
		}
		out = append(out, option(ref.Key, ref.Label, ref.Title, tone))
	}
	return out
}

func prependOption(key, label, title, tone string, options []domain.Option) []domain.Option {
	return append([]domain.Option{option(key, label, title, tone)}, options...)
}

func rolesFromGrants(grants []permissions.ActiveGrant) []string {
	seen := map[string]struct{}{}
	var roles []string
	for _, grant := range grants {
		role := strings.TrimSpace(grant.Role)
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return roles
}

func canonicalGrantParts(grants []permissions.ActiveGrant) []string {
	parts := make([]string, 0, len(grants))
	for _, grant := range grants {
		parts = append(parts, grant.Role+"|"+grant.ScopeType+"|"+grant.ScopeID)
	}
	sort.Strings(parts)
	return parts
}

func grantsAuthorize(grants []permissions.ActiveGrant, tenantID string, required []string) bool {
	return permissions.RolesAuthorize(tenantRoles(grants, tenantID), required, false)
}

func tenantRoles(grants []permissions.ActiveGrant, tenantID string) []string {
	var roles []string
	for _, grant := range grants {
		if grant.ScopeType == "tenant" && grant.ScopeID == tenantID {
			roles = append(roles, grant.Role)
		}
	}
	return roles
}

func hasAnyRole(roles []string, targets ...string) bool {
	set := map[string]struct{}{}
	for _, role := range roles {
		set[role] = struct{}{}
	}
	for _, target := range targets {
		if _, ok := set[target]; ok {
			return true
		}
	}
	return false
}

func highestRole(roles []string) string {
	for _, role := range []string{permissions.RoleCEOInternal, permissions.RolePCDirector, permissions.RoleParkHead, permissions.RoleVerifier, permissions.RoleOperator} {
		for _, got := range roles {
			if got == role {
				return role
			}
		}
	}
	if len(roles) == 0 {
		return permissions.RoleCEOInternal
	}
	return roles[0]
}

func roleLensForRole(role string) domain.RoleLensContract {
	switch role {
	case permissions.RoleCEOInternal:
		return domain.RoleLensContract{ID: "coo", Name: "CEO / CXO", AuditShort: "CXO", Scope: "all · deep", Description: "Central Command · all parks", FullAccess: true}
	case permissions.RolePCDirector:
		return domain.RoleLensContract{ID: "health-director", Name: "Health Director", AuditShort: "Health Dir", Scope: "health vertical · all parks", Description: "PC / health governance view"}
	case permissions.RoleParkHead:
		return domain.RoleLensContract{ID: "park-head", Name: "Park Head", AuditShort: "Park Head", Scope: "all verticals · assigned park", Description: "Assigned park leadership view"}
	case permissions.RoleVerifier:
		return domain.RoleLensContract{ID: "health-manager", Name: "Health Manager", AuditShort: "Health Mgr", Scope: "health vertical · assigned park", Description: "Assigned-park PC manager view"}
	case permissions.RoleOperator:
		return domain.RoleLensContract{ID: "ground", Name: "Assist / Ground", AuditShort: "Assist", Scope: "tasks · assigned park", Description: "field execution queue"}
	default:
		return domain.RoleLensContract{ID: role, Name: role, AuditShort: role, Scope: "assigned scope", Description: "Backend RBAC role"}
	}
}

func roleLensName(role string) string {
	return roleLensForRole(role).Name
}

func roleInitials(role string) string {
	switch role {
	case permissions.RoleCEOInternal:
		return "CX"
	case permissions.RolePCDirector:
		return "HD"
	case permissions.RoleParkHead:
		return "PH"
	case permissions.RoleVerifier:
		return "HV"
	case permissions.RoleOperator:
		return "OP"
	default:
		return "RB"
	}
}

func scopeSummary(grants []permissions.ActiveGrant, parkCount int) string {
	if len(grants) == 0 {
		return "Role and park scope resolved by backend RBAC"
	}
	for _, grant := range grants {
		if grant.ScopeType == "tenant" {
			if parkCount > 0 {
				label := "active parks"
				if parkCount == 1 {
					label = "active park"
				}
				return fmt.Sprintf("tenant scope · %d %s", parkCount, label)
			}
			return "tenant scope"
		}
	}
	return "assigned scoped access"
}

func permissionsForNav(id string) []string {
	switch id {
	case "control-tower", "action-center", "protocol-adherence", "workflows", "preventive-care-vaccination":
		return []string{permissions.ObligationRead, permissions.VaccinationRead}
	case "preventive-care-weighing":
		return []string{permissions.WeighingMonitor}
	case "calendar":
		return []string{permissions.CalendarRead, permissions.VaccinationRead, permissions.ObligationRead}
	case "procurement-source-entry":
		return []string{permissions.ProcurementRead}
	case "counts-herd", "counts-breakdown":
		return []string{permissions.GoatRead}
	case "audit-log":
		return []string{permissions.OperatorsViewAudit}
	case "dlq-center":
		return []string{permissions.OperatorsViewAudit}
	case "config":
		return []string{permissions.ProtocolRead}
	case "sop-library":
		return []string{permissions.SOPRead}
	case "approvals":
		// Coarse surface gate for the Approvals page (maintainer decision 2026-07-21). Held by the
		// four org tiers + admin + ceo_internal; park_head no longer holds it, so its nav item is
		// disabled. Matches the /admin-web/counts/approvals route gate.
		return []string{permissions.CountsApproveAccess}
	default:
		return nil
	}
}

func familyHashes(resp domain.BootstrapResponse, families ReferenceFamilies, input BootstrapInput, familyErr error) map[string]string {
	hashes := map[string]string{
		"chrome": hashStruct(struct {
			Nav domain.NavigationContract
			Top domain.TopBarContract
		}{resp.Navigation, resp.TopBar}),
		"pages":       hashStruct(resp.Pages),
		"permissions": hashStruct(canonicalGrantParts(input.Grants)),
		"locations":   hashStruct(families.Parks),
		"config":      hashStruct(struct{ Breeds, Health, Repro, Defer, SOP []ReferenceOption }{families.Breeds, families.HealthStatuses, families.ReproductiveStates, families.DeferStates, families.SOPLabels}),
		"ui-config":   hashStruct(families.UIConfig),
	}
	for key, value := range families.RevisionInputs {
		hashes["db:"+key] = hashString(value)
	}
	if familyErr != nil {
		hashes["family_load_error"] = hashString(familyErr.Error())
	}
	return hashes
}

func hashStruct(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return hashString(fmt.Sprintf("%#v", v))
	}
	return hashString(string(b))
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}
