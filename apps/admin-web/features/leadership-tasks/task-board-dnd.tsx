"use client";

import { FOUR_LANE_COLUMN_WIDTH } from "@/components/app/kanban/board-layout";
import { visuallyHidden } from "@mui/utils";
import { useCallback, useRef, useState, useTransition } from "react";
import { createPortal } from "react-dom";
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  pointerWithin,
  rectIntersection,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type Announcements,
  type CollisionDetection,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
  type KeyboardCoordinateGetter,
} from "@dnd-kit/core";
import { faro } from "@grafana/faro-web-sdk";
import Box from "@mui/material/Box";
import Alert from "@mui/material/Alert";
import Typography from "@mui/material/Typography";
import { varAlpha } from "minimal-shared/utils";

import { KanbanBoard } from "@/components/app/kanban";
import { Label } from "@/components/minimal/label";
import { ColumnList, ColumnRoot, ColumnWrapper, kanbanColumnState } from "@/components/app/kanban/column-styles";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import { TaskBoardCard } from "./task-board-card";
import { rowFromTask, type TaskRow } from "./task-row";
import {
  currentTaskRowVersion,
  publishTaskRow,
  runTaskWrite,
  taskRowPatch,
  useTaskRowsVersion,
} from "./task-row-store";
import { changeLeadershipTaskStatusInPlaceAction, loadLeadershipTaskAction, type StatusChangeResult } from "./actions";
import { refusalSentence } from "./task-feedback-copy";
import type { TaskBoardColumn } from "./task-url";

/**
 * The board's COLUMN TRACK, and the only place a status may be changed by dragging.
 *
 * ── WHY DND-KIT (POINTER, TOUCH AND KEYBOARD SENSORS), NOT HTML5 DRAG-AND-DROP ────────────────
 * The board used native HTML5 DnD (`draggable` + `onDragStart`). Two defects came with it
 * (Ravi, 2026-09-30): touch browsers never fire HTML5 drag events, so a card could not be moved
 * on a phone at all, and the browser's drag image is a snapshot of the `<a>` alone, which has no
 * background, so the text floated transparently over the other cards. `@dnd-kit/core` fixes both
 * at the root:
 *   - three sensors: the MOUSE starts a drag after 5px of travel (so a click still opens the
 *     card), TOUCH after a 200ms long-press with 5px tolerance (so a tap still opens the card and
 *     a swipe still scrolls the page or the column track -- the template kanban's constraint),
 *     and the KEYBOARD on Space (Enter stays the link's own "open"), arrows jumping column to
 *     column, Space/Enter to drop, Escape to cancel;
 *   - a `DragOverlay`: the dragged card is the SAME `TaskBoardCard`, rendered on the paper
 *     background with the template's lift (shadow, slight rotation and scale) in a portal, while
 *     the source slot keeps the template `--dragging` placeholder state (grayscale, faded).
 * The native anchor drag is switched off (`draggable={false}`), so a card never also ships a URL
 * drag. guard: task-board-touch-dnd (task-board-dnd.test.mjs + e2e/task-board-dnd.e2e.mjs).
 *
 * ── WHY A DROP CAN NEVER JUST "MOVE THE CARD" ─────────────────────────────────────────────────
 * A status change goes through `changeLeadershipTaskStatusAction`, which is fenced on
 * `row_version` and carries a client-minted idempotency key. Three things can refuse it, and
 * they are three different sentences to a human, not one "something went wrong":
 *   - 409 `version_conflict` — the `row_version` this page was served with is stale, because the
 *     phone or another leader touched the task first;
 *   - 409 `task_closed` — the task is in a closed state and no longer moves;
 *   - a permission refusal (`not_assignee` / `not_raiser`) — authority here is per-actor: the
 *     assignee walks the ladder, the raiser cancels.
 * So the drop is OPTIMISTIC and IN PLACE -- the same write the drawer's status menu makes
 * (`changeLeadershipTaskStatusInPlaceAction`, which RETURNS the task): the card moves at once
 * through the row store, the backend's row replaces it when it lands, and a refusal puts the card
 * back in its column with the sentence for that refusal under the board. There is no redirect
 * and no route re-render (docs/decisions/admin-web-interaction-patterns.md: a click costs what it
 * changes). A `version_conflict` re-reads the task and publishes where it IS now, so the board
 * shows the other actor's result, not a stale one. The write is serialised behind any other write
 * to the same task (`runTaskWrite`) and reads its `row_version` fence when it is sent.
 *
 * ── WHY ONLY SOME COLUMNS ACCEPT A CARD ───────────────────────────────────────────────────────
 * There is no transition matrix in this file. `task.statusOptions` is `status_options` off the
 * row, which is `domain.StatusOptionsFor(task, actor)` — the backend's own list of the moves THIS
 * actor may make on THIS task, and the very list `domain.CheckTransition` validates the write
 * against. A column is droppable exactly when its key appears in it, so the board and the write
 * cannot disagree, and a task whose options are empty cannot be dragged at all.
 *
 * ── PHONE WIDTH AND THE WEBVIEW ───────────────────────────────────────────────────────────────
 * Below the page's 760px breakpoint the columns sit in a horizontally snapping track. A drag is
 * a long-press, so the page and the track still scroll under a plain swipe; while a card is held
 * the snap is lifted so dnd-kit's auto-scroll can carry it to an off-screen column, and the card
 * suppresses the iOS link callout and text selection that a long-press would otherwise raise.
 *
 * ── KEYBOARD AND SCREEN READER ────────────────────────────────────────────────────────────────
 * The drag is an ACCELERATOR, never the only affordance. Every card stays the same `?task=<uuid>`
 * link it was, reachable by Tab and openable with Enter, and the detail panel it opens carries
 * `TaskStatusActions` — one plain submit button per legal transition, the same server action this
 * component posts. dnd-kit's announcements are fed our own task numbers and column labels (never
 * a raw id), and a polite live region announces the move in flight.
 */

