# Vaccination Plan Console — redesign proposal

**Status:** proposal, not built
**Replaces:** `/config?scope_mode=company&category=vaccination` (Config — Protocol Rules)
**Mock:** [`vaccination-plan-console.mock.html`](./vaccination-plan-console.mock.html) — standalone, interactive, no backend
**Baseline:** `origin/main` @ `b6800387f`

The current authoring page is a developer's view of `rule_dsl` rendered as a form. It asks a
non-technical CEO for `offset_days`, `due_window_days`, `min_gap_days`, and an "Executable SOP
version", shows read-only tiles for figures nothing reads, and hides the one number the vet
protocol actually varies. This proposes a page organised around the decisions a CEO makes, with
every control traced to a field the engine reads.

---

## 1. Why change it

| Problem today | Evidence |
|---|---|
| Schedule authored as day-offset columns | `rule-editor-modal.tsx` age-course table |
| Revaccination interval shown read-only | `revaccination_interval_days` is writable and drives boosters — the UI never exposes it |
| `±7d` tolerance implies a symmetric window | Engine is forward-only (§4) — the label is wrong |
| Dose (ml) and vial size presented as headline facts | Never read by obligation generation (§5) |
| "Executable SOP version (required to publish)" | Vaccination-only concept, named after its storage table (§6) |
| Publish gate reads as an error banner | "Not publishable yet · Save the draft first" |
| No blast-radius preview | Publishing silently reschedules thousands of tasks |
| One 4,854-line component | `features/config/rule-editor-modal.tsx` |

---

## 2. Structure

```
Preventive Care / Vaccination plan / Company · version 4 draft

┌ plan header ──────────────────────────────────────────────┐
│ Applies to · Animals · Starts on · Replaces               │
└───────────────────────────────────────────────────────────┘
┌ vaccines ────┐ ┌ selected vaccine ─────────────────────────┐
│ ET+TT        │ │ Which animals get it                      │
│ PPR          │ │ The doses a young goat gets               │
│ Goat Pox     │ │ Deadline                                  │
│ …            │ │ How often to repeat it                    │
├ checklist ───┤ │ Vaccine reference (collapsed)             │
│ ✓ …          │ └───────────────────────────────────────────┘
└──────────────┘
   How the operator proves it        ← plan-level, once
   Goats bought in as adults         ← plan-level, once
   Automatic safety rules (read-only)← plan-level, once
   What publishing will change       ← plan-level, once
┌ pinned action bar ────────────────────────────────────────┐
│ ● Draft          Reset · Save draft · Publish plan        │
└───────────────────────────────────────────────────────────┘
```

Only the vaccine card is per-vaccine. Everything else is plan-level and rendered once — the
current page repeats plan-level sections inside every vaccine.

Actions are pinned to the bottom of the viewport, not stacked at the top above a long form.
The readiness checklist sits in the left column, permanently visible, with no expand/collapse.

---

## 3. Schedule authoring

Day-offset columns become sentences with inline controls.

```
FIRST DOSE   Give it when the goat is [4 weeks] old
BOOSTER      Give it [3 weeks] after the first dose
             Counted from the day the first dose was actually given,
             not from the date it was due. Never less than 3 weeks.
```

The booster is **relative**, matching the engine: dose 2 uses
`trigger_type: after_previous_completion` with `offset_days: 21`, not an absolute age
(`cmd/seed-vaccination-trigger/main.go:354`). It is not an age field.

Only two vaccines are booster courses — ET+TT and Blue Tongue (`Type` column, Vaccination Rules).
`validCourseTypes = {"single","booster"}` (`cmd/seed-vaccination-real/main.go:4153`). Single-dose
vaccines render one **ONLY DOSE** row plus an "+ Add a booster dose" affordance.

### Repeat interval — the main gap this closes

```
REPEAT   Do it again every [6] [months] after [the last dose the goat actually got]
         (3 months) (6 months) (9 months) (1 year) (3 years) (Custom…)
         Matches vet guidance for ET + TT (6 months).
```

`rule_dsl.schedule[].revaccination_interval_days` already exists and drives booster generation.
This is a **frontend-only** change. Presets come from the Vaccination Rules table; choosing a
different value is allowed and shows an amber note that the deviation is audit-logged.

---

## 4. Deadline — forward only, one number

```
DEADLINE   Any dose can be up to [2 weeks] late
   Due day                         Deadline
   15 Aug                          29 Aug
   [███ Can be given ███][██ Missed → needs your approval ██]
```

The window runs **forward from the due day only**:

```sql
-- obligation/adapters/postgres/repository.go:2124
window_end = oi.due_at + make_interval(days => pr.due_window_days)
```

`window_start` is the due date itself. No `due_at - window` exists anywhere in the engine.
A dose due 15 Aug with a 14-day window is on time 15–29 Aug; giving it on 10 Aug does **not**
count as on time. The current `4w ±7d` label states the opposite and should be corrected
regardless of whether this redesign ships.

