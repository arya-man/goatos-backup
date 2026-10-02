import type { ReactNode } from "react";

import { redirect } from "next/navigation";
import { Banknote, Boxes } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import {
  actionFeedbackCopy,
  controlEnabled,
  copy,
  table,
  tableLabels,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, listProcurementVendorOptions, listSaleLocations } from "@/lib/api/server";
import type { ProcurementVendorOptions, SaleLocationCatalog } from "@/lib/api/server";
import { getLoadwiseSales, getSalesOptions, listSalesDeals, listSellableProducts } from "@/lib/api/procurement-server";
import type { LoadwiseLoad, SalesDeal } from "@/lib/api/procurement";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { breedBeyondProduct, dealStatusTone, humanDate, inr, num } from "./sales-format";
import { SalesItemsAndRecordDrawer } from "./sales-config-items";
import { SaleAllocationDrawer } from "./sale-allocation-drawer";
import { LoadCostDrawer } from "./load-cost-drawer";
import { getMarketConfig, getMarketReporters } from "@/lib/api/market-server";
import { getValuationAssumptions } from "@/lib/api/sales-valuation-server";
import { MarketConfigSection } from "./market-config-section";
import { MarketReportersSection } from "./market-reporters-section";
import { ValuationSection } from "./valuation-section";
import { salesErrorText } from "./sales-error";

const PAGE_PATH = "/sales/config";
const DEFAULT_LIMIT = 25;

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

/**
 * Sales Config — the ONE place a sales fact is entered or changed (maintainer decision
 * 2026-09-01).
 *
 * Every sales write lives here: recording a sale and the animals it is made of, the payments
 * and status edits on a deal, and a purchased load's landed cost.
 * `/sales` and `/sales/loads` read those same facts back and declare no write of their own.
 *
 * Why one page rather than a button on each read page: the two boards are read at a different
 * time and by a different eye than the desk work of entering a sale, and an entry form that
 * exists in two places is a form whose two copies drift. The backend enforces the same split —
 * neither read page's contract declares a write control, so neither can render one.
 *
 * The forms themselves are the SAME drawer components the read pages used to mount; they were
 * moved, not rewritten, so the field vocabulary, the idempotency keys and the blocked-safe
 * boundaries are unchanged.
 */
