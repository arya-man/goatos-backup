import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

// guard: window-custom-state (TR1-#9). "Custom" is the state of a window picked in the calendar, not
// a destination: on a preset window its link pointed at the page it was already on, so a click did
// nothing and the tab never selected (audit interact: tab-not-selected). It is a disabled label
// unless the served window is custom, and SegmentedLinks carries `disabled` through to the Tab.
test("health analytics: the Custom window option is disabled unless it is the served window", () => {
  const page = read("./health-analytics.tsx");
  assert.match(page, /value: "custom",[\s\S]{0,200}disabled: selectedPreset !== "custom",/);
  const seg = read("../../components/segmented-links.tsx");
  assert.match(seg, /disabled\?: boolean;/);
  assert.match(seg, /disabled: option\.disabled,/);
});

// guard: health-loading-mirror (TR1-#1/#2, Ravi: "the skeleton doesn't match the content"). The
// loading.tsx walks the page's blocks in order: header, window row (field + segment strip + caption),
// five KPI course cards on the page's own md:4 grid, the tab strip, the chart cards.
test("health analytics loading mirrors the page blocks", () => {
  const loading = read("../../app/(admin)/health/analytics/loading.tsx");
  assert.match(loading, /<PageSkeleton root="" gap=\{3\}>/);
  assert.match(loading, /<ControlRowSkeleton caption=\{\d+\}>\s*<FieldSkeleton width=\{\{ xs: "100%", md: 296 \}\} height=\{\{ xs: 44, md: 56 \}\} \/>\s*<TabsSkeleton count=\{5\} variant="pill" \/>/);
  assert.match(loading, /<KpiRowSkeleton count=\{5\} hint size=\{\{ xs: 12, sm: 6, md: 4 \}\} \/>/);
  assert.match(read("./health-analytics.tsx"), /md = 4,/);
});
