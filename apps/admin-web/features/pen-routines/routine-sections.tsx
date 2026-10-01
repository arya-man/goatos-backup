"use client";

import { Plus, Trash2 } from "lucide-react";
import { useId, useMemo, useRef } from "react";

import { ThemedDatePicker } from "@/components/themed-date-picker";
import type { PenRoutineKeyLabel } from "@/lib/api/pen-routines-server";
import {
  cleanQuestionId,
  LIMITS,
  slugQuestionId,
  type CadenceDraft,
  type CadenceKind,
  type EvidenceDraft,
  type QuestionDraft,
} from "./routine-form-model";

/**
 * The SCHEDULE and WHAT-TO-RECORD sections of a pen routine, shared by the /routines drawer and the
 * "Task with its own phone tab" SOP editor (docs/decisions/simple-task-phone-tabs.md). Both author
 * the same pen-routine cadence and evidence, so both edit them through these components rather
 * than two copies that would drift.
 *
 * Every visible word is resolved by the caller through `words(key, fallbackKey)`: the keys are the
 * /routines page-contract keys (`field.cadence`, `action.add_question`, ...), and each host decides
 * which contract answers them. Nothing here composes a sentence of its own.
 */
export type RoutineWords = (key: string, fallbackKey?: string) => string;

/** The closed vocabularies these sections offer, as the backend catalog serves them. */
export type RoutineVocabulary = {
  cadence_kinds?: PenRoutineKeyLabel[];
  work_kinds?: PenRoutineKeyLabel[];
  question_kinds?: PenRoutineKeyLabel[];
  question_proof_kinds?: PenRoutineKeyLabel[];
  question_proof_counts?: PenRoutineKeyLabel[];
  presence_kinds?: PenRoutineKeyLabel[];
};

export function toggle(list: string[], value: string): string[] {
  return list.includes(value) ? list.filter((item) => item !== value) : [...list, value];
}

export function toggleNumber(list: number[], value: number): number[] {
  return list.includes(value) ? list.filter((item) => item !== value) : [...list, value].sort((a, b) => a - b);
}

/** Mon..Sun in the reader's short form; a weekday is not a date and keeps its own shape. */
function weekdayLabels(): string[] {
  const monday = Date.UTC(2024, 0, 1);
  return Array.from({ length: 7 }, (_, index) => new Intl.DateTimeFormat("en-GB", { weekday: "short", timeZone: "UTC" }).format(new Date(monday + index * 86400000)));
}

/**
 * The push-time choices: every quarter hour of the day as HH:MM (a time on its own keeps its own
 * shape -- it is not a date). A stored value off that grid is kept as its own option, so opening an
 * old routine never silently moves its push.
 */
function timeOptions(current: string): string[] {
  const grid = Array.from({ length: 96 }, (_, index) => `${String(Math.floor(index / 4)).padStart(2, "0")}:${String((index % 4) * 15).padStart(2, "0")}`);
  const trimmed = current.slice(0, 5);
  if (trimmed && !grid.includes(trimmed)) grid.push(trimmed);
  return grid.sort();
}

/** One numbered step of the form: the same heading everywhere, the step's fields beneath. */
export function Step({ index, title, hint, children, aside }: { index: number; title: string; hint?: string; children: React.ReactNode; aside?: React.ReactNode }) {
  const headingId = useId();
  return (
    <section className="prt-step" aria-labelledby={headingId}>
      <header className="prt-step-hd">
        <span className="prt-step-no" aria-hidden="true">
          {index}
        </span>
        <h4 id={headingId}>{title}</h4>
        {aside ? <span className="prt-step-aside">{aside}</span> : null}
      </header>
      {hint ? <p className="prt-hint">{hint}</p> : null}
      {children}
    </section>
  );
}

