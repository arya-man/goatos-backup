# Goat OS automation — handover

**Read this first, then only the section you need.** Written 2026-09-23 so another session can
take over. Everything here was measured, not assumed; where something is unproven it says so.

| I need to… | Go to |
|---|---|
| Know what must never happen again | [§1 Production incident](#1-production-incident--the-rules-that-came-from-it) |
| Know Ravi's standing instructions | [§2 The contract](#2-the-contract--ravis-standing-instructions) |
| Know what the automation actually catches | [§3 Coverage: the real number](#3-coverage-the-real-number) |
| Know what is built and what is proven | [§4 The five lanes](#4-the-five-lanes) |
| Pick up open work | [§5 Open work](#5-open-work) |
| Decide something only Ravi can | [§6 Decisions waiting on Ravi](#6-decisions-waiting-on-ravi) |
| Find a fact (access, limits, paths) | [§7 Environment facts](#7-environment-facts-verified) |
| Avoid a trap that already cost hours | [§8 Traps](#8-traps-that-already-cost-hours) |

---

## 1. Production incident — the rules that came from it

**On 2026-09-23 the automation took production down.** ~14 agents were run in parallel, all
pointed at the live API: 18 headless browsers sweeping 146 routes, a latency sweep of 20 samples
across 58 endpoints, and SQL returning 5,000+ rows.

The API is capped at **`containerConcurrency: 10`, `maxScale: 2` = 20 concurrent slots**. Every
slot filled; the dashboard's own requests queued behind the automation's; users got
`backend_down`. Codex later raised maxScale 2→4 (p99 18.44s → 8.72s), but the cause was the
automation.

**Rules, now enforced in code (`run-oci.sh`), not by anyone's memory:**

- A **flock run lock**. A second sweep refuses and exits 2. It does not queue.
- A **refusal to start if any browser is already running**. Stopping a parent agent does NOT stop
  its browsers — they outlive it, which is how the first attempt to stop this did nothing.
- **Concurrency capped at 4**, 150ms between requests, well under the 20-slot ceiling.
- **`statement_timeout=15s`** on every query.

**Current state: both OCI timers DISABLED, cron deleted, zero browsers on either machine.**
Nothing runs on a schedule. Do not re-enable without Ravi, and when you do, run **one supervised
cycle** you both watch — not a timer.

---

## 2. The contract — Ravi's standing instructions

Full text: `scratchpad/CONTRACT.md` in the session that wrote it; the substance is here.

**§0 An example is never the scope.** When he reports a bug he is showing ONE INSTANCE of a
CLASS. "Fix that page" is never the job. Every check sweeps every route (146), both viewports
(1440 **and** 390), every tab and sub-tab, every modal/drawer/sheet/popover, every Edit and row
action, L1/L2/L3, and the Android screens. A check that only runs where the example came from is
not done. *He had to say this four times: partitions, flicker, tab-reload, and again for "all
fixes since August".*

**Cover everything since 2026-08-01** — every commit, fix and feature. Backend AND frontend AND
Android. Not a sample.

**"Covered" means the check FAILS when the fix is reverted.** Nothing weaker counts.

**NO FALSE POSITIVES.** A check that fires on a correct page is worse than no check. Never fix
noise by raising a threshold, deleting the check, or exempting a page — fix the measurement.

**No unearned verdicts, either direction.** A check that did not run must not render a verdict.
A check must not report pass without attempting what it claims to prove. And silence is not
acceptable either — "not checked" must say so.

**Where data may be touched:** reads → `goatos-stg` read-only. Writes → throwaway DBs on the OCI
box only, cleared out after. **Never** write to `dashboard.mesha.sg`, `api.goatos.mesha.sg` or
`stg-api.dashboard.mesha.sg` — stg IS production data. Android: dev flavour only.

**Don't waste OCI space.** Drop every throwaway DB/table/snapshot and verify it is gone.

**Findings** name the page, the device, and what a person sees. No selectors, property names,
field paths, status codes, SQL or check codes. Flicker needs a GIF or filmstrip — a still cannot
show it.

**Process:** ONE branch (`automation/lane-fixes`), ONE PR. Never `gh pr merge`, never push to
main, always `make land-main` with a green receipt. Skills are an INDEX with triggers, not a
dumping ground. Don't increase CI time — relevant checks only; a docs change must stay instant.

---

## 3. Coverage: the real number

**4% of commits since 2026-08-01 are proven covered.** Not 100%.

The root cause was in the guard's own code: to count as "covered" an entry needed a status, a
route the sweep visits, and one interaction listed. **Nothing required that an assertion could
fail.** "Covered" meant *"the sweep visits this page"*.

The assertion engine implemented four operators — `visible`, `absent`, `count`, `url`. **There
was no way to state an expected value.** So a check for *"Spend share only shows the feeds the
farm buys"* asserted the chart's **heading** was visible; put every wrong feed back in the pie
and it passed.

Proved by planting bugs, not by arguing:

| planted | result |
|---|---|
| 18 wrong-value API payloads | **18 passed** |
| 24 data bugs | **16 passed** |
| 29 broken vaccination payloads | **29 accepted** |
| 276 weighing/vaccination assertions vs a **blank page** | **178 reported green** |
| `vaccination-plan-edit` vs an empty screen | **17/17 passed** |
| a page whose figure BOXES drew but whose figures did not, counted | **passed as a stable summary** |

**THE WAYS A CHECK PASSES AGAINST NOTHING, because the list keeps growing.** Read this before
trusting a green sweep. Each one was found by asking what a check does when it is handed nothing,
not by reading the check and agreeing with it.

1. **The page drew nothing and the checks are defect FINDERS.** Colliding labels, overpainting
   cells, leaked raw text — a page with nothing on it has no defects to find, so every finder is
   silent and the silence reads as clean. This is why a blank-page gate runs BEFORE the first
   detector rather than beside it.
2. **The check could not RUN, and that was recorded as the page being empty.** A blocked script or
   a navigation mid-measurement returned nothing, which looked exactly like a page that drew
   nothing — and that is a finding *against* the page. A measurement that could not be taken is
   "not checked", never an accusation. The collector now stamps proof that it ran.
3. **Two readings of a figure that was never drawn AGREE, and agreeing is the pass.** Nothing
   equals nothing. Refused now in two places on purpose (see 5).
4. **The figure's BOX drew and the figure did not.** The newest one, and the least obvious: a
   summary counted by how many figures are on screen counted the empty boxes instead, got the same
   answer twice, and reported a stable summary for a screen with no figures on it. The general
   comparison could not catch it — by the time it sees the pair they are two equal numbers, and
   nothing in them says they were empty boxes.
5. **Two defences, deliberately redundant, and neither is decoration.** The shared comparison
   refuses two readings that found nothing, which protects every caller including ones written
   later. The individual check refuses an empty reading before the comparison is asked, which
   protects it whichever version of the shared code is present — not hypothetical, the two landed
   in separate commits and the branch carried one without the other for a while. Only the second
   can know that a count of empty boxes is not a reading of figures. Deleting either leaves a real
   hole, and deleting either fails a test.
6. **A count of something is not a measurement of it.** Every number above is now counted from what
   was READ, and a source that could not be read is an error rather than a zero — a listed-but-
   unread input contributed nothing silently, and in one case made a check recommend MORE work as
   safe than was true.

**The definition is now fixed.** `covered` requires a discriminating reference — one that can
fail while the page still loads. Third status `smoke-only` added. Honest recount:
**87.2% → 5.6%** (109/125 → 7/125); **102 entries moved to `smoke-only`**. Nothing deleted, only
relabelled to what it always was. Every feature area is at **zero**; the seven survivors are
repeat-bug detectors that run on every page.

Per-domain audits (independent, mutation-proven): feed/sales/procurement **8%** against a ledger
claim of 90%. Weighing/vaccination **18.8%**, raised to 23.0% by work done.

---

## 4. The five lanes

| Lane | What it does | Runs | Proven |
|---|---|---|---|
| 1 browser sweep | 146 routes × 1440/390, layout + regression checks | nightly (now disabled) | partly — see §3 |
| 2 data sanity | read-only SQL on `goatos-stg` | default on | **17 real findings on production** |
| 3 API contracts | GET-only shape + p95 latency, 58 endpoints | default on | **12 real findings** |
| 4 write journeys | 9 journeys on the OCI clone, snapshot→write→assert→restore | default OFF | DB half yes, screen half 1 of 9 |
| 5 Android | 47 journeys, Firebase Test Lab virtual | default OFF | **0 of 47 — harness works, lane proves nothing** |

**Lane 2's live findings (real, on production data).** Numbers below are the ones with a stored
receipt in `scratchpad/lane2-run/data-sanity.json`; an independent verification pass found several
figures quoted in conversation were wrong, so trust these and re-measure before quoting onward:
139 upcoming vaccinations for animals already sold or dead (**re-measured 145**, not 144); 4 feeds
issued in greater quantity than ever purchased (one item **1,076 kg issued, 0 purchased**); sales
screen 701 sold vs herd register 155 (**re-measured 706 vs 160**, not 688); 500+ vaccination rounds
>2 days overdue; 90 vaccinations naming a pen the animal left.

**Lane 3's:** **Operations health returns HTTP 500 on 2 of 20 samples**, median 5,031ms, p95
15,214ms — the endpoint that reports whether event delivery is stuck is itself the least reliable
one. Leave-and-cover returns `items: null`. A sales deal returns a null animal count. The 12 real
findings have two agreeing sources (`scratchpad/slackrun/api-contracts.json` and
`docs/engineering/lane3-api-contracts.md:401`).

**The noise figure that belongs beside it, and §2 says it matters more than the 12:** lane 3's 62
history-mined checks, if enabled, produce **1,184 false findings against those 12 real ones** — a
99:1 ratio. They are parked behind `GOATOS_DASHBOARD_API_CONTRACTS_INCLUDE_HISTORY=1`. Do not
enable them without measuring first. Judge finding J-010 records the same shape for 57
history-derived `absent` assertions aimed at the alert channel.

**Lane 5's honest position:** its Kotlin could never launch the app on **any** virtual device —
`monkey -c LAUNCHER` exits -5 without starting it, so four cold-boot tests were reading the
**launcher home screen** and reporting *"the app closes itself as soon as it is opened"*. Fixed.
Coverage is 0 of 47 because everything past sign-in needs a backend it can reach.

---

## 5. Open work

**PR #375** `automation/lane-fixes` — everything automation. **NOT ready to land.**
- **Tests are GREEN**: 445 tests, 0 failures across `tools/dashboard-automation` and
  `apps/admin-web/scripts`, verified at PR head and mutation-tested (reverting the fix makes them
  red, so they bite). `origin/main` by contrast has 260 tests with **1 failure**, which this
  branch fixes. An earlier reading of "2 red tests" came from a **stale local ref** — always
  `git fetch` and check `origin/automation/lane-fixes`, not the local branch.
- **Blocker: `CONFLICTING / DIRTY` against main** in three files both sides changed —
  `.agents/skills/goatos-build/SKILL.md`, `.agents/skills/goatos-code-review/SKILL.md`,
  `tools/dashboard-automation/feature-assertions.json`. Another session edits the skills on main
  while this branch restructured them; resolve by content and keep both sides.
- **15 of its commits are authored `noreply@anthropic.com`** — `check-git-identity` correctly
  blocks. Needs an author rewrite; six worktrees share the ref, so do it when quiet.
- Note: 5 tests under `apps/admin-web/scripts` need `apps/admin-web/node_modules`
  (playwright, typescript) and throw `ERR_MODULE_NOT_FOUND` in a fresh worktree. Symlink the main
  clone's rather than reporting them red.
- Flicker + reload checks still only sweep 4 of 146 routes, phone-only, zero dialogs.

**PR #376** `fix/notification-feed-cte-pagination` — the site-slowness fix. Code pushed.
**`EXPLAIN` before/after and identical-results proof NOT done.** Not mergeable without them.
`DISTINCT ON` + keyset is where a faster query silently drops a row.

**Not started, nobody assigned:** edit forms / row actions / inline editors (named in §0, zero
coverage), modals as a class, L3 screens, flicker at 1440, tablet width, dark mode, empty and
error states, role/permission levels. Lane 2 has **never executed** in a real sweep — no receipt
shows one of its SQL checks running, so by §2 that is 0 proven, not 49.

---

## 6. Decisions waiting on Ravi

1. **Re-enable the timers?** Both disabled. Recommend one supervised cycle first, not a timer.
2. **`toolresults.googleapis.com`** on `goatos-stg` — free, one toggle, unblocks lane 5's Test Lab
   run. But note lane 5 needs a reachable non-production backend regardless; the API toggle alone
   buys little.
3. **`minmax(0,112px)`** at `mesha-theme.css:3244` — the min is zero, so the mobile pen-label
   column collapses. That is why pens render `C..` and `Castro 1` prints one letter per line. One
   line, his crushed-label complaint, not yet pushed.
4. **`check-write-scope.sh` is inert** — `.agent/scope.json` declares `allowed_paths: ["*"]` and
   the guard exits 0 on `"*"`. Changing it changes what agents may write.
5. **Rotate the OCI clone's `POSTGRES_PASSWORD`?** It reached a session transcript. Not in any
   commit. Rotation breaks `make land-main` on the laptop until the DSN is updated — it is one
   credential, confirmed.

---

## 7. Environment facts (verified)

- **OCI box:** `ssh goatos-oci` (key `~/.ssh/goatos_oci_dev_ed25519`; a bare
  `ssh opc@144.24.107.47` is refused and looks like missing access). Repo at
  `/home/opc/goatos-automation/goatos`; env at `/home/opc/.config/goatos/dashboard-automation.env`
  (0600 — load by path, never print). Always Free, 0.00 SGD.
- **OCI storage:** boot volume 100 GB. `total-free-storage-gb` says 100 available but the binding
  limit is the tenancy's `total-storage-gb` quota of **150** — so **50 GB was the real maximum**
  and a 50 GB volume is now attached at `/mnt/automation`, with the render directory moved onto
  it. `janitor.sh` prunes run dirs and drops throwaway DBs before every sweep.
- **The "read-only replica" is the PRIMARY.** `pg_is_in_recovery()` is **false** on
  `GOATOS_STG_READONLY_DATABASE_URL`, despite "readonly" in the name and "replica" in the runbook.
  Every SQL check runs against the database serving the product.
- **API:** `containerConcurrency 10`, `maxScale` now 4 (was 2), `minScale 1`. Deploy scripts still
  say 2, so a future deploy resets it.
- **Tokens expire and `/version` returns 200 unauthenticated** — a stale bearer produced a full
  page of false negatives. Verify auth against real tenant data before trusting "no findings".
- **Sheds and partitions, from the database** (the code comment is WRONG — it calls Yashoda
  unpartitioned): every shed is partitioned. Numeric → `Castro 1`, `Gandhi 2`, `Ho Chi Minh 1`,
  `Old Yashoda 3`, **`Yashoda 1`–`Yashoda 10`**. Worded → `Godel 1 - Part 3`, `Godel 2`,
  `Mandela 1/2`, `Sumathi 1/2`. `Yashoda` and `Old Yashoda` both exist, so never substring-match.

---

## 8. Traps that already cost hours

- **`make land-main` rebases**, so GitHub cannot auto-close the PR unless the rebased branch was
  force-pushed first. Three PRs sat looking unmerged.
- **Six worktrees share `automation/lane-fixes`.** HEAD advances under you and `git status` then
  shows siblings' commits as *staged reverts of their work*. Two were caught holding 27 and 18
  staged deletions of the PR's own files. Never `git add -A`; add by path.
- **`--self-test` proves nothing about code the dry-run path returns before reaching.** Three
  crashes shipped through a green gate that way.
- **A guard can be deleted and nothing notices.** Script + manifest + Makefile + CI step removed:
  `check-guardrail-registration` printed *"140 guards registered … all registered"*, and none of
  the 136 survivors noticed. **87 (guard, input) pairs across 29 guards exit 0 when their input
  file is deleted.** Both now fail closed behind a `GUARD-WEAKENING-ACK`.
- **A code comment is not a contract** — derive from the database or the rendered product.
- **camelCase hides from greps.** Every hand-written grep for the display-fallback bug used the
  snake_case field name; 5 real bypasses in camelCase were invisible for months.
- **`pgrep -f` matches your own command string** — it will tell you a sweep is running when it is
  your own `ssh` line.

---

## 9. Numbers in this doc: what is verified

An independent pass scored every headline claim: **13 verified, 7 wrong, 3 unreproducible,
3 prose-only** (repeated by this work's own writing, not independent evidence).

**Known wrong, corrected above:** the "re-measured" vaccination figure is **145**, not 144; the
sales reconciliation is **706 vs 160**, not 688 vs 160.

**Known unreproducible — do not quote:** the bug-cluster counts **564 proof/media, 356 verification
gate, 136 published-version-lock**. A later pass measuring commit *subjects* (one grep, one line,
no double counting) gives **332, 180, 67**, with pen/partition at **326** not 99. The larger set
came from matching commit *bodies*, which cross-contaminates: one Slack fix scores as a
verification fix, a proof/media fix and an idempotency fix because its body mentions all three.
**Publish the subject-only numbers and state the method.**

Before quoting any figure from this document onward, re-derive it. This session published numbers
that were true by their own definition and worthless in practice, which is the same failure the
coverage ledger made — see §3.
