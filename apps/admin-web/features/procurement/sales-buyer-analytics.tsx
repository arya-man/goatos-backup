import Card from "@mui/material/Card";
import CardHeader, { cardHeaderClasses } from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Typography from "@mui/material/Typography";
import { Label } from "@/components/minimal/label";
import { RingCard } from "@/components/app/ring-card";
import { EmptyState } from "@/components/app/empty-state";
import { KpiWidget } from "@/components/app/kpi-widget";
import { redirect } from "next/navigation";

import {
  controlEnabled,
  copy,
  optionalCopy,
  optionGroup,
  table,
  tablePageSizes,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getBuyerAnalytics } from "@/lib/api/procurement-server";
import type { BuyerAnalytics, BuyerAnalyticsRow } from "@/lib/api/procurement";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { countKey, num } from "./sales-format";
import {
  SALES_DEFAULT_FARM,
  SalesFarmToggle,
  SalesPageHeader,
  hrefWithQuery,
  readSalesParkScope,
} from "./sales-chrome";
import { BuyerTable } from "./buyer-table";
import { ProcurementTableFooter } from "./table-footer-links";
import Alert from "@mui/material/Alert";
import { tableOrderFromParams, type TableOrder } from "./table-order";
import { salesErrorText } from "./sales-error";
import Box from "@mui/material/Box";
import { UrlSuspense } from "@/components/app/url-suspense";
import { SalesBuyerAnalyticsBodySkeleton } from "./sales-skeletons";
import type { SxProps, Theme } from "@mui/material/styles";
import { cardTableScrollSx } from "./procurement-sx";
import { SALES_DEFAULT_LIMIT, SALES_GRID } from "./sales-layout";

// The buyer ledger is a fixed-layout table (template invoice list density) with set column shares;
// it scrolls inside its card from a 70rem floor (65rem on a phone).
const pct = (w: string) => ({ width: w });
const BUYER_TABLE_SX: SxProps<Theme> = {
  ...(cardTableScrollSx as object),
  "& table.sales-buyer-analytics-table": { minWidth: { xs: "65rem", sm: "70rem" }, tableLayout: "fixed" },
  "& table.sales-buyer-analytics-table :is(th, td)": {
    px: { xs: 1.25, sm: 1.375 },
    fontSize: "var(--fs-caption)",
    overflow: "visible",
    textOverflow: "clip",
  },
  "& table.sales-buyer-analytics-table :is(th, td):nth-of-type(1)": { ...pct("17%"), pl: 2.25 },
  "& table.sales-buyer-analytics-table :is(th, td):nth-of-type(2)": pct("12%"),
  "& table.sales-buyer-analytics-table :is(th, td):nth-of-type(3)": pct("10%"),
  "& table.sales-buyer-analytics-table :is(th, td):nth-of-type(4)": pct("7%"),
  "& table.sales-buyer-analytics-table :is(th, td):nth-of-type(5)": pct("12%"),
  "& table.sales-buyer-analytics-table :is(th, td):nth-of-type(6)": pct("15%"),
  "& table.sales-buyer-analytics-table :is(th, td):is(:nth-of-type(7), :nth-of-type(8))": pct("9%"),
  "& table.sales-buyer-analytics-table :is(th, td):nth-of-type(9)": { ...pct("9%"), pr: 2.25 },
  "& table.sales-buyer-analytics-table td .muted.small": { whiteSpace: "normal !important", overflow: "visible", textOverflow: "clip" },
  "& table.sales-buyer-analytics-table td:is(:nth-of-type(2), :nth-of-type(4), :nth-of-type(5), :nth-of-type(7), :nth-of-type(8), :nth-of-type(9))": {
    whiteSpace: "nowrap",
  },
};

const PAGE_PATH = "/sales/buyer-analytics";
/** Only used when an older backend contract carries no buyers table; the contract page size wins. */
const FALLBACK_LIMIT = SALES_DEFAULT_LIMIT;
const MAX_OFFSET = 10000;

/** Fills a backend copy template's `{name}` slots; the sentence itself stays backend-owned. */
function fill(template: string, values: Record<string, string>): string {
  return template.replace(
    /\{(\w+)\}/g,
    (match, key: string) => values[key] ?? match,
  );
}

