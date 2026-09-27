import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";

import { PageHeader } from "@/components/app/page-header";
import { SegmentTabs } from "@/components/app/list/segment-tabs";
import { controlEnabled, copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getLoadwiseSales } from "@/lib/api/procurement-server";
import { getShedWeights } from "@/lib/api/server";
import { istDayPlus, todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { pensFromPlacements } from "@/lib/load-pens";
import { LoadwiseSection, type LoadCurrentWeights, type LoadPensByRef } from "./loadwise-section";
import { SalesFarmToggle, readSalesParkScope } from "./sales-chrome";
import { UrlSuspense } from "@/components/app/url-suspense";
import { SalesLoadsBodySkeleton } from "./sales-skeletons";

const PAGE_PATH = "/sales/loads";
/** The tab the page opens on when the URL names none — the first option the contract serves. */
const DEFAULT_VIEW = "purchased";

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
 * Load wise — every batch of animals reconciled against what it cost and what it returned.
 *
 * Its own page under Sales rather than a block on the board (maintainer decision 2026-08-31): the
 * board answers how sales are going, this answers how each batch did, which is read at a different
 * time.
 *
 * READ-ONLY since the 2026-09-01 decision that moved every sales entry to /sales/config. The
 * load-cost write that used to open from these rows lives there; this page's backend contract
 * declares no control, so nothing here can open a form.
 */
export async function SalesLoadsPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;

  // The selected tab, validated against the SERVED option keys and never trusted raw. Purchased is
  // the default, so the page always opens on it.
  const views = optionGroup(pageContract, "sales_views");
  const rawView = one(sp, "view") ?? DEFAULT_VIEW;
  const view = views.some((option) => option.key === rawView) ? rawView : DEFAULT_VIEW;

  // The page's park: the SHELL's `park` (one filter across every Sales page, 2026-09-25), chosen on
  // the same farm chips Summary, Farm value, Buyer analytics and Farm born carry, and validated
  // against the caller's parks. The whole load-wise read — rows, charts, tiles and total — narrows
  // to it server-side (maintainer report 2026-09-03). An empty value is every farm.
  const { parkId, parks } = await readSalesParkScope(sp, pageContract, PAGE_PATH);

  return (
    // `sales-loads-page` is not decoration: the global `.crumb` rule uppercases every breadcrumb,
    // which rendered this page's own name as "PURCHASE AND BORN". A load is bought or born -- those
    // are ordinary words, not a code -- so the page scopes the transform off (maintainer,
    // 2026-09-02).
    <div className="screen on sales-loads-page">
      {/* The toggle is CENTRED and lifted onto the title line rather than sharing the subtitle's
          row. Measured: the subtitle needs 596px unwrapped, while a centred toggle leaves only
          533px beside it at 1600px -- so on one row the subtitle is forced to wrap. Lifting the
          toggle clears it vertically (the title itself is short), and the title block goes back to
          its natural width so the sentence renders in full on one line. */}
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb"), href: "/sales" }, { label: pageContract.title }]}
        tabs={
          views.length > 1 ? (
            <SegmentTabs
              keepScroll
              ariaLabel={copy(pageContract, "page.tabs.aria")}
              value={view}
              tabs={views.map((option) => ({
                value: option.key,
                label: option.label,
                href: hrefWithQuery(sp, { view: option.key === DEFAULT_VIEW ? null : option.key }),
              }))}
            />
          ) : undefined
        }
      />

      <SalesFarmToggle
        pageContract={pageContract}
        pagePath={PAGE_PATH}
        searchParams={sp}
        parkId={parkId}
        parks={parks}
        clears={["cost_load"]}
      />

      {/* The load-wise panel streams (guard: url-keyed-panel): a tab or farm click swaps it to its
          skeleton at once; header, tabs and farm chips stay on screen. */}
      <UrlSuspense searchParams={sp} watch={PANEL_WATCH} fallback={<SalesLoadsBodySkeleton />}>
        <SalesLoadsPanel sp={sp} pageContract={pageContract} view={view} parkId={parkId} />
      </UrlSuspense>
    </div>
  );
}

/** The params the load-wise reads take. */
const PANEL_WATCH = ["view", "park"] as const;

async function SalesLoadsPanel({
  sp,
  pageContract,
  view,
  parkId,
}: {
  sp: RouteSearchParams;
  pageContract: AdminUiPageContract;
  view: string;
  parkId: string;
}) {
  // Fetch = render: the Farm born tab reads nothing yet, so it asks for nothing.
  // The "weighs now" series reads weighing only when the contract enabled it for this principal
  // (weights_current_average_series, gated on WeighingMonitor): fetch = render. The window runs a
  // year back so every pen's LATEST weigh is inside it; by_load keeps the latest per pen.
  const nowSeriesEnabled = controlEnabled(pageContract, "weights_current_average_series", false);
  const today = todayIso();
  const [loadwiseResult, weightsResult] = await Promise.all([
    view === DEFAULT_VIEW ? getLoadwiseSales({ park_id: parkId || undefined }) : Promise.resolve(null),
    view === DEFAULT_VIEW && nowSeriesEnabled
      ? getShedWeights({ from: istDayPlus(today, -365), to: today, park_id: parkId || undefined })
      : Promise.resolve(null),
  ]);
  if (loadwiseResult && firstAuthRequiredError(loadwiseResult)) redirect(INTERNAL_LOGIN_PATH);
  let currentWeights: LoadCurrentWeights | null = null;
  // The pens each load sits in, for the bracket beside its name on every chart (maintainer
  // request 2026-09-22). Read from the SAME by_load buckets as the "weighs now" series, and kept
  // in its own map deliberately: that series is withheld from a SOLD load, whose animals have
  // left, while the pens it was weighed in remain the honest answer to "where was this load".
  // Null when the weighing read was not fetched for this principal — the charts then name no pen
  // rather than guessing one.
  let loadPens: LoadPensByRef | null = null;
  if (weightsResult?.ok) {
    currentWeights = {};
    loadPens = {};
    for (const bucket of listOrEmpty(weightsResult.data.by_load)) {
      currentWeights[bucket.load_ref] = { averageKg: bucket.average_weight_kg, animals: bucket.animals };
      loadPens[bucket.load_ref] = pensFromPlacements(bucket.placements);
    }
  }

  // READ-ONLY BY CONTRACT (maintainer decision 2026-09-01): this page's backend contract declares
  // no write control, so `controlEnabled` is false for everyone and the load rows below are not
  // clickable. Recording a load's cost lives on /sales/config. Do not "restore" the drawer here —
  // add the control back to this page's contract first, which
  // TestSalesReadPagesCarryNoWriteControl refuses.
  const canRecordCost = controlEnabled(pageContract, "record_load_cost", false);

  return (
    <LoadwiseSection
      pageContract={pageContract}
      view={view}
      loadwise={loadwiseResult}
      canRecordCost={canRecordCost}
      costHref={(loadId) => hrefWithQuery(sp, { cost_load: loadId })}
      currentWeights={currentWeights}
      loadPens={loadPens}
    />
  );
}
