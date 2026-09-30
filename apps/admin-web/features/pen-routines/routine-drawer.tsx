"use client";

import { Iconify } from "@/components/minimal/iconify";
import { ASSIGNEE_FIELD_SX } from "@/components/app/assignee-field-sx";
import { useRouter } from "next/navigation";
import { startTransition, useActionState, useEffect, useId, useMemo, useRef, useState, useSyncExternalStore } from "react";

import Accordion from "@mui/material/Accordion";
import AccordionDetails from "@mui/material/AccordionDetails";
import AccordionSummary from "@mui/material/AccordionSummary";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import Checkbox from "@mui/material/Checkbox";
import Divider from "@mui/material/Divider";
import FormControlLabel from "@mui/material/FormControlLabel";
import FormGroup from "@mui/material/FormGroup";
import IconButton from "@mui/material/IconButton";
import InputAdornment from "@mui/material/InputAdornment";
import Radio from "@mui/material/Radio";
import RadioGroup from "@mui/material/RadioGroup";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import ToggleButton from "@mui/material/ToggleButton";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import Typography from "@mui/material/Typography";
import { TAP_MIN } from "@/components/app/tap";
import { Label } from "@/components/minimal/label";
import { AssigneePicker } from "@/components/assignee-picker";
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
  /** The ONE person the routine is for ("" until chosen). */
  assigneeUserId: string;
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

// Checkbox rows keep a 44px tap target on a phone (WebView rule) without the template's
// negative margin pulling the box outside the form column.
const CHECK_ROW_SX = { m: 0, minHeight: TAP_MIN, "& .MuiFormControlLabel-label": { typography: "body2" } } as const;
const TWO_COL_SX = { display: "grid", gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))" }, gap: 2, alignItems: "start" } as const;

/** One numbered step of the form: template Label number + subtitle heading, the step's fields beneath. */
function Step({ index, title, hint, children, aside }: { index: number; title: string; hint?: string; children: React.ReactNode; aside?: React.ReactNode }) {
  const headingId = useId();
  return (
    <Stack component="section" aria-labelledby={headingId} spacing={2}>
      <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }}>
        <Label color="primary" variant="soft" aria-hidden="true">
          {index}
        </Label>
        <Typography id={headingId} component="h4" variant="subtitle1" sx={{ flexGrow: 1, minWidth: 0 }}>
          {title}
        </Typography>
        {aside ? (
          <Typography variant="caption" sx={{ color: "text.secondary" }}>
            {aside}
          </Typography>
        ) : null}
      </Stack>
      {hint ? (
        <Typography variant="body2" sx={{ color: "text.secondary", mt: -1 }}>
          {hint}
        </Typography>
      ) : null}
      {children}
    </Stack>
  );
}

/** A labelled group of controls inside a step (template form-row caption above its controls). */
function FieldGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Stack spacing={1}>
      <Typography variant="overline" sx={{ color: "text.secondary" }}>
        {title}
      </Typography>
      {children}
    </Stack>
  );
}

