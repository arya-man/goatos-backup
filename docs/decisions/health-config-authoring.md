# Health Config — authoring treatment protocols in the app

**Status:** Accepted · maintainer decision 2026-08-06
**Supersedes:** the import-only posture of `ReplacePublishedProtocols` (migration `000098`)
**Code:** `backend/internal/health/**`, `apps/admin-web/features/health/**`,
migration `000121_health_protocol_authoring.sql`

## The decision

Treatment protocols — per disease, per age band, the day-by-day course of medicines, actions and
critical handoffs — are authored in admin-web at **`/health/config`**, backed by `/health-config/*`.

Four things were decided together, and each changes what got built:

| Question | Decision |
| --- | --- |
| Who owns the truth after this ships? | **The web.** The Google Sheet import becomes a one-time bootstrap. |
| What happens to the old value on an edit? | **A new version.** Publishing retires the version it replaces; nothing is overwritten. |
| How much is editable? | **Full step authoring** — add/remove/reorder steps, edit medicines, dosages, units, routes, instructions, critical handoffs, the number of days, and add diseases. |
| Who may edit? | **`ceo_internal` + `health_director`**, via `health.config.read` / `health.config.write`. |

## Why versioning, not in-place editing

A dosage is an instruction a field operator administers to an animal without re-deriving it. Three
properties follow, and the version model is what delivers all three:

1. **A goat mid-treatment finishes on the dosages it started on.** `health_cases` pins
   `health_protocol_version_id` at diagnosis, and `health_session_steps` holds that case's own copy
   of the steps. Publishing changes what the NEXT diagnosis loads, never what an animal currently
   being treated receives.
2. **The version an animal was actually treated from stays readable forever.** Retired versions and
   their steps are kept. "What dose did this goat get in March" is answerable.
3. **Nothing an author types reaches an operator until they publish.** Editing copies the published
   version into a draft; the live protocol keeps serving diagnoses untouched while the draft is
   worked on.

The schema built in `000098` already supported this — `version`, `status IN
('draft','published','retired')`, one published row per `(tenant, disease, age band)`. Migration
`000121` adds only what AUTHORING needs on top: at most one open draft per protocol, an
idempotency/audit ledger, and `created_by` / `updated_by`.

## Why the sheet import is now locked

`ReplacePublishedProtocols` retires EVERY published protocol and republishes the supplied set. Run
after an author edits a dosage in the app, it would discard that edit — and because it publishes a
new version rather than mutating one, it would do so with no constraint violation to notice and no
error to see.

It therefore fails closed once any version carries the authored source ref
(`health-config:app`), returning `ports.ErrImportAfterAuthoring`. The check is inside the import's
own transaction, so an author publishing while an import runs is caught rather than raced past.

`ReplacePublishedProtocolsOverwritingAuthored` is the break-glass form for a maintainer
re-bootstrapping from a corrected sheet. It is a separate exported method rather than an env var so
that using it is visible in reviewed code.

The check reads the source ref rather than "any version > 1" deliberately: repeated pre-authoring
imports legitimately produce versions 2, 3, …, so the question is not how many versions exist but
whether a human authored one in the app.

## Why `/health/config` is an allowed module surface

AGENTS.md forbids a vertical from nesting a duplicate of the top-level Admin/Data Ops `/config`
screen. This is the SECOND recorded exception to that rule, alongside `/feed/config`, and it is the
same shape:

- A treatment protocol is a **day-by-day medication document** owned by the Health module and served
  by `/health-config/*`. It is not a protocol `rule_dsl` row.
- `/config?category=health` cannot render it. That screen shows governed protocol rules; this one
  shows an ordered course of medicine / dosage / unit / route / instruction per day and session.
- `/config` remains the single generic protocol-rule authority screen. The backend contract
  classifies `/health/config` as `module-surface`, not `authority-screen`, and ships it as a Health
  nav leaf.
