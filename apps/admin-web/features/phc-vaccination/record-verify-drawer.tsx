import Link from "next/link";
import { Syringe, X } from "lucide-react";
import type {
  VaccinationOperationsCell,
  VaccinationOperationsCohort,
  VaccinationOperationsCounts,
  VaccinationOperationsProtocol,
} from "@/lib/api/server";
import { WORK_STATE_META, type Tone } from "@/features/process-integrity";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
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

// EXACT, proven reasons (cited from the read-model contracts + PHC vaccination wiki). A dose is recorded by the
// field health worker's `vaccination.execute` SOP run (scan → administer → record dose → upload shed + vial
// video → verify). Admin-web has no record route, and the /vaccination read-models carry no id to write against.
const RECORD_REASON =
  "Recording a dose is the field worker’s vaccination.execute SOP action (scan → administer → record dose → upload shed + vial video). This command surface has no record route — /vaccination/operations + /vaccination/execution return status + counts only, with no task_id for POST /app/tasks/{task_id}/submissions or /app/proofs/*.";
const VERIFY_REASON =
  "Verify accepts/rejects one recorded dose by completion_id — exposed only by /vaccination/verification-queue (per goat), never on this cohort rollup. Act on the real obligation/completion rows in the Action Center.";

type CountKey = keyof VaccinationOperationsCounts;
const COUNT_CHIPS: Array<{ key: CountKey; label: string; tone: Tone }> = [
  { key: "overdue", label: "overdue", tone: "dng" },
  { key: "due", label: "due", tone: "warn" },
  { key: "inProgress", label: "in progress", tone: "info" },
  { key: "proofPending", label: "proof pending", tone: "warn" },
  { key: "accepted", label: "accepted", tone: "ok" },
  { key: "rejected", label: "rework", tone: "dng" },
];

