import { redirect } from "next/navigation";

import { ChartHover } from "@/components/chart-hover";
import type { DateRangePickerLabels } from "@/components/date-range-picker";
import { SeriesLegend, StackedColumns, seriesColorVar, type StackedDay } from "@/components/svg-series";
import { Tag } from "@/components/ui-primitives";
import { copy, table, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getCountsMortality,
  type MortalityBucket,
  type MortalityCrossCell,
  type MortalityResponse,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDate, istDayPlus, todayIso } from "@/lib/format";
import { backendScope, parseScope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { VaccinationTablePager } from "@/features/preventive-care-vaccination";
import { HerdAnalyticsDateFilter } from "./herd-analytics-date-filter";
import { RecentDeathsTable } from "./mortality-tables";
import { MortalityTelemetry } from "./mortality-telemetry";

// Counts -> Mortality. One question — which animals died, and what did they have in common —
// asked from every angle the farm can ask it.
//
// Two kinds of series, and the page keeps them visibly apart:
//
//   RATE series (kids/adults, stage, breed, sex, species, farm, pen, load, vendor) are drawn as a
//   rate table: deaths, every animal that was in the section during the window, and the rate with a proportional bar, because a breed with
//   3 deaths out of 40 and one with 3 out of 400 must not look alike.
//
//   COUNT series (age at death, season, cause, days since arrival, days since vaccination)
//   are drawn as share tables: they are facts about the death alone and carry no rate.
//
// The page derives NO business number of its own. Every figure below is a backend field; the
// only arithmetic is re-grouping backend rows into the shape a mark is drawn from (a cross
// tab's row totals are sums of the backend's own cells, rendered as such). The KPI tiles read
// `totals`, which the backend rolled up over the WHOLE window — re-summing a series or counting
// the capped recent list would be the banned read-time rollup.
//
// Park scope belongs to the top bar (Scope Chrome Rule); the page body owns only the window,
// as a URL param so a view survives reload and pastes as a link.

const PAGE_PATH = "/counts/mortality";
/** Mirrors counts/domain.HerdAnalyticsMaxDays — the widest window the read serves. */
const MAX_WINDOW_DAYS = 1150;
/** Mirrors counts/domain.HerdAnalyticsDefaultMonths. */
const DEFAULT_MONTHS = 12;
/** Mirrors counts/domain.HerdAnalyticsFloorDate. */
const FLOOR_DATE = "2026-08-01";
const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/;

const nf = (value: number) => value.toLocaleString("en-IN");
const pct = (value: number | null | undefined) =>
  value == null ? null : `${value.toLocaleString("en-IN", { maximumFractionDigits: 1 })}%`;

function mc(pageContract: AdminUiPageContract, key: string): string {
  return copy(pageContract, key);
}

/** Same default-window rule as Herd Analytics, so the two Counts read screens agree. */
function defaultWindow(): { from: string; to: string } {
  const today = todayIso();
  let cursor = `${today.slice(0, 7)}-01`;
  for (let i = 0; i < DEFAULT_MONTHS - 1; i += 1) {
    cursor = `${istDayPlus(cursor, -1).slice(0, 7)}-01`;
  }
  if (cursor < FLOOR_DATE) cursor = FLOOR_DATE;
  return { from: cursor, to: today };
}

function readWindow(sp: RouteSearchParams): { from?: string; to?: string } {
  const from = one(sp, "from");
  const to = one(sp, "to");
  if (!from || !to || !DATE_PATTERN.test(from) || !DATE_PATTERN.test(to)) return {};
  if (to < from) return {};
  const days = Math.round((Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86_400_000) + 1;
  if (!Number.isFinite(days) || days < 1 || days > MAX_WINDOW_DAYS) return {};
  return { from, to };
}

function ChartCard({ title, hint, children }: { title: string; hint: string; children: React.ReactNode }) {
  return (
    <div className="chartcard">
      <h4>{title}</h4>
      <div className="cap">{hint}</div>
      {children}
    </div>
  );
}

/**
 * A RATE series: one row per bucket with deaths, the animals that were in the section during the window and the rate, plus a bar whose length
 * is the rate against the series' highest rate. Buckets with no deaths still list (a breed with
 * zero deaths out of 400 is a finding), sorted by the backend — most deaths first.
 */
function RateTable({
  buckets,
  unassignedLabel,
  deathsLabel,
  animalsLabel,
  rateLabel,
  noRateLabel,
  emptyLabel,
  ariaLabel,
}: {
  buckets: MortalityBucket[];
  unassignedLabel: string;
  deathsLabel: string;
  animalsLabel: string;
  rateLabel: string;
  noRateLabel: string;
  emptyLabel: string;
  ariaLabel: string;
}) {
  if (buckets.length === 0) {
    return (
      <div className="muted small" style={{ padding: "14px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }
  const maxRate = Math.max(0, ...buckets.map((b) => b.rate_pct ?? 0));
  return (
    <table className="mortality-rate-table" aria-label={ariaLabel}>
      <thead>
        <tr>
          <th scope="col" />
          <th scope="col" className="num">
            {deathsLabel}
          </th>
          <th scope="col" className="num">
            {animalsLabel}
          </th>
          <th scope="col" className="num">
            {rateLabel}
          </th>
          <th scope="col" className="bar" />
        </tr>
      </thead>
      <tbody>
        {buckets.map((bucket) => {
          const rate = bucket.rate_pct ?? null;
          const width = rate == null || maxRate <= 0 ? 0 : Math.max(rate > 0 ? 2 : 0, (rate / maxRate) * 100);
          return (
            <tr key={bucket.key || "__unassigned"}>
              <th scope="row">{bucket.label || unassignedLabel}</th>
              <td className="num">{bucket.deaths > 0 ? <strong>{nf(bucket.deaths)}</strong> : nf(bucket.deaths)}</td>
              <td className="num muted">{nf(bucket.animals)}</td>
              <td className="num">{rate == null ? <span className="muted" title={noRateLabel}>—</span> : pct(rate)}</td>
              <td className="bar">
                <span className="mortality-rate-bar" style={{ width: `${width}%` }} aria-hidden="true" />
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

/**
 * A cross tab. Rows and columns are the backend's own labels in first-seen order (the backend
 * already sorts rows by deaths); each cell is a backend count and the margins are sums of those
 * same cells, shown so a reader can check the table against the KPI above it.
 */
function CrossTable({
  cells,
  totalLabel,
  emptyLabel,
  ariaLabel,
}: {
  cells: MortalityCrossCell[];
  totalLabel: string;
  emptyLabel: string;
  ariaLabel: string;
}) {
  if (cells.length === 0) {
    return (
      <div className="muted small" style={{ padding: "14px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }
  const rows: { key: string; label: string }[] = [];
  const cols: { key: string; label: string }[] = [];
  const seenRow = new Set<string>();
  const seenCol = new Set<string>();
  const grid = new Map<string, number>();
  const rowTotals = new Map<string, number>();
  const colTotals = new Map<string, number>();
  for (const cell of cells) {
    if (!seenRow.has(cell.row_key)) {
      seenRow.add(cell.row_key);
      rows.push({ key: cell.row_key, label: cell.row_label });
    }
    if (!seenCol.has(cell.col_key)) {
      seenCol.add(cell.col_key);
      cols.push({ key: cell.col_key, label: cell.col_label });
    }
    grid.set(`${cell.row_key}\t${cell.col_key}`, (grid.get(`${cell.row_key}\t${cell.col_key}`) ?? 0) + cell.deaths);
    rowTotals.set(cell.row_key, (rowTotals.get(cell.row_key) ?? 0) + cell.deaths);
    colTotals.set(cell.col_key, (colTotals.get(cell.col_key) ?? 0) + cell.deaths);
  }
  // Rows by their total, columns by theirs, so the heaviest corner is top-left.
  rows.sort((a, b) => (rowTotals.get(b.key) ?? 0) - (rowTotals.get(a.key) ?? 0));
  cols.sort((a, b) => (colTotals.get(b.key) ?? 0) - (colTotals.get(a.key) ?? 0));
  const max = Math.max(1, ...grid.values());
  const grand = [...rowTotals.values()].reduce((a, b) => a + b, 0);
  return (
    <div className="health-analytics-scroll" tabIndex={0} role="region" aria-label={ariaLabel}>
      <table className="mortality-cross-table" aria-label={ariaLabel}>
        <thead>
          <tr>
            <th scope="col" />
            {cols.map((col) => (
              <th key={col.key || "__none"} scope="col" className="num">
                {col.label}
              </th>
            ))}
            <th scope="col" className="num total">
              {totalLabel}
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.key || "__none"}>
              <th scope="row">{row.label}</th>
              {cols.map((col) => {
                const value = grid.get(`${row.key}\t${col.key}`) ?? 0;
                // Heat: the cell's share of the largest cell, as an alpha on the brand colour.
                const alpha = value === 0 ? 0 : 0.12 + 0.55 * (value / max);
                return (
                  <td
                    key={col.key || "__none"}
                    className={`num${value === 0 ? " zero" : ""}`}
                    style={value === 0 ? undefined : { background: `color-mix(in srgb, var(--danger) ${Math.round(alpha * 100)}%, transparent)` }}
                  >
                    {value === 0 ? "·" : nf(value)}
                  </td>
                );
              })}
              <td className="num total">{nf(rowTotals.get(row.key) ?? 0)}</td>
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr>
            <th scope="row">{totalLabel}</th>
            {cols.map((col) => (
              <td key={col.key || "__none"} className="num total">
                {nf(colTotals.get(col.key) ?? 0)}
              </td>
            ))}
            <td className="num total">{nf(grand)}</td>
          </tr>
        </tfoot>
      </table>
    </div>
  );
}

/**
 * A COUNT series as a table: label, deaths, share of all deaths and a bar against the largest
 * bucket. Every band is listed, zeros included, so "none in summer" is a visible zero rather
 * than a missing row. The cause series adds a basis chip per row, which a bar chart cannot carry.
 */
function ShareTable({
  buckets,
  totalDeaths,
  basisLabels,
  deathsLabel,
  shareLabel,
  emptyLabel,
  ariaLabel,
  unassignedLabel,
}: {
  buckets: MortalityBucket[];
  totalDeaths: number;
  basisLabels?: Record<"recorded" | "inferred" | "none", string>;
  deathsLabel: string;
  shareLabel: string;
  emptyLabel: string;
  ariaLabel: string;
  unassignedLabel?: string;
}) {
  if (buckets.length === 0 || totalDeaths === 0) {
    return (
      <div className="muted small" style={{ padding: "14px 2px", textAlign: "center" }}>
        {emptyLabel}
      </div>
    );
  }
  const max = Math.max(1, ...buckets.map((b) => b.deaths));
  return (
    <table className="mortality-rate-table" aria-label={ariaLabel}>
      <thead>
        <tr>
          <th scope="col" />
          {basisLabels ? <th scope="col" /> : null}
          <th scope="col" className="num">
            {deathsLabel}
          </th>
          <th scope="col" className="num">
            {shareLabel}
          </th>
          <th scope="col" className="bar" />
        </tr>
      </thead>
      <tbody>
        {buckets.map((bucket) => {
          const basis = (bucket.basis ?? "none") as "recorded" | "inferred" | "none";
          const share = (bucket.deaths / totalDeaths) * 100;
          const muted = basisLabels ? basis === "none" : false;
          return (
            <tr key={`${bucket.basis ?? ""}:${bucket.key}`}>
              <th scope="row">{bucket.label || unassignedLabel || bucket.key}</th>
              {basisLabels ? (
                <td>
                  <Tag tone={basis === "recorded" ? "teal" : basis === "inferred" ? "info" : "mut"}>{basisLabels[basis]}</Tag>
                </td>
              ) : null}
              <td className="num">{bucket.deaths > 0 ? <strong>{nf(bucket.deaths)}</strong> : <span className="muted">{nf(bucket.deaths)}</span>}</td>
              <td className="num muted">{bucket.deaths === 0 ? "—" : share < 1 ? "<1%" : pct(Math.round(share * 10) / 10)}</td>
              <td className="bar">
                <span
                  className="mortality-rate-bar"
                  style={{ width: `${(bucket.deaths / max) * 100}%`, background: muted ? "var(--muted)" : undefined }}
                  aria-hidden="true"
                />
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function Kpi({ accent, label, value, sub }: { accent: string; label: string; value: React.ReactNode; sub: React.ReactNode }) {
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

export async function MortalityPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const requested = readWindow(sp);
  const { parkId } = backendScope(parseScope(sp));

  // The deaths list pages on the SERVER: the window can hold more animals than one screen, and
  // slicing a capped list on the client would show a pager that stops before the tile above it
  // does. Both parameters are sent as asked and RESOLVED there -- an unknown size or a bad offset
  // lands on the first page rather than failing a screen whose every other figure is whole-window.
  const pageSizeOptions = tablePageSizes(pageContract, "recent-deaths");
  const requestedSize = Number(one(sp, "md_limit"));
  const recentLimit = pageSizeOptions.includes(requestedSize) ? requestedSize : (pageSizeOptions[1] ?? pageSizeOptions[0] ?? 25);
  const requestedPage = Math.max(1, Number(one(sp, "md_page")) || 1);

  // ONE round trip for the whole screen.
  const result = await getCountsMortality({
    park_id: parkId,
    from: requested.from,
    to: requested.to,
    recent_limit: recentLimit,
    recent_offset: (requestedPage - 1) * recentLimit,
  });
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);
  const data: MortalityResponse | null = result.ok ? result.data : null;

  const pickerLabels: DateRangePickerLabels = {
    field: mc(pageContract, "filter.date"),
    today: mc(pageContract, "filter.date.today"),
    single: mc(pageContract, "filter.date.single"),
    range: mc(pageContract, "filter.date.range"),
    aria: mc(pageContract, "filter.date.aria"),
    previousMonth: mc(pageContract, "filter.date.previous_month"),
    nextMonth: mc(pageContract, "filter.date.next_month"),
    rangeStartHint: mc(pageContract, "filter.date.range_start_hint"),
    rangeEndHint: mc(pageContract, "filter.date.range_end_hint"),
    rangeSeparator: mc(pageContract, "filter.date.range_separator"),
  };
  const fallback = defaultWindow();

  if (!data) {
    return (
      <div className="pagegrid">
        <MortalityTelemetry routeId={pageContract.route_id} parkId={parkId} months={0} deaths={0} />
        <section className="card">
          <h2 className="h">{mc(pageContract, "error.title")}</h2>
          <p className="muted small">{mc(pageContract, "error.body")}</p>
        </section>
      </div>
    );
  }

  const totals = data.totals;

  // Every pager link carries the rest of the URL forward -- the window and the park scope live
  // there too, and a page link that dropped them would silently re-scope the screen it is paging.
  function hrefWithParam(key: string, value: string, options?: { drop?: string[] }): string {
    const dropped = new Set([key, ...(options?.drop ?? [])]);
    const next = new URLSearchParams();
    for (const [paramKey, paramValue] of Object.entries(sp)) {
      if (dropped.has(paramKey)) continue;
      if (Array.isArray(paramValue)) {
        for (const item of paramValue) if (item) next.append(paramKey, item);
      } else if (paramValue) {
        next.set(paramKey, paramValue);
      }
    }
    if (value) next.set(key, value);
    const qs = next.toString();
    return qs ? `${PAGE_PATH}?${qs}` : PAGE_PATH;
  }

  const deathsNoun = mc(pageContract, "label.deaths_noun");
  const emptyChart = mc(pageContract, "chart.empty");
  const animalsWord = mc(pageContract, "kpi.animals");
  const noRate = mc(pageContract, "kpi.no_rate");
  const rateLabels = {
    deathsLabel: mc(pageContract, "series.deaths"),
    animalsLabel: mc(pageContract, "kpi.animals"),
    rateLabel: mc(pageContract, "series.rate"),
    noRateLabel: noRate,
    emptyLabel: emptyChart,
  };
  const basisLabels = {
    recorded: mc(pageContract, "basis.recorded"),
    inferred: mc(pageContract, "basis.inferred"),
    none: mc(pageContract, "basis.none"),
  } as const;

  const monthDays: StackedDay[] = data.months.map((m) => ({ key: m.month, label: m.label, segments: [m.kids, m.adults] }));
  // StackedColumns colours its segments by series INDEX from the shared palette, so the legend
  // takes the same index-derived colour rather than naming one of its own.
  const monthSeries = [
    { label: mc(pageContract, "series.kids"), colorVar: seriesColorVar(0) },
    { label: mc(pageContract, "series.adults"), colorVar: seriesColorVar(1) },
  ];
  const causeEstablished = totals.cause_recorded + totals.cause_inferred;
  const rateWithAnimals = (rate: number | null | undefined, animals: number) =>
    rate == null ? noRate : `${pct(rate)} · ${nf(animals)} ${animalsWord}`;
  const showParks = data.park.length > 1;
  const showSpecies = data.species.length > 1;

  return (
    <div className="pagegrid mortality-page">
      <MortalityTelemetry routeId={pageContract.route_id} parkId={parkId} months={data.months.length} deaths={totals.deaths} />

      <p className="muted small" style={{ margin: "0 0 4px" }}>
        {mc(pageContract, "banner.basis")}
      </p>

      <div className="ha-filter">
        <HerdAnalyticsDateFilter
          labels={pickerLabels}
          basePath={PAGE_PATH}
          from={data.window_from}
          to={data.window_to}
          today={todayIso()}
          defaultFrom={fallback.from}
          defaultTo={fallback.to}
        />
        <span className="muted small ha-filter-hint">{mc(pageContract, "filter.scope_readonly")}</span>
      </div>

      <section className="grid g3 kpi-row" style={{ gap: 14 }} aria-label={mc(pageContract, "section.kpi.aria")}>
        <Kpi accent="var(--danger)" label={mc(pageContract, "kpi.deaths.label")} value={nf(totals.deaths)} sub={mc(pageContract, "kpi.deaths.sub")} />
        <Kpi
          accent="var(--brand)"
          label={mc(pageContract, "kpi.rate.label")}
          value={pct(totals.rate_pct) ?? "—"}
          sub={totals.rate_pct == null ? noRate : `${nf(totals.animals)} ${animalsWord} · ${mc(pageContract, "kpi.rate.sub")}`}
        />
        <Kpi accent="var(--amber)" label={mc(pageContract, "kpi.kids.label")} value={nf(totals.kid_deaths)} sub={rateWithAnimals(totals.kid_rate_pct, totals.kid_animals)} />
        <Kpi accent="var(--purple)" label={mc(pageContract, "kpi.adults.label")} value={nf(totals.adult_deaths)} sub={rateWithAnimals(totals.adult_rate_pct, totals.adult_animals)} />
        <Kpi accent="var(--teal)" label={mc(pageContract, "kpi.first_week.label")} value={nf(totals.first_week_deaths)} sub={mc(pageContract, "kpi.first_week.sub")} />
        <Kpi
          accent="var(--info)"
          label={mc(pageContract, "kpi.cause.label")}
          value={totals.deaths === 0 ? "—" : `${nf(causeEstablished)} / ${nf(totals.deaths)}`}
          sub={mc(pageContract, "kpi.cause.sub")}
        />
      </section>

      <section className="card wchart" aria-label={mc(pageContract, "chart.months.title")}>
        <h2 className="h">{mc(pageContract, "chart.months.title")}</h2>
        <p className="muted small">{mc(pageContract, "chart.months.hint")}</p>
        <ChartHover>
          <StackedColumns
            days={monthDays}
            seriesLabels={monthSeries.map((s) => s.label)}
            valueNoun={deathsNoun}
            chartLabel={mc(pageContract, "chart.months.title")}
            emptyLabel={emptyChart}
          />
        </ChartHover>
        <SeriesLegend entries={monthSeries} />
      </section>

      {/* RATE series. Each card names what it divides by. */}
      <section aria-label={mc(pageContract, "section.rates.aria")}>
        <div className="mortality-grid">
          <ChartCard title={mc(pageContract, "chart.stage.title")} hint={mc(pageContract, "chart.stage.hint")}>
            <RateTable buckets={data.stage} unassignedLabel={mc(pageContract, "label.unassigned_stage")} ariaLabel={mc(pageContract, "chart.stage.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.breed.title")} hint={mc(pageContract, "chart.breed.hint")}>
            <RateTable buckets={data.breed} unassignedLabel={mc(pageContract, "label.unassigned_breed")} ariaLabel={mc(pageContract, "chart.breed.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.load.title")} hint={mc(pageContract, "chart.load.hint")}>
            <RateTable buckets={data.load} unassignedLabel={mc(pageContract, "label.no_load")} ariaLabel={mc(pageContract, "chart.load.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.vendor.title")} hint={mc(pageContract, "chart.vendor.hint")}>
            <RateTable buckets={data.vendor} unassignedLabel={mc(pageContract, "label.no_vendor")} ariaLabel={mc(pageContract, "chart.vendor.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.pen.title")} hint={mc(pageContract, "chart.pen.hint")}>
            <RateTable buckets={data.pen} unassignedLabel={mc(pageContract, "label.unassigned_pen")} ariaLabel={mc(pageContract, "chart.pen.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.kid_adult.title")} hint={mc(pageContract, "chart.kid_adult.hint")}>
            <RateTable buckets={data.kid_adult} unassignedLabel={mc(pageContract, "label.unassigned_stage")} ariaLabel={mc(pageContract, "chart.kid_adult.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.sex.title")} hint={mc(pageContract, "chart.sex.hint")}>
            <RateTable buckets={data.sex} unassignedLabel={mc(pageContract, "label.unassigned_sex")} ariaLabel={mc(pageContract, "chart.sex.title")} {...rateLabels} />
          </ChartCard>
          {showSpecies ? (
            <ChartCard title={mc(pageContract, "chart.species.title")} hint={mc(pageContract, "chart.species.hint")}>
              <RateTable buckets={data.species} unassignedLabel={mc(pageContract, "label.unassigned_species")} ariaLabel={mc(pageContract, "chart.species.title")} {...rateLabels} />
            </ChartCard>
          ) : null}
          {showParks ? (
            <ChartCard title={mc(pageContract, "chart.park.title")} hint={mc(pageContract, "chart.park.hint")}>
              <RateTable buckets={data.park} unassignedLabel={mc(pageContract, "label.unassigned_park")} ariaLabel={mc(pageContract, "chart.park.title")} {...rateLabels} />
            </ChartCard>
          ) : null}
        </div>
      </section>

      {/* COUNT series: facts about the death alone. */}
      <section aria-label={mc(pageContract, "section.counts.aria")}>
        <div className="mortality-grid">
          <ChartCard title={mc(pageContract, "chart.cause.title")} hint={mc(pageContract, "chart.cause.hint")}>
            <ShareTable
              buckets={data.cause}
              totalDeaths={totals.deaths}
              basisLabels={basisLabels}
              deathsLabel={mc(pageContract, "series.deaths")}
              shareLabel={mc(pageContract, "label.share")}
              emptyLabel={emptyChart}
              ariaLabel={mc(pageContract, "chart.cause.title")}
            />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.age.title")} hint={mc(pageContract, "chart.age.hint")}>
            <ShareTable
              buckets={data.age_at_death}
              totalDeaths={totals.deaths}
              deathsLabel={mc(pageContract, "series.deaths")}
              shareLabel={mc(pageContract, "label.share")}
              emptyLabel={emptyChart}
              ariaLabel={mc(pageContract, "chart.age.title")}
            />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.season.title")} hint={mc(pageContract, "chart.season.hint")}>
            <ShareTable
              buckets={data.season}
              totalDeaths={totals.deaths}
              deathsLabel={mc(pageContract, "series.deaths")}
              shareLabel={mc(pageContract, "label.share")}
              emptyLabel={emptyChart}
              ariaLabel={mc(pageContract, "chart.season.title")}
            />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.arrival.title")} hint={mc(pageContract, "chart.arrival.hint")}>
            <ShareTable
              buckets={data.days_since_arrival}
              totalDeaths={totals.deaths}
              deathsLabel={mc(pageContract, "series.deaths")}
              shareLabel={mc(pageContract, "label.share")}
              emptyLabel={emptyChart}
              ariaLabel={mc(pageContract, "chart.arrival.title")}
            />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.vaccine.title")} hint={mc(pageContract, "chart.vaccine.hint")}>
            <ShareTable
              buckets={data.days_since_vaccination}
              totalDeaths={totals.deaths}
              deathsLabel={mc(pageContract, "series.deaths")}
              shareLabel={mc(pageContract, "label.share")}
              emptyLabel={emptyChart}
              ariaLabel={mc(pageContract, "chart.vaccine.title")}
            />
          </ChartCard>
        </div>
      </section>

      {/* Cross tabs. */}
      <section aria-label={mc(pageContract, "section.cross.aria")}>
        <div className="mortality-grid mortality-grid-wide">
          <ChartCard title={mc(pageContract, "cross.season_stage.title")} hint={mc(pageContract, "cross.season_stage.hint")}>
            <CrossTable cells={data.season_by_stage} totalLabel={mc(pageContract, "cross.total")} emptyLabel={emptyChart} ariaLabel={mc(pageContract, "cross.season_stage.title")} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "cross.load_cause.title")} hint={mc(pageContract, "cross.load_cause.hint")}>
            <CrossTable cells={data.load_by_cause} totalLabel={mc(pageContract, "cross.total")} emptyLabel={emptyChart} ariaLabel={mc(pageContract, "cross.load_cause.title")} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "cross.vendor_cause.title")} hint={mc(pageContract, "cross.vendor_cause.hint")}>
            <CrossTable cells={data.vendor_by_cause} totalLabel={mc(pageContract, "cross.total")} emptyLabel={emptyChart} ariaLabel={mc(pageContract, "cross.vendor_cause.title")} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "cross.breed_cause.title")} hint={mc(pageContract, "cross.breed_cause.hint")}>
            <CrossTable cells={data.breed_by_cause} totalLabel={mc(pageContract, "cross.total")} emptyLabel={emptyChart} ariaLabel={mc(pageContract, "cross.breed_cause.title")} />
          </ChartCard>
        </div>
      </section>

      <section className="card mortality-recent-card" aria-label={mc(pageContract, "table.recent.title")}>
        <h2 className="h">{mc(pageContract, "table.recent.title")}</h2>
        <p className="muted small">{mc(pageContract, "table.recent.hint")}</p>
        <RecentDeathsTable
          contract={table(pageContract, "recent-deaths")}
          rows={data.deaths.map((d) => ({
            goatId: d.goat_id,
            diedOn: fmtDate(d.died_on),
            sortDate: d.died_on,
            tag: d.tag,
            displayId: d.display_id,
            breed: d.breed,
            sex: d.sex,
            stage: d.stage,
            ageDays: d.age_days ?? null,
            ageBandLabel: d.age_band_label,
            park: d.park,
            pen: d.pen,
            loadRef: d.load_ref,
            causeLabel: d.cause_label,
            causeBasis: d.cause_basis,
            basisLabel: basisLabels[d.cause_basis],
          }))}
          ariaLabel={mc(pageContract, "table.recent.title")}
          empty={<span className="muted small">{emptyChart}</span>}
          noDataLabel={mc(pageContract, "label.no_load")}
          daysSuffix={mc(pageContract, "label.days_suffix")}
        />
        {/* The pager reads the page the SERVER actually served (`recent_offset` / `recent_limit`),
            never the one the URL asked for, so a resolved parameter cannot leave the footer
            describing a page the table is not showing. Its total is `totals.deaths`: the list and
            that tile count the same window deaths, which is why paging moves no figure above. */}
        <VaccinationTablePager
          pageContract={pageContract}
          pageSizeOptions={pageSizeOptions}
          page={Math.floor(data.recent_offset / data.recent_limit) + 1}
          pageSize={data.recent_limit}
          total={totals.deaths}
          start={data.deaths.length === 0 ? 0 : data.recent_offset + 1}
          end={data.recent_offset + data.deaths.length}
          noun={mc(pageContract, "table.recent.noun")}
          hrefForPage={(nextPage) => hrefWithParam("md_page", nextPage === 1 ? "" : String(nextPage))}
          /* A size change returns to the first page: page 4 of 10-row pages is a different set of
             animals from page 4 of 50-row pages, and keeping the number would scroll the reader
             somewhere they did not ask to go. */
          hrefForPageSize={(nextSize) => hrefWithParam("md_limit", String(nextSize), { drop: ["md_page"] })}
        />
      </section>
    </div>
  );
}
