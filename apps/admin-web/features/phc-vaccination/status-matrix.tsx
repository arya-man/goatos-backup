import Link from "next/link";
import { Syringe } from "lucide-react";
import type { VaccinationOperationsResponse, VaccinationOperationsCell } from "@/lib/api/server";
import type { Tone } from "@/features/process-integrity";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { scopeHref, type Scope } from "@/lib/scope";
import { VaccinationFilterButton, VisibleTableSearch } from "./vaccination-filter-modal";
import { VaccinationRecordVerifyDrawer } from "./record-verify-drawer";
import { VaccinationTablePager } from "./table-pager";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { sortVaccinationProtocols, vaccinationProtocolDisplayName } from "./vaccine-display";

// Matrix chips use the mock's short operational language. The backend work_state stays canonical; this is only
// display copy for the cohort x vaccine table.
function matrixCellMeta(cell: VaccinationOperationsCell): { label: string; tone: Tone } {
  switch (cell.workState) {
    case "completed":
      return { label: "done", tone: "ok" };
    case "overdue":
      return { label: "overdue", tone: "dng" };
    case "due":
      return { label: dueLabel(cell.nextDue), tone: "warn" };
    case "proof_pending":
      return { label: "video pending", tone: "warn" };
    case "scheduled":
      return { label: "scheduled", tone: "warn" };
    case "in_progress":
      return { label: "in progress", tone: "info" };
    case "verification_pending":
      return { label: "verify pending", tone: "warn" };
    case "rejected":
      return { label: "rework", tone: "dng" };
    case "deferred":
      return { label: "deferred", tone: "mut" };
    case "blocked":
      return { label: "blocked", tone: "dng" };
    case "owner_missing":
      return { label: "owner missing", tone: "dng" };
    default:
      return { label: String(cell.workState).replace(/_/g, " "), tone: "mut" };
  }
}

function dueLabel(nextDue: string | undefined): string {
  if (!nextDue) return "due";
  const due = new Date(nextDue);
  if (Number.isNaN(due.getTime())) return "due";
  const now = new Date();
  const dayMs = 24 * 60 * 60 * 1000;
  const days = Math.ceil((startOfDay(due).getTime() - startOfDay(now).getTime()) / dayMs);
  return days > 0 ? `due ${days}d` : "due";
}

function startOfDay(value: Date): Date {
  return new Date(value.getFullYear(), value.getMonth(), value.getDate());
}

function LegendSwatch({ varName, label }: { varName: string; label: string }) {
  return (
    <span>
      <i className="sw" style={{ background: `var(${varName})` }} aria-hidden="true" />
      {label}
    </span>
  );
}

