// Package postgres implements the protocol Repository over generated sqlc queries.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	protocoldb "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

const defaultQueryTimeout = 3 * time.Second

// configListLimit bounds the Config authority list. Protocol versions per tenant/category are
// inherently small (dozens), so a fixed cap is safe at million-goat scale and needs no cursor.
const configListLimit = 500

// animalStageListLimit bounds the animal-stage reference read. A tenant has a handful of stage
// bands (K0/K1/K2/…), so a small fixed cap is safe and needs no cursor.
const animalStageListLimit = 200

// Repository is the Postgres-backed protocol repository.
type Repository struct {
	pool         *pgxpool.Pool
	queries      *protocoldb.Queries
	queryTimeout time.Duration
}

// NewRepository builds a Repository bound to a pgx pool.
func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, queries: protocoldb.New(pool), queryTimeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.queryTimeout)
}

// Ping checks pool connectivity.
func (r *Repository) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.pool.Ping(ctx)
}

// CreateDefinition inserts a protocol definition.
func (r *Repository) CreateDefinition(ctx context.Context, in domain.NewDefinition) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", fmt.Errorf("protocol: tenant id: %w", err)
	}
	id, err := r.queries.CreateProtocolDefinition(ctx, protocoldb.CreateProtocolDefinitionParams{
		TenantID:  tenant,
		Code:      in.Code,
		Name:      in.Name,
		Category:  in.Category,
		Status:    in.Status,
		CreatedBy: pgconv.NullableUUID(in.CreatedBy),
	})
	if err != nil {
		return "", fmt.Errorf("protocol: create definition: %w", err)
	}
	return id, nil
}

// GetDefinitionByCode fetches a definition by its code within a tenant.
func (r *Repository) GetDefinitionByCode(ctx context.Context, tenantID, code string) (domain.Definition, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.Definition{}, fmt.Errorf("protocol: tenant id: %w", err)
	}
	row, err := r.queries.GetProtocolDefinitionByCode(ctx, protocoldb.GetProtocolDefinitionByCodeParams{TenantID: tenant, Code: code})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Definition{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Definition{}, fmt.Errorf("protocol: get definition: %w", err)
	}
	return domain.Definition{
		ProtocolID: row.ProtocolID,
		Code:       row.Code,
		Name:       row.Name,
		Category:   row.Category,
		Status:     row.Status,
		RowVersion: row.RowVersion,
	}, nil
}

// CreateVersion inserts a protocol version.
func (r *Repository) CreateVersion(ctx context.Context, in domain.NewVersion) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", fmt.Errorf("protocol: tenant id: %w", err)
	}
	protocol, err := pgconv.UUID(in.ProtocolID)
	if err != nil {
		return "", fmt.Errorf("protocol: protocol id: %w", err)
	}
	id, err := r.queries.CreateProtocolVersion(ctx, protocoldb.CreateProtocolVersionParams{
		TenantID:      tenant,
		ProtocolID:    protocol,
		ScopeType:     in.ScopeType,
		ScopeID:       pgconv.NullableUUID(in.ScopeID),
		Version:       in.Version,
		VersionLabel:  in.VersionLabel,
		Status:        in.Status,
		EffectiveFrom: pgconv.Date(&in.EffectiveFrom),
		EffectiveTo:   pgconv.Date(in.EffectiveTo),
		RuleDsl:       pgconv.JSONB(in.RuleDsl),
		ProofPolicy:   pgconv.JSONB(in.ProofPolicy),
		SopVersionID:  pgconv.NullableUUID(in.SopVersionID),
		DraftedBy:     pgconv.NullableUUID(in.DraftedBy),
	})
	if err != nil {
		return "", fmt.Errorf("protocol: create version: %w", err)
	}
	return id, nil
}

