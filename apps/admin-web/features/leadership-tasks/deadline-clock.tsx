import type { TaskRow } from "./task-row";

/**
 * THE BIG NUMBER: days left to the deadline, green while more than two days remain and red from
 * two days out and once overdue, with the deadline beneath. Every value is the backend's -- this
 * component counts nothing and decides no colour; it only maps the tone the backend named onto a
 * class and splits the worded label into number + unit. A task without a deadline renders a quiet
 * dash so the column still lines up.
 */
export function DeadlineClock({
  task,
  compact = false,
  deadlineWord = "Deadline",
}: {
  task: TaskRow;
  compact?: boolean;
  /** The word before the deadline, resolved from the page contract by the caller. */
  deadlineWord?: string;
}) {
  if (!task.deadlineTone || task.daysLeft === null) {
    return compact ? <span className="muted small">—</span> : null;
  }
  const tone = task.deadlineTone === "ok" ? "lt-clock-ok" : "lt-clock-late";
  // "5 days left" -> 5 + "days left"; "Due today" has no number and shows the words alone.
  const unit = task.daysLeftLabel.replace(/^\d+\s*/, "");
  const showNumber = task.daysLeft !== 0;
  return (
    <div
      className={`lt-clock ${tone}${compact ? " lt-clock-compact" : ""}`}
      role="group"
      aria-label={`${task.daysLeftLabel}, ${task.deadlineStateLabel.toLowerCase()}, ${deadlineWord.toLowerCase()} ${task.deadlineLabel}`}
    >
      <div className="lt-clock-num">
        {showNumber ? <b>{Math.abs(task.daysLeft)}</b> : null}
        <span>{unit}</span>
      </div>
      <div className="lt-clock-meta">
        <span className="lt-clock-state">{task.deadlineStateLabel}</span>
        <span className="lt-clock-deadline">
          {deadlineWord} {task.deadlineLabel}
        </span>
      </div>
    </div>
  );
}
