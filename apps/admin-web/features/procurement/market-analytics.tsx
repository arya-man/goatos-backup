import { redirect } from "next/navigation";
import { Phone } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { LinkPending } from "@/components/link-pending";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getMarketAnalytics, getMarketSurveyDay } from "@/lib/api/market-server";
import type { MarketAnalytics, MarketLatestCell } from "@/lib/api/market-server";
import { istDayPlus, todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { humanDate, num } from "./sales-format";
import { SalesPageHeader } from "./sales-chrome";
import { MarketTrendSection } from "./market-trend-section";

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
            <LinkPending />
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

          <MarketTrendSection
            series={analytics.series}
            questions={questions}
            cities={cities}
            from={from}
            today={today}
            initialQuestion={selectedQuestion}
            initialCity={selectedCity}
            labels={{
              title: copy(pageContract, "section.trend.title"),
              sub: copy(pageContract, "section.trend.sub"),
              aria: copy(pageContract, "section.trend.aria"),
              questionGroup: copy(pageContract, "filter.question.label"),
              cityGroup: copy(pageContract, "filter.city.label"),
              cityAll: copy(pageContract, "filter.city.all"),
              empty: copy(pageContract, "chart.trend.empty"),
            }}
          />
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
