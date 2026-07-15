# Vaccination Seed & Schedule — Audit Findings + Fix Plan (2026-07-14)

Audit of the 1,311-animal local seed (`goatos-local-current`, sourced from
Vaccination DB V2 `Combined` / Demo DB `goats`+`vaccination`). The seed is
genuinely source-backed and E2E did **not** corrupt it — the defects are
deterministic importer + rule behavior. This runbook is the remediation spec.

Schedule content source of truth: `docs/preventive-care-vaccination/APPROVED-SCHEDULE-MATRIX.md`
and `docs/preventive-care-vaccination/vaccination-rules.md`. **Do NOT** use V2's
`Vaccination Plan` sheet ("Crude Version") — it conflicts with the approved matrix.

## Final locked behavior (maintainer, 2026-07-14)

### 1. Existing vaccine history — per-vaccine continuation
- Each vaccine progresses **independently from its own latest completion**.
  ET+TT continues from the latest ET+TT completion; every vaccine from its own.
- **FMD next due = latest trusted FMD administration + 9 months** (FMD is
  "single + repeat", anchored on the LATEST administration — not first-dose+274).
- **Never reverse-engineer DOB** from vaccination dates. (Proven unreliable: the
  same animal's completions back-derive DOBs ~10 months apart because they are
  real catch-up / field-drive dates, not textbook-age doses.)

### 2. Never-received vaccine with no DOB — catch up (option a)
- Enters the **adult catch-up / primary path at the next compatible drive**.
- **Do NOT** generate an old `kid_12w` / `kid_16w` obligation for it.
- Respect max **two vaccines per visit**, live-vaccine spacing, and health /
  pregnancy constraints when placing it.
- **Do not defer merely because DOB is missing.**

### 3. Age transition
- A **new kid course may START only through 16 weeks**.
- A kid **already in the course may FINISH** its spacing-dependent dose at 20 weeks.
- After that → adult primary / catch-up. **Never replay missed kid weeks** on an
  animal past the cutoff.

### 4. Procured animals
- Recorded two-visit procurement primary → **continue to boosters**.
- No recorded primary → schedule the **adult two-visit course**.
- `origin_type=procured` **alone must NOT** falsely mark the primary completed —
  a real administration record is required.

### 5. Pregnancy and health
- Missing / unknown health or gestation → **schedule normally** (do not block).
- Explicit `sick` / `ICU` / `quarantine`, or pregnancy **month 4–5** → **defer
  existing open work**.
- **Recovery / delivery → reopen and reschedule automatically.**

