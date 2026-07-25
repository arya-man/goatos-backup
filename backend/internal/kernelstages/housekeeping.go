package kernelstages

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	domainconsumerpg "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	proofpg "github.com/vgoats/goatos/backend/internal/proof/adapters/postgres"
)

const defaultProcessedEventRetention = 14 * 24 * time.Hour

// ProcessedEventSweeperStage deletes old terminal rows from
// domain_event_processed_events, reusing
// domainconsumerpg.ProcessedEventStore.SweepProcessedBefore — the same code path
// as the domain-event-processed-sweeper one-shot. Daily housekeeping cadence.
type ProcessedEventSweeperStage struct {
	store     *domainconsumerpg.ProcessedEventStore
	tenantID  string
	limit     int
	retention time.Duration
	logger    *slog.Logger
}

// NewProcessedEventSweeperStage builds the processed-event housekeeping stage.
// An empty tenant id sweeps all tenants.
func NewProcessedEventSweeperStage(deps Deps, tenantID string) *ProcessedEventSweeperStage {
	limit := intEnv("GOATOS_DOMAIN_EVENT_PROCESSED_SWEEPER_LIMIT", 1000)
	if limit < 1 || limit > 5000 {
		limit = 1000
	}
	retention := durationEnv("GOATOS_DOMAIN_EVENT_PROCESSED_RETENTION", defaultProcessedEventRetention)
	return &ProcessedEventSweeperStage{
		store:     domainconsumerpg.NewProcessedEventStore(deps.Pool, deps.PgCfg.QueryTimeout),
		tenantID:  strings.TrimSpace(tenantID),
		limit:     limit,
		retention: retention,
		logger:    deps.Logger,
	}
}

// Name implements worker.StageRunner.
func (s *ProcessedEventSweeperStage) Name() string { return "domain-event-processed-sweeper" }

// Run deletes processed events older than the retention window.
func (s *ProcessedEventSweeperStage) Run(ctx context.Context) error {
	before := time.Now().UTC().Add(-s.retention)
	count, err := s.store.SweepProcessedBefore(ctx, s.tenantID, before, s.limit, false)
	if err != nil {
		return fmt.Errorf("sweep processed events: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("domain_event_processed_sweep_stage_complete", "deleted", count, "before", before.Format(time.RFC3339))
	}
	return nil
}

// IdempotencyKeySweeperStage deletes expired idempotency_keys with a
// keyset-chunked FOR UPDATE SKIP LOCKED claim — the same query as the
// idempotency-key-sweeper one-shot. Daily housekeeping cadence.
type IdempotencyKeySweeperStage struct {
	pool     *pgxpool.Pool
	tenantID string
	limit    int
	logger   *slog.Logger
}

// NewIdempotencyKeySweeperStage builds the idempotency-key housekeeping stage.
// An empty tenant id sweeps all tenants.
func NewIdempotencyKeySweeperStage(deps Deps, tenantID string) *IdempotencyKeySweeperStage {
	limit := intEnv("GOATOS_IDEMPOTENCY_SWEEPER_LIMIT", 1000)
	if limit < 1 || limit > 5000 {
		limit = 1000
	}
	return &IdempotencyKeySweeperStage{
		pool:     deps.Pool,
		tenantID: strings.TrimSpace(tenantID),
		limit:    limit,
		logger:   deps.Logger,
	}
}

// Name implements worker.StageRunner.
func (s *IdempotencyKeySweeperStage) Name() string { return "idempotency-key-sweeper" }

// Run deletes idempotency keys whose expiry has passed.
func (s *IdempotencyKeySweeperStage) Run(ctx context.Context) error {
	before := time.Now().UTC()
	var tenant any
	if s.tenantID != "" {
		uuid, err := pgconv.UUID(s.tenantID)
		if err != nil {
			return fmt.Errorf("tenant-id must be a uuid: %w", err)
		}
		tenant = uuid
	}
	tag, err := s.pool.Exec(ctx, `
WITH expired AS (
  SELECT idempotency_key
  FROM idempotency_keys
  WHERE expires_at IS NOT NULL
    AND expires_at <= $1::timestamptz
    AND ($2::uuid IS NULL OR tenant_id = $2::uuid)
  ORDER BY expires_at ASC, idempotency_key ASC
  LIMIT $3
  FOR UPDATE SKIP LOCKED
)
DELETE FROM idempotency_keys k
USING expired
WHERE k.idempotency_key = expired.idempotency_key`, before, tenant, s.limit)
	if err != nil {
		return fmt.Errorf("delete expired idempotency keys: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("idempotency_key_sweep_stage_complete", "deleted", tag.RowsAffected(), "before", before.Format(time.RFC3339))
	}
	return nil
}

// ProofRetentionSweeperStage deletes completed proof rows past their retention
// boundary and abandoned upload registrations whose direct/chunked upload never
// completed. Object-store lifecycle rules still own physical media deletion.
type ProofRetentionSweeperStage struct {
	repo   *proofpg.Repository
	limit  int
	logger *slog.Logger
}

func NewProofRetentionSweeperStage(deps Deps) *ProofRetentionSweeperStage {
	limit := intEnv("GOATOS_PROOF_RETENTION_SWEEPER_LIMIT", 1000)
	if limit < 1 || limit > 5000 {
		limit = 1000
	}
	return &ProofRetentionSweeperStage{
		repo:   proofpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		limit:  limit,
		logger: deps.Logger,
	}
}

func (s *ProofRetentionSweeperStage) Name() string { return "proof-retention-sweeper" }

func (s *ProofRetentionSweeperStage) Run(ctx context.Context) error {
	before := time.Now().UTC()
	expired, err := s.repo.PurgeExpired(ctx, before, s.limit)
	if err != nil {
		return fmt.Errorf("delete expired proof artifacts: %w", err)
	}
	abandoned, err := s.repo.PurgeAbandonedUploads(ctx, before, s.limit)
	if err != nil {
		return fmt.Errorf("delete abandoned proof uploads: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("proof_retention_sweep_stage_complete", "expired", expired, "abandoned_uploads", abandoned, "before", before.Format(time.RFC3339))
	}
	return nil
}
