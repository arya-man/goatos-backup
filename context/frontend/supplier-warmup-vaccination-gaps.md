# Supplier Warmup + Holding-Farm Vaccination — Frontend/Backend Gap

Date: 2026-06-25
Status: **NOT covered end-to-end. Required gap before real vaccination E2E.**

This captures the corrected status after review. It is scoped to the supplier
warmup / Holding-Farm pre-arrival vaccination use case only. It does not change
the status of the two trigger-closure screens.

## What IS done

- `/counts/herd` (Counts -> Herd Register): real `searchGoats` table, honest
  disabled write controls, gates green. Done for read-only scope only; register
  and bulk-import write drawers are still pending.
- `/operations/audit` (Operations -> Audit Log): real-data version landed by the
  backend/Codex forward fix. Verified with backend tests and admin-web build
  gates on 2026-06-25; visual smoke with populated local data is still required
  before E2E.
- Procurement -> Source Entry skeleton: journey stages (purchase -> holding
  warmup -> source health SOP -> pre-dispatch -> transit -> arrival -> accepted
  intake), holding-stay fields, and the reject-before-truck boundary
  (rejected/unresolved stay procurement history; only accepted intake flows to
  PHC). This is general source-entry tracking — NOT the supplier-warmup
  vaccination-evidence panel.

## Findings (verified against repo)

Latest mock checked:

- `mock/goatos-dashboard-mock.html` has a `Supplier warmup - Holding Farm`
  section in the vaccination area with columns Load, Holding farm/supplier,
  Purpose, Animals, Warmup, Tagging, Vaccination at HF, Health/selection, and
  Status. The copy says purpose controls warmup duration, HF doses import as
  completion evidence to avoid double-dosing, and rejected-before-truck goats do
  not ship or enter park counts.

### P1 — Warmup rule is stale: hardcoded universal 45–70 days

The frontend treats 45–70 days as the single normal warmup window for all goats:

- `apps/admin-web/features/procurement/work-state.ts:191` — `NORMAL_WARMUP_MAX_DAYS = 70`;
  `warmupMeta()` classifies 0–45 as ok, 45–70 as info, >70 as watch. No purpose
  dimension.
- `apps/admin-web/features/procurement/source-entry-board.tsx:62` — copy: "Source
  warmup of 45–70 days is normal."
- `apps/admin-web/features/procurement/load-detail.tsx:363` — copy: "source
  warmup 45–70 days is normal."
- `apps/admin-web/features/procurement/load-forms.tsx:165` — source-goat form has
  only a `warmup_days` input; there is **no purpose/classification field**
  (breeding vs fattening/non-breeding), so the UI cannot classify warmup
  correctly.

This conflicts with the committed business rule already in
`context/frontend/current-admin-web-scope.md` (~line 365): purpose-specific
source warmup — **breeding 45–70 days; fattening/non-breeding can be 0 days or
around 2 weeks**. The code is behind the doc.

### P1 — Holding-Farm vaccination evidence / no-double-dose is not built

- The generated admin client exposes only a raw passthrough
  `trusted_vaccination_history?: { [key: string]: unknown }[]` on accepted intake
  (`packages/api-client/src/generated/admin-api.ts:2046`). There is no HF-dose
  import / review / completion contract.
- `/vaccination` (`apps/admin-web/features/phc-vaccination/operations.tsx`)
  renders the regular PHC vaccination operations / status / drive surfaces, NOT
  the mock's "Supplier warmup — Holding Farm" table (load / supplier / animals /
  warmup wk / tagging / vaccination·at HF / health·selection / status).
- Therefore we cannot yet claim that imported HF doses prevent post-arrival
  double-dosing end-to-end.

### P2 — Ownership boundary

Supplier warmup **actions** belong under Procurement -> Source Entry
(`current-admin-web-scope.md:~360`). PHC / Vaccination may show **read-only**
source / trusted-evidence context after accepted intake. Do not build a second
supplier-warmup action surface under PHC / Vaccination.

### P2 — "Procurement half handled" was overstated

Stages, holding-stay fields, and the reject-before-truck boundary exist, but the
specific supplier-warmup vaccination-evidence panel (the mock table shape) does
not. Current board is general source-entry tracking.

## Required work (before vaccination E2E)

Backend contract (new, not just `trusted_vaccination_history` passthrough):

- Goat `purpose` / class (e.g. breeding vs fattening/non-breeding) captured at
  source-goat creation, so warmup expectation is purpose-specific.
- HF vaccination dose **import / review / completion** contract: record doses
  given at the Holding Farm as completion evidence keyed to the goat + protocol,
  reviewable, so generated obligations on arrival reconcile against them and do
  not double-dose.
- Regenerated client + matching OpenAPI.

Frontend:

- Procurement source-goat form: add purpose/class field; drive warmup
  classification from purpose (breeding 45–70; fattening 0/~2wk), replacing the
  hardcoded universal window in `work-state.ts` and the two copy strings.
- Procurement source-entry: the supplier-warmup vaccination-evidence panel (mock
  "Supplier warmup — Holding Farm" table) — HF doses, tagging, health/selection,
  status — as a real surface, generated-client-backed.
- PHC / Vaccination: read-only display of imported HF evidence on accepted-intake
  goats; obligations show as already-satisfied where HF doses cover them
  (no double-dose), per the protocol-engine reconciliation rule.

Ownership: actions under Procurement -> Source Entry; PHC consumes/reads evidence
only.

## E2E gate

This use case is a required gap. Real vaccination E2E should not claim the
procured-goat path is covered until: purpose-specific warmup is correct, the HF
vaccination evidence import/review/completion contract exists and is wired, and
the no-double-dose reconciliation is demonstrated.