/**
 * The Faro event for this surface's primary action (TELEMETRY_GUARDRAILS.md §2.2: a new
 * interactive surface wires its own analytics event). A module constant, never an inline string.
 * It records the attempt and its from/to, because the interesting number is how often a drag is
 * REFUSED — the refusal itself then arrives on the redirect the banner reads.
 */
const BOARD_DRAG_EVENT = "leadership_task_board_drag_status_change";

/**
 * The sensors' activation constraints (the template kanban's numbers). A mouse drag starts after
 * 5px of travel, so a click is still a click; a touch drag after a 200ms long-press that has not
 * moved more than 5px, so a tap opens the card and a swipe scrolls.
 */
export const MOUSE_ACTIVATION = { distance: 5 } as const;
export const TOUCH_ACTIVATION = { delay: 200, tolerance: 5 } as const;
/** Space picks a card up; Enter is left to the link, which opens the card. */
const KEYBOARD_CODES = { start: ["Space"], cancel: ["Escape"], end: ["Space", "Enter"] };

export type BoardColumnMeta = {
  key: TaskBoardColumn;
  label: string;
  /** The whole-list total, or null when the backend publishes none for this status. */
  total: number | null;
  emptyMessage: string;
  focusHref: string | null;
};

type PendingMove = { taskID: string; to: TaskBoardColumn };

