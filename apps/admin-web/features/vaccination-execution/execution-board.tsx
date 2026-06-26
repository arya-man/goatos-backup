import Link from "next/link";
import { Ban, ChevronRight, Layers, MapPin, ShieldCheck, Syringe, UserRound, Warehouse, X } from "lucide-react";
import { getVaccinationExecution } from "@/lib/api/server";
import type {
  VaccinationExecutionRow,
  VaccinationExecutionSeverity,
  VaccinationExecutionWorkState,
} from "@/lib/api/vaccination-execution";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import {
  PROOF_META,
  SEVERITY_META,
  SEVERITY_ORDER,
  SEVERITY_RANK,
  SOP_META,
  VERIFICATION_META,
  WORK_STATE_META,
  WORK_STATE_ORDER,
} from "./work-state";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { VaccinationFilterButton, VisibleTableSearch } from "@/features/phc-vaccination/vaccination-filter-modal";
import { VaccinationRecordFormFields } from "@/features/phc-vaccination/record-verify-drawer";
import { vaccinationDriveDisplayName } from "@/features/phc-vaccination/vaccine-display";
import { VaccinationTablePager } from "@/features/phc-vaccination/table-pager";
import { ShedEventActions } from "./shed-event-actions";

// Work states that mean "someone must act now" — used for the per-park attention count.
const ATTENTION_STATES = new Set<VaccinationExecutionWorkState>(["overdue", "blocked", "owner_missing", "rejected"]);

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
function OwnerChain({ row }: { row: VaccinationExecutionRow }) {
  const o = row.owner;
  const operatorMissing = !o?.operatorName;
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4, minWidth: 0 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
        <UserRound className="ic" style={{ width: 13, opacity: 0.75, flexShrink: 0 }} aria-hidden="true" />
        {operatorMissing ? (
          <Tag tone="dng" title="No operator assigned to this shed drive">operator: unassigned</Tag>
        ) : (
          <span className="small">{o?.operatorName}</span>
        )}
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
        <MapPin className="ic" style={{ width: 13, opacity: 0.75, flexShrink: 0 }} aria-hidden="true" />
        <span className="small muted">{o?.parkHeadName ?? "park head: unassigned"}</span>
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
        <ShieldCheck className="ic" style={{ width: 13, opacity: 0.75, flexShrink: 0 }} aria-hidden="true" />
        <span className="small muted">{o?.verifierName ?? "Video Verification Team"}</span>
      </div>
    </div>
  );
}

function StatusChips({ row }: { row: VaccinationExecutionRow }) {
  return (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
      {row.sopStatus ? <Tag tone={SOP_META[row.sopStatus].tone}>{SOP_META[row.sopStatus].label}</Tag> : null}
      {row.proofStatus ? <Tag tone={PROOF_META[row.proofStatus].tone}>{PROOF_META[row.proofStatus].label}</Tag> : null}
      {row.verificationStatus ? (
        <Tag tone={VERIFICATION_META[row.verificationStatus].tone}>{VERIFICATION_META[row.verificationStatus].label}</Tag>
      ) : null}
    </div>
  );
}

function executionDriveLabel(row: VaccinationExecutionRow): string {
  // Canonical vaccine label + dose phase ("FMD · Primary") — strips the TEST-ONLY/seed-code noise and reads
  // consistently with the status-matrix column headers.
  return vaccinationDriveDisplayName(row.driveName);
}

function executionActionTitle(row: VaccinationExecutionRow): string {
  if (row.workState === "owner_missing") return `Assign owner chain — ${row.shedName}`;
  if (row.proofStatus === "missing") return `Capture vaccination proof — ${row.shedName}`;
  if (row.verificationStatus === "pending") return `Verify vaccination proof — ${row.shedName}`;
  if (row.workState === "overdue") return `${executionDriveLabel(row)} overdue — ${row.shedName}`;
  return `${executionDriveLabel(row)} — ${row.shedName}`;
}

function ExecutionRow({ row, drawerHref }: { row: VaccinationExecutionRow; drawerHref: string }) {
  const meta = WORK_STATE_META[row.workState];
  const driveLabel = executionDriveLabel(row);
  return (
    <Link href={drawerHref} scroll={false} className="pexr" aria-label={`Open vaccination shed event for ${row.shedName}`}>
      <div className="pexc">
        <div className="pexc-h">Shed · stage</div>
        <span className="lk small" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <Warehouse className="ic" style={{ width: 14 }} aria-hidden="true" />
          {row.shedName}
        </span>
        <div className="muted small" style={{ marginTop: 2 }}>{row.animalStage}</div>
      </div>
      <div className="pexc">
        <div className="pexc-h">Drive · due</div>
        <span className="small" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <Syringe className="ic" style={{ width: 13, opacity: 0.75, flexShrink: 0 }} aria-hidden="true" />
          {driveLabel}
        </span>
        <div className="muted small" style={{ marginTop: 2 }}>due {fmtDate(row.dueDate)}</div>
      </div>
      <div className="pexc">
        <div className="pexc-h">Work state</div>
        <Tag tone={meta.tone}>{meta.label}</Tag>
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
      <div className="pexc">
        <div className="pexc-h">Owner chain</div>
        <OwnerChain row={row} />
      </div>
      <div className="pexc">
        <div className="pexc-h">SOP · proof · verify</div>
        <StatusChips row={row} />
      </div>
      <div className="pexc">
        <div className="pexc-h">Next action</div>
        {/* Backend-suggested next step — a HINT, not a wired button. The row itself opens the drawer; owner
            assignment isn't actionable yet, so this must not masquerade as a CTA button. Plain muted text. */}
        <span className="small muted" style={{ display: "block", maxWidth: 240, lineHeight: 1.3 }}>
          {row.nextAction}
        </span>
      </div>
    </Link>
  );
}

