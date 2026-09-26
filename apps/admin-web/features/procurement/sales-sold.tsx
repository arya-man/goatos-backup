import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import CardHeader, { cardHeaderClasses } from "@mui/material/CardHeader";
import { StatStrip } from "@/components/minimal/widgets/stat-strip";
import { EmptyState } from "@/components/app/empty-state";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { GoatGlyph } from "@/components/goat-glyph";
import { KpiValue } from "./kpi-value";
import { IdentityCell } from "@/components/data-table";
import type { ReactNode } from "react";

import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Banknote, IndianRupee, Package, Weight } from "lucide-react";

import { HBarList } from "@/components/hbar-list";
import { StackedColumns } from "@/components/svg-series";
import { Tag } from "@/components/ui-primitives";
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
  inrAxisTick,
  inrCompact,
  monthLabel,
  monthlyAnimalRevenueTotal,
  monthlyAnimalsTotal,
  monthlyRevenueTotal,
  num,
  numAxisTick,
  numCompactWhole,
  trimEmptyMonthlyStart,
  breedBeyondProduct,
} from "./sales-format";
import { ProcurementTableFooter } from "./table-footer-links";
import { SalesRecordDrawer } from "./sales-record-drawer";
import { SALES_DEFAULT_FARM, SalesFarmToggle, SalesPageHeader, hrefWithQuery, readSalesParkScope } from "./sales-chrome";
import { salesErrorText } from "./sales-error";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { salesKpiRowSx } from "./procurement-sx";

const PAGE_PATH = "/sales/sold";
const DEFAULT_LIMIT = 25;
/** Only used when an older backend contract has no buyer board table; the contract page size wins. */
const BUYERS_PAGE_SIZE = 10;

