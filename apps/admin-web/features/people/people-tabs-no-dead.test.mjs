// guard: no-disabled-contract-tabs (TR-2 P1-4). /people renders only the module tabs the contract
// ENABLES (and the view_clock gate allows): a disabled "soon" tab is a dead control. With one tab
// left the strip is dropped, so the page never stacks an inert strip over the status tabs. The
// people toolbar selects apply on change (template UserTableToolbar has no Apply button); the
// submit button only shows inside the phone filters drawer.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

export function deadTabFindings(src) {
  const out = [];
  if (/disabled:\s*!\s*\w+\.enabled/.test(src)) out.push("module tab rendered disabled from the contract");
  if (!/optionGroup\(pageContract, "people_view_tabs"\)\.filter\(\(tab\) => tab\.enabled/.test(src)) out.push("people_view_tabs not filtered to enabled tabs");
  if (!/tabs\.length > 1 \?/.test(src)) out.push("single-tab strip not dropped");
  return out;
}

test("people page renders no disabled module tab", () => {
  assert.deepEqual(deadTabFindings(read("./people-page.tsx")), []);
});

test("self-test: the old inert-tab mapping is caught", () => {
  const bad = `const tabs = optionGroup(pageContract, "people_view_tabs").map((t) => t);\n<TemplateTabs items={tabs.map((tab) => ({ disabled: !tab.enabled }))} />`;
  assert.equal(deadTabFindings(bad).length, 3);
});

test("people toolbar: selects apply on change, Apply only in the phone drawer", () => {
  const board = read("./people-board.tsx");
  const selects = [...board.matchAll(/<PeopleFormSelect\b[\s\S]*?\/>/g)].map((m) => m[0]);
  assert.ok(selects.length >= 2, "toolbar selects found");
  for (const el of selects) assert.match(el, /\bautoSubmit\b/, "toolbar select submits on change");
  assert.match(board, /type="submit" form="people-filter-form"[^>]*display: \{ xs: "inline-flex", sm: "none" \}/, "Apply hidden from sm up");
  assert.match(read("./people-form-select.tsx"), /autoSubmit && form\)[\s\S]{0,120}requestSubmit\(\)/);
});
