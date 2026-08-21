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
        <ExecutionTab data={execution.data} pageContract={pageContract} />
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
          <div className="tablewrap" tabIndex={0} role="group" aria-label={fa(pageContract, "stock.farms.title")}>
            <table className="tbl">
              <thead>
                <tr>
                  <th>{fa(pageContract, "stock.farms.col.item")}</th>
                  <th>{fa(pageContract, "stock.farms.col.farm")}</th>
                  <th>{fa(pageContract, "stock.farms.col.first_purchase")}</th>
                  <th>{fa(pageContract, "stock.farms.col.directed_since")}</th>
                  <th>{fa(pageContract, "stock.farms.col.last_load")}</th>
                  <th>{fa(pageContract, "stock.farms.col.avg")}</th>
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
                      <div>
                        {[
                          `${fa(pageContract, "stock.farms.batch")} ${row.last_load_batch_no}`,
                          fmtDate(row.last_load_date),
                          `${nf(num(row.last_load_quantity_kg))} ${fa(pageContract, "unit.kg")}`,
                        ].join(" · ")}
                      </div>
                      {row.last_load_vendor !== "" ? (
                        <div className="muted small">{row.last_load_vendor}</div>
                      ) : null}
                    </td>
                    <td>
                      {row.avg_daily_kg === ""
                        ? "—"
                        : `${nf(num(row.avg_daily_kg))} ${fa(pageContract, "unit.kg")}`}
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
