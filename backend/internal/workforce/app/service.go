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

func (s *Service) DeregisterDevice(ctx context.Context, cmd ports.DeregisterDeviceCommand, traceID string) (*domain.DeviceResponse, error) {
	if err := validateTenant(cmd.TenantID); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(cmd.ActorID) {
		return nil, BadRequest("invalid_actor", "an authenticated actor is required")
	}
	if !uuidutil.IsUUIDString(cmd.DeviceID) {
		return nil, BadRequest("invalid_device_id", "device_id must be a UUID")
	}
	item, err := s.repo.DeregisterDevice(ctx, cmd)
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
		healed, healErr := s.reactivateRecoverableDevice(ctx, cmd.TenantID, cmd.ActorID, item, domain.RegisterDeviceRequest{
			AppInstallID:  item.AppInstallID,
			AppVersion:    cmd.Body.AppVersion,
			OSVersion:     cmd.Body.OSVersion,
			PushTokenHash: cmd.Body.PushTokenHash,
			FcmToken:      cmd.Body.FcmToken,
		})
		if healErr != nil {
			return nil, healErr
		}
		item = healed
	}
	return &domain.DeviceResponse{Device: item, TraceID: traceID}, nil
}

// isAdministrativelyRevoked reports whether a non-active device was put there by a deliberate
// admin/security action (workforce RevokeDevice, which stamps metadata["revocation_reason"]) as
// opposed to a push-delivery side effect (notification SuppressInvalidRecipient, which stamps
// metadata["fcm_invalidated_reason"] and -- since the P0 device-lockout fix -- no longer even
// touches status). Only the administrative path is a terminal, non-recoverable state here.
func isAdministrativelyRevoked(item domain.DeviceSummary) bool {
	if item.Metadata == nil {
		return false
	}
	_, revokedByAdmin := item.Metadata["revocation_reason"]
	return revokedByAdmin
}

