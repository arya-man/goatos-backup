# STG Partition Catalog Data Repair (2026-08-07)

## Symptom

A maintainer photographed a shifting-destination dropdown listing `Godel 1 1`,
`Godel 1 10`, and an Add-birth dropdown listing `Godel 1` **six times**. This
is partly a rendering bug (naive-join / name-group composition, see
`AGENTS.md` "Operational Location and Partition Convention", OL-2/OL-3) and
partly a **data** problem in the stg replica catalog itself, quantified here.

Replica audited (read-only): `postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable`

Tenant: `00000000-0000-4000-8000-000000000001` (single tenant in this replica).

## What Is Wrong (Real Counts)

### Class A — CORRUPT catalog rows (2 rows)

`shed_partitions` entries whose `shed_id` points at a **location-alias row**
(itself registered as `locations.location_type = 'shed'`) instead of the true
parent shed:

| shed_id | shed_id resolves to | partition_label | normalized_label | source |
|---|---|---|---|---|
| `da1f37fc-a939-4357-a980-6e96914dfee4` | "Godel 1 - Part 1" | `0` | `0` | `location_alias` |
| `3505169a-e3a9-49ab-b53b-06a8e7bc7eff` | "Godel 2 - Part 1" | `0` | `0` | `location_alias` |

Verified 0 goats reference either `shed_id` (checked `goats.shed_id`,
`goats.current_location_id`, `goat_shed_partitions.shed_id`). Not dangerous
to remove from the catalog.

### Class B — redundant alias-as-shed `locations` rows, safe to retire (15 rows)

`locations` holds a real parent shed (e.g. `Godel 1`, 120 animals, active)
**and** separate active top-level rows named `Godel 1 - Part 1/2/3`, etc.
Each of these duplicates a pen that is **already correctly cataloged** in
`shed_partitions` against the true parent:

```
shed_partitions: shed_id=a80948b1... (Godel 1), partition_label='Part 1', source=goat_attested, status=active
locations:       location_id=da1f37fc... name='Godel 1 - Part 1', status=active   <- duplicate
```

Full list (all confirmed 0 goats referencing them directly):

| shed | active alias-as-shed children |
|---|---|
| Godel 1 | 3 (`Part 1`, `Part 2`, `Part 3`) |
| Godel 2 | 2 (`Part 1`, `Part 2`) |
| Mandela 2 | 10 (`Part 1`..`Part 10`) |

15 rows total. These are the rows populating the sixfold "Godel 1" entries
in the Add-birth dropdown and the "Godel 1 1" / "Godel 1 10" entries in the
shifting-destination dropdown — any picker that lists active `locations`
rows of `location_type = 'shed'` without excluding alias-derived duplicates
will surface these alongside the real, correctly-partitioned parent.

Also found: the 3 active `Godel 1 - Part N` rows are parented under park
**Coimbatore** while the true parent shed `Godel 1` is parented under park
**Channapatna** — a second, independent corruption (wrong park), folded into
the same retire action since these rows are being retired regardless.

### Class C — redundant alias-as-shed rows, NEEDS HUMAN DECISION (10 rows)

`Mandela 1 - Part 1` .. `Mandela 1 - Part 10` are all active, all hold 0
goats directly, but **there is no active parent `Mandela 1` row** — unlike
Godel 1/2 and Mandela 2, there is nothing in `shed_partitions` cataloging
these pens against a real parent shed. Retiring these 10 rows the same way
as Class B would delete the **only** surviving record that "Mandela 1" pens
exist, with no replacement.

**This script does not touch these 10 rows.** See "What A Human Must
Decide" below.

### Class D — `goat_shed_partitions` rows with no matching catalog entry

Checked: 324 non-`'whole'` rows in `goat_shed_partitions`. Cross-referenced
against `shed_partitions` (active, matching `normalized_label`). **Result:
0 orphans** — every per-goat attested partition label has a matching active
catalog row. (An initial pass of this check had a case-sensitivity bug in
the label-normalization regex and produced a false positive of 9 "orphan"
rows; re-verified correct — see script comments. Reported here so this
finding is not silently dropped a second time.)

### Class E — `Yashoda`-style numeric-suffix sheds: NOT partitions, and mechanically distinguishable

`Yashoda 1` .. `Yashoda 10` are ten genuinely separate sheds. Verified:

- None of their `locations.name` values contain the `" - Part "` token that
  every genuine alias-as-shed row uses.
- Each `Yashoda N` is its own top-level `locations` row parented **directly
  under a park** (`Coimbatore` / `Channapatna`), never under another shed.
- No `shed_partitions` row has a `shed_id` resolving to any `Yashoda N` row.
- No bare `Yashoda` parent row exists (there is nothing for `Yashoda N` to be
  a partition *of*).

So — contrary to the concern raised in the task — `Yashoda N` sheds **are**
mechanically distinguishable from partition-style rows in this replica: the
`" - Part N"` naming convention plus "parented under a shed vs. parented
under a park" structure discriminates them cleanly. If a future seed ever
produces a `Yashoda` (bare) parent row with `Yashoda 1..10` reparented under
it, that would collide with this rule and needs re-auditing before reuse.

## What The Script Does

`tools/data-repair/stg-partition-catalog-repair.sql`:

1. Deletes the 2 Class A corrupt `shed_partitions` rows (goat-reference
   guarded, idempotent).
2. Soft-retires (status -> `inactive`, `retired_at` set) the 15 Class B
   redundant `locations` rows (goat-reference guarded, idempotent). **No
   `locations` row is ever deleted** — `shed_partitions_shed_fk` and
   `goat_shed_partitions_shed_fk` are `ON DELETE RESTRICT`, and other tables
   (`goat_location_history`, `shifting_events`, `weighing_campaign_sheds`,
   etc.) may hold historical FK references that were not exhaustively
   enumerated; soft-retire preserves all of them.
