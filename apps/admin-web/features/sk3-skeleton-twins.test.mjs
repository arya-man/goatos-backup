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
  assert.match(twin, /fields=\{\[CB_VACCINE_FIELD_MIN, CB_DRIVE_FIELD_MIN\]\}/);
  const view = read("preventive-care-vaccination/command-board-view.tsx");
  assert.match(view, /size=\{CB_KPI_SIZE\}/);
  assert.match(view, /sm: CB_VACCINE_FIELD_MIN/);
  assert.match(view, /sm: CB_DRIVE_FIELD_MIN/);
  // The deck renders exactly the layout's KPI keys, in order.
  const layout = read("preventive-care-vaccination/command-board-layout.ts");
  const keys = [...layout.match(/CB_KPI_KEYS = \[([\s\S]*?)\]/)[1].matchAll(/"([a-z_]+)"/g)].map((m) => m[1]);
  const tiles = [...view.matchAll(/kpiTile\("command_board\.kpi\.([a-z_]+)"/g)].map((m) => m[1]);
  assert.deepEqual(tiles, keys);
});

test("/vaccination/plan loading reads plan-layout.ts, as PlanConsole does", () => {
  const loading = app("vaccination/plan/loading.tsx");
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

test("/counts/milk-preparation twin reads milk-preparation-layout.ts, as the page does", () => {
  const twin = read("counts/milk-preparation-skeletons.tsx");
  assert.match(twin, /from "\.\/milk-preparation-layout"/);
  assert.doesNotMatch(twin, LITERAL_SIZE);
  const page = read("counts/milk-preparation.tsx");
  assert.match(page, /size=\{MILK_KPI_SIZE\}/);
  assert.match(page, /fallback=\{<MilkPreparationPanelSkeleton \/>\}/);
  assert.match(app("counts/milk-preparation/loading.tsx"), /<MilkPreparationPanelSkeleton \/>/);
  // The page's KPI keys and which carry a unit caption equal the layout's list.
  const layout = read("counts/milk-preparation-layout.ts");
  const want = [...layout.matchAll(/\{ key: "([a-z]+)", unit: (true|false) \}/g)].map((m) => `${m[1]}:${m[2]}`);
  const got = [...page.matchAll(/\{ key: "([a-z]+)", total: [^}]*?(, unit[^}]*)? \}/g)].map((m) => `${m[1]}:${Boolean(m[2])}`);
  assert.deepEqual(got, want);
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
