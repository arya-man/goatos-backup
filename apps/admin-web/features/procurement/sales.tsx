import type { ReactNode } from "react";

import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Banknote } from "lucide-react";

import { HBarList } from "@/components/hbar-list";
import { MonthColumns } from "@/components/month-columns";
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
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getSalesOverview, listSalesDeals } from "@/lib/api/procurement-server";
import type { SalesDeal, SalesOverview } from "@/lib/api/procurement";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import {
  dealStatusTone,
  humanDate,
  inr,
  inrCompact,
  marketLossPerKg,
  monthLabel,
  monthlyAnimalsTotal,
  monthlyRevenueTotal,
  num,
  numCompact,
  resolveFarm,
  salesHref,
} from "./sales-format";
import { SalesRecordDrawer } from "./sales-record-drawer";

const PAGE_PATH = "/procurement/sales";
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
  return qs ? `${PAGE_PATH}?${qs}` : PAGE_PATH;
}

function OverviewSections({
  overview,
  pageContract,
}: {
  overview: SalesOverview;
  pageContract: AdminUiPageContract;
}) {
  const summary = overview.summary;
  const none = copy(pageContract, "value.none");
  const kgSuffix = copy(pageContract, "value.kg_suffix");
  const perKgSuffix = copy(pageContract, "value.per_kg_suffix");
  const seriesLabel = (productType: string) => copy(pageContract, `chart.series.${productType.toLowerCase()}`);
  const statusLabel = (status: string) =>
    status === "uncontacted" ? copy(pageContract, "value.status.uncontacted") : status;
  return (
    <>
          {/* 1 — headline figures, verbatim from the overview summary. */}
          <section className="grid g4 kpi-row" aria-label={copy(pageContract, "section.headline.aria")}>
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
          </section>

          <p className="muted small" style={{ margin: "6px 0 14px" }}>
            {copy(pageContract, "kpi.period")}:{" "}
            {summary.period_from ? `${humanDate(summary.period_from)} – ${humanDate(summary.period_to)}` : none}
            {" · "}
            {copy(pageContract, "kpi.live_weight")}: {num(summary.live_weight_kg)} {kgSuffix}
          </p>

          {/* 2 — month by month. Three separate charts: rupees, heads and kg never share an axis.
              Stacked full-width so every column carries its month label and value. */}
          <section className="card" aria-label={copy(pageContract, "section.monthly.aria")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.monthly.title")}</h3>
            </div>
            <div className="mt" style={{ marginTop: 4 }}>
              {copy(pageContract, "chart.monthly_revenue.title")}
            </div>
            <MonthColumns
              data={overview.monthly.map((month) => ({
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
            <MonthColumns
              data={overview.monthly.map((month) => ({
                key: month.month,
                axisLabel: monthLabel(month.month),
                label: monthLabel(month.month),
                value: monthlyAnimalsTotal(month),
                display: num(monthlyAnimalsTotal(month)),
              }))}
              chartLabel={copy(pageContract, "chart.monthly_animals.title")}
              valueNoun={copy(pageContract, "chart.monthly_animals.value")}
              emptyLabel={copy(pageContract, "chart.monthly_animals.empty")}
            />
            <div className="mt">{copy(pageContract, "chart.monthly_manure.title")}</div>
            <MonthColumns
              data={overview.monthly.map((month) => ({
                key: month.month,
                axisLabel: monthLabel(month.month),
                label: monthLabel(month.month),
                value: month.manure_kg,
                display: numCompact(month.manure_kg),
              }))}
              chartLabel={copy(pageContract, "chart.monthly_manure.title")}
              valueNoun={copy(pageContract, "chart.monthly_manure.value")}
              emptyLabel={copy(pageContract, "chart.monthly_manure.empty")}
            />
          </section>

          {/* 3 — realized price per kg by breed, ordered as served (highest first). */}
          <section className="card" aria-label={copy(pageContract, "section.price_bands.aria")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.price_bands.title")}</h3>
            </div>
            <HBarList
              data={overview.price_bands.map((band) => ({
                key: `${band.product_type}|${band.breed}`,
                label: `${band.breed} · ${seriesLabel(band.product_type)}`,
                value: Math.round(band.avg_price_per_kg),
                display: `${inr(Math.round(band.avg_price_per_kg))} ${perKgSuffix}`,
              }))}
              emptyLabel={copy(pageContract, "chart.price_bands.empty")}
              valueNoun={copy(pageContract, "chart.price_bands.value")}
              chartLabel={copy(pageContract, "chart.price_bands.title")}
              maxBars={12}
            />
          </section>

          {/* 4 — market reality check. */}
          <section className="card" aria-label={copy(pageContract, "section.market.title")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.market.title")}</h3>
              <div className="sp" style={{ flex: 1 }} />
              <span className="muted small">{copy(pageContract, "section.market.subtitle")}</span>
            </div>
            {overview.market_benchmarks.length === 0 ? (
              <div className="empty">{copy(pageContract, "empty.market")}</div>
            ) : (
              <div className="twrap">
                <table>
                  <thead>
                    <tr>
                      <th>{copy(pageContract, "column.market")}</th>
                      <th>{copy(pageContract, "column.category")}</th>
                      <th>{copy(pageContract, "column.breed")}</th>
                      <th>{copy(pageContract, "column.source")}</th>
                      <th>{copy(pageContract, "column.ex_farm_rate")}</th>
                      <th>{copy(pageContract, "column.transport_rate")}</th>
                      <th>{copy(pageContract, "column.landing_cost_per_kg")}</th>
                      <th>{copy(pageContract, "column.market_price_per_kg")}</th>
                      <th>{copy(pageContract, "column.market_gap")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {overview.market_benchmarks.map((benchmark, index) => {
                      const loss = marketLossPerKg(benchmark.landing_cost_per_kg, benchmark.market_price_per_kg);
                      return (
                        <tr key={`${benchmark.breed}|${benchmark.market ?? ""}|${benchmark.source ?? ""}|${index}`}>
                          <td>{benchmark.market ?? none}</td>
                          <td>{benchmark.category ?? none}</td>
                          <td>
                            <b>{benchmark.breed}</b>
                          </td>
                          <td>{benchmark.source ?? none}</td>
                          <td>{benchmark.ex_farm_rate ?? none}</td>
                          <td>{benchmark.transport_rate ?? none}</td>
                          <td className="num">
                            {benchmark.landing_cost_per_kg == null
                              ? none
                              : `${inr(benchmark.landing_cost_per_kg)} ${perKgSuffix}`}
                          </td>
                          <td className="num">
                            {benchmark.market_price_per_kg == null
                              ? none
                              : `${inr(benchmark.market_price_per_kg)} ${perKgSuffix}`}
                          </td>
                          <td className="num">
                            {loss == null ? (
                              <span className="muted">{none}</span>
                            ) : (
                              <Tag tone={loss > 0 ? "dng" : "ok"}>
                                {inr(loss)} {perKgSuffix}
                              </Tag>
                            )}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </section>

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
              <div className="twrap">
                <table>
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
                    {overview.buyers.map((buyer) => (
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
          </section>

          {/* 6 — demand pipeline: buyer leads and farmer groups, status shape plus geography. */}
          <section className="grid g2" aria-label={copy(pageContract, "section.pipeline.title")}>
            <div className="card">
              <div className="hd">
                <h3>{copy(pageContract, "pipeline.buyers.title")}</h3>
                <Tag tone={overview.buyer_pipeline.total > 0 ? "info" : "mut"}>
                  {num(overview.buyer_pipeline.total)} {copy(pageContract, "pipeline.buyers.total")}
                </Tag>
              </div>
              {overview.buyer_pipeline.total === 0 ? (
                <div className="empty">{copy(pageContract, "empty.buyer_pipeline")}</div>
              ) : (
                <>
                  <div className="mt">{copy(pageContract, "pipeline.status_heading")}</div>
                  <HBarList
                    data={overview.buyer_pipeline.statuses.map((status) => ({
                      key: status.status,
                      label: statusLabel(status.status),
                      value: status.count,
                    }))}
                    emptyLabel={copy(pageContract, "empty.buyer_pipeline")}
                    valueNoun={copy(pageContract, "pipeline.buyers.total")}
                    chartLabel={copy(pageContract, "pipeline.status_heading")}
                    maxBars={10}
                  />
                  <div className="mt" style={{ marginTop: 8 }}>
                    {copy(pageContract, "pipeline.buyers.places")}
                  </div>
                  <div className="chips">
                    {overview.buyer_pipeline.top_places.map((place) => (
                      <Tag key={place.place} tone="mut">
                        {place.place} · {num(place.count)}
                      </Tag>
                    ))}
                  </div>
                </>
              )}
            </div>
            <div className="card">
              <div className="hd">
                <h3>{copy(pageContract, "pipeline.fpo.title")}</h3>
                <Tag tone={overview.fpo_pipeline.total > 0 ? "info" : "mut"}>
                  {num(overview.fpo_pipeline.total)} {copy(pageContract, "pipeline.fpo.total")}
                </Tag>
              </div>
              {overview.fpo_pipeline.total === 0 ? (
                <div className="empty">{copy(pageContract, "empty.fpo_pipeline")}</div>
              ) : (
                <>
                  <div className="mt">{copy(pageContract, "pipeline.status_heading")}</div>
                  <HBarList
                    data={overview.fpo_pipeline.statuses.map((status) => ({
                      key: status.status,
                      label: statusLabel(status.status),
                      value: status.count,
                    }))}
                    emptyLabel={copy(pageContract, "empty.fpo_pipeline")}
                    valueNoun={copy(pageContract, "pipeline.fpo.total")}
                    chartLabel={copy(pageContract, "pipeline.status_heading")}
                    maxBars={10}
                  />
                  <div className="mt" style={{ marginTop: 8 }}>
                    {copy(pageContract, "pipeline.fpo.districts")}
                  </div>
                  <div className="chips">
                    {overview.fpo_pipeline.districts.map((district) => (
                      <Tag key={district.place} tone="mut">
                        {district.place} · {num(district.count)}
                      </Tag>
                    ))}
                  </div>
                </>
              )}
            </div>
          </section>

          {/* 7 — sale evidence: tag handovers and the video-vs-book weight check. */}
          <section className="grid g2" aria-label={copy(pageContract, "section.evidence.title")}>
            <div className="card">
              <div className="hd">
                <h3 style={{ whiteSpace: "nowrap" }}>{copy(pageContract, "evidence.tags.title")}</h3>
              </div>
              <p className="muted small" style={{ marginTop: 0 }}>
                {copy(pageContract, "section.evidence.subtitle")}
              </p>
              {overview.tag_roster.total === 0 ? (
                <div className="empty">{copy(pageContract, "empty.tags")}</div>
              ) : (
                <>
                  <p className="muted small">
                    {num(overview.tag_roster.total)} {copy(pageContract, "evidence.tags.total")}
                    {" · "}
                    {num(overview.tag_roster.sales_count)} {copy(pageContract, "evidence.tags.sales")}
                  </p>
                  <div className="mt">{copy(pageContract, "evidence.tags.by_type")}</div>
                  <HBarList
                    data={overview.tag_roster.by_type.map((entry) => ({
                      key: entry.label,
                      label: entry.label,
                      value: entry.count,
                    }))}
                    emptyLabel={copy(pageContract, "empty.tags")}
                    valueNoun={copy(pageContract, "evidence.tags.total")}
                    chartLabel={copy(pageContract, "evidence.tags.by_type")}
                    maxBars={10}
                  />
                </>
              )}
            </div>
            <div className="card">
              <div className="hd">
                <h3 style={{ whiteSpace: "nowrap" }}>{copy(pageContract, "evidence.audit.title")}</h3>
              </div>
              <p className="muted small" style={{ marginTop: 0 }}>
                {copy(pageContract, "evidence.audit.subtitle")}
              </p>
              {overview.weight_audit.total === 0 ? (
                <div className="empty">{copy(pageContract, "empty.audit")}</div>
              ) : (
                <>
                  <HBarList
                    data={[
                      {
                        key: "within_0_3",
                        label: copy(pageContract, "evidence.audit.within_0_3"),
                        value: overview.weight_audit.within_0_3_kg,
                      },
                      {
                        key: "within_1",
                        label: copy(pageContract, "evidence.audit.within_1"),
                        value: overview.weight_audit.within_1_kg,
                      },
                      {
                        key: "over_1",
                        label: copy(pageContract, "evidence.audit.over_1"),
                        value: overview.weight_audit.over_1_kg,
                      },
                    ]}
                    emptyLabel={copy(pageContract, "empty.audit")}
                    valueNoun={copy(pageContract, "evidence.tags.total")}
                    chartLabel={copy(pageContract, "evidence.audit.title")}
                  />
                  <p className="muted small">
                    {copy(pageContract, "evidence.audit.max_gap")}: {num(overview.weight_audit.max_gap_kg, 1)}{" "}
                    {kgSuffix}
                  </p>
                </>
              )}
            </div>
          </section>
    </>
  );
}

export async function SalesPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;

  // Farm scope: validated against the SERVED option keys, never trusted raw. The whole page —
  // overview blocks and ledger alike — reads the one selected scope.
  const farmOptions = optionGroup(pageContract, "sales_farms");
  const farm = resolveFarm(
    one(sp, "farm"),
    farmOptions.map((option) => option.key),
    DEFAULT_FARM,
  );

  const dealsTable = table(pageContract, "sales-deals");
  const pageSizes = dealsTable.page_size_options.length > 0 ? dealsTable.page_size_options : [DEFAULT_LIMIT];
  const limit = boundedInt(one(sp, "limit"), pageSizes[0], 1, 100);
  const offset = boundedInt(one(sp, "offset"), 0, 0, 10000);

  // The whole screen's data: ONE parallel pair — the overview contract and one ledger page.
  const [overviewResult, dealsResult] = await Promise.all([
    getSalesOverview({ farm }),
    listSalesDeals({ farm, limit, offset }),
  ]);

  if (firstAuthRequiredError(overviewResult, dealsResult)) redirect(INTERNAL_LOGIN_PATH);

  const overview: SalesOverview | null = overviewResult.ok ? overviewResult.data : null;
  const deals: SalesDeal[] = dealsResult.ok ? dealsResult.data.deals : [];
  const total = dealsResult.ok ? dealsResult.data.total : 0;
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const pageNumber = Math.min(pageCount, Math.floor(offset / limit) + 1);

  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const canRecord = controlEnabled(pageContract, "record_sale", false);
  const none = copy(pageContract, "value.none");
  const dealColumns = tableLabels(pageContract, "sales-deals");
  const listHref = hrefWithQuery(sp, { deal_id: null });

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
      </div>

      {/* Write feedback. Without this the operator records a sale and the drawer simply closes,
          which is indistinguishable from the save being dropped. */}
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

      {!overviewResult.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{overviewResult.error.code ?? overviewResult.error.kind}</b>&nbsp;
          {overviewResult.error.message || copy(pageContract, "error.load")}
        </div>
      ) : null}

      {/* Farm scope toggle: server-rendered links, so the selection survives a reload and a shared
          URL. A farm switch drops the ledger offset by construction (salesHref omits it). */}
      <div className="chips" role="group" aria-label={copy(pageContract, "filter.farm")} style={{ marginBottom: 14 }}>
        <span className="muted small" style={{ marginRight: 6 }}>
          {copy(pageContract, "filter.farm")}
        </span>
        {farmOptions.map((option) => (
          <Link
            key={option.key}
            href={salesHref({ farm: option.key, limit }, { farm: DEFAULT_FARM, limit: pageSizes[0] })}
            scroll={false}
            className={option.key === farm ? "btn sm p" : "btn sm"}
            aria-current={option.key === farm ? "true" : undefined}
          >
            {option.label}
          </Link>
        ))}
      </div>

      {overview ? <OverviewSections overview={overview} pageContract={pageContract} /> : null}

      {/* 8 — the deals ledger. */}
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
            <b>{dealsResult.error.code ?? dealsResult.error.kind}</b>&nbsp;
            {dealsResult.error.message || copy(pageContract, "error.load")}
          </div>
        ) : null}

        {deals.length === 0 ? (
          <div className="empty">
            {farm !== DEFAULT_FARM ? copy(pageContract, "empty.deals") : copy(pageContract, "empty.deals.unset")}
          </div>
        ) : (
          <div className="twrap">
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
              <Link
                href={salesHref(
                  { farm, limit, offset: Math.max(0, offset - limit) },
                  { farm: DEFAULT_FARM, limit: pageSizes[0] },
                )}
                scroll={false}
                className="btn"
              >
                {copy(pageContract, "action.prev_page")}
              </Link>
            ) : null}
            {pageNumber < pageCount ? (
              <Link
                href={salesHref({ farm, limit, offset: offset + limit }, { farm: DEFAULT_FARM, limit: pageSizes[0] })}
                scroll={false}
                className="btn"
              >
                {copy(pageContract, "action.next_page")}
              </Link>
            ) : null}
          </div>
        ) : null}
      </section>

      {/* Always mounted: LocalOverlayLink changes the URL without an RSC request, so an overlay
          gated on a server-read search param would never appear. */}
      <SalesRecordDrawer deals={deals} pageContract={pageContract} listHref={listHref} canRecord={canRecord} />
    </div>
  );
}
