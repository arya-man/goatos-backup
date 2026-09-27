"use client";

import Link from "@/components/no-prefetch-link";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import type { DragEvent } from "react";

import { KanbanItemRoot } from "@/components/app/kanban";
import { ItemContent, ItemInfo, ItemName, ItemStatus } from "@/components/app/kanban/item-styles";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import { DeadlineClock } from "./deadline-clock";
import { initials } from "./task-presentation";
import type { TaskRow } from "./task-row";

/**
 * One task on the status board, in the template kanban item anatomy (sections/kanban/item):
 * the ItemRoot shell, ItemContent padding, the priority arrow top-right (the backend's deadline
 * tone: overdue high, near medium), the name, then the ItemInfo row (attachment count, assignee
 * avatar). Our own readings -- the deadline clock, the raiser and the task key -- sit between the
 * name and the info row. The whole card is the link that opens the task drawer.
 */
export function TaskBoardCard({
  task,
  pageContract,
  href,
  selected,
  draggable,
  dragging = false,
  pending = false,
  onDragStart,
  onDragEnd,
}: {
  task: TaskRow;
  pageContract: AdminUiPageContract;
  href: string;
  selected: boolean;
  draggable: boolean;
  dragging?: boolean;
  pending?: boolean;
  onDragStart?: (event: DragEvent<HTMLAnchorElement>) => void;
  onDragEnd?: (event: DragEvent<HTMLAnchorElement>) => void;
}) {
  const deadlineWord = copy(pageContract, "label.deadline");
  const placeholder = copy(pageContract, "label.placeholder", "—");
  const status = task.deadlineTone === "over" ? "high" : task.deadlineTone === "near" ? "medium" : null;
  return (
    <KanbanItemRoot
      className={`ltb-card-root${dragging ? " --dragging" : ""}`}
      data-selected={selected ? "true" : undefined}
      sx={{
        cursor: draggable ? "grab" : "pointer",
        ...(dragging ? { filter: "grayscale(1)", "& > *": { opacity: 0.4 } } : {}),
        ...(pending ? { opacity: 0.72 } : {}),
      }}
    >
      <Box
        component={Link}
        href={href}
        scroll={false}
        className={`ltb-card${selected ? " is-selected" : ""}${dragging ? " is-dragging" : ""}${pending ? " is-moving" : ""}`}
        aria-current={selected ? "true" : undefined}
        aria-label={`${copy(pageContract, "action.open_task")} ${task.number}: ${task.title}`}
        draggable={draggable}
        onDragStart={draggable ? onDragStart : undefined}
        onDragEnd={draggable ? onDragEnd : undefined}
        sx={{ display: "block", minWidth: 0, color: "inherit", textDecoration: "none", borderRadius: "inherit" }}
      >
        <ItemContent>
          <ItemStatus status={status} />
          {/* Two lines, not the template's one: a task title IS the card, and the board is read
              for it. The clamp sits on the block span itself (never a grid item, which would
              blockify it and drop the clamp). */}
          <ItemName
            name={task.title}
            noWrap={false}
            title={task.title}
            sx={{ pr: 2.5, display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical", overflow: "hidden", overflowWrap: "anywhere" }}
          />

          {task.deadlineTone ? (
            <Box sx={{ mt: 1.5 }}>
              <DeadlineClock task={task} compact deadlineWord={deadlineWord} />
            </Box>
          ) : null}

          <Typography component="span" variant="caption" noWrap title={task.raisedBy || undefined} sx={{ display: "block", mt: 1, color: "text.secondary" }}>
            {copy(pageContract, "column.raised_by")} {task.raisedBy || placeholder}
          </Typography>

          <ItemInfo
            attachments={task.attachments}
            assignee={[{ id: task.assigneeUserID || "assignee", name: task.assignee || placeholder, initial: initials(task.assignee) }]}
            assigneeTitle={task.assignee || undefined}
          >
            <Box component="b" sx={{ typography: "caption", fontWeight: "fontWeightBold", color: "text.primary" }}>
              {task.number}
            </Box>
            <Box component="span" sx={{ typography: "caption", color: "text.disabled" }}>
              {task.age}
            </Box>
          </ItemInfo>
        </ItemContent>
      </Box>
    </KanbanItemRoot>
  );
}
