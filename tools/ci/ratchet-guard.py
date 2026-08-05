#!/usr/bin/env python3
"""ratchet-guard.py — shrink-only debt ratchet for whole-tree guardrail scans.

Problem this solves
--------------------
exception-guard and telemetry-guard are diff-scoped by design (see their own
docstrings): they only look at lines a diff touches, because the existing
tree has legacy violations that would fail CI on day one otherwise. That is
correct for the PR path, but it has a silent failure mode: the `--all`
whole-tree variant of each guard (`exception-guard-audit`,
`telemetry-guard-audit`) is NOT wired into `make ci-local`, so nothing ever
re-checks the *existing* violations. A guard that only watches new diffs
gives a permanent green light while hundreds of real "swallowed exception" /
"missing telemetry" violations sit in the tree — enforcement in name only.

The fix is a ratchet, not a hard `--all` gate (which would fail on day one
and get switched off): freeze today's known violations into a checked-in
baseline, then fail CI only on violations that are NOT in that baseline. New
code can never add a new swallow/missing-telemetry surface. Existing debt
stays visible (it is printed every run) without blocking anyone.

To keep the ratchet honest, it is shrink-only in both directions:
  - current violations NOT in the baseline  -> FAIL (new debt introduced)
  - baseline entries NOT in current violations -> FAIL (baseline is
    stale-high: some debt was fixed but the baseline was never shrunk down,
    which would silently free up "budget" to reintroduce an equivalent
    violation elsewhere without ever tripping the ratchet)

Baseline keying
----------------
Entries are keyed by (file, rule-kind) — e.g. ("backend/internal/foo/bar.go",
"discarded_err") — NEVER by line number. Line numbers churn on every
unrelated edit to a file (an import added above the violation shifts every
line below it), so a line-keyed baseline would desync from reality on the
next commit: it would either false-pass (line moved but file still has the
same violation, now "vanished" from the FAIL set because line N no longer
matches) or false-fail (an unrelated edit shifts the violation's line by one
and the ratchet reports it as "new"). File+rule-kind is stable across
unrelated edits and only changes when the violation itself is actually fixed
or a genuinely new one of the same kind is added to the same file — which is
exactly the signal the ratchet is meant to catch.

Baseline is COUNT-aware, not presence-only
-------------------------------------------
Each (file, kind) key stores an integer COUNT of how many findings of that
kind the file had when the baseline was cut, not just a boolean "this key
exists". A file FAILS the ratchet if its current count for a (file, kind)
exceeds the baselined count for that key — not merely if the key is new.

This was a real, reproduced defeat: with presence-only keying (the key
existing in `entries` at all was enough to pass), a file already baselined
for e.g. `swallowed_err` could receive an UNLIMITED number of additional
swallowed errors of that same kind in the same file and the ratchet still
reported PASS, because `file|kind` was already "known". A judge added 3 new
swallows to a file baselined with 1 existing `swallowed_err` (making the real
count 4) and the old ratchet exited 0. Worse, `--regenerate` laundered it:
regenerating after stacking new debt onto an already-baselined file produced
an identical entry count and a zero-diff baseline, so the stacked debt was
never visible in review either.

Counting was chosen over content-fingerprinting each individual violation
(e.g. hashing the offending line or an AST node) for the same reason line
numbers were rejected above, one level more subtle: fingerprints are more
precise in principle (they could in theory catch "this exact violation
still exists" vs "a different violation of the same kind exists"), but they
are fragile in practice. A token-level fingerprint of the offending
expression can collide (two different swallowed-errors written the same way
hash identically) or shift under an unrelated refactor (renaming a nearby
variable, reformatting, or wrapping the line changes the token stream even
though the violation itself didn't move). A line-anchored fingerprint has
the exact churn problem line numbers already have. A count-per-(file, kind)
is simpler, has none of that churn sensitivity, is robust to unrelated edits
in the same file, and directly catches the one failure mode that matters for
a ratchet: MORE debt of a kind landing in a file than was there before. It
does not distinguish which individual violation is new when a file has
multiple of the same kind — but the ratchet's job is "did total debt grow",
not "which line is the new one"; the printed current-vs-baseline counts and
`git diff` on the PR are what a reviewer uses to see the new line(s).

Usage
-----
  # CI check mode (default) — fails on drift in either direction, prints debt count.
  python3 tools/ci/ratchet-guard.py --config tools/ci/ratchet-guard.exception.json

  # Regenerate the baseline after fixing (shrinks it) or after a legitimate
  # `// exception:exempt` / marker fix changes the violation set. Never use
  # this to "make CI pass" by baselining a NEW violation you just introduced
  # — see the docs note in AGENTS.md about this being a rejected escape hatch.
  python3 tools/ci/ratchet-guard.py --config tools/ci/ratchet-guard.exception.json --regenerate

Each ratchet is described by a small JSON config (see
tools/ci/ratchet-guard.exception.json and .telemetry.json) rather than flags,
so the same driver works for both guards (and any future whole-tree guard)
without code changes.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]


def load_ratchet_config(path: Path) -> dict:
    with open(path, encoding="utf-8") as fh:
        return json.load(fh)


def run_guard(cmd: list[str], repo: Path) -> dict:
    result = subprocess.run(cmd, cwd=repo, capture_output=True, text=True, check=False)
    if not result.stdout.strip():
        print(result.stderr, file=sys.stderr)
        raise SystemExit(f"ratchet-guard: guard command produced no stdout: {' '.join(cmd)}")
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        print(result.stdout, file=sys.stderr)
        print(result.stderr, file=sys.stderr)
        raise SystemExit(f"ratchet-guard: guard command did not emit valid JSON: {exc}")


def finding_key(finding: dict, key_fields: list[str]) -> str:
    return "|".join(str(finding.get(f, "")) for f in key_fields)


def extract_keys(payload: dict, key_fields: list[str], severity_filter: str | None) -> dict[str, int]:
    """Return a COUNT per key, not a set of keys.

    A set could only answer "does this file already have a violation of this kind", so once a file
    appeared in the baseline it could absorb unlimited additional violations of the same kind and
    still pass — reproduced live: a file baselined with 1 swallowed error took 3 more and the
    ratchet still printed PASS. Counting is what makes "shrink-only" true per violation while
    staying immune to line-number churn (which is why keys are file+kind, never file+line).
    """
    findings = payload.get("findings", [])
    counts: dict[str, int] = {}
    for f in findings:
        if severity_filter is not None and f.get("severity") != severity_filter:
            continue
        key = finding_key(f, key_fields)
        counts[key] = counts.get(key, 0) + 1
    return counts


def load_baseline(path: Path) -> dict[str, int]:
    if not path.is_file():
        return {}
    with open(path, encoding="utf-8") as fh:
        data = json.load(fh)
    entries = data.get("entries", {})
    # Legacy baselines stored a bare list of keys with no counts. Treat each as count 1 — the
    # smallest honest reading — so a stale legacy file fails loudly and gets regenerated rather
    # than silently licensing whatever count happens to exist today.
    if isinstance(entries, list):
        return {key: 1 for key in entries}
    return {str(k): int(v) for k, v in entries.items()}


def write_baseline(path: Path, counts: dict[str, int], name: str) -> None:
    payload = {
        "_comment": (
            f"Shrink-only ratchet baseline for {name}. Each entry is a COUNT of violations for "
            f"that file+rule, so adding one to an already-listed file still fails. Regenerate with "
            f"`python3 tools/ci/ratchet-guard.py --config <this-config> --regenerate` "
            "after genuinely fixing violations. Never hand-edit to add a new "
            "entry or raise a count to land new code — that defeats the ratchet."
        ),
        "key_fields": "see the ratchet config's key_fields — stable file+rule identity, never line number",
        "total": sum(counts.values()),
        "entries": {k: counts[k] for k in sorted(counts)},
    }
    path.parent.mkdir(parents=True, exist_ok=True)
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(payload, fh, indent=2, sort_keys=False)
        fh.write("\n")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--config", required=True, help="Path to a ratchet config JSON (see tools/ci/ratchet-guard.*.json)")
    parser.add_argument("--regenerate", action="store_true", help="Rewrite the baseline to match current whole-tree findings")
    parser.add_argument("--repo", default=str(REPO_ROOT))
    args = parser.parse_args(argv)

    repo = Path(args.repo).resolve()
    cfg = load_ratchet_config(Path(args.config))

    name = cfg["name"]
    cmd = cfg["command"]
    key_fields = cfg["key_fields"]
    severity_filter = cfg.get("severity_filter", "FAIL")
    baseline_path = repo / cfg["baseline_path"]

    payload = run_guard(cmd, repo)
    current_keys = extract_keys(payload, key_fields, severity_filter)

    if args.regenerate:
        # Print the delta. Regeneration used to be silent, so adding debt to an already-listed
        # file produced a baseline with the SAME entry count and a ZERO-line git diff — a reviewer
        # inspecting the PR saw nothing at all. Whatever changes must be said out loud.
        previous = load_baseline(baseline_path)
        added = {k: v for k, v in current_keys.items() if k not in previous}
        removed = {k: v for k, v in previous.items() if k not in current_keys}
        raised = {k: (previous[k], v) for k, v in current_keys.items() if k in previous and v > previous[k]}
        lowered = {k: (previous[k], v) for k, v in current_keys.items() if k in previous and v < previous[k]}
        write_baseline(baseline_path, current_keys, name)
        print(
            f"ratchet-guard[{name}]: baseline regenerated at {baseline_path} — "
            f"{len(current_keys)} entries, {sum(current_keys.values())} violation(s) total "
            f"(was {sum(previous.values())})"
        )
        for k, v in sorted(added.items()):
            print(f"    +NEW  {k} = {v}")
        for k, (before, after) in sorted(raised.items()):
            print(f"    +MORE {k} = {before} -> {after}")
        for k, (before, after) in sorted(lowered.items()):
            print(f"    -LESS {k} = {before} -> {after}")
        for k, v in sorted(removed.items()):
            print(f"    -GONE {k} (was {v})")
        if added or raised:
            print(
                f"ratchet-guard[{name}]: WARNING — this regeneration ADMITTED NEW DEBT. That is not "
                "an accepted way to land code; fix the violations instead."
            )
        return 0

    baseline_keys = load_baseline(baseline_path)

    new_violations = sorted(k for k in current_keys if k not in baseline_keys)
    stale_baseline = sorted(k for k in baseline_keys if k not in current_keys)
    # The case a set-based baseline was blind to: the file+rule is already known, but it now has
    # MORE violations than it was admitted with.
    increased = sorted(
        (k, baseline_keys[k], current_keys[k])
        for k in current_keys
        if k in baseline_keys and current_keys[k] > baseline_keys[k]
    )
    decreased = sorted(
        (k, baseline_keys[k], current_keys[k])
        for k in current_keys
        if k in baseline_keys and current_keys[k] < baseline_keys[k]
    )

    print(
        f"ratchet-guard[{name}]: whole-tree scan — {sum(current_keys.values())} violation(s) "
        f"across {len(current_keys)} file+rule key(s)"
    )
    print(
        f"ratchet-guard[{name}]: baseline allows {sum(baseline_keys.values())} across "
        f"{len(baseline_keys)} key(s) at {cfg['baseline_path']}"
    )

    ok = True

    if new_violations:
        ok = False
        print(f"ratchet-guard[{name}]: BLOCKED — {len(new_violations)} violation(s) not covered by the baseline (new debt):")
        for k in new_violations:
            print(f"    NEW: {k}")
        print(
            f"ratchet-guard[{name}]: fix these before landing. Adding them to the baseline instead of "
            "fixing them is not an accepted way to land new code."
        )

    if increased:
        ok = False
        print(
            f"ratchet-guard[{name}]: BLOCKED — {len(increased)} file+rule key(s) gained violations "
            "(the baseline covers this file, but not this MUCH of it):"
        )
        for k, before, after in increased:
            print(f"    MORE: {k} = {before} -> {after} (+{after - before})")
        print(
            f"ratchet-guard[{name}]: fix the new ones. A file already carrying debt is not a "
            "licence to add more to it."
        )

    if decreased:
        ok = False
        print(
            f"ratchet-guard[{name}]: BLOCKED — {len(decreased)} key(s) improved but the baseline "
            "was not shrunk (debt budget must ratchet DOWN, never linger):"
        )
        for k, before, after in decreased:
            print(f"    FIXED: {k} = {before} -> {after}")
        print(
            f"ratchet-guard[{name}]: run `python3 tools/ci/ratchet-guard.py --config {args.config} "
            "--regenerate` and commit the shrunk baseline."
        )

    if stale_baseline:
        ok = False
        print(
            f"ratchet-guard[{name}]: BLOCKED — baseline is stale-high, {len(stale_baseline)} entries no "
            "longer reproduce (debt was fixed but the baseline was not shrunk):"
        )
        for k in stale_baseline:
            print(f"    STALE: {k}")
        print(
            f"ratchet-guard[{name}]: run `python3 tools/ci/ratchet-guard.py --config {args.config} "
            "--regenerate` and commit the updated baseline."
        )

    if ok:
        print(f"ratchet-guard[{name}]: PASS — no new violations, baseline is current")
        print(f"ratchet-guard[{name}]: outstanding debt = {sum(current_keys.values())} violation(s) across {len(current_keys)} key(s) (visible, non-blocking, shrink it whenever you touch a file)")
        return 0

    return 1


if __name__ == "__main__":
    sys.exit(main())
