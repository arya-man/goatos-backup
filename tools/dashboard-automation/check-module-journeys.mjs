#!/usr/bin/env node
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { compareRoutes, discoverFilesystemRoutes, discoverSmokeRoutes } from "./discover-admin-routes.mjs";

const selfTest = process.argv.includes("--self-test");
const manifestPath = "tools/dashboard-automation/module-journeys.json";

if (selfTest) {
  const dir = mkdtempSync(join(tmpdir(), "goatos-module-journeys-"));
  try {
    const appRoot = join(dir, "app", "(admin)");
    writeFileSync(joinWithDirs(appRoot, "feed", "analytics", "page.tsx"), "export default function Page() { return null; }\n");
    writeFileSync(joinWithDirs(appRoot, "new-module", "page.tsx"), "export default function Page() { return null; }\n");
    const fsRoutes = discoverFilesystemRoutes(appRoot);
    const smokeRoutes = [{ name: "feed-analytics", path: "/feed/analytics?scope_mode=company" }];
    const fakeManifest = {
      requiredModules: ["feed"],
      journeys: [{ id: "feed", routes: ["feed-analytics"], coverage: ["tabs"], assertText: ["Feed"], safeClicks: [{ text: "Filters", optional: true }] }],
    };
    const result = checkJourneys({ manifest: fakeManifest, filesystemRoutes: fsRoutes, smokeRoutes });
    assert.ok(result.findings.some((finding) => /new-module/.test(finding)));
    const missingClickResult = checkJourneys({
      manifest: { requiredModules: ["feed"], journeys: [{ id: "feed", routes: ["feed-analytics"], coverage: ["tabs"], assertText: ["Feed"] }] },
      filesystemRoutes: fsRoutes.filter((route) => route.path !== "/new-module"),
      smokeRoutes,
    });
    assert.ok(missingClickResult.findings.some((finding) => /safe click/.test(finding)));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
  console.log("module journey guard self-test: PASS");
  process.exit(0);
}

if (!existsSync(manifestPath)) {
  console.error(`missing module journey manifest: ${manifestPath}`);
  process.exit(1);
}

const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
const result = checkJourneys({ manifest });
if (result.findings.length) {
  console.error(result.findings.join("\n"));
  process.exit(1);
}

console.log(`module journey guard: PASS (${manifest.journeys.length} module journeys, ${result.coveredRoutes.size} covered smoke routes)`);

export function checkJourneys({ manifest, filesystemRoutes = discoverFilesystemRoutes(), smokeRoutes = discoverSmokeRoutes() }) {
  const findings = [];
  const smokeByName = new Map(smokeRoutes.map((route) => [route.name, route]));
  const smokeByPath = new Map(smokeRoutes.map((route) => [stripQuery(route.path), route.name]));
  const journeys = Array.isArray(manifest.journeys) ? manifest.journeys : [];
  const requiredModules = new Set(manifest.requiredModules ?? []);
  const journeyIds = new Set(journeys.map((journey) => journey.id));
  const coveredRoutes = new Set();

  for (const required of requiredModules) {
    if (!journeyIds.has(required)) findings.push(`${manifestPath}: missing required module journey ${required}`);
  }

  for (const journey of journeys) {
    if (!journey.id) findings.push(`${manifestPath}: journey missing id`);
    if (!Array.isArray(journey.routes) || journey.routes.length === 0) findings.push(`${manifestPath}: ${journey.id} must list smoke routes`);
    if (!Array.isArray(journey.coverage) || journey.coverage.length < 2) findings.push(`${manifestPath}: ${journey.id} must list real coverage dimensions`);
    if (!Array.isArray(journey.assertText) || journey.assertText.length === 0) findings.push(`${manifestPath}: ${journey.id} must include visible text assertions`);
    if (!Array.isArray(journey.safeClicks) || journey.safeClicks.length === 0) findings.push(`${manifestPath}: ${journey.id} must include at least one read-only safe click target`);
    for (const routeName of journey.routes ?? []) {
      coveredRoutes.add(routeName);
      if (!smokeByName.has(routeName)) findings.push(`${manifestPath}: ${journey.id} references unknown smoke route ${routeName}`);
    }
  }

  const routeComparison = compareRoutes(filesystemRoutes, smokeRoutes);
  for (const missing of routeComparison.missing) {
    findings.push(`admin-web filesystem route ${missing.path} still lacks visual smoke coverage before module journey coverage can be trusted`);
  }
  for (const route of filesystemRoutes) {
    if (isRouteExempt(route.path)) continue;
    const smokeName = smokeByPath.get(route.path);
    if (smokeName && !coveredRoutes.has(smokeName)) {
      findings.push(`admin-web route ${route.path} is in visual smoke as ${smokeName} but missing module journey ownership`);
    }
  }
  for (const route of smokeRoutes) {
    if (!coveredRoutes.has(route.name)) {
      findings.push(`smoke route state ${route.name} is missing module journey ownership`);
    }
  }

  for (const requiredRoute of [
    "weighing-weights",
    "vaccination",
    "feed-analytics",
    "sales-sold",
    "procurement-source-entry",
    "counts-herd",
    "health-analytics",
    "work-board",
    "action-center",
    "calendar",
    "people",
    "operations-audit",
  ]) {
    if (!coveredRoutes.has(requiredRoute)) findings.push(`${manifestPath}: missing critical journey route ${requiredRoute}`);
  }

  return { findings, coveredRoutes };
}

function stripQuery(path) {
  const clean = path.split("?")[0].split("#")[0];
  return clean.replaceAll(/\/placeholder\b/g, "/[dynamic]");
}

function isRouteExempt(path) {
  return path.includes("[");
}

function joinWithDirs(root, ...parts) {
  const full = join(root, ...parts);
  const dir = full.split("/").slice(0, -1).join("/");
  mkdirSync(dir, { recursive: true });
  return full;
}
