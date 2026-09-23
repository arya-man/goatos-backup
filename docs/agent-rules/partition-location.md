# Operational Location and Partition Convention

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## Operational Location and Partition Convention (maintainer lock, 2026-08-06; clarified 2026-08-16)

**Read `docs/decisions/partition-is-operational-shed.md` FIRST. It outranks the
storage wording below.** In product terms `Castro 1` and `Castro 2` ARE sheds —
separate buildings, with animals physically in them. There is no operator-facing
"parent shed plus partition". Everything in this section describes how those
sheds are currently STORED while the operational-location migration is in
progress; it is not a claim about the farm.

Every goat's ground location is stored as: `park + physical_shed + optional
partition_label`. That triple is one shed. `shed_id` alone never names it.

**The convention is LOCKED by evidence from THREE independent sources (master registry, live BigQuery, legacy production code), with FOUR worked wrong-examples from production bugs. This section tightens the rule with those examples and a guard.**

### Rule 1: Normalize Partition Labels at Seed/Import

Sheds whose names share a base (`Castro 1`, `Godel 1 - Part 3`) are STORED as
`shed_name + partition_label`. This is a storage layout, not a statement that the
base name is a building:
  - `Castro 1`, `Castro 2`, `Castro 3` → three sheds, stored under one `locations`
    row `Castro` with labels `1`, `2`, `3`. `Castro` is grouping metadata; it is
    not a shed anyone works in.
  - `Godel 1 - Part 3` → the shed `Godel 1 - Part 3`, stored as `Godel 1` + `Part 3`

Undivided sheds (numeric-suffix names that are NOT subdivided, like `Ho Chi Minh 1`, `Yashoda`) → stored with NULL / '' / 'whole' partition. The `1` in the shed name is NOT a partition.

**NEVER seed raw partition strings as new `locations` rows.** The `locations`
table is the single source of truth for which sheds exist; inventing a row from a
label string duplicates a shed that is already stored. This is a rule about how
to WRITE `locations`, not a claim that the labelled sheds are less real than the
base name.

### Rule 2: Storage vs. Display Are Different (Maintainer 2026-08-05, clarified 2026-08-16)

Storage keeps the sheds `Castro 1` and `Castro 2` as `Castro + label 1/2`. Display ALWAYS puts the two halves back together, because the label half carries the shed's actual name:
- No label (NULL / '' / 'whole') → `Yashoda`, `Ho Chi Minh 1`: undivided sheds whose trailing digit is part of the name (Rule 1). Never `Yashoda - 2`, and never a bare base name for a shed that HAS a label
- Bare numeric partition → `Castro 1`, `Gandhi 2`, `Gandhi 3` (space separator; the farm's actual physical shed names as painted on buildings)
- Worded/prefixed partition → `Godel 1 - Part 3`, `Mandela 1 - Part 1` (dash separator; visual boundary since 75% of live shed names end in digits)

**Separator rule (2026-08-16 clarification):** Numeric partitions use SPACE because the farm's sheds ARE NAMED `Castro 1`, `Gandhi 2`, etc. — that is the real name painted on the building, not a display formatting choice. Worded labels use " - " (dash) for visual boundary: `Godel 1 - Part 3` is unambiguous from the shed name.

**NEVER render.** Where a forbidden string is shown it is paired with the correct
one; the numeric-dash rule is stated in words instead, so the wrong form is not
sitting on the page as something to copy:

- `WRONG: Yashoda whole` -> `RIGHT: Yashoda` — `'whole'` is a matching key, never user copy
- Numeric pens must not use dash separators: write `Castro 1`. A dash-separated
  numeric pen name contradicts the farm's physical naming and is never rendered.
- `WRONG: Godel 1 1` -> `RIGHT: Godel 1 - Part 1` — naive space-numeric join, truncated
- `WRONG: Godel 1` -> `RIGHT: Godel 1 - Part 3` — the base name alone when the shed has a label; both halves always render together

**Both layers must always be read together.** The normalization is a storage rule; the partition is a product rule (and the separator reflects the farm's real-world naming).

### Rule 3: Carry Partition in All Location-Bearing Responses

`shed_id` alone is NOT the ground location when a partition exists. Every location-bearing response struct MUST include:
- `shed_id` (UUID, the canonical key)
- `shed_name` (display name of the physical shed)
- `partition_label` (text or NULL)
- `operational_location_display` (backend-composed: `DisplayName(shed_name, partition_label)`)

Tables and response structs that **must** carry partition: `verification_items`, `weighing_campaign_sheds`, `health_cases`, shifting source/destination, counting/census rows, passport/herd register, vaccination detail.

### Rule 4: Query by `shed_id + park`, Not by Name

Group and key by `shed_id` (UUID) + park, NEVER by shed NAME. Names repeat across parks (two `Castro`, two `Gandhi`, two `Yashoda`). Name-keyed grouping silently merges parks — OL-2 worked example: six duplicate `Godel 1` rows in a shed selector because six partitions got grouped as one "Godel 1" row instead of six disjoint rows.

### Rule 5a: Use the Canonical FETCH, Not Just the Canonical Display

Composing the display has had a shared helper for a while. FETCHING the parts did not, so
every site wrote its own `SELECT` -- and the schema offers two columns that look
interchangeable and are not:

```
partition_label   'Part 3'   HUMAN label -- the only one that may be displayed
normalized_label  '3'        scrubbed MATCHING KEY -- joins only, never a screen
```

Selecting the wrong one compiles, passes review, and renders `Mandela 2 - 3` to an operator.
That defect shipped, was fixed, and was then REINTRODUCED hours later by a change in another
module that hand-wrote the same query. Centralising the fetch makes the mistake unavailable
rather than merely discouraged.

```
backend/internal/platform/oploc/resolve.go
  ShedScopedLocationSQL   the ONE query resolving a shed id -> (shed name, partition label)
  ResolveShedLocation()   scans it into an OperationalLocation
