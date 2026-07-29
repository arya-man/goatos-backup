# Goat OS Mobile — Feature Action Flows and Delivery Roadmap

**Status:** Draft for product, operations, engineering, and SOP review
**Written:** Monday, 27 July 2026
**Primary review gate:** Thursday, 30 July 2026
**Full slice complete:** Thursday, 6 August 2026

**Source of record for current behaviour:**
[Mobile workflow review workbook](https://docs.google.com/spreadsheets/d/1qJRQK2DDy2C4y359CHVBh1OhWBk0K7FJMMVvXCUqr0A/edit?gid=1398447374#gid=1398447374)
— read on 27 July 2026 (10,891 action rows across 9 workflow types).

---

## 1. What this document is

### Accepted implementation correction — 28 July 2026

The following maintainer decision supersedes older "upload" wording and any
operator-visible approval/verification step in this roadmap:

- Operator actions advance sequentially within their lane: only the first
  incomplete action is enabled; its completion enables the next.
- Birth, Death, Shifting, Feed Distribution, and Feed Packing proofs are captured
  with the live in-app camera. Gallery/import controls are not shown. Automatic
  background upload after capture remains part of the proof pipeline.
- Vaccination is exempt and keeps its current gallery picker.
- Death has exactly two operator steps: Record death video → Record post-mortem
  video. Admin approval and media verification stay on their authorized surfaces,
  not in the operator action list.
- Feed Distribution is ordered: record feed video → enable water photo/video →
  Submit. Shifting and Feed Packing each remain a one-proof camera flow.

Canonical state semantics remain in `docs/decisions/birth-death-workflows.md`,
`docs/decisions/shifting-verification.md`, and
`docs/decisions/feed-distribution-verification.md`.

The review outcome is:

- **Change** three workflows already in the app — Birth, Death, Shifting.
- **Build** four new pages — Milk Preparation, Health, Feed Transport,
  Colostrum Feeding.

Every one of these is already running in production today, but in a
**Slack + Google Sheets automation**, not in Goat OS. The workbook is not a
wishlist — it is the live system, with real operators, real volumes, and a real
workflow engine behind it. This document reads that engine, states the exact
action flow for each task, maps it onto what Goat OS has today, and gives a
delivery plan.

The single most important structural finding:

> The legacy system is not seven separate features. It is **one generic
> task-and-action engine** driven by configuration, with seven configurations
> loaded into it. Goat OS has no equivalent engine. Building seven bespoke
> screens would be the wrong answer and would not survive the eighth workflow.

---

## 2. The legacy engine — the model to absorb

Three tables define everything.

| Tab | Grain | Meaning |
|---|---|---|
| `Workflow Type` | 13 rows, `RT-001`…`RT-013` | The **template**: what actions a task type has |
| `Workflow` | one row per real event | The **task instance**: `WF-0007`, a shifting on 11-03-2026 at CPT |
| `Action` | one row per step | The **ordered actions** of that task: `ACT-000083` "Upload Shifting Video" |

So "each task has actions" is literally the production schema:
`Workflow Type` → `Workflow` → `Action[]`.

### 2.1 Action-type vocabulary (the DSL)

Eight action types carry all 10,891 production rows. This is the vocabulary the
Goat OS engine must support:

| Action type | Volume | Operator sees | Completion |
|---|---:|---|---|
| `ACTION` | 4,170 | Instruction + upload a video/photo | Proof uploaded |
| `QUESTION` | 2,710 | A question, free/yes-no answer | Answer recorded |
| `MODAL_FORM` | 1,436 | A multi-field structured form | Form submitted |
| `ACTION_AUTO_VIDEO` | 928 | Auto-scheduled step that still needs video | Proof uploaded |
| `ACTION_AUTO` | 693 | Nothing — system completes it | Auto-completed |
| `TRIGGER_EVENT` | 515 | Nothing — spawns another workflow | Child workflow created |
| `APPROVAL` | 255 | Approver sees request, approves/rejects | Verdict recorded |
| `QUESTION_SELECT` | 184 | Pick from a configured option list | Selection recorded |

### 2.2 Scheduling rules

Actions are not all due at once. Real rules in use:

- `EVENT+1H`, `EVENT+2D`, `EVENT+9D` — offset from the triggering event
- `EVENT+2D_07:00` — offset **and** a fixed clock time
- `NEXT_DAY_08:00`, `NEXT_DAY_12:00`, `NEXT_DAY_16:00`, `NEXT_DAY_21:00` —
  next-day session slots (milk feeding)
- `FUNC_ORS_2` — a named function rule (custom logic)

All times are farm-local. Under the Goat OS time contract these resolve against
`Asia/Kolkata` business days.

### 2.3 Conditions

Actions are conditionally present, evaluated at creation or at runtime:

- Creation-time on task fields — `{PRIORITY} = High`, `{PRIORITY} = Low`
- Creation-time on counters — `{REFUSAL_COUNT}>=1`, `{REFUSAL_COUNT}=3`
- Runtime on an earlier answer — `RUNTIMECOND:{GOAT_MILK_USED}=Yes`
- Compound — `{EMERGENCY}=yes,{SUCKLE}=yes` and `{REFUSAL_COUNT}=3,{SLOSH}=no`

The last group matters: **an answer to action N decides whether action N+1
exists.** A static checklist screen cannot express this.

### 2.4 Per-action state

Every action row carries: `Scheduled Date/Time`, `Actual Date/Time`, `Status`,
`Response`, `Video Link`, `Details`, `Uploaded By`, `Verified By`,
`Verification Status`, `Remarks`, `Reminder Sent`.

Live status distribution: `Completed` 8,631 · `Scheduled` 1,704 · `Posted` 322 ·
`Skipped` 135 · `Cancelled` 99.

Note `Skipped` and `Cancelled` are first-class. Cancelling a parent workflow
cancels its still-open actions but keeps the completed ones — history is never
rewritten.

---

## 3. Where Goat OS stands today

| Feature | Legacy volume | Backend today | Android today | Verdict |
|---|---:|---|---|---|
| Birth | 4,033 actions | `counts` | `BirthDeathScreen` | **Rework** — single form, no action chain |
| Milk Preparation | 3,848 actions | none | none | **Build** |
| Shifting | 2,581 actions | `counts` + `verification` | `ShiftingScreen`, `ShiftingExecuteScreen`, `ShiftingPendingScreen` | **Rework** — closest to target |
| Milk Refusal SOP (Health) | 205 actions | `health/` is an empty `.gitkeep` | none | **Build** |
| Death | 118 actions | `counts` | `BirthDeathScreen` | **Rework** |
| Feed Transport | 2 actions | `feed`, `feeddirection`, `inventory` | `FeedDirectionScreen`, `FeedPackingScreen` | **Build** |
| Colostrum Feeding | inside Birth | none | none | **Build** |

Two facts that set the whole plan:

1. **There is no task/action engine.** `backend/internal/tasks/` contains only
   `doc.go`. `backend/internal/forms/domain/` is empty. `backend/internal/health/`
   is a `.gitkeep`. The engine has to be built, and it is the critical path for
   six of the seven features.
2. **Shifting already has the right action shape.** Raised work appears in Actions; Park Head
   approval and operator completion are independent gates; the second gate relocates. Mandatory
   video verification is post-task evidence review and cannot roll back census truth.

---

## 4. Feature action flows

Each flow below is the **actual production sequence** read from the workbook,
with the Goat OS delta called out.

### 4.1 Birth — rework

Highest-volume workflow in the system (4,033 actions). It is not one form; it is
**three parallel action tracks** plus a colostrum series.

**Trigger:** delivery recorded → workflow created, entity = Goat, multi-entity
thread (one mother, N kids).

**Track A — Mother** (146 occurrences each):

All six implemented mother steps require one live-camera video. Question steps
submit the selected response and that one video together. Mother's Medicine is
one task and one video for the complete four-medicine administration.

| # | Type | Action | Schedule | Details |
|---|---|---|---|---|
| 1 | `QUESTION` | Are there any babies still inside? | immediate | |
| 2 | `QUESTION` | Is the mother licking her babies? | immediate | |
| 3 | `ACTION` | Mother's Medicine | immediate | ONE task: Chocolate Injection 1.5 ml SQ; Meloxicam Paracetamol 4 ml IM; Exapar 20 ml; Glucoboost 100 ml + 150 g concentrate. One video covers the complete medicine task. |
| 4 | `ACTION` | Give ORS water | immediate | |
| 5 | `QUESTION` | Is the mother eating? | immediate | |
| 6 | `ACTION` | Give ORS water (2nd) | `FUNC_ORS_2` | |
| 7 | `TRIGGER_EVENT` | Create Shifting Request | `EVENT+2D` | K0 → Mother shed |

**Track B — each Kid** (184 occurrences each):

| # | Type | Action | Schedule | Details |
|---|---|---|---|---|
| 1 | `QUESTION` | Is the kid clean? (If not, wipe with a towel.) | immediate | |
| 2 | `ACTION` | Iodine dipping of umbilical cord | immediate | |
| 3 | `QUESTION` | Are the front teeth outside the lower gum? | immediate | |
| 4 | `QUESTION` | Is the kid able to suck when you put a finger in its mouth? | immediate | |
| 5 | `QUESTION` | 1st Colostrum | immediate | If suck reflex absent, collect colostrum with a 10 ml syringe, give ≥100 ml. Video must first show the udder contains milk. Follow with 100 ml milk. |
| 6 | `QUESTION_SELECT` | Take weight of kid | immediate | |
| 7 | `QUESTION` | Is the kid standing? | `EVENT+1H` | |
| 8 | `ACTION` | Tag the kid | `EVENT+2D_07:00` | |
| 9 | `TRIGGER_EVENT` | Create Shifting Request | `EVENT+2D` | K0 → K1 |
| 10 | `TRIGGER_EVENT` | Create Shifting Request | `EVENT+9D` | K1 → K2 |

**Track C — Abortion** (`RT-002`, separate type, 4 actions): babies still
inside? → Mother's Medicine → ORS water → is the mother eating? No kid track,
no colostrum.

**Colostrum series** — see §4.7. It is generated from Birth but is its own page.

**Implemented form + workflow contract:** Birth opens on a live action list with
separate mother and per-kid tracks. One delivery submit creates the selected
`1`, `Twins`, or `Triplets` as distinct canonical children. Breed is visible and
required; the mother's permanent RFID is required/scannable and resolves to the
canonical mother goat ID. Each child receives a distinct farm-coded provisional
ID (`CBE-#####`/`CPT-#####`) and its own workflow. After server acceptance,
Android clears all entered values, returns to Birth, and refreshes those normal
workflow cards. It shows no pending-approval cards: web approval separately
controls only whether the already-created children enter herd counts. **Tag the
kid** promotes each permanent RFID and retires its provisional ID. The removed
shifting trigger rows remain owned by Shifting, not Birth.

**Completion effects:** submission creates all live children and their
`goat_births` rows immediately; web approval atomically activates count
eligibility for the complete litter and never creates a goat. After all actions
for one child finish, its birth evidence is sent to Verify. Dead offspring
creates no animal and no vaccination work;
`Create Shifting Request` events must go through the real shifting producer, not
a private path.

---

### 4.2 Death — rework

Smallest action set, and the review question is already answered by the data.

| # | Type | Action | Details |
|---|---|---|---|
| 1 | `ACTION` | Upload death video | Please upload video with the timestamp |
| 2 | `ACTION` | Upload post mortem video | Please upload video with the timestamp |

**Post-mortem is unconditional.** Production shows 59 death videos and 59
post-mortem videos — exactly 1:1, no condition column, no cause-based branch.
The earlier draft of this document listed "is post-mortem always required?" as
an open decision; the workbook answers it. Treat it as *confirmed by evidence,
pending clinical sign-off* rather than as an open question.

**What changes vs the app today:** two mandatory sequential video proofs with
independent status, rather than one combined submit. Death approval continues to
apply immediately (unchanged by the 2026-07-26 shifting decision).

**Completion effects:** close the animal lifecycle, update canonical counts,
cancel or rescope open work while preserving completed and in-progress history,
reject a second death for the same animal.

---

### 4.3 Shifting — rework

Already the closest to target. Two variants, and the branch is **priority**.

**Variant 1 — Shifting Request** (with authorisation, 255 in production):

| # | Type | Action | Condition |
|---|---|---|---|
| 1 | `APPROVAL` | Authorisation | always |
| 2 | `ACTION_AUTO` | Feed Direction | `{PRIORITY} = High` |
| 3 | `ACTION` | Feed Quantity | `{PRIORITY} = High` |
| 4 | `ACTION` | Feed Consumption | `{PRIORITY} = High` |
| 5 | `MODAL_FORM` | Post-Shift Animal Count | `{PRIORITY} = High` |
| 6 | `ACTION` | Upload Shifting Video | `{PRIORITY} = Low` |

**Variant 2 — Shifting Direction** (no authorisation step; same six minus the
approval).

Production counts confirm the branch: Feed Direction / Feed Quantity / Feed
Consumption / Post-Shift Count all sit at 473, Upload Shifting Video at 434.

Proof text in use: Feed Quantity — *"Please upload a video showing the quantity
measured for feed."* Feed Consumption — *"Please upload a video showing the feed
being given to the animal."* Shifting Video — *"{SRC_SHED} → {DST_SHED}"*.

**What changes vs the app today:** the app models a shifting as one mandatory
completion video followed by evidence review. The real workflow is a **conditional chain of up to six
actions**, where a high-priority move pulls in a three-step feed sequence and a
post-shift count form. `ShiftingExecuteScreen` must render the action list the
backend gives it instead of a fixed single-proof screen.

**What must NOT change** — already-ratified Goat OS rules:

- Goats never move between parks; leaving a park is a terminal transfer/sale.
- Park Head approval and operator completion with mandatory video are independent;
  whichever arrives second atomically relocates the animals and changes the count.
- Verification is post-task evidence review. Approval marks evidence verified;
  rejection requests a re-shoot and never rolls back location or count.
- Missing proof → `422 proof_required`.
- Destination stage comes only from the destination shed's active
  `shed_profiles` row via `animal_stage_lookup`.
- Feed projection counts approved-but-unapplied movement intent, excludes
  completion-before-approval and `applied`, with no lead-day/priority branch.

Note the tension to resolve: legacy branches feed actions on `{PRIORITY} = High`,
while the confirmed Goat OS feed-projection rule explicitly says *"forget high
priority"* for **feed timing**. These are different things — projection timing vs
which actions appear — but the overlap needs an explicit owner decision so the
two rules do not drift into each other.

---

### 4.4 Milk Preparation — build

Second-highest volume (3,848 actions; 218 preparation workflows × 4 feeding
sessions). Entity is a **Shed Tag**, not a goat.

**Stage 1 — Preparation (session 0), same day:**

| # | Type | Action | Condition | Proof instruction |
|---|---|---|---|---|
| 1 | `ACTION_AUTO` | Milk Direction | — | auto |
| 2 | `MODAL_FORM` | Goat Milking Report | — | form |
| 3 | `ACTION_AUTO_VIDEO` | Quantity of goat milk to be boiled (litres) | `{GOAT_MILK_USED}=Yes` | video showing measured quantity |
| 4 | `ACTION_AUTO_VIDEO` | Record boiling temperature | `{GOAT_MILK_USED}=Yes` | video of milk boiled **at 72 °C for 1 minute** |
| 5 | `ACTION_AUTO_VIDEO` | Record temperature after milk cools down | `{GOAT_MILK_USED}=Yes` | video showing milk after cooling |
| 6 | `ACTION_AUTO_VIDEO` | Quantity of UHT milk | — | show number of tetrapaks used |
| 7 | `ACTION_AUTO_VIDEO` | Add `{CITRIC_ACID_GRAMS}` g citric acid (`5.5 g × {TOTAL_LITRES}` L), mix, store outside | — | video showing measured total milk |

Action 7 is a **computed instruction** — the text itself is generated
(`Add 594 gms Citric Acid (5.5 gms × 108 litres)`). The app must render a
backend-computed string, never compute it client-side.

**Stage 2 — Feeding sessions 1–4, next day at 08:00 / 12:00 / 16:00 / 21:00.**
Each session repeats the same three actions:

| # | Type | Action | Schedule |
|---|---|---|---|
| 1 | `ACTION` | Show clean bottles | `NEXT_DAY_08:00` (then 12:00 / 16:00 / 21:00) |
| 2 | `ACTION` | Mix the milk and fill required bottles | same session |
| 3 | `MODAL_FORM` | Milk Feeding Report | same session |

872 of each = 218 workflows × 4 sessions. Exact.

**Quantity config (authored, not hardcoded):**

| Shed tag | ml per session | Sessions/day |
|---|---:|---:|
| K1 | 200 | 4 |
| K2 | 300 | 4 |
| K3 | 200 | 2 |
| Citric acid | 5.5 g per litre | — |

**Milk Feeding Report** is a large structured form, per shed tag (K0, K1, Kids
ICU, K2), capturing per attempt: count, "how many did not drink cow milk", udder
milk count and refusals, ORS count and refusals, plus number of kids crossing
weaning weight with IDs.

**Refusal linkage:** the report feeds the Milk Refusal SOP —
`Carry Forward Days = 1`, `Carry Forward Threshold = 3`, SOP workflow `RT-012`.
Three refusals carried across a day escalates into the Health flow (§4.5).

**SOP gate:** 72 °C for 1 minute, 5.5 g/L citric acid, and the K1/K2/K3 volumes
are read from the live workbook. They are **operationally in force today** but
must be published into Goat OS authored config by the SOP owner before they
become app behaviour — not hardcoded.

---

### 4.5 Health — build

The workbook's clinical surface is the **Milk Refusal SOP** (`RT-012`, 205
actions), plus `Treatment` (`RT-003`, defined but no template) and
`Medicine Procurement` (`RT-013`). Milk Refusal is the one with a real,
detailed, branching protocol — and it is the strongest test of the engine,
because **every branch depends on a previous answer**.

