#!/usr/bin/env python3
"""telemetry_guard.py — TELEMETRY GOVERNANCE GUARDRAIL for Goat OS.

Enforces the standing rule in docs/observability/TELEMETRY_GUARDRAILS.md: every
new or changed user-facing surface must wire product analytics (Firebase
Analytics on Android, Faro on admin-web) plus, where applicable, crash/error
logging and funnel/journey step tracking.

Modes (mirrors tools/agent-hooks/check-mobile-list-fetch.mjs's diff-scoping):
  (default)     diff-scoped: merge-base(origin/main, HEAD)...HEAD, falling back
                to HEAD~1...HEAD, falling back to a full scan with a warning.
  --base <ref>  diff-scoped against an explicit ref: <ref>...HEAD.
  --staged      diff-scoped against the git index (git diff --cached).
  --all         full repository scan (ignores git diff state entirely).

Escape hatch: a line containing `// telemetry:exempt <reason>` (configurable via
config.json `exempt_marker`) anywhere in the file exempts that file from the
marker requirement. The reason is not validated for content, only presence.

Stdlib only. See tools/telemetry-guard/README.md for usage and
tools/telemetry-guard/test_telemetry_guard.py for the test suite.
"""

from __future__ import annotations

import argparse
import fnmatch
import json
import re
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterable

REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_CONFIG_PATH = Path(__file__).resolve().parent / "config.json"


# --------------------------------------------------------------------------
# Config
# --------------------------------------------------------------------------


def load_config(config_path: Path = DEFAULT_CONFIG_PATH) -> dict:
    with open(config_path, encoding="utf-8") as fh:
        return json.load(fh)


# --------------------------------------------------------------------------
# Git plumbing
# --------------------------------------------------------------------------


def _run_git(args: list[str], cwd: Path) -> str | None:
    try:
        result = subprocess.run(
            ["git", *args],
            cwd=cwd,
            capture_output=True,
            text=True,
            check=False,
        )
    except (OSError, FileNotFoundError):
        return None
    if result.returncode != 0:
        return None
    return result.stdout


def _ref_resolves(ref: str, cwd: Path) -> bool:
    return _run_git(["rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}"], cwd) is not None


def _parse_name_status(output: str) -> list[tuple[str, str]]:
    """Parse `git diff --name-status` output into (status, path) pairs.

    Renames (`R100\told\tnew`) are reported as the new path with status 'A'
    (the new path is what a surface-glob check cares about).
    """
    changes: list[tuple[str, str]] = []
    for line in output.splitlines():
        line = line.rstrip("\n")
        if not line:
            continue
        parts = line.split("\t")
        status = parts[0]
        if status.startswith("R") or status.startswith("C"):
            # rename/copy: old path, new path
            if len(parts) >= 3:
                changes.append(("A", parts[2]))
            continue
        if len(parts) >= 2:
            simple_status = "D" if status.startswith("D") else ("A" if status.startswith("A") else "M")
            changes.append((simple_status, parts[1]))
    return changes


def changed_files(
    repo: Path,
    base: str | None = None,
    staged: bool = False,
    warn=lambda msg: None,
) -> list[tuple[str, str]] | None:
    """Return list of (status, relpath) for the resolved diff scope.

    status is one of 'A' (added), 'M' (modified). Deleted files are dropped.
    Returns None if no diff base could be resolved (caller should fall back to
    a full scan).
    """
    if staged:
        out = _run_git(["diff", "--name-status", "--diff-filter=d", "--cached"], repo)
        if out is None:
            return None
        return _parse_name_status(out)

    candidate_ranges: list[str]
    if base:
        candidate_ranges = [f"{base}...HEAD"]
    else:
        candidate_ranges = ["origin/main...HEAD", "HEAD~1...HEAD"]

    for rng in candidate_ranges:
        ref = rng.split("...")[0]
        if not _ref_resolves(ref, repo):
            continue
        out = _run_git(["diff", "--name-status", "--diff-filter=d", rng], repo)
        if out is None:
            continue
        return _parse_name_status(out)

    warn(
        f"telemetry-guard: could not resolve a diff base ({', '.join(candidate_ranges)}); "
        "falling back to --all full scan"
    )
    return None


