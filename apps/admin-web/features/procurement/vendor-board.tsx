import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Building2 } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import {
  firstAuthRequiredError,
  listProcurementVendorCatalog,
  listProcurementVendors,
  type ProcurementVendor,
} from "@/lib/api/server";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { Tag, type Tone } from "@/components/ui-primitives";
import { actionFeedbackCopy, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { VendorFilterBar, type VendorFilterKey } from "./vendor-filter-bar";
import { VendorLocalDrawer } from "./vendor-local-drawer";

const PAGE_SIZE = 25;

/**
 * Status tone for the register's chip.
 *
 * The LABEL comes from the backend (`status_label`), never from here -- this maps only the visual
 * tone, which is presentation and therefore the frontend's to own.
 */
function statusTone(status: string): Tone {
  switch (status) {
    case "active":
      return "ok";
    case "negotiating":
      return "warn";
    case "banned":
      return "dng";
    default:
      return "mut";
  }
}

function hrefWithQuery(pathname: string, sp: RouteSearchParams, patch: Record<string, string | null>): string {
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
  return qs ? `${pathname}?${qs}` : pathname;
}

const FILTER_KEYS: VendorFilterKey[] = ["record_type", "status", "state", "city", "breed"];

/**
 * Which half of the register this page shows, read from the page's OWN backend contract.
 *
 * The side is declared once, in the table's `data_source` (`/procurement/vendors?side=sales`), and
 * is read back out here rather than passed in as a prop. That is deliberate: a prop would be a
 * second statement of the same fact, and the two could disagree -- a page whose contract says sales
 * while its reads say procurement would show the buying desk's suppliers under the Sales heading
 * with nothing on screen admitting it.
 *
 * A contract with no side reads the WHOLE register, which is the behaviour every caller had before
 * the split, so an older backend still renders this page correctly instead of showing nothing.
 */
function sideFromContract(pageContract: AdminUiPageContract): string | undefined {
  for (const table of pageContract.tables) {
    const query = table.data_source.split("?")[1];
    if (!query) continue;
    const side = new URLSearchParams(query).get("side");
    if (side) return side;
  }
  return undefined;
}

export async function VendorBoardPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  // The page's own route, from its contract. Both registers render through this one component, so
  // a hardcoded path would send every link, filter and drawer on Sales > Vendors back to
  // Procurement > Vendors.
  const pathname = pageContract.href;
  const side = sideFromContract(pageContract);
  const sp = searchParams;

  const search = one(sp, "search") ?? "";
  const limit = boundedInt(one(sp, "limit"), PAGE_SIZE, 1, 100);
  const offset = boundedInt(one(sp, "offset"), 0, 0, 10000);

  const activeFilters: Partial<Record<VendorFilterKey, string>> = {};
  for (const key of FILTER_KEYS) {
    const value = one(sp, key);
    if (value) activeFilters[key] = value;
  }

  // Both reads in parallel: the catalog is needed to render the filter selects and the edit form,
  // and it does not depend on the page of vendors.
  const [result, catalogResult] = await Promise.all([
    listProcurementVendors({ search: search || undefined, limit, offset, side, ...activeFilters }),
    listProcurementVendorCatalog({ side }),
  ]);

  const authError = firstAuthRequiredError(result, catalogResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const vendors: ProcurementVendor[] = result.ok ? result.data.vendors : [];
  const total = result.ok ? result.data.total : 0;
  // Page numbers come from the backend-owned total, so the pager cannot disagree with the count
  // badge above it. Both read the same number.
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const pageNumber = Math.min(pageCount, Math.floor(offset / limit) + 1);
  const catalog = catalogResult.ok ? catalogResult.data : null;

  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");

  const hasAnyFilter = Boolean(search) || Object.keys(activeFilters).length > 0;

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
        <LocalOverlayLink
          href={hrefWithQuery(pathname, sp, { vendor: "new" })}
          className="btn primary"
          scroll={false}
          style={{ marginBottom: 4 }}
        >
          {copy(pageContract, "action.add")}
        </LocalOverlayLink>
      </div>

      {/* Write feedback. Without this the operator saves a vendor and the drawer simply closes,
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

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message || copy(pageContract, "error.load")}
        </div>
      ) : null}

      <VendorFilterBar
        pageContract={pageContract}
        pathname={pathname}
        catalog={catalog}
        search={search}
        filters={activeFilters}
      />

      <section className="card">
        <div className="hd">
          <Building2 className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.vendors.title")}</h3>
          {/* The WHOLE-FILTER total from the backend, not vendors.length -- this must keep reading
              "306 vendors" while the table shows 25 of them. */}
          <Tag tone={total ? "info" : "mut"}>
            {total} {copy(pageContract, "summary.count")}
          </Tag>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{copy(pageContract, "section.vendors.row_hint")}</span>
        </div>

        {vendors.length === 0 ? (
          <div className="empty">
            {hasAnyFilter ? copy(pageContract, "empty.vendors") : copy(pageContract, "empty.vendors.unset")}
          </div>
        ) : (
          <div className="twrap">
            <table className="procurement-vendors-table" aria-label={copy(pageContract, "section.vendors.aria")}>
              <thead>
                <tr>
                  <th>{copy(pageContract, "column.business_name")}</th>
                  <th>{copy(pageContract, "column.record_type")}</th>
                  <th>{copy(pageContract, "column.phone_number")}</th>
                  <th>{copy(pageContract, "column.location_display")}</th>
                  <th>{copy(pageContract, "column.status")}</th>
                </tr>
              </thead>
              <tbody>
                {vendors.map((vendor) => {
                  const drawerHref = hrefWithQuery(pathname, sp, { vendor: vendor.vendor_id });
                  return (
                    <tr key={vendor.vendor_id}>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          <b>{vendor.business_name}</b>
                          {vendor.contact_person_name ? (
                            <div className="muted small">{vendor.contact_person_name}</div>
                          ) : null}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {vendor.record_type}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {vendor.phone_number ?? copy(pageContract, "value.none")}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {vendor.location_display}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {/* Backend-owned label; the frontend chooses only the tone. */}
                          <Tag tone={statusTone(vendor.status)}>{vendor.status_label}</Tag>
                        </LocalOverlayLink>
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
                href={hrefWithQuery(pathname, sp, { offset: String(Math.max(0, offset - limit)) })}
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
                href={hrefWithQuery(pathname, sp, { offset: String(offset + limit) })}
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
      <VendorLocalDrawer
        vendors={vendors}
        catalog={catalog}
        pageContract={pageContract}
        listHref={hrefWithQuery(pathname, sp, { vendor: null })}
      />
    </div>
  );
}
