import Table from "@mui/material/Table";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import type { ReactNode } from "react";

import { redirect } from "next/navigation";

import { LocalOverlayLink } from "@/components/local-overlay-link";
import { TONE_COLOR } from "@/components/ui-primitives";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/minimal/table/table-head-custom";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import ListItemText from "@mui/material/ListItemText";
import Stack from "@mui/material/Stack";
import { IdentityCell } from "@/components/data-table";
import { ProcurementTableFooter } from "./table-footer-links";
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
import { SalesPageHeader } from "./sales-chrome";
import { SaleAllocationDrawer } from "./sale-allocation-drawer";
import { LoadCostDrawer } from "./load-cost-drawer";
import { getMarketConfig, getMarketReporters } from "@/lib/api/market-server";
import { getValuationAssumptions } from "@/lib/api/sales-valuation-server";
import { MarketConfigSection } from "./market-config-section";
import { MarketReportersSection } from "./market-reporters-section";
import { ValuationSection } from "./valuation-section";
import Alert from "@mui/material/Alert";
import { EmptyState } from "@/components/app/empty-state";
import Button from "@mui/material/Button";
import { salesErrorText } from "./sales-error";
import { DEFAULT_LIMIT, SALES_CONFIG_TABS, type SalesConfigTab } from "./sales-config-layout";

