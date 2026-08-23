import { redirect } from "next/navigation";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getFeedAnalyticsDirected,
  getFeedAnalyticsExecution,
  getFeedAnalyticsExperiment,
  getFeedAnalyticsStock,
  type ApiResult,
  type FeedAnalyticsDirectedResponse,
  type FeedAnalyticsExecutionResponse,
  type FeedAnalyticsExperimentResponse,
  type FeedAnalyticsStockResponse,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDate, istDayPlus, todayIso } from "@/lib/format";
import { backendScope, parseScope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { ChartHover } from "@/components/chart-hover";
import { RangeCoverageNote } from "./range-coverage-note";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { SegmentedLinks } from "@/components/segmented-links";
import { SvgBars } from "@/components/svg-bars";
import {
  FEED_SERIES_VARS,
  FeedChartLegend,
  FeedLines,
  FeedStackedColumns,
  seriesColorVar,
  type LineSeries,
  type StackedDay,
} from "./feed-analytics-charts";
import { FeedFaroView } from "./feed-faro-view";

// Feed -> Feed Analytics. The leadership read of the feed chain over a date
// window: DIRECTED kg off the frozen sheet, ration per animal, execution
// adherence off the proof gates, and the trial arms.
//
// The one word this page lives or dies on is DIRECTED. Completions carry
// proofs, never weights, so nothing here is consumption — the contract's
// `banner.basis` line says so on every tab and every quantity label comes from
// the backend copy map already carrying that framing.
//
// Rendering rules inherited from the repo chart stack: server components only,
// inline SVG per the mock's chart anatomy, series colour follows the FEED ITEM
// across charts (never its rank on one chart), and the page derives NO business
// number of its own — every figure below is a backend field or a straight
// per-day re-grouping of backend rows for drawing. Park scope belongs to the
// top bar (Scope Chrome Rule); the page body owns only the range and tab state,
// both as URL params so a view survives reload and pastes as a link.

const PAGE_PATH = "/feed/analytics";

// Select options from served rows: first label wins per value, sorted for a stable dropdown.
function dedupeOptions(options: { value: string; label: string }[]): { value: string; label: string }[] {
  const seen = new Map<string, string>();
  for (const o of options) {
    if (o.value !== "" && !seen.has(o.value)) seen.set(o.value, o.label);
  }
  return [...seen.entries()].map(([value, label]) => ({ value, label })).sort((a, b) => a.label.localeCompare(b.label));
}
const TABS = ["overview", "items", "peranimal", "experiment", "execution"] as const;
type Tab = (typeof TABS)[number];
const RANGES = ["30", "61", "92"] as const;
type Range = (typeof RANGES)[number];

const nf = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 1 });
const money = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 0 });
const rate = (value: number) =>
  value.toLocaleString("en-IN", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const num = (raw: string) => {
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : 0;
};

function fa(pageContract: AdminUiPageContract, key: string): string {
  return copy(pageContract, key);
}

function readTab(sp: RouteSearchParams): Tab {
  const raw = one(sp, "tab");
  return (TABS as readonly string[]).includes(raw ?? "") ? (raw as Tab) : "overview";
}

function readRange(sp: RouteSearchParams): Range {
  const raw = one(sp, "range");
  return (RANGES as readonly string[]).includes(raw ?? "") ? (raw as Range) : "30";
}

/** The wastage table's own day picker; absent/malformed means the backend default (today, IST). */
function readWastageDay(sp: RouteSearchParams): string | undefined {
  const raw = one(sp, "wastage_day");
  return raw && /^\d{4}-\d{2}-\d{2}$/.test(raw) ? raw : undefined;
}

/** Preserves every other param so switching tab/range never resets scope. */
function hrefWith(sp: RouteSearchParams | undefined, next: Record<string, string | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(sp ?? {})) {
    if (key in next) continue;
    if (Array.isArray(value)) value.forEach((item) => params.append(key, item));
    else if (value) params.set(key, value);
  }
  for (const [key, value] of Object.entries(next)) {
    if (value) params.set(key, value);
  }
  const query = params.toString();
  return query ? `${PAGE_PATH}?${query}` : PAGE_PATH;
}

function rangeDates(range: Range): { date_from: string; date_to: string } {
  // Asia/Kolkata calendar arithmetic via the shared IST helpers — the same
  // business-day rule the backend applies to its own defaults. The previous
  // Date/toISOString version ran on the server's UTC clock and dropped
  // yesterday for any request between 00:00 and 05:29 IST (review, PR #64).
  const days = Number(range);
  const to = istDayPlus(todayIso(), -1);
  return { date_from: istDayPlus(to, -(days - 1)), date_to: to };
}

// ---------------------------------------------------------------------------
// Directed-view regrouping (drawing shape only — no derived business numbers)
// ---------------------------------------------------------------------------

type DirectedView = {
  dayLabels: string[];
  /** Top feed items by window kg, in stable order; the rest fold into one slot. */
  itemLabels: string[];
  stacked: StackedDay[];
  mix: { key: string; label: string; value: number }[];
  itemSeries: LineSeries[];
  perHead: LineSeries[];
  latestDay?: FeedAnalyticsDirectedResponse["days"][number];
};

