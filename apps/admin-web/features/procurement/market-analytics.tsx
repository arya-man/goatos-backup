import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { Caption } from "@/components/app/caption";
import { EmptyState } from "@/components/app/empty-state";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import { redirect } from "next/navigation";
import { Building2, CalendarRange, CircleCheck, Clock, Phone } from "lucide-react";

import Link from "@/components/no-prefetch-link";
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
import Alert from "@mui/material/Alert";
import { MarketTrendSection } from "./market-trend-section";
import { salesErrorText } from "./sales-error";
import Box from "@mui/material/Box";

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
        <Alert severity="error" style={{ marginBottom: 14 }}>
          {salesErrorText(analyticsResult.error, copy(pageContract, "error.load"))}
        </Alert>
      ) : null}

      <Box sx={{ mt: 1.5 }}>
      <KpiGrid>
        <KpiCard
          variant="gradient"
          tone="primary"
          label={copy(pageContract, "kpi.cities.label")}
          value={cities.length}
          watermark={<Building2 aria-hidden="true" />}
        />
        <KpiCard
          variant="tint"
          tone="info"
          label={copy(pageContract, "kpi.days.label")}
          value={analytics.days}
          watermark={<CalendarRange aria-hidden="true" />}
        />
        <KpiCard
          variant="tint"
          tone="violet"
          label={copy(pageContract, "kpi.latest.label")}
          value={latestDate ? <Box component="span" sx={{ whiteSpace: "nowrap", overflowWrap: "normal", wordBreak: "normal", fontSize: { xs: "clamp(1.0625rem, 5.2vw, 1.375rem)", sm: "inherit" } }}>{humanDate(latestDate)}</Box> : none}
          watermark={<Clock aria-hidden="true" />}
        />
        <KpiCard
          variant="tint"
          tone="success"
          label={copy(pageContract, "kpi.coverage.label")}
          value={day ? `${num(day.done)} / ${num(day.done + day.pending)}` : none}
          watermark={<CircleCheck aria-hidden="true" />}
        />
      </KpiGrid>
      </Box>

      <Box sx={{ mt: 1.5, mb: 1.75 }}>

        <AnimatedTabs
          variant="pill"
          ariaLabel={copy(pageContract, "filter.window.label")}
          value={String(windowDays)}
          items={WINDOWS.map((w) => ({
            value: String(w),
            label: copy(pageContract, `filter.window.${w}`),
            href: hrefWithQuery(sp, { window: w === DEFAULT_WINDOW ? null : String(w) }),
          }))}
        />

      </Box>

      {analytics.series.length === 0 ? (
        <div style={{ marginTop: 12 }}>
          <EmptyState
            title={day && day.cards.length === 0 ? copy(pageContract, "empty.config") : copy(pageContract, "empty.analytics")}
            action={<Link href="/sales/config" className="btn">{copy(pageContract, "link.sales_config")}</Link>}
          />
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
            <Caption>{copy(pageContract, "section.latest.sub")}</Caption>
            <div className="twrap" tabIndex={0} role="region" aria-label={copy(pageContract, "section.latest.title")}>
              <Table className="market-latest-table">
                <TableHead>
                  <TableRow>
                    <TableCell component="th">{copy(pageContract, "column.city")}</TableCell>
                    {latestQuestions.map((q) => (
                      <TableCell component="th" key={q.id} className="num">
                        {q.label}
                      </TableCell>
                    ))}
                    <TableCell component="th">{copy(pageContract, "column.recorded_on")}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {latestCities.map((city) => {
                    const row = latestByCity.get(city.id);
                    const rowDate = row ? [...row.values()].reduce((max, c) => (c.business_date > max ? c.business_date : max), "") : "";
                    return (
                      <TableRow key={city.id}>
                        <TableCell data-label={copy(pageContract, "column.city")}>
                          <b>{city.label}</b>
                        </TableCell>
                        {latestQuestions.map((q) => {
                          const cell = row?.get(q.id);
                          if (!cell) {
                            return (
                              <TableCell key={q.id} className="num muted" data-label={q.label}>
                                {none}
                              </TableCell>
                            );
                          }
                          const delta = cell.previous_price == null ? null : cell.price - cell.previous_price;
                          return (
                            <TableCell key={q.id} className="num" data-label={q.label} title={`${copy(pageContract, "column.recorded_on")} ${humanDate(cell.business_date)}`}>
                              <Box component="span" sx={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 0.25 }}>
                              <b>{priceWithUnit(cell.price, cell.unit_label)}</b>
                              <span className="small"style={{ color: delta == null ? "var(--muted)" : delta > 0 ? "var(--ok)" : delta < 0 ? "var(--danger)" : "var(--muted)" }}>
                                {delta == null
                                  ? copy(pageContract, "value.no_previous")
                                  : `${delta > 0 ? "▲" : delta < 0 ? "▼" : "•"} ${num(Math.abs(delta), Number.isInteger(delta) ? 0 : 2)}`}
                              </span>
                              </Box>
                            </TableCell>
                          );
                        })}
                        <TableCell className="muted small" data-label={copy(pageContract, "column.recorded_on")}>{rowDate ? humanDate(rowDate) : none}</TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
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
