import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import { listOrEmpty } from "@/lib/list-or-empty";
import { EmptyState } from "@/components/app/empty-state";
import { Label } from "@/components/minimal/label";
import { TableHeadCustom } from "@/components/app/table";
import { KpiWidget } from "@/components/app/kpi-widget";
import { EcommerceSalesOverview } from "@/components/app/sections/overview/e-commerce/ecommerce-sales-overview";
import { RankedTableCard } from "@/components/app/ranked-table-card";
import { EcommerceLatestProducts } from "@/components/app/sections/overview/e-commerce/ecommerce-latest-products";
import { SalesSoldMonthly } from "./sales-sold-monthly";
import ListItemText from "@mui/material/ListItemText";
import type { ReactNode } from "react";

import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";

import {
  controlEnabled,
  copy,
  table,
  tableLabels,
  tablePageSizes,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, listProcurementVendorOptions } from "@/lib/api/server";
import type { ProcurementVendorOptions } from "@/lib/api/server";
import { getSalesOverview, listSalesDeals } from "@/lib/api/procurement-server";
import type { SalesDeal, SalesOverview } from "@/lib/api/procurement";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import {
  dealStatusTone,
  humanDate,
  inr,
  inrCompact,
  monthLabel,
  monthlyAnimalRevenueTotal,
  monthlyAnimalsTotal,
  monthlyRevenueTotal,
  num,
  numCompactWhole,
  trimEmptyMonthlyStart,
  breedBeyondProduct,
} from "./sales-format";
import { ProcurementTableFooter } from "./table-footer-links";
import { SalesRecordDrawer } from "./sales-record-drawer";
import { SALES_DEFAULT_FARM, SalesFarmToggle, SalesPageHeader, hrefWithQuery, readSalesParkScope } from "./sales-chrome";
import { salesErrorText } from "./sales-error";
import Alert from "@mui/material/Alert";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";

const PAGE_PATH = "/sales/sold";
const DEFAULT_LIMIT = 25;
/** Only used when an older backend contract has no buyer board table; the contract page size wins. */
const BUYERS_PAGE_SIZE = 10;

// Template widget / chart colours from the locked palette (scheme-aware CSS variables the chart
// resolves at draw time), in the template Ecommerce overview's light→main gradient pairs.
/** Template Label colour for a deal status (the old Tag tones). */
const STATUS_LABEL = { ok: "success", info: "info", warn: "warning", dng: "error", mut: "default" } as const;
/** Template rank-chip colours by position (EcommerceBestSalesman: Top 1..4, then the rest). */
const RANK_LABEL = ["primary", "secondary", "info", "warning"] as const;
const BAND_BAR = ["primary", "info", "secondary", "warning"] as const;
/** Ledger columns (contract order: date, farm, buyer, product, breed, animals, weight, value, status) that are figures. */
const NUMERIC_DEAL_COLUMNS = new Set([5, 6, 7]);

/**
 * Sold — what has already left the farm (the retired Sales board divided in two, maintainer
 * decision 2026-09-11), laid out as the MUI Minimal Ecommerce overview
 * (docs/design/page-template-map.md): the headline figures as template widget summaries beside
 * sold animals by weight, month by month (Yearly sales card) beside price per kg by breed (Latest
 * products list), the buyer board (Best salesman table) and LAST the deals ledger with its
 * read-only deal drawer. Farm value keeps the live-herd valuation and /sales redirects here.
 *
 * READ-ONLY BY CONTRACT: the backend page contract declares no write control, so nothing here can
 * open a form. Recording, editing and tagging live on /sales/config.
 */
