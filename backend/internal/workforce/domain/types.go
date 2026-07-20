package domain

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorEnvelope struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	FieldErrors []FieldError `json:"field_errors"`
	TraceID     string       `json:"trace_id"`
	Retryable   bool         `json:"retryable"`
}

type OperatorProfile struct {
	OperatorID        string         `json:"operator_id"`
	UserID            *string        `json:"user_id"`
	DisplayCode       string         `json:"display_code"`
	DisplayName       string         `json:"display_name"`
	Status            string         `json:"status"`
	PrimaryRoleHint   string         `json:"primary_role_hint"`
	PrimaryLocationID *string        `json:"primary_location_id"`
	PrimaryLocation   *string        `json:"primary_location"`
	GrantCount        int            `json:"grant_count"`
	CapabilityCount   int            `json:"capability_count"`
	ActiveDeviceCount int            `json:"active_device_count"`
	Metadata          map[string]any `json:"metadata"`
	RowVersion        int            `json:"row_version"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type OperatorListResponse struct {
	Items   []OperatorProfile `json:"items"`
	TraceID string            `json:"trace_id"`
}

type OperatorResponse struct {
	Operator OperatorProfile `json:"operator"`
	TraceID  string          `json:"trace_id"`
}

type CreateOperatorRequest struct {
	UserID            *string        `json:"user_id"`
	DisplayCode       string         `json:"display_code"`
	DisplayName       string         `json:"display_name"`
	Status            string         `json:"status"`
	PrimaryRoleHint   string         `json:"primary_role_hint"`
	PrimaryLocationID *string        `json:"primary_location_id"`
	Metadata          map[string]any `json:"metadata"`
}

type UpdateOperatorRequest struct {
	DisplayName       *string        `json:"display_name"`
	PrimaryRoleHint   *string        `json:"primary_role_hint"`
	PrimaryLocationID *string        `json:"primary_location_id"`
	Metadata          map[string]any `json:"metadata"`
	RowVersion        int            `json:"row_version"`
}

type StatusChangeRequest struct {
	Reason     string `json:"reason"`
	RowVersion int    `json:"row_version"`
}

type GrantSummary struct {
	GrantID   string  `json:"grant_id"`
	UserID    string  `json:"user_id"`
	Role      string  `json:"role"`
	ScopeType string  `json:"scope_type"`
	ScopeID   string  `json:"scope_id"`
	Status    string  `json:"status"`
	ValidFrom string  `json:"valid_from"`
	ValidTo   *string `json:"valid_to"`
	CreatedAt string  `json:"created_at"`
	CreatedBy *string `json:"created_by"`
}

type GrantListResponse struct {
	Items   []GrantSummary `json:"items"`
	TraceID string         `json:"trace_id"`
}

type CreateGrantRequest struct {
	Role      string  `json:"role"`
	ScopeType string  `json:"scope_type"`
	ScopeID   string  `json:"scope_id"`
	ValidTo   *string `json:"valid_to"`
}

type CapabilityAssignment struct {
	MemberCapabilityID string  `json:"member_capability_id"`
	CapabilityID       string  `json:"capability_id"`
	CapabilityCode     string  `json:"capability_code"`
	Description        string  `json:"description"`
	ScopeType          string  `json:"scope_type"`
	ScopeID            string  `json:"scope_id"`
	Status             string  `json:"status"`
	ValidFrom          string  `json:"valid_from"`
	ValidTo            *string `json:"valid_to"`
	CreatedAt          string  `json:"created_at"`
}

type CapabilityResponse struct {
	Capability CapabilityAssignment `json:"capability"`
	TraceID    string               `json:"trace_id"`
}

// CreateCapabilityRequest creates a time-bounded capability grant for a member.
// ValidFrom defaults to now() if omitted; ValidTo must be after ValidFrom.
// For roster coverage (design doc S4.6), ValidFrom should be set to the coverage
// window start, not now(), so the capability is active ONLY during the window.
type CreateCapabilityRequest struct {
	CapabilityCode string  `json:"capability_code"`
	ScopeType      string  `json:"scope_type"`
	ScopeID        string  `json:"scope_id"`
	ValidFrom      *string `json:"valid_from"`
	ValidTo        *string `json:"valid_to"`
}

type DeviceSummary struct {
	DeviceID            string  `json:"device_id"`
	OperatorID          string  `json:"operator_id"`
	Platform            string  `json:"platform"`
	AppInstallID        string  `json:"app_install_id"`
	DevicePublicKeyHash *string `json:"device_public_key_hash"`
	PushTokenHash       *string `json:"push_token_hash"`
	// FCMToken is the raw FCM registration token the push gateway needs (message.token).
	// PushTokenHash stays the identity/dedup hash; FCMToken is the delivery address (migration 000171).
	FCMToken     *string        `json:"fcm_token,omitempty"`
	AppVersion   string         `json:"app_version"`
	OSVersion    string         `json:"os_version"`
	Status       string         `json:"status"`
	LastSeenAt   string         `json:"last_seen_at"`
	RegisteredAt string         `json:"registered_at"`
	RevokedAt    *string        `json:"revoked_at"`
	Metadata     map[string]any `json:"metadata"`
	RowVersion   int            `json:"row_version"`
}

type DeviceListResponse struct {
	Items   []DeviceSummary `json:"items"`
	TraceID string          `json:"trace_id"`
}

type DeviceResponse struct {
	Device  DeviceSummary `json:"device"`
	TraceID string        `json:"trace_id"`
}

type RegisterDeviceRequest struct {
	AppInstallID        string  `json:"app_install_id"`
	DevicePublicKeyHash *string `json:"device_public_key_hash"`
	PushTokenHash       *string `json:"push_token_hash"`
	// FcmToken is the raw FCM registration token (optional -- backward compatible with clients that
	// have not yet upgraded to send it; push_token_hash keeps working as the identity/dedup hash).
	FcmToken   *string        `json:"fcm_token"`
	AppVersion string         `json:"app_version"`
	OSVersion  string         `json:"os_version"`
	Metadata   map[string]any `json:"metadata"`
}

type HeartbeatDeviceRequest struct {
	AppVersion    string         `json:"app_version"`
	OSVersion     string         `json:"os_version"`
	PushTokenHash *string        `json:"push_token_hash"`
	FcmToken      *string        `json:"fcm_token"`
	Metadata      map[string]any `json:"metadata"`
}

type RevokeDeviceRequest struct {
	Reason     string `json:"reason"`
	RowVersion int    `json:"row_version"`
}

type SourceCandidate struct {
	CandidateID       string         `json:"candidate_id"`
	OperatorID        *string        `json:"operator_id"`
	SourceSystem      string         `json:"source_system"`
	SourceFlow        string         `json:"source_flow"`
	ExternalRefType   string         `json:"external_ref_type"`
	ExternalRefHash   string         `json:"external_ref_hash"`
	Status            string         `json:"status"`
	Confidence        float64        `json:"confidence"`
	FirstSeenAt       string         `json:"first_seen_at"`
	LastSeenAt        string         `json:"last_seen_at"`
	ObservationCount  int64          `json:"observation_count"`
	ReviewReason      *string        `json:"review_reason"`
	ObservedRoleHint  *string        `json:"observed_role_hint"`
	ObservedScopeHint *string        `json:"observed_scope_hint"`
	Metadata          map[string]any `json:"metadata"`
	RowVersion        int            `json:"row_version"`
}

type SourceCandidateListResponse struct {
	Items   []SourceCandidate `json:"items"`
	TraceID string            `json:"trace_id"`
}

type SourceCandidateResponse struct {
	Candidate SourceCandidate `json:"candidate"`
	TraceID   string          `json:"trace_id"`
}

type MapSourceCandidateRequest struct {
	OperatorID string `json:"operator_id"`
	Reason     string `json:"reason"`
	RowVersion int    `json:"row_version"`
}

type RejectSourceCandidateRequest struct {
	Reason     string `json:"reason"`
	RowVersion int    `json:"row_version"`
}

type AppMeResponse struct {
	ActorID  string           `json:"actor_id"`
	Operator *OperatorProfile `json:"operator_profile"`
	Grants   []GrantSummary   `json:"grants"`
	TraceID  string           `json:"trace_id"`
}

type BootstrapResponse struct {
	Actor                   BootstrapActor            `json:"actor"`
	OperatorProfile         OperatorProfile           `json:"operator_profile"`
	RolesAndScopes          []GrantSummary            `json:"roles_and_scopes"`
	Capabilities            []CapabilityAssignment    `json:"capabilities"`
	DeviceState             BootstrapDeviceState      `json:"device_state"`
	AppMinSupportedVersion  string                    `json:"app_min_supported_version"`
	FeatureFlags            map[string]bool           `json:"feature_flags"`
	VisibleNavigation       []BootstrapNavigationItem `json:"visible_navigation"`
	Modules                 []BootstrapModule         `json:"modules"`
	NavChrome               string                    `json:"nav_chrome"`
	TaskQueueDescriptors    []BootstrapTaskQueue      `json:"task_queue_descriptors"`
	PinnedSOPVersions       []BootstrapSOPVersion     `json:"pinned_sop_versions"`
	SupportedFieldTypes     []string                  `json:"supported_field_types"`
	SupportedRuleOperators  []string                  `json:"supported_rule_operators"`
	SupportedProofActions   []string                  `json:"supported_proof_actions"`
	OptionSourceDescriptors []BootstrapOptionSource   `json:"option_source_descriptors"`
	SyncPolicy              BootstrapSyncPolicy       `json:"sync_policy"`
	ServerTime              string                    `json:"server_time"`
	TraceID                 string                    `json:"trace_id"`
}

type BootstrapActor struct {
	ActorID  string `json:"actor_id"`
	TenantID string `json:"tenant_id"`
}

type BootstrapDeviceState struct {
	Required bool           `json:"required"`
	Device   *DeviceSummary `json:"device"`
	Status   string         `json:"status"`
	Reason   *string        `json:"reason"`
}

// BootstrapModule is a drawer entry: the module's identity plus the bottom-bar items
// it contributes. NavItems is MODULE-SCOPED — selecting this module in the drawer
// swaps the bottom bar to these items. "soon" modules render disabled with no items.
// See docs/decisions/role-module-nav-composition.md.
type BootstrapModule struct {
	Key      string                    `json:"key"`
	Label    string                    `json:"label"`
	Href     string                    `json:"href"`
	Status   string                    `json:"status"`
	NavItems []BootstrapNavigationItem `json:"nav_items"`
}

type BootstrapNavigationItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Href  string `json:"href"`
}

// Nav chrome states shared by both bootstraps (see contract schema NavChrome).
// expanded = show the module switcher; minimal = bottom-bar-only / no-sidebar.
const (
	NavChromeExpanded = "expanded"
	NavChromeMinimal  = "minimal"
)

type BootstrapTaskQueue struct {
	Key                  string   `json:"key"`
	Label                string   `json:"label"`
	RequiredCapabilities []string `json:"required_capabilities"`
}

type BootstrapSOPVersion struct {
	SOPCode      string  `json:"sop_code"`
	SOPVersionID *string `json:"sop_version_id"`
	Status       string  `json:"status"`
	Compatible   bool    `json:"compatible"`
	Reason       *string `json:"reason"`
}

type BootstrapOptionSource struct {
	Key        string `json:"key"`
	ScopeType  string `json:"scope_type"`
	ScopeID    string `json:"scope_id"`
	TTLSeconds int    `json:"ttl_seconds"`
}

type BootstrapSyncPolicy struct {
	OfflineDraftTTLHours     int `json:"offline_draft_ttl_hours"`
	HeartbeatIntervalSeconds int `json:"heartbeat_interval_seconds"`
	MaxRetryBackoffSeconds   int `json:"max_retry_backoff_seconds"`
}
