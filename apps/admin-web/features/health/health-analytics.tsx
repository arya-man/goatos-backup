import { redirect } from "next/navigation";

import { ChartHover } from "@/components/chart-hover";
import type { DateRangePickerLabels } from "@/components/date-range-picker";
import { SegmentedLinks } from "@/components/segmented-links";
import { SvgBars, type SvgBarDatum } from "@/components/svg-bars";
import { SeriesLegend, SeriesLines, type LineSeries } from "@/components/svg-series";
import { WindowDateFilter } from "@/components/window-date-filter";
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
 *   MORTALITY — and here is the hole. Goat OS records no CODED cause of death: the death
 *   workflow captures a written account and two videos, and `exit_reason` is the manner of
 *   exit, never a diagnosis. So a death carries a disease ONLY where a case was open at the
 *   time, every other death is reported as not attributed, and the banner says so. A reader
 *   who does not know that would read the unattributed column as missing data rather than as
 *   the detection gap it actually measures.
 *
 * The page derives NO business number of its own. Every figure below is a backend field; the
 * only arithmetic here is re-shaping backend rows into what a mark is drawn from. In
 * particular the KPI tiles read `totals`, which the backend rolled up over the WHOLE window —
 * re-summing `months`, or counting the capped `deaths` list, would be the banned read-time
 * rollup and would silently disagree the day the two stop matching.
 */

const PAGE_PATH = "/health/analytics";
const TAB_PARAM = "tab";
const TABS = ["overview", "diseases", "mortality", "treatment", "engine"] as const;
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
 * Type size on the bar charts, relative to the shared chart's base — the same multiplier and
 * the same reason as Herd Analytics.
 *
 * These cards run the FULL WIDTH of the page, so the 1100-unit viewBox scales up barely at all
 * and the base 9-unit type lands at roughly 9 real pixels: legible on a card three to a row,
 * too small on one that fills the page. Confirmed on the rendered screen before it was set.
 */
const BAR_TEXT_SCALE = 1.7;

/**
 * A series colour follows the SERIES, never its rank on one chart: a death under treatment is
 * always teal and one nobody saw coming is always amber, on every chart and in every filter
 * state, so a reader's eye can carry between the two.
 */
const SERIES_COLOR = {
  attributed: "var(--teal)",
  unattributed: "var(--amber)",
} as const;

const nf = (value: number) => value.toLocaleString("en-IN");
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

