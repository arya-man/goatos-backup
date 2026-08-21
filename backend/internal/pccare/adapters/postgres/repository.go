// Package postgres owns the PC Care module's three tables (pc_care_tasks,
// pc_care_task_assignees, pc_care_task_animals). Every read is set-based and bounded by the
// park's pen catalog, the task list page, or one task's animals — never by herd size. Every
// write is one transaction carrying the canonical row(s), audit, and request-level idempotency.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// Repository implements ports.TaskStore over the pc_care_* tables.
type Repository struct {
	pool      *pgxpool.Pool
	txTimeout time.Duration
}

// NewRepository constructs the store.
func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Repository{pool: pool, txTimeout: timeout}
}

var _ ports.TaskStore = (*Repository)(nil)

func (r *Repository) timeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.txTimeout)
}

// requireShedPartitionInPark asserts the physical shed belongs to the park and the requested
// partition identity matches the catalog: a partitioned shed fails closed on a blank label, an
// undivided shed fails closed on a fabricated one (feeddirection requireShedPartitionInPark
// clone — the partition catalog is shed_partitions, never a per-goat table).
func requireShedPartitionInPark(ctx context.Context, tx pgx.Tx, tenantID, parkID, shedID, partitionLabel string) error {
	var ok bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM locations shed
  WHERE shed.tenant_id = $1::uuid
    AND shed.location_id = $3::uuid
    AND shed.parent_location_id = $2::uuid
    AND shed.location_type = 'shed'
    AND shed.status = 'active'
    AND shed.retired_at IS NULL
)`, tenantID, parkID, shedID).Scan(&ok); err != nil {
		return fmt.Errorf("pccare: resolve shed in park: %w", err)
	}
	if !ok {
		return ports.ErrShedNotInPark
	}

	normalizedRequested := domain.PartitionMatchKey(partitionLabel)
	rows, err := tx.Query(ctx, `
SELECT COALESCE(NULLIF(BTRIM(partition_label), ''), 'whole')
FROM shed_partitions
WHERE tenant_id = $1::uuid
  AND shed_id = $2::uuid
  AND status = 'active'
  AND COALESCE(NULLIF(BTRIM(partition_label), ''), 'whole') <> 'whole'`,
		tenantID, shedID)
	if err != nil {
		return fmt.Errorf("pccare: resolve shed partitions: %w", err)
	}
	defer rows.Close()

	hasPartitions := false
	matches := false
	for rows.Next() {
		hasPartitions = true
		var catalogLabel string
		if err := rows.Scan(&catalogLabel); err != nil {
			return fmt.Errorf("pccare: scan shed partition: %w", err)
		}
		if domain.PartitionMatchKey(catalogLabel) == normalizedRequested {
			matches = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("pccare: iterate shed partitions: %w", err)
	}
	if hasPartitions {
		if normalizedRequested == "whole" || !matches {
			return ports.ErrInvalidPartition
		}
		return nil
	}
	if normalizedRequested != "whole" {
		return ports.ErrInvalidPartition
	}
	return nil
}
