import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = dirname(dirname(dirname(fileURLToPath(import.meta.url))));
const adminAppDir = join(repoRoot, "apps", "admin-web", "app", "(admin)");
const smokePath = join(repoRoot, "apps", "admin-web", "scripts", "smoke-visual-live.mjs");

export function discoverFilesystemRoutes(root = adminAppDir) {
  const routes = [];
  walk(root, (path) => {
    if (!path.endsWith(`${sep}page.tsx`)) return;
    const rel = relative(root, dirname(path)).split(sep).filter(Boolean);
    const routePath = `/${rel.filter((part) => !part.startsWith("(")).join("/")}`.replace(/\/$/, "") || "/";
    routes.push({
      path: routePath,
      dynamic: routePath.includes("["),
      source: relative(repoRoot, path),
    });
  });
  return routes.sort((a, b) => a.path.localeCompare(b.path));
}

export function discoverSmokeRoutes(source = readFileSync(smokePath, "utf8")) {
  const block = source.match(/function buildRoutes\([\s\S]*?const pagerMinimums = new Map/)?.[0] ?? "";
  return Array.from(block.matchAll(/\{\s*name:\s*"([^"]+)"[\s\S]{0,600}?path:\s*([`"])([^`"]+)/g), ([, name, quote, rawPath]) => ({
    name,
    path: quote === "`" ? normalizeTemplatePath(rawPath) : rawPath,
  })).sort((a, b) => a.name.localeCompare(b.name));
}

export function compareRoutes(filesystemRoutes = discoverFilesystemRoutes(), smokeRoutes = discoverSmokeRoutes()) {
  const smokePaths = new Set(smokeRoutes.map((route) => stripQuery(route.path)));
  const missing = [];
  for (const route of filesystemRoutes) {
    if (isRouteExempt(route.path)) continue;
    if (!smokePaths.has(route.path)) missing.push(route);
  }
  return { filesystemRoutes, smokeRoutes, missing };
}

function walk(root, visit) {
  if (!existsSync(root)) return;
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) walk(path, visit);
    else visit(path);
  }
}

function normalizeTemplatePath(path) {
  return path
    .replaceAll(/\$\{encodeURIComponent\([^)]*\)\}/g, "[dynamic]")
    .replaceAll(/\$\{[^}]+\}/g, "[dynamic]");
}

function stripQuery(path) {
  const clean = path.split("?")[0].split("#")[0];
  return clean
    .replaceAll("/[dynamic]", "/[dynamic]")
    .replaceAll(/\/placeholder\b/g, "/[dynamic]");
}

function isRouteExempt(path) {
  return path === "/goats/[goat_id]" || path.includes("[");
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const result = compareRoutes();
  console.log(JSON.stringify(result, null, 2));
  if (process.argv.includes("--fail-on-missing") && result.missing.length > 0) {
    console.error(`Missing admin-web smoke coverage for ${result.missing.length} route(s)`);
    process.exit(1);
  }
}