export async function SalesConfigPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;

  const dealsTable = table(pageContract, "sales-deals");
  const pageSizes = dealsTable.page_size_options.length > 0 ? dealsTable.page_size_options : [DEFAULT_LIMIT];
  const limit = boundedInt(one(sp, "limit"), pageSizes[0], 1, 100);
  const offset = boundedInt(one(sp, "offset"), 0, 0, 10000);

  // Every drawer opens from this data: a LocalOverlayLink changes the URL without an RSC request,
  // so a form that fetched on open would never see its own data arrive. Keep the reads serialized:
  // they are individually bounded, but firing all of them during Cloud Run warmup can still produce
  // the backend_down/admin-contract failure screens this page must avoid.
  // serial-await: allow bounded sales/config bootstrap reads are intentionally serialized to avoid Cloud Run warmup fanout.
  const dealsResult = await listSalesDeals({ farm: "all", limit, offset });
  const loadwiseResult = await getLoadwiseSales();

  // The tag-animals picker's park/shed/pen vocabulary, backend-owned.
  // serial-await: allow bounded vocabulary reads stay serialized with the sales/config bootstrap above.
  const saleLocations = await listSaleLocations();
  // The record-sale drawer's two vocabularies: who may be sold TO (maintainer decision
  // 2026-08-27; ONE bounded read, never a paged walk of /procurement/vendors, which is the banned
  // SSR full-walk shape) and WHAT may be sold (the farm's own registry, migration 000422). They
  // are independent, so they are read TOGETHER rather than one after the other -- and each is
  // handled on its own below, so one failing does not take the other down.
  // serial-await: allow one bounded pair of drawer vocabulary reads after the sales/config core data.
  const [vendorOptionsResult, salesOptionsResult, sellableProductsResult] = await Promise.all([
    listProcurementVendorOptions(),
    getSalesOptions(),
    listSellableProducts(),
  ]);
  // The market survey's cities and questions (maintainer decision 2026-09-14): one bounded
  // read of the whole authored config.
  // serial-await: allow one bounded market-config read after prior sales/config reads to avoid request fanout.
  const marketConfigResult = await getMarketConfig();
  // serial-await: allow one bounded market-reporter read stays serialized with sales/config bootstrap to avoid request fanout.
  const marketReportersResult = await getMarketReporters();
  // serial-await: allow one bounded valuation read stays serialized with sales/config bootstrap to avoid request fanout.
  const valuationResult = await getValuationAssumptions();

  if (firstAuthRequiredError(dealsResult, loadwiseResult)) redirect(INTERNAL_LOGIN_PATH);

  const deals: SalesDeal[] = dealsResult.ok ? dealsResult.data.deals : [];
  const total = dealsResult.ok ? dealsResult.data.total : 0;
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const pageNumber = Math.min(pageCount, Math.floor(offset / limit) + 1);
  const loads: LoadwiseLoad[] = loadwiseResult.ok ? loadwiseResult.data.loads : [];
  // null means the park/shed catalog could NOT be read, which is a different fact from a
  // farm that has no sheds. Collapsing the two rendered a picker offering "Choose a park"
  // and nothing to choose, and read as a broken screen rather than a missing grant.
  const tagLocations: SaleLocationCatalog | null = saleLocations.ok ? saleLocations.data : null;
  // null means the register could NOT be read (its own permission), which is a different fact
  // from an EMPTY register; the drawer gives the two different copy.
  const vendorOptions: ProcurementVendorOptions | null = vendorOptionsResult.ok ? vendorOptionsResult.data : null;
  const salesOptions = salesOptionsResult.ok ? salesOptionsResult.data : null;
  const sellableProducts = sellableProductsResult.ok ? sellableProductsResult.data : null;

  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  // ONE backend-composed sentence beneath the banner's contract copy, for a refusal whose useful
  // part is a figure no fixed copy key could carry -- what the feed store actually holds. It is
  // farm copy the backend owns, rendered verbatim; the page's BEHAVIOUR keys on action_key.
  const actionDetail = one(sp, "action_detail");
  const canRecord = controlEnabled(pageContract, "record_sale", false);
  const canAllocateAnimals = controlEnabled(pageContract, "allocate_sale_animals", false);
  const canRecordCost = controlEnabled(pageContract, "record_load_cost", false);
  const canConfigureMarket = controlEnabled(pageContract, "market_config_write", false);
  const valuationControl = pageContract.controls.find((c) => c.id === "edit_valuation");
  const canEditValuation = valuationControl?.enabled ?? false;
  const none = copy(pageContract, "value.none");
  const advanceOnlyLabel = copy(pageContract, "value.advance_only");
  const dealColumns = tableLabels(pageContract, "sales-deals");
  const listHref = hrefWithQuery(sp, { deal_id: null, cost_load: null, tag_sale: null });
  const pagerHref = (nextOffset: number) => hrefWithQuery(sp, { offset: nextOffset > 0 ? String(nextOffset) : null });

  return (
    <div className="screen on">
      <div className="phead" style={{ marginTop: 12, alignItems: "flex-end", paddingBottom: 6 }}>
        <div>
          <div className="crumb">
            <b>{copy(pageContract, "crumb")}</b> · {pageContract.title}
          </div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {canRecord ? (
          <LocalOverlayLink
            href={hrefWithQuery(sp, { deal_id: "new" })}
            className="btn primary"
            scroll={false}
            style={{ marginBottom: 4 }}
          >
            {copy(pageContract, "action.record_sale.label")}
          </LocalOverlayLink>
        ) : null}
        {/* Tagging animals to a sale WRITES HERD IDENTITY — it exits each animal as sold — so it
            is gated on the same capability as recording the deal, and additionally on there
            being a recorded sale to tag animals to. */}
        {canAllocateAnimals && deals.length > 0 ? (
          <LocalOverlayLink
            href={hrefWithQuery(sp, { tag_sale: deals[0].deal_id })}
            className="btn"
            scroll={false}
            style={{ marginBottom: 4 }}
            title={copy(pageContract, "action.tag_animals.hint")}
          >
            {copy(pageContract, "action.tag_animals.label")}
          </LocalOverlayLink>
        ) : null}
      </div>

      {/* Write feedback. Without this a save simply closes the drawer, which is
          indistinguishable from the save being dropped. */}
      {actionStatus ? (
        actionStatus === "success" ? (
          <div className="note" style={{ marginBottom: 14 }}>
            {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        ) : (
          <div className="alert" style={{ marginBottom: 14 }}>
            {/* ONE flex child: .alert lays its children out in a row, so a detail sentence beside
                the headline gets squeezed and clipped at the card edge. Stacked inside a single
                block, the sentence gets the card's full width and wraps. */}
            <div style={{ minWidth: 0 }}>
              {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
              {actionDetail ? <div className="note" style={{ marginTop: 6 }}>{actionDetail}</div> : null}
            </div>
          </div>
        )
      ) : null}

      {/* 1 — the sales themselves. The ledger is here as the way IN to a deal's payment and
          status edits, not as a board: clicking a row opens the same drawer the write uses. */}
      <section className="card">
        <div className="hd">
          <Banknote className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.sales_entry.title")}</h3>
          {/* The WHOLE-FILTER total from the backend, not deals.length. */}
          <Tag tone={total ? "info" : "mut"}>
            {num(total)} {copy(pageContract, "summary.count")}
          </Tag>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{copy(pageContract, "section.sales_entry.row_hint")}</span>
        </div>
        <p className="muted small sales-config-card-copy">
          {copy(pageContract, "section.sales_entry.subtitle")}
        </p>

        {!dealsResult.ok ? (
          <div className="alert" style={{ marginBottom: 14 }}>
            {salesErrorText(dealsResult.error, copy(pageContract, "error.load"))}
          </div>
        ) : null}

        {/* A failed read is not an empty ledger: under the error box, "No sales recorded yet.
            Record the first sale" told the desk the opposite of what happened. */}
        {!dealsResult.ok ? null : deals.length === 0 ? (
          <div className="empty">{copy(pageContract, "empty.deals.unset")}</div>
        ) : (
          <div className="twrap" tabIndex={0} role="region" aria-label={copy(pageContract, "section.sales_entry.title")}>
            <table className="sales-deals-table" aria-label={copy(pageContract, "section.sales_entry.title")}>
              <thead>
                <tr>
                  {dealColumns.map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {deals.map((deal) => {
                  const drawerHref = hrefWithQuery(sp, { deal_id: deal.deal_id });
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
              <Link href={pagerHref(Math.max(0, offset - limit))} scroll={false} className="btn">
                {copy(pageContract, "action.prev_page")}
              </Link>
            ) : null}
            {pageNumber < pageCount ? (
              <Link href={pagerHref(offset + limit)} scroll={false} className="btn">
                {copy(pageContract, "action.next_page")}
              </Link>
            ) : null}
          </div>
        ) : null}
      </section>

      {/* 3 — Purchase and Born: a load's landed cost. Its own permission, so this section can be
          the only inert one on an otherwise live page. */}
      <section className="card">
        <div className="hd">
          <Boxes className="ic" style={{ color: "var(--ok)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.load_entry.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{copy(pageContract, "section.load_entry.row_hint")}</span>
        </div>
        <p className="muted small sales-config-card-copy">
          {copy(pageContract, "section.load_entry.subtitle")}
        </p>

        {!loadwiseResult.ok ? (
          <div className="alert" style={{ marginBottom: 14 }}>
            {salesErrorText(loadwiseResult.error, copy(pageContract, "error.load"))}
          </div>
        ) : null}

        {!canRecordCost ? <div className="note">{copy(pageContract, "disabled.load_cost")}</div> : null}

        {!loadwiseResult.ok ? null : loads.length === 0 ? (
          <div className="empty">{copy(pageContract, "empty.loads")}</div>
        ) : (
          <div className="twrap" tabIndex={0} role="region" aria-label={copy(pageContract, "section.load_entry.title")}>
            <table className="sales-load-entry-table" aria-label={copy(pageContract, "section.load_entry.title")}>
              <thead>
                <tr>
                  <th>{copy(pageContract, "column.load")}</th>
                  <th>{copy(pageContract, "column.farm")}</th>
                  <th className="num">{copy(pageContract, "column.purchased")}</th>
                  <th className="num">{copy(pageContract, "column.sold")}</th>
                  <th className="num">{copy(pageContract, "column.remaining")}</th>
                  <th className="num">{copy(pageContract, "column.purchase_value")}</th>
                </tr>
              </thead>
              <tbody>
                {loads.map((load) => {
                  const costHref = hrefWithQuery(sp, { cost_load: load.load_id });
                  const costCell = (value: ReactNode, extra?: string) => (
                    <td className={extra}>
                      <LocalOverlayLink href={costHref} className="celllink" scroll={false}>
                        {value}
                      </LocalOverlayLink>
                    </td>
                  );
                  return (
                    <tr key={load.load_id}>
                      {costCell(
                        <>
                          <b>{load.load_ref || load.vendor_name}</b>
                          {load.purchase_date ? (
                            <span className="muted small"> · {humanDate(load.purchase_date)}</span>
                          ) : null}
                        </>,
                      )}
                      {costCell(load.farm || none)}
                      {costCell(num(load.purchased), "num")}
                      {costCell(num(load.sold), "num")}
                      {costCell(num(load.remaining), "num")}
                      {/* Absent cost is "not recorded", never ₹0: a load bought with no recorded
                          cost and one that cost nothing are different facts. */}
                      {costCell(
                        load.purchase_value == null ? copy(pageContract, "value.cost_missing") : inr(load.purchase_value),
                        "num",
                      )}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* 5 — Market survey: what the morning calls ask. Its own permission (the Sales module's
          Configure level), so like the load-cost section it can be the inert one on a live page. */}
      <MarketConfigSection
        pageContract={pageContract}
        configResult={marketConfigResult}
        canConfigure={canConfigureMarket}
      />
      {/* Who makes the calls (maintainer instruction 2026-09-19): beside the survey it reports. */}
      <MarketReportersSection pageContract={pageContract} result={marketReportersResult} canConfigure={canConfigureMarket} />

      {/* 6 — Farm valuation (maintainer instruction 2026-09-19): the decided figures behind Farm
          value and Load wise. Same gating shape as the market survey. */}
      <ValuationSection
        pageContract={pageContract}
        result={valuationResult}
        canEdit={canEditValuation}
        disabledReason={valuationControl?.disabled_reason ?? ""}
      />

      {/* Always mounted: LocalOverlayLink changes the URL without an RSC request, so an overlay
          gated on a server-read search param would never appear. */}
      {/* 4 — WHAT WE SELL (maintainer instruction 2026-09-23) and the record-sale drawer, as ONE
          client boundary: adding an item here puts it in that drawer's dropdown with no reload.
          The drawer is always mounted -- LocalOverlayLink changes the URL without an RSC request,
          so an overlay gated on a server-read search param would never appear. */}
      <SalesItemsAndRecordDrawer
        productsPage={sellableProducts}
        salesOptions={salesOptions}
        pageContract={pageContract}
        canWriteProducts={controlEnabled(pageContract, "record_sellable_product", false)}
        productsDisabledReason={
          pageContract.controls?.find((control) => control.id === "record_sellable_product")?.disabled_reason ?? ""
        }
        deals={deals}
        listHref={listHref}
        canRecord={canRecord}
        vendorOptions={vendorOptions}
        stockConfirmNeeded={actionKey === "action.sale_feed_stock_confirm"}
        statusStockConfirmNeeded={actionKey === "action.deal_status_feed_stock_confirm"}
        stockConfirmDetail={actionDetail}
      />
      {canAllocateAnimals ? (
        <SaleAllocationDrawer
          deals={deals}
          locations={tagLocations}
          pageContract={pageContract}
          listHref={listHref}
        />
      ) : null}
      <LoadCostDrawer
        loads={loads}
        pageContract={pageContract}
        listHref={listHref}
        canRecordCost={canRecordCost}
      />
    </div>
  );
}
