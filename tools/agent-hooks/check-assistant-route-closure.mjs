#!/usr/bin/env node
// Route-closure guard for the Mesha Leadership Assistant.
//
// This guard is intentionally a THIN wrapper over the authoritative, source-parsing
// Go gate (backend/internal/ceoai/adapters/keywordplanner TestCatalogConsistency +
// TestGateHasTeeth) plus a docs-staleness check. Earlier this file hand-rolled its
// own planner/catalog parsers and shipped TOOTHLESS (it passed on an injected bogus
// tool name). The Go test reads the real planner rules, fallback aliases, bootstrap
// wiring, Cube bindings, and the MCP Toolbox yaml — so route/tool/wiring drift
// (e.g. feed_direction_preview vs feed_direction_today, or a planner route to an
// unwired-and-unaliased API tool) FAILS the build there. We run it here so the
// route-closure requirement is a named CI guard, and we add the docs check the Go
// test does not cover.
//
// --self-test proves teeth: it injects a bogus planner tool name and asserts the Go
// gate fails, then restores.

import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync, readdirSync, existsSync } from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

// Resolve the `go` binary absolutely — execFileSync resolves the command name
// against the PARENT process PATH (not options.env.PATH), so a minimal-PATH
// invocation (hooks/CI) otherwise fails ENOENT.
function goBin() {
  for (const p of ["/opt/homebrew/bin/go", "/usr/local/go/bin/go", "/usr/local/bin/go", "/usr/bin/go"]) {
    if (existsSync(p)) return p;
  }
  return "go";
}

// This file: <root>/tools/agent-hooks/<f> -> repo root is two dirs up.
const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
const PLANNER = path.join(repo, "backend/internal/ceoai/adapters/keywordplanner/planner.go");
const DOCS_DIR = path.join(repo, "docs/ceo-ai");
const STALE = [
  { re: /ProjectedCountFor/, why: "counts reader no longer uses ProjectedCountFor (deleted); docs are stale" },
  { re: /empty[- ]facts fallback/i, why: "empty-facts fallback replaced by ToolResult.Err + runtime retry; docs are stale" },
];

function runGoGate() {
  // Ensure `go` is findable even when invoked with a minimal PATH (CI runners
  // and hooks): prepend the common Go install dirs.
  const extraPath = ["/opt/homebrew/bin", "/usr/local/go/bin", "/usr/local/bin", "/usr/bin", "/bin"].join(":");
  const env = { ...process.env, PATH: `${extraPath}:${process.env.PATH || ""}` };
  execFileSync(
    goBin(),
    ["test", "./internal/ceoai/adapters/keywordplanner/", "-run", "TestCatalogConsistency|TestGateHasTeeth", "-count=1"],
    { cwd: path.join(repo, "backend"), stdio: "pipe", env },
  );
}

function checkDocs() {
  const problems = [];
  for (const f of readdirSync(DOCS_DIR)) {
    if (!f.endsWith(".md")) continue;
    const text = readFileSync(path.join(DOCS_DIR, f), "utf8");
    for (const s of STALE) {
      if (s.re.test(text)) problems.push(`docs/ceo-ai/${f}: ${s.why}`);
    }
  }
  return problems;
}

function selfTest() {
  const original = readFileSync(PLANNER, "utf8");
  const mutated = original.replace(
    '"feed_direction_today", "feed_direction_today"',
    '"feed_direction_today", "feed_direction_BOGUS_SELFTEST"',
  );
  if (mutated === original) {
    throw new Error("self-test could not inject a mismatch (planner shape changed) — fix the self-test");
  }
  writeFileSync(PLANNER, mutated);
  let caught = false;
  try {
    runGoGate();
  } catch {
    caught = true;
  } finally {
    writeFileSync(PLANNER, original);
  }
  if (!caught) {
    throw new Error("route-closure gate is TOOTHLESS: an injected bogus planner tool name did NOT fail the Go gate");
  }
  console.log("assistant-route-closure guard self-test passed (injected mismatch was caught)");
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }
  try {
    runGoGate();
  } catch (e) {
    console.error("assistant-route-closure guard failed: planner/catalog/wiring drift.");
    console.error(String((e && e.stdout) || (e && e.message) || e));
    process.exit(1);
  }
  const docs = checkDocs();
  if (docs.length) {
    console.error("assistant-route-closure guard failed: stale assistant docs:");
    for (const p of docs) console.error(`  - ${p}`);
    process.exit(1);
  }
  console.log("assistant-route-closure guard passed");
}

main();
