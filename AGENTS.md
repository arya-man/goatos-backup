# Goat OS Workspace Agent Context

## PR Review + Land Main Rule

When the maintainer asks to review a GitHub PR and land main, the task is not
done after pushing the certified commit to `origin/main`. After local CI passes
and `make land-main` lands the commit, also resolve the GitHub PR itself:

1. Verify the PR head branch and `origin/main` both point at the landed SHA, or
   merge the PR through GitHub if it is still mergeable and not already landed.
2. If the PR branch is stale but the exact PR content is already in `main`,
   update the PR head branch to the landed SHA so GitHub closes the PR as
   resolved.
3. Report the PR state separately from the main SHA. If GitHub cannot mark it
   "Merged" because the branch already equals `main`, say that explicitly.

## GCP Billing Console Landing Rule

When investigating Google Cloud billing for Goat OS, always land directly on
the working billing reports page for the Mesha account:

```text
https://console.cloud.google.com/billing/01FEDE-96BCB3-76D992/reports?authuser=2&organizationId=563962826703&project=goatos-stg
```

Use the `ravi@mesha.sg` Google account. Do not use the personal Gmail accounts
for billing reports; they land on the Google Cloud "You need additional access"
error and are missing permissions such as `billing.resourceCosts.get`.

For the 2026-08-26 billing investigation, the reports page showed August
forecasted cost of about `₹34,382.31`, mostly driven by Cloud Run
(`₹17,869.22` for 1-25 Aug), then Cloud SQL (`₹4,954.30`) and BigQuery
(`₹2,152.09`). Always read the report table before guessing from the overview
balance.

## Vaccination Anchor Date Rule

When the maintainer tells Codex, Claude, or any other agent to add a vaccination
drive, anchor date, campaign date, baseline date, or "start from this date" for
one or more vaccines, treat that date as a **vaccine timeline anchor**, not as a
manual one-off obligation insert.

Read the detailed operational runbook before changing anchor code, config, or
data: `docs/preventive-care-vaccination/vaccination-anchor-runbook.md`.

Required behavior:

1. Resolve the exact vaccine/program name the maintainer used. For example,
   `Z1+Z3` is the vaccine/program label, not separate `Z1`, `Z2`, or `Z3`
   management stages.
2. Resolve the intended animal set from live herd scope: park, shed,
   partition, species, sex, current stage, and explicit RFID/tag identifiers
   where relevant. Report animal identifiers as actual RFID/tag values, not
   internal goat ids.
3. Clear, cancel, or supersede bad old obligations only when asked, and keep
   that separate from the new anchor. Old missed rows are history; do not assume
   deleting or canceling them will make the sweeper invent a new campaign.
4. Create or configure the anchor through the vaccination generation/kernel path
   so future boosters and revaccination are derived from the anchor date.
   Do not blind-insert a single drive row unless the maintainer explicitly asks
   for a one-off data repair and accepts the loss of future-rule semantics.
5. Before claiming a date is scheduled, verify same-day and cross-vaccine
   safety: live/live, live/killed, killed/live, killed/killed, maximum vaccines
   per session, booster gaps, existing future obligations, and accepted vaccine
   history. If another vaccine lands on the requested date, the backend/kernel
   must either keep a medically compatible pair or push the lower-priority /
   overflow work forward by the configured safe-gap rules.
6. Respect operator-day packing: default cap is 200 animals per operator-day,
   counted by animals, not doses. Fill with complete sheds first. For partitioned
   sheds with a common parent, such as `Mandela 1 - Part 1` through
   `Mandela 1 - Part 8`, keep sibling partitions together before mixing
   unrelated sheds when they fit safely under the cap. If complete buckets total
   180 and the next whole shed would exceed 200, keep 180 and carry the next
   shed/group forward instead of splitting it.
7. Never invent an operator fallback. A vaccination drive assignment's
   `operator_id` must be an active workforce member whose
   `primary_location_id` is the same park as the assignment's `park_id`.
   If `vaccination_operator_assignment_config` is missing for a park, stop and
   fix the park config; do not use another park's default operator. Any manual
   SQL repair must include a pre-commit check that no assigned operator belongs
   to a different park.
8. After generation, report what actually happened: animals scheduled on the
   requested anchor date, animals pushed to another date, the reason for each
   push, remaining missing work, and next booster/revaccination dates.

For the current Goat OS vaccination rules, `Z1+Z3` is goat + sheep, killed,
bacterial/toxoid, first course at 4 weeks with booster at 7 weeks, and
revaccination every 6 months. If the maintainer says "all kids and adults Oct
15", that means anchor all selected live animals on October 15 and let the
kernel apply compatibility and future scheduling from there.

## Ravi Laptop Default: OCI DB, Not Local Docker Postgres

On Ravi's laptop, default local Goat OS backend/admin-web development to the OCI
Postgres tunnel when it is available:

```text
Database: postgres://postgres:${REMOTE_POSTGRES_PASSWORD}@127.0.0.1:15432/goatos?sslmode=disable
Tunnel:   127.0.0.1:15432 -> OCI VM 127.0.0.1:5432
```

Current OCI dev VM connection details, credentials, and recovery metadata must
live outside git. Resolve them from the operator's local environment or Google
Secret Manager; do not commit account names, public IPs, laptop home paths, SSH
keys, or Postgres passwords.

Do not install or start Colima, Docker Desktop, Docker CLI, Lima, `goatos-local-current`,
or any other local Postgres container just because older local-stack docs mention
`5433` or a CI gate asks for Docker. Before any Goat OS work that appears to need
Docker/Colima on Ravi's laptop, first check whether the OCI tunnel on `15432` is
active and whether the task can use OCI instead. For `make land-main`,
`validate-sqlc-plans`, query-plan proof, or any other disposable Postgres proof,
use an OCI-hosted throwaway DB/container and clean it after the landing attempt;
do not install Docker/Colima locally as the workaround. Use local Docker Postgres
only when the maintainer explicitly asks for a disposable/local Docker DB, a
Docker-specific integration test, or an isolated mutation test that must not touch
OCI/staging-like data. If a stale `goatos-local-current` container or Colima VM is
running while the active dev stack uses OCI, stop it instead of treating it as
canonical.

**HARD RULE - OCI/STG E2E data repair is delta-only.** Ravi's OCI database is a
maintained staging-like clone, not a disposable target. For any OCI/STG E2E,
parity, or validation task, first identify exactly which tables/rows differ from
STG and repair only that delta. Do not replace, reset, drop schemas from, or
full-restore the entire OCI database from STG unless the maintainer explicitly
asks for a full refresh using those words after being told it will overwrite the
OCI database. A normal request to "make OCI match STG" means: run a diff, capture
the mismatched table/row delta, apply the smallest targeted SQL or copy for that
delta, then re-run parity.

## Legacy Local Stack Canonical Ports

For local Goat OS browser/debug work, use one shared local stack unless the user
explicitly asks for an isolated throwaway stack:

```text
Frontend: http://127.0.0.1:3300
Backend:  http://127.0.0.1:8080
Database: postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable
Docker DB container: goatos-local-current
```

This `5433` Docker DB setup is legacy/local-only on Ravi's machine and is not
the default when the OCI tunnel is active.

Before cloning, seeding, importing, or debugging local data, first verify the
running backend's `DATABASE_URL`. On Ravi's laptop, prefer the OCI tunnel above;
use the legacy `5433` Docker DB only after an explicit Docker/local DB request.
Do not infer the local DB from a previous temp worktree, a random Docker port,
or a stale shell variable. If a temp stack is unavoidable, clearly label it as
throwaway and do not call it "the local DB".

**HARD RULE - No circular OCI/E2E retries.** Before rerunning any long OCI DB,
generation, Chrome E2E, CI, or landing command after a failure, identify the
specific changed condition that makes the retry different: a code patch, data
repair, tunnel repair, config change, or narrower diagnostic. Use an explicit
timeout and capture the terminal result. If the same command fails twice with
the same blocker, stop repeating it and switch to diagnosis or report the exact
blocker; do not start another blind long run.

**HARD RULE - UI fixes require real-surface proof after the final edit.** Claude
and Codex must not call a UI fix done from code/tests alone. For any
`apps/admin-web` browser-visible change, reload Chrome on the exact target URL
after the last code edit and verify the changed UI is actually rendered there.
For any Android/operator-mobile change, open the app on the physical phone or
emulator target required by the task and verify the changed screen there.
Static tests, typecheck, backend API checks, and screenshots from before the
last edit are not enough.

**HARD RULE - ADB text is literal, not URL-decoded.** When entering credentials
or any literal text with `adb shell input text`, never encode `@` as `%40`;
`adb input text` types `%40` literally. Use a literal escaped at-sign such as
`natheswar7\@gmail.com`, then verify the field text in the UI hierarchy before
tapping submit/sign-in.

**HARD RULE - UI work requires BOTH visual regression and E2E.** For every
browser-visible `apps/admin-web` change, after the final code edit and before
reporting done or pushing as ready, agents must complete both checks on the real
surface:

1. Visual regression proof: open the exact changed route in Chrome, capture the
   rendered screen after the final edit, inspect it for layout/copy/state
   regressions, and compare it to the authoritative mock/design or the user
   screenshot that reported the defect.
2. Click-through E2E proof: exercise the changed controls end to end in Chrome,
   including disabled/enabled states, changed checkbox/select/input values,
   preview/apply/save/publish buttons, close/cancel paths, and the expected
   backend result or blocked-safe boundary.

Do not stop at one of the two. A screenshot without clicks is not E2E; a passing
click path without a post-edit screenshot is not visual regression. If either
check is blocked by server startup, auth, data, network, or a browser-control
failure, diagnose and fix the blocker when it is in-repo or local-state
controllable. Only report "blocked" after naming the exact blocker and the exact
command/browser step that proved it. Do not say the UI work is done until both
visual regression and E2E are actually complete after the final edit.

For any change that touches `apps/admin-web` Weights UI, Weights page copy,
Weights charts, generated API contracts used by Weights, or backend read-model
data consumed by `/weighing/weights`, verify the local Chrome page is not on
`ERR_CONNECTION_REFUSED`, not showing backend-down copy, and not showing the
React "Something went wrong" fallback. For chip/label/calendar changes, verify
the actual rendered row labels, chips, calendar markers, and tooltip/info text
in Chrome. If Chrome/phone verification is blocked, say it is blocked; do not
present the UI fix as verified.

When the user says "my local DB" or "local frontend/backend", treat that as:

```text
Chrome -> 127.0.0.1:3300 -> 127.0.0.1:8080 -> 127.0.0.1:5433/goatos
```

For physical Android phone scan/RBAC testing, do not mutate the normal local DB.
Use the reset-first throwaway database and runbook:

```text
Database: postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable
Docker DB container: goatos-phone-qa
Runbook: docs/runbooks/phone-qa-throwaway-rbac.md
```

## Android CLI Bootstrap - Claude AND Codex

Before any Goat OS Android developer command, Claude, Codex, and human
developers must ensure Google's Android CLI is available. Use the repo helper;
do not hand-roll separate install steps:

```bash
bash tools/dev/ensure-android-cli.sh
```

The helper is idempotent. If `android` is missing, it installs the user-local
Android CLI for the developer's platform, runs `android update`, runs
`android init`, and runs `android skills add --all` so Codex, Claude, and other
detected agents receive the official Android skills. If `android` is already on
`PATH`, the helper stays quiet unless the base Codex/Claude Android CLI skill or
the broader official skill set is missing. The Android entrypoints
`make android-doctor`, `make android-emulator-ensure`, and `make
android-dev-run` already run this first; agents that call lower-level Android
scripts directly must preserve that bootstrap.

For what Android CLI and Journeys are allowed to prove in Goat OS, read
`docs/mobile/android-cli-and-journeys.md`. Journeys supplement the existing
Gradle/Paparazzi/phone-QA gates; they do not replace them.

**HARD RULE — phone/mobile QA must NEVER use or repoint the default ports.**
`127.0.0.1:3300` (admin-web), `127.0.0.1:8080` (API), and `127.0.0.1:5433`
(database) carry the maintainer's LOCAL REPLICA OF STG DATA. Phone QA is mock
scan data (the throwaway 20-animal seed). Never start, stop, kill, restart, or
repoint anything on `3300`, `8080`, or `5433` for mobile testing, and never free
a default port by killing whatever holds it — parallel agent sessions (Claude
and Codex) share this laptop, and the process you kill is another session's
stack.

Run the phone-QA API on a NON-DEFAULT port and remap the tunnel instead. The
device always calls its own `localhost:8080`, so only the host side moves — no
APK rebuild and no token re-mint are required:

```bash
# phone-QA API on 8081 -> throwaway DB 127.0.0.1:15544
# set GOATOS_HTTP_ADDR=127.0.0.1:8081 for that backend process
adb -s <serial> reverse tcp:8080 tcp:8081
```

Before telling the maintainer a physical-phone run is clean, verify this exact
single-target chain and write the result in the handoff:

```bash
lsof -nP -iTCP:8081 -sTCP:LISTEN
ps eww -p <8081-pid> | tr ' ' '\n' | rg 'GOATOS_HTTP_ADDR|DATABASE_URL'
adb -s <serial> reverse --list
```

Expected for phone QA:

```text
host API: 127.0.0.1:8081
host DB:  postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable
device:   tcp:8080 -> host tcp:8081
```

If any of those three do not match, stop and fix the target before scanning.
Do not debug vaccine counts, disappeared rows, submit state, or proof upload
state until this target chain is proven. A mismatch means the phone and psql are
looking at different worlds.

Before a fresh vaccination phone test, clear both durable sides before launch:
backend vaccination scan/proof/submission rows in the throwaway DB, and Android
Room/app data for the visible profile. Then install, set `adb reverse`, launch,
and screenshot-check the actual phone. Do not launch first and clear later. A
clean test starts from no `sop_task_scan_*`, no vaccination completion/rejection,
no proof artifact/outbox rows for the prior run, and an app profile cleared via
the visible Android user.

For Android physical-device work, the target-chain proof is not enough. Every
time an agent installs, clears, launches, relaunches, changes `adb reverse`,
changes backend process/port, changes DB seed/reset state, or asks the
maintainer to test, the agent must also verify the actual opened phone screen is
showing data from the throwaway DB. Use a real device screenshot or focused UI
inspection after launch, and compare the visible shed/task/counts/tags against a
fresh SQL read from `127.0.0.1:15544`. Do not stop after "installed" or "DB
cleared"; the handoff is only valid after the phone is open, focused, and
showing the expected throwaway data.

This supersedes any earlier wording suggesting the backend behind `8080` may
swap its database target. Taking `8080` for phone QA caused a real incident
(2026-08-03): the maintainer's `5433`-backed API was killed to free the port,
`8080` was pointed at the throwaway `15544` database, and admin-web then showed
the 20-animal mock set in place of the 324 CPT adults — while the phone's
`adb reverse` still aimed at `8080`, one port flip away from writing mobile scan
data into the stg replica.

The fixture intentionally maps five physical vaccination RFIDs into ten goat
identities across CBE and CPT while preserving the production uniqueness rule on
`goat_identifiers`; Weighing remains free-flow and must keep raw RFID input.

## Weighing Is ISOLATED — No Herd, No Vaccination, No Exceptions (Claude AND Codex)

Weighing owns its own tables and reads NOTHING from another module's schema, in
either direction, on ANY path — writes, reads, reports, read models, exports,
analytics. It is not "free-flow on the write path". It is isolated.

BANNED on every path: `goats`, `goat_identifiers`, `herd_*`, `vaccination_*`,
`sop_*`, `protocol_*`, `obligation_*` — anything describing an ANIMAL or another
module's rules. Weighing knows a scanned string and a weight. It does not know
what animal that is and must never ask.

RECORDED REPORTING EXCEPTIONS (maintainer decisions 2026-08-07 and 2026-08-19). The admin-web Weights
screen reports average weight by BREED, SEX and MANAGEMENT STAGE. Those three
facts live only on the animal, so exactly one file may resolve a scanned tag:
`backend/internal/weighing/adapters/postgres/weight_demographics.go`, allowlisted
BY NAME in `check-weighing-free-flow-guard.mjs` (`HERD_JOIN_EXEMPT_FILES`) and
permitted `goats` + `goat_identifiers` for same-animal reporting only. The same
file may read `goat_shed_partitions` only to label lump-sum Weights read-model
rows by the exact `(shed, partition)` resident cohort (`Godel 2 - Part 1`,
`Castro 1/2/3`, `Gandhi 1/2/3`, legacy `Gandi 1/2/3`). It must not use that
table to gate capture, submit, close, expected animals, or any write path.
Everything else stays banned, on
every path, in every other weighing file — the exemption is file-scoped precisely
so it cannot leak to the write path, which is the 2026-08-04 defect.

What keeps it safe, and what a future change must preserve: it is READ-ONLY; it
is a reporting path with no capture, submit or close behaviour; NO scan is gated
on identity; and a tag that resolves to nothing is COUNTED and reported, never
rejected — free-flow capture is untouched. The same file may return row context
chips such as "F2 / female" or "Anantapur Sheep / male" for the admin-web
Weights table; those chips label the weighed shed row and must not become a
write-path lookup or validation rule. A whole-shed weigh has no tags and is
attributed by the shed's own cohort only when that cohort is homogeneous for the
reported dimension. Mixed whole-shed averages may be labelled with multiple
breed/sex chips, but are never split across breed or sex buckets, because
splitting one shed average across a mix invents a distribution nobody measured.
Widening this exemption — another file, another table, or any write path — is a
MAINTAINER decision, never a developer convenience.

SECOND RECORDED EXCEPTION (maintainer decision 2026-08-24): the LUMP-SUM CENSUS
SNAPSHOT. Operators kept typing wrong lump-sum head counts, so the operator no
longer enters one: `RecordShedObservation` snapshots the bucket's live resident
count from `goats` + `goat_shed_partitions` INSIDE the submit transaction via
exactly one file — `backend/internal/weighing/adapters/postgres/lump_sum_census.go`,
allowlisted BY NAME in `check-weighing-free-flow-guard.mjs` — stores it frozen on
`weighing_shed_observations.animal_count`, and derives the average from it. The
snapshot never changes afterwards: herd moves do not recompute it, replays return
the original, and the verifier's weight correction is WEIGHT ONLY on both grains
(a correction naming a count is refused, `animal_count_not_applicable`; the
verification spec no longer declares a count field). A register-empty bucket
refuses the submit (422 `shed_count_unavailable`) rather than inventing a count.
This is knowingly a WRITE-PATH read and is recorded as such; its boundaries — one
COUNT of the bucket's own (shed, pen), no per-animal identity, individual
free-flow capture untouched — are stated in the guard header and the census file
itself. Canonical prose: `docs/decisions/weighing-lump-sum-census-count.md`.

THIRD RECORDED EXCEPTION (maintainer decision 2026-08-26): the WEIGHTS SEX FILTER. The
admin-web Weights page carries a **Sex** filter in its own filter bar, beside Weighing, and it
governs the WHOLE page — every KPI, the shed table, both leaderboards, the load chart, the
Growth Director widgets and the breed gain card. A page whose cards disagree about which kids
they counted has no true number on it, which is why this is a page filter and not a card
control. A weighing row knows only a scanned string, so exactly one more file may resolve it:
`backend/internal/weighing/adapters/postgres/sex_scope.go`, allowlisted BY NAME in
`check-weighing-free-flow-guard.mjs`.

That file answers "which weighs belong to this sex" ONCE and hands the other reads an OPAQUE
list — tag strings and (location, partition) buckets — so `shed_weights.go`, `growth.go`,
`load_weights.go` and the Growth Director reads still name no herd table and still know nothing
about animals. Letting each of them join `goat_identifiers` instead is exactly the leak the
2026-08-04 defect was about. It is READ-ONLY and REPORTING-ONLY: no capture, submit, close or
verdict path calls it, NO scan is gated on identity, and an empty sex resolves to an empty scope
that every caller reads as "no filter", so the unfiltered page runs the query it ran before this
file existed and reads no goat row at all.

An individual weigh is claimed through the animal its tag resolves to. A WHOLE-SHED weigh has
no tag and is claimed only when its shed's resident cohort is entirely that sex — the
maintainer's own rule is that a lump-sum shed holds one sex — and a shed the register shows as
mixed is claimed by NEITHER side rather than split, because one shed average cannot be divided
between two cohorts. A tag that resolves to nothing is still recorded and still counted in the
unfiltered view; it simply cannot answer a question about sex, so the filtered halves do not add
up to the unfiltered total, and that gap is honest rather than missing data.

FOURTH RECORDED EXCEPTION (maintainer decision 2026-09-01): the WEIGHTS ORIGIN FILTER, FARM BORN
vs PURCHASED. Beside Weighing and Sex, and governing the WHOLE page on the same terms: the farm
both breeds its own kids and buys them in loads, and the two grow differently enough that reading
them together answers nothing. Exactly one more file may resolve identity,
`backend/internal/weighing/adapters/postgres/origin_scope.go`, allowlisted BY NAME in
`check-weighing-free-flow-guard.mjs`.

IT DID NOT NEED AN EXCEPTION AT FIRST, and why it does now is the whole rule. Origin looked like a
fact about a PEN -- the farm buys a load and puts it in a shed -- and weighing already owns that
mapping in `weighing_shed_load_tags` (000131), so the first version read no herd table at all. That
is correct for the seven pens whose every resident came off a load (CBE Castro 1/2/3, CPT Castro
1/2, CPT Godel 2 - Part 1/2). It is WRONG for a MIXED pen: CPT Mandela 1 - Part 1 holds 13 kids of
which only FOUR were bought, and judging a scanned weigh by its pen filed all 12 of that pen's
scanned kids as purchased. The maintainer caught it on the first run.

So the rule is PER ANIMAL where the evidence allows it. A SCANNED weigh carries a tag, so it is
claimed through the animal that tag resolves to, and `procurement_load_goats` -- allowlisted for
THIS FILE ONLY -- is the only table that says which animal came off which load. A WHOLE-SHED weigh
carries no tag, so it is claimed through its pen and ONLY when every live resident agrees: all
bought, or none. A mixed pen is claimed by NEITHER side, because one average weight cannot be
divided between two cohorts and claiming it whole is the same defect one grain up. This is the
identical agree-or-neither shape the Sex filter uses for a shed holding both sexes.

The guard's table allowlist is now keyed PER FILE, precisely so this cannot leak: a procurement
table is legal in `origin_scope.go` and still a finding in `sex_scope.go`, `weight_demographics.go`
and `lump_sum_census.go`, none of which has any business asking where an animal was bought. That
per-file scoping has its own adversarial self-test.

It is READ-ONLY and REPORTING-ONLY: no capture, submit, close or verdict path calls it, NO scan is
gated on origin, and an empty origin resolves to an empty scope every caller reads as "no filter",
so the unfiltered page runs the query it ran before this file existed. A tag that resolves to
NOTHING is claimed by neither side -- it is still recorded and still counted unfiltered, it simply
cannot answer where the animal came from -- so the filtered halves need not add up to the
unfiltered total, and that gap is honest rather than missing data.

ROAD TO SALE COUNTS WHOLE-SHED PENS TOO (maintainer decision 2026-09-01, same day): the Growth
Director's band board read `weighing_observations` alone and so answered "where does every kid sit"
from scanned tags only -- 501 kids while 555 more sat in nine pens. A pen now contributes ALL its
animals at the pen's average, in the bands AND in moved-up/held/slipped-back, the same trade the
daily-gain headline already takes: a pen creeping 24.9 -> 25.1 kg moves every kid in it, and a pen
average also moves when animals enter or leave. The losing-animals list stays scanned-only, because
it NAMES individual animals and a shed average cannot name one. Wire fields renamed with the
meaning (`identity_count` -> `animal_count`, `BandMovement.pair_identities` -> `pair_animals`),
leaving the tag-matching counts untouched since a pen carries no tag to match. Canonical prose:
`docs/decisions/weights-origin-filter.md`.


ONE DAILY-GAIN NUMBER, AND WHOLE-SHED PENS ARE IN IT (maintainer decision 2026-08-26, same day,
SUPERSEDING the individual-only headline). The farm's daily gain is the ANIMAL-WEIGHTED MEAN over
every kid weighed twice (each kid once, at the median of its own pairs) PLUS every whole-shed pen
weighed twice in the window, each pen contributing its average-weight movement ONCE PER ANIMAL it
holds. `weighing.leadership.growth`'s headline and the Weights page's gain-by-breed/sex/stage
charts now compute the IDENTICAL statistic, so a page filtered to one sex shows the same number in
the headline and in the chart.

It did not, and the maintainer found it: filtered to Male the page read 133 g/day in the headline
above 200 g/day in the chart. Three mismatches at once — MEDIAN vs weighted MEAN, PAIRS vs ANIMALS,
and whole-shed pens counted in one and not the other. Each was individually defensible; together
they left the screen with no true number on it. Most of this farm's kids are weighed by the whole
shed (339 of 791 in the landing window), so the old headline also answered "how fast is the herd
growing" from under half the herd.

The wire field is `average_adg_g_per_day` (was `median_adg_g_per_day`) and `headline_animals` is
its denominator — `pair_count` remains the SCANNED-pair count and is now only the denominator of
the pair statistics. Renaming was part of the fix, not tidying: a field named `median_` returning a
mean is the same trap as the caption that told readers "Daily gain uses only the same animals
weighed twice" while 65% of the number was whole-shed movement. Android reads the same endpoint and
moved in the same change; the two surfaces must never report different herd growth.

KNOWN AND ACCEPTED: a whole-shed average moves when animals ENTER OR LEAVE the pen, not only when
they grow, so this is a coarser measure than a scanned pair. That is the trade taken deliberately
rather than report the herd from a minority of it. The pair-based statistics (positive %, negative
pairs, losing animals) stay individual-only — a shed average has no per-animal sign, and inventing
one would put animals in a losing list nobody weighed.

Pinned by `TestGrowthHeadlineEqualsTheGainChartForTheSameSex`, which filters to one sex so the
chart holds exactly one bucket and the headline must equal it animal for animal; it was
mutation-tested by restoring the old median-of-pairs headline and confirming it goes red.

ALLOWED besides `weighing_*`: proof / idempotency / audit / outbox plumbing, and
exactly four ORG tables — `locations`, `workforce_members`, `user_scope_grants`,
`shed_partitions` (a task belongs to a park, a person, and a physical partition).
Adding to that list is a MAINTAINER decision, never a developer convenience.

CRITICAL DISTINCTION (maintainer decision 2026-08-06; reporting exception clarified 2026-08-19): `shed_partitions` is an
ORG-scoped CATALOG of partitions that exist, keyed by (tenant_id, shed_id,
normalized_label), with NO per-animal data. It is allowed. `goat_shed_partitions`
is a PER-GOAT table (PK tenant_id, goat_id) that reveals which animal sits where.
It is strictly BANNED except for the single reporting file named above, where it
may be used only to label lump-sum composition at the selected operational
location grain. This distinction is enforced by the weighing isolation guard
(`check-weighing-free-flow-guard.mjs` mode 16): reading one maintains isolation,
reading the other breaks it unless the read stays inside that file-scoped
reporting exception.

Also banned, because they are invented rules on a path that has none: any
weighing CADENCE ("weekly", "monthly on the 15th", a minimum interval between
weighs), any "overdue"/"missed weigh"/"expected next weigh" concept, and any
roster/ownership/clinical gate before accepting a scan.

Why this is here and not only in the ledger: on 2026-08-04 an ADG read model
shipped a `LEFT JOIN goat_identifiers` to resolve a scanned tag to a goat_id.
The rule already existed in
`context/repo-audits/weighing-implementation-do-not-reopen-ledger.md` (A-6, B-4,
C-3) and in `docs/features/weighing/TRD.md`, and a dedicated guard with 15
failure modes was already wired into `ci-local` — but every mode was scoped to
the WRITE path, the guard only ran at CI time, and the constraint was never
passed into the subagent brief that proposed the join. Three ways to miss one
rule. It is now: (1) stated here, in always-loaded context; (2) enforced on
READ paths too by `check-weighing-free-flow-guard.mjs` (mode 16,
`weighing-reads-non-weighing-table`); (3) run on EVERY weighing file edit by
`tools/agent-hooks/check-weighing-isolation-on-edit.sh`, wired into PostToolUse
for BOTH `.claude/settings.json` and `.codex/hooks.json`.

If you delegate weighing work to a subagent, the isolation rule goes in the
brief. An agent that was never told the boundary will propose crossing it, and
it will sound reasonable.

Isolation is not permission to create a private coordination island. Weighing
continues to commit its own domain state, audit, idempotency, proof, and outbox
without an inbound kernel dependency. The shared task kernel, outside the
Weighing package, consumes those durable events outward-only to materialize
owner/clock, hierarchy, contact-waterfall, proof, and sign-off coordination.
That consumer must be receipt-backed, idempotent, version-fenced, bounded,
observable, replayable, and reconciled against Weighing source rows. This does
not widen the Weighing table allowlist, and generic task state must never gate
scan, submit, verdict, reopen, or close.

## Never Kill Another Agent's Build — and Never Wait For One (Claude AND Codex)

Gradle is NOT a lock. Separate worktrees run separate daemons and build concurrently.
The 2026-08-03 deadlock that cost 90 minutes was agents **killing each other's
workers** and each restarting — not contention over a shared resource.

Rules:

1. **Build when you need to.** Do not serialize, do not ask permission, do not wait
   for someone else's build to finish. Use `--max-workers=1` so a parallel build does
   not eat the machine.