**Trigger:** refusal count reaches threshold from the Milk Feeding Report.

**Triage — Normal vs Emergency:**

| Type | Action | Condition | Detail |
|---|---|---|---|
| `QUESTION` | Check rectal temperature | `{REFUSAL_COUNT}>=1` | Enter °F. **Below 100 °F → Emergency Protocol** |
| `QUESTION` | Is the kid standing? | `{REFUSAL_COUNT}>=1` | **Not standing → Emergency Protocol** |

**Normal protocol — escalates by refusal count:**

| Refusal | Actions |
|---|---|
| = 1 | Offer ORS 300 ml bottle (*"Present gently. If kid refuses, walk away. One missed feed is not a crisis."*) · Did kid drink ORS? (note ml) · Any diarrhea or nasal discharge? |
| ≥ 2 | Give RL 200 ml SQ split across 2 shoulders, 100 ml each, upload video |
| = 2 | Offer ORS 300 ml · Mouth check for orf lesions/ulcers/redness · Left flank bloating · Perineum diarrhea soiling |
| = 3 | RL 200 ml SQ, wait 20 min · Slosh test (bounce left flank, sloshing sound?) · **if slosh = no** force feed MILK full volume, 5–10 ml per squeeze over 5–7 min · **if slosh = yes** force feed ORS 300 ml |
| = 4 | RL 200 ml SQ, wait 20 min · Force feed MILK full volume **regardless of slosh — non-negotiable** · Move to hospital pen, heat source, temp every 3 h overnight, supervisor must see this kid |

