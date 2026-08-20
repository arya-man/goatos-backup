# Vaccination Plan Console — implementation spec

The mock ([`vaccination-plan-console.mock.html`](./vaccination-plan-console.mock.html)) is the
**contract**, not a picture. The implementation must match it to the pixel and to the logic.
Where this document and the mock disagree, the mock is wrong and must be fixed — do not silently
diverge.

Everything below was decided during review with the maintainer. Each rule says *why*, because a
rule without a reason gets "improved" back into the thing it replaced.

---

## 1. Where it lives

| | |
|---|---|
| Route | Preventive Care → **Vaccination plan** |
| Moves from | Admin / Data Ops → Config. That page is vaccination-only; Admin / Data Ops is where the person who owns the decision will never look. |
| Absorbs | the separate **Vaccination SOP** page (§7) |
| Breadcrumb | `Preventive Care / Vaccination plan` on the list, `Preventive Care / Vaccination plan / V4 draft` in the editor |

---

## 2. Two screens, not one

Mirrors the existing product exactly. Do not merge them.

```
SCREEN 1 — list                          SCREEN 2 — editor
┌──────────────────────────┐            ┌──────────────────────────┐
│ Vaccination plan         │            │ ← Vaccination plan       │
│ [Start a new version]    │──────────▶ │ Editing V4 draft ·       │
│                          │            │ based on V3              │
│ ┌ Live right now ──────┐ │            │ ┌ vaccines ┐┌ editor ──┐ │
│ │ V3 · in force since  │ │            │ │ rail     ││ one card │ │
│ │ vaccines table       │ │            │ ├ checklist┤│          │ │
│ │ ⚠ draft waiting      │ │            │ └──────────┘└──────────┘ │
│ └──────────────────────┘ │            │ plan-level cards         │
│ ┌ Earlier versions ────┐ │  ◀──────── │ [pinned action bar]      │
└──────────────────────────┘            └──────────────────────────┘
```

The action bar exists **only** on the editor. A list screen with Save/Publish buttons is wrong.

---

## 3. The version lifecycle — THE critical rule

### 3.1 Starting a version copies the live plan wholesale

**"Start a new version" deep-copies the currently live plan into a new draft.** The CEO changes
one field; everything else carries over untouched.

Verified in the mock: a fresh draft carries all 8 vaccines, 7 switched on, ET+TT still at
6 months / first dose 4 weeks, and the entire Procurement holding block (7-day settle-in,
16-week kid cutoff, 28-day second wave, first wave ET+TT + PPR).

**This is not a convenience — it is what the backend requires.** A published `protocol_version`
is immutable and `protocol_rules` may only be attached while the version is a `draft`. So a new
version must carry a **complete** set of rules. The implementation copies all rules from the
live version and mutates the ones the user touched.

> Changing one vaccine's interval must never require re-authoring the other 16 rules.
> If the UI ever asks the user to re-enter an unchanged value, it is broken.

```
live version ──copy all rules──▶ new draft ──user edits 1 field──▶ publish ──▶ retire previous
```

### 3.2 The lifecycle the database enforces

Proven empirically in [`E2E-PUBLISH-SEMANTICS.md`](./E2E-PUBLISH-SEMANTICS.md) §1:

1. **One live plan per scope.** `protocol_versions_published_no_overlap`, partial on
   `status='published'`. Retiring the old version frees the date range.
2. **Published versions are immutable.** Only `status → retired` is permitted. `effective_to`
   cannot be set on retirement.
3. **Rules attach to drafts only.** `ensure_protocol_child_version_is_draft()`.

Therefore the publish sequence is: **create draft → attach rules → publish transaction**, where
the publish transaction *retires the previous version first and then publishes the draft*, in one
atomic step (`retirePublishedVaccinationMatrixOverlapsTx`, `repository.go:908`). The retire must
come first inside that transaction, because the exclusion constraint forbids two overlapping
published versions. Attempting it in any other order hits a database error.

### 3.3 State machine

