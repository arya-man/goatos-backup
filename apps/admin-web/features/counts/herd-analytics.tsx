import { redirect } from "next/navigation";

import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import { PageHeader } from "@/components/app/page-header";
import { KpiGrid } from "@/components/app/kpi-grid";
import { EmptyContent } from "@/components/minimal/empty-content";
import { KpiWidget, splitParts } from "@/components/app/kpi-widget";
import { EcommerceSaleByGender } from "@/components/minimal/sections/overview/e-commerce/ecommerce-sale-by-gender";
import {
  EcommerceSalesOverview,
  type EcommerceSalesOverviewItem,
} from "@/components/minimal/sections/overview/e-commerce/ecommerce-sales-overview";
import { AnalyticsWebsiteVisits } from "@/components/minimal/sections/overview/analytics/analytics-website-visits";
import type { DateRangePickerLabels } from "@/components/date-range-picker";
import type { SvgBarDatum } from "@/components/svg-bars";
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

/** Net change is the one figure that can be negative, and the sign is the point. */

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

/** Composition rows as template EcommerceSalesOverview progress rows: count + share of the whole. */
const ROW_COLORS = ["primary", "info", "warning", "success", "secondary", "error"] as const;
function overviewRows(bars: SvgBarDatum[]): EcommerceSalesOverviewItem[] {
  const total = bars.reduce((sum, bar) => sum + Math.max(bar.value, 0), 0);
  return bars.map((bar, i) => ({
    key: bar.key,
    label: bar.label,
    value: total > 0 ? Math.round((Math.max(bar.value, 0) / total) * 1000) / 10 : 0,
    display: nf(bar.value),
    color: ROW_COLORS[i % ROW_COLORS.length],
  }));
}

