# Verification & Coverage — what makes a check, a lane, or a proof mean anything

Load this chapter when the diff touches a test, a check, a guard, a lane, a smoke
run, a receipt, a coverage ledger, or any artifact that reports pass/fail —
`tools/ci/**`, `tools/agent-hooks/check-*`, `tools/dashboard-automation/**`,
`apps/admin-web/scripts/**`, `**/*.test.*`, `**/*_test.go` — and on any review
that is asked to trust a green result.

Everything here is a reviewer rule. Each is stated as reject/require, with the
instance that cost us the hours.

---

## 1. An assertion that checks existence does not cover correctness {#assertion-strength}

**Reject** a check whose strongest assertion is `visible`, `text contains`,
`count >= 1`, or `absent`. **Require** at least one assertion that compares a
produced value against an independently expected value — a number to a number, a
set to a set, an ordered list to an ordered list.

Instance (2026-09-23): all **928** web assertions in the automation lanes were
existence-shaped; **zero** compared a number to an expected number. The commit
`6fbc825f2` "Spend share only shows the feeds the farm buys" is covered by an
assertion that the chart's *heading* is visible. Put every feed back into the pie
and that assertion still passes.

Reviewer test: name the value the check would have to see change before it fails.
If the answer is "the element would have to disappear", it is an existence check
wearing a correctness check's name.

## 2. "Covered" means the check fails when the fix is reverted {#covered-means-revert-fails}

**Reject** any coverage claim backed only by a mapping — a commit with a check's
name written next to it. **Require** the revert proof: revert the fix (or apply
the pre-fix behaviour to a fixture), run the named check, watch it go red.

Instance: the old ledger called **99** partition commits "covered" while no check
in the suite ever looked at a pen label. The number was true by its own
definition and worth nothing. This is the same rule as the repo-wide
failing-before regression rule in `goatos-build/SKILL.md` — applied to coverage
ledgers, not just to a single fix.

## 3. A check that did not run must never render a verdict — in either direction {#no-unearned-verdict}

**Reject** any pass **and** any finding produced by a path that did not attempt
the operation it claims to judge. The only correct output of an unattempted check
is `not-attempted`, naming what blocked it.

Five instances in one night:
- a lane reported "People are signed out at random" when the app never launched;
- two tests passed while the page under them was an error screen;
- a lane accused the product of losing a plan that nothing had asked it to save;
- three journeys reported a business rule holds while never attempting the
  operation that rule forbids.

Reviewer test: for each verdict the run emits, find the line that performed the
action. If the action is conditional, find the branch that reports
`not-attempted` when the condition is false. A missing `not-attempted` branch is
the finding.

## 4. Silence is also a verdict {#silence-is-a-verdict}

**Reject** a run that can skip everything and emit nothing. **Require** that a run
where every check skipped says so, loudly, in the same place the findings would
have gone. "No alert" is read by everyone as "fine".

## 5. Never fix noise by raising a threshold, deleting the check, or exempting the page {#never-weaken-a-check}

**Reject** a noise fix that moves a threshold, removes an assertion, or adds a
page/route/selector to an exemption list. **Require** the measurement be fixed
instead — measure the thing the finding was actually about.

A check that fires on a correct page is worse than no check, because it trains
everyone to ignore it. A check that stopped catching the real thing is worse
still, because it reports green. This is the same rule as contract §3; here it is
the reviewer's version: when a diff makes a check quieter, the diff must show why
the *measurement* was wrong, not why the *result* was inconvenient.

## 6. `--self-test` proves nothing about code the dry-run path returns before reaching {#self-test-branch-blindness}

**Reject** a gate whose self-test can only reach one branch while presenting
itself as proof of the whole script. **Require** the gate state out loud which
branches it exercises and which it cannot.

