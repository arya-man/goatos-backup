#!/usr/bin/env node
// check-screen-view-authoring.mjs — authoring-time screen-view guard.
//
// Thin Node wrapper around `python3 tools/telemetry-guard/telemetry-guard.py
// --only-surfaces screen_view` (see that script for the actual detection
// logic — this file intentionally does NOT reimplement it, to avoid the two
// checks drifting apart). Scoped ONLY to the `screen_view` surface (every
// Compose top-level screen/route destination must emit a screen-view event)
// so it stays fast and single-purpose enough to run on every edit to a
// *Screen.kt/*Route.kt file, not just at `make ci-local` time.
//
// Intended wiring (this repo's Claude/Codex PostToolUse hooks are configured
// in .claude/settings.json / .codex/hooks.json — see tools/agent-hooks/README
// and AGENTS.md for the actual wiring mechanism used elsewhere in this repo,
// e.g. tools/docs-graph/post-edit-hook.sh). This script was NOT live-wired
// into a PostToolUse hook as part of this change (that requires editing
// .claude/settings.json, which is outside this change's allowed path scope —
// tools/telemetry-guard/**, tools/exception-guard/**, tools/ci/**,
// tools/agent-hooks/**, docs/**). To wire it:
//
//   1. Add a PostToolUse matcher for Edit/Write on `apps/goatos-android/**/*
//      Screen.kt` (and `*Route.kt`) to .claude/settings.json (or the
//      project's hook config), pointing at this script.
//   2. The hook receives the touched file path; pass it straight through as
//      the single positional arg (falls back to git-diff-vs-origin/main
//      scoping if omitted, same as the guard's own default CLI behavior).
//   3. A non-zero exit should surface the guard's stdout to the authoring
//      agent/session as a blocking finding, exactly like any other
//      PostToolUse guard in tools/agent-hooks/ already wired this way (see
//      e.g. check-refresh-binding.mjs for the general shape, though that one
//      is a native-JS check rather than a Python delegate).
//
// Until wired, this script is reachable at authoring time by manual/CI
// invocation (`node tools/agent-hooks/check-screen-view-authoring.mjs
// <path/to/FooScreen.kt>`) and via `make telemetry-guard` /
// `make telemetry-guard-ratchet-v2` (whole-tree, ratcheted) at commit time —
// so a missing screen-view event is never landable even if the authoring-time
// hook is not wired in a given agent harness.

import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const guard = path.join(repoRoot, "tools/telemetry-guard/telemetry-guard.py");

function runGuard(extraArgs) {
  const result = spawnSync("python3", [guard, "--only-surfaces", "screen_view", "--json", ...extraArgs], {
    cwd: repoRoot,
    encoding: "utf-8",
  });
  return result;
}

function selfTest() {
  // Not a full fixture suite (that lives in
  // tools/telemetry-guard/test_telemetry_guard.py and is run by `make
  // telemetry-guard`) — this only proves the wrapper actually invokes the
  // guard and can parse its JSON contract, since a wrapper that silently
  // fails open (bad path, guard crashes, JSON shape changes) is worse than
  // no wrapper.
  const result = runGuard(["--all"]);
  if (result.status !== 0 && result.status !== 1) {
    console.error(`check-screen-view-authoring --self-test: FAIL — guard exited ${result.status}, expected 0 or 1`);
    console.error(result.stderr);
    return 1;
  }
  let payload;
  try {
    payload = JSON.parse(result.stdout);
  } catch (err) {
    console.error("check-screen-view-authoring --self-test: FAIL — guard did not emit valid JSON");
    console.error(result.stdout);
    return 1;
  }
  const requiredKeys = ["scope", "findings", "fail_count", "blocked"];
  const missing = requiredKeys.filter((k) => !(k in payload));
  if (missing.length) {
    console.error(`check-screen-view-authoring --self-test: FAIL — JSON payload missing keys: ${missing.join(", ")}`);
    return 1;
  }
  console.log("check-screen-view-authoring --self-test: PASS — wrapper invokes the guard and parses its JSON contract");
  return 0;
}

function main() {
  const args = process.argv.slice(2);
  if (args.includes("--self-test")) {
    process.exit(selfTest());
  }

  const filePath = args.find((a) => !a.startsWith("--"));
  const guardArgs = filePath ? ["--staged"] : [];
  const result = runGuard(guardArgs);

  if (result.status === null) {
    console.error("check-screen-view-authoring: guard process failed to start");
    console.error(result.stderr);
    process.exit(2);
  }

  let payload;
  try {
    payload = JSON.parse(result.stdout);
  } catch {
    console.error("check-screen-view-authoring: guard did not emit valid JSON");
    console.error(result.stdout, result.stderr);
    process.exit(2);
  }

  const relevant = filePath
    ? payload.findings.filter((f) => filePath.endsWith(f.file) || f.file.endsWith(filePath))
    : payload.findings;

  if (relevant.length === 0) {
    console.log("check-screen-view-authoring: PASS — no missing screen-view events" + (filePath ? ` on ${filePath}` : ""));
    process.exit(0);
  }

  for (const f of relevant) {
    console.log(`[${f.severity}] ${f.file}: ${f.reason}`);
    console.log(`    fix: ${f.fix_hint}`);
  }
  process.exit(relevant.some((f) => f.severity === "FAIL") ? 1 : 0);
}

main();
