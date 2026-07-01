# Vaccination V1 Bug Bash + E2E Checklist

Date: 2026-07-01
Status: V1 demo blockers closed; route optimization/production-scale programs are outside this demo contract
Scope: Clean-slate PHC Vaccination V1 setup and demo flow
Primary surface: Admin/Data Ops -> SOP Library -> PHC / Vaccination SOP builder
Related surface: Admin/Data Ops -> Config -> Vaccination rule builder

This file captures the bugs found while walking the clean-slate vaccination V1
setup flow. The V1 demo blockers are now closed by the evidence update below.
The remaining unchecked items are production-hardening, route/resource
optimization, scale/permutation, or broader browser-negative coverage and must
not be read as blocking the local V1 demo. The vaccination rule matrix, source
schedules, dose/vial/revaccination data, and compatibility spacing are V1
scope.

## 2026-07-01 Evidence Update

Validated V1 demo closure evidence:

- Full E2E smoke: `NUANCE-RULES-20260701-V1-MATRIX-R2`
  - Report: `/Users/ravi/mesha/goatos-vaccination-v1-close/.codex-goatos-render/e2e-smoke/NUANCE-RULES-20260701-V1-MATRIX-R2`
- SOP/Config authoring smoke: `NUANCE-RULES-20260701-V1-MATRIX-R2`
  - Report: `/Users/ravi/mesha/goatos-vaccination-v1-close/.codex-goatos-render/vaccination-authoring/NUANCE-RULES-20260701-V1-MATRIX-R2/authoring.md`
  - Proves the Nuance Rules matrix loads, saves, and publishes ET+TT, PPR, Goat Pox, FMD, and HS as separate protocol versions from one grid flow.
- Click/interlink matrix: `NUANCE-RULES-20260701-V1-MATRIX-R2`
  - Report: `/Users/ravi/mesha/goatos-vaccination-v1-close/.codex-goatos-render/vaccination-click-matrix/NUANCE-RULES-20260701-V1-MATRIX-R2/matrix.md`
- Chain proof: `vaccination-chain-proof stamp=1782915162`
- Rework proof: `vaccination-rework-proof stamp=1782899439`
  - Proves reject -> rework -> resubmit -> accept, accepted/rejected Passport history, and real Passport workflow row linkage.
- Visual smoke screenshots:
  - `/Users/ravi/mesha/goatos-vaccination-v1-close/.codex-goatos-render/admin-web-screenshots/2026-07-01T14-12-57-572Z`
  - Rework Passport: `/Users/ravi/mesha/goatos-vaccination-v1-close/.codex-goatos-render/admin-web-screenshots/2026-07-01T09-04-55-269Z/desktop-goat-passport-rework-proof.png`

Closed for V1 demo: SOP builder lifecycle, all supported SOP question types,
multi-row Config matrix authoring/publish, source schedule/dose/vial/revaccination
rows, V1 compatibility spacing, generated work, accepted proof, rejected/rework
proof in the data plane, Goat Passport vaccination history/open due rows, and
current visible click/interlink paths.

Outside the V1 demo contract: route/resource optimizer, million-goat
scale/permutation testing, business-facing user/shed admin setup UI, and full
browser-negative coverage for every invalid input.

## Non-Negotiable Vaccination Slice Rule

Every visible vaccination-slice control must be treated as product behavior, not
decoration.

This includes:

- buttons
- row clicks
- cards
- chips
- dropdowns
- checkboxes
- radio/segmented controls
- text/number/date fields
- file inputs
- modals
- side drawers
- pagination
- search
- filters
- links between Config, SOP Library, Vaccination, Action Center, Protocol
  Adherence, Workflows, Control Tower, Goat Passport, and shed execution detail

For every one of these controls, E2E must prove one of two things:

- it performs a real backed vaccination action and the result is visible after
  refresh/reopen, or
- it is intentionally unavailable and shows a clear reason directly in the UI.

No vaccination-slice UI should look clickable if it does nothing. No disabled
button should be silent. No drawer should mix a real action with a placeholder
explanation. No seeded-only shortcut should be used as proof that the admin flow
works from scratch.

## Kernel / Event Chain Gate

UI screenshots are not enough. Vaccination V1 is demo-ready only when the E2E
proves the operational kernel chain.

The clean proof chain is:

1. Published vaccination SOP exists.
2. Published vaccination config/protocol exists.
3. Goats exist in valid sheds with usable stage, sex, breed, lifecycle, health,
   and reproductive facts.
4. Vaccination generation creates per-goat due obligations.
5. Sweeper groups due obligations into shed execution work.
6. SOP/proof workflow is attached to the generated work.
7. Calendar, Action Center, Protocol Adherence, Workflows, Control Tower, and
   Vaccination screens refresh from those backend rows.

Required local processes for proof:

- Postgres
- backend API
- admin-web
- outbox relay or in-process event dispatcher
- domain event consumer or equivalent local event handler path
- obligation generation/backfill for goats already in the database
- obligation sweeper/projector for batching, missed-dose, reminder, and calendar
  projection rows

Important clean-slate rule:

- New goat creation/import should flow through `goat.created` -> event consumer
  -> vaccination obligation generation.
- Existing goats already present before a config is published need explicit
  generation/backfill after publish.
- The sweeper batches obligations that already exist; it does not create the
  first per-goat due rows by itself.
- Calendar, Protocol Adherence, Workflows, Action Center, and Control Tower
  should be verified only after obligation generation and sweeper/projector
  steps have run.

Time-accelerated demo fixtures must cover:

- due today
- due soon
- overdue
- missed
- completed with accepted proof
- proof pending
- rejected/rework
- deferred sick/treatment/ICU/quarantine
- reopened after recovery/exit
- death/sale/exit cancellation
- shed shift
- existing goat backfill
- duplicate-run idempotency

## Goat Entry Paths That Feed Vaccination

Vaccination should not invent goats. It should react when a goat becomes a
canonical GoatOS goat with enough facts to evaluate the vaccination matrix.

The vaccination E2E must cover all three goat entry paths:

1. Counts -> Herd Register -> Register goat
   - One goat is created directly in GoatOS.
   - The create action emits a `goat.created` domain event.
   - Vaccination generation evaluates that one goat against the published
     matrix/config.

2. Counts -> Herd Register -> Import sheet / bulk commit
   - Many goats are uploaded from the template.
   - Each accepted row becomes a canonical goat.
   - Each accepted goat must be eligible for the same event/generation path as
     a manually registered goat.
   - Invalid rows must not generate vaccination work.

