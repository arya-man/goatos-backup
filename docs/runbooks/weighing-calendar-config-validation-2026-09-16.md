# Weighing calendar DB configuration validation — September 16, 2026

## Scope

PR [#281](https://github.com/vgoats/goatos/pull/281), branch `feat/weighing-calendar-db-config`, started from `origin/main` at `d431d1561`. The primary checkout remains untouched.

The database owns the earliest selectable day and landing start mode/value. Backend bootstrap reads the tenant row from `public.weighing_calendar_config`; the existing frontend calendar consumes that contract. Initial values are July 5, 2026 minimum and fixed August 3, 2026 default. Optional rolling weeks count back from the current IST day: September 16 minus six weeks is August 5.

There are no UI layout, styling, editor, OpenAPI, or generated-client changes relative to the base branch. Earlier SOP configuration work was superseded by the explicit DB-only requirement. The final configuration authority is independent of SOP publication. Exact tenant-scoped SQL and verification commands are in [the settings runbook](weighing-calendar-settings.md).

## Completed verification

- Migration 319 applied through the official migration runner to the maintained OCI clone, without reset or bulk seed. Migration 318 is a reserved no-op to preserve numbering; it changes no SOP data.
- Focused Go tests and the final OCI database integration passed (140.723 seconds), including DB authority, SOP independence, revision invalidation, row metadata and invalid configurations/date bounds. The runbook readback SQL was executed against OCI.
- Integration testing caught a missing tenant query argument; fixed before accepting configuration proof. The earlier round trip was discarded. The successful repeat used the corrected backend.
- Production frontend build passed in an isolated copy of the final source; the live frontend build was untouched. Token-leak scan skipped because the build had no bearer token; this is a build check, not a clean-SHA landing receipt.
- Final calendar checks passed at both sizes: July 4 disabled, July 5 selectable, fixed August 3 default. Screenshots were visually inspected.
- 50 focused frontend tests, TypeScript, scoped ESLint, mock-fidelity, weighing-SOP, backend-foundations, duplicate-migration-version, seed-migration and whitespace checks passed.
- Direct SQL changed fixed August 3 → six weeks (August 5) → two weeks (September 2) → fixed August 3. Both 1440px and 390px browsers reflected each setting with the same running frontend/backend processes. The bounded contract cache was allowed to expire. SOP JSON hash remained unchanged. Final DB row is July 5 minimum / fixed August 3.
- All seven analytics tabs passed at 1440px and 390px: **14 browser cases, 206 comparisons** of displayed numeric/label values with the corresponding OCI API responses. The period was explicitly July 5–September 16, 2026, using default male scope.

| Tab | Values compared per viewport | Result |
| --- | ---: | --- |
| General | 8 | Pass |
| Breed-wise | 22 | Pass |
| Birth-wise | 24 | Pass |
| Pen-wise | 24 | Pass |
| Weight-wise | 12 | Pass |
| Time-wise | 9 | Pass |
| Comparison | 4 representative values | Pass against existing all-time basis |

The selected-period responses show **516 animals**, **28 weighed pens**, **15,663.4 kg** total, **30.355 kg** average and **160.209 g/day** ADG. Displayed rounded totals matched. The eleven relevant API requests all returned HTTP 200 with populated relevant sections. Browser checks reject `backend_down`, `Admin-web contract unavailable`, `The board could not be loaded`, and `Weights could not be loaded`.

**Scope of accuracy proof:** this compares rendered totals/chart values to real API responses, not an independent recalculation of every source observation. Comparison intentionally retains its existing all-time procurement/latest-weight basis, ignoring the period and sex filter; its response contains eight loads and 735 animals across 41 weighed pens. The configuration change does not alter that behavior.

## Review and known validation limits

Independent frontend review approved the DB reader and verified all six UI files exactly match `origin/main`. Backend review applied the relevant month of history and preserved the existing hoisted-SQL convention; the tenant-binding defect was fixed and invalid date bounds are constrained in PostgreSQL. Browser review completed the configuration round trip and all-tab comparisons.

The global `make validate-migrations` remains blocked by pre-existing historical migration policy violations and numbering gaps on `origin/main` (including missing 000003). The new migrations add no numbering gap. This is not a full repository CI or landing receipt. No performance improvement is claimed; individual API probes were approximately 561–1176 ms in this local OCI setup.

## Environment and evidence

Isolated frontend: port 13306. Isolated backend: port 18086. Existing OCI PostgreSQL tunnel: port 15432. Latest weighing data in this clone is September 8, 2026; explicit report selection can still end September 16. No business weighing records are mutated for testing.

Local raw proof lives under `artifacts/weighing-calendar-config/` and is excluded from the PR. The progress record there tracks tested revisions and outstanding checks. Screenshots are accepted only after visual inspection.

## Promotion status

No merge, main push, or deployment. One initial migration/code deployment is needed to introduce the DB configuration; later configuration updates use SQL without code deployment. No latency improvement is claimed.