function buildDirectedView(data: FeedAnalyticsDirectedResponse, otherLabel: string): DirectedView {
  const dayKeys = data.days.map((d) => d.feed_day);
  const totalsByItem = new Map<string, { label: string; total: number }>();
  for (const item of data.items) {
    const existing = totalsByItem.get(item.feed_item_key);
    const total = (existing?.total ?? 0) + num(item.directed_kg);
    totalsByItem.set(item.feed_item_key, { label: item.feed_item_label, total });
  }
  const ranked = [...totalsByItem.entries()].sort((a, b) => b[1].total - a[1].total);
  // Every feed item gets its own slice (maintainer request 2026-08-21) — the
  // window's item count is bounded by the catalog, and folding the tail into
  // one "Other feeds" slot hid real feeds from the chart and legend. Ranked
  // order keeps each item's colour stable across the page's charts.
  const top = ranked;
  const topKeys = new Set(top.map(([key]) => key));
  const hasOther = ranked.length > top.length;
  const itemLabels = [...top.map(([, v]) => v.label), ...(hasOther ? [otherLabel] : [])];
  const rowByDayItem = new Map<string, FeedAnalyticsDirectedResponse["items"][number]>();
  for (const item of data.items) {
    rowByDayItem.set(`${item.feed_day}\u0000${item.feed_item_key}`, item);
  }
  const rowFor = (day: string, itemKey: string) => rowByDayItem.get(`${day}\u0000${itemKey}`);

  const byDayItem = new Map<string, number[]>();
  for (const day of dayKeys) byDayItem.set(day, new Array(itemLabels.length).fill(0));
  for (const item of data.items) {
    const slots = byDayItem.get(item.feed_day);
    if (!slots) continue;
    const idx = topKeys.has(item.feed_item_key)
      ? top.findIndex(([key]) => key === item.feed_item_key)
      : hasOther
        ? itemLabels.length - 1
        : -1;
    if (idx >= 0) slots[idx] += num(item.directed_kg);
  }

  // One small-multiple series per feed item, in ranked order so each item's
  // colour matches its slot on the stacked chart and legend.
  const itemSeries: { label: string; colorVar: string; points: (number | null)[] }[] = ranked.map(
    ([key, v], s) => ({
      label: v.label,
      colorVar: seriesColorVar(s),
      points: dayKeys.map((day) => {
        const row = rowFor(day, key);
        return row ? num(row.directed_kg) : null;
      }),
    }),
  );

  const perHeadSeries: LineSeries[] = ranked.map(([key, v], s) => ({
    label: v.label,
    colorVar: seriesColorVar(s),
    points: dayKeys.map((day) => {
      const row = rowFor(day, key);
      return row && row.per_head_grams !== "" ? num(row.per_head_grams) : null;
    }),
  }));

  return {
    dayLabels: dayKeys,
    itemLabels,
    stacked: dayKeys.map((day) => ({
      key: day,
      label: day,
      segments: byDayItem.get(day) ?? [],
    })),
    mix: ranked.map(([key, v]) => ({ key, label: v.label, value: Math.round(v.total) })),
    itemSeries,
    perHead: perHeadSeries,
    latestDay: data.days.length > 0 ? data.days[data.days.length - 1] : undefined,
  };
}

// ---------------------------------------------------------------------------

export async function FeedAnalyticsPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const tab = readTab(searchParams);
  const range = readRange(searchParams);
  const { parkId } = backendScope(parseScope(searchParams));
  const window = rangeDates(range);
  const params = { park_id: parkId, ...window };

  // Overview needs directed + execution (for the adherence KPI); every other
  // tab reads exactly its own endpoint.
  const wantDirected = tab === "overview" || tab === "items" || tab === "peranimal";
  const wantExecution = tab === "overview" || tab === "execution";
  const wantExperiment = tab === "experiment";
  // The experiment tab carries its OWN park dropdown (fa_park), the same
  // disable-when-top-bar-owns rule every feed page uses; other tabs stay on
  // top-bar scope alone.
  const faPark = one(searchParams, "fa_park") ?? "";
  const experimentParkId = parkId || faPark;
  // The packing-mismatch table carries its own narrowing: farm and feed-item selects over the
  // served rows, plus a calendar day (fav_day) that re-reads the execution endpoint pinned to that
  // single business day, so a reader can step back past the page's rolling window.
  const favDay = one(searchParams, "fav_day") ?? "";
  const favPark = one(searchParams, "fav_park") ?? "";
  const favItem = one(searchParams, "fav_item") ?? "";
  const locations = wantExperiment ? await getCensusLocations() : { parks: [] as { id: string; name: string }[], sheds: [] };
  const wantStock = tab === "overview" || tab === "items";
  const [directed, execution, experiment, stock] = await Promise.all([
    wantDirected
      ? getFeedAnalyticsDirected(params)
      : Promise.resolve<ApiResult<FeedAnalyticsDirectedResponse> | null>(null),
    wantExecution
      ? getFeedAnalyticsExecution(params)
      : Promise.resolve<ApiResult<FeedAnalyticsExecutionResponse> | null>(null),
    wantExperiment
      ? getFeedAnalyticsExperiment({ ...params, park_id: experimentParkId, wastage_day: readWastageDay(searchParams) })
      : Promise.resolve<ApiResult<FeedAnalyticsExperimentResponse> | null>(null),
    wantStock
      ? getFeedAnalyticsStock(params)
      : Promise.resolve<ApiResult<FeedAnalyticsStockResponse> | null>(null),
  ]);
  // Second, day-pinned execution read for the mismatch table's calendar. Only its
  // packing_variance is used; the tab's charts keep the page's rolling window.
  const executionDay =
    tab === "execution" && favDay !== ""
      ? await getFeedAnalyticsExecution({ park_id: parkId, date_from: favDay, date_to: favDay })
      : null;
  const nonNull = [directed, execution, experiment, stock].filter((r) => r !== null);
  if (firstAuthRequiredError(...nonNull)) redirect(INTERNAL_LOGIN_PATH);

  // Stock is deliberately absent from the failure gate: the rest of the page
  // must stay useful when the purchase ledger is not bootstrapped yet.
  const failed = [directed, execution, experiment].some((r) => r !== null && !r.ok);

  return (
    <div className="pagegrid">
      <FeedFaroView routeId={pageContract.route_id} parkId={parkId} />

      <p className="muted small" style={{ margin: "0 0 4px" }}>
        {fa(pageContract, "banner.basis")}
      </p>

      <div style={{ display: "flex", flexWrap: "wrap", gap: 10, alignItems: "center", justifyContent: "space-between" }}>
        <SegmentedLinks
          current={tab}
          options={TABS.map((t) => ({
            value: t,
            label: fa(pageContract, `tab.${t}`),
            href: hrefWith(searchParams, { tab: t === "overview" ? undefined : t }),
          }))}
        />
        <SegmentedLinks
          current={range}
          ariaLabel={fa(pageContract, "range.aria")}
          options={RANGES.map((r) => ({
            value: r,
            label: fa(pageContract, `range.${r}`),
            href: hrefWith(searchParams, { range: r === "30" ? undefined : r }),
          }))}
        />
      </div>

      {failed ? (
        <section className="card">
          <h2 className="h">{fa(pageContract, "error.title")}</h2>
          <p className="muted small">{fa(pageContract, "error.body")}</p>
        </section>
      ) : null}

      {directed?.ok && (tab === "overview" || tab === "items" || tab === "peranimal") ? (
        <DirectedTabs
          tab={tab}
          range={range}
          data={directed.data}
          execution={execution?.ok ? execution.data : null}
          stock={stock?.ok ? stock.data : null}
          pageContract={pageContract}
        />
      ) : null}

      {tab === "execution" && execution?.ok ? (
        <ExecutionTab
          data={execution.data}
          pageContract={pageContract}
          variance={{
            rows: (executionDay?.ok ? executionDay.data : execution.data).packing_variance,
            day: favDay,
            park: favPark,
            item: favItem,
          }}
        />
      ) : null}

      {tab === "experiment" && experiment?.ok ? (
        <ExperimentTab
          data={experiment.data}
          pageContract={pageContract}
          parkField={{
            kind: "select",
            param: "fa_park",
            label: fa(pageContract, "filter.park_label"),
            value: faPark,
            allowAll: true,
            disabledReason: parkId ? fa(pageContract, "filter.scope_readonly") : undefined,
            options: locations.parks.map((park) => ({ value: park.id, label: park.name })),
          }}
        />
      ) : null}
    </div>
  );
}

