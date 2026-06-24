import Link from "next/link";
import { ArrowLeft, Ban, MapPin, ShieldCheck, Syringe, UserRound, Warehouse } from "lucide-react";
import { getVaccinationExecutionShedDrilldown } from "@/lib/api/server";
import type { VaccinationExecutionRow } from "@/lib/api/vaccination-execution";
import { PROOF_META, SEVERITY_META, SOP_META, VERIFICATION_META, WORK_STATE_META, type Tone } from "./work-state";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";

function Stat({ label, value, tone }: { label: string; value: number; tone: Tone }) {
  return (
    <div>
      <div className="k">{label}</div>
      <div className="v" style={{ display: "flex", alignItems: "center", gap: 6 }}>
        {value}
        {value > 0 ? <Tag tone={tone}>{tone === "ok" ? "done" : "open"}</Tag> : null}
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

function NotFoundOrError({ shedId, message }: { shedId: string; message: string }) {
  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <Link href="/vaccination#execution" className="lk">
              PHC · Vaccination · Execution
            </Link>
          </div>
          <h1>Shed unavailable</h1>
          <div className="sub">{message}</div>
        </div>
      </div>
      <section className="card">
        <div className="bd">
          <p className="muted small" style={{ marginBottom: 12 }}>
            Shed “{shedId}” returned no vaccination execution context. It may be outside the current drive scope, or the
            service is unavailable.
          </p>
          <Link href="/vaccination#execution" className="btn">
            <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> Back to vaccination execution
          </Link>
        </div>
      </section>
    </div>
  );
}

export async function ShedExecutionDetailPage({ shedId, asOf }: { shedId: string; asOf?: string }) {
  const result = await getVaccinationExecutionShedDrilldown(shedId, { asOf });
  if (!result.ok) {
    return <NotFoundOrError shedId={shedId} message={result.error.message} />;
  }
  const shed = result.data;

  const s = shed.summary;
  // Owner chain comes from the most-at-risk row so the drilldown header shows the live accountable chain.
  const owner = shed.rows.find((r) => r.owner?.operatorName)?.owner ?? shed.rows[0]?.owner;
  const blockers = shed.rows.filter((r) => r.blockerReason);

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <Link href="/vaccination#execution" className="lk">
              PHC · Vaccination · Execution
            </Link>{" "}
            · {shed.parkName} · <b>{shed.shedName}</b>
          </div>
          <h1 style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <Warehouse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            {shed.parkName} · {shed.shedName}
          </h1>
          <div className="sub">Animal stages: {shed.animalStages.join(" · ") || "—"}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href="/vaccination#execution" className="btn">
          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> Back to execution
        </Link>
      </div>

      {/* Work-state summary for this shed */}
      <section className="card" style={{ marginBottom: 14 }}>
        <div className="hd">
          <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>Work state</h3>
          <Tag tone="mut">{s.total} drive rows</Tag>
        </div>
        <div className="bd">
          <div className="metagrid" style={{ gridTemplateColumns: "repeat(auto-fill,minmax(150px,1fr))", gap: 14 }}>
            <Stat label="Due" value={s.due} tone="warn" />
            <Stat label="Overdue" value={s.overdue} tone="dng" />
            <Stat label="Proof pending" value={s.proofPending} tone="warn" />
            <Stat label="Verification pending" value={s.verificationPending} tone="pur" />
            <Stat label="Rejected" value={s.rejected} tone="dng" />
            <Stat label="Deferred" value={s.deferred} tone="mut" />
            <Stat label="Blocked" value={s.blocked} tone="dng" />
            <Stat label="Owner missing" value={s.ownerMissing} tone="dng" />
            <Stat label="Completed" value={s.completed} tone="ok" />
          </div>
        </div>
      </section>

      <div className="grid g2" style={{ marginBottom: 14 }}>
        {/* Drives */}
        <section className="card">
          <div className="hd">
            <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            <h3>Drives at this shed</h3>
            <Tag tone="mut">{shed.drives.length}</Tag>
          </div>
          <div className="bd">
            <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
              {shed.drives.length === 0 ? (
                <span className="muted small">No drives scheduled at this shed.</span>
              ) : (
                shed.drives.map((d, i) => (
                  <Tag key={`${d.driveId ?? "drive"}-${i}`} tone={WORK_STATE_META[d.workState].tone} title={SEVERITY_META[d.severity].label}>
                    {d.driveName ?? "drive"} · {WORK_STATE_META[d.workState].label}
                  </Tag>
                ))
              )}
            </div>
          </div>
        </section>

        {/* Owner chain */}
        <section className="card">
          <div className="hd">
            <ShieldCheck className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            <h3>Owner chain</h3>
          </div>
          <div className="bd">
            <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <UserRound className="ic" style={{ width: 14, opacity: 0.75 }} aria-hidden="true" />
                <div>
                  <div className="k">Operator (ground)</div>
                  <div className="v">{owner?.operatorName ?? <Tag tone="dng">unassigned</Tag>}</div>
                </div>
              </div>
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <MapPin className="ic" style={{ width: 14, opacity: 0.75 }} aria-hidden="true" />
                <div>
                  <div className="k">Park head</div>
                  <div className="v">{owner?.parkHeadName ?? "—"}</div>
                </div>
              </div>
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <ShieldCheck className="ic" style={{ width: 14, opacity: 0.75 }} aria-hidden="true" />
                <div>
                  <div className="k">Verifier</div>
                  <div className="v">{owner?.verifierName ?? "Video Verification Team"}</div>
                </div>
              </div>
            </div>
          </div>
        </section>
      </div>

      {/* Blockers / deferred reasons surfaced explicitly */}
      {blockers.length > 0 ? (
        <section className="card" style={{ marginBottom: 14 }}>
          <div className="hd">
            <Ban className="ic" style={{ color: "var(--danger)" }} aria-hidden="true" />
            <h3>Blocked / deferred</h3>
            <Tag tone="dng">{blockers.length}</Tag>
          </div>
          <div className="bd">
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {blockers.map((r, i) => (
                <div key={`${r.driveId ?? "drive"}-${i}`} className="note">
                  <Tag tone={WORK_STATE_META[r.workState].tone}>{WORK_STATE_META[r.workState].label}</Tag>{" "}
                  <b>{r.driveName ?? "drive"}</b> ({r.animalStage}) — {r.blockerReason}
                </div>
              ))}
            </div>
          </div>
        </section>
      ) : null}

      {/* Full row detail */}
      <section className="card">
        <div className="hd">
          <Warehouse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>Drive rows</h3>
          <Tag tone="mut">{shed.rows.length}</Tag>
        </div>
        <div className="pexec" role="group" aria-label={`${shed.shedName} drive rows`}>
          <div className="pexh">
            <div>Animal stage</div>
            <div>Drive</div>
            <div>Due</div>
            <div>Work state</div>
            <div>SOP · proof · verify</div>
            <div>Next action</div>
          </div>
          {shed.rows.map((row, idx) => (
            <div className="pexr" key={`${row.driveId ?? "drive"}-${idx}`}>
              <div className="pexc">
                <div className="pexc-h">Animal stage</div>
                <span className="small">{row.animalStage}</span>
              </div>
              <div className="pexc">
                <div className="pexc-h">Drive</div>
                <span className="small">{row.driveName ?? "—"}</span>
              </div>
              <div className="pexc">
                <div className="pexc-h">Due</div>
                <span className="small">{fmtDate(row.dueDate)}</span>
              </div>
              <div className="pexc">
                <div className="pexc-h">Work state</div>
                <Tag tone={WORK_STATE_META[row.workState].tone}>{WORK_STATE_META[row.workState].label}</Tag>
              </div>
              <div className="pexc">
                <div className="pexc-h">SOP · proof · verify</div>
                <StatusChips row={row} />
              </div>
              <div className="pexc">
                <div className="pexc-h">Next action</div>
                <span className="small">{row.nextAction}</span>
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
