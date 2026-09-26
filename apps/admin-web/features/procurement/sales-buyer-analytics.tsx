import Card from "@mui/material/Card";
import CardHeader, { cardHeaderClasses } from "@mui/material/CardHeader";
import CardContent from "@mui/material/CardContent";
import { RadialStat } from "@/components/app/radial-stat";
import { EmptyState } from "@/components/app/empty-state";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { Tag } from "@/components/ui-primitives";
import { KpiValue } from "./kpi-value";
import { redirect } from "next/navigation";

import { IndianRupee, Repeat, Users } from "lucide-react";
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
import type { SxProps, Theme } from "@mui/material/styles";
import { PHONE, cardTableScrollSx } from "./procurement-sx";

// Phone deck: two widgets per row with no icon badge, so the figure is sized to stay on one line
// instead of a rupee amount breaking mid-number.
const BUYER_KPI_SX: SxProps<Theme> = {
  [PHONE]: {
    "& .kit-kpi-value": { fontSize: "var(--fs-h4) !important", whiteSpace: "nowrap", overflowWrap: "normal" },
    "& .kit-kpi-value .proc-kpi-value": { flexWrap: "nowrap" },
  },
};

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
const FALLBACK_LIMIT = 25;
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
    <>
      {/* The headline deck, on the kit KPI card: tinted surface, watermark icon behind the
          number and the same figure the page printed before, through the same formatters. */}
      <Box sx={BUYER_KPI_SX}>
      <KpiGrid className="sales-kpi-row">
        <KpiCard
          variant="gradient"
          tone="primary"
          label={copy(pageContract, "kpi.buyers")}
          value={<KpiValue value={summary.buyers} />}
          watermark={<Users aria-hidden="true" />}
          // Older contracts lack this optional standalone detail; never reuse the
          // legacy key, whose sentence follows an unregistered-buyer count (0dd2097e3).
          hint={optionalCopy(pageContract, "kpi.buyers.closed_sale_detail") ? copy(pageContract, "kpi.buyers.closed_sale_detail") : undefined}
        />
        <KpiCard
          variant="tint"
          tone="info"
          label={copy(pageContract, "kpi.repeat_buyers")}
          value={<KpiValue value={summary.repeat_buyers} />}
          watermark={<Repeat aria-hidden="true" />}
          hint={`${num(repeatPct, 0)}% · ${copy(pageContract, "kpi.repeat_buyers.detail")}`}
        />
        <KpiCard
          variant="tint"
          tone="success"
          label={copy(pageContract, "kpi.repeat_revenue")}
          value={<KpiValue value={summary.repeat_revenue} kind="inr" />}
          watermark={<IndianRupee aria-hidden="true" />}
          hint={`${num(summary.repeat_revenue_pct, 0)}% ${copy(pageContract, "kpi.repeat_revenue.detail")} · ${num(summary.purchases)} ${copy(pageContract, "kpi.purchases.detail")}`}
        />
        <KpiCard
          variant="tint"
          tone="warning"
          label={copy(pageContract, "kpi.outstanding")}
          value={<KpiValue value={summary.outstanding} kind="inr" />}
          watermark={<IndianRupee aria-hidden="true" />}
          hint={copy(pageContract, "kpi.outstanding.detail")}
        />
      </KpiGrid>
      </Box>

      {/* The two shares the summary already carries, as gauges. Both numbers are the backend's
          own percentages — nothing is derived here that the deck above did not already print. */}
      <Box sx={{ mt: { xs: 1.5, sm: 1.75 } }}>
      <Card>
        <CardHeader
          title={copy(pageContract, "kpi.repeat_buyers")}
          action={<Tag tone="info">{num(summary.repeat_buyers)} / {num(summary.buyers)}</Tag>}
          sx={{ alignItems: "center", px: { xs: 2, sm: 3 }, pt: { xs: 2, sm: 3 }, [`& .${cardHeaderClasses.action}`]: { alignSelf: "center", m: 0 } }}
        />
        <CardContent sx={{ p: { xs: 2, sm: 3 } }}>
        {/* Template analytics radial pair (AnalyticsCurrentVisits-style gauges) centred in the card. */}
        <Box
          sx={{
            display: "flex",
            flexWrap: "wrap",
            alignItems: "flex-start",
            justifyContent: { xs: "space-between", sm: "center" },
            columnGap: { xs: 1.5, sm: 5.5 },
            rowGap: { xs: 1.5, sm: 2.25 },
          }}
        >
          <RadialStat
            value={repeatPct}
            tone="info"
            caption={copy(pageContract, "kpi.repeat_buyers")}
          />
          <RadialStat
            value={summary.repeat_revenue_pct}
            tone="success"
            caption={copy(pageContract, "kpi.repeat_revenue")}
          />
        </Box>
        </CardContent>
      </Card>
      </Box>

      <section
        className="card"
        aria-label={copy(pageContract, "section.buyers.title")}
      >
        <CardHeader
          title={copy(pageContract, "section.buyers.title")}
          sx={{ px: 2, py: 1.5, borderBottom: 1, borderColor: "divider", alignItems: "center" }}
        />
        {!showPhones ? (
          <p className="muted small" style={{ marginTop: 0 }}>
            {phoneHiddenReason}
          </p>
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
      </section>
    </>
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
  // Page size is the contract's; the offset is bounded to the backend's own ceiling.
  const pageSizes = tablePageSizes(pageContract, "sales-buyer-analytics");
  const defaultLimit = pageSizes[0] ?? FALLBACK_LIMIT;
  const limit = resolveLimit(one(sp, "limit"), pageSizes, defaultLimit);
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
    <div className="screen on sales-buyer-analytics-page">
      <SalesPageHeader pageContract={pageContract} />

      {!result.ok ? (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          {salesErrorText(result.error, copy(pageContract, "error.load"))}
        </Alert>
      ) : null}

      <SalesFarmToggle
        pageContract={pageContract}
        pagePath={PAGE_PATH}
        searchParams={sp}
        parkId={parkId}
        parks={parks}
        clears={["offset"]}
      />

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
    </div>
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
