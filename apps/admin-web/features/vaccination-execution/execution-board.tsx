import type { ComponentProps } from "react";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableRow from "@mui/material/TableRow";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { EmptyContent } from "@/components/minimal/empty-content";
import { TableHeadCustom } from "@/components/app/table";
import { listOrEmpty } from "@/lib/list-or-empty";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { LocalOverlayDrawer, type LocalOverlayDrawerItem } from "@/components/local-overlay-drawer";
import { getVaccinationExecution, type ApiResult } from "@/lib/api/server";
import type {
  VaccinationExecutionResponse,
  VaccinationExecutionRow,
  VaccinationExecutionSeverity,
  VaccinationExecutionWorkState,
} from "@/lib/api/vaccination-execution";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope, scopeHref } from "@/lib/scope";
import {
  SEVERITY_ORDER,
  SEVERITY_RANK,
  WORK_STATE_ORDER,
} from "./work-state";
import { Tag, type Tone } from "@/components/ui-primitives";
import { TemplateTabs, TabPanel } from "@/components/app/template-tabs";
import { fmtDate } from "@/lib/format";
import { operationalLocationLabel } from "@/lib/operational-location";
import {
  copy,
  optionGroup,
  optionLabel,
  optionTone,
  tableLabels,
  tablePageSizes,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import {
  VaccinationFilterButton,
  VisibleTableSearch,
  VaccinationRecordFormFields,
  vaccinationDriveDisplayName,
  paginateRows,
  VaccinationTablePager,
  type VaccinationPageSize,
} from "@/features/preventive-care-vaccination";
import { ShedEventActions } from "./shed-event-actions";
import { vaccinationCurrentViewScope } from "@/features/vaccination-sheds";
import { DrawerMetaGrid, DrawerMetaItem } from "@/components/app/detail-drawer";
import { LinkButton } from "@/components/app/link-button";

// Work states that mean "someone must act now" — used for the per-park attention count.
const ATTENTION_STATES = new Set<VaccinationExecutionWorkState>(["overdue", "missed", "blocked", "rejected"]);

interface ParkGroup {
  parkId: string;
  parkName: string;
  rows: VaccinationExecutionRow[];
  severity: VaccinationExecutionSeverity;
  attention: number;
}

interface PhysicalShedGroup {
  key: string;
  physicalShed: string;
  rows: VaccinationExecutionRow[];
  animals: number;
  severity: VaccinationExecutionSeverity;
  operators: string[];
}

function groupByPark(rows: VaccinationExecutionRow[]): ParkGroup[] {
  const byId = new Map<string, ParkGroup>();
  for (const r of rows) {
    let g = byId.get(r.parkId);
    if (!g) {
      g = { parkId: r.parkId, parkName: r.parkName, rows: [], severity: "ok", attention: 0 };
      byId.set(r.parkId, g);
    }
    g.rows.push(r);
    if (SEVERITY_RANK[r.severity] > SEVERITY_RANK[g.severity]) g.severity = r.severity;
    if (ATTENTION_STATES.has(r.workState)) g.attention += 1;
  }
  // Parks with the worst severity / most attention float to the top.
  return Array.from(byId.values()).sort(
    (a, b) => SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity] || b.attention - a.attention,
  );
}

function physicalShedName(row: VaccinationExecutionRow): string {
  return (row.physicalShed || row.shedName || "").trim() || row.shedName;
}

function partitionLabel(row: VaccinationExecutionRow): string {
  return row.operational_location_display || operationalLocationLabel({ shedName: row.shedName, partitionLabel: row.partition_label });
}

function groupByPhysicalShed(rows: VaccinationExecutionRow[]): PhysicalShedGroup[] {
  const byKey = new Map<string, PhysicalShedGroup>();
  for (const row of rows) {
    const physicalShed = physicalShedName(row);
    // Key by shed_id only (no partition), so partitions group under one physical shed.
    // Display = parent shed name; individual partitions shown per-row via partitionLabel().
    const key = `${row.parkId}|${row.shedId}`;
    let group = byKey.get(key);
    if (!group) {
      group = { key, physicalShed, rows: [], animals: 0, severity: "ok", operators: [] };
      byKey.set(key, group);
    }
    group.rows.push(row);
    group.animals += row.targetCount;
    if (SEVERITY_RANK[row.severity] > SEVERITY_RANK[group.severity]) group.severity = row.severity;
    const operatorName = row.owner?.operatorName?.trim();
    if (operatorName && !group.operators.includes(operatorName)) group.operators.push(operatorName);
  }
  return Array.from(byKey.values()).sort((a, b) => {
    if (SEVERITY_RANK[b.severity] !== SEVERITY_RANK[a.severity]) return SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity];
    return a.physicalShed.localeCompare(b.physicalShed, undefined, { numeric: true });
  });
}