/**
 * Sold — what has already left the farm (the retired Sales board divided in two, maintainer
 * decision 2026-09-11): the headline figures, sold animals by weight, month by month, price per
 * kg by breed, the buyer board, and LAST the deals
 * ledger with its read-only deal drawer. These are the board's own blocks, moved here verbatim;
 * Farm value keeps the live-herd valuation and /sales redirects here.
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
  // Last twelve months of the same monthly series the Month-by-month card charts, as the KPI
  // widgets' sparklines (Minimal widget anatomy). No trend chip: the current month is partial, so a
  // month-on-month % would compare a part month with a whole one.
  const sparkMonths = overview.monthly.slice(-12);
  const revenueSpark = sparkMonths.map(monthlyRevenueTotal);
  const animalsSpark = sparkMonths.map(monthlyAnimalsTotal);
  const manureSpark = sparkMonths.map((month) => month.manure_revenue);
  // Months with no priced live line carry 0 (no price, not a free animal); they are left out so
  // the line joins the months that really have a realized price.
  const priceSpark = sparkMonths.map((month) => month.realized_price_per_kg).filter((price) => price > 0);
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
  return (
    <>
          {/* SOLD — what has already left the farm. Same tiles as before, minus the two valuation
              ones that moved up into their own block. */}
          <Typography variant="h6" component="h3" sx={{ mt: 2.75, mb: 1 }}>
            {copy(pageContract, "section.sold.title")}
          </Typography>
          <Box sx={salesKpiRowSx}>
          <KpiGrid className="sales-kpi-row">
            <KpiCard
              variant="gradient"
              tone="primary"
              label={copy(pageContract, "kpi.revenue")}
              value={<KpiValue value={summary.revenue} kind="inr" />}
              icon={<Banknote aria-hidden="true" />}
              sparkline={revenueSpark}
              hint={`${num(summary.deals)} ${copy(pageContract, "kpi.deals")}`}
            />
            <KpiCard
              variant="tint"
              tone="info"
              label={copy(pageContract, "kpi.animals")}
              value={<KpiValue value={summary.animals} />}
              icon={<GoatGlyph aria-hidden="true" />}
              sparkline={animalsSpark}
              hint={`${num(summary.sheep)} ${seriesLabel("sheep")} · ${num(summary.goats)} ${seriesLabel("goat")}`}
            />
            <KpiCard
              variant="tint"
              tone="success"
              label={copy(pageContract, "kpi.realized_price")}
              // Zero means no weighed live sale exists — printing ₹0 per kg would claim we give
              // animals away.
              value={summary.realized_price_per_kg > 0 ? <KpiValue value={summary.realized_price_per_kg} kind="inr" suffix={perKgSuffix} /> : none}
              icon={<IndianRupee aria-hidden="true" />}
              sparkline={priceSpark}
              sparkVariant="line"
            />
            <KpiCard
              variant="tint"
              tone="violet"
              label={copy(pageContract, "kpi.manure")}
              value={<KpiValue value={summary.manure_kg} suffix={kgSuffix} />}
              icon={<Package aria-hidden="true" />}
              sparkline={manureSpark}
              hint={`${inr(summary.manure_revenue)} · ${copy(pageContract, "kpi.manure.detail")}`}
            />
            {/* Feed sold off the store is in the revenue above; without its own tile the headline
                could not be read back into what was sold. Shown only when some was sold (main
                d35d4db8d). */}
            {summary.feed_kg > 0 || summary.feed_revenue > 0 ? (
              <KpiCard
                variant="tint"
                tone="warning"
                label={copy(pageContract, "kpi.feed")}
                value={<KpiValue value={summary.feed_kg} suffix={kgSuffix} />}
                icon={<Package aria-hidden="true" />}
                hint={`${inr(summary.feed_revenue)} · ${copy(pageContract, "kpi.feed.detail")}`}
              />
            ) : null}
          </KpiGrid>
          </Box>

          {/* Sold animals by weight (maintainer decisions 2026-09-08 and 2026-09-21; placed ABOVE the
              monthly charts at the maintainer's request): every animal sold on a closed deal, in the
              maintainer's four bands, whole register. Each tile names where its weights came from,
              because a scale reading and a load average are not the same evidence and the reader is
              owed the difference. The unweighed remainder is named beside the total rather than
              hidden in a band. Backend owns every count and every word here. */}
          <section className="card sales-card" aria-label={copy(pageContract, "section.sold_weight.aria")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.sold_weight.title")}</h3>
            </div>
            {overview.sold_weight_bands.total === 0 ? (
              <EmptyState title={copy(pageContract, "empty.sold_weight")} />
            ) : (
              <>
                <StatStrip
                  ariaLabel={copy(pageContract, "section.sold_weight.aria")}
                  cells={overview.sold_weight_bands.bands.map((band, i) => ({
                    key: band.band,
                    label: copy(pageContract, `sold_weight.band.${band.band}`),
                    value: band.total,
                    tone: (["primary", "success", "info", "warning"] as const)[i] ?? "primary",
                    icon: <Weight aria-hidden="true" />,
                    // Where this band's weights came from -- the three are not the same kind of
                    // evidence, so a band whose animals were all weighed one way says nothing extra.
                    meta:
                      [
                        band.measured > 0 ? `${num(band.measured)} ${copy(pageContract, "sold_weight.source.measured")}` : "",
                        band.load_average > 0 ? `${num(band.load_average)} ${copy(pageContract, "sold_weight.source.load_average")}` : "",
                        band.estimated > 0 ? `${num(band.estimated)} ${copy(pageContract, "sold_weight.source.estimated")}` : "",
                      ]
                        .filter(Boolean)
                        .join(" · ") || copy(pageContract, "sold_weight.total"),
                    share: overview.sold_weight_bands.total > 0 ? (band.total / overview.sold_weight_bands.total) * 100 : 0,
                  }))}
                />
                {overview.sold_weight_bands.estimated > 0 ? (
                  <Typography variant="body2" color="text.secondary" sx={{ mt: 1, mx: { xs: 2, sm: 3 }, mb: { xs: 2, sm: 3 } }}>
                    {copy(pageContract, "sold_weight.estimated.note")}
                  </Typography>
                ) : null}
              </>
            )}
          </section>

          {/* 2 — month by month. Three separate charts: rupees, heads and kg never share an axis.
              Stacked full-width so every column carries its month label and value. */}
          <section className="card sales-card" aria-label={copy(pageContract, "section.monthly.aria")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.monthly.title")}</h3>
            </div>
            <Typography variant="overline" component="div" color="text.secondary" className="mt" sx={{ mt: 0.5 }}>
              {copy(pageContract, "chart.monthly_revenue.title")}
            </Typography>
            {/* Template column chart: every month labelled with its figure on the column (a phone
                scrolls the strip sideways), round rupee ticks on the axis. Keyed by farm so a
                filter change remounts the chart and it draws in again with the new figures. */}
            <StackedColumns
              key={`rev-${chartKey}`}
              days={trimEmptyMonthlyStart(overview.monthly, monthlyRevenueTotal).map((month) => ({
                key: month.month,
                label: monthLabel(month.month),
                segments: [monthlyRevenueTotal(month)],
              }))}
              seriesLabels={[copy(pageContract, "chart.monthly_revenue.value")]}
              valueNoun={copy(pageContract, "chart.monthly_revenue.value")}
              chartLabel={copy(pageContract, "chart.monthly_revenue.title")}
              emptyLabel={copy(pageContract, "chart.monthly_revenue.empty")}
              formatValue={inrCompact}
              formatTick={inrAxisTick}
              columnFigures
            />
            <Typography variant="overline" component="div" color="text.secondary" className="mt">{copy(pageContract, "chart.monthly_animals.title")}</Typography>
            {/* Head count owns the bar; the rupees it earned ride in the hover card as their own
                row so the two units are read separately and never share the axis. */}
            <StackedColumns
              key={`animals-${chartKey}`}
              days={trimEmptyMonthlyStart(overview.monthly, monthlyAnimalsTotal).map((month) => ({
                key: month.month,
                label: monthLabel(month.month),
                segments: [monthlyAnimalsTotal(month)],
                // Every month carries its rupee row, a measured ₹0 included, so no hover card
                // is missing a line.
                extra: [{ label: copy(pageContract, "chart.monthly_animals.sub"), value: inrCompact(monthlyAnimalRevenueTotal(month)) }],
              }))}
              seriesLabels={[copy(pageContract, "chart.monthly_animals.value")]}
              valueNoun={copy(pageContract, "chart.monthly_animals.value")}
              chartLabel={copy(pageContract, "chart.monthly_animals.title")}
              emptyLabel={copy(pageContract, "chart.monthly_animals.empty")}
              formatValue={num}
              columnFigures
            />
            <Typography variant="overline" component="div" color="text.secondary" className="mt">{copy(pageContract, "chart.monthly_manure.title")}</Typography>
            <StackedColumns
              key={`manure-${chartKey}`}
              days={trimEmptyMonthlyStart(overview.monthly, (month) => month.manure_kg).map((month) => ({
                key: month.month,
                label: monthLabel(month.month),
                segments: [month.manure_kg],
                extra: [{ label: copy(pageContract, "chart.monthly_manure.sub"), value: inrCompact(month.manure_revenue) }],
              }))}
              seriesLabels={[copy(pageContract, "chart.monthly_manure.value")]}
              valueNoun={copy(pageContract, "chart.monthly_manure.value")}
              chartLabel={copy(pageContract, "chart.monthly_manure.title")}
              emptyLabel={copy(pageContract, "chart.monthly_manure.empty")}
              formatValue={numCompactWhole}
              formatTick={numAxisTick}
              columnFigures
            />
          </section>

          {/* 3 — realized price per kg by breed, ordered as served (highest first). */}
          <section className="card sales-card" aria-label={copy(pageContract, "section.price_bands.aria")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.price_bands.title")}</h3>
            </div>
            <HBarList
              data={overview.price_bands.map((band) => ({
                key: `${band.product_type}|${band.breed}`,
                label: [breedBeyondProduct(band.product_type, band.breed), seriesLabel(band.product_type)]
                  .filter(Boolean)
                  .join(" · "),
                value: Math.round(band.avg_price_per_kg),
                display: inr(Math.round(band.avg_price_per_kg)),
              }))}
              emptyLabel={copy(pageContract, "chart.price_bands.empty")}
              valueNoun={copy(pageContract, "chart.price_bands.value")}
              chartLabel={copy(pageContract, "chart.price_bands.title")}
              maxBars={12}
            />
          </section>

          {/* The market benchmark table was removed from this board (maintainer request
              2026-09-03); the quotes are still entered and kept on /sales/config. */}

          {/* 5 — buyers. */}
          <section className="card" aria-label={copy(pageContract, "section.buyers.title")}>
            <CardHeader
              title={copy(pageContract, "section.buyers.title")}
              action={<Tag tone="mut">{num(overview.buyers.length)} {copy(pageContract, "summary.buyers")}</Tag>}
              sx={{ px: 2, py: 1.5, borderBottom: 1, borderColor: "divider", alignItems: "center", [`& .${cardHeaderClasses.action}`]: { alignSelf: "center", m: 0 } }}
            />
            {overview.buyers.length === 0 ? (
              <EmptyState title={copy(pageContract, "empty.buyers")} />
            ) : (
              <div id="sales-sold-buyers" className="twrap" tabIndex={0} role="region" aria-label={copy(pageContract, "section.buyers.title")}>
                <Table aria-label={copy(pageContract, "section.buyers.title")}>
                  <TableHead>
                    <TableRow>
                      <TableCell component="th">{copy(pageContract, "column.buyer_name")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "column.buyer_place")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "column.product_types")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "column.deals")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "column.animals")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "column.revenue")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "column.share_pct")}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {buyersRows.map((buyer) => (
                      <TableRow key={`${buyer.buyer_name}|${buyer.buyer_place}`}>
                        <TableCell>
                          <IdentityCell primary={buyer.buyer_name} secondary={buyer.buyer_place || undefined} />
                        </TableCell>
                        <TableCell>{buyer.buyer_place || none}</TableCell>
                        <TableCell>{buyer.product_types.join(" · ")}</TableCell>
                        <TableCell className="num">{num(buyer.deals)}</TableCell>
                        <TableCell className="num">{num(buyer.animals)}</TableCell>
                        <TableCell className="num">{inr(buyer.revenue)}</TableCell>
                        <TableCell className="num">{num(buyer.share_pct, 1)}%</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
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
          </section>
    </>
  );
}

export async function SalesSoldPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;

  // Park scope: the SHELL's `park` (one filter across every Sales page), resolved to the deal farm
  // code the sales reads filter by. Every block on the page reads the one selected scope.
  const { parkId, farm, parks } = await readSalesParkScope(sp, pageContract, PAGE_PATH);
  // Buyer board page. A hand-edited value is clamped here and again against the served row count,
  // so an out-of-range page can never take the section down.
  const buyersPage = boundedInt(one(sp, "buyers_page"), 1, 1, 1000);

  const dealsTable = table(pageContract, "sales-deals");
  const pageSizes = dealsTable.page_size_options.length > 0 ? dealsTable.page_size_options : [DEFAULT_LIMIT];
  const limit = boundedInt(one(sp, "limit"), pageSizes[0], 1, 100);
  const offset = boundedInt(one(sp, "offset"), 0, 0, 10000);

  // The page's data in ONE parallel read: the overview contract (every block above the ledger),
  // one ledger page, and the vendor register the deal drawer names (LocalOverlayLink opens
  // without an RSC request, so drawer data must ride with the page). Fetch = render: the
  // weighing count belongs to Farm value.
  // No /sales/options: it feeds only the record-sale FORM, and this page's contract declares no
  // record control (read-only by contract), so reading it on every filter change was pure waste.
  const [overviewResult, dealsResult, vendorOptionsResult] = await Promise.all([
    getSalesOverview({ farm }),
    listSalesDeals({ farm, limit, offset }),
    // The deal drawer here is a READ-ONLY detail, and it still names the buyer's vendor. Resolving
    // that id to the register's name needs the active register with the page. ONE bounded read,
    // never a paged walk of /procurement/vendors: that is the banned SSR full-walk shape.
    listProcurementVendorOptions(),
  ]);
  if (firstAuthRequiredError(overviewResult, dealsResult)) redirect(INTERNAL_LOGIN_PATH);
  const overview: SalesOverview | null = overviewResult.ok ? overviewResult.data : null;

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
    <div className="screen on">
      <SalesPageHeader pageContract={pageContract} />

      {!overviewResult.ok ? (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          {salesErrorText(overviewResult.error, copy(pageContract, "error.load"))}
        </Alert>
      ) : null}

      <SalesFarmToggle
        pageContract={pageContract}
        pagePath={PAGE_PATH}
        searchParams={sp}
        parkId={parkId}
        parks={parks}
        clears={["offset", "buyers_page", "deal_id"]}
      />

      {overview ? (
        <SoldSections
          overview={overview}
          pageContract={pageContract}
          chartKey={farm}
          buyersPage={buyersPage}
          buyersHref={(page) => hrefWithQuery(PAGE_PATH, sp, { buyers_page: page > 1 ? String(page) : null })}
        />
      ) : null}

      {/* LAST — the deals ledger (maintainer instruction 2026-09-11: "the deals table keep it at
          last"). Whole-filter total from the backend, server-paged, row opens the read-only
          deal drawer. */}
      <section className="card">
        <div className="hd">
          <Banknote className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.ledger.title")}</h3>
          {/* The WHOLE-FILTER total from the backend, not deals.length. */}
          <Tag tone={total ? "info" : "mut"}>
            {num(total)} {copy(pageContract, "summary.count")}
          </Tag>
          <div className="sp" style={{ flex: 1 }} />
        </div>

        {!dealsResult.ok ? (
          <Alert severity="error" style={{ marginBottom: 14 }}>
            {salesErrorText(dealsResult.error, copy(pageContract, "error.load"))}
          </Alert>
        ) : null}

        {deals.length === 0 ? (
          <EmptyState title={farm !== SALES_DEFAULT_FARM ? copy(pageContract, "empty.deals") : copy(pageContract, "empty.deals.unset")} />
        ) : (
          <div id="sales-sold-deals" className="twrap" tabIndex={0} role="region" aria-label={copy(pageContract, "section.ledger.aria")}>
            <Table
              className="sales-deals-table"
              aria-label={copy(pageContract, "section.ledger.aria")}
              sx={{
                // Single-line ledger cells: the global .celllink overflow-wrap:anywhere otherwise splits
                // "2026-08-11" and "CPT" mid-token. Headers may wrap so a 1-2px overshoot never scrolls the ledger.
                "&& td, && td .celllink": { whiteSpace: "nowrap", overflowWrap: "normal", wordBreak: "normal" },
                "&& td .celllink": { maxWidth: "none", minWidth: "max-content" },
                "&& th": { whiteSpace: "normal", overflowWrap: "normal", wordBreak: "normal" },
              }}
            >
              <TableHead>
                <TableRow>
                  {dealColumns.map((label) => (
                    <TableCell component="th" key={label}>{label}</TableCell>
                  ))}
                </TableRow>
              </TableHead>
              <TableBody>
                {deals.map((deal) => {
                  const drawerHref = hrefWithQuery(PAGE_PATH, sp, { deal_id: deal.deal_id });
                  const dealCell = (value: ReactNode, extra?: string) => (
                    <TableCell className={extra}>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        {value}
                      </LocalOverlayLink>
                    </TableCell>
                  );
                  return (
                    <TableRow key={deal.deal_id}>
                      {dealCell(humanDate(deal.sale_date))}
                      {dealCell(deal.farm)}
                      {dealCell(<IdentityCell primary={deal.buyer_name} secondary={deal.product_type || undefined} />)}
                      {dealCell(deal.product_type)}
                      {dealCell(breedBeyondProduct(deal.product_type, deal.breed) ?? "")}
                      {dealCell(deal.animal_count == null ? none : num(deal.animal_count), "num")}
                      {dealCell(deal.total_weight_kg == null ? none : num(deal.total_weight_kg, 1), "num")}
                      {dealCell(inr(deal.sales_value), "num")}
                      {dealCell(<Tag tone={dealStatusTone(deal.status)}>{deal.status}</Tag>)}
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
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
      </section>

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
    </div>
  );
}
