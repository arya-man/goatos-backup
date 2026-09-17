# Active progress — PR 287 Config and SOP Studio

## Scope and state — 2026-09-17

User authorized fixing requirements and CEO usability, form and Excel animal entry, agent validation, consolidating changes into PR #287 and reviewing again. Target: `tmp/manju-mock-refinement`, branch `design/manju-mock-refinement-20260916`, port 4322. Port 4391 belongs to Claude and is excluded. No main merge or deployment.

## Done

- Category → subcategory → record workspace; source feed and medicine references retained. Module access remains separate from business categories.
- CRUD, archive/restore and dependency guards for ten animal/farm registers and medicine/feed/equipment/partner catalogues.
- Full-page animal setup and category manager replace rejected long modal forms. Pending edits survive navigation; stale setup commits cannot overwrite newer data.
- Herd Register supports individual form entry and actual XLSX/CSV bulk import. Type-specific templates include configured options and text RFID cells. Preview errors block the entire batch. Inline park/pen creation retains unsaved form values.
- Health navigation, source-based SOP charts, procurement deduplication, accurate weighing flows, and existing vaccination placeholder preserved.

## Proof

- Repo-local `sh docs/prototypes/config-sop-studio/run-checks.sh` executes every judge; all 31 judges passed after final edits (exit 0); git diff --check passed. Receipt: /tmp/pr287-validation/final-checks.log.
- Browser: Chrome laptop SOP variants (Feed packing/transport/distribution, Weighing individual/lump, Procurement imported questionnaire, Health Kids/Adults), vaccination route; all charts opened from cards.
- IAB 390x844: create/edit/archive ten register families, medicines/feed/equipment/vendors/buyers; custom category and subcategory; reload persistence. Desktop full-page setup and category manager visually inspected. Mobile Herd Register, animal form, import and Health chart inspected; responsive chart fix verified after cache-versioned asset reload: bounded selector and 16 source steps.
- Excel browser upload: duplicate two-row XLSX disabled Import, valid one-row XLSX imported, reload retained both RFID strings with leading zeros and Goat/Boer/Female/K1/CBE/Import pen. Chrome verified actual goat-animals.xlsx download (30.7KB).
- Form browser save: RFID-only display fallback worked. Inline new pen creation preserved RFID and saved animal under the new pen.
- Independent agents found and fixed pending-input loss, stale setup overwrite, medicine archive alias guard, duplicate category-manager root, fresh-form missing pen setup, and mobile section selector overflow.
- Exact failures guarded: `backend_down`, `Admin-web contract unavailable`, `The board could not be loaded`, `Weights could not be loaded`; static prototype validation does not certify real backend/sync availability.

## Metrics / limitations

No performance claim or backend modification. Prototype saves in browser local storage. All source-derived catalogues/graphs are prototype fixtures; production API persistence/sync is not implemented or certified by this PR.

## Pending

Pushed functional commit `90b20b97a0e4e20611873d65ad6c017e2bf450c8` to PR287; GitHub head verified. Fresh CRUD review clean. Fresh Excel review found asynchronous file A/B preview race; corrected with generation guards on selection/type/open/cancel/commit and immediate old-preview removal. Out-of-order completion regression and all31 judges pass; correction ready to push. Deployment: none; main untouched. Final local receipt: `/tmp/pr287-validation/FINAL-RECEIPT.md`.

## Clickable row correction — 2026-09-17

User reiterated whole list/card activation without separate Edit buttons. Removed redundant actions from common record lists and Herd Register; rows support pointer, Enter and Space with visible focus. Removed obsolete action-column sizing and mobile hiding. Completed broader UI pass: grouped Animals/Farm locations/Catalogues navigation; mobile category picker; contextual Add labels; compact 49px desktop records; single row activation; focused entity dialogs; category editor actions inside opened row; bounded animal form with sticky Save; themed fields. Browser verified 1440x900 and390x844: pointer activation from status cell, Enter/Space partner activation, Herd row, category row, mobile category rows57px, animal Save bottom821px within844px viewport. Dark/light list screenshots inspected; no horizontal page overflow on mobile. All31 judge scripts and git diff --check pass after final edits; executable row guards cover pointer/Enter/Space and no nested edit buttons. Final PR push pending. No main merge/deploy.
