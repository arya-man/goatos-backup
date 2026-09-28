package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// rowQuerier is what both a pool and a transaction offer.
type rowQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// queryBound runs a query assembled from a shared SELECT builder (operatorSelectSQL, grantsSQL,
// capabilitiesSQL, devicesSQL + a WHERE tail) only after sqlbind has checked that its placeholders
// are exactly $1..$N and N matches the arguments -- the final SQL and its binds as one contract.
func queryBound(ctx context.Context, q rowQuerier, sql string, args ...any) (pgx.Rows, error) {
	bound, err := sqlbind.Bind(sql, args...)
	if err != nil {
		return nil, err
	}
	return q.Query(ctx, bound.SQL(), bound.Args()...)
}
