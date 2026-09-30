"use client";

import Link from "@/components/no-prefetch-link";
import Box from "@mui/material/Box";
import Avatar from "@mui/material/Avatar";
import ListItemText from "@mui/material/ListItemText";

import { columnsFromContract, DataTable } from "@/components/data-table";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { copy, type AdminUiPageContract, type AdminUiTableContract } from "@/lib/admin-ui-contract";
import { DeadlineClock } from "./deadline-clock";
import { initials, statusTone } from "./task-presentation";
import type { RouteSearchParams } from "@/lib/search-params";
import { TASK_PARAM, tasksHref } from "./params";
import type { TaskRow } from "./task-row";
import { taskRowPatch, useTaskRowsVersion } from "./task-row-store";
import { patchApplies } from "./task-detail-pick";

/**
 * The task list, with its column set taken from the compiled table contract.
 *
 * `columnsFromContract` is the only sanctioned way to build the columns, and it throws on a
 * contract column with no renderer — which is precisely the mismatch this change had to settle:
 * the contract declared `priority`, the old hand-rolled table rendered a `Days` column the
 * contract never declared, and nothing connected the two. The renderers below cover BOTH spellings
 * of the same idea, so the table is correct under the contract as it stands (`priority`) and stays
 * correct the moment the backend renames that column to `days_left`, which is what it actually is:
 * `LeadershipTask` carries no priority field, and the deadline tone (ok / near / over) IS this
 * product's urgency. A renderer for a key the contract does not declare simply never runs.
 */
