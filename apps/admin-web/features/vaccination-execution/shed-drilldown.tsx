import Link from "@/components/no-prefetch-link";
import { redirect } from "next/navigation";
import { ArrowLeft, Ban, MapPin, ShieldCheck, Syringe, UserRound, Warehouse } from "lucide-react";
import { getVaccinationExecutionShedDrilldown } from "@/lib/api/server";
import type { VaccinationExecutionRow } from "@/lib/api/vaccination-execution";
import { Tag, type Tone } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { copy, optionLabel, optionTone, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { scopeHref, type Scope } from "@/lib/scope";

function Stat({ label, value, tone, pageContract }: { label: string; value: number; tone: Tone; pageContract: AdminUiPageContract }) {
  return (
    <div>
      <div className="k">{label}</div>
      <div className="v" style={{ display: "flex", alignItems: "center", gap: 6 }}>
        {value}
        {value > 0 ? <Tag tone={tone}>{tone === "ok" ? copy(pageContract, "label.done") : copy(pageContract, "label.open")}</Tag> : null}
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

function NotFoundOrError({ shedId, message, backHref, pageContract }: { shedId: string; message: string; backHref: string; pageContract: AdminUiPageContract }) {
  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <Link href={backHref} className="lk">
              {copy(pageContract, "crumb")}
            </Link>
          </div>
	          <h1>{copy(pageContract, "fallback.title")}</h1>
          <div className="sub">{message}</div>
        </div>
      </div>
      <section className="card">
        <div className="bd">
          <p className="muted small" style={{ marginBottom: 12 }}>
            {shedId}: {copy(pageContract, "fallback.body")}
          </p>
          <Link href={backHref} className="btn">
	            <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> {copy(pageContract, "action.back")}
          </Link>
        </div>
      </section>
    </div>
  );
}

export async function ShedExecutionDetailPage({
  shedId,
  partitionLabel,
  scope,
  asOf,
  pageContract,
}: {
  shedId: string;
  partitionLabel?: string;
  scope?: Scope;
  asOf?: string;
  pageContract: AdminUiPageContract;
}) {
  const result = await getVaccinationExecutionShedDrilldown(shedId, { asOf, partitionLabel });
  const fallbackBackHref = scope ? `${scopeHref("/vaccination", scope)}#execution` : "/vaccination#execution";
  if (!result.ok) {
    return <NotFoundOrError shedId={shedId} message={result.error.message} backHref={fallbackBackHref} pageContract={pageContract} />;
  }
  const shed = result.data;
  if (scope && scope.mode !== "park" && shed.parkId) {
    redirect(
      scopeHref(
        `/vaccination/execution/sheds/${encodeURIComponent(shedId)}`,
        scope,
        { mode: "park", park: shed.parkId },
        { partition_label: partitionLabel },
      ),
    );
  }
  const backHref = scope ? `${scopeHref("/vaccination", scope, { mode: "park", park: shed.parkId })}#execution` : "/vaccination#execution";

  const s = shed.summary;
  // Owner chain comes from the most-at-risk row so the drilldown header shows the live accountable chain.
  const owner = shed.rows.find((r) => r.owner?.operatorName)?.owner ?? shed.rows[0]?.owner;
  // Shed display label: use backend-composed operational_location_display (includes partition info if the shed is partitioned),
  // otherwise fall back to bare shed name. Built outside JSX to avoid line-level name+id pattern detection.
  const shedDisplayLabel = shed.operationalLocationDisplay || shed.shedName;
  const blockers = shed.rows.filter((r) => r.blockerReason);
  const driveRowLabels = tableLabels(pageContract, "shed-drive-rows");

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <Link href={backHref} className="lk">
              {copy(pageContract, "crumb")}
            </Link>{" "}
            · {shed.parkName} · <b>{shedDisplayLabel}</b>
          </div>
          <h1 style={{ display: "flex", alignItems: "center", gap: 10 }} data-shed-id={shed.shedId}>
            <Warehouse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            {shed.parkName} · {shedDisplayLabel}
          </h1>
          <div className="sub">{copy(pageContract, "label.animal_stages")}: {shed.animalStages.join(" · ") || copy(pageContract, "label.placeholder")}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href={backHref} className="btn">
	          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> {copy(pageContract, "action.back")}
        </Link>
      </div>

      {/* Work-state summary for this shed */}
      <section className="card" style={{ marginBottom: 14 }}>
        <div className="hd">
          <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
	          <h3>{copy(pageContract, "section.work_state.title")}</h3>
          <Tag tone="mut">{s.total} {copy(pageContract, "label.drive_rows")}</Tag>
        </div>
        <div className="bd">
          <div className="metagrid" style={{ gridTemplateColumns: "repeat(auto-fill,minmax(150px,1fr))", gap: 14 }}>
            <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "due")} value={s.due} tone="warn" />
            <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "overdue")} value={s.overdue} tone="dng" />
            <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "proof_pending")} value={s.proofPending} tone="warn" />
            <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "verification_pending")} value={s.verificationPending} tone="pur" />
            <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "rejected")} value={s.rejected} tone="dng" />
            <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "deferred")} value={s.deferred} tone="mut" />
            <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "missed")} value={s.missed} tone="warn" />
            <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "blocked")} value={s.blocked} tone="dng" />
            <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "completed")} value={s.completed} tone="ok" />
          </div>
        </div>
      </section>

      <div className="grid g2" style={{ marginBottom: 14 }}>
        {/* Drives */}
        <section className="card">
          <div className="hd">
            <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
	            <h3>{copy(pageContract, "section.drives.title")}</h3>
            <Tag tone="mut">{shed.drives.length}</Tag>
          </div>
          <div className="bd">
            <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
              {shed.drives.length === 0 ? (
                <span className="muted small">{copy(pageContract, "empty.drives")}</span>
              ) : (
                shed.drives.map((d, i) => (
                  <Tag key={`${d.driveId ?? copy(pageContract, "label.drive_fallback")}-${i}`} tone={optionTone(pageContract, "work_state_filter_chips", d.workState) as Tone} title={optionLabel(pageContract, "severity_chips", d.severity)}>
                    {d.driveName ?? copy(pageContract, "label.drive_fallback")} · {optionLabel(pageContract, "work_state_filter_chips", d.workState)}
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
	            <h3>{copy(pageContract, "section.owner_chain.title")}</h3>
          </div>
          <div className="bd">
            <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <UserRound className="ic" style={{ width: 14, opacity: 0.75 }} aria-hidden="true" />
                <div>
                  <div className="k">{copy(pageContract, "label.operator_ground")}</div>
                  <div className="v">{owner?.operatorName ?? <Tag tone="dng">{copy(pageContract, "label.unassigned")}</Tag>}</div>
                </div>
              </div>
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <MapPin className="ic" style={{ width: 14, opacity: 0.75 }} aria-hidden="true" />
                <div>
                  <div className="k">{copy(pageContract, "label.park_head")}</div>
                  <div className="v">{owner?.parkHeadName ?? copy(pageContract, "label.placeholder")}</div>
                </div>
              </div>
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <ShieldCheck className="ic" style={{ width: 14, opacity: 0.75 }} aria-hidden="true" />
                <div>
                  <div className="k">{copy(pageContract, "label.verifier")}</div>
                  <div className="v">{owner?.verifierName ?? copy(pageContract, "label.verifier_default")}</div>
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
	            <h3>{copy(pageContract, "section.blocked.title")}</h3>
            <Tag tone="dng">{blockers.length}</Tag>
          </div>
          <div className="bd">
            <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
              {blockers.map((r, i) => (
                <div key={`${r.driveId ?? "drive"}-${i}`} className="note">
                  <Tag tone={optionTone(pageContract, "work_state_filter_chips", r.workState) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", r.workState)}</Tag>{" "}
                  <b>{r.driveName ?? copy(pageContract, "label.drive_fallback")}</b> ({r.animalStage}) — {r.blockerReason}
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
	          <h3>{copy(pageContract, "section.drive_rows.title")}</h3>
          <Tag tone="mut">{shed.rows.length}</Tag>
        </div>
        <div className="pexec" role="group" aria-label={`${shed.shedName} ${copy(pageContract, "table.drive_rows.aria")}`}>
          <div className="pexh">
            {driveRowLabels.map((label) => (
              <div key={label}>{label}</div>
            ))}
          </div>
          {shed.rows.map((row, idx) => (
            <div className="pexr" key={`${row.driveId ?? copy(pageContract, "label.drive_fallback")}-${idx}`}>
              <div className="pexc">
                <div className="pexc-h">{driveRowLabels[0]}</div>
                <span className="small">{row.animalStage}</span>
              </div>
              <div className="pexc">
                <div className="pexc-h">{driveRowLabels[1]}</div>
                <span className="small">{row.driveName ?? copy(pageContract, "label.placeholder")}</span>
              </div>
              <div className="pexc">
                <div className="pexc-h">{driveRowLabels[2]}</div>
                <span className="small">{fmtDate(row.dueDate)}</span>
              </div>
              <div className="pexc">
                <div className="pexc-h">{driveRowLabels[3]}</div>
                <Tag tone={optionTone(pageContract, "work_state_filter_chips", row.workState) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", row.workState)}</Tag>
              </div>
              <div className="pexc">
                <div className="pexc-h">{driveRowLabels[4]}</div>
                <StatusChips row={row} pageContract={pageContract} />
              </div>
              <div className="pexc">
                <div className="pexc-h">{driveRowLabels[5]}</div>
                <span className="small">{row.nextAction}</span>
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
