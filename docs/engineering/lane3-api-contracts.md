# Lane 3 — backend API contract and latency checks

**Status:** built, tested, and run against production read-only. Runs alongside lane 1 in
the daily `production-smoke`.

Lane 3 asks one question of every screen on the dashboard:

> When this page loads, does the server hand it usable data, fast enough that nobody notices?

It never writes. Every request is a GET against `https://api.goatos.mesha.sg`.

| Thing | Where |
|---|---|
| Catalogue | `tools/dashboard-automation/api-contract-checks.json` (58 endpoints) |
| Runner | `tools/dashboard-automation/check-api-contracts.mjs` |
| Tests | `tools/dashboard-automation/check-api-contracts.test.mjs` (35 tests) |
| Slack rendering | `tools/dashboard-automation/lib/finding-kinds/api-contracts.mjs` |
| Layer | `run.mjs`, layer `api-contracts`, env `GOATOS_DASHBOARD_API_CONTRACTS` |

---

## 1. How the catalogue was built (and why you can trust the field names)

The endpoint list is **not** guesswork and **not** copied from documentation.

1. The inventory started from the union of every `tools/perf/hot-paths.*.json` manifest —
   135 distinct GET requests, 91 distinct base paths — cross-checked against how
   `apps/admin-web` actually calls the backend (`apps/admin-web/lib/api/server.ts`, the
   per-feature `*-server.ts` adapters) and against the authoritative route table
   `backend/internal/permissions/routes.go`.
2. Each endpoint was then **called on production, read-only**, from the OCI box, and its
   real response shape recorded (structure only — key names and types, never values).
3. Every `requiredFields` and `forbiddenValues` path in the catalogue was checked against
   that observed response. **The first draft had 24 field paths that do not exist in
   production.** They were corrected against the real responses, not against the docs.
   Nothing ungrounded is in the file.

That third step matters. Several fields that documentation and the generated types imply
are simply not what production returns:

| Endpoint | Documented / assumed | What production actually returns |
|---|---|---|
| `/sales/deals` | `items` | `deals`, `total`, `limit`, `offset` |
| `/procurement/vendors` | `items` | `vendors`, `total`, … |
| `/toxin/review` | `items` | `tasks`, `status_counts` |
| `/alerts/config` | `items` | `rules`, `event_rules`, `event_kinds` |
| `/vaccination/schedule` | `rows` | `protocols`, `cohorts` |
| `/weighing/weight-demographics` | `period_start`/`period_end` | neither; `by_breed`, `by_sex`, `shed_composition`, … |
| `/feed-config/feed-items` | `items[].label` | `items[].feed_item` |
| `/counts/mortality` | `cause[].count` | `cause[].deaths` |

A catalogue written from the docs would have fired a false "required field missing" on all
eight of these, every run, forever.

### The page contract

There is **one** page-catalogue endpoint, not one per page: `GET /admin-web/bootstrap`
(`backend/internal/adminui/adapters/http/handler.go`, built by
`backend/internal/adminui/app/service.go` and narrowed per person by `compiler.go`). Pages
call `requireAdminWebPageContract("<route_id>")` and redirect away if their contract is
absent. It is the first entry in the catalogue because if it fails, **every** screen is
empty — not one.

### Enum allowlists

Unknown-enum checks read their allowed members from `contracts/openapi/app-api.yaml`, not
from a sample of today's data. Today's data is far too narrow: `/vaccination/execution`
returned only `ok`, `watch`, `broken` for `severity`, but `at_risk` is a legitimate member.
An allowlist built from one afternoon's rows would page somebody the first time a real
animal got into a different state.

The OpenAPI descriptions say why this check exists, in the team's own words —
`VaccinationCapacityStatus` carries the note *"NEVER show raw tokens in CEO UI"*. An enum
member the screen has no label for is exactly how a raw code reaches a farm manager.

**A false alarm this caught before it shipped:** production returns
`drive_capacity_state: "medical_defer"`, which is not a member of
`VaccinationCapacityStatus`. That looked like contract drift. It is not — that field is
typed `DriveCapacityState`, a different schema, and `medical_defer` is a valid member of
it. The check was not raised.

---