**Emergency protocol** (`{EMERGENCY}=yes`):

1. IP Dextrose 20% at 10 ml/kg — warm to 39 °C first (syringe in warm water
   1 min), inject 1 cm beside navel toward tail, 20G 1-inch needle.
   **Never give under the skin — causes tissue necrosis.**
2. Give RL 200 ml SQ — separate site from the IP injection, split across two
   shoulders.
3. Warm the kid — heat lamp or warming box, warm water bottles wrapped in cloth,
   never direct on skin, check temp every 15 min, target above 98.6 °F, warm
   gradually over 30–60 min.
4. Suckle reflex — stroke corner of mouth. **Only proceed to feeding when Yes**;
   if No, continue warming and recheck every 15 min.
5. Force feed warm milk slowly (`{EMERGENCY}=yes,{SUCKLE}=yes`) — only when
   upright **and** suckle reflex present; take 10 min for full volume.
6. Move to hospital pen — do not return to general pen, monitor temp every
   2–3 h, offer milk at every regular session.

**Discharge criteria** (`{HOSPITAL}=yes`): drinking voluntarily · temp above
101 °F without external heat for 24 h · active, mobile, and alert.

**Design consequence:** this flow **must not** be implemented as a static
checklist. `{EMERGENCY}`, `{SUCKLE}`, `{SLOSH}`, `{HOSPITAL}` are all runtime
values produced by answering an earlier action. The engine needs conditional
next-action evaluation, and the app must render whatever the backend says is
next. A wrong medical action here is a P0 — this is the same class of rule as
the mandatory clinical defer set.

