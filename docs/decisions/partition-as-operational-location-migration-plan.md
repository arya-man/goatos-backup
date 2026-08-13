# Partition As Operational Location Migration Plan

Status: draft audit plan
Owner: Goat OS
Date: 2026-08-10

## Decision Direction

Physical partition/pen is the operational location. If goats live in
`Godel 1 Part 1`, the goat's canonical residence must be that exact pen, not
the abstract parent `Godel 1`.

Target invariant:

| Column | Meaning after migration |
| --- | --- |
| `goats.current_location_id` | Exact operational residence: pen for partitioned sheds, shed for undivided/lumpsum sheds |
| `goats.shed_id` | Physical parent-shed rollup for this migration; changing it to a pen id or removing it is out of scope |
| `goats.park_id` | Parent park rollup |
| `goat_shed_partitions` | Temporary compatibility mirror, not long-term residence truth |

Do not reinterpret every existing `shed_id` as a pen id. Existing APIs, Android
routes, admin filters, reports, idempotency keys, and queued offline work already
use `shed_id` as the physical parent shed. The safe domain change is to make
`current_location_id` the real place and keep parent identity explicit for
rollups.

This plan supersedes the current parent-shed-plus-partition operational-location
convention. Before implementation, add a short ADR update that states the new
invariant and updates the operational-location guard.

## Why This Exists

Current Goat OS has two competing sources for where a partitioned goat lives:

1. `goats.shed_id` / `goats.current_location_id` usually point to the parent
   shed.
2. `goat_shed_partitions.partition_label` says which physical partition the goat
   is actually in.

That is why a partition can have animals in vaccination/weighing history but
still disappear from some Android/admin dropdowns or rosters: the code path may
look only at active `locations` rows or only at `goats.shed_id`, then never joins
the partition mapping.

## Execution Plan

This is a small-data migration. Goat OS does not need a big-company migration
ceremony for fewer than 2,000 animals. The plan is to make the mapping explicit,
prove every live goat, then update the open work that depends on that mapping.

1. Create one canonical active `location_type='pen'` row under each physical
   parent shed. Reuse a row only when tenant, type, and parent already match.
   Preserve legacy park-level alias rows as migration evidence; do not activate
   them as canonical pens. Example: `Godel 1 Part 1`, `Godel 1 Part 2`,
   `Mandela 2 - Part 6`.
2. Add and backfill durable `shed_partitions.operational_location_id`, keyed by
   `(tenant_id, shed_id, normalized_label)`. Enforce a tenant-scoped FK to one
   active pen under `shed_id` and uniqueness of each operational location.
   For an undivided shed with no active partition mappings, the parent shed is
   the operational location. A missing or `whole` partition under a subdivided
   shed is ambiguous and blocks migration.
3. Prove live goats by RFID:
   start from each live goat's active primary `animal_identifier_1` RFID, join
   to `goats` and `goat_shed_partitions`, and require exactly one
   parent-plus-partition mapping. Missing RFID, duplicate RFID, missing
   partition evidence, or multiple mappings blocks cutover.
4. Update live goats:
   `goats.current_location_id = operational_location_id`.
   Keep `goats.shed_id = parent_shed_id` for rollups/reporting.
5. Regenerate the compatibility mirror:
   `goat_shed_partitions` should match the goat's current pen location, not be
   an independent truth source.
6. Remap open work only:
   vaccine obligations/assignments, open weighing buckets/work items, pending
   shifting/feed/SOP/verification rows.
7. Preserve terminal history:
   accepted proofs, completed vaccination, submitted weighing observations,
   finalized snapshots, location history, immutable event payloads, and
   completed feed/shifting/SOP rows retain their original values.
8. Change code so new features ask one question:
   “what is the goat's `current_location_id`?” The parent shed is only for
   grouping.

If a live goat has `goat_shed_partitions` but no matching active partition
location, the fix is not to keep it on the parent shed. The fix is to create the
missing partition location and map the goat there.

Live means `lifecycle_status NOT IN ('dead','sold','culled','transferred',
'lost','merged','inactive')` and `merged_into_goat_id IS NULL`.

## Non-Negotiable Audit Rule

Do not migrate by memory. Every affected table must be classified before code or
STG data changes:

- canonical animal residence
- partition catalog / compatibility bridge
- assignment or work item
- proof/completion/history snapshot
- read model/projection/cache
- import/seed/tooling data

