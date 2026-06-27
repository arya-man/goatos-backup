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
	defaultContractCacheTTL = 60 * time.Second
	redisTTLHintSeconds     = 600
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

type ReferenceFamilies struct {
	Parks              []ReferenceOption
	RuleCategories     []ReferenceOption
	Breeds             []ReferenceOption
	HealthStatuses     []ReferenceOption
	ReproductiveStates []ReferenceOption
	DeferStates        []ReferenceOption
	SOPLabels          []ReferenceOption
	RevisionInputs     map[string]string
}

type ReferenceOption struct {
	Key   string
	Label string
	Title string
	Tone  string
}

type cacheEntry struct {
	expiresAt time.Time
	response  domain.BootstrapResponse
}

func (s *Service) bootstrapCached(ctx context.Context, input BootstrapInput) domain.BootstrapResponse {
	key := s.cacheKey(input)
	now := s.now()
	if cached, ok := s.cached(key, now); ok {
		return cached
	}
	resp := s.compile(ctx, input)
	s.storeCache(key, resp, now.Add(s.cacheTTL))
	return resp
}

func (s *Service) cacheKey(input BootstrapInput) string {
	roles := rolesFromGrants(input.Grants)
	sort.Strings(roles)
	grantParts := make([]string, 0, len(input.Grants))
	for _, grant := range input.Grants {
		grantParts = append(grantParts, grant.Role+"|"+grant.ScopeType+"|"+grant.ScopeID)
	}
	sort.Strings(grantParts)
	return strings.Join([]string{
		strings.TrimSpace(input.TenantID),
		strings.Join(roles, ","),
		strings.Join(grantParts, ","),
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
		return domain.BootstrapResponse{}, false
	}
	return entry.response, true
}

func (s *Service) storeCache(key string, resp domain.BootstrapResponse, expiresAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = map[string]cacheEntry{}
	}
	s.cache[key] = cacheEntry{expiresAt: expiresAt, response: resp}
}

