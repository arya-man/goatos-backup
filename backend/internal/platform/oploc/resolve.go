package oploc

import (
	"context"
	"fmt"
)

// ShedScopedLocationSQL is THE query for resolving a shed id to its operational location.
//
// It exists because the recurring defect on this codebase is not composing the display -- that
// has had a canonical helper for a while -- it is FETCHING the parts. Every site that needed a
// partition wrote its own SELECT, and the schema offers two columns that look interchangeable
// and are not:
//
//	partition_label   'Part 3'  the HUMAN label -- the only one that may be displayed
//	normalized_label  '3'       the scrubbed MATCHING KEY -- joins only, never a screen
//
// Selecting the wrong one compiles, passes review, and renders 'Mandela 2 - 3' to an operator.
// That exact defect shipped, was fixed, and was then reintroduced by a later change to a
// different module. Centralising the fetch makes the mistake unavailable rather than merely
// discouraged.
//
// AGREE-OR-GO-BARE is baked in: a shed with exactly ONE active real partition resolves to that
// partition; a shed with several is ambiguous at shed grain, so it returns BARE (empty
// partition) rather than picking one. `ORDER BY ... LIMIT 1` over rows that can legitimately
// differ fabricates an answer that silently flips as partitions change.
//
// 'whole' is the unpartitioned sentinel. It is filtered here so it can never reach a caller,
// and therefore never a screen.
//
// Parameters: $1 tenant_id, $2 shed location_id.
const ShedScopedLocationSQL = `
SELECT COALESCE(NULLIF(shed.name, ''), shed.location_code, ''),
       COALESCE((
         SELECT min(sp.partition_label)
         FROM shed_partitions sp
         WHERE sp.tenant_id = shed.tenant_id
           AND sp.shed_id = shed.location_id
           AND sp.status = 'active'
           AND COALESCE(NULLIF(sp.partition_label, ''), 'whole') <> 'whole'
         HAVING count(*) = 1
       ), '')
FROM locations shed
WHERE shed.tenant_id = $1::uuid AND shed.location_id = $2::uuid`

// PartitionAliasExclusionSQL returns a NOT EXISTS predicate that suppresses LEGACY
// PARTITION-ALIAS location rows for the shed table aliased as shedAlias.
//
// The farm's pens exist twice in `locations`: as the canonical parent shed plus a
// `shed_partitions` catalog row ("Castro" + partition "1", rendered "Castro - 1"), and as an
// OLD, still-`active`, still-`location_type='shed'` row literally named "Castro 1" or
// "Godel 1 - Part 3". Nothing in the row marks it as an alias -- there is no column for it --
// so any membership query written as `location_type='shed' AND status='active'` silently
// counts each pen a second time as if it were its own building. On the live data that is 105
// phantom sheds against 21 real ones; none of them holds a single goat.
//
// The predicate is park-LOCAL on purpose: two parks genuinely own a shed named "Castro", and a
// tenant-wide sibling check would delete one of them.
//
// Name matching handles BOTH alias shapes the farm actually has, which is why this lives here
// instead of being copied again. The variant in counts/shifting_destinations.go strips
// non-alphanumerics and compares the remainder to normalized_label; that suppresses "Castro 1"
// but NOT "Godel 1 - Part 3", whose remainder is "part3" against a normalized_label of "3".
// This form strips a leading "- Part" and whitespace, so both shapes resolve to the catalog's
// matching key.
//
// normalized_label is correct HERE and only here: this is a join key, never display copy. The
// human `partition_label` is what reaches a screen (AGENTS.md operational-location rule 5a).
func PartitionAliasExclusionSQL(shedAlias string) string {
	return `NOT EXISTS (
  SELECT 1
  FROM locations parent_shed
  JOIN shed_partitions parent_partition
    ON parent_partition.tenant_id = parent_shed.tenant_id
   AND parent_partition.shed_id = parent_shed.location_id
   AND parent_partition.status = 'active'
  WHERE parent_shed.tenant_id = ` + shedAlias + `.tenant_id
    AND parent_shed.parent_location_id = ` + shedAlias + `.parent_location_id
    AND parent_shed.location_id <> ` + shedAlias + `.location_id
    AND parent_shed.location_type = 'shed'
    AND parent_shed.status = 'active'
    AND parent_shed.retired_at IS NULL
    AND starts_with(BTRIM(` + shedAlias + `.name), BTRIM(parent_shed.name))
    AND NULLIF(
      regexp_replace(
        BTRIM(replace(BTRIM(` + shedAlias + `.name), BTRIM(parent_shed.name), '')),
        '^\s*-\s*part\s*|\s+',
        '',
        'gi'
      ),
      ''
    ) = parent_partition.normalized_label
)`
}

