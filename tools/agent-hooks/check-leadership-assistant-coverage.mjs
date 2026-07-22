#!/usr/bin/env node
import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import process from "node:process";

const COVERAGE_FILES = [
  "docs/ceo-ai/ceo-chatbot-purpose-and-build-plan.md",
  "docs/ceo-ai/mcp-toolbox-plan.md",
  "docs/ceo-ai/mcp-toolbox-tools.yaml",
  "context/agents/ceo-bot-analytics-context.md",
];

const TRIGGER_PREFIXES = [
  "backend/internal/",
  "backend/migrations/",
  "backend/db/",
  "contracts/openapi/",
  "contracts/schemas/",
  "apps/admin-web/app/",
  "apps/admin-web/features/",
  "apps/goatos-android/",
  "analytics/",
  "infra/",
  "deploy/",
];

const TRIGGER_EXTS = [".go", ".sql", ".yaml", ".yml", ".json", ".ts", ".tsx", ".kt"];

const IGNORE_RE = /(^|\/)(testdata|fixtures|__tests__|node_modules|build|dist|\.next)\//;
const TEST_RE = /(_test\.go|\.test\.(ts|tsx|mjs)$|\.spec\.(ts|tsx|mjs)$|Test\.kt$)/;
const DOC_RE = /(^docs\/(?!ceo-ai\/)|\.md$)/;
const COVERAGE_TOKEN_RE = /\b(leadership assistant|assistant coverage|MCP|Toolbox|ceo_ai|Gemini|Vertex|read API|read-only SQL|tool catalog)\b/i;

function hasTriggeringChange(rel) {
  if (IGNORE_RE.test(rel) || TEST_RE.test(rel)) return false;
  if (DOC_RE.test(rel) && !rel.startsWith("docs/ceo-ai/")) return false;
  if (!TRIGGER_PREFIXES.some((prefix) => rel.startsWith(prefix))) return false;
  return TRIGGER_EXTS.some((ext) => rel.endsWith(ext));
}

function isCoverageChange(rel) {
  return COVERAGE_FILES.includes(rel) || rel.startsWith("docs/ceo-ai/");
}

export function evaluateChangedFiles(files, readFile = () => "") {
  const triggers = files.filter(hasTriggeringChange);
  if (triggers.length === 0) return [];

  const coverage = files.filter(isCoverageChange);
  if (coverage.length === 0) {
    return [
      `leadership assistant coverage missing: ${triggers.length} implementation/contract/data change(s) need a matching docs/ceo-ai, context/agents, or skill update`,
    ];
  }

  const hasUsefulCoverage = coverage.some((rel) => COVERAGE_TOKEN_RE.test(readFile(rel)));
  if (!hasUsefulCoverage) {
    return [
      `leadership assistant coverage update is too weak: touched ${coverage.join(", ")} but did not mention assistant coverage, MCP/Toolbox, ceo_ai, read APIs, or SQL fallback`,
    ];
  }

  return [];
}

function changedFiles() {
  const base = process.env.LEADERSHIP_ASSISTANT_COVERAGE_BASE || process.env.GOATOS_CI_BASE || "origin/main";
  const ranges = [`${base}...HEAD`, "HEAD~1...HEAD"];
  for (const range of ranges) {
    try {
      const ref = range.split("...")[0];
      execSync(`git rev-parse --verify --quiet ${ref}^{commit}`, { stdio: "ignore" });
      return execSync(`git diff --name-only --diff-filter=ACMR ${range}`, { encoding: "utf8" })
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean);
    } catch {
      // Try the next range.
    }
  }
  return [];
}

function selfTest() {
  const bad = evaluateChangedFiles(["backend/internal/newmodule/service.go"], () => "package newmodule");
  if (bad.length === 0) throw new Error("self-test: backend module change without assistant coverage was not blocked");

  const apiBad = evaluateChangedFiles(["contracts/openapi/app-api.yaml"], () => "paths:\n  /new-module:\n    get: {}\n");
  if (apiBad.length === 0) throw new Error("self-test: API contract change without assistant coverage was not blocked");

  const good = evaluateChangedFiles(
    ["backend/internal/newmodule/service.go", "docs/ceo-ai/mcp-toolbox-plan.md"],
    (rel) => (rel.endsWith("mcp-toolbox-plan.md") ? "leadership assistant MCP Toolbox ceo_ai read API coverage" : "package newmodule"),
  );
  if (good.length !== 0) throw new Error(`self-test: valid coverage update was blocked: ${good.join("; ")}`);

  const weak = evaluateChangedFiles(
    ["backend/internal/newmodule/service.go", "docs/ceo-ai/mcp-toolbox-plan.md"],
    () => "typo fix only",
  );
  if (weak.length === 0) throw new Error("self-test: weak coverage update was not blocked");

  const genericDocs = evaluateChangedFiles(
    ["backend/internal/newmodule/service.go", "AGENTS.md", "SKILLS.md", ".agents/skills/goatos-build/SKILL.md"],
    () => "MCP Vertex read API leadership assistant",
  );
  if (genericDocs.length === 0) throw new Error("self-test: generic agent docs satisfied assistant coverage");

  console.log("leadership-assistant-coverage guard self-test passed");
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }

  const files = changedFiles();
  const errors = evaluateChangedFiles(files, (rel) => {
    try {
      return readFileSync(rel, "utf8");
    } catch {
      return "";
    }
  });

  if (errors.length > 0) {
    console.error("leadership-assistant-coverage guard failed:");
    for (const error of errors) console.error(`- ${error}`);
    console.error("");
    console.error("Fix: update the leadership assistant docs/context/tool catalog, or document an explicit exclusion.");
    process.exit(1);
  }

  console.log("leadership-assistant-coverage guard passed");
}

main();