// Embeddable execution section. Parks does NOT own a vaccination product surface — this renders INSIDE
// PHC / Vaccination (/vaccination#execution), scoped by the top-bar park dropdown (?park). The
// `basePath` parameterizes the filter self-links so they stay on the embedding route; shed-drilldown rows
// deep-link to /vaccination/execution/sheds/{shed_id} (physical execution-context detail).
export async function VaccinationExecutionBoard({
  searchParams,
  basePath = "/vaccination",
  baseParams = {},
}: {
  searchParams?: RouteSearchParams;
  basePath?: string;
  // Query params always kept on filter/reset links (e.g. {section:"execution"}) so the embedding tab stays selected.
  baseParams?: Record<string, string>;
}) {
  const sp = searchParams ?? {};
  const stateFilter = (WORK_STATE_ORDER.find((s) => s === one(sp, "state")) ?? "all") as VaccinationExecutionWorkState | "all";
  const severityFilter = (SEVERITY_ORDER.find((s) => s === one(sp, "severity")) ?? "all") as VaccinationExecutionSeverity | "all";
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);

  const result = await getVaccinationExecution({
    parkId,
    asOf,
    workState: stateFilter === "all" ? undefined : stateFilter,
    limit: 500,
  });
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

  const parks = groupByPark(rows);
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
    return scopeHref(basePath, scope, {}, { ...baseParams, severity: severityFilter, state: stateFilter, ...overrides });
  }
  // Reset clears the page filters but keeps the full top-bar scope + embedding params.
  const resetHref = scopeHref(basePath, scope, {}, baseParams);

  return (
    <>
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
        <VisibleTableSearch label="Search vaccination shed events" />
        <VaccinationFilterButton
          title="Filter — Vaccination shed events"
          searchReason="Search shed, owner, proof, status..."
          filterReason="Use visible-row search, quick facets, severity chips, and work-state chips on this board."
          rowsLabel={`${rows.length} rows · park, shed, owner, proof, verify`}
          actionHref={scopeHref("/action-center", scope)}
          actionLabel="Open Action Center"
          facets={["Owner", "Proof status", "Verification status", "SOP status", "Due window"]}
        />
        <span className="muted small">{rows.length} rows</span>
        <span className="muted small">click a row → shed execution detail</span>
      </div>

      {/* Severity filter */}
      <div className="chipset" style={{ marginBottom: 10 }}>
        <Link href={hrefWith({ severity: "all" })} replace scroll={false} className={`chip${severityFilter === "all" ? " on" : ""}`}>
          All severity
        </Link>
        {SEVERITY_ORDER.map((s) => {
          const count = sevCounts.get(s) ?? 0;
          return (
            <Link key={s} href={hrefWith({ severity: s })} replace scroll={false} className={`chip${severityFilter === s ? " on" : ""}`}>
              {SEVERITY_META[s].label} <Tag tone={severityFilter === s ? SEVERITY_META[s].tone : "mut"}>{count}</Tag>
            </Link>
          );
        })}
      </div>

      {/* Work-state filter board (most-broken first). Server-side filter: always render every state as
          navigation (so selecting one never collapses the board), count only in the unfiltered view. */}
      <div className="chipset" style={{ marginBottom: 8 }}>
        <Link href={hrefWith({ state: "all" })} replace scroll={false} className={`chip${stateFilter === "all" ? " on" : ""}`}>
          All states {showStateCounts ? <Tag tone={stateFilter === "all" ? "ok" : "mut"}>{allRows.length}</Tag> : null}
        </Link>
        {(showStateCounts ? WORK_STATE_ORDER.filter((s) => (stateCounts.get(s) ?? 0) > 0) : WORK_STATE_ORDER).map((s) => (
          <Link key={s} href={hrefWith({ state: s })} replace scroll={false} className={`chip${stateFilter === s ? " on" : ""}`}>
            {WORK_STATE_META[s].label}
            {showStateCounts ? <> <Tag tone="mut">{stateCounts.get(s) ?? 0}</Tag></> : null}
          </Link>
        ))}
      </div>

      {/* Honest provenance: counts/rows come from a bounded, server-filtered fetch — not tenant-wide totals. */}
      {result.ok ? (
        <div className="muted small" style={{ marginBottom: 16 }}>
          {showStateCounts
            ? `Counts reflect the returned result set (max 500 rows), scoped by the top bar and filters${capped ? " — result is capped; narrow with Filters" : ""}.`
            : "Work-state filters are applied server-side; counts are hidden while filtered. Severity narrows the returned rows in this view."}
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
                {!result.ok ? "Park/shed execution is unavailable" : noWork ? "No park/shed execution rows yet" : "No shed work matches these filters"}
              </b>
              <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
                {!result.ok
                  ? "The vaccination execution service did not return data. Resolve the error above, then reload."
                  : noWork
                    ? "Rows appear once a published vaccination drive generates obligations against a park / shed cohort."
                    : "Clear a filter to see other parks and sheds."}
              </span>
            </div>
            {result.ok && !noWork ? (
              <Link href={resetHref} replace scroll={false} className="btn sm">
                Reset filters
              </Link>
            ) : null}
          </div>
        </section>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          {parks.map((park) => (
            <section className="card" key={park.parkId}>
              <div className="hd">
                <MapPin className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
                <h3>{park.parkName}</h3>
                <Tag tone={SEVERITY_META[park.severity].tone}>{SEVERITY_META[park.severity].label}</Tag>
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
                    {park.attention} need attention
                  </Link>
                ) : (
                  <span className="muted small">on track</span>
                )}
              </div>
              <div className="pexec" role="group" aria-label={`${park.parkName} shed vaccination rows`}>
                <div className="pexh">
                  <div>Shed · stage</div>
                  <div>Drive · due</div>
                  <div>Work state</div>
                  <div>Owner chain</div>
                  <div>SOP · proof · verify</div>
                  <div>Next action</div>
                </div>
                {park.rows.map((row, idx) => (
                  <ExecutionRow
                    key={`${row.shedId}-${row.driveId ?? idx}`}
                    row={row}
                    drawerHref={hrefWith({ shed_event: shedEventId(row) })}
                  />
                ))}
              </div>
              <VaccinationTablePager rows={park.rows.length} noun="shed event" />
              <div className="bd" style={{ paddingTop: 12 }}>
                <Link
                  href={scopeHref("/action-center", scope, { park: park.parkId, mode: "park" })}
                  className="lk small"
                  style={{ display: "inline-flex", alignItems: "center", gap: 4 }}
                >
                  Open {park.parkName} in the Action Center
                  <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
                </Link>
              </div>
            </section>
          ))}
        </div>
      )}
      {selectedEvent ? <ShedEventDrawer row={selectedEvent} scope={scope} closeHref={hrefWith({ shed_event: undefined })} /> : null}
    </>
  );
}