function SoldSections({
  overview,
  pageContract,
  buyersHref,
  buyersPage,
  chartKey,
}: {
  overview: SalesOverview;
  pageContract: AdminUiPageContract;
  /** Changes with the served farm scope so the charts remount (and draw in again) on a filter change. */
  chartKey: string;
  /** Link builder for the buyer board's pager, preserving every other selected search param. */
  buyersHref: (page: number) => string;
  /** 1-based buyer board page, already clamped by the caller. */
  buyersPage: number;
}) {
  const summary = overview.summary;
  const none = copy(pageContract, "value.none");
  const kgSuffix = copy(pageContract, "value.kg_suffix");
  const perKgSuffix = copy(pageContract, "value.per_kg_suffix");
  const seriesLabel = (productType: string) => copy(pageContract, `chart.series.${productType.toLowerCase()}`);
  // The buyer board rides on the overview response (a bounded, pre-aggregated board), so its pages
  // are sliced here rather than re-fetched. The page SIZE is the backend contract's, not a local
  // literal, and the pager reports the whole-list total -- never the sliced page's length.
  const buyersPageSize = tablePageSizes(pageContract, "sales-buyers")[0] ?? BUYERS_PAGE_SIZE;
  const buyersPageCount = Math.max(1, Math.ceil(overview.buyers.length / buyersPageSize));
  const buyersPageNumber = Math.min(Math.max(buyersPage, 1), buyersPageCount);
  const buyersStart = (buyersPageNumber - 1) * buyersPageSize;
  const buyersRows = overview.buyers.slice(buyersStart, buyersStart + buyersPageSize);
  const showFeed = summary.feed_kg > 0 || summary.feed_revenue > 0;

  // Month by month: one template Yearly sales card whose select switches between rupees, heads and
  // kg. Each series drops its own leading empty months and draws on its own scale.
  const revenueMonths = trimEmptyMonthlyStart(overview.monthly, monthlyRevenueTotal);
  const animalMonths = trimEmptyMonthlyStart(overview.monthly, monthlyAnimalsTotal);
  const manureMonths = trimEmptyMonthlyStart(overview.monthly, (month) => month.manure_kg);
  const sum = (values: number[]) => values.reduce((total, value) => total + value, 0);
  const pricedMonths = overview.monthly.filter((month) => month.realized_price_per_kg > 0);
  const monthlySeries = [
    {
      name: copy(pageContract, "chart.monthly_revenue.value"),
      categories: revenueMonths.map((month) => monthLabel(month.month)),
      format: "inr" as const,
      totals: [inrCompact(sum(revenueMonths.map(monthlyRevenueTotal)))],
      data: [{ name: copy(pageContract, "chart.monthly_revenue.title"), data: revenueMonths.map(monthlyRevenueTotal) }],
      empty: copy(pageContract, "chart.monthly_revenue.empty"),
    },
    {
      name: copy(pageContract, "chart.monthly_animals.value"),
      categories: animalMonths.map((month) => monthLabel(month.month)),
      format: "number" as const,
      totals: [num(sum(animalMonths.map(monthlyAnimalsTotal)))],
      data: [{ name: copy(pageContract, "chart.monthly_animals.title"), data: animalMonths.map(monthlyAnimalsTotal) }],
      // Head count owns the line; the rupees it earned ride in the tooltip as their own figure so
      // the two units are read separately and never share the axis.
      notes: animalMonths.map((month) => `${copy(pageContract, "chart.monthly_animals.sub")} ${inrCompact(monthlyAnimalRevenueTotal(month))}`),
      empty: copy(pageContract, "chart.monthly_animals.empty"),
    },
    {
      name: copy(pageContract, "chart.monthly_manure.value"),
      categories: manureMonths.map((month) => monthLabel(month.month)),
      format: "number" as const,
      totals: [numCompactWhole(sum(manureMonths.map((month) => month.manure_kg)))],
      data: [{ name: copy(pageContract, "chart.monthly_manure.title"), data: manureMonths.map((month) => month.manure_kg) }],
      notes: manureMonths.map((month) => `${copy(pageContract, "chart.monthly_manure.sub")} ${inrCompact(month.manure_revenue)}`),
      empty: copy(pageContract, "chart.monthly_manure.empty"),
    },
    {
      // Realized price per kg by month (the headline price tile's monthly series; months with no
      // weighed live sale have no price and are left out rather than drawn as ₹0).
      name: copy(pageContract, "kpi.realized_price"),
      categories: pricedMonths.map((month) => monthLabel(month.month)),
      format: "inr" as const,
      totals: [summary.realized_price_per_kg > 0 ? inr(Math.round(summary.realized_price_per_kg)) : none],
      data: [{ name: copy(pageContract, "kpi.realized_price"), data: pricedMonths.map((month) => Math.round(month.realized_price_per_kg)) }],
      empty: copy(pageContract, "chart.monthly_revenue.empty"),
    },
  ];

  return (
    <Grid container spacing={3}>
      {/* Headline figures: template CourseWidgetSummary (KpiWidget), two by two beside sold-by-weight. */}
      <Grid size={{ xs: 12, lg: 8 }}>
        <Grid container spacing={3} component="section" aria-label={copy(pageContract, "section.sold.aria")} sx={{ height: 1 }}>
          <Grid size={{ xs: 12, sm: 6 }}>
            <KpiWidget
              title={copy(pageContract, "kpi.revenue")}
              total={summary.revenue}
              caption={[`₹`, `${num(summary.deals)} ${copy(pageContract, "kpi.deals")}`].filter(Boolean).join(" · ")}
              icon="completed"
              color="primary"
              sx={{ height: 1 }}
            />
          </Grid>
          <Grid size={{ xs: 12, sm: 6 }}>
            <KpiWidget
              title={copy(pageContract, "kpi.animals")}
              total={summary.animals}
              caption={`${num(summary.sheep)} ${seriesLabel("sheep")} · ${num(summary.goats)} ${seriesLabel("goat")}`}
              color="info"
              sx={{ height: 1 }}
            />
          </Grid>
          <Grid size={{ xs: 12, sm: 6 }}>
            {/* Zero means no weighed live sale exists — printing ₹0 per kg would claim we give
                animals away (null renders an empty figure). */}
            <KpiWidget
              title={copy(pageContract, "kpi.realized_price")}
              total={summary.realized_price_per_kg > 0 ? summary.realized_price_per_kg : null}
              caption={summary.realized_price_per_kg > 0 ? `₹ ${perKgSuffix}` : none}
              color="secondary"
              sx={{ height: 1 }}
            />
          </Grid>
          <Grid size={{ xs: 12, sm: 6 }}>
            <KpiWidget
              title={copy(pageContract, "kpi.manure")}
              total={summary.manure_kg}
              caption={[`${kgSuffix}`, `${inr(summary.manure_revenue)} · ${copy(pageContract, "kpi.manure.detail")}`].filter(Boolean).join(" · ")}
              color="warning"
              sx={{ height: 1 }}
            />
          </Grid>
          {/* Feed sold off the store is in the revenue above; without its own figure the headline
              could not be read back into what was sold. Shown only when some was sold (main
              d35d4db8d). */}
          {showFeed ? (
            <Grid size={12}>
              <KpiWidget
                title={copy(pageContract, "kpi.feed")}
                total={summary.feed_kg}
                caption={[`${kgSuffix}`, `${inr(summary.feed_revenue)} · ${copy(pageContract, "kpi.feed.detail")}`].filter(Boolean).join(" · ")}
              />
            </Grid>
          ) : null}
        </Grid>
      </Grid>

      {/* Sold animals by weight (maintainer decisions 2026-09-08 and 2026-09-21): every animal sold
          on a closed deal, in the maintainer's four bands, whole register -- template Sales
          overview progress rows. Each band names where its weights came from, because a scale
          reading and a load average are not the same evidence; the unweighed remainder is named
          beside the total rather than hidden in a band. Backend owns every count and word. */}
      <Grid size={{ xs: 12, lg: 4 }}>
        <EcommerceSalesOverview
          component="section"
          aria-label={copy(pageContract, "section.sold_weight.aria")}
          title={copy(pageContract, "section.sold_weight.title")}
          subheader={[
            `${num(overview.sold_weight_bands.total)} ${copy(pageContract, "sold_weight.total")}`,
            overview.sold_weight_bands.unweighed > 0 ? `${num(overview.sold_weight_bands.unweighed)} ${copy(pageContract, "sold_weight.unweighed")}` : "",
          ]
            .filter(Boolean)
            .join(" · ")}
          data={
            overview.sold_weight_bands.total === 0
              ? []
              : overview.sold_weight_bands.bands.map((band, i) => ({
                  label: copy(pageContract, `sold_weight.band.${band.band}`),
                  value: overview.sold_weight_bands.total > 0 ? (band.total / overview.sold_weight_bands.total) * 100 : 0,
                  display: num(band.total),
                  color: BAND_BAR[i] ?? "primary",
                  // Where this band's weights came from -- the three are not the same kind of
                  // evidence, so a band whose animals were all weighed one way says nothing extra.
                  caption: [
                    band.measured > 0 ? `${num(band.measured)} ${copy(pageContract, "sold_weight.source.measured")}` : "",
                    band.load_average > 0 ? `${num(band.load_average)} ${copy(pageContract, "sold_weight.source.load_average")}` : "",
                    band.estimated > 0 ? `${num(band.estimated)} ${copy(pageContract, "sold_weight.source.estimated")}` : "",
                  ]
                    .filter(Boolean)
                    .join(" · "),
                }))
          }
          sx={{ height: 1 }}
        >
          {overview.sold_weight_bands.total === 0 ? <EmptyState title={copy(pageContract, "empty.sold_weight")} /> : null}
          {overview.sold_weight_bands.estimated > 0 ? (
            <Typography variant="caption" component="p" color="text.secondary" sx={{ m: 0 }}>
              {copy(pageContract, "sold_weight.estimated.note")}
            </Typography>
          ) : null}
        </EcommerceSalesOverview>
      </Grid>

      {/* Month by month (template Yearly sales): rupees, heads and kg, one at a time. Keyed by farm
          so a filter change remounts the chart and it draws in again with the new figures. */}
      <Grid size={{ xs: 12, lg: 8 }}>
        <SalesSoldMonthly
          key={`monthly-${chartKey}`}
          title={copy(pageContract, "section.monthly.title")}
          ariaLabel={copy(pageContract, "section.monthly.aria")}
          series={monthlySeries}
        />
      </Grid>

      {/* Realized price per kg by breed, ordered as served (highest first) -- template Latest
          products list. */}
      <Grid size={{ xs: 12, lg: 4 }}>
        {overview.price_bands.length === 0 ? (
          <Card component="section" aria-label={copy(pageContract, "section.price_bands.aria")} sx={{ height: 1 }}>
            <CardHeader title={copy(pageContract, "section.price_bands.title")} />
            <EmptyState title={copy(pageContract, "chart.price_bands.empty")} />
          </Card>
        ) : (
          <EcommerceLatestProducts
            component="section"
            aria-label={copy(pageContract, "section.price_bands.aria")}
            title={copy(pageContract, "section.price_bands.title")}
            list={overview.price_bands.slice(0, 12).map((band) => ({
              id: `${band.product_type}|${band.breed}`,
              name: [breedBeyondProduct(band.product_type, band.breed), seriesLabel(band.product_type)].filter(Boolean).join(" · "),
              display: inr(Math.round(band.avg_price_per_kg)),
            }))}
            // Phone webview: no inner scroller at xs (the list is short), names wrap.
            slotProps={{ scrollbar: { minHeight: { xs: "auto", sm: 384 } }, list: { minWidth: { xs: 0, sm: 360 } } }}
            wrapNames
            sx={{ height: 1 }}
          />
        )}
      </Grid>

      {/* The market benchmark table was removed from this board (maintainer request
          2026-09-03); the quotes are still entered and kept on /sales/config. */}

      {/* Buyers -- template Best salesman table, the share of revenue as its rank chip. */}
      <Grid size={12}>
        <RankedTableCard
          component="section"
          aria-label={copy(pageContract, "section.buyers.title")}
          title={copy(pageContract, "section.buyers.title")}
          action={
            <Label variant="soft" color="default">
              {num(overview.buyers.length)} {copy(pageContract, "summary.buyers")}
            </Label>
          }
          tableLabel={copy(pageContract, "section.buyers.title")}
          regionId="sales-sold-buyers"
          headCells={[
            { id: "buyer", label: copy(pageContract, "column.buyer_name") },
            { id: "place", label: copy(pageContract, "column.buyer_place") },
            { id: "buys", label: copy(pageContract, "column.product_types") },
            { id: "deals", label: copy(pageContract, "column.deals"), align: "right" },
            { id: "animals", label: copy(pageContract, "column.animals"), align: "right" },
            { id: "revenue", label: copy(pageContract, "column.revenue"), align: "right" },
            { id: "share", label: copy(pageContract, "column.share_pct"), align: "right" },
          ]}
          tableData={buyersRows.map((buyer, i) => ({
            id: `${buyer.buyer_name}|${buyer.buyer_place}`,
            name: buyer.buyer_name,
            secondary: buyer.buyer_place || undefined,
            cells: [
              { value: buyer.buyer_place || none },
              { value: buyer.product_types.join(" · ") },
              { value: num(buyer.deals), align: "right" },
              { value: num(buyer.animals), align: "right" },
              { value: inr(buyer.revenue), align: "right" },
            ],
            rank: { label: `${num(buyer.share_pct, 1)}%`, color: RANK_LABEL[buyersStart + i] ?? "default" },
          }))}
          sx={{ "& .MuiCardHeader-action": { alignSelf: "center" } }}
        >
          {overview.buyers.length === 0 ? <EmptyState title={copy(pageContract, "empty.buyers")} /> : null}
          {buyersPageCount > 1 ? (
            <ProcurementTableFooter
              denseLabel={copy(pageContract, "action.dense", "Dense")}
              rowsLabel={copy(pageContract, "pager.rows", "Rows")}
              page={buyersPageNumber}
              pageCount={buyersPageCount}
              rangeLabel={`${buyersStart + 1}\u2013${buyersStart + buyersRows.length} ${copy(pageContract, "pager.of")} ${num(overview.buyers.length)} ${copy(pageContract, "summary.buyers")}`}
              prevHref={buyersHref(buyersPageNumber - 1)}
              nextHref={buyersHref(buyersPageNumber + 1)}
              prevLabel={copy(pageContract, "action.prev_page")}
              nextLabel={copy(pageContract, "action.next_page")}
              denseTargetId="sales-sold-buyers"
            />
          ) : null}
        </RankedTableCard>
      </Grid>
    </Grid>
  );
}