Backed by `due_window_days`, mirrored into `max_delay_days` (publish requires
`max_delay_days >= due_window_days`, `protocol/app/publish.go:875`). Presenting one number
keeps the two consistent by construction.

### Spacing overrides the date

The date shown is a target, not a promise. Cross-vaccine spacing applies a **floor**:

```go
// vaccination/app/compatibility.go:206
floor := businessDayStart(admin.AdministeredAt).AddDate(0, 0, int(gap))
if out.Before(floor) { out = floor }
```

It can only push a dose later, never earlier. Applied against history
(`compatibility.go:192`) and against co-due vaccines in the same pass
(`generation.go:1578`), so PPR and Goat Pox both due today are split by the live→live gap
rather than both landing same-day. Shifts are recorded as `clinical_spacing_auto_shift` with
the conflicting vaccine and rule.

**Open:** the mock does not yet surface this. A line under each schedule —
*"Actual dates can slide later if another vaccine was given too recently"* — is pending.

---

## 5. Dose and vial demoted

Moved from headline "Vaccine facts" tiles into a collapsed disclosure labelled
**"Vaccine reference — not used for scheduling"**.

- `dose_amount` / `dose_unit` and `vial_doses` are never read by obligation generation.
- Stock decrements in **doses**, not millilitres (`vaccine_stock.doses_available -= 1`).
- The wiki lists them in a reference table with no operational rule attached — no wastage
  tracking by volume, no vial open-time limit, no indent maths.

Kept visible-on-demand so the plan still reads correctly against the vet sheet.

---

## 6. "Executable SOP version" → a binary proof choice

The current control asks the CEO to select a row from `sop_versions` and blocks publish without
one (`protocol/app/publish.go:573`, `:599`). For vaccination this is a required field with one
meaningful answer:

- migration `000001_..._baseline.sql:12955` **strips** the manual medical fields (vaccine batch,
  cold chain, dose, route/site, administered-at, adverse reaction) from the
  `vaccination.drive` / `vaccination.session` `form_dsl`;
- `:14814` rewrites the field list to exactly `goat_ids` (scan) + `shed_video`.

So the live vaccination procedure is *scan every goat, then record video*, and only the video
granularity varies between the two vaccination SOPs that exist. The page asks the real question
instead:

```
HOW THE OPERATOR PROVES IT
( ) One video per shed   — a 200-goat shed produces 1 clip.
(•) One video per goat   — a 200-goat shed produces 200 clips.   ← default
```

**The proposed default is per-goat**, which is a deliberate departure from what staging runs
today (`vaccination.drive`, shed-level, set by migration `000021`). Per-goat proof is the
stronger evidence — one clip tied to one scanned tag — and the per-goat code path already exists
(`vaccination.per_goat_video_qa`). The cost is volume: a 200-goat shed produces 200 clips to
record, upload, verify, and store, against 1 today.

This is a business decision, not a UI one. The page states the cost plainly on both cards so it
is made with eyes open, and either choice is one click.

The system attaches the matching `sop_version_id` itself. The separate **Vaccination SOP** page
folds into this page; its question-builder authors field types that the migration deletes for
vaccination.

**Not proposed:** removing the SOP concept. Only removing the CEO-facing version picker for
vaccination, where it has one answer.

---

## 7. Goats bought in as adults

`rule_dsl.procurement_policy` — named "Procurement holding" today, which collides with the
Procurement *module* (suppliers, loads, transport). Retitled to describe the animals.

| Control | Field | Effect |
|---|---|---|
| Settle in · 7 days | `warmup_no_vaccination_days` | No vaccine while transport-stressed |
| Kid or adult · 16 weeks | `kids_normal_schedule_until_weeks` | Drives `SchedulePathForGoat()` — kid vs adult catch-up, including the 16–20w continuation-only rule (`vaccination/app/schedule_policy.go:186`) |
| Seller's word · count them | `adult_prior_vaccination_allowed` | Whether unproven claims skip doses |
| First / second visit | `first_wave`, `second_wave_after_days`, `goat_second_wave`, `sheep_second_wave` | Two visits 28 days apart — max 2 vaccines per visit, live vaccines need spacing |

Seed defaults: `cmd/seed-vaccination-real/main.go:3943`. Validated at
`protocol/app/publish.go:1003`.

---

## 8. Automatic safety rules

Read-only, copied verbatim from `internal/adminui/app/service.go:3974–3980`. Retained from the
current page unchanged. One rule of theirs constrains this design directly:

> Mother vaccinated/unknown category is ignored. **It is never a rule selector.**

