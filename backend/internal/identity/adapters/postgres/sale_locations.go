package postgres

import (
	"context"
	"fmt"
	"sort"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// ListSaleLocations is the sale picker's park/shed/pen vocabulary.
//
// WHY THIS EXISTS INSTEAD OF THE GENERIC LOCATIONS LIST. The farm's pens exist TWICE in
// `locations`: as the canonical parent shed plus its shed_partitions catalog rows
// ("Castro" + pens 1/2/3), and as OLD, still-active, still-location_type='shed' rows
// literally named "Castro 1", "Castro 2", "Castro 3". Nothing on the row marks it as an
// alias -- there is no column for it.
//
// A picker built on `location_type='shed' AND status='active'` therefore offers both, and
// the alias rows hold NOTHING: on this tenant "Castro" carries 201 live animals across 3
// catalogued pens while every "Castro N" alias carries 0 animals and 0 pens. Choosing one
// returned an empty list and read as a broken screen. That is the reported bug, and it is
// the OL-2 duplicate-shed defect class.
//
// oploc.PartitionAliasExclusionSQL is the ONE implementation of that suppression, shared
// with the other reads that need it. It is park-LOCAL on purpose: two parks genuinely own
// a shed named "Castro", and a tenant-wide sibling check would delete one of them.
//
// projection-review: membership=one row per active non-alias shed in an authorized park; group_key=(shed.location_id) via the pens LATERAL's own aggregate; join_cardinality=park is 1 per shed on locations' primary key and the pens LATERAL is exactly 1 aggregate row per shed, so neither widens the set; pagination=NONE, bounded by the authored shed/pen estate (tens of rows per tenant, not animal-scaled); scope=tenant_id, plus location_type/status
func (r *Repository) ListSaleLocations(ctx context.Context, tenantID string) (*ports.SaleLocationCatalog, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	rows, err := r.pool.Query(ctx, `
SELECT shed.location_id::text,
       COALESCE(shed.parent_location_id::text, ''),
       COALESCE(NULLIF(pk.location_code, ''), pk.name, ''),
       COALESCE(NULLIF(shed.name, ''), shed.location_code, ''),
       COALESCE(pens.labels, '{}')
FROM locations shed
JOIN locations pk
  ON pk.location_id = shed.parent_location_id AND pk.tenant_id = shed.tenant_id
 AND pk.location_type = 'park' AND pk.status = 'active'
LEFT JOIN LATERAL (
  -- The pen CATALOG, so an empty pen stays selectable. 'whole' is the unpartitioned
  -- sentinel and is a matching key, never user copy, so it can never reach the picker.
  SELECT array_agg(sp.partition_label ORDER BY sp.display_order NULLS LAST, sp.partition_label) AS labels
  FROM shed_partitions sp
  WHERE sp.tenant_id = shed.tenant_id AND sp.shed_id = shed.location_id
    AND sp.status = 'active'
    AND COALESCE(NULLIF(sp.partition_label, ''), 'whole') <> 'whole'
) pens ON TRUE
WHERE shed.tenant_id = $1::uuid
  AND shed.location_type = 'shed'
  AND shed.status = 'active'
  AND shed.retired_at IS NULL
  AND `+oploc.PartitionAliasExclusionSQL("shed")+`
  -- SECOND, INDEPENDENT CONDITION, and it is not redundant. The alias suppression above
  -- can only recognise an alias by matching it against its PARENT'S PEN CATALOG, so an
  -- alias whose parent has no catalogued pens yet slips straight through it -- which is
  -- exactly what the baseline CPT shed does. A shed with no live animals AND no catalogued
  -- pens can never yield a single candidate, so offering it is the empty-picker bug no
  -- matter why it is empty.
  --
  -- This is a SOURCE picker (which animals are being sold), not a destination picker, so
  -- keying partly on live animals is correct here. The "empty destinations must stay
  -- selectable" rule governs where animals may be PUT; nothing can be sold out of a shed
  -- that holds nobody. A real shed that is merely empty today still appears the moment it
  -- has catalogued pens.
  AND (EXISTS (SELECT 1 FROM shed_partitions sp2
                WHERE sp2.tenant_id = shed.tenant_id AND sp2.shed_id = shed.location_id
                  AND sp2.status = 'active')
       OR EXISTS (SELECT 1 FROM goats g2
                   WHERE g2.tenant_id = shed.tenant_id AND g2.shed_id = shed.location_id
                     AND g2.lifecycle_status NOT IN
                         ('dead','sold','culled','transferred','lost','merged','inactive')))
ORDER BY 3, 4`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("identity: list sale locations: %w", err)
	}
	defer rows.Close()

	out := &ports.SaleLocationCatalog{Parks: []ports.SaleLocationPark{}, Locations: []ports.SaleLocationEntry{}}
	seenPark := map[string]bool{}
	for rows.Next() {
		var shedID, parkID, parkLabel, shedName string
		var pens []string
		if err := rows.Scan(&shedID, &parkID, &parkLabel, &shedName, &pens); err != nil {
			return nil, fmt.Errorf("identity: scan sale location: %w", err)
		}
		if !seenPark[parkID] {
			seenPark[parkID] = true
			out.Parks = append(out.Parks, ports.SaleLocationPark{ParkID: parkID, Label: parkLabel})
		}
		// PEN-WISE. A subdivided shed contributes one entry PER PEN and none for the
		// parent, because the parent is not a place anyone stands: every animal in
		// "Castro" is in Castro 1, 2 or 3, so offering the bare shed would offer a
		// selection that means "I do not know which pen".
		if len(pens) == 0 {
			out.Locations = append(out.Locations, ports.SaleLocationEntry{
				ShedID: shedID, ParkID: parkID, OperationalLocationDisplay: shedName,
			})
			continue
		}
		for _, pen := range pens {
			out.Locations = append(out.Locations, ports.SaleLocationEntry{
				ShedID:         shedID,
				ParkID:         parkID,
				PartitionLabel: pen,
				// Composed by the canonical helper, never hand-joined: it is the one
				// place that knows a numeric pen joins with a space and a worded one
				// with a dash.
				OperationalLocationDisplay: (oploc.OperationalLocation{
					ShedID: shedID, ShedName: shedName, PartitionLabel: pen,
				}).Display(),
			})
		}
	}
	sort.SliceStable(out.Locations, func(i, j int) bool {
		if out.Locations[i].ParkID != out.Locations[j].ParkID {
			return out.Locations[i].ParkID < out.Locations[j].ParkID
		}
		return out.Locations[i].OperationalLocationDisplay < out.Locations[j].OperationalLocationDisplay
	})
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: list sale locations: %w", err)
	}
	return out, nil
}
