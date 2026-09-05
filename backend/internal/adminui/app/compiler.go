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
	// The person's own page ticks are resolved BEFORE the cache is consulted, and their
	// fingerprint is part of the key.
	//
	// This is not an optimisation -- it is what makes the screen usable. The key is built
	// from tenant, actor, roles and grants, none of which move when an admin edits access,
	// so a saved change would have sat behind the 60-second TTL: the admin ticks a box,
	// tells the person to reload, and nothing happens for a minute. That never mattered
	// while a role change was a deploy; it matters now that access is an edit.
	//
	// The resolved value is carried into compile() rather than read again, so this costs
	// ONE small indexed read per bootstrap, not two.
	access, assigned, accessErr := s.personPageAccessFor(ctx, input)
	if procurementDirectorStockOnly(input) {
		access = permissions.PageAccess{
			Pages: map[string]struct{}{
				"sales-config":               {},
				"feed-analytics":             {},
				"procurement-vendors":        {},
				"procurement-feed-purchases": {},
			},
			Modules: map[string]struct{}{
				"sales":          {},
				"feed_direction": {},
				"vendors":        {},
				"feed_purchases": {},
			},
		}
		assigned = true
		accessErr = nil
	}
	fingerprint := pageAccessFingerprint(access, assigned)
	revisionKey := ""
	if revisions, ok := s.loadFamilyRevisions(ctx, input.TenantID); ok {
		revisionKey = s.cacheKey(input, ReferenceFamilies{RevisionInputs: revisions}, nil) + "::" + fingerprint
		if cached, ok := s.cached(revisionKey, now); ok {
			return cached
		}
	}
	families, familyErr := s.loadFamilies(ctx, input.TenantID)
	key := s.cacheKey(input, families, familyErr) + "::" + fingerprint
	if cached, ok := s.cached(key, now); ok {
		return cached
	}
	resp := s.compile(ctx, input, families, familyErr, access, assigned, accessErr)
	expiresAt := now.Add(s.cacheTTL)
	s.storeCache(key, resp, now, expiresAt)
	if revisionKey != "" && familyErr == nil {
		s.storeCache(revisionKey, resp, now, expiresAt)
	}
	return resp
}

func procurementDirectorStockOnly(input BootstrapInput) bool {
	roles := rolesFromGrants(input.Grants)
	if hasAnyRole(roles, permissions.RoleCEOInternal) {
		return false
	}
	return hasAnyRole(roles, permissions.RoleProcurementDirector)
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
		// The ACTOR is part of the key: two people carrying identical roles can now be
		// ticked for different pages, so a role-keyed cache would serve one of them the
		// other's sidebar (per-person access, maintainer decision 2026-08-27).
		strings.TrimSpace(input.ActorID),
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

// pageAccessFingerprint identifies one principal's resolved page ticks for the cache key.
//
// An UNASSIGNED principal (no stored rows) gets a distinct constant rather than the empty
// set's hash: "not narrowed at all" and "narrowed to nothing" compile to different
// contracts, and sharing a key between them would serve one of them the other's sidebar.
func pageAccessFingerprint(access permissions.PageAccess, assigned bool) string {
	if !assigned {
		return "pages:none"
	}
	parts := make([]string, 0, len(access.Pages)+len(access.Modules))
	for key := range access.Pages {
		parts = append(parts, "p:"+key)
	}
	for key := range access.Modules {
		parts = append(parts, "m:"+key)
	}
	// Sorted: Go's map iteration order would give the same access set a different
	// fingerprint on every request, which is a cache that never hits.
	sort.Strings(parts)
	return "pages:" + hashString(strings.Join(parts, ","))
}

func (s *Service) compile(
	ctx context.Context,
	input BootstrapInput,
	families ReferenceFamilies,
	familyErr error,
	access permissions.PageAccess,
	pageAccessAssigned bool,
	pageAccessErr error,
) domain.BootstrapResponse {
	families.UIConfig = applicableConfigEntries(families.UIConfig)
	resp := baseBootstrap()
	resp = compileRequestContext(resp, input, families)
	resp = applyConfigEntries(resp, families.UIConfig)
	// The verifier-only workspace narrows the fully-compiled contract instead of building a
	// parallel one, so /verify keeps the same controls/copy/options every other principal gets.
	// It runs before familyHashes so the contract revision reflects what is actually served.
	if isVerifierLensPrincipal(input) {
		resp = applyVerifierLens(resp, s.verifierNavModules(ctx, input))
	} else if pageAccessAssigned {
		// Per-person page narrowing (maintainer decision 2026-08-27). This REPLACES the
		// hand-coded procurement-director lens: "only Procurement and Feed, and not Feed
		// Config" is now that person's ticks on /people rather than a Go file.
		resp = applyPersonPageLens(resp, access)
	} else if pageAccessErr != nil {
		// The read failed. The contract is served UNNARROWED -- a person must not be locked
		// out of a product they are authorized for by a database blip -- and it SAYS so.
		resp.DisplayRules = append(resp.DisplayRules, personPageAccessUnavailableRule(pageAccessErr))
	}
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
		// best-effort: load revision map for contract family matching; if unavailable, fall back to no revisions
		// exception:exempt graceful fallback; the contract family matching is optional and doesn't block bootstrap
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
		case "config", "vaccination-plan":
			out[i].OptionGroups = compileConfigOptionGroups(out[i].OptionGroups, families)
			out[i].Controls = compileConfigControls(out[i].Controls, input, out[i].Copy)
		case "herd-register":
			// Source-backed reproductive vocabulary for the Herd Register reproductive edit drawer.
			// Same status_definitions family the Config rule editor uses, minus the "any" sentinel.
			out[i].OptionGroups = replaceOptionGroup(out[i].OptionGroups, "herd_reproductive", optionsFromReferences(families.ReproductiveStates, ""))
		case "feed-direction", "feed-packing", "feed-config", "feed-analytics":
			// Live feed vocabulary. feedOptionGroups() declares only fixed schema constraints;
			// the actual feed items are tenant data from feed_item_catalog and arrive here as
			// ReferenceFamilies.FeedItems. This is the intended injection path — the alternative
			// (a constant list of item labels in contract code) is the banned pattern.
			out[i].OptionGroups = mergeOptionGroupReferences(out[i].OptionGroups, "feed_items", families.FeedItems, "")
			out[i].OptionGroups = replaceOptionGroup(out[i].OptionGroups, "feed_parks", optionsFromReferences(families.Parks, "info"))
			out[i].OptionGroups = replaceOptionGroup(out[i].OptionGroups, "feed_breeds", optionsFromReferences(families.Breeds, ""))
			if out[i].RouteID == "feed-analytics" {
				out[i].OptionGroups = compileFeedAnalyticsOptionGroups(out[i].OptionGroups, input)
			}
		case "weighing-weights", "weighing-analytics":
			// Live park vocabulary, same injection path Feed uses. The contract declares
			// the group empty; the parks themselves are tenant rows and must never be
			// constants in contract code.
			out[i].OptionGroups = replaceOptionGroup(out[i].OptionGroups, "weighing_parks", optionsFromReferences(families.Parks, "info"))
			if out[i].RouteID == "weighing-analytics" {
				// The Comparison tab's value chart, gated on SalesRead (see compileWeightsAnalyticsControls).
				out[i].Controls = compileWeightsAnalyticsControls(out[i].Controls, input, out[i].Copy)
			}
		case "dlq-center":
			out[i].OptionGroups = compileDLQOptionGroups(out[i].OptionGroups, input)
		case "verification-review":
			out[i].Controls = compileVerificationReviewControls(out[i].Controls, input, out[i].Copy)
		case "health-config":
			out[i].Controls = compileHealthConfigControls(out[i].Controls, input, out[i].Copy)
		case "sales":
			// READ card, not a write: /sales stays read-only by contract (below). The Over 35 kg
			// card reads weighing, which is a different desk's permission, so it is declared here
			// and gated the same way a write would be -- see compileSalesWeightCards.
			out[i].Controls = compileSalesWeightCards(out[i].Controls, input, out[i].Copy)
		case "sales-loads":
			// READ control for the "weighs now" series on the load chart -- weighing's
			// permission, gated exactly like the Sales board's Over 35 kg card.
			out[i].Controls = compileLoadsWeightSeries(out[i].Controls, input, out[i].Copy)
		case "sales-config":
			// Sales Config is the ONLY sales write surface (maintainer decision 2026-09-01).
			// /sales and /sales/loads are deliberately absent from this switch: a page that
			// declares no write control renders none, which is what makes them read-only.
			out[i].OptionGroups = compileSalesConfigOptionGroups(out[i].OptionGroups, input)
			out[i].Controls = compileSalesConfigControls(out[i].Controls, input, out[i].Copy)
		case "feed-purchases":
			out[i].Controls = compileFeedPurchaseControls(out[i].Controls, input, out[i].Copy)
		case "people":
			out[i].Controls = compilePeopleControls(out[i].Controls, input, out[i].Copy)
		case "counts-breakdown":
			out[i].Controls = compileCountsBreakdownControls(out[i].Controls, input, out[i].Copy)
			// The breed catalog for the inline breed correction, injected the same way Feed's
			// vocabularies are. Contract code declares the group; the values are tenant rows.
			out[i].OptionGroups = replaceOptionGroup(out[i].OptionGroups, "counts_breed", optionsFromReferences(families.Breeds, ""))
		}
	}
	return out
}

