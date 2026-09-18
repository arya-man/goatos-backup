# Pen routines: configurable recurring pen checks for the park head

Maintainer instruction, 2026-09-16 (chat): *"Park Head daily tasks: 1) each shed cleaned?
2) it should be configurable fully ... per shed, have filters like should we configure it daily
or weekly or monthly, or do we need to trigger it after any task ... for each park whom we need
to assign tasks, everything should be configurable ... what we expect from them: questions,
video / photo, or any clock-in and clock-out stating some person entered the shed."*

Status: ACCEPTED, built on `feat/pen-routines`. On screen the module is **Routines**; the word
is *pen*, never *shed* (`docs/decisions/pen-not-shed-vocabulary.md`).

## 2026-09-17 revision: assign by ROLE, general tasks, every N days (SUPERSEDES the parts named)

Maintainer instruction (chat, 2026-09-17): *"in web I need to configure whom the task is for --
preventive care director, breeding director, park heads, CXOs -- whether it is daily, weekly or
three days once, what the task is for, and for completion questions and answers or photo, video
or both."* Asked two questions, the maintainer answered: assign **by role**, not by named person;
a task is **either per pen or a general park task, chosen per task**. Also: *"we have two park
heads, check that."*

1. **ASSIGN BY ROLE (replaces the named-people list, `pen_routine_assignees` is gone).** A routine
   carries `assignee_roles`, one or more of the closed vocabulary below. Whoever holds that role
   for the routine's park gets the task; any one of them doing it is enough; a new holder of the
   role inherits it with no edit.

   | key | on screen |
   |---|---|
   | `park_head` | Park Head |
   | `pc_director` | Preventive Care Director |
   | `breeding_director` | Breeding Director |
   | `growth_director` | Growth Director |
   | `feed_director` | Feed Director |
   | `health_director` | Health Director |
   | `procurement_director` | Procurement Director |
   | `ceo_internal` | CXO |

   **Which park a role holder covers.** A park-scoped grant covers its park. A tenant-scoped grant
   covers every park -- EXCEPT `park_head`: the farm's two park heads (Chandrakant, Dinakar) both
   hold `park_head` at TENANT scope on the live data (checked 2026-09-17), so tenant scope would
   hand each of them both parks' tasks. A tenant-scoped park head covers only the park named by
   his HRMS profile, `workforce_members.primary_location_id` (Chandrakant -> Channapatna, Dinakar ->
   Coimbatore -- the same split `pen_visit_park_assignees` records). A park head with no home park
   covers none, loudly. Only an active grant (`status = 'active'`, `valid_to` null or future) on an
   active workforce member with a `user_id` counts. A routine whose roles resolve to nobody raises
   nothing and the kernel names it (never a fallback person).

   **CXOs now execute** (reverses the V1 "CEO writes the rule but never walks pens"): every role
   above holds `pen_routines.execute`, because a task assigned to a CXO must be openable by one.

2. **GENERAL TASKS.** `scope_kind` gains `park`: ONE task per occurrence for the park, no pen
   ("Check the medicine store"). A park task has no shed and no pen label; its title is
   "<routine> · <park>". `after_work` needs pens (work happens IN a pen) and is refused with
   `scope_kind = park`. The check-in is still available on a park task and reads "Check in" rather
   than "Check in to the pen".

3. **EVERY N DAYS.** `cadence_kind` gains `every_n_days` with `interval_days` (2..90). Every
   routine now carries `start_date` (defaults to today, IST): nothing raises before it, and an
   every-N-days routine raises on start_date, start_date + N, start_date + 2N, ...

## 2026-09-18 revision: PROOF PER QUESTION (maintainer instruction, chat)

Maintainer instruction (chat, 2026-09-18), after seeing that the drawer's Photos/Videos
min/max sat under the question list as a task-wide count: *"add it per question -- for that
question to complete it, do they need to add a proof or not -- there's an option that they
should upload photo or video, multiple or single."*

**Each question may carry its own capture rule.** `evidence.questions[].proof` is
`{kind: photo | video | photo_or_video, count: single | multiple}` or absent. Absent means the
value alone answers the question, which is every question authored before this revision, so
no stored routine changes meaning and no migration is needed (the evidence document is JSONB;
`ParseEvidence` still refuses unknown keys, and `proof` is now a known one).