function statusMeta(workState: string): { label: string; tone: Tone } {
  const meta = WORK_STATE_META[workState as keyof typeof WORK_STATE_META];
  return meta ?? { label: workState, tone: "mut" };
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

function DisabledChipset({ options, on = 0 }: { options: string[]; on?: number }) {
  return (
    <div className="chipset" aria-disabled="true" style={{ opacity: 0.6, pointerEvents: "none" }}>
      {options.map((opt, i) => (
        <span key={opt} className={`chip${i === on ? " on" : ""}`} aria-disabled="true">
          {opt}
        </span>
      ))}
    </div>
  );
}

// The mock vaccine record FORM body (full `vaccRecordModal` field anatomy), with every write sub-control
// disabled-with-honest-reason. Shared by the cohort/cell drawer and the shed-event drawer so a clicked
// vaccination item always opens the same mock-shaped record/verify form, never a flatter substitute.
export function VaccinationRecordFormFields({ cohortShed, vaccineName }: { cohortShed: string; vaccineName: string }) {
  return (
    <>
      <div className="note" style={{ margin: "14px 0 12px" }}>
        {RECORD_REASON}
      </div>
      <DisabledField label="Cohort / shed">
        <input value={cohortShed} disabled readOnly />
      </DisabledField>
      <DisabledField label="Vaccine">
        <select value={vaccineName} disabled>
          <option>{vaccineName}</option>
        </select>
      </DisabledField>
      <DisabledField label="Batch (FEFO) · lot">
        <input placeholder="lot…" disabled />
      </DisabledField>
      <DisabledField label="Dose / qty · route / site">
        <input placeholder="e.g. 1 dose · S/C neck" disabled />
      </DisabledField>
      <DisabledField label="Cold-chain intact?">
        <DisabledChipset options={["Yes", "No"]} />
      </DisabledField>
      <DisabledField label="Adverse reaction?">
        <DisabledChipset options={["None", "Mild", "Severe"]} />
      </DisabledField>
      {/* Proof = file upload (shed + vial video), per the SOP proof_policy. Web proof is a FILE UPLOAD, never a
          camera capture. The real <input type=file> is the honest control; it is disabled here because admin-web
          has no task_id to attach the upload to (POST /app/proofs/* + /app/tasks/{task_id}/submissions). */}
      <DisabledField
        label="Proof — shed + vial video (file upload)"
        reason="Uploaded by the field worker in the SOP task — no camera capture on web. Not attachable here: this surface has no task_id for /app/proofs/* + /app/tasks/{task_id}/submissions."
      >
        <input type="file" accept="video/*,image/*" multiple disabled aria-disabled="true" />
      </DisabledField>
      <div className="note" style={{ marginTop: 2 }}>
        {VERIFY_REASON}
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
}: {
  context: VaccinationRecordContext;
  scope: Scope;
  closePath?: string;
}) {
  const { cohort, protocol, cell } = context;
  const counts = cell?.counts ?? cohort.counts;
  const workState = cell?.workState ?? cohort.workState;
  const meta = statusMeta(workState);
  const lastDose = cell?.lastDose ?? cohort.lastDose;
  const nextDue = cell?.nextDue ?? cohort.nextDue;
  const vaccineName = protocol ? vaccinationProtocolDisplayName(protocol) : "All cohort protocols";

  const closeHref = scopeHref(closePath, scope);
  const actionCenterHref = scopeHref("/action-center", scope, {}, { state: workState });
  const adherenceHref = scopeHref("/protocol-adherence", scope);

  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label="Close record / verify drawer" scroll={false} />
      <aside className="drawer on" aria-label="Record or verify vaccination">
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Syringe className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">VACCINE</div>
            <h2>Record / verify vaccination</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label="Close record / verify drawer" scroll={false}>
            <X className="ic" />
          </Link>
        </div>

        <div className="dc">
          <div className="note" style={{ marginBottom: 12 }}>
            Pick vaccine + batch (FEFO), confirm cold-chain, capture proof. Verify posts a consume movement and
            advances the cohort; overdue escalates to the Health Director.
          </div>

          {/* Real cohort × protocol context (RECORD anatomy = .metagrid, never a flat stack). */}
          <div className="metagrid">
            <div>
              <div className="k">Cohort · shed</div>
              <div className="v">
                {cohort.stage} · {cohort.shedName}
              </div>
            </div>
            <div>
              <div className="k">Vaccine</div>
              <div className="v">{vaccineName}</div>
            </div>
            <div>
              <div className="k">Animals</div>
              <div className="v">{cohort.animals || "—"}</div>
            </div>
            <div>
              <div className="k">Age band</div>
              <div className="v">{cohort.ageBand ?? cohort.stage}</div>
            </div>
            <div>
              <div className="k">Last dose</div>
              <div className="v">{lastDose ? fmtDate(lastDose) : "—"}</div>
            </div>
            <div>
              <div className="k">Next due</div>
              <div className="v">{nextDue ? fmtDate(nextDue) : "—"}</div>
            </div>
            <div>
              <div className="k">Status</div>
              <div className="v">
                <Tag tone={meta.tone}>{meta.label}</Tag>
              </div>
            </div>
            <div>
              <div className="k">Park</div>
              <div className="v">{cohort.parkName}</div>
            </div>
          </div>

          {/* Live obligation/completion tallies for this cell (or cohort rollup). */}
          <div className="chipset" style={{ margin: "14px 0 4px" }}>
            {COUNT_CHIPS.map(({ key, label, tone }) => {
              const n = counts[key] ?? 0;
              if (!n) return null;
              return (
                <Tag key={key} tone={tone}>
                  {label} · {n}
                </Tag>
              );
            })}
            {COUNT_CHIPS.every(({ key }) => !(counts[key] ?? 0)) ? (
              <span className="muted small">No open obligations for this cohort.</span>
            ) : null}
          </div>

          {/* Mock vaccine record form — full anatomy, write sub-controls disabled with honest reason. */}
          <VaccinationRecordFormFields cohortShed={`${cohort.stage} · ${cohort.shedName}`} vaccineName={vaccineName} />
        </div>

        <div className="df">
          <button type="button" className="btn p" aria-disabled="true" disabled title={RECORD_REASON}>
            Record + verify
          </button>
          <Link href={actionCenterHref} className="btn" scroll={false}>
            Open Action Center
          </Link>
          <Link href={adherenceHref} className="btn" scroll={false}>
            Protocol Adherence
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            Cancel
          </Link>
        </div>
      </aside>
    </>
  );
}