export function TaskBoardColumns({
  pageContract,
  columns,
  rows: serverRows,
  cardHrefs,
  selectedTaskID,
  activeFilter,
  returnTo,
}: {
  pageContract: AdminUiPageContract;
  columns: BoardColumnMeta[];
  rows: TaskRow[];
  /** task id -> the `?task=<uuid>` deep link, minted server-side where the URL vocabulary lives. */
  cardHrefs: Record<string, string>;
  selectedTaskID?: string;
  activeFilter: string;
  /** The contract's dash. Accepted for the callers that pass it; no pill renders it any more
   *  (Gate-1 #4, #14 -- see the pill below), so it is not read here. */
  placeholder?: string;
  /** Retired: the drop writes IN PLACE (`changeLeadershipTaskStatusInPlaceAction`) and no longer
   *  posts the redirecting action. Accepted and ignored so an older caller still type-checks. */
  action?: (formData: FormData) => void | Promise<void>;
  returnTo: string;
}) {
  const [, startTransition] = useTransition();
  /**
   * The rows as the browser knows them: a status changed in the drawer is published to the row
   * store and the card moves lane HERE, without the route re-rendering. The pills follow: a
   * published move is +1 / -1 against the server's whole-list totals.
   */
  useTaskRowsVersion();
  const rows = serverRows.map((row) => {
    const patch = taskRowPatch(row.id);
    // Applied unless the server row is strictly newer (an optimistic move has no version).
    return patch && !(typeof patch.rowVersion === "number" && patch.rowVersion < row.rowVersion)
      ? { ...row, ...patch }
      : row;
  });
  const totalDelta = (columnKey: string): number => {
    let delta = 0;
    for (let i = 0; i < rows.length; i += 1) {
      const before = serverRows[i].status;
      const after = rows[i].status;
      if (before === after) continue;
      if (after === columnKey) delta += 1;
      if (before === columnKey) delta -= 1;
    }
    return delta;
  };
  /** The move on the wire, for the card's saving state and the live announcement. */
  const [pendingMove, setPendingMove] = useState<PendingMove | null>(null);
  /** The sentence for a refused drop, under the board (never a page banner, never a raw code). */
  const [refusal, setRefusal] = useState("");
  /** The card under the reader's hand, so the columns can say whether they would accept it. */
  const [draggingTaskID, setDraggingTaskID] = useState<string | null>(null);
  const [overColumn, setOverColumn] = useState<string | null>(null);

  const draggingTask = draggingTaskID
    ? rows.find((task) => task.id === draggingTaskID)
    : undefined;

  const statusOf = (task: TaskRow): string =>
    pendingMove && pendingMove.taskID === task.id ? pendingMove.to : task.status;

  const move = (task: TaskRow, to: TaskBoardColumn) => {
    // The key is minted HERE, in the drop handler, and never during server render: a
    // server-rendered key is part of the HTML, so a back navigation replays it and the second,
    // legitimate change is swallowed as a duplicate. This is the same shape
    // `task-write-forms.tsx` mints in its click handler, and the same prefix, so a drop and a
    // button press are the same command to the backend.
    const idempotencyKey = `admin-web-leadership-task-status:${task.id}:${to}:${crypto.randomUUID()}`;
    const formData = new FormData();
    formData.set("idempotency_key", idempotencyKey);
    formData.set("return_to", returnTo);
    formData.set("task_id", task.id);
    formData.set("status", to);
    // The status this board was SHOWING for the card, so a refusal can say whether the task moved
    // under the reader or was changed some other way. Not an input to the write; see `actions.ts`.
    formData.set("from_status", task.status);
    try {
      faro.api?.pushEvent(BOARD_DRAG_EVENT, {
        from: task.status,
        to,
        row_version: String(task.rowVersion),
      });
    } catch {
      // Telemetry must never break a write.
    }
    const chosen = task.statusOptions.find((option) => option.key === to);
    const before = { status: task.status, statusLabel: task.statusLabel };
    const serverRow = serverRows.find((row) => row.id === task.id);
    setRefusal("");
    setPendingMove({ taskID: task.id, to });
    // OPTIMISTIC: the card changes lane now, through the same row store the drawer publishes to.
    publishTaskRow(task.id, { status: to, statusLabel: chosen?.label ?? task.statusLabel });
    startTransition(async () => {
      let result: StatusChangeResult;
      try {
        result = await runTaskWrite(task.id, () => {
          // The fence is read when the write is SENT, off the row as the browser knows it then.
          formData.set("row_version", String(currentTaskRowVersion(task.id, serverRow?.rowVersion ?? task.rowVersion)));
          return changeLeadershipTaskStatusInPlaceAction(formData);
        });
      } catch {
        result = { ok: false, code: "network" };
      }
      setPendingMove(null);
      if (result.ok) {
        publishTaskRow(task.id, rowFromTask(result.task));
        return;
      }
      // ROLLBACK: the card goes back where it was, then (on a conflict) to where it IS now.
      publishTaskRow(task.id, before);
      if (result.code === "version_conflict" || result.statusNow) {
        const fresh = await loadLeadershipTaskAction(task.id).catch(() => null);
        if (fresh?.ok) publishTaskRow(task.id, rowFromTask(fresh.task));
      }
      setRefusal(
        refusalSentence((k, fb) => copy(pageContract, k, fb), result.code, result.statusNow, result.who) ||
          copy(pageContract, "action.failed_message", "Action could not be completed."),
      );
    });
  };

  const legalFor = (columnKey: string): boolean =>
    Boolean(draggingTask?.statusOptions.some((option) => option.key === columnKey)) &&
    statusOf(draggingTask!) !== columnKey;

  const announcement = pendingMove
    ? `${copy(pageContract, "board.drag_moving", "Moving")} ${
        rows.find((task) => task.id === pendingMove.taskID)?.number ?? ""
      } ${columns.find((column) => column.key === pendingMove.to)?.label ?? pendingMove.to}`
    : "";

  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: MOUSE_ACTIVATION }),
    useSensor(TouchSensor, { activationConstraint: TOUCH_ACTIVATION }),
    useSensor(KeyboardSensor, { keyboardCodes: KEYBOARD_CODES, coordinateGetter: columnKeyboardCoordinates }),
  );
  const suppressNextClick = useClickSuppressor();

  const taskNumber = (id: string | number | undefined): string =>
    rows.find((task) => task.id === id)?.number ?? "";
  const columnLabel = (id: string | number | undefined): string =>
    columns.find((column) => column.key === id)?.label ?? "";
  // dnd-kit's own live region, fed our numbers and labels: never a raw task UUID, never English
  // sentences the page contract does not serve.
  const announcements: Announcements = {
    onDragStart: ({ active }) => taskNumber(active.id),
    onDragOver: ({ active, over }) => (over ? `${taskNumber(active.id)} ${columnLabel(over.id)}` : taskNumber(active.id)),
    onDragEnd: ({ active, over }) => (over ? `${taskNumber(active.id)} ${columnLabel(over.id)}` : taskNumber(active.id)),
    onDragCancel: ({ active }) => taskNumber(active.id),
  };

  const endDrag = () => {
    setDraggingTaskID(null);
    setOverColumn(null);
    suppressNextClick();
  };

  return (
    <DndContext
      id="leadership-task-board"
      sensors={sensors}
      collisionDetection={pointerThenRect}
      accessibility={{ announcements }}
      onDragStart={(event: DragStartEvent) => {
        setRefusal("");
        setDraggingTaskID(String(event.active.id));
      }}
      onDragOver={(event: DragOverEvent) => setOverColumn(event.over ? String(event.over.id) : null)}
      onDragCancel={endDrag}
      onDragEnd={(event: DragEndEvent) => {
        const taskID = String(event.active.id);
        const overKey = event.over ? String(event.over.id) : null;
        endDrag();
        const task = rows.find((row) => row.id === taskID);
        const column = columns.find((candidate) => candidate.key === overKey);
        if (!task || !column) return;
        if (!task.statusOptions.some((option) => option.key === column.key)) return;
        if (statusOf(task) === column.key) return;
        move(task, column.key);
      }}
    >
      {/* Template sections/kanban: the KanbanBoard track, one template column per status
          (ColumnWrapper + ColumnRoot, the round count Label and the h6 name in the column
          toolbar, the ColumnList of items); a legal drop target takes the template's
          column-over / task-over state. */}
      <KanbanBoard
        className="ltb-cols"
        role="group"
        aria-label={copy(pageContract, "board.aria", "Tasks by status")}
        data-dragging={draggingTask ? "true" : undefined}
        sx={{
          "--kanban-column-width": FOUR_LANE_COLUMN_WIDTH,
          overscrollBehaviorX: "contain",
          // The snap is lifted while a card is held, so auto-scroll can carry it across columns.
          scrollSnapType: draggingTask ? "none" : { xs: "x mandatory", md: "none" },
          "& > section": { scrollSnapAlign: "start" },
        }}
      >
          {columns.map((column) => {
            const cards = rows.filter((task) => statusOf(task) === column.key);
            const droppable = legalFor(column.key);
            const over = droppable && overColumn === column.key;
            const count = column.total === null ? cards.length : Math.max(0, column.total + totalDelta(column.key));
            return (
              <DroppableColumn
                key={column.key}
                columnKey={column.key}
                droppable={droppable}
                className={`ltb-col ltb-col-${column.key}${
                  activeFilter === column.key ? " is-focused" : ""
                }${droppable ? " ltb-drop-ok" : ""}${over ? " ltb-drop-over" : ""}${
                  draggingTask && !droppable ? " ltb-drop-no" : ""
                }`}
              >
                <ColumnRoot
                  className={over ? kanbanColumnState.taskOver : draggingTask && droppable ? kanbanColumnState.columnOver : draggingTask ? kanbanColumnState.dragging : undefined}
                  // Tailwind preflight gives ::before `border-style: solid`; the template's idle
                  // 1px pseudo border relied on the default `none`, so name it (the drag states
                  // set their own style with a higher specificity). guard: preflight-pseudo-border
                  sx={{ flexGrow: 1, "&::before": { borderStyle: "none" } }}
                >
                  <Box
                    component="header"
                    className="ltb-colhd"
                    sx={{ display: "flex", alignItems: "center", gap: 1, pt: "var(--kanban-column-pt)", px: "var(--kanban-column-px)" }}
                  >
                    {/* A count never reads "—" (Gate-1 #4, #14): where the list query publishes
                        no whole-list total for a column -- the overdue lens, and Cancelled --
                        the count is the cards on this page and its tooltip says so. */}
                    <Label
                      className="ltb-colcount"
                      title={
                        column.total === null
                          ? copy(
                              pageContract,
                              "board.total_on_page",
                              "The whole-list total for this column is not published; this counts the cards on this page.",
                            )
                          : undefined
                      }
                      sx={(theme) => ({ borderRadius: "50%", borderColor: varAlpha(theme.vars.palette.grey["500Channel"], 0.24) })}
                    >
                      {count}
                    </Label>
                    <Typography component="span" variant="subtitle1" noWrap className="ltb-colname" sx={{ flexGrow: 1, minWidth: 0 }}>
                      {column.label}
                    </Typography>
                  </Box>
                  <ColumnList className="ltb-colbd">
                    {cards.length ? (
                      cards.map((task) => (
                        <DraggableTaskCard
                          key={task.id}
                          task={task}
                          pageContract={pageContract}
                          href={cardHrefs[task.id] ?? ""}
                          selected={task.id === selectedTaskID}
                          canDrag={task.statusOptions.length > 0}
                          dragging={draggingTaskID === task.id}
                          pending={pendingMove?.taskID === task.id}
                        />
                      ))
                    ) : column.emptyMessage ? (
                      // An empty template column is the bare list: the message is for screen
                      // readers only, never a dashed "Nothing here" box (TR1-#24).
                      <Box component="li" className="ltb-colempty" sx={visuallyHidden}>
                        {column.emptyMessage}
                      </Box>
                    ) : null}
                  </ColumnList>
                </ColumnRoot>
              </DroppableColumn>
            );
          })}
      </KanbanBoard>

      {/* The dragged card itself: the SAME card component on the paper background with the
          template's lift, in a portal so no scroller or stacking context clips it. Never the
          browser's transparent snapshot of the link. */}
      <BoardDragOverlay>
        {draggingTask ? (
          <TaskBoardCard
            task={draggingTask}
            pageContract={pageContract}
            href={cardHrefs[draggingTask.id] ?? ""}
            selected={false}
            draggable={false}
            overlay
          />
        ) : null}
      </BoardDragOverlay>

      {refusal ? (
        <Alert severity="error" role="alert" data-testid="ltb-refusal" sx={{ mt: 2 }}>
          {refusal}
        </Alert>
      ) : null}
      {/* Not `aria-grabbed` (deprecated): the move in flight is announced as text instead. */}
      <Box component="p" role="status" aria-live="polite" sx={visuallyHidden}>
        {announcement}
      </Box>
    </DndContext>
  );
}

