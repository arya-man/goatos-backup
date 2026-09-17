"use client";

import { CheckCircle2, MessageSquareText } from "lucide-react";
import { useRef } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
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
}: {
  task: TaskRow;
  pageContract: AdminUiPageContract;
  action: (formData: FormData) => void | Promise<void>;
  returnTo: string;
}) {
  const keyRef = useRef<HTMLInputElement>(null);
  return (
    <form action={action} className="lt-comment-form">
      <input ref={keyRef} type="hidden" name="idempotency_key" />
      <input type="hidden" name="return_to" value={returnTo} />
      <input type="hidden" name="task_id" value={task.id} />
      <label className="fld">
        <span>{copy(pageContract, "note.label")}</span>
        <textarea
          name="comment"
          maxLength={2000}
          rows={3}
          placeholder={copy(pageContract, "note.placeholder")}
          required
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
