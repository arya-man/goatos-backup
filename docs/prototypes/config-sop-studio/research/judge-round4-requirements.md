# Round 4 independent requirements challenge

Verdict: **refinement required**. Two new reproducible defects remain in the integrated local SOP authoring/testing journey despite all18 packaged Node suites passing. This is a browser-local prototype review, not production certification. Implementation was not edited by this judge.

## Evidence and requirement context

Re-read all eleven anonymous voice entries and screenshot requirements in `voice-requirements.md`, prior round3 findings and corrections, and frontend/backend/Android architecture and real staging research. The architecture evidence remains pinned to source397114d1d06baddb50dffc7d2c2f9df1d0497b7b and the separately recorded live migration317; neither is refreshed deployment proof.

V02/V03/V06–V11 require one arbitrary configuration interface and one-to-many module availability, with V10 extending applicability to category/subcategory/item. V04/V08 require the real SOP authoring consumer, not only registry presentation. V10 prices motivate typed numeric values and compatible units. Thus conflicting authoring paths and a tester that disagrees with execution directly undermine the requested foundation. Screenshot transit dependencies remain separate from sharing: same-context upstream/activation gates and required preparation completion. No new defect was established in that bounded simulator during this pass.

## P1 — Direct condition inspector silently ignores edits to a shared comparison

**Code:** `app.js:26–27` exposes Decision value/source/comparison through `editNode`; `rule-options.js:6–9` exposes additional clause inputs through `ruleClause`. These paths retain an existing `valueRef`. The separate question-owned editor clears or updates references via `question-rules.js:13`, but the original condition editor remains reachable by selecting the condition node. Compilation at `question-rules.js:9,26` then replaces the apparently edited value with the referenced configuration value.

**Independent actual-stack reproduction:** reuse the combined application/items/sources/generic/typed/question-rule harness; select a Number condition bound to `config:config-reporting_weight_35`; call the direct inspector's `editNode('value','99')`. Readback is `{draft:"99",ref:"config:config-reporting_weight_35",compiled:"0"}`. The fixture had intentionally set the shared threshold to0;99 is ignored. The same stale-reference issue applies to direct clause edits. Source/unit/operator changes can also leave hidden incompatible references rather than follow the question-owned editor's transition rules.

**Correction:** make both authoring surfaces use one explicit reference/value mutation contract. Either route managed condition editing to the owning question with clear shared binding disclosure, or show the shared selector on the direct inspector and clear the binding when switching to a literal. Do not silently modify the shared global value. Preserve pinned publications.

**Acceptance:** use both primary and extra-clause direct inspector paths; bind a shared config, switch to literal99 and compile99 with no reference for that clause; edit source/operator and verify incompatible references are visibly resolved or cleared; current shared-bound comparisons still compile the current configured value; old snapshots stay unchanged.

## P2 — Branch tester and operator disagree on equivalent numeric input

**Code:** `question-rules.js:27–28` presents a Number input but stores `el.value` as a string. `app.js:34` converts Number answers to numbers during operator simulation. Numeric equality in `app.js:30` is numeric only when the answer is already numeric.

**Independent actual-stack reproduction:** configured equality to shared35kg; branch tester input `35.0` yields `Otherwise / unanswered → Complete and record outcome`. Evaluating the same compiled rule with the operator's numeric35 yields `true`. Both are valid representations of the same numeric answer. Leading zero and exponent representations have the same discrepancy; inequalities do not expose it because they already coerce numbers.

**Correction:** normalize test answers by their question type using the same semantics as operator input. Preserve empty/unanswered distinctly from zero; reject non-finite numeric values. Do not change text/identifier comparison semantics merely to fix Number answers.

**Acceptance:** for `=` and `!=`,35,35.0 and equivalent finite numeric strings agree between branch tester and operator; zero remains a valid answer, blank stays unanswered, invalid numbers show a validation error. Include typed-reference and literal comparisons.

## Tests, retained strengths and limits

`sh run-checks.sh` completed exit0 with all18 Node files; receipt `round4-independent-checks.txt`. The two new probes reused the actual combined-model harness outside the implementation tree and demonstrate coverage gaps in that green result. No new browser replay was performed by this judge in this pass; parent/visual judge own new UI evidence.

Existing actual-stack assertions continue to pass for direct/category/subcategory union, excluded modules, actual action and catalogue compilation, revocation failing closed, stable typed identities, immutable value/revision snapshots, usage discovery and inherited-removal acknowledgement. The local transit model retains context isolation, duplicate-event protection and required/optional gates. These retained results are not a substitute for fixing the two new paths.

Production database persistence, real task dispatch, full generic analytics and mobile sync remain outside this mock. Scope notes are descriptive. The earlier actual Vaccination route failure remains separate. No production write, push, merge or deploy occurred.

## Recheck

Both findings above are retained as discovery evidence and subsequently corrected. Independent actual-stack replay and all18-suite final run passed. See `round4-final.md` for the bounded final judgement, separately attributed browser proof and persistence compatibility assessment.
