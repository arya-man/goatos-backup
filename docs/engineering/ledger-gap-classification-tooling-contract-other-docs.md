# Ledger gaps outside backend, admin-web and Android — what they actually are

Written 2026-09-24 against `tools/dashboard-automation/commit-ledger.jsonl` on
`automation/lane-fixes`. Scope: the four non-product areas — **tooling 75, other 23,
contract 14, docs 10 = 122 rows marked `gap`.**

The bar throughout is the branch's own: **a row is closed only when reverting the
commit turns a named check red, demonstrated, not argued.** Rows that do not deserve
a test are named as such with the reason, because a test written to move a number is
the decorative coverage this branch exists to remove.

## Headline

Most of these are **not uncovered — they are unseeable.** The ledger's gap detector
looks for a separate test *file* accompanying a fix. Three whole classes of check in
this repo do not have one:

1. a guard script keeps its adversarial fixtures in a `--self-test` **inside the file
   it tests**;
2. a contract is checked by a `make` target that **regenerates and diffs**, with no
   test file anywhere;
3. a portability or wiring rule is enforced by a **shell doctor** over the whole tree.

All three are real checks that bite. None of them is a `_test.go`.

## Proved, with the revert done

| rows | area | how it was proved |
|---|---|---|
| **24** | tooling | `prove-guard-selftest-reverts.mjs` (added in this branch): self-test green at the commit in a worktree, rebuild as parent logic + this commit's `selfTest()`, require red. Receipts in `guard-selftest-revert-receipts.json`. |
| **12** | contract | spec + generated client pairs, covered by `make api-client-check` via `check-contract-drift.sh` in `ci-local`. |
| **1** | contract | `a985f5822`, the overlay-motion guard. |
| **1** | docs | `aa47cb093`, the clone-portable session ledger. |

**The contract proof.** Adding one path to `contracts/openapi/app-api.yaml` without
regenerating makes `make api-client-check` exit 2. The *other* direction does not hold
and is worth knowing: a hand edit to a generated file is silently overwritten by
regeneration and never reported, so that check catches a stale client, never a doctored
one. The 12 rows are all "spec moved, client regenerated", which is the direction it
covers.

**The overlay-motion proof** bites in both directions, which is the property worth
having. Dropping its suppression check makes it flag correctly-suppressed CSS (two
fixtures red); making it never report makes the 2026-08-06 regression fixture red. A
guard that only proves it can stay silent has not been tested.

**The docs proof.** `aa47cb093` removed a hardcoded repo root from a committed ledger.
Putting that exact line back makes `ai-doctor` print
`BREAKER (doc, repo-root path hardcoded)` and FAIL. The control existed before the row
did; the commit message even says so.

## Real gaps — logic changed, nothing shipped to notice

**10 rows**, verdict `no-fixture` in the receipts: `49baf4cc5`, `f8e91a6bf`,
`5cc4cf577`, `040731438`, `94920fb0b`, `0bbb9bc48`, `954bd2fd1`, `09eb4b1f1`,
`743d55f53`. These are the honest remainder of the tooling pile.

Three of them (`94920fb0b`, `0bbb9bc48`, `954bd2fd1`, all on
`check-android-runtime-permission-sdk-gates.mjs`) were attempted and **deliberately not
closed**. Fixtures were written for all three shapes, all three passed, and no mutation
could be found that only the new fixture caught: the behaviours are enforced
structurally by the brace-stack in `notificationLineIsSdkGated`, not by a separable
branch, and the one mutation that did bite (`goodBraceOnNextLine`) was already caught by
the pre-existing `goodMultilineCondition`. A fixture that passes for a reason unrelated
to the rule it names is worse than no fixture, so they were reverted rather than landed.

