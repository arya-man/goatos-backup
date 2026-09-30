"use client";

import Link from "@/components/no-prefetch-link";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import type { Theme } from "@mui/material/styles";
import type { DraggableSyntheticListeners } from "@dnd-kit/core";

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
 *
 * Drag (task-board-dnd.tsx) is dnd-kit: `dragHandle` hands in the measured node, the activator
 * (the link) and the sensor listeners. The link's NATIVE drag is always off (`draggable={false}`
 * on the anchor): touch browsers never fire it and its drag image is a transparent snapshot.
 * `overlay` renders the same card, not a link, as the DragOverlay: the paper background with the
 * template lift (shadow, a slight tilt and scale). guard: task-board-touch-dnd
 */
export type TaskBoardCardDragHandle = {
  rootRef: (node: HTMLElement | null) => void;
  activatorRef: (node: HTMLElement | null) => void;
  listeners?: DraggableSyntheticListeners;
  describedBy?: string;
};

/** The template's lifted card while it is held (sections/kanban ItemPreview + the drag lift). */
const OVERLAY_SX = (theme: Theme) => ({
  backgroundColor: theme.vars.palette.background.paper,
  boxShadow: theme.vars.customShadows.z24,
  transform: "rotate(3deg) scale(1.03)",
  cursor: "grabbing",
  pointerEvents: "none",
  "&:hover": { boxShadow: theme.vars.customShadows.z24 },
});
export function TaskBoardCard({
  task,
  pageContract,
  href,
  selected,
  draggable,
  dragging = false,
  pending = false,
  dragHandle,
  overlay = false,
}: {
  task: TaskRow;
  pageContract: AdminUiPageContract;
  href: string;
  selected: boolean;
  draggable: boolean;
  dragging?: boolean;
  pending?: boolean;
  dragHandle?: TaskBoardCardDragHandle;
  /** The DragOverlay copy of the card: no link, hidden from assistive tech, lifted on paper. */
  overlay?: boolean;
}) {
  const deadlineWord = copy(pageContract, "label.deadline");
  const placeholder = copy(pageContract, "label.placeholder", "—");
  const status = task.deadlineTone === "over" ? "high" : task.deadlineTone === "near" ? "medium" : null;
  return (
    <KanbanItemRoot
      ref={overlay ? undefined : dragHandle?.rootRef}
      as={overlay ? "div" : undefined}
      aria-hidden={overlay ? true : undefined}
      className={`ltb-card-root${dragging ? " --dragging" : ""}${overlay ? " --overlay" : ""}`}
      data-selected={selected ? "true" : undefined}
      data-board-draggable={overlay ? undefined : draggable ? "true" : "false"}
      sx={[{
        cursor: draggable ? "grab" : "pointer",
        // A held card: the template `--dragging` placeholder stays in the source slot.
        ...(dragging ? { filter: "grayscale(1)", "& > *": { opacity: 0.4 } } : {}),
        // A long-press must not raise the iOS link callout or select the card's text.
        ...(draggable ? { WebkitTouchCallout: "none", userSelect: "none", touchAction: "manipulation" } : {}),
      }, ...(overlay ? [OVERLAY_SX] : [])]}
    >
      <Box
        component={overlay ? "div" : Link}
        {...(overlay ? {} : { href, scroll: false })}
        ref={overlay ? undefined : dragHandle?.activatorRef}
        className={`ltb-card${overlay ? "-overlay" : ""}${selected ? " is-selected" : ""}${dragging ? " is-dragging" : ""}${pending ? " is-moving" : ""}`}
        aria-current={selected ? "true" : undefined}
        aria-label={overlay ? undefined : `${copy(pageContract, "action.open_task")} ${task.number}: ${task.title}`}
        aria-describedby={overlay ? undefined : dragHandle?.describedBy}
        draggable={false}
        {...(overlay ? {} : dragHandle?.listeners)}
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
