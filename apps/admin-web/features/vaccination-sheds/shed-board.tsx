import Box from "@mui/material/Box";
import { InfoTip } from "@/components/app/info-tip";
import { SHED_BOARD_PAGE_SIZE } from "./shed-board-layout";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import Link from "@/components/no-prefetch-link";
import { Layers, MapPin, Warehouse } from "lucide-react";
import { getVaccinationShedSummary, type ApiResult, type VaccinationShedSummaryResponse } from "@/lib/api/server";
import type {
  VaccinationCapacityStatus,
  VaccinationShedStatus,
  VaccinationShedSummaryRow,
} from "@/lib/api/vaccination-sheds";
import { Tag, ClipText, type Tone } from "@/components/ui-primitives";
import {
  copy,
  optionLabel,
  optionTone,
  table,
  tablePageSizes,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import { parseScope, scopeHref } from "@/lib/scope";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { fmtDate } from "@/lib/format";
import { VaccinationTablePager, type VaccinationPageSize } from "@/features/preventive-care-vaccination";
import { InfoHint } from "@/components/app/info-hint";
import { TemplateTabs, TabPanel } from "@/components/app/template-tabs";
import { IdentityCell } from "@/components/data-table";
import { ShedFilterBar } from "./shed-filter-bar";
import { UrlSuspense } from "@/components/app/url-suspense";
import { TableSkeleton } from "@/components/app/skeletons";
import { ShedRowActions, ShedSelectAllHeader, ShedSelectCheckbox, ShedSelectionProvider } from "./shed-table-client";
import { penDetailParams, vaccinationCurrentViewScope } from "./shed-scope";

// Merged CEO status headline order (highest priority first) — matches the backend headline priority and
// the shed_status_chips contract group. Used to validate the ?sheds_status filter and render chips.
const SHED_STATUS_ORDER: VaccinationShedStatus[] = ["overdue", "needs_review", "split", "due", "scheduled", "on_track"];
// Capacity filter order (All / Within cap / Split / Capacity action) — capacity_chips contract group.
const CAPACITY_ORDER: VaccinationCapacityStatus[] = ["within_cap", "over_cap", "capacity_breach"];

const DEFAULT_PAGE_SIZE = SHED_BOARD_PAGE_SIZE;

// Columns that carry a count, so they are right-aligned and tabular (`.num`) rather than reading as prose.
const NUMERIC_COLUMNS = new Set(["animals", "due", "done", "sessions"]);

// Per-column skeleton widths, so the placeholder reads as the real column and not as a stripe grid.
const SKELETON_CELL_WIDTHS: Record<string, number> = {
  animals: 44,
  due: 36,
  done: 36,
  sessions: 32,
  next_due: 78,
  manager: 132,
  backup: 96,
  status: 92,
};

function shedDisplayName(row: VaccinationShedSummaryRow): string {
  return row.operationalLocationDisplay || row.shedName;
}

function shedInitial(row: VaccinationShedSummaryRow): string {
  return (shedDisplayName(row).trim()[0] ?? "?").toUpperCase();
}

function DriveOperatorsCell({ row, pageContract }: { row: VaccinationShedSummaryRow & { driveOperatorNames?: string[] }; pageContract: AdminUiPageContract }) {
  const names = row.driveOperatorNames?.filter(Boolean) ?? [];
  if (names.length === 0) return <Tag tone="dng">{copy(pageContract, "label.operators_unassigned")}</Tag>;
  return <ClipText title={names.join(", ")} className="small">{names.join(", ")}</ClipText>;
}

function assignmentLabel(row: VaccinationShedSummaryRow & { driveOperatorNames?: string[] }, pageContract: AdminUiPageContract) {
  const operatorCount = row.driveOperatorNames?.filter(Boolean).length ?? 0;
  if (operatorCount <= 0) return copy(pageContract, "label.no_drive");
  return `${operatorCount} ${copy(pageContract, operatorCount === 1 ? "label.operator_count_singular" : "label.operator_count_plural")}`;
}

function shedStatusLabel(pageContract: AdminUiPageContract, status: VaccinationShedStatus): string {
  if (status === "scheduled") return copy(pageContract, "status.scheduled_drive");
  if (status === "on_track") return copy(pageContract, "status.no_work_due");
  return optionLabel(pageContract, "shed_status_chips", status);
}

export function getVaccinationShedSummaryParams(searchParams: RouteSearchParams | undefined, pageContract: AdminUiPageContract) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const { parkId } = vaccinationCurrentViewScope(scope);

  const status = SHED_STATUS_ORDER.find((s) => s === one(sp, "sheds_status"));
  const capacity = CAPACITY_ORDER.find((c) => c === one(sp, "sheds_capacity"));
  const search = one(sp, "sheds_q");
  const pageSizeOptions = tablePageSizes(pageContract, "shed-summary");
  const requestedLimit = Number(one(sp, "sheds_limit"));
  const limit: VaccinationPageSize = pageSizeOptions.includes(requestedLimit) ? requestedLimit : DEFAULT_PAGE_SIZE;
  const page = boundedInt(one(sp, "sheds_page"), 1, 1, 1_000_000);
  const offset = (page - 1) * limit;

  return { sp, scope, parkId, status, capacity, search, limit, page, offset, pageSizeOptions };
}