2. **NEVER kill another process's Gradle workers or daemons.** `pkill -f
   GradleWorkerMain` is banned unless you started that build yourself and it is dead.
   Reap only YOUR OWN orphans, after your own killed build.
3. **NEVER wait-loop on a resource.** A wait loop that outlives its condition is worse
   than a failure: on 2026-08-03 two agents sat waiting on ORPHANED workers from a
   build that had already died, so the wait could never end. If something you need is
   busy, do the work that does not need it and report the blockage.
4. **Surface a genuine block to the maintainer immediately** — name the resource and
   the holder so they can decide. Never absorb it into a status line as "still
   running". A long-running agent card may also be STALE: verify against the process
   table or the branch, not the card.
5. **Exit 137 from Gradle is an OOM SIGKILL** from memory pressure, not a test failure.
   Re-run once with `--max-workers=1`; if it recurs, report it rather than looping.

Generalizes to any shared thing (Docker, a port, a device, the local stack): parallel
use is fine, killing someone else's is not, and waiting forever is never the answer.

## Fast Lane for Tiny Fixes

When the maintainer asks to make a small, low-risk fix and land it on `main`,
optimize for elapsed time. Do not run the full local CI matrix, mobile install,
cloud deploy, browser proof suite, or graph/document maintenance unless the
change actually touches that surface or the maintainer explicitly asks for it.

Default verification should be the narrowest command that proves the touched
surface still works. Examples:

- Android Kotlin-only UI or view-model edit: run the targeted Gradle compile or
  targeted unit test; install to a physical device only when device behavior is
  the thing being verified.
- Android weighing list edits: repeated Compose row keys must include full
  work/category/period identity, not only `campaignShedId`; run
  `WeighingRouteIdentityTest` for this guardrail.
- Android weighing assignment-card date edits: delayed backlog rows carry both
  original `planned_business_date` and rolled/current `due_business_date`. Show
  the operator **Delayed** with the original planned date; do not make old
  backlog look newly scheduled for today. Run
  `WeighingAssignmentModeAwarenessTest`.
- Android vaccination proof-list edits: proof-needed rows are obligation-grain,
  not goat-grain. Key by `obligationId` before `goatId`, and run
  `ScanProofIdentityTest` plus `make android-compose-lists-guard`.
- Admin-web component/style edit: run the relevant typecheck/test/lint slice or
  a focused browser check, not the whole product suite.
- Docs/copy/config-only edit: inspect the diff and run format/schema validation
  only if that file type has one.

Before pushing, verify repo, remote, active identity, branch/head, and dirty
state. Avoid detached-HEAD limbo for ordinary work: use the current branch when
it is safe, or push the verified commit explicitly with `git push origin
HEAD:main` when the maintainer asked to land directly on `main`. Never include
unrelated proof files, screenshots, temp folders, or local artifacts in the
commit.

When the maintainer asks whether a fix was pushed or why it was not pushed,
answer the status plainly first and do not argue. If the maintainer's intent is
to land the already-reviewed/focused fix on `main`, do the repo/identity/dirty
state checks and push the scoped fix to `main` instead of stopping at an
explanation. If the worktree contains unrelated dirty files, isolate only the
fix files in the commit/push path or state the concrete blocker.

If `main` push is rejected by the landing gate, **do not stop at "can't push to
main."** Run `make land-main` from a clean isolated worktree, inspect every named
failure, fix branch-owned blockers, commit them, push the branch, and rerun the
gate. Repeat until the exact SHA lands on `main` or the remaining blocker is a
real external prerequisite the agent cannot change (for example a missing local
OCI tunnel/VM credential, expired cloud auth, or an unavailable maintainer-owned
service). A missing local Docker binary is **not** a blocker on Ravi's laptop:
follow the OCI-DB rule at the top of this file and use OCI-hosted disposable
Postgres/query-plan proof instead of asking for or installing local Docker. If a
gate prints `docker: command not found`, first look for its OCI/admin-DSN
override (for example `GOATOS_SQLC_PLAN_ADMIN_DSN` for query-plan proof) and run
that path; do not report local Docker absence as the reason `main` cannot land.
Even then, report the specific prerequisite and the exact command/output that
proved it; do not present a guard failure as the final answer while fixable
blockers remain.

Report the verification boundary honestly and briefly. If only a narrow check
was run, say so; do not spend 20 minutes manufacturing confidence for a one-line
change.

## Main Merge Requires Exact-SHA CI Evidence

No PR, GitHub UI merge, connector/API merge, merge queue action, or direct push
may put code on `main` unless one of these is true for the exact commit being
landed:

1. `make land-main` completed green from a clean isolated worktree.
2. GitHub `ci` completed green for the exact current PR head SHA after the
   branch was rebased onto fresh `origin/main`.

Pending, failed, cancelled, stale, skipped, or targeted-only checks do not
authorize a merge to `main`. Targeted local checks are review/preflight evidence
only. If neither exact-SHA proof exists, do not merge; run `make land-main`
locally or wait for/dispatch GitHub CI and verify the exact SHA is green first.

## MANDATORY: 4-Layer Lookup on Every Code Question

Work through layers in order. Stop at the layer that answers the question. Do NOT jump to files/grep first.

### Layer 1 — CRG (code structure)
For callers, callees, imports, blast radius, architecture, dead code, test coverage:
```
repo_root: <absolute path of your goatos checkout>   # git rev-parse --show-toplevel

Cold/review/diff task      -> get_minimal_context_tool, then one targeted graph query
Known-symbol traversal     -> query_graph_tool callers_of/callees_of/imports_of/tests_for
Keyword/domain lookup      -> semantic_search_nodes_tool, then query_graph_tool
Changing code              -> detect_changes_tool + get_impact_radius_tool
Single file/function read  -> read the file; use graph only if impact is unclear
```

### Layer 2 — Graphify (business/product context + technical docs)
Two graphs. Query both in parallel:

**mesha_docs_graph** — wiki SOPs, farm workflows, vaccination protocols, org model:
```
MCP: mesha_docs_graph
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph /Users/ravi/mesha/graphify-out/graph.json
```

**goatos-docs graph** — TRDs, ADRs, phase docs, obligation engine, skill references (locally generated; run `make ai-rebuild-docs` if missing):
```
CLI: uvx --from 'graphifyy[mcp]==0.8.44' graphify query "QUESTION" \
       --graph ./graphify-out/graph.json
```
When built, it covers protocol engine, Preventive Care (PC) vaccination, feed direction,
frontend scope, analytics infra, execution plans, observability, auth, SOP
cutover, and skill references.

### Layer 3 — Skill references (architecture decisions, TRDs, phase contracts)
When CRG + Graphify don't cover it — deep implementation rules, phase PRDs/TRDs,
OpenAPI contracts, form DSL, analytics infra, security/ops rules:
```
Load .agents/skills/goatos-build/SKILL.md → pick only the relevant reference doc
Do NOT load all reference docs — let CRG + Graphify narrow which one applies
```

### Layer 4 — Grep/Read (CRG blind spots)
Only for what the graph cannot see:
- HTTP route strings (`r.GET("/api/v1/...")`)
- Middleware wired via reflection or string keys
- Config/env values and constants
- SQL query strings
- Uncommitted/unstaged code
- Any `callers_of = 0` result that seems wrong — verify with grep

## Production-Facing Environment Decision

Current operator-facing production cleanup uses the existing `goatos-stg`
Google/Firebase project internally. Do not infer from the project id that public
surfaces should keep staging names. Public app, browser, and operator-facing
surfaces must use production names:

- Android package: `sg.mesha.goatos`
- Dashboard: `https://dashboard.mesha.sg`
- API: `https://api.goatos.mesha.sg/` unless the maintainer explicitly chooses a
  different prod API host in the same request
- Firebase Auth issuer/audience may still be `goatos-stg` while the existing
  Firebase project is reused. This is internal auth plumbing, not public naming.

When editing docs, skills, release notes, app config, or deploy guidance for the
current live operator path, describe it as production-facing even if the backing
GCP/Firebase project id is `goatos-stg`. Keep historical incident/runbook facts
unchanged only when they are explicitly about the old staging environment.

## STG Deployment Contract

For Goat OS, STG deploy is NOT GitHub Actions and NOT PR-driven.

GitHub Actions is billing-blocked and must not be used for deployment
(validate only via `make ci-local` on the pushed SHA).
Do not create main→stg PRs as a deploy mechanism.
Do not force-push a `stg` branch and wait for CI.
Do not infer CI deployment from branch names.

If STG shows Google Frontend `429 Rate exceeded` after a paid/restored Google
bill, do not guess or redeploy app code first. Read and follow
`docs/runbooks/stg-cloud-run-billing-recovery.md`: verify `ravi@mesha.sg`,
`goatos-stg`, `billingEnabled: true`, Cloud Run service readiness in
`asia-south1`, and finish with both terminal curls and live Chrome verification.
The 2026-08-26 maintainer baseline for `goatos-api-stg` is min-instances `2`
and max-instances `2`.

Authoritative STG deploy path:
1. Read `docs/runbooks/stg-deploy.md` (short contract) →
   `docs/runbooks/cloud-deploy-staging.md` (full Cloud Deploy mechanics).
2. Use the Slack deploy button in `#goatos-stg-deploy`. The button triggers the
   Google Cloud Build manual trigger `goatos-stg-deploy-main`, which reads
   `cloudbuild.stg.yaml` and creates the Cloud Deploy release from `origin/main`.
3. Verify active account is `ravi@mesha.sg`.
4. Verify target org is `vgoats.com` and environment is Goat OS STG
   (`goatos-stg`).
5. Never use Slice/Heva GitHub identity or cloud project for Goat OS.

If a user asks to "push to STG", "promote STG", or "deploy STG", this means:
use the Slack button/Cloud Build route from the latest approved `origin/main`,
following the runbook. Do not run a local deploy unless the Slack/Cloud Build
route itself is broken and the maintainer explicitly asks for break-glass.

If a user asks whether STG deploy is done, failed, or stuck, check the Cloud
Build run started by the Slack bot first, then the Cloud Deploy release/rollout
linked from that build. Do not infer status from local shell output or branch
names.

If a user asks to "publish Firebase", "upload to Firebase", "Firebase App
Distribution", "release Android", "push the APK", "internal test", "Play
internal testing", or includes an Android APK/AAB as part of a STG deploy, the
Android release is not complete after Firebase App Distribution alone. Follow
`docs/mobile/production-facing-release.md` and publish the employee/internal release to
all required channels: Firebase App Distribution, Google Play Internal Testing,
and the stable operator URL `https://mesha.sg/app.apk`. That URL redirects to
`gs://goatos-stg-public-downloads/operator/latest/app.apk`; do not copy APKs
into the Mesha marketing website repo and do not deploy Firebase Hosting merely
to update the APK. Use the exact same generated APK bytes for Firebase App
Distribution and the Storage mirror. Play Internal Testing uses an AAB, so
build/upload it from the same source commit, `versionName`, and `versionCode`;
do not invent a second release identity. Keep the Play internal tester list to
the same email IDs that have access to Firebase App Distribution; do not
maintain a separate hand-picked Play tester list. Keep the browser download
filename versioned as `Mesha-<versionName>.apk`, verify matching APK hashes, and
validate the live versioned URL in Chrome before reporting done.

Do not ask whether to use GitHub Actions, PR merge, or force-push `stg` unless
the user explicitly asks to change deployment architecture. The machine-readable
form of this contract lives at `context/deploy-contract.json`.

## Mandatory Android APK Source Traceability

Every Android APK uploaded to Firebase App Distribution must be traceable to
the exact source revision that produced it.

- Use only `:app:appDistributionUploadProdRelease` for the active
  production-facing Firebase App Distribution Android uploads. Do not upload
  ad-hoc APK files manually from the Firebase
  console, `firebase appdistribution:distribute`, or any other path unless the
  maintainer explicitly asks for a one-off rescue build and the release notes
  still record the source label.
- The distributed APK must include/bake the source commit and tag metadata
  (`SOURCE_COMMIT`, `SOURCE_TAG`, `SOURCE_BRANCH`, `SOURCE_LABEL`, and the
  matching string resources) and the Firebase release notes must carry the same
  source label.
- Never upload from a dirty worktree. The only exception is an explicit
  throwaway/debug build using `-PallowDirtyFirebaseDistribution=true`; label it
  as throwaway in the release notes and do not use it to answer whether a
  production-like phone APK contains a feature.
- When answering "does the latest Firebase APK have feature X?", first verify
  and record the installed/Firebase release source label, then compare that
  commit/tag against the commit that introduced the feature. If the source label
  cannot be verified, say that clearly instead of inferring from local `HEAD`,
  `origin/main`, or memory.

## Mobile/API Provenance For "Who Did What"

When the maintainer asks who performed an action, whether an old APK caused a
bad submission, which phone captured a bad audio/video proof, or which device
sent an RFID/weighing/vaccination action, check backend audit and request
provenance first.

Android sends these headers on every API request:

```text
X-GoatOS-App-Version
X-GoatOS-App-Version-Code
X-GoatOS-Build-Type
X-GoatOS-Device-Id
X-GoatOS-Platform
X-GoatOS-OS-Version
X-GoatOS-SDK-Version
X-GoatOS-Device-Model
```

Backend request logs include the same fields, and `audit_log.metadata->'client'`
is the durable SQL source. For a specific weighing/vaccination/proof issue,
join from the domain row to its audit event and inspect that client block before
guessing from screenshots, Firebase, or local code. Firebase Analytics is useful
for dashboards; the audit row is the source of truth for a submitted business
action.

## WEIGHING IS SCAN-AND-SUBMIT. Nothing else. (Claude AND Codex, every session)

Maintainer statement, 2026-08-03. Sessions keep re-deriving weighing rules that do
not exist, and the maintainer keeps re-explaining them. This is the WHOLE feature:

```
CEO assigns sheds to an operator or a director (the Growth Director executes too)
individual  → scan RFID, enter weight, record video — per animal
lump-sum    → total weight, video(s) — per shed (head count is snapshotted
              server-side from the herd register at submit; maintainer decision
              2026-08-24, frozen forever, verifier edits weight only)
submit
```

**The ONLY business rule: an animal cannot be scanned twice in the same bucket before
submit.**

There is **NO** shed↔RFID validation (a scanned tag is stored verbatim and is never
checked against a shed), **NO** roster / expected animal count / denominator /
progress percentage, **NO** herd, goat, clinical or lifecycle lookup, **NO** vaccine,
protocol or obligation rules, and **NO** "shed is empty" concept — free-flow means the
system cannot know what is in a shed and must not try to.

**Do not invent problems that cannot exist in this model.** Two were raised and killed
on 2026-08-03: an "empty shed outcome" (impossible — nothing knows a shed is empty),
and the per-animal verifier queue called a grain bug (one video per animal means one
review per animal; the grain follows the EVIDENCE — ban B-5).

Legitimate weighing work is **plumbing, never rules**: do writes reach the server, is
evidence reviewable, are failures visible, do screens show honest numbers.

Machine-enforced by `make weighing-free-flow-guard` (in `make guardrails` and
`make ci-local`), which blocks a herd/goat/vaccination join on the write path, a
roster gate, a clinical-state read, and an expected-animal denominator in weighing UI.
Canonical prose: `docs/features/weighing/TRD.md` → "What weighing IS"; bans and their
history: `context/repo-audits/weighing-implementation-do-not-reopen-ledger.md`.

## Business and medical rule changes (maintainer lock)

When the maintainer states a **new working rule, condition, timing, or workflow**
(in chat, WhatsApp screenshots, Preventive Care sign-off, or ad-hoc instructions) that may
**contradict or supersede** existing docs, seeded config, implemented kernel
behavior, or a prior decision in the same thread:

1. **Stop and surface the conflict first** — quote the old rule/source and the new
   instruction side by side. Do **not** silently pick one, blend them, or change
   code/docs on assumption.
2. **Ask explicitly** which rule wins, whether the old rule is retired, or
   whether both apply in different scopes (species, stage, procurement path, etc.).
3. **Implement only after confirmation** — then update the canonical source in the
   same change as the code (`docs/preventive-care-vaccination/vaccination-rules.md`,
   published `rule_dsl`, TRD/ADR, or this file when appropriate).

Ambiguity is not approval. Informal agreement in a screenshot or chat applies to
**that** scenario until it is written into the source contract.

Confirmed Preventive Care (PC) vaccination override: never ask about, model, seed,
import, expose, or schedule from mother-not-vaccinated / unknown-mother status.
The private source/wiki may contain that branch, but GoatOS ignores it. Mothers
are kept vaccinated operationally, and every kid uses the approved standard
schedule in `docs/preventive-care-vaccination/vaccination-rules.md`.

Confirmed Preventive Care (PC) Z1+Z3 course rule: Z1+Z3 is a two-dose course
before the 182-day repeat. Dose 2 is due 21 days after dose 1 for both kid and
adult courses. Imported/seeded Z1+Z3 dose 1 must create the dose 2 obligation
first; it must not jump straight to the 182-day repeat. The 182-day repeat
starts only after accepted Z1+Z3 dose 2/course completion. Blue Tongue kid dose
2 is due 21 days after dose 1, at 19 weeks/133 days; pox vaccines still obey
the 28-day live-to-live spacing after PPR.

Hard seed/generation guard: after real vaccination seeding, any accepted
legacy `et_tt_adult_w1` completion without a same-goat legacy `et_tt_adult_w2` obligation or
completion is a broken database, not a warning. Do not report future drives from
`vaccination_drive_assignments` alone; first audit missing required obligations
against `protocol_rules` and accepted history, especially adult Z1+Z3 dose 2.

Confirmed module ownership and weighing planning authority (maintainer decision
2026-08-01): each operational module has ONE accountable director, and a module's
verification notification must reach that director in that module's own wording -- never
another module's recipients or copy. Vaccination -> `pc_director`. Weighing ->
`growth_director`. Feed -> `feed_director`. Counts -> `health_director`, which is a DISTINCT
role from `pc_director` (preventive care) and must not be merged with it. The verifier is
TENANT-level: one verifier reviews proof videos across all parks and sheds. Feed ownership is
documented (`wiki/Handbooks/Feed_Director.pdf`, Role Purpose + M1 daily video double
verification). Counts ownership is a maintainer decision rather than a documented one: no
counting department or counting handbook exists in any source (the live `Counting DB` records
only a "Staff (Counted)" person with no role, no verifier and no approver, and
`Health_Director.pdf` never mentions count, census, headcount or shifting). The routing SHAPE
is well supported either way -- both handbooks carry the same "Meet verifier every day" /
Video Verification Team duty the other directors have.

COUNTS IS AN OFF FEATURE and stays that way. `health_director` is created and recorded as
the counts owner so the module has a declared owner when it is switched on; the role exists
ahead of the feature deliberately.

Note precisely HOW counts is off, because it is easy to switch on by accident: the module is
registered `moduleStatusAvailable` in `workforce/app/bootstrap_copy.go` and is held back only
by `counts.read` / `counts.write`, which today only `ceo_internal` holds. Granting those to
`health_director` would light up the Counts nav for him and thereby ENABLE the feature. So
`health_director` gets counts OWNERSHIP (it is the leadership recipient for a counts/shifting
proof, replacing the silent vaccination default) but NOT `counts.read`/`counts.write` until
the feature is deliberately turned on. Ownership and access are separate decisions here.

Confirmed Health-protocol authoring rule (maintainer decision 2026-08-06): `health_director` DOES
hold `health.config.read` / `health.config.write` -- the authored treatment rulebook behind
`/health/config`. This is a SECOND grant to that role and it is a different kind from the counts
one above: counts ownership is an extension of the handbook, whereas this is squarely inside it
(`Health_Director.pdf` Responsibilities 1-4 put observation, diagnosis, treatment and treatment
tracking on that desk, and the protocol IS the standard those are carried out against). The role
already held `goat.write_health` to record a clinical fact about ONE animal; this lets it author
the standing course EVERY animal with that disease is treated under.

Read the two together and the pattern is: `health_director` gets Health authority in full and
Counts ownership without Counts access. It still gets NO Preventive Care permission -- `pc_director`
and `health_director` are separate departments and merging them is prohibited, so vaccination
protocol authoring stays on `/config` behind `ProtocolWrite`, which `health_director` does not hold.
`health.config.write` is also deliberately withheld from `operator` (executes a course, does not
author it), `park_head` (runs a park's execution) and `verifier` (separation of duty: the verifier
must not rewrite the standard the work is judged against). Only `ceo_internal` and `health_director`
hold it.

TREATMENT PROTOCOLS ARE VERSIONED, NEVER EDITED IN PLACE, and that is a medical-safety property
rather than an implementation preference. An edit builds a DRAFT; publishing promotes it and RETIRES
the version it replaces. `health_cases` pins `health_protocol_version_id` at diagnosis, so a goat
mid-treatment finishes on the dosages it started on and the version it was actually treated from
stays readable forever. Do not "simplify" this into an in-place update: that changes the dose an
animal currently being treated receives.

The Google Sheet (`Adults SOP` / `Kids SOP`) is now a ONE-TIME BOOTSTRAP, not an ongoing sync.
`ReplacePublishedProtocols` retires every published protocol and republishes the set, so running it
after an app edit would silently discard that edit; it therefore fails closed with
`ports.ErrImportAfterAuthoring` once any version carries the `health-config:app` source ref.
`ReplacePublishedProtocolsOverwritingAuthored` is the reviewed break-glass. Canonical prose:
`docs/decisions/health-config-authoring.md`.

Separately: PLANNING a weighing task is CEO-only. `growth_director` monitors weighing across
both parks, oversees the operators and may execute, but does not raise the task; the two
planner reads that feed the create wizard (`/app/weighing/planner/catalog` and
`.../parks/{park_id}/buckets`) are planning surfaces and carry `weighing.plan` despite being
GETs.

Enforcement note: a module that enqueues a verification item MUST have an entry in
`pendingModuleProfiles` (`backend/internal/notificationbridge/verification_notify_consumer.go`)
and at least one `verify` duty holder in `position_module_duties`. There is deliberately no
fallback profile -- an unclaimed module notifies nobody loudly rather than the wrong people
quietly, which is how weighing proofs reached the vaccination verifier and PC Director in
vaccination wording. Both conditions are asserted by tests; neither is a comment.

Confirmed movement rule (maintainer decision 2026-07-19): goats never move
between parks — shed moves exist only within one park; leaving a park is a
terminal transferred/sold exit, never a move. Initial placement is exempt. See
`context/source-findings/goats-and-parks-source-findings.md` → Movement
Semantics.

Weighing vocabulary (maintainer decision 2026-08-03): the weighing workflow has
exactly two verbs — CLOSE a task, or REOPEN it if it is already closed. There is
no third verb, and no force, override or skip variant of close.

THE CLOSE GATE IS UNCONDITIONAL. A bucket cannot close while verification is
pending, and there is no caller-supplied way past that. If a bucket will not
close, the answer is to RESOLVE the verification — get the verdict — never to add
a path around the gate. Machine-enforced by
`tools/agent-hooks/check-weighing-close-gate-guard.mjs`; see
`context/repo-audits/weighing-implementation-do-not-reopen-ledger.md` → D-5.

Confirmed "You" / profile nav placement rule (maintainer decision 2026-08-03,
stated THREE times and implemented wrong twice before this — read it exactly):

> **"You" belongs in the NAVIGATION DRAWER for any principal with 2 or more
> features/modules — CEO, leadership, and a verifier who verifies more than one
> feature. It must NOT be sent as a bottom-bar tab in every feature's bar.**

- **2+ modules** → "You" appears ONCE, in the drawer. Never in the per-module
  bottom bar. Repeating it in vaccination's bar, then weighing's bar, then every
  future verifiable feature's bar is the exact defect being banned.
- **Exactly 1 module** → that principal has no meaningful drawer, so "You" stays
  reachable in their bottom bar.
- "You" must ALWAYS be reachable. Deleting it outright is a regression (that was
  the first wrong implementation).
- This is the same shape as the existing nav-chrome rule: drawer/sidebar when
  there are 2+ modules, bottom bar when there is one. See
  `docs/decisions/role-module-nav-composition.md`.

Two traps recorded so the next author does not repeat them:
1. `shared_key: "you"` does NOT enforce this. `shared_key` has exactly one
   reader, `composeNavigationFromModules`; `verificationModuleForFeature` builds
   its nav items by hand and never calls it, and `visibleNavigationFor` returns
   those items directly. Setting it there was mutation-tested — flipping it back
   to `""` passed the entire suite and changed no served payload. Any mechanism
   used for this rule MUST be mutation-tested: break it deliberately and confirm
   a test goes red.
2. The verifier alerts tab is titled just **"Alerts"** in every locale. The alerts
   stay feature-scoped through `?category=` on the href
   (`vaccination_proof`, `weighing_proof`, `shifting_move`); only the LABEL
   stopped naming the module the verifier is already inside. The per-feature
   label keys (`nav.alerts.vaccination` etc.) and their resolver are deleted —
   do not reintroduce them. The Alerts SCREEN title is likewise just "Alerts";
   it was previously hardcoded to "Vaccination alerts" in
   `AlertsViewModel.kt`, which is also a violation of the backend-owns-labels
   rule.

Confirmed leadership vs. verifier surface separation (maintainer decision 2026-08-06, STANDING LOCK, CORRECTED for deceptive-helper defect): Leadership (audit/overview, read-only evidence trail) and Verifier (action queue, verdict casting) are SEPARATE SCREENS on SEPARATE ROUTES with NO shared composables, ViewModels, or UI components.

Incident report: on 2026-08-06 morning, the leadership "Videos" navigation item was repointed to resolve directly to a verifier route (`/verify/...`), coupling leadership to verifier UI changes and creating UX confusion (verdict buttons appeared greyed out in leadership screens because leadership rendered verifier composables). CRITICAL CORRECTION: `leadershipVideosHref()` is a deceptively-named helper — it RETURNS `/verify?module=X&status=all`, still a verifier route, NOT a leadership-owned route. The guard was initially checking helper NAMES, not return values, and thus passed the exact defect it should catch. The rule and guard now prevent a recurrence.

**Three bans, machine-enforced by `check-leadership-verifier-surface-separation.mjs`:**

1. **Backend nav routes:** Leadership navigation hrefs emitted by `bootstrap_copy.go` MUST point to leadership-owned routes (e.g., `/videos/module`), NEVER use `leadershipVideosHref()` (which returns `/verify*`), NEVER call `verifyQueueHref()`, and NEVER point directly to `/verify` routes. Why: Leadership and Verifier have SEPARATE routes on SEPARATE screens. When the `/videos` screen is built, leadership nav will use `/videos/module`, not `/verify*`. (CORRECTED: previously stated `leadershipVideosHref()` was correct; it is not — it returns a verifier route.)

2. **Android leadership imports:** Leadership-owned screen files (under `.../leadership/` in feature modules) MUST NOT import from `sg.mesha.goatos.feature.verify`, use verifier composables (e.g., `VerifyDetailScreen`), or use verifier ViewModels. Why: rendering a verifier composable couples leadership to verifier state (permissions, verdict handlers) and makes leadership a thin wrapper around the action queue.

3. **Android leadership controls:** Leadership-owned screen files MUST NOT render verdict buttons (approve/reject/rework/reassign) or verdict-casting UI. Why: verdict casting is verifier-only. Leadership sees the RESULT of a verdict (a status chip), not the action to cast it.

Adversarial self-test: `check-leadership-verifier-surface-separation.mjs` includes a test case `bootstrap-bad-leadership-videos-href-returns-verify` that calls `leadershipVideosHref()` (looks correct by name) and verifies the guard FAILS it (correct) — because the helper returns a verifier route. This proves the guard catches the deceptive-helper defect that the old guard missed.

Canonical source: `docs/decisions/leadership-vs-verifier-surface-separation.md` and `context/architecture/verifier-app-and-flow.md`. Machine enforcement: `make leadership-verifier-surface-separation-guard`. This decision is FINAL and LOCKED; any future cross-surface wiring MUST address why the 2026-08-06 incident is not a risk.

Confirmed role-scoped UI is capability-gated (maintainer decision, STG incident 2026-08-12): admin-web pages are ROLE-AGNOSTIC single components — the SAME `/verify` component serves both a verifier and CEO/director oversight via `?scope_mode=company`. Role differences MUST come ONLY from (a) permission-gated endpoints and (b) capability-driven page contracts (`controlEnabled(pageContract, "control_id", false)` / `optionGroup(pageContract, ...)`, compiled in `backend/internal/adminui/app/compiler.go` off named permission constants in `backend/internal/permissions/permissions.go`). NEVER a role-string or permission-string conditional inside a component, and NEVER a per-role page copy. Incident: the CEO's oversight filters on `/verify` (module chips, capture-date range) rendered for every role that could open the page, including `RoleVerifier`, because nothing distinguished the caller. Fix: `permissions.VerificationOversee`, gated at both the page contract (`oversight_filters` control) AND the backend query (`ports.ListQueueParams.OversightFiltersEnabled` — ignores `nav_module`/`business_date_from`/`business_date_to` and blanks `filter_options.modules` for non-oversight callers). When asking an agent for role-scoped UI, name BOTH halves (the contract control AND the endpoint enforcement) in one prompt — a pixel-only ask reproduces this incident's inverse. Canonical source: `docs/decisions/role-scoped-ui-is-capability-gated.md`. Machine enforcement: `make role-scoped-ui-contract-guard` (`tools/agent-hooks/check-role-scoped-ui-contract.mjs`).

Confirmed vaccination progress rule (maintainer decision 2026-08-03): drive
progress is **FIELD WORK DONE = completed + submitted**, never completed-only.
The operator vaccinated the animal, so it counts: a drive whose animals are all
vaccinated and whose proofs are submitted reads **100%** and **"4 of 4 sheds
done"**, and the outstanding video review is carried by the
`verification_pending` status and its chip — never by holding the ring below
100%. The backend owns the single number (`progress_basis`,
`progress_completed`, `progress_total`, `progress_pct`, and `sheds_completed`);
admin-web and Android render it verbatim and must not derive their own.
Pinned by `TestDriveSummaryEmitsBackendOwnedProgressContract` and
`TestCalendarDriveSummaryFiveBucketsAreDisjointWhenSubmittedIsLateOrDeferred`.

**Why this is a lock, not a preference:** an earlier session found admin-web and
Android showing different completion numbers for the same drive and resolved the
parity defect by adopting the stricter surface — making the numerator
completed-only. That silently redefined "done" as "verified" and showed an
operator who had finished every animal in every shed a 0% ring with "0 of 4
sheds done". The choice was then written into two tests and a code comment, so it
read to every later author as intentional. Do NOT revert to completed-only.

**General rule this establishes:** a cross-surface disagreement about a business
number is a MAINTAINER QUESTION, not an implementation detail. Both surfaces may
be wrong, and picking the stricter one is still a product decision. When two
surfaces disagree about what a count means, stop and surface the conflict per the
maintainer-lock rule above; fix parity by making the backend own one number, not
by choosing a client's semantics.