| State | List screen shows | Action bar |
|---|---|---|
| No draft | `[Start a new version]` | — (no bar) |
| Draft exists | `[Open V4 draft]` + amber banner `A draft is waiting… [Discard it] [Open the draft]` | Reset · Save draft · Publish (disabled) |
| Draft edited | as above | Publish stays disabled |
| Draft saved | as above | **Publish enabled** |
| Published | `[Start a new version]`, new version becomes "Live right now", previous drops into Earlier versions with today's date | — |

**Publish is gated on Save.** The checklist item "Impact reviewed" is satisfied by saving,
because the impact figures the CEO approves must be the ones the server computed (§8).

Discard and Reset **confirm first** — they destroy work.

---

## 4. Never re-enter, never rewrite history

| Rule | Enforcement |
|---|---|
| Publishing never alters completed or canceled work | SQL predicate `status IN ('scheduled','due','deferred')` — terminal states structurally excluded |
| Publishing never alters in-flight work | same predicate |
| Future scheduled work moves to the new dates | new version generates; **see the open defect below** |
| Old versions are never editable | copy into the current draft instead |

> **Open defect, must be fixed before this ships.** Publishing currently *orphans* the previous
> version's open obligations rather than superseding them — 6,908 rows in the measured case.
> See `E2E-PUBLISH-SEMANTICS.md` §2. The console promises "only what is still coming up can
> move"; that promise is not yet true at the data layer.

---

## 5. Schedule authoring — plain language, not day offsets

Never show `offset_days`, `due_window_days`, `min_gap_days`, `max_delay_days`,
`course_lapse_policy` or any other field name.

```
FIRST DOSE   Give it when the goat is [4 weeks] old
BOOSTER      Give it [3 weeks] after the first dose
             Counted from the day the first dose was actually given, not from the
             date it was due. Never less than 3 weeks.
```

- The booster is **relative to the first dose**, not an absolute age — matching
  `trigger_type: after_previous_completion`.
- Only **ET+TT** and **Blue Tongue** are booster courses; the rest are single-dose. Single-dose
  vaccines show **ONLY DOSE** plus an "+ Add a booster dose" affordance that disappears once one
  exists.
- Header tag reads **Two doses** or **One dose**. Never "course type".

### Repeat interval

```
REPEAT   Do it again every [6] [months] after [the last dose the goat actually got]
         (3 months)(6 months)(9 months)(1 year)(3 years)(Custom…)
         Matches vet guidance for ET + TT (6 months).
```

Backed by `revaccination_interval_days`, which **already exists and already drives booster
generation** — exposing it is a frontend-only change. Deviating from vet guidance is allowed and
shows an amber note that the change is audit-logged.

### Deadline — one number, forward only

```
DEADLINE   Any dose can be up to [2 weeks] late
   Due day 15 Aug                      Deadline 29 Aug
   [███ Can be given ███][██ Missed → needs your approval ██]
```

The window runs **forward from the due day only** — `window_end = due_at + due_window_days`;
no `due_at − window` exists anywhere in the engine. A dose due 15 Aug with a 14-day window is
valid 15–29 Aug. Giving it on 10 Aug does **not** count as on time.

**The current `4w ±7d` label is wrong and must be corrected regardless of this redesign.**

One control writes both `due_window_days` and `max_delay_days`, keeping them consistent by
construction (publish requires `max_delay_days >= due_window_days`).

Removed deliberately: **"If missed past that"**. A dose is either given or it moves; the three
`course_lapse_policy` options were schema leaking into the UI.

---

## 6. Every duration control accepts any value

**No canned lists anywhere.** Every duration opens the same picker:

```
GIVE THE FIRST DOSE AT
  [ − ]  [  17  ]  [ + ]   [ weeks ▾ ]
  (1 week)(2 weeks)(4 weeks)(12 weeks)(16 weeks)(20 weeks)
```

- Free number field + unit selector (days / weeks / months / years).
- Presets are shortcuts, never limits.
- Applies live; no Apply button.
- Arrow keys step; Enter closes.
- Native `<select>` with fixed options is **banned** for durations — including inside
  "Add a vaccine", where first dose, booster gap, repeat interval and deadline are all
  number + unit pairs.

Verified: a first dose of **17 weeks** and a deadline of **5 months** are both accepted.

---

## 7. Proof — one binary choice for the whole plan

Replaces "Executable SOP version (required to publish)".

