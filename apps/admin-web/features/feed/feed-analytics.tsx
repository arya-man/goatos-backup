import { FilterChip } from "@/components/minimal/list/filter-chip";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import { Label } from "@/components/minimal/label";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";

import { InfoHint } from "@/components/app/info-hint";
import { PageHeader } from "@/components/app/page-header";
import Grid from "@mui/material/Grid";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import { EmptyContent } from "@/components/minimal/empty-content";
import { EcommerceWidgetSummary } from "@/components/minimal/sections/overview/e-commerce/ecommerce-widget-summary";
import { FeedMixCard } from "./feed-mix-card";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";

import { copy, optionGroup, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getFeedAnalyticsDirected,
  getFeedAnalyticsExecution,
  getFeedAnalyticsExperiment,
  getFeedAnalyticsStock,
  getFeedAnalyticsStockLoads,
  getFeedAnalyticsFollowUp,
  getFeedAnalyticsShedFeed,
  type ApiResult,
  type FeedAnalyticsDirectedResponse,
  type FeedAnalyticsExecutionResponse,
  type FeedAnalyticsExperimentResponse,
  type FeedAnalyticsStockResponse,
  type FeedAnalyticsStockLoadsResponse,
  type FeedAnalyticsFollowUpResponse,
  type FeedAnalyticsShedFeedResponse,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDate, istDayPlus, todayIso } from "@/lib/format";
import { backendScope, parseScope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { RangeCoverageNote } from "./range-coverage-note";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedPager } from "./feed-pager";
import { FeedCompletionTable } from "./feed-completion-table";
import { FeedFollowUpTab } from "./feed-follow-up";
import { FeedStockLoadsTable } from "./feed-stock-loads-table";
import { FeedShedFeedCharts } from "./feed-shed-feed-charts";
import { feedHref, feedLimit, feedOffset } from "./feed-scope";
import { SegmentedLinks } from "@/components/segmented-links";
import { LocalViewPane, LocalViewToggle } from "@/components/local-view-switch";
import {
  FEED_SERIES_VARS,
  FeedChartLegend,
  FeedLines,
  FeedSpendPie,
  FeedStackedColumns,
  seriesColorVar,
  type LineSeries,
  type PieSlice,
  type StackedDay,
} from "./feed-analytics-charts";
import { FeedFaroView } from "./feed-faro-view";
import { completeDaySeries } from "./feed-spark-series";
import { FeedAnalyticsExport } from "@/components/analytics-export";
import { stageLabel } from "@/lib/stage-labels";

// Feed -> Feed Analytics. The leadership read of the feed chain over a date
// window: DIRECTED kg off the frozen sheet, ration per animal, execution
// adherence off the proof gates, and the trial arms.
//
// The one word this page lives or dies on is DIRECTED. Completions carry
// proofs, never weights, so nothing here is consumption — the contract's
// `banner.basis` line says so on every tab and every quantity label comes from
// the backend copy map already carrying that framing.
//
// Rendering rules inherited from the repo chart stack: a server page whose charts
// are the template's ApexCharts marks fed pre-composed strings (components/
// svg-series.tsx), series colour follows the FEED ITEM
// across charts (never its rank on one chart), and the page derives NO business
// number of its own — every figure below is a backend field or a straight
// per-day re-grouping of backend rows for drawing. Park scope belongs to the
// top bar (Scope Chrome Rule); the page body owns only the range and tab state,
// both as URL params so a view survives reload and pastes as a link.

const PAGE_PATH = "/feed/analytics";
// A reading with no day series: the template widget draws no sparkline under two points.
const NO_SPARK = { categories: [], series: [] };

// Follow-up lines are one per (pen, change day); counted here, on the server, because the tab that
// sorts and slices them is a client module and cannot be called from this one.
function followUpLineCount(data: FeedAnalyticsFollowUpResponse): number {
  return data.rows.reduce((n, pen) => n + pen.days.length, 0);
}

// Select options from served rows: first label wins per value, sorted for a stable dropdown.
function dedupeOptions(options: { value: string; label: string }[]): { value: string; label: string }[] {
  const seen = new Map<string, string>();
  for (const o of options) {
    if (o.value !== "" && !seen.has(o.value)) seen.set(o.value, o.label);
  }
  return [...seen.entries()].map(([value, label]) => ({ value, label })).sort((a, b) => a.label.localeCompare(b.label));
}
const TABS = ["overview", "items", "peranimal", "experiment", "execution", "followup"] as const;
type Tab = (typeof TABS)[number];
// The Consumption tab's two readings (maintainer request 2026-09-17): General is everything the tab
// already showed; Status-wise is the average directed feed one animal gets per day, per pen tag.
const CONSUMPTION_VIEWS = ["general", "status"] as const;
type ConsumptionView = (typeof CONSUMPTION_VIEWS)[number];
const RANGES = ["30", "61", "92"] as const;
type Range = (typeof RANGES)[number];
// The expenditure chart's two readings (maintainer ask 2026-09-07): the day's ₹ as it is, or
// that ₹ over the animals on the SAME day's sheet. A URL param like the range, so a pasted
// link opens on the reading the reader was looking at; absent means overall.
const SPEND_MODES = ["overall", "per_animal"] as const;
type SpendMode = (typeof SPEND_MODES)[number];

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

function readTab(sp: RouteSearchParams, allowedTabs: readonly Tab[] = TABS): Tab {
  const raw = one(sp, "tab");
  if ((allowedTabs as readonly string[]).includes(raw ?? "")) return raw as Tab;
  return allowedTabs[0] ?? "overview";
}

function readRange(sp: RouteSearchParams): Range {
  const raw = one(sp, "range");
  return (RANGES as readonly string[]).includes(raw ?? "") ? (raw as Range) : "30";
}

function readSpendMode(sp: RouteSearchParams): SpendMode {
  const raw = one(sp, "spend");
  return (SPEND_MODES as readonly string[]).includes(raw ?? "") ? (raw as SpendMode) : "overall";
}

/**
 * The completion table's own day picker; absent/malformed means the BACKEND default (yesterday,
 * IST). Deliberately not defaulted here: the day the table describes is business truth the payload
 * echoes back as `completion_day`, and a second client-side default would drift from it the moment
 * the two clocks disagree — which they do for every request between midnight and 05:30 IST.
 */
function readCompletionDay(sp: RouteSearchParams): string | undefined {
  const raw = one(sp, "fdc_day");
  return raw && /^\d{4}-\d{2}-\d{2}$/.test(raw) ? raw : undefined;
}

/**
 * The completion table's status filter. An unknown value is dropped rather than forwarded: the
 * backend rejects one with a 400, which would take the whole page down over a mistyped URL.
 */
const COMPLETION_STATUSES = ["not_started", "pending_verification", "rework", "completed"] as const;
function readCompletionStatus(sp: RouteSearchParams): string {
  const raw = one(sp, "fdc_status") ?? "";
  return (COMPLETION_STATUSES as readonly string[]).includes(raw) ? raw : "";
}

/** The wastage table's own day picker; absent/malformed means the backend default (today, IST). */
function readWastageDay(sp: RouteSearchParams): string | undefined {
  const raw = one(sp, "wastage_day");
  return raw && /^\d{4}-\d{2}-\d{2}$/.test(raw) ? raw : undefined;
}

