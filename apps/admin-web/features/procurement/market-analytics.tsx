import Table from "@mui/material/Table";
import { UrlSuspense } from "@/components/app/url-suspense";
import { SalesMarketKpisSkeleton, SalesMarketPanelsSkeleton } from "./sales-skeletons";
import { TableHeadCustom } from "@/components/app/table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { EmptyState } from "@/components/app/empty-state";
import { KpiGrid } from "@/components/app/kpi-grid";
import { KpiWidget } from "@/components/app/kpi-widget";
import { SegmentTabs } from "@/components/app/list/segment-tabs";
import { redirect } from "next/navigation";

import Link from "@/components/no-prefetch-link";
import { Label } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
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
import Button from "@mui/material/Button";
import Stack from "@mui/material/Stack";
import { MARKET_WINDOWS } from "./market-analytics-layout";
import { PageRoot } from "@/components/app/page-root";

const PAGE_PATH = "/sales/market-analytics";
/** The window chips, in days. 90 is the backend's own default. */
const WINDOWS = MARKET_WINDOWS;
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
    <PageRoot>
      <SalesPageHeader pageContract={pageContract} />

      {!analyticsResult.ok ? (
        <Alert severity="error" sx={{ mb: 3 }}>
          {salesErrorText(analyticsResult.error, copy(pageContract, "error.load"))}
        </Alert>
      ) : null}

      <Stack spacing={3}>
      {/* The window's figures (guard: url-keyed-panel): a window click swaps the KPI deck and the
          price panels to their skeletons at once; header and window strip stay on screen. */}
      <UrlSuspense searchParams={sp} watch={WINDOW_WATCH} fallback={<SalesMarketKpisSkeleton />}>
      <KpiGrid>
        <KpiWidget color="primary" title={copy(pageContract, "kpi.cities.label")} total={cities.length} />
        <KpiWidget color="info" title={copy(pageContract, "kpi.days.label")} total={analytics.days} icon="certificates" />
        {/* The template widget figure is a number: the latest survey date is the visible sub-line. */}
        <KpiWidget
          color="secondary"
          title={copy(pageContract, "kpi.latest.label")}
          total={null}
          caption={latestDate ? humanDate(latestDate) : none}
        />
        <KpiWidget
          color="success"
          title={copy(pageContract, "kpi.coverage.label")}
          total={day ? day.done : null}
          caption={day ? `of ${num(day.done + day.pending)}` : none}
          icon="completed"
        />
      </KpiGrid>
      </UrlSuspense>

      <Box>

        <SegmentTabs
          keepScroll
          ariaLabel={copy(pageContract, "filter.window.label")}
          value={String(windowDays)}
          tabs={WINDOWS.map((w) => ({
            value: String(w),
            label: copy(pageContract, `filter.window.${w}`),
            href: hrefWithQuery(sp, { window: w === DEFAULT_WINDOW ? null : String(w) }),
          }))}
        />

      </Box>

      <UrlSuspense searchParams={sp} watch={WINDOW_WATCH} fallback={<SalesMarketPanelsSkeleton />}>
      {analytics.series.length === 0 ? (
        <Card>
          <EmptyState sx={{ py: 10 }}
            title={day && day.cards.length === 0 ? copy(pageContract, "empty.config") : copy(pageContract, "empty.analytics")}
            action={<Button component={Link} href="/sales/config" variant="contained" color="primary">{copy(pageContract, "link.sales_config")}</Button>}
          />
        </Card>
      ) : (
        <>
          {/* Latest prices: template table card (CardHeader title + subheader + date Label). */}
          <Card aria-label={copy(pageContract, "section.latest.aria")} component="section">
            <CardHeader
              title={copy(pageContract, "section.latest.title")}
              subheader={copy(pageContract, "section.latest.sub")}
              action={latestDate ? <Label variant="soft" color="info">{humanDate(latestDate)}</Label> : null}
            />
            <Box sx={{ mt: 3 }}>
            <Scrollbar>
            <Box tabIndex={0} role="region" aria-label={copy(pageContract, "section.latest.title")}>
              <Table sx={{ minWidth: 560 }}>
                <TableHeadCustom
                  headCells={[
                    { id: "city", label: copy(pageContract, "column.city") },
                    ...latestQuestions.map((q) => ({ id: `q-${q.id}`, label: q.label, align: "right" as const })),
                    { id: "recorded_on", label: copy(pageContract, "column.recorded_on") },
                  ]}
                />
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
                              <TableCell key={q.id} align="right" data-label={q.label} sx={{ color: "text.secondary" }}>
                                {none}
                              </TableCell>
                            );
                          }
                          const delta = cell.previous_price == null ? null : cell.price - cell.previous_price;
                          return (
                            <TableCell key={q.id} align="right" data-label={q.label} title={`${copy(pageContract, "column.recorded_on")} ${humanDate(cell.business_date)}`}>
                              <Box component="span" sx={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 0.25 }}>
                              <b>{priceWithUnit(cell.price, cell.unit_label)}</b>
                              <Box component="span" sx={{ typography: "caption", color: delta == null ? "text.secondary" : delta > 0 ? "success.main" : delta < 0 ? "error.main" : "text.secondary" }}>
                                {delta == null
                                  ? copy(pageContract, "value.no_previous")
                                  : `${delta > 0 ? "▲" : delta < 0 ? "▼" : "•"} ${num(Math.abs(delta), Number.isInteger(delta) ? 0 : 2)}`}
                              </Box>
                              </Box>
                            </TableCell>
                          );
                        })}
                        <TableCell data-label={copy(pageContract, "column.recorded_on")} sx={{ color: "text.secondary" }}>{rowDate ? humanDate(rowDate) : none}</TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </Box>
            </Scrollbar>
            </Box>
          </Card>

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
      </UrlSuspense>
      </Stack>
    </PageRoot>
  );
}

/** The one param the market read takes (question / city pick the trend client-side). */
const WINDOW_WATCH = ["window"] as const;

function uniqueBy<T>(items: T[], key: (item: T) => string, label: (item: T) => string): { id: string; label: string }[] {
  const seen = new Map<string, string>();
  for (const item of items) {
    const k = key(item);
    if (!seen.has(k)) seen.set(k, label(item));
  }
  return [...seen.entries()].map(([id, l]) => ({ id, label: l }));
}
