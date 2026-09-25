"use client";

import { CalendarClock, CheckCircle2, MessageSquareText } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";

import { ThemedDatePicker } from "@/components/themed-date-picker";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { splitFarmDeadlineLocal } from "./deadline";
import {
  MentionTextarea,
  resolveMentionComposerCopy,
  type MentionCandidate,
} from "@/features/notifications";
import type { TaskRow } from "./task-row";

/**
 * The two in-place writes on the detail panel, with their idempotency keys minted CLIENT-SIDE.
 *
 * Both used to interpolate `crypto.randomUUID()` straight into the server-rendered markup. A
 * server-rendered key is part of the HTML, so a back navigation posts the same key again and the
 * second, legitimate change is swallowed as a duplicate of the first — the change appears to be
 * accepted and nothing happens. Minting in the click handler (which runs before the form submits)
 * makes every press its own request, and makes a double-press of ONE button still idempotent only
 * for the duration of that press.
 *
 * `row_version` comes from the row being rendered, so the fence is never a stale number captured
 * when the page first loaded.
 */
export function TaskStatusActions({
  task,
  pageContract,
  action,
  returnTo,
}: {
  task: TaskRow;
  pageContract: AdminUiPageContract;
  action: (formData: FormData) => void | Promise<void>;
  returnTo: string;
}) {
  if (!task.statusOptions.length) return null;
  return (
    <div className="lt-status-actions">
      {task.statusOptions.map((option) => (
        <StatusForm
          key={option.key}
          task={task}
          option={option}
          action={action}
          returnTo={returnTo}
          pageContract={pageContract}
        />
      ))}
    </div>
  );
}

function StatusForm({
  task,
  option,
  action,
  returnTo,
}: {
  task: TaskRow;
  option: { key: string; label: string };
  action: (formData: FormData) => void | Promise<void>;
  returnTo: string;
  pageContract: AdminUiPageContract;
}) {
  const keyRef = useRef<HTMLInputElement>(null);
  return (
    <form action={action}>
      <input ref={keyRef} type="hidden" name="idempotency_key" />
      <input type="hidden" name="return_to" value={returnTo} />
      <input type="hidden" name="task_id" value={task.id} />
      <input type="hidden" name="row_version" value={task.rowVersion} />
      <input type="hidden" name="status" value={option.key} />
      {/* The status this panel was SHOWING, so a refused change can say whether the task moved
          under the reader. Not an input to the write — the fence and the backend's own transition
          check decide that. The board's drop posts the same field. */}
      <input type="hidden" name="from_status" value={task.status} />
      <button
        type="submit"
        className="btn"
        onClick={() => {
          if (keyRef.current) {
            keyRef.current.value = `admin-web-leadership-task-status:${task.id}:${option.key}:${crypto.randomUUID()}`;
          }
        }}
      >
        <CheckCircle2 className="ic" aria-hidden="true" />
        {/* The button's wording is the backend's own status-option label. */}
        {option.label}
      </button>
    </form>
  );
}

export function TaskCommentForm({
  task,
  pageContract,
  action,
  returnTo,
  mentionCandidates = [],
}: {
  task: TaskRow;
  pageContract: AdminUiPageContract;
  action: (formData: FormData) => void | Promise<void>;
  returnTo: string;
  /**
   * Who an update may name with `@`. These are the leadership ASSIGNEES the page already loads
   * (`GET /app/leadership-tasks/assignees`) -- the raise targets. The narrower, task-scoped
   * `GET /app/leadership-tasks/{task_id}/mentionable-users` (the task's own parties plus the
   * leadership roles) is the more correct source and has no reader in `lib/api/server.ts` yet;
   * adding one belongs to that file's owner. The consequence of the substitute is a candidate
   * LIST that is wider than the task's own parties -- never a wrong write, because the backend
   * re-validates every id under the task's row lock and refuses one that cannot see the task
   * (403 `mention_not_visible`).
   */
  mentionCandidates?: readonly MentionCandidate[];
}) {
  const keyRef = useRef<HTMLInputElement>(null);
  return (
    <form action={action} className="lt-comment-form">
      <input ref={keyRef} type="hidden" name="idempotency_key" />
      <input type="hidden" name="return_to" value={returnTo} />
      <input type="hidden" name="task_id" value={task.id} />
      <label className="fld">
        <span>{copy(pageContract, "note.label")}</span>
        {/* The composer emits BOTH halves: the prose in `comment`, and the ids the writer
            actually picked in `mention_user_ids`. The server does not parse "@Ravi" out of the
            text -- two active people share a display name -- so the ids are the contract. It is a
            real named textarea plus a hidden input, so this form stays uncontrolled and the text
            still submits without JavaScript; only the picker needs it. */}
        <MentionTextarea
          name="comment"
          mentionsName="mention_user_ids"
          candidates={mentionCandidates}
          composerCopy={resolveMentionComposerCopy(pageContract.copy)}
          rows={3}
          maxLength={2000}
          required
          placeholder={copy(pageContract, "note.placeholder")}
        />
      </label>
      <button
        type="submit"
        className="btn p"
        onClick={() => {
          if (keyRef.current) {
            keyRef.current.value = `admin-web-leadership-task-note:${task.id}:${crypto.randomUUID()}`;
          }
        }}
      >
        <MessageSquareText className="ic" aria-hidden="true" />
        {copy(pageContract, "note.send")}
      </button>
    </form>
  );
}

/** Every hour of the farm's day, as the two-digit strings the form posts. */
const DEADLINE_HOURS = Array.from({ length: 24 }, (_, hour) => `${hour}`.padStart(2, "0"));
/** Five-minute steps; a stored deadline on an odd minute is added to the list so it round-trips. */
const DEADLINE_MINUTES = Array.from({ length: 12 }, (_, step) => `${step * 5}`.padStart(2, "0"));

