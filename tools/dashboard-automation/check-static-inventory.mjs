#!/usr/bin/env node
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const configPath = path.join(repo, "tools/dashboard-automation/config.json");
const smokePath = path.join(repo, "apps/admin-web/scripts/smoke-visual-live.mjs");
const adminAppRoot = path.join(repo, "apps/admin-web/app/(admin)");

const args = new Set(process.argv.slice(2));
if (args.has("--self-test")) {
  selfTest();
  process.exit(0);
}

const config = JSON.parse(readFileSync(configPath, "utf8"));
const problems = validateInventory({
  adminAppRoot,
  smokeScriptText: readFileSync(smokePath, "utf8"),
  failureStrings: config.requiredFailureStrings,
  exclusions: config.explicitRouteExclusions
});

if (problems.length) {
  console.error("dashboard automation static inventory guard failed:");
  for (const problem of problems) console.error(`- ${problem}`);
  process.exit(1);
}

console.log("dashboard automation static inventory guard: routes and required failure strings are covered");

export function validateInventory({ adminAppRoot, smokeScriptText, failureStrings, exclusions = [] }) {
  const problems = [];
  const fsRoutes = discoverPageRoutes(adminAppRoot);
  const smokePaths = discoverSmokePaths(smokeScriptText);
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

  return problems;
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
      failureStrings: ["backend_down", "Admin-web contract unavailable"]
    }), []);
    const missingRoute = validateInventory({
      adminAppRoot: root,
      smokeScriptText: goodSmoke.replace("/alpha?scope_mode=company", "/other?scope_mode=company"),
      failureStrings: ["backend_down", "Admin-web contract unavailable"]
    });
    assert.ok(missingRoute.some((problem) => problem.includes("/alpha")));
    const missingString = validateInventory({
      adminAppRoot: root,
      smokeScriptText: goodSmoke.replace("backend_down", "different_error"),
      failureStrings: ["backend_down", "Admin-web contract unavailable"]
    });
    assert.ok(missingString.some((problem) => problem.includes("backend_down")));
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
  console.log("dashboard automation static inventory guard: self-test passed");
}

function requireDir(dir) {
  mkdirSync(dir, { recursive: true });
}
