"use client";

import { useMemo, useState, useTransition } from "react";
import { BranchField } from "./branch-field";
import { FollowUpFlow } from "./followup-flow";
import type { FlowInsert } from "./flow-layout";
import { useRouter } from "next/navigation";
import MuiTextField from "@mui/material/TextField";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import ButtonBase from "@mui/material/ButtonBase";
import Stack from "@mui/material/Stack";
import { visuallyHidden } from "@mui/utils";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { TemplateTabs, TabPanel } from "@/components/app/template-tabs";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { EditorHeader, InlineSelect, StudioViewToggle } from "./editor-chrome";
import { AddButton, CheckLine, ConfigBox, EDITOR_ICON, EditorPage, FieldGrid, Hint, IconAction, LockedNote, MoveActions, NumBadge, ProblemList, QuestionShell, Spacer, StickyBar } from "./editor-parts";
import {
  ENGINE_BOUND_TASK_TYPES,
  blankStep,
  emitFollowUp,
  expandSeriesRows,
  followStepKey,
  followUpProblems,
  keyForTitle,
  slugKey,
  type FollowUpRows,
  type FollowUpStepRow,
  type ScheduleKind,
  type SeriesBasis,
} from "./followup-model";
import { publishFollowUpVersion, saveFollowUpVersion, type FollowUpSaveResult } from "./sop-actions";
import { publishedHref } from "./published-href";
import { followUpCopy, roundWhen } from "./followup-summary";
import Alert from "@mui/material/Alert";

// SOP-DRIVEN HERD OPERATIONS (maintainer decision 2026-09-13,
// docs/decisions/sop-driven-herd-operations.md). The operator-steps editor for a Herd Operations
// SOP: one section per track (kid / mother / death / reconcile / shifting), one card per step.
// Every label, option list and reason is backend copy from the page contract; the step types and
// their answer kinds are the tenant's Task Type Registry (option groups sop_task_types /
// sop_task_type_answer_kinds), never a constant here.
//
// Engine-bound steps (weigh, tag, record_pen, feed_colostrum, death_evidence, return_to_pen) keep
// their key and type fixed -- the server matches behaviour on them -- and everything else is
// editable. Publishing applies to workflows opened from then on.


// Spec §7 totals block under an editor section's items (theme tokens only).
const TOTALS_SX = {
  display: "flex",
  alignItems: "center",
  columnGap: 2.25,
  rowGap: 0.5,
  flexWrap: "wrap",
  mt: 1.5,
  pt: 1.5,
  borderTop: 1,
  borderTopStyle: "dashed",
  borderColor: "divider",
  typography: "caption",
  color: "text.secondary",
  "& b": { typography: "subtitle2", color: "text.primary", fontVariantNumeric: "tabular-nums" },
} as const;

