# Round 3 final independent judge

**Verdict: PASS for the reviewed local code/model corrections. All round-3 code findings are closed; browser and visual evidence remain separately qualified below.** This is a judgement of the browser-local configuration prototype, not a production certification.

## Closed findings

| Round-3 finding | Independently checked correction |
|---|---|
| Arbitrary Weight/Rate keys could bind built-ins | Newly authored references use `config:<stable item ID>`. Picker values, resolution and compilation preserve that identity. Legacy bare weight/rate references retain intentional migration semantics. Actual-stack test pins custom Weight42 independently of the built-in threshold, edits it to44, and retains published42. |
| Fresh SOP questions lacked price/quantity/percentage units | Applicable typed units are added to the real Unit control; `%` and `percent` normalize consistently. INR/kg and g/head are discoverable without test-only state injection. Unit-list and resolver tests pass; the final onchange wrapper preserves the existing handler and refreshes Compare with immediately. Duplicate binding is guarded and callback tests pass. |
| Typed branch references missing from impact review | Usage discovery now inspects conditions and clauses, stable ID references and published configSnapshot IDs. Actual-stack draft and Published v1 usage assertions pass; revocation blocks new compilation while historical usage/value remain. |
| Scope/effective-period note looked functional | Authoring label now says Scope note (descriptive only), with explicit text that park/cohort/effective-date rules are not applied. This closes the misleading-control finding without inventing a scoped price engine. |

Additional targeted validation rejects blank/whitespace quantity units and invalid relationships between the known weighing minimum/maximum video counts. The rule is local to those configuration keys; it does not add a cross-feature proof cap.

## Evidence

Independent `judge-final-integration.cjs` now exercises actual application/item/source/generic/typed/branch/compiler layers together. Only visual canvas hooks are stubbed. Round-3 checks include custom stable identity versus legacy aliases, new-versus-pinned value/revision, module revoke, typed draft/published dependencies, source unit discovery, percentage equivalence, missing quantity unit and weighing proof pair validation.

`sh run-checks.sh` passed all 18 Node files, exit 0, after the final unit-change refresh correction and navigation scroll correction. Output: `round3-independent-checks.txt`. The implementation agent's narrower tests also cover the same fixes; those do not substitute for this combined-layer run.

## Scope and remaining limits

- Browser/visual proof is separately owned by the parent and visual judge. This recheck does not claim it personally replayed all forms or every desktop/mobile state. Model/compiler tests cannot prove layout or every click path.
- The inherited catalogue→Health/Preventive Care action-picker browser journey from the preceding round remains separately attributed evidence. The current numeric-reference journey is recorded in `round3-browser.md` and attributed below.
- All eleven transcript records and screenshot dependency examples remain accounted for in the round-3 requirements report. Machine-transcription uncertainty is preserved.
- Database integration, real orchestration/SOP dispatch, production analytics and mobile sync remain unimplemented by the local mock. Scope notes are metadata only; sample/reference routes are explicitly limited. The existing live Vaccination error is separate and not fixed here.
- No production writes, push, merge or deployment were performed by this judge.

## Final browser receipt and shell review

The parent supplied `round3-browser.md`: a new arbitrary Rate525 INR/kg record survived save/reload independently of valuation450; the same record was linked to Weighing, selected through the fresh Number question Unit/Compare with controls, and saved in a draft. In the branch checker,525 matched supervisor approval while450 followed the otherwise path. Its detail showed one SOP reference, and an attempted archive displayed the named draft dependency in Review impact; archive was discarded. This was a local in-app-browser journey reported by the parent, not independently replayed by this judge.

Independently reviewed `production-shell.js:36`: explicit route navigation renders the selected route and resets both `#main.scrollTop` and window scroll. The guard around `window.scrollTo` supports the model harness; no configuration or SOP data is changed by this correction. No actionable finding remains in this small change. The final independent all18-file run passed with exit0 after this edit (`round3-independent-checks.txt`).

Independently opened and visually inspected `final-visual/adg-round3-after.png`. It shows the intended local ADG Analytics route, active sidebar entry, header, visible Park/Period/Weighing/Sex/Origin controls, Download, analytics tabs and KPI cards; no login, loading screen, error or modal. This verifies that captured desktop state, not every route or viewport. The parent separately reports reloading the latest mock in Chrome and marking it as the deliverable. Automated pixel-diff coverage is not claimed.
