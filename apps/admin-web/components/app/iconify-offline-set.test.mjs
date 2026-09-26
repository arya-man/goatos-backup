// Guard: iconify-offline-set. Every literal Iconify name the app renders must be registered in the
// offline icon set (layouts/template/iconify/icon-sets.ts). An unregistered name is fetched from the
// Iconify API at runtime: the icon flickers in, is missing offline / in the Android webview, and the
// console warns "is currently loaded online". Add the icon's JSON to icon-sets.ts or pick a
// registered name.
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const appRoot = fileURLToPath(new URL("../..", import.meta.url));
const registered = new Set(
  [...readFileSync(join(appRoot, "layouts/template/iconify/icon-sets.ts"), "utf8").matchAll(/^\s*'([a-z0-9-]+:[a-z0-9-]+)'\s*:/gm)].map((m) => m[1]),
);
const ICON_LITERAL = /\bicon(?:=|:\s*)(?:\{\s*)?["']([a-z0-9-]+:[a-z0-9-]+)["']/g;

function walk(dir, out) {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name.startsWith(".")) continue;
    const full = join(dir, name);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (/\.(tsx|ts)$/.test(name) && !name.endsWith(".d.ts") && !full.endsWith("icon-sets.ts")) out.push(full);
  }
  return out;
}

export function unregisteredIcons(source, known = registered) {
  return [...source.matchAll(ICON_LITERAL)].map((m) => m[1]).filter((name) => !known.has(name));
}

test("iconify-offline-set: self-test", () => {
  const known = new Set(["solar:check-circle-bold"]);
  assert.deepEqual(unregisteredIcons(`<Iconify icon="solar:check-circle-bold" />`, known), []);
  assert.deepEqual(unregisteredIcons(`<Iconify icon="solar:check-circle-bold-duotone" />`, known), ["solar:check-circle-bold-duotone"]);
  assert.deepEqual(unregisteredIcons(`{ icon: "solar:nope-bold" }`, known), ["solar:nope-bold"]);
});

test("iconify-offline-set: every literal icon name is in the offline set", () => {
  assert.ok(registered.size > 50, "icon-sets.ts parsed");
  const misses = [];
  for (const dir of ["app", "components", "features", "layouts", "lib"]) {
    for (const file of walk(join(appRoot, dir), [])) {
      for (const name of unregisteredIcons(readFileSync(file, "utf8"))) misses.push(`${relative(appRoot, file)}: ${name}`);
    }
  }
  assert.deepEqual(misses, [], `unregistered Iconify names (add to layouts/template/iconify/icon-sets.ts or use a registered one):\n${misses.join("\n")}`);
});
