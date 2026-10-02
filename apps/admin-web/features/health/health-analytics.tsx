import { redirect } from "next/navigation";

import type { DateRangePickerLabels } from "@/components/date-range-picker";
import { SegmentedLinks } from "@/components/segmented-links";
import { SvgBars, type SvgBarDatum } from "@/components/svg-bars";
import { WindowDateFilter } from "@/components/window-date-filter";
import { TrendChart } from "@/components/app/trend-chart";
import { Iconify } from "@/components/minimal/iconify";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import type { KitTone } from "@/lib/tone";
import { Caption } from "@/components/app/caption";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { KpiWidget, kpiColor, completeMonthPercent } from "@/components/app/kpi-widget";
import { TemplateTabs } from "@/components/app/template-tabs";
import { copy, table, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getHealthAnalytics,
  type HealthAnalyticsResponse,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDate, todayIso } from "@/lib/format";
import { backendScope, parseScope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import {
  toDeathRows,
  toDiseaseRows,
  toEngineRuleRows,
  toMedicineRows,
  type AgeBandLabels,
} from "./health-analytics-rows";
import {
  DeathsTable,
  DiseaseBoardTable,
  EngineRulesTable,
  MedicinesTable,
  type DeathRow,
  type DiseaseRow,
  type EngineRuleRow,
  type MedicineRow,
} from "./health-analytics-tables";
import { HealthAnalyticsTelemetry } from "./health-analytics-telemetry";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { HealthKpiSkeleton, healthKpiSize } from "./health-analytics-layout";
import { LinkButton } from "@/components/app/link-button";

/**
 * Health -> Health Analytics. Three questions on one screen, and the page has to be honest
 * that the third one has a hole in it.
 *
 *   SICKNESS — what the herd is being treated for, at CASE grain, keyed on the diagnosis
 *   RULE the engine named rather than on the treatment card.
 *
 *   EXECUTION — whether the prescribed course is actually carried out, at SESSION grain,
 *   with late kept apart from never-done.
 *
 *   MORTALITY — recorded causes where the death form captured one, with the older open-case
 *   inference retained for deaths recorded before that field existed. Every other death is
 *   reported as not attributed, and the banner says so. A reader who does not know that would
 *   read the unattributed column as missing data rather than as the detection gap it actually
 *   measures.
 *
 * The page derives NO business number of its own. Every figure below is a backend field; the
 * only arithmetic here is re-shaping backend rows into what a mark is drawn from. In
 * particular the KPI tiles read `totals`, which the backend rolled up over the WHOLE window —
 * re-summing `months`, or counting the capped `deaths` list, would be the banned read-time
 * rollup and would silently disagree the day the two stop matching.
 */

const PAGE_PATH = "/health/analytics";
const TAB_PARAM = "tab";
const TABS = ["overview", "problems", "diseases", "mortality", "treatment", "engine"] as const;
type Tab = (typeof TABS)[number];

/** Mirrors health/domain.HealthAnalyticsMaxDays — the widest window the read serves. */
const MAX_WINDOW_DAYS = 1150;
/** Mirrors health/domain.HealthAnalyticsDefaultMonths. */
const DEFAULT_MONTHS = 6;
/**
 * Mirrors health/domain.HealthAnalyticsFloorDate — the Health module's own history starts
 * here, so the default window never opens earlier. Named windows before this date are still
 * valid backend reads and must remain selectable.
 */
const FLOOR_DATE = "2026-08-01";
/** Wire format of a window bound; the shared calendar speaks exactly this. */
const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/;

/**
 * A series colour follows the SERIES, never its rank on one chart: a death under treatment is
 * always teal and one nobody saw coming is always amber, on every chart and in every filter
 * state, so a reader's eye can carry between the two.
 */
const SERIES_COLOR = {
  attributed: "var(--teal)",
  unattributed: "var(--amber)",
} as const;

/** Existing status tokens only (brand lock): each accent this page already used maps to its kit tone. */
const ACCENT_TONE: Record<string, KitTone> = {
  "var(--palette-primary-main)": "primary",
  "var(--palette-info-main)": "info",
  "var(--teal)": "info",
  "var(--palette-error-main)": "error",
  "var(--amber)": "warning",
  "var(--palette-secondary-main)": "violet",
  "var(--palette-text-secondary)": "neutral",
};

const nf = (value: number) => value.toLocaleString("en-IN");
const round1 = (value: number) => Math.round(value * 10) / 10;
const pct = (value: number) => `${value.toLocaleString("en-IN", { maximumFractionDigits: 1 })}%`;

function ha(pageContract: AdminUiPageContract, key: string): string {
  return copy(pageContract, key);
}

/**
 * The backend's no-param default window, recomputed here for ONE purpose: deciding whether a
 * selection should be written into the URL or expressed by its absence.
 *
 * Mirrors health/domain.HealthAnalyticsDefaultWindow — the first of the month five back
 * through today, in IST, never earlier than the history floor. It opens on a month boundary
 * because the chart buckets by month, and an arbitrary start day would put a half-empty first
 * column on the default view that reads as a collapse in cases rather than a window edge.
 */
function defaultWindow(): { from: string; to: string } {
  const today = todayIso();
  const [year, month] = today.split("-").map(Number);
  const zeroBased = (year ?? 0) * 12 + ((month ?? 1) - 1) - (DEFAULT_MONTHS - 1);
  const fromYear = Math.floor(zeroBased / 12);
  const fromMonth = (zeroBased % 12) + 1;
  const from = `${String(fromYear).padStart(4, "0")}-${String(fromMonth).padStart(2, "0")}-01`;
  return { from: from < FLOOR_DATE ? FLOOR_DATE : from, to: today };
}

/**
 * The four windows the farm reads health problems by (maintainer instruction 2026-09-22):
 * the last 30, 90 or 120 days, or everything since the reforms began.
 *
 * A preset is just a from/to pair written into the URL, NOT a mode the page remembers: the
 * calendar beside the chips writes the same two parameters, so a preset and a hand-picked range
 * are the same state and the page cannot end up showing one while the chips claim the other.
 * Whichever chip matches the served window is the one that reads as selected; when none does,
 * the reader picked their own dates and "Custom" is shown as selected instead.
 *
 * "Since the reforms" opens on FLOOR_DATE, the first day the Health module recorded anything.
 * A longer window would only pad the charts with empty months that read as a herd with nothing
 * wrong with it, and the read refuses anything wider than MAX_WINDOW_DAYS anyway.
 *
 * The bounds are INCLUSIVE, so "last 30 days" is today and the 29 days before it.
 */
const WINDOW_PRESETS = [
  { key: "30", days: 30 },
  { key: "90", days: 90 },
  { key: "120", days: 120 },
  { key: "all", days: 0 },
] as const;

function presetWindow(preset: (typeof WINDOW_PRESETS)[number], today: string): { from: string; to: string } {
  if (preset.days === 0) return { from: FLOOR_DATE, to: today };
  const end = Date.parse(`${today}T00:00:00Z`);
  const start = new Date(end - (preset.days - 1) * 86_400_000);
  return { from: start.toISOString().slice(0, 10), to: today };
}

/**
 * The window the reader asked for, as inclusive "YYYY-MM-DD" bounds.
 *
 * Only a WELL-FORMED, correctly-ordered, in-range pair is passed through; anything else falls
 * back to the backend's own default window rather than being sent on to be rejected. The page
 * is a renderer, so a hand-edited URL should land the reader on the default view, not an error
 * card — while the API itself still rejects the same input outright, which is what protects
 * any other caller.
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

/** The params the KPI deck reads (window + park scope). */
const WINDOW_WATCH = ["from", "to", "park", "scope_mode"] as const;
/** The params the tab panel reads. */
const PANEL_WATCH = [...WINDOW_WATCH, "tab"] as const;

function hrefWith(searchParams: RouteSearchParams, updates: Record<string, string | null>): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(searchParams)) {
    if (typeof value === "string") next.set(key, value);
    else if (Array.isArray(value) && value[0] !== undefined) next.set(key, value[0]);
  }
  for (const [key, value] of Object.entries(updates)) {
    if (value === null) next.delete(key);
    else next.set(key, value);
  }
  const qs = next.toString();
  return qs ? `${PAGE_PATH}?${qs}` : PAGE_PATH;
}