export function FollowUpEditor({
  pageContract,
  basePath,
  sopId,
  sopName,
  versionLabel,
  initial,
  initialView = "list",
}: {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: FollowUpRows;
  /** The view the page opened on (`?view=flow`), read on the server so SSR and client agree. */
  initialView?: "list" | "flow";
}) {
  const pc = pageContract;
  const router = useRouter();
  const taskTypes = optionGroup(pc, "sop_task_types");
  const answerKinds = useMemo(() => Object.fromEntries(optionGroup(pc, "sop_task_type_answer_kinds").map((o) => [o.key, o.label])), [pc]);
  const scheduleKinds = optionGroup(pc, "sop_schedule_kinds");
  // WHO DOES A STEP (SALES SOP, 2026-09-19): the designation catalog, compiled by the bootstrap;
  // an empty group (an older contract) simply hides the select.
  const owners = optionGroup(pc, "sop_step_owners");
  const conditions = optionGroup(pc, "sop_step_conditions");
  // A SALE step's condition is its own backend group (2026-09-25): "only when the sale has
  // animals". The herd conditions mean nothing on a sale and are never offered there.
  const saleConditions = optionGroup(pc, "sop_step_conditions_sales");
  const sections = optionGroup(pc, "sop_step_sections");

  const [rows, setRows] = useState<FollowUpRows>(initial);
  // Step keys the loaded version already carries never move (the engine and stamped rows match on
  // them); a new step's key follows its whole title (keyForTitle).
  const [savedKeys] = useState<Set<string>>(() => new Set(initial.tracks.flatMap((t) => t.steps.map((s) => s.key)).filter(Boolean)));
  const [notice, setNotice] = useState<FollowUpSaveResult | null>(null);
  const [pending, startTransition] = useTransition();
  const [openTrack, setOpenTrack] = useState<string>(initial.tracks[0]?.key ?? "");
  // The studio's two views (maintainer instruction 2026-09-18): LIST is the default, FLOW is the
  // chart. Both edit the same rows; the choice is per page, kept in the address bar so a link
  // opens the view it was copied from.
  const [view, setView] = useState<"list" | "flow">(initialView);
  const [selectedStep, setSelectedStep] = useState<string>("");
  const answerKindLabels = useMemo(
    () => Object.fromEntries(["yes_no", "select", "multiselect", "number", "text"].map((k) => [k, copy(pc, `studio.answer.${k}`)])),
    [pc],
  );
  function switchView(next: "list" | "flow") {
    setView(next);
    if (typeof window !== "undefined") {
      const url = new URL(window.location.href);
      if (next === "flow") url.searchParams.set("view", "flow");
      else url.searchParams.delete("view");
      window.history.replaceState(window.history.state, "", url.toString());
    }
  }
  function insertStep(trackKey: string, insert: FlowInsert) {
    const step = blankStep(taskTypes[0]?.key ?? "record_yes_no");
    if (insert.when) {
      step.whenStep = insert.when.whenStep;
      step.whenOp = insert.when.whenOp;
      step.whenValues = insert.when.whenValues;
    }
    setRows((prev) => ({
      tracks: prev.tracks.map((t) => {
        if (t.key !== trackKey) return t;
        const steps = [...t.steps];
        steps.splice(Math.min(Math.max(insert.index, 0), steps.length), 0, step);
        return { ...t, steps };
      }),
    }));
    setSelectedStep(step.id);
  }

  const problems = useMemo(() => followUpProblems(rows, answerKinds), [rows, answerKinds]);

  function updateStep(trackKey: string, id: string, patch: Partial<FollowUpStepRow>) {
    setRows((prev) => ({
      tracks: prev.tracks.map((t) => (t.key !== trackKey ? t : { ...t, steps: followStepKey(t.steps, id, patch) })),
    }));
  }
  function moveStep(trackKey: string, id: string, dir: -1 | 1) {
    setRows((prev) => ({
      tracks: prev.tracks.map((t) => {
        if (t.key !== trackKey) return t;
        const i = t.steps.findIndex((s) => s.id === id);
        const j = i + dir;
        if (i < 0 || j < 0 || j >= t.steps.length) return t;
        const steps = [...t.steps];
        [steps[i], steps[j]] = [steps[j], steps[i]];
        return { ...t, steps };
      }),
    }));
  }
  function removeStep(trackKey: string, id: string) {
    setRows((prev) => ({
      tracks: prev.tracks.map((t) => {
        if (t.key !== trackKey) return t;
        const removed = t.steps.find((s) => s.id === id);
        const steps = t.steps.filter((s) => s.id !== id).map((s) => ({
          ...s,
          requires: s.requires.filter((r) => r !== removed?.key),
          afterStep: s.afterStep === removed?.key ? "" : s.afterStep,
        }));
        return { ...t, steps };
      }),
    }));
  }
  function addStep(trackKey: string) {
    setRows((prev) => ({
      tracks: prev.tracks.map((t) => {
        if (t.key !== trackKey) return t;
        const step = blankStep(taskTypes[0]?.key ?? "record_yes_no");
        return { ...t, steps: [...t.steps, step] };
      }),
    }));
  }

  function submit(publish: boolean) {
    if (problems.length > 0) {
      setNotice({ ok: false, message: problems[0] });
      return;
    }
    setNotice(null);
    startTransition(async () => {
      const doc = emitFollowUp(rows);
      const res = publish ? await publishFollowUpVersion(sopId, doc) : await saveFollowUpVersion(sopId, doc);
      setNotice(res);
      if (res.ok && publish) {
        // Publish CLOSES the editor: the library reopens with a banner naming the version and
        // the card it belongs to (maintainer report 2026-09-15: a note above an unchanged
        // editor did not read as "your change is live").
        router.push(publishedHref(basePath, sopId, res.versionNumber));
        router.refresh();
      }
    });
  }

  return (
    <EditorPage>
      <EditorHeader
        crumbs={[copy(pc, "crumb"), pc.title, sopName]}
        title={copy(pc, "followup.title")}
        subtitle={copy(pc, "followup.subtitle")}
        version={versionLabel}
        notice={copy(pc, "followup.notice.capture_kept")}
        backHref={basePath}
        actions={
          <StudioViewToggle
            label={copy(pc, "studio.view.label")}
            listLabel={copy(pc, "studio.view.list")}
            flowLabel={copy(pc, "studio.view.flow")}
            value={view}
            onChange={switchView}
          />
        }
      />

      {/* One track at a time: the strip selects which track's steps are open below. */}
      {rows.tracks.length > 1 ? (
        <div>
          <TemplateTabs
            variant="pill"
            ariaLabel={copy(pc, "followup.track")}
            value={openTrack}
            items={rows.tracks.map((t) => ({ value: t.key, label: t.label || t.key, count: t.steps.length }))}
            onChange={(next) => setOpenTrack(next)}
          />
        </div>
      ) : null}

      {notice ? (
        <Alert severity={notice.ok ? "success" : "warning"} role="status">
          {notice.message}
          <ProblemList items={notice.report && !notice.report.valid ? notice.report.errors.map((e) => `${e.field} ${e.message}`) : []} max={50} />
        </Alert>
      ) : null}

      {rows.tracks.map((track) => {
        const open = openTrack === track.key;
        return (
          <Card key={track.key} sx={{ overflow: "visible" }}>
            <ButtonBase aria-expanded={open} onClick={() => setOpenTrack(open ? "" : track.key)} sx={{ display: "block", width: 1, borderRadius: "inherit" }}>
              <CardHeader
                component="span"
                avatar={<Label variant="soft">{copy(pc, "followup.track")}</Label>}
                title={track.label || track.key}
                subheader={`${track.steps.length} ${track.steps.length === 1 ? copy(pc, "label.step", copy(pc, "label.steps")) : copy(pc, "label.steps")}`}
                action={<Iconify icon={open ? EDITOR_ICON.up : EDITOR_ICON.down} sx={{ color: "text.secondary", mt: 0.5 }} />}
                slotProps={{ title: { variant: "subtitle1" }, subheader: { variant: "body2" }, action: { sx: { alignSelf: "center", m: 0 } } }}
                sx={{ display: "flex", width: 1, p: { xs: 2, sm: 3 }, textAlign: "left", "& .MuiCardHeader-content": { minWidth: 0 } }}
              />
            </ButtonBase>
            {open && view === "flow" ? (
              <FollowUpFlow
                pc={pc}
                track={track}
                answerKinds={answerKinds}
                answerKindLabels={answerKindLabels}
                taskTypes={taskTypes}
                owners={owners}
                selectedId={selectedStep}
                onSelect={setSelectedStep}
                onInsert={(insert) => insertStep(track.key, insert)}
                renderCard={(step, index) => (
                  <StepCard
                    key={step.id}
                    pc={pc}
                    index={index}
                    step={step}
                    earlier={track.steps.slice(0, index)}
                    taskTypes={taskTypes}
                    answerKinds={answerKinds}
                    scheduleKinds={scheduleKinds}
                    owners={owners}
                    conditions={track.module === "sales" ? saleConditions : conditions}
                    sections={sections}
                    onChange={(patch) => updateStep(track.key, step.id, patch)}
                    onMove={(dir) => moveStep(track.key, step.id, dir)}
                    onRemove={() => {
                      removeStep(track.key, step.id);
                      setSelectedStep("");
                    }}
                    takenKeys={new Set(track.steps.filter((s) => s.id !== step.id).map((s) => s.key))}
                    savedKeys={savedKeys}
                    legacyCondition={hasEngineCondition(track.module)}
                  />
                )}
              />
            ) : null}
            {open && view === "list" ? (
              // Spec §6: the panel swap fades out and rises in, and the card keeps its height
              // instead of jumping between tracks of different lengths.
              <TabPanel tabKey={track.key}>
                <Stack spacing={1.5} sx={{ px: { xs: 2, sm: 3 }, pb: { xs: 2, sm: 3 } }}>
                {track.steps.length === 0 ? <Hint>{copy(pc, "followup.empty")}</Hint> : null}
                {track.steps.map((step, index) => (
                  <StepCard
                    key={step.id}
                    pc={pc}
                    index={index}
                    step={step}
                    earlier={track.steps.slice(0, index)}
                    taskTypes={taskTypes}
                    answerKinds={answerKinds}
                    scheduleKinds={scheduleKinds}
                    owners={owners}
                    conditions={track.module === "sales" ? saleConditions : conditions}
                    sections={sections}
                    onChange={(patch) => updateStep(track.key, step.id, patch)}
                    onMove={(dir) => moveStep(track.key, step.id, dir)}
                    onRemove={() => removeStep(track.key, step.id)}
                    takenKeys={new Set(track.steps.filter((s) => s.id !== step.id).map((s) => s.key))}
                    savedKeys={savedKeys}
                    legacyCondition={hasEngineCondition(track.module)}
                  />
                ))}
                <AddButton label={copy(pc, "followup.step.add")} onClick={() => addStep(track.key)} />
                {/* Spec §7 totals block: what this track actually runs. */}
                <Box sx={TOTALS_SX}>
                  <span>
                    <b>{track.steps.length}</b> {track.steps.length === 1 ? copy(pc, "label.step", copy(pc, "label.steps")) : copy(pc, "label.steps")}
                  </span>
                  <span>
                    <b>{track.steps.filter((s) => s.scheduleKind === "series").length}</b>{" "}
                    {copy(pc, "followup.totals.sessions")}
                  </span>
                </Box>
                </Stack>
              </TabPanel>
            ) : null}
          </Card>
        );
      })}

      <StickyBar>
        <Box sx={{ minWidth: 0 }}>
          <Typography variant="body2" component="span">
            <strong>{rows.tracks.reduce((n, t) => n + t.steps.length, 0)}</strong> {copy(pc, "label.steps")}
          </Typography>
          <ProblemList items={problems} muted />
        </Box>
        <Spacer />
        <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap" }}>
          <Button color="primary" variant="outlined" loading={pending} disabled={pending} onClick={() => submit(false)}>
            {copy(pc, "followup.action.save_draft")}
          </Button>
          <Button variant="contained" color="primary" startIcon={<Iconify icon={EDITOR_ICON.check} />} disabled={pending || problems.length > 0} onClick={() => submit(true)}>
            {copy(pc, "followup.action.publish")}
          </Button>
        </Stack>
      </StickyBar>
    </EditorPage>
  );
}

