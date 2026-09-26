// guard: kpi-template-anatomy. The shared KPI card renders the template widget summaries so it is
// right in dark and light: EcommerceWidgetSummary (paper card, chart right) by default,
// CourseWidgetSummary (icon corner) with an icon, AnalyticsWidgetSummary only when a page asks for it.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const card = readFileSync(new URL("./minimal/widgets/kpi-card.tsx", import.meta.url), "utf8");
const spark = readFileSync(new URL("./minimal/widgets/widget-spark-chart.tsx", import.meta.url), "utf8");
const course = readFileSync(new URL("./minimal/widgets/course-widget-summary.tsx", import.meta.url), "utf8");

test("KpiCard defaults to the EcommerceWidgetSummary anatomy on the theme paper", () => {
  assert.match(card, /variant = "plain"/);
  assert.match(card, /sparkVariant = "line"/);
  // The plain and course cards never set their own background: Card paints background.paper.
  const plain = card.slice(card.indexOf("const cardSx = hero"));
  assert.match(plain, /: \{ \.\.\.pad, height: 1, position: "relative" as const, isolation: "isolate" as const \}/);
  // The pastel common.white + lighter/light gradient exists only on the hero (Analytics) branch.
  assert.equal(card.match(/common\.white/g)?.length, 1);
  assert.match(card, /hero\s*\n?\s*\? \(t: Theme\) => \(\{[\s\S]*?backgroundColor: "common\.white"/);
});

test("the plain mini chart is the Ecommerce gradient line (tone light -> main, 100x66)", () => {
  assert.match(card, /\[theme\.palette\[color\]\.light, theme\.palette\[color\]\.main\]/);
  assert.match(spark, /type: 'gradient'/);
  assert.match(spark, /width: 100, height: 66/);
});

test("an icon card without a series is the CourseWidgetSummary anatomy", () => {
  assert.match(card, /const course = !hero && !hasChart && Boolean\(icon\)/);
  assert.match(card, /top: 24,\s*right: 20,\s*width: 36,\s*height: 36/);
  assert.match(card, /transform: "rotate\(40deg\)"/);
  assert.match(course, /Copied from the licensed MUI Minimal template \(sections\/overview\/course\/course-widget-summary\.tsx\)/);
});

test("the pastel Analytics card keeps its contents on the light scheme in dark mode", () => {
  assert.match(card, /hero && color && "kit-scheme-light"/);
  assert.match(card, /hero && color \? \{ "data-theme": "light" \} : \{\}/);
});