/**
 * Names the buckets of a FIXED spine that came back at zero, under the chart that could not
 * draw them. Renders nothing when every bucket has cases, so a full chart carries no clutter.
 */
function EmptyBuckets({ lead, names }: { lead: string; names: string[] }) {
  if (names.length === 0) return null;
  return <Typography variant="body2" sx={{ color: "text.secondary", mt: 1 }}>{`${lead}: ${names.join(", ")}`}</Typography>;
}

/** One KPI: the template CourseWidgetSummary (KpiWidget) (overview/e-commerce) in a Grid cell. */
function Kpi({
  accent,
  label,
  value,
  unit,
  sub,
  monthly,
  windowTo,
  tile,
  md = 4,
}: {
  accent: string;
  label: string;
  /** Template CourseWidgetSummary takes a number; the unit goes in the title. */
  value: number | null;
  unit?: string;
  sub: string;
  /** Month-on-month change of the monthly series behind the figure (drawn in the chart below). */
  monthly?: number[];
  /** The served window's last day: its month is left out of the change while it is running. */
  windowTo?: string;
  /** Position in HEALTH_KPI_MD: the page and HealthKpiSkeleton size each tile from the same list. */
  tile?: number;
  /** A tab panel's own KPI row sizes its tiles itself. */
  md?: number;
}) {
  const tone = ACCENT_TONE[accent];
  const percent = monthly ? completeMonthPercent(monthly, windowTo) : null;
  const lead = value == null ? "—" : unit;
  return (
    <Grid size={tile == null ? { xs: 12, sm: 6, md } : healthKpiSize(tile)}>
      <KpiWidget
        title={label}
        total={value}
        caption={lead ? `${lead} · ${sub}` : sub}
        color={kpiColor(tone)}
        trend={percent == null ? null : { percent, period: "month" }}
        sx={{ height: 1 }}
      />
    </Grid>
  );
}

