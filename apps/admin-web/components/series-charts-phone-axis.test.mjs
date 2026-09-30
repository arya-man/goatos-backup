import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: phone-day-axis (FIXJ5, LAND3 P0). On /feed/analytics at 390 the "Daily feed expenditure"
// and "Daily directed feed" day axes fanned 15 dates at -45deg into each other, and the "Feed mix"
// kg axis printed "60,000" into "80,000". The fixes are chart adapter options (components/minimal
// stays verbatim): below sm the day axis keeps five flat labels with Apex hiding any that touch, and
// the Feed mix value axis keeps three ticks with the feed names narrowed. The r2 text-fit
// axis-label-overlap check (full audit, every route, 390 dark) holds the rendered result.
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("series line and column charts bound their day axis on a phone", () => {
  const src = read("./series-charts.tsx");
  assert.match(src, /PHONE_DAY_AXIS_LABELS = \{\s*tickAmount: 4,\s*labels: \{[^}]*hideOverlappingLabels: true,/);
  assert.match(src, /formatter: \(value: string \| number\) => String\(value \?\? ""\)\.replace\(/, "phone day ticks drop the year");
  assert.match(src, /theme\.breakpoints\.down\("sm"\), \{ noSsr: true \}/);
  assert.equal((src.match(/\.\.\.\(phone \? PHONE_DAY_AXIS_LABELS : \{\}\)/g) || []).length, 2, "both the lines and the stacked columns chart");
});

test("Feed mix value axis keeps three ticks and hides overlapping ones on a phone", () => {
  const src = read("../features/feed/feed-mix-card.tsx");
  assert.match(src, /labels: \{ formatter: .*?, \.\.\.\(phone \? \{ hideOverlappingLabels: true \} : \{\}\) \}/);
  assert.match(src, /\.\.\.\(phone \? \{ tickAmount: 2 \} : \{\}\)/);
  assert.match(src, /breakpoints\.down\("sm"\), \{ noSsr: true \}/);
});
