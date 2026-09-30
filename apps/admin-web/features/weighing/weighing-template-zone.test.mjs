// guard: weighing-template-zone (FIXJ6-W). The weighing tables (pens, FCR pens, feed by weight band,
// week-gain pivots) moved off the legacy `.tbl` / `.loadwise-table` / `.wt-feedband*` / `.wt-band*`
// stylesheet rules onto MUI Table parts + theme sx. This pins the whole area as a legacy-free zone,
// keeps the retired class names out of the code and the stylesheets, and keeps the phone-only
// behaviours the rules used to carry (table hidden under 768px with stacked cards, 44px exit taps).
import test from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";

const read = (rel) => readFileSync(new URL(`../../${rel}`, import.meta.url), "utf8");
const RETIRED = /\b(?:wt-feedband[\w-]*|wt-band(?:c|bar)|loadwise-table|tbl)\b/;

test("features/weighing and app/(admin)/weighing stay legacy-free zones", () => {
  const zones = JSON.parse(read("scripts/legacy-free-zones.json")).zones;
  assert.ok(zones.includes("features/weighing/"), "features/weighing/ left the legacy-free zones");
  assert.ok(zones.includes("app/(admin)/weighing/"), "app/(admin)/weighing/ left the legacy-free zones");
});

test("the retired weighing table classes are gone from code and stylesheets", () => {
  for (const rel of [
    "features/weighing/feed-weight-band-table.tsx",
    "features/weighing/fcr-pens-table.tsx",
    "features/weighing/pens-table.tsx",
    "features/weighing/week-gain-table.tsx",
    "features/weighing/load-week-gain-table.tsx",
    "features/weighing/pen-week-gain-table.tsx",
  ]) {
    const src = read(rel);
    assert.ok(!/cellClassName|headerClassName/.test(src), `${rel}: column styling goes through meta.cellStyle / align, never a class name`);
    for (const m of src.matchAll(/className\s*[=:]\s*(["'`{])([^\n]*)/g)) {
      assert.ok(!RETIRED.test(m[2]), `${rel}: retired weighing class in ${m[0].slice(0, 80)}`);
    }
  }
  for (const rel of ["app/mesha-theme.css", "app/minimal-theme.css", "app/frame.css"]) {
    if (!existsSync(new URL(`../../${rel}`, import.meta.url))) continue;
    const css = read(rel).replace(/\/\*[\s\S]*?\*\//g, "");
    assert.ok(!/\.(?:wt-feedband|wt-band|loadwise-table|tbl)\b/.test(css), `${rel} still styles a retired weighing class`);
  }
});

test("feed-by-weight-band keeps its phone cards and 44px exit taps", () => {
  const src = read("features/weighing/feed-weight-band-table.tsx");
  assert.match(src, /breakpoints\.down\(768\)\]: \{ display: "none" \}/, "the twelve-column table must hide under 768px");
  assert.match(src, /breakpoints\.down\(768\)\]: \{ display: "flex" \}/, "the stacked phone cards must show under 768px");
  assert.match(src, /minHeight: "var\(--tap-min\)"/, "the phone exit note link must be a 44px tap target");
  assert.match(src, /data-feedband-exit/, "the exit link keeps its hook for overlay-journeys");
  const journeys = read("scripts/lib/overlay-journeys.mjs");
  assert.ok(!/wt-feedband/.test(journeys), "overlay-journeys still targets a retired weighing class");
});