// The one engine condition ("only when the kid pen could not be resolved") belongs to the herd
// operations; a general work instruction or the sale has no such context, so the select is
// not offered there (an "Include this step: Always" beside a sale step is noise).
function hasEngineCondition(module: string): boolean {
  return module !== "general";
}

function StepCard({
  pc,
  index,
  step,
  earlier,
  taskTypes,
  answerKinds,
  scheduleKinds,
  owners = [],
  conditions,
  sections,
  onChange,
  onMove,
  onRemove,
  takenKeys,
  savedKeys,
  legacyCondition = true,
}: {
  pc: AdminUiPageContract;
  index: number;
  step: FollowUpStepRow;
  earlier: FollowUpStepRow[];
  taskTypes: { key: string; label: string; title?: string }[];
  answerKinds: Record<string, string>;
  scheduleKinds: { key: string; label: string; title?: string }[];
  /** Designations a step can be for (`sop_step_owners`); empty hides the Done-by select. */
  owners?: { key: string; label: string }[];
  conditions: { key: string; label: string }[];
  sections: { key: string; label: string; title?: string }[];
  onChange: (patch: Partial<FollowUpStepRow>) => void;
  onMove: (dir: -1 | 1) => void;
  onRemove: () => void;
  takenKeys: Set<string>;
  /** Step keys the loaded version carries; a new step's key follows its title until saved. */
  savedKeys: Set<string>;
  /** Whether the track has an engine condition to offer (herd operations); a general track has none. */
  legacyCondition?: boolean;
}) {
  // Example event time for the next-sessions preview: the rounds depend on when the animal was born.
  const [exampleTime, setExampleTime] = useState("15:00");
  // An engine-bound step the loaded version carries keeps its key and type; a new one is still being named.
  const locked = ENGINE_BOUND_TASK_TYPES.has(step.taskType) && savedKeys.has(step.key);
  const answerKind = step.answer || answerKinds[step.taskType] || "none";
  const needsOptions = answerKind === "select" || answerKind === "multiselect";
  const typeDescription = taskTypes.find((t) => t.key === step.taskType)?.title;

  return (
    <QuestionShell
      testId={`followup-step-${step.key}`}
      head={
        <>
          <NumBadge>{index + 1}</NumBadge>
          <InlineSelect
            label={copy(pc, "followup.step.type")}
            value={step.taskType}
            disabled={locked}
            minWidth={190}
            options={taskTypes.map((t) => ({ value: t.key, label: t.label }))}
            onChange={(next) => onChange({ taskType: next, answer: "" })}
          />
          {locked ? <LockedNote title={copy(pc, "followup.notice.locked_key")}>{step.key}</LockedNote> : null}
          <Spacer />
          <MoveActions
            upLabel={copy(pc, "followup.step.move_up")}
            downLabel={copy(pc, "followup.step.move_down")}
            removeLabel={copy(pc, "followup.step.remove")}
            first={false}
            last={false}
            removeDisabled={locked}
            onUp={() => onMove(-1)}
            onDown={() => onMove(1)}
            onRemove={onRemove}
          />
        </>
      }
    >
      {typeDescription ? <Hint caption>{typeDescription}</Hint> : null}

      <MuiTextField
        label={copy(pc, "followup.step.title")}
        fullWidth
        size="small"
        value={step.title}
        placeholder={step.titlePattern || ""}
        slotProps={{ inputLabel: { shrink: true } }}
        onChange={(e) => {
          const title = e.target.value;
          onChange(locked ? { title } : { title, key: keyForTitle(title, step.key, savedKeys, takenKeys) });
        }}
        onBlur={() => {
          if (!locked && !step.key && step.title) onChange({ key: slugKey(step.title, takenKeys) });
        }}
      />
      <MuiTextField label={copy(pc, "followup.step.detail")} fullWidth multiline minRows={2} value={step.detail} onChange={(e) => onChange({ detail: e.target.value })} />

      {needsOptions ? (
        <ConfigBox title={copy(pc, "followup.step.options")}>
          {step.options.map((opt, i) => (
            <Stack key={i} direction="row" spacing={1} sx={{ alignItems: "center" }}>
              <MuiTextField fullWidth size="small" value={opt} onChange={(e) => onChange({ options: step.options.map((o, j) => (j === i ? e.target.value : o)) })} />
              <IconAction icon={EDITOR_ICON.remove} danger label={copy(pc, "followup.step.remove")} onClick={() => onChange({ options: step.options.filter((_, j) => j !== i) })} />
            </Stack>
          ))}
          <AddButton label={copy(pc, "followup.step.add_option")} onClick={() => onChange({ options: [...step.options, ""] })} />
        </ConfigBox>
      ) : null}

      <ConfigBox>
        <FieldGrid>
          <MuiTextField label={copy(pc, "followup.step.proof_videos")} size="small" type="number" slotProps={{ htmlInput: { min: 0, max: 10 } }} value={step.proofVideos} onChange={(e) => onChange({ proofVideos: Math.max(0, Number(e.target.value) || 0) })} />
          <MuiTextField label={copy(pc, "followup.step.proof_photos")} size="small" type="number" slotProps={{ htmlInput: { min: 0, max: 10 } }} value={step.proofPhotos} onChange={(e) => onChange({ proofPhotos: Math.max(0, Number(e.target.value) || 0) })} />
        </FieldGrid>
      </ConfigBox>

      {owners.length > 0 ? (
        <ConfigBox>
          <InlineSelect
            label={copy(pc, "followup.step.owner")}
            value={step.owner}
            minWidth={220}
            options={[{ value: "", label: copy(pc, "followup.step.owner_any") }, ...owners.map((o) => ({ value: o.key, label: o.label }))]}
            onChange={(next) => onChange({ owner: next })}
          />
          <Box component="span" sx={visuallyHidden} data-testid="step-owner">
            {copy(pc, "followup.step.owner_hint")}
          </Box>
        </ConfigBox>
      ) : null}

      <ConfigBox>
        <FieldGrid>
        <InlineSelect
          label={copy(pc, "followup.step.schedule")}
          value={step.scheduleKind}
          minWidth={180}
          options={scheduleKinds.map((k) => ({ value: k.key, label: k.label }))}
          onChange={(next) => onChange({ scheduleKind: next as ScheduleKind })}
        />
        {step.scheduleKind === "after_event" ? (
          <MuiTextField label={copy(pc, "followup.step.offset_minutes")} size="small" type="number" slotProps={{ htmlInput: { min: 0 } }} value={step.offsetMinutes} onChange={(e) => onChange({ offsetMinutes: Number(e.target.value) || 0 })} />
        ) : null}
        {step.scheduleKind === "at_fixed_time" ? (
          <>
            <MuiTextField label={copy(pc, "followup.step.day_offset")} size="small" type="number" slotProps={{ htmlInput: { min: 0 } }} value={step.dayOffset} onChange={(e) => onChange({ dayOffset: Number(e.target.value) || 0 })} />
            <MuiTextField label={copy(pc, "followup.step.time")} size="small" value={step.time} placeholder="07:00" onChange={(e) => onChange({ time: e.target.value })} />
          </>
        ) : null}
        {step.scheduleKind === "series" ? (
          <>
            <InlineSelect
              label={copy(pc, "followup.step.basis")}
              value={step.basis}
              minWidth={180}
              options={[
                { value: "fixed_times", label: copy(pc, "followup.basis.fixed_times") },
                { value: "next_sessions", label: copy(pc, "followup.basis.next_sessions") },
                { value: "from_event", label: copy(pc, "followup.basis.from_event") },
              ]}
              onChange={(next) => onChange({ basis: next as SeriesBasis })}
            />
            {step.basis === "from_event" ? (
              <>
                <MuiTextField label={copy(pc, "followup.step.interval_minutes")} size="small" type="number" slotProps={{ htmlInput: { min: 1 } }} value={step.intervalMinutes} onChange={(e) => onChange({ intervalMinutes: Math.max(1, Number(e.target.value) || 1) })} />
                <MuiTextField label={copy(pc, "followup.step.count")} size="small" type="number" slotProps={{ htmlInput: { min: 1, max: 100 } }} value={step.count} onChange={(e) => onChange({ count: Math.max(1, Number(e.target.value) || 1) })} />
              </>
            ) : (
              <>
                <MuiTextField label={copy(pc, "followup.step.times")} fullWidth size="small" value={step.times} placeholder="07:00, 11:00, 15:00" slotProps={{ inputLabel: { shrink: true } }} onChange={(e) => onChange({ times: e.target.value })} />
                {step.basis === "next_sessions" ? (
                  <MuiTextField label={copy(pc, "followup.step.count")} size="small" type="number" slotProps={{ htmlInput: { min: 1, max: 100 } }} value={step.count} onChange={(e) => onChange({ count: Math.max(1, Number(e.target.value) || 1) })} />
                ) : (
                  <MuiTextField label={copy(pc, "followup.step.days")} size="small" type="number" slotProps={{ htmlInput: { min: 1 } }} value={step.days} onChange={(e) => onChange({ days: Math.max(1, Number(e.target.value) || 1) })} />
                )}
                <MuiTextField label={copy(pc, "followup.step.pre_notify")} size="small" type="number" slotProps={{ htmlInput: { min: 0 } }} value={step.preNotifyMinutes} onChange={(e) => onChange({ preNotifyMinutes: Number(e.target.value) || 0 })} />
              </>
            )}
          </>
        ) : null}
        {step.scheduleKind === "after_step" ? (
          <>
            <InlineSelect
              label={copy(pc, "followup.step.after_step")}
              value={step.afterStep}
              minWidth={180}
              options={[{ value: "", label: "—" }, ...earlier.map((s) => ({ value: s.key, label: s.title || s.key }))]}
              onChange={(next) => onChange({ afterStep: next })}
            />
            <MuiTextField label={copy(pc, "followup.step.offset_minutes")} size="small" type="number" slotProps={{ htmlInput: { min: 1 } }} value={step.offsetMinutes} onChange={(e) => onChange({ offsetMinutes: Number(e.target.value) || 0 })} />
          </>
        ) : null}
        </FieldGrid>
        {step.scheduleKind === "series" ? (
          <Box sx={{ typography: "caption", color: "text.secondary" }}>
            {step.basis === "next_sessions" ? (
              <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: "center", flexWrap: "wrap" }}>
                {followUpCopy(pc)("followup.preview.series_next_sessions", { n: step.count })}
                <MuiTextField size="small" value={exampleTime} placeholder="15:00" slotProps={{ htmlInput: { "aria-label": copy(pc, "followup.preview.example_time") } }} sx={{ width: 120 }} onChange={(e) => setExampleTime(e.target.value)} />
              </Stack>
            ) : (
              followUpCopy(pc)(step.basis === "from_event" ? "followup.preview.series_from_event" : "followup.preview.series", { n: expandSeriesRows(step).length })
            )}
            <Box component="ul" sx={{ m: 0, mt: 0.5, pl: 2, columns: { sm: 2 }, columnGap: 3 }}>
              {expandSeriesRows(step, exampleTime).map((r) => (
                <Box component="li" key={`${r.dayOffset}-${r.time}-${r.afterMinutes ?? ""}`} sx={{ breakInside: "avoid" }}>
                  {r.title} — {roundWhen(pc, r)}
                </Box>
              ))}
            </Box>
          </Box>
        ) : null}
      </ConfigBox>

      <ConfigBox>
        <FieldGrid>
          <InlineSelect
            label={copy(pc, "followup.step.section")}
            value={step.section}
            minWidth={170}
            options={sections.map((s) => ({ value: s.key, label: s.label, title: s.title }))}
            onChange={(next) => onChange({ section: next })}
          />
          {/* The legacy engine condition (kid pen unresolved) and an answer-driven branch are two
              gates; rendering both selects on one step read as a contradiction (PR 308 review).
              The legacy select is offered only where it means something: a track that has such a
              condition and a step not already on a branch. */}
          {legacyCondition && !step.whenStep ? (
            <InlineSelect
              label={copy(pc, "followup.step.condition")}
              value={step.when}
              minWidth={170}
              options={conditions.map((c) => ({ value: c.key, label: c.label }))}
              onChange={(next) => onChange({ when: next })}
            />
          ) : null}
        </FieldGrid>
        {step.when ? null : <BranchField pc={pc} step={step} earlier={earlier} answerKinds={answerKinds} onChange={onChange} />}
        <Stack>
          <CheckLine checked={step.hardTimeGate} onChange={(hardTimeGate) => onChange({ hardTimeGate })} label={copy(pc, "followup.step.hard_time_gate")} />
          <CheckLine checked={step.waitForAll} onChange={(waitForAll) => onChange({ waitForAll })} label={copy(pc, "followup.step.wait_for_all")} />
        </Stack>
        {earlier.length > 0 ? (
          <Stack>
            <Hint caption>{copy(pc, "followup.step.requires")}</Hint>
            {earlier.map((s) => (
              <CheckLine
                key={s.key}
                checked={step.requires.includes(s.key)}
                onChange={(on) => onChange({ requires: on ? [...step.requires, s.key] : step.requires.filter((r) => r !== s.key) })}
                label={s.title || s.key}
              />
            ))}
          </Stack>
        ) : null}
      </ConfigBox>
    </QuestionShell>
  );
}
