import { redirect } from "next/navigation";

import Link from "@/components/no-prefetch-link";
import { controlEnabled, copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getLoadwiseSales } from "@/lib/api/procurement-server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { LoadwiseSection } from "./loadwise-section";
import { LoadCostDrawer } from "./load-cost-drawer";

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
 * Purchase & barn — every batch of animals reconciled against what it cost and what it returned.
 *
 * Its own page under Sales rather than a block on the board (maintainer decision 2026-08-31): the
 * board answers how sales are going, this answers how each batch did, which is read at a different
 * time and carries its own write.
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

  // Fetch = render: the Farm born tab reads nothing yet, so it asks for nothing.
  const loadwiseResult = view === DEFAULT_VIEW ? await getLoadwiseSales() : null;
  if (loadwiseResult && firstAuthRequiredError(loadwiseResult)) redirect(INTERNAL_LOGIN_PATH);

  const canRecordCost = controlEnabled(pageContract, "record_load_cost", false);
  const listHref = hrefWithQuery(sp, { cost_load: null });
  const loads = loadwiseResult?.ok ? loadwiseResult.data.loads : [];

  return (
    <div className="screen on">
      <div className="phead" style={{ marginTop: 12, alignItems: "flex-end", paddingBottom: 6 }}>
        <div style={{ flex: 1, minWidth: 0 }}>
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
          className="chips"
          role="group"
          aria-label={copy(pageContract, "page.tabs.aria")}
          style={{ marginBottom: 4, justifyContent: "center" }}
        >
          {views.map((option) => (
            <Link
              key={option.key}
              href={hrefWithQuery(sp, {
                view: option.key === DEFAULT_VIEW ? null : option.key,
                cost_load: null,
              })}
              scroll={false}
              className={option.key === view ? "btn p" : "btn"}
              aria-current={option.key === view ? "true" : undefined}
            >
              {option.label}
            </Link>
          ))}
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      <LoadwiseSection
        pageContract={pageContract}
        view={view}
        loadwise={loadwiseResult}
        canRecordCost={canRecordCost}
        costHref={(loadId) => hrefWithQuery(sp, { cost_load: loadId })}
      />

      {/* Always mounted: LocalOverlayLink changes the URL without an RSC request, so an overlay
          gated on a server-read search param would never appear. */}
      <LoadCostDrawer
        loads={loads}
        pageContract={pageContract}
        listHref={listHref}
        canRecordCost={canRecordCost}
      />
    </div>
  );
}