// compileHealthConfigControls splits /health/config by authority: HealthConfigRead reaches the
// screen and reads the standing dosages; only HealthConfigWrite may change them.
//
// The four controls are declared here rather than left to the renderer so a read-only principal
// gets a visibly DISABLED control carrying a reason, not a missing one. A missing button reads as
// a broken page; a disabled button with "your role can read the protocols but cannot change them"
// is an answer. This is the same shape compileConfigControls uses for protocol publish.
func compileHealthConfigControls(controls []domain.Control, input BootstrapInput, copy map[string]string) []domain.Control {
	// An unauthenticated/grantless compile (contract shape requests, fixtures) keeps every control
	// enabled, matching how compileConfigControls treats the same case.
	allowed := len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.HealthConfigWrite})
	reason := ""
	if !allowed {
		reason = controlCopy(copy, "health_config.disabled_no_write", "Your current role can read the treatment protocols but cannot change them.")
	}
	for _, c := range []domain.Control{
		{
			ID:     "add_disease",
			Label:  controlCopy(copy, "action.add_disease", "Add disease"),
			Kind:   "primary_action",
			Action: "POST /health-config/diseases",
		},
		{
			ID:     "edit_protocol",
			Label:  controlCopy(copy, "action.edit_protocol", "Edit"),
			Kind:   "row_action",
			Action: "POST /health-config/drafts",
		},
		{
			ID:     "publish_protocol",
			Label:  controlCopy(copy, "action.publish_protocol", "Publish"),
			Kind:   "primary_action",
			Action: "POST /health-config/protocols/{protocol_version_id}/publish",
		},
		{
			ID:     "discard_draft",
			Label:  controlCopy(copy, "action.discard_draft", "Discard draft"),
			Kind:   "secondary_action",
			Action: "POST /health-config/protocols/{protocol_version_id}/discard",
		},
	} {
		c.Enabled = allowed
		c.DisabledReason = reason
		controls = upsertControl(controls, c)
	}
	return controls
}