function readConsumptionView(sp: RouteSearchParams): ConsumptionView {
  const raw = one(sp, "fc_view");
  return (CONSUMPTION_VIEWS as readonly string[]).includes(raw ?? "") ? (raw as ConsumptionView) : "general";
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

function rangeDates(range: Range, endDay?: string): { date_from: string; date_to: string } {
  // Asia/Kolkata calendar arithmetic via the shared IST helpers — the same
  // business-day rule the backend applies to its own defaults. The previous
  // Date/toISOString version ran on the server's UTC clock and dropped
  // yesterday for any request between 00:00 and 05:29 IST (review, PR #64).
  const days = Number(range);
  const to = endDay ?? istDayPlus(todayIso(), -1);
  return { date_from: istDayPlus(to, -(days - 1)), date_to: to };
}

// ---------------------------------------------------------------------------
// Directed-view regrouping (drawing shape only — no derived business numbers)
// ---------------------------------------------------------------------------

/** A feed item's line, keyed so the stock read's money can be joined to it. */
type ItemLineSeries = LineSeries & { key: string };

type DirectedView = {
  dayLabels: string[];
  /** Top feed items by window kg, in stable order; the rest fold into one slot. */
  itemLabels: string[];
  stacked: StackedDay[];
  mix: { key: string; label: string; value: number }[];
  itemSeries: ItemLineSeries[];
  perHead: LineSeries[];
  latestDay?: FeedAnalyticsDirectedResponse["days"][number];
  /** The last settled business day (yesterday); the KPI sparklines end here, never on today. */
  settledDay: string;
};

function buildDirectedView(
  data: FeedAnalyticsDirectedResponse,
  otherLabel: string,
  settledDay: string,
): DirectedView {
  // Day totals are intentionally sheet-only because the KPI tiles read them as
  // "on the issued sheet", but item rows can now include milk-only days from
  // feed_effective_external_consumption. The chart axis must carry both sets or
  // it silently drops those milk rows before rendering.
  const dayKeys = [...new Set([...data.days.map((d) => d.feed_day), ...data.items.map((item) => item.feed_day)])].sort();
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
  const itemSeries: ItemLineSeries[] = ranked.map(
    ([key, v], s) => ({
      key,
      label: v.label,
      colorVar: seriesColorVar(s),
      points: dayKeys.map((day) => {
        const row = rowFor(day, key);
        return row ? num(row.directed_kg) : null;
      }),
    }),
  );

  // A ration card per feed item — but ONLY for items that have a per-head figure at
  // all. The milk ledger records a park's litres for a day, not which pens drank them,
  // so its per-head points are all empty; rendering its card anyway put a "—" beside
  // "No issued feed direction covers the selected dates", which blames the sheet for a
  // question this feed cannot answer. The colour comes from the item's ranked position,
  // so dropping a card leaves every other item the colour it has on the other charts.
  const perHeadSeries: LineSeries[] = ranked
    .map(([key, v], s) => ({
      label: v.label,
      colorVar: seriesColorVar(s),
      points: dayKeys.map((day) => {
        const row = rowFor(day, key);
        return row && row.per_head_grams !== "" ? num(row.per_head_grams) : null;
      }),
    }))
    .filter((series) => series.points.some((point) => point !== null));

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
    // The tiles describe the last SETTLED business day — yesterday — not the last day the
    // charts draw. Since the series now runs through today, taking the array's last element
    // would have pointed "Directed yesterday" at a day the farm is still feeding, and at a
    // sheet whose second park may not be issued yet. Named explicitly rather than taken
    // positionally, so the number under the label is the day the label says.
    latestDay: data.days.find((d) => d.feed_day === settledDay),
    settledDay,
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
  const allowedTabs = optionGroup(pageContract, "feed_analytics_tabs")
    .map((item) => item.key)
    .filter((key): key is Tab => (TABS as readonly string[]).includes(key));
  // Stock-scope only: a reader holding the stock permission but not the full feed read (the
  // Procurement Director) is offered Stock and its load-by-load reading and nothing that needs
  // the directed rollup -- so none of the directed/execution reads run for them.
  const stockOnly = !allowedTabs.includes("overview");
  const tab = readTab(searchParams, allowedTabs);
  // Status-wise reads ONLY the pen-tag arm of the directed rollup: none of General's KPI tiles,
  // spend, money cards or pen charts render there, so none of their reads run.
  const consumptionView: ConsumptionView = tab === "overview" ? readConsumptionView(searchParams) : "general";
  const range = stockOnly ? "30" : readRange(searchParams);
  const spendMode = readSpendMode(searchParams);
  const { parkId } = backendScope(parseScope(searchParams));
  const window = rangeDates(range);
  const params = { park_id: parkId, ...window };
  // The two DAILY SERIES run through TODAY (maintainer decision 2026-09-02): today's sheet
  // is already issued and frozen, so the kg it directs is settled fact and holding the
  // charts back a day showed the reader an empty space where a known day belongs.
  //
  // The rest of the page stays on the window ending YESTERDAY, and that is the point of
  // keeping two windows rather than moving one: the execution arm counts verified packing
  // and distribution, and today's work is still being done — folding it in would read as a
  // verification failure rather than as work in progress. The KPI tiles likewise keep
  // naming yesterday (see settledDay below).
  const chartWindow = rangeDates(range, todayIso());
  const chartParams = { park_id: parkId, ...chartWindow };

  // Overview needs directed + execution (for the adherence KPI); every other
  // tab reads exactly its own endpoint.
  // The Stock tab reads the purchase ledger only: its cards must never wait on (or be hidden by)
  // the daily-sheet read, which it does not use (maintainer request 2026-09-24).
  const wantDirected = !stockOnly && (tab === "overview" || tab === "peranimal");
  // Consumption renders BOTH its readings (General and Status-wise) so the toggle between them is
  // local and never asks the server again; one directed read carries both sections.
  const directedSections = tab === "overview" ? "days,items,pen_tags" : "days,items";
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
  // Resolved after the main batch: with no day in the URL the table opens on the latest packing
  // day that HAS measured bags (see below), not on today, whose bags nobody has weighed yet.
  let variancePackingDay = favDay || todayIso();
  const favPark = one(searchParams, "fav_park") ?? "";
  const favItem = one(searchParams, "fav_item") ?? "";
  const variancePageSizes = tablePageSizes(pageContract, "packing-mismatches");
  const varianceLimit = feedLimit(searchParams, "fav_limit", variancePageSizes, variancePageSizes[0]);
  const varianceOffset = feedOffset(searchParams, "fav_offset");
  // The completion table's own state: a day, a page and three narrowing filters, all in the URL so
  // a view survives reload and pastes as a link. The DAY is not defaulted here -- the backend owns
  // it (yesterday, IST) and echoes it back as completion_day, and a second client-side default
  // would drift from it for every request between midnight and 05:30 IST.
  const followUpPageSizes = tablePageSizes(pageContract, "feed-follow-up");
  const followUpLimit = feedLimit(searchParams, "ffu_limit", followUpPageSizes, followUpPageSizes[0]);
  const followUpOffset = feedOffset(searchParams, "ffu_offset");
  const completionPageSizes = tablePageSizes(pageContract, "distribution-completions");
  const completionLimit = feedLimit(searchParams, "fdc_limit", completionPageSizes, completionPageSizes[0]);
  const completionOffset = feedOffset(searchParams, "fdc_offset");
  const completionParkFilter = parkId || (one(searchParams, "fdc_park") ?? "");
  const completionShedFilter = one(searchParams, "fdc_shed") ?? "";
  const completionStatusFilter = readCompletionStatus(searchParams);
  const wantStock = tab === "overview" || tab === "items";
  // Purchased vs consumed is the LAST TABLE ON THE STOCK TAB (maintainer instruction 2026-09-21),
  // not a tab of its own: the cards answer "how much is in the store", this answers "what happened
  // to each load that put it there", and a reader should not have to change tabs between the two.
  // Its own paged endpoint keeps its own farm / feed-item narrowing.
  const wantLoads = tab === "items";
  const loadsPageSizes = tablePageSizes(pageContract, "stock-loads");
  const loadsLimit = feedLimit(searchParams, "fl_limit", loadsPageSizes, loadsPageSizes[0]);
  const loadsOffset = feedOffset(searchParams, "fl_offset");
  const loadsFarm = one(searchParams, "fl_farm") ?? "";
  const loadsItem = one(searchParams, "fl_item") ?? "";
  // Each tab asks for exactly the stock arms IT renders, and the backend honours
  // the narrowing strictly -- an arm not named here comes back empty, with no
  // error. Consumption (overview) is the ONLY tab that builds itemMoney, so
  // item_expenditure belongs HERE: without it the spend-share pie has no slices
  // and hides itself, every per-item card falls back to its unpriced state, and
  // the spend ranking collapses because rankItemCards sorts on a money total
  // that is null for every feed. The Stock tab never reads item_expenditure and
  // must not pay for it.
  const stockSections =
    tab === "overview"
      ? "expenditure,spend,item_expenditure"
      : "items,farm_items,forecast";
  // The overview's "Feed by pen" charts read their OWN last-7-days window
  // (ending yesterday, the page's stated basis), independent of the range
  // chips — the maintainer asked for 7 days there while the charts above
  // default to 30. Its farm/pen-name filters run client-side over the served
  // bounded pen set, like the completion table's narrowing.
  // Feed follow-up reads its own endpoint and nothing else; it keeps the page's
  // range chips, because "did the sheet react" is asked over a period.
  const wantFollowUp = !stockOnly && tab === "followup";
  const wantShedFeed = tab === "overview";
  const shedFeedTo = istDayPlus(todayIso(), -1);
  const shedFeedWindow = { date_from: istDayPlus(shedFeedTo, -6), date_to: shedFeedTo };
  const [locations, directed, execution, experiment, stock, shedFeed, loads, followUp] = await Promise.all([
    wantExperiment ? getCensusLocations() : Promise.resolve({ parks: [] as { id: string; code: string | null; name: string }[], sheds: [] }),
    wantDirected
      ? getFeedAnalyticsDirected({ ...chartParams, sections: directedSections })
      : Promise.resolve<ApiResult<FeedAnalyticsDirectedResponse> | null>(null),
    wantExecution
      ? getFeedAnalyticsExecution({
          ...params,
          // Overview reads ONLY the adherence KPI off `days`, so it asks for that arm alone rather
          // than paying for the mismatch, consumption and completion queries it never renders.
          ...(tab === "overview" ? { sections: "days" } : {}),
          ...(tab === "execution" ? { sections: "days,consumption,distribution_completions" } : {}),
          variance_limit: String(varianceLimit),
          variance_offset: String(varianceOffset),
          variance_park_label: favPark,
          variance_feed_item_key: favItem,
          completion_day: readCompletionDay(searchParams),
          completion_limit: String(completionLimit),
          completion_offset: String(completionOffset),
          completion_park_id: completionParkFilter,
          completion_shed_id: completionShedFilter,
          completion_status: completionStatusFilter,
        })
      : Promise.resolve<ApiResult<FeedAnalyticsExecutionResponse> | null>(null),
    wantExperiment
      ? getFeedAnalyticsExperiment({ ...params, park_id: experimentParkId, wastage_day: readWastageDay(searchParams) })
      : Promise.resolve<ApiResult<FeedAnalyticsExperimentResponse> | null>(null),
    wantStock
      ? getFeedAnalyticsStock({ ...chartParams, sections: stockSections })
      : Promise.resolve<ApiResult<FeedAnalyticsStockResponse> | null>(null),
    wantShedFeed
      ? getFeedAnalyticsShedFeed({ park_id: parkId, ...shedFeedWindow })
      : Promise.resolve<ApiResult<FeedAnalyticsShedFeedResponse> | null>(null),
    wantLoads
      ? getFeedAnalyticsStockLoads({
          park_id: parkId,
          farm: loadsFarm,
          feed_item_key: loadsItem,
          limit: String(loadsLimit),
          offset: String(loadsOffset),
        })
      : Promise.resolve<ApiResult<FeedAnalyticsStockLoadsResponse> | null>(null),
    wantFollowUp
      ? getFeedAnalyticsFollowUp(chartParams)
      : Promise.resolve<ApiResult<FeedAnalyticsFollowUpResponse> | null>(null),
  ]);
  if (!favDay && tab === "execution" && execution?.ok) {
    const measured = execution.data.consumption_trend
      .filter((d) => d.actual_kg !== "")
      .map((d) => d.packing_day)
      .sort();
    if (measured.length > 0) variancePackingDay = measured[measured.length - 1];
  }
  const executionDay =
    tab === "execution"
      ? await getFeedAnalyticsExecution({
          park_id: parkId,
          date_from: istDayPlus(variancePackingDay, 1),
          date_to: istDayPlus(variancePackingDay, 1),
          sections: "packing_variance",
          variance_limit: String(varianceLimit),
          variance_offset: String(varianceOffset),
          variance_park_label: favPark,
          variance_feed_item_key: favItem,
        })
      : null;
  // The day-pinned execution read for the mismatch table's calendar runs after the
  // main batch so it does not overlap the rolling-window execution read. Only its
  // packing_variance is used; the tab's charts keep the page's rolling window.
  // fav_day is a PACKING day (maintainer decision 2026-08-24, the axis Feed Packing already
  // browses by): a packer works day P on the sheet the animals eat on P+1, so a reader asking for
  // "yesterday's packing" means the feed day after it. The endpoint still keys on the feed day --
  // this is a relabel of the axis, not a second grain.
  const nonNull = [directed, execution, experiment, stock, shedFeed, executionDay, loads, followUp].filter((r) => r !== null);
  if (firstAuthRequiredError(...nonNull)) redirect(INTERNAL_LOGIN_PATH);

  // For the full feed analytics page, stock is supporting context and should not blank the
  // charts. For a stock-only page, it is the page, so failures must be visible.
  // Follow-up IS the tab it serves, so its failure must blank the tab rather
  // than leave an empty page reading as "nothing happened".
  // On the Stock tab the stock read IS the tab, so its failure shows as an error rather than as
  // the "no purchase ledger yet" empty state.
  const gated = [directed, execution, experiment, shedFeed, loads, followUp, tab === "execution" ? executionDay : null, stockOnly || tab === "items" ? stock : null];
  const failed = gated.some((r) => r !== null && !r.ok);
  const failedError = gated.find((r) => r !== null && !r.ok)?.error;

  // Tab count chip and the export payload, both taken from the rows THIS request already read.
  // A tab whose payload was not fetched on this request carries no chip: the tile would otherwise
  // be a number nobody measured.
  const tabCount =
    tab === "execution"
      ? (execution?.ok ? listOrEmpty(execution.data.days).length : undefined)
      : tab === "experiment"
        ? (experiment?.ok ? listOrEmpty(experiment.data.items).length : undefined)
        : (directed?.ok ? listOrEmpty(directed.data.days).length : undefined);
  const exportRows: (string | number)[][] = directed?.ok
    ? [
        [fa(pageContract, "col.variance.day"), fa(pageContract, "col.variance.item"), fa(pageContract, "unit.kg")],
        ...directed.data.items.map((item) => [item.feed_day, item.feed_item_label, item.directed_kg ?? ""]),
      ]
    : [];

  return (
    <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      <FeedFaroView routeId={pageContract.route_id} parkId={parkId} />

      <Box>
        <PageHeader
          title={pageContract.title}
          crumbs={[{ label: fa(pageContract, "crumb") }, { label: fa(pageContract, "section.analytics.title") }]}
          actions={<FeedAnalyticsExport rows={exportRows} filename={`${pageContract.route_id}-${range}`} label={fa(pageContract, "action.export")} />}
        />
      </Box>

      {/* Stock-only callers (the page narrowed to its Stock tab) still need a way between the
          tabs they are allowed; the full page carries its strip inside the !stockOnly block. */}
      {stockOnly && allowedTabs.length > 1 ? (
        <Stack direction="row" sx={{ flexWrap: "wrap", gap: 1 }}>
          <SegmentedLinks
            current={tab}
            options={allowedTabs.map((t) => ({
              value: t,
              label: fa(pageContract, `tab.${t}`),
              href: hrefWith(searchParams, { tab: t === allowedTabs[0] ? undefined : t }),
            }))}
          />
        </Stack>
      ) : null}

      {!stockOnly ? (
        <>
          <Stack spacing={2}>
            {/* URL-driven tabs: each tab is a distinct server read, so the segment is a real link
                (AnimatedTabs' `href` mode) rather than client state. The sliding underline and the
                count chip are the kit's; the count is shown only for the tab whose payload this
                render actually fetched — the others are not read on this request, and a badge
                invented for them would be a number nobody measured. */}
            <AnimatedTabs
              value={tab}
              ariaLabel={fa(pageContract, "range.aria")}
              items={allowedTabs.map((t) => ({
                value: t,
                label: fa(pageContract, `tab.${t}`),
                count: t === tab ? tabCount : undefined,
                href: hrefWith(searchParams, { tab: t === "overview" ? undefined : t, fc_view: undefined }),
              }))}
            />
            {/* Window as filter chips: the module strip above is the page's ONE tab component. */}
            <Stack direction="row" role="group" aria-label={fa(pageContract, "range.aria")} sx={{ flexWrap: "wrap", gap: 1, alignItems: "center" }}>
              {RANGES.map((r) => (
                <FilterChip key={r} href={hrefWith(searchParams, { range: r === "30" ? undefined : r })} on={range === r} label={fa(pageContract, `range.${r}`)} />
              ))}
              {/* The DIRECTED-not-consumed caveat rides an info hint beside the window, not a
                  paragraph under the title (design system: no prose under titles). */}
              <InfoHint text={fa(pageContract, "banner.basis")} />
              {/* One toolbar under the tabs (MUI): the Consumption view toggle rides the same row
                  as the window chips instead of a third stacked row. */}
              {tab === "overview" ? (
                <Box sx={{ ml: { sm: "auto" } }}>
                  <LocalViewToggle
                    param="fc_view"
                    current={consumptionView}
                    defaultValue="general"
                    ariaLabel={fa(pageContract, "consumption.view.aria")}
                    options={CONSUMPTION_VIEWS.map((v) => ({ value: v, label: fa(pageContract, `consumption.view.${v}`) }))}
                  />
                </Box>
              ) : null}
            </Stack>
          </Stack>
        </>
      ) : null}

      {failed ? (
        <Alert severity="error" variant="outlined">
          <AlertTitle>{fa(pageContract, "error.title")}</AlertTitle>
          {failedError?.message || fa(pageContract, "error.body")}
        </Alert>
      ) : null}

      {tab === "items" && !failed ? (
        <StockCards stock={stock?.ok ? stock.data : null} pageContract={pageContract} />
      ) : null}

      {!stockOnly && tab === "followup" && followUp?.ok ? (
        <>
          <FeedFollowUpTab
            data={followUp.data}
            pageContract={pageContract}
            page={{ offset: followUpOffset, limit: followUpLimit }}
          />
          {followUpLineCount(followUp.data) > 0 ? (
            <FeedPager
              pageContract={pageContract}
              offset={followUpOffset}
              limit={followUpLimit}
              rowCount={Math.max(0, Math.min(followUpLimit, followUpLineCount(followUp.data) - followUpOffset))}
              hasMore={followUpOffset + followUpLimit < followUpLineCount(followUp.data)}
              noun={fa(pageContract, "followup.noun")}
              pageSizeOptions={followUpPageSizes}
              hrefForOffset={(next) => feedHref(PAGE_PATH, searchParams, "ffu_offset", next === 0 ? "" : String(next))}
              hrefForLimit={(next) => feedHref(PAGE_PATH, searchParams, "ffu_limit", String(next))}
            />
          ) : null}
        </>
      ) : null}

      {!stockOnly && tab === "overview" && directed?.ok ? (
        <LocalViewPane param="fc_view" value="status" current={consumptionView}>
          <FeedStatusWise data={directed.data} pageContract={pageContract} />
        </LocalViewPane>
      ) : null}

      <LocalViewPane param="fc_view" value="general" current={tab === "overview" ? consumptionView : "general"}>
      {!stockOnly && directed?.ok && (tab === "overview" || tab === "peranimal") ? (
        <DirectedTabs
          tab={tab}
          range={range}
          data={directed.data}
          execution={execution?.ok ? execution.data : null}
          stock={stock?.ok ? stock.data : null}
          stockOnly={stockOnly}
          spendMode={spendMode}
          pageContract={pageContract}
        />
      ) : null}

      {tab === "overview" && shedFeed?.ok ? (
        <FeedShedFeedCharts
          data={shedFeed.data}
          basePath={PAGE_PATH}
          pageContract={pageContract}
          parkScopeLocked={Boolean(parkId)}
          filters={{
            park: parkId || (one(searchParams, "fsf_park") ?? ""),
            shed: one(searchParams, "fsf_shed") ?? "",
          }}
        />
      ) : null}
      </LocalViewPane>

      {tab === "execution" && execution?.ok ? (
        <ExecutionTab
          data={execution.data}
          pageContract={pageContract}
          variance={{
            rows: (executionDay?.ok ? executionDay.data : execution.data).packing_variance,
            hasMore: (executionDay?.ok ? executionDay.data : execution.data).packing_variance_has_more,
            day: variancePackingDay,
            park: favPark,
            item: favItem,
            limit: varianceLimit,
            offset: varianceOffset,
            pageSizes: variancePageSizes,
            searchParams,
          }}
          completion={{
            searchParams,
            parkScopeLocked: Boolean(parkId),
            park: completionParkFilter,
            shed: completionShedFilter,
            status: completionStatusFilter,
            selectedRowId: one(searchParams, "fdc_row") ?? "",
            offset: completionOffset,
            limit: completionLimit,
            pageSizes: completionPageSizes,
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
            // The farm by its CODE (CBE, CPT), in code order: the spelling and order every other
            // table on this page uses. The full name sorted Channapatna ahead of Coimbatore.
            options: locations.parks
              .map((park) => ({ value: park.id, label: park.code || park.name }))
              .sort((a, b) => a.label.localeCompare(b.label)),
          }}
        />
      ) : null}
      {tab === "items" && loads?.ok ? (
        <FeedStockLoadsTable
          data={loads.data}
          pageContract={pageContract}
          basePath={PAGE_PATH}
          searchParams={searchParams}
          filters={{ farm: loadsFarm, item: loadsItem, limit: loadsLimit, offset: loadsOffset, pageSizes: loadsPageSizes }}
        />
      ) : null}
    </Stack>
  );
}

// ---------------------------------------------------------------------------
// Status-wise: per PEN TAG over the whole window, rupees spent per day and kg per animal per day.
// Every figure is the backend's. Mixed-tag pens ("F2-Male + K3") are left out on the backend's
// flag: the maintainer asked for the single-status categories only (2026-09-17).
// ---------------------------------------------------------------------------

// Template chart-card anatomy (AnalyticsWebsiteVisits / BankingBalanceStatistics): Card, CardHeader
// with title + subheader, the reading figures as the template ChartLegends value row, then the chart.
function ChartCard({
  title,
  subheader,
  figures,
  children,
}: {
  title: string;
  subheader?: string;
  figures: { value: string; label?: string }[];
  children: React.ReactNode;
}) {
  return (
    <Card sx={{ height: 1 }}>
      <CardHeader title={title} subheader={subheader} />
      <Stack direction="row" sx={{ px: 3, pt: 2, gap: 3, flexWrap: "wrap" }}>
        {figures.map((figure, index) => (
          <Box key={index}>
            <Typography variant="h6">{figure.value}</Typography>
            {figure.label ? (
              <Typography variant="caption" sx={{ color: "text.secondary" }}>
                {figure.label}
              </Typography>
            ) : null}
          </Box>
        ))}
      </Stack>
      <Box sx={{ pl: 1, py: 2.5, pr: 2.5 }}>{children}</Box>
    </Card>
  );
}

function FeedStatusWise({
  data,
  pageContract,
}: {
  data: FeedAnalyticsDirectedResponse;
  pageContract: AdminUiPageContract;
}) {
  const single = data.pen_tags.filter((t) => !t.mixed);
  if (single.length === 0) {
    return (
      <Card>
        <EmptyContent title={fa(pageContract, "empty.title")} description={fa(pageContract, "status.empty")} />
      </Card>
    );
  }
  // The shared day axis is the served window, so every card's line starts and ends on the same
  // days and a day a stage was not fed draws a gap rather than being squeezed out.
  const dayLabels: string[] = [];
  for (let day = data.date_from; day <= data.date_to; day = istDayPlus(day, 1)) dayLabels.push(day);
  const kgNoun = fa(pageContract, "status.kg_per_animal");
  const rupeeNoun = fa(pageContract, "unit.rupees");
  return (
    <>
      {/* One card per stage in the Feed Items card anatomy (maintainer request 2026-09-17): the two
          figures on top, and the line below shows how the stage has run across the range. */}
      <Grid container spacing={3}>
        {single.map((row, index) => {
          const byDay = new Map(row.days.map((d) => [d.feed_day, d]));
          const priced = row.days.some((d) => d.rupees !== "");
          const colorVar = seriesColorVar(index);
          const kgSeries: LineSeries = {
            label: fa(pageContract, "status.series.kg"),
            colorVar,
            points: dayLabels.map((day) => {
              const point = byDay.get(day);
              return point && point.per_head_kg !== "" ? num(point.per_head_kg) : null;
            }),
          };
          const animals = row.avg_animals === "" ? "—" : nf(num(row.avg_animals));
          return (
            <Grid key={row.pen_tag_key} size={{ xs: 12, md: 6 }}>
            <ChartCard
              title={stageLabel(row.pen_tag_label)}
              subheader={fa(pageContract, "status.chart.cap").replace("{count}", animals)}
              figures={[
                { value: row.rupees_per_day === "" ? "—" : `₹${money(num(row.rupees_per_day))}`, label: fa(pageContract, "status.spend_per_day") },
                { value: row.per_head_kg === "" ? "—" : rate(num(row.per_head_kg)), label: kgNoun },
              ]}
            >
              {priced ? (
                <FeedLines
                  hideZeroInTip
                  series={[
                    {
                      label: fa(pageContract, "status.series.spend"),
                      colorVar,
                      points: dayLabels.map((day) => {
                        const point = byDay.get(day);
                        return point && point.rupees !== "" ? num(point.rupees) : null;
                      }),
                    },
                  ]}
                  secondary={{ series: kgSeries, valueNoun: kgNoun }}
                  dayLabels={dayLabels}
                  valueNoun={rupeeNoun}
                  chartLabel={stageLabel(row.pen_tag_label)}
                  emptyLabel={fa(pageContract, "status.empty")}
                />
              ) : (
                <FeedLines
                  hideZeroInTip
                  series={[kgSeries]}
                  dayLabels={dayLabels}
                  valueNoun={kgNoun}
                  chartLabel={stageLabel(row.pen_tag_label)}
                  emptyLabel={fa(pageContract, "status.empty")}
                />
              )}
            </ChartCard>
            </Grid>
          );
        })}
      </Grid>
    </>
  );
}

// ---------------------------------------------------------------------------
// Per-item money view (drawing shape only): the stock read's priced series,
// indexed to the directed chart's day axis so each feed card can put ₹ per
// day beside kg per day on one x-axis.
// ---------------------------------------------------------------------------

type ItemMoney = {
  /** ₹ per day slot, null where no load rate priced that day. */
  rupees: (number | null)[];
  rupeesTotal: number;
  /** The kg the rupees were priced from — the rate's honest denominator. */
  pricedKg: number;
  pricedDays: number;
};

function buildItemMoney(stock: FeedAnalyticsStockResponse | null, dayKeys: string[]): Map<string, ItemMoney> {
  const out = new Map<string, ItemMoney>();
  if (!stock) return out;
  const slot = new Map<string, number>();
  dayKeys.forEach((day, i) => slot.set(day, i));
  for (const row of stock.item_expenditure ?? []) {
    const i = slot.get(row.feed_day);
    if (i === undefined) continue;
    let m = out.get(row.feed_item_key);
    if (!m) {
      m = { rupees: new Array<number | null>(dayKeys.length).fill(null), rupeesTotal: 0, pricedKg: 0, pricedDays: 0 };
      out.set(row.feed_item_key, m);
    }
    const rupees = num(row.rupees);
    m.rupees[i] = (m.rupees[i] ?? 0) + rupees;
    m.rupeesTotal += rupees;
    m.pricedKg += num(row.directed_kg);
    m.pricedDays += 1;
  }
  return out;
}

/** Money first: priced feeds by window ₹ descending, then unpriced feeds in their kg rank. */
/**
 * The per-item cards' hidden-feed rule, shared with the pie above it: exact names, and an EMPTY
 * rule hides nothing.
 */
function itemCardHidden(rule: string, label: string): boolean {
  const names = rule.split(",").map((s) => s.trim().toLowerCase()).filter(Boolean);
  return names.includes(label.trim().toLowerCase());
}

/**
 * Keep each slice's own item colour, but never two slices on one hue (maintainer request
 * 2026-09-14). The item palette is ranked across EVERY feed and wraps past nine, so a feed ranked
 * tenth comes back as a darker shade of the first -- Mesha Adult Concentrate sat next to Dry Masoor
 * Bhusa in near-identical green. A slice whose base colour an earlier slice already holds takes the
 * next palette colour no slice is using; a pie of three feeds is then three plainly different
 * colours while the line charts, which never draw two feeds this close, keep their colours as is.
 */
function distinctSliceColors(slices: PieSlice[]): PieSlice[] {
  // The HUE, not the token: `--brand-d` / `--brand-l` are the brand green darkened and
  // lightened, and a colour-mix of a token is that token shaded -- all the same hue to a reader.
  // A blend of TWO chart tokens (the palette's indigo/orange/lime/pink/cyan/mint entries) is its
  // own hue, so it is keyed by the whole expression.
  const hueOf = (colorVar: string) => {
    const tokens = colorVar.match(/var\(--[a-z]+(?:-[a-z]+)*\)/g) ?? [];
    const chartTokens = tokens.filter((t) => !/--(ink|panel)\)/.test(t));
    if (chartTokens.length > 1) return colorVar;
    return (tokens[0] ?? colorVar).replace(/-[dl]\)$/, ")");
  };
  const used = new Set<string>();
  return slices.map((slice) => {
    let colorVar = slice.colorVar;
    if (used.has(hueOf(colorVar))) {
      colorVar = FEED_SERIES_VARS.find((candidate) => !used.has(hueOf(candidate))) ?? colorVar;
    }
    used.add(hueOf(colorVar));
    return { ...slice, colorVar };
  });
}

