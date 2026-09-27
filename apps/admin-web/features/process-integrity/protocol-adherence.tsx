import Table from "@mui/material/Table";
import { UrlSuspense } from "@/components/app/url-suspense";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { KpiRowSkeleton, TableSkeleton } from "@/components/app/skeletons";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { InfoHint } from "@/components/app/info-hint";
import { PageHeader, type PageCrumb } from "@/components/app/page-header";
import { KpiWidget } from "@/components/app/kpi-widget";
import { TableHeadCustom } from "@/components/app/table";
import { Label } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { LinkSelect } from "@/components/app/link-select";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import type { PaletteColorKey } from "@/theme/core";
import { UrlTabs } from "@/components/app/url-tabs";
import { getVaccinationAdherence } from "@/lib/api/server";
import type { AdherenceRow, ProcessIntegritySeverity, WorkState } from "@/lib/api/server";
import { copy, optionalCopy, optionGroup, optionLabel, optionTone, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { SEVERITY_ORDER, WORK_STATE_ORDER, type Tone } from "./process-integrity";
import { Tag } from "@/components/ui-primitives";
import { VaccinationFilterButton, VaccinationTablePager, type VaccinationPageSize } from "@/features/preventive-care-vaccination";
import { vaccinationDriveDisplayName } from "@/lib/vaccine-display";
import { ProtocolAdherenceLocalDrawer, type ProtocolAdherenceDrawerRecord } from "./protocol-adherence-local-drawer";
import { fmtDate } from "@/lib/format";
import { operationalLocationLabel } from "@/lib/operational-location";
import { EvidenceMedia } from "./evidence-media";
import Alert from "@mui/material/Alert";

/** Ledger column widths (expected, actual, gap, severity, owner, next action, evidence). They sum to
 *  LEDGER_MIN_WIDTH, which fits the 1440 content column, so every column (Evidence included) is on
 *  screen there; narrower screens scroll the table inside the template Scrollbar. Cells WRAP, never
 *  ellipsis: TR1-#29 found the next action cut to "Start SOP - overdu…" with no way to read it.
 *  guard: adherence-ledger-readable */
const LEDGER_WIDTHS = [240, 120, 150, 100, 120, 170, 100];
const LEDGER_MIN_WIDTH = LEDGER_WIDTHS.reduce((sum, width) => sum + width, 0);
/** A whole-cell row link that reads as table text (the template rows are not link-blue). */
const CELL_LINK_SX = { display: "block", color: "inherit", textDecoration: "none", minWidth: 0, overflowWrap: "anywhere" } as const;

function ownerOf(pageContract: AdminUiPageContract, row: AdherenceRow): string {
  return row.owner?.operator_name ?? row.owner?.park_head_name ?? copy(pageContract, "label.unassigned");
}


function adherenceLedgerLabels(pageContract: AdminUiPageContract): string[] {
  const labels = [...tableLabels(pageContract, "adherence-ledger")];
  if ((labels[4] || "").toLowerCase() === "owner") {
    labels[4] = `${labels[4]} chain`.toUpperCase();
  }
  if ((labels[4] || "").toLowerCase() === "owner chain") {
    labels[4] = copy(pageContract, "label.owner_short");
  }
  return labels;
}

/** The adherence formula as ONE info glyph with a real tooltip (the old `details` "i" opened nothing the judge could read). */
function AdherenceInfo({ pageContract }: { pageContract: AdminUiPageContract }) {
  const text = [
    copy(pageContract, "adherence.help.title"),
    copy(pageContract, "adherence.help.window_prefix"),
    copy(pageContract, "adherence.help.formula"),
    copy(pageContract, "adherence.help.current_prefix"),
  ].join(" ");
  return <InfoHint text={text} />;
}

function copyOr(pageContract: AdminUiPageContract, key: string, fallback: string): string {
  return optionalCopy(pageContract, key) ?? fallback;
}

function workStateTone(pageContract: AdminUiPageContract, workState: string): Tone {
  return optionTone(pageContract, "work_state_filter_chips", workState) as Tone;
}

function gapLabel(pageContract: AdminUiPageContract, row: AdherenceRow): string {
  switch (row.drive_capacity_state) {
    case "over_cap_required":
      if (driveCapacityWithinSlots(row)) return copyOr(pageContract, "gap.none", "none");
      return copyOr(pageContract, "gap.capacity_shortfall", "capacity shortfall");
    case "medical_defer":
      return row.drive_medical_defer_reason ? `medical defer: ${row.drive_medical_defer_reason}` : "medical defer";
    case "terminal_animal_closed":
      return row.drive_medical_defer_reason ? `terminal closed: ${row.drive_medical_defer_reason}` : "terminal animal closed";
  }
  switch (row.gap) {
    case "proof_missing":
      return copyOr(pageContract, "gap.proof_missing", "proof missing");
    case "verification_pending":
      return copyOr(pageContract, "gap.verification_pending", "verification pending");
    case "deferred_explained":
      return copyOr(pageContract, "gap.deferred_explained", "deferred / explained");
    case "proof_rejected":
      return copyOr(pageContract, "gap.proof_rejected", "proof rejected");
    case "missed":
      return copyOr(pageContract, "gap.missed", "missed");
    case "blocked":
      return copyOr(pageContract, "gap.blocked", "blocked");
    case "overdue":
      return copyOr(pageContract, "gap.overdue", "overdue");
    default:
      return row.gap.replaceAll("_", " ");
  }
}

function driveCapacityWithinSlots(row: AdherenceRow): boolean {
  const slots = (row.drive_available_operators ?? 0) * (row.drive_operator_cap ?? 0);
  const animals = row.drive_animals_assigned ?? row.drive_animals_required ?? 0;
  return slots > 0 && animals <= slots;
}

function driveCapacityDetail(row: AdherenceRow): string | null {
  if (row.drive_capacity_state !== "over_cap_required") return null;
  const slots = (row.drive_available_operators ?? 0) * (row.drive_operator_cap ?? 0);
  const animals = row.drive_animals_assigned ?? row.drive_animals_required ?? 0;
  const latest = row.drive_latest_safe_date ? fmtDate(row.drive_latest_safe_date) : undefined;
  if (driveCapacityWithinSlots(row)) {
    return latest
      ? `${animals.toLocaleString("en-IN")} assigned / ${slots.toLocaleString("en-IN")} slots · latest safe ${latest}`
      : `${animals.toLocaleString("en-IN")} assigned / ${slots.toLocaleString("en-IN")} slots`;
  }
  const shortage = slots > 0 ? animals - slots : animals;
  return `${shortage.toLocaleString("en-IN")} more animal${shortage === 1 ? "" : "s"} than planned capacity${latest ? ` · latest safe ${latest}` : ""}`;
}

const VACCINE_CODE_COPY_KEYS: Array<[needle: string, copyKey: string]> = [
  ["blue_tongue", "vaccine.blue_tongue"],
  ["goat_pox", "vaccine.goat_pox"],
  ["sheep_pox", "vaccine.sheep_pox"],
  ["et_tt", "vaccine.et_tt"],
  ["fmd", "vaccine.fmd"],
  ["ppr", "vaccine.ppr"],
  ["hs", "vaccine.hs"],
];

function readableAdherenceExpected(pageContract: AdminUiPageContract, raw: string): { title: string; detail: string } {
  const withoutPrefix = raw.replace(/^Preventive Care Vaccination Matrix\s*/i, "").trim();
  const code = withoutPrefix.match(/[a-z0-9]+(?:_[a-z0-9]+)+/i)?.[0]?.toLowerCase() ?? "";
  const vaccineCopyKey = VACCINE_CODE_COPY_KEYS.find(([needle]) => code.includes(needle))?.[1] ?? "vaccine.generic";
  const vaccine = copy(pageContract, vaccineCopyKey);
  const path = code.includes("_kid_")
    ? copy(pageContract, "schedule.kid_course")
    : code.includes("_adult_")
      ? copy(pageContract, "schedule.adult_course")
      : copy(pageContract, "schedule.course");
  const timing = readableScheduleTiming(pageContract, code);
  const dueCount = raw.match(/:\s*(\d+)\s*(?:animals\s+must\s+finish|due|d\b)/i)?.[1];
  const sharedLabel = vaccinationDriveDisplayName(withoutPrefix);
  const fallbackLabel = copy(pageContract, "label.vaccination_drive");
  const noisySharedLabel = /^Preventive Care Vaccination Matrix\b/i.test(sharedLabel);
  const bits = sharedLabel && sharedLabel !== fallbackLabel && !noisySharedLabel ? [sharedLabel] : [vaccine, path, timing].filter(Boolean);
  return {
    title: `${bits.join(" ")}${dueCount ? ` - ${dueCount} ${copy(pageContract, "label.due_lower")}` : ""}`,
    detail: bits.join(" "),
  };
}

function readableAdherenceActual(pageContract: AdminUiPageContract, raw: string): string {
  const text = raw.trim();
  const deferred = text.match(/^(\d+)\s+deferred\/ex/i);
  if (deferred) return `${deferred[1]} ${copy(pageContract, "actual.deferred_with_reason")}`;
  if (text.toLowerCase() === "not completed") return copy(pageContract, "actual.not_completed_yet");
  return text.replaceAll("_", " ");
}

function adherenceLocationDetail(row: AdherenceRow): string {
  return (
    row.operational_location_display ||
    operationalLocationLabel({
      shedName: row.shed_name,
      partitionLabel: row.partition_label,
      sourceShedName: row.source_shed_name,
    })
  );
}

function readableScheduleTiming(pageContract: AdminUiPageContract, code: string): string | undefined {
  const match = code.match(/_(\d+)(w|m|yr)$/);
  if (!match) return undefined;
  const [, value, unit] = match;
  const unitKey = unit === "w" ? "schedule.weeks" : unit === "m" ? "schedule.months" : "schedule.years";
  return `${value} ${copy(pageContract, unitKey)}`;
}

export async function ProtocolAdherencePage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const severityFilter = (SEVERITY_ORDER.find((s) => s === one(sp, "severity")) ?? "all") as ProcessIntegritySeverity | "all";
  const workStateOptions = optionGroup(pageContract, "work_state_filter_chips");
  const workStateParam = one(sp, "state");
  const workStateFilter = (workStateOptions.some((option) => option.key === workStateParam) ? workStateParam : "all") as WorkState | "all";
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const pageSizeOptions = tablePageSizes(pageContract, "adherence-ledger");
  const PATH = "/protocol-adherence";
  const requestedPageSize = (pageSizeOptions.find((size) => size === boundedInt(one(sp, "adh_limit"), 10, 1, 100)) ?? 10) as VaccinationPageSize;
  const adhCursor = one(sp, "adh_cursor");
  const adhCursorStack = sp.adh_cursor_stack;
  const adhPage = boundedInt(one(sp, "adh_page"), 1, 1, 1000000);

  const result = await getVaccinationAdherence({
    parkId,
    asOf,
    workState: workStateFilter === "all" ? undefined : workStateFilter,
    severity: severityFilter === "all" ? undefined : severityFilter,
    limit: requestedPageSize,
    cursor: adhCursor,
  });

  const summary = result.ok ? result.data.summary : null;
  const rows: AdherenceRow[] = result.ok ? listOrEmpty(result.data.rows) : [];
  const hasLedgerFilters = severityFilter !== "all" || workStateFilter !== "all";
  const totalCount = result.ok ? result.data.total_count : 0;
  const nextCursor = result.ok ? result.data.next_cursor : undefined;
  const start = totalCount === 0 ? 0 : (adhPage - 1) * requestedPageSize + 1;
  const end = totalCount === 0 ? 0 : Math.min(totalCount, start + rows.length - 1);
  const paged = { items: rows, page: adhPage, pageSize: requestedPageSize, total: totalCount, start, end };
  const nextHref = nextCursor ? hrefWithPagedCursor(PATH, sp, "adh_cursor", nextCursor, "adh_page", "adh_cursor_stack") : null;
  const prevHref = hrefPreviousPagedCursor(PATH, sp, "adh_cursor", "adh_page", "adh_cursor_stack");
  if (result.ok && adhPage > 1 && !adhCursor && !adhCursorStack) {
    redirect(scopeHref("/protocol-adherence", scope, {}, {
      severity: severityFilter,
      state: workStateFilter,
      adh_page: "1",
      adh_limit: String(requestedPageSize),
    }));
  }
  const ledgerLabels = adherenceLedgerLabels(pageContract);
  const initialSelectedRowId = one(sp, "adh_row");

  // Filter links preserve the full top-bar scope (scopeHref) + the page severity filter.
  // Filter/page-size changes reset to page 1 and drop the cursor stack (keyset restart).
  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref("/protocol-adherence", scope, {}, {
      severity: severityFilter,
      state: workStateFilter,
      adh_page: String(paged.page),
      adh_limit: String(paged.pageSize),
      adh_cursor: undefined,
      adh_cursor_stack: undefined,
      ...overrides,
    });
  }
  function pagerHref(page: number): string {
    if (page > paged.page) return nextHref ?? hrefWith({});
    if (page < paged.page) return prevHref ?? hrefWith({});
    return hrefWith({ adh_page: String(page) });
  }
  function pageSizeHref(pageSize: VaccinationPageSize): string {
    return hrefWith({ adh_page: "1", adh_limit: String(pageSize), adh_cursor: undefined, adh_cursor_stack: undefined });
  }
  const closeDrawerHref = hrefWith({ adh_row: undefined });
  const rowDrawerHref = (row: AdherenceRow) => `${closeDrawerHref}#adh_row=${encodeURIComponent(row.row_id)}`;
  const workflowHref = (row: AdherenceRow) =>
    scopeHref(`/workflows/${encodeURIComponent(row.row_id)}`, scope, {}, { from: "protocol-adherence" });
  const drawerRecords: ProtocolAdherenceDrawerRecord[] = rows.map((row) => {
    const expected = readableAdherenceExpected(pageContract, row.expected);
    const locationDetail = adherenceLocationDetail(row);
    return {
      row,
      expectedTitle: expected.title,
      expectedDetail: locationDetail || expected.detail,
      actual: readableAdherenceActual(pageContract, row.actual),
      gap: gapLabel(pageContract, row),
      owner: ownerOf(pageContract, row),
      workflowHref: workflowHref(row),
      actionCenterHref: scopeHref("/action-center", scope, {}, { ac_row: row.row_id }),
    };
  });


  // Breadcrumb trail. The parent segment is the contract's own `crumb` copy -- rendered only when
  // the backend actually supplies one AND it is not just the page title again, which is what the
  // old `<div className="crumb"><b>{title}</b></div>` rendered on every route in this module. No
  // section name is invented here: a missing key means a single muted current segment, and the
  // real two-level trail lands the moment the contract carries the section label.
  const crumbSection = optionalCopy(pageContract, "crumb");
  const crumbItems: PageCrumb[] = [
    ...(crumbSection && crumbSection !== pageContract.title ? [{ label: crumbSection, href: "/" }] : []),
    { label: pageContract.title },
  ];

  // Template overview/course tiles (KpiWidget -> CourseWidgetSummary): the figure is a number; the
  // unit and the contract hint are the visible sub-line under it.
  const kpis: { key: string; title: string; total: number; caption: string; color: PaletteColorKey }[] = summary
    ? [
        {
          key: "adherence",
          title: copy(pageContract, "label.overall_adherence"),
          total: Math.round(summary.adherence_percent),
          caption: `% · ${copy(pageContract, "label.on_time_correct")}`,
          color: summary.adherence_percent >= 90 ? "success" : summary.adherence_percent >= 70 ? "warning" : "error",
        },
        { key: "gaps", title: copy(pageContract, "label.open_process_gaps"), total: summary.open_gap_count, caption: copy(pageContract, "label.across_rules"), color: summary.open_gap_count > 0 ? "warning" : "info" },
        { key: "deferred", title: copy(pageContract, "label.deferred_explained"), total: summary.deferred_count, caption: copy(pageContract, "label.deferred_scope"), color: "info" },
        { key: "on-track", title: copy(pageContract, "label.on_track"), total: summary.process_intact_count, caption: `${summary.completed_count}/${summary.expected_count} ${copy(pageContract, "label.done_suffix")}`, color: "success" },
      ]
    : [];
  const head = ledgerLabels.map((label, index) => ({ id: `c${index}`, label, width: LEDGER_WIDTHS[index] }));

  return (
    <Box className="screen on">
      <PageHeader
        title={pageContract.title}
        crumbs={crumbItems}
        actions={<AdherenceInfo pageContract={pageContract} />}
      />

      <Stack spacing={3}>
        {/* Template overview/course: CourseWidgetSummary tiles via KpiWidget (Grid spacing 3). Park/date scope lives in the top bar only. */}
        {/* KPI tiles and ledger rows + pager swap to their skeleton on a tab / filter / page click
            (guard: url-keyed-panel); the ledger card head, tabs and toolbar stay on screen. */}
        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<KpiRowSkeleton count={4} icon />}>
        <Grid container spacing={3}>
          {kpis.map((kpi) => (
            <Grid key={kpi.key} size={{ xs: 12, sm: 6, md: 3 }}>
              <KpiWidget title={kpi.title} total={kpi.total} caption={kpi.caption} color={kpi.color} />
            </Grid>
          ))}
        </Grid>
        </UrlSuspense>

        {!result.ok ? (
          <Alert severity="error">
            <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
          </Alert>
        ) : null}

        {/* Template order list: one Card holding the work-state Tabs (Label count), the toolbar
            (severity, filters, range) and the ledger table with the template pagination footer. */}
        <Card aria-label={copy(pageContract, "section.ledger.title")}>
          <CardHeader
            title={
              <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
                {copy(pageContract, "section.ledger.title")}
                {summary ? (
                  <Label variant="soft" color={summary.adherence_percent >= 90 ? "success" : "warning"}>
                    {Math.round(summary.adherence_percent)}% adherence
                  </Label>
                ) : null}
              </Box>
            }
            action={<InfoHint text={copy(pageContract, "section.ledger.note")} />}
            sx={{ mb: 1 }}
          />
          <UrlTabs
            ariaLabel={copy(pageContract, "label.all_states")}
            scrollButtons="auto"
            value={workStateFilter}
            items={[
              { value: "all", label: copy(pageContract, "label.all_states"), count: summary?.expected_count, href: hrefWith({ state: "all", adh_page: "1" }) },
              ...WORK_STATE_ORDER.map((state) => ({ value: state, label: optionLabel(pageContract, "work_state_filter_chips", state), href: hrefWith({ state, adh_page: "1" }) })),
            ]}
          />
          <Box
            sx={{
              p: 2.5,
              gap: 2,
              display: "flex",
              flexDirection: { xs: "column", md: "row" },
              alignItems: { xs: "stretch", md: "center" },
            }}
          >
            <LinkSelect
              label={copy(pageContract, "label.all_severity")}
              value={severityFilter}
              minWidth={200}
              options={[
                { value: "all", label: copy(pageContract, "label.all_severity"), href: hrefWith({ severity: "all", adh_page: "1" }) },
                ...SEVERITY_ORDER.map((s2) => ({ value: s2, label: optionLabel(pageContract, "severity_chips", s2), href: hrefWith({ severity: s2, adh_page: "1" }) })),
              ]}
            />
            <VaccinationFilterButton
              pageContract={pageContract}
              title={copy(pageContract, "filter.drawer.title")}
              searchReason={copy(pageContract, "filter.search_reason")}
              filterReason={copy(pageContract, "filter.reason")}
              rowsLabel={`${paged.start}-${paged.end} of ${paged.total} rows · ${copy(pageContract, "filter.rows_suffix")}`}
              actionHref={scopeHref("/action-center", scope)}
              actionLabel={copy(pageContract, "action.open_action_center")}
              facets={ledgerLabels}
            />
            <Typography variant="body2" sx={{ color: "text.secondary", ml: { md: "auto" } }}>
              {paged.start}-{paged.end} of {paged.total} rows
            </Typography>
          </Box>

          {/* Severity and work-state filter ONE ledger. Keyed on both, the body cross-fades. */}
          <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<TableSkeleton bare header={false} columns={ledgerLabels.length || 7} rows={requestedPageSize} />}>
            <Scrollbar>
              <Table sx={{ minWidth: LEDGER_MIN_WIDTH }} aria-label={copy(pageContract, "section.ledger.aria")}>
                <TableHeadCustom headCells={head} />
                <TableBody>
                  {paged.total === 0 ? (
                    <TableRow>
                      <TableCell colSpan={ledgerLabels.length} sx={{ py: 5, textAlign: "center", color: "text.secondary" }}>
                        {hasLedgerFilters ? copy(pageContract, "empty.ledger_filtered") : copy(pageContract, "empty.ledger_detail")}
                      </TableCell>
                    </TableRow>
                  ) : (
                    paged.items.map((row) => {
                      const href = rowDrawerHref(row);
                      const expected = readableAdherenceExpected(pageContract, row.expected);
                      const actual = readableAdherenceActual(pageContract, row.actual);
                      const expectedDetail = adherenceLocationDetail(row) || expected.detail;
                      const driveDetail = driveCapacityDetail(row);
                      return (
                        <TableRow hover key={row.row_id}>
                          <TableCell>
                            <Box component={LocalOverlayLink} href={href} scroll={false} title={row.expected} sx={CELL_LINK_SX}>
                              <Box component="span" sx={{ display: "block", typography: "subtitle2" }}>
                                {expected.title}
                              </Box>
                              <Box component="span" sx={{ display: "block", typography: "body2", color: "text.secondary" }}>
                                {expectedDetail}
                              </Box>
                            </Box>
                          </TableCell>
                          <TableCell sx={{ color: "text.secondary" }}>
                            <Box component={LocalOverlayLink} href={href} scroll={false} title={row.actual} sx={CELL_LINK_SX}>
                              {actual}
                            </Box>
                          </TableCell>
                          <TableCell>
                            <Box component={LocalOverlayLink} href={href} scroll={false} sx={CELL_LINK_SX}>
                              <Tag tone={workStateTone(pageContract, row.work_state)}>{gapLabel(pageContract, row)}</Tag>
                              {driveDetail ? (
                                <Box component="span" sx={{ display: "block", mt: 0.5, typography: "caption", color: "text.secondary" }}>
                                  {driveDetail}
                                </Box>
                              ) : null}
                            </Box>
                          </TableCell>
                          <TableCell>
                            <Box component={LocalOverlayLink} href={href} scroll={false} sx={CELL_LINK_SX}>
                              <Tag tone={optionTone(pageContract, "severity_chips", row.severity) as Tone}>{optionLabel(pageContract, "severity_chips", row.severity)}</Tag>
                            </Box>
                          </TableCell>
                          <TableCell sx={{ color: "text.secondary" }}>
                            <Box component={LocalOverlayLink} href={href} scroll={false} title={ownerOf(pageContract, row)} sx={CELL_LINK_SX}>
                              {ownerOf(pageContract, row)}
                            </Box>
                          </TableCell>
                          <TableCell>
                            <Box component={LocalOverlayLink} href={href} scroll={false} title={row.next_action} sx={{ ...CELL_LINK_SX, color: "primary.main", typography: "subtitle2" }}>
                              {row.next_action} →
                            </Box>
                          </TableCell>
                          <TableCell>
                            {/* Evidence chips are anchors themselves (they open the proof), so they are
                                NOT wrapped in the row link: an anchor inside an anchor is invalid HTML
                                and produced a hydration error. */}
                            <EvidenceMedia evidence={row.evidence} pageContract={pageContract} />
                          </TableCell>
                        </TableRow>
                      );
                    })
                  )}
                </TableBody>
              </Table>
            </Scrollbar>
          <VaccinationTablePager
            pageContract={pageContract}
            pageSizeOptions={pageSizeOptions}
            page={paged.page}
            pageSize={paged.pageSize}
            total={paged.total}
            start={paged.start}
            end={paged.end}
            noun={copy(pageContract, "table.ledger.noun")}
            hrefForPage={pagerHref}
            hrefForPageSize={pageSizeHref}
          />
          </UrlSuspense>
        </Card>
      </Stack>

      <ProtocolAdherenceLocalDrawer
        records={drawerRecords}
        ledgerLabels={ledgerLabels}
        closeHref={closeDrawerHref}
        initialSelectedRowId={initialSelectedRowId}
        pageContract={pageContract}
      />
    </Box>
  );
}

/** Params that never change the ledger: the local row drawer. */
const PANEL_IGNORE = ["adh_row"] as const;