/** One status column as a dnd-kit drop target; an illegal column is disabled, so it never collides. */
function DroppableColumn({
  columnKey,
  droppable,
  className,
  children,
}: {
  columnKey: TaskBoardColumn;
  droppable: boolean;
  className: string;
  children: React.ReactNode;
}) {
  const { setNodeRef } = useDroppable({ id: columnKey, disabled: !droppable });
  return (
    <ColumnWrapper ref={setNodeRef} className={className} data-column={columnKey}>
      {children}
    </ColumnWrapper>
  );
}

/** One card as a dnd-kit draggable: the `<li>` is measured, the `<a>` is the activator. */
function DraggableTaskCard({
  task,
  canDrag,
  ...card
}: Omit<React.ComponentProps<typeof TaskBoardCard>, "draggable" | "dragHandle" | "overlay"> & { canDrag: boolean }) {
  const { setNodeRef, setActivatorNodeRef, listeners, attributes } = useDraggable({ id: task.id, disabled: !canDrag });
  return (
    <TaskBoardCard
      {...card}
      task={task}
      draggable={canDrag}
      dragHandle={{
        rootRef: setNodeRef,
        activatorRef: setActivatorNodeRef,
        listeners: canDrag ? listeners : undefined,
        describedBy: canDrag ? attributes["aria-describedby"] : undefined,
      }}
    />
  );
}

