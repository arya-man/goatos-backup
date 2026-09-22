#!/usr/bin/env node
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const configPath = path.join(repo, "tools/dashboard-automation/config.json");
const journeyPath = path.join(repo, "tools/dashboard-automation/module-journeys.json");
const androidJourneysPath = path.join(repo, "tools/dashboard-automation/android-journeys.json");
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
  packageScripts: adminPackage.scripts ?? {},
  androidJourneys: existsSync(androidJourneysPath) ? JSON.parse(readFileSync(androidJourneysPath, "utf8")) : null
});

if (problems.length) {
  console.error("dashboard automation static inventory guard failed:");
  for (const problem of problems) console.error(`- ${problem}`);
  process.exit(1);
}

console.log("dashboard automation static inventory guard: routes, journeys, phone journeys, scripts, and required failure strings are covered");

export function validateInventory({ adminAppRoot, smokeScriptText, failureStrings, exclusions = [], journeyManifest = null, packageScripts = {}, androidJourneys = null }) {
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
  problems.push(...validateAndroidJourneys(androidJourneys));

  return problems;
}

// Lane 5's phone-journey catalogue. The invariants here are the ones that, if they
// ever quietly stopped holding, would let a Test Lab run on a VIRTUAL device claim a
// check that only a real farm phone can prove.
export function validateAndroidJourneys(catalog) {
  if (!catalog) return [];
  const problems = [];
  if (!Array.isArray(catalog.journeys) || catalog.journeys.length === 0) {
    return ["the Android journey catalogue must define at least one journey"];
  }
  const seen = new Set();
  for (const journey of catalog.journeys) {
    const name = journey.name;
    if (!name || seen.has(name)) problems.push(`Android journey has a missing or duplicate name: ${JSON.stringify(name)}`);
    seen.add(name);
    if (!journey.humanFailure) problems.push(`Android journey ${name} has no sentence for a farm manager to read`);
    if (!journey.story) problems.push(`Android journey ${name} has no plain-English story`);
    if (!Array.isArray(journey.sourceCommits)) problems.push(`Android journey ${name} does not name the commits it covers`);
    if (!["virtual", "physical"].includes(journey.device)) {
      problems.push(`Android journey ${name} must say whether it needs a virtual or a physical device`);
    }
    if (journey.device === "physical") {
      if (!journey.physicalReason) {
        problems.push(`Android journey ${name} needs a physical device but does not say why a virtual one would be a false green`);
      }
      if (journey.automation?.testClass) {
        problems.push(`Android journey ${name} is physical-device-only, so it must carry no test class that a virtual run could execute`);
      }
      if (journey.automation?.tier !== "physical-only") {
        problems.push(`Android journey ${name} needs a physical device, so its automation tier must stay physical-only`);
      }
    }
    // Anything claimed as covered has to say which half of its check it really proves.
    if (journey.automation?.tier === "runs-on-virtual" && !journey.automation?.coversPartially) {
      problems.push(`Android journey ${name} is claimed as covered, so it must say which half of its check it actually proves`);
    }
    // Nothing is parked without a reason.
    if (["parked", "needs-seeded-session"].includes(journey.automation?.tier) && !String(journey.automation?.note ?? "").trim()) {
      problems.push(`Android journey ${name} is parked with no reason`);
    }
  }
  // A physical allowance inside anything labelled free tier reads as permission to spend.
  const freeTier = JSON.stringify(catalog.budget?.freeTier ?? {});
  if (/physical\w*"\s*:\s*\d/i.test(freeTier)) {
    problems.push("the Android free-tier budget must carry no physical-device allowance; the free tier grants none");
  }
  const claimed = catalog.journeys.filter((j) => j.automation?.tier === "runs-on-virtual").length;
  if (catalog.coverageToday && catalog.coverageToday.journeysAVirtualRunCanProveToday !== claimed) {
    problems.push(`the Android catalogue says ${catalog.coverageToday.journeysAVirtualRunCanProveToday} journeys are covered but ${claimed} rows claim it`);
  }
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
  // The guard must actually catch a physical check being dressed up as a virtual one.
  {
    const physicalAsVirtual = {
      budget: { freeTier: { virtualTestsPerDay: 10 } },
      journeys: [{
        name: "proof-capture", story: "A stockman photographs the work.", humanFailure: "Photos do not attach to the job.",
        sourceCommits: [], device: "physical", physicalReason: "needs a real camera",
        automation: { tier: "runs-on-virtual", testClass: "GoatOsColdBootJourneys" }
      }]
    };
    const found = validateAndroidJourneys(physicalAsVirtual);
    assert.ok(found.some((p) => /must carry no test class/.test(p)), "a physical journey with a test class must be caught");
    assert.ok(found.some((p) => /physical-only/.test(p)), "a physical journey claimed as virtual must be caught");
  }
  {
    const parkedWithNoReason = {
      journeys: [{
        name: "x", story: "A story about the farm.", humanFailure: "It did not work.", sourceCommits: [],
        device: "virtual", automation: { tier: "parked", note: "  " }
      }]
    };
    assert.ok(validateAndroidJourneys(parkedWithNoReason).some((p) => /parked with no reason/.test(p)),
      "parked work with no reason must be caught");
  }
  {
    const spendAllowance = { journeys: [{ name: "x", story: "A story about the farm.", humanFailure: "It did not work.", sourceCommits: [], device: "virtual", automation: { tier: "parked", note: "n" } }], budget: { freeTier: { physicalTestsPerDay: 5 } } };
    assert.ok(validateAndroidJourneys(spendAllowance).some((p) => /no physical-device allowance/.test(p)),
      "a physical allowance inside the free-tier block must be caught");
  }
  assert.deepEqual(validateAndroidJourneys(null), [], "a repo without the Android catalogue must still pass");
  console.log("dashboard automation static inventory guard: self-test passed");
}

function requireDir(dir) {
  mkdirSync(dir, { recursive: true });
}
