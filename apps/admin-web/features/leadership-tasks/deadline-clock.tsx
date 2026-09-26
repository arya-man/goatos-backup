import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";

import { Label } from "@/components/minimal/label";

import type { TaskRow } from "./task-row";

/**
 * THE BIG NUMBER: days left to the deadline, green while more than two days remain and red from
 * two days out and once overdue, with the deadline beneath. Every value is the backend's -- this
 * component counts nothing and decides no colour; it only maps the tone the backend named onto a
 * template soft Label and splits the worded label into number + unit. A task without a deadline
 * renders a quiet dash so the column still lines up.
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
    return compact ? (
      <Typography component="span" variant="body2" sx={{ color: "text.disabled" }}>
        —
      </Typography>
    ) : null;
  }
  const color = task.deadlineTone === "ok" ? "success" : "error";
  // "5 days left" -> 5 + "days left"; "Due today" has no number and shows the words alone.
  const unit = task.daysLeftLabel.replace(/^\d+\s*/, "");
  const showNumber = task.daysLeft !== 0;
  return (
    <Box
      role="group"
      aria-label={`${task.daysLeftLabel}, ${task.deadlineStateLabel.toLowerCase()}, ${deadlineWord.toLowerCase()} ${task.deadlineLabel}`}
      sx={{ display: "flex", flexDirection: "column", alignItems: "flex-start", gap: 0.5, minWidth: 0 }}
    >
      <Label variant="soft" color={color} sx={{ fontVariantNumeric: "tabular-nums" }}>
        {showNumber ? <Box component="b" sx={{ mr: 0.5 }}>{Math.abs(task.daysLeft)}</Box> : null}
        {unit}
      </Label>
      <Typography component="span" variant="caption" sx={{ color: "text.secondary", ...(compact ? {} : { typography: "body2" }) }}>
        {compact ? task.deadlineLabel : `${task.deadlineStateLabel} · ${deadlineWord} ${task.deadlineLabel}`}
      </Typography>
    </Box>
  );
}