function Kpi({
  accent,
  label,
  value,
  sub,
}: {
  accent: string;
  label: string;
  value: string;
  sub: string;
}) {
  return (
    <div className="kpi">
      <span className="acc" style={{ background: accent }} />
      <div className="lab">{label}</div>
      <div className="val">{value}</div>
      <div className="dl">
        <span className="muted">{sub}</span>
      </div>
    </div>
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
      <div className="pagegrid">
        <HealthAnalyticsTelemetry routeId={pageContract.route_id} parkId={parkId} tab={tab} months={0} />
        <section className="card">
          <h2 className="h">{ha(pageContract, "error.title")}</h2>
          <p className="muted small">{ha(pageContract, "error.body")}</p>
        </section>
      </div>
    );
  }

  const totals = data.totals;
  const monthLabels = data.months.map((month) => month.label);
  const deathSeries: LineSeries[] = [
    {
      label: ha(pageContract, "series.attributed"),
      colorVar: SERIES_COLOR.attributed,
      points: data.months.map((month) => month.deaths_attributed),
    },
    {
      label: ha(pageContract, "series.unattributed"),
      colorVar: SERIES_COLOR.unattributed,
      points: data.months.map((month) => month.deaths_unattributed),
    },
  ];
  const newCaseSeries: LineSeries[] = [
    {
      label: ha(pageContract, "kpi.new.label"),
      colorVar: "var(--brand)",
      points: data.months.map((month) => month.new_cases),
    },
  ];

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
  const fallback = defaultWindow();

  const nothingRecorded =
    totals.open_cases === 0 &&
    totals.new_cases === 0 &&
    totals.deaths === 0 &&
    data.adherence.sessions_due === 0;

  return (
    <div className="pagegrid">
      <HealthAnalyticsTelemetry
        routeId={pageContract.route_id}
        parkId={parkId}
        tab={tab}
        months={data.months.length}
      />

      {/* THE HONEST DISCLOSURE, above everything. Without it the "not attributed" column
          reads as missing data rather than as the detection gap it measures. */}
      <p className="muted small" style={{ margin: "0 0 4px" }}>
        {ha(pageContract, "banner.basis")}
      </p>

      <div className="ha-filter">
        <WindowDateFilter
          labels={pickerLabels}
          basePath={PAGE_PATH}
          from={data.window_from}
          to={data.window_to}
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

      <section className="grid g3 kpi-row" style={{ gap: 14 }} aria-label={ha(pageContract, "section.kpi.aria")}>
        <Kpi
          accent="var(--info)"
          label={ha(pageContract, "kpi.open.label")}
          value={nf(totals.open_cases)}
          sub={ha(pageContract, "kpi.open.sub")}
        />
        <Kpi
          accent="var(--brand)"
          label={ha(pageContract, "kpi.new.label")}
          value={nf(totals.new_cases)}
          sub={ha(pageContract, "kpi.new.sub")}
        />
        <Kpi
          accent="var(--teal)"
          label={ha(pageContract, "kpi.recovery.label")}
          // Of the cases CLOSED in the window: a still-open course has no outcome yet, and
          // counting it against recovery would drag a long supportive case down forever.
          value={pct(totals.closed_cases === 0 ? 0 : (totals.recovered / totals.closed_cases) * 100)}
          sub={ha(pageContract, "kpi.recovery.sub")}
        />
        <Kpi
          accent="var(--danger)"
          label={ha(pageContract, "kpi.deaths.label")}
          value={nf(totals.deaths)}
          sub={ha(pageContract, "kpi.deaths.sub")}
        />
        <Kpi
          accent="var(--amber)"
          label={ha(pageContract, "kpi.unattributed.label")}
          value={pct(totals.deaths === 0 ? 0 : (totals.deaths_unattributed / totals.deaths) * 100)}
          sub={ha(pageContract, "kpi.unattributed.sub")}
        />
      </section>

      <div className="feed-tabbar">
        <SegmentedLinks
          ariaLabel={ha(pageContract, "tab.group.aria")}
          current={tab}
          options={TABS.map((name) => ({
            value: name,
            label: ha(pageContract, `tab.${name}`),
            // The default tab clears the parameter, so a shared link keeps meaning "the tab
            // this page opens on" rather than freezing on the one it was copied from.
            href: hrefWith(sp, { [TAB_PARAM]: name === "overview" ? null : name }),
          }))}
        />
      </div>

      {tab === "overview" ? (
        <>
          <section className="card wchart" aria-label={ha(pageContract, "chart.deaths.title")}>
            <h2 className="h">{ha(pageContract, "chart.deaths.title")}</h2>
            <p className="muted small">{ha(pageContract, "chart.deaths.hint")}</p>
            <ChartHover>
              <SeriesLines
                series={deathSeries}
                dayLabels={monthLabels}
                valueNoun={ha(pageContract, "kpi.deaths.label")}
                chartLabel={ha(pageContract, "chart.deaths.title")}
                emptyLabel={emptyChart}
              />
            </ChartHover>
            <SeriesLegend entries={deathSeries.map((s) => ({ label: s.label, colorVar: s.colorVar }))} />
          </section>

          <section className="card wchart" aria-label={ha(pageContract, "chart.diseases.title")}>
            <h2 className="h">{ha(pageContract, "chart.diseases.title")}</h2>
            <p className="muted small">{ha(pageContract, "chart.diseases.hint")}</p>
            <ChartHover>
              <SvgBars
                data={diseaseBars}
                maxBars={diseaseBars.length}
                textScale={BAR_TEXT_SCALE}
                valueNoun={casesNoun}
                chartLabel={ha(pageContract, "chart.diseases.title")}
                emptyLabel={emptyChart}
              />
            </ChartHover>
          </section>
        </>
      ) : null}

      {tab === "diseases" ? (
        <>
          <section className="card">
            <h2 className="h">{ha(pageContract, "section.diseases.title")}</h2>
            <p className="muted small">{ha(pageContract, "section.diseases.note")}</p>
            <DiseaseBoardTable
              contract={table(pageContract, "health-disease-board")}
              rows={diseaseRows}
              ariaLabel={ha(pageContract, "section.diseases.title")}
              empty={emptyChart}
            />
          </section>

          <section className="card wchart" aria-label={ha(pageContract, "chart.trend.title")}>
            <h2 className="h">{ha(pageContract, "chart.trend.title")}</h2>
            <p className="muted small">{ha(pageContract, "chart.trend.hint")}</p>
            <ChartHover>
              <SeriesLines
                series={newCaseSeries}
                dayLabels={monthLabels}
                valueNoun={casesNoun}
                chartLabel={ha(pageContract, "chart.trend.title")}
                emptyLabel={emptyChart}
              />
            </ChartHover>
          </section>
        </>
      ) : null}

      {tab === "mortality" ? (
        <>
          <section className="grid g3 kpi-row" style={{ gap: 14 }} aria-label={ha(pageContract, "section.kpi.aria")}>
            <Kpi
              accent="var(--teal)"
              label={ha(pageContract, "label.attributed")}
              sub={ha(pageContract, "stat.attributed.sub")}
              value={nf(totals.deaths_attributed)}
            />
            <Kpi
              accent="var(--amber)"
              label={ha(pageContract, "label.unattributed")}
              value={nf(totals.deaths_unattributed)}
              sub={ha(pageContract, "kpi.unattributed.sub")}
            />
            <Kpi
              accent="var(--muted)"
              label={ha(pageContract, "stat.never.label")}
              value={nf(totals.deaths_never_diagnosed)}
              sub={ha(pageContract, "stat.never.sub")}
            />
          </section>

          <section className="card wchart" aria-label={ha(pageContract, "chart.fatality.title")}>
            <h2 className="h">{ha(pageContract, "chart.fatality.title")}</h2>
            <p className="muted small">{ha(pageContract, "chart.fatality.hint")}</p>
            <ChartHover>
              <SvgBars
                data={fatalityBars}
                maxBars={fatalityBars.length}
                textScale={BAR_TEXT_SCALE}
                valueNoun="%"
                chartLabel={ha(pageContract, "chart.fatality.title")}
                emptyLabel={emptyChart}
              />
            </ChartHover>
          </section>

          <section className="card">
            <h2 className="h">{ha(pageContract, "section.deaths.title")}</h2>
            <p className="muted small">{ha(pageContract, "section.deaths.note")}</p>
            <DeathsTable
              contract={table(pageContract, "health-deaths")}
              rows={deathRows}
              ariaLabel={ha(pageContract, "section.deaths.title")}
              empty={ha(pageContract, "empty.deaths")}
              noDataLabel={ha(pageContract, "label.no_pen")}
            />
          </section>
        </>
      ) : null}

      {tab === "treatment" ? (
        <>
          <section className="grid g3 kpi-row" style={{ gap: 14 }} aria-label={ha(pageContract, "section.kpi.aria")}>
            <Kpi
              accent="var(--brand)"
              label={ha(pageContract, "stat.sessions.label")}
              value={nf(data.adherence.sessions_due)}
              sub={ha(pageContract, "stat.sessions.sub")}
            />
            <Kpi
              accent="var(--info)"
              label={ha(pageContract, "stat.awaiting.label")}
              value={nf(data.adherence.awaiting_verification)}
              sub={ha(pageContract, "stat.awaiting.sub")}
            />
          </section>

          <section className="card wchart" aria-label={ha(pageContract, "chart.adherence.title")}>
            <h2 className="h">{ha(pageContract, "chart.adherence.title")}</h2>
            <p className="muted small">{ha(pageContract, "chart.adherence.hint")}</p>
            <ChartHover>
              <SvgBars
                data={adherenceBars}
                maxBars={adherenceBars.length}
                textScale={BAR_TEXT_SCALE}
                // The four buckets PARTITION sessions_due, so a share is a real share here.
                showShare
                valueNoun={ha(pageContract, "stat.sessions.label")}
                chartLabel={ha(pageContract, "chart.adherence.title")}
                emptyLabel={emptyChart}
              />
            </ChartHover>
          </section>

          <section className="card">
            <h2 className="h">{ha(pageContract, "section.medicines.title")}</h2>
            <p className="muted small">{ha(pageContract, "section.medicines.note")}</p>
            <MedicinesTable
              contract={table(pageContract, "health-medicines")}
              rows={medicineRows}
              ariaLabel={ha(pageContract, "section.medicines.title")}
              empty={ha(pageContract, "empty.medicines")}
            />
          </section>
        </>
      ) : null}

      {tab === "engine" ? (
        <>
          <p className="muted small" style={{ margin: "0 0 -4px" }}>
            {ha(pageContract, "section.engine.note")}
          </p>
          <section className="grid g3 kpi-row" style={{ gap: 14 }} aria-label={ha(pageContract, "section.engine.title")}>
            <Kpi
              accent="var(--info)"
              label={ha(pageContract, "stat.observations.label")}
              value={nf(data.engine.observations)}
              sub={ha(pageContract, "stat.observations.sub")}
            />
            <Kpi
              accent="var(--brand)"
              label={ha(pageContract, "stat.confirmed.label")}
              value={nf(data.engine.confirmed)}
              sub={pct(data.engine.confirmed_pct)}
            />
            <Kpi
              accent="var(--amber)"
              label={ha(pageContract, "stat.declined.label")}
              value={nf(data.engine.declined)}
              sub={ha(pageContract, "stat.pending.label") + ": " + nf(data.engine.pending)}
            />
            <Kpi
              accent="var(--purple)"
              label={ha(pageContract, "stat.median.label")}
              // NULL is "nothing was confirmed", not "confirmed instantly": a zero here would
              // be a claim the data cannot make.
              value={
                data.engine.median_hours_to_confirm === null || data.engine.median_hours_to_confirm === undefined
                  ? "—"
                  : `${nf(data.engine.median_hours_to_confirm)}${ha(pageContract, "stat.median.unit")}`
              }
              sub={ha(pageContract, "stat.superseded.label") + ": " + nf(data.engine.superseded)}
            />
          </section>

          <section className="card">
            <h2 className="h">{ha(pageContract, "section.engine_rules.title")}</h2>
            <p className="muted small">{ha(pageContract, "section.engine_rules.note")}</p>
            <EngineRulesTable
              contract={table(pageContract, "health-engine-rules")}
              rows={engineRuleRows}
              ariaLabel={ha(pageContract, "section.engine_rules.title")}
              empty={ha(pageContract, "empty.engine")}
            />
          </section>
        </>
      ) : null}
    </div>
  );
}