## 2. Why each assertion exists

Every check is tied to a repeat bug pattern in
`tools/dashboard-automation/bug-pattern-coverage.json` and to named commits. The catalogue
carries `sourceCommits` and `bugPatterns` per entry.

| Assertion | The repeat bug it comes from | Named commits |
|---|---|---|
| **No 5xx** | `sql-bind-arity-and-control-flow` — dynamic SQL compiles but drops or renumbers a bind at runtime, so a page 500s only for certain filters. | `5e4dd68ba` enforce PostgreSQL bind contracts, `0361dd110` reject mutable named args, `3c5294a76` anchor the band-edges placeholder so section pruning never drops `$31` |
| **Required field present and non-null** | `admin-web-contract-and-known-failure-screens` — a page renders its scaffolding while the data behind it is missing, and static tests stay green. | `c186fee72` wire live visual and perf guards, `165ab9f76` fail webview guard on route http errors, `e3a5e4116` harden admin web contract guards |
| **No null / NaN / empty in a rendered numeric field** | `chart-value-integrity-and-empty-scaffolds` — charts show blank scaffolding or false bars for missing values. | `38924a13e` every column carries its figure, on a phone too; `f9752acbf` wait for chart marks in route visuals; `91192e83a` charts straddling the fold draw at load |
| **No unknown enum member** | same pattern, its "internal code shown to users" half. The OpenAPI schema states the rule: the UI renders a provided label, never the raw token. | as above, plus the schema notes on `VaccinationShedStatus` / `VaccinationCapacityStatus` |
| **Latency within budget** | `api-fanout-and-latency-regression` — pages fan out into too many reads, or do per-row lookups, pushing normal APIs past 500 ms. | `a80171412` reuse live operations cache window, `8964bf79b` pipeline the detail and list reads; put the tasks endpoints on the latency gate; `docs/reviews/work-board-latency-2026-09-14.md` |

### Attribution from the history miner

`tools/dashboard-automation/lane-checks.json` (branch `auto/hist-20260923`) derives 62
lane-3 checks from 609 commits. **42 of those 62 were folded into this catalogue**, by
matching endpoint path, which raised the catalogue's attribution from 24 unique SHAs to
**275 unique SHAs**. All 275 resolve to real commits in this repo.

**Do not over-read that number.** Two honest caveats:

- The miner routes many commits to a check by **file path alone**. Of the 275 SHAs, **40
  (15%) are `docs:`, `chore:` or `test:` commits** — for example `1a0927b44`
  *"docs(feed): shorten the loads table's caption"* is attributed to the bootstrap check.
  Those are weak links. They show the endpoint's area was being worked on; they do not
  show the check proves that commit's behaviour. The strong justifications are the ones
  named in the table above, which were read individually.
- The remaining **20 miner checks were parked, not merged**, because their endpoint is not
  in the catalogue and its shape was never observed on production. Their field
  expectations are derived from commit text rather than from a real response, so folding
  them in would have produced exactly the false "missing field" noise this lane exists to
  avoid. Parked: `pc-care-tasks`, `ceo-ai-ask`, `ceo-ai-facts-match-the-page`,
  `feed-config-pens`, `feed-config-ration-rates`, `feed-analytics-experiment`,
  `feed-analytics-shed-feed`, `workforce-clock-entries`, `pen-routines`, `workboard`,
  `weighing-dates`, `leadership-tasks-detail`, `operations-audit`,
  `procurement-feed-purchases`, `market-prices`, `sales-buyer-leads`, `shifting-events`,
  and the three cross-cutting ones (`no-5xx-on-any-admin-hot-path`,
  `hot-paths-stay-under-budget`, `every-app-endpoint-refuses-a-bad-envelope`) which the
  sweep already performs on every entry by construction.

At runtime, any `lane-checks.json` row is normalised and put through the **same** gate as a
hand-written entry (`normalizeLaneChecks`). A row with no page, no human sentence, no
required field or no commit is parked with a reason and never reaches Slack. Without that,
a miner row would print a bullet reading `undefined` to a farm manager.

---

## 3. Latency: the budgets, and the noise problem