**Also in scope for the Health page** (thin, from the workbook):

- Medicine Procurement (`RT-013`): upload video of purchased medicines · upload
  purchase invoice.
- Treatment (`RT-003`): defined as a workflow type with **no template** — the
  action flow does not exist yet and must be authored by the clinical owner.

---

### 4.6 Feed Transport — build

Barely exercised in the legacy system — **2 action rows total**, one template
action:

| # | Type | Action | Details |
|---|---|---|---|
| 1 | `ACTION` | Feed Transport Video | Please upload video for Session 1 |

Entity is a Shed. Report title `🚚 Feed Transport - {SHED}`.

**Honest read:** there is almost no production signal here. The legacy flow is a
single video and nothing else. Two options:

- **(a) Port as-is** — one video action. Cheap, faithful, and shippable in
  hours. Recommended for the first cut.
- **(b) Design a real transport flow** — pickup confirmation, dispatched
  quantity, vehicle reference, receipt at destination, discrepancy handling.
  This is **new product design**, not a port, and needs an owner.

The earlier draft of this document assumed (b) without flagging that it is net-new.
Recommend shipping (a) by 30 July and scheduling (b) as a separate design item.

---

### 4.7 Colostrum Feeding — build

Currently embedded inside the Birth workflow as repeating `QUESTION` actions.
Production shows the real shape:

| Action | Count |
|---|---:|
| 1st Colostrum | 184 |
| 2nd Colostrum | 184 |
| 3rd Colostrum | 182 |
| 4th Colostrum | 178 |
| 5th–7th Colostrum | 120 each |
| 8th Colostrum | 108 |
| 9th Colostrum | 71 |
| 10th Colostrum | 21 |
| 11th Colostrum | 2 |
| Morning Colostrum | 64 |

**This is the key finding for this page.** Colostrum is not a fixed 4-session
checklist — it is an **open-ended per-kid series that decays**, running to 11
attempts in real cases, with a separate "Morning Colostrum". Any design assuming
a fixed session count is wrong against production.

**Session schedule** (`Delivery Template`, Colostrum Sessions block):

| Session | Time | Pre-notify |
|---|---|---|
| Session 1 | 07:00 | 15 min |
| Session 2 | 11:00 | 15 min |
| Session 3 | 15:00 | 15 min |
| Session 4 | 18:30 | 15 min |

**Ownership** (`Colostrum Config`) is by farm **and** session — five sessions
configured per farm, each with a named assignee. Ownership hands over
mid-day (at CPT, sessions 1–2 to one operator, 3–5 to another). The page must
resolve the owner from Goat OS workforce roster and role grants, not from the
Slack user IDs in the config tab.