function rankItemCards(
  itemSeries: ItemLineSeries[],
  itemMoney: Map<string, ItemMoney>,
): { series: ItemLineSeries; money: ItemMoney | null }[] {
  return itemSeries
    .map((series) => ({ series, money: itemMoney.get(series.key) ?? null }))
    .sort((a, b) => (b.money?.rupeesTotal ?? -1) - (a.money?.rupeesTotal ?? -1));
}

function DirectedTabs({
  tab,
  range,
  spendMode,
  data,
  execution,
  stock,
  stockOnly,
  pageContract,
}: {
  tab: Tab;
  range: Range;
  spendMode: SpendMode;
  data: FeedAnalyticsDirectedResponse;
  execution: FeedAnalyticsExecutionResponse | null;
  stock: FeedAnalyticsStockResponse | null;
  stockOnly: boolean;
  pageContract: AdminUiPageContract;
}) {
  const view = buildDirectedView(data, fa(pageContract, "series.other"), istDayPlus(todayIso(), -1));
  const itemMoney = tab === "overview" ? buildItemMoney(stock, view.dayLabels) : new Map<string, ItemMoney>();
  // The pie's slices: EVERY feed with spend in the window, less the same hidden feeds the cards
  // below leave out (maintainer request 2026-09-23) — the pie and the cards under it show one set
  // of feeds, so UHT Milk and any feed the farm starts buying get a slice with no list to widen.
  // Computed here so the section is gated on what the pie would actually show — an empty pie is
  // hidden, not captioned with the directed-feed empty copy, which would say the wrong thing.
  const spendShareSlices: PieSlice[] = distinctSliceColors(
    rankItemCards(view.itemSeries, itemMoney).flatMap(({ series, money }) =>
      money && money.pricedDays > 0 && !itemCardHidden(fa(pageContract, "chart.item.hidden_feeds"), series.label)
        ? [{ label: series.label, value: money.rupeesTotal / money.pricedDays, colorVar: series.colorVar }]
        : [],
    ),
  );
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
      <Card>
        <EmptyContent title={noData} description={fa(pageContract, "empty.body")} />
      </Card>
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
  // Feed cost per animal per day (maintainer ask 2026-09-07): yesterday's feed expenditure
  // over the animals fed yesterday. Both halves are backend numbers already on this page —
  // the priced expenditure day and the sheet's head count — matched on the SAME settled
  // business day the other tiles describe, and divided for display only, the way the
  // adherence share above is. A day with no priced expenditure or no animals reads "—"
  // rather than ₹0: an unpriced sheet is not a free one.
  let costPerAnimal: string | null = null;
  if (latest && stock && latest.head_days > 0) {
    const spentDay = stock.expenditure.find((d) => d.feed_day === latest.feed_day);
    if (spentDay) costPerAnimal = `₹${rate(num(spentDay.rupees) / latest.head_days)}`;
  }

  // KPI sparklines: the last 14 served feed days of the SAME daily figures the tiles headline, and
  // the trend chip is the settled day against the day before it -- both backend figures already on
  // the page, compared for display only. Fewer than two days: no spark, no chip.
  // The series runs through today, which the farm is still feeding; the sparklines stop at the
  // settled day the tiles describe, so the last point is never a half-issued sheet.
  const recentDays = data.days.filter((d) => view.settledDay >= d.feed_day).slice(-14);
  // A trailing day still being recorded (missing, zero, or under 60% of the trailing 7-day
  // median) is dropped by completeDaySeries, so the line ends at the last complete day.
  // The day's DIRECTED total decides whether it is complete; every tile's line then ends on that
  // same day, so a half-issued day cannot leave a per-head or head-count point behind either.
  const completeDays = completeDaySeries(recentDays.map((d) => num(d.directed_kg)))?.length ?? 0;
  const sparkDays = recentDays.slice(0, completeDays);
  const daySpark = (pick: (d: (typeof data.days)[number]) => number | null) => completeDaySeries(sparkDays.map(pick));
  const dayTrend = (pick: (d: (typeof data.days)[number]) => number | null) => {
    if (!latest) return undefined;
    const i = data.days.findIndex((d) => d.feed_day === latest.feed_day);
    const prev = i > 0 ? pick(data.days[i - 1]) : null;
    const cur = pick(latest);
    if (prev === null || cur === null || prev === 0) return undefined;
    return Math.round(((cur - prev) / prev) * 1000) / 10;
  };
  const directedOf = (d: (typeof data.days)[number]) => num(d.directed_kg);
  const headOf = (d: (typeof data.days)[number]) => d.head_days;
  const perHeadOf = (d: (typeof data.days)[number]) => (d.per_head_grams === "" ? null : num(d.per_head_grams));
  // Template widget colours: [light, main] of the palette key (EcommerceWidgetSummary default).
  const sparkChart = (series: number[] | null | undefined, color: string) => ({
    categories: [],
    series: series ?? [],
    colors: [`var(--palette-${color}-light)`, `var(--palette-${color}-main)`],
  });

  return (
    <>
      {coverageNote !== null ? (
        <RangeCoverageNote key={`${range}-${coveredDays}`} message={coverageNote} />
      ) : null}
      {tab === "overview" ? (
        // KPI row: template EcommerceWidgetSummary (overview/e-commerce), three per row.
        <Grid container spacing={3} component="section" aria-label={fa(pageContract, "chart.daily.title")}>
          {[
            { key: "directed", total: latest ? `${nf(num(latest.directed_kg))} ${fa(pageContract, "unit.kg")}` : "—", pick: directedOf, color: "primary" },
            { key: "head_days", total: latest ? nf(latest.head_days) : "—", pick: headOf, color: "info" },
            { key: "per_head", total: latest && latest.per_head_grams !== "" ? `${nf(num(latest.per_head_grams))} g` : "—", pick: perHeadOf, color: "warning" },
          ].map((kpi) => (
            <Grid key={kpi.key} size={{ xs: 12, sm: 6, md: 4 }}>
              <EcommerceWidgetSummary
                title={fa(pageContract, `kpi.${kpi.key}.label`)}
                total={kpi.total}
                percent={dayTrend(kpi.pick)}
                caption={fa(pageContract, `kpi.${kpi.key}.sub`)}
                chart={sparkChart(daySpark(kpi.pick), kpi.color)}
                sx={{ height: 1 }}
              />
            </Grid>
          ))}
          <Grid size={{ xs: 12, sm: 6, md: 4 }}>
            <EcommerceWidgetSummary title={fa(pageContract, "kpi.adherence.label")} total={adherence ?? "—"} caption={fa(pageContract, "kpi.adherence.sub")} chart={NO_SPARK} sx={{ height: 1 }} />
          </Grid>
          <Grid size={{ xs: 12, sm: 6, md: 4 }}>
            <EcommerceWidgetSummary title={fa(pageContract, "kpi.cost_per_animal.label")} total={costPerAnimal ?? "—"} caption={fa(pageContract, "kpi.cost_per_animal.sub")} chart={NO_SPARK} sx={{ height: 1 }} />
          </Grid>
        </Grid>
      ) : null}

      {tab === "overview" ? (
        // Stacked columns in the template chart card anatomy (Card + CardHeader + legend + chart,
        // BankingBalanceStatistics); the Apex marks are the shared series chart.
        <Card component="section" aria-label={fa(pageContract, "chart.daily.title")}>
          <CardHeader title={fa(pageContract, "chart.daily.title")} sx={{ mb: 3 }} />
          <Box sx={{ px: 3 }}>
            <FeedChartLegend
              entries={view.itemLabels.map((label, s) => ({
                label,
                colorVar: seriesColorVar(s),
              }))}
            />
          </Box>
          <Box sx={{ pl: 1, py: 2.5, pr: 2.5 }}>
            <FeedStackedColumns
              hideZeroInTip
              days={view.stacked}
              seriesLabels={view.itemLabels}
              valueNoun={fa(pageContract, "unit.kg")}
              chartLabel={fa(pageContract, "chart.daily.title")}
              emptyLabel={fa(pageContract, "empty.body")}
            />
          </Box>
        </Card>
      ) : null}

      {tab === "overview" && stock && stock.expenditure.length > 0 ? (
        // Spend by period: four template EcommerceWidgetSummary tiles.
        <Grid container spacing={3} component="section" aria-label={fa(pageContract, "chart.spend.title")}>
          {([
            ["week", stock.spend.last_7_days],
            ["month", stock.spend.this_month],
            ["quarter", stock.spend.three_months],
            ["year", stock.spend.this_year],
          ] as const).map(([period, rupees]) => (
            <Grid key={period} size={{ xs: 12, sm: 6, md: 3 }}>
              <EcommerceWidgetSummary
                title={fa(pageContract, `spend.${period}.label`)}
                total={`₹${nf(num(rupees))}`}
                caption={fa(pageContract, `spend.${period}.sub`)}
                chart={NO_SPARK}
                sx={{ height: 1 }}
              />
            </Grid>
          ))}
        </Grid>
      ) : null}

      {tab === "overview" && stock && stock.expenditure.length > 0 ? (
        // One chart, two readings, chosen top-right (maintainer ask 2026-09-07). "Overall" is
        // the day's priced ₹ as served. "Per animal" divides EACH day's ₹ by the animals on
        // THAT day's sheet, matched on feed_day — never by position, since the expenditure
        // series and the directed days can start on different dates. A day whose sheet has
        // no animals has no per-animal figure and breaks the line rather than reading ₹0;
        // the tile above divides the same two halves for yesterday alone.
        // Template EcommerceYearlySales anatomy: CardHeader with the reading picker as its action.
        <Card component="section" aria-label={fa(pageContract, "chart.spend.title")}>
          <CardHeader
            title={fa(pageContract, "chart.spend.title")}
            subheader={SPEND_MODES.map((mode) => (
              <LocalViewPane key={mode} param="spend" value={mode} current={spendMode}>
                {fa(pageContract, mode === "per_animal" ? "chart.spend.per_animal.hint" : "chart.spend.hint")}
              </LocalViewPane>
            ))}
            action={
              <LocalViewToggle
                param="spend"
                current={spendMode}
                defaultValue="overall"
                ariaLabel={fa(pageContract, "chart.spend.mode.aria")}
                options={SPEND_MODES.map((m) => ({ value: m, label: fa(pageContract, `chart.spend.mode.${m}`) }))}
              />
            }
            sx={{ mb: 3 }}
          />
          <Box sx={{ pl: 1, py: 2.5, pr: 2.5 }}>
          {SPEND_MODES.map((mode) => (
            <LocalViewPane key={mode} param="spend" value={mode} current={spendMode}>
            <FeedLines
              hideZeroInTip
              series={[
                mode === "per_animal"
                  ? {
                      label: fa(pageContract, "chart.spend.per_animal.label"),
                      colorVar: FEED_SERIES_VARS[2],
                      points: stock.expenditure.map((d) => {
                        const day = data.days.find((x) => x.feed_day === d.feed_day);
                        return day && day.head_days > 0 ? num(d.rupees) / day.head_days : null;
                      }),
                    }
                  : {
                      label: fa(pageContract, "chart.spend.title"),
                      colorVar: FEED_SERIES_VARS[2],
                      points: stock.expenditure.map((d) => num(d.rupees)),
                    },
              ]}
              dayLabels={stock.expenditure.map((d) => d.feed_day)}
              valueNoun={fa(pageContract, mode === "per_animal" ? "unit.rupees_per_animal" : "unit.rupees")}
              chartLabel={fa(pageContract, "chart.spend.title")}
              emptyLabel={fa(pageContract, "stock.empty")}
            />
            </LocalViewPane>
          ))}
          </Box>
        </Card>
      ) : null}

      {tab === "overview" ? (
        // Template e-commerce row: the share donut (EcommerceSaleByGender slot, lg 4) beside the
        // feed mix bars (AnalyticsConversionRates, lg 8).
        <Grid container spacing={3}>
          {spendShareSlices.length > 0 ? (
            // Where the money goes: one slice per feed at its AVERAGE ₹ per priced day, the same
            // figure the strip on each card below leads with, in each feed's own colour. Ranked by
            // spend so the biggest slice starts at twelve o'clock; an unpriced feed has no rupees
            // and so no slice — the cards below still show its kg.
            <Grid size={{ xs: 12, md: 6 }}>
              <Card component="section" aria-label={fa(pageContract, "chart.spend_share.title")} sx={{ height: 1 }}>
                <CardHeader title={fa(pageContract, "chart.spend_share.title")} />
                <Box sx={{ p: 3 }}>
                  <FeedSpendPie
                    slices={spendShareSlices}
                    valueNoun={fa(pageContract, "chart.spend_share.unit")}
                    formatValue={(v) => `₹${v.toLocaleString("en-IN", { maximumFractionDigits: 0 })}`}
                    chartLabel={fa(pageContract, "chart.spend_share.title")}
                    emptyLabel={fa(pageContract, "empty.body")}
                  />
                </Box>
              </Card>
            </Grid>
          ) : null}
          <Grid size={{ xs: 12, md: spendShareSlices.length > 0 ? 6 : 12 }}>
            {view.mix.length > 0 ? (
              <FeedMixCard title={fa(pageContract, "chart.mix.title")} unit={fa(pageContract, "unit.kg")} rows={view.mix} />
            ) : (
              <Card component="section" aria-label={fa(pageContract, "chart.mix.title")} sx={{ height: 1 }}>
                <CardHeader title={fa(pageContract, "chart.mix.title")} />
                <EmptyContent title={fa(pageContract, "empty.body")} />
              </Card>
            )}
          </Grid>
        </Grid>
      ) : null}

      {tab === "overview" ? (
        // Consumption tab (maintainer request 2026-09-04, moved off Stock): one card per feed item, each in its
        // ranked colour, over the same window, below the stock cards. MONEY
        // FIRST (maintainer request 2026-09-03): the solid line is ₹ spent per
        // day, the dashed line on the right-hand scale is the kg fed, and the
        // strip above the chart carries the per-day figures. Cards are ordered by what
        // each feed cost; a feed with no load rate yet falls to the end and
        // keeps its kg line, so it is never hidden for lack of a price.
        <Grid container spacing={3}>
          {rankItemCards(view.itemSeries, itemMoney)
            // The retired split concentrates are hidden here (maintainer request 2026-09-14): the
            // farm feeds the two merged Mesha concentrates now, and four cards of sacks running
            // down to zero drowned the feeds it actually buys. Same exact-name rule as the pie,
            // authored on the contract, so the list of hidden feeds is one line of backend copy.
            .filter(({ series }) => !itemCardHidden(fa(pageContract, "chart.item.hidden_feeds"), series.label))
            .map(({ series, money }) => {
            const fedDays = series.points.filter((p) => p !== null).length;
            const fedKg = series.points.reduce<number>((acc, p) => acc + (p ?? 0), 0);
            const kgNoun = fa(pageContract, "unit.kg");
            const rupeeNoun = fa(pageContract, "unit.rupees");
            const fedSeries: LineSeries = { label: fa(pageContract, "item.series.fed"), colorVar: series.colorVar, points: series.points };
            return (
              <Grid key={series.key} size={{ xs: 12, md: 6 }}>
                <ChartCard
                  title={series.label}
                  subheader={fa(pageContract, "chart.item.hint")}
                  figures={[
                    money
                      ? {
                          value: money.pricedDays > 0 ? `₹${(money.rupeesTotal / money.pricedDays).toLocaleString("en-IN", { maximumFractionDigits: 0 })}` : "—",
                          label: fa(pageContract, "item.spend.per_day"),
                        }
                      : { value: "—", label: fa(pageContract, "item.unpriced") },
                    { value: fedDays > 0 ? nf(fedKg / fedDays) : "—", label: fa(pageContract, "item.kg.per_day") },
                    ...(money && money.pricedKg > 0 ? [{ value: `₹${rate(money.rupeesTotal / money.pricedKg)}`, label: fa(pageContract, "item.rate") }] : []),
                  ]}
                >
                  {money ? (
                    <FeedLines
                      hideZeroInTip
                      series={[{ label: fa(pageContract, "item.series.spend"), colorVar: series.colorVar, points: money.rupees }]}
                      secondary={{ series: fedSeries, valueNoun: kgNoun }}
                      dayLabels={view.dayLabels}
                      valueNoun={rupeeNoun}
                      chartLabel={series.label}
                      emptyLabel={fa(pageContract, "empty.body")}
                    />
                  ) : (
                    <FeedLines
                      hideZeroInTip
                      series={[fedSeries]}
                      dayLabels={view.dayLabels}
                      valueNoun={kgNoun}
                      chartLabel={series.label}
                      emptyLabel={fa(pageContract, "empty.body")}
                    />
                  )}
                </ChartCard>
              </Grid>
            );
          })}
        </Grid>
      ) : null}

      {tab === "peranimal" ? (
        // One RATION CARD per feed item: the current figure a director actually
        // asks for ("how many grams is each animal getting?") big, with the
        // item's own trend on its own scale — a shared-scale multi-line let the
        // 950 g Masoor line flatten every concentrate into the baseline.
        <Grid container spacing={3}>
          {view.perHead.map((series) => {
            const lastIdx = series.points.reduce<number>((acc, point, index) => (point === null ? acc : index), -1);
            const latest = lastIdx >= 0 ? (series.points[lastIdx] as number) : null;
            return (
              <Grid key={series.label} size={{ xs: 12, md: 6 }}>
                <ChartCard
                  title={series.label}
                  subheader={fa(pageContract, "chart.perhead.hint")}
                  figures={[{ value: latest === null ? "—" : `${nf(latest)} ${fa(pageContract, "unit.g_per_head")}` }]}
                >
                  <FeedLines
                    hideZeroInTip
                    series={[series]}
                    dayLabels={view.dayLabels}
                    valueNoun={fa(pageContract, "unit.g_per_head")}
                    chartLabel={series.label}
                    emptyLabel={fa(pageContract, "empty.body")}
                  />
                </ChartCard>
              </Grid>
            );
          })}
        </Grid>
      ) : null}
    </>
  );
}