3. Procurement -> Source Entry -> Accepted intake
   - Purchased goats start in the procurement/source-entry journey.
   - Holding-farm vaccination evidence may suppress duplicate arrival
     vaccination where the rules allow it.
   - Only accepted-intake goats should enter the normal PHC vaccination engine.
   - Rejected, canceled, source-only, or pre-intake goats must not create PHC
     vaccination obligations.

Plain operating rule:

- The matrix is checked per goat.
- Due obligations are created per goat and dose.
- Execution work is batched later by shed/protocol/due window so operators do
  not handle one goat at a time unless the rule says to force a micro-drive.

## Kernel And Event Contract

The demo cannot be treated as real if the UI only shows seeded cards. The V1
kernel must prove the event chain:

1. A business action happens.
2. A domain event or equivalent durable change is emitted.
3. The outbox/pubsub/consumer path processes it.
4. The vaccination generator/sweeper updates obligations, SOP tasks, read
   models, and audit/process-integrity rows.
5. Vaccination, Action Center, Protocol Adherence, Workflows, Calendar, and
   Control Tower show the same backend truth with the same IDs.

The E2E must prove the kernel for these event families:

- config draft saved
- config published
- SOP draft saved
- SOP published
- goat created manually
- goat imported from template/CSV
- procurement accepted intake creates canonical goats
- goat stage changed
- goat shed/location changed
- goat health/defer state changed
- goat reproductive state changed
- goat death/sale/exit cancels open work
- trusted external vaccination history suppresses duplicate work
- vaccination due work generated
- due work grouped into shed/cohort execution rows
- missed deadline sweeper marks missed/overdue
- deferred work reopens after recovery/exit
- SOP proof submitted
- proof accepted records vaccination history
- proof rejected creates rework
- completion schedules next dose/booster where configured

Scale rule:

- The kernel must be tested as per-goat generation, but read models and
  execution views must group by shed/protocol/dose/window so the design can
  scale toward 1 million goats.
- Re-running generators/sweepers must be idempotent: no duplicate obligations,
  no duplicate SOP tasks, and no duplicated workflow rows for the same goat,
  protocol, dose, and due window.

## Interlinked Navigation Contract

Cross-screen navigation is valid only when it preserves the same business
record. A click must carry the same goat, shed, protocol, dose, obligation,
batch, workflow, or source-load context into the next screen.

Expected behavior:

- Protocol Adherence row click opens a read-only adherence drawer for that
  exact expected-vs-actual record.
- Protocol Adherence -> Open Action Center is allowed only if it opens the
  related Action Center card for the same obligation/action. It must not open a
  random card.
- Protocol Adherence -> Workflow record is allowed only if it opens the same
  workflow chain.
- Action Center -> Workflow record must open the same workflow chain.
- Action Center -> Goat Passport must open the exact goat when the action is
  goat-scoped. If the action is only batch/shed-scoped, the button must be
  hidden or disabled with a clear reason.
- Vaccination row/drawer -> shed execution detail must open the same shed event
  or cohort.
- Procurement source-load drawer -> load actions/HF evidence must open the same
  source load.
- Herd Register row actions that affect vaccination must preserve the same goat
  and shed context.

If the target record does not exist or is outside the V1 vaccination slice, the
control must not look clickable. It must be hidden or disabled with a clear
reason.

## Historical Blocking Bugs Now Covered For V1 Demo

The observations below are retained as the bug-bash ledger. The V1 demo
regression coverage for them is `NUANCE-RULES-20260701-V1-MATRIX-R2`,
`NUANCE-RULES-20260701-V1-MATRIX-R2`,
`vaccination-chain-proof stamp=1782915162`, and
`vaccination-rework-proof stamp=1782899439`.

### 1. Save draft does not prove persistence

Observed:

- Clicking Save draft gives no clear success or error feedback.
- The modal can remain open with no visible state change.
- The newly created draft is not visible in SOP Library after returning to the
  list.
- Only the seeded published Vaccination Session is visible.

Expected:

- Save draft must create or update a draft SOP version.
- User must get clear success or validation error feedback.
- Saved draft must appear in SOP Library immediately.
- Saved draft must be reopenable with the same name, trigger, steps, field
  types, labels, conditional rules, proof policy, and status.

### 2. Step 1 shows invalid conditional rules

Observed:

- Step 1 can show rules like "require this if previous answered".
- Step 1 has no previous step, so this rule is invalid.

Expected:

- Step 1 should hide previous-step-dependent rules.
- Step 1 should allow only rules that make sense, such as no rule or required /
  block submission if empty.
- Later steps may use previous-step conditional logic.

### 3. Select and multiselect fields have no answer option builder

Observed:

- The builder allows field type select and multiselect.
- After selecting them, there is no visible way to add answer choices.
- Without choices, the question cannot be answered correctly by an operator.

Expected:

- Select fields must show an option builder.
- Multiselect fields must show an option builder.
- Options must support add, edit, remove, and reorder.
- Save draft and publish must block if select/multiselect has zero options.
- Reopening the draft must show the saved options.

### 4. Field-type-specific inputs are incomplete or unclear

Observed:

- The builder exposes text, number, yes/no, select, multiselect, goat scan/RFID,
  shed picker, vaccine batch picker, medicine picker, photo proof, and video
  proof as field types.
- The UI does not clearly show the required configuration for each type.

Expected:

- Text: label, required rule, optional placeholder/help.
- Number: label, required rule, unit, min/max when needed.
- Yes/no: label, required rule, pass/fail behavior when needed.
- Select: label, required rule, options.
- Multiselect: label, required rule, options, min/max selected when needed.
- Goat scan/RFID: label, required rule, expected source of eligible goats.
- Shed picker: label, required rule, scope source.
- Vaccine batch picker: label, required rule, FEFO/lot source.
- Medicine picker: label, required rule, inventory source.
- Photo proof: label, required rule, proof policy binding.
- Video proof: label, required rule, proof policy binding.

### 5. Publish and dry-run readiness is not obvious

Observed:

- Dry-run and Publish can appear disabled without a clear reason.
- It is not clear whether the blocker is missing source, missing required
  fields, missing options, missing proof policy, or backend failure.
- A fully filled-looking form can still leave Publish disabled with no visible
  error location.

Expected:

- Disabled buttons must show exact missing requirements.
- Dry-run should validate the draft and show what would be created.
- Publish should require a valid draft, source/review requirements, and any
  mandatory proof policy.
- Publish must never be disabled silently. The UI must point to the exact field,
  section, or backend validation error blocking publish.

### 6. Error UI and validation placement must be demo-safe

Observed:

