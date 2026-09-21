#!/usr/bin/env node
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const configPath = path.join(repo, "tools/dashboard-automation/config.json");
const journeyPath = path.join(repo, "tools/dashboard-automation/module-journeys.json");
const smokePath = path.join(repo, "apps/admin-web/scripts/smoke-visual-live.mjs");
const packagePath = path.join(repo, "apps/admin-web/package.json");
const adminAppRoot = path.join(repo, "apps/admin-web/app/(admin)");

const args = new Set(process.argv.slice(2));
if (args.has("--self-test")) {
  selfTest();
  process.exit(0);
}

const config = JSON.parse(readFileSync(configPath, "utf8"));
const journeyManifest = JSON.parse(readFileSync(journeyPath, "utf8"));
const adminPackage = JSON.parse(readFileSync(packagePath, "utf8"));
const problems = validateInventory({
  adminAppRoot,
  smokeScriptText: readFileSync(smokePath, "utf8"),
  failureStrings: config.requiredFailureStrings,
  exclusions: config.explicitRouteExclusions,
  journeyManifest,
  packageScripts: adminPackage.scripts ?? {}
});

if (problems.length) {
  console.error("dashboard automation static inventory guard failed:");
  for (const problem of problems) console.error(`- ${problem}`);
  process.exit(1);
}

console.log("dashboard automation static inventory guard: routes, journeys, scripts, and required failure strings are covered");

export function validateInventory({ adminAppRoot, smokeScriptText, failureStrings, exclusions = [], journeyManifest = null, packageScripts = {} }) {
  const problems = [];
  const fsRoutes = discoverPageRoutes(adminAppRoot);
  const smokePaths = discoverSmokePaths(smokeScriptText);
  const smokeRouteNames = discoverSmokeRouteNames(smokeScriptText);
  const exclusionMatchers = exclusions.map((item) => wildcardToRegExp(item.pattern));

  for (const route of fsRoutes) {
    if (exclusionMatchers.some((matcher) => matcher.test(route))) continue;
    if (!smokePaths.some((smokeRoute) => routeMatchesSmokePath(route, smokeRoute))) {
      problems.push(`admin route ${route} is not covered by smoke-visual-live.mjs and has no explicit exclusion`);
    }
  }

  for (const text of failureStrings) {
    if (!smokeScriptText.includes(text)) {
      problems.push(`required production failure string is not asserted by smoke-visual-live.mjs: ${JSON.stringify(text)}`);
    }
  }

  problems.push(...validateJourneyManifest({ journeyManifest, smokeRouteNames, packageScripts }));

  return problems;
}

function validateJourneyManifest({ journeyManifest, smokeRouteNames, packageScripts }) {
  if (!journeyManifest) return [];
  const problems = [];
  if (!Array.isArray(journeyManifest.journeys) || journeyManifest.journeys.length === 0) {
    return ["read-only journey manifest must define at least one journey"];
  }
  const seenModules = new Set();
  for (const journey of journeyManifest.journeys) {
    const moduleId = journey.module ?? journey.id;
    if (!moduleId || seenModules.has(moduleId)) {
      problems.push(`read-only journey has missing or duplicate module: ${JSON.stringify(moduleId)}`);
    }
    seenModules.add(moduleId);
    const journeyRoutes = journey.routeNames ?? journey.routes;
    if (!Array.isArray(journeyRoutes) || journeyRoutes.length === 0) {
      problems.push(`read-only journey ${moduleId} must name at least one smoke route`);
      continue;
    }
    for (const routeName of journeyRoutes) {
      if (!smokeRouteNames.has(routeName)) {
        problems.push(`read-only journey ${moduleId} references route not covered by smoke-visual-live.mjs: ${routeName}`);
      }
    }
    for (const scriptName of journey.companionScripts ?? []) {
      if (!packageScripts[scriptName]) {
        problems.push(`read-only journey ${moduleId} references missing admin-web package script: ${scriptName}`);
      }
    }
    const assertions = journey.assertions ?? journey.assertText ?? journey.coverage;
    if (!Array.isArray(assertions) || assertions.length === 0) {
      problems.push(`read-only journey ${moduleId} must describe the user-visible assertions it protects`);
    }
  }
  for (const routeName of smokeRouteNames) {
    if (![...seenJourneyRoutes(journeyManifest)].includes(routeName)) {
      problems.push(`smoke route ${routeName} is not assigned to any read-only journey module`);
    }
  }
  return problems;
}

function seenJourneyRoutes(journeyManifest) {
  const routes = new Set();
  for (const journey of journeyManifest.journeys ?? []) {
    for (const routeName of journey.routeNames ?? journey.routes ?? []) routes.add(routeName);
  }
  return routes;
}

function discoverPageRoutes(root) {
  if (!existsSync(root)) return [];
  const routes = [];
  walk(root, (file) => {
    if (!file.endsWith("/page.tsx")) return;
    const rel = path.relative(root, file).replaceAll(path.sep, "/").replace(/(^|\/)page\.tsx$/, "");
    const route = `/${rel}`
      .replaceAll(/\([^)]*\)\//g, "")
      .replaceAll(/\[[^/]+\]/g, "placeholder")
      .replace(/\/$/, "");
    routes.push(route === "/" ? "/" : route);
  });
  return routes.sort();
}

