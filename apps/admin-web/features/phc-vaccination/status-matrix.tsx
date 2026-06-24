import Link from "next/link";
import { Syringe } from "lucide-react";
import type { VaccinationOperationsResponse, VaccinationOperationsCell } from "@/lib/api/server";
import { WORK_STATE_META, type Tone } from "@/features/process-integrity";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";

// Vaccination status matrix (mock centerpiece). Source-backed: it renders the cohort × protocol cells from
// the /vaccination/operations read model — NOT a client-side pivot of capped Action Center rows. Each cell
// carries its computed work state and the real last accepted dose date (tooltip). Honest empty when the
// endpoint returns zero protocols/cohorts.
function cellMeta(workState: string): { label: string; tone: Tone } {
  const meta = WORK_STATE_META[workState as keyof typeof WORK_STATE_META];
  return meta ?? { label: workState, tone: "mut" };
}

function LegendSwatch({ varName, label }: { varName: string; label: string }) {
  return (
    <span>
      <i className="sw" style={{ background: `var(${varName})` }} aria-hidden="true" />
      {label}
    </span>
  );
}

export function VaccinationStatusMatrix({ operations, ok }: { operations: VaccinationOperationsResponse | null; ok: boolean }) {
  const protocols = operations?.protocols ?? [];
  const cohorts = operations?.cohorts ?? [];
  const empty = protocols.length === 0 || cohorts.length === 0;

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
              <Link href="/config?category=vaccination" className="btn sm p">
                Protocol Rules
              </Link>
              <Link href="/sops" className="btn sm">
                SOP Library
              </Link>
            </div>
          ) : null}
        </div>
      ) : (
        <div className="bd" style={{ padding: 0 }}>
          <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Vaccination status matrix">
            <table>
              <thead>
                <tr>
                  <th>Cohort</th>
                  {protocols.map((p) => (
                    <th key={p.protocolId}>{p.name}</th>
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
                        const meta = cellMeta(cell.workState);
                        const title = cell.lastDose ? `${p.name} — ${meta.label} · last dose ${fmtDate(cell.lastDose)}` : `${p.name} — ${meta.label}`;
                        return (
                          <td key={p.protocolId}>
                            <Tag tone={meta.tone} title={title}>
                              {meta.label}
                            </Tag>
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
            <Link href="/protocol-adherence" className="lk">
              Protocol Adherence
            </Link>
            ; act on individual drives in the{" "}
            <Link href="/action-center" className="lk">
              Action Center
            </Link>
            .
          </div>
        </div>
      )}
    </section>
  );
}
