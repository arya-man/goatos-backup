package postgres

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/platform/parkcatalog"
)

// ListParkCodes returns the codes of the tenant's active parks (Configuration > Items & settings >
// Parks), so a park added there can receive a purchase load at once.
func (r *Repository) ListParkCodes(ctx context.Context, tenantID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	parks, err := parkcatalog.ListActive(ctx, r.pool, tenantID)
	if err != nil {
		return nil, err
	}
	return parkcatalog.Codes(parks), nil
}