- Validation errors are not shown near the broken field.
- It is unclear whether the user should scroll, save, dry-run, or fix a field.
- Prior UI fixes caused text overlap and poor alignment in vaccination surfaces.

Expected:

- Every field-level error appears directly below or next to that field.
- Section-level errors appear at the top of the section and link/focus the
  broken field.
- Footer actions show a concise disabled reason and do not hide form content.
- No error, helper text, chip, dropdown, or sticky footer may overlap another UI
  element at desktop or narrow viewport.
- E2E must include screenshots for error, success, disabled, and published
  states.

### 7. Vaccination config SOP dropdown shows wrong options and raw IDs

Observed:

- Vaccination config shows SOP options with raw internal IDs such as
  `b0000000...`.
- Non-vaccination SOPs such as Shifting appear in the vaccination config SOP
  dropdown.
- A newly created/published SOP is not proven to appear in the dropdown.

Expected:

- Vaccination config must show only published PHC / Vaccination SOP versions.
- Non-vaccination SOPs must be hidden.
- Labels must be human-readable, for example
  `Vaccination Session - v1 - Published`.
- Raw UUID/version IDs must not be the primary visible label.
- A newly published vaccination SOP must appear in this dropdown without manual
  database work.

### 8. Vaccination matrix entry is too repetitive for real use

Observed:

- The current UI makes one matrix row feel like one long form.
- Creating ET/K1, PPR/K2, and other vaccine/stage/breed combinations means
  repeating many shared fields.
- This is slow and error-prone for an admin configuring a real vaccine roster.

Expected:

- The UI should support a matrix/grid entry mode for vaccination rows.
- Shared header fields such as scope, source, approval, and SOP can be entered
  once.
- Repeated matrix rows should be editable in a table: vaccine, stage, sex,
  breed, reproductive rule, defer rule, dose sequence, trigger, offset, window,
  amount, unit, route/site, and inventory item.
- The backend may still store each row as separate protocol/rule data, but the
  admin UX should not force repeated full-form entry for every simple
  vaccine/stage combination.

### 9. Escalation policy is free-text and over-claims behavior

Observed:

- Escalation policy is shown as free text like
  `miss -> Asst -> Park Head -> PHC Director; overdue -> escalate`.
- It is unclear whether this creates Action Center items, Control Tower gaps,
  notifications, or only stores text.
- It is unclear what happens if users/operators/park heads are not seeded or
  mapped.

Expected:

- Escalation must be structured, not free text.
- The UI must show exactly what will happen:
  Action Center item, Control Tower breach, reminder/notification, or review
  queue.
- If notification delivery is not implemented, the UI must not imply external
  notifications.
- If required operators/roles are missing, publish/preview must show a clear
  missing-owner or missing-role blocker.
- E2E must prove the configured escalation creates the expected visible
  operational record.

### 10. Preview Impact uses developer wording

Observed:

- Preview section title says `Impact preview - computed from schedule[]`.
- `schedule[]` is implementation language and does not explain the business
  outcome to an admin, CEO, COO, or operations user.

Expected:

- Title should be user-facing, for example `Preview impact` or
  `Before you publish`.
- Subtitle should explain the result, for example:
  `Shows how many goats, tasks, and doses this rule will create before publishing.`
- E2E screenshots must prove the developer wording is gone.

### 11. Schedule builder and checkbox rules must prove business behavior

Observed:

- The form shows vaccine eligibility checkboxes such as pregnant/lactating
  exclusion and Sick/Treatment/ICU/Quarantine defer rules.
- The schedule builder shows dose/phase rows, but it is not obvious from the UI
  whether those rows actually control due-date generation, preview counts, and
  backfilled obligations.

Expected:

- Each checkbox must be tested as a real rule, not only as saved UI state.
- Pregnant/lactating exclusions must affect Preview Impact and generated work.
- Sick/Treatment/ICU/Quarantine rules must mark affected obligations deferred,
  not hide or silently drop them.
- Each dose/phase row must affect Preview Impact and generated obligations.
- E2E must include one goat included by the matrix, one goat excluded by the
  matrix, and one goat deferred by the matrix.

### 12. Every vaccination modal needs save/publish/click E2E

Observed:

- Bugs were found only after manually clicking modals, dropdowns, side drawers,
  pagination, and row actions.
- Current tests were not strong enough to catch broken clicks, silent disabled
  buttons, missing success states, raw IDs, text overlap, or wrong redirects.
- Some controls looked like real actions but only opened explanatory drawers or
  routed to another screen without completing the action.
- Some screens mixed seeded demo data with admin-created data, making it unclear
  what was actually created through the UI.

Expected:

- Every modal in the vaccination slice must have the same save/draft/publish,
  validation, success, failure, reopen, close, and screenshot checks.
- Every row action and drawer action must either perform a real backed action or
  show a clear disabled/unavailable reason.
- Every control must be tested both before and after refresh so the test proves
  persistence, not just temporary client state.
- Every successful action must create the expected visible result in the next
  relevant vaccination surface.
- Every failed action must keep user input and show the exact blocking reason.
- E2E must fail on console errors, Next runtime errors, backend_down contract
  screens, text overlap, invisible drafts, and fake clickable controls.
- Test runs must be bounded: if the same failure repeats twice, stop, record the
  blocker, and fix that blocker before starting another broad run.

### 13. Kernel chain is not proven from clean slate

Observed:

- Some screens can show seeded/demo rows without proving the full backend
  event chain.
- UI-only proof does not prove that config publish, goat events, generation,
  sweeper batching, SOP workflow, calendar projection, and read models are tied
  together.

Expected:

- Clean-slate E2E must prove:
  `SOP + config publish -> goat created/imported or existing-goat backfill -> obligation generation -> sweeper batching -> SOP/proof workflow -> Calendar/PA/WF/AC/CT/Vaccination read models`.
- The test must show database or API evidence for each transition, then show
  the matching UI surface.
- The same E2E run must verify repeated generation/sweeper runs are idempotent.

### 14. Calendar / PA / WF / AC / CT data conditions are not explicit

Observed:

- It is unclear when Calendar rows, Protocol Adherence rows, Workflow records,
  Action Center cards, and Control Tower signals should appear.
- This makes it hard to tell whether an empty screen is correct, broken, or
  waiting for a background kernel step.

Expected:

- Calendar appears after obligations exist and calendar projection/refresh runs.
- Protocol Adherence appears after expected obligations exist; actual/proof/
  completion/missed state updates the gap.
- Workflows appear after generated work has a chain record to inspect.
- Action Center appears after generated work has an actionable next step,
  blocker, overdue state, proof need, or escalation.