For each table, record whether it should point to the exact operational
location, the rollup parent shed, both, or preserve immutable historical values.

## Core Location Model

| Store | Current assumption | Required target |
| --- | --- | --- |
| `locations` | Supports `pen`, but most code validates active direct `location_type='shed'` under a park. | Create one active pen row for each real partition. Undivided sheds remain shed rows. Add guards for `park -> shed -> pen` hierarchy, active ancestors, tenant, no cycles, and no duplicate normalized pen labels under a shed. |
| `shed_partitions` | Catalog of labels under a parent shed, but no stable `location_id` bridge and fresh DBs can miss rows. | Repair the catalog first. Add durable `operational_location_id` with uniqueness and hierarchy guards. Include empty pens, not only pens with goats. |
| `goat_shed_partitions` | Current side-table residence truth for imported goats. | Use as migration evidence and temporary compatibility mirror. It must become derivable from `goats.current_location_id`. |
| `goats` | `current_location_id` and `shed_id` often both equal parent shed. | `current_location_id` exact pen/shed, `shed_id` rollup parent shed, `park_id` rollup park. Reject rows where the three disagree. |
| `goat_location_history` | Parent location ids plus nullable partition labels. | New rows write exact operational ids and retain labels as snapshots. Existing history is terminal and must not be rewritten. |
| `goat_identity_events` | JSON payloads and scope are parent-shed-oriented. | Version events to include operational location id plus rollup shed/park. Preserve old event meaning. |
| `location_aliases` | Active aliases are tenant/source scoped and not safely park-scoped. | Alias mapping must include park/parent context or it can collide across CBE/CPT repeated names. |
| `location_capacity_records`, `shed_profiles` | Capacity/profile can be duplicated between location records and shed profile. | Define inheritance: parent shed defaults, pen override optional. Avoid multiplying parent capacity across every partition. |

Decision: every physical partition is represented by an active
`location_type='pen'` row under its parent shed. Obligations currently validate
`scope_type = locations.location_type`, so obligations must support
`scope_type='pen'` or scope validation must decouple work scope names from raw
location type before pen rows are used.

## Highest-Risk Domains

### Identity, Intake, And Movement

All goat writers must be converted before the data backfill, otherwise the next
move/intake will undo the new residence model.

| Surface | Current risk | Required change |
| --- | --- | --- |
| Admin goat create / relocation | Validates destination as direct child `location_type='shed'` and writes both `current_location_id` and `shed_id` to that parent. | Accept operational pen or undivided shed. Resolve rollup shed/park from hierarchy. Write all three fields atomically. |
| Bulk relocation / shifting apply | Keeps parent shed as canonical and partition in side table. | Resolve legacy `(shed_id, partition_label)` to operational id; write current location exact, shed rollup, partition mirror. |
| Procurement `AcceptIntake` | Accepts parent shed plus partition and writes `goat_shed_partitions`. | Add operational location resolution. Keep handoff/source snapshots, but place goat into exact location. |
| `procurement_pc_handoffs` | Stores only park/shed. | Add operational location for new handoffs or preserve as rollup-only with explicit derived placement at accept time. |
| Seed/import/repair tools | Several guards expect `current_location_id = shed_id`. | Replace with hierarchy consistency: current location is a shed or pen under `shed_id`, and `shed_id` is under `park_id`. |

### Vaccination And Obligations

Vaccination is the most fragile surface because eligibility, assignments,
members, proof, and command boards use parent shed plus partition side data.

| Table/code | Current risk | Required target |
| --- | --- | --- |
| `obligation_instances(scope_type, scope_id)` | Goat obligations commonly scope to parent shed. Existing validation rejects mismatched location type. | Open/future goat obligations scope to exact operational location, or carry exact operational id beside legacy scope. Completed obligations preserve historical scope. |
| `obligation_batches` | Batch may be park/sweep level while instances are shed level. | Keep batch as planning scope if needed; exact member/work scope must be operational. |
| `obligation_goat_shift_watermarks` | Last scope tracks parent shed movement. | Track exact operational location so sibling pen movement reschedules correctly. |
| `vaccination_drive_assignments` | Keyed by `park_id`, `shed_id`, `physical_shed`, `partition_label`; membership joins compare parent shed and side-table label. | Add/derive operational location id. Future assignments use exact location. Legacy fields remain display/back-compat until clients migrate. |
| `vaccination_drive_assignment_members` | Bound to assignment rows generated under old identity. | Rebuild/reconcile open and future members after remap. Preserve completed members and completions. |
| `vaccination_completions` and proof records | Accepted proof/completion history can lose audit meaning if rewritten. | Preserve immutable completed history. Only remap pending/open work where source rows prove exact pen. |
| Command board / adherence / control tower | Filters and summaries assume parent `shed_id`. | Parent filters include descendants; exact operational filters exclude siblings. |

