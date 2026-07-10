import Link from "next/link";
import { Ban, ChevronRight, Layers, MapPin, ShieldCheck, Syringe, UserRound, Warehouse, X } from "lucide-react";
import { getVaccinationExecution, type ApiResult } from "@/lib/api/server";
import type {
  VaccinationExecutionResponse,
  VaccinationExecutionRow,
  VaccinationExecutionSeverity,
  VaccinationExecutionWorkState,
} from "@/lib/api/vaccination-execution";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import {
  SEVERITY_ORDER,
  SEVERITY_RANK,
  WORK_STATE_ORDER,
} from "./work-state";
import { ClipText, Tag, type Tone } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
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

// Work states that mean "someone must act now" — used for the per-park attention count.
const ATTENTION_STATES = new Set<VaccinationExecutionWorkState>(["overdue", "missed", "blocked", "rejected"]);

interface ParkGroup {
  parkId: string;
  parkName: string;
  rows: VaccinationExecutionRow[];
  severity: VaccinationExecutionSeverity;
  attention: number;
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

// Owner chain: operator (ground) -> park head -> verifier (Video Verification Team).
function OwnerChain({ row, pageContract }: { row: VaccinationExecutionRow; pageContract: AdminUiPageContract }) {
  const o = row.owner;
  const operatorMissing = !o?.operatorName;
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4, minWidth: 0 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
        <UserRound className="ic" style={{ width: 13, opacity: 0.75, flexShrink: 0 }} aria-hidden="true" />
        {operatorMissing ? (
          <Tag tone="dng" title={copy(pageContract, "reason.no_operator")}>{copy(pageContract, "label.operator_unassigned")}</Tag>
        ) : (
          <span className="small">{o?.operatorName}</span>
        )}
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
        <MapPin className="ic" style={{ width: 13, opacity: 0.75, flexShrink: 0 }} aria-hidden="true" />
        <span className="small muted">{o?.parkHeadName ?? copy(pageContract, "label.park_head_unassigned")}</span>
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
        <ShieldCheck className="ic" style={{ width: 13, opacity: 0.75, flexShrink: 0 }} aria-hidden="true" />
        <span className="small muted">{o?.verifierName ?? copy(pageContract, "label.verifier_default")}</span>
      </div>
    </div>
  );
}

function StatusChips({ row, pageContract }: { row: VaccinationExecutionRow; pageContract: AdminUiPageContract }) {
  return (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
      {row.sopStatus ? <Tag tone={optionTone(pageContract, "sop_state_chips", row.sopStatus) as Tone}>{optionLabel(pageContract, "sop_state_chips", row.sopStatus)}</Tag> : null}
      {row.proofStatus ? <Tag tone={optionTone(pageContract, "proof_state_chips", row.proofStatus) as Tone}>{optionLabel(pageContract, "proof_state_chips", row.proofStatus)}</Tag> : null}
      {row.verificationStatus ? (
        <Tag tone={optionTone(pageContract, "verification_state_chips", row.verificationStatus) as Tone}>{optionLabel(pageContract, "verification_state_chips", row.verificationStatus)}</Tag>
      ) : null}
    </div>
  );
}

function executionDriveLabel(row: VaccinationExecutionRow): string {
  // Canonical vaccine label + dose phase ("FMD · Primary") — strips the TEST-ONLY/seed-code noise and reads
  // consistently with the status-matrix column headers.
  return vaccinationDriveDisplayName(row.driveName);
}

function executionActionTitle(pageContract: AdminUiPageContract, row: VaccinationExecutionRow): string {
  if (row.proofStatus === "missing") return `${copy(pageContract, "action.capture_vaccination_proof")} — ${row.shedName}`;
  if (row.verificationStatus === "pending") return `${copy(pageContract, "action.verify_vaccination_proof")} — ${row.shedName}`;
  if (row.workState === "overdue") return `${executionDriveLabel(row)} ${copy(pageContract, "label.overdue")} — ${row.shedName}`;
  return `${executionDriveLabel(row)} — ${row.shedName}`;
}

