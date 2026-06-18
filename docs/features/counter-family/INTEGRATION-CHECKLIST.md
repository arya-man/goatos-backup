# Counter-Family Integration Checklist

Status: coordinator checklist for Locations, Counts, and Mortality integration.

Run this after the feature agents finish local implementation and before any
dev deploy.

## Coordinator Preflight

- Confirm target repo, branch, and dirty files before agent fan-out.
- Confirm the current highest committed migration number and write down the
  reserved non-overlapping migration ranges.
- Confirm read-only BQ/Sheets/Drive credentials work for all required sources.
- Confirm which required sources are Drive-backed or blocked before fan-out.
- Confirm discovery targets belong to approved Goat OS legacy/migration sources,
  not Heva/Slice projects, organizations, service accounts, or browser sessions.

## Repo And Scope

- Confirm the target repo is `https://github.com/vgoats/goatos.git`.
- Confirm the branch/worktree contains only intended Goat OS changes.
- Confirm no live legacy repo was modified.
- Confirm no raw BQ/Sheets/Drive exports or credentials are committed.
- Confirm each feature PRD/TRD still matches implementation behavior.

## Merge Order

1. Locations
2. Counts
3. Mortality

Locations must land first because Counts and Mortality depend on aliases and
seeded scope protections. Counts must land before Mortality rate completion
because Mortality depends on denominator projections.

## Shared Contracts

- OpenAPI contracts compile and generated clients are updated.
- Migration files stay inside the reserved feature ranges:
  Locations `000024`-`000029`, Counts `000030`-`000039`, Mortality
  `000040`-`000049`.
- There are no duplicate migration prefixes and no feature uses another
  feature's reserved range.
- OpenAPI path ownership is respected: Locations `/admin/locations*`, Counts
  `/analytics/counts*` and optional `/admin/counts*`, Mortality
  `/analytics/mortality*` and `/admin/mortality*`.
- Shared admin-web route-shell files are edited only by the Locations/coordinator
  integration change: `components/layout/app-sidebar.tsx`,
  `components/layout/navbar.tsx`, `lib/routes.ts`, and
  `scripts/smoke-visual-live.mjs`.
- Migrations apply cleanly to a fresh local Postgres database.
- Migration ordering does not create circular feature dependencies.
- Permission namespaces follow `analytics.<feature>.*` for dashboards and
  `locations.*` for Locations master data.
- Coverage registry semantics match `docs/features/cutover-contract.md`.
- Freshness uses `freshness_status = green|yellow|red|unknown` plus
  `serving_state`.

## Live Source Proof

For each feature, verify:

- live BQ/Sheets/Drive discovery was used, not stale local dumps
- required sources have schema evidence and watermarks
- row/source counts are recorded
- unavailable sources are explicit and do not write zeros
- required blocked sources have a data/source owner and product de-scope
  decision if completion is claimed

## Local DB Proof

- Local migrations applied.
- Local sync dry-run succeeds.
- Local sync execute succeeds only after dry-run review.
- Projection rebuild publishes atomically.
- Re-running sync is idempotent.
- Review/conflict rows are created for ambiguous inputs.
- Projection state carries freshness, serving state, source composition,
  unavailable sources, conflict count, and projection version.

## Scale Proof

- Hot API reads use projection tables only.
- Hot reads are tenant scoped and section/date/view scoped.
- `EXPLAIN`/`EXPLAIN ANALYZE` artifacts show indexed bounded access.
- Large rebuilds are chunked and retry-safe.
- No request path performs full-herd scans or raw all-history aggregation.
- Synthetic or plan-based proof covers one-million-goat shape.

## Browser QA

- Admin-web runs locally against local backend.
- Locations page works for table/tree/detail/alias/capacity/usage/review flows.
- Counts page renders all required legacy tabs/sections.
- Mortality page renders all required legacy tabs/sections.
- Desktop and narrow screenshots are reviewed.
- Legacy dashboard comparison notes exist where applicable.
- UI shows stale/source-unavailable/rebuilding/never-synced/conflict states.
- No frontend BQ/Sheets/Drive/raw DB calls exist.

## Parity Artifacts

- Counts numeric parity artifact exists.
- Counts canonical shadow parity artifact exists for any BQ/Sheets removal.
- Counts shadow parity handles `daily_summary_dev` yesterday-IST behavior.
- Mortality numeric parity artifact exists.
- Mortality canonical shadow parity artifact exists for any BQ/Sheets removal.
- Mortality event-source coverage artifact exists.
- Locations label/capacity parity notes exist for Counts/Mortality dependencies.
- Every delta is `match`, `explained_delta`, `unexplained_delta`, or
  `pending_source`; any `unexplained_delta` blocks completion.

## Dev Deploy Gate

Dev deploy is allowed only after:

- all local DB checks pass
- all browser QA checks pass
- all required parity artifacts exist
- query-plan/load proof exists
- required live sources are available or product explicitly de-scoped the
  affected required scope
- coordinator signs off on merge order and shared contracts

Do not push/deploy to dev from individual feature agents.
