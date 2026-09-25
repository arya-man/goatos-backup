// Package postgres implements sales-ledger persistence.
package postgres

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

const defaultQueryTimeout = 3 * time.Second

// Repository is the sales module's Postgres adapter. Raw pgx with a per-call timeout, the same
// shape as procurement's vendor register.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ ports.SalesRepository = (*Repository)(nil)