// compileSalesConfigControls declares every sales WRITE, all of it on /sales/config (maintainer
// decision 2026-09-01). /sales and /sales/loads read the same facts and declare no write of their
// own, so an entry form exists in exactly one place and cannot drift between two.
//
// Splitting by authority is unchanged by the move: SalesRead reaches the page, only SalesWrite may
// record a sale or a receipt, and only LoadCostWrite may cost a load. One page, two permissions.
//
// The control is declared for every principal who reaches the page and DISABLED with a reason for
// those who may not use it, rather than omitted -- a missing button reads as a broken page, a
// disabled one carrying "your role can view sales but not record them" is an answer. Same shape as
// compileHealthConfigControls. The route behind it requires the same permission, so a principal
// who defeats the disabled state still gets 403; the control is the honest label, not the lock.
// compileSalesWeightCards declares the Sales board's "Over 35 kg" card (maintainer request
// 2026-09-03): how many kids weighed in the last six weeks stand at or above the sale weight.
// Sales has no time filter, so the window is fixed and named on the card.
//
// It is a READ control with no Action, so TestSalesReadPagesCarryNoWriteControl is untouched and
// /sales stays read-only. It is gated all the same, because the figure comes from
// /weighing/shed-weights, which needs WeighingMonitor -- a permission the sales desk does not
// hold. Role-scoped UI is capability-gated (2026-08-12): the page renders the card only when
// this control is enabled and shows the backend's reason otherwise, and the endpoint enforces
// the same permission, so a principal who defeats the disabled state still gets 403.
func compileSalesWeightCards(controls []domain.Control, input BootstrapInput, copy map[string]string) []domain.Control {
	allowed := len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.WeighingMonitor})
	reason := ""
	if !allowed {
		reason = controlCopy(copy, "disabled.weights", "Your current role can view sales but not weighing.")
	}
	return upsertControl(controls, domain.Control{
		ID:             "weights_over_35_card",
		Label:          controlCopy(copy, "kpi.over35", "Over 35 kg"),
		Kind:           "summary_card",
		Enabled:        allowed,
		DisabledReason: reason,
	})
}

// compileWeightsAnalyticsControls declares the Comparison tab's VALUE chart (purchased value
// against current stock value). It is priced from the SalesRead-only loadwise read, so it is a
// read control gated on SalesRead: the Growth Director, who reaches the tab on WeighingMonitor
// alone, sees the backend's reason in its place rather than money the endpoint would refuse.
// Declares no Action; the analytics page stays read-only by contract.
func compileWeightsAnalyticsControls(controls []domain.Control, input BootstrapInput, copy map[string]string) []domain.Control {
	allowed := len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.SalesRead})
	reason := ""
	if !allowed {
		reason = controlCopy(copy, "disabled.load_value", "Your current role can view weights but not purchase and sales money.")
	}
	return upsertControl(controls, domain.Control{
		ID:             "load_value_chart",
		Label:          controlCopy(copy, "section.load_value.title", "Purchased value against current stock value"),
		Kind:           "chart",
		Enabled:        allowed,
		DisabledReason: reason,
	})
}

// compileLoadsWeightSeries declares the Purchase and Born chart's third weight series
// (maintainer request 2026-09-03): for a load that has sold nothing, the average its animals
// weigh NOW, from the latest weighing of the pens the load was placed into. It reads
// /weighing/shed-weights, so it is gated on WeighingMonitor the same way as the Sales board's
// Over 35 kg card, and declares no Action so /sales/loads stays read-only by contract.
func compileLoadsWeightSeries(controls []domain.Control, input BootstrapInput, copy map[string]string) []domain.Control {
	allowed := len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.WeighingMonitor})
	reason := ""
	if !allowed {
		reason = controlCopy(copy, "disabled.weights", "Your current role can view loads but not weighing.")
	}
	return upsertControl(controls, domain.Control{
		ID:             "weights_current_average_series",
		Label:          controlCopy(copy, "chart.series.current_avg_weight", "Weighs now"),
		Kind:           "chart_series",
		Enabled:        allowed,
		DisabledReason: reason,
	})
}

func compileSalesConfigOptionGroups(groups []domain.OptionGroup, input BootstrapInput) []domain.OptionGroup {
	return replaceOptionGroup(groups, "sales_config_read_links", []domain.Option{
		option("sales-board", "See the sales board", "", ""),
		option("sales-loads", "See Purchase and Born", "", ""),
	})
}

