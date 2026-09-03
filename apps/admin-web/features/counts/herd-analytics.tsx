import { redirect } from "next/navigation";

import { ChartHover } from "@/components/chart-hover";
import type { DateRangePickerLabels } from "@/components/date-range-picker";
import { SvgBars, type SvgBarDatum } from "@/components/svg-bars";
import { SeriesLegend, SeriesLines, type LineSeries } from "@/components/svg-series";
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
const COMPOSITION_TEXT_SCALE = 1.7;
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
    label: point.label || unassignedLabel,
    value: point.count,
  }));
}

function ChartCard({
  title,
  hint,
  children,
}: {
  title: string;
  hint: string;
  children: React.ReactNode;
}) {
  return (
    <div className="chartcard">
      <h4>{title}</h4>
      <div className="cap">{hint}</div>
      {children}
    </div>
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
    return (
      <div className="pagegrid">
        <HerdAnalyticsTelemetry routeId={pageContract.route_id} parkId={parkId} months={0} />
        <section className="card">
          <h2 className="h">{ha(pageContract, "error.title")}</h2>
          <p className="muted small">{ha(pageContract, "error.body")}</p>
        </section>
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
  // The window the BACKEND served, not the one the URL asked for. When the request
  // carried no bounds the backend chose the default, and the filter must show that
  // choice rather than two empty boxes — otherwise the reader cannot tell what they
  // are looking at, and pressing Apply would submit an empty window.
  const servedFrom = data.window_from;
  const servedTo = data.window_to;

  const ageBars = toBars(data.age_band, ha(pageContract, "label.unassigned_stage"));
  const showParks = data.park.length > 1;
  const nothingRecorded = totals.live_animals === 0 && data.months.every((month) => month.births + month.deaths + month.sold + month.other_exits + month.movements === 0);

  return (
    <div className="pagegrid">
      <HerdAnalyticsTelemetry routeId={pageContract.route_id} parkId={parkId} months={data.months.length} />

      <p className="muted small" style={{ margin: "0 0 4px" }}>
        {ha(pageContract, "banner.basis")}
      </p>

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
        <span className="muted small ha-filter-hint">{ha(pageContract, "filter.scope_readonly")}</span>
      </div>

      {nothingRecorded ? (
        <section className="card">
          <h2 className="h">{ha(pageContract, "empty.title")}</h2>
          <p className="muted small">{ha(pageContract, "empty.body")}</p>
        </section>
      ) : null}

      <section
        className="grid g3 kpi-row"
        style={{ gap: 14 }}
        aria-label={ha(pageContract, "section.kpi.aria")}
      >
        <div className="kpi">
          <span className="acc" style={{ background: "var(--brand)" }} />
          <div className="lab">{ha(pageContract, "kpi.live.label")}</div>
          <div className="val">{nf(totals.live_animals)}</div>
          <div className="dl">
            <span className="muted">{ha(pageContract, "kpi.live.sub")}</span>
          </div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: "var(--teal)" }} />
          <div className="lab">{ha(pageContract, "kpi.age.label")}</div>
          <div className="val">{`${nf(totals.kids)} · ${nf(totals.adults)}`}</div>
          <div className="dl">
            <span className="muted">{ha(pageContract, "kpi.age.sub")}</span>
          </div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: "var(--ok)" }} />
          <div className="lab">{ha(pageContract, "kpi.births.label")}</div>
          <div className="val">{nf(totals.births)}</div>
          <div className="dl">
            <span className="muted">{ha(pageContract, "kpi.births.sub")}</span>
          </div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: "var(--danger)" }} />
          <div className="lab">{ha(pageContract, "kpi.deaths.label")}</div>
          <div className="val">{nf(totals.deaths)}</div>
          <div className="dl">
            <span className="muted">{ha(pageContract, "kpi.deaths.sub")}</span>
          </div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: "var(--info)" }} />
          <div className="lab">{ha(pageContract, "kpi.sold.label")}</div>
          <div className="val">{nf(totals.sold)}</div>
          <div className="dl">
            <span className="muted">{ha(pageContract, "kpi.sold.sub")}</span>
          </div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: "var(--amber)" }} />
          <div className="lab">{ha(pageContract, "kpi.net.label")}</div>
          <div className="val">{signed(totals.net_change)}</div>
          <div className="dl">
            <span className="muted">{ha(pageContract, "kpi.net.sub")}</span>
          </div>
        </div>
      </section>

      <section className="card wchart" aria-label={ha(pageContract, "chart.flow.title")}>
        <h2 className="h">{ha(pageContract, "chart.flow.title")}</h2>
        <p className="muted small">{ha(pageContract, "chart.flow.hint")}</p>
        <ChartHover>
          <SeriesLines
            series={flowSeries}
            dayLabels={monthLabels}
            valueNoun={animalsNoun}
            chartLabel={ha(pageContract, "chart.flow.title")}
            emptyLabel={emptyChart}
          />
        </ChartHover>
        <SeriesLegend entries={flowSeries.map((series) => ({ label: series.label, colorVar: series.colorVar }))} />
      </section>

      {/* No section heading: each chart card already names what it shows, and a band title
          above five self-describing cards was one heading the reader had to skip. The section
          keeps its accessible name so the grouping is still announced. */}
      <section aria-label={ha(pageContract, "section.mix.aria")}>
        <div className="herd-analytics-charts">
          <ChartCard title={ha(pageContract, "chart.breed.title")} hint={ha(pageContract, "chart.breed.hint")}>
            <ChartHover>
              <SvgBars
                data={toBars(data.breed, ha(pageContract, "label.unassigned_breed"))}
                maxBars={data.breed.length}
                showShare
                textScale={COMPOSITION_TEXT_SCALE}
                valueNoun={animalsNoun}
                chartLabel={ha(pageContract, "chart.breed.title")}
                emptyLabel={emptyChart}
              />
            </ChartHover>
          </ChartCard>

          <ChartCard title={ha(pageContract, "chart.stage.title")} hint={ha(pageContract, "chart.stage.hint")}>
            <ChartHover>
              <SvgBars
                data={toBars(data.stage, ha(pageContract, "label.unassigned_stage"))}
                maxBars={data.stage.length}
                showShare
                textScale={COMPOSITION_TEXT_SCALE}
                valueNoun={animalsNoun}
                chartLabel={ha(pageContract, "chart.stage.title")}
                emptyLabel={emptyChart}
              />
            </ChartHover>
          </ChartCard>

          <ChartCard title={ha(pageContract, "chart.age.title")} hint={ha(pageContract, "chart.age.hint")}>
            <ChartHover>
              <SvgBars
                data={ageBars}
                maxBars={ageBars.length}
                showShare
                textScale={COMPOSITION_TEXT_SCALE}
                valueNoun={animalsNoun}
                chartLabel={ha(pageContract, "chart.age.title")}
                emptyLabel={emptyChart}
              />
            </ChartHover>
          </ChartCard>

          <ChartCard title={ha(pageContract, "chart.sex.title")} hint={ha(pageContract, "chart.sex.hint")}>
            <ChartHover>
              <SvgBars
                data={toBars(data.sex, ha(pageContract, "label.unassigned_sex"))}
                maxBars={data.sex.length}
                showShare
                textScale={COMPOSITION_TEXT_SCALE}
                valueNoun={animalsNoun}
                chartLabel={ha(pageContract, "chart.sex.title")}
                emptyLabel={emptyChart}
              />
            </ChartHover>
          </ChartCard>

          {showParks ? (
            <ChartCard title={ha(pageContract, "chart.park.title")} hint={ha(pageContract, "chart.park.hint")}>
              <ChartHover>
                <SvgBars
                  data={toBars(data.park, ha(pageContract, "label.unassigned_park"))}
                  maxBars={data.park.length}
                  showShare
                  textScale={COMPOSITION_TEXT_SCALE}
                  valueNoun={animalsNoun}
                  chartLabel={ha(pageContract, "chart.park.title")}
                  emptyLabel={emptyChart}
                />
              </ChartHover>
            </ChartCard>
          ) : null}
        </div>
      </section>
    </div>
  );
}