/**
 * The DEADLINE, in both modals: the console's own calendar for the day plus two selects for the
 * time, on the farm's clock (IST).
 *
 * It replaced a bare native `datetime-local`. Chrome draws that control's picker OUTSIDE the
 * modal box -- its hour and minute columns landed over the attachment buttons and Send -- and it
 * orders the day, month and year by the browser's locale rather than the DD/MM/YYYY every other
 * date in this console renders. `ThemedDatePicker` is the app's one date field; its popover opens
 * IN FLOW inside the modal (the modal's stylesheet makes it static, as the phone filter sheet does
 * for the person popup), so it can neither be clipped by the modal body's scroller nor drawn over
 * the controls beneath it. The Server Action joins `deadline_date` + `deadline_hour` +
 * `deadline_minute` back into the `YYYY-MM-DDTHH:MM` shape it always read (`deadline.ts`).
 *
 * `required` makes the calendar refuse the submit with the backend's own sentence when no day is
 * picked; the time selects carry the native `required`. The EDIT form passes `required=false`,
 * where a blank day means "keep the stored deadline".
 */
export function TaskDeadlineFields({
  pageContract,
  defaultLocal,
  min,
  required,
  missing = false,
  label,
  hint,
}: {
  pageContract?: AdminUiPageContract;
  /** `YYYY-MM-DDTHH:MM` on the farm's clock, or "" for nothing chosen yet. */
  defaultLocal: string;
  /** Earliest selectable day, `YYYY-MM-DD`. */
  min?: string;
  required: boolean;
  /**
   * The form found the day, hour or minute blank on Send. One sentence under the field says so
   * (2026-09-25): the browser's own bubble fired on the HOUR box with "Please select an item in
   * the list", and nothing told the raiser the DAY was missing.
   */
  missing?: boolean;
  label: string;
  hint: string;
}) {
  const initial = splitFarmDeadlineLocal(defaultLocal);
  const [hour, setHour] = useState(initial.hour);
  const [minute, setMinute] = useState(initial.minute);
  const wrapRef = useRef<HTMLDivElement>(null);
  const hintId = useId();
  const text = (key: string, fallback: string) =>
    pageContract ? copy(pageContract, key, fallback) : fallback;
  const minutes = DEADLINE_MINUTES.includes(minute) || !minute
    ? DEADLINE_MINUTES
    : [...DEADLINE_MINUTES, minute].sort();

  // The calendar opens in flow, so on a short phone it can land below the modal body's fold; it
  // is scrolled into view the moment it opens, the way the person popup is.
  useEffect(() => {
    const details = wrapRef.current?.querySelector("details");
    if (!details) return undefined;
    function onToggle() {
      if (details?.open) details.scrollIntoView({ block: "nearest" });
    }
    details.addEventListener("toggle", onToggle);
    return () => details.removeEventListener("toggle", onToggle);
  }, []);

  return (
    <div
      className="fld lt-deadline"
      ref={wrapRef}
      // Escape unwinds ONE layer. The calendar and the modal shell both listen for Escape on
      // `document`; left alone, one press closed the calendar AND the modal, and the raiser lost
      // the whole form. Caught here first (React's capture phase runs at the root, before any
      // document listener), an open calendar swallows the press; the next press reaches the shell.
      onKeyDownCapture={(event) => {
        if (event.key !== "Escape") return;
        const details = wrapRef.current?.querySelector("details");
        if (!details?.open) return;
        event.preventDefault();
        event.nativeEvent.stopImmediatePropagation();
        details.open = false;
        (details.querySelector("summary") as HTMLElement | null)?.focus();
      }}
    >
      <span className="lt-fld-label">{label}</span>
      <div className="lt-deadline-row">
        <div className="lt-deadline-date">
          <ThemedDatePicker
            name="deadline_date"
            label={text("deadline.day", "Choose a day")}
            min={min}
            // The New task form checks day, hour and minute itself and says so in ONE sentence
            // (`missing`); the picker's own required check opened the calendar with a second red
            // line ("Pick a day on or after ..."), so it is not asked to.
            required={false}
            defaultValue={initial.date}
            previousMonthLabel={text("date.previous_month", "Previous month")}
            nextMonthLabel={text("date.next_month", "Next month")}
            invalidDateText={text("deadline.day_min", "Pick a day on or after {date}.")}
          />
        </div>
        <label className="lt-deadline-time">
          <span className="lt-fld-sub">{text("deadline.hour", "Hour")}</span>
          <select
            name="deadline_hour"
            aria-required={required}
            aria-invalid={missing && !hour ? true : undefined}
            value={hour}
            aria-describedby={hintId}
            onChange={(event) => setHour(event.target.value)}
          >
            <option value="">--</option>
            {DEADLINE_HOURS.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>
        <label className="lt-deadline-time">
          <span className="lt-fld-sub">{text("deadline.minute", "Minute")}</span>
          <select
            name="deadline_minute"
            aria-required={required}
            aria-invalid={missing && !minute ? true : undefined}
            value={minute}
            aria-describedby={hintId}
            onChange={(event) => setMinute(event.target.value)}
          >
            <option value="">--</option>
            {minutes.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>
      </div>
      {missing ? (
        <small className="lt-fnote" role="alert" data-testid="lt-deadline-missing">
          {text("feedback.missing_deadline", "Choose the deadline day and time.")}
        </small>
      ) : null}
      <small id={hintId} className="lt-assignee-hint lt-deadline-hint">
        <CalendarClock className="ic" aria-hidden="true" />
        <span>{hint}</span>
      </small>
    </div>
  );
}
