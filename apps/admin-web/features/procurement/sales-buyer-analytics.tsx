import { redirect } from "next/navigation";

import Link from "@/components/no-prefetch-link";
import { Tag } from "@/components/ui-primitives";
import {
  controlEnabled,
  copy,
  optionGroup,
  tablePageSizes,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getBuyerAnalytics } from "@/lib/api/procurement-server";
import type { BuyerAnalytics, BuyerAnalyticsRow } from "@/lib/api/procurement";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { humanDate, inr, num, resolveFarm, salesHref } from "./sales-format";
import { SALES_DEFAULT_FARM, SalesFarmToggle, SalesPageHeader } from "./sales-chrome";

const PAGE_PATH = "/sales/buyer-analytics";
/** Only used when an older backend contract carries no buyers table; the contract page size wins. */
const FALLBACK_LIMIT = 25;
const MAX_OFFSET = 10000;

/** Fills a backend copy template's `{name}` slots; the sentence itself stays backend-owned. */
function fill(template: string, values: Record<string, string>): string {
  return template.replace(/\{(\w+)\}/g, (match, key: string) => values[key] ?? match);
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
}: {
  analytics: BuyerAnalytics;
  pageContract: AdminUiPageContract;
  offset: number;
  limit: number;
  pageHref: (offset: number) => string;
}) {
  const summary = analytics.summary;
  const none = copy(pageContract, "value.none");
  const phoneControl = pageContract.controls.find((item) => item.id === "buyer_phone_column");
  const showPhones = analytics.phones_visible && controlEnabled(pageContract, "buyer_phone_column", false);
  const phoneHiddenReason = phoneControl?.disabled_reason || copy(pageContract, "hint.phone_hidden");
  const pageNumber = Math.floor(offset / limit) + 1;
  const pageCount = Math.max(1, Math.ceil(analytics.total_buyers / limit));
  const repeatPct = summary.buyers > 0 ? (summary.repeat_buyers / summary.buyers) * 100 : 0;

  // Two short lines under the chip rather than one long sentence, so the column stays narrow
  // enough for the sale dates and the balance to sit on screen beside it.
  const cadence = (row: BuyerAnalyticsRow): string[] => {
    if (!row.repeat) return [];
    const parts = [fill(copy(pageContract, "value.repeat_purchases"), { count: num(row.repeat_purchases) })];
    if (row.avg_days_between != null) {
      parts.push(fill(copy(pageContract, "value.every_days"), { days: num(row.avg_days_between) }));
    }
    return parts;
  };
  const recency = (row: BuyerAnalyticsRow): string => {
    if (row.days_since_last == null) return "";
    if (row.days_since_last === 0) return copy(pageContract, "value.today");
    return fill(copy(pageContract, "value.days_ago"), { days: num(row.days_since_last) });
  };

  return (
    <>
      <section className="grid g4 kpi-row sales-kpi-row" aria-label={copy(pageContract, "section.headline.aria")}>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.buyers")}</div>
          <div className="val">{num(summary.buyers)}</div>
          <div className="dl">
            {num(summary.not_in_register)} {copy(pageContract, "kpi.buyers.detail")}
          </div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.repeat_buyers")}</div>
          <div className="val">{num(summary.repeat_buyers)}</div>
          <div className="dl">
            {num(repeatPct, 0)}% · {copy(pageContract, "kpi.repeat_buyers.detail")}
          </div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.repeat_revenue")}</div>
          <div className="val">{inr(summary.repeat_revenue)}</div>
          <div className="dl">
            {num(summary.repeat_revenue_pct, 0)}% {copy(pageContract, "kpi.repeat_revenue.detail")} ·{" "}
            {num(summary.purchases)} {copy(pageContract, "kpi.purchases.detail")}
          </div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.outstanding")}</div>
          <div className="val">{inr(summary.outstanding)}</div>
          <div className="dl">{copy(pageContract, "kpi.outstanding.detail")}</div>
        </div>
      </section>

      <section className="card" aria-label={copy(pageContract, "section.buyers.title")}>
        <div className="hd">
          <h3>{copy(pageContract, "section.buyers.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{copy(pageContract, "section.buyers.subtitle")}</span>
        </div>
        {!showPhones ? (
          <p className="muted small" style={{ marginTop: 0 }}>
            {phoneHiddenReason}
          </p>
        ) : null}
        {analytics.total_buyers === 0 ? (
          <div className="empty">{copy(pageContract, "empty.buyers")}</div>
        ) : (
          <div className="twrap" tabIndex={0} role="region" aria-label={copy(pageContract, "section.buyers.title")}>
            <table aria-label={copy(pageContract, "section.buyers.title")}>
              <thead>
                <tr>
                  {/* Category and place ride under the buyer's name (one cell, two lines) so the
                      row's eleven facts fit a desktop width without the table's own scroll
                      hiding the sale dates and the balance behind it. */}
                  <th>
                    {copy(pageContract, "column.buyer_name")}
                    <span className="muted small">
                      {" "}
                      · {copy(pageContract, "column.category")} · {copy(pageContract, "column.place")}
                    </span>
                  </th>
                  {showPhones ? <th>{copy(pageContract, "column.phone_number")}</th> : null}
                  <th className="num">{copy(pageContract, "column.purchases")}</th>
                  <th className="num">{copy(pageContract, "column.animals")}</th>
                  <th className="num">{copy(pageContract, "column.revenue")}</th>
                  <th>{copy(pageContract, "column.repeat")}</th>
                  <th>{copy(pageContract, "column.first_sale_date")}</th>
                  <th>{copy(pageContract, "column.last_sale_date")}</th>
                  <th className="num">{copy(pageContract, "column.outstanding")}</th>
                </tr>
              </thead>
              <tbody>
                {analytics.buyers.map((row) => (
                  <tr key={row.buyer_key}>
                    <td>
                      <b>{row.buyer_name}</b>
                      <div className="muted small">
                        {[row.category, row.place].filter(Boolean).join(" · ") || none}
                      </div>
                      {!row.in_register ? (
                        <div className="chips" style={{ marginTop: 4 }}>
                          <Tag tone="warn" title={copy(pageContract, "hint.not_in_register")}>
                            {copy(pageContract, "chip.not_in_register")}
                          </Tag>
                        </div>
                      ) : null}
                    </td>
                    {showPhones ? <td>{row.phone_number || none}</td> : null}
                    <td className="num">
                      <b>{num(row.purchases)}</b>
                      {row.product_types.length > 0 ? (
                        <div className="muted small">{row.product_types.join(" · ")}</div>
                      ) : null}
                    </td>
                    <td className="num">{num(row.animals)}</td>
                    <td className="num">
                      {inr(row.revenue)}
                      <div className="muted small">{num(row.share_pct, 1)}%</div>
                    </td>
                    <td>
                      <Tag tone={row.repeat ? "ok" : "mut"}>
                        {copy(pageContract, row.repeat ? "chip.repeat" : "chip.one_time")}
                      </Tag>
                      {cadence(row).map((line) => (
                        <div className="muted small" key={line}>
                          {line}
                        </div>
                      ))}
                    </td>
                    <td>{humanDate(row.first_sale_date)}</td>
                    <td>
                      {humanDate(row.last_sale_date)}
                      {recency(row) ? <div className="muted small">{recency(row)}</div> : null}
                    </td>
                    <td className="num">
                      {row.outstanding > 0 ? inr(row.outstanding) : <span className="muted">{copy(pageContract, "value.settled")}</span>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {pageCount > 1 ? (
          <div className="pager2">
            <span className="muted">
              {copy(pageContract, "pager.page")} {pageNumber} {copy(pageContract, "pager.of")} {pageCount} ·{" "}
              {num(analytics.total_buyers)} {copy(pageContract, "summary.buyers")}
            </span>
            {offset > 0 ? (
              <Link href={pageHref(Math.max(0, offset - limit))} scroll={false} className="btn">
                {copy(pageContract, "action.prev_page")}
              </Link>
            ) : (
              <span className="btn" aria-disabled="true">
                {copy(pageContract, "action.prev_page")}
              </span>
            )}
            {offset + limit < analytics.total_buyers ? (
              <Link href={pageHref(offset + limit)} scroll={false} className="btn">
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

  // Farm scope: validated against the SERVED option keys, never trusted raw.
  const farmOptions = optionGroup(pageContract, "sales_farms");
  const farm = resolveFarm(
    one(sp, "farm"),
    farmOptions.map((option) => option.key),
    SALES_DEFAULT_FARM,
  );
  // Page size is the contract's; the offset is bounded to the backend's own ceiling.
  const pageSizes = tablePageSizes(pageContract, "sales-buyer-analytics");
  const defaultLimit = pageSizes[0] ?? FALLBACK_LIMIT;
  const limit = resolveLimit(one(sp, "limit"), pageSizes, defaultLimit);
  const offset = boundedInt(one(sp, "offset"), 0, 0, MAX_OFFSET);

  const result = await getBuyerAnalytics({
    farm: farm === SALES_DEFAULT_FARM ? undefined : farm,
    limit,
    offset,
  });
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);

  const pageHref = (nextOffset: number) =>
    salesHref({ farm, limit, offset: nextOffset }, { farm: SALES_DEFAULT_FARM, limit: defaultLimit }, PAGE_PATH);

  return (
    <div className="screen on">
      <SalesPageHeader pageContract={pageContract} />

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;
          {result.error.message || copy(pageContract, "error.load")}
        </div>
      ) : null}

      <SalesFarmToggle
        pageContract={pageContract}
        pagePath={PAGE_PATH}
        farm={farm}
        limit={limit}
        defaultLimit={defaultLimit}
      />

      {result.ok ? (
        <BuyerSections
          analytics={result.data}
          pageContract={pageContract}
          offset={result.data.offset}
          limit={result.data.limit}
          pageHref={pageHref}
        />
      ) : null}
    </div>
  );
}

/** A requested page size is honoured only when the contract offers it. */
function resolveLimit(raw: string | undefined, offered: readonly number[], fallback: number): number {
  const parsed = raw ? Number.parseInt(raw, 10) : Number.NaN;
  if (Number.isFinite(parsed) && offered.includes(parsed)) return parsed;
  return fallback;
}