- **Owed when answered, always when required.** A required question with a proof rule cannot
  be submitted without its capture; an optional one owes it only once the assignee has given
  it a value. An optional question left blank asks for nothing -- the rule follows the answer,
  not the form.
- **Single is exactly one; multiple is up to five** (`MaxProofPerKind`, the same cap the
  task-wide rule has always used). A second capture on a single-count question is refused
  (`proof_count`); a capture naming a question that asked for the other medium, an unknown
  question, or a question with no proof rule is refused (`invalid_proof`); a required or
  answered question with no capture is refused (`question_proof_missing`). The phone mirrors
  every one of those in its Submit gate so no submit the server would refuse is ever armed.
- **Question captures do not count toward the task-wide rule.** A capture carries
  `question_id` on `proof_refs[]`; blank means task-wide and is counted against the routine's
  Photo/Video min/max exactly as before. The two pools never mix, because a photo of the
  water trough is not a photo of the pen.
- **The verifier reads each clip beside the claim it proves.** A question capture is
  labelled `<question title> · Photo 1` in the verification item's media; task-wide captures
  keep `Photo 1` / `Video 1`. Counts run per question and per medium.
- **On the phone the capture sits under its question**, never in the task-wide Photos/Videos
  lists. A photo-or-video question offers both cameras on an empty slot and shows whichever
  was taken; a re-take that switches medium keeps only the newer capture, and only the
  captures the form shows ride the submit. Slot keys are
  `routine-q-<question id>-<photo|video>-<n>` (question ids are letters, digits and
  underscores, so the key parses back).
- **On the web** the question editor gains "Proof for this question" (No proof / Photo /
  Video / Photo or video) and, when set, "How many" (One / Up to 5). Both vocabularies are
  served by the catalog (`question_proof_kinds`, `question_proof_counts`) and rendered
  verbatim; `none` is the catalog's key for no proof and never travels.
- The routine list's evidence line adds "N questions with proof" so a reader can tell a
  routine that asks per question from one that asks per task.

Pinned by `domain.TestQuestionProofRulesAreEnforcedPerQuestion` (mutation-tested: dropping
the owed-capture check turns it red), the admin-web decoder test in
`pen-routines.test.mjs`, and the Android
`PenRoutineDetailViewModelTest` "a question's own proof gates the submit" case.

## 2026-09-17 answers to the open questions (maintainer, chat)

1. **Verifier review stays an OPTION per routine.** `review_kind = none` closes the check the
   moment the assignee submits the required answers and captures; `review_kind = verifier`
   closes it when the verifier accepts, and a rejection sends it back to be done again. Nothing
   else closes a check.
2. **No distance rule on check-in for now.** The check-in records GPS, address and the
   mock-location flag for the verifier and refuses nothing.
3. **A routine is never a care visit.** Vaccination, deworming, preventive-care work and the
   next-day pen visit are their own modules; a routine never closes, reopens or changes any of
   them, including an `after_work` routine raised by that work.

## What it is in one paragraph

A **routine** is a rule the CEO writes once, per park: *"In Coimbatore, every day, for every
occupied pen, the park head must answer 'Was the pen cleaned?', take one photo, and check in to
the pen before answering."* The kernel turns that rule into **one task per pen per occurrence**
(the pen-visit shape: system-raised, never typed), lists those tasks on the assignee's phone as
cards, and the assignee works each card: **check in to the pen → answer the questions → capture
the photo/video → submit**. Submit either completes the task or hands its evidence to the
verifier, as the routine says. A missed day rolls forward as *delayed* with the date it was owed
and never disappears. Everything the maintainer listed is a column on the routine.

## The routine (what is configurable)

