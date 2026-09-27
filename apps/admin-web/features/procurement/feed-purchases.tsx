import Box from "@mui/material/Box";
import { orderToolbarFilterSx } from "@/components/app/order-toolbar-filter";
import { DividedStack } from "@/components/app/divided-stack";
import { UrlSuspense } from "@/components/app/url-suspense";
import { StatStripSkeleton, TableSkeleton } from "@/components/app/skeletons";
import Card from "@mui/material/Card";
import Link from "@mui/material/Link";
import Stack from "@mui/material/Stack";
import Table from "@mui/material/Table";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import TableRow from "@mui/material/TableRow";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { randomUUID } from "node:crypto";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { LinkSelect } from "@/components/app/link-select";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getFeedPurchaseForm, getFeedPurchaseOptions, listFeedPurchases } from "@/lib/api/procurement-server";
import type { FeedPurchase, FeedPurchaseOptions } from "@/lib/api/procurement";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { Tag } from "@/components/ui-primitives";
import {
  actionFeedbackCopy,
  controlEnabled,
  copy,
  optionGroup,
  table,
  tableLabels,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { UrlTabs } from "@/components/app/url-tabs";
import { TableHeadCustom } from "@/components/app/table/table-head-custom";
import { InvoiceAnalytic } from "@/components/app/sections/invoice/invoice-analytic";
import { deliveryStatusChip, paymentStatusChip } from "./feed-purchase-format";
import { inr, num, resolveFarm } from "./sales-format";
import { ProcurementTableFooter } from "./table-footer-links";
import { ProcurementFiltersResult, ProcurementListToolbar, type ToolbarChip } from "./table-toolbar";
import { FeedPurchaseDrawer } from "./feed-purchase-drawer";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import { DEFAULT_LIMIT } from "./feed-purchases-layout";

const PATHNAME = "/procurement/feed-purchases";
const DEFAULT_FARM = "all";
const DEFAULT_DELIVERY = "all";

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
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

/**
 * Feed Purchases — the BUYING side of the feed chain.
 *
 * One server-paged ledger of loads bought for CBE and CPT, and one entry drawer behind the
 * backend-declared `record_feed_purchase` capability. There is deliberately NO role-string check in
 * this component: the difference between a read-only Feed Director and the procurement desk arrives
 * only through that control and the route's own permission.
 *
 * These rows are what the stock and days-left cards on /feed/analytics are counted from, which is
 * why the header says so — an operator who records a load here should know where its effect shows.
 */
export async function FeedPurchasesPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;

  // Farm scope: validated against the SERVED option keys, never trusted raw.
  const farmOptions = optionGroup(pageContract, "feed_purchase_farms");
  const farm = resolveFarm(
    one(sp, "farm"),
    farmOptions.map((option) => option.key),
    DEFAULT_FARM,
  );

  // Delivery scope (on the road / reached): validated against the SERVED option keys too.
  const deliveryOptions = optionGroup(pageContract, "feed_purchase_delivery_statuses");
  const delivery = resolveFarm(
    one(sp, "delivery"),
    deliveryOptions.map((option) => option.key),
    DEFAULT_DELIVERY,
  );

  const ledgerTable = table(pageContract, "feed-purchases");
  const pageSizes = ledgerTable.page_size_options.length > 0 ? ledgerTable.page_size_options : [DEFAULT_LIMIT];
  const limit = boundedInt(one(sp, "limit"), pageSizes[0], 1, 100);
  const offset = boundedInt(one(sp, "offset"), 0, 0, 10000);

  // Both reads in parallel: the options feed the entry drawer's selects and do not depend on the
  // page of rows. LocalOverlayLink opens the drawer without an RSC request, so its data must ride
  // with the page rather than be fetched on open.
  const [result, optionsResult, formResult] = await Promise.all([
    listFeedPurchases({ farm, delivery, limit, offset }),
    getFeedPurchaseOptions(),
    // THE FEED PURCHASE FORM IS AUTHORED (2026-09-20): whatever the farm added beyond the ledger's
    // own columns. Read beside the options and for the same reason -- the drawer opens without an
    // RSC request, so its data rides with the page. A failed read leaves the drawer on its typed
    // fields rather than blocking the ledger.
    getFeedPurchaseForm(),
  ]);

  if (firstAuthRequiredError(result, optionsResult)) redirect(INTERNAL_LOGIN_PATH);

  const purchases: FeedPurchase[] = result.ok ? listOrEmpty(result.data.purchases) : [];
  // Whole-filter aggregates from the backend, never sums over the rendered page: these must keep
  // reading the ledger's real totals while the table shows 25 of them.
  const total = result.ok ? result.data.total : 0;
  const quantityKg = result.ok ? result.data.quantity_kg : 0;
  const spendRupees = result.ok ? result.data.spend_rupees : 0;
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const pageNumber = Math.min(pageCount, Math.floor(offset / limit) + 1);
  const optionsReady = optionsResult.ok;
  const options: FeedPurchaseOptions | null = optionsReady ? optionsResult.data : null;
  const purchaseForm = formResult.ok ? formResult.data : null;

  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const canRecord = controlEnabled(pageContract, "record_feed_purchase", false);
  const canOpenRecordDrawer = canRecord && optionsReady;
  const none = copy(pageContract, "value.none");
  const columns = tableLabels(pageContract, "feed-purchases");
  const listHref = hrefWithQuery(sp, { purchase_id: null });
  const isFiltered = farm !== DEFAULT_FARM || delivery !== DEFAULT_DELIVERY;

  const farmLabel = farmOptions.find((option) => option.key === farm)?.label ?? farm;
  const deliveryLabel = deliveryOptions.find((option) => option.key === delivery)?.label ?? delivery;
  const chips: ToolbarChip[] = [
    ...(farm !== DEFAULT_FARM
      ? [{ id: "farm", group: copy(pageContract, "filter.farm", "Farm"), label: farmLabel, href: hrefWithQuery(sp, { farm: null, offset: null, purchase_id: null }) }]
      : []),
  ];
  const countWord = copy(pageContract, total === 1 ? "summary.count.one" : "summary.count");

  return (
    <div className="screen on">
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
        actions={
          canOpenRecordDrawer ? (
            <Button
              component={LocalOverlayLink}
              href={hrefWithQuery(sp, { purchase_id: "new" })}
              scroll={false}
              variant="contained"
              color="primary"
              startIcon={<Iconify icon="mingcute:add-line" />}
            >
              {copy(pageContract, "action.record_feed_purchase.label")}
            </Button>
          ) : null
        }
      />

      {/* Write feedback. Without this the operator saves a load and the drawer simply closes, which
          is indistinguishable from the save being dropped. */}
      {actionStatus ? (
        <Alert severity={actionStatus === "success" ? "success" : "error"} sx={{ mb: 3 }}>
          {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
        </Alert>
      ) : null}

      {!result.ok ? (
        <Alert severity="error" sx={{ mb: 3 }}>
          {result.error.message || copy(pageContract, "error.load")}
        </Alert>
      ) : null}

      {!optionsResult.ok ? (
        <Alert severity="error" sx={{ mb: 3 }}>
          {optionsResult.error.message || copy(pageContract, "error.options")}
        </Alert>
      ) : null}

      {/* Template invoice list: the InvoiceAnalytic strip on its own card (whole-filter aggregates
          from the backend, never sums over the rendered page), then the list card. */}
      {/* Whole-filter aggregates and the ledger rows (guard: url-keyed-panel): a delivery tab / farm /
          page change swaps them to their skeleton at once; tabs and toolbar stay on screen. */}
      <UrlSuspense searchParams={sp} watch={AGGREGATE_WATCH} fallback={<Box sx={{ mb: { xs: 3, md: 5 } }}><StatStripSkeleton count={2} /></Box>}>
      <Card sx={{ mb: { xs: 3, md: 5 } }}>
        <Scrollbar sx={{ minHeight: 108 }}>
          <DividedStack
            dividerOrientation="vertical"
            direction="row"
            sx={{ py: 2 }}
          >
            <InvoiceAnalytic
              title={ledgerTable.title}
              total={total}
              caption={`${total} ${countWord}`}
              value={`${copy(pageContract, "summary.spend")} ${inr(spendRupees)}`}
              percent={100}
              icon="solar:bill-list-bold-duotone"
              color="info.main"
            />
            <InvoiceAnalytic
              title={copy(pageContract, "summary.quantity")}
              total={quantityKg}
              caption={farm === DEFAULT_FARM ? copy(pageContract, "filter.farm.all", farmLabel) : farmLabel}
              value={`${num(quantityKg, 0)} kg`}
              percent={100}
              icon="solar:cart-3-bold"
              color="success.main"
            />
          </DividedStack>
        </Scrollbar>
      </Card>
      </UrlSuspense>

      <Card>
        {/* Delivery scope as the template's status Tabs (on the road / reached), the same
            `?delivery=` param the select used to write. Only the shown tab has a count: the
            backend counts the whole filter, never every status at once. */}
        <UrlTabs
          ariaLabel={copy(pageContract, "filter.delivery")}
          value={delivery}
          items={[
            { value: DEFAULT_DELIVERY, label: copy(pageContract, "filter.delivery.all") },
            ...deliveryOptions.map((option) => ({ value: option.key, label: option.label })),
          ].map((tab) => ({
            ...tab,
            href: hrefWithQuery(sp, { delivery: tab.value === DEFAULT_DELIVERY ? null : tab.value, offset: null, purchase_id: null }),
            count: tab.value === delivery ? total : undefined,
          }))}
        />

        {/* Farm scope: a server-built link select, so the selection survives a reload and a
            shared URL. A switch drops the offset by construction. */}
        <ProcurementListToolbar
          tableId="feed-purchases-ledger"
          exportName="feed-purchases"
          columnsLabel={copy(pageContract, "action.columns", "Columns")}
          exportLabel={copy(pageContract, "action.export", "Export")}
          moreLabel={copy(pageContract, "action.more", "More")}
          filters={
            <Box sx={[orderToolbarFilterSx, { "& .MuiTextField-root": { width: 1 } }]}>
              <LinkSelect
                label={copy(pageContract, "filter.farm")}
                value={farm}
                options={farmOptions.map((option) => ({
                  value: option.key,
                  label: option.label,
                  href: hrefWithQuery(sp, {
                    farm: option.key === DEFAULT_FARM ? null : option.key,
                    offset: null,
                    purchase_id: null,
                  }),
                }))}
              />
            </Box>
          }
        />

        <ProcurementFiltersResult
          chips={chips}
          totalResults={total}
          clearHref={hrefWithQuery(sp, { farm: null, offset: null, purchase_id: null })}
        />

        <UrlSuspense searchParams={sp} watch={LEDGER_WATCH} fallback={<TableSkeleton bare header={false} columns={Math.max(columns.length, 1)} rows={limit} />}>
        <Box id="feed-purchases-ledger" tabIndex={0} role="region" aria-label={ledgerTable.title}>
          <Scrollbar>
            <Table sx={{ minWidth: 1100 }} aria-label={ledgerTable.title}>
              {/* Header labels come from the page contract IN ITS ORDER; the body cells below are
                  written in that same order. Both must move together if the contract's column list
                  changes. */}
              <TableHeadCustom headCells={columns.map((label) => ({ id: label, label, sortable: false }))} />
              <TableBody>
                {purchases.map((purchase) => {
                  const drawerHref = hrefWithQuery(sp, { purchase_id: purchase.feed_purchase_id });
                  const delivered = deliveryStatusChip(pageContract, purchase.delivery_status, none);
                  const paid = paymentStatusChip(pageContract, purchase.payment_status, none);
                  return (
                    <TableRow key={purchase.feed_purchase_id} hover>
                      {/* Template invoice row: the lead cell is a two-line date, the item cell a
                          link + caption; short cells never wrap (a three-letter farm code broken
                          across two lines reads as a different farm). */}
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{fmtDate(purchase.purchase_date)}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{purchase.farm}</TableCell>
                      <TableCell>
                        <Stack sx={{ typography: "body2", alignItems: "flex-start", minWidth: 0 }}>
                          <Link component={LocalOverlayLink} href={drawerHref} scroll={false} color="inherit" sx={{ cursor: "pointer" }}>
                            {purchase.feed_item}
                          </Link>
                          {purchase.vendor ? (
                            <Box component="span" sx={{ color: "text.disabled" }}>
                              {purchase.vendor}
                            </Box>
                          ) : null}
                        </Stack>
                      </TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{purchase.batch_no}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{num(purchase.quantity_kg, 0)}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>
                        {/* Colour AND label are backend option metadata. A reached load also shows
                            the day it came in: "Reached" alone does not say when stock started. */}
                        <Stack spacing={0.5} sx={{ alignItems: "flex-start" }}>
                          <Tag tone={delivered.tone}>{delivered.label}</Tag>
                          {purchase.reached_on ? (
                            <Box component="span" sx={{ color: "text.disabled", typography: "caption" }}>
                              {fmtDate(purchase.reached_on)}
                            </Box>
                          ) : null}
                        </Stack>
                      </TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{purchase.total_cost == null ? none : inr(purchase.total_cost)}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{purchase.per_kg_cost == null ? none : inr(purchase.per_kg_cost, 2)}</TableCell>
                      <TableCell>{purchase.vendor || none}</TableCell>
                      <TableCell>
                        {/* Colour AND label are backend-owned option metadata, not a comparison
                            against a hardcoded payment word. */}
                        <Tag tone={paid.tone}>{paid.label}</Tag>
                      </TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>
                        {/* BACKEND-derived money still owed (total minus instalments, floored at
                            zero); "—" while the landed cost is unknown. The page never subtracts
                            anything itself. */}
                        {purchase.payment_balance == null ? none : inr(purchase.payment_balance)}
                      </TableCell>
                    </TableRow>
                  );
                })}
                {purchases.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={Math.max(columns.length, 1)}>
                      <EmptyState sx={{ py: 10 }} title={isFiltered ? copy(pageContract, "empty.purchases") : copy(pageContract, "empty.purchases.unset")} />
                    </TableCell>
                  </TableRow>
                ) : null}
              </TableBody>
            </Table>
          </Scrollbar>
        </Box>

        <ProcurementTableFooter
          denseLabel={copy(pageContract, "action.dense", "Dense")}
          rowsLabel={copy(pageContract, "pager.rows", "Rows")}
          page={pageNumber}
          pageCount={pageCount}
          rangeLabel={total ? `${offset + 1}–${offset + purchases.length} ${copy(pageContract, "pager.of")} ${total}` : `0 ${countWord}`}
          rowsValue={limit}
          rowsOptions={pageSizes.map((size) => ({ size, href: hrefWithQuery(sp, { limit: String(size), offset: null }) }))}
          prevHref={hrefWithQuery(sp, { offset: String(Math.max(0, (pageNumber - 2) * limit)) })}
          nextHref={hrefWithQuery(sp, { offset: String(pageNumber * limit) })}
          prevLabel={copy(pageContract, "action.prev_page")}
          nextLabel={copy(pageContract, "action.next_page")}
          denseTargetId="feed-purchases-ledger"
        />
        </UrlSuspense>
      </Card>

      {/* Always mounted: LocalOverlayLink changes the URL without an RSC request, so an overlay
          gated on a server-read search param would never appear. */}
      <FeedPurchaseDrawer
        purchases={purchases}
        options={options}
        purchaseForm={purchaseForm}
        recordIdempotencyKey={randomUUID()}
        paymentIdempotencyKey={randomUUID()}
        pageContract={pageContract}
        listHref={listHref}
        canRecord={canOpenRecordDrawer}
      />
    </div>
  );
}

/** The params the ledger's whole-filter aggregates take. */
const AGGREGATE_WATCH = ["farm", "delivery"] as const;
/** The params the ledger page takes. */
const LEDGER_WATCH = ["farm", "delivery", "limit", "offset"] as const;
