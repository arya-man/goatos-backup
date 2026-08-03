# Park-scope bug ledger — counts, identity, feed, feedconfig, feeddirection

**Status: PARKED, deliberately. Do not fix in a vaccination or weighing PR.**

Maintainer decision (2026-08-03): vaccination and weighing are being stabilised
first. Everything in this file is real, none of it touches those two modules,
and all of it waits.

Recorded so the findings are not lost when the review context is. Found while
judging PR #26 (`fix/review-counter-22-bf4f875`, base `origin/main` `bf4f875b`)
— an adversarial reviewer was asked to sweep the whole backend for the bug class
that PR was fixing, and it found the same class in five modules that were never
part of the original 22-item review.

## The bug class, in one sentence

A route's permission gate answers **"does this actor hold the capability
somewhere"** (the flat, park-blind role list), and then the handler takes
`park_id` — or a campaign/shed id that implies a park — **verbatim off the
request** with no check that the actor's authority covers it.

This is exactly what PR #26 fixed in weighing and vaccination. Tenant isolation
is intact everywhere below; this is park-within-tenant only.

## Verified state

`grep` for `ResolveAuthorizedParkScope | AuthorizedParkIDsForCapability |
ScopeIDsForPermission` across non-test files:

| Module | Files using any park-scope helper |
|---|---|
| `internal/feed` | 0 |
| `internal/feedconfig` | 0 |
| `internal/feeddirection` | 0 |
| `internal/counts` | 0 |
| `internal/identity` | 0 |

All five are mounted and live — `identityhttp.Register`, `countshttp.Register` /
`RegisterAppWrites` / `RegisterApprovals`, `feedconfighttp.Register`,
`feeddirectionhttp.Register`, all on `protectedMux` (`internal/bootstrap/api.go`
:787-813). `counts` has full nav (`/counts`, `/counts/birth-death`,
`/counts/shifting`, `/counts/approvals`). Real roles hold the permissions today.

They are **not** disabled. One route *inside* feed-direction is deliberately
unwired — the old instant-completion store, so the pre-gate path cannot write
`completed` at operator submit and walk around the verification gate
(`api.go:447`) — but that is one route, not the module.

## LIVE — reachable by a park-scoped actor today

**L1. `/app/counts/*` takes `park_id` unclamped.**
`internal/counts/adapters/http/app_temporary_tagged_handler.go:69` reads
`r.URL.Query().Get("park_id")` straight into the query. Gated on `CountsWrite`
only. `/app/`-prefixed routes **do** accept park-scoped grants
(`httpmiddleware.routeAllowsScopedGrants`, which admits `/calendar/`, `/app/`,
`/verification/`), so a park-scoped operator on the phone can name another park.

## LATENT — real, but needs a tenant grant to reach today

These read `park_id` verbatim, but their routes are **not** in
`routeAllowsScopedGrants`, so `routeRoles` drops park-scoped grants and the
caller must already hold a tenant grant — which sees everything anyway. No
escalation today.

**They become live the moment anyone adds an `/app/` route to these modules.**
That is precisely how the vaccination hole came to exist.

- **L2** `internal/counts/adapters/http/handler.go:47` — `/counts/breakdown`,
  gated `CountsRead`.
- **L3** `internal/feedconfig/adapters/http/handler.go:137,210,229,248,267` —
  `/feed-config/*`, gated `FeedConfigRead`.
- **L4** `internal/feeddirection/adapters/http/handler.go:428,485` —
  `/feed-direction/preview`, `/feed-direction/generation-preview`, gated
  `FeedDirectionRead` (held by `RoleOperator` and `RoleParkHead`).
- **L5** `internal/feed/adapters/http/handler.go:70,110` and
  `internal/identity/adapters/http/handler.go:95,111`.

> Correction on record: the judge reported L4 as a live cross-park read by a
> park-A Park Head. That is wrong — the route requires a tenant grant. The
> finding is real, the severity was overstated. Independently re-verified.

## Also parked — same class, different module

**L6. Two capability-BLIND park resolvers remain** (judge "F2"):
`internal/processintegrity/adapters/http/handler.go:208` and
`internal/vaccination/adapters/http/handler.go:519` still call
`ResolveAuthorizedParkScope`, which uses scope-only `HasTenantWideGrant` plus
capability-blind `AuthorizedParkIDs`. Both are authorization decisions. The
capability-aware replacement (`ResolveAuthorizedParkScopeForCapabilities`) is in
the same file and already used by their sibling modules.

Same latency caveat: admin-route patterns drop park-scoped grants, so this is
unsafe-by-construction rather than currently exploitable. `HasTenantWideGrant`
and `AuthorizedParkIDs` become dead once L6 is fixed.

`internal/vaccination` is the vaccination **config** surface, not execution —
PR #26 did not touch it. Judge the stabilisation scope on that basis.

**L7. Verify-queue category is client-supplied with no module-duty check.**
`internal/verification/adapters/http/handler.go:147,157` accept `category`
verbatim, gated only on flat `permissions.VerificationReview`; there is no
`position_module_duties` check in `internal/verification`. A Counts verifier can
substitute `category=vaccination_proof`. Park scope **is** correctly enforced
(`verificationParkScope:399` uses `ScopeIDsForPermission`), so this is
module-crossing within authorized parks, not cross-park. Pre-existing, but
PR #26's bootstrap change made `category` a first-class client parameter, so it
is worth closing when verification is next touched.

## Blocked on a product decision, not on engineering

`counts` is the herd register — "how many goats do we have, where". That is a
**tenant-wide** question by nature. Before scoping it, somebody has to answer:

> Does a Park Head see tenant-wide herd totals, or only their own park's?

Weighing's answer was obvious (a park's weighing work belongs to that park).
Counts' is not, and guessing it would be worse than the current state. The same
question applies to `feedconfig` ration rates and shed tags, which may be
legitimately tenant-global reference data.

Do not start this work by writing a park filter. Start it by getting that answer.

## Relationship to weighing — none

Weighing is deliberately isolated from every module in this file. Verified: zero
imports of `counts`/`identity`/`feed*` anywhere under `internal/weighing`, and
zero SQL joins to `goats`, `goat_identifiers` or `herd_animals`. Enforced by
`tools/agent-hooks/check-weighing-free-flow-guard.mjs`.

Weighing is **free-flow**: a scan has no expected animal set,
`weighing_observations.animal_id` was dropped in `000078` and
`weighing_expected_animals` in `000079`. The animal's identity in weighing is
the scanned tag string and never resolves to a goat record.

**Practical consequence:** fixing park scope in these modules cannot break
weighing or vaccination execution, and vice versa. No shared code, no shared
tables. Whenever this is picked up, it is safely a separate PR.