def all_tracked_files(repo: Path) -> list[str]:
    out = _run_git(["ls-files"], repo)
    if out is None:
        # Not a git repo (or git unavailable) — walk the filesystem instead.
        return [
            str(p.relative_to(repo))
            for p in repo.rglob("*")
            if p.is_file() and ".git" not in p.parts
        ]
    return [line for line in out.splitlines() if line]


# --------------------------------------------------------------------------
# Matching helpers
# --------------------------------------------------------------------------


def matches_any_glob(relpath: str, globs: Iterable[str]) -> bool:
    return any(fnmatch.fnmatchcase(relpath, g) for g in globs)


def is_new_feature_file(relpath: str, scope_root: str, dir_markers: list[str]) -> bool:
    if not dir_markers:
        return False
    if not relpath.startswith(scope_root):
        return False
    return any(marker in relpath for marker in dir_markers)


def read_text(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return ""


def has_marker(text: str, markers: Iterable[str]) -> str | None:
    for marker in markers:
        if marker in text:
            return marker
    return None


def has_exempt(text: str, exempt_marker: str) -> bool:
    return exempt_marker in text


def sibling_marker(file_path: Path, extensions: list[str], markers: Iterable[str]) -> tuple[Path, str] | None:
    directory = file_path.parent
    if not directory.is_dir():
        return None
    for sibling in sorted(directory.iterdir()):
        if sibling == file_path or not sibling.is_file():
            continue
        if extensions and sibling.suffix not in extensions:
            continue
        text = read_text(sibling)
        marker = has_marker(text, markers)
        if marker:
            return sibling, marker
    return None


# --------------------------------------------------------------------------
# Findings
# --------------------------------------------------------------------------


@dataclass
class Finding:
    surface: str
    severity: str  # "FAIL" or "WARN"
    file: str
    reason: str
    fix_hint: str
    kind: str = ""  # ratchet key component; defaults to `surface` if unset


def _finding_kind(f: "Finding") -> str:
    return f.kind or f.surface


@dataclass
class Report:
    findings: list[Finding] = field(default_factory=list)
    scanned_files: int = 0
    scope_description: str = ""

    @property
    def has_blocking(self) -> bool:
        return any(f.severity == "FAIL" for f in self.findings)


def check_surface(
    surface_name: str,
    surface_cfg: dict,
    candidates: list[tuple[str, str]],
    repo: Path,
    exempt_marker: str,
) -> list[Finding]:
    """candidates: list of (status, relpath) where status in {'A','M','X'}
    ('X' = treat-as-existing, used by --all full scans)."""
    findings: list[Finding] = []
    globs = surface_cfg.get("path_globs", [])
    markers = surface_cfg.get("markers", [])
    mode = surface_cfg.get("mode", "block")
    severity = "FAIL" if mode == "block" else "WARN"
    sibling_lookup = surface_cfg.get("sibling_lookup", True)
    sibling_exts = surface_cfg.get("sibling_extensions", [])
    new_file_markers = surface_cfg.get("new_file_dir_markers", [])
    scope_root = surface_cfg.get("new_file_scope_root", "")
    check_added = surface_cfg.get("check_added", True)
    check_modified = surface_cfg.get("check_modified", True)
    check_new_feature_files = surface_cfg.get("check_new_feature_files", False)
    new_file_extensions = surface_cfg.get("new_file_extensions", [])
    new_file_excludes = surface_cfg.get("new_file_excludes", [])

    for status, relpath in candidates:
        is_glob_match = matches_any_glob(relpath, globs)
        # A new file under a feature package only needs telemetry if it can actually EMIT any:
        # the rule flagged mesha_logo.png in five drawable densities and a reusable
        # SubmitConfirmationDialog, none of which is a screen. Excludes are declarative so the
        # next reader can see what is deliberately out of scope rather than re-deriving it.
        is_new_feature = (
            check_new_feature_files
            and status in ("A", "X")
            and is_new_feature_file(relpath, scope_root, new_file_markers)
            and (not new_file_extensions or relpath.endswith(tuple(new_file_extensions)))
            and not matches_any_glob(relpath, new_file_excludes)
        )

        if is_glob_match:
            if status == "A" and not check_added:
                continue
            if status == "M" and not check_modified:
                continue
        elif is_new_feature:
            pass
        else:
            continue

        file_path = repo / relpath
        if not file_path.is_file():
            continue
        text = read_text(file_path)

        if has_exempt(text, exempt_marker):
            continue

        marker = has_marker(text, markers)
        if marker:
            continue

        if sibling_lookup:
            sib = sibling_marker(file_path, sibling_exts, markers)
            if sib:
                continue

        reason = (
            f"no telemetry marker found ({', '.join(markers)}) in this file or a sibling in "
            f"the same directory"
        )
        fix_hint = (
            f"wire one of [{', '.join(markers)}] in this file (or an adjacent file in the same "
            f"feature directory), or add `// {exempt_marker} <reason>` if this surface genuinely "
            "has no user-facing telemetry to wire"
        )
        findings.append(
            Finding(
                surface=surface_name,
                severity=severity,
                file=relpath,
                reason=reason,
                fix_hint=fix_hint,
            )
        )

    return findings



# --------------------------------------------------------------------------
# Reserved Firebase Analytics name check (hard-fail, not exemptable)
# --------------------------------------------------------------------------
#
# Firebase silently REJECTS events using its own reserved names/prefixes — no
# error surfaces in the app, the event is just dropped on the floor. This bit
# this exact repo once for real: `session_start` was the FIRST event of every
# journey and Firebase discarded it outright, so the very start of every
# funnel was invisible until someone noticed via `AnalyticsEvents.SESSION_START
# = "app_session_start"` (see the comment there). This check exists so the
# next occurrence of that class of bug is a CI failure instead of a silent
# analytics gap discovered weeks later.
#
# Detection is deliberately NARROW, not "every quoted string in the tree":
# scanning every string literal produced overwhelming noise on a first pass
# (generic strings like "error" or object keys like "google_error" appear
# constantly in UI code with nothing to do with analytics). Instead this only
# looks at two specific shapes, both of which are exactly where a real event
# name gets minted:
#   1. a Kotlin/TS constant DECLARATION whose value looks like an event/param
#      name, e.g. `const val LOGIN_FAILURE = "login_failure"` or
#      `export const SCREEN_OPENED = "screen_opened"`.
#   2. a literal passed DIRECTLY as the first argument to an emission call:
#      `.track("...")`, `logEvent("...")`, `trackEvent("...")`, `pushEvent("...")`.
# It CANNOT see a name built up via string concatenation/interpolation at
# runtime, a name referenced only via its constant (not the literal), or a
# name passed through an intermediate variable before the call. That is an
# accepted false-negative gap for a stdlib-only, no-AST guard — see
# docs/TELEMETRY.md "reserved-name trap" section.

_RESERVED_CONST_DECL_RE = re.compile(
    r'(?:const\s+val\s+\w+(?:\s*:\s*\w+)?|export\s+const\s+\w+(?:\s*:\s*\w+)?)\s*=\s*"([^"]+)"'
)
_RESERVED_CALL_SITE_RE = re.compile(
    r'\.(?:track|logEvent)\s*\(\s*"([^"]+)"|(?:^|[^.\w])(?:trackEvent|pushEvent|logScreenView|trackScreenView)\s*\(\s*"([^"]+)"'
)


def check_reserved_names(candidates: list[tuple[str, str]], repo: Path, config: dict) -> list[Finding]:
    findings: list[Finding] = []
    reserved_events = set(config.get("reserved_event_names", []))
    reserved_param_prefixes = tuple(config.get("reserved_param_prefixes", []))
    scan_globs = config.get("reserved_name_scan_globs", ["**/*.kt", "**/*.tsx", "**/*.ts", "**/*.go"])
    if not reserved_events and not reserved_param_prefixes:
        return findings

    for status, relpath in candidates:
        if status == "D":
            continue
        if not matches_any_glob(relpath, scan_globs):
            continue
        file_path = repo / relpath
        if not file_path.is_file():
            continue
        text = read_text(file_path)
        for lineno, line in enumerate(text.splitlines(), start=1):
            literals: list[str] = []
            m = _RESERVED_CONST_DECL_RE.search(line)
            if m:
                literals.append(m.group(1))
            for cm in _RESERVED_CALL_SITE_RE.finditer(line):
                literals.append(cm.group(1) or cm.group(2))
            for literal in literals:
                if literal in reserved_events:
                    findings.append(
                        Finding(
                            surface="reserved_names",
                            kind="reserved_event_name",
                            severity="FAIL",
                            file=relpath,
                            reason=(
                                f'event/param name "{literal}" on line {lineno} is a Firebase '
                                "RESERVED name — Firebase silently drops events using it"
                            ),
                            fix_hint=(
                                f'rename to a non-reserved name (e.g. prefix with `app_`, as '
                                f'AnalyticsEvents.SESSION_START did: "session_start" -> '
                                f'"app_session_start"); this is not exemptable — Firebase itself '
                                "rejects the literal name, an exempt comment cannot change that"
                            ),
                        )
                    )
                elif literal.startswith(reserved_param_prefixes):
                    findings.append(
                        Finding(
                            surface="reserved_names",
                            kind="reserved_param_prefix",
                            severity="FAIL",
                            file=relpath,
                            reason=(
                                f'param/user-property name "{literal}" on line {lineno} uses a '
                                "Firebase RESERVED prefix (firebase_/google_/ga_)"
                            ),
                            fix_hint="rename the parameter to drop the reserved prefix; not exemptable",
                        )
                    )
    return findings


def run(
    repo: Path,
    config: dict,
    base: str | None,
    staged: bool,
    scan_all: bool,
    warn=lambda msg: None,
    only_surfaces: list[str] | None = None,
) -> Report:
    exempt_marker = config.get("exempt_marker", "telemetry:exempt")
    surfaces = config.get("surfaces", {})
    if only_surfaces is not None:
        surfaces = {k: v for k, v in surfaces.items() if k in only_surfaces}

    if scan_all:
        tracked = all_tracked_files(repo)
        candidates = [("X", relpath) for relpath in tracked]
        scope_description = "full repository scan (--all)"
    else:
        diff = changed_files(repo, base=base, staged=staged, warn=warn)
        if diff is None:
            tracked = all_tracked_files(repo)
            candidates = [("X", relpath) for relpath in tracked]
            scope_description = "full repository scan (no diff base resolved)"
        else:
            candidates = diff
            scope_description = (
                f"staged changes" if staged else f"diff vs {base or 'origin/main (or HEAD~1 fallback)'}"
            )

    report = Report(scanned_files=len(candidates), scope_description=scope_description)
    for surface_name, surface_cfg in surfaces.items():
        report.findings.extend(
            check_surface(surface_name, surface_cfg, candidates, repo, exempt_marker)
        )
    # Reserved-name check is not part of the `surfaces` marker-presence engine
    # and is never exemptable (see check_reserved_names docstring above). It
    # runs by default; pass --only-surfaces without "reserved_names" to scope
    # a ratchet invocation to marker-presence surfaces only (used to keep the
    # original telemetry-guard-ratchet baseline stable when new rule kinds
    # were added — see tools/ci/ratchet-guard.telemetry.json vs
    # tools/ci/ratchet-guard.telemetry-v2.json).
    if only_surfaces is None or "reserved_names" in only_surfaces:
        report.findings.extend(check_reserved_names(candidates, repo, config))
    return report


# --------------------------------------------------------------------------
# CLI
# --------------------------------------------------------------------------


def format_text_report(report: Report) -> str:
    lines = [f"telemetry-guard: scope = {report.scope_description}"]
    if not report.findings:
        lines.append("telemetry-guard: PASS — no missing telemetry on changed surfaces")
        return "\n".join(lines)

    fails = [f for f in report.findings if f.severity == "FAIL"]
    warns = [f for f in report.findings if f.severity == "WARN"]

    for f in report.findings:
        lines.append(f"[{f.severity}] {f.surface}/{_finding_kind(f)}: {f.file}")
        lines.append(f"    reason: {f.reason}")
        lines.append(f"    fix:    {f.fix_hint}")

    lines.append("")
    lines.append(f"telemetry-guard: {len(fails)} FAIL, {len(warns)} WARN")
    if fails:
        lines.append("telemetry-guard: BLOCKED — resolve FAIL findings above before committing")
        lines.append(
            "telemetry-guard: escape hatch: add `// telemetry:exempt <reason>` if a surface "
            "genuinely has no user-facing telemetry to wire"
        )
    else:
        lines.append("telemetry-guard: PASS (WARN findings are non-blocking)")
    return "\n".join(lines)


def format_json_report(report: Report) -> str:
    payload = {
        "scope": report.scope_description,
        "scanned_candidates": report.scanned_files,
        "findings": [
            {
                "surface": f.surface,
                "kind": _finding_kind(f),
                "severity": f.severity,
                "file": f.file,
                "reason": f.reason,
                "fix_hint": f.fix_hint,
            }
            for f in report.findings
        ],
        "fail_count": sum(1 for f in report.findings if f.severity == "FAIL"),
        "warn_count": sum(1 for f in report.findings if f.severity == "WARN"),
        "blocked": report.has_blocking,
    }
    return json.dumps(payload, indent=2)


def main(argv: list[str] | None = None) -> int:
    import os

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--base",
        default=os.environ.get("TELEMETRY_GUARD_BASE"),
        help="Diff base ref, e.g. origin/main (env: TELEMETRY_GUARD_BASE)",
    )
    parser.add_argument("--all", action="store_true", help="Full repository scan")
    parser.add_argument("--staged", action="store_true", help="Diff-scope against the git index")
    parser.add_argument("--json", action="store_true", help="Emit JSON instead of text")
    parser.add_argument("--config", default=str(DEFAULT_CONFIG_PATH), help="Path to config.json")
    parser.add_argument("--repo", default=str(REPO_ROOT), help="Repo root override (mainly for tests)")
    parser.add_argument(
        "--only-surfaces",
        default=None,
        help=(
            "Comma-separated surface names to scope this run to (e.g. "
            "'android,admin_web' or 'screen_view,primary_action,failure_outcome,reserved_names'). "
            "Used to keep separate ratchet baselines for the original marker-presence "
            "surfaces vs the newer screen-view/primary-action/failure-outcome/reserved-name "
            "rules without disturbing the existing baseline counts."
        ),
    )
    args = parser.parse_args(argv)

    if args.all and args.staged:
        print("telemetry-guard: --all and --staged are mutually exclusive", file=sys.stderr)
        return 2

    repo = Path(args.repo).resolve()
    config = load_config(Path(args.config))
    only_surfaces = [s.strip() for s in args.only_surfaces.split(",")] if args.only_surfaces else None

    warnings: list[str] = []
    report = run(
        repo=repo,
        config=config,
        base=args.base,
        staged=args.staged,
        scan_all=args.all,
        warn=warnings.append,
        only_surfaces=only_surfaces,
    )

    for w in warnings:
        print(w, file=sys.stderr)

    if args.json:
        print(format_json_report(report))
    else:
        print(format_text_report(report))

    return 1 if report.has_blocking else 0


if __name__ == "__main__":
    sys.exit(main())