/** The params each Sold panel reads: the park scope (and the legacy farm) plus the panel's own pager. */
const SCOPE_PARAMS = ["park", "scope_mode", "farm"] as const;
const OVERVIEW_WATCH = [...SCOPE_PARAMS, "buyers_page"];
const LEDGER_WATCH = [...SCOPE_PARAMS, "limit", "offset"];

export async function SalesSoldPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;

  // Park scope: the SHELL's `park` (one filter across every Sales page), resolved to the deal farm
  // code the sales reads filter by. Resolved from the (request-cached) bootstrap only, so the
  // header and the farm chips render before any sales read; each panel below streams its own data
  // behind a skeleton keyed by the params it reads (guard: url-keyed-panel).
  const { parkId, farm, parks } = await readSalesParkScope(sp, pageContract, PAGE_PATH);

  return (
    <div className="screen on">
      <SalesPageHeader pageContract={pageContract} />

      <SalesFarmToggle
        pageContract={pageContract}
        pagePath={PAGE_PATH}
        searchParams={sp}
        parkId={parkId}
        parks={parks}
        clears={["offset", "buyers_page", "deal_id"]}
      />

      <UrlSuspense searchParams={sp} watch={OVERVIEW_WATCH} fallback={<PanelSkeleton kpis={4} charts={2} spark />}>
        <SoldOverviewPanel sp={sp} farm={farm} pageContract={pageContract} />
      </UrlSuspense>

      <UrlSuspense searchParams={sp} watch={LEDGER_WATCH} fallback={<PanelSkeleton table={8} tableWidths={["1.4fr", "1.2fr", "1fr", "0.9fr", "0.9fr", "0.7fr"]} />}>
        <SoldLedgerPanel sp={sp} farm={farm} pageContract={pageContract} />
      </UrlSuspense>
    </div>
  );
}

