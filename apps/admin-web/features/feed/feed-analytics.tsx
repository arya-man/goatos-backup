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
import { istDayPlus, todayIso } from "@/lib/format";
import { backendScope, parseScope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { ChartHover } from "@/components/chart-hover";
import { SegmentedLinks } from "@/components/segmented-links";
import { SvgBars } from "@/components/svg-bars";
import {
  FEED_SERIES_VARS,
  FeedChartLegend,
  FeedLines,
  FeedStackedColumns,
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
const TABS = ["overview", "items", "peranimal", "experiment", "execution"] as const;
type Tab = (typeof TABS)[number];
const RANGES = ["30", "61", "92"] as const;
type Range = (typeof RANGES)[number];

const nf = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 1 });
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

/**
 * Explicit calendar window from ?date_from/?date_to (both required, valid ISO
 * dates, from <= to). Overrides the preset range when present; the preset bar
 * links clear these params so the two controls never fight.
 */
function readCustomWindow(sp: RouteSearchParams): { date_from: string; date_to: string } | null {
  const isoDay = /^\d{4}-\d{2}-\d{2}$/;
  const from = one(sp, "date_from");
  const to = one(sp, "date_to");
  if (!from || !to || !isoDay.test(from) || !isoDay.test(to) || from > to) return null;
  return { date_from: from, date_to: to };
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
  headsLine: LineSeries[];
  latestDay?: FeedAnalyticsDirectedResponse["days"][number];
};