/** A choice between a closed vocabulary's options, drawn as tiles; each tile is a real radio. */
export function ChoiceTiles({
  name,
  options,
  value,
  onChange,
  isDisabled,
}: {
  name: string;
  options: PenRoutineKeyLabel[];
  value: string;
  onChange: (key: string) => void;
  isDisabled?: (key: string) => boolean;
}) {
  return (
    <div className="prt-tiles" role="radiogroup">
      {options.map((option) => {
        const off = isDisabled?.(option.key) ?? false;
        return (
          <label key={option.key} className={["prt-tile", value === option.key ? "on" : "", off ? "off" : ""].filter(Boolean).join(" ")}>
            <input type="radio" name={name} value={option.key} checked={value === option.key} disabled={off} onChange={() => onChange(option.key)} />
            <span>{option.label}</span>
          </label>
        );
      })}
    </div>
  );
}

/**
 * When: which business dates raise a task, from which day, and when the push goes out. The inputs
 * keep their `name`s so the /routines drawer can post them as a form; the SOP editor reads `value`.
 */
export function CadenceFields({
  words,
  vocabulary,
  value,
  onChange,
  onChooseCadence,
  disableAfterWork,
  idPrefix = "pr",
}: {
  words: RoutineWords;
  vocabulary: RoutineVocabulary | null;
  value: CadenceDraft;
  onChange: (patch: Partial<CadenceDraft>) => void;
  /** Switching cadence; defaults to dropping a typed offset back to the backend default. */
  onChooseCadence?: (kind: CadenceKind) => void;
  /** Work happens IN a pen, so a whole-park task cannot follow it. */
  disableAfterWork: boolean;
  idPrefix?: string;
}) {
  const field = (key: string) => words(`field.${key}`);
  const weekdays = useMemo(() => weekdayLabels(), []);
  const times = useMemo(() => timeOptions(value.notifyTime), [value.notifyTime]);
  const choose = onChooseCadence ?? ((cadenceKind: CadenceKind) => onChange({ cadenceKind, dueOffsetDays: "" }));
  return (
    <>
      <ChoiceTiles
        name="cadence_kind"
        options={vocabulary?.cadence_kinds ?? []}
        value={value.cadenceKind}
        onChange={(key) => choose(key as CadenceKind)}
        isDisabled={(key) => key === "after_work" && disableAfterWork}
      />
      {value.cadenceKind === "weekly" ? (
        <div className="fld">
          <span className="prt-sub">{field("weekdays")}</span>
          <div className="prt-pills" role="group" aria-label={field("weekdays")}>
            {weekdays.map((name, index) => {
              const day = index + 1;
              const on = value.weekdays.includes(day);
              return (
                <label key={day} className={on ? "prt-pill on" : "prt-pill"}>
                  <input type="checkbox" checked={on} onChange={() => onChange({ weekdays: toggleNumber(value.weekdays, day) })} />
                  <span>{name}</span>
                </label>
              );
            })}
          </div>
        </div>
      ) : null}
      {value.cadenceKind === "monthly" ? (
        <div className="fld">
          <span className="prt-sub">{field("month_days")}</span>
          <div className="prt-pills prt-month" role="group" aria-label={field("month_days")}>
            {Array.from({ length: 31 }, (_, index) => index + 1).map((day) => {
              const on = value.monthDays.includes(day);
              return (
                <label key={day} className={on ? "prt-pill on" : "prt-pill"}>
                  <input type="checkbox" checked={on} onChange={() => onChange({ monthDays: toggleNumber(value.monthDays, day) })} />
                  <span>{day}</span>
                </label>
              );
            })}
          </div>
        </div>
      ) : null}
      {value.cadenceKind === "every_n_days" ? (
        <div className="fld prt-narrow">
          <label htmlFor={`${idPrefix}-interval`}>{field("interval_days")}</label>
          <input
            id={`${idPrefix}-interval`}
            name="interval_days"
            type="number"
            inputMode="numeric"
            required
            min={LIMITS.intervalMin}
            max={LIMITS.intervalMax}
            value={value.intervalDays}
            onChange={(e) => onChange({ intervalDays: e.target.value })}
          />
        </div>
      ) : null}
      {value.cadenceKind === "after_work" ? (
        <>
          <div className="fld">
            <span className="prt-sub">{field("after_work_kinds")}</span>
            <div className="prt-pills" role="group" aria-label={field("after_work_kinds")}>
              {(vocabulary?.work_kinds ?? []).map((option) => {
                const on = value.afterWorkKinds.includes(option.key);
                return (
                  <label key={option.key} className={on ? "prt-pill on" : "prt-pill"}>
                    <input type="checkbox" checked={on} onChange={() => onChange({ afterWorkKinds: toggle(value.afterWorkKinds, option.key) })} />
                    <span>{option.label}</span>
                  </label>
                );
              })}
            </div>
          </div>
          <div className="fld prt-narrow">
            <label htmlFor={`${idPrefix}-due-offset`}>{field("due_offset_days")}</label>
            <input
              id={`${idPrefix}-due-offset`}
              name="due_offset_days"
              type="number"
              inputMode="numeric"
              min={LIMITS.dueOffsetMin}
              max={LIMITS.dueOffsetMax}
              value={value.dueOffsetDays}
              onChange={(e) => onChange({ dueOffsetDays: e.target.value })}
            />
          </div>
        </>
      ) : (
        // A calendar cadence keeps a stored offset untouched; a blank one lets the backend default apply.
        <input type="hidden" name="due_offset_days" value={value.dueOffsetDays} />
      )}
      <div className="prt-row2">
        <div className="fld">
          <span className="prt-sub">{field("start_date")}</span>
          <ThemedDatePicker
            name="start_date"
            label={field("start_date")}
            value={value.startDate}
            onChange={(key) => onChange({ startDate: key })}
            previousMonthLabel={words("date.previous_month", "action.previous")}
            nextMonthLabel={words("date.next_month", "action.next")}
            invalidDateText={words("date.invalid", "action.error_form")}
          />
        </div>
        <div className="fld">
          <label htmlFor={`${idPrefix}-notify-time`}>{field("notify_time")}</label>
          <select id={`${idPrefix}-notify-time`} name="notify_time" value={value.notifyTime.slice(0, 5)} onChange={(e) => onChange({ notifyTime: e.target.value })}>
            {times.map((time) => (
              <option key={time} value={time}>
                {time}
              </option>
            ))}
          </select>
        </div>
      </div>
    </>
  );
}

