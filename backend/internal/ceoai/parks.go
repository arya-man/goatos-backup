package ceoai

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/platform/parkcatalog"
)

// NewParkDirectory answers the tenant's ACTIVE parks from the one park catalog (Configuration >
// Items & settings > Parks), so the assistant recognises a park added there by name or code. A nil
// pool yields nil, and the assistant then recognises no park name rather than a stale constant.
func NewParkDirectory(pool *pgxpool.Pool, timeout time.Duration) ports.ParkDirectory {
	if pool == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &parkDirectory{pool: pool, timeout: timeout}
}

type parkDirectory struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func (d *parkDirectory) ActiveParks(ctx context.Context, tenantID string) ([]domain.ParkRef, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	parks, err := parkcatalog.ListActive(ctx, d.pool, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ParkRef, 0, len(parks))
	for _, p := range parks {
		out = append(out, domain.ParkRef{Code: p.Code, Name: p.Name, ID: p.ID})
	}
	return out, nil
}
