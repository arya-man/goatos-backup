"use client";

import { useCallback, useEffect, useRef, useState, useTransition } from "react";
import { XCircle } from "lucide-react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import { statusTone } from "./task-presentation";
import type { TaskRow } from "./task-row";
import { changeLeadershipTaskStatusInPlaceAction, type StatusChangeResult } from "./actions";
import { refusalSentence } from "./task-feedback-copy";
import { publishTaskRow, useTaskRow } from "./task-row-store";
import { rowFromTask } from "./task-row";

/**
 * THE status control of the task drawer: one dropdown, the Work Board's menu.
 *
 * The drawer used to show `STATUS [To do ▾] [In progress] [Done] [Cancel task]` — a pill whose
 * caret promised a menu that never opened, then one button per transition beside it, so a single
 * decision ("what state is this task in?") was spread over four controls with three visual
 * languages. The CEO rejected that as not the same product as the Work Board.
 *
 * This is the one control. The trigger is the status pill (its tone and wording are the
 * backend's `status_chip`); it opens the Work Board's `.wb .menu` (the same classes the board's
 * Module and Assignee menus use, scoped under a local `.wb` so the board's rules apply verbatim
 * and nothing is restyled here). The items are the backend's `status_options` in the backend's
 * order, and "Cancel task" — whenever the API lists it — is the LAST item, below a rule and in
 * the danger tone, behind a confirm. Who may cancel is decided by `can_cancel` upstream: this
 * menu never shows a move the API did not list.
 *
 * The write is the same server action the buttons posted, with the same fields (`task_id`,
 * `row_version` fence, `status`, `from_status`, a browser-minted `idempotency_key`, `return_to`).
 * `task-write-forms.tsx`'s `TaskStatusActions` still exists for the board's other hosts; this
 * file only replaces it in the drawer.
 *
 * `data-ltd-status="control"` names a pill that opens; with no `status_options` (a reader who
 * may not move this task, or a cancelled one) the caller renders the plain badge instead and
 * this component is not mounted, so the caret is never a dead promise.
 */
export function TaskStatusMenu({
  task: serverTask,
  pageContract,
  action,
  returnTo,
}: {
  task: TaskRow;
  pageContract: AdminUiPageContract;
  /** The redirecting action: the no-JS form's path only. With JS the change RETURNS. */
  action: (formData: FormData) => void | Promise<void>;
  returnTo: string;
}) {
  // The row as the browser knows it -- a change made here, or a comment that bumped the version.
  const task = useTaskRow(serverTask);
  const rowVersion = task.rowVersion;
  const [open, setOpen] = useState(false);
  const [pending, startTransition] = useTransition();
  const [refusal, setRefusal] = useState<string>("");
  const close = useCallback(() => setOpen(false), []);
  const ref = useOutsideClose(open, close);
  const formRef = useRef<HTMLFormElement>(null);
  const keyRef = useRef<HTMLInputElement>(null);
  const statusRef = useRef<HTMLInputElement>(null);
  const menuId = `ltd-status-menu-${task.id}`;

  const moves = task.statusOptions.filter((option) => option.key !== "cancelled");
  const cancel = task.statusOptions.find((option) => option.key === "cancelled") ?? null;
  const cancelConfirm = copy(pageContract, "status.cancel_confirm", "Cancel this task? It cannot be reopened afterwards.");

  const submit = (key: string) => {
    if (key === "cancelled" && !window.confirm(cancelConfirm)) {
      close();
      return;
    }
    if (statusRef.current) statusRef.current.value = key;
    if (keyRef.current) {
      keyRef.current.value = `admin-web-leadership-task-status:${task.id}:${key}:${crypto.randomUUID()}`;
    }
    close();
    // IN PLACE: the write returns the task; the pill, the fence and the board card update from
    // the published row. No redirect, no route re-render, no skeleton (the flicker the CEO saw).
    const form = formRef.current;
    if (!form) return;
    const formData = new FormData(form);
    setRefusal("");
    startTransition(async () => {
      let result: StatusChangeResult;
      try {
        result = await changeLeadershipTaskStatusInPlaceAction(formData);
      } catch {
        result = { ok: false, code: "network" };
      }
      if (result.ok) {
        publishTaskRow(task.id, rowFromTask(result.task));
        return;
      }
      const sentence =
        refusalSentence(
          (k, fb) => copy(pageContract, k, fb),
          result.code,
          result.statusNow,
          result.who,
        ) || copy(pageContract, "action.failed_message", "Action could not be completed.");
      setRefusal(sentence);
    });
  };

  return (
    <div className="wb ltd-statusmenu" ref={ref}>
      <form ref={formRef} action={action}>
        <input ref={keyRef} type="hidden" name="idempotency_key" />
        <input type="hidden" name="return_to" value={returnTo} />
        <input type="hidden" name="task_id" value={task.id} />
        <input type="hidden" name="row_version" value={rowVersion} />
        {/* Uncontrolled on purpose: the chosen key is written into the input right before
            requestSubmit(), and a controlled `value` would be reset by the re-render that closes
            the menu, posting the CURRENT status back as the target. */}
        <input ref={statusRef} type="hidden" name="status" defaultValue={task.status} />
        {/* The status this drawer was SHOWING, so a refused change can say whether the task moved
            under the reader. Not an input to the write — the fence and the backend's own
            transition check decide that. The board's drop posts the same field. */}
        <input type="hidden" name="from_status" value={task.status} />
      </form>
      <button
        type="button"
        className={`ltd-status ltd-status-${statusTone(task.status)}${open ? " on" : ""}`}
        data-ltd-status="control"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        aria-label={`${copy(pageContract, "label.status", "Status")}: ${task.statusLabel}`}
        aria-busy={pending || undefined}
        disabled={pending}
        onClick={() => setOpen((v) => !v)}
      >
        {task.statusLabel}
        <span className="ltd-status-caret" aria-hidden="true" />
      </button>
      {refusal ? (
        <p className="ltd-status-refusal" role="alert">
          {refusal}
        </p>
      ) : null}
      {open ? (
        <div className="menu ltd-status-pop" role="menu" id={menuId} aria-label={copy(pageContract, "status.menu_aria", "Change status")}>
          {moves.map((option) => (
            <button key={option.key} type="button" role="menuitem" className="opt" onClick={() => submit(option.key)}>
              <span className={`ltd-status-dot ltd-status-${statusTone(option.key as TaskRow["status"])}`} aria-hidden="true" />
              {/* The item's wording is the backend's own status-option label. */}
              {option.label}
            </button>
          ))}
          {cancel ? (
            <div className={moves.length ? "foot ltd-status-foot" : "ltd-status-foot"}>
              <button type="button" role="menuitem" className="opt ltd-opt-danger" onClick={() => submit(cancel.key)}>
                <XCircle className="ic" aria-hidden="true" />
                {cancel.label}
              </button>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

// The same close rule as the Work Board's toolbar menus (`components/assignee-picker.tsx`): an
// outside click closes, and Escape closes on a CAPTURING document listener that stops
// propagation, so the drawer's own Escape (which closes the whole drawer) never hears the press
// that closed this menu — one press, one layer. Focus goes back to the trigger.
function useOutsideClose(open: boolean, close: () => void) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDoc = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) close();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      close();
      ref.current?.querySelector<HTMLElement>("[aria-expanded]")?.focus();
    };
    document.addEventListener("click", onDoc);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("click", onDoc);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [open, close]);
  return ref;
}