// RowScanner is satisfied by pgx.Row, so callers pass their own pool/tx without this package
// taking a database dependency.
type RowScanner interface {
	Scan(dest ...any) error
}

// ResolveShedLocation scans the result of ShedScopedLocationSQL into an OperationalLocation.
//
// A shed that cannot be resolved returns the zero value and a nil error: callers DEGRADE to
// their location-less label rather than rendering a raw uuid or a dangling separator. That is
// deliberate -- a sibling queue once shipped `Raised by <uuid>` to operators.
func ResolveShedLocation(_ context.Context, row RowScanner) (OperationalLocation, error) {
	var shedName, partitionLabel string
	if err := row.Scan(&shedName, &partitionLabel); err != nil {
		return OperationalLocation{}, fmt.Errorf("oploc: scan shed location: %w", err)
	}
	return OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}, nil
}

// ShedScopedLocationBatchSQL resolves MANY shed ids in one round trip.
//
// It exists so a list does not have to choose between two bad options: calling
// ShedScopedLocationSQL once per row (an N+1 fan-out on a read path -- one round
// trip per animal, which is exactly the banned shape) or hand-writing the
// partition SELECT inline, which is how `Mandela 2 - 3` reached an operator twice.
//
// It is the SAME query as its scalar sibling with the shed id list widened, and
// every property that makes that one safe is preserved and must stay preserved:
// `partition_label` never `normalized_label`; the 'whole' sentinel filtered so it
// cannot reach a caller; and agree-or-go-bare via HAVING count(*) = 1 rather than
// ORDER BY ... LIMIT 1, which fabricates an answer that flips as partitions change.
//
// A shed id with no row is simply ABSENT from the result. Callers degrade to their
// location-less label for it, the same way ResolveShedLocation returns the zero
// value rather than an error.
//
// Parameters: $1 tenant_id, $2 shed location_ids (uuid[]).
const ShedScopedLocationBatchSQL = `
SELECT shed.location_id::text,
       COALESCE(NULLIF(shed.name, ''), shed.location_code, ''),
       COALESCE((
         SELECT min(sp.partition_label)
         FROM shed_partitions sp
         WHERE sp.tenant_id = shed.tenant_id
           AND sp.shed_id = shed.location_id
           AND sp.status = 'active'
           AND COALESCE(NULLIF(sp.partition_label, ''), 'whole') <> 'whole'
         HAVING count(*) = 1
       ), '')
FROM locations shed
WHERE shed.tenant_id = $1::uuid AND shed.location_id = ANY($2::uuid[])`

// RowsScanner is the subset of pgx.Rows ResolveShedLocations needs.
type RowsScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

// ResolveShedLocations scans ShedScopedLocationBatchSQL into a shed-id-keyed map.
//
// Keyed by SHED ID, never by shed name: names repeat across parks (two Castro, two
// Gandhi, two Yashoda), so a name-keyed map silently merges parks.
func ResolveShedLocations(_ context.Context, rows RowsScanner) (map[string]OperationalLocation, error) {
	out := map[string]OperationalLocation{}
	for rows.Next() {
		var shedID, shedName, partitionLabel string
		if err := rows.Scan(&shedID, &shedName, &partitionLabel); err != nil {
			return nil, fmt.Errorf("oploc: scan shed locations: %w", err)
		}
		out[shedID] = OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("oploc: read shed locations: %w", err)
	}
	return out, nil
}