/** The overlay wrapper only carries the grabbing hand; the card inside draws the template lift. */
function BoardDragOverlay({ children }: { children: React.ReactNode }) {
  const overlay = (
    <DragOverlay className="ltb-drag-overlay" zIndex={1500}>
      {children ? <Box sx={{ cursor: "grabbing" }}>{children}</Box> : null}
    </DragOverlay>
  );
  return typeof document === "undefined" ? overlay : createPortal(overlay, document.body);
}

/**
 * Pointer first (mouse and touch: the column under the finger wins, even when the lifted card
 * overlaps two), the rectangle for the keyboard, which has no pointer.
 */
const pointerThenRect: CollisionDetection = (args) => {
  const hits = pointerWithin(args);
  return hits.length ? hits : rectIntersection(args);
};

/**
 * The keyboard moves a held card COLUMN to column (Arrow Right / Down forward, Left / Up back),
 * not 25px at a time: the target is the next enabled (legal) drop column in reading order.
 */
const columnKeyboardCoordinates: KeyboardCoordinateGetter = (event, { context, currentCoordinates }) => {
  const forward = event.code === "ArrowRight" || event.code === "ArrowDown";
  const back = event.code === "ArrowLeft" || event.code === "ArrowUp";
  if (!forward && !back) return undefined;
  event.preventDefault();
  const { collisionRect, droppableRects, droppableContainers } = context;
  if (!collisionRect) return currentCoordinates;
  const targets = droppableContainers
    .getEnabled()
    .map((container) => droppableRects.get(container.id))
    .filter((rect): rect is NonNullable<typeof rect> => Boolean(rect))
    .sort((a, b) => a.left - b.left || a.top - b.top);
  const centre = collisionRect.left + collisionRect.width / 2;
  const next = forward
    ? targets.find((rect) => rect.left > centre)
    : [...targets].reverse().find((rect) => rect.right < centre);
  if (!next) return currentCoordinates;
  return { x: next.left + (next.width - collisionRect.width) / 2, y: next.top + 56 };
};

/**
 * A drag ends with a pointer-up, and the browser may follow it with a click on the card the drag
 * started on (a drop back into the card's own slot). That click must not open the drawer, so a
 * click in the next 100ms is swallowed; a later, real click is untouched.
 */
const CLICK_SUPPRESS_MS = 100;

function useClickSuppressor(): () => void {
  const armed = useRef<((event: MouseEvent) => void) | null>(null);
  return useCallback(() => {
    if (armed.current) window.removeEventListener("click", armed.current, true);
    const swallow = (event: MouseEvent) => {
      event.preventDefault();
      event.stopPropagation();
    };
    armed.current = swallow;
    window.addEventListener("click", swallow, true);
    window.setTimeout(() => {
      window.removeEventListener("click", swallow, true);
      if (armed.current === swallow) armed.current = null;
    }, CLICK_SUPPRESS_MS);
  }, []);
}
