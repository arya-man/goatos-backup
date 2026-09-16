# Weighing calendar configuration validation — September 16, 2026

## Scope and source

Branch `feat/weighing-calendar-db-config` was created from `origin/main` at `d431d1561`. The dirty primary checkout was preserved. This extends the existing published Weighing SOP configuration; it does not introduce a second configuration table or authority.

- Earliest selectable day: July 5, 2026.
- Default landing start: fixed August 3, 2026.
- New optional mode: 1–520 weeks before the current IST day.
- Existing rolling-day behavior, scoped latest-weighing end, explicit selections, and pinned task rules remain intact.

## Completed proof

- 51 focused Node tests passed: weighing date behavior, page/export wiring, SOP editor parsing/emission/validation.
- Focused Go tests passed: weighing domain, weighing SOP adapter/application, admin UI compiler and repository packages.
- OCI disposable PostgreSQL migration test passed in 113 seconds. Six fixture scenarios (old seed, custom fixed settings, rolling days, draft, retired version, missing page block) applied migration twice and checked calendar result, unchanged task rules and unchanged version.
- Admin-web typecheck passed. Initial dependency reuse incorrectly resolved an older checkout's API client; isolated dependency links fixed resolution and the rerun passed.
- Backend UI-contract literal guard passed; scoped diff whitespace check passed.
- OCI migration runner applied only migration 318 to the maintained clone. No reset, bulk restore, or seed run occurred.
- Existing OCI v1 predates the entire `weighing` block. Published v2 through the normal SOP API using its original capture form/proof policy plus the seeded weighing rules and requested fixed calendar configuration; readback and bootstrap confirmed July 5 / August 3.
- Real Chrome E2E at 1440px and 390px passed: August 3 default; July 4 disabled; July 5 enabled; selecting July 5 changes the report. The July report has 25 real table rows. No known error strings appeared.
- July calendar screenshots were visually inspected against the user's defect screenshot; the expected dates are enabled and the real report is visible.

OCI test endpoints are frontend port 13306 and backend port 18086, using the existing OCI PostgreSQL tunnel on 15432. These are isolated from the shared default stack. OCI's latest weighing day is September 8, so its default end differs from the user's live screenshot; the date-end algorithm is unchanged.

## Browser configuration round trip

All scenarios passed in isolated real Chrome at both 1440px and 390px, against populated OCI data:

| Scenario | Result |
| --- | --- |
| Fixed default | August 3; July 4 disabled; July 5 selectable |
| Manual July 5 selection | Analytics 25 table rows; Weights 45 table rows |
| Export picker | July 4 disabled; July 5 enabled |
| Published six weeks (v3) | August 5 through latest weighing September 8 |
| SOP editor: six → two weeks, Publish SOP (v4) | Actual UI publication succeeded; fresh landing September 2 |
| Restore fixed via normal publish (v5) | August 3 default restored; July 5 selectable |

Neither frontend nor backend restarted during these configuration changes. The first immediate reload after publication can show the cached contract; subsequent reload after the bounded cache expiry showed the new setting. All requested failure strings were absent. Desktop and mobile calendar, report, export, and editor screenshots were visually inspected. Local raw evidence is under `artifacts/weighing-calendar-config/` (not committed); `focused-browser-summary.json` records the scenario assertions.

## Pending

- PR creation and three independent review results; resolve findings and rerun affected proof.

## Metrics and failure record

Calendar minimum changes August 1 → July 5. Fixed default remains August 3. On September 16, six weeks gives August 5 and two weeks gives September 2. No latency improvement is claimed and no extra page API request was added.

Initial local readiness correctly rejected pending migration 318 until the official migration runner applied it. Initial TypeScript failures were dependency path contamination from a different checkout, resolved by isolated package links. Initial proof script used `id` instead of bootstrap's `route_id`; publication had succeeded and was verified by readback without another write.

## Judge and deployment status

Judges pending after PR creation. No push to main, merge, or staging deployment. The feature requires one deployment before production can use the new mode; subsequent SOP publications need no deployment. Implementation tested at `0c818fc57`; subsequent documentation-only receipts record PR and review results.