Budgets come from `tools/dashboard-automation/config.json` `apiLatencyPolicy`:
`hotPathP90Ms: 300`, `hotPathP95Ms: 500`, `normalDashboardApisMustStayUnderMs: 500`. No
threshold is invented here, and `latencyBudgetFor` caps any per-entry budget at the policy
ceiling so nothing can quietly buy itself more headroom.
`allowLongRunningPatterns` (`/bulk-upload`, `/imports`, `/export.csv`,
`/proof-media/upload`) is honoured; the catalogue deliberately contains none of them.

### The measurement, and why it is shaped this way

This was the most important finding of building the lane.

A first pass took **3 samples with no warmup** and reported **18 of 135 endpoints as slow**.
Re-measuring the same 18 with **3 warmup requests discarded and 20 measured samples**, only
**5 were actually slow**:

| Endpoint | cold p95 (3 samples) | warm median | warm p95 | verdict |
|---|---|---|---|---|
| `/operations/kernel-health` | 3435 ms | **3487 ms** | 10292 ms | genuinely slow |
| `/work-board/page` (CPT park) | 843 ms | **4750 ms** | 7249 ms | genuinely slow, and degrades under repeat load |
| `/app/vaccination/execution` (with card summaries) | 641 ms | **1955 ms** | 3005 ms | genuinely slow |
| `/vaccination/command` | 859 ms | **829 ms** | 1390 ms | genuinely slow |
| `/vaccination/command/cohort-matrix` | 566 ms | **500 ms** | 616 ms | borderline |
| `/vaccination/action-center` | 2292 ms | 17 ms | **20 ms** | cold container only |
| `/feed-analytics/stock` | 590 ms | 19 ms | **21 ms** | cold container only |
| `/calendar/vaccination/events` | 778 ms | 18 ms | **200 ms** | cold container only |
| …9 more | 560–1030 ms | 21–273 ms | 25–459 ms | cold container only |

**13 of 18 were cold-container artifacts.** Shipping the first version would have put
thirteen screens in Slack every single morning with nothing wrong with any of them.

So the sweep:

- makes **3 warmup requests that are discarded**, then **20 measured samples** — the same
  shape as `tools/perf/api-latency-gate.mjs` (which uses 5 warmup and 20 iterations), so a
  p95 here means what a p95 there means. `percentile()` is character-for-character the
  gate's function, and a test pins it by extracting and executing the gate's own source, so
  the two cannot drift;
- reports a screen as slow only when **p95 and the median are both over budget** — that is,
  it is slow more often than not. A single 900 ms response on a cold container is not
  something a person saw, and it is not a finding. `SLOW_RULE` names this in the receipt.

Date placeholders (`{today}`, `{today_minus_30}`, …) resolve in **IST**, not UTC. In UTC a
"today" filter is a day behind for the first 5.5 hours after midnight IST, so a "today"
screen would be checked against yesterday's data.

### This lane does not replace `api-latency`

`tools/perf/api-latency-gate.mjs` certifies a **build**: exact-SHA checks, warmup evidence
with a hard per-request ceiling, response-size ceilings, actor and build-SHA stability
across the measurement. Lane 3 is a broad per-run sweep that also checks response shape.
They overlap on latency only, and the `api-latency` layer is untouched. `run.mjs`'s
self-test asserts that layer still exists so this lane cannot quietly displace it.

---

## 4. The guards: GET-only and the host allowlist

Lane 3 holds the production bearer token, so the guards are enforced by construction, not
by convention. All of these are covered by tests, listed in section 6.

**One fetch call site.** `get()` in `check-api-contracts.mjs` is the only place the lane
calls `fetch`, and it hard-codes `method: "GET"`. There is no method parameter to get
wrong. A test walks every file in the lane and fails on a raw `fetch(` anywhere else.

**The allowlist is applied to the RESOLVED url, on every request.** The hosts are
`api.goatos.mesha.sg`, `api.mesha.sg`, `goatos-api.mesha.sg` — the same three as
`run.mjs`'s `runProductionSmoke`, and a test parses them out of `run.mjs` and asserts they
still match. `assertProductionApiUrl` parses with `new URL`, requires `https:`, compares
`hostname` **exactly**, and refuses a URL carrying credentials.

