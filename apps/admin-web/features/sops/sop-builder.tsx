"use client";


import { useMemo, useState, useTransition, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import CardContent from "@mui/material/CardContent";
import Stack from "@mui/material/Stack";
import Grid from "@mui/material/Grid";
import MuiTextField from "@mui/material/TextField";
import { Label } from "@/components/minimal/label";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { varAlpha } from "minimal-shared/utils";
import Box from "@mui/material/Box";
import { TemplateTabs } from "@/components/app/template-tabs";
import { EditorHeader, FieldRow, FieldSelect } from "./editor-chrome";
import { CheckLine, EDITOR_ICON, EditorPage, Hint, ProblemList, Spacer } from "./editor-parts";
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

/** A builder section card: template Card + CardHeader (icon avatar, title, action) + CardContent. */
function SectionCard({ icon, title, action, children }: { icon?: IconifyName; title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <Card>
      <CardHeader
        avatar={
          icon ? (
            <Box sx={(theme) => ({ display: "inline-flex", p: 0.75, borderRadius: "var(--r-md)", color: "primary.main", bgcolor: varAlpha(theme.vars.palette.primary.mainChannel, 0.12) })}>
              <Iconify icon={icon} width={18} />
            </Box>
          ) : undefined
        }
        title={title}
        action={action}
        slotProps={{ title: { variant: "subtitle1" }, action: { sx: { alignSelf: "center", m: 0 } } }}
      />
      <CardContent>
        <Stack spacing={2}>{children}</Stack>
      </CardContent>
    </Card>
  );
}

/** A read-only fact the page decides (domain, SOP kind): outlined box, label + value, wraps on a phone. */
function LockedFact({ ariaLabel, title, children, testId }: { ariaLabel: string; title: string; children: ReactNode; testId?: string }) {
  return (
    <Stack
      direction="row"
      spacing={1}
      useFlexGap
      aria-label={ariaLabel}
      title={title}
      data-testid={testId}
      sx={{ alignItems: "center", flexWrap: "wrap", justifyContent: { xs: "space-between", sm: "flex-start" }, minHeight: "var(--tap-min)", px: 1.25, border: 1, borderColor: "divider", borderRadius: "var(--r-md)", bgcolor: "background.neutral" }}
    >
      {children}
    </Stack>
  );
}

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
    <EditorPage>
      <EditorHeader
        crumbs={[copy(pc, "crumb"), pc.title, editing ? copy(pc, "builder.crumb_edit") : copy(pc, "builder.crumb_current")]}
        title={editing ? copy(pc, "builder.title_edit") : copy(pc, "modal.builder.title")}
        subtitle={editing ? copy(pc, "builder.subtitle_edit") : copy(pc, "builder.subtitle")}
        backHref={basePath}
        actions={
          <Button variant="contained" color="primary" startIcon={<Iconify icon="solar:eye-bold" />} onClick={() => setPreviewOpen(true)}>
            {copy(pc, "builder.preview.open")}
          </Button>
        }
      />

      {notice ? (
        <Alert severity={notice.ok ? "success" : "warning"}>
          {notice.ok ? <Label color="success" sx={{ mr: 1 }}>{copy(pc, "modal.builder.notice_ok")}</Label> : null}
          {notice.message}
        </Alert>
      ) : null}

      {editBlocked ? <Alert severity="warning">{copy(pc, "builder.edit_blocked")}</Alert> : null}

      <Grid container spacing={3}>
        <Grid size={{ xs: 12, lg: 8 }} data-testid="builder-main">
          <Stack spacing={3}>
          {/* Basics */}
          <SectionCard icon="solar:notes-bold-duotone" title={copy(pc, "builder.section.basics")}>
            {/* Phone: the name, domain and kind stack instead of squeezing onto one row. */}
            <Stack direction={{ xs: "column", sm: "row" }} spacing={1.25} useFlexGap sx={{ flexWrap: "wrap", "& > *": { flex: { sm: "1 1 var(--field-basis)" }, minWidth: 0 }, "--field-basis": (theme) => theme.spacing(22.5) }}>
              <MuiTextField
                label={copy(pc, "modal.builder.field.name")}
                size="small"
                value={name}
                placeholder={copy(pc, "modal.builder.placeholder.name")}
                slotProps={{ inputLabel: { shrink: true }, htmlInput: { "data-testid": "builder-name" } }}
                onChange={(e) => {
                  setName(e.target.value);
                  resetResults();
                }}
              />
              <LockedFact ariaLabel={copy(pc, "modal.builder.domain_aria")} title={copy(pc, "modal.builder.domain_title")}>
                <Label color="secondary">{copy(pc, "modal.builder.domain_label")}</Label>
                <Hint caption>{copy(pc, "modal.builder.domain_locked")}</Hint>
              </LockedFact>
              {/* The SOP KIND (2026-09-18) is decided by the page: a module page authors
                  module-level SOPs, Configuration › Work instructions authors general ones. */}
              <LockedFact
                ariaLabel={copy(pc, "studio.kind.label")}
                title={copy(pc, domain === "general" ? "studio.kind.general_hint" : "studio.kind.module_hint")}
                testId="builder-kind"
              >
                <Hint caption>{copy(pc, "studio.kind.label")}</Hint>
                <Label>{copy(pc, domain === "general" ? "studio.kind.general" : "studio.kind.module")}</Label>
              </LockedFact>
            </Stack>
            {/* The SOP code (`counts.herd_operation`) is an internal key, never shown: the
                page already names the module, and this line says what the SOP governs. */}
            <Hint caption>{copy(pc, "modal.builder.policy_label")}</Hint>
            <Stack spacing={1}>
              <Typography variant="subtitle2" component="span">{copy(pc, "modal.builder.field.trigger")}</Typography>
              <TemplateTabs
                variant="pill"
                ariaLabel={copy(pc, "modal.builder.field.trigger")}
                value={trigger}
                items={triggerOptions.map((t) => ({ value: t.key, label: t.label }))}
                onChange={(next) => {
                  setTrigger(next as SopTrigger);
                  resetResults();
                }}
              />
            </Stack>
          </SectionCard>

          {/* Questions */}
          <SectionCard icon="eva:checkmark-circle-2-outline" title={copy(pc, "builder.section.questions")} action={<Label>{fieldCount}</Label>}>
            <Hint caption>{copy(pc, "builder.section.questions_hint")}</Hint>
            {steps.length === 0 ? <Alert severity="info">{copy(pc, "builder.empty_questions")}</Alert> : null}
            <Stack spacing={1.5}>
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
            </Stack>
            <Button color="primary" variant="outlined" size="small" startIcon={<Iconify icon={EDITOR_ICON.add} />} sx={{ alignSelf: "flex-start" }} onClick={addStep}>
              {copy(pc, "builder.add_question")}
            </Button>
          </SectionCard>

          {/* Gates & proof -- not for a general work instruction: its steps carry their own proofs
              and the document policy is the seeded run-scoped one (PR 308 review). */}
          {domain === "general" ? null : (
          <SectionCard icon="solar:videocamera-record-bold" title={copy(pc, "builder.section.gates")}>
            <Stack direction="row" spacing={2} useFlexGap sx={{ flexWrap: "wrap" }}>
              <CheckLine checked={proofRequired} onChange={(on) => { setProofRequired(on); resetResults(); }} label={copy(pc, "modal.builder.label.proof_required")} />
              <CheckLine checked={verifyBeforeApply} onChange={(on) => { setVerifyBeforeApply(on); resetResults(); }} label={copy(pc, "modal.builder.label.verify_before_apply")} />
            </Stack>
            <FieldRow>
              <FieldSelect
                label={copy(pc, "modal.builder.field.proof_type")}
                value={proofType}
                options={proofTypeOptions.map((p) => ({ value: p.key, label: p.label }))}
                onChange={(next) => { setProofType(next as ProofType); resetResults(); }}
              />
              <MuiTextField
                label={copy(pc, "modal.builder.field.min_count")}
                size="small"
                type="number"
                value={minCount}
                slotProps={{ htmlInput: { min: 1 } }}
                sx={{ flex: (theme) => `0 1 ${theme.spacing(20)}` }}
                onChange={(e) => { setMinCount(Number(e.target.value)); resetResults(); }}
              />
              <FieldSelect
                label={copy(pc, "modal.builder.field.subject_scope")}
                value={subjectScope}
                options={subjectScopeOptions.map((sc) => ({ value: sc.key, label: sc.label }))}
                onChange={(next) => { setSubjectScope(next as SubjectScope); resetResults(); }}
              />
            </FieldRow>
            <Hint caption>{copy(pc, "builder.gates.subject_hint")}</Hint>
            {!proofGapOk ? <Alert severity="warning">{copy(pc, "modal.builder.proof_gap")}</Alert> : null}
          </SectionCard>
          )}
          </Stack>
        </Grid>

        {/* Aside: preview + review */}
        <Grid size={{ xs: 12, lg: 4 }} data-testid="builder-side">
          <Stack spacing={3} sx={{ position: { lg: "sticky" }, top: { lg: 14 } }}>
          <SectionCard title={copy(pc, "builder.preview.title")}>
            <BuilderPreview pc={pc} steps={steps} />
          </SectionCard>

          <SectionCard title={copy(pc, "builder.section.review")}>
            <Stack direction="row" spacing={0.75} useFlexGap sx={{ flexWrap: "wrap" }}>
              <Label>{fieldCount} {copy(pc, "builder.summary.fields")}</Label>
              <Label>{ruleCount} {copy(pc, "builder.summary.rules")}</Label>
              {proofRequired && domain !== "general" ? <Label color="secondary">{proofType} {copy(pc, "builder.summary.proof")}</Label> : null}
            </Stack>

            {saved?.report ? (
              <Alert severity={saved.report.valid ? "success" : "warning"} icon={false}>
                <Label color={saved.report.valid ? "success" : "warning"} sx={{ mr: 1 }}>
                  {saved.report.valid ? copy(pc, "modal.builder.validation.valid") : copy(pc, "modal.builder.validation.issues")}
                </Label>
                {saved.versionId ? <Typography variant="caption" component="span" sx={{ color: "text.secondary" }}>{copy(pc, "modal.builder.label.draft")} · {saved.versionId.slice(0, 8)}</Typography> : null}
                <ProblemList items={saved.report.errors.map((e) => `${e.field}: ${e.message}`)} max={50} muted />
              </Alert>
            ) : null}

            {dryRun ? (
              <Alert severity={dryRun.valid ? "success" : "warning"} icon={false}>
                <Label color={dryRun.valid ? "success" : "warning"} sx={{ mr: 1 }}>
                  {copy(pc, "modal.builder.label.dry_run")} {dryRun.valid ? copy(pc, "modal.builder.notice_ok") : dryRun.final_state}
                </Label>
                <Typography variant="caption" component="span" sx={{ color: "text.secondary" }}>
                  {copy(pc, "modal.builder.label.workflow")} {dryRun.workflow_path.join(" → ") || copy(pc, "label.placeholder")} · {copy(pc, "modal.builder.label.final")} {dryRun.final_state}
                </Typography>
                {dryRun.field_states.length > 0 ? (
                  <Stack direction="row" spacing={0.75} useFlexGap sx={{ flexWrap: "wrap", mt: 1 }}>
                    {dryRun.field_states.map((fs) => (
                      <Label key={fs.key} color={fs.blocked ? "warning" : fs.required ? "info" : "default"}>
                        {fs.key}
                        {fs.required ? " *" : ""}
                        {fs.blocked ? ` · ${copy(pc, "modal.builder.label.blocked")}` : ""}
                      </Label>
                    ))}
                  </Stack>
                ) : null}
              </Alert>
            ) : null}

            <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap", alignItems: "center" }}>
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
                startIcon={<Iconify icon="carbon:play" />}
                onClick={dry}
                disabled={pending || !saved?.versionId}
                title={!saved?.versionId ? copy(pc, "modal.builder.title.save_first") : copy(pc, "modal.builder.title.preview")}
              >
                {copy(pc, "modal.builder.action.dry_run")}
              </Button>
              <Spacer />
              <Button
                variant="contained"
                color="primary"
                startIcon={<Iconify icon={EDITOR_ICON.check} />}
                onClick={publish}
                disabled={pending || !canPublish}
                title={!saved?.versionId ? copy(pc, "modal.builder.title.save_first") : canPublish ? copy(pc, "modal.builder.title.publish") : copy(pc, "modal.builder.title.resolve")}
              >
                {copy(pc, "action.publish")}
              </Button>
            </Stack>
          </SectionCard>
          </Stack>
        </Grid>
      </Grid>

      <Dialog fullWidth maxWidth="sm" open={previewOpen} onClose={() => setPreviewOpen(false)} slotProps={{ paper: { "aria-label": copy(pc, "builder.preview.title") } }}>
        <DialogTitle component="div" sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
          <Box sx={(theme) => ({ display: "inline-flex", p: 0.75, borderRadius: "var(--r-md)", color: "primary.main", bgcolor: varAlpha(theme.vars.palette.primary.mainChannel, 0.12) })}>
            <Iconify icon="solar:eye-bold" />
          </Box>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography variant="overline" component="div" sx={{ color: "text.secondary" }}>{copy(pc, "modal.builder.eyebrow")}</Typography>
            <Typography variant="h6" component="h2">{copy(pc, "builder.preview.title")}</Typography>
          </Box>
          <IconButton onClick={() => setPreviewOpen(false)} aria-label={copy(pc, "builder.preview.close")}>
            <Iconify icon="mingcute:close-line" />
          </IconButton>
        </DialogTitle>
        <DialogContent dividers>
          <BuilderPreview pc={pc} steps={steps} />
        </DialogContent>
      </Dialog>
    </EditorPage>
  );
}
