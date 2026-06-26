"use client";

import { useMemo, useRef, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, NotebookPen, Play, Plus, X } from "lucide-react";
import {
  DSL_GAPS,
  ON_ANSWER_ACTIONS,
  STEP_TYPES,
  buildFormDsl,
  buildSopCode,
  hasProofField,
  type BuilderStep,
  type OnAnswerAction,
  type ProofType,
  type SopBuilderInput,
  type SopSliceDomain,
  type SopTrigger,
  type StepTypeValue,
  type SubjectScope,
} from "./sop-derive";
import { runDryRun, saveSopDraft, publishSop, type SaveSopResult } from "./sop-actions";
import type { DryRunResponse } from "@/lib/api/server";

// Trigger segmented controls — exact mock labels (Form · Schedule / cron · Sensor · Manual).
const TRIGGER_CHIPS: Array<{ value: SopTrigger; label: string }> = [
  { value: "form", label: "Form" },
  { value: "cron", label: "Schedule / cron" },
  { value: "sensor", label: "Sensor" },
  { value: "manual", label: "Manual" },
];

const PROOF_TYPES: ProofType[] = ["video", "photo"];

// Stable per-step id. No module-level mutable counter (it would drift across mounts/HMR and risk
// SSR/CSR key divergence); a random id is unique per step and is only used as a React key + for
// showIf de-dup within one modal session.
function makeStep(type: StepTypeValue, label: string, showIf = -1): BuilderStep {
  return { id: `step-${crypto.randomUUID()}`, type, label, showIf, onAnswer: "none" };
}

// Default seed = a Vaccination Session/drive skeleton (handoff scope lock + mock Vaccination Session
// entry): vaccine batch, cold-chain, goat scan, dose, route, administration proof video.
function seedSteps(): BuilderStep[] {
  return [
    makeStep("vaccine_batch_picker", "Select vaccine + batch (FEFO lot)"),
    makeStep("yesno", "Cold-chain intact (2-8°C)?"),
    makeStep("goat_scan", "Scan goat RFID / tag"),
    makeStep("number", "Dose volume administered (ml)"),
    makeStep("select", "Route / site"),
    makeStep("video_proof", "Upload administration proof video"),
  ];
}

