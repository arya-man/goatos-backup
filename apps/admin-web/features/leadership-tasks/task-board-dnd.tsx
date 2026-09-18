"use client";

import { useEffect, useOptimistic, useState, useTransition } from "react";
import { faro } from "@grafana/faro-web-sdk";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import { TaskBoardCard } from "./task-board-card";
import type { TaskRow } from "./task-row";
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
 * So the drop is optimistic for the ~one frame a reader can feel, and the SERVER is the
 * reconciliation: the action redirects with `task_status`/`task_code` on the URL and
 * `revalidatePath`s the desk, so the board re-renders from a fresh read and `TaskFeedbackBanner`
 * says which of the three happened. `useOptimistic` drops the provisional move the moment that
 * transition settles, which means a refusal puts the card back in its original column without a
 * single line of undo code — the card's position is always the last server truth plus, briefly,
 * the move in flight. A `version_conflict` is therefore already the "someone else changed this —
 * refreshing" story: the redirect IS the refresh, and the board the reader is left looking at is
 * the other actor's result, not a stale board.
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
  rows,
  cardHrefs,
  selectedTaskID,
  activeFilter,
  action,
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
  action: (formData: FormData) => void | Promise<void>;
  returnTo: string;
}) {
  const dragCapable = useDragCapable();
  const [, startTransition] = useTransition();
  /**
   * The move in flight. Optimistic, so React itself discards it when the transition that set it
   * settles — which is the whole revert-on-refusal mechanism (see the file comment).
   */
  const [pendingMove, setPendingMove] = useOptimistic<PendingMove | null, PendingMove | null>(
    null,
    (_current, next) => next,
  );
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
    // The fence comes off the ROW being rendered, not off a number captured at page load.
    formData.set("row_version", String(task.rowVersion));
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
    startTransition(async () => {
      setPendingMove({ taskID: task.id, to });
      await action(formData);
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
      <div
        className="ltb-scroll"
        role="group"
        aria-label={copy(pageContract, "board.aria", "Tasks by status")}
      >
        <div className="ltb-cols">
          {columns.map((column) => {
            const cards = rows.filter((task) => statusOf(task) === column.key);
            const droppable = legalFor(column.key);
            const over = droppable && overColumn === column.key;
            // An EMPTY Cancelled column is a slim rail (title + count, ~56px): cancelled work is
            // the exception on a desk, and a full-width column of "Nothing in this status"
            // took a quarter of the board to say so. It widens the moment it holds a card,
            // and it stays a drop target while it is a rail. The other columns never collapse
            // -- an empty To do is news, not noise.
            const rail = column.key === "cancelled" && cards.length === 0 && activeFilter !== column.key;
            return (
              <section
                key={column.key}
                className={`ltb-col ltb-col-${column.key}${
                  activeFilter === column.key ? " is-focused" : ""
                }${rail ? " is-rail" : ""}${droppable ? " ltb-drop-ok" : ""}${over ? " ltb-drop-over" : ""}${
                  draggingTask && !droppable ? " ltb-drop-no" : ""
                }`}
                data-ltb-rail={rail ? "true" : undefined}
                aria-label={rail ? column.label : undefined}
                onDragOver={(event) => {
                  // Only a legal column calls preventDefault, which is what MAKES it a drop
                  // target: an illegal one keeps the browser's own `no-drop` cursor and its
                  // `drop` never fires. The legality is the row's `status_options`, not a rule
                  // written here.
                  if (!droppable) return;
                  event.preventDefault();
                  event.dataTransfer.dropEffect = "move";
                  if (overColumn !== column.key) setOverColumn(column.key);
                }}
                onDragLeave={(event) => {
                  // Only when the pointer really left this section, not when it crossed onto a
                  // card inside it.
                  if (event.currentTarget.contains(event.relatedTarget as Node | null)) return;
                  setOverColumn((current) => (current === column.key ? null : current));
                }}
                onDrop={(event) => {
                  event.preventDefault();
                  setOverColumn(null);
                  /**
                   * The card being dragged is identified from this component's OWN state first,
                   * and from the drag payload only as a fallback. Both are written by the same
                   * `dragstart`, so they agree — state is primary because it is the SAME value the
                   * column's droppability was computed from, so the drop and the highlight the
                   * reader was shown can never be about two different cards. The payload is still
                   * set, and still read as a fallback, because it is what makes a drop OUTSIDE the
                   * board carry a private type instead of the card's URL.
                   */
                  const taskID =
                    draggingTaskID || event.dataTransfer.getData(DRAG_MIME);
                  const task = rows.find((row) => row.id === taskID);
                  setDraggingTaskID(null);
                  // Re-checked against the row's own options: the dragover guard is a cursor, and
                  // a cursor is not an authorisation.
                  if (!task) return;
                  if (!task.statusOptions.some((option) => option.key === column.key)) return;
                  if (task.status === column.key) return;
                  move(task, column.key);
                }}
              >
                <header className="ltb-colhd">
                  <span className="ltb-colname">{column.label}</span>
                  {/* A pill never reads "—" (Gate-1 #4, #14): where the list query publishes
                      no whole-list total for a column -- the overdue lens, and Cancelled --
                      the pill counts the cards on this page and its tooltip says so, so the
                      numbers beside each other reconcile (the header's "142 of 408", the
                      column pills, the "N on this page" lines). */}
                  <span
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
                  >
                    {column.total === null ? cards.length : column.total}
                  </span>
                </header>
                <div className="ltb-colbd">
                  {cards.length ? (
                    cards.map((task) => (
                      <TaskBoardCard
                        key={task.id}
                        task={task}
                        pageContract={pageContract}
                        href={cardHrefs[task.id] ?? ""}
                        selected={task.id === selectedTaskID}
                        /**
                         * A card is draggable only on a wide, precise-pointer client AND only
                         * when the backend gave this actor somewhere to drag it. Everything else
                         * is `draggable={false}`, which ALSO switches off the browser's native
                         * anchor drag — an `<a>` is draggable by default, so "no drag" has to be
                         * said out loud.
                         */
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
                  ) : (
                    column.emptyMessage ? <p className="ltb-colempty">{column.emptyMessage}</p> : null
                  )}
                </div>
              </section>
            );
          })}
        </div>
      </div>

      {/* Not `aria-grabbed` (deprecated): the move in flight is announced as text instead. */}
      <p className="ltb-dndlive" role="status" aria-live="polite">
        {announcement}
      </p>
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
