"use client";

import { useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { Plus, Trash2 } from "lucide-react";

import { copy, optionalCopy, optionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { HealthConfigProtocolDetail, HealthConfigStep } from "@/lib/api/server";
import type { HealthConfigActionResult } from "./health-config-actions";
import { afterSubmit, CLOSED_STATE, openIntent, type AuthoringIdempotencyState } from "@/lib/authoring-idempotency";

// The authoring surface for one treatment protocol.
//
// WHY THE WHOLE DOCUMENT IS EDITED AT ONCE
//
// A protocol is read by an operator as one sequence, so the unit of save is "the document as the
// author last saw it", not a field. Per-field autosave would let a partially-applied edit exist —
// a step sitting on day 9 of a course that is now 7 days long — which is a protocol nobody can
// execute and which no screen would show as broken.
//
// WHY STEPS CARRY NO SEQUENCE NUMBER
//
// Order is positional here and the server assigns seq 1..N. `health_protocol_steps` is UNIQUE on
// (version, seq), so a client-driven renumber collides with itself halfway through a reorder.
// Sending order instead of numbers makes a reorder a single replacement that either commits whole
// or not at all.
//
// WHY NUMERIC INPUTS ARE UNCONTROLLED TEXT
//
// Same reason as Feed Config: a controlled `type="number"` is where a cleared box quietly becomes 0.
// Here that would author a dosage or a course length nobody typed. The fields hold exactly the
// characters the author left in them, and the server action decides what a blank means.

// Split across two aliases so the arrow type does not read as `> Promise<` — the contract-literal
// guard scans for `>text<` to catch visible JSX copy, and an inline generic return type trips it.
type ActionResult = Promise<HealthConfigActionResult>;
type SubmitAction = (formData: FormData) => ActionResult;

/** One editable step row. `key` is a stable client id so React does not reuse rows across edits. */
type DraftStep = {
  key: string;
  day_no: string;
  session: string;
  record_type: string;
  medicine_name: string;
  dosage_text: string;
  dosage_denominator: string;
  medicine_route: string;
  instruction: string;
  critical_action_type: string;
};

// The four authoring vocabularies come from the page contract's option groups, keys AND labels.
//
// They are not declared here on purpose. Session, step type, route, unit and critical-action type
// are the values the BACKEND validates against, so a list maintained in this file could drift from
// the set that is actually accepted and would then offer an author a choice that fails on save.
// Reading them from the contract means one source decides what a protocol may contain.

/** Renders one option list, preserving the backend's order (which is meaningful, not alphabetical). */
function OptionList({ options, includeBlank }: { options: AdminUiOption[]; includeBlank?: boolean }) {
  return (
    <>
      {includeBlank ? <option value="" /> : null}
      {options.map((choice) => (
        <option key={choice.key} value={choice.key} title={choice.title || undefined}>
          {choice.label}
        </option>
      ))}
    </>
  );
}

function stepFromContract(step: HealthConfigStep, index: number): DraftStep {
  return {
    key: `stored-${step.step_id ?? index}`,
    day_no: String(step.day_no ?? ""),
    session: step.session ?? "",
    record_type: step.record_type ?? "",
    medicine_name: step.medicine_name ?? "",
    dosage_text: step.dosage_text ?? "",
    dosage_denominator: step.dosage_denominator ?? "",
    medicine_route: step.medicine_route ?? "",
    instruction: step.instruction ?? "",
    critical_action_type: step.critical_action_type ?? "",
  };
}

function emptyStep(dayNo: number, key: string): DraftStep {
  return {
    key,
    day_no: String(dayNo),
    session: "morning",
    record_type: "medication",
    medicine_name: "",
    dosage_text: "",
    dosage_denominator: "",
    medicine_route: "",
    instruction: "",
    critical_action_type: "",
  };
}

/**
 * Serializes the editor's rows into the save payload.
 *
 * `day_no` is sent as a NUMBER when it parses and as 0 when it does not, so a non-numeric day is
 * rejected by the backend with a field error naming that row — rather than being dropped here,
 * which would silently remove a step from a medical document.
 */
function toPayload(steps: DraftStep[]) {
  return steps.map((step) => ({
    day_no: /^\d+$/.test(step.day_no) ? Number(step.day_no) : 0,
    session: step.session,
    record_type: step.record_type,
    medicine_name: step.medicine_name,
    dosage_text: step.dosage_text,
    dosage_denominator: step.dosage_denominator,
    medicine_route: step.medicine_route,
    instruction: step.instruction,
    critical_action_type: step.critical_action_type,
  }));
}

function FieldErrors({
  result,
  pageContract,
  prefix,
}: {
  result: HealthConfigActionResult | null;
  pageContract: AdminUiPageContract;
  prefix?: string;
}) {
  if (!result || result.ok) return null;
  const scoped = (result.fieldErrors ?? []).filter(
    (fieldError) => !prefix || fieldError.field.startsWith(prefix),
  );
  // `optionalCopy`, never `copy`, for the message: `copy()` THROWS on a key the contract does not
  // carry, and this component only renders when something already went wrong. A backend that
  // returns a code this build has no copy key for would then crash the whole screen with a runtime
  // error INSTEAD OF showing the failure -- turning a recoverable error into a white page. The
  // error renderer must be the one thing that cannot itself fail.
  const headline =
    optionalCopy(pageContract, result.messageKey) ?? copy(pageContract, "action.error_backend");
  return (
    <div className="alert" style={{ marginTop: 10 }}>
      <div>
        <b>{headline}</b>
        {result.detail ? <div className="small muted">{result.detail}</div> : null}
        {scoped.length > 0 ? (
          <ul className="small" style={{ margin: "6px 0 0", paddingLeft: 18, lineHeight: 1.6 }}>
            {scoped.map((fieldError) => (
              <li key={`${fieldError.field}:${fieldError.message}`}>
                <code>{fieldError.field}</code> — {fieldError.message}
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </div>
  );
}

/**
 * "Add disease" — opens drafts for BOTH age bands.
 *
 * `duration_days` is deliberately left blank by default rather than pre-filled with the backend's
 * default: a pre-filled number is a value the author appears to have chosen. Blank sends nothing
 * and lets the backend apply its declared default.
 */
export function AddDiseaseForm({
  pageContract,
  action,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  action: SubmitAction;
  enabled: boolean;
  disabledReason: string;
}) {
  const [pending, startTransition] = useTransition();
  const [result, setResult] = useState<HealthConfigActionResult | null>(null);
  const [idem, setIdem] = useState<AuthoringIdempotencyState>(CLOSED_STATE);

  if (!idem.open) {
    return (
      <button
        type="button"
        className="btn sm p"
        disabled={!enabled}
        aria-disabled={!enabled}
        title={enabled ? copy(pageContract, "note.both_bands") : disabledReason}
        onClick={() => {
          setResult(null);
          setIdem(openIntent(() => crypto.randomUUID()));
        }}
      >
        <Plus className="ic" aria-hidden="true" />
        {copy(pageContract, "action.add_disease")}
      </button>
    );
  }

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        const formData = new FormData(event.currentTarget);
        startTransition(async () => {
          const outcome = await action(formData);
          setResult(outcome);
          setIdem((prev) => afterSubmit(prev, outcome.ok, () => crypto.randomUUID()));
          if (outcome.ok) setIdem(CLOSED_STATE);
        });
      }}
      style={{ display: "flex", flexDirection: "column", gap: 8, minWidth: 280 }}
    >
      <input type="hidden" name="idempotency_key" value={idem.key ?? ""} />
      <label className="fld">
        <span>{copy(pageContract, "label.disease")}</span>
        <input name="display_name" type="text" autoFocus required maxLength={80} />
      </label>
      <label className="fld">
        <span>{copy(pageContract, "label.duration_days")}</span>
        <input name="duration_days" type="text" inputMode="numeric" placeholder="" />
      </label>
      <p className="small muted" style={{ margin: 0, lineHeight: 1.5 }}>
        {copy(pageContract, "note.both_bands")}
      </p>
      <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
        <button type="submit" className="btn sm p" disabled={pending}>
          {pending ? copy(pageContract, "state.loading") : copy(pageContract, "action.apply")}
        </button>
        <button type="button" className="btn sm" disabled={pending} onClick={() => setIdem(CLOSED_STATE)}>
          {copy(pageContract, "action.cancel")}
        </button>
      </div>
      <FieldErrors result={result} pageContract={pageContract} />
    </form>
  );
}

/** A one-button form that posts a single hidden value (open draft / publish / discard). */
export function ProtocolActionButton({
  pageContract,
  action,
  fields,
  labelKey,
  enabled,
  disabledReason,
  primary,
  confirmKey,
  navigateOnSuccess,
  basePath,
}: {
  pageContract: AdminUiPageContract;
  action: SubmitAction;
  fields: Record<string, string>;
  labelKey: string;
  enabled: boolean;
  disabledReason: string;
  primary?: boolean;
  /** When set, the button asks once before running. Used for publish and discard. */
  confirmKey?: string;
  /**
   * Where to go after a successful write. A serializable ENUM, not a callback: this component is
   * rendered from a Server Component, and React refuses to pass a function across that boundary.
   *
   *   "selected-version" -- select the version the write returned (Edit -> show the draft)
   *   "base"             -- deselect (Discard -> the version no longer exists)
   *
   * The navigation lives here rather than as a `redirect()` inside the server action, and that is
   * load-bearing: a server action invoked from an event handler inside startTransition swallows
   * NEXT_REDIRECT, so the action succeeded and the browser never moved. That was the "Edit opens a
   * draft but nothing appears" bug.
   */
  navigateOnSuccess?: "selected-version" | "base";
  /** The page path both navigation targets are built from. */
  basePath?: string;
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [result, setResult] = useState<HealthConfigActionResult | null>(null);
  const [armed, setArmed] = useState(false);
  const [idem] = useState(() => crypto.randomUUID());

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        const formData = new FormData(event.currentTarget);
        startTransition(async () => {
          const outcome = await action(formData);
          setResult(outcome);
          setArmed(false);
          if (outcome.ok && navigateOnSuccess && basePath) {
            if (navigateOnSuccess === "base") {
              router.push(basePath);
            } else if (outcome.versionId) {
              router.push(`${basePath}?hc_version=${encodeURIComponent(outcome.versionId)}`);
            }
          }
          // The version this screen is showing is GONE -- someone else published or discarded it,
          // or it was removed out from under this tab. Refresh so the server re-renders: the page
          // itself handles a missing version (see health-config.tsx) by dropping the dead section
          // and showing a plain notice above the catalog.
          //
          // A refresh, NOT a router.replace(basePath). Replacing to the same route is a soft
          // navigation that Next resolves without clearing the query string, so the stale
          // ?hc_version= survived and a reload resurrected the error. Letting the server own the
          // missing-version case means it also renders correctly on a cold load of that URL,
          // which no amount of client navigation can achieve.
          if (!outcome.ok && outcome.code === "not_found") {
            router.refresh();
          }
        });
      }}
      style={{ display: "inline-flex", flexDirection: "column", gap: 6 }}
    >
      {Object.entries(fields).map(([name, value]) => (
        <input key={name} type="hidden" name={name} value={value} />
      ))}
      <input type="hidden" name="idempotency_key" value={idem} />
      {confirmKey && !armed ? (
        <button
          type="button"
          className={primary ? "btn sm p" : "btn sm"}
          disabled={!enabled || pending}
          aria-disabled={!enabled}
          title={enabled ? undefined : disabledReason}
          onClick={() => setArmed(true)}
        >
          {copy(pageContract, labelKey)}
        </button>
      ) : (
        <span style={{ display: "inline-flex", gap: 6, alignItems: "center", flexWrap: "wrap" }}>
          <button
            type="submit"
            className={primary ? "btn sm p" : "btn sm"}
            disabled={!enabled || pending}
            aria-disabled={!enabled}
            title={enabled ? undefined : disabledReason}
          >
            {pending ? copy(pageContract, "state.loading") : copy(pageContract, labelKey)}
          </button>
          {armed ? (
            <button type="button" className="btn sm" disabled={pending} onClick={() => setArmed(false)}>
              {copy(pageContract, "action.cancel")}
            </button>
          ) : null}
        </span>
      )}
      {armed && confirmKey ? (
        <span className="small muted" style={{ maxWidth: 320, lineHeight: 1.5 }}>
          {copy(pageContract, confirmKey)}
        </span>
      ) : null}
      <FieldErrors result={result} pageContract={pageContract} />
    </form>
  );
}

/**
 * The draft editor: name, number of days, and the ordered step list.
 *
 * Rows are held in local state and submitted whole. Adding a row appends a blank one on the last
 * day, which the server drops if it is still blank at save time — so clicking "Add step" and
 * changing your mind costs nothing.
 */
export function DraftEditor({
  pageContract,
  draft,
  action,
  enabled,
  disabledReason,
}: {
  pageContract: AdminUiPageContract;
  draft: HealthConfigProtocolDetail;
  action: SubmitAction;
  enabled: boolean;
  disabledReason: string;
}) {
  const [pending, startTransition] = useTransition();
  const [result, setResult] = useState<HealthConfigActionResult | null>(null);
  const [idem, setIdem] = useState(() => crypto.randomUUID());
  const [displayName, setDisplayName] = useState(draft.display_name);
  const [durationDays, setDurationDays] = useState(String(draft.duration_days));
  const [steps, setSteps] = useState<DraftStep[]>(() => (draft.steps ?? []).map(stepFromContract));
  const [nextKey, setNextKey] = useState(0);

  const sessions = useMemo(() => optionGroup(pageContract, "health_sessions"), [pageContract]);
  const recordTypes = useMemo(() => optionGroup(pageContract, "health_record_types"), [pageContract]);
  const routes = useMemo(() => optionGroup(pageContract, "health_medicine_routes"), [pageContract]);
  const denominators = useMemo(() => optionGroup(pageContract, "health_dosage_units"), [pageContract]);
  const criticalTypes = useMemo(() => optionGroup(pageContract, "health_critical_actions"), [pageContract]);

  function updateStep(key: string, patch: Partial<DraftStep>) {
    setSteps((prev) => prev.map((step) => (step.key === key ? { ...step, ...patch } : step)));
  }

  function addStep() {
    const lastDay = steps.length > 0 ? steps[steps.length - 1].day_no : "1";
    const day = /^\d+$/.test(lastDay) ? Number(lastDay) : 1;
    setSteps((prev) => [...prev, emptyStep(day, `new-${nextKey}`)]);
    setNextKey((n) => n + 1);
  }

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        const formData = new FormData(event.currentTarget);
        formData.set("steps", JSON.stringify(toPayload(steps)));
        startTransition(async () => {
          const outcome = await action(formData);
          setResult(outcome);
          // Rotate the key only after a confirmed success: a failed save must be retryable under
          // the SAME key so the retry is recognised as the same intent, not a second edit.
          if (outcome.ok) setIdem(crypto.randomUUID());
        });
      }}
      style={{ display: "flex", flexDirection: "column", gap: 12 }}
    >
      <input type="hidden" name="disease_key" value={draft.disease_key} />
      <input type="hidden" name="age_band" value={draft.age_band} />
      <input type="hidden" name="idempotency_key" value={idem} />

      <div style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "flex-end" }}>
        <label className="fld" style={{ minWidth: 220 }}>
          <span>{copy(pageContract, "label.disease")}</span>
          <input
            name="display_name"
            type="text"
            maxLength={80}
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
          />
        </label>
        <label className="fld" style={{ maxWidth: 120 }}>
          <span>{copy(pageContract, "label.duration_days")}</span>
          <input
            name="duration_days"
            type="text"
            inputMode="numeric"
            value={durationDays}
            onChange={(event) => setDurationDays(event.target.value)}
          />
        </label>
        <p className="small muted" style={{ margin: 0, maxWidth: 420, lineHeight: 1.5 }}>
          {copy(pageContract, "note.days_shrink")} {copy(pageContract, "note.rename_scope")}
        </p>
      </div>

      {/* ONE READABLE BLOCK PER STEP, not a 10-column table.
          A treatment step has fields that only apply to its own kind: a medicine step has a
          dosage and a route and no instruction, an action has an instruction and none of the
          rest. Laying all ten columns side by side made every row mostly empty cells and pushed
          the instruction — the longest and most important field on an action step — off the
          right edge behind a horizontal scrollbar. Here each step shows only its own fields,
          the instruction gets full width, and nothing scrolls sideways. */}
      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        {steps.length === 0 ? (
          <div className="muted small" style={{ padding: "18px 4px", textAlign: "center" }}>
            {copy(pageContract, "empty.steps")}
          </div>
        ) : (
          steps.map((step, index) => {
            const isMedicine = step.record_type === "medication";
            const isCritical = step.record_type === "critical_action";
            return (
              <div
                key={step.key}
                style={{
                  border: "1px solid var(--line)",
                  borderRadius: "var(--r)",
                  padding: "10px 12px",
                  display: "flex",
                  flexDirection: "column",
                  gap: 10,
                }}
              >
                {/* WHEN: day, session and kind — the three things that place a step in the course. */}
                <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}>
                  <span className="tag t-mut" style={{ alignSelf: "center" }}>
                    {index + 1}
                  </span>
                  <label className="fld" style={{ width: 84 }}>
                    <span>{copy(pageContract, "label.day_no")}</span>
                    <input
                      type="text"
                      inputMode="numeric"
                      value={step.day_no}
                      onChange={(event) => updateStep(step.key, { day_no: event.target.value })}
                    />
                  </label>
                  <label className="fld" style={{ minWidth: 150 }}>
                    <span>{copy(pageContract, "label.session")}</span>
                    <select
                      value={step.session}
                      onChange={(event) => updateStep(step.key, { session: event.target.value })}
                    >
                      <OptionList options={sessions} />
                    </select>
                  </label>
                  <label className="fld" style={{ minWidth: 170 }}>
                    <span>{copy(pageContract, "label.record_type")}</span>
                    <select
                      value={step.record_type}
                      onChange={(event) => {
                        const recordType = event.target.value;
                        // Clearing the fields that do not belong to the new kind is not a
                        // convenience: the database CHECK constraints reject a medicine step
                        // carrying a handoff type, and a critical step carrying a medicine.
                        updateStep(step.key, {
                          record_type: recordType,
                          ...(recordType === "medication"
                            ? { critical_action_type: "" }
                            : { medicine_name: "", dosage_text: "", dosage_denominator: "", medicine_route: "" }),
                          ...(recordType === "critical_action" ? {} : { critical_action_type: "" }),
                        });
                      }}
                    >
                      <OptionList options={recordTypes} />
                    </select>
                  </label>
                  <button
                    type="button"
                    className="btn sm"
                    style={{ marginLeft: "auto" }}
                    title={copy(pageContract, "action.remove_step")}
                    onClick={() => setSteps((prev) => prev.filter((row) => row.key !== step.key))}
                  >
                    <Trash2 className="ic" aria-hidden="true" />
                    {copy(pageContract, "action.remove_step")}
                  </button>
                </div>

                {/* WHAT: only the fields this kind of step actually carries. */}
                {isMedicine ? (
                  <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}>
                    <label className="fld" style={{ flex: "1 1 260px", minWidth: 200 }}>
                      <span>{copy(pageContract, "label.medicine_name")}</span>
                      <input
                        type="text"
                        value={step.medicine_name}
                        onChange={(event) => updateStep(step.key, { medicine_name: event.target.value })}
                      />
                    </label>
                    <label className="fld" style={{ width: 110 }}>
                      <span>{copy(pageContract, "label.dosage_text")}</span>
                      <input
                        type="text"
                        inputMode="decimal"
                        value={step.dosage_text}
                        onChange={(event) => updateStep(step.key, { dosage_text: event.target.value })}
                      />
                    </label>
                    <label className="fld" style={{ width: 130 }}>
                      <span>{copy(pageContract, "label.dosage_denominator")}</span>
                      <select
                        title={copy(pageContract, "note.dosage_unit")}
                        value={step.dosage_denominator}
                        onChange={(event) => updateStep(step.key, { dosage_denominator: event.target.value })}
                      >
                        <OptionList options={denominators} includeBlank />
                      </select>
                    </label>
                    <label className="fld" style={{ width: 170 }}>
                      <span>{copy(pageContract, "label.medicine_route")}</span>
                      <select
                        value={step.medicine_route}
                        onChange={(event) => updateStep(step.key, { medicine_route: event.target.value })}
                      >
                        <OptionList options={routes} includeBlank />
                      </select>
                    </label>
                  </div>
                ) : (
                  <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}>
                    <label className="fld" style={{ flex: "1 1 100%" }}>
                      <span>{copy(pageContract, "label.instruction")}</span>
                      <textarea
                        rows={2}
                        value={step.instruction}
                        onChange={(event) => updateStep(step.key, { instruction: event.target.value })}
                      />
                    </label>
                    {isCritical ? (
                      <label className="fld" style={{ minWidth: 260 }}>
                        <span>{copy(pageContract, "label.critical_action_type")}</span>
                        <select
                          value={step.critical_action_type}
                          onChange={(event) => updateStep(step.key, { critical_action_type: event.target.value })}
                        >
                          <OptionList options={criticalTypes} includeBlank />
                        </select>
                      </label>
                    ) : null}
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>

      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
        <button type="button" className="btn sm" onClick={addStep}>
          <Plus className="ic" aria-hidden="true" />
          {copy(pageContract, "action.add_step")}
        </button>
        <button
          type="submit"
          className="btn sm p"
          disabled={pending || !enabled}
          aria-disabled={!enabled}
          title={enabled ? undefined : disabledReason}
        >
          {pending ? copy(pageContract, "state.loading") : copy(pageContract, "action.save_draft")}
        </button>
        <span className="small muted" style={{ lineHeight: 1.5 }}>
          {copy(pageContract, "note.unscheduled_session")}
        </span>
      </div>

      <FieldErrors result={result} pageContract={pageContract} />
    </form>
  );
}
