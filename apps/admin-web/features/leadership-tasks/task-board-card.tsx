import Link from "@/components/no-prefetch-link";
import { Paperclip } from "lucide-react";
import type { DragEvent } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import { DeadlineClock } from "./deadline-clock";
import { initials } from "./task-presentation";
import type { TaskRow } from "./task-row";

/**
 * One card on the status board.
 *
 * It is a LINK to `?task=<uuid>`, the same deep link a table row mints, so the detail panel, the
 * back button and a pasted URL all keep working and the board adds no second way to select a
 * task. Nothing on the card is computed here: the number, the status wording, the deadline
 * countdown and its tone are all the backend's, exactly as the table renders them.
 *
 * WHAT THE CARD SHOWS, and why in this order
 *   1. the title, clamped to three lines — the only thing a reader scans a column for;
 *   2. the deadline clock, when the task has a deadline. This product has no priority field:
 *      the deadline tone (ok / near / over) IS the urgency, so the clock sits where Jira puts
 *      its coloured label chip and carries the same "what is on fire" signal;
 *   3. the assignee's avatar AND NAME, plus who raised it. Our readers identify each other by
 *      name, not by a coloured circle, so the name is text and the circle is decoration beside
 *      it. Both truncate with an ellipsis rather than wrapping the card to twice its height;
 *   4. the issue key and the attachment count, when there is one.
 *
 * DRAGGING IS NOT DECIDED HERE. The card renders whatever `task-board-dnd.tsx` hands it, and
 * `draggable` is REQUIRED rather than optional on purpose: an `<a>` is draggable by default, so a
 * caller that forgot to say either way would ship the browser's native URL drag. The card owns no
 * drag state and no transition rule; it is told.
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
  /** False also switches OFF the browser's own anchor drag. See the note above. */
  draggable: boolean;
  /** This card is the one under the reader's hand. */
  dragging?: boolean;
  /** This card's status change is in flight; its column here is provisional. */
  pending?: boolean;
  onDragStart?: (event: DragEvent<HTMLAnchorElement>) => void;
  onDragEnd?: (event: DragEvent<HTMLAnchorElement>) => void;
}) {
  const deadlineWord = copy(pageContract, "label.deadline");
  return (
    <Link
      href={href}
      scroll={false}
      className={`ltb-card${selected ? " is-selected" : ""}${dragging ? " is-dragging" : ""}${
        pending ? " is-moving" : ""
      }`}
      aria-current={selected ? "true" : undefined}
      aria-label={`${copy(pageContract, "action.open_task")} ${task.number}: ${task.title}`}
      draggable={draggable}
      onDragStart={draggable ? onDragStart : undefined}
      onDragEnd={draggable ? onDragEnd : undefined}
    >
      {/* The clamp sits on an INNER span on purpose: a direct grid item has its `display`
          blockified, which silently discards `-webkit-box` and takes the three-line clamp with
          it. Measured on the live board — as a grid item the title computed to `flow-root` with a
          height of ZERO and every card rendered its title invisible. */}
      <span className="ltb-card-title">
        <span className="ltb-card-titletx">{task.title}</span>
      </span>

      {task.deadlineTone ? (
        <span className="ltb-card-clock">
          <DeadlineClock task={task} compact deadlineWord={deadlineWord} />
        </span>
      ) : null}

      <span className="ltb-card-people">
        <span className="lt-avx" aria-hidden="true">
          {initials(task.assignee)}
        </span>
        <span className="ltb-card-who">
          <span className="ltb-card-name" title={task.assignee}>
            {task.assignee}
          </span>
          <span className="ltb-card-raiser" title={task.raisedBy}>
            {copy(pageContract, "column.raised_by")} {task.raisedBy}
          </span>
        </span>
      </span>

      <span className="ltb-card-foot">
        <b className="ltb-card-key">{task.number}</b>
        <span className="ltb-card-age">{task.age}</span>
        {task.attachments > 0 ? (
          <span
            className="ltb-card-att"
            aria-label={`${copy(pageContract, "column.evidence")} ${task.attachments}`}
          >
            <Paperclip className="ic" aria-hidden="true" />
            <b>{task.attachments}</b>
          </span>
        ) : null}
      </span>
    </Link>
  );
}