/** One composition card: template EcommerceSalesOverview, or its empty state inside the same card. */
function MixCard({ title, bars, emptyLabel }: { title: string; bars: SvgBarDatum[]; emptyLabel: string }) {
  return (
    <EcommerceSalesOverview title={title} data={overviewRows(bars)} aria-label={title} sx={{ height: 1 }}>
      {bars.length === 0 ? <EmptyContent title={emptyLabel} sx={{ py: 3 }} /> : null}
    </EcommerceSalesOverview>
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

  const header = (rows: (string | number)[][]) => (
    <PageHeader
      title={pageContract.title}
      crumbs={[{ label: ha(pageContract, "crumb") }, { label: ha(pageContract, "section.analytics.title") }]}
      actions={<HerdAnalyticsExport rows={rows} filename={pageContract.route_id} label={ha(pageContract, "action.export")} />}
    />
  );

  if (!data) {
    // A failed read still gets the page's own chrome. Without it the reader lands on a headerless
    // card and cannot tell which screen failed, or navigate from it.
    return (
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
        <HerdAnalyticsTelemetry routeId={pageContract.route_id} parkId={parkId} months={0} />
        {header([])}
        <Alert severity="error" variant="outlined">
          <AlertTitle>{ha(pageContract, "error.title")}</AlertTitle>
          {ha(pageContract, "error.body")}
        </Alert>
      </Stack>
    );
  }

  const totals = data.totals;
  const monthLabels = data.months.map((month) => month.label);
  // Colour follows the SERIES (births green, deaths red) on the flow chart and the KPI sparklines.
  const flowKeys = ["births", "deaths", "sold", "other_exits"] as const;
  const flowSeries = flowKeys.map((key) => ({ name: ha(pageContract, `series.${key}`), data: data.months.map((m) => m[key]) }));
  // The window the BACKEND served, not the one the URL asked for. When the request
  // carried no bounds the backend chose the default, and the filter must show that
  // choice rather than two empty boxes — otherwise the reader cannot tell what they
  // are looking at, and pressing Apply would submit an empty window.
  const servedFrom = data.window_from;
  const servedTo = data.window_to;

  const ageBars = toBars(data.age_band, ha(pageContract, "label.unassigned_stage"));
  const sexBars = toBars(data.sex, ha(pageContract, "label.unassigned_sex"));
  const sexTotal = sexBars.reduce((sum, bar) => sum + Math.max(bar.value, 0), 0);
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
    <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      <HerdAnalyticsTelemetry routeId={pageContract.route_id} parkId={parkId} months={data.months.length} />
      {header(exportRows)}

      <Box>
        <HerdAnalyticsDateFilter
          labels={pickerLabels}
          basePath={PAGE_PATH}
          from={servedFrom}
          to={servedTo}
          today={todayIso()}
          defaultFrom={fallback.from}
          defaultTo={fallback.to}
        />
      </Box>

      {/* Everything the window reads (guard: url-keyed-panel): a date / park change swaps it to its
          skeleton at once; header and date filter stay on screen. */}
      <UrlSuspense searchParams={sp} watch={WINDOW_WATCH} fallback={<PanelSkeleton kpis={6} charts={2} />}>
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      {nothingRecorded ? (
        <Card>
          <EmptyContent filled title={ha(pageContract, "empty.title")} description={ha(pageContract, "empty.body")} sx={{ py: 6 }} />
        </Card>
      ) : null}

      {/* KPI row: template CourseWidgetSummary (KpiWidget) (overview/e-commerce), backend window totals. */}
      <Box component="section" aria-label={ha(pageContract, "section.kpi.aria")}>
        <KpiGrid>
          <KpiWidget title={ha(pageContract, "kpi.live.label")} total={totals.live_animals} sx={{ height: 1 }} />
          <KpiWidget
            title={ha(pageContract, "kpi.age.label")}
            total={totals.kids + totals.adults}
            caption={splitParts(ha(pageContract, "kpi.age.label"), [totals.kids, totals.adults])
              .map((part) => `${nf(Number(part.value))} ${part.label}`)
              .join(" \u00b7 ")}
            sx={{ height: 1 }}
          />
          <KpiWidget title={ha(pageContract, "kpi.births.label")} total={totals.births} sx={{ height: 1 }} />
          <KpiWidget title={ha(pageContract, "kpi.deaths.label")} total={totals.deaths} sx={{ height: 1 }} />
          <KpiWidget title={ha(pageContract, "kpi.sold.label")} total={totals.sold} sx={{ height: 1 }} />
          <KpiWidget title={ha(pageContract, "kpi.net.label")} total={totals.net_change} sx={{ height: 1 }} />
        </KpiGrid>
      </Box>

      <Grid container spacing={3}>
        {/* Flow by month: template AnalyticsWebsiteVisits (grouped columns, legend, tooltip). */}
        <Grid size={{ xs: 12, lg: 8 }}>
          <AnalyticsWebsiteVisits
            aria-label={ha(pageContract, "chart.flow.title")}
            title={ha(pageContract, "chart.flow.title")}
            empty={<EmptyContent title={emptyChart} />}
            valueNoun={animalsNoun}
            chart={{
              categories: monthLabels,
              colors: flowKeys.map((key) => SERIES_COLOR[key]),
              series: flowSeries,
            }}
            sx={{ height: 1 }}
          />
        </Grid>
        {/* Sex split: template EcommerceSaleByGender radial (share of the live herd). */}
        <Grid size={{ xs: 12, lg: 4 }}>
          <EcommerceSaleByGender
            aria-label={ha(pageContract, "chart.sex.title")}
            title={ha(pageContract, "chart.sex.title")}
            total={nf(sexTotal)}
            totalLabel={animalsNoun}
            chart={{
              series: sexBars.map((bar) => ({
                label: bar.label,
                value: sexTotal > 0 ? Math.round((Math.max(bar.value, 0) / sexTotal) * 100) : 0,
                display: nf(bar.value),
              })),
            }}
            sx={{ height: 1 }}
          />
        </Grid>
      </Grid>

      {/* Composition now: template EcommerceSalesOverview progress rows (count + share). The
          section keeps its accessible name so the grouping is still announced. */}
      <Grid container spacing={3} component="section" aria-label={ha(pageContract, "section.mix.aria")}>
        {/* Breed is the long list: it takes the left column, the short mixes stack on the right. */}
        <Grid size={{ xs: 12, md: 6 }}>
          <MixCard title={ha(pageContract, "chart.breed.title")} bars={toBars(data.breed, ha(pageContract, "label.unassigned_breed"))} emptyLabel={emptyChart} />
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <Stack spacing={3}>
            <MixCard title={ha(pageContract, "chart.stage.title")} bars={toBars(data.stage, ha(pageContract, "label.unassigned_stage"))} emptyLabel={emptyChart} />
            <MixCard title={ha(pageContract, "chart.age.title")} bars={ageBars} emptyLabel={emptyChart} />
            {showParks ? (
              <MixCard title={ha(pageContract, "chart.park.title")} bars={toBars(data.park, ha(pageContract, "label.unassigned_park"))} emptyLabel={emptyChart} />
            ) : null}
          </Stack>
        </Grid>
      </Grid>
      </Stack>
      </UrlSuspense>
    </Stack>
  );
}

/** The params the one herd-analytics read takes (window + park scope). */
const WINDOW_WATCH = ["from", "to", "park", "scope_mode"] as const;