const PAGE_PATH = "/sales/config";

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

  // Template account tabs (`?tab=`): one card per tab instead of one long stack of cards.
  const rawTab = one(sp, "tab") ?? "";
  const tab: SalesConfigTab = (SALES_CONFIG_TABS as readonly string[]).includes(rawTab) ? (rawTab as SalesConfigTab) : "sales";
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
  // Fetch = render: the market survey, its reporters and the farm valuation are read only on the
  // tab that shows them (template account tabs, FJ3 P1-3). The deals, loads and drawer
  // vocabularies above are read on every tab because the header's Record sale / Tag animals and
  // the load-cost drawer open from any of them.
  // serial-await: allow one bounded market-config read after prior sales/config reads to avoid request fanout.
  const marketConfigResult = tab === "market" ? await getMarketConfig() : null;
  // serial-await: allow one bounded market-reporter read stays serialized with sales/config bootstrap to avoid request fanout.
  const marketReportersResult = tab === "reporters" ? await getMarketReporters() : null;
  // serial-await: allow one bounded valuation read stays serialized with sales/config bootstrap to avoid request fanout.
  const valuationResult = tab === "valuation" ? await getValuationAssumptions() : null;

  if (firstAuthRequiredError(dealsResult, loadwiseResult)) redirect(INTERNAL_LOGIN_PATH);

  const deals: SalesDeal[] = dealsResult.ok ? listOrEmpty(dealsResult.data.deals) : [];
  const total = dealsResult.ok ? dealsResult.data.total : 0;
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const pageNumber = Math.min(pageCount, Math.floor(offset / limit) + 1);
  const loads: LoadwiseLoad[] = loadwiseResult.ok ? listOrEmpty(loadwiseResult.data.loads) : [];
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
  const dealColumns = tableLabels(pageContract, "sales-deals");
  const listHref = hrefWithQuery(sp, { deal_id: null, cost_load: null, tag_sale: null });
  const pagerHref = (nextOffset: number) => hrefWithQuery(sp, { offset: nextOffset > 0 ? String(nextOffset) : null });

  const tabLabel: Record<SalesConfigTab, string> = {
    sales: copy(pageContract, "section.sales_entry.title"),
    loads: copy(pageContract, "section.load_entry.title"),
    items: copy(pageContract, "section.sellable_products.title"),
    market: copy(pageContract, "section.market.title"),
    reporters: copy(pageContract, "market.reporters.title"),
    valuation: copy(pageContract, "section.valuation.title"),
  };
  const dealHead = dealColumns.map((label, index) => ({ id: `c${index}`, label, align: index >= 5 && index <= 7 ? ("right" as const) : undefined }));
  const loadHead = [
    { id: "load", label: copy(pageContract, "column.load") },
    { id: "farm", label: copy(pageContract, "column.farm") },
    { id: "purchased", label: copy(pageContract, "column.purchased"), align: "right" as const },
    { id: "sold", label: copy(pageContract, "column.sold"), align: "right" as const },
    { id: "remaining", label: copy(pageContract, "column.remaining"), align: "right" as const },
    { id: "purchase_value", label: copy(pageContract, "column.purchase_value"), align: "right" as const },
  ];

  return (
    <div className="screen on">
      <SalesPageHeader
        pageContract={pageContract}
        actions={
          <>
        {canRecord ? (
          <Button
            component={LocalOverlayLink}
            href={hrefWithQuery(sp, { deal_id: "new" })}
            variant="contained"
            color="primary"
            scroll={false}
            startIcon={<Iconify icon="mingcute:add-line" />}
          >
            {copy(pageContract, "action.record_sale.label")}
          </Button>
        ) : null}
        {/* Tagging animals to a sale WRITES HERD IDENTITY — it exits each animal as sold — so it
            is gated on the same capability as recording the deal, and additionally on there
            being a recorded sale to tag animals to. */}
        {canAllocateAnimals && deals.length > 0 ? (
          <Button
            component={LocalOverlayLink}
            href={hrefWithQuery(sp, { tag_sale: deals[0].deal_id })}
            variant="outlined"
            color="inherit"
            scroll={false}
            title={copy(pageContract, "action.tag_animals.hint")}
          >
            {copy(pageContract, "action.tag_animals.label")}
          </Button>
        ) : null}
          </>
        }
        tabs={
          // Template account view: Tabs above one card per tab (`?tab=`, soft navigation).
          <Box sx={{ mb: 2 }}>
            <AnimatedTabs
              ariaLabel={pageContract.title}
              value={tab}
              items={SALES_CONFIG_TABS.map((key) => ({
                value: key,
                label: tabLabel[key],
                href: hrefWithQuery(sp, { tab: key === "sales" ? null : key, offset: null, deal_id: null, cost_load: null, tag_sale: null }),
                count: key === "sales" && dealsResult.ok ? total : key === "loads" && loadwiseResult.ok ? loads.length : undefined,
              }))}
            />
          </Box>
        }
      />

      <Stack spacing={3}>
      {/* Write feedback. Without this a save simply closes the drawer, which is
          indistinguishable from the save being dropped. */}
      {actionStatus ? (
        <Alert severity={actionStatus === "success" ? "success" : "error"} sx={{ "& .MuiAlert-message": { minWidth: 0 } }}>
          {/* ONE block: a detail sentence beside the headline got squeezed and clipped at the card
              edge; stacked, the sentence gets the full width and wraps. */}
          {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          {actionDetail ? <Box sx={{ mt: 0.75 }}>{actionDetail}</Box> : null}
        </Alert>
      ) : null}

      {/* 1 — the sales themselves. The ledger is here as the way IN to a deal's payment and
          status edits, not as a board: clicking a row opens the same drawer the write uses.
          Template order-list card: CardHeader + whole-filter count Label, Scrollbar table under
          TableHeadCustom, soft status Label, template pagination. */}
      {/* The tab's section (guard: url-keyed-panel): a tab / pager click shows the clicked tab's
          skeleton at once; header, tabs and the always-mounted drawers stay on screen. */}
      <UrlSuspense searchParams={sp} watch={CONFIG_PANEL_WATCH} fallback={CONFIG_TAB_SKELETON[tab]} fallbackBy={{ param: "tab", shapes: { ...CONFIG_TAB_SKELETON, "": CONFIG_TAB_SKELETON.sales } }}>
      <Stack spacing={3}>
      {tab === "sales" ? (
      <Card>
        <CardHeader
          title={copy(pageContract, "section.sales_entry.title")}
          subheader={copy(pageContract, "section.sales_entry.subtitle")}
          action={
            // The WHOLE-FILTER total from the backend, not deals.length.
            <Label variant="soft" color={total ? "info" : "default"}>
              {num(total)} {copy(pageContract, "summary.count")}
            </Label>
          }
        />

        {!dealsResult.ok ? (
          <Box sx={{ p: 3 }}>
            <Alert severity="error">{salesErrorText(dealsResult.error, copy(pageContract, "error.load"))}</Alert>
          </Box>
        ) : null}

        {/* A failed read is not an empty ledger: under the error box, "No sales recorded yet.
            Record the first sale" told the desk the opposite of what happened. */}
        {!dealsResult.ok ? null : deals.length === 0 ? (
          <Box sx={{ p: 3 }}>
            <EmptyState sx={{ py: 10 }} title={copy(pageContract, "empty.deals.unset")} />
          </Box>
        ) : (
          <Box id="sales-config-deals" tabIndex={0} role="region" aria-label={copy(pageContract, "section.sales_entry.title")} sx={{ mt: 3 }}>
          <Scrollbar>
            <Table
              className="sales-deals-table"
              aria-label={copy(pageContract, "section.sales_entry.title")}
              sx={{
                minWidth: 960,
                // Single-line ledger cells: the global .celllink overflow-wrap:anywhere otherwise splits
                // "2026-08-11" and "CPT" mid-token.
                "&& td, && td .celllink": { whiteSpace: "nowrap", overflowWrap: "normal", wordBreak: "normal" },
                "&& td .celllink": { maxWidth: "none", minWidth: "max-content" },
                "&& th": { whiteSpace: "normal", overflowWrap: "normal", wordBreak: "normal" },
              }}
            >
              <TableHeadCustom headCells={dealHead} />
              <TableBody>
                {deals.map((deal) => {
                  const drawerHref = hrefWithQuery(sp, { deal_id: deal.deal_id });
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
                      {dealCell(<IdentityCell primary={deal.buyer_name} secondary={deal.product_type || undefined} />)}
                      {dealCell(deal.product_type)}
                      {dealCell(breedBeyondProduct(deal.product_type, deal.breed) ?? "")}
                      {dealCell(deal.animal_count == null ? none : num(deal.animal_count), "right")}
                      {dealCell(deal.total_weight_kg == null ? none : num(deal.total_weight_kg, 1), "right")}
                      {dealCell(inr(deal.sales_value), "right")}
                      {dealCell(<Label variant="soft" color={TONE_COLOR[dealStatusTone(deal.status)]}>{deal.status}</Label>)}
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </Scrollbar>
          </Box>
        )}

        {dealsResult.ok && deals.length > 0 ? (
          <ProcurementTableFooter
            denseLabel={copy(pageContract, "action.dense", "Dense")}
            rowsLabel={copy(pageContract, "pager.rows", "Rows")}
            page={pageNumber}
            pageCount={pageCount}
            rangeLabel={`${offset + 1}–${offset + deals.length} ${copy(pageContract, "pager.of")} ${num(total)}`}
            rowsValue={limit}
            rowsOptions={pageSizes.map((size) => ({ size, href: hrefWithQuery(sp, { limit: String(size), offset: null }) }))}
            prevHref={pagerHref(Math.max(0, (pageNumber - 2) * limit))}
            nextHref={pagerHref(pageNumber * limit)}
            prevLabel={copy(pageContract, "action.prev_page")}
            nextLabel={copy(pageContract, "action.next_page")}
            denseTargetId="sales-config-deals"
          />
        ) : null}
      </Card>
      ) : null}

      {/* 3 — Purchase and Born: a load's landed cost. Its own permission, so this section can be
          the only inert one on an otherwise live page. */}
      {tab === "loads" ? (
      <Card>
        <CardHeader title={copy(pageContract, "section.load_entry.title")} subheader={copy(pageContract, "section.load_entry.subtitle")} />

        {!loadwiseResult.ok || !canRecordCost ? (
          <Stack spacing={2} sx={{ px: 3, pt: 3 }}>
            {!loadwiseResult.ok ? <Alert severity="error">{salesErrorText(loadwiseResult.error, copy(pageContract, "error.load"))}</Alert> : null}
            {!canRecordCost ? <Alert severity="info">{copy(pageContract, "disabled.load_cost")}</Alert> : null}
          </Stack>
        ) : null}

        {!loadwiseResult.ok ? null : loads.length === 0 ? (
          <Box sx={{ p: 3 }}>
            <EmptyState sx={{ py: 10 }} title={copy(pageContract, "empty.loads")} />
          </Box>
        ) : (
          <Box tabIndex={0} role="region" aria-label={copy(pageContract, "section.load_entry.title")} sx={{ mt: 3 }}>
          <Scrollbar>
            <Table
              aria-label={copy(pageContract, "section.load_entry.title")}
              // Phone-width Load wise cells stay whole (main f864abb22): no wrapping inside a cell,
              // the table pans instead of shredding "L-12" or a farm name across lines.
              sx={{
                minWidth: 720,
                "& th, & td, & td .celllink": { whiteSpace: "nowrap", overflowWrap: "normal", wordBreak: "normal" },
                "& td .celllink": { maxWidth: "none", minWidth: "max-content" },
              }}
            >
              <TableHeadCustom headCells={loadHead} />
              <TableBody>
                {loads.map((load) => {
                  const costHref = hrefWithQuery(sp, { cost_load: load.load_id });
                  const costCell = (value: ReactNode, align?: "right") => (
                    <TableCell align={align}>
                      <LocalOverlayLink href={costHref} className="celllink" scroll={false}>
                        {value}
                      </LocalOverlayLink>
                    </TableCell>
                  );
                  return (
                    <TableRow key={load.load_id} hover>
                      {costCell(
                        <ListItemText
                          primary={load.load_ref || load.vendor_name}
                          secondary={load.purchase_date ? humanDate(load.purchase_date) : undefined}
                          slotProps={{ primary: { sx: { typography: "subtitle2" } }, secondary: { sx: { typography: "caption" } } }}
                          sx={{ m: 0 }}
                        />,
                      )}
                      {costCell(load.farm || none)}
                      {costCell(num(load.purchased), "right")}
                      {costCell(num(load.sold), "right")}
                      {costCell(num(load.remaining), "right")}
                      {/* Absent cost is "not recorded", never ₹0: a load bought with no recorded
                          cost and one that cost nothing are different facts. */}
                      {costCell(
                        load.purchase_value == null ? (
                          <Box component="span" sx={{ color: "text.disabled" }}>{copy(pageContract, "value.cost_missing")}</Box>
                        ) : (
                          inr(load.purchase_value)
                        ),
                        "right",
                      )}
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </Scrollbar>
          </Box>
        )}
      </Card>
      ) : null}

      {/* 5 — Market survey: what the morning calls ask. Its own permission (the Sales module's
          Configure level), so like the load-cost section it can be the inert one on a live page. */}
      {tab === "market" && marketConfigResult ? (
        <MarketConfigSection
          pageContract={pageContract}
          configResult={marketConfigResult}
          canConfigure={canConfigureMarket}
        />
      ) : null}
      {/* Who makes the calls (maintainer instruction 2026-09-19). */}
      {tab === "reporters" && marketReportersResult ? (
        <MarketReportersSection pageContract={pageContract} result={marketReportersResult} canConfigure={canConfigureMarket} />
      ) : null}

      {/* 6 — Farm valuation (maintainer instruction 2026-09-19): the decided figures behind Farm
          value and Load wise. Same gating shape as the market survey. */}
      {tab === "valuation" && valuationResult ? (
        <ValuationSection
          pageContract={pageContract}
          result={valuationResult}
          canEdit={canEditValuation}
          disabledReason={valuationControl?.disabled_reason ?? ""}
        />
      ) : null}
      </Stack>
      </UrlSuspense>

      {/* 4 — WHAT WE SELL (maintainer instruction 2026-09-23) and the record-sale drawer, as ONE
          client boundary: adding an item here puts it in that drawer's dropdown with no reload.
          The drawer is always mounted -- LocalOverlayLink changes the URL without an RSC request,
          so an overlay gated on a server-read search param would never appear. The items card
          shows only on its own tab. */}
      <SalesItemsAndRecordDrawer
        showProducts={tab === "items"}
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
      </Stack>
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

/** The params the tab sections read: the tab and the deals ledger's pager. */
const CONFIG_PANEL_WATCH = ["tab", "limit", "offset"] as const;
/** Each tab's section skeleton (the items tab renders inside the always-mounted drawer boundary). */
const CONFIG_TAB_SKELETON: Record<SalesConfigTab, ReactNode> = Object.fromEntries(
  SALES_CONFIG_TABS.map((key) => [
    key,
    key === "sales" || key === "loads" ? <PanelSkeleton table={8} /> : key === "items" ? null : <PanelSkeleton charts={1} table={6} />,
  ]),
) as Record<SalesConfigTab, ReactNode>;
