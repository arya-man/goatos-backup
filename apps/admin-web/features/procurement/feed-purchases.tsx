import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import type { ReactNode } from "react";
import { randomUUID } from "node:crypto";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Wheat } from "lucide-react";
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
import { deliveryStatusChip, paymentStatusChip } from "./feed-purchase-format";
import { inr, num, resolveFarm } from "./sales-format";
import { IdentityCell } from "@/components/data-table";
import { ProcurementTableFooter } from "./table-footer-links";
import { ProcurementTableToolbar } from "./table-toolbar";
import { FeedPurchaseDrawer } from "./feed-purchase-drawer";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import Alert from "@mui/material/Alert";

const PATHNAME = "/procurement/feed-purchases";
const DEFAULT_FARM = "all";
const DEFAULT_DELIVERY = "all";
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

  return (
    <div className="screen on">
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
        actions={
          canOpenRecordDrawer ? (
            <LocalOverlayLink href={hrefWithQuery(sp, { purchase_id: "new" })} className="btn primary" scroll={false}>
              {copy(pageContract, "action.record_feed_purchase.label")}
            </LocalOverlayLink>
          ) : null
        }
      />

      {/* Write feedback. Without this the operator saves a load and the drawer simply closes, which
          is indistinguishable from the save being dropped. */}
      {actionStatus ? (
        actionStatus === "success" ? (
          <div className="note" style={{ marginBottom: 14 }}>
            {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        ) : (
          <Alert severity="error" style={{ marginBottom: 14 }}>
            {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </Alert>
        )
      ) : null}

      {!result.ok ? (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          {result.error.message || copy(pageContract, "error.load")}
        </Alert>
      ) : null}

      {!optionsResult.ok ? (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          {optionsResult.error.message || copy(pageContract, "error.options")}
        </Alert>
      ) : null}

      {/* Farm and delivery scope: server-built links behind outlined selects, so the selection still
          survives a reload and a shared URL. A switch drops the offset by construction. */}
      <ProcurementTableToolbar
        clearLabel={copy(pageContract, "action.clear_all", "Clear all")}
        columnsLabel={copy(pageContract, "action.columns", "Columns")}
        exportLabel={copy(pageContract, "action.export", "Export")}
        moreLabel={copy(pageContract, "action.more", "More")}
        ariaLabel={ledgerTable.title}
        tableId="feed-purchases-ledger"
        exportName="feed-purchases"
        chips={[
          ...(farm !== DEFAULT_FARM
            ? [{
                id: "farm",
                label: `${copy(pageContract, "filter.farm", "Farm")}: ${farmOptions.find((option) => option.key === farm)?.label ?? farm}`,
                href: hrefWithQuery(sp, { farm: null, offset: null, purchase_id: null }),
              }]
            : []),
          ...(delivery !== DEFAULT_DELIVERY
            ? [{
                id: "delivery",
                label: `${copy(pageContract, "filter.delivery", "Delivery")}: ${deliveryOptions.find((option) => option.key === delivery)?.label ?? delivery}`,
                href: hrefWithQuery(sp, { delivery: null, offset: null, purchase_id: null }),
              }]
            : []),
        ]}
        clearHref={isFiltered ? hrefWithQuery(sp, { farm: null, delivery: null, offset: null, purchase_id: null }) : undefined}
      >
        <LinkSelect
          label={copy(pageContract, "filter.farm")}
          value={farm}
          minWidth={200}
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
        <LinkSelect
          label={copy(pageContract, "filter.delivery")}
          value={delivery}
          minWidth={200}
          options={[
            {
              value: DEFAULT_DELIVERY,
              label: copy(pageContract, "filter.delivery.all"),
              href: hrefWithQuery(sp, { delivery: null, offset: null, purchase_id: null }),
            },
            ...deliveryOptions.map((option) => ({
              value: option.key,
              label: option.label,
              href: hrefWithQuery(sp, { delivery: option.key, offset: null, purchase_id: null }),
            })),
          ]}
        />
      </ProcurementTableToolbar>

      <section className="card">
        <div className="hd">
          <Wheat className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{ledgerTable.title}</h3>
          <Tag tone={total ? "info" : "mut"}>
            {/* Both words are backend-owned; the renderer only picks which one the number takes,
                so the badge never reads "1 loads". */}
            {total} {copy(pageContract, total === 1 ? "summary.count.one" : "summary.count")}
          </Tag>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">
            {copy(pageContract, "summary.quantity")} {num(quantityKg, 0)} kg · {copy(pageContract, "summary.spend")}{" "}
            {inr(spendRupees)}
          </span>
        </div>

        {purchases.length === 0 ? (
          <EmptyState title={isFiltered ? copy(pageContract, "empty.purchases") : copy(pageContract, "empty.purchases.unset")} />
        ) : (
          <div id="feed-purchases-ledger" className="twrap" tabIndex={0} role="region" aria-label={ledgerTable.title}>
            <Table className="feed-purchases-table" aria-label={ledgerTable.title}>
              <TableHead>
                {/* Header labels come from the page contract IN ITS ORDER; the body cells below
                    are written in that same order. Both must move together if the contract's
                    column list changes. */}
                <TableRow>
                  {columns.map((label) => (
                    <TableCell component="th" key={label}>{label}</TableCell>
                  ))}
                </TableRow>
              </TableHead>
              <TableBody>
                {purchases.map((purchase) => {
                  const drawerHref = hrefWithQuery(sp, { purchase_id: purchase.feed_purchase_id });
                  const cellLink = (content: ReactNode) => (
                    <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                      {content}
                    </LocalOverlayLink>
                  );
                  return (
                    <TableRow key={purchase.feed_purchase_id}>
                      {/* The short cells never wrap: with nine columns the browser was breaking a
                          three-letter farm code across two lines, which reads as a different farm. */}
                      <TableCell style={{ whiteSpace: "nowrap" }}>{cellLink(fmtDate(purchase.purchase_date))}</TableCell>
                      <TableCell style={{ whiteSpace: "nowrap" }}>{cellLink(purchase.farm)}</TableCell>
                      <TableCell>{cellLink(<IdentityCell primary={purchase.feed_item} secondary={purchase.vendor || undefined} />)}</TableCell>
                      <TableCell style={{ whiteSpace: "nowrap" }}>{cellLink(purchase.batch_no)}</TableCell>
                      <TableCell style={{ whiteSpace: "nowrap" }}>{cellLink(num(purchase.quantity_kg, 0))}</TableCell>
                      <TableCell style={{ whiteSpace: "nowrap" }}>
                        {cellLink(
                          // Tone AND label are backend option metadata. A reached load also shows
                          // the day it came in, because "Reached" alone does not say when stock
                          // started.
                          <>
                            <Tag tone={deliveryStatusChip(pageContract, purchase.delivery_status, none).tone}>
                              {deliveryStatusChip(pageContract, purchase.delivery_status, none).label}
                            </Tag>
                            {purchase.reached_on ? (
                              <span className="muted small" style={{ marginLeft: 6 }}>
                                {fmtDate(purchase.reached_on)}
                              </span>
                            ) : null}
                          </>,
                        )}
                      </TableCell>
                      <TableCell style={{ whiteSpace: "nowrap" }}>
                        {cellLink(purchase.total_cost == null ? none : inr(purchase.total_cost))}
                      </TableCell>
                      <TableCell style={{ whiteSpace: "nowrap" }}>
                        {cellLink(purchase.per_kg_cost == null ? none : inr(purchase.per_kg_cost, 2))}
                      </TableCell>
                      <TableCell>{cellLink(purchase.vendor || none)}</TableCell>
                      <TableCell>
                        {cellLink(
                          // Tone AND label are backend-owned option metadata, not a comparison
                          // against a hardcoded payment word.
                          <Tag tone={paymentStatusChip(pageContract, purchase.payment_status, none).tone}>
                            {paymentStatusChip(pageContract, purchase.payment_status, none).label}
                          </Tag>,
                        )}
                      </TableCell>
                      <TableCell style={{ whiteSpace: "nowrap" }}>
                        {/* BACKEND-derived money still owed (total minus instalments, floored at
                            zero); "—" while the landed cost is unknown. The page never subtracts
                            anything itself. */}
                        {cellLink(purchase.payment_balance == null ? none : inr(purchase.payment_balance))}
                      </TableCell>
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
            rangeLabel={`${offset + 1}\u2013${offset + purchases.length} ${copy(pageContract, "pager.of")} ${total}`}
            rowsValue={limit}
            rowsOptions={pageSizes.map((size) => ({ size, href: hrefWithQuery(sp, { limit: String(size), offset: null }) }))}
            prevHref={hrefWithQuery(sp, { offset: String(Math.max(0, (pageNumber - 2) * limit)) })}
            nextHref={hrefWithQuery(sp, { offset: String(pageNumber * limit) })}
            prevLabel={copy(pageContract, "action.prev_page")}
            nextLabel={copy(pageContract, "action.next_page")}
            denseTargetId="feed-purchases-ledger"
          />
        ) : null}
      </section>

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
