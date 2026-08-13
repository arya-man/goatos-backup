# ADR: Operational Location — Storage and Display Contract

Status: Accepted (2026-08-05), tightened (2026-08-13)

## Context

Exact physical shed is atomic. If farm operations names a shed `Castro 2`,
`Gandhi 1`, or `Godel 1 Part 3`, that full name is the shed where animals
live. A parent/base name such as `Godel 1` can remain as grouping metadata, but
live product behavior must not require a separate partition concept to identify
the animal's residence.

Goat OS needs to represent this cleanly across two layers: **how it is stored**
and **how it is displayed**.

### The problem

Early implementations correctly had raw exact shed rows:

```
locations row "Castro 1" (id=uuid1)
locations row "Castro 2" (id=uuid2)
```

The old compatibility schema confused that real-world model by storing a
parent/group as `goats.shed_id` and a separate `partition_label`. That made
humans and code join two fields to rediscover the physical shed, which is how
half-migrated rows rendered as `Castro 2 2` and `Gandhi 1 1`.

The accepted model stores exact residence in `goats.shed_id` and uses
`goats.shed_group_id` only for the grouped parent header. `partition_label`
survives only as compatibility/history metadata while old rows are cleaned.

## Decision

**Storage layer:**

- `goats.current_location_id` is the exact physical shed residence.
- `goats.shed_id` is also the exact physical shed residence.
- `goats.shed_group_id` is the parent/group shed id when a historical group exists and
  NULL for undivided sheds.
- A compatibility partition mapping may be stored as
  `goat_shed_partitions.partition_label = '1'`, `'2'`, `'Part 3'`, etc. and
  `shed_partitions.operational_location_id` links that legacy group+label to an
  existing exact physical shed row. Live code must not append that label to the
  exact shed name or require it to identify a shed.
- Partition labels follow two conventions:
  - **Numeric**: `'1'`, `'2'`, `'3'` (rare, for simple splits)
  - **Prefixed**: `'Part 1'`, `'Part 3'`, `'Part W'` (common, explicit naming)
- Not every shed has historical partition metadata. A plain shed has no
  `goat_shed_partitions` rows and no partition requirement.

This makes the raw DB obvious: `shed_id` answers "where is the goat physically?"
and `shed_group_id` answers "which parent/group header does that partition
belong under?" Any exact residence filter that widens to `shed_group_id = X` is
a bug.

**Product/display layer (operational, every surface):**

An animal's ground location is the operational location:

- No partition → `"Yashoda"` (bare shed name)
- Numbered shed → `"Castro 2"`
- Part-named shed → `"Godel 1 Part 3"` or `"Mandela 2 Part 1"` exactly as the
  shed row is named

**Never render `"Yashoda whole"` — `whole` is an internal matching sentinel
only, never user-facing copy.** `NULL`, `''`, and `'whole'` all mean
non-partitioned.

Every user-facing surface (counts, herd register, shifting destinations,
vaccination detail, Action Center, CEO reporting, search results) must:

1. Carry the exact shed id/name in the response payload.
2. Render the backend exact shed display instead of rebuilding a name.
3. Group/key by exact residence (`current_location_id` or `shed_id`), never by
   shed name and never by `shed_id + partition_label` for live identity.
4. Never collapse exact sheds into the parent/group unless explicitly the
   aggregate.

**Exact shed catalog is authoritative:**

- A physical shed with zero live animals still exists and must remain selectable
  when its exact `locations` row is active.
- `shed_partitions` is compatibility/remediation metadata. It may link old
  group+label rows to an existing exact shed, but it must not create, rename, or
  retire live `locations` rows.
- `goat_shed_partitions` is a per-goat historical bridge. It is evidence for
  cleanup, not the live location model.

## Consequences

- Operators express shed-to-shed moves directly (`Castro 1 → Castro 2`) through
  exact shed ids.
- Counts no longer lie: "20 animals in Castro 1" reports only that partition's
  animals, not the whole-shed total.
- CEO reports show `Castro 1` and `Castro 2` as distinct real sheds when an
  operator was assigned to partition-specific work.
- The parent/group name can still support rollups through `shed_group_id`, but
  it must not masquerade as exact residence.
- Machine gates reject the old partition-required model on location-bearing surfaces:
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
