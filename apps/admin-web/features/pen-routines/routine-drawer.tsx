"use client";

import { Check, Search } from "lucide-react";
import { useRouter } from "next/navigation";
import { startTransition, useActionState, useEffect, useRef, useState, useSyncExternalStore } from "react";

import { AssigneePicker } from "@/components/assignee-picker";
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
  evidenceBodyFromDraft,
  FORM_JSON_FIELDS,
  LIMITS,
  penKey,
  type CadenceDraft,
  type CadenceKind,
  type EvidenceDraft,
  type ScopeKind,
} from "./routine-form-model";
import { CadenceFields, ChoiceTiles, EvidenceFields, Step, toggle, type RoutineWords } from "./routine-sections";

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

type Draft = CadenceDraft &
  EvidenceDraft & {
    parkId: string;
    name: string;
    instruction: string;
    scopeKind: ScopeKind;
    occupiedOnly: boolean;
    pens: string[];
    reviewKind: "verifier" | "none";
    /** The ONE person the routine is for ("" until chosen). */
    assigneeUserId: string;
  };

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
      assigneeUserId: "",
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
    assigneeUserId: routine.assignee?.user_id ?? "",
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

/** Closes the overlay the same way the drawer's own X does: Back when the entry is local, else replace. */
function closeOverlay(listHref: string): void {
  if (currentHistoryEntryIsLocalOverlay()) {
    window.history.back();
    return;
  }
  replaceLocalOverlayUrl(listHref);
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
  managedNote,
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
  /**
   * Set when the routine comes from a module SOP (`sop_code`): it is read-only here and this note,
   * linking to that SOP page, replaces the no-access note (docs/decisions/simple-task-phone-tabs.md).
   */
  managedNote?: React.ReactNode;
}) {
  const isEdit = routine !== undefined;
  const router = useRouter();
  const [draft, setDraft] = useState<Draft>(() => draftFrom(routine, catalogParkId, catalog));
  const [state, formAction, pending] = useActionState(isEdit ? updateRoutineAction : createRoutineAction, INITIAL);
  const [statusState, statusFormAction, statusPending] = useActionState(setRoutineStatusAction, INITIAL);
  const [confirmRetire, setConfirmRetire] = useState(false);
  const [penQuery, setPenQuery] = useState("");

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
  const update = (patch: Partial<Draft>) => setDraft((current) => ({ ...current, ...patch }));
  // The shared schedule / evidence sections read the same /routines contract keys (routine-sections).
  const words: RoutineWords = (key, fallbackKey) => (fallbackKey ? label(pageContract, key, fallbackKey) : copy(pageContract, key));

  const pens: PenRoutineCatalogPen[] = catalog?.pens ?? [];
  // "Who does it" is one person, picked by name like a task (maintainer decision 2026-09-26).
  // The catalog lists everyone who may do routines at this park, each once with their titles; a
  // park head appears only for their own park.
  const people = catalog?.people ?? [];
  const owners = people.map((person) => ({ id: person.user_id, name: person.display_name, title: person.title }));
  // A saved person the catalog no longer lists has lost the role here: the routine raises nothing
  // until someone else is chosen, and the drawer says so.
  const assigneeGone = Boolean(draft.assigneeUserId) && !people.some((person) => person.user_id === draft.assigneeUserId);
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


  return (
    <div className="prt">
      <form
        id={formId}
        aria-busy={pending}
        className="pen-routine-form prt-form"
        onSubmit={(event) => {
          // Submitted through a transition rather than `action=`: React resets a form after its
          // action runs, and the reset snapped the Park select back to its first option (the drawer
          // read "Coimbatore" on a Channapatna routine after a refused save). The refusal is shown
          // beside the fields the reader typed, untouched.
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          startTransition(() => formAction(data));
        }}
      >
        {isEdit ? <input type="hidden" name="routine_id" value={routine.routine_id} /> : null}
        {isEdit ? <input type="hidden" name="row_version" value={routine.row_version} /> : null}
        <input type="hidden" name={FORM_JSON_FIELDS.pens} value={JSON.stringify(pens.filter((pen) => draft.pens.includes(penKey(pen))).map((pen) => ({ shed_id: pen.shed_id, partition_label: pen.partition_label })))} />
        <input type="hidden" name={FORM_JSON_FIELDS.weekdays} value={JSON.stringify(draft.weekdays)} />
        <input type="hidden" name={FORM_JSON_FIELDS.monthDays} value={JSON.stringify(draft.monthDays)} />
        <input type="hidden" name={FORM_JSON_FIELDS.afterWorkKinds} value={JSON.stringify(draft.afterWorkKinds)} />
        <input type="hidden" name={FORM_JSON_FIELDS.evidence} value={JSON.stringify(evidenceBodyFromDraft(draft))} />
        <input type="hidden" name="occupied_only" value={draft.occupiedOnly ? "on" : "off"} />

        {isEdit && !managedNote ? <div className="note">{copy(pageContract, "hint.versions")}</div> : null}
        {managedNote ? <div className="note prt-managed">{managedNote}</div> : null}
        {!managedNote && readOnly ? <div className="note">{copy(pageContract, "configure.disabled_no_access")}</div> : null}

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

          {/* 2. Who does it: ONE person, picked by name exactly like the Tasks "For" field. The
              picker posts assignee_user_id; the server derives the roles through which that person
              may do it at this park and refuses anyone who holds none there. */}
          <Step index={2} title={field("assignee_roles")} hint={copy(pageContract, "hint.assignee_roles")}>
            <AssigneePicker
              mode="single"
              name="assignee_user_id"
              labels={{
                label: field("assignee_roles"),
                search: label(pageContract, "assignee.search", "filter.pens_search"),
                none: label(pageContract, "assignee.none", "empty.role_people"),
                placeholder: label(pageContract, "assignee.placeholder", "field.assignee_roles"),
              }}
              owners={owners}
              selected={draft.assigneeUserId || undefined}
              onSelect={(next) => update({ assigneeUserId: next ?? "" })}
            />
            {assigneeGone ? (
              <p className="prt-hint prt-warn" role="status">
                {label(pageContract, "assignee.unavailable", "empty.role_people")}
              </p>
            ) : null}
            {!owners.length ? <p className="prt-hint prt-warn">{copy(pageContract, "empty.role_people")}</p> : null}
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
            <CadenceFields
              words={words}
              vocabulary={catalog}
              value={draft}
              onChange={update}
              onChooseCadence={chooseCadence}
              disableAfterWork={draft.scopeKind === "park"}
            />
          </Step>

          {/* 5. What to record: the questions, the capture counts, the pen check-in. */}
          <Step index={5} title={label(pageContract, "section.capture", "field.questions")}>
            <EvidenceFields words={words} vocabulary={catalog} value={draft} onChange={update} />
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
              <button key="retire-confirm" type="submit" name="status" value="retired" className="btn sm danger" disabled={statusPending}>
                {copy(pageContract, "action.retire")}
              </button>
              <button key="retire-keep" type="button" className="btn sm" onClick={() => setConfirmRetire(false)} disabled={statusPending}>
                {label(pageContract, "action.retire.keep", "action.close")}
              </button>
            </>
          ) : (
            <>
              {routine.status === "active" ? (
                <button key="status-paused" type="submit" name="status" value="paused" className="btn sm" disabled={statusPending}>
                  {copy(pageContract, "action.pause")}
                </button>
              ) : (
                <button key="status-active" type="submit" name="status" value="active" className="btn sm" disabled={statusPending}>
                  {copy(pageContract, "action.resume")}
                </button>
              )}
              {/* Distinct keys: without them React reuses this node as the confirm's submit button while
                  the click is still being handled, and the browser then submits it -- one click
                  would retire the routine. */}
              <button key="retire-ask" type="button" className="btn sm" onClick={() => setConfirmRetire(true)} disabled={statusPending}>
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

export function SaveStateBridge({ formId, pending, message, tone }: { formId: string } & SaveView) {
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
