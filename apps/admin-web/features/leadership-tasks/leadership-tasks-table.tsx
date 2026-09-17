"use client";

import Link from "@/components/no-prefetch-link";
import { Paperclip } from "lucide-react";

import { columnsFromContract, DataTable } from "@/components/data-table";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract, type AdminUiTableContract } from "@/lib/admin-ui-contract";
import { DeadlineClock } from "./deadline-clock";
import type { TaskRow } from "./task-row";

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
  scopeKey,
  selectedTaskID,
}: {
  pageContract: AdminUiPageContract;
  contract: AdminUiTableContract;
  rows: TaskRow[];
  basePath: string;
  scopeKey: string;
  selectedTaskID?: string;
}) {
  const taskCell = (task: TaskRow) => (
    <>
      <Link
        href={`${basePath}?scope=${encodeURIComponent(scopeKey)}&task=${encodeURIComponent(task.id)}`}
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
            <span className="lt-code muted">{task.assigneeRole}</span>
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
      data={rows}
      getRowId={(task) => task.id}
      ariaLabel={contract.title}
      className="lt-task-table"
      empty={<div className="muted small lt-table-empty">{copy(pageContract, "empty.tasks")}</div>}
    />
  );
}

export function statusTone(status: TaskRow["status"]): Tone {
  if (status === "in_progress") return "info";
  if (status === "done") return "ok";
  if (status === "cancelled") return "mut";
  return "warn";
}

export function initials(name: string): string {
  return name
    .split(/\s+/)
    .map((part) => part.charAt(0))
    .join("")
    .slice(0, 2)
    .toUpperCase();
}
