# Locations Agent Task

Status: implementation task guide.

Read first:

- `AGENTS.md`
- `SKILLS.md`
- `context/README.md`
- `.agents/skills/goatos-build/SKILL.md`
- `docs/features/locations/PRD.md`
- `docs/features/locations/TRD.md`
- `docs/features/cutover-contract.md`
- `docs/decisions/high-scale-dashboard-projections.md`

## Scope

Own Locations only:

- canonical location CRUD/list/detail/search
- tree, parent, breadcrumb, children
- aliases and alias conflicts
- seeded CBE/CPT/HF protections
- seeded BQ shed alias protections
- capacity records and effective dates
- usage checks before risky edit/retire/delete
- review queue for unknown/conflicting source labels
- projection invalidation hooks for dependent dashboards
- admin-web Locations tab

Do not implement Counts or Mortality projection logic except the minimal shared
interfaces needed for location invalidation and alias resolution.

## Shared Resource Ownership

- Use only migration numbers `000024`-`000029`.
- Own `contracts/openapi/admin-api.yaml` paths under `/admin/locations*`,
  including location aliases, capacity, usage checks, and review operations.
- Own shared admin-web route-shell registration for the counter-family slice:
  `apps/admin-web/components/layout/app-sidebar.tsx`,
  `apps/admin-web/components/layout/navbar.tsx`,
  `apps/admin-web/lib/routes.ts`, and
  `apps/admin-web/scripts/smoke-visual-live.mjs`.
- Add or update only route-shell entries needed to expose Locations and to land
  pre-agreed Counts/Mortality route placeholders or route metadata. Counts and
  Mortality agents should not edit those shared shell files directly.

## Required Local Proof

- Apply migrations to local Postgres.
- Seed or verify existing CBE/CPT/HF and BQ shed alias rows.
- Run CRUD/alias/capacity/review/usage tests.
- Verify protected seeded rows cannot be edited/remapped without an approved
  migration-plan artifact.
- Verify unknown legacy labels create review items.
- Verify Counts and Mortality can resolve labels through the same alias service.
- Run query-plan proof for list/search/tree/usage endpoints.
- Run local browser QA for the Locations tab.

## Live Source Discovery

Use live sources read-only:

- Counts detail farm/shed labels
- BQ dashboard shed labels
- capacity sources
- Mortality farm/shed/housing/status labels needed by Mortality

Record source names, watermarks, counts, and blocked-source errors. Do not
commit raw rows.

## Required Artifacts

- `.codex-goatos-render/locations/<timestamp>/source-discovery.md`
- `.codex-goatos-render/locations/<timestamp>/local-db-checks.md`
- `.codex-goatos-render/locations/<timestamp>/query-plans.md`
- `.codex-goatos-render/locations/<timestamp>/browser-review.md`
- label/capacity parity notes for Counts/Mortality dependencies

## Done Criteria

Locations is ready for integration only when every required item in the
Locations PRD Required Scope Manifest is implemented, tested locally, browser
checked, and no required label/capacity source remains blocked without a product
de-scope decision.
