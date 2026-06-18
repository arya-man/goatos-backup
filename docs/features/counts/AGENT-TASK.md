# Counts Agent Task

Status: implementation task guide.

Read first:

- `AGENTS.md`
- `SKILLS.md`
- `context/README.md`
- `.agents/skills/goatos-build/SKILL.md`
- `docs/features/counter-family/AGENT-RUNBOOK.md`
- `docs/features/counter-family/INTEGRATION-CHECKLIST.md`
- `docs/features/counts/PRD.md`
- `docs/features/counts/TRD.md`
- `docs/features/locations/PRD.md`
- `docs/features/locations/TRD.md`
- `docs/features/cutover-contract.md`

## Scope

Own Counts only:

- live-source discovery for required Counts sources
- source rows and normalized snapshot rows
- projection rows and projection state
- coverage registry integration
- cross-source logical fact keys
- sync/rebuild worker
- analytics API contract
- admin-web Counts UI parity
- numeric parity and canonical shadow parity
- query-plan/load proof

Do not create private location maps. Use Locations aliases. Do not implement
Mortality rates or events.

## Required Live Sources

Probe live sources read-only:

- `ceo_dashboard.counting_db_with_holding_dev`
- `farm.daily_summary_dev`
- `ceo_dashboard.counting_kpis_daily`
- `ceo_dashboard.core_farm_genderwise`
- `ceo_dashboard.counting_shed_capacity_status_dev`
- `ceo_dashboard.shed_capacity_count_dev`

Feed/Vaccination sources are supporting compatibility evidence unless their
feature slice is explicitly assigned.

## Required Local Proof

- Apply migrations to local Postgres.
- Run live-source dry-run discovery.
- Execute local sync/rebuild against local Postgres.
- Verify API payloads for all required tabs and sections.
- Verify `summary_source_date` and yesterday-IST summary behavior.
- Verify Locations alias resolution for farm/shed/housing labels.
- Verify source unavailable does not write fake zeros.
- Verify projection publish is atomic and idempotent.
- Run query-plan/load proof for hot reads and rebuilds.
- Run browser QA against local admin-web.

## Required Artifacts

- `.codex-goatos-render/counts-parity/<timestamp>/comparison-notes.md`
- `.codex-goatos-render/counts-parity/<timestamp>/canonical-shadow-comparison.md`
- `.codex-goatos-render/counts/<timestamp>/source-discovery.md`
- `.codex-goatos-render/counts/<timestamp>/query-plans.md`
- `.codex-goatos-render/counts/<timestamp>/browser-review.md`

## Done Criteria

Counts is ready for integration only when every required item in the Counts PRD
Required Scope Manifest is implemented, parity checked, browser checked, and
scale checked locally. Required sections with `pending_source` or
`unexplained_delta` block completion.
