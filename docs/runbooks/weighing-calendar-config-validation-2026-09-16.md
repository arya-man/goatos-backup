# Weighing calendar DB configuration validation — September 16, 2026

## Scope

PR [#281](https://github.com/vgoats/goatos/pull/281), branch `feat/weighing-calendar-db-config`, started from `origin/main` at `d431d1561`. The primary checkout remains untouched.

The database owns the earliest selectable day and landing start mode/value. Backend bootstrap reads the tenant row from `public.weighing_calendar_config`; the existing frontend calendar consumes that contract. Initial values are July 5, 2026 minimum and fixed August 3, 2026 default. Optional rolling weeks count back from the current IST day: September 16 minus six weeks is August 5.

There are no UI layout, styling, editor, OpenAPI, or generated-client changes relative to the base branch. Earlier SOP configuration work was superseded by the explicit DB-only requirement. The final configuration authority is independent of SOP publication. Exact tenant-scoped SQL and verification commands are in [the settings runbook](weighing-calendar-settings.md).

## Verification in progress

- Migration 319 applied through the official migration runner to the maintained OCI clone, without reset or bulk seed.
- Integration testing caught a missing tenant query argument; fixed before accepting configuration proof. The earlier round trip was discarded and is being rerun against the corrected backend.
- Focused backend tests, independent frontend review, direct-SQL fixed/six-week/two-week/fixed round trip, and browser comparison across every analytics tab are being completed.
- July 5–September 16 API probes returned HTTP 200 for all eleven tab-related requests with populated relevant sections. Default male scope: 516 animals, 28 weighed pens, 15,663.4 kg total, approximately 160.209 g/day ADG. Browser comparisons remain pending.
- Comparison intentionally uses its existing all-time procurement/latest-weight basis; it is not the selected-period cohort. Its response contains eight loads and 735 animals across 41 weighed pens. This behavior is unchanged by the configuration work.

## Environment and evidence

Isolated frontend: port 13306. Isolated backend: port 18086. Existing OCI PostgreSQL tunnel: port 15432. Latest weighing data in this clone is September 8, 2026; explicit report selection can still end September 16. No business weighing records are mutated for testing.

Local raw proof lives under `artifacts/weighing-calendar-config/` and is excluded from the PR. The progress record there tracks tested revisions and outstanding checks. Screenshots are accepted only after visual inspection.

## Promotion status

No merge, main push, or deployment. One initial migration/code deployment is needed to introduce the DB configuration; later configuration updates use SQL without code deployment. No latency improvement is claimed.
