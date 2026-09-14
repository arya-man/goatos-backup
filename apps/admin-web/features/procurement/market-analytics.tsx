import { redirect } from "next/navigation";
import { Phone } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { ChartHover } from "@/components/chart-hover";
import { SeriesLegend, SeriesLines, seriesColorVar, type LineSeries } from "@/components/svg-series";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getMarketAnalytics, getMarketSurveyDay } from "@/lib/api/market-server";
import type { MarketAnalytics, MarketLatestCell, MarketSeries } from "@/lib/api/market-server";
import { istDayPlus, todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { humanDate, num } from "./sales-format";
import { SalesPageHeader } from "./sales-chrome";

const PAGE_PATH = "/sales/market-analytics";
/** The window chips, in days. 90 is the backend's own default. */
const WINDOWS = [30, 90, 180, 365] as const;
const DEFAULT_WINDOW = 90;

function hrefWithQuery(sp: RouteSearchParams, patch: Record<string, string | null>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    const single = Array.isArray(value) ? value[0] : value;
    if (single) query.set(key, single);
  }
  for (const [key, value] of Object.entries(patch)) {
    if (value === null || value === "") query.delete(key);
    else query.set(key, value);
  }
  const qs = query.toString();
  return qs ? `${PAGE_PATH}?${qs}` : PAGE_PATH;
}

/** A price in the words it was recorded with: "₹620 / kg" from 620 + "₹/kg". */
function priceWithUnit(price: number, unit: string): string {
  const figure = Number.isInteger(price) ? num(price) : num(price, 2);
  const bare = unit.replace(/^₹\s*/, "");
  return bare.startsWith("/") ? `₹${figure} ${bare}` : `₹${figure} ${unit}`;
}

/**
 * Market analytics (maintainer decision 2026-09-14): what goat and sheep fetch in the markets
 * the procurement director phones each morning, read back over time.
 *
 * READ-ONLY BY CONTRACT: the backend page declares no write control, so nothing here can open a
 * form. Prices are entered on the phone's Market tab; the cities and questions are Sales Config.
 * Question labels, unit labels and city names are the farm's own config, served on every row as
 * the words the price was recorded against -- none of them is composed here.
 *
 * ONE LINE PER (city, question, unit): a question whose unit was changed starts a new line from
 * that morning, so the chart never rescales an old price into a new unit. Filters are
 * server-rendered links so the choice survives a reload and a shared URL.
 */