- No command lens (Control Tower, Action Center, Calendar, Protocol Adherence, Workflows) is
  duplicated under `/health`, and none may be.

Machine-enforced by the two-entry allowlist `MODULE_SURFACE_ROUTE_EXCEPTIONS` in
`apps/admin-web/scripts/check-ia-guard.mjs`. Widening it again needs a new recorded maintainer
decision first.

## Why `health_director` gains authoring rights

`health_director` was created as the COUNTS owner (maintainer decision 2026-08-01), which is an
extension of the handbook rather than something written in it. This grant is the opposite: it is
squarely inside `Health_Director.pdf`, whose Responsibilities 1–4 put observation, diagnosis,
treatment and treatment tracking on that desk. The protocol IS the treatment standard those
responsibilities are carried out against, and the role already holds `goat.write_health` to record a
clinical fact about one animal.

It comes with **no** Preventive Care permission. `pc_director` and `health_director` are separate
departments and merging them is prohibited; vaccination protocol authoring stays on `/config` behind
`ProtocolWrite`, which `health_director` does not hold.

`health.config.write` is deliberately NOT granted to:

- `operator` — executes a course; does not author it.
- `park_head` — runs a park's execution.
- `pc_director` — different department.
- `verifier` — separation of duty: the verifier checks captured work and must not be able to rewrite
  the standard that work is judged against.

## Validation is validate-or-reject, and stricter at publish

A **draft** may be incomplete: zero steps, a step past the last day, a medicine with no route. That
is the point of a draft — an author saves and returns to it.

**Publish** applies the full rulebook against what is actually STORED (re-read inside the publish
transaction, not trusted from the last save):

- at least one step;
- no step on a day beyond the course length;
- no day within the course with no step at all;
- every medicine step carrying a route, and a dosage amount and unit that are either both present or
  both absent.

Every offending field is returned together, each naming `steps[i].field`, because a 28-step protocol
rejected one field per round trip is not authorable.

Nothing is auto-fixed. Silently deleting steps past a shortened duration, or silently extending the
duration to cover them, are each a guess about a medical document.

## Shapes worth knowing before changing this

- **Adding a disease creates BOTH age bands.** The phone picks the protocol from the goat's own age
  band, so a disease authored for adults only fails at diagnosis for a kid with "protocol not
  published" — which reads as a bug rather than a deliberate gap. The two drafts start identical and
  are edited apart, which is exactly the shape of the imported data (25 of 27 diseases are identical
  across bands; 2 genuinely differ).
- **Steps carry no client-supplied `seq`.** Order is positional and the server assigns 1..N.
  `health_protocol_steps` is UNIQUE on `(version, seq)`, so a client-driven renumber collides with
  itself halfway through a reorder. Saving replaces the whole ordered list in one statement.
- **A rename applies to the disease, not one band.** Both bands are the same illness; letting them
  drift would show an operator one name on an adult and another on a kid.
- **`content_hash` means two different things.** An app-authored row hashes THAT protocol's own
  content, which is what lets an identical re-save return `unchanged` instead of churning out an
  indistinguishable new version. A sheet-imported row carries one hash for the whole import
  snapshot, shared by every protocol in it — which is why it cannot distinguish two versions of one
  disease.
- **`health_config_write_log` is a ledger, not columns on the version row.** A publish spans two
  rows (one retired, one published), a discard leaves none, and an `unchanged` save writes no
  version row at all — so the write's identity belongs to none of them.

## Machine gates

- `make ci-local` — the standard path.
- `npm --prefix apps/admin-web run check:mock-fidelity` — includes the IA guard carrying the
  two-entry exception list.
- `npm --prefix apps/admin-web run check:ui-contract` — the four authoring vocabularies (session,
  step type, route, unit, critical-action type) come from the backend contract's option groups, keys
  AND labels, so a list maintained in the client cannot drift from the set the backend accepts.