// Owner chain: operator (ground) -> park head -> verifier (Video Verification Team).
function OwnerChain({ row, pageContract }: { row: VaccinationExecutionRow; pageContract: AdminUiPageContract }) {
  const o = row.owner;
  const operatorMissing = !o?.operatorName;
  const line = (icon: ComponentProps<typeof Iconify>["icon"], content: React.ReactNode) => (
    <Stack direction="row" spacing={0.75} sx={{ alignItems: "center", minWidth: 0 }}>
      <Iconify icon={icon} width={14} sx={{ flexShrink: 0, color: "text.disabled" }} />
      {content}
    </Stack>
  );
  return (
    <Stack spacing={0.5} sx={{ minWidth: 0 }}>
      {line(
        "solar:user-rounded-bold",
        operatorMissing ? (
          <Tag tone="dng" title={copy(pageContract, "reason.no_operator")}>{copy(pageContract, "label.operator_unassigned")}</Tag>
        ) : (
          <Typography component="span" variant="body2" noWrap>{o?.operatorName}</Typography>
        ),
      )}
      {line(
        "mingcute:location-fill",
        <Typography component="span" variant="body2" noWrap sx={{ color: "text.secondary" }}>
          {o?.parkHeadName ?? copy(pageContract, "label.park_head_unassigned")}
        </Typography>,
      )}
      {line(
        "solar:shield-check-bold",
        <Typography component="span" variant="body2" noWrap sx={{ color: "text.secondary" }}>
          {o?.verifierName ?? copy(pageContract, "label.verifier_default")}
        </Typography>,
      )}
    </Stack>
  );
}

function StatusChips({ row, pageContract }: { row: VaccinationExecutionRow; pageContract: AdminUiPageContract }) {
  return (
    <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
      {row.sopStatus ? <Tag tone={optionTone(pageContract, "sop_state_chips", row.sopStatus) as Tone}>{optionLabel(pageContract, "sop_state_chips", row.sopStatus)}</Tag> : null}
      {row.proofStatus ? <Tag tone={optionTone(pageContract, "proof_state_chips", row.proofStatus) as Tone}>{optionLabel(pageContract, "proof_state_chips", row.proofStatus)}</Tag> : null}
      {row.verificationStatus ? (
        <Tag tone={optionTone(pageContract, "verification_state_chips", row.verificationStatus) as Tone}>{optionLabel(pageContract, "verification_state_chips", row.verificationStatus)}</Tag>
      ) : null}
    </Box>
  );
}

function executionDriveLabel(row: VaccinationExecutionRow): string {
  // Canonical vaccine label + dose phase ("FMD · Primary") — strips the TEST-ONLY/seed-code noise and reads
  // consistently with the status-matrix column headers.
  return vaccinationDriveDisplayName(row.driveName);
}

function executionActionTitle(pageContract: AdminUiPageContract, row: VaccinationExecutionRow): string {
  const location = partitionLabel(row);
  if (row.proofStatus === "missing") return `${copy(pageContract, "action.capture_vaccination_proof")} — ${location}`;
  if (row.verificationStatus === "pending") return `${copy(pageContract, "action.verify_vaccination_proof")} — ${location}`;
  if (row.workState === "overdue") return `${executionDriveLabel(row)} ${copy(pageContract, "label.overdue")} — ${location}`;
  return `${executionDriveLabel(row)} — ${location}`;
}