export async function HealthAnalyticsPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const requested = readWindow(sp);
  const { parkId } = backendScope(parseScope(sp));
  const rawTab = one(sp, TAB_PARAM);
  const tab: Tab = (TABS as readonly string[]).includes(rawTab ?? "") ? (rawTab as Tab) : "overview";

  // ONE round trip for the whole screen. Every tab reads the same response, which is what
  // stops the mortality tab and the KPI strip disagreeing about how many animals died.
  const result = await getHealthAnalytics({ park_id: parkId, from: requested.from, to: requested.to });
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);
  const data: HealthAnalyticsResponse | null = result.ok ? result.data : null;

  const emptyChart = ha(pageContract, "chart.empty");
  const casesNoun = ha(pageContract, "label.cases_noun");

  if (!data) {
    return (
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
        <HealthAnalyticsTelemetry routeId={pageContract.route_id} parkId={parkId} tab={tab} months={0} />
        <Card>
          <CardHeader title={ha(pageContract, "error.title")} subheader={ha(pageContract, "error.body")} sx={{ pb: 3 }} />
        </Card>
      </Stack>
    );
  }

  const totals = data.totals;
  // Same backend month rows, re-shaped for the TrendChart wrapper (no arithmetic).
  const deathRowsByMonth = data.months.map((m) => ({
    month: m.label,
    attributed: m.deaths_attributed,
    unattributed: m.deaths_unattributed,
  }));
  const deathChartSeries = [
    { key: "attributed", label: ha(pageContract, "series.attributed"), color: SERIES_COLOR.attributed },
    { key: "unattributed", label: ha(pageContract, "series.unattributed"), color: SERIES_COLOR.unattributed },
  ];
  const newCaseRowsByMonth = data.months.map((m) => ({ month: m.label, new_cases: m.new_cases }));
  // A spine of zero months is "nothing recorded", not a chart: drawing it leaves a bare 0-4 axis
  // frame with no bars and no words (C1).
  const newCasesAllZero = newCaseRowsByMonth.every((m) => m.new_cases === 0);
  const deathsAllZero = deathRowsByMonth.every((m) => m.attributed + m.unattributed === 0);

  const diseaseBars: SvgBarDatum[] = data.diseases.map((row) => ({
    key: row.key,
    label: row.label,
    value: row.new_cases,
  }));
  // Only diseases that actually killed something. A row of zeroes says nothing and would
  // push the ones that matter off the bottom of the card.
  const fatalityBars: SvgBarDatum[] = data.diseases
    .filter((row) => row.died > 0)
    .map((row) => ({ key: row.key, label: row.label, value: row.case_fatality_pct }));
  const adherenceBars: SvgBarDatum[] = [
    { key: "on_time", label: ha(pageContract, "series.on_time"), value: data.adherence.on_time },
    { key: "late", label: ha(pageContract, "series.late"), value: data.adherence.late },
    { key: "rework", label: ha(pageContract, "series.rework"), value: data.adherence.rework },
    { key: "not_done", label: ha(pageContract, "series.not_done"), value: data.adherence.not_done },
  ];

  const ageBands: AgeBandLabels = {
    adult: ha(pageContract, "label.age.adult"),
    kid: ha(pageContract, "label.age.kid"),
    both: ha(pageContract, "label.age.both"),
    unknown: ha(pageContract, "label.age.unknown"),
  };
  const diseaseRows: DiseaseRow[] = toDiseaseRows(data.diseases, ageBands);
  const deathRows: DeathRow[] = toDeathRows(
    data.deaths,
    {
      never: ha(pageContract, "label.never"),
      unattributed: ha(pageContract, "label.unattributed"),
      noTag: ha(pageContract, "label.no_tag"),
    },
    ageBands,
    fmtDate,
  );
  const medicineRows: MedicineRow[] = toMedicineRows(data.medicines);
  const engineRuleRows: EngineRuleRow[] = toEngineRuleRows(data.engine.rules);
  const TAB_COUNTS: Record<(typeof TABS)[number], number | undefined> = {
    overview: undefined,
    // The problems tab's own grain: episodes in the window, which the backend already totals.
    problems: data.problems.total,
    diseases: diseaseRows.length,
    mortality: deathRows.length,
    treatment: medicineRows.length,
    engine: engineRuleRows.length,
  };

  // Every visible string in the shared calendar arrives resolved from the page contract.
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
  // THE FOUR BREAKDOWNS, rendered exactly as the backend ordered them. The page does no
  // sorting, no capping and no relabelling of its own: the fixed pen-type and age spines carry
  // meaning in their order (two sides that must not swap, bands that must stay youngest-first),
  // and re-sorting them by size here would destroy it.
  const problems = data.problems;
  const toBars = (buckets: typeof problems.by_breed): SvgBarDatum[] =>
    buckets.map((bucket) => ({ key: bucket.key, label: bucket.label, value: bucket.cases }));
  const breedBars = toBars(problems.by_breed);
  // The shared bar chart draws nothing for a zero. On the two FIXED spines that would be a
  // lie by omission: a pen-type chart showing one bar reads as "the farm only has elevated
  // pens", and an age chart missing its youngest bands reads as though nobody weighed kids.
  // The empty buckets are named underneath instead, so a zero stays visibly a zero.
  const emptyNames = (buckets: typeof problems.by_pen_type) =>
    buckets.filter((bucket) => bucket.cases === 0).map((bucket) => bucket.label);
  const noneRecorded = ha(pageContract, "problems.none_recorded");
  const penTypeBars = toBars(problems.by_pen_type);
  const ageBars = toBars(problems.by_age);

  const fallback = defaultWindow();
  const today = todayIso();
  // Which chip reads as selected: the one whose dates ARE the served window. Derived from what
  // the backend answered with, never from what the URL asked for, so a window the read clamped
  // or defaulted can never leave a chip highlighted that does not describe the chart below it.
  const selectedPreset =
    WINDOW_PRESETS.find((preset) => {
      const window = presetWindow(preset, today);
      return window.from === data.window_from && window.to === data.window_to;
    })?.key ?? "custom";

  const nothingRecorded =
    totals.open_cases === 0 &&
    totals.new_cases === 0 &&
    totals.deaths === 0 &&
    data.adherence.sessions_due === 0;

  return (
    <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      <HealthAnalyticsTelemetry
        routeId={pageContract.route_id}
        parkId={parkId}
        tab={tab}
        months={data.months.length}
      />

      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb", "Health") }, { label: pageContract.title }]}
        actions={
          <LinkButton href="/health/config" variant="contained" color="primary" startIcon={<Iconify icon="solar:file-check-bold-duotone" aria-hidden="true" />}>
            {copy(pageContract, "action.manage_protocols", "Health config")}
          </LinkButton>
        }
      />

      {/* THE HONEST DISCLOSURE, above everything. Without it the "not attributed" column
          reads as missing data rather than as the detection gap it measures. */}

      <Stack direction="row" sx={{ flexWrap: "wrap", gap: 2, alignItems: "center" }}>
        <WindowDateFilter
          labels={pickerLabels}
          basePath={PAGE_PATH}
          from={data.window_from}
          to={data.window_to}
          today={todayIso()}
          defaultFrom={fallback.from}
          defaultTo={fallback.to}
        />
        <SegmentedLinks
          ariaLabel={ha(pageContract, "filter.window.aria")}
          current={selectedPreset}
          options={[
            ...WINDOW_PRESETS.map((preset) => {
              const window = presetWindow(preset, today);
              return {
                value: preset.key,
                label: ha(pageContract, `filter.window.${preset.key}`),
                href: hrefWith(sp, { from: window.from, to: window.to }),
              };
            }),
            // Custom is a STATE, never a destination: there is no window it could send the
            // reader to that is not one of the four above. It appears selected when the served
            // window matches none of them (a range picked in the calendar) and is otherwise a
            // disabled label: a click on it went nowhere (TR1-#9, the tab never selected), and a
            // control that does nothing is a dead control. The calendar is how a custom window starts.
            {
              value: "custom",
              label: ha(pageContract, "filter.window.custom"),
              href: hrefWith(sp, { from: null, to: null }),
              disabled: selectedPreset !== "custom",
            },
          ]}
        />
        {/* No "Park scope is set in the top bar." caption (TR2-P2-15): the header park switcher says it. */}
      </Stack>

      {nothingRecorded ? (
        <EmptyState title={ha(pageContract, "empty.title")} />
      ) : null}

      <UrlSuspense searchParams={sp} watch={WINDOW_WATCH} fallback={<HealthKpiSkeleton />}>
      <Box component="section" aria-label={ha(pageContract, "section.kpi.aria")}>
        <Grid container spacing={3}>
        <Kpi
          tile={0}
          accent="var(--palette-info-main)"
          label={ha(pageContract, "kpi.open.label")}
          value={totals.open_cases}
          sub={ha(pageContract, "kpi.open.sub")}
        />
        <Kpi
          tile={1}
          accent="var(--palette-primary-main)"
          label={ha(pageContract, "kpi.new.label")}
          value={totals.new_cases}
          monthly={newCaseRowsByMonth.map((m) => m.new_cases)}
          windowTo={data.window_to}
          sub={ha(pageContract, "kpi.new.sub")}
        />
        <Kpi
          tile={2}
          accent="var(--teal)"
          label={ha(pageContract, "kpi.recovery.label")}
          // Of the cases CLOSED in the window: a still-open course has no outcome yet, and
          // counting it against recovery would drag a long supportive case down forever.
          value={totals.closed_cases === 0 ? null : round1((totals.recovered / totals.closed_cases) * 100)}
          unit="%"
          sub={ha(pageContract, "kpi.recovery.sub")}
        />
        <Kpi
          tile={3}
          accent="var(--palette-error-main)"
          label={ha(pageContract, "kpi.deaths.label")}
          value={totals.deaths}
          monthly={deathRowsByMonth.map((m) => m.attributed + m.unattributed)}
          windowTo={data.window_to}
          sub={ha(pageContract, "kpi.deaths.sub")}
        />
        <Kpi
          tile={4}
          accent="var(--amber)"
          label={ha(pageContract, "kpi.unattributed.label")}
          value={totals.deaths === 0 ? null : round1((totals.deaths_unattributed / totals.deaths) * 100)}
          unit="%"
          sub={ha(pageContract, "kpi.unattributed.sub")}
        />
        </Grid>
      </Box>
      </UrlSuspense>

      <TemplateTabs
        ariaLabel={ha(pageContract, "tab.group.aria")}
        value={tab}
        items={TABS.map((name) => ({
          value: name,
          label: ha(pageContract, `tab.${name}`),
          // Count badges (spec 6): every tab but the overview fronts one table, so the badge is
          // that table's row count for the current window.
          count: TAB_COUNTS[name] === undefined ? undefined : nf(TAB_COUNTS[name] as number),
          // The default tab clears the parameter, so a shared link keeps meaning "the tab
          // this page opens on" rather than freezing on the one it was copied from.
          href: hrefWith(sp, { [TAB_PARAM]: name === "overview" ? null : name }),
        }))}
      />

      {/* The tab's panel (guard: url-keyed-panel): a tab or window click swaps it to its skeleton at
          once; header, filters, KPIs and the strip stay on screen. */}
      <UrlSuspense searchParams={sp} watch={PANEL_WATCH} fallback={<PanelSkeleton charts={2} />}>
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>

      {tab === "overview" ? (
        <>
          <Card aria-label={ha(pageContract, "chart.deaths.title")}>
            <CardHeader title={ha(pageContract, "chart.deaths.title")} />
            <Box sx={{ p: 3 }}>            {deathsAllZero ? (
              <Typography variant="body2" sx={{ color: "text.secondary" }}>{emptyChart}</Typography>
            ) : (
              // Monthly counts are discrete: columns, not an area. A two-point area drew one straight
              // line with a wash under it and read as a trend that was never measured.
              <TrendChart data={deathRowsByMonth} xKey="month" kind="bar" stacked integerY series={deathChartSeries} height={300} />
            )}            </Box>
          </Card>

          <Card aria-label={ha(pageContract, "chart.diseases.title")}>
            <CardHeader title={ha(pageContract, "chart.diseases.title")} />
            <Box sx={{ p: 3 }}>            <SvgBars
              data={diseaseBars}
              maxBars={diseaseBars.length}
              valueNoun={casesNoun}
              chartLabel={ha(pageContract, "chart.diseases.title")}
              emptyLabel={emptyChart}
            />            </Box>
          </Card>
        </>
      ) : null}

      {tab === "problems" ? (
        <>
          {/* NO HEADLINE TILE HERE (maintainer, 2026-09-22). The number the four charts cut is
              already on this page: `problems.total` and `totals.new_cases` count the same rows
              with the same predicate -- TestHealthProblemsOneToManyCountsEachEpisodeOnceAcrossEveryBreakdown
              asserts they are equal -- so the KPI strip above the tabs is showing it as "New
              cases". A second tile would be the same figure twice under two names, which is the
              cross-surface disagreement this repo bans, one card apart. */}
          <Card aria-label={ha(pageContract, "problems.chart.month.title")}>
            <CardHeader title={ha(pageContract, "problems.chart.month.title")} />
            <Box sx={{ p: 3 }}>            {newCasesAllZero ? (
              <EmptyState title={emptyChart} />
            ) : (
              <TrendChart
                data={newCaseRowsByMonth}
                xKey="month"
                kind={newCaseRowsByMonth.length <= 3 ? "bar" : "area"}
                integerY
                series={[{ key: "new_cases", label: ha(pageContract, "kpi.new.label"), color: "var(--palette-primary-main)" }]}
                height={300}
              />
            )}            </Box>
          </Card>

          <Card aria-label={ha(pageContract, "problems.chart.breed.title")}>
            <CardHeader title={ha(pageContract, "problems.chart.breed.title")} />
            <Box sx={{ p: 3 }}>            <SvgBars
              data={breedBars}
              maxBars={breedBars.length}
              valueNoun={casesNoun}
              chartLabel={ha(pageContract, "problems.chart.breed.title")}
              emptyLabel={emptyChart}
            />
            <Caption>{ha(pageContract, "problems.capped_breeds")}</Caption>            </Box>
          </Card>

          <Card aria-label={ha(pageContract, "problems.chart.pen_type.title")}>
            <CardHeader title={ha(pageContract, "problems.chart.pen_type.title")} />
            <Box sx={{ p: 3 }}>            <SvgBars
              data={penTypeBars}
              maxBars={penTypeBars.length}
              valueNoun={casesNoun}
              chartLabel={ha(pageContract, "problems.chart.pen_type.title")}
              emptyLabel={emptyChart}
            />
            <EmptyBuckets lead={noneRecorded} names={emptyNames(problems.by_pen_type)} />            </Box>
          </Card>

          <Card aria-label={ha(pageContract, "problems.chart.age.title")}>
            <CardHeader title={ha(pageContract, "problems.chart.age.title")} />
            <Box sx={{ p: 3 }}>            <SvgBars
              data={ageBars}
              maxBars={ageBars.length}
              valueNoun={casesNoun}
              chartLabel={ha(pageContract, "problems.chart.age.title")}
              emptyLabel={emptyChart}
            />
            <EmptyBuckets lead={noneRecorded} names={emptyNames(problems.by_age)} />            </Box>
          </Card>
        </>
      ) : null}

      {tab === "diseases" ? (
        <>
          <Card>
            <CardHeader title={ha(pageContract, "section.diseases.title")} />
            <Box sx={{ p: 3 }}>            <DiseaseBoardTable
              contract={table(pageContract, "health-disease-board")}
              rows={diseaseRows}
              ariaLabel={ha(pageContract, "section.diseases.title")}
              empty={emptyChart}
            />            </Box>
          </Card>

          <Card aria-label={ha(pageContract, "chart.trend.title")}>
            <CardHeader title={ha(pageContract, "chart.trend.title")} />
            <Box sx={{ p: 3 }}>            {newCasesAllZero ? (
              <Typography variant="body2" sx={{ color: "text.secondary" }}>{emptyChart}</Typography>
            ) : (
              <TrendChart
                data={newCaseRowsByMonth}
                xKey="month"
                kind={newCaseRowsByMonth.length <= 3 ? "bar" : "area"}
                integerY
                series={[{ key: "new_cases", label: ha(pageContract, "kpi.new.label"), color: "var(--palette-primary-main)" }]}
                height={300}
              />
            )}            </Box>
          </Card>
        </>
      ) : null}

      {tab === "mortality" ? (
        <>
          <section aria-label={ha(pageContract, "section.kpi.aria")}>
            <Grid container spacing={3}>
            <Kpi
              accent="var(--teal)"
              label={ha(pageContract, "label.attributed")}
              sub={ha(pageContract, "stat.attributed.sub")}
              value={totals.deaths_attributed}
            />
            <Kpi
              accent="var(--amber)"
              label={ha(pageContract, "label.unattributed")}
              value={totals.deaths_unattributed}
              sub={ha(pageContract, "kpi.unattributed.sub")}
            />
            <Kpi
              accent="var(--palette-text-secondary)"
              label={ha(pageContract, "stat.never.label")}
              value={totals.deaths_never_diagnosed}
              sub={ha(pageContract, "stat.never.sub")}
            />
            </Grid>
          </section>

          <Card aria-label={ha(pageContract, "chart.fatality.title")}>
            <CardHeader title={ha(pageContract, "chart.fatality.title")} />
            <Box sx={{ p: 3 }}>            <SvgBars
              data={fatalityBars}
              maxBars={fatalityBars.length}
              valueNoun="%"
              chartLabel={ha(pageContract, "chart.fatality.title")}
              emptyLabel={emptyChart}
            />            </Box>
          </Card>

          <Card>
            <CardHeader title={ha(pageContract, "section.deaths.title")} />
            <Box sx={{ p: 3 }}>            <DeathsTable
              contract={table(pageContract, "health-deaths")}
              rows={deathRows}
              ariaLabel={ha(pageContract, "section.deaths.title")}
              empty={ha(pageContract, "empty.deaths")}
              noDataLabel={ha(pageContract, "label.no_pen")}
              inferredLabel={ha(pageContract, "label.inferred")}
            />            </Box>
          </Card>
        </>
      ) : null}

      {tab === "treatment" ? (
        <>
          <section aria-label={ha(pageContract, "section.kpi.aria")}>
            <Grid container spacing={3}>
            <Kpi
              md={6}
              accent="var(--palette-primary-main)"
              label={ha(pageContract, "stat.sessions.label")}
              value={data.adherence.sessions_due}
              sub={ha(pageContract, "stat.sessions.sub")}
            />
            <Kpi
              md={6}
              accent="var(--palette-info-main)"
              label={ha(pageContract, "stat.awaiting.label")}
              value={data.adherence.awaiting_verification}
              sub={ha(pageContract, "stat.awaiting.sub")}
            />
            </Grid>
          </section>

          <Card aria-label={ha(pageContract, "chart.adherence.title")}>
            <CardHeader title={ha(pageContract, "chart.adherence.title")} />
            <Box sx={{ p: 3 }}>            <SvgBars
              data={adherenceBars}
              maxBars={adherenceBars.length}
              // The four buckets PARTITION sessions_due, so a share is a real share here.
              showShare
              valueNoun={ha(pageContract, "stat.sessions.label")}
              chartLabel={ha(pageContract, "chart.adherence.title")}
              emptyLabel={emptyChart}
            />            </Box>
          </Card>

          <Card>
            <CardHeader title={ha(pageContract, "section.medicines.title")} />
            <Box sx={{ p: 3 }}>            <MedicinesTable
              contract={table(pageContract, "health-medicines")}
              rows={medicineRows}
              ariaLabel={ha(pageContract, "section.medicines.title")}
              empty={ha(pageContract, "empty.medicines")}
            />            </Box>
          </Card>
        </>
      ) : null}

      {tab === "engine" ? (
        <>
          <section aria-label={ha(pageContract, "section.engine.title")}>
            <Grid container spacing={3}>
            <Kpi
              md={3}
              accent="var(--palette-info-main)"
              label={ha(pageContract, "stat.observations.label")}
              value={data.engine.observations}
              sub={ha(pageContract, "stat.observations.sub")}
            />
            <Kpi
              md={3}
              accent="var(--palette-primary-main)"
              label={ha(pageContract, "stat.confirmed.label")}
              value={data.engine.confirmed}
              sub={pct(data.engine.confirmed_pct)}
            />
            <Kpi
              md={3}
              accent="var(--amber)"
              label={ha(pageContract, "stat.declined.label")}
              value={data.engine.declined}
              sub={ha(pageContract, "stat.pending.label") + ": " + nf(data.engine.pending)}
            />
            <Kpi
              md={3}
              accent="var(--palette-secondary-main)"
              label={ha(pageContract, "stat.median.label")}
              // NULL is "nothing was confirmed", not "confirmed instantly": a zero here would
              // be a claim the data cannot make.
              value={data.engine.median_hours_to_confirm ?? null}
              unit={ha(pageContract, "stat.median.unit")}
              sub={ha(pageContract, "stat.superseded.label") + ": " + nf(data.engine.superseded)}
            />
            </Grid>
          </section>

          <Card>
            <CardHeader title={ha(pageContract, "section.engine_rules.title")} />
            <Box sx={{ p: 3 }}>            <EngineRulesTable
              contract={table(pageContract, "health-engine-rules")}
              rows={engineRuleRows}
              ariaLabel={ha(pageContract, "section.engine_rules.title")}
              empty={ha(pageContract, "empty.engine")}
            />            </Box>
          </Card>
        </>
      ) : null}
      </Stack>
      </UrlSuspense>
    </Stack>
  );
}
