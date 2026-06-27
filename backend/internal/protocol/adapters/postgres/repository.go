// Package postgres implements the protocol Repository over generated sqlc queries.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
		Category:          row.Category,
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

const (
	protocolPublishedEventType      = "protocol.version.published"
	protocolPublishedSchemaVersion  = "1.0.0"
	protocolPublishedSchemaRef      = "contracts/jsonschema/domain-event-envelope.schema.json#protocol.version.published"
	protocolPublishedTopic          = "protocol.events"
	protocolPublishedAggregateType  = "protocol_version"
	protocolPublishedProducerModule = "protocol"
)

// PublishVersion flips a draft version to published and emits the durable protocol.version.published
// outbox event in the same transaction. Generation workers consume the event idempotently, so a
// process crash after the status flip cannot lose the publish-triggered obligation cascade.
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

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("protocol: begin publish: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var published protocolPublishedRow
	err = tx.QueryRow(ctx, `
UPDATE protocol_versions pv
SET status = 'published',
    published_by = $1,
    published_at = now(),
    row_version = pv.row_version + 1,
    updated_at = now()
FROM protocol_definitions pd
WHERE pv.tenant_id = $2
  AND pv.protocol_version_id = $3
  AND pv.status = 'draft'
  AND pd.tenant_id = pv.tenant_id
  AND pd.protocol_id = pv.protocol_id
RETURNING pv.protocol_version_id::text,
          pv.protocol_id::text,
          pd.category,
          pv.scope_type,
          COALESCE(pv.scope_id::text, ''),
          pv.published_at`, pgconv.NullableUUID(publishedBy), tenant, vid).Scan(
		&published.VersionID,
		&published.ProtocolID,
		&published.Category,
		&published.ScopeType,
		&published.ScopeID,
		&published.PublishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrVersionNotDraft
	}
	if err != nil {
		return fmt.Errorf("protocol: publish version: %w", err)
	}
	if err := insertProtocolPublishedOutbox(ctx, tx, tenantID, published, publishedBy); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("protocol: commit publish: %w", err)
	}
	return nil
}

type protocolPublishedRow struct {
	VersionID   string
	ProtocolID  string
	Category    string
	ScopeType   string
	ScopeID     string
	PublishedAt time.Time
}

func insertProtocolPublishedOutbox(ctx context.Context, tx pgx.Tx, tenantID string, published protocolPublishedRow, publishedBy *string) error {
	var eventID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&eventID); err != nil {
		return fmt.Errorf("protocol: generate published event id: %w", err)
	}
	idempotencyKey := "protocol:version:published:" + published.VersionID
	traceID := idempotencyKey
	payload := map[string]any{
		"protocol_version_id": published.VersionID,
		"protocol_id":         published.ProtocolID,
		"category":            published.Category,
		"scope_type":          published.ScopeType,
		"scope_id":            nullableString(published.ScopeID),
		"published_at":        published.PublishedAt.UTC().Format(time.RFC3339Nano),
	}
	envelope, err := protocolPublishedEnvelope(tenantID, eventID, idempotencyKey, traceID, published, publishedBy, payload)
	if err != nil {
		return err
	}
	headers, _ := json.Marshal(map[string]any{
		"protocol_version_id": published.VersionID,
		"category":            published.Category,
		"schema_version":      protocolPublishedSchemaVersion,
	})
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, $11, 'pending', now()
)`, tenantID, eventID, protocolPublishedEventType, protocolPublishedSchemaVersion,
		protocolPublishedAggregateType, published.VersionID, protocolPublishedTopic, envelope, headers, idempotencyKey, traceID)
	if err != nil {
		return fmt.Errorf("protocol: insert published outbox: %w", err)
	}
	return nil
}

func protocolPublishedEnvelope(tenantID, eventID, idempotencyKey, traceID string, published protocolPublishedRow, publishedBy *string, payload map[string]any) ([]byte, error) {
	actorType := "system_rule"
	if publishedBy != nil && strings.TrimSpace(*publishedBy) != "" {
		actorType = "human"
	}
	actorID := any(nil)
	if publishedBy != nil && strings.TrimSpace(*publishedBy) != "" {
		actorID = strings.TrimSpace(*publishedBy)
	}
	occurred := published.PublishedAt.UTC().Format(time.RFC3339Nano)
	visibilityScope := map[string]any{"tenant_id": tenantID}
	if published.ScopeType == "park" && published.ScopeID != "" {
		visibilityScope["park_id"] = published.ScopeID
	}
	return json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     protocolPublishedEventType,
		"schema_version": protocolPublishedSchemaVersion,
		"schema_ref":     protocolPublishedSchemaRef,
		"aggregate_type": protocolPublishedAggregateType,
		"aggregate_id":   published.VersionID,
		"occurred_at":    occurred,
		"recorded_at":    occurred,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  protocolPublishedProducerModule,
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": actorType,
			"actor_id":   actorID,
			"actor_ref":  nil,
		},
		"subject_type":     "protocol_version",
		"subject_id":       published.VersionID,
		"visibility_scope": visibilityScope,
		"evidence_refs": []map[string]string{{
			"evidence_type": "event",
			"evidence_id":   published.VersionID,
		}},
		"payload":  payload,
		"trace_id": traceID,
	})
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
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
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrVersionNotDraft
	}
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
			ScopeLabel:        textFromAny(row.ScopeLabel),
			Status:            row.Status,
			EffectiveFrom:     pgconv.DateValue(row.EffectiveFrom),
			EffectiveTo:       pgconv.DateValue(row.EffectiveTo),
			SopVersionID:      row.SopVersionID,
			PublishedBy:       row.PublishedBy,
			SourceSystem:      row.SourceSystem,
			SourceRef:         row.SourceRef,
			ReviewStatus:      row.ReviewStatus,
			ApprovedBy:        row.ApprovedBy,
			ApprovedAt:        row.ApprovedAt,
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

func textFromAny(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
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
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrVersionNotDraft
	}
	if err != nil {
		return "", fmt.Errorf("protocol: create trigger: %w", err)
	}
	return id, nil
}
