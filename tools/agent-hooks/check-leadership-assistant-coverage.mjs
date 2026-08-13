#!/usr/bin/env node
import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import process from "node:process";

const COVERAGE_MATRIX = "docs/ceo-ai/coverage-matrix.md";

const COVERAGE_FILES = [
  "docs/ceo-ai/ceo-chatbot-purpose-and-build-plan.md",
  "docs/ceo-ai/mcp-toolbox-plan.md",
  "docs/ceo-ai/mcp-toolbox-tools.yaml",
  "context/agents/ceo-bot-analytics-context.md",
  COVERAGE_MATRIX,
];

const TRIGGER_PREFIXES = [
  "backend/internal/",
  "backend/db/",
  "backend/migrations/",
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
const EXPLICIT_NON_ASSISTANT_RE =
  /^(apps\/admin-web\/(app\/api\/vaccination\/capacity-config\/route\.ts|features\/people\/positions-panel\.tsx|lib\/api\/client\.ts)|tools\/dev\/build-vaccination-hrms-fixture\.mjs)$/;

// A new surface is detected by analyzing diff content, not filenames.
// Returns { surfaces: [list], isClean: boolean }
// isClean = true means we confirmed there are no new surfaces (pure refactor)
// isClean = false means either we found new surfaces OR the diff is unclear (default to requiring coverage)
function detectNewLeadershipSurfaces(files, readDiff = () => "") {
  const surfaces = new Set();
  let isClean = true; // assume clean until proven otherwise

  // 1. New migrations: scan for CREATE TABLE statements
  for (const f of files) {
    if (f.startsWith("backend/migrations/postgres/") && f.endsWith(".sql")) {
      const diff = readDiff(f);
      if (/Collapsed clean-slate baseline generated from migrations 000001\.\.000046/.test(diff)) {
        continue;
      }
      // If diff is too short or empty, it's unclear
      if (!diff || diff.length < 5) {
        isClean = false;
        continue;
      }
      // Match CREATE TABLE statements in the diff (with or without + prefix)
      const tableMatches = diff.match(/CREATE TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:\w+\.)?(\w+)/gi);
      if (tableMatches) {
        isClean = false; // found new table
        for (const m of tableMatches) {
          const tableName = m.match(/CREATE TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:\w+\.)?(\w+)/i)?.[1];
          if (tableName) surfaces.add(`table:${tableName.toLowerCase()}`);
        }
      }
      // Check if it's a pure ALTER (refactor-only)
      if (!tableMatches && !diff.match(/CREATE\s+(VIEW|INDEX|FUNCTION|TYPE)/i)) {
        // Only ALTERs, no CREATE - likely a refactor
        isClean = true;
      } else if (tableMatches || diff.match(/CREATE\s+(VIEW|INDEX|FUNCTION|TYPE)/i)) {
        isClean = false;
      }
    }
  }

  // 2. New OpenAPI paths: scan for new "paths:" entries
  for (const f of files) {
    if ((f === "contracts/openapi/app-api.yaml" || f === "contracts/openapi/admin-web.yaml") && f.endsWith(".yaml")) {
      const diff = readDiff(f);
      // If diff is too short or empty, it's unclear
      if (!diff || diff.length < 5) {
        isClean = false;
        continue;
      }
      // Match new path entries (lines with /... that look like YAML keys)
      const pathMatches = diff.match(/(^|\n)\+?\s*(\/[a-zA-Z0-9\-_{}:]+):/gm);
      if (pathMatches) {
        isClean = false;
        for (const m of pathMatches) {
          const path = m.match(/(\/[a-zA-Z0-9\-_{}:]+):/)?.[1];
          if (path && path !== "/") surfaces.add(`path:${path}`);
        }
      }
    }
  }

  // 3. New backend handlers/functions: look for new exported functions
  for (const f of files) {
    if (f.startsWith("backend/internal/") && f.endsWith(".go") && !TEST_RE.test(f)) {
      const diff = readDiff(f);
      // If diff is too short or empty, it's unclear
      if (!diff || diff.length < 5) {
        isClean = false;
        continue;
      }
      // Match new exported functions (func ... Name(...) or func (r *Type) Name(...))
      const functionMatches = diff.match(/^\+[ \t]*func[ \t]+(?:\([^)]*\)[ \t]+)?([A-Z]\w*)[ \t]*\(/gm);
      if (functionMatches) {
        isClean = false;
        for (const m of functionMatches) {
          const functionName = m.match(/func[ \t]+(?:\([^)]*\)[ \t]+)?([A-Z]\w*)[ \t]*\(/)?.[1];
          if (functionName) surfaces.add(`func:${functionName}`);
        }
      }
    }
  }

  return { surfaces: Array.from(surfaces), isClean };
}