export async function MarketAnalyticsPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;
  const rawWindow = Number(one(sp, "window") ?? DEFAULT_WINDOW);
  const windowDays = (WINDOWS as readonly number[]).includes(rawWindow) ? rawWindow : DEFAULT_WINDOW;
  const today = todayIso();
  const from = istDayPlus(today, -(windowDays - 1));

  const [analyticsResult, dayResult] = await Promise.all([getMarketAnalytics({ from, to: today }), getMarketSurveyDay()]);
  if (firstAuthRequiredError(analyticsResult)) redirect(INTERNAL_LOGIN_PATH);

  const analytics: MarketAnalytics = analyticsResult.ok ? analyticsResult.data : { from, to: today, days: 0, latest: [], series: [] };
  const day = dayResult.ok ? dayResult.data : null;
  const none = copy(pageContract, "value.none");

  // The filter vocabularies come from the DATA, in the order the entries carry them: a question
  // is a chip only if a price was ever recorded for it in this window.
  const questions = uniqueBy(analytics.series, (s) => s.question_id, (s) => s.question_label);
  const cities = uniqueBy(analytics.series, (s) => s.city_id, (s) => s.city_name);
  const rawQuestion = one(sp, "question") ?? "";
  const selectedQuestion = questions.some((q) => q.id === rawQuestion) ? rawQuestion : questions[0]?.id ?? "";
  const rawCity = one(sp, "city") ?? "";
  const selectedCity = cities.some((c) => c.id === rawCity) ? rawCity : "";

  // Latest table: cities down, questions across, in config order as the entries carry it.
  const latestByCity = new Map<string, Map<string, MarketLatestCell>>();
  for (const cell of analytics.latest) {
    const row = latestByCity.get(cell.city_id) ?? new Map<string, MarketLatestCell>();
    row.set(cell.question_id, cell);
    latestByCity.set(cell.city_id, row);
  }
  const latestQuestions = uniqueBy(analytics.latest, (c) => c.question_id, (c) => c.question_label);
  const latestCities = uniqueBy(analytics.latest, (c) => c.city_id, (c) => c.city_name);
  const latestDate = analytics.latest.reduce((max, c) => (c.business_date > max ? c.business_date : max), "");

  // Trend chart: the selected question, one line per city (and per unit within a city), on a
  // shared day axis. The axis starts on the FIRST morning any price was recorded inside the
  // window rather than on the window's first day, so a survey that began last week is not
  // squeezed into the right edge of a 90-day axis. Days with no price are null so the line
  // breaks honestly rather than interpolating a morning nobody phoned.
  const trendSeries: MarketSeries[] = analytics.series.filter(
    (s) => s.question_id === selectedQuestion && (selectedCity === "" || s.city_id === selectedCity),
  );
  const firstRecorded = analytics.series.reduce((min, s) => {
    const first = s.points[0]?.business_date ?? "";
    return first && (min === "" || first < min) ? first : min;
  }, "");
  const axisFrom = firstRecorded && firstRecorded > from ? firstRecorded : from;
  const dayKeys: string[] = [];
  for (let d = axisFrom; d <= today; d = istDayPlus(d, 1)) dayKeys.push(d);
  const dayIndex = new Map(dayKeys.map((d, i) => [d, i] as const));
  const lines: LineSeries[] = trendSeries.map((s, i) => {
    const points: (number | null)[] = dayKeys.map(() => null);
    for (const p of s.points) {
      const idx = dayIndex.get(p.business_date);
      if (idx !== undefined) points[idx] = p.price;
    }
    return { label: `${s.city_name} · ${s.unit_label}`, colorVar: seriesColorVar(i), points };
  });
  // Axis labels, thinned so a long axis stays legible: every day up to two weeks, then weekly,
  // fortnightly, monthly.
  const span = dayKeys.length;
  const step = span <= 14 ? 1 : span <= 90 ? 7 : span <= 180 ? 14 : 30;
  const dayLabels = dayKeys.map((d, i) => (i % step === 0 ? humanDate(d) : ""));
  const selectedQuestionLabel = questions.find((q) => q.id === selectedQuestion)?.label ?? "";
  const selectedUnit = trendSeries[0]?.unit_label ?? "";

  return (
    <div className="screen on market-analytics-page">
      <SalesPageHeader pageContract={pageContract} />

      {!analyticsResult.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{analyticsResult.error.code ?? analyticsResult.error.kind}</b>&nbsp;
          {analyticsResult.error.message || copy(pageContract, "error.load")}
        </div>
      ) : null}

      <div className="grid g4 kpi-row" style={{ marginTop: 12 }}>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.cities.label")}</div>
          <div className="val">{num(cities.length)}</div>
          <div className="dl">{copy(pageContract, "kpi.cities.hint")}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.days.label")}</div>
          <div className="val">{num(analytics.days)}</div>
          <div className="dl">{copy(pageContract, "kpi.days.hint")}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.latest.label")}</div>
          <div className="val">{latestDate ? humanDate(latestDate) : none}</div>
          <div className="dl">{copy(pageContract, "kpi.latest.hint")}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.coverage.label")}</div>
          <div className="val">{day ? `${num(day.done)} / ${num(day.done + day.pending)}` : none}</div>
          <div className="dl">{copy(pageContract, "kpi.coverage.hint")}</div>
        </div>
      </div>

      <div className="chips" role="group" aria-label={copy(pageContract, "filter.window.label")} style={{ marginTop: 12 }}>
        {WINDOWS.map((w) => (
          <Link
            key={w}
            href={hrefWithQuery(sp, { window: w === DEFAULT_WINDOW ? null : String(w) })}
            scroll={false}
            className={w === windowDays ? "btn sm p" : "btn sm"}
            aria-current={w === windowDays ? "true" : undefined}
          >
            {copy(pageContract, `filter.window.${w}`)}
          </Link>
        ))}
      </div>

      {analytics.series.length === 0 ? (
        <div className="empty" style={{ marginTop: 12 }}>
          {day && day.cards.length === 0 ? copy(pageContract, "empty.config") : copy(pageContract, "empty.analytics")}{" "}
          <Link href="/sales/config">{copy(pageContract, "link.sales_config")}</Link>
        </div>
      ) : (
        <>
          <section className="card" style={{ marginTop: 12 }} aria-label={copy(pageContract, "section.latest.aria")}>
            <div className="hd">
              <Phone className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
              <h3>{copy(pageContract, "section.latest.title")}</h3>
              <div className="sp" style={{ flex: 1 }} />
              {latestDate ? <Tag tone="info">{humanDate(latestDate)}</Tag> : null}
            </div>
            <p className="muted small">{copy(pageContract, "section.latest.sub")}</p>
            <div className="twrap" tabIndex={0} role="region" aria-label={copy(pageContract, "section.latest.title")}>
              <table className="market-latest-table">
                <thead>
                  <tr>
                    <th>{copy(pageContract, "column.city")}</th>
                    {latestQuestions.map((q) => (
                      <th key={q.id} className="num">
                        {q.label}
                      </th>
                    ))}
                    <th>{copy(pageContract, "column.recorded_on")}</th>
                  </tr>
                </thead>
                <tbody>
                  {latestCities.map((city) => {
                    const row = latestByCity.get(city.id);
                    const rowDate = row ? [...row.values()].reduce((max, c) => (c.business_date > max ? c.business_date : max), "") : "";
                    return (
                      <tr key={city.id}>
                        <td>
                          <b>{city.label}</b>
                        </td>
                        {latestQuestions.map((q) => {
                          const cell = row?.get(q.id);
                          if (!cell) {
                            return (
                              <td key={q.id} className="num muted">
                                {none}
                              </td>
                            );
                          }
                          const delta = cell.previous_price == null ? null : cell.price - cell.previous_price;
                          return (
                            <td key={q.id} className="num" title={`${copy(pageContract, "column.recorded_on")} ${humanDate(cell.business_date)}`}>
                              <b>{priceWithUnit(cell.price, cell.unit_label)}</b>
                              <div className="small" style={{ color: delta == null ? "var(--mut)" : delta > 0 ? "var(--ok)" : delta < 0 ? "var(--danger)" : "var(--mut)" }}>
                                {delta == null
                                  ? copy(pageContract, "value.no_previous")
                                  : `${delta > 0 ? "▲" : delta < 0 ? "▼" : "•"} ${num(Math.abs(delta), Number.isInteger(delta) ? 0 : 2)}`}
                              </div>
                            </td>
                          );
                        })}
                        <td className="muted small">{rowDate ? humanDate(rowDate) : none}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </section>

          <section className="card wchart" style={{ marginTop: 12 }} aria-label={copy(pageContract, "section.trend.aria")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.trend.title")}</h3>
              <div className="sp" style={{ flex: 1 }} />
              {selectedUnit ? <Tag tone="mut">{selectedUnit}</Tag> : null}
            </div>
            <p className="muted small">{copy(pageContract, "section.trend.sub")}</p>
            <div className="chips" role="group" aria-label={copy(pageContract, "filter.question.label")}>
              {questions.map((q) => (
                <Link
                  key={q.id}
                  href={hrefWithQuery(sp, { question: q.id === questions[0]?.id ? null : q.id })}
                  scroll={false}
                  className={q.id === selectedQuestion ? "btn sm p" : "btn sm"}
                  aria-current={q.id === selectedQuestion ? "true" : undefined}
                >
                  {q.label}
                </Link>
              ))}
            </div>
            <div className="chips" role="group" aria-label={copy(pageContract, "filter.city.label")} style={{ marginTop: 6 }}>
              <Link href={hrefWithQuery(sp, { city: null })} scroll={false} className={selectedCity === "" ? "btn sm p" : "btn sm"} aria-current={selectedCity === "" ? "true" : undefined}>
                {copy(pageContract, "filter.city.all")}
              </Link>
              {cities.map((c) => (
                <Link
                  key={c.id}
                  href={hrefWithQuery(sp, { city: c.id })}
                  scroll={false}
                  className={c.id === selectedCity ? "btn sm p" : "btn sm"}
                  aria-current={c.id === selectedCity ? "true" : undefined}
                >
                  {c.label}
                </Link>
              ))}
            </div>
            <ChartHover>
              <SeriesLines
                series={lines}
                dayLabels={dayLabels}
                valueNoun={selectedUnit || selectedQuestionLabel}
                chartLabel={`${copy(pageContract, "section.trend.title")} · ${selectedQuestionLabel}`}
                emptyLabel={copy(pageContract, "chart.trend.empty")}
              />
            </ChartHover>
            <SeriesLegend entries={lines.map((l) => ({ label: l.label, colorVar: l.colorVar }))} />
          </section>
        </>
      )}
    </div>
  );
}

function uniqueBy<T>(items: T[], key: (item: T) => string, label: (item: T) => string): { id: string; label: string }[] {
  const seen = new Map<string, string>();
  for (const item of items) {
    const k = key(item);
    if (!seen.has(k)) seen.set(k, label(item));
  }
  return [...seen.entries()].map(([id, l]) => ({ id, label: l }));
}