func compileSalesConfigControls(controls []domain.Control, input BootstrapInput, copy map[string]string) []domain.Control {
	// An unauthenticated/grantless compile (contract shape requests, fixtures) keeps the control
	// enabled, matching compileConfigControls and compileHealthConfigControls.
	allowed := len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.SalesWrite})
	reason := ""
	if !allowed {
		reason = controlCopy(copy, "disabled.write", "Your current role can view sales but not record them.")
	}
	controls = upsertControl(controls, domain.Control{
		ID:             "record_sale",
		Label:          controlCopy(copy, "action.record_sale.label", "Record sale"),
		Kind:           "primary_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "POST /sales/deals",
	})
	// One capability gate for the pipeline/evidence writes (leads, farmer groups, market quotes,
	// tag lists, weight checks): they all ride SalesWrite, and the sheet they replaced is retired
	// (maintainer decision 2026-08-18), so entry lives here or nowhere.
	pipelineAllowed := allowed && !procurementDirectorStockOnly(input)
	pipelineReason := reason
	if allowed && !pipelineAllowed {
		pipelineReason = controlCopy(copy, "disabled.pipeline", "Pipeline and evidence entry is not enabled for your current role.")
	}
	controls = upsertControl(controls, domain.Control{
		ID:             "record_pipeline",
		Label:          controlCopy(copy, "action.record_pipeline.label", "Add record"),
		Kind:           "secondary_action",
		Enabled:        pipelineAllowed,
		DisabledReason: pipelineReason,
		Action:         "POST /sales/buyer-leads",
	})
	allocateAllowed := len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.SalesAllocateAnimals})
	allocateReason := ""
	if !allocateAllowed {
		allocateReason = controlCopy(copy, "disabled.allocate_animals", "Tagging animals to a sale needs herd allocation access.")
	}
	controls = upsertControl(controls, domain.Control{
		ID:             "allocate_sale_animals",
		Label:          controlCopy(copy, "action.tag_animals.label", "Tag animals to sale"),
		Kind:           "secondary_action",
		Enabled:        allocateAllowed,
		DisabledReason: allocateReason,
		Action:         "POST /admin/goats/sale-allocations/confirm",
	})
	// A buyer receipt is a money write on the same ledger, so it rides the same permission as
	// recording the deal. Declared-and-disabled for read-only principals, like every write here.
	controls = upsertControl(controls, domain.Control{
		ID:             "record_sales_deal_payment",
		Label:          controlCopy(copy, "action.record_deal_payment.label", "Add payment"),
		Kind:           "row_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "POST /sales/deals/{deal_id}/payments",
	})
	controls = upsertControl(controls, domain.Control{
		ID:             "update_sales_deal_payment",
		Label:          controlCopy(copy, "action.update_deal_payment.label", "Save payment"),
		Kind:           "row_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "PUT /sales/deals/{deal_id}/payments/{payment_id}",
	})
	controls = upsertControl(controls, domain.Control{
		ID:             "delete_sales_deal_payment",
		Label:          controlCopy(copy, "action.delete_deal_payment.label", "Remove payment"),
		Kind:           "row_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "DELETE /sales/deals/{deal_id}/payments/{payment_id}",
	})
	// The lifecycle edit that closes an expected sale on the day it happens. Same authority as
	// recording the deal.
	controls = upsertControl(controls, domain.Control{
		ID:             "update_sales_deal_status",
		Label:          controlCopy(copy, "action.update_deal_status.label", "Update status"),
		Kind:           "row_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "POST /sales/deals/{deal_id}/status",
	})

	// The load-cost write moved here with every other sales entry, but it keeps its OWN
	// permission. Recording a LOAD's landed cost is buying-desk money, not sales recording, so it
	// carries LoadCostWrite (the FeedPurchaseWrite precedent) rather than riding SalesWrite -- a
	// sales recorder who is not the buying desk sees this one control disabled with its reason
	// while the rest of the page stays live, per the role-scoped-UI-is-capability-gated lock.
	// Sharing the page must never mean sharing the authority.
	costAllowed := len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.LoadCostWrite})
	costReason := ""
	if !costAllowed {
		costReason = controlCopy(copy, "disabled.load_cost", "Recording a load's cost needs the buying desk's access.")
	}
	return upsertControl(controls, domain.Control{
		ID:             "record_load_cost",
		Label:          controlCopy(copy, "action.record_load_cost.label", "Record cost"),
		Kind:           "row_action",
		Enabled:        costAllowed,
		DisabledReason: costReason,
		Action:         "PUT /procurement/loads/{load_id}/cost",
	})
}

// compileFeedPurchaseControls splits /procurement/feed-purchases by authority: FeedPurchaseRead
// reaches the ledger; only FeedPurchaseWrite may record a purchased load.
//
// Same shape as compileSalesControls, and for the same reason: the control is DECLARED for every
// principal who reaches the page and disabled with a reason for those who may not use it, because
// a missing button reads as a broken page while a disabled one carrying "your role can view feed
// purchases but not record them" is an answer. The route behind it requires the same permission,
// so a principal who defeats the disabled state still gets 403 -- the control is the honest label,
// not the lock.
//
// This is the capability half of the 2026-08-24 decision that retired migration 000174's
// read-only lock. There is deliberately NO role-string conditional in the page component: the
// difference between a Feed Director (read) and the procurement desk (write) arrives ONLY through
// this control and the route's permission, per the role-scoped-UI-is-capability-gated lock.
func compileFeedPurchaseControls(controls []domain.Control, input BootstrapInput, copy map[string]string) []domain.Control {
	// An unauthenticated/grantless compile (contract shape requests, fixtures) keeps the control
	// enabled, matching compileSalesControls and compileHealthConfigControls.
	allowed := len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.FeedPurchaseWrite})
	reason := ""
	if !allowed {
		reason = controlCopy(copy, "disabled.write", "Your current role can view feed purchases but not record them.")
	}
	controls = upsertControl(controls, domain.Control{
		ID:             "record_feed_purchase",
		Label:          controlCopy(copy, "action.record_feed_purchase.label", "Record purchase"),
		Kind:           "primary_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "POST /procurement/feed-purchases",
	})
	// The payment writes share the same permission and therefore the same disabled reason: a
	// principal who can record the load can record the money against it, and a read-only tier can
	// do neither. Two controls rather than one because they are two different writes -- the drawer
	// shows/hides each on its own control, never on a role string.
	controls = upsertControl(controls, domain.Control{
		ID:             "record_feed_purchase_payment",
		Label:          controlCopy(copy, "action.record_feed_payment.label", "Add payment"),
		Kind:           "row_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "POST /procurement/feed-purchases/{purchase_id}/payments",
	})
	controls = upsertControl(controls, domain.Control{
		ID:             "update_feed_purchase_payment_status",
		Label:          controlCopy(copy, "action.update_payment_status.label", "Update status"),
		Kind:           "row_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "PUT /procurement/feed-purchases/{purchase_id}/payment-status",
	})
	// Editing a recorded load's values is the same authority as recording it: whoever buys feed
	// may correct a wrongly-typed quantity or cost. Identity (farm/feed/batch) stays immutable at
	// the contract's own route.
	controls = upsertControl(controls, domain.Control{
		ID:             "edit_feed_purchase",
		Label:          controlCopy(copy, "action.edit_feed_purchase.label", "Edit purchase"),
		Kind:           "row_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "PUT /procurement/feed-purchases/{purchase_id}",
	})
	// Marking a load reached (maintainer decision 2026-09-03) is the desk that bought it saying
	// the truck came in: same permission, same disabled reason. It is the write that turns a
	// purchase into stock and raises its toxin test, so it is its own control rather than a
	// field on the edit form.
	return upsertControl(controls, domain.Control{
		ID:             "record_feed_purchase_delivery",
		Label:          controlCopy(copy, "action.mark_reached.label", "Mark reached"),
		Kind:           "row_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "PUT /procurement/feed-purchases/{purchase_id}/delivery",
	})
}

