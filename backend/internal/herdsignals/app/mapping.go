package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// The mapping WRITES: bind a BLE tag to an animal, mark/unmark an existing identifier as
// smart-tag capable, and replace a mapping (re-tagging) in one transaction.
//
// This layer validates the caller's input and hands the transactional work to the repository --
// the whole point of a mapping write is that the identifier rows and the denormalised monitoring
// boundary on the hot read path commit together, which is a single-transaction property only the
// adapter can hold.

var uuidLike = func(v string) bool {
	v = strings.TrimSpace(v)
	if len(v) != 36 {
		return false
	}
	for i, c := range v {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// BindTagMapping binds a BLE tag to an animal identifier.
func (s *Service) BindTagMapping(ctx context.Context, actor domain.Actor, req domain.BindTagMappingRequest) (domain.TagMappingResponse, error) {
	if actor.TenantID == "" || actor.UserID == "" {
		return domain.TagMappingResponse{}, fmt.Errorf("actor tenant_id and user_id required")
	}
	req.GoatID = strings.TrimSpace(req.GoatID)
	if !uuidLike(req.GoatID) {
		return domain.TagMappingResponse{}, fmt.Errorf("goat_id must be a valid UUID: %w", domain.ErrValidation)
	}
	if strings.TrimSpace(req.TagID) == "" {
		return domain.TagMappingResponse{}, fmt.Errorf("tag_id is required: %w", domain.ErrValidation)
	}
	resp, err := s.repo.BindTagMapping(ctx, actor.TenantID, req)
	if err != nil {
		s.log.Warn("bind_tag_mapping_failed", "goat_id", req.GoatID, "tag_id", req.TagID, "error", err)
		return domain.TagMappingResponse{}, err
	}
	s.log.Info("herd_signals_tag_mapping_bound", "goat_id", resp.GoatID, "tag_id", resp.TagID,
		"identifier_ids", resp.IdentifierIDs, "monitoring_since", resp.MonitoringSince, "actor_id", actor.UserID)
	return resp, nil
}

// SetSmartTagCapable marks or unmarks an existing identifier as smart-tag capable.
func (s *Service) SetSmartTagCapable(ctx context.Context, actor domain.Actor, identifierID string, req domain.SetSmartTagCapableRequest) (domain.TagMappingResponse, error) {
	if actor.TenantID == "" || actor.UserID == "" {
		return domain.TagMappingResponse{}, fmt.Errorf("actor tenant_id and user_id required")
	}
	identifierID = strings.TrimSpace(identifierID)
	if !uuidLike(identifierID) {
		return domain.TagMappingResponse{}, fmt.Errorf("identifier_id must be a valid UUID: %w", domain.ErrValidation)
	}
	resp, err := s.repo.SetSmartTagCapable(ctx, actor.TenantID, identifierID, req.SmartTagCapable)
	if err != nil {
		s.log.Warn("set_smart_tag_capable_failed", "identifier_id", identifierID, "capable", req.SmartTagCapable, "error", err)
		return domain.TagMappingResponse{}, err
	}
	s.log.Info("herd_signals_smart_tag_flag_set", "identifier_id", identifierID, "capable", req.SmartTagCapable,
		"monitoring_since", resp.MonitoringSince, "actor_id", actor.UserID)
	return resp, nil
}

// ReplaceTagMapping unbinds the animal's current smart tag and binds a new one, atomically.
func (s *Service) ReplaceTagMapping(ctx context.Context, actor domain.Actor, req domain.ReplaceTagMappingRequest) (domain.TagMappingResponse, error) {
	if actor.TenantID == "" || actor.UserID == "" {
		return domain.TagMappingResponse{}, fmt.Errorf("actor tenant_id and user_id required")
	}
	req.GoatID = strings.TrimSpace(req.GoatID)
	if !uuidLike(req.GoatID) {
		return domain.TagMappingResponse{}, fmt.Errorf("goat_id must be a valid UUID: %w", domain.ErrValidation)
	}
	if strings.TrimSpace(req.NewTagID) == "" {
		return domain.TagMappingResponse{}, fmt.Errorf("new_tag_id is required: %w", domain.ErrValidation)
	}
	resp, err := s.repo.ReplaceTagMapping(ctx, actor.TenantID, req)
	if err != nil {
		s.log.Warn("replace_tag_mapping_failed", "goat_id", req.GoatID, "new_tag_id", req.NewTagID, "error", err)
		return domain.TagMappingResponse{}, err
	}
	s.log.Info("herd_signals_tag_mapping_replaced", "goat_id", resp.GoatID, "new_tag_id", resp.TagID,
		"unbound_identifier_ids", resp.UnboundIdentifierIDs, "monitoring_since", resp.MonitoringSince, "actor_id", actor.UserID)
	return resp, nil
}

// RecordGatewayHeartbeat records a sta_gw_hb gateway heartbeat.
//
// The timestamp is the SERVER clock, stamped here -- never the caller's. That is the same rule
// received_at follows everywhere else in this module and for the same reason: a far-future
// caller-supplied value would freeze the gateway's freshness judgement permanently.
func (s *Service) RecordGatewayHeartbeat(ctx context.Context, actor domain.Actor, req domain.GatewayHeartbeatRequest) (domain.GatewayHeartbeatResponse, error) {
	if actor.TenantID == "" || actor.UserID == "" {
		return domain.GatewayHeartbeatResponse{}, fmt.Errorf("actor tenant_id and user_id required")
	}
	req.GatewayID = strings.TrimSpace(req.GatewayID)
	if req.GatewayID == "" {
		return domain.GatewayHeartbeatResponse{}, fmt.Errorf("gateway_id is required: %w", domain.ErrValidation)
	}
	// A state name we do not recognise is refused rather than silently counted as proof of life:
	// the whole value of a heartbeat is that it means "this gateway is alive right now", and a
	// future firmware state we have not read must not be allowed to assert that by accident.
	if req.State != "" && req.State != domain.GatewayHeartbeatState {
		return domain.GatewayHeartbeatResponse{}, fmt.Errorf("state %q is not a gateway heartbeat (expected %q): %w", req.State, domain.GatewayHeartbeatState, domain.ErrValidation)
	}
	at := time.Now().UTC()
	rebooted, err := s.repo.RecordGatewayHeartbeat(ctx, actor.TenantID, req, at)
	if err != nil {
		s.log.Error("record_gateway_heartbeat_failed", "gateway_id", req.GatewayID, "error", err)
		return domain.GatewayHeartbeatResponse{}, err
	}
	if rebooted {
		// ticks_cnt went backwards. Same counter-reset discipline as motion_count and pkt_sn:
		// re-anchor and record the reboot, never a negative.
		s.log.Warn("gateway_ticks_cnt_decreased_likely_reboot", "gateway_id", req.GatewayID, "ticks_cnt", req.TicksCnt)
	}
	return domain.GatewayHeartbeatResponse{
		GatewayID:       req.GatewayID,
		LastHeartbeatAt: at,
		RebootDetected:  rebooted,
		TraceID:         httpmiddleware.TraceIDFromContext(ctx),
	}, nil
}