function ExecutionRow({ row, drawerHref, pageContract, labels }: { row: VaccinationExecutionRow; drawerHref: string; pageContract: AdminUiPageContract; labels: string[] }) {
  const driveLabel = executionDriveLabel(row);
  return (
    <Link
      href={drawerHref}
      scroll={false}
      className="pexr"
      aria-label={`${copy(pageContract, "action.open_shed_event_for")} ${row.shedName}`}
      title={`${row.shedName} · ${driveLabel} · ${row.nextAction}`}
    >
      <div className="pexc pexc-shed">
        <div className="pexc-h">{labels[0]}</div>
        <span className="lk small" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <Warehouse className="ic" style={{ width: 14 }} aria-hidden="true" />
          <ClipText title={row.shedName} className="inline">
            {row.shedName}
          </ClipText>
        </span>
        <ClipText title={row.animalStage} className="muted small" style={{ marginTop: 2 }}>
          {row.animalStage}
        </ClipText>
      </div>
      <div className="pexc pexc-drive">
        <div className="pexc-h">{labels[1]}</div>
        <span className="small pexec-line" style={{ display: "flex", alignItems: "center", gap: 6, minWidth: 0 }}>
          <Syringe className="ic" style={{ width: 13, opacity: 0.75, flexShrink: 0 }} aria-hidden="true" />
          <ClipText title={driveLabel} className="inline pexec-drive-title">
            {driveLabel}
          </ClipText>
        </span>
        <div className="muted small" style={{ marginTop: 2 }}>{copy(pageContract, "label.due_prefix")} {fmtDate(row.dueDate)}</div>
      </div>
      <div className="pexc pexc-work">
        <div className="pexc-h">{labels[2]}</div>
        <Tag tone={optionTone(pageContract, "work_state_filter_chips", row.workState) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", row.workState)}</Tag>
        {row.blockerReason ? (
          <div
            className="small"
            title={row.blockerReason}
            style={{ display: "flex", alignItems: "center", gap: 6, color: "var(--danger)", marginTop: 6 }}
          >
            <Ban className="ic" style={{ width: 13, flexShrink: 0 }} aria-hidden="true" />
            {/* Concise head on the list ("ICU / quarantine"); full reason on hover + in the drilldown. */}
            <span style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
              {row.blockerReason.split(" — ")[0]}
            </span>
          </div>
        ) : null}
      </div>
      <div className="pexc pexc-owner">
        <div className="pexc-h">{labels[3]}</div>
        <OwnerChain row={row} pageContract={pageContract} />
      </div>
      <div className="pexc pexc-status">
        <div className="pexc-h">{labels[4]}</div>
        <StatusChips row={row} pageContract={pageContract} />
      </div>
      <div className="pexc pexc-action">
        <div className="pexc-h">{labels[5]}</div>
        {/* Backend-suggested next step — a HINT, not a wired button. The row itself opens the drawer; owner
            assignment isn't actionable yet, so this must not masquerade as a CTA button. Plain muted text. */}
        <ClipText title={row.nextAction} className="small muted" style={{ display: "block", maxWidth: 240, lineHeight: 1.3 }}>
          {row.nextAction}
        </ClipText>
      </div>
    </Link>
  );
}

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
  const { parkId, asOf } = backendScope(scope);

  const result =
    executionResult ??
    (await getVaccinationExecution({
      parkId,
      asOf,
      workState: stateFilter === "all" ? undefined : stateFilter,
      limit: 500,
    }));
  const allRows: VaccinationExecutionRow[] = result.ok ? result.data.rows : [];

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
  const pageSizeOptions = tablePageSizes(pageContract, "shed-events");
  const paged = paginateRows(rows, sp, "exec", 10, pageSizeOptions);
  const parks = groupByPark(paged.items);
  const selectedEventId = one(sp, "shed_event");
  const selectedEvent = selectedEventId ? rows.find((row) => shedEventId(row) === selectedEventId) : undefined;

  // work_state is applied SERVER-SIDE and the result is capped (limit), so per-state counts are only
  // meaningful when no state filter is active. Park scope belongs to the shell top bar / Filters.
  const showStateCounts = stateFilter === "all";
  const capped = result.ok && allRows.length >= 500;
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
    return hrefWith({ exec_page: "1", exec_limit: String(pageSize) });
  }

  return (
    <>
    <div data-filter-scope>
      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      {/* Filter chrome (severity + work-state + provenance) is hidden when there is genuinely no work — the
          chips would all read 0. It returns the instant any row exists or a filter is active. */}
      {!noWork && (
        <>
      <div className="tbar" style={{ marginBottom: 10, border: "1px solid var(--line2)", borderRadius: 10 }}>
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
        <span className="muted small">
          {paged.start}-{paged.end} {copy(pageContract, "pager.of")} {rows.length} {copy(pageContract, "pager.rows").toLowerCase()}
        </span>
        <span className="muted small">{copy(pageContract, "section.shed_events.row_hint")}</span>
      </div>

      {/* Severity filter */}
      <div className="chipset" style={{ marginBottom: 10 }}>
        <Link href={hrefWith({ severity: "all", exec_page: "1" })} replace scroll={false} className={`chip${severityFilter === "all" ? " on" : ""}`}>
          {copy(pageContract, "label.all_severity")}
        </Link>
        {SEVERITY_ORDER.map((s) => {
          const count = sevCounts.get(s) ?? 0;
          return (
            <Link key={s} href={hrefWith({ severity: s, exec_page: "1" })} replace scroll={false} className={`chip${severityFilter === s ? " on" : ""}`}>
              {optionLabel(pageContract, "severity_chips", s)} <Tag tone={severityFilter === s ? (optionTone(pageContract, "severity_chips", s) as Tone) : "mut"}>{count}</Tag>
            </Link>
          );
        })}
      </div>

      {/* Work-state filter board (most-broken first). Server-side filter: always render every state as
          navigation (so selecting one never collapses the board), count only in the unfiltered view. */}
      <div className="chipset" style={{ marginBottom: 8 }}>
        <Link href={hrefWith({ state: "all", exec_page: "1" })} replace scroll={false} className={`chip${stateFilter === "all" ? " on" : ""}`}>
          {copy(pageContract, "label.all_states")} {showStateCounts ? <Tag tone={stateFilter === "all" ? "ok" : "mut"}>{allRows.length}</Tag> : null}
        </Link>
        {(showStateCounts ? WORK_STATE_ORDER.filter((s) => (stateCounts.get(s) ?? 0) > 0) : WORK_STATE_ORDER).map((s) => (
          <Link key={s} href={hrefWith({ state: s, exec_page: "1" })} replace scroll={false} className={`chip${stateFilter === s ? " on" : ""}`}>
            {optionLabel(pageContract, "work_state_filter_chips", s)}
            {showStateCounts ? <> <Tag tone="mut">{stateCounts.get(s) ?? 0}</Tag></> : null}
          </Link>
        ))}
      </div>

      {/* Honest provenance: counts/rows come from a bounded, server-filtered fetch — not tenant-wide totals. */}
      {result.ok ? (
        <div className="muted small" style={{ marginBottom: 16 }}>
          {showStateCounts
            ? `${copy(pageContract, "note.execution_counts")}${capped ? ` ${copy(pageContract, "note.execution_counts_capped")}` : ""}.`
            : copy(pageContract, "note.execution_counts_filtered")}
        </div>
      ) : null}
        </>
      )}

      {parks.length === 0 ? (
        <section className="card">
          <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 16px", flexWrap: "wrap" }}>
            <Layers className="ic" aria-hidden="true" style={{ width: 18, height: 18, color: "var(--brand)", flexShrink: 0 }} />
            <div style={{ minWidth: 0, flex: 1 }}>
              <b style={{ fontSize: 14 }}>
                {!result.ok
                  ? copy(pageContract, "section.shed_events.empty_unavailable_title")
                  : noWork
                    ? copy(pageContract, "section.shed_events.empty_none_title")
                    : copy(pageContract, "section.shed_events.empty_filtered_title")}
              </b>
              <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
                {!result.ok
                  ? copy(pageContract, "section.shed_events.empty_unavailable_body")
                  : noWork
                    ? copy(pageContract, "section.shed_events.empty_none_body")
                    : copy(pageContract, "section.shed_events.empty_filtered_body")}
              </span>
            </div>
            {result.ok && !noWork ? (
              <Link href={resetHref} replace scroll={false} className="btn sm">
                {copy(pageContract, "action.reset_filters")}
              </Link>
            ) : null}
          </div>
        </section>
      ) : (
        <>
        <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          {parks.map((park) => (
            <section className="card" key={park.parkId}>
              <div className="hd">
                <MapPin className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
                <h3>{park.parkName}</h3>
                <Tag tone={optionTone(pageContract, "severity_chips", park.severity) as Tone}>{optionLabel(pageContract, "severity_chips", park.severity)}</Tag>
                <div className="sp" style={{ flex: 1 }} />
                {park.attention > 0 ? (
                  // Scope to this park (top-bar scope override, NOT a stray filter param) AND filter to the
                  // attention rows (severity=broken) so the click actually narrows the board instead of being
                  // a no-op reset.
                  <Link
                    href={scopeHref(basePath, scope, { park: park.parkId, mode: "park" }, { ...baseParams, severity: "broken", state: stateFilter })}
                    replace
                    scroll={false}
                    className="btn gh sm"
                  >
                    {park.attention} {copy(pageContract, "label.need_attention")}
                  </Link>
                ) : (
                  <span className="muted small">{copy(pageContract, "label.on_track")}</span>
                )}
              </div>
              <div className="pexec" role="group" aria-label={`${park.parkName} ${copy(pageContract, "section.shed_events.aria")}`}>
                <div className="pexh">
                  {labels.map((label) => (
                    <div key={label}>{label}</div>
                  ))}
                </div>
                {park.rows.map((row, idx) => (
                  <ExecutionRow
                    key={`${row.shedId}-${row.driveId ?? idx}`}
                    row={row}
                    drawerHref={hrefWith({ shed_event: shedEventId(row) })}
                    pageContract={pageContract}
                    labels={labels}
                  />
                ))}
              </div>
              <div className="bd" style={{ paddingTop: 12 }}>
                <Link
                  href={scopeHref("/action-center", scope, { park: park.parkId, mode: "park" })}
                  className="lk small"
                  style={{ display: "inline-flex", alignItems: "center", gap: 4 }}
                >
                  {copy(pageContract, "action.open_park_action_center")}
                  <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
                </Link>
              </div>
            </section>
          ))}
        </div>
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
        </>
      )}
      {selectedEvent ? <ShedEventDrawer row={selectedEvent} scope={scope} closeHref={hrefWith({ shed_event: undefined })} pageContract={pageContract} /> : null}
    </div>
    </>
  );
}

