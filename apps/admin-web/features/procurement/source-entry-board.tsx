import Table from "@mui/material/Table";
import Card from "@mui/material/Card";
import Avatar from "@mui/material/Avatar";
import ListItemText from "@mui/material/ListItemText";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getProcurementLoad, listProcurementLoads } from "@/lib/api/procurement-server";
import type { ProcurementLoad, ProcurementLoadDetail, ProcurementLoadStatus } from "@/lib/api/procurement";
import { boundedInt, hrefPreviousCursor, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { fmtDate, shortId } from "@/lib/format";
import { Tag, type Tone } from "@/components/ui-primitives";
import { actionFeedbackCopy, copy, optionGroup, optionLabel, optionTitle, optionTone, table, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { warmupMeta } from "./work-state";
import { NewLoadForm } from "./load-forms";
import { ProcurementPager } from "./pager";
import { SourceEntryLocalDrawer, type SourceEntryDrawerItem } from "./source-entry-local-drawer";
import { VaccinationFilterButton, VisibleTableSearch } from "@/features/preventive-care-vaccination";
import { PageHeader } from "@/components/app/page-header";
import { UrlTabs } from "@/components/app/url-tabs";
import { getProcurementOrigins } from "./load-detail";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import { UrlSuspense } from "@/components/app/url-suspense";
import { SourceLoadRowsSkeleton } from "./source-entry-skeletons";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table";
import { OrderTableToolbar } from "@/components/app/sections/order/order-table-toolbar";
import { orderToolbarFilterSx, orderToolbarSearchSx } from "@/components/app/order-toolbar-filter";
import { LinkFiltersResult, type LinkFilterChip } from "@/components/app/link-filters-result";
import { CELL_LINK, cellLinksSx, phoneLoadCardsSx } from "./procurement-sx";
import { LinkSelect } from "@/components/app/link-select";
import { SOURCE_LOAD_COLUMNS, SOURCE_LOAD_TAB_STATES, SOURCE_LOAD_TABLE_COLUMNS } from "./source-entry-layout";
import { PageRoot } from "@/components/app/page-root";

// Phone cards: load + status on the first line, holding farm under it, then the animal count
// (labelled -- the header row is hidden) beside the warmup reading. nth = the TABLE column order
// (SOURCE_LOAD_TABLE_COLUMNS): load, holding, animals, warmup, health / selection, status.
const LOAD_CARDS_SX = phoneLoadCardsSx("source-loads-table", [
  { nth: 1, column: "1", row: 1 },
  { nth: 6, column: "2", row: 1, alignEnd: true },
  { nth: 2, column: "1 / -1", row: 2, secondary: true },
  { nth: 3, column: "1", row: 3, secondary: true, labelled: true },
  { nth: 4, column: "2", row: 3, alignEnd: true },
]);

function daysSince(date: string | null | undefined): number | null {
  if (!date) return null;
  const start = new Date(`${date}T00:00:00Z`);
  if (Number.isNaN(start.getTime())) return null;
  const diff = Date.now() - start.getTime();
  return Math.max(0, Math.floor(diff / 86_400_000));
}

function contractTone(pageContract: AdminUiPageContract, groupId: string, key: string): Tone {
  return optionTone(pageContract, groupId, key) as Tone;
}

function warmupExpectationKey(purpose: string): string {
  if (purpose === "fattening" || purpose === "non_breeding" || purpose === "breeding") return purpose;
  return "unspecified";
}

function warmupCell(load: ProcurementLoad, detail: ProcurementLoadDetail | undefined, pageContract: AdminUiPageContract): { label: string; tone: Tone; note: string } {
  const goats = detail?.goats ?? [];
  const purposeValues = Array.from(new Set(goats.map((g) => g.purpose).filter(Boolean)));
  const goatDays = goats
    .map((g) => g.warmup_days)
    .filter((d): d is number => typeof d === "number");
  const days = goatDays.length > 0 ? Math.max(...goatDays) : daysSince(load.purchase_date);

  if (purposeValues.length > 1) {
    return {
      label: days === null ? copy(pageContract, "label.mixed_windows") : `${days}d · ${copy(pageContract, "label.mixed")}`,
      tone: "info",
      note: copy(pageContract, "warmup.mixed_note"),
    };
  }

  const purpose = purposeValues[0] ?? "unspecified";
  const warm = warmupMeta(days, purpose);
  const expectationKey = warmupExpectationKey(purpose);
  return {
    label: warm.label === "—" ? copy(pageContract, "label.placeholder") : `${warm.label} / ${optionLabel(pageContract, "warmup_expectations", expectationKey)}`,
    tone: warm.tone,
    note: optionTitle(pageContract, "warmup_expectations", expectationKey),
  };
}

function healthSelectionLabel(status: ProcurementLoadStatus, pageContract: AdminUiPageContract): { label: string; tone: Tone } {
  let key = "cleared_forward";
  switch (status) {
    case "source_warmup":
      key = "warming";
      break;
    case "health_pending":
      key = "health_pending";
      break;
    case "pre_dispatch_pending":
    case "dispatch_ready":
      key = "selection_ok";
      break;
    case "rejected":
    case "blocked":
      key = "blocked_rejected";
      break;
    case "deferred":
      key = "review";
      break;
  }
  return { label: optionLabel(pageContract, "health_selection_states", key), tone: contractTone(pageContract, "health_selection_states", key) };
}

function sourcePartyLabel(load: ProcurementLoad): string {
  return load.source_party_name || shortId(load.source_party_id);
}

function sourceLocationLabel(load: ProcurementLoad, pageContract: AdminUiPageContract): string {
  return load.source_location_name || load.source_location_code || copy(pageContract, "label.holding_not_set");
}

function hrefWithQuery(pathname: string, params: RouteSearchParams, changes: Record<string, string | null | undefined>): string {
  const next = new URLSearchParams();
  for (const [name, value] of Object.entries(params)) {
    if (Object.prototype.hasOwnProperty.call(changes, name)) continue;
    if (Array.isArray(value)) {
      for (const item of value) if (item) next.append(name, item);
    } else if (value) {
      next.set(name, value);
    }
  }
  for (const [name, value] of Object.entries(changes)) {
    next.delete(name);
    if (value && value !== "all") next.set(name, value);
  }
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

function purposeLabel(detail: ProcurementLoadDetail | undefined, pageContract: AdminUiPageContract): string {
  const purposes = Array.from(new Set((detail?.goats ?? []).map((g) => g.purpose).filter(Boolean)));
  const [first] = purposes;
  if (!first) return copy(pageContract, "label.placeholder");
  if (purposes.length === 1) return optionLabel(pageContract, "proc_purpose", first);
  return copy(pageContract, "label.mixed");
}

function taggingLabel(detail: ProcurementLoadDetail | undefined, expectedCount: number, pageContract: AdminUiPageContract): string {
  if (!detail) return copy(pageContract, "label.placeholder");
	const goats = detail?.goats ?? [];
	const tagged = goats.filter((g) => Boolean(g.animal_identifier_1 && g.animal_identifier_2)).length;
	return `${tagged}/${expectedCount}`;
}

function hfVaccinationLabel(detail: ProcurementLoadDetail | undefined, pageContract: AdminUiPageContract): { label: string; tone: Tone } {
  if (!detail) {
    return { label: copy(pageContract, "label.placeholder"), tone: "mut" };
  }
  const evidence = detail?.hf_vaccination_evidence ?? [];
  let key = "due";
  if (evidence.some((row) => row.review_status === "trusted")) key = "trusted";
  else if (evidence.some((row) => row.review_status === "imported")) key = "imported";
  if (evidence.some((row) => row.review_status === "rejected" || row.review_status === "conflicting")) {
    key = "flagged";
  }
  return { label: optionLabel(pageContract, "warmup_evidence_states", key), tone: contractTone(pageContract, "warmup_evidence_states", key) };
}

export async function SourceEntryBoardPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const pathname = "/procurement/source-entry";
  const sourceLoadStatuses = optionGroup(pageContract, "source_load_status");
  const sourceLoadStatusOrder = sourceLoadStatuses.map((status) => status.key as ProcurementLoadStatus);
  const statusFilter = (sourceLoadStatusOrder.find((s) => s === one(sp, "status")) ?? "all") as ProcurementLoadStatus | "all";
  const cursor = one(sp, "cursor");
  const page = boundedInt(one(sp, "page"), 1, 1, 1_000_000);
  // Rows per page (TR2 P1-6): the contract's page sizes, chosen in the URL (`limit`), default the
  // largest so the board keeps showing as many loads as before on its first page. The read stays
  // cursor-paged; a size change drops the cursor chain so the footer range is exact.
  const pageSizes = sourceLoadPageSizes(pageContract);
  const requestedLimit = Number(one(sp, "limit"));
  const PAGE_SIZE = pageSizes.includes(requestedLimit) ? requestedLimit : pageSizes[pageSizes.length - 1];
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const selectedLoadId = one(sp, "source_load");

  const [result, origins] = await Promise.all([
    listProcurementLoads({
      status: statusFilter === "all" ? undefined : statusFilter,
      limit: PAGE_SIZE,
      cursor,
    }),
    getProcurementOrigins(),
  ]);
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  // Sort the returned page by the canonical stage order so the board reads source-side -> intake.
  const loads: ProcurementLoad[] = result.ok
    ? [...result.data.items].sort((a, b) => sourceLoadStatusOrder.indexOf(a.status) - sourceLoadStatusOrder.indexOf(b.status))
    : [];
  // request-plan:ignore owner=procurement-platform issue=C35-016 expires=2026-09-30 reason=list contract lacks card facets; replace with enriched paged list or batch detail API
  const detailByLoad = new Map<string, ProcurementLoadDetail>();
  if (selectedLoadId && loads.some((load) => load.load_id === selectedLoadId)) {
    const selectedDetail = await getProcurementLoad(selectedLoadId);
    if (selectedDetail.ok) detailByLoad.set(selectedLoadId, selectedDetail.data.detail);
  }
  const suppliers = Array.from(
    new Map(loads.filter((load) => load.source_party_name).map((load) => [load.source_party_id, { id: load.source_party_id, name: load.source_party_name as string }])).values(),
  ).sort((a, b) => a.name.localeCompare(b.name));
  const nextCursor = result.ok ? result.data.next_cursor ?? null : null;
  const nextHref = hrefWithCursor(pathname, sp, nextCursor);
  const prevHref = hrefPreviousCursor(pathname, sp);
  const loadLabels = tableLabels(pageContract, "source-loads");
  // The table shows the columns the list read answers (source-entry-layout.ts); the drawer keeps
  // every contract label, indexed in contract order.
  const tableLabelsShown = loadLabels.filter((_, index) => {
    const key = SOURCE_LOAD_COLUMNS[index];
    return key === undefined || (SOURCE_LOAD_TABLE_COLUMNS as readonly string[]).includes(key);
  });
  const animalsLabel = loadLabels[SOURCE_LOAD_COLUMNS.indexOf("animals")] ?? "";
  const closeDrawerHref = hrefWithQuery(pathname, sp, { source_load: null });
  const drawerItems: SourceEntryDrawerItem[] = loads.map((load) => {
    const detail = detailByLoad.get(load.load_id);
    return {
      id: load.load_id,
      sourceLocation: sourceLocationLabel(load, pageContract),
      sourceParty: sourcePartyLabel(load),
      purpose: purposeLabel(detail, pageContract),
      expectedCount: load.expected_count,
      goatsInLoad: detail ? String((detail.goats ?? []).length) : copy(pageContract, "label.placeholder"),
      warmup: warmupCell(load, detail, pageContract),
      tagging: taggingLabel(detail, load.expected_count, pageContract),
      hfVaccination: hfVaccinationLabel(detail, pageContract),
      healthSelection: healthSelectionLabel(load.status, pageContract),
      status: {
        label: optionLabel(pageContract, "source_load_status", load.status),
        tone: contractTone(pageContract, "source_load_status", load.status),
      },
      detailHref: hrefWithQuery(`/procurement/source-entry/loads/${encodeURIComponent(load.load_id)}`, sp, {
        source_load: null,
        cursor: null,
        cursor_stack: null,
        page: null,
      }),
    };
  });

  // Status filter resets the cursor/page (a new filter starts a fresh first page).
  function statusHref(status: ProcurementLoadStatus | "all"): string {
    return hrefWithQuery(pathname, sp, {
      status: status === "all" ? null : status,
      cursor: null,
      cursor_stack: null,
      page: null,
      source_load: null,
    });
  }

  const placeholder = copy(pageContract, "label.placeholder");
  const statusChips: LinkFilterChip[] =
    statusFilter === "all"
      ? []
      : [{ id: "status", label: `${loadLabels[loadLabels.length - 1]}:`, value: optionLabel(pageContract, "source_load_status", statusFilter), href: statusHref("all") }];

  return (
    <PageRoot>
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
        actions={<NewLoadForm returnTo={hrefWithQuery(pathname, sp, { source_load: null })} pageContract={pageContract} origins={origins} suppliers={suppliers} />}
      />

      {actionStatus ? (
        <Alert severity={actionStatus === "success" ? "success" : "error"} sx={{ mb: 3 }}>
          {actionStatus === "success" ? null : <b>{copy(pageContract, "action.failed_title")}&nbsp;</b>}
          {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
        </Alert>
      ) : null}

      {!result.ok ? (
        <Alert severity="error" sx={{ mb: 3 }}>
          {result.error.message}
        </Alert>
      ) : null}

      {/* Template order list (sections/order/view/order-list-view): one Card holding the status
          Tabs (Label count on the active tab — the backend issues no whole-filter count per stage,
          so only the shown stage can be counted honestly), the OrderTableToolbar, the filters
          result, the Scrollbar table under TableHeadCustom and the pager. The status tabs drive a
          server navigation; the kit TabPanel is NOT wrapped around the card (it branches on
          useReducedMotion(), which differs server/client and breaks hydration). */}
      <Card data-filter-scope="">
        {/* Template order list: All + 4 in-flight stages as tabs (the template's 5, so the strip fits
            the card with no scroll arrows); EVERY stage stays reachable through the status select in
            the toolbar. Both drive the same `status` param. guard: source-entry-tabs-fit */}
        <UrlTabs
          ariaLabel={copy(pageContract, "filter.all_states")}
          value={statusFilter}
          items={[
            { value: "all", label: copy(pageContract, "filter.all_states"), href: statusHref("all"), count: statusFilter === "all" ? loads.length : undefined },
            ...sourceLoadStatuses
              .filter((status) => (SOURCE_LOAD_TAB_STATES as readonly string[]).includes(status.key))
              .map((status) => ({
                value: status.key,
                label: status.label,
                href: statusHref(status.key as ProcurementLoadStatus),
                count: statusFilter === status.key ? loads.length : undefined,
              })),
          ]}
        />

        <OrderTableToolbar
          filters={
            <Box sx={SOURCE_STATUS_SELECT_SX}>
              <LinkSelect
                label={loadLabels[loadLabels.length - 1]}
                value={statusFilter}
                options={[
                  { value: "all", label: copy(pageContract, "filter.all_states"), href: statusHref("all") },
                  ...sourceLoadStatuses.map((status) => ({ value: status.key, label: status.label, href: statusHref(status.key as ProcurementLoadStatus) })),
                ]}
              />
            </Box>
          }
          search={<Box sx={orderToolbarSearchSx}><Box sx={{ display: "flex" }}>
              <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.search_label")} />
            </Box></Box>}
          trailing={
            <VaccinationFilterButton
              pageContract={pageContract}
              title={copy(pageContract, "filter.drawer.title")}
              searchReason={copy(pageContract, "filter.search_reason")}
              filterReason={copy(pageContract, "filter.reason")}
              rowsLabel={`${loads.length} ${copy(pageContract, "label.rows")} · ${copy(pageContract, "filter.rows_suffix")}`}
              facets={loadLabels}
            />
          }
        />

        <LinkFiltersResult totalResults={loads.length} chips={statusChips} resetHref={statusHref("all")} />

        {/* The loads (guard: url-keyed-panel): a stage tab / page click swaps them to their skeleton at
            once; tabs, toolbar and chips stay on screen. The load drawer param never suspends it. */}
        <UrlSuspense searchParams={sp} watch={LOADS_WATCH} fallback={<SourceLoadRowsSkeleton columns={Math.max(tableLabelsShown.length, 1)} />}>
        <Box sx={LOAD_CARDS_SX} role="group" aria-label={copy(pageContract, "section.loads.aria")}>
          <Scrollbar>
            <Table className="source-loads-table" sx={SOURCE_LOADS_TABLE_SX}>
              <TableHeadCustom headCells={tableLabelsShown.map((label, index) => ({ id: `c${index}`, label, sortable: false }))} />
              <TableBody>
                {loads.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={tableLabelsShown.length} sx={{ py: 5, textAlign: "center", color: "text.secondary", typography: "body2" }}>
                      {result.ok
                        ? statusFilter === "all"
                          ? copy(pageContract, "empty.loads_detail")
                          : `${copy(pageContract, "empty.loads_filtered_prefix")} “${optionLabel(pageContract, "source_load_status", statusFilter as ProcurementLoadStatus)}” ${copy(pageContract, "empty.loads_filtered_suffix")}`
                        : copy(pageContract, "empty.unavailable")}
                    </TableCell>
                  </TableRow>
                ) : (
                  loads.map((load) => {
                    const drawerHref = hrefWithQuery(pathname, sp, { source_load: load.load_id });
                    const healthSelection = healthSelectionLabel(load.status, pageContract);
                    const detail = detailByLoad.get(load.load_id);
                    const warmup = warmupCell(load, detail, pageContract);
                    return (
                      <TableRow key={load.load_id} hover>
                        <TableCell>
                          <LocalOverlayLink href={drawerHref} className={CELL_LINK} scroll={false}>
                            <Box sx={{ gap: 2, display: "flex", alignItems: "center" }}>
                              <Avatar alt={sourcePartyLabel(load)}>{sourcePartyLabel(load).slice(0, 1).toUpperCase()}</Avatar>
                              <ListItemText
                                primary={sourcePartyLabel(load)}
                                secondary={load.purchase_date ? fmtDate(load.purchase_date) : undefined}
                                slotProps={{ primary: { sx: { typography: "body2" } }, secondary: { sx: { color: "text.disabled" } } }}
                              />
                            </Box>
                          </LocalOverlayLink>
                        </TableCell>
                        <TableCell>
                          <LocalOverlayLink href={drawerHref} className={CELL_LINK} scroll={false}>
                            {sourceLocationLabel(load, pageContract)}
                          </LocalOverlayLink>
                        </TableCell>
                        <TableCell align="center">
                          <LocalOverlayLink href={drawerHref} className={CELL_LINK} scroll={false} data-label={animalsLabel}>
                            {load.expected_count}
                          </LocalOverlayLink>
                        </TableCell>
                        <TableCell>
                          <LocalOverlayLink href={drawerHref} className={CELL_LINK} scroll={false}>
                            <ListItemText
                              primary={warmup.label === placeholder ? null : <Tag tone={warmup.tone}>{warmup.label}</Tag>}
                              secondary={load.purchase_date ? `${copy(pageContract, "label.from_date_prefix")} ${fmtDate(load.purchase_date)}` : copy(pageContract, "label.purchase_date_missing")}
                              title={warmup.note}
                              slotProps={{ secondary: { sx: { mt: 0.5, typography: "caption" } } }}
                            />
                          </LocalOverlayLink>
                        </TableCell>
                        <TableCell>
                          <LocalOverlayLink href={drawerHref} className={CELL_LINK} scroll={false}>
                            <Tag tone={healthSelection.tone}>{healthSelection.label}</Tag>
                          </LocalOverlayLink>
                        </TableCell>
                        <TableCell>
                          <LocalOverlayLink href={drawerHref} className={CELL_LINK} scroll={false}>
                            <Tag tone={contractTone(pageContract, "source_load_status", load.status)}>{optionLabel(pageContract, "source_load_status", load.status)}</Tag>
                          </LocalOverlayLink>
                        </TableCell>
                      </TableRow>
                    );
                  })
                )}
              </TableBody>
            </Table>
          </Scrollbar>
        </Box>
        <ProcurementPager
          prevHref={prevHref}
          nextHref={nextHref}
          page={page}
          count={loads.length}
          noun={loadLabels[0].toLowerCase()}
          forceVisible
          dense
          rowsPerPage={PAGE_SIZE}
          rowsPerPageHrefs={pageSizes.map((size) => ({ value: size, href: hrefWithQuery(pathname, sp, { limit: String(size), cursor: null, cursor_stack: null, page: null, source_load: null }) }))}
          labelRowsPerPage={copy(pageContract, "pager.rows_per_page")}
        />
        </UrlSuspense>
      </Card>
      <SourceEntryLocalDrawer
        items={drawerItems}
        initialSelectedId={selectedLoadId}
        closeHref={closeDrawerHref}
        loadLabels={loadLabels}
        pageContract={pageContract}
      />
    </PageRoot>
  );
}

/** The params the loads read takes. */
const LOADS_WATCH = ["status", "cursor", "cursor_stack", "page", "limit"] as const;

/** The contract's rows-per-page choices for the loads table (template 5/10/25/50). */
function sourceLoadPageSizes(pageContract: AdminUiPageContract): number[] {
  const options = pageContract.tables.some((item) => item.id === "source-loads") ? table(pageContract, "source-loads").page_size_options : [];
  return options.length > 0 ? [...options].sort((a, b) => a - b) : [50];
}

/**
 * The status select: the template toolbar's leading field (full width on a phone). The stage names
 * ("Pre-dispatch pending", "Arrival review") do not fit the template's 160px field, which ellipsised
 * the chosen value ("Health p…"); from md it takes 15rem (it still shrinks before the search).
 */
const SOURCE_STATUS_SELECT_SX = { ...orderToolbarFilterSx, flex: { md: "0 1 15rem" }, display: "flex", "& > .MuiTextField-root": { flex: 1, minWidth: 0 } } as const;

/**
 * The loads table (TR3-P1-2): headings stay on ONE line (the template TableHeadCustom nowrap; no
 * override here) and the table scrolls inside the card's template Scrollbar when it is wider than the
 * card, as the template order list does. Cell readings stay on one line; the load's party name and
 * its holding farm / supplier wrap at word breaks. Status Tags stay whole. No row chevron: the
 * template order list has none (the whole row is the drawer link, with the hover row).
 */
const SOURCE_LOADS_TABLE_SX = {
  minWidth: 960,
  ...cellLinksSx,
  "& tbody td .cell-link, & tbody td .MuiListItemText-root": { whiteSpace: "nowrap" },
  "& tbody td:first-of-type .MuiListItemText-primary": { whiteSpace: "normal", minWidth: 120 },
  "& tbody td:nth-of-type(2) .cell-link": { whiteSpace: "normal", display: "block", minWidth: 88 },
  "& .minimal__label__root, & .MuiChip-root": { whiteSpace: "nowrap" },
} as const;
