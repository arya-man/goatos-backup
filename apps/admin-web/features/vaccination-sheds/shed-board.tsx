import Link from "@/components/no-prefetch-link";
import { Layers, MapPin, Warehouse } from "lucide-react";
import { getVaccinationShedSummary, type ApiResult, type VaccinationShedSummaryResponse } from "@/lib/api/server";
import type {
  VaccinationCapacityStatus,
  VaccinationShedStatus,
  VaccinationShedSummaryRow,
} from "@/lib/api/vaccination-sheds";
import { Tag, InfoTooltip, ClipText, type Tone } from "@/components/ui-primitives";
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
import { ShedFilterBar } from "./shed-filter-bar";
import { vaccinationCurrentViewScope } from "./shed-scope";

// Merged CEO status headline order (highest priority first) — matches the backend headline priority and
// the shed_status_chips contract group. Used to validate the ?sheds_status filter and render chips.
const SHED_STATUS_ORDER: VaccinationShedStatus[] = ["overdue", "needs_review", "split", "due", "scheduled", "on_track"];
// Capacity filter order (All / Within cap / Split / Capacity action) — capacity_chips contract group.
const CAPACITY_ORDER: VaccinationCapacityStatus[] = ["within_cap", "over_cap", "capacity_breach"];

const DEFAULT_PAGE_SIZE = 25;

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

export function VaccinationShedBoardSkeleton({
  pageContract,
}: {
  pageContract: AdminUiPageContract;
}) {
  const shedTable = table(pageContract, "shed-summary");
  const cols = shedTable.columns.filter((column) => column.visible);
  return (
    <section id="sheds" className="card" style={{ scrollMarginTop: 80 }} aria-busy="true">
      <div className="hd">
        <Warehouse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.sheds.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "section.sheds.note")}</span>
      </div>
      <div className="bd" style={{ padding: "14px 14px 0" }}>
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "center" }}>
          <div className="skel" style={{ width: 280, maxWidth: "100%", height: 34 }} />
          <div className="skel" style={{ width: 72, height: 34 }} />
          <div className="sp" style={{ flex: 1 }} />
          <div className="skel" style={{ width: 118, height: 18 }} />
        </div>
      </div>
      <div className="chipset" style={{ padding: "12px 14px 8px" }}>
        {[96, 86, 116, 72, 110, 132].map((w, i) => (
          <div key={i} className="skel" style={{ width: w, height: 30, borderRadius: 999 }} />
        ))}
      </div>
      <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
        <table className="shed-summary-table">
          <thead>
            <tr>
              {cols.map((col) => (
                <th key={col.key}>{col.label}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {Array.from({ length: 5 }, (_, row) => (
              <tr key={row}>
                {cols.map((col, index) => (
                  <td key={col.key} className={index > 1 ? "muted" : undefined}>
                    <span className="skel" style={{ width: index < 2 ? 118 : 64, height: 16 }} />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

// Shed-wise vaccination board — the MAIN /vaccination table (one row per shed). Animal-level Due/Done,
// planned Sessions, capacity, and a merged CEO Status headline, all computed server-side. Park scope comes
// from the shell top bar (?park); status/capacity/search filters + offset pagination are server-side via
// GET /vaccination/sheds. Rows deep-link to /vaccination/execution/sheds/{shedId} (shed detail), carrying
// the current board state in ?ret so Back returns to the same filtered/paginated list.
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
  const rows: VaccinationShedSummaryRow[] = result.ok ? result.data.rows : [];
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
  // Shed detail carries the current board href in ?ret so its Back button restores the exact list state.
  function detailHref(row: VaccinationShedSummaryRow): string {
    return scopeHref(
      `/vaccination/execution/sheds/${encodeURIComponent(row.shedId)}`,
      scope,
      { mode: "park", park: row.parkId },
      { ret: hrefWith({}) },
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
      <div className="chipset" style={{ padding: "0 14px 8px" }}>
        <Link href={allStatusHref} replace scroll={false} className={`chip${!statusFilter ? " on" : ""}`}>
          {copy(pageContract, "label.all_status")}
        </Link>
        {SHED_STATUS_ORDER.map((s) => (
          <Link key={s} href={hrefWith({ sheds_status: s, sheds_page: "1" })} replace scroll={false} className={`chip${statusFilter === s ? " on" : ""}`}>
            {shedStatusLabel(pageContract, s)}
          </Link>
        ))}
      </div>

      {/* Capacity filter (All / Within cap / Split / Capacity action). Server-side via ?sheds_capacity. */}
      <div className="chipset" style={{ padding: "0 14px 10px" }}>
        <Link href={allCapacityHref} replace scroll={false} className={`chip${!capacityFilter ? " on" : ""}`}>
          {copy(pageContract, "label.all_capacity")}
        </Link>
        {CAPACITY_ORDER.map((c) => (
          <Link key={c} href={hrefWith({ sheds_capacity: c, sheds_page: "1" })} replace scroll={false} className={`chip${capacityFilter === c ? " on" : ""}`}>
            {optionLabel(pageContract, "capacity_chips", c)}
          </Link>
        ))}
      </div>

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
          <div className="bd" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.sheds.title")}>
            <table className="shed-summary-table">
              <thead>
                <tr>
                  {cols.map((col) => (
                    <th key={col.key}>
                      {col.key === "sessions" ? (
                        <span style={{ display: "inline-flex", alignItems: "center" }}>
                          {col.label}
                          <InfoTooltip label={copy(pageContract, "tooltip.sessions.label")}>
                            {copy(pageContract, "tooltip.sessions.body")}
                          </InfoTooltip>
                        </span>
                      ) : col.key === "manager" ? (
                        copy(pageContract, "label.operators")
                      ) : col.key === "backup" ? (
                        copy(pageContract, "label.assignment")
                      ) : (
                        col.label
                      )}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((row, rowIndex) => {
                  const href = detailHref(row);
                  const cell = (content: React.ReactNode, extra?: string, withRowLink = false) => (
                    <td className={extra}>
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
                    </td>
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
                    <tr key={partitionAwareKey} className="shed-summary-row">
                      {cell(
                        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
                          <MapPin className="ic" style={{ width: 13, opacity: 0.75, flexShrink: 0 }} aria-hidden="true" />
                          <ClipText title={row.parkName}>{row.parkName}</ClipText>
                        </span>,
                        undefined,
                        true,
                      )}
                      {cell(<ClipText title={row.operationalLocationDisplay || row.shedName}>{row.operationalLocationDisplay || row.shedName}</ClipText>)}
                      {cell(row.animals, "muted")}
                      {cell(row.due)}
                      {cell(row.done, "muted")}
                      {cell(row.sessions)}
                      {cell(row.nextDue ? fmtDate(row.nextDue) : copy(pageContract, "label.placeholder"), "muted")}
                      {cell(<DriveOperatorsCell row={row} pageContract={pageContract} />)}
                      {cell(<Tag tone={row.sessions > 1 ? "warn" : "mut"}>{assignmentLabel(row, pageContract)}</Tag>)}
                      {cell(
                        <Tag tone={optionTone(pageContract, "shed_status_chips", row.status) as Tone}>
                          {shedStatusLabel(pageContract, row.status)}
                        </Tag>,
                      )}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div className="note" style={{ margin: "10px 14px 0" }}>
            {copy(pageContract, "note.sheds_counts")}
          </div>
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
    </section>
  );
}
