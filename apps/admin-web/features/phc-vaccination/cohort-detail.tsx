import { Syringe } from "lucide-react";
import type { VaccinationOperationsResponse } from "@/lib/api/server";
import { WORK_STATE_META, type Tone } from "@/features/process-integrity";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";

// Per-cohort vaccination detail (mock table). Source-backed from /vaccination/operations: one row per
// cohort with headcount, age band, REAL last dose (latest accepted administered_at), next due, and worst
// computed status. No Action Center pivot, no faked last_dose.
function statusTag(workState: string) {
  if (workState === "completed") return <Tag tone="ok">on schedule</Tag>;
  const meta = WORK_STATE_META[workState as keyof typeof WORK_STATE_META] as { label: string; tone: Tone } | undefined;
  return <Tag tone={meta?.tone ?? "mut"}>{meta?.label ?? workState}</Tag>;
}

export function VaccinationCohortDetail({ operations, ok }: { operations: VaccinationOperationsResponse | null; ok: boolean }) {
  const cohorts = operations?.cohorts ?? [];
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Syringe className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>Per-cohort vaccination detail</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="small muted">animals · age band · last dose · next due</span>
      </div>
      {cohorts.length === 0 ? (
        <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 16px", flexWrap: "wrap" }}>
          <Syringe className="ic" style={{ width: 18, height: 18, color: "var(--brand)", flexShrink: 0 }} aria-hidden="true" />
          <span className="muted small" style={{ lineHeight: 1.5 }}>
            {ok
              ? "No cohorts with vaccination obligations yet. Rows appear per park/shed cohort once a protocol is published and drives generate."
              : "Cohort detail is unavailable until the service responds; resolve the error above and reload."}
          </span>
        </div>
      ) : (
        <div className="bd" style={{ padding: 0 }}>
          <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Per-cohort vaccination detail">
            <table>
              <thead>
                <tr>
                  <th>Cohort</th>
                  <th>Animals</th>
                  <th>Age band</th>
                  <th>Last dose</th>
                  <th>Next due</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {cohorts.map((c) => (
                  <tr key={`${c.parkId}|${c.shedId}|${c.stage}`}>
                    <td>
                      <b>{`${c.stage} · ${c.shedName}`}</b>
                      <div className="muted small">{c.parkName}</div>
                    </td>
                    <td className="muted">{c.animals || "—"}</td>
                    <td>
                      <Tag tone="info">{c.ageBand ?? c.stage}</Tag>
                    </td>
                    <td className="muted">{c.lastDose ? fmtDate(c.lastDose) : "—"}</td>
                    <td className="muted">{c.nextDue ? fmtDate(c.nextDue) : "—"}</td>
                    <td>{statusTag(c.workState)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="note" style={{ margin: "12px 14px" }}>
            Status is keyed on the cohort&apos;s open obligations + interval. <b>Last dose</b> is the latest accepted
            administered dose for the cohort; animal counts drive dose quantities and FEFO stock reserves on verify.
          </div>
        </div>
      )}
    </section>
  );
}
