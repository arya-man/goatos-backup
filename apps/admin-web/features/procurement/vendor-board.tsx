import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Link from "@mui/material/Link";
import Stack from "@mui/material/Stack";
import Table from "@mui/material/Table";
import Alert from "@mui/material/Alert";
import Avatar from "@mui/material/Avatar";
import Button from "@mui/material/Button";
import TableRow from "@mui/material/TableRow";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import IconButton from "@mui/material/IconButton";
import { redirect } from "next/navigation";
import { listOrEmpty } from "@/lib/list-or-empty";
import { LocalOverlayLink } from "@/components/local-overlay-link";
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
import { actionFeedbackCopy, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { Label, type LabelColor } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/minimal/table/table-head-custom";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import { ProcurementTableFooter } from "./table-footer-links";
import { ProcurementFiltersResult, type ToolbarChip } from "./table-toolbar";
import { VendorFilterBar, type VendorFilterKey } from "./vendor-filter-bar";
import { VendorLocalDrawer } from "./vendor-local-drawer";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import { salesErrorText } from "./sales-error";

const PAGE_SIZE = 25;

/**
 * Status colour for the register's Label.
 *
 * The LABEL comes from the backend (`status_label`), never from here -- this maps only the visual
 * colour, which is presentation and therefore the frontend's to own.
 */
function statusColor(status: string): LabelColor {
  switch (status) {
    case "active":
      return "success";
    case "negotiating":
      return "warning";
    case "banned":
      return "error";
    default:
      return "default";
  }
}

/** The catalog's label for an applied facet value (the chip shows "Tamil Nadu", not "tn"). */
function catalogLabel(catalog: ProcurementVendorCatalog | null, key: VendorFilterKey, value: string): string {
  return catalog?.[VENDOR_CATALOG_KEY[key]]?.find((entry) => entry.value === value)?.label ?? value;
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

  const statusValue = activeFilters.status ?? "";
  const statusTabs = [
    { value: "", label: copy(pageContract, "filter.all") },
    ...(catalog?.statuses ?? []).filter((entry) => entry.is_active || entry.value === statusValue).map((entry) => ({ value: entry.value, label: entry.label })),
  ];
  const headCells = [
    { id: "business_name", label: copy(pageContract, "column.business_name"), sortable: false },
    { id: "record_type", label: copy(pageContract, "column.record_type"), sortable: false },
    { id: "phone_number", label: copy(pageContract, "column.phone_number"), sortable: false },
    { id: "location_display", label: copy(pageContract, "column.location_display"), sortable: false },
    { id: "status", label: copy(pageContract, "column.status"), sortable: false },
    { id: "", width: 64, sortable: false },
  ];
  const chips: ToolbarChip[] = [
    ...(search ? [{ id: "search", group: copy(pageContract, "filter.search", "Search"), label: search, href: hrefWithQuery(pathname, sp, { search: null, offset: null }) }] : []),
    ...FILTER_KEYS.filter((key) => key !== "status" && activeFilters[key]).map((key) => ({
      id: key,
      group: copy(pageContract, `filter.${key}`, key),
      label: catalogLabel(catalog, key, activeFilters[key] ?? ""),
      href: hrefWithQuery(pathname, sp, { [key]: null, offset: null }),
    })),
  ];

  return (
    <div className="screen on">
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
        actions={
          <Button
            component={LocalOverlayLink}
            href={hrefWithQuery(pathname, sp, { vendor: "new" })}
            scroll={false}
            variant="contained"
            color="primary"
            startIcon={<Iconify icon="mingcute:add-line" />}
          >
            {copy(pageContract, "action.add")}
          </Button>
        }
      />

      {/* Write feedback. Without this the operator saves a vendor and the drawer simply closes,
          which is indistinguishable from the save being dropped. */}
      {actionStatus ? (
        <Alert severity={actionStatus === "success" ? "success" : "error"} sx={{ mb: 3 }}>
          {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
        </Alert>
      ) : null}

      {!result.ok ? (
        <Alert severity="error" sx={{ mb: 3 }}>
          {salesErrorText(result.error, copy(pageContract, "error.load"))}
        </Alert>
      ) : null}

      {/* Template user list: Card > status Tabs with Label counts > toolbar > filters result >
          table > pagination. The status tab is the `?status=` filter; only the shown tab has a
          count (the backend counts the whole filter, never every status at once). */}
      <Card>
        <AnimatedTabs
          ariaLabel={copy(pageContract, "filter.status")}
          value={statusValue}
          items={statusTabs.map((tab) => ({
            value: tab.value,
            label: tab.label,
            href: hrefWithQuery(pathname, sp, { status: tab.value || null, offset: null }),
            count: tab.value === statusValue ? total : undefined,
          }))}
          sx={{ px: { md: 2.5 } }}
        />

        <VendorFilterBar
          pageContract={pageContract}
          pathname={pathname}
          catalog={catalog}
          search={search}
          filters={activeFilters}
          tableId="procurement-vendors"
          exportName="vendors"
        />

        <ProcurementFiltersResult
          chips={chips}
          totalResults={total}
          clearHref={hrefWithQuery(pathname, sp, {
            search: null,
            offset: null,
            ...Object.fromEntries(FILTER_KEYS.filter((key) => key !== "status").map((key) => [key, null])),
          })}
        />

        <Box id="procurement-vendors" tabIndex={0} role="region" aria-label={copy(pageContract, "section.vendors.aria")}>
          <Scrollbar>
            <Table sx={{ minWidth: 800 }} aria-label={copy(pageContract, "section.vendors.aria")}>
              <TableHeadCustom headCells={headCells} />
              <TableBody>
                {vendors.map((vendor) => {
                  const drawerHref = hrefWithQuery(pathname, sp, { vendor: vendor.vendor_id });
                  return (
                    <TableRow key={vendor.vendor_id} hover>
                      <TableCell>
                        <Box sx={{ gap: 2, display: "flex", alignItems: "center" }}>
                          <Avatar alt={vendor.business_name}>{vendor.business_name.slice(0, 1).toUpperCase()}</Avatar>
                          <Stack sx={{ typography: "body2", flex: "1 1 auto", alignItems: "flex-start", minWidth: 0 }}>
                            <Link component={LocalOverlayLink} href={drawerHref} scroll={false} color="inherit" sx={{ cursor: "pointer" }}>
                              {vendor.business_name}
                            </Link>
                            <Box component="span" sx={{ color: "text.disabled" }}>
                              {vendor.contact_person_name || vendor.location_display || copy(pageContract, "value.none")}
                            </Box>
                          </Stack>
                        </Box>
                      </TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{vendor.record_type}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{vendor.phone_number ?? copy(pageContract, "value.none")}</TableCell>
                      <TableCell>{vendor.location_display}</TableCell>
                      <TableCell>
                        {/* Backend-owned label; the frontend chooses only the colour. */}
                        <Label variant="soft" color={statusColor(vendor.status)}>
                          {vendor.status_label}
                        </Label>
                      </TableCell>
                      <TableCell align="right">
                        <IconButton component={LocalOverlayLink} href={drawerHref} scroll={false} aria-label={`${copy(pageContract, "action.edit", "Edit")} ${vendor.business_name}`}>
                          <Iconify icon="solar:pen-bold" />
                        </IconButton>
                      </TableCell>
                    </TableRow>
                  );
                })}
                {vendors.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={headCells.length}>
                      <EmptyState sx={{ py: 10 }} title={hasAnyFilter ? copy(pageContract, "empty.vendors") : copy(pageContract, "empty.vendors.unset")} />
                    </TableCell>
                  </TableRow>
                ) : null}
              </TableBody>
            </Table>
          </Scrollbar>
        </Box>

        <ProcurementTableFooter
          denseLabel={copy(pageContract, "action.dense", "Dense")}
          rowsLabel={copy(pageContract, "pager.rows", "Rows")}
          page={pageNumber}
          pageCount={pageCount}
          rangeLabel={total ? `${offset + 1}–${offset + vendors.length} ${copy(pageContract, "pager.of")} ${total}` : `0 ${copy(pageContract, "summary.count")}`}
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
      </Card>

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
