# Pen routines: configurable recurring pen checks for the park head

Maintainer instruction, 2026-09-16 (chat): *"Park Head daily tasks: 1) each shed cleaned?
2) it should be configurable fully ... per shed, have filters like should we configure it daily
or weekly or monthly, or do we need to trigger it after any task ... for each park whom we need
to assign tasks, everything should be configurable ... what we expect from them: questions,
video / photo, or any clock-in and clock-out stating some person entered the shed."*

Status: ACCEPTED, built on `feat/pen-routines`. On screen the module is **Routines**; the word
is *pen*, never *shed* (`docs/decisions/pen-not-shed-vocabulary.md`).

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
| Scope | `all_pens` / `selected_pens` | All ACTIVE pens of the park (partition catalog, the same source the herd register pickers use), or a ticked list. |
| Occupied only | bool, default true | With `all_pens`, skip pens holding no live animals that day. A cleaning check on an empty pen is noise. |
| Cadence | `daily` / `weekly` / `monthly` / `after_work` | See *Cadence* below. |
| Weekdays / month days | `[1..7]` (Mon=1) / `[1..31]` | For weekly / monthly. A month day past the month's end means the last day. |
| After-work kinds | `vaccination`, `deworming`, `anti_protozoan`, `ticks_removal`, `hoof_trimming`, `hair_trimming`, `weighing`, `feed_distribution`, `shifting` | For `after_work`: work of these kinds SUBMITTED in a pen raises the routine there. |
| Due offset | days, default 0 (`after_work` default 1) | Planned date = the cadence day (or the work day) + offset. |
| Notify time | local IST `HH:MM`, default 07:00 | When the day's push goes out. |
| Assignees | one or more people | Per routine (and so per park). Any one of them doing it is enough — the pen-visit rule. A routine with nobody assigned raises nothing and is reported loudly; there is never a fallback person. |
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
| `pen_routines.execute` | `park_head` role, the six director roles, anyone ticked Do on Routines | phone module + `/app/pen-routines*` |
| `pen_routines.read` | `ceo_internal`, directors, park heads | `/routines` page + `/admin/pen-routines*` reads |
| `pen_routines.configure` | `ceo_internal` only on the role; per-person Configure tick | routine writes |

`ceo_internal` holds read + configure and NOT execute: the CXO desk writes the rule, it does
not walk pens (the toxin rule). WHO may work a given task is decided per row against
`pen_routine_assignees`, never by a role string.

## Relationship to pen visits

Pen visits stay exactly as they are: a fixed, system-owned rule (the day after care work, one
video, closes the parent care task). A routine is the CONFIGURABLE generalisation — its
`after_work` cadence can express "visit the pen the day after deworming with one video", but it
closes no parent and is not a substitute. Folding pen visits into routines is a separate
maintainer decision; this change does not touch `penvisits/`.

## Open questions surfaced to the maintainer

1. Whether a routine should ever close a parent task the way a pen visit does (V1: no).
2. Whether presence should refuse a check-in by GPS distance from the park (V1: recorded, not
   refused — the workforce clock's own D3 decision).
3. Whether the `Routines` list should fold into the Tasks module's "For me" tab as a second
   card type (the 2026-09-14 pen-visit note). V1 gives Routines its own module because its
   audience is the park head, who holds no Tasks module today.

## Pinned by

`penroutines/domain` unit tests (cadence, clamped month days, submit rules, answer validation,
copy), the Postgres integration tests (natural-key idempotency, roll-forward, presence gate,
verifier vs none review, version pinning), `permissions` tests (CEO never executes; routes are
gated), the workforce module-offer test, and the Android `PenRoutine*ViewModelTest`s.
Schema: migration `000320_pen_routines.sql` (+ `000321` notification type, `000322` outbox
validator).
