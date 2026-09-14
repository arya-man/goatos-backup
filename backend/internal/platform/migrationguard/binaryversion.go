package migrationguard

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/vgoats/goatos/backend/migrations"
)

// migrationFilenameVersion extracts the leading zero-padded numeric version
// from a migration filename or version marker, e.g. "000188" from
// "000188_drop_process_integrity_projection_summaries.sql".
var migrationFilenameVersion = regexp.MustCompile(`^([0-9]+)_`)

// BinaryVersion returns the highest migration family this compiled binary
// knows about, derived from the migrations/postgres/*.sql files embedded at
// build time (see backend/migrations/embed.go). It does not touch the
// filesystem, so it returns the same answer whether the process runs via
// `go run` from a full checkout or as the trimmed production Docker image
// (backend/Dockerfile), which does not copy backend/migrations into the
// image.
//
// The highest version is determined by numeric comparison of the leading
// version prefix (not lexicographic), so it remains correct if migration
// numbering ever crosses digit-width boundaries (e.g. 999999 → 1000000).
// The returned marker includes how many migration files share that highest
// prefix, e.g. "000306#2", so /readyz cannot pass a database that applied only
// one sibling from a duplicated numeric migration family.
func BinaryVersion() (string, error) {
	entries, err := migrations.Postgres.ReadDir("postgres")
	if err != nil {
		return "", fmt.Errorf("migrationguard: read embedded migrations: %w", err)
	}
	var max string
	var maxNum int64 = -1
	maxCount := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		m := migrationFilenameVersion.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		version := m[1]
		// Parse as int64 for numeric comparison, but keep the original
		// zero-padded string version for return value.
		num, err := parseVersion(version)
		if err != nil {
			continue
		}
		if num > maxNum {
			maxNum = num
			max = version
			maxCount = 1
			continue
		}
		if num == maxNum {
			maxCount++
		}
	}
	if max == "" {
		return "", fmt.Errorf("migrationguard: no migration files found in embedded migrations/postgres")
	}
	return migrationFamilyVersion(max, maxCount), nil
}