Confirmed shifting TYPED-RAISE rule (maintainer decisions 2026-08-20, SUPERSEDING the
2026-08-15 tag-toggle rule below on WHO decides for a raise that names a category, and
superseding the clinical raise-time refusal FOR `health` MOVEMENTS ONLY): **THE SHIFT
TYPE DECIDES THE TAG — the raiser is no longer asked.** Every typed raise carries a
`category` that IS the rule selector: `health` stamps the destination tag on both legs
(the one type allowed to stamp a clinical state — a health shift IS the health team
acting); `growth` stamps the destination tag FORWARD ONLY along the authored lifecycle
ladder (one reverse edge, Pregnant → Non-Pregnant; sexed stages refuse the wrong sex);
`breeding` never changes the tag; `delivery` stamps the destination tag except never the
newborn stage (into an empty untagged recovery shed the mother keeps her tag and the pen
ADOPTS it); `spacing` moves the WHOLE source pen carrying its tag ("half-half is not an
option") into a same-tag or empty destination (an empty pen adopts the tag); `flushing`
moves females onto the Flushing tag into an empty or already-flushing pen. A raise a
rule refuses is rejected at RAISE time with backend-owned farm copy — before approval and
before any video. Pen adoption is snapshotted at raise (`adopt_pen_tag`, migration
000179) and re-validated under the apply row lock, failing the whole apply closed
(`ErrDestinationPenChanged`) when the pen changed underneath the approval. The client
still names no stage of its own — `target_management_stage` stays rejected; the 2026-08-15
toggle below survives ONLY as the legacy path for a category-less raise from an older APK.
Canonical prose: `docs/features/shifting/shifting-rewrite-tag-rules.md`; rulebook:
`backend/internal/counts/domain.ResolveShiftTypeDecision`. Open decisions recorded there:
Mother/Milking/M0/Warmup have no growth edges yet (a growth raise touching them refuses),
and an approver-chooses-tag capability for an untagged spacing source is a follow-up.

Confirmed shifting TAG TOGGLE rule (maintainer decision 2026-08-15, now the LEGACY path
governing only category-less raises per the 2026-08-20 typed-raise rule above; it had
itself SUPERSEDED the
2026-08-03 no-chooser rule below on WHO decides, and its FLUSHING carve-out outright;
the 2026-08-03 rule had itself superseded the 2026-07-29 three-mode operator chooser and
the 2026-07-20 destination `shed_profiles` authority rule): **the raiser chooses again —
but between two BACKEND-OWNED answers, never a stage of their own.**

The raise form shows a two-position toggle:

```text
keep_current      the animals keep the tag they already carry
destination_stage the animals adopt the destination PEN's tag   (DEFAULT)
```

`stage_mode` on `POST /app/counts/shifting-events` carries the choice. ABSENT means
`destination_stage`, so an APK predating the toggle keeps behaving exactly as it does
today; a present-but-invalid value is REJECTED (`invalid_stage_mode`), never rewritten to
the default — silently defaulting would stamp the pen's tag on a movement whose raiser
asked for the opposite.

**What did NOT change, and is the real content of the 2026-08-03 lock:
`target_management_stage` is still rejected as an unknown field.** The client sends a
MODE; the BACKEND still resolves which tag that means, from the same catalog the form
renders. A phone therefore still cannot invent a cohort, cannot name one the relocation
would refuse at the second gate, and cannot disagree with what the park head approved. Do
not "simplify" the toggle into a stage picker — that is the 2026-07-29 chooser, and it was
retired for these reasons.

**FLUSHING IS NOW ADOPTED.** The carve-out (flushing is a nutrition cohort owned by its own
workflow) is retired. The maintainer was shown the consequence — a move into a flushing pen
puts that animal on flushing ration and re-keys her vaccination schedule — and accepted it.
Migration `000171_flushing_stage_is_writable.sql` lists it as writable, which is the other
half: nothing special-cases the string any more, so the WRITABLE VOCABULARY governs it.

**A CLINICAL STATE IS STILL REFUSED, and is now refused EARLIER.** Bare `ICU`,
`Quarantine`, `sick`, `under_treatment`, `recovering` may never be stamped by a movement:
an animal in one of them has her vaccinations POSTPONED, so a placement action must not
make that medical call. This is now enforced at RAISE time in
`counts/domain.resolveConfiguredStage` (via `protocol/domain.IsClinicalManagementStage`,
the ONE implementation, shared with identity's second-gate guard) rather than only at the
second gate. It matters because a tenant really can list `ICU`/`Quarantine` in
`animal_stage_lookup` — `migrations/postgres/stage_age_band_test.go` seeds exactly those —
so the vocabulary check alone would resolve one at raise and then fail in
`identity/adapters/postgres.resolveDestinationTag` AFTER the operator shot the completion
video and the park head approved. The clinical PEN names `ICU-Kid` / `Quarantine kids` are
NOT states and stay writable (migration 000167): a movement may say which pen an animal is
in, never what condition she is in.

Three cases still keep the current stage because the destination cannot be resolved: a pen
holding more than one cohort, an empty pen, and a tag absent from active
`animal_stage_lookup`. Keeping the current stage is the already-shipped empty-target
behaviour, never a fabricated cohort; do not "improve" it into a majority-resident pick,
which stamps a stage on thin evidence and flips as animals move.

**The unavailable option is GREYED OUT WITH A REASON, never silently inert.**
`GET /app/counts/shifting/destinations` carries `destination_stage` and
`destination_stage_reason` per pen, exactly one of which is non-empty. The reasons are
BACKEND-OWNED farm copy rendered verbatim (`counts/domain.StageReason*`): "This destination
has no tag set", "This destination holds a mix of tags", "This destination's tag can only be
set by the health team". The phone must not compose its own reason from a blank tag — a blank tag does not
say WHY it is blank, and the operator is owed that. The catalog and the raise resolve
through the SAME function, so the tag the toggle advertises is the tag the raise stamps.

Canonical rule: `backend/internal/counts/domain.ResolveShiftingDestinationPenStageDetailed`;
resolution happens at RAISE time so the park head approves the same stage the completion
applies. Pinned by `TestPenStageAdoptsFlushingAndRefusesClinicalStates`,
`TestPenStageReasonsAreFarmWordedAndExclusive`,
`TestRecordShiftingEventHonoursTheRaisersTagToggle` and
`TestRecordShiftingEventRejectsAnUnknownTagToggle` (each mutation-tested when written).
Once Park Head approval and operator completion both exist, the
second-gate transaction must atomically update the goat's `shed_id` and, when selected,
`management_stage`, write identity audit, and publish
per-animal `goat.location.changed` plus `goat.stage_changed` when the stage
changed. Vaccination must consume the result twice: rescope open shed-scoped
work while preserving in-progress/completed history, then re-evaluate clinical
eligibility/schedule. Selecting `Mother` changes only `goats.management_stage`; movement must
not create or modify pregnancy or lactation records, and must not fabricate health,
or other clinical facts; those stay on their authoritative workflows. A real
shifting-completion → Vaccination E2E test is mandatory—separate producer and
consumer tests are not closure.

Confirmed shifting APPROVE-FIRST gate (maintainer decision 2026-08-09, SUPERSEDING
the 2026-07-28 "independent, order-free gates" rule, which itself superseded the
2026-07-26 "verifier approval applies the move" rule): **Park Head approval comes
FIRST, always.** A raised movement is NOT in the operator's Actions work list and
CANNOT be completed until an approver authorizes it. This applies to every
movement, low and high priority alike.

Two halves, and both are enforced:

1. **Visibility.** The Actions `all` bucket EXCLUDES `event_status='pending'`. The
   raiser still sees their movement, read-only, under the retained `pending` tab:
   `primary_action_key='none'`, chip "Awaiting Park Head approval", not tappable.
   The `rework` bucket likewise excludes `pending`, so an unapproved movement
   cannot re-enter the work list through an evidence verdict. Every non-canceled
   row still lands in exactly one bucket, and each tab's count equals what that
   tab lists.
2. **Write.** `POST /app/counts/shifting-events/{id}/complete` REFUSES an
   unapproved movement with `ErrShiftingNotAuthorized` (400
   `shifting_not_authorized`) and writes NOTHING — no `proof_ref`, no completion
   stamp, no verification item. Gated on `authorization_state='authorized'`, not
   on `event_status`, because a legacy `pending_verification` row can be unapproved.

Why this replaced the order-free rule: an operator could burn the mandatory video
— all THREE videos on a high-priority move — on a movement the park head then
rejected, and a verifier could be handed evidence for a move nobody authorized.

**ACTIONS LEAD TIME (same maintainer decision, 2026-08-09).** An approved movement
awaiting operator work appears in Actions when it is DUE:

- **High priority → due the second it is approved.** No lead time at all.
- **Low priority → planned work.** Raised BEFORE 13:30 IST it is due the NEXT day;
  raised AT OR AFTER 13:30 IST it is due the DAY AFTER THAT. Due means 00:00 IST
  of that day.

A held movement keeps its RAISED business date and is simply absent from the queue
until due — it does not move to a later date bucket. Consequence to know: on its
due day the operator must page back to the raise date to find it, which is what the
previous-dates strip is for. Anchoring on RAISE time also makes a late approval
self-solving: if approval lands after the due instant, `now` is already past it and
the row appears immediately, so approved work is never hidden in the past.

Held rows are ONLY `event_status='authorized'`. An applied movement (completed, or
in evidence rework) is history and is never held — hiding it would erase work an
operator demonstrably did. A `pending` row is never held either, so a raiser always
sees what they just raised.

This is NOT an authority gate: completion is NOT blocked before the due date,
because the animals may genuinely have walked today and refusing to record a real
movement would make the herd register lie. Approval remains the only gate on
completion.

**This binds the ACTIONS QUEUE ONLY.** It does NOT change the feed-direction
shifting projection, which by the 2026-07-27 decision below has NO lead time and NO
priority branch ("forget high priority") and counts every authorized-but-unexecuted
movement immediately. The two rules look alike and are not: this one decides when an
operator is SHOWN work, that one decides how many mouths a shed is fed for. Do not
collapse them, and do not read this as reviving the retired normal-2-day /
high-priority-1-day projection lead.

Canonical rule: `counts/domain.ShiftingActionsDueFrom` (mirrored in SQL by
`shiftingActionsVisibleSQL`, which the page, the status counts, and the
previous-dates strip all share so a tab badge cannot advertise work the tab hides).

The approval-arrives-second apply branch in `authorizeShiftingEventInTx` is KEPT
deliberately as rollout compatibility for rows completed under the retired rule;
it is not a supported new path. Do NOT delete it, and do NOT treat its existence
as permission to complete before approval.

Operator completion still requires a MANDATORY live-camera video
(`shifting_events.proof_ref`; blank is 422). The COMPLETION transaction — now always
the second gate, since approval must already exist — atomically updates canonical
`goats.shed_id` and destination stage, publishes location/stage events, flips the
movement `applied`, and therefore moves Herd Register / Counts. Approval alone still
moves NOTHING. Verification is post-task evidence review only:
APPROVE marks evidence verified; REWORK creates evidence rework/audit without
changing the applied movement or rolling back goat location/count. Generic
Verification enqueue and verdict consumers remain wired, but verdicts do not own
census truth. Canonical source: `docs/decisions/shifting-verification.md`; forward
migrations `000049_shifting_approval_completion_gate.sql` and
`000050_shifting_actions_index.sql`.

Confirmed high-priority shifting feed-evidence rule (maintainer decision 2026-07-29): low-priority
shifting remains the existing one-live-camera-video flow. High-priority shifting embeds feed packing
and feeding inside Shifting, resolves exact feed type/quantity from active destination Feed Config
matched to the movement's EFFECTIVE management stage and moved animals' ration groups, and requires
THREE live-camera videos: shifting, feed packing, and configured feed being given to the animal(s).
All three proofs are reviewed together in ONE `shifting_move` verification item. Embedded packing
proof is shifting-scoped only and never creates or completes the separate Feed Packing/Feed
Distribution workflows. Park Head approval + operator completion still apply location/stage/counts
on the second gate; verification remains post-task review and rejection creates operator rework
without rollback. Missing config blocks, and a semantic fingerprint shown to the phone is
revalidated under the shifting row lock so changed config returns `feed_config_changed` rather than
guessing.

EFFECTIVE STAGE (maintainer decision 2026-08-12): the ration is priced against the snapshotted
target stage, or -- when that is BLANK -- against each ANIMAL's own current stage. Blank is the
normal outcome whenever `ResolveShiftingDestinationStage` declines to adopt a destination cohort
(empty pen, mixed pen, clinical state, or a cohort the relocation cannot write); it means "keep each
animal's current stage", NOT a missing input, and the raiser is never asked for a stage. Keying the
ration off the blank target hard-blocked EVERY high-priority movement into an EMPTY PEN with
"selected destination management stage is missing" -- naming a choice the phone does not offer. Do
not restore that key. An animal with no stage on either side still blocks, with a message naming
the herd-data gap rather than blaming the raiser. Canonical source:
`docs/decisions/shifting-verification.md`; migration
`000053_high_priority_shifting_feed_evidence.sql`.

Confirmed feed-distribution verification gate (maintainer decision 2026-07-26,
SUPERSEDING the "operator marks a shed-session fed (optional video), completed at
submit" contract FOR the feed-DIRECTION operator flow ONLY): a feed-direction
shed-session is completed only after a verifier approves the operator's proof.
The operator submits TWO MANDATORY proofs per session — a feed-distribution VIDEO
(`distribution_proof_ref`) and a water-distribution proof that may be PHOTO OR
VIDEO (`water_proof_ref`); a completion missing either is rejected 422
`proof_required`. That flips a NEW `feed_distribution_completions` row to
`pending_verification` and enqueues ONE `feed_distribution` verification item
carrying BOTH proofs — NOTHING is completed yet. ONE verifier APPROVE
(`ApplyVerifiedDistribution`) covers both proofs and flips the session to
`completed` (this is when `feed.distribution.completed` is emitted); a REJECT
(`BounceDistributionForRework`) flips it to `rework` for a re-shoot. Applies to
BOTH `normal` and `experiment` workflows. Feed direction is app-only: the
`/feed/direction` admin-web left-bar leaf is removed (keep `/feed/config`).
Wiring is the generic Verification module: producer `feeddirection`
`CompleteDistribution` + `feeddirection/adapters/verificationbridge` enqueue;
consumer `feeddirection/app.FeedDistributionVerificationHandler` on
`verification.verdict.approved`/`.rework`, filtered to `source.module=feed,
ref_type=feed_distribution_completion`. Canonical source:
`docs/decisions/feed-distribution-verification.md`; migration
`000032_feed_distribution_verification_gate.sql`.

Confirmed FEED PURCHASE ENTRY rule (maintainer decision 2026-08-24, SUPERSEDING the READ-ONLY
half — and only that half — of the 2026-08-17 lock recorded in migration `000174`): feed bought
for CBE and CPT is now RECORDED IN THE APP on `/procurement/feed-purchases`, carrying the same
fields the legacy Feed DB sheet's Purchase row keeps. 000174's own comment said "There is no
authoring UI; purchase/vendor entry screens belong to the future Procurement vertical" — that
vertical now exists, so the screen was built where the lock said it belonged.

The other two decisions in 000174 STAND and are enforced on the write path: CURRENT-CATALOG FEEDS
ONLY (an entered feed must resolve to an ACTIVE `feed_item_catalog` row, checked inside the write
transaction; unknown feeds are rejected, never invented into the catalog) and STOCK DEPLETES AT
SHEET LOCK (an app row sets `depletes_from = purchase_date`, `consumed_at_import_kg = 0`, so the
existing stock/days-left read on `/feed/analytics` needed NO change). PROCUREMENT owns the write;
feeddirection keeps the read.

`feed.purchase.read` / `feed.purchase.write` are DEDICATED permissions, never a reuse of
`ProcurementRead` — `operator` and `park_head` hold that for the source-entry screens they work,
and this ledger carries supplier prices and payment state. `feed_director` holds READ ONLY: it
owns what the farm feeds and is accountable for the stock cards these loads are counted from, but
buying is the procurement desk's job. Canonical prose: `docs/decisions/feed-purchase-entry.md`;
migration `000206_feed_purchases_app_entry.sql`. Pinned by
`TestRecordFeedPurchaseControlIsCapabilityGated` (the feed_director row is the mutation test: it
holds every feed permission there is, so enabling the control from a broader key turns it red),
`TestFeedPurchaseRolePermissions` and `TestFeedPurchaseRoutesAreGatedOnTheDedicatedPermissions`.

Confirmed EXPERIMENT FEED IS AUTHORED PER ANIMAL (maintainer decision 2026-09-01,
SUPERSEDING the "absolute_kg is a shed total and head_count is never a multiplier"
rule for every NEWLY authored cell, and only for those): an experiment pen is still
hand-entered cell by cell with no ration grid involved — only the question each cell
answers changed. `grams_per_head` is what ONE animal gets, and the feed sheet
multiplies it by the pen's LIVE projected head count, the same count the ration grid
uses. The pen's shed factor is deliberately NOT applied: an experiment quantity is
grams x head count and nothing else.

`feed_experiment_config.head_count` STAYS INFORMATIONAL and is still never a
multiplier — it records the population the author had in mind, and scaling by it would
freeze a pen's quantity at the count typed on the day it was authored.

THE EXISTING VALUES ARE SEEDED ACROSS (maintainer instruction the same day, REPLACING
an earlier "no previously authored data changes" answer in the same conversation):
migration `000238` converts every legacy cell in place as
`grams = trunc(kg x 1000 / the pen's LIVE resident count, 3)`.

USE LIVE ONLY, FORGET RECORDED (maintainer instruction, same day, and it governs the
whole module). `feed_experiment_config.head_count` — a figure an author once typed beside
the quantity — is no longer read, written, asked for or shown; it disagreed with the
actual population on 15 of 34 pens and nothing maintained it. It survives only as
provenance for what the conversion divided by. Every count anything shows or multiplies
by is the pen's LIVE population: the sheet's, and the Feed Config screen's `live_head_count`,
resolved per request from the herd register. Dividing the conversion by the live count is
also what makes it SAFE — the rate back-multiplies by the number it was divided by, so no
pen's feed moves on conversion day; from tomorrow the total follows the animals. A pen with
NO live animals has no denominator and stays on the legacy basis. The rate is TRUNCATED,
never rounded to nearest: the generator rounds a session quantity UP to a packable 0.1 kg,
so a rate a hair above exact lifts an unchanged pen's sheet by a notch.
`feed_experiment_basis_conversions` keeps every conversion's inputs — including the live
count, which nobody could reconstruct later — so the arithmetic is auditable and the Down
path exact.

The table still carries two figures and a `quantity_basis` naming which one a row holds:
a row with NO usable count cannot be converted and stays legacy, and the basis is per
CELL. The two readings differ by the pen's ENTIRE POPULATION, so a cell whose basis
cannot be read BLOCKS rather than resolving to either number. Every write authors the
per-animal basis; there is no route that writes a pen total any more. The workbook seeder
converts on the way in with the SAME truncated arithmetic and the SAME live denominator (a
migrated database and a seeded one must land on identical rates), falling back to the
workbook's own count only for a pen with no live animals — otherwise the ORDER of two seed
commands would decide whether feed config lands at all. It skips any cell stamped
`source = 'app'`, so a re-seed cannot discard a rate the farm corrected on screen.

Canonical prose: `docs/decisions/feed-experiment-per-animal.md`; schema: migration
`000237_feed_experiment_grams_per_head.sql`; rulebook:
`feeddirection/domain.ExperimentPlanner`. Pinned by
`TestExperimentStrategyMultipliesGramsPerHeadByTheLiveHeadCount`,
`TestExperimentPenMixingBothBasesReadsEachCellOnItsOwnBasis` and
`TestExperimentCellWithUnknownBasisBlocksRatherThanGuessing`, with the legacy
`TestExperimentStrategyUsesAbsoluteKgAndIgnoresHeadCount` kept unchanged beside them.

Confirmed feed-PACKING SHED-SESSION grain (maintainer decision 2026-08-11,
REVERTING the 2026-08-10 PEN-DAY grain in full and restoring the shed-SESSION grain
of the packing gate below): a pen's morning and evening shares are TWO SEPARATE
BAGS. Each is packed on its own, filmed on its own, and verified on its own — TWO
CARDS, TWO VIDEOS, TWO verification items per pen per feed day. Completion grain is
`(tenant, park, shed, partition, session_no, target_date, workflow)`, the natural
key `feed_packing_completions_natural_uq` has always carried (migration `000150`).

Why the pen-day merge was wrong: ONE CLIP CANNOT PROVE TWO BAGS. The two shares are
weighed out at different times, so a single video shows at most one of them, and a
verifier judging it against a day total cannot tell a crew that packed the morning
share twice from one that packed both correctly.

**Do NOT re-merge them.** The specific things that came back, each of which the
merge had removed:

1. `session_no` is REQUIRED on `POST /feed-direction/packing/complete`. A missing or
   `0` value is REJECTED (`ErrInvalidSession`): `0` is not "the whole day" — it is a
   value no worklist line matches, so accepting it would write a row the operator's
   bag never resolves to and leave that bag showing as still owed. The DB agrees —
   `CHECK (session_no >= 1)`.
2. `/feed-packing/worklist` accepts `session` again (0/absent = every session).
   `summary.line_count` counts pen×session lines.
3. The verifier's item is subjected `Session N · Castro 2`. Without the prefix a
   verifier holding a pen's two cards cannot tell which bag each clip proves.
4. The expected-ration context on that item names THAT SESSION's quantities, not the
   day's — one clip proves one bag, so a day total would show twice what the video
   should contain.
5. `packingCompletedKey` is gone; packing shares the session-bearing `completedKey`
   with distribution again.

Two things the merge did NOT touch and that stay as they are:

- **The PEN is part of the key.** Castro 1/2/3 are different animals on different
  rations; `000137` exists because one Castro 1 clip closed out all three. This
  survived the merge and must survive any future change.
- **Feed DISTRIBUTION was never merged** and needs no repair.

WHAT MIGRATION `000150` CAN AND CANNOT UNDO, because a future reader will ask. It
drops `feed_packing_completions_pen_day_uq`, promotes each surviving pen-day row's
`session_no` from the sentinel `0` to `1` (its video and verdict stand as the
MORNING packing; the pen's evening reappears as work still owed), restores the
`>= 1` check, and puts the `Session N · ` prefix back on in-flight verifier labels.
It CANNOT restore the rows `000149` DELETED when it collapsed each pen-day — those
are gone, and those pens' second bags simply reappear unpacked, which is the honest
state. `000149` is NOT amended: it is already applied on STG, and STG records
migration checksums.

The Android outbox needs the same promotion: a packing row queued by the pen-day
build carries no session, decodes as `0`, and `SyncEngine` maps it to session 1 —
the same choice `000150` makes server-side, so phone and database agree on what an
unlabelled pen-day video proves. The Room packing cache namespace is bumped
(`session-v3`); a stale `sessions`-shaped cached row would otherwise deserialize
WITHOUT ERROR into a card with no feed lines at all.

The afternoon correction (rule above) reopens **EVERY SESSION** of a pen whose head
count moved, never just one: head count scales the morning and evening ration alike,
so both videos now prove the wrong quantity and a partial reopen would leave one bag
packed for a head count the farm no longer has. `ReopenPackingForFeedChange` names
pens WITHOUT a session and applies no session predicate. A verdict already CAST is
kept as history (only a still-`pending` item is `withdrawn`), while the completion
row loses `verified_by`/`verified_at` and returns to `rework`.

Canonical prose: `docs/decisions/feed-distribution-verification.md` → "Feed packing
is proved ONCE PER BAG". Pinned by `TestPackingBagIsOnePerPenPerSession`,
`TestPackingLinesKeepPartitionsAndSessionsApart`,
`TestReopenPackingWithdrawsPendingItemsAndKeepsCastVerdicts` (mutation-tested two
ways: a session predicate on the reopen, and withdrawing a cast verdict — each turns
it red) and the `TestKernelStory_FeedAfternoonCorrection` E2E.

Confirmed feed-PACKING verification gate (maintainer decision 2026-07-26,
SUPERSEDING the "FEED PACKING IS DELIBERATELY NOT GATED" rule that the
feed-distribution lock above originally carried; its GRAIN was briefly superseded by
the 2026-08-10 pen-day rule and RESTORED by the 2026-08-11 rule above): feed PACKING
is now gated the same way as feed direction. The operator completes a packing
shed-session with
ONE MANDATORY packing VIDEO (`packing_proof_ref`); a completion missing it is
rejected 422 `proof_required`. That flips a NEW `feed_packing_completions` row to
`pending_verification` and enqueues ONE `feed_packing` verification item carrying
the video — NOTHING is completed yet. ONE verifier APPROVE
(`ApplyVerifiedPacking`) flips the shed-session to `completed` (this is when
`feed.packing.completed` is emitted); a REJECT (`BouncePackingForRework`) flips
it to `rework` for a re-shoot. Applies to BOTH `normal` and `experiment`
workflows; the serve overlay reads `ListPackingCompletionStatuses`. The gated
flow is a SEPARATE record on a NEW table, never an ALTER of the old packing
table. The OLD instant packing path — `feed_direction_session_completions`
(migration `000030`), `POST /feed-direction/complete`, `feed.direction.completed`,
`CompleteSession`, and the mobile `FeedCompleteScreen`/`feedCompleteRoute` — is
left INERT (no longer navigated to from packing) but not deleted; retiring it is
a separate cleanup. Both feed gates share `source.module=feed` and are kept apart
ONLY by `ref_type` (`feed_packing_completion` vs `feed_distribution_completion`).
Wiring: producer `feeddirection` `CompletePacking` +
`feeddirection/adapters/verificationbridge` `NewPacking`; consumer
`feeddirection/app.FeedPackingVerificationHandler`. Route
`POST /feed-direction/packing/complete` (registered in `permissions/routes.go`
alongside the distribution route, which had been unregistered and would 403).
Canonical source: `docs/decisions/feed-distribution-verification.md`; migration
`000033_feed_packing_verification_gate.sql`.

Confirmed Feed Transport daily verification rule (maintainer decisions 2026-07-29
and 2026-08-10, REAFFIRMED 2026-08-12 against a partition grain, SUPERSEDING
transport session/batch/consolidation wording): Feed
Transport is one daily task per active physical shed and is never per feed session.

**NOR PER PARTITION.** A shed's pens are packed and fed as separate bags, but they
are LOADED AND STAGED as one trip, so transport is ONE task and ONE video for the
whole shed. Migration `000143` fanned the materializer out over `shed_partitions`
and a partitioned shed began listing `Castro 1`, `Castro 2`, `Castro 3` as
three transport tasks -- three videos of one load. That was never a recorded
decision; it contradicted this rule and `docs/decisions/feed-transport-verification.md`
at the same time. `000152_feed_transport_restore_shed_grain.sql` is the forward
repair (`000143`/`000146` are NOT amended -- STG records checksums). It retires only
UNSTARTED pen tasks; a pen task already carrying an attempt keeps its status and its
proof, because an operator really filmed it. `partition_label` is kept and stops
being written. **Pen grain belongs to PACKING and DISTRIBUTION** -- those really are
one bag per pen -- and copying their shape onto transport is the specific mistake
this paragraph exists to stop.
The controlling source clock requires packed/diff-corrected feed to be loaded and
staged outside sheds by Day N 15:00 for Day N+1 service. The current 15:30 task
creation is compatibility behavior and a source/runtime defect: materialize and
assign the task early enough to complete by 15:00; a versioned route policy may be
stricter. The operator records one mandatory fresh in-app-camera video; submit
moves the task to
`verification_due`. Verifier APPROVE moves it to `completed`; REJECT moves it to
`rework` assigned to the same operator. Every rework requires a new video and appends
a new proof attempt; rejected proof attempts remain immutable history. Canonical source:
`docs/decisions/feed-transport-verification.md`; migration
`000054_feed_transport_daily_verification.sql`.

Confirmed verifier verdict-exclusivity rule (maintainer decision 2026-08-03, SUPERSEDING the
CEO/CxO `verification.review` override for the DECISION only): approve/reject on a verification
item belongs to the Verifier role ALONE. `verification.verdict` is split out of
`verification.review` and granted to `verifier` only — never `ceo_internal`, `pc_director`,
`growth_director`, `park_head`, or `operator`. Leadership KEEPS `verification.review` (see the
evidence queue, media, and recorded verdicts) and KEEPS `verification.act` (close the work, rework
or reassign the source task); it simply cannot sign the second check itself. Do not "fix" this by
restoring the verdict grant to CEO to satisfy the founder/builder visibility invariant — that
invariant is satisfied by the read, and an independent check the checked party can approve is not
independent. Route `POST /verification/items/{item_id}/verdict` is gated on
`verification.verdict`; `GET /verification/queue` stays on `verification.review`. The admin-web
`record_verdict` control and `isVerifierLensPrincipal` both key on `verification.verdict`.
Canonical source: `context/architecture/verifier-app-and-flow.md` → "Roles (truth table
alignment)"; pinned by `TestVerificationSeparationOfDuty` and
`TestVerdictRouteIsVerifierOnlyWhileQueueReadStaysLeadershipVisible`.

Confirmed TOXIN module rule (maintainer decisions 2026-08-25; a RECORDED, SCOPED exception
to the verifier verdict-exclusivity rule above that leaves that rule untouched): every feed
load recorded on `/procurement/feed-purchases` owes one aflatoxin strip test (SafetiX SHF
001-A), born automatically per feed-purchase row from `procurement.feed_purchase.recorded`
— never hand-created, no calendar, no due clock. The test is a 7-STEP GUIDED FLOW with
PROOF AT EVERY WORKING STEP: steps 1/2/3/5/6 one in-app-camera VIDEO each, step 4 a
settling wait (the farm does NOT centrifuge — the extract sits ~1 hour), step 7 one
in-app-camera strip PHOTO plus the reading (Negative/Positive/Invalid). ALL THREE WAITS
ARE HARD-BLOCKED ON THE SERVER CLOCK (60 min after step 3 → step 5; 3 min → step 6; 8 min
→ step 7); the phone renders server step states and never derives gate logic from its own
clock. Steps are PERSON-INDEPENDENT among `toxin.execute` holders; each completion records
who. An Invalid strip or a rejected review CANCELS the whole round and mints a fresh
retest task in the SAME transaction (`round_no+1`; one live round per load, enforced by a
partial unique index); rejects require a reason and never name a step. REVIEW IS CEO/CXO
ONLY: `toxin.verdict` is granted to `ceo_internal` alone on its own routes — the module is
an approval gate in the `counts_approver` shape, deliberately NOT a Verification category,
so the verifier never sees toxin work and `verification.verdict` stays verifier-only.
Access is PER PERSON via `toxin_tester` (`perPersonGrants`; today the two named park
heads) — never on the park_head/director job. `toxin.execute` is ORed into the
`/app/proofs/*` routes. **CEO/CXO WATCHES AND JUDGES BUT NEVER RUNS A TEST (maintainer
decision 2026-08-26, correcting the 2026-08-25 grant): `ceo_internal` holds
`toxin.read` + `toxin.verdict` and NOT `toxin.execute`.** Do not add it back — a CEO who
could film the steps would be approving their own evidence. Enforced on BOTH halves per
the capability-gated lock: the step/submit routes refuse leadership at the route table,
AND `can_execute` on `ToxinTask` (the CALLER's permission, resolved per request) makes the
composed payload render every unfinished step `locked` and the phone card non-tappable, so
no camera is ever offered for a write the server would refuse. Pinned by
`TestToxinExecuteIsTesterOnlyAndNeverCEO`, `TestWatcherSeesNoActionableStep`, and the
`ToxinTaskListViewModelTest` watcher case. v1: accepted Positive FLAGS the load, does not block feeding; no
FCM. Canonical prose: `docs/decisions/toxin-testing-module.md`; pinned by
`TestToxinVerdictIsCEOOnly`, `TestToxinTesterCarriesOnlyTestingAuthority`,
`TestToxinModuleIsOfferedPerPersonNotPerJob` (each mutation-tested when written).

Confirmed THE APPROVE CARRIES THE NUMBER rule (maintainer decision 2026-08-20, SUPERSEDING
the separate-save-act half of the 2026-08-17 weighing weight-correction and 2026-08-18 feed
wastage measurement decisions): where a verification item declares a measurement, the verifier
types the value and presses **Approve ONCE**. There is **NO separate save button**, on the
phone or in the admin-web drawer.

**Why this is a lock and not a preference.** Recording the measurement RELABELS the
verification item, and the relabel is `row_version = row_version + 1`. The verdict UPDATE is
version-fenced (`AND row_version = $6`), so the Approve pressed straight after a save carried
the version the screen had loaded with, matched no row, and SILENTLY DID NOTHING. Two acts for
one judgement, the second broken by the first, with no error the verifier could see. Do not
reintroduce a save button: it recreates the defect exactly.

Four parts, each load-bearing:

1. **The number rides the verdict.** `measurement` on
   `POST /verification/items/{item_id}/verdict`. It names NO target — the record it lands on is
   resolved from the ITEM's own source, because a client that could name its own target could
   aim one item's approve at another item's record.
2. **Verification still does not know what the number MEANS.** It reaches the write through
   `verificationapp.MeasurementApplier`, registered per category at composition time exactly
   like the enqueue/withdraw/relabel seams producers already register. Each applier forwards to
   the SAME producer service its standalone route calls, so range checks, idempotency, audit and
   relabel are ONE implementation. Do NOT make verification read a producer's table.
3. **`RequiredForApprove` is TRUE for feed wastage and FALSE for weighing, and that asymmetry is
   the rule, not an oversight.** Wastage's operator submits a VIDEO AND NO NUMBER, so the
   reading is born on the verifier's screen and approving blank would complete a pen-day with no
   wastage recorded at all — checked BEFORE the verdict, because the producer's own
   `ErrWastageMeasurementRequired` fires in the CONSUMER, after the verdict is durable, and
   strands the item mid-apply. Weighing's operator already recorded a weight, so blank means
   "his weight is right" and MUST stay a single tap.
4. **A REJECT never carries the number.** Rejection sends the work back to be recorded again, so
   a value written onto a record about to be redone is a number nobody will use. Reject is also
   never held on the measurement: a reading that cannot be taken off the clip is exactly the case
   that must be sent back.

Order inside one request: fence on the version she had on screen -> apply the measurement ->
re-read `row_version` (it moved through OUR relabel, not a competing verifier's) -> record the
verdict. Concurrency is still fenced, because the verdict UPDATE also requires the item to be
`pending`. A producer that refuses the value stops the whole approve rather than leaving an
approved item beside a number that never landed.

Both producer routes (`.../weight-correction`, `.../wastage/{id}/measurement`) STAY SERVED for
installed APKs that still show their own save button, and an item measured that way is still
approvable — the applier is asked whether a value is already recorded. No current client calls
them; do not build a new one that does.

Canonical prose: `docs/decisions/feed-distribution-verification.md` -> "THE APPROVE CARRIES THE
NUMBER". Pinned by `backend/internal/verification/app/verdict_measurement_test.go` (which keeps
the save-then-approve 409 reproduced as the defect being replaced) and the Android
`VerifyDetailViewModelAnalyticsTest` approve/reject pair; each was mutation-tested when written.

Confirmed Approvals-on-mobile rule (maintainer decision 2026-08-05, SUPERSEDING the
2026-07-21 decision that removed approvals from mobile and moved them to admin-web
only): the birth/death/shifting approval queue is BACK on the phone, as its OWN
module (`approvals`), not as a tab inside Counts.

Two halves, and the second is the one a later session will break by accident:

1. **Approvals is a separate module.** Counts stays CAPTURE-ONLY on the phone
   (birth, death, shifting for holders of `counts.write`) and must NOT regain an
   approval tab. Approving is not capturing and the audiences barely overlap: the
   two named approvers hold no `counts.write`, and operators hold no approval
   authority. Pinned by `TestCountsModuleRoleMatrix`,
   `TestCountsModuleBarIsCaptureOnlyAndOmitsYouTab`, and the Android
   `TopLevelChromeTest`.
2. **The authority is granted PER PERSON, never per job.** The maintainer's words
   were "keep rbac per person, not per group". `permissions.RoleCountsApprover`
   (`counts_approver`) carries exactly `counts.approve_access` /
   `counts.approve_lifecycle` / `counts.approve_shifting` and NOTHING else — no
   bootstrap, no read, no write — and is granted to NAMED individuals alongside
   their job role. Today: Chandrakant (`pc_director`) and Dinakar
   (`growth_director`). Their job roles are byte-for-byte unchanged, so a future
   PC Director or Growth Director inherits no approval power by holding the job.

**Do NOT "simplify" this by adding the approval permissions to `pc_director` or
`growth_director`.** That hands the authority to every future holder of those
jobs, reverses the one-module-one-director segregation lock
(`director_module_segregation_test.go`), contradicts "growth_director runs
Weighing and ONLY Weighing", and overrides `health_director` as the documented
Counts owner. `TestApprovalsModuleIsPerPersonAndLeavesCountsCaptureOnly` and
`TestCountsApproverRoleCarriesOnlyApprovalAuthority` both go red if it is tried;
both were mutation-tested when written.

The named list is `perPersonGrants` in
`backend/cmd/seed-stg-login-grants/approvers.go` — adding an email there IS the
act of granting approval authority, on the phone and admin-web alike (one
permission, one set of routes, two surfaces). Role catalog row: migration
`000108_counts_approver_role.sql`. Canonical prose:
`docs/runbooks/current-active-rbac-roles.md` -> "`counts_approver` is granted by
NAME".

Reaffirmed and widened 2026-08-07: `perPersonGrants` is now the general
"this person, not this job" list. Chandrakant holds `counts_approver` + `operator`
alongside `pc_director`; Dinakar holds `counts_approver` + `pc_director` +
`growth_director` + `operator`. The maintainer was offered the alternative of
moving counts authority onto the `pc_director` ROLE and declined it, so the lock
above stands unchanged. Two consequences worth carrying forward: those `operator`
grants are TENANT-scoped (allowed only because they are layered on directors, and
each must carry a `stg-operator-scope: tenant approved` justification in its own
block — block-scoped enforcement in `check-stg-operator-scope.mjs`), and a tenant
`operator` grant does NOT add anyone to the vaccination drive operator pool, which
reads `workforce_positions` with `position_tier <> 'director'` rather than the RBAC
role.

Extended 2026-08-07 (same day, second decision): those two ALSO get the Herd
Operations (Counts) CAPTURE module on the phone — birth, death, shifting — and
they get it the per-person way, keyed on the explicit `operator` GRANT they hold,
never on their director job. `leadershipModuleKeys` offers `counts` when
`hasRole(grants, RoleOperator)`. A bare `pc_director` or `growth_director` is still
offered nothing, so a future holder of either job inherits no capture, and the
segregation lock above stands unchanged.

Why the GRANT and not the `counts.write` PERMISSION, which reads like the obvious
key and is wrong: `park_head` holds `counts.write` on the ROLE, and
`TestCountsModuleRoleMatrix` pins that a park head does NOT get the capture module.
Keying the offer on the permission compiled, passed the new test, and silently
handed Counts to every park head — the same per-job widening one layer over. That
pre-existing test is what caught it. The offer is now keyed on the `operator` grant,
which is exactly what `perPersonGrants` layers onto a named individual.

Note also that leadership module offers are NOT reachable from the database:
`candidateModuleKeys` returns `leadershipModuleKeys(grants)` for any principal
holding a leadership role and never consults `department_module_grants`. Giving a
director a module is therefore always a code change — there is no grant row that
does it. Pinned by `TestHerdOperationsIsOfferedPerPersonNotPerDirectorJob`
(mutation-tested three ways: branch removed, keyed on the job, keyed on the
permission — each turns it red).

Same change closed a copy-firewall defect on that queue: the phone used to render
`Raised by <uuid>` and `to shed <uuid>` because it composed the row's copy itself
from the echoed payload and had no name source. The backend now owns both lines
(`raised_by_name`, `summary_line` on `CountsApprovalListItem`), resolving ids to
names in ONE batched query per entity kind, and a fact whose name cannot be
resolved is DROPPED from the line rather than rendered as an id. Clients render
both verbatim; do not reintroduce client-side composition of that copy.

Confirmed RANDOMIZED VERIFICATION SAMPLING rule (maintainer decision 2026-08-26): the CEO sets, per
verification category, the PERCENTAGE of that category's proof videos the verifier actually has to
watch. Her day is complete when she has cleared HER SHARE -- at 40% on feed packing, reviewing those
40% IS 100% of her work, and the progress number is backend-owned so no surface derives its own.

An UNSAMPLED video is AUTO-ACCEPTED, never left hanging, and that is the load-bearing half. Verifier
approval is not merely review for feed and weighing -- it is the gate that COMPLETES the work (a feed
pen-session stays pending_verification until an approve lands; a weighing bucket cannot close while
verification is pending, ledger D-5, unconditional). Hiding the unsampled ones would stall those
workflows forever, so the closeout stage approves them with
`verification_items.auto_resolution = 'not_sampled'` and NO verified_by, emitting the ordinary
`verification.verdict.approved` event -- every producer's consumer applies exactly as it does for a
human approve. There is no second apply path, and a waived item can never be counted as her work.
Sampling decides what gets WATCHED; a video nobody watched is never evidence the work was wrong, so
a waived item is always an approval and never a rejection.

Four narrowings, each load-bearing. (1) The draw is DETERMINISTIC AND MONOTONIC -- `sampling_bucket`
is a GENERATED column, in sample when `bucket < percent` -- so raising the share mid-day only ADDS
videos and can never retract one she is already holding. That is what makes "takes effect the same
day" safe. (2) The policy is EFFECTIVE-DATED: a change writes a row at TODAY's business date and a
past day keeps the percentage it actually ran at; the date is the SERVER's, never the client's.
(3) A category whose approve must CARRY a measurement (feed packing's packed quantities, feed
wastage's leftover weight) is LOCKED at 100% and the write is refused `sampling_not_available` --
there the verifier is the DATA SOURCE, not a spot check, and waiving would either record no quantity
at all or strand the item mid-apply. Derived from `MeasurementCorrection.RequiredForApprove`, never a
hardcoded list. (4) The CLOSEOUT settles only CLOSED business days, because a video waived the moment
it arrived could not be recruited back by a raise that afternoon.

THE SHARE IS A FLOOR, NOT A CEILING (maintainer decision 2026-08-27, raised in review). A verdict on
an item the policy did NOT draw is ACCEPTED and recorded as a HUMAN verdict (`verified_by` set,
`auto_resolution` NULL). There is deliberately no sampling gate on the verdict route, and adding one
would make bad work unreportable -- she watches an undrawn video, sees the work was wrong, and the
rejection is refused so the work proceeds to `completed` -- as well as discarding a review already
performed, since only a LOWERED share can drop an item she was holding. It mislabels nothing:
`not_sampled` is the contract for a video NOBODY reviewed, `Reviewed`/`Selected` are share-scoped so
an extra review cannot pass 100%, and the closeout skips any item a verifier already decided.
Sampling is NOT an authorization boundary; what takes an item out of her reach is leaving `pending`.
Reported as a P1 in review and closed as working-as-decided -- do not re-open it without reading
`context/repo-audits/verification-randomization-do-not-reopen-ledger.md` -> B-1, which carries the
reasoning, what a REAL defect here would look like, and the two stricter variants already costed.

`permissions.VerificationSampling` is CEO-ONLY and narrower than every other capability on /verify:
`pc_director` holds VerificationOversee and does NOT hold this. Oversight WATCHES the verification
workload; randomization DECIDES how much of it a human must watch, and a director setting that for
his own department's work is the separation of duty that keeps VerificationVerdict off leadership.
The VERIFIER's queue is narrowed by the policy; LEADERSHIP's is not -- the principal who sets the
percentage must be able to audit what it waived. Canonical prose:
`docs/decisions/verification-randomization-sampling.md`; schema: migration
`000214_verification_sampling.sql`; the cross-surface impact table (what sampling does to the KPI
strip, the vaccination live tracker, the People proof stats and the verifier push) is in that same
decision doc, and every row of it is asserted by `TestKernelStory_VerificationRandomization`. Two
rules fall out of it and bind future changes: a count of what a PERSON STILL OWES uses
`verification/samplingsql.InSample` (drawn items only), and a count of what a PERSON DID excludes
`auto_resolution IS NOT NULL` -- a settled item carries the closeout's `verified_at` and would
otherwise read as a verdict nobody cast, collapsing the reject rate with approvals no one decided.
Note also `backend/internal/verificationcatalog`: the category
set is now read by TWO processes (the API's registry and the worker's closeout), and a worker holding
a hand-copied subset would not fail loudly -- it would silently never settle the categories it was
missing. Declaring a category inline in `bootstrap/api.go` is blocked by
`TestBootstrapDeclaresNoCategoryOfItsOwn`.

Confirmed PAGE-GRAIN ACCESS rule (maintainer decision 2026-08-27, SUPERSEDING the MECHANISM
-- and only the mechanism -- of the 2026-08-21 procurement-director workspace decision, whose
OUTCOME is preserved byte for byte): **a person's admin-web sidebar is exactly the pages ticked
for them on /people.** One layer, editable by a human.

Until now TWO layers decided it and they disagreed. PERMISSIONS said what someone may do; a
LENS -- hand-written Go keyed on a ROLE -- then deleted nav leaves and page contracts regardless.
The Procurement Director HOLDS `feed_config.read/write`, `operators.*`, `roster.*` and
`verification.act` through the `feed_director` role he also wears, and saw none of it, because
`procurement_director_lens.go` kept only the Procurement and Feed groups and hid `/feed/config`.
Both layers were right about their own question; together they meant the People access editor
(which reads permissions) advertised modules he could not reach, and every future "this person
should not see that page" was a commit.

`adminui/app/procurement_director_lens.go` is DELETED. Its narrowing was written onto that
person's OWN rows by the backfill, once, as data (`NarrowForRetiredLenses`).

**THE VERIFIER LENS STAYS** (maintainer instruction, same day) and is not the same kind of
thing: it does not subtract from the ordinary console, it composes a DIFFERENT workspace -- a
queue, its own registry-built modules, its own landing. Retiring it would delete a product
surface rather than a narrowing. It is still checked FIRST, so a verifier is never page-narrowed.

**THE PHONE DOES NOT CHANGE.** Android composes its bar from the mobile module registry; a page
tick is web-only and never reaches it. Operator access is untouched.

Four properties, each load-bearing:

1. **AN EMPTY PAGE LIST MEANS EVERY PAGE OF THAT MODULE.** This is what makes a page shipped
   tomorrow reach whoever already holds the module, instead of silently reaching nobody until
   someone re-ticks thirty people. Narrowing is opt-in: you have to say "not that one".
2. **THE CATALOG IS ASSERTED AGAINST THE REAL NAVIGATION.** `permissions.ModulePages` carries
   every nav leaf; `TestEveryNavLeafIsATickablePage` fails if a leaf ships without a row (it
   would be unwithholdable) and `TestEveryPageContractRouteIsOwnedByAModule` fails if a page
   contract's route belongs to no module (it could never be narrowed). Adding a screen without
   a catalog row is a build failure, not a silent hole.
3. **FAIL OPEN ON ABSENCE AND ON ERROR.** A person with no stored rows is NOT narrowed -- they
   are still on the retired role path, and narrowing them to nothing would lock out anyone the
   backfill has not reached. A source error is logged and the full contract served. The sidebar
   is a convenience; every route behind it is independently permission-gated, and the 403 is the
   lockout.
4. **TWO REFUSALS ON THE WRITE PATH.** A page key from another module is REJECTED (a dropped
   tick reads as granted while granting nothing). A granted module with screens and NONE ticked
   is REJECTED -- it would resolve to every page by property 1, the opposite of what the admin
   just did on screen.

**A SCREEN IS OFFERED ONLY WHEN IT CAN BE OPENED**, and this is the fifth property rather than
a detail. Every page declares the permissions its own screen needs, and a screen the person
cannot open is never ticked -- so it can never render greyed. The catalog and the navigation
gate (`adminui/app.permissionsForNav`) are asserted IDENTICAL by
`TestPageCatalogPermissionsMatchTheNavigationGate`; they are two layers and drift between them
is what produced dead rows. Openability is computed from the person's WHOLE permission set,
not the owning module: Feed SOP is grouped under Feed and needs `sop.read` from Protocols &
SOPs, and checking only the owner hid it from the CEO. A module is where a screen is TICKED,
never where its authority comes from. Note also that permissions union across SURFACES, so
removing a module from web removes the SCREENS while the phone's own grant still carries the
ability -- consistent, because a route does not know which surface called it.

The live sweep that proves all of this is `tools/dev/audit-person-access.py` (930 checks over
all 31 people, both surfaces). Its PASS 3 opens the DATA ROUTE behind every visible leaf and
fails on a 403; that is what found NINE dead leaves for four real people, every one of them a
pre-existing leaf with no navigation gate at all, plus Counts Breakdown gated on `goat.read`
while `/counts/breakdown` checks `counts.read`. Run it after any change to the access model --
it needs the local stack, so it is deliberately not in CI.

Resolution is `permissions.PageAccessForAssignments`, read by BOTH the bootstrap narrowing and
the access editor, so the ticks the screen shows are the ticks the sidebar obeys. Schema:
migration `000220_person_page_access.sql`. Canonical prose:
`docs/decisions/per-person-page-access.md`. Pinned by
`TestRetiredProcurementDirectorLensIsReproducedByTicks` (the holder's two stacked roles produce
exactly the six leaves his live bootstrap served on 2026-08-27, and no page contract for
`/feed/config`, `/people`, `/verify` or `/`) and `TestCeoIsNeverNarrowed`.

Confirmed verifier admin-web workspace rule (maintainer decision 2026-08-03): the
verifier-only workspace, previously mobile-only, also runs on admin-web with the SAME
five evidence modules as mobile — Vaccination, Weighing, Counts, Feed, Health. `verifier`
now holds `admin_web.bootstrap`, but that opens the SHELL ONLY. A principal holding
`verification.review` and NOT `verification.act` receives the verifier LENS: the sidebar
is composed from the Verification type registry's navigation metadata (one group per
`NavigationModule`, one leaf per `PageKey`, each pointing at
`/actions?category=<disjoint category>`), and EVERY other admin-web page contract is
dropped so a typed URL fails closed at `requireAdminWebPageContract`. Never express this
as a per-role nav template — registering a producer category is the only way to add a
module, and `make nav-composition-guard` still applies. CEO/CxO holds review AND act as
the documented override and therefore keeps the full admin IA; the lens must never narrow
a leadership principal. `/actions` serves both personas, split by the page contract's
controls: `record_verdict` (`verification.review`) vs `request_rework`/`reassign_task`
(`verification.act`). A page rendering from LOCAL literal copy has no contract to
withhold and must gate itself with `adminWebRouteOffered` (`/approvals` does). Known
boundary: the rework/assign routes require `task.verify`/`task.assign` and a verifier
holds `task.verify`, so that half of the split is contract-layer, not a backend lockout on
that shared SOP route. Canonical source: `context/architecture/verifier-app-and-flow.md`
→ "Verifier WEB workspace"; code `backend/internal/adminui/app/verifier_lens.go`.

Confirmed AFTERNOON FEED CORRECTION rule (maintainer decision 2026-08-10,
SUPERSEDING the APPROVAL half — and only that half — of the 2026-07-27 projection
rule immediately below): **a RAISED shifting counts toward the feed sheet before
a park head approves it, and the 14:00 correction reopens any pen already packed
against the old count.**

The defect it fixes: a low-priority movement raised at 09:00 is due TOMORROW, but
tomorrow's normal sheet was issued at **07:00 that same morning** and is already
being packed. Ten animals arriving in a pen fed for one had no feed at all,
because the projection waited for authorization. Under-feeding animals that
really arrive is worse than over-packing for a movement the park head later turns
down.

Three parts, and each narrowing is load-bearing:

1. **Approval no longer starts the feed clock; only REJECTION stops it.** The
   projection now counts `authorization_state='pending' AND event_status='pending'`
   alongside the existing authorized set. The two branches are disjoint on
   `authorization_state`, so one movement contributes exactly once as it travels
   from raised to approved. `rejected`, `canceled` and `applied` are excluded by
   construction — a movement that is turned down stops feeding a shed at once.
   The effective date for a RAISED movement is the **ACTIONS lead time**
   (`ShiftingActionsDueFrom`: low priority raised before 13:30 IST → tomorrow, at
   or after 13:30 → the day after), **not** the raise day. This is the one place
   the two rules deliberately meet: an unapproved movement has no authorization
   instant, and the honest answer to "when do these animals eat here" is the day
   they are expected to walk. Anchoring on the raise day would feed a destination
   a full day before a 13:45 raise's animals move.
   Canonical rule: `counts/domain.FeedShiftingRaisedEffectiveBusinessDate`.
2. **The 14:00 correction REOPENS an already-packed pen — EVERY SESSION of it.**
   The correction (`correction_time`, already 14:00 for both workflows — this rule
   adds no new clock) recomputes the frozen sheet, and any pen whose packing was
   already submitted goes back to `rework` with an operator-facing sentence, its
   still-pending verification item `withdrawn`, and `verified_by`/`verified_at`
   cleared. An **already-APPROVED** video is reopened too: it proves the packer
   packed the OLD quantity, which is now the wrong quantity, so an approved clip
   is no more usable than an unapproved one. A verdict already CAST is kept as
   history rather than rewritten — only a still-`pending` item is `withdrawn`,
   because that is the one sitting in a verifier's queue pointing at a stale clip.
   **BOTH of a pen's bags come back** (2026-08-11, once packing returned to the
   shed-SESSION grain): head count scales the morning and the evening ration alike,
   so a partial reopen would leave one bag packed for a head count the farm no
   longer has. `ReopenPackingForFeedChange` names pens WITHOUT a session and applies
   no session predicate.
3. **Two narrowings that must not be widened.** *Experiment is EXEMPT* — and as of
   the 2026-09-01 per-animal decision below, that is a KEPT TRADE rather than an
   arithmetic fact. Its rations used to be absolute kg per pen, so a head-count
   change moved no quantity there and reopening one would have discarded a good
   video for a sheet that did not change. Experiment cells are now authored as
   grams per animal and DO move with the head count; the maintainer was shown that
   consequence and kept the exemption, accepting that an experiment pen whose count
   moves between packing and the correction keeps a video proving the
   pre-correction quantity. *HEAD COUNT ONLY, PER PEN* — `AffectedShedIDs` also fires for a
   relabelled ration group and is shed-wide, so driving the reopen from it would
   make the packers of Castro 1 and Castro 3 refilm because Castro 2 gained
   animals. Making an operator refilm is expensive; it is spent only where the
   number of mouths actually moved. Canonical rule:
   `feeddirection/domain.CellDiff.HeadCountChangedPens` →
   `app.reopenPackingForCorrection` → `ports.ReopenPackingForFeedChange`.

There is **no new state**: a reopened pen uses the existing `rework`, which
normalizes to the client bucket `pending` ("needs my action again"). That means
the CHIP CANNOT distinguish a reopened pen from one nobody has packed — the
backend-composed `rework_reason` on `FeedPackingRow` is the only thing that can,
so it must never be dropped from the contract or replaced by client-side copy.

**No lock is lifted and none may be.** The transport lock is 15:30, after the
14:00 correction, so the correction was never blocked by it; `ErrAmendAfterLock`
stays. Do not add a path that amends a locked sheet — past the transport cutoff
the feed has physically left and a correction cannot reach the shed.

Pinned by `TestFeedShiftingRaisedEffectiveBusinessDate`,
`TestRaisedAndAuthorizedRulesStayDistinct`, `TestDiffCellsReportsOnlyTheChangedPenOfASharedShed`,
`TestAfternoonCorrectionNeverReopensExperimentPacking` and
`TestPackingReworkReasonIsCarriedOnlyWhileThePenIsActuallyInRework`. Every one of
those was mutation-tested when written: deleting the experiment branch, keying the
reopen on the shed, or widening it past head count each turns one red.

Confirmed feed-direction shifting-projection timing rule (maintainer decision
2026-07-27; its APPROVAL requirement is SUPERSEDED by the 2026-08-10 afternoon
correction rule ABOVE — a raised movement now counts before approval — while
everything below about AUTHORIZED movements, zero lead, no priority branch and
the applied/pending_verification boundary stands unchanged. This rule itself
SUPERSEDED the priority-based lead-day rule — normal 2-day /
high-priority 1-day — that the projection previously applied): the feed sheet's
projected shed head count = the live herd PLUS every authorized-but-unexecuted
shifting, with NO lead time and NO priority branch. A shifting is a pending feed
input the moment a park head AUTHORIZES it, so it counts toward the next feed
sheet immediately (destination shed +heads, source shed −heads), affecting ONLY
the feed projection and never the census counts. "Forget high priority": normal
and high-priority movements are treated identically for feed timing (high still
completes same-day operationally, so it lands in the live herd quickly anyway).
A movement stops counting in the projection ONLY when it is `applied`
(verifier-approved), at which point its animals already sit in the destination
shed in canonical `goats` — so the delta must count BOTH `authorized` AND
`pending_verification` (operator completed with proof, not yet approved, animals
NOT yet relocated) and EXCLUDE `applied`, or the shed is either double-fed
(counting applied) or under-fed (dropping pending_verification). The "overdue"
flag fires only when a counted movement was authorized BEFORE the packing day
(feed day − 1) and is still unexecuted, so a freshly authorized move under the
zero lead does not spuriously read as overdue. Canonical source: the pure-Go
spec `backend/internal/counts/domain.FeedShiftingEffectiveBusinessDate` /
`FeedShiftingCountsToward` / `FeedShiftingIsOverdue` and the SQL it mirrors in
`counts/adapters/postgres/feed_projected_counts.go` (`event_status IN
('authorized','pending_verification')`). Proof:
`counts/domain.TestFeedShifting*` and
`counts/adapters/postgres.TestFeedProjectionTimingRule` /
`TestFeedProjectionExcludesAppliedMovements` (includes the pending_verification
case). This changes ONLY the shifting-aware feed projection; the 7:30-style
auto-issue scheduler and the calendar surfacing of next-day feed remain
separate, unbuilt items.

Confirmed Feed Direction stage-clock rule (maintainer decision 2026-08-10):
the source default is Day N 09:00 full direction for Day N+1, Day N 13:30
shifting cutoff, Day N 13:30-13:45 Diff, Day N 15:00 packed/diff-corrected feed
staged outside sheds, then Day N+1 09:00 and 15:00 serving slots. Packing,
transport, and distribution are time-bounded work, not timeless claim pools.
The source requires packing/loading/transport staging outside sheds to be
complete by Day N 15:00. The current Transport materializer's 15:30 creation is
a source/runtime defect, not an accepted extension: create and assign the task
early enough to meet the 15:00 hard deadline; route/park policy may be stricter.
Missing owner, stock/config, route, vehicle, system, or proof readiness is
attributed before any person-level candidate. Follow
`docs/decisions/task-timing-alerting-violations-and-appeals.md`; a verifier delay
is never charged to the operator.

## Domain Event Integration Is Mandatory

Backend, admin-web, and mobile business mutations all use the same domain-event
architecture. Any CRUD/import/sheet/mobile-offline/worker path that creates,
moves, closes, reclassifies, or consumes business state must register producer,
event, consumer, replay/DLQ behavior, and E2E proof in
`context/architecture/domain-event-registry.json`, following
`context/architecture/domain-event-integration-contract.md`. Run
`make domain-event-architecture-guard`. This guard is part of the mandatory
`run_common` path in `make ci-local`; an optional compatibility job or a textual
mention elsewhere is not accepted as CI wiring.

Vaccination FCM is part of that event spine, not a client feature flag. Every
push-facing vaccination state must have an explicit contract for trigger,
audience source, cadence/SLA, message summary, and tap route. Token delivery
targets come from active `workforce_member_devices`; tenant leadership
recipients (`ceo_internal`, `pc_director`, future CXO/director aliases) resolve
from active role grants/profile truth, never from the single-seat
`workforce_positions` table alone. Routine day-start/afternoon operator nudges
stay field-scoped; the 20:30 IST due-today checkpoint includes PC director/CEO
leadership when scheduled sheds are still not submitted. Shed proof submission
immediately notifies the park verifier(s) and leadership with role-specific
routes: verifier to video review, leadership to Vaccination overview. Run
`make fcm-recipient-routing-guard` with local CI for any notification change.

## Operational Read Model Contract Is Mandatory

Shared command surfaces (Calendar, Control Tower, Action Center, Protocol
Adherence, Workflows, admin-web detail pages, Android execution/proof screens,
and CEO/AI reporting) are renderers of backend-owned operational read contracts;
they must not invent private business truth or recompute whole-result totals
from page-local rows. Every new vertical or module, including shifting, counts,
breeding, weighing, feed, procurement, and future preventive-care modules, must
plug into the pattern in
`docs/architecture/operational-read-model-contract.md` before it is exposed on a
shared surface.

Mandatory rules for Claude, Codex, and human developers:

1. Name the grain of every shared count and status bucket (`animal`,
   `obligation`, `completion`, `proof`, `verification`, `shed`, `partition`,
   `drive`, `park_day`, `task`, `alert`, etc.).
2. State whether buckets are disjoint or overlapping. Do not add overlapping
   counts in UI unless the contract explicitly defines a union count.
3. Summaries are whole-filter aggregates unless explicitly named `page_*`.
   Pagination changes rows only, never summary truth.
4. Selected operational scope must use stable identity. Rule ID alone is not a
   drive selector when rules recur across dates, sheds, partitions, operators,
   or batches.
5. Backend response structs, OpenAPI, generated TypeScript clients, Android
   DTOs, admin-web renderers, and mobile renderers must move together.
6. A new vertical is not pluggable until it declares its canonical write owner,
   work-item identity, scope grain, time grain, state machine, evidence model,
   shared summaries, Calendar representation, Control Tower representation, and
   mobile/admin surface contract.
7. Cross-surface golden fixtures are the proof: the same fixture must make
   Calendar, Action Center, Protocol Adherence, Control Tower, Workflows, Admin
   Web, Android, and reporting agree on the facts they share.

Run `make operational-read-model-contract-guard` for any change touching shared
read models, OpenAPI, admin-web command lenses, Android execution/proof screens,
or new vertical/module onboarding. This discoverability/static-text guard is
part of local CI, but it is not a semantic Go/OpenAPI/Kotlin/frontend drift
checker yet.

## Critical Animal Action Guardrails Are Mandatory

Quarantine, ICU, death, contagious-disease isolation, high-risk movement, and
sale/allocation blockers are critical animal actions, not ordinary CRUD. Until a
complete policy-pack module owns a transition, every route/UI/action must fail
closed or return a deterministic guardrail-required reason as described in
`docs/features/critical-animal-action-guardrails.md`. Run
`make critical-animal-action-availability-guard` for movement, health,
vaccination defer/reopen, or Goat Passport changes.

Shared vaccination drive tasks are aggregate bookkeeping only. A hidden park/
batch-level `sop_tasks.state` must not be used as per-shed submitted/proof/
verification truth in WF, CT, AC, Calendar, Android, or verifier queues. Shed
grain state comes from shed-scoped facts: `sop_submissions`,
`sop_submission_items`, `vaccination_completions`, and `proof_artifacts`, joined
by the active shed/submission/batch grain. The mobile shed-submit idempotency key
must include the active shed scope, and backend submit must stay idempotent when
another shed on the same shared parent already submitted. That sibling allowance
stops at `needs_review`: once the shared parent is `accepted`, fresh submit keys
must fail before writing any new submission, fanout, audit, or movement side
effect; only exact idempotency replay may read back the existing result.
Write-path grain is part of the same rule, not a separate implementation detail:
a shed-level proof submission may receive broad scan captures for the shared
parent task, but it must filter `SubmissionItems` to goats whose current
`goats.shed_id` matches the shed `subject_id` in completed `proof_refs` before
inserting `sop_submission_items` or `vaccination_completions`. Never "fix" a
WF/CT/AC/Calendar/Android review leak by changing display precedence while the
shared parent write still materializes sibling sheds. The mandatory regression is
an adversarial two-shed submit where one proof carries shed A, the command also
contains shed B scan items, and shed B writes zero submission items/completions.
Run `make goat-shed-scope-guard` and the targeted Postgres SOP/PI tests for any
submit, proof, verification, or projection change.

Frontend/mobile render backend-owned contracts and send idempotent commands; they
do not create private business follow-up pipelines. Direct live-animal table
writes are allowed only through registered canonical producers or approved seed
closeout paths. Future shifting, dead-birth, feed-direction, procurement, and
vaccination changes must plug into this same event spine.

Register BOTH ends, every time (Claude AND Codex): a producer with no consumer
on both durable buses is a silent drop, a consumer with no producer is dead
code, and a payload KEY no consumer parses is an accept-and-discard that reads
to the next author as already honored. Delete the unread key and its struct
field, or name the handler that reads it in the registry. See
`.agents/skills/domain-event-architecture/SKILL.md` and
`docs/decisions/scale-anti-patterns.md` -> "Operator-cascade wiring
anti-patterns".

## Grain Predicates and Executable Gates (Mandatory, Claude AND Codex)

Three defect classes recur across unrelated modules and must be checked on every
change that reads a plan/aggregate row, writes a rule into a doc, or loads a
committed fixture:

1. **Write the grain proof down; the grain rule itself already exists.** The
   aggregate rule above ("identify the canonical membership source, use the same
   stable group key on producer and consumer, prove every join is 1:1 or
   pre-aggregate the many side") is not new, and FIVE instances shipped anyway —
   so this is an adherence failure, not a missing rule, and restating the
   principle a sixth time fixes nothing. What is mandatory now is the written
   proof, next to the `projection-review:` marker: (a) the producer's unique
   column list and the consumer's match/group column list, side by side; (b) the
   row multiplicity of every joined side; (c) for any ratio or cap check, the key
   set each of numerator and denominator ranges over, shown identical. Check all
   three of `WHERE`, `GROUP BY`, and the compared-against key set — a complete
   predicate with a collapsed `GROUP BY` is the same defect one clause over
   (BUG-027: cohort spans N dates, `GROUP BY a.operator_id` collapses cap to one
   operator-day). If those three lines cannot be written, the query is not
   reviewable. `ORDER BY ... LIMIT 1` over rows the producer can legitimately
   duplicate fabricates an answer — the fix is an exact membership source, not a
   better ranking. Both sub-shapes, all five sites, and the mandatory
   mixed-vaccine / two-partition / two-date fixture:
   `docs/decisions/scale-anti-patterns.md` -> "Read-model grain is not the grain
   the consumer assumes".
2. **A documented rule with no executable check is not a gate.** When a runbook,
   validation doc, or fixture README states an automatic-failure condition or a
   required step, grep for the code that enforces it in the same change. If
   there is none, the finding is the missing check. Enforcement belongs in the
   `make` target that performs the mutation.
3. **Fixture/contract loaders must fail loud on unknown keys.** `encoding/json`
   drops unmatched keys silently, so a fixture block with no struct field seeds
   nothing and still reports success. Loaders of committed fixtures use
   `Decoder.DisallowUnknownFields()` or an explicit schema pass.

A guard is only as strong as what it can see. A literal-token grep sold as an
architectural boundary enforces the string from the original incident, not the
rule; when the rule is "package A must not depend on package B", check the
import graph, and state every remaining blind spot in the guard's own header
comment with a self-test fixture for each.

## Whole-Packet Review Scope (Mandatory, Claude AND Codex)

When the maintainer gives a review/fix/landing packet, treat the entire packet as
the task goal until proven otherwise. That includes PR numbers and merge state,
screenshots or attached docs, pasted reviewer notes, prompts, fixes claimed by
other agents, lenses, judges, sub-agent briefs, branch/base SHAs, and any
maintainer corrections in chat. Do not narrow the task to only the first visible
diff, only `origin/main`, only one PR, or only a screenshot table unless the
maintainer explicitly says to ignore the rest.

Before reporting "pending bugs only", "already fixed", "not a bug", or "nothing
to push", reconcile every finding against the complete packet and the current
candidate SHA. If a document says a finding was fixed by a later PR/SHA, verify
that later PR/SHA is actually in the reviewed candidate. If the maintainer asks
whether PR 115 was reviewed, answer from evidence that includes 115, not from a
stale main checkout. If the packet names lenses or judges, run or inspect those
review surfaces as first-class acceptance criteria, not optional commentary.

## Root-Cause Fixes Only — No Partial / Surface Fixes (Mandatory, Claude AND Codex)

When fixing ANY reported bug (review finding, audit item, regression):

1. **Reproduce the EXACT failure FIRST.** Write a failing test that reproduces the
   precise scenario described (the retry path, the race, the production caller, the
   >cap input), and confirm it FAILS on current code. No fix without a red test that
   models the real failure — not the cited line in isolation.
2. **Fix the ROOT CAUSE, not the symptom.** Trace the actual PRODUCTION path. Do NOT
   patch a sibling method, an adjacent symptom, or the one line quoted and declare
   done. If the production caller invokes a different method than the one you changed,
   you have not fixed it.
3. **A green narrow unit test is NOT proof** if it does not exercise the production
   caller, the retry/partial-failure/edge path, or the concurrency race. Prove the fix
   on the real path.
4. **Never report "fixed" / "already fixed"** without pasting failing-then-passing
   evidence on the real path. "Looks fixed", "compiles + tests pass", and "the guard is
   green" are NOT closure. Verify against the exact failure condition the reviewer gave.
5. Applies to sub-agents too: an orchestrator MUST independently re-verify each
   sub-agent's claim (run the failing test on old code, confirm it fails; on new,
   confirm it passes) before landing — sub-agents have repeatedly done shallow
   "already fixed" passes.

## Operational Task-Kernel Non-Deviation Lock (Mandatory)

Maintainer decision 2026-08-10: Goat OS is one event-driven, interlinked
task/ticketing waterfall. Every operational feature follows:

```text
business event -> canonical transaction + audit/outbox -> real owner + clock
-> bounded task hierarchy -> acknowledgement-gated contact waterfall -> proof
-> separate verification/sign-off task -> close/reopen rollup -> shared reads
```

Module state machines remain authoritative for domain facts, but no module may
create, retain as canonical, or exempt a private app-visible task authority,
scheduler, owner fallback, overdue calculation, reminder/escalation ladder,
verification queue, or screen-only follow-up pipeline. A feature whose shared adapter is not ready
stays shadowed or blocked; it does not bypass the kernel. Every activated,
app-visible task has a real owner and pinned clock. Failed owner resolution
creates a separate durable exception owned by a real configuration/operations
resolver; the exception is not a substitute owner and the task stays hidden.
Operator and verifier/sign-off work are separate sibling leaves.
Acknowledgement stops contacts, not the work clock, and authorized descendant
reopen propagates upward.

Task clocks and accountability follow
`docs/decisions/task-timing-alerting-violations-and-appeals.md`. Planned,
available, flexible, hard-deadline, clinical-safe, contact, and appeal clocks
must never be collapsed. Vaccination drives and Weighing allow the accepted
two-day carry-forward described there. A breach is not a personal violation:
the kernel must complete attribution, notice, appeal, and independent decision
before a final finding, and it never calculates or changes salary/payroll.

Read and obey
`context/execution/operational-task-kernel-remediation-plan.md` and
`context/execution/defect-prevention-execution-contract.md` for any trigger,
task, Today/My Tasks, owner/duty, clock, reminder, escalation, proof,
verification, hierarchy, or shared operational-read change. Changing this
architecture requires an explicit maintainer decision plus same-change updates
to the kernel architecture, plan, skills, guardrails, and adversarial tests.
Ambiguity is not approval.

The persistent multi-session checkpoint is
`context/execution/operational-kernel-program-state.md`. Coordinators update it
on the sole integration branch after each accepted batch; worker and review
agents never edit it.

## Defect Prevention Closure (Mandatory)

Every bug fix, audit batch, kernel milestone, and feature change follows
`context/execution/defect-prevention-execution-contract.md`. A fix is not closed
until the same current-SHA packet includes the failing-before production-path
regression, the root-cause implementation, the strongest applicable recurrence
control, ordinary affected `make ci-local` wiring, recovery/observability where
needed, docs/skill/anti-pattern sync, and independent counter-review.

When a rule is mechanically detectable, ship its structural guard in the same
batch with adversarial negative fixtures, manifest registration, self-test,
Make target, and standard `run_common` or component-job wiring. A hook or
compatibility-only `JOB=guardrails` path is not enforcement. If a static guard
is weaker than a DB/transaction/contract/runtime control, record that choice and
ship the stronger control; do not write a literal-only false-green grep.

Delegated briefs must name the absolute repo path, fresh base SHA, owned files,
invariants, banned patterns, red/green tests, prevention work, and CI commands.
The coordinator owns shared migrations/contracts, independently verifies every
claim, and keeps closure-pending work open.

## Consolidated Defect-Ledger Closure (Mandatory)

When asked to fix/continue/close the consolidated audit ledger or its bugs, read
both of these before editing:

- `context/repo-audits/last-35-commits-consolidated-bug-ledger.md`
- `context/repo-audits/consolidated-ledger-defect-closure-program.md`

Select one highest-priority unblocked root defect (or an inseparable cluster),
reconstruct the live count from the file, and follow the closure program across
every affected backend, SQL, API, admin-web, Android, architecture, performance,
memory, retry, pagination, security, E2E, observability, and CI/CD layer. Do not
mark a row fixed until its current-SHA proof packet and independent Claude/Codex
counter-review pass. Merge duplicate-root evidence instead of inflating counts.
`CLAUDE.md` and `CODEX.md` remain thin shims to this shared rule.

Read first:

- `context/README.md`
- `SKILLS.md`
- `.agents/skills/goatos-build/SKILL.md`
- `context/architecture/final-architecture.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `docs/mobile/README.md` (Goat OS Android app — one common role-aware app for field operator + leadership; native Kotlin + Compose, `apps/goatos-android/`, app id `sg.mesha.goatos`; read before any mobile work)
- `context/forms/final-forms-sop-engine.md`
- `context/analytics/final-analytics-infra.md`
- `context/agents/ai-agent-context-and-protocols.md`

Android verification is not allowed to stop at a missing inherited `JAVA_HOME`
or unavailable USB phone. Run `make android-doctor`; repo tooling resolves the
pinned JDK/SDK itself. Run `make android-dev-run`; it prefers an authorized
physical phone and otherwise starts/waits for the configured emulator. See
`docs/runbooks/android-dev-device.md`. JDK 21 runs Gradle/AGP; app bytecode
compatibility remains Java/Kotlin 17.

Historical planning/archive docs were removed from the active tree. If a human
explicitly asks for archaeology, use git history or source material rather than
normal build docs.

Purpose:

- Goat OS is the operating system for mixed-species herd-animal identity, health, vaccination, genetics, breeding, workforce, SOP tasks, media proof, verification, devices, commerce interfaces, and analytics.
- Canonical backend/data model/app APIs are built fresh.
- `Goats and Parks.docx` is the base source for herd-animal and park semantics
  across every slice. Any feature touching herd-animal identity, species/breed
  labels, park/shed scope, shed tags, lifecycle/stage, pregnancy/lactation/
  warm-up/fattening, feed safety, weighing, handling, medicine administration,
  park roles, or feed sessions must start from
  `context/source-findings/goats-and-parks-source-findings.md` and must not
  invent conflicting semantics. Feature-specific docs may add stricter
  source-backed rules, but conflicts require an explicit source/owner decision.
- Scope lock: build exactly the user-approved slice, not adjacent product areas
  that the shared platform could theoretically support. Generic foundations are
  allowed only when they serve the approved slice; visible routes, nav, seeded
  cards, mock data, screenshots, and handoff language must not imply another
  vertical is built. For the current admin-web review, the visible slice is Preventive Care (PC)
  Vaccination plus Admin/Data Ops config and vaccination SOP policy.
- Current admin-web frontend scope supersedes the old dashboard/admin product
  surface. For admin-web UI work, read
  `context/frontend/current-admin-web-scope.md`: build the connected Admin
  Config + Preventive Care (PC) Vaccination + vaccination execution context slice (rendered inside
  /vaccination, with shed detail under /vaccination/execution/sheds/{shed_id}),
  with Control Tower summarizing only process
  gaps. Old Operations/Legacy/SOP/counts/import routes are removed from active
  admin-web and must not be rebuilt unless scope is explicitly reopened. Parks is
  NOT a separate vaccination product route or sidebar entry.
- **NON-NEGOTIABLE — the ONLY admin-web UI/UX source of truth is the mock**
  `mock/goatos-dashboard-mock.html`. PORT its layout, structure, table shapes,
  empty states, icon system, spacing, and density. It is **not a color theme**.
  **Never reuse/adapt/recolor old admin UI** (`admin-primitives.tsx`, old
  cyan/slate palette, emoji icons, collapse-to-KPI layouts) — the old admin UI
  is gone; rebuild from scratch to the mock. MANDATORY before any frontend
  `git mesha-push`: `npm --prefix apps/admin-web run check:mock-fidelity` must
  pass + visual compare to the mock.
- Frontend product taxonomy is non-negotiable:
  - **Vertical** = business operating domain/department, such as Preventive Care (PC), Parks,
    Procurement, Admin/Data Ops, Counts, Breeding, Inventory, HR/People, Farmer
    Network. A vertical owns operational context.
  - **Module** = a concrete workflow/product inside a vertical, such as
    Preventive Care (PC) -> Vaccination, Preventive Care (PC) -> future Treatment/Deworming, Procurement -> Source
    Entry, or future Parks modules. Parks is a scope/context dimension for
    vaccination execution, not the owner of a vaccination module.
  - **Command lens** = top-level cross-module screen, not a vertical or module:
    Control Tower, Action Center, Calendar, Protocol Adherence, and Workflows.
  Preventive Care (PC) is a vertical and must not use the syringe/injection icon; the syringe/
  injection icon belongs to the Vaccination module. Counts is a separate
  vertical, so Control Tower must not show raw goat census totals as its own
  KPI. Control Tower is for gaps, adherence, exceptions, escalations, and next
  actions.
- Frontend command-room/authority guardrail: Control Tower, Action Center,
  Calendar, Protocol Adherence, and Workflows are top-level screens only. Config
  is a top-level Admin / Data Ops authority screen only. Do not
  duplicate them under procurement/source-entry, Preventive Care (PC), Parks, or any future
  vertical as routes, redirects, tabs, or nav items. A vertical can feed those
  top-level screens through a selected domain/filter/lens such as
  `?domain=procurement` or `?category=vaccination`, but it must not create
  nested routes like
  `/vaccination/adherence`, `/vaccination/config`,
  `/procurement/source-entry/action-center`, `/procurement/source-entry/control-tower`,
  or any `/parks/vaccination` nested command paths. Vaccination execution
  renders INSIDE /vaccination, never as a separate Parks route.
  **Ratified exception (maintainer decision 2026-07-19): `/feed/config`.** Feed
  authors a ration grid (ration group x shed tag x feed item -> grams per head),
  per-shed factors, the session template, and the per-workflow dispatch clock.
  That is a Feed-owned data model served by `/feed-config/*`, not protocol
  `rule_dsl`, and `/config?category=feed_direction` cannot render it. `/config`
  remains the single generic protocol-rule authority screen; `/feed/config` is
  classified `module-surface`, not `authority-screen`. This exception covers
  Config for Feed ONLY. No command lens (Control Tower, Action Center, Calendar,
  Protocol Adherence, Workflows) is exempt for any vertical, and none may be.
  **Ratified exception (maintainer decision 2026-08-06): `/health/config`.**
  The SECOND and only other entry, same shape and same reasoning. Health authors
  a treatment protocol: per disease x age band, an ordered day/session course of
  medicine + dosage + unit + route, plain actions, and critical handoffs. That is
  a Health-owned data model served by `/health-config/*`, not protocol `rule_dsl`,
  and `/config?category=health` cannot render a per-day medicine grid. `/config`
  remains the single generic protocol-rule authority screen; `/health/config` is
  classified `module-surface`, not `authority-screen`. This exception covers
  Config for Health ONLY. Canonical prose:
  `docs/decisions/health-config-authoring.md`.
  **SOP split (maintainer decision 2026-08-18): the top-level SOP Library
  (`/sops`) is RETIRED.** SOPs are per-module module-surfaces, mirroring the
  `/feed/config` shape: `/vaccination/sops` (Vaccination SOP, under Preventive
  Care), `/counts/sops` (Herd Operations SOP: birth / death / shifting), and
  `/feed/sops` (Feed SOP: distribution / packing / transport). All three render
  the same `sop-library` table contract over `/admin/sops`, scoped by SOP code
  prefix; the full-page SOP builder lives at `<module page>?compose=1`. There is
  no `/sops` route, redirect, or nav leaf any more, and `/config` stays the
  single generic authority screen.
  **SOP split EXTENSION (maintainer decision 2026-08-22): `/milk/sops` (Milk
  SOP: preparation / feeding) and `/weighing/sops` (Weighing SOP: the
  scan-and-submit session) join the same shape** — the same `sop-library`
  contract over `/admin/sops`, scoped by the `milk.` and `weighing.` code
  prefixes, with the library documents seeded by migration
  `000186_sop_library_milk_and_weighing.sql` (`milk.preparation`,
  `milk.feeding`, `weighing.session`; library documents only, never a second
  execution engine — sop_tasks stay vaccination.drive-only per the 000175
  precedent). Any OTHER nested `*/sops` route still needs
  its own recorded maintainer decision — the five routes are named in
  `check-ia-guard.mjs` `MODULE_SURFACE_ROUTE_EXCEPTIONS`.
  **Confirmed LANDED-COST rule (maintainer decision 2026-09-01): a load's Purchase
  value is its LANDED cost — animals PLUS transport PLUS booking, labour, transit
  and transition feed — never the ex-farm animal price.** The formula
  (`procurement/domain.loadPurchaseValue`) was always right; only the animal
  figure was ever imported, because the farm's Procurement DB sheet records cost
  as ONE ROW PER EVENT per load and the importer read the Purchase row and
  concluded no split existed. That understated the eight live loads by ₹2.99L
  (5.8%) and overstated profit by the same. All six cost types count; `animal`
  and `transport` keep their columns and the rest roll into `other`. An UNKNOWN
  kind rolls into `other` rather than being dropped — an unclassified cost is
  still money spent. The list keeps three columns and the itemisation appears on
  CLICK; `procurement_load_cost_lines` is the source and the three columns are a
  roll-up maintained in the same transaction, with a hand edit replacing that
  load's lines so a breakdown can never disagree with the figure beside it.
  The table also carries **Landing price / live kg** (landed cost ÷
  `purchase_weight_kg`), and three charts follow the money chart in the same load
  order: weight per animal in vs out, price per kg landing vs sale, and fattening
  days. **The fattening clock starts on ARRIVAL, not purchase** — the farm warms
  animals up at the source — and is ANIMAL-WEIGHTED across a load's sales.
  On the sale side the maintainer kept the DISPLAYED sold value and took only
  weight from `salesDB_clean`, so `sold_weighed_value` feeds price-per-kg ONLY and
  must never be summed into the sold-value column. `sold_weighed_animals` is the
  denominator that keeps the average honest: load 101 sold 66 animals but only 26
  were weighed, and dividing by 66 reports a shrinking animal that never existed.
  A load that has sold nothing reports ABSENCE, never zero.
  **The AGE clock is separate from the fattening clock and must not be merged**
  (maintainer request 2026-09-01): `fattening_days` starts on ARRIVAL and stops at
  SALE; `days_since_purchase` starts at PURCHASE and runs while the load is open.
  A load past `procurement/domain.LoadAgeAlertDays` (90, strictly greater — "exceeds
  90 days") that STILL HOLDS ANIMALS raises a daily alert to the CXO ALONE; a load
  past 90 days that sold out is history and is deliberately silent, because alerting
  on it forever would train the reader to ignore the alert. Once per day comes from
  the BUSINESS DATE in the idempotency key, never a private scheduler — the
  `FeedLowStockNotifier` pattern, required by the task-kernel lock. The notifier
  consumes the FINISHED load-wise read model so the push and the chart can never
  disagree about which loads are overdue. Canonical prose:
  `docs/decisions/load-landed-cost-and-growth.md`; schema: migration
  `000235_procurement_load_cost_lines.sql`.

  **Ratified exception (maintainer decision 2026-09-01): `/sales/config`.**
  The THIRD Config entry, same shape and same reasoning as the two above, plus a
  second half the others do not have. Sales Config is where every sales fact is
  ENTERED or CHANGED — recording a sale, tagging the animals it is made of,
  buyer and farmer-group leads, market quotes, sold-tag lists, weight checks,
  a deal's payments and status, and a purchased load's landed cost. It authors
  nothing generic and duplicates no lens; it is classified `module-surface`, not
  `authority-screen`, and `/config` remains the single generic protocol-rule
  authority screen.
  **The second half is the lock: `/sales` and `/sales/loads` are READ-ONLY.**
  Their backend page contracts declare NO write control at all — not a disabled
  one — so neither page can render a button, a form or an entry drawer for
  anyone, the CEO included. That is what makes entry exist in exactly one place;
  an entry form living on two screens is a form whose two copies drift. The
  authorities did not merge with the pages: `record_sale`, `record_pipeline`,
  `record_sales_deal_payment` and `update_sales_deal_status` ride `SalesWrite`,
  while `record_load_cost` keeps `LoadCostWrite`, so the sales desk sees the
  cost control disabled with its reason on a page whose other controls are live.
  The page itself is reached on `SalesRead` (the `/health/config` shape): a
  reader opens it and sees each control disabled with a backend reason rather
  than finding the leaf missing. Pinned by
  `TestSalesReadPagesCarryNoWriteControl` (mutation-tested: restoring the write
  compilation on `/sales` turns it red), `TestSalesConfigPageContract` and the
  load-cost gate's sales-director row.
  The machine guard carries the same allowlist — the three Config entries plus
  the five SOP-split routes — in `apps/admin-web/scripts/check-ia-guard.mjs`;
  widening it needs a new recorded maintainer decision here first.
- Config / Protocol Rules is a generic Admin / Data Ops authority screen
  (`/config`) for CEO/COO/superadmin users. It is not owned by Preventive Care (PC) / Vaccination.
  Preventive Care (PC) / Vaccination may link to `/config?category=vaccination`, but the Config UI
  must stay category/schema-driven: changing category changes the form fields and
  `rule_dsl`; do not show vaccination fields for `feed_direction`.

Code navigation (graph-first):

- For code-structure questions (callers, callees, dependencies,
  blast-radius/impact, diff review, architecture, hub/dead-code), query the
  `code-review-graph` MCP tools before broad file scans. Read files for what the
  graph cannot see: constants, config values, HTTP route strings, error text,
  and uncommitted code.
- Route by task shape, not ritual:
  - cold/review/diff: `get_minimal_context_tool` first, then one targeted graph
    query;
  - known symbol: go straight to `query_graph_tool`;
  - keyword/domain lookup: `semantic_search_nodes_tool`, then targeted graph;
  - single file/function read: read the file, then graph only for impact.
- Graph is the fast first pass for traversal; native Grep/Read is the fallback
  for graph blind spots. One graph query replaces many grep/read cycles when the
  question is graph-shaped.
- Framework/library docs routing: for implementation, debugging, dependency
  upgrades, or review that depends on third-party APIs/framework behavior, use
  the local Context7 docs cache before relying on model memory. `make ai-setup`
  and `make ai-doctor` run `tools/agent-docs/ensure-context7.sh`, which fetches
  the Context7 key from Secret Manager when local Mesha `gcloud` auth is
  available and registers Context7 MCP for Codex/Claude when those CLIs exist.
  Query order is: repo code graph and Goat OS docs first; then local framework
  docs under `agent-docs/context7/` or `.agent-docs/context7/`; then live
  Context7 for missing/stale topics. Fetch narrow topic docs only (for example
  "Next.js route handlers caching" or "Room migration testing"); never load
  entire library documentation into the model context.
- Setup is per-machine and agent-enforced on fresh clones: if `make ai-setup`
  has never run on this clone, the committed `ai-setup-guard` hook blocks the
  first real tool call with bootstrap instructions — run `make ai-setup` first,
  then resume the task (see `docs/ai/README.md`). The graph DB (`.code-review-graph/`) and
  Graphify outputs (`graphify-out/graph.json`, reports, cost files, cache) are
  gitignored and regenerated locally. To enable the portable setup, run
  `make ai-setup`; to rebuild local graphs, run `make ai-rebuild`; to verify the
  clone is wired without committed graph artifacts, run `make ai-doctor`.
- Maintainer-local only: the Graphify Mesha wiki/doc/visual graphs
  (`mesha_docs_graph`, `mesha_visual_graph`) are built from sources outside this
  repo and cannot be reproduced here. Use them if already configured; otherwise
  skip and use Grep/Read.
- **Agent tool choice (human)**: before a non-trivial task, read
  `docs/ai/agent-tool-routing.md` — Cursor for admin-web UI and small fixes;
  Claude Code (terminal `claude` in repo root) for contracts, backend engine,
  migrations, and multi-module work. No second IDE required.

Current repos:

- `dashboard/` - current live CEO-style Next.js dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/admin-web/`.
- `vgoats-dashboard/` - current live investor/reduced dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/investor-web-shadow/`.
- `procurement_app/` - reusable React Native camera/upload/team ideas; currently procurement/Firebase coupled.
- `website/` - public MESHA site; business-context reference only, out of Goat OS core.
- `slack-automation-scripts/` - legacy Slack/Sheets/App Script automation.

Organization boundaries:

- Mesha/VGoats, Heva, and Slice are separate businesses and must never be
  mixed in GitHub or Google Cloud operations.
- Goat OS belongs to Mesha/VGoats. Google Cloud work for Goat OS targets the
  `vgoats.com` organization and future `goatos-dev`, `goatos-stg`, and
  `goatos-prod` projects.
- Do not use Heva projects/orgs, Slice projects/orgs, or `hevaplatform` for
  Goat OS work.
- Current active Goat OS agent/tooling and Firebase project is still
  `goatos-stg`, but it backs the current production-facing cleanup path. Do not
  create, update, read, grant IAM on, or store agent/tooling secrets in
  `goatos-dev` unless the user explicitly says `goatos-dev` in the same request.
  For Context7, Gemini/Graphify, Claude/Codex bootstrap, and local agent docs,
  `goatos-stg` is mandatory. Public URLs/app package/release labels must use
  production-facing names, not staging names.
- Do not modify or replace the legacy `goatos-sheets` project while creating
  Goat OS projects.
- Before any cloud/GitHub command that creates, updates, deletes, grants IAM,
  links billing, deploys, or changes configuration, verify and state the active
  account, organization, folder, project, and target repo. If the target is not
  Mesha/VGoats for Goat OS work, stop and correct context first.
- If any Google auth surface expires or cannot refresh non-interactively
  (`gcloud`, ADC, Cloud SQL Auth Proxy, Secret Manager, Google Drive/Docs/
  Sheets, or a Google browser session), do not stop at "token refresh failed"
  when the task requires Google access. Use browser-based reauthentication
  immediately: `gcloud auth login ravi@mesha.sg` for CLI user credentials,
  `gcloud auth application-default login` for ADC, or the relevant browser/
  connector sign-in for Drive/Docs/Sheets. After reauth, re-verify the active
  account, organization, project, and target before any write/deploy/config
  mutation. For Goat OS, the expected account is `ravi@mesha.sg` and the
  expected Google Cloud organization is `vgoats.com`.
- For read-only Google-backed data pulls, Cloud SQL queries, dashboard issue
  CSVs, or any request phrased as "use gcloud/browser login", follow
  `docs/runbooks/google-cloud-environments.md` -> `goatos-dev Read-Only Cloud
  SQL Access` before touching Chrome or dashboard UI. The default source is
  gcloud + Secret Manager + Cloud SQL Auth Proxy + Postgres, not dashboard DOM
  scraping.
- For GitHub operations in this repo, use the Mesha/VGoats repository token
  path: `git mesha-push main` for pushes and the `MESHA_GITHUB_PAT`-backed
  remote URL for direct remote/CI verification. Do not rely on whatever `gh`
  account is active; this workspace may also have Heva and Slice GitHub
  accounts configured, and those must not be used for Goat OS repo authority.
- Git commits from this repo must use a Mesha identity only. Before committing
  or landing, `git config user.email` must end in `@mesha.sg`; Heva, Slice,
  gmail, or personal identities are blocked by `make git-identity-guard` and
  the local CI common gate. The expected maintainer identity is
  `Raviteja <ravi@mesha.sg>`.
- **Staging deployment is Slack-triggered Cloud Build into Cloud Deploy.** Do
  not create or wait for a `main -> stg` pull request, GitHub Actions workflow,
  or direct `stg` branch push as a deployment mechanism. Agents must use the
  `#goatos-stg-deploy` Slack button, which runs Cloud Build trigger
  `goatos-stg-deploy-main` from latest approved `origin/main`; manual scripts
  under `tools/deploy/stg-clouddeploy-*.sh` are break-glass/repair mechanics.
  Never push any local ref, local `stg`, `main`, `HEAD`, agent branch, or
  refspec directly to remote `stg`; the branch is not deployment authority. Run
  `make ai-setup` so the local guard blocks accidental remote `stg` writes. Do
  not bypass it with `--no-verify`.
- Create Goat OS cloud resources under `vgoats.com`, preferably in a `goat-os`
  folder, or directly under the org if folder creation is not available. Do not
  create Goat OS resources inside `system-gsuite` or `apps-script`.

Do:

- Keep architecture facts in `context/`.
- Treat every test or script labeled E2E as a production-path proof, never a
  seeded readback. E2E fixtures may insert only external/input facts required to
  start the scenario (for example tenant, herd animal, location, workforce,
  inventory, or authored configuration). Obligations, batches, completions,
  verification outcomes, SOP tasks/submissions, notifications/escalations,
  cancellations, and Calendar/process-integrity screen output must be produced
  by the same service, API, durable event consumer, sweeper, canonical-read
  query path, or projector used in production. Per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the Calendar,
  process-integrity, and vaccination shed/execution/operations screens are
  served at the current 5k-50k envelope directly from canonical indexed SQL —
  the `calendar_event_projections`, `process_integrity_projection_rows`, and
  `vaccination_shed/execution/operations_projection_rows` projection tables are
  retired, not replaced by a seeded stand-in. E2E for those screens must still
  drive the real canonical-read path end to end; if a screen later earns its
  own projection under that ADR's scale-out ladder, this same production-path
  requirement carries over to that projector. A narrower test that
  intentionally seeds derived state must live with the owning package as an
  integration/read-model test and must not appear in an E2E report.
  `tools/agent-hooks/check-e2e-kernel-integrity.sh` enforces this rule for both
  Claude and Codex and in CI.
- Couple migrations to initial seed setup. If a migration changes tenant/goat/
  RFID/location, HRMS/ownership, founder grants, protocol/SOP/capacity,
  obligation/completion/proof, notification/verification, or app-visible
  projection/read-model tables, update the matching seed command,
  seed/projection test, or seed runbook in the same patch. The
  `seed-migration-guard` target is part of `make guardrails` and blocks
  schema/read-model drift where source rows seed correctly but the live app reads
  empty or missing projection tables. See
  `docs/runbooks/initial-seed-migration-coupling.md`.
- Do not make seed scripts hand-fill every new table. Classify setup tables as
  source/canonical, derived/read-model, static catalog/config, or
  operational/audit/event. Per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the default at
  the current 5k-50k envelope is that a new app-visible surface is served by a
  canonical indexed SQL read — no new projection table, and no closeout wiring,
  for that default case. A derived/read-model table exists only where it
  survives this envelope (the vaccination eligibility rollup and counts
  summaries) or where the ADR's scale-out ladder later adds one for a specific
  measured hot read. Any such surviving or newly-added projection table must
  still be rebuilt from canonical data through `make seed-closeout` /
  `tools/dev/seed-closeout.sh`, and still needs access-pattern indexes,
  freshness/version state, and an explicit partitioning decision at the point
  it is introduced.
- Register projection closeout by app-visible output, not just by executable
  name. If one projector command owns multiple read models, `seed-closeout`
  must pass explicit flags for each output. Any default-false `-project-*` flag
  for a visible read model must appear as `-project-...=true` on the owning
  command invocation in closeout, and the guard must verify the executed
  `tools/dev/seed-closeout.sh --dry-run` output rather than raw shell text. A
  commented, disabled, or uncalled invocation does not count, or the seed can
  claim the projector ran while leaving that table empty.
- Projection-backed operator pages must follow the last-known-good serving
  contract. No first projection, no serving rows, or a requested window outside
  projected coverage may fail closed. A stale/yellow/rebuilding/failed/over-TTL
  projection that still has serving rows covering the request must serve those
  rows with freshness metadata instead of taking the page down. See
  `docs/decisions/high-scale-dashboard-projections.md`.
- Source-backed vaccination seed means the whole executable setup, not goats
  alone: founder grants, HRMS roster, attendance/leave, timetable-backed
  positions, strict shed manager/backup mapping, position duties, published
  `vaccination.matrix` config, trusted vaccination history, generated future
  obligations, generated drive batches, and deterministic closeout. Missing
  HRMS/config is a failed seed, even when goat rows exist. A reseed/import/local
  proof is also failed if it stops after generation and leaves visible-window
  `scheduled`/`due` vaccination obligations unbatched; `tools/dev/seed-closeout.sh`
  must run the obligation sweeper and fail on that condition.
- Every accepted live goat in seed/import/dev data must resolve to a real active
  shed. During the current build phase, missing source placement is completed
  deterministically into an explicit seed-intake park/shed; do not skip the
  animal, leave `shed_id` blank, or fall back to a park/tenant vaccination
  obligation. Goat vaccination obligations are **shed-scoped only**; park is
  the drive execution/grouping scope. Required guards:
  `make goat-shed-scope-guard`; post-seed DB proof:
  `make goat-shed-integrity-db-proof` or `tools/dev/seed-closeout.sh`.
## The Word On Screen Is PEN, Never SHED (maintainer lock, 2026-09-02)

Every user-visible string says **pen**. The word *shed* appears on no screen a person reads --
page and table titles, column labels, filters, chips, KPIs, empty states, notes, tooltips,
drawer copy, user-facing error text, CSV export headers and download filenames.

This is a VOCABULARY decision and nothing else. Behaviour, grain, schema and API contracts do
not change because of it. A change made in the name of this rule that alters what the software
DOES is wrong.

**What stays `shed`, deliberately and permanently:** column KEYS (`shed`, `shed_tag`), copy KEYS
(`kpi.sheds.label`), table/section ids (`shed-weights`), route paths (`/vaccination/sheds`),
every database table/column/enum (`subject_type='shed'`, `position_code='shed_manager'`), and
every Go/TS identifier and JSON wire field (`shed_id`, `shed_name`). Do NOT rename these to
match the label. The mismatch between the stored word and the shown word is the design.

**Copy comes from FOUR places and a new screen must get all four right** -- missing one is how
the first pass shipped eleven tables still saying "Shed" on pages whose own copy said pen:
(1) the page-contract copy maps in `adminui/app/service.go`; (2) `humanLabel()` in the same
file, which DERIVES column labels from the column key rather than reading the copy map;
(3) sentences composed in a producing module's Go or SQL (`weighing/domain.CorrectedSubjectLabel`
"Whole pen", counts shifting "Pen move", the verification batch label, the calendar subtitle);
(4) seeded rows in the database, which need a forward migration. Plus position TITLES,
prettified from `position_code` in `workforce/app.formatPositionCode`.

**Three places the two words meant different things, and substitution was WRONG.** Feed Config's
multiplier is a **FEED factor**, not a pen factor: `feed_shed_factors` has no partition column,
so one row scales every pen in the building and "pen factor" would be false. Feed TRANSPORT is
shed-grain on purpose (one trip per building, migration 000152), so its copy says "physical
location" -- writing "pen" states the opposite of the rule it explains. Explainers that existed
only to relate the two words ("an undivided shed is its single pen") are circular with one word
and are DELETED, not reworded. General form: when a sentence needs both words, name the thing
accurately without either noun, or drop the clause -- never substitute.

**The trap that can silently corrupt data:** `pen` ALREADY MEANT PARTITION in the animal bulk
importer (`identity/app.normalizeHeader`: `pen`, `pen_label` -> `partition_label`). The CSV
template header is built from the option LABELS and parsed by header NAME, so labelling the
location column "Pen" would file a pen name into `partition_label` on every row -- no error, a
wrong location on every animal. It is labelled **"Pen name"**; bare `pen` keeps its meaning.
Generalise: a template header label IS a parser input. Add the new name as an ALIAS and keep
the old one, or every sheet already saved stops importing.

Enforced by `adminui/app.TestBootstrapContractSaysPenNeverShed` (walks the whole served
bootstrap JSON; mutation-tested), `TestColumnLabelsSpeakPenWhileTheKeysStayShed`,
`identity/app.TestImportHeaderAliasesSurviveThePenRename`,
`workforce/app.TestPositionTitlesSayPenWhileTheCodesStayShed`, and admin-web
`features/counts/pen-import-headers.test.mjs`. NOT yet done: the Android app's ~471 own
hardcoded "shed" strings, tracked separately. Canonical prose:
`docs/decisions/pen-not-shed-vocabulary.md`.

## Operational Location and Partition Convention (maintainer lock, 2026-08-06; clarified 2026-08-16)

**Read `docs/decisions/partition-is-operational-shed.md` FIRST. It outranks the
storage wording below.** In product terms `Castro 1` and `Castro 2` ARE sheds —
separate buildings, with animals physically in them. There is no operator-facing
"parent shed plus partition". Everything in this section describes how those
sheds are currently STORED while the operational-location migration is in
progress; it is not a claim about the farm.

Every goat's ground location is stored as: `park + physical_shed + optional
partition_label`. That triple is one shed. `shed_id` alone never names it.

**The convention is LOCKED by evidence from THREE independent sources (master registry, live BigQuery, legacy production code), with FOUR worked wrong-examples from production bugs. This section tightens the rule with those examples and a guard.**

### Rule 1: Normalize Partition Labels at Seed/Import

Sheds whose names share a base (`Castro 1`, `Godel 1 - Part 3`) are STORED as
`shed_name + partition_label`. This is a storage layout, not a statement that the
base name is a building:
  - `Castro 1`, `Castro 2`, `Castro 3` → three sheds, stored under one `locations`
    row `Castro` with labels `1`, `2`, `3`. `Castro` is grouping metadata; it is
    not a shed anyone works in.
  - `Godel 1 - Part 3` → the shed `Godel 1 - Part 3`, stored as `Godel 1` + `Part 3`

Undivided sheds (numeric-suffix names that are NOT subdivided, like `Ho Chi Minh 1`, `Yashoda`) → stored with NULL / '' / 'whole' partition. The `1` in the shed name is NOT a partition.

**NEVER seed raw partition strings as new `locations` rows.** The `locations`
table is the single source of truth for which sheds exist; inventing a row from a
label string duplicates a shed that is already stored. This is a rule about how
to WRITE `locations`, not a claim that the labelled sheds are less real than the
base name.

### Rule 2: Storage vs. Display Are Different (Maintainer 2026-08-05, clarified 2026-08-16)

Storage keeps the sheds `Castro 1` and `Castro 2` as `Castro + label 1/2`. Display ALWAYS puts the two halves back together, because the label half carries the shed's actual name:
- No label (NULL / '' / 'whole') → `Yashoda`, `Ho Chi Minh 1`: undivided sheds whose trailing digit is part of the name (Rule 1). Never `Yashoda - 2`, and never a bare base name for a shed that HAS a label
- Bare numeric partition → `Castro 1`, `Gandhi 2`, `Gandhi 3` (space separator; the farm's actual physical shed names as painted on buildings)
- Worded/prefixed partition → `Godel 1 - Part 3`, `Mandela 1 - Part 1` (dash separator; visual boundary since 75% of live shed names end in digits)

**Separator rule (2026-08-16 clarification):** Numeric partitions use SPACE because the farm's sheds ARE NAMED `Castro 1`, `Gandhi 2`, etc. — that is the real name painted on the building, not a display formatting choice. Worded labels use " - " (dash) for visual boundary: `Godel 1 - Part 3` is unambiguous from the shed name.

**NEVER render.** Where a forbidden string is shown it is paired with the correct
one; the numeric-dash rule is stated in words instead, so the wrong form is not
sitting on the page as something to copy:

- `WRONG: Yashoda whole` -> `RIGHT: Yashoda` — `'whole'` is a matching key, never user copy
- Numeric pens must not use dash separators: write `Castro 1`. A dash-separated
  numeric pen name contradicts the farm's physical naming and is never rendered.
- `WRONG: Godel 1 1` -> `RIGHT: Godel 1 - Part 1` — naive space-numeric join, truncated
- `WRONG: Godel 1` -> `RIGHT: Godel 1 - Part 3` — the base name alone when the shed has a label; both halves always render together

**Both layers must always be read together.** The normalization is a storage rule; the partition is a product rule (and the separator reflects the farm's real-world naming).

### Rule 3: Carry Partition in All Location-Bearing Responses

`shed_id` alone is NOT the ground location when a partition exists. Every location-bearing response struct MUST include:
- `shed_id` (UUID, the canonical key)
- `shed_name` (display name of the physical shed)
- `partition_label` (text or NULL)
- `operational_location_display` (backend-composed: `DisplayName(shed_name, partition_label)`)

Tables and response structs that **must** carry partition: `verification_items`, `weighing_campaign_sheds`, `health_cases`, shifting source/destination, counting/census rows, passport/herd register, vaccination detail.

### Rule 4: Query by `shed_id + park`, Not by Name

Group and key by `shed_id` (UUID) + park, NEVER by shed NAME. Names repeat across parks (two `Castro`, two `Gandhi`, two `Yashoda`). Name-keyed grouping silently merges parks — OL-2 worked example: six duplicate `Godel 1` rows in a shed selector because six partitions got grouped as one "Godel 1" row instead of six disjoint rows.

### Rule 5a: Use the Canonical FETCH, Not Just the Canonical Display

Composing the display has had a shared helper for a while. FETCHING the parts did not, so
every site wrote its own `SELECT` -- and the schema offers two columns that look
interchangeable and are not:

```
partition_label   'Part 3'   HUMAN label -- the only one that may be displayed
normalized_label  '3'        scrubbed MATCHING KEY -- joins only, never a screen
```

Selecting the wrong one compiles, passes review, and renders `Mandela 2 - 3` to an operator.
That defect shipped, was fixed, and was then REINTRODUCED hours later by a change in another
module that hand-wrote the same query. Centralising the fetch makes the mistake unavailable
rather than merely discouraged.

```
backend/internal/platform/oploc/resolve.go
  ShedScopedLocationSQL   the ONE query resolving a shed id -> (shed name, partition label)
  ResolveShedLocation()   scans it into an OperationalLocation
```

It bakes in the three rules that keep being re-derived wrong: `partition_label` never
`normalized_label`; `'whole'` filtered so it cannot reach a caller; and agree-or-go-bare via
`HAVING count(*) = 1` rather than `ORDER BY ... LIMIT 1`, which fabricates an answer that
silently flips as partitions change. An unresolvable shed returns the zero value with a nil
error, so callers DEGRADE to their location-less label instead of rendering a raw uuid or a
dangling separator.

Do not inline a partition `SELECT`. If a set-based read model genuinely cannot call into Go,
mirror `oploc.Display()` exactly and name it in a comment as the contract being mirrored.

### Rule 5: Use Canonical Composition, Never Hand-Roll

Shared location helpers exist in ONE place per language; use them instead of re-deriving:
- Go: `backend/internal/platform/oploc` → `DisplayName(shed, partition)`
- Admin-web: `apps/admin-web/lib/operational-location.ts`
- Android: `core/core-ui/.../PartitionLabel.kt`

Hand-rolled copies drift. OL-3 worked example: weighing screens rendered `Godel 1 1` (truncated partition name + shed name via naive join). OL-7 worked example: six SQL paths each composed the display differently (`Godel 1 - Part 3` vs. `Godel 1-Part 3` vs. `GODEL 1 - PART 3` vs. `Godel 1 Part 3`), breaking filtering and cross-screen navigation.

### Rule 6: New Tables Must Declare `partition_label`

Any table that records location must include a `partition_label` column (nullable for undivided sheds). OL-4/5/6 found examples:
- `verification_items` — missing partition (cannot tell which `Godel 1` partition a proof applies to)
- `weighing_campaign_sheds` — missing partition (ambiguous shed assignment)
- `health_cases` — missing partition (partition-scoped epidemiology is impossible)

### Worked Wrong Examples (All Production Bugs, 2026-08-06)

| Bug | Code | Impact | Fix |
|-----|------|--------|-----|
| **OL-1: Name-Keying Merge** | `groupBy { it.shedName }` collapses two `Castro` sheds across parks (different `shed_id`, same name) | 324 CPT adults + 89 Mandela adults both landed on one `Castro` selector row; operator selection was ambiguous | Use `groupBy { it.parkId to it.shedId }` |
| **OL-3: Naive Join Truncates** | `'Godel 1' + ' ' + 'Part 3'` → `'Godel 1 Part 3'` → truncated to `'Godel 1 1'` on screens | Weighing board unreadable; operators cannot identify partition | Use canonical `DisplayName(shed, partition)` |
| **OL-7: SQL Drift (6 paths)** | Feed query: `'Godel 1-Part 3'` / Dashboard: `'GODEL 1 - PART 3'` / Herd: `'Godel 1 Part 3'` | Same animal rendered differently on each screen; filtering broken | Audit all locations, use materialized `operational_location_display` or canonical helper |
| **OL-4: No Partition in New Tables** | `verification_items` lacks `partition_label` | Verifier cannot distinguish which `Godel 1` partition a proof is from; metrics aggregated at shed-level only | Add `partition_label` + compose in API responses |
| **OL-2: Duplicate Partition Rows in UI** | Six partitions of `Godel 1` rendered as six separate `Godel 1` rows in a shed selector instead of one shed with six partitions | Operator picker showed the same shed name six times with no way to tell partitions apart | Group by `shed_id` first, list partitions under it |

**All of these bugs came from hand-rolling composition or grouping by shed name.** The convention makes them impossible.

### Ten Defect Classes From Session 2026-08-07 (MUST-ENCODE)

Session 2026-08-07 found ~15 live defects, ALL from ONE class: partition/location handling failures across the ~5-handoff chain (SQL → Go → wire DTO → OpenAPI → client render). These ten rules encode the failures so the chain cannot break silently again:

1. **`normalized_label` is a MATCHING KEY, never display.** `partition_label` = `Part 3` (human), `normalized_label` = `3` (scrubbed, for joins). Six review rounds passed rendering `Mandela 2 - 3` (shed + key instead of shed + label) because reviewers checked field-carrying, never field-VALUE correctness.

2. **Location crosses ~5 handoffs; dropping it at ANY ONE shows bare shed name.** Real instances: domain correct + wire DTO dropped (verifier queue, submit header), wire correct + renderer ignored (Android verify, 18 admin-web sites), SQL correct + Go struct never declared (calendar chips). A gap anywhere in the chain renders the partition missing end-to-end.

3. **Never add a struct field without wiring it end-to-end.** FOUR instances: field DECLARED never populated (twice), schema declared what Go never emitted (twice), wire name renamed on one side only. A field nothing fills reads as done — worse than omitting it.

4. **Scan-count discipline:** adding a struct field without adding the SQL column is a RUNTIME failure (`number of field descriptions must equal number of destinations`). Adding a partition column and renaming a CTE column broke a query so badly it could not even EXPLAIN. If a downstream CTE groups, orders, scans, or renders a column, every upstream `SELECT s.*`/`SELECT *` carrier must explicitly project that column in the same patch.

5. **`jsonb_array_elements(x)::text` is NOT `jsonb_array_elements_text(x)`.** The first leaves JSON quoting and turns JSON null into the 4-character string `"null"` (non-empty, passes all "has partition?" checks) — fabricating a partition on a shed with none.

6. **Agree-or-go-bare:** compose a partition ONLY when every animal in scope resolves to the SAME real (non-'whole') partition; spanning several or none renders bare shed name. Never invent, never take `rows[0]`.

7. **Never key or group by shed NAME.** Names repeat across parks (Castro, Gandhi, Yashoda appear twice each). Name-keying merges COUNTS, not just labels.

8. **Parallel arrays must be built from the same grain.** One array `DISTINCT`, its partner not, silently shifts every index and pairs the wrong partition with the wrong shed.

9. **Tests must assert OUTPUT STRING against DB round-trip,** not field presence and not pure-Go formatter unit tests. Both weaker forms passed while real output was wrong.

10. **Fixes that regress the suite get REVERTED, not patched under pressure.** Two fixes this session regressed cross-surface parity tests and were reverted; record that as the expected response — never hold a breaking "fix" waiting for a second pass.

### "Active Shed" Means Active Location

The product concept **"active shed"** means **active operational location** (partition if subdivided, shed if not), not "physical building holding ≥1 live animal after collapsing partitions". Using the old definition produced parent-only dropdowns that forced operators to guess.

### Partitions with Zero Animals Still Exist

A partition holding ZERO animals still EXISTS (e.g., CBE `Yashoda 5` is real and empty). A partition catalog derived only from per-goat tables (`goat_shed_partitions`, PK `tenant_id, goat_id`) hides empty partitions and makes them unreachable as shifting destinations. Use the `locations` table as the partition catalog until a real `shed_partitions` table is built.

For write pickers such as Herd Register, shifting destinations, feed/vaccination
execution destinations, and weighing task setup, never derive selectable
operational locations from census/count facets. Facets answer "where animals
currently are"; write pickers answer "where animals/tasks are allowed to be".
Use the partition catalog (`shed_partitions` or the feed-config pens API that
exposes it), and fail closed if that catalog is unavailable.

### Machine Enforcement

`make operational-location-guard` (`tools/agent-hooks/check-operational-location.mjs`,
part of `make guardrails` and `make ci-local`) is a STATIC pattern scan, not a
runtime/seed-value checker. It checks:
1. No `GROUP BY` / `SELECT DISTINCT` on `locations.name` without `shed_id` + park co-grouping (`shed-name-keying`)
2. Hand-rolled display composition instead of the canonical helper — SQL `CASE`
   statements (`sql-display-drift`), Go string concatenation
   (`go-display-drift`), TypeScript (`ts-display-drift`), and Kotlin
   (`kt-display-drift`) must route through `oploc.Display()` /
   `operational-location.ts` / `PartitionLabel.kt` rather than re-deriving the
   string. **This checks composition-pattern shape, not runtime seed-value
   equality** — it does not execute a query or compare against known seed rows,
   so a hand-written helper that happens to match `DisplayName()` byte-for-byte
   on today's seeds but drifts on a future one is out of its reach.
3. New location-bearing tables declare `partition_label` column (`missing-partition-column`)
4. OpenAPI response schemas that identify a shed declare partition/display
   context alongside it (not name-only, and one hop into a `$ref`'d shed
   type) — a static schema-shape check. It does **not** verify that the Go
   struct or the actual wire emission populates those fields; that is
   item 4 in the Partition Change Verification Checklist below, done by
   hand.
5. `whole-leak`, `alias-locations`, `location-type-as-partition`, `counts-grain`,
   and `shifting-contract` — see the check list in the guard's own header
   comment for the full set and each check's rationale.
6. Herd Register partition picker source — the Register drawer must use catalog
   partitions, not count/census facets, so empty partitions remain reachable.
7. Weighing alias/idempotency invariants and staging deploy failure handling for
   the 2026-08-10 staging regression class.

**Not checked by this guard:** `jsonb_array_elements(x)::text` vs
`jsonb_array_elements_text(x)` (OL-5, the JSON-quoting/`"null"`-string defect
class in the partition-catalog session) has no static check in this file today
— it is caught only by code review and the `Partition Change Verification
Checklist` below. Do not assume `make operational-location-guard` would catch
a reintroduction of that defect.

Known blind spots: hardcoded string literals, runtime-composed strings in application code, reflective queries. Code review and the subagent brief catch those cases.

### Partition Change Verification Checklist

Before committing a change that adds, modifies, or displays a partition:

1. **SQL layer (backend/migrations/postgres):** 
   - [ ] New location-bearing table includes `partition_label` column (nullable for undivided sheds)
   - [ ] If populating from existing data, verify both the source query and the target column read the same grain (test on real seed data)
   - [ ] EXPLAIN on the updated query with ~500k row bounds shows no Seq Scan on large tables
   - [ ] If using `jsonb_array_elements` on label arrays, use `jsonb_array_elements_text(x)` (never the `::text` cast)

2. **Go domain/wire layer (backend/internal):**
   - [ ] Struct in `internal/**` domain declares `partition_label` (text pointer or string) + `operational_location_display` (string)
   - [ ] All writers populate both fields (scan each constructor/builder/adapter)
   - [ ] Use only `platform/oploc.DisplayName()` to compose the display string; never hand-roll

3. **OpenAPI contract (contracts/openapi/app-api.yaml):**
   - [ ] Response schema declares `shed_name`, `partition_label`, `operational_location_display` (mandatory for location-bearing rows)
   - [ ] Request schema (if location is input) declares the expected input shape (e.g., `shed_id` alone or `shed_id + partition_label`)
   - [ ] Compare schema and Go struct field-by-field; they must match exactly

4. **Client render (admin-web / Android):**
   - [ ] Generated TypeScript/Kotlin client receives the backend-composed `operational_location_display`
   - [ ] Renderer uses that string, never hand-rolls location composition
   - [ ] For partition pickers: group by `shed_id` + `park`, never by `shed_name`
   - [ ] Visual proof: screenshot showing correct `Godel 1 - Part 3` format (dashed, both halves), not `Godel 1 1` (truncated) or bare `Godel 1` (missing partition)

5. **Test closure (must run before push):**
   - [ ] Unit test on the Go `DisplayName()` helper or formatter covers all known locations (subdivided + undivided)
   - [ ] Integration/E2E test asserts the OUTPUT STRING on a DB round-trip (not field presence alone)
   - [ ] If a partition picker or grouping changed, confirm cross-surface agreement on counts/labels (Calendar vs. Vaccination Board vs. Herd Register)
   - [ ] Run `make operational-location-guard` — must pass

### Five Hard Rules From Session 2026-08-07 (MUST-ENCODE)

These rules cost real bugs today. Each one makes a class of defect impossible. Encode them in every subagent brief and code review:

#### Rule 1: An Undivided Shed Whose Name Ends in a Number Is Never Split

`Yashoda 2` is a SHED NAME, whole. It renders `Yashoda 2`, never `Yashoda - 2`. Same for `Ho Chi Minh 1`. The trailing number is part of the name, not a partition. Contrast with a genuinely partitioned shed: `Mandela 1` + `Part 2` renders `Mandela 1 - Part 2`.

**Defect discovered:** A test fixture fed `operationalLocationLabel("Yashoda", "2")` and its expectation was "corrected" to `Yashoda - 2`. The formatter was right for those inputs; the FIXTURE was wrong, and it taught every reader that `Yashoda - 2` is a real label. **Rule: a fixture that asserts a shape the farm does not have is a defect even when the assertion passes.** Never hand-wave away green tests on wrong data shapes.

**Verification:** Grep for every shed name in `backend/migrations/postgres/` backfill scripts and seed code. Match against the master registry (`wiki/Sheds DB.xlsx`). Names with trailing numbers must be checked: if they appear in `goat_shed_partitions` or `shed_partitions` with a partition suffix (e.g., `Yashoda` + partition `2`), they ARE split; if they appear ONLY in `locations` with NULL partition, they are NOT split.

```bash
# Grep evidence: check seed code for undivided shed names
grep -n "Yashoda\|Ho Chi Minh" backend/cmd/seed-*/main.go
# Should show: only whole sheds, no partition assignments
```

#### Rule 2: A Required Contract Field Must Be Populated on Every Construction Path, in the Same Change

Marking a field `required` in OpenAPI while the Go struct lacks it, or has it and never fills it, ships a contract the client cannot rely on. This happened EIGHT times on this branch.

**Defect discovered:** `operational_location_display` was marked required on `WeighingShedVideos` in OpenAPI while the serving struct had neither field nor composition logic. Clients faithfully rendered null/absent.

**The checklist (mandatory):** SQL column → scan destination → Go struct field → populated at every construction site → wire DTO → OpenAPI → generated client → a renderer that actually reads it. A gap at ANY hop renders bare location end to end.

**Verification:** For every location-bearing response field added:
1. Grep the SQL schema for the column
2. Grep the repository's SELECT clauses for the column in the same query
3. Grep the Go struct for the corresponding field
4. Grep the adapter/builder for an assignment to that field
5. Grep OpenAPI for the declared response field
6. Run `npm run client:generate` (admin-web) or `make build-android` (mobile) and confirm the generated client includes the field

If ANY step is missing, the field is a contract lie.

```bash
# Grep evidence: every handoff from SQL to OpenAPI
grep -n "partition_label\|operational_location_display" backend/internal/*/adapters/postgres/repository.go
grep -n "partition_label\|operational_location_display" backend/internal/*/domain/types.go
grep -n "PartitionLabel\|OperationalLocationDisplay" contracts/openapi/app-api.yaml
```

#### Rule 3: Scaffolded Is Not Wired

A migration, a domain field, a decoder helper, an OpenAPI entry and two client DTOs can all exist while the repository and handler touch none of them. **The verification partition feature sat in exactly that state; its composite-key decoder was called only by its own unit test.** Field-presence tests and pure-formatter unit tests both passed while real output was wrong.

**Defect discovered:** A partition feature added SQL migration (000125), domain field (`PartitionLabel`), decoder helper (`parsePartitionLabel`), OpenAPI schema (`PartitionLabel`), and client DTOs — yet the serving handler never called the decoder, never populated the field, and real API responses carried null/missing partition.

**Rule: a feature is not done until a test asserts the OUTPUT STRING on a real round trip.** Field-presence tests and pure-formatter unit tests both pass for scaffolding. Proof requires:
1. Insert a test shed with partition into the test DB (e.g., `Godel 1 - Part 3`)
2. Call the API/screen that READS that shed
3. Assert the RETURNED STRING exactly matches the database round-trip (e.g., `operational_location_display = 'Godel 1 - Part 3'`)

```bash
# Grep evidence: verify the handler calls the decoder/resolver
grep -A 20 "func.*weighing.*List" backend/internal/weighing/adapters/postgres/repository.go | grep -i partition
# Should show: a call to oploc.ResolveShed or direct SelectPartition in the query
```

#### Rule 4: Verify Data Against the Live Database Before Writing a Repair

~500 lines of guarded repair SQL, a runbook and a decision process were written against a mistaken reading of STG inferred from code and a stale audit. A single read-only check showed every repair class returns ZERO rows.

**Defect discovered:** Repair scripts were generated to handle hypothetical `Mandela 1` partition-catalog orphans that never existed in STG. The database read showed 10 partitions correctly cataloged, no orphans, no breakage.

**Rule: query the live database FIRST; a repair script written from inferred shape is a destructive operation aimed at a problem that may not exist.**

**Verification before writing ANY repair:**
1. Run a read-only verification query against STG via the runbook (`docs/runbooks/google-cloud-environments.md`)
2. Confirm the defect class exists and quantify affected rows
3. Verify the repair will not delete correct data (dry-run with `RETURNING` to see target rows)
4. Only after proof of existence, write the repair

```bash
# Grep evidence: verification queries must run before repair authoring
# Example: count orphan partition-catalog rows BEFORE repair authoring
SELECT COUNT(*) FROM shed_partitions sp
WHERE NOT EXISTS (
  SELECT 1 FROM locations l
  WHERE l.tenant_id = sp.tenant_id
  AND l.shed_id = sp.shed_id
);
# MUST return > 0 before any repair is written
```

#### Rule 5: Confirm the Repo Path Before Editing

This workspace has multiple checkouts (`<another checkout>`, `<this repo>`, review worktrees). An agent did a full task in the wrong one and the work was unusable; it also reported that files "don't exist" when it was simply in the wrong tree.

**Defect discovered:** Subagent reported `docs/decisions/operational-location-convention.md` missing after editing `<another checkout>/docs/decisions/operational-location-convention.md` instead of `<this repo>/docs/decisions/operational-location-convention.md`.

**Rule for delegated work:** state the absolute repo path in the brief and confirm with `git rev-parse --show-toplevel` before the first edit. "File not found" means check the tree before concluding the code is missing.

```bash
# Grep evidence: verify every agent logs its repo root
# Expected in every agent session start:
git rev-parse --show-toplevel  # Must print THIS repo root, not another checkout
```

### Settled Model for Partition Documentation

**State this plainly wherever partitions are described**, because it was misread twice today:

- A shed is `Mandela 1` (physical building name)
- Its pens are partitions `Part 1`, `Part 2`, etc., stored as `partition_label` under that shed
- **Verified read-only against live STG on 2026-08-07**
- Partitions are NOT separate shed rows and must NOT be restructured into them
- The old `Mandela 1 - Part N` location rows are INACTIVE aliases only (legacy data shape)

### Subagent Brief Rule

**If you delegate location-bearing work to a subagent, include this rule in the brief.** Quote the worked examples, the five rules above, and name what makes the work location-bearing ("updates shed-scoped queries" or "adds a location picker"). An agent never told the boundary will cross it reasonably.

### Related Documentation

- Full decision: `docs/decisions/operational-location-convention.md` → evidence table, all worked examples, closure criteria
- Defect ledger: `context/repo-audits/operational-location-do-not-reopen-ledger.md` → all bugs found 2026-08-06, closure status
- Adult animals with no accepted history for a vaccine automatically join that
  vaccine's normal adult drive. Do not require or render a separate manual
  campaign; `repeat` versus `initial/catch-up` is per-animal dose status inside
  the same logical drive. Overlapping repeat safe windows must coalesce on their
  latest shared ready date, and that date applies to both history-backed and
  blank-history obligations. A physical shed at or below the full per-operator cap
  is indivisible and must carry to the next operator-day when residual capacity
  is insufficient. Verification/director closure timestamps never replace the
  operator submission's `administered_at` medical anchor.
- Vaccination drive batching is park-level, animal-first, and safe-window-bound.
  Shed count is never a merge constraint; it is display/proof detail. A 1-2
  animal drive is valid only after proving no compatible same-park animal group
  can join between that group's due/ready date and binding safe-until date.
  Normal per-drive animal caps are soft on the last safe day, but the per-animal
  shot cap remains hard. Reseed/local proof must run
  `make vaccination-drive-clubbing-db-proof` after sweeper closeout; without it,
  Calendar/Full Schedule screenshots are not batching evidence.
- Vaccination source dates are base history anchors, not open due work. A seed
  or reseed must preserve trusted past dates as accepted history, suppress any
  seed-created open work on or before the backend business date, and let the
  vaccination kernel generate only future obligations from that base. Seed code
  must not hand-roll kid/adult path selection; it must use the live vaccination
  schedule-path helper/config so stale source tags such as `origin=birth` or
  `K1/K2` cannot force old kid-course work. Raw source vaccination cells also
  must not be pre-mapped as kid-course history to prove their own schedule path:
  classify first from independent evidence, then persist the source date as the
  selected rule family's history anchor. The concrete checklist lives in
  `docs/runbooks/vaccination-seed-source-date-contract.md`.
- After any destructive seed, bulk import, fixture reset, or large canonical
  backfill, refresh Postgres planner statistics for the touched canonical
  tables before projector recompute or latency gates. The normal source seed
  must `ANALYZE` the freshly loaded location, HRMS, goat, protocol, obligation,
  event, and vaccination-completion tables after commit and before read-model
  projection. This prevents projection timeouts caused by stale empty-table
  planner estimates.
- Do not start local, staging, or production app code against a database that is
  behind that build's migrations. Apply migrations first, seed only canonical
  source truth second, run deterministic closeout/projectors third, then start
  API/admin/workers or mark the environment green.
- Do not serve the normal local API/admin-web from temporary worktrees under
  `/tmp`, `/private/tmp`, or `/var/folders`. Local stack wrappers must fail by
  default there so the browser cannot silently exercise a disposable checkout
  while the canonical repo is stale or dirty. Use
  `GOATOS_ALLOW_TEMP_WORKTREE_LOCAL_STACK=1` only for explicit throwaway
  experiments, never for handoff.
- Normal local laptop runtime must resolve exactly one Goat OS app database for
  API, admin-web, and mobile. Use the single detected `goatos-local-current`
  Docker DB or the `127.0.0.1:5433/goatos` fallback; if multiple Goat OS app
  Postgres containers are running, local launchers must fail instead of
  guessing. E2E/proof/load scripts must fail closed unless
  `GOATOS_E2E_DATABASE_URL` or `DATABASE_URL` is explicitly passed. Read-only
  E2E checks may target the normal `5433` app DB, but mutating proof/load
  scripts that create goats/proofs, replay outbox, insert history, or run
  migrations must always refuse `5433`. There is no override for mutating the
  normal app DB from E2E. Destructive/load tests must use an isolated DB with
  its own seed/cleanup, such as the explicit local GCP-kernel stack on `55432`;
  that stack must never become the default laptop runtime DB.
- Deploy `goatos-stg` through the Slack button backed by Cloud Build and Cloud
  Deploy. Build systems may create images and Cloud Deploy releases, but Cloud
  Run service/job mutations for staging belong to
  `deploy/clouddeploy/stg/clouddeploy.yaml` and
  `tools/deploy/stg-clouddeploy-task.sh`. Direct `gcloud run services update`,
  `gcloud run jobs update`, or manual migration execution is break-glass only
  and must be followed by a Cloud Deploy release from the same commit; see
  `docs/runbooks/cloud-deploy-staging.md`.
- Never amend an already-applied Postgres migration or baseline to repair a
  shared environment. Ship the next numbered forward migration, because STG
  records migration checksums and will fail before pending repairs if an earlier
  applied file changed. Before declaring any migration-backed STG fix complete,
  verify `public.goatos_schema_migrations`, the live table/column/data contract,
  and `/readyz`; see `docs/runbooks/stg-deploy.md`.
- Use `.agents/skills/goatos-build/SKILL.md` as the active agent reference map.
- Use ports/adapters for replaceable vendors and tools.
- Use OpenAPI REST/JSON for web/mobile app APIs.
- Use JSON Schema for form DSL and event payload contracts.
- Use protobuf/gRPC only behind the app API boundary when a real internal workload needs it.
- Keep frontend/mobile data access behind generated clients and app APIs.
- Golden frontend rule for Codex, Claude, and every developer using this repo:
  admin-web/goatos-android (mobile) are renderers, not product-truth owners. Backend
  OpenAPI/app contracts must own visible navigation, route availability, page
  titles, section/table labels, filter/sort/page-size semantics, chips/tabs,
  row-click params, drawer/action labels, empty/error copy, disabled reasons,
  and summary-vs-detail field sets. Frontend may own layout, CSS, responsive
  density, icon-token rendering, focus/hover state, and local open/closed or
  selected-row state only. If a visible label/control/action is hardcoded in a
  frontend page, either move it into a backend contract plus OpenAPI/generated
  client, or document the temporary exception in `context/frontend/` before
  shipping.
  Backend-owned does not mean backend-code hardcoded live data: tenant/location/
  person/goat/shed/vendor/operator IDs, park codes/names, capacities, role/actor
  scope, permissions, and business-managed dropdown vocabularies must come from
  Postgres/source-backed config and be compiled into the contract by backend.
  Stable UI text that rarely changes (nav/page titles, table/filter labels,
  chips/tabs, empty/error copy, disabled reasons) belongs in the backend
  bootstrap contract; when it needs runtime governance, store it as tenant-scoped
  `admin_ui_config_entries` and compile it into `/admin-web/bootstrap`.
  These entries may not relabel live/module-DB-owned options such as parks,
  sheds, breeds, SOP labels, feed items, or role/grant scopes, and may not
  override semantic option metadata such as source-system publishability.
  Frontend must not ship local defaults that later get replaced by async config.
  Backend code may hold only product contract shape, compile mapping, and
  intentional default skeletons for missing optional UI config rows; live/domain
  values stay in canonical module tables.
- User-facing copy firewall for mobile and frontend: CEO, director, and operator
  screens must use farm/product language only. Never show internal implementation,
  debug, test, or roadmap wording in visible UI copy, screenshots, empty states,
  toasts/snackbars, banners, cards, chips, buttons, bottom sheets, drawers, or
  alerts. Banned visible words/patterns include `V1`, `V2`, `debug`, `mock`,
  `fixture`, `Paparazzi`, `Room`, `outbox`, `idempotency`, `groupKey`,
  `payload`, `backend`, `frontend`, `API`, `route`, `PRD`, `TRD`, `TODO`,
  `local`, and `localhost`, unless the screen is explicitly a developer/admin
  diagnostics tool. Technical facts belong in docs, tests, logs, and code
  comments; UI must say the business thing: "Proof uploads in background",
  "Waiting for network", "Already scanned", "Needs proof", "Cannot submit yet",
  "Wrong shed", "Try again", etc. Before handing off any mobile/frontend UI
  change, scan changed strings/screenshot fixtures for internal words and inspect
  rendered screenshots for leaked technical copy.
- Same-page drawers, sidebars, modals, and popovers are client-local UI state.
  Ordinary open/close clicks must not navigate, issue a document/RSC request,
  or trigger page-level loading UI. Use `LocalOverlayLink` and a narrow local
  controller with Back/Escape/outside/X/focus restoration. When detail is not
  present in the list response, open immediately from list summary data and
  fetch only the missing detail inside the drawer through an authenticated
  Server Action/Route Handler. Query-only Next links, native anchor/forms, or
  router pushes used to toggle an overlay are banned. Keep
  `make admin-web-local-overlay-guard` at a zero legacy baseline.
- When the user asks to fix a frontend/UI issue, rendered browser review is part
  of the requested fix for Codex, Claude, and every developer. Do not treat it
  as optional judgment or defer it to the user. Reproduce the user’s route,
  viewport, scope, filters, drawer/modal state, and click path as closely as
  possible; if the user supplied a screenshot, that screenshot is the minimum
  acceptance case. Do not push a frontend fix until the changed screen has been
  opened locally and visually checked, or until you explicitly report why local
  rendering is blocked.
- Raw vaccination config/protocol tokens are never API presentation copy or
  user-facing UI copy. Codes
  such as `et_tt`, `et_tt_adult_w2`, `ppr_booster`, `blue_tongue_first`,
  `goat_pox`, the protocol family name `Preventive Care Vaccination Matrix`,
  and similar backend/config identifiers may exist in backend config,
  raw storage/contracts/DTOs, non-UI tests, or a dedicated display mapper only.
  Backend display fields (`driveName`, `vaccineLabel`, `vaccine_labels`, card
  titles/subtitles, alerts), admin-web, Android screens, Paparazzi screenshot
  fixtures, cards, rows, chips, alerts, logs visible to operators, and generated
  UI galleries must render human labels such as `ET+TT`, `PPR · Booster`,
  `Blue Tongue`, and `Goat Pox`.
  `make ui-vaccine-labels-guard` is part of the standard guardrail/local-CI
  path and must fail any direct UI leak.
- **Maintainer decision, 2026-08-02:** every user-facing notification (push,
  in-app, banner, or leadership escalation — vaccination, weighing, feed, and
  counts alike) must be MEANINGFUL, never abstract. It must carry park name,
  shed/partition label, vaccine/work-item name in human form, animal/shed
  counts, and a farm-readable due date in IST; a leadership escalation must
  name which sheds are outstanding, not just report a count. Prevents the
  count-only-abstract-notification defect (e.g. "Vaccination(s) due soon · 3"
  telling nobody which park/shed/vaccine/date). Enforced by
  `make notification-specificity-guard`
  (`tools/agent-hooks/check-notification-specificity.mjs`), which composes
  with — and does not duplicate — `ui-vaccine-labels-guard`. See
  `docs/decisions/2026-08-02-meaningful-notification-copy.md`.
- Vaccination proof grain is SOP/backend-owned. Do not hardcode "per goat",
  "shed level", "camera only", or "gallery allowed" in admin-web or Android.
  Backend SOP/form DSL/proof policy decides the proof mode, subject scope,
  minimum/maximum proof count, capture sources, and verifier instruction; clients
  render that contract. Both modes must remain supported: per-goat video proof
  and shed-level video proof. A change from one mode to the other must never
  delete the unused mode, bypass GCS proof upload, skip verifier instructions,
  or invent proof requirements in mobile/frontend state.
- For frontend code changes, perform rendered visual QA before pushing. Open the
  changed local page, capture and inspect screenshots, and compare with the
  authoritative UI/UX source of truth, the mock `mock/goatos-dashboard-mock.html`
  (port its structure, not just its colors). Old dashboard/admin pages are NOT
  the visual target and must not be reused/recolored. Before push, run the
  mandatory gate `npm --prefix apps/admin-web run check:mock-fidelity`.
  Check pixel-level UI quality: sidebar/nav alignment, tab/title spacing,
  typography, color, card padding, chart sizing, labels, icons, empty space,
  overflow, clipping, and desktop/narrow responsive states. Do not accept
  typecheck/build or a `missing_config` page as frontend visual proof. For
  admin-web, run `npm --prefix apps/admin-web run smoke:visual:live` when the
  local backend/admin-web can be started; it captures desktop/narrow
  screenshots and runs layout/a11y/token-leak checks. Open the resulting images
  under `.codex-goatos-render/admin-web-screenshots/` and include the screenshot
  review result in the handoff before pushing. Build passing means only that the
  code compiles; it does not mean the UI ships.
  Route/table/drawer/popover changes also require reproducing the exact changed
  URL and viewport, then visually checking right-edge columns, horizontal
  overflow, clipped or stripped chips, active nav highlight, row-click
  destination, drawer/popup outside-click close, and drawer/popup close button
  behavior. A screenshot supplied by a reviewer/user is a failing visual test
  case until the same route is re-opened and the rendered screen is inspected.
- When a maintainer asks to "fix frontend" or reports a visible UI defect, treat
  frontend review as part of the fix, not as optional agent judgment. Use the
  same rendered lens for every session (Codex, Claude, Cursor, or sub-agent):
  reproduce the route, inspect the changed UI, catch adjacent clipping/overflow/
  close-behavior regressions introduced or exposed by the change, and document
  the visual proof. If a focused frontend fix is green, commit and push that
  focused fix to `main` promptly; do not pile unrelated visual-smoke fallout into
  one end-of-session batch. Split newly discovered adjacent UI bugs into their
  own focused commits unless they block the original fix's visual proof.
- Admin-web route/table/drawer/popover changes require a visual-closeout checklist
  before push. Reproduce the exact URL/viewport from any user screenshot when one
  exists, then verify: no right-edge/status-column clipping; no horizontal page
  overflow unless the table owns it; chips truncate intentionally without
  character-splitting or escaping their cell; drawers and popovers close by their
  close control and by outside click/back navigation; row clicks keep the correct
  module selected in the sidebar; and opened detail views show only the scoped
  real records for the clicked row. If any of these cannot be visually confirmed,
  the change is not ready to land.
- Read wide, write narrow: agents may inspect the whole tree, but edits must stay within declared task scope.
- Treat scale-safe design as a hard requirement on every design, prompt, and
  code change, sized to the current release scale target. Per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the present
  release envelope is 5,000-50,000 animals, with query-plan proof required at
  the envelope's upper bound (up to ~500k obligation rows); one-million to
  1-5M-animal deployment is the future certification bar, not a present
  release requirement. Regardless of that target, before accepting any new query,
  worker, import path, reporting path, or UI data flow, check the scale shape:
  tenant/run scoped, indexed, chunked or paginated, bounded in memory/
  goroutines, idempotent for retries, and covered by query-plan validation when
  it touches large tables.
- Treat hot API/SSR latency as part of scale safety, not polish. Every
  operator-facing API read, admin-web SSR page bootstrap, dashboard, schedule,
  calendar, worklist, and drawer/list load has a hard sub-500ms budget under the
  API latency policy (`tools/perf/api-latency-policy.mjs`: p90 <= 300ms,
  p95/p99 <= 500ms). A seconds-class load is a bug even when `make ci-local`
  passes; `ci-local` is not latency evidence unless the live latency gate ran
  and recorded samples. Do not fix this with bigger limits, longer timeouts,
  skeletons, prefetch, or frontend caches. Fix the serving shape: narrow endpoint
  for the screen grain/window, indexed/keyset query, batched read, or accepted
  projection/read model.
- NEVER write these scale anti-patterns in `backend/internal/**` (request paths,
  app services, worker repo methods). They are fast at ~1k rows and fatal at 1M.
  Each is machine-blocked by `make scale-guard` (CI `guardrails` job); named,
  explained, and given its approved alternative in
  `docs/decisions/scale-anti-patterns.md`. The rule underneath all of them:
  **compute-on-write (projections), never compute-on-read** — with one scoped
  exemption: per
  `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, the five named
  Calendar/process-integrity/vaccination-shed/execution/operations screen reads
  carry a scoped `// scale-guard:ignore: 5k-50k-envelope` annotation and serve
  canonical indexed SQL directly at the current 5k-50k release envelope. The
  guard is NOT globally disabled: compute-on-read stays banned for every other
  path in `backend/internal/**`, and the exemption is removed from a screen the
  moment it earns its own projection under that ADR's scale-out ladder.
  - **compute-on-read / god-CTE** — reconstructing derived state from raw
    event/instance tables per request via a big multi-CTE query. Use a
    materialized read model updated on write; the request does an indexed lookup.
  - **capped read-time rollup presented as truth** — fetching a larger raw page,
    grouping in app/service/frontend state, then clearing pagination/cursor and
    showing the collapsed card/count as business truth. This is banned for
    Calendar, Action Center, and other operator projections. Put the grouped row
    in the projector/read model and prove it with seed/projector E2E.
  - **full (stop-the-world) MV refresh** — `DELETE FROM <projection> WHERE
    tenant_id` + full reinsert. Use incremental (outbox-delta) maintenance, or a
    version-swap; never whole-tenant delete+reinsert.
  - **N+1 query** — a `.Query/.QueryRow/.Exec/.SendBatch` inside a `for`/`range`.
    Use one set-based statement (`UNNEST`, `INSERT ... SELECT`, `CASE` bulk update).
  - **N+1 fan-out (the nested "N+2" case)** — a ctx-taking call to an injected I/O
    dependency (repo/reader/port/client/roster/ownership) inside a `for`/`range`,
    where the real `.Query/.Exec` sits one adapter layer down — invisible to the
    raw-driver **N+1 query** check above. "Small data, still slow": one round trip
    per row, so a 25-row page becomes 51 serial reads. Machine-blocked as the
    `n-plus-one-fanout` rule (`make scale-guard`, distinct from `n-plus-one`;
    baselined debt in `tools/scale-guard/baseline.txt`). Fix by batching to a
    single `*ByIDs` / `= ANY($1)` read (as `ShedSummary` now does with
    `ShedOwnerships`), not by looping a per-item service/port call.
    Vaccination operator availability is explicitly in this class: a sweep or
    preflight may probe many dates, but it must share one session cache at
    `(tenant, park, business_date, cap_per_operator)` grain across capacity
    scoring, `ConductedBy`, effective-cap, and assignment-split helpers. Do not
    call `AvailableVaccinationOperatorsForDrive` from those helpers independently
    or inside park/date loops.
  - **OFFSET pagination** — `LIMIT/OFFSET` with a growable offset. Use keyset/cursor.
  - **non-SARGable predicate** — `lower(col) LIKE '%x%'` / function on an indexed
    column. Use a normalized column, expression index, or `pg_trgm` GIN.
  - **column-side type cast in a predicate** — `indexed_uuid::text =
    ANY($1::text[])` can disable the ordinary index on the stored column. Keep
    the column bare and cast the typed bind array (`indexed_uuid =
    ANY($1::uuid[])`). Add a natural planner proof at a realistic row count;
    forcing `enable_seqscan=off` is not sufficient.
  - **polling full scan / unbounded worker tick** — copy the keyset-chunked
    `FOR UPDATE SKIP LOCKED` claim used by the obligation/idempotency sweepers.
  - **non-terminating pagination loop** — a read-page loop with no cursor advance.
    Guarantee forward progress (monotonic cursor or exclude processed rows).
  If a case is genuinely bounded, annotate it `// scale-guard:ignore: <reason>`;
  do not disable the guard. A green latency gate today means "correct shape", not
  "1M-proven" (gates run at ~1k rows — see the ADR's runtime-gap section).
  - **guard false-green across configuration blocks** — a static Terraform/HCL
    guard must parse/bound the resource and require related `name`/`value` (or
    equivalent) fields in the same block. Every guard self-test must include an
    adversarial sibling-block fixture, and `ci-local` must run both the
    self-test and the real check.
  The admin-web twin lives in `apps/admin-web/**`: a Next.js SSR helper that drains
  a paginated endpoint cursor-by-cursor into one array to compute a KPI (the
  `searchAllGoats` full-herd walk, removed in `810bc1b3`) is machine-blocked by
  `make admin-web-request-reads-guard`
  (`tools/agent-hooks/check-admin-web-request-reads.mjs`); read a projection/summary
  endpoint instead. Rule + rationale in `docs/decisions/scale-anti-patterns.md`.
- Treat every aggregate/projection/card/summary as a grain-and-identity proof,
  not an arithmetic exercise. Before writing or approving a query that combines
  `JOIN` with `COUNT`/`SUM`/`GROUP BY`, identify the canonical membership source,
  use the same stable group key on producer and consumer, prove every join is
  1:1 or deduplicate/pre-aggregate the many side, map farm/park/shed/cohort with
  an explicit scope matrix, and keep totals/reminders independent of UI page
  size. Tests must adversarially cover one-to-many fan-out, shifted
  due-vs-execution dates, every supported scope depth, page boundaries, and the
  live DB status matrix when status buckets exist. Add the nearby
  `projection-review:` evidence marker defined in
  `.agents/skills/goatos-code-review/references/aggregates-and-projections.md`
  and run `make aggregate-projection-guard`; the required CI guard includes
  committed, staged, unstaged, and untracked changes.
- Cross-surface count parity (Claude AND Codex): the SAME business fact must show
  the SAME number on every surface that renders it — admin-web, the mobile app,
  and the API. If two surfaces disagree (e.g. a drive shows 200 doses on the web
  operator schedule but 400 on the mobile calendar), one backend read model is
  wrong even if each query is internally consistent — the frontend/mobile is
  usually faithfully rendering a wrong backend number, so "web fine, mobile
  broken" is really "two backend read models of the same fact disagree". Pick ONE
  authoritative source+grain per business count and reuse it across surfaces
  (for "animals in a drive/day" that is `count(DISTINCT target_id)` over the real
  `vaccination_drive_assignments`, the grain the operator schedule uses). NEVER
  render an estimate/rollup column (`estimated_targets`, `estimated_*`,
  `*_quantity`, cached counters) as a user-facing count while a sibling surface
  reads the actuals. When you add or change a count shown on more than one
  surface, prove parity in the same change next to the `projection-review:`
  marker and add a test asserting the surfaces resolve to the same source/grain.
  Full rule + the 200-vs-400 incident:
  `docs/decisions/scale-anti-patterns.md` -> "Cross-surface count parity".
- Mobile Vaccination Overview current-drive count lock (maintainer decision
  2026-07-29): the mobile Vaccination Overview top summary is NOT a CEO
  adherence KPI, NOT an obligation-history rollup, NOT a shift/carry-forward
  number, and NOT a dose-administration count. It must answer only: for the
  current visible vaccination drive, how many distinct animals are in the drive,
  how many have been vaccinated/submitted, and how many are left. The denominator
  must be the real current drive animal membership
  (`count(DISTINCT target_id)` over the drive assignment/membership grain; for
  the current STG ET+TT example this is 324 animals, never 1296). Do not join
  protocol dimensions/rule rows/dose rows in a way that fans out animals. Do not
  include completed history from older drive dates unless those animals are part
  of the current visible drive membership. Any mobile change touching this card
  must include a regression test for a multi-dimension ET+TT rule where the
  display remains 324 total animals and shows vaccinated vs left from the same
  drive grain.
- E2E publishing rule for Codex, Claude, and every feature agent: any generated
  E2E result for a feature, fix, audit, or scale gate must be committed inside
  this repo and surfaced on the GitHub Pages CI report site before handoff. The
  report must appear as a card/list item on the main CI reports index
  (`https://vgoats.github.io/goatos/`), not only as a standalone deep link.
  If the E2E belongs to an existing category (for example a vaccination kernel
  story belongs inside `/e2e-report/`), add it inside that category's report;
  do not create another root card. Create a new root card only for a genuinely
  new report category, then wire that category in `.github/workflows/pages.yml`
  and document it in `docs/runbooks/github-workflows.md`. The detail page must
  follow the existing E2E report visual contract: self-contained HTML with
  title/subtitle, summary tiles, pass/fail/pending badges, report sections, and
  readable code/evidence blocks. It must explain the test in enough detail for
  a reviewer who did not write the code: what behavior is under test, why it
  matters, setup/data, action/trigger, assertions, evidence source, and any
  certification boundary. One-line headings or test names are not enough. Do
  not publish raw markdown, a bare `<pre>`, screenshots-only evidence, or a
  hidden artifact as the final report.
  Do not leave E2E reports only in `/tmp`, scratchpads, attachments, local
  `.codex/` or `.claude/` folders, or chat. State clearly when a report is local
  E2E only and not staging or production certification. Before handoff, verify
  the live root index and the report URL with `curl`; if GitHub Pages caching is
  in play, include a `?v=<commit-sha>` cache-busting URL plus the workflow run.
- Treat the operational kernel as the golden rule for every feature. Read
  `context/architecture/operational-kernel.md` before designing or implementing
  triggers, obligations, reminders, notifications, deadlines, escalations,
  dashboards, Calendar, Action Center, Protocol Adherence, Workflow, or
  process-integrity views. Every feature must answer: what process was expected,
  was it followed, where did it break, who owns next action, what is due by
  when, what evidence proves it, and what alert/escalation fires when a deadline
  is crossed.
- For any projection-backed serving read behind a freshness/coverage gate
  (Vaccination execution/operations/shed, CT/AC/PA, Calendar), follow the
  Serving-Read Freshness Contract in
  `docs/decisions/high-scale-dashboard-projections.md` (also in the
  `kernel-scale-lens` skill), Claude AND Codex: (1) freshness TTL must exceed the
  projector refresh schedule (jitter headroom) and is AGE-based — never widen the
  TTL to mask a date-coverage bug; (2) date coverage is inclusive-query vs
  exclusive projection `date_to` — project one day beyond the max query range
  (45d ⇒ 46d), calendar/day-based projectors must align default windows to
  business-day boundaries and cover the UI's supported week/month query windows
  (for example Monday-start weeks and previous/current/next first-to-last-day
  month picker requests)
  rather than `now±N` clock instants, and fixed-date tests seed the window
  around their fixed dates; (3) stale last-known-good rows serve with freshness metadata while
  no first projection or an uncovered date/window fails closed; (4) a rebuild
  keeps serving last-known-good and a failed rebuild never clobbers it; (5) reads
  served entirely from a bounded canonical index (completed/accepted history)
  are NOT gated on the hot projection; (6) a prune of
  non-serving versions re-derives `serving_projection_version` inside the DELETE,
  never a version captured before the txn/advisory-lock released.
- Treat every Android READ screen as offline-first with Room as the single source
  of truth for the UI (hard rule — Claude, Codex, and humans). Backend owns the data;
  on-device, the screen renders from Room and the network refresh runs in the
  background (stale-while-revalidate): persist every backend read response to Room,
  have the repository expose a `Flow` the ViewModel observes, refresh-on-open to upsert
  Room (which re-emits), and show a sync/stale indicator — NEVER a blank/loading wall
  on re-entry when cached data exists. A network-only read repository (a thin
  `api.xxx()` pass-through with no Room persistence) is BANNED for screen-facing reads;
  new read models ship with their Room entity + DAO + Flow from day one. Do not call
  the app "offline-first" until the read models are cached (bootstrap + the write
  outbox already are; Calendar/Control-Tower/Execution/Adherence/Insights must be
  migrated). Full rule + the NetworkBoundResource pattern:
  `docs/decisions/android-offline-first.md`; refs the Android data-layer + offline-first
  architecture guides.
- Every Android READ screen must be refresh-on-open (hard rule — Claude, Codex,
  humans): call the shared `sg.mesha.goatos.core.ui.RefreshOnResume { onEvent(XEvent.Refresh) }`
  composable (`core/core-ui/.../RefreshOnResume.kt`, wraps
  `LifecycleEventEffect(Lifecycle.Event.ON_RESUME)`) near the top of the screen's
  composable body so cached Room data shows instantly and a background refresh
  fires automatically every time the user lands on or returns to the screen — a
  retained ViewModel on the nav backstack must never show data that was only
  fetched once at ViewModel creation. Never rely on a manual sync button/icon as
  the only way to see fresh data; a visible sync affordance is allowed as a
  supplementary manual trigger, not the primary refresh path. Skip this only for
  screens where a resume-triggered refresh would disrupt in-progress user input
  (scan-capture flows, forms, mid-entry screens) — the ViewModel's `refresh()`
  itself must stay non-blocking (upsert Room on success, leave cache visible on
  failure) so this never produces a loading wall. See
  `docs/decisions/android-offline-first.md`.
- Every Android READ screen's sync/refresh icon must show a spinning animation while a refresh
  is in flight and become non-clickable to prevent duplicate refresh triggers (hard rule —
  Claude, Codex, humans). Use the shared `SyncIconButton` composable
  (`sg.mesha.goatos.core.ui.SyncIconButton`, `core/core-ui/.../SyncIconButton.kt`) on every
  screen with a manual refresh affordance: pass `isSyncing = state.isRefreshing` (or
  `refreshInFlight` if the screen names it differently) and `onSync = { onEvent(XEvent.Refresh)
  }`. When `isSyncing` is true, the icon continuously rotates 360° and the button is disabled,
  so duplicate taps are ignored. When false, the icon is static and clickable. Applies to all
  read screens with visible refresh buttons: Calendar, Sheds, Counts, Approval, Verify (queue),
  Leadership (all three screens), and any future read screens that expose manual sync. This is
  consistent across the app and prevents race conditions from overlapping refresh requests.
- Treat every Android Room schema change as an installed-APK upgrade contract, never just a
  fresh-install schema (hard rule — Claude, Codex, humans). Room builds a DB two ways: a fresh
  install runs `createAllTables` (every @Entity), but an in-place upgrade runs ONLY the registered
  `Migration` objects and then validates against the @Entity set — so an @Entity added to a
  @Database with no migration to CREATE its table compiles, works on fresh installs, and CRASHES
  every upgrade on open (`Migration didn't properly handle <table>`). This actually shipped
  (roster_timetable_cache / roster_coverage_cache, MOB-007) and a plain in-memory Room test is
  blind to it. Required: `exportSchema = true` + committed `schemas/<db>/<version>.json`; every
  version bump ships its `Migration(N-1, N)` that creates exactly the new tables/columns/indices;
  additive + non-destructive (no `fallbackToDestructiveMigration` — the outbox holds unsynced
  operator writes, the cache is the offline SSOT); and BOTH a schema-equivalence `*MigrationTest`
  AND an upgrade-crash `*UpgradeCrashTest` (seed an old-version file via a test-only old @Database,
  reopen with current schema + real migrations, assert no crash + data preserved). Machine-blocked
  by `make room-migration-guard` (`tools/agent-hooks/check-room-migration-safety.mjs`, diff-scoped,
  in the CI `guardrails` job). Full rule: `docs/decisions/room-migration-safety.md`.
- NEVER fetch more than one screen-page of rows on mobile/web (hard rule — Claude,
  Codex, humans). A phone viewport holds ~7-10 items; pulling 50/200/1000 rows to
  render is the mobile twin of compute-on-read. Machine-blocked by `make mobile-guard`
  (`tools/agent-hooks/check-mobile-list-fetch.mjs`, diff-scoped in CI so a commit with
  no mobile code passes instantly); rule + rationale in
  `docs/decisions/mobile-data-fetch-anti-patterns.md`. The rules:
  - **Calendar week/month overview = DOTS ONLY** — one per-day marker (a drive exists;
    optional tone) from a backend day-marker set (`includeDateMarkers` /
    `CalendarDateMarkerDto`). Never fetch or parse a day's events to draw the grid.
  - **Every drill level paginates** — L1 day list, L2 sheds, L3 vaccine-capture
    (done/pending/skipped animals) are each a keyset page of **~20** with infinite
    scroll (prefetch next at item ~17-18). Never request > ~20 rows in one page,
    and never show a tappable "Load more" row/button for normal mobile work
    queues. Pagination is app-owned viewport behavior; users should see only the
    work list plus a passive loading footer while the next page is already in flight.
  - **A vaccination drive is a park visit with a mix of SHEDS, never grouped by
    vaccine** — one drive can contain one or many sheds. Coverage-by-vaccine is a
    metric, not the drive grouping.
  - **Vaccination date moves/reverts are kernel writes, not read-model sidecars** —
    moving a vaccine from a mixed operator-cap drive must update raw
    `vaccination_drive_assignments` membership so the old date loses only that
    vaccine and the target date gains it. Reverting by selecting the original
    date must cancel the active override and restore the original raw
    assignment membership. Never declare this fixed from frontend banners or
    read-time `COALESCE(override_date)` behavior; E2E must assert raw DB rows
    across move and revert while preserving all clinical rule outputs
    (kid/adult, boosters, live/killed spacing, sick/ICU/pregnant/dead/cull
    deferrals) and operator animal caps.
  - Parse/transform each field ONCE (never re-parse inside `.find`/`.filter` → O(n^2)),
    off the Main thread (`Dispatchers.Default`, ideally in the repo via `flowOn`).
  - **Room is the single source of truth, so pagination binds BOTH layers** — the network
    fetch AND the Room read the UI observes use the same keyset + ~20 page size. NEVER
    `SELECT *` / `observeAll()` / an ever-growing accumulated blob; the observed read is a
    bounded keyset window (Room `PagingSource`; Paging 3 + `RemoteMediator` for large lists).
    Otherwise the over-fetch just moves from network to DB.
  If a case is genuinely bounded (e.g. a fixed 7-cell week loop) annotate the line
  `// mobile-guard:ignore: <reason>`; do not disable the guard.
  Android navigation has a separate structural invariant: backend-composed root
  destinations are L0 and alone own the bottom bar/drawer. Every L1/L2/L3/L4
  drill is a distinct hosted `NavHost` destination with Up/Back and no root
  chrome; exact route membership is mandatory, prefix matching and reusing an
  L0 route as a drill target are forbidden, and structural details must not be
  disguised as modal sheets. Machine-blocked by
  `make android-navigation-stack-guard`; canonical decision:
  `docs/decisions/android-navigation-stack.md`.
  Placement is a separate invariant: a FEATURE ENTRY POINT (alerts, inbox,
  videos, profile, a module switch) belongs in the bottom bar or the module
  drawer and NEVER in the top-right app bar, which carries only actions on the
  current screen. Machine-blocked by `make nav-entry-point-placement-guard`;
  canonical decision: `docs/decisions/nav-entry-point-placement.md`.
  The retention twin is memory, not fetch size: an in-heap cache/accumulator that
  grows with no cap/TTL/eviction, or a DAO reading a whole table into memory
  (`observeAll` `SELECT *`), OOMs the phone at scale (fixed in `7058fff2` +
  `d58acac2`). Machine-blocked by `make android-bounded-memory-guard`
  (`tools/agent-hooks/check-android-bounded-memory.mjs`, diff-scoped) — use an
  `LruCache` or a Room `JsonBlobCacheDao` with `readCachedJson` (TTL) +
  `enforceCacheBounds` (row/byte cap), or filter the DAO read to active rows
  (`WHERE status IN (...)`) / a `LIMIT` window.
- Treat idempotency as a mandatory write-path contract for every mutating API,
  worker, importer, webhook, state transition, outbox producer/consumer, server
  action, and UI-triggered write. Each write path must accept or derive a stable
  idempotency key or operation identity, persist that key and a semantic request
  fingerprint in the same transaction as the side effects, return the original
  result for an exact replay without rerunning side effects or outbox work, and
  reject a same-key different-payload replay or return the original result with
  no new side effects. The SQL pattern `ON CONFLICT DO UPDATE` with only
  `idempotency_key = EXCLUDED.idempotency_key` is not sufficient when later code
  can still mutate state. Tests must cover first call, exact replay, same-key
  different-payload replay, and downstream duplicate prevention.
- Treat a state transition and the sync of any derived read model it OWNS as ONE
  atomic transaction. A record must never reach its published/committed state
  while a read model it is the sole writer of failed to save. Do the derived
  upsert AND any post-write parity/verification check INSIDE the same DB
  transaction as the state change, so a sync failure rolls the whole transition
  back — no status flip, no outbox event, no audit row, no partially-written read
  model. A post-commit "best-effort" sync is allowed ONLY as a fallback for an
  already-committed replay or an adapter without transactional support, never as
  the first-commit path. Canonical case: publishing a vaccination protocol version
  upserts + parity-checks `rule_dsl.capacity` into `vaccination_capacity_config`
  inside the publish transaction (`PublishVersionWithCapacity` /
  `PublishVersionWithDerivedRules`), and a parity mismatch
  (`ports.ErrCapacityParityMismatch`) rolls the publish back. Every such flow needs
  a rollback regression test — failed sync ⇒ source stays in its prior state with
  zero side effects; see `TestPublishVersionWithCapacityRollsBackOnSyncFailure`.
- Treat authored config/business values as validate-or-reject, never
  silently-default. A field that is PRESENT but out of range (e.g.
  `rule_dsl.capacity.max_per_day < 1`, `max_buffer_days < 0`) must FAIL the
  publish/save with a clear error, not be rewritten to a default business value
  the author never entered; defaults apply ONLY to genuinely-absent fields.
  Frontends must keep a cleared field distinct from an explicit `0` (a blank input
  publishes the declared default; an explicit out-of-range value is sent verbatim
  so the backend rejects it) — never coerce blank to `0` or to an invented value,
  and never let a React default become authored business truth.
- Treat the clinical defer set as a mandatory medical safety block, never an
  authored subset (C35-010). The four clinical states `sick`, `under_treatment`,
  `quarantine`, `icu` are non-optional postponement rules per
  `docs/preventive-care-vaccination/vaccination-rules.md`: an animal in any of them
  must have its open vaccination work DEFERRED (held for recovery), never cancelled
  or left scheduled — a wrong medical action is P0 regardless of how cleanly it
  compiles. A published rule's `eligibility.defer_states` may only ADD states; it
  may never drop one of the four. Enforce on BOTH layers: publish/validation must
  REJECT a present, non-empty `defer_states` that omits any mandatory clinical
  state (an empty/absent list maps to the safe full default), and the generator
  must union the mandatory set in regardless of the authored list so an
  already-published partial rule is still safe at runtime. The single source of
  truth is `backend/internal/protocol/domain.MandatoryClinicalDeferStates`
  (`EffectiveClinicalDeferStates` / `MissingMandatoryClinicalDeferStates`) — do not
  re-hardcode the set elsewhere; the SQL siblings
  (`vaccination_eligibility_rollups` usable flag,
  `ListRecoverableDeferredVaccinationGoatIDs`) must stay in sync with it. Mechanical
  backstop: `make clinical-defer-guard` (required in CI).
- CI availability is never a closure blocker (Claude AND Codex). A GitHub Actions
  billing/spending/platform failure — the synthetic `BuildFailed` /
  `(Unknown event)` / zero-job `startup_failure` runs — must NOT be recorded as
  an external blocker or used to defer a fix. When remote GitHub Actions cannot
  execute, run the SAME affected-component gates LOCALLY via `make ci-local`.
  The default classifier compares the candidate to `origin/main`, always runs
  common repository guards, and adds backend, admin-web, and/or Android jobs only
  when their owned paths or shared contracts changed. Unmapped paths and changes
  to CI workflows, CI scripts, agent hooks, or the Makefile force the full suite;
  `make ci-local MODE=all` is the explicit full-suite command. Treat a green
  `make ci-local` on the exact pushed SHA as the authoritative ordinary
  deterministic CI gate. It does not replace applicable PostgreSQL, migration,
  device, browser, deploy, or live-state certification lanes; those remain
  closure blockers. Record the `make ci-local` SHA + result as current-SHA
  ordinary-CI proof. Restoring org Actions billing stays a separate maintainer
  task, tracked but never blocking closure.

- **Postgres tests are explicit opt-in only**: Default `make ci-local`, every
  `JOB=...`/`MODE=all` invocation, pull-request workflow, push workflow, and
  scheduled workflow must not start Postgres or run Docker-backed DB tests.
  A local database run requires `GOATOS_RUN_POSTGRES_TESTS=1`. Hosted DB gates
  are not a Goat OS staging deploy path while GitHub Actions is unavailable.
  `MODE=all` means all affected component jobs, not Postgres.
  `GOATOS_REQUIRE_DOCKER=1` may make an explicitly requested DB run fail closed,
  but it must never opt a default run into Postgres by itself.

- **Exact-SHA local-CI push gate (main)**: Only a complete green `make ci-local`
  on the exact commit SHA authorizes a push to `main`. The pre-push hook installed by
  `make ai-setup` enforces this via a machine-local SHA-bound receipt
  (`goatos-ci-local-receipt.json` in the worktree git directory). The receipt
  is either mode `all`, or mode `scoped` bound to the exact remote-main base,
  component-rule hash, and complete classifier-selected job list. The hook
  recomputes scoped coverage at push time; a changed base, stale rules, missing
  component, or newly-full diff is rejected. Explicit partial
  `JOB=...` runs intentionally write NO receipt and never authorize a push. Every
  new machine guardrail MUST be registered in `tools/ci/guardrail-manifest.json`
  and wired into both `Makefile:guardrails` and `tools/ci/run-local-ci.sh`. The
  current `guardrail-registration-guard` proves enumeration, declarations, and
  textual reachability only; semantic execution/routing, existence/uniqueness,
  and spoof resistance remain F0 work and must be manually verified until that
  hardening lands. See `docs/runbooks/local-release-evidence.md` →
  "Exact-SHA Local-CI Push Gate (Main)" and `docs/runbooks/local-ci.md` →
  "Guardrail registration and exact-SHA push evidence" for the full flow. Do not
  bypass the hook with `--no-verify`.
- **Mandatory ordinary main landing (Codex and Claude)**: For ordinary work and
  this documentation foundation, when the requested outcome includes pushing
  to `main`, run **`make land-main`** instead of composing
  `git fetch` / `git rebase` / `make ci-local` / `git mesha-push` by hand. The
  target refuses a dirty worktree, fetches fresh `origin/main`, rebases the
  candidate before CI, runs the complete affected-component `make ci-local`,
  fetches main again, and reruns rebase + CI if main moved before pushing the
  exact certified SHA. Agent hooks block direct agent-issued pushes to `main`,
  and the Git pre-push hook independently rejects a candidate that does not
  contain the current remote-main SHA. Do not auto-rebase at session start:
  sessions may open on dirty/shared worktrees with other agents' changes. Commit
  only the scoped work and use a clean isolated worktree for landing. Standalone
  `make ci-local` remains valid for development/hosted CI; `make land-main` is
  the release path that mutates history and pushes. Do not use GitHub connector,
  `gh pr merge`, or the web merge button as a shortcut unless the current PR
  head already has a completed green GitHub `ci` run on the exact SHA after a
  fresh-main rebase.
- **Whole-ledger/task-kernel program landing exception**: the documentation
  foundation may use ordinary `make land-main`, but the approved implementation
  program uses exactly one external integration PR. It must not use milestone
  PRs or ordinary `make land-main`. F0 first adds the repo-owned
  `make land-integration-pr PR=<number>` exact-head fast-forward gate defined in
  `context/execution/defect-prevention-execution-contract.md`; until then no
  implementation batch closes and the program PR cannot land.
- **Mandatory GitHub release tags and Firebase provenance**: Every dev/stg/prod
  release must create an annotated GitHub tag through `make release-tag`, never
  a hand-written `git tag` command. The tag message must keep separate Backend,
  Frontend/Admin Web, Mobile Android, Infra/Deploy, Docs/Seed/Data, and Other
  sections. STG Cloud Deploy creates the tag automatically after verified
  rollout. Firebase App Distribution releases must restore credentials with
  `make restore-stg-android-release-env`, then add the Android version/code and
  Firebase release URL to the tag before handoff. See
  `docs/runbooks/release-tags.md` and `docs/mobile/stg-signed-release.md`.
- Treat Goat OS time semantics as India-business-calendar semantics. Physical
  storage may use `timestamptz`/absolute instants, but every business meaning
  derived from those instants — scheduling, due/missed buckets, reminder keys,
  reporting groups, audit-log display, and UI labels — must convert to
  `Asia/Kolkata` first. UTC must never define a Goat OS business day.
- **VACCINATION TIME GRAIN IS THE BUSINESS DAY — NEVER HOURS (maintainer rule,
  Claude AND Codex).** A vaccination drive is a business DAY in `Asia/Kolkata`.
  It is not an instant, not a timestamp, and never "now ± N hours". This is a
  business rule about how the farm actually works, not a test-hygiene
  preference: operators work a day, a drive is planned for a day,
  `planned_date` is a `DATE`, and two vaccination facts on the same business day
  are the same day no matter how many hours separate their timestamps.
  - Never place a drive, due value, safe window, or query window with hour or
    minute arithmetic. No `time.Now().Add(-2 * time.Hour)`, no
    `dueAt.Add(-1 * time.Hour)`, no `now±N` clock instants — in production code,
    fixtures, or assertions.
  - Anchor to `biztime.BusinessDayStart(...)` / `biztime.BusinessDate(...)`, or
    to a fixed business date. Compare business DATES, not instants.
  - Never widen an hour tolerance to make a same-day comparison pass. If a
    same-day check fails because two timestamps differ by hours, the DAY is the
    correct unit and whichever side compares instants is the defect.
  - Why this is a hard rule: hour-anchored fixtures shipped a defect class where
    15 calendar tests passed or failed depending on the time of day they ran — a
    batched park drive anchors at 00:00 IST, the fixtures asked for `dueAt - 1h`,
    and that fell outside the window except during a ~1-hour slice of each day.
    Sub-day precision on a vaccination date is always a bug in the making.
  - If a specific case genuinely needs sub-day precision, it needs a recorded
    maintainer decision first. Ambiguity is not approval.
- Pinned-clock tests must derive time-sensitive fixture fields such as
  `valid_from`, `valid_to`, due instants, and recipient eligibility from the
  same pinned anchor. Never mix a pinned application clock with SQL `now()` or
  a second `time.Now()` when the fixture is evaluated against that anchor.
- For dashboards or reports that slice data by month, date, breed, farm, shed,
  load, category, status, gender, operator, source, or similar dimensions, use
  the canonical rule in `docs/decisions/high-scale-dashboard-projections.md`
  before coding.
- Add observability for new APIs/workers: latency, errors, DB pressure, queue lag, DLQ, and media failures.
- Goat identifiers (RFID, old tag, breed, farm, shed, partition) are operational
  livestock business data, NOT PII. Log them in diagnostics so a failure is
  traceable to the exact goat/row. The only logging redaction rule is secrets:
  never log credentials, tokens, or service-account JSON. Repo hygiene is
  separate and still applies: do not commit raw private source files or row
  dumps to git.
- Exception: Goat OS STG tester/demo login credentials that are intentionally
  documented in `docs/runbooks/stg-operator-login-credentials.md` are approved
  committed runbook data, not a review finding. Do not flag those STG
  email/password rows as leaked secrets unless the maintainer says they are no
  longer approved, they include production credentials, or they expose tokens,
  service-account JSON, API keys, private keys, or other non-demo secrets.
- Construct backend loggers via `backend/internal/platform/observability`
  (env sink `GOATOS_OBS_SINK`: `stdout_json`/`otlp`/`gcm`); do not hand-roll
  `slog.New` in new code. Log once at boundaries with trace/request/tenant/
  import_run_id context, and recover-and-log panics at goroutine edges. See
  `docs/decisions/observability.md`.
- Keep committed project docs role-based rather than person-based. Use labels
  such as data owner, reviewer, operator, CEO/internal admin, or vendor instead
  of individual names unless a legal/contract artifact explicitly requires a
  named person.

Do not:

- Shared local-stack identity and isolation (Codex and Claude): the browser-visible
  stack is exactly one atomic trio — admin-web `127.0.0.1:3300`, API
  `127.0.0.1:8080`, and database `goatos` in the named `goatos-local-current`
  container. FE and BE must run from the same clean checkout at **exact origin/main**.
  Start/recover it only through the persistent service wrapper;
  the supervisor owns both ports, discards ambient `DATABASE_URL` /
  `GOATOS_E2E_DATABASE_URL`, includes the `libpq` tools required by closeout,
  pins local query headroom so the canonical Calendar read cannot false-fail at
  the production-oriented 3-second deadline during local build load, and must
  verify an authenticated Calendar data-plane read in addition to `/readyz`.
  It must stop/restart FE+BE together if either child fails or `origin/main`
  advances. Never point the shared UI at a feature-worktree API or an alternate
  database, accept `/readyz` alone as proof that page data works, or start only
  half the shared pair.
- An **isolated E2E** stack is a separate test appliance with its own non-shared
  FE/BE ports and explicit throwaway database. It is intentionally independent
  of the shared exact-main stack. Shared-stack recovery must never stop, reuse,
  migrate, seed, fast-forward, or delete an isolated E2E process/container/DB.
  Conversely, E2E scripts must never claim `3300`, `8080`, or mutate the normal
  `5433/goatos` database. Inspect exact port owners and database targets before
  cleanup; do not infer that every local Goat OS process belongs to the shared
  stack. Enforcement: `make local-stack-service-guard`, required by normal
  `make ci-local`; operating contract:
  `docs/runbooks/local-full-stack-rehearsal.md`.
- Local dev servers (`:3300` admin-web, `:8080` backend): the workspace owner has
  granted agents (Codex and Claude) STANDING authority to stop, restart, re-port,
  or `next build` over them WITHOUT asking — just do it when the work needs it
  (clean build, or an expired local token making routes redirect to `/login`).
  Admin-web must be restarted through the local wrapper:
  `npm --prefix apps/admin-web run dev` / `dev:local`, including custom isolated
  ports like `npm --prefix apps/admin-web run dev -- --port 3318`. Plain
  `next dev` is forbidden because it bypasses `GOATOS_AUTH_*` env and makes
  `/admin-web/bootstrap` fail with `invalid_bearer_token`. Do not pause to ask
  permission for a restart/rebuild. The only
  discipline: restore the server on the SAME port, never silently change ports,
  don't run `next build` concurrently with a live `next dev` on the same `.next`
  (stop it first), and if you break it, restore it. See
  `apps/admin-web/AGENTS.md` → "Local Dev Server Safety" for the full rule. This
  applies to every agent (Codex and Claude).
- Always-on local stack rule (Codex and Claude): when a task needs any local
  frontend, backend, worker, importer, proxy, emulator, database container, or
  other Goat OS service, first check whether it is already running and do not
  stop it just because the immediate command is done. Prefer the persistent
  service wrapper (`make dev-local-service-start`, `make dev-local-service-status`,
  `make dev-local-service-logs`) over foreground one-off terminals for long-lived
  stack work. Leave required services running at the end of the turn/session
  unless the user explicitly asks to stop them or stopping is required to prevent
  machine damage/data loss. If code/env changes require a restart, restart on the
  same ports and health-check before reporting done. Do not finish with a needed
  app stack stopped, and do not leave required servers as active Codex terminal
  sessions that block the final response; use the service wrapper/supervisor and
  logs. Status/final updates must name what is running plus the URL/port. If a
  service cannot be kept running, state the blocker and the exact restore command.
- Founder/builder visibility invariant: `ravi@mesha.sg`, `manohark@mesha.sg`,
  `manju@mesha.sg`, `abhishek@mesha.sg`, and `aryaman@mesha.sg` are the
  platform-owner leadership cohort. In local, staging, and production seed/
  provisioning paths they must be granted `role='ceo_internal'`,
  tenant scope, and the RBAC grants needed for every built visible module.
  New features or visible route changes are incomplete until leadership
  seed commands, bootstrap/nav tests, and docs include the module. RBAC-based
  route visibility applies to non-founder operators, not to these five builder
  accounts.
  **A STG (or any) seed is INCOMPLETE until this grant is MATERIALIZED, not
  merely pending.** `auth_pending_email_grants` rows only become an active
  `user_scope_grants` row via the `/auth/session-events` runtime claim path,
  and admin-web Google SSO does not reliably trigger that path on the first
  login after a fresh seed — the observed failure is `403 permission_denied`.
  For STG, run `make seed-stg-9-person-login`
  (`backend/cmd/seed-stg-login-grants`), which materializes the ACTIVE grant
  directly: tenant scope for the 5 leadership accounts, park scope for named
  park staff/operators, keyed by `platformauth.StableSubjectID(issuer,
  firebase_uid)` — the same derivation the backend uses at request time. Verify
  with `make verify-stg-9-person-login` and
  `docs/runbooks/stg-9-person-login-verification.md` before declaring the seed
  done. `docs/runbooks/stg-login-seed-contract.md` is the canonical personnel
  rule this command implements.
  **Leadership log in with Google SSO OR Firebase email/password** (maintainer
  decision 2026-07-24; the prior SSO-only rule is retired — password convention
  `<FirstName>@2026`, maintainer sets the Firebase password). **All 9 accounts,
  leadership included, also need an active `workforce_members` profile**: the
  mobile `/app/bootstrap` hard-requires a profile row and returns
  `403 operator_profile_missing` without one, so leadership could open admin-web
  but got "Couldn't load your workspace" on the Android app until
  `ensureLeadershipMember` (in `seed-stg-login-grants`) created their
  `auth:<uid>` profile. Materialized grant alone is not enough; the profile is
  part of the completion bar.
- Operator scope invariant: no real operator may receive `scope_type='tenant'`.
  Operators belong to exactly one park (`scope_type='park'`, `scope_id=<park
  location_id>`) plus their explicit shed/task assignments. Tenant scope is
  allowed for platform leadership (`ceo_internal`) and director visibility
  roles (`pc_director`, future director aliases) when they must see both parks.
  If a director also needs to execute scanning work, give that person explicit
  operator-style park/task execution assignment; do not make the operator grant
  tenant-wide. STG login seed changes must pass
  `make stg-operator-scope-guard`; if this guard fails, fix the seed source
  instead of relying on downstream task filtering.
- Current active RBAC roles are documented in
  `docs/runbooks/current-active-rbac-roles.md`. Treat roles outside that list
  (for example `director_preventive_care`, `director_breeding`,
  `manager_feed`, `head_health`, `am_growth`) as dormant catalog scaffolding,
  not live STG/mobile personas. Do not grant or document them as current access
  without also shipping backend permission behavior, Android role handling,
  seed docs, and tests in the same change.
- CPT operator-drive rehearsal seed invariant: the committed packet at
  `fixtures/vaccination-cpt-operator-drive-2026-07-23/` is CPT/Channapatna only
  and uses business date `2026-07-23`. Do not synthesize CBE/Coimbatore rows.
  Seed Amit Kumar, Darshan Talwar, and Sagar Mahoor as equal vaccination
  operators with `200` unique animals/day/operator; seed Chandrakant as
  director-only monitoring scope. The `Adult` source filenames do not narrow
  the vaccination kernel: kid/adult/booster/clinical/combo-spacing/safe-window
  rules still come from backend vaccination rules.
  **Materialized grant + department binding is part of this invariant, not a
  separate concern.** Amit, Darshan, Sagar (operator role) and Chandrakant
  (`pc_director` role) are only real, working STG logins once their
  `user_scope_grants` row is `status='active'` AND their existing named
  `workforce_members` roster row (seeded by `seed-roster-real` /
  `seed-vaccination-cpt-operator-drive`) is bound to `user_id` with
  `department_id = preventive_care`, so `department_module_grants` gives them
  the vaccination bottom bar. `make seed-stg-9-person-login` is wired as a
  required final step of `seed-vaccination-source-full` and
  `seed-vaccination-cpt-operator-drive` when `GOATOS_ENV=stg` — do not seed CPT
  operator-drive rehearsal data on STG without it, and do not declare the
  rehearsal seeded until `make verify-stg-9-person-login` passes.
- Leadership assistant coverage invariant: every leadership-relevant table,
  read API, OpenAPI contract, admin-web route, mobile workflow, reporting view,
  domain event, or official KPI must resolve to a Cube governed metric, a
  `ceo_ai.*` view, an MCP Toolbox tool, a mapped Mesha read API, or a documented
  exclusion in `docs/ceo-ai/coverage-matrix.md` — in the same change. The guard
  is STRUCTURED, not keyword-based (tightened 2026-07-23): a new `CREATE TABLE`
  migration, a new OpenAPI `/path`, or a new exported read handler must ship a
  real coverage artifact (`ceo_ai.*` view / MCP tool / Cube binding / wired
  `Set*DataReader`) or a coverage-matrix row/exclusion NAMING that surface in the
  same commit; a bare keyword-bearing doc touch no longer satisfies it, and pure
  refactors pass without a coverage file. The external MCP connector is not a
  raw table/API auto-publisher; it exposes the leadership assistant product
  entrypoint. New tables/APIs become visible through Claude/Codex/CEO chat only
  after they are covered by the Cube/read-API/Toolbox/`ceo_ai`/SQL-fallback
  layer or explicitly excluded. The read-path routing is Cube-first (official
  KPI → Cube; then read APIs → MCP Toolbox `ceo_ai.*` tools → read-only SQL
  fallback). The planner → catalog →
  wiring → reader chain must be LIVE and CLOSED end-to-end (ROUTE-CLOSURE rule):
  every tool name must resolve in the runtime registry (Cube binding, executor spec,
  toolbox tool, or fallback alias), every RouteAPI target must have a wired reader or
  fallback alias, and every coverage row must reference a golden eval question. HOW-TO:
  `.agents/skills/goatos-leadership-assistant/SKILL.md` (includes ROUTE-CLOSURE rules).
  External MCP setup/docs: `docs/ceo-ai/external-mcp-integration.md`.
  Scaffold: `node tools/ceo-ai/scaffold-coverage.mjs <module>`. Enforced by
  `make leadership-assistant-coverage-guard` + `make assistant-route-closure-guard`
  (local CI + PostToolUse nudge for Claude and Codex).
- Do not reintroduce old staging labels as architecture.
- Do not commit generated Graphify/CRG graphs. `graphify-out/graph.json`,
  `manifest.json`, `GRAPH_REPORT.md`, `graph.html`, `cost.json` and the
  `.code-review-graph/` DB are gitignored and machine-regenerated locally. Commit
  ONLY the setup docs, rules, hooks, and generation scripts — never the graph
  artifacts themselves. Run `make ai-doctor` before pushing AI-tooling changes.
- Do not let the `ceo_ai` reporting/assistant namespace sit between the core
  Backend <-> Frontend <-> Mobile layers. `ceo_ai` is the leadership-assistant
  chatbot (`backend/internal/ceoai/**`, `/api/ceo-ai/*`) plus its read-only
  reporting SQL schema (`ceo_ai.*` views/functions read by Cube and the
  assistant). Data flows ONE way: core BE is the operator source of truth, and
  the assistant/Cube CONSUME it via Mesha read APIs, the MCP Toolbox, or
  read-only SQL. A core operator read path must never join `ceo_ai.*` or read a
  `ceo_ai_*` table for its own runtime data (this once 500'd Control Tower when
  the schema was absent — SQLSTATE 3F000). Shared display/derivation logic (e.g.
  vaccine labels) lives in a neutral core package such as
  `backend/internal/vaccination/domain`, read by both operator screens and the
  assistant. Frontend/mobile core pages must not route their data through
  `/api/ceo-ai/*` or import an assistant client module; the global assistant
  bubble in `MeshaShell` is allowed chrome, not a data path. Machine-gated by
  `make ceo-ai-boundary-guard` (backend SQL schema/table access, matched across
  newlines; plus FE/mobile `/api/ceo-ai` route + assistant-import coupling
  outside assistant-owned dirs); full rule in
  `docs/decisions/ceo-ai-reporting-boundary.md`.
- Do not let frontend/mobile read BigQuery, Sheets, Firestore, GCS, or operational databases directly.
- Do not spread vendor SDK calls through product code.
- Do not modify current live dashboard repos while building Goat OS copies.
- Do not add unbounded goroutines, full-table/full-herd API scans, direct media proxying through APIs, or dashboard raw BigQuery scans.
- Do not use direct gRPC for browser/React Native product clients without a new written ADR.
- Do not duplicate architecture decisions across random docs.
- Do not put individual staff/founder/vendor names into PRDs, TRDs, runbooks,
  prompts committed as docs, status files, or skill references when a role label
  is enough. The founder/builder visibility invariant above is the narrow
  exception because those exact accounts are provisioning seed truth.

Validation expectation:

- Run the narrowest relevant typecheck/build/test command for changed code.
- For DB query or migration changes on large tables, verify the indexed access
  path and add/update `make validate-sqlc-plans` coverage when the query is on a
  hot path or can touch import/goat/event/counter rows at scale.
- At phase closeout, compare code/contracts/migrations/tests against PRD/TRD and
  update context/skills/agent references if implementation changed the truth.
- For docs-only edits, run greps for stale terms when the user has explicitly banned wording.

Morning README update expectation:

- For Goat OS work sessions that start in the morning, check whether `README.md`
  reflects the latest pushed project status before moving deep into new
  implementation work.
- If phase progress, shipped backend/frontend pieces, deploy gates, real-data
  import status, or next-step priorities changed, update `README.md` with
  executive status wording and push it.
- Be precise: do not call Phase 1 shippable until auth/RBAC, frontend screens,
  production event egress, and real data-run gaps are actually closed.

Workflow documentation expectation:

- If GitHub Actions workflows or CI guardrail scripts change, update
  `docs/runbooks/github-workflows.md` with clear project-facing wording in the
  same change.
- The runbook must explain what each workflow does, when it runs, what temporary
  services it starts, and what common failures mean.

Mock auto-push expectation (Codex AND Claude):

- Standing order (2026-06-25): whenever you edit the ops-console mock
  `mock/goatos-dashboard-mock.html`, commit and push it IMMEDIATELY — do not wait
  for confirmation, so the pushed copy is never behind local edits.
- Run `tools/agent-hooks/push-mock.sh` after editing the mock (Claude also wires
  it to a Stop hook). The script commits ONLY the mock file and pushes `main` via
  `git mesha-push` — it never `git add -A`, so unrelated in-flight work is left
  untouched. It no-ops when the mock is clean.
- This applies only to the mock. Other code/doc changes follow the normal
  review-and-push flow.

<!-- BEGIN TELEMETRY GUARDRAIL (generated by telemetry-guard lane; do not hand-edit inline, extend docs/observability/TELEMETRY_GUARDRAILS.md instead) -->
## TELEMETRY GUARDRAIL (mandatory)

Whenever you add or modify a user-facing surface — an Android screen,
viewmodel, or flow in `apps/goatos-android`; an admin-web route in
`apps/admin-web`; or a new product-meaningful event emitted anywhere — you
MUST wire:

1. **Firebase Analytics event(s)** — `AnalyticsPort.track(...)` with an
   `AnalyticsEvents` constant (never an inline string) on Android; a
   Faro event (`faro`/`trackEvent`/`pushEvent`) or route error-boundary
   coverage on admin-web.
2. **Crashlytics fatal + non-fatal logging** on error paths that surface can
   hit (Android; wiring itself is tracked as TODO — see the doc below).
3. **The relevant funnel/journey step**, when the surface sits on a tracked
   journey (`login → bootstrap → drive-open → scan → vaccination-capture →
   submit`, or a future documented funnel).

Run `make telemetry-guard` (or `python3 tools/telemetry-guard/telemetry-guard.py`)
before committing — it is part of `make guardrails`, the compatibility
`make ci-local JOB=guardrails`, and the affected admin-web/Android component
jobs. It is diff-scoped against `origin/main` so unrelated commits pass instantly.

Use `// telemetry:exempt <reason>` only with a real justification (internal
debug-only screen, pure presentational component, route fully covered by a
parent error boundary) — it is a reviewer-facing escape hatch, not a rubber
stamp.

Full rule, rationale, required symbol names (including what is wired today vs
TODO), compliant/non-compliant examples, and how the guard works:
`docs/observability/TELEMETRY_GUARDRAILS.md`.
<!-- END TELEMETRY GUARDRAIL -->

**Whole-tree enforcement note (not part of the generated block above — do not
let a regen strip this):** `telemetry-guard` and its sibling `exception-guard`
("never swallow an exception" — same doc, §8) are diff-scoped by design, but
that is NOT the whole enforcement story. Pre-existing whole-tree debt is
enforced separately by a shrink-only ratchet — `make exception-guard-ratchet`
and `make telemetry-guard-ratchet`, both wired into `make ci-local` via
`guardrails` — that fails if a NEW violation appears anywhere in the tree
(not just on diff-touched lines) or if the checked-in baseline
(`tools/exception-guard/baseline.json`, `tools/telemetry-guard/baseline.json`)
goes stale relative to fixes. **Adding an entry to a baseline file to make a
new change stop failing the ratchet is not an accepted way to land code** —
fix the violation or add a genuine `exception:exempt`/`telemetry:exempt`
marker instead. Full mechanism, the reason baselines are keyed by
file+rule-kind and never by line number, and how to add a ratchet for a
future guard: `docs/observability/GUARDRAIL_RATCHET.md`. The lesson recorded
there: **a diff-scoped guard, by construction, silently permits unlimited
pre-existing debt unless it is paired with a whole-tree ratchet like this
one** — an adversarial audit found ~150 `exception-guard` FAILs and 51
`telemetry-guard` FAILs sitting in this tree with a permanently green
`ci-local` before this ratchet existed.

## Mesha / Goat OS RFID Language

When a maintainer asks for "RFID", "tag", or "tag IDs" for animals in Goat OS,
return the actual animal tag columns from `goat_identifiers`:
`animal_identifier_1` and `animal_identifier_2`. Do not answer with
`goats.display_id` (`G-...`) unless the user explicitly asks for display IDs or
both display ID and RFID. At least one of the two animal identifier columns is
expected to be present for active goat records; treat a missing RFID answer as a
data-quality finding, not as permission to substitute display IDs.