| Knob | Values | Meaning |
|---|---|---|
| Park | one park | A routine belongs to ONE park; the same check in both parks is two routines, each with its own people. |
| Name, instruction | text | The card title and the sentence on the detail screen. |
| Scope | `all_pens` / `selected_pens` / `park` | All ACTIVE pens of the park (partition catalog, the same source the herd register pickers use), or a ticked list. |
| Occupied only | bool, default true | With `all_pens`, skip pens holding no live animals that day. A cleaning check on an empty pen is noise. |
| Cadence | `daily` / `weekly` / `monthly` / `every_n_days` / `after_work` | See *Cadence* below. |
| Weekdays / month days | `[1..7]` (Mon=1) / `[1..31]` | For weekly / monthly. A month day past the month's end means the last day. |
| After-work kinds | `vaccination`, `deworming`, `anti_protozoan`, `ticks_removal`, `hoof_trimming`, `hair_trimming`, `weighing`, `feed_distribution`, `shifting` | For `after_work`: work of these kinds SUBMITTED in a pen raises the routine there. |
| Due offset | days, default 0 (`after_work` default 1) | Planned date = the cadence day (or the work day) + offset. |
| Notify time | local IST `HH:MM`, default 07:00 | When the day's push goes out. |
| Who does it | one or more ROLES | See the 2026-09-17 revision: resolved per park against live role grants; any holder doing it is enough; nobody resolved means nothing raised, loudly. |
| Evidence | questions, photo min/max, video min/max, presence | See *Evidence* below. |
| Review | `verifier` / `none` | `verifier`: submit hands ONE verification item (all proofs + the answers as context rows) to the verifier; `none`: submit completes the task. |
| Status | `active` / `paused` / `retired` | Paused raises nothing until resumed; retired never raises again. Open tasks are untouched by either. |

**Every edit is a new VERSION** (`pen_routine_versions`), the health-protocol rule: an open task
pins the version it was raised under, so the form the park head opened is the form he submits.
The next occurrence uses the latest version.

## Cadence, in business days

- `daily` — every business day.
- `weekly` — the listed weekdays.
- `monthly` — the listed month days, clamped to the month's last day.
- `after_work` — the day work of a listed kind was SUBMITTED in a pen (its verification item's
  `created_at` in the IST day, the pen-visit read), plus the due offset. This is what makes
  *"trigger it after any task"* configurable rather than hard-wired the way pen visits are.

The kernel stage `pen-routine-kernel` (operational 5-minute lane) does, per tick: materialize
every active routine's tasks for each business date in a bounded look-back window (default 2
days, so a worker down at the day boundary catches up), push one digest per routine once its
notify time has passed, then roll forward. All idempotent on the natural key
`(routine, shed, pen, planned date)`: the first tick after 00:00 IST inserts, every later tick
inserts nothing — "once per day" from the key, not from a schedule, as the task-kernel lock
requires. A task born late (routine created mid-day, worker outage) is due the LATER of its
planned date and today, so nothing is born delayed.

## Evidence

```json
{
  "questions": [
    {"id": "cleaned", "kind": "yes_no", "title": "Was the pen cleaned?", "required": true},
    {"id": "water", "kind": "choice", "title": "Water trough", "required": true,
     "options": [{"value": "clean", "label": "Clean"}, {"value": "dirty", "label": "Dirty"}]},
    {"id": "count", "kind": "number", "title": "Sick animals seen", "min": 0, "max": 500},
    {"id": "note", "kind": "text", "title": "Anything else"}
  ],
  "photo": {"min": 1, "max": 3},
  "video": {"min": 0, "max": 1},
  "presence": "required"
}
```

- Question kinds: `yes_no`, `choice`, `multi_choice`, `number`, `text` — the same widgets the
  phone already renders for the weighing SOP and the procurement inspection.
- Photo and video are in-app live-camera captures validated like every other proof (finished,
  tenant-owned, `capture_source = in_app_camera`, mime matches kind); a gallery pick is refused.
- **Presence** is the maintainer's *"clock-in and clock-out stating some person entered the
  shed"*. `required` means the submit is refused unless the SUBMITTER checked in to THIS pen on
  THIS task first (`pen_routine_task_presence`, event `enter`), and the submit itself records
  the `leave`. Each event carries the honest-capture fields the workforce clock already carries
  (GPS, accuracy, location status, mock-location flag, device). Presence is evidence, never a
  geofence: V1 does not refuse a check-in by distance, it records it for the verifier.

## The task (what the kernel raises)

