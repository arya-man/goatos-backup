package app

import (
	"context"
	"errors"
	"strings"

	"github.com/vgoats/goatos/backend/internal/locations/domain"
	"github.com/vgoats/goatos/backend/internal/locations/ports"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

type Service struct {
	repo ports.Repository
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListLocations(ctx context.Context, params ports.ListParams, traceID string) (*domain.LocationListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.LocationType = strings.TrimSpace(params.LocationType)
	params.Status = strings.TrimSpace(params.Status)
	params.ParentLocationID = strings.TrimSpace(params.ParentLocationID)
	if params.ParentLocationID != "" && !uuidutil.IsUUIDString(params.ParentLocationID) {
		return nil, BadRequest("invalid_parent_location_id", "parent_location_id must be a UUID")
	}
	params.Limit = boundedLimit(params.Limit, 100)
	params.Offset = boundedOffset(params.Offset)
	items, err := s.repo.ListLocations(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.LocationListResponse{Items: items, TraceID: traceID}, nil
}

func (s *Service) GetLocation(ctx context.Context, tenantID, locationID, traceID string) (*domain.LocationResponse, error) {
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(strings.TrimSpace(locationID)) {
		return nil, BadRequest("invalid_location_id", "location_id must be a UUID")
	}
	item, err := s.repo.GetLocation(ctx, tenantID, locationID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.LocationResponse{Location: item, TraceID: traceID}, nil
}

func (s *Service) ListChildren(ctx context.Context, tenantID, locationID string, limit int, traceID string) (*domain.LocationListResponse, error) {
	if err := validateTenantAndLocation(tenantID, locationID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListChildren(ctx, tenantID, locationID, boundedLimit(limit, 200))
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.LocationListResponse{Items: items, TraceID: traceID}, nil
}

func (s *Service) ListAliases(ctx context.Context, tenantID, locationID string, limit int, traceID string) (*domain.LocationAliasListResponse, error) {
	if err := validateTenantAndLocation(tenantID, locationID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListAliases(ctx, tenantID, locationID, boundedLimit(limit, 200))
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.LocationAliasListResponse{Items: items, TraceID: traceID}, nil
}

func (s *Service) ListCapacity(ctx context.Context, tenantID, locationID string, limit int, traceID string) (*domain.LocationCapacityListResponse, error) {
	if err := validateTenantAndLocation(tenantID, locationID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListCapacity(ctx, tenantID, locationID, boundedLimit(limit, 200))
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.LocationCapacityListResponse{Items: items, TraceID: traceID}, nil
}

func (s *Service) ListReviewItems(ctx context.Context, params ports.ReviewParams, traceID string) (*domain.LocationReviewListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.Limit = boundedLimit(params.Limit, 100)
	items, err := s.repo.ListReviewItems(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.LocationReviewListResponse{Items: items, TraceID: traceID}, nil
}

func (s *Service) Usage(ctx context.Context, tenantID, locationID, traceID string) (*domain.LocationUsageResponse, error) {
	if err := validateTenantAndLocation(tenantID, locationID); err != nil {
		return nil, err
	}
	usage, err := s.repo.Usage(ctx, tenantID, locationID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	usage.TraceID = traceID
	return &usage, nil
}

func validateTenant(tenantID string) error {
	if !uuidutil.IsUUIDString(strings.TrimSpace(tenantID)) {
		return Unauthorized("missing_tenant_scope", "tenant scope is required")
	}
	return nil
}

func validateTenantAndLocation(tenantID, locationID string) error {
	if err := validateTenant(tenantID); err != nil {
		return err
	}
	if !uuidutil.IsUUIDString(strings.TrimSpace(locationID)) {
		return BadRequest("invalid_location_id", "location_id must be a UUID")
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

func boundedOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	if offset > 1000000 {
		return 1000000
	}
	return offset
}

func mapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ports.ErrInvalidFilter) {
		return BadRequest("invalid_filter", "one or more Locations filters are invalid")
	}
	if errors.Is(err, ports.ErrNotFound) {
		return NotFound("location is missing or outside tenant scope")
	}
	if errors.Is(err, ports.ErrIdempotencyConflict) {
		return Conflict("idempotency_conflict", "Idempotency-Key was reused with a different request")
	}
	if errors.Is(err, ports.ErrIdempotencyPending) {
		return Conflict("idempotency_pending", "Idempotency-Key is still pending")
	}
	if errors.Is(err, ports.ErrWriteConflict) {
		return Conflict("write_conflict", "location write conflicted with current state")
	}
	if errors.Is(err, ports.ErrBlockingUsage) {
		return Conflict("location_has_blocking_usage", "location has blocking usage and cannot be retired or deleted")
	}
	return Internal("locations repository operation failed")
}