/** What to record: the questions (each with its own optional proof), the task-wide capture counts, the pen check-in. */
export function EvidenceFields({
  words,
  vocabulary,
  value,
  onChange,
  idPrefix = "pr",
}: {
  words: RoutineWords;
  vocabulary: RoutineVocabulary | null;
  value: EvidenceDraft;
  onChange: (patch: Partial<EvidenceDraft>) => void;
  idPrefix?: string;
}) {
  const field = (key: string) => words(`field.${key}`);
  const nextQuestionKey = useRef(1000);
  const updateQuestion = (key: number, patch: Partial<QuestionDraft>) => onChange({ questions: value.questions.map((q) => (q.key === key ? { ...q, ...patch } : q)) });
  const addQuestion = () => {
    if (value.questions.length >= LIMITS.questionsMax) return;
    const key = nextQuestionKey.current++;
    onChange({ questions: [...value.questions, { key, id: "", kind: "yes_no", title: "", required: true, idTouched: false }] });
  };
  const removeQuestion = (key: number) => onChange({ questions: value.questions.filter((q) => q.key !== key) });
  // Per-question proof is offered only when there is a choice to make; while the catalog serves just
  // "no proof" (the phone rollout gate), the select would be a dead control with one entry.
  const proofKinds = vocabulary?.question_proof_kinds ?? [];
  const proofChoosable = proofKinds.length > 1;

  const countField = (id: string, current: number, onValue: (next: number) => void) => (
    <input id={id} type="number" inputMode="numeric" min={0} max={LIMITS.proofMax} value={current} onChange={(e) => onValue(Number.parseInt(e.target.value, 10) || 0)} />
  );

  return (
    <>
      <span className="prt-sub">{field("questions")}</span>
      <div className="prt-questions">
        {value.questions.map((question, index) => (
          <div key={question.key} className="prt-question">
            <div className="prt-question-hd">
              <span className="prt-step-no sm" aria-hidden="true">
                {index + 1}
              </span>
              <button
                type="button"
                className="iconbtn"
                aria-label={`${words("action.remove_question", "action.close")} ${index + 1}`}
                title={words("action.remove_question", "action.close")}
                onClick={() => removeQuestion(question.key)}
              >
                <Trash2 className="ic" aria-hidden="true" />
              </button>
            </div>
            <div className="fld">
              <label htmlFor={`${idPrefix}-q-title-${question.key}`}>{words("field.question_title", "field.name")}</label>
              <input
                id={`${idPrefix}-q-title-${question.key}`}
                value={question.title}
                required
                onChange={(e) => updateQuestion(question.key, { title: e.target.value, id: question.idTouched ? question.id : slugQuestionId(e.target.value) })}
              />
            </div>
            <div className="prt-row2">
              <div className="fld">
                <label htmlFor={`${idPrefix}-q-kind-${question.key}`}>{words("field.question_kind", "field.questions")}</label>
                <select id={`${idPrefix}-q-kind-${question.key}`} value={question.kind} onChange={(e) => updateQuestion(question.key, { kind: e.target.value as QuestionDraft["kind"] })}>
                  {(vocabulary?.question_kinds ?? []).map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor={`${idPrefix}-q-hint-${question.key}`}>{words("field.question_hint", "field.instruction")}</label>
                <input id={`${idPrefix}-q-hint-${question.key}`} value={question.hint ?? ""} onChange={(e) => updateQuestion(question.key, { hint: e.target.value })} />
              </div>
            </div>
            {question.kind === "number" ? (
              <div className="prt-row3">
                <div className="fld">
                  <label htmlFor={`${idPrefix}-q-min-${question.key}`}>{field("min")}</label>
                  <input id={`${idPrefix}-q-min-${question.key}`} type="number" value={question.min ?? ""} onChange={(e) => updateQuestion(question.key, { min: e.target.value === "" ? null : Number(e.target.value) })} />
                </div>
                <div className="fld">
                  <label htmlFor={`${idPrefix}-q-max-${question.key}`}>{field("max")}</label>
                  <input id={`${idPrefix}-q-max-${question.key}`} type="number" value={question.max ?? ""} onChange={(e) => updateQuestion(question.key, { max: e.target.value === "" ? null : Number(e.target.value) })} />
                </div>
                <div className="fld">
                  <label htmlFor={`${idPrefix}-q-unit-${question.key}`}>{words("field.question_unit", "field.questions")}</label>
                  <input id={`${idPrefix}-q-unit-${question.key}`} value={question.unit ?? ""} onChange={(e) => updateQuestion(question.key, { unit: e.target.value })} />
                </div>
              </div>
            ) : null}
            {question.kind === "choice" || question.kind === "multi_choice" ? (
              <div className="fld">
                <span className="prt-sub">{words("field.question_options", "field.questions")}</span>
                {(question.options ?? []).map((option, optionIndex) => (
                  <div key={optionIndex} className="prt-option">
                    <input
                      value={option.label}
                      aria-label={words("field.option_label", "field.name")}
                      placeholder={words("field.option_label", "field.name")}
                      onChange={(e) => {
                        // The stored value follows the label until someone edits it by hand,
                        // the same way a question's key follows its title.
                        const options = (question.options ?? []).map((item, i) =>
                          i === optionIndex ? { label: e.target.value, value: item.value && item.value !== slugQuestionId(item.label) ? item.value : slugQuestionId(e.target.value) } : item,
                        );
                        updateQuestion(question.key, { options });
                      }}
                    />
                    <input
                      value={option.value}
                      className="prt-option-key"
                      aria-label={words("field.option_value", "field.name")}
                      placeholder={words("field.option_value", "field.name")}
                      onChange={(e) => {
                        const options = (question.options ?? []).map((item, i) => (i === optionIndex ? { ...item, value: cleanQuestionId(e.target.value) } : item));
                        updateQuestion(question.key, { options });
                      }}
                    />
                    <button
                      type="button"
                      className="iconbtn"
                      aria-label={words("action.remove_option", "action.close")}
                      title={words("action.remove_option", "action.close")}
                      onClick={() => updateQuestion(question.key, { options: (question.options ?? []).filter((_, i) => i !== optionIndex) })}
                    >
                      <Trash2 className="ic" aria-hidden="true" />
                    </button>
                  </div>
                ))}
                {(question.options ?? []).length < LIMITS.optionsMax ? (
                  <button type="button" className="btn sm" onClick={() => updateQuestion(question.key, { options: [...(question.options ?? []), { value: "", label: "" }] })}>
                    <Plus className="ic" aria-hidden="true" /> {words("action.add_option", "field.questions")}
                  </button>
                ) : null}
              </div>
            ) : null}
            <div className="prt-question-ft">
              <label className="prt-check">
                <input type="checkbox" checked={question.required} onChange={(e) => updateQuestion(question.key, { required: e.target.checked })} />
                <span>{words("field.question_required", "field.presence")}</span>
              </label>
              {/* Per-question proof (maintainer instruction 2026-09-18): what capture this
                  question needs to count as answered, and one or several. "none" is the
                  catalog's own key for no proof; it never travels. */}
              {proofChoosable ? (
                <span className="prt-proof">
                  <select
                    aria-label={words("field.question_proof", "field.photo")}
                    value={question.proof?.kind ?? "none"}
                    onChange={(e) => {
                      const kind = e.target.value;
                      updateQuestion(question.key, {
                        proof: kind === "none" ? undefined : { kind: kind as NonNullable<QuestionDraft["proof"]>["kind"], count: question.proof?.count ?? "single" },
                      });
                    }}
                  >
                    {proofKinds.map((option) => (
                      <option key={option.key} value={option.key}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                  {question.proof ? (
                    <select
                      aria-label={words("field.question_proof_count", "field.max")}
                      value={question.proof.count}
                      onChange={(e) => updateQuestion(question.key, { proof: { kind: question.proof!.kind, count: e.target.value as NonNullable<QuestionDraft["proof"]>["count"] } })}
                    >
                      {(vocabulary?.question_proof_counts ?? []).map((option) => (
                        <option key={option.key} value={option.key}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  ) : null}
                </span>
              ) : null}
            </div>
            {/* The key is what the phone's answer is stored under; it follows the title until
                edited, so most people never need to touch it. */}
            <details className="prt-key">
              <summary>{words("field.question_id", "field.name")}</summary>
              <input
                id={`${idPrefix}-q-id-${question.key}`}
                aria-label={words("field.question_id", "field.name")}
                value={question.id}
                required
                onChange={(e) => updateQuestion(question.key, { id: cleanQuestionId(e.target.value), idTouched: true })}
              />
            </details>
          </div>
        ))}
      </div>
      {value.questions.length < LIMITS.questionsMax ? (
        <button type="button" className="btn sm prt-add" onClick={addQuestion}>
          <Plus className="ic" aria-hidden="true" /> {words("action.add_question", "field.questions")}
        </button>
      ) : null}

      <div className="prt-row2 prt-media">
        {(["photo", "video"] as const).map((kind) => (
          <div key={kind} className="prt-media-box">
            <span className="prt-sub">{field(kind)}</span>
            <div className="prt-row2">
              <div className="fld">
                <label htmlFor={`${idPrefix}-${kind}-min`}>{field("min")}</label>
                {countField(`${idPrefix}-${kind}-min`, value[kind].min, (min) => onChange({ [kind]: { ...value[kind], min } } as Partial<EvidenceDraft>))}
              </div>
              <div className="fld">
                <label htmlFor={`${idPrefix}-${kind}-max`}>{field("max")}</label>
                {countField(`${idPrefix}-${kind}-max`, value[kind].max, (max) => onChange({ [kind]: { ...value[kind], max } } as Partial<EvidenceDraft>))}
              </div>
            </div>
          </div>
        ))}
      </div>
      <span className="prt-sub">{field("presence")}</span>
      <ChoiceTiles name="presence_kind" options={vocabulary?.presence_kinds ?? []} value={value.presence} onChange={(key) => onChange({ presence: key as EvidenceDraft["presence"] })} />
    </>
  );
}
