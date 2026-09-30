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
  // guard: kpi-subline-in-flow (TR2-P1-2): the sub-line prints in flow under the figure/title block
  // (the template content box's ::after, text carried by --kpi-caption), never pinned to the card
  // bottom by an absolutely positioned overlay, so a tile hugs its content like the template's.
  assert.match(adapter, /"&::after": \{\s*content: "var\(--kpi-caption\)"/, "caption renders in flow under the title");
  assert.match(adapter, /"--kpi-caption": cssString\(caption\)/, "caption text rides on the card");
  assert.doesNotMatch(adapter, /position: "absolute"[^\n]*bottom/, "sub-line is not pinned to the card bottom");
  assert.doesNotMatch(adapter, /<Tooltip\b/, "caption is not hover-only");
  // REVIEW-11: a cut sub-line is hover-only again. It wraps in full: no noWrap / ellipsis / clamp.
  assert.doesNotMatch(adapter, /\bnoWrap\b|textOverflow|WebkitLineClamp|lineClamp/, "sub-line never truncates");
  // Each trend period maps to the template widget whose fixed period text is true.
  assert.match(adapter, /period === "week"[\s\S]{0,200}<EcommerceWidgetSummary/);
  assert.match(adapter, /period === "7d"[\s\S]{0,200}<AppWidgetSummary/);
  // kpi-row-one-kind (J2 P1-8): a month trend stays on the Course card (the change leads the
  // sub-line), so a KPI row never mixes the Booking anatomy with Course tiles.
  assert.doesNotMatch(adapter, /<BookingWidgetSummary/);
  assert.match(adapter, /t\.period === "month" \? monthChange\(t\.percent\)/);
  assert.match(adapter, /data-kpi-kind=\{kind\}/);
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

// guard: kpi-map-truth (TR1-#17). A route mapped to the Ecommerce overview names the widget each of
// its KPI tiles REALLY renders through the adapter: EcommerceWidgetSummary only where a tile passes a
// weekly series (trend period "week") or renders the widget directly; a tile with no trend is a
// CourseWidgetSummary and the row must say so; 7d -> AppWidgetSummary; month -> CourseWidgetSummary
// (the change leads the sub-line, J2 P1-8).
export function kpiMapFindings(mapText, readSource) {
  const out = [];
  for (const line of mapText.split("\n")) {
    if (!line.startsWith("| `/")) continue;
    const cells = line.split("|").map((c) => c.trim());
    if (!/Ecommerce overview/.test(cells[2] ?? "")) continue;
    const route = cells[1];
    const blocks = cells[5] ?? "";
    const src = [...(cells[3] ?? "").matchAll(/`([^`]+\.tsx)`/g)].map((m) => readSource(m[1]) ?? "").join("\n");
    const calls = [...src.matchAll(/<KpiWidget\b[\s\S]*?\/>/g)].map((m) => m[0]);
    if (!calls.length) continue;
    const week = /period: "week"/.test(src) || /<EcommerceWidgetSummary\b/.test(src);
    const noTrend = calls.some((c) => !/\btrend=/.test(c)) || /period: "month"/.test(src);
    if (/EcommerceWidgetSummary/.test(blocks) && !week) out.push(`${route}: claims EcommerceWidgetSummary but no tile passes a weekly series`);
    if (noTrend && !/CourseWidgetSummary/.test(blocks)) out.push(`${route}: has trend-less tiles (CourseWidgetSummary) the row does not name`);
    if (/period: "7d"/.test(src) && !/AppWidgetSummary/.test(blocks)) out.push(`${route}: 7-day tiles render AppWidgetSummary, not named`);
    if (/BookingWidgetSummary/.test(blocks)) out.push(`${route}: names BookingWidgetSummary, which the KPI adapter no longer renders (month tiles are CourseWidgetSummary)`);
  }
  return out;
}

test("kpi-map-truth: self-test", () => {
  const row = (blocks) => `| \`/x\` | Ecommerce overview | \`a.tsx\` | mods | ${blocks} |`;
  const noSeries = () => `<KpiWidget title="t" total={1} />`;
  assert.deepEqual(kpiMapFindings(row("KPIs → CourseWidgetSummary"), noSeries), []);
  assert.equal(kpiMapFindings(row("KPIs → EcommerceWidgetSummary"), noSeries).length, 2);
  const weekly = () => `<KpiWidget title="t" total={1} trend={{ percent: 1, period: "week", series }} />`;
  assert.deepEqual(kpiMapFindings(row("gain → EcommerceWidgetSummary"), weekly), []);
});

test("kpi-map-truth: Ecommerce-overview map rows name the widgets their tiles render", () => {
  const map = readFileSync(join(appDir, "../../docs/design/page-template-map.md"), "utf8");
  const readSource = (rel) => { try { return read(rel); } catch { return null; } };
  assert.deepEqual(kpiMapFindings(map, readSource), []);
});

test("kpi-subline-in-flow: cssString escapes quotes, backslashes and line breaks", async () => {
  const src = adapter.match(/export function cssString[\s\S]*?\n}/)[0].replace(/\(text: string\): string/, "(text)");
  const cssString = new Function(`${src.replace("export ", "")}; return cssString;`)();
  assert.equal(cssString('a "b" \\ c\nd'), '"a \\"b\\" \\\\ c d"');
});

// guard: kpi-row-no-stretch (TR2-P1-2): a KPI grid on the Sales pages never takes the row height of
// a taller sibling card (no `height: 1` on the KPI container) and Farm value's KPI deck holds no
// form control (the Over 35 kg error margin lives in its own toolbar form).
test("kpi-row-no-stretch: sales KPI containers hug their tiles", () => {
  const sold = read("features/procurement/sales-sold.tsx");
  assert.doesNotMatch(sold, /aria-label=\{copy\(pageContract, "section\.sold\.aria"\)\} sx=\{\{ height: 1 \}\}/, "sold KPI container stretches to the side card");
  const over35 = read("features/procurement/over35-kpi.tsx");
  const kpi = over35.match(/export function Over35Kpi[\s\S]*?\n}\n/)[0];
  assert.doesNotMatch(kpi, /SalesReadyToleranceControl|Slider|Button/, "Over 35 KPI tile embeds a form control");
});