An earlier draft of this mock split the schedule by mother-vaccination status, following the
two columns in the Vaccination Rules table. That contradicts the built system and was removed.
The table's "Mother not vaccinated" column is documented but not implemented, and the engine
deliberately ignores it.

---

## 9. What publishing will change

New. Publishing today silently reschedules future work with no preview.

The first draft of this section used four stat tiles — animals covered / tasks moved / due
within 7 days / completed records affected. On an unedited plan that reads `2,033 · 0 · 0 · 0`,
which tells a CEO nothing, and "become due within 7 days" is engine vocabulary, not a business
consequence. Replaced with prose that only appears when there is something to say.

**No edits:**

> Nothing will change. This draft is the same as the plan that is already live.

**After shortening the ET+TT interval from 6 months to 3:**

> Publishing this changes **849** upcoming vaccinations.
>
> ⚠ **59 of them land in the first week.** That is extra work arriving at the parks the day you
> publish, on top of what is already scheduled.
>
> | Vaccine | What you changed | What happens |
> |---|---|---|
> | ET + TT | Repeat every 6 months → every 3 months | 849 goats need it sooner |

**After lengthening it instead**, the amber block becomes a green one: *"None of it lands in the
first week — the parks get time to absorb it."*

The workload spike is the number that matters. Everything else is context for it. Effects are
phrased in animals rather than tasks — "849 goats need it sooner", not "849 tasks rescheduled" —
because the CEO is deciding about animals and field labour, not rows.

### When it is computed

In the mock it recomputes client-side on every edit, from hardcoded herd constants — that is a
demo affordance, not a proposal.

In a real build it must not run per keystroke. The figures require counting obligations across
the whole herd, so the proposal is **compute server-side on Save draft**:

```
edit … edit … edit  →  Save draft  →  server diffs draft vs live  →  impact shown  →  Publish enabled
```

This is already the shape of the readiness checklist — "Impact reviewed" is satisfied by saving,
and Publish is disabled until then. It also means the numbers a CEO approves are the numbers the
server computed, not a client-side estimate that could drift from what publish actually does.

Rows disappear when a change is reverted.

**Not built.** This needs a real backend endpoint. The nearest existing thing is
`POST /protocols/vaccination/impact-preview`, already called by the current page — the counts
here are illustrative and must be replaced with its response before this ships. Whether it can
return the first-week spike figure is the first thing to check.

## 10. Navigation

- **Config** leaves Admin / Data Ops and becomes **Vaccination plan** under Preventive Care.
  It is a vaccination-only page; Admin / Data Ops is where it is least likely to be found by
  the person who owns the decision.
- **Vaccination SOP** folds into this page (§6).

The mock shows both the old and new positions so the move is legible; a real build would show
only the new one.

---

## 11. Ownership — open question

The mock treats the CEO as editor of record and audit-logs any deviation from vet guidance.
This does not match the documented org model: the PHC Director "plans and executes the full
vaccination calendar", and the wiki presents revaccination intervals as fixed veterinary
protocol rather than a business setting.

Two options, both cheap, but the decision is not the author's to make:

1. **CEO edits, deviations logged** — what the mock does.
2. **Intervals locked to vet defaults**, with a director-only override capability.

---

## 12. Design system

The mock uses the real `apps/admin-web/app/mesha-theme.css` tokens — `--sidebar:#0A0F0C`,
`--brand:#7CCB45`, `--danger:#F0635F` — and the shipped `.nav` / `.ggrp` / `.leaf` / `.subnav`
sidebar anatomy, so it composites correctly with the existing shell in both themes.

It is standalone HTML. admin-web has no component primitives: no shadcn, no Radix, despite
`tailwind.config.ts` already declaring shadcn-style HSL variables. Every select, popover, and
stepper is hand-rolled per page, which is the main reason `rule-editor-modal.tsx` is 4,854
lines. Adopting a primitive layer is a prerequisite for building this properly and is tracked
separately from this proposal.

---

## 13. Verification performed on the mock

Painted-box overflow detection (every child measured against its container edges, not
`scrollWidth`, which misses flex items that grow instead of clipping) across 7 vaccines ×
on/off × both proof modes × 1512 / 1280 / 1024 / 820 px, light and dark. Zero spills, zero page
overflow, no console errors. Below 980 px is unverified — the preview harness clamps there.

---

## 14. Status of each claim

| Claim | Basis |
|---|---|
| Schedule ages, revaccination intervals, dose/vial, priority | Vaccination Rules wiki table |
| Forward-only window, spacing floor, booster trigger, path selection | Read from backend source |
| Safety rules text | Verbatim from `service.go` |
| Procurement defaults | `seed-vaccination-real/main.go` |
| Impact preview counts | **Illustrative — not real data** |
| Drive priority 1–5 | Wiki column; **nothing in the backend reads it today** |
| Herd counts (2,033 / 1,806 / 227) | Approximate, from prior reconciliation notes |
