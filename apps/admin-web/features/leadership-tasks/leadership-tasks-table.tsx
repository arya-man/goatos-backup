"use client";

import Link from "@/components/no-prefetch-link";
import { Paperclip } from "lucide-react";

import { columnsFromContract, DataTable } from "@/components/data-table";
import { Tag } from "@/components/ui-primitives";
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
  const taskCell = (task: TaskRow) => (
    <>
      <Link
        // The same deep link the board card mints: `task=<id>` ADDED to the current URL, so the
        // view, status filter, sort, people filters and page survive the click. Building the href
        // from scratch here dropped `t_view=list`, which is why clicking a list row flipped the
        // page back to the board.
        href={tasksHref(basePath, sp, { [TASK_PARAM.scope]: scopeKey, [TASK_PARAM.task]: task.id })}
        scroll={false}
        className="lt-tasklink"
        aria-label={`${copy(pageContract, "action.open_task")} ${task.number}: ${task.title}`}
        aria-current={task.id === selectedTaskID ? "true" : undefined}
      >
        <b>{task.number}</b>
        <span className="muted small">{task.title}</span>
      </Link>
      {/* At phone width the urgency column is dropped -- header AND body cells, via
          `.lt-days-col{display:none}` at max-width:760px and the paired
          `cellClassName`/`headerClassName` below. Every other column still renders, so the same
          clock is repeated here, under the title, where the row has room for it. CSS shows one or
          the other, never both. */}
      {task.deadlineTone ? (
        <div className="lt-clock-inline">
          <DeadlineClock task={task} compact />
        </div>
      ) : null}
    </>
  );

  const clockCell = (task: TaskRow) => <DeadlineClock task={task} compact />;

  const columns = columnsFromContract<TaskRow>(contract, {
    task: { cell: taskCell, sortValue: (task) => task.title },
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
        <div className="lt-opname">
          <span className="lt-avx">{initials(task.assignee)}</span>
          <span>
            {task.assignee}
            <span className="lt-code muted">
              {task.assigneeRole ||
                (task.isAssignee ? copy(pageContract, "label.assigned_to_me", "Assigned to me") : "")}
            </span>
          </span>
        </div>
      ),
      sortValue: (task) => task.assignee,
    },
    raised_by: { cell: (task) => task.raisedBy, sortValue: (task) => task.raisedBy },
    status: {
      cell: (task) => <Tag tone={statusTone(task.status)}>{task.statusLabel}</Tag>,
      sortValue: (task) => task.status,
    },
    evidence: {
      cell: (task) => (
        <span style={{ display: "inline-flex", alignItems: "center", gap: 7, minWidth: 0 }}>
          <Paperclip className="ic" style={{ width: 15, color: "var(--muted)" }} aria-hidden="true" />
          <b>{task.attachments}</b>
          <span className="muted small">{task.evidence}</span>
        </span>
      ),
      sortValue: (task) => task.attachments,
    },
  });

  return (
    <DataTable<TaskRow>
      columns={columns}
      data={liveRows}
      getRowId={(task) => task.id}
      ariaLabel={contract.title}
      className="lt-task-table"
      empty={<div className="muted small lt-table-empty">{copy(pageContract, "empty.tasks")}</div>}
    />
  );
}

/**
 * Re-exported for the callers that have always imported them from here. They now LIVE in
 * `task-presentation.ts` because this module is `"use client"`, and a function exported from a
 * client module cannot be CALLED by a server component — which the board and the detail panel
 * both are. A server caller must import from `./task-presentation` directly; importing them
 * through this file would hand it a client reference again.
 */
export { initials, statusTone };