- Control Tower appears after aggregate read models refresh from operational
  state.
- Each empty state must say which upstream kernel step is missing.

### 15. Time-accelerated kernel scenarios are missing

Observed:

- Real farm time cannot be used during demo/testing to wait for due-soon,
  overdue, missed, deferred, reopened, or completed states.

Expected:

- E2E must seed dates/as-of values to prove due-soon, due-now, overdue, missed,
  completed, deferred, reopened, death/sale cancellation, shed shift, and
  duplicate-run idempotency in minutes.
- Every seeded timing scenario must be traceable to the generated backend rows,
  not just a hardcoded UI card.

### 16. Production-hardening Procurement and Herd Register entry paths

Observed:

- The vaccination flow discussion covered config, SOP, generated work, and
  execution screens, but procurement/source-entry and Herd Register entry paths
  were not treated as first-class demo inputs.
- It is unclear from UI alone whether a goat came from manual Herd Register,
  CSV/template import, or procurement accepted intake.
- It is unclear which procurement states should create PHC vaccination work and
  which should not.

Expected:

- E2E must prove manual Herd Register goat creation triggers vaccination
  generation.
- E2E must prove Herd Register CSV/template import triggers vaccination
  generation for each accepted goat row.
- E2E must prove procurement accepted intake triggers vaccination generation.
- E2E must prove rejected/canceled/source-only/pre-intake procurement goats do
  not create PHC vaccination obligations.
- E2E must prove trusted holding-farm vaccination evidence suppresses duplicate
  PHC obligations where configured.
- All procurement and Herd Register controls that affect vaccination must either
  work, or be hidden/disabled with a clear reason.

### 17. Interlinked navigation can lose meaning or context

Observed:

- Protocol Adherence row clicks can open a drawer, and then Open Action Center
  can navigate to Action Center with another drawer open.
- This is only useful if the destination is the exact same vaccination
  obligation/action. Otherwise it feels random and is not demo-safe.
- Similar risks exist for Action Center -> Workflow, Protocol Adherence ->
  Workflow, Vaccination -> shed execution detail, Procurement -> load actions,
  and Herd Register -> goat/vaccination context.

Expected:

- Every cross-screen button preserves the same business context and visible
  identifiers.
- The destination screen must show the same protocol, dose, goat/cohort, shed,
  due date, workflow/action id, and status where applicable.
- Back/close returns to the previous screen without losing filters, page, or
  selected row.
- If a destination cannot be resolved, the button is hidden or disabled with a
  clear reason.
- E2E must click every vaccination-relevant row, drawer action, and cross-link
  and assert the destination context, not just that a page opened.

### 18. Older goats with unknown history can flood due work

Observed:

- A vaccine can have multiple age-based doses, for example dose 1 at 6 months
  and dose 2 at 10 months.
- If a 1-year-old or 2-year-old goat is created/imported with missing
  vaccination history, the generator may be tempted to create all missed
  historical doses as due today.
- At scale, that would flood Calendar, Action Center, Protocol Adherence, and
  Control Tower with noisy work.

Expected:

- Config must support an explicit catch-up policy for late/unknown-history
  goats.
- Catch-up policy must be per vaccine/dose/course, not a global guess.
- The policy must support:
  - max age to start a dose/course
  - skip-if-past-age behavior
  - PHC review-required behavior for unknown history
  - first-catch-up-dose-only behavior
  - min gap between catch-up doses so multiple doses are not scheduled same day
  - trusted-history suppression when imported/procurement evidence is accepted
- E2E must prove that older goats with missing history do not create multiple
  same-day historical obligations unless the source-approved policy explicitly
  allows it.

Implementation status, 2026-07-01:

- Kernel guard added in `backend/internal/vaccination/app/generation.go`: for a
  single goat/protocol run, old non-`next_cycle` catch-up rows materialize one
  safe catch-up/review action after trusted-history checks, rather than every
  historical dose row.
- Focused tests added in
  `backend/internal/vaccination/app/generation_test.go`:
  `TestOlderGoatUnknownHistoryCreatesOnlyOneHistoricalCatchUp`,
  `TestOlderGoatUnknownHistoryCreatesOnlyOnePHCReviewCatchUp`, and
  `TestOlderGoatTrustedFirstDoseAllowsNextMissingDose`.
- Live trusted-history proof added in
  `tools/dev/vaccination-trusted-history-proof.sh`. Latest passing run:
  `vaccination-trusted-history-proof stamp=1782917268`, which seeded a goat
  with accepted/verified ET+TT 4-week history, suppressed the old 4-week row,
  generated the next ET+TT 7-week obligation, batched it into a shed drive/SOP
  task, and verified Calendar drive aggregation plus Action Center, Workflow,
  Passport, Control Tower, and Protocol Adherence visibility.
- Verified with `go test ./internal/vaccination/app` and
  `go test ./cmd/generate-vaccination-obligations`.

## Clean-Slate Vaccination V1 E2E Path

This is the end-to-end path that must pass after fixes. It follows the demo flow
from a clean tenant/state, not isolated page checks.

### 1. Users and role setup

- [ ] Seed or create admin/superadmin user.
- [ ] Seed or create PHC director/author user.
- [ ] Seed or create vaccination operator user.
- [ ] Seed or create park head/manager user.
- [ ] Seed or create verifier user.
- [ ] Map operator, park head, and verifier to the relevant park/scope.
- [ ] E2E verifies missing user/role mappings produce clear setup errors, not
  silent owner-missing rows.

### 2. Park and shed setup

- [ ] Create or seed park.
- [ ] Create or seed shed under park.
- [ ] Shed has required stage metadata, for example K1/K2.
- [ ] Shed creation/import success is visible.
- [ ] E2E screenshot proves the shed is visible before goat entry.

### 3. Goat setup

- [ ] Create or import goats into the shed.
- [ ] Goat has required facts: id/tag, sex, breed, date of birth/age, lifecycle,
  current shed, health/defer state, and reproductive state when applicable.
- [ ] Existing goats already in the database are included in backfill after rule
  publish.
- [ ] E2E screenshot proves goats exist before vaccination config is published.

### 3A. Goat entry paths that must be tested

- [ ] Herd Register single-goat create creates one canonical goat.
- [ ] Herd Register single-goat create emits or queues the goat creation event
  needed by vaccination generation.
- [ ] Herd Register single-goat create results in due vaccination work when the
  goat matches the published matrix.
- [ ] Herd Register CSV/template import previews valid and invalid rows.
- [ ] Herd Register CSV/template import commits only valid accepted rows.
- [ ] Each accepted imported goat is evaluated by vaccination generation.
- [ ] Invalid imported rows do not create goats or vaccination work.
- [ ] Procurement Source Entry can create a purchased load.
- [ ] Procurement holding-farm vaccination evidence can be recorded or marked
  missing with a clear state.
