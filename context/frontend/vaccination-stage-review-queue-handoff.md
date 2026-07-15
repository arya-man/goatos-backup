# Vaccination stage-review queue — admin-web handoff (VACC-REV-10B UI)

Status: **backend + API shipped** (`faa9dad3`, VACC-REV-10A/10B). **Admin-web lane
NOT built** — this is the remaining half of VACC-REV-10B. The paginated list
endpoint currently has zero UI consumers, so operators cannot see or work these
items.

## What this queue is (domain)

Kid goats get the PHC kid vaccination schedule up to the **20-week** cutoff, then
graduate to adult. Vaccination generation routes by the goat's **management-stage
tag**. Failure case: a goat is **past 20 weeks old** but its tag still reads a kid
stage (**K1/K2**) because the tag was never advanced. Generation reads "adult",
so it **never generates the kid vaccinations the goat still owes** — silently
missed shots. That is a medical-safety data gap
(see `docs/preventive-care-vaccination/vaccination-rules.md`).

Generation **detects** the conflict and writes a durable `open` review item
(`vaccination_stage_review_items`, migration `000196`). The lifecycle is
`open -> resolved`; dedup is scoped to OPEN items only (partial unique index), so
one goat carries at most one open item at a time.

## Backend contract (already live — do not re-implement)

- `GET /admin/vaccination/stage-review-items?limit=&cursor=`
  → `{ items: StageReviewItem[], nextCursor: string | null }`.
  Keyset (cursor) pagination — VACC-REV-10B fixed the >200-item inaccessibility;
  page it, do NOT fetch all. `StageReviewItem` fields:
  `review_item_id`, `goat_id`, `reason`, `observed_stage` (the stale tag, e.g.
  `K1`/`K2`), `observed_age_weeks` (actual age, past 20w), `status`,
  `resolved_by`, `resolved_at`, `resolution_note`, `created_at`.
- `POST /admin/vaccination/stage-review-items/{review_item_id}/resolve`
  body `{ "note": string }` → marks the item `open -> resolved`, records
  `resolved_by` / `resolved_at` / `resolution_note`. **Idempotent**: a replay on
  an already-resolved or missing item returns `404` without changing state.

Both are in the generated client (`packages/api-client/src/generated/admin-api.ts`,
`listVaccinationStageReviewItems`). Regenerate via `make api-client-generate` if
the contract changes.

## CRITICAL — what "resolve" does and does NOT do

`resolve` **closes the review item with a note** (a human acknowledged it). It
does **NOT** auto-fix the goat: it does not change the goat's stage tag and does
not generate the missing vaccinations. The operator fixes the root cause
separately — correct the goat's stage via the identity/correction flow, which
lets generation produce the right vaccinations — then marks the review item
resolved with a note describing what was done. The UI must not label the action
in a way that implies an automatic fix (the earlier "Reconcile" mock label was
misleading; use "Mark resolved" + a note field).

## Admin-web design (to build)

Add a **`Stage review`** lane/tab to the existing Action Center
(`apps/admin-web/features/process-integrity/action-center.tsx`) — a sub-view
alongside `Status board` and `SOP queues`. It is NOT a new route or screen.

- List rows from the list endpoint, cursor-paginated (~20/page, infinite scroll
  or "Load more" — never fetch the whole set; see
  `docs/decisions/mobile-data-fetch-anti-patterns.md` for the pagination rule and
  `admin-web-request-reads-guard`).
- Each row: goat identity, `observed_stage` as a "stale K1/K2" chip,
  `observed_age_weeks` as "Nw past cutoff", owner, and a `Mark resolved` action
  opening a note field → `POST .../resolve`.
- Resolving removes the row from the open list (status flips to `resolved`).

### Contract-ownership + gates (non-negotiable)

- Backend owns visible chrome: tab label, column labels, empty/error copy,
  disabled reasons, page-size semantics come from the backend contract, not
  hardcoded strings (golden frontend rule, `AGENTS.md`; audit trail in
  `admin-web-hardcoded-contract-audit.md`). Any temporary hardcode must be
  documented here before shipping.
- Before push: `npm --prefix apps/admin-web run check:mock-fidelity` +
  `make admin-web-request-reads-guard` + `make telemetry-guard` (wire a Faro
  event / route error-boundary for the new surface).
- Mock is the UI source of truth: `mock/goatos-dashboard-mock.html`.

## Follow-up

Tracked as the open half of VACC-REV-10B. Backend closed; this doc is the spec
for the admin-web lane. Related: `vaccination-process-integrity-frontend-handoff.md`,
`current-admin-web-scope.md`.
