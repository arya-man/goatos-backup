#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { redactText } from "./lib/redact.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const matrix = JSON.parse(readFileSync(path.join(repo, "tools/dashboard-automation/module-journeys.json"), "utf8"));
const args = parseArgs(process.argv.slice(2));

if (args.selfTest) {
  selfTest();
  process.exit(0);
}

const outDir = path.resolve(args.outDir ?? path.join(repo, ".codex-goatos-render/dashboard-automation/module-journeys"));
mkdirSync(outDir, { recursive: true });
const selected = new Set((process.env.GOATOS_DASHBOARD_MODULES ?? "").split(",").map((item) => item.trim()).filter(Boolean));
const matrixModules = Array.isArray(matrix.modules) ? matrix.modules : matrix.journeys;
const modules = selected.size ? matrixModules.filter((mod) => selected.has(mod.id)) : matrixModules;
if (modules.length === 0) throw new Error(`no module journeys selected by GOATOS_DASHBOARD_MODULES=${process.env.GOATOS_DASHBOARD_MODULES}`);

const receipt = {
  startedAt: new Date().toISOString(),
  readOnly: true,
  modules: []
};

for (const mod of modules) {
  const routeList = [...new Set(mod.routes)].join(",");
  const startedAt = new Date().toISOString();
  const result = spawnSync("npm", ["--prefix", "apps/admin-web", "run", "smoke:visual:live"], {
    cwd: repo,
    env: {
      ...process.env,
      GOATOS_SMOKE_ONLY_ROUTES: routeList,
      GOATOS_SMOKE_READ_ONLY: "1",
      GOATOS_SMOKE_MODULE_ASSERT_TEXT: JSON.stringify(mod.assertText ?? []),
      GOATOS_SMOKE_MODULE_SAFE_CLICKS: JSON.stringify(mod.safeClicks ?? [])
    },
    encoding: "utf8"
  });
  receipt.modules.push({
    id: mod.id,
    label: mod.label,
    routes: mod.routes,
    coverage: mod.coverage ?? [],
    requiredInteractions: mod.requiredInteractions ?? mod.coverage ?? [],
    forbiddenWrites: mod.forbiddenWrites ?? matrix.forbiddenActions ?? [],
    startedAt,
    finishedAt: new Date().toISOString(),
    status: result.status === 0 ? "pass" : "fail",
    assertText: mod.assertText ?? [],
    stdoutTail: redactText(result.stdout ?? "").slice(-4000),
    stderrTail: redactText(result.stderr ?? "").slice(-4000)
  });
  writeFileSync(path.join(outDir, "module-journeys-receipt.json"), `${JSON.stringify(receipt, null, 2)}\n`);
  if (result.status !== 0) {
    console.error(redactText(result.stderr || result.stdout));
    throw new Error(`module journey ${mod.id} failed`);
  }
}

receipt.finishedAt = new Date().toISOString();
receipt.status = "pass";
writeFileSync(path.join(outDir, "module-journeys-receipt.json"), `${JSON.stringify(receipt, null, 2)}\n`);
console.log(`dashboard module journeys: PASS (${modules.length} modules)`);

function parseArgs(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    const arg = raw[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--out-dir") parsed.outDir = raw[++i];
    else throw new Error(`unknown argument: ${arg}`);
  }
  return parsed;
}

function selfTest() {
  const modules = Array.isArray(matrix.modules) ? matrix.modules : matrix.journeys;
  if (!Array.isArray(modules) || modules.length < 8) throw new Error("self-test: module matrix missing real module set");
  for (const mod of modules) {
    if (!Array.isArray(mod.routes) || mod.routes.length === 0) throw new Error(`self-test: ${mod.id} has no routes`);
    if (!Array.isArray(mod.coverage) || mod.coverage.length === 0) throw new Error(`self-test: ${mod.id} has no coverage dimensions`);
    if (!Array.isArray(mod.assertText) || mod.assertText.length === 0) throw new Error(`self-test: ${mod.id} has no assertText`);
  }
  console.log("dashboard module journey runner: self-test passed");
}