- [ ] Procurement accepted intake creates canonical goats.
- [ ] Procurement accepted intake emits or queues the goat creation event needed
  by vaccination generation.
- [ ] Procurement accepted-intake goats result in due vaccination work when they
  match the published matrix.
- [ ] Procurement rejected, canceled, source-only, and pre-intake goats do not
  create PHC vaccination obligations.
- [ ] Trusted holding-farm vaccination history suppresses duplicate PHC
  obligations where configured.
- [ ] The UI makes the source of each tested goat clear enough for demo:
  manual, template import, or procurement accepted intake.

### 3B. Business source cross-check

- [ ] Cross-check goat identity, park, shed, stage, and lifecycle assumptions
  against Goats and Parks source material.
- [ ] Cross-check vaccination matrix fields against the vaccination source notes
  and leadership note: vaccine, stage/age, sex, breed, lifecycle, reproductive
  state, health/defer state, dose timing, window, route, amount, proof, and
  source approval.
- [ ] Cross-check purchased-goat intake assumptions against procurement/source
  material: holding farm, warmup, HF vaccination evidence, accepted intake,
  rejected/canceled loads, and mixed age/stage groups.
- [ ] Cross-check SOP/operator/proof/escalation language against farm handbook
  and PHC role material.
- [ ] Where docs do not answer a business rule, mark it as review-needed in the
  handover instead of pretending the algorithm knows it.

### 4. Vaccination SOP setup

- [ ] Open SOP Library.
- [ ] Create Vaccination Session SOP from the modal.
- [ ] Create at least two SOP drafts so E2E proves list refresh, unique names,
  status labels, and no accidental overwrite.
- [ ] Add required execution steps/questions.
- [ ] Save draft and verify it appears in SOP Library.
- [ ] Reopen draft and verify every field persists.
- [ ] Dry-run the SOP and verify success/error output.
- [ ] Publish SOP and verify published version appears.
- [ ] Verify the expected SOP draft/publish events or audit records are created.
- [ ] Verify the published SOP is selectable by vaccination config and unrelated
  SOPs are not selectable.
- [ ] E2E screenshots cover empty form, filled form, validation errors, saved
  draft, and published SOP.

### 5. Vaccination config/matrix setup

- [ ] Open Config -> Vaccination.
- [ ] Create matrix rows for the source-approved vaccine/stage combinations.
- [x] Use matrix/grid entry where possible so shared fields are not repeated.
- [ ] Attach the published Vaccination Session SOP.
- [ ] Fill source/review/approval fields.
- [ ] Fill schedule rows: dose, trigger, offset, window, amount, unit,
  route/site.
- [ ] Toggle pregnant, lactating, Sick, Treatment, ICU, and Quarantine rules and
  prove they change preview/generation behavior where expected.
- [ ] Preview impact and verify eligible goats, obligations, shed tasks, doses,
  and stock warnings.
- [ ] Publish config only after every validation passes.
- [ ] Verify the expected config draft/publish events or audit records are
  created.
- [ ] E2E screenshots cover empty config, filled matrix rows, preview impact,
  validation errors, and published config.

### 6. Generation and backfill

- [ ] After publish, existing eligible goats receive due vaccination work.
- [ ] After publish, the first place to check is generated due work in the
  Vaccination page for the matching shed/goat cohort.
- [ ] New goat creation/import triggers vaccination due generation.
- [ ] Manual Herd Register goat creation triggers the same generation path as
  imported goats and procurement accepted-intake goats.
- [ ] CSV/template import is evaluated per accepted goat row but can be committed
  and tested as one bulk operation.
- [ ] Procurement accepted intake is evaluated only after the goat becomes a
  canonical GoatOS goat; procurement warmup/source-only rows do not enter PHC
  generation.
- [ ] The generated obligation is per goat/per dose; the generated execution
  work is grouped by shed/protocol/due window.
- [ ] Stage/shed/health/defer changes recheck affected goats.
- [ ] No duplicate obligations are created for the same goat/vaccine/dose.
- [ ] Dead/sold/transferred goats do not remain overdue.
- [ ] New-goat flow proves `goat.created` or equivalent domain event reaches
  vaccination generation.
- [ ] Existing-goat flow proves explicit generation/backfill runs after config
  publish.
- [ ] Sweeper/projector runs after generation and creates shed execution work,
  missed-dose state, reminders, and calendar projection rows where applicable.
- [ ] Re-running generation and sweeper does not duplicate obligations, shed
  events, SOP tasks, calendar rows, or Action Center cards.
- [ ] Action Center, Protocol Adherence, Workflows, and Control Tower reflect
  only real generated work, not seeded noise or fake rows.

### 7. Operational surfaces

- [ ] Vaccination page shows due work clearly.
- [ ] Counts -> Herd Register shows created/imported goats that will feed the
  vaccination matrix.
- [ ] Procurement -> Source Entry shows only purchase/source/warmup/intake
  state; it must not imply PHC vaccination work before accepted intake.
- [ ] Action Center shows the correct actionable items.
- [ ] Protocol Adherence shows expected vs actual without raw/internal gap keys.
- [ ] Workflows show the chain with readable statuses.
- [ ] Control Tower shows process gaps/breaches only when real.
- [ ] Every row click, drawer, pagination control, filter, close button, and
  drilldown either works or shows a clear disabled reason.
- [ ] E2E screenshots cover each page and drawer used in the demo.

Surface data conditions:

- [ ] Herd Register is the direct GoatOS goat-entry source for manual and CSV
  goats.
- [ ] Procurement Source Entry feeds vaccination only after accepted intake.
- [ ] Vaccination page is checked first after generation/sweeper because it
  shows due work, matrix/cohort state, and shed execution rows.
- [ ] Action Center shows cards only when generated work has a next action,
  blocker, overdue state, proof need, or escalation.
- [ ] Protocol Adherence shows expected-vs-actual gaps only after obligations
  exist.
- [ ] Workflows shows the lifecycle chain for one generated work record; it is
  not the first proof that generation happened.
- [x] Calendar shows due/missed/reminder projections only after calendar
  projection/refresh runs; after the V1 sweeper attaches goat due rows to a
  shed drive batch, active Calendar suppresses the batched per-goat
  `vaccination_dose_due` rows and shows one `vaccination_drive` row instead.
- [ ] Control Tower shows aggregate breach/health signals only after read models
  refresh.

### 7A. Screen order after publishing config

