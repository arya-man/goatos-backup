package postgres

import (
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// splitShedFilter decodes a shed filter value into a shed uuid and a normalized
// partition key.
//
// The filter OPTIONS this repository hands out are keyed by oploc.Key() --
// "<shed_uuid>#<normalized partition>" -- because a partitioned shed yields one
// option per partition and a bare shed uuid cannot tell them apart. The queue
// query, however, filters on `vi.shed_id = $n::uuid`. Passing the composite key
// straight into that cast is not a soft failure: Postgres rejects
// "uuid#part 3" with `invalid input syntax for type uuid`, the request errors,
// and the screen silently falls back to whatever is in the local cache -- so the
// operator sees stale rows and no error. Reported on the leadership Videos
// filter, 2026-08-07.
//
// Splitting here rather than changing the option IDs to bare uuids is deliberate:
// bare uuids would make the partition options indistinguishable, which is the
// defect the partition convention exists to prevent.
//
// A value with no "#" is returned as-is with an empty partition key, so a client
// that still sends a plain shed uuid keeps working unchanged and matches every
// partition of that shed.
func splitShedFilter(raw string) (shedID string, partitionKey string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}
	id, partition, found := strings.Cut(trimmed, "#")
	if !found {
		return trimmed, ""
	}
	return strings.TrimSpace(id), oploc.NormalizePartition(partition)
}

// shedPartitionPredicate is the SQL that pairs with splitShedFilter's partition
// key. It MUST stay identical to oploc.NormalizePartition: lower, trim, drop a
// leading "part " word, and treat an absent label as the "whole" sentinel. Any
// drift here means a partition the filter offers is a partition the query cannot
// find.
//
// The parameter is applied as a no-op when empty so a bare-uuid filter (or no
// filter at all) matches every partition.
const shedPartitionPredicate = `regexp_replace(lower(btrim(COALESCE(vi.partition_label, 'whole'))), '^part[[:space:]]+', '')`