function discoverSmokePaths(text) {
  const paths = new Set();
  for (const match of text.matchAll(/\bpath:\s*(["'`])([^"'`$]+)\1/g)) {
    paths.add(stripQueryAndHash(match[2]));
  }
  for (const match of text.matchAll(/\bpath:\s*`([^`]+)`/g)) {
    paths.add(stripQueryAndHash(match[1].replaceAll(/\$\{[^}]+\}/g, "placeholder")));
  }
  for (const match of text.matchAll(/\bpath:\s*`\$\{[^}]+\s*\?\?\s*(["'])([^"']+)\1/g)) {
    paths.add(stripQueryAndHash(match[2]));
  }
  return [...paths].sort();
}

function discoverSmokeRouteNames(text) {
  const routeBlock = text.match(/function buildRoutes\([\s\S]*?const pagerMinimums = new Map/)?.[0] ?? text;
  const names = new Set();
  for (const match of routeBlock.matchAll(/\bname:\s*"([^"]+)"/g)) {
    names.add(match[1]);
  }
  return names;
}

function stripQueryAndHash(value) {
  const route = String(value).split(/[?#]/)[0];
  return route === "" ? "/" : route.replace(/\/$/, "") || "/";
}

function routeMatchesSmokePath(route, smokeRoute) {
  if (route === smokeRoute) return true;
  const routeParts = route.split("/").filter(Boolean);
  const smokeParts = smokeRoute.split("/").filter(Boolean);
  if (routeParts.length !== smokeParts.length) return false;
  return routeParts.every((part, index) => part === smokeParts[index] || part === "placeholder" || smokeParts[index] === "placeholder");
}

function wildcardToRegExp(pattern) {
  const escaped = String(pattern).replace(/[.+^${}()|[\]\\]/g, "\\$&").replaceAll("**", ".*").replaceAll("*", "[^/]*");
  return new RegExp(`^${escaped}$`);
}

function walk(dir, visit) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, visit);
    else visit(full);
  }
}

function selfTest() {
  const dir = mkdtempSync(path.join(tmpdir(), "dashboard-inventory-"));
  try {
    const root = path.join(dir, "app");
    const alpha = path.join(root, "alpha");
    const goat = path.join(root, "goats/[goat_id]");
    requireDir(alpha);
    requireDir(goat);
    writeFileSync(path.join(alpha, "page.tsx"), "export default function Page() { return null; }\n");
    writeFileSync(path.join(goat, "page.tsx"), "export default function Page() { return null; }\n");
    const goodSmoke = `
      const routes = [
        { name: "alpha", path: "/alpha?scope_mode=company" },
        { name: "goat", path: "/goats/placeholder?scope_mode=company" }
      ];
      assertNoText("backend_down");
      assertNoText("Admin-web contract unavailable");
    `;
    assert.deepEqual(validateInventory({
      adminAppRoot: root,
      smokeScriptText: goodSmoke,
      failureStrings: ["backend_down", "Admin-web contract unavailable"],
      journeyManifest: {
        journeys: [
          { module: "alpha", routeNames: ["alpha"], assertions: ["loads"] },
          { module: "goat", routeNames: ["goat"], assertions: ["loads"] }
        ]
      }
    }), []);
    const missingRoute = validateInventory({
      adminAppRoot: root,
      smokeScriptText: goodSmoke.replace("/alpha?scope_mode=company", "/other?scope_mode=company"),
      failureStrings: ["backend_down", "Admin-web contract unavailable"],
      journeyManifest: {
        journeys: [
          { module: "alpha", routeNames: ["alpha"], assertions: ["loads"] },
          { module: "goat", routeNames: ["goat"], assertions: ["loads"] }
        ]
      }
    });
    assert.ok(missingRoute.some((problem) => problem.includes("/alpha")));
    const missingString = validateInventory({
      adminAppRoot: root,
      smokeScriptText: goodSmoke.replace("backend_down", "different_error"),
      failureStrings: ["backend_down", "Admin-web contract unavailable"],
      journeyManifest: {
        journeys: [
          { module: "alpha", routeNames: ["alpha"], assertions: ["loads"] },
          { module: "goat", routeNames: ["goat"], assertions: ["loads"] }
        ]
      }
    });
    assert.ok(missingString.some((problem) => problem.includes("backend_down")));
    const missingJourneyRoute = validateInventory({
      adminAppRoot: root,
      smokeScriptText: goodSmoke,
      failureStrings: ["backend_down", "Admin-web contract unavailable"],
      journeyManifest: {
        journeys: [
          { module: "alpha", routeNames: ["alpha", "missing"], assertions: ["loads"] },
          { module: "goat", routeNames: ["goat"], assertions: ["loads"] }
        ]
      }
    });
    assert.ok(missingJourneyRoute.some((problem) => problem.includes("missing")));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
  console.log("dashboard automation static inventory guard: self-test passed");
}

function requireDir(dir) {
  mkdirSync(dir, { recursive: true });
}
