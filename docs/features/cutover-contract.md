# Legacy-To-Canonical Cutover Contract

Status: draft shared contract for implementation review.

This contract applies to feature specs that temporarily read legacy
BigQuery/Sheets source evidence and later cut over to Goat OS Android/backend
commands as the primary write path.

Current users:

- `docs/features/locations/PRD.md`
- `docs/features/locations/TRD.md`

Old counts and mortality feature specs were deleted from the active tree. Do
not use this contract to revive the old dashboard parity track.

The goal is one stable frontend/API/projection contract while source adapters
change underneath it.

## Source Modes

Every migration-facing projection input must declare its source mode:

```text
legacy_bq
legacy_sheet
goatos_canonical
manual_review
```

`legacy_bq` and `legacy_sheet` are temporary upstream source modes. They can
feed staging/source tables and parity projections, but they must not become
frontend dependencies or final product truth.

Legacy sources often do not carry a Goat OS tenant ID. Backend sync jobs must
bind tenant from the authenticated run target, source registry entry, or
approved project configuration, then write that tenant into source/fact rows.
Do not infer tenant from arbitrary legacy row labels, and do not accept a
multi-tenant source file unless it has an explicit tenant mapping policy.

`goatos_canonical` is the final source mode. It is written by backend commands,
including Android SOP submissions, admin review decisions, imports that have
passed deterministic validation, and module-owned domain commands.

## Blend Mode

Cutover is not a single flip for the whole product. During migration, one
feature, location, module, shed, period, or metric may be canonical while another
is still legacy-backed.

Every projection rebuild must support blend mode:

```text
canonical wins where canonical coverage is complete for that grain
legacy fills gaps where canonical coverage is not complete
unknown or conflicting coverage creates review work
same real-world fact must not be counted twice
```

Coverage must be tracked at the smallest safe grain for the feature. Examples:

- Counts: tenant + snapshot date + view + section + location or metric family.
- Locations: tenant + source context + normalized source label.
- Mortality: tenant + event date or period + metric section + event/source
  class.

Do not use one tenant-wide `legacy`/`canonical` boolean unless the feature truly
cuts over atomically. A tenant-wide flag hides mixed-source states and makes
parity failures hard to explain.

Projection/API freshness must expose blend state through the standard freshness
envelope:

```text
freshness_status = green | yellow | red | unknown
serving_state = never_synced | fresh | stale | rebuilding | failed | source_unavailable
source_composition = legacy_only | canonical_only | blended
```

`source_composition` is explanatory state, not authorization and not metric
truth.

## Coverage Registry

`coverage complete` is an explicit, audited state, not a value inferred only from
canonical row counts.

Each cutover feature must persist coverage state at the feature's minimum safe
grain before projection logic can prefer canonical facts over legacy facts. The
coverage state may live in a shared table or feature-owned table, but it must
record at least:

- tenant_id
- feature/module
- section and metric or source context
- grain key and covered date/window
- source mode being promoted
- coverage status: proposed, shadow_passed, complete, blocked
- canonical source/version and legacy source/version compared
- shadow parity artifact path
- approving actor or automated migration job id
- approved_at, audit id, and rollback/expire policy

Shadow parity with no unexplained deltas is required before a required grain can
move to `complete`. A worker may propose coverage from source metadata, but it
must not flip production coverage solely because canonical counts happen to equal
or exceed legacy counts.

## Composite Metrics

Rates and ratios are composite metrics. Their numerator and denominator must
carry their own source composition and source version.

For production completion, a required composite metric must either:

- use numerator and denominator inputs from the same completed source mode for
  that grain, or
- have an explicitly reviewed `explained_delta`/exception that records why the
  mixed composition is valid for that metric.

If the numerator is `canonical_only` while the denominator is still
`legacy_only`, or vice versa, the metric must surface as `blended` or pending in
internal/dev review. It must not masquerade as a fresh canonical rate.

## Cross-Source Dedup

Source-row idempotency prevents duplicate processing of the same source row. It
does not prevent duplicate counting when legacy and canonical sources describe
the same real-world fact.

Every cutover feature must define a source-independent logical fact key.

Examples:

- Counts count-verification fact:
  tenant + snapshot date + resolved location + metric family + status/stage +
  breed/sex/age bucket where applicable.
- Mortality death fact:
  tenant + event type + event date + goat identity when resolved; or tenant +
  event type + event date + stable source goat identifier when no Goat OS
  identity is resolved.
- Location alias fact:
  tenant + source context + normalized source label.

Unresolved mortality events with no Goat OS identity and no stable source goat
identifier are not safe for hard unique dedup by bucket alone. Use a
`dedup_candidate_key` for review plus a source/run ordinal or source row
disambiguator so two real deaths on the same date/farm/load/breed are not
collapsed into one event. Cross-source dedup for those rows requires review or a
later stable identifier; source-row idempotency still prevents the same source
row from importing twice.

If a canonical fact lands for a logical fact key that was previously represented
by legacy source rows, projection logic must either:

- prefer the canonical fact and retire/ignore the overlapping legacy row for
  serving, or
- open a review item when the two sources disagree.

The system may keep both source rows for audit. The dashboard-serving projection
must not count both as separate business facts.

## Shadow Parity Gate

Legacy parity proves the legacy adapter and projection formula. It does not prove
that canonical Android/backend writes can replace the legacy bridge.

Before BQ/Sheets can be removed for a feature section, run a shadow/dual-source
parity gate over an overlap window:

```text
legacy source snapshot -> projection rows
canonical facts/events -> projection rows
diff value by value
```

The artifact must record:

```text
feature | section | metric | dimension | legacy_value | canonical_value | diff | status | reason
```

Allowed statuses:

```text
match
explained_delta
unexplained_delta
pending_source
```

Any `unexplained_delta` blocks BQ/Sheets removal for that section. A
`pending_source` may appear in internal/dev review UI only as honest
pending/source-unavailable state, never as zero. It blocks production completion
for any required section.

## Locations Alias Rule

Any dashboard or SOP that uses farm, park, shed, housing, holding, quarantine,
ICU, or similar location labels must resolve labels through the Locations module.

The only allowed exception is source discovery code that is cataloging unknown
labels before creating review items.

Feature modules must not keep private location maps. They may cache resolved
location IDs in projection input rows for performance, but Locations owns the
canonical tree, aliases, capacity records, and source-label review workflow.

Legacy `farm` labels are not automatically canonical `location_type = farm`
rows. For the existing Phase 1 data, CBE and CPT legacy farm labels resolve to
the seeded park-scope rows because old-tag scope depends on those rows.

## Canonical SOP Cutover

Slack/App Script/Sheets remain source material and temporary migration inputs.
Final field execution is:

```text
Android operator runner
  -> Goat OS app API
  -> backend validation against pinned SOP version
  -> module-owned canonical command/event
  -> audit/outbox/projections
```

Every Android submission must carry idempotency and enough source evidence to
dedupe against legacy overlap. Server-side validation is authority; mobile
offline validation is only operator UX.

## Removal Gate

BQ/Sheets can be removed for a feature section only when:

- the coverage registry is `complete` for the section's metric grain
- cross-source dedup has tests for the overlap case
- shadow parity has no unexplained deltas
- freshness/source-composition states render honestly
- Locations alias resolution is used for all location-like labels
- frontend and mobile code read Goat OS APIs only

If one section is ready and another is not, remove BQ/Sheets only for the ready
section and keep the not-ready section legacy-backed in development until its
coverage gates pass. A required not-ready section blocks production completion
for the feature.
