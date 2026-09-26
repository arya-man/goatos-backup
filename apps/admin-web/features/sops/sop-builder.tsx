"use client";

import { Tag } from "@/components/ui-primitives";

import { useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { Check, Eye, NotebookPen, Play, Plus, Video } from "lucide-react";
import Card from "@mui/material/Card";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import Box from "@mui/material/Box";
import type { Theme } from "@mui/material/styles";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import { EditorHeader, FieldRow, FieldSelect } from "./editor-chrome";
import {
  buildFormDsl,
  fieldConfigKind,
  hasProofField,
  type BuilderInitial,
  type BuilderOption,
  type BuilderStep,
  type ProofType,
  type SopBuilderInput,
  type SopScopeDomain,
  type SopTrigger,
  type StepTypeValue,
  type SubjectScope,
} from "./sop-derive";
import { publishSop, runDryRun, saveSopDraft, saveSopVersionDraft, type SaveSopResult } from "./sop-actions";
import { publishedHref } from "./published-href";
import { QuestionCard, type PriorStep } from "./question-card";
import { BuilderPreview } from "./builder-preview";
import type { DryRunResponse } from "@/lib/api/server";
import { copy, optionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import Alert from "@mui/material/Alert";

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
    multiScan: type === "goat_scan", // default a goat scan to multi (whole-shed batch is the norm)
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

// Builder basics: the name / domain / kind row stacks on a phone and the read-only chips wrap
// (template compact field group). `&&&` keeps it above the shared builder row rule.
const BASICS_ROW_SX = (theme: Theme) => ({
  [theme.breakpoints.down("sm")]: {
    "&&&": { display: "grid", gridTemplateColumns: "minmax(0, 1fr)", gap: 1.25 },
    "&& > *": { minWidth: 0, width: 1 },
    "&& > div[aria-label]": { minHeight: "var(--tap-min)", justifyContent: "space-between", flexWrap: "wrap", alignContent: "center" },
    "&& > div[aria-label] .tag": { flex: "0 0 auto" },
    "&& > div[aria-label] .muted": { minWidth: 0, whiteSpace: "normal", textAlign: "right" },
  },
});

export function SopBuilder({
  pageContract,
  basePath,
  domain,
  initial,
  editSopId,
  editBlocked = false,
}: {
  pageContract: AdminUiPageContract;
  /** The module SOP page path this builder returns to (e.g. "/vaccination/sops"). */
  basePath: string;
  /** The module slice this route authors. Locked by the route, not user-selectable. */
  domain: SopScopeDomain;
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
  const [previewOpen, setPreviewOpen] = useState(false);

  const input: SopBuilderInput = useMemo(
    () => ({ name, domain, trigger, steps, proofRequired, proofType, verifyBeforeApply, minCount, subjectScope }),
    [name, domain, trigger, steps, proofRequired, proofType, verifyBeforeApply, minCount, subjectScope],
  );

  const emitted = buildFormDsl(input);
  const fieldCount = emitted.fields.length;
  const ruleCount = emitted.rules?.length ?? 0;
  const proofGapOk = domain === "general" || !proofRequired || hasProofField(input);
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
        router.push(publishedHref(basePath, saved.sopId!, res.versionNumber));
        router.refresh();
      }
    });
  }

  return (
    <div className="kit-enter screen on sop-kit">
      <EditorHeader
        crumbs={[copy(pc, "crumb"), pc.title, editing ? copy(pc, "builder.crumb_edit") : copy(pc, "builder.crumb_current")]}
        title={editing ? copy(pc, "builder.title_edit") : copy(pc, "modal.builder.title")}
        subtitle={editing ? copy(pc, "builder.subtitle_edit") : copy(pc, "builder.subtitle")}
        backHref={basePath}
        actions={
          <Button variant="contained" color="primary" startIcon={<Eye size={18} />} onClick={() => setPreviewOpen(true)}>
            {copy(pc, "builder.preview.open")}
          </Button>
        }
      />

      {notice ? (
        <div>
          {notice.ok ? (
            <div className="note" style={{ marginBottom: 12 }}>
              <Tag tone="ok">{copy(pc, "modal.builder.notice_ok")}</Tag> {notice.message}
            </div>
          ) : (
            <Alert severity="warning" style={{ marginBottom: 12 }}><div>{notice.message}</div>
            </Alert>
          )}
        </div>
      ) : null}

      {editBlocked ? (
        <div>
          <Alert severity="warning" style={{ marginBottom: 12 }}><div>{copy(pc, "builder.edit_blocked")}</div>
          </Alert>
        </div>
      ) : null}

      <div className="builderwrap">
        <div className="kit-enter buildermain">
          {/* Basics */}
          <div>
          <Card className="card">
            <div className="hd">
              <span className="fic" style={{ width: 26, height: 26, background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                <NotebookPen className="ic" style={{ width: 14 }} />
              </span>
              <h3 style={{ fontSize: 14 }}>{copy(pc, "builder.section.basics")}</h3>
            </div>
            <div className="bd">
              <div className="fld">
                <label>{copy(pc, "modal.builder.field.name")}</label>
                {/* Phone: the name, domain and kind stack instead of squeezing onto one row. */}
                <Box className="rowf" sx={BASICS_ROW_SX}>
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
                    style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 8, border: "1px solid var(--line)", background: "var(--bg)", borderRadius: 8, padding: "8px 10px" }}
                  >
                    <Tag tone="pur">{copy(pc, "modal.builder.domain_label")}</Tag>
                    <span className="muted small">{copy(pc, "modal.builder.domain_locked")}</span>
                  </div>
                  {/* The SOP KIND (2026-09-18) is decided by the page: a module page authors
                      module-level SOPs, Configuration › Work instructions authors general ones. */}
                  <div
                    aria-label={copy(pc, "studio.kind.label")}
                    title={copy(pc, domain === "general" ? "studio.kind.general_hint" : "studio.kind.module_hint")}
                    style={{ display: "flex", alignItems: "center", gap: 8, border: "1px solid var(--line)", background: "var(--bg)", borderRadius: 8, padding: "8px 10px" }}
                    data-testid="builder-kind"
                  >
                    <span className="muted small">{copy(pc, "studio.kind.label")}</span>
                    <span className="tag">{copy(pc, domain === "general" ? "studio.kind.general" : "studio.kind.module")}</span>
                  </div>
                </Box>
                <div className="muted small" style={{ marginTop: 5 }}>
                  {/* The SOP code (`counts.herd_operation`) is an internal key, never shown: the
                      page already names the module, and this line says what the SOP governs. */}
                  {copy(pc, "modal.builder.policy_label")}
                </div>
              </div>
              <div className="fld" style={{ marginBottom: 0 }}>
                <label>{copy(pc, "modal.builder.field.trigger")}</label>
                <AnimatedTabs
                  variant="pill"
                  ariaLabel={copy(pc, "modal.builder.field.trigger")}
                  value={trigger}
                  items={triggerOptions.map((t) => ({ value: t.key, label: t.label }))}
                  onChange={(next) => {
                    setTrigger(next as SopTrigger);
                    resetResults();
                  }}
                />
              </div>
            </div>
          </Card>
          </div>

          {/* Questions */}
          <div>
          <Card className="card">
            <div className="hd">
              <span className="fic" style={{ width: 26, height: 26, background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                <Check className="ic" style={{ width: 14 }} />
              </span>
              <h3 style={{ fontSize: 14 }}>{copy(pc, "builder.section.questions")}</h3>
              <span className="sp" style={{ flex: 1 }} />
              <Tag tone="mut">{fieldCount}</Tag>
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
              <Button color="primary" variant="outlined" size="small" startIcon={<Plus size={16} />} style={{ marginTop: 10 }} onClick={addStep}>
                {copy(pc, "builder.add_question")}
              </Button>
            </div>
          </Card>
          </div>

          {/* Gates & proof -- not for a general work instruction: its steps carry their own proofs
              and the document policy is the seeded run-scoped one (PR 308 review). */}
          {domain === "general" ? null : (
          <div>
          <Card className="card">
            <div className="hd">
              <span className="fic" style={{ width: 26, height: 26, background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                <Video className="ic" style={{ width: 14 }} />
              </span>
              <h3 style={{ fontSize: 14 }}>{copy(pc, "builder.section.gates")}</h3>
            </div>
            <div className="bd">
              <div className="cfgchk" style={{ flexWrap: "wrap", gap: 14 }}>
                <FormControlLabel control={<Checkbox checked={proofRequired} onChange={(e) => { setProofRequired(e.target.checked); resetResults(); }} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{" "}
                  {copy(pc, "modal.builder.label.proof_required")}</>} />
                <FormControlLabel control={<Checkbox checked={verifyBeforeApply} onChange={(e) => { setVerifyBeforeApply(e.target.checked); resetResults(); }} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{" "}
                  {copy(pc, "modal.builder.label.verify_before_apply")}</>} />
              </div>
              <FieldRow>
                <FieldSelect
                  label={copy(pc, "modal.builder.field.proof_type")}
                  value={proofType}
                  options={proofTypeOptions.map((p) => ({ value: p.key, label: p.label }))}
                  onChange={(next) => { setProofType(next as ProofType); resetResults(); }}
                />
                <label className="numfield" style={{ flex: "0 1 160px" }}>
                  <span className="numlbl">{copy(pc, "modal.builder.field.min_count")}</span>
                  <input
                    aria-label={copy(pc, "modal.builder.field.min_count")}
                    type="number"
                    min={1}
                    value={minCount}
                    onChange={(e) => { setMinCount(Number(e.target.value)); resetResults(); }}
                  />
                </label>
                <FieldSelect
                  label={copy(pc, "modal.builder.field.subject_scope")}
                  value={subjectScope}
                  options={subjectScopeOptions.map((sc) => ({ value: sc.key, label: sc.label }))}
                  onChange={(next) => { setSubjectScope(next as SubjectScope); resetResults(); }}
                />
              </FieldRow>
              <div className="muted small" style={{ marginTop: 6 }}>{copy(pc, "builder.gates.subject_hint")}</div>
              {!proofGapOk ? (
                <Alert severity="warning" style={{ marginTop: 8 }}><div>{copy(pc, "modal.builder.proof_gap")}</div>
                </Alert>
              ) : null}
            </div>
          </Card>
          </div>
          )}
        </div>

        {/* Aside: preview + review */}
        <div className="kit-enter builderside">
          <div>
          <Card className="card">
            <div className="hd">
              <h3 style={{ fontSize: 14 }}>{copy(pc, "builder.preview.title")}</h3>
            </div>
            <div className="bd">
              <BuilderPreview pc={pc} steps={steps} />
            </div>
          </Card>
          </div>

          <div>
          <Card className="card">
            <div className="hd">
              <h3 style={{ fontSize: 14 }}>{copy(pc, "builder.section.review")}</h3>
            </div>
            <div className="bd">
              <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginBottom: 10 }}>
                <Tag tone="mut">{fieldCount} {copy(pc, "builder.summary.fields")}</Tag>
                <Tag tone="mut">{ruleCount} {copy(pc, "builder.summary.rules")}</Tag>
                {proofRequired && domain !== "general" ? <Tag tone="pur">{proofType} {copy(pc, "builder.summary.proof")}</Tag> : null}
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
                <Button
                  color="primary"
                  variant="outlined"
                  onClick={save}
                  loading={pending}
                  disabled={pending || !proofGapOk || editBlocked}
                  title={editBlocked ? copy(pc, "builder.edit_blocked") : undefined}
                >
                  {pending ? copy(pc, "modal.builder.action.saving") : saved?.ok ? copy(pc, "modal.builder.action.re_save") : copy(pc, "modal.builder.action.save")}
                </Button>
                <Button
                  color="primary"
                  variant="outlined"
                  startIcon={<Play size={16} />}
                  onClick={dry}
                  disabled={pending || !saved?.versionId}
                  title={!saved?.versionId ? copy(pc, "modal.builder.title.save_first") : copy(pc, "modal.builder.title.preview")}
                >
                  {copy(pc, "modal.builder.action.dry_run")}
                </Button>
                <span className="spacer" style={{ flex: 1 }} />
                <Button
                  variant="contained"
                  color="primary"
                  startIcon={<Check size={16} />}
                  onClick={publish}
                  disabled={pending || !canPublish}
                  title={!saved?.versionId ? copy(pc, "modal.builder.title.save_first") : canPublish ? copy(pc, "modal.builder.title.publish") : copy(pc, "modal.builder.title.resolve")}
                >
                  {copy(pc, "action.publish")}
                </Button>
              </div>
            </div>
          </Card>
          </div>
        </div>
      </div>

      <Dialog fullWidth maxWidth="sm" open={previewOpen} onClose={() => setPreviewOpen(false)} slotProps={{ paper: { "aria-label": copy(pc, "builder.preview.title") } }}>
        <DialogTitle component="div" sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand)", width: 32, height: 32, borderRadius: 9 }}>
            <Eye className="ic" />
          </span>
          <div style={{ flex: 1, minWidth: 0 }}>
            <Typography variant="overline" component="div" sx={{ color: "text.secondary" }}>{copy(pc, "modal.builder.eyebrow")}</Typography>
            <Typography variant="h6" component="h2">{copy(pc, "builder.preview.title")}</Typography>
          </div>
          <IconButton onClick={() => setPreviewOpen(false)} aria-label={copy(pc, "builder.preview.close")}>
            <Iconify icon="mingcute:close-line" />
          </IconButton>
        </DialogTitle>
        <DialogContent dividers>
          <BuilderPreview pc={pc} steps={steps} />
        </DialogContent>
      </Dialog>
    </div>
  );
}