### Weighing

Weighing must remain bucket-first and free-flow. Do not resurrect
`weighing_expected_animals` as roster truth.

| Table/code | Current risk | Required target |
| --- | --- | --- |
| `weighing_campaign_sheds` | Bucket identity is `location_id + partition_label`; old DB indexes on STG still blocked sibling partitions under one parent shed. | Preserve `campaign_shed_id`. For open/future buckets, set `location_id` to exact operational location and keep label/display as snapshot. Keep partition-aware uniqueness only. |
| `weighing_work_items` | Mirrors campaign bucket into operator work. | Keep in sync with campaign bucket remap. |
| `weighing_observations` | Stores expected/current/actual location snapshots. | New observations store exact operational ids. Preserve submitted historical snapshots. |
| `weighing_shed_observations` and proofs | Tied to `campaign_shed_id`. | No rewrite if `campaign_shed_id` is preserved; remap open proof scopes only when exact. |
| `weighing_shed_load_tags` | Parent location cannot distinguish sibling partition loads. | Remap to exact operational location or block ambiguous loads. |
| Admin Weights read models | One current query path references `partition_label` without selecting it. | Fix independently before relying on admin weight views for migration proof. |

STG example already seen: obsolete parent-only indexes
`uq_weighing_open_shed_per_park_date_v2` and
`uq_weighing_open_shed_per_park_date` canceled sibling buckets like
`Godel 2 - Part 2`. The correct rule is one open weighing bucket per
park/date/operational location, not one per parent shed.

### Counts, Shifting, And Herd Projections

| Surface | Current risk | Required target |
| --- | --- | --- |
| Count anchors/projections | Many rows store only parent `shed_id`. | Rebuild projections after backfill. Parent totals must be explicit rollups from pen rows. |
| Counts breakdown | Backend may group by partition, but Android cache keys can omit partition. | Add partition/operational id to cache identity before partition-level rows become authoritative. |
| `shifting_events` | Source/destination columns are named and used as parent shed ids. | Add operational source/destination ids or resolve legacy parent+partition to exact ids before execution. |
| Shifting verification | Request/bridge can drop partition and enqueue only parent shed. | Carry exact operational id; preserve old pending items only if source event can resolve one pen. |
| Herd register projections | Derived from `goats.current_location_id`. | Rebuild after migration and add parent rollup views where dashboards still need shed totals. |

### Feed

| Surface | Current risk | Required target |
| --- | --- | --- |
| `feed_shed_factors` | Parent-shed-only factors. | Decide parent inheritance vs pen override. Do not silently multiply parent config across every pen. |
| `feed_experiment_config` | Uses parent shed plus partition key. | Map experiment pen keys to operational ids and keep old keys as compatibility. |
| Feed direction | Completion payload can carry only `shed_id`, while packing/distribution can carry `partition_label`. | Old queued direction completions must remain shed-level or be rejected; server must not guess exact pen. New direction work carries operational id. |
| Feed transport | Already expects partition-specific tasks in newer migration notes. | Ensure one transport task per operational location where workflow requires it, and no abstract parent task for a subdivided shed. |

### SOP, Proof, Verification, Calendar, Workforce

| Surface | Current risk | Required target |
| --- | --- | --- |
| `sop_tasks(scope_type, scope_id)` | Scope is generic and may be parent shed. | Partition-level SOP tasks scope to exact operational location; parent-rollup SOP must be explicit. |
| `sop_submissions`, proof artifacts | Accepted evidence is immutable snapshot. | Preserve history. Pending/open proof scopes can be remapped only with exact source mapping. |
| `verification_items` | Shed filter keys can be `shed_uuid#partition`. | Add compatibility decoder and exact operational id. Sibling proof must not satisfy another pen. |
| Calendar/process integrity | Joins often require goat current location to be `location_type='shed'` and direct park child. | Resolve park through hierarchy and support pen current locations. |
| Workforce/permissions | Park resolution can assume one parent hop. | Parent filters/grants include descendant pens; exact pen permission remains possible later. |

