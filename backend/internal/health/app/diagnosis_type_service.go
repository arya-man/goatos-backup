package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
)

// DiagnosisTypeService is the ROUTING half of Health Config: which diagnosis types exist and
// which animals reach each one (migration 000395).
//
// It is thin on purpose. The rules that matter here are either in the domain commands
// (shape, the built-in retire refusal) or in the repository transaction (key immutability,
// the still-routed check, the active-type check), because both of those are places a second
// client cannot route around. Putting them here as well would give three answers to maintain.
type DiagnosisTypeService struct {
	repo ports.DiagnosisTypeAuthoring
}

func NewDiagnosisTypeService(repo ports.DiagnosisTypeAuthoring) *DiagnosisTypeService {
	return &DiagnosisTypeService{repo: repo}
}

// Routing reads the whole Types screen: the types, their routes, and the stages that hold
// animals and reach nothing.
func (s *DiagnosisTypeService) Routing(ctx context.Context, tenantID string) (domain.DiagnosisRoutingView, error) {
	if !validUUID(tenantID) {
		return domain.DiagnosisRoutingView{}, ErrInvalidInput
	}
	return s.repo.DiagnosisRouting(ctx, tenantID)
}

// SaveType creates a type or relabels/retires one.
func (s *DiagnosisTypeService) SaveType(ctx context.Context, cmd domain.SaveDiagnosisTypeCommand) (domain.DiagnosisType, error) {
	if !validUUID(cmd.TenantID) {
		return domain.DiagnosisType{}, ErrInvalidInput
	}
	return s.repo.SaveDiagnosisType(ctx, cmd)
}

// SaveRoute points one stage, or a whole age band, at a type.
func (s *DiagnosisTypeService) SaveRoute(ctx context.Context, cmd domain.SaveStageRouteCommand) (domain.StageRouteRow, error) {
	if !validUUID(cmd.TenantID) {
		return domain.StageRouteRow{}, ErrInvalidInput
	}
	return s.repo.SaveStageRoute(ctx, cmd)
}

// DeleteRoute removes one route.
func (s *DiagnosisTypeService) DeleteRoute(ctx context.Context, cmd domain.DeleteStageRouteCommand) error {
	if !validUUID(cmd.TenantID) {
		return ErrInvalidInput
	}
	return s.repo.DeleteStageRoute(ctx, cmd)
}
