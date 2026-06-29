// Package app coordinates Counts/Shifting projection use-cases.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

var (
	ErrMissingRequiredField = errors.New("counts: missing required field")
	ErrInvalidCount         = errors.New("counts: invalid count")
	ErrInvalidJSON          = errors.New("counts: invalid json object")
	ErrMissingImpact        = errors.New("counts: shifting event requires structured impact")
)

type Service struct {
	repo ports.Repository
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) RecordBaseCountAnchor(ctx context.Context, in domain.BaseCountAnchor) (string, bool, error) {
	in.SourceSystem = defaultString(in.SourceSystem, "physical_base_count")
	in.DiscrepancyState = defaultString(in.DiscrepancyState, "not_checked")
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ParkID) == "" ||
		strings.TrimSpace(in.ShedID) == "" || strings.TrimSpace(in.BreedKey) == "" ||
		strings.TrimSpace(in.BreedLabel) == "" || in.CountedAt.IsZero() ||
		strings.TrimSpace(in.SourceRef) == "" || strings.TrimSpace(in.SourceHash) == "" ||
		strings.TrimSpace(in.IdempotencyKey) == "" || strings.TrimSpace(in.RequestFingerprint) == "" {
		return "", false, ErrMissingRequiredField
	}
	if in.HeadCount < 0 {
		return "", false, ErrInvalidCount
	}
	return s.repo.RecordBaseCountAnchor(ctx, in)
}

func (s *Service) RecordShiftingEvent(ctx context.Context, in domain.ShiftingEvent) (string, bool, error) {
	in.Priority = defaultString(in.Priority, "normal")
	in.Category = defaultString(in.Category, "routine")
	in.AuthorizationState = defaultString(in.AuthorizationState, "pending")
	in.VerificationState = defaultString(in.VerificationState, "unverified")
	in.EventStatus = defaultString(in.EventStatus, "pending")
	in.SourceSystem = defaultString(in.SourceSystem, "manual_review")
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.LogicalShiftingEventKey) == "" ||
		strings.TrimSpace(in.DestinationParkID) == "" || strings.TrimSpace(in.DestinationShedID) == "" ||
		in.RaisedAt.IsZero() || in.EffectiveAt.IsZero() || strings.TrimSpace(in.SourceRef) == "" ||
		strings.TrimSpace(in.PayloadHash) == "" || strings.TrimSpace(in.IdempotencyKey) == "" ||
		strings.TrimSpace(in.RequestFingerprint) == "" {
		return "", false, ErrMissingRequiredField
	}
	if len(in.Impacts) == 0 {
		return "", false, ErrMissingImpact
	}
	for _, impact := range in.Impacts {
		if strings.TrimSpace(impact.GrainKey) == "" || strings.TrimSpace(impact.BreedKey) == "" ||
			strings.TrimSpace(impact.BreedLabel) == "" {
			return "", false, ErrMissingRequiredField
		}
		if impact.HeadCount <= 0 || impact.PregnantCount < 0 || impact.LactatingCount < 0 || impact.WarmupCount < 0 ||
			impact.PregnantCount > impact.HeadCount || impact.LactatingCount > impact.HeadCount || impact.WarmupCount > impact.HeadCount {
			return "", false, ErrInvalidCount
		}
		if !isJSONObject(impact.RiskFlagsJSON) {
			return "", false, ErrInvalidJSON
		}
	}
	return s.repo.RecordShiftingEvent(ctx, in)
}

func (s *Service) CreateProjectionSnapshot(ctx context.Context, in domain.ProjectionSnapshot) (string, error) {
	in.Horizon = defaultString(in.Horizon, "feed_target_date")
	in.ProjectionStatus = defaultString(in.ProjectionStatus, "blocked")
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ParkID) == "" ||
		in.TargetDate.IsZero() || in.AsOf.IsZero() || strings.TrimSpace(in.SourceContractVersion) == "" ||
		strings.TrimSpace(in.SourceHash) == "" || strings.TrimSpace(in.BaseAnchorIDsHash) == "" ||
		strings.TrimSpace(in.ShiftingEventIDsHash) == "" || strings.TrimSpace(in.GeneratedBy) == "" {
		return "", ErrMissingRequiredField
	}
	for _, row := range in.Rows {
		if strings.TrimSpace(row.ParkID) == "" || strings.TrimSpace(row.ShedID) == "" ||
			strings.TrimSpace(row.GrainKey) == "" || strings.TrimSpace(row.BreedKey) == "" ||
			strings.TrimSpace(row.BreedLabel) == "" || strings.TrimSpace(row.SourceRowHash) == "" {
			return "", ErrMissingRequiredField
		}
		if row.HeadCount < 0 || row.PregnantCount < 0 || row.LactatingCount < 0 || row.WarmupCount < 0 ||
			row.PregnantCount > row.HeadCount || row.LactatingCount > row.HeadCount || row.WarmupCount > row.HeadCount {
			return "", ErrInvalidCount
		}
	}
	for _, exception := range in.Exceptions {
		if strings.TrimSpace(exception.ExceptionType) == "" || strings.TrimSpace(exception.SourceKey) == "" ||
			strings.TrimSpace(exception.GrainKey) == "" || strings.TrimSpace(exception.BlockerReason) == "" {
			return "", ErrMissingRequiredField
		}
		if !isJSONObject(exception.EvidenceJSON) {
			return "", ErrInvalidJSON
		}
	}
	return s.repo.CreateProjectionSnapshot(ctx, in)
}

func (s *Service) Readiness(ctx context.Context, tenantID string) (domain.Readiness, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.Readiness{}, ErrMissingRequiredField
	}
	return s.repo.Readiness(ctx, tenantID)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func isJSONObject(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	var obj map[string]any
	return json.Unmarshal(raw, &obj) == nil
}
