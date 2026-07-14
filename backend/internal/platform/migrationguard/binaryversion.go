package migrationguard

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/vgoats/goatos/backend/migrations"
)

// migrationFilenameVersion extracts the leading zero-padded numeric version
// from a migration filename, e.g. "000188" from
// "000188_drop_process_integrity_projection_summaries.sql".
var migrationFilenameVersion = regexp.MustCompile(`^([0-9]+)_`)

// BinaryVersion returns the highest migration version this compiled binary
// knows about, derived from the migrations/postgres/*.sql files embedded at
// build time (see backend/migrations/embed.go). It does not touch the
// filesystem, so it returns the same answer whether the process runs via
// `go run` from a full checkout or as the trimmed production Docker image
// (backend/Dockerfile), which does not copy backend/migrations into the
// image.
//
// The comparison is the same lexicographic-on-zero-padded-filename ordering
// cmd/migrate itself uses to sort migrations (backend/cmd/migrate/main.go's
// loadMigrations), so this stays consistent with how migrations are actually
// applied.
func BinaryVersion() (string, error) {
	entries, err := migrations.Postgres.ReadDir("postgres")
	if err != nil {
		return "", fmt.Errorf("migrationguard: read embedded migrations: %w", err)
	}
	var max string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		m := migrationFilenameVersion.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		if version := m[1]; version > max {
			max = version
		}
	}
	if max == "" {
		return "", fmt.Errorf("migrationguard: no migration files found in embedded migrations/postgres")
	}
	return max, nil
}