Instance: `GOATOS_DASHBOARD_SLACK_DRY_RUN=1` returns before any upload, so the
self-test never entered `postSlack`. Three separate crashes shipped through that
green gate — the upload catch block could throw again and kill the notifier
(`2ccbc5bb7`), alerts posted with no screenshots because `postSlack` crashed
(`3db2925c4`), findings never carried their link (`83182e472`).

Reviewer test: read the dry-run/self-test entry point and find its first early
return. Everything after it is unproven by that gate.

## 7. A loader that cannot find its input must fail loudly {#loader-must-fail-loud}

**Reject** a reader, loader, or shape-adapter that returns empty, `null`, or a
default when its input path is missing. **Require** a named, non-zero failure
that says which file and which key.

Instance: `tools/dashboard-automation/check-data-sanity.mjs` read `extra.lane2`
while the rows live at `lanes.lane2.checks`. **112** checks vanished into one
tidy line reading "the derived check file could not be read", and the run
looked clean.

This is the committed-loader rule already stated for fixtures in
`goatos-build/SKILL.md` (`Decoder.DisallowUnknownFields()`), applied to the other
direction: unknown key in, missing key out — both are hard errors.

## 8. Prove a restore by content fingerprint, not row count {#restore-by-fingerprint}

**Reject** a backup/restore/reinsert/migration proof whose assertion is a row
count. **Require** a content fingerprint — a checksum or ordered digest over the
restored columns, compared to one taken before.

Instance: a generated column broke a reinsert. Same row count, different data.
The row-count check called it green.

## 9. An expired token can return 200 {#auth-before-no-findings}

**Reject** "no findings" from any lane whose only liveness probe is an
unauthenticated endpoint. **Require** the run prove authentication by reading
real, authorization-gated data before it is allowed to report an empty result.

Instance: `/version` is unauthenticated, so a stale bearer token produced a full
page of false negatives. The stored bearer on the box expires silently
(contract §8).

## 10. A code comment is not a contract {#comment-is-not-a-contract}

**Reject** an expectation derived from a comment, a doc string, a constant's
name, or a fixture's prose. **Require** it be derived from the database or the
rendered product.

Instance: `apps/admin-web/lib/operational-location.ts` documents shed "Yashoda"
as unpartitioned; the database says it has partitions 1-10. Every check built on
that comment was checking the comment.

## 11. An example is never the scope {#example-is-never-the-scope}

**Reject** a fix or a check that covers only the page, route, or record the
report named. A reported bug is **one instance of a class**; the deliverable is
every place the class can occur.

The sweep, for any UI-visible class: every route lane 1 knows about at **1440
and 390**; every tab and sub-tab; every modal, drawer, sheet and popup; every
Edit / row-action / inline editor; L1, L2 and L3 (list, detail, and the screen
reached from the detail); and the Android screens for the same journey. If part
of the surface cannot be reached, the review says which part and why — it never
silently narrows.

Corrected only after the maintainer repeated himself: pen/partition labels
treated as "partitions" instead of every label on every screen; flicker treated
as "the Tasks filter bar" instead of every overlay on every page; full page
reload treated as "Health Config" instead of every tab, modal, edit and in-app
navigation control in the product.

## 12. Deep-linking never exercises L2 or L3 {#deep-link-never-exercises-l2}

**Reject** a sweep that reaches every route by URL and calls the surface covered.
**Require** the journey navigate the way a person does — tap the row, open the
drawer, switch the tab — for anything that only exists after an interaction.

Instance: `.navback` is rendered only after tapping a row, so a sweep that
deep-links every route had never once seen it, and never could.

## 13. A rebase orphans the PR {#rebase-orphans-the-pr}

`make land-main` rebases onto fresh `origin/main` before it pushes, so the
commits that reach `main` are **not** the commits on the PR branch. GitHub cannot
auto-close the PR unless the rebased branch was force-pushed first.

Instance: three PRs stayed open, looking unmerged, after their work was already
on `main`. Force-push the rebased branch before landing, or close the PR
explicitly and say which SHA carried it.
