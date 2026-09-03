import { redirect } from "next/navigation";

import Link from "@/components/no-prefetch-link";
import { controlEnabled, copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getLoadwiseSales } from "@/lib/api/procurement-server";
import { getShedWeights } from "@/lib/api/server";
import { istDayPlus, todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { LoadwiseSection, type LoadCurrentWeights } from "./loadwise-section";

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
 * Purchase and Born — every batch of animals reconciled against what it cost and what it returned.
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

  // The top-bar park selector's value. It was silently ignored here (maintainer report
  // 2026-09-03: switching All Parks / CBE / CPT changed none of the graphs), so the whole
  // load-wise read — rows, charts, tiles and total — now narrows to the selected park
  // server-side. An empty value is All Parks.
  const parkId = one(sp, "park") ?? "";

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
  if (weightsResult?.ok) {
    currentWeights = {};
    for (const bucket of weightsResult.data.by_load) {
      currentWeights[bucket.load_ref] = { averageKg: bucket.average_weight_kg, animals: bucket.animals };
    }
  }

  // READ-ONLY BY CONTRACT (maintainer decision 2026-09-01): this page's backend contract declares
  // no write control, so `controlEnabled` is false for everyone and the load rows below are not
  // clickable. Recording a load's cost lives on /sales/config. Do not "restore" the drawer here —
  // add the control back to this page's contract first, which
  // TestSalesReadPagesCarryNoWriteControl refuses.
  const canRecordCost = controlEnabled(pageContract, "record_load_cost", false);

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
      <div className="phead sales-loads-head">
        <div>
          <div className="crumb">
            <b>{copy(pageContract, "crumb")}</b> · {pageContract.title}
          </div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
        {/* Equal spacers either side put the toggle in the MIDDLE of the page header rather than
            hard against the right edge: it switches the whole page, so it reads as the page's own
            control instead of an action belonging to the title block.
            Purchased sits on the left and is selected by default, Farm born on the right.
            Server-rendered links, so the choice survives a reload and a shared URL.
            The title block and the trailing spacer carry the SAME flex share, which is what puts
            the toggle on the header's true midpoint -- two spacers alone would only centre it in
            the space the title leaves over. */}
        <div
          className="chips sales-loads-tabs"
          role="group"
          aria-label={copy(pageContract, "page.tabs.aria")}
        >
          {views.map((option) => (
            <Link
              key={option.key}
              href={hrefWithQuery(sp, { view: option.key === DEFAULT_VIEW ? null : option.key })}
              scroll={false}
              className={option.key === view ? "btn p" : "btn"}
              aria-current={option.key === view ? "true" : undefined}
            >
              {option.label}
            </Link>
          ))}
        </div>
      </div>

      <LoadwiseSection
        pageContract={pageContract}
        view={view}
        loadwise={loadwiseResult}
        canRecordCost={canRecordCost}
        costHref={(loadId) => hrefWithQuery(sp, { cost_load: loadId })}
        currentWeights={currentWeights}
      />
    </div>
  );
}