// GetVersion fetches a version by id within a tenant.
func (r *Repository) GetVersion(ctx context.Context, tenantID, versionID string) (domain.Version, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.Version{}, fmt.Errorf("protocol: tenant id: %w", err)
	}
	vid, err := pgconv.UUID(versionID)
	if err != nil {
		return domain.Version{}, fmt.Errorf("protocol: version id: %w", err)
	}
	row, err := r.queries.GetProtocolVersion(ctx, protocoldb.GetProtocolVersionParams{TenantID: tenant, ProtocolVersionID: vid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Version{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Version{}, fmt.Errorf("protocol: get version: %w", err)
	}
	return domain.Version{
		ProtocolVersionID: row.ProtocolVersionID,
		ProtocolID:        row.ProtocolID,
		ScopeType:         row.ScopeType,
		ScopeID:           row.ScopeID,
		Version:           row.Version,
		Status:            row.Status,
		EffectiveFrom:     pgconv.DateValue(row.EffectiveFrom),
		EffectiveTo:       pgconv.DateValue(row.EffectiveTo),
		RuleDsl:           row.RuleDsl,
		ProofPolicy:       row.ProofPolicy,
		SopVersionID:      row.SopVersionID,
		RowVersion:        row.RowVersion,
	}, nil
}

// ListPublishedVersions returns published versions for a protocol, newest effective_from first.
func (r *Repository) ListPublishedVersions(ctx context.Context, tenantID, protocolID string) ([]domain.Version, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("protocol: tenant id: %w", err)
	}
	protocol, err := pgconv.UUID(protocolID)
	if err != nil {
		return nil, fmt.Errorf("protocol: protocol id: %w", err)
	}
	rows, err := r.queries.ListPublishedVersionsForProtocol(ctx, protocoldb.ListPublishedVersionsForProtocolParams{TenantID: tenant, ProtocolID: protocol})
	if err != nil {
		return nil, fmt.Errorf("protocol: list published versions: %w", err)
	}
	out := make([]domain.Version, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Version{
			ProtocolVersionID: row.ProtocolVersionID,
			ProtocolID:        protocolID,
			ScopeType:         row.ScopeType,
			ScopeID:           row.ScopeID,
			Version:           row.Version,
			Status:            "published",
			EffectiveFrom:     pgconv.DateValue(row.EffectiveFrom),
			EffectiveTo:       pgconv.DateValue(row.EffectiveTo),
		})
	}
	return out, nil
}

// PublishVersion flips a draft version to published.
func (r *Repository) PublishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("protocol: tenant id: %w", err)
	}
	vid, err := pgconv.UUID(versionID)
	if err != nil {
		return fmt.Errorf("protocol: version id: %w", err)
	}
	if err := r.queries.PublishProtocolVersion(ctx, protocoldb.PublishProtocolVersionParams{
		PublishedBy:       pgconv.NullableUUID(publishedBy),
		TenantID:          tenant,
		ProtocolVersionID: vid,
	}); err != nil {
		return fmt.Errorf("protocol: publish version: %w", err)
	}
	return nil
}

// ListPublishedVaccinationVersions returns the ids of published vaccination protocol versions.
func (r *Repository) ListPublishedVaccinationVersions(ctx context.Context, tenantID string) ([]string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("protocol: tenant id: %w", err)
	}
	ids, err := r.queries.ListPublishedVaccinationVersions(ctx, tenant)
	if err != nil {
		return nil, fmt.Errorf("protocol: list published vaccination versions: %w", err)
	}
	return ids, nil
}

// CreateRule inserts one dose/phase rule under a version.
func (r *Repository) CreateRule(ctx context.Context, in domain.NewRule) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", fmt.Errorf("protocol: tenant id: %w", err)
	}
	vid, err := pgconv.UUID(in.ProtocolVersionID)
	if err != nil {
		return "", fmt.Errorf("protocol: version id: %w", err)
	}
	id, err := r.queries.CreateProtocolRule(ctx, protocoldb.CreateProtocolRuleParams{
		TenantID:            tenant,
		ProtocolVersionID:   vid,
		DoseCode:            in.DoseCode,
		Sequence:            in.Sequence,
		TriggerType:         in.TriggerType,
		OffsetDays:          in.OffsetDays,
		DueWindowDays:       in.DueWindowDays,
		MinGapDays:          in.MinGapDays,
		Repeat:              in.Repeat,
		RepeatUntilAfterAge: pgconv.Text(in.RepeatUntilAfterAge),
		CatchUp:             in.CatchUp,
		EligibilityJson:     pgconv.JSONB(in.EligibilityJSON),
		SopVersionID:        pgconv.NullableUUID(in.SopVersionID),
		ProofPolicy:         pgconv.JSONB(in.ProofPolicy),
		WithdrawalDays:      pgconv.Int4(in.WithdrawalDays),
		SortOrder:           in.SortOrder,
	})
	if err != nil {
		return "", fmt.Errorf("protocol: create rule: %w", err)
	}
	return id, nil
}

