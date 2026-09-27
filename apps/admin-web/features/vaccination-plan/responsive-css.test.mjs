// Guard: vaccination-plan-template. /vaccination/plan and /vaccination/plan/edit are composed from
// template parts (course KPI row, Card + CardHeader, TableHeadCustom, MUI Dialog / Switch /
// ToggleButtonGroup / Chip, template Grid md 4 / md 8), not the mock's hand-made `.vplan` markup and
// its 250 lines of namespaced CSS (deleted 2026-09-27). A `.vplan` / `.vp-*` rule or class coming back,
// a raw <button>/<input>/<select>, or a fixed-position div "modal" fails here.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");
const files = {
  console: read("./plan-console.tsx"),
  sheet: read("./version-sheet.tsx"),
  editor: read("./plan-editor.tsx"),
  anchors: read("./anchor-panel.tsx"),
  duration: read("./duration-field.tsx"),
};
const css = ["../../app/mesha-theme.css", "../../app/frame.css", "../../app/minimal-theme.css"].map(read).join("\n");

test("vaccination-plan-template: no legacy .vplan / .vp-* CSS rules", () => {
  const rules = css.split("\n").filter((line) => /\.(vplan|vp-[a-z])[^{]*\{/.test(line) && !/^\s*(\/\*|\*)/.test(line));
  assert.deepEqual(rules, []);
});

test("vaccination-plan-template: no legacy markup or raw controls", () => {
  for (const [name, source] of Object.entries(files)) {
    assert.doesNotMatch(source, /className="(vplan|card|card-h|card-b|scroll|tabl|btn[^"]*|pill[^"]*|dose[^"]*|sec-label|vp-(modal|sheet|head|body|dur|seg|nv[a-z]*))"/, `${name}: legacy class`);
    assert.doesNotMatch(source, /<(button|select|input)\b(?![^>]*type="hidden")/, `${name}: raw control`);
  }
});

test("vaccination-plan-template: template anatomy", () => {
  // Live-version facts are dates/names, which the template number widgets cannot print: MUI Card +
  // CardHeader (fact as title, label as subheader), never a string forced into CourseWidgetSummary.
  assert.match(files.console, /<Card sx=\{\{ height: 1 \}\}>\s*<CardHeader/);
  assert.doesNotMatch(files.console, /<CourseWidgetSummary/);
  assert.doesNotMatch(files.console, /KpiCard/);
  assert.match(files.console, /<UrlSuspense[^>]*watch=\{\["page"\]\}/, "live table panel keyed by page (url-keyed-panel)");
  assert.match(files.console, /<TableHeadCustom/);
  assert.match(files.sheet, /<Dialog\b/);
  assert.match(files.editor, /size=\{\{ xs: 12, md: 4 \}\}/);
  assert.match(files.editor, /size=\{\{ xs: 12, md: 8 \}\}/);
  assert.match(files.editor, /<ToggleButtonGroup/);
  assert.match(files.editor, /function AddVaccineModal[\s\S]*<Dialog\b/);
  assert.match(files.anchors, /<TableHeadCustom/);
  assert.match(files.duration, /<CustomPopover[\s\S]*?anchorEl=\{pop\.anchorEl\}/, "duration popover is the template popover (viewport-clamped on mobile)");
  assert.match(files.duration, /<Button[\s\S]*?variant=\{plain \? "text" : "soft"\}/);
});
