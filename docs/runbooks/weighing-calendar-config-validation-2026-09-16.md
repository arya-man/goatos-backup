# Weighing calendar configuration validation — September 16, 2026

## Scope and source

Branch `feat/weighing-calendar-db-config` was created from `origin/main` at `d431d1561`. The dirty primary checkout was preserved. This extends the existing published Weighing SOP configuration; it does not introduce a second configuration table or authority.

- Earliest selectable day: July 5, 2026.
- Default landing start: fixed August 3, 2026.
- New optional mode: 1–520 weeks before the current IST day.
- Existing rolling-day behavior, scoped latest-weighing end, explicit selections, and pinned task rules remain intact.

## Completed proof

- 55 focused Node tests passed after the final code edit: weighing date behavior, page/export wiring, SOP editor parsing/emission/validation, and Chromium responsive geometry.
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

Report and calendar scenarios passed in isolated real Chrome at both 1440px and 390px, against populated OCI data. The SOP publication click-through used a 1280px desktop viewport:

| Scenario | Result |
| --- | --- |
| Fixed default | August 3; July 4 disabled; July 5 selectable |
| Manual July 5 selection | Analytics 25 table rows; Weights 45 table rows |
| Export picker | July 4 disabled; July 5 enabled |
| Published six weeks (v3) | August 5 through latest weighing September 8 |
| SOP editor: six → two weeks, Publish SOP (v4) | Actual UI publication succeeded; fresh landing September 2 |
| Restore fixed via normal publish (v5) | August 3 default restored; July 5 selectable |

Neither frontend nor backend restarted during these configuration changes. The first immediate reload after publication can show the cached contract; subsequent reload after the bounded cache expiry showed the new setting. All requested failure strings were absent. Desktop and mobile calendar, report, and export screenshots, plus the desktop SOP editor screenshot, were visually inspected. Local raw evidence is under `artifacts/weighing-calendar-config/` (not committed); `focused-browser-summary.json` records the scenario assertions.

## Review results and fixes

PR [#281](https://github.com/vgoats/goatos/pull/281) received three independent post-PR reviews, including the relevant month of history and current SOP/bootstrap architecture:

| Reviewer scope | Result |
| --- | --- |
| Frontend/date resolver/editor | Approved; no implementation defect; independent 51-test rerun passed |
| Backend/domain/migration/contracts | Approved; no implementation defect; independent targeted Go rerun passed |
| End-to-end product/evidence | Requested missing mobile editor/nested-tab evidence; additional mobile proof found clipped date/year; approved after fix and verification |

The discovered mobile issue is fixed with a feature-scoped media rule: below 600px, the three Weights page configuration fields stack at full width. Other SOP sections and desktop columns are unchanged. A real Chromium geometry regression test checks readable widths and vertical stacking at 390px, and retained columns at 1440px. The complete focused suite now passes 55/55.

The named `responsive:guard` passed **20/20 cases**: General, Breed, Breed wide window, Birth, Pen, Weight, Time, Comparison, SOP list, and Weights, each at laptop and mobile sizes. It used a genuine wrapper launch receipt and matching API/actor identity at `ac3148f37`. The test services were restarted once to add this provenance **after** completing the no-restart configuration round trip; the earlier round trip remains independent evidence. Raw manifest and browser receipt: `.codex-goatos-render/admin-web-screenshots/2026-09-16T09-43-55-787Z/`.

After the final CSS edit, the actual SOP editor was retested at **390px and 1440px**: select rolling weeks, change six to two, then cancel. Screenshots were visually inspected: complete date/year visible on mobile; desktop remains a row. The **publication** test described above was desktop only; mobile proof is edit/cancel and reporting behavior. Published v5 remains fixed August 3 / earliest July 5.

Additional checks: scoped ESLint, `check:mock-fidelity`, and `make ai-doctor` passed. The isolated checkout's missing Repowise index was built using safe static fast mode, then the doctor passed with no bypass. A production build from a clean archive of the tested commit passed, including TypeScript and the token-leak guard with a real test token, without touching the live frontend build directory. The archive omits `.git`, so this is a compile/build receipt, not a main-landing provenance receipt.

No review findings remain open. These are feature-scope approvals, not permission or certification to merge/deploy.

## Metrics and failure record

Calendar minimum changes August 1 → July 5. Fixed default remains August 3. On September 16, six weeks gives August 5 and two weeks gives September 2. No latency improvement is claimed and no extra page API request was added.

Initial local readiness correctly rejected pending migration 318 until the official migration runner applied it. Initial TypeScript failures were dependency path contamination from a different checkout, resolved by isolated package links. Initial proof script used `id` instead of bootstrap's `route_id`; publication had succeeded and was verified by readback without another write.

## Judge and deployment status

All three review scopes approved after the mobile layout and evidence fixes. No push to main, merge, or staging deployment. The feature requires one deployment before production can use the new mode; subsequent SOP publications need no deployment. Initial implementation was tested at `0c818fc57`; the PR follow-up adds the mobile layout fix and geometry guard described above. Exact pushed head is recorded on PR #281 and in the final local progress receipt.