// reactivateRecoverableDevice self-heals a device that is not active but was never
// administratively revoked (see isAdministrativelyRevoked): a stale non-active row left over from
// before the SuppressInvalidRecipient fix (or any other push-side-effect deactivation), which
// today's SuppressInvalidRecipient no longer produces but which may already exist in the fleet.
// Recoverable state machine for workforce_member_devices.status:
//   - "active"            -> normal, nothing to do.
//   - "revoked" WITHOUT
//     metadata.revocation_reason -> RECOVERABLE. Never a deliberate admin action; self-heal here.
//   - "revoked" WITH
//     metadata.revocation_reason -> NOT RECOVERABLE. A human/security workflow (operators device
//     revoke) deliberately locked this device out; heartbeat/bootstrap must keep 403'ing it and
//     must NEVER silently reactivate it.
//   - "not_registered" (synthetic, no row yet) -> NOT handled here; the device must call
//     RegisterDevice first, which is unambiguous because there is no device row to relitigate.
//
// Reactivation is always performed via the same RegisterDevice upsert path a fresh install uses
// (keyed on (tenant_id, app_install_id), which uniquely identifies the row we already resolved as
// belonging to this device/actor), so it reuses the exact write path already trusted to create an
// 'active' row -- there is no second, bespoke "unlock" code path to audit. Critically, this
// function is only ever reached AFTER the caller has resolved item via a device lookup scoped to
// the AUTHENTICATED actor (GetDeviceForActor / activeProfileAndGrants), so reactivation requires
// the same principal that owns the device row -- never device id alone.
func (s *Service) reactivateRecoverableDevice(ctx context.Context, tenantID, actorID string, item domain.DeviceSummary, body domain.RegisterDeviceRequest) (domain.DeviceSummary, error) {
	if isAdministrativelyRevoked(item) {
		return domain.DeviceSummary{}, Forbidden("device_revoked", "device was administratively revoked")
	}
	if strings.TrimSpace(item.AppInstallID) == "" {
		// No row to re-key against (e.g. the synthetic not_registered summary) -- cannot self-heal.
		return domain.DeviceSummary{}, Forbidden("device_revoked", "device is not active")
	}
	body.AppInstallID = item.AppInstallID
	body.AppVersion = strings.TrimSpace(body.AppVersion)
	body.OSVersion = strings.TrimSpace(body.OSVersion)
	if body.AppVersion == "" {
		body.AppVersion = item.AppVersion
	}
	if body.OSVersion == "" {
		body.OSVersion = item.OSVersion
	}
	healed, err := s.repo.RegisterDevice(ctx, ports.RegisterDeviceCommand{
		TenantID: tenantID,
		ActorID:  actorID,
		Body:     body,
	})
	if err != nil {
		return domain.DeviceSummary{}, mapRepoErr(err)
	}
	return healed, nil
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
	// Module grants drive which modules appear in the drawer and which bottom bar is
	// served. A person with no department/grants gets an empty module set rather than
	// an implicit default, so nav reflects real authority.
	grantedModules, err := s.repo.ListGrantedModuleKeys(ctx, tenantID, actorID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	// PER-PERSON PHONE MODULES (maintainer decision 2026-08-27). The ticks decide the bar,
	// the same way they decide the web sidebar.
	//
	// Until this, the two halves of a phone module came from DIFFERENT places: the
	// permissions from the person's own rows, the BAR from department_module_grants. Removing
	// a module on the phone therefore took the ability away in 0.02s and left the icon in
	// place -- for ever. Not on a refresh, and not on a log out and log in, because nothing
	// was stale: the server kept answering "yes, he has it" from a source the tick never
	// touched. The operator tapped a module he still saw and the work failed.
	personAssignments, err := s.repo.ListPersonAssignments(ctx, tenantID, actorID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	personModules := make([]string, 0, len(personAssignments))
	for _, a := range personAssignments {
		if a.Surface == permissions.SurfaceMobile {
			personModules = append(personModules, a.Module)
		}
	}
	personPermissions := permissions.PermissionsForAssignmentsWithBaseline(personAssignments)
	// A STANDALONE VERIFIER IS EXEMPT. Her modules are the features her verify DUTIES name
	// (position_module_duties), not a department grant, and the whole verifier workspace is
	// composed from them. Feeding her ticks in here would recompose that workspace, and the
	// maintainer's instruction was that the verifier's separate interface does not change.
	// THE TICKS DECIDE, THROUGH THE PERMISSION FILTER -- not by replacing the module list.
	//
	// The phone's nav filter already drops a module whose every item is gated away; it was
	// simply asking the ROLE map. Asking the person's own resolved permissions instead means
	// an unticked module disappears on its own, while WHICH modules are offered stays exactly
	// as it is today. Replacing the offered list instead was measured against the real STG
	// roster and moved 31 bars; this moves none, and loses no permission.
	scope := scopeOf(grants)
	var tickedModules []string
	if len(personAssignments) > 0 && !isStandaloneVerifierPrincipal(grants) {
		held := make(map[string]struct{}, 64)
		for _, p := range personPermissions {
			held[p] = struct{}{}
		}
		scope.held = held
		// nil means "no stored rows"; an empty non-nil slice means "ticked for nothing",
		// and those are different answers.
		tickedModules = personModules
		if tickedModules == nil {
			tickedModules = []string{}
		}
	}
	fromTicks := false
	var device *domain.DeviceSummary
	deviceState := domain.BootstrapDeviceState{Required: true, Status: "not_registered"}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID != "" {
		item, err := s.repo.GetDeviceForActor(ctx, tenantID, actorID, deviceID)
		if err != nil {
			if errors.Is(err, ports.ErrNotFound) {
				item = domain.DeviceSummary{DeviceID: deviceID, Status: "not_registered"}
				device = &item
				deviceState = domain.BootstrapDeviceState{Required: true, Device: device, Status: item.Status}
			} else {
				return nil, mapRepoErr(err)
			}
		} else {
			if item.Status != "active" {
				healed, healErr := s.reactivateRecoverableDevice(ctx, tenantID, actorID, item, domain.RegisterDeviceRequest{})
				if healErr != nil {
					return nil, healErr
				}
				item = healed
			}
			device = &item
			deviceState = domain.BootstrapDeviceState{Required: true, Device: device, Status: item.Status}
		}
	}
	now := s.now().UTC()
	bootstrapModules := modulesForScope(scope, grantedModules, localeTag, fromTicks, tickedModules)
	navChrome := navChromeFor(grants, bootstrapModules)
	visibleNav := visibleNavigationForTicks(scope, grantedModules, localeTag, tickedModules)
	visibleNav, bootstrapModules = applyProfileEntryPlacement(navChrome, visibleNav, bootstrapModules)
	return &domain.BootstrapResponse{
		Actor:                  domain.BootstrapActor{ActorID: actorID, TenantID: tenantID},
		OperatorProfile:        profile,
		RolesAndScopes:         grants,
		Capabilities:           caps,
		DeviceState:            deviceState,
		AppMinSupportedVersion: "0.1.0",
		FeatureFlags: map[string]bool{
			"tasks":                       true,
			"sop_runner":                  true,
			"proof_capture":               hasCapability(caps, "media.video_capture"),
			"animal_id_scan":              hasCapability(caps, "animal_id.scan"),
			"protocol_adherence_card":     canViewProtocolAdherenceCard(grants),
			"vaccination_execute":         canExecuteVaccinationFrom(grants, grantedModules, fromTicks),
			"weighing_execute":            canExecuteWeighingFrom(grants, grantedModules, fromTicks),
			"weighing_oversee_operators":  canOverseeWeighingOperatorsFrom(grants, grantedModules, fromTicks),
			"pc_care_execute":             canExecutePCCareFrom(grants, grantedModules, fromTicks),
			"pc_care_plan":                canPlanPCCareFrom(grants, grantedModules, fromTicks),
			"verification_video_controls": canUseVerificationVideoControls(grants),
		},
		VisibleNavigation:       visibleNav,
		Modules:                 bootstrapModules,
		NavChrome:               navChrome,
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
	if errors.Is(err, ports.ErrIdempotencyInFlight) {
		return &Error{Code: "idempotency_in_flight", Message: "the same request is already being processed; retry shortly", HTTPStatus: 409, Retryable: true}
	}
	if errors.Is(err, ports.ErrInvalidFilter) {
		return BadRequest("invalid_filter", "one or more filters are invalid")
	}
	if errors.Is(err, ports.ErrDenied) {
		return Forbidden("operator_gate_denied", "operator gate denied")
	}
	if errors.Is(err, ports.ErrMinOperatorCoverage) {
		return Conflict("min_operator_coverage", err.Error())
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
	case "operator", "park_head", "pc_director", "growth_director", "feed_director", "health_director", "verifier", "supervisor", "cxo", "other":
		return true
	default:
		return false
	}
}

func validRole(value string) bool {
	switch value {
	case "admin", "park_head", "pc_director", "growth_director", "feed_director", "health_director", "operator", "verifier", "ceo_internal":
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

// leadershipGrantRoles are the workforce grant roles that get the curated
// mobile module set (for example Vaccination plus CEO modules). Mirrors the role-lens
// tiers already used for the admin-web bootstrap (see roleLensForRole in
// internal/adminui/app/compiler.go): ceo_internal is CEO/CXO, pc_director is
// the health director and park_head is the park manager.
// Verifier is deliberately excluded: it owns a standalone evidence-review app,
// not leadership action navigation. permissions.RoleOperator is also not leadership.
var leadershipGrantRoles = map[string]bool{
	permissions.RoleCEOInternal:    true,
	permissions.RolePCDirector:     true,
	permissions.RoleGrowthDirector: true,
	// Live as of the 2026-08-01 module-ownership decision: feed_director owns Feed and
	// health_director owns Counts, so both are leadership for nav composition the same way
	// pc_director and growth_director are. Their module set still comes from their granted
	// permissions, not from this map.
	permissions.RoleFeedDirector:   true,
	permissions.RoleHealthDirector: true,
	permissions.RoleParkHead:       true,
}

func isVerifierPrincipal(grants []domain.GrantSummary) bool {
	for _, g := range grants {
		if g.Role == permissions.RoleVerifier {
			return true
		}
	}
	return false
}

func isStandaloneVerifierPrincipal(grants []domain.GrantSummary) bool {
	return isVerifierPrincipal(grants) && !isLeadershipPrincipal(grants)
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

// nav-composition rule "1 available module -> clean bottom bar, >=2 -> module
// switcher drawer". This applies uniformly to field operators and leadership:
// a preventive-care leader whose only drawer entry is the vaccination home, or
// an operator granted a single module, gets the clean bottom bar; a CEO with
// vaccination + counts, or an OPERATOR granted vaccination + counts + feed,
// gets the expanded drawer so they can switch between their modules.
//
// Maintainer decision 2026-07-27: the operator module-switcher drawer rollout is
// ENABLED (it was previously pinned to minimal). Operators with >=2 granted
// modules now get the same >=2->drawer treatment as leadership. The verifier
// workspace follows the same rule: its per-feature evidence modules are
// backend-composed drawer entries like any other module.

// navItemKeyYou is the account destination's stable key in the nav registry.
const navItemKeyYou = "you"

// applyProfileEntryPlacement is THE decision about where the account entry ("You") lives,
// made once for every principal. MAINTAINER RULING 2026-08-03, stated three times:
//
//	"You option should be on navigation bar for CEO and verifier and whoever got >=2
//	 features, instead of sending that in bottom bar for every feature."
//
// "You" is the person, not a feature, so it must appear exactly ONCE -- not once per
// module the principal happens to hold.
//
//   - >=2 modules  -> navChrome is "expanded", which means the client renders the module
//     drawer. The drawer footer owns the account row (GoatOsShell.kt DrawerFooter,
//     beside Sign out), so the entry is stripped from the served bar AND from every
//     module's own bar. Stripping the per-module bars too is what makes switching
//     modules in the drawer unable to resurrect a second You.
//   - exactly 1 module -> navChrome is "minimal", there is no drawer, and the bottom bar
//     is the ONLY route to /you. The entry stays.
//
// The >=2 test is not re-derived here: it is navChromeFor's count of AVAILABLE composed
// modules, the same threshold docs/decisions/role-module-nav-composition.md uses to
// decide the drawer exists at all. Keying off chrome instead of a second count is
// deliberate -- the drawer's existence and the account entry's home cannot drift apart.
//
// There is NO role exception, and specifically no verifier exception. A verifier who
// verifies vaccination AND weighing has a drawer like anyone else with two features; the
// carve-out that used to sit here is exactly what put You in both the drawer footer and
// the bottom bar of every verify feature.
func applyProfileEntryPlacement(navChrome string, visibleNav []domain.BootstrapNavigationItem, modules []domain.BootstrapModule) ([]domain.BootstrapNavigationItem, []domain.BootstrapModule) {
	if navChrome != domain.NavChromeExpanded {
		return visibleNav, modules
	}
	visibleNav = withoutNavItem(visibleNav, navItemKeyYou)
	for i := range modules {
		modules[i].NavItems = withoutNavItem(modules[i].NavItems, navItemKeyYou)
	}
	return visibleNav, modules
}

// withoutNavItem drops one contribution from a composed bar.
func withoutNavItem(items []domain.BootstrapNavigationItem, key string) []domain.BootstrapNavigationItem {
	out := make([]domain.BootstrapNavigationItem, 0, len(items))
	for _, item := range items {
		if item.Key == key {
			continue
		}
		out = append(out, item)
	}
	return out
}

func navChromeFor(grants []domain.GrantSummary, modules []domain.BootstrapModule) string {
	// Verifiers with ≥2 feature modules get expanded chrome (drawer + You + Sign out).
	// Single-module verifiers use minimal chrome (bottom bar only).
	// This follows the same pattern as operators/leadership: ≥2 modules → drawer.
	if isStandaloneVerifierPrincipal(grants) {
		available := 0
		for _, m := range modules {
			if m.Status == moduleStatusAvailable {
				available++
			}
		}
		if available >= 2 {
			return domain.NavChromeExpanded
		}
		return domain.NavChromeMinimal
	}
	available := 0
	for _, m := range modules {
		if m.Status == moduleStatusAvailable {
			available++
		}
	}
	if available >= 2 {
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
