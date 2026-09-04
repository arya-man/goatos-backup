import type { ReactNode } from "react";

import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Banknote } from "lucide-react";

import { HBarList } from "@/components/hbar-list";
import { MonthColumns } from "@/components/month-columns";
import { Tag } from "@/components/ui-primitives";
import {
  controlEnabled,
  copy,
  optionGroup,
  table,
  tableLabels,
  tablePageSizes,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getShedWeights, listProcurementVendorOptions } from "@/lib/api/server";
import { istDayPlus, todayIso } from "@/lib/format";
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
  numCompact,
  numCompactWhole,
  resolveFarm,
  salesHref,
} from "./sales-format";
import { SalesRecordDrawer } from "./sales-record-drawer";
import { SalesReadyToleranceControl } from "./sales-ready-tolerance-control";

const PAGE_PATH = "/sales";
const DEFAULT_FARM = "all";
const DEFAULT_LIMIT = 25;
/** Only used when an older backend contract has no buyer board table; the contract page size wins. */
const BUYERS_PAGE_SIZE = 10;

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
 * The Over 35 kg card's figure (maintainer request 2026-09-03). `count` is null when the read
 * failed or the caller may not read weighing; the card then shows the backend's reason rather
 * than a zero that would claim no kid is ready for sale.
 */
type Over35Card = {
  enabled: boolean;
  disabledReason: string;
  count: number | null;
  from: string;
  to: string;
  toleranceG: number;
  thresholdKg: number;
  preserveQuery: [string, string][];
};

/** The nominal sale-ready lookback; backend clamps tolerance reads to reliable weighing data. */
const OVER35_WINDOW_DAYS = 42;
const OVER35_MAX_TOLERANCE_G = 1000;