function DirectedTabs({
  tab,
  range,
  data,
  execution,
  stock,
  pageContract,
}: {
  tab: Tab;
  range: Range;
  data: FeedAnalyticsDirectedResponse;
  execution: FeedAnalyticsExecutionResponse | null;
  stock: FeedAnalyticsStockResponse | null;
  pageContract: AdminUiPageContract;
}) {
  const view = buildDirectedView(data, fa(pageContract, "series.other"));
  const empty = data.days.length === 0;
  const noData = fa(pageContract, "empty.title");

  // Transient coverage note (maintainer request 2026-08-21): a wider window
  // than the sheets cover shows "data covers only N days" for two seconds.
  // 2-month range notes at ≤30 covered days, 3-month at ≤60; the 30-day range
  // never notes. Covered days = feed days with an issued sheet in the window.
  const coveredDays = data.days.length;
  const coverageFloor = range === "61" ? 30 : range === "92" ? 60 : 0;
  const coverageNote =
    coveredDays > 0 && coverageFloor > 0 && coveredDays <= coverageFloor
      ? fa(pageContract, "range.coverage_note").replace("{days}", String(coveredDays))
      : null;

  if (empty) {
    return (
      <section className="card">
        <h2 className="h">{noData}</h2>
        <p className="muted small">{fa(pageContract, "empty.body")}</p>
      </section>
    );
  }

  const latest = view.latestDay;
  // Verified share of the window's packing+distribution completions — backend
  // counts, summed for display only (a share of two backend counts, not a new
  // business number).
  let adherence: string | null = null;
  if (execution) {
    let verified = 0;
    let all = 0;
    for (const day of execution.days) {
      verified += day.packing_verified + day.distribution_verified;
      all +=
        day.packing_verified + day.packing_awaiting + day.packing_rework +
        day.distribution_verified + day.distribution_awaiting + day.distribution_rework;
    }
    adherence = all > 0 ? `${Math.round((verified / all) * 100)}%` : null;
  }

  return (
    <>
      {coverageNote !== null ? (
        <RangeCoverageNote key={`${range}-${coveredDays}`} message={coverageNote} />
      ) : null}
      {tab === "overview" ? (
        <section className="grid g4 kpi-row" aria-label={fa(pageContract, "chart.daily.title")}>
          <div className="kpi card">
            <div className="val">{latest ? `${nf(num(latest.directed_kg))} ${fa(pageContract, "unit.kg")}` : "—"}</div>
            <div className="dl">{fa(pageContract, "kpi.directed.label")}</div>
            <div className="muted small">{fa(pageContract, "kpi.directed.sub")}</div>
          </div>
          <div className="kpi card">
            <div className="val">{latest ? nf(latest.head_days) : "—"}</div>
            <div className="dl">{fa(pageContract, "kpi.head_days.label")}</div>
            <div className="muted small">{fa(pageContract, "kpi.head_days.sub")}</div>
          </div>
          <div className="kpi card">
            <div className="val">
              {latest && latest.per_head_grams !== "" ? `${nf(num(latest.per_head_grams))} g` : "—"}
            </div>
            <div className="dl">{fa(pageContract, "kpi.per_head.label")}</div>
            <div className="muted small">{fa(pageContract, "kpi.per_head.sub")}</div>
          </div>
          <div className="kpi card">
            <div className="val">{adherence ?? "—"}</div>
            <div className="dl">{fa(pageContract, "kpi.adherence.label")}</div>
            <div className="muted small">{fa(pageContract, "kpi.adherence.sub")}</div>
          </div>
        </section>
      ) : null}

      {tab === "items" ? <StockCards stock={stock} pageContract={pageContract} /> : null}

      {tab === "items" ? (
        // The artifact's Feed Items tab: one small chart per feed item, each in
        // its ranked colour, over the same window, below the stock cards.
        // Two charts per row (single column on narrow), sized up from the
        // .charts 3-up column flow so each item's day-to-day movement is
        // readable, with breathing room under the tab bar.
        <div
          className="grid"
          style={{ gridTemplateColumns: "repeat(auto-fit, minmax(420px, 1fr))", gap: 14, marginTop: 14 }}
        >
          {view.itemSeries.map((series) => (
            <div className="chartcard" key={series.label}>
              <h4>{series.label}</h4>
              <div className="cap">{fa(pageContract, "chart.item.hint")}</div>
              <ChartHover>
                <FeedLines
                  series={[series]}
                  dayLabels={view.dayLabels}
                  valueNoun={fa(pageContract, "unit.kg")}
                  chartLabel={series.label}
                  emptyLabel={fa(pageContract, "empty.body")}
                />
              </ChartHover>
            </div>
          ))}
        </div>
      ) : null}

      {tab === "overview" ? (
        <section className="card wchart" aria-label={fa(pageContract, "chart.daily.title")}>
          <h2 className="h">{fa(pageContract, "chart.daily.title")}</h2>
          <p className="muted small">{fa(pageContract, "chart.daily.hint")}</p>
          <ChartHover>
            <FeedStackedColumns
              days={view.stacked}
              seriesLabels={view.itemLabels}
              valueNoun={fa(pageContract, "unit.kg")}
              chartLabel={fa(pageContract, "chart.daily.title")}
              emptyLabel={fa(pageContract, "empty.body")}
            />
          </ChartHover>
          <FeedChartLegend
            entries={view.itemLabels.map((label, s) => ({
              label,
              colorVar: seriesColorVar(s),
            }))}
          />
        </section>
      ) : null}

      {tab === "overview" && stock && stock.expenditure.length > 0 ? (
        <section
          className="grid g4 kpi-row"
          style={{ marginTop: 14, gap: 14 }}
          aria-label={fa(pageContract, "chart.spend.title")}
        >
          {([
            ["week", stock.spend.this_week],
            ["month", stock.spend.this_month],
            ["quarter", stock.spend.three_months],
            ["year", stock.spend.this_year],
          ] as const).map(([period, rupees]) => (
            <div className="kpi card" key={period}>
              <div className="val">{`₹${nf(num(rupees))}`}</div>
              <div className="dl">{fa(pageContract, `spend.${period}.label`)}</div>
              <div className="muted small">{fa(pageContract, `spend.${period}.sub`)}</div>
            </div>
          ))}
        </section>
      ) : null}

      {tab === "overview" && stock && stock.expenditure.length > 0 ? (
        <section className="card wchart" aria-label={fa(pageContract, "chart.spend.title")}>
          <h2 className="h">{fa(pageContract, "chart.spend.title")}</h2>
          <p className="muted small">{fa(pageContract, "chart.spend.hint")}</p>
          <ChartHover>
            <FeedLines
              series={[{
                label: fa(pageContract, "chart.spend.title"),
                colorVar: FEED_SERIES_VARS[2],
                points: stock.expenditure.map((d) => num(d.rupees)),
              }]}
              dayLabels={stock.expenditure.map((d) => d.feed_day)}
              valueNoun={fa(pageContract, "unit.rupees")}
              chartLabel={fa(pageContract, "chart.spend.title")}
              emptyLabel={fa(pageContract, "stock.empty")}
            />
          </ChartHover>
        </section>
      ) : null}

      {tab === "overview" ? (
        <section className="card wchart" aria-label={fa(pageContract, "chart.mix.title")}>
          <h2 className="h">{fa(pageContract, "chart.mix.title")}</h2>
          <p className="muted small">{fa(pageContract, "chart.mix.hint")}</p>
          <ChartHover>
            <SvgBars
              data={view.mix}
              valueNoun={fa(pageContract, "unit.kg")}
              chartLabel={fa(pageContract, "chart.mix.title")}
              emptyLabel={fa(pageContract, "empty.body")}
            />
          </ChartHover>
        </section>
      ) : null}

      {tab === "peranimal" ? (
        // One RATION CARD per feed item: the current figure a director actually
        // asks for ("how many grams is each animal getting?") big, with the
        // item's own trend on its own scale — a shared-scale multi-line let the
        // 950 g Masoor line flatten every concentrate into the baseline.
        <div
          className="grid"
          style={{ gridTemplateColumns: "repeat(auto-fit, minmax(420px, 1fr))", gap: 14, marginTop: 14 }}
        >
          {view.perHead.map((series) => {
            const lastIdx = series.points.reduce<number>((acc, point, index) => (point === null ? acc : index), -1);
            const latest = lastIdx >= 0 ? (series.points[lastIdx] as number) : null;
            return (
              <div className="chartcard" key={series.label}>
                <h4>{series.label}</h4>
                <div className="cap">{fa(pageContract, "chart.perhead.hint")}</div>
                <div className="val" style={{ fontSize: 26, fontWeight: 700, margin: "2px 0 6px" }}>
                  {latest === null ? "—" : `${nf(latest)} ${fa(pageContract, "unit.g_per_head")}`}
                </div>
                <ChartHover>
                  <FeedLines
                    series={[series]}
                    dayLabels={view.dayLabels}
                    valueNoun={fa(pageContract, "unit.g_per_head")}
                    chartLabel={series.label}
                    emptyLabel={fa(pageContract, "empty.body")}
                  />
                </ChartHover>
              </div>
            );
          })}
        </div>
      ) : null}
    </>
  );
}

