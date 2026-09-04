import type { ReactNode } from "react";
import { randomUUID } from "node:crypto";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Wheat } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getFeedPurchaseOptions, listFeedPurchases } from "@/lib/api/procurement-server";
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
import { paymentStatusChip } from "./feed-purchase-format";
import { inr, num, resolveFarm } from "./sales-format";
import { FeedPurchaseDrawer } from "./feed-purchase-drawer";

const PATHNAME = "/procurement/feed-purchases";
const DEFAULT_FARM = "all";
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

  const ledgerTable = table(pageContract, "feed-purchases");
  const pageSizes = ledgerTable.page_size_options.length > 0 ? ledgerTable.page_size_options : [DEFAULT_LIMIT];
  const limit = boundedInt(one(sp, "limit"), pageSizes[0], 1, 100);
  const offset = boundedInt(one(sp, "offset"), 0, 0, 10000);

  // Both reads in parallel: the options feed the entry drawer's selects and do not depend on the
  // page of rows. LocalOverlayLink opens the drawer without an RSC request, so its data must ride
  // with the page rather than be fetched on open.
  const [result, optionsResult] = await Promise.all([
    listFeedPurchases({ farm, limit, offset }),
    getFeedPurchaseOptions(),
  ]);

  if (firstAuthRequiredError(result, optionsResult)) redirect(INTERNAL_LOGIN_PATH);

  const purchases: FeedPurchase[] = result.ok ? result.data.purchases : [];
  // Whole-filter aggregates from the backend, never sums over the rendered page: these must keep
  // reading the ledger's real totals while the table shows 25 of them.
  const total = result.ok ? result.data.total : 0;
  const quantityKg = result.ok ? result.data.quantity_kg : 0;
  const spendRupees = result.ok ? result.data.spend_rupees : 0;
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const pageNumber = Math.min(pageCount, Math.floor(offset / limit) + 1);
  const optionsReady = optionsResult.ok;
  const options: FeedPurchaseOptions | null = optionsReady ? optionsResult.data : null;

  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const canRecord = controlEnabled(pageContract, "record_feed_purchase", false);
  const canOpenRecordDrawer = canRecord && optionsReady;
  const none = copy(pageContract, "value.none");
  const columns = tableLabels(pageContract, "feed-purchases");
  const listHref = hrefWithQuery(sp, { purchase_id: null });
  const isFiltered = farm !== DEFAULT_FARM;

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
        {canOpenRecordDrawer ? (
          <LocalOverlayLink
            href={hrefWithQuery(sp, { purchase_id: "new" })}
            className="btn primary"
            scroll={false}
            style={{ marginBottom: 4 }}
          >
            {copy(pageContract, "action.record_feed_purchase.label")}
          </LocalOverlayLink>
        ) : null}
      </div>

      {/* Write feedback. Without this the operator saves a load and the drawer simply closes, which
          is indistinguishable from the save being dropped. */}
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

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message || copy(pageContract, "error.load")}
        </div>
      ) : null}

      {!optionsResult.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{optionsResult.error.code ?? optionsResult.error.kind}</b>&nbsp;
          {optionsResult.error.message || copy(pageContract, "error.options")}
        </div>
      ) : null}

      {/* Farm scope toggle: server-rendered links, so the selection survives a reload and a shared
          URL. A farm switch drops the offset by construction (the patch clears it). */}
      <div className="chips" role="group" aria-label={copy(pageContract, "filter.farm")} style={{ marginBottom: 14 }}>
        <span className="muted small" style={{ marginRight: 6 }}>
          {copy(pageContract, "filter.farm")}
        </span>
        {farmOptions.map((option) => (
          <Link
            key={option.key}
            href={hrefWithQuery(sp, {
              farm: option.key === DEFAULT_FARM ? null : option.key,
              offset: null,
              purchase_id: null,
            })}
            scroll={false}
            className={option.key === farm ? "btn sm p" : "btn sm"}
            aria-current={option.key === farm ? "true" : undefined}
          >
            {option.label}
          </Link>
        ))}
      </div>

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
          <div className="empty">
            {isFiltered ? copy(pageContract, "empty.purchases") : copy(pageContract, "empty.purchases.unset")}
          </div>
        ) : (
          <div className="twrap">
            <table className="feed-purchases-table" aria-label={ledgerTable.title}>
              <thead>
                {/* Header labels come from the page contract IN ITS ORDER; the body cells below
                    are written in that same order. Both must move together if the contract's
                    column list changes. */}
                <tr>
                  {columns.map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {purchases.map((purchase) => {
                  const drawerHref = hrefWithQuery(sp, { purchase_id: purchase.feed_purchase_id });
                  const cellLink = (content: ReactNode) => (
                    <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                      {content}
                    </LocalOverlayLink>
                  );
                  return (
                    <tr key={purchase.feed_purchase_id}>
                      {/* The short cells never wrap: with nine columns the browser was breaking a
                          three-letter farm code across two lines, which reads as a different farm. */}
                      <td style={{ whiteSpace: "nowrap" }}>{cellLink(fmtDate(purchase.purchase_date))}</td>
                      <td style={{ whiteSpace: "nowrap" }}>{cellLink(purchase.farm)}</td>
                      <td>{cellLink(<b>{purchase.feed_item}</b>)}</td>
                      <td style={{ whiteSpace: "nowrap" }}>{cellLink(purchase.batch_no)}</td>
                      <td style={{ whiteSpace: "nowrap" }}>{cellLink(num(purchase.quantity_kg, 0))}</td>
                      <td style={{ whiteSpace: "nowrap" }}>
                        {cellLink(purchase.total_cost == null ? none : inr(purchase.total_cost))}
                      </td>
                      <td style={{ whiteSpace: "nowrap" }}>
                        {cellLink(purchase.per_kg_cost == null ? none : inr(purchase.per_kg_cost, 2))}
                      </td>
                      <td>{cellLink(purchase.vendor || none)}</td>
                      <td>
                        {cellLink(
                          // Tone AND label are backend-owned option metadata, not a comparison
                          // against a hardcoded payment word.
                          <Tag tone={paymentStatusChip(pageContract, purchase.payment_status, none).tone}>
                            {paymentStatusChip(pageContract, purchase.payment_status, none).label}
                          </Tag>,
                        )}
                      </td>
                      <td style={{ whiteSpace: "nowrap" }}>
                        {/* BACKEND-derived money still owed (total minus instalments, floored at
                            zero); "—" while the landed cost is unknown. The page never subtracts
                            anything itself. */}
                        {cellLink(purchase.payment_balance == null ? none : inr(purchase.payment_balance))}
                      </td>
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
                href={hrefWithQuery(sp, { offset: String(Math.max(0, offset - limit)) })}
                scroll={false}
                className="btn"
              >
                {copy(pageContract, "action.prev_page")}
              </Link>
            ) : (
              <span className="btn" aria-disabled="true">
                {copy(pageContract, "action.prev_page")}
              </span>
            )}
            {pageNumber < pageCount ? (
              <Link href={hrefWithQuery(sp, { offset: String(offset + limit) })} scroll={false} className="btn">
                {copy(pageContract, "action.next_page")}
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
      <FeedPurchaseDrawer
        purchases={purchases}
        options={options}
        recordIdempotencyKey={randomUUID()}
        paymentIdempotencyKey={randomUUID()}
        pageContract={pageContract}
        listHref={listHref}
        canRecord={canOpenRecordDrawer}
      />
    </div>
  );
}