function buildDirectedView(data: FeedAnalyticsDirectedResponse, otherLabel: string, headsLabel: string): DirectedView {
  const dayKeys = data.days.map((d) => d.feed_day);
  const totalsByItem = new Map<string, { label: string; total: number }>();
  for (const item of data.items) {
    const existing = totalsByItem.get(item.feed_item_key);
    const total = (existing?.total ?? 0) + num(item.directed_kg);
    totalsByItem.set(item.feed_item_key, { label: item.feed_item_label, total });
  }
  const ranked = [...totalsByItem.entries()].sort((a, b) => b[1].total - a[1].total);
  const top = ranked.slice(0, 4);
  const topKeys = new Set(top.map(([key]) => key));
  const hasOther = ranked.length > top.length;
  const itemLabels = [...top.map(([, v]) => v.label), ...(hasOther ? [otherLabel] : [])];

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
      colorVar: FEED_SERIES_VARS[s % FEED_SERIES_VARS.length],
      points: dayKeys.map((day) => {
        const row = data.items.find((it) => it.feed_day === day && it.feed_item_key === key);
        return row ? num(row.directed_kg) : null;
      }),
    }),
  );

  const perHeadSeries: LineSeries[] = ranked.map(([key, v], s) => ({
    label: v.label,
    colorVar: FEED_SERIES_VARS[s % FEED_SERIES_VARS.length],
    points: dayKeys.map((day) => {
      const row = data.items.find((it) => it.feed_day === day && it.feed_item_key === key);
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
    headsLine: [
      {
        label: headsLabel,
        colorVar: FEED_SERIES_VARS[0],
        points: data.days.map((d) => d.head_days),
      },
    ],
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
  const customWindow = readCustomWindow(searchParams);
  const window = customWindow ?? rangeDates(range);
  const params = { park_id: parkId, ...window };

  // Overview needs directed + execution (for the adherence KPI); every other
  // tab reads exactly its own endpoint.
  const wantDirected = tab === "overview" || tab === "items" || tab === "peranimal";
  const wantExecution = tab === "overview" || tab === "execution";
  const wantExperiment = tab === "experiment";
  const wantStock = tab === "overview" || tab === "items";
  const [directed, execution, experiment, stock] = await Promise.all([
    wantDirected
      ? getFeedAnalyticsDirected(params)
      : Promise.resolve<ApiResult<FeedAnalyticsDirectedResponse> | null>(null),
    wantExecution
      ? getFeedAnalyticsExecution(params)
      : Promise.resolve<ApiResult<FeedAnalyticsExecutionResponse> | null>(null),
    wantExperiment
      ? getFeedAnalyticsExperiment(params)
      : Promise.resolve<ApiResult<FeedAnalyticsExperimentResponse> | null>(null),
    wantStock
      ? getFeedAnalyticsStock(params)
      : Promise.resolve<ApiResult<FeedAnalyticsStockResponse> | null>(null),
  ]);
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
          current={customWindow ? "" : range}
          ariaLabel={fa(pageContract, "range.aria")}
          options={RANGES.map((r) => ({
            value: r,
            label: fa(pageContract, `range.${r}`),
            href: hrefWith(searchParams, {
              range: r === "30" ? undefined : r,
              date_from: undefined,
              date_to: undefined,
            }),
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
          data={directed.data}
          execution={execution?.ok ? execution.data : null}
          stock={stock?.ok ? stock.data : null}
          pageContract={pageContract}
          searchParams={searchParams}
        />
      ) : null}

      {tab === "execution" ? (
        <ExecutionCalendarFilter
          searchParams={searchParams}
          window={window}
          pageContract={pageContract}
        />
      ) : null}

      {tab === "execution" && execution?.ok ? (
        <ExecutionTab data={execution.data} pageContract={pageContract} />
      ) : null}

      {tab === "experiment" && experiment?.ok ? (
        <ExperimentTab data={experiment.data} pageContract={pageContract} />
      ) : null}
    </div>
  );
}

function DirectedTabs({
  tab,
  data,
  execution,
  stock,
  pageContract,
  searchParams,
}: {
  tab: Tab;
  data: FeedAnalyticsDirectedResponse;
  execution: FeedAnalyticsExecutionResponse | null;
  stock: FeedAnalyticsStockResponse | null;
  pageContract: AdminUiPageContract;
  searchParams: RouteSearchParams;
}) {
  const view = buildDirectedView(data, fa(pageContract, "series.other"), fa(pageContract, "unit.heads"));
  const empty = data.days.length === 0;
  const noData = fa(pageContract, "empty.title");

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

      {tab === "items" ? (
        <StockCards stock={stock} pageContract={pageContract} searchParams={searchParams} />
      ) : null}

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
              colorVar: FEED_SERIES_VARS[s % FEED_SERIES_VARS.length],
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

      {tab === "overview" ? (
        <section className="card wchart" aria-label={fa(pageContract, "chart.heads.title")}>
          <h2 className="h">{fa(pageContract, "chart.heads.title")}</h2>
          <p className="muted small">{fa(pageContract, "chart.heads.hint")}</p>
          <ChartHover>
            <FeedLines
              series={view.headsLine}
              dayLabels={view.dayLabels}
              valueNoun={fa(pageContract, "unit.heads")}
              chartLabel={fa(pageContract, "chart.heads.title")}
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

/**
 * Calendar window for the Execution tab (maintainer request 2026-08-19).
 * A plain GET form: native date pickers write ?date_from/?date_to and the
 * server component re-reads. Defaults to the effective window — last 30 days
 * ending yesterday unless a custom window is already applied. Scope/tab params
 * ride along as hidden inputs so applying dates never resets them.
 */
function ExecutionCalendarFilter({
  searchParams,
  window,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  window: { date_from: string; date_to: string };
  pageContract: AdminUiPageContract;
}) {
  const carried: [string, string][] = [];
  for (const [key, value] of Object.entries(searchParams ?? {})) {
    if (key === "date_from" || key === "date_to") continue;
    if (Array.isArray(value)) value.forEach((item) => carried.push([key, item]));
    else if (value) carried.push([key, value]);
  }
  return (
    <form
      method="get"
      action={PAGE_PATH}
      className="card"
      style={{ display: "flex", flexWrap: "wrap", gap: 10, alignItems: "end", padding: 12 }}
      aria-label={fa(pageContract, "filter.calendar.aria")}
    >
      {carried.map(([key, value], i) => (
        <input key={`${key}-${i}`} type="hidden" name={key} value={value} />
      ))}
      <label className="small" style={{ display: "grid", gap: 4 }}>
        <span className="muted">{fa(pageContract, "filter.from.label")}</span>
        <input type="date" name="date_from" defaultValue={window.date_from} max={window.date_to} required />
      </label>
      <label className="small" style={{ display: "grid", gap: 4 }}>
        <span className="muted">{fa(pageContract, "filter.to.label")}</span>
        <input type="date" name="date_to" defaultValue={window.date_to} required />
      </label>
      <button type="submit" className="btn">
        {fa(pageContract, "filter.apply.label")}
      </button>
    </form>
  );
}

function ExecutionTab({
  data,
  pageContract,
}: {
  data: FeedAnalyticsExecutionResponse;
  pageContract: AdminUiPageContract;
}) {
  if (data.days.length === 0) {
    return (
      <section className="card">
        <h2 className="h">{fa(pageContract, "empty.title")}</h2>
        <p className="muted small">{fa(pageContract, "empty.execution.body")}</p>
      </section>
    );
  }
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
}: {
  data: FeedAnalyticsExperimentResponse;
  pageContract: AdminUiPageContract;
}) {
  if (data.sheds.length === 0) {
    return (
      <section className="card">
        <h2 className="h">{fa(pageContract, "empty.title")}</h2>
        <p className="muted small">{fa(pageContract, "empty.experiment.body")}</p>
      </section>
    );
  }
  // Shed-wise (maintainer decision 2026-08-19): each trial PEN is its own
  // series and table row, labelled by the backend-composed location display.
  const dayKeys = [...new Set(data.sheds.map((s) => s.feed_day))].sort();
  const penNames = [...new Set(data.sheds.map((s) => s.operational_location_display))];
  // Latest sheet day per pen, used to rank the chart and fill the table.
  const latestByPen = penNames.map((pen) => {
    const rows = data.sheds.filter((s) => s.operational_location_display === pen);
    return rows[rows.length - 1];
  });
  // The farm runs many more trial pens than the series palette has hues. Chart
  // only the largest (one per palette slot, so no two lines share a colour) and
  // DISCLOSE the cap — the table below lists every pen. Ranking is a sort of a
  // backend field, not a new business number.
  const charted = [...latestByPen]
    .sort((a, b) => num(b.absolute_kg) - num(a.absolute_kg))
    .slice(0, FEED_SERIES_VARS.length)
    .map((row) => row.operational_location_display);
  const series: LineSeries[] = charted.map((pen, s) => ({
    label: pen,
    colorVar: FEED_SERIES_VARS[s % FEED_SERIES_VARS.length],
    points: dayKeys.map((day) => {
      const row = data.sheds.find(
        (r) => r.feed_day === day && r.operational_location_display === pen,
      );
      return row ? num(row.absolute_kg) : null;
    }),
  }));
  return (
    <div className="grid" style={{ gap: 14 }}>
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
        <p className="muted small">{fa(pageContract, "chart.experiment.top")}</p>
      </section>
      <ExperimentItemsChart items={data.items} pageContract={pageContract} />
      <section className="card" aria-label={fa(pageContract, "table.arms.title")}>
        <h2 className="h">{fa(pageContract, "table.arms.title")}</h2>
        <p className="muted small">{fa(pageContract, "table.arms.hint")}</p>
        <div className="tablewrap" tabIndex={0} role="group" aria-label={fa(pageContract, "table.arms.title")}>
          <table className="tbl">
            <thead>
              <tr>
                <th>{fa(pageContract, "col.pen")}</th>
                <th>{fa(pageContract, "col.arm")}</th>
                <th>{fa(pageContract, "col.kg")}</th>
              </tr>
            </thead>
            <tbody>
              {latestByPen.map((row) => (
                <tr key={row.operational_location_display}>
                  <td>{row.operational_location_display}</td>
                  <td>{row.experiment_arm}</td>
                  <td>{nf(num(row.absolute_kg))}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="muted small">{fa(pageContract, "chart.experiment.hint")}</p>
      </section>
    </div>
  );
}

/**
 * Kg per day by FEED TYPE across the trial pens (maintainer request
 * 2026-08-19): what the experiment sheds actually ate — Dry Masoor Bhusa, the
 * concentrates — rather than how many pens ate it.
 */
function ExperimentItemsChart({
  items,
  pageContract,
}: {
  items: FeedAnalyticsExperimentResponse["items"];
  pageContract: AdminUiPageContract;
}) {
  if (items.length === 0) return null;
  const dayKeys = [...new Set(items.map((it) => it.feed_day))].sort();
  const labels = [...new Set(items.map((it) => it.feed_item_label))];
  // Rank items by window kg and chart one per palette slot, same cap-and-
  // disclose shape as the pens chart above.
  const windowKg = new Map<string, number>();
  for (const it of items) {
    windowKg.set(it.feed_item_label, (windowKg.get(it.feed_item_label) ?? 0) + num(it.absolute_kg));
  }
  const charted = [...labels]
    .sort((a, b) => (windowKg.get(b) ?? 0) - (windowKg.get(a) ?? 0))
    .slice(0, FEED_SERIES_VARS.length);
  const series: LineSeries[] = charted.map((label, s) => ({
    label,
    colorVar: FEED_SERIES_VARS[s % FEED_SERIES_VARS.length],
    points: dayKeys.map((day) => {
      const row = items.find((it) => it.feed_day === day && it.feed_item_label === label);
      return row ? num(row.absolute_kg) : null;
    }),
  }));
  return (
    <section className="card wchart" aria-label={fa(pageContract, "chart.expitems.title")}>
      <h2 className="h">{fa(pageContract, "chart.expitems.title")}</h2>
      <p className="muted small">{fa(pageContract, "chart.expitems.hint")}</p>
      <ChartHover>
        <FeedLines
          series={series}
          dayLabels={dayKeys}
          valueNoun={fa(pageContract, "unit.kg")}
          chartLabel={fa(pageContract, "chart.expitems.title")}
          emptyLabel={fa(pageContract, "empty.experiment.body")}
        />
      </ChartHover>
      <FeedChartLegend entries={series.map((s) => ({ label: s.label, colorVar: s.colorVar }))} />
    </section>
  );
}


function StockCards({
  stock,
  pageContract,
  searchParams,
}: {
  stock: FeedAnalyticsStockResponse | null;
  pageContract: AdminUiPageContract;
  searchParams: RouteSearchParams;
}) {
  // Only items with a live days-left figure make a card (maintainer request
  // 2026-08-18): an item not directed recently has no burn rate to divide by,
  // and a wall of "not directed recently" boxes buried the ones that matter.
  const active = (stock?.items ?? []).filter(
    (item) => item.days_left !== null && item.days_left !== undefined,
  );
  if (!stock || active.length === 0) {
    return (
      <section className="card" style={{ marginTop: 14 }}>
        <h2 className="h">{fa(pageContract, "stock.title")}</h2>
        <p className="muted small">{fa(pageContract, "stock.empty")}</p>
      </section>
    );
  }
  // Stock is a PER-PARK fact (maintainer decision 2026-08-19): one farm's
  // store per view, toggled like the maintainer's stock cards — a link-based
  // segmented control on ?stockpark, so it survives SSR without client state.
  const parks = [...new Set(active.map((item) => item.park_label))].sort();
  const requested = one(searchParams, "stockpark");
  const selectedPark = parks.includes(requested ?? "") ? (requested as string) : parks[0];
  const shown = active.filter((item) => item.park_label === selectedPark);
  return (
    <section style={{ marginTop: 14 }} aria-label={fa(pageContract, "stock.title")}>
      <h2 className="h">{fa(pageContract, "stock.title")}</h2>
      <p className="muted small">{fa(pageContract, "stock.hint")}</p>
      {parks.length > 1 ? (
        <div style={{ margin: "8px 0" }}>
          <SegmentedLinks
            current={selectedPark}
            ariaLabel={fa(pageContract, "stock.park.aria")}
            options={parks.map((park) => ({
              value: park,
              label: park,
              href: hrefWith(searchParams, { stockpark: park === parks[0] ? undefined : park }),
            }))}
          />
        </div>
      ) : null}
      <div className="grid kpi-row" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))", gap: 14, marginTop: 8 }}>
        {shown.map((item) => (
          <div className="kpi card" key={item.feed_item_key}>
            <div className="dl" title={item.feed_item_label}>{item.feed_item_label}</div>
            <div className="val" style={item.low_stock ? { color: "var(--danger)" } : undefined}>
              {item.days_left === null || item.days_left === undefined
                ? fa(pageContract, "stock.never_directed")
                : `${nf(item.days_left)} ${fa(pageContract, "stock.days_left")}`}
            </div>
            <div className="muted small">
              {`${nf(num(item.balance_kg))} ${fa(pageContract, "stock.balance")} · ${fa(pageContract, "stock.batch")} ${item.latest_batch_no}`}
            </div>
            {item.low_stock ? <span className="tag t-dng">{fa(pageContract, "stock.low")}</span> : null}
          </div>
        ))}
      </div>
    </section>
  );
}
