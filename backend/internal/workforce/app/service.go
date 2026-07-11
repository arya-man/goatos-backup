package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

type Service struct {
	repo ports.Repository
	now  func() time.Time
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

func (s *Service) ListOperators(ctx context.Context, params ports.ListOperatorsParams, traceID string) (*domain.OperatorListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.Status = strings.TrimSpace(params.Status)
	params.RoleHint = strings.TrimSpace(params.RoleHint)
	params.LocationID = strings.TrimSpace(params.LocationID)
	params.Search = strings.TrimSpace(params.Search)
	if params.LocationID != "" && !uuidutil.IsUUIDString(params.LocationID) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	params.Limit = boundedLimit(params.Limit, 100)
	items, err := s.repo.ListOperators(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.OperatorListResponse{Items: items, TraceID: traceID}, nil
}

func (s *Service) CreateOperator(ctx context.Context, cmd ports.CreateOperatorCommand, traceID string) (*domain.OperatorResponse, error) {
	if err := validateTenantAndActor(cmd.TenantID, cmd.ActorID); err != nil {
		return nil, err
	}
	normalizeCreateOperator(&cmd.Body)
	if err := validateCreateOperator(cmd.Body); err != nil {
		return nil, err
	}
	item, err := s.repo.CreateOperator(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.OperatorResponse{Operator: item, TraceID: traceID}, nil
}

func (s *Service) GetOperator(ctx context.Context, tenantID, operatorID, traceID string) (*domain.OperatorResponse, error) {
	if err := validateTenantAndOperator(tenantID, operatorID); err != nil {
		return nil, err
	}
	item, err := s.repo.GetOperator(ctx, tenantID, operatorID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.OperatorResponse{Operator: item, TraceID: traceID}, nil
}

func (s *Service) UpdateOperator(ctx context.Context, cmd ports.UpdateOperatorCommand, traceID string) (*domain.OperatorResponse, error) {
	if err := validateTenantActorOperator(cmd.TenantID, cmd.ActorID, cmd.OperatorID); err != nil {
		return nil, err
	}
	if cmd.Body.RowVersion <= 0 {
		return nil, BadRequest("invalid_row_version", "row_version is required")
	}
	if cmd.Body.DisplayName != nil {
		v := strings.TrimSpace(*cmd.Body.DisplayName)
		cmd.Body.DisplayName = &v
		if v == "" {
			return nil, BadRequest("invalid_display_name", "display_name is required")
		}
	}
	if cmd.Body.PrimaryRoleHint != nil {
		v := strings.TrimSpace(*cmd.Body.PrimaryRoleHint)
		cmd.Body.PrimaryRoleHint = &v
		if !validRoleHint(v) {
			return nil, BadRequest("invalid_primary_role_hint", "primary_role_hint is invalid")
		}
	}
	if cmd.Body.PrimaryLocationID != nil {
		v := strings.TrimSpace(*cmd.Body.PrimaryLocationID)
		cmd.Body.PrimaryLocationID = &v
		if v != "" && !uuidutil.IsUUIDString(v) {
			return nil, BadRequest("invalid_location_id", "primary_location_id must be a UUID or empty string")
		}
	}
	item, err := s.repo.UpdateOperator(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.OperatorResponse{Operator: item, TraceID: traceID}, nil
}

func (s *Service) SetOperatorStatus(ctx context.Context, cmd ports.StatusCommand, traceID string) (*domain.OperatorResponse, error) {
	if err := validateTenantActorOperator(cmd.TenantID, cmd.ActorID, cmd.OperatorID); err != nil {
		return nil, err
	}
	cmd.Reason = strings.TrimSpace(cmd.Reason)
	if cmd.RowVersion <= 0 {
		return nil, BadRequest("invalid_row_version", "row_version is required")
	}
	if cmd.Status != "active" && cmd.Status != "inactive" {
		return nil, BadRequest("invalid_status", "status change is invalid")
	}
	item, err := s.repo.SetOperatorStatus(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.OperatorResponse{Operator: item, TraceID: traceID}, nil
}

func (s *Service) ListGrants(ctx context.Context, tenantID, operatorID, traceID string) (*domain.GrantListResponse, error) {
	if err := validateTenantAndOperator(tenantID, operatorID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListGrants(ctx, tenantID, operatorID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.GrantListResponse{Items: items, TraceID: traceID}, nil
}

func (s *Service) CreateGrant(ctx context.Context, cmd ports.CreateGrantCommand, traceID string) (*domain.GrantListResponse, error) {
	if err := validateTenantActorOperator(cmd.TenantID, cmd.ActorID, cmd.OperatorID); err != nil {
		return nil, err
	}
	cmd.Body.Role = strings.TrimSpace(cmd.Body.Role)
	cmd.Body.ScopeType = strings.TrimSpace(cmd.Body.ScopeType)
	cmd.Body.ScopeID = strings.TrimSpace(cmd.Body.ScopeID)
	if !validRole(cmd.Body.Role) {
		return nil, BadRequest("invalid_role", "role is invalid")
	}
	if !validScope(cmd.Body.ScopeType, cmd.Body.ScopeID) {
		return nil, BadRequest("invalid_scope", "scope_type and scope_id are required")
	}
	if cmd.Body.ValidTo != nil {
		v := strings.TrimSpace(*cmd.Body.ValidTo)
		cmd.Body.ValidTo = &v
		if _, err := time.Parse(time.RFC3339, v); err != nil {
			return nil, BadRequest("invalid_valid_to", "valid_to must be RFC3339")
		}
	}
	if _, err := s.repo.CreateGrant(ctx, cmd); err != nil {
		return nil, mapRepoErr(err)
	}
	items, err := s.repo.ListGrants(ctx, cmd.TenantID, cmd.OperatorID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.GrantListResponse{Items: items, TraceID: traceID}, nil
}

func (s *Service) AssignCapability(ctx context.Context, cmd ports.CapabilityCommand, traceID string) (*domain.CapabilityResponse, error) {
	if err := validateTenantActorOperator(cmd.TenantID, cmd.ActorID, cmd.OperatorID); err != nil {
		return nil, err
	}
	cmd.Body.CapabilityCode = strings.TrimSpace(cmd.Body.CapabilityCode)
	cmd.Body.ScopeType = strings.TrimSpace(cmd.Body.ScopeType)
	cmd.Body.ScopeID = strings.TrimSpace(cmd.Body.ScopeID)
	if cmd.Body.CapabilityCode == "" {
		return nil, BadRequest("invalid_capability_code", "capability_code is required")
	}
	if !validScope(cmd.Body.ScopeType, cmd.Body.ScopeID) {
		return nil, BadRequest("invalid_scope", "scope_type and scope_id are required")
	}
	item, err := s.repo.AssignCapability(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.CapabilityResponse{Capability: item, TraceID: traceID}, nil
}

func (s *Service) RemoveCapability(ctx context.Context, cmd ports.RemoveCapabilityCommand, traceID string) (*domain.CapabilityResponse, error) {
	if err := validateTenantActorOperator(cmd.TenantID, cmd.ActorID, cmd.OperatorID); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(cmd.CapabilityID) {
		return nil, BadRequest("invalid_capability_id", "capability_id must be a UUID")
	}
	if err := s.repo.RemoveCapability(ctx, cmd); err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.CapabilityResponse{TraceID: traceID}, nil
}

func (s *Service) ListDevices(ctx context.Context, tenantID, operatorID, traceID string) (*domain.DeviceListResponse, error) {
	if err := validateTenantAndOperator(tenantID, operatorID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListDevices(ctx, tenantID, operatorID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.DeviceListResponse{Items: items, TraceID: traceID}, nil
}

func (s *Service) RevokeDevice(ctx context.Context, cmd ports.RevokeDeviceCommand, traceID string) (*domain.DeviceResponse, error) {
	if err := validateTenantActorOperator(cmd.TenantID, cmd.ActorID, cmd.OperatorID); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(cmd.DeviceID) {
		return nil, BadRequest("invalid_device_id", "device_id must be a UUID")
	}
	if cmd.RowVersion <= 0 {
		return nil, BadRequest("invalid_row_version", "row_version is required")
	}
	item, err := s.repo.RevokeDevice(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.DeviceResponse{Device: item, TraceID: traceID}, nil
}

func (s *Service) ListSourceCandidates(ctx context.Context, params ports.ListSourceCandidatesParams, traceID string) (*domain.SourceCandidateListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.Status = strings.TrimSpace(params.Status)
	params.SourceSystem = strings.TrimSpace(params.SourceSystem)
	params.Limit = boundedLimit(params.Limit, 100)
	items, err := s.repo.ListSourceCandidates(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SourceCandidateListResponse{Items: items, TraceID: traceID}, nil
}

func (s *Service) MapSourceCandidate(ctx context.Context, cmd ports.MapSourceCandidateCommand, traceID string) (*domain.SourceCandidateResponse, error) {
	if err := validateTenantAndActor(cmd.TenantID, cmd.ActorID); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(cmd.CandidateID) || !uuidutil.IsUUIDString(cmd.Body.OperatorID) {
		return nil, BadRequest("invalid_mapping", "candidate_id and operator_id must be UUIDs")
	}
	if cmd.Body.RowVersion <= 0 {
		return nil, BadRequest("invalid_row_version", "row_version is required")
	}
	item, err := s.repo.MapSourceCandidate(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SourceCandidateResponse{Candidate: item, TraceID: traceID}, nil
}

func (s *Service) RejectSourceCandidate(ctx context.Context, cmd ports.RejectSourceCandidateCommand, traceID string) (*domain.SourceCandidateResponse, error) {
	if err := validateTenantAndActor(cmd.TenantID, cmd.ActorID); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(cmd.CandidateID) {
		return nil, BadRequest("invalid_candidate_id", "candidate_id must be a UUID")
	}
	if cmd.Body.RowVersion <= 0 {
		return nil, BadRequest("invalid_row_version", "row_version is required")
	}
	item, err := s.repo.RejectSourceCandidate(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.SourceCandidateResponse{Candidate: item, TraceID: traceID}, nil
}

func (s *Service) AppMe(ctx context.Context, tenantID, actorID, traceID string) (*domain.AppMeResponse, error) {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return nil, err
	}
	grants, err := s.repo.ListActiveGrantsForActor(ctx, tenantID, actorID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	profile, err := s.repo.GetMemberForActor(ctx, tenantID, actorID)
	if errors.Is(err, ports.ErrNotFound) {
		return &domain.AppMeResponse{ActorID: actorID, Operator: nil, Grants: grants, TraceID: traceID}, nil
	}
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.AppMeResponse{ActorID: actorID, Operator: &profile, Grants: grants, TraceID: traceID}, nil
}

func (s *Service) RegisterDevice(ctx context.Context, cmd ports.RegisterDeviceCommand, traceID string) (*domain.DeviceResponse, error) {
	if err := validateTenantAndActor(cmd.TenantID, cmd.ActorID); err != nil {
		return nil, err
	}
	cmd.Body.AppInstallID = strings.TrimSpace(cmd.Body.AppInstallID)
	cmd.Body.AppVersion = strings.TrimSpace(cmd.Body.AppVersion)
	cmd.Body.OSVersion = strings.TrimSpace(cmd.Body.OSVersion)
	if cmd.Body.AppInstallID == "" || cmd.Body.AppVersion == "" {
		return nil, BadRequest("invalid_device", "app_install_id and app_version are required")
	}
	if _, _, err := s.activeProfileAndGrants(ctx, cmd.TenantID, cmd.ActorID); err != nil {
		return nil, err
	}
	item, err := s.repo.RegisterDevice(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.DeviceResponse{Device: item, TraceID: traceID}, nil
}

func (s *Service) HeartbeatDevice(ctx context.Context, cmd ports.HeartbeatDeviceCommand, traceID string) (*domain.DeviceResponse, error) {
	if err := validateTenantAndActor(cmd.TenantID, cmd.ActorID); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(cmd.DeviceID) {
		return nil, BadRequest("invalid_device_id", "device_id must be a UUID")
	}
	if _, _, err := s.activeProfileAndGrants(ctx, cmd.TenantID, cmd.ActorID); err != nil {
		return nil, err
	}
	item, err := s.repo.HeartbeatDevice(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if item.Status != "active" {
		return nil, Forbidden("device_revoked", "device is not active")
	}
	return &domain.DeviceResponse{Device: item, TraceID: traceID}, nil
}

func (s *Service) Bootstrap(ctx context.Context, tenantID, actorID, deviceID, localeTag, traceID string) (*domain.BootstrapResponse, error) {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return nil, err
	}
	localeTag = localization.Normalize(localeTag)
	profile, grants, err := s.activeProfileAndGrants(ctx, tenantID, actorID)
	if err != nil {
		return nil, err
	}
	caps, err := s.repo.ListCapabilities(ctx, tenantID, profile.OperatorID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	var device *domain.DeviceSummary
	deviceState := domain.BootstrapDeviceState{Required: true, Status: "not_registered"}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID != "" {
		item, err := s.repo.GetDeviceForActor(ctx, tenantID, actorID, deviceID)
		if err != nil {
			return nil, mapRepoErr(err)
		}
		if item.Status != "active" {
			reason := "device is not active"
			return nil, Forbidden("device_revoked", reason)
		}
		device = &item
		deviceState = domain.BootstrapDeviceState{Required: true, Device: device, Status: item.Status}
	}
	now := s.now().UTC()
	return &domain.BootstrapResponse{
		Actor:                  domain.BootstrapActor{ActorID: actorID, TenantID: tenantID},
		OperatorProfile:        profile,
		RolesAndScopes:         grants,
		Capabilities:           caps,
		DeviceState:            deviceState,
		AppMinSupportedVersion: "0.1.0",
		FeatureFlags: map[string]bool{
			"tasks":          true,
			"sop_runner":     true,
			"proof_capture":  hasCapability(caps, "media.video_capture"),
			"animal_id_scan": hasCapability(caps, "animal_id.scan"),
		},
		VisibleNavigation:       visibleNavigationFor(grants, localeTag),
		NavChrome:               navChromeFor(grants),
		TaskQueueDescriptors:    queuesFor(caps, localeTag),
		PinnedSOPVersions:       []domain.BootstrapSOPVersion{},
		SupportedFieldTypes:     []string{"text", "number", "date_time", "boolean", "select", "multiselect", "goat_scan", "animal_id_scan", "goat_lookup", "shed_picker", "photo_proof", "video_proof"},
		SupportedRuleOperators:  []string{"visible_if", "required_if", "enabled_if", "proof_required_if", "block_submission_if", "repeat_for_each_goat"},
		SupportedProofActions:   []string{"photo", "video", "original_proof", "rectified_proof", "verifier_proof"},
		OptionSourceDescriptors: optionSourcesFor(grants),
		SyncPolicy: domain.BootstrapSyncPolicy{
			OfflineDraftTTLHours:     24,
			HeartbeatIntervalSeconds: 300,
			MaxRetryBackoffSeconds:   900,
		},
		ServerTime: now.Format(time.RFC3339),
		TraceID:    traceID,
	}, nil
}

func (s *Service) activeProfileAndGrants(ctx context.Context, tenantID, actorID string) (domain.OperatorProfile, []domain.GrantSummary, error) {
	profile, err := s.repo.GetMemberForActor(ctx, tenantID, actorID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return domain.OperatorProfile{}, nil, Forbidden("operator_profile_missing", "active operator profile is required")
		}
		return domain.OperatorProfile{}, nil, mapRepoErr(err)
	}
	if profile.Status != "active" {
		return domain.OperatorProfile{}, nil, Forbidden("operator_profile_inactive", "operator profile is not active")
	}
	grants, err := s.repo.ListActiveGrantsForActor(ctx, tenantID, actorID)
	if err != nil {
		return domain.OperatorProfile{}, nil, mapRepoErr(err)
	}
	if len(grants) == 0 {
		return domain.OperatorProfile{}, nil, Forbidden("operator_grant_missing", "active operator grant is required")
	}
	return profile, grants, nil
}

func normalizeCreateOperator(body *domain.CreateOperatorRequest) {
	body.DisplayCode = strings.TrimSpace(body.DisplayCode)
	body.DisplayName = strings.TrimSpace(body.DisplayName)
	body.Status = strings.TrimSpace(body.Status)
	body.PrimaryRoleHint = strings.TrimSpace(body.PrimaryRoleHint)
	if body.Status == "" {
		body.Status = "candidate"
	}
	if body.PrimaryRoleHint == "" {
		body.PrimaryRoleHint = "operator"
	}
	if body.PrimaryLocationID != nil {
		v := strings.TrimSpace(*body.PrimaryLocationID)
		body.PrimaryLocationID = &v
	}
	if body.UserID != nil {
		v := strings.TrimSpace(*body.UserID)
		body.UserID = &v
	}
}

func validateCreateOperator(body domain.CreateOperatorRequest) error {
	if body.DisplayCode == "" {
		return BadRequest("invalid_display_code", "display_code is required")
	}
	if body.DisplayName == "" {
		return BadRequest("invalid_display_name", "display_name is required")
	}
	if !validMemberStatus(body.Status) {
		return BadRequest("invalid_status", "status is invalid")
	}
	if !validRoleHint(body.PrimaryRoleHint) {
		return BadRequest("invalid_primary_role_hint", "primary_role_hint is invalid")
	}
	if body.UserID != nil && *body.UserID != "" && !uuidutil.IsUUIDString(*body.UserID) {
		return BadRequest("invalid_user_id", "user_id must be a UUID")
	}
	if body.PrimaryLocationID != nil && *body.PrimaryLocationID != "" && !uuidutil.IsUUIDString(*body.PrimaryLocationID) {
		return BadRequest("invalid_location_id", "primary_location_id must be a UUID")
	}
	return nil
}

func validateTenant(tenantID string) error {
	if !uuidutil.IsUUIDString(strings.TrimSpace(tenantID)) {
		return Unauthorized("missing_tenant_scope", "tenant scope is required")
	}
	return nil
}

func validateTenantAndActor(tenantID, actorID string) error {
	if err := validateTenant(tenantID); err != nil {
		return err
	}
	if !uuidutil.IsUUIDString(strings.TrimSpace(actorID)) {
		return Unauthorized("missing_actor", "authenticated actor is required")
	}
	return nil
}

func validateTenantAndOperator(tenantID, operatorID string) error {
	if err := validateTenant(tenantID); err != nil {
		return err
	}
	if !uuidutil.IsUUIDString(strings.TrimSpace(operatorID)) {
		return BadRequest("invalid_operator_id", "operator_id must be a UUID")
	}
	return nil
}

func validateTenantActorOperator(tenantID, actorID, operatorID string) error {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return err
	}
	if !uuidutil.IsUUIDString(strings.TrimSpace(operatorID)) {
		return BadRequest("invalid_operator_id", "operator_id must be a UUID")
	}
	return nil
}

func boundedLimit(limit, fallback int) int {
	if limit <= 0 {
		return fallback
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func mapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ports.ErrNotFound) {
		return NotFound("resource is missing or outside tenant scope")
	}
	if errors.Is(err, ports.ErrConflict) {
		return Conflict("write_conflict", "row changed or conflicts with an existing active record")
	}
	if errors.Is(err, ports.ErrIdempotencyConflict) {
		return Conflict("idempotency_conflict", "Idempotency-Key was reused with a different request payload")
	}
	if errors.Is(err, ports.ErrInvalidFilter) {
		return BadRequest("invalid_filter", "one or more filters are invalid")
	}
	if errors.Is(err, ports.ErrDenied) {
		return Forbidden("operator_gate_denied", "operator gate denied")
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal("workforce repository operation failed")
}

func validMemberStatus(value string) bool {
	switch value {
	case "candidate", "active", "inactive", "suspended", "left":
		return true
	default:
		return false
	}
}

func validRoleHint(value string) bool {
	switch value {
	case "operator", "park_head", "pc_director", "verifier", "supervisor", "admin", "other":
		return true
	default:
		return false
	}
}

func validRole(value string) bool {
	switch value {
	case "admin", "park_head", "pc_director", "operator", "verifier", "ceo_internal":
		return true
	default:
		return false
	}
}

func validScope(scopeType, scopeID string) bool {
	switch scopeType {
	case "tenant", "custodian_party", "farm", "park", "shed", "cohort":
	default:
		return false
	}
	return uuidutil.IsUUIDString(scopeID)
}

func hasCapability(items []domain.CapabilityAssignment, code string) bool {
	for _, item := range items {
		if item.Status == "active" && item.CapabilityCode == code {
			return true
		}
	}
	return false
}

// leadershipGrantRoles are the workforce grant roles that see the fixed
// leadership mobile nav (Calendar / Overview / Alerts). Mirrors the role-lens
// tiers already used for the admin-web bootstrap (see roleLensForRole in
// internal/adminui/app/compiler.go): ceo_internal/admin fold to CEO/COO,
// pc_director is the health director, park_head is the park manager, and
// verifier is the (assigned-park) health manager. permissions.RoleOperator is
// the only grant role that is never leadership.
var leadershipGrantRoles = map[string]bool{
	permissions.RoleAdmin:       true,
	permissions.RoleCEOInternal: true,
	permissions.RolePCDirector:  true,
	permissions.RoleParkHead:    true,
	permissions.RoleVerifier:    true,
}

// isLeadershipPrincipal reports whether any active grant carries a
// leadership-tier role, regardless of the grant's scope (a park-scoped Park
// Head or Health Manager is still leadership for their park, not an
// operator). An actor with only operator grants is not leadership.
func isLeadershipPrincipal(grants []domain.GrantSummary) bool {
	for _, g := range grants {
		if leadershipGrantRoles[g.Role] {
			return true
		}
	}
	return false
}

// navChromeFor gives leadership/CEO principals the module drawer (expanded);
// field operators stay on bottom-bar-only chrome (minimal).
func navChromeFor(grants []domain.GrantSummary) string {
	if isLeadershipPrincipal(grants) {
		return domain.NavChromeExpanded
	}
	return domain.NavChromeMinimal
}

func optionSourcesFor(grants []domain.GrantSummary) []domain.BootstrapOptionSource {
	items := make([]domain.BootstrapOptionSource, 0, len(grants)*3)
	seen := map[string]struct{}{}
	for _, grant := range grants {
		for _, key := range []string{"active_goats", "active_sheds", "operators_by_scope"} {
			dedupe := key + ":" + grant.ScopeType + ":" + grant.ScopeID
			if _, ok := seen[dedupe]; ok {
				continue
			}
			seen[dedupe] = struct{}{}
			items = append(items, domain.BootstrapOptionSource{
				Key:        key,
				ScopeType:  grant.ScopeType,
				ScopeID:    grant.ScopeID,
				TTLSeconds: 900,
			})
		}
	}
	return items
}
