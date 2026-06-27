import Link from "next/link";
import { Syringe, X } from "lucide-react";
import type {
  VaccinationOperationsCell,
  VaccinationOperationsCohort,
  VaccinationOperationsCounts,
  VaccinationOperationsProtocol,
} from "@/lib/api/server";
import { Tag, type Tone } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { copy, optionGroup, optionLabel, optionTone, tableLabels, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { scopeHref, type Scope } from "@/lib/scope";
import { vaccinationProtocolDisplayName } from "./vaccine-display";

// Shared, mock-shaped Record / verify vaccination drawer (mock `vaccRecordModal`). Ported anatomy:
//   .dh header → .dc body (sub note · real context .metagrid · live status counts · the vaccine record form
//   fields .fld/select/.chipset/.videobox) → .df footer.
//
// Backend honesty (the rule the maintainer set): a vaccination dose is RECORDED by the shed operator through
// the SOP task + proof upload (TaskExecute), and a single recorded dose is VERIFIED per `completion_id` from
// the verification queue. The cohort × protocol cell here is a status rollup — it carries counts, not a single
// completion id, and admin-web exposes no record route. So the record/verify form is rendered to full mock
// shape but each write sub-control is DISABLED with an honest reason; the live actions route out to the
// Action Center / Protocol Adherence where the real obligation + completion rows live. No fake submit.

type CountKey = keyof VaccinationOperationsCounts;

function statusMeta(pageContract: AdminUiPageContract, workState: string): { label: string; tone: Tone } {
  return {
    label: optionLabel(pageContract, "work_state_filter_chips", workState),
    tone: optionTone(pageContract, "work_state_filter_chips", workState) as Tone,
  };
}

// A mock `.fld` whose control is shown but disabled, with the honest reason inline. Keeps the mock's exact
// field anatomy (label + control) rather than dropping the field — disable ≠ simplify.
function DisabledField({ label, children, reason }: { label: string; children: React.ReactNode; reason?: string }) {
  return (
    <div className="fld" aria-disabled="true">
      <label>{label}</label>
      {children}
      {reason ? (
        <div className="muted small" style={{ marginTop: 4, lineHeight: 1.45 }}>
          {reason}
        </div>
      ) : null}
    </div>
  );
}

function DisabledChipset({ options, on = 0 }: { options: AdminUiOption[]; on?: number }) {
  return (
    <div className="chipset" aria-disabled="true" style={{ opacity: 0.6, pointerEvents: "none" }}>
      {options.map((opt, i) => (
        <span key={opt.key} className={`chip${i === on ? " on" : ""}`} aria-disabled="true">
          {opt.label}
        </span>
      ))}
    </div>
  );
}

// The mock vaccine record FORM body (full `vaccRecordModal` field anatomy), with every write sub-control
// disabled-with-honest-reason. Shared by the cohort/cell drawer and the shed-event drawer so a clicked
// vaccination item always opens the same mock-shaped record/verify form, never a flatter substitute.
export function VaccinationRecordFormFields({ cohortShed, vaccineName, pageContract }: { cohortShed: string; vaccineName: string; pageContract: AdminUiPageContract }) {
  return (
    <>
      <div className="note" style={{ margin: "14px 0 12px" }}>
        {copy(pageContract, "drawer.record_verify.record_reason")}
      </div>
      <DisabledField label={copy(pageContract, "drawer.record_verify.form.cohort_shed")}>
        <input value={cohortShed} disabled readOnly />
      </DisabledField>
      <DisabledField label={copy(pageContract, "drawer.record_verify.form.vaccine")}>
        <select value={vaccineName} disabled>
          <option>{vaccineName}</option>
        </select>
      </DisabledField>
      <DisabledField label={copy(pageContract, "drawer.record_verify.form.batch")}>
        <input placeholder={copy(pageContract, "drawer.record_verify.form.batch_placeholder")} disabled />
      </DisabledField>
      <DisabledField label={copy(pageContract, "drawer.record_verify.form.dose")}>
        <input placeholder={copy(pageContract, "drawer.record_verify.form.dose_placeholder")} disabled />
      </DisabledField>
      <DisabledField label={copy(pageContract, "drawer.record_verify.form.cold_chain")}>
        <DisabledChipset options={optionGroup(pageContract, "yes_no")} />
      </DisabledField>
      <DisabledField label={copy(pageContract, "drawer.record_verify.form.adverse")}>
        <DisabledChipset options={optionGroup(pageContract, "adverse_reaction")} />
      </DisabledField>
      {/* Proof = file upload (shed + vial video), per the SOP proof_policy. Web proof is a FILE UPLOAD, never a
          camera capture. The real <input type=file> is the honest control; it is disabled here because admin-web
          has no task_id to attach the upload to (POST /app/proofs/* + /app/tasks/{task_id}/submissions). */}
      <DisabledField
        label={copy(pageContract, "drawer.record_verify.form.proof")}
        reason={copy(pageContract, "drawer.record_verify.proof_reason")}
      >
        <input type="file" accept="video/*,image/*" multiple disabled aria-disabled="true" />
      </DisabledField>
      <div className="note" style={{ marginTop: 2 }}>
        {copy(pageContract, "drawer.record_verify.verify_reason")}
      </div>
    </>
  );
}

export type VaccinationRecordContext = {
  cohort: VaccinationOperationsCohort;
  protocol?: VaccinationOperationsProtocol | null;
  cell?: VaccinationOperationsCell | null;
};

export function VaccinationRecordVerifyDrawer({
  context,
  scope,
  closePath = "/vaccination",
  pageContract,
}: {
  context: VaccinationRecordContext;
  scope: Scope;
  closePath?: string;
  pageContract: AdminUiPageContract;
}) {
  const { cohort, protocol, cell } = context;
  const counts = cell?.counts ?? cohort.counts;
  const workState = cell?.workState ?? cohort.workState;
  const meta = statusMeta(pageContract, workState);
  const lastDose = cell?.lastDose ?? cohort.lastDose;
  const nextDue = cell?.nextDue ?? cohort.nextDue;
  const vaccineName = protocol ? vaccinationProtocolDisplayName(protocol) : copy(pageContract, "drawer.record_verify.all_protocols");
  const cohortLabels = tableLabels(pageContract, "cohort-detail");
  const countChips = optionGroup(pageContract, "obligation_count_chips") as Array<AdminUiOption & { key: CountKey }>;

  const closeHref = scopeHref(closePath, scope);
  const actionCenterHref = scopeHref("/action-center", scope, {}, { state: workState });
  const adherenceHref = scopeHref("/protocol-adherence", scope);

  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label={copy(pageContract, "drawer.record_verify.close_label")} scroll={false} />
      <aside className="drawer on" aria-label={copy(pageContract, "drawer.record_verify.aria")}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Syringe className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "drawer.record_verify.eyebrow")}</div>
            <h2>{copy(pageContract, "drawer.record_verify.title")}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label={copy(pageContract, "drawer.record_verify.close_label")} scroll={false}>
            <X className="ic" />
          </Link>
        </div>

        <div className="dc">
          <div className="note" style={{ marginBottom: 12 }}>
            {copy(pageContract, "drawer.record_verify.note")}
          </div>

          {/* Real cohort × protocol context (RECORD anatomy = .metagrid, never a flat stack). */}
          <div className="metagrid">
            <div>
              <div className="k">{copy(pageContract, "drawer.record_verify.form.cohort_shed")}</div>
              <div className="v">
                {cohort.stage} · {cohort.shedName}
              </div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.record_verify.form.vaccine")}</div>
              <div className="v">{vaccineName}</div>
            </div>
            <div>
              <div className="k">{cohortLabels[1]}</div>
              <div className="v">{cohort.animals || copy(pageContract, "label.placeholder")}</div>
            </div>
            <div>
              <div className="k">{cohortLabels[2]}</div>
              <div className="v">{cohort.ageBand ?? cohort.stage}</div>
            </div>
            <div>
              <div className="k">{cohortLabels[3]}</div>
              <div className="v">{lastDose ? fmtDate(lastDose) : copy(pageContract, "label.placeholder")}</div>
            </div>
            <div>
              <div className="k">{cohortLabels[4]}</div>
              <div className="v">{nextDue ? fmtDate(nextDue) : copy(pageContract, "label.placeholder")}</div>
            </div>
            <div>
              <div className="k">{cohortLabels[5]}</div>
              <div className="v">
                <Tag tone={meta.tone}>{meta.label}</Tag>
              </div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.park")}</div>
              <div className="v">{cohort.parkName}</div>
            </div>
          </div>

          {/* Live obligation/completion tallies for this cell (or cohort rollup). */}
          <div className="chipset" style={{ margin: "14px 0 4px" }}>
            {countChips.map(({ key, label, tone }) => {
              const n = counts[key] ?? 0;
              if (!n) return null;
              return (
                <Tag key={key} tone={(tone || "mut") as Tone}>
                  {label} · {n}
                </Tag>
              );
            })}
            {countChips.every(({ key }) => !(counts[key] ?? 0)) ? (
              <span className="muted small">{copy(pageContract, "drawer.record_verify.no_obligations")}</span>
            ) : null}
          </div>

          {/* Mock vaccine record form — full anatomy, write sub-controls disabled with honest reason. */}
          <VaccinationRecordFormFields cohortShed={`${cohort.stage} · ${cohort.shedName}`} vaccineName={vaccineName} pageContract={pageContract} />
        </div>

        <div className="df">
          <button type="button" className="btn p" aria-disabled="true" disabled title={copy(pageContract, "drawer.record_verify.record_reason")}>
            {copy(pageContract, "action.record_verify")}
          </button>
          <Link href={actionCenterHref} className="btn" scroll={false}>
            {copy(pageContract, "action.open_action_center")}
          </Link>
          <Link href={adherenceHref} className="btn" scroll={false}>
            {copy(pageContract, "action.open_protocol_adherence")}
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            {copy(pageContract, "action.cancel")}
          </Link>
        </div>
      </aside>
    </>
  );
}