function shedEventId(row: VaccinationExecutionRow): string {
  return `${row.shedId}|${row.driveId ?? "drive"}|${row.animalStage}`;
}

function ShedEventDrawer({ row, scope, closeHref, pageContract }: { row: VaccinationExecutionRow; scope: ReturnType<typeof parseScope>; closeHref: string; pageContract: AdminUiPageContract }) {
  const driveLabel = executionDriveLabel(row);
  const detailHref = scopeHref(`/vaccination/execution/sheds/${encodeURIComponent(row.shedId)}`, scope, { mode: "park", park: row.parkId });
  const actionCenterHref = scopeHref("/action-center", scope, {}, { state: row.workState });
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label={copy(pageContract, "drawer.shed_event.close_label")} scroll={false} />
      <aside className="drawer on" aria-label={copy(pageContract, "drawer.shed_event.aria")}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Syringe className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "drawer.shed_event.eyebrow")}</div>
            <h2>{executionActionTitle(pageContract, row)}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label={copy(pageContract, "drawer.shed_event.close_label")} scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="metagrid">
            <div>
              <div className="k">{copy(pageContract, "drawer.shed_event.shed_event")}</div>
              <div className="v">{driveLabel}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.shed_event.shed")}</div>
              <div className="v">{row.shedName}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.shed_event.owner_assist")}</div>
              <div className="v">{row.owner?.operatorName ?? copy(pageContract, "label.owner_chain_to_assign")}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.shed_event.stock")}</div>
              <div className="v">{copy(pageContract, "label.stock_resolved_action_center")}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.shed_event.status")}</div>
              <div className="v">
                <Tag tone={optionTone(pageContract, "work_state_filter_chips", row.workState) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", row.workState)}</Tag>
              </div>
            </div>
          </div>
          <VaccinationRecordFormFields cohortShed={`${row.animalStage} · ${row.shedName}`} vaccineName={driveLabel} pageContract={pageContract} />
          <div style={{ marginTop: 16 }}>
            <ShedEventActions pageContract={pageContract} />
          </div>
        </div>
        <div className="df">
          <Link href={actionCenterHref} className="btn" scroll={false}>
            {copy(pageContract, "action.open_action_center")}
          </Link>
          <Link href={detailHref} className="btn">
            {copy(pageContract, "action.shed_detail")}
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            {copy(pageContract, "action.close")}
          </Link>
        </div>
      </aside>
    </>
  );
}