// compilePeopleControls gates the per-person ACCESS editor (maintainer decision 2026-08-24).
//
// The authority is OperatorsManageCapability, deliberately NOT the OperatorsWrite that creates a
// person: adding a colleague and deciding what every colleague may do are different jobs, and this
// one can grant every other permission in the catalog -- including itself.
//
// Declared-and-disabled rather than omitted, the same shape as sales and health config: a missing
// button reads as a broken page, and a disabled one carrying "your role can view access but not
// change it" is an answer. The PUT route behind it requires the same permission, so a principal
// who defeats the disabled state still gets 403 -- the control is the honest label, not the lock.
func compilePeopleControls(controls []domain.Control, input BootstrapInput, copy map[string]string) []domain.Control {
	allowed := len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.OperatorsManageCapability})
	reason := ""
	if !allowed {
		reason = controlCopy(copy, "disabled.access_write", "Your current role can view access but not change it.")
	}
	controls = upsertControl(controls, domain.Control{
		ID:             "edit_access",
		Label:          controlCopy(copy, "access.action.save", "Save access"),
		Kind:           "primary_action",
		Enabled:        allowed,
		DisabledReason: reason,
		Action:         "PUT /admin/workforce/people/{person_id}/access",
	})
	// view_clock gates the Clock In / Out tab (maintainer decision 2026-08-28:
	// clock.presence.read, leadership only). The tab option itself stays in the
	// static group; the renderer enables it only when this control is enabled,
	// and the backend routes refuse regardless — the control is the honest
	// label, not the lock.
	clockAllowed := len(input.Grants) == 0 || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.ClockPresenceRead})
	clockReason := ""
	if !clockAllowed {
		clockReason = controlCopy(copy, "clock.tab.locked", "Clock oversight is limited to leadership.")
	}
	return upsertControl(controls, domain.Control{
		ID:             "view_clock",
		Label:          controlCopy(copy, "clock.tab.title", "Clock In / Out"),
		Kind:           "view",
		Enabled:        clockAllowed,
		DisabledReason: clockReason,
		Action:         "GET /admin/workforce/clock-entries",
	})
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

// compileVerificationReviewControls splits /verify by duty (verifier-app-and-flow.md §Roles):
// the VERIFIER records the verdict, the AUTHORITY acts on the source task. One page serves both
// personas, so the backend contract -- not the renderer -- decides which half each principal gets.
//
// Boundary worth knowing: the rework/reassign controls are gated on verification.act because that
// is the authority permission the doctrine names, but the routes behind them
// (POST /admin/tasks/{task_id}/rework, /assign) require task.verify and task.assign. A verifier
// holds task.verify, so hiding rework from her lens is a duty split at the contract layer, not a
// hard backend lockout on that generic SOP route. Narrowing reworkTask itself would change every
// other caller of a shared route and belongs in its own change.
// compileCountsBreakdownControls declares the whole-pen stage-change action.
//
// The control is DECLARED for every principal who reaches the page and disabled with a reason for
// those who may not use it, rather than omitted. A missing button reads as "this screen cannot do
// that"; a disabled one carrying "Only the CEO can change a whole pen's stage" tells a park head
// the truth, which is that the capability exists and is not theirs.
//
// Enablement follows permissions.GoatReclassifyShedStage -- held by ceo_internal alone -- and NOT
// CountsWrite, which park_head and operator also hold. The routes require the same permission, so
// a principal who defeats the disabled state still gets 403; the control is the honest label, not
// the lock.
func compileCountsBreakdownControls(controls []domain.Control, input BootstrapInput, copy map[string]string) []domain.Control {
	// An unauthenticated/grantless compile (contract shape requests, fixtures) keeps the control
	// enabled, matching compileVerificationReviewControls and compileConfigControls.
	ungated := len(input.Grants) == 0
	mayChange := ungated || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.GoatReclassifyShedStage})

	reason := ""
	if !mayChange {
		reason = controlCopy(copy, "stage_change.disabled_no_access", "Only the CEO can change a whole pen's stage.")
	}
	return upsertControl(controls, domain.Control{
		ID:             "change_shed_stage",
		Label:          controlCopy(copy, "stage_change.title", "Change stage"),
		Kind:           "primary_action",
		Enabled:        mayChange,
		DisabledReason: reason,
		Action:         "POST /admin/goats/shed-stage/commit",
	})
}