Do not jump randomly between screens after config publish. Verify in this order:

1. Config
   - Published rule appears in the config list with correct status/version.
   - The rule can be reopened/read without losing matrix values.

2. Vaccination page
   - This is the first operational screen after config publish.
   - It should show generated due work for the eligible goats/sheds.
   - It should not show non-eligible goats as due.
   - Deferred goats should appear as deferred/recheck-needed, not as missing.

3. Action Center
   - Shows only actionable work that needs a human next step.
   - No seeded noise, fake owner gaps, or irrelevant non-vaccination rows.

4. Protocol Adherence
   - Shows expected vs actual only after due work exists.
   - Gap labels must be business-readable, not raw internal keys.

5. Workflows
   - Shows the chain for the same generated vaccination work:
     config -> obligation -> drive/SOP -> proof -> verification -> close.
   - Every link must point back to the same goat/shed/protocol version.

6. Control Tower
   - Shows process breaches only when real:
     missed deadline, proof failure, verification failure, repeated overdue,
     stock/cold-chain issue, or configured escalation.

7. Execution/proof screen
   - Use only a backed working execution path.
   - Do not show fake upload fields or fake action buttons as working.

### 8. Execution and proof path

- [x] Open a generated vaccination task/drive.
- [x] SOP context shows goat/shed/vaccine/dose from published config.
- [x] Operator records dose/proof only through a backed working path.
- [x] Proof required state, proof submitted state, verification pending state,
  accepted state, rejected/rework state, and missed/overdue state are covered.
- [ ] If a control is not implemented in web V1, it is hidden or clearly marked
  unavailable with the correct reason.

### 9. Demo handover output

- [x] Provide final demo runbook: where to start, which login/user, which URL,
  which seeded park/shed/goats, what to click, what should appear, and what not
  to demo.
- [x] Provide the exact screen order after config publish: Config -> Vaccination
  page -> Action Center -> Protocol Adherence -> Workflows -> Control Tower ->
  Execution/proof.
- [x] Provide screenshot folder paths for the validated demo flow.
- [x] Provide exact command/script to reset and seed the demo state.
- [x] Provide exact command/script to run the vaccination V1 E2E proof.
- [x] Provide a short known-limitations section that does not over-claim route/resource optimization.
- [x] Provide the drive-batching algorithm in plain language and the exact UI
  changes needed to support it.

Handover artifact: `docs/phc-vaccination/V1-DEMO-HANDOVER.md`.

## E2E Checklist After Fixes

These checks are in addition to the clean-slate E2E path above.

### Control-by-control vaccination QA gate

- [x] Current implemented shell/sidebar route links, Vaccination, Action Center,
  Protocol Adherence, Workflows, Config, and SOP Library controls/drawers/
  interlinks are clicked in live E2E.
- [x] Current sidebar links for Counts -> Herd Register and Procurement ->
  Source Entry are clicked in live E2E.
- [x] Every SOP-builder authoring button/control needed for V1 demo is clicked
  in E2E.
- [x] Every Config-builder authoring button/control needed for V1 demo is
  clicked in E2E.
- [x] Every V1 demo button either completes a backed action or shows a clear disabled
  reason.
- [x] Every V1 demo button success state is visible without reading browser logs.
- [ ] Every button failure state shows the exact field/section/backend reason.
- [x] Every V1 demo row in each vaccination list opens the correct row context, not a
  random Action Center or Workflow record.
- [x] Every V1 demo side drawer opens, closes, preserves background state, and does not
  create page scroll confusion.
- [x] Every V1 demo side drawer action is backed, or hidden/disabled with a clear reason.
- [x] Every V1 demo modal save action persists after full page refresh.
- [ ] Every modal cancel/close action preserves saved drafts and discards only
  unsaved changes with clear confirmation where needed.
- [x] Every V1 demo draft/publish flow proves list refresh, reopen, status label, audit
  event, and backend persistence.
- [x] Every V1 demo form field in SOP builder and vaccination config is filled, saved,
  reopened, edited, and validated.
- [ ] Every dropdown option list is tested for correct domain filtering,
  readable labels, no raw UUID-first labels, no unrelated options, and no empty
  option bug.
- [ ] Every checkbox and segmented control is tested for saved state and
  business effect where it claims to change generation/preview behavior.
- [ ] Every search, filter, and pagination control preserves the selected scope,
  page, row, drawer state, and URL.
- [x] Every V1 demo link between Config, SOP Library, Vaccination, Action Center,
  Protocol Adherence, Workflows, Control Tower, Goat Passport, and shed
  execution detail lands on the intended record and keeps the same
  goat/shed/protocol context.
- [x] Every V1 demo deep link from Herd Register and Procurement Source Entry into
  vaccination-related context lands on the intended goat/load/work record, not
  only the sidebar route.
- [ ] Every list has an empty state, loaded state, error state, and no-results
  state that is readable and non-overlapping.
- [x] Every tested screen is captured in screenshots at desktop and narrow
  viewport.
- [x] The E2E run fails immediately on console errors, Next runtime errors,
  backend_down screens, hydration errors, text overlap, or invisible clickable
  controls.

### SOP draft lifecycle

- [x] Open SOP Library.
- [x] Click New SOP.
- [x] Enter SOP name Vaccination session test.
- [x] Confirm domain is PHC / Vaccination.
- [x] Select trigger Form.
- [x] Add all supported field types without UI overlap.
- [x] Click Save draft.
- [x] Verify success feedback is shown.
- [x] Verify draft appears in SOP Library.
- [ ] Verify Save draft failure shows a visible error and keeps user input.
- [x] Reopen the draft.
- [x] Verify every saved field is still present.
- [x] Edit one field and save again.
- [x] Verify the edit persists after reopening.

### Field type validation

- [x] Text field can be saved and reopened. Required-value execution blocking is
  missing during execution dry-run.
- [x] Number field can be saved and reopened. Unit/min/max validation is
  configured.
- [x] Yes/no field can be saved and reopened.
- [ ] Select field cannot save/publish with zero options.
- [x] Select field can save/publish after options are added.
- [x] Select options can be added, edited, removed, reordered, saved, and
  reopened.
- [ ] Multiselect field cannot save/publish with zero options.
- [x] Multiselect field can save/publish after options are added.
- [x] Multiselect options can be added, edited, removed, reordered, saved, and
  reopened.
- [x] Goat scan/RFID field can be saved, reopened, and shows how the eligible
  goat list is sourced.
- [x] Shed picker field can be saved, reopened, and shows how shed scope is
  sourced.
- [x] Vaccine batch picker field can be saved, reopened, and shows FEFO/lot
  source.
