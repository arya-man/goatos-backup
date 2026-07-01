"use client";

import { useMemo, useRef, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, NotebookPen, Play, Plus, X } from "lucide-react";
import {
  DSL_GAPS,
  STEP_TYPES,
  buildFormDsl,
  buildSopCode,
  hasProofField,
  type BuilderStep,
  type OnAnswerAction,
  type ProofType,
  type SopCardView,
  type SopBuilderInput,
  type SopSliceDomain,
  type SopTrigger,
  type StepTypeValue,
  type SubjectScope,
} from "./sop-derive";
import { runDryRun, saveSopDraft, saveSopVersionDraft, publishSop, type SaveSopResult } from "./sop-actions";
import type { DryRunResponse } from "@/lib/api/server";
import { copy, optionGroup, optionLabel, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Stable per-step id. No module-level mutable counter (it would drift across mounts/HMR and risk
// SSR/CSR key divergence); a random id is unique per step and is only used as a React key + for
// showIf de-dup within one modal session.
function makeStep(type: StepTypeValue, label: string, showIf = -1): BuilderStep {
  return { id: `step-${crypto.randomUUID()}`, type, label, showIf, onAnswer: "none" };
}

function builderTypeFromBackend(type: string): StepTypeValue {
  switch (type) {
    case "number":
      return "number";
    case "boolean":
      return "yesno";
    case "select":
      return "select";
    case "multiselect":
      return "multiselect";
    case "goat_scan":
    case "rfid_scan":
    case "goat_lookup":
      return "goat_scan";
    case "shed_picker":
    case "cohort_picker":
    case "location_picker":
      return "shed_picker";
    case "vaccine_batch_picker":
      return "vaccine_batch_picker";
    case "medicine_picker":
      return "medicine_picker";
    case "photo_proof":
      return "photo_proof";
    case "video_proof":
      return "video_proof";
    default:
      return "text";
  }
}

function stepsFromView(view: SopCardView | null | undefined): BuilderStep[] | null {
  if (!view || view.fields.length === 0) return null;
  return view.fields.map((field) => makeStep(builderTypeFromBackend(field.type), field.label));
}

function proofTypeFromView(view: SopCardView | null | undefined, fallback: ProofType): ProofType {
  const proofField = view?.fields.find((field) => field.type === "photo_proof" || field.type === "video_proof");
  return proofField?.type === "photo_proof" ? "photo" : proofField?.type === "video_proof" ? "video" : fallback;
}

export function NewSopModal({
  open,
  onClose,
  pageContract,
  initialView,
}: {
  open: boolean;
  onClose: () => void;
  pageContract: AdminUiPageContract;
  initialView?: SopCardView | null;
}) {
  const router = useRouter();
  const triggerOptions = optionGroup(pageContract, "sop_trigger_chips");
  const seedStepOptions = optionGroup(pageContract, "sop_seed_steps");
  const onAnswerOptions = optionGroup(pageContract, "sop_on_answer_actions");
  const proofTypeOptions = optionGroup(pageContract, "proof_types");
  const subjectScopeOptions = optionGroup(pageContract, "subject_scopes");

  function firstKey(options: AdminUiOption[], groupId: string): string {
    const [first] = options;
    if (!first) throw new Error(`Admin-web page contract ${pageContract.route_id} has empty option group ${groupId}`);
    return first.key;
  }
  function defaultKey(options: AdminUiOption[], groupId: string, preferred: string): string {
    return options.some((option) => option.key === preferred) ? preferred : firstKey(options, groupId);
  }
  function seedSteps(): BuilderStep[] {
    return seedStepOptions.map((step) => makeStep(step.key as StepTypeValue, step.label));
  }

  const [name, setName] = useState(initialView?.name ?? copy(pageContract, "modal.builder.default_name"));
  // Domain is LOCKED to the vaccination slice — not user-selectable in this product slice.
  const domain: SopSliceDomain = "vaccination";
  const [trigger, setTrigger] = useState<SopTrigger>((initialView?.trigger ?? defaultKey(triggerOptions, "sop_trigger_chips", "cron")) as SopTrigger);
  const [steps, setSteps] = useState<BuilderStep[]>(() => stepsFromView(initialView) ?? seedSteps());

  const defaultProofType = defaultKey(proofTypeOptions, "proof_types", "video") as ProofType;
  const [proofRequired, setProofRequired] = useState(initialView ? initialView.gates.some((gate) => gate.toLowerCase().includes("proof")) : true);
  const [proofType, setProofType] = useState<ProofType>(proofTypeFromView(initialView, defaultProofType));
  const [verifyBeforeApply, setVerifyBeforeApply] = useState(initialView ? initialView.gates.some((gate) => gate.toLowerCase().includes("verify")) : true);
  const [minCount, setMinCount] = useState(1);
  // Vaccination drives are per-goat (repeat-per-goat) by default.
  const [subjectScope, setSubjectScope] = useState<SubjectScope>(
    (initialView?.gates.some((gate) => gate.toLowerCase().includes("batch"))
      ? "batch"
      : defaultKey(subjectScopeOptions, "subject_scopes", "goat")) as SubjectScope,
  );

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
      const res = initialView?.sopId ? await saveSopVersionDraft(initialView.sopId, input) : await saveSopDraft(input);
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
      else setNotice({ ok: false, message: res.message ?? copy(pageContract, "modal.builder.message.dry_run_failed") });
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
        aria-label={copy(pageContract, "modal.builder.aria")}
      >
        <div className="cmh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand)", width: 32, height: 32, borderRadius: 9 }}>
            <NotebookPen className="ic" />
          </span>
          <div>
            <div className="mono muted" style={{ fontSize: 11 }}>
              {copy(pageContract, "modal.builder.eyebrow")}
            </div>
            <div className="b700">{copy(pageContract, "modal.builder.title")}</div>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="x" onClick={onClose} aria-label={copy(pageContract, "modal.builder.close_label")}>
            <X className="ic" />
          </button>
        </div>

        <div className="cmb" style={{ display: "block" }}>
          {notice ? (
            notice.ok ? (
              <div className="note" style={{ marginBottom: 12 }}>
                <span className="tag t-ok">{copy(pageContract, "modal.builder.notice_ok")}</span> {notice.message}
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
            <label>{copy(pageContract, "modal.builder.field.name_domain")}</label>
            <div className="rowf">
              <input
                aria-label={copy(pageContract, "modal.builder.field.name")}
                value={name}
                onChange={(e) => {
                  setName(e.target.value);
                  resetResults();
                }}
                placeholder={copy(pageContract, "modal.builder.placeholder.name")}
              />
              <div
                aria-label={copy(pageContract, "modal.builder.domain_aria")}
                title={copy(pageContract, "modal.builder.domain_title")}
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
                <span className="tag t-pur">{copy(pageContract, "modal.builder.domain_label")}</span>
                <span className="muted small">{copy(pageContract, "modal.builder.domain_locked")}</span>
              </div>
            </div>
            <div className="muted small" style={{ marginTop: 5 }}>
              {copy(pageContract, "modal.builder.code_prefix")} <span className="mono">{initialView?.code ?? code}</span> · {copy(pageContract, "modal.builder.policy_label")}
            </div>
          </div>

          {/* Trigger segmented controls */}
          <div className="fld">
            <label>{copy(pageContract, "modal.builder.field.trigger")}</label>
            <div className="chipset">
              {triggerOptions.map((t) => (
                <button
                  type="button"
                  key={t.key}
                  className={`chip${trigger === t.key ? " on" : ""}`}
                  aria-pressed={trigger === t.key}
                  onClick={() => {
                    setTrigger(t.key as SopTrigger);
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
            <label>{copy(pageContract, "modal.builder.field.steps")}</label>
            <div className="sopsteps">
              {steps.map((step, i) => (
                <div className="sopstep" key={step.id}>
                  <span className="sn">{i + 1}</span>
                  <select
                    aria-label={`${copy(pageContract, "modal.builder.step_type_aria_prefix")} ${i + 1} ${copy(pageContract, "modal.builder.step_type_aria_suffix")}`}
                    value={step.type}
                    onChange={(e) => {
                      patchStep(step.id, { type: e.target.value as StepTypeValue });
                      resetResults();
                    }}
                  >
                    {STEP_TYPES.map((t) => (
                      <option key={t.value} value={t.value}>
                        {optionLabel(pageContract, "sop_step_types", t.value)}
                      </option>
                    ))}
                  </select>
                  <input
                    aria-label={`${copy(pageContract, "modal.builder.step_type_aria_prefix")} ${i + 1} ${copy(pageContract, "modal.builder.step_label_aria_suffix")}`}
                    value={step.label}
                    placeholder={copy(pageContract, "modal.builder.placeholder.step")}
                    onChange={(e) => {
                      patchStep(step.id, { label: e.target.value });
                      resetResults();
                    }}
                  />
                  <button
                    type="button"
                    className="ia del"
                    title={copy(pageContract, "modal.builder.action.remove_step")}
                    aria-label={`${copy(pageContract, "modal.builder.action.remove_step")} ${i + 1}`}
                    onClick={() => {
                      removeStep(step.id);
                      resetResults();
                    }}
                  >
                    <X className="ic" />
                  </button>
                  <div className="ss-cond" style={{ display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap" }}>
                    <span>{copy(pageContract, "modal.builder.condition.show")}</span>
                    <select
                      aria-label={`${copy(pageContract, "modal.builder.step_type_aria_prefix")} ${i + 1} ${copy(pageContract, "modal.builder.condition.show")}`}
                      value={step.showIf}
                      onChange={(e) => {
                        patchStep(step.id, { showIf: Number(e.target.value) });
                        resetResults();
                      }}
                    >
                      <option value={-1}>{copy(pageContract, "modal.builder.condition.always")}</option>
                      {steps.slice(0, i).map((_, j) => (
                        <option key={j} value={j}>
                          {copy(pageContract, "modal.builder.condition.only_if_prefix")} {j + 1} {copy(pageContract, "modal.builder.condition.only_if_suffix")}
                        </option>
                      ))}
                    </select>
                    <span>{copy(pageContract, "modal.builder.condition.on_answer")}</span>
                    <select
                      aria-label={`${copy(pageContract, "modal.builder.step_type_aria_prefix")} ${i + 1} ${copy(pageContract, "modal.builder.condition.on_answer")}`}
                      value={step.onAnswer}
                      onChange={(e) => {
                        patchStep(step.id, { onAnswer: e.target.value as OnAnswerAction });
                        resetResults();
                      }}
                    >
                      {onAnswerOptions.map((a) => (
                        <option key={a.key} value={a.key}>
                          {a.label}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>
              ))}
            </div>
            <button type="button" className="btn sm" style={{ marginTop: 8 }} onClick={addStep}>
              <Plus className="ic" /> {copy(pageContract, "modal.builder.action.add_step")}
            </button>
            <div className="note" style={{ marginTop: 8 }}>
              <b>{copy(pageContract, "modal.builder.logic_title")}</b> {copy(pageContract, "modal.builder.logic_body")}
            </div>
          </div>

          {/* Proof policy */}
          <div className="fld">
            <label>{copy(pageContract, "modal.builder.field.proof_policy")}</label>
            <div className="cfgchk" style={{ flexWrap: "wrap", gap: 14 }}>
              <label>
                <input type="checkbox" checked={proofRequired} onChange={(e) => { setProofRequired(e.target.checked); resetResults(); }} /> {copy(pageContract, "modal.builder.label.proof_required")}
              </label>
              <label>
                <input type="checkbox" checked={verifyBeforeApply} onChange={(e) => { setVerifyBeforeApply(e.target.checked); resetResults(); }} /> {copy(pageContract, "modal.builder.label.verify_before_apply")}
              </label>
            </div>
            <div className="rowf" style={{ marginTop: 8 }}>
              <select aria-label={copy(pageContract, "modal.builder.field.proof_type")} value={proofType} onChange={(e) => { setProofType(e.target.value as ProofType); resetResults(); }}>
                {proofTypeOptions.map((p) => (
                  <option key={p.key} value={p.key}>
                    {p.label}
                  </option>
                ))}
              </select>
              <input
                aria-label={copy(pageContract, "modal.builder.field.min_count")}
                type="number"
                min={1}
                value={minCount}
                onChange={(e) => { setMinCount(Number(e.target.value)); resetResults(); }}
              />
              <select aria-label={copy(pageContract, "modal.builder.field.subject_scope")} value={subjectScope} onChange={(e) => { setSubjectScope(e.target.value as SubjectScope); resetResults(); }}>
                {subjectScopeOptions.map((scope) => (
                  <option key={scope.key} value={scope.key}>{scope.label}</option>
                ))}
              </select>
            </div>
            {!proofGapOk ? (
              <div className="alert warn" style={{ marginTop: 8 }}>
                <AlertTriangle className="ic" />
                <div>{copy(pageContract, "modal.builder.proof_gap")}</div>
              </div>
            ) : null}
          </div>

          {/* Backend / DSL gap disclosure */}
          <details style={{ marginTop: 4 }}>
            <summary className="muted small" style={{ cursor: "pointer" }}>
              {copy(pageContract, "modal.builder.details_prefix")} ({fieldCount} {copy(pageContract, "modal.builder.details_middle")} ({DSL_GAPS.length})
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
                {saved.report.valid ? copy(pageContract, "modal.builder.validation.valid") : copy(pageContract, "modal.builder.validation.issues")}
              </span>{" "}
              {saved.versionId ? <span className="muted small">{copy(pageContract, "modal.builder.label.draft")} · {saved.versionId.slice(0, 8)}</span> : null}
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
              <span className={`tag ${dryRun.valid ? "t-ok" : "t-warn"}`}>{copy(pageContract, "modal.builder.label.dry_run")} {dryRun.valid ? copy(pageContract, "modal.builder.notice_ok") : dryRun.final_state}</span>{" "}
              <span className="muted small">
                {copy(pageContract, "modal.builder.label.workflow")} {dryRun.workflow_path.join(" → ") || copy(pageContract, "label.placeholder")} · {copy(pageContract, "modal.builder.label.final")} {dryRun.final_state}
              </span>
              {dryRun.field_states.length > 0 ? (
                <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginTop: 8 }}>
                  {dryRun.field_states.map((fs) => (
                    <span key={fs.key} className={`tag ${fs.blocked ? "t-warn" : fs.required ? "t-info" : "t-mut"}`}>
                      {fs.key}
                      {fs.required ? " *" : ""}
                      {fs.blocked ? ` · ${copy(pageContract, "modal.builder.label.blocked")}` : ""}
                    </span>
                  ))}
                </div>
              ) : null}
            </div>
          ) : null}
        </div>

        <div className="cfgmf">
          <button type="button" className="btn" onClick={onClose}>
            {copy(pageContract, "action.cancel")}
          </button>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="btn" onClick={save} disabled={pending || !proofGapOk}>
            {pending ? copy(pageContract, "modal.builder.action.saving") : saved?.ok ? copy(pageContract, "modal.builder.action.re_save") : copy(pageContract, "modal.builder.action.save")}
          </button>
          <button
            type="button"
            className="btn"
            onClick={dry}
            disabled={pending || !saved?.versionId}
            title={!saved?.versionId ? copy(pageContract, "modal.builder.title.save_first") : copy(pageContract, "modal.builder.title.preview")}
            style={!saved?.versionId ? { opacity: 0.45, cursor: "not-allowed" } : undefined}
          >
            <Play className="ic" /> {copy(pageContract, "modal.builder.action.dry_run")}
          </button>
          <button
            type="button"
            className="btn p"
            onClick={publish}
            disabled={pending || !canPublish}
            title={!saved?.versionId ? copy(pageContract, "modal.builder.title.save_first") : canPublish ? copy(pageContract, "modal.builder.title.publish") : copy(pageContract, "modal.builder.title.resolve")}
            style={!canPublish ? { opacity: 0.45, cursor: "not-allowed" } : undefined}
          >
            <Check className="ic" /> {copy(pageContract, "action.publish")}
          </button>
        </div>
      </div>
    </>
  );
}