## Contracts And Clients

The migration cannot be DB-only because shipped clients and generated APIs use
legacy field names everywhere.

Required compatibility contract:

1. Accept either `operational_location_id` or legacy `(shed_id, partition_label)`.
2. When both are supplied, resolve both and reject disagreement with `409` or
   `422`; never guess.
3. Keep existing `{shed_id}` routes stable as parent-shed routes, or add
   versioned `/operational-locations/{id}` routes.
4. Preserve legacy `shed_id` query semantics as parent plus descendants.
5. Add exact `operational_location_id` filters where callers need one pen only.
6. Canonicalize both request forms before new idempotency fingerprints, while
   preserving already queued offline fingerprints.
7. Ship Android Room/cache migrations that re-key read caches but preserve
   outbox rows, proofs, feed captures, drafts, and unsynced work.
8. Regenerate OpenAPI clients only after contract changes are explicit.

Known client blockers:

- Android counts cache can collapse sibling partition rows if partition is not
  part of the cache key.
- Android vaccination/feed/weighing routes and outbox payloads carry legacy
  `shedId` plus optional `partitionLabel`.
- Admin web location pickers synthesize partition options from parent sheds and
  partition labels.
- Generated TypeScript and Kotlin DTOs expose both snake/camel aliases; do not
  clean those up during migration.

## Table Inventory Summary

The table audit reduces to four actions:

1. **Map live state:** current goat residence, open work, active assignments,
   current proof queues, and current projections.
2. **Preserve terminal history:** completed work, accepted proofs, submitted
   observations, finalized snapshots, immutable event rows, and old audit logs.
3. **Rebuild mutable derived rows:** herd register current projections,
   open/current count projections, vaccination rollups, calendar/process views,
   and mobile/admin caches.
4. **Leave unrelated location data alone:** farm/park profiles, feed rates,
   workforce member records, inventory history, milk farm workflows, and broad
   source/holding locations unless their own row has exact pen evidence.

| Area | Tables that need direct migration or rebuild |
| --- | --- |
| Location/herd | `locations`, `shed_partitions`, `goats`, `goat_shed_partitions`, `goat_location_history`, `goat_identity_events`, `herd_register_goat_projection`, `herd_register_summary_projection`, `location_aliases`, `location_review_items` |
| Vaccination/obligation | `obligation_batches`, `obligation_instances`, `obligation_goat_shift_watermarks`, `vaccination_drive_assignments`, `vaccination_drive_assignment_members`, `vaccination_eligibility_rollups`, `vaccination_reminder_cadence_fires` |
| Weighing | `weighing_campaign_sheds`, `weighing_work_items`, `weighing_observations`, `weighing_shed_load_tags`, proof/idempotency rows inherited from campaign buckets |
| Counts/shifting | `count_base_anchors`, `count_projection_exceptions`, `count_projection_snapshot_rows`, `count_projection_snapshots`, `shifting_events`, shifting verification rows |
| Feed | `feed_experiment_config`, `feed_direction_issue_rows`, `feed_distribution_completions`, `feed_packing_completions`, `feed_transport_tasks`, active feed direction work |
| SOP/proof/verification | `sop_tasks`, `sop_submissions`, `proof_artifacts`, `verification_items`, `audit_log` for future typed scopes |
| Procurement/source | `procurement_load_goats`, `procurement_pc_handoffs`, `source_holding_stays`, `transit_handoffs` only when exact pen evidence exists |
| Health/workflows | `health_cases`, `workflow_instances` for active/current rows with exact shed+partition evidence |
| Access/calendar/workforce | `user_scope_grants`, `workforce_roster_assignments`, calendar scope resolution, process-integrity views |

Explicitly do not recreate `weighing_expected_animals`; it was dropped and
weighing should stay free-flow.

## Data Inventory To Produce

Create:

`docs/decisions/artifacts/partition-operational-location-table-inventory-2026-08-10.md`

Minimum SQL inventory:

