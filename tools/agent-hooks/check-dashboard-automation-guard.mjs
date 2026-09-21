import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
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
  "tools/dashboard-automation/run-dashboard-automation.mjs",
  "tools/dashboard-automation/preflight-oci-free.mjs",
  "docs/runbooks/dashboard-automation-oci.md",
]) {
  if (!existsSync(file)) failures.push(`required dashboard automation file missing: ${file}`);
}

if (failures.length > 0) {
  console.error(failures.join("\n"));
  process.exit(1);
}

console.log(`dashboard automation guard: PASS (${comparison.filesystemRoutes.length} filesystem routes, ${smokeRoutes.length} smoke entries)`);

function joinWithDirs(root, ...parts) {
  const full = join(root, ...parts);
  const dir = full.split("/").slice(0, -1).join("/");
  mkdirSync(dir, { recursive: true });
  return full;
}
