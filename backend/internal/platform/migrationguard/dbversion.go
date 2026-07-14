package migrationguard

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// undefinedTableSQLState is Postgres error code 42P01 ("undefined_table"),
// returned when goatos_schema_migrations does not exist yet - i.e. no
// migration has ever been applied to this database (see cmd/migrate's
// `CREATE TABLE IF NOT EXISTS goatos_schema_migrations`).
const undefinedTableSQLState = "42P01"

// AppliedVersion returns the highest migration version recorded in
// goatos_schema_migrations, normalized to the same bare leading-digits shape
// BinaryVersion returns (e.g. "000188"). It returns "" with a nil error when
// the table does not exist yet - a never-migrated database, not a query
// failure - so Check can treat it as the (now enforced) BinaryAhead
// direction rather than an operational error.
//
// goatos_schema_migrations.version is NOT already in that bare shape:
// cmd/migrate stores the full filename stem there (see
// backend/cmd/migrate/main.go's `Version: strings.TrimSuffix(entry.Name(),
// ".sql")`), e.g. "000188_drop_process_integrity_projection_summaries", not
// "000188". `ORDER BY version DESC` still finds the right row - every stem
// shares the same fixed-width, zero-padded numeric prefix, so lexicographic
// order on the full string agrees with numeric order on the prefix - but the
// returned value must be normalized before Check compares it against
// BinaryVersion's bare numeric string.
func AppliedVersion(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	if pool == nil {
		return "", errors.New("migrationguard: pool is required")
	}
	var stored string
	err := pool.QueryRow(ctx, `SELECT version FROM goatos_schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&stored)
	switch {
	case err == nil:
		m := migrationFilenameVersion.FindStringSubmatch(stored)
		if m == nil {
			return "", fmt.Errorf("migrationguard: goatos_schema_migrations.version %q does not start with a numeric migration version", stored)
		}
		return m[1], nil
	case errors.Is(err, pgx.ErrNoRows):
		// Table exists but has zero rows (should not happen in practice -
		// cmd/migrate inserts a row per applied migration - but treat it the
		// same as "never migrated" rather than erroring).
		return "", nil
	default:
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == undefinedTableSQLState {
			return "", nil
		}
		return "", fmt.Errorf("migrationguard: query applied migration version: %w", err)
	}
}
