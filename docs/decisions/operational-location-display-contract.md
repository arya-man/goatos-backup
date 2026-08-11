# ADR: Operational Location — Storage and Display Contract

Status: Accepted (2026-08-05), tightened (2026-08-11)

## Context

Shed partitions are a real operational feature: when a named shed is split,
each partition is itself the real shed where animals live. `Godel 1` can be a
group/header name; `Godel 1 - Part 1`, `Godel 1 - Part 2`, and
`Godel 1 - Part 3` are the real residences.

Goat OS needs to represent this cleanly across two layers: **how it is stored**
and **how it is displayed**.

### The problem

Early implementations modeled raw partition-bearing shed names as separate
buildings in the system:

```
locations row "Castro 1" (id=uuid1)
locations row "Castro 2" (id=uuid2)
```

The old schema confused that real-world model by storing a parent/group as
`goats.shed_id`. That made humans and code read the group as the goat's
physical residence. The accepted model stores exact residence in
`goats.shed_id` and uses `goats.shed_group_id` only for the grouped parent
header.

The other extreme — storing only `Castro` and pretending both partitions are one
shed — loses operational truth. An operator cannot express "move the goat from
Castro 1 to Castro 2" without a partition field.

## Decision

**Storage layer:**

- `goats.current_location_id` is the exact real residence. For a partitioned
  animal it points at the partition operational location; for an undivided shed
  it points at the shed itself.
- `goats.shed_id` is also the exact real shed/partition residence. For a
  partitioned animal this is the partition shed id; for an undivided shed this is
  the shed id.
- `goats.shed_group_id` is the parent/group shed id when partitions exist and
  NULL for undivided sheds.
- A compatibility partition mapping is stored as
  `goat_shed_partitions.partition_label = '1'`, `'2'`, `'Part 3'`, etc. and
  `shed_partitions.operational_location_id` links that label to the real
  operational location.
- Partition labels follow two conventions:
  - **Numeric**: `'1'`, `'2'`, `'3'` (rare, for simple splits)
  - **Prefixed**: `'Part 1'`, `'Part 3'`, `'Part W'` (common, explicit naming)
- Not every shed has partitions. A non-partitioned shed has no
  `goat_shed_partitions` rows.

This makes the raw DB obvious: `shed_id` answers "where is the goat physically?"
and `shed_group_id` answers "which parent/group header does that partition
belong under?" Any exact residence filter that widens to `shed_group_id = X` is
a bug.

**Product/display layer (operational, every surface):**

An animal's ground location is the operational location:

- No partition → `"Yashoda"` (bare shed name)
- Numeric partition → `"Castro 2"`
- Prefixed partition → `"Godel 1 - Part 3"`

**Never render `"Yashoda whole"` — `whole` is an internal matching sentinel
only, never user-facing copy.** `NULL`, `''`, and `'whole'` all mean
non-partitioned.

Every user-facing surface (counts, herd register, shifting destinations,
vaccination detail, Action Center, CEO reporting, search results) must:

1. Carry `partition_label` in the response payload when one exists.
2. Render `OperationalLocation.Display()` instead of bare shed names.
3. Group/key by a partition-aware identity (`current_location_id` or `shed_id`
   for exact residence; `shed_group_id + partition_label` only where the group
   bridge is intentionally being read), never by shed name.
4. Never collapse partitions into the parent unless explicitly the aggregate.

**Partition catalog must enumerate all existing partitions:**

- A partition with zero live animals still exists (e.g., CBE `Yashoda 5`) and
  must remain a valid shifting destination.
- Derive partition catalogs from `shed_partitions` (migration 000112), not from
  `goat_shed_partitions` (a per-goat relation that hides empty partitions).
- `locations` rows named `Castro 1` with `status='inactive'` are NOT dead:
  `weighing_campaign_sheds` references them. Do not use one as a goat's
  `shed_id`; do use them when enumerating partitions until the real catalog
  replaces them.

## Consequences

- Operators can now express partition-to-partition moves (`Castro 1 → Castro 2`)
  through the shifting contract.
- Counts no longer lie: "20 animals in Castro 1" reports only that partition's
  animals, not the whole-shed total.
- CEO reports show `Castro 1` and `Castro 2` as distinct real sheds when an
  operator was assigned to partition-specific work.
- The parent/group name can still support rollups through `shed_group_id`, but
  it must not masquerade as exact residence.
- Machine gates enforce partition presence on all location-bearing surfaces:
  `make operational-location-guard`, part of `make guardrails` and `make
  ci-local`. Shared primitives: Go
  `backend/internal/platform/oploc`, admin-web
  `apps/admin-web/lib/operational-location.ts`, Android
  `core/core-ui/.../PartitionLabel.kt`.

## Incident evidence

Two defects discovered pre-migration:

1. **Counts Breakdown dropdown** showed only parent sheds, not partitions. An
   operator could not select "Castro 1" as a census movement destination; the
   dropdown listed only "Castro".
2. **Android Record Shifting** read a chip saying `"Coimbatore · Yashoda"` with
   no partition selector visible, making `Yashoda 1 → Yashoda 2` unexpressible
   in the mobile UI.

Both were caused by queries collapsing partitions at read time. This ADR closes
that gap.

### Post-migration recurrence (2026-08-06) — the display half

The read-time collapse was fixed; the DISPLAY half then failed in two new ways,
found while exercising the shifting destination picker against real STG data.
Both are now machine-blocked (`oploc-display-wire-name`,
`backend-owned-oploc-label` in `check-operational-location.mjs`).

1. **The label shipped under two different wire names.**
   `/app/counts/shifting/destinations` emitted `json:"display"`, while
   OpenAPI's `ShiftingDestinationShed` and Android's `CountsDestinationShedDto`
   both declared `operational_location_display`. Nothing errored: kotlinx gives
   an absent key its default, so the field deserialized to `""`. **A contract
   break between a Go json tag and a client `@SerialName` is SILENT — there is no
   type error, no 4xx, just an empty string on a screen.** The canonical wire name
   is `operational_location_display` on every surface; `display` is banned for
   this concept.

2. **Clients re-derived the label instead of rendering it.** `ShiftingScreen`
   called `operationalLocationLabel(name, partitionLabel)` locally rather than
   using the shipped string — which is why defect 1 went unnoticed for so long:
   *the second bug masked the first.* Because the client composed its own label,
   the empty backend field was never read. Worse, `AddBirthScreen` and
   `BirthDeathScreen` rendered a bare `.name`, so a shed with 10 partitions
   appeared as 10 identical `"Godel 1"` rows keyed by the same `shedId`, and
   `firstOrNull { it.shedId == … }` silently resolved every one of them to
   partition 1.

**Two lessons worth carrying forward.** First, duplicated display logic hides
contract drift: the rule lives in three languages (`oploc.Display()`,
`PartitionLabel.kt`, `operational-location.ts`), and as long as each client
composes its own string, the backend's field can be wrong indefinitely without
anyone noticing. Backend composes, clients render — that is what makes the
contract observable. Second, when scoping a guard for this class, do NOT require
a nearby `partition_label` mention as the trigger: `AddBirthScreen.kt` names
partitions **zero** times, and that total absence is precisely the defect. A
guard gated on a partition mention can only ever catch code that already
half-remembered the rule.

## Exception

A genuinely partition-exempt surface carries a COMPLETE inline directive:

```
operational-location:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>
```

Example: an internal-only migration repair script may batch-update `goats.shed_id`
without exposing `partition_label` to an API; the script carries the directive.