`pen_routine_tasks` is the PC Care / pen-visit two-dimension row: the KERNEL clock
(`work_state`: scheduled → delayed → completed | canceled; `planned_business_date` immutable,
`due_business_date` rolls forward only) and the GATE (`status`: open → pending_verification →
completed | rework). Natural key `(tenant, routine, shed, partition_key, planned date)`.
`answers jsonb` keyed by question id, `proof_refs jsonb [{ref, kind}]`, the presence stamps,
`submitted_by/at`, `verified_by/at`, `rework_reason`, `row_version`.

Rules the write re-runs under the row lock (`domain.CheckSubmit`): the caller is an assignee;
the task awaits recording; every required question is answered and every answer fits its kind;
photo/video counts sit inside min/max; presence is satisfied when required; the row version the
screen loaded with still matches. A routine with `review: none` completes on submit in the same
transaction; `review: verifier` locks the row pending and the verdict consumer completes or
bounces it (rework re-collects everything; the old answers stay as history on the audit row).

## Surfaces

- **Phone**: module `pen_routines` (label *Routines*), offered by the per-person tick on /people
  (the counts-approver rule: *"rbac per person, not per group"*) and by the `park_head` role.
  One list of cards ("Pen cleaning · Castro 2 · Coimbatore", chip "Due today" / "Delayed since
  14 Sep" / "In review" / "Sent back" / "Done"), To do / Done chips, keyset pages of 20, Room
  cache, refresh on open. Tap → detail: instruction, **Check in to pen** (when required),
  questions, capture slots, **Submit**. Writes ride the outbox (proof upload + submit under one
  group key). Push `pen_routine_due` lands on `/pen-routines`.
- **Web**: `/routines` (module `pen_routines`, page *Routines*): the routines table per park
  with a drawer to create / edit / pause / retire, and a *Today* table of the tasks. Reading
  needs `pen_routines.read`; authoring controls are capability-gated on
  `pen_routines.configure` on BOTH halves (page contract control + route table).
- **Work Board**: rows under *Tasks* ("Pen cleaning · Castro 2", subtitle the routine's cadence
  line), source type `pen_routine_task`, visible on `pen_routines.execute` like pen visits.
- **Verifier**: category `pen_routine` under navigation module `pen_routines` (*Routines*), one
  item per submit carrying every proof and the answers as context rows; SLA 24 h.

## Permissions

| Permission | Who | Opens |
|---|---|---|
| `pen_routines.execute` | every assignable role (`park_head`, the six director roles, `ceo_internal`), anyone ticked Do on Routines | phone module + `/app/pen-routines*` |
| `pen_routines.read` | `ceo_internal`, directors, park heads | `/routines` page + `/admin/pen-routines*` reads |
| `pen_routines.configure` | `ceo_internal` only on the role; per-person Configure tick | routine writes |

WHO may work a given check is decided per row by resolving the routine's `assignee_roles`
against live role grants for its park (see the 2026-09-17 revision), never by a role string the
client sends. A verifier sees routine checks through her position's `pen_routines` verify duty
(migration `000332`, added after the 2026-09-17 proof run found existing databases had none).

## Relationship to pen visits

Pen visits stay exactly as they are: a fixed, system-owned rule (the day after care work, one
video, closes the parent care task). A routine is the CONFIGURABLE generalisation — its
`after_work` cadence can express "visit the pen the day after deworming with one video", but it
closes no parent and is not a substitute. Folding pen visits into routines is a separate
maintainer decision; this change does not touch `penvisits/`.

## Questions answered

All three V1 questions are closed by the maintainer's 2026-09-17 answers above: no parent
closure, no distance refusal, and Routines stays its own phone module (its audience includes
park heads and CXOs, who hold no Tasks "For me" tab).

## Pinned by

`penroutines/domain` unit tests (cadence, clamped month days, submit rules, answer validation,
copy), the Postgres integration tests (natural-key idempotency, roll-forward, presence gate,
verifier vs none review, version pinning), `permissions` tests (every assignable role executes, configure is CXO-only; routes are
gated), the workforce module-offer test, and the Android `PenRoutine*ViewModelTest`s.
Schema: migrations `000328_pen_routines.sql` (tables), `000329` notification type, `000330`
outbox validator, `000331` per-person access ticks, `000332` verifier verify duty. The kernel
E2E proof is `TestKernelStory_PenRoutineDailyCheck`.
