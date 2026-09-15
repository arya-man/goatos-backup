# Mesha configuration & SOP studio

Local interactive design prototype using the existing admin theme tokens from `apps/admin-web/app/mesha-theme.css`.

Open http://127.0.0.1:4318 while the local server is running. To restart:

```sh
cd /Users/raviteja/mesha/tmp/config-sop-studio
python3 -m http.server 4318 --bind 127.0.0.1
```

## Supported interactions

- Sales eligibility comparison, minimum, allowance and target rate; review/publish updates linked Weighing values.
- Per-module proposed business rules. Common resource checkbox sharing changes SOP catalogue/action availability.
- Reusable questions and actions, local draft/publication, per-definition sharing.
- Multiple SOPs per module; draft graph nodes can be added, dragged, edited, deleted and connected through destination selectors.
- Numeric, text, boolean, choice, multiple choice, date, photo/video and catalogue questions.
- Conditions with numeric comparisons, equality and contains; explicit match and otherwise paths.
- Graph validation for missing/dangling destinations, cycles, unreachable steps, unavailable resources and unanswered condition sources.
- Operator simulation follows actual answers and actions. Invalid or missing answers block progression.
- Published snapshots are versioned independently of the editable draft. Director/operator views expose published workflows.

## Boundaries

All state is stored under `mesha-studio-v1` in browser localStorage. There are no production API calls, media uploads, inventory mutations, external notifications or deployments. Role switching demonstrates UI behaviour; it is not authentication. Catalogue entries and non-Sales settings are illustrative. The supplied 103°F threshold demonstrates branching and is not medical advice. Medicine selection requires a vet-approved protocol, not an invented dose.

See DESIGN.md and PROGRESS.md for design rationale, acceptance evidence and remaining scope.

## Refinement: Items and treatment courses

- **Items Config:** Vertical → Category → Subcategory → Item; create/edit, descriptions, purpose, units, active/archive, per-item module sharing, exact linked workflow steps.
- **Compiled workflow definitions:** validated JSON export and browser execution, stable item IDs, versioned item/catalogue/config snapshots. This is a proposed integration format, not a deployed Android/backend compiler.
- **Health → Treatment courses:** live-source Fever example from Adults SOP and Kids SOP (16 steps each), editable day/session groups, ordered actions/medication rows, schedule/flow views, local publish and read-only snapshots. Course item references participate in archive-impact review.
- Health source item names and raw fields are taken from the explicitly supplied sheet, read 2026-09-15. No missing dosage numerator units are inferred; source-unspecified units are retained. Broader clinical course scheduling, nested SOP execution, and the other source diseases are not implemented by this bounded example.

Verification: `node judge-functional-tests.cjs` (39), `node judge-v2-functional.cjs` (29), `node judge-health-functional.cjs` (21). Browser and visual evidence is recorded in BROWSER-QA.md and judge receipts.

## Combined v3
Start with Health → Diagnosis for Aryaman source scenarios and nested rule drafts. Common workspace → Data sources manages reusable dropdown collections (vaccines, medicines, feed, vendors, pens). Each module's Business rules separates its own settings from references to other owners. General SOPs retain interactive branching and pinned local publications.
Comparison: COMPARISON-DECISIONS.md. Final evidence: judge-v3-functional.md (125 assertions +260 source cases) and judge-ux-v3.md (scoped desktop/mobile visual pass).
Health runs recorded source scenarios; edited rules do not evaluate new observations. All writes stay local to this browser.

## Direct canvas editor
Open any module SOP: drag output/input circles to connect (or click each), use the + on a path to insert a connected step, drag nodes to arrange, and pan empty canvas. Toolbar offers Undo/Redo and Fit. Validation issues jump to affected nodes. Counts includes a separate browser-local Daily count canvas review example; prior drafts are preserved.
Desktop proof: judge-canvas-functional.md (143 combined assertions) and judge-canvas-ux.md (actual authoring and operator branch checks). Mobile authoring usability is not certified in this revision.

## Review this PR

From this directory, run `python3 -m http.server 4318 --bind 127.0.0.1` and open `http://127.0.0.1:4318/#Procurement/Editor`. Run all focused Node checks with `sh run-checks.sh`. No npm install is needed. Browser drafts belong to the URL origin and are not committed.

`judge-v3-oracle.py` is an optional comparison against a separate upstream checkout; set `HEALTH_SOP_SOURCE` to its path. The runtime uses the included snapshots. Historical review notes record the state at each review, not blanket current certification. See PROGRESS.md for the latest receipt.