/**
 * Buyer analytics (maintainer request 2026-09-15): who the farm sells to, one row per buyer --
 * name, phone, category, place, purchases so far, animals, revenue, whether they come back and
 * how often, first and last sale, and what they still owe. Read-only by contract, the /sales/sold
 * shape: the page declares no write control, so nothing here opens a form.
 *
 * The buyer identity and every figure are the backend's (see the procurement buyer read); this
 * renders them verbatim. The phone column follows the page contract's `buyer_phone_column`
 * control AND the payload's `phones_visible` flag: the contract explains WHY the column is
 * absent, the payload is what actually decided.
 */
function BuyerSections({
  analytics,
  pageContract,
  offset,
  limit,
  pageHref,
  limitHref,
  pageSizes,
  order,
}: {
  analytics: BuyerAnalytics;
  pageContract: AdminUiPageContract;
  offset: number;
  limit: number;
  pageHref: (offset: number) => string;
  limitHref: (limit: number) => string;
  pageSizes: readonly number[];
  order: TableOrder;
}) {
  const summary = analytics.summary;
  const none = copy(pageContract, "value.none");
  const phoneControl = pageContract.controls.find(
    (item) => item.id === "buyer_phone_column",
  );
  const showPhones =
    analytics.phones_visible &&
    controlEnabled(pageContract, "buyer_phone_column", false);
  const phoneHiddenReason =
    phoneControl?.disabled_reason || copy(pageContract, "hint.phone_hidden");
  const pageNumber = Math.floor(offset / limit) + 1;
  const pageCount = Math.max(1, Math.ceil(analytics.total_buyers / limit));
  const repeatPct =
    summary.buyers > 0 ? (summary.repeat_buyers / summary.buyers) * 100 : 0;

  // Two short lines under the chip rather than one long sentence, so the column stays narrow
  // enough for the sale dates and the balance to sit on screen beside it.
  const cadence = (row: BuyerAnalyticsRow): string[] => {
    if (!row.repeat) return [];
    const parts = [
      fill(copy(pageContract, "value.repeat_purchases"), {
        count: num(row.repeat_purchases),
      }),
    ];
    if (row.avg_days_between != null) {
      parts.push(
        fill(copy(pageContract, countKey(Math.round(row.avg_days_between), "value.every_day", "value.every_days")), {
          days: num(row.avg_days_between),
        }),
      );
    }
    return parts;
  };
  const recency = (row: BuyerAnalyticsRow): string => {
    if (row.days_since_last == null) return "";
    if (row.days_since_last === 0) return copy(pageContract, "value.today");
    return fill(copy(pageContract, countKey(row.days_since_last, "value.day_ago", "value.days_ago")), {
      days: num(row.days_since_last),
    });
  };

  return (
    <Grid container spacing={3}>
      {/* Headline figures: template CourseWidgetSummary (KpiWidget), two by two
          beside the repeat-share radial -- the Ecommerce overview's widget + Sale-by-gender row. */}
      <Grid size={SALES_GRID.main}>
        <Grid container spacing={3} sx={{ height: 1 }}>
          <Grid size={SALES_GRID.half}>
            <KpiWidget
              color="primary"
              title={copy(pageContract, "kpi.buyers")}
              total={summary.buyers}
              // Older contracts lack this optional standalone detail; never reuse the legacy key,
              // whose sentence follows an unregistered-buyer count (0dd2097e3).
              caption={optionalCopy(pageContract, "kpi.buyers.closed_sale_detail") ? copy(pageContract, "kpi.buyers.closed_sale_detail") : undefined}
              sx={{ height: 1 }}
            />
          </Grid>
          <Grid size={SALES_GRID.half}>
            <KpiWidget
              color="info"
              icon="certificates"
              title={copy(pageContract, "kpi.repeat_buyers")}
              total={summary.repeat_buyers}
              caption={`${num(repeatPct, 0)}% · ${copy(pageContract, "kpi.repeat_buyers.detail")}`}
              sx={{ height: 1 }}
            />
          </Grid>
          <Grid size={SALES_GRID.half}>
            <KpiWidget
              color="success"
              title={copy(pageContract, "kpi.repeat_revenue")}
              total={summary.repeat_revenue}
              caption={[`₹`, `${num(summary.repeat_revenue_pct, 0)}% ${copy(pageContract, "kpi.repeat_revenue.detail")} · ${num(summary.purchases)} ${copy(pageContract, "kpi.purchases.detail")}`].filter(Boolean).join(" · ")}
              sx={{ height: 1 }}
            />
          </Grid>
          <Grid size={SALES_GRID.half}>
            <KpiWidget
              color="warning"
              title={copy(pageContract, "kpi.outstanding")}
              total={summary.outstanding}
              caption={[`₹`, copy(pageContract, "kpi.outstanding.detail")].filter(Boolean).join(" · ")}
              sx={{ height: 1 }}
            />
          </Grid>
        </Grid>
      </Grid>

      {/* The two shares the summary already carries, as the template Sale-by-gender radial. Both
          numbers are the backend's own percentages -- nothing is derived here that the figures
          beside it did not already print. */}
      <Grid size={SALES_GRID.side}>
        <RingCard
          title={copy(pageContract, "kpi.repeat_buyers")}
          total={summary.repeat_buyers}
          totalLabel={`/ ${num(summary.buyers)} ${copy(pageContract, "kpi.buyers")}`}
          series={[
              { label: copy(pageContract, "kpi.repeat_buyers"), value: Math.round(repeatPct * 10) / 10, display: `${num(repeatPct, 0)}%` },
              { label: copy(pageContract, "kpi.repeat_revenue"), value: Math.round(summary.repeat_revenue_pct * 10) / 10, display: `${num(summary.repeat_revenue_pct, 0)}%` },
            ]}
          sx={{ height: 1 }}
        />
      </Grid>

      <Grid size={12}>
      {/* Template table card: CardHeader with the whole-list count, the buyer table, pager. */}
      <Card component="section" aria-label={copy(pageContract, "section.buyers.title")}>
        <CardHeader
          title={copy(pageContract, "section.buyers.title")}
          action={
            <Label variant="soft" color={analytics.total_buyers ? "info" : "default"}>
              {num(analytics.total_buyers)} {copy(pageContract, "summary.buyers")}
            </Label>
          }
          sx={{ mb: 3, [`& .${cardHeaderClasses.action}`]: { alignSelf: "center" } }}
        />
        {!showPhones ? (
          <Typography variant="body2" color="text.secondary" sx={{ px: 3, mb: 2 }}>
            {phoneHiddenReason}
          </Typography>
        ) : null}
        <Box
          tabIndex={0}
          role="region"
          aria-label={copy(pageContract, "section.buyers.title")}
          id="sales-buyers-analytics"
          sx={BUYER_TABLE_SX}
        >
          <BuyerTable
            contract={table(pageContract, "sales-buyer-analytics")}
            rows={analytics.buyers.map((row) => ({
              ...row,
              cadence_lines: cadence(row),
              recency: recency(row),
            }))}
            showPhones={showPhones}
            order={order}
            labels={{
              sortAll: copy(pageContract, "table.sort_all"),
              ariaLabel: copy(pageContract, "section.buyers.title"),
              none,
              repeat: copy(pageContract, "chip.repeat"),
              oneTime: copy(pageContract, "chip.one_time"),
              settled: copy(pageContract, "value.settled"),
              empty: (
                <EmptyState title={copy(pageContract, "empty.buyers")} />
              ),
            }}
          />
        </Box>
        {pageCount > 1 ? (
          <ProcurementTableFooter
            denseLabel={copy(pageContract, "action.dense", "Dense")}
            rowsLabel={copy(pageContract, "pager.rows", "Rows")}
            page={pageNumber}
            pageCount={pageCount}
            rangeLabel={`${offset + 1}\u2013${Math.min(offset + limit, analytics.total_buyers)} ${copy(pageContract, "pager.of")} ${num(analytics.total_buyers)} ${copy(pageContract, "summary.buyers")}`}
            rowsValue={limit}
            rowsOptions={pageSizes.map((size) => ({ size, href: limitHref(size) }))}
            prevHref={pageHref(Math.max(0, (pageNumber - 2) * limit))}
            nextHref={pageHref(pageNumber * limit)}
            prevLabel={copy(pageContract, "action.prev_page")}
            nextLabel={copy(pageContract, "action.next_page")}
            denseTargetId="sales-buyers-analytics"
          />
        ) : null}
      </Card>
      </Grid>
    </Grid>
  );
}

export async function SalesBuyerAnalyticsPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;

  // Park scope: the SHELL's `park` (one filter across every Sales page), resolved to the deal farm
  // code the buyer read filters by.
  const { parkId, farm, parks } = await readSalesParkScope(sp, pageContract, PAGE_PATH);

  return (
    <div className="screen on sales-buyer-analytics-page">
      <SalesPageHeader pageContract={pageContract} />

      <SalesFarmToggle
        pageContract={pageContract}
        pagePath={PAGE_PATH}
        searchParams={sp}
        parkId={parkId}
        parks={parks}
        clears={["offset"]}
      />

      {/* The buyer read streams (guard: url-keyed-panel): a farm / sort / page click swaps it to its
          skeleton at once; header and farm chips stay on screen. */}
      <UrlSuspense searchParams={sp} watch={BUYERS_WATCH} fallback={<SalesBuyerAnalyticsBodySkeleton limit={buyerLimit(sp, pageContract).limit} />}>
        <BuyerAnalyticsPanel sp={sp} pageContract={pageContract} farm={farm} />
      </UrlSuspense>
    </div>
  );
}

/** The buyer table's page size: the URL `limit` when the contract offers it, else its first size. */
function buyerLimit(sp: RouteSearchParams, pageContract: AdminUiPageContract) {
  const pageSizes = tablePageSizes(pageContract, "sales-buyer-analytics");
  const defaultLimit = pageSizes[0] ?? FALLBACK_LIMIT;
  return { pageSizes, defaultLimit, limit: resolveLimit(one(sp, "limit"), pageSizes, defaultLimit) };
}