// ListRules returns rules for a version ordered by sort_order, sequence.
func (r *Repository) ListRules(ctx context.Context, tenantID, versionID string) ([]domain.Rule, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("protocol: tenant id: %w", err)
	}
	vid, err := pgconv.UUID(versionID)
	if err != nil {
		return nil, fmt.Errorf("protocol: version id: %w", err)
	}
	rows, err := r.queries.ListRulesForVersion(ctx, protocoldb.ListRulesForVersionParams{TenantID: tenant, ProtocolVersionID: vid})
	if err != nil {
		return nil, fmt.Errorf("protocol: list rules: %w", err)
	}
	out := make([]domain.Rule, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Rule{
			RuleID:              row.RuleID,
			DoseCode:            row.DoseCode,
			Sequence:            row.Sequence,
			TriggerType:         row.TriggerType,
			OffsetDays:          row.OffsetDays,
			DueWindowDays:       row.DueWindowDays,
			MinGapDays:          row.MinGapDays,
			Repeat:              row.Repeat,
			RepeatUntilAfterAge: row.RepeatUntilAfterAge,
			CatchUp:             row.CatchUp,
			SortOrder:           row.SortOrder,
		})
	}
	return out, nil
}

// ListConfigs returns every protocol version (draft/published/retired) in a category for a tenant,
// projected for the generic Config authority screen. Read-only; no mutation, no audit.
func (r *Repository) ListConfigs(ctx context.Context, tenantID, category string) ([]domain.ConfigListItem, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("protocol: tenant id: %w", err)
	}
	rows, err := r.queries.ListProtocolConfigsForCategory(ctx, protocoldb.ListProtocolConfigsForCategoryParams{
		TenantID: tenant,
		Category: category,
		RowLimit: configListLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("protocol: list configs: %w", err)
	}
	out := make([]domain.ConfigListItem, 0, len(rows))
	for _, row := range rows {
		item := domain.ConfigListItem{
			ProtocolID:        row.ProtocolID,
			Code:              row.Code,
			Name:              row.Name,
			Category:          row.Category,
			ProtocolVersionID: row.ProtocolVersionID,
			Version:           row.Version,
			VersionLabel:      row.VersionLabel,
			ScopeType:         row.ScopeType,
			ScopeID:           row.ScopeID,
			Status:            row.Status,
			EffectiveFrom:     pgconv.DateValue(row.EffectiveFrom),
			EffectiveTo:       pgconv.DateValue(row.EffectiveTo),
			SopVersionID:      row.SopVersionID,
			PublishedBy:       row.PublishedBy,
			SourceSystem:      row.SourceSystem,
			SourceRef:         row.SourceRef,
			ReviewStatus:      row.ReviewStatus,
			ApprovedBy:        row.ApprovedBy,
			RuleCount:         row.RuleCount,
		}
		if row.PublishedAt.Valid {
			t := row.PublishedAt.Time
			item.PublishedAt = &t
		}
		if row.UpdatedAt.Valid {
			t := row.UpdatedAt.Time
			item.UpdatedAt = &t
		}
		out = append(out, item)
	}
	return out, nil
}

// ListActiveAnimalStages returns the tenant's active animal_stage_lookup rows in display order.
// Read-only reference data for the Config authoring stage picker; tenant-scoped and bounded.
func (r *Repository) ListActiveAnimalStages(ctx context.Context, tenantID string) ([]domain.AnimalStage, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("protocol: tenant id: %w", err)
	}
	rows, err := r.queries.ListActiveAnimalStages(ctx, protocoldb.ListActiveAnimalStagesParams{
		TenantID: tenant,
		RowLimit: animalStageListLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("protocol: list animal stages: %w", err)
	}
	out := make([]domain.AnimalStage, 0, len(rows))
	for _, row := range rows {
		stage := domain.AnimalStage{
			AnimalStageID: row.AnimalStageID,
			StageCode:     row.StageCode,
			Name:          row.Name,
			SortOrder:     row.SortOrder,
		}
		if row.MinAgeDays.Valid {
			v := row.MinAgeDays.Int32
			stage.MinAgeDays = &v
		}
		if row.MaxAgeDays.Valid {
			v := row.MaxAgeDays.Int32
			stage.MaxAgeDays = &v
		}
		out = append(out, stage)
	}
	return out, nil
}

// CreateTrigger inserts a protocol trigger.
func (r *Repository) CreateTrigger(ctx context.Context, in domain.NewTrigger) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", fmt.Errorf("protocol: tenant id: %w", err)
	}
	vid, err := pgconv.UUID(in.ProtocolVersionID)
	if err != nil {
		return "", fmt.Errorf("protocol: version id: %w", err)
	}
	id, err := r.queries.CreateProtocolTrigger(ctx, protocoldb.CreateProtocolTriggerParams{
		TenantID:          tenant,
		ProtocolVersionID: vid,
		TriggerType:       in.TriggerType,
		TriggerConfig:     pgconv.JSONB(in.TriggerConfig),
		IsActive:          in.IsActive,
	})
	if err != nil {
		return "", fmt.Errorf("protocol: create trigger: %w", err)
	}
	return id, nil
}
