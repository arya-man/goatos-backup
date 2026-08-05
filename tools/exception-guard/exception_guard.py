#!/usr/bin/env python3
"""exception_guard.py — EXCEPTION-SWALLOWING GUARDRAIL for Goat OS.

Enforces the maintainer's golden rule: "Never swallow any exception. Always
either dump it into Firebase non-fatal errors (mobile) or backend logs
(server)."

Mirrors tools/telemetry-guard/telemetry_guard.py's structure and CLI
conventions:
  (default)     diff-scoped: origin/main...HEAD, falling back to HEAD~1...HEAD,
                falling back to a full scan with a warning.
  --base <ref>  diff-scoped against an explicit ref: <ref>...HEAD.
  --staged      diff-scoped against the git index (git diff --cached).
  --all         full repository scan (ignores git diff state entirely).
  --self-test   run the inline fixture suite (violations fire, handled/exempt
                equivalents don't, test files are ignored) and exit 0/1.

Unlike telemetry-guard (file-presence marker check), this guard is
DIFF-ADDED-LINES-ONLY: it looks at the actual `catch`/`if err != nil`/discard
blocks that a diff touches, not whole files, because the existing codebase has
legacy violations that would fail day one otherwise. A block is in scope only
if at least one of its lines is a newly added line in the diff.

Escape hatch: a line containing `// exception:exempt <reason>` (configurable
via config.json `exempt_marker`) inside (or immediately preceding) the
catch/error block exempts that block.

Deliberately NOT detected (see docs/observability/TELEMETRY_GUARDRAILS.md and
apps/goatos-android/docs/TELEMETRY.md for the write-up):
  - Multi-line catch bodies with complex control flow beyond the simple
    shapes below (best-effort brace matching only).
  - Errors wrapped in a custom type and returned up without a local log call
    (logged at a higher layer) — only flagged when the block does NOTHING
    with err at all (no return of it, no wrap, no log).
  - Generic Result<T>/Either monadic error handling.
  - Test files (`*Test.kt`, `*_test.go`, test directories) — excluded
    entirely.
  - `_ = err` (or similar) immediately following a Close()/Rollback()-style
    call — conventionally safe to discard in Go.
  - `if err != nil { return }` with a BARE return (no value) — common Go
    guard-clause idiom for "bail out of this optional/logging helper, the
    error was already handled elsewhere/by the caller." Only an EXPLICIT
    `return nil` (or `return ..., nil`) inside the block is flagged, because
    that shape means the code affirmatively converted a real error into a
    reported success. This narrowing was made after the guard's first run
    against this repo's real working-tree diff flagged two legitimate
    success-only-logging guard clauses in backend/internal/sop/adapters/http
    and backend/internal/weighing/adapters/http as false positives.

Stdlib only.
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
# Git plumbing (mirrors telemetry_guard.py)
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
    changes: list[tuple[str, str]] = []
    for line in output.splitlines():
        line = line.rstrip("\n")
        if not line:
            continue
        parts = line.split("\t")
        status = parts[0]
        if status.startswith("R") or status.startswith("C"):
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
        f"exception-guard: could not resolve a diff base ({', '.join(candidate_ranges)}); "
        "falling back to --all full scan"
    )
    return None


def all_tracked_files(repo: Path) -> list[str]:
    out = _run_git(["ls-files"], repo)
    if out is None:
        return [
            str(p.relative_to(repo))
            for p in repo.rglob("*")
            if p.is_file() and ".git" not in p.parts
        ]
    return [line for line in out.splitlines() if line]


def added_line_numbers(
    repo: Path,
    relpath: str,
    base: str | None,
    staged: bool,
    scan_all: bool,
) -> set[int] | None:
    """Return the set of 1-based line numbers added by the diff for relpath.

    Returns None when scan_all (caller should treat every line as in-scope) or
    when the diff could not be computed (caller should also treat every line
    as in-scope — better a false positive on an unresolved diff than silently
    skipping review).
    """
    if scan_all:
        return None

    if staged:
        out = _run_git(["diff", "--unified=0", "--diff-filter=d", "--cached", "--", relpath], repo)
    else:
        rng = None
        for candidate in ([f"{base}...HEAD"] if base else ["origin/main...HEAD", "HEAD~1...HEAD"]):
            ref = candidate.split("...")[0]
            if _ref_resolves(ref, repo):
                rng = candidate
                break
        if rng is None:
            return None
        out = _run_git(["diff", "--unified=0", "--diff-filter=d", rng, "--", relpath], repo)

    if out is None:
        return None

    added: set[int] = set()
    hunk_re = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@")
    cur_line = 0
    for line in out.splitlines():
        m = hunk_re.match(line)
        if m:
            cur_line = int(m.group(1))
            continue
        if line.startswith("+++") or line.startswith("---"):
            continue
        if line.startswith("+"):
            added.add(cur_line)
            cur_line += 1
        elif line.startswith("-"):
            continue
        else:
            cur_line += 1
    return added


# --------------------------------------------------------------------------
# Matching helpers
# --------------------------------------------------------------------------


def matches_any_glob(relpath: str, globs: Iterable[str]) -> bool:
    return any(fnmatch.fnmatchcase(relpath, g) or fnmatch.fnmatchcase(Path(relpath).name, g) for g in globs)


def read_text(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return ""


def has_any(text: str, needles: Iterable[str]) -> bool:
    return any(n in text for n in needles)


# --------------------------------------------------------------------------
# Findings
# --------------------------------------------------------------------------


@dataclass
class Finding:
    language: str
    kind: str
    severity: str
    file: str
    line: int
    reason: str
    fix_hint: str


@dataclass
class Report:
    findings: list[Finding] = field(default_factory=list)
    scanned_files: int = 0
    scope_description: str = ""

    @property
    def has_blocking(self) -> bool:
        return any(f.severity == "FAIL" for f in self.findings)


# --------------------------------------------------------------------------
# Block extraction (brace matching)
# --------------------------------------------------------------------------


def _line_of_offset(text: str, offset: int) -> int:
    return text.count("\n", 0, offset) + 1


def _find_matching_brace(text: str, open_idx: int) -> int | None:
    """open_idx points at the '{' char. Returns index of matching '}' or None."""
    depth = 0
    i = open_idx
    n = len(text)
    while i < n:
        c = text[i]
        if c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
            if depth == 0:
                return i
        i += 1
    return None


@dataclass
class Block:
    header: str
    body: str
    start_line: int
    end_line: int


def _extract_brace_blocks(text: str, header_re: re.Pattern) -> list[Block]:
    blocks: list[Block] = []
    for m in header_re.finditer(text):
        open_idx = text.find("{", m.end() - 1)
        if open_idx == -1:
            continue
        close_idx = _find_matching_brace(text, open_idx)
        if close_idx is None:
            continue
        body = text[open_idx + 1 : close_idx]
        blocks.append(
            Block(
                header=m.group(0),
                body=body,
                start_line=_line_of_offset(text, m.start()),
                end_line=_line_of_offset(text, close_idx),
            )
        )
    return blocks


def _strip_comments_kotlin(body: str) -> str:
    body = re.sub(r"//.*", "", body)
    body = re.sub(r"/\*.*?\*/", "", body, flags=re.DOTALL)
    return body


def _has_exempt_nearby(text: str, block: Block, exempt_marker: str, lookback_lines: int = 3) -> bool:
    if exempt_marker in block.body:
        return True
    lines = text.splitlines()
    start_idx = max(0, block.start_line - 1 - lookback_lines)
    preceding = "\n".join(lines[start_idx : block.start_line - 1])
    return exempt_marker in preceding


def _block_overlaps_added(block: Block, added: set[int] | None) -> bool:
    if added is None:
        return True
    return any(ln in added for ln in range(block.start_line, block.end_line + 1))


# --------------------------------------------------------------------------
# Kotlin checks
# --------------------------------------------------------------------------

_KT_CATCH_HEADER_RE = re.compile(r"catch\s*\(([^)]*)\)\s*\{")
_KT_RUNCATCHING_RE = re.compile(r"runCatching\s*\{")
_KT_LOG_CALL_RE = re.compile(r"\b(Log\.[dwevi]|println|Timber\.[dwevi])\s*\(")

# Kotlin coroutine cancellation: `catch (e: CancellationException) { ... }` (or
# `catch (e: kotlinx.coroutines.CancellationException)`) that does NOT rethrow
# `e` breaks structured concurrency — the parent coroutine/scope never learns
# the child was cancelled, so cancellation silently stops propagating and the
# job can appear "stuck" or keep doing work after its scope was torn down.
# This is a well-known Kotlin coroutines footgun (the same shape as a bare
# `catch (e: Exception)` that also matches CancellationException, since it is
# itself an Exception subtype). Always a hard violation: there is no legitimate
# reason to swallow it, so — unlike every other rule in this file — the
# `// exception:exempt` marker does NOT suppress it.
_KT_CANCELLATION_CATCH_RE = re.compile(r"catch\s*\(\s*(\w+)\s*:\s*(?:[\w.]*\.)?CancellationException\s*\)")


def _is_cancellation_catch(header: str) -> str | None:
    """Returns the caught exception's variable name if `header` catches
    CancellationException, else None."""
    m = _KT_CANCELLATION_CATCH_RE.search(header)
    return m.group(1) if m else None


def check_kotlin_cancellation(text: str, relpath: str, added: set[int] | None) -> list[Finding]:
    findings: list[Finding] = []
    for block in _extract_brace_blocks(text, _KT_CATCH_HEADER_RE):
        var_name = _is_cancellation_catch(block.header)
        if not var_name:
            continue
        if not _block_overlaps_added(block, added):
            continue
        stripped = _strip_comments_kotlin(block.body)
        rethrow_re = re.compile(rf"\bthrow\s+{re.escape(var_name)}\b")
        if rethrow_re.search(stripped):
            continue
        findings.append(
            Finding(
                language="kotlin",
                kind="cancellation_swallowed",
                severity="FAIL",
                file=relpath,
                line=block.start_line,
                reason=(
                    f"`catch ({var_name}: CancellationException)` does not rethrow {var_name} — "
                    "this breaks structured concurrency (the coroutine's cancellation stops "
                    "propagating to its parent scope)"
                ),
                fix_hint=(
                    f"add `throw {var_name}` as the LAST statement in this catch body (after any "
                    "logging/cleanup); this is a hard rule with no `// exception:exempt` escape "
                    "hatch — cancellation must always propagate"
                ),
            )
        )
    return findings


def check_kotlin(text: str, relpath: str, added: set[int] | None, config: dict) -> list[Finding]:
    findings: list[Finding] = []
    markers = config.get("recording_markers", [])
    exempt_marker = config.get("exempt_marker", "exception:exempt")

    # Cancellation-swallow is checked and reported as its own dedicated,
    # non-exemptable rule (see check_kotlin_cancellation) — never mixed into
    # the recording-marker rules below to avoid double-reporting the same
    # catch block under two different kinds with two different remediations.
    findings.extend(check_kotlin_cancellation(text, relpath, added))
    cancellation_catch_lines = {
        b.start_line
        for b in _extract_brace_blocks(text, _KT_CATCH_HEADER_RE)
        if _is_cancellation_catch(b.header)
    }

    for block in _extract_brace_blocks(text, _KT_CATCH_HEADER_RE):
        if block.start_line in cancellation_catch_lines:
            continue
        if not _block_overlaps_added(block, added):
            continue
        if _has_exempt_nearby(text, block, exempt_marker):
            continue
        if has_any(block.body, markers):
            continue

        stripped = _strip_comments_kotlin(block.body).strip()

        if stripped == "":
            findings.append(
                Finding(
                    language="kotlin",
                    kind="empty_catch",
                    severity="FAIL",
                    file=relpath,
                    line=block.start_line,
                    reason="empty catch body swallows the exception silently",
                    fix_hint=(
                        "call CrashReporter.recordException(e, \"...\") (or an equivalent from "
                        f"[{', '.join(markers)}]) in the catch body, or add `// {exempt_marker} <reason>`"
                    ),
                )
            )
            continue

        non_log_statements = []
        for stmt in re.split(r"[\n;]", stripped):
            stmt = stmt.strip()
            if not stmt:
                continue
            if _KT_LOG_CALL_RE.match(stmt):
                continue
            non_log_statements.append(stmt)

        if not non_log_statements:
            findings.append(
                Finding(
                    language="kotlin",
                    kind="log_only_catch",
                    severity="FAIL",
                    file=relpath,
                    line=block.start_line,
                    reason="catch body only logs (Log.x/println) — no crash/non-fatal recording call",
                    fix_hint=(
                        "add CrashReporter.recordException(e, ...) alongside the log call, or add "
                        f"`// {exempt_marker} <reason>`"
                    ),
                )
            )
            continue

        trivial_re = re.compile(r"^(return(\s+.*)?|emit\s*\(.*\)|continue|break)$")
        if all(trivial_re.match(s) for s in non_log_statements):
            findings.append(
                Finding(
                    language="kotlin",
                    kind="swallowed_catch",
                    severity="FAIL",
                    file=relpath,
                    line=block.start_line,
                    reason="catch body only returns/emits without recording the exception anywhere",
                    fix_hint=(
                        "record the exception before returning/emitting, or add "
                        f"`// {exempt_marker} <reason>`"
                    ),
                )
            )

    for m in _KT_RUNCATCHING_RE.finditer(text):
        open_idx = text.find("{", m.end() - 1)
        if open_idx == -1:
            continue
        close_idx = _find_matching_brace(text, open_idx)
        if close_idx is None:
            continue
        tail = text[close_idx + 1 : close_idx + 200]
        tail_match = re.match(r"\s*\.(getOrNull|getOrDefault)\s*\(", tail)
        if not tail_match:
            continue
        # Look ahead a bit further for an .onFailure{...} chained before the
        # terminal call — if present, treat as handled.
        between = tail[: tail_match.start()]
        if "onFailure" in between:
            continue
        block_start_line = _line_of_offset(text, m.start())
        block_end_line = _line_of_offset(text, close_idx + tail_match.end())
        fake_block = Block(header=m.group(0), body=text[open_idx + 1 : close_idx], start_line=block_start_line, end_line=block_end_line)
        if not _block_overlaps_added(fake_block, added):
            continue
        if _has_exempt_nearby(text, fake_block, exempt_marker):
            continue
        if has_any(fake_block.body, markers):
            continue
        findings.append(
            Finding(
                language="kotlin",
                kind="runcatching_discard",
                severity="FAIL",
                file=relpath,
                line=block_start_line,
                reason=f"runCatching {{ ... }}.{tail_match.group(1)}(...) silently discards the exception",
                fix_hint=(
                    "chain `.onFailure { CrashReporter.recordException(it, ...) }` before "
                    f"`.{tail_match.group(1)}(...)`, or add `// {exempt_marker} <reason>`"
                ),
            )
        )

    return findings


# --------------------------------------------------------------------------
# Go checks
# --------------------------------------------------------------------------

_GO_IF_ERR_RE = re.compile(r"if\s+(\w*[Ee]rr\w*)\s*!=\s*nil\s*\{")
_GO_DISCARD_RE = re.compile(r"^\s*_\s*=\s*(\w*[Ee]rr\w*)\b")
_GO_SAFE_RECEIVER_RE = re.compile(r"\.(Close|Rollback|CloseRows?)\s*\(")


def check_go(text: str, relpath: str, added: set[int] | None, config: dict) -> list[Finding]:
    findings: list[Finding] = []
    markers = config.get("recording_markers", [])
    exempt_marker = config.get("exempt_marker", "exception:exempt")

    for m in _GO_IF_ERR_RE.finditer(text):
        err_name = m.group(1)
        open_idx = text.find("{", m.end() - 1)
        if open_idx == -1:
            continue
        close_idx = _find_matching_brace(text, open_idx)
        if close_idx is None:
            continue
        body = text[open_idx + 1 : close_idx]
        start_line = _line_of_offset(text, m.start())
        end_line = _line_of_offset(text, close_idx)
        block = Block(header=m.group(0), body=body, start_line=start_line, end_line=end_line)

        if not _block_overlaps_added(block, added):
            continue
        if _has_exempt_nearby(text, block, exempt_marker):
            continue
        if has_any(body, markers):
            continue

        stripped = re.sub(r"//.*", "", body).strip()
        if not stripped:
            continue  # not a realistic Go shape; skip rather than risk a false positive

        statements = [s.strip() for s in stripped.split("\n") if s.strip()]
        # "handled" if err is referenced anywhere in a return/wrap (e.g. `return
        # fmt.Errorf("...: %w", err)` or `return err`) OR any recording marker
        # matched above. Only flag the pure swallow shape: every statement is a
        # bare `return` (optionally with non-err literals) and err never
        # appears in the body at all beyond the if-condition.
        if err_name in body:
            continue

        # Only flag when the block explicitly converts the error into an
        # explicit nil/success return (e.g. `return nil` or `return result,
        # nil`) — that is the "I noticed the error and threw it away" shape.
        # A BARE `return` with no value is deliberately NOT flagged: it is a
        # common Go guard-clause idiom ("bail out of this optional/logging
        # helper, the error was already handled by the caller/elsewhere") and
        # flagging it produced false positives on exactly that pattern in this
        # codebase (see docs). Prefer the false negative.
        explicit_nil_return_re = re.compile(r"^return\s+.*\bnil\b.*$")
        return_only = all(re.match(r"^return(\s+.*)?$", s) for s in statements)
        has_explicit_nil_return = any(explicit_nil_return_re.match(s) for s in statements)
        if return_only and has_explicit_nil_return:
            findings.append(
                Finding(
                    language="go",
                    kind="swallowed_err",
                    severity="FAIL",
                    file=relpath,
                    line=start_line,
                    reason=(
                        f"`if {err_name} != nil {{ ... }}` returns without wrapping, logging, or "
                        f"otherwise referencing `{err_name}`"
                    ),
                    fix_hint=(
                        f"return fmt.Errorf(\"...: %w\", {err_name}) or slog.Error(...)/logger.Error(...) "
                        f"before returning, or add `// {exempt_marker} <reason>`"
                    ),
                )
            )

    for lineno, line in enumerate(text.split("\n"), start=1):
        dm = _GO_DISCARD_RE.match(line)
        if not dm:
            continue
        if added is not None and lineno not in added:
            continue
        if exempt_marker in line:
            continue
        if _GO_SAFE_RECEIVER_RE.search(line):
            continue
        # look one line back for a safe-receiver call producing this err (common
        # `err := foo.Close(); _ = err` two-liner) or a `defer func() { ... }()`
        # wrapper on the same/adjacent line.
        prev_line = text.split("\n")[lineno - 2] if lineno >= 2 else ""
        if _GO_SAFE_RECEIVER_RE.search(prev_line):
            continue
        findings.append(
            Finding(
                language="go",
                kind="discarded_err",
                severity="FAIL",
                file=relpath,
                line=lineno,
                reason=f"`{line.strip()}` discards an error with a blank-identifier assignment",
                fix_hint=(
                    "log it (slog.Error/logger.Error) or return it wrapped instead of discarding, or add "
                    f"`// {exempt_marker} <reason>` (Close()/Rollback() discards are allowed and not flagged)"
                ),
            )
        )

    return findings


# --------------------------------------------------------------------------
# Bare exempt-marker check (no reason given)
# --------------------------------------------------------------------------
#
# `// exception:exempt <reason>` is documented (see module docstring) as an
# escape hatch whose "reason is not validated for content, only presence" —
# but that meant a bare `// exception:exempt` with NO reason text at all was
# silently accepted, which is indistinguishable from someone reaching for the
# marker just to make the guard stop complaining. This check makes the
# reason mandatory: at least one non-whitespace character must follow the
# marker on the same line.

_BARE_EXEMPT_RE_TEMPLATE = r"{marker}\s*(\S.*)?\s*$"


def check_bare_exempt_markers(
    text: str, relpath: str, added: set[int] | None, exempt_marker: str
) -> list[Finding]:
    findings: list[Finding] = []
    pattern = re.compile(re.escape(exempt_marker) + r"([ \t]*)(.*)$")
    for lineno, line in enumerate(text.split("\n"), start=1):
        m = pattern.search(line)
        if not m:
            continue
        if added is not None and lineno not in added:
            continue
        remainder = m.group(2).strip()
        if remainder:
            continue
        findings.append(
            Finding(
                language="kotlin" if relpath.endswith(".kt") else "go",
                kind="bare_exempt_marker",
                severity="FAIL",
                file=relpath,
                line=lineno,
                reason=(
                    f"`{exempt_marker}` on this line has no reason text after it — a bare "
                    "exempt marker is indistinguishable from reaching for the escape hatch just "
                    "to silence the guard"
                ),
                fix_hint=f"add a reason after the marker, e.g. `{exempt_marker} <why this is genuinely safe>`",
            )
        )
    return findings


# --------------------------------------------------------------------------
# Runner
# --------------------------------------------------------------------------


def is_excluded(relpath: str, exclude_globs: Iterable[str]) -> bool:
    return matches_any_glob(relpath, exclude_globs)


def run(
    repo: Path,
    config: dict,
    base: str | None,
    staged: bool,
    scan_all: bool,
    warn=lambda msg: None,
    only_kinds: list[str] | None = None,
) -> Report:
    kt_cfg = config.get("kotlin", {})
    go_cfg = config.get("go", {})
    exempt_marker = config.get("exempt_marker", "exception:exempt")

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
                "staged changes" if staged else f"diff vs {base or 'origin/main (or HEAD~1 fallback)'}"
            )

    report = Report(scanned_files=len(candidates), scope_description=scope_description)

    for status, relpath in candidates:
        if status == "D":
            continue

        is_kt = matches_any_glob(relpath, kt_cfg.get("path_globs", []))
        is_go = matches_any_glob(relpath, go_cfg.get("path_globs", []))
        if not (is_kt or is_go):
            continue

        if is_kt and is_excluded(relpath, kt_cfg.get("exclude_globs", [])):
            continue
        if is_go and is_excluded(relpath, go_cfg.get("exclude_globs", [])):
            continue

        file_path = repo / relpath
        if not file_path.is_file():
            continue
        text = read_text(file_path)
        if not text:
            continue

        added = added_line_numbers(repo, relpath, base, staged, scan_all)

        if is_kt:
            report.findings.extend(check_kotlin(text, relpath, added, config))
        if is_go:
            report.findings.extend(check_go(text, relpath, added, config))
        report.findings.extend(check_bare_exempt_markers(text, relpath, added, exempt_marker))

    if only_kinds is not None:
        report.findings = [f for f in report.findings if f.kind in only_kinds]

    return report


# --------------------------------------------------------------------------
# CLI output
# --------------------------------------------------------------------------


def format_text_report(report: Report) -> str:
    lines = [f"exception-guard: scope = {report.scope_description}"]
    if not report.findings:
        lines.append("exception-guard: PASS — no swallowed exceptions on changed surfaces")
        return "\n".join(lines)

    for f in report.findings:
        lines.append(f"[{f.severity}] {f.language}:{f.kind} {f.file}:{f.line}")
        lines.append(f"    reason: {f.reason}")
        lines.append(f"    fix:    {f.fix_hint}")

    fails = [f for f in report.findings if f.severity == "FAIL"]
    lines.append("")
    lines.append(f"exception-guard: {len(fails)} FAIL")
    lines.append("exception-guard: BLOCKED — resolve FAIL findings above before committing")
    lines.append(
        "exception-guard: escape hatch: add `// exception:exempt <reason>` if this catch/error "
        "path genuinely has nothing to record"
    )
    return "\n".join(lines)


def format_json_report(report: Report) -> str:
    payload = {
        "scope": report.scope_description,
        "scanned_candidates": report.scanned_files,
        "findings": [
            {
                "language": f.language,
                "kind": f.kind,
                "severity": f.severity,
                "file": f.file,
                "line": f.line,
                "reason": f.reason,
                "fix_hint": f.fix_hint,
            }
            for f in report.findings
        ],
        "fail_count": sum(1 for f in report.findings if f.severity == "FAIL"),
        "blocked": report.has_blocking,
    }
    return json.dumps(payload, indent=2)


# --------------------------------------------------------------------------
# --self-test
# --------------------------------------------------------------------------


def _self_test_config() -> dict:
    return load_config()


def _run_self_test() -> int:
    config = _self_test_config()
    failures: list[str] = []

    def expect_findings(name: str, text: str, checker, expected_kinds: set[str]):
        added = set(range(1, text.count("\n") + 2))
        findings = checker(text, f"fixture/{name}", added, config)
        got_kinds = {f.kind for f in findings}
        if got_kinds != expected_kinds:
            failures.append(f"{name}: expected kinds {expected_kinds}, got {got_kinds}")

    def expect_none(name: str, text: str, checker):
        added = set(range(1, text.count("\n") + 2))
        findings = checker(text, f"fixture/{name}", added, config)
        if findings:
            failures.append(f"{name}: expected no findings, got {[f.kind for f in findings]}")

    def expect_none_out_of_scope(name: str, text: str, checker):
        # No lines marked as added -> nothing should fire even for a real violation.
        findings = checker(text, f"fixture/{name}", set(), config)
        if findings:
            failures.append(f"{name} (out-of-scope): expected no findings, got {[f.kind for f in findings]}")

    # --- Kotlin violations ---
    expect_findings(
        "kt_empty_catch",
        "fun f() {\n  try {\n    risky()\n  } catch (e: IOException) {\n  }\n}\n",
        check_kotlin,
        {"empty_catch"},
    )
    expect_findings(
        "kt_log_only_catch",
        "fun f() {\n  try {\n    risky()\n  } catch (e: IOException) {\n    Log.e(TAG, \"boom\", e)\n  }\n}\n",
        check_kotlin,
        {"log_only_catch"},
    )
    expect_findings(
        "kt_swallowed_return",
        "fun f(): Boolean {\n  try {\n    risky()\n  } catch (e: IOException) {\n    return false\n  }\n}\n",
        check_kotlin,
        {"swallowed_catch"},
    )
    expect_findings(
        "kt_runcatching_getornull",
        "fun f() {\n  val x = runCatching {\n    risky()\n  }.getOrNull()\n}\n",
        check_kotlin,
        {"runcatching_discard"},
    )

    # --- Kotlin handled / exempt equivalents (should NOT fire) ---
    expect_none(
        "kt_catch_with_crashreporter",
        "fun f() {\n  try {\n    risky()\n  } catch (e: IOException) {\n    CrashReporter.recordException(e, \"risky failed\")\n  }\n}\n",
        check_kotlin,
    )
    expect_none(
        "kt_catch_with_exempt",
        "fun f() {\n  try {\n    risky()\n  } catch (e: IOException) {\n    // exception:exempt cancellation is expected here\n    return\n  }\n}\n",
        check_kotlin,
    )
    expect_none(
        "kt_runcatching_with_onfailure",
        "fun f() {\n  val x = runCatching {\n    risky()\n  }.onFailure { CrashReporter.recordException(it, \"x\") }.getOrNull()\n}\n",
        check_kotlin,
    )

    # --- Kotlin out-of-scope (no added lines) — legacy violation must NOT fire ---
    expect_none_out_of_scope(
        "kt_legacy_empty_catch",
        "fun f() {\n  try {\n    risky()\n  } catch (e: IOException) {\n  }\n}\n",
        check_kotlin,
    )

    # --- Go violations ---
    expect_findings(
        "go_swallowed_explicit_nil_return",
        'func f() error {\n\terr := risky()\n\tif err != nil {\n\t\treturn nil\n\t}\n\treturn nil\n}\n',
        check_go,
        {"swallowed_err"},
    )
    expect_findings(
        "go_discard_err",
        'func f() {\n\t_, err := risky()\n\t_ = err\n}\n',
        check_go,
        {"discarded_err"},
    )

    # --- Go handled / exempt equivalents (should NOT fire) ---
    expect_none(
        "go_wrapped_err",
        'func f() error {\n\terr := risky()\n\tif err != nil {\n\t\treturn fmt.Errorf("risky: %w", err)\n\t}\n\treturn nil\n}\n',
        check_go,
    )
    expect_none(
        "go_logged_err",
        'func f() error {\n\terr := risky()\n\tif err != nil {\n\t\tslog.Error("risky failed", "err", err)\n\t\treturn nil\n\t}\n\treturn nil\n}\n',
        check_go,
    )
    expect_none(
        "go_exempt_err",
        'func f() error {\n\terr := risky()\n\tif err != nil {\n\t\t// exception:exempt best-effort cleanup, caller does not need this\n\t\treturn nil\n\t}\n\treturn nil\n}\n',
        check_go,
    )
    expect_none(
        "go_bare_return_guard_clause",
        # Deliberately NOT detected: bare `return` (no value) is a common Go
        # guard-clause idiom where the error was already handled/logged by the
        # caller or elsewhere in the flow; this exact shape produced real
        # false positives in backend/internal/{sop,weighing}/adapters/http
        # (success-only logging helpers) during the initial working-tree run.
        'func logOnSuccess(err error) {\n\tif err != nil {\n\t\treturn\n\t}\n\tlogSuccess()\n}\n',
        check_go,
    )
    expect_none(
        "go_safe_close_discard",
        'func f() {\n\tdefer func() {\n\t\t_ = resp.Body.Close()\n\t}()\n}\n',
        check_go,
    )

    # --- Go out-of-scope (no added lines) — legacy violation must NOT fire ---
    expect_none_out_of_scope(
        "go_legacy_swallowed",
        'func f() error {\n\terr := risky()\n\tif err != nil {\n\t\treturn nil\n\t}\n\treturn nil\n}\n',
        check_go,
    )

    # --- Kotlin cancellation-swallow (new rule, always hard-fail, never exempt) ---
    expect_findings(
        "kt_cancellation_swallowed",
        "fun f() {\n  try {\n    risky()\n  } catch (e: CancellationException) {\n    Log.d(TAG, \"cancelled\")\n  }\n}\n",
        check_kotlin,
        {"cancellation_swallowed"},
    )
    expect_none(
        "kt_cancellation_rethrown",
        "fun f() {\n  try {\n    risky()\n  } catch (e: CancellationException) {\n    Log.d(TAG, \"cancelled\")\n    throw e\n  }\n}\n",
        check_kotlin,
    )
    # Exempt marker must NOT suppress cancellation_swallowed — it is a hard rule.
    expect_findings(
        "kt_cancellation_swallowed_with_exempt_marker_still_fires",
        "fun f() {\n  try {\n    risky()\n  } catch (e: CancellationException) {\n    // exception:exempt cancellation is fine here\n  }\n}\n",
        check_kotlin,
        {"cancellation_swallowed"},
    )

    # --- Bare exempt marker (no reason) ---
    bare_findings = check_bare_exempt_markers(
        "fun f() {\n  // exception:exempt\n  risky()\n}\n", "fixture/kt_bare_exempt", {2}, "exception:exempt"
    )
    if {f.kind for f in bare_findings} != {"bare_exempt_marker"}:
        failures.append(f"kt_bare_exempt: expected bare_exempt_marker, got {[f.kind for f in bare_findings]}")
    reasoned_findings = check_bare_exempt_markers(
        "fun f() {\n  // exception:exempt this is deliberate\n  risky()\n}\n",
        "fixture/kt_reasoned_exempt",
        {2},
        "exception:exempt",
    )
    if reasoned_findings:
        failures.append(f"kt_reasoned_exempt: expected no findings, got {[f.kind for f in reasoned_findings]}")

    # --- Test-file exclusion (via the runner's glob exclusion, checked directly) ---
    if not is_excluded("apps/goatos-android/foo/FooTest.kt", config["kotlin"]["exclude_globs"]):
        failures.append("test-file exclusion: FooTest.kt should be excluded by kotlin exclude_globs")
    if not is_excluded("backend/internal/foo/foo_test.go", config["go"]["exclude_globs"]):
        failures.append("test-file exclusion: foo_test.go should be excluded by go exclude_globs")

    print(f"exception-guard --self-test: {len(failures)} failure(s)")
    if failures:
        for f in failures:
            print(f"  FAIL: {f}")
        return 1
    print("exception-guard --self-test: PASS — all fixtures behaved as expected")
    return 0


# --------------------------------------------------------------------------
# CLI
# --------------------------------------------------------------------------


def main(argv: list[str] | None = None) -> int:
    import os

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--base",
        default=os.environ.get("EXCEPTION_GUARD_BASE"),
        help="Diff base ref, e.g. origin/main (env: EXCEPTION_GUARD_BASE)",
    )
    parser.add_argument("--all", action="store_true", help="Full repository scan")
    parser.add_argument("--staged", action="store_true", help="Diff-scope against the git index")
    parser.add_argument("--json", action="store_true", help="Emit JSON instead of text")
    parser.add_argument("--config", default=str(DEFAULT_CONFIG_PATH), help="Path to config.json")
    parser.add_argument("--repo", default=str(REPO_ROOT), help="Repo root override (mainly for tests)")
    parser.add_argument("--self-test", action="store_true", help="Run the inline fixture self-test suite")
    parser.add_argument(
        "--only-kinds",
        default=None,
        help=(
            "Comma-separated finding kinds to scope this run to (e.g. "
            "'empty_catch,log_only_catch,swallowed_catch,runcatching_discard,swallowed_err,discarded_err' "
            "or 'cancellation_swallowed,bare_exempt_marker'). Used to keep the original "
            "exception-guard-ratchet baseline stable when the two new rule kinds were added — "
            "see tools/ci/ratchet-guard.exception.json vs tools/ci/ratchet-guard.exception-v2.json."
        ),
    )
    args = parser.parse_args(argv)

    if args.self_test:
        return _run_self_test()

    if args.all and args.staged:
        print("exception-guard: --all and --staged are mutually exclusive", file=sys.stderr)
        return 2

    repo = Path(args.repo).resolve()
    config = load_config(Path(args.config))
    only_kinds = [k.strip() for k in args.only_kinds.split(",")] if args.only_kinds else None

    warnings: list[str] = []
    report = run(
        repo=repo,
        config=config,
        base=args.base,
        staged=args.staged,
        scan_all=args.all,
        warn=warnings.append,
        only_kinds=only_kinds,
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