```sql
-- Location hierarchy and candidate pen rows.
SELECT location_type, status, COUNT(*)
FROM locations
GROUP BY location_type, status
ORDER BY location_type, status;

-- Partition catalog rows without an active operational location.
SELECT sp.tenant_id, sp.shed_id, parent.name AS parent_name, sp.partition_label
FROM shed_partitions sp
JOIN locations parent ON parent.location_id = sp.shed_id
LEFT JOIN locations pen
  ON pen.tenant_id = sp.tenant_id
 AND pen.parent_location_id = sp.shed_id
 AND lower(regexp_replace(pen.name, '\s+', ' ', 'g')) =
     lower(regexp_replace(parent.name || ' - ' || sp.partition_label, '\s+', ' ', 'g'))
 AND pen.status = 'active'
WHERE pen.location_id IS NULL;

-- Live goats whose side-table partition cannot resolve to one mapped active pen.
SELECT g.goat_id, gi.identifier_value AS rfid, g.park_id, g.shed_id,
       gsp.partition_label, COUNT(sp.operational_location_id) AS mappings
FROM goats g
JOIN goat_shed_partitions gsp
  ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
LEFT JOIN goat_identifiers gi
  ON gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id
 AND gi.identifier_type = 'animal_identifier_1'
 AND gi.is_primary_for_goat
 AND gi.status = 'active'
LEFT JOIN shed_partitions sp
  ON sp.tenant_id = g.tenant_id
 AND sp.shed_id = g.shed_id
 AND sp.normalized_label = lower(regexp_replace(gsp.partition_label, '\s+', ' ', 'g'))
LEFT JOIN locations pen
  ON pen.tenant_id = sp.tenant_id
 AND pen.location_id = sp.operational_location_id
 AND pen.parent_location_id = g.shed_id
 AND pen.location_type = 'pen'
 AND pen.status = 'active'
WHERE g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
  AND g.merged_into_goat_id IS NULL
GROUP BY g.goat_id, gi.identifier_value, g.park_id, g.shed_id, gsp.partition_label
HAVING COUNT(sp.operational_location_id) <> 1
    OR COUNT(pen.location_id) <> 1
    OR COUNT(gi.identifier_value) <> 1;

-- Current-location/shed/park hierarchy consistency after backfill.
SELECT g.goat_id, gi.identifier_value AS rfid, g.current_location_id, g.shed_id, g.park_id
FROM goats g
LEFT JOIN goat_identifiers gi
  ON gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id
 AND gi.identifier_type = 'animal_identifier_1'
 AND gi.is_primary_for_goat
 AND gi.status = 'active'
JOIN locations cur ON cur.tenant_id = g.tenant_id AND cur.location_id = g.current_location_id
JOIN locations shed ON shed.tenant_id = g.tenant_id AND shed.location_id = g.shed_id
WHERE g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
  AND g.merged_into_goat_id IS NULL
  AND (
    NOT (
    (cur.location_type = 'pen' AND cur.parent_location_id = g.shed_id)
    OR (cur.location_type = 'shed' AND cur.location_id = g.shed_id)
  )
    OR shed.parent_location_id <> g.park_id
  );

-- Open/future work that still points only at abstract parent sheds.
-- Fill this section per domain: obligations, vaccination assignments,
-- weighing buckets/work items/proofs, shifting pending events, feed tasks,
-- SOP tasks, verification items.
```

The inventory must include CBE/CPT sample RFIDs traced end-to-end:

- `goats`
- `goat_identifiers`
- `goat_shed_partitions`
- vaccination obligations/assignments/members
- weighing campaign buckets/observations
- shifting/feed/SOP/proof/verification rows when present

## Migration Workstreams

### 1. Repair Catalog First

- Build deterministic partition catalog from source docs, seeded data,
  `goat_shed_partitions`, vaccination/weighing evidence, and empty known pens.
- Do not parse any trailing number as a partition; names like `Ho Chi Minh 1`
  are real shed names.
- Ensure every partition has exactly one active operational location row.
- Add a bridge from partition catalog to operational location id.

### 2. Add Resolver And Compatibility Layer

Add one shared resolver:

```text
legacy parent shed + partition label -> operational location id
operational location id -> rollup shed id + park id + display name
```

All writers must use it. All readers that accept legacy filters must use it.

### 3. Convert Writers Before Data

Priority:

1. Goat create, procurement accept, relocation, shifting apply, seed/import.
2. Vaccination generation/execution and obligation rescope.
3. Feed direction/packing/distribution/transport.
4. SOP/proof/verification producers.
5. Weighing planners/work items/proof scope.

### 4. Backfill Live Residence

Backfill only after writers are safe:

1. Create a live-goat migration audit artifact containing:
   `(tenant_id, goat_id, rfid, old_parent_shed_id, partition_label,
   operational_location_id, rollup_shed_id, park_id, reason)`.
   This records use of the authoritative parent-plus-partition mapping; it is
   not that mapping itself.
2. Block if any live goat mapping is missing, duplicate, inactive, cross-park,
   or derived only from ambiguous alias text.
3. Update `goats.current_location_id` to operational location.
4. Keep `goats.shed_id` as rollup parent and validate hierarchy.
5. Refresh `goat_shed_partitions` compatibility mirror from current location.

### 5. Remap Open/Future Work

Remap only open, non-terminal work, including scheduled future work. A unique
mapping is mandatory; ambiguous rows block for explicit repair. Terminal rows
are never remapped.

- open/future vaccination obligations, assignments, assignment members
- active obligation watermarks
- open weighing buckets, work items, load tags, pending proof scopes
- pending shifting events and verification queue items
- open feed direction/packing/distribution/transport work
- open SOP tasks and pending verification items
- read models/projections/caches

Preserve completed vaccination completions, accepted proofs, submitted weighing
observations, finalized snapshots, completed feed/shifting/SOP history, location
history, and old event payloads as snapshots.

### 6. Rebuild Projections And Caches

- Herd register/count projections
- Feed projected counts
- Vaccination rollups
- Calendar/process-integrity views
- Android Room/read caches through versioned migrations
- Admin option/read models

## Required Test Gates

| Gate | Must prove |
| --- | --- |
| Mapping | Two sibling pens, an empty pen, undivided shed, repeated shed names in CBE/CPT, retired pen, missing mapping, ambiguous alias. |
| Writers | New create/intake/move/shift never reverts `current_location_id` to parent shed. |
| Vaccination | Open obligations and assignment members match the goat's exact operational location; sibling pen proof cannot satisfy another pen. |
| Weighing | Sibling buckets can coexist; `campaign_shed_id` remains stable; free-flow scans do not depend on a goat roster table. |
| Counts/shifting | Pen totals sum to parent totals; pending movement deltas use exact source/destination; no goat counted twice. |
| Feed | Parent factor inheritance and pen overrides are explicit; open queued feed work is remapped only when parent shed plus partition resolves uniquely; terminal feed completions are never rewritten. |
| SOP/proof/verification | Parent rollup tasks are explicit; partition tasks are exact; historical accepted evidence remains unchanged. |
| Contracts/mobile | Old and new JSON decode; old queued outbox survives; mismatch requests reject; Android cache keeps sibling partitions separate. |
| Seeds/guards | Integrity checks validate hierarchy, not `current_location_id = shed_id`. |
| Cutover | Zero terminal-row mutations, stable open-work counts, idempotent reruns, and continued absence of `weighing_expected_animals`. |

## Things That Can Go Wrong

- Activating partition location rows makes dropdowns visible, but rosters still
  stay empty because membership reads ignore `current_location_id`.
- Moving only DB data fails because relocation/procurement/seed writers put goats
  back on the parent shed.
- Changing `shed_id` to pen id breaks parent filters, URLs, Android cache keys,
  admin grouping, reports, and queued offline commands.
- Using only `goat_shed_partitions` misses empty partitions and fresh-DB catalog
  gaps.
- Rewriting completed proof/completion history destroys audit meaning.
- Feed transport/direction accidentally creates both parent and partition tasks.
- Calendar/workforce/process views lose pens because they only resolve direct
  park children of type `shed`.
- Old generated clients keep sending legacy fields; backend guesses and writes
  the wrong sibling partition.

## Deliverables Before Code Migration

1. Table inventory doc with STG counts and exact SQL output.
2. ADR update superseding the old operational-location convention.
3. Resolver contract and mismatch behavior.
4. Migration SQL dry run on STG clone/local copy.
5. Vaccine proof with sample RFIDs.
6. Weighing proof with sample campaign buckets/RFIDs.
7. Contracts/mobile compatibility matrix.
8. Rollback plan and immutable-history preservation list.