/** Every block above the ledger: the overview contract (KPIs, weight bands, monthly, price, buyers). */
async function SoldOverviewPanel({ sp, farm, pageContract }: { sp: RouteSearchParams; farm: string; pageContract: AdminUiPageContract }) {
  // Buyer board page. A hand-edited value is clamped here and again against the served row count,
  // so an out-of-range page can never take the section down.
  const buyersPage = boundedInt(one(sp, "buyers_page"), 1, 1, 1000);
  const overviewResult = await getSalesOverview({ farm });
  if (firstAuthRequiredError(overviewResult)) redirect(INTERNAL_LOGIN_PATH);
  const overview: SalesOverview | null = overviewResult.ok ? overviewResult.data : null;
  return (
    <>
      {!overviewResult.ok ? (
        <Alert severity="error" sx={{ mb: 3 }}>
          {salesErrorText(overviewResult.error, copy(pageContract, "error.load"))}
        </Alert>
      ) : null}
      {overview ? (
        <SoldSections
          overview={overview}
          pageContract={pageContract}
          chartKey={farm}
          buyersPage={buyersPage}
          buyersHref={(page) => hrefWithQuery(PAGE_PATH, sp, { buyers_page: page > 1 ? String(page) : null })}
        />
      ) : null}
    </>
  );
}

