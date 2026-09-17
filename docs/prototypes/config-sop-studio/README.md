# CODEX prototype — Mesha configuration and SOP studio

A browser-local design extending the existing admin UI. Open **Configuration → Items and settings** or **Work instructions**. Work instructions opens each department’s SOPs; **Stages and approvals** combines smaller SOPs with prerequisites, parallel work, approval and waiting periods.

## Start this prototype

This is **CODEX PR #287**, branch `design/manju-mock-refinement-20260916`. It is separate from the Claude prototype. Use port **4322** for CODEX; do not stop or replace Claude's server on4391.

Requirements: Python3 to serve the files; Node.js to run the judge scripts. No npm installation, build, `.env`, backend or database is needed.

From the repository root:

```sh
sh docs/prototypes/config-sop-studio/start.sh
```

On Ravi's current worktree, the same command from any directory is:

```sh
sh /Users/raviteja/mesha/tmp/manju-mock-refinement/docs/prototypes/config-sop-studio/start.sh
```

Keep that terminal running. Open these URLs in Chrome:

- [Items, categories and Herd Register](http://127.0.0.1:4322/#/configuration/items)
- [Pricing, weight allowances and operational settings](http://127.0.0.1:4322/#/configuration/business-rules)
- [Work instructions and departmental SOPs](http://127.0.0.1:4322/#/configuration/work-instructions)

Click a record row/card to open it. Animal identities opens Herd Register with individual form entry and Excel/CSV upload; templates use the selected animal type. In Business rules, click the default card or add a breed/sex-specific group rule. Choose CEO/CXO in the top-right profile control to edit; Director/Operator are read-only previews.

Stop with **Ctrl+C** in the serving terminal. If4322 is occupied, first open the existing URL and check its content; do not kill an unknown server. To run a separate instance:

```sh
sh docs/prototypes/config-sop-studio/start.sh 4323
```

Use4323 in the URLs for that instance. Changing the port changes the browser storage origin, so it starts with separate local drafts. Existing Chrome tabs need a reload after source changes; use Cmd+Shift+R if an old asset remains cached. A404 generally means the wrong folder is being served; the launcher resolves its own folder automatically. Do not open index.html using file://.

## Validation and saved drafts

From the repository root:

```sh
sh docs/prototypes/config-sop-studio/run-checks.sh
git diff --check
```

`run-checks.sh` discovers all `judge-*.cjs` files. Browser validation is separate: inspect the actual routes in Chrome at desktop/mobile widths, including forms, clickable rows and import previews. The latest work receipt is at repository-root `GOATOS_ACTIVE_PROGRESS.md`; older receipts in this folder are historical.

Drafts are saved in this browser's localStorage key `mesha-studio-v1`, scoped to host and port. Reloading keeps them; another browser or port has independent drafts. For a clean review, use an incognito window or another port. Back up any wanted drafts before clearing this site's browser storage; there is no server backup. Do not clear all browser data.

## Design coverage

- Arbitrary items, category/subcategory organisation, typed settings and sharing across departments, with actual SOP question/action consumers and change-impact checks.
- Browser-local entity registers under **Configuration → Items and settings → Farm and animal registers** only for cross-feature farm and animal records that existing feature screens need to reference: parks, pens, pen partitions, animal species, breeds, lifecycle stages, shed/stage tags, animal groups and animals. Existing Feed, Sales, Procurement, Vaccination and Health CRUD stays in those modules and is linked from the prototype instead of duplicated.
- Existing question/decision/action editor and operator preview; saved child SOPs and their immutable versions; stage prerequisites, approvals, waits and repeated evidence checks.
- Optional Procurement example: seller inspection from the current published form (7 load / 40 animal questions), selection and boarding/arrival subsets, tagging, referenced vaccination plan, holding, travel, parallel shed preparation and warm-up. Animal review uses synthetic data and preserves decision history.
- Business rules for default pricing, animal type/breed/sex overrides, minimum weight and allowed shortfall in grams, with shared Sales/Weighing checks. Existing source valuation and reporting values migrate into the editor; these prototype proposals do not change actual sale records or production enforcement.
- Existing Health assessment/course examples and department-owned settings remain available. Clinical values are not invented by the generic builder.
- Existing production navigation and visual conventions, with plain-language Configuration destinations. Old Workflow links and Run insights URLs now open Work instructions; the rejected standalone event-engine UI is not loaded.

## Evidence and limits

Open `research/index.html` for the anonymous source notes, complete written requirements, feature matrix, frontend/backend/Android inventories and real GCP staging readbacks. `REFINEMENT-PROGRESS.md` records current tests and judge status. Earlier review receipts are historical and do not certify the latest design.

All changes stay in browser localStorage (`mesha-studio-v1`). No database writes, API integration, real operator dispatch, media upload, authentication or deployment is provided. Role switching demonstrates the proposed UI boundary only. Imported inspection and clinical documents retain source meaning; production handoffs and resolver precedence require implementation work identified in the matrix. An inspected table or code path is not claimed to be runtime-certified.
