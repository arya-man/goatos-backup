// guard: preflight-pseudo-border — admin-web loads Tailwind preflight, which gives every
// ::before/::after `border-style: solid`. The template kanban ColumnRoot's idle pseudo element sets
// only borderWidth (it relies on the browser default `none`), so it drew a solid currentColor ring
// round every /tasks column in dark mode. Template files stay verbatim, so every use site of that
// ColumnRoot names the idle borderStyle in its sx.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

function files(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? files(join(dir, e.name)) : e.name.endsWith(".tsx") ? [join(dir, e.name)] : []));
}

export function columnRootOffences(src) {
  if (!/from "@\/components\/minimal\/sections\/kanban\/column\/styles"/.test(src)) return [];
  const out = [];
  for (const m of src.matchAll(/<ColumnRoot\b[\s\S]*?>/g)) {
    if (!/"&::before":\s*\{\s*borderStyle:/.test(m[0])) out.push(m[0].slice(0, 60));
  }
  return out;
}

test("self-test", () => {
  const imp = `import { ColumnRoot } from "@/components/minimal/sections/kanban/column/styles";\n`;
  assert.equal(columnRootOffences(imp + `<ColumnRoot sx={{ flexGrow: 1 }}>`).length, 1);
  assert.equal(columnRootOffences(imp + `<ColumnRoot sx={{ flexGrow: 1, "&::before": { borderStyle: "none" } }}>`).length, 0);
});

test("kanban ColumnRoot use sites neutralise the preflight pseudo border", () => {
  const offenders = [];
  for (const f of files(join(root, "features"))) for (const o of columnRootOffences(readFileSync(f, "utf8"))) offenders.push(`${f.slice(root.length + 1)}: ${o}`);
  assert.deepEqual(offenders, []);
});
