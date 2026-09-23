# The commit coverage ledger

**One row per user-visible commit since 2026-08-01, derived from `git log` — never hand-typed.**

| I need to… | Go to |
|---|---|
| Know why this replaced the old ledger | [§1](#1-why) |
| Read a row | [§2](#2-what-a-row-says) |
| Know what each status means | [§3](#3-the-four-statuses) |
| Re-derive the behaviour grouping | [§4](#4-the-grouping-rule) |
| Know what the guard catches, and what it cannot | [§5](#5-the-guard) |
| Turn `claimed` into `covered` | [§6](#6-proving-a-row) |
| See today's numbers | [§7](#7-todays-numbers) |

---

## 1. Why

The ledger this replaces was **125 hand-typed descriptions citing 6 commits**, against a standing
instruction to cover *everything* since 2026-08-01. **2,674 user-visible commits** had landed. The
denominator was wrong by three orders of magnitude, and nobody could see it — because a list
written by hand has no denominator at all. It can only report what somebody remembered; it can
never report what it is missing.

So the denominator now comes from git, and the rows are generated:

```bash
make commit-ledger-regenerate   # rewrite the rows from git log
make commit-ledger-guard        # fail if a user-visible commit has no row
make commit-ledger-prove        # revert a sample of commits, see which tests bite
```

Files:

| Path | What it is |
|---|---|
| `tools/dashboard-automation/commit-ledger.jsonl` | the rows, one JSON object per line |
| `tools/dashboard-automation/commit-ledger.json` | the summary, counted while reading the rows |
| `tools/dashboard-automation/commit-ledger-receipts.json` | revert receipts — the only source of `covered` |
| `tools/dashboard-automation/commit-ledger-lib.mjs` | the pure derivation (status, surfaces, grouping) |
| `tools/dashboard-automation/build-commit-ledger.mjs` | the generator |
| `tools/dashboard-automation/prove-commit-ledger-reverts.mjs` | the revert prover |
| `tools/agent-hooks/check-commit-ledger.mjs` | the guard |

**Scope.** `feat` and `fix` commits only, read off the conventional-commit type. Everything else
(`chore`, `docs`, `test`, `refactor`, `ci`, `build`, `perf`, `style`) is recorded as out of scope
and counted, not silently dropped — today that is **1,498 of 4,172** commits in range.

---

## 2. What a row says

```json
{"sha":"f216c968f","date":"2026-09-12","type":"fix",
 "subject":"fix(vaccination): keep legacy proof and merged batch progress honest",
 "behaviour":"backend/vaccination","area":"backend","alsoTouches":["vaccinationexecution"],
 "modules":["vaccination","vaccinationexecution"],
 "adminWebRoutes":[],"adminWebFeatures":[],"androidModules":[],"migrations":[],
 "touchesContract":false,"fileCount":4,
 "status":"claimed","claimTier":"ships-own-test",
 "evidence":["backend/internal/vaccination/app/generation_test.go"],
 "reason":"this commit changed its own test — not shown to fail on revert"}
```

Every field is derived from the commit's subject and the paths it touched. Nothing is typed by a
person, which is the point: two people regenerating get the same file.

---

## 3. The four statuses

| Status | Means | Source |
|---|---|---|
| `covered` | a **named check was demonstrated to fail** when this commit's non-test changes are reverted | a stored revert receipt, and nothing else |
| `claimed` | a named test plausibly covers it, and the row says which and on what evidence — **but nobody has watched it bite** | the repo's real test suite, indexed from tracked files |
| `smoke-only` | the browser sweep reaches a surface it touched and nothing there can fail | the sweep's own route catalogue and feature assertions |
| `gap` | no test in the repo touches anything it changed and no sweep reaches it | exhaustion, with a named reason |

**`covered` and `claimed` are deliberately not one status.** Collapsing them is precisely the
mistake the previous ledger made: it read 87.2% covered and was 5.6% on an honest recount, because
"covered" had quietly come to mean "the sweep visits this page". A test file sitting next to a
changed file is evidence that *a test exists*. It is not evidence that the test *notices* the
change — and when we went and checked, **38 of 144 conclusive attempts did not notice** (§7).

**`claimed` tiers**, strongest first, recorded on each row so the ledger is orderable by how much
its own claim is worth:

- `ships-own-test` — the commit itself changed a test file
- `sibling-test` — a file it changed sits in a directory that holds tests
- `module-test` — the module has tests, but not beside the files this commit changed
- `feature-assertion-value` — a browser feature assertion on this commit pins a value

**Every `gap` carries a named reason.** A gap with no reason is a number nobody can act on and
cannot be ordered worst-first, so the guard rejects one.

---

## 4. The grouping rule

Forty commits iterating on vaccination drive batching are **one behaviour with forty cases**, not
forty rows demanding forty tests. The grouping must be re-derivable, or it is a hand-written
ledger wearing a rule's clothes. It is:

```
behaviour = "<area>/<subject>"
```

- **area** — the layer the commit's files land in, in a fixed *precedence*:
  `admin-web > android > backend > migration > contract > tooling > docs`.
  Precedence, **not** "the most files": a commit that adds one screen and twelve backend files is
  still that screen's behaviour to the person who sees it, and a rule keyed on file count moves a
  row between groups when somebody reformats.
- **subject** — the single most specific thing that area names: the admin-web feature directory,
  the Android feature module, the backend module, or the conventional-commit scope when the area
  names nothing (a lone migration, a contract-only change).
- where an area names more than one subject, the **alphabetically first** is the key and the rest
  land in `alsoTouches`. Arbitrary, but *stable*; "the most important one" is an opinion.

2,674 commits collapse to **241 behaviours** this way.

---

## 5. The guard

`make commit-ledger-guard` — in `run_common`, ~200 ms (one `git log --format=%h`, no `--name-only`,
no tree walk). It is **not diff-scoped**, on purpose: a commit creates a row requirement by
*existing*, not by touching `tools/`.

**What it catches**

- a `feat`/`fix` commit on main since 2026-08-01 with no row
- a row for a commit that is not in that range (a hand-added row)
- a status outside `covered|claimed|smoke-only|gap`
- a `gap` with no reason
- `covered` claimed with no revert receipt naming the check
- `claimed` with no test named
- a duplicate row
- **the summary disagreeing with the rows counted while reading them** — the assertion that
  catches a ledger whose rows were edited and whose headline was not

**What it cannot catch — stated here rather than left for somebody to find**

- **whether a `claimed` row's test actually bites.** Only a revert receipt shows that, and
  receipts come from a separate sampling tool. The guard enforces that `covered` is receipted; it
  does not make anyone go and get more receipts.
- **whether a named test is a *good* test.** A test beside a changed file is evidence a test
  exists.
- **a commit that changed real behaviour under a `chore:`/`refactor:` subject.** Scope is read off
  the commit type, so a mislabelled commit is invisible here. That is a commit-message problem one
  layer up.
- **the cross-module chains.** A producer test and a consumer test can both pass while the wiring
  between them is broken (`AGENTS.md`: separate producer and consumer tests are not closure). No
  per-commit row can see that; it is owned elsewhere.
- **what the product actually renders.** Owned by the browser sweep.

**The guard's own blind spot, closed deliberately.** An empty commit range would pass *any*
ledger, including an empty one — the exact shape of guard this repo has been burned by (a guard
printed "539 files scanned" with the tree deleted). So the denominator is a required argument, an
unresolvable base ref is a failure rather than an empty set, and two of the ten self-test fixtures
assert that a ledger graded against nothing is refused.

---

## 6. Proving a row

```bash
make commit-ledger-prove SAMPLE=25
```

For each candidate: check the commit out in a scratch worktree → **run its package's tests and
require them to PASS first** (without this baseline, an already-broken test reads as "reverting
the fix broke it") → restore only the **non-test** files to the parent (reverting the tests too
would just delete the check) → re-run, and require a **named** test to fail.

Four verdicts, and three of them are not receipts:

| Verdict | Means |
|---|---|
| `proved` | a named test went red — a receipt is written, the row becomes `covered` |
| `disproved` | the tests **still pass** with the commit's code removed. A finding, not an absence: a test shipped beside a fix that does not notice the fix being undone |
| `inconclusive` | the package went red with **no `--- FAIL:` line** — a build break, not a failing check. Recorded as its own verdict precisely so it is never counted as a proof |
| `skipped` | the package did not pass at the commit, or the commit changed only tests |

**Known limit of `disproved`:** the prover runs only the packages whose *test files this commit
touched*. A behaviour caught by a test in a different package would read as `disproved` here. It
still means the commit's **own** shipped test does not notice the change, which is the thing worth
knowing — but it is not proof that nothing anywhere catches it.

---

## 7. Today's numbers

Measured 2026-09-24 against `origin/main`. Re-derive before quoting onward.

**4,172 commits** in range, of which **2,674 are user-visible** (`feat`/`fix`) and 1,498 are not.
2,674 rows in **241 behaviours**:

| Status | Rows | Share of 2,674 |
|---|---:|---:|
| `covered` (revert-proven) | 106 | 4.0% |
| `claimed` (a named test, not proven to bite) | 2,337 | 87.4% |
| `smoke-only` | 27 | 1.0% |
| `gap` | 204 | 7.6% |

**Claim tiers within `claimed`:** 1,716 `ships-own-test`, 566 `sibling-test`, 55 `module-test`.

**The revert sample is the number to read next to that 87.4%.** 220 backend `ships-own-test`
candidates were attempted: **106 proved, 38 disproved, 72 inconclusive (build break), 32 skipped**.
Of the 144 that reached a verdict either way, **26% did not notice their own commit being
reverted**. That is a measurement on 220 commits, not on 2,337 — do not extrapolate it as a
coverage figure, and do not quote it without this denominator. The 72 inconclusive results are
reverts that stopped the package compiling; a build break is evidence the test *references* the
changed code, but it is not a failing check, so it earns no receipt.

**Gaps, worst-first by behaviour:**

| Gaps | Of | Behaviour |
|---:|---:|---|
| 28 | 159 | `android/android` |
| 25 | 48 | `tooling/tooling` |
| 16 | 41 | `android/app` |
| 15 | 15 | `tooling/ci` |
| 12 | 12 | `other/deploy` |
| 10 | 10 | `tooling/android` |
| 6 | 6 | `tooling/guard` |
| 5 | 6 | `contract/contracts` |

**By reason:** 79 Android (module holds no test, no journey reaches it), 76 tooling/CI, 36 nothing
at all, 10 docs-only, 3 admin-web (no test, route not swept). The Android figure is the product
one: **9 of 22 Android feature modules hold a Kotlin test**, against 62 of 73 backend modules.

**Six modules can never be reached by a per-screen check** and are named as their own gap reason
rather than ordinary gaps: `countsbridge`, `sopbridge`, `notificationbridge`, `domainconsumer`,
`eventwiring`, `kernelstages`. They wire other modules together and carry no route, screen or
endpoint of their own. No commit in range landed in that reason bucket today — every commit
touching one of them also touched a directory holding tests — but the reason exists so that when
one does, it is not filed as "go write a screen check", which is impossible for them.
