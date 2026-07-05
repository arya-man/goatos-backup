#!/usr/bin/env bash
# PreToolUse hook (Claude AND Codex): route only large, display-style
# `git diff` / `git show` output through RTK before raw diffs hit context.
#
# This intentionally does NOT touch programmatic git diff modes such as
# --quiet/--exit-code/--check/--name-only/--stat/--numstat/--raw.
# Set GOATOS_RTK=0 or GOATOS_RTK_GIT_DIFF=0 to disable. Tune the size gate with
# GOATOS_RTK_MIN_BYTES (default: 50000). Set it to 0 to route every safe display
# diff/show command.
set -u

[ "${GOATOS_RTK:-1}" = "0" ] && exit 0
[ "${GOATOS_RTK_GIT_DIFF:-1}" = "0" ] && exit 0
command -v rtk >/dev/null 2>&1 || exit 0

INPUT="$(cat 2>/dev/null || true)"

HOOK_INPUT="$INPUT" python3 - <<'PY'
import json
import os
import shlex
import subprocess
import sys


UNSAFE_DIFF_FLAGS = {
    "--quiet",
    "--exit-code",
    "--check",
    "--name-only",
    "--name-status",
    "--stat",
    "--numstat",
    "--shortstat",
    "--raw",
    "--summary",
    "--dirstat",
    "--compact-summary",
    "--ext-diff",
    "--textconv",
}
UNSAFE_SHOW_FLAGS = UNSAFE_DIFF_FLAGS | {
    "--no-patch",
    "--format",
    "--pretty",
}
GLOBAL_OPTS_WITH_VALUE = {"-C", "-c", "--git-dir", "--work-tree"}
GLOBAL_OPTS_NO_VALUE = {
    "--no-pager",
    "--no-optional-locks",
    "--bare",
    "--literal-pathspecs",
}
CONTROL_MARKERS = ("\n", "&&", "||", "|", ";", ">", "<", "`", "$(")
SHELL_TOOLS = {
    "Bash",
    "Shell",
    "exec_command",
    "functions.exec_command",
    "unified_exec",
    "command_execution",
    "local_shell",
}


def payload():
    raw = os.environ.get("HOOK_INPUT", "") or "{}"
    try:
        data = json.loads(raw)
    except Exception:
        data = {}
    if not isinstance(data, dict):
        data = {}
    tool = data.get("tool_name") or data.get("tool") or data.get("name") or ""
    ti = data.get("tool_input") or data.get("input") or data.get("arguments") or {}
    if isinstance(ti, str):
        try:
            ti = json.loads(ti)
        except Exception:
            ti = {}
    if not isinstance(ti, dict):
        ti = {}
    if not ti:
        try:
            ti = json.loads(os.environ.get("CLAUDE_TOOL_INPUT", "{}")) or {}
        except Exception:
            ti = {}
        if isinstance(ti, str):
            try:
                ti = json.loads(ti)
            except Exception:
                ti = {}
        if not isinstance(ti, dict):
            ti = {}
    cwd = data.get("cwd") or ti.get("cwd") or os.getcwd()

    def command_string(value):
        if isinstance(value, list):
            parts = [str(v) for v in value]
            if parts and parts[0] in {"bash", "sh", "zsh", "/bin/bash", "/bin/sh", "/bin/zsh"}:
                return parts[-1] if parts else ""
            return " ".join(parts)
        return str(value) if value is not None else ""

    cmd = command_string(ti.get("command") or ti.get("cmd") or data.get("command") or data.get("argv"))
    tool = tool or ("Bash" if cmd else "")
    return str(tool), str(cmd), str(cwd)


def has_unsafe_flag(args, unsafe):
    for arg in args:
        if arg in unsafe:
            return True
        if any(arg.startswith(flag + "=") for flag in unsafe):
            return True
        if arg in ("-s", "--no-patch"):
            return True
    return False


def classify(cmd):
    if any(marker in cmd for marker in CONTROL_MARKERS):
        return None
    try:
        parts = shlex.split(cmd)
    except ValueError:
        return None
    if not parts or parts[0] in {"rtk", "command"}:
        return None
    if parts[0] != "git":
        return None

    global_args = []
    i = 1
    while i < len(parts):
        token = parts[i]
        if token in ("diff", "show"):
            subcommand = token
            rest = parts[i + 1 :]
            unsafe = UNSAFE_DIFF_FLAGS if subcommand == "diff" else UNSAFE_SHOW_FLAGS
            if has_unsafe_flag(rest, unsafe):
                return None
            return ["git", *global_args, subcommand, *rest], ["rtk", "git", *global_args, subcommand, *rest]
        if token in GLOBAL_OPTS_WITH_VALUE:
            if i + 1 >= len(parts):
                return None
            global_args.extend([token, parts[i + 1]])
            i += 2
            continue
        if any(token.startswith(opt + "=") for opt in GLOBAL_OPTS_WITH_VALUE):
            global_args.append(token)
            i += 1
            continue
        if token in GLOBAL_OPTS_NO_VALUE:
            global_args.append(token)
            i += 1
            continue
        return None
    return None


def output_exceeds(argv, cwd, threshold):
    if threshold <= 0:
        return True
    env = os.environ.copy()
    env.update({"GIT_PAGER": "cat", "NO_COLOR": "1", "CLICOLOR": "0", "TERM": "dumb"})
    try:
        proc = subprocess.Popen(
            argv,
            cwd=cwd,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            env=env,
        )
    except Exception:
        return False
    total = 0
    try:
        assert proc.stdout is not None
        while True:
            chunk = proc.stdout.read(8192)
            if chunk:
                total += len(chunk)
                if total > threshold:
                    proc.terminate()
                    try:
                        proc.wait(timeout=2)
                    except subprocess.TimeoutExpired:
                        proc.kill()
                    return True
            else:
                proc.wait(timeout=2)
                return False
    except Exception:
        try:
            proc.kill()
        except Exception:
            pass
        return False


tool, command, cwd = payload()
if tool not in SHELL_TOOLS or not command:
    sys.exit(0)

classified = classify(command)
if not classified:
    sys.exit(0)
raw_argv, rtk_argv = classified

try:
    min_bytes = int(os.environ.get("GOATOS_RTK_MIN_BYTES", "50000"))
except ValueError:
    min_bytes = 50000

if not output_exceeds(raw_argv, cwd, min_bytes):
    sys.exit(0)

env = os.environ.copy()
env.update({"GIT_PAGER": "cat", "NO_COLOR": "1", "CLICOLOR": "0", "TERM": "dumb"})
result = subprocess.run(rtk_argv, cwd=cwd, text=True, capture_output=True, env=env)

print("RTK-GIT-DIFF AUTO-ROUTE", file=sys.stderr)
print("Original raw git output exceeded GOATOS_RTK_MIN_BYTES and was not run into context.", file=sys.stderr)
print("Ran: " + shlex.join(rtk_argv), file=sys.stderr)
print("Disable with GOATOS_RTK=0 or tune with GOATOS_RTK_MIN_BYTES.", file=sys.stderr)
print("", file=sys.stderr)
if result.stdout:
    print(result.stdout, end="" if result.stdout.endswith("\n") else "\n", file=sys.stderr)
if result.stderr:
    print(result.stderr, end="" if result.stderr.endswith("\n") else "\n", file=sys.stderr)
if result.returncode != 0:
    print(f"[rtk exited {result.returncode}]", file=sys.stderr)
sys.exit(2)
PY