3. Runs the Class C evidence query (no mutation) so a human sees the 10
   Mandela 1 rows without the script silently skipping them.
4. Runs a post-repair verification query (both corrupt-row and redundant-row
   counts should be 0).
5. Everything is inside `BEGIN; ... ROLLBACK;` — a human must change
   `ROLLBACK;` to `COMMIT;` deliberately after reading the SELECT output.

Every DELETE/UPDATE is preceded by a SELECT showing exactly what it will
touch, and every mutating WHERE clause is scoped tightly enough to be a
no-op on re-run (idempotent).

## How To Verify Before Running

```bash
psql "postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable" \
  -f tools/data-repair/stg-partition-catalog-repair.sql
```

Run the whole file — since it never auto-commits, this is safe to execute
fully; read the SELECT output for STEP 1 (2 expected rows, all goat counts
0), STEP 3 (15 expected rows, all goat counts 0), and STEP 5 (10 rows,
informational). Confirm counts match the baseline in this runbook. If any
goat-reference count is non-zero, **stop** — do not commit — treat it as a
new finding and re-scope the affected row out of the repair.

## How To Verify After Running (once a human commits)

```sql
-- both must return 0
SELECT count(*) FROM shed_partitions
WHERE source='location_alias' AND partition_label='0' AND normalized_label='0';

SELECT count(*) FROM locations
WHERE status='active'
  AND (name LIKE 'Godel 1 - Part %' OR name LIKE 'Godel 2 - Part %' OR name LIKE 'Mandela 2 - Part %');

-- sanity: real parent sheds still active and still hold their animals
SELECT name, status FROM locations WHERE name IN ('Godel 1','Godel 2','Mandela 2');
SELECT shed_id, count(*) FROM goats
WHERE shed_id IN (SELECT location_id FROM locations WHERE name IN ('Godel 1','Godel 2','Mandela 2'))
GROUP BY shed_id;
```

Then re-check the operator-facing dropdowns (shifting destination,
Add-birth) against the code fix — the duplicate/garbled entries sourced from
Class A/B rows should be gone. Mandela 1's ambiguity persists until the
Class C human decision is made and executed separately.

## What A Human Must Decide

**Class C — `Mandela 1 - Part 1..10` (10 rows), before any repair touches them:**

Real animals are not directly at stake (0 goats reference these rows), but
the *shed itself* is ambiguous — a decision here shapes where any future
placement/import lands. Options:

1. **Create a `Mandela 1` parent shed** (mirroring `Godel 1`/`Godel 2`/
   `Mandela 2`), register `shed_partitions` catalog rows for `Part 1..10`
   against it (`source='manual'` or `'location_alias'` as appropriate), then
   retire the 10 standalone alias rows the same way Class B was retired.
   This is the "make Mandela 1 consistent with its siblings" option and
   matches the AGENTS.md convention (`Godel 1 - Part 3` -> `ONE shed Godel 1
   with partition Part 3`).
2. **Leave the 10 rows as standalone active sheds** if there is a real,
   independently confirmed reason a `Mandela 1` parent should not exist
   (e.g. the numbering is coincidental and these are genuinely 10 separate
   physical sheds, like `Yashoda`). Nothing in the data audited here
   supports this — no goat, catalog, or alias evidence distinguishes
   `Mandela 1 - Part N` from the Godel/Mandela-2 pattern — but a human with
   physical-site knowledge should confirm before assuming option 1.

Do not silently pick one; the choice changes which shed a future goat's
placement is recorded against.

## What CANNOT Be Repaired By Data Alone

The root cause of the whole class of bugs (per `AGENTS.md` "Operational
Location and Partition Convention") is that **the catalog stores the same
pen two ways depending on `source`** — as a `shed_partitions` row scoped
under the correct parent (`goat_attested` / `manual`), and, independently,
as a free-standing top-level `locations` row of `location_type='shed'`
(`location_alias`-sourced). Data repair can clean up every *instance* found
today, but it cannot stop a future import from re-creating the same
class of row, because:

- Nothing in the schema prevents a `locations` row named `"<Shed> - Part N"`
  from being created and left un-linked to its true parent's
  `shed_partitions` catalog.
- `shed_partitions` has no FK/constraint tying a `partition_label` to a
  single canonical representation — a future `location_alias` import can
  still register `shed_id` pointing at another `locations` row instead of
  the true parent shed (recreating Class A).
- There is no unique constraint preventing two `locations` rows with the
  same conceptual pen (parent shed + partition) from both being `active`
  simultaneously under `location_type='shed'`.

Per AGENTS.md, this needs the **`partition_id` schema migration**: give
each real partition a first-class stable identity (e.g. `shed_partitions`
gets a surrogate `partition_id` PK, and every location-bearing table stores
`partition_id` instead of re-deriving `shed_id + partition_label` strings),
plus a constraint/guard preventing a second `locations` row of
`location_type='shed'` from being created for a pen that already has a
`shed_partitions` catalog entry under its true parent. That is a code +
migration change, out of scope for this data-only repair, and is why this
repair is necessarily a point-in-time cleanup, not a permanent fix.

## Related

- `AGENTS.md` → "Operational Location and Partition Convention (maintainer
  lock, 2026-08-06)" — the code-side convention and worked bug examples
  (`OL-2`, `OL-3`, `OL-7`) this data corruption feeds.
- `context/repo-audits/operational-location-do-not-reopen-ledger.md`
- `docs/decisions/operational-location-convention.md`
- `tools/data-repair/stg-partition-catalog-repair.sql` — the repair script
  described here.
