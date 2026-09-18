"use client";

import { Plus, Trash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useActionState, useEffect, useMemo, useRef, useState } from "react";

import { currentHistoryEntryIsLocalOverlay, replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type {
  PenRoutineCatalog,
  PenRoutineCatalogPen,
  PenRoutineKeyLabel,
  PenRoutinePark,
  PenRoutineRow,
} from "@/lib/api/pen-routines-server";
import { createRoutineAction, setRoutineStatusAction, updateRoutineAction, type PenRoutineActionState } from "./pen-routine-actions";
import {
  cleanQuestionId,
  FORM_JSON_FIELDS,
  LIMITS,
  slugQuestionId,
  type CadenceKind,
  type ScopeKind,
  type PenRoutineEvidenceBody,
  type PenRoutineQuestionBody,
} from "./routine-form-model";

/**
 * The routine drawer body: one form that creates a routine or saves a new VERSION of one, plus,
 * on edit, the status row (pause / resume / retire). It lives inside the page's LocalOverlayDrawer,
 * so opening and closing never navigate; only Save posts, and it lands in place through
 * `useActionState` -- the outcome sentence appears beside the form, a success closes the drawer
 * and the page's revalidation has already re-read the table.
 *
 * Every visible word arrives resolved from the page contract or from the catalog the backend
 * composed (pens, roles and who holds them, the closed vocabularies with their labels). This file
 * composes none.
 *
 * The lists (pens, weekdays, month days, after-work kinds, roles) and the evidence block travel to
 * the action as ONE hidden JSON field each (routine-form-model), so the decoder stays simple.
 */

const INITIAL: PenRoutineActionState = { status: "idle", code: "", detail: "", ticket: 0 };

type QuestionDraft = PenRoutineQuestionBody & { key: number; idTouched: boolean };

type Draft = {
  parkId: string;
  name: string;
  instruction: string;
  scopeKind: ScopeKind;
  occupiedOnly: boolean;
  pens: string[];
  cadenceKind: CadenceKind;
  weekdays: number[];
  monthDays: number[];
  afterWorkKinds: string[];
  /** Kept as the input's own text so a cleared field stays blank rather than turning into 0. */
  intervalDays: string;
  startDate: string;
  /** "" means absent: the backend's default (0, or 1 after work) applies. */
  dueOffsetDays: string;
  notifyTime: string;
  reviewKind: "verifier" | "none";
  roles: string[];
  questions: QuestionDraft[];
  photo: { min: number; max: number };
  video: { min: number; max: number };
  presence: "required" | "off";
};

/** A pen's stable form identity: shed + partition, never the shed alone (a pen IS that pair). */
function penKey(pen: { shed_id: string; partition_label: string }): string {
  return `${pen.shed_id}|${pen.partition_label ?? ""}`;
}

function draftFrom(routine: PenRoutineRow | undefined, parkId: string, catalog: PenRoutineCatalog | null): Draft {
  let nextKey = 1;
  if (!routine) {
    return {
      parkId,
      name: "",
      instruction: "",
      scopeKind: "all_pens",
      occupiedOnly: true,
      pens: [],
      cadenceKind: "daily",
      weekdays: [],
      monthDays: [],
      afterWorkKinds: [],
      intervalDays: catalog ? String(catalog.defaults.interval_days) : "",
      startDate: catalog?.defaults.start_date ?? "",
      dueOffsetDays: "",
      notifyTime: catalog?.defaults.notify_time ?? "",
      reviewKind: "none",
      roles: [],
      questions: [],
      photo: { min: 0, max: 0 },
      video: { min: 0, max: 0 },
      presence: "off",
    };
  }
  return {
    parkId: routine.park_id,
    name: routine.name,
    instruction: routine.instruction,
    scopeKind: routine.scope_kind,
    occupiedOnly: routine.occupied_only,
    pens: routine.pens.map(penKey),
    cadenceKind: routine.cadence_kind,
    weekdays: [...routine.weekdays],
    monthDays: [...routine.month_days],
    afterWorkKinds: [...routine.after_work_kinds],
    intervalDays: routine.interval_days !== null ? String(routine.interval_days) : catalog ? String(catalog.defaults.interval_days) : "",
    startDate: routine.start_date,
    dueOffsetDays: String(routine.due_offset_days),
    notifyTime: routine.notify_time,
    reviewKind: routine.review_kind,
    roles: routine.assignee_roles.map((option) => option.key),
    questions: routine.evidence.questions.map((question) => ({
      ...question,
      options: question.options ? question.options.map((option) => ({ ...option })) : undefined,
      key: nextKey++,
      idTouched: true,
    })),
    photo: { ...routine.evidence.photo },
    video: { ...routine.evidence.video },
    presence: routine.evidence.presence,
  };
}

function toggle(list: string[], value: string): string[] {
  return list.includes(value) ? list.filter((item) => item !== value) : [...list, value];
}

function toggleNumber(list: number[], value: number): number[] {
  return list.includes(value) ? list.filter((item) => item !== value) : [...list, value].sort((a, b) => a - b);
}

/** Mon..Sun in the reader's short form; a weekday is not a date and keeps its own shape. */
function weekdayLabels(): string[] {
  const monday = Date.UTC(2024, 0, 1);
  return Array.from({ length: 7 }, (_, index) => new Intl.DateTimeFormat("en-GB", { weekday: "short", timeZone: "UTC" }).format(new Date(monday + index * 86400000)));
}

/** Closes the overlay the same way the drawer's own X does: Back when the entry is local, else replace. */
function closeOverlay(listHref: string): void {
  if (currentHistoryEntryIsLocalOverlay()) {
    window.history.back();
    return;
  }
  replaceLocalOverlayUrl(listHref);
}

function evidenceBody(draft: Draft): PenRoutineEvidenceBody {
  return {
    questions: draft.questions.map((question) => {
      const { key, idTouched, ...body } = question;
      void key;
      void idTouched;
      return body;
    }),
    photo: draft.photo,
    video: draft.video,
    presence: draft.presence,
  };
}

/**
 * A label the contract may not carry yet, resolved through the preferred key first so the backend
 * can add it without a web change, and through an existing key of the same page meanwhile.
 */
function label(pageContract: AdminUiPageContract, preferred: string, fallbackKey: string): string {
  return copy(pageContract, preferred, copy(pageContract, fallbackKey));
}

export function RoutineDrawerForm({
  pageContract,
  routine,
  parks,
  catalog,
  catalogParkId,
  parkHrefs,
  canEdit,
  canSetStatus,
  listHref,
}: {
  pageContract: AdminUiPageContract;
  /** Absent for the create form. */
  routine?: PenRoutineRow;
  parks: PenRoutinePark[];
  /** The page's ONE catalog, read for `catalogParkId`; null when that read failed (no pens or roles offered). */
  catalog: PenRoutineCatalog | null;
  catalogParkId: string;
  /** Create only: choosing another park is a real navigation to that park's page with the drawer open. */
  parkHrefs: Record<string, string>;
  canEdit: boolean;
  canSetStatus: boolean;
  listHref: string;
}) {
  const isEdit = routine !== undefined;
  const router = useRouter();
  const [draft, setDraft] = useState<Draft>(() => draftFrom(routine, catalogParkId, catalog));
  const [state, formAction, pending] = useActionState(isEdit ? updateRoutineAction : createRoutineAction, INITIAL);
  const [statusState, statusFormAction, statusPending] = useActionState(setRoutineStatusAction, INITIAL);
  const nextQuestionKey = useRef(1000);

  // A successful save or status change closes the drawer; the page has already re-read the table
  // in the same response, so the row the reader sees is the new version.
  const lastClosedTicket = useRef(0);
  useEffect(() => {
    const latest = [state, statusState].find((item) => item.status === "success" && item.ticket > 0);
    if (!latest) return;
    const ticket = state.ticket * 1000 + statusState.ticket;
    if (lastClosedTicket.current === ticket) return;
    lastClosedTicket.current = ticket;
    closeOverlay(listHref);
  }, [state, statusState, listHref]);

  const field = (key: string) => copy(pageContract, `field.${key}`);
  const kinds = (list: PenRoutineKeyLabel[] | undefined) => list ?? [];
  const weekdays = useMemo(() => weekdayLabels(), []);
  const update = (patch: Partial<Draft>) => setDraft((current) => ({ ...current, ...patch }));
  const updateQuestion = (key: number, patch: Partial<QuestionDraft>) =>
    setDraft((current) => ({ ...current, questions: current.questions.map((q) => (q.key === key ? { ...q, ...patch } : q)) }));

  const pens: PenRoutineCatalogPen[] = catalog?.pens ?? [];
  const roles = catalog?.roles ?? [];
  // Roles travel in the catalog's vocabulary order; a stored role the catalog did not serve is kept.
  const orderedRoles = [...roles.map((option) => option.key).filter((key) => draft.roles.includes(key)), ...draft.roles.filter((key) => !roles.some((option) => option.key === key))];
  const storedCadence = routine?.cadence_kind;
  const chooseScope = (scopeKind: ScopeKind) =>
    // A whole-park task names no pens and cannot follow work done in a pen.
    setDraft((current) =>
      scopeKind === "park"
        ? { ...current, scopeKind, pens: [], occupiedOnly: false, cadenceKind: current.cadenceKind === "after_work" ? "daily" : current.cadenceKind }
        : { ...current, scopeKind, occupiedOnly: current.scopeKind === "park" ? true : current.occupiedOnly },
    );
  const chooseCadence = (cadenceKind: CadenceKind) =>
    // Switching cadence drops a typed offset back to the backend default, unless returning to the stored cadence.
    setDraft((current) => ({ ...current, cadenceKind, dueOffsetDays: cadenceKind === storedCadence && routine ? String(routine.due_offset_days) : "" }));
  const occupiedCount = pens.filter((pen) => pen.occupied).length;
  const outcome = (item: PenRoutineActionState) =>
    item.status === "idle" ? "" : item.detail || copy(pageContract, `action.${item.code}`, copy(pageContract, "action.failed_message"));
  const message = outcome(state);
  const statusMessage = outcome(statusState);
  const readOnly = !canEdit;

  const addQuestion = () => {
    if (draft.questions.length >= LIMITS.questionsMax) return;
    const key = nextQuestionKey.current++;
    update({ questions: [...draft.questions, { key, id: "", kind: "yes_no", title: "", required: true, idTouched: false }] });
  };
  const removeQuestion = (key: number) => update({ questions: draft.questions.filter((q) => q.key !== key) });

  return (
    <>
      <form action={formAction} aria-busy={pending} className="pen-routine-form" style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        {isEdit ? <input type="hidden" name="routine_id" value={routine.routine_id} /> : null}
        {isEdit ? <input type="hidden" name="row_version" value={routine.row_version} /> : null}
        <input type="hidden" name={FORM_JSON_FIELDS.pens} value={JSON.stringify(pens.filter((pen) => draft.pens.includes(penKey(pen))).map((pen) => ({ shed_id: pen.shed_id, partition_label: pen.partition_label })))} />
        <input type="hidden" name={FORM_JSON_FIELDS.weekdays} value={JSON.stringify(draft.weekdays)} />
        <input type="hidden" name={FORM_JSON_FIELDS.monthDays} value={JSON.stringify(draft.monthDays)} />
        <input type="hidden" name={FORM_JSON_FIELDS.afterWorkKinds} value={JSON.stringify(draft.afterWorkKinds)} />
        <input type="hidden" name={FORM_JSON_FIELDS.assigneeRoles} value={JSON.stringify(orderedRoles)} />
        <input type="hidden" name={FORM_JSON_FIELDS.evidence} value={JSON.stringify(evidenceBody(draft))} />
        <input type="hidden" name="occupied_only" value={draft.occupiedOnly ? "on" : "off"} />

        {isEdit ? <div className="note">{copy(pageContract, "hint.versions")}</div> : null}

        <div className="fld">
          <label htmlFor="pr-name">{field("name")}</label>
          <input id="pr-name" name="name" required maxLength={LIMITS.nameMax} value={draft.name} onChange={(e) => update({ name: e.target.value })} readOnly={readOnly} />
        </div>
        <div className="fld">
          <label htmlFor="pr-instruction">{field("instruction")}</label>
          <textarea id="pr-instruction" name="instruction" rows={2} value={draft.instruction} onChange={(e) => update({ instruction: e.target.value })} readOnly={readOnly} />
        </div>

        {/* Park: chosen once at create; a routine belongs to ONE park, so edit shows it fixed. */}
        <div className="fld">
          <label htmlFor="pr-park">{field("park")}</label>
          {isEdit ? (
            <>
              <input type="hidden" name="park_id" value={routine.park_id} />
              <input id="pr-park" value={routine.park_name} readOnly disabled />
            </>
          ) : (
            <select
              id="pr-park"
              name="park_id"
              value={draft.parkId}
              onChange={(e) => {
                // Another park means another catalog: navigate to that park's page with the
                // create drawer open rather than reading a second catalog from here.
                const parkId = e.target.value;
                const target = parkHrefs[parkId];
                if (parkId !== catalogParkId && target) router.push(target, { scroll: false });
                else update({ parkId });
              }}
              disabled={readOnly}
            >
              {parks.map((park) => (
                <option key={park.park_id} value={park.park_id}>
                  {park.name}
                </option>
              ))}
            </select>
          )}
        </div>

        {/* Who does it: roles, never named people. Under each, who holds it for this park today. */}
        <fieldset className="fld" disabled={readOnly}>
          <legend>{field("assignee_roles")}</legend>
          <div className="pen-routine-checklist">
            {roles.map((option) => (
              <label key={option.key} className="pen-routine-check" style={{ alignItems: "flex-start" }}>
                <input type="checkbox" checked={draft.roles.includes(option.key)} onChange={() => update({ roles: toggle(draft.roles, option.key) })} />
                <span style={{ display: "flex", flexDirection: "column" }}>
                  <span>{option.label}</span>
                  <span className="muted small">
                    {option.people.length ? option.people.map((person) => person.display_name).join(", ") : copy(pageContract, "empty.role_people")}
                  </span>
                </span>
              </label>
            ))}
          </div>
          <p className="muted small">{copy(pageContract, "hint.assignee_roles")}</p>
        </fieldset>

        {/* Where: every pen, a ticked list from the partition catalog, or ONE task for the whole park. */}
        <fieldset className="fld" disabled={readOnly}>
          <legend>{field("scope")}</legend>
          <div className="pen-routine-radios">
            {kinds(catalog?.scope_kinds).map((option) => (
              <label key={option.key} className="pen-routine-check">
                <input type="radio" name="scope_kind" value={option.key} checked={draft.scopeKind === option.key} onChange={() => chooseScope(option.key as Draft["scopeKind"])} />
                <span>{option.label}</span>
              </label>
            ))}
          </div>
          {draft.scopeKind === "all_pens" ? (
            <label className="pen-routine-check">
              <input type="checkbox" checked={draft.occupiedOnly} onChange={(e) => update({ occupiedOnly: e.target.checked })} />
              <span>{field("occupied_only")}</span>
              <span className="muted small">
                {occupiedCount} / {pens.length}
              </span>
            </label>
          ) : null}
          {draft.scopeKind === "selected_pens" ? (
            <div className="pen-routine-checklist">
              {pens.map((pen) => {
                const key = penKey(pen);
                return (
                  <label key={key} className={pen.occupied ? "pen-routine-check" : "pen-routine-check muted"}>
                    <input type="checkbox" checked={draft.pens.includes(key)} onChange={() => update({ pens: toggle(draft.pens, key) })} />
                    {/* Backend-composed pen label, rendered verbatim: "Castro 2", "Godel 1 - Part 3". */}
                    <span>{pen.operational_location_display}</span>
                  </label>
                );
              })}
            </div>
          ) : null}
          {draft.scopeKind === "park" ? <p className="muted small">{copy(pageContract, "hint.park_scope")}</p> : null}
        </fieldset>

        {/* When: which business dates raise a task, from which day, and when the push goes out. */}
        <fieldset className="fld" disabled={readOnly}>
          <legend>{field("cadence")}</legend>
          <div className="pen-routine-radios">
            {kinds(catalog?.cadence_kinds).map((option) => {
              // Work happens IN a pen, so a whole-park task cannot follow it.
              const unavailable = option.key === "after_work" && draft.scopeKind === "park";
              return (
                <label key={option.key} className={unavailable ? "pen-routine-check muted" : "pen-routine-check"}>
                  <input type="radio" name="cadence_kind" value={option.key} checked={draft.cadenceKind === option.key} disabled={unavailable} onChange={() => chooseCadence(option.key as Draft["cadenceKind"])} />
                  <span>{option.label}</span>
                </label>
              );
            })}
          </div>
          {draft.cadenceKind === "weekly" ? (
            <div className="fld">
              <span className="muted small">{field("weekdays")}</span>
              <div className="pen-routine-toggles" role="group" aria-label={field("weekdays")}>
                {weekdays.map((name, index) => {
                  const day = index + 1;
                  const on = draft.weekdays.includes(day);
                  return (
                    <button key={day} type="button" className={on ? "btn sm primary" : "btn sm"} aria-pressed={on} onClick={() => update({ weekdays: toggleNumber(draft.weekdays, day) })}>
                      {name}
                    </button>
                  );
                })}
              </div>
            </div>
          ) : null}
          {draft.cadenceKind === "monthly" ? (
            <div className="fld">
              <span className="muted small">{field("month_days")}</span>
              <div className="pen-routine-toggles" role="group" aria-label={field("month_days")}>
                {Array.from({ length: 31 }, (_, index) => index + 1).map((day) => {
                  const on = draft.monthDays.includes(day);
                  return (
                    <button key={day} type="button" className={on ? "btn sm primary" : "btn sm"} aria-pressed={on} onClick={() => update({ monthDays: toggleNumber(draft.monthDays, day) })}>
                      {day}
                    </button>
                  );
                })}
              </div>
            </div>
          ) : null}
          {draft.cadenceKind === "every_n_days" ? (
            <div className="fld">
              <label htmlFor="pr-interval">{field("interval_days")}</label>
              <input
                id="pr-interval"
                name="interval_days"
                type="number"
                required
                min={LIMITS.intervalMin}
                max={LIMITS.intervalMax}
                value={draft.intervalDays}
                onChange={(e) => update({ intervalDays: e.target.value })}
              />
            </div>
          ) : null}
          {draft.cadenceKind === "after_work" ? (
            <>
              <div className="fld">
                <span className="muted small">{field("after_work_kinds")}</span>
                <div className="pen-routine-checklist">
                  {kinds(catalog?.work_kinds).map((option) => (
                    <label key={option.key} className="pen-routine-check">
                      <input type="checkbox" checked={draft.afterWorkKinds.includes(option.key)} onChange={() => update({ afterWorkKinds: toggle(draft.afterWorkKinds, option.key) })} />
                      <span>{option.label}</span>
                    </label>
                  ))}
                </div>
              </div>
              <div className="fld">
                <label htmlFor="pr-due-offset">{field("due_offset_days")}</label>
                <input id="pr-due-offset" name="due_offset_days" type="number" min={LIMITS.dueOffsetMin} max={LIMITS.dueOffsetMax} value={draft.dueOffsetDays} onChange={(e) => update({ dueOffsetDays: e.target.value })} />
              </div>
            </>
          ) : (
            // A calendar cadence keeps a stored offset untouched; a blank one lets the backend default apply.
            <input type="hidden" name="due_offset_days" value={draft.dueOffsetDays} />
          )}
          <div className="grid g2">
            <div className="fld">
              <label htmlFor="pr-start-date">{field("start_date")}</label>
              <input id="pr-start-date" name="start_date" type="date" value={draft.startDate} onChange={(e) => update({ startDate: e.target.value })} />
            </div>
            <div className="fld">
              <label htmlFor="pr-notify-time">{field("notify_time")}</label>
              <input id="pr-notify-time" name="notify_time" type="time" value={draft.notifyTime} onChange={(e) => update({ notifyTime: e.target.value })} />
            </div>
          </div>
        </fieldset>

        {/* Evidence: the questions, the capture counts, the pen check-in. */}
        <fieldset className="fld" disabled={readOnly}>
          <legend>{field("questions")}</legend>
          <div className="pen-routine-questions">
            {draft.questions.map((question, index) => (
              <div key={question.key} className="pen-routine-question card" style={{ marginTop: 0, padding: 10 }}>
                <div className="grid g2">
                  <div className="fld">
                    <label htmlFor={`pr-q-title-${question.key}`}>{label(pageContract, "field.question_title", "field.name")}</label>
                    <input
                      id={`pr-q-title-${question.key}`}
                      value={question.title}
                      required
                      onChange={(e) =>
                        updateQuestion(question.key, { title: e.target.value, id: question.idTouched ? question.id : slugQuestionId(e.target.value) })
                      }
                    />
                  </div>
                  <div className="fld">
                    <label htmlFor={`pr-q-kind-${question.key}`}>{label(pageContract, "field.question_kind", "field.questions")}</label>
                    <select id={`pr-q-kind-${question.key}`} value={question.kind} onChange={(e) => updateQuestion(question.key, { kind: e.target.value as QuestionDraft["kind"] })}>
                      {kinds(catalog?.question_kinds).map((option) => (
                        <option key={option.key} value={option.key}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="fld">
                    <label htmlFor={`pr-q-id-${question.key}`}>{label(pageContract, "field.question_id", "field.name")}</label>
                    <input id={`pr-q-id-${question.key}`} value={question.id} required onChange={(e) => updateQuestion(question.key, { id: cleanQuestionId(e.target.value), idTouched: true })} />
                  </div>
                  <div className="fld">
                    <label htmlFor={`pr-q-hint-${question.key}`}>{label(pageContract, "field.question_hint", "field.instruction")}</label>
                    <input id={`pr-q-hint-${question.key}`} value={question.hint ?? ""} onChange={(e) => updateQuestion(question.key, { hint: e.target.value })} />
                  </div>
                  <div className="fld">
                    <label className="pen-routine-check" htmlFor={`pr-q-required-${question.key}`}>
                      <input id={`pr-q-required-${question.key}`} type="checkbox" checked={question.required} onChange={(e) => updateQuestion(question.key, { required: e.target.checked })} />
                      <span>{label(pageContract, "field.question_required", "field.presence")}</span>
                    </label>
                  </div>
                  {/* Per-question proof (maintainer instruction 2026-09-18): what capture this question
                      needs to count as answered, and one or several. "none" is the catalog's own key for
                      no proof; it never travels -- the body simply carries no proof block. */}
                  <div className="fld">
                    <label htmlFor={`pr-q-proof-${question.key}`}>{label(pageContract, "field.question_proof", "field.photo")}</label>
                    <select
                      id={`pr-q-proof-${question.key}`}
                      value={question.proof?.kind ?? "none"}
                      onChange={(e) => {
                        const kind = e.target.value;
                        updateQuestion(question.key, {
                          proof: kind === "none" ? undefined : { kind: kind as NonNullable<QuestionDraft["proof"]>["kind"], count: question.proof?.count ?? "single" },
                        });
                      }}
                    >
                      {kinds(catalog?.question_proof_kinds).map((option) => (
                        <option key={option.key} value={option.key}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  </div>
                  {question.proof ? (
                    <div className="fld">
                      <label htmlFor={`pr-q-proof-count-${question.key}`}>{label(pageContract, "field.question_proof_count", "field.max")}</label>
                      <select
                        id={`pr-q-proof-count-${question.key}`}
                        value={question.proof.count}
                        onChange={(e) => updateQuestion(question.key, { proof: { kind: question.proof!.kind, count: e.target.value as NonNullable<QuestionDraft["proof"]>["count"] } })}
                      >
                        {kinds(catalog?.question_proof_counts).map((option) => (
                          <option key={option.key} value={option.key}>
                            {option.label}
                          </option>
                        ))}
                      </select>
                    </div>
                  ) : null}
                  {question.kind === "number" ? (
                    <>
                      <div className="fld">
                        <label htmlFor={`pr-q-min-${question.key}`}>{field("min")}</label>
                        <input id={`pr-q-min-${question.key}`} type="number" value={question.min ?? ""} onChange={(e) => updateQuestion(question.key, { min: e.target.value === "" ? null : Number(e.target.value) })} />
                      </div>
                      <div className="fld">
                        <label htmlFor={`pr-q-max-${question.key}`}>{field("max")}</label>
                        <input id={`pr-q-max-${question.key}`} type="number" value={question.max ?? ""} onChange={(e) => updateQuestion(question.key, { max: e.target.value === "" ? null : Number(e.target.value) })} />
                      </div>
                      <div className="fld">
                        <label htmlFor={`pr-q-unit-${question.key}`}>{label(pageContract, "field.question_unit", "field.questions")}</label>
                        <input id={`pr-q-unit-${question.key}`} value={question.unit ?? ""} onChange={(e) => updateQuestion(question.key, { unit: e.target.value })} />
                      </div>
                    </>
                  ) : null}
                </div>
                {question.kind === "choice" || question.kind === "multi_choice" ? (
                  <div className="fld" style={{ marginTop: 8 }}>
                    <span className="muted small">{label(pageContract, "field.question_options", "field.questions")}</span>
                    {(question.options ?? []).map((option, optionIndex) => (
                      <div key={optionIndex} className="pen-routine-option" style={{ display: "flex", gap: 6, alignItems: "center" }}>
                        <input
                          value={option.value}
                          aria-label={label(pageContract, "field.option_value", "field.name")}
                          onChange={(e) => {
                            const options = (question.options ?? []).map((item, i) => (i === optionIndex ? { ...item, value: cleanQuestionId(e.target.value) } : item));
                            updateQuestion(question.key, { options });
                          }}
                          style={{ flex: 1 }}
                        />
                        <input
                          value={option.label}
                          aria-label={label(pageContract, "field.option_label", "field.name")}
                          onChange={(e) => {
                            const options = (question.options ?? []).map((item, i) => (i === optionIndex ? { ...item, label: e.target.value } : item));
                            updateQuestion(question.key, { options });
                          }}
                          style={{ flex: 2 }}
                        />
                        <button type="button" className="iconbtn" aria-label={label(pageContract, "action.remove_option", "action.clear")} onClick={() => updateQuestion(question.key, { options: (question.options ?? []).filter((_, i) => i !== optionIndex) })}>
                          <Trash2 className="ic" aria-hidden="true" />
                        </button>
                      </div>
                    ))}
                    {(question.options ?? []).length < LIMITS.optionsMax ? (
                      <button type="button" className="btn sm" onClick={() => updateQuestion(question.key, { options: [...(question.options ?? []), { value: "", label: "" }] })}>
                        <Plus className="ic" aria-hidden="true" /> {label(pageContract, "action.add_option", "field.questions")}
                      </button>
                    ) : null}
                  </div>
                ) : null}
                <div style={{ display: "flex", justifyContent: "flex-end", marginTop: 8 }}>
                  <button type="button" className="btn sm" aria-label={`${label(pageContract, "action.remove_question", "action.clear")} ${index + 1}`} onClick={() => removeQuestion(question.key)}>
                    <Trash2 className="ic" aria-hidden="true" /> {label(pageContract, "action.remove_question", "action.clear")}
                  </button>
                </div>
              </div>
            ))}
          </div>
          {draft.questions.length < LIMITS.questionsMax ? (
            <button type="button" className="btn sm" onClick={addQuestion}>
              <Plus className="ic" aria-hidden="true" /> {label(pageContract, "action.add_question", "field.questions")}
            </button>
          ) : null}

          <div className="grid g2" style={{ marginTop: 10 }}>
            <div className="fld">
              <span className="muted small">{field("photo")}</span>
              <div className="pen-routine-proof">
                <div className="fld">
                  <label htmlFor="pr-photo-min">{field("min")}</label>
                  <input id="pr-photo-min" type="number" min={0} max={LIMITS.proofMax} value={draft.photo.min} onChange={(e) => update({ photo: { ...draft.photo, min: Number.parseInt(e.target.value, 10) || 0 } })} />
                </div>
                <div className="fld">
                  <label htmlFor="pr-photo-max">{field("max")}</label>
                  <input id="pr-photo-max" type="number" min={0} max={LIMITS.proofMax} value={draft.photo.max} onChange={(e) => update({ photo: { ...draft.photo, max: Number.parseInt(e.target.value, 10) || 0 } })} />
                </div>
              </div>
            </div>
            <div className="fld">
              <span className="muted small">{field("video")}</span>
              <div className="pen-routine-proof">
                <div className="fld">
                  <label htmlFor="pr-video-min">{field("min")}</label>
                  <input id="pr-video-min" type="number" min={0} max={LIMITS.proofMax} value={draft.video.min} onChange={(e) => update({ video: { ...draft.video, min: Number.parseInt(e.target.value, 10) || 0 } })} />
                </div>
                <div className="fld">
                  <label htmlFor="pr-video-max">{field("max")}</label>
                  <input id="pr-video-max" type="number" min={0} max={LIMITS.proofMax} value={draft.video.max} onChange={(e) => update({ video: { ...draft.video, max: Number.parseInt(e.target.value, 10) || 0 } })} />
                </div>
              </div>
            </div>
          </div>
          <div className="fld">
            <span className="muted small">{field("presence")}</span>
            <div className="pen-routine-radios">
              {kinds(catalog?.presence_kinds).map((option) => (
                <label key={option.key} className="pen-routine-check">
                  <input type="radio" name="presence_kind" value={option.key} checked={draft.presence === option.key} onChange={() => update({ presence: option.key as Draft["presence"] })} />
                  <span>{option.label}</span>
                </label>
              ))}
            </div>
          </div>
        </fieldset>

        {/* Review: what a submit does. */}
        <fieldset className="fld" disabled={readOnly}>
          <legend>{field("review")}</legend>
          <div className="pen-routine-radios">
            {kinds(catalog?.review_kinds).map((option) => (
              <label key={option.key} className="pen-routine-check">
                <input type="radio" name="review_kind" value={option.key} checked={draft.reviewKind === option.key} onChange={() => update({ reviewKind: option.key as Draft["reviewKind"] })} />
                <span>{option.label}</span>
              </label>
            ))}
          </div>
        </fieldset>

        {readOnly ? (
          <div className="note">{copy(pageContract, "configure.disabled_no_access")}</div>
        ) : (
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <button type="submit" className="btn primary" disabled={pending}>
              {copy(pageContract, "action.save")}
            </button>
            {message ? (
              <span role="status" className="small" style={{ color: state.status === "success" ? "var(--ok)" : "var(--danger)" }}>
                {message}
              </span>
            ) : null}
          </div>
        )}
      </form>

      {/* Status: pause / resume / retire. Open tasks are untouched by any of these. */}
      {isEdit && canSetStatus && routine.status !== "retired" ? (
        <form action={statusFormAction} aria-busy={statusPending} className="pen-routine-status" style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 16 }}>
          <input type="hidden" name="routine_id" value={routine.routine_id} />
          <input type="hidden" name="row_version" value={routine.row_version} />
          <Tag tone={routine.status === "active" ? "ok" : "warn"}>{routine.status_label}</Tag>
          {routine.status === "active" ? (
            <button type="submit" name="status" value="paused" className="btn sm" disabled={statusPending}>
              {copy(pageContract, "action.pause")}
            </button>
          ) : (
            <button type="submit" name="status" value="active" className="btn sm" disabled={statusPending}>
              {copy(pageContract, "action.resume")}
            </button>
          )}
          <button
            type="submit"
            name="status"
            value="retired"
            className="btn sm"
            disabled={statusPending}
            onClick={(event) => {
              // Retire is permanent for the routine (never raises again), so it asks once.
              if (!window.confirm(copy(pageContract, "action.retire.confirm"))) event.preventDefault();
            }}
          >
            {copy(pageContract, "action.retire")}
          </button>
          {statusMessage ? (
            <span role="status" className="small" style={{ color: statusState.status === "success" ? "var(--ok)" : "var(--danger)" }}>
              {statusMessage}
            </span>
          ) : null}
        </form>
      ) : null}
    </>
  );
}
