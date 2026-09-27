import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

// guard: filter-summary-only-with-content (SK1). FilterBar renders its summary strip (px 2.5, pb 2.5)
// whenever `summary` is truthy, so an always-passed chip Box with no chips is 20px of empty padding
// under the toolbar: the loaded card grows past its skeleton twin and the page jumps as it lands
// (/leave, /routines). Pass the summary only when it has content: `summary={chips.length ? (...) : null}`.
const root = new URL("../../", import.meta.url).pathname;
const files = [];
const walk = (dir) => {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p);
    else if (/\.tsx$/.test(name)) files.push(p);
  }
};
walk(join(root, "features"));
walk(join(root, "components"));
// Always has visible content ("{shown} / {total}" count line), so the strip is never empty.
const ALWAYS_CONTENT = new Set(["features/leadership-tasks/task-table.tsx"]);

export function unconditionalSummaries(src) {
  if (!src.includes("<FilterBar")) return 0;
  return (src.match(/\bsummary=\{\s*<(Box|div|Stack)\b/g) ?? []).length;
}

test("self-test: an unconditional chip strip is caught, a conditional one passes", () => {
  assert.equal(unconditionalSummaries(`<FilterBar summary={\n  <Box sx={{}}>{chips.map(x)}</Box>\n}>`), 1);
  assert.equal(unconditionalSummaries(`<FilterBar summary={\n  chips.length ? (\n<Box>{chips.map(x)}</Box>) : null\n}>`), 0);
});

test("FilterBar summary strips render only with content", () => {
  const bad = files
    .map((f) => [f.slice(root.length), readFileSync(f, "utf8")])
    .filter(([rel, src]) => !ALWAYS_CONTENT.has(rel) && unconditionalSummaries(src) > 0)
    .map(([rel]) => rel);
  assert.deepEqual(bad, [], `always-on FilterBar summary (empty 20px strip): ${bad.join(", ")}`);
});
