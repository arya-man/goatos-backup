# Round 3 code audit

Date: 2026-09-16. Scope: integrated local prototype and the eleven-note voice ledger, production frontend/backend/Android research and read-only staging report. This reviewer had implementation context; this is fresh adversarial code testing, not an independent visual certification. No production writes or deployment.

## Verdict

**Code findings identified in this round are corrected; browser retest remains required.** Generic analytics and production integration remain outside the completed mock scope. Scope/effective-period text is explicitly descriptive, not an executable tenant/park/date rule engine.

## Findings and corrections

| Priority | Concrete failure | Correction and evidence |
|---|---|---|
| P1 | A newly authored configuration with key `weight` and value42 was offered in the numeric branch selector, but the old alias resolver selected reporting35 instead. Arbitrary names must not collide with backward compatibility aliases. | New selector references use `config:<stable item ID>`. Old `weight`/`rate` refs still map to reporting and valuation keys. Compiled conditions retain stable ID/key/revision/value/unit/owner. `question-rules.js:2–9`; focused test creates an arbitrary Weight config42 and proves compilation pins42, while legacy weight still resolves reporting35. |
| P1 | Fresh Number question authoring could not choose INR/kg or g/head; `%` did not match typed `percent`. Programmatic tests had bypassed the UI's unit restriction. | Unit selector is extended through the real inspector wrapper, using module configuration units plus currency/ration/percentage choices. Canonical normalization supports `%`/`percent` and `₹/kg`/`INR/kg`. Focused selector and percentage resolution tests added. Browser must prove fresh question creation through this control. |
| P1 | Numeric branch references were not included in item usage discovery, so archiving/removing module access did not disclose those affected SOPs. | Typed usage discovery now inspects conditions and clauses, legacy references, stable-ID references and pinned snapshots across active, archived and published SOP versions. Existing archive/unlink impact acknowledgement receives these references. `typed-config.js` final `itemUsages` wrapper; direct and actual-stack repro now return the draft numeric comparison. |
| P2 | A quantity20 with empty/whitespace unit passed typed validation. | Quantity now requires a nonblank unit. Explicit zero with g/head remains valid. No generic quantity is silently assigned kg. |
| P2 | Seeded minimum videos could be edited to6 while maximum remained5, creating an impossible configured proof interval. | Validation cross-checks only the two known weighing keys and requires positive counts. Minimum cannot exceed maximum. Other integer/media quantities retain their own semantics; no global proof cap introduced. |

## Actual-stack verification

Loaded all **26 external scripts in index.html order** into one Node VM. DOM was stubbed and shell rendering suppressed during initial composition; all application model/compile wrappers were retained. This is runtime composition testing, not browser/visual proof.

Observed after final corrections:

- Actual `compileWorkflow` on a Sales numeric kg question with legacy `weight` returns `configReferences.reporting_weight_35` containing ID `config-reporting_weight_35`, revision1, value35, unitkg, typeweight, ownerWeighing.
- Actual `itemUsages` on that typed record returns the active Sales draft's condition reference.
- Actual validation of min6/max5 returns `Minimum videos must not exceed maximum videos in this weighing policy.`
- Actual validation of quantity20/unitblank returns `Quantity requires a unit, for example g/head or kg.`
- Scope text such as a future effective date does not alter `configNumber` resolution. This remains intentionally descriptive, and the form now says so; do not claim scoped execution or effective-dating support.

All **18 packaged Node judge suites** passed before the final two narrow validation fixes. Focused `judge-typed-config.cjs` and the actual-stack probe passed again after those fixes; the parent should run the final combined receipt after its concurrent edits stop.

## Requirements cross-check

- One arbitrary category/subcategory/item interface: implemented locally with stable identities.
- Category + subcategory + item + source module relations: union resolver is used by lists, counts, catalogue choices and typed source choices; legacy ownership is retained and Common is not a consuming business module.
- Catalogue item versus typed config: ordinary medicine/resource choices exclude typed configurations. Typed sources feed numeric branches and representative reporting/valuation/feed views through separate APIs.
- Typed prices, numeric values, times, booleans, percentages and enums: represented; zero/null/type/unit tests exist. Existing source-bound unit/type changes are rejected rather than misinterpreted by consumers.
- Sale reporting thresholds, valuation assumptions and recorded transactions: separate meanings; no universal below35kg sale prohibition is claimed.
- Published snapshots: compiled numeric branch values are copied and pinned; revocation blocks new compilation, not playback of existing snapshots.
- Production UI matching, actual arbitrary-price authoring, actual inherited question/action selection, archive confirmation and save/reload: need the separate browser/visual evidence, not this VM receipt.
- Existing production domain config is not migrated or replaced. Current source and live migration state remain separately documented.

## Retest checklist

1. Create an arbitrary currency config; create a fresh Number question through the UI; choose INR/kg and that stable-ID source; compile/run; edit config and prove only a new compile changes.
2. Create a config literally named Weight; confirm its42 value wins when chosen, while a legacy reporting branch still uses its own record.
3. Archive/unlink a used numeric config; confirm the affected SOP is named in review and new validation blocks, while existing published run stays pinned.
4. Confirm blank quantity units and impossible weighing min/max changes visibly refuse saving without losing the draft.

No further unresolved code defect was demonstrated in this bounded audit after those corrections. This is not a claim of exhaustive coverage of every legacy module or of complete production behavior.
