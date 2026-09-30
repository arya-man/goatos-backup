import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

// TR-2 template anatomy guards for /counts/herd and /counts/breakdown (P1-8, P1-9).
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");
const herdRegister = read("./herd-register.tsx");
const filters = read("./counts-breakdown-filters.tsx");
const pensTable = read("./counts-breakdown-pens-table.tsx");
const tagEditor = read("./shed-tag-editor.tsx");
const meshaTheme = legacyCss("mesha-theme");

test("guard: herd-header-one-primary - /counts/herd header is Register animal + the ⋮ menu, no extra/dead button", () => {
  const header = herdRegister.slice(herdRegister.indexOf("<PageHeader"), herdRegister.indexOf("/>", herdRegister.indexOf("</>", herdRegister.indexOf("<PageHeader"))));
  assert.match(header, /<HerdActions/);
  assert.doesNotMatch(header, /<Button\b/, "a second header Button wraps onto its own row at 390 (TR2-P1-8); secondary actions go in the ⋮ RowMenu");
});

test("guard: breakdown-filters-no-apply - the breakdown filter bar applies on change, with no Apply button", () => {
  assert.doesNotMatch(filters, /filter\.apply/, "template UserTableToolbar has no Apply button (TR2-P1-9)");
  assert.match(filters, /onClose=\{applyStaged\}/, "multi-select ticks apply when the menu closes");
  assert.match(filters, /applyNow\(field\.param/, "single selects apply on pick");
});

test("guard: breakdown-cells-no-chips - pen composition cells are text lines, not Label/chip clouds", () => {
  assert.doesNotMatch(pensTable, /<Label\b/, "chip clouds in cells (TR2-P1-9)");
  assert.doesNotMatch(tagEditor, /className="tag"/, "the legacy 12px .tag chip in the stage cell");
});

test("guard: breakdown-head-14px - no rule shrinks the pen table head below the template 14px", () => {
  assert.doesNotMatch(meshaTheme, /counts-pens-table th\{font-size:11px/);
  assert.doesNotMatch(meshaTheme, /counts-pens-table td\{padding-left:12px/);
});

test("guard: herd-status-dash-plain - an unset herd status is the plain dash, not a grey Label", () => {
  assert.match(herdRegister, /function StatusCell[\s\S]{0,300}if \(!value\) return <Box component="span" sx=\{\{ color: "text\.disabled" \}\}>/);
  assert.doesNotMatch(herdRegister, /<Label variant="soft" color=\{TONE_COLOR\[statusTone\(g\./, "status cells go through StatusCell");
});
