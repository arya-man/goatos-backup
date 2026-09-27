// guard: table-adapter-imports (REVIEW-16 O20). components/minimal/table is the verbatim template;
// pages and components get the Mesha head (contract sort) and the phone-fit pager from
// components/app/table. Importing TablePaginationCustom / TableHeadCustom straight from the
// template folder silently drops the phone layout (rows-per-page hidden, toolbar wrap at 390/412).
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import test from "node:test";

const appDir = new URL("../../..", import.meta.url).pathname;

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name.startsWith(".")) continue;
    const abs = join(dir, name);
    if (statSync(abs).isDirectory()) walk(abs, out);
    else if (/\.(tsx?|mjs)$/.test(name) && !/\.test\.mjs$/.test(name)) out.push(abs);
  }
  return out;
}

test("TablePaginationCustom / TableHeadCustom never come from components/minimal/table outside the adapter", () => {
  const offenders = [];
  for (const root of ["app", "components", "features", "stories", "lib"]) {
    for (const abs of walk(join(appDir, root))) {
      const rel = relative(appDir, abs);
      if (rel.startsWith("components/minimal/") || rel.startsWith("components/app/table/")) continue;
      const src = readFileSync(abs, "utf8");
      for (const m of src.matchAll(/import\s*\{([^}]*)\}\s*from\s*["']@\/components\/minimal\/table(?:\/[\w-]+)?["']/g)) {
        if (/\b(TablePaginationCustom|TableHeadCustom)\b/.test(m[1])) offenders.push(rel);
      }
    }
  }
  assert.deepEqual(offenders, [], "import them from @/components/app/table");
});