export function NewSopModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const router = useRouter();
  const [name, setName] = useState("Vaccination session");
  // Domain is LOCKED to the vaccination slice — not user-selectable in this product slice.
  const domain: SopSliceDomain = "vaccination";
  const [trigger, setTrigger] = useState<SopTrigger>("cron");
  const [steps, setSteps] = useState<BuilderStep[]>(() => seedSteps());

  const [proofRequired, setProofRequired] = useState(true);
  const [proofType, setProofType] = useState<ProofType>("video");
  const [verifyBeforeApply, setVerifyBeforeApply] = useState(true);
  const [minCount, setMinCount] = useState(1);
  // Vaccination drives are per-goat (repeat-per-goat) by default.
  const [subjectScope, setSubjectScope] = useState<SubjectScope>("goat");

  const [saved, setSaved] = useState<SaveSopResult | null>(null);
  const [dryRun, setDryRun] = useState<DryRunResponse | null>(null);
  const [notice, setNotice] = useState<{ ok: boolean; message: string } | null>(null);
  const [pending, startTransition] = useTransition();
  const dialogRef = useRef<HTMLDivElement>(null);

  const input: SopBuilderInput = useMemo(
    () => ({ name, domain, trigger, steps, proofRequired, proofType, verifyBeforeApply, minCount, subjectScope }),
    [name, domain, trigger, steps, proofRequired, proofType, verifyBeforeApply, minCount, subjectScope],
  );

  const fieldCount = buildFormDsl(input).fields.length;
  const code = buildSopCode(input);
  const proofGapOk = !proofRequired || hasProofField(input);
  const canPublish = Boolean(saved?.ok && saved.versionId && saved.report?.valid);

  function patchStep(id: string, patch: Partial<BuilderStep>) {
    setSteps((rows) => rows.map((s) => (s.id === id ? { ...s, ...patch } : s)));
  }
  function addStep() {
    setSteps((rows) => [...rows, makeStep("yesno", "")]);
  }
  function removeStep(id: string) {
    setSteps((rows) => {
      const idx = rows.findIndex((s) => s.id === id);
      if (idx < 0) return rows;
      // showIf is the index of the prior step a condition references. After removing index `idx`:
      // only the step that pointed AT idx loses its condition; references to later steps shift down by
      // one. References to earlier steps are unchanged. (The old code cleared far too many rules.)
      return rows
        .filter((s) => s.id !== id)
        .map((s) => {
          if (s.showIf < 0) return s;
          if (s.showIf === idx) return { ...s, showIf: -1 };
          if (s.showIf > idx) return { ...s, showIf: s.showIf - 1 };
          return s;
        });
    });
  }

  function resetResults() {
    setSaved(null);
    setDryRun(null);
  }

  function save() {
    setNotice(null);
    startTransition(async () => {
      const res = await saveSopDraft(input);
      setSaved(res);
      setDryRun(null);
      setNotice({ ok: res.ok, message: res.message });
      if (res.ok) router.refresh();
    });
  }
  function dry() {
    if (!saved?.sopId || !saved.versionId) return;
    startTransition(async () => {
      const res = await runDryRun(saved.sopId!, saved.versionId!);
      if (res.ok && res.data) setDryRun(res.data);
      else setNotice({ ok: false, message: res.message ?? "dry-run failed" });
    });
  }
  function publish() {
    if (!saved?.sopId || !saved.versionId || saved.rowVersion === undefined) return;
    startTransition(async () => {
      const res = await publishSop(saved.sopId!, saved.versionId!, saved.rowVersion!);
      setNotice({ ok: res.ok, message: res.message });
      if (res.ok) {
        router.refresh();
        onClose();
      }
    });
  }

  if (!open) return null;

  return (
    <>
      <div className="cfgback on" onClick={onClose} />
      <div
        ref={dialogRef}
        className="cfgmodal on"
        style={{ width: "min(880px,96vw)" }}
        role="dialog"
        aria-modal="true"
        aria-label="New SOP form builder"
      >
        <div className="cmh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand)", width: 32, height: 32, borderRadius: 9 }}>
            <NotebookPen className="ic" />
          </span>
          <div>
            <div className="mono muted" style={{ fontSize: 11 }}>
              SOP · PHC / VACCINATION
            </div>
            <div className="b700">New SOP — form builder</div>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="x" onClick={onClose} aria-label="Close">
            <X className="ic" />
          </button>
        </div>

        <div className="cmb" style={{ display: "block" }}>
          {notice ? (
            notice.ok ? (
              <div className="note" style={{ marginBottom: 12 }}>
                <span className="tag t-ok">ok</span> {notice.message}
              </div>
            ) : (
              <div className="alert warn" style={{ marginBottom: 12 }}>
                <AlertTriangle className="ic" />
                <div>{notice.message}</div>
              </div>
            )
          ) : null}

          {/* SOP name + locked domain (vaccination slice) */}
          <div className="fld">
            <label>SOP name &amp; domain</label>
            <div className="rowf">
              <input
                aria-label="SOP name"
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                  resetResults();
                }}
                placeholder="Vaccination session"
              />
              <div
                aria-label="Domain — locked to PHC / Vaccination"
                title="Domain is locked to PHC / Vaccination for the current slice"
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  border: "1px solid var(--line)",
                  background: "var(--bg)",
                  borderRadius: 8,
                  padding: "8px 10px",
                }}
              >
                <span className="tag t-pur">PHC / Vaccination</span>
                <span className="muted small">domain locked</span>
              </div>
            </div>
            <div className="muted small" style={{ marginTop: 5 }}>
              sop_code <span className="mono">{code}</span> · vaccination drive/session policy
            </div>
          </div>

          {/* Trigger segmented controls */}
          <div className="fld">
            <label>Trigger — what starts it?</label>
            <div className="chipset">
              {TRIGGER_CHIPS.map((t) => (
                <button
                  type="button"
                  key={t.value}
                  className={`chip${trigger === t.value ? " on" : ""}`}
                  aria-pressed={trigger === t.value}
                  onClick={() => {
                    setTrigger(t.value);
                    resetResults();
                  }}
                >
                  {t.label}
                </button>
              ))}
            </div>
          </div>

          {/* Steps / questions builder — numbered green circles, type, label, conditional rules */}
          <div className="fld">
            <label>Steps &amp; questions — add/remove, pick a type, set conditional rules</label>
            <div className="sopsteps">
              {steps.map((step, i) => (
                <div className="sopstep" key={step.id}>
                  <span className="sn">{i + 1}</span>
                  <select
                    aria-label={`Step ${i + 1} type`}
                    value={step.type}
                    onChange={(e) => {
                      patchStep(step.id, { type: e.target.value as StepTypeValue });
                      resetResults();
                    }}
                  >
                    {STEP_TYPES.map((t) => (
                      <option key={t.value} value={t.value}>
                        {t.label}
                      </option>
                    ))}
                  </select>
                  <input
                    aria-label={`Step ${i + 1} label`}
                    value={step.label}
                    placeholder="question / step text"
                    onChange={(e) => {
                      patchStep(step.id, { label: e.target.value });
                      resetResults();
                    }}
                  />
                  <button
                    type="button"
                    className="ia del"
                    title="Remove step"
                    aria-label={`Remove step ${i + 1}`}
                    onClick={() => {
                      removeStep(step.id);
                      resetResults();
                    }}
                  >
                    <X className="ic" />
                  </button>
                  <div className="ss-cond" style={{ display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap" }}>
                    <span>show:</span>
                    <select
                      aria-label={`Step ${i + 1} visibility`}
                      value={step.showIf}
                      onChange={(e) => {
                        patchStep(step.id, { showIf: Number(e.target.value) });
                        resetResults();
                      }}
                    >
                      <option value={-1}>always show</option>
                      {steps.slice(0, i).map((_, j) => (
                        <option key={j} value={j}>
                          only if step {j + 1} answered
                        </option>
                      ))}
                    </select>
                    <span>· on answer →</span>
                    <select
                      aria-label={`Step ${i + 1} action`}
                      value={step.onAnswer}
                      onChange={(e) => {
                        patchStep(step.id, { onAnswer: e.target.value as OnAnswerAction });
                        resetResults();
                      }}
                    >
                      {ON_ANSWER_ACTIONS.map((a) => (
                        <option key={a.value} value={a.value}>
                          {a.label}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>
              ))}
            </div>
            <button type="button" className="btn sm" style={{ marginTop: 8 }} onClick={addStep}>
              <Plus className="ic" /> Add step / question
            </button>
            <div className="note" style={{ marginTop: 8 }}>
              <b>Conditional logic:</b> a step can show only if a previous step was answered, and an answer can
              require this step, require proof, or block submission — emitted as declarative{" "}
              <span className="mono">visible_if / required_if / proof_required_if / block_submission_if</span> rules.
              Cross-domain actions are routed through Action Center ownership and verification.
            </div>
          </div>

          {/* Proof policy */}
          <div className="fld">
            <label>Proof policy</label>
            <div className="cfgchk" style={{ flexWrap: "wrap", gap: 14 }}>
              <label>
                <input type="checkbox" checked={proofRequired} onChange={(e) => { setProofRequired(e.target.checked); resetResults(); }} /> proof required
              </label>
              <label>
                <input type="checkbox" checked={verifyBeforeApply} onChange={(e) => { setVerifyBeforeApply(e.target.checked); resetResults(); }} /> verify before apply
              </label>
            </div>
            <div className="rowf" style={{ marginTop: 8 }}>
              <select aria-label="Proof type" value={proofType} onChange={(e) => { setProofType(e.target.value as ProofType); resetResults(); }}>
                {PROOF_TYPES.map((p) => (
                  <option key={p} value={p}>
                    {p} proof
                  </option>
                ))}
              </select>
              <input
                aria-label="Minimum proof count"
                type="number"
                min={1}
                value={minCount}
                onChange={(e) => { setMinCount(Number(e.target.value)); resetResults(); }}
              />
              <select aria-label="Subject scope" value={subjectScope} onChange={(e) => { setSubjectScope(e.target.value as SubjectScope); resetResults(); }}>
                <option value="batch">subject: batch</option>
                <option value="goat">subject: per-goat</option>
              </select>
            </div>
            {!proofGapOk ? (
              <div className="alert warn" style={{ marginTop: 8 }}>
                <AlertTriangle className="ic" />
                <div>Proof required but no photo/video proof step — add one or turn proof off (backend rejects otherwise).</div>
              </div>
            ) : null}
          </div>

          {/* Backend / DSL gap disclosure */}
          <details style={{ marginTop: 4 }}>
            <summary className="muted small" style={{ cursor: "pointer" }}>
              Emits valid <span className="mono">form_dsl</span> ({fieldCount} fields, native types) + <span className="mono">proof_policy</span> · integration notes ({DSL_GAPS.length})
            </summary>
            <ul className="muted small" style={{ margin: "8px 0 0", paddingLeft: 18, lineHeight: 1.6 }}>
              {DSL_GAPS.map((g, i) => (
                <li key={i}>{g}</li>
              ))}
            </ul>
          </details>

          {/* Backend validation report after save */}
          {saved?.report ? (
            <div className="note" style={{ marginTop: 12 }}>
              <span className={`tag ${saved.report.valid ? "t-ok" : "t-warn"}`}>
                {saved.report.valid ? "form_dsl valid" : "validation issues"}
              </span>{" "}
              {saved.versionId ? <span className="muted small">draft · {saved.versionId.slice(0, 8)}</span> : null}
              {saved.report.errors.length > 0 ? (
                <ul className="muted small" style={{ margin: "6px 0 0", paddingLeft: 18 }}>
                  {saved.report.errors.map((e, i) => (
                    <li key={i}>
                      <span className="mono">{e.field}</span>: {e.message}
                    </li>
                  ))}
                </ul>
              ) : null}
            </div>
          ) : null}

          {/* Dry-run result */}
          {dryRun ? (
            <div className="note" style={{ marginTop: 10 }}>
              <span className={`tag ${dryRun.valid ? "t-ok" : "t-warn"}`}>dry-run {dryRun.valid ? "ok" : dryRun.final_state}</span>{" "}
              <span className="muted small">
                workflow: {dryRun.workflow_path.join(" → ") || "—"} · final: {dryRun.final_state}
              </span>
              {dryRun.field_states.length > 0 ? (
                <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginTop: 8 }}>
                  {dryRun.field_states.map((fs) => (
                    <span key={fs.key} className={`tag ${fs.blocked ? "t-warn" : fs.required ? "t-info" : "t-mut"}`}>
                      {fs.key}
                      {fs.required ? " *" : ""}
                      {fs.blocked ? " · blocked" : ""}
                    </span>
                  ))}
                </div>
              ) : null}
            </div>
          ) : null}
        </div>

        <div className="cfgmf">
          <button type="button" className="btn" onClick={onClose}>
            Cancel
          </button>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="btn" onClick={save} disabled={pending || !proofGapOk}>
            {pending ? "Saving…" : saved?.ok ? "Re-save draft" : "Save draft"}
          </button>
          <button
            type="button"
            className="btn"
            onClick={dry}
            disabled={pending || !saved?.versionId}
            title={!saved?.versionId ? "Save the draft first" : "Run server-side preview validation"}
            style={!saved?.versionId ? { opacity: 0.45, cursor: "not-allowed" } : undefined}
          >
            <Play className="ic" /> Dry-run
          </button>
          <button
            type="button"
            className="btn p"
            onClick={publish}
            disabled={pending || !canPublish}
            title={!saved?.versionId ? "Save the draft first" : canPublish ? "Publish immutable version" : "Resolve validation issues before publishing"}
            style={!canPublish ? { opacity: 0.45, cursor: "not-allowed" } : undefined}
          >
            <Check className="ic" /> Publish
          </button>
        </div>
      </div>
    </>
  );
}