function ExecutionTab({
  data,
  pageContract,
  variance,
  completion,
}: {
  data: FeedAnalyticsExecutionResponse;
  pageContract: AdminUiPageContract;
  /** The mismatch table's own narrowing: rows (day-pinned when `day` is set) + applied filters. */
  variance: {
    rows: FeedAnalyticsExecutionResponse["packing_variance"];
    hasMore: boolean;
    day: string;
    park: string;
    item: string;
    limit: number;
    offset: number;
    pageSizes: number[];
    searchParams: RouteSearchParams;
  };
  /** The completion table's page and filters; its DAY and its rows are owned by the backend read. */
  completion: {
    searchParams: RouteSearchParams;
    parkScopeLocked: boolean;
    park: string;
    shed: string;
    status: string;
    selectedRowId: string;
    offset: number;
    limit: number;
    pageSizes: number[];
  };
}) {
  const completionTable = (
    <FeedCompletionTable
      data={data}
      searchParams={completion.searchParams}
      basePath={PAGE_PATH}
      pageContract={pageContract}
      filters={{
        park: completion.park,
        shed: completion.shed,
        status: completion.status,
        selectedRowId: completion.selectedRowId,
        offset: completion.offset,
        limit: completion.limit,
        pageSizes: completion.pageSizes,
      }}
      parkScopeLocked={completion.parkScopeLocked}
    />
  );
  // A window with no completions at all still renders the completion table, and that is the point:
  // "nobody proved anything in 30 days" is exactly the state this table was added to make visible,
  // and hiding it behind the charts' empty card would answer the question with a blank screen.
  if (data.days.length === 0) {
    return (
      <Stack spacing={3}>
        <Card>
          <EmptyContent title={fa(pageContract, "empty.title")} description={fa(pageContract, "empty.execution.body")} />
        </Card>
        {completionTable}
      </Stack>
    );
  }
  // Farm/item narrowing is applied by the backend before LIMIT/OFFSET; applying it here after
  // paging would hide matching rows that live on a later unfiltered page.
  const varianceRows = variance.rows;
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
  return (
    <>
      <Grid container spacing={3} component="section" aria-label={fa(pageContract, "chart.execution.title")}>
        {[
          { key: "packing", total: pct(packingDone, packingAll), caption: `${nf(packingDone)} / ${nf(packingAll)} · ${fa(pageContract, "kpi.packing.sub")}` },
          { key: "distribution", total: pct(distDone, distAll), caption: `${nf(distDone)} / ${nf(distAll)} · ${fa(pageContract, "kpi.distribution.sub")}` },
          { key: "transport", total: pct(transDone, transAll), caption: `${nf(transDone)} / ${nf(transAll)} · ${fa(pageContract, "kpi.transport.sub")}` },
          { key: "latency", total: latestLatency === null ? "—" : `${nf(latestLatency)} ${fa(pageContract, "unit.minutes")}`, caption: fa(pageContract, "kpi.latency.sub") },
        ].map((kpi) => (
          <Grid key={kpi.key} size={{ xs: 12, sm: 6, md: 3 }}>
            <EcommerceWidgetSummary title={fa(pageContract, `kpi.${kpi.key}.label`)} total={kpi.total} caption={kpi.caption} chart={NO_SPARK} sx={{ height: 1 }} />
          </Grid>
        ))}
      </Grid>
      <Card component="section" aria-label={fa(pageContract, "chart.execution.title")}>
        <CardHeader title={fa(pageContract, "chart.execution.title")} sx={{ mb: 3 }} />
        <Box sx={{ pl: 1, py: 2.5, pr: 2.5 }}>
          <ExecutionStacked stacked={stacked} statuses={statuses} pageContract={pageContract} />
        </Box>
      </Card>
      {/* Intended-vs-entered packing mismatches (maintainer decision 2026-08-21). The verifier
          enters her per-item readings BLIND -- this comparison exists only on this leadership
          page, never on any verifier surface. Every measured bag is listed, biggest difference
          first. The bar narrows by farm and feed item over the served rows, and the calendar
          re-reads the endpoint pinned to one business day so older values than the page window
          stay reachable. */}
      <Card component="section" aria-label={fa(pageContract, "variance.title")}>
        <CardHeader title={fa(pageContract, "variance.title")} sx={{ mb: 1 }} />
        <FeedFilters
          basePath={PAGE_PATH}
          pageParam="fav_offset"
          fields={[
            {
              kind: "select",
              param: "fav_park",
              label: fa(pageContract, "col.variance.park"),
              value: variance.park,
              allowAll: true,
              options: dedupeOptions([
                ...(variance.park ? [{ value: variance.park, label: variance.park }] : []),
                ...variance.rows.map((r) => ({ value: r.park_label, label: r.park_label })),
              ]),
            },
            {
              kind: "select",
              param: "fav_item",
              label: fa(pageContract, "col.variance.item"),
              value: variance.item,
              allowAll: true,
              options: dedupeOptions([
                ...(variance.item ? [{ value: variance.item, label: variance.item }] : []),
                ...variance.rows.map((r) => ({ value: r.feed_item_key, label: r.feed_item_label })),
              ]),
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
                aria: fa(pageContract, "variance.date.aria"),
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
          <EmptyContent title={fa(pageContract, "variance.empty")} sx={{ py: 5 }} />
        ) : (
          <TableContainer tabIndex={0} role="group" aria-label={fa(pageContract, "variance.title")}>
            <Table sx={{ minWidth: 960 }}>
              <TableHead>
                <TableRow>
                  <TableCell component="th">{fa(pageContract, "col.variance.day")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "col.variance.park")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "col.variance.pen")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "col.variance.session")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "col.variance.item")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "col.consumption.breed")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "col.variance.planned")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "col.variance.verified")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "col.variance.diff")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {varianceRows.map((row) => (
                  <TableRow key={`${row.feed_day}:${row.shed_id}:${row.partition_label ?? ""}:${row.session_no}:${row.feed_item_key}:${row.workflow}`}>
                    <TableCell>{fmtDate(row.packing_day)}</TableCell>
                    <TableCell>{row.park_label}</TableCell>
                    <TableCell>{row.operational_location_display}</TableCell>
                    <TableCell>{row.session_label || row.session_no}</TableCell>
                    <TableCell>{row.feed_item_label}</TableCell>
                    {/* Empty when the frozen sheet row behind the reading is gone -- said in words
                        rather than left blank, so the reader knows the cohort is unknown for this
                        bag rather than absent from the pen. */}
                    <TableCell>{row.breed_label || fa(pageContract, "variance.cohort_unknown")}</TableCell>
                    <TableCell>
                      {row.planned_kg === ""
                        ? fa(pageContract, "variance.planned_unknown")
                        : `${row.planned_kg} ${fa(pageContract, "unit.kg")}`}
                    </TableCell>
                    <TableCell>{`${row.verified_kg} ${fa(pageContract, "unit.kg")}`}</TableCell>
                    <TableCell>
                      {/* Two tones, on the BACKEND's judgement (maintainer decision 2026-08-24,
                          superseding the no-tolerance-flag decision made earlier the same day): a
                          bag within the packing tolerance is quiet, a bag beyond it is loud. The
                          threshold itself is never applied here — `exceeds_tolerance` arrives
                          decided, from the same constant the packed-vs-given trend counts on, so a
                          renderer cannot drift from the trend beside it. Over and under are the
                          same breach: a bag packed heavy does not match the sheet either, and the
                          arrow is what says which way it went. */}
                      <Label
                        variant="soft"
                        color={row.exceeds_tolerance ? "error" : "success"}
                        title={fa(
                          pageContract,
                          row.exceeds_tolerance ? "variance.beyond_tolerance" : "variance.within_tolerance",
                        )}
                        startIcon={num(row.variance_kg) === 0 ? undefined : (
                          <span aria-hidden="true">{num(row.variance_kg) > 0 ? "↑" : "↓"}</span>
                        )}
                      >
                        {`${nf(Math.abs(num(row.variance_kg)))} ${fa(pageContract, "unit.kg")}`}
                      </Label>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        )}
        {/* The pager sits between the rows and the trend: it belongs to the TABLE, and the graph
            below it is a whole-window aggregate that paging must never appear to move. */}
        {/* No pager under an empty day: "0 measured bags" beside Rows / Previous / Next is controls
            for nothing. It comes back as soon as the day has rows, or when paged past the end. */}
        {varianceRows.length === 0 && variance.offset === 0 ? null : (
        <FeedPager
          pageContract={pageContract}
          offset={variance.offset}
          limit={variance.limit}
          rowCount={varianceRows.length}
          hasMore={variance.hasMore}
          noun={fa(pageContract, "variance.noun")}
          pageSizeOptions={variance.pageSizes}
          hrefForOffset={(next) => feedHref(PAGE_PATH, variance.searchParams, "fav_offset", next === 0 ? "" : String(next))}
          hrefForLimit={(next) => feedHref(PAGE_PATH, variance.searchParams, "fav_limit", String(next))}
        />
        )}
        {/* The packed-vs-given trend that sat here was removed (maintainer request 2026-09-24):
            it compared the whole sheet with only the bags a verifier weighed and read as feed going
            missing. The table above answers the day; nothing below it re-plots the window. */}
      </Card>
      {/* The completion table sits UNDER everything else on this tab (maintainer ask): the charts
          answer "how is adherence trending", this answers "who did not upload yesterday". */}
      {completionTable}
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
      <FeedChartLegend entries={statuses} />
      <FeedStackedColumns
        hideZeroInTip
        days={stacked}
        seriesLabels={statuses.map((s) => s.label)}
        // The legend owns the colours: filling by position painted "Awaiting verdict" blue and
        // "Rework" amber under a legend that says amber and red.
        seriesColors={statuses.map((s) => s.colorVar)}
        valueNoun={fa(pageContract, "table.items.noun")}
        chartLabel={fa(pageContract, "chart.execution.title")}
        emptyLabel={fa(pageContract, "empty.execution.body")}
      />
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
    <Stack spacing={3}>
      {data.items.length === 0 ? (
        <Card>
          <EmptyContent title={fa(pageContract, "empty.title")} description={fa(pageContract, "empty.experiment.body")} />
        </Card>
      ) : (
        <Card component="section" aria-label={fa(pageContract, "chart.experiment.title")}>
          <CardHeader title={fa(pageContract, "chart.experiment.title")} sx={{ mb: 3 }} />
          <Box sx={{ px: 3 }}>
            <FeedChartLegend entries={series.map((s) => ({ label: s.label, colorVar: s.colorVar }))} />
          </Box>
          <Box sx={{ pl: 1, py: 2.5, pr: 2.5 }}>
            <FeedLines
              hideZeroInTip
              series={series}
              dayLabels={dayKeys}
              valueNoun={fa(pageContract, "unit.kg")}
              chartLabel={fa(pageContract, "chart.experiment.title")}
              emptyLabel={fa(pageContract, "empty.experiment.body")}
            />
          </Box>
        </Card>
      )}
      <Card component="section" aria-label={fa(pageContract, "wastage.title")}>
        <CardHeader title={fa(pageContract, "wastage.title")} sx={{ mb: 1 }} />
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
          <EmptyContent title={fa(pageContract, "wastage.empty")} sx={{ py: 5 }} />
        ) : (
          <TableContainer tabIndex={0} role="group" aria-label={fa(pageContract, "wastage.title")}>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell component="th">{fa(pageContract, "col.wastage.park")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "col.wastage.pen")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "col.wastage.kg")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "col.wastage.status")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {data.wastage_pens.map((row) => (
                  <TableRow key={`${row.shed_id}|${row.partition_label}`}>
                    <TableCell>{row.park_label}</TableCell>
                    <TableCell>{row.operational_location_display}</TableCell>
                    <TableCell>{row.wastage_kg === "" ? "—" : nf(num(row.wastage_kg))}</TableCell>
                    <TableCell>{wastageStatus(row)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </Card>
    </Stack>
  );
}



// TEMPORARY (maintainer request 2026-09-06): hide a concentrate stock card --
// the Mesha adult/kids x goat/sheep feeds, plus the farm's plain "Concentrate"
// -- but ONLY when it reads zero days left. The same feed with stock still on
// it stays on screen, which is why this is decided per farm and not per feed. This is
// a frontend-only hide with no backend or contract change, and it is meant to
// be deleted: drop this function and the .filter() call in StockCards to bring
// the cards straight back.
//
// A plain "Mesha Adult Concentrate" or "Mesha Kids Concentrate" -- no species in
// the name -- always keeps its card, zero days or not. That carve-out is the
// reason this is a named list rather than "any concentrate": widening it would
// swallow those two.
function isHideableConcentrate(feedItemLabel: string): boolean {
  const label = (feedItemLabel ?? "").trim().toLowerCase();
  // The farm's own unbranded feed, hidden per park (maintainer request
  // 2026-09-06): CBE reads zero days and goes, CPT still has days and stays.
  if (label === "concentrate") return true;
  return (
    label.includes("mesha") &&
    label.includes("concentrate") &&
    (label.includes("goat") || label.includes("sheep"))
  );
}

// Catalog keys whose stock CARD is dropped outright, days left or not
// (maintainer instruction 2026-09-07). The farm's plain "Concentrate" is not one
// of its concentrates -- those are the Mesha Adult / Kids families, which keep
// their cards -- so its card sat beside them reading a runway nobody uses (CPT
// still showed 65 days after the zero-days hide above). Keyed on the catalog KEY,
// never on the numbers, so a not-started or low-stock card can never be hidden
// by it. Card grid only: the per-farm table and forecast keep the zero-days rule.
const ALWAYS_HIDDEN_STOCK_CARD_KEYS = new Set<string>(["concentrate"]);

function isTemporarilyHiddenStockItem(item: {
  feed_item_key: string;
  feed_item_label: string;
  days_left?: number | null;
}): boolean {
  if (ALWAYS_HIDDEN_STOCK_CARD_KEYS.has(item.feed_item_key)) return true;
  if (!isHideableConcentrate(item.feed_item_label)) return false;
  return item.days_left !== null && item.days_left !== undefined && item.days_left <= 0;
}

// TEMPORARY companion to isTemporarilyHiddenStockItem, same maintainer request
// and same rule one grain over: a Mesha concentrate ROW of the per-farm table
// drops when it reads zero days left. The table has no served days_left, so it
// is recomputed exactly as DaysLeftText renders it -- stock over the recent
// daily average, floored at zero -- and a row whose average is unavailable has
// no days figure at all, so it is kept. The species-less "Mesha Adult
// Concentrate" is never hidden, matching the card rule. Delete this function
// and the two .filter() calls above to bring the rows back.
//
// The next-7-days forecast table applies the SAME rule off its own stock and
// average columns, so its week total counts exactly the rows it lists -- a
// total over hidden rows would not add up on screen.
function isTemporarilyHiddenFarmItem(feedItemLabel: string, avgDailyKg: string, stockKg: string): boolean {
  if (!isHideableConcentrate(feedItemLabel)) return false;
  const avg = num(avgDailyKg);
  if (avg <= 0 || stockKg === "") return false;
  return Math.max(Math.floor(num(stockKg) / avg), 0) <= 0;
}

function StockCards({
  stock,
  pageContract,
}: {
  stock: FeedAnalyticsStockResponse | null;
  pageContract: AdminUiPageContract;
}) {
  // Every served row is a card (maintainer decision 2026-09-06). The backend
  // already scopes items to the farm's ACTIVE feed vocabulary, so the wall of
  // "not directed recently" boxes the 2026-08-18 filter removed is gone at the
  // source -- and dropping rows here instead hid two things that matter: a
  // freshly bought load nobody has started feeding, and a retired feed whose
  // frozen burn rate kept it on screen. Both are the backend's call now.
  const active = (stock?.items ?? []).filter((item) => !isTemporarilyHiddenStockItem(item));
  const farmItems = (stock?.farm_items ?? []).filter((row) => !isTemporarilyHiddenFarmItem(row.feed_item_label, row.avg_daily_kg, row.ledger_stock_kg));
  const forecast = (stock?.forecast ?? []).filter((row) => !isTemporarilyHiddenFarmItem(row.feed_item_label, row.avg_daily_kg, row.stock_kg));
  return (
    <>
      {!stock || active.length === 0 ? (
        <Card>
          <CardHeader title={fa(pageContract, "stock.title")} />
          <EmptyContent title={fa(pageContract, "stock.empty")} sx={{ py: 5 }} />
        </Card>
      ) : (
        // One template EcommerceWidgetSummary per farm × feed: days left big, balance + batch below.
        <Box component="section" aria-label={fa(pageContract, "stock.title")}>
          <Typography variant="h6" sx={{ mb: 2 }}>{fa(pageContract, "stock.title")}</Typography>
          <Grid container spacing={3}>
            {active.map((item) => (
              <Grid key={`${item.farm_label}|${item.feed_item_key}`} size={{ xs: 12, sm: 6, md: 4 }}>
                <EcommerceWidgetSummary
                  title={`${item.farm_label} · ${item.feed_item_label}`}
                  total={
                    item.not_started
                      ? `${nf(num(item.balance_kg))} ${fa(pageContract, "unit.kg")}`
                      : item.days_left === null || item.days_left === undefined
                        ? fa(pageContract, "stock.never_directed")
                        : `${nf(Math.max(item.days_left, 0))} ${fa(pageContract, "stock.days_left")}`
                  }
                  caption={
                    <>
                      {item.not_started ? <Label variant="soft" color="success" sx={{ mr: 0.5 }}>{fa(pageContract, "stock.not_started")}</Label> : null}
                      {item.low_stock ? <Label variant="soft" color="error" sx={{ mr: 0.5 }}>{fa(pageContract, "stock.low")}</Label> : null}
                      {item.not_started
                        ? `${fa(pageContract, "stock.not_started_sub")} · ${fa(pageContract, "stock.batch")} ${item.latest_batch_no}`
                        : `${nf(num(item.balance_kg))} ${fa(pageContract, "stock.balance")}${
                            item.avg_daily_kg ? ` · ${nf(num(item.avg_daily_kg))} ${fa(pageContract, "stock.per_day")}` : ""
                          } · ${fa(pageContract, "stock.batch")} ${item.latest_batch_no}`}
                    </>
                  }
                  chart={NO_SPARK}
                  sx={{ height: 1 }}
                />
              </Grid>
            ))}
          </Grid>
        </Box>
      )}
      <Card component="section" aria-label={fa(pageContract, "stock.farms.title")}>
        <CardHeader title={fa(pageContract, "stock.farms.title")} sx={{ mb: 2 }} />
        {farmItems.length === 0 ? (
          <EmptyContent title={fa(pageContract, "stock.farms.empty")} sx={{ py: 5 }} />
        ) : (
          <TableContainer tabIndex={0} role="group" aria-label={fa(pageContract, "stock.farms.title")}>
            <Table sx={{ minWidth: 1080 }}>
              <TableHead>
                <TableRow>
                  <TableCell component="th">{fa(pageContract, "stock.farms.col.item")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "stock.farms.col.farm")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "stock.farms.col.directed_since")}</TableCell>
                  <TableCell component="th">{fa(pageContract, "stock.farms.col.last_load")}</TableCell>
                  <TableCell component="th">
                    <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}>
                      {fa(pageContract, "stock.farms.col.avg")}
                      <InfoHint size={20} text="Avg / Day is the average kg from the latest 3 locked feed days for this farm and feed. A feed that replaced older ones is counted as one feed here, so a day the farm fed the old sack counts once, not twice. The top card days-left uses Ledger stock divided by this same average." />
                    </Box>
                  </TableCell>
                  <TableCell component="th">
                    <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}>
                      {fa(pageContract, "stock.farms.col.week")}
                      <InfoHint size={20} text="Week need = Avg / Day times 7, using the latest 3 locked feed days for this farm and feed." />
                    </Box>
                  </TableCell>
                  <TableCell component="th">
                    <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}>
                      {fa(pageContract, "stock.farms.col.stock")}
                      <InfoHint size={20} text="Ledger = purchased kg minus consumed-at-import kg, minus locked directed kg from the purchase depletion date onward. A feed that replaced older ones also carries whatever is left of their sacks." />
                    </Box>
                  </TableCell>
                  <TableCell component="th">
                    <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}>
                      {fa(pageContract, "stock.farms.col.days_left")}
                      <InfoHint size={20} text="Days left = ledger stock divided by Avg / Day. Avg / Day uses the latest 3 locked feed days for this farm and feed." />
                    </Box>
                  </TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {farmItems.map((row) => (
                  <TableRow key={`${row.feed_item_key}|${row.farm_label}`}>
                    <TableCell>{row.feed_item_label}</TableCell>
                    <TableCell>{row.farm_label}</TableCell>
                    <TableCell>
                      {row.last_load_consumption_from !== ""
                        ? fmtDate(row.last_load_consumption_from)
                        : row.avg_daily_kg !== ""
                          ? fa(pageContract, "stock.farms.load_pending")
                          : fa(pageContract, "stock.never_directed")}
                    </TableCell>
                    <TableCell>
                      <Typography variant="body2">
                        {[
                          `${fa(pageContract, "stock.farms.batch")} ${row.last_load_batch_no}`,
                          fmtDate(row.last_load_date),
                          `${nf(num(row.last_load_quantity_kg))} ${fa(pageContract, "unit.kg")}`,
                        ].join(" · ")}
                      </Typography>
                      {row.last_load_vendor !== "" ? (
                        <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{row.last_load_vendor}</Typography>
                      ) : null}
                      {row.last_load_total_cost !== "" ? (
                        <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{`₹${money(num(row.last_load_total_cost))} · ₹${rate(num(row.last_load_per_kg_cost))}/kg`}</Typography>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      {row.avg_daily_kg === ""
                        ? "—"
                        : `${nf(num(row.avg_daily_kg))} ${fa(pageContract, "unit.kg")}`}
                    </TableCell>
                    <TableCell>
                      {row.weekly_required_kg === ""
                        ? "—"
                        : `${nf(num(row.weekly_required_kg))} ${fa(pageContract, "unit.kg")}`}
                    </TableCell>
                    <TableCell>
                      <Typography variant="subtitle2">{`${nf(num(row.ledger_stock_kg))} ${fa(pageContract, "unit.kg")}`}</Typography>
                    </TableCell>
                    <TableCell>
                      <DaysLeftText row={row} pageContract={pageContract} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </Card>
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
    <Card component="section" aria-label={fa(pageContract, "forecast.title")}>
      <CardHeader title={fa(pageContract, "forecast.title")} sx={{ mb: 2 }} />
      {rows.length === 0 ? (
        <EmptyContent title={fa(pageContract, "forecast.empty")} sx={{ py: 5 }} />
      ) : (
        <TableContainer tabIndex={0} role="group" aria-label={fa(pageContract, "forecast.title")}>
          <Table sx={{ minWidth: 960 }}>
            <TableHead>
              <TableRow>
                <TableCell component="th">{fa(pageContract, "forecast.col.farm")}</TableCell>
                <TableCell component="th">{fa(pageContract, "forecast.col.item")}</TableCell>
                <TableCell component="th">{fa(pageContract, "forecast.col.avg")}</TableCell>
                <TableCell component="th">{fa(pageContract, "forecast.col.required")}</TableCell>
                <TableCell component="th">{fa(pageContract, "forecast.col.stock")}</TableCell>
                <TableCell component="th">{fa(pageContract, "forecast.col.shortfall")}</TableCell>
                <TableCell component="th">{fa(pageContract, "forecast.col.rate")}</TableCell>
                <TableCell component="th">{fa(pageContract, "forecast.col.required_cost")}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((row) => {
                const kg = fa(pageContract, "unit.kg");
                const short = num(row.shortfall_kg);
                return (
                  <TableRow key={`${row.farm_label}:${row.feed_item_key}`}>
                    <TableCell>{row.farm_label}</TableCell>
                    <TableCell>{row.feed_item_label}</TableCell>
                    <TableCell>{`${nf(num(row.avg_daily_kg))} ${kg}`}</TableCell>
                    <TableCell>
                      <strong>{`${nf(num(row.required_kg))} ${kg}`}</strong>
                    </TableCell>
                    <TableCell>{row.stock_kg === "" ? "—" : `${nf(num(row.stock_kg))} ${kg}`}</TableCell>
                    <TableCell>
                      {row.shortfall_kg === "" ? (
                        "—"
                      ) : short > 0 ? (
                        <Label variant="soft" color="error">{`${nf(short)} ${kg}`}</Label>
                      ) : (
                        <Label variant="soft" color="success">{fa(pageContract, "forecast.covered")}</Label>
                      )}
                    </TableCell>
                    <TableCell>
                      {row.per_kg_cost === ""
                        ? fa(pageContract, "forecast.unpriced")
                        : `₹${rate(num(row.per_kg_cost))}`}
                    </TableCell>
                    <TableCell>{row.required_cost === "" ? "—" : `₹${money(num(row.required_cost))}`}</TableCell>
                  </TableRow>
                );
              })}
              {anyPriced ? (
                <TableRow>
                  <TableCell colSpan={7}>
                    <strong>{fa(pageContract, "forecast.total")}</strong>
                  </TableCell>
                  <TableCell>
                    <strong>{`₹${money(requiredCostTotal)}`}</strong>
                  </TableCell>
                </TableRow>
              ) : null}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </Card>
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