/** A choice between a closed vocabulary's options: a template RadioGroup drawn as bordered tiles. */
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
    <RadioGroup
      name={name}
      value={value}
      onChange={(_event, key) => onChange(key)}
      sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))" }, gap: 1 }}
    >
      {options.map((option) => {
        const off = isDisabled?.(option.key) ?? false;
        const on = value === option.key;
        return (
          <FormControlLabel
            key={option.key}
            value={option.key}
            disabled={off}
            control={<Radio size="small" />}
            label={option.label}
            sx={{
              m: 0,
              pr: 1.5,
              minHeight: TAP_MIN,
              border: 1,
              borderRadius: "var(--r-md)",
              borderColor: on ? "primary.main" : "divider",
              bgcolor: on ? "action.selected" : "transparent",
              "& .MuiFormControlLabel-label": { typography: "body2" },
            }}
          />
        );
      })}
    </RadioGroup>
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

  const countField = (id: string, fieldLabel: string, value: number, onValue: (next: number) => void) => (
    <TextField
      id={id}
      label={fieldLabel}
      type="number"
      fullWidth
      value={value}
      onChange={(e) => onValue(Number.parseInt(e.target.value, 10) || 0)}
      slotProps={{ htmlInput: { inputMode: "numeric", min: 0, max: LIMITS.proofMax } }}
    />
  );

  // Template drawer form: numbered steps (Label + subtitle heading, dashed dividers between them),
  // MUI TextFields with their own labels, RadioGroup tiles and FormControlLabel checkbox lists.
  const labelShrink = { inputLabel: { shrink: true } } as const;
  return (
    // The shared single-person AssigneePicker is drawn as a template outlined field (ASSIGNEE_FIELD_SX).
    <Stack spacing={3} sx={ASSIGNEE_FIELD_SX}>
      <form
        id={formId}
        aria-busy={pending}
        className="pen-routine-form"
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
        <input type="hidden" name={FORM_JSON_FIELDS.evidence} value={JSON.stringify(evidenceBody(draft))} />
        <input type="hidden" name="occupied_only" value={draft.occupiedOnly ? "on" : "off"} />

        <Stack spacing={1.5} sx={{ mb: isEdit || readOnly ? 3 : 0 }}>
          {isEdit ? <Alert severity="info">{copy(pageContract, "hint.versions")}</Alert> : null}
          {readOnly ? <Alert severity="info">{copy(pageContract, "configure.disabled_no_access")}</Alert> : null}
        </Stack>

        <Stack component="fieldset" disabled={readOnly} spacing={3} divider={<Divider flexItem sx={{ borderStyle: "dashed" }} />} sx={{ border: 0, m: 0, p: 0, minWidth: 0 }}>
          {/* 1. Details: name, instruction, the ONE park it belongs to. */}
          <Step index={1} title={label(pageContract, "section.details", "drawer.routine.title")}>
            <TextField id="pr-name" name="name" label={field("name")} fullWidth required value={draft.name} onChange={(e) => update({ name: e.target.value })} slotProps={{ htmlInput: { maxLength: LIMITS.nameMax } }} />
            <TextField id="pr-instruction" name="instruction" label={field("instruction")} fullWidth multiline minRows={2} value={draft.instruction} onChange={(e) => update({ instruction: e.target.value })} />
            {isEdit ? (
              <>
                <input type="hidden" name="park_id" value={routine.park_id} />
                <TextField id="pr-park" label={field("park")} fullWidth value={routine.park_name} disabled slotProps={{ htmlInput: { readOnly: true } }} />
              </>
            ) : (
              <TextField
                select
                fullWidth
                id="pr-park"
                name="park_id"
                label={field("park")}
                value={draft.parkId}
                slotProps={{ ...labelShrink, select: { native: true } }}
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
              </TextField>
            )}
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
              <Alert severity="warning" role="status">
                {label(pageContract, "assignee.unavailable", "empty.role_people")}
              </Alert>
            ) : null}
            {!owners.length ? <Alert severity="warning">{copy(pageContract, "empty.role_people")}</Alert> : null}
          </Step>

          {/* 3. Pens: every pen, a ticked list from the partition catalog, or ONE task for the whole park. */}
          <Step index={3} title={field("scope")} aside={parkName}>
            <ChoiceTiles name="scope_kind" options={kinds(catalog?.scope_kinds)} value={draft.scopeKind} onChange={(key) => chooseScope(key as ScopeKind)} />
            {draft.scopeKind === "all_pens" ? (
              <Stack direction="row" sx={{ alignItems: "center", flexWrap: "wrap", columnGap: 1 }}>
                <FormControlLabel
                  sx={CHECK_ROW_SX}
                  control={<Checkbox size="small" checked={draft.occupiedOnly} onChange={(e) => update({ occupiedOnly: e.target.checked })} />}
                  label={field("occupied_only")}
                />
                <Typography variant="caption" sx={{ color: "text.secondary" }}>
                  {sentence(pageContract, "count.pens_occupied", { occupied: occupiedCount, total: pens.length }, `${occupiedCount} / ${pens.length}`)}
                </Typography>
              </Stack>
            ) : null}
            {draft.scopeKind === "selected_pens" ? (
              <Stack spacing={1.5}>
                <Stack direction="row" sx={{ alignItems: "center", flexWrap: "wrap", gap: 1 }}>
                  <TextField
                    type="search"
                    size="small"
                    value={penQuery}
                    placeholder={label(pageContract, "filter.pens_search", "field.scope")}
                    onChange={(e) => setPenQuery(e.target.value)}
                    sx={{ flex: "1 1 180px", minWidth: 0 }}
                    slotProps={{
                      htmlInput: { "aria-label": label(pageContract, "filter.pens_search", "field.scope") },
                      input: {
                        startAdornment: (
                          <InputAdornment position="start">
                            <Iconify icon="eva:search-fill" aria-hidden="true" sx={{ color: "text.disabled" }} />
                          </InputAdornment>
                        ),
                      },
                    }}
                  />
                  <Button
                    type="button"
                    size="small"
                    variant="outlined"
                    color="inherit"
                    onClick={() => update({ pens: [...new Set([...draft.pens, ...shownPens.map(penKey)])] })}
                    disabled={!shownPens.length}
                  >
                    {label(pageContract, "action.select_all_pens", "filter.park.all")}
                  </Button>
                  <Button
                    type="button"
                    size="small"
                    variant="outlined"
                    color="inherit"
                    onClick={() => update({ pens: draft.pens.filter((key) => !shownPens.some((pen) => penKey(pen) === key)) })}
                    disabled={!draft.pens.length}
                  >
                    {label(pageContract, "action.clear_pens", "action.close")}
                  </Button>
                  <Typography variant="caption" sx={{ color: "text.secondary" }}>
                    {sentence(pageContract, "count.pens_chosen", { chosen: draft.pens.length, total: pens.length }, `${draft.pens.length} / ${pens.length}`)}
                  </Typography>
                </Stack>
                <FormGroup sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))" }, columnGap: 1 }}>
                  {shownPens.map((pen) => {
                    const key = penKey(pen);
                    const on = draft.pens.includes(key);
                    return (
                      <FormControlLabel
                        key={key}
                        sx={CHECK_ROW_SX}
                        control={<Checkbox size="small" checked={on} onChange={() => update({ pens: toggle(draft.pens, key) })} />}
                        label={
                          <>
                            {/* Backend-composed pen label, rendered verbatim: "Castro 2", "Godel 1 - Part 3". */}
                            {pen.operational_location_display}
                            {pen.occupied ? null : (
                              <Typography component="span" variant="caption" sx={{ ml: 1, color: "text.disabled" }}>
                                {copy(pageContract, "label.pen_empty", "")}
                              </Typography>
                            )}
                          </>
                        }
                      />
                    );
                  })}
                </FormGroup>
                {!shownPens.length ? (
                  <Typography variant="body2" sx={{ color: "text.secondary" }}>
                    {label(pageContract, "empty.pens_search", "empty.tasks")}
                  </Typography>
                ) : null}
              </Stack>
            ) : null}
            {draft.scopeKind === "park" ? (
              <Typography variant="body2" sx={{ color: "text.secondary" }}>
                {copy(pageContract, "hint.park_scope")}
              </Typography>
            ) : null}
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
              <FieldGroup title={field("weekdays")}>
                <FormGroup row aria-label={field("weekdays")} sx={{ columnGap: 1 }}>
                  {weekdays.map((name, index) => {
                    const day = index + 1;
                    const on = draft.weekdays.includes(day);
                    return (
                      <FormControlLabel
                        key={day}
                        sx={CHECK_ROW_SX}
                        control={<Checkbox size="small" checked={on} onChange={() => update({ weekdays: toggleNumber(draft.weekdays, day) })} />}
                        label={name}
                      />
                    );
                  })}
                </FormGroup>
              </FieldGroup>
            ) : null}
            {draft.cadenceKind === "monthly" ? (
              <FieldGroup title={field("month_days")}>
                {/* Template ToggleButtonGroup (multi-select): 31 day buttons in a 7-column grid. */}
                <ToggleButtonGroup
                  value={draft.monthDays}
                  onChange={(_event, days: number[]) => update({ monthDays: [...days].sort((a, b) => a - b) })}
                  aria-label={field("month_days")}
                  size="small"
                  sx={{
                    display: "grid",
                    gridTemplateColumns: "repeat(7, minmax(0, 1fr))",
                    gap: 0.5,
                    border: 0,
                    "& .MuiToggleButton-root": { minWidth: 0, minHeight: TAP_MIN, border: 1, borderColor: "divider", borderRadius: "var(--r-md)", m: 0 },
                  }}
                >
                  {Array.from({ length: 31 }, (_, index) => index + 1).map((day) => (
                    <ToggleButton key={day} value={day}>
                      {day}
                    </ToggleButton>
                  ))}
                </ToggleButtonGroup>
              </FieldGroup>
            ) : null}
            {draft.cadenceKind === "every_n_days" ? (
              <TextField
                id="pr-interval"
                name="interval_days"
                type="number"
                label={field("interval_days")}
                required
                value={draft.intervalDays}
                onChange={(e) => update({ intervalDays: e.target.value })}
                sx={{ maxWidth: { sm: 240 } }}
                slotProps={{ htmlInput: { inputMode: "numeric", min: LIMITS.intervalMin, max: LIMITS.intervalMax } }}
              />
            ) : null}
            {draft.cadenceKind === "after_work" ? (
              <>
                <FieldGroup title={field("after_work_kinds")}>
                  <FormGroup row aria-label={field("after_work_kinds")} sx={{ columnGap: 1 }}>
                    {kinds(catalog?.work_kinds).map((option) => {
                      const on = draft.afterWorkKinds.includes(option.key);
                      return (
                        <FormControlLabel
                          key={option.key}
                          sx={CHECK_ROW_SX}
                          control={<Checkbox size="small" checked={on} onChange={() => update({ afterWorkKinds: toggle(draft.afterWorkKinds, option.key) })} />}
                          label={option.label}
                        />
                      );
                    })}
                  </FormGroup>
                </FieldGroup>
                <TextField
                  id="pr-due-offset"
                  name="due_offset_days"
                  type="number"
                  label={field("due_offset_days")}
                  value={draft.dueOffsetDays}
                  onChange={(e) => update({ dueOffsetDays: e.target.value })}
                  sx={{ maxWidth: { sm: 240 } }}
                  slotProps={{ htmlInput: { inputMode: "numeric", min: LIMITS.dueOffsetMin, max: LIMITS.dueOffsetMax } }}
                />
              </>
            ) : (
              // A calendar cadence keeps a stored offset untouched; a blank one lets the backend default apply.
              <input type="hidden" name="due_offset_days" value={draft.dueOffsetDays} />
            )}
            <Box sx={TWO_COL_SX}>
              <FieldGroup title={field("start_date")}>
              <ThemedDatePicker
                name="start_date"
                label={field("start_date")}
                value={draft.startDate}
                onChange={(key) => update({ startDate: key })}
                previousMonthLabel={label(pageContract, "date.previous_month", "action.previous")}
                nextMonthLabel={label(pageContract, "date.next_month", "action.next")}
                invalidDateText={label(pageContract, "date.invalid", "action.error_form")}
              />
              </FieldGroup>
              <TextField
                select
                fullWidth
                id="pr-notify-time"
                name="notify_time"
                label={field("notify_time")}
                value={draft.notifyTime.slice(0, 5)}
                onChange={(e) => update({ notifyTime: e.target.value })}
                slotProps={{ ...labelShrink, select: { native: true } }}
              >
                {times.map((time) => (
                  <option key={time} value={time}>
                    {time}
                  </option>
                ))}
              </TextField>
            </Box>
          </Step>

          {/* 5. What to record: the questions, the capture counts, the pen check-in. */}
          <Step index={5} title={label(pageContract, "section.capture", "field.questions")}>
            <FieldGroup title={field("questions")}>
              {draft.questions.map((question, index) => (
                <Card key={question.key} variant="outlined" sx={{ p: 2, display: "flex", flexDirection: "column", gap: 2 }}>
                  <Stack direction="row" sx={{ alignItems: "center", justifyContent: "space-between" }}>
                    <Label color="default" variant="soft" aria-hidden="true">
                      {index + 1}
                    </Label>
                    <IconButton
                      type="button"
                      size="small"
                      aria-label={`${label(pageContract, "action.remove_question", "action.close")} ${index + 1}`}
                      title={label(pageContract, "action.remove_question", "action.close")}
                      onClick={() => removeQuestion(question.key)}
                    >
                      <Iconify icon="solar:trash-bin-trash-bold" aria-hidden="true" />
                    </IconButton>
                  </Stack>
                  <TextField
                    id={`pr-q-title-${question.key}`}
                    label={label(pageContract, "field.question_title", "field.name")}
                    fullWidth
                    value={question.title}
                    required
                    onChange={(e) =>
                      updateQuestion(question.key, { title: e.target.value, id: question.idTouched ? question.id : slugQuestionId(e.target.value) })
                    }
                  />
                  <Box sx={TWO_COL_SX}>
                    <TextField
                      select
                      fullWidth
                      id={`pr-q-kind-${question.key}`}
                      label={label(pageContract, "field.question_kind", "field.questions")}
                      value={question.kind}
                      onChange={(e) => updateQuestion(question.key, { kind: e.target.value as QuestionDraft["kind"] })}
                      slotProps={{ ...labelShrink, select: { native: true } }}
                    >
                      {kinds(catalog?.question_kinds).map((option) => (
                        <option key={option.key} value={option.key}>
                          {option.label}
                        </option>
                      ))}
                    </TextField>
                    <TextField
                      id={`pr-q-hint-${question.key}`}
                      label={label(pageContract, "field.question_hint", "field.instruction")}
                      fullWidth
                      value={question.hint ?? ""}
                      onChange={(e) => updateQuestion(question.key, { hint: e.target.value })}
                    />
                  </Box>
                  {question.kind === "number" ? (
                    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: "repeat(3, minmax(0, 1fr))" }, gap: 2 }}>
                      <TextField id={`pr-q-min-${question.key}`} label={field("min")} type="number" fullWidth value={question.min ?? ""} onChange={(e) => updateQuestion(question.key, { min: e.target.value === "" ? null : Number(e.target.value) })} />
                      <TextField id={`pr-q-max-${question.key}`} label={field("max")} type="number" fullWidth value={question.max ?? ""} onChange={(e) => updateQuestion(question.key, { max: e.target.value === "" ? null : Number(e.target.value) })} />
                      <TextField id={`pr-q-unit-${question.key}`} label={label(pageContract, "field.question_unit", "field.questions")} fullWidth value={question.unit ?? ""} onChange={(e) => updateQuestion(question.key, { unit: e.target.value })} />
                    </Box>
                  ) : null}
                  {question.kind === "choice" || question.kind === "multi_choice" ? (
                    <FieldGroup title={label(pageContract, "field.question_options", "field.questions")}>
                      {(question.options ?? []).map((option, optionIndex) => (
                        <Stack key={optionIndex} direction="row" spacing={1} sx={{ alignItems: "center" }}>
                          <TextField
                            size="small"
                            fullWidth
                            value={option.label}
                            placeholder={label(pageContract, "field.option_label", "field.name")}
                            slotProps={{ htmlInput: { "aria-label": label(pageContract, "field.option_label", "field.name") } }}
                            onChange={(e) => {
                              // The stored value follows the label until someone edits it by hand,
                              // the same way a question's key follows its title.
                              const options = (question.options ?? []).map((item, i) =>
                                i === optionIndex ? { label: e.target.value, value: item.value && item.value !== slugQuestionId(item.label) ? item.value : slugQuestionId(e.target.value) } : item,
                              );
                              updateQuestion(question.key, { options });
                            }}
                          />
                          <TextField
                            size="small"
                            fullWidth
                            value={option.value}
                            placeholder={label(pageContract, "field.option_value", "field.name")}
                            slotProps={{ htmlInput: { "aria-label": label(pageContract, "field.option_value", "field.name"), style: { fontFamily: "monospace" } } }}
                            onChange={(e) => {
                              const options = (question.options ?? []).map((item, i) => (i === optionIndex ? { ...item, value: cleanQuestionId(e.target.value) } : item));
                              updateQuestion(question.key, { options });
                            }}
                          />
                          <IconButton
                            type="button"
                            size="small"
                            aria-label={label(pageContract, "action.remove_option", "action.close")}
                            title={label(pageContract, "action.remove_option", "action.close")}
                            onClick={() => updateQuestion(question.key, { options: (question.options ?? []).filter((_, i) => i !== optionIndex) })}
                          >
                            <Iconify icon="solar:trash-bin-trash-bold" aria-hidden="true" />
                          </IconButton>
                        </Stack>
                      ))}
                      {(question.options ?? []).length < LIMITS.optionsMax ? (
                        <Box>
                          <Button
                            type="button"
                            size="small"
                            variant="outlined"
                            color="inherit"
                            startIcon={<Iconify icon="mingcute:add-line" aria-hidden="true" />}
                            onClick={() => updateQuestion(question.key, { options: [...(question.options ?? []), { value: "", label: "" }] })}
                          >
                            {label(pageContract, "action.add_option", "field.questions")}
                          </Button>
                        </Box>
                      ) : null}
                    </FieldGroup>
                  ) : null}
                  <Stack direction="row" sx={{ alignItems: "center", flexWrap: "wrap", gap: 1.5 }}>
                    <FormControlLabel
                      sx={CHECK_ROW_SX}
                      control={<Checkbox size="small" checked={question.required} onChange={(e) => updateQuestion(question.key, { required: e.target.checked })} />}
                      label={label(pageContract, "field.question_required", "field.presence")}
                    />
                    {/* Per-question proof (maintainer instruction 2026-09-18): what capture this
                        question needs to count as answered, and one or several. "none" is the
                        catalog's own key for no proof; it never travels. */}
                    {proofChoosable ? (
                      <Stack direction="row" spacing={1} sx={{ ml: { sm: "auto" } }}>
                        <TextField
                          select
                          size="small"
                          slotProps={{ select: { native: true }, htmlInput: { "aria-label": label(pageContract, "field.question_proof", "field.photo") } }}
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
                        </TextField>
                        {question.proof ? (
                          <TextField
                            select
                            size="small"
                            slotProps={{ select: { native: true }, htmlInput: { "aria-label": label(pageContract, "field.question_proof_count", "field.max") } }}
                            value={question.proof.count}
                            onChange={(e) => updateQuestion(question.key, { proof: { kind: question.proof!.kind, count: e.target.value as NonNullable<QuestionDraft["proof"]>["count"] } })}
                          >
                            {kinds(catalog?.question_proof_counts).map((option) => (
                              <option key={option.key} value={option.key}>
                                {option.label}
                              </option>
                            ))}
                          </TextField>
                        ) : null}
                      </Stack>
                    ) : null}
                  </Stack>
                  {/* The key is what the phone's answer is stored under; it follows the title until
                      edited, so most people never need to touch it (template Accordion, closed). */}
                  <Accordion disableGutters elevation={0} sx={{ bgcolor: "transparent", "&::before": { display: "none" } }}>
                    <AccordionSummary expandIcon={<Iconify icon="eva:arrow-ios-downward-fill" aria-hidden="true" />} sx={{ px: 0, minHeight: TAP_MIN }}>
                      <Typography variant="body2" sx={{ color: "text.secondary" }}>
                        {label(pageContract, "field.question_id", "field.name")}
                      </Typography>
                    </AccordionSummary>
                    <AccordionDetails sx={{ px: 0 }}>
                      <TextField
                        id={`pr-q-id-${question.key}`}
                        size="small"
                        fullWidth
                        value={question.id}
                        required
                        slotProps={{ htmlInput: { "aria-label": label(pageContract, "field.question_id", "field.name"), style: { fontFamily: "monospace" } } }}
                        onChange={(e) => updateQuestion(question.key, { id: cleanQuestionId(e.target.value), idTouched: true })}
                      />
                    </AccordionDetails>
                  </Accordion>
                </Card>
              ))}
              {draft.questions.length < LIMITS.questionsMax ? (
                <Box>
                  <Button type="button" size="small" variant="outlined" color="inherit" startIcon={<Iconify icon="mingcute:add-line" aria-hidden="true" />} onClick={addQuestion}>
                    {label(pageContract, "action.add_question", "field.questions")}
                  </Button>
                </Box>
              ) : null}
            </FieldGroup>

            <Box sx={TWO_COL_SX}>
              {(["photo", "video"] as const).map((kind) => (
                <FieldGroup key={kind} title={field(kind)}>
                  <Box sx={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 2 }}>
                    {countField(`pr-${kind}-min`, field("min"), draft[kind].min, (min) => update({ [kind]: { ...draft[kind], min } } as Partial<Draft>))}
                    {countField(`pr-${kind}-max`, field("max"), draft[kind].max, (max) => update({ [kind]: { ...draft[kind], max } } as Partial<Draft>))}
                  </Box>
                </FieldGroup>
              ))}
            </Box>
            <FieldGroup title={field("presence")}>
              <ChoiceTiles name="presence_kind" options={kinds(catalog?.presence_kinds)} value={draft.presence} onChange={(key) => update({ presence: key as Draft["presence"] })} />
            </FieldGroup>
          </Step>

          {/* 6. Review: what a submit does. */}
          <Step index={6} title={field("review")}>
            <ChoiceTiles name="review_kind" options={kinds(catalog?.review_kinds)} value={draft.reviewKind} onChange={(key) => update({ reviewKind: key as Draft["reviewKind"] })} />
          </Step>
        </Stack>

        {/* The outcome sentence also shows in the footer beside Save; this copy keeps it next to the
            fields for a reader who scrolled up to fix one. */}
        {message && state.status === "error" ? (
          <Alert severity="error" role="alert" sx={{ mt: 3 }}>
            {message}
          </Alert>
        ) : null}
      </form>

      {/* Status: pause / resume / retire. Open tasks are untouched by any of these. Retire is
          permanent, so it asks once -- in place, as two buttons, never the browser's own box. */}
      {isEdit && canSetStatus && routine.status !== "retired" ? (
        <Stack component="form" action={statusFormAction} aria-busy={statusPending} direction="row" sx={{ alignItems: "center", flexWrap: "wrap", gap: 1, pt: 3, borderTop: 1, borderColor: "divider", borderTopStyle: "dashed" }}>
          <input type="hidden" name="routine_id" value={routine.routine_id} />
          <input type="hidden" name="row_version" value={routine.row_version} />
          <Tag tone={routine.status === "active" ? "ok" : "warn"}>{routine.status_label}</Tag>
          {confirmRetire ? (
            <>
              <Typography variant="body2" sx={{ color: "error.main" }}>{copy(pageContract, "action.retire.confirm")}</Typography>
              <Button key="retire-confirm" type="submit" name="status" value="retired" size="small" variant="contained" color="error" disabled={statusPending}>
                {copy(pageContract, "action.retire")}
              </Button>
              <Button key="retire-keep" type="button" size="small" variant="outlined" color="inherit" onClick={() => setConfirmRetire(false)} disabled={statusPending}>
                {label(pageContract, "action.retire.keep", "action.close")}
              </Button>
            </>
          ) : (
            <>
              {routine.status === "active" ? (
                <Button key="status-paused" type="submit" name="status" value="paused" size="small" variant="outlined" color="inherit" disabled={statusPending}>
                  {copy(pageContract, "action.pause")}
                </Button>
              ) : (
                <Button key="status-active" type="submit" name="status" value="active" size="small" variant="outlined" color="inherit" disabled={statusPending}>
                  {copy(pageContract, "action.resume")}
                </Button>
              )}
              {/* Distinct keys: without them React reuses this node as the confirm's submit button while
                  the click is still being handled, and the browser then submits it -- one click
                  would retire the routine. */}
              <Button key="retire-ask" type="button" size="small" variant="outlined" color="inherit" onClick={() => setConfirmRetire(true)} disabled={statusPending}>
                {copy(pageContract, "action.retire")}
              </Button>
            </>
          )}
          {statusMessage ? (
            <Typography role="status" variant="body2" sx={{ color: statusState.status === "success" ? "success.main" : "error.main" }}>
              {statusMessage}
            </Typography>
          ) : null}
        </Stack>
      ) : null}

      {/* The footer's Save lives outside the scroll; this publishes the form's pending state and
          outcome to it (see RoutineSaveFooter). */}
      <SaveStateBridge formId={formId} pending={pending} message={message} tone={state.status} />
    </Stack>
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
    <Stack direction="row" sx={{ alignItems: "center", flexWrap: "wrap", gap: 1.5, width: 1 }}>
      <Button
        type="submit"
        form={formId}
        variant="contained"
        color="primary"
        loading={view.pending}
        loadingPosition="start"
        startIcon={<Iconify icon="eva:checkmark-fill" aria-hidden="true" />}
      >
        {saveLabel}
      </Button>
      {view.message ? (
        <Typography role="status" variant="body2" sx={{ color: view.tone === "success" ? "success.main" : "error.main" }}>
          {view.message}
        </Typography>
      ) : null}
    </Stack>
  );
}
