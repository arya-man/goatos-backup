package oploc

import (
	"context"
	"fmt"
)

// ShedScopedLocationSQL resolves a location id to the exact shed display.
//
// Live operational identity is the shed row itself: "Castro 2", "Mandela 2 Part 1", etc.
// Legacy partition rows may still exist for history and migration compatibility, but this
// resolver must not export them as display identity because callers will otherwise rejoin
// "shed name + partition" and create labels such as "Castro 2 2".
//
// Parameters: $1 tenant_id, $2 shed location_id.
const ShedScopedLocationSQL = `
SELECT COALESCE(NULLIF(shed.name, ''), shed.location_code, ''),
       ''
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
