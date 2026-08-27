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
const MCP_MAIN = path.join(repo, "backend/cmd/mcp/main.go");
const GOLDEN_DIR = path.join(repo, "tools/ceo-ai/eval/golden");
const EXTERNAL_MCP_REQUIRED_TOOLS = [
  "get_vaccination_today",
  "get_action_center",
  "get_verification_backlog",
  "get_feed_today",
  "get_procurement_pipeline",
  "get_sales_overview",
  "get_sales_deals",
  "get_counts_summary",
  "get_health_today",
  "get_health_work_items",
  "get_milk_feeding_today",
  "get_workforce_coverage",
  "get_weighing_progress",
  "get_weighing_growth_adg",
  "get_weighing_shed_weights",
  "get_weighing_process_state",
  "get_weighing_weight_demographics",
];
const GOLDEN_EXTERNAL_MCP_EXPECTATIONS = {
  vaccination_live_tracker: ["get_vaccination_today", "GET /vaccination/live-tracker"],
  verification_queue: ["get_verification_backlog", "GET /verification/queue"],
  counts_breakdown: ["get_counts_summary", "GET /counts/breakdown"],
  health_work_items: ["get_health_today", "get_health_work_items", "get_milk_feeding_today", "GET /app/health/work-items", "GET /app/counts/milk-feeding/tasks"],
  workforce_coverage: ["get_workforce_coverage", "GET /admin/roster/coverage"],
  feed_direction_today: ["get_feed_today", "GET /feed-direction/preview"],
  feed_blocked_config_gaps: ["get_feed_today", "GET /feed-direction/preview"],
  procurement_open_loads: ["get_procurement_pipeline", "GET /procurement/source-entry/loads"],
  sales_overview: ["get_sales_overview", "GET /sales/overview"],
  sales_deals: ["get_sales_deals", "GET /sales/deals"],
  action_center_queue: ["get_action_center", "GET /action-center/obligations"],
  weighing_progress: ["get_weighing_progress", "GET /weighing/campaigns"],
  weighing_process_state: ["get_weighing_process_state", "GET /weighing/process-state"],
  weighing_growth_adg: ["get_weighing_growth_adg", "GET /weighing/leadership/growth"],
  weighing_shed_weights: ["get_weighing_shed_weights", "GET /weighing/shed-weights"],
  weighing_weight_demographics: ["get_weighing_weight_demographics", "GET /weighing/weight-demographics"],
};
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

function checkExternalMCPTools() {
  const text = readFileSync(MCP_MAIN, "utf8");
  const problems = [];
  for (const tool of EXTERNAL_MCP_REQUIRED_TOOLS) {
    if (!text.includes(`Name:        "${tool}"`) && !text.includes(`"name":        "${tool}"`)) {
      problems.push(`backend/cmd/mcp/main.go: missing external MCP typed tool ${tool}`);
    }
  }
  const coverage = readFileSync(path.join(repo, "docs/ceo-ai/coverage-matrix.md"), "utf8");
  const integration = readFileSync(path.join(repo, "docs/ceo-ai/external-mcp-integration.md"), "utf8");
  for (const tool of EXTERNAL_MCP_REQUIRED_TOOLS) {
    if (!coverage.includes(`external MCP:${tool}`) && tool !== "get_vaccination_today") {
      problems.push(`docs/ceo-ai/coverage-matrix.md: missing external MCP coverage marker for ${tool}`);
    }
    if (!integration.includes(`\`${tool}\``)) {
      problems.push(`docs/ceo-ai/external-mcp-integration.md: missing tool catalog entry for ${tool}`);
    }
  }
  if (!/Feed MCP answers planned\/issued feed[\s\S]*feed_adherence/i.test(integration)) {
    problems.push("docs/ceo-ai/external-mcp-integration.md: must explicitly state feed actuals/adherence are not covered by external MCP yet");
  }
  if (!/reorder thresholds are not configured/i.test(integration)) {
    problems.push("docs/ceo-ai/external-mcp-integration.md: must explicitly state inventory reorder thresholds are not configured yet");
  }
  if (!/feed actuals\/adherence are not covered/i.test(text)) {
    problems.push("backend/cmd/mcp/main.go: get_feed_today must warn that feed actuals/adherence are not covered yet");
  }
  return problems;
}

function checkGoldenExternalMCPMappings() {
  const problems = [];
  const seenExpectedClasses = new Set();
  for (const f of readdirSync(GOLDEN_DIR)) {
    if (!f.endsWith(".json")) continue;
    const rel = `tools/ceo-ai/eval/golden/${f}`;
    let cases;
    try {
      cases = JSON.parse(readFileSync(path.join(GOLDEN_DIR, f), "utf8"));
    } catch (error) {
      problems.push(`${rel}: invalid JSON: ${error.message}`);
      continue;
    }
    if (!Array.isArray(cases)) {
      problems.push(`${rel}: expected top-level array`);
      continue;
    }
    for (const testCase of cases) {
      const klass = testCase && testCase.class;
      const expected = GOLDEN_EXTERNAL_MCP_EXPECTATIONS[klass];
      const actual = testCase?.expect?.external_mcp_tools_any_of;
      if (!expected) continue;
      seenExpectedClasses.add(klass);
      if (!Array.isArray(actual)) {
        problems.push(`${rel}:${testCase.id || klass}: missing external_mcp_tools_any_of for ${klass}`);
        continue;
      }
      for (const item of expected) {
        if (!actual.includes(item)) {
          problems.push(`${rel}:${testCase.id || klass}: ${klass} must include ${item} in external_mcp_tools_any_of`);
        }
      }
    }
  }
  for (const klass of Object.keys(GOLDEN_EXTERNAL_MCP_EXPECTATIONS).sort()) {
    if (!seenExpectedClasses.has(klass)) {
      problems.push(`tools/ceo-ai/eval/golden/*.json: missing golden question for required external MCP class ${klass}`);
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
  const externalMCP = checkExternalMCPTools();
  const goldenMCP = checkGoldenExternalMCPMappings();
  const problems = [...docs, ...externalMCP, ...goldenMCP];
  if (problems.length) {
    console.error("assistant-route-closure guard failed: stale assistant docs:");
    for (const p of problems) console.error(`  - ${p}`);
    process.exit(1);
  }
  console.log("assistant-route-closure guard passed");
}

main();
