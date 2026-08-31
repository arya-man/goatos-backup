import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./weights.tsx", import.meta.url), "utf8");
const contract = readFileSync(
  new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url),
  "utf8",
);

test("the period control is a calendar, not a fixed-window select", () => {
  assert.match(source, /kind: "daterange"/);
  // The two presets and the vocabulary behind them are GONE, not left unused: a stale option group
  // reads to the next author as a control that still exists somewhere.
  assert.doesNotMatch(source, /weighing_period/);
  assert.doesNotMatch(source, /periodDays/);
  assert.doesNotMatch(contract, /ID: "weighing_period"/);
  assert.doesNotMatch(contract, /"filter\.period\.4w"/);
  assert.doesNotMatch(contract, /"filter\.period\.12w"/);
});

test("the page lands on the latest two lump-sum weighing dates when no period is selected", () => {
  assert.match(source, /const DEFAULT_WINDOW_DAYS = 15;/);
  assert.match(source, /const LATEST_LUMP_LOOKBACK_DAYS = 400;/);
  assert.match(source, /async function landingWindow/);
  assert.match(source, /getShedWeights\(\{\s*\n\s*park_id: parkID \|\| undefined,\s*\n\s*\.\.\.lookback,/);
  assert.match(source, /const dates = \[\.\.\.new Set\(result\.data\.lump_weighing_dates \?\? \[\]\)\]\.sort\(\);/);
  assert.match(source, /from: dates\[dates\.length - 2\],/);
  // THE END IS NOT A LUMP DATE. On 25 Aug 2026 the farm scanned 199 kids across 17 sheds and weighed
  // no shed whole, so that day never entered lump_weighing_dates and a window closing on the later
  // lump date shut a day early -- dropping all 199 from the KPIs, the gain charts and Fair fight
  // while the period label read as though nothing was missing. The START still comes from the lump
  // dates, because two whole-shed weighs are what make a shed-average movement measurable.
  assert.match(source, /const latest = result\.data\.latest_weighing_date \?\? "";/);
  assert.match(source, /latest > dates\[dates\.length - 1\] \? latest : dates\[dates\.length - 1\]/);
  // Clamped, like every other window this file resolves: a future end is never rendered.
  assert.match(source, /to: end > today \? today : end,/);
  // The date is BACKEND-owned. A max taken across the returned rows would be the page deriving
  // business truth from its own rows, which is the rollup-from-a-slice shape this repo bans.
  assert.doesNotMatch(source, /Math\.max\([^)]*last_weighed_date/);
  assert.match(source, /return \{ from: istDayPlus\(today, -\(DEFAULT_WINDOW_DAYS - 1\)\), to: today \};/);
  // istDayPlus is pure calendar arithmetic on an already-resolved IST day. Re-entering a timezone
  // here (or hardcoding +05:30) is what the shared helper exists to prevent.
  assert.match(source, /import \{ fmtDate, istDayPlus, todayIso \} from "@\/lib\/format";/);
  assert.doesNotMatch(source, /5\.5 \* 60/);
});

test("the default window is passed as NAMED fields, never spread", () => {
  // `{...defaultWindow(today)}` spreads `{from, to}` — the same two keys the SELECTED window uses —
  // and would silently overwrite the resolved latest-two-weighings window. It typechecks and
  // renders; only the data is wrong.
  assert.match(source, /defaultFrom: defaultWindow\(today\)\.from,\s*\n\s*defaultTo: defaultWindow\(today\)\.to,/);
  assert.doesNotMatch(source, /\.\.\.defaultWindow\(/);
});

test("a hand-edited window falls back instead of taking the page down", () => {
  // Inverted, malformed or absent parameters land on the default; a future end clamps to today,
  // because a weigh cannot have happened tomorrow.
  assert.match(source, /rawFrom <= rawTo/);
  assert.match(source, /rawFrom > today \? today : rawFrom/);
  assert.match(source, /if \(rawFrom \|\| rawTo\) return defaultWindow\(today\);/);
  assert.match(source, /return defaultWindow\(today\);/);
});

test("the headline row is five cards, and the gain figure is stated once", () => {
  // The sixth card printed the SAME number, denominator and sub-line as the "All parks — daily
  // gain" card in the row below it. Its copy keys are deleted too, so the duplicate cannot be
  // reinstated by pasting the markup back.
  assert.match(source, /className="grid g5 kpi-row"/);
  assert.doesNotMatch(source, /className="grid g6 kpi-row"/);
  assert.doesNotMatch(source, /"kpi\.gain\.label"/);
  assert.doesNotMatch(contract, /"kpi\.gain\.label":/);
  assert.doesNotMatch(contract, /"kpi\.gain\.sub":/);
  // g5 needs its own responsive ladder; without it the row falls back to one column.
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(css, /\.g5\{grid-template-columns:repeat\(5,minmax\(0,1fr\)\)\}/);
  assert.match(css, /@media\(max-width:1400px\)\{\.g5\{/);
  assert.match(css, /@media\(max-width:640px\)\{\.g5\{grid-template-columns:1fr\}\}/);
});

test("daily gain survives a park-scoped page", () => {
  // The gain row used to render ONLY when the page showed more than one park, because the deleted
  // headline card carried the figure in every other scope. Keeping that condition would have
  // removed the growth number entirely from a park-scoped page — the one thing this screen is for.
  assert.doesNotMatch(source, /perParkGain\.length > 0 \?/);
  // Its first card names the CURRENT scope, so "All parks" is only shown when it really is all.
  assert.match(source, /\{selectedParkName \|\| copy\(pageContract, "kpi\.park_gain\.all"\)\}/);
  // Never the raw park id: that would put an internal identifier in front of a CEO.
  assert.match(source, /parks\.find\(\(park\) => park\.park_id === parkFilter\)\?\.name \?\? ""/);
});

test("shed lists and gain chart only show sheds weighed in the selected window", () => {
  assert.match(source, /const weighedRows = rows\.filter\(\(row\) => row\.animals_weighed > 0\);/);
  assert.match(source, /modeFilter === "all" \? weighedRows : weighedRows\.filter/);
  assert.match(source, /const visibleRowKeys = new Set\(visibleRows\.map\(\(row\) => shedKey\(row\.location_id, row\.partition_label\)\)\);/);
  assert.match(source, /shed\.adg_pair_count > 0 && visibleRowKeys\.has\(shedKey\(shed\.location_id, shed\.partition_label\)\)/);
  // A shed with ONE weigh has no daily gain, so it is not plotted in the gain chart at all.
  // It used to be — with a zero-length bar labelled in KILOGRAMS beside real g/day bars, which
  // put "Castro 1 · 34.4 kg" in a daily-gain chart. Two measures on one axis is the defect;
  // these three assertions are what pinned it, so they now pin its absence.
  assert.doesNotMatch(source, /const singleWeighRows/);
  assert.match(source, /const gainChartData = \[\.\.\.perAnimalGainRows, \.\.\.shedAverageGainRows\]\.sort\(/);
  // No hand-written kg label survives anywhere in this page's chart data. The Weight view
  // still shows every shed's average — it carries `unit: "kg"` on the SERIES, so the unit is
  // declared once for the whole chart and cannot leak into the gain chart beside it.
  assert.equal(source.match(/valueLabel: `\$\{kg\(row\.average_weight_kg\)\} kg`/g)?.length ?? 0, 0);
  assert.match(source, /unit: "kg"/);
});

test("weighed shed rows render breed and sex composition chips from the backend contract", () => {
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(source, /demo\?\.shed_composition \?\? \[\]/);
  assert.match(source, /shedLabelWithComposition/);
  // Three, not four: the gain chart no longer builds a row for a shed with one weigh.
  assert.equal(source.match(/label: shedLabelWithComposition\(/g)?.length, 3);
  assert.match(source, /replaceAll\(" · ", " - "\)/);
  // The chart's per-cohort suffix carries the resident COUNT (maintainer ask
  // 2026-08-31), from the chip's own backend `animals` figure — same number the
  // sheds table chips have always shown.
  assert.match(source, /× \$\{chip\.animals\.toLocaleString\("en-IN"\)\}/);
  assert.doesNotMatch(source, /shed avg/);
  assert.match(source, /className="wcomp-chips"/);
  assert.match(source, /className="wcomp-chip"/);
  assert.match(source, /compositionLabel\(chip, pageContract\)/);
  assert.match(source, /copy\(pageContract, "composition\.unknown_breed"\)/);
  assert.match(source, /copy\(pageContract, "composition\.unknown_sex"\)/);
  assert.match(contract, /"composition\.unknown_breed":/);
  assert.match(contract, /"composition\.unknown_sex":/);
  assert.doesNotMatch(source, /composition\.source === "scanned_tags"/);
  assert.match(css, /\.wcomp-chips\{/);
  assert.match(css, /\.wcomp-chip\{/);
});

test("small shed charts do not reserve the tall empty panel height", () => {
  assert.match(source, /size: gainChartData\.length <= 8 \? \("short" as const\) : \("tall" as const\)/);
  assert.match(source, /size: chartData\.length <= 8 \? \("short" as const\) : \("tall" as const\)/);
  assert.match(source, /<ShedMetricChart/);
});

test("both shed-chart metrics use ONE order, so the toggle only changes the bars", () => {
  // The gain view has been alphabetical since 2026-08-22 so an operator can find a pen by
  // name. The weight view was heaviest-first, so switching metric reshuffled every row on a
  // control the reader expects to change only the measure. Both now sort by the composed
  // label with numeric collation ("Castro 2" before "Castro 10").
  // Counted, not matched once: the two call sites are formatted differently (one wraps),
  // so this asserts BOTH series carry the same comparator rather than that one exists.
  const alphabetical = /a\.label\.localeCompare\(b\.label, undefined, \{ numeric: true \}\)/g;
  assert.equal((source.match(alphabetical) ?? []).length, 2, "both chart series must sort A→Z by label");
  assert.match(source, /const gainChartData = \[[\s\S]{0,120}\.sort\(/);
  assert.match(source, /const chartData = visibleRows[\s\S]{0,1400}\.sort\(\(a, b\) => a\.label\.localeCompare/);
  // The weight ranking must not come back: it is the specific behaviour being replaced.
  assert.doesNotMatch(source, /sort\(\(a, b\) => b\.average_weight_kg - a\.average_weight_kg\)[\s\S]{0,400}chartData/);
});

test("chart metric switches are local state, not route reloads", () => {
  const client = readFileSync(new URL("./metric-chart.tsx", import.meta.url), "utf8");
  assert.match(client, /"use client"/);
  assert.match(client, /useState<Metric>/);
  assert.match(client, /type="button"/);
  assert.match(source, /series=\{\{\s*adg:/);
  assert.doesNotMatch(source, /hrefWith\(params, \{ \[param\]: option \}\)/);
  assert.doesNotMatch(source, /breed_metric"\} current/);
  assert.doesNotMatch(source, /shed_metric"\} current/);
});

test("full-width shed chart labels fit without overlapping rows", () => {
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(css, /\.wcols \.wbar\{[^}]*min-height:86px/);
  assert.match(css, /@media\(max-width:1200px\)\{\.wcols \.wbar\{[^}]*min-height:92px/);
  assert.match(css, /\.wbar \.wbl-text\{[^}]*display:-webkit-box/);
  assert.match(css, /\.wbar \.wbl-text\{[^}]*-webkit-line-clamp:2/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*display:-webkit-box/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*-webkit-line-clamp:3/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*white-space:normal/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*overflow:hidden/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*text-overflow:ellipsis/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*overflow-wrap:anywhere/);
  assert.doesNotMatch(css, /\.wcols \.wbar \.wbl\{[^}]*overflow:visible/);
  assert.match(css, /\.wbar\{[^}]*grid-template-columns:minmax\(88px,clamp\(160px,36%,260px\)\) minmax\(72px,1fr\) 72px/);
  assert.match(css, /\.wbar\{[^}]*min-height:34px/);
  assert.match(css, /\.wbar \.wbl\{[^}]*min-width:0/);
  assert.match(css, /\.wbar \.wbt\{[^}]*min-width:0/);
  assert.match(css, /@media\(max-width:900px\)\{\.wbar\{[^}]*minmax\(0,112px\)/);
});

test("the two table cards are inset without losing their full-bleed tables", () => {
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(source, /className="card wtable" aria-label=\{copy\(pageContract, "section\.sheds\.aria"\)\}/);
  assert.match(source, /className="card wtable" aria-label=\{copy\(pageContract, "section\.losing\.aria"\)\}/);
  // Vertical padding on the CARD, horizontal on its children — never on the card itself, which
  // would inset the table away from its own header rule and row separators.
  assert.match(css, /\.wtable\{padding:14px 0\}/);
  assert.match(css, /\.wtable > :not\(\.tablewrap\)\{padding-left:16px;padding-right:16px\}/);
  // The outer columns match the card's own 17px text edge; a cell padding, so the rules still reach
  // the frame.
  assert.match(css, /\.wtable table\.tbl th:first-child,\s*\n\.wtable table\.tbl td:first-child\{padding-left:16px\}/);
});

test("every visible string on the weighing calendar is backend-contract copy", () => {
  for (const key of [
    "filter.period.label",
    "filter.period.today",
    "filter.period.single",
    "filter.period.range",
    "filter.period.aria",
    "filter.period.previous_month",
    "filter.period.next_month",
    "filter.period.range_start_hint",
    "filter.period.range_end_hint",
    "filter.period.range_separator",
    "filter.period.lump_marker_hint",
  ]) {
    const escaped = key.replace(/\./g, "\\.");
    assert.match(source, new RegExp(`copy\\(pageContract, "${escaped}"\\)`), `page: ${key}`);
    assert.match(contract, new RegExp(`"${escaped}":`), `contract: ${key}`);
  }
});

test("the weighing calendar marks backend-reported lump-sum weigh dates", () => {
  assert.match(source, /markerHint: copy\(pageContract, "filter\.period\.lump_marker_hint"\)/);
  assert.match(source, /markerFetchPath: `\/api\/weighing\/lump-markers/);
  assert.doesNotMatch(source, /markerDates: lumpWeighingDates/);
  assert.match(contract, /"filter\.period\.lump_marker_hint":/);
});

test("lump marker proxy reads a calendar window independently of the report range", () => {
  const route = readFileSync(
    new URL("../../app/api/weighing/lump-markers/route.ts", import.meta.url),
    "utf8",
  );
  assert.match(route, /url\.searchParams\.get\("from"\)/);
  assert.match(route, /url\.searchParams\.get\("to"\)/);
  assert.match(route, /getShedWeights\(\{\s*\n\s*park_id: parkID \|\| undefined,\s*\n\s*from,\s*\n\s*to,/);
  assert.match(route, /dates: result\.data\.lump_weighing_dates/);
});