/** The params the buyer read takes. */
const BUYERS_WATCH = ["park", "farm", "limit", "offset", "sort", "dir"] as const;

async function BuyerAnalyticsPanel({ sp, pageContract, farm }: { sp: RouteSearchParams; pageContract: AdminUiPageContract; farm: string }) {
  // Page size is the contract's; the offset is bounded to the backend's own ceiling.
  const { pageSizes, defaultLimit, limit } = buyerLimit(sp, pageContract);
  const offset = boundedInt(one(sp, "offset"), 0, 0, MAX_OFFSET);

  // The table's whole-result order, validated against the contract's sortable columns.
  const order = tableOrderFromParams(sp, table(pageContract, "sales-buyer-analytics"));
  // serial-await: allow buyer analytics depends on readSalesParkScope mapping shell park to deal farm.
  const result = await getBuyerAnalytics({
    farm: farm === SALES_DEFAULT_FARM ? undefined : farm,
    limit,
    offset,
    sort: order.sort || undefined,
    dir: order.sort ? order.dir : undefined,
  });
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);

  // The pager keeps the page's park and every other parameter; only the offset moves.
  const pageHref = (nextOffset: number) =>
    hrefWithQuery(PAGE_PATH, sp, { offset: nextOffset > 0 ? String(nextOffset) : null });
  // Changing the page size always returns to the first page: keeping the offset would drop the
  // reader into the middle of a differently-sized list. The park and every other parameter stay.
  const limitHref = (nextLimit: number) =>
    hrefWithQuery(PAGE_PATH, sp, { limit: nextLimit === defaultLimit ? null : String(nextLimit), offset: null });

  return (
    <>
      {!result.ok ? (
        <Alert severity="error" sx={{ mb: 3 }}>
          {salesErrorText(result.error, copy(pageContract, "error.load"))}
        </Alert>
      ) : null}

      {result.ok ? (
        <BuyerSections
          analytics={result.data}
          pageContract={pageContract}
          offset={result.data.offset}
          limit={result.data.limit}
          pageHref={pageHref}
          limitHref={limitHref}
          pageSizes={pageSizes}
          order={order}
        />
      ) : null}
    </>
  );
}

/** A requested page size is honoured only when the contract offers it. */
function resolveLimit(
  raw: string | undefined,
  offered: readonly number[],
  fallback: number,
): number {
  const parsed = raw ? Number.parseInt(raw, 10) : Number.NaN;
  if (Number.isFinite(parsed) && offered.includes(parsed)) return parsed;
  return fallback;
}
