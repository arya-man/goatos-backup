#!/usr/bin/env bash
# PreToolUse hook (Claude AND Codex): route known noisy verification/log
# commands through RTK before raw output hits context.
#
# This is intentionally narrower than "all shell commands". It targets tests,
# builds, typechecks, lint, and local logs, and it skips dev/watch/debug/deploy/
# migration/cloud mutation flows.
#
# Disable with GOATOS_RTK=0 or GOATOS_RTK_NOISY_COMMANDS=0.
set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE="$(cd "$HERE/../.." && pwd)"

case "$(basename "$WORKSPACE")" in
    mesha)
        [ "${MESHA_RTK:-1}" = "0" ] && exit 0
        [ "${MESHA_RTK_NOISY_COMMANDS:-1}" = "0" ] && exit 0
        RTK_PREFIX="MESHA"
        ;;
    heva)
        [ "${HEVA_RTK:-1}" = "0" ] && exit 0
        [ "${HEVA_RTK_NOISY_COMMANDS:-1}" = "0" ] && exit 0
        RTK_PREFIX="HEVA"
        ;;
    goatos)
        [ "${GOATOS_RTK:-1}" = "0" ] && exit 0
        [ "${GOATOS_RTK_NOISY_COMMANDS:-1}" = "0" ] && exit 0
        RTK_PREFIX="GOATOS"
        ;;
    *)
        exit 0
        ;;
esac

command -v rtk >/dev/null 2>&1 || exit 0

INPUT="$(cat 2>/dev/null || true)"

HOOK_INPUT="$INPUT" WORKSPACE="$WORKSPACE" RTK_PREFIX="$RTK_PREFIX" python3 - <<'PY'
import json
import os
from pathlib import Path
import re
import shlex
import subprocess
import sys

CONTROL_MARKERS = ("\n", "&&", "||", "|", ";", ">", "<", "`", "$(")
ASSIGNMENT = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*=.*$")
SHELL_TOOLS = {
    "Bash",
    "Shell",
    "exec_command",
    "functions.exec_command",
    "unified_exec",
    "command_execution",
    "local_shell",
}

