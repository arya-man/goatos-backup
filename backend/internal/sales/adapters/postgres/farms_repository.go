package postgres

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/platform/parkcatalog"
)

// ListFarms returns the codes of the tenant's active parks: the farms a deal, a buyer lead or a
// sold-tag list may name. Parks are authored on Configuration > Items & settings, so a park added
// there is a sales farm immediately.
func (r *Repository) ListFarms(ctx context.Context, tenantID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	parks, err := parkcatalog.ListActive(ctx, r.pool, tenantID)
	if err != nil {
		return nil, err
	}
	return parkcatalog.Codes(parks), nil
}
