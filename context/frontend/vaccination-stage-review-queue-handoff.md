# Vaccination stage/age-mismatch review queue — handoff (VACC-REV-10)

Status: **backend + API shipped**; **admin-web lane NOT built**. The paginated
list endpoint has no UI consumers, so operators cannot see or work these items.

## What this queue is (corrected)

It is a **stage/age data-mismatch** queue, **not** a "missed vaccinations" queue.

Every goat carries a **management-stage tag** (operational shed/cohort state —
K1, K2, … kid stages, then adult). Kid goats follow the kid vaccination schedule
up to the ~20-week cutoff (`kidWeeks` default 16, +4). Generation detects a
**contradiction**: a goat whose **DOB-derived age is past the cutoff** but whose
**tag still reads a kid stage (K-something)**. That contradiction is flagged
(`staleKidStageAfterCutoff`, `backend/internal/vaccination/app/schedule_policy.go`).

Important, and the reason the earlier framing was wrong:

- The stale tag does **not** make vaccinations disappear. A past-cutoff goat is
  routed to the **adult path**; genuinely-missing coverage is handled by **adult
  catch-up**, and accepted history suppresses already-given doses. So a flagged
  goat may be **perfectly vaccinated** — just mis-tagged.
- The flag is intentionally **independent of vaccination history**. Do NOT gate
  it on "a kid dose is missing" — that would hide genuinely stale stage/age data
  (a real data-integrity problem regardless of coverage).
- Therefore the flag can be a **false positive for missed shots** but is a **true
  positive for a stage/age mismatch**. Name it accordingly everywhere:
  **"stage/age mismatch"**, not "missed kid vaccinations".

The flag fires only when a DOB exists. **Missing-DOB goats are unaffected** — they
are scheduled off vaccination-history anchors and never enter this check.

## Backend contract (live — do not re-implement)

- `GET /admin/vaccination/stage-review-items?limit=&cursor=`
  → `{ items: StageReviewItem[], nextCursor: string | null }`. Keyset (cursor)
  pagination — page it (~20/page), never fetch all. `StageReviewItem`:
  `review_item_id`, `goat_id`, `reason` (`kid_stage_past_age_cutoff`),
  `observed_stage` (the stale tag), `observed_age_weeks` (DOB-derived age),
  `status`, `resolved_by`, `resolved_at`, `resolution_note`, `created_at`.
- `POST /admin/vaccination/stage-review-items/{review_item_id}/resolve`
  body `{ "note": string }` → marks `open -> resolved` with who/when/note.
  **Idempotent**: replay on an already-resolved/missing item returns `404`.

Table `vaccination_stage_review_items` (migration `000196`). Lifecycle
`open -> resolved`. **Open uniqueness is per (tenant, goat)** (migration `000207`
fixed an earlier defect where the idempotency key embedded the stage, so a goat
drifting K1 → K2 got two open items). The open item is **updated in place** as the
observed stage/age changes.

## CRITICAL — what "resolve" does and does NOT do

`resolve` **closes the review item with a note** (a human triaged it). It does
**NOT** fix the goat: it does not change the stage tag and does not generate or
alter any vaccination. The operator fixes the root cause **separately** (correct
the goat's stage in the identity/correction flow), then marks the item resolved.
The UI must not imply one click fixes the goat. Label the action **"Mark
resolved"** with a required note — never "Reconcile" (which implies auto-fix).

## Admin-web design (to build)

Add a **`Stage review`** lane/tab to the existing Action Center
(`apps/admin-web/features/process-integrity/action-center.tsx`), alongside
`Status board` and `SOP queues`. Not a new route/screen.

- Rows from the list endpoint, cursor-paginated (~20/page, infinite scroll or
  "Load more" — never the whole set; `admin-web-request-reads-guard`).
- Each row: goat identity, `observed_stage` chip, `observed_age_weeks` as
  "Nw past cutoff", owner.
- **Show vaccination coverage beside each mismatch for triage** — so an operator
  can tell "mis-tagged but fully vaccinated" (low urgency) from "mis-tagged AND a
  dose is actually missing" (act now) at a glance. This is display context; it
  does NOT change what gets flagged.
- Action: **Mark resolved** with a required note. Resolving removes the row.
  Prefer resolve **only after** the stage/DOB conflict is corrected; otherwise
  require an explicit "reviewed exception" reason in the note.

## Design decisions (from maintainer review)

1. Keep the stage/age mismatch flag **independent of vaccination history**.
2. Language is **"stage/age mismatch"** — drop any "shots were missed" claim.
3. UI shows coverage **next to** each mismatch for triage (not as a flag gate).
4. Resolve only after the stage/DOB conflict is corrected, else require an
   explicit reviewed-exception reason.
5. If the business also needs to find **genuinely missing doses**, build a
   **separate vaccination-coverage alert** — do not overload this queue.
6. **Do not derive management stage solely from DOB.** Stage represents
   operational shed/cohort state; DOB is used to *validate whether that state is
   plausible*, not to overwrite it. (So the stale-tag condition can't simply be
   "engineered away" by computing stage from age — the tag carries operational
   meaning DOB doesn't.)

## Gates when built

Backend owns visible chrome (tab/column labels, empty/error copy, disabled
reasons) via contract, not hardcoded strings (golden frontend rule, `AGENTS.md`).
Before push: `check:mock-fidelity` + `admin-web-request-reads-guard` +
`telemetry-guard` (wire a Faro event / route error boundary). Mock is UI truth:
`mock/goatos-dashboard-mock.html`.

## Follow-up

Backend closed (list + resolve + per-goat open uniqueness). Open: the admin-web
lane above, and — if the business wants it — a separate missing-dose coverage
alert. Related: `vaccination-process-integrity-frontend-handoff.md`,
`current-admin-web-scope.md`.
