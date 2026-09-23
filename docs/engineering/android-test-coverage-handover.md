# Android test coverage — measured position and handover

Written 2026-09-24, from `tools/dashboard-automation/commit-ledger.jsonl` (2,674
user-visible commits since 2026-08-01) and from the tree itself. Every number below
was re-derived for this page; none is quoted from another document.

Android work is **deferred, not cancelled**. This page exists so whoever picks it up
does not have to re-measure it first.

## The measured position

| | Android rows |
|---|---|
| rows in the ledger | **584** |
| `gap` — no test at all | **79** |
| `claimed` — a test exists, nobody has watched it bite | **505** |
| `covered` — proven by reverting the commit | **0** |

Repo-wide the ledger holds 2,674 rows: 2,337 `claimed`, 204 `gap`, 106 `covered`,
27 `smoke-only`. Android is 584 of those rows and **79 of the 204 gaps — the largest
single product gap on the branch.**

Of the 505 `claimed` Android rows, 441 ship their own test, 51 lean on a module test
and 13 on a sibling's.

## Which modules hold nothing

Counted as Kotlin files under each module's `src/test`. **9 of 22 Android feature
modules hold a test; 13 hold none.** For contrast, **62 of 73 `backend/internal`
modules hold a Go test.**

Empty feature modules, all at zero:

`feature-clock`, `feature-feed`, `feature-pccare`, `feature-pen-routines`,
`feature-pen-visits`, `feature-profile`, `feature-record`, `feature-scan`,
`feature-sheds`, `feature-submit`, `feature-timetable`, `feature-vaccination`,
`feature-workboard`.

`feature-feed` and `feature-pccare` are the two that should worry a reader most: the
ledger carries 27 feed and 29 pc-care behaviours, and neither module holds a single
test file of its own.

The nine that do: `feature-verify` (7 files), `feature-weighing` (6),
`feature-health` (3), `feature-counts` (2), and one file each in `feature-auth`,
`feature-calendar`, `feature-leadership-tasks`, `feature-toxin`, `feature-vendors`.

Core modules are better but not even: `core-data` holds 86 test files and
`core-network` 14, while `core-datastore`, `core-model`, `core-notifications` and
`core-testing` hold none.

Where the ledger names an Android module on a gap row, the gaps land on `scan` (2 of
14), `submit` (3 of 6), `sheds` (1 of 6) and `pen-visits` (1 of 4). The remaining
**72 of the 79 gaps carry no module attribution at all**, which is itself a finding:
those rows cannot be routed to an owner without opening each commit.

## Why zero are proven

`tools/dashboard-automation/prove-commit-ledger-reverts.mjs` is the only thing on this
branch that turns `claimed` into `covered`, and it matches Go and nothing else:

```js
if (!/^backend\/.*_test\.go$/.test(f)) continue;   // test files it will run
(files ?? []).filter((f) => /^backend\/.*\.go$/ ...) // files it will revert
```

There is no Kotlin path through it, so **no Android test on this branch has ever been
shown to go red when its commit is reverted.** That is a tooling gap, not a judgement
about the 505 tests — most of them are probably fine, and nobody can currently say
which.

## What it would take

1. **A Kotlin revert-prover.** The same three steps the Go one takes, which are what
   make a proof mean anything: run the module's tests at the commit and require them
   to PASS first (without that baseline an already-broken test reads as a proof);
   restore only the non-test files to their parent state, leaving the test files at
   the commit; re-run and require a named failure. The differences are mechanical —
   `./gradlew :<module>:testStgReleaseUnitTest --tests ...` instead of `go test`, and
   reading failed test names out of the JUnit XML under
   `<module>/build/test-results/` instead of stdout.
2. **Per-module test scaffolding for the 13 empty modules.** A module with no
   `src/test` has no test dependencies wired either, so the first test in each one
   costs a build-file change before it costs a line of Kotlin.
3. **A working `testDebugUnitTest` for the library modules.** Today
   `:core:core-data:testDebugUnitTest` fails at `processDebugUnitTestResources` —
   `style/WhiteBackgroundTheme` from androidx.test:core does not resolve — so all 86
   of those test files are unrunnable as things stand, and `make ci-local` runs
   `:app:testStgReleaseUnitTest` and nothing else on Android. **A test written into a
   core or feature module today does not run anywhere.** That is why the three
   Android tests added alongside this page live in `:app`.

## The repo rules any future Android test must obey

Carried here so they do not have to be rediscovered. All of these are enforced
somewhere — a guard, a decision doc, or a maintainer lock — and a test that violates
one is worse than no test.

- **Room is the single source of truth.** Every read screen renders from Room and
  refreshes in the background; a network-only read repository is banned. A read-screen
  test that asserts against an API fake rather than the Room flow is testing the wrong
  layer.
- **Refresh-on-open.** Read screens call `RefreshOnResume`; a retained ViewModel must
  never show data fetched once at creation.
- **~20-row keyset pagination, both layers.** Never `observeAll` / `SELECT *`, and
  never more than one screen-page fetched at a time. `make mobile-guard`.
- **Room schema changes ship a migration plus BOTH tests:** a schema-equivalence test
  and an upgrade-crash test that seeds an old-version file and reopens it. An `@Entity`
  added with no migration compiles, works on fresh installs, and crashes every upgrade.
  `make room-migration-guard`.
- **Compose row keys carry full identity** — work, category and period, not one id.
- **No client-wide proof or video caps.** Caps are per feature and per field.
- **`ProofMediaPreview` needs a stable `mediaIdentity`** keyed on proof/outbox/slot id,
  never on a temporary signed URL.
- **Farm language in user-facing copy.** No `V1`, `debug`, `outbox`, `payload`, `API`,
  `route` on any screen an operator reads.
- **Bounded memory:** no uncapped in-heap cache or accumulator.
  `make android-bounded-memory-guard`.

## What was closed before the deferral

Seven tests, each mutation-proven by reverting the production change and watching
exactly the intended test go red:

- `MarketTodayAliasTest` (4 tests) — pins `a13e0d41` (a dated refresh must not file
  another day under the today alias) and `78d405df` (a corrupt cached day is
  quarantined, not skipped).
- `MilkFeedingViewModelTest` / `MilkPreparationViewModelTest` (3 tests) — pin
  `bc2c6c43`, the `subject_type=park` the backend has never accepted.

That leaves **72 Android gaps open**, and 505 claimed rows still unproven.