// One shed event: a template table row whose whole surface opens the shed-event drawer (a stretched
// LocalOverlayLink under the cell content), like the template list rows that open their record.
function ExecutionRow({ row, drawerHref, pageContract }: { row: VaccinationExecutionRow; drawerHref: string; pageContract: AdminUiPageContract }) {
  const driveLabel = executionDriveLabel(row);
  const shedLabel = physicalShedName(row);
  const partition = partitionLabel(row);
  return (
    <TableRow hover data-exec-row="" sx={{ position: "relative", cursor: "pointer", "& > td": { verticalAlign: "top" } }}>
      <TableCell sx={{ pl: 4.5 }}>
        <Box
          component={LocalOverlayLink}
          href={drawerHref}
          scroll={false}
          aria-haspopup="dialog"
          aria-label={`${copy(pageContract, "action.open_shed_event_for")} ${shedLabel} ${partition}`}
          title={`${shedLabel} · ${partition} · ${driveLabel} · ${row.nextAction}`}
          sx={ROW_LINK_SX}
        />
        <Box sx={CELL_CONTENT_SX}>
          <Typography component="div" variant="subtitle2" noWrap title={`${shedLabel} · ${partition}`}>
            {partition}
          </Typography>
          <Typography component="div" variant="body2" noWrap title={row.animalStage} sx={{ color: "text.secondary", mt: 0.25 }}>
            {row.animalStage}
          </Typography>
        </Box>
      </TableCell>
      <TableCell>
        <Box sx={CELL_CONTENT_SX}>
          <Stack direction="row" spacing={0.75} sx={{ alignItems: "center", minWidth: 0 }}>
            <Iconify icon="solar:medical-kit-bold" width={14} sx={{ flexShrink: 0, color: "text.disabled" }} />
            <Typography component="span" variant="body2" noWrap title={driveLabel}>
              {driveLabel}
            </Typography>
          </Stack>
          <Typography component="div" variant="body2" sx={{ color: "text.secondary", mt: 0.25 }}>
            {copy(pageContract, "label.due_prefix")} {fmtDate(row.dueDate)}
          </Typography>
        </Box>
      </TableCell>
      <TableCell>
        <Box sx={CELL_CONTENT_SX}>
          <Tag tone={optionTone(pageContract, "work_state_filter_chips", row.workState) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", row.workState)}</Tag>
          {row.blockerReason ? (
            <Stack direction="row" spacing={0.75} title={row.blockerReason} sx={{ alignItems: "center", color: "error.main", mt: 0.75, minWidth: 0 }}>
              <Iconify icon="solar:forbidden-circle-bold" width={14} sx={{ flexShrink: 0 }} />
              {/* Concise head on the list ("ICU / quarantine"); full reason on hover + in the drilldown. */}
              <Typography component="span" variant="body2" noWrap>
                {row.blockerReason.split(" — ")[0]}
              </Typography>
            </Stack>
          ) : null}
        </Box>
      </TableCell>
      <TableCell>
        <Box sx={CELL_CONTENT_SX}>
          <OwnerChain row={row} pageContract={pageContract} />
        </Box>
      </TableCell>
      <TableCell>
        <Box sx={CELL_CONTENT_SX}>
          <StatusChips row={row} pageContract={pageContract} />
        </Box>
      </TableCell>
      <TableCell>
        {/* Backend-suggested next step — a HINT, not a wired button. The row itself opens the drawer; owner
            assignment isn't actionable yet, so this must not masquerade as a CTA button. Plain muted text. */}
        <Typography variant="body2" title={row.nextAction} sx={{ ...CELL_CONTENT_SX, color: "text.secondary", overflowWrap: "anywhere" }}>
          {row.nextAction}
        </Typography>
      </TableCell>
    </TableRow>
  );
}

// Stretched row link: covers the row; cell content sits above it without taking the click.
const ROW_LINK_SX = { position: "absolute", inset: 0, zIndex: 1 } as const;
const CELL_CONTENT_SX = { position: "relative", zIndex: 2, pointerEvents: "none", minWidth: 0 } as const;

// Embeddable execution section. Parks does NOT own a vaccination product surface — this renders INSIDE
// Preventive Care (PC) / Vaccination (/vaccination#execution), scoped by the top-bar park dropdown (?park). The
// `basePath` parameterizes the filter self-links so they stay on the embedding route; shed-drilldown rows
// deep-link to /vaccination/execution/sheds/{shed_id} (physical execution-context detail).
export async function VaccinationExecutionBoard({
  searchParams,
  basePath = "/vaccination",
  baseParams = {},
  pageContract,
  executionResult,
}: {
  searchParams?: RouteSearchParams;
  basePath?: string;
  // Query params always kept on filter/reset links (e.g. {section:"execution"}) so the embedding tab stays selected.
  baseParams?: Record<string, string>;
  pageContract: AdminUiPageContract;
  executionResult?: ApiResult<VaccinationExecutionResponse>;
}) {
  const sp = searchParams ?? {};
  const stateFilter = (WORK_STATE_ORDER.find((s) => s === one(sp, "state")) ?? "all") as VaccinationExecutionWorkState | "all";
  const severityFilter = (SEVERITY_ORDER.find((s) => s === one(sp, "severity")) ?? "all") as VaccinationExecutionSeverity | "all";
  const scope = parseScope(sp);
  const { parkId } = vaccinationCurrentViewScope(scope);
  const pageSizeOptions = tablePageSizes(pageContract, "shed-events");
  const requestedBackendLimit = pageSizeOptions.find((size) => String(size) === one(sp, "exec_limit")) ?? 10;
  const executionCursor = one(sp, "exec_cursor");

  const result =
    executionResult ??
    (await getVaccinationExecution({
      parkId,
      workState: stateFilter === "all" ? undefined : stateFilter,
      limit: requestedBackendLimit,
      cursor: executionCursor,
    }));
  const allRows: VaccinationExecutionRow[] = result.ok ? listOrEmpty(result.data.rows) : [];
  const nextCursor = result.ok ? result.data.nextCursor ?? null : null;

  let rows = allRows;
  if (severityFilter !== "all") rows = rows.filter((r) => r.severity === severityFilter);

  const stateCounts = new Map<VaccinationExecutionWorkState, number>();
  const sevCounts = new Map<VaccinationExecutionSeverity, number>();
  // Counts reflect the backend-scoped result set; park and state filters are applied before pagination.
  for (const r of allRows) {
    stateCounts.set(r.workState, (stateCounts.get(r.workState) ?? 0) + 1);
    sevCounts.set(r.severity, (sevCounts.get(r.severity) ?? 0) + 1);
  }

  const labels = tableLabels(pageContract, "shed-events");
  const paged = paginateRows(rows, sp, "exec", 10, pageSizeOptions);
  const parks = groupByPark(paged.items);
  const selectedEventId = one(sp, "shed_event");

  // work_state is applied SERVER-SIDE and the result is capped (limit), so per-state counts are only
  // meaningful when no state filter is active. Park scope belongs to the shell top bar / Filters.
  const showStateCounts = stateFilter === "all";
  const capped = nextCursor !== null;
  // True empty: the service answered with zero rows and no filter is narrowing them. The severity/state
  // chips would all read 0 (dead microcopy), so suppress the filter chrome and show a compact empty row.
  const noWork = result.ok && allRows.length === 0 && severityFilter === "all" && stateFilter === "all";

  // Filter hrefs preserve the FULL top-bar scope (scopeHref) + the page severity/state filters + any
  // embedding params — never hand-rolled, so park/range/as_of/date_from/date_to are never dropped.
  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref(basePath, scope, {}, {
      ...baseParams,
      severity: severityFilter,
      state: stateFilter,
      exec_cursor: executionCursor,
      exec_page: String(paged.page),
      exec_limit: String(paged.pageSize),
      ...overrides,
    });
  }
  // Reset clears the page filters but keeps the full top-bar scope + embedding params.
  const resetHref = scopeHref(basePath, scope, {}, { ...baseParams, exec_page: "1", exec_limit: String(paged.pageSize) });
  function pagerHref(page: number): string {
    return hrefWith({ exec_page: String(page) });
  }
  function pageSizeHref(pageSize: VaccinationPageSize): string {
    return hrefWith({ exec_cursor: undefined, exec_page: "1", exec_limit: String(pageSize) });
  }
  const nextBackendHref = nextCursor ? hrefWith({ exec_cursor: nextCursor, exec_page: "1" }) : null;

  return (
    <Box data-filter-scope="">
      {/* Filter chrome (severity + work-state + provenance) is hidden when there is genuinely no work — the
          chips would all read 0. It returns the instant any row exists or a filter is active. */}
      {!noWork && (
        <Stack spacing={1.25} sx={{ mb: 2 }}>
          {/* Template list toolbar: visible-row search, Filters, the row range. */}
          <Card variant="outlined" sx={{ p: 2, display: "flex", flexWrap: "wrap", alignItems: "center", gap: 2 }}>
            <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.shed_events.search")} />
            <VaccinationFilterButton
              pageContract={pageContract}
              title={copy(pageContract, "filter.shed_events.title")}
              searchReason={copy(pageContract, "filter.shed_events.reason")}
              filterReason={copy(pageContract, "filter.shed_events.filter_reason")}
              rowsLabel={`${paged.start}-${paged.end} ${copy(pageContract, "pager.of")} ${rows.length} ${copy(pageContract, "pager.rows").toLowerCase()} · ${copy(pageContract, "filter.shed_events.rows_suffix")}`}
              actionHref={scopeHref("/action-center", scope)}
              actionLabel={copy(pageContract, "action.open_action_center")}
              facets={optionGroup(pageContract, "shed_event_facets").map((facet) => facet.label)}
            />
            <Typography component="span" variant="body2" sx={{ color: "text.secondary" }}>
              {paged.start}-{paged.end} {copy(pageContract, "pager.of")} {rows.length} {copy(pageContract, "pager.rows").toLowerCase()}
            </Typography>
          </Card>

          {/* Severity filter — animated pill tabs (sliding indicator, URL-driven). */}
          <TemplateTabs
            variant="pill"
            ariaLabel={copy(pageContract, "label.all_severity")}
            value={severityFilter}
            items={[
              { value: "all", label: copy(pageContract, "label.all_severity"), href: hrefWith({ severity: "all", exec_cursor: undefined, exec_page: "1" }) },
              ...SEVERITY_ORDER.map((s) => ({
                value: s,
                label: optionLabel(pageContract, "severity_chips", s),
                count: sevCounts.get(s) ?? 0,
                href: hrefWith({ severity: s, exec_cursor: undefined, exec_page: "1" }),
              })),
            ]}
          />

          {/* Work-state filter board (most-broken first). Server-side filter: always render every state as
              navigation (so selecting one never collapses the board), count only in the unfiltered view. */}
          <TemplateTabs
            variant="pill"
            ariaLabel={copy(pageContract, "label.all_states")}
            value={stateFilter}
            items={[
              {
                value: "all",
                label: copy(pageContract, "label.all_states"),
                count: showStateCounts ? allRows.length : undefined,
                href: hrefWith({ state: "all", exec_cursor: undefined, exec_page: "1" }),
              },
              ...(showStateCounts ? WORK_STATE_ORDER.filter((s) => (stateCounts.get(s) ?? 0) > 0) : WORK_STATE_ORDER).map((s) => ({
                value: s,
                label: optionLabel(pageContract, "work_state_filter_chips", s),
                count: showStateCounts ? (stateCounts.get(s) ?? 0) : undefined,
                href: hrefWith({ state: s, exec_cursor: undefined, exec_page: "1" }),
              })),
            ]}
          />

          {/* Honest provenance: counts/rows come from a bounded, server-filtered fetch — not tenant-wide totals. */}
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {showStateCounts
              ? `${copy(pageContract, "note.execution_counts")}${capped ? ` ${copy(pageContract, "note.execution_counts_capped")}` : ""}.`
              : copy(pageContract, "note.execution_counts_filtered")}
          </Typography>
        </Stack>
      )}

      <TabPanel tabKey={`${severityFilter}|${stateFilter}`}>
      {parks.length === 0 ? (
        <Card>
          <EmptyContent
            filled
            title={noWork ? copy(pageContract, "section.shed_events.empty_none_title") : copy(pageContract, "section.shed_events.empty_filtered_title")}
            description={noWork ? copy(pageContract, "section.shed_events.empty_none_body") : copy(pageContract, "section.shed_events.empty_filtered_body")}
            action={
              !noWork ? (
                <LinkButton href={resetHref} replace scroll={false} variant="outlined" color="inherit" size="small" sx={{ mt: 2 }}>
                  {copy(pageContract, "action.reset_filters")}
                </LinkButton>
              ) : undefined
            }
            sx={{ m: 2.5, py: 5 }}
          />
        </Card>
      ) : (
        <>
        <Stack spacing={2}>
          {parks.map((park) => {
            const physicalSheds = groupByPhysicalShed(park.rows);
            return (
            <Card key={park.parkId}>
              <CardHeader
                title={
                  <Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}>
                    <Box component="span">{park.parkName}</Box>
                    <Tag tone={optionTone(pageContract, "severity_chips", park.severity) as Tone}>{optionLabel(pageContract, "severity_chips", park.severity)}</Tag>
                  </Stack>
                }
                action={
                  park.attention > 0 ? (
                    // Scope to this park (top-bar scope override, NOT a stray filter param) AND filter to the
                    // attention rows (severity=broken) so the click actually narrows the board instead of being
                    // a no-op reset.
                    <LinkButton
                      href={scopeHref(basePath, scope, { park: park.parkId, mode: "park" }, { ...baseParams, severity: "broken", state: stateFilter })}
                      replace
                      scroll={false}
                      size="small"
                      variant="soft"
                      color="error"
                    >
                      {park.attention} {copy(pageContract, "label.need_attention")}
                    </LinkButton>
                  ) : (
                    <Typography component="span" variant="body2" sx={{ color: "text.secondary" }}>
                      {copy(pageContract, "label.on_track")}
                    </Typography>
                  )
                }
                sx={{ mb: 2 }}
              />
              <Scrollbar>
                <Table sx={{ minWidth: 1180 }} aria-label={`${park.parkName} ${copy(pageContract, "section.shed_events.aria")}`}>
                  <TableHeadCustom headCells={labels.map((label, index) => ({ id: `c${index}`, label, width: EXEC_COLUMN_WIDTHS[index] }))} />
                  <TableBody>
                    {physicalSheds.map((shedGroup) => [
                      <TableRow key={shedGroup.key}>
                        <TableCell colSpan={labels.length} sx={{ bgcolor: "background.neutral", py: 1.25 }}>
                          <Stack direction={{ xs: "column", sm: "row" }} spacing={{ xs: 0.75, sm: 1.5 }} sx={{ alignItems: { sm: "center" }, minWidth: 0 }}>
                            <Stack direction="row" spacing={1} sx={{ alignItems: "center", minWidth: 0 }}>
                              <Iconify icon="solar:home-angle-bold-duotone" width={16} sx={{ flexShrink: 0, color: "primary.main" }} />
                              <Typography component="span" variant="subtitle2" noWrap title={shedGroup.physicalShed}>
                                {shedGroup.physicalShed}
                              </Typography>
                              <Tag tone={optionTone(pageContract, "severity_chips", shedGroup.severity) as Tone}>{optionLabel(pageContract, "severity_chips", shedGroup.severity)}</Tag>
                            </Stack>
                            <Typography component="span" variant="body2" sx={{ color: "text.secondary", ml: { sm: "auto" } }}>
                              {shedGroup.rows.length} partitions · {shedGroup.animals} animals
                              {shedGroup.operators.length ? ` · ${shedGroup.operators.join(", ")}` : ""}
                            </Typography>
                          </Stack>
                        </TableCell>
                      </TableRow>,
                      ...shedGroup.rows.map((row, idx) => {
                        const partitionAwareKey = `${row.shedId}|${row.partition_label ?? ""}|${row.driveId ?? idx}`;
                        return (
                          <ExecutionRow
                            key={partitionAwareKey}
                            row={row}
                            drawerHref={hrefWith({ shed_event: shedEventId(row) })}
                            pageContract={pageContract}
                          />
                        );
                      }),
                    ])}
                  </TableBody>
                </Table>
              </Scrollbar>
              <Box sx={{ px: 2, py: 1.5, borderTop: 1, borderColor: "divider", borderTopStyle: "dashed" }}>
                <LinkButton
                  href={scopeHref("/action-center", scope, { park: park.parkId, mode: "park" })}
                  size="small"
                  color="inherit"
                  endIcon={<Iconify icon="eva:arrow-ios-forward-fill" />}
                >
                  {copy(pageContract, "action.open_park_action_center")}
                </LinkButton>
              </Box>
            </Card>
          )})}
        </Stack>
        <VaccinationTablePager
          pageContract={pageContract}
          pageSizeOptions={pageSizeOptions}
          page={paged.page}
          pageSize={paged.pageSize}
          total={paged.total}
          start={paged.start}
          end={paged.end}
          noun={copy(pageContract, "label.shed_event_noun")}
          hrefForPage={pagerHref}
          hrefForPageSize={pageSizeHref}
        />
        {nextBackendHref ? (
          <Stack direction="row" spacing={2} sx={{ alignItems: "center", mt: 1 }}>
            <Typography variant="body2" sx={{ color: "text.secondary", flex: 1 }}>
              {copy(pageContract, "pager.scale_note")}
            </Typography>
            <LinkButton href={nextBackendHref} replace scroll={false} size="small" variant="outlined" color="inherit" endIcon={<Iconify icon="eva:arrow-ios-forward-fill" />}>
              {copy(pageContract, "action.next")}
            </LinkButton>
          </Stack>
        ) : null}
        </>
      )}
      </TabPanel>
      <LocalOverlayDrawer
        items={rows.map((row) => shedEventDrawerItem(row, scope, pageContract))}
        selectionKey="shed_event"
        initialSelectedId={selectedEventId}
        closeHref={hrefWith({ shed_event: undefined })}
        ariaLabel={copy(pageContract, "drawer.shed_event.aria")}
        closeLabel={copy(pageContract, "drawer.shed_event.close_label")}
      />
    </Box>
  );
}