func (s *Service) compile(ctx context.Context, input BootstrapInput) domain.BootstrapResponse {
	families, familyErr := s.loadFamilies(ctx, input.TenantID)
	resp := baseBootstrap()
	resp = compileRequestContext(resp, input, families)
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

func compileRequestContext(resp domain.BootstrapResponse, input BootstrapInput, families ReferenceFamilies) domain.BootstrapResponse {
	resp.TopBar = compileTopBar(resp.TopBar, input, families)
	resp.RoleLenses = compileRoleLenses(input)
	resp.Navigation = compileNavigation(resp.Navigation, input)
	resp.Pages = compilePages(resp.Pages, families)
	return resp
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
	if hasAnyRole(roles, permissions.RoleAdmin, permissions.RoleCEOInternal) {
		return roleLenses()
	}
	out := make([]domain.RoleLensContract, 0, len(roles))
	for _, role := range roles {
		out = append(out, roleLensForRole(role))
	}
	return out
}

func compileNavigation(nav domain.NavigationContract, input BootstrapInput) domain.NavigationContract {
	if len(input.Grants) == 0 {
		return nav
	}
	nav.Primary = compileNavItems(nav.Primary, input)
	for i := range nav.Groups {
		nav.Groups[i].Leaves = compileNavItems(nav.Groups[i].Leaves, input)
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

func compilePages(pages []domain.PageContract, families ReferenceFamilies) []domain.PageContract {
	out := make([]domain.PageContract, len(pages))
	copy(out, pages)
	for i := range out {
		switch out[i].RouteID {
		case "action-center", "vaccination", "shed-execution":
			out[i].OptionGroups = replaceOptionGroup(out[i].OptionGroups, "park_display_chips", optionsFromReferences(families.Parks, "info"))
		case "config":
			out[i].OptionGroups = compileConfigOptionGroups(out[i].OptionGroups, families)
		}
	}
	return out
}

func compileConfigOptionGroups(groups []domain.OptionGroup, families ReferenceFamilies) []domain.OptionGroup {
	out := groups
	if len(families.RuleCategories) > 0 {
		out = replaceOptionGroup(out, "rule_categories", optionsFromReferences(families.RuleCategories, ""))
	}
	out = replaceOptionGroup(out, "rule_scopes", ruleScopeOptions(families.Parks))
	if len(families.Breeds) > 0 {
		out = replaceOptionGroup(out, "rule_breeds", prependOption("all", "all", "", "", optionsFromReferences(families.Breeds, "")))
	}
	if len(families.HealthStatuses) > 0 {
		out = replaceOptionGroup(out, "rule_healths", append(optionsFromReferences(families.HealthStatuses, ""), option("any", "any", "", "")))
	}
	if len(families.ReproductiveStates) > 0 {
		out = replaceOptionGroup(out, "rule_reproductive", prependOption("any", "any", "", "", optionsFromReferences(families.ReproductiveStates, "")))
	}
	if deferStates := deferableStates(families.DeferStates); len(deferStates) > 0 {
		out = replaceOptionGroup(out, "defer_states", optionsFromReferences(deferStates, ""))
	}
	if len(families.SOPLabels) > 0 {
		out = replaceOptionGroup(out, "schedule_sop_labels", optionsFromReferences(families.SOPLabels, ""))
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
	for _, role := range []string{permissions.RoleCEOInternal, permissions.RoleAdmin, permissions.RolePHCDirector, permissions.RoleParkHead, permissions.RoleVerifier, permissions.RoleOperator} {
		for _, got := range roles {
			if got == role {
				return role
			}
		}
	}
	if len(roles) == 0 {
		return permissions.RoleAdmin
	}
	return roles[0]
}

func roleLensForRole(role string) domain.RoleLensContract {
	switch role {
	case permissions.RoleCEOInternal, permissions.RoleAdmin:
		return domain.RoleLensContract{ID: "coo", Name: "Superadmin / CEO / COO", AuditShort: "COO", Scope: "all · deep", Description: "Central Command · all parks", Superadmin: true}
	case permissions.RolePHCDirector:
		return domain.RoleLensContract{ID: "health-director", Name: "Health Director", AuditShort: "Health Dir", Scope: "health vertical · all parks", Description: "PHC / health governance view"}
	case permissions.RoleParkHead:
		return domain.RoleLensContract{ID: "park-head", Name: "Park Head", AuditShort: "Park Head", Scope: "all verticals · assigned park", Description: "Assigned park leadership view"}
	case permissions.RoleVerifier:
		return domain.RoleLensContract{ID: "health-manager", Name: "Health Manager", AuditShort: "Health Mgr", Scope: "health vertical · assigned park", Description: "Assigned-park PHC manager view"}
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
	case permissions.RoleCEOInternal, permissions.RoleAdmin:
		return "AD"
	case permissions.RolePHCDirector:
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
				return fmt.Sprintf("tenant scope · %d active parks", parkCount)
			}
			return "tenant scope"
		}
	}
	return "assigned scoped access"
}

func permissionsForNav(id string) []string {
	switch id {
	case "control-tower", "action-center", "protocol-adherence", "workflows", "phc-vaccination":
		return []string{permissions.ObligationRead, permissions.VaccinationRead}
	case "calendar":
		return []string{permissions.CalendarRead, permissions.VaccinationRead, permissions.ObligationRead}
	case "procurement-source-entry":
		return []string{permissions.ProcurementRead}
	case "counts-herd":
		return []string{permissions.GoatRead}
	case "audit-log":
		return []string{permissions.OperatorsViewAudit}
	case "config":
		return []string{permissions.ProtocolRead}
	case "sop-library":
		return []string{permissions.SOPRead}
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
		"permissions": hashStruct(input.Grants),
		"locations":   hashStruct(families.Parks),
		"config":      hashStruct(struct{ Categories, Breeds, Health, Repro, Defer, SOP []ReferenceOption }{families.RuleCategories, families.Breeds, families.HealthStatuses, families.ReproductiveStates, families.DeferStates, families.SOPLabels}),
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