TEST_SCRIPTS = {"test", "test:smoke", "test:e2e", "test:integration", "test:unit"}
ERR_SCRIPTS = {
    "build",
    "check",
    "typecheck",
    "type-check",
    "lint",
    "format:check",
    "ci",
    "ci-fast",
    "ci-protected",
    "pre-pr",
}
SCRIPT_SKIP_PARTS = {
    "dev",
    "start",
    "watch",
    "ui",
    "debug",
    "headed",
    "report",
    "deploy",
    "release",
    "publish",
    "push",
    "sync",
    "migrate",
    "migration",
    "seed",
    "reset",
    "truncate",
    "install",
    "prebuild",
    "fix",
}
MAKE_TEST_TARGETS = {"test", "test-race", "test-integration", "test-changed"}
MAKE_ERR_TARGETS = {
    "check",
    "ci-fast",
    "ci-protected",
    "pre-pr",
    "lint",
    "staticcheck",
    "fmt-check",
    "swagger-check",
    "coverage-diff",
    "policy-gate",
    "guardrails",
    "docker-storage-scripts-test",
    "validate-migrations",
    "validate-sqlc-plans",
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


def under_workspace(cwd):
    workspace = Path(os.environ["WORKSPACE"]).resolve()
    current = Path(cwd).expanduser().resolve(strict=False)
    try:
        current.relative_to(workspace)
        return True
    except ValueError:
        return False


def split_env(parts):
    env = {}
    i = 0
    if parts and parts[0] == "env":
        i = 1
    while i < len(parts) and ASSIGNMENT.match(parts[i]):
        key, value = parts[i].split("=", 1)
        env[key] = value
        i += 1
    return env, parts[i:]


def skip_script(script):
    lowered = script.lower()
    bits = re.split(r"[:._-]+", lowered)
    return any(bit in SCRIPT_SKIP_PARTS for bit in bits)


def script_kind(script):
    lowered = script.lower()
    if skip_script(lowered):
        return None
    if lowered in TEST_SCRIPTS or lowered.startswith("test:"):
        return "test"
    if (
        lowered in ERR_SCRIPTS
        or lowered.startswith("check:")
        or lowered.startswith("build:")
        or lowered.startswith("lint:")
        or lowered.startswith("typecheck:")
        or lowered.startswith("type-check:")
    ):
        return "err"
    return None


def package_manager(parts):
    binary = parts[0]
    if binary == "npm":
        i = 1
        while i < len(parts) and parts[i].startswith("-"):
            i += 2 if parts[i] in {"-w", "--workspace", "--prefix"} else 1
        if i >= len(parts):
            return None
        if parts[i] == "test":
            if has_mutating_flag(parts[i + 1 :]):
                return None
            return ["rtk", "test", *parts]
        if parts[i] == "run" and i + 1 < len(parts):
            if has_mutating_flag(parts[i + 2 :]):
                return None
            kind = script_kind(parts[i + 1])
            if kind == "test":
                return ["rtk", "test", *parts]
            if kind == "err":
                return ["rtk", "err", *parts]
        return None

    if binary == "pnpm":
        i = 1
        while i < len(parts) and parts[i].startswith("-"):
            i += 2 if parts[i] in {"-F", "--filter", "-C", "--dir"} else 1
        if i >= len(parts):
            return None
        if parts[i] in {"install", "add", "remove", "update", "upgrade", "dev", "start"}:
            return None
        script = parts[i + 1] if parts[i] == "run" and i + 1 < len(parts) else parts[i]
        rest = parts[i + 2 :] if parts[i] == "run" and i + 1 < len(parts) else parts[i + 1 :]
        if has_mutating_flag(rest):
            return None
        kind = script_kind(script)
        if kind == "test":
            return ["rtk", "test", *parts]
        if kind == "err":
            return ["rtk", "err", *parts]
        return None

    return None


def npx(parts):
    i = 1
    while i < len(parts) and parts[i].startswith("-"):
        i += 2 if parts[i] in {"-p", "--package"} else 1
    if i >= len(parts):
        return None
    tool = Path(parts[i]).name
    rest = parts[i + 1 :]
    if has_mutating_flag(rest):
        return None
    if tool == "tsc":
        return ["rtk", "tsc", *rest]
    if tool == "eslint":
        return ["rtk", "lint", *rest]
    if tool == "playwright" and rest[:1] == ["test"] and not any(x in rest for x in {"--ui", "--debug", "--headed"}):
        return ["rtk", "playwright", *rest]
    if tool in {"jest", "vitest"}:
        return ["rtk", tool, *rest]
    if tool == "expo" and rest[:1] == ["lint"]:
        return ["rtk", "err", *parts]
    return None


def direct(parts):
    binary = Path(parts[0]).name
    if binary in {"npm", "pnpm"}:
        return package_manager(parts)
    if binary == "npx":
        return npx(parts)
    if binary == "tsc":
        return ["rtk", "tsc", *parts[1:]]
    if binary == "eslint":
        if has_mutating_flag(parts[1:]):
            return None
        return ["rtk", "lint", *parts[1:]]
    if binary == "next" and parts[1:2] == ["build"]:
        return ["rtk", "next", *parts[1:]]
    if binary == "vite" and parts[1:2] == ["build"]:
        return ["rtk", "err", *parts]
    if binary in {"vitest", "jest"}:
        if binary == "vitest" and (len(parts) == 1 or parts[1] in {"watch", "--watch"}):
            return None
        if has_mutating_flag(parts[1:]):
            return None
        return ["rtk", binary, *parts[1:]]
    if binary == "playwright" and parts[1:2] == ["test"] and not any(x in parts for x in {"--ui", "--debug", "--headed"}):
        if has_mutating_flag(parts[1:]):
            return None
        return ["rtk", "playwright", *parts[1:]]
    if binary == "go" and len(parts) > 1 and parts[1] in {"test", "vet"}:
        return ["rtk", "go", *parts[1:]]
    if binary in {"pytest", "ruff", "mypy"}:
        if has_mutating_flag(parts[1:]):
            return None
        return ["rtk", binary, *parts[1:]]
    if binary == "make" and len(parts) > 1:
        target = parts[1]
        if target in MAKE_TEST_TARGETS or target.startswith("test-"):
            return ["rtk", "test", *parts]
        if target in MAKE_ERR_TARGETS:
            return ["rtk", "err", *parts]
    if binary == "docker" and parts[1:3] == ["compose", "logs"]:
        return ["rtk", "docker", *parts[1:]]
    if binary == "docker" and parts[1:2] == ["logs"]:
        return ["rtk", "docker", *parts[1:]]
    return None


def classify(command):
    if any(marker in command for marker in CONTROL_MARKERS):
        return None, None
    try:
        raw_parts = shlex.split(command)
    except ValueError:
        return None, None
    env_overrides, parts = split_env(raw_parts)
    if not parts:
        return None, None
    if parts[0] in {"rtk", "git", "graphify", "code-review-graph", "gh", "gcloud", "firebase", "terraform"}:
        return None, None
    return env_overrides, direct(parts)


def has_mutating_flag(args):
    return any(
        arg == flag or arg.startswith(flag + "=")
        for arg in args
        for flag in {"--fix", "--fix-dry-run", "--write", "--write-mode", "--update", "--update-snapshots"}
    )


tool, command, cwd = payload()
if tool not in SHELL_TOOLS or not command or not under_workspace(cwd):
    sys.exit(0)

env_overrides, rtk_argv = classify(command)
if not rtk_argv:
    sys.exit(0)

prefix = os.environ.get("RTK_PREFIX", "RTK")
dry_run = os.environ.get(f"{prefix}_RTK_NOISY_DRY_RUN") == "1" or os.environ.get("RTK_NOISY_DRY_RUN") == "1"

print("RTK-NOISY-COMMAND AUTO-ROUTE", file=sys.stderr)
print("Original noisy command was run through RTK before reaching context.", file=sys.stderr)
print("Ran: " + shlex.join(rtk_argv), file=sys.stderr)
print(f"Disable with {prefix}_RTK=0 or {prefix}_RTK_NOISY_COMMANDS=0.", file=sys.stderr)
print("", file=sys.stderr)

if dry_run:
    print("[dry run: command not executed]", file=sys.stderr)
    sys.exit(2)

env = os.environ.copy()
env.update(env_overrides)
env.update({"NO_COLOR": "1", "CLICOLOR": "0", "TERM": "dumb"})
result = subprocess.run(rtk_argv, cwd=cwd, text=True, capture_output=True, env=env)
if result.stdout:
    print(result.stdout, end="" if result.stdout.endswith("\n") else "\n", file=sys.stderr)
if result.stderr:
    print(result.stderr, end="" if result.stderr.endswith("\n") else "\n", file=sys.stderr)
if result.returncode != 0:
    print(f"[rtk exited {result.returncode}]", file=sys.stderr)
sys.exit(2)
PY
