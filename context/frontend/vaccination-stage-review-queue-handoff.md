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

## Backend contract (live — verify against the generated client)

- `GET /admin/vaccination/stage-review-items?limit=&cursor=`
  → `{ items: StageReviewItem[], nextCursor: string | null }`. Keyset (cursor)
  pagination — page it (~20/page), never fetch all. The generated client response
  is **camelCase** and each item carries **only**:
  `reviewItemId`, `goatId`, `reason` (`kid_stage_past_age_cutoff`),
  `observedStage` (the stale tag), `observedAgeWeeks` (DOB-derived age), `status`,
  `createdAt`. **It does NOT include** resolution metadata, goat identity/tag,
  owner, or vaccination coverage — see the enrichment gap below.
- `POST /admin/vaccination/stage-review-items/{review_item_id}/resolve`
  body `{ "resolution": "corrected" | "exception", "note": string }` — **both
  required** (server rejects a missing/empty `resolution` or `note` with `400`).
  Marks `open -> resolved`, records who/when + `resolution_mode` + note.
  **Idempotent**: replay on an already-resolved/missing item returns `404`.

Table `vaccination_stage_review_items` (migrations `000196`, `000207`, `000208`,
`000210`). Lifecycle `open -> resolved`. **Open uniqueness is per (tenant, goat),
now enforced at the DATABASE layer** by a `(tenant_id, goat_id) WHERE
status='open'` partial unique index (`000210`). History: `000207` first added
that index, `000208` reverted to a stage-free idempotency key because the index
alone was not migrate-first-safe (a still-live predecessor's `ON CONFLICT
(tenant_id, idempotency_key)` insert would break). `000210` re-adds it **safely**
because the recorder is now an index-independent **bridge writer** (per-(tenant,
goat) advisory lock + update-else-insert + self-heal collapse): the writer no
longer depends on any one index, and the hard index makes every OTHER writer's
DUPLICATE insert fail closed — a predecessor's second stage-keyed open row now
errors (retried next pass) instead of creating a duplicate. `000210` **keeps** the
old `(tenant, idempotency_key)` open index (so a still-live predecessor's `ON
CONFLICT (idempotency_key)` FIRST insert still works during rollout — dropping it
would 42P10 every predecessor write, not just duplicates; drop it in a later
release after predecessors drain) and adds `age_cutoff_weeks`. A goat drifting
K1 → K2 **updates the one open item in place**.

### Enrichment gap (blocks the row design below — NOT built)

The list response above is the **bounded page only**. The row design wants goat
tag/identity, **owner**, and **vaccination coverage** per row — none of which are
in the response. The existing coverage API is an **aggregate by scope/protocol,
not per goat**. So "backend closed" is **inaccurate for the UI**: building the
rows would require either (a) adding identity/owner/coverage to this page
response, or (b) a **single batch enrichment endpoint** keyed by the page's
goat ids. Do NOT build it with per-row (N+1) client calls. This backend
enrichment is the first task before the lane can be built.

## CRITICAL — what "resolve" does and does NOT do

`resolve` **closes the review item with a typed mode + note** (a human triaged
it). It does **NOT** fix the goat: it does not change the stage tag and does not
generate or alter any vaccination. The operator fixes the root cause
**separately** (correct the goat's stage in the identity/correction flow), then
marks the item resolved. The server now **requires** `resolution` = `corrected`
(the stage/DOB conflict was fixed) or `exception` (explicit reviewed exception),
plus a non-empty `note` — so an active mismatch cannot be silently hidden. The UI
must not imply one click fixes the goat. Label the action **"Mark resolved"** with
a mode selector + required note (≤500 chars) — never "Reconcile" (which implies
auto-fix). For `resolution=corrected`, the server re-evaluates the goat against the
**same invariant that raised the item** (the tenant's configured kid cutoff) in one
atomic, row-locked statement, and **fails closed** if the goat is missing; if the
mismatch is still active it returns **409 `still_in_conflict`**. The UI should
surface that 409 and steer the operator to correct the stage first or choose
`exception`. Note limit is 500 characters (runes, not bytes). The re-check
evaluates against the cutoff **persisted on the item** (`age_cutoff_weeks`, the
exact effective procurement policy that raised it), not a value re-derived across
all published versions — so a later config change cannot silently move the bar an
item is judged against. A legacy/predecessor item with an **unknown** cutoff
(`age_cutoff_weeks` NULL) **fails the corrected re-check closed** (409) rather than
defaulting to an invented value; it clears only after generation re-records the
real cutoff or the operator resolves it as an `exception`.

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

Backend done: the list endpoint (cursor-paged), resolve with required
`corrected`|`exception` mode + note, and per-goat open uniqueness. **NOT done and
required before the lane can be built:** (1) per-row **enrichment** — goat
tag/identity, owner, and per-goat vaccination coverage are not in the list
response and the coverage API is aggregate-only, so add them to the page response
or a single batch enrichment endpoint (no N+1); (2) the admin-web lane itself;
(3) if the business wants it, a separate missing-dose coverage alert. (Resolve
already re-verifies a `corrected` conflict server-side and 409s if still active.)
Related:
`vaccination-process-integrity-frontend-handoff.md`, `current-admin-web-scope.md`.
