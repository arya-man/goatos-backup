package httpmiddleware

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// AuthDatabaseBusyRetryAfter is the Retry-After (seconds) sent with a 503 when an
// auth-path database read could not be served right now.
const AuthDatabaseBusyRetryAfter = "2"

// DatabaseUnavailable reports whether err means the database could not serve an
// auth step right now -- a transient 503 with Retry-After, never a 403 or 500:
// context timeout/cancel (or the request context already done), a connect
// failure, a retry-safe or timed-out pgconn error, or a server-side
// insufficient-resources (53xxx, incl. 53300 too_many_connections), connection
// exception (08xxx) or shutdown / cannot-connect-now (57P01-57P03).
// It is the ONE classifier for every auth path (session-events and the
// protected-route middleware).
func DatabaseUnavailable(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || (ctx != nil && ctx.Err() != nil) {
		return true
	}
	var connectErr *pgconn.ConnectError
	if errors.As(err, &connectErr) || pgconn.SafeToRetry(err) || pgconn.Timeout(err) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case strings.HasPrefix(pgErr.Code, "53"),
			strings.HasPrefix(pgErr.Code, "08"),
			pgErr.Code == "57P01", pgErr.Code == "57P02", pgErr.Code == "57P03":
			return true
		}
	}
	return false
}
