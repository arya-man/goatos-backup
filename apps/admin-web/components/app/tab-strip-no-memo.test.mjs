import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

// guard: tab-strip-no-memo (TR1-#38). A memo()'d client component rendered from a server page
// remounted on every same-route navigation, so each tab / filter click rebuilt the tab strip (audit
// interact: tabs-remount on /alerts, /approvals, /weighing/*). The strip is a plain function.
test("TemplateTabs is a plain function component, never memo()", () => {
  const src = read("./template-tabs.tsx");
  assert.match(src, /export function TemplateTabs\(/);
  assert.doesNotMatch(src, /import \{[^}]*\bmemo\b|=\s*memo\(/);
});

// guard: gain-view-local (TR1-#38). The weights gain-mark Chart/Table switch lived inside the URL
// panel, so a click re-rendered the whole panel from the server and tore the strip down. It is a
// LocalViewToggle over two LocalViewPanes (URL param rewritten in place, no navigation).
test("weights gain-mark view is a local toggle", () => {
  const src = read("../../features/weighing/weights.tsx");
  assert.match(src, /<LocalViewToggle\s+param=\{GAIN_VIEW_PARAM\}/);
  assert.equal((src.match(/<LocalViewPane param=\{GAIN_VIEW_PARAM\}/g) ?? []).length, 2);
  assert.match(src, /const PANEL_IGNORE = \["wt_export", GAIN_VIEW_PARAM\] as const;/);
});
