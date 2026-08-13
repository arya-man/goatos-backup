package postgres

import (
	"strings"
)

// splitShedFilter accepts legacy "<shed_uuid>#<partition>" filters but returns
// only the exact shed id as the live identity. New filter options are plain shed
// ids; stale partition suffixes are ignored so they cannot hide exact-shed rows.
func splitShedFilter(raw string) (shedID string, partitionKey string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}
	id, _, found := strings.Cut(trimmed, "#")
	if !found {
		return trimmed, ""
	}
	return strings.TrimSpace(id), ""
}

// shedPartitionPredicate remains only for old query placeholders. splitShedFilter
// always supplies a blank partition key, so this predicate is a live no-op.
const shedPartitionPredicate = `regexp_replace(lower(COALESCE(NULLIF(btrim(vi.partition_label), ''), 'whole')), '^part[[:space:]]+', '')`
