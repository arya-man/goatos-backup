// Package postgres implements the protocol Repository over generated sqlc queries.
package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
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

type protocolIdemReservation struct {
	proceed    bool
	resultType string
	resultID   string
}

func protocolIdempotencyKey(clientKey, fingerprint string) string {
	key := strings.TrimSpace(clientKey)
	if key == "" {
		key = "auto:" + fingerprint
	}
	return key
}

func scopedProtocolIdempotencyKey(tenantID, scope, key string) string {
	return tenantID + ":" + scope + ":" + key
}

func protocolFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func canonicalJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func optionalInt32(value *int32) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(*value)
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func reserveProtocolIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, fingerprint, resultType string) (protocolIdemReservation, error) {
	scoped := scopedProtocolIdempotencyKey(tenantID, scope, key)
	var claimed string
	err := tx.QueryRow(ctx, `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status)
VALUES ($1, $2::uuid, $3, $4, 'started')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key`, scoped, tenantID, scope, fingerprint).Scan(&claimed)
	if err == nil {
		return protocolIdemReservation{proceed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return protocolIdemReservation{}, err
	}

	var existingHash, status, existingType, existingID string
	if err := tx.QueryRow(ctx, `
SELECT request_hash, status, COALESCE(result_type, ''), COALESCE(result_id::text, '')
FROM idempotency_keys
WHERE idempotency_key = $1`, scoped).Scan(&existingHash, &status, &existingType, &existingID); err != nil {
		return protocolIdemReservation{}, err
	}
	if existingHash != fingerprint || status != "completed" || existingType != resultType || strings.TrimSpace(existingID) == "" {
		return protocolIdemReservation{}, ports.ErrIdempotencyConflict
	}
	return protocolIdemReservation{proceed: false, resultType: existingType, resultID: existingID}, nil
}

func completeProtocolIdempotency(ctx context.Context, tx pgx.Tx, tenantID, scope, key, resultType, resultID string) error {
	scoped := scopedProtocolIdempotencyKey(tenantID, scope, key)
	_, err := tx.Exec(ctx, `
UPDATE idempotency_keys
SET status = 'completed',
    result_type = $2,
    result_id = $3::uuid,
    completed_at = now()
WHERE idempotency_key = $1`, scoped, resultType, resultID)
	return err
}

func recordProtocolCreateAudit(ctx context.Context, tx pgx.Tx, action, tenantID, resourceType, resourceID, scopeType, scopeID string, actor *string, afterState any, metadata map[string]any) error {
	actorType := "system_rule"
	actorID := ""
	if actor != nil && strings.TrimSpace(*actor) != "" {
		actorType = "human"
		actorID = strings.TrimSpace(*actor)
	}
	if scopeType == "tenant" {
		scopeID = ""
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    actorType,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ScopeType:    scopeType,
		ScopeID:      scopeID,
		AfterState:   afterState,
		Metadata:     metadata,
		TraceID:      action + ":" + resourceID,
	}); err != nil {
		return fmt.Errorf("protocol: audit %s: %w", action, err)
	}
	return nil
}

