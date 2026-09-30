"use client";

import { useRef, useState, useTransition } from "react";
import { Iconify } from "@/components/minimal/iconify";
import { usePopover } from "minimal-shared/hooks";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Divider from "@mui/material/Divider";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import Typography from "@mui/material/Typography";
import type { Theme } from "@mui/material/styles";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import type { TaskRow } from "./task-row";
import { changeLeadershipTaskStatusInPlaceAction, loadLeadershipTaskAction, type StatusChangeResult } from "./actions";
import { refusalSentence } from "./task-feedback-copy";
import { currentTaskRowVersion, publishTaskRow, runTaskWrite, useTaskRow, useTaskWriteInFlight } from "./task-row-store";
import { rowFromTask } from "./task-row";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { TAP_MIN } from "@/components/app/tap";

/**
 * THE status control of the task drawer: one dropdown, the Work Board's menu.
 *
 * The drawer used to show `STATUS [To do ▾] [In progress] [Done] [Cancel task]` — a pill whose
 * caret promised a menu that never opened, then one button per transition beside it, so a single
 * decision ("what state is this task in?") was spread over four controls with three visual
 * languages. The CEO rejected that as not the same product as the Work Board.
 *
 * This is the one control. The trigger is the status pill (its tone and wording are the
 * backend's `status_chip`); it opens the template menu popover (CustomPopover + MenuList, the
 * same as the board's Module and Assignee menus). The items are the backend's `status_options` in the backend's
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
  const menu = usePopover();
  const open = menu.open;
  const [ownPending, startTransition] = useTransition();
  // A comment (or any other write) to this task still on the wire holds the menu: the status write
  // would otherwise be queued behind it carrying the fence from BEFORE the comment landed.
  const writing = useTaskWriteInFlight(task.id);
  const pending = ownPending || writing;
  const [refusal, setRefusal] = useState<string>("");
  // The cancel confirm is IN the menu (the console's own control), never the browser's
  // `window.confirm` dialog -- "127.0.0.1 says" is not a thing the CEO should read (2026-09-18).
  const [confirmingCancel, setConfirmingCancel] = useState(false);
  // MUI Popover closes on outside click and Escape (its own handler stops the press, so the
  // drawer's Escape never hears it -- one press, one layer) and returns focus to the trigger.
  const close = () => {
    menu.onClose();
    setConfirmingCancel(false);
  };
  const formRef = useRef<HTMLFormElement>(null);
  const keyRef = useRef<HTMLInputElement>(null);
  const statusRef = useRef<HTMLInputElement>(null);
  const menuId = `ltd-status-menu-${task.id}`;

  const moves = task.statusOptions.filter((option) => option.key !== "cancelled");
  const cancel = task.statusOptions.find((option) => option.key === "cancelled") ?? null;
  const cancelConfirm = copy(pageContract, "status.cancel_confirm", "Cancel this task? It cannot be reopened afterwards.");

  const submit = (key: string) => {
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
    // OPTIMISTIC: the write is 0.5-0.7 s over the farm's link, and a pill that sits unchanged for
    // that long reads as dead (the CEO clicked it three times, 2026-09-18). The pill and the board
    // card move NOW with the chosen status; the backend's row replaces it when it lands, and a
    // refusal puts the old status back beside the reason.
    const chosen = task.statusOptions.find((option) => option.key === key);
    const before = { status: task.status, statusLabel: task.statusLabel };
    // The options are deliberately KEPT on the optimistic row: an empty list makes the panel
    // swap this menu for a plain badge, unmounting it mid-write -- which threw away the spinner
    // and the refusal sentence (Judge B, P2-1/P2-2). The button is disabled while pending.
    publishTaskRow(task.id, {
      status: key as TaskRow["status"],
      statusLabel: chosen?.label ?? task.statusLabel,
    });
    startTransition(async () => {
      let result: StatusChangeResult;
      try {
        // Serialised behind any write to this task still in flight; the fence is read when the
        // write is SENT, so it is the version the previous write published, not the one on
        // screen at the click.
        result = await runTaskWrite(task.id, () => {
          formData.set("row_version", String(currentTaskRowVersion(task.id, serverTask.rowVersion)));
          return changeLeadershipTaskStatusInPlaceAction(formData);
        });
      } catch {
        result = { ok: false, code: "network" };
      }
      if (result.ok) {
        publishTaskRow(task.id, rowFromTask(result.task));
        return;
      }
      publishTaskRow(task.id, before);
      // The refusal names where the task IS now ("moved to In progress while this board was
      // open ... the board now shows that"); make the board show that (Judge B, P2-2).
      if (result.code === "version_conflict" || result.statusNow) {
        const fresh = await loadLeadershipTaskAction(task.id).catch(() => null);
        if (fresh?.ok) publishTaskRow(task.id, rowFromTask(fresh.task));
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
    <Box className="ltd-statusmenu" sx={{ display: "inline-block" }}>
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
      {/* Template kanban details toolbar status control: a small soft Button with a down caret. */}
      <Button
        size="small"
        variant="soft"
        color={STATUS_COLOR[task.status] ?? "inherit"}
        className="ltd-status"
        data-ltd-status="control"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={menuId}
        aria-label={`${copy(pageContract, "label.status", "Status")}: ${task.statusLabel}`}
        aria-busy={pending || undefined}
        disabled={pending}
        onClick={menu.onOpen}
        endIcon={<Iconify icon="eva:arrow-ios-downward-fill" width={16} sx={{ ml: -0.5 }} />}
      >
        {task.statusLabel}
      </Button>
      {refusal ? (
        <Typography variant="caption" role="alert" sx={{ display: "block", mt: 1, color: "error.main" }}>
          {refusal}
        </Typography>
      ) : null}
      <CustomPopover
        open={open}
        anchorEl={menu.anchorEl}
        onClose={close}
        slotProps={{ arrow: { placement: "top-left" }, paper: { sx: { maxWidth: 320 } } }}
      >
        <MenuList id={menuId} aria-label={copy(pageContract, "status.menu_aria", "Change status")}>
          {moves.map((option) => (
            <MenuItem key={option.key} onClick={() => submit(option.key)} sx={tapRow}>
              <Box
                component="span"
                aria-hidden="true"
                sx={{ width: "var(--sp-1)", height: "var(--sp-1)", borderRadius: "50%", flex: "none", bgcolor: STATUS_COLOR[option.key as TaskRow["status"]] ? `${STATUS_COLOR[option.key as TaskRow["status"]]}.main` : "text.disabled" }}
              />
              {/* The item's wording is the backend's own status-option label. */}
              {option.label}
            </MenuItem>
          ))}
          {cancel && moves.length ? <Divider sx={{ borderStyle: "dashed" }} /> : null}
          {cancel && !confirmingCancel ? (
            <MenuItem onClick={() => setConfirmingCancel(true)} sx={(theme) => ({ ...tapRow(theme), color: theme.palette.error.main })}>
              <Iconify icon="solar:close-circle-bold" />
              {cancel.label}
            </MenuItem>
          ) : null}
        </MenuList>
        {cancel && confirmingCancel ? (
          <Box role="group" aria-label={cancel.label} sx={{ px: 1, pb: 1, display: "grid", gap: 1 }}>
            <Typography variant="body2" sx={{ color: "text.secondary" }}>{cancelConfirm}</Typography>
            <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
              <Button size="small" variant="contained" color="error" onClick={() => submit(cancel.key)} sx={tapRow}>
                {copy(pageContract, "status.cancel_yes", "Yes, cancel task")}
              </Button>
              <Button size="small" variant="outlined" color="inherit" onClick={() => setConfirmingCancel(false)} sx={tapRow}>
                {copy(pageContract, "status.cancel_no", "Keep task")}
              </Button>
            </Box>
          </Box>
        ) : null}
      </CustomPopover>
    </Box>
  );
}

const STATUS_COLOR: Partial<Record<TaskRow["status"], "warning" | "info" | "success">> = {
  open: "warning",
  in_progress: "info",
  done: "success",
};

/** Phone tap floor for menu rows and the confirm buttons (webview rule: >=44px). */
function tapRow(theme: Theme) {
  return { [theme.breakpoints.down("sm")]: { minHeight: TAP_MIN } };
}
