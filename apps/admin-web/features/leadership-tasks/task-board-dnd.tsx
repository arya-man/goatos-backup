"use client";

import { visuallyHidden } from "@mui/utils";
import { useEffect, useState, useTransition } from "react";
import { faro } from "@grafana/faro-web-sdk";
import Box from "@mui/material/Box";
import Alert from "@mui/material/Alert";
import Typography from "@mui/material/Typography";
import { varAlpha } from "minimal-shared/utils";

import { KanbanBoard } from "@/components/minimal/kanban";
import { Label } from "@/components/minimal/label";
import { ColumnList, ColumnRoot, ColumnWrapper, kanbanColumnState } from "@/components/minimal/sections/kanban/column/styles";

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
 * ── WHY HTML5 DRAG-AND-DROP, AND NO LIBRARY ───────────────────────────────────────────────────
 * The repo has no drag library and `@base-ui/react` is a dead dependency; neither is being woken
 * up for this. Between the two dependency-free options:
 *   - native HTML5 DnD wins because the cards ALREADY are `<a>` elements, which the browser
 *     drags natively anyway. Without this component the anchors are draggable and drop a URL
 *     somewhere unhelpful, so the choice is "own the drag" or "explicitly turn it off" — there is
 *     no neutral option. It also gives the drag image, the `no-drop` cursor over an illegal
 *     column and the column scroll-during-drag for free, and it fires no pointer handler while
 *     the reader is merely scrolling the page.
 *   - pointer events would mean re-implementing hit-testing against `.ltb-colbd`, which is a
 *     height-capped scroller inside a horizontally-scrolling track — two nested scroll contexts
 *     to autoscroll by hand, plus a touch-action fight with the page.
 * HTML5 DnD does not fire for touch, which on this page is a FEATURE (see the phone note below),
 * not a gap: the non-drag path is the real path there.
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
 * ── PHONE WIDTH: DRAG IS OFF, DELIBERATELY ────────────────────────────────────────────────────
 * Below the page's 760px breakpoint the columns STACK, and this page is opened at phone width in
 * the WhatsApp in-app webview, where a long-press drag fights the page scroll and a drop target
 * is off-screen behind that scroll. So there the cards stay plain links (`draggable={false}`,
 * which also suppresses the browser's own anchor drag) and the status actions in the detail panel
 * are the path. The gate is a `matchMedia` on `(min-width: 761px) and (pointer: fine)` evaluated
 * in an effect, so the server-rendered HTML is the non-drag one and a narrow or touch client
 * never upgrades.
 *
 * ── KEYBOARD AND SCREEN READER ────────────────────────────────────────────────────────────────
 * The drag is an ACCELERATOR, never the only affordance, and it adds no ARIA theatre:
 * `aria-grabbed` is deprecated and a fake listbox over the columns would be a worse lie than the
 * truth, which is that the real control is elsewhere. Every card stays the same `?task=<uuid>`
 * link it was, reachable by Tab and openable with Enter, and the detail panel it opens carries
 * `TaskStatusActions` — one plain submit button per legal transition, the same server action this
 * component posts. The hint line below the columns says so in words (desktop-only, since it
 * describes a desktop affordance), and a polite live region announces the move in flight so a
 * screen-reader user who does drag is not left guessing.
 */

/**
 * The drag payload's own media type. A private type rather than `text/plain` so a card dropped
 * outside the board (an address bar, a text field) carries nothing this component minted, and so
 * a drag that started elsewhere can never be read as a task.
 */
const DRAG_MIME = "application/x-mesha-leadership-task";

/**
 * The Faro event for this surface's primary action (TELEMETRY_GUARDRAILS.md §2.2: a new
 * interactive surface wires its own analytics event). A module constant, never an inline string.
 * It records the attempt and its from/to, because the interesting number is how often a drag is
 * REFUSED — the refusal itself then arrives on the redirect the banner reads.
 */
const BOARD_DRAG_EVENT = "leadership_task_board_drag_status_change";

/** Drag is offered above the page's single breakpoint, and only to a precise pointer. */
const DRAG_MEDIA_QUERY = "(min-width: 761px) and (pointer: fine)";

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
  const dragCapable = useDragCapable();
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

  return (
    <>
      {/* Template sections/kanban: the KanbanBoard track, one template column per status
          (ColumnWrapper + ColumnRoot, the round count Label and the h6 name in the column
          toolbar, the ColumnList of items); a legal drop target takes the template's
          column-over / task-over state. */}
      <KanbanBoard
        className="ltb-cols"
        role="group"
        aria-label={copy(pageContract, "board.aria", "Tasks by status")}
        sx={{
          "--kanban-column-width": { xs: "86vw", sm: "clamp(calc(var(--sp-5) * 6), calc((100% - 3 * var(--kanban-column-gap)) / 4), var(--kanban-col-w))" },
          overscrollBehaviorX: "contain",
          scrollSnapType: { xs: "x mandatory", md: "none" },
          "& > section": { scrollSnapAlign: "start" },
        }}
      >
          {columns.map((column) => {
            const cards = rows.filter((task) => statusOf(task) === column.key);
            const droppable = legalFor(column.key);
            const over = droppable && overColumn === column.key;
            const count = column.total === null ? cards.length : Math.max(0, column.total + totalDelta(column.key));
            return (
              <ColumnWrapper
                key={column.key}
                className={`ltb-col ltb-col-${column.key}${
                  activeFilter === column.key ? " is-focused" : ""
                }${droppable ? " ltb-drop-ok" : ""}${over ? " ltb-drop-over" : ""}${
                  draggingTask && !droppable ? " ltb-drop-no" : ""
                }`}
                onDragOver={(event) => {
                  if (!droppable) return;
                  event.preventDefault();
                  event.dataTransfer.dropEffect = "move";
                  if (overColumn !== column.key) setOverColumn(column.key);
                }}
                onDragLeave={(event) => {
                  if (event.currentTarget.contains(event.relatedTarget as Node | null)) return;
                  setOverColumn((current) => (current === column.key ? null : current));
                }}
                onDrop={(event) => {
                  event.preventDefault();
                  setOverColumn(null);
                  const taskID =
                    draggingTaskID || event.dataTransfer.getData(DRAG_MIME);
                  const task = rows.find((row) => row.id === taskID);
                  setDraggingTaskID(null);
                  if (!task) return;
                  if (!task.statusOptions.some((option) => option.key === column.key)) return;
                  if (task.status === column.key) return;
                  move(task, column.key);
                }}
              >
                <ColumnRoot
                  className={over ? kanbanColumnState.taskOver : draggingTask && droppable ? kanbanColumnState.columnOver : draggingTask ? kanbanColumnState.dragging : undefined}
                  sx={{ flexGrow: 1 }}
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
                        <TaskBoardCard
                          key={task.id}
                          task={task}
                          pageContract={pageContract}
                          href={cardHrefs[task.id] ?? ""}
                          selected={task.id === selectedTaskID}
                          draggable={dragCapable && task.statusOptions.length > 0}
                          dragging={draggingTaskID === task.id}
                          pending={pendingMove?.taskID === task.id}
                          onDragStart={(event) => {
                            event.dataTransfer.setData(DRAG_MIME, task.id);
                            event.dataTransfer.effectAllowed = "move";
                            setDraggingTaskID(task.id);
                          }}
                          onDragEnd={() => {
                            setDraggingTaskID(null);
                            setOverColumn(null);
                          }}
                        />
                      ))
                    ) : column.emptyMessage ? (
                      <Box
                        component="li"
                        className="ltb-colempty"
                        sx={{ p: 2, border: 1, borderStyle: "dashed", borderColor: "divider", borderRadius: "var(--kanban-item-radius)", typography: "body2", color: "text.disabled" }}
                      >
                        {column.emptyMessage}
                      </Box>
                    ) : null}
                  </ColumnList>
                </ColumnRoot>
              </ColumnWrapper>
            );
          })}
      </KanbanBoard>

      {refusal ? (
        <Alert severity="error" role="alert" data-testid="ltb-refusal" sx={{ mt: 2 }}>
          {refusal}
        </Alert>
      ) : null}
      {/* Not `aria-grabbed` (deprecated): the move in flight is announced as text instead. */}
      <Box component="p" role="status" aria-live="polite" sx={visuallyHidden}>
        {announcement}
      </Box>
    </>
  );
}

/**
 * Whether this client may drag at all.
 *
 * Starts FALSE so the server-rendered markup is the non-drag markup — the phone and the WhatsApp
 * webview get plain links with no hydration flip to correct — and is raised only by the effect on
 * a client that is both wide enough for side-by-side columns and driven by a precise pointer.
 */
function useDragCapable(): boolean {
  const [capable, setCapable] = useState(false);
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const query = window.matchMedia(DRAG_MEDIA_QUERY);
    const sync = () => setCapable(query.matches);
    sync();
    query.addEventListener("change", sync);
    return () => query.removeEventListener("change", sync);
  }, []);
  return capable;
}