**1st Colostrum instruction (verbatim, production):** *"If suck reflexes are not
active use 10 ml syringe for collecting the colostrum and give at least 100 ml.
At the beginning of the video, please verify whether the udder contains milk.
After giving colostrum give 100ml milk. Is Kid Taking Colostrum?"*

**Action flow per attempt:** show kid, mother, birth time, shed, owner, due
session window, attempt number → record method/quantity/time + required video →
record outcome → a refused or partial attempt must create the next attempt or
escalate, never silently close the obligation.

**SOP gate:** the ≥100 ml volume, the 10 ml syringe fallback, the session times,
and the escalation rule are live operational practice but need clinical
publication into Goat OS config.

---

## 5. Shared platform work — the critical path

Six of the seven features are blocked on the same thing. Build it once.

**Backend — workflow/action engine** (`backend/internal/tasks/`, currently empty):

- `workflow_types` (authored template), `workflows` (task instance),
  `workflow_actions` (ordered steps with per-action status, schedule, proof ref,
  response, verifier, remarks).
- Action-type support for all eight types in §2.1.
- Schedule-rule evaluation (`EVENT+{n}D`, `EVENT+{n}H`, `EVENT+{n}D_HH:MM`,
  `NEXT_DAY_HH:MM`, named functions) resolved on `Asia/Kolkata` business days.
