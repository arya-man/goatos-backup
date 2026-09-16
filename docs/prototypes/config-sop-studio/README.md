# Mesha configuration and SOP prototype

A browser-local design extending the existing admin UI. Open **Configuration → Items and settings** or **Work instructions**. Work instructions opens each department’s SOPs; **Stages and approvals** combines smaller SOPs with prerequisites, parallel work, approval and waiting periods.

## Run and review

From this directory:

```sh
python3 -m http.server 4320 --bind 127.0.0.1
```

Open http://127.0.0.1:4320/#/configuration/work-instructions in Chrome. Run `sh run-checks.sh` for the focused checks. No package installation is needed.

## Design coverage

- Arbitrary items, category/subcategory organisation, typed settings and sharing across departments, with actual SOP question/action consumers and change-impact checks.
- Browser-local entity registers for feature-owned CRUD: parks, pens, pen partitions, animals, vendors/sellers, buyers, trucks, feed catalogue and stock lots, vaccines and batches, symptoms, diseases, protocols, people/roles, approval policies and SOP templates.
- Existing question/decision/action editor and operator preview; saved child SOPs and their immutable versions; stage prerequisites, approvals, waits and repeated evidence checks.
- Optional Procurement example: seller inspection from the current published form (7 load / 40 animal questions), selection and boarding/arrival subsets, tagging, referenced vaccination plan, holding, travel, parallel shed preparation and warm-up. Animal review uses synthetic data and preserves decision history.
- Proposed Sales group eligibility, minimum weight/tolerance and price settings with a shared Weighing preview. These remain distinct from hardcoded reporting/valuation assumptions and actual sale records.
- Existing Health assessment/course examples and department-owned settings remain available. Clinical values are not invented by the generic builder.
- Existing production navigation and visual conventions, with plain-language Configuration destinations. Old Workflow links and Run insights URLs now open Work instructions; the rejected standalone event-engine UI is not loaded.

## Evidence and limits

Open `research/index.html` for the anonymous source notes, complete written requirements, feature matrix, frontend/backend/Android inventories and real GCP staging readbacks. `REFINEMENT-PROGRESS.md` records current tests and judge status. Earlier review receipts are historical and do not certify the latest design.

All changes stay in browser localStorage (`mesha-studio-v1`). No database writes, API integration, real operator dispatch, media upload, authentication or deployment is provided. Role switching demonstrates the proposed UI boundary only. Imported inspection and clinical documents retain source meaning; production handoffs and resolver precedence require implementation work identified in the matrix. An inspected table or code path is not claimed to be runtime-certified.
