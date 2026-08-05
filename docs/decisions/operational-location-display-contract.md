# ADR: Operational Location — Storage and Display Contract

Status: Accepted (2026-08-05)

## Context

Shed partitions are a real operational feature: a 200-animal shed is often split
into two working spaces with separate operators, separate vaccination batches,
and separate live-camera proof videos. A `Castro` shed physically contains `Castro
1` and `Castro 2` as distinct work locations.

Goat OS needs to represent this cleanly across two layers: **how it is stored**
and **how it is displayed**.

### The problem

Early implementations modeled raw partition-bearing shed names as separate
buildings in the system:

```
locations row "Castro 1" (id=uuid1)
locations row "Castro 2" (id=uuid2)
```

This multiplies schema work, breaks operator-position ownership (one position
owns an animal's shed_id, not a partition), and makes counts lie. An operator
who vaccinated 100 animals in `Castro 1` and 100 in `Castro 2` would appear to
have worked in two separate sheds, each tracked independently.

The other extreme — storing only `Castro` and pretending both partitions are one
shed — loses operational truth. An operator cannot express "move the goat from
Castro 1 to Castro 2" without a partition field.

## Decision

**Storage layer (canonical, one-time normalization):**

- A physical shed is stored once: shed entity `Castro`, `Godel 1`, etc.
- An animal's physical shed is `goats.shed_id = <shed_uuid>`.
- A sub-location within a shed is `goat_shed_partitions.partition_label = '1'`,
  `'2'`, `'Part 3'`, etc.
- Partition labels follow two conventions:
  - **Numeric**: `'1'`, `'2'`, `'3'` (rare, for simple splits)
  - **Prefixed**: `'Part 1'`, `'Part 3'`, `'Part W'` (common, explicit naming)
- Not every shed has partitions. A non-partitioned shed has no
  `goat_shed_partitions` rows.

**Product/display layer (operational, every surface):**

An animal's ground location is `OperationalLocation = park + physical_shed +
optional partition_label`.

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
3. Group/key by `shed_id` (uuid) + park, never by shed name (names repeat across
   parks).
4. Never collapse partitions into the parent unless explicitly the aggregate.

**Partition catalog must enumerate all existing partitions:**

- A partition with zero live animals still exists (e.g., CBE `Yashoda 5`) and
  must remain a valid shifting destination.
- Derive partition catalogs from `shed_partitions` (migration 000111), not from
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
- CEO reports show `Castro 1` and `Castro 2` as distinct work locations when an
  operator was assigned to partition-specific work.
- Backend ownership is simple: one shed, one HRMS position, clear operator
  accountability. Partitions are a sub-location detail, not a separate entity.
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

## Exception

A genuinely partition-exempt surface carries a COMPLETE inline directive:

```
operational-location:ignore: owner=<name> issue=<url|id> scope=<why> expiry=<YYYY-MM-DD>
```

Example: an internal-only migration repair script may batch-update `goats.shed_id`
without exposing `partition_label` to an API; the script carries the directive.
