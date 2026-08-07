# STG Partition Catalog Audit — Finding 7 (2026-08-07)

## STATUS: NO REPAIR NEEDED — STG DATA IS CORRECT

**OPERATIVE FINDING:** STG data is already in the correct shape. When all repair
verification queries were run against live STG read-only on 2026-08-07, every
repair class returned ZERO rows, proving no defects exist. The data shape is
already as intended.

**CRITICAL ACTION:** Do NOT run `stg-partition-catalog-repair.sql` or
`stg-mandela1-class-c-repair.sql`. The scripts are historical investigation
records only and were written against a mistaken model of the data. Running
them against STG would corrupt correct data.

**HOW TO VERIFY STG IS CORRECT:** Run the verification queries below (read-only)
against the STG replica. All queries should return zero rows (or two rows of
intact Mandela 1/Mandela 2 parent sheds in the last check).

---

## Investigation Context (Historical Record)

A maintainer photographed a shifting-destination dropdown listing `Godel 1 1`,
`Godel 1 10`, and an Add-birth dropdown listing `Godel 1` **six times**. This
was partly a rendering bug (naive-join / name-group composition, see
`AGENTS.md` "Operational Location and Partition Convention", OL-2/OL-3) and
partly suspected to be a **data** problem in the stg replica catalog. This
section documents the investigation that proved the data is actually correct.

Replica audited (read-only): `postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable`

Tenant: `00000000-0000-4000-8000-000000000001` (single tenant in this replica).

## Investigation Found: All Repair Targets Already Correct

### Class A — CORRUPT catalog rows (FOUND: 0 rows)

Investigation query in `stg-partition-catalog-repair.sql` STEP 1 found **zero** rows
matching the corrupt pattern (shed_partitions with source='location_alias' AND
partition_label='0' AND normalized_label='0').

**Conclusion:** No Class A corruption exists in STG. No repair needed.

### Class B — redundant alias-as-shed `locations` rows (FOUND: 0 rows)

Investigation query in `stg-partition-catalog-repair.sql` STEP 3 found **zero** rows
matching the pattern (active locations with names like `Godel 1 - Part %`,
`Godel 2 - Part %`, or `Mandela 2 - Part %`).

**Conclusion:** No redundant alias-as-shed rows exist in STG. No repair needed.

---

**Historical note:** The initial concern was that these rows might be duplicating
partition metadata that should belong only in `shed_partitions` catalog. However,
further investigation revealed that the correct data model for STG is already in
place: Godel 1 and Godel 2 are real parent sheds (one per park, with correct
partition labels), and no separate standalone `Godel X - Part N` location rows
exist as duplicates.

### Class C — `Mandela 1` partition rows (FOUND: 0 orphan rows, 2 correct parent sheds)

Investigation query in `stg-mandela1-class-c-repair.sql` STEP 2 found **zero** orphan
`Mandela 1 - Part N` rows that lacked a parent shed. Instead, it found:

- **2 active parent sheds named `Mandela 1` and `Mandela 2`** (one per park —
  **Channapatna** and **Coimbatore**, correctly sited)
- **20 active shed_partitions rows** (`Mandela 1` and `Mandela 2` each with `Part 1`..`Part 10`)
- **0 orphan alias rows**

**Conclusion:** The data is already correct. `Mandela 1` is a real parent shed
(not an invented need for repair) with its 10 partitions properly cataloged.
No repair or script execution is needed.

---

**Historical note:** The investigation script `stg-mandela1-class-c-repair.sql`
was written to handle a hypothetical Class C case (orphan `Mandela 1 - Part N`
rows without a parent). That case does not exist in STG. The data model is
already sound: Mandela 1 and Mandela 2 are both real parent sheds with their
partition labels stored in `shed_partitions`, not as separate top-level location
rows.

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

## Scripts (Historical Record — DO NOT RUN)

Two repair scripts were written during the investigation:

- `tools/data-repair/stg-partition-catalog-repair.sql` — would delete Class A
  rows and retire Class B rows (if they existed)
- `tools/data-repair/stg-mandela1-class-c-repair.sql` — would create a parent
  Mandela 1 shed (if needed)

**These scripts MUST NOT be run.** Both now carry superseded headers warning
against execution. The data is already correct, and running them would corrupt
STG.

The scripts are preserved as investigation records and source-of-truth for
understanding the partition data model that was validated.

## How To Verify STG is Correct (Read-Only Confirmation)

Run these read-only queries against the STG replica to confirm all data is in
the correct shape. Every result should match the expected values below.

```sql
-- VERIFICATION 1: Confirm NO Class A corrupt catalog rows
SELECT count(*) FROM shed_partitions
WHERE source='location_alias' AND partition_label='0' AND normalized_label='0';
-- EXPECTED: 0 (if >0, Class A corruption exists — escalate)

-- VERIFICATION 2: Confirm NO Class B redundant alias-as-shed rows
SELECT count(*) FROM locations
WHERE status='active'
  AND (name LIKE 'Godel 1 - Part %' OR name LIKE 'Godel 2 - Part %' OR name LIKE 'Mandela 2 - Part %');
-- EXPECTED: 0 (if >0, Class B redundant rows exist — escalate)

-- VERIFICATION 3: Confirm real parent sheds exist and hold their animals
SELECT name, status, location_type FROM locations
WHERE status = 'active' AND location_type = 'shed' AND name IN ('Godel 1', 'Godel 2', 'Mandela 1', 'Mandela 2')
ORDER BY name;
-- EXPECTED: 4 rows (Godel 1, Godel 2, Mandela 1, Mandela 2)

-- VERIFICATION 4: Confirm partition catalog is populated for each shed
SELECT shed_id, count(*) as partition_count FROM shed_partitions
WHERE shed_id IN (SELECT location_id FROM locations WHERE name IN ('Godel 1', 'Godel 2', 'Mandela 1', 'Mandela 2'))
GROUP BY shed_id
ORDER BY partition_count DESC;
-- EXPECTED: 4 rows, each with partition_count=10 (every shed has Part 1..10 in the catalog)

-- VERIFICATION 5: Sample goat distribution across real parent sheds
SELECT shed_id, count(*) as goat_count FROM goats
WHERE shed_id IN (SELECT location_id FROM locations WHERE name IN ('Godel 1', 'Godel 2', 'Mandela 1', 'Mandela 2'))
GROUP BY shed_id
ORDER BY shed_id;
-- EXPECTED: Live goats are distributed across the real parent sheds (exact counts may vary)
```

**If all queries return the expected results, STG data is correct.** No repair
is needed. The dropdown rendering issue (Godel 1 shown six times, Godel 1 1 /
Godel 1 10) is a frontend bug in the picker composition logic, not a data
problem — fix it in the code, not the database.

## Future Prevention (Beyond This Audit)

The root cause of the partition catalog class of bugs (per `AGENTS.md` "Operational
Location and Partition Convention") is architectural: **the catalog can store the same
pen two ways depending on `source`** — as a `shed_partitions` row scoped
under the correct parent (`goat_attested` / `manual`), and, independently,
as a free-standing top-level `locations` row of `location_type='shed'`
(`location_alias`-sourced). This audit found no such issues in current STG data.
However, to prevent similar classes of row from re-appearing on future imports, a schema change is needed:

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
  lock, 2026-08-06)" — the code-side convention that defines correct partition
  storage and display rules.
- `context/repo-audits/operational-location-do-not-reopen-ledger.md` — overall
  audit findings on partition/location handling (see "Class C: Mandela 1 orphans"
  entry, now marked RESOLVED-AS-NOT-A-DEFECT).
- `docs/decisions/operational-location-convention.md` — the architectural rule
  preventing a recurrence of this class of bug (requires `partition_id` schema
  migration).
