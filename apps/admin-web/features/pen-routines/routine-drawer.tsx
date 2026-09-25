"use client";

import { Check, Plus, Search, Trash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useActionState, useEffect, useId, useMemo, useRef, useState, useSyncExternalStore } from "react";

import { initials } from "@/components/assignee-picker";
import { currentHistoryEntryIsLocalOverlay, replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { ThemedDatePicker } from "@/components/themed-date-picker";
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
 * `useActionState` -- the outcome sentence appears beside Save, a success closes the drawer and the
 * page's revalidation has already re-read the table.
 *
 * LAYOUT (maintainer report 2026-09-26: "routines, especially the assigning part, is worst"): the
 * form reads as numbered steps -- Details, Who does it, Pens, When, What to record, Review -- each a
 * card with the same heading. "Who does it" is the part that was worst: eight bare checkboxes with
 * the holders greyed underneath, and nothing saying who the routine would actually reach. It is now
 * one row per role (a real checkbox, the role, the people holding it for this park as avatars) with
 * a live line above the list naming everyone the routine will reach, and a warning when that is
 * nobody. Save sits in the drawer's sticky footer (`form=` attribute), so it is never scrolled away.
 *
 * Every visible word arrives resolved from the page contract or from the catalog the backend
 * composed (pens, roles and who holds them, the closed vocabularies with their labels). This file
 * composes none; keys the contract may not carry yet are read through `label()` with an existing
 * key as the stand-in.
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

/** Fills `{name}` holes in a backend sentence. */
function fill(template: string, values: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (hole, key: string) => (key in values ? String(values[key]) : hole));
}

/**
 * A counted or named sentence the contract may not carry yet. With the key served, its template is
 * filled; without it, the bare data (`plain`) is shown rather than a stand-in word that drops it.
 */
function sentence(pageContract: AdminUiPageContract, key: string, values: Record<string, string | number>, plain: string): string {
  const template = copy(pageContract, key, "");
  return template ? fill(template, values) : plain;
}

