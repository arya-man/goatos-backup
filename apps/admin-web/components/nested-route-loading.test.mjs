import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

// guard: nested-route-loading (TR-2 N1). Next 16's segment-cache prefetch stops at the FIRST segment
// that has a loading.tsx, so a parent route's loading.tsx (vaccination/loading.tsx) is what a
// navigation into ANY child (/vaccination/care-coverage) paints, even though the child has its own
// twin: the parent's skeleton flashes on the production build (dev does not prefetch, so it passes
// there). A segment that has child routes keeps its index page + loading.tsx in an `(index)` route
// group, so the loading boundary wraps only the index page and each child's own twin wins.
const here = dirname(fileURLToPath(import.meta.url));
const admin = join(here, "..", "app", "(admin)");
const isGroup = (name) => /^\(.*\)$/.test(name);

function* dirs(dir) {
  yield dir;
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) yield* dirs(full);
  }
}

/** page.tsx files under `dir` that belong to a DIFFERENT route (reached through a non-group folder). */
function childRoutePages(dir) {
  const out = [];
  const walk = (d, crossed) => {
    for (const name of readdirSync(d)) {
      const full = join(d, name);
      if (statSync(full).isDirectory()) walk(full, crossed || !isGroup(name));
      else if (name === "page.tsx" && crossed) out.push(relative(admin, full));
    }
  };
  walk(dir, false);
  return out;
}

test("no loading.tsx sits above a child route (the most specific route's skeleton wins)", () => {
  const offenders = [];
  for (const dir of dirs(admin)) {
    if (dir === admin) continue; // the (admin) group root is the shared layout, always mounted
    let has = false;
    try {
      has = statSync(join(dir, "loading.tsx")).isFile();
    } catch {}
    if (!has) continue;
    const pages = childRoutePages(dir);
    if (pages.length) offenders.push(`${relative(admin, dir)}/loading.tsx wraps ${pages.join(", ")}`);
  }
  assert.deepEqual(offenders, [], "move the parent's page.tsx + loading.tsx into an (index) route group");
});

test("route-skeleton registry: every registered route resolves to its OWN loading (most specific first)", () => {
  const src = readFileSync(join(here, "route-skeleton.tsx"), "utf8");
  const imports = new Map([...src.matchAll(/import (L\d+) from "@\/app\/\(admin\)\/(.+)\/loading";/g)].map((m) => [m[1], m[2]]));
  const entries = [...src.matchAll(/\[\/(.+?)\/, (L\d+)\]/g)].map((m) => [new RegExp(m[1]), m[2]]);
  assert.ok(entries.length >= imports.size - 1, "registry parse");
  const wrong = [];
  for (const [id, rel] of imports) {
    const path = "/" + rel.split("/").filter((s) => !isGroup(s)).map((s) => (/^\[.*\]$/.test(s) ? "sample-id" : s)).join("/");
    const hit = entries.find(([re]) => re.test(path));
    if (!hit) wrong.push(`${path}: not registered`);
    else if (hit[1] !== id) wrong.push(`${path}: resolves to ${hit[1]} (${imports.get(hit[1])}), not ${id}`);
  }
  assert.deepEqual(wrong, []);
});
