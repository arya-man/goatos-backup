import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Building2 } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import {
  firstAuthRequiredError,
  getProcurementVendorForm,
  listProcurementVendorCatalog,
  listProcurementVendors,
  type ProcurementVendor,
  type ProcurementVendorCatalog,
} from "@/lib/api/server";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { Tag, type Tone } from "@/components/ui-primitives";
import { actionFeedbackCopy, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { IdentityCell } from "@/components/data-table";
import { ProcurementTableFooter } from "./table-footer-links";
import { ProcurementTableToolbar } from "./table-toolbar";
import { VendorFilterBar, type VendorFilterKey } from "./vendor-filter-bar";
import { VendorLocalDrawer } from "./vendor-local-drawer";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import Alert from "@mui/material/Alert";
import { salesErrorText } from "./sales-error";

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

/** Where each facet's offered values live in the vendor catalog (mirrors the filter bar's selects). */
const VENDOR_CATALOG_KEY = {
  record_type: "record_types",
  status: "statuses",
  state: "states",
  city: "cities",
  breed: "breeds",
} as const satisfies Record<VendorFilterKey, keyof ProcurementVendorCatalog>;

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

  const requestedFilters: Partial<Record<VendorFilterKey, string>> = {};
  for (const key of FILTER_KEYS) {
    const value = one(sp, key);
    if (value) requestedFilters[key] = value;
  }

  // The catalog renders the filter selects and the edit form. A facet value the catalog does not
  // offer (a stale bookmark, a hand-edited URL) is dropped ONCE here, so the select ("All"), the
  // applied chip and the vendor query all agree -- the select used to show "All" while the chip and
  // the server still applied the stale value (0 vendors). With no facet in the URL the vendor read
  // does not wait for the catalog. If the catalog read fails the values pass through unchecked.
  // The published vendor form (VENDOR FORM IS AUTHORED, 2026-09-19) rides along: it is what the
  // add / edit drawer renders, question by question.
  const catalogPromise = listProcurementVendorCatalog({ side });
  const hasRequestedFilter = Object.keys(requestedFilters).length > 0;
  const validFilters = (catalogRes: Awaited<typeof catalogPromise>): Partial<Record<VendorFilterKey, string>> => {
    if (!catalogRes.ok) return requestedFilters;
    const cleaned: Partial<Record<VendorFilterKey, string>> = {};
    for (const key of FILTER_KEYS) {
      const value = requestedFilters[key];
      if (value && (catalogRes.data[VENDOR_CATALOG_KEY[key]] ?? []).some((entry) => entry.value === value)) cleaned[key] = value;
    }
    return cleaned;
  };
  const listFor = (filters: Partial<Record<VendorFilterKey, string>>) =>
    listProcurementVendors({ search: search || undefined, limit, offset, side, ...filters });
  const [result, catalogResult, formResult] = await Promise.all([
    hasRequestedFilter ? catalogPromise.then((catalogRes) => listFor(validFilters(catalogRes))) : listFor({}),
    catalogPromise,
    getProcurementVendorForm({ side }),
  ]);
  const activeFilters = hasRequestedFilter ? validFilters(catalogResult) : requestedFilters;

  const authError = firstAuthRequiredError(result, catalogResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const vendors: ProcurementVendor[] = result.ok ? listOrEmpty(result.data.vendors) : [];
  const total = result.ok ? result.data.total : 0;
  // Page numbers come from the backend-owned total, so the pager cannot disagree with the count
  // badge above it. Both read the same number.
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const pageNumber = Math.min(pageCount, Math.floor(offset / limit) + 1);
  const catalog = catalogResult.ok ? catalogResult.data : null;
  const vendorForm = formResult.ok ? formResult.data : null;

  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");

  const hasAnyFilter = Boolean(search) || Object.keys(activeFilters).length > 0;

  return (
    <div className="screen on">
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
        actions={
          <LocalOverlayLink href={hrefWithQuery(pathname, sp, { vendor: "new" })} className="btn primary" scroll={false}>
            {copy(pageContract, "action.add")}
          </LocalOverlayLink>
        }
      />

      {/* Write feedback. Without this the operator saves a vendor and the drawer simply closes,
          which is indistinguishable from the save being dropped. */}
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
          {salesErrorText(result.error, copy(pageContract, "error.load"))}
        </Alert>
      ) : null}

      <ProcurementTableToolbar
        clearLabel={copy(pageContract, "action.clear_all", "Clear all")}
        columnsLabel={copy(pageContract, "action.columns", "Columns")}
        exportLabel={copy(pageContract, "action.export", "Export")}
        moreLabel={copy(pageContract, "action.more", "More")}
        ariaLabel={copy(pageContract, "section.vendors.title")}
        tableId="procurement-vendors"
        exportName="vendors"
        chips={[
          ...(search ? [{ id: "search", label: `${copy(pageContract, "filter.search", "Search")}: ${search}`, href: hrefWithQuery(pathname, sp, { search: null, offset: null }) }] : []),
          ...FILTER_KEYS.filter((key) => activeFilters[key]).map((key) => ({
            id: key,
            label: `${copy(pageContract, `filter.${key}`, key)}: ${activeFilters[key]}`,
            href: hrefWithQuery(pathname, sp, { [key]: null, offset: null }),
          })),
        ]}
        clearHref={
          search || FILTER_KEYS.some((key) => activeFilters[key])
            ? hrefWithQuery(pathname, sp, {
                search: null,
                offset: null,
                ...Object.fromEntries(FILTER_KEYS.map((key) => [key, null])),
              })
            : undefined
        }
      >
        <VendorFilterBar
          pageContract={pageContract}
          pathname={pathname}
          catalog={catalog}
          search={search}
          filters={activeFilters}
        />
      </ProcurementTableToolbar>

      <section className="card">
        <div className="hd">
          <Building2 className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.vendors.title")}</h3>
          {/* The WHOLE-FILTER total from the backend, not vendors.length -- this must keep reading
              "306 vendors" while the table shows 25 of them. */}
          <Tag tone={total ? "info" : "mut"}>
            {total} {copy(pageContract, "summary.count")}
          </Tag>
        </div>

        {vendors.length === 0 ? (
          <EmptyState title={hasAnyFilter ? copy(pageContract, "empty.vendors") : copy(pageContract, "empty.vendors.unset")} />
        ) : (
          <div id="procurement-vendors" className="twrap" tabIndex={0} role="region" aria-label={copy(pageContract, "section.vendors.aria")}>
            <Table className="procurement-vendors-table" aria-label={copy(pageContract, "section.vendors.aria")}>
              <TableHead>
                <TableRow>
                  <TableCell component="th">{copy(pageContract, "column.business_name")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "column.record_type")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "column.phone_number")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "column.location_display")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "column.status")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {vendors.map((vendor) => {
                  const drawerHref = hrefWithQuery(pathname, sp, { vendor: vendor.vendor_id });
                  return (
                    <TableRow key={vendor.vendor_id}>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          <IdentityCell
                            primary={vendor.business_name}
                            secondary={vendor.contact_person_name || vendor.location_display || undefined}
                            lead={<span className="kit-idcell-tile" aria-hidden="true">{vendor.business_name.slice(0, 1).toUpperCase()}</span>}
                          />
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {vendor.record_type}
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {vendor.phone_number ?? copy(pageContract, "value.none")}
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {vendor.location_display}
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {/* Backend-owned label; the frontend chooses only the tone. */}
                          <Tag tone={statusTone(vendor.status)}>{vendor.status_label}</Tag>
                        </LocalOverlayLink>
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
            rangeLabel={`${offset + 1}\u2013${offset + vendors.length} ${copy(pageContract, "pager.of")} ${total}`}
            rowsValue={limit}
            rowsOptions={[10, 25, 50, 100].map((size) => ({
              size,
              href: hrefWithQuery(pathname, sp, { limit: String(size), offset: null }),
            }))}
            prevHref={hrefWithQuery(pathname, sp, { offset: String(Math.max(0, (pageNumber - 2) * limit)) })}
            nextHref={hrefWithQuery(pathname, sp, { offset: String(pageNumber * limit) })}
            prevLabel={copy(pageContract, "action.prev_page")}
            nextLabel={copy(pageContract, "action.next_page")}
            denseTargetId="procurement-vendors"
          />
        ) : null}
      </section>

      {/* Always mounted: LocalOverlayLink changes the URL without an RSC request, so an overlay
          gated on a server-read search param would never appear. */}
      <VendorLocalDrawer
        vendors={vendors}
        catalog={catalog}
        form={vendorForm}
        pageContract={pageContract}
        listHref={hrefWithQuery(pathname, sp, { vendor: null })}
      />
    </div>
  );
}