function shedEventId(row: VaccinationExecutionRow): string {
  return `${row.shedId}|${row.driveId ?? "drive"}|${row.animalStage}`;
}

function ShedEventDrawer({ row, scope, closeHref }: { row: VaccinationExecutionRow; scope: ReturnType<typeof parseScope>; closeHref: string }) {
  const meta = WORK_STATE_META[row.workState];
  const driveLabel = executionDriveLabel(row);
  const detailHref = `/vaccination/execution/sheds/${encodeURIComponent(row.shedId)}`;
  const actionCenterHref = scopeHref("/action-center", scope, {}, { state: row.workState });
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label="Close shed event drawer" scroll={false} />
      <aside className="drawer on" aria-label="Vaccination shed event">
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Syringe className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">RECORD</div>
            <h2>{executionActionTitle(row)}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label="Close shed event drawer" scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="metagrid">
            <div>
              <div className="k">Shed event</div>
              <div className="v">{driveLabel}</div>
            </div>
            <div>
              <div className="k">Shed</div>
              <div className="v">{row.shedName}</div>
            </div>
            <div>
              <div className="k">Owner → assist</div>
              <div className="v">{row.owner?.operatorName ?? "owner chain to assign"}</div>
            </div>
            <div>
              <div className="k">Stock (FEFO)</div>
              <div className="v">resolved in Action Center</div>
            </div>
            <div>
              <div className="k">Status</div>
              <div className="v">
                <Tag tone={meta.tone}>{meta.label}</Tag>
              </div>
            </div>
          </div>
          <VaccinationRecordFormFields cohortShed={`${row.animalStage} · ${row.shedName}`} vaccineName={driveLabel} />
          <div style={{ marginTop: 16 }}>
            <ShedEventActions
              obligationId={row.obligationId}
              sopTaskId={row.sopTaskId}
              completionId={row.completionId}
            />
          </div>
        </div>
        <div className="df">
          <Link href={actionCenterHref} className="btn" scroll={false}>
            Open Action Center
          </Link>
          <Link href={detailHref} className="btn">
            Shed detail
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            Close
          </Link>
        </div>
      </aside>
    </>
  );
}