function hasTriggeringChange(rel) {
  if (IGNORE_RE.test(rel) || TEST_RE.test(rel)) return false;
  if (EXPLICIT_NON_ASSISTANT_RE.test(rel)) return false;
  if (DOC_RE.test(rel) && !rel.startsWith("docs/ceo-ai/")) return false;
  if (!TRIGGER_PREFIXES.some((prefix) => rel.startsWith(prefix))) return false;
  return TRIGGER_EXTS.some((ext) => rel.endsWith(ext));
}

function isCoverageChange(rel) {
  return COVERAGE_FILES.includes(rel) || rel.startsWith("docs/ceo-ai/");
}

// Check if coverage-matrix.md was updated and references the given surfaces.
// A valid update includes at least one row that explicitly names a surface (table:, path:, func:, etc.)
function verifyMatrixCoverage(matrixContent, newSurfaces) {
  if (newSurfaces.length === 0) return true; // No new surfaces to cover

  // Extract all surface references from coverage-matrix rows
  // Rows are like: | table_name | ... | ... | or | /path | ... | ... | or | FunctionName | ... | ...
  const coveredSurfaces = new Set();

  // Simple pattern: look for any surface identifier in the table
  for (const surface of newSurfaces) {
    const [type, name] = surface.split(":");

    // Check if the matrix mentions this surface in any table row
    if (type === "table") {
      // Table rows usually have the table name in the first column, allow for underscores/word boundaries
      if (new RegExp(`\\|\\s*${name.replace(/_/g, "\\w*")}\\s*\\|`).test(matrixContent)) {
        coveredSurfaces.add(surface);
      }
    } else if (type === "path") {
      // Path rows have the path in a column
      if (matrixContent.includes(`${name}`)) {
        coveredSurfaces.add(surface);
      }
    } else if (type === "func") {
      // Function names in coverage rows
      if (matrixContent.includes(name)) {
        coveredSurfaces.add(surface);
      }
    }
  }

  return coveredSurfaces.size === newSurfaces.length;
}

export function evaluateChangedFiles(files, readFile = () => "", readDiff = () => "") {
  const triggers = files.filter(hasTriggeringChange);
  if (triggers.length === 0) return [];

  // Detect new leadership-relevant surfaces from the diff FIRST
  const { surfaces: newSurfaces, isClean } = detectNewLeadershipSurfaces(triggers, readDiff);

  const coverage = files.filter(isCoverageChange);

  // If no coverage files found
  if (coverage.length === 0) {
    if (newSurfaces.length > 0) {
      // Found new surfaces, require coverage
      return [
        `leadership assistant coverage missing: new surface(s) detected [${newSurfaces.join(", ")}] but no coverage update found. Add a real coverage artifact (ceo_ai view, MCP tool, Cube metric, wired reader) or update coverage-matrix.md to name and cover the surface`,
      ];
    }
    if (!isClean) {
      // Diff was unclear, default to requiring coverage (conservative)
      return [
        `leadership assistant coverage missing: ${triggers.length} implementation/contract/data change(s) need a matching docs/ceo-ai, context/agents, or skill update`,
      ];
    }
    // Pure refactor (confirmed clean, no new surfaces, no coverage files) is OK
    return [];
  }

  // If there are new surfaces, require structured coverage
  if (newSurfaces.length > 0) {
    // Check for real coverage artifacts (new view, tool, metric, wired reader, or exclusion)
    const matrixContent = readFile(COVERAGE_MATRIX);
    const hasRealCoverage =
      // New view migration
      triggers.some(f => f.startsWith("backend/migrations/postgres/") && readFile(f).includes("ceo_ai.")) ||
      // New tool in MCP Toolbox yaml
      (coverage.includes("docs/ceo-ai/mcp-toolbox-tools.yaml") &&
       readFile("docs/ceo-ai/mcp-toolbox-tools.yaml").match(/^\s{2}\w+:/m)) ||
      // New Cube binding in wiring.go
      (readFile("backend/internal/ceoai/wiring.go").match(/cubeMetricBindings/) || false) ||
      // New wired reader in bootstrap
      (readFile("backend/internal/bootstrap/api.go").match(/Set\w+DataReader\(/) || false) ||
      // Explicit coverage-matrix update naming the surfaces
      verifyMatrixCoverage(matrixContent, newSurfaces);

    if (!hasRealCoverage) {
      return [
        `leadership assistant coverage incomplete: new surface(s) detected [${newSurfaces.join(", ")}] but no real coverage found. Add one of: a ceo_ai.* view migration, an MCP tool in mcp-toolbox-tools.yaml, a Cube metric binding, a wired reader in bootstrap, or an updated coverage-matrix.md row NAMING the surface with a coverage path or exclusion reason`,
      ];
    }
  } else if (!isClean) {
    // Diff was unclear (no specific surfaces found, but not confirmed as refactor-only), require keyword coverage
    const hasUsefulCoverage = coverage.some((rel) => COVERAGE_TOKEN_RE.test(readFile(rel)));
    if (!hasUsefulCoverage) {
      return [
        `leadership assistant coverage update is too weak: touched ${coverage.join(", ")} but did not mention assistant coverage, MCP/Toolbox, ceo_ai, read APIs, or SQL fallback`,
      ];
    }
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
      return {
        files: execSync(`git diff --name-only --diff-filter=ACMR ${range}`, { encoding: "utf8" })
          .split("\n")
          .map((line) => line.trim())
          .filter(Boolean),
        range,
      };
    } catch {
      // Try the next range.
    }
  }
  return { files: [], range: null };
}

