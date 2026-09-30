"use client";

import {
  conditionNeedsList,
  conditionNeedsValue,
  fieldConfigKind,
  type BuilderCondition,
  type BuilderOption,
  type BuilderStep,
  type ConditionOperator,
  type StepTypeValue,
} from "./sop-derive";
import { copy, optionGroup, optionLabel, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import MuiTextField from "@mui/material/TextField";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { InlineSelect } from "./editor-chrome";
import { AddButton, CheckLine, CondRow, ConfigBox, EDITOR_ICON, FieldGrid, Hint, IconAction, NumBadge, OptionRow, QuestionShell, Spacer } from "./editor-parts";

// Registered (offline) template icons per answer type, shown beside the type select.
const TYPE_ICON: Record<StepTypeValue, IconifyName> = {
  text: "ic:round-format-align-left",
  number: "carbon:chevron-sort",
  yesno: "eva:checkmark-circle-2-outline",
  select: "eva:radio-button-off-fill",
  multiselect: "eva:done-all-fill",
  goat_scan: "solar:tag-horizontal-bold-duotone",
  shed_picker: "mingcute:location-fill",
  vaccine_batch_picker: "solar:medical-kit-bold",
  medicine_picker: "solar:medical-kit-bold",
  photo_proof: "solar:camera-add-bold",
  video_proof: "solar:videocamera-record-bold",
};

export type PriorStep = { id: string; position: number; label: string };

export interface QuestionCardProps {
  pageContract: AdminUiPageContract;
  step: BuilderStep;
  index: number;
  total: number;
  priorSteps: PriorStep[];
  onPatch: (patch: Partial<BuilderStep>) => void;
  onChangeType: (type: StepTypeValue) => void;
  onRemove: () => void;
  onDuplicate: () => void;
  onMoveUp: () => void;
  onMoveDown: () => void;
  dragging: boolean;
  onDragStart: () => void;
  onDragEnter: () => void;
  onDragEnd: () => void;
}

export function QuestionCard(props: QuestionCardProps) {
  const { pageContract: pc, step, index, total, priorSteps, onPatch, onChangeType } = props;
  const kind = fieldConfigKind(step.type);
  const typeIcon = TYPE_ICON[step.type] ?? TYPE_ICON.text;
  const stepTypes = optionGroup(pc, "sop_step_types");
  const number = `${copy(pc, "builder.question_label")} ${index + 1}`;

  function patchOptions(next: BuilderOption[]) {
    onPatch({ options: next });
  }

  return (
    <QuestionShell
      testId="sop-question"
      dragging={props.dragging}
      dragProps={{
        draggable: true,
        onDragStart: props.onDragStart,
        onDragEnter: props.onDragEnter,
        onDragEnd: props.onDragEnd,
        onDragOver: (e: React.DragEvent) => e.preventDefault(),
      }}
      head={
        <>
          <Box component="span" aria-hidden="true" title={copy(pc, "builder.action.drag")} sx={{ display: "inline-flex", color: "text.disabled", cursor: "grab", flexShrink: 0 }}>
            <Iconify icon={EDITOR_ICON.drag} width={16} />
          </Box>
          <NumBadge>{index + 1}</NumBadge>
          <Iconify icon={typeIcon} width={16} aria-hidden="true" sx={{ color: "text.secondary", flexShrink: 0 }} />
          <InlineSelect
            label={`${number} · ${copy(pc, "builder.field.answer_type")}`}
            value={step.type}
            options={stepTypes.map((t) => ({ value: t.key, label: optionLabel(pc, "sop_step_types", t.key) }))}
            onChange={(next) => onChangeType(next as StepTypeValue)}
            minWidth={168}
          />
          <Spacer />
          <IconAction icon={EDITOR_ICON.up} title={copy(pc, "builder.action.move_up")} label={`${copy(pc, "builder.action.move_up")} · ${number}`} disabled={index === 0} onClick={props.onMoveUp} />
          <IconAction icon={EDITOR_ICON.down} title={copy(pc, "builder.action.move_down")} label={`${copy(pc, "builder.action.move_down")} · ${number}`} disabled={index === total - 1} onClick={props.onMoveDown} />
          <IconAction icon={EDITOR_ICON.copy} title={copy(pc, "builder.action.duplicate")} label={`${copy(pc, "builder.action.duplicate")} · ${number}`} onClick={props.onDuplicate} />
          <IconAction icon={EDITOR_ICON.trash} danger title={copy(pc, "builder.action.remove_question")} label={`${copy(pc, "builder.action.remove_question")} · ${number}`} onClick={props.onRemove} />
        </>
      }
      foot={
        <>
          <CheckLine checked={step.required} onChange={(required) => onPatch({ required })} label={copy(pc, "builder.required")} />
          <Spacer />
          <ConditionEditor pc={pc} step={step} index={index} priorSteps={priorSteps} onPatch={onPatch} />
        </>
      }
    >
      <MuiTextField
        label={copy(pc, "builder.field.question_text")}
        fullWidth
        size="small"
        value={step.label}
        placeholder={copy(pc, "builder.placeholder.question")}
        slotProps={{ inputLabel: { shrink: true }, htmlInput: { "aria-label": `${number} · ${copy(pc, "builder.field.question_text")}`, "data-testid": "sop-question-text" } }}
        onChange={(e) => onPatch({ label: e.target.value })}
      />
      <MuiTextField
        label={copy(pc, "builder.field.help_text")}
        fullWidth
        size="small"
        value={step.helpText}
        placeholder={copy(pc, "builder.placeholder.help_text")}
        slotProps={{ inputLabel: { shrink: true }, htmlInput: { "aria-label": `${number} · ${copy(pc, "builder.field.help_text")}` } }}
        onChange={(e) => onPatch({ helpText: e.target.value })}
      />

      {kind === "options" ? (
        <ConfigBox
          title={copy(pc, "builder.options.label")}
          action={<Hint caption>{step.type === "multiselect" ? copy(pc, "builder.options.multi_hint") : copy(pc, "builder.options.single_hint")}</Hint>}
        >
          {step.options.length === 0 ? <Alert severity="warning">{copy(pc, "builder.options.empty")}</Alert> : null}
          {step.options.map((option, oi) => (
            <OptionRow
              key={option.id}
              multi={step.type === "multiselect"}
              action={
                <IconAction
                  icon={EDITOR_ICON.remove}
                  danger
                  title={copy(pc, "builder.options.remove")}
                  label={`${copy(pc, "builder.options.remove")} ${oi + 1}`}
                  onClick={() => patchOptions(step.options.filter((o) => o.id !== option.id))}
                />
              }
            >
              <MuiTextField
                fullWidth
                size="small"
                value={option.label}
                placeholder={copy(pc, "builder.options.placeholder")}
                slotProps={{ htmlInput: { "aria-label": `${number} · ${copy(pc, "builder.options.label")} ${oi + 1}`, "data-testid": "sop-option" } }}
                onChange={(e) => patchOptions(step.options.map((o) => (o.id === option.id ? { ...o, label: e.target.value } : o)))}
              />
            </OptionRow>
          ))}
          <AddButton label={copy(pc, "builder.options.add")} onClick={() => patchOptions([...step.options, { id: `opt-${crypto.randomUUID()}`, label: "" }])} />
        </ConfigBox>
      ) : null}

      {kind === "number" ? (
        <ConfigBox>
          <FieldGrid>
            <MuiTextField
              label={copy(pc, "builder.number.min")}
              size="small"
              type="number"
              value={step.min}
              slotProps={{ htmlInput: { "aria-label": `${number} · ${copy(pc, "builder.number.min")}` } }}
              onChange={(e) => onPatch({ min: e.target.value })}
            />
            <MuiTextField
              label={copy(pc, "builder.number.max")}
              size="small"
              type="number"
              value={step.max}
              slotProps={{ htmlInput: { "aria-label": `${number} · ${copy(pc, "builder.number.max")}` } }}
              onChange={(e) => onPatch({ max: e.target.value })}
            />
            <MuiTextField
              label={copy(pc, "builder.number.unit")}
              size="small"
              value={step.unit}
              placeholder={copy(pc, "builder.number.unit_placeholder")}
              slotProps={{ inputLabel: { shrink: true }, htmlInput: { "aria-label": `${number} · ${copy(pc, "builder.number.unit")}` } }}
              onChange={(e) => onPatch({ unit: e.target.value })}
            />
          </FieldGrid>
          <Hint caption>{copy(pc, "builder.number.hint")}</Hint>
        </ConfigBox>
      ) : null}

      {kind === "text" ? (
        <ConfigBox>
          <MuiTextField
            label={copy(pc, "builder.text.placeholder_label")}
            fullWidth
            size="small"
            value={step.placeholder}
            placeholder={copy(pc, "builder.text.placeholder_hint")}
            slotProps={{ inputLabel: { shrink: true }, htmlInput: { "aria-label": `${number} · ${copy(pc, "builder.text.placeholder_label")}` } }}
            onChange={(e) => onPatch({ placeholder: e.target.value })}
          />
          <CheckLine checked={step.longText} onChange={(longText) => onPatch({ longText })} label={copy(pc, "builder.text.long")} />
        </ConfigBox>
      ) : null}

      {kind === "proof" ? <Alert severity="info">{copy(pc, "builder.proof.note")}</Alert> : null}
      {kind === "picker" ? <Alert severity="info">{copy(pc, "builder.picker.note")}</Alert> : null}
      {kind === "scan" ? (
        <ConfigBox
          title={copy(pc, "builder.scan.mode_label")}
          action={<Hint caption>{step.multiScan ? copy(pc, "builder.scan.multi_hint") : copy(pc, "builder.scan.single_hint")}</Hint>}
        >
          <InlineSelect
            label={`${number} · ${copy(pc, "builder.scan.mode_label")}`}
            value={step.multiScan ? "multi" : "single"}
            options={optionGroup(pc, "sop_scan_modes").map((m) => ({ value: m.key, label: optionLabel(pc, "sop_scan_modes", m.key) }))}
            onChange={(next) => onPatch({ multiScan: next === "multi" })}
            minWidth={200}
          />
          <Alert severity="info">{copy(pc, "builder.scan.note")}</Alert>
        </ConfigBox>
      ) : null}
      {kind === "boolean" ? <Alert severity="info">{copy(pc, "builder.boolean.note")}</Alert> : null}
    </QuestionShell>
  );
}

function ConditionEditor({
  pc,
  step,
  index,
  priorSteps,
  onPatch,
}: {
  pc: AdminUiPageContract;
  step: BuilderStep;
  index: number;
  priorSteps: PriorStep[];
  onPatch: (patch: Partial<BuilderStep>) => void;
}) {
  const operators = optionGroup(pc, "sop_condition_operators");

  // The first question always shows — a condition can only reference an EARLIER answer.
  if (index === 0 || priorSteps.length === 0) {
    return (
      <Box component="span" data-testid="sop-cond-note" sx={{ display: "contents" }}>
        <Hint caption sx={{ maxWidth: 340, textAlign: { sm: "right" } }}>{copy(pc, "builder.logic.first_note")}</Hint>
      </Box>
    );
  }

  const cond = step.visibleWhen;
  if (!cond) {
    return (
      <Button
        color="primary"
        variant="text"
        size="small"
        onClick={() => onPatch({ visibleWhen: { refId: priorSteps[priorSteps.length - 1].id, operator: "answered", value: "" } })}
      >
        {copy(pc, "builder.logic.add")}
      </Button>
    );
  }

  // Guard against a stale ref (e.g. the referenced question was reordered after this one) — fall back to
  // the nearest earlier question so the control never points at a missing/later question.
  const refValid = priorSteps.some((p) => p.id === cond.refId);
  const refId = refValid ? cond.refId : priorSteps[priorSteps.length - 1].id;
  const setCond = (patch: Partial<BuilderCondition>) => onPatch({ visibleWhen: { ...cond, refId, ...patch } });

  // data-testid hooks for scripts/sop-builder-e2e.mjs (the old .condrow / .condval classes are gone).
  return (
    <Box component="span" data-testid="sop-cond" sx={{ display: "contents" }}>
    <CondRow>
      {copy(pc, "builder.logic.show_when")}
      <InlineSelect
        label={`${copy(pc, "builder.logic.show_when")} · ${copy(pc, "builder.logic.answer_to")}`}
        value={refId}
        options={priorSteps.map((p) => ({ value: p.id, label: `${copy(pc, "builder.question_label")} ${p.position}` }))}
        onChange={(next) => setCond({ refId: next })}
        minWidth={120}
      />
      <InlineSelect
        label={copy(pc, "builder.logic.title")}
        value={cond.operator}
        options={operators.map((op) => ({ value: op.key, label: optionLabel(pc, "sop_condition_operators", op.key) }))}
        onChange={(next) => setCond({ operator: next as ConditionOperator })}
        minWidth={140}
      />
      {conditionNeedsValue(cond.operator) ? (
        <MuiTextField
          size="small"
          value={cond.value}
          placeholder={conditionNeedsList(cond.operator) ? copy(pc, "builder.logic.values_placeholder") : copy(pc, "builder.logic.value_placeholder")}
          slotProps={{ htmlInput: { "aria-label": copy(pc, "builder.logic.value_placeholder"), "data-testid": "sop-cond-value" } }}
          sx={{ minWidth: 120, maxWidth: 200 }}
          onChange={(e) => setCond({ value: e.target.value })}
        />
      ) : null}
      <IconAction icon={EDITOR_ICON.remove} danger label={copy(pc, "builder.logic.remove")} onClick={() => onPatch({ visibleWhen: null })} />
    </CondRow>
    </Box>
  );
}
