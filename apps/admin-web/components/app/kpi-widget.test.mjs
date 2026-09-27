// guard: kpi-widget-fidelity (REVIEW-8 O12-O14). The KPI adapter swaps the look, never the data:
//  - the figure is a number (the verbatim template widgets print fNumber / fShortenNumber, a string
//    renders "NaN"); units / remainders / no-data text lead the sub-line instead;
//  - the sub-line renders VISIBLY inside the card, never as a hover-only tooltip;
//  - a tile that had a real series still passes one (trend -> the template widget whose own period
//    text is true), or the page's chart card draws it;
//  - titles are the contract labels, no unit suffix glued on.
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";

const appDir = new URL("../..", import.meta.url).pathname;
const read = (rel) => readFileSync(join(appDir, rel), "utf8");
const adapter = read("components/app/kpi-widget.tsx");

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const abs = join(dir, name);
    if (statSync(abs).isDirectory()) walk(abs, out);
    else if (/\.tsx$/.test(name) && !/\.stories\.tsx$/.test(name)) out.push(abs);
  }
  return out;
}

test("adapter: numeric figure only, visible sub-line", () => {
  assert.match(adapter, /total: number \| null \| undefined;/, "figure is typed number | null");
  assert.doesNotMatch(adapter, /total\??: [^;\n]*string/, "no string figure");
  assert.match(adapter, /<Typography[\s\S]{0,200}data-kpi-subline[\s\S]{0,300}\{caption\}/, "caption renders as visible Typography");
  assert.doesNotMatch(adapter, /<Tooltip\b/, "caption is not hover-only");
  // REVIEW-11: a cut sub-line is hover-only again. It wraps in full: no noWrap / ellipsis / clamp.
  assert.doesNotMatch(adapter, /\bnoWrap\b|textOverflow|WebkitLineClamp|lineClamp/, "sub-line never truncates");
  assert.match(adapter, /content: "attr\(data-kpi-caption\)"/, "card reserves the wrapped sub-line height");
  // Each trend period maps to the template widget whose fixed period text is true.
  assert.match(adapter, /period === "week"[\s\S]{0,200}<EcommerceWidgetSummary/);
  assert.match(adapter, /period === "7d"[\s\S]{0,200}<AppWidgetSummary/);
  assert.match(adapter, /period === "month"[\s\S]{0,200}<BookingWidgetSummary/);
});

test("KPI call sites pass no string figure and no unit glued onto the title", () => {
  const offenders = [];
  for (const abs of walk(join(appDir, "features"))) {
    const src = readFileSync(abs, "utf8");
    for (const m of src.matchAll(/<KpiWidget(?:Action)?\b[\s\S]*?\/>/g)) {
      const el = m[0];
      if (/total=\{`|total="|total=\{[^}]*\?\s*"[^"]*"\s*:/.test(el)) offenders.push(`${abs}: string figure`);
      if (/title=\{`[^`]*\((?:₹|%|kg|g|h|d|\/day|\$\{[^}]*\})\)`\}/.test(el)) offenders.push(`${abs}: unit in title`);
    }
  }
  assert.deepEqual(offenders, []);
});

test("tiles that had a series still carry one (or the page chart draws it)", () => {
  const expect = [
    ["features/counts/herd-analytics.tsx", /trend=\{monthTrend\("births"\)\}[\s\S]*trend=\{monthTrend\("deaths"\)\}[\s\S]*trend=\{monthTrend\("sold"\)\}/],
    ["features/counts/mortality.tsx", /trend: monthTrend\(data\.months\.map\(\(m\) => m\.deaths\)\)[\s\S]*m\.kids[\s\S]*m\.adults/],
    ["features/health/health-analytics.tsx", /monthly=\{newCaseRowsByMonth[\s\S]*monthly=\{deathRowsByMonth/],
    ["features/feed/feed-analytics.tsx", /trend: dayTrend\(\(d\) => num\(d\.directed_kg\)\)[\s\S]*trend: dayTrend\(\(d\) => d\.head_days\)[\s\S]*trend: dayTrend\(\(d\) =>/],
    ["features/weighing/weights.tsx", /trend=\{weeklyGainPercent == null \? null : \{ percent: weeklyGainPercent, period: "week", series: weeklyGainSpark/],
    // Sold: revenue / animals / manure are the Month-by-month chart's series; price per kg is its fourth.
    ["features/procurement/sales-sold.tsx", /name: copy\(pageContract, "kpi\.realized_price"\)[\s\S]*pricedMonths\.map\(\(month\) => Math\.round\(month\.realized_price_per_kg\)\)/],
  ];
  for (const [rel, re] of expect) assert.match(read(rel), re, rel);
});
