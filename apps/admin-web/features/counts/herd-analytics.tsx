import { redirect } from "next/navigation";

import { ArrowUpDown, Baby, HeartOff, HeartPulse } from "lucide-react";

import { TrendChart } from "@/components/app/trend-chart";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import CardContent from "@mui/material/CardContent";
import { BarList, type BarListRow } from "@/components/bar-list";
import { PageHeader } from "@/components/app/page-header";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { splitParts } from "@/components/minimal/widgets";
import { GoatGlyph } from "@/components/goat-glyph";
import type { DateRangePickerLabels } from "@/components/date-range-picker";
import type { SvgBarDatum } from "@/components/svg-bars";
import { type LineSeries } from "@/components/svg-series";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getHerdAnalytics,
  type HerdAnalyticsResponse,
  type HerdAnalyticsSeriesPoint,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { backendScope, parseScope } from "@/lib/scope";
import { istDayPlus, todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { HerdAnalyticsDateFilter } from "./herd-analytics-date-filter";
import { HerdAnalyticsTelemetry } from "./herd-analytics-telemetry";
import { FeedAnalyticsExport as HerdAnalyticsExport } from "@/components/analytics-export";
import { stageLabel } from "@/lib/stage-labels";

// Counts -> Herd Analytics. Two questions on one screen, deliberately kept apart
// because they have different time grains:
//
//   COMPOSITION — what the herd IS, right now. Breed, pen tag, sex, kid/adult, farm.
//   It is the SAME live population Counts Breakdown reports, so a reader moving
//   between the two Counts screens never sees the denominator change under them.
//
//   FLOW — what CHANGED it, month by month. Births in, deaths and sales out, other
//   exits, pen movements within.
//
// The page derives NO business number of its own. Every figure below is a backend
// field; the only arithmetic here is re-grouping backend rows into the shape a mark
// is drawn from. In particular the KPI tiles read `totals`, which the backend rolled
// up over the WHOLE window — re-summing `months` to get them would be the banned
// read-time rollup and would silently disagree the day the two stop matching.
//
// Rendering rules inherited from the repo chart stack: server components only, inline
// SVG per the mock's chart anatomy, CSS-custom-property fills, and a series colour that
// follows the SERIES (births are always green, deaths always red) rather than its rank
// on one chart. Park scope belongs to the top bar (Scope Chrome Rule); the page body
// owns only the window length, as a URL param so a view survives reload and pastes as
// a link.

const PAGE_PATH = "/counts/analytics";
/** Mirrors counts/domain.HerdAnalyticsMaxDays — the widest window the read serves. */
const MAX_WINDOW_DAYS = 1150;
/** Mirrors counts/domain.HerdAnalyticsDefaultMonths. */
const HERD_ANALYTICS_DEFAULT_MONTHS = 12;
/**
 * Mirrors counts/domain.HerdAnalyticsFloorDate — the herd's flow history in the product
 * starts in August 2026, so the default window never opens earlier. Named windows
 * before this date are still valid backend reads and must remain selectable.
 */
const HERD_ANALYTICS_FLOOR_DATE = "2026-08-01";

/**
 * Type size on the composition bars, relative to the shared chart's base.
 *
 * These cards run the full width of the page, so the 1100-unit viewBox scales up barely at
 * all and the base 9-unit type lands at roughly 9 real pixels — legible on a chart card
 * three to a row, too small on one that fills the page. Named rather than repeated at each
 * call site so all five charts cannot drift to different sizes.
 */
/** Wire format of a window bound; the shared calendar speaks exactly this. */
const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/;

/**
 * The backend's no-param default window, recomputed here for ONE purpose: deciding
 * whether a selection should be written into the URL or expressed by its absence.
 *
 * Mirrors counts/domain.HerdAnalyticsDefaultWindow — the first of the month eleven
 * back, through today, in IST, never earlier than the history floor. It opens on a
 * month boundary because the chart buckets by month, and an arbitrary start day would
 * put a half-empty first column on the default view that reads as a collapse in
 * births rather than the edge of the window.
 */
function defaultWindow(): { from: string; to: string } {
  const today = todayIso();
  // Step back one month at a time by landing on the first of the month, then stepping
  // one day earlier — month arithmetic without a Date object, so the whole calculation
  // stays on the IST helpers rather than the server's UTC clock.
  let cursor = `${today.slice(0, 7)}-01`;
  for (let i = 0; i < HERD_ANALYTICS_DEFAULT_MONTHS - 1; i += 1) {
    cursor = `${istDayPlus(cursor, -1).slice(0, 7)}-01`;
  }
  // "YYYY-MM-DD" orders lexicographically, so the floor clamp is a string compare.
  if (cursor < HERD_ANALYTICS_FLOOR_DATE) cursor = HERD_ANALYTICS_FLOOR_DATE;
  return { from: cursor, to: today };
}

// Colour follows the SERIES, not its rank: a reader who learns that red is deaths on
// the flow chart must not meet a red "sold" line on the next one.
const SERIES_COLOR = {
  births: "var(--ok)",
  deaths: "var(--danger)",
  sold: "var(--info)",
  other_exits: "var(--amber)",
} as const;

const nf = (value: number) => value.toLocaleString("en-IN");

/** Composition rows: value plus its share of the whole, in the value column ("312 · 41%"). */
function shareRows(bars: SvgBarDatum[]): BarListRow[] {
  const total = bars.reduce((sum, bar) => sum + Math.max(bar.value, 0), 0);
  return bars.map((bar) => ({
    key: bar.key,
    label: bar.label,
    value: bar.value,
    display: total > 0 ? `${nf(bar.value)} \u00b7 ${Math.round((bar.value / total) * 100)}%` : nf(bar.value),
  }));
}
/** Net change is the one figure that can be negative, and the sign is the point. */
const signed = (value: number) => (value > 0 ? `+${nf(value)}` : nf(value));

function ha(pageContract: AdminUiPageContract, key: string): string {
  return copy(pageContract, key);
}

/**
 * The window the reader asked for, as inclusive "YYYY-MM-DD" bounds.
 *
 * Only a WELL-FORMED, correctly-ordered, in-range pair is passed through; anything
 * else falls back to the backend's own default window rather than being sent on to
 * be rejected. The page is a renderer, so a hand-edited URL should land the reader
 * on the default view, not an error card — while the API itself still rejects the
 * same input outright, which is what protects any other caller.
 */
function readWindow(sp: RouteSearchParams): { from?: string; to?: string } {
  const from = one(sp, "from");
  const to = one(sp, "to");
  if (!from || !to || !DATE_PATTERN.test(from) || !DATE_PATTERN.test(to)) return {};
  if (to < from) return {};
  const days = Math.round((Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86_400_000) + 1;
  if (!Number.isFinite(days) || days < 1 || days > MAX_WINDOW_DAYS) return {};
  return { from, to };
}

/**
 * A composition series as bar data. An empty key is a real bucket — animals whose
 * breed/tag/sex was never recorded — and it is LABELLED from contract copy rather
 * than dropped, because hiding it would quietly shrink a total the KPI above still
 * reports in full.
 */
function toBars(points: HerdAnalyticsSeriesPoint[], unassignedLabel: string): SvgBarDatum[] {
  return points.map((point) => ({
    key: point.key || unassignedLabel,
    label: stageLabel(point.label) || unassignedLabel,
    value: point.count,
  }));
}

function ChartCard({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <Card className="herd-mix-card">
      <CardHeader title={title} />
      <CardContent>{children}</CardContent>
    </Card>
  );
}

export async function HerdAnalyticsPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const requested = readWindow(sp);
  const { parkId } = backendScope(parseScope(sp));

  // ONE round trip for the whole screen: composition series, month rows and the
  // whole-window totals arrive together, so there is no per-chart fan-out.
  const result = await getHerdAnalytics({ park_id: parkId, from: requested.from, to: requested.to });
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);

  const data: HerdAnalyticsResponse | null = result.ok ? result.data : null;

  // Every visible string in the shared calendar arrives resolved from the page
  // contract, same as the Verify board supplies it.
  const pickerLabels: DateRangePickerLabels = {
    field: ha(pageContract, "filter.date"),
    today: ha(pageContract, "filter.date.today"),
    single: ha(pageContract, "filter.date.single"),
    range: ha(pageContract, "filter.date.range"),
    aria: ha(pageContract, "filter.date.aria"),
    previousMonth: ha(pageContract, "filter.date.previous_month"),
    nextMonth: ha(pageContract, "filter.date.next_month"),
    rangeStartHint: ha(pageContract, "filter.date.range_start_hint"),
    rangeEndHint: ha(pageContract, "filter.date.range_end_hint"),
    rangeSeparator: ha(pageContract, "filter.date.range_separator"),
  };
  const fallback = defaultWindow();

  const animalsNoun = ha(pageContract, "label.animals_noun");
  const emptyChart = ha(pageContract, "chart.empty");

  if (!data) {
    // A failed read still gets the page's own chrome. Without it the reader lands on a headerless
    // card and cannot tell which screen failed, or navigate from it.
    return (
      <div className="kit-enter pagegrid ha-kit-stack">
        <HerdAnalyticsTelemetry routeId={pageContract.route_id} parkId={parkId} months={0} />
      <div>
        <PageHeader
          title={pageContract.title}
          crumbs={[{ label: ha(pageContract, "crumb") }, { label: ha(pageContract, "section.analytics.title") }]}
          actions={<HerdAnalyticsExport rows={[]} filename={pageContract.route_id} label={ha(pageContract, "action.export")} />}
        />
      </div>
        <div>
          <Card>
            <CardHeader title={ha(pageContract, "error.title")} subheader={ha(pageContract, "error.body")} sx={{ pb: 3 }} />
          </Card>
        </div>
      </div>
    );
  }

  const totals = data.totals;
  const monthLabels = data.months.map((month) => month.label);
  const flowSeries: LineSeries[] = [
    { label: ha(pageContract, "series.births"), colorVar: SERIES_COLOR.births, points: data.months.map((m) => m.births) },
    { label: ha(pageContract, "series.deaths"), colorVar: SERIES_COLOR.deaths, points: data.months.map((m) => m.deaths) },
    { label: ha(pageContract, "series.sold"), colorVar: SERIES_COLOR.sold, points: data.months.map((m) => m.sold) },
    {
      label: ha(pageContract, "series.other_exits"),
      colorVar: SERIES_COLOR.other_exits,
      points: data.months.map((m) => m.other_exits),
    },
  ];
  // Same backend month rows, re-shaped for the shared chart wrapper (no arithmetic).
  const flowKeys = ["births", "deaths", "sold", "other_exits"] as const;
  const flowRows = data.months.map((m) => ({ month: m.label, births: m.births, deaths: m.deaths, sold: m.sold, other_exits: m.other_exits }));
  // KPI sparklines are the SAME monthly series the flow chart draws (one bar per served month), so
  // the card and the chart can never disagree. Fewer than four months is not a shape, it is two
  // stubs, so a short window shows the icon instead and no trend chip is invented from it.
  const spark = (key: "births" | "deaths" | "sold") => (flowRows.length >= 4 ? flowRows.map((r) => r[key]) : undefined);
  const flowChartSeries = flowKeys.map((key, i) => ({ key, label: flowSeries[i].label, color: SERIES_COLOR[key] }));
  // The window the BACKEND served, not the one the URL asked for. When the request
  // carried no bounds the backend chose the default, and the filter must show that
  // choice rather than two empty boxes — otherwise the reader cannot tell what they
  // are looking at, and pressing Apply would submit an empty window.
  const servedFrom = data.window_from;
  const servedTo = data.window_to;

  const ageBars = toBars(data.age_band, ha(pageContract, "label.unassigned_stage"));
  const showParks = data.park.length > 1;
  // The month rows exactly as the backend served them — the same figures the flow chart draws.
  const exportRows: (string | number)[][] = [
    [
      ha(pageContract, "label.animals_noun"),
      ha(pageContract, "series.births"),
      ha(pageContract, "series.deaths"),
      ha(pageContract, "series.sold"),
      ha(pageContract, "series.other_exits"),
    ],
    ...data.months.map((m) => [m.label, m.births, m.deaths, m.sold, m.other_exits]),
  ];
  const nothingRecorded = totals.live_animals === 0 && data.months.every((month) => month.births + month.deaths + month.sold + month.other_exits + month.movements === 0);

  return (
    <div className="kit-enter pagegrid ha-kit-stack">
      <HerdAnalyticsTelemetry routeId={pageContract.route_id} parkId={parkId} months={data.months.length} />
      <div>
        <PageHeader
          title={pageContract.title}
          crumbs={[{ label: ha(pageContract, "crumb") }, { label: ha(pageContract, "section.analytics.title") }]}
          actions={<HerdAnalyticsExport rows={exportRows} filename={pageContract.route_id} label={ha(pageContract, "action.export")} />}
        />
      </div>

      <div className="ha-filter">
        <HerdAnalyticsDateFilter
          labels={pickerLabels}
          basePath={PAGE_PATH}
          from={servedFrom}
          to={servedTo}
          today={todayIso()}
          defaultFrom={fallback.from}
          defaultTo={fallback.to}
        />
      </div>

      {nothingRecorded ? (
        <Card className="counts-empty-state" sx={{ p: { xs: 2, sm: 3 } }}>
          <CardHeader title={ha(pageContract, "empty.title")} sx={{ p: 0 }} />
          <div className="counts-empty-copy muted small">{ha(pageContract, "empty.body")}</div>
        </Card>
      ) : null}

      <div>
        <section aria-label={ha(pageContract, "section.kpi.aria")}>
        <KpiGrid min={200}>
          <KpiCard tone="primary" icon={<GoatGlyph size={22} />} label={ha(pageContract, "kpi.live.label")} value={totals.live_animals} />
          <KpiCard tone="info" icon={<Baby size={22} />} label={ha(pageContract, "kpi.age.label")} value={totals.kids + totals.adults} parts={splitParts(ha(pageContract, "kpi.age.label"), [totals.kids, totals.adults])} />
          <KpiCard tone="success" icon={<HeartPulse size={22} />} sparkline={spark("births")} label={ha(pageContract, "kpi.births.label")} value={totals.births} />
          <KpiCard tone="error" icon={<HeartOff size={22} />} sparkline={spark("deaths")} label={ha(pageContract, "kpi.deaths.label")} value={totals.deaths} />
          <KpiCard tone="violet" icon={<GoatGlyph size={22} />} sparkline={spark("sold")} label={ha(pageContract, "kpi.sold.label")} value={totals.sold} />
          <KpiCard
            tone={totals.net_change < 0 ? "warning" : "success"}
            icon={<ArrowUpDown size={22} />}
            label={ha(pageContract, "kpi.net.label")}
            value={signed(totals.net_change)}
           
          />
        </KpiGrid>
        </section>
      </div>

      <div>
      <Card aria-label={ha(pageContract, "chart.flow.title")}>
        <CardHeader title={ha(pageContract, "chart.flow.title")} />
        <CardContent>
          {flowRows.length === 0 ? (
            <p className="muted small">{emptyChart}</p>
          ) : (
            <TrendChart data={flowRows} xKey="month" kind="bar" integerY series={flowChartSeries} height={300} />
          )}
        </CardContent>
      </Card>
      </div>

      {/* No section heading: each chart card already names what it shows, and a band title
          above five self-describing cards was one heading the reader had to skip. The section
          keeps its accessible name so the grouping is still announced. */}
      <div>
        <section aria-label={ha(pageContract, "section.mix.aria")}>
        <div className="herd-analytics-charts">
          <ChartCard title={ha(pageContract, "chart.breed.title")}>
            <BarList
              rows={shareRows(toBars(data.breed, ha(pageContract, "label.unassigned_breed")))}
              ariaLabel={ha(pageContract, "chart.breed.title")}
              valueNoun={animalsNoun}
              emptyLabel={emptyChart}
            />
          </ChartCard>

          <ChartCard title={ha(pageContract, "chart.stage.title")}>
            <BarList
              rows={shareRows(toBars(data.stage, ha(pageContract, "label.unassigned_stage")))}
              ariaLabel={ha(pageContract, "chart.stage.title")}
              valueNoun={animalsNoun}
              emptyLabel={emptyChart}
            />
          </ChartCard>

          <ChartCard title={ha(pageContract, "chart.age.title")}>
            <BarList
              rows={shareRows(ageBars)}
              ariaLabel={ha(pageContract, "chart.age.title")}
              valueNoun={animalsNoun}
              emptyLabel={emptyChart}
            />
          </ChartCard>

          <ChartCard title={ha(pageContract, "chart.sex.title")}>
            <BarList
              rows={shareRows(toBars(data.sex, ha(pageContract, "label.unassigned_sex")))}
              ariaLabel={ha(pageContract, "chart.sex.title")}
              valueNoun={animalsNoun}
              emptyLabel={emptyChart}
            />
          </ChartCard>

          {showParks ? (
            <ChartCard title={ha(pageContract, "chart.park.title")}>
              <BarList
                rows={shareRows(toBars(data.park, ha(pageContract, "label.unassigned_park")))}
                ariaLabel={ha(pageContract, "chart.park.title")}
                valueNoun={animalsNoun}
                emptyLabel={emptyChart}
              />
            </ChartCard>
          ) : null}
        </div>
        </section>
      </div>
    </div>
  );
}
