package app

import (
	"context"
	"strings"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

type Service struct {
	repo ports.Repository
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateCampaign(ctx context.Context, actor domain.Actor, cmd domain.CreateCampaign) (domain.Campaign, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingPlan}, false) {
		return domain.Campaign{}, ports.ErrForbidden
	}
	cmd.TenantID = actor.TenantID
	cmd.CreatedBy = actor.UserID
	if err := validateCreate(cmd); err != nil {
		return domain.Campaign{}, err
	}
	return s.repo.CreateCampaign(ctx, cmd)
}

func (s *Service) PublishCampaign(ctx context.Context, actor domain.Actor, campaignID, idempotencyKey string) (domain.Campaign, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingPlan}, false) {
		return domain.Campaign{}, ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) || strings.TrimSpace(idempotencyKey) == "" {
		return domain.Campaign{}, ports.ErrInvalidArgument
	}
	return s.repo.PublishCampaign(ctx, actor.TenantID, campaignID, actor.UserID, idempotencyKey)
}

func (s *Service) ListCampaigns(ctx context.Context, actor domain.Actor) ([]domain.Campaign, error) {
	canMonitor := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false)
	canExecute := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false)
	if !canMonitor && !canExecute {
		return nil, ports.ErrForbidden
	}
	return s.repo.ListCampaigns(ctx, actor.TenantID)
}

func (s *Service) ListScopeRoster(ctx context.Context, actor domain.Actor, campaignID, campaignShedID string, limit int) ([]domain.ExpectedAnimal, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false) {
		return nil, ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) || !uuidutil.IsUUIDString(campaignShedID) {
		return nil, ports.ErrInvalidArgument
	}
	if limit <= 0 {
		limit = 250
	}
	if limit > 5000 {
		limit = 5000
	}
	return s.repo.ListScopeRoster(ctx, actor.TenantID, campaignID, campaignShedID, limit)
}

func (s *Service) RecordAnimalObservation(ctx context.Context, actor domain.Actor, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false) {
		return domain.Observation{}, ports.ErrForbidden
	}
	cmd.TenantID = actor.TenantID
	cmd.RecordedBy = actor.UserID
	if !uuidutil.IsUUIDString(cmd.CampaignID) || !uuidutil.IsUUIDString(cmd.AnimalID) || !uuidutil.IsUUIDString(cmd.ProofArtifactID) || cmd.WeightKg <= 0 || strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return domain.Observation{}, ports.ErrInvalidArgument
	}
	return s.repo.RecordAnimalObservation(ctx, cmd)
}

func (s *Service) RecordShedObservation(ctx context.Context, actor domain.Actor, cmd domain.RecordShedObservation) (domain.Observation, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false) {
		return domain.Observation{}, ports.ErrForbidden
	}
	cmd.TenantID = actor.TenantID
	cmd.RecordedBy = actor.UserID
	if !uuidutil.IsUUIDString(cmd.CampaignID) || !uuidutil.IsUUIDString(cmd.CampaignShedID) || !uuidutil.IsUUIDString(cmd.ProofArtifactID) || cmd.WeightKg <= 0 || strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return domain.Observation{}, ports.ErrInvalidArgument
	}
	return s.repo.RecordShedObservation(ctx, cmd)
}

func validateCreate(cmd domain.CreateCampaign) error {
	if !uuidutil.IsUUIDString(cmd.TenantID) || !uuidutil.IsUUIDString(cmd.ParkID) || !uuidutil.IsUUIDString(cmd.OperatorUserID) || strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return ports.ErrInvalidArgument
	}
	if cmd.PeriodStartDate == "" || cmd.PeriodEndDate == "" || cmd.StartBusinessDate == "" || len(cmd.Sheds) == 0 {
		return ports.ErrInvalidArgument
	}
	if cmd.PlannedCapPerDay <= 0 {
		cmd.PlannedCapPerDay = 100
	}
	for _, shed := range cmd.Sheds {
		if !uuidutil.IsUUIDString(shed.LocationID) || strings.TrimSpace(shed.DisplayName) == "" {
			return ports.ErrInvalidArgument
		}
		switch shed.WeighingCategory {
		case domain.CategoryIndividualAnimal, domain.CategoryPerShedPartition:
		default:
			return ports.ErrInvalidArgument
		}
	}
	return nil
}
