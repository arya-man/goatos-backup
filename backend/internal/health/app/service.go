package app

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"strings"
	"time"
)

var (
	ErrInvalidInput = errors.New("health: invalid input")
	ErrInvalidDate  = errors.New("health: invalid date")
)

type Service struct{ repo ports.Repository }

func NewService(repo ports.Repository) *Service { return &Service{repo: repo} }

func (s *Service) OpenCase(ctx context.Context, in domain.OpenCaseInput) (domain.OpenCaseResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.GoatID = strings.TrimSpace(in.GoatID)
	in.DiseaseKey = strings.ToLower(strings.TrimSpace(in.DiseaseKey))
	in.AgeBand = strings.ToLower(strings.TrimSpace(in.AgeBand))
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if !validUUID(in.TenantID) || !validUUID(in.ActorID) || !validUUID(in.GoatID) ||
		in.DiseaseKey == "" || !validAgeBand(in.AgeBand) || in.IdempotencyKey == "" || in.StartDate.IsZero() {
		return domain.OpenCaseResult{}, ErrInvalidInput
	}
	return s.repo.OpenCase(ctx, in)
}
func (s *Service) ListWorkItems(ctx context.Context, f domain.ListFilter) (domain.WorkItemPage, error) {
	f.TenantID = strings.TrimSpace(f.TenantID)
	f.AgeBand = strings.ToLower(strings.TrimSpace(f.AgeBand))
	f.Status = strings.ToLower(strings.TrimSpace(f.Status))
	f.DiseaseKey = strings.ToLower(strings.TrimSpace(f.DiseaseKey))
	f.ParkID = strings.TrimSpace(f.ParkID)
	f.ShedID = strings.TrimSpace(f.ShedID)
	f.Session = strings.ToLower(strings.TrimSpace(f.Session))
	if !validUUID(f.TenantID) || !validAgeBand(f.AgeBand) {
		return domain.WorkItemPage{}, ErrInvalidInput
	}
	if _, err := time.Parse("2006-01-02", f.Date); err != nil {
		return domain.WorkItemPage{}, ErrInvalidDate
	}
	if f.Status == "held" {
		f.Status = "held_death_review"
	}
	if f.Status != "" && !validWorkStatus(f.Status) {
		return domain.WorkItemPage{}, ErrInvalidInput
	}
	if (f.ParkID != "" && !validUUID(f.ParkID)) || (f.ShedID != "" && !validUUID(f.ShedID)) || !validSessionFilter(f.Session) {
		return domain.WorkItemPage{}, ErrInvalidInput
	}
	if f.Limit <= 0 || f.Limit > domain.MaxPageSize {
		f.Limit = domain.MaxPageSize
	}
	return s.repo.ListWorkItems(ctx, f)
}
func (s *Service) GetWorkItem(ctx context.Context, tenantID, sessionID string) (domain.WorkItemDetail, error) {
	if !validUUID(strings.TrimSpace(tenantID)) || !validUUID(strings.TrimSpace(sessionID)) {
		return domain.WorkItemDetail{}, ErrInvalidInput
	}
	return s.repo.GetWorkItem(ctx, tenantID, sessionID)
}
func (s *Service) CompleteWorkItem(ctx context.Context, in domain.CompleteInput) (domain.CompleteResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.SessionID = strings.TrimSpace(in.SessionID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if !validUUID(in.TenantID) || !validUUID(in.ActorID) || !validUUID(in.SessionID) || in.IdempotencyKey == "" {
		return domain.CompleteResult{}, ErrInvalidInput
	}
	return s.repo.CompleteWorkItem(ctx, in)
}
func validAgeBand(v string) bool { return v == domain.AgeBandAdult || v == domain.AgeBandKid }
func validUUID(v string) bool    { _, err := uuid.Parse(v); return err == nil }
func validWorkStatus(v string) bool {
	switch v {
	case "scheduled", "due", "in_progress", "completed", "rework", "held_death_review", "canceled_death":
		return true
	default:
		return false
	}
}
func validSessionFilter(v string) bool {
	switch v {
	case "", domain.SessionMorning, domain.SessionAfternoon, domain.SessionEvening, domain.SessionUnscheduled:
		return true
	default:
		return false
	}
}