/** One numbered step of the form: the same heading everywhere, the step's fields beneath. */
function Step({ index, title, hint, children, aside }: { index: number; title: string; hint?: string; children: React.ReactNode; aside?: React.ReactNode }) {
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
function ChoiceTiles({
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
  formId,
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
  /** The form's id; the drawer footer's Save names it through `form=` so it can sit outside the scroll. */
  formId: string;
}) {
  const isEdit = routine !== undefined;
  const router = useRouter();
  const [draft, setDraft] = useState<Draft>(() => draftFrom(routine, catalogParkId, catalog));
  const [state, formAction, pending] = useActionState(isEdit ? updateRoutineAction : createRoutineAction, INITIAL);
  const [statusState, statusFormAction, statusPending] = useActionState(setRoutineStatusAction, INITIAL);
  const [confirmRetire, setConfirmRetire] = useState(false);
  const [penQuery, setPenQuery] = useState("");
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
  const times = useMemo(() => timeOptions(draft.notifyTime), [draft.notifyTime]);
  const update = (patch: Partial<Draft>) => setDraft((current) => ({ ...current, ...patch }));
  const updateQuestion = (key: number, patch: Partial<QuestionDraft>) =>
    setDraft((current) => ({ ...current, questions: current.questions.map((q) => (q.key === key ? { ...q, ...patch } : q)) }));

  const pens: PenRoutineCatalogPen[] = catalog?.pens ?? [];
  const roles = catalog?.roles ?? [];
  // Roles travel in the catalog's vocabulary order; a stored role the catalog did not serve is kept.
  const orderedRoles = [...roles.map((option) => option.key).filter((key) => draft.roles.includes(key)), ...draft.roles.filter((key) => !roles.some((option) => option.key === key))];
  // Everyone the chosen roles reach at this park, each person once however many roles they hold.
  const reached = useMemo(() => {
    const seen = new Map<string, string>();
    for (const option of roles) {
      if (!draft.roles.includes(option.key)) continue;
      for (const person of option.people) if (!seen.has(person.user_id)) seen.set(person.user_id, person.display_name);
    }
    return [...seen.values()];
  }, [roles, draft.roles]);
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
  const outcome = (item: PenRoutineActionState, success: string, failure: string) =>
    item.status === "idle" ? "" : item.detail || (item.status === "success" ? success : item.code === "error_form" ? copy(pageContract, "action.error_form") : failure);
  const message = outcome(state, label(pageContract, "action.routine_saved", "action.success_message"), label(pageContract, "action.routine_failed", "action.failed_message"));
  const statusMessage = outcome(statusState, copy(pageContract, "action.success_message"), copy(pageContract, "action.failed_message"));
  const readOnly = !canEdit;
  const parkName = isEdit ? routine.park_name : parks.find((park) => park.park_id === draft.parkId)?.name ?? "";

  const needle = penQuery.trim().toLowerCase();
  const shownPens = needle ? pens.filter((pen) => pen.operational_location_display.toLowerCase().includes(needle)) : pens;

  const addQuestion = () => {
    if (draft.questions.length >= LIMITS.questionsMax) return;
    const key = nextQuestionKey.current++;
    update({ questions: [...draft.questions, { key, id: "", kind: "yes_no", title: "", required: true, idTouched: false }] });
  };
  const removeQuestion = (key: number) => update({ questions: draft.questions.filter((q) => q.key !== key) });
  // Per-question proof is offered only when there is a choice to make; while the catalog serves just
  // "no proof" (the phone rollout gate), the select would be a dead control with one entry.
  const proofKinds = kinds(catalog?.question_proof_kinds);
  const proofChoosable = proofKinds.length > 1;

  const countField = (id: string, value: number, onValue: (next: number) => void) => (
    <input
      id={id}
      type="number"
      inputMode="numeric"
      min={0}
      max={LIMITS.proofMax}
      value={value}
      onChange={(e) => onValue(Number.parseInt(e.target.value, 10) || 0)}
    />
  );

  return (
    <div className="prt">
      <form id={formId} action={formAction} aria-busy={pending} className="pen-routine-form prt-form">
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
        {readOnly ? <div className="note">{copy(pageContract, "configure.disabled_no_access")}</div> : null}

        <fieldset disabled={readOnly} className="prt-fieldset">
          {/* 1. Details: name, instruction, the ONE park it belongs to. */}
          <Step index={1} title={label(pageContract, "section.details", "drawer.routine.title")}>
            <div className="fld">
              <label htmlFor="pr-name">{field("name")}</label>
              <input id="pr-name" name="name" required maxLength={LIMITS.nameMax} value={draft.name} onChange={(e) => update({ name: e.target.value })} />
            </div>
            <div className="fld">
              <label htmlFor="pr-instruction">{field("instruction")}</label>
              <textarea id="pr-instruction" name="instruction" rows={2} value={draft.instruction} onChange={(e) => update({ instruction: e.target.value })} />
            </div>
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
                    // Another park means another catalog (its pens, who holds each role there):
                    // move to that park's page with the create drawer open rather than reading a
                    // second catalog from here.
                    const parkId = e.target.value;
                    const target = parkHrefs[parkId];
                    if (parkId !== catalogParkId && target) router.replace(target, { scroll: false });
                    else update({ parkId });
                  }}
                >
                  {parks.map((park) => (
                    <option key={park.park_id} value={park.park_id}>
                      {park.name}
                    </option>
                  ))}
                </select>
              )}
            </div>
          </Step>

          {/* 2. Who does it: roles, never named people -- whoever holds a role for this park owes the
              task. Each row shows who that is today; the line above says who the routine reaches. */}
          <Step index={2} title={field("assignee_roles")} hint={copy(pageContract, "hint.assignee_roles")}>
            <div className={reached.length ? "prt-reach" : "prt-reach warn"} role="status" aria-live="polite">
              {!draft.roles.length ? (
                label(pageContract, "summary.no_roles", "hint.assignee_roles")
              ) : reached.length ? (
                <>
                  <span className="avs prt-reach-avs" aria-hidden="true">
                    {reached.slice(0, 5).map((name) => (
                      <span key={name} className="av">
                        {initials(name)}
                      </span>
                    ))}
                  </span>
                  <span>{sentence(pageContract, "summary.goes_to", { names: reached.join(", "), count: reached.length }, reached.join(", "))}</span>
                </>
              ) : (
                label(pageContract, "summary.no_holders", "empty.role_people")
              )}
            </div>
            <div className="prt-roles">
              {roles.map((option) => {
                const on = draft.roles.includes(option.key);
                return (
                  <label key={option.key} className={on ? "prt-role on" : "prt-role"}>
                    <input type="checkbox" checked={on} onChange={() => update({ roles: toggle(draft.roles, option.key) })} />
                    <span className="prt-role-text">
                      <span className="prt-role-name">{option.label}</span>
                      <span className={option.people.length ? "prt-role-people" : "prt-role-people none"}>
                        {option.people.length ? option.people.map((person) => person.display_name).join(", ") : copy(pageContract, "empty.role_people")}
                      </span>
                    </span>
                    {option.people.length ? (
                      <span className="avs prt-role-avs" aria-hidden="true">
                        {option.people.slice(0, 3).map((person) => (
                          <span key={person.user_id} className="av">
                            {initials(person.display_name)}
                          </span>
                        ))}
                        {option.people.length > 3 ? <span className="av more">+{option.people.length - 3}</span> : null}
                      </span>
                    ) : null}
                  </label>
                );
              })}
            </div>
          </Step>

          {/* 3. Pens: every pen, a ticked list from the partition catalog, or ONE task for the whole park. */}
          <Step index={3} title={field("scope")} aside={parkName}>
            <ChoiceTiles name="scope_kind" options={kinds(catalog?.scope_kinds)} value={draft.scopeKind} onChange={(key) => chooseScope(key as ScopeKind)} />
            {draft.scopeKind === "all_pens" ? (
              <label className="prt-check">
                <input type="checkbox" checked={draft.occupiedOnly} onChange={(e) => update({ occupiedOnly: e.target.checked })} />
                <span>{field("occupied_only")}</span>
                <span className="prt-count">
                  {sentence(pageContract, "count.pens_occupied", { occupied: occupiedCount, total: pens.length }, `${occupiedCount} / ${pens.length}`)}
                </span>
              </label>
            ) : null}
            {draft.scopeKind === "selected_pens" ? (
              <div className="prt-pens">
                <div className="prt-pens-bar">
                  <span className="prt-search">
                    <Search className="ic" aria-hidden="true" />
                    <input
                      type="search"
                      value={penQuery}
                      placeholder={label(pageContract, "filter.pens_search", "field.scope")}
                      aria-label={label(pageContract, "filter.pens_search", "field.scope")}
                      onChange={(e) => setPenQuery(e.target.value)}
                    />
                  </span>
                  <button
                    type="button"
                    className="btn sm"
                    onClick={() => update({ pens: [...new Set([...draft.pens, ...shownPens.map(penKey)])] })}
                    disabled={!shownPens.length}
                  >
                    {label(pageContract, "action.select_all_pens", "filter.park.all")}
                  </button>
                  <button
                    type="button"
                    className="btn sm"
                    onClick={() => update({ pens: draft.pens.filter((key) => !shownPens.some((pen) => penKey(pen) === key)) })}
                    disabled={!draft.pens.length}
                  >
                    {label(pageContract, "action.clear_pens", "action.close")}
                  </button>
                  <span className="prt-count">{sentence(pageContract, "count.pens_chosen", { chosen: draft.pens.length, total: pens.length }, `${draft.pens.length} / ${pens.length}`)}</span>
                </div>
                <div className="prt-pen-list">
                  {shownPens.map((pen) => {
                    const key = penKey(pen);
                    const on = draft.pens.includes(key);
                    return (
                      <label key={key} className={["prt-check", "prt-pen", on ? "on" : "", pen.occupied ? "" : "empty"].filter(Boolean).join(" ")}>
                        <input type="checkbox" checked={on} onChange={() => update({ pens: toggle(draft.pens, key) })} />
                        {/* Backend-composed pen label, rendered verbatim: "Castro 2", "Godel 1 - Part 3". */}
                        <span>{pen.operational_location_display}</span>
                        {pen.occupied ? null : <span className="prt-count">{copy(pageContract, "label.pen_empty", "")}</span>}
                      </label>
                    );
                  })}
                  {!shownPens.length ? <p className="prt-hint">{label(pageContract, "empty.pens_search", "empty.tasks")}</p> : null}
                </div>
              </div>
            ) : null}
            {draft.scopeKind === "park" ? <p className="prt-hint">{copy(pageContract, "hint.park_scope")}</p> : null}
          </Step>

          {/* 4. When: which business dates raise a task, from which day, and when the push goes out. */}
          <Step index={4} title={field("cadence")}>
            <ChoiceTiles
              name="cadence_kind"
              options={kinds(catalog?.cadence_kinds)}
              value={draft.cadenceKind}
              onChange={(key) => chooseCadence(key as CadenceKind)}
              // Work happens IN a pen, so a whole-park task cannot follow it.
              isDisabled={(key) => key === "after_work" && draft.scopeKind === "park"}
            />
            {draft.cadenceKind === "weekly" ? (
              <div className="fld">
                <span className="prt-sub">{field("weekdays")}</span>
                <div className="prt-pills" role="group" aria-label={field("weekdays")}>
                  {weekdays.map((name, index) => {
                    const day = index + 1;
                    const on = draft.weekdays.includes(day);
                    return (
                      <label key={day} className={on ? "prt-pill on" : "prt-pill"}>
                        <input type="checkbox" checked={on} onChange={() => update({ weekdays: toggleNumber(draft.weekdays, day) })} />
                        <span>{name}</span>
                      </label>
                    );
                  })}
                </div>
              </div>
            ) : null}
            {draft.cadenceKind === "monthly" ? (
              <div className="fld">
                <span className="prt-sub">{field("month_days")}</span>
                <div className="prt-pills prt-month" role="group" aria-label={field("month_days")}>
                  {Array.from({ length: 31 }, (_, index) => index + 1).map((day) => {
                    const on = draft.monthDays.includes(day);
                    return (
                      <label key={day} className={on ? "prt-pill on" : "prt-pill"}>
                        <input type="checkbox" checked={on} onChange={() => update({ monthDays: toggleNumber(draft.monthDays, day) })} />
                        <span>{day}</span>
                      </label>
                    );
                  })}
                </div>
              </div>
            ) : null}
            {draft.cadenceKind === "every_n_days" ? (
              <div className="fld prt-narrow">
                <label htmlFor="pr-interval">{field("interval_days")}</label>
                <input
                  id="pr-interval"
                  name="interval_days"
                  type="number"
                  inputMode="numeric"
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
                  <span className="prt-sub">{field("after_work_kinds")}</span>
                  <div className="prt-pills" role="group" aria-label={field("after_work_kinds")}>
                    {kinds(catalog?.work_kinds).map((option) => {
                      const on = draft.afterWorkKinds.includes(option.key);
                      return (
                        <label key={option.key} className={on ? "prt-pill on" : "prt-pill"}>
                          <input type="checkbox" checked={on} onChange={() => update({ afterWorkKinds: toggle(draft.afterWorkKinds, option.key) })} />
                          <span>{option.label}</span>
                        </label>
                      );
                    })}
                  </div>
                </div>
                <div className="fld prt-narrow">
                  <label htmlFor="pr-due-offset">{field("due_offset_days")}</label>
                  <input
                    id="pr-due-offset"
                    name="due_offset_days"
                    type="number"
                    inputMode="numeric"
                    min={LIMITS.dueOffsetMin}
                    max={LIMITS.dueOffsetMax}
                    value={draft.dueOffsetDays}
                    onChange={(e) => update({ dueOffsetDays: e.target.value })}
                  />
                </div>
              </>
            ) : (
              // A calendar cadence keeps a stored offset untouched; a blank one lets the backend default apply.
              <input type="hidden" name="due_offset_days" value={draft.dueOffsetDays} />
            )}
            <div className="prt-row2">
              <div className="fld">
                <span className="prt-sub">{field("start_date")}</span>
                <ThemedDatePicker
                  name="start_date"
                  label={field("start_date")}
                  value={draft.startDate}
                  onChange={(key) => update({ startDate: key })}
                  previousMonthLabel={label(pageContract, "date.previous_month", "action.previous")}
                  nextMonthLabel={label(pageContract, "date.next_month", "action.next")}
                  invalidDateText={label(pageContract, "date.invalid", "action.error_form")}
                />
              </div>
              <div className="fld">
                <label htmlFor="pr-notify-time">{field("notify_time")}</label>
                <select id="pr-notify-time" name="notify_time" value={draft.notifyTime.slice(0, 5)} onChange={(e) => update({ notifyTime: e.target.value })}>
                  {times.map((time) => (
                    <option key={time} value={time}>
                      {time}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          </Step>

          {/* 5. What to record: the questions, the capture counts, the pen check-in. */}
          <Step index={5} title={label(pageContract, "section.capture", "field.questions")}>
            <span className="prt-sub">{field("questions")}</span>
            <div className="prt-questions">
              {draft.questions.map((question, index) => (
                <div key={question.key} className="prt-question">
                  <div className="prt-question-hd">
                    <span className="prt-step-no sm" aria-hidden="true">
                      {index + 1}
                    </span>
                    <button
                      type="button"
                      className="iconbtn"
                      aria-label={`${label(pageContract, "action.remove_question", "action.close")} ${index + 1}`}
                      title={label(pageContract, "action.remove_question", "action.close")}
                      onClick={() => removeQuestion(question.key)}
                    >
                      <Trash2 className="ic" aria-hidden="true" />
                    </button>
                  </div>
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
                  <div className="prt-row2">
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
                      <label htmlFor={`pr-q-hint-${question.key}`}>{label(pageContract, "field.question_hint", "field.instruction")}</label>
                      <input id={`pr-q-hint-${question.key}`} value={question.hint ?? ""} onChange={(e) => updateQuestion(question.key, { hint: e.target.value })} />
                    </div>
                  </div>
                  {question.kind === "number" ? (
                    <div className="prt-row3">
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
                    </div>
                  ) : null}
                  {question.kind === "choice" || question.kind === "multi_choice" ? (
                    <div className="fld">
                      <span className="prt-sub">{label(pageContract, "field.question_options", "field.questions")}</span>
                      {(question.options ?? []).map((option, optionIndex) => (
                        <div key={optionIndex} className="prt-option">
                          <input
                            value={option.label}
                            aria-label={label(pageContract, "field.option_label", "field.name")}
                            placeholder={label(pageContract, "field.option_label", "field.name")}
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
                            aria-label={label(pageContract, "field.option_value", "field.name")}
                            placeholder={label(pageContract, "field.option_value", "field.name")}
                            onChange={(e) => {
                              const options = (question.options ?? []).map((item, i) => (i === optionIndex ? { ...item, value: cleanQuestionId(e.target.value) } : item));
                              updateQuestion(question.key, { options });
                            }}
                          />
                          <button
                            type="button"
                            className="iconbtn"
                            aria-label={label(pageContract, "action.remove_option", "action.close")}
                            title={label(pageContract, "action.remove_option", "action.close")}
                            onClick={() => updateQuestion(question.key, { options: (question.options ?? []).filter((_, i) => i !== optionIndex) })}
                          >
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
                  <div className="prt-question-ft">
                    <label className="prt-check">
                      <input type="checkbox" checked={question.required} onChange={(e) => updateQuestion(question.key, { required: e.target.checked })} />
                      <span>{label(pageContract, "field.question_required", "field.presence")}</span>
                    </label>
                    {/* Per-question proof (maintainer instruction 2026-09-18): what capture this
                        question needs to count as answered, and one or several. "none" is the
                        catalog's own key for no proof; it never travels. */}
                    {proofChoosable ? (
                      <span className="prt-proof">
                        <select
                          aria-label={label(pageContract, "field.question_proof", "field.photo")}
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
                            aria-label={label(pageContract, "field.question_proof_count", "field.max")}
                            value={question.proof.count}
                            onChange={(e) => updateQuestion(question.key, { proof: { kind: question.proof!.kind, count: e.target.value as NonNullable<QuestionDraft["proof"]>["count"] } })}
                          >
                            {kinds(catalog?.question_proof_counts).map((option) => (
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
                    <summary>{label(pageContract, "field.question_id", "field.name")}</summary>
                    <input
                      id={`pr-q-id-${question.key}`}
                      aria-label={label(pageContract, "field.question_id", "field.name")}
                      value={question.id}
                      required
                      onChange={(e) => updateQuestion(question.key, { id: cleanQuestionId(e.target.value), idTouched: true })}
                    />
                  </details>
                </div>
              ))}
            </div>
            {draft.questions.length < LIMITS.questionsMax ? (
              <button type="button" className="btn sm prt-add" onClick={addQuestion}>
                <Plus className="ic" aria-hidden="true" /> {label(pageContract, "action.add_question", "field.questions")}
              </button>
            ) : null}

            <div className="prt-row2 prt-media">
              {(["photo", "video"] as const).map((kind) => (
                <div key={kind} className="prt-media-box">
                  <span className="prt-sub">{field(kind)}</span>
                  <div className="prt-row2">
                    <div className="fld">
                      <label htmlFor={`pr-${kind}-min`}>{field("min")}</label>
                      {countField(`pr-${kind}-min`, draft[kind].min, (min) => update({ [kind]: { ...draft[kind], min } } as Partial<Draft>))}
                    </div>
                    <div className="fld">
                      <label htmlFor={`pr-${kind}-max`}>{field("max")}</label>
                      {countField(`pr-${kind}-max`, draft[kind].max, (max) => update({ [kind]: { ...draft[kind], max } } as Partial<Draft>))}
                    </div>
                  </div>
                </div>
              ))}
            </div>
            <span className="prt-sub">{field("presence")}</span>
            <ChoiceTiles name="presence_kind" options={kinds(catalog?.presence_kinds)} value={draft.presence} onChange={(key) => update({ presence: key as Draft["presence"] })} />
          </Step>

          {/* 6. Review: what a submit does. */}
          <Step index={6} title={field("review")}>
            <ChoiceTiles name="review_kind" options={kinds(catalog?.review_kinds)} value={draft.reviewKind} onChange={(key) => update({ reviewKind: key as Draft["reviewKind"] })} />
          </Step>
        </fieldset>

        {/* The outcome sentence also shows in the footer beside Save; this copy keeps it next to the
            fields for a reader who scrolled up to fix one. */}
        {message && state.status === "error" ? (
          <div className="prt-outcome error" role="alert">
            {message}
          </div>
        ) : null}
      </form>

      {/* Status: pause / resume / retire. Open tasks are untouched by any of these. Retire is
          permanent, so it asks once -- in place, as two buttons, never the browser's own box. */}
      {isEdit && canSetStatus && routine.status !== "retired" ? (
        <form action={statusFormAction} aria-busy={statusPending} className="prt-status">
          <input type="hidden" name="routine_id" value={routine.routine_id} />
          <input type="hidden" name="row_version" value={routine.row_version} />
          <Tag tone={routine.status === "active" ? "ok" : "warn"}>{routine.status_label}</Tag>
          {confirmRetire ? (
            <>
              <span className="prt-confirm">{copy(pageContract, "action.retire.confirm")}</span>
              <button type="submit" name="status" value="retired" className="btn sm danger" disabled={statusPending}>
                {copy(pageContract, "action.retire")}
              </button>
              <button type="button" className="btn sm" onClick={() => setConfirmRetire(false)} disabled={statusPending}>
                {label(pageContract, "action.retire.keep", "action.close")}
              </button>
            </>
          ) : (
            <>
              {routine.status === "active" ? (
                <button type="submit" name="status" value="paused" className="btn sm" disabled={statusPending}>
                  {copy(pageContract, "action.pause")}
                </button>
              ) : (
                <button type="submit" name="status" value="active" className="btn sm" disabled={statusPending}>
                  {copy(pageContract, "action.resume")}
                </button>
              )}
              <button type="button" className="btn sm" onClick={() => setConfirmRetire(true)} disabled={statusPending}>
                {copy(pageContract, "action.retire")}
              </button>
            </>
          )}
          {statusMessage ? (
            <span role="status" className={statusState.status === "success" ? "prt-outcome ok" : "prt-outcome error"}>
              {statusMessage}
            </span>
          ) : null}
        </form>
      ) : null}

      {/* The footer's Save lives outside the scroll; this publishes the form's pending state and
          outcome to it (see RoutineSaveFooter). */}
      <SaveStateBridge formId={formId} pending={pending} message={message} tone={state.status} />
    </div>
  );
}

type SaveView = { pending: boolean; message: string; tone: PenRoutineActionState["status"] };
const IDLE_VIEW: SaveView = { pending: false, message: "", tone: "idle" };

/**
 * The footer Save is rendered by the page in the drawer's footer slot, a sibling of this form in the
 * React tree. It submits through `form=` and reads the form's pending state and outcome from this
 * small store, keyed by form id, so the two stay one form without lifting the action state into the
 * server component that builds the drawer items.
 */
const saveViews = new Map<string, SaveView>();
const saveListeners = new Set<() => void>();

function publishSaveView(formId: string, view: SaveView): void {
  const current = saveViews.get(formId);
  if (current && current.pending === view.pending && current.message === view.message && current.tone === view.tone) return;
  saveViews.set(formId, view);
  for (const listener of saveListeners) listener();
}

function subscribeSaveViews(listener: () => void): () => void {
  saveListeners.add(listener);
  return () => saveListeners.delete(listener);
}

function SaveStateBridge({ formId, pending, message, tone }: { formId: string } & SaveView) {
  useEffect(() => {
    publishSaveView(formId, { pending, message, tone });
  }, [formId, pending, message, tone]);
  return null;
}

/** The drawer footer's Save: a submit button that names the routine form, plus the outcome sentence. */
export function RoutineSaveFooter({ formId, saveLabel, canSave }: { formId: string; saveLabel: string; canSave: boolean }) {
  const view = useSyncExternalStore(
    subscribeSaveViews,
    () => saveViews.get(formId) ?? IDLE_VIEW,
    () => IDLE_VIEW,
  );
  if (!canSave) return null;
  return (
    <div className="prt-foot">
      <button type="submit" form={formId} className="btn primary" disabled={view.pending}>
        {view.pending ? <span className="prt-spin" aria-hidden="true" /> : <Check className="ic" aria-hidden="true" />}
        {saveLabel}
      </button>
      {view.message ? (
        <span role="status" className={view.tone === "success" ? "prt-outcome ok" : "prt-outcome error"}>
          {view.message}
        </span>
      ) : null}
    </div>
  );
}
