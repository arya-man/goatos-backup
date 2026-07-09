// Package domain defines the backend-owned admin-web UI contract.
package domain

type BootstrapResponse struct {
	Source           string              `json:"source"`
	SchemaVersion    string              `json:"schema_version"`
	ContractRevision string              `json:"contract_revision"`
	FamilyHashes     map[string]string   `json:"family_hashes"`
	CachePolicy      ContractCachePolicy `json:"cache_policy"`
	Navigation       NavigationContract  `json:"navigation"`
	NavChrome        string              `json:"nav_chrome"`
	OwnedModules     []OwnedModule       `json:"owned_modules"`
	RouteLabels      []RouteLabelRule    `json:"route_labels"`
	TopBar           TopBarContract      `json:"top_bar"`
	RoleLenses       []RoleLensContract  `json:"role_lenses"`
	Pages            []PageContract      `json:"pages"`
	Copy             map[string]string   `json:"copy"`
	DisplayRules     []DisplayRule       `json:"display_rules"`
}

// Nav chrome states shared by both bootstraps (contract schema NavChrome).
// expanded = show the sidebar (>=2 owned visible modules); minimal = no sidebar
// for single/zero-module principals.
const (
	NavChromeExpanded = "expanded"
	NavChromeMinimal  = "minimal"
)

// OwnedModule is a product module the principal owns via their HR department;
// the set drives visible-nav filtering + the nav_chrome threshold and never
// widens access.
type OwnedModule struct {
	Vertical string `json:"vertical"`
	Module   string `json:"module"`
}

type ContractCachePolicy struct {
	ETag            string `json:"etag"`
	InProcessTTLSec int    `json:"in_process_ttl_sec"`
	RedisTTLHintSec int    `json:"redis_ttl_hint_sec"`
	RevisionSource  string `json:"revision_source"`
}

type NavigationContract struct {
	Primary []NavigationItem  `json:"primary"`
	Groups  []NavigationGroup `json:"groups"`
	Footer  string            `json:"footer"`
}

type NavigationItem struct {
	ID             string            `json:"id"`
	Label          string            `json:"label"`
	Href           string            `json:"href"`
	Icon           string            `json:"icon"`
	BadgeKey       string            `json:"badge_key"`
	Enabled        bool              `json:"enabled"`
	DisabledReason string            `json:"disabled_reason"`
	Domain         string            `json:"domain"`
	Extra          map[string]string `json:"extra"`
}

type NavigationGroup struct {
	ID          string           `json:"id"`
	Label       string           `json:"label"`
	Icon        string           `json:"icon"`
	DefaultOpen bool             `json:"default_open"`
	BadgeKey    string           `json:"badge_key"`
	Leaves      []NavigationItem `json:"leaves"`
}

type RouteLabelRule struct {
	Pattern string `json:"pattern"`
	Label   string `json:"label"`
	Match   string `json:"match"`
}

type TopBarContract struct {
	ProductName       string           `json:"product_name"`
	LogoText          string           `json:"logo_text"`
	ScopeModeToggle   []TopBarOption   `json:"scope_mode_toggle"`
	ParkSelector      TopBarControl    `json:"park_selector"`
	DateRangeSelector TopBarControl    `json:"date_range_selector"`
	Notifications     TopBarControl    `json:"notifications"`
	RolePreview       RolePreviewActor `json:"role_preview"`
}

type TopBarOption struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	Title          string `json:"title"`
	Enabled        bool   `json:"enabled"`
	DisabledReason string `json:"disabled_reason"`
}

type TopBarControl struct {
	Label          string         `json:"label"`
	Enabled        bool           `json:"enabled"`
	DisabledReason string         `json:"disabled_reason"`
	Hint           string         `json:"hint"`
	Options        []TopBarOption `json:"options"`
}

type RolePreviewActor struct {
	DisplayName string `json:"display_name"`
	Initials    string `json:"initials"`
	Subtitle    string `json:"subtitle"`
}

type RoleLensContract struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AuditShort  string `json:"audit_short"`
	Scope       string `json:"scope"`
	Description string `json:"description"`
	Superadmin  bool   `json:"superadmin"`
}

type PageContract struct {
	RouteID         string            `json:"route_id"`
	Href            string            `json:"href"`
	PathPattern     string            `json:"path_pattern"`
	Title           string            `json:"title"`
	Subtitle        string            `json:"subtitle"`
	SurfaceKind     string            `json:"surface_kind"`
	SourceScope     []string          `json:"source_scope"`
	Sections        []Section         `json:"sections"`
	Tables          []TableContract   `json:"tables"`
	Drawers         []DrawerContract  `json:"drawers"`
	Controls        []Control         `json:"controls"`
	Copy            map[string]string `json:"copy"`
	OptionGroups    []OptionGroup     `json:"option_groups"`
	MigrationStatus string            `json:"migration_status"`
	ValidationNotes []string          `json:"validation_notes"`
}

type Section struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Kind     string   `json:"kind"`
	ChipKeys []string `json:"chip_keys"`
}

type TableContract struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	DataSource      string       `json:"data_source"`
	Columns         []Column     `json:"columns"`
	Filters         []Filter     `json:"filters"`
	SortKeys        []SortKey    `json:"sort_keys"`
	PageSizeOptions []int        `json:"page_size_options"`
	RowClick        RowClickRule `json:"row_click"`
	SummaryFields   []string     `json:"summary_fields"`
	DetailFields    []string     `json:"detail_fields"`
}

type Column struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Sortable bool   `json:"sortable"`
	Visible  bool   `json:"visible"`
}

type Filter struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	Kind           string `json:"kind"`
	Enabled        bool   `json:"enabled"`
	DisabledReason string `json:"disabled_reason"`
}

type SortKey struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Direction string `json:"direction"`
}

type RowClickRule struct {
	Enabled       bool     `json:"enabled"`
	Param         string   `json:"param"`
	TargetDrawer  string   `json:"target_drawer"`
	SummaryFields []string `json:"summary_fields"`
	DetailFields  []string `json:"detail_fields"`
}

type DrawerContract struct {
	ID            string    `json:"id"`
	TitleSource   string    `json:"title_source"`
	TriggerParam  string    `json:"trigger_param"`
	DataSource    string    `json:"data_source"`
	Anatomy       string    `json:"anatomy"`
	SummaryFields []string  `json:"summary_fields"`
	DetailFields  []string  `json:"detail_fields"`
	FooterActions []Control `json:"footer_actions"`
}

type Control struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Kind           string `json:"kind"`
	Enabled        bool   `json:"enabled"`
	DisabledReason string `json:"disabled_reason"`
	Action         string `json:"action"`
}

type OptionGroup struct {
	ID      string   `json:"id"`
	Options []Option `json:"options"`
}

type Option struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	Title          string `json:"title"`
	Enabled        bool   `json:"enabled"`
	DisabledReason string `json:"disabled_reason"`
	Tone           string `json:"tone"`
}

type DisplayRule struct {
	ID           string   `json:"id"`
	AppliesTo    []string `json:"applies_to"`
	Summary      string   `json:"summary"`
	FrontendOwns []string `json:"frontend_owns"`
	BackendOwns  []string `json:"backend_owns"`
}
