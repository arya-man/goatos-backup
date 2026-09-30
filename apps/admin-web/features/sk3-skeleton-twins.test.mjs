// guard: sk3-skeleton-twins (REVIEW-45 O72). A route's loading twin imports the page's layout instead of
// retyping it: one features/<x>/*-layout.ts holds the Grid sizes, card counts, field lists and page
// sizes, read by BOTH the page and its twin; the route loading.tsx and the page's UrlSuspense
// fallbacks render the same exported twin component.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (rel) => readFileSync(new URL(`./${rel}`, import.meta.url), "utf8");
const app = (rel) => readFileSync(new URL(`../app/(admin)/${rel}`, import.meta.url), "utf8");
// A literal Grid size inside a twin is a retyped layout.
const LITERAL_SIZE = /size[=:]\s*\{\{?\s*xs:/;

test("weighing twins read weights-analytics-layout.ts, as the pages do", () => {
  const twin = read("weighing/weights-skeletons.tsx");
  assert.match(twin, /from "\.\/weights-analytics-layout"/);
  assert.doesNotMatch(twin, LITERAL_SIZE, "weights-skeletons.tsx: Grid sizes come from KIDS_GRID / GENERAL_GRID");
  for (const key of ["kpi", "ring", "breedGain", "breed", "sexStage"]) assert.match(read("weighing/weights.tsx"), new RegExp(`size=\\{KIDS_GRID\\.${key}\\}`), `weights.tsx uses KIDS_GRID.${key}`);
  for (const key of ["kpi", "ring", "weekly", "rank", "parkGain"]) assert.match(read("weighing/weights-analytics.tsx"), new RegExp(`size=\\{GENERAL_GRID\\.${key}\\}`), `weights-analytics.tsx uses GENERAL_GRID.${key}`);
  assert.match(read("weighing/weights.tsx"), /fallback=\{<WeightsKidsPanelSkeleton \/>\}/);
  assert.match(read("weighing/weights-analytics.tsx"), /general: <WeightsGeneralPanelSkeleton \/>/);
  assert.match(app("weighing/weights/loading.tsx"), /<WeightsKidsPanelSkeleton \/>/);
  assert.match(app("weighing/analytics/loading.tsx"), /<WeightsGeneralPanelSkeleton \/>/);
});

test("herd-signals twin counts KPI_DEFS and reads the filter bar layout", () => {
  const twin = read("herd-signals/herd-signals-skeletons.tsx");
  assert.match(twin, /count=\{KPI_DEFS\.length\}/);
  assert.match(twin, /fields=\{HERD_SIGNALS_BAR_FIELDS\}/);
  assert.doesNotMatch(twin, /fields=\{\[\d/, "no hard-coded field widths");
  assert.match(read("herd-signals/herd-signals-kpis.tsx"), /import \{ KPI_DEFS \} from "\.\/herd-signals-kpi-defs"/);
  const filters = read("herd-signals/herd-signals-filters.tsx");
  assert.match(filters, /sx=\{HERD_SIGNALS_SEARCH_SX\}/);
  assert.match(filters, /sm: HERD_SIGNALS_SELECT_MIN/);
  assert.match(app("herd-signals/loading.tsx"), /<HerdSignalsLivePanelSkeleton \/>/);
});

test("/vaccination twin reads command-board-layout.ts, as the Command Board does", () => {
  const twin = read("preventive-care-vaccination/vaccination-skeletons.tsx");
  assert.match(twin, /from "\.\/command-board-layout"/);
  assert.doesNotMatch(twin, LITERAL_SIZE);
  assert.match(twin, /count=\{CB_KPI_KEYS\.length\} size=\{CB_KPI_SIZE\}/);
  // TR2-P2-6: the status multi-select joined the filter row.
  assert.match(twin, /fields=\{\[CB_VACCINE_FIELD_MIN, CB_DRIVE_FIELD_MIN, CB_STATUS_FIELD_MIN\]\}/);
  const view = read("preventive-care-vaccination/command-board-view.tsx");
  assert.match(view, /size=\{CB_KPI_SIZE\}/);
  assert.match(view, /sm: CB_VACCINE_FIELD_MIN/);
  assert.match(view, /sm: CB_DRIVE_FIELD_MIN/);
  assert.match(view, /sm: CB_STATUS_FIELD_MIN/);
  // The deck renders exactly the layout's KPI keys, in order.
  const layout = read("preventive-care-vaccination/command-board-layout.ts");
  const keys = [...layout.match(/CB_KPI_KEYS = \[([\s\S]*?)\]/)[1].matchAll(/"([a-z_]+)"/g)].map((m) => m[1]);
  const tiles = [...view.matchAll(/kpiTile\("command_board\.kpi\.([a-z_]+)"/g)].map((m) => m[1]);
  assert.deepEqual(tiles, keys);
});

test("/vaccination/plan loading reads plan-layout.ts, as PlanConsole does", () => {
  const loading = app("vaccination/plan/(index)/loading.tsx");
  assert.match(loading, /from "@\/features\/vaccination-plan\/plan-layout"/);
  assert.match(loading, /count=\{PLAN_FACT_KEYS\.length\} size=\{PLAN_FACT_SIZE\}/);
  assert.match(loading, /columns=\{LIVE_HEAD_CELLS\.length\}/);
  const page = read("vaccination-plan/plan-console.tsx");
  assert.match(page, /size=\{PLAN_FACT_SIZE\}/);
  assert.match(page, /headCells=\{LIVE_HEAD_CELLS\}/);
  assert.match(page, /headCells=\{EARLIER_HEAD_CELLS\}/);
  const layout = read("vaccination-plan/plan-layout.ts");
  const keys = [...layout.match(/PLAN_FACT_KEYS = \[([^\]]*)\]/)[1].matchAll(/"([a-z]+)"/g)].map((m) => m[1]);
  const facts = [...page.matchAll(/key: "([a-z]+)"/g)].map((m) => m[1]).filter((k) => keys.includes(k));
  assert.deepEqual(facts, keys);
});

// guard: plan-header-action-width (FIXJ6). "Open V<n>" (draft waiting, 105px) and "Start a new version"
// (176px) share one header slot width, so the twin's single action placeholder matches either state
// at 390 (skeleton IoU 0.6 before); the draft banner twin is the ActionAlert's measured phone height.
test("/vaccination/plan header action is one width in both states, read by page and twin", () => {
  const layout = read("vaccination-plan/plan-layout.ts");
  assert.match(layout, /export const PLAN_HEADER_ACTION_WIDTH = 176;/);
  assert.match(layout, /headerActionWidths: \[PLAN_HEADER_ACTION_WIDTH\]/);
  assert.match(layout, /draftBannerHeight: \{ xs: 98, md: 50 \}/);
  const page = read("vaccination-plan/plan-console.tsx");
  assert.equal((page.match(/sx=\{\{ minWidth: PLAN_HEADER_ACTION_WIDTH \}\}/g) ?? []).length, 2, "both header actions carry the slot width");
});

test("/counts/milk-preparation twin reads milk-preparation-layout.ts, as the page does", () => {
  const twin = read("counts/milk-preparation-skeletons.tsx");
  assert.match(twin, /from "\.\/milk-preparation-layout"/);
  const page = read("counts/milk-preparation.tsx");
  assert.match(page, /\{MILK_KPIS\.map\(\(kpi\) =>/, "the page renders its KPI cards from MILK_KPIS");
  assert.match(page, /caption=\{kpi\.unit \?/, "the unit caption follows MILK_KPIS[].unit");
  assert.match(page, /MILK_FARM_STATE_KEYS\.map\(/, "the farm-state strip renders MILK_FARM_STATE_KEYS");
  assert.match(page, /size=\{MILK_KPI_SIZE\}/);
  assert.match(page, /fallback=\{<MilkPreparationPanelSkeleton \/>\}/);
  assert.match(app("counts/milk-preparation/loading.tsx"), /<MilkPreparationPanelSkeleton \/>/);
});

test("/counts twins read counts-layout.ts, as the pages do", () => {
  assert.match(read("counts/counts-skeletons.tsx"), /from "\.\/counts-layout"/);
  const ha = read("counts/herd-analytics.tsx");
  for (const k of ["size={HA_GRID.flow}", "size={HA_GRID.sex}", "size={HA_GRID.mix}", "fallback={<HerdAnalyticsPanelSkeleton />}"]) assert.ok(ha.includes(k), `herd-analytics: ${k}`);
  const bd = read("counts/counts-breakdown.tsx");
  for (const k of ["size={BD_GRID.breed}", "size={BD_GRID.stageSex}", "size={BD_GRID.pens}", "fallback={<BreakdownKpiSkeleton />}", "fallback={<BreakdownChartsSkeleton />}"]) assert.ok(bd.includes(k), `counts-breakdown: ${k}`);
  const hr = read("counts/herd-register.tsx");
  for (const k of ["size={HERD_KPI_SIZE}", "fallback={<HerdKpiSkeleton />}"]) assert.ok(hr.includes(k), `herd-register: ${k}`);
  const mo = read("counts/mortality.tsx");
  for (const k of ["MORTALITY_RATE_SIZE", "fallback={<MortalityPanelSkeleton />}"]) assert.ok(mo.includes(k), `mortality: ${k}`);
  assert.match(app("counts/analytics/loading.tsx"), /<HerdAnalyticsPanelSkeleton \/>/);
  assert.match(app("counts/mortality/loading.tsx"), /<MortalityPanelSkeleton \/>/);
  assert.match(app("counts/breakdown/loading.tsx"), /<BreakdownKpiSkeleton \/>[\s\S]*<BreakdownChartsSkeleton \/>/);
  assert.match(app("counts/herd/loading.tsx"), /<HerdKpiSkeleton \/>/);
});

test("filter controls read the shared width floors their twins use", () => {
  const wf = readFileSync(new URL("../components/worklist-filters.tsx", import.meta.url), "utf8");
  assert.match(wf, /sm: FILTER_SELECT_MIN/);
  assert.match(wf, /sm: FILTER_DATE_MIN/);
  assert.doesNotMatch(wf, /minWidth: \{ xs: 0, sm: (160|180) \}/);
  assert.match(readFileSync(new URL("../components/date-range-picker.tsx", import.meta.url), "utf8"), /sm: DATE_RANGE_PICKER_MIN/);
});

test("/vaccination/live-tracker twin reads live-tracker-layout.ts, as the board does", () => {
  const twin = read("vaccination-live-tracker/live-tracker-skeleton.tsx");
  assert.match(twin, /from "\.\/live-tracker-layout"/);
  assert.doesNotMatch(twin, LITERAL_SIZE);
  const board = read("vaccination-live-tracker/live-tracker-board.tsx");
  for (const k of ["mb: LT_HEADER_MB", "mb: LT_BLOCK_MB", "size={LT_MAIN_SIZE}", "size={LT_RAIL_SIZE}", "fallback={<LiveTrackerBodySkeleton />}"]) assert.ok(board.includes(k), `board: ${k}`);
  assert.match(read("vaccination-live-tracker/live-tracker-filters.tsx"), /minmax\(\$\{LT_FILTER_MIN\}px, 1fr\)/);
  const layout = read("vaccination-live-tracker/live-tracker-layout.ts");
  const ids = [...layout.match(/LT_FILTER_IDS = \[([^\]]*)\]/)[1].matchAll(/"([a-z_]+)"/g)].map((m) => m[1]);
  assert.deepEqual([...board.matchAll(/id: "(lt_[a-z_]+)"/g)].map((m) => m[1]), ids);
  const keys = [...layout.match(/LT_KPI_KEYS = \[([^\]]*)\]/)[1].matchAll(/"([a-z_]+)"/g)].map((m) => m[1]);
  assert.deepEqual([...read("vaccination-live-tracker/live-tracker-kpis.tsx").matchAll(/key: "([a-z_]+)"/g)].map((m) => m[1]), keys);
  assert.match(app("vaccination/live-tracker/loading.tsx"), /<LiveTrackerPageSkeleton \/>/);
});

// REVIEW-49: no number typed into a twin. Sizes, counts, widths, heights and caption lines come from the
// feature's *-layout.ts (page-read where the page sets them, a named measured estimate where it does
// not); only layout spacing (spacing / gap / sx) may be literal.
const NUMERIC_PROP_VALUE = /\b(?:columns|rows|count|width|widths|height|titleWidth|actionWidths|crumbWidths|hintLines|fields|lanes|pages|size)=\{((?:[^{}]|\{[^{}]*\})*)\}/g;
const hasNumber = (value) => /(^|[^\w.$])\d/.test(value);
const NUMERIC_PROP = { test: (line) => [...line.matchAll(NUMERIC_PROP_VALUE)].some((m) => hasNumber(m[1])) };
const NUMERIC_KEY = /\b(?:hintLines|height|width|size|length):\s*(?:\d|\{\s*xs:\s*\d)/;
export function numericLiterals(src) {
  return src.split("\n").map((line, i) => [i + 1, line]).filter(([, line]) => !/^\s*(\/\/|\*|\/\*)/.test(line) && (NUMERIC_PROP.test(line) || NUMERIC_KEY.test(line))).map(([n, line]) => `${n}: ${line.trim()}`);
}
test("self-test: numeric literal props are caught, layout constants and spacing pass", () => {
  assert.equal(numericLiterals('<TableSkeleton columns={10} rows={ROWS} />').length, 1);
  assert.equal(numericLiterals('<ChartCardSkeleton height={{ xs: 358, lg: PLOT }} />').length, 1);
  assert.equal(numericLiterals('shapes={K.map(() => ({ hint: true, hintLines: 2 }))}').length, 1);
  assert.equal(numericLiterals('<PageHeaderSkeleton actionWidths={[150, 151]} />').length, 1);
  assert.equal(numericLiterals('<TableSkeleton columns={LT_TABLES.pens.columns} rows={LT_TABLES.pens.rows} />').length, 0);
  assert.equal(numericLiterals('<StackSkeleton spacing={3}><Box sx={{ px: 3, pb: 2 }} /></StackSkeleton>').length, 0);
});
test("SK3 twins type no numbers", () => {
  const twins = [
    "weighing/weights-skeletons.tsx",
    "herd-signals/herd-signals-skeletons.tsx",
    "preventive-care-vaccination/vaccination-skeletons.tsx",
    "counts/milk-preparation-skeletons.tsx",
    "counts/counts-skeletons.tsx",
    "vaccination-live-tracker/live-tracker-skeleton.tsx",
  ].map((f) => [f, read(f)]);
  const loadings = ["vaccination/(index)", "vaccination/plan/(index)", "herd-signals", "weighing/weights", "weighing/analytics", "counts/analytics", "counts/breakdown", "counts/herd", "counts/mortality", "counts/milk-preparation", "vaccination/live-tracker"].map((r) => [`app/(admin)/${r}/loading.tsx`, app(`${r}/loading.tsx`)]);
  const hits = [...twins, ...loadings].flatMap(([f, src]) => numericLiterals(src).map((h) => `${f}:${h}`));
  assert.deepEqual(hits, []);
});