export function LeadershipTasksTable({
  pageContract,
  contract,
  rows,
  basePath,
  sp,
  scopeKey,
  selectedTaskID,
}: {
  pageContract: AdminUiPageContract;
  contract: AdminUiTableContract;
  rows: TaskRow[];
  basePath: string;
  sp: RouteSearchParams;
  scopeKey: string;
  selectedTaskID?: string;
}) {
  // The rows as the browser knows them: a status changed in the drawer, a comment that bumped a
  // version -- published to the row store and read here, so the chip in the row changes with
  // the pill in the drawer, with no route render (maintainer, 2026-09-18: "coming back to the
  // list not showing updated state").
  useTaskRowsVersion();
  // A patch OLDER than the server row is a leftover from before the last full render and must not
  // repaint the row backwards -- the same fence the board applies (`task-board-dnd.tsx`).
  const liveRows = rows.map((row) => {
    const patch = taskRowPatch(row.id);
    return patchApplies(row.rowVersion, patch) ? { ...row, ...patch } : row;
  });
  // Template user-list row anatomy (sections/user/user-table-row.tsx): a ListItemText name cell
  // (title primary, key secondary) whose link stretches over the whole row, an Avatar + name
  // people cell, soft Labels for status.
  const taskCell = (task: TaskRow) => (
    <>
      <Box
        component={Link}
        href={tasksHref(basePath, sp, { [TASK_PARAM.scope]: scopeKey, [TASK_PARAM.task]: task.id })}
        scroll={false}
        className="lt-tasklink"
        aria-label={`${copy(pageContract, "action.open_task")} ${task.number}: ${task.title}`}
        aria-current={task.id === selectedTaskID ? "true" : undefined}
        sx={{ display: "block", maxWidth: 320, color: "inherit", textDecoration: "none", minHeight: { xs: 44, md: "auto" }, "&:hover .MuiListItemText-primary": { textDecoration: "underline" } }}
      >
        <ListItemText
          primary={task.title}
          secondary={task.number}
          slotProps={{ primary: { variant: "subtitle2" }, secondary: { variant: "caption", sx: { color: "text.disabled" } } }}
          sx={{ m: 0 }}
        />
      </Box>
      {/* At phone width the urgency column is dropped -- header AND body cells (`.lt-days-col`),
          so the same clock is repeated here, under the title, where the row has room for it. One
          or the other shows, never both. */}
      {task.deadlineTone ? (
        <Box className="lt-clock-inline" sx={{ display: { xs: "block", md: "none" }, mt: 0.75 }}>
          <DeadlineClock task={task} compact />
        </Box>
      ) : null}
      {/* Stacked phone row (FJ1-P1-11): below sm the people / status / evidence columns are dropped
          (`.lt-wide-col`), so the assignee and status sit here instead of a column clipped at the
          card edge. guard: tasks-phone-stacked-row */}
      <Box className="lt-phone-meta" sx={{ display: { xs: "flex", sm: "none" }, alignItems: "center", flexWrap: "wrap", gap: 1, mt: 0.75, typography: "body2", color: "text.secondary" }}>
        {/* Same fallback as the laptop Assignee cell; an empty name renders nothing (no leading gap). */}
        {task.assignee || task.isAssignee ? (
          <Box component="span" sx={{ overflowWrap: "anywhere" }}>
            {task.assignee || copy(pageContract, "label.assigned_to_me", "Assigned to me")}
          </Box>
        ) : null}
        <Label variant="soft" color={statusColor(task.status)}>{task.statusLabel}</Label>
      </Box>
    </>
  );

  const clockCell = (task: TaskRow) => <DeadlineClock task={task} compact />;

  const columns = columnsFromContract<TaskRow>(contract, {
    task: { cell: taskCell, meta: { cellClassName: "lt-task-col", headerClassName: "lt-task-col" }, sortValue: (task) => task.title },
    // Both spellings of the urgency column; see the note above.
    priority: {
      cell: clockCell,
      meta: { cellClassName: "lt-days-col", headerClassName: "lt-days-col" },
      sortValue: (task) => task.daysLeft ?? undefined,
    },
    days_left: {
      cell: clockCell,
      meta: { cellClassName: "lt-days-col", headerClassName: "lt-days-col" },
      sortValue: (task) => task.daysLeft ?? undefined,
    },
    deadline: {
      cell: clockCell,
      meta: { cellClassName: "lt-days-col", headerClassName: "lt-days-col" },
      sortValue: (task) => task.daysLeft ?? undefined,
    },
    assignee: {
      cell: (task) => (
        <Box sx={{ gap: 1.5, display: "flex", alignItems: "center", minWidth: 0 }}>
          <Avatar sx={{ width: "calc(4 * var(--spacing))", height: "calc(4 * var(--spacing))", typography: "caption" }}>{initials(task.assignee)}</Avatar>
          <ListItemText
            primary={task.assignee}
            secondary={task.assigneeRole || (task.isAssignee ? copy(pageContract, "label.assigned_to_me", "Assigned to me") : "")}
            slotProps={{ primary: { variant: "body2" }, secondary: { variant: "caption", sx: { color: "text.disabled" } } }}
            sx={{ m: 0, minWidth: 0 }}
          />
        </Box>
      ),
      meta: { cellClassName: "lt-people-col lt-wide-col", headerClassName: "lt-people-col lt-wide-col" },
      sortValue: (task) => task.assignee,
    },
    raised_by: { cell: (task) => task.raisedBy, meta: { cellClassName: "lt-people-col lt-wide-col", headerClassName: "lt-people-col lt-wide-col" }, sortValue: (task) => task.raisedBy },
    status: {
      cell: (task) => <Label variant="soft" color={statusColor(task.status)}>{task.statusLabel}</Label>,
      meta: { cellClassName: "lt-wide-col", headerClassName: "lt-wide-col" },
      sortValue: (task) => task.status,
    },
    evidence: {
      cell: (task) => (
        <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.75, minWidth: 0, typography: "body2" }}>
          <Iconify icon="eva:attach-2-fill" width={16} sx={{ color: "text.disabled" }} />
          <b>{task.attachments}</b>
          <Box component="span" sx={{ color: "text.secondary", typography: "caption" }}>{task.evidence}</Box>
        </Box>
      ),
      meta: { cellClassName: "lt-wide-col", headerClassName: "lt-wide-col" },
      sortValue: (task) => task.attachments,
    },
  });

  return (
    <Box
      sx={(theme) => ({
        // The task link stretches over its row, so the whole row opens the drawer.
        "& tbody tr:has(.lt-tasklink)": { position: "relative", cursor: "pointer" },
        "& .lt-tasklink::after": { content: '""', position: "absolute", inset: 0, zIndex: 1 },
        "& .lt-tasklink:focus-visible": { outline: `2px solid ${theme.vars.palette.primary.main}`, outlineOffset: 2, borderRadius: 0.75 },
        "& td.lt-people-col": { whiteSpace: "normal", minWidth: 170, overflowWrap: "anywhere" },
        "& td.lt-task-col": { minWidth: 220 },
        "& td.lt-days-col": { whiteSpace: "nowrap" },
        [theme.breakpoints.down("md")]: { "& .lt-days-col": { display: "none" } },
        [theme.breakpoints.down("sm")]: { "& .lt-wide-col": { display: "none" }, "& td.lt-task-col": { minWidth: 0 } },
      })}
    >
      <DataTable<TaskRow>
        columns={columns}
        data={liveRows}
        getRowId={(task) => task.id}
        ariaLabel={contract.title}
        className="lt-task-table"
        empty={copy(pageContract, "empty.tasks")}
      />
    </Box>
  );
}

function statusColor(status: TaskRow["status"]): "warning" | "info" | "success" | "default" {
  if (status === "in_progress") return "info";
  if (status === "done") return "success";
  if (status === "cancelled") return "default";
  return "warning";
}

/**
 * Re-exported for the callers that have always imported them from here. They now LIVE in
 * `task-presentation.ts` because this module is `"use client"`, and a function exported from a
 * client module cannot be CALLED by a server component — which the board and the detail panel
 * both are. A server caller must import from `./task-presentation` directly; importing them
 * through this file would hand it a client reference again.
 */
export { initials, statusTone };
