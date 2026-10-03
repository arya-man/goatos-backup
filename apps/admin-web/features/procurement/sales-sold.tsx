import type { ReactNode } from "react";

import Link from "@/components/no-prefetch-link";
import { LinkPending } from "@/components/link-pending";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Banknote } from "lucide-react";

import { HBarList } from "@/components/hbar-list";
import { MonthColumns } from "@/components/month-columns";
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
  inrCompact,
  monthLabel,
  monthlyAnimalRevenueTotal,
  monthlyAnimalsTotal,
  monthlyRevenueTotal,
  num,
  trimEmptyMonthlyStart,
  breedBeyondProduct,
} from "./sales-format";
import { SalesRecordDrawer } from "./sales-record-drawer";
import { SALES_DEFAULT_FARM, SalesFarmToggle, SalesPageHeader, hrefWithQuery, readSalesParkScope } from "./sales-chrome";
import { salesErrorText } from "./sales-error";

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
}: {
  overview: SalesOverview;
  pageContract: AdminUiPageContract;
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
  return (
    <>
          {/* SOLD — what has already left the farm. Same tiles as before, minus the two valuation
              ones that moved up into their own block. */}
          <div className="sales-block-hd sales-block-hd-spaced" role="presentation">
            <h3>{copy(pageContract, "section.sold.title")}</h3>
            <span className="muted small">{copy(pageContract, "section.sold.sub")}</span>
          </div>
          <section className="grid g4 kpi-row sales-kpi-row" aria-label={copy(pageContract, "section.sold.aria")}>
            <div className="kpi">
              <div className="lab">{copy(pageContract, "kpi.revenue")}</div>
              <div className="val">{inr(summary.revenue)}</div>
              <div className="dl">
                {num(summary.deals)} {copy(pageContract, "kpi.deals")}
              </div>
            </div>
            <div className="kpi">
              <div className="lab">{copy(pageContract, "kpi.animals")}</div>
              <div className="val">{num(summary.animals)}</div>
              <div className="dl" title={copy(pageContract, "kpi.animals.detail")}>
                {num(summary.sheep)} {seriesLabel("sheep")} · {num(summary.goats)} {seriesLabel("goat")}
              </div>
            </div>
            <div className="kpi">
              <div className="lab">{copy(pageContract, "kpi.realized_price")}</div>
              {/* Zero means no weighed live sale exists — printing ₹0 per kg would claim we give
                  animals away. */}
              <div className="val">
                {summary.realized_price_per_kg > 0 ? `${inr(summary.realized_price_per_kg)} ${perKgSuffix}` : none}
              </div>
              <div className="dl">{copy(pageContract, "kpi.realized_price.hint")}</div>
            </div>
            <div className="kpi">
              <div className="lab">{copy(pageContract, "kpi.manure")}</div>
              <div className="val">
                {num(summary.manure_kg)} {kgSuffix}
              </div>
              <div className="dl">
                {inr(summary.manure_revenue)} · {copy(pageContract, "kpi.manure.detail")}
              </div>
            </div>
            {/* Feed sold off the store is in the revenue above; without its own tile the headline
                could not be read back into what was sold. Shown only when some was sold. */}
            {summary.feed_kg > 0 || summary.feed_revenue > 0 ? (
              <div className="kpi">
                <div className="lab">{copy(pageContract, "kpi.feed")}</div>
                <div className="val">
                  {num(summary.feed_kg)} {kgSuffix}
                </div>
                <div className="dl">
                  {inr(summary.feed_revenue)} · {copy(pageContract, "kpi.feed.detail")}
                </div>
              </div>
            ) : null}
          </section>

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
            <p className="muted small" style={{ marginTop: 0 }}>
              {copy(pageContract, "section.sold_weight.subtitle")}
            </p>
            {overview.sold_weight_bands.total === 0 ? (
              <div className="empty">{copy(pageContract, "empty.sold_weight")}</div>
            ) : (
              <>
                <p className="muted small">
                  {num(overview.sold_weight_bands.total)} {copy(pageContract, "sold_weight.total")}
                  {overview.sold_weight_bands.unweighed > 0
                    ? ` · ${num(overview.sold_weight_bands.unweighed)} ${copy(pageContract, "sold_weight.unweighed")}`
                    : ""}
                </p>
                {/* All FOUR bands, always, zeros included: the maintainer asked to see the count in
                    each range, and a band that vanishes when it is empty reads as a band that does
                    not exist. Hence tiles rather than the bar list, which drops zero rows. The band
                    ORDER is the backend's, heaviest first; the page does not re-sort it. */}
                <div className="grid g4 kpi-row" style={{ marginTop: 8 }}>
                  {overview.sold_weight_bands.bands.map((band) => (
                    <div key={band.band} className="kpi" data-band={band.band}>
                      <div className="lab">{copy(pageContract, `sold_weight.band.${band.band}`)}</div>
                      <div className="val">{num(band.total)}</div>
                      {/* The split is named only where it exists: a band whose animals were all
                          weighed the same way says nothing extra rather than repeating itself. */}
                      <div className="dl">
                        {[
                          band.measured > 0 ? `${num(band.measured)} ${copy(pageContract, "sold_weight.source.measured")}` : "",
                          band.load_average > 0 ? `${num(band.load_average)} ${copy(pageContract, "sold_weight.source.load_average")}` : "",
                          band.estimated > 0 ? `${num(band.estimated)} ${copy(pageContract, "sold_weight.source.estimated")}` : "",
                        ]
                          .filter(Boolean)
                          .join(" · ") || copy(pageContract, "sold_weight.total")}
                      </div>
                    </div>
                  ))}
                </div>
              </>
            )}
          </section>

          {/* 2 — month by month. Three separate charts: rupees, heads and kg never share an axis.
              Stacked full-width so every column carries its month label and value. */}
          <section className="card sales-card" aria-label={copy(pageContract, "section.monthly.aria")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.monthly.title")}</h3>
            </div>
            <div className="mt" style={{ marginTop: 4 }}>
              {copy(pageContract, "chart.monthly_revenue.title")}
            </div>
            <MonthColumns
              data={trimEmptyMonthlyStart(overview.monthly, monthlyRevenueTotal).map((month) => ({
                key: month.month,
                axisLabel: monthLabel(month.month),
                label: monthLabel(month.month),
                value: monthlyRevenueTotal(month),
                display: inrCompact(monthlyRevenueTotal(month)),
              }))}
              chartLabel={copy(pageContract, "chart.monthly_revenue.title")}
              valueNoun={copy(pageContract, "chart.monthly_revenue.value")}
              emptyLabel={copy(pageContract, "chart.monthly_revenue.empty")}
            />
            <div className="mt">{copy(pageContract, "chart.monthly_animals.title")}</div>
            {/* Head count owns the bar; the rupees it earned ride under the month label so the two
                units are read separately and never share the axis. */}
            <MonthColumns
              data={trimEmptyMonthlyStart(overview.monthly, monthlyAnimalsTotal).map((month) => ({
                key: month.month,
                axisLabel: monthLabel(month.month),
                label: monthLabel(month.month),
                value: monthlyAnimalsTotal(month),
                display: num(monthlyAnimalsTotal(month)),
                subDisplay:
                  monthlyAnimalRevenueTotal(month) > 0 ? inrCompact(monthlyAnimalRevenueTotal(month)) : "",
              }))}
              chartLabel={copy(pageContract, "chart.monthly_animals.title")}
              valueNoun={copy(pageContract, "chart.monthly_animals.value")}
              subValueNoun={copy(pageContract, "chart.monthly_animals.sub")}
              emptyLabel={copy(pageContract, "chart.monthly_animals.empty")}
            />
            <div className="mt">{copy(pageContract, "chart.monthly_manure.title")}</div>
            <MonthColumns
              data={trimEmptyMonthlyStart(overview.monthly, (month) => month.manure_kg).map((month) => ({
                key: month.month,
                axisLabel: monthLabel(month.month),
                label: monthLabel(month.month),
                value: month.manure_kg,
                // The whole figure with its unit: "27k" read as a mystery letter (2026-10-03).
                display: `${num(month.manure_kg)} ${kgSuffix}`,
                subDisplay: month.manure_revenue > 0 ? inrCompact(month.manure_revenue) : "",
              }))}
              chartLabel={copy(pageContract, "chart.monthly_manure.title")}
              valueNoun={copy(pageContract, "chart.monthly_manure.value")}
              subValueNoun={copy(pageContract, "chart.monthly_manure.sub")}
              emptyLabel={copy(pageContract, "chart.monthly_manure.empty")}
            />
            {/* Feed sold off the store (2026-10-03), the manure chart's twin: its kilograms are the
                lines' QUANTITY, never live weight. Shown only once some feed has been sold. */}
            {overview.monthly.some((month) => month.feed_kg > 0 || month.feed_revenue > 0) ? (
              <>
                <div className="mt">{copy(pageContract, "chart.monthly_feed.title")}</div>
                <MonthColumns
                  data={trimEmptyMonthlyStart(overview.monthly, (month) => month.feed_kg).map((month) => ({
                    key: month.month,
                    axisLabel: monthLabel(month.month),
                    label: monthLabel(month.month),
                    value: month.feed_kg,
                    display: `${num(month.feed_kg)} ${kgSuffix}`,
                    subDisplay: month.feed_revenue > 0 ? inrCompact(month.feed_revenue) : "",
                  }))}
                  chartLabel={copy(pageContract, "chart.monthly_feed.title")}
                  valueNoun={copy(pageContract, "chart.monthly_feed.value")}
                  subValueNoun={copy(pageContract, "chart.monthly_feed.sub")}
                  emptyLabel={copy(pageContract, "chart.monthly_feed.empty")}
                />
              </>
            ) : null}
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
            <div className="hd">
              <h3>{copy(pageContract, "section.buyers.title")}</h3>
              <div className="sp" style={{ flex: 1 }} />
              <span className="muted small">{copy(pageContract, "section.buyers.subtitle")}</span>
            </div>
            {overview.buyers.length === 0 ? (
              <div className="empty">{copy(pageContract, "empty.buyers")}</div>
            ) : (
              <div className="twrap" tabIndex={0} role="region" aria-label={copy(pageContract, "section.buyers.title")}>
                <table aria-label={copy(pageContract, "section.buyers.title")}>
                  <thead>
                    <tr>
                      <th>{copy(pageContract, "column.buyer_name")}</th>
                      <th>{copy(pageContract, "column.buyer_place")}</th>
                      <th>{copy(pageContract, "column.product_types")}</th>
                      <th>{copy(pageContract, "column.deals")}</th>
                      <th>{copy(pageContract, "column.animals")}</th>
                      <th>{copy(pageContract, "column.revenue")}</th>
                      <th>{copy(pageContract, "column.share_pct")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {buyersRows.map((buyer) => (
                      <tr key={`${buyer.buyer_name}|${buyer.buyer_place}`}>
                        <td>
                          <b>{buyer.buyer_name}</b>
                        </td>
                        <td>{buyer.buyer_place || none}</td>
                        <td>{buyer.product_types.join(" · ")}</td>
                        <td className="num">{num(buyer.deals)}</td>
                        <td className="num">{num(buyer.animals)}</td>
                        <td className="num">{inr(buyer.revenue)}</td>
                        <td className="num">{num(buyer.share_pct, 1)}%</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {buyersPageCount > 1 ? (
              <div className="pager2">
                <span className="muted">
                  {copy(pageContract, "pager.page")} {buyersPageNumber} {copy(pageContract, "pager.of")}{" "}
                  {buyersPageCount} · {num(overview.buyers.length)} {copy(pageContract, "summary.buyers")}
                </span>
                {buyersPageNumber > 1 ? (
                  <Link href={buyersHref(buyersPageNumber - 1)} scroll={false} className="btn">
                    {copy(pageContract, "action.prev_page")}
                    <LinkPending />
                  </Link>
                ) : (
                  <span className="btn" aria-disabled="true">
                    {copy(pageContract, "action.prev_page")}
                  </span>
                )}
                {buyersPageNumber < buyersPageCount ? (
                  <Link href={buyersHref(buyersPageNumber + 1)} scroll={false} className="btn">
                    {copy(pageContract, "action.next_page")}
                    <LinkPending />
                  </Link>
                ) : (
                  <span className="btn" aria-disabled="true">
                    {copy(pageContract, "action.next_page")}
                  </span>
                )}
              </div>
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
  const deals: SalesDeal[] = dealsResult.ok ? dealsResult.data.deals : [];
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
  const advanceOnlyLabel = copy(pageContract, "value.advance_only");
  const dealColumns = tableLabels(pageContract, "sales-deals");
  const listHref = hrefWithQuery(PAGE_PATH, sp, { deal_id: null });

  return (
    <div className="screen on">
      <SalesPageHeader pageContract={pageContract} />

      {!overviewResult.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          {salesErrorText(overviewResult.error, copy(pageContract, "error.load"))}
        </div>
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
          <span className="muted small">{copy(pageContract, "section.ledger.row_hint")}</span>
        </div>

        {!dealsResult.ok ? (
          <div className="alert" style={{ marginBottom: 14 }}>
            {salesErrorText(dealsResult.error, copy(pageContract, "error.load"))}
          </div>
        ) : null}

        {deals.length === 0 ? (
          <div className="empty">
            {farm !== SALES_DEFAULT_FARM ? copy(pageContract, "empty.deals") : copy(pageContract, "empty.deals.unset")}
          </div>
        ) : (
          <div className="twrap" tabIndex={0} role="region" aria-label={copy(pageContract, "section.ledger.aria")}>
            <table className="sales-deals-table" aria-label={copy(pageContract, "section.ledger.aria")}>
              <thead>
                <tr>
                  {dealColumns.map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {deals.map((deal) => {
                  const drawerHref = hrefWithQuery(PAGE_PATH, sp, { deal_id: deal.deal_id });
                  const dealCell = (value: ReactNode, extra?: string) => (
                    <td className={extra}>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        {value}
                      </LocalOverlayLink>
                    </td>
                  );
                  return (
                    <tr key={deal.deal_id}>
                      {dealCell(humanDate(deal.sale_date))}
                      {dealCell(deal.farm)}
                      {dealCell(<b>{deal.buyer_name}</b>)}
                      {/* An advance taken before the sale was decided names no product yet (2026-10-02). */}
                      {dealCell(deal.advance_only ? advanceOnlyLabel : deal.product_type)}
                      {dealCell(breedBeyondProduct(deal.product_type, deal.breed) ?? "")}
                      {dealCell(deal.animal_count == null ? none : num(deal.animal_count), "num")}
                      {dealCell(deal.total_weight_kg == null ? none : num(deal.total_weight_kg, 1), "num")}
                      {dealCell(deal.advance_only ? none : inr(deal.sales_value), "num")}
                      {/* Backend-derived per-sale rates (domain.Deal.Rates), never divided here. */}
                      {dealCell(deal.price_per_kg == null ? none : num(deal.price_per_kg), "num")}
                      {dealCell(deal.weight_per_animal_kg == null ? none : num(deal.weight_per_animal_kg, 1), "num")}
                      {dealCell(deal.price_per_animal == null ? none : num(deal.price_per_animal), "num")}
                      {dealCell(<Tag tone={dealStatusTone(deal.status)}>{deal.status}</Tag>)}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {pageCount > 1 ? (
          <div className="pager2" style={{ paddingRight: 56 }}>
            <span className="muted">
              {copy(pageContract, "pager.page")} {pageNumber} {copy(pageContract, "pager.of")} {pageCount}
            </span>
            {pageNumber > 1 ? (
              <Link
                href={hrefWithQuery(PAGE_PATH, sp, {
                  offset: offset - limit > 0 ? String(offset - limit) : null,
                  deal_id: null,
                })}
                scroll={false}
                className="btn"
              >
                {copy(pageContract, "action.prev_page")}
                <LinkPending />
              </Link>
            ) : (
              <span className="btn" aria-disabled="true">
                {copy(pageContract, "action.prev_page")}
              </span>
            )}
            {pageNumber < pageCount ? (
              <Link
                href={hrefWithQuery(PAGE_PATH, sp, { offset: String(offset + limit), deal_id: null })}
                scroll={false}
                className="btn"
              >
                {copy(pageContract, "action.next_page")}
                <LinkPending />
              </Link>
            ) : (
              <span className="btn" aria-disabled="true">
                {copy(pageContract, "action.next_page")}
              </span>
            )}
          </div>
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