```
HOW THE OPERATOR PROVES IT
( ) One video per shed   — a 200-goat shed produces 1 clip.
(•) One video per goat   — a 200-goat shed produces 200 clips.   ← default
```

- **Per-goat is the proposed default**, a deliberate departure from staging (shed-level since
  migration `000021`). The per-goat path already exists (`vaccination.per_goat_video_qa`).
- The system attaches the matching `sop_version_id` itself. The CEO never picks a UUID.
- Both cards state the volume cost so the choice is made with eyes open.
- Plan-level, not per-vaccine.

The separate Vaccination SOP page folds in. Its question-builder authors field types that
migration `12955` deletes for vaccination — it is authoring against a schema vaccination no
longer uses.

---

## 8. What publishing will change

The card **does not exist** when the draft matches the live plan. No empty state, no zeros.

When something changed:

```
Publishing this changes 775 upcoming vaccinations.
⚠ 59 of them land in the first week. That is extra work arriving at the parks the day
  you publish, on top of what is already scheduled.

VACCINE   WHAT YOU CHANGED                        WHAT HAPPENS
ET + TT   Repeat every 6 months → every 3 months  775 goats need it sooner
```

- Lengthening an interval turns the amber block green: *"None of it lands in the first week."*
- Effects are phrased in **animals**, never tasks — "775 goats need it sooner", not "775 rows".
- **Computed server-side on Save**, not per keystroke: the figures require counting obligations
  across the herd. The client-side recompute in the mock is a demo affordance only.
- Candidate endpoint: `POST /protocols/vaccination/impact-preview`, already called by the current
  page. Confirm it can return the first-week spike before building.

---

## 9. Procurement holding

Keep the name. It is the existing term, and it is `rule_dsl.procurement_policy`. Do **not**
rename it to "Goats bought in as adults" — that was tried and rejected.

| Control | Field | Effect |
|---|---|---|
| Settle in · 7 days | `warmup_no_vaccination_days` | no vaccine while transport-stressed |
| Kid or adult · 16 weeks | `kids_normal_schedule_until_weeks` | drives `SchedulePathForGoat()` |
| Prior doses · count them | `adult_prior_vaccination_allowed` | whether unproven claims skip doses |
| First wave (multi-select chips) | `first_wave` | given when settle-in ends |
| Second wave, after · 28 days | `second_wave_after_days` | |
| Goat / Sheep second wave (chips) | `goat_second_wave`, `sheep_second_wave` | |

Wave chips are **multi-select, driven by the live vaccine list** — add a vaccine and it appears
as a wave option. Changing them produces its own impact row.

---

## 10. Automatic safety rules — read-only, verbatim

Copied exactly from `internal/adminui/app/service.go:3974-3980`. Do not paraphrase.

One of them constrains this design directly:

> Mother vaccinated/unknown category is ignored. **It is never a rule selector.**

An earlier draft split the schedule by mother-vaccination status, following the two columns in
the wiki's Vaccination Rules table. That contradicts the built system and was removed. The
"Mother not vaccinated" column is documented but not implemented.

---

## 10a. No "Starts on" — publish is immediate

Removed from the plan header. Publishing takes effect at once; already-live drives and completed
history are unaffected, and every future obligation for a changed vaccine moves.

This is not only a simplification. Future-dating cannot be done safely today: `effective_to` is
immutable on a published row, so the outgoing version cannot be closed to open the new one later.
Retiring it stops it immediately, leaving a window with **no effective plan at all**. A control
that silently creates a coverage gap should not exist. See the ADR, "Open" item 2.

Publish must therefore be one atomic transaction: retire the current version + publish the new
one. `retirePublishedVaccinationMatrixOverlapsTx` already does this.

---

## 11. Real data only — no invented values

| | Real value | Source |
|---|---|---|
| Parks | **Coimbatore** (934 alive), **Channapatna** (715 alive) | `locations` where `location_type='park'` |
| Scope options | Both parks / Coimbatore only / Channapatna only | — |
| Herd | 1,649 alive — 804 goats, 845 sheep | `goats` where `lifecycle_status='alive'` |
| Schedules | ET+TT 4w+7w/6mo · PPR 16w/3yr · Goat Pox 16w/1yr · Sheep Pox 12w/1yr · Blue Tongue 16w+20w/1yr · FMD 12w/9mo · HS 12w/1yr | wiki Vaccination Rules table |

