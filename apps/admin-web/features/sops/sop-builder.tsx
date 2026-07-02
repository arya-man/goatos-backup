"use client";

import { useMemo, useState, useTransition } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, ChevronLeft, NotebookPen, Play, Plus, Video } from "lucide-react";
import {
  buildFormDsl,
  buildSopCode,
  fieldConfigKind,
  hasProofField,
  type BuilderInitial,
  type BuilderOption,
  type BuilderStep,
  type ProofType,
  type SopBuilderInput,
  type SopSliceDomain,
  type SopTrigger,
  type StepTypeValue,
  type SubjectScope,
} from "./sop-derive";
import { publishSop, runDryRun, saveSopDraft, saveSopVersionDraft, type SaveSopResult } from "./sop-actions";
import { QuestionCard, type PriorStep } from "./question-card";
import { BuilderPreview } from "./builder-preview";
import type { DryRunResponse } from "@/lib/api/server";
import { copy, optionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";

function newId(prefix: string): string {
  return `${prefix}-${crypto.randomUUID()}`;
}
function newOption(label = ""): BuilderOption {
  return { id: newId("opt"), label };
}
function blankStep(type: StepTypeValue, label = ""): BuilderStep {
  const kind = fieldConfigKind(type);
  return {
    id: newId("q"),
    type,
    label,
    helpText: "",
    required: false,
    options: kind === "options" ? [newOption(), newOption()] : [],
    min: "",
    max: "",
    unit: "",
    placeholder: "",
    longText: false,
    visibleWhen: null,
  };
}

// After a reorder, a question's visibility condition may now point at a question that sits at or below
// it (a forward/self reference the emitter would drop, silently turning a conditionally-required field
// into an always-required one). Clear any such now-invalid condition so builder state stays consistent
// with what buildFormDsl can emit — same discipline removeStep already applies to dangling refs.
function sanitizeVisibility(rows: BuilderStep[]): BuilderStep[] {
  const indexById = new Map(rows.map((s, i) => [s.id, i]));
  return rows.map((s, i) => {
    if (!s.visibleWhen) return s;
    const refIdx = indexById.get(s.visibleWhen.refId);
    return refIdx === undefined || refIdx >= i ? { ...s, visibleWhen: null } : s;
  });
}

export function SopBuilder({
  pageContract,
  initial,
  editSopId,
  editBlocked = false,
}: {
  pageContract: AdminUiPageContract;
  initial?: BuilderInitial;
  editSopId?: string;
  editBlocked?: boolean;
}) {
  const pc = pageContract;
  const router = useRouter();
  const editing = Boolean(editSopId);
  const triggerOptions = optionGroup(pc, "sop_trigger_chips");
  const seedStepOptions = optionGroup(pc, "sop_seed_steps");
  const proofTypeOptions = optionGroup(pc, "proof_types");
  const subjectScopeOptions = optionGroup(pc, "subject_scopes");

  function firstKey(options: AdminUiOption[]): string {
    const [first] = options;
    if (!first) throw new Error(`page contract ${pc.route_id} empty option group`);
    return first.key;
  }
  function defaultKey(options: AdminUiOption[], preferred: string): string {
    return options.some((o) => o.key === preferred) ? preferred : firstKey(options);
  }

  const domain: SopSliceDomain = "vaccination"; // locked to the current slice
  // Editing an existing SOP seeds every control from its persisted version (faithful round-trip);
  // creating seeds name/trigger defaults + the contract's seed questions.
  const [name, setName] = useState(initial?.name ?? copy(pc, "modal.builder.default_name"));
  const [trigger, setTrigger] = useState<SopTrigger>(initial?.trigger ?? (defaultKey(triggerOptions, "cron") as SopTrigger));
  const [steps, setSteps] = useState<BuilderStep[]>(() => initial?.steps ?? seedStepOptions.map((s) => blankStep(s.key as StepTypeValue, s.label)));
  const [proofRequired, setProofRequired] = useState(initial?.proofRequired ?? true);
  const [proofType, setProofType] = useState<ProofType>(initial?.proofType ?? (defaultKey(proofTypeOptions, "video") as ProofType));
  const [verifyBeforeApply, setVerifyBeforeApply] = useState(initial?.verifyBeforeApply ?? true);
  const [minCount, setMinCount] = useState(initial?.minCount ?? 1);
  const [subjectScope, setSubjectScope] = useState<SubjectScope>(initial?.subjectScope ?? (defaultKey(subjectScopeOptions, "goat") as SubjectScope));

  const [saved, setSaved] = useState<SaveSopResult | null>(null);
  const [dryRun, setDryRun] = useState<DryRunResponse | null>(null);
  const [notice, setNotice] = useState<{ ok: boolean; message: string } | null>(null);
  const [pending, startTransition] = useTransition();
  const [dragIndex, setDragIndex] = useState<number | null>(null);

  const input: SopBuilderInput = useMemo(
    () => ({ name, domain, trigger, steps, proofRequired, proofType, verifyBeforeApply, minCount, subjectScope }),
    [name, trigger, steps, proofRequired, proofType, verifyBeforeApply, minCount, subjectScope],
  );

  const emitted = buildFormDsl(input);
  const fieldCount = emitted.fields.length;
  const ruleCount = emitted.rules?.length ?? 0;
  // While editing, a new version pins to the existing SOP code — the name may change but the code does not.
  const code = editing && initial ? initial.code : buildSopCode(input);
  const proofGapOk = !proofRequired || hasProofField(input);
  const canPublish = Boolean(saved?.ok && saved.versionId && saved.report?.valid);

  function resetResults() {
    setSaved(null);
    setDryRun(null);
  }
  function mutate(next: BuilderStep[]) {
    setSteps(next);
    resetResults();
  }
  function patchStep(id: string, patch: Partial<BuilderStep>) {
    mutate(steps.map((s) => (s.id === id ? { ...s, ...patch } : s)));
  }
  function changeType(id: string, type: StepTypeValue) {
    mutate(
      steps.map((s) => {
        if (s.id !== id) return s;
        if (fieldConfigKind(s.type) === fieldConfigKind(type)) return { ...s, type };
        const base = blankStep(type, s.label);
        return { ...base, id: s.id, helpText: s.helpText, required: s.required, visibleWhen: s.visibleWhen };
      }),
    );
  }
  function addStep() {
    mutate([...steps, blankStep("yesno")]);
  }
  function duplicateStep(id: string) {
    const idx = steps.findIndex((s) => s.id === id);
    if (idx < 0) return;
    const source = steps[idx];
    const clone: BuilderStep = {
      ...source,
      id: newId("q"),
      options: source.options.map((o) => ({ id: newId("opt"), label: o.label })),
    };
    mutate([...steps.slice(0, idx + 1), clone, ...steps.slice(idx + 1)]);
  }
  function removeStep(id: string) {
    // Drop any conditional visibility that pointed at the removed question so no rule dangles.
    mutate(steps.filter((s) => s.id !== id).map((s) => (s.visibleWhen?.refId === id ? { ...s, visibleWhen: null } : s)));
  }
  function moveStep(from: number, to: number) {
    if (to < 0 || to >= steps.length || from === to) return;
    const next = [...steps];
    const [item] = next.splice(from, 1);
    next.splice(to, 0, item);
    mutate(sanitizeVisibility(next));
  }

  function save() {
    setNotice(null);
    startTransition(async () => {
      // Editing an existing SOP appends a new DRAFT version to it; creating makes a new SOP + v1 draft.
      const res = editSopId ? await saveSopVersionDraft(editSopId, input) : await saveSopDraft(input);
      setSaved(res);
      setDryRun(null);
      setNotice({ ok: res.ok, message: res.message });
    });
  }
  function dry() {
    if (!saved?.sopId || !saved.versionId) return;
    startTransition(async () => {
      const res = await runDryRun(saved.sopId!, saved.versionId!);
      if (res.ok && res.data) setDryRun(res.data);
      else setNotice({ ok: false, message: res.message ?? copy(pc, "modal.builder.message.dry_run_failed") });
    });
  }
  function publish() {
    if (!saved?.sopId || !saved.versionId || saved.rowVersion === undefined) return;
    startTransition(async () => {
      const res = await publishSop(saved.sopId!, saved.versionId!, saved.rowVersion!);
      setNotice({ ok: res.ok, message: res.message });
      if (res.ok) {
        router.push("/sops");
        router.refresh();
      }
    });
  }

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pc, "crumb")} · {pc.title} · <b>{editing ? copy(pc, "builder.crumb_edit") : copy(pc, "builder.crumb_current")}</b>
          </div>
          <h1>{editing ? copy(pc, "builder.title_edit") : copy(pc, "modal.builder.title")}</h1>
          <div className="sub">{editing ? copy(pc, "builder.subtitle_edit") : copy(pc, "builder.subtitle")}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link className="btn" href="/sops">
          <ChevronLeft className="ic" /> {copy(pc, "builder.back")}
        </Link>
      </div>

      {notice ? (
        notice.ok ? (
          <div className="note" style={{ marginBottom: 12 }}>
            <span className="tag t-ok">{copy(pc, "modal.builder.notice_ok")}</span> {notice.message}
          </div>
        ) : (
          <div className="alert warn" style={{ marginBottom: 12 }}>
            <AlertTriangle className="ic" />
            <div>{notice.message}</div>
          </div>
        )
      ) : null}

      {editBlocked ? (
        <div className="alert warn" style={{ marginBottom: 12 }}>
          <AlertTriangle className="ic" />
          <div>{copy(pc, "builder.edit_blocked")}</div>
        </div>
      ) : null}

      <div className="builderwrap">
        <div className="buildermain">
          {/* Basics */}
          <section className="card">
            <div className="hd">
              <span className="fic" style={{ width: 26, height: 26, background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                <NotebookPen className="ic" style={{ width: 14 }} />
              </span>
              <h3 style={{ fontSize: 14 }}>{copy(pc, "builder.section.basics")}</h3>
            </div>
            <div className="bd">
              <div className="fld">
                <label>{copy(pc, "modal.builder.field.name")}</label>
                <div className="rowf">
                  <input
                    aria-label={copy(pc, "modal.builder.field.name")}
                    value={name}
                    onChange={(e) => {
                      setName(e.target.value);
                      resetResults();
                    }}
                    placeholder={copy(pc, "modal.builder.placeholder.name")}
                  />
                  <div
                    aria-label={copy(pc, "modal.builder.domain_aria")}
                    title={copy(pc, "modal.builder.domain_title")}
                    style={{ display: "flex", alignItems: "center", gap: 8, border: "1px solid var(--line)", background: "var(--bg)", borderRadius: 8, padding: "8px 10px" }}
                  >
                    <span className="tag t-pur">{copy(pc, "modal.builder.domain_label")}</span>
                    <span className="muted small">{copy(pc, "modal.builder.domain_locked")}</span>
                  </div>
                </div>
                <div className="muted small" style={{ marginTop: 5 }}>
                  {copy(pc, "modal.builder.code_prefix")} <span className="mono">{code}</span> · {copy(pc, "modal.builder.policy_label")}
                </div>
              </div>
              <div className="fld" style={{ marginBottom: 0 }}>
                <label>{copy(pc, "modal.builder.field.trigger")}</label>
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
            </div>
          </section>

          {/* Questions */}
          <section className="card">
            <div className="hd">
              <span className="fic" style={{ width: 26, height: 26, background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                <Check className="ic" style={{ width: 14 }} />
              </span>
              <h3 style={{ fontSize: 14 }}>{copy(pc, "builder.section.questions")}</h3>
              <span className="sp" style={{ flex: 1 }} />
              <span className="tag t-mut">{fieldCount}</span>
            </div>
            <div className="bd">
              <div className="muted small" style={{ marginBottom: 10 }}>
                {copy(pc, "builder.section.questions_hint")}
              </div>
              {steps.length === 0 ? <div className="note">{copy(pc, "builder.empty_questions")}</div> : null}
              <div className="qlist">
                {steps.map((step, i) => {
                  const priorSteps: PriorStep[] = steps.slice(0, i).map((s, j) => ({ id: s.id, position: j + 1, label: s.label }));
                  return (
                    <QuestionCard
                      key={step.id}
                      pageContract={pc}
                      step={step}
                      index={i}
                      total={steps.length}
                      priorSteps={priorSteps}
                      onPatch={(patch) => patchStep(step.id, patch)}
                      onChangeType={(type) => changeType(step.id, type)}
                      onRemove={() => removeStep(step.id)}
                      onDuplicate={() => duplicateStep(step.id)}
                      onMoveUp={() => moveStep(i, i - 1)}
                      onMoveDown={() => moveStep(i, i + 1)}
                      dragging={dragIndex === i}
                      onDragStart={() => setDragIndex(i)}
                      onDragEnter={() => {
                        if (dragIndex !== null && dragIndex !== i) {
                          moveStep(dragIndex, i);
                          setDragIndex(i);
                        }
                      }}
                      onDragEnd={() => setDragIndex(null)}
                    />
                  );
                })}
              </div>
              <button type="button" className="btn sm" style={{ marginTop: 10 }} onClick={addStep}>
                <Plus className="ic" /> {copy(pc, "builder.add_question")}
              </button>
            </div>
          </section>

          {/* Gates & proof */}
          <section className="card">
            <div className="hd">
              <span className="fic" style={{ width: 26, height: 26, background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                <Video className="ic" style={{ width: 14 }} />
              </span>
              <h3 style={{ fontSize: 14 }}>{copy(pc, "builder.section.gates")}</h3>
            </div>
            <div className="bd">
              <div className="cfgchk" style={{ flexWrap: "wrap", gap: 14 }}>
                <label>
                  <input type="checkbox" checked={proofRequired} onChange={(e) => { setProofRequired(e.target.checked); resetResults(); }} />{" "}
                  {copy(pc, "modal.builder.label.proof_required")}
                </label>
                <label>
                  <input type="checkbox" checked={verifyBeforeApply} onChange={(e) => { setVerifyBeforeApply(e.target.checked); resetResults(); }} />{" "}
                  {copy(pc, "modal.builder.label.verify_before_apply")}
                </label>
              </div>
              <div className="rowf" style={{ marginTop: 8 }}>
                <select aria-label={copy(pc, "modal.builder.field.proof_type")} value={proofType} onChange={(e) => { setProofType(e.target.value as ProofType); resetResults(); }}>
                  {proofTypeOptions.map((p) => (
                    <option key={p.key} value={p.key}>{p.label}</option>
                  ))}
                </select>
                <input
                  aria-label={copy(pc, "modal.builder.field.min_count")}
                  type="number"
                  min={1}
                  value={minCount}
                  onChange={(e) => { setMinCount(Number(e.target.value)); resetResults(); }}
                />
                <select aria-label={copy(pc, "modal.builder.field.subject_scope")} value={subjectScope} onChange={(e) => { setSubjectScope(e.target.value as SubjectScope); resetResults(); }}>
                  {subjectScopeOptions.map((sc) => (
                    <option key={sc.key} value={sc.key}>{sc.label}</option>
                  ))}
                </select>
              </div>
              {!proofGapOk ? (
                <div className="alert warn" style={{ marginTop: 8 }}>
                  <AlertTriangle className="ic" />
                  <div>{copy(pc, "modal.builder.proof_gap")}</div>
                </div>
              ) : null}
            </div>
          </section>
        </div>

        {/* Aside: preview + review */}
        <aside className="builderside">
          <section className="card">
            <div className="hd">
              <h3 style={{ fontSize: 14 }}>{copy(pc, "builder.preview.title")}</h3>
            </div>
            <div className="bd">
              <BuilderPreview pc={pc} steps={steps} />
            </div>
          </section>

          <section className="card">
            <div className="hd">
              <h3 style={{ fontSize: 14 }}>{copy(pc, "builder.section.review")}</h3>
            </div>
            <div className="bd">
              <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginBottom: 10 }}>
                <span className="tag t-mut">{fieldCount} {copy(pc, "builder.summary.fields")}</span>
                <span className="tag t-mut">{ruleCount} {copy(pc, "builder.summary.rules")}</span>
                {proofRequired ? <span className="tag t-pur">{proofType} {copy(pc, "builder.summary.proof")}</span> : null}
              </div>

              {saved?.report ? (
                <div className="note" style={{ marginBottom: 10 }}>
                  <span className={`tag ${saved.report.valid ? "t-ok" : "t-warn"}`}>
                    {saved.report.valid ? copy(pc, "modal.builder.validation.valid") : copy(pc, "modal.builder.validation.issues")}
                  </span>{" "}
                  {saved.versionId ? <span className="muted small">{copy(pc, "modal.builder.label.draft")} · {saved.versionId.slice(0, 8)}</span> : null}
                  {saved.report.errors.length > 0 ? (
                    <ul className="muted small" style={{ margin: "6px 0 0", paddingLeft: 18 }}>
                      {saved.report.errors.map((e, i) => (
                        <li key={i}><span className="mono">{e.field}</span>: {e.message}</li>
                      ))}
                    </ul>
                  ) : null}
                </div>
              ) : null}

              {dryRun ? (
                <div className="note" style={{ marginBottom: 10 }}>
                  <span className={`tag ${dryRun.valid ? "t-ok" : "t-warn"}`}>{copy(pc, "modal.builder.label.dry_run")} {dryRun.valid ? copy(pc, "modal.builder.notice_ok") : dryRun.final_state}</span>{" "}
                  <span className="muted small">
                    {copy(pc, "modal.builder.label.workflow")} {dryRun.workflow_path.join(" → ") || copy(pc, "label.placeholder")} · {copy(pc, "modal.builder.label.final")} {dryRun.final_state}
                  </span>
                  {dryRun.field_states.length > 0 ? (
                    <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginTop: 8 }}>
                      {dryRun.field_states.map((fs) => (
                        <span key={fs.key} className={`tag ${fs.blocked ? "t-warn" : fs.required ? "t-info" : "t-mut"}`}>
                          {fs.key}
                          {fs.required ? " *" : ""}
                          {fs.blocked ? ` · ${copy(pc, "modal.builder.label.blocked")}` : ""}
                        </span>
                      ))}
                    </div>
                  ) : null}
                </div>
              ) : null}

              <div className="builderactions">
                <button
                  type="button"
                  className="btn"
                  onClick={save}
                  disabled={pending || !proofGapOk || editBlocked}
                  title={editBlocked ? copy(pc, "builder.edit_blocked") : undefined}
                  style={editBlocked ? { opacity: 0.45, cursor: "not-allowed" } : undefined}
                >
                  {pending ? copy(pc, "modal.builder.action.saving") : saved?.ok ? copy(pc, "modal.builder.action.re_save") : copy(pc, "modal.builder.action.save")}
                </button>
                <button
                  type="button"
                  className="btn"
                  onClick={dry}
                  disabled={pending || !saved?.versionId}
                  title={!saved?.versionId ? copy(pc, "modal.builder.title.save_first") : copy(pc, "modal.builder.title.preview")}
                  style={!saved?.versionId ? { opacity: 0.45, cursor: "not-allowed" } : undefined}
                >
                  <Play className="ic" /> {copy(pc, "modal.builder.action.dry_run")}
                </button>
                <button
                  type="button"
                  className="btn p"
                  onClick={publish}
                  disabled={pending || !canPublish}
                  title={!saved?.versionId ? copy(pc, "modal.builder.title.save_first") : canPublish ? copy(pc, "modal.builder.title.publish") : copy(pc, "modal.builder.title.resolve")}
                  style={!canPublish ? { opacity: 0.45, cursor: "not-allowed" } : undefined}
                >
                  <Check className="ic" /> {copy(pc, "action.publish")}
                </button>
              </div>
            </div>
          </section>
        </aside>
      </div>
    </div>
  );
}
