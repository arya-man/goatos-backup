# Vaccination Seed & Schedule — Audit Findings + Fix Plan (2026-07-14)

Audit of the 1,311-animal local seed (`goatos-local-current`, sourced from
Vaccination DB V2 `Combined` / Demo DB `goats`+`vaccination`). The seed is
genuinely source-backed and E2E did **not** corrupt it — the defects are
deterministic importer + rule behavior. This runbook is the remediation spec.

Source of truth for schedule content: `docs/preventive-care-vaccination/APPROVED-SCHEDULE-MATRIX.md`
and `docs/preventive-care-vaccination/vaccination-rules.md`. **Do NOT** use V2's
`Vaccination Plan` sheet — it is explicitly labelled "Crude Version" and conflicts
with the approved matrix (FMD, HS cadence differ).

## Maintainer-locked rules (2026-07-14)

1. **Kid schedule cutoff = 16 weeks; last due date = 20 weeks.** Ladder is
   4 → 7 → 12 → 16 → 20 weeks of age. 16w is the conceptual cutoff (last new
   vaccine); the two live vaccines at 16w (Goat Pox, Blue Tongue 2nd) shift +4w
   to 20w for live-live spacing, so the last actual dose lands at 20 weeks.
2. **Age gate:** animal **≤ 20 weeks old → kid schedule**; **> 20 weeks → adult**.
   Never generate kid doses for an animal older than 20 weeks.
3. **Unvaccinated adult → adult primary course (2 doses ~4 weeks apart) + boosters.**
   Never re-run the age-based kid doses on an adult.
4. **Procured animals** frequently receive the 2-dose/4-week adult primary AT the
   procurement farm (held 4 weeks, both doses, then moved to our sheds). Recognize
   that primary as complete and go straight to boosters — do not re-schedule the
   adult primary.
5. **Anchor fallback:** if an animal's DOB **or** arrival date is missing but a
   vaccination record exists, anchor its schedule on the vaccination date
   (first/last completion). (Verified: 0 animals have all three — DOB, arrival,
   and vaccination — missing, so this rescues 100% of the current deferrals.)
6. **Pregnancy:** the 4th/5th-month gestation defer code path exists; only the
   source data is missing. For now schedule pregnant animals as normal (do not
   block); wire real reproductive data later.

## Confirmed bugs (5)

| # | Bug | Evidence | Location |
|---|-----|----------|----------|
| 1 | **447 FMD booster completions dropped by the importer.** Source has 3,836 dated cells; DB has 3,389 completions — the missing 447 are FMD boosters (dated 2026-03-20). The FMD booster branch exits before import. | source 3836 vs db 3389 | `backend/cmd/seed-vaccination-real/main.go:760` |
| 2 | **FMD revaccination anchored to the wrong (earlier) dose.** 285/308 scheduled FMD revacs anchor to the first dose instead of the latest course completion. Ex `901007000503822`: DB 2026-11-22, correct 2026-12-19 (booster 2026-03-20 + 274d). | 285/308 | `generation.go` revac anchor |
| 3 | **Missing-anchor deferral despite vaccination history.** 531 animals (257 no-DOB + 274 no-arrival) — all with vaccination history — get ~1,994 doses deferred (`due_at` = generation timestamp) instead of anchored on their vaccination date. Presented in the UI as "Deferred" (implies sickness). | 1,994 rows / 531 animals | `generation.go dueAt/genOneGoat` |
| 4 | **Health / reproductive / shed-tag source signals dropped.** Demo DB has health (`Closed` 234, `Open` 7, `Extended` 1) + shed tags (`Pregnant`, `Non-Pregnant`, `ICU`, `ICU-Kid`, `Quarantine kids`). The mapper only recognizes healthy/sick/icu → `Open/Closed/Extended` → NULL; `shed_tag` is not loaded; `reproductive_status` is not imported. All 1,311 goats therefore have blank health/reproductive state. Clinical-defer safety block is effectively dead. | 242 source health rows lost | `main.go:1794` + shed_tag loader |
| 5 | **Kid doses generated for adults (no >20-week age cap).** birth_age kid rules have no upper age bound, so animals well past 20 weeks still get kid-stage obligations. 186 kid-stage doses currently scheduled onto adults (1,756 kid-dose rows on adults total). Ex: adult DOB 2025-04-06 (~66 weeks) shown `fmd_kid_12w`/`blue_tongue_kid_20w` "due today". | 186 scheduled on adults | `generation.go` / rule config |

Verified CORRECT (not bugs): kid interval values (28/49/84/112/140d match the
matrix), per-animal grain, no duplicate open obligations, no zero-obligation
alive goats. Deferrals are missing-anchor, NOT clinical/pregnancy.

## Fix plan

### A. Importer (`backend/cmd/seed-vaccination-real/main.go`)
- A1. Import the FMD booster completions (fix the booster-branch early exit at
  `:760`) so all 3,836 source dated facts land (target: 3,836 completions).
- A2. Map health vocabulary: `Open/Closed/Extended` (and any other source values)
  → the real `health_status` domain, at `:1794`; do not silently NULL them.
- A3. Load `shed_tag` → clinical/reproductive/location signals (`ICU`,
  `Quarantine`, `Pregnant`, `Non-Pregnant`, `ICU-Kid`).
- A4. Import `reproductive_status`.

### B. Generation rules (`backend/internal/vaccination/app/generation.go`)
- B1. **Age cap:** stop generating kid (`birth_age`) doses once age > 20 weeks.
- B2. **Adult routing:** an animal > 20 weeks with no completed kid series → the
  adult primary course (2 doses ~4 weeks apart) + boosters. Never kid doses.
- B3. **Anchor fallback:** DOB or arrival missing + vaccination present → anchor
  on the vaccination date (first/last completion) instead of deferring.
- B4. **FMD revac anchor:** anchor revaccination on the *latest* course completion
  (the booster), not the first dose.
- B5. **Procurement primary recognition:** treat a procured animal's 2-dose/4-week
  procurement-farm primary as complete → boosters, not a re-scheduled primary.
- B6. Follow the APPROVED matrix, never V2's Crude-Version plan.

### C. Re-seed + re-verify
Re-seed clean, then assert:
- completions = 3,836 (not 3,389) — FMD boosters present;
- 0 kid doses on animals > 20 weeks;
- 0 obligations deferred for `missing_due_date` where a vaccination anchor exists
  (target: deferrals only for genuinely un-anchorable animals — currently 0);
- FMD revac dates anchored to latest completion (spot-check `901007000503822`
  → 2026-12-19);
- health/reproductive/shed-tag populated where the source provides them;
- interval math + grain + coverage unchanged.

## Open data caveats
- `procurement_hf_vaccination_evidence` (the designed home for the procurement
  primary) has **0 rows** — confirm the 498 procured animals' primary is captured
  in the V2 vaccination history before relying on B5; otherwise it is a data gap.
- 257 goats have blank `origin_type` + no DOB + no arrival; they all have
  vaccination history, so B3 anchors them, but their identity provenance is thin.

## Do NOT
- Do not reseed with the current uncommitted "Crude Version" working-tree patch:
  it still cannot import the FMD booster and lets ET+TT revac fire 6 months after
  dose 1 while the booster is Pending. Build from this spec against the approved
  matrix instead.
