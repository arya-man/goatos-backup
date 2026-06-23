// Package postgres implements the vaccination Repository over generated sqlc queries.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	vaccinationdb "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

const defaultQueryTimeout = 3 * time.Second

// Repository is the Postgres-backed vaccination repository.
type Repository struct {
	pool         *pgxpool.Pool
	queries      *vaccinationdb.Queries
	queryTimeout time.Duration
}

// NewRepository builds a Repository bound to a pgx pool.
func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, queries: vaccinationdb.New(pool), queryTimeout: queryTimeout}
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

// RecordCompletion appends an idempotent dose-administered record.
func (r *Repository) RecordCompletion(ctx context.Context, in domain.NewCompletion) (string, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	obligation, err := pgconv.UUID(in.ObligationID)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: obligation id: %w", err)
	}
	goat, err := pgconv.UUID(in.GoatID)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: goat id: %w", err)
	}
	doseML, err := pgconv.Numeric(in.DoseMlGiven)
	if err != nil {
		return "", false, fmt.Errorf("vaccination: dose_ml_given: %w", err)
	}
	status := in.Status
	if status == "" {
		status = "recorded"
	}
	id, err := r.queries.RecordVaccinationCompletion(ctx, vaccinationdb.RecordVaccinationCompletionParams{
		TenantID:                 tenant,
		ObligationID:             obligation,
		BatchID:                  pgconv.NullableUUID(in.BatchID),
		GoatID:                   goat,
		SopSubmissionItemID:      pgconv.NullableUUID(in.SopSubmissionItemID),
		VaccineInventoryLotID:    pgconv.NullableUUID(in.VaccineInventoryLotID),
		Doses:                    pgconv.Int4(in.Doses),
		DoseMlGiven:              doseML,
		RouteSite:                pgconv.Text(in.RouteSite),
		AdverseReaction:          in.AdverseReaction,
		AdverseReactionProblemID: pgconv.NullableUUID(in.AdverseReactionProblemID),
		ColdChainVerified:        in.ColdChainVerified,
		AdministeredAt:           pgconv.Timestamptz(in.AdministeredAt),
		Status:                   status,
		WithdrawalUntilDate:      pgconv.Date(in.WithdrawalUntilDate),
		RecordedBy:               pgconv.NullableUUID(in.RecordedBy),
		IdempotencyKey:           in.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil // already recorded for this idempotency key
	}
	if err != nil {
		return "", false, fmt.Errorf("vaccination: record completion: %w", err)
	}
	return id, true, nil
}

// AcceptCompletion marks a recorded completion accepted (SM-5).
func (r *Repository) AcceptCompletion(ctx context.Context, tenantID, completionID string, verifiedBy *string, withdrawalUntil *time.Time) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("vaccination: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return fmt.Errorf("vaccination: completion id: %w", err)
	}
	if err := r.queries.AcceptVaccinationCompletion(ctx, vaccinationdb.AcceptVaccinationCompletionParams{
		VerifiedBy:          pgconv.NullableUUID(verifiedBy),
		WithdrawalUntilDate: pgconv.Date(withdrawalUntil),
		TenantID:            tenant,
		CompletionID:        cid,
	}); err != nil {
		return fmt.Errorf("vaccination: accept completion: %w", err)
	}
	return nil
}

// RejectCompletion marks a recorded completion rejected (SM-5 rework).
func (r *Repository) RejectCompletion(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return fmt.Errorf("vaccination: tenant id: %w", err)
	}
	cid, err := pgconv.UUID(completionID)
	if err != nil {
		return fmt.Errorf("vaccination: completion id: %w", err)
	}
	if err := r.queries.RejectVaccinationCompletion(ctx, vaccinationdb.RejectVaccinationCompletionParams{
		VerifiedBy:      pgconv.NullableUUID(verifiedBy),
		RejectionReason: pgconv.Text(reason),
		TenantID:        tenant,
		CompletionID:    cid,
	}); err != nil {
		return fmt.Errorf("vaccination: reject completion: %w", err)
	}
	return nil
}