func compileVerificationReviewControls(controls []domain.Control, input BootstrapInput, copy map[string]string) []domain.Control {
	// An unauthenticated/grantless compile (contract shape requests, fixtures) keeps every control
	// enabled, matching how compileConfigControls treats the same case.
	ungated := len(input.Grants) == 0
	// The VERDICT control follows verification.verdict, not verification.review: leadership reads
	// the same queue but may not decide on it (maintainer decision 2026-08-03).
	mayDecide := ungated || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.VerificationVerdict})
	mayAct := ungated || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.VerificationAct})

	reviewReason := ""
	if !mayDecide {
		reviewReason = controlCopy(copy, "verdict.disabled_no_access", "Recording a verdict is limited to the video verification team.")
	}
	actReason := ""
	if !mayAct {
		actReason = controlCopy(copy, "action.disabled_no_authority", "Acting on the source task is limited to the park head, director, or CEO.")
	}

	// The module chip row is OFFERED by default and withdrawn only for the verifier lens, whose
	// sidebar already carries one leaf per evidence module (applyVerifierLens). Declaring it here
	// keeps leadership -- who reach /verify from a single primary nav item and have no module
	// leaves -- on the row they need to pick a module at all.
	controls = upsertControl(controls, domain.Control{
		ID:      "module_filter",
		Label:   copy["filter.module"],
		Kind:    "filter",
		Enabled: true,
	})

	out := upsertControl(controls, domain.Control{
		ID:             "record_verdict",
		Label:          copy["verdict.title"],
		Kind:           "primary_action",
		Enabled:        mayDecide,
		DisabledReason: reviewReason,
		Action:         "POST /verification/items/{item_id}/verdict",
	})
	out = upsertControl(out, domain.Control{
		ID:             "request_rework",
		Label:          copy["rework.submit"],
		Kind:           "action",
		Enabled:        mayAct,
		DisabledReason: actReason,
		Action:         "POST /admin/tasks/{task_id}/rework",
	})
	out = upsertControl(out, domain.Control{
		ID:             "reassign_task",
		Label:          copy["reassign.submit"],
		Kind:           "action",
		Enabled:        mayAct,
		DisabledReason: actReason,
		Action:         "POST /admin/tasks/{task_id}/assign",
	})
	// oversight_filters gates the CROSS-MODULE oversight chrome on /verify (module chips, the
	// capture-date range picker): see permissions.VerificationOversee. Incident (2026-08-12, STG):
	// these filters were built for the CEO's oversight view but rendered for every role that can
	// open /verify, including RoleVerifier, because the page is a single role-agnostic component.
	// The renderer must gate on THIS control -- not on the caller's role, and not by inferring
	// oversight from grant shape -- so the verifier's working queue (status chips, shed filter;
	// both predate the oversight rollout) is unaffected. See
	// docs/decisions/role-scoped-ui-is-capability-gated.md.
	mayOversee := ungated || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.VerificationOversee})
	oversightReason := ""
	if !mayOversee {
		oversightReason = controlCopy(copy, "oversight_filters.disabled_no_access", "Cross-module filters are limited to leadership oversight of verification.")
	}
	out = upsertControl(out, domain.Control{
		ID:             "oversight_filters",
		Label:          copy["filter.module"],
		Kind:           "visibility",
		Enabled:        mayOversee,
		DisabledReason: oversightReason,
		Action:         "",
	})
	// capture_date_filter gates the CAPTURE-DATE RANGE picker on /verify -- on every page of the
	// verifier's workspace, since they are one component under different categories (maintainer
	// decision 2026-08-17).
	//
	// SPLIT OUT of oversight_filters deliberately. The 2026-08-12 incident was CROSS-MODULE chrome
	// leaking to every role, and the module chips stay leadership-only for exactly that reason. A
	// date range crosses no module boundary: it narrows the caller's own queue to the days she is
	// working. Without it the verifier's board is pinned to one date she cannot change, which on
	// real data means an empty screen sitting on top of a full backlog.
	mayFilterByCaptureDate := ungated || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.VerificationFilterByCaptureDate})
	captureDateReason := ""
	if !mayFilterByCaptureDate {
		captureDateReason = controlCopy(copy, "capture_date_filter.disabled_no_access", "Filtering by capture date is limited to the video verification team and leadership.")
	}
	out = upsertControl(out, domain.Control{
		ID:             "capture_date_filter",
		Label:          controlCopy(copy, "capture_date_filter.label", "Capture date"),
		Kind:           "visibility",
		Enabled:        mayFilterByCaptureDate,
		DisabledReason: captureDateReason,
		Action:         "",
	})
	// oversight_analytics gates the CEO/Director analytics section ABOVE the queue table on
	// /verify: waiting count, per-module pending, per-verifier last-14d, watch-integrity. Same
	// capability as oversight_filters (permissions.VerificationOversee) -- it is a second, distinct
	// control rather than the renderer reusing oversight_filters for two different pieces of
	// chrome, so a future change to one visibility rule cannot silently move the other.
	out = upsertControl(out, domain.Control{
		ID:             "oversight_analytics",
		Label:          controlCopy(copy, "oversight_analytics.title", "Verification oversight"),
		Kind:           "visibility",
		Enabled:        mayOversee,
		DisabledReason: oversightReason,
		Action:         "GET /verification/oversight-analytics",
	})
	// video_log gates the VIDEO LOG panel on /verify: one business day, per shed, the time each
	// proof was uploaded (maintainer decision 2026-08-14).
	//
	// It follows permissions.VerificationEvidenceTimeline, NOT VerificationOversee, and that is the
	// entire point of it being a separate control: the VERIFIER holds this capability and does not
	// hold oversight, so she gets the Video Log button and still gets no module chips, no
	// capture-date range picker and no analytics drawer. Reusing oversight_analytics here would
	// have handed her all three, which is the 2026-08-12 STG incident again.
	mayReadTimeline := ungated || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.VerificationEvidenceTimeline})
	timelineReason := ""
	if !mayReadTimeline {
		timelineReason = controlCopy(copy, "video_log.disabled_no_access", "The video log is limited to the verification team and leadership.")
	}
	out = upsertControl(out, domain.Control{
		ID:             "video_log",
		Label:          controlCopy(copy, "video_log.open", "Video Log"),
		Kind:           "visibility",
		Enabled:        mayReadTimeline,
		DisabledReason: timelineReason,
		Action:         "GET /verification/video-log",
	})
	// The TOXIN review tab (maintainer decision 2026-08-25). Both controls follow
	// permissions.ToxinVerdict, which only ceo_internal holds -- toxin review is deliberately NOT
	// the generic Verification module and NOT verification.verdict, so the tenant verifier must
	// never see this tab (the verifier lens additionally never enables it: applyVerifierLens keys
	// on verification.verdict, and a verifier holds no toxin permission). Capability-gated per
	// docs/decisions/role-scoped-ui-is-capability-gated.md: this contract control is the UI half;
	// the endpoint half is the ToxinVerdict gate on listToxinReview / recordToxinVerdict in
	// permissions/routes.go.
	mayToxin := ungated || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.ToxinVerdict})
	toxinReason := ""
	if !mayToxin {
		toxinReason = controlCopy(copy, "toxin_tab.disabled_no_access", "Feed toxin tests are reviewed by the CEO's office.")
	}
	out = upsertControl(out, domain.Control{
		ID:             "toxin_tab",
		Label:          controlCopy(copy, "toxin_tab.title", "Toxin"),
		Kind:           "visibility",
		Enabled:        mayToxin,
		DisabledReason: toxinReason,
		Action:         "GET /toxin/review",
	})
	out = upsertControl(out, domain.Control{
		ID:             "toxin_verdict",
		Label:          controlCopy(copy, "toxin_verdict.title", "Record toxin verdict"),
		Kind:           "primary_action",
		Enabled:        mayToxin,
		DisabledReason: toxinVerdictReason(copy, mayToxin),
		Action:         "POST /toxin/tasks/{task_id}/verdict",
	})
	// randomization gates the CEO-only sampling section on /verify: per module, what percentage of
	// that module's proof videos the verifier actually has to watch, and today's progress against
	// that share (maintainer decision 2026-08-26).
	//
	// It follows permissions.VerificationSampling, which is narrower than every other control on
	// this page -- RolePCDirector holds oversight and does NOT hold this. Oversight WATCHES the
	// verification workload; this DECIDES how much of it a human is required to watch, and a
	// director setting that for his own department's work is the separation of duty that keeps
	// verdict authority off leadership in the first place. It is its own control for the same
	// reason oversight_analytics is not folded into oversight_filters: one visibility rule must
	// never move because another changed.
	//
	// This is the UI gate; the SAME capability gates the data on GET/PUT /verification/sampling. A
	// pixel-only gate would be the 2026-08-12 incident's inverse.
	mayRandomize := ungated || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.VerificationSampling})
	randomizationReason := ""
	if !mayRandomize {
		randomizationReason = controlCopy(copy, "randomization.disabled_no_access", "Setting how much proof is reviewed is limited to the CEO.")
	}
	return upsertControl(out, domain.Control{
		ID:             "randomization",
		Label:          controlCopy(copy, "randomization.title", "Randomization"),
		Kind:           "visibility",
		Enabled:        mayRandomize,
		DisabledReason: randomizationReason,
		Action:         "GET /verification/sampling",
	})
}