```

It bakes in the three rules that keep being re-derived wrong: `partition_label` never
`normalized_label`; `'whole'` filtered so it cannot reach a caller; and agree-or-go-bare via
`HAVING count(*) = 1` rather than `ORDER BY ... LIMIT 1`, which fabricates an answer that
silently flips as partitions change. An unresolvable shed returns the zero value with a nil
error, so callers DEGRADE to their location-less label instead of rendering a raw uuid or a
dangling separator.

Do not inline a partition `SELECT`. If a set-based read model genuinely cannot call into Go,
mirror `oploc.Display()` exactly and name it in a comment as the contract being mirrored.

### Rule 5: Use Canonical Composition, Never Hand-Roll

Shared location helpers exist in ONE place per language; use them instead of re-deriving:
- Go: `backend/internal/platform/oploc` → `DisplayName(shed, partition)`
- Admin-web: `apps/admin-web/lib/operational-location.ts`
- Android: `core/core-ui/.../PartitionLabel.kt`

Hand-rolled copies drift. OL-3 worked example: weighing screens rendered `Godel 1 1` (truncated partition name + shed name via naive join). OL-7 worked example: six SQL paths each composed the display differently (`Godel 1 - Part 3` vs. `Godel 1-Part 3` vs. `GODEL 1 - PART 3` vs. `Godel 1 Part 3`), breaking filtering and cross-screen navigation.

### Rule 6: New Tables Must Declare `partition_label`

Any table that records location must include a `partition_label` column (nullable for undivided sheds). OL-4/5/6 found examples:
- `verification_items` — missing partition (cannot tell which `Godel 1` partition a proof applies to)
- `weighing_campaign_sheds` — missing partition (ambiguous shed assignment)
- `health_cases` — missing partition (partition-scoped epidemiology is impossible)

### Worked Wrong Examples (All Production Bugs, 2026-08-06)

| Bug | Code | Impact | Fix |
|-----|------|--------|-----|
| **OL-1: Name-Keying Merge** | `groupBy { it.shedName }` collapses two `Castro` sheds across parks (different `shed_id`, same name) | 324 CPT adults + 89 Mandela adults both landed on one `Castro` selector row; operator selection was ambiguous | Use `groupBy { it.parkId to it.shedId }` |
| **OL-3: Naive Join Truncates** | `'Godel 1' + ' ' + 'Part 3'` → `'Godel 1 Part 3'` → truncated to `'Godel 1 1'` on screens | Weighing board unreadable; operators cannot identify partition | Use canonical `DisplayName(shed, partition)` |
| **OL-7: SQL Drift (6 paths)** | Feed query: `'Godel 1-Part 3'` / Dashboard: `'GODEL 1 - PART 3'` / Herd: `'Godel 1 Part 3'` | Same animal rendered differently on each screen; filtering broken | Audit all locations, use materialized `operational_location_display` or canonical helper |
| **OL-4: No Partition in New Tables** | `verification_items` lacks `partition_label` | Verifier cannot distinguish which `Godel 1` partition a proof is from; metrics aggregated at shed-level only | Add `partition_label` + compose in API responses |
| **OL-2: Duplicate Partition Rows in UI** | Six partitions of `Godel 1` rendered as six separate `Godel 1` rows in a shed selector instead of one shed with six partitions | Operator picker showed the same shed name six times with no way to tell partitions apart | Group by `shed_id` first, list partitions under it |

**All of these bugs came from hand-rolling composition or grouping by shed name.** The convention makes them impossible.

### Ten Defect Classes From Session 2026-08-07 (MUST-ENCODE)

Session 2026-08-07 found ~15 live defects, ALL from ONE class: partition/location handling failures across the ~5-handoff chain (SQL → Go → wire DTO → OpenAPI → client render). These ten rules encode the failures so the chain cannot break silently again:

1. **`normalized_label` is a MATCHING KEY, never display.** `partition_label` = `Part 3` (human), `normalized_label` = `3` (scrubbed, for joins). Six review rounds passed rendering `Mandela 2 - 3` (shed + key instead of shed + label) because reviewers checked field-carrying, never field-VALUE correctness.

2. **Location crosses ~5 handoffs; dropping it at ANY ONE shows bare shed name.** Real instances: domain correct + wire DTO dropped (verifier queue, submit header), wire correct + renderer ignored (Android verify, 18 admin-web sites), SQL correct + Go struct never declared (calendar chips). A gap anywhere in the chain renders the partition missing end-to-end.

3. **Never add a struct field without wiring it end-to-end.** FOUR instances: field DECLARED never populated (twice), schema declared what Go never emitted (twice), wire name renamed on one side only. A field nothing fills reads as done — worse than omitting it.

4. **Scan-count discipline:** adding a struct field without adding the SQL column is a RUNTIME failure (`number of field descriptions must equal number of destinations`). Adding a partition column and renaming a CTE column broke a query so badly it could not even EXPLAIN. If a downstream CTE groups, orders, scans, or renders a column, every upstream `SELECT s.*`/`SELECT *` carrier must explicitly project that column in the same patch.

5. **`jsonb_array_elements(x)::text` is NOT `jsonb_array_elements_text(x)`.** The first leaves JSON quoting and turns JSON null into the 4-character string `"null"` (non-empty, passes all "has partition?" checks) — fabricating a partition on a shed with none.

6. **Agree-or-go-bare:** compose a partition ONLY when every animal in scope resolves to the SAME real (non-'whole') partition; spanning several or none renders bare shed name. Never invent, never take `rows[0]`.

7. **Never key or group by shed NAME.** Names repeat across parks (Castro, Gandhi, Yashoda appear twice each). Name-keying merges COUNTS, not just labels.

8. **Parallel arrays must be built from the same grain.** One array `DISTINCT`, its partner not, silently shifts every index and pairs the wrong partition with the wrong shed.

9. **Tests must assert OUTPUT STRING against DB round-trip,** not field presence and not pure-Go formatter unit tests. Both weaker forms passed while real output was wrong.

10. **Fixes that regress the suite get REVERTED, not patched under pressure.** Two fixes this session regressed cross-surface parity tests and were reverted; record that as the expected response — never hold a breaking "fix" waiting for a second pass.

### "Active Shed" Means Active Location

The product concept **"active shed"** means **active operational location** (partition if subdivided, shed if not), not "physical building holding ≥1 live animal after collapsing partitions". Using the old definition produced parent-only dropdowns that forced operators to guess.

### Partitions with Zero Animals Still Exist

A partition holding ZERO animals still EXISTS (e.g., CBE `Yashoda 5` is real and empty). A partition catalog derived only from per-goat tables (`goat_shed_partitions`, PK `tenant_id, goat_id`) hides empty partitions and makes them unreachable as shifting destinations. Use the `locations` table as the partition catalog until a real `shed_partitions` table is built.

For write pickers such as Herd Register, shifting destinations, feed/vaccination
execution destinations, and weighing task setup, never derive selectable
operational locations from census/count facets. Facets answer "where animals
currently are"; write pickers answer "where animals/tasks are allowed to be".
Use the partition catalog (`shed_partitions` or the feed-config pens API that
exposes it), and fail closed if that catalog is unavailable.

### Machine Enforcement

`make operational-location-guard` (`tools/agent-hooks/check-operational-location.mjs`,
part of `make guardrails` and `make ci-local`) is a STATIC pattern scan, not a
runtime/seed-value checker. It checks:
1. No `GROUP BY` / `SELECT DISTINCT` on `locations.name` without `shed_id` + park co-grouping (`shed-name-keying`)
2. Hand-rolled display composition instead of the canonical helper — SQL `CASE`
   statements (`sql-display-drift`), Go string concatenation
   (`go-display-drift`), TypeScript (`ts-display-drift`), and Kotlin
   (`kt-display-drift`) must route through `oploc.Display()` /
   `operational-location.ts` / `PartitionLabel.kt` rather than re-deriving the
   string. **This checks composition-pattern shape, not runtime seed-value
   equality** — it does not execute a query or compare against known seed rows,
   so a hand-written helper that happens to match `DisplayName()` byte-for-byte
   on today's seeds but drifts on a future one is out of its reach.
3. New location-bearing tables declare `partition_label` column (`missing-partition-column`)
4. OpenAPI response schemas that identify a shed declare partition/display
   context alongside it (not name-only, and one hop into a `$ref`'d shed
   type) — a static schema-shape check. It does **not** verify that the Go
   struct or the actual wire emission populates those fields; that is
   item 4 in the Partition Change Verification Checklist below, done by
   hand.
5. `whole-leak`, `alias-locations`, `location-type-as-partition`, `counts-grain`,
   and `shifting-contract` — see the check list in the guard's own header
   comment for the full set and each check's rationale.
6. Herd Register partition picker source — the Register drawer must use catalog
   partitions, not count/census facets, so empty partitions remain reachable.
7. Weighing alias/idempotency invariants and staging deploy failure handling for
   the 2026-08-10 staging regression class.

**Not checked by this guard:** `jsonb_array_elements(x)::text` vs
`jsonb_array_elements_text(x)` (OL-5, the JSON-quoting/`"null"`-string defect
class in the partition-catalog session) has no static check in this file today
— it is caught only by code review and the `Partition Change Verification
Checklist` below. Do not assume `make operational-location-guard` would catch
a reintroduction of that defect.

Known blind spots: hardcoded string literals, runtime-composed strings in application code, reflective queries. Code review and the subagent brief catch those cases.

### Partition Change Verification Checklist

Before committing a change that adds, modifies, or displays a partition:

1. **SQL layer (backend/migrations/postgres):** 
   - [ ] New location-bearing table includes `partition_label` column (nullable for undivided sheds)
   - [ ] If populating from existing data, verify both the source query and the target column read the same grain (test on real seed data)
   - [ ] EXPLAIN on the updated query with ~500k row bounds shows no Seq Scan on large tables
   - [ ] If using `jsonb_array_elements` on label arrays, use `jsonb_array_elements_text(x)` (never the `::text` cast)

2. **Go domain/wire layer (backend/internal):**
   - [ ] Struct in `internal/**` domain declares `partition_label` (text pointer or string) + `operational_location_display` (string)
   - [ ] All writers populate both fields (scan each constructor/builder/adapter)
   - [ ] Use only `platform/oploc.DisplayName()` to compose the display string; never hand-roll

3. **OpenAPI contract (contracts/openapi/app-api.yaml):**
   - [ ] Response schema declares `shed_name`, `partition_label`, `operational_location_display` (mandatory for location-bearing rows)
   - [ ] Request schema (if location is input) declares the expected input shape (e.g., `shed_id` alone or `shed_id + partition_label`)
   - [ ] Compare schema and Go struct field-by-field; they must match exactly

4. **Client render (admin-web / Android):**
   - [ ] Generated TypeScript/Kotlin client receives the backend-composed `operational_location_display`
   - [ ] Renderer uses that string, never hand-rolls location composition
   - [ ] For partition pickers: group by `shed_id` + `park`, never by `shed_name`
   - [ ] Visual proof: screenshot showing correct `Godel 1 - Part 3` format (dashed, both halves), not `Godel 1 1` (truncated) or bare `Godel 1` (missing partition)

5. **Test closure (must run before push):**
   - [ ] Unit test on the Go `DisplayName()` helper or formatter covers all known locations (subdivided + undivided)
   - [ ] Integration/E2E test asserts the OUTPUT STRING on a DB round-trip (not field presence alone)
   - [ ] If a partition picker or grouping changed, confirm cross-surface agreement on counts/labels (Calendar vs. Vaccination Board vs. Herd Register)
   - [ ] Run `make operational-location-guard` — must pass

### Five Hard Rules From Session 2026-08-07 (MUST-ENCODE)

These rules cost real bugs today. Each one makes a class of defect impossible. Encode them in every subagent brief and code review:

#### Rule 1: An Undivided Shed Whose Name Ends in a Number Is Never Split

`Yashoda 2` is a SHED NAME, whole. It renders `Yashoda 2`, never `Yashoda - 2`. Same for `Ho Chi Minh 1`. The trailing number is part of the name, not a partition. Contrast with a genuinely partitioned shed: `Mandela 1` + `Part 2` renders `Mandela 1 - Part 2`.

**Defect discovered:** A test fixture fed `operationalLocationLabel("Yashoda", "2")` and its expectation was "corrected" to `Yashoda - 2`. The formatter was right for those inputs; the FIXTURE was wrong, and it taught every reader that `Yashoda - 2` is a real label. **Rule: a fixture that asserts a shape the farm does not have is a defect even when the assertion passes.** Never hand-wave away green tests on wrong data shapes.

**Verification:** Grep for every shed name in `backend/migrations/postgres/` backfill scripts and seed code. Match against the master registry (`wiki/Sheds DB.xlsx`). Names with trailing numbers must be checked: if they appear in `goat_shed_partitions` or `shed_partitions` with a partition suffix (e.g., `Yashoda` + partition `2`), they ARE split; if they appear ONLY in `locations` with NULL partition, they are NOT split.

```bash
# Grep evidence: check seed code for undivided shed names
grep -n "Yashoda\|Ho Chi Minh" backend/cmd/seed-*/main.go
# Should show: only whole sheds, no partition assignments
```

#### Rule 2: A Required Contract Field Must Be Populated on Every Construction Path, in the Same Change

Marking a field `required` in OpenAPI while the Go struct lacks it, or has it and never fills it, ships a contract the client cannot rely on. This happened EIGHT times on this branch.

**Defect discovered:** `operational_location_display` was marked required on `WeighingShedVideos` in OpenAPI while the serving struct had neither field nor composition logic. Clients faithfully rendered null/absent.

**The checklist (mandatory):** SQL column → scan destination → Go struct field → populated at every construction site → wire DTO → OpenAPI → generated client → a renderer that actually reads it. A gap at ANY hop renders bare location end to end.

**Verification:** For every location-bearing response field added:
1. Grep the SQL schema for the column
2. Grep the repository's SELECT clauses for the column in the same query
3. Grep the Go struct for the corresponding field
4. Grep the adapter/builder for an assignment to that field
5. Grep OpenAPI for the declared response field
6. Run `npm run client:generate` (admin-web) or `make build-android` (mobile) and confirm the generated client includes the field

If ANY step is missing, the field is a contract lie.

```bash
# Grep evidence: every handoff from SQL to OpenAPI
grep -n "partition_label\|operational_location_display" backend/internal/*/adapters/postgres/repository.go
grep -n "partition_label\|operational_location_display" backend/internal/*/domain/types.go
grep -n "PartitionLabel\|OperationalLocationDisplay" contracts/openapi/app-api.yaml
```

#### Rule 3: Scaffolded Is Not Wired

A migration, a domain field, a decoder helper, an OpenAPI entry and two client DTOs can all exist while the repository and handler touch none of them. **The verification partition feature sat in exactly that state; its composite-key decoder was called only by its own unit test.** Field-presence tests and pure-formatter unit tests both passed while real output was wrong.

**Defect discovered:** A partition feature added SQL migration (000125), domain field (`PartitionLabel`), decoder helper (`parsePartitionLabel`), OpenAPI schema (`PartitionLabel`), and client DTOs — yet the serving handler never called the decoder, never populated the field, and real API responses carried null/missing partition.

**Rule: a feature is not done until a test asserts the OUTPUT STRING on a real round trip.** Field-presence tests and pure-formatter unit tests both pass for scaffolding. Proof requires:
1. Insert a test shed with partition into the test DB (e.g., `Godel 1 - Part 3`)
2. Call the API/screen that READS that shed
3. Assert the RETURNED STRING exactly matches the database round-trip (e.g., `operational_location_display = 'Godel 1 - Part 3'`)

```bash
# Grep evidence: verify the handler calls the decoder/resolver
grep -A 20 "func.*weighing.*List" backend/internal/weighing/adapters/postgres/repository.go | grep -i partition
# Should show: a call to oploc.ResolveShed or direct SelectPartition in the query
```

#### Rule 4: Verify Data Against the Live Database Before Writing a Repair

~500 lines of guarded repair SQL, a runbook and a decision process were written against a mistaken reading of STG inferred from code and a stale audit. A single read-only check showed every repair class returns ZERO rows.

**Defect discovered:** Repair scripts were generated to handle hypothetical `Mandela 1` partition-catalog orphans that never existed in STG. The database read showed 10 partitions correctly cataloged, no orphans, no breakage.

**Rule: query the live database FIRST; a repair script written from inferred shape is a destructive operation aimed at a problem that may not exist.**

**Verification before writing ANY repair:**
1. Run a read-only verification query against STG via the runbook (`docs/runbooks/google-cloud-environments.md`)
2. Confirm the defect class exists and quantify affected rows
3. Verify the repair will not delete correct data (dry-run with `RETURNING` to see target rows)
4. Only after proof of existence, write the repair

```bash
# Grep evidence: verification queries must run before repair authoring
# Example: count orphan partition-catalog rows BEFORE repair authoring
SELECT COUNT(*) FROM shed_partitions sp
WHERE NOT EXISTS (
  SELECT 1 FROM locations l
  WHERE l.tenant_id = sp.tenant_id
  AND l.shed_id = sp.shed_id
);
# MUST return > 0 before any repair is written
```

#### Rule 5: Confirm the Repo Path Before Editing

This workspace has multiple checkouts (`<another checkout>`, `<this repo>`, review worktrees). An agent did a full task in the wrong one and the work was unusable; it also reported that files "don't exist" when it was simply in the wrong tree.

**Defect discovered:** Subagent reported `docs/decisions/operational-location-convention.md` missing after editing `<another checkout>/docs/decisions/operational-location-convention.md` instead of `<this repo>/docs/decisions/operational-location-convention.md`.

**Rule for delegated work:** state the absolute repo path in the brief and confirm with `git rev-parse --show-toplevel` before the first edit. "File not found" means check the tree before concluding the code is missing.

```bash
# Grep evidence: verify every agent logs its repo root
# Expected in every agent session start:
git rev-parse --show-toplevel  # Must print THIS repo root, not another checkout
```

### Settled Model for Partition Documentation

**State this plainly wherever partitions are described**, because it was misread twice today:

- A shed is `Mandela 1` (physical building name)
- Its pens are partitions `Part 1`, `Part 2`, etc., stored as `partition_label` under that shed
- **Verified read-only against live STG on 2026-08-07**
- Partitions are NOT separate shed rows and must NOT be restructured into them
- The old `Mandela 1 - Part N` location rows are INACTIVE aliases only (legacy data shape)

### Subagent Brief Rule

**If you delegate location-bearing work to a subagent, include this rule in the brief.** Quote the worked examples, the five rules above, and name what makes the work location-bearing ("updates shed-scoped queries" or "adds a location picker"). An agent never told the boundary will cross it reasonably.

### Related Documentation

- Full decision: `docs/decisions/operational-location-convention.md` → evidence table, all worked examples, closure criteria
- Defect ledger: `context/repo-audits/operational-location-do-not-reopen-ledger.md` → all bugs found 2026-08-06, closure status
