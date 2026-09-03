import type { ReactNode } from "react";

import { redirect } from "next/navigation";
import { Banknote, Boxes, ClipboardList } from "lucide-react";

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
import {
  getLoadwiseSales,
  listSalesBuyerLeads,
  listSalesDeals,
  listSalesFpoLeads,
} from "@/lib/api/procurement-server";
import type { LoadwiseLoad, SalesDeal } from "@/lib/api/procurement";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { dealStatusTone, humanDate, inr, num } from "./sales-format";
import { SalesRecordDrawer } from "./sales-record-drawer";
import { SaleAllocationDrawer } from "./sale-allocation-drawer";
import { SalesPipelineDrawers, type SalesPanel } from "./sales-pipeline-drawers";
import { LoadCostDrawer } from "./load-cost-drawer";

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

/** One pipeline/evidence entry button. Shown only when the backend grants the write. */
function panelButton(href: string, label: string) {
  return (
    <LocalOverlayLink href={href} className="btn sm" scroll={false}>
      {label}
    </LocalOverlayLink>
  );
}

/**
 * Sales Config — the ONE place a sales fact is entered or changed (maintainer decision
 * 2026-09-01).
 *
 * Every sales write lives here: recording a sale and the animals it is made of, the buyer and
 * farmer-group leads, market quotes, sold-tag lists and weight checks the retired Sales DB sheet
 * used to carry, the payments and status edits on a deal, and a purchased load's landed cost.
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
  const canRecordPipeline = controlEnabled(pageContract, "record_pipeline", false);

  // The whole screen's data in ONE parallel read. Every drawer opens from this data: a
  // LocalOverlayLink changes the URL without an RSC request, so a form that fetched on open would
  // never see its own data arrive.
  const [dealsResult, loadwiseResult, buyerLeadsResult, fpoLeadsResult, saleLocations, vendorOptionsResult] =
    await Promise.all([
      listSalesDeals({ farm: "all", limit, offset }),
      getLoadwiseSales(),
      canRecordPipeline ? listSalesBuyerLeads({ limit: 20 }) : Promise.resolve(null),
      canRecordPipeline ? listSalesFpoLeads({ limit: 20 }) : Promise.resolve(null),
      // The tag-animals picker's park/shed/pen vocabulary, backend-owned.
      listSaleLocations(),
      // Every sale is made TO a vendor (maintainer decision 2026-08-27). ONE bounded read, never
      // a paged walk of /procurement/vendors: that is the banned SSR full-walk shape.
      listProcurementVendorOptions(),
    ]);

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
  const buyerLeadPage = buyerLeadsResult?.ok ? buyerLeadsResult.data : { leads: [], total: 0, status_options: [] };
  const fpoLeadPage = fpoLeadsResult?.ok ? fpoLeadsResult.data : { leads: [], total: 0, status_options: [] };

  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const canRecord = controlEnabled(pageContract, "record_sale", false);
  const canAllocateAnimals = controlEnabled(pageContract, "allocate_sale_animals", false);
  const canRecordCost = controlEnabled(pageContract, "record_load_cost", false);
  const none = copy(pageContract, "value.none");
  const dealColumns = tableLabels(pageContract, "sales-deals");
  const listHref = hrefWithQuery(sp, { deal_id: null, panel: null, cost_load: null, tag_sale: null });
  const panelHrefFor = (panel: SalesPanel) => hrefWithQuery(sp, { deal_id: null, cost_load: null, panel });
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
            {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
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
            <b>{dealsResult.error.code ?? dealsResult.error.kind}</b>&nbsp;
            {dealsResult.error.message || copy(pageContract, "error.load")}
          </div>
        ) : null}

        {deals.length === 0 ? (
          <div className="empty">{copy(pageContract, "empty.deals.unset")}</div>
        ) : (
          <div className="twrap">
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
                      {dealCell(deal.sale_date)}
                      {dealCell(deal.farm)}
                      {dealCell(<b>{deal.buyer_name}</b>)}
                      {dealCell(deal.product_type)}
                      {dealCell(deal.breed)}
                      {dealCell(deal.animal_count == null ? none : num(deal.animal_count), "num")}
                      {dealCell(deal.total_weight_kg == null ? none : num(deal.total_weight_kg, 1), "num")}
                      {dealCell(inr(deal.sales_value), "num")}
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

      {canRecordPipeline ? (
        <section className="card">
          <div className="hd">
            <ClipboardList className="ic" style={{ color: "var(--warn)" }} aria-hidden="true" />
            <h3>{copy(pageContract, "section.pipeline_entry.title")}</h3>
          </div>
          <p className="muted small sales-config-card-copy">
            {copy(pageContract, "section.pipeline_entry.subtitle")}
          </p>
          <div className="chips">
            {panelButton(panelHrefFor("buyer_leads"), copy(pageContract, "action.add_lead"))}
            {panelButton(panelHrefFor("fpo_leads"), copy(pageContract, "action.add_fpo"))}
            {panelButton(panelHrefFor("quote"), copy(pageContract, "action.add_quote"))}
            {panelButton(panelHrefFor("tags"), copy(pageContract, "action.add_tags"))}
            {panelButton(panelHrefFor("weight_check"), copy(pageContract, "action.add_weight_check"))}
          </div>
        </section>
      ) : null}

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
            <b>{loadwiseResult.error.code ?? loadwiseResult.error.kind}</b>&nbsp;
            {loadwiseResult.error.message || copy(pageContract, "error.load")}
          </div>
        ) : null}

        {!canRecordCost ? <div className="note">{copy(pageContract, "disabled.load_cost")}</div> : null}

        {loads.length === 0 ? (
          <div className="empty">{copy(pageContract, "empty.loads")}</div>
        ) : (
          <div className="twrap">
            <table aria-label={copy(pageContract, "section.load_entry.title")}>
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

      {/* Always mounted: LocalOverlayLink changes the URL without an RSC request, so an overlay
          gated on a server-read search param would never appear. */}
      <SalesRecordDrawer
        deals={deals}
        pageContract={pageContract}
        listHref={listHref}
        canRecord={canRecord}
        vendorOptions={vendorOptions}
      />
      {canAllocateAnimals ? (
        <SaleAllocationDrawer
          deals={deals}
          locations={tagLocations}
          pageContract={pageContract}
          listHref={listHref}
        />
      ) : null}
      <SalesPipelineDrawers
        pageContract={pageContract}
        listHref={listHref}
        panelHrefs={{
          buyer_leads: panelHrefFor("buyer_leads"),
          fpo_leads: panelHrefFor("fpo_leads"),
          quote: panelHrefFor("quote"),
          tags: panelHrefFor("tags"),
          weight_check: panelHrefFor("weight_check"),
        }}
        canRecord={canRecordPipeline}
        buyerLeads={buyerLeadPage.leads}
        buyerStatusOptions={buyerLeadPage.status_options}
        fpoLeads={fpoLeadPage.leads}
        fpoStatusOptions={fpoLeadPage.status_options}
      />
      <LoadCostDrawer
        loads={loads}
        pageContract={pageContract}
        listHref={listHref}
        canRecordCost={canRecordCost}
      />
    </div>
  );
}