**Locked health/reproductive mapping (source-backed — Slack health flow "close if
resolved / extend if disease persists", `context/source-findings/drive-docs-findings.md:357`):**

| Source value | Canonical | Vaccination behavior |
|---|---|---|
| `Open` (active problem) | `sick` | defer |
| `Extended` (disease persists) | `under_treatment` | defer |
| `Closed` (resolved) | `healthy` | schedule normally |
| ICU shed tag | `icu` | defer (overrides case status) |
| Quarantine shed tag | `quarantine` | defer (overrides case status) |
| `Pregnant`, month unknown | `pregnant` | schedule normally |
| `Pregnant`, explicitly month 4–5 | pregnancy constraint | defer |

`Closed` maps to `healthy`, NOT `recovering` — the source has no continuing
"recovering" state; a resolved case is done. This recovers **health eligibility
signals** (current health/reproductive fields), NOT the full health history —
the source also carries problems / diagnoses / disease dates / medicine /
delivery / abortion, which this importer does not load. Describe results as
"242 health eligibility signals recovered", not "health histories".

### 6. Dynamic kernel behavior (recompute)
Health / reproductive / stage / location rechecks are already wired
(`backend/internal/vaccination/app/generation_handler.go:83`). Implementation
must additionally PROVE:
- DOB / entry-date updates trigger recomputation;
- imported vaccination history triggers recomputation;
- obsolete obligations are canceled / superseded;
- deferred obligations reopen when the blocking data changes;
- projections / cards refresh after reconciliation.

### 7. FMD / source accounting
- **Preserve all 3,836 dated source facts.** The 447 FMD rows must **anchor
  future FMD recurrence** even though the approved matrix calls FMD "single +
  repeat", not a booster course.
- Verification gate reads **"3,836 source facts reconciled with zero silent
  drops"** — NOT "every fact represented through a fabricated booster rule."

## Confirmed bugs (5)

| # | Bug | Evidence | Location |
|---|-----|----------|----------|
| 1 | **447 FMD administrations silently dropped** (source 3,836 dated cells vs DB 3,389). FMD's later-course branch exits before import, so those facts never land and cannot anchor FMD recurrence. | 3836 vs 3389 | `backend/cmd/seed-vaccination-real/main.go:760` |
| 2 | **FMD recurrence anchored to the wrong (earliest) administration** — 285/308 anchor to first dose, not latest. Ex `901007000503822`: DB 2026-11-22 vs correct (latest 2026-03-20 + 9mo). | 285/308 | `generation.go` recurrence |
| 3 | **Missing-anchor deferral despite vaccination history** — 531 animals (257 no-DOB + 274 no-arrival), all with vaccination history, ~1,994 doses deferred (`due_at`=generation time) instead of continued per-vaccine. Shown as "Deferred" (implies sickness). | 1,994 / 531 | `generation.go dueAt/genOneGoat` |
| 4 | **Health / reproductive / shed-tag source signals dropped** — Demo DB has health (`Closed` 234, `Open` 7, `Extended` 1) + shed tags (`Pregnant`, `ICU`, `ICU-Kid`, `Quarantine kids`); mapper NULLs `Open/Closed/Extended`, `shed_tag` unloaded, `reproductive_status` unimported → all 1,311 blank, clinical-defer safety dead. | 242 health rows lost | `main.go:1794` + shed_tag loader |
| 5 | **Kid doses generated for animals past the cutoff** — no age gate, so >16w animals still get new kid obligations (186 kid doses scheduled onto adults; 1,756 kid-dose rows on adults). | 186 | `generation.go` / rule config |

Verified CORRECT: kid interval values (28/49/84/112/140d), per-animal grain, no
duplicate open obligations, no zero-obligation alive goats. Deferrals are
missing-anchor, NOT clinical/pregnancy.

## Fix plan

### A. Importer (`backend/cmd/seed-vaccination-real/main.go`)
- A1. Reconcile **all 3,836 dated source facts with zero silent drops**; the 447
  FMD administrations must be retained and available to anchor FMD recurrence
  (do NOT invent a booster rule to hold them). Fix the branch at `:760`.
  - **[P0] Reconcile against PERSISTED Postgres rows, not intended counts.** The
    intended-row reconciliation runs before insert; inserts use
    `ON CONFLICT DO NOTHING`, so a collision can silently discard a row while the
    report still reads 3,836/3,836. The gate must compare the source dated facts
    to the rows actually COMMITTED (and behave correctly on replay).
  - **[P1] Count RAW dated cells BEFORE vaccine-header filtering.** Unrecognized
    vaccine columns are filtered before dated cells are counted, so a renamed /
    misspelled source header could vanish outside the denominator. Count raw
    dated cells first and FAIL on any unknown vaccine header that carries a date.
- A2. Map health vocabulary per the locked table in section 5: `Open→sick`,
  `Extended→under_treatment`, `Closed→healthy` (NOT `recovering`), at `:1794`;
  never silently NULL a populated source cell. This recovers **health
  eligibility signals**, not full health history.
- A3. Load `shed_tag` → clinical / reproductive / location signals (`ICU`,
  `Quarantine`, `Pregnant`, `Non-Pregnant`, `ICU-Kid`).
- A4. Import `reproductive_status`.

### B. Generation rules (`backend/internal/vaccination/app/generation.go`)
- B1. **Per-vaccine continuation** — each vaccine's next dose from its own latest
  completion; FMD = latest administration + 9 months. Never reverse-engineer DOB.
- B2. **Anchor fallback** — DOB/arrival missing + vaccination present → continue
  per-vaccine (B1); never defer for missing DOB alone.
- B3. **Never-received vaccine + no DOB** → adult catch-up at next compatible
  drive (≤2 vaccines/visit, live spacing, health/pregnancy gates); no kid_12w/16w.
- B4. **Age transition** — new kid course starts only ≤16 wk; an in-course kid may
  finish its 20-wk spacing dose; past that → adult primary/catch-up, never replay.
- B5. **Procurement primary** — recognized only from a real 2-visit administration
  record → boosters; no record → adult two-visit course; `procured` flag alone
  does not complete the primary.
- B6. **Pregnancy/health** — unknown → normal; explicit sick/ICU/quarantine or
  pregnancy month 4–5 → defer open work; recovery/delivery → auto reopen+reschedule.
- B7. **Dynamic recompute** (section 6) — prove DOB/entry & history updates
  recompute, obsolete obligations supersede, deferred reopen, projections refresh.
- B8. Follow the APPROVED matrix, never V2's Crude-Version plan.

### C. Re-seed + re-verify (gates) — LOCKED (maintainer, 2026-07-15)

**VACC-REV-01 (reconcile by source lineage, not summed totals):** the abandoned
`verifyCommittedDatedFacts` (which summed accepted completions + completed
obligations = 2× per past fact) is REMOVED by the clean reset and must NOT be
reintroduced. A regression test must prove all 3,836 DISTINCT dated source facts
are accounted for **exactly once by source identity/lineage** (source-fact id /
idempotency lineage), never by adding completion counts to obligation counts.

**VACC-REV-02 (in-txn source verify + real seed-run state machine):**
- The persisted source-row check runs in the SAME transaction, BEFORE commit; a
  source-row drop rolls the whole transaction back.
- The seed run carries an explicit persisted STATE. It is `loading`/`generating`
  during insert + kernel generation — NEVER `ready`.
- Only a SUCCESSFUL post-generation invariant verification may mark the run
  `verified`/`ready`.
- A post-generation failure marks the run `failed`/`reset_required`. Doc-only
  "reset required" is INSUFFICIENT — the state must be persisted and
  **seed-closeout, CI, and deployment promotion MUST reject a
  `failed`/`reset_required` database**. No half-seeded DB may appear healthy or
  promotable.

- **3,836 source facts reconciled with zero silent drops, verified against
  COMMITTED Postgres rows** (not intended counts; correct on replay) — [P0];
- **raw dated cells counted before vaccine-header filtering; any unknown header
  carrying a date FAILS the gate** — [P1];
- 0 new kid obligations generated for animals past the 16-wk start cutoff
  (in-course 20-wk finish allowed);
- 0 obligations deferred for `missing_due_date` where any vaccination anchor
  exists (per-vaccine continuation applied);
- FMD recurrence anchored to the latest administration + 9 months (spot-check
  `901007000503822`);
- health / reproductive / shed-tag populated where the source provides them
  (Open→sick, Extended→under_treatment, Closed→healthy);
- **dynamic behavior PROVEN with Postgres-backed tests, not just unit** — [P1]:
  healthy→sick defers existing scheduled work; sick→recovered (Closed) reopens/
  reschedules; pregnancy month 4–5 defers; delivery/gestation change reopens;
  newly entered DOB / imported history recomputes + supersedes obsolete
  obligations. (The DOB/entry-fix producer command may not exist yet — if so,
  prove the history-triggered path and record the missing-producer as a tracked
  follow-up, do not fake it.)
- interval math, grain, coverage unchanged.
- **Do NOT push/reseed until the generation implementation (B) is independently
  reviewed and A's P0/P1 gates pass against a real Postgres seed.**

### D. Implementation status — VACC-REV-01/02 gates (2026-07-15)

Landed on `fix/vaccination-seed-reconcile-dynamic` (foundation A/B = `e4fe81c8` +
`ab4a2e59`; gates 3–7 in the follow-up commit):

- **Persisted lineage ledger (VACC-REV-01).** New table
  `vaccination_source_facts` (migration `000189_vaccination_source_facts.sql`* )
  records one row per DATED source cell keyed by a stable `lineage_key`
  (`animal_key|vaccine_header|dose_code|sequence|source_value`).
  `reconcileSourceFactLineage` proves every dated fact is accounted for exactly
  once by lineage — never by summing completion + obligation counts — and FAILS
  on any unknown dated vaccine header [P1]. A same-target collapse is recorded
  explicitly as `later_administration_merge`, never a silent drop. Regression:
  `TestReconcileSourceFactLineage*`, `TestSourceFactLineageKeyIsUniquePerCell`.
- **In-txn committed-row verify + rollback (VACC-REV-02 [P0]).**
  `verifyPersistedSourceFactsInTx` runs in the SAME transaction, BEFORE commit,
  and compares each non-excluded ledger fact to the row ACTUALLY persisted
  (set-based `= ANY`, not intended counts). A dropped obligation/completion rolls
  the whole seed transaction back. Proof (real Postgres):
  `TestVerifyPersistedSourceFactsRollsBackOnDroppedRow` (+ committed-passes twin).
- **Seed-run state machine.** New table `seed_runs`
  (migration `000190_seed_runs.sql`*) + `internal/seedrun`. The seed writes
  `loading` on a connection OUTSIDE the data tx, `generating` after commit, and
  `verified` only after the post-generation invariant verification passes; any
  error return marks it `failed` (RESET_REQUIRED) via defer.
- **Closeout / promotion rejection.** `cmd/seed-state-check`
  (`seedrun.EvaluateGate`) rejects a `failed` DB at closeout and a never-verified
  DB at promotion; wired into `tools/dev/seed-closeout.sh` and
  `make seed-state-guard`. Decision table: `internal/seedrun` `TestEvaluateGate`.
- **Event-driven Postgres proof (gate 5).** `tests/e2e`
  `TestKernelStoryVaccRev_EventDrivenRecomputeCanonicalRead` drives the
  production `HealthGoat` and `ReproductiveGoat` commands → persisted
  `outbox_messages` event → registered recheck consumer → canonical obligation
  `deferred` + `obligation_status_events` history + the canonical, projection-free
  `/vaccination/sheds` HTTP read (defer → reopen). Not a seeded projection.

*Numbers are branch-local (this branch's max was `000188`). Any later
integration onto a base that already uses `000189/000190` must renumber these two
migrations to the next free numbers.

**Gate 7 — DOB / entry-date correction producer stays UNRESOLVED.** No canonical
identity command emits `goat.identity.changed` today (`MoveGoat`/`ExitGoat`/
`StageGoat`/`HealthGoat`/`ReproductiveGoat` exist; none corrects DOB/entry_date on
an existing goat). `EventGoatIdentityChanged` and its subscription only ensure the
vaccination recompute is READY the moment that producer ships — no synthetic
producer was wired. The history-triggered recompute path IS proven (gate 5); the
DOB/entry producer is a tracked follow-up, not claimed complete.

## Open data caveats
- `procurement_hf_vaccination_evidence` has **0 rows** — the 498 procured
  animals' procurement primary is not in that table; confirm it is captured in the
  V2 vaccination history before relying on B5, else it is a data gap.
- 257 goats have blank `origin_type` + no DOB + no arrival; all have vaccination
  history (B1/B2 continue them), but identity provenance is thin.

## Do NOT
- Do not reseed with the uncommitted "Crude Version" working-tree patch (still
  cannot reconcile FMD, lets ET+TT revac fire 6 months after dose 1 while the
  booster is Pending). Build from this spec against the approved matrix. Discard
  that patch before starting clean.