// Pen / drive / work / owner / status / next-action column floors (the table scrolls in its card below).
const EXEC_COLUMN_WIDTHS = ["16%", "22%", "14%", "18%", "14%", "16%"];

function shedEventId(row: VaccinationExecutionRow): string {
  return `${row.shedId}|${row.partition_label ?? ""}|${row.driveId ?? "drive"}|${row.animalStage}`;
}

function shedEventDrawerItem(row: VaccinationExecutionRow, scope: ReturnType<typeof parseScope>, pageContract: AdminUiPageContract): LocalOverlayDrawerItem {
  const driveLabel = executionDriveLabel(row);
  const detailHref = scopeHref(
    `/vaccination/execution/sheds/${encodeURIComponent(row.shedId)}`,
    scope,
    { mode: "park", park: row.parkId },
    { partition_label: row.partition_label ?? undefined },
  );
  const actionCenterHref = scopeHref("/action-center", scope, {}, { state: row.workState });
  return {
    id: shedEventId(row),
    eyebrow: copy(pageContract, "drawer.shed_event.eyebrow"),
    title: executionActionTitle(pageContract, row),
    icon: <Iconify icon="solar:medical-kit-bold" />,
    body: (
      <>
          <DrawerMetaGrid>
            <DrawerMetaItem label={copy(pageContract, "drawer.shed_event.shed_event")}>{driveLabel}</DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "drawer.shed_event.shed")}>{/* Render the backend-composed operational location: "Godel 1 - Part 3", not bare "Godel 1" when partitioned */}
              {partitionLabel(row)}</DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "drawer.shed_event.owner_assist")}>{row.owner?.operatorName ?? copy(pageContract, "label.owner_chain_to_assign")}</DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "drawer.shed_event.stock")}>{copy(pageContract, "label.stock_resolved_action_center")}</DrawerMetaItem>
            <DrawerMetaItem label={copy(pageContract, "drawer.shed_event.status")}><Tag tone={optionTone(pageContract, "work_state_filter_chips", row.workState) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", row.workState)}</Tag></DrawerMetaItem>
          </DrawerMetaGrid>
          <VaccinationRecordFormFields cohortShed={`${row.animalStage} · ${partitionLabel(row)}`} vaccineName={driveLabel} pageContract={pageContract} />
          <ShedEventActions pageContract={pageContract} />
      </>
    ),
    footer: (
      <>
          <LinkButton href={actionCenterHref} variant="outlined" color="inherit" scroll={false}>
            {copy(pageContract, "action.open_action_center")}
          </LinkButton>
          <LinkButton href={detailHref} variant="outlined" color="inherit">
            {copy(pageContract, "action.shed_detail")}
          </LinkButton>
      </>
    ),
  };
}
