package oploc

import "fmt"

// PartitionAliasExclusionSQL is THE predicate that keeps a legacy partition-ALIAS `locations` row
// out of an operational-location picker.
//
// The defect it exists for, observed on live STG 2026-08-11. A subdivided shed is stored ONCE as
// the physical building (`Castro`) with its pens in the `shed_partitions` catalog (`1`, `2`, `3`),
// and renders `Castro - 1`. But the pre-catalog data shape ALSO carried one `locations` row per pen
// named `Castro 1` / `Godel 1 - Part 3`, parented to the PARK rather than to the shed. Migration
// 000112 built the catalog by READING those rows, and documented the contract that keeps them out
// of every picker: they carry status='inactive' -- "not a building, but a place". It deliberately
// mutated nothing, so NOTHING in the schema enforces that status, and nothing can: a seed, an
// import, a repair script or an admin write can set it back to 'active' at any time. On STG
// something did, on 2026-08-10, to all 130 of them at once -- leaving 175 active shed rows of which
// only 45 are buildings. Every picker that trusts `status='active'` alone then offers the same
// physical pen twice, `Castro - 1` from the catalog and `Castro 1` from the resurrected alias, and
// the operator has no way to tell which is real.
//
// So a read path cannot treat "active" as evidence that a shed row is a building. It has to ask the
// catalog, which is the authority on which pens exist. That is why this is a query-layer rule and
// not a data cleanup: a cleanup fixes today's rows, this survives the next reseed. It is also why
// the bug is invisible locally -- the clone DB has those rows inactive.
//
// The match is by NAME because the alias rows carry no foreign key to the shed they duplicate --
// that missing link is exactly why 000112 had to parse names to seed the catalog in the first
// place. The two accepted spellings (`Castro 1`, `Godel 1 - Part 3`) are the same pair migration
// 000142 matched on when it re-pointed stranded weighing buckets, so this reuses a reviewed shape
// rather than inventing a third. It matches `partition_label`, the HUMAN label, and never
// `normalized_label`, because the alias rows were named from the label the farm writes.
//
// THREE narrowings, each of which prevents hiding a REAL shed:
//
//   - The candidate must have NO active pens of its own. A row the catalog knows as a building is a
//     building, whatever it happens to be named.
//   - The parent must be a DIFFERENT active, non-retired shed in the SAME park. Two parks each hold
//     a `Castro` and a `Gandhi`; matching across parks would hide one park's real shed because the
//     other park catalogued a pen by that name.
//   - The parent must have that pen CATALOGUED. Without this, a genuinely undivided shed whose name
//     merely ends in a digit -- `Ho Chi Minh 1`, `Yashoda 2`, the case AGENTS.md Rule 1 calls out --
//     would vanish the moment a same-park shed happened to be named `Ho Chi Minh`. The catalog has
//     to actually claim the pen.
//
// What it deliberately does NOT do: it never consults goat placement. A pen holding zero animals is
// real and is usually the one about to be filled (CBE `Yashoda 5`), and deriving destinations from
// where animals currently stand is the "cannot fill an empty pen" bug 000112 was written to retire.
// It also does not care whether the ALIAS row holds animals: an animal standing in a duplicate row
// is a data problem to repair, not a reason to keep offering that duplicate as somewhere to move
// animals TO.
//
// shedAlias is the SQL alias of the candidate `locations` row in the caller's query. It is a
// compile-time literal at every call site, never request input.
//
// Cost: a correlated EXISTS over `locations` (~175 rows/tenant) joined to `shed_partitions` (~130),
// both authored infrastructure that cannot grow with herd size, and both served by their own
// (tenant_id, ...) keys. It is evaluated only inside catalog reads already annotated as bounded.
func PartitionAliasExclusionSQL(shedAlias string) string {
	return fmt.Sprintf(`NOT (
    NOT EXISTS (
      SELECT 1 FROM shed_partitions own
      WHERE own.tenant_id = %[1]s.tenant_id
        AND own.shed_id = %[1]s.location_id
        AND own.status = 'active'
    )
    AND EXISTS (
      SELECT 1
      FROM locations parent
      JOIN shed_partitions sp
        ON sp.tenant_id = parent.tenant_id
       AND sp.shed_id = parent.location_id
       AND sp.status = 'active'
      WHERE parent.tenant_id = %[1]s.tenant_id
        AND parent.parent_location_id = %[1]s.parent_location_id
        AND parent.location_id <> %[1]s.location_id
        AND parent.location_type = 'shed'
        AND parent.status = 'active'
        AND parent.retired_at IS NULL
        AND lower(btrim(%[1]s.name)) IN (
              lower(concat_ws(' - ', btrim(parent.name), NULLIF(btrim(sp.partition_label), ''))),
              lower(concat_ws(' ',   btrim(parent.name), NULLIF(btrim(sp.partition_label), '')))
            )
    )
  )`, shedAlias)
}