- [x] Medicine picker field can be saved, reopened, and shows inventory source.
- [x] Photo proof field can be saved, reopened, and binds to proof policy.
- [x] Video proof field can be saved, reopened, and binds to proof policy.

### Vaccination modal coverage

- [x] SOP builder modal supports save draft, reopen, dry-run, publish, close,
  validation errors, success states, and failure states.
- [x] Vaccination config modal supports save draft, reopen, preview impact,
  publish, close, validation errors, success states, and failure states.
- [x] Vaccination row/detail drawer supports open, close, drilldown, disabled
  action explanation, and no fake working controls.
- [x] Action Center drawer supports open, close, drilldown, disabled action
  explanation, and no fake working controls.
- [x] Workflow record page supports back/list navigation and no broken row
  actions.
- [x] Protocol Adherence row/detail flow supports open, close, drilldown, and no
  raw internal gap keys.
- [ ] Pagination on every vaccination slice list preserves filters and does not
  open the wrong row/drawer.
- [ ] Search/filter controls on every vaccination slice list either work or show
  a clear unavailable reason.
- [x] Every V1 demo modal/drawer is checked at desktop and narrow viewport with
  screenshots.

### Conditional rule validation

- [ ] Step 1 does not show previous-step-dependent rules.
- [ ] Step 2+ can show previous-step-dependent rules.
- [x] Conditional show rules persist after save/reopen.
- [x] Conditional answer rules persist after save/reopen.
- [ ] Invalid conditional rules are blocked with a clear error.

### Publish readiness

- [x] Dry-run explains missing requirements before publish.
- [x] Publish is disabled with an exact reason when required data is missing.
- [ ] Publish disabled reason links or scrolls to the exact broken field.
- [ ] Publish disabled reason covers unsaved draft, missing SOP name, invalid
  conditional rule, select/multiselect missing options, incomplete proof policy,
  and backend validation failure.
- [x] Publish succeeds for a complete valid SOP.
- [x] Publish success feedback is visible.
- [x] Published SOP appears in SOP Library with version/status.
- [x] Published SOP appears in vaccination config SOP dropdown.
- [x] Published SOP appears with a human-readable label, not a raw UUID.
- [ ] Non-vaccination SOPs do not appear in vaccination config SOP dropdown.

### Visual and click QA

- [x] No modal text overlaps at desktop viewport in the V1 demo smoke.
- [x] No modal text overlaps at narrow viewport in the V1 demo smoke.
- [x] Error text does not overlap controls in the V1 demo smoke.
- [x] Success/error toasts do not cover sticky footer actions in the V1 demo smoke.
- [x] Sticky footer does not cover fields in the V1 demo smoke.
- [x] Dropdowns do not overflow outside the modal in the V1 demo smoke.
- [x] Close/cancel behavior is consistent and does not lose saved drafts in the V1 demo smoke.
- [x] Every visible V1 demo clickable control either works or shows a clear disabled
  reason.

### Vaccination config form E2E

- [x] Open Config -> Vaccination.
- [x] Click New draft rule.
- [ ] Capture screenshot of the empty New draft rule modal.
- [ ] Capture screenshot after filling a valid vaccination matrix row.
- [x] Every visible V1 demo field can be focused without layout shift or overlap.
- [ ] Protocol code validates required/format/duplicate cases.
- [ ] Protocol code duplicate error is shown next to the field and blocks save /
  publish.
- [ ] Protocol code is auto-suggested from protocol name when blank.
- [x] Protocol code can be edited before publish.
- [x] Published protocol code is stable and not casually editable in-place.
- [x] Protocol name validates required/empty cases in V1 demo coverage.
- [x] Scope validates tenant/park selection in V1 demo coverage.
- [x] Effective-from validates required date and invalid date in V1 demo coverage.
- [x] SOP dropdown lists only published vaccination SOPs.
- [x] SOP dropdown hides draft SOPs.
- [x] SOP dropdown hides non-vaccination SOPs.
- [x] SOP dropdown uses human-readable labels.
- [x] Vaccine code validates required/empty/format cases in V1 demo coverage.
- [x] Vaccine name validates required/empty cases in V1 demo coverage.
- [x] Vaccine type validates allowed values and unknown-review-needed behavior in V1 demo coverage.
- [x] Inventory item id accepts optional value and preserves it after save.
- [x] Manufacturer/source detail accepts and preserves text.
- [x] Disease/protection target accepts and preserves text.
- [x] Compatibility group/family accepts and preserves text.
- [x] Animal stage selector saves, reloads, and affects preview eligibility.
- [x] Sex selector saves, reloads, and affects preview eligibility.
- [x] Breed selector saves, reloads, and affects preview eligibility.
- [ ] Lifecycle selector saves, reloads, and affects preview eligibility.
- [ ] Reproductive selector saves, reloads, and affects preview eligibility.
- [ ] Pregnant exclusion checkbox saves and reloads.
- [ ] Lactating exclusion checkbox saves and reloads.
- [ ] Sick defer checkbox saves and reloads.
- [ ] Treatment defer checkbox saves and reloads.
- [ ] ICU defer checkbox saves and reloads.
- [ ] Quarantine defer checkbox saves and reloads.
- [ ] Pregnant exclusion changes Preview Impact when a matching pregnant goat is
  in the test data.
- [ ] Lactating exclusion changes Preview Impact when a matching lactating goat
  is in the test data.
- [ ] Sick defer changes generated work to deferred when a matching sick goat is
  in the test data.
- [ ] Treatment defer changes generated work to deferred when a matching
  treatment goat is in the test data.
- [ ] ICU defer changes generated work to deferred when a matching ICU goat is in
  the test data.
- [ ] Quarantine defer changes generated work to deferred when a matching
  quarantined goat is in the test data.
- [ ] Booster/catch-up/missed-dose policy saves and reloads.
- [ ] Stock/lot requirement saves and reloads.
- [ ] Escalation policy saves and reloads.
- [ ] Source type validates publishability.
- [ ] Source reference validates required when source-backed publish is needed.
- [ ] Reviewed-by and approved-by validation is visible.
- [ ] Schedule builder can add a dose row.
- [ ] Schedule builder can edit dose/sequence.
- [ ] Schedule builder can edit trigger.
- [ ] Schedule builder can edit offset days.
- [ ] Schedule builder can edit due window days.
- [ ] Schedule builder can edit dose amount.
- [ ] Schedule builder can edit unit.
- [ ] Schedule builder can edit route/site.
- [ ] Schedule builder blocks invalid missing or negative values.
- [x] Schedule builder supports multi-dose rows and preserves order.
- [x] Schedule builder row 1 can create a primary dose from birth age.
- [x] Schedule builder row 2 can create a booster/next dose from previous
  completion when supported.
