// Package postgres implements the feed-direction Repository over generated sqlc queries.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	feeddb "github.com/vgoats/goatos/backend/internal/feed/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/feed/domain"
	"github.com/vgoats/goatos/backend/internal/feed/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
)

const defaultQueryTimeout = 3 * time.Second

// Repository is the Postgres-backed feed-direction repository.
type Repository struct {
	pool         *pgxpool.Pool
	queries      *feeddb.Queries
	queryTimeout time.Duration
}

// NewRepository builds a Repository bound to a pgx pool.
func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, queries: feeddb.New(pool), queryTimeout: queryTimeout}
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

// RecordDirection appends an idempotent shed feed-direction execution.
func (r *Repository) RecordDirection(ctx context.Context, in domain.NewDirection) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", false, fmt.Errorf("feed: tenant id: %w", err)
	}
	obligation, err := pgconv.UUID(in.ObligationID)
	if err != nil {
		return "", false, fmt.Errorf("feed: obligation id: %w", err)
	}
	shed, err := pgconv.UUID(in.ShedID)
	if err != nil {
		return "", false, fmt.Errorf("feed: shed id: %w", err)
	}
	qty, err := pgconv.Numeric(in.QuantityFed)
	if err != nil {
		return "", false, fmt.Errorf("feed: quantity_fed: %w", err)
	}
	status := in.Status
	if status == "" {
		status = "recorded"
	}
	id, err := r.queries.RecordFeedDirection(ctx, feeddb.RecordFeedDirectionParams{
		TenantID:                tenant,
		ObligationID:            obligation,
		BatchID:                 pgconv.NullableUUID(in.BatchID),
		ShedID:                  shed,
		RationProtocolVersionID: pgconv.NullableUUID(in.RationProtocolVersionID),
		SopSubmissionItemID:     pgconv.NullableUUID(in.SopSubmissionItemID),
		FeedInventoryLotID:      pgconv.NullableUUID(in.FeedInventoryLotID),
		QuantityFed:             qty,
		QuantityUnit:            pgconv.Text(in.QuantityUnit),
		HeadCount:               pgconv.Int4(in.HeadCount),
		FedAt:                   pgconv.Timestamptz(in.FedAt),
		Status:                  status,
		RecordedBy:              pgconv.NullableUUID(in.RecordedBy),
		IdempotencyKey:          in.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil // replay (idempotency conflict)
	}
	if err != nil {
		return "", false, fmt.Errorf("feed: record direction: %w", err)
	}
	return id, true, nil
}

// AcceptDirection accepts a recorded direction on verification, returning its verification context
// (idempotent: applied is false on replay).
func (r *Repository) AcceptDirection(ctx context.Context, tenantID, completionID string, verifiedBy *string) (domain.AcceptedDirection, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.AcceptedDirection{}, false, fmt.Errorf("feed: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return domain.AcceptedDirection{}, false, fmt.Errorf("feed: completion id: %w", err)
	}
	rows, err := r.queries.AcceptFeedDirection(ctx, feeddb.AcceptFeedDirectionParams{
		VerifiedBy:   pgconv.NullableUUID(verifiedBy),
		TenantID:     tenant,
		CompletionID: cid,
	})
	if err != nil {
		return domain.AcceptedDirection{}, false, fmt.Errorf("feed: accept direction: %w", err)
	}
	if len(rows) == 0 {
		return domain.AcceptedDirection{}, false, nil // already verified
	}
	row := rows[0]
	return domain.AcceptedDirection{
		ObligationID:       row.ObligationID,
		ShedID:             row.ShedID,
		BatchID:            row.BatchID,
		FeedInventoryLotID: row.FeedInventoryLotID,
		QuantityFed:        row.QuantityFed,
		QuantityUnit:       row.QuantityUnit,
		FedAt:              row.FedAt.Time,
	}, true, nil
}

// RejectDirection marks a recorded direction rejected (rework). applied is false on replay.
func (r *Repository) RejectDirection(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return false, fmt.Errorf("feed: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return false, fmt.Errorf("feed: completion id: %w", err)
	}
	n, err := r.queries.RejectFeedDirection(ctx, feeddb.RejectFeedDirectionParams{
		VerifiedBy:      pgconv.NullableUUID(verifiedBy),
		RejectionReason: pgconv.Text(reason),
		TenantID:        tenant,
		CompletionID:    cid,
	})
	if err != nil {
		return false, fmt.Errorf("feed: reject direction: %w", err)
	}
	return n == 1, nil
}

// ListDirectionsByShed returns a shed's feed history (most recent first).
func (r *Repository) ListDirectionsByShed(ctx context.Context, tenantID, shedID string, limit int32) ([]domain.DirectionHistoryItem, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("feed: tenant id: %w", err)
	}
	shed, err := pgconv.UUID(shedID)
	if err != nil {
		return nil, fmt.Errorf("feed: shed id: %w", err)
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.queries.ListFeedDirectionsByShed(ctx, feeddb.ListFeedDirectionsByShedParams{
		TenantID: tenant, ShedID: shed, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("feed: list by shed: %w", err)
	}
	out := make([]domain.DirectionHistoryItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.DirectionHistoryItem{
			CompletionID: row.CompletionID,
			ObligationID: row.ObligationID,
			BatchID:      row.BatchID,
			FedAt:        row.FedAt.Time,
			Status:       row.Status,
			QuantityFed:  row.QuantityFed,
			QuantityUnit: row.QuantityUnit,
			HeadCount:    row.HeadCount,
		})
	}
	return out, nil
}

// ListRecordedDirections returns directions awaiting review (status='recorded'), earliest fed first.
func (r *Repository) ListRecordedDirections(ctx context.Context, tenantID string, limit int32) ([]domain.RecordedDirection, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("feed: tenant id: %w", err)
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.queries.ListRecordedFeedDirections(ctx, feeddb.ListRecordedFeedDirectionsParams{
		TenantID: tenant, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("feed: list recorded: %w", err)
	}
	out := make([]domain.RecordedDirection, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.RecordedDirection{
			CompletionID: row.CompletionID,
			ObligationID: row.ObligationID,
			ShedID:       row.ShedID,
			BatchID:      row.BatchID,
			FedAt:        row.FedAt.Time,
			QuantityFed:  row.QuantityFed,
			QuantityUnit: row.QuantityUnit,
			HeadCount:    row.HeadCount,
		})
	}
	return out, nil
}