- Condition evaluation at creation **and** at runtime against prior answers.
- `TRIGGER_EVENT` → publish through the registered producer for the child
  workflow (Birth → Shifting must use the real shifting producer).
- Cancel/skip semantics that close open actions and preserve completed history.

**Contracts:** OpenAPI for task list, task detail with ordered actions, action
submit, proof upload, approval verdict, rework, history. Labels, instruction
text, option lists, disabled reasons, and proof requirements are all
**backend-owned** — the app renders them.

**Domain events:** register producer, event, consumer, replay/DLQ, and E2E proof
in `context/architecture/domain-event-registry.json` for every new state
transition. Both ends, every time.

**Android:** Room entities + DAOs + Flows for task and action (offline-first
SSOT, `RefreshOnResume`, `SyncIconButton`, ~20-row keyset pages, Room migration
+ upgrade-crash test). One reusable action-renderer component set covering the
eight action types, so a new workflow is configuration rather than a new screen.

**Reuse what exists:** `verification` (approval gate), `proof` + `media` (video
upload), `sop` + `submissions` (proof grain), `counts` (birth/death/shifting
canonical state), `notification` (session pre-notify at 15 min).

**Guards that must pass:** `domain-event-architecture-guard`, `scale-guard`,
`mobile-guard`, `android-bounded-memory-guard`, `android-navigation-stack-guard`,
`room-migration-guard`, `idempotency-writes-guard`,
`atomic-readmodel-sync-guard`, `telemetry-guard`,
`leadership-assistant-coverage-guard`, `nav-composition-guard`.

---

## 6. Roadmap

### Feasibility statement

All seven features by Thursday 30 July is **not achievable**, and it would be
dishonest to plan it that way. The engine that six of them depend on does not
exist yet — `tasks/` is one file, `health/` is a `.gitkeep`, `forms/domain/` is
empty. The plan below is split into two gates so the 30 July review has
something real to look at.

### Sprint 1 — to Thursday 30 July: engine + the three reworks

| Day | Work |
|---|---|
| **Mon 27** | Freeze the action-type DSL, schedule-rule grammar, and condition grammar. Schema + migration for `workflow_types` / `workflows` / `workflow_actions`. OpenAPI contract for task + ordered actions. Confirm the priority-branch question in §4.3 with the maintainer. |
| **Tue 28** | Backend engine: action generation from template, schedule resolution on IST business days, condition evaluation, `TRIGGER_EVENT` wiring to real producers. Register domain events both ends. |
| **Wed 29** | Android: Room task/action entities + migration + upgrade-crash test, offline outbox for action submit, reusable renderers for `ACTION`, `QUESTION`, `QUESTION_SELECT`, `MODAL_FORM`, `APPROVAL`. |
| **Thu 30** | Rework Birth (mother/kid tracks + scheduled follow-ups), Death (two mandatory videos), Shifting (conditional six-action chain with independent approval/completion and post-task evidence review). Feed Transport as the single-video port (§4.6 option a). Run affected-component `make ci-local`, rendered device check, demo. |

**Gate-1 exit:** four workflows running on the generic engine, on a device, with
production-path E2E for replay, offline recovery, rework, and app restart.

### Sprint 2 — to Thursday 6 August: the clinical and milk pages

| Day | Work |
|---|---|
| **Fri 31** | Colostrum Feeding — open-ended attempt series, session windows, per-session owner from workforce roster, escalation on refusal. |
| **Mon 3** | Milk Preparation stage 1 — preparation chain, `ACTION_AUTO_VIDEO`, backend-computed citric-acid instruction, runtime condition on `{GOAT_MILK_USED}`. |
| **Tue 4** | Milk Preparation stage 2 — four next-day feeding sessions, Milk Feeding Report form, per-shed-tag quantity config, refusal carry-forward into the SOP. |
| **Wed 5** | Health — Milk Refusal SOP with full normal/emergency/discharge branching on `{EMERGENCY}` / `{SUCKLE}` / `{SLOSH}` / `{HOSPITAL}`. Medicine Procurement two-action flow. |
| **Thu 6** | Integration, full guard suite, rendered review of all seven, demo, gap list with owners and dates. |

