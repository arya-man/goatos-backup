"use client";

import { CheckCircle2, MessageSquareText } from "lucide-react";
import { useRef } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
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
