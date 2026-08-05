# OperationalLocation repair — findings and validation (2026-08-05)

Branch `fix/operational-location-partition`, worktree off fetched `origin/main`
`205d7a63d`.

## 1. Verdict: no database repair required

The reported symptom looked like missing partition data. It is not. Every alive
animal already carries a partition row; the defect is entirely in read models,
contracts, and UI collapsing back to the parent shed.

Proof, against the local clubbed DB
(`127.0.0.1:15635`, `goatos_stg_latest_clubbed_20260805`):

```sql
select count(*) from goats g
left join goat_shed_partitions gsp
  on gsp.tenant_id = g.tenant_id and gsp.goat_id = g.goat_id
where g.lifecycle_status = 'alive' and gsp.goat_id is null;
-- 0
```

| Measure | Value |
| --- | --- |
| Alive goats | 1670 |
| Alive goats with a `goat_shed_partitions` row | 1670 |
| Alive goats missing a partition row | **0** |

No partition backfill, no reseed, no data movement. The fix is read-model,
contract, and UI only.

## 2. Ground truth confirmed, including the park collision

Per-park partition counts reproduce the maintainer's stated numbers exactly:

| Park | Shed | Partition | Alive |
| --- | --- | --- | --- |
| Channapatna | Castro | 1 | 33 |
| Channapatna | Castro | 2 | 31 |
| Coimbatore | Castro | 1 | 63 |
| Coimbatore | Castro | 2 | 74 |
| Coimbatore | Castro | 3 | 65 |

**Shed names are not unique across parks.** `Castro`, `Gandhi`, `Godel 1`,
`Godel 2`, `Mandela 1`, `Mandela 2` and `Yashoda` each exist once per park with
distinct `location_id`s. A park-blind rollup silently merges them — grouping by
`Castro` alone yields 96/105/65 instead of the two parks' real splits above.

Consequence, now enforced in code: **every grouping, filter, and cache key uses
`shed_id` (uuid) plus park — never `shed_name`.**

## 3. Two label conventions, three "no partition" encodings

Live partition labels use two conventions simultaneously:

- bare numeric — `Castro 1/2/3`, `Gandhi 1/2/3`, `Yashoda 1..10`,
  `Ho Chi Minh 1`, `Old Yashoda 1..5`
- `Part N` prefixed — `Godel 1 - Part 3`, `Mandela 1 - Part 10`,
  `Sumathi 2 - Part 8`

and the codebase encodes "not partitioned" three different ways: SQL `NULL`,
empty string, and a literal `'whole'` sentinel.

The shared primitive collapses all three to one key and bridges both
conventions. Verified against the SQL normalizer already used by operator
execution reads — all 20 distinct live labels agree:

```
regexp_replace(lower(btrim(COALESCE(partition_label,'whole'))), '^part[[:space:]]+', '')
```

`'Part 3'` and `'3'` both key to `3`; `NULL`, `''`, `'whole'` all key to
`whole`. Display keeps the label as stored, so the screen matches the shed.

## 4. Inactive alias locations are real, and are the phantom dropdown rows

`locations` contains rows named `Castro 1`, `Gandhi 1 - Part 1`,
`Godel 1 - Part 3`, `Mandela 1 - Part 10` … with `location_type='shed'`,
`status='inactive'`, and **0 animals**. These are the "Castro 1 = 0 animals"
entries that make it look like animals do not live in partitions.

Rules applied:

- partitions are derived from `DISTINCT goat_shed_partitions.partition_label`
  for a shed, **never** from `locations` rows;
- inactive locations are excluded from every selectable-location catalog;
- no goat is ever written to an alias `location_id`.

## 5. Preserved data

The repaired clubbed vaccination history is untouched by this change (no
migration alters `vaccination_*` rows, no reseed was run):

- CPT adult ET+TT W1 — 85 on 2026-06-30 + 239 on 2026-07-01 = **324**
- CPT adult ET+TT W2 — 114 on 2026-07-24 + 163 on 2026-07-25 + 47 on
  2026-07-26 = **324**

Aug 5 and future vaccination/weighing data are likewise untouched. No writes
were made to STG.

## 6. Shared primitive

`backend/internal/platform/oploc` is the single definition of
OperationalLocation:

```
OperationalLocation = park + physical_shed + optional partition_label
```

- `NormalizePartition` — mirrors the SQL normalizer exactly
- `SamePartition` — convention-tolerant comparison
- `Display()` — `Yashoda` / `Castro 2` / `Godel 1 - Part 3`, and **never**
  `Yashoda whole`
- `Key()` — `shed_id` + normalized partition, so two parks' `Castro 1` stay
  distinct

Tests cover all three non-partitioned encodings, both label conventions, the
cross-park collision, and the "never show whole" rule.

## 7. Prevention

New guard `operational-location-guard`
(`tools/agent-hooks/check-operational-location.mjs`), registered in
`tools/ci/guardrail-manifest.json`, `Makefile:guardrails`, and
`tools/ci/run-local-ci.sh` (92 guards; `guardrail-registration-guard` green).

Checks, each with an adversarial self-test fixture:

| Check | Blocks |
| --- | --- |
| `whole-leak` | the `'whole'` sentinel concatenated into a user-facing label |
| `shifting-contract` | shifting submit lacking `destination_partition_label` |
| `location-type-as-partition` | `location_type` (an enum) used as a partition label |
| `counts-grain` | counts aggregation grouping by shed with no partition dimension |
| `alias-locations` | selectable-location query reading `locations` without excluding inactive rows |

The guard deliberately does **not** flag comparisons (`= 'whole'`) or
`COALESCE(..., 'whole')` — that is the matching key working correctly, and an
earlier draft that flagged it would have punished the code that already gets
this right.

### Surfaces the guard found that no manual audit did

Running it against the tree surfaced three files missed by all six audits:

- `backend/internal/counts/adapters/postgres/feed_projected_counts.go:100,160`
- `backend/internal/counts/adapters/postgres/shifting_feed_requirement.go:61`
- `backend/internal/counts/adapters/postgres/shifting_execution.go:843,844`

The last is the shifting **execution** (write) path reading `locations` without
excluding inactive rows — the path that could place an animal onto a dead alias
id. This is the strongest argument for the guard: hand audits missed it, the
machine check did not.

## 8. Verification boundary

State honestly what has and has not been proven at the time of writing.

- Proven: DB partition completeness; per-park ground-truth counts; Go/SQL
  normalizer parity across all live labels; `oploc` unit tests; guard self-test
  and real-tree scan; guardrail registration.
- Not yet proven at this point in the change: full backend test suite, OpenAPI
  validation, generated TS client check, admin-web typecheck, Android compile,
  and Chrome browser proof. Browser proof is **not** claimed until the pages are
  actually opened and inspected.
