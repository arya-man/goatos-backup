"use client";

import { useMemo, useState, useTransition } from "react";
import Link from "@/components/no-prefetch-link";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, ChevronLeft, ChevronDown, ChevronUp, Lock, Plus, X } from "lucide-react";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  ENGINE_BOUND_TASK_TYPES,
  blankStep,
  emitFollowUp,
  expandSeriesRows,
  followUpProblems,
  slugKey,
  type FollowUpRows,
  type FollowUpStepRow,
  type ScheduleKind,
  type SeriesBasis,
} from "./followup-model";
import { publishFollowUpVersion, saveFollowUpVersion, type FollowUpSaveResult } from "./sop-actions";
import { followUpCopy, roundWhen } from "./followup-summary";

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

export function FollowUpEditor({
  pageContract,
  basePath,
  sopId,
  sopName,
  sopCode,
  versionLabel,
  initial,
}: {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: FollowUpRows;
}) {
  const pc = pageContract;
  const router = useRouter();
  const taskTypes = optionGroup(pc, "sop_task_types");
  const answerKinds = useMemo(() => Object.fromEntries(optionGroup(pc, "sop_task_type_answer_kinds").map((o) => [o.key, o.label])), [pc]);
  const scheduleKinds = optionGroup(pc, "sop_schedule_kinds");
  const conditions = optionGroup(pc, "sop_step_conditions");
  const sections = optionGroup(pc, "sop_step_sections");

  const [rows, setRows] = useState<FollowUpRows>(initial);
  const [notice, setNotice] = useState<FollowUpSaveResult | null>(null);
  const [pending, startTransition] = useTransition();
  const [openTrack, setOpenTrack] = useState<string>(initial.tracks[0]?.key ?? "");

  const problems = useMemo(() => followUpProblems(rows, answerKinds), [rows, answerKinds]);

  function updateStep(trackKey: string, id: string, patch: Partial<FollowUpStepRow>) {
    setRows((prev) => ({
      tracks: prev.tracks.map((t) => (t.key !== trackKey ? t : { ...t, steps: t.steps.map((s) => (s.id === id ? { ...s, ...patch } : s)) })),
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
        router.refresh();
      }
    });
  }

  return (
    <div className="screen on sop-followup" aria-label={copy(pc, "followup.title")}>
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pc, "crumb")} · {pc.title} · <b>{sopName}</b>
          </div>
          <h1>{copy(pc, "followup.title")}</h1>
          <div className="sub">{copy(pc, "followup.subtitle")}</div>
          <div className="sub muted small">
            <code>{sopCode}</code> · {versionLabel} · {copy(pc, "followup.notice.capture_kept")}
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link className="btn" href={basePath}>
          <ChevronLeft className="ic" /> {copy(pc, "builder.back")}
        </Link>
      </div>

      {notice ? (
        <div className={notice.ok ? "note" : "alert warn"} role="status" style={{ marginBottom: 12 }}>
          {notice.ok ? <Check className="ic" /> : <AlertTriangle className="ic" />} {notice.message}
          {notice.report && !notice.report.valid ? (
            <ul>
              {notice.report.errors.map((e, i) => (
                <li key={i}>
                  <code>{e.field}</code> {e.message}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}

      {rows.tracks.map((track) => {
        const open = openTrack === track.key;
        return (
          <div className="card followup-track" key={track.key}>
            <button type="button" className="followup-track-head" aria-expanded={open} onClick={() => setOpenTrack(open ? "" : track.key)}>
              <span className="qtype">{copy(pc, "followup.track")}</span>
              <strong>{track.label || track.key}</strong>
              <span className="muted small">
                {track.steps.length} {copy(pc, "label.steps")}
              </span>
              {open ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
            </button>
            {open ? (
              <div className="followup-steps">
                {track.steps.length === 0 ? <p className="muted">{copy(pc, "followup.empty")}</p> : null}
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
                    conditions={conditions}
                    sections={sections}
                    onChange={(patch) => updateStep(track.key, step.id, patch)}
                    onMove={(dir) => moveStep(track.key, step.id, dir)}
                    onRemove={() => removeStep(track.key, step.id)}
                    takenKeys={new Set(track.steps.filter((s) => s.id !== step.id).map((s) => s.key))}
                  />
                ))}
                <button type="button" className="btn sm ghost" onClick={() => addStep(track.key)}>
                  <Plus size={14} /> {copy(pc, "followup.step.add")}
                </button>
              </div>
            ) : null}
          </div>
        );
      })}

      <footer className="card followup-publish">
        <div>
          <strong>{rows.tracks.reduce((n, t) => n + t.steps.length, 0)}</strong> {copy(pc, "label.steps")}
          {problems.length > 0 ? (
            <ul className="muted small">
              {problems.slice(0, 5).map((p, i) => (
                <li key={i}>{p}</li>
              ))}
            </ul>
          ) : null}
        </div>
        <div className="followup-actions">
          <button type="button" className="btn" disabled={pending} onClick={() => submit(false)}>
            {copy(pc, "followup.action.save_draft")}
          </button>
          <button type="button" className="btn p" disabled={pending || problems.length > 0} onClick={() => submit(true)}>
            <Check className="ic" /> {copy(pc, "followup.action.publish")}
          </button>
        </div>
      </footer>
    </div>
  );
}

function StepCard({
  pc,
  index,
  step,
  earlier,
  taskTypes,
  answerKinds,
  scheduleKinds,
  conditions,
  sections,
  onChange,
  onMove,
  onRemove,
  takenKeys,
}: {
  pc: AdminUiPageContract;
  index: number;
  step: FollowUpStepRow;
  earlier: FollowUpStepRow[];
  taskTypes: { key: string; label: string; title?: string }[];
  answerKinds: Record<string, string>;
  scheduleKinds: { key: string; label: string; title?: string }[];
  conditions: { key: string; label: string }[];
  sections: { key: string; label: string; title?: string }[];
  onChange: (patch: Partial<FollowUpStepRow>) => void;
  onMove: (dir: -1 | 1) => void;
  onRemove: () => void;
  takenKeys: Set<string>;
}) {
  const locked = ENGINE_BOUND_TASK_TYPES.has(step.taskType) && step.key !== "";
  const answerKind = step.answer || answerKinds[step.taskType] || "none";
  const needsOptions = answerKind === "select" || answerKind === "multiselect";
  const typeDescription = taskTypes.find((t) => t.key === step.taskType)?.title;

  return (
    <div className="qcard followup-step" data-step-key={step.key}>
      <div className="qcfg-head">
        <span className="qnum">{index + 1}</span>
        <label className="qtype">
          {copy(pc, "followup.step.type")}
          <select value={step.taskType} disabled={locked} onChange={(e) => onChange({ taskType: e.target.value, answer: "" })}>
            {taskTypes.map((t) => (
              <option key={t.key} value={t.key}>
                {t.label}
              </option>
            ))}
          </select>
        </label>
        {locked ? (
          <span className="muted small" title={copy(pc, "followup.notice.locked_key")}>
            <Lock size={12} /> {step.key}
          </span>
        ) : null}
        <span className="qcfg-actions">
          <button type="button" className="ia" aria-label={copy(pc, "followup.step.move_up")} onClick={() => onMove(-1)}>
            <ChevronUp size={14} />
          </button>
          <button type="button" className="ia" aria-label={copy(pc, "followup.step.move_down")} onClick={() => onMove(1)}>
            <ChevronDown size={14} />
          </button>
          <button type="button" className="ia del" aria-label={copy(pc, "followup.step.remove")} disabled={locked} onClick={onRemove}>
            <X size={14} />
          </button>
        </span>
      </div>
      {typeDescription ? <p className="qhelp muted small">{typeDescription}</p> : null}

      <label className="qtext">
        {copy(pc, "followup.step.title")}
        <input
          value={step.title}
          placeholder={step.titlePattern || ""}
          onChange={(e) => {
            const title = e.target.value;
            onChange(locked || step.key ? { title } : { title, key: slugKey(title, takenKeys) });
          }}
          onBlur={() => {
            if (!locked && !step.key && step.title) onChange({ key: slugKey(step.title, takenKeys) });
          }}
        />
      </label>
      <label className="qhelp">
        {copy(pc, "followup.step.detail")}
        <textarea rows={2} value={step.detail} onChange={(e) => onChange({ detail: e.target.value })} />
      </label>

      {needsOptions ? (
        <div className="qcfg">
          <div className="qcfg-title">{copy(pc, "followup.step.options")}</div>
          {step.options.map((opt, i) => (
            <div className="rowf" key={i}>
              <input value={opt} onChange={(e) => onChange({ options: step.options.map((o, j) => (j === i ? e.target.value : o)) })} />
              <button type="button" className="ia del" aria-label={copy(pc, "followup.step.remove")} onClick={() => onChange({ options: step.options.filter((_, j) => j !== i) })}>
                <X size={14} />
              </button>
            </div>
          ))}
          <button type="button" className="btn sm ghost" onClick={() => onChange({ options: [...step.options, ""] })}>
            <Plus size={14} /> {copy(pc, "followup.step.add_option")}
          </button>
        </div>
      ) : null}

      <div className="qcfg followup-proof">
        <label className="numlbl">
          {copy(pc, "followup.step.proof_videos")}
          <input className="numfield" type="number" min={0} max={10} value={step.proofVideos} onChange={(e) => onChange({ proofVideos: Math.max(0, Number(e.target.value) || 0) })} />
        </label>
        <label className="numlbl">
          {copy(pc, "followup.step.proof_photos")}
          <input className="numfield" type="number" min={0} max={10} value={step.proofPhotos} onChange={(e) => onChange({ proofPhotos: Math.max(0, Number(e.target.value) || 0) })} />
        </label>
      </div>

      <div className="qcfg followup-schedule">
        <label>
          {copy(pc, "followup.step.schedule")}
          <select value={step.scheduleKind} onChange={(e) => onChange({ scheduleKind: e.target.value as ScheduleKind })}>
            {scheduleKinds.map((k) => (
              <option key={k.key} value={k.key} title={k.title}>
                {k.label}
              </option>
            ))}
          </select>
        </label>
        {step.scheduleKind === "after_event" ? (
          <label className="numlbl">
            {copy(pc, "followup.step.offset_minutes")}
            <input className="numfield" type="number" min={0} value={step.offsetMinutes} onChange={(e) => onChange({ offsetMinutes: Number(e.target.value) || 0 })} />
          </label>
        ) : null}
        {step.scheduleKind === "at_fixed_time" ? (
          <>
            <label className="numlbl">
              {copy(pc, "followup.step.day_offset")}
              <input className="numfield" type="number" min={0} value={step.dayOffset} onChange={(e) => onChange({ dayOffset: Number(e.target.value) || 0 })} />
            </label>
            <label className="numlbl">
              {copy(pc, "followup.step.time")}
              <input className="numfield" value={step.time} placeholder="07:00" onChange={(e) => onChange({ time: e.target.value })} />
            </label>
          </>
        ) : null}
        {step.scheduleKind === "series" ? (
          <>
            <label>
              {copy(pc, "followup.step.basis")}
              <select value={step.basis} onChange={(e) => onChange({ basis: e.target.value as SeriesBasis })}>
                <option value="fixed_times">{copy(pc, "followup.basis.fixed_times")}</option>
                <option value="from_event">{copy(pc, "followup.basis.from_event")}</option>
              </select>
            </label>
            {step.basis === "from_event" ? (
              <>
                <label className="numlbl">
                  {copy(pc, "followup.step.interval_minutes")}
                  <input className="numfield" type="number" min={1} value={step.intervalMinutes} onChange={(e) => onChange({ intervalMinutes: Math.max(1, Number(e.target.value) || 1) })} />
                </label>
                <label className="numlbl">
                  {copy(pc, "followup.step.count")}
                  <input className="numfield" type="number" min={1} max={100} value={step.count} onChange={(e) => onChange({ count: Math.max(1, Number(e.target.value) || 1) })} />
                </label>
              </>
            ) : (
              <>
                <label>
                  {copy(pc, "followup.step.times")}
                  <input value={step.times} placeholder="07:00, 11:00, 15:00" onChange={(e) => onChange({ times: e.target.value })} />
                </label>
                <label className="numlbl">
                  {copy(pc, "followup.step.days")}
                  <input className="numfield" type="number" min={1} value={step.days} onChange={(e) => onChange({ days: Math.max(1, Number(e.target.value) || 1) })} />
                </label>
                <label className="numlbl">
                  {copy(pc, "followup.step.pre_notify")}
                  <input className="numfield" type="number" min={0} value={step.preNotifyMinutes} onChange={(e) => onChange({ preNotifyMinutes: Number(e.target.value) || 0 })} />
                </label>
              </>
            )}
            <div className="followup-series-preview muted small">
              {followUpCopy(pc)(step.basis === "from_event" ? "followup.preview.series_from_event" : "followup.preview.series", { n: expandSeriesRows(step).length })}
              <ul>
                {expandSeriesRows(step).map((r) => (
                  <li key={`${r.dayOffset}-${r.time}-${r.afterMinutes ?? ""}`}>
                    {r.title} — {roundWhen(pc, r)}
                  </li>
                ))}
              </ul>
            </div>
          </>
        ) : null}
        {step.scheduleKind === "after_step" ? (
          <>
            <label>
              {copy(pc, "followup.step.after_step")}
              <select value={step.afterStep} onChange={(e) => onChange({ afterStep: e.target.value })}>
                <option value="">—</option>
                {earlier.map((s) => (
                  <option key={s.key} value={s.key}>
                    {s.title || s.key}
                  </option>
                ))}
              </select>
            </label>
            <label className="numlbl">
              {copy(pc, "followup.step.offset_minutes")}
              <input className="numfield" type="number" min={1} value={step.offsetMinutes} onChange={(e) => onChange({ offsetMinutes: Number(e.target.value) || 0 })} />
            </label>
          </>
        ) : null}
      </div>

      <div className="qcfg followup-gates">
        <label>
          {copy(pc, "followup.step.section")}
          <select value={step.section} onChange={(e) => onChange({ section: e.target.value })}>
            {sections.map((s) => (
              <option key={s.key} value={s.key} title={s.title}>
                {s.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          {copy(pc, "followup.step.condition")}
          <select value={step.when} onChange={(e) => onChange({ when: e.target.value })}>
            {conditions.map((c) => (
              <option key={c.key || "always"} value={c.key}>
                {c.label}
              </option>
            ))}
          </select>
        </label>
        <label className="chkline">
          <input type="checkbox" checked={step.hardTimeGate} onChange={(e) => onChange({ hardTimeGate: e.target.checked })} /> {copy(pc, "followup.step.hard_time_gate")}
        </label>
        <label className="chkline">
          <input type="checkbox" checked={step.waitForAll} onChange={(e) => onChange({ waitForAll: e.target.checked })} /> {copy(pc, "followup.step.wait_for_all")}
        </label>
        {earlier.length > 0 ? (
          <div className="followup-requires">
            <span className="muted small">{copy(pc, "followup.step.requires")}</span>
            {earlier.map((s) => (
              <label key={s.key} className="chkline">
                <input
                  type="checkbox"
                  checked={step.requires.includes(s.key)}
                  onChange={(e) => onChange({ requires: e.target.checked ? [...step.requires, s.key] : step.requires.filter((r) => r !== s.key) })}
                />{" "}
                {s.title || s.key}
              </label>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}