Validating only the base URL once is not enough, because request paths come from the
catalogue **and from `lane-checks.json`, which another automation generates**. So each
request builds `new URL(requestPath, baseUrl)` and re-checks the resolved object. This is
not theoretical: with string concatenation,

- `"https://api.goatos.mesha.sg" + "@evil.test/x"` resolves to host **`evil.test`** — the
  allowlisted host becomes userinfo and the bearer token goes to the attacker;
- `"https://api.goatos.mesha.sg" + ".evil.test/x"` resolves to host
  **`api.goatos.mesha.sg.evil.test`**.

Resolving against the base turns both into ordinary paths. The test asserts **both halves**
— that the concatenation really would escape, and that the resolution neutralises it — so
the resolution can never be swapped back for concatenation without a test failing.

**Redirects are manual.** `fetch` follows redirects by default and the allowlist would
never see the hop; a `307`/`308` preserves the method and headers. `get()` passes
`redirect: "manual"`, re-runs the allowlist on every `Location`, and caps at 3 hops. A
redirect that leaves the allowlist is refused — the request is never made — and a redirect
that stays on it is reported as a finding in its own right.

**Secrets.** Response fragments are redacted by **field name** as well as by value before
they are stored, because `lib/redact.mjs`'s catch-all pattern excludes the double quote and
therefore cannot see `{"token":"…"}` — the shape every JSON response uses. The bearer token
is never logged and never written into a result.

---

## 5. What a finding looks like in Slack

Slack gets the **page**, a link to it, and a sentence a farm manager understands. It never
gets a URL path, a field path, a status code or a check code — those live in the HTML
report (`api-contracts-report.html`) and in the receipt artifact.

Two kinds:

> **This screen's data did not load**
> • **[Weights](https://dashboard.mesha.sg/weighing/weights)** — The weights page would show a blank column where the average daily gain should be, so nobody could tell whether the animals are growing.

> **This screen is slow to answer** (should answer within 0.5s)
> • **[Operations health](https://dashboard.mesha.sg/operations/dlq)** — takes about 3.5s to answer

Rendering lives entirely in `lib/finding-kinds/api-contracts.mjs`. `notify-slack.mjs` is
touched in exactly two lines — one import and one `FINDING_KINDS` entry — on top of
`origin/auto/lane2-20260923`, which is the canonical base (lane 4's registry plus PR #367's
guarded fix) and is taken verbatim. A lane-1-only receipt renders byte-identically, which
the registry's own self-test asserts.

`issueRules()` returns `[]` **deliberately**. The registry splices registered rules into the
shared `issueRules()` that labels **lane 1's** visual failures. A regex here could silently
relabel a lane 1 issue, which the judge reproduced. Lane 3 describes its own findings
inside `renderSection`, where it cannot reach lane 1's text.

`renderReplies()` returns `[]` **deliberately, and this is load-bearing — do not "improve"
it without reading this paragraph.** A threaded reply in this message carries a screenshot;
an API finding has none, and inventing a picture would be worse than the summary line plus
the report. It also keeps lane 3 out of the Slack upload path entirely, which matters
because that path has been the source of three separate crashes during this build: an
unguarded `headline()` call, a `postSlack` catch block that threw a second unhandled
`TypeError` when handed a non-path, and a finding-kind module that took down **every**
alert including lane 1's when it threw. The registry is now guarded (lane 2's
`notify-slack.mjs`, PR #367) and lane 4 is fixing the rest, but returning `[]` means lane 3
cannot reach that code at all. Anyone adding replies here is opting back into it and needs
to re-test the upload path, not just the rendering.

### Why latency findings are not merged into lane 1's `groupSlowPages`

Two reasons, and this is a deliberate decision, not an oversight:

1. `groupSlowPages` exists to merge *one page seen on laptop and on phone* into a single
   line. A backend answer has **no device dimension** — the server replies identically to
   both — so there is nothing for it to merge. What is left after removing the device
   merge is a sort by duration, which is not a fork of anything.
2. Budgets differ by 16×. Lane 1 groups slow **page loads** against an 8 s budget; this
   lane measures **server answer time** against the 500 ms API budget. `groupSlowPages`
   prints one budget for the whole section, so merging would put a 0.6 s API next to a
   14.8 s page under one heading and one wrong budget.

The sections are therefore separate and unmistakably named — lane 1's "Slow pages" against
this lane's "This screen is slow to answer" — and a test asserts an API finding never
appears in lane 1's grouped section and never changes its budget line.

**Open point for the coordinator:** lane 4 owns and froze `renderSection(findings)`, which
passes no helpers, so `groupSlowPages` is not reachable from a finding-kind module even if
merging were wanted. If the intent really is one merged section, the registry signature has
to change and that is lane 4's call, not lane 3's.

---

## 6. Proof

Tests: `node --test tools/dashboard-automation/check-api-contracts.test.mjs` — **34 tests,
all passing**. They cover, in order of what they protect:

- the GET-only guard refusing every other method, `checkEntry` refusing a non-GET entry
  **without issuing the request**, and the structural invariant that the lane has exactly
  one `fetch` call site;
- the host allowlist matching `run.mjs` exactly, and refusing non-https, suffix lookalikes
  (`api.goatos.mesha.sg.evil.test`), substring-in-query, subdomains, and credentials;
- the two escapes the judge actually got through before the fix — `@evil.test/x` and
  `.evil.test/x` — now permanent regressions, asserting both that concatenation escapes and
  that resolution neutralises;
- an **off-allowlist redirect refused with the hop never requested**, i.e. the bearer token
  is never sent to a host outside the allowlist; and the on-allowlist hop cap;
- required-field detection (missing, null, array elements, `false`/`0`/`""` treated as real
  values, empty arrays tolerated);
- forbidden-value detection including a **nested NaN** under a declared field, and the
  proof that a NaN nobody renders is deliberately **not** a finding;
- p95 pinned character-for-character against the latency gate's own function across 1–50
  samples; warmup samples excluded from the count; a spiky endpoint producing **no** slow
  finding and a consistently slow one producing exactly one;
- redaction of JSON-quoted secrets that `lib/redact.mjs` cannot see, no token fragment
  reaching a result, and the bearer never written into a result;
- every `humanFailure` containing no URL path, field path, status code or snake_case
  identifier — **plus a test that the sentence lint itself rejects each of those**, so the
  lint cannot silently stop working.

Self-tests, all passing:
`check-api-contracts.mjs --self-test`, `notify-slack.mjs --self-test`,
`run.mjs --self-test`.

### The live run

Read-only GET against `https://api.goatos.mesha.sg` (build `7e0939befad2`) from the OCI
box, using the bearer from `~/.config/goatos/dashboard-automation.env` refreshed through
`refresh-firebase-token.mjs`. Nothing was written to production; no `POST`, `PUT`, `PATCH`
or `DELETE` was issued at any point. 58 endpoints, 3 warmup plus 20 measured samples each.

**12 findings from the 57 production-verified catalogue entries** (the outbox entry was
added after this run):

Three screens whose data did not load:

| Screen | What a person would see | What the server did |
|---|---|---|
| Operations health | The panel that says whether the system is keeping up with its own background work is itself broken | `/operations/kernel-health` answered **HTTP 500** |
| Leave and cover | The cover page cannot show who is standing in for whom | `/admin/roster/coverage` returned `items: null` — null, not an empty list |
| Sales deals | One deal in the list shows a blank where the number of animals should be | `/sales/deals` returned `deals[28].animal_count: null` |

Nine screens slow to answer, median over the 500 ms budget:

| Screen | Median | p95 |
|---|---|---|
| Vaccination command board (pen and dose grid) | 3990 ms | 8005 ms |
| Work Board | 3746 ms | 6731 ms |
| Operations audit | 3267 ms | 6464 ms |
| Vaccination command board (cohort grid) | 2246 ms | 4513 ms |
| Vaccination command board | 1809 ms | 4267 ms |
| Herd signals | 1026 ms | 1477 ms |
| Vaccination work list | 986 ms | 1215 ms |
| Vaccination pens list | 518 ms | 1220 ms |
| People | 508 ms | 1451 ms |

The Operations health result is the one worth reading twice: that endpoint reports whether
event delivery is stuck, it is both intermittently 500 and consistently multi-second, and
it is the endpoint lane 2's parked outbox check depends on.

### Why history-mined checks are off by default — the number that decided it

The same run also included 17 history-derived checks. The 57 verified catalogue entries
produced **12 findings**. The 17 history-derived entries produced **1184** — every one of
them an expectation production never promised (`items[0].shed_label is missing` on an
endpoint that has no `shed_label`, and 7 × HTTP 403 on endpoints the automation identity
cannot reach at all). Their page names were check sentences, not screens.

That is a 99-to-1 noise ratio, and it is exactly the alert channel Ravi stopped trusting.
So `normalizeLaneChecks` now parks every history-derived row by default with that reason,
behind `GOATOS_DASHBOARD_API_CONTRACTS_INCLUDE_HISTORY=1`, while they are grounded one at a
time. Their **commits are still used** — folded into the catalogue's attribution — which is
the part of the miner's work that is sound.

---

## 7. Parked, with reasons

- **All 62 history-mined checks are parked at runtime** behind
  `GOATOS_DASHBOARD_API_CONTRACTS_INCLUDE_HISTORY=1`. 42 of them contributed their commits
  to the catalogue's attribution; none of them contributes a field expectation, because
  none was verified against a production response. The live run measured the cost of not
  doing this: 1184 false findings against 12 real ones. See section 6.
- **Endpoints needing a real id** are excluded from the catalogue: `/goats/{goat_id}`,
  `/vaccination/workflows/{row_id}`, `/app/leadership-tasks/{task_id}`,
  `/procurement/source-entry/loads/{load_id}`, `/calendar/vaccination/events/{event_id}`
  and the rest. Checking them means discovering an id from a list endpoint first, which is
  a second request whose failure mode is confusing to report. Not built.
- **`/vaccination/operator-assignment/config` returns 409 `park_scope_ambiguous`** for the
  automation's identity, because that account's scope covers more than one park. That is
  correct behaviour, not a fault, so the endpoint is not in the catalogue.
- **Threaded per-finding screenshots** — not applicable, see section 5. This also keeps the
  lane out of the Slack upload path, deliberately.
- **Lane 2's `messages-not-stuck-in-the-outbox`** — **picked up, not parked.** Lane 2 parked
  it as infrastructure health belonging to lane 3, and it is now catalogue entry
  `operations_outbox_delivery`. It asserts the backend's own `OutboxHealth.status`
  (`contracts/openapi/admin-api.yaml`: `healthy` | `degraded`) is `healthy`, plus the four
  counts being present and numeric. The verdict is the system's own, not a threshold
  invented here, which is why it is a legitimate check rather than a rule of thumb. If
  event delivery stalls, something a worker recorded on the phone never reaches the
  dashboard — so the sentence a manager reads is about that, not about outboxes.
  Commits: `cfe0cfeab`, `591e4ab9b`, `60f5b8096`, `6229e3dbe`, `43f4de422`, `7a71ab29a`.
- **The HTML report is not attached to the Slack thread.** Lane 4's frozen registry has no
  attachment hook. The report is written beside the receipt and listed in the receipt's
  artifacts. If it should reach the thread, the registry needs an attachment hook.

### Two things found while building this that are not lane 3's to fix

- **Stale parameters in the perf manifests.** `hot-paths.*.json` sends `date=` to
  `/feed-direction/preview` and `/feed-packing/worklist`, which production rejects with
  *"target_date is required"*; `/vaccination/live-tracker` is pinned to `business_date=2026-09-04`,
  now outside the supported window; four `/feed-config/*` entries omit the required
  `park_id`. Nine manifest entries are recording a request failure rather than performing a
  check. This catalogue uses the corrected parameters and rolling dates. Worth fixing in
  `tools/perf/` separately.
- **`humanIssue` in `notify-slack.mjs` computes `url` and then does not return it**
  (`const url = failure.url ?? null;` with no `url` key in the returned object). So
  `groupSlowPages` always reads `undefined` and lane 1's slow-page lines never carry their
  link. This is lane 1's code and fixing it would change lane-1 rendering, so it is
  reported, not touched.