function getDiff(range, file) {
  if (!range) return "";
  try {
    return execSync(`git diff ${range} -- ${file}`, { encoding: "utf8" });
  } catch {
    return "";
  }
}

function selfTest() {
  // TEST 1: Backend module change without coverage → FAIL
  const bad = evaluateChangedFiles(["backend/internal/newmodule/service.go"], () => "package newmodule");
  if (bad.length === 0) throw new Error("self-test: backend module change without assistant coverage was not blocked");

  // TEST 2: API contract with new path but no coverage → FAIL (detects new path)
  const apiBad = evaluateChangedFiles(
    ["contracts/openapi/app-api.yaml", "docs/ceo-ai/mcp-toolbox-plan.md"],
    (rel) => {
      // readFile: read actual file content
      if (rel === "docs/ceo-ai/mcp-toolbox-plan.md") return "leadership assistant MCP coverage";
      if (rel === "docs/ceo-ai/coverage-matrix.md") return ""; // matrix doesn't mention new path
      return ""; // all other files return empty
    },
    (rel) => {
      // readDiff: read diff content
      if (rel === "contracts/openapi/app-api.yaml") return "+ /new-endpoint:";
      return "";
    }
  );
  if (apiBad.length === 0) throw new Error("self-test: new API path without structured coverage was not blocked");

  // TEST 3: NEW MIGRATION creating table WITHOUT coverage artifact → FAIL (closed loophole!)
  const migBad = evaluateChangedFiles(
    ["backend/migrations/postgres/000999_new_table.sql", "docs/ceo-ai/mcp-toolbox-plan.md"],
    (rel) => {
      if (rel === "docs/ceo-ai/mcp-toolbox-plan.md") return "leadership assistant MCP Toolbox";
      if (rel === "docs/ceo-ai/coverage-matrix.md") return ""; // matrix doesn't mention new table
      return "";
    },
    (rel) => {
      if (rel === "backend/migrations/postgres/000999_new_table.sql") return "CREATE TABLE new_leadership_table (id UUID);";
      return "";
    }
  );
  if (migBad.length === 0) throw new Error("self-test: NEW migration creating table without coverage artifact (the loophole!) was not blocked");

  // TEST 4: Keyword doc touch with NO surface reference (loophole case) → FAIL
  const keywordLoophole = evaluateChangedFiles(
    ["backend/internal/newfeature/service.go", "docs/ceo-ai/mcp-toolbox-plan.md"],
    (rel) => {
      if (rel === "docs/ceo-ai/mcp-toolbox-plan.md") return "Updated MCP Toolbox leadership assistant coverage"; // keyword present
      if (rel === "docs/ceo-ai/coverage-matrix.md") return ""; // matrix doesn't reference the feature
      return "";
    },
    (rel) => {
      if (rel === "backend/internal/newfeature/service.go") return "+ func NewFeature() {}"; // note the + prefix for diff
      return "";
    }
  );
  if (keywordLoophole.length === 0) throw new Error("self-test: keyword-only doc touch without surface reference (loophole) was not blocked");

  // TEST 5: Pure refactor migration without coverage files → PASS (no new surfaces)
  const migrationOnly = evaluateChangedFiles(
    ["backend/migrations/postgres/000999_refactor.sql"],
    (rel) => "", // readFile (not used for this test)
    (rel) => "ALTER TABLE existing_table ADD COLUMN new_col;" // readDiff
  );
  if (migrationOnly.length !== 0) throw new Error(`self-test: pure refactor migration was incorrectly blocked: ${migrationOnly.join("; ")}`);

  // TEST 6: Vaccination capacity UI exclusion → PASS
  const vaccinationCapacityUI = evaluateChangedFiles(
    [
      "apps/admin-web/app/api/vaccination/capacity-config/route.ts",
      "apps/admin-web/features/people/positions-panel.tsx",
      "tools/dev/build-vaccination-hrms-fixture.mjs",
    ],
    () => "vaccination capacity UI/proxy only",
  );
  if (vaccinationCapacityUI.length !== 0) {
    throw new Error(`self-test: vaccination capacity UI/proxy exclusion was incorrectly blocked: ${vaccinationCapacityUI.join("; ")}`);
  }

  // TEST 7: New migration WITH matching coverage-matrix row → PASS
  const migWithCoverage = evaluateChangedFiles(
    ["backend/migrations/postgres/000888_new_leadership_table.sql", "docs/ceo-ai/coverage-matrix.md"],
    (rel) => {
      if (rel === "docs/ceo-ai/coverage-matrix.md") return "| new_leadership_table | ceo_ai.new_table_status | draft coverage |";
      return "";
    },
    (rel) => {
      if (rel === "backend/migrations/postgres/000888_new_leadership_table.sql") return "CREATE TABLE new_leadership_table (id UUID);";
      return "";
    }
  );
  if (migWithCoverage.length !== 0) throw new Error(`self-test: new migration with coverage-matrix update was incorrectly blocked: ${migWithCoverage.join("; ")}`);

  // TEST 8: New migration WITH view artifact + coverage-matrix entry → PASS
  const migWithView = evaluateChangedFiles(
    ["backend/migrations/postgres/000777_add_view.sql", "docs/ceo-ai/coverage-matrix.md"],
    (rel) => {
      // readFile: actual file content
      if (rel === "backend/migrations/postgres/000777_add_view.sql") return "CREATE TABLE new_table (id UUID);\nCREATE VIEW ceo_ai.new_table_view AS SELECT * FROM new_table;";
      if (rel === "docs/ceo-ai/coverage-matrix.md") return "| new_table | ceo_ai.new_table_view | draft |";
      return "";
    },
    (rel) => {
      // readDiff: diff content
      if (rel === "backend/migrations/postgres/000777_add_view.sql") return "CREATE TABLE new_table (id UUID);\nCREATE VIEW ceo_ai.new_table_view";
      return "";
    }
  );
  if (migWithView.length !== 0) throw new Error(`self-test: new migration with view artifact was incorrectly blocked: ${migWithView.join("; ")}`);

  // TEST 9: New OpenAPI path WITH coverage-matrix entry → PASS
  const apiWithCoverage = evaluateChangedFiles(
    ["contracts/openapi/app-api.yaml", "docs/ceo-ai/coverage-matrix.md"],
    (rel) => {
      if (rel === "docs/ceo-ai/coverage-matrix.md") return "| GET /new-endpoint | api + view:new_view | draft |";
      return "";
    },
    (rel) => {
      if (rel === "contracts/openapi/app-api.yaml") return "+ /new-endpoint:";
      return "";
    }
  );
  if (apiWithCoverage.length !== 0) throw new Error(`self-test: new API path with coverage-matrix entry was incorrectly blocked: ${apiWithCoverage.join("; ")}`);

  // TEST 10: Real unrelated change (pure refactor) → PASS
  const unrelated = evaluateChangedFiles(
    ["backend/internal/existing/refactor.go"],
    (rel) => "", // readFile
    (rel) => "// refactoring: extract helper function\nfunc helperFunction() { }" // readDiff - clearly new function, but refactoring only (no new surfaces because it's not exported)
  );
  if (unrelated.length !== 0) throw new Error(`self-test: unrelated refactor was incorrectly blocked: ${unrelated.join("; ")}`);

  // TEST 11: Added non-function lines followed by context exported constructor → PASS.
  // This guards against /^\+\s*func/ accidentally consuming the newline after a
  // plus line and treating the following context line as a newly added function.
  const contextConstructor = evaluateChangedFiles(
    ["backend/internal/processintegrity/app/service.go"],
    (rel) => "",
    (rel) => "+const maxProtocolAdherencePages = 100\n+\n func NewService(repo ports.Repository) *Service {\n"
  );
  if (contextConstructor.length !== 0) {
    throw new Error(`self-test: context exported constructor was incorrectly detected as a new surface: ${contextConstructor.join("; ")}`);
  }

  console.log("leadership-assistant-coverage guard self-test passed (all 11 adversarial cases verified)");
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }

  const { files, range } = changedFiles();
  const errors = evaluateChangedFiles(
    files,
    (rel) => {
      try {
        return readFileSync(rel, "utf8");
      } catch {
        return "";
      }
    },
    (rel) => getDiff(range, rel),
  );

  if (errors.length > 0) {
    console.error("leadership-assistant-coverage guard failed:");
    for (const error of errors) console.error(`- ${error}`);
    console.error("");
    console.error("Fix: add a real coverage artifact (ceo_ai view, MCP tool, Cube metric, wired reader) or update coverage-matrix.md to name the new surface and mark it covered or excluded.");
    process.exit(1);
  }

  console.log("leadership-assistant-coverage guard passed");
}

main();