That attempt did turn up two live findings in that guard, filed separately: it reports a
**false positive** on a correct gate whose condition opens with a helper call
(`if (shouldAsk() && Build.VERSION.SDK_INT >= 33)` is flagged; swapping the operands is
clean, because the condition regex stops at the nested call's `)`), and
`parseIfConditionFromWindow` is **dead** — deleting the call leaves every fixture and
every shape tried unchanged.

## Not worth a test, with the reason

**The 12 `other/deploy` rows.** Four of them are literally the same failure — the STG
Cloud Build invoked a tool the runner had not installed (`make`, JDK 21, a headless JDK,
the Cloud Run proxy) — which looks like the textbook case for a static guard. It is not,
and the reason matters: deciding whether a tool is missing needs to know what each base
image already ships. `gcr.io/google.com/cloudsdktool/google-cloud-cli:slim` provides
`python3` and `gcloud`, and `tools/deploy/stg-mobile-distribution.sh` uses `python3`
without the step installing it — correctly. A guard without that table would flag it,
and a guard with one carries a hand-maintained list of image contents nobody can verify
from this repo, which drifts and then accuses correct config. That is the direction that
teaches people to wave a guard through. The real control is that the build fails loudly,
which it did, four times, each fixed the same day.

**`37f1b9835`, `844057c61`, `89a63a47f`** — progress notes, restored strategy docs, and
a review write-up. No behaviour changed in the repo; there is nothing to revert.

**`5231a0134`, `51ea40c23`, `8033902bf`, `5a8570726`** — mock HTML. The mock is the
*specification* admin-web is checked against by `check:mock-fidelity`. Writing a test
that asserts the mock says what the mock says is circular; the check that matters
already runs in the other direction.

## The two shapes the prover cannot reach

**`no-baseline` (2 rows: `812acaf9e`, `e19044c05`).** The guard did not parse at that
commit — an unterminated regex literal, repaired one commit later by `954bd2fd1`. A
broken guard shipped and passed review. Nothing can be proven by reverting a commit
whose baseline is already red, and the fix that mattered is the one with no fixture.

**`skipped` (9 rows).** A guard introduced fresh in that commit: there is no parent to
revert to. These ship a `--self-test` and a `guardrail-manifest.json` registration, so
they are covered by construction rather than by revert — but "covered by construction"
is a weaker claim than the rest of this page and is not counted as proved.


## The shell half: 9 rows are commits whose entire content is a test

`prove-shell-test-reverts.mjs` does the same three steps for a commit that ships a
`*.test.sh` beside the script it tests. Over the 16 such pairs in these areas:
**0 proved, 0 disproved, 9 test-only, 6 unpaired, 1 no-baseline.**

**`test-only` is the interesting verdict and it is a classifier finding, not a coverage
one.** Nine of these rows — `6a77f7a5a`, `cebb0d899`, `d7cf14c78`, `a726bd98e`,
`d1f80e498` (×2), `42622280b`, `6ece9efd7`, `71e2bdb8f` — changed nothing but test
files and progress notes. They are *test-hardening* commits. The gap detector counted
each as a behaviour that needs a test, which is backwards: the commit IS the test. Every
one of them inflates the gap number by one.

**The first run of this prover produced five `disproved` verdicts and every one was
false.** It had reverted one guard and then run a *different* guard's test — which of
course passed, and read as "the test does not notice its own fix". `d548ffd2e` changes
`check-android-screenshot-proof.sh` and was judged by `check-ci-base-provenance.test.sh`;
`985ca28b9` changes the worktree lock and was judged by the push-hook test. Two rules
were added and are commented in the file: a test only runs against the script it names,
and a commit whose only non-test change is markdown has no fix to revert. After them,
**nothing in this set is disproved.** It is recorded here because the failure mode —
an assertion that matches a family rather than the thing it is about — is the one this
branch exists to find, and the prover fell into it first.

`5ad4594fc` is `no-baseline` for an honest reason: the gradle-worktree-lock test scopes
itself to developer machines and self-hosted runners (that scoping is `985ca28b9`), so a
detached temp worktree is not an environment it answers in.

## Where the 122 stand

| | rows |
|---|---|
| proved by revert | **38** |
| test-only — the commit is the test, counted as a gap in error | **9** |
| not worth a test, reason recorded | **19** |
| real gaps still open | **10** |
| unreachable by revert (new guard, no baseline, unpaired) | **~18** |
| not yet classified (herd-signals seeds, phone-QA scripts, AI setup) | **~28** |

The three attempted-and-refused fixtures are inside the 10.

## Running it

```
node tools/dashboard-automation/prove-guard-selftest-reverts.mjs --self-test
node tools/dashboard-automation/prove-shell-test-reverts.mjs <sha>:<test path> ...
node tools/dashboard-automation/prove-guard-selftest-reverts.mjs <sha>:<path> ... \
  --receipts=tools/dashboard-automation/guard-selftest-revert-receipts.json
```

The prover's own self-test is mutation-proven: restoring naive brace counting for
`selfTestBlock` turns all five of its checks red, because these guards are full of regex
literals containing `{` that a counter reads as an unclosed block — the bug that silently
reported "no selfTest()" on four rows before it was found.