- [ ] Schedule builder rejects unsupported trigger/repeat policies with visible
  review-needed errors.
- [x] Schedule builder preview shows which due date comes from trigger + offset
  and which latest-safe date comes from due window.
- [x] Schedule builder preview changes when offset days are edited.
- [x] Schedule builder preview changes when due window days are edited.
- [x] Schedule builder route/site and dose amount flow into SOP/execution proof
  context if that behavior is in V1 scope; otherwise the UI must not imply it is
  enforced.
- [x] Save draft persists every field above.
- [x] Reopen draft shows every field above unchanged.
- [x] Preview impact reports clear success or exact validation errors.
- [x] Preview impact shows eligible goats count.
- [x] Preview impact shows obligations/cycle count.
- [x] Preview impact shows batches/SOP task count.
- [x] Preview impact shows doses required.
- [ ] Preview impact shows stock availability when inventory item is linked.
- [ ] Preview impact shows a clear warning when no vaccine inventory item is set.
- [x] Preview impact does not mutate real obligations or tasks.
- [ ] Capture screenshot of Preview impact before inventory item is set.
- [ ] Capture screenshot of Preview impact after a valid inventory item is set.
- [x] Publish is disabled with exact field-level reasons until valid.
- [x] Publish succeeds only when every required config field is valid.
- [x] Published config appears in the Config list with correct status/version.
- [x] Generated rule JSON preview matches the visible form values.
- [x] After publish, generated due work appears on the Vaccination page for
  eligible goats.
- [x] After publish, non-eligible goats do not get due work.
- [x] After publish, deferred goats appear as deferred/recheck-needed rather than
  due or missing.
- [x] After publish, workflow rows and adherence rows point back to the same
  protocol/config version.
- [x] Creating multiple matrix rows for different vaccine/stage combinations is
  covered by E2E (`NUANCE-RULES-20260701-V1-MATRIX-R2` loads ET+TT, PPR, Goat Pox,
  FMD, and HS as separate published config rows).
- [x] Matrix/grid entry mode allows adding ET/K1 and PPR/K2-style rows without
  repeating shared header/source/SOP/schedule fields.
- [x] Matrix/grid entry mode validates each row independently; row-indexed
  failures are returned by the batch save/publish action.
- [x] Matrix/grid entry mode persists all rows as separate protocol versions and
  reloads them in the Config list/drawer.
- [x] Matrix/grid entry mode shows row-level controls/errors in the modal without
  text overlap in the tested desktop viewport.
- [ ] Escalation policy is configured with structured role/action fields, not
  free text.
- [ ] Escalation preview shows whether it creates Action Center item, Control
  Tower breach, notification, or review queue.
- [ ] Escalation publish is blocked with clear reason when required operators or
  role mappings are missing.
- [ ] Escalation E2E proves a missed/overdue obligation appears in the expected
  visible surface.
- [ ] Replace developer copy `computed from schedule[]` with user-facing Preview
  Impact wording and verify it in screenshots.

## Demo Rule

You can demo the V1 vaccination SOP builder and Config authoring flow covered by
`NUANCE-RULES-20260701-V1-MATRIX-R2`.

Do not demo or claim route/resource optimization, business-facing user/shed
admin setup UI, full browser-negative coverage, or million-goat permutation
proof. Do demo the V1 source-backed rule matrix, including compatibility
spacing, only when the Nuance smoke evidence below is fresh.

## Handover: V1 Batching Boundary And Later Planner

The handover must clearly separate V1 due generation plus basic shed-drive
batching from the later route/resource optimizer. Basic Calendar de-duplication
and source compatibility spacing are V1.

### V1 due generation + basic shed-drive batching

- Matrix/config decides which goats are eligible.
- Schedule rows decide due date, latest date, dose amount, unit, and route/site.
- Goat creation/import/backfill/stage/health/location changes trigger due-work
  generation or recheck.
- This creates per-goat due vaccination work.
- The V1 sweeper groups compatible same-rule due work by shed/cohort into
  `obligation_batches` for execution.
- Calendar uses the batch as the active operations item: one shed-drive row with
  target count. Batched per-goat due rows remain available to Passport,
  Protocol Adherence, Vaccination detail, and audit, but must not flood Calendar
  as separate active events.

### Later route/resource optimizer

- Plan across sheds, routes, workers, stock/cold-chain capacity, due windows,
  and fairness rules.
- Consume the V1-authored safe vaccine groups; do not invent new medical
  compatibility rules.
- Use batching rules to decide whether to run now, wait, split, defer, or
  escalate.
- Score safe candidate plans using urgency, latest-safe date, goat count, shed
  route, stock/cold-chain, worker capacity, and small-shed fairness.
- Assign operator, verifier, stock/lot, route, and proof policy.
- Replan only the affected goat/shed/vaccine cohort when a goat dies, is sold,
  shifts shed, becomes sick, becomes pregnant/lactating, enters/exits
  quarantine/ICU, proof fails, stock fails, or cold-chain fails.

### UI changes needed for later route/resource optimizer

- Add a drive-planning rule section separate from per-goat vaccine config.
- Add batching thresholds:
  minimum goats per shed drive, max safe wait days, force micro-drive rule, and
  small-shed fairness rule.
- Add route/resource controls:
  operator capacity, cold-chain duration, stock lot/expiry, and verifier
  availability.
- Add execution-day reconciliation:
  expected goats, missing goats, extra goats, shifted goats, sick/deferred goats,
  proof pending, verification reject, rework, and cancellation.
- Add clear generated-plan preview before creating drive work.

## Test Guardrails

- Run one focused E2E path at a time: setup, SOP builder, config/matrix, publish,
  generation, operational surfaces, execution/proof.
- Keep the bug-bash goal bounded to the Vaccination V1 demo path. Do not pull
  in Feed Direction, full route/resource optimization, full procurement operations,
  or unrelated admin-web redesign.
- Target a 4-5 hour closure loop:
  setup proof, SOP builder, config/matrix, generation/backfill, operational
  surfaces, then demo runbook.
- Do not keep rerunning the full browser smoke when a focused blocker is already
  known.
- If the same failure appears twice, stop the loop, record the failing URL,
  screenshot, console/server error, and exact expected behavior.
- Fix that blocker, then rerun only the focused failing path before broad smoke.
- A demo-ready claim requires screenshots and command output for the full
  clean-slate path, not only unit tests or seeded UI screenshots.