// CreateDefinition inserts a protocol definition.
func (r *Repository) CreateDefinition(ctx context.Context, in domain.NewDefinition) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", fmt.Errorf("protocol: tenant id: %w", err)
	}
	fingerprint := protocolFingerprint(
		"definition",
		in.TenantID,
		strings.TrimSpace(in.Code),
		strings.TrimSpace(in.Name),
		strings.TrimSpace(in.Category),
		strings.TrimSpace(in.Status),
	)
	key := protocolIdempotencyKey(in.IdempotencyKey, fingerprint)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("protocol: begin create definition: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reservation, err := reserveProtocolIdempotency(ctx, tx, in.TenantID, "protocol.definition.create", key, fingerprint, "protocol_definition")
	if err != nil {
		return "", fmt.Errorf("protocol: reserve definition idempotency: %w", err)
	}
	if !reservation.proceed {
		return reservation.resultID, nil
	}

	qtx := r.queries.WithTx(tx)
	id, err := qtx.CreateProtocolDefinition(ctx, protocoldb.CreateProtocolDefinitionParams{
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
	if err := recordProtocolCreateAudit(ctx, tx, protocolDefinitionCreatedAction, in.TenantID, "protocol_definition", id, "tenant", "", in.CreatedBy, map[string]any{
		"code":     in.Code,
		"name":     in.Name,
		"category": in.Category,
		"status":   in.Status,
	}, map[string]any{"idempotency_scope": "protocol.definition.create"}); err != nil {
		return "", err
	}
	if err := completeProtocolIdempotency(ctx, tx, in.TenantID, "protocol.definition.create", key, "protocol_definition", id); err != nil {
		return "", fmt.Errorf("protocol: complete definition idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("protocol: commit create definition: %w", err)
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
	fingerprint := protocolFingerprint(
		"version",
		in.TenantID,
		in.ProtocolID,
		strings.TrimSpace(in.ScopeType),
		optionalString(in.ScopeID),
		fmt.Sprint(in.Version),
		strings.TrimSpace(in.VersionLabel),
		strings.TrimSpace(in.Status),
		in.EffectiveFrom.UTC().Format(time.RFC3339Nano),
		optionalTime(in.EffectiveTo),
		canonicalJSON(in.RuleDsl),
		canonicalJSON(in.ProofPolicy),
		optionalString(in.SopVersionID),
	)
	key := protocolIdempotencyKey(in.IdempotencyKey, fingerprint)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("protocol: begin create version: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reservation, err := reserveProtocolIdempotency(ctx, tx, in.TenantID, "protocol.version.create", key, fingerprint, "protocol_version")
	if err != nil {
		return "", fmt.Errorf("protocol: reserve version idempotency: %w", err)
	}
	if !reservation.proceed {
		return reservation.resultID, nil
	}
	scopeUUID := pgconv.NullableUUID(in.ScopeID)
	if in.Version <= 0 {
		lockKey := strings.Join([]string{in.TenantID, in.ProtocolID, strings.TrimSpace(in.ScopeType), optionalString(in.ScopeID)}, ":")
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, lockKey); err != nil {
			return "", fmt.Errorf("protocol: lock version allocator: %w", err)
		}
		if err := tx.QueryRow(ctx, `
SELECT COALESCE(MAX(version), 0) + 1
FROM protocol_versions
WHERE tenant_id = $1
  AND protocol_id = $2
  AND scope_type = $3
  AND scope_id IS NOT DISTINCT FROM $4`, tenant, protocol, in.ScopeType, scopeUUID).Scan(&in.Version); err != nil {
			return "", fmt.Errorf("protocol: allocate version number: %w", err)
		}
	}

	qtx := r.queries.WithTx(tx)
	id, err := qtx.CreateProtocolVersion(ctx, protocoldb.CreateProtocolVersionParams{
		TenantID:      tenant,
		ProtocolID:    protocol,
		ScopeType:     in.ScopeType,
		ScopeID:       scopeUUID,
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
	scopeID := optionalString(in.ScopeID)
	if in.ScopeType == "tenant" {
		scopeID = ""
	}
	if err := recordProtocolCreateAudit(ctx, tx, protocolVersionCreatedAction, in.TenantID, "protocol_version", id, in.ScopeType, scopeID, in.DraftedBy, map[string]any{
		"protocol_id":    in.ProtocolID,
		"scope_type":     in.ScopeType,
		"scope_id":       optionalString(in.ScopeID),
		"version":        in.Version,
		"version_label":  in.VersionLabel,
		"status":         in.Status,
		"effective_from": in.EffectiveFrom.UTC().Format(time.RFC3339Nano),
		"effective_to":   optionalTime(in.EffectiveTo),
	}, map[string]any{"idempotency_scope": "protocol.version.create"}); err != nil {
		return "", err
	}
	if err := completeProtocolIdempotency(ctx, tx, in.TenantID, "protocol.version.create", key, "protocol_version", id); err != nil {
		return "", fmt.Errorf("protocol: complete version idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("protocol: commit create version: %w", err)
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

// GetVersionsByIDs fetches a bounded page of versions in one round-trip for cohort generation.
func (r *Repository) GetVersionsByIDs(ctx context.Context, tenantID string, versionIDs []string) (map[string]domain.Version, error) {
	out := make(map[string]domain.Version, len(versionIDs))
	if len(versionIDs) == 0 {
		return out, nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("protocol: tenant id: %w", err)
	}
	ids, err := pgconv.UUIDs(versionIDs)
	if err != nil {
		return nil, fmt.Errorf("protocol: version ids: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
SELECT pv.protocol_version_id::text AS protocol_version_id,
       pv.protocol_id::text AS protocol_id,
       pd.category AS category,
       pv.scope_type,
       COALESCE(pv.scope_id::text, '')::text AS scope_id,
       pv.version,
       pv.status,
       pv.effective_from,
       pv.effective_to,
       pv.rule_dsl,
       pv.proof_policy,
       COALESCE(pv.sop_version_id::text, '')::text AS sop_version_id,
       pv.row_version
FROM protocol_versions pv
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id
 AND pd.protocol_id = pv.protocol_id
WHERE pv.tenant_id = $1
  AND pv.protocol_version_id = ANY($2::uuid[])`, tenant, ids)
	if err != nil {
		return nil, fmt.Errorf("protocol: get versions by ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row domain.Version
		var effectiveFrom, effectiveTo pgtype.Date
		if err := rows.Scan(
			&row.ProtocolVersionID,
			&row.ProtocolID,
			&row.Category,
			&row.ScopeType,
			&row.ScopeID,
			&row.Version,
			&row.Status,
			&effectiveFrom,
			&effectiveTo,
			&row.RuleDsl,
			&row.ProofPolicy,
			&row.SopVersionID,
			&row.RowVersion,
		); err != nil {
			return nil, fmt.Errorf("protocol: scan version by id: %w", err)
		}
		row.EffectiveFrom = pgconv.DateValue(effectiveFrom)
		row.EffectiveTo = pgconv.DateValue(effectiveTo)
		out[row.ProtocolVersionID] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("protocol: get versions by ids: %w", err)
	}
	return out, nil
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
	protocolDefinitionCreatedAction = "protocol.definition.created"
	protocolVersionCreatedAction    = "protocol.version.created"
	protocolRuleCreatedAction       = "protocol.rule.created"
	protocolPublishedEventType      = "protocol.version.published"
	protocolRetiredEventType        = "protocol.version.retired"
	protocolPublishedSchemaVersion  = "1.0.0"
	protocolPublishedSchemaRef      = "contracts/jsonschema/domain-event-envelope.schema.json#protocol.version.published"
	protocolRetiredSchemaRef        = "contracts/jsonschema/domain-event-envelope.schema.json#protocol.version.retired"
	protocolPublishedTopic          = "protocol.events"
	protocolPublishedAggregateType  = "protocol_version"
	protocolPublishedProducerModule = "protocol"
)

// PublishVersion flips a draft version to published and emits the durable protocol.version.published
// outbox event in the same transaction. Generation workers consume the event idempotently, so a
// process crash after the status flip cannot lose the publish-triggered obligation cascade.
// upsertVaccinationCapacityConfigTx upserts the versioned capacity into vaccination_capacity_config and
// verifies the stored row equals the authored values, all INSIDE the caller's publish transaction. A
// parity mismatch returns ports.ErrCapacityParityMismatch so the deferred Rollback leaves the version
// draft — publish and capacity sync commit together or not at all.
func upsertVaccinationCapacityConfigTx(ctx context.Context, tx pgx.Tx, tenantID string, want domain.PublishedCapacity) error {
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("protocol: tenant id: %w", err)
	}
	var got domain.PublishedCapacity
	err = tx.QueryRow(ctx, `
INSERT INTO vaccination_capacity_config (tenant_id, max_per_day, capacity_scope, max_buffer_days, overflow_policy)
VALUES ($1::uuid, $2, $3, $4, $5)
ON CONFLICT (tenant_id) DO UPDATE SET
  max_per_day = EXCLUDED.max_per_day,
  capacity_scope = EXCLUDED.capacity_scope,
  max_buffer_days = EXCLUDED.max_buffer_days,
  overflow_policy = EXCLUDED.overflow_policy,
  row_version = vaccination_capacity_config.row_version + 1,
  updated_at = now()
RETURNING max_per_day, max_buffer_days, capacity_scope, overflow_policy`,
		tenant, want.MaxPerDay, want.CapacityScope, want.MaxBufferDays, want.OverflowPolicy).
		Scan(&got.MaxPerDay, &got.MaxBufferDays, &got.CapacityScope, &got.OverflowPolicy)
	if err != nil {
		return fmt.Errorf("protocol: sync vaccination capacity config: %w", err)
	}
	if got != want {
		return fmt.Errorf("%w (rule_dsl=%+v stored=%+v)", ports.ErrCapacityParityMismatch, want, got)
	}
	return nil
}

func (r *Repository) PublishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string, idempotencyKey ...string) error {
	return r.publishVersion(ctx, tenantID, versionID, publishedBy, nil, idempotencyKey...)
}

// DiscardVersion permanently deletes a DRAFT version and its rules.
//
// The SQL carries a status = 'draft' predicate, so this cannot remove a published or
// retired version whatever id is passed: history stays complete by construction rather
// than by the caller remembering to check. Zero rows affected therefore means either
// "no such version" or "not a draft", and the two are distinguished by reading the row
// back so the caller can say which one happened.
func (r *Repository) DiscardVersion(ctx context.Context, tenantID, versionID string) error {
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
	rows, err := r.queries.DiscardProtocolVersion(ctx, protocoldb.DiscardProtocolVersionParams{TenantID: tenant, ProtocolVersionID: vid})
	if err != nil {
		return fmt.Errorf("protocol: discard version: %w", err)
	}
	if rows == 0 {
		if _, err := r.GetVersion(ctx, tenantID, versionID); err != nil {
			return err
		}
		return ports.ErrVersionNotDraft
	}
	return nil
}

// PublishVersionWithCapacity publishes a draft version and, in the SAME transaction, upserts and
// parity-checks its versioned vaccination capacity into vaccination_capacity_config. If the capacity
// sync or parity check fails, the whole publish rolls back and the version stays draft.
func (r *Repository) PublishVersionWithCapacity(ctx context.Context, tenantID, versionID string, publishedBy *string, capacity domain.PublishedCapacity, idempotencyKey ...string) error {
	return r.publishVersion(ctx, tenantID, versionID, publishedBy, &capacity, idempotencyKey...)
}

func (r *Repository) publishVersion(ctx context.Context, tenantID, versionID string, publishedBy *string, capacity *domain.PublishedCapacity, idempotencyKey ...string) error {
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
	fingerprint := protocolFingerprint("publish", tenantID, versionID)
	key := protocolIdempotencyKey(firstString(idempotencyKey), fingerprint)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("protocol: begin publish: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reservation, err := reserveProtocolIdempotency(ctx, tx, tenantID, "protocol.version.publish", key, fingerprint, "protocol_version")
	if err != nil {
		return fmt.Errorf("protocol: reserve publish idempotency: %w", err)
	}
	if !reservation.proceed {
		return nil
	}

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
		var status string
		statusErr := tx.QueryRow(ctx, `
SELECT status
FROM protocol_versions
WHERE tenant_id = $1
  AND protocol_version_id = $2`, tenant, vid).Scan(&status)
		if errors.Is(statusErr, pgx.ErrNoRows) {
			return ports.ErrNotFound
		}
		if statusErr != nil {
			return fmt.Errorf("protocol: check publish status: %w", statusErr)
		}
		if status == "published" {
			if err := completeProtocolIdempotency(ctx, tx, tenantID, "protocol.version.publish", key, "protocol_version", versionID); err != nil {
				return fmt.Errorf("protocol: complete published replay idempotency: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("protocol: commit published replay: %w", err)
			}
			return nil
		}
		return ports.ErrVersionNotDraft
	}
	if err != nil {
		return fmt.Errorf("protocol: publish version: %w", err)
	}
	if err := recordProtocolPublishedAudit(ctx, tx, tenantID, published, publishedBy); err != nil {
		return err
	}
	if err := insertProtocolPublishedOutbox(ctx, tx, tenantID, published, publishedBy); err != nil {
		return err
	}
	if err := completeProtocolIdempotency(ctx, tx, tenantID, "protocol.version.publish", key, "protocol_version", versionID); err != nil {
		return fmt.Errorf("protocol: complete publish idempotency: %w", err)
	}
	if capacity != nil {
		if err := upsertVaccinationCapacityConfigTx(ctx, tx, tenantID, *capacity); err != nil {
			return err
		}
		if err := enqueueVaccinationCapacityChangedForConfiguredParks(ctx, tx, tenantID, versionID); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("protocol: commit publish: %w", err)
	}
	return nil
}

// enqueueVaccinationCapacityChangedForConfiguredParks enqueues vaccination.capacity.changed (same
// transaction as the publish) for every park that has an operator-assignment config in this
// tenant, whenever a protocol publish changes the tenant-wide vaccination_capacity_config
// (max_per_day/buffer/overflow -- rule_dsl.capacity). Before this, publishing with changed
// capacity synced vaccination_capacity_config transactionally but emitted only
// protocol.version.published (consumed only by obligation regeneration, ProtocolPublishedHandler)
// -- OperatorConfigReplanHandler was never notified, so a park's already-planned future drives
// were not released/replanned to reflect the new capacity policy. vaccination_capacity_config
// itself is tenant-scoped (PK is tenant_id, not park-scoped), so there is no single park to target;
// emitting per configured park is the same fan-out shape UpsertOperatorAssignmentConfig already
// uses for the per-park N/default-operator cascade, just enumerated across every affected park
// instead of one caller-supplied park_id. The park set is the UNION of parks with an
// operator-assignment config and parks that still have future planned vaccination work (fallback /
// no-config parks plan drives too, and their future rows would otherwise stay stale).
func enqueueVaccinationCapacityChangedForConfiguredParks(ctx context.Context, tx pgx.Tx, tenantID, versionID string) error {
	// One set-based park enumeration: parks with an operator-assignment config UNION parks that
	// still have FUTURE planned vaccination work. A fallback/no-config park can carry already-planned
	// future drive rows; skipping it would leave those rows stale against the new capacity policy.
	// "Future" is the Asia/Kolkata business date (biztime), same semantics as the rest of the repo.
	rows, err := tx.Query(ctx, `
SELECT park_id::text FROM vaccination_operator_assignment_config WHERE tenant_id = $1::uuid
UNION
SELECT DISTINCT vda.park_id::text
FROM vaccination_drive_assignments vda
JOIN obligation_batches ob
  ON ob.tenant_id = vda.tenant_id AND ob.batch_id = vda.batch_id
WHERE vda.tenant_id = $1::uuid
  AND vda.planned_date >= $2::date
  AND ob.status IN ('planned', 'in_progress')`,
		tenantID, biztime.BusinessDate(time.Now()))
	if err != nil {
		return fmt.Errorf("protocol: list parks for capacity-changed cascade: %w", err)
	}
	defer rows.Close()
	var parkIDs []string
	for rows.Next() {
		var parkID string
		if err := rows.Scan(&parkID); err != nil {
			return fmt.Errorf("protocol: scan park for capacity-changed cascade: %w", err)
		}
		parkIDs = append(parkIDs, parkID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("protocol: iterate parks for capacity-changed cascade: %w", err)
	}

	if len(parkIDs) == 0 {
		return nil
	}

	// Build the per-park rows in memory, then insert them in ONE set-based statement (UNNEST).
	// A tx.Exec per park would be an N+1 write inside a loop (banned by make scale-guard).
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
	eventIDs := make([]string, 0, len(parkIDs))
	aggregateIDs := make([]string, 0, len(parkIDs))
	envelopes := make([]string, 0, len(parkIDs))
	headerRows := make([]string, 0, len(parkIDs))
	idempotencyKeys := make([]string, 0, len(parkIDs))
	for _, parkID := range parkIDs {
		idempotencyKey := fmt.Sprintf("protocol.version-publish.capacity:%s:%s", versionID, parkID)
		eventID := platformoutbox.DeterministicUUID(idempotencyKey)
		envelope, err := json.Marshal(map[string]any{
			"event_id":       eventID,
			"event_type":     "vaccination.capacity.changed",
			"schema_version": "1.0.0",
			"schema_ref":     "domain-event-envelope.v1",
			"aggregate_type": "park",
			"aggregate_id":   parkID,
			"occurred_at":    now,
			"recorded_at":    now,
			"producer": map[string]any{
				"service": "goatos-api",
				"module":  "protocol",
				"version": nil,
			},
			"idempotency_key": idempotencyKey,
			"actor": map[string]any{
				"actor_type": "system_rule",
				"actor_id":   nil,
				"actor_ref":  nil,
			},
			"subject_type": "location",
			"subject_id":   parkID,
			"visibility_scope": map[string]any{
				"tenant_id": tenantID,
				"park_id":   parkID,
			},
			"evidence_refs": []map[string]string{{
				"evidence_type": "location",
				"evidence_id":   parkID,
			}},
			"payload":  map[string]any{"park_id": parkID},
			"trace_id": idempotencyKey,
		})
		if err != nil {
			return fmt.Errorf("protocol: marshal capacity-changed envelope: %w", err)
		}
		headers, err := json.Marshal(map[string]any{
			"producer":        "protocol.PublishVersionWithCapacity",
			"schema_version":  "1.0.0",
			"park_id":         parkID,
			"idempotency_key": idempotencyKey,
		})
		if err != nil {
			return fmt.Errorf("protocol: marshal capacity-changed headers: %w", err)
		}
		eventIDs = append(eventIDs, eventID)
		aggregateIDs = append(aggregateIDs, parkID)
		envelopes = append(envelopes, string(envelope))
		headerRows = append(headerRows, string(headers))
		idempotencyKeys = append(idempotencyKeys, idempotencyKey)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
)
SELECT $1::uuid, e.event_id::uuid, 'vaccination.capacity.changed', '1.0.0', 'park', e.aggregate_id::uuid,
       'vaccination.events', e.payload::jsonb, e.headers::jsonb, e.idempotency_key, e.idempotency_key, 'pending', now()
FROM unnest($2::text[], $3::text[], $4::text[], $5::text[], $6::text[])
  AS e(event_id, aggregate_id, payload, headers, idempotency_key)
ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = 'vaccination.capacity.changed' DO NOTHING`,
		tenantID, eventIDs, aggregateIDs, envelopes, headerRows, idempotencyKeys); err != nil {
		return fmt.Errorf("protocol: enqueue capacity-changed to outbox: %w", err)
	}
	return nil
}

// PublishVersionWithDerivedRules replaces vaccination.matrix derived rules/dimensions and publishes
// the version in one transaction. The matrix JSON is the authoring source of truth; protocol_rules
// and protocol_rule_dimensions are regenerated execution indexes, never hand-authored authority.
func (r *Repository) PublishVersionWithDerivedRules(ctx context.Context, tenantID string, v domain.Version, rules []domain.NewRule, dimensions []domain.RuleDimension, publishedBy *string, capacity *domain.PublishedCapacity, seedOwnedGuardActor string, idempotencyKey ...string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("protocol: tenant id: %w", err)
	}
	vid, err := pgconv.UUID(v.ProtocolVersionID)
	if err != nil {
		return fmt.Errorf("protocol: version id: %w", err)
	}
	fingerprint := protocolFingerprint(
		"publish-matrix",
		tenantID,
		v.ProtocolVersionID,
		canonicalJSON(v.RuleDsl),
		derivedRulesFingerprint(rules),
		derivedDimensionsFingerprint(dimensions),
	)
	key := protocolIdempotencyKey(firstString(idempotencyKey), fingerprint)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("protocol: begin matrix publish: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reservation, err := reserveProtocolIdempotency(ctx, tx, tenantID, "protocol.version.publish", key, fingerprint, "protocol_version")
	if err != nil {
		return fmt.Errorf("protocol: reserve matrix publish idempotency: %w", err)
	}
	if !reservation.proceed {
		return nil
	}

	var status, scopeType, scopeID, targetDraftedBy string
	err = tx.QueryRow(ctx, `
SELECT status, scope_type, COALESCE(scope_id::text, ''), COALESCE(drafted_by::text, '')
FROM protocol_versions
WHERE tenant_id = $1
  AND protocol_version_id = $2
FOR UPDATE`, tenant, vid).Scan(&status, &scopeType, &scopeID, &targetDraftedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("protocol: lock matrix version: %w", err)
	}
	if status == "published" {
		if err := completeProtocolIdempotency(ctx, tx, tenantID, "protocol.version.publish", key, "protocol_version", v.ProtocolVersionID); err != nil {
			return fmt.Errorf("protocol: complete matrix published replay idempotency: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("protocol: commit matrix published replay: %w", err)
		}
		return nil
	}
	if status != "draft" {
		return ports.ErrVersionNotDraft
	}
	lockKey := strings.Join([]string{tenantID, "vaccination.matrix", scopeType, scopeID}, ":")
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, lockKey); err != nil {
		return fmt.Errorf("protocol: lock vaccination matrix scope: %w", err)
	}
	// VAX-SEED-01/03/R2 ownership safety: a seed-owned publish may (a) publish ONLY a target the seed
	// itself drafted, and (b) retire ONLY seed-owned overlapping matrices. Both checks run INSIDE the
	// publish transaction, after the vaccination-matrix advisory lock and BEFORE the overlap-retire, so
	// they are atomic with the retire — a matrix published concurrently (or authored via Config under
	// the SAME protocol) is seen here and blocks the retire (fail closed) instead of being silently
	// retired. seedOwnedGuardActor is empty for ordinary/Config publishes, which keep the normal
	// supersede-on-publish behavior.
	if seedOwnedGuardActor != "" {
		// R2: the caller supplied a seed actor, so the target being published must itself be seed-drafted;
		// a seed publish must never launder a user-drafted (or unstamped) target into a seed operation.
		if targetDraftedBy != seedOwnedGuardActor {
			return fmt.Errorf("%w: target version %s is drafted_by %q, not the seed actor", ErrVaccinationMatrixOwnershipConflict, v.ProtocolVersionID, targetDraftedBy)
		}
		if err := assertOnlySeedOwnedMatrixOverlapsTx(ctx, tx, tenant, vid, seedOwnedGuardActor); err != nil {
			return err
		}
	}
	retiredRows, err := retirePublishedVaccinationMatrixOverlapsTx(ctx, tx, tenant, vid)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
DELETE FROM protocol_rule_dimensions
WHERE tenant_id = $1 AND protocol_version_id = $2`, tenant, vid); err != nil {
		return fmt.Errorf("protocol: delete matrix rule dimensions: %w", err)
	}
	if _, err := tx.Exec(ctx, `
DELETE FROM protocol_rules
WHERE tenant_id = $1 AND protocol_version_id = $2`, tenant, vid); err != nil {
		return fmt.Errorf("protocol: delete matrix derived rules: %w", err)
	}
	for _, rule := range rules {
		if err := insertDerivedProtocolRuleTx(ctx, tx, tenant, rule); err != nil {
			return err
		}
	}
	if err := insertProtocolRuleDimensionsTx(ctx, tx, tenant, vid, dimensions); err != nil {
		return err
	}

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
		return fmt.Errorf("protocol: publish matrix version: %w", err)
	}
	if err := recordProtocolPublishedAudit(ctx, tx, tenantID, published, publishedBy); err != nil {
		return err
	}
	for _, retired := range retiredRows {
		retired.ReplacedByVersionID = published.VersionID
		if err := recordProtocolRetiredAudit(ctx, tx, tenantID, retired, publishedBy); err != nil {
			return err
		}
		if err := insertProtocolRetiredOutbox(ctx, tx, tenantID, retired, publishedBy); err != nil {
			return err
		}
	}
	if err := insertProtocolPublishedOutbox(ctx, tx, tenantID, published, publishedBy); err != nil {
		return err
	}
	if err := completeProtocolIdempotency(ctx, tx, tenantID, "protocol.version.publish", key, "protocol_version", v.ProtocolVersionID); err != nil {
		return fmt.Errorf("protocol: complete matrix publish idempotency: %w", err)
	}
	if capacity != nil {
		if err := upsertVaccinationCapacityConfigTx(ctx, tx, tenantID, *capacity); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("protocol: commit matrix publish: %w", err)
	}
	return nil
}

// PublishPublishedMatrixReplay resolves a client retry for an already-published
// vaccination.matrix version without rebuilding derived execution rows. Old
// published matrices may predate today's stricter authoring validators; once
// the DB has committed the publish, replay must converge through idempotency
// instead of re-validating yesterday's authoring shape.
func (r *Repository) PublishPublishedMatrixReplay(ctx context.Context, tenantID string, v domain.Version, _ *string, idempotencyKey ...string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("protocol: tenant id: %w", err)
	}
	vid, err := pgconv.UUID(v.ProtocolVersionID)
	if err != nil {
		return fmt.Errorf("protocol: version id: %w", err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("protocol: begin matrix published replay: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `
SELECT status
FROM protocol_versions
WHERE tenant_id = $1
  AND protocol_version_id = $2
FOR UPDATE`, tenant, vid).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("protocol: lock matrix published replay: %w", err)
	}
	if status != "published" {
		return ports.ErrVersionNotDraft
	}

	if clientKey := strings.TrimSpace(firstString(idempotencyKey)); clientKey != "" {
		scoped := scopedProtocolIdempotencyKey(tenantID, "protocol.version.publish", clientKey)
		var existingHash, existingStatus, existingType, existingID string
		err = tx.QueryRow(ctx, `
SELECT request_hash, status, COALESCE(result_type, ''), COALESCE(result_id::text, '')
FROM idempotency_keys
WHERE idempotency_key = $1
FOR UPDATE`, scoped).Scan(&existingHash, &existingStatus, &existingType, &existingID)
		if errors.Is(err, pgx.ErrNoRows) {
			fingerprint := protocolFingerprint("publish-matrix-replay", tenantID, v.ProtocolVersionID)
			if _, err := tx.Exec(ctx, `
INSERT INTO idempotency_keys (
  idempotency_key, tenant_id, scope, request_hash, status, result_type, result_id, completed_at
) VALUES (
  $1, $2::uuid, 'protocol.version.publish', $3, 'completed', 'protocol_version', $4::uuid, now()
)`, scoped, tenantID, fingerprint, v.ProtocolVersionID); err != nil {
				return fmt.Errorf("protocol: record matrix published replay idempotency: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("protocol: read matrix published replay idempotency: %w", err)
		} else {
			if existingStatus != "completed" {
				return ports.ErrIdempotencyConflict
			}
			if existingType != "protocol_version" || strings.TrimSpace(existingID) != v.ProtocolVersionID {
				return ports.ErrIdempotencyConflict
			}
			// Completed original matrix publishes used the full publish fingerprint. A retry after
			// validation rules changed may enter this replay path with a different replay-only
			// fingerprint; same completed result is the durable idempotency authority.
			_ = existingHash
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("protocol: commit matrix published replay: %w", err)
	}
	return nil
}

func derivedRulesFingerprint(rules []domain.NewRule) string {
	parts := make([]string, 0, len(rules)*16)
	for _, rule := range rules {
		parts = append(parts,
			strings.TrimSpace(rule.RuleID),
			strings.TrimSpace(rule.DoseCode),
			fmt.Sprint(rule.Sequence),
			strings.TrimSpace(rule.TriggerType),
			fmt.Sprint(rule.OffsetDays),
			fmt.Sprint(rule.DueWindowDays),
			fmt.Sprint(rule.MinGapDays),
			strings.TrimSpace(rule.Repeat),
			strings.TrimSpace(rule.RepeatUntilAfterAge),
			strings.TrimSpace(rule.CatchUp),
			canonicalJSON(rule.EligibilityJSON),
			optionalString(rule.SopVersionID),
			canonicalJSON(rule.ProofPolicy),
			optionalInt32(rule.WithdrawalDays),
			fmt.Sprint(rule.SortOrder),
		)
	}
	return protocolFingerprint(parts...)
}

func derivedDimensionsFingerprint(dimensions []domain.RuleDimension) string {
	parts := make([]string, 0, len(dimensions)*12)
	for _, dim := range dimensions {
		parts = append(parts,
			strings.TrimSpace(dim.RuleID),
			strings.TrimSpace(dim.SelectorKey),
			strings.TrimSpace(dim.MatrixRowID),
			strings.TrimSpace(dim.DoseCode),
			strings.TrimSpace(dim.SourceDoseCode),
			strings.TrimSpace(dim.VaccineCode),
			strings.TrimSpace(dim.Species),
			strings.TrimSpace(dim.AnimalStage),
			strings.TrimSpace(dim.Sex),
			strings.TrimSpace(dim.Breed),
			fmt.Sprint(dim.OffsetDays),
			fmt.Sprint(dim.MaxDelayDays),
		)
	}
	return protocolFingerprint(parts...)
}

// ErrVaccinationMatrixOwnershipConflict aliases the ports sentinel so existing callers/tests that
// reference the postgres symbol keep working; the canonical definition lives in ports so the app layer
// can also return it (e.g. the seed-owned publish target-ownership check before dispatch).
var ErrVaccinationMatrixOwnershipConflict = ports.ErrVaccinationMatrixOwnershipConflict

// VaccinationMatrixVersionDraftedBy returns the target version's drafted_by (empty string when NULL).
// The app's seed-owned publish uses it to verify a seed publish only ever targets a seed-drafted
// version, closing the already-published replay path where PublishPublishedMatrixReplay would otherwise
// return success without an ownership check.
func (r *Repository) VaccinationMatrixVersionDraftedBy(ctx context.Context, tenantID, versionID string) (string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", fmt.Errorf("protocol: tenant id: %w", err)
	}
	vid, err := pgconv.UUID(versionID)
	if err != nil {
		return "", fmt.Errorf("protocol: version id: %w", err)
	}
	var draftedBy string
	err = r.pool.QueryRow(ctx, `SELECT COALESCE(drafted_by::text, '') FROM protocol_versions WHERE tenant_id = $1 AND protocol_version_id = $2`, tenant, vid).Scan(&draftedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("protocol: read version drafted_by: %w", err)
	}
	return draftedBy, nil
}

// assertOnlySeedOwnedMatrixOverlapsTx enforces, inside the publish transaction and under the
// vaccination-matrix advisory lock, that no published matrix version overlapping the one being
// published is authored by anyone other than the seed. It uses the SAME overlap predicate as
// retirePublishedVaccinationMatrixOverlapsTx, so it flags exactly the versions the retire would
// clear. An overlap is seed-owned ONLY when drafted_by = seedGuardActor. Any overlap whose author is
// different OR cannot be positively identified as the seed (drafted_by NULL — published versions are
// immutable and cannot be back-stamped, and NULL is a schema-permitted value that is not provably the
// seed) makes it fail closed with ErrVaccinationMatrixOwnershipConflict. Legacy seed DRAFTS are
// back-stamped before publish (see reconcileSeedMatrixDraftVersion), so a legitimate reseed only ever
// supersedes its own stamped versions.
func assertOnlySeedOwnedMatrixOverlapsTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, versionID pgtype.UUID, seedGuardActor string) error {
	actor, err := pgconv.UUID(seedGuardActor)
	if err != nil {
		return fmt.Errorf("protocol: seed guard actor id: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT other.protocol_version_id::text, other.protocol_id::text, COALESCE(other.version_label, '')
FROM protocol_versions other
JOIN protocol_versions target ON target.tenant_id = $1 AND target.protocol_version_id = $2
JOIN protocol_definitions pd ON pd.tenant_id = other.tenant_id AND pd.protocol_id = other.protocol_id
WHERE other.tenant_id = target.tenant_id
  AND other.protocol_version_id <> target.protocol_version_id
  AND other.status = 'published'
  AND pd.category = 'vaccination'
  AND other.scope_type = target.scope_type
  AND other.scope_id IS NOT DISTINCT FROM target.scope_id
  AND daterange(other.effective_from, other.effective_to, '[)') &&
      daterange(target.effective_from, target.effective_to, '[)')
  AND (
    lower(COALESCE(other.rule_dsl->>'ruleset_family', '')) = 'vaccination.matrix'
    OR lower(COALESCE(other.rule_dsl->'vaccine'->>'code', '')) = 'vaccination.matrix'
    OR other.rule_dsl ? 'matrix_rows'
  )
  AND other.drafted_by IS DISTINCT FROM $3::uuid
ORDER BY other.protocol_version_id`, tenant, versionID, actor)
	if err != nil {
		return fmt.Errorf("protocol: check seed-owned matrix overlaps: %w", err)
	}
	defer rows.Close()
	var offenders []string
	for rows.Next() {
		var versionIDText, protocolIDText, label string
		if err := rows.Scan(&versionIDText, &protocolIDText, &label); err != nil {
			return fmt.Errorf("protocol: scan matrix overlap: %w", err)
		}
		offenders = append(offenders, fmt.Sprintf("version %s (protocol %s, label %q)", versionIDText, protocolIDText, label))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("protocol: matrix overlap rows: %w", err)
	}
	if len(offenders) > 0 {
		return fmt.Errorf("%w: %v", ErrVaccinationMatrixOwnershipConflict, offenders)
	}
	return nil
}

func retirePublishedVaccinationMatrixOverlapsTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, versionID pgtype.UUID) ([]protocolRetiredRow, error) {
	// Vaccination matrix authoring has one active company/park version at a time. Publishing a new
	// complete matrix retires the older overlapping matrix in the same transaction, preserving the
	// immutable historical version while making the newly published one authoritative.
	rows, err := tx.Query(ctx, `
WITH retired AS (
  UPDATE protocol_versions AS other
SET status = 'retired',
    retired_at = now(),
    row_version = other.row_version + 1,
    updated_at = now()
FROM protocol_versions target, protocol_definitions pd
WHERE target.tenant_id = $1
  AND target.protocol_version_id = $2
  AND other.tenant_id = target.tenant_id
  AND other.protocol_version_id <> target.protocol_version_id
  AND pd.tenant_id = other.tenant_id
  AND pd.protocol_id = other.protocol_id
  AND other.status = 'published'
  AND pd.category = 'vaccination'
  AND other.scope_type = target.scope_type
  AND other.scope_id IS NOT DISTINCT FROM target.scope_id
  AND daterange(other.effective_from, other.effective_to, '[)') &&
      daterange(target.effective_from, target.effective_to, '[)')
  AND (
    lower(COALESCE(other.rule_dsl->>'ruleset_family', '')) = 'vaccination.matrix'
    OR lower(COALESCE(other.rule_dsl->'vaccine'->>'code', '')) = 'vaccination.matrix'
    OR other.rule_dsl ? 'matrix_rows'
  )
  RETURNING other.protocol_version_id::text AS protocol_version_id,
            other.protocol_id::text AS protocol_id,
            pd.category AS category,
            other.scope_type AS scope_type,
            COALESCE(other.scope_id::text, '') AS scope_id,
            other.retired_at AS retired_at
)
SELECT protocol_version_id, protocol_id, category, scope_type, scope_id, retired_at
FROM retired`, tenant, versionID)
	if err != nil {
		return nil, fmt.Errorf("protocol: retire overlapping vaccination matrix versions: %w", err)
	}
	defer rows.Close()
	var retired []protocolRetiredRow
	for rows.Next() {
		var row protocolRetiredRow
		if err := rows.Scan(
			&row.VersionID,
			&row.ProtocolID,
			&row.Category,
			&row.ScopeType,
			&row.ScopeID,
			&row.RetiredAt,
		); err != nil {
			return nil, fmt.Errorf("protocol: scan retired vaccination matrix version: %w", err)
		}
		retired = append(retired, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("protocol: read retired vaccination matrix versions: %w", err)
	}
	return retired, nil
}

func insertDerivedProtocolRuleTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, in domain.NewRule) error {
	ruleID, err := pgconv.UUID(in.RuleID)
	if err != nil {
		return fmt.Errorf("protocol: derived rule id %q: %w", in.RuleID, err)
	}
	vid, err := pgconv.UUID(in.ProtocolVersionID)
	if err != nil {
		return fmt.Errorf("protocol: version id: %w", err)
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, "sequence", trigger_type, offset_days,
  due_window_days, min_gap_days, "repeat", repeat_until_after_age, catch_up,
  eligibility_json, sop_version_id, proof_policy, withdrawal_days, sort_order
) SELECT
  $1, $2, $3, $4, $5, $6, $7,
  $8, $9, $10, $11, $12,
  $13, $14, $15, $16, $17
FROM protocol_versions pv
WHERE pv.tenant_id = $2
  AND pv.protocol_version_id = $3
  AND pv.status = 'draft'`,
		ruleID, tenant, vid, in.DoseCode, in.Sequence, in.TriggerType, in.OffsetDays,
		in.DueWindowDays, in.MinGapDays, in.Repeat, pgconv.Text(in.RepeatUntilAfterAge), in.CatchUp,
		pgconv.JSONB(in.EligibilityJSON), pgconv.NullableUUID(in.SopVersionID), pgconv.JSONB(in.ProofPolicy), pgconv.Int4(in.WithdrawalDays), in.SortOrder,
	)
	if err != nil {
		return fmt.Errorf("protocol: insert derived rule %s: %w", in.DoseCode, err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrVersionNotDraft
	}
	if err := recordProtocolCreateAudit(ctx, tx, protocolRuleCreatedAction, in.TenantID, "protocol_rule", in.RuleID, "tenant", "", in.CreatedBy, map[string]any{
		"protocol_version_id": in.ProtocolVersionID,
		"dose_code":           in.DoseCode,
		"sequence":            in.Sequence,
		"trigger_type":        in.TriggerType,
		"repeat":              in.Repeat,
		"catch_up":            in.CatchUp,
		"sort_order":          in.SortOrder,
	}, map[string]any{"idempotency_scope": "protocol.version.publish.derived_rule"}); err != nil {
		return err
	}
	return nil
}

// recordProtocolPublishedAudit writes the canonical {domain row + audit + outbox} audit_log entry for
// a publish, in the same transaction as the status flip and outbox insert, so the operational kernel
// invariant holds: every published version leaves a durable audit trail alongside its event.
func recordProtocolPublishedAudit(ctx context.Context, tx pgx.Tx, tenantID string, published protocolPublishedRow, publishedBy *string) error {
	actorType := "system_rule"
	actorID := ""
	if publishedBy != nil && strings.TrimSpace(*publishedBy) != "" {
		actorType = "human"
		actorID = strings.TrimSpace(*publishedBy)
	}
	scopeID := published.ScopeID
	if published.ScopeType == "tenant" {
		scopeID = ""
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    actorType,
		Action:       protocolPublishedEventType,
		ResourceType: protocolPublishedAggregateType,
		ResourceID:   published.VersionID,
		ScopeType:    published.ScopeType,
		ScopeID:      scopeID,
		AfterState: map[string]any{
			"status":      "published",
			"category":    published.Category,
			"protocol_id": published.ProtocolID,
			"scope_type":  published.ScopeType,
		},
		Metadata: map[string]any{
			"protocol_id":  published.ProtocolID,
			"category":     published.Category,
			"published_at": published.PublishedAt.UTC().Format(time.RFC3339Nano),
		},
		TraceID: "protocol:version:published:" + published.VersionID,
	}); err != nil {
		return fmt.Errorf("protocol: audit version published: %w", err)
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

type protocolRetiredRow struct {
	VersionID           string
	ProtocolID          string
	Category            string
	ScopeType           string
	ScopeID             string
	RetiredAt           time.Time
	ReplacedByVersionID string
}

func recordProtocolRetiredAudit(ctx context.Context, tx pgx.Tx, tenantID string, retired protocolRetiredRow, retiredBy *string) error {
	actorType := "system_rule"
	actorID := ""
	if retiredBy != nil && strings.TrimSpace(*retiredBy) != "" {
		actorType = "human"
		actorID = strings.TrimSpace(*retiredBy)
	}
	scopeID := retired.ScopeID
	if retired.ScopeType == "tenant" {
		scopeID = ""
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    actorType,
		Action:       protocolRetiredEventType,
		ResourceType: protocolPublishedAggregateType,
		ResourceID:   retired.VersionID,
		ScopeType:    retired.ScopeType,
		ScopeID:      scopeID,
		AfterState: map[string]any{
			"status":                          "retired",
			"category":                        retired.Category,
			"protocol_id":                     retired.ProtocolID,
			"scope_type":                      retired.ScopeType,
			"replaced_by_protocol_version_id": retired.ReplacedByVersionID,
		},
		Metadata: map[string]any{
			"protocol_id":                     retired.ProtocolID,
			"category":                        retired.Category,
			"retired_at":                      retired.RetiredAt.UTC().Format(time.RFC3339Nano),
			"replaced_by_protocol_version_id": retired.ReplacedByVersionID,
		},
		TraceID: "protocol:version:retired:" + retired.VersionID + ":by:" + retired.ReplacedByVersionID,
	}); err != nil {
		return fmt.Errorf("protocol: audit version retired: %w", err)
	}
	return nil
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
	)
	ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = 'protocol.version.published' DO NOTHING`, tenantID, eventID, protocolPublishedEventType, protocolPublishedSchemaVersion,
		protocolPublishedAggregateType, published.VersionID, protocolPublishedTopic, envelope, headers, idempotencyKey, traceID)
	if err != nil {
		return fmt.Errorf("protocol: insert published outbox: %w", err)
	}
	return nil
}

func insertProtocolRetiredOutbox(ctx context.Context, tx pgx.Tx, tenantID string, retired protocolRetiredRow, retiredBy *string) error {
	var eventID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&eventID); err != nil {
		return fmt.Errorf("protocol: generate retired event id: %w", err)
	}
	idempotencyKey := "protocol:version:retired:" + retired.VersionID + ":by:" + retired.ReplacedByVersionID
	traceID := idempotencyKey
	payload := map[string]any{
		"protocol_version_id":             retired.VersionID,
		"protocol_id":                     retired.ProtocolID,
		"category":                        retired.Category,
		"scope_type":                      retired.ScopeType,
		"scope_id":                        nullableString(retired.ScopeID),
		"retired_at":                      retired.RetiredAt.UTC().Format(time.RFC3339Nano),
		"replaced_by_protocol_version_id": retired.ReplacedByVersionID,
	}
	envelope, err := protocolRetiredEnvelope(tenantID, eventID, idempotencyKey, traceID, retired, retiredBy, payload)
	if err != nil {
		return err
	}
	headers, _ := json.Marshal(map[string]any{
		"protocol_version_id":             retired.VersionID,
		"category":                        retired.Category,
		"schema_version":                  protocolPublishedSchemaVersion,
		"replaced_by_protocol_version_id": retired.ReplacedByVersionID,
	})
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) SELECT
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, $11, 'pending', now()
WHERE NOT EXISTS (
  SELECT 1
  FROM outbox_messages
  WHERE tenant_id = $1::uuid
    AND event_type = $3
    AND idempotency_key = $10
)`, tenantID, eventID, protocolRetiredEventType, protocolPublishedSchemaVersion,
		protocolPublishedAggregateType, retired.VersionID, protocolPublishedTopic, envelope, headers, idempotencyKey, traceID)
	if err != nil {
		return fmt.Errorf("protocol: insert retired outbox: %w", err)
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

func protocolRetiredEnvelope(tenantID, eventID, idempotencyKey, traceID string, retired protocolRetiredRow, retiredBy *string, payload map[string]any) ([]byte, error) {
	actorType := "system_rule"
	if retiredBy != nil && strings.TrimSpace(*retiredBy) != "" {
		actorType = "human"
	}
	actorID := any(nil)
	if retiredBy != nil && strings.TrimSpace(*retiredBy) != "" {
		actorID = strings.TrimSpace(*retiredBy)
	}
	occurred := retired.RetiredAt.UTC().Format(time.RFC3339Nano)
	visibilityScope := map[string]any{"tenant_id": tenantID}
	if retired.ScopeType == "park" && retired.ScopeID != "" {
		visibilityScope["park_id"] = retired.ScopeID
	}
	return json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     protocolRetiredEventType,
		"schema_version": protocolPublishedSchemaVersion,
		"schema_ref":     protocolRetiredSchemaRef,
		"aggregate_type": protocolPublishedAggregateType,
		"aggregate_id":   retired.VersionID,
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
		"subject_id":       retired.VersionID,
		"visibility_scope": visibilityScope,
		"evidence_refs": []map[string]string{{
			"evidence_type": "event",
			"evidence_id":   retired.VersionID,
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

// ListPublishedVaccinationVersions returns the ids of ALL published vaccination protocol versions
// (sweeper/backfill: open obligations may have been generated under any published version).
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

// ListEffectiveVaccinationVersionsForGoat returns one published vaccination version id per protocol —
// the version effective as of asOf that covers a goat in parkID (its park-scoped override when one is
// effective, else the tenant default). Per-goat SM-1 (goat.created) uses this so a new goat is generated
// only against each protocol's currently-active version for its scope, never a superseded version and
// never two coexisting scopes. parkID is empty for a goat with no park (matches tenant-default only).
func (r *Repository) ListEffectiveVaccinationVersionsForGoat(ctx context.Context, tenantID, parkID string, asOf time.Time) ([]string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("protocol: tenant id: %w", err)
	}
	park := pgtype.UUID{} // NULL → only tenant-default versions match
	if parkID != "" {
		park, err = pgconv.UUID(parkID)
		if err != nil {
			return nil, fmt.Errorf("protocol: park id: %w", err)
		}
	}
	ids, err := r.queries.ListEffectiveVaccinationVersionsForGoat(ctx, protocoldb.ListEffectiveVaccinationVersionsForGoatParams{
		TenantID: tenant,
		AsOf:     pgconv.Timestamptz(asOf),
		ParkID:   park,
	})
	if err != nil {
		return nil, fmt.Errorf("protocol: list effective vaccination versions for goat: %w", err)
	}
	return ids, nil
}

// ListEffectiveVaccinationVersionsForParks returns the effective vaccination version ids for every
// park scope in a generation page. Keys match the supplied park ids; the empty key is tenant scope.
func (r *Repository) ListEffectiveVaccinationVersionsForParks(ctx context.Context, tenantID string, parkIDs []string, asOf time.Time) (map[string][]string, error) {
	out := make(map[string][]string, len(parkIDs))
	if len(parkIDs) == 0 {
		return out, nil
	}
	for _, parkID := range parkIDs {
		out[parkID] = nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("protocol: tenant id: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
WITH input_parks AS (
  SELECT DISTINCT
         COALESCE(NULLIF(park_id_text, ''), '') AS park_key,
         NULLIF(park_id_text, '')::uuid AS park_id
  FROM unnest($2::text[]) AS input(park_id_text)
),
operating_timezone AS (
  SELECT 'Asia/Kolkata'::text AS timezone
),
effective_clock AS (
  SELECT ($3::timestamptz AT TIME ZONE ot.timezone)::date AS business_date
  FROM operating_timezone ot
),
picked AS (
  SELECT DISTINCT ON (ip.park_key, pv.protocol_id)
         ip.park_key,
         pv.protocol_id,
         pv.protocol_version_id::text AS protocol_version_id,
         (pv.scope_type = 'park') AS park_specific,
         pv.effective_from
  FROM input_parks ip
  JOIN protocol_versions pv
    ON pv.tenant_id = $1
  JOIN protocol_definitions pd
    ON pd.tenant_id = pv.tenant_id
   AND pd.protocol_id = pv.protocol_id
  CROSS JOIN effective_clock ec
  WHERE pv.status = 'published'
    AND pd.category = 'vaccination'
    AND pv.effective_from <= ec.business_date
    AND (pv.effective_to IS NULL OR pv.effective_to > ec.business_date)
    AND (
      pv.scope_type = 'tenant'
      OR (ip.park_id IS NOT NULL AND pv.scope_type = 'park' AND pv.scope_id = ip.park_id)
    )
  ORDER BY ip.park_key,
           pv.protocol_id,
           (pv.scope_type = 'park') DESC,
           pv.effective_from DESC,
           pv.protocol_version_id DESC
)
SELECT park_key, protocol_version_id
FROM picked
ORDER BY park_key, protocol_id`, tenant, parkIDs, pgconv.Timestamptz(asOf))
	if err != nil {
		return nil, fmt.Errorf("protocol: list effective vaccination versions for parks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var parkID, versionID string
		if err := rows.Scan(&parkID, &versionID); err != nil {
			return nil, fmt.Errorf("protocol: scan effective vaccination versions for parks: %w", err)
		}
		out[parkID] = append(out[parkID], versionID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("protocol: list effective vaccination versions for parks: %w", err)
	}
	return out, nil
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
	fingerprint := protocolFingerprint(
		"rule",
		in.TenantID,
		in.ProtocolVersionID,
		strings.TrimSpace(in.DoseCode),
		fmt.Sprint(in.Sequence),
		strings.TrimSpace(in.TriggerType),
		fmt.Sprint(in.OffsetDays),
		fmt.Sprint(in.DueWindowDays),
		fmt.Sprint(in.MinGapDays),
		strings.TrimSpace(in.Repeat),
		strings.TrimSpace(in.RepeatUntilAfterAge),
		strings.TrimSpace(in.CatchUp),
		canonicalJSON(in.EligibilityJSON),
		optionalString(in.SopVersionID),
		canonicalJSON(in.ProofPolicy),
		optionalInt32(in.WithdrawalDays),
		fmt.Sprint(in.SortOrder),
	)
	key := protocolIdempotencyKey(in.IdempotencyKey, fingerprint)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("protocol: begin create rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reservation, err := reserveProtocolIdempotency(ctx, tx, in.TenantID, "protocol.rule.create", key, fingerprint, "protocol_rule")
	if err != nil {
		return "", fmt.Errorf("protocol: reserve rule idempotency: %w", err)
	}
	if !reservation.proceed {
		return reservation.resultID, nil
	}

	qtx := r.queries.WithTx(tx)
	id, err := qtx.CreateProtocolRule(ctx, protocoldb.CreateProtocolRuleParams{
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
	if err := recordProtocolCreateAudit(ctx, tx, protocolRuleCreatedAction, in.TenantID, "protocol_rule", id, "tenant", "", in.CreatedBy, map[string]any{
		"protocol_version_id": in.ProtocolVersionID,
		"dose_code":           in.DoseCode,
		"sequence":            in.Sequence,
		"trigger_type":        in.TriggerType,
		"repeat":              in.Repeat,
		"catch_up":            in.CatchUp,
		"sort_order":          in.SortOrder,
	}, map[string]any{"idempotency_scope": "protocol.rule.create"}); err != nil {
		return "", err
	}
	if err := completeProtocolIdempotency(ctx, tx, in.TenantID, "protocol.rule.create", key, "protocol_rule", id); err != nil {
		return "", fmt.Errorf("protocol: complete rule idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("protocol: commit create rule: %w", err)
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
			ProtocolVersionID:   row.ProtocolVersionID,
			ProtocolID:          row.ProtocolID,
			DoseCode:            row.DoseCode,
			Sequence:            row.Sequence,
			TriggerType:         row.TriggerType,
			OffsetDays:          row.OffsetDays,
			DueWindowDays:       row.DueWindowDays,
			MinGapDays:          row.MinGapDays,
			Repeat:              row.Repeat,
			RepeatUntilAfterAge: row.RepeatUntilAfterAge,
			CatchUp:             row.CatchUp,
			EligibilityJSON:     row.EligibilityJson,
			SopVersionID:        row.SopVersionID,
			SortOrder:           row.SortOrder,
		})
	}
	return out, nil
}

// ListRulesForVersions returns ordered protocol rules for a bounded page of version ids.
func (r *Repository) ListRulesForVersions(ctx context.Context, tenantID string, versionIDs []string) (map[string][]domain.Rule, error) {
	out := make(map[string][]domain.Rule, len(versionIDs))
	if len(versionIDs) == 0 {
		return out, nil
	}
	for _, versionID := range versionIDs {
		out[versionID] = nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("protocol: tenant id: %w", err)
	}
	ids, err := pgconv.UUIDs(versionIDs)
	if err != nil {
		return nil, fmt.Errorf("protocol: version ids: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
SELECT pr.rule_id::text AS rule_id,
       pr.protocol_version_id::text AS protocol_version_id,
       pv.protocol_id::text AS protocol_id,
       pr.dose_code,
       pr."sequence",
       pr.trigger_type,
       pr.offset_days,
       pr.due_window_days,
       pr.min_gap_days,
       pr."repeat",
       COALESCE(pr.repeat_until_after_age, '')::text AS repeat_until_after_age,
       pr.catch_up,
       pr.eligibility_json,
       COALESCE(pr.sop_version_id::text, '')::text AS sop_version_id,
       pr.sort_order
FROM protocol_rules pr
JOIN protocol_versions pv
  ON pv.tenant_id = pr.tenant_id
 AND pv.protocol_version_id = pr.protocol_version_id
WHERE pr.tenant_id = $1
  AND pr.protocol_version_id = ANY($2::uuid[])
ORDER BY array_position($2::uuid[], pr.protocol_version_id), pr.sort_order ASC, pr."sequence" ASC`, tenant, ids)
	if err != nil {
		return nil, fmt.Errorf("protocol: list rules for versions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row domain.Rule
		if err := rows.Scan(
			&row.RuleID,
			&row.ProtocolVersionID,
			&row.ProtocolID,
			&row.DoseCode,
			&row.Sequence,
			&row.TriggerType,
			&row.OffsetDays,
			&row.DueWindowDays,
			&row.MinGapDays,
			&row.Repeat,
			&row.RepeatUntilAfterAge,
			&row.CatchUp,
			&row.EligibilityJSON,
			&row.SopVersionID,
			&row.SortOrder,
		); err != nil {
			return nil, fmt.Errorf("protocol: scan rules for versions: %w", err)
		}
		out[row.ProtocolVersionID] = append(out[row.ProtocolVersionID], row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("protocol: list rules for versions: %w", err)
	}
	return out, nil
}

// ReplaceProtocolRuleDimensions rewrites the compiled selector rows for one protocol version.
// The caller owns DSL validation; this method only makes the materialized table atomic.
func (r *Repository) ReplaceProtocolRuleDimensions(ctx context.Context, tenantID, versionID string, dimensions []domain.RuleDimension) error {
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
		return fmt.Errorf("protocol: begin replace rule dimensions: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
DELETE FROM protocol_rule_dimensions
WHERE tenant_id = $1 AND protocol_version_id = $2`, tenant, vid); err != nil {
		return fmt.Errorf("protocol: delete rule dimensions: %w", err)
	}
	if err := insertProtocolRuleDimensionsTx(ctx, tx, tenant, vid, dimensions); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("protocol: commit replace rule dimensions: %w", err)
	}
	return nil
}

const insertProtocolRuleDimensionSQL = `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, ruleset_family, matrix_row_id, selector_key,
  dose_code, source_dose_code, vaccine_code, vaccine_type, pathogen_class, compatibility_group,
  species, animal_stage, sex, breed, lifecycle, health, reproductive, min_age_days, max_age_days,
  trigger_type, sequence, offset_days, due_window_days, min_gap_days, repeat, catch_up,
  max_delay_days, revaccination_interval_days, eligibility_json, vaccine_json, schedule_json
) VALUES (
  $1, $2, $3, $4, $5, $6, $7,
  $8, $9, $10, $11, $12, $13,
  $14, $15, $16, $17, $18, $19, $20, $21, $22,
	$23, $24, $25, $26, $27, $28, $29,
	$30, $31, $32, $33, $34
)`

// insertProtocolRuleDimensionsTx preserves the all-or-nothing publish transaction while sending
// the compiled execution index in one pgx batch. Matrix publication previously paid one network
// round trip per selector row, which made valid matrices exceed the repository operation deadline
// over Cloud SQL even though every individual INSERT was small.
func insertProtocolRuleDimensionsTx(ctx context.Context, tx pgx.Tx, tenant pgtype.UUID, vid pgtype.UUID, dimensions []domain.RuleDimension) error {
	if len(dimensions) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, dim := range dimensions {
		ruleID, err := pgconv.UUID(dim.RuleID)
		if err != nil {
			return fmt.Errorf("protocol: dimension rule id %q: %w", dim.RuleID, err)
		}
		batch.Queue(insertProtocolRuleDimensionSQL, tenant, vid, ruleID,
			dim.Category, dim.RulesetFamily, dim.MatrixRowID, dim.SelectorKey,
			dim.DoseCode, dim.SourceDoseCode, dim.VaccineCode, dim.VaccineType, dim.PathogenClass, dim.CompatibilityGroup,
			dim.Species, dim.AnimalStage, dim.Sex, dim.Breed, dim.Lifecycle, dim.Health, dim.Reproductive, pgconv.Int4(dim.MinAgeDays), pgconv.Int4(dim.MaxAgeDays),
			dim.TriggerType, dim.Sequence, dim.OffsetDays, dim.DueWindowDays, dim.MinGapDays, dim.Repeat, dim.CatchUp,
			dim.MaxDelayDays, dim.RevaccinationIntervalDays, pgconv.JSONB(defaultJSON(dim.EligibilityJSON)), pgconv.JSONB(defaultJSON(dim.VaccineJSON)), pgconv.JSONB(defaultJSON(dim.ScheduleJSON)),
		)
	}
	results := tx.SendBatch(ctx, batch)
	for idx, dim := range dimensions {
		// scale-guard:ignore: consumes already-queued pgx batch results; this does not issue one network round trip per dimension.
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("protocol: insert rule dimension %s (batch index %d): %w", dim.SelectorKey, idx, err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("protocol: close rule dimension insert batch: %w", err)
	}
	return nil
}

func defaultJSON(raw []byte) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	return raw
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
			AnimalStageID:      row.AnimalStageID,
			StageCode:          row.StageCode,
			Name:               row.Name,
			AgeBand:            row.AgeBand.String,
			AssignableAsCohort: !domain.IsClinicalStage(row.StageCode),
			SortOrder:          row.SortOrder,
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

// SyncVaccinationCapacityConfig upserts the operational daily-vaccination-capacity read model from a
// published version's rule_dsl.capacity and returns the stored row for a post-publish parity check.
// vaccination_capacity_config is a DERIVED read model (the session-splitting planner reads it); publish
// is the only writer. Keyed by tenant_id (one operational cap per tenant).
func (r *Repository) SyncVaccinationCapacityConfig(ctx context.Context, tenantID string, want domain.PublishedCapacity) (domain.PublishedCapacity, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.PublishedCapacity{}, fmt.Errorf("protocol: tenant id: %w", err)
	}
	var got domain.PublishedCapacity
	err = r.pool.QueryRow(ctx, `
INSERT INTO vaccination_capacity_config (tenant_id, max_per_day, capacity_scope, max_buffer_days, overflow_policy)
VALUES ($1::uuid, $2, $3, $4, $5)
ON CONFLICT (tenant_id) DO UPDATE SET
  max_per_day = EXCLUDED.max_per_day,
  capacity_scope = EXCLUDED.capacity_scope,
  max_buffer_days = EXCLUDED.max_buffer_days,
  overflow_policy = EXCLUDED.overflow_policy,
  row_version = vaccination_capacity_config.row_version + 1,
  updated_at = now()
RETURNING max_per_day, max_buffer_days, capacity_scope, overflow_policy`,
		tenant, want.MaxPerDay, want.CapacityScope, want.MaxBufferDays, want.OverflowPolicy).
		Scan(&got.MaxPerDay, &got.MaxBufferDays, &got.CapacityScope, &got.OverflowPolicy)
	if err != nil {
		return domain.PublishedCapacity{}, fmt.Errorf("protocol: sync vaccination capacity config: %w", err)
	}
	return got, nil
}