// toxinVerdictReason keeps the enabled control reason-free, matching every other control here.
func toxinVerdictReason(copy map[string]string, mayToxin bool) string {
	if mayToxin {
		return ""
	}
	return controlCopy(copy, "toxin_verdict.disabled_no_access", "Feed toxin tests are reviewed by the CEO's office.")
}

func controlCopy(copy map[string]string, key, fallback string) string {
	if value := strings.TrimSpace(copy[key]); value != "" {
		return value
	}
	return fallback
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

func compileFeedAnalyticsOptionGroups(groups []domain.OptionGroup, input BootstrapInput) []domain.OptionGroup {
	ungated := len(input.Grants) == 0
	mayReadFullFeed := !procurementDirectorStockOnly(input) &&
		(ungated || grantsAuthorize(input.Grants, input.TenantID, []string{permissions.FeedDirectionRead}))
	tabs := []domain.Option{option("items", "Stock", "", "")}
	if mayReadFullFeed {
		tabs = []domain.Option{
			option("overview", "Consumption", "", ""),
			option("items", "Stock", "", ""),
			option("peranimal", "Per Animal", "", ""),
			option("experiment", "Experiment", "", ""),
			option("execution", "Execution", "", ""),
		}
	}
	return replaceOptionGroup(groups, "feed_analytics_tabs", tabs)
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
	// Director seats rank between CEO and Park Head. feed_director / health_director were absent,
	// so a principal holding one fell through to roles[0] and was lensed by an arbitrary role.
	for _, role := range []string{
		permissions.RoleCEOInternal,
		permissions.RolePCDirector,
		permissions.RoleGrowthDirector,
		// Above feed_director so the current holder — who carries BOTH keys — is chipped as the
		// Procurement Director he was appointed as (maintainer decision 2026-08-21).
		permissions.RoleProcurementDirector,
		permissions.RoleFeedDirector,
		permissions.RoleHealthDirector,
		permissions.RoleBreedingDirector,
		permissions.RoleParkHead,
		permissions.RoleVerifier,
		permissions.RoleOperator,
	} {
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
	// pc_director is PREVENTIVE CARE, not Health. Labelling it "Health Director" conflated it
	// with health_director, which the 2026-08-01 maintainer decision makes a separate role in a
	// separate department -- a user-facing merge of exactly the two roles that must not merge.
	case permissions.RolePCDirector:
		return domain.RoleLensContract{ID: "pc-director", Name: "Preventive Care Director", AuditShort: "PC Dir", Scope: "preventive care · all parks", Description: "Vaccination governance view"}
	case permissions.RoleGrowthDirector:
		return domain.RoleLensContract{ID: "growth-director", Name: "Growth Director", AuditShort: "Growth Dir", Scope: "weighing · all parks", Description: "Weighing governance view"}
	case permissions.RoleFeedDirector:
		return domain.RoleLensContract{ID: "feed-director", Name: "Feed Director", AuditShort: "Feed Dir", Scope: "feed · all parks", Description: "Feed governance view"}
	case permissions.RoleProcurementDirector:
		return domain.RoleLensContract{ID: "procurement-director", Name: "Procurement Director", AuditShort: "Proc Dir", Scope: "procurement + feed · all parks", Description: "Procurement governance view"}
	case permissions.RoleHealthDirector:
		return domain.RoleLensContract{ID: "health-director", Name: "Health Director", AuditShort: "Health Dir", Scope: "health · all parks", Description: "Health / counts governance view"}
	case permissions.RoleBreedingDirector:
		return domain.RoleLensContract{ID: "breeding-director", Name: "Breeding Director", AuditShort: "Breeding Dir", Scope: "hoof & hair trimming · all parks", Description: "Breeding husbandry planning view"}
	case permissions.RoleParkHead:
		return domain.RoleLensContract{ID: "park-head", Name: "Park Head", AuditShort: "Park Head", Scope: "all verticals · assigned park", Description: "Assigned park leadership view"}
	case permissions.RoleVerifier:
		// The Verifier is the cross-vertical Video Verification Team (verifier-app-and-flow.md), not
		// a health manager and not park-scoped. The old "Health Manager · health vertical · assigned
		// park" label was invisible while the role had no admin-web access; it is the account chip on
		// her own workspace now, so it has to say what she actually is.
		return domain.RoleLensContract{ID: "verifier", Name: "Verifier", AuditShort: "Verifier", Scope: "video verification · all verticals", Description: "Independent proof review"}
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
	// "HD" belongs to the Health Director; the PC Director gets his own initials for the same
	// reason his lens name changed.
	case permissions.RolePCDirector:
		return "PC"
	case permissions.RoleGrowthDirector:
		return "GD"
	case permissions.RoleFeedDirector:
		return "FD"
	case permissions.RoleProcurementDirector:
		return "PD"
	case permissions.RoleHealthDirector:
		return "HD"
	case permissions.RoleBreedingDirector:
		return "BD"
	case permissions.RoleParkHead:
		return "PH"
	case permissions.RoleVerifier:
		return "VF"
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
	case "calendar":
		return []string{permissions.CalendarRead, permissions.VaccinationRead, permissions.ObligationRead}
	case "procurement-source-entry":
		return []string{permissions.ProcurementRead}
	case "procurement-vendors":
		// The dedicated register permission, NOT ProcurementRead. ProcurementRead is held by seven
		// roles including operator and park_head because it gates the source-entry/intake screens
		// they work; the register carries negotiated prices, contact numbers and banking
		// instruments. Gating the leaf on ProcurementRead would put it in every operator's sidebar.
		return []string{permissions.VendorRead}
	case "sales-board", "sales-loads", "sales-config":
		// The dedicated sales permission, NOT ProcurementRead: sales carries revenue, buyer names
		// and realized prices -- the selling side, not the intake screens operators work.
		//
		// Purchase and Born reads the same commercial facts per load, so it rides the same
		// permission. Sales Config rides it too rather than SalesWrite: a sales reader who cannot
		// record still reaches the page and sees each control DISABLED with its reason, which is
		// the health-config shape -- a missing leaf reads as a broken product, a disabled button
		// carrying "your role can view sales but not record them" is an answer. The WRITES on it
		// are separately gated (SalesWrite, and LoadCostWrite for a load's cost).
		return []string{permissions.SalesRead}
	case "procurement-feed-purchases":
		// The dedicated ledger permission, NOT ProcurementRead: the purchase ledger carries
		// supplier prices and payment state. Gating on ProcurementRead would put it in every
		// operator's and park head's sidebar -- the same leak VendorRead exists to avoid.
		return []string{permissions.FeedPurchaseRead}
	case "counts-herd":
		// The Herd Register really is goat data. Its leaf is withheld from the sidebar today
		// (maintainer decision 2026-08-20) but the route stays reachable.
		return []string{permissions.GoatRead}
	// These leaves had NO gate, so they rendered for anyone whose sidebar carried the group
	// and then 403'd on their own data -- a dead screen. An exhaustive persona sweep found
	// nine of them across four real people. Each gate below is the permission that leaf's
	// OWN data route already requires (permissions/routes.go), so the leaf is offered
	// exactly when it can be opened.
	case "counts-herd-analytics", "milk-preparation", "counts-breakdown":
		// counts.read, which is what these three screens' own data routes require. Counts
		// Breakdown was gated on goat.read while /counts/breakdown checks counts.read, so it
		// rendered for three real people and 403'd when they opened it. Counts is a
		// deliberately OFF feature held back by exactly counts.read, so this also stops the
		// leaf advertising a module that is switched off.
		return []string{permissions.CountsRead}
	case "counts-sops", "milk-sops", "feed-sops", "weighing-sops":
		return []string{permissions.SOPRead}
	case "health-analytics":
		// health.read, which is what this screen's own data route requires. It must
		// match the page catalog entry exactly or the leaf renders and then 403s --
		// the dead-leaf class an exhaustive persona sweep found nine of.
		return []string{permissions.HealthRead}
	case "feed-config":
		return []string{permissions.FeedConfigRead}
	case "feed-analytics":
		return []string{permissions.FeedAnalyticsStockRead}
	case "vaccination-live-tracker":
		return []string{permissions.LocationsRead, permissions.ObligationRead, permissions.VaccinationRead}
	case "herd-signals":
		return []string{permissions.HerdSignalsRead}
	case "weighing-weights", "weighing-analytics":
		// The MONITOR capability, matching /app/weighing/shed-weights. Weights is an
		// oversight read-out, not a planning surface, so it must not gate on
		// WeighingPlan (CEO-only): the Growth Director owns weighing oversight and
		// would otherwise be locked out of the estate they are accountable for.
		// ADG Analytics' Comparison tab reads /procurement/loadwise-weights for the
		// purchase side -- the NARROW, unpriced load read that accepts WeighingMonitor
		// as an alternate permission (permissions/routes.go). The priced read stays
		// SalesRead-only, and the tab's value chart is gated on it (load_value_chart).
		return []string{permissions.WeighingMonitor}
	case "people":
		// The staff directory. Before this case existed the leaf fell through to
		// the nil default and rendered for ANY principal with admin_web.bootstrap
		// — the nav-leak fixed by the 2026-08-22 People/HRMS rewrite.
		return []string{permissions.OperatorsRead}
	case "audit-log":
		return []string{permissions.OperatorsViewAudit}
	case "dlq-center":
		return []string{permissions.OperatorsViewAudit}
	case "config", "vaccination-plan":
		return []string{permissions.ProtocolRead}
	case "sop-library":
		return []string{permissions.SOPRead}
	case "approvals":
		// Coarse surface gate for the Approvals page (maintainer decision 2026-07-21). Held by the
		// four org tiers + admin + ceo_internal; park_head no longer holds it, so its nav item is
		// disabled. Matches the /admin-web/counts/approvals route gate.
		return []string{permissions.CountsApproveAccess}
	case "verification-actions":
		return []string{permissions.VerificationReview}
	case "health-config":
		// The READ permission, not the write one: a principal allowed to inspect the standing
		// dosages should reach the screen and see it read-only. Whether the save/publish controls
		// are offered is a separate decision made from HealthConfigWrite on the page contract.
		return []string{permissions.HealthConfigRead}
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
