"use client";

import {
  Camera,
  ChevronDown,
  ChevronUp,
  Copy,
  FlaskConical,
  GripVertical,
  Hash,
  List,
  ListChecks,
  MapPin,
  Plus,
  ScanLine,
  Syringe,
  ToggleLeft,
  Trash2,
  Type as TypeIcon,
  Video,
  X,
} from "lucide-react";
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
import Button from "@mui/material/Button";
import { InlineSelect } from "./editor-chrome";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";

const TYPE_ICON: Record<StepTypeValue, React.ElementType> = {
  text: TypeIcon,
  number: Hash,
  yesno: ToggleLeft,
  select: List,
  multiselect: ListChecks,
  goat_scan: ScanLine,
  shed_picker: MapPin,
  vaccine_batch_picker: Syringe,
  medicine_picker: FlaskConical,
  photo_proof: Camera,
  video_proof: Video,
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
  const TypeIco = TYPE_ICON[step.type] ?? TypeIcon;
  const stepTypes = optionGroup(pc, "sop_step_types");
  const number = `${copy(pc, "builder.question_label")} ${index + 1}`;

  function patchOptions(next: BuilderOption[]) {
    onPatch({ options: next });
  }

  return (
    <div
      className={`qcard${props.dragging ? " dragging" : ""}`}
      draggable
      onDragStart={props.onDragStart}
      onDragEnter={props.onDragEnter}
      onDragEnd={props.onDragEnd}
      onDragOver={(e) => e.preventDefault()}
    >
      <div className="qhead">
        <span className="qgrip" title={copy(pc, "builder.action.drag")} aria-hidden="true">
          <GripVertical className="ic" />
        </span>
        <span className="qnum">{index + 1}</span>
        <span className="qtype">
          <TypeIco className="ic" style={{ width: 14 }} aria-hidden="true" />
          <InlineSelect
            label={`${number} · ${copy(pc, "builder.field.answer_type")}`}
            value={step.type}
            options={stepTypes.map((t) => ({ value: t.key, label: optionLabel(pc, "sop_step_types", t.key) }))}
            onChange={(next) => onChangeType(next as StepTypeValue)}
            minWidth={168}
          />
        </span>
        <span className="sp" style={{ flex: 1 }} />
        <button
          type="button"
          className="ia"
          title={copy(pc, "builder.action.move_up")}
          aria-label={`${copy(pc, "builder.action.move_up")} · ${number}`}
          disabled={index === 0}
          onClick={props.onMoveUp}
        >
          <ChevronUp className="ic" />
        </button>
        <button
          type="button"
          className="ia"
          title={copy(pc, "builder.action.move_down")}
          aria-label={`${copy(pc, "builder.action.move_down")} · ${number}`}
          disabled={index === total - 1}
          onClick={props.onMoveDown}
        >
          <ChevronDown className="ic" />
        </button>
        <button
          type="button"
          className="ia"
          title={copy(pc, "builder.action.duplicate")}
          aria-label={`${copy(pc, "builder.action.duplicate")} · ${number}`}
          onClick={props.onDuplicate}
        >
          <Copy className="ic" />
        </button>
        <button
          type="button"
          className="ia del"
          title={copy(pc, "builder.action.remove_question")}
          aria-label={`${copy(pc, "builder.action.remove_question")} · ${number}`}
          onClick={props.onRemove}
        >
          <Trash2 className="ic" />
        </button>
      </div>

      <div className="qbody">
        <input
          className="qtext"
          aria-label={`${number} · ${copy(pc, "builder.field.question_text")}`}
          value={step.label}
          placeholder={copy(pc, "builder.placeholder.question")}
          onChange={(e) => onPatch({ label: e.target.value })}
        />
        <input
          className="qhelp"
          aria-label={`${number} · ${copy(pc, "builder.field.help_text")}`}
          value={step.helpText}
          placeholder={copy(pc, "builder.placeholder.help_text")}
          onChange={(e) => onPatch({ helpText: e.target.value })}
        />

        {kind === "options" ? (
          <div className="qcfg">
            <div className="qcfg-head">
              <span className="qcfg-title">{copy(pc, "builder.options.label")}</span>
              <span className="muted small">
                {step.type === "multiselect" ? copy(pc, "builder.options.multi_hint") : copy(pc, "builder.options.single_hint")}
              </span>
            </div>
            {step.options.length === 0 ? <div className="note warn-note">{copy(pc, "builder.options.empty")}</div> : null}
            {step.options.map((option, oi) => (
              <div className="optrow" key={option.id}>
                <span className="optmark" aria-hidden="true">
                  {step.type === "multiselect" ? <span className="optbox" /> : <span className="optdot" />}
                </span>
                <input
                  aria-label={`${number} · ${copy(pc, "builder.options.label")} ${oi + 1}`}
                  value={option.label}
                  placeholder={copy(pc, "builder.options.placeholder")}
                  onChange={(e) => patchOptions(step.options.map((o) => (o.id === option.id ? { ...o, label: e.target.value } : o)))}
                />
                <button
                  type="button"
                  className="ia del"
                  title={copy(pc, "builder.options.remove")}
                  aria-label={`${copy(pc, "builder.options.remove")} ${oi + 1}`}
                  onClick={() => patchOptions(step.options.filter((o) => o.id !== option.id))}
                >
                  <X className="ic" />
                </button>
              </div>
            ))}
            <Button
              color="primary"
              variant="text"
              size="small"
              startIcon={<Plus size={14} />}
              onClick={() => patchOptions([...step.options, { id: `opt-${crypto.randomUUID()}`, label: "" }])}
            >
              {copy(pc, "builder.options.add")}
            </Button>
          </div>
        ) : null}

        {kind === "number" ? (
          <div className="qcfg">
            <div className="rowf">
              <label className="numfield">
                <span className="numlbl">{copy(pc, "builder.number.min")}</span>
                <input
                  type="number"
                  aria-label={`${number} · ${copy(pc, "builder.number.min")}`}
                  value={step.min}
                  onChange={(e) => onPatch({ min: e.target.value })}
                />
              </label>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "builder.number.max")}</span>
                <input
                  type="number"
                  aria-label={`${number} · ${copy(pc, "builder.number.max")}`}
                  value={step.max}
                  onChange={(e) => onPatch({ max: e.target.value })}
                />
              </label>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "builder.number.unit")}</span>
                <input
                  aria-label={`${number} · ${copy(pc, "builder.number.unit")}`}
                  value={step.unit}
                  placeholder={copy(pc, "builder.number.unit_placeholder")}
                  onChange={(e) => onPatch({ unit: e.target.value })}
                />
              </label>
            </div>
            <div className="muted small">{copy(pc, "builder.number.hint")}</div>
          </div>
        ) : null}

        {kind === "text" ? (
          <div className="qcfg">
            <label className="numfield" style={{ flex: 1 }}>
              <span className="numlbl">{copy(pc, "builder.text.placeholder_label")}</span>
              <input
                aria-label={`${number} · ${copy(pc, "builder.text.placeholder_label")}`}
                value={step.placeholder}
                placeholder={copy(pc, "builder.text.placeholder_hint")}
                onChange={(e) => onPatch({ placeholder: e.target.value })}
              />
            </label>
            <FormControlLabel className="chkline" control={<Checkbox checked={step.longText} onChange={(e) => onPatch({ longText: e.target.checked })} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{" "}
              {copy(pc, "builder.text.long")}</>} />
          </div>
        ) : null}

        {kind === "proof" ? <div className="note">{copy(pc, "builder.proof.note")}</div> : null}
        {kind === "picker" ? <div className="note">{copy(pc, "builder.picker.note")}</div> : null}
        {kind === "scan" ? (
          <div className="qcfg">
            <div className="qcfg-head">
              <span className="qcfg-title">{copy(pc, "builder.scan.mode_label")}</span>
              <span className="muted small">{step.multiScan ? copy(pc, "builder.scan.multi_hint") : copy(pc, "builder.scan.single_hint")}</span>
            </div>
            <InlineSelect
              label={`${number} · ${copy(pc, "builder.scan.mode_label")}`}
              value={step.multiScan ? "multi" : "single"}
              options={optionGroup(pc, "sop_scan_modes").map((m) => ({ value: m.key, label: optionLabel(pc, "sop_scan_modes", m.key) }))}
              onChange={(next) => onPatch({ multiScan: next === "multi" })}
              minWidth={200}
            />
            <div className="note">{copy(pc, "builder.scan.note")}</div>
          </div>
        ) : null}
        {kind === "boolean" ? <div className="note">{copy(pc, "builder.boolean.note")}</div> : null}
      </div>

      <div className="qfoot">
        <FormControlLabel className="chkline" control={<Checkbox checked={step.required} onChange={(e) => onPatch({ required: e.target.checked })} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={<>{" "}
          {copy(pc, "builder.required")}</>} />
        <span className="sp" style={{ flex: 1 }} />
        <ConditionEditor pc={pc} step={step} index={index} priorSteps={priorSteps} onPatch={onPatch} />
      </div>
    </div>
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
    return <span className="muted small cond-note">{copy(pc, "builder.logic.first_note")}</span>;
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

  return (
    <div className="condrow">
      <span className="muted small">{copy(pc, "builder.logic.show_when")}</span>
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
        <input
          className="condval"
          aria-label={copy(pc, "builder.logic.value_placeholder")}
          value={cond.value}
          placeholder={conditionNeedsList(cond.operator) ? copy(pc, "builder.logic.values_placeholder") : copy(pc, "builder.logic.value_placeholder")}
          onChange={(e) => setCond({ value: e.target.value })}
        />
      ) : null}
      <button
        type="button"
        className="ia del"
        title={copy(pc, "builder.logic.remove")}
        aria-label={copy(pc, "builder.logic.remove")}
        onClick={() => onPatch({ visibleWhen: null })}
      >
        <X className="ic" />
      </button>
    </div>
  );
}
