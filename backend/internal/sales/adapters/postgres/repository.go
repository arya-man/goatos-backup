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
	// tagged answers "how many animals are tagged to this deal" before it may be marked failed.
	// Wired in production (bootstrap/api.go, asserted by a test); nil skips the check.
	tagged ports.SaleTaggingReader
}

// WithTaggedAnimals wires the herd-side reader the failed-deal refusal asks.
func (r *Repository) WithTaggedAnimals(reader ports.SaleTaggingReader) *Repository {
	r.tagged = reader
	return r
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ ports.SalesRepository = (*Repository)(nil)
