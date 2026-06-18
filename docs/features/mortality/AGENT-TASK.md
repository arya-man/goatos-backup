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

## Required Live Sources

Probe live sources read-only according to the Mortality TRD source list,
including:

- `mortality_total_dev`
- `mortality_overall_breedwise_dev`
- `mortality_this_month_dev`
- `mortality_this_month_farmwise_dev`
- `monthly_mortality_rate`
- `overall_farmwise_mortality_dev`
- `deaths_fact_dev`
- source tables/views used by load, delivery/litter, trend, gender, status, and
  housing sections once formula rows are pinned

Confirm whether `_dev` views are the currently served legacy oracle or whether a
production replacement exists.

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
