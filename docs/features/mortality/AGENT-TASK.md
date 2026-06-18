# Mortality Agent Task

Status: implementation task guide.

Read first:

- `AGENTS.md`
- `SKILLS.md`
- `context/README.md`
- `.agents/skills/goatos-build/SKILL.md`
- `docs/features/counter-family/AGENT-RUNBOOK.md`
- `docs/features/counter-family/INTEGRATION-CHECKLIST.md`
- `docs/features/mortality/PRD.md`
- `docs/features/mortality/TRD.md`
- `docs/features/locations/PRD.md`
- `docs/features/locations/TRD.md`
- `docs/features/counts/PRD.md`
- `docs/features/counts/TRD.md`
- `docs/features/cutover-contract.md`

## Scope

Own Mortality only:

- live-source discovery for mortality sources
- mortality source rows
- mortality events
- logical-event dedup and unresolved candidate review
- projection rows and projection state
- denominator provenance and mixed-source rate gates
- coverage registry integration
- sync/rebuild worker
- analytics/admin API contract
- admin-web Mortality UI parity
- numeric parity, event-source coverage, and canonical shadow parity
- query-plan/load proof

Do not create private location maps. Use Locations aliases. Do not recompute
Counts/identity denominators inside Mortality request handlers.

## Shared Resource Ownership

- Use only migration numbers `000040`-`000049`.
- Own `contracts/openapi/analytics-api.yaml` paths under
  `/analytics/mortality*`.
- Own `contracts/openapi/admin-api.yaml` paths under `/admin/mortality*` for
  sync, review, event-resolution, and rebuild commands.
- Create Mortality page/component files inside the Mortality route/module area.
- Do not edit shared admin-web shell files directly:
  `apps/admin-web/components/layout/app-sidebar.tsx`,
  `apps/admin-web/components/layout/navbar.tsx`,
  `apps/admin-web/lib/routes.ts`, or
  `apps/admin-web/scripts/smoke-visual-live.mjs`. Provide route label, href,
  icon, and smoke-test path notes to the Locations/coordinator integration
  change.

## Required Live Sources

Probe live sources read-only according to the Mortality TRD source list,
including:

- `mortality_total_dev`
- `mortality_overall_breedwise_dev`
- `mortality_this_month_dev`
- `mortality_this_month_farmwise_dev`
- `monthly_mortality_rate`
- `overall_farmwise_mortality_dev`
- `load_Wise_pct_data`
- `deaths_monthly_trend_v`
- `mortality_genderwise`
- `mortality_trend_dev`
- `deaths_fact_dev`
- `mother_litter_size_dev_breedwise`
- `mother_litter_size_dev_overall`
- `mortality_by_litter_size_overall_dev`
- `last_month_mother_mortality_breed_dev`
- `last_month_mother_mortality_litter_dev`
- `last_month_mortality_by_litter_size_view`
- `load_wise_procurement_with_status`
- `breedwise_load_pct`
- `birth_analysis_view` or `mother_kid_facts` when legacy delivery/litter
  formulas use them

Confirm whether `_dev` views are the currently served legacy oracle or whether a
production replacement exists.

Do not defer discovery for required Load-wise, By Delivery, Trends,
Gender-wise, Status-wise, or Housing/shed-wise sections. If a source for a
required section is blocked or the formula cannot be pinned, record
`source_unavailable` or `pending_source_coverage`; that section blocks
production completion until resolved or explicitly removed from required scope.

## Required Local Proof

- Apply migrations to local Postgres.
- Run live-source dry-run discovery.
- Execute local sync/rebuild against local Postgres.
- Verify every required Mortality tab/section API payload.
- Verify Locations alias resolution for farm/shed/housing/status labels.
- Verify event-source coverage proves required rollup totals or blocks
  completion.
- Verify logical-event dedup prevents legacy + Android double count.
- Verify unresolved same-bucket candidate-only deaths do not collapse.
- Verify rate rows include numerator/denominator source composition and version.
- Verify mixed-source rates are blended or explained, not fresh canonical.
- Run query-plan/load proof for hot reads and rebuilds.
- Run browser QA against local admin-web.

## Required Artifacts

- `.codex-goatos-render/mortality-parity/<timestamp>/comparison-notes.md`
- `.codex-goatos-render/mortality-parity/<timestamp>/canonical-shadow-comparison.md`
- `.codex-goatos-render/mortality/<timestamp>/event-source-coverage.md`
- `.codex-goatos-render/mortality/<timestamp>/source-discovery.md`
- `.codex-goatos-render/mortality/<timestamp>/query-plans.md`
- `.codex-goatos-render/mortality/<timestamp>/browser-review.md`

## Done Criteria

Mortality is ready for integration only when every required item in the Mortality
PRD Required Scope Manifest is implemented, event-source covered, parity checked,
browser checked, and scale checked locally. Required sections with
`pending_source_coverage`, `source_unavailable`, or `unexplained_delta` block
completion.
