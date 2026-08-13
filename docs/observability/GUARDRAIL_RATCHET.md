# Guardrail whole-tree ratchet

> Status: **enforced** (local CI, `make ci-local` runs the whole-tree ratchets
> directly from `run_common` in `tools/ci/run-local-ci.sh`, plus the
> diff-scoped `exception-guard`/`telemetry-guard` per-component; the
> `guardrails` Makefile target is a separate hand-run aggregate, not the
> path `make ci-local` uses) · Owner: platform/observability
> Driver: `tools/ci/ratchet-guard.py`. Configs: `tools/ci/ratchet-guard.exception.json`,
> `tools/ci/ratchet-guard.telemetry.json`. Baselines: `tools/exception-guard/baseline.json`,
> `tools/telemetry-guard/baseline.json`.
> Companion doc: `docs/observability/TELEMETRY_GUARDRAILS.md` (what the underlying
> guards check and why they're diff-scoped).

## Incident: presence-only keying let debt stack silently (fixed — do not revert)

The ratchet originally keyed each baseline entry on **presence** of
`file|kind` (or `file|surface`) — a plain list of strings, no count. An
adversarial review reproduced three defeats against that scheme:

1. **Stacking was invisible.** A file already baselined for a kind (e.g.
   `swallowed_err`) could receive an **unlimited number of additional**
   violations of that same kind in the same file and the ratchet still
   printed PASS, because the key `file|kind` already existed in the
   baseline regardless of how many times it occurred. Concretely: 3 new
   swallowed errors were added to `backend/cmd/counts-workbook-source-scan/
   main.go`, which already had 1 baselined `swallowed_err` (so the real
   count became 4) — `exception-guard-ratchet` still exited 0.
2. **`--regenerate` laundered it.** Regenerating the baseline after stacking
   new debt onto an already-baselined file produced a baseline with an
   **identical entry count and a zero-line `git diff`** — a reviewer looking
   at the baseline diff on the PR saw nothing at all.
3. **The diff-scoped guard was never wired into `make ci-local` either.**
   `make exception-guard` (the diff-scoped, non-ratchet check) existed only
   inside the Makefile's `guardrails` aggregate target, which itself was
   never invoked from `tools/ci/run-local-ci.sh`. So neither the diff-scoped
   check nor the whole-tree ratchet caught stacked-in-file debt through
   `make ci-local` — the presence-only ratchet was the only whole-tree
   backstop, and it was blind to exactly this failure mode.

**Fix:** the baseline is now keyed on `(file, kind)` → **integer count**, not
presence. A file fails the ratchet the moment its current count for a
`(file, kind)` exceeds the baselined count — see `tools/ci/ratchet-guard.py`
(`extract_keys`, `load_baseline`, `write_baseline`, the `increased`/
`decreased` comparison in `main`). `--regenerate` now prints an explicit
`+NEW` / `+MORE` / `-LESS` / `-GONE` delta so a regeneration that admits new
debt is visible in the command output, not just (possibly) in a JSON diff.
`tools/ci/run-local-ci.sh` now also runs the diff-scoped `make
exception-guard` directly in `run_common` (previously only reachable via the
unwired `guardrails` target).

**A content-fingerprint per violation (hash of the offending line/AST node)
was considered and rejected** in favor of counting. Fingerprints are more
precise in theory — they could in principle tell "this exact violation is
still here" apart from "a different violation of the same kind appeared" —
but they are fragile in the ways that matter for a ratchet: two unrelated
swallowed-errors written identically hash to the same fingerprint (collision
across genuinely different violations), and an unrelated refactor near the
line (renaming a nearby variable, reformatting, wrapping) shifts the token
stream or line anchor even though the flagged violation didn't move,
producing the exact same false-new/false-vanish churn that ruled out plain
line numbers above. A count-per-`(file, kind)` has none of that churn
sensitivity, is trivial to reason about in a baseline diff, and catches the
one thing a ratchet actually needs to catch: **more debt of a kind landing
in a file than was there before.** It does not tell a reviewer *which* line
in a multi-violation file is the new one — the guard's own diff-scoped run
plus `git diff` on the PR is what answers that.

**Do not revert this back to presence-only keying.** If you are tempted to
simplify the baseline back to a bare list of keys "to make the JSON
smaller," you will reopen exactly the three defeats above.

## The lesson (read this before adding another diff-scoped guard)

`exception-guard` and `telemetry-guard` are, by design, **diff-scoped**: they
only inspect lines a diff actually adds/touches (`origin/main...HEAD`,
falling back to `HEAD~1...HEAD`). That's correct for the PR path — a guard
that failed on every pre-existing violation in the repo would fail on day one
and someone would switch it off under deadline pressure.

But diff-scoping has a structural blind spot that is easy to miss: **a
diff-scoped guard silently permits unlimited pre-existing debt.** Its `--all`
whole-tree sibling (`exception-guard-audit`, `telemetry-guard-audit`) existed
the whole time and could see that debt — but nothing ever ran it as part of
`make ci-local`. An adversarial whole-tree audit of this repo found ~150
`exception-guard` FAILs (Go + Kotlin combined) and 51 `telemetry-guard` FAILs
(+24 WARNs) sitting in the tree with a permanently green CI. **A guard that
cannot fail is documentation, not enforcement.**

This is not specific to these two guards. Any future diff-scoped guard has
the identical blind spot unless it is paired with a whole-tree ratchet like
this one. If you add a new diff-scoped guard to this repo, either:

- pair it with a ratchet the same way (cheapest: reuse `tools/ci/ratchet-guard.py`
  with a new config — see "Adding a new ratchet" below), or
- explicitly document in its own doc why whole-tree debt genuinely cannot
  exist for that guard (rare — most checks that scan file content can find
  pre-existing violations).

## Why not just turn on `--all` and require zero?

Because day one it would fail on ~150-200 pre-existing violations across two
guards, and the fix (temporarily disabling the gate) is worse than the
original problem — it trains people to route around red CI instead of fixing
it, and the gate tends to never come back. The ratchet gets the same
end-state enforcement (nothing gets in the tree that resembles the
pre-existing debt) without that failure mode.

## The mechanism

1. **Baseline.** Today's known violations are frozen into a committed JSON
   file — one per guard, `tools/exception-guard/baseline.json` and
   `tools/telemetry-guard/baseline.json`.

2. **Stable keying — file + rule-kind, never line number — with a COUNT, not
   just presence.** Each baseline entry's key is `"file|kind"`
   (exception-guard) or `"file|surface"` (telemetry-guard), e.g.
   `backend/internal/foo/bar.go|discarded_err`, mapped to an **integer count**
   of how many findings of that kind the file had when the baseline was cut
   (`{"entries": {"file|kind": N, ...}}` — see `write_baseline` /
   `load_baseline` in `ratchet-guard.py`). Line numbers are deliberately
   excluded from the key: they churn on every unrelated edit to a file (an
   import added three lines above a violation shifts its line number), so a
   line-keyed baseline would desync from reality on the very next commit —
   either false-passing (the violation is still there, but the line number
   baseline no longer matches, so it silently "disappears" from tracking) or
   false-failing (an unrelated, unrelated-to-the-violation edit shifts the
   line and the ratchet reports it as brand new). File+rule-kind is stable
   across any edit that doesn't change the violation itself. The count
   exists because file+rule-kind presence alone is not enough — see the
   "Incident" section above for the stacking defeat this closes.

3. **Check mode (`make exception-guard-ratchet`, `make
   telemetry-guard-ratchet`, both run directly from `run_common` in
   `tools/ci/run-local-ci.sh`, so every `make ci-local` invocation hits them
   regardless of which component job is selected).** Runs the guard's
   `--all --json` mode, filters to `severity == "FAIL"` (WARN-mode surfaces
   stay non-blocking — that's the underlying guard's own per-surface config,
   unrelated to the ratchet), counts findings per `(file, kind)`, and
   compares against the baselined counts:
   - any current `(file, kind)` **not** in the baseline → **FAIL** (new debt
     in a file that had none of this kind before).
   - any current `(file, kind)` whose count **exceeds** the baselined count
     → **FAIL** (new debt stacked onto a file that already had some — the
     case presence-only keying missed; see "Incident" above).
   - any `(file, kind)` whose count **dropped** below the baselined count →
     **FAIL** ("stale-high" baseline — debt was fixed, but the baseline was
     never shrunk to match). Combined with the point above, this is what
     makes the ratchet **shrink-only in both directions**: without it,
     fixing violations and leaving the baseline at its old (higher) count
     would silently create "budget" to reintroduce an equivalent violation
     elsewhere without ever tripping the "new/increased debt" checks.
   - any baseline `(file, kind)` **not** reproduced at all by the current
     scan → **FAIL** (same stale-high case, the whole key vanished rather
     than just shrinking).
   - Every run — pass or fail — prints the outstanding debt count as both a
     violation total and a key count (`sum(current_keys.values())` across
     `len(current_keys)` keys), so the backlog stays visible rather than
     forgotten. It is intentionally **not** part of the pass/fail decision by
     itself (a nonzero backlog is expected and tracked separately, e.g.
     `TELEMETRY_GUARDRAILS.md` §3.2's TODO list) — only *drift* in either
     direction fails the gate.

4. **Regenerate mode (`--regenerate`).** Rewrites the baseline file to match
   the current whole-tree scan exactly. Use this after genuinely fixing
   violations (baseline shrinks) or after a legitimate marker/exempt change
   alters the violation set:
   ```bash
   make exception-guard-ratchet-regenerate
   make telemetry-guard-ratchet-regenerate
   # or directly:
   python3 tools/ci/ratchet-guard.py --config tools/ci/ratchet-guard.exception.json --regenerate
   python3 tools/ci/ratchet-guard.py --config tools/ci/ratchet-guard.telemetry.json --regenerate
   ```
   Commit the resulting baseline file alongside the fix.

## What is explicitly NOT an accepted way to land code

**Adding a new entry to a baseline file so that a change you just made stops
failing the ratchet.** The baseline exists to freeze *pre-existing* debt at a
point in time, not to be a manually-editable allowlist for new violations.
If `make exception-guard-ratchet` or `make telemetry-guard-ratchet` fails on
your change, the correct responses, in order of preference, are:

1. Fix the violation (add the `CrashReporter.recordException(...)` call, wrap
   the Go error, wire the missing analytics/Faro marker).
2. If the finding is a genuine false positive or a case with nothing to
   record, add the guard's own escape hatch — `// exception:exempt <reason>`
   or `// telemetry:exempt <reason>` — with a real reason. This changes the
   underlying guard's finding set (the violation goes away entirely), which
   is a legitimate reason to regenerate the baseline afterward.
3. If genuinely stuck, ask a human — do not hand-edit the baseline JSON to
   add your new violation's key.

`--regenerate` is a *shrink* operation in spirit even though the CLI doesn't
enforce monotonic shrinkage mechanically (a regenerate that grows the
baseline is possible to run) — code review on the baseline file's diff is
the backstop: a baseline diff should almost always be a net removal of
entries, or a 1:1 swap tied to a marker/config change explained in the same
commit, never an unexplained addition.

## Runtime cost

Both whole-tree scans are cheap (`exception-guard --all --json` and
`telemetry-guard --all --json` each run in well under a second on this repo
— they're pure-Python AST-free text/regex scans over already-tracked files,
no compilation or type-checking involved). Measured on this repo:

```
$ time (python3 tools/ci/ratchet-guard.py --config tools/ci/ratchet-guard.exception.json && \
        python3 tools/ci/ratchet-guard.py --config tools/ci/ratchet-guard.telemetry.json)
...
0.08s user 0.03s system 15% cpu 0.700 total
```

Because of this, both ratchets are plain steps inside the `guardrails`
Makefile target (which `make ci-local` always runs) rather than gated behind
`MODE=all` or a separate slow-tier target — they do not meaningfully change
`ci-local`'s runtime. If a future ratcheted guard is expensive (e.g. it needs
a build or a database), give it its own `*-ratchet` target the same way, but
gate that one behind `MODE=all` or a dedicated CI job instead of the default
`guardrails` path, and note the cost here.

## Adding a new ratchet for another guard

1. Make sure the guard supports a whole-tree `--all --json` mode whose
   findings include stable fields you can key on (never line number) and a
   `severity` field.
2. Add `tools/ci/ratchet-guard.<name>.json`:
   ```json
   {
     "name": "<name>-ratchet",
     "command": ["python3", "path/to/guard.py", "--all", "--json"],
     "key_fields": ["file", "<stable-rule-field>"],
     "severity_filter": "FAIL",
     "baseline_path": "path/to/baseline.json"
   }
   ```
3. Generate the initial baseline: `python3 tools/ci/ratchet-guard.py --config tools/ci/ratchet-guard.<name>.json --regenerate`.
4. Add `<name>-ratchet` and `<name>-ratchet-regenerate` Makefile targets
   (mirror `exception-guard-ratchet` / `exception-guard-ratchet-regenerate`)
   and wire `<name>-ratchet` into the `guardrails` target (or a dedicated
   slow-tier target if the scan is expensive — see "Runtime cost" above).
5. Document the guard's own diff-scoped behavior AND the ratchet in the
   guard's doc, cross-linking here — do not let a new diff-scoped guard ship
   without this pairing, or it inherits the exact blind spot this doc exists
   to close.