export function loadVaccinationShedSummary(searchParams: RouteSearchParams | undefined, pageContract: AdminUiPageContract) {
  const params = getVaccinationShedSummaryParams(searchParams, pageContract);
  return getVaccinationShedSummary({
    parkId: params.parkId,
    status: params.status,
    capacity: params.capacity,
    search: params.search,
    limit: params.limit,
    offset: params.offset,
  });
}

// Shed-wise vaccination board — the MAIN /vaccination table (one row per shed). Animal-level Due/Done,
// planned Sessions, capacity, and a merged CEO Status headline, all computed server-side. Park scope comes
// from the shell top bar (?park); status/capacity/search filters + offset pagination are server-side via
// GET /vaccination/sheds. Rows deep-link to /vaccination/execution/sheds/{shedId} (shed detail), carrying
// the current board state in ?ret so Back returns to the same filtered/paginated list.
/** Header cells of the pen table: leading checkbox + the contract's visible columns + row actions. */
export function shedBoardColumns(pageContract: AdminUiPageContract): number {
  return table(pageContract, "shed-summary").columns.filter((column) => column.visible).length + 2;
}

export async function VaccinationShedBoard({
  searchParams,
  pageContract,
  summaryResult,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
  summaryResult?: ApiResult<VaccinationShedSummaryResponse>;
}) {
  const {
    scope,
    status: statusFilter,
    capacity: capacityFilter,
    search,
    limit: pageSize,
    page,
    offset,
    pageSizeOptions,
  } = getVaccinationShedSummaryParams(searchParams, pageContract);
  const result = summaryResult ?? (await loadVaccinationShedSummary(searchParams, pageContract));
  const rows: VaccinationShedSummaryRow[] = result.ok ? listOrEmpty(result.data.rows) : [];
  // Honest only unfiltered: `rows` is one server-filtered page.
  const statusCounts = new Map<string, number>();
  if (!statusFilter && !capacityFilter) {
    for (const row of rows) {
      statusCounts.set(row.status, (statusCounts.get(row.status) ?? 0) + 1);
    }
  }
  const total = result.ok ? result.data.page.total : 0;
  const hasFilter = Boolean(statusFilter || capacityFilter || search);

  // Every filter/pager link preserves the FULL top-bar scope + the board's filter/page state.
  function hrefWith(overrides: Record<string, string | undefined>): string {
    return (
      scopeHref("/vaccination", scope, {}, {
        sheds_status: statusFilter,
        sheds_capacity: capacityFilter,
        sheds_q: search,
        sheds_page: String(page),
        sheds_limit: String(pageSize),
        ...overrides,
      }) + "#sheds"
    );
  }
  const resetHref = scopeHref("/vaccination", scope, {}, { sheds_limit: String(pageSize) }) + "#sheds";
  const allStatusHref = hrefWith({ sheds_status: undefined, sheds_page: "1" });
  const allCapacityHref = hrefWith({ sheds_capacity: undefined, sheds_page: "1" });
  function pagerHref(p: number): string {
    return hrefWith({ sheds_page: String(p) });
  }
  function pageSizeHref(size: VaccinationPageSize): string {
    return hrefWith({ sheds_page: "1", sheds_limit: String(size) });
  }
  // Pen detail carries the row's own partition (a row is one pen, not the building) and the current
  // board href in ?ret so its Back button restores the exact list state.
  function detailHref(row: VaccinationShedSummaryRow): string {
    return scopeHref(
      `/vaccination/execution/sheds/${encodeURIComponent(row.shedId)}`,
      scope,
      { mode: "park", park: row.parkId },
      penDetailParams(row, hrefWith({})),
    );
  }

  const shedTable = table(pageContract, "shed-summary");
  const cols = shedTable.columns.filter((column) => column.visible);
  const start = total === 0 ? 0 : offset + 1;
  const end = total === 0 ? 0 : Math.min(total, offset + rows.length);

  return (
    <section id="sheds" className="card" style={{ scrollMarginTop: 80 }}>
      <div className="hd">
        <Warehouse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.sheds.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "section.sheds.note")}</span>
      </div>

      <ShedFilterBar total={total} pageContract={pageContract} />

      {/* Status filter (merged CEO headline). Server-side via ?sheds_status. */}
      <div style={{ padding: "0 14px 8px" }}>
        <TemplateTabs
          variant="pill"
          ariaLabel={copy(pageContract, "label.all_status")}
          value={statusFilter || "all"}
          items={[
            { value: "all", label: copy(pageContract, "label.all_status"), href: allStatusHref },
            ...SHED_STATUS_ORDER.map((s) => ({
              value: s,
              label: shedStatusLabel(pageContract, s),
              count: statusCounts.get(s),
              href: hrefWith({ sheds_status: s, sheds_page: "1" }),
            })),
          ]}
        />
      </div>

      {/* Capacity filter (All / Within cap / Split / Capacity action). Server-side via ?sheds_capacity. */}
      <div style={{ padding: "0 14px 10px" }}>
        <TemplateTabs
          variant="pill"
          ariaLabel={copy(pageContract, "label.all_capacity")}
          value={capacityFilter || "all"}
          items={[
            { value: "all", label: copy(pageContract, "label.all_capacity"), href: allCapacityHref },
            ...CAPACITY_ORDER.map((c) => ({
              value: c,
              label: optionLabel(pageContract, "capacity_chips", c),
              href: hrefWith({ sheds_capacity: c, sheds_page: "1" }),
            })),
          ]}
        />
      </div>

      {/* The pen table (guard: url-keyed-panel): a status / capacity / search / page click swaps it
          to its skeleton at once; the section header, search and pill strips stay on screen. */}
      <UrlSuspense searchParams={searchParams ?? {}} watch={SHED_TABLE_WATCH} fallback={<TableSkeleton columns={shedBoardColumns(pageContract)} rows={SHED_BOARD_PAGE_SIZE} />}>
      <TabPanel tabKey={`${statusFilter ?? "all"}|${capacityFilter ?? "all"}`}>
      {!result.ok ? (
        <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "18px 16px", flexWrap: "wrap" }}>
          <Layers className="ic" aria-hidden="true" style={{ width: 18, height: 18, color: "var(--danger)", flexShrink: 0 }} />
          <div style={{ minWidth: 0, flex: 1 }}>
            <b style={{ fontSize: 14 }}>{copy(pageContract, "section.sheds.unavailable_title")}</b>
            <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
              {copy(pageContract, "section.sheds.unavailable_body")} {result.error.message}
            </span>
          </div>
        </div>
      ) : rows.length === 0 ? (
        <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "18px 16px", flexWrap: "wrap" }}>
          <Layers className="ic" aria-hidden="true" style={{ width: 18, height: 18, color: "var(--brand)", flexShrink: 0 }} />
          <div style={{ minWidth: 0, flex: 1 }}>
            <b style={{ fontSize: 14 }}>
              {hasFilter
                ? copy(pageContract, "section.sheds.empty_filtered_title")
                : copy(pageContract, "section.sheds.empty_none_title")}
            </b>
            <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
              {hasFilter
                ? copy(pageContract, "section.sheds.empty_filtered_body")
                : copy(pageContract, "section.sheds.empty_none_body")}
            </span>
          </div>
          {hasFilter ? (
            <Link href={resetHref} replace scroll={false} className="btn sm">
              {copy(pageContract, "action.reset_filters")}
            </Link>
          ) : null}
        </div>
      ) : (
        <>
          <ShedSelectionProvider allIds={rows.map((row) => row.shedId)}>
          <div className="bd twrap" style={{ padding: 0 }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.sheds.title")}>
            <Table className="shed-summary-table">
              <TableHead>
                <TableRow>
                  <TableCell component="th" className="kit-check">
                    <ShedSelectAllHeader label={copy(pageContract, "action.select_all", "Select all")} />
                  </TableCell>
                  {cols.filter((col) => col.key !== "park").map((col) => (
                    <TableCell component="th" key={col.key} className={NUMERIC_COLUMNS.has(col.key) ? "num" : undefined}>
                      {col.key === "sessions" ? (
                        <span style={{ display: "inline-flex", alignItems: "center" }}>
                          {col.label}
                          <InfoTip title={copy(pageContract, "tooltip.sessions.body")} />
                        </span>
                      ) : col.key === "manager" ? (
                        copy(pageContract, "label.operators")
                      ) : col.key === "backup" ? (
                        copy(pageContract, "label.assignment")
                      ) : (
                        col.label
                      )}
                    </TableCell>
                  ))}
                  <TableCell component="th" className="kit-rowactions-th" aria-label={copy(pageContract, "label.actions", "Actions")} />
                </TableRow>
              </TableHead>
              <TableBody>
                {rows.map((row, rowIndex) => {
                  const href = detailHref(row);
                  const cell = (content: React.ReactNode, extra?: string, withRowLink = false) => (
                    <TableCell className={extra}>
                      {withRowLink ? (
                        <Link
                          href={href}
                          className="shed-summary-row-link"
                          scroll={false}
                          prefetch={false}
                          aria-label={`${copy(pageContract, "action.open_shed_board")} ${row.operationalLocationDisplay || row.shedName}`}
                        />
                      ) : null}
                      <span className="shed-summary-cell-content">
                        {content}
                      </span>
                    </TableCell>
                  );
                  const partitionAwareKey = [
                    row.parkId,
                    row.shedId,
                    row.partitionLabel ?? "",
                    row.nextDue ?? "",
                    row.status,
                    row.capacity,
                    row.animals,
                    row.due,
                    row.done,
                    row.sessions,
                    rowIndex,
                  ].join("|");
                  return (
                    <TableRow key={partitionAwareKey} className="shed-summary-row">
                      <TableCell className="kit-check">
                        <ShedSelectCheckbox
                          id={row.shedId}
                          label={`${copy(pageContract, "action.open_shed_board")} ${shedDisplayName(row)}`}
                        />
                      </TableCell>
                      {cell(
                        <IdentityCell
                          lead={<span className="kit-avatar" aria-hidden="true">{shedInitial(row)}</span>}
                          primary={<ClipText title={shedDisplayName(row)}>{shedDisplayName(row)}</ClipText>}
                          secondary={
                            <span style={{ display: "inline-flex", alignItems: "center", gap: 4, minWidth: 0 }}>
                              <MapPin className="ic" style={{ width: 12, opacity: 0.75, flexShrink: 0 }} aria-hidden="true" />
                              <ClipText title={row.parkName}>{row.parkName}</ClipText>
                            </span>
                          }
                        />,
                        undefined,
                        true,
                      )}
                      {cell(row.animals, "num muted")}
                      {cell(row.due, "num")}
                      {cell(row.done, "num muted")}
                      {cell(row.sessions, "num")}
                      {cell(row.nextDue ? fmtDate(row.nextDue) : copy(pageContract, "label.placeholder"), "muted")}
                      {cell(<DriveOperatorsCell row={row} pageContract={pageContract} />)}
                      {cell(<Tag tone={row.sessions > 1 ? "warn" : "mut"}>{assignmentLabel(row, pageContract)}</Tag>)}
                      {cell(
                        <Tag tone={optionTone(pageContract, "shed_status_chips", row.status) as Tone}>
                          {shedStatusLabel(pageContract, row.status)}
                        </Tag>,
                      )}
                      <TableCell className="kit-rowactions">
                        <ShedRowActions
                          shedId={row.shedId}
                          detailHref={href}
                          parkHref={scopeHref("/vaccination", scope, { mode: "park", park: row.parkId }, {})}
                          labels={{
                            menu: copy(pageContract, "label.row_actions", "Row actions"),
                            open: copy(pageContract, "action.open_shed_board"),
                            park: copy(pageContract, "label.park", "Open park"),
                            copy: copy(pageContract, "action.copy_id", "Copy pen ID"),
                          }}
                        />
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
          </ShedSelectionProvider>
          <Box sx={{ display: "flex", justifyContent: "flex-end", mt: 1.25, mx: 1.75 }}>
            <InfoHint text={copy(pageContract, "note.sheds_counts")} />
          </Box>
          <VaccinationTablePager
            pageContract={pageContract}
            pageSizeOptions={pageSizeOptions}
            page={page}
            pageSize={pageSize}
            total={total}
            start={start}
            end={end}
            noun={copy(pageContract, "label.shed_noun")}
            hrefForPage={pagerHref}
            hrefForPageSize={pageSizeHref}
          />
        </>
      )}
      </TabPanel>
      </UrlSuspense>
    </section>
  );
}

/** The params the pen table reads. */
const SHED_TABLE_WATCH = ["sheds_status", "sheds_capacity", "sheds_q", "sheds_page", "sheds_limit", "park", "scope_mode"] as const;
