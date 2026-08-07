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
         SELECT sp.partition_label
         FROM shed_partitions sp
         WHERE sp.tenant_id = shed.tenant_id
           AND sp.shed_id = shed.location_id
           AND sp.status = 'active'
           AND COALESCE(NULLIF(sp.partition_label, ''), 'whole') <> 'whole'
         HAVING count(*) = 1
       ), '')
FROM locations shed
WHERE shed.tenant_id = $1::uuid AND shed.location_id = $2::uuid`

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