// ListCompletionsByGoat returns a goat's vaccination history (most recent first).
func (r *Repository) ListCompletionsByGoat(ctx context.Context, tenantID, goatID string, limit int32) ([]domain.CompletionHistoryItem, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return nil, fmt.Errorf("vaccination: goat id: %w", err)
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.queries.ListVaccinationCompletionsByGoat(ctx, vaccinationdb.ListVaccinationCompletionsByGoatParams{
		TenantID: tenant, GoatID: goat, RowLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("vaccination: list completions: %w", err)
	}
	out := make([]domain.CompletionHistoryItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.CompletionHistoryItem{
			CompletionID:        row.CompletionID,
			ObligationID:        row.ObligationID,
			BatchID:             row.BatchID,
			AdministeredAt:      row.AdministeredAt.Time,
			Status:              row.Status,
			Doses:               row.Doses,
			RouteSite:           row.RouteSite,
			AdverseReaction:     row.AdverseReaction,
			WithdrawalUntilDate: pgconv.DateValue(row.WithdrawalUntilDate),
		})
	}
	return out, nil
}

// GetLastAcceptedForGoat returns the most recent accepted administration (found=false when none).
func (r *Repository) GetLastAcceptedForGoat(ctx context.Context, tenantID, goatID string) (domain.LastAccepted, bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return domain.LastAccepted{}, false, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	goat, err := pgconv.UUID(goatID)
	if err != nil {
		return domain.LastAccepted{}, false, fmt.Errorf("vaccination: goat id: %w", err)
	}
	row, err := r.queries.GetLastAcceptedCompletionForGoat(ctx, vaccinationdb.GetLastAcceptedCompletionForGoatParams{TenantID: tenant, GoatID: goat})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LastAccepted{}, false, nil
	}
	if err != nil {
		return domain.LastAccepted{}, false, fmt.Errorf("vaccination: last accepted: %w", err)
	}
	return domain.LastAccepted{
		CompletionID:   row.CompletionID,
		ObligationID:   row.ObligationID,
		AdministeredAt: row.AdministeredAt.Time,
	}, true, nil
}

func (r *Repository) eligParams(f domain.ImpactFilter) (vaccinationdb.CountEligibleGoatsParams, error) {
	tenant, err := pgconv.UUID(f.TenantID)
	if err != nil {
		return vaccinationdb.CountEligibleGoatsParams{}, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	return vaccinationdb.CountEligibleGoatsParams{
		TenantID: tenant,
		Stage:    f.Stage,
		Sex:      f.Sex,
		Breed:    f.Breed,
		Health:   f.Health,
		ParkID:   pgconv.NullableUUID(f.ParkID),
	}, nil
}

// CountEligibleGoats counts alive goats matching the eligibility filter.
func (r *Repository) CountEligibleGoats(ctx context.Context, f domain.ImpactFilter) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	p, err := r.eligParams(f)
	if err != nil {
		return 0, err
	}
	n, err := r.queries.CountEligibleGoats(ctx, p)
	if err != nil {
		return 0, fmt.Errorf("vaccination: count eligible: %w", err)
	}
	return n, nil
}

// CountCatchupGoats counts eligible goats with a prior accepted completion.
func (r *Repository) CountCatchupGoats(ctx context.Context, f domain.ImpactFilter) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	p, err := r.eligParams(f)
	if err != nil {
		return 0, err
	}
	n, err := r.queries.CountCatchupGoats(ctx, vaccinationdb.CountCatchupGoatsParams(p))
	if err != nil {
		return 0, fmt.Errorf("vaccination: count catchup: %w", err)
	}
	return n, nil
}

// CountEligibleShedScopes counts distinct sheds holding eligible goats (≈ drive batches).
func (r *Repository) CountEligibleShedScopes(ctx context.Context, f domain.ImpactFilter) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	p, err := r.eligParams(f)
	if err != nil {
		return 0, err
	}
	n, err := r.queries.CountEligibleShedScopes(ctx, vaccinationdb.CountEligibleShedScopesParams(p))
	if err != nil {
		return 0, fmt.Errorf("vaccination: count shed scopes: %w", err)
	}
	return n, nil
}

// SumAvailableStock returns available (unreserved) quantity + earliest expiry for an item.
func (r *Repository) SumAvailableStock(ctx context.Context, tenantID, itemID string, locationID *string) (string, *time.Time, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return "", nil, fmt.Errorf("vaccination: tenant id: %w", err)
	}
	item, err := pgconv.UUID(itemID)
	if err != nil {
		return "", nil, fmt.Errorf("vaccination: item id: %w", err)
	}
	row, err := r.queries.SumAvailableStockForItem(ctx, vaccinationdb.SumAvailableStockForItemParams{
		TenantID:   tenant,
		ItemID:     item,
		LocationID: pgconv.NullableUUID(locationID),
	})
	if err != nil {
		return "", nil, fmt.Errorf("vaccination: sum available stock: %w", err)
	}
	return pgconv.NumericString(row.Available), pgconv.DateValue(row.EarliestExpiry), nil
}