"Ashoka Park", "Gandhi Park", 2,033 and 1,806 were invented and have been removed. **Never
invent a park, a count, or a schedule.**

### Z1 + Z3

One **combination** row, like ET+TT. **There is no Z2.** Killed, bacterial/toxoid, 4 weeks +
booster at 7 weeks, every 6 months. Flagged in-card that the real label names are still unknown.
Full context in [`../vaccination-rules.md`](../vaccination-rules.md).

---

## 12. Copy rules

Banned from the UI entirely: `obligation`, `SOP`, `rule_dsl`, `protocol version id`, `scope`,
`offset`, `window`, `min gap`, `lapse`, `course type`, `drive priority`, `dose (ml)`,
`route/site`, `vial`, `trigger type`, `catch-up policy`, `escalation policy`.

| Instead of | Say |
|---|---|
| Executable SOP version | How the operator proves it |
| Obligation | vaccination / job on the operator's phone |
| Retired | earlier version |
| Effective from | in force since |
| ±7 days | can be up to 2 weeks late |

- **No dead text.** Every element must change a decision or be deleted. If a card has nothing to
  say, the card does not render.
- **No numbers without meaning.** Never show a count without saying what it means for the farm.
- Version IDs (`7d5c2ccc…`) belong in the audit log, never on a CEO's screen. Show the label.

---

## 13. Chrome and layout

- Real `mesha-theme.css` tokens only: `--sidebar:#0A0F0C`, `--brand:#7CCB45`, `--danger:#F0635F`,
  `--warn`, `--panel`, `--line`. No colour literals — one slipped in and became `--on-status`.
- Existing sidebar anatomy: `.nav` / `.ggrp` / `.leaf` / `.subnav`, with `aria-expanded` and
  `aria-current`.
- Light and dark must both work. Tokens defined on bare `:root`, redefined under
  `prefers-color-scheme: dark` and `[data-theme="dark"]`.
- **`[hidden]{display:none!important}`** — `.nvf{display:flex}` and `.draftnudge{display:flex}`
  silently beat the UA `[hidden]` rule. This caused two bugs (Discard appearing dead, the booster
  field never hiding). Keep the override.
- Every flex child holding text needs `min-width:0`, or it grows past its container instead of
  ellipsising — and `scrollWidth` checks will not catch it.
- **One page scroll.** No inner scroll region competing with the page.
- Action bar pinned to the viewport bottom, editor only.
- Checklist lives in the left column, permanently visible — no expand/collapse.

---

## 14. Every control must work

The mock has **zero** dead controls; the implementation must too. Enumerate every `button`,
`select` and `input`, and assert each has a handler. 17 were dead at one point and only a
scripted sweep found them.

Includes chrome: burger collapses the sidebar, park picker re-scopes the plan, bell and account
chip respond, sidebar destinations navigate or say they are elsewhere, theme toggle works both
directions, Escape closes every modal.

### Validation the mock enforces

| Rule | Message |
|---|---|
| Duplicate vaccine code | `Short code "PPR" is already used by PPR.` |
| Duplicate vaccine name | `"PPR" is already in this plan.` |
| Every vaccine switched off | publish blocked — *"Every vaccine is switched off — there is nothing to publish."* |
| Unsaved edits | publish blocked until Save |
| Booster fields | hidden unless "First dose + booster" is selected, and reset when switching back |

---

## 15. Known gaps — decide before building

1. **Publish does not supersede old obligations** (§4). Fix at the data layer first.
2. **Publish is not role-gated.** The legacy UI gates on CEO/COO; the mock does not model it.
3. **No way to delete a vaccine**, only switch it off.
4. **No version name or change note** — "What changed" in history is currently authored nowhere.
5. **Ownership.** The mock treats the CEO as editor of record and audit-logs deviations from vet
   guidance. The wiki assigns vaccination planning to the PHC Director. Unresolved.
6. **Spacing shifts are invisible.** Cross-vaccine gaps can push a dose later
   (`clinical_spacing_auto_shift`); the console presents dates as fixed.
