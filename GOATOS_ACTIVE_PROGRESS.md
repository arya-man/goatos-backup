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

## Business-rule configuration completion — 2026-09-17

Scope: expose omitted pricing, animal type/breed/sex overrides, minimum weight and allowed shortfall, and existing operational settings in PR287. Configuration → Business rules, Sales → Business rules, Weighing → Weight rules are discoverable routes. Default card and override/setting rows open directly; no redundant Edit buttons. Rules are local prototype proposals, not live transaction enforcement.

Done: shared validated rule model; ₹/kg and per-animal override basis; numeric inheritance; create/read/update/archive/restore; 0–1,000g shortfall; ambiguous-overlap rejection; stable registry references and archive/reparent guards; canonical legacy editor synchronization; migration preserves authored global values and legacy disabled groups/sharing; bounded repair screen preserves invalid legacy data. Sales Farm value and Weighing ADG retain existing sample analytics and add the same saved-rule check. Other settings expose existing source-owned values and keep derived references read-only.

Review findings fixed: default seed overriding authored40kg/₹600 values; editing archived override restoring it implicitly; legacy editor scalar drift; ignored Weighing-sharing preference; referenced entity archive/reparent breaking rules. Independent model/integration reviewers verified corrections. Relevant past month prototype history and current backend reporting/valuation semantics inspected. No performance claim: ₹450/kg remains an indicative fattening valuation assumption; 35kg is a reporting threshold and 500g is editable allowed shortfall, not measurement error or a production default.

Final local proof: `sh docs/prototypes/config-sop-studio/run-checks.sh` exit0 after final source edits, all33 judge scripts; `git diff --check` exit0. Log `/tmp/pr287-business-checks-final.log`. New model and actual UI harness judges cover conversions/boundaries, units, inheritance/specificity, invalid/ambiguous rules, reference guards, migration/source preservation, disabled sharing, read-only mutations, archive/edit/restore, canonical sync and route integration.

Browser proof: IAB real4322 routes, CEO saved450/kg +35kg +500g +>=;34.5 passes (₹15,525),34.49 fails; Goat/Boer/Male500/kg override yields₹17,250 at34.5kg; reload persistence; Enter opens override; archive/restore; Weighing sharing off/on; Sales Farm value and Weighing ADG both read identical saved threshold; lower weight40 rejected against upper35; Director disabled inputs/no Save. Desktop1280x720 and mobile390x844 visually inspected; mobile dialog Save visible; no page-width overflow. Browser console error list empty. Exact failure strings absent: backend_down, Admin-web contract unavailable, The board could not be loaded, Weights could not be loaded. This static-browser proof does not certify production backend/sync.

Evidence: `/Users/raviteja/mesha/tmp/pr287-business-rules-evidence/desktop.png` and `mobile-rule.png`, both opened/visually inspected before reporting. Restored missing task worktree and restarted only task server4322; Claude4391 untouched.

SHA before this commit: a2627c39418ac42b44bb046fb2ba52d3e1c2283b. Judge status green. Pending: commit/push this change to existing PR287 and verify remote SHA. Deployment: none; no main merge/push. Production API persistence remains outside this prototype change.
