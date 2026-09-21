import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { compareRoutes, discoverFilesystemRoutes, discoverSmokeRoutes } from "../dashboard-automation/discover-admin-routes.mjs";

const selfTest = process.argv.includes("--self-test");

if (selfTest) {
  const dir = mkdtempSync(join(tmpdir(), "dashboard-automation-guard-"));
  try {
    const appRoot = join(dir, "app", "(admin)");
    writeFileSync(joinWithDirs(appRoot, "new-surface", "page.tsx"), "export default function Page() { return null; }\n");
    const fsRoutes = discoverFilesystemRoutes(appRoot);
    const smokeRoutes = [{ name: "control-tower", path: "/?scope_mode=company" }];
    const result = compareRoutes(fsRoutes, smokeRoutes);
    assert.equal(result.missing.length, 1);
    assert.equal(result.missing[0].path, "/new-surface");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
  console.log("dashboard automation guard self-test: PASS");
  process.exit(0);
}

const comparison = compareRoutes();
const failures = [];
if (comparison.missing.length > 0) {
  failures.push(`admin-web route(s) missing deterministic smoke coverage: ${comparison.missing.map((route) => `${route.path} (${route.source})`).join(", ")}`);
}

const smokeRoutes = discoverSmokeRoutes();
const requiredNames = new Set(smokeRoutes.map((route) => route.name));
for (const required of ["work-board", "weighing-weights", "herd-signals", "action-center", "calendar"]) {
  if (!requiredNames.has(required)) failures.push(`smoke route inventory lost historically risky route: ${required}`);
}

for (const file of [
  "tools/dashboard-automation/config.json",
  "tools/dashboard-automation/run.mjs",
  "tools/dashboard-automation/run-oci.sh",
  "tools/dashboard-automation/install-oci-user-timer.sh",
  "tools/dashboard-automation/check-business-data-parity.mjs",
  "tools/dashboard-automation/self-heal-pr.mjs",
  "docs/runbooks/dashboard-automation-oci.md",
]) {
  if (!existsSync(file)) failures.push(`required dashboard automation file missing: ${file}`);
}
failures.push(...dashboardStateContractFindings());

if (failures.length > 0) {
  console.error(failures.join("\n"));
  process.exit(1);
}

console.log(`dashboard automation guard: PASS (${comparison.filesystemRoutes.length} filesystem routes, ${smokeRoutes.length} smoke entries)`);

function dashboardStateContractFindings() {
  const findings = [];
  for (const rel of adminWebSourceFiles()) {
    const source = readFileSync(rel, "utf8");
    if (/defaultFrom[\s\S]{0,160}defaultTo[\s\S]{0,160}delete\([^)]*(?:param|toParam)/.test(source)) {
      findings.push(
        `${rel}: date/range URL state must not delete explicit picked params just because the pick equals a default. Absence can mean a derived server window; explicit choices must survive tabs, park, scope, and sibling filter changes.`,
      );
    }
    if (/defaultTo[\s\S]{0,160}defaultFrom[\s\S]{0,160}delete\([^)]*(?:param|toParam)/.test(source)) {
      findings.push(
        `${rel}: date/range URL state must not delete explicit picked params just because the pick equals a default. Absence can mean a derived server window; explicit choices must survive tabs, park, scope, and sibling filter changes.`,
      );
    }
  }
  const worklistFilters = "apps/admin-web/components/worklist-filters.tsx";
  if (existsSync(worklistFilters)) {
    const source = readFileSync(worklistFilters, "utf8");
    const applyRange = source.match(/function applyRange\([\s\S]*?\n  \}/)?.[0] ?? "";
    if (/from\s*===\s*field\.defaultFrom[\s\S]*to\s*===\s*field\.defaultTo/.test(applyRange)) {
      findings.push(
        `${worklistFilters}: daterange applyRange must not delete wt_from/wt_to just because a picked span equals defaultFrom/defaultTo. Absence can mean a derived server window, so picked URL state must stay pinned across park/scope/filter changes.`,
      );
    }
    if (!/next\.set\(field\.param,\s*from\);[\s\S]*next\.set\(field\.toParam,\s*to\);/.test(applyRange)) {
      findings.push(`${worklistFilters}: daterange applyRange must write both from/to params in one transition so half windows never hit the backend.`);
    }
  }
  return findings;
}

function adminWebSourceFiles() {
  const roots = ["apps/admin-web/app", "apps/admin-web/components", "apps/admin-web/features"];
  const files = [];
  for (const root of roots) walkFiles(root, files);
  return files.filter((file) => /\.(?:tsx|ts|jsx|js|mjs)$/.test(file));
}

function walkFiles(path, files) {
  if (!existsSync(path)) return;
  const stat = statSync(path);
  if (stat.isFile()) {
    files.push(path);
    return;
  }
  for (const entry of readdirSync(path)) {
    if (entry === "node_modules" || entry.startsWith(".")) continue;
    walkFiles(`${path}/${entry}`, files);
  }
}

function joinWithDirs(root, ...parts) {
  const full = join(root, ...parts);
  const dir = full.split("/").slice(0, -1).join("/");
  mkdirSync(dir, { recursive: true });
  return full;
}