/** The deals ledger (one server page) and the read-only deal drawer its rows open. */
async function SoldLedgerPanel({ sp, farm, pageContract }: { sp: RouteSearchParams; farm: string; pageContract: AdminUiPageContract }) {
  const dealsTable = table(pageContract, "sales-deals");
  const pageSizes = dealsTable.page_size_options.length > 0 ? dealsTable.page_size_options : [DEFAULT_LIMIT];
  const limit = boundedInt(one(sp, "limit"), pageSizes[0], 1, 100);
  const offset = boundedInt(one(sp, "offset"), 0, 0, 10000);

  // One ledger page and the vendor register the deal drawer names (LocalOverlayLink opens without
  // an RSC request, so drawer data must ride with the ledger). No /sales/options: it feeds only the
  // record-sale FORM, and this page's contract declares no record control.
  const [dealsResult, vendorOptionsResult] = await Promise.all([
    listSalesDeals({ farm, limit, offset }),
    // The deal drawer here is a READ-ONLY detail, and it still names the buyer's vendor. Resolving
    // that id to the register's name needs the active register with the page. ONE bounded read,
    // never a paged walk of /procurement/vendors: that is the banned SSR full-walk shape.
    listProcurementVendorOptions(),
  ]);
  if (firstAuthRequiredError(dealsResult)) redirect(INTERNAL_LOGIN_PATH);
  // null means the register could NOT be read (it is a separate permission, procurement.vendor.read).
  // The drawer renders a stated error for that case rather than an empty dropdown, which would read
  // as "there are no vendors" and send the person to add one that already exists.
  const vendorOptions: ProcurementVendorOptions | null = vendorOptionsResult.ok ? vendorOptionsResult.data : null;
  const deals: SalesDeal[] = dealsResult.ok ? listOrEmpty(dealsResult.data.deals) : [];
  const total = dealsResult.ok ? dealsResult.data.total : 0;
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const pageNumber = Math.min(pageCount, Math.floor(offset / limit) + 1);

  // READ-ONLY BY CONTRACT (maintainer decision 2026-09-01): this page's backend contract declares
  // no write control at all, so `controlEnabled` is false for everyone including the CEO and the
  // deal drawer below opens as a detail view. Recording, editing and tagging live on
  // /sales/config. Do not "restore" a button here — add the control back to this page's contract
  // first, which TestSalesReadPagesCarryNoWriteControl refuses.
  const canRecord = controlEnabled(pageContract, "record_sale", false);
  const none = copy(pageContract, "value.none");
  const dealColumns = tableLabels(pageContract, "sales-deals");
  const listHref = hrefWithQuery(PAGE_PATH, sp, { deal_id: null });
  // The pager keeps the page's park and every other parameter; only the offset moves.
  const dealsPageHref = (page: number) => {
    const nextOffset = Math.max(0, (page - 1) * limit);
    return hrefWithQuery(PAGE_PATH, sp, { offset: nextOffset > 0 ? String(nextOffset) : null, deal_id: null });
  };

  return (
    <>
      {/* LAST — the deals ledger (maintainer instruction 2026-09-11: "the deals table keep it at
          last"). Template order list anatomy: card, header with the whole-filter count Label,
          TableHeadCustom, server-paged TablePagination footer; a row opens the read-only deal
          drawer. */}
      <Card component="section" aria-label={copy(pageContract, "section.ledger.aria")} sx={{ mt: 3 }}>
        <CardHeader
          title={copy(pageContract, "section.ledger.title")}
          action={
            // The WHOLE-FILTER total from the backend, not deals.length.
            <Label variant="soft" color={total ? "info" : "default"}>
              {num(total)} {copy(pageContract, "summary.count")}
            </Label>
          }
          sx={{ mb: 3, "& .MuiCardHeader-action": { alignSelf: "center" } }}
        />

        {!dealsResult.ok ? (
          <Alert severity="error" sx={{ mx: 3, mb: 3 }}>
            {salesErrorText(dealsResult.error, copy(pageContract, "error.load"))}
          </Alert>
        ) : null}

        {deals.length === 0 ? (
          <EmptyState title={farm !== SALES_DEFAULT_FARM ? copy(pageContract, "empty.deals") : copy(pageContract, "empty.deals.unset")} />
        ) : (
          <Box id="sales-sold-deals" tabIndex={0} role="region" aria-label={copy(pageContract, "section.ledger.aria")} sx={{ overflowX: "auto", maxWidth: "100%" }}>
            <Table
              className="sales-deals-table"
              aria-label={copy(pageContract, "section.ledger.aria")}
              sx={{
                minWidth: 960,
                // Single-line ledger cells: the global .celllink overflow-wrap:anywhere otherwise splits
                // "2026-08-11" and "CPT" mid-token.
                "&& td, && td .celllink": { whiteSpace: "nowrap", overflowWrap: "normal", wordBreak: "normal" },
                "&& td .celllink": { maxWidth: "none", minWidth: "max-content", color: "inherit", textDecoration: "none" },
              }}
            >
              <TableHeadCustom
                headCells={dealColumns.map((label, index) => ({
                  id: `${index}-${label}`,
                  label,
                  align: NUMERIC_DEAL_COLUMNS.has(index) ? "right" : "left",
                }))}
              />
              <TableBody>
                {deals.map((deal) => {
                  const drawerHref = hrefWithQuery(PAGE_PATH, sp, { deal_id: deal.deal_id });
                  const dealCell = (value: ReactNode, align?: "right") => (
                    <TableCell align={align}>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        {value}
                      </LocalOverlayLink>
                    </TableCell>
                  );
                  return (
                    <TableRow key={deal.deal_id} hover>
                      {dealCell(humanDate(deal.sale_date))}
                      {dealCell(deal.farm)}
                      {dealCell(
                        // Template order row: primary + secondary text (ListItemText).
                        <ListItemText
                          primary={deal.buyer_name}
                          secondary={deal.product_type || undefined}
                          slotProps={{ primary: { noWrap: true, variant: "subtitle2" }, secondary: { component: "span" } }}
                        />,
                      )}
                      {dealCell(deal.product_type)}
                      {dealCell(breedBeyondProduct(deal.product_type, deal.breed) ?? "")}
                      {dealCell(deal.animal_count == null ? none : num(deal.animal_count), "right")}
                      {dealCell(deal.total_weight_kg == null ? none : num(deal.total_weight_kg, 1), "right")}
                      {dealCell(inr(deal.sales_value), "right")}
                      {dealCell(
                        <Label variant="soft" color={STATUS_LABEL[dealStatusTone(deal.status)]}>
                          {deal.status}
                        </Label>,
                      )}
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </Box>
        )}

        {pageCount > 1 ? (
          <ProcurementTableFooter
            denseLabel={copy(pageContract, "action.dense", "Dense")}
            rowsLabel={copy(pageContract, "pager.rows", "Rows")}
            page={pageNumber}
            pageCount={pageCount}
            rangeLabel={`${offset + 1}–${offset + deals.length} ${copy(pageContract, "pager.of")} ${num(total)}`}
            rowsValue={limit}
            rowsOptions={pageSizes.map((size) => ({
              size,
              // The park and every other parameter stay; a new page size starts at the first page.
              href: hrefWithQuery(PAGE_PATH, sp, { limit: size === pageSizes[0] ? null : String(size), offset: null, deal_id: null }),
            }))}
            prevHref={dealsPageHref(pageNumber - 1)}
            nextHref={dealsPageHref(pageNumber + 1)}
            prevLabel={copy(pageContract, "action.prev_page")}
            nextLabel={copy(pageContract, "action.next_page")}
            denseTargetId="sales-sold-deals"
          />
        ) : null}
      </Card>

      {/* Always mounted: LocalOverlayLink changes the URL without an RSC request, so an overlay
          gated on a server-read search param would never appear. */}
      <SalesRecordDrawer
        deals={deals}
        pageContract={pageContract}
        listHref={listHref}
        canRecord={canRecord}
        vendorOptions={vendorOptions}
        salesOptions={null}
        // Sold is READ-ONLY (maintainer decision 2026-09-11): its page contract declares no write
        // control, so this drawer never renders the record form and has no submit to confirm.
        // Every sales write is made on /sales/config, which is where the confirmation appears.
        stockConfirmNeeded={false}
        statusStockConfirmNeeded={false}
      />
    </>
  );
}