### Parallel, not on the critical path

SOP/clinical publication of every value in §7. Engineering can build against
authored config with the values held as unpublished draft; the app must not ship
them hardcoded.

---

## 7. Open decisions and owners

| # | Decision | Evidence today | Owner | Needed by |
|---|---|---|---|---|
| 1 | Shifting: legacy branches feed actions on `{PRIORITY} = High`, but the confirmed Goat OS feed-projection rule says "forget high priority". Do both rules stand in their own scope? | Conflict between workbook and maintainer decision 2026-07-27 | Maintainer | **Mon 27** — blocks §4.3 |
| 2 | Feed Transport: port the single video as-is, or design a full dispatch/receipt/discrepancy flow? | 2 production rows, 1 template action | Product + Operations | **Mon 27** — changes Sprint 1 scope |
| 3 | Post-mortem video always mandatory? | 59:59 in production, no condition column — evidence says yes | Clinical | Contract freeze |
| 4 | Milk: publish 72 °C for 1 min, 5.5 g/L citric acid, K1 200 ml ×4 / K2 300 ml ×4 / K3 200 ml ×2 | In force operationally, unpublished in Goat OS | SOP/Clinical | Before Sprint 2 |
| 5 | Colostrum: publish ≥100 ml minimum, 10 ml syringe fallback, session times 07:00/11:00/15:00/18:30, max attempts, refusal escalation | Runs to 11 attempts in production | Clinical | Before Sprint 2 |
| 6 | Milk Refusal SOP: publish full drug protocol (IP Dextrose 20% 10 ml/kg, RL 200 ml SQ, temperature thresholds, slosh-test branch, discharge criteria) | Detailed in workbook, unpublished | Clinical | Before Sprint 2 |
| 7 | Treatment (`RT-003`): workflow type defined with **no template** — action flow does not exist | Empty in workbook | Clinical | Before Sprint 2 |
| 8 | Colostrum session ownership: Slack user IDs in `Colostrum Config` must map to Goat OS workforce members and role grants | Legacy IDs only | Operations | Before Sprint 2 |
| 9 | Role grants for operator, approver, verifier, clinician across all seven | — | Product + Operations | Contract freeze |

---

## 8. Definition of done

A workflow is done only when:

- Its ordered actions carry type, condition, schedule, required proof, owner,
  due time, status, and next action — all backend-owned.
- Conditional actions are evaluated from real prior answers, not rendered as a
  static checklist.
- Commands are idempotent, audited, event-backed, replay-safe, and offline-safe.
- Birth creates children at submission and activates counts at web approval;
  Death applies at approval; Shifting applies when approval and operator completion both exist.
- No unpublished medical value from the workbook has become hardcoded app
  behaviour.
- Room-backed UI distinguishes pending sync, pending approval/verification,
  rework, completed, cancelled, skipped, and blocked.
- A production-path E2E exercises the real producer, durable bus, consumer,
  canonical state, and read model — not a seeded readback.
- Required guards pass on the delivery candidate SHA, and the E2E report is
  published on the CI reports index.

---

## Appendix — production volumes (27 July 2026)

| Workflow | Action rows |
|---|---:|
| Birth | 4,033 |
| Milk Preparation | 3,848 |
| Shifting | 2,581 |
| Milk Refusal SOP | 205 |
| Death | 118 |
| Procurement Transit | 55 |
| Farming Report | 45 |
| Abortion | 4 |
| Feed Transport | 2 |
| **Total** | **10,891** |

Status: Completed 8,631 · Scheduled 1,704 · Posted 322 · Skipped 135 ·
Cancelled 99.

Workflow types not in this slice but present in the workbook and eventually
needing the same engine: Feed Packing (`RT-004`), Farming Report (`RT-009`),
Harvesting (`RT-010`), Procurement Transit (`RT-011`), plus the Cleaning
template (6 video actions, no workflow type assigned).