function OverviewSections({
  overview,
  pageContract,
  buyersHref,
  buyersPage,
  over35,
}: {
  overview: SalesOverview;
  pageContract: AdminUiPageContract;
  over35: Over35Card;
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
  const statusLabel = (status: string) =>
    status === "uncontacted" ? copy(pageContract, "value.status.uncontacted") : status;
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
          {/* 1 — headline figures, verbatim from the overview summary. */}
          <section className="grid g5 kpi-row sales-kpi-row" aria-label={copy(pageContract, "section.headline.aria")}>
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
            {/* Over 35 kg: the Weights pages' sale-weight count on the backend's reliable weighing
                window. Gated by the page contract: a role that may not read weights sees the
                backend's reason, never a zero. */}
            <div className="kpi">
              <div className="lab">{copy(pageContract, "kpi.over35")}</div>
              <div className="val">{over35.count == null ? none : num(over35.count)}</div>
              <div className="dl">
                {!over35.enabled
                  ? over35.disabledReason
                  : over35.count == null
                    ? copy(pageContract, "kpi.over35.none")
                    : `${copy(pageContract, "kpi.over35.sub")} · ${num(over35.thresholdKg, 1)}+`}
              </div>
            </div>
          </section>
          {over35.enabled ? (
            <SalesReadyToleranceControl
              key={over35.toleranceG}
              valueG={over35.toleranceG}
              maxG={OVER35_MAX_TOLERANCE_G}
              preserveQuery={over35.preserveQuery}
              label={copy(pageContract, "kpi.over35.tolerance")}
              applyLabel={copy(pageContract, "kpi.over35.apply")}
            />
          ) : null}


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
            {/* Head count owns the bar; the rupees it earned ride under the month label so the two
                units are read separately and never share the axis. */}
            <MonthColumns
              data={overview.monthly.map((month) => ({
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
              data={overview.monthly.map((month) => ({
                key: month.month,
                axisLabel: monthLabel(month.month),
                label: monthLabel(month.month),
                value: month.manure_kg,
                display: numCompactWhole(month.manure_kg),
                subDisplay: month.manure_revenue > 0 ? inrCompact(month.manure_revenue) : "",
              }))}
              chartLabel={copy(pageContract, "chart.monthly_manure.title")}
              valueNoun={copy(pageContract, "chart.monthly_manure.value")}
              subValueNoun={copy(pageContract, "chart.monthly_manure.sub")}
              emptyLabel={copy(pageContract, "chart.monthly_manure.empty")}
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
                label: `${band.breed} · ${seriesLabel(band.product_type)}`,
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
                  </Link>
                ) : (
                  <span className="btn" aria-disabled="true">
                    {copy(pageContract, "action.prev_page")}
                  </span>
                )}
                {buyersPageNumber < buyersPageCount ? (
                  <Link href={buyersHref(buyersPageNumber + 1)} scroll={false} className="btn">
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

          {/* 6 — demand pipeline: buyer leads and farmer groups, status shape plus geography. */}
          <section className="grid g2" aria-label={copy(pageContract, "section.pipeline.title")}>
            <div className="card sales-card">
              <div className="hd">
                <h3>{copy(pageContract, "pipeline.buyers.title")}</h3>
                <Tag tone={overview.buyer_pipeline.total > 0 ? "info" : "mut"}>
                  {num(overview.buyer_pipeline.total)} {copy(pageContract, "pipeline.buyers.total")}
                </Tag>
                <div className="sp" style={{ flex: 1 }} />
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
            <div className="card sales-card">
              <div className="hd">
                <h3>{copy(pageContract, "pipeline.fpo.title")}</h3>
                <Tag tone={overview.fpo_pipeline.total > 0 ? "info" : "mut"}>
                  {num(overview.fpo_pipeline.total)} {copy(pageContract, "pipeline.fpo.total")}
                </Tag>
                <div className="sp" style={{ flex: 1 }} />
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
            <div className="card sales-card">
              <div className="hd">
                <h3 style={{ whiteSpace: "nowrap" }}>{copy(pageContract, "evidence.tags.title")}</h3>
                <div className="sp" style={{ flex: 1 }} />
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
            <div className="card sales-card">
              <div className="hd">
                <h3 style={{ whiteSpace: "nowrap" }}>{copy(pageContract, "evidence.audit.title")}</h3>
                <div className="sp" style={{ flex: 1 }} />
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
  // Buyer board page. A hand-edited value is clamped here and again against the served row count,
  // so an out-of-range page can never take the section down.
  const buyersPage = boundedInt(one(sp, "buyers_page"), 1, 1, 1000);

  // The whole screen's data in ONE parallel read: the overview contract, one ledger page, and the
  // first page of each pipeline (the entry drawers list and update them; LocalOverlayLink opens
  // without an RSC request, so drawer data must ride with the page).
  //
  // The pipeline lead lists and the tag-animals location catalog are deliberately NOT read here
  // any more: they fed ENTRY forms, and entry moved to /sales/config (maintainer decision
  // 2026-09-01). Fetch = render — this page renders no form, so it asks for no form's data.
  // The Over 35 kg card reads weighing only when the contract enables it: fetch = render, and a
  // role the weighing endpoint would refuse is never asked to make that call.
  const over35Control = pageContract.controls.find((item) => item.id === "weights_over_35_card");
  const over35Enabled = over35Control?.enabled ?? false;
  const over35To = todayIso();
  const over35From = istDayPlus(over35To, -OVER35_WINDOW_DAYS);
  const over35ToleranceG = boundedInt(one(sp, "sale_ready_tolerance_g"), 0, 0, OVER35_MAX_TOLERANCE_G);
  const over35ThresholdKg = Math.max(0, 35 - over35ToleranceG / 1000);
  const over35Params = {
    from: over35From,
    to: over35To,
    sale_threshold_tolerance_g: String(over35ToleranceG),
  };
  const [overviewResult, dealsResult, vendorOptionsResult, weightsResult] = await Promise.all([
    getSalesOverview({ farm }),
    listSalesDeals({ farm, limit, offset }),
    // The deal drawer here is a READ-ONLY detail, and it still names the buyer's vendor. Resolving
    // that id to the register's name needs the active register with the page — LocalOverlayLink
    // opens the drawer without an RSC request. ONE bounded read, never a paged walk of
    // /procurement/vendors: that is the banned SSR full-walk shape.
    listProcurementVendorOptions(),
    // Unscoped first: the response also carries the park vocabulary this page's farm code is
    // matched against, so a farm-scoped card needs exactly one more read, below.
    over35Enabled ? getShedWeights(over35Params) : Promise.resolve(null),
  ]);

  if (firstAuthRequiredError(overviewResult, dealsResult)) redirect(INTERNAL_LOGIN_PATH);

  // null means the register could NOT be read (it is a separate permission, procurement.vendor.read).
  // The drawer renders a stated error for that case rather than an empty dropdown, which would read
  // as "there are no vendors" and send the person to add one that already exists.
  const vendorOptions: ProcurementVendorOptions | null = vendorOptionsResult.ok ? vendorOptionsResult.data : null;

  // Farm scope for the card. The weighing park vocabulary names parks by their code (CBE, CPT),
  // the same code this page's farm filter carries, so the selected farm resolves to a park id
  // and the count is re-read for that park alone. An unknown farm code keeps the all-parks
  // figure rather than showing a zero for a park that was never asked about.
  let over35Count: number | null = null;
  if (weightsResult?.ok) {
    over35Count = weightsResult.data.summary.at_or_above_35kg;
    if (farm !== DEFAULT_FARM) {
      const park = weightsResult.data.parks.find((item) => item.name === farm);
      if (park) {
        const scoped = await getShedWeights({ ...over35Params, park_id: park.park_id });
        over35Count = scoped.ok ? scoped.data.summary.at_or_above_35kg : null;
      }
    }
  }
  const over35PreserveQuery = Object.entries(sp).flatMap(([key, value]) => {
    if (key === "sale_ready_tolerance_g") return [];
    const first = Array.isArray(value) ? value[0] : value;
    return first ? ([[key, first]] as [string, string][]) : [];
  });
  const over35: Over35Card = {
    enabled: over35Enabled,
    disabledReason: over35Control?.disabled_reason ?? "",
    count: over35Count,
    from: over35From,
    to: over35To,
    toleranceG: over35ToleranceG,
    thresholdKg: over35ThresholdKg,
    preserveQuery: over35PreserveQuery,
  };

  const overview: SalesOverview | null = overviewResult.ok ? overviewResult.data : null;
  const deals: SalesDeal[] = dealsResult.ok ? dealsResult.data.deals : [];
  const total = dealsResult.ok ? dealsResult.data.total : 0;
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const pageNumber = Math.min(pageCount, Math.floor(offset / limit) + 1);

  // READ-ONLY BY CONTRACT (maintainer decision 2026-09-01): the backend page contract for /sales
  // declares no write control at all, so `controlEnabled` is false for everyone including the CEO
  // and the deal drawer below opens as a detail view. Recording, editing and tagging live on
  // /sales/config. Do not "restore" a button here — add the control back to this page's contract
  // first, which TestSalesReadPagesCarryNoWriteControl refuses.
  const canRecord = controlEnabled(pageContract, "record_sale", false);
  const none = copy(pageContract, "value.none");
  const dealColumns = tableLabels(pageContract, "sales-deals");
  const listHref = hrefWithQuery(sp, { deal_id: null });

  return (
    <div className="screen on">
      <div className="phead" style={{ marginTop: 12, alignItems: "flex-end", paddingBottom: 6 }}>
        <div>
          <div className="crumb">
            {/* The crumb names the VERTICAL and the title names the page. Sales is now a vertical
                whose single page carries the same name, so appending the title unconditionally
                repeated that one word on both sides of the separator. The dedupe is presentation
                only -- both strings stay backend-owned and neither is composed here. */}
            <b>{copy(pageContract, "crumb")}</b>
            {copy(pageContract, "crumb") === pageContract.title ? null : <> · {pageContract.title}</>}
          </div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
      </div>

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
            href={salesHref(
              { farm: option.key, limit, saleReadyToleranceG: over35.toleranceG },
              { farm: DEFAULT_FARM, limit: pageSizes[0] },
            )}
            scroll={false}
            className={option.key === farm ? "btn sm p" : "btn sm"}
            aria-current={option.key === farm ? "true" : undefined}
          >
            {option.label}
          </Link>
        ))}
      </div>

      {overview ? (
        <OverviewSections
          overview={overview}
          pageContract={pageContract}
          buyersPage={buyersPage}
          buyersHref={(page) => hrefWithQuery(sp, { buyers_page: page > 1 ? String(page) : null })}
          over35={over35}
        />
      ) : null}

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
                  { farm, limit, offset: Math.max(0, offset - limit), saleReadyToleranceG: over35.toleranceG },
                  { farm: DEFAULT_FARM, limit: pageSizes[0] },
                )}
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
              <Link
                href={salesHref(
                  { farm, limit, offset: offset + limit, saleReadyToleranceG: over35.toleranceG },
                  { farm: DEFAULT_FARM, limit: pageSizes[0] },
                )}
                scroll={false}
                className="btn"
              >
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
      <SalesRecordDrawer
        deals={deals}
        pageContract={pageContract}
        listHref={listHref}
        canRecord={canRecord}
        vendorOptions={vendorOptions}
      />
    </div>
  );
}