function ExecutionTab({
  data,
  pageContract,
  variance,
}: {
  data: FeedAnalyticsExecutionResponse;
  pageContract: AdminUiPageContract;
  /** The mismatch table's own narrowing: rows (day-pinned when `day` is set) + applied filters. */
  variance: {
    rows: FeedAnalyticsExecutionResponse["packing_variance"];
    day: string;
    park: string;
    item: string;
  };
}) {
  if (data.days.length === 0) {
    return (
      <section className="card">
        <h2 className="h">{fa(pageContract, "empty.title")}</h2>
        <p className="muted small">{fa(pageContract, "empty.execution.body")}</p>
      </section>
    );
  }
  // Farm/item narrowing applies over the served rows; the calendar already narrowed the fetch.
  const varianceRows = variance.rows.filter(
    (r) =>
      (variance.park === "" || r.park_label === variance.park) &&
      (variance.item === "" || r.feed_item_key === variance.item),
  );
  const consumptionRows = data.consumption_rows.filter(
    (r) =>
      (variance.park === "" || r.park_label === variance.park) &&
      (variance.item === "" || r.feed_item_key === variance.item),
  );
  // Window totals for the KPI row: shares of backend counts, no new business math.
  let packingDone = 0, packingAll = 0, distDone = 0, distAll = 0, transDone = 0, transAll = 0;
  let latestLatency: number | null = null;
  for (const d of data.days) {
    packingDone += d.packing_verified;
    packingAll += d.packing_verified + d.packing_awaiting + d.packing_rework;
    distDone += d.distribution_verified;
    distAll += d.distribution_verified + d.distribution_awaiting + d.distribution_rework;
    transDone += d.transport_completed;
    transAll += d.transport_completed + d.transport_open + d.transport_awaiting_verdict + d.transport_rework;
    if (d.median_verify_latency_minutes !== null && d.median_verify_latency_minutes !== undefined) {
      latestLatency = d.median_verify_latency_minutes;
    }
  }
  const pct = (done: number, all: number) => (all > 0 ? `${Math.round((done / all) * 100)}%` : "—");

  const statuses = [
    { label: fa(pageContract, "legend.verified"), colorVar: FEED_SERIES_VARS[0] },
    { label: fa(pageContract, "legend.awaiting"), colorVar: FEED_SERIES_VARS[2] },
    { label: fa(pageContract, "legend.rework"), colorVar: FEED_SERIES_VARS[5] },
  ];
  const stacked: StackedDay[] = data.days.map((d) => ({
    key: d.date,
    label: d.date,
    segments: [
      d.packing_verified + d.distribution_verified,
      d.packing_awaiting + d.distribution_awaiting,
      d.packing_rework + d.distribution_rework,
    ],
  }));
  // Series colours must match the legend order above — verified is the brand
  // slot, awaiting the amber slot, rework the danger slot.
  const latency: LineSeries[] = [
    {
      label: fa(pageContract, "chart.latency.title"),
      colorVar: FEED_SERIES_VARS[1],
      points: data.days.map((d) => d.median_verify_latency_minutes ?? null),
    },
  ];
  const consumptionDayLabels = data.consumption_trend.map((d) => d.feed_day);
  const consumptionSeries: LineSeries[] = [
    {
      label: fa(pageContract, "col.consumption.target"),
      colorVar: FEED_SERIES_VARS[0],
      points: data.consumption_trend.map((d) => num(d.target_kg)),
    },
    {
      label: fa(pageContract, "col.consumption.actual"),
      colorVar: FEED_SERIES_VARS[4],
      points: data.consumption_trend.map((d) => num(d.actual_kg)),
    },
  ];
  return (
    <>
      <section className="grid g4 kpi-row" aria-label={fa(pageContract, "chart.execution.title")}>
        <div className="kpi card">
          <div className="val">{pct(packingDone, packingAll)}</div>
          <div className="dl">{fa(pageContract, "kpi.packing.label")}</div>
          <div className="muted small">{`${nf(packingDone)} / ${nf(packingAll)} · ${fa(pageContract, "kpi.packing.sub")}`}</div>
        </div>
        <div className="kpi card">
          <div className="val">{pct(distDone, distAll)}</div>
          <div className="dl">{fa(pageContract, "kpi.distribution.label")}</div>
          <div className="muted small">{`${nf(distDone)} / ${nf(distAll)} · ${fa(pageContract, "kpi.distribution.sub")}`}</div>
        </div>
        <div className="kpi card">
          <div className="val">{pct(transDone, transAll)}</div>
          <div className="dl">{fa(pageContract, "kpi.transport.label")}</div>
          <div className="muted small">{`${nf(transDone)} / ${nf(transAll)} · ${fa(pageContract, "kpi.transport.sub")}`}</div>
        </div>
        <div className="kpi card">
          <div className="val">{latestLatency === null ? "—" : `${nf(latestLatency)} ${fa(pageContract, "unit.minutes")}`}</div>
          <div className="dl">{fa(pageContract, "kpi.latency.label")}</div>
          <div className="muted small">{fa(pageContract, "kpi.latency.sub")}</div>
        </div>
      </section>
      <section className="card wchart" aria-label={fa(pageContract, "chart.execution.title")}>
        <h2 className="h">{fa(pageContract, "chart.execution.title")}</h2>
        <p className="muted small">{fa(pageContract, "chart.execution.hint")}</p>
        <ExecutionStacked stacked={stacked} statuses={statuses} pageContract={pageContract} />
      </section>
      <section className="card wchart" aria-label={fa(pageContract, "chart.latency.title")}>
        <h2 className="h">{fa(pageContract, "chart.latency.title")}</h2>
        <p className="muted small">{fa(pageContract, "chart.latency.hint")}</p>
        <ChartHover>
          <FeedLines
            series={latency}
            dayLabels={data.days.map((d) => d.date)}
            valueNoun={fa(pageContract, "unit.minutes")}
            chartLabel={fa(pageContract, "chart.latency.title")}
            emptyLabel={fa(pageContract, "empty.execution.body")}
          />
        </ChartHover>
      </section>
      <section className="card wchart" aria-label={fa(pageContract, "consumption.trend.title")}>
        <h2 className="h">{fa(pageContract, "consumption.trend.title")}</h2>
        <p className="muted small">{fa(pageContract, "consumption.trend.hint")}</p>
        <ChartHover>
          <FeedLines
            series={consumptionSeries}
            dayLabels={consumptionDayLabels}
            valueNoun={fa(pageContract, "unit.kg")}
            chartLabel={fa(pageContract, "consumption.trend.title")}
            emptyLabel={fa(pageContract, "consumption.empty")}
          />
        </ChartHover>
        <FeedChartLegend entries={consumptionSeries.map((s) => ({ label: s.label, colorVar: s.colorVar }))} />
      </section>
      <section className="card" aria-label={fa(pageContract, "consumption.title")}>
        <div className="hd">
          <h3>{fa(pageContract, "consumption.title")}</h3>
          <span className="small muted">{fa(pageContract, "consumption.hint")}</span>
        </div>
        {consumptionRows.length === 0 ? (
          <p className="muted small">{fa(pageContract, "consumption.empty")}</p>
        ) : (
          <div className="tablewrap" tabIndex={0} role="group" aria-label={fa(pageContract, "consumption.title")}>
            <table className="tbl">
              <thead>
                <tr>
                  <th>{fa(pageContract, "col.consumption.day")}</th>
                  <th>{fa(pageContract, "col.consumption.park")}</th>
                  <th>{fa(pageContract, "col.consumption.shed")}</th>
                  <th>{fa(pageContract, "col.consumption.breed")}</th>
                  <th>{fa(pageContract, "col.consumption.age_group")}</th>
                  <th>{fa(pageContract, "col.consumption.item")}</th>
                  <th>{fa(pageContract, "col.consumption.target")}</th>
                  <th>{fa(pageContract, "col.consumption.actual")}</th>
                  <th>{fa(pageContract, "col.consumption.variance")}</th>
                </tr>
              </thead>
              <tbody>
                {consumptionRows.map((row, rowIndex) => (
                  <tr
                    key={`${row.feed_day}:${row.shed_id}:${row.partition_label ?? ""}:${row.feed_item_key}:${rowIndex}`}
                    className={row.has_variance ? "feed-variance-row" : undefined}
                  >
                    <td>{fmtDate(row.feed_day)}</td>
                    <td>{row.park_label}</td>
                    <td>{row.operational_location_display}</td>
                    <td>{row.breed_label}</td>
                    <td>{row.age_group}</td>
                    <td>{row.feed_item_label}</td>
                    <td>{`${row.target_kg} ${fa(pageContract, "unit.kg")}`}</td>
                    <td>{`${row.actual_kg} ${fa(pageContract, "unit.kg")}`}</td>
                    <td>
                      <span className={`${row.has_variance ? "tag t-dng" : "tag t-ok"} feed-stock-check-tag`}>
                        <span>{`${nf(Math.abs(num(row.variance_kg)))} ${fa(pageContract, "unit.kg")}`}</span>
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      {/* Intended-vs-entered packing mismatches (maintainer decision 2026-08-21). The verifier
          enters her per-item readings BLIND -- this comparison exists only on this leadership
          page, never on any verifier surface. Rows pop past the 0.2 kg tolerance. The bar narrows
          by farm and feed item over the served rows, and the calendar re-reads the endpoint pinned
          to one business day so older values than the page window stay reachable. */}
      <section className="card" aria-label={fa(pageContract, "variance.title")}>
        <div className="hd">
          <h3>{fa(pageContract, "variance.title")}</h3>
          <span className="small muted">{fa(pageContract, "variance.hint")}</span>
        </div>
        <FeedFilters
          basePath={PAGE_PATH}
          pageParam="fa_offset"
          fields={[
            {
              kind: "select",
              param: "fav_park",
              label: fa(pageContract, "col.variance.park"),
              value: variance.park,
              allowAll: true,
              options: dedupeOptions(variance.rows.map((r) => ({ value: r.park_label, label: r.park_label }))),
            },
            {
              kind: "select",
              param: "fav_item",
              label: fa(pageContract, "col.variance.item"),
              value: variance.item,
              allowAll: true,
              options: dedupeOptions(variance.rows.map((r) => ({ value: r.feed_item_key, label: r.feed_item_label }))),
            },
            {
              kind: "date",
              param: "fav_day",
              label: fa(pageContract, "col.variance.day"),
              value: variance.day,
              today: todayIso(),
              labels: {
                field: fa(pageContract, "col.variance.day"),
                today: fa(pageContract, "filter.date.today"),
                single: fa(pageContract, "filter.date.single"),
                range: fa(pageContract, "filter.date.range"),
                aria: fa(pageContract, "filter.date.aria"),
                previousMonth: fa(pageContract, "filter.date.previous_month"),
                nextMonth: fa(pageContract, "filter.date.next_month"),
                rangeStartHint: fa(pageContract, "filter.date.range_start_hint"),
                rangeEndHint: fa(pageContract, "filter.date.range_end_hint"),
                rangeSeparator: fa(pageContract, "filter.date.range_separator"),
              },
            },
          ]}
          pageContract={pageContract}
        />
        {varianceRows.length === 0 ? (
          <p className="muted small">{fa(pageContract, "variance.empty")}</p>
        ) : (
          <div className="tablewrap" tabIndex={0} role="group" aria-label={fa(pageContract, "variance.title")}>
            <table className="tbl">
              <thead>
                <tr>
                  <th>{fa(pageContract, "col.variance.day")}</th>
                  <th>{fa(pageContract, "col.variance.park")}</th>
                  <th>{fa(pageContract, "col.variance.pen")}</th>
                  <th>{fa(pageContract, "col.variance.session")}</th>
                  <th>{fa(pageContract, "col.variance.item")}</th>
                  <th>{fa(pageContract, "col.variance.planned")}</th>
                  <th>{fa(pageContract, "col.variance.verified")}</th>
                  <th>{fa(pageContract, "col.variance.diff")}</th>
                </tr>
              </thead>
              <tbody>
                {varianceRows.map((row) => (
                  <tr key={`${row.feed_day}:${row.shed_id}:${row.partition_label ?? ""}:${row.session_no}:${row.feed_item_key}:${row.workflow}`}>
                    <td>{fmtDate(row.feed_day)}</td>
                    <td>{row.park_label}</td>
                    <td>{row.operational_location_display}</td>
                    <td>{row.session_label || row.session_no}</td>
                    <td>{row.feed_item_label}</td>
                    <td>
                      {row.planned_kg === ""
                        ? fa(pageContract, "variance.planned_unknown")
                        : `${row.planned_kg} ${fa(pageContract, "unit.kg")}`}
                    </td>
                    <td>{`${row.verified_kg} ${fa(pageContract, "unit.kg")}`}</td>
                    <td>
                      {/* Same tag anatomy as the stock check: over-packed points up, short points
                          down, and every row here IS a mismatch, so the tag is always the danger
                          tone for a shortfall and ok tone for an overage. */}
                      <span className={`${num(row.variance_kg) >= 0 ? "tag t-ok" : "tag t-dng"} feed-stock-check-tag`}>
                        <span aria-hidden="true">{num(row.variance_kg) >= 0 ? "↑" : "↓"}</span>
                        <span>{`${nf(Math.abs(num(row.variance_kg)))} ${fa(pageContract, "unit.kg")}`}</span>
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  );
}

function ExecutionStacked({
  stacked,
  statuses,
  pageContract,
}: {
  stacked: StackedDay[];
  statuses: { label: string; colorVar: string }[];
  pageContract: AdminUiPageContract;
}) {
  return (
    <>
      <ChartHover>
        <FeedStackedColumns
          days={stacked}
          seriesLabels={statuses.map((s) => s.label)}
          valueNoun={fa(pageContract, "table.items.noun")}
          chartLabel={fa(pageContract, "chart.execution.title")}
          emptyLabel={fa(pageContract, "empty.execution.body")}
        />
      </ChartHover>
      <FeedChartLegend entries={statuses} />
    </>
  );
}

function ExperimentTab({
  data,
  pageContract,
  parkField,
}: {
  data: FeedAnalyticsExperimentResponse;
  pageContract: AdminUiPageContract;
  parkField: FeedFilterField;
}) {
  // ------- Authored kg BY FEED ITEM (masoor, bhusa, ...) — never trial-arm labels. -------
  const dayKeys = [...new Set(data.items.map((it) => it.feed_day))].sort();
  const itemKeys = [...new Set(data.items.map((it) => it.feed_item_key))];
  // Latest-day kg ranks the chart; one palette slot per item so no two lines share a colour.
  const latestKg = new Map<string, number>();
  for (const key of itemKeys) {
    const rows = data.items.filter((it) => it.feed_item_key === key);
    latestKg.set(key, num(rows[rows.length - 1]?.kg ?? "0"));
  }
  const charted = [...itemKeys]
    .sort((a, b) => (latestKg.get(b) ?? 0) - (latestKg.get(a) ?? 0))
    .slice(0, FEED_SERIES_VARS.length);
  const labelByItem = new Map<string, string>();
  const rowByDayItem = new Map<string, FeedAnalyticsExperimentResponse["items"][number]>();
  for (const item of data.items) {
    labelByItem.set(item.feed_item_key, item.feed_item_label);
    rowByDayItem.set(`${item.feed_day}\u0000${item.feed_item_key}`, item);
  }
  const series: LineSeries[] = charted.map((key, s) => ({
    label: labelByItem.get(key) ?? key,
    colorVar: FEED_SERIES_VARS[s % FEED_SERIES_VARS.length],
    points: dayKeys.map((day) => {
      const row = rowByDayItem.get(`${day}\u0000${key}`);
      return row ? num(row.kg) : null;
    }),
  }));

  // ------- Per-pen wastage for ONE selected day (the card's own date picker). -------
  const wastageStatus = (row: FeedAnalyticsExperimentResponse["wastage_pens"][number]): string => {
    if (row.wastage_kg !== "") return fa(pageContract, "wastage.status.done");
    if (row.lifecycle_status === "") return fa(pageContract, "wastage.status.none");
    if (row.lifecycle_status === "rework") return fa(pageContract, "wastage.status.rework");
    return fa(pageContract, "wastage.status.await");
  };

  return (
    <div className="grid" style={{ gap: 14 }}>
      {data.items.length === 0 ? (
        <section className="card">
          <h2 className="h">{fa(pageContract, "empty.title")}</h2>
          <p className="muted small">{fa(pageContract, "empty.experiment.body")}</p>
        </section>
      ) : (
        <section className="card wchart" aria-label={fa(pageContract, "chart.experiment.title")}>
          <h2 className="h">{fa(pageContract, "chart.experiment.title")}</h2>
          <p className="muted small">{fa(pageContract, "chart.experiment.hint")}</p>
          <ChartHover>
            <FeedLines
              series={series}
              dayLabels={dayKeys}
              valueNoun={fa(pageContract, "unit.kg")}
              chartLabel={fa(pageContract, "chart.experiment.title")}
              emptyLabel={fa(pageContract, "empty.experiment.body")}
            />
          </ChartHover>
          <FeedChartLegend entries={series.map((s) => ({ label: s.label, colorVar: s.colorVar }))} />
        </section>
      )}
      <section className="card" aria-label={fa(pageContract, "wastage.title")}>
        <div className="hd">
          <h3>{fa(pageContract, "wastage.title")}</h3>
          <span className="small muted">{fa(pageContract, "wastage.hint")}</span>
        </div>
        <FeedFilters
          basePath={PAGE_PATH}
          pageParam="fa_offset"
          fields={[
            parkField,
            {
              kind: "date",
              param: "wastage_day",
              label: fa(pageContract, "wastage.date.label"),
              value: data.wastage_day,
              today: todayIso(),
              labels: {
                field: fa(pageContract, "wastage.date.label"),
                today: fa(pageContract, "filter.date.today"),
                single: fa(pageContract, "filter.date.single"),
                range: fa(pageContract, "filter.date.range"),
                aria: fa(pageContract, "filter.date.aria"),
                previousMonth: fa(pageContract, "filter.date.previous_month"),
                nextMonth: fa(pageContract, "filter.date.next_month"),
                rangeStartHint: fa(pageContract, "filter.date.range_start_hint"),
                rangeEndHint: fa(pageContract, "filter.date.range_end_hint"),
                rangeSeparator: fa(pageContract, "filter.date.range_separator"),
              },
            },
          ]}
          pageContract={pageContract}
        />
        {data.wastage_pens.length === 0 ? (
          <p className="muted small">{fa(pageContract, "wastage.empty")}</p>
        ) : (
          <div className="tablewrap" tabIndex={0} role="group" aria-label={fa(pageContract, "wastage.title")}>
            <table className="tbl">
              <thead>
                <tr>
                  <th>{fa(pageContract, "col.wastage.park")}</th>
                  <th>{fa(pageContract, "col.wastage.pen")}</th>
                  <th>{fa(pageContract, "col.wastage.kg")}</th>
                  <th>{fa(pageContract, "col.wastage.status")}</th>
                </tr>
              </thead>
              <tbody>
                {data.wastage_pens.map((row) => (
                  <tr key={`${row.shed_id}|${row.partition_label}`}>
                    <td>{row.park_label}</td>
                    <td>{row.operational_location_display}</td>
                    <td>{row.wastage_kg === "" ? "—" : nf(num(row.wastage_kg))}</td>
                    <td>{wastageStatus(row)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}



function StockCards({
  stock,
  pageContract,
}: {
  stock: FeedAnalyticsStockResponse | null;
  pageContract: AdminUiPageContract;
}) {
  // Only items with a live days-left figure make a card (maintainer request
  // 2026-08-18): an item not directed recently has no burn rate to divide by,
  // and a wall of "not directed recently" boxes buried the ones that matter.
  const active = (stock?.items ?? []).filter(
    (item) => item.days_left !== null && item.days_left !== undefined,
  );
  const farmItems = stock?.farm_items ?? [];
  const forecast = stock?.forecast ?? [];
  return (
    <>
      {!stock || active.length === 0 ? (
        <section className="card" style={{ marginTop: 14 }}>
          <h2 className="h">{fa(pageContract, "stock.title")}</h2>
          <p className="muted small">{fa(pageContract, "stock.empty")}</p>
        </section>
      ) : (
        <section style={{ marginTop: 14 }} aria-label={fa(pageContract, "stock.title")}>
          <h2 className="h">{fa(pageContract, "stock.title")}</h2>
          <p className="muted small">{fa(pageContract, "stock.hint")}</p>
          <div className="grid kpi-row" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))", gap: 14, marginTop: 8 }}>
            {active.map((item) => (
              <div className="kpi card" key={`${item.farm_label}|${item.feed_item_key}`}>
                <div className="dl" title={`${item.farm_label} · ${item.feed_item_label}`}>
                  {`${item.farm_label} · ${item.feed_item_label}`}
                </div>
                <div className="val" style={item.low_stock ? { color: "var(--danger)" } : undefined}>
                  {item.days_left === null || item.days_left === undefined
                    ? fa(pageContract, "stock.never_directed")
                    : `${nf(Math.max(item.days_left, 0))} ${fa(pageContract, "stock.days_left")}`}
                </div>
                <div className="muted small">
                  {`${nf(num(item.balance_kg))} ${fa(pageContract, "stock.balance")}${
                    item.avg_daily_kg ? ` · ${nf(num(item.avg_daily_kg))} ${fa(pageContract, "stock.per_day")}` : ""
                  } · ${fa(pageContract, "stock.batch")} ${item.latest_batch_no}`}
                </div>
                {item.low_stock ? <span className="tag t-dng">{fa(pageContract, "stock.low")}</span> : null}
              </div>
            ))}
          </div>
        </section>
      )}
      <section className="card" style={{ marginTop: 14 }} aria-label={fa(pageContract, "stock.farms.title")}>
        <div className="hd">
          <h3>{fa(pageContract, "stock.farms.title")}</h3>
          <span className="small muted">{fa(pageContract, "stock.farms.hint")}</span>
        </div>
        {farmItems.length === 0 ? (
          <div className="bd">
            <p className="muted small">{fa(pageContract, "stock.farms.empty")}</p>
          </div>
        ) : (
          <div className="tablewrap feed-stock-tablewrap" tabIndex={0} role="group" aria-label={fa(pageContract, "stock.farms.title")}>
            <table className="tbl feed-stock-table">
              <thead>
                <tr>
                  <th>{fa(pageContract, "stock.farms.col.item")}</th>
                  <th>{fa(pageContract, "stock.farms.col.farm")}</th>
                  <th>{fa(pageContract, "stock.farms.col.first_purchase")}</th>
                  <th>{fa(pageContract, "stock.farms.col.directed_since")}</th>
                  <th>{fa(pageContract, "stock.farms.col.last_load")}</th>
                  <th>
                    <span className="feed-stock-check-head">
                      {fa(pageContract, "stock.farms.col.avg")}
                      <span className="feed-stock-info" tabIndex={0} aria-label="How average per day is calculated">
                        i
                        <span className="feed-stock-info-pop" role="tooltip">
                          Avg / Day is the average kg from the latest 3 locked feed days for this farm and item. The top card days-left uses Ledger stock divided by this same average.
                        </span>
                      </span>
                    </span>
                  </th>
                  <th>
                    <span className="feed-stock-check-head">
                      {fa(pageContract, "stock.farms.col.week")}
                      <span className="feed-stock-info" tabIndex={0} aria-label="How weekly requirement is calculated">
                        i
                        <span className="feed-stock-info-pop" role="tooltip">
                          Week need = Avg / Day times 7, using the latest 3 locked feed days for this farm and item.
                        </span>
                      </span>
                    </span>
                  </th>
                  <th>
                    <span className="feed-stock-check-head">
                      {fa(pageContract, "stock.farms.col.stock")}
                      <span className="feed-stock-info" tabIndex={0} aria-label="How stock is calculated">
                        i
                        <span className="feed-stock-info-pop" role="tooltip">
                          Ledger = purchased kg minus consumed-at-import kg, minus locked directed kg from the purchase depletion date onward.
                        </span>
                      </span>
                    </span>
                  </th>
                  <th>
                    <span className="feed-stock-check-head">
                      {fa(pageContract, "stock.farms.col.days_left")}
                      <span className="feed-stock-info" tabIndex={0} aria-label="How days left is calculated">
                        i
                        <span className="feed-stock-info-pop" role="tooltip">
                          Days left = ledger stock divided by Avg / Day. Avg / Day uses the latest 3 locked feed days for this farm and item.
                        </span>
                      </span>
                    </span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {farmItems.map((row) => (
                  <tr key={`${row.feed_item_key}|${row.farm_label}`}>
                    <td>{row.feed_item_label}</td>
                    <td>{row.farm_label}</td>
                    <td>{fmtDate(row.first_purchase_date)}</td>
                    <td>{row.first_directed_day === "" ? fa(pageContract, "stock.never_directed") : fmtDate(row.first_directed_day)}</td>
                    <td>
                      <div className="feed-stock-load">
                        {[
                          `${fa(pageContract, "stock.farms.batch")} ${row.last_load_batch_no}`,
                          fmtDate(row.last_load_date),
                          `${nf(num(row.last_load_quantity_kg))} ${fa(pageContract, "unit.kg")}`,
                        ].join(" · ")}
                      </div>
                      {row.last_load_vendor !== "" ? (
                        <div className="muted small">{row.last_load_vendor}</div>
                      ) : null}
                      {row.last_load_total_cost !== "" ? (
                        <div className="muted small">{`₹${money(num(row.last_load_total_cost))} · ₹${rate(num(row.last_load_per_kg_cost))}/kg`}</div>
                      ) : null}
                    </td>
                    <td>
                      {row.avg_daily_kg === ""
                        ? "—"
                        : `${nf(num(row.avg_daily_kg))} ${fa(pageContract, "unit.kg")}`}
                    </td>
                    <td>
                      {row.weekly_required_kg === ""
                        ? "—"
                        : `${nf(num(row.weekly_required_kg))} ${fa(pageContract, "unit.kg")}`}
                    </td>
                    <td>
                      <div className="feed-stock-qty">{`${nf(num(row.ledger_stock_kg))} ${fa(pageContract, "unit.kg")}`}</div>
                    </td>
                    <td>
                      <DaysLeftText row={row} pageContract={pageContract} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <ForecastTable rows={forecast} pageContract={pageContract} />
    </>
  );
}

// Next-7-days requirement and cost. Every figure is backend-composed; an empty
// string on a row means the backend could not price or balance it (no load in
// the purchase ledger for that farm and feed), which is rendered as a stated
// reason rather than a silent zero -- a zero would read as "nothing needed".
function ForecastTable({
  rows,
  pageContract,
}: {
  rows: FeedAnalyticsStockResponse["forecast"];
  pageContract: AdminUiPageContract;
}) {
  let requiredCostTotal = 0;
  let anyPriced = false;
  rows.forEach((row) => {
    if (row.required_cost !== "") {
      requiredCostTotal += num(row.required_cost);
      anyPriced = true;
    }
  });
  return (
    <section className="card" style={{ marginTop: 14 }} aria-label={fa(pageContract, "forecast.title")}>
      <div className="hd">
        <h3>{fa(pageContract, "forecast.title")}</h3>
        <span className="small muted">{fa(pageContract, "forecast.hint")}</span>
      </div>
      {rows.length === 0 ? (
        <p className="muted small">{fa(pageContract, "forecast.empty")}</p>
      ) : (
        <div className="tablewrap" tabIndex={0} role="group" aria-label={fa(pageContract, "forecast.title")}>
          <table className="tbl feed-forecast-table">
            <thead>
              <tr>
                <th>{fa(pageContract, "forecast.col.farm")}</th>
                <th>{fa(pageContract, "forecast.col.item")}</th>
                <th>{fa(pageContract, "forecast.col.avg")}</th>
                <th>{fa(pageContract, "forecast.col.required")}</th>
                <th>{fa(pageContract, "forecast.col.stock")}</th>
                <th>{fa(pageContract, "forecast.col.shortfall")}</th>
                <th>{fa(pageContract, "forecast.col.rate")}</th>
                <th>{fa(pageContract, "forecast.col.required_cost")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const kg = fa(pageContract, "unit.kg");
                const short = num(row.shortfall_kg);
                return (
                  <tr key={`${row.farm_label}:${row.feed_item_key}`}>
                    <td>{row.farm_label}</td>
                    <td>{row.feed_item_label}</td>
                    <td>{`${nf(num(row.avg_daily_kg))} ${kg}`}</td>
                    <td>
                      <strong>{`${nf(num(row.required_kg))} ${kg}`}</strong>
                    </td>
                    <td>{row.stock_kg === "" ? "—" : `${nf(num(row.stock_kg))} ${kg}`}</td>
                    <td>
                      {row.shortfall_kg === "" ? (
                        "—"
                      ) : short > 0 ? (
                        <span className="tag t-dng feed-stock-check-tag">
                          <span>{`${nf(short)} ${kg}`}</span>
                        </span>
                      ) : (
                        <span className="tag t-ok feed-stock-check-tag">
                          <span>{fa(pageContract, "forecast.covered")}</span>
                        </span>
                      )}
                    </td>
                    <td>
                      {row.per_kg_cost === ""
                        ? fa(pageContract, "forecast.unpriced")
                        : `₹${rate(num(row.per_kg_cost))}`}
                    </td>
                    <td>{row.required_cost === "" ? "—" : `₹${money(num(row.required_cost))}`}</td>
                  </tr>
                );
              })}
              {anyPriced ? (
                <tr className="feed-forecast-total">
                  <td colSpan={7}>
                    <strong>{fa(pageContract, "forecast.total")}</strong>
                  </td>
                  <td>
                    <strong>{`₹${money(requiredCostTotal)}`}</strong>
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function DaysLeftText({
  row,
  pageContract,
}: {
  row: FeedAnalyticsStockResponse["farm_items"][number];
  pageContract: AdminUiPageContract;
}) {
  const avg = num(row.avg_daily_kg);
  if (avg <= 0) return <>{fa(pageContract, "stock.farms.unavailable")}</>;
  const daysLeft = Math.max(Math.floor(num(row.ledger_stock_kg) / avg), 0);
  return <>{`${nf(daysLeft)} ${fa(pageContract, "stock.days_left")}`}</>;
}