export function VaccinationStatusMatrix({
  operations,
  ok,
  scope,
  searchParams,
}: {
  operations: VaccinationOperationsResponse | null;
  ok: boolean;
  scope: Scope;
  searchParams?: RouteSearchParams;
}) {
  const protocols = sortVaccinationProtocols(operations?.protocols ?? []);
  const cohorts = operations?.cohorts ?? [];
  const empty = protocols.length === 0 || cohorts.length === 0;
  const selectedId = one(searchParams ?? {}, "vacc_record");
  let selected: { id: string; cohort: (typeof cohorts)[number]; protocol: (typeof protocols)[number]; cell: VaccinationOperationsCell } | null = null;
  if (selectedId) {
    for (const cohort of cohorts) {
      for (const protocol of protocols) {
        const cell = cohort.cells.find((candidate) => candidate.protocolId === protocol.protocolId);
        const id = matrixRecordId(cohort, protocol.protocolId);
        if (cell && id === selectedId) selected = { id, cohort, protocol, cell };
      }
    }
  }

  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Syringe className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>Vaccination status matrix</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="legend">
          <LegendSwatch varName="--brand" label="up to date" />
          <LegendSwatch varName="--amber" label="due soon" />
          <LegendSwatch varName="--danger" label="overdue" />
        </span>
      </div>

      {empty ? (
        <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 16px", flexWrap: "wrap" }}>
          <Syringe className="ic" style={{ width: 18, height: 18, color: "var(--brand)", flexShrink: 0 }} aria-hidden="true" />
          <div style={{ minWidth: 0, flex: 1 }}>
            <b style={{ fontSize: 14 }}>{ok ? "No cohort × vaccine status yet" : "Status matrix is unavailable"}</b>
            <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
              {ok
                ? "Columns are the published vaccination protocols; rows are park/shed cohorts. Publish a source-backed protocol in Config and a vaccination SOP — obligations then generate against cohorts and fill this grid."
                : "Operations are unavailable until the service responds; resolve the error above and reload."}
            </span>
          </div>
          {ok ? (
            <div style={{ display: "flex", gap: 8, flexShrink: 0 }}>
              <Link href={scopeHref("/config", scope, {}, { category: "vaccination" })} className="btn sm p">
                Protocol Rules
              </Link>
              <Link href={scopeHref("/sops", scope)} className="btn sm">
                SOP Library
              </Link>
            </div>
          ) : null}
        </div>
      ) : (
        <div className="bd" style={{ padding: 0 }}>
          <div className="tbar">
            <VisibleTableSearch label="Search vaccination matrix rows" />
            <VaccinationFilterButton
              title="Filter — Vaccination status matrix"
              searchReason="Search cohort, protocol, vaccine, status..."
              filterReason="Use visible-row search and quick facets; click a cell to open the matching work context."
              rowsLabel={`${cohorts.length} rows · cohort × protocol`}
              actionHref={scopeHref("/action-center", scope)}
              actionLabel="Open Action Center"
              facets={["Cohort", "Protocol", "Vaccine", "Due window", "Work state"]}
            />
            <span className="muted small">{cohorts.length} rows</span>
            <span className="muted small">click a cell → record / verify</span>
          </div>
          <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Vaccination status matrix">
            <table>
              <thead>
                <tr>
                  <th>Cohort</th>
                  {protocols.map((p) => (
                    <th key={p.protocolId}>{vaccinationProtocolDisplayName(p)}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {cohorts.map((c) => {
                  const byProtocol = new Map<string, VaccinationOperationsCell>();
                  for (const cell of c.cells) byProtocol.set(cell.protocolId, cell);
                  return (
                    <tr key={`${c.parkId}|${c.shedId}|${c.stage}`}>
                      <td>
                        <b>{`${c.stage} · ${c.shedName}`}</b>
                        <div className="muted small">{c.parkName}</div>
                      </td>
                      {protocols.map((p) => {
                        const cell = byProtocol.get(p.protocolId);
                        if (!cell) {
                          return (
                            <td key={p.protocolId}>
                              <Tag tone="mut">—</Tag>
                            </td>
                          );
                        }
                        const meta = matrixCellMeta(cell);
                        const protocolLabel = vaccinationProtocolDisplayName(p);
                        const title = cell.lastDose ? `${protocolLabel} — ${meta.label} · last dose ${fmtDate(cell.lastDose)}` : `${protocolLabel} — ${meta.label}`;
                        return (
                          <td key={p.protocolId}>
                            <Link
                              href={scopeHref("/vaccination", scope, {}, { vacc_record: matrixRecordId(c, p.protocolId) })}
                              className="celllink"
                              title={title}
                              scroll={false}
                            >
                              <Tag tone={meta.tone}>{meta.label}</Tag>
                            </Link>
                          </td>
                        );
                      })}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div className="note" style={{ margin: "12px 14px" }}>
            Cells are keyed on each cohort&apos;s open obligations; <b>last dose</b> (hover) is the latest accepted
            administered dose. Overdue cells escalate via{" "}
            <Link href={scopeHref("/protocol-adherence", scope)} className="lk">
              Protocol Adherence
            </Link>
            ; act on individual drives in the{" "}
            <Link href={scopeHref("/action-center", scope)} className="lk">
              Action Center
            </Link>
            .
          </div>
          <VaccinationTablePager rows={cohorts.length} noun="cohort" />
        </div>
      )}
      {selected ? (
        <VaccinationRecordVerifyDrawer
          context={{ cohort: selected.cohort, protocol: selected.protocol, cell: selected.cell }}
          scope={scope}
        />
      ) : null}
    </section>
  );
}

function matrixRecordId(cohort: VaccinationOperationsResponse["cohorts"][number], protocolId: string): string {
  return `${cohort.parkId}|${cohort.shedId}|${cohort.stage}|${protocolId}`;
}
